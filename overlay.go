package main

import (
	"image"
	"image/color"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/annotate"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

// overlay 盖住一块显示器。
// 实时模式下背景透明，直接透出桌面；冻结模式下在底层绘制捕获好的静态位图。
type overlay struct {
	win     *mygo.Window
	bitmap  *ui.Bitmap
	pixels  *image.NRGBA
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
	a.lastOverlayIdx = -1
	a.paletteBarNotice.Set("", nil, &a.paletteNoticeText)
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
			pixels: frame.Pixels,
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
	a.cancelPreviewSnap()
	a.previewSnapGen++
	a.previewSnapRect = image.Rectangle{}
	a.previewSnapPending = image.Rectangle{}
	a.previewSnapImg = nil
	a.previewSnapBmp = nil
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
				lx, ly, over := hit.PointerPosition()
				gx := float64(o.bounds.Min.X) + float64(lx)
				gy := float64(o.bounds.Min.Y) + float64(ly)
				canvasHovered := hit.Hovered() && over
				hit.Cursor(cursorForHandle(a.session.HitTest(gx, gy)))
				hit.HandleInput(func(ev ui.InputEvent) bool {
					return a.onCanvasInput(o, ev)
				})
				a.paintMask(c, o, canvasHovered)
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
		hoverChanged := a.session.Hover(gx, gy)
		if (len(a.session.PickedColors()) > 0 || a.session.ColorPickerMode() || a.session.AutoOCR()) && len(a.overlays) > 1 && o.index != a.lastOverlayIdx {
			a.lastOverlayIdx = o.index
			a.invalidateOthers(o)
			return true
		}
		a.lastOverlayIdx = o.index
		if hoverChanged {
			a.invalidateOthers(o)
			return true
		}
		if !a.session.IsLive() || (a.session.Phase() == session.PhaseAdjusting && a.session.Annotations().Tool == annotate.ToolLoupe) {
			// 冻结模式下放大镜/标尺及标注放大镜悬停预览跟随每帧光标移动。
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
				ac := a.session.Annotations()
				idx, part := ac.HitTest(gx, gy)
				items := ac.Items()
				hitText := part != annotate.EditNone && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolText
				if ev.Clicks >= 2 && hitText {
					a.session.CancelDrag()
					a.textEditing = true
					a.textEditIdx = idx
					a.textEditX, a.textEditY = items[idx].P0.X, items[idx].P0.Y
					a.textEditBuf = items[idx].Text
					ac.Selected = idx
					a.invalidateAll()
					return true
				}
				if ac.Tool == annotate.ToolText && !hitText {
					a.textEditing = true
					a.textEditIdx = -1
					a.textEditX, a.textEditY = gx, gy
					a.textEditBuf = ""
					a.invalidateAll()
					return true
				}
				if ev.Clicks >= 2 && ac.Tool == annotate.ToolNone && !hitText {
					a.deferUI(a.session.Copy)
					return true
				}
			}
		}
		if ev.Button == 0 && ev.Mods&ui.Shift != 0 && a.session.ColorPickerMode() && a.session.Phase() == session.PhaseSelecting {
			a.sampleColorToPaletteWithFeedback()
			return true
		}
		a.session.BeginDrag(o.index, gx, gy)
		a.invalidateOthers(o)
		return true

	case ui.InputPointerUp:
		if ev.Button != 0 || (!a.session.Dragging() && !a.session.AnnotatingDrag()) || a.session.DragOwner() != o.index {
			return false
		}
		if ev.Canceled {
			// 指针被系统取消只中止当前拖拽，保留已有选区与标注。
			a.session.CancelDrag()
			a.invalidateAll()
			return true
		}
		a.session.MoveDrag(o.index, gx, gy)
		a.session.EndDrag()
		a.invalidateAll()
		return true
	}
	return false
}

func commitCanvasTextEdit(ac *annotate.Canvas, editing *bool, idx *int, x, y float64, buf *string) {
	if !*editing {
		return
	}
	if ac != nil {
		if *idx >= 0 {
			ac.UpdateText(*idx, *buf)
		} else if *buf != "" {
			ac.AddText(x, y, *buf)
		}
	}
	*editing = false
	*idx = -1
	*buf = ""
}

func (a *app) commitOrCloseTextEdit() {
	var ac *annotate.Canvas
	if a.session != nil {
		ac = a.session.Annotations()
	}
	commitCanvasTextEdit(ac, &a.textEditing, &a.textEditIdx, a.textEditX, a.textEditY, &a.textEditBuf)
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

var _ = color.NRGBA{}
