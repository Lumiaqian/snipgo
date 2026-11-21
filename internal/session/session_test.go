package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"snipgo/internal/annotate"
	"snipgo/internal/crop"
)

type fakeUI struct {
	status string
	shown  int
	hidden int
	pins   int
	saved  [][]byte
	ocr    []*image.NRGBA
	at     image.Point
	dip    image.Point
}

func (f *fakeUI) SetStatus(text string)       { f.status = text }
func (f *fakeUI) ShowSelection([]crop.Screen) { f.shown++ }
func (f *fakeUI) HideSelection()              { f.hidden++ }
func (f *fakeUI) ShowPin(_ *image.NRGBA, _ []byte, at image.Point, dipW, dipH int) {
	f.pins++
	f.at = at
	f.dip = image.Pt(dipW, dipH)
}
func (f *fakeUI) SavePNG(b []byte) {
	f.saved = append(f.saved, append([]byte(nil), b...))
}
func (f *fakeUI) RecognizeImage(img *image.NRGBA) {
	f.ocr = append(f.ocr, img)
}

type fakeClip struct {
	data []byte
	text string
	err  error
}

func (f *fakeClip) WritePNG(b []byte) error {
	if f.err != nil {
		return f.err
	}
	f.data = append([]byte(nil), b...)
	return nil
}

func (f *fakeClip) WriteText(s string) { f.text = s }

func redScreen() crop.Screen {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	img.SetNRGBA(2, 2, color.NRGBA{G: 255, A: 255})
	return crop.Screen{Bounds: image.Rect(0, 0, 20, 20), Scale: 2, Pixels: img}
}

func TestHotkeyDeniedDoesNotCapture(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	launches := 0
	s := &Session{
		Allowed: func() (bool, string) { return false, "需要屏幕录制权限" },
		Clip:    clip,
		UI:      ui,
		Launch:  func(int) { launches++ },
	}
	s.Hotkey()
	if launches != 0 || clip.data != nil || ui.shown != 0 {
		t.Fatalf("launches %d clip %d shown %d", launches, len(clip.data), ui.shown)
	}
	if ui.status != "需要屏幕录制权限" || s.Status() != ui.status {
		t.Fatalf("status %q", ui.status)
	}
}

func TestHotkeyCapturesAndSelects(t *testing.T) {
	ui := &fakeUI{}
	snap := Snapshot{Frames: []crop.Screen{redScreen()}}
	calls := 0
	done := make(chan struct{})
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		Capture: func(context.Context) (Snapshot, error) {
			calls++
			return snap, nil
		},
		Clip: &fakeClip{},
		UI:   ui,
		OnMain: func(fn func()) {
			fn()
			close(done)
		},
	}
	s.Hotkey()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture did not return")
	}
	if calls != 1 || ui.shown != 1 {
		t.Fatalf("calls %d shown %d", calls, ui.shown)
	}
}

func TestStaleCaptureIsDropped(t *testing.T) {
	ui := &fakeUI{}
	var gens []int
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      ui,
		Clip:    &fakeClip{},
		Launch:  func(gen int) { gens = append(gens, gen) },
	}
	s.Hotkey()
	s.Deliver(gens[0], Snapshot{Frames: []crop.Screen{redScreen()}}, nil)
	if ui.shown != 1 {
		t.Fatal("first capture")
	}
	s.Hotkey()
	if ui.hidden != 1 {
		t.Fatal("second hotkey should close the selection")
	}
	s.Deliver(gens[0], Snapshot{Frames: []crop.Screen{redScreen()}}, nil)
	if ui.shown != 1 {
		t.Fatalf("stale capture shown again: %d", ui.shown)
	}
	s.Deliver(gens[1], Snapshot{Frames: []crop.Screen{redScreen()}}, nil)
	if ui.shown != 2 {
		t.Fatalf("fresh capture shown %d", ui.shown)
	}
}

func TestDragEntersAdjustingWithoutAutoPin(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s := ready(ui, clip, nil)
	s.BeginDrag(0, 3.2, 4)
	s.MoveDrag(0, 1, 1) // 反向拖动，规范化成 (1,1)-(3,4)
	s.EndDrag()

	if s.Phase() != PhaseAdjusting {
		t.Fatalf("phase %v, want PhaseAdjusting", s.Phase())
	}
	// 松手只锁定选区，不自动复制也不强制贴图。
	if clip.data != nil || ui.pins != 0 {
		t.Fatalf("unexpected auto action: clip %d pins %d", len(clip.data), ui.pins)
	}
	rect, locked, ok := s.ActiveRect()
	if !ok || !locked || rect != image.Rect(1, 1, 3, 4) {
		t.Fatalf("active rect %v locked %v ok %v", rect, locked, ok)
	}

	// 按回车复制：只写剪贴板，不生成贴图。
	s.Copy()
	if ui.pins != 0 || s.Phase() != PhaseIdle {
		t.Fatalf("pins %d phase %v", ui.pins, s.Phase())
	}
	got, err := png.Decode(bytes.NewReader(clip.data))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 6 {
		t.Fatalf("png %v, want 4x6", got.Bounds().Size())
	}
	if c := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA); c != (color.NRGBA{G: 255, A: 255}) {
		t.Fatalf("png origin %v, want marked pixel", c)
	}
}

func TestHoverAndSingleClickSelectsWindowWhenSpaceToggled(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	wins := []image.Rectangle{
		image.Rect(2, 3, 12, 14), // 顶层窗口
		image.Rect(0, 0, 18, 18), // 下层窗口
	}
	s := ready(ui, clip, wins)
	// 进入 Selecting 后鼠标悬停自动高亮顶层窗口，单击锁定进入 Adjusting。
	if !s.Hover(5, 6) {
		t.Fatal("hover should highlight window under cursor")
	}
	rect, locked, ok := s.ActiveRect()
	if !ok || locked || rect != wins[0] {
		t.Fatalf("hover rect %v locked %v ok %v", rect, locked, ok)
	}

	s.BeginDrag(0, 5, 6)
	s.EndDrag()
	if s.Phase() != PhaseAdjusting {
		t.Fatalf("phase %v, want PhaseAdjusting", s.Phase())
	}
	rect, locked, ok = s.ActiveRect()
	if !ok || !locked || rect != wins[0] {
		t.Fatalf("locked rect %v", rect)
	}

	// 第一次右键退回 Selecting，第二次右键退出截图。
	s.RightClick()
	if s.Phase() != PhaseSelecting {
		t.Fatalf("first right click should return to PhaseSelecting, got %v", s.Phase())
	}
	s.RightClick()
	if s.Phase() != PhaseIdle {
		t.Fatalf("second right click should cancel to PhaseIdle, got %v", s.Phase())
	}
}

func TestAutoSelectFrontWindowAndDynamicHover(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	front := image.Rect(10, 10, 100, 90)
	other := image.Rect(110, 20, 190, 150)
	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:      []crop.Screen{{Bounds: image.Rect(0, 0, 200, 200), Scale: 2}},
				FrontWindow: front,
				Windows:     []image.Rectangle{front, other},
			}, nil
		},
		Clip: clip,
		UI:   ui,
	}
	s.StartLive()
	s.InitCursor(195, 5) // 模拟从屏幕右上角菜单栏触发
	// 1. 启动瞬间即自动高亮当前前台应用窗口。
	r, locked, ok := s.ActiveRect()
	if !ok || locked || r != front || s.Phase() != PhaseSelecting {
		t.Fatalf("front window not highlighted initially: rect=%v locked=%v phase=%v", r, locked, s.Phase())
	}
	// 2. 鼠标移向另一个窗口后，高亮框自动跟随切换到该窗口，单击即可锁定进入 Adjusting。
	if !s.Hover(130, 60) {
		t.Fatal("moving cursor to another window should switch hoverWin")
	}
	r, locked, ok = s.ActiveRect()
	if !ok || locked || r != other {
		t.Fatalf("hover did not switch to other window: %v", r)
	}
	s.BeginDrag(0, 130, 60)
	s.EndDrag()
	r, locked, ok = s.ActiveRect()
	if !ok || !locked || r != other || s.Phase() != PhaseAdjusting {
		t.Fatalf("single click should lock hovered window: rect=%v locked=%v phase=%v", r, locked, s.Phase())
	}
}

func TestMaximizedForegroundWindowOccludesBackgroundWindows(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	screen := crop.Screen{Bounds: image.Rect(1728, 0, 3648, 1080), Scale: 2}
	maxWarp := image.Rect(1728, 0, 3648, 1080) // 顶层全屏/最大化窗口
	buried := image.Rect(2000, 200, 2800, 700) // 被遮挡在底下的小窗口
	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:      []crop.Screen{screen},
				FrontWindow: maxWarp,
				Windows:     []image.Rectangle{maxWarp, buried},
			}, nil
		},
		Clip: clip,
		UI:   ui,
	}
	s.StartLive()
	s.InitCursor(2400, 400)
	// Z 序在前的最大化窗口不能被跳过，绝不能穿透选中底下被遮住的窗口。
	r, locked, ok := s.ActiveRect()
	if !ok || locked || r != maxWarp {
		t.Fatalf("expected maximized foreground window %v, got %v", maxWarp, r)
	}
	s.Hover(2450, 450)
	if r, _, _ = s.ActiveRect(); r != maxWarp {
		t.Fatalf("hover over maximized foreground window picked occluded %v, want %v", r, maxWarp)
	}
}

func TestHotkeyPrefersCursorWindowWhileMenuPrefersFrontWindow(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	front := image.Rect(50, 120, 600, 500)
	side := image.Rect(650, 30, 1200, 500)
	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:      []crop.Screen{{Bounds: image.Rect(0, 0, 1400, 800), Scale: 2}},
				FrontWindow: front,
				Windows:     []image.Rectangle{front, side},
			}, nil
		},
		Clip: clip,
		UI:   ui,
	}

	// 1. 快捷键启动且光标正指着可见旁侧窗口 side：立即精确高亮光标下的 side，而非跳回 front。
	s.StartLive()
	s.InitCursor(800, 200)
	if r, _, ok := s.ActiveRect(); !ok || r != side {
		t.Fatalf("hotkey should highlight window under cursor %v, got %v", side, r)
	}
	// 在空白桌面单击（不拖拽）应清空悬停，不能误锁远处的窗口。
	s.BeginDrag(0, 1300, 700)
	s.EndDrag()
	if s.Phase() != PhaseSelecting {
		t.Fatalf("single click on empty desktop should stay in PhaseSelecting, got %v", s.Phase())
	}

	// 2. 从菜单栏点击启动（光标被迫在右上角菜单位置压在 side 上）：优先选中当前应用窗口 front；
	// 随后用户移动鼠标 >= 4px 后再跟随光标切换到 side。
	s.StartLiveFromMenu()
	s.InitCursor(1100, 45)
	if r, _, ok := s.ActiveRect(); !ok || r != front {
		t.Fatalf("menu start should highlight FrontWindow %v initially, got %v", front, r)
	}
	if !s.Hover(900, 250) {
		t.Fatal("moving mouse after menu start should switch to window under cursor")
	}
	if r, _, _ := s.ActiveRect(); r != side {
		t.Fatalf("after mouse move got %v, want %v", r, side)
	}
}

func TestAdjustMoveResizeAndNudge(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s := ready(ui, clip, nil)
	s.BeginDrag(0, 2, 2)
	s.MoveDrag(0, 10, 10)
	s.EndDrag()

	// 选区内平移 (+3, +4) -> (5,6)-(13,14)
	if h := s.HitTest(6, 6); h != HandleMove {
		t.Fatalf("hit %v, want HandleMove", h)
	}
	s.BeginDrag(0, 6, 6)
	s.MoveDrag(0, 9, 10)
	s.EndDrag()
	if r, _, _ := s.ActiveRect(); r != image.Rect(5, 6, 13, 14) {
		t.Fatalf("after move: %v", r)
	}

	// 拖拽右下角手柄 (+2, -2) -> (5,6)-(15,12)
	if h := s.HitTest(13, 14); h != HandleSE {
		t.Fatalf("hit %v, want HandleSE", h)
	}
	s.BeginDrag(0, 13, 14)
	s.MoveDrag(0, 15, 12)
	s.EndDrag()
	if r, _, _ := s.ActiveRect(); r != image.Rect(5, 6, 15, 12) {
		t.Fatalf("after resize: %v", r)
	}

	// 方向键微调平移 (-1, +2) -> (4,8)-(14,14)
	s.Nudge(-1, 2, false)
	if r, _, _ := s.ActiveRect(); r != image.Rect(4, 8, 14, 14) {
		t.Fatalf("after nudge: %v", r)
	}

	// Pin 只贴图，不覆盖剪贴板。
	s.Pin()
	if ui.pins != 1 || ui.at != image.Pt(4, 8) || ui.dip != image.Pt(10, 6) || clip.data != nil {
		t.Fatalf("pin %d at %v dip %v clip %d", ui.pins, ui.at, ui.dip, len(clip.data))
	}
}

func TestCopyColorAndSave(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s := ready(ui, clip, nil)
	// 光标在 DIP (1.1, 1.1) -> Scale 2 下像素 (2, 2) 是预设绿点 #00FF00。
	s.Hover(1.1, 1.1)
	if !s.CopyColor(false) || clip.text != "#00FF00" {
		t.Fatalf("copied color %q, want #00FF00", clip.text)
	}

	s = ready(ui, clip, nil)
	s.BeginDrag(0, 0, 0)
	s.MoveDrag(0, 6, 6)
	s.EndDrag()
	s.Save()
	if len(ui.saved) != 1 || len(ui.saved[0]) == 0 || s.Phase() != PhaseIdle {
		t.Fatalf("saved %d phase %v", len(ui.saved), s.Phase())
	}
}

func TestHotkeyIgnoredWhileCapturing(t *testing.T) {
	launches := 0
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      &fakeUI{},
		Clip:    &fakeClip{},
		Launch:  func(int) { launches++ },
	}
	s.Hotkey()
	s.Hotkey()
	if launches != 1 {
		t.Fatalf("launches %d, want 1", launches)
	}
	s.Deliver(s.gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil)
	if s.phase != PhaseSelecting {
		t.Fatal("first capture should still be delivered")
	}
}

func TestLiveModeShowsImmediatelyAndCapturesOnlyOnConfirm(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	captures := 0
	var capturedSel image.Rectangle
	done := make(chan struct{})

	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:  []crop.Screen{{Bounds: image.Rect(0, 0, 100, 80), Scale: 2}},
				Windows: []image.Rectangle{image.Rect(10, 12, 50, 42)},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			captures++
			capturedSel = sel
			img := image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2))
			img.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
			return img, nil
		},
		Clip: clip,
		UI:   ui,
		OnMain: func(fn func()) {
			fn()
			close(done)
		},
	}

	// 按下快捷键：0 次截图调用即立刻显示蒙层。
	s.Hotkey()
	if captures != 0 || ui.shown != 1 || !s.IsLive() || s.Phase() != PhaseSelecting {
		t.Fatalf("captures %d shown %d live %v phase %v", captures, ui.shown, s.IsLive(), s.Phase())
	}

	// 悬停并单击窗口锁定到 Adjusting，仍未触发屏幕采集。
	s.Hover(20, 20)
	s.BeginDrag(0, 20, 20)
	s.EndDrag()
	if captures != 0 || s.Phase() != PhaseAdjusting {
		t.Fatalf("captures %d phase %v", captures, s.Phase())
	}

	// 点击复制：立即隐藏蒙层，再异步按选区采集一次。
	s.Copy()
	if ui.hidden != 1 {
		t.Fatalf("overlay should hide immediately on confirm, hidden=%d", ui.hidden)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CaptureRect did not finish")
	}
	if captures != 1 || capturedSel != image.Rect(10, 12, 50, 42) {
		t.Fatalf("captures %d sel %v", captures, capturedSel)
	}
	if len(clip.data) == 0 || ui.pins != 0 {
		t.Fatalf("clip %d pins %d", len(clip.data), ui.pins)
	}
}

func TestAnnotationsWindowOnlyBeautifyOCRAndRuler(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	winRect := image.Rect(10, 10, 90, 70)
	var winCaptured uint32

	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:      []crop.Screen{{Bounds: image.Rect(0, 0, 200, 150), Scale: 2}},
				FrontWindow: winRect,
				FrontWinID:  4242,
				Windows:     []image.Rectangle{winRect},
				WindowInfos: []WinTarget{{ID: 4242, Bounds: winRect}},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		CaptureWindow: func(_ context.Context, winID uint32, scale float64) (*image.NRGBA, error) {
			winCaptured = winID
			return image.NewNRGBA(image.Rect(0, 0, int(float64(winRect.Dx())*scale), int(float64(winRect.Dy())*scale))), nil
		},
		Clip:   clip,
		UI:     ui,
		OnMain: func(fn func()) { fn() },
	}

	s.StartLive()
	s.InitCursor(30, 30)
	// 单击锁定窗口，开启单窗无背景模式 + 美化卡片 + 绘制一条矩形标注。
	s.BeginDrag(0, 30, 30)
	s.EndDrag()
	if !s.CanWindowOnly() || !s.ToggleWindowOnly() || !s.WindowOnly() {
		t.Fatal("WindowOnly should be available on locked window")
	}
	s.ToggleBeautify()
	s.Annotations().Tool = annotate.ToolRect
	s.BeginDrag(0, 20, 20)
	s.MoveDrag(0, 60, 50)
	s.EndDrag()
	if len(s.Annotations().Items()) != 1 {
		t.Fatalf("expected 1 annotation item, got %d", len(s.Annotations().Items()))
	}

	// 第一次右键仅退出当前标注工具回到 ToolNone，保留已锁定选区！
	s.RightClick()
	if s.Phase() != PhaseAdjusting || s.Annotations().Tool != annotate.ToolNone {
		t.Fatalf("right click with active tool should stay in Adjusting and clear tool: phase=%v tool=%v", s.Phase(), s.Annotations().Tool)
	}

	done := make(chan struct{})
	s.OnMain = func(fn func()) {
		fn()
		close(done)
	}
	s.Pin()
	<-done
	if winCaptured != 4242 || ui.pins != 1 || ui.dip.X <= winRect.Dx() {
		t.Fatalf("expected independent window 4242 + beautify padded pin: win=%d pins=%d dip=%v", winCaptured, ui.pins, ui.dip)
	}

	// 测试 OCR 动作。
	doneOCR := make(chan struct{})
	s.OnMain = func(fn func()) {
		fn()
		close(doneOCR)
	}
	s.StartLive()
	s.InitCursor(30, 30)
	s.OCR()
	<-doneOCR
	if len(ui.ocr) != 1 || ui.ocr[0] == nil {
		t.Fatalf("expected OCR callback with image, got %d", len(ui.ocr))
	}

	// 测试冻结模式下的十字测量尺：单击直接复制像素宽高。
	frozen := ready(ui, clip, nil)
	if !frozen.ToggleMeasure() || !frozen.MeasureMode() {
		t.Fatal("expected MeasureMode on frozen session")
	}
	frozen.Hover(5, 5)
	if rd, ok := frozen.SampleRuler(); !ok || rd.WidthPx <= 0 || rd.HeightPx <= 0 {
		t.Fatalf("unexpected ruler reading: %+v ok=%v", rd, ok)
	}
	frozen.BeginDrag(0, 5, 5)
	if clip.text == "" {
		t.Fatal("clicking in MeasureMode should copy pixel dimensions to clipboard")
	}
}

func ready(ui *fakeUI, clip *fakeClip, wins []image.Rectangle) *Session {
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      ui,
		Clip:    clip,
		Launch:  func(int) {},
	}
	s.Hotkey()
	s.Deliver(s.gen, Snapshot{Frames: []crop.Screen{redScreen()}, Windows: wins}, nil)
	return s
}

// newLiveFailSession 构造实时模式会话；capture 由调用方通过 *capErr 控制成败，每次 OnMain 通过 done 通知。
func newLiveFailSession(capErr *error, ui *fakeUI, clip *fakeClip, done chan struct{}) *Session {
	return &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 100, 80), Scale: 2}}}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			if *capErr != nil {
				return nil, *capErr
			}
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		Clip: clip,
		UI:   ui,
		OnMain: func(fn func()) {
			fn()
			done <- struct{}{}
		},
	}
}

func waitDone(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CaptureRect did not finish")
	}
}

func drawLiveRectAnnotation(s *Session) {
	s.Hotkey()
	s.BeginDrag(0, 10, 10)
	s.MoveDrag(0, 60, 50)
	s.EndDrag()
	s.Annotations().Tool = annotate.ToolRect
	s.BeginDrag(0, 20, 20)
	s.MoveDrag(0, 40, 40)
	s.EndDrag()
}

func TestLiveCaptureFailureKeepsSelectionAndAnnotations(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	done := make(chan struct{}, 1)
	capErr := errors.New("boom")
	s := newLiveFailSession(&capErr, ui, clip, done)

	drawLiveRectAnnotation(s)
	orig, _, ok := s.ActiveRect()
	if !ok || orig != image.Rect(10, 10, 60, 50) || len(s.Annotations().Items()) != 1 {
		t.Fatalf("setup: rect %v ok %v items %d", orig, ok, len(s.Annotations().Items()))
	}
	s.Copy()
	waitDone(t, done)
	if s.Phase() != PhaseIdle || !s.HasAborted() || s.AbortedItemCount() != 1 {
		t.Fatalf("phase %v aborted %v count %d", s.Phase(), s.HasAborted(), s.AbortedItemCount())
	}
	if ui.status != "boom" {
		t.Fatalf("status %q", ui.status)
	}

	shown := ui.shown
	if !s.RestoreAborted() {
		t.Fatal("RestoreAborted failed")
	}
	if s.HasAborted() || s.Phase() != PhaseAdjusting || ui.shown != shown+1 || !s.IsLive() {
		t.Fatalf("aborted %v phase %v shown %d->%d live %v", s.HasAborted(), s.Phase(), shown, ui.shown, s.IsLive())
	}
	if r, _, ok := s.ActiveRect(); !ok || r != orig {
		t.Fatalf("restored rect %v ok %v", r, ok)
	}
	items := s.Annotations().Items()
	if len(items) != 1 || items[0].Tool != annotate.ToolRect {
		t.Fatalf("restored items %+v", items)
	}

	// 恢复后重试成功：写入剪贴板，且不再有可恢复快照。
	capErr = nil
	s.Copy()
	waitDone(t, done)
	if len(clip.data) == 0 || s.HasAborted() || s.Phase() != PhaseIdle {
		t.Fatalf("clip %d aborted %v phase %v", len(clip.data), s.HasAborted(), s.Phase())
	}
}

func TestLiveCaptureSuccessLeavesNoAborted(t *testing.T) {
	ui := &fakeUI{}
	done := make(chan struct{}, 1)
	var capErr error
	s := newLiveFailSession(&capErr, ui, &fakeClip{}, done)
	drawLiveRectAnnotation(s)
	s.Copy()
	waitDone(t, done)
	if s.HasAborted() || s.AbortedItemCount() != 0 || s.RestoreAborted() {
		t.Fatal("successful capture must not leave an aborted stash")
	}
}

func TestLiveCaptureTimeoutStashesAndDiscard(t *testing.T) {
	ui := &fakeUI{}
	done := make(chan struct{}, 1)
	capErr := context.DeadlineExceeded
	s := newLiveFailSession(&capErr, ui, &fakeClip{}, done)
	drawLiveRectAnnotation(s)
	s.Copy()
	waitDone(t, done)
	if ui.status != "截图超时，请重试" || !s.HasAborted() || s.AbortedItemCount() != 1 {
		t.Fatalf("status %q aborted %v count %d", ui.status, s.HasAborted(), s.AbortedItemCount())
	}
	s.DiscardAborted()
	if s.HasAborted() || s.RestoreAborted() {
		t.Fatal("discard should drop the stash")
	}
}

// previewTestSession 构造一个已锁定窗口选区(10,12,50,42)、scale=2 的实时会话。
func previewTestSession(t *testing.T, captures *int, withPreview bool) (*Session, *fakeClip, chan struct{}) {
	t.Helper()
	clip := &fakeClip{}
	done := make(chan struct{})
	s := &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:  []crop.Screen{{Bounds: image.Rect(0, 0, 100, 80), Scale: 2}},
				Windows: []image.Rectangle{image.Rect(10, 12, 50, 42)},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			*captures++
			img := image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2))
			for i := 0; i < len(img.Pix); i += 4 {
				img.Pix[i+2], img.Pix[i+3] = 255, 255
			}
			return img, nil
		},
		Clip: clip,
		UI:   &fakeUI{},
		OnMain: func(fn func()) {
			fn()
			close(done)
		},
	}
	if withPreview {
		s.PreviewFrame = func(sel image.Rectangle) (*image.NRGBA, bool) {
			img := image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2))
			for i := 0; i < len(img.Pix); i += 4 {
				img.Pix[i], img.Pix[i+3] = 255, 255
			}
			return img, true
		}
	}
	s.Hotkey()
	s.Hover(20, 20)
	s.BeginDrag(0, 20, 20)
	s.EndDrag()
	if s.Phase() != PhaseAdjusting {
		t.Fatalf("phase %v", s.Phase())
	}
	return s, clip, done
}

func centerPixel(t *testing.T, data []byte) color.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	return color.NRGBAModel.Convert(img.At(b.Dx()/2, b.Dy()/2)).(color.NRGBA)
}

func TestLiveConfirmUsesPreviewFrameForRedaction(t *testing.T) {
	captures := 0
	s, clip, done := previewTestSession(t, &captures, true)
	s.Annotations().Items() // 确保画布已创建
	s.Annotations().AddRedactions([]annotate.RectF{{X: 10, Y: 12, W: 40, H: 30}}, true)
	s.Copy()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("confirm did not finish")
	}
	if captures != 0 {
		t.Fatalf("CaptureRect called %d times, want 0", captures)
	}
	// 擦除会取周围像素，但源图是纯红，结果必然偏红而不是蓝。
	if c := centerPixel(t, clip.data); c.R < 200 || c.B > 60 {
		t.Fatalf("center pixel %+v, want red-ish from preview frame", c)
	}
}

func TestLiveConfirmWithoutRedactionStillCaptures(t *testing.T) {
	captures := 0
	s, clip, done := previewTestSession(t, &captures, true)
	s.Copy()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("confirm did not finish")
	}
	if captures != 1 {
		t.Fatalf("CaptureRect called %d times, want 1", captures)
	}
	if c := centerPixel(t, clip.data); c.B < 200 || c.R > 60 {
		t.Fatalf("center pixel %+v, want blue from CaptureRect", c)
	}
}

func TestLiveConfirmIgnoresMismatchedPreviewFrame(t *testing.T) {
	captures := 0
	s, _, done := previewTestSession(t, &captures, true)
	s.PreviewFrame = func(image.Rectangle) (*image.NRGBA, bool) {
		return image.NewNRGBA(image.Rect(0, 0, 3, 3)), true
	}
	s.Annotations().AddRedactions([]annotate.RectF{{X: 10, Y: 12, W: 40, H: 30}}, false)
	s.Copy()
	<-done
	if captures != 1 {
		t.Fatalf("CaptureRect called %d times, want 1 for size mismatch", captures)
	}
}

// encodeOffMainSession 构造 OnMain 会记录“当前在主线程回调内”的会话，并让 Encode 钩子记录是否运行在主线程回调里。
func encodeOffMainSession(ui *fakeUI, clip *fakeClip, live bool) (s *Session, encodedInMain *atomic.Bool, encodes *atomic.Int32, done chan struct{}) {
	encodedInMain = &atomic.Bool{}
	encodes = &atomic.Int32{}
	done = make(chan struct{}, 4)
	var inMain atomic.Bool
	s = &Session{
		Allowed: func() (bool, string) { return true, "" },
		Clip:    clip,
		UI:      ui,
		OnMain: func(fn func()) {
			inMain.Store(true)
			fn()
			inMain.Store(false)
			done <- struct{}{}
		},
		Encode: func(img *image.NRGBA) ([]byte, error) {
			encodes.Add(1)
			if inMain.Load() {
				encodedInMain.Store(true)
			}
			var buf bytes.Buffer
			err := png.Encode(&buf, img)
			return buf.Bytes(), err
		},
	}
	if live {
		s.Live = func() (Snapshot, error) {
			return Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 100, 80), Scale: 2}}}, nil
		}
		s.CaptureRect = func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		}
	} else {
		snap := Snapshot{Frames: []crop.Screen{redScreen()}}
		s.Launch = func(int) {}
		s.Hotkey()
		s.Deliver(s.gen, snap, nil)
	}
	return
}

func TestFrozenCopyEncodesOffMainThread(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s, inMain, encodes, done := encodeOffMainSession(ui, clip, false)
	s.BeginDrag(0, 0, 0)
	s.MoveDrag(0, 20, 20)
	s.EndDrag()
	s.Copy()
	waitDone(t, done)
	if encodes.Load() != 1 || inMain.Load() {
		t.Fatalf("encodes %d encodedInMain %v", encodes.Load(), inMain.Load())
	}
	if len(clip.data) == 0 || s.Phase() != PhaseIdle {
		t.Fatalf("clip %d phase %v", len(clip.data), s.Phase())
	}
}

func TestFrozenPinAndSaveStillWorkAsync(t *testing.T) {
	for _, act := range []ConfirmAction{ActionPin, ActionSave} {
		ui := &fakeUI{}
		s, _, _, done := encodeOffMainSession(ui, &fakeClip{}, false)
		s.BeginDrag(0, 0, 0)
		s.MoveDrag(0, 20, 20)
		s.EndDrag()
		s.confirm(act)
		waitDone(t, done)
		if act == ActionPin && ui.pins != 1 || act == ActionSave && len(ui.saved) != 1 || s.Phase() != PhaseIdle {
			t.Fatalf("act %v pins %d saved %d phase %v", act, ui.pins, len(ui.saved), s.Phase())
		}
	}
}

func TestLiveCopyEncodesOffMainThread(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s, inMain, encodes, done := encodeOffMainSession(ui, clip, true)
	s.Hotkey()
	s.BeginDrag(0, 10, 10)
	s.MoveDrag(0, 60, 50)
	s.EndDrag()
	s.Copy()
	waitDone(t, done)
	if encodes.Load() != 1 || inMain.Load() {
		t.Fatalf("encodes %d encodedInMain %v", encodes.Load(), inMain.Load())
	}
	if len(clip.data) == 0 || s.Phase() != PhaseIdle {
		t.Fatalf("clip %d phase %v", len(clip.data), s.Phase())
	}
}

func TestFrozenEncodeFailureReportsAndStaleGenDropped(t *testing.T) {
	ui := &fakeUI{}
	s, _, _, done := encodeOffMainSession(ui, &fakeClip{}, false)
	s.Encode = func(*image.NRGBA) ([]byte, error) { return nil, errors.New("enc") }
	s.BeginDrag(0, 0, 0)
	s.MoveDrag(0, 20, 20)
	s.EndDrag()
	s.Copy()
	waitDone(t, done)
	if ui.status != "enc" || s.Phase() != PhaseIdle {
		t.Fatalf("status %q phase %v", ui.status, s.Phase())
	}
}
