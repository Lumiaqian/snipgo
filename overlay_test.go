package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/crop"
	"snipgo/internal/session"
	"snipgo/internal/vision"
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
func (testPinUI) SavePNG([]byte)              {}
func (testPinUI) RecognizeImage(*image.NRGBA) {}

func TestToolbarAnnotationTemplateAndOCRWorkflow(t *testing.T) {
	var ocrImg *image.NRGBA
	var tplImg *image.NRGBA
	var tplRect image.Rectangle
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
		OnTemplate: func(img *image.NRGBA, r image.Rectangle) {
			tplImg = img
			tplRect = r
		},
		OnMain: func(fn func()) { mainQueue <- fn },
	}
	a.session.StartLive()
	a.session.InitCursor(300, 200)

	tt := ui.NewTester(a.selectionView(o), 900, 650)
	tt.Frame()

	// 1. 单击锁定窗口，浮动工具栏展示标注、模板与识字按钮。
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

	// 3. 点击「识图」触发无污染原图采集与 OCR 回调（不混入标注或边框，确保识别率）。
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

	// 4. 重新启动选区并绘制红色矩形标注，点击「模板」验证标注已烘培进传入套模板工作台的图像。
	a.session.StartLive()
	a.session.InitCursor(300, 200)
	tt.Frame()
	tt.ClickAt(300, 200)
	tt.Frame()

	rectBtn, ok := tt.Find("框")
	if !ok {
		t.Fatal("missing 框 button")
	}
	tt.ClickAt(rectBtn.X+rectBtn.W/2, rectBtn.Y+rectBtn.H/2)
	tt.Frame()
	a.session.Annotations().Color = annotate.Palette[0]
	a.session.Annotations().Stroke = 3.0
	tt.Press(180, 140)
	tt.Frame()
	tt.Move(340, 240)
	tt.Frame()
	tt.Release(340, 240)
	tt.Frame()

	tplBtn, ok := tt.Find("模板")
	if !ok {
		t.Fatal("missing 模板 button")
	}
	tt.ClickAt(tplBtn.X+tplBtn.W/2, tplBtn.Y+tplBtn.H/2)
	tt.Frame()
	(<-mainQueue)()
	if tplImg == nil || tplRect != winRect {
		t.Fatalf("expected OnTemplate delivered with winRect %v, got img=%v rect=%v", winRect, tplImg != nil, tplRect)
	}
	// 验证红色矩形边框已被渲染到 tplImg 中：局部坐标 (180-120)*2=120, y 从 (140-80)*2=120 到 (240-80)*2=320。
	foundRed := false
	for x := 115; x <= 440; x++ {
		px := tplImg.NRGBAAt(x, 120)
		if px.R > 200 && px.A > 200 {
			foundRed = true
			break
		}
	}
	if !foundRed {
		t.Fatalf("expected baked red rect annotation inside template input image")
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
	for _, lbl := range []string{"框", "圆", "箭头", "荧光", "序号", "打码", "擦除", "聚光", "文字", "放大", "撤销", "脱敏", "抠图", "单窗", "模板", "长图", "识图", "贴图", "保存", "取消", "复制"} {
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
		// 开启放大镜与标注子栏后渲染 Light + Dark 双外观 Tahoe Liquid Glass 高分屏工具栏对比。
		subO := &overlay{bounds: image.Rect(0, 0, 800, 230), index: 0}
		a.overlays = []*overlay{subO}
		a.session.Cancel()
		a.session.Live = func() (session.Snapshot, error) {
			sel := image.Rect(40, 24, 760, 114)
			return session.Snapshot{
				Frames:      []crop.Screen{{Bounds: subO.bounds, Scale: 2}},
				FrontWindow: sel,
				FrontWinID:  99,
				Windows:     []image.Rectangle{sel},
				WindowInfos: []session.WinTarget{{ID: 99, Bounds: sel}},
			}, nil
		}
		a.session.StartLive()
		a.session.InitCursor(200, 72)
		a.session.BeginDrag(0, 200, 72)
		a.session.EndDrag()
		a.session.Annotations().Tool = annotate.ToolLoupe

		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Column(c).Fill().Children(func() {
				ui.Box(c).Width(800).Height(230).
					Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, false) }).
					Children(func() {
						ui.Themed(c.Key("tb-light"), ui.LightTheme()).Fill().Background(ui.Transparent).Children(func() {
							a.selectionView(subO)(c)
						})
					})
				ui.Box(c).Width(800).Height(230).
					Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
					Children(func() {
						ui.Themed(c.Key("tb-dark"), ui.DarkTheme()).Fill().Background(ui.Transparent).Children(func() {
							a.selectionView(subO)(c)
						})
					})
			})
		}, 800, 460, 2)
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
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Padding(16, 16).
				Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
				Children(func() {
					ui.Themed(c, ui.DarkTheme()).Fill().Background(ui.Transparent).Children(func() {
						a.paintOCRView(c, ow)
					})
				})
		}, ocrWinW+32, ocrWinH+32, 2)
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

func TestOCRTableViewAndSourceToggle(t *testing.T) {
	a := &app{}
	tbl := vision.Table{
		Cols: 2,
		Rows: [][]string{
			{"4个已更改文件 +133 -89", "变更"},
			{".github/workflows", "+9 -17"},
			{"internal/vision", "+120 -68"},
			{"README.md", "+4 -4"},
		},
	}
	var copied string
	ow := &ocrWindow{
		loading:     false,
		plainText:   "4个已更改文件 +133 -89 变更\n.github/workflows +9 -17\ninternal/vision +120 -68\nREADME.md +4 -4",
		text:        tbl.Markdown(),
		table:       tbl,
		hasTable:    true,
		mode:        ocrModeMarkdown,
		copiedBadge: "已复制 Markdown",
		onCopyText: func(s string) {
			copied = s
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintOCRView(c, ow)
	}, ocrWinW, ocrWinH)
	tt.Frame()

	for _, want := range []string{"纯文本", "Markdown", "表格 TSV", "表格", "源码", "internal/vision", "+120 -68", "复制 Markdown"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in OCR table view, texts=%v", want, tt.Texts())
		}
	}

	// 点击表格单元格可单独复制对应内容并刷新提示徽标。
	cellRect, ok := tt.Find("+120 -68")
	if !ok {
		t.Fatalf("cannot find table cell +120 -68")
	}
	tt.ClickAt(cellRect.X+cellRect.W/2, cellRect.Y+cellRect.H/2)
	tt.Frame()
	if copied != "+120 -68" || ow.copiedBadge != "已复制单元格" {
		t.Fatalf("cell click copied=%q badge=%q, want +120 -68 / 已复制单元格", copied, ow.copiedBadge)
	}

	// 切换到「源码」视图可编辑原始 Markdown 文本。
	srcRect, ok := tt.Find("源码")
	if !ok {
		t.Fatalf("cannot find 源码 toggle")
	}
	tt.ClickAt(srcRect.X+srcRect.W/2, srcRect.Y+srcRect.H/2)
	tt.Frame()
	if !ow.showRaw {
		t.Fatal("expected showRaw = true after clicking 源码")
	}
	if !tt.HasText("编辑 Markdown 源码") {
		t.Fatalf("expected raw editor footer hint, texts=%v", tt.Texts())
	}
}

func TestOCREditPreserveAndTableResync(t *testing.T) {
	a := &app{}
	tbl := vision.Table{
		Cols: 2,
		Rows: [][]string{
			{"模块", "状态"},
			{"OCR", "正常"},
		},
	}
	ow := &ocrWindow{
		loading:   false,
		plainText: "原始纯文本",
		text:      "用户编辑后的纯文本",
		table:     tbl,
		hasTable:  true,
		mode:      ocrModePlain,
	}

	// 切换到 Markdown 后再切回纯文本，应保留用户在纯文本模式下的手动编辑。
	ow.switchMode(ocrModeMarkdown)
	if ow.mode != ocrModeMarkdown || !strings.Contains(ow.text, "| 模块 | 状态 |") {
		t.Fatalf("expected Markdown text after switch, got mode=%v text=%q", ow.mode, ow.text)
	}
	// 在 Markdown 源码里手动修改单元格后切去 TSV 再切回，保留编辑并可重新解析同步表格视图。
	ow.text = "| 模块 | 状态 |\n| --- | --- |\n| OCR | 已升级 |"
	ow.switchMode(ocrModePlain)
	if ow.text != "用户编辑后的纯文本" {
		t.Fatalf("expected plainText edit preserved, got %q", ow.text)
	}
	ow.switchMode(ocrModeMarkdown)
	if !strings.Contains(ow.text, "已升级") {
		t.Fatalf("expected Markdown edit preserved, got %q", ow.text)
	}
	parsed, ok := parseEditedTable(ocrModeMarkdown, ow.text)
	if !ok || len(parsed.Rows) != 2 || parsed.Rows[1][1] != "已升级" {
		t.Fatalf("parseEditedTable markdown = (%+v, %v), want updated row", parsed, ok)
	}
	ow.table = parsed
	tt := ui.NewTester(func(c *ui.Context) {
		a.paintOCRView(c, ow)
	}, ocrWinW, ocrWinH)
	tt.Frame()
	if !tt.HasText("已升级") {
		t.Fatalf("expected updated table cell in view, texts=%v", tt.Texts())
	}

	tsvParsed, ok := parseEditedTable(ocrModeTSV, "A\tB\n1\t2")
	if !ok || tsvParsed.Cols != 2 || tsvParsed.Rows[1][0] != "1" {
		t.Fatalf("parseEditedTable TSV = (%+v, %v)", tsvParsed, ok)
	}
}

func TestTemplateCmdCTypingDoesNotHijack(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	a := &app{}
	tw := &templateWindow{
		srcImg:     src,
		srcScale:   2,
		previewSrc: src,
		opts:       beautify.DefaultTemplateOptions(),
	}
	tw.opts.Title = "输入标题测试"
	tw.refreshPreview()

	tt := ui.NewTester(func(c *ui.Context) {
		a.paintTemplateView(c, tw)
	}, templateWinW, templateWinH)
	tt.Frame()

	// 先点击标题输入框让输入框获得焦点，再按 Cmd+C 不应触发「复制成图」。
	titleRect, ok := tt.Find("卡片标题")
	if !ok {
		t.Fatalf("cannot find 卡片标题 input, texts=%v", tt.Texts())
	}
	tt.ClickAt(titleRect.X+titleRect.W/2, titleRect.Y+titleRect.H/2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if tw.statusBadge != "" {
		t.Fatalf("expected Cmd+C while typing not to trigger copyTemplateResult, got badge=%q", tw.statusBadge)
	}
}

func TestClosedPinHistoryRingBuffer(t *testing.T) {
	a := &app{}
	for i := range 14 {
		a.pushClosedPin(closedPinRecord{
			png:   testHistoryPNG(10, 10),
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

func TestLoupeWYSIWYGPreviewAndCircularClip(t *testing.T) {
	const dipW, dipH = 400, 360
	bg := image.NewNRGBA(image.Rect(0, 0, dipW*2, dipH*2))
	draw.Draw(bg, bg.Bounds(), image.NewUniform(color.NRGBA{R: 24, G: 30, B: 44, A: 255}), image.Point{}, draw.Src)
	// 在逻辑坐标 [55..105]×[55..105] 绘制大块鲜绿目标（3× 放大后覆盖 ±75 DIP，若未圆形裁切则会溢出到方形外角）。
	draw.Draw(bg, image.Rect(110, 110, 210, 210), image.NewUniform(color.NRGBA{R: 48, G: 215, B: 96, A: 255}), image.Point{}, draw.Src)

	bounds := image.Rect(0, 0, dipW, dipH)
	o := &overlay{bitmap: ui.NewBitmap(bg), pixels: bg, bounds: bounds, index: 0}
	a := &app{}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		Capture: func(context.Context) (session.Snapshot, error) { return session.Snapshot{}, nil },
		UI:      testOCRUI{},
	}
	a.session.Launch = func(gen int) {
		a.session.Deliver(gen, session.Snapshot{Frames: []crop.Screen{{Bounds: bounds, Scale: 2, Pixels: bg}}}, nil)
	}
	a.session.StartFrozen()
	a.session.BeginDrag(0, 20, 20)
	a.session.MoveDrag(0, 360, 220)
	a.session.EndDrag()

	ac := a.session.Annotations()
	ac.Tool = annotate.ToolLoupe
	ac.Stroke = 3.0 // rad = 52
	ac.SetLoupeZoom(3.0)
	ac.BeginPointer(80, 80)
	ac.EndPointer(220, 120)

	rendered := ui.Render(a.selectionView(o), dipW, dipH, 1)
	centerPx := rendered.RGBAAt(220, 120)
	if centerPx.G < 180 || centerPx.R > 80 {
		t.Fatalf("expected WYSIWYG magnified green at loupe center (220,120), got %+v", centerPx)
	}
	// dist((±44,-44)) ≈ 62.2 > rad+ring+accent，必须保留深海蓝底色而不是方形绿色外角。
	for _, corner := range []image.Point{{220 + 44, 120 - 44}, {220 - 44, 120 - 44}, {220 + 44, 120 + 44}} {
		px := rendered.RGBAAt(corner.X, corner.Y)
		if px.R > 45 || px.G > 55 || px.B > 75 {
			t.Fatalf("expected circular clip outside loupe lens at %v, got %+v", corner, px)
		}
	}
}

func TestErasePreviewSamplesDarkBackgroundAndLoupeClickMovesExisting(t *testing.T) {
	const dipW, dipH = 300, 200
	bg := image.NewNRGBA(image.Rect(0, 0, dipW*2, dipH*2))
	draw.Draw(bg, bg.Bounds(), image.NewUniform(color.NRGBA{R: 18, G: 22, B: 30, A: 255}), image.Point{}, draw.Src)

	bounds := image.Rect(0, 0, dipW, dipH)
	o := &overlay{bitmap: ui.NewBitmap(bg), pixels: bg, bounds: bounds, index: 0}
	a := &app{}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		Capture: func(context.Context) (session.Snapshot, error) { return session.Snapshot{}, nil },
		UI:      testOCRUI{},
	}
	a.session.Launch = func(gen int) {
		a.session.Deliver(gen, session.Snapshot{Frames: []crop.Screen{{Bounds: bounds, Scale: 2, Pixels: bg}}}, nil)
	}
	a.session.StartFrozen()
	a.session.BeginDrag(0, 10, 10)
	a.session.MoveDrag(0, 290, 190)
	a.session.EndDrag()

	ac := a.session.Annotations()
	ac.Tool = annotate.ToolErase
	ac.BeginPointer(40, 40)
	ac.EndPointer(120, 80)

	rendered := ui.Render(a.selectionView(o), dipW, dipH, 1)
	eraseMid := rendered.RGBAAt(80, 60)
	if eraseMid.R > 45 || eraseMid.G > 50 || eraseMid.B > 60 {
		t.Fatalf("expected ToolErase preview to match dark background, got %+v", eraseMid)
	}

	// 验证激活 ToolLoupe 时点击已有放大镜内部会直接拖动，而非叠出第二个放大镜。
	ac.Reset()
	ac.Tool = annotate.ToolLoupe
	a.session.BeginDrag(0, 100, 100)
	a.session.MoveDrag(0, 101, 101)
	a.session.EndDrag()
	if got := len(ac.Items()); got != 1 {
		t.Fatalf("expected 1 loupe after initial placement, got %d", got)
	}

	a.session.BeginDrag(0, 110, 105)
	a.session.MoveDrag(0, 160, 125)
	a.session.EndDrag()
	items := ac.Items()
	if len(items) != 1 {
		t.Fatalf("expected dragging inside existing loupe to move it instead of stacking, got %d items", len(items))
	}
	if items[0].P0 != (annotate.PointF{X: 150, Y: 120}) || items[0].P1 != (annotate.PointF{X: 150, Y: 120}) {
		t.Fatalf("expected loupe translated by (+50,+20) to (150,120), got %+v", items[0])
	}
}

type testOCRUI struct {
	onOCR func(*image.NRGBA)
}

func (testOCRUI) SetStatus(string)                                    {}
func (testOCRUI) ShowSelection([]crop.Screen)                         {}
func (testOCRUI) HideSelection()                                      {}
func (testOCRUI) ShowPin(*image.NRGBA, []byte, image.Point, int, int) {}
func (testOCRUI) SavePNG([]byte)                                      {}
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
func (testSessionUI) SavePNG([]byte)              {}
func (testSessionUI) RecognizeImage(*image.NRGBA) {}

func TestApplyRedactionResultDropsStaleGeneration(t *testing.T) {
	a := &app{}
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 100, 80), Scale: 2}}}, nil
		},
		OnMain: func(fn func()) { fn() },
		UI:     testSessionUI{},
	}
	a.session.StartLive()
	a.session.InitCursor(20, 20)
	a.session.BeginDrag(0, 10, 10)
	a.session.MoveDrag(0, 60, 50)
	a.session.EndDrag()
	if a.session.Phase() != session.PhaseAdjusting {
		t.Fatalf("phase %v", a.session.Phase())
	}
	oldGen := a.session.Gen()
	boxes := []annotate.RectF{{X: 12, Y: 12, W: 20, H: 10}}

	// 会话换代后到达的旧结果必须丢弃，且 redacting 复位。
	a.session.Cancel()
	a.session.StartLive()
	a.session.InitCursor(20, 20)
	a.session.BeginDrag(0, 10, 10)
	a.session.MoveDrag(0, 60, 50)
	a.session.EndDrag()
	a.redacting = true
	a.applyRedactionResult(oldGen, boxes)
	if a.redacting {
		t.Fatal("redacting should be cleared even when dropped")
	}
	if n := len(a.session.Annotations().Items()); n != 0 {
		t.Fatalf("stale result added %d items", n)
	}

	a.redacting = true
	a.applyRedactionResult(a.session.Gen(), boxes)
	if a.redacting {
		t.Fatal("redacting should be cleared")
	}
	if n := len(a.session.Annotations().Items()); n != 1 {
		t.Fatalf("matching result items = %d, want 1", n)
	}
}
