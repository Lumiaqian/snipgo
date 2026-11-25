package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/pinops"
)

const (
	collapsedPillW  = 176
	collapsedPillH  = 32
	maxClosedPinCap = 10
	// 已关闭贴图只保留 PNG 字节，总量超过上限时从最旧的开始淘汰。
	maxClosedPinBytes = 200 << 20
	// 贴图位图显示后释放 CPU 像素前的等待，确保首帧已绘制。
	pinReleasePixelsDelay = 400 * time.Millisecond
)

type pinWin struct {
	win                  *mygo.Window
	baseImg              *image.NRGBA
	baseBmp              *ui.Bitmap
	img                  *image.NRGBA
	bmp                  *ui.Bitmap
	png                  []byte
	annot                *annotate.Canvas
	origW                int
	origH                int
	curW                 int
	curH                 int
	zoom                 float64
	opacity              int
	prevOpacity          int
	clickThrough         bool
	collapsed            bool
	rulerMode            bool
	editMode             bool
	cutout               bool
	cutting              bool
	compareMode          bool
	compareDiff          bool
	comparePeer          *image.NRGBA
	comparePeerBmp       *ui.Bitmap
	compareDiffRes       pinops.DiffResult
	compareDiffBmp       *ui.Bitmap
	comparePending       bool // 像素 Diff 正在后台计算
	compareGen           int  // 每次进入/退出对比递增，用于丢弃过期的后台结果
	compareSplit         float64
	compareDrag          bool
	annotDrag            bool
	annotEditDrg         bool
	textEditing          bool
	textEditIdx          int
	textEditX            float64
	textEditY            float64
	textEditBuf          string
	hoverX               float64
	hoverY               float64
	hasHover             bool
	paletteColors        []color.NRGBA
	paletteFmt           beautify.ColorFormat
	paletteToast         badgeState
	paletteToastFallback string
}

// closedPinRecord 只保存 PNG 与几何信息；恢复时再解码，避免长期占用整张 NRGBA。
type closedPinRecord struct {
	png           []byte
	at            image.Point
	origW         int
	origH         int
	zoom          float64
	opacity       int
	cutout        bool
	paletteColors []color.NRGBA
	paletteFmt    beautify.ColorFormat
}

func (a *app) ShowPin(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int) {
	a.showPinFull(img, pngBytes, at, dipW, dipH, 1.0, 100, false, nil, beautify.ColorHEX)
}

func (a *app) showPalettePin(colors []color.NRGBA, img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int) {
	paletteFmt := beautify.ParseColorFormat(a.cfg.ColorFormat)
	if a.session != nil {
		paletteFmt = a.session.ColorFormat()
	}
	a.showPinFull(img, pngBytes, at, dipW, dipH, 1.0, 100, false, colors, paletteFmt)
}

func (a *app) showPinWithState(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int, zoom float64, opacity int, cutout bool) {
	a.showPinFull(img, pngBytes, at, dipW, dipH, zoom, opacity, cutout, nil, beautify.ColorHEX)
}

func (a *app) showPinFull(img *image.NRGBA, pngBytes []byte, at image.Point, dipW, dipH int, zoom float64, opacity int, cutout bool, paletteColors []color.NRGBA, paletteFmt beautify.ColorFormat) {
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
		baseImg:       img,
		baseBmp:       baseBmp,
		img:           img,
		bmp:           baseBmp,
		png:           append([]byte(nil), pngBytes...),
		annot:         annotate.NewCanvas(),
		textEditIdx:   -1,
		origW:         dipW,
		origH:         dipH,
		curW:          curW,
		curH:          curH,
		zoom:          zoom,
		opacity:       opacity,
		prevOpacity:   opacity,
		cutout:        cutout,
		compareSplit:  0.5,
		paletteColors: cloneColorSlice(paletteColors),
		paletteFmt:    paletteFmt,
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
			png:           pngBytes,
			at:            image.Pt(b.X, b.Y),
			origW:         p.origW,
			origH:         p.origH,
			zoom:          p.zoom,
			opacity:       p.opacity,
			cutout:        p.cutout,
			paletteColors: cloneColorSlice(p.paletteColors),
			paletteFmt:    p.paletteFmt,
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
	releaseBitmapPixelsLater(p.win, baseBmp)
	p.refreshShadowSoon()
	a.refreshMenu()
}

// releaseBitmapPixelsLater 在位图首帧显示后释放其 CPU 像素副本（主线程执行）。
// 贴图仍持有 img/baseImg，mygo 需要时会从源图重新生成，所以显示与编辑不受影响。
func releaseBitmapPixelsLater(win *mygo.Window, bmps ...*ui.Bitmap) {
	if win == nil || !mygo.App.IsReady() {
		return
	}
	time.AfterFunc(pinReleasePixelsDelay, func() {
		mygo.RunOnMain(func() {
			if win.IsDestroyed() {
				return
			}
			for _, b := range bmps {
				if b != nil {
					b.ReleasePixels()
				}
			}
		})
	})
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
		cmdShiftS := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS)
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

		typing := (p.editMode && p.textEditing) || c.FocusTakesText()
		if p.editMode && p.textEditing {
			if esc && p.win != nil {
				p.textEditing = false
				p.textEditIdx = -1
				p.textEditBuf = ""
				p.win.Invalidate()
			}
		}

		var pickedPinTool annotate.Tool = -1
		if p.editMode && !typing {
			for _, tk := range [...]struct {
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
			} {
				if c.Shortcut(0, tk.key) && pickedPinTool < 0 {
					pickedPinTool = tk.tool
				}
			}
		}

		if !typing {
			if tabKey && len(p.paletteColors) > 0 && !p.collapsed && !p.editMode && !p.rulerMode && !p.compareMode {
				p.paletteFmt = beautify.NextColorFormat(p.paletteFmt)
				p.setPinNotice("格式: " + beautify.ColorFormatName(p.paletteFmt))
				return
			}
			if cmdC {
				if p.compareMode {
					a.copyCompareComposite(p)
					return
				}
				if pngCopy := p.exportPNG(); len(pngCopy) > 0 {
					if mygo.App.IsReady() {
						go mygo.RunOnMain(func() { _ = mygo.Clipboard.WriteImage(pngCopy) })
					}
					p.setPinNotice("已复制图像")
				}
				return
			}
			if cmdShiftS {
				if data := p.exportPNG(); len(data) > 0 {
					a.SavePNGAs(data)
				}
				return
			}
			if cmdS {
				if data := p.exportPNG(); len(data) > 0 {
					a.SavePNG(data)
				}
				return
			}
		}

		if p.win != nil && !typing {
			switch {
			case pickedPinTool >= 0 && p.editMode:
				p.annot.ClearSelection()
				if p.annot.Tool == pickedPinTool {
					p.annot.Tool = annotate.ToolNone
				} else {
					p.annot.Tool = pickedPinTool
				}
				p.win.Invalidate()
				return
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
					if p.editMode && p.textEditing {
						p.commitOrClosePinTextEdit()
						return true
					}
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
					if p.textEditing {
						p.commitOrClosePinTextEdit()
					}
					ox, oy := toOrig(ev)
					idx, part := p.annot.HitTest(ox, oy)
					items := p.annot.Items()
					hitText := part != annotate.EditNone && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolText
					if ev.Clicks >= 2 && hitText {
						p.annotEditDrg = false
						p.annotDrag = false
						p.textEditing = true
						p.textEditIdx = idx
						p.textEditX, p.textEditY = items[idx].P0.X, items[idx].P0.Y
						p.textEditBuf = items[idx].Text
						p.annot.Selected = idx
						return true
					}
					if p.annot.Tool == annotate.ToolText && !hitText {
						p.textEditing = true
						p.textEditIdx = -1
						p.textEditX, p.textEditY = ox, oy
						p.textEditBuf = ""
						return true
					}
					if part != annotate.EditNone {
						hitExistingLoupe := p.annot.Tool == annotate.ToolLoupe && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe
						hitExistingText := p.annot.Tool == annotate.ToolText && hitText
						if p.annot.Tool == annotate.ToolNone || part == annotate.EditP0 || part == annotate.EditP1 || hitExistingLoupe || hitExistingText {
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
						if mygo.App.IsReady() {
							go mygo.RunOnMain(func() { mygo.Clipboard.WriteText(txt) })
						}
						p.setPinNotice("已复制 " + txt)
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
				a.paintPalettePinOverlay(c, p, w, h)
				if showHoverBar {
					a.paintPinHoverBar(c, p)
				}
			}
			p.paintPinNoticeOverlay(c)
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

func (a *app) pushClosedPin(rec closedPinRecord) {
	if len(rec.png) == 0 {
		return
	}
	a.closedPins = append(a.closedPins, rec)
	total := 0
	for _, r := range a.closedPins {
		total += len(r.png)
	}
	// 先按条数、再按总字节数淘汰最旧记录（至少保留刚关闭的这一张）。
	for len(a.closedPins) > 1 && (len(a.closedPins) > maxClosedPinCap || total > maxClosedPinBytes) {
		total -= len(a.closedPins[0].png)
		a.closedPins[0] = closedPinRecord{}
		a.closedPins = a.closedPins[1:]
	}
}

// decodeClosedPin 把记录里的 PNG 解码回可显示的 NRGBA。
func decodeClosedPin(rec closedPinRecord) (*image.NRGBA, error) {
	img, err := decodeImageLimited(rec.png)
	if err != nil {
		return nil, err
	}
	return toNRGBA(img), nil
}

// RestoreLastPin 恢复最近一次关闭的贴图窗口（支持连续恢复多张）。解码在后台完成，窗口在主线程创建。
func (a *app) RestoreLastPin() {
	if len(a.closedPins) == 0 {
		return
	}
	last := a.closedPins[len(a.closedPins)-1]
	a.closedPins[len(a.closedPins)-1] = closedPinRecord{}
	a.closedPins = a.closedPins[:len(a.closedPins)-1]
	go func() {
		img, err := decodeClosedPin(last)
		if err != nil {
			return
		}
		mygo.RunOnMain(func() {
			a.showPinFull(img, last.png, last.at, last.origW, last.origH, last.zoom, last.opacity, last.cutout, last.paletteColors, last.paletteFmt)
		})
	}()
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
