package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/scrollcap"
	"snipgo/internal/session"
	"snipgo/internal/store"
	"snipgo/internal/textrender"
)

func TestVisualVerifyEveryFeature(t *testing.T) {
	dir := os.Getenv("SNIPGO_PREVIEW_DIR")
	if dir == "" {
		return
	}

	// 构造一张逼真的应用界面底图（760x460 DIP -> 1520x920 @2x Retina），含深色卡片、按钮和敏感文字（供马赛克与标尺验证）。
	const dipW, dipH = 760, 460
	bgPx := image.NewNRGBA(image.Rect(0, 0, dipW*2, dipH*2))
	draw.Draw(bgPx, bgPx.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 34, B: 44, A: 255}), image.Point{}, draw.Src)
	// 内部白色/浅蓝卡片供像素测量尺吸附验证：(80, 60)-(640, 380) DIP => (160,120)-(1280,760) px
	draw.Draw(bgPx, image.Rect(160, 120, 1280, 760), image.NewUniform(color.NRGBA{R: 42, G: 48, B: 62, A: 255}), image.Point{}, draw.Src)
	// 按钮块：(460, 80)-(590, 118) DIP => (920,160)-(1180,236) px
	draw.Draw(bgPx, image.Rect(920, 160, 1180, 236), image.NewUniform(color.NRGBA{R: 10, G: 132, B: 255, A: 255}), image.Point{}, draw.Src)

	if secretTxt := textrender.Render("Token: sk-live-9876543210", textrender.Options{
		FontSize:  28,
		Weight:    0.3,
		Monospace: true,
		Color:    color.NRGBA{R: 245, G: 246, B: 250, A: 255},
	}); secretTxt != nil {
		sb := secretTxt.Bounds()
		draw.Draw(bgPx, sb.Add(image.Pt(200, 560)), secretTxt, sb.Min, draw.Over)
	}

	// 准备全部 7 种标注工具的测试实例
	sel := image.Rect(60, 40, 660, 360)
	a := &app{}
	o := &overlay{
		bitmap: ui.NewBitmap(bgPx),
		bounds: image.Rect(0, 0, dipW, dipH),
		index:  0,
	}
	a.overlays = []*overlay{o}
	a.session = &session.Session{
		Capture: func(context.Context) (session.Snapshot, error) { return session.Snapshot{}, nil },
		UI:      testOCRUI{},
	}
	// 用冻结帧直接喂入 session，确保放大镜、测量尺、导出全部走真实像素。
	a.session.Launch = func(gen int) {
		a.session.Deliver(gen, session.Snapshot{
			Frames:      []crop.Screen{{Bounds: o.bounds, Scale: 2, Pixels: bgPx}},
			FrontWindow: sel,
			FrontWinID:  7,
			Windows:     []image.Rectangle{sel},
			WindowInfos: []session.WinTarget{{ID: 7, Bounds: sel}},
		}, nil)
	}
	a.session.StartFrozen()
	a.session.InitCursor(200, 150)
	a.session.BeginDrag(0, 200, 150)
	a.session.EndDrag()

	ac := a.session.Annotations()
	// 1. 矩形框
	ac.Tool = annotate.ToolRect
	ac.Color = annotate.Palette[0]
	ac.Stroke = 3.0
	ac.BeginPointer(90, 75)
	ac.EndPointer(230, 145)

	// 2. 扁椭圆（200x64，检验绝不是胶囊圆角矩形！）
	ac.Tool = annotate.ToolEllipse
	ac.Color = annotate.Palette[2]
	ac.Stroke = 3.0
	ac.BeginPointer(255, 75)
	ac.EndPointer(435, 140)

	// 3. 多方向、多粗细箭头（斜向长箭头 + 水平中箭头 + 粗箭头）
	ac.Tool = annotate.ToolArrow
	ac.Color = annotate.Palette[0]
	ac.Stroke = 3.0
	ac.BeginPointer(110, 225)
	ac.EndPointer(250, 165)

	ac.Color = annotate.Palette[3]
	ac.Stroke = 6.0
	ac.BeginPointer(275, 225)
	ac.EndPointer(425, 185)

	// 4. 多折线段荧光笔（检验拐点绝不变深结块！）
	ac.Tool = annotate.ToolPen
	ac.Color = annotate.Palette[1]
	ac.Stroke = 3.5
	ac.BeginPointer(460, 210)
	ac.MovePointer(520, 195)
	ac.MovePointer(580, 215)
	ac.EndPointer(630, 200)

	// 5. 步骤序号 ①（红底白字）、②（蓝底白字）、③（白底深色字！）
	ac.Tool = annotate.ToolStep
	ac.Color = annotate.Palette[0]
	ac.BeginPointer(475, 100)
	ac.Color = annotate.Palette[3]
	ac.BeginPointer(525, 100)
	ac.Color = annotate.Palette[4] // 白色底！
	ac.BeginPointer(575, 100)

	// 6. 马赛克打码（盖住 Token 后半段）
	ac.Tool = annotate.ToolMosaic
	ac.BeginPointer(215, 275)
	ac.EndPointer(390, 315)

	// 7. 文字批注（中英混排）
	ac.Color = annotate.Palette[1]
	ac.Stroke = 3.0
	ac.AddText(415, 280, "敏感 Token 已打码 ✓")

	ac.Tool = annotate.ToolArrow
	ac.Color = annotate.Palette[0]
	ac.Stroke = 3.0

	// 图 1：实时 UI 预览（含全部 7 种标注 + 工具栏 + 子栏）
	liveImg := ui.Render(a.selectionView(o), dipW, dipH, 2)
	savePNGFile(t, filepath.Join(dir, "verify_1_annotations_live.png"), liveImg)

	// 图 2：2x Retina 真实裁剪 + 栅格化导出结果
	cropped, err := crop.Crop([]crop.Screen{{Bounds: o.bounds, Scale: 2, Pixels: bgPx}}, sel)
	if err != nil {
		t.Fatalf("crop failed: %v", err)
	}
	annotate.Render(cropped, sel, ac.Items())
	savePNGFile(t, filepath.Join(dir, "verify_2_annotations_exported.png"), cropped)

	// 图 3：圆角阴影渐变美化卡片 + #HEX 色卡贴图 + 代码便签贴图拼盘
	card := beautify.RenderCard(cropped, 2.0, beautify.PresetSunset)
	hexPin := beautify.RenderTextPin("#0A84FF", 2.0)
	notePin := beautify.RenderTextPin("func ExportArrow() {\n    // 流线型实心箭头已就绪\n}", 2.0)
	cb := card.Bounds()
	hb := hexPin.Bounds()
	nb := notePin.Bounds()
	sheetW := cb.Dx() + max(hb.Dx(), nb.Dx()) + 80
	sheetH := max(cb.Dy(), hb.Dy()+nb.Dy()+40) + 40
	sheet := image.NewNRGBA(image.Rect(0, 0, sheetW, sheetH))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.NRGBA{R: 20, G: 22, B: 28, A: 255}), image.Point{}, draw.Src)
	draw.Draw(sheet, cb.Add(image.Pt(20, 20)), card, cb.Min, draw.Over)
	draw.Draw(sheet, hb.Add(image.Pt(cb.Dx()+50, 20)), hexPin, hb.Min, draw.Over)
	draw.Draw(sheet, nb.Add(image.Pt(cb.Dx()+50, hb.Dy()+50)), notePin, nb.Min, draw.Over)
	savePNGFile(t, filepath.Join(dir, "verify_3_beautify_and_cards.png"), sheet)

	// 图 4：像素测量尺（吸附蓝色按钮内部 260x76 px）+ 贴图悬停栏 + 迷你折叠胶囊
	a.session.Cancel()
	a.session.StartFrozen()
	a.session.Hover(520, 98) // 光标位于蓝色按钮 (460,80)-(590,118) 内部
	a.session.ToggleMeasure()
	rulerPreview := ui.Render(func(c *ui.Context) {
		a.selectionView(o)(c)
		// 右下角同时渲染折叠胶囊贴图预览
		ui.Box(c).Absolute().Right(20).Bottom(20).ZIndex(20).Children(func() {
			a.paintCollapsedPill(c, &pinWin{origW: 320, origH: 200})
		})
		// 左下角渲染带悬停快捷栏的贴图预览
		ui.Box(c).Absolute().Left(20).Bottom(20).Size(240, 120).ZIndex(20).
			Background(ui.RGB(38, 44, 58)).Border(1, ui.RGBA(255, 255, 255, 0.2)).
			Children(func() {
				ui.Text(c, "悬浮贴图悬停栏预览").Absolute().Left(14).Bottom(14).FontSize(12).TextColor(ui.RGB(220, 224, 232))
				a.paintPinHoverBar(c, &pinWin{origW: 240, origH: 120, curW: 240, curH: 120, png: []byte{1}})
			})
	}, dipW, dipH, 2)
	savePNGFile(t, filepath.Join(dir, "verify_4_ruler_and_pins.png"), rulerPreview)

	// 图 5：聚光灯高亮 + 无痕智能擦除 + 标注图元点选控制柄（实时预览 vs 2x 真实导出并排核对）
	a.session.Cancel()
	a.session.StartFrozen()
	a.session.InitCursor(200, 150)
	a.session.BeginDrag(0, 200, 150)
	a.session.EndDrag()
	ac = a.session.Annotations()
	ac.Reset()
	// 用无痕擦除抹掉 Token 后半段 sk-live-9876543210
	ac.Tool = annotate.ToolErase
	ac.BeginPointer(205, 275)
	ac.EndPointer(400, 315)
	// 用聚光灯聚焦右上角蓝色按钮
	ac.Tool = annotate.ToolSpotlight
	ac.BeginPointer(450, 72)
	ac.EndPointer(600, 126)
	// 从压暗区域画一支红箭头指向聚光灯内部按钮，并保持该箭头为「已点选」状态展示首尾控制柄与删除按钮
	ac.Tool = annotate.ToolArrow
	ac.Color = annotate.Palette[0]
	ac.Stroke = 3.5
	ac.BeginPointer(280, 180)
	ac.EndPointer(442, 108)
	ac.Tool = annotate.ToolNone
	ac.BeginEdit(360, 144)
	ac.EndEdit()

	spotLive := ui.Render(a.selectionView(o), dipW, dipH, 2)
	spotCrop, err := crop.Crop([]crop.Screen{{Bounds: o.bounds, Scale: 2, Pixels: bgPx}}, sel)
	if err != nil {
		t.Fatalf("spot crop: %v", err)
	}
	annotate.Render(spotCrop, sel, ac.Items())
	spotSheet := image.NewNRGBA(image.Rect(0, 0, spotLive.Bounds().Dx()+spotCrop.Bounds().Dx()+40, max(spotLive.Bounds().Dy(), spotCrop.Bounds().Dy())+20))
	draw.Draw(spotSheet, spotSheet.Bounds(), image.NewUniform(color.NRGBA{R: 18, G: 20, B: 26, A: 255}), image.Point{}, draw.Src)
	draw.Draw(spotSheet, spotLive.Bounds().Add(image.Pt(10, 10)), spotLive, image.Point{}, draw.Over)
	draw.Draw(spotSheet, spotCrop.Bounds().Add(image.Pt(spotLive.Bounds().Dx()+30, 140)), spotCrop, image.Point{}, draw.Over)
	savePNGFile(t, filepath.Join(dir, "verify_5_spotlight_erase_select.png"), spotSheet)

	// 图 6：滚动长截图实时控制面板 + 贴图原地二次标注模式 + 拖出文件手柄预览
	st := scrollcap.New()
	st.Push(spotCrop)
	scrollSess := &scrollCaptureSession{
		sel:     sel,
		stitch:  st,
		preview: ui.NewBitmap(spotCrop),
		pxW:     1200,
		pxH:     3480,
		frames:  6,
	}
	pEdit := &pinWin{
		baseImg: spotCrop,
		baseBmp: ui.NewBitmap(spotCrop),
		img:     spotCrop,
		bmp:     ui.NewBitmap(spotCrop),
		png:     []byte{1, 2, 3},
		annot:   annotate.NewCanvas(),
		origW:   420,
		origH:   260,
		curW:    420,
		curH:    260,
		zoom:    1.0,
		opacity: 100,
		editMode: true,
	}
	pEdit.annot.Tool = annotate.ToolArrow
	pEdit.annot.Color = annotate.Palette[1]
	pEdit.annot.Stroke = 3.0
	pEdit.annot.BeginPointer(40, 200)
	pEdit.annot.EndPointer(180, 90)
	pEdit.annot.Tool = annotate.ToolStep
	pEdit.annot.Color = annotate.Palette[2]
	pEdit.annot.BeginPointer(220, 80)
	pEdit.annot.Tool = annotate.ToolArrow

	scrollAndPinImg := ui.Render(func(c *ui.Context) {
		ui.Row(c).Fill().Padding(16, 16).Gap(20).Background(ui.RGB(20, 22, 28)).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(scrollCtrlW, scrollCtrlH).Children(func() {
				a.paintScrollCtrlView(c, scrollSess)
			})
			ui.Column(c).Gap(12).Children(func() {
				ui.Box(c).Size(420, 260).Children(func() {
					a.pinView(c, pEdit)
				})
				ui.Box(c).Size(420, 56).Radius(8).Background(ui.RGB(34, 38, 48)).Children(func() {
					ui.Text(c, "悬停栏（含左一 PNG 拖出手柄 + 左二原地编辑）：").Absolute().Left(12).Top(18).FontSize(11.5).TextColor(ui.RGB(210, 216, 226))
					a.paintPinHoverBar(c, &pinWin{origW: 420, origH: 56, curW: 420, curH: 56, png: []byte{1}})
				})
			})
		})
	}, 720, 380, 2)
	savePNGFile(t, filepath.Join(dir, "verify_6_scroll_and_pin_edit.png"), scrollAndPinImg)

	// 图 7：最近截图历史抽屉 + 偏好设置面板 + 3 秒延时倒计时胶囊
	tmpHistDir := t.TempDir()
	hStore, err := store.Open(tmpHistDir, 20)
	if err != nil {
		t.Fatalf("open history store: %v", err)
	}
	a.history = hStore
	a.picturesDir = tmpHistDir
	a.cfg = config.Default(tmpHistDir)

	fBytes, _ := os.ReadFile(filepath.Join(dir, "verify_2_annotations_exported.png"))
	_, _ = hStore.Add(fBytes, 1200, 640)
	_, _ = hStore.Add(fBytes, 960, 540)
	hw := &historyWindow{thumbs: map[string]*ui.Bitmap{}}
	a.histWin = hw
	a.reloadHistoryWindow()
	hw.statusBadge = "按住缩略图可直接拖出 PNG"

	sw := &settingsWindow{
		draft: a.cfg,
		saved: "✓ 设置已生效",
	}
	cd := &countdownSession{remaining: 3}

	histAndSetImg := ui.Render(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(14, 14).Gap(12).Background(ui.RGB(18, 20, 26)).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(246, 42).Children(func() {
				a.paintCountdownView(c, cd)
			})
			ui.Row(c).Gap(16).Children(func() {
				ui.Box(c).Size(historyWinW, 380).Children(func() {
					a.paintHistoryView(c, hw)
				})
				ui.Box(c).Size(settingsWinW, 380).Children(func() {
					a.paintSettingsView(c, sw)
				})
			})
		})
	}, 1068, 462, 2)
	savePNGFile(t, filepath.Join(dir, "verify_7_history_and_settings.png"), histAndSetImg)
	a.histWin = nil
}

func savePNGFile(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}
