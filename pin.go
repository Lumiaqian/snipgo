package main

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/measure"
	"snipgo/internal/pinops"
	"snipgo/internal/vision"
)

const (
	collapsedPillW  = 176
	collapsedPillH  = 32
	maxClosedPinCap = 10
)

type pinWin struct {
	win            *mygo.Window
	baseImg        *image.NRGBA
	baseBmp        *ui.Bitmap
	img            *image.NRGBA
	bmp            *ui.Bitmap
	png            []byte
	annot          *annotate.Canvas
	origW          int
	origH          int
	curW           int
	curH           int
	zoom           float64
	opacity        int
	prevOpacity    int
	clickThrough   bool
	collapsed      bool
	rulerMode      bool
	editMode       bool
	cutout         bool
	cutting        bool
	compareMode    bool
	compareDiff    bool
	comparePeer    *image.NRGBA
	comparePeerBmp *ui.Bitmap
	compareDiffRes pinops.DiffResult
	compareDiffBmp *ui.Bitmap
	compareSplit   float64
	compareDrag    bool
	annotDrag      bool
	annotEditDrg   bool
	hoverX         float64
	hoverY         float64
	hasHover       bool
}

type closedPinRecord struct {
	img     *image.NRGBA
	png     []byte
	at      image.Point
	origW   int
	origH   int
	zoom    float64
	opacity int
	cutout  bool
}

func (a *app) ShowPin(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int) {
	a.showPinWithState(img, pngBytes, at, dipW, dipH, 1.0, 100, false)
}

func (a *app) showPinWithState(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int, zoom float64, opacity int, cutout bool) {
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
		baseImg:      img,
		baseBmp:      baseBmp,
		img:          img,
		bmp:          baseBmp,
		png:          append([]byte(nil), pngBytes...),
		annot:        annotate.NewCanvas(),
		origW:        dipW,
		origH:        dipH,
		curW:         curW,
		curH:         curH,
		zoom:         zoom,
		opacity:      opacity,
		prevOpacity:  opacity,
		cutout:       cutout,
		compareSplit: 0.5,
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
	applyWindowAppearance(p.win, a.cfg.ThemeAppearance)
	p.win.SetBounds(mygo.Rectangle{X: at.X, Y: at.Y, Width: curW, Height: curH})
	if opacity < 100 {
		p.win.SetOpacity(float64(opacity) / 100)
	}
	p.win.OnClose(func(*mygo.CloseEvent) {
		if a.quitting || p.win == nil || p.win.IsDestroyed() {
			return
		}
		b := p.win.Bounds()
		// 编辑中直接关闭时，可恢复记录也要带上未完成的标注
		pngBytes := p.exportPNG()
		a.pushClosedPin(closedPinRecord{
			img:     p.img,
			png:     pngBytes,
			at:      image.Pt(b.X, b.Y),
			origW:   p.origW,
			origH:   p.origH,
			zoom:    p.zoom,
			opacity: p.opacity,
			cutout:  p.cutout,
		})
	})
	p.win.OnMove(func() {
		a.snapPinWindow(p)
	})
	p.win.OnClosed(func() {
		a.dropPin(p)
		if !a.quitting {
			a.refreshMenu()
		}
	})
	a.pins = append(a.pins, p)
	p.win.ShowInactive()
	p.refreshShadowSoon()
	a.refreshMenu()
}

func (p *pinWin) refreshShadowSoon() {
	if p == nil || p.win == nil || p.win.IsDestroyed() || !mygo.App.IsReady() {
		return
	}
	win := p.win
	invalidatePinShadow(win)
	go func() {
		time.Sleep(45 * time.Millisecond)
		mygo.RunOnMain(func() {
			if !win.IsDestroyed() {
				invalidatePinShadow(win)
			}
		})
	}()
}

func (a *app) pinView(c *ui.Context, p *pinWin) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		esc := c.Shortcut(0, ui.KeyEscape)
		enter := c.Shortcut(0, ui.KeyEnter)
		cmdQ := c.Shortcut(ui.Cmd, ui.KeyQ)
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
		keyK := c.Shortcut(0, ui.KeyK)
		keyD := c.Shortcut(0, ui.KeyD)
		tabKey := c.Shortcut(0, ui.KeyTab)

		if cmdQ {
			go mygo.RunOnMain(func() { mygo.App.Quit() })
			return
		}

		if p.win != nil {
			switch {
			case esc:
				if p.compareMode {
					p.exitCompareMode()
					return
				}
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
			case tabKey && p.compareMode:
				p.compareDiff = !p.compareDiff
				p.win.Invalidate()
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
					if p.compareMode {
						p.exitCompareMode()
					}
					p.togglePinEdit()
				}
				return
			case keyK:
				if !p.collapsed && !p.cutting {
					a.cutoutPinInPlace(p)
				}
				return
			case keyD:
				if !p.collapsed {
					a.togglePinCompare(p)
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
				if p.compareMode {
					a.copyCompareComposite(p)
					return
				}
				if pngCopy := p.exportPNG(); len(pngCopy) > 0 {
					go mygo.RunOnMain(func() { _ = mygo.Clipboard.WriteImage(pngCopy) })
				}
			case cmdS:
				if data := p.exportPNG(); len(data) > 0 {
					a.SavePNG(data)
				}
			case keyO:
				a.recognizePin(p)
			case keyB:
				a.openPinTemplate(p)
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
		hovered := box.Hovered()
		showHoverBar := hovered && !p.rulerMode && !p.editMode && !p.compareMode && !p.clickThrough && p.curW >= 156 && p.curH >= 64
		if p.compareMode && !p.compareDiff {
			box.Cursor(ui.CursorResizeEW)
		} else if p.rulerMode || (p.editMode && p.annot.Tool != annotate.ToolNone) {
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
				if p.compareMode && !p.compareDiff && p.compareDrag && p.curW > 0 {
					p.compareSplit = max(0.02, min(0.98, float64(ev.X)/float64(p.curW)))
					return true
				}
				if p.editMode && (p.annotDrag || p.annotEditDrg) {
					ox, oy := toOrig(ev)
					if p.annotEditDrg {
						p.annot.MoveEdit(ox, oy)
					} else {
						p.annot.MovePointer(ox, oy)
					}
					return true
				}
				return p.rulerMode || (p.editMode && p.annot.Tool == annotate.ToolLoupe)
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
				if ev.Button == 0 && p.compareMode && !p.compareDiff && p.curW > 0 {
					p.compareDrag = true
					p.compareSplit = max(0.02, min(0.98, float64(ev.X)/float64(p.curW)))
					return true
				}
				if ev.Button == 0 && p.editMode {
					ox, oy := toOrig(ev)
					if idx, part := p.annot.HitTest(ox, oy); part != annotate.EditNone {
						items := p.annot.Items()
						hitExistingLoupe := p.annot.Tool == annotate.ToolLoupe && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe
						if p.annot.Tool == annotate.ToolNone || part == annotate.EditP0 || part == annotate.EditP1 || hitExistingLoupe {
							if p.annot.BeginEdit(ox, oy) {
								p.annotEditDrg = true
								return true
							}
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
				if ev.Button == 0 && p.compareDrag {
					p.compareDrag = false
					return true
				}
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
			if p.compareMode {
				a.paintPinCompareLayer(c, p, w, h)
			} else {
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
					p.paintPinEditLayer(c, w, h, hovered)
				}
				if p.rulerMode {
					p.paintPinRuler(c, w, h)
				}
				if showHoverBar {
					a.paintPinHoverBar(c, p)
				}
			}
			pal := uiPalette(c)
			borderCol := ui.Transparent
			switch {
			case p.compareMode:
				borderCol = ui.RGBA(255, 69, 132, 0.85)
			case p.editMode:
				borderCol = pal.Accent.Alpha(0.85)
			case p.clickThrough:
				borderCol = pal.Accent.Alpha(0.45)
			case hovered:
				borderCol = pal.Accent.Alpha(0.55)
			}
			borderBox := ui.Box(c.Key("pin-border")).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true)
			animBd := borderBox.AnimateColor("pin-bd", borderCol, colorMorph)
			if animBd.A > 0 {
				borderBox.Border(1, animBd)
			}
		})
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
	pal := uiPalette(c)
	bar := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Right(8).Top(8).
			Height(32).
			Padding(0, 6).
			Gap(2).
			AlignItems(ui.Center).
			ZIndex(5),
		16,
		false,
	)
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
				Radius(12).
				Shrink(0).
				Cursor(ui.CursorGrab)
			dragBtn.DragDataFrom(func() transfer.Data {
				// 拖出瞬间才导出，编辑中也包含未完成的标注
				return tempPNGDragData(p.exportPNG(), "SnipGo-Pin")
			})
			if dragBtn.Hovered() {
				liquidSecondaryChrome(c, dragBtn, true, 12)
			}
			dragBtn.Children(func() {
				ui.Icon(c, iconDragFile).FontSize(13.5).TextColor(pal.Accent)
			})
		}
		// 2. 贴图原地二次矢量标注 / 一键主体抠图 / 套模板 / 像素对比
		if pinMiniIconBtn(c, iconEdit, "原地编辑标注 (E)") {
			p.togglePinEdit()
		}
		if pinMiniIconBtn(c, iconTemplate, "套模板（便签 / 拍立得 / 浏览器…）(B)") {
			a.openPinTemplate(p)
		}
		if pinMiniIconBtn(c, iconCutout, "离线 AI 主体抠图去背景 (K)") && !p.cutting {
			a.cutoutPinInPlace(p)
		}
		if len(a.pins) >= 2 && pinMiniIconBtn(c, iconCompare, "拉帘对比 / 像素 Diff (D)") {
			a.togglePinCompare(p)
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
			pngCopy := p.exportPNG()
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
	p.flattenEdits()
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

// flattenEdits 把 baseImg 与当前标注合成为 img/bmp/png，不退出编辑模式。
func (p *pinWin) flattenEdits() {
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
}

// exportPNG 返回用于复制/保存/拖出的 PNG；编辑模式下先合成进行中的标注。
func (p *pinWin) exportPNG() []byte {
	if p.editMode {
		p.flattenEdits()
	}
	return p.png
}

func (p *pinWin) paintPinEditLayer(c *ui.Context, w, h float32, hovered bool) {
	z := float32(max(0.05, p.zoom))
	items := p.annot.Items()
	selIdx := p.annot.Selected
	if d := p.annot.Drawing(); d != nil {
		items = append(items, *d)
	}
	var hoverLoupe *annotate.Item
	if hovered && p.hasHover && p.annot.Tool == annotate.ToolLoupe && !p.annotDrag && !p.annotEditDrg {
		ox := max(0, min(float64(p.origW), p.hoverX/float64(z)))
		oy := max(0, min(float64(p.origH), p.hoverY/float64(z)))
		if idx, _ := p.annot.HitTest(ox, oy); !(idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe) {
			zoom := p.annot.LoupeZoom
			if zoom <= 0 {
				zoom = 2.5
			}
			hoverLoupe = &annotate.Item{
				Tool:   annotate.ToolLoupe,
				Color:  p.annot.Color,
				Stroke: p.annot.Stroke,
				P0:     annotate.PointF{X: ox, Y: oy},
				P1:     annotate.PointF{X: ox, Y: oy},
				Zoom:   zoom,
			}
		}
	}
	if len(items) > 0 || hoverLoupe != nil {
		src := canvasPreviewSource{
			bmp:  p.baseBmp,
			img:  p.baseImg,
			rect: image.Rect(0, 0, p.origW, p.origH),
		}
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				paintCanvasAnnotations(pt, r, items, selIdx, 0, 0, r.X, r.Y, z, src, hoverLoupe)
			})
	}

	// 底部内嵌紧凑标注工具栏
	bar := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Right(8).Bottom(8).
			Height(36).
			Padding(0, 6).
			Gap(2).
			AlignItems(ui.Center).
			ZIndex(8),
		18,
		false,
	)
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
			{iconLoupe, "放大镜特写", annotate.ToolLoupe},
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

func (a *app) paintCollapsedPill(c *ui.Context, p *pinWin) {
	pal := uiPalette(c)
	pill := liquidGlassCapsule(
		c,
		ui.Row(c).
			Size(collapsedPillW, collapsedPillH).
			Padding(0, 12).
			Gap(6).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			DragWindow(),
		16,
		true,
	).Border(1, pal.PanelRim)
	if pill.WindowDoubleClicked() {
		go mygo.RunOnMain(func() { p.toggleCollapsed() })
	}
	pill.Children(func() {
		ui.Text(c, fmt.Sprintf("贴图 %d×%d", p.origW, p.origH)).
			FontSize(11.5).
			FontFeatures("tnum").
			TextColor(pal.Text)
		if miniPillBtn(c, nil, "展开", false) {
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
	pal := uiPalette(c)
	liquidGlassCapsule(
		c,
		ui.Box(c).Absolute().Left(bx).Top(by).
			Padding(4, 9).
			IgnoreInput(true),
		12,
		false,
	).Children(func() {
		ui.Text(c, txt).FontSize(11).FontFeatures("tnum").TextColor(pal.Text)
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
	p.refreshShadowSoon()
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

func (a *app) openPinTemplate(p *pinWin) {
	if p == nil || p.img == nil {
		return
	}
	var anchor image.Rectangle
	if p.win != nil && !p.win.IsDestroyed() {
		b := p.win.Bounds()
		anchor = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	} else {
		anchor = image.Rect(0, 0, p.origW, p.origH)
	}
	imgCopy := p.img
	go mygo.RunOnMain(func() {
		a.openTemplateWindow(imgCopy, anchor)
	})
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
	a.showPinWithState(last.img, last.png, last.at, last.origW, last.origH, last.zoom, last.opacity, last.cutout)
}

func (p *pinWin) applyZoom(targetZoom, anchorX, anchorY float64) {
	if p.win == nil || p.win.IsDestroyed() || p.collapsed {
		return
	}
	// 长图贴图初始缩放可能低于 0.2，下限取当前值以免滚轮时跳变
	nextZoom := max(min(0.2, p.zoom), min(5.0, targetZoom))
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
	p.refreshShadowSoon()
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
			Label:       "套模板（便签 / 拍立得 / 浏览器…）",
			Accelerator: "B",
			Disabled:    p.img == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.openPinTemplate(p)
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
				_ = mygo.Clipboard.WriteImage(p.exportPNG())
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
			Label:       "离线 AI 主体抠图（去背景）",
			Accelerator: "K",
			Disabled:    p.collapsed || p.img == nil || p.cutting,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.cutoutPinInPlace(p)
			},
		},
		{
			Label:       "与相邻贴图拉帘对比 / 像素 Diff",
			Accelerator: "D",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.compareMode,
			Disabled:    p.collapsed || (!p.compareMode && len(a.pins) < 2),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.togglePinCompare(p)
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
				a.SavePNG(p.exportPNG())
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
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{
				Label: "横向拼接所有贴图为一张图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.mergeAllPins(true) },
			},
			&mygo.MenuItem{
				Label: "纵向拼接所有贴图为一张图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.mergeAllPins(false) },
			},
			&mygo.MenuItem{
				Label: "关闭全部贴图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.closeAllPins() },
			},
		)
	}
	mygo.NewMenu(items).Popup(p.win)
}

// cutoutSelection 对选区截图执行离线 Vision 前景实例分割，复制透明 PNG 并贴为无边框异形贴纸。
func (a *app) cutoutSelection(img *image.NRGBA, r image.Rectangle) {
	if img == nil || img.Bounds().Empty() {
		return
	}
	scale := 2.0
	if r.Dx() > 0 {
		scale = max(1.0, float64(img.Bounds().Dx())/float64(r.Dx()))
	}
	a.sessionStatus("正在离线 AI 抠取主体…")
	go func() {
		cut, err := vision.Cutout(img)
		if err != nil {
			mygo.RunOnMain(func() {
				a.sessionStatus("抠图失败: " + err.Error())
				go a.showErrorAlert("主体抠图未检测到清晰主体", err.Error())
			})
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, cut); err != nil {
			return
		}
		data := buf.Bytes()
		dipW := max(24, int(math.Round(float64(cut.Bounds().Dx())/scale)))
		dipH := max(24, int(math.Round(float64(cut.Bounds().Dy())/scale)))
		at := image.Pt(r.Min.X+(r.Dx()-dipW)/2, r.Min.Y+(r.Dy()-dipH)/2)
		mygo.RunOnMain(func() {
			_ = mygo.Clipboard.WriteImage(data)
			a.recordHistory(data, cut.Bounds().Dx(), cut.Bounds().Dy())
			a.sessionStatus("主体抠图已复制并贴为透明贴纸")
			a.showPinWithState(cut, data, at, dipW, dipH, 1.0, 100, true)
		})
	}()
}

// cutoutPinInPlace 原地将悬浮贴图主体从背景中抠出，变为无边框透明异形贴纸并同步写入剪贴板。
func (a *app) cutoutPinInPlace(p *pinWin) {
	if p == nil || p.img == nil || p.cutting {
		return
	}
	p.cutting = true
	src := p.img
	scale := max(1.0, float64(src.Bounds().Dx())/float64(max(1, p.origW)))
	a.sessionStatus("正在抠取贴图主体…")
	go func() {
		cut, err := vision.Cutout(src)
		if err != nil {
			mygo.RunOnMain(func() {
				p.cutting = false
				a.sessionStatus("抠图失败: " + err.Error())
				go a.showErrorAlert("主体抠图失败", err.Error())
			})
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, cut); err != nil {
			mygo.RunOnMain(func() { p.cutting = false })
			return
		}
		data := buf.Bytes()
		mygo.RunOnMain(func() {
			p.cutting = false
			if p.win == nil || p.win.IsDestroyed() {
				return
			}
			newOrigW := max(24, int(math.Round(float64(cut.Bounds().Dx())/scale)))
			newOrigH := max(24, int(math.Round(float64(cut.Bounds().Dy())/scale)))
			newCurW := max(24, int(math.Round(float64(newOrigW)*p.zoom)))
			newCurH := max(24, int(math.Round(float64(newOrigH)*p.zoom)))
			b := p.win.Bounds()
			cx := b.X + b.Width/2
			cy := b.Y + b.Height/2
			bmp := ui.NewBitmap(cut)
			p.baseImg = cut
			p.baseBmp = bmp
			p.img = cut
			p.bmp = bmp
			p.png = data
			p.annot.Reset()
			p.cutout = true
			p.origW = newOrigW
			p.origH = newOrigH
			p.curW = newCurW
			p.curH = newCurH
			p.win.SetBounds(mygo.Rectangle{X: cx - newCurW/2, Y: cy - newCurH/2, Width: newCurW, Height: newCurH})
			p.win.Invalidate()
			p.refreshShadowSoon()
			_ = mygo.Clipboard.WriteImage(data)
			a.recordHistory(data, cut.Bounds().Dx(), cut.Bounds().Dy())
			a.sessionStatus("贴图已抠出透明主体并复制")
		})
	}()
}

// togglePinCompare 开关 Before/After 拉帘对比与像素 Diff 热力图模式（自动配对最近/重叠的另一张贴图）。
func (a *app) togglePinCompare(p *pinWin) {
	if p == nil || p.collapsed {
		return
	}
	if p.compareMode {
		p.exitCompareMode()
		return
	}
	peer := a.findComparePeer(p)
	if peer == nil || peer.img == nil || p.img == nil {
		a.sessionStatus("需要至少 2 张贴图才能开启拉帘/像素对比")
		return
	}
	if p.editMode {
		p.finishPinEdit()
	}
	p.rulerMode = false
	p.compareMode = true
	p.compareDiff = false
	p.comparePeer = peer.img
	p.comparePeerBmp = ui.NewBitmap(peer.img)
	p.compareDiffRes = pinops.RenderPixelDiff(peer.img, p.img, 12)
	if p.compareDiffRes.Image != nil {
		p.compareDiffBmp = ui.NewBitmap(p.compareDiffRes.Image)
	}
	if p.compareSplit <= 0.02 || p.compareSplit >= 0.98 {
		p.compareSplit = 0.5
	}
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Focus()
		p.win.Invalidate()
	}
}

func (p *pinWin) exitCompareMode() {
	p.compareMode = false
	p.compareDiff = false
	p.compareDrag = false
	p.comparePeer = nil
	p.comparePeerBmp = nil
	p.compareDiffBmp = nil
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

func (a *app) findComparePeer(self *pinWin) *pinWin {
	if self == nil {
		return nil
	}
	var selfR image.Rectangle
	if self.win != nil && !self.win.IsDestroyed() {
		b := self.win.Bounds()
		selfR = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	}
	var best *pinWin
	bestScore := math.MaxFloat64
	for _, q := range a.pins {
		if q == self || q.collapsed || q.img == nil {
			continue
		}
		score := 0.0
		if q.win != nil && !q.win.IsDestroyed() && !selfR.Empty() {
			qb := q.win.Bounds()
			qr := image.Rect(qb.X, qb.Y, qb.X+qb.Width, qb.Y+qb.Height)
			inter := selfR.Intersect(qr)
			if !inter.Empty() {
				// 有重叠的贴图最优先配对（重叠面积越大越优先）。
				score = -float64(inter.Dx() * inter.Dy())
			} else {
				dx := float64((selfR.Min.X+selfR.Max.X)/2 - (qr.Min.X+qr.Max.X)/2)
				dy := float64((selfR.Min.Y+selfR.Max.Y)/2 - (qr.Min.Y+qr.Max.Y)/2)
				score = math.Hypot(dx, dy)
			}
		}
		if score < bestScore {
			bestScore = score
			best = q
		}
	}
	return best
}

func (a *app) paintPinCompareLayer(c *ui.Context, p *pinWin, w, h float32) {
	if p.compareDiff && p.compareDiffBmp != nil {
		ui.Image(c, p.compareDiffBmp).Size(w, h)
	} else {
		// 右侧底层展示当前贴图 B，左侧按 splitX 裁剪覆盖展示对比贴图 A。
		if p.bmp != nil {
			ui.Image(c, p.bmp).Size(w, h)
		}
		splitX := max(float32(2), min(w-2, float32(p.compareSplit)*w))
		if p.comparePeerBmp != nil {
			ui.Box(c).Absolute().Left(0).Top(0).Size(splitX, h).Clip().IgnoreInput(true).Children(func() {
				ui.Image(c, p.comparePeerBmp).Size(w, h)
			})
		}
		// 垂直拉帘分割线与中央抓手圆钮
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).ZIndex(4).IgnoreInput(true).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				sx := r.X + splitX
				pt.Line(sx, r.Y, sx, r.Y+r.H, 2, ui.RGBA(255, 255, 255, 0.95))
				cy := r.Y + r.H*0.5
				const kr = float32(13)
				pt.Shadow(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, kr, 0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.45))
				pt.Fill(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, ui.RGBA(22, 24, 30, 0.92), kr)
				pt.Stroke(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, ui.RGB(255, 255, 255), kr, 1.5)
				sp := ui.Span{Text: "↔", Size: 12, Weight: 700, Color: ui.RGB(255, 255, 255)}
				tw, th := pt.MeasureText(0, sp)
				pt.RichText(sx-tw*0.5, cy-th*0.5, 0, sp)
			})
		// 左上 / 右上极简角标
		pal := uiPalette(c)
		liquidGlassCapsule(
			c,
			ui.Row(c).Absolute().Left(8).Top(8).Height(20).Padding(0, 8).AlignItems(ui.Center).ZIndex(5).
				IgnoreInput(true),
			10,
			false,
		).Children(func() {
			ui.Text(c, "对比 A").FontSize(10.5).FontWeight(500).NoWrap().TextColor(pal.Text)
		})
		liquidGlassCapsule(
			c,
			ui.Row(c).Absolute().Right(8).Top(8).Height(20).Padding(0, 8).AlignItems(ui.Center).ZIndex(5).
				IgnoreInput(true),
			10,
			false,
		).Children(func() {
			ui.Text(c, "当前 B").FontSize(10.5).FontWeight(500).NoWrap().TextColor(pal.ActiveText)
		})
	}

	// 底部对比控制条
	bar := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Right(8).Bottom(8).
			Height(34).
			Padding(0, 6).
			Gap(4).
			AlignItems(ui.Center).
			ZIndex(8),
		17,
		false,
	)
	_ = bar.Clicked()
	bar.Children(func() {
		diffLabel := fmt.Sprintf("热力图 · %.2f%%", p.compareDiffRes.DiffPercent)
		cmpSel := 0
		if p.compareDiff {
			cmpSel = 1
		}
		if picked := compactSegmented(c, "pin-cmp", cmpSel, []string{"拉帘", diffLabel}); picked >= 0 {
			p.compareDiff = picked == 1
		}
		if miniPillBtn(c, iconCopy, "复制", true) {
			a.copyCompareComposite(p)
		}
		if miniPillBtn(c, iconClose, "退出", false) {
			p.exitCompareMode()
		}
	})
}

func (a *app) copyCompareComposite(p *pinWin) {
	if p == nil || !p.compareMode {
		return
	}
	var out *image.NRGBA
	if p.compareDiff {
		out = p.compareDiffRes.Image
	} else {
		scale := max(1.0, float64(p.img.Bounds().Dx())/float64(max(1, p.origW)))
		out = pinops.RenderSplitCompare(p.comparePeer, p.img, p.compareSplit, scale)
	}
	if out == nil || out.Bounds().Empty() {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return
	}
	data := buf.Bytes()
	go mygo.RunOnMain(func() {
		_ = mygo.Clipboard.WriteImage(data)
		a.recordHistory(data, out.Bounds().Dx(), out.Bounds().Dy())
		a.sessionStatus("已复制贴图对比结果")
	})
}

// snapPinWindow 在拖拽移动贴图时自动与相邻贴图的四边/中线进行 8px 磁吸对齐。
func (a *app) snapPinWindow(p *pinWin) {
	if a.snappingPin || p == nil || p.win == nil || p.win.IsDestroyed() || len(a.pins) < 2 {
		return
	}
	b := p.win.Bounds()
	moving := image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	others := make([]image.Rectangle, 0, len(a.pins)-1)
	for _, q := range a.pins {
		if q == p || q.win == nil || q.win.IsDestroyed() {
			continue
		}
		qb := q.win.Bounds()
		others = append(others, image.Rect(qb.X, qb.Y, qb.X+qb.Width, qb.Y+qb.Height))
	}
	snapped, sx, sy := pinops.SnapBounds(moving, others, 8)
	if !sx && !sy {
		return
	}
	a.snappingPin = true
	p.win.SetBounds(mygo.Rectangle{X: snapped.Min.X, Y: snapped.Min.Y, Width: b.Width, Height: b.Height})
	a.snappingPin = false
}

// mergeAllPins 将当前桌面所有未折叠贴图按空间位置横向或纵向一键拼接为一张图，复制并新建贴图展示。
func (a *app) mergeAllPins(horizontal bool) {
	type entry struct {
		pin *pinWin
		x   int
		y   int
	}
	var list []entry
	for _, p := range a.pins {
		if p.collapsed || p.img == nil {
			continue
		}
		var x, y int
		if p.win != nil && !p.win.IsDestroyed() {
			b := p.win.Bounds()
			x, y = b.X, b.Y
		}
		list = append(list, entry{pin: p, x: x, y: y})
	}
	if len(list) < 2 {
		return
	}
	slices.SortFunc(list, func(i, j entry) int {
		if horizontal {
			if c := cmp.Compare(i.x, j.x); c != 0 {
				return c
			}
			return cmp.Compare(i.y, j.y)
		}
		if c := cmp.Compare(i.y, j.y); c != 0 {
			return c
		}
		return cmp.Compare(i.x, j.x)
	})
	imgs := make([]*image.NRGBA, len(list))
	for i, e := range list {
		imgs[i] = e.pin.img
	}
	merged := pinops.MergeImages(imgs, horizontal, 16, color.NRGBA{R: 22, G: 24, B: 30, A: 255})
	if merged == nil || merged.Bounds().Empty() {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, merged); err != nil {
		return
	}
	data := buf.Bytes()
	_ = mygo.Clipboard.WriteImage(data)
	a.recordHistory(data, merged.Bounds().Dx(), merged.Bounds().Dy())
	dipW := max(120, min(1100, merged.Bounds().Dx()/2))
	dipH := max(80, min(800, merged.Bounds().Dy()/2))
	cur := mygo.Screen.CursorScreenPoint()
	a.ShowPin(merged, data, image.Pt(max(20, cur.X-dipW/2), max(40, cur.Y-dipH/2)), dipW, dipH)
	a.sessionStatus(fmt.Sprintf("已拼接 %d 张贴图并复制到剪贴板", len(imgs)))
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
