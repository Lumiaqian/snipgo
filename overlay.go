package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/crop"
	"snipgo/internal/session"
	"snipgo/internal/vision"
)

// overlay 盖住一块显示器。
// 实时模式下背景透明，直接透出桌面；冻结模式下在底层绘制捕获好的静态位图。
type overlay struct {
	win     *mygo.Window
	bitmap  *ui.Bitmap
	bounds  image.Rectangle
	index   int
	closing bool
}

// overlayTitle 标记选区蒙层窗口，采集时按它把蒙层排除在截图外。
const overlayTitle = "SnipGo 选区"

func (a *app) ShowSelection(frames []crop.Screen) {
	a.HideSelection()
	a.textEditing = false
	a.textEditBuf = ""
	cursor := mygo.Screen.CursorScreenPoint()
	a.session.InitCursor(float64(cursor.X), float64(cursor.Y))
	key := -1
	a.overlays = make([]*overlay, 0, len(frames))
	for i, frame := range frames {
		b := frame.Bounds.Canon()
		if b.Empty() {
			continue
		}
		var bmp *ui.Bitmap
		if frame.Pixels != nil {
			bmp = ui.NewBitmap(frame.Pixels)
		}
		o := &overlay{
			bitmap: bmp,
			bounds: b,
			index:  i,
		}
		o.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           overlayTitle,
			Width:           b.Dx(),
			Height:          b.Dy(),
			X:               40,
			Y:               40,
			Hidden:          true,
			Frameless:       true,
			Transparent:     true,
			DisableResize:   true,
			DisableMaximize: true,
			DisableShadow:   true,
			Panel:           true,
			Content:         ui.View(a.selectionView(o)),
		})
		o.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting || o.closing {
				return
			}
			e.PreventDefault()
			a.session.Cancel()
		})
		// 设为 NSWindowSharingNone，由 WindowServer 硬件级排除在所有截图之外，杜绝蒙层残留入镜。
		o.win.SetContentProtection(true)
		prepareOverlayWindow(o.win)
		applyWindowAppearance(o.win, a.cfg.ThemeAppearance)
		// X 和 Y 同时为 0 会把窗口放到屏幕中央，所以先放在 40,40 再挪到显示器原点。
		o.win.SetBounds(mygo.Rectangle{X: b.Min.X, Y: b.Min.Y, Width: b.Dx(), Height: b.Dy()})
		a.overlays = append(a.overlays, o)
		if cursor.X >= b.Min.X && cursor.X < b.Max.X && cursor.Y >= b.Min.Y && cursor.Y < b.Max.Y {
			key = len(a.overlays) - 1
		}
	}
	if key < 0 {
		key = len(a.overlays) - 1
	}
	showOverlay := func(o *overlay) {
		o.win.Show()
		if actual := o.win.Bounds(); actual.Width > 0 && actual.Height > 0 {
			o.bounds = image.Rect(actual.X, actual.Y, actual.X+actual.Width, actual.Y+actual.Height)
		}
	}
	for i, o := range a.overlays {
		if i == key {
			continue
		}
		showOverlay(o)
	}
	if key >= 0 && key < len(a.overlays) {
		showOverlay(a.overlays[key])
	}
}

func (a *app) HideSelection() {
	a.textEditing = false
	a.textEditBuf = ""
	old := a.overlays
	a.overlays = nil
	for _, o := range old {
		o.closing = true
		if o.win != nil {
			o.win.Hide()
			o.win.Close()
		}
	}
}

func (a *app) selectionView(o *overlay) func(*ui.Context) {
	return func(c *ui.Context) {
		// MyGo 默认用主题背景色（深色模式下为纯黑 #18181b）填充 Root；透明蒙层必须显式清空。
		c.Root().Background(ui.Transparent)
		if o.closing {
			return
		}

		withAppTheme(c, func() {
			if a.handleSelectionKeys(c) {
				return
			}
			w := float32(o.bounds.Dx())
			h := float32(o.bounds.Dy())
			var toolbarAct func()
			ui.Box(c).Size(w, h).Children(func() {
				if o.bitmap != nil {
					ui.Image(c, o.bitmap).Size(w, h).Absolute().Left(0).Top(0).IgnoreInput(true)
				}
				hit := ui.Box(c).Size(w, h).Absolute().Left(0).Top(0).ZIndex(2)
				if o.bitmap == nil {
					// macOS SkyLight 合成器会把 alpha < 0.05 (13/255) 的透明窗口像素当成鼠标穿透区；
					// 0.055 (14/255) 刚好高于穿透阈值以捕获鼠标移动与点击，同时视觉上几乎完全透明。
					hit.Background(ui.RGBA(128, 128, 128, 0.055))
				}
				lx, ly, _ := hit.PointerPosition()
				gx := float64(o.bounds.Min.X) + float64(lx)
				gy := float64(o.bounds.Min.Y) + float64(ly)
				hit.Cursor(cursorForHandle(a.session.HitTest(gx, gy)))
				hit.HandleInput(func(ev ui.InputEvent) bool {
					return a.onCanvasInput(o, ev)
				})
				a.paintMask(c, o)
				a.paintTextEditor(c, o)
				toolbarAct = a.paintToolbar(c, o)
			})
			if toolbarAct != nil {
				a.deferUI(toolbarAct)
			}
		})
	}
}

func (a *app) onCanvasInput(o *overlay, ev ui.InputEvent) bool {
	gx := float64(o.bounds.Min.X) + float64(ev.X)
	gy := float64(o.bounds.Min.Y) + float64(ev.Y)

	switch ev.Kind {
	case ui.InputPointerMove:
		if a.session.Dragging() || a.session.AnnotatingDrag() {
			if a.session.DragOwner() == o.index {
				a.session.MoveDrag(o.index, gx, gy)
				a.invalidateOthers(o)
				return true
			}
			return false
		}
		if a.session.Hover(gx, gy) {
			a.invalidateOthers(o)
			return true
		}
		if !a.session.IsLive() {
			// 冻结模式下放大镜与像素测量尺跟随每帧光标移动。
			return true
		}
		return false

	case ui.InputPointerDown:
		if ev.Button == 1 || (ev.Button == 0 && ev.Mods == ui.Ctrl) {
			if a.textEditing {
				a.commitOrCloseTextEdit()
				a.invalidateAll()
				return true
			}
			a.deferUI(func() {
				a.session.RightClick()
				a.invalidateAll()
			})
			return true
		}
		if ev.Button != 0 {
			return false
		}
		if a.textEditing {
			a.commitOrCloseTextEdit()
		}
		pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
		if a.session.Phase() == session.PhaseAdjusting {
			if sel, locked, ok := a.session.ActiveRect(); ok && locked && pt.In(sel) {
				if a.session.Annotations().Tool == annotate.ToolText {
					a.textEditing = true
					a.textEditX, a.textEditY = gx, gy
					a.textEditBuf = ""
					a.invalidateAll()
					return true
				}
				if ev.Clicks >= 2 && a.session.Annotations().Tool == annotate.ToolNone {
					a.deferUI(a.session.Copy)
					return true
				}
			}
		}
		a.session.BeginDrag(o.index, gx, gy)
		a.invalidateOthers(o)
		return true

	case ui.InputPointerUp:
		if ev.Button != 0 || (!a.session.Dragging() && !a.session.AnnotatingDrag()) || a.session.DragOwner() != o.index {
			return false
		}
		if ev.Canceled {
			a.session.Cancel()
			return true
		}
		a.session.MoveDrag(o.index, gx, gy)
		a.session.EndDrag()
		a.invalidateAll()
		return true
	}
	return false
}

func (a *app) commitOrCloseTextEdit() {
	if !a.textEditing {
		return
	}
	if a.textEditBuf != "" {
		a.session.Annotations().AddText(a.textEditX, a.textEditY, a.textEditBuf)
	}
	a.textEditing = false
	a.textEditBuf = ""
}

func (a *app) handleSelectionKeys(c *ui.Context) bool {
	esc := c.Shortcut(0, ui.KeyEscape)
	if a.textEditing {
		if esc {
			a.textEditing = false
			a.textEditBuf = ""
			a.invalidateAll()
		}
		return false
	}

	cmdQ := c.Shortcut(ui.Cmd, ui.KeyQ)
	cmdW := c.Shortcut(ui.Cmd, ui.KeyW)
	space := c.Shortcut(0, ui.KeySpace)
	enter := c.Shortcut(0, ui.KeyEnter)
	cmdC := c.Shortcut(ui.Cmd, ui.KeyC)
	cmdZ := c.Shortcut(ui.Cmd, ui.KeyZ)
	cmdShiftZ := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ)
	keyT := c.Shortcut(0, ui.KeyT)
	cmdT := c.Shortcut(ui.Cmd, ui.KeyT)
	cmdS := c.Shortcut(ui.Cmd, ui.KeyS)
	keyO := c.Shortcut(0, ui.KeyO)
	keyW := c.Shortcut(0, ui.KeyW)
	keyB := c.Shortcut(0, ui.KeyB)
	keyR := c.Shortcut(0, ui.KeyR)
	keyL := c.Shortcut(0, ui.KeyL)
	keyX := c.Shortcut(0, ui.KeyX)
	keyK := c.Shortcut(0, ui.KeyK)
	keyP := c.Shortcut(0, ui.KeyP)
	shiftP := c.Shortcut(ui.Shift, ui.KeyP)
	tabKey := c.Shortcut(0, ui.KeyTab)
	keyC := c.Shortcut(0, ui.KeyC)
	shiftC := c.Shortcut(ui.Shift, ui.KeyC)
	delKey := c.Shortcut(0, ui.KeyBackspace) || c.Shortcut(0, ui.KeyDelete)

	toolKeys := [...]struct {
		key  ui.Key
		tool annotate.Tool
	}{
		{ui.Key1, annotate.ToolRect},
		{ui.Key2, annotate.ToolEllipse},
		{ui.Key3, annotate.ToolArrow},
		{ui.Key4, annotate.ToolPen},
		{ui.Key5, annotate.ToolStep},
		{ui.Key6, annotate.ToolMosaic},
		{ui.Key7, annotate.ToolText},
		{ui.Key8, annotate.ToolSpotlight},
		{ui.Key9, annotate.ToolErase},
		{ui.Key0, annotate.ToolLoupe},
	}
	var pickedTool annotate.Tool = -1
	for _, tk := range toolKeys {
		if c.Shortcut(0, tk.key) && pickedTool < 0 {
			pickedTool = tk.tool
		}
	}

	type nudgeKey struct {
		mods   ui.Modifiers
		key    ui.Key
		dx, dy int
		resize bool
	}
	nudges := [...]nudgeKey{
		{0, ui.KeyLeft, -1, 0, false},
		{0, ui.KeyRight, 1, 0, false},
		{0, ui.KeyUp, 0, -1, false},
		{0, ui.KeyDown, 0, 1, false},
		{ui.Shift, ui.KeyLeft, -10, 0, false},
		{ui.Shift, ui.KeyRight, 10, 0, false},
		{ui.Shift, ui.KeyUp, 0, -10, false},
		{ui.Shift, ui.KeyDown, 0, 10, false},
		{ui.Alt, ui.KeyLeft, -1, 0, true},
		{ui.Alt, ui.KeyRight, 1, 0, true},
		{ui.Alt, ui.KeyUp, 0, -1, true},
		{ui.Alt, ui.KeyDown, 0, 1, true},
		{ui.Shift | ui.Alt, ui.KeyLeft, -10, 0, true},
		{ui.Shift | ui.Alt, ui.KeyRight, 10, 0, true},
		{ui.Shift | ui.Alt, ui.KeyUp, 0, -10, true},
		{ui.Shift | ui.Alt, ui.KeyDown, 0, 10, true},
	}
	var firedNudge *nudgeKey
	for i := range nudges {
		if c.Shortcut(nudges[i].mods, nudges[i].key) && firedNudge == nil {
			firedNudge = &nudges[i]
		}
	}

	switch {
	case cmdQ:
		a.deferUI(func() { mygo.App.Quit() })
		return true
	case esc || cmdW:
		a.deferUI(a.session.Cancel)
		return true
	case cmdShiftZ:
		if a.session.Annotations().Redo() {
			a.invalidateAll()
		}
		return false
	case cmdZ:
		if a.session.Annotations().Undo() {
			a.invalidateAll()
		}
		return false
	case delKey:
		if a.session.Annotations().DeleteSelected() {
			a.invalidateAll()
		}
		return false
	case pickedTool >= 0 && a.session.Phase() == session.PhaseAdjusting:
		ac := a.session.Annotations()
		ac.ClearSelection()
		if ac.Tool == pickedTool {
			ac.Tool = annotate.ToolNone
		} else {
			ac.Tool = pickedTool
		}
		a.invalidateAll()
		return false
	case keyX && a.session.Phase() == session.PhaseAdjusting:
		a.autoRedactSelection()
		return false
	case keyK:
		a.commitOrCloseTextEdit()
		a.deferUI(a.session.Cutout)
		return true
	case tabKey:
		a.session.CycleColorFormat()
		a.invalidateAll()
		return false
	case shiftP:
		if len(a.session.PickedColors()) > 0 {
			a.deferUI(func() { a.session.PinPickedPalette() })
			return true
		}
		return false
	case keyP:
		if a.session.SampleColorToPalette(func(color.NRGBA) { a.invalidateAll() }) {
			a.invalidateAll()
		}
		return false
	case keyL && a.session.Phase() == session.PhaseAdjusting:
		if sel, locked, ok := a.session.ActiveRect(); ok && locked {
			a.deferUI(func() { a.startScrollCapture(sel) })
			return true
		}
		return false
	case space:
		if a.session.ToggleWindowSnap() {
			a.invalidateAll()
		}
		return false
	case enter || cmdC:
		a.deferUI(a.session.Copy)
		return true
	case keyT || cmdT:
		a.deferUI(a.session.Pin)
		return true
	case cmdS:
		a.deferUI(a.session.Save)
		return true
	case keyO:
		a.deferUI(a.session.OCR)
		return true
	case keyW:
		if a.session.ToggleWindowOnly() {
			a.invalidateAll()
		}
		return false
	case keyB:
		a.commitOrCloseTextEdit()
		a.deferUI(a.session.Template)
		return true
	case keyR:
		if a.session.ToggleMeasure() {
			a.invalidateAll()
		}
		return false
	case keyC:
		a.deferUI(func() { a.session.CopyColor(false) })
		return true
	case shiftC:
		a.deferUI(func() { a.session.CopyColor(true) })
		return true
	case firedNudge != nil:
		if a.session.Nudge(firedNudge.dx, firedNudge.dy, firedNudge.resize) {
			a.invalidateAll()
		}
	}
	return false
}

func cursorForHandle(h session.Handle) ui.Cursor {
	switch h {
	case session.HandleMove:
		return ui.CursorMove
	case session.HandleNW, session.HandleSE:
		return ui.CursorResizeNWSE
	case session.HandleNE, session.HandleSW:
		return ui.CursorResizeNESW
	case session.HandleN, session.HandleS:
		return ui.CursorResizeNS
	case session.HandleE, session.HandleW:
		return ui.CursorResizeEW
	default:
		return ui.CursorCrosshair
	}
}

func (a *app) invalidateOthers(self *overlay) {
	for _, o := range a.overlays {
		if o != self && o.win != nil {
			o.win.Invalidate()
		}
	}
}

func (a *app) invalidateAll() {
	for _, o := range a.overlays {
		if o.win != nil {
			o.win.Invalidate()
		}
	}
}

func (a *app) paintMask(c *ui.Context, o *overlay) {
	hole := image.Rectangle{}
	rect, locked, hasRect := a.session.ActiveRect()
	if hasRect {
		hole = localSelection(o.bounds, rect)
	}
	win := image.Rect(0, 0, o.bounds.Dx(), o.bounds.Dy())
	_, border := dimBands(win, hole)
	accent := uiPalette(c).Accent
	if a.session.WindowOnly() {
		accent = ui.RGB(52, 199, 89)
	}
	if !border.Empty() {
		strokeW := float32(1.5)
		if !locked && !a.session.Dragging() {
			strokeW = 2
		}
		// MyGo 的 Border 向盒内绘制线宽；向外外扩 strokeW（并在屏边夹紧）让内沿精确贴齐选区像素边界。
		bx0 := max(float32(win.Min.X), float32(border.Min.X)-strokeW)
		by0 := max(float32(win.Min.Y), float32(border.Min.Y)-strokeW)
		bx1 := min(float32(win.Max.X), float32(border.Max.X)+strokeW)
		by1 := min(float32(win.Max.Y), float32(border.Max.Y)+strokeW)
		ui.Box(c).Absolute().
			Left(bx0).Top(by0).
			Size(bx1-bx0, by1-by0).
			ZIndex(3).
			Border(strokeW, accent).
			Shadow(0, 0, 4, 0, ui.RGBA(0, 0, 0, 0.35)).
			IgnoreInput(true)

		if locked {
			a.paintAnnotations(c, o, border)
		}
	}
	if locked && !a.session.Dragging() && a.session.Annotations().Tool == annotate.ToolNone {
		for _, gp := range session.HandlePoints(rect) {
			lx := gp.X - o.bounds.Min.X
			ly := gp.Y - o.bounds.Min.Y
			if lx < -4 || lx > win.Max.X+4 || ly < -4 || ly > win.Max.Y+4 {
				continue
			}
			ui.Box(c).Absolute().
				Left(float32(lx)-4.5).Top(float32(ly)-4.5).
				Size(9, 9).
				ZIndex(4).
				Radius(4.5).
				Background(ui.RGB(255, 255, 255)).
				Border(1.5, accent).
				Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.45)).
				IgnoreInput(true)
		}
	}
	nativeGlass := o.bitmap == nil
	pal := uiPalette(c)
	if hasRect && !border.Empty() && a.ownsPoint(o, rect.Min) {
		pxW, pxH := a.session.PixelSize(rect)
		if label := formatSizeLabel(rect.Dx(), rect.Dy(), pxW, pxH); label != "" {
			if a.session.WindowOnly() {
				label += " · 独立单窗"
			}
			pos := hudTopLeft(win, border)
			liquidGlassCapsule(
				c,
				ui.Box(c).Absolute().
					Left(float32(pos.X)).Top(float32(pos.Y)).
					ZIndex(5).
					Padding(4, 10).
					IgnoreInput(true),
				13,
				nativeGlass,
			).Children(func() {
				ui.Text(c, label).
					FontSize(11.5).
					FontWeight(500).
					FontFeatures("tnum").
					TextColor(pal.Text)
			})
		}
	}
	if a.session.AutoOCR() && len(a.overlays) > 0 && o == a.overlays[0] {
		liquidGlassCapsule(
			c,
			ui.Row(c).Absolute().
				Left(float32(max(12, win.Dx()/2-200))).Top(44).
				Height(34).
				Padding(0, 14).
				Gap(7).
				AlignItems(ui.Center).
				ZIndex(6).
				IgnoreInput(true),
			17,
			nativeGlass,
		).Children(func() {
			ui.Icon(c, iconOCR).FontSize(14).TextColor(pal.Accent)
			ui.Text(c, "识图模式 · 拖拽框选或单击窗口松手即刻提取文字与二维码 · Esc 取消").
				FontSize(12).FontWeight(500).NoWrap().TextColor(pal.Text)
		})
	}
	if len(a.session.PickedColors()) > 0 && len(a.overlays) > 0 && o == a.overlays[0] {
		a.paintPickedPaletteBar(c, o, win)
	}
	if a.session.MeasureMode() {
		a.paintRuler(c, o, win)
	} else if a.session.ShowLoupe() {
		a.paintLoupe(c, o, win)
	}
}

func (a *app) paintAnnotations(c *ui.Context, o *overlay, border image.Rectangle) {
	ac := a.session.Annotations()
	items := ac.Items()
	selectedIdx := ac.Selected
	if d := ac.Drawing(); d != nil {
		items = append(items, *d)
	}
	if len(items) == 0 {
		return
	}
	ox := float64(o.bounds.Min.X)
	oy := float64(o.bounds.Min.Y)

	ui.Box(c).Absolute().
		Left(float32(border.Min.X)).Top(float32(border.Min.Y)).
		Size(float32(border.Dx()), float32(border.Dy())).
		ZIndex(3).
		IgnoreInput(true).
		Draw(func(p *ui.Painter, r ui.Rect) {
			baseX := r.X - float32(border.Min.X)
			baseY := r.Y - float32(border.Min.Y)
			paintCanvasAnnotations(p, r, items, selectedIdx, ox, oy, baseX, baseY)
		})
}

func paintCanvasAnnotations(p *ui.Painter, clipR ui.Rect, items []annotate.Item, selectedIdx int, ox, oy float64, baseX, baseY float32) {
	p.Clip(clipR, 0, func() {
		toUI := func(pt annotate.PointF) (float32, float32) {
			return baseX + float32(pt.X-ox), baseY + float32(pt.Y-oy)
		}

		// 第一趟：擦除块与马赛克预览 + 收集聚光灯窗口
		var spotRects []image.Rectangle
		var spotUIRects []ui.Rect
		for _, it := range items {
			x0, y0 := toUI(it.P0)
			x1, y1 := toUI(it.P1)
			rx, ry := min(x0, x1), min(y0, y1)
			rw, rh := float32(math.Abs(float64(x1-x0))), float32(math.Abs(float64(y1-y0)))
			switch it.Tool {
			case annotate.ToolErase:
				drawErasePreview(p, ui.Rect{X: rx, Y: ry, W: rw, H: rh})
			case annotate.ToolMosaic:
				drawMosaicPreview(p, ui.Rect{X: rx, Y: ry, W: rw, H: rh})
			case annotate.ToolSpotlight:
				if rw >= 2 && rh >= 2 {
					spotUIRects = append(spotUIRects, ui.Rect{X: rx, Y: ry, W: rw, H: rh})
					spotRects = append(spotRects, image.Rect(int(math.Round(float64(rx))), int(math.Round(float64(ry))), int(math.Round(float64(rx+rw))), int(math.Round(float64(ry+rh)))))
				}
			}
		}

		// 第二趟：聚光灯全局统一压暗（任意多个重叠聚光灯互不二次叠加变黑）
		if len(spotRects) > 0 {
			whole := image.Rect(
				int(math.Floor(float64(clipR.X))),
				int(math.Floor(float64(clipR.Y))),
				int(math.Ceil(float64(clipR.X+clipR.W))),
				int(math.Ceil(float64(clipR.Y+clipR.H))),
			)
			for _, band := range subtractRects([]image.Rectangle{whole}, spotRects) {
				p.Fill(ui.Rect{
					X: float32(band.Min.X),
					Y: float32(band.Min.Y),
					W: float32(band.Dx()),
					H: float32(band.Dy()),
				}, ui.RGBA(0, 0, 0, 0.58), 0)
			}
			for _, sr := range spotUIRects {
				p.Stroke(sr, ui.RGBA(255, 255, 255, 0.72), 6, 1.5)
			}
		}

		// 第三趟：上层高对比矢量标注
		for _, it := range items {
			col := ui.RGBA(it.Color.R, it.Color.G, it.Color.B, float32(it.Color.A)/255)
			sw := float32(max(1.5, it.Stroke))
			x0, y0 := toUI(it.P0)
			x1, y1 := toUI(it.P1)
			rx, ry := min(x0, x1), min(y0, y1)
			rw, rh := float32(math.Abs(float64(x1-x0))), float32(math.Abs(float64(y1-y0)))

			switch it.Tool {
			case annotate.ToolRect:
				p.Stroke(ui.Rect{X: rx, Y: ry, W: rw, H: rh}, col, 4, sw)
			case annotate.ToolEllipse:
				drawPreviewEllipse(p, rx, ry, rw, rh, sw, col)
			case annotate.ToolArrow:
				drawPreviewArrow(p, it.P0, it.P1, toUI, sw, col)
			case annotate.ToolPen:
				drawPreviewHighlighter(p, it.Points, toUI, float32(max(10, it.Stroke)), ui.RGBA(it.Color.R, it.Color.G, it.Color.B, 0.42))
			case annotate.ToolStep:
				drawPreviewStepBadge(p, x0, y0, it.Step, it.Color, col)
			case annotate.ToolText:
				fSize := float32(13 + it.Stroke*2)
				p.RichText(x0+1, y0+1, 0, ui.Span{Text: it.Text, Size: fSize, Weight: 600, Color: ui.RGBA(0, 0, 0, 0.65)})
				p.RichText(x0, y0, 0, ui.Span{Text: it.Text, Size: fSize, Weight: 600, Color: col})
			case annotate.ToolLoupe:
				zoom := it.Zoom
				if zoom <= 0 {
					zoom = 2.5
				}
				drawPreviewLoupe(p, x0, y0, x1, y1, float32(annotate.LoupeRadius(it.Stroke)), zoom, col)
			}
		}

		// 第四趟：已点选标注图元的编辑手柄高亮
		if selectedIdx >= 0 && selectedIdx < len(items) {
			sel := items[selectedIdx]
			sx0, sy0 := toUI(sel.P0)
			sx1, sy1 := toUI(sel.P1)
			drawSelectionHandles(p, sel, sx0, sy0, sx1, sy1)
		}
	})
}

func drawErasePreview(p *ui.Painter, r ui.Rect) {
	p.Fill(r, ui.RGBA(236, 238, 244, 0.92), 3)
	p.StrokeDashed(r, ui.RGBA(10, 132, 255, 0.78), 3, 1.2)
}

func drawPreviewLoupe(p *ui.Painter, x0, y0, x1, y1, rad float32, zoom float64, accent ui.Color) {
	if rad < 14 {
		rad = 48
	}
	dist := float32(math.Hypot(float64(x1-x0), float64(y1-y0)))
	if dist > 10 {
		ux, uy := (x1-x0)/dist, (y1-y0)/dist
		const tr = float32(7)
		if dist > tr+rad+4 {
			lx0, ly0 := x0+ux*tr, y0+uy*tr
			lx1, ly1 := x1-ux*(rad+3), y1-uy*(rad+3)
			var stem ui.Path
			stem.Cap(ui.RoundCap).MoveTo(lx0, ly0).LineTo(lx1, ly1)
			p.StrokePath(&stem, 3.6, ui.RGBA(255, 255, 255, 0.85))
			p.StrokePath(&stem, 2.0, accent)
		}
		p.Stroke(ui.Rect{X: x0 - tr, Y: y0 - tr, W: tr * 2, H: tr * 2}, accent, tr, 2)
		p.Fill(ui.Rect{X: x0 - 2, Y: y0 - 2, W: 4, H: 4}, accent, 2)
	}
	lensR := ui.Rect{X: x1 - rad, Y: y1 - rad, W: rad * 2, H: rad * 2}
	p.Shadow(lensR, rad, 0, 5, 16, 0, ui.RGBA(0, 0, 0, 0.42))
	p.Fill(lensR, ui.RGBA(20, 24, 34, 0.24), rad)
	p.Stroke(lensR, ui.RGBA(255, 255, 255, 0.96), rad, 3)
	p.Stroke(ui.Rect{X: x1 - rad - 1.5, Y: y1 - rad - 1.5, W: (rad + 1.5) * 2, H: (rad + 1.5) * 2}, accent, rad+1.5, 1.3)

	// 镜内右下角精致倍率角标
	badge := fmt.Sprintf("%.1f×", zoom)
	sp := ui.Span{Text: badge, Size: 11, Weight: 700, Features: "tnum", Color: ui.RGB(255, 255, 255)}
	tw, th := p.MeasureText(0, sp)
	bw, bh := tw+10, th+4
	bx := x1 - bw*0.5
	by := y1 + rad - bh - 6
	p.Fill(ui.Rect{X: bx, Y: by, W: bw, H: bh}, ui.RGBA(18, 20, 26, 0.82), bh*0.5)
	p.RichText(bx+5, by+2, 0, sp)
}

func drawSelectionHandles(p *ui.Painter, it annotate.Item, x0, y0, x1, y1 float32) {
	hi := ui.RGB(10, 132, 255)
	dot := func(cx, cy float32) {
		p.Fill(ui.Rect{X: cx - 5, Y: cy - 5, W: 10, H: 10}, ui.RGB(255, 255, 255), 5)
		p.Stroke(ui.Rect{X: cx - 5, Y: cy - 5, W: 10, H: 10}, hi, 5, 2)
	}
	switch it.Tool {
	case annotate.ToolStep, annotate.ToolText:
		dot(x0, y0)
	case annotate.ToolLoupe:
		dot(x0, y0)
		dot(x1, y1)
	case annotate.ToolPen:
		if len(it.Points) > 0 {
			dot(x0, y0)
			if len(it.Points) > 1 {
				dot(x1, y1)
			}
		}
	default:
		rx, ry := min(x0, x1), min(y0, y1)
		rw, rh := float32(math.Abs(float64(x1-x0))), float32(math.Abs(float64(y1-y0)))
		if it.Tool != annotate.ToolArrow {
			p.StrokeDashed(ui.Rect{X: rx - 2, Y: ry - 2, W: rw + 4, H: rh + 4}, ui.RGBA(64, 172, 255, 0.75), 4, 1)
		}
		dot(x0, y0)
		dot(x1, y1)
	}
}

func subtractRects(bands []image.Rectangle, holes []image.Rectangle) []image.Rectangle {
	for _, h := range holes {
		h = h.Canon()
		if h.Empty() {
			continue
		}
		var next []image.Rectangle
		for _, b := range bands {
			inter := b.Intersect(h)
			if inter.Empty() {
				next = append(next, b)
				continue
			}
			if inter.Min.Y > b.Min.Y {
				next = append(next, image.Rect(b.Min.X, b.Min.Y, b.Max.X, inter.Min.Y))
			}
			if inter.Max.Y < b.Max.Y {
				next = append(next, image.Rect(b.Min.X, inter.Max.Y, b.Max.X, b.Max.Y))
			}
			if inter.Min.X > b.Min.X {
				next = append(next, image.Rect(b.Min.X, inter.Min.Y, inter.Min.X, inter.Max.Y))
			}
			if inter.Max.X < b.Max.X {
				next = append(next, image.Rect(inter.Max.X, inter.Min.Y, b.Max.X, inter.Max.Y))
			}
		}
		bands = next
	}
	return bands
}

func drawPreviewEllipse(p *ui.Painter, rx, ry, rw, rh, sw float32, col ui.Color) {
	if rw < 1 || rh < 1 {
		return
	}
	const kappa = 0.55228475
	a := rw * 0.5
	b := rh * 0.5
	cx := rx + a
	cy := ry + b
	kx := a * kappa
	ky := b * kappa
	var path ui.Path
	path.MoveTo(cx, cy-b).
		CubeTo(cx+kx, cy-b, cx+a, cy-ky, cx+a, cy).
		CubeTo(cx+a, cy+ky, cx+kx, cy+b, cx, cy+b).
		CubeTo(cx-kx, cy+b, cx-a, cy+ky, cx-a, cy).
		CubeTo(cx-a, cy-ky, cx-kx, cy-b, cx, cy-b).
		Close()
	p.StrokePath(&path, sw, col)
}

func drawPreviewArrow(p *ui.Painter, p0, p1 annotate.PointF, toUI func(annotate.PointF) (float32, float32), sw float32, col ui.Color) {
	poly, ok := annotate.ArrowPolygon(p0, p1, float64(sw))
	if !ok {
		return
	}
	var path ui.Path
	x0, y0 := toUI(poly[0])
	path.MoveTo(x0, y0)
	for i := 1; i < len(poly); i++ {
		xi, yi := toUI(poly[i])
		path.LineTo(xi, yi)
	}
	path.Close()
	p.FillPath(&path, col)

	// 尾部半圆封帽与导出栅格保持完全一致。
	tx, ty := toUI(p0)
	tailR := float32(math.Hypot(poly[0].X-poly[6].X, poly[0].Y-poly[6].Y) * 0.5)
	if tailR >= 0.5 {
		p.Fill(ui.Rect{X: tx - tailR, Y: ty - tailR, W: tailR * 2, H: tailR * 2}, col, tailR)
	}
}

func drawPreviewHighlighter(p *ui.Painter, pts []annotate.PointF, toUI func(annotate.PointF) (float32, float32), pw float32, ink ui.Color) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		cx, cy := toUI(pts[0])
		r := pw * 0.5
		p.Fill(ui.Rect{X: cx - r, Y: cy - r, W: pw, H: pw}, ink, r)
		return
	}
	var path ui.Path
	path.Cap(ui.RoundCap).Join(ui.RoundJoin)
	x0, y0 := toUI(pts[0])
	path.MoveTo(x0, y0)
	for i := 1; i < len(pts); i++ {
		xi, yi := toUI(pts[i])
		path.LineTo(xi, yi)
	}
	p.StrokePath(&path, pw, ink)
}

func drawPreviewStepBadge(p *ui.Painter, cx, cy float32, step int, rawCol color.NRGBA, col ui.Color) {
	const rad = float32(13)
	p.Shadow(ui.Rect{X: cx - rad - 2, Y: cy - rad - 2, W: (rad + 2) * 2, H: (rad + 2) * 2}, rad+2, 0, 1.5, 4, 0, ui.RGBA(0, 0, 0, 0.38))
	p.Fill(ui.Rect{X: cx - rad - 2, Y: cy - rad - 2, W: (rad + 2) * 2, H: (rad + 2) * 2}, ui.RGB(255, 255, 255), rad+2)
	p.Fill(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, col, rad)

	numCol := ui.RGB(255, 255, 255)
	if int(rawCol.R)+int(rawCol.G)+int(rawCol.B) > 650 {
		numCol = ui.RGB(24, 24, 28)
	}
	span := ui.Span{
		Text:     strconv.Itoa(step),
		Size:     13.5,
		Weight:   700,
		Features: "tnum",
		Color:    numCol,
	}
	tw, th := p.MeasureText(0, span)
	p.RichText(cx-tw*0.5, cy-th*0.5, 0, span)
}

func drawMosaicPreview(p *ui.Painter, r ui.Rect) {
	const cell = float32(10)
	c1 := ui.RGBA(40, 44, 52, 0.78)
	c2 := ui.RGBA(180, 186, 198, 0.78)
	row := 0
	for y := r.Y; y < r.Y+r.H; y += cell {
		h := min(cell, r.Y+r.H-y)
		colIdx := row
		for x := r.X; x < r.X+r.W; x += cell {
			w := min(cell, r.X+r.W-x)
			col := c1
			if colIdx%2 == 1 {
				col = c2
			}
			p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, col, 0)
			colIdx++
		}
		row++
	}
	p.StrokeDashed(r, ui.RGBA(255, 255, 255, 0.65), 2, 1)
}

func (a *app) paintTextEditor(c *ui.Context, o *overlay) {
	if !a.textEditing {
		return
	}
	pt := image.Pt(int(math.Floor(a.textEditX)), int(math.Floor(a.textEditY)))
	if !a.ownsPoint(o, pt) {
		return
	}
	lx := max(8, min(pt.X-o.bounds.Min.X, o.bounds.Dx()-230))
	ly := max(8, min(pt.Y-o.bounds.Min.Y, o.bounds.Dy()-42))
	col := a.session.Annotations().Color

	box := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Left(float32(lx)).Top(float32(ly)).
			Width(220).
			ZIndex(12).
			Padding(4, 6).
			Gap(6).
			AlignItems(ui.Center),
		12,
		o.bitmap == nil,
	).Border(1.5, ui.RGB(col.R, col.G, col.B))
	_ = box.Clicked()
	box.Children(func() {
		inp := baseTextInput(c.Key("annot-text-input"), &a.textEditBuf).
			Grow(1).
			Placeholder("输入批注，回车确认").
			AutoFocus().
			Radius(8)
		if inp.Submitted() {
			a.commitOrCloseTextEdit()
			a.invalidateAll()
		}
	})
}

func (a *app) paintRuler(c *ui.Context, o *overlay, win image.Rectangle) {
	rd, ok := a.session.SampleRuler()
	if !ok {
		return
	}
	pt := image.Pt(int(math.Floor(rd.OriginX)), int(math.Floor(rd.OriginY)))
	if !pt.In(o.bounds) {
		return
	}
	ox := float32(o.bounds.Min.X)
	oy := float32(o.bounds.Min.Y)
	cx := float32(rd.OriginX) - ox
	cy := float32(rd.OriginY) - oy
	x0 := float32(rd.LeftDIP) - ox
	x1 := float32(rd.RightDIP) - ox
	y0 := float32(rd.TopDIP) - oy
	y1 := float32(rd.BottomDIP) - oy

	ui.Box(c).Absolute().Left(0).Top(0).Size(float32(win.Dx()), float32(win.Dy())).
		ZIndex(6).IgnoreInput(true).
		Draw(func(p *ui.Painter, r ui.Rect) {
			lineCol := ui.RGB(255, 69, 58)
			p.Line(r.X+x0, r.Y+cy, r.X+x1, r.Y+cy, 1.5, lineCol)
			p.Line(r.X+cx, r.Y+y0, r.X+cx, r.Y+y1, 1.5, lineCol)
			// 两端止动短线
			p.Line(r.X+x0, r.Y+cy-5, r.X+x0, r.Y+cy+5, 1.5, lineCol)
			p.Line(r.X+x1, r.Y+cy-5, r.X+x1, r.Y+cy+5, 1.5, lineCol)
			p.Line(r.X+cx-5, r.Y+y0, r.X+cx+5, r.Y+y0, 1.5, lineCol)
			p.Line(r.X+cx-5, r.Y+y1, r.X+cx+5, r.Y+y1, 1.5, lineCol)
		})

	badge := fmt.Sprintf("↔ %d px (%d)  ↕ %d px (%d) · 单击复制", rd.WidthPx, rd.WidthDIP, rd.HeightPx, rd.HeightDIP)
	pos := loupePos(win, image.Pt(int(cx), int(cy)), 240, 32)
	pal := uiPalette(c)
	liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Left(float32(pos.X)).Top(float32(pos.Y)).
			ZIndex(7).
			Padding(5, 10).
			Gap(6).
			AlignItems(ui.Center).
			IgnoreInput(true),
		13,
		o.bitmap == nil,
	).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Shrink(0).Background(ui.RGB(255, 69, 58))
		ui.Text(c, badge).FontSize(11.5).FontWeight(500).FontFeatures("tnum").TextColor(pal.Text)
	})
}

func (a *app) paintLoupe(c *ui.Context, o *overlay, win image.Rectangle) {
	gx, gy, ok := a.session.Cursor()
	if !ok {
		return
	}
	pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
	if !pt.In(o.bounds) {
		return
	}
	loupe, ok := a.session.SampleLoupe(5)
	if !ok || loupe.Side <= 0 {
		return
	}
	const (
		cellSize = 10
		pad      = 6
		footerH  = 54
	)
	gridPx := loupe.Side * cellSize
	boxW := max(gridPx+pad*2, 186)
	boxH := gridPx + pad*2 + footerH
	gridLeft := (boxW - gridPx) / 2
	localCursor := image.Pt(pt.X-o.bounds.Min.X, pt.Y-o.bounds.Min.Y)
	pos := loupePos(win, localCursor, boxW, boxH)
	cFmt := a.session.ColorFormat()
	colorText := beautify.FormatColor(loupe.Center, cFmt)
	fmtTag := beautify.ColorFormatName(cFmt)
	coordText := fmt.Sprintf("(%d,%d) · Tab 格式 · C 复制 · P 色板", loupe.DIP.X, loupe.DIP.Y)
	pal := uiPalette(c)

	liquidGlassCapsule(
		c,
		ui.Box(c).Absolute().
			Left(float32(pos.X)).Top(float32(pos.Y)).
			Size(float32(boxW), float32(boxH)).
			ZIndex(6).
			IgnoreInput(true),
		16,
		o.bitmap == nil,
	).Children(func() {
		ui.Box(c).Absolute().
			Left(float32(gridLeft)).Top(pad).
			Size(float32(gridPx), float32(gridPx)).
			Radius(8).Clip().
			Draw(func(p *ui.Painter, r ui.Rect) {
				drawLoupeGrid(p, r, loupe, cellSize)
			})
		ui.Row(c).Absolute().
			Left(pad).Top(float32(pad + gridPx + 6)).
			Width(float32(boxW - pad*2)).
			AlignItems(ui.Center).Gap(5).
			Children(func() {
				sw := loupe.Center
				ui.Box(c).Size(12, 12).Radius(4).Shrink(0).
					Background(ui.RGB(sw.R, sw.G, sw.B)).
					Border(1, pal.SubtleBorder)
				ui.Text(c, colorText).
					FontSize(11).
					FontFeatures("tnum").
					NoWrap().
					MaxLines(1).
					Ellipsis("…").
					Grow(1).
					TextColor(pal.Text)
				ui.Row(c).Height(16).Padding(0, 5).Radius(8).Shrink(0).AlignItems(ui.Center).
					Background(pal.BadgeInfoBg).
					Border(1, pal.BadgeInfoBd).
					Children(func() {
						ui.Text(c, fmtTag).FontSize(9.5).FontWeight(500).NoWrap().TextColor(pal.BadgeInfoFg)
					})
			})
		ui.Text(c, coordText).Absolute().
			Left(pad).Top(float32(pad + gridPx + 26)).
			FontSize(9.5).
			FontFeatures("tnum").
			TextColor(pal.TextSecondary)
	})
}

func (a *app) paintPickedPaletteBar(c *ui.Context, o *overlay, win image.Rectangle) {
	colors := a.session.PickedColors()
	if len(colors) == 0 {
		return
	}
	pal := uiPalette(c)
	bar := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Left(float32(max(12, win.Dx()/2-210))).Top(14).
			Height(34).
			Padding(0, 10).
			Gap(6).
			AlignItems(ui.Center).
			ZIndex(11),
		17,
		o.bitmap == nil,
	)
	_ = bar.Clicked()
	bar.Children(func() {
		ui.Icon(c, iconPalette).FontSize(13.5).TextColor(pal.Accent)
		ui.Text(c, fmt.Sprintf("色板 (%d)", len(colors))).FontSize(11.5).FontWeight(500).NoWrap().TextColor(pal.Text)
		for i, col := range colors {
			col := col
			tip := beautify.FormatColor(col, a.session.ColorFormat()) + " · " + beautify.NearestTailwind(col)
			dot := ui.Box(c.Key(700+i)).Size(16, 16).Radius(8).Shrink(0).
				Tooltip(tip).
				Background(ui.RGB(col.R, col.G, col.B)).
				Border(1.5, pal.SubtleBorder).
				Cursor(ui.CursorPointer)
			if dot.Clicked() {
				mygo.Clipboard.WriteText(beautify.FormatColor(col, a.session.ColorFormat()))
			}
		}
		if miniPillBtn(c, iconPin, "贴色卡 (⇧P)", true) {
			a.deferUI(func() { a.session.PinPickedPalette() })
		}
		if miniPillBtn(c, iconClose, "清空", false) {
			a.session.ClearPickedColors()
			a.invalidateAll()
		}
	})
}

func drawLoupeGrid(p *ui.Painter, r ui.Rect, loupe session.Loupe, cell float32) {
	side := loupe.Side
	for y := range side {
		for x := range side {
			px := loupe.Grid[y*side+x]
			cellRect := ui.Rect{
				X: r.X + float32(x)*cell,
				Y: r.Y + float32(y)*cell,
				W: cell,
				H: cell,
			}
			p.Fill(cellRect, ui.RGB(px.R, px.G, px.B), 0)
		}
	}
	gridColor := ui.RGBA(255, 255, 255, 0.08)
	for i := 1; i < side; i++ {
		off := float32(i) * cell
		p.Line(r.X+off, r.Y, r.X+off, r.Y+r.H, 1, gridColor)
		p.Line(r.X, r.Y+off, r.X+r.W, r.Y+off, 1, gridColor)
	}
	mid := float32(side/2) * cell
	accent := ui.RGBA(0, 168, 255, 0.9)
	p.Stroke(ui.Rect{X: r.X + mid, Y: r.Y + mid, W: cell, H: cell}, accent, 0, 1.5)
	p.Stroke(r, ui.RGBA(255, 255, 255, 0.22), 0, 1)
}

func (a *app) paintToolbar(c *ui.Context, o *overlay) func() {
	sel, locked, ok := a.session.ActiveRect()
	if !ok || !locked || a.session.Dragging() || a.session.AnnotatingDrag() {
		return nil
	}
	anchorPt := image.Pt(sel.Max.X-1, sel.Max.Y-1)
	if !a.ownsPoint(o, anchorPt) {
		return nil
	}
	win := image.Rect(0, 0, o.bounds.Dx(), o.bounds.Dy())
	_, border := dimBands(win, localSelection(o.bounds, sel))
	if border.Empty() {
		return nil
	}

	ac := a.session.Annotations()
	hasSelected := ac.Selected >= 0
	showStyleControls := ac.Tool != annotate.ToolNone || hasSelected
	showSubBar := showStyleControls
	const (
		mainBarH = 40
		subBarH  = 38
		estBarW  = 740
		edgeGap  = 8
	)
	totalH := mainBarH
	if showSubBar {
		totalH = mainBarH + 6 + subBarH
	}
	pos := toolbarTopLeft(win, border, estBarW, totalH)
	anchorRight := border.Max.X >= min(win.Max.X-edgeGap, estBarW+edgeGap*2)

	var chosen func()
	wrap := ui.Column(c).Absolute().
		Top(float32(pos.Y)).
		ZIndex(10).
		Gap(6)
	if anchorRight {
		wrap.Right(float32(max(edgeGap, win.Max.X-border.Max.X))).AlignItems(ui.End)
	} else {
		leftX := max(edgeGap, min(border.Min.X, win.Max.X-estBarW-edgeGap))
		wrap.Left(float32(leftX)).AlignItems(ui.Start)
	}

	nativeGlass := o.bitmap == nil
	wrap.Children(func() {
		bar := liquidGlassCapsule(
			c,
			ui.Row(c).
				Height(mainBarH).
				Padding(4, 8).
				Gap(1.5).
				AlignItems(ui.Center).
				Shrink(0),
			20,
			nativeGlass,
		)
		_ = bar.Clicked()
		bar.Children(func() {
			tools := [...]struct {
				label string
				tip   string
				icon  *ui.SVG
				tool  annotate.Tool
			}{
				{"框", "矩形框 (1)", iconRect, annotate.ToolRect},
				{"圆", "椭圆框 (2)", iconEllipse, annotate.ToolEllipse},
				{"箭头", "箭头指示 (3)", iconArrow, annotate.ToolArrow},
				{"荧光", "荧光马克笔 (4)", iconHighlighter, annotate.ToolPen},
				{"序号", "自动步骤序号 (5)", iconStep, annotate.ToolStep},
				{"打码", "像素马赛克 (6)", iconMosaic, annotate.ToolMosaic},
				{"擦除", "无痕擦除文字 (9)", iconErase, annotate.ToolErase},
				{"聚光", "聚光灯高亮 (8)", iconSpotlight, annotate.ToolSpotlight},
				{"文字", "文字批注 (7)", iconText, annotate.ToolText},
				{"放大", "局部放大镜特写 (0)", iconLoupe, annotate.ToolLoupe},
			}
			for i, t := range tools {
				if iconToolBtn(c.Key(100+i), t.icon, t.label, t.tip, ac.Tool == t.tool, false) {
					a.commitOrCloseTextEdit()
					ac.ClearSelection()
					if ac.Tool == t.tool {
						ac.Tool = annotate.ToolNone
					} else {
						ac.Tool = t.tool
					}
					a.invalidateAll()
				}
			}
			// 撤销槽位保持固定 30×30 宽度，防止绘制首笔后整个工具栏发生水平跳动。
			canUndo := ac.CanUndo()
			if iconToolBtn(c, iconUndo, "撤销", "撤销上一笔 (⌘Z)", false, !canUndo) && canUndo {
				ac.Undo()
				a.invalidateAll()
			}

			toolbarDivider(c)

			if iconToolBtn(c, iconShield, "脱敏", "自动识别并遮盖 API Key / 手机 / 邮箱 / IP (X)", false, a.redacting) && !a.redacting {
				a.commitOrCloseTextEdit()
				a.autoRedactSelection()
			}
			if iconToolBtn(c, iconCutout, "抠图", "离线 AI 主体抠图并贴为异形贴纸 (K)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Cutout
			}
			if a.session.CanWindowOnly() {
				if iconToolBtn(c, iconWindowOnly, "单窗", "纯净单窗抠图（去壁纸遮挡）(W)", a.session.WindowOnly(), false) {
					a.session.ToggleWindowOnly()
					a.invalidateAll()
				}
			}
			if iconToolBtn(c, iconTemplate, "模板", "套用便签 / 拍立得 / 浏览器 / 分享卡等模板 (B)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Template
			}
			if !a.session.IsLive() {
				if iconToolBtn(c, iconRuler, "标尺", "像素间距测量尺 (R)", a.session.MeasureMode(), false) {
					a.session.ToggleMeasure()
					a.invalidateAll()
				}
			}
			if iconToolBtn(c, iconScrollCap, "长图", "滚动长截图 (L)", false, false) {
				a.commitOrCloseTextEdit()
				selCopy := sel
				chosen = func() { a.startScrollCapture(selCopy) }
			}

			toolbarDivider(c)

			if actionPillBtn(c, iconOCR, "识图", "提取图中文字与二维码 (O)", false, true) {
				a.commitOrCloseTextEdit()
				chosen = a.session.OCR
			}
			if actionPillBtn(c, iconPin, "贴图", "钉在桌面悬浮贴图 (T)", false, true) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Pin
			}
			if iconToolBtn(c, iconSave, "保存", "保存为 PNG (⌘S)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Save
			}
			if iconToolBtn(c, iconClose, "取消", "取消 (Esc)", false, false) {
				chosen = a.session.Cancel
			}
			if actionPillBtn(c, iconCheck, "复制", "复制到剪贴板 (Enter)", true, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Copy
			}
		})

		if showSubBar {
			sub := liquidGlassCapsule(
				c,
				ui.Row(c).
					Height(subBarH).
					Padding(4, 10).
					Gap(7).
					AlignItems(ui.Center).
					Shrink(0),
				19,
				nativeGlass,
			)
			_ = sub.Clicked()
			sub.Children(func() {
				if showStyleControls {
					uiPal := uiPalette(c)
					curCol := ac.Color
					curStroke := ac.Stroke
					if selIt, ok := ac.SelectedItem(); ok {
						curCol = selIt.Color
						curStroke = selIt.Stroke
					}
					for i, pal := range annotate.Palette {
						pal := pal
						active := curCol == pal
						dot := ui.Box(c.Key(i)).Size(18, 18).Radius(9).
							Shrink(0).
							Background(ui.RGB(pal.R, pal.G, pal.B)).
							Cursor(ui.CursorPointer)
						if dot.Pressed() {
							dot.Scale(0.92, 0.92)
						}
						if active {
							dot.Border(2, uiPal.Accent).
								InsetShadow(0, 0, 0, 1.5, ui.RGB(255, 255, 255))
						} else {
							dot.Border(1, uiPal.SubtleBorder)
						}
						if dot.Clicked() {
							ac.SetColor(pal)
							a.invalidateAll()
						}
					}
					toolbarDivider(c)
					for i, st := range [...]struct {
						label string
						tip   string
						w     float64
						dotR  float32
					}{
						{"细", "细笔画", 2.0, 2.0},
						{"中", "中笔画", 3.0, 3.5},
						{"粗", "粗笔画", 6.0, 5.2},
					} {
						if strokeDotBtn(c.Key(200+i), st.label, st.tip, st.dotR, math.Abs(curStroke-st.w) < 0.4) {
							ac.SetStroke(st.w)
							a.invalidateAll()
						}
					}
					if hasSelected {
						toolbarDivider(c)
						if iconToolBtn(c, iconTrash, "删除标注", "删除当前选中的标注 (⌫)", false, false) {
							ac.DeleteSelected()
							a.invalidateAll()
						}
					}
					selIt, hasSelIt := ac.SelectedItem()
					if ac.Tool == annotate.ToolLoupe || (hasSelIt && selIt.Tool == annotate.ToolLoupe) {
						toolbarDivider(c)
						curZoom := ac.LoupeZoom
						if hasSelIt && selIt.Tool == annotate.ToolLoupe && selIt.Zoom > 0 {
							curZoom = selIt.Zoom
						}
						zoomVals := [...]float64{2.0, 2.5, 3.0, 4.0}
						zoomSel := 0
						for i, z := range zoomVals {
							if math.Abs(curZoom-z) < 0.15 {
								zoomSel = i
								break
							}
						}
						if picked := compactSegmented(c, "loupe-zoom", zoomSel, []string{"2×", "2.5×", "3×", "4×"}); picked >= 0 && picked != zoomSel {
							ac.SetLoupeZoom(zoomVals[picked])
							a.invalidateAll()
						}
					}
				}
			})
		}
	})
	return chosen
}

// autoRedactSelection 自动离线扫描当前选区中的敏感凭证（API Key / 手机 / 邮箱 / 身份证 / IP）并生成可编辑无痕脱敏块。
func (a *app) autoRedactSelection() {
	if a.redacting || a.session == nil || a.session.Phase() != session.PhaseAdjusting {
		return
	}
	sel, locked, ok := a.session.ActiveRect()
	if !ok || !locked || sel.Empty() {
		return
	}
	if frozenImg, frozenSel, ok := a.session.CropFrozenSelection(); ok {
		a.redacting = true
		go a.runVisionAutoRedact(frozenImg, frozenSel)
		return
	}
	a.redacting = true
	a.invalidateAll()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		img, err := a.captureRect(ctx, sel)
		if err != nil || img == nil || img.Bounds().Empty() {
			mygo.RunOnMain(func() {
				a.redacting = false
				a.invalidateAll()
			})
			return
		}
		a.runVisionAutoRedact(img, sel)
	}()
}

func (a *app) runVisionAutoRedact(img *image.NRGBA, sel image.Rectangle) {
	res, err := vision.Recognize(img)
	var boxes []annotate.RectF
	if err == nil {
		for _, sb := range vision.SensitiveBoxes(res) {
			boxes = append(boxes, annotate.RectF{
				X: float64(sel.Min.X) + sb.X*float64(sel.Dx()),
				Y: float64(sel.Min.Y) + sb.Top*float64(sel.Dy()),
				W: max(8, sb.W*float64(sel.Dx())),
				H: max(8, sb.H*float64(sel.Dy())),
			})
		}
	}
	mygo.RunOnMain(func() {
		a.redacting = false
		if a.session != nil && a.session.Phase() == session.PhaseAdjusting && len(boxes) > 0 {
			n := a.session.Annotations().AddRedactions(boxes, true)
			a.sessionStatus(fmt.Sprintf("已自动脱敏 %d 处敏感信息", n))
		}
		a.invalidateAll()
	})
}

func (a *app) ownsPoint(o *overlay, pt image.Point) bool {
	if pt.In(o.bounds) {
		return true
	}
	for _, other := range a.overlays {
		if pt.In(other.bounds) {
			return false
		}
	}
	return len(a.overlays) > 0 && o == a.overlays[0]
}

func localSelection(bounds, sel image.Rectangle) image.Rectangle {
	return image.Rect(
		sel.Min.X-bounds.Min.X,
		sel.Min.Y-bounds.Min.Y,
		sel.Max.X-bounds.Min.X,
		sel.Max.Y-bounds.Min.Y,
	)
}

func hudTopLeft(win, border image.Rectangle) image.Point {
	const (
		hudH      = 24
		gap       = 6
		minRightW = 180
	)
	x := border.Min.X
	if x+minRightW > win.Max.X {
		x = max(win.Min.X+gap, win.Max.X-minRightW)
	}
	y := border.Min.Y - hudH - gap
	if y < win.Min.Y+gap {
		y = border.Min.Y + gap
	}
	return image.Pt(x, y)
}

func toolbarTopLeft(win, border image.Rectangle, barW, barH int) image.Point {
	const gap = 8
	x := border.Max.X - barW
	x = max(win.Min.X+gap, min(x, win.Max.X-barW-gap))
	y := border.Max.Y + gap
	if y+barH+gap > win.Max.Y {
		y = border.Min.Y - barH - gap
	}
	if y < win.Min.Y+gap {
		y = max(win.Min.Y+gap, border.Max.Y-barH-gap)
	}
	return image.Pt(x, y)
}

func loupePos(win image.Rectangle, cursor image.Point, boxW, boxH int) image.Point {
	const offset = 20
	x := cursor.X + offset
	if x+boxW+8 > win.Max.X {
		x = cursor.X - boxW - offset
	}
	y := cursor.Y + offset
	if y+boxH+8 > win.Max.Y {
		y = cursor.Y - boxH - offset
	}
	x = max(win.Min.X+6, min(x, win.Max.X-boxW-6))
	y = max(win.Min.Y+6, min(y, win.Max.Y-boxH-6))
	return image.Pt(x, y)
}

func formatSizeLabel(dipW, dipH, pxW, pxH int) string {
	if dipW <= 0 || dipH <= 0 {
		return ""
	}
	if pxW <= 0 || pxH <= 0 || (pxW == dipW && pxH == dipH) {
		return fmt.Sprintf("%d × %d px", dipW, dipH)
	}
	return fmt.Sprintf("%d × %d (%d × %d px)", dipW, dipH, pxW, pxH)
}

func dimBands(win, hole image.Rectangle) (bands []image.Rectangle, border image.Rectangle) {
	win = win.Canon()
	hole = hole.Canon().Intersect(win)
	if hole.Empty() {
		if !win.Empty() {
			bands = append(bands, win)
		}
		return bands, image.Rectangle{}
	}
	if hole.Min.Y > win.Min.Y {
		bands = append(bands, image.Rect(win.Min.X, win.Min.Y, win.Max.X, hole.Min.Y))
	}
	if hole.Max.Y < win.Max.Y {
		bands = append(bands, image.Rect(win.Min.X, hole.Max.Y, win.Max.X, win.Max.Y))
	}
	if hole.Min.X > win.Min.X {
		bands = append(bands, image.Rect(win.Min.X, hole.Min.Y, hole.Min.X, hole.Max.Y))
	}
	if hole.Max.X < win.Max.X {
		bands = append(bands, image.Rect(hole.Max.X, hole.Min.Y, win.Max.X, hole.Max.Y))
	}
	return bands, hole
}

var _ = color.NRGBA{}
