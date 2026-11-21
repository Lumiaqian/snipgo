package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/pinops"
	"snipgo/internal/scrollcap"
	"snipgo/internal/session"
	"snipgo/internal/store"
	"snipgo/internal/textrender"
	"snipgo/internal/vision"
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
		Color:     color.NRGBA{R: 245, G: 246, B: 250, A: 255},
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
	tallPreview := image.NewNRGBA(image.Rect(0, 0, spotCrop.Bounds().Dx(), spotCrop.Bounds().Dy()*3))
	draw.Draw(tallPreview, tallPreview.Bounds(), image.NewUniform(color.NRGBA{R: 22, G: 26, B: 36, A: 255}), image.Point{}, draw.Src)
	for i := range 3 {
		draw.Draw(tallPreview, spotCrop.Bounds().Sub(spotCrop.Bounds().Min).Add(image.Pt(0, i*spotCrop.Bounds().Dy())), spotCrop, spotCrop.Bounds().Min, draw.Src)
	}
	scrollSess := &scrollCaptureSession{
		sel:     sel,
		stitch:  st,
		preview: ui.NewBitmap(tallPreview),
		pxW:     tallPreview.Bounds().Dx(),
		pxH:     tallPreview.Bounds().Dy(),
		frames:  6,
	}
	pEdit := &pinWin{
		baseImg:  spotCrop,
		baseBmp:  ui.NewBitmap(spotCrop),
		img:      spotCrop,
		bmp:      ui.NewBitmap(spotCrop),
		png:      []byte{1, 2, 3},
		annot:    annotate.NewCanvas(),
		origW:    420,
		origH:    260,
		curW:     420,
		curH:     260,
		zoom:     1.0,
		opacity:  100,
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
		ui.Box(c).Fill().Padding(16, 16).
			Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
			Children(func() {
				ui.Themed(c, ui.DarkTheme()).Fill().Background(ui.Transparent).Children(func() {
					ui.Row(c).Fill().Gap(20).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(scrollCtrlW, scrollCtrlH).Children(func() {
							a.paintScrollCtrlView(c, scrollSess)
						})
						ui.Column(c).Gap(12).Children(func() {
							ui.Box(c).Size(420, 260).Children(func() {
								a.pinView(c, pEdit)
							})
							ui.Box(c).Size(420, 56).Radius(12).Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
								ui.Text(c, "悬停栏：").Absolute().Left(14).Top(20).FontSize(11.5).TextColor(ui.RGB(210, 216, 226))
								a.paintPinHoverBar(c, &pinWin{origW: 420, origH: 56, curW: 420, curH: 56, png: []byte{1}})
							})
						})
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
	// 缩略图改为后台加载：用本 goroutine 充当主线程，取回 2 张缩略图后再渲染。
	thumbMain := make(chan func(), 4)
	prevRun := runOnMain
	runOnMain = func(fn func()) { thumbMain <- fn }
	a.reloadHistoryWindow()
	for range 2 {
		(<-thumbMain)()
	}
	runOnMain = prevRun
	hw.statusBadge = "✓ 已复制到剪贴板"

	sw := &settingsWindow{
		tab:   1,
		draft: a.cfg,
		saved: "✓ 设置已生效",
	}
	cd := &countdownSession{remaining: 3}

	histAndSetImg := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Padding(14, 14).
			Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, false) }).
			Children(func() {
				ui.Themed(c, ui.LightTheme()).Fill().Background(ui.Transparent).Children(func() {
					ui.Column(c).Fill().Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(246, 42).Children(func() {
							a.paintCountdownView(c, cd)
						})
						ui.Row(c).Gap(16).Children(func() {
							ui.Box(c).Size(historyWinW, historyWinH).Children(func() {
								a.paintHistoryView(c, hw)
							})
							ui.Box(c).Size(settingsWinW, settingsWinH).Children(func() {
								a.paintSettingsView(c, sw)
							})
						})
					})
				})
			})
	}, historyWinW+settingsWinW+44, max(historyWinH, settingsWinH)+82, 2)
	savePNGFile(t, filepath.Join(dir, "verify_7_history_and_settings.png"), histAndSetImg)
	a.histWin = nil

	// 图 8：局部放大镜特写（ToolLoupe 实时预览 + 2x 真实导出）+ 离线 Vision 主体抠图（棋盘格透明底验证）
	a.session.Cancel()
	a.session.StartFrozen()
	a.session.InitCursor(200, 150)
	a.session.BeginDrag(0, 200, 150)
	a.session.EndDrag()
	ac = a.session.Annotations()
	ac.Reset()
	ac.Tool = annotate.ToolLoupe
	ac.Color = annotate.Palette[3] // 蓝色
	ac.Stroke = 3.0
	ac.SetLoupeZoom(3.0)
	ac.BeginPointer(520, 98) // 放大蓝色按钮
	ac.EndPointer(340, 175)
	// 第二个原地放大镜放大文字区域
	ac.Color = annotate.Palette[0]
	ac.Stroke = 2.0
	ac.SetLoupeZoom(2.5)
	ac.BeginPointer(225, 292)
	ac.EndPointer(228, 294) // <12 DIP -> 原地放大镜
	ac.Tool = annotate.ToolLoupe

	loupeLive := ui.Render(a.selectionView(o), dipW, dipH, 2)
	loupeCrop, err := crop.Crop([]crop.Screen{{Bounds: o.bounds, Scale: 2, Pixels: bgPx}}, sel)
	if err != nil {
		t.Fatalf("loupe crop: %v", err)
	}
	annotate.Render(loupeCrop, sel, ac.Items())

	// 真实 Vision 前景主体抠图放在棋盘格背景上展示透明通道效果
	subCanvas := image.NewNRGBA(image.Rect(0, 0, 280, 280))
	draw.Draw(subCanvas, subCanvas.Bounds(), image.NewUniform(color.NRGBA{R: 236, G: 240, B: 246, A: 255}), image.Point{}, draw.Src)
	for y := 55; y < 225; y++ {
		for x := 55; x < 225; x++ {
			dx, dy := float64(x-140), float64(y-140)
			d := dx*dx + dy*dy
			if d <= 75*75 {
				c := color.NRGBA{R: 245, G: 84, B: 66, A: 255}
				if d >= 28*28 && d <= 48*48 {
					c = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				}
				subCanvas.SetNRGBA(x, y, c)
			}
		}
	}
	cutImg, cutErr := vision.Cutout(subCanvas)
	if cutErr != nil {
		t.Logf("Cutout skipped or failed: %v", cutErr)
		cutImg = subCanvas
	}
	checker := image.NewNRGBA(image.Rect(0, 0, cutImg.Bounds().Dx()+48, cutImg.Bounds().Dy()+48))
	for y := range checker.Bounds().Dy() {
		for x := range checker.Bounds().Dx() {
			c := color.NRGBA{R: 48, G: 52, B: 60, A: 255}
			if ((x/14)+(y/14))%2 == 1 {
				c = color.NRGBA{R: 72, G: 76, B: 86, A: 255}
			}
			checker.SetNRGBA(x, y, c)
		}
	}
	draw.Draw(checker, cutImg.Bounds().Add(image.Pt(24, 24)), cutImg, cutImg.Bounds().Min, draw.Over)

	s8W := loupeLive.Bounds().Dx() + max(loupeCrop.Bounds().Dx(), checker.Bounds().Dx()) + 50
	s8H := max(loupeLive.Bounds().Dy(), loupeCrop.Bounds().Dy()+checker.Bounds().Dy()+40) + 20
	sheet8 := image.NewNRGBA(image.Rect(0, 0, s8W, s8H))
	draw.Draw(sheet8, sheet8.Bounds(), image.NewUniform(color.NRGBA{R: 18, G: 20, B: 26, A: 255}), image.Point{}, draw.Src)
	draw.Draw(sheet8, loupeLive.Bounds().Add(image.Pt(10, 10)), loupeLive, image.Point{}, draw.Over)
	draw.Draw(sheet8, loupeCrop.Bounds().Add(image.Pt(loupeLive.Bounds().Dx()+30, 10)), loupeCrop, image.Point{}, draw.Over)
	draw.Draw(sheet8, checker.Bounds().Add(image.Pt(loupeLive.Bounds().Dx()+30, loupeCrop.Bounds().Dy()+30)), checker, image.Point{}, draw.Over)
	savePNGFile(t, filepath.Join(dir, "verify_8_loupe_and_cutout.png"), sheet8)

	// 图 9：贴图拉帘对比 / 像素 Diff 热力图 + OCR 表格还原面板（Markdown 表格视图）
	cmpBefore := image.NewNRGBA(image.Rect(0, 0, 640, 400))
	cmpAfter := image.NewNRGBA(image.Rect(0, 0, 640, 400))
	draw.Draw(cmpBefore, cmpBefore.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 34, B: 44, A: 255}), image.Point{}, draw.Src)
	draw.Draw(cmpAfter, cmpAfter.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 34, B: 44, A: 255}), image.Point{}, draw.Src)
	draw.Draw(cmpBefore, image.Rect(40, 50, 280, 140), image.NewUniform(color.NRGBA{R: 10, G: 132, B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(cmpAfter, image.Rect(40, 50, 280, 140), image.NewUniform(color.NRGBA{R: 52, G: 199, B: 89, A: 255}), image.Point{}, draw.Src)
	draw.Draw(cmpBefore, image.Rect(340, 180, 580, 310), image.NewUniform(color.NRGBA{R: 72, G: 78, B: 96, A: 255}), image.Point{}, draw.Src)
	draw.Draw(cmpAfter, image.Rect(360, 195, 580, 325), image.NewUniform(color.NRGBA{R: 249, G: 115, B: 22, A: 255}), image.Point{}, draw.Src)

	diffRes := pinops.RenderPixelDiff(cmpBefore, cmpAfter, 12)
	pSplit := &pinWin{
		img: cmpAfter, bmp: ui.NewBitmap(cmpAfter),
		origW: 320, origH: 200, curW: 320, curH: 200, zoom: 1, opacity: 100,
		compareMode: true, compareDiff: false, compareSplit: 0.48,
		comparePeer: cmpBefore, comparePeerBmp: ui.NewBitmap(cmpBefore),
		compareDiffRes: diffRes, compareDiffBmp: ui.NewBitmap(diffRes.Image),
	}
	pDiff := &pinWin{
		img: cmpAfter, bmp: ui.NewBitmap(cmpAfter),
		origW: 320, origH: 200, curW: 320, curH: 200, zoom: 1, opacity: 100,
		compareMode: true, compareDiff: true, compareSplit: 0.48,
		comparePeer: cmpBefore, comparePeerBmp: ui.NewBitmap(cmpBefore),
		compareDiffRes: diffRes, compareDiffBmp: ui.NewBitmap(diffRes.Image),
	}
	tbl, _ := vision.ExtractTable([]vision.TextBox{
		{Text: "模块", X: 0.05, Top: 0.10, W: 0.18, H: 0.06},
		{Text: "耗时", X: 0.42, Top: 0.10, W: 0.15, H: 0.06},
		{Text: "状态", X: 0.74, Top: 0.10, W: 0.16, H: 0.06},
		{Text: "主体抠图", X: 0.05, Top: 0.30, W: 0.20, H: 0.06},
		{Text: "0.28s", X: 0.42, Top: 0.30, W: 0.14, H: 0.06},
		{Text: "离线可用", X: 0.74, Top: 0.30, W: 0.18, H: 0.06},
		{Text: "表格还原", X: 0.05, Top: 0.50, W: 0.20, H: 0.06},
		{Text: "0.15s", X: 0.42, Top: 0.50, W: 0.14, H: 0.06},
		{Text: "Markdown/TSV", X: 0.74, Top: 0.50, W: 0.22, H: 0.06},
	})
	owTbl := &ocrWindow{
		thumb:       ui.NewBitmap(cmpAfter),
		plainText:   "模块 耗时 状态\n主体抠图 0.28s 离线可用\n表格还原 0.15s Markdown/TSV",
		text:        tbl.Markdown(),
		table:       tbl,
		hasTable:    true,
		mode:        ocrModeMarkdown,
		copiedBadge: "已复制 Markdown",
	}
	sheet9Img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Padding(16, 16).
			Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
			Children(func() {
				ui.Themed(c, ui.DarkTheme()).Fill().Background(ui.Transparent).Children(func() {
					ui.Column(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
						ui.Row(c).Gap(16).Children(func() {
							ui.Box(c).Size(320, 200).Children(func() { a.pinView(c, pSplit) })
							ui.Box(c).Size(320, 200).Children(func() { a.pinView(c, pDiff) })
						})
						ui.Box(c).Size(520, 310).Children(func() { a.paintOCRView(c, owTbl) })
					})
				})
			})
	}, 688, 560, 2)
	savePNGFile(t, filepath.Join(dir, "verify_9_compare_diff_and_table_ocr.png"), sheet9Img)

	// 图 10：开发者多色板 + OKLCH/Tailwind 卡片 + 主色自适应流体渐变 & macOS 交通灯顶栏卡片
	palCard := beautify.RenderPaletteCard([]color.NRGBA{
		{R: 14, G: 165, B: 233, A: 255},
		{R: 16, G: 185, B: 129, A: 255},
		{R: 249, G: 115, B: 22, A: 255},
		{R: 168, G: 85, B: 247, A: 255},
	}, 2.0)
	singleColorCard := beautify.RenderTextPin("#0EA5E9", 2.0)
	chromeCard := beautify.RenderCardWithOptions(cropped, 2.0, beautify.CardOptions{
		Preset:       beautify.PresetAuto,
		WindowChrome: true,
	})
	pb1 := palCard.Bounds()
	pb2 := singleColorCard.Bounds()
	cb1 := chromeCard.Bounds()
	s10W := max(pb1.Dx()+pb2.Dx()+60, cb1.Dx()+40)
	s10H := max(pb1.Dy(), pb2.Dy()) + cb1.Dy() + 60
	sheet10 := image.NewNRGBA(image.Rect(0, 0, s10W, s10H))
	draw.Draw(sheet10, sheet10.Bounds(), image.NewUniform(color.NRGBA{R: 18, G: 20, B: 26, A: 255}), image.Point{}, draw.Src)
	draw.Draw(sheet10, pb1.Add(image.Pt(20, 20)), palCard, pb1.Min, draw.Over)
	draw.Draw(sheet10, pb2.Add(image.Pt(pb1.Dx()+40, 20)), singleColorCard, pb2.Min, draw.Over)
	draw.Draw(sheet10, cb1.Add(image.Pt(20, max(pb1.Dy(), pb2.Dy())+40)), chromeCard, cb1.Min, draw.Over)
	savePNGFile(t, filepath.Join(dir, "verify_10_color_palette_and_chrome_card.png"), sheet10)

	// 图 11：套模板全部 10 套主题卡片 5×2 全景展示
	type tplCase struct {
		kind    beautify.TemplateKind
		theme   int
		title   string
		caption string
	}
	tplCases := []tplCase{
		{beautify.TemplateStickyNote, 0, "接口联调记录", "红色标注区已脱敏，确认合并前复核"},
		{beautify.TemplatePolaroid, 0, "UI 走查定格", "对齐像素栅格 · Retina @2x"},
		{beautify.TemplateBrowser, 0, "https://snipgo.dev/docs", ""},
		{beautify.TemplateEditorial, 0, "SnipGo 套模板", "支持 10 套高颜值实用模板与 PNG 直接拖出"},
		{beautify.TemplateReceipt, 0, "SNIPGO STUDIO", "ZERO-CGO · VISION OCR · NATIVE"},
		{beautify.TemplateTerminal, 0, "zsh — snipgo studio", "go test -race ./...  ✓ passed 14 pkgs"},
		{beautify.TemplateFilm, 0, "KODAK 400 · FRAME 24A", "35mm Contact Sheet · Natural Grain"},
		{beautify.TemplateReview, 0, "ISSUE #408 · 排版走查", "高分屏文字清晰 · 建议放大细节对齐像素"},
		{beautify.TemplatePostcard, 0, "SHANGHAI · AIR MAIL", "Greeting from SnipGo Studio · 2026"},
		{beautify.TemplateSlide, 0, "SnipGo 26 液态玻璃", "原生态透明选区、离线 AI 抠图与结构化识图一步直达"},
	}
	const (
		galCols = 5
		galRows = 2
		cellW   = 380
		cellH   = 340
		galGap  = 18
	)
	galW := galCols*cellW + (galCols+1)*galGap
	galH := galRows*cellH + (galRows+1)*galGap
	sheet11 := image.NewNRGBA(image.Rect(0, 0, galW, galH))
	draw.Draw(sheet11, sheet11.Bounds(), image.NewUniform(color.NRGBA{R: 16, G: 18, B: 24, A: 255}), image.Point{}, draw.Src)
	for idx, tc := range tplCases {
		cardImg := beautify.RenderTemplate(cropped, 1.45, beautify.TemplateOptions{
			Kind:     tc.kind,
			Theme:    tc.theme,
			Title:    tc.title,
			Caption:  tc.caption,
			ShowDate: true,
			Padding:  beautify.PadNormal,
		})
		if cardImg == nil {
			continue
		}
		fitImg := fitImageBilinear(cardImg, cellW-16, cellH-16)
		col := idx % galCols
		row := idx / galCols
		cx := galGap + col*(cellW+galGap) + (cellW-fitImg.Bounds().Dx())/2
		cy := galGap + row*(cellH+galGap) + (cellH-fitImg.Bounds().Dy())/2
		draw.Draw(sheet11, fitImg.Bounds().Add(image.Pt(cx, cy)), fitImg, fitImg.Bounds().Min, draw.Over)
	}
	savePNGFile(t, filepath.Join(dir, "verify_11_templates_gallery.png"), sheet11)

	// 图 12：套模板工作台（paintTemplateView）940×656 @2x 浅色与深色 Liquid Glass 双外观渲染预览
	tplPrevSrc, tplFactor := downscaleForPreview(cropped, 620)
	tw := &templateWindow{
		srcImg:       cropped,
		srcScale:     2.0,
		previewSrc:   tplPrevSrc,
		previewScale: max(0.85, 2.0*tplFactor),
		opts: beautify.TemplateOptions{
			Kind:     beautify.TemplateTerminal,
			Theme:    0,
			Title:    "zsh — snipgo studio",
			Caption:  "支持实时预览、自定义配色、日期水印与 PNG 拖拽出图",
			ShowDate: true,
			Padding:  beautify.PadNormal,
		},
		statusBadge: "✓ 已复制 · 1409×974 px",
	}
	tw.refreshPreview()
	studioLight := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Padding(14, 14).
			Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, false) }).
			Children(func() {
				ui.Themed(c, ui.LightTheme()).Fill().Background(ui.Transparent).Children(func() {
					a.paintTemplateView(c, tw)
				})
			})
	}, templateWinW+28, templateWinH+28, 2)
	savePNGFile(t, filepath.Join(dir, "verify_12_template_studio_light.png"), studioLight)

	studioDark := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Padding(14, 14).
			Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
			Children(func() {
				ui.Themed(c, ui.DarkTheme()).Fill().Background(ui.Transparent).Children(func() {
					a.paintTemplateView(c, tw)
				})
			})
	}, templateWinW+28, templateWinH+28, 2)
	savePNGFile(t, filepath.Join(dir, "verify_12_template_studio_dark.png"), studioDark)

	// 图 13：全新 App 图标（Dock 浅色/深色多尺寸）+ 菜单栏模板图标（浅色/深色）真像素核验表
	saveIconPreviewSheet(t, filepath.Join(dir, "icon_preview_sheet.png"))

	// 图 14：自动更新器面板（浅色发现新版本 + 深色下载校验中与已是最新）
	uIcon, _ := ui.DecodeBitmap(appIconPNG)
	mockRel := &updateCandidate{
		Version: "0.2.0",
		Size:    4920000,
		Notes: "### 核心亮点\n" +
			"- 全新 macOS 26/27 Liquid Glass 自适应浅色与深色液态玻璃外观，支持清透/标准/浓磨砂与主题色自定义\n" +
			"- 套模板工作台精选 10 套实用样式，涵盖终端代码、35mm 胶片、评审工单与幻灯分栏\n" +
			"- 全浮窗采用 macOS 左上角红灯关闭（⌘W / Esc）与 ⌘Q 原生快捷习惯，内置签名自动更新",
	}
	uAvail := &updateManager{
		iconBmp:       uIcon,
		state:         updAvailable,
		release:       mockRel,
		canSelfUpdate: true,
	}
	uDown := &updateManager{
		iconBmp:       uIcon,
		state:         updDownloading,
		release:       mockRel,
		downloaded:    3145728,
		total:         4920000,
		canSelfUpdate: true,
	}
	uUpToDate := &updateManager{
		iconBmp:       uIcon,
		state:         updUpToDate,
		canSelfUpdate: true,
	}

	updaterImg := ui.Render(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(528).FillHeight().Padding(14, 14).
				AlignItems(ui.Center).Justify(ui.Center).
				Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, false) }).
				Children(func() {
					ui.Themed(c.Key("upd-light"), ui.LightTheme()).Size(updateWinW, updateWinH).Background(ui.Transparent).Children(func() {
						a.paintUpdateView(c, uAvail)
					})
				})
			ui.Box(c).Width(528).FillHeight().Padding(14, 14).
				AlignItems(ui.Center).Justify(ui.Center).
				Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, true) }).
				Children(func() {
					ui.Themed(c.Key("upd-dark"), ui.DarkTheme()).Fill().Background(ui.Transparent).
						Gap(12).AlignItems(ui.Center).Justify(ui.Center).
						Children(func() {
							ui.Box(c.Key("upd-dl")).Size(updateWinW, 304).Children(func() {
								a.paintUpdateView(c, uDown)
							})
							ui.Box(c.Key("upd-cur")).Size(updateWinW, 304).Children(func() {
								a.paintUpdateView(c, uUpToDate)
							})
						})
				})
		})
	}, 1056, 648, 2)
	savePNGFile(t, filepath.Join(dir, "verify_14_updater_ui.png"), updaterImg)

	// 图 15：外观与主题实时定制展示（清透+极光紫 / 深色+标准+珊瑚粉 / 浅色+浓磨砂+翡翠绿）
	prevTheme := getActiveTheme()
	defer setActiveTheme(prevTheme)

	renderTile := func(w, h int, darkWallpaper bool, st appThemeState, fn func(*ui.Context)) *image.RGBA {
		setActiveTheme(st)
		return ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Padding(14, 14).
				Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, darkWallpaper) }).
				Children(func() {
					fn(c)
				})
		}, w+28, h+28, 2)
	}

	swPurple := &settingsWindow{
		draft: config.Settings{
			ThemeAppearance:  "light",
			GlassLevel:       "clear",
			AccentColor:      "purple",
			QuickSave:        true,
			CopyOnSave:       true,
			QuickSaveDir:     a.cfg.QuickSaveDir,
			DelaySeconds:     3,
			SnipShortcut:     a.cfg.SnipShortcut,
			OCRShortcut:      a.cfg.OCRShortcut,
			FreezeShortcut:   a.cfg.FreezeShortcut,
			DelayShortcut:    a.cfg.DelayShortcut,
			PinShortcut:      a.cfg.PinShortcut,
			HistoryShortcut:  a.cfg.HistoryShortcut,
			AutoCheckUpdates: true,
		},
		saved: "实时预览（点保存生效）",
	}
	tile1 := renderTile(settingsWinW, settingsWinH, false, appThemeState{Appearance: "light", GlassLevel: "clear", Accent: "purple"}, func(c *ui.Context) {
		a.paintSettingsView(c, swPurple)
	})
	tile2 := renderTile(updateWinW, settingsWinH, true, appThemeState{Appearance: "dark", GlassLevel: "regular", Accent: "pink"}, func(c *ui.Context) {
		a.paintUpdateView(c, uAvail)
	})
	tile3 := renderTile(440, settingsWinH, false, appThemeState{Appearance: "light", GlassLevel: "frosted", Accent: "green"}, func(c *ui.Context) {
		a.paintOCRView(c, owTbl)
	})

	s15W := tile1.Bounds().Dx() + tile2.Bounds().Dx() + tile3.Bounds().Dx()
	s15H := max(tile1.Bounds().Dy(), max(tile2.Bounds().Dy(), tile3.Bounds().Dy()))
	sheet15 := image.NewNRGBA(image.Rect(0, 0, s15W, s15H))
	xOff := 0
	for _, tile := range []*image.RGBA{tile1, tile2, tile3} {
		draw.Draw(sheet15, tile.Bounds().Add(image.Pt(xOff, 0)), tile, tile.Bounds().Min, draw.Src)
		xOff += tile.Bounds().Dx()
	}
	savePNGFile(t, filepath.Join(dir, "verify_15_theme_showcase.png"), sheet15)

	// 图 16：新版「识图提取」Liquid Glass 全场景预览（深色交互式表格 / 深色等宽源码 / 浅色纯文本+链接）
	diffTbl := vision.Table{
		Cols: 2,
		Rows: [][]string{
			{"4个已更改文件 +133-89", "变更统计"},
			{".github/workflows", "+9 -17"},
			{"internal/vision", "+120 -68"},
			{"README.md", "+4 -4"},
		},
	}
	owDarkTable := &ocrWindow{
		plainText:   "4个已更改文件 +133-89 变更统计\n.github/workflows +9 -17\ninternal/vision +120 -68\nREADME.md +4 -4",
		text:        diffTbl.Markdown(),
		table:       diffTbl,
		hasTable:    true,
		mode:        ocrModeMarkdown,
		showRaw:     false,
		copiedBadge: "已复制 Markdown",
	}
	owDarkRaw := &ocrWindow{
		plainText:   owDarkTable.plainText,
		text:        diffTbl.TSV(),
		table:       diffTbl,
		hasTable:    true,
		mode:        ocrModeTSV,
		showRaw:     true,
		copiedBadge: "已复制 TSV",
	}
	owLightPlain := &ocrWindow{
		plainText:   "SnipGo 支持离线 macOS Vision OCR、二维码提取与结构化表格还原。\n按 ⌥⌘O 一键框选识图，自动复制结果。",
		text:        "SnipGo 支持离线 macOS Vision OCR、二维码提取与结构化表格还原。\n按 ⌥⌘O 一键框选识图，自动复制结果。",
		mode:        ocrModePlain,
		links:       []string{"https://github.com/Lumiaqian/snipgo"},
		copiedBadge: "已复制 · 58 字",
	}

	ocrTile1 := renderTile(ocrWinW+32, ocrWinH+32, true, appThemeState{Appearance: "dark", GlassLevel: "regular", Accent: "blue"}, func(c *ui.Context) {
		a.paintOCRView(c, owDarkTable)
	})
	ocrTile2 := renderTile(ocrWinW+32, ocrWinH+32, true, appThemeState{Appearance: "dark", GlassLevel: "regular", Accent: "blue"}, func(c *ui.Context) {
		a.paintOCRView(c, owDarkRaw)
	})
	ocrTile3 := renderTile(ocrWinW+32, ocrWinH+32, false, appThemeState{Appearance: "light", GlassLevel: "regular", Accent: "blue"}, func(c *ui.Context) {
		a.paintOCRView(c, owLightPlain)
	})

	s16W := ocrTile1.Bounds().Dx() + ocrTile2.Bounds().Dx() + ocrTile3.Bounds().Dx()
	s16H := max(ocrTile1.Bounds().Dy(), max(ocrTile2.Bounds().Dy(), ocrTile3.Bounds().Dy()))
	sheet16 := image.NewNRGBA(image.Rect(0, 0, s16W, s16H))
	x16 := 0
	for _, tile := range []*image.RGBA{ocrTile1, ocrTile2, ocrTile3} {
		draw.Draw(sheet16, tile.Bounds().Add(image.Pt(x16, 0)), tile, tile.Bounds().Min, draw.Src)
		x16 += tile.Bounds().Dx()
	}
	savePNGFile(t, filepath.Join(dir, "verify_16_ocr_redesign.png"), sheet16)

	// 图 17：深色壁纸下的浅色三档玻璃偏好设置 + 无边缘雾条更新日志对比
	var row1Tiles []*image.RGBA
	for _, lvl := range []string{"clear", "regular", "frosted"} {
		swLight := &settingsWindow{draft: swPurple.draft}
		swLight.draft.GlassLevel = lvl
		swLight.draft.AccentColor = "system"
		row1Tiles = append(row1Tiles, renderTile(settingsWinW, settingsWinH, true, appThemeState{
			Appearance: "light",
			GlassLevel: lvl,
			Accent:     "system",
		}, func(c *ui.Context) {
			a.paintSettingsView(c, swLight)
		}))
	}
	var row2Tiles []*image.RGBA
	for _, appMode := range []string{"dark", "light"} {
		row2Tiles = append(row2Tiles, renderTile(updateWinW, updateWinH, true, appThemeState{
			Appearance: appMode,
			GlassLevel: "regular",
			Accent:     "system",
		}, func(c *ui.Context) {
			a.paintUpdateView(c, uAvail)
		}))
	}
	r1W, r1H := 0, 0
	for _, tile := range row1Tiles {
		r1W += tile.Bounds().Dx()
		r1H = max(r1H, tile.Bounds().Dy())
	}
	r2W, r2H := 0, 0
	for _, tile := range row2Tiles {
		r2W += tile.Bounds().Dx()
		r2H = max(r2H, tile.Bounds().Dy())
	}
	sheet17 := image.NewNRGBA(image.Rect(0, 0, max(r1W, r2W), r1H+r2H))
	draw.Draw(sheet17, sheet17.Bounds(), image.NewUniform(color.NRGBA{R: 16, G: 20, B: 32, A: 255}), image.Point{}, draw.Src)
	x17 := 0
	for _, tile := range row1Tiles {
		draw.Draw(sheet17, tile.Bounds().Add(image.Pt(x17, 0)), tile, tile.Bounds().Min, draw.Src)
		x17 += tile.Bounds().Dx()
	}
	x17 = 0
	for _, tile := range row2Tiles {
		draw.Draw(sheet17, tile.Bounds().Add(image.Pt(x17, r1H)), tile, tile.Bounds().Min, draw.Src)
		x17 += tile.Bounds().Dx()
	}
	savePNGFile(t, filepath.Join(dir, "verify_17_light_on_dark_and_updater.png"), sheet17)
}

func paintTahoeWallpaper(p *ui.Painter, r ui.Rect, dark bool) {
	if dark {
		p.FillGradient(r, ui.LinearGradient{
			From:  ui.RGB(16, 26, 54),
			To:    ui.RGB(48, 22, 62),
			Angle: 135,
			Oklab: true,
		}, 0)
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{X: 0.22, Y: 0.24},
			Radius: r.W * 0.55,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(24, 128, 255, 0.45), At: 0},
				{Color: ui.RGBA(24, 128, 255, 0), At: 1},
			},
		}, 0)
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{X: 0.80, Y: 0.72},
			Radius: r.W * 0.50,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(198, 68, 224, 0.34), At: 0},
				{Color: ui.RGBA(198, 68, 224, 0), At: 1},
			},
		}, 0)
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{X: 0.55, Y: 0.18},
			Radius: r.W * 0.36,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(28, 196, 174, 0.26), At: 0},
				{Color: ui.RGBA(28, 196, 174, 0), At: 1},
			},
		}, 0)
		return
	}
	p.FillGradient(r, ui.LinearGradient{
		From:  ui.RGB(146, 206, 255),
		To:    ui.RGB(240, 196, 232),
		Angle: 135,
		Oklab: true,
	}, 0)
	p.FillRadialGradient(r, ui.RadialGradient{
		Center: ui.Point{X: 0.20, Y: 0.26},
		Radius: r.W * 0.52,
		Stops: []ui.GradientStop{
			{Color: ui.RGBA(74, 178, 255, 0.52), At: 0},
			{Color: ui.RGBA(74, 178, 255, 0), At: 1},
		},
	}, 0)
	p.FillRadialGradient(r, ui.RadialGradient{
		Center: ui.Point{X: 0.82, Y: 0.70},
		Radius: r.W * 0.50,
		Stops: []ui.GradientStop{
			{Color: ui.RGBA(255, 134, 186, 0.44), At: 0},
			{Color: ui.RGBA(255, 134, 186, 0), At: 1},
		},
	}, 0)
	p.FillRadialGradient(r, ui.RadialGradient{
		Center: ui.Point{X: 0.54, Y: 0.20},
		Radius: r.W * 0.38,
		Stops: []ui.GradientStop{
			{Color: ui.RGBA(92, 224, 182, 0.38), At: 0},
			{Color: ui.RGBA(92, 224, 182, 0), At: 1},
		},
	}, 0)
}

func fitImageBilinear(src *image.NRGBA, maxW, maxH int) *image.NRGBA {
	if src == nil || src.Bounds().Empty() || maxW <= 0 || maxH <= 0 {
		return src
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	r := min(float64(maxW)/float64(sw), float64(maxH)/float64(sh))
	if r >= 1.0 {
		return src
	}
	dw := max(1, int(math.Round(float64(sw)*r)))
	dh := max(1, int(math.Round(float64(sh)*r)))
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		sy := (float64(y)+0.5)/float64(dh)*float64(sh) - 0.5
		y0 := max(0, min(sh-1, int(math.Floor(sy))))
		y1 := max(0, min(sh-1, y0+1))
		fy := sy - float64(y0)
		for x := range dw {
			sx := (float64(x)+0.5)/float64(dw)*float64(sw) - 0.5
			x0 := max(0, min(sw-1, int(math.Floor(sx))))
			x1 := max(0, min(sw-1, x0+1))
			fx := sx - float64(x0)

			c00 := src.NRGBAAt(sb.Min.X+x0, sb.Min.Y+y0)
			c10 := src.NRGBAAt(sb.Min.X+x1, sb.Min.Y+y0)
			c01 := src.NRGBAAt(sb.Min.X+x0, sb.Min.Y+y1)
			c11 := src.NRGBAAt(sb.Min.X+x1, sb.Min.Y+y1)
			lerpU8 := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bot := float64(c)*(1-fx) + float64(d)*fx
				return uint8(math.Round(max(0, min(255, top*(1-fy)+bot*fy))))
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: lerpU8(c00.R, c10.R, c01.R, c11.R),
				G: lerpU8(c00.G, c10.G, c01.G, c11.G),
				B: lerpU8(c00.B, c10.B, c01.B, c11.B),
				A: lerpU8(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return dst
}

func saveIconPreviewSheet(t *testing.T, outPath string) {
	t.Helper()
	iconBytes, err := os.ReadFile(filepath.Join("resources", "icon.png"))
	if err != nil {
		t.Fatalf("read resources/icon.png: %v", err)
	}
	iconDec, err := png.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		t.Fatalf("decode icon: %v", err)
	}
	iconNRGBA := image.NewNRGBA(image.Rect(0, 0, iconDec.Bounds().Dx(), iconDec.Bounds().Dy()))
	draw.Draw(iconNRGBA, iconNRGBA.Bounds(), iconDec, iconDec.Bounds().Min, draw.Src)

	trayRaw, err := png.Decode(bytes.NewReader(trayIcon()))
	if err != nil {
		t.Fatalf("decode trayIcon: %v", err)
	}
	trayBlack := image.NewNRGBA(image.Rect(0, 0, trayRaw.Bounds().Dx(), trayRaw.Bounds().Dy()))
	draw.Draw(trayBlack, trayBlack.Bounds(), trayRaw, trayRaw.Bounds().Min, draw.Src)
	trayWhite := image.NewNRGBA(trayBlack.Bounds())
	for y := range trayWhite.Bounds().Dy() {
		for x := range trayWhite.Bounds().Dx() {
			c := trayBlack.NRGBAAt(x, y)
			trayWhite.SetNRGBA(x, y, color.NRGBA{R: 245, G: 247, B: 250, A: c.A})
		}
	}

	const W, H = 900, 620
	canvas := image.NewNRGBA(image.Rect(0, 0, W, H))
	// 上半区：浅色 macOS 桌面与 Dock/菜单栏；下半区：深色 macOS 桌面与 Dock/菜单栏
	draw.Draw(canvas, image.Rect(0, 0, W, H/2), image.NewUniform(color.NRGBA{R: 238, G: 241, B: 246, A: 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(0, H/2, W, H), image.NewUniform(color.NRGBA{R: 22, G: 24, B: 30, A: 255}), image.Point{}, draw.Src)

	drawBand := func(yBase int, tintTray *image.NRGBA) {
		i256 := fitImageBilinear(iconNRGBA, 256, 256)
		i64 := fitImageBilinear(iconNRGBA, 64, 64)
		i32 := fitImageBilinear(iconNRGBA, 32, 32)
		draw.Draw(canvas, i256.Bounds().Add(image.Pt(36, yBase+27)), i256, image.Point{}, draw.Over)
		draw.Draw(canvas, i64.Bounds().Add(image.Pt(330, yBase+123)), i64, image.Point{}, draw.Over)
		draw.Draw(canvas, i32.Bounds().Add(image.Pt(424, yBase+139)), i32, image.Point{}, draw.Over)

		// 菜单栏图标 4× 放大展示 (144×144) 与真实 @2x 展示 (36×36)
		tb4 := image.NewNRGBA(image.Rect(0, 0, 144, 144))
		tw := tintTray.Bounds().Dx()
		for y := range 144 {
			for x := range 144 {
				tb4.SetNRGBA(x, y, tintTray.NRGBAAt(min(tw-1, x/4), min(tw-1, y/4)))
			}
		}
		draw.Draw(canvas, tb4.Bounds().Add(image.Pt(540, yBase+83)), tb4, image.Point{}, draw.Over)
		draw.Draw(canvas, tintTray.Bounds().Add(image.Pt(730, yBase+137)), tintTray, image.Point{}, draw.Over)
	}
	drawBand(0, trayBlack)
	drawBand(H/2, trayWhite)
	savePNGFile(t, outPath, canvas)
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
