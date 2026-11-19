package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/measure"
)

const (
	collapsedPillW  = 176
	collapsedPillH  = 32
	maxClosedPinCap = 10
)

type pinWin struct {
	win          *mygo.Window
	baseImg      *image.NRGBA
	baseBmp      *ui.Bitmap
	img          *image.NRGBA
	bmp          *ui.Bitmap
	png          []byte
	annot        *annotate.Canvas
	origW        int
	origH        int
	curW         int
	curH         int
	zoom         float64
	opacity      int
	prevOpacity  int
	clickThrough bool
	collapsed    bool
	rulerMode    bool
	editMode     bool
	annotDrag    bool
	annotEditDrg bool
	hoverX       float64
	hoverY       float64
	hasHover     bool
}

type closedPinRecord struct {
	img     *image.NRGBA
	png     []byte
	at      image.Point
	origW   int
	origH   int
	zoom    float64
	opacity int
}

func (a *app) ShowPin(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int) {
	a.showPinWithState(img, pngBytes, at, dipW, dipH, 1.0, 100)
}

func (a *app) showPinWithState(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int, zoom float64, opacity int) {
	if img == nil || dipW < 1 || dipH < 1 {
		return
	}
	if zoom <= 0 {
		zoom = 1.0
	}
	if opacity <= 0 || opacity > 100 {
		opacity = 100
	}
	curW := max(24, int(math.Round(float64(dipW)*zoom)))
	curH := max(24, int(math.Round(float64(dipH)*zoom)))

	baseBmp := ui.NewBitmap(img)
	p := &pinWin{
		baseImg:     img,
		baseBmp:     baseBmp,
		img:         img,
		bmp:         baseBmp,
		png:         append([]byte(nil), pngBytes...),
		annot:       annotate.NewCanvas(),
		origW:       dipW,
		origH:       dipH,
		curW:        curW,
		curH:        curH,
		zoom:        zoom,
		opacity:     opacity,
		prevOpacity: opacity,
	}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "贴图",
		Width:           curW,
		Height:          curH,
		X:               40,
		Y:               40,
		Hidden:          true,
		Frameless:       true,
		Transparent:     true,
		Panel:           true,
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableResize:   true,
		DisableMaximize: true,
		Content:         ui.View(func(c *ui.Context) { a.pinView(c, p) }),
	})
	preparePinWindow(p.win)
	p.win.SetBounds(mygo.Rectangle{X: at.X, Y: at.Y, Width: curW, Height: curH})
	if opacity < 100 {
		p.win.SetOpacity(float64(opacity) / 100)
	}
	p.win.OnClose(func(*mygo.CloseEvent) {
		if a.quitting || p.win == nil || p.win.IsDestroyed() {
			return
		}
		b := p.win.Bounds()
		a.pushClosedPin(closedPinRecord{
			img:     p.img,
			png:     p.png,
			at:      image.Pt(b.X, b.Y),
			origW:   p.origW,
			origH:   p.origH,
			zoom:    p.zoom,
			opacity: p.opacity,
		})
	})
	p.win.OnClosed(func() {
		a.dropPin(p)
		if !a.quitting {
			a.refreshMenu()
		}
	})
	a.pins = append(a.pins, p)
	p.win.ShowInactive()
	a.refreshMenu()
}

func (a *app) pinView(c *ui.Context, p *pinWin) {
	c.Root().Background(ui.Transparent)
	esc := c.Shortcut(0, ui.KeyEscape)
	enter := c.Shortcut(0, ui.KeyEnter)
	cmdW := c.Shortcut(ui.Cmd, ui.KeyW)
	cmdC := c.Shortcut(ui.Cmd, ui.KeyC)
	cmdS := c.Shortcut(ui.Cmd, ui.KeyS)
	cmdZ := c.Shortcut(ui.Cmd, ui.KeyZ)
	cmdShiftZ := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ)
	delKey := c.Shortcut(0, ui.KeyBackspace) || c.Shortcut(0, ui.KeyDelete)
	keyE := c.Shortcut(0, ui.KeyE)
	keyO := c.Shortcut(0, ui.KeyO)
	keyB := c.Shortcut(0, ui.KeyB)
	keyP := c.Shortcut(0, ui.KeyP)
	keyM := c.Shortcut(0, ui.KeyM)
	keyR := c.Shortcut(0, ui.KeyR)

	if p.win != nil {
		switch {
		case esc:
			if p.editMode {
				p.finishPinEdit()
				return
			}
			if p.rulerMode {
				p.rulerMode = false
				p.win.Invalidate()
				return
			}
			go mygo.RunOnMain(func() {
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Close()
				}
			})
			return
		case enter && p.editMode:
			p.finishPinEdit()
			return
		case cmdZ && p.editMode:
			if p.annot.Undo() {
				p.win.Invalidate()
			}
			return
		case cmdShiftZ && p.editMode:
			if p.annot.Redo() {
				p.win.Invalidate()
			}
			return
		case delKey && p.editMode:
			if p.annot.DeleteSelected() {
				p.win.Invalidate()
			}
			return
		case keyE:
			if !p.collapsed {
				p.togglePinEdit()
			}
			return
		case cmdW:
			go mygo.RunOnMain(func() {
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Close()
				}
			})
			return
		case cmdC:
			if len(p.png) > 0 {
				pngCopy := p.png
				go mygo.RunOnMain(func() { _ = mygo.Clipboard.WriteImage(pngCopy) })
			}
		case cmdS:
			if len(p.png) > 0 {
				a.SavePNG(p.png)
			}
		case keyO:
			a.recognizePin(p)
		case keyB:
			a.beautifyPinToClipboard(p)
		case keyP:
			go mygo.RunOnMain(func() { a.togglePinPassthrough(p) })
		case keyM:
			go mygo.RunOnMain(func() { p.toggleCollapsed() })
		case keyR:
			if !p.collapsed {
				p.rulerMode = !p.rulerMode
				p.win.Invalidate()
			}
		}
	}

	if p.collapsed {
		a.paintCollapsedPill(c, p)
		return
	}

	w := float32(p.curW)
	h := float32(p.curH)
	box := ui.Box(c).Size(w, h)
	showHoverBar := box.Hovered() && !p.rulerMode && !p.editMode && !p.clickThrough && p.curW >= 156 && p.curH >= 64
	if p.rulerMode || (p.editMode && p.annot.Tool != annotate.ToolNone) {
		box.Cursor(ui.CursorCrosshair)
	}
	toOrig := func(ev ui.InputEvent) (float64, float64) {
		z := max(0.05, p.zoom)
		ox := max(0, min(float64(p.origW), float64(ev.X)/z))
		oy := max(0, min(float64(p.origH), float64(ev.Y)/z))
		return ox, oy
	}
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerMove:
			p.hoverX, p.hoverY, p.hasHover = float64(ev.X), float64(ev.Y), true
			if p.editMode && (p.annotDrag || p.annotEditDrg) {
				ox, oy := toOrig(ev)
				if p.annotEditDrg {
					p.annot.MoveEdit(ox, oy)
				} else {
					p.annot.MovePointer(ox, oy)
				}
				return true
			}
			return p.rulerMode
		case ui.InputPointerDown:
			if ev.Button == 1 || (ev.Button == 0 && ev.Mods == ui.Ctrl) {
				if p.editMode && p.annot.Tool != annotate.ToolNone {
					p.annot.CancelPointer()
					p.annot.Tool = annotate.ToolNone
					return true
				}
				go mygo.RunOnMain(func() { a.popupPinMenu(p) })
				return true
			}
			if ev.Button == 0 && p.editMode {
				ox, oy := toOrig(ev)
				if _, part := p.annot.HitTest(ox, oy); part != annotate.EditNone && (p.annot.Tool == annotate.ToolNone || part == annotate.EditP0 || part == annotate.EditP1) {
					if p.annot.BeginEdit(ox, oy) {
						p.annotEditDrg = true
						return true
					}
				}
				if p.annot.Tool == annotate.ToolStep {
					p.annot.BeginPointer(ox, oy)
					return true
				}
				if p.annot.BeginPointer(ox, oy) {
					p.annotDrag = true
					return true
				}
				p.annot.ClearSelection()
				return true
			}
			if ev.Button == 0 && p.rulerMode {
				p.hoverX, p.hoverY, p.hasHover = float64(ev.X), float64(ev.Y), true
				if sp, ok := p.sampleRuler(); ok {
					txt := fmt.Sprintf("%d × %d px", sp.WidthPx(), sp.HeightPx())
					go mygo.RunOnMain(func() { mygo.Clipboard.WriteText(txt) })
				}
				return true
			}
		case ui.InputPointerUp:
			if ev.Button == 0 && p.editMode && (p.annotDrag || p.annotEditDrg) {
				ox, oy := toOrig(ev)
				if p.annotEditDrg {
					p.annot.MoveEdit(ox, oy)
					p.annot.EndEdit()
					p.annotEditDrg = false
				} else {
					p.annot.EndPointer(ox, oy)
					p.annotDrag = false
				}
				return true
			}
		case ui.InputScroll:
			if ev.DY == 0 {
				return false
			}
			if ev.Mods&(ui.Shift|ui.Alt) != 0 {
				delta := int(math.Copysign(5, -float64(ev.DY)))
				go mygo.RunOnMain(func() {
					next := max(15, min(100, p.opacity+delta))
					if next != p.opacity {
						p.opacity = next
						if p.win != nil && !p.win.IsDestroyed() {
							p.win.SetOpacity(float64(next) / 100)
						}
					}
				})
				return true
			}
			factor := 1.0 - float64(ev.DY)*0.004
			if !ev.Precise {
				if ev.DY < 0 {
					factor = 1.08
				} else {
					factor = 1.0 / 1.08
				}
			}
			ax, ay := float64(ev.X), float64(ev.Y)
			go mygo.RunOnMain(func() { p.applyZoom(p.zoom*factor, ax, ay) })
			return true
		}
		return false
	}).Children(func() {
		activeBmp := p.bmp
		if p.editMode && p.baseBmp != nil {
			activeBmp = p.baseBmp
		}
		if activeBmp != nil {
			imgEl := ui.Image(c, activeBmp).Size(w, h)
			if !p.rulerMode && !p.editMode {
				imgEl.DragWindow()
				if imgEl.WindowDoubleClicked() {
					go mygo.RunOnMain(func() {
						if p.win != nil && !p.win.IsDestroyed() {
							p.win.Close()
						}
					})
				}
			}
		}
		if p.editMode {
			p.paintPinEditLayer(c, w, h)
		}
		if p.rulerMode {
			p.paintPinRuler(c, w, h)
		}
		if showHoverBar {
			a.paintPinHoverBar(c, p)
		}
		borderCol := ui.RGBA(128, 128, 128, 0.28)
		if p.editMode {
			borderCol = ui.RGBA(10, 132, 255, 0.85)
		} else if p.clickThrough {
			borderCol = ui.RGBA(0, 168, 255, 0.45)
		}
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).
			Border(1, borderCol).
			IgnoreInput(true)
	})
}

func (a *app) recognizePin(p *pinWin) {
	if p == nil || p.img == nil {
		return
	}
	var anchor image.Rectangle
	if p.win != nil && !p.win.IsDestroyed() {
		b := p.win.Bounds()
		anchor = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	}
	a.RecognizeImageAt(p.img, anchor)
}

func (a *app) paintPinHoverBar(c *ui.Context, p *pinWin) {
	bar := ui.Row(c).Absolute().
		Right(6).Top(6).
		Height(28).
		Padding(2, 4).
		Gap(2).
		AlignItems(ui.Center).
		ZIndex(5).
		Radius(7).
		Background(ui.RGBA(22, 23, 28, 0.92)).
		Border(1, ui.RGBA(255, 255, 255, 0.14)).
		Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.35))
	_ = bar.Clicked()
	bar.Children(func() {
		// 1. 原生文件拖出手柄：按住直接将贴图作为 .png 文件拖进微信/Slack/浏览器/Finder！
		if len(p.png) > 0 {
			dragBtn := ui.Row(c).
				Label("拖出文件").
				Tooltip("按住直接拖出为 PNG 文件（可拖入微信 / Slack / 浏览器）").
				Size(24, 24).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Radius(5).
				Shrink(0).
				Cursor(ui.CursorGrab)
			pngCopy := p.png
			dragBtn.DragDataFrom(func() transfer.Data {
				return tempPNGDragData(pngCopy, "SnipGo-Pin")
			})
			if dragBtn.Hovered() {
				dragBtn.Background(ui.RGBA(255, 255, 255, 0.15))
			}
			dragBtn.Children(func() {
				ui.Icon(c, iconDragFile).FontSize(13.5).TextColor(ui.RGB(118, 210, 255))
			})
		}
		// 2. 贴图原地二次矢量标注
		if pinMiniIconBtn(c, iconEdit, "原地编辑标注 (E)") {
			p.togglePinEdit()
		}
		if pinMiniIconBtn(c, iconOCR, "识图提取文字 (O)") {
			a.recognizePin(p)
		}
		if pinMiniIconBtn(c, iconGhost, "鼠标点击穿透 (P)") {
			go mygo.RunOnMain(func() { a.togglePinPassthrough(p) })
		}
		if pinMiniIconBtn(c, iconCollapse, "折叠为胶囊 (M)") {
			go mygo.RunOnMain(func() { p.toggleCollapsed() })
		}
		if len(p.png) > 0 && pinMiniIconBtn(c, iconCopy, "复制图像 (⌘C)") {
			pngCopy := p.png
			go mygo.RunOnMain(func() { _ = mygo.Clipboard.WriteImage(pngCopy) })
		}
		if pinMiniIconBtn(c, iconClose, "关闭贴图 (Esc)") {
			go mygo.RunOnMain(func() {
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Close()
				}
			})
		}
	})
}

func (p *pinWin) togglePinEdit() {
	if p.editMode {
		p.finishPinEdit()
		return
	}
	p.rulerMode = false
	p.editMode = true
	if p.annot.Tool == annotate.ToolNone && !p.annot.HasItems() {
		p.annot.Tool = annotate.ToolArrow
	}
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Focus()
		p.win.Invalidate()
	}
}

func (p *pinWin) finishPinEdit() {
	p.editMode = false
	p.annotDrag = false
	p.annotEditDrg = false
	p.annot.CancelPointer()
	p.annot.ClearSelection()
	if p.baseImg == nil {
		return
	}
	items := p.annot.Items()
	if len(items) == 0 {
		p.img = p.baseImg
		p.bmp = p.baseBmp
	} else {
		b := p.baseImg.Bounds()
		merged := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(merged, merged.Bounds(), p.baseImg, b.Min, draw.Src)
		annotate.Render(merged, image.Rect(0, 0, p.origW, p.origH), items)
		p.img = merged
		p.bmp = ui.NewBitmap(merged)
	}
	var buf bytes.Buffer
	if png.Encode(&buf, p.img) == nil {
		p.png = buf.Bytes()
	}
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

func (p *pinWin) paintPinEditLayer(c *ui.Context, w, h float32) {
	z := float32(max(0.05, p.zoom))
	items := p.annot.Items()
	selIdx := p.annot.Selected
	if d := p.annot.Drawing(); d != nil {
		items = append(items, *d)
	}
	if len(items) > 0 {
		// 按当前贴图缩放比映射标注坐标预览。
		scaled := make([]annotate.Item, len(items))
		for i, it := range items {
			it.P0.X *= float64(z)
			it.P0.Y *= float64(z)
			it.P1.X *= float64(z)
			it.P1.Y *= float64(z)
			it.Stroke *= float64(z)
			if len(it.Points) > 0 {
				pts := make([]annotate.PointF, len(it.Points))
				for j, pt := range it.Points {
					pts[j] = annotate.PointF{X: pt.X * float64(z), Y: pt.Y * float64(z)}
				}
				it.Points = pts
			}
			scaled[i] = it
		}
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				paintCanvasAnnotations(pt, r, scaled, selIdx, 0, 0, r.X, r.Y)
			})
	}

	// 底部内嵌紧凑标注工具栏
	bar := ui.Row(c).Absolute().
		Right(6).Bottom(6).
		Height(32).
		Padding(3, 5).
		Gap(2).
		AlignItems(ui.Center).
		ZIndex(8).
		Radius(8).
		Background(ui.RGBA(20, 21, 26, 0.95)).
		Border(1, ui.RGBA(255, 255, 255, 0.16))
	_ = bar.Clicked()
	bar.Children(func() {
		for i, t := range [...]struct {
			ic   *ui.SVG
			tip  string
			tool annotate.Tool
		}{
			{iconRect, "矩形", annotate.ToolRect},
			{iconEllipse, "椭圆", annotate.ToolEllipse},
			{iconArrow, "箭头", annotate.ToolArrow},
			{iconHighlighter, "荧光笔", annotate.ToolPen},
			{iconStep, "序号", annotate.ToolStep},
			{iconMosaic, "马赛克", annotate.ToolMosaic},
			{iconErase, "无痕擦除", annotate.ToolErase},
		} {
			active := p.annot.Tool == t.tool
			if iconToolBtn(c.Key(900+i), t.ic, t.tip, t.tip, active, false) {
				if active {
					p.annot.Tool = annotate.ToolNone
				} else {
					p.annot.Tool = t.tool
				}
				if p.win != nil {
					p.win.Invalidate()
				}
			}
		}
		canUndo := p.annot.CanUndo()
		if iconToolBtn(c, iconUndo, "撤销贴图标注", "撤销 (⌘Z)", false, !canUndo) && canUndo {
			p.annot.Undo()
			if p.win != nil {
				p.win.Invalidate()
			}
		}
		if miniPillBtn(c, iconCheck, "完成", true) {
			p.finishPinEdit()
		}
	})
}

func pinMiniIconBtn(c *ui.Context, ic *ui.SVG, tip string) bool {
	btn := ui.Row(c).
		Tooltip(tip).
		Size(24, 24).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Radius(5).
		Shrink(0).
		Cursor(ui.CursorPointer)
	if btn.Pressed() {
		btn.Scale(0.92, 0.92)
	}
	if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.15))
	}
	btn.Children(func() {
		ui.Icon(c, ic).FontSize(13.5).TextColor(ui.RGBA(242, 244, 248, 0.92))
	})
	return btn.Clicked()
}

func (a *app) paintCollapsedPill(c *ui.Context, p *pinWin) {
	pill := ui.Row(c).
		Size(collapsedPillW, collapsedPillH).
		Padding(0, 10).
		Gap(6).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Radius(8).
		Background(ui.RGBA(26, 27, 32, 0.94)).
		Border(1, ui.RGBA(0, 168, 255, 0.65)).
		DragWindow()
	if pill.WindowDoubleClicked() {
		go mygo.RunOnMain(func() { p.toggleCollapsed() })
	}
	pill.Children(func() {
		ui.Text(c, fmt.Sprintf("贴图 %d×%d", p.origW, p.origH)).
			FontSize(11.5).
			FontFeatures("tnum").
			TextColor(ui.RGB(240, 242, 246))
		expandBtn := ui.Row(c).
			Height(20).
			Padding(0, 6).
			AlignItems(ui.Center).
			Radius(4).
			Background(ui.RGBA(0, 168, 255, 0.28)).
			Cursor(ui.CursorPointer)
		expandBtn.Children(func() {
			ui.Text(c, "展开").FontSize(11).TextColor(ui.RGB(130, 218, 255))
		})
		if expandBtn.Clicked() {
			go mygo.RunOnMain(func() { p.toggleCollapsed() })
		}
	})
}

func (p *pinWin) sampleRuler() (measure.Span, bool) {
	if p.img == nil || !p.hasHover || p.curW <= 0 || p.curH <= 0 {
		return measure.Span{}, false
	}
	ib := p.img.Bounds()
	px := ib.Min.X + int(math.Floor(p.hoverX*float64(ib.Dx())/float64(p.curW)))
	py := ib.Min.Y + int(math.Floor(p.hoverY*float64(ib.Dy())/float64(p.curH)))
	return measure.Cast(p.img, px, py, measure.DefaultTolerance)
}

func (p *pinWin) paintPinRuler(c *ui.Context, w, h float32) {
	sp, ok := p.sampleRuler()
	if !ok {
		return
	}
	ib := p.img.Bounds()
	sx := float32(p.curW) / float32(max(1, ib.Dx()))
	sy := float32(p.curH) / float32(max(1, ib.Dy()))
	cx := float32(p.hoverX)
	cy := float32(p.hoverY)
	x0 := float32(sp.Left-ib.Min.X) * sx
	x1 := float32(sp.Right+1-ib.Min.X) * sx
	y0 := float32(sp.Top-ib.Min.Y) * sy
	y1 := float32(sp.Bottom+1-ib.Min.Y) * sy

	ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			col := ui.RGB(255, 69, 58)
			pt.Line(r.X+x0, r.Y+cy, r.X+x1, r.Y+cy, 1.5, col)
			pt.Line(r.X+cx, r.Y+y0, r.X+cx, r.Y+y1, 1.5, col)
			pt.Line(r.X+x0, r.Y+cy-4, r.X+x0, r.Y+cy+4, 1.5, col)
			pt.Line(r.X+x1, r.Y+cy-4, r.X+x1, r.Y+cy+4, 1.5, col)
			pt.Line(r.X+cx-4, r.Y+y0, r.X+cx+4, r.Y+y0, 1.5, col)
			pt.Line(r.X+cx-4, r.Y+y1, r.X+cx+4, r.Y+y1, 1.5, col)
		})

	txt := fmt.Sprintf("↔ %d px  ↕ %d px", sp.WidthPx(), sp.HeightPx())
	bx := max(float32(4), min(cx+12, w-148))
	by := max(float32(4), min(cy+12, h-28))
	ui.Box(c).Absolute().Left(bx).Top(by).
		Padding(3, 7).Radius(4).
		Background(ui.RGBA(20, 20, 24, 0.9)).
		IgnoreInput(true).
		Children(func() {
			ui.Text(c, txt).FontSize(11).FontFeatures("tnum").TextColor(ui.RGB(255, 255, 255))
		})
}

func (p *pinWin) toggleCollapsed() {
	if p.win == nil || p.win.IsDestroyed() {
		return
	}
	b := p.win.Bounds()
	p.collapsed = !p.collapsed
	if p.collapsed {
		p.rulerMode = false
		p.win.SetBounds(mygo.Rectangle{X: b.X, Y: b.Y, Width: collapsedPillW, Height: collapsedPillH})
	} else {
		p.win.SetBounds(mygo.Rectangle{X: b.X, Y: b.Y, Width: p.curW, Height: p.curH})
	}
	p.win.Invalidate()
}

func (a *app) togglePinPassthrough(p *pinWin) {
	if p == nil || p.win == nil || p.win.IsDestroyed() {
		return
	}
	p.clickThrough = !p.clickThrough
	if p.clickThrough {
		p.prevOpacity = p.opacity
		if p.opacity > 65 {
			p.opacity = 55
			p.win.SetOpacity(0.55)
		}
		p.win.SetIgnoreMouseEvents(true)
	} else {
		p.win.SetIgnoreMouseEvents(false)
		if p.prevOpacity > 0 {
			p.opacity = p.prevOpacity
			p.win.SetOpacity(float64(p.opacity) / 100)
		}
	}
	p.win.Invalidate()
	a.refreshMenu()
}

func (a *app) passthroughPinCount() int {
	n := 0
	for _, p := range a.pins {
		if p.clickThrough {
			n++
		}
	}
	return n
}

func (a *app) restorePassthroughPins() {
	for _, p := range a.pins {
		if p.clickThrough && p.win != nil && !p.win.IsDestroyed() {
			p.clickThrough = false
			p.win.SetIgnoreMouseEvents(false)
			if p.prevOpacity > 0 {
				p.opacity = p.prevOpacity
				p.win.SetOpacity(float64(p.opacity) / 100)
			}
			p.win.Invalidate()
		}
	}
	a.refreshMenu()
}

func (a *app) beautifyPinToClipboard(p *pinWin) {
	if p == nil || p.img == nil {
		return
	}
	src := p.img
	go func() {
		card := beautify.RenderCard(src, 2.0, beautify.PresetOcean)
		var buf bytes.Buffer
		if err := png.Encode(&buf, card); err != nil {
			return
		}
		data := buf.Bytes()
		mygo.RunOnMain(func() {
			_ = mygo.Clipboard.WriteImage(data)
		})
	}()
}

func (a *app) pushClosedPin(rec closedPinRecord) {
	if rec.img == nil {
		return
	}
	if len(a.closedPins) >= maxClosedPinCap {
		a.closedPins = a.closedPins[1:]
	}
	a.closedPins = append(a.closedPins, rec)
}

// RestoreLastPin 恢复最近一次关闭的贴图窗口（支持连续恢复多张）。
func (a *app) RestoreLastPin() {
	if len(a.closedPins) == 0 {
		return
	}
	last := a.closedPins[len(a.closedPins)-1]
	a.closedPins = a.closedPins[:len(a.closedPins)-1]
	a.showPinWithState(last.img, last.png, last.at, last.origW, last.origH, last.zoom, last.opacity)
}

func (p *pinWin) applyZoom(targetZoom, anchorX, anchorY float64) {
	if p.win == nil || p.win.IsDestroyed() || p.collapsed {
		return
	}
	nextZoom := max(0.2, min(5.0, targetZoom))
	newW := max(24, int(math.Round(float64(p.origW)*nextZoom)))
	newH := max(24, int(math.Round(float64(p.origH)*nextZoom)))
	if newW == p.curW && newH == p.curH {
		return
	}
	b := p.win.Bounds()
	rx := anchorX / float64(max(1, p.curW))
	ry := anchorY / float64(max(1, p.curH))
	newX := b.X + int(math.Round(anchorX-rx*float64(newW)))
	newY := b.Y + int(math.Round(anchorY-ry*float64(newH)))
	p.zoom = nextZoom
	p.curW = newW
	p.curH = newH
	p.win.SetBounds(mygo.Rectangle{X: newX, Y: newY, Width: newW, Height: newH})
	p.win.Invalidate()
}

func (a *app) popupPinMenu(p *pinWin) {
	if p == nil || p.win == nil || p.win.IsDestroyed() {
		return
	}
	opacityItems := make([]*mygo.MenuItem, 0, 4)
	for _, pct := range []int{100, 80, 55, 30} {
		pct := pct
		opacityItems = append(opacityItems, &mygo.MenuItem{
			Label:   fmt.Sprintf("%d%%", pct),
			Type:    mygo.MenuItemCheckbox,
			Checked: p.opacity == pct,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if p.win != nil && !p.win.IsDestroyed() {
					p.opacity = pct
					p.win.SetOpacity(float64(pct) / 100)
				}
			},
		})
	}
	zoomPct := int(math.Round(p.zoom * 100))
	moreItems := []*mygo.MenuItem{
		{
			Label:       "像素测量尺",
			Accelerator: "R",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.rulerMode,
			Disabled:    p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.rulerMode = !p.rulerMode
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Invalidate()
				}
			},
		},
		{
			Label:       "生成美化卡片并复制",
			Accelerator: "B",
			Disabled:    p.img == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.beautifyPinToClipboard(p)
			},
		},
		{
			Label:    "恢复原始尺寸 (100%)",
			Badge:    fmt.Sprintf("%d%%", zoomPct),
			Disabled: zoomPct == 100 || p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.applyZoom(1.0, float64(p.curW)/2, float64(p.curH)/2)
			},
		},
	}

	items := []*mygo.MenuItem{
		{
			Label:       "复制图像",
			Accelerator: "Cmd+C",
			Disabled:    len(p.png) == 0,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				_ = mygo.Clipboard.WriteImage(p.png)
			},
		},
		{
			Label:       "原地编辑标注",
			Accelerator: "E",
			Disabled:    p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.togglePinEdit()
			},
		},
		{
			Label:       "识图提取文字",
			Accelerator: "O",
			Disabled:    p.img == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.recognizePin(p)
			},
		},
		{
			Label:       "保存为 PNG...",
			Accelerator: "Cmd+S",
			Disabled:    len(p.png) == 0,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.SavePNG(p.png)
			},
		},
		mygo.Separator(),
		{
			Label:       "鼠标穿透比对",
			Accelerator: "P",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.clickThrough,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.togglePinPassthrough(p)
			},
		},
		{
			Label:       "折叠为迷你胶囊",
			Accelerator: "M",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.toggleCollapsed()
			},
		},
		{
			Label:   "工具与缩放",
			Submenu: moreItems,
		},
		{
			Label:   "不透明度",
			Submenu: opacityItems,
		},
		mygo.Separator(),
		{
			Label:       "关闭",
			Accelerator: "Esc",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if p.win != nil {
					p.win.Close()
				}
			},
		},
	}
	if len(a.pins) > 1 {
		items = append(items, &mygo.MenuItem{
			Label: "关闭全部贴图",
			Click: func(*mygo.MenuItem, *mygo.Window) { a.closeAllPins() },
		})
	}
	mygo.NewMenu(items).Popup(p.win)
}

// PasteClipboardPin 把剪贴板里的图像、图片文件、代码/文字或 #RRGGBB 色号直接转为悬浮贴图。
func (a *app) PasteClipboardPin() {
	cursor := mygo.Screen.CursorScreenPoint()
	scale := 2.0
	for _, d := range mygo.Screen.Displays() {
		db := image.Rect(d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.Width, d.Bounds.Y+d.Bounds.Height)
		if image.Pt(cursor.X, cursor.Y).In(db) && d.ScaleFactor > 0 {
			scale = d.ScaleFactor
			break
		}
	}

	nrgba, err := readClipboardContentAsNRGBA(scale)
	if err != nil {
		log.Printf("PasteClipboardPin: %v", err)
		a.sessionStatus(err.Error())
		go a.showErrorAlert("剪贴板贴图失败", err.Error())
		return
	}
	b := nrgba.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, nrgba); err != nil {
		log.Printf("PasteClipboardPin encode png: %v", err)
		return
	}
	dipW := max(16, int(math.Round(float64(b.Dx())/scale)))
	dipH := max(16, int(math.Round(float64(b.Dy())/scale)))
	at := image.Pt(cursor.X-dipW/2, cursor.Y-dipH/2)
	a.ShowPin(nrgba, pngBuf.Bytes(), at, dipW, dipH)
}

func readClipboardContentAsNRGBA(scale float64) (*image.NRGBA, error) {
	// 1. 优先读取 Finder / 聊天软件复制的图片文件（避免读到 Finder 的通用文件图标 TIFF）。
	files, _ := mygo.Clipboard.ReadFiles()
	for _, p := range files {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif":
			if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
				if img, _, decErr := image.Decode(bytes.NewReader(data)); decErr == nil {
					return toNRGBA(img), nil
				}
			}
		}
	}
	// 2. 剪贴板位图图像。
	if raw := mygo.Clipboard.ReadImage(); len(raw) > 0 {
		decoded, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("无法解析剪贴板图像: %v", err)
		}
		return toNRGBA(decoded), nil
	}
	// 3. 剪贴板文本 / 代码片段 / #HEX 色号直接渲染为高分屏悬浮便签或色卡！
	if txt := strings.TrimSpace(mygo.Clipboard.ReadText()); txt != "" {
		if card := beautify.RenderTextPin(txt, scale); card != nil {
			return card, nil
		}
	}
	return nil, fmt.Errorf("剪贴板为空（支持图片、图片文件、文字、代码或 #HEX 色号贴图）")
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

func (a *app) sessionStatus(msg string) {
	a.extraStatus = msg
	a.refreshMenu()
}

func (a *app) closeAllPins() {
	pins := append([]*pinWin(nil), a.pins...)
	for _, p := range pins {
		if p.win != nil {
			p.win.Close()
		}
	}
}

func (a *app) dropPin(p *pinWin) {
	for i, q := range a.pins {
		if q == p {
			a.pins = append(a.pins[:i], a.pins[i+1:]...)
			return
		}
	}
}
