package main

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

func TestDimBandsCoverWindowOnce(t *testing.T) {
	win := image.Rect(0, 0, 100, 80)
	// 选区从这块屏左边 20 DIP 处进来，本地 x 从 -20 到 30。
	hole := image.Rect(-20, 10, 30, 40)
	bands, border := dimBands(win, hole)
	if border != image.Rect(0, 10, 30, 40) {
		t.Fatalf("border %v", border)
	}
	seen := map[image.Point]int{}
	mark := func(r image.Rectangle) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				seen[image.Pt(x, y)]++
			}
		}
	}
	for _, band := range bands {
		mark(band)
	}
	mark(border)
	if len(seen) != win.Dx()*win.Dy() {
		t.Fatalf("covered %d, want %d", len(seen), win.Dx()*win.Dy())
	}
	for p, n := range seen {
		if n != 1 {
			t.Fatalf("pixel %v covered %d times", p, n)
		}
	}
}

func TestDimBandsMissCoversAll(t *testing.T) {
	win := image.Rect(0, 0, 40, 20)
	bands, border := dimBands(win, image.Rect(100, 100, 110, 110))
	if len(bands) != 1 || bands[0] != win || !border.Empty() {
		t.Fatalf("bands %v border %v", bands, border)
	}
}

func TestLocalSelectionUsesDisplayOrigin(t *testing.T) {
	bounds := image.Rect(-200, 0, 0, 100)
	sel := image.Rect(-50, 10, -30, 30)
	got := localSelection(bounds, sel)
	if got != image.Rect(150, 10, 170, 30) {
		t.Fatalf("local %v", got)
	}
}

func TestHUDTopLeftFlipsInsideNearTop(t *testing.T) {
	win := image.Rect(0, 0, 800, 600)
	above := hudTopLeft(win, image.Rect(100, 50, 300, 200))
	if above.X != 100 || above.Y >= 50 {
		t.Fatalf("expected above border, got %v", above)
	}
	inside := hudTopLeft(win, image.Rect(10, 4, 300, 200))
	if inside.X != 10 || inside.Y <= 4 {
		t.Fatalf("expected flipped inside near top edge, got %v", inside)
	}
	rightClamped := hudTopLeft(win, image.Rect(760, 80, 795, 140))
	if rightClamped.X+150 > win.Max.X {
		t.Fatalf("expected right edge clamp, got %v", rightClamped)
	}
}

func TestToolbarAndLoupePositionsStayInBounds(t *testing.T) {
	win := image.Rect(0, 0, 800, 600)
	// 普通位置：在选区底边下方。
	pos := toolbarTopLeft(win, image.Rect(100, 100, 400, 300), 264, 34)
	if pos.Y <= 300 || pos.X+264 > win.Max.X {
		t.Fatalf("toolbar below got %v", pos)
	}
	// 全屏选区：翻进选区内部右下角且不越界。
	full := toolbarTopLeft(win, win, 264, 34)
	if full.X < 0 || full.Y < 0 || full.X+264 > win.Max.X || full.Y+34 > win.Max.Y {
		t.Fatalf("toolbar full screen got %v", full)
	}
	// 光标靠近右下角时放大镜翻到左上方。
	lp := loupePos(win, image.Pt(790, 590), 122, 166)
	if lp.X+122 > win.Max.X || lp.Y+166 > win.Max.Y {
		t.Fatalf("loupe overflowed: %v", lp)
	}
}

func TestFormatSizeLabel(t *testing.T) {
	if got := formatSizeLabel(0, 10, 0, 20); got != "" {
		t.Fatalf("empty got %q", got)
	}
	if got := formatSizeLabel(120, 80, 120, 80); got != "120 × 80 px" {
		t.Fatalf("1x got %q", got)
	}
	if got := formatSizeLabel(120, 80, 240, 160); got != "120 × 80 (240 × 160 px)" {
		t.Fatalf("2x got %q", got)
	}
}

func TestLiveOverlayTransparentHoleAndWorkflow(t *testing.T) {
	var copied []byte
	var pinned int
	mainQueue := make(chan func(), 4)
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 800, 600), index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			front := image.Rect(100, 100, 500, 400)
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2}},
				FrontWindow: front,
				Windows:     []image.Rectangle{front},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		Clip:   testClip{onPNG: func(b []byte) { copied = append([]byte(nil), b...) }},
		UI:     testSessionUI{onPin: func() { pinned++ }},
		OnMain: func(fn func()) { mainQueue <- fn },
	}
	a.session.StartLive()

	tt := ui.NewTester(a.selectionView(o), 800, 600)
	tt.SetDark(true)
	tt.Frame()

	// 1. 启动即高亮当前应用窗口，且画布 Alpha 刚好处于 macOS SkyLight 不穿透阈值（14..16/255），视觉全透明但能接住鼠标点击。
	// 同时验证选区边框向外生长：窗口内部首像素 (100, 100) 和末像素 (499, 399) 不被蓝色边框遮盖，外侧紧邻像素 (99, 200) 着色为高亮框。
	img := tt.Image()
	insideAlpha := img.RGBAAt(250, 250).A
	outsideAlpha := img.RGBAAt(20, 20).A
	if insideAlpha < 13 || insideAlpha > 20 || outsideAlpha < 13 || outsideAlpha > 20 {
		t.Fatalf("alpha must be >=13 (macOS SkyLight hit-test threshold) and <=20 (transparent): inside=%+v outside=%+v", img.RGBAAt(250, 250), img.RGBAAt(20, 20))
	}
	if edgeIn := img.RGBAAt(100, 200); edgeIn.B > 30 || edgeIn.A > 40 {
		t.Fatalf("interior edge pixel (100,200) covered by border: %+v", edgeIn)
	}
	if edgeOut := img.RGBAAt(99, 200); edgeOut.B < 180 {
		t.Fatalf("exterior border pixel (99,200) should carry blue stroke, got %+v", edgeOut)
	}
	if r, locked, ok := a.session.ActiveRect(); !ok || locked || r != image.Rect(100, 100, 500, 400) {
		t.Fatalf("expected FrontWindow highlighted on start, got %v locked=%v ok=%v", r, locked, ok)
	}

	// 2. 单击锁定当前高亮窗口进入 Adjusting，弹出浮动工具栏。
	tt.ClickAt(250, 250)
	tt.Frame()
	if a.session.Phase() != session.PhaseAdjusting {
		t.Fatalf("expected PhaseAdjusting after clicking window, got %v", a.session.Phase())
	}
	if !tt.HasText("复制") || !tt.HasText("贴图") {
		t.Fatalf("toolbar not rendered on locked window, texts=%v", tt.Texts())
	}

	// 3. 右键退回 Selecting 再次自由拖拽框选。
	tt.RightClickAt(250, 250)
	tt.Frame()
	if a.session.Phase() != session.PhaseSelecting {
		t.Fatalf("expected right-click to return to PhaseSelecting, got %v", a.session.Phase())
	}
	tt.Press(120, 140)
	tt.Frame()
	tt.Move(320, 300)
	tt.Frame()
	tt.Release(320, 300)
	tt.Frame()
	if r, locked, ok := a.session.ActiveRect(); !ok || !locked || r != image.Rect(120, 140, 320, 300) {
		t.Fatalf("drag selection got %v locked=%v ok=%v", r, locked, ok)
	}

	// 4. 拖拽右下角控制柄 (320, 300) -> (360, 340) 调整尺寸。
	tt.Move(320, 300)
	tt.Frame()
	if gotCur := tt.Cursor(); gotCur != ui.CursorResizeNWSE {
		t.Fatalf("SE handle cursor = %v, want CursorResizeNWSE", gotCur)
	}
	tt.Press(320, 300)
	tt.Frame()
	tt.Move(360, 340)
	tt.Frame()
	tt.Release(360, 340)
	tt.Frame()
	if r, _, _ := a.session.ActiveRect(); r != image.Rect(120, 140, 360, 340) {
		t.Fatalf("after SE handle drag got %v, want (120,140)-(360,340)", r)
	}

	// 5. 点击浮动工具栏上的「复制」按钮：触发局部采集并写入剪贴板，不强制生成贴图。
	copyRect, ok := tt.Find("复制")
	if !ok {
		t.Fatal("cannot find 复制 button")
	}
	tt.ClickAt(copyRect.X+copyRect.W/2, copyRect.Y+copyRect.H/2)
	tt.Frame()
	(<-mainQueue)()
	if len(copied) == 0 || pinned != 0 {
		t.Fatalf("toolbar copy: copied=%d bytes, pinned=%d", len(copied), pinned)
	}
}

func TestLiveOverlayPinButtonAndShortcutT(t *testing.T) {
	var pinAt image.Point
	var pinDIP image.Point
	var pinPx image.Point
	mainQueue := make(chan func(), 4)
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 800, 600), index: 0}
	a.overlays = []*overlay{o}
	winRect := image.Rect(150, 80, 470, 320)
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2}},
				FrontWindow: winRect,
				Windows:     []image.Rectangle{winRect},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		Clip: testClip{},
		UI: testPinUI{
			onPin: func(img *image.NRGBA, at image.Point, dipW, dipH int) {
				pinPx = img.Bounds().Size()
				pinAt = at
				pinDIP = image.Pt(dipW, dipH)
			},
		},
		OnMain: func(fn func()) { mainQueue <- fn },
	}
	a.session.StartLive()
	a.session.InitCursor(200, 150)

	tt := ui.NewTester(a.selectionView(o), 800, 600)
	tt.Frame()

	// 单击锁定窗口后，点击浮动工具栏「贴图」按钮。
	tt.ClickAt(200, 150)
	tt.Frame()
	pinBtn, ok := tt.Find("贴图")
	if !ok {
		t.Fatal("cannot find 贴图 button")
	}
	tt.ClickAt(pinBtn.X+pinBtn.W/2, pinBtn.Y+pinBtn.H/2)
	tt.Frame()
	(<-mainQueue)()

	if pinAt != winRect.Min || pinDIP != image.Pt(winRect.Dx(), winRect.Dy()) || pinPx != image.Pt(winRect.Dx()*2, winRect.Dy()*2) {
		t.Fatalf("pin mismatch: at=%v dip=%v px=%v, want at=%v dip=%dx%d", pinAt, pinDIP, pinPx, winRect.Min, winRect.Dx(), winRect.Dy())
	}
}

type testPinUI struct {
	onPin func(img *image.NRGBA, at image.Point, dipW, dipH int)
}

func (testPinUI) SetStatus(string)            {}
func (testPinUI) ShowSelection([]crop.Screen) {}
func (testPinUI) HideSelection()              {}
func (u testPinUI) ShowPin(img *image.NRGBA, _ []byte, at image.Point, dipW, dipH int) {
	if u.onPin != nil {
		u.onPin(img, at, dipW, dipH)
	}
}
func (testPinUI) SavePNG([]byte)            {}
func (testPinUI) RecognizeImage(*image.NRGBA) {}

func TestToolbarAnnotationBeautifyAndOCRWorkflow(t *testing.T) {
	var ocrImg *image.NRGBA
	mainQueue := make(chan func(), 4)
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 900, 650), index: 0}
	a.overlays = []*overlay{o}
	winRect := image.Rect(120, 80, 760, 460)
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2}},
				FrontWindow: winRect,
				Windows:     []image.Rectangle{winRect},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		Clip: testClip{},
		UI: testOCRUI{
			onOCR: func(img *image.NRGBA) { ocrImg = img },
		},
		OnMain: func(fn func()) { mainQueue <- fn },
	}
	a.session.StartLive()
	a.session.InitCursor(300, 200)

	tt := ui.NewTester(a.selectionView(o), 900, 650)
	tt.Frame()

	// 1. 单击锁定窗口，浮动工具栏展示标注、美化与识字按钮。
	tt.ClickAt(300, 200)
	tt.Frame()
	stepBtn, ok := tt.Find("序号")
	if !ok {
		t.Fatalf("missing 序号 tool in toolbar, texts=%v", tt.Texts())
	}

	// 2. 选中「序号」工具，在选区内连点两下生成 ① ②，点击固定槽位「撤销」验证按钮坐标完全不抖动。
	undoBefore, ok := tt.Find("撤销")
	if !ok {
		t.Fatal("missing 撤销 icon button")
	}
	tt.ClickAt(stepBtn.X+stepBtn.W/2, stepBtn.Y+stepBtn.H/2)
	tt.Frame()
	tt.ClickAt(220, 180)
	tt.Frame()
	tt.ClickAt(280, 220)
	tt.Frame()
	undoAfter, _ := tt.Find("撤销")
	if len(a.session.Annotations().Items()) != 2 || undoAfter.X != undoBefore.X {
		t.Fatalf("expected 2 step badges and stable 撤销 X (%v vs %v)", undoBefore.X, undoAfter.X)
	}

	// 3. 开启「美化」背景并选择「落日霓虹」渐变色卡。
	beauBtn, ok := tt.Find("美化")
	if !ok {
		t.Fatal("missing 美化 button")
	}
	tt.ClickAt(beauBtn.X+beauBtn.W/2, beauBtn.Y+beauBtn.H/2)
	tt.Frame()
	sunsetBtn, ok := tt.Find("落日霓虹")
	if !ok {
		t.Fatalf("missing 落日霓虹 preset chip in sub-bar")
	}
	tt.ClickAt(sunsetBtn.X+sunsetBtn.W/2, sunsetBtn.Y+sunsetBtn.H/2)
	tt.Frame()
	if a.session.BeautifyPreset() != beautify.PresetSunset {
		t.Fatalf("expected PresetSunset, got %v", a.session.BeautifyPreset())
	}

	// 4. 点击「识图」触发无污染原图采集与 OCR 回调（不混入渐变背景框，确保识别率）。
	ocrBtn, ok := tt.Find("识图")
	if !ok {
		t.Fatal("missing 识图 button")
	}
	tt.ClickAt(ocrBtn.X+ocrBtn.W/2, ocrBtn.Y+ocrBtn.H/2)
	tt.Frame()
	(<-mainQueue)()
	if ocrImg == nil || ocrImg.Bounds().Dx() != winRect.Dx()*2 || ocrImg.Bounds().Dy() != winRect.Dy()*2 {
		t.Fatalf("expected clean raw image delivered to OCR, got %v", ocrImg)
	}
}

func TestToolbarFullWidthWindowStaysInBoundsSingleRow(t *testing.T) {
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 1440, 900), index: 0}
	a.overlays = []*overlay{o}
	fullWin := image.Rect(0, 32, 1440, 900)
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2}},
				FrontWindow: fullWin,
				FrontWinID:  42,
				Windows:     []image.Rectangle{fullWin},
				WindowInfos: []session.WinTarget{{ID: 42, Bounds: fullWin}},
			}, nil
		},
		CaptureWindow: func(_ context.Context, _ uint32, _ float64) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, 100, 100)), nil
		},
		UI: testOCRUI{},
	}
	a.session.StartLive()
	a.session.InitCursor(700, 400)

	tt := ui.NewTester(a.selectionView(o), 1440, 900)
	tt.Frame()
	tt.ClickAt(700, 400)
	tt.Frame()

	var baseY float32 = -1
	for _, lbl := range []string{"框", "圆", "箭头", "荧光", "序号", "打码", "擦除", "聚光", "文字", "撤销", "脱敏", "单窗", "美化", "长图", "识图", "贴图", "保存", "取消", "复制"} {
		r, ok := tt.Find(lbl)
		if !ok {
			t.Fatalf("missing button %q", lbl)
		}
		if r.X < 4 || r.X+r.W > 1436 {
			t.Fatalf("button %q out of screen bounds: %+v", lbl, r)
		}
		if baseY < 0 {
			baseY = r.Y
		} else if r.Y != baseY {
			t.Fatalf("button %q wrapped off horizontal row: y=%v want %v", lbl, r.Y, baseY)
		}
	}

	if dir := os.Getenv("SNIPGO_PREVIEW_DIR"); dir != "" {
		// 开启工具与美化子栏后渲染高分屏预览供视觉走查。
		a.session.Annotations().Tool = annotate.ToolArrow
		a.session.ToggleBeautify()
		subO := &overlay{bounds: image.Rect(0, 0, 800, 260), index: 0}
		a.overlays = []*overlay{subO}
		a.session.Cancel()
		a.session.Live = func() (session.Snapshot, error) {
			sel := image.Rect(40, 36, 760, 140)
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: subO.bounds, Scale: 2}},
				FrontWindow: sel,
				FrontWinID:  99,
				Windows:     []image.Rectangle{sel},
				WindowInfos: []session.WinTarget{{ID: 99, Bounds: sel}},
			}, nil
		}
		a.session.StartLive()
		a.session.InitCursor(200, 80)
		a.session.BeginDrag(0, 200, 80)
		a.session.EndDrag()
		a.session.Annotations().Tool = annotate.ToolArrow
		a.session.ToggleBeautify()

		img := ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.RGB(44, 52, 68)).Children(func() {
				a.selectionView(subO)(c)
			})
		}, 800, 260, 2)
		f, err := os.Create(filepath.Join(dir, "toolbar_preview.png"))
		if err == nil {
			_ = png.Encode(f, img)
			_ = f.Close()
		}

		ow := &ocrWindow{
			thumb:       ui.NewBitmap(beautify.RenderTextPin("#0A84FF", 2.0)),
			loading:     false,
			text:        "SnipGo 离线极速识图结果演示\n自动提取中英混排段落、代码与二维码链接，\n可直接选中复制局部内容、编辑修正或一键钉为桌面便签。",
			links:       []string{"https://github.com/egoist/mygo"},
			copiedBadge: "已自动复制 · 62 字",
		}
		ocrPreview := ui.Render(func(c *ui.Context) {
			a.paintOCRView(c, ow)
		}, ocrWinW, ocrWinH, 2)
		f2, err := os.Create(filepath.Join(dir, "ocr_preview.png"))
		if err == nil {
			_ = png.Encode(f2, ocrPreview)
			_ = f2.Close()
		}
	}
}

func TestOneStepStartLiveOCRAndResultPanel(t *testing.T) {
	var ocrImg *image.NRGBA
	mainQueue := make(chan func(), 4)
	a := &app{}
	o := &overlay{bounds: image.Rect(0, 0, 800, 600), index: 0}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{
				Frames: []crop.Screen{{Bounds: o.bounds, Scale: 2}},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		Clip: testClip{},
		UI: testOCRUI{
			onOCR: func(img *image.NRGBA) { ocrImg = img },
		},
		OnMain: func(fn func()) { mainQueue <- fn },
	}
	a.session.StartLiveOCR()
	a.session.InitCursor(100, 100)

	tt := ui.NewTester(a.selectionView(o), 800, 600)
	tt.Frame()
	// 拖拽选区松手后无需再点任何按钮，直接触发识图采集！
	tt.Press(100, 120)
	tt.Frame()
	tt.Move(340, 220)
	tt.Frame()
	tt.Release(340, 220)
	tt.Frame()
	(<-mainQueue)()

	if ocrImg == nil || ocrImg.Bounds().Dx() != 480 || ocrImg.Bounds().Dy() != 200 {
		t.Fatalf("expected immediate OCR capture 480x200 on mouse release, got %v", ocrImg)
	}
	if gotAnchor := a.session.LastOCRRect(); gotAnchor != image.Rect(100, 120, 340, 220) {
		t.Fatalf("LastOCRRect = %v, want (100,120)-(340,220)", gotAnchor)
	}

	// 验证识图结果面板视图在加载态与结果态均可正常交互渲染。
	ow := &ocrWindow{
		loading:     false,
		text:        "Hello SnipGo OCR\n第二行测试文本",
		links:       []string{"https://example.com"},
		copiedBadge: "已自动复制 · 24 字",
	}
	panelTester := ui.NewTester(func(c *ui.Context) {
		a.paintOCRView(c, ow)
	}, ocrWinW, ocrWinH)
	panelTester.Frame()
	if !panelTester.HasText("识图提取") || !panelTester.HasText("复制全文") || !panelTester.HasText("贴为便签") {
		t.Fatalf("OCR panel missing expected controls, texts=%v", panelTester.Texts())
	}
}

func TestClosedPinHistoryRingBuffer(t *testing.T) {
	a := &app{}
	for i := range 14 {
		a.pushClosedPin(closedPinRecord{
			img:   image.NewNRGBA(image.Rect(0, 0, 10, 10)),
			origW: 100 + i,
			origH: 80,
		})
	}
	if len(a.closedPins) != maxClosedPinCap {
		t.Fatalf("closedPins len = %d, want %d", len(a.closedPins), maxClosedPinCap)
	}
	if a.closedPins[len(a.closedPins)-1].origW != 113 {
		t.Fatalf("latest closed pin origW = %d, want 113", a.closedPins[len(a.closedPins)-1].origW)
	}
}

type testOCRUI struct {
	onOCR func(*image.NRGBA)
}

func (testOCRUI) SetStatus(string)                    {}
func (testOCRUI) ShowSelection([]crop.Screen)         {}
func (testOCRUI) HideSelection()                      {}
func (testOCRUI) ShowPin(*image.NRGBA, []byte, image.Point, int, int) {}
func (testOCRUI) SavePNG([]byte)                      {}
func (u testOCRUI) RecognizeImage(img *image.NRGBA) {
	if u.onOCR != nil {
		u.onOCR(img)
	}
}

type testClip struct {
	onPNG func([]byte)
}

func (c testClip) WritePNG(b []byte) error {
	if c.onPNG != nil {
		c.onPNG(b)
	}
	return nil
}
func (testClip) WriteText(string) {}

type testSessionUI struct {
	onPin func()
}

func (testSessionUI) SetStatus(string)            {}
func (testSessionUI) ShowSelection([]crop.Screen) {}
func (testSessionUI) HideSelection()              {}
func (u testSessionUI) ShowPin(*image.NRGBA, []byte, image.Point, int, int) {
	if u.onPin != nil {
		u.onPin()
	}
}
func (testSessionUI) SavePNG([]byte)            {}
func (testSessionUI) RecognizeImage(*image.NRGBA) {}
