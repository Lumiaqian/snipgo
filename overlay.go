package main

import (
	"image"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/crop"
)

// overlay 盖住一块显示器。画面在快捷键按下时已经冻结，蒙层画在冻结图上。
type overlay struct {
	win     *mygo.Window
	bitmap  *ui.Bitmap
	bounds  image.Rectangle
	index   int
	closing bool
}

type pinWin struct {
	win *mygo.Window
	bmp *ui.Bitmap
}

type pointerAct int

const (
	actNone pointerAct = iota
	actEnd
	actCancel
)

func (a *app) ShowSelection(frames []crop.Screen) {
	a.HideSelection()
	cursor := mygo.Screen.CursorScreenPoint()
	key := -1
	a.overlays = make([]*overlay, 0, len(frames))
	for i, frame := range frames {
		if frame.Pixels == nil {
			continue
		}
		o := &overlay{
			bitmap: ui.NewBitmap(frame.Pixels),
			bounds: frame.Bounds.Canon(),
			index:  i,
		}
		b := o.bounds
		o.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         "SnipGo",
			Width:         b.Dx(),
			Height:        b.Dy(),
			X:             40,
			Y:             40,
			Hidden:        true,
			Frameless:     true,
			Transparent:   true,
			DisableResize: true,
			DisableShadow: true,
			Panel:         true,
			Content:       ui.View(a.selectionView(o)),
		})
		o.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting || o.closing {
				return
			}
			e.PreventDefault()
			a.session.Cancel()
		})
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
	for i, o := range a.overlays {
		if i == key {
			continue
		}
		o.win.Show()
	}
	if key >= 0 && key < len(a.overlays) {
		a.overlays[key].win.Show()
	}
}

func (a *app) HideSelection() {
	old := a.overlays
	a.overlays = nil
	for _, o := range old {
		o.closing = true
		if o.win != nil {
			o.win.Close()
		}
	}
}

func (a *app) selectionView(o *overlay) func(*ui.Context) {
	return func(c *ui.Context) {
		if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
			a.session.Cancel()
			return
		}
		w := float32(o.bounds.Dx())
		h := float32(o.bounds.Dy())
		var act pointerAct
		ui.Box(c).Size(w, h).Children(func() {
			if o.bitmap != nil {
				ui.Image(c, o.bitmap).Size(w, h).Absolute().Left(0).Top(0).IgnoreInput(true)
			}
			hit := ui.Box(c).Size(w, h).Absolute().Left(0).Top(0).ZIndex(2)
			act = a.trackPointer(o, hit)
			if act == actCancel {
				return
			}
			a.paintMask(c, o)
		})
		switch act {
		case actCancel:
			a.session.Cancel()
		case actEnd:
			a.session.EndDrag()
		}
	}
}

func (a *app) trackPointer(o *overlay, hit ui.Element) pointerAct {
	if hit.RightClicked() {
		return actCancel
	}
	lx, ly, _ := hit.PointerPosition()
	gx := float64(o.bounds.Min.X) + float64(lx)
	gy := float64(o.bounds.Min.Y) + float64(ly)
	button, pressing := hit.PressedButton()
	primary := pressing && button == 0
	owner, _, dragging := a.session.Drag()
	switch {
	case primary && !dragging:
		a.session.BeginDrag(o.index, gx, gy)
		a.invalidateOthers(o)
	case primary && dragging && owner == o.index:
		a.session.MoveDrag(o.index, gx, gy)
		a.invalidateOthers(o)
	case !primary && dragging && owner == o.index:
		a.session.MoveDrag(o.index, gx, gy)
		return actEnd
	}
	return actNone
}

func (a *app) invalidateOthers(self *overlay) {
	for _, o := range a.overlays {
		if o != self && o.win != nil {
			o.win.Invalidate()
		}
	}
}

func (a *app) paintMask(c *ui.Context, o *overlay) {
	hole := image.Rectangle{}
	if _, rect, ok := a.session.Drag(); ok {
		hole = localSelection(o.bounds, rect)
	}
	win := image.Rect(0, 0, o.bounds.Dx(), o.bounds.Dy())
	bands, border := dimBands(win, hole)
	for _, band := range bands {
		ui.Box(c).Absolute().
			Left(float32(band.Min.X)).Top(float32(band.Min.Y)).
			Size(float32(band.Dx()), float32(band.Dy())).
			Background(ui.RGBA(0, 0, 0, 0.45)).
			IgnoreInput(true)
	}
	if border.Empty() {
		return
	}
	ui.Box(c).Absolute().
		Left(float32(border.Min.X)).Top(float32(border.Min.Y)).
		Size(float32(border.Dx()), float32(border.Dy())).
		Border(1, ui.RGB(255, 255, 255)).
		IgnoreInput(true)
}

func (a *app) ShowPin(img *image.NRGBA, at image.Point, dipW, dipH int) {
	if img == nil || dipW < 1 || dipH < 1 {
		return
	}
	p := &pinWin{bmp: ui.NewBitmap(img)}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "贴图",
		Width:         dipW,
		Height:        dipH,
		X:             40,
		Y:             40,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		SkipTaskbar:   true,
		DisableShadow: true,
		DisableResize: true,
		Content: ui.View(func(c *ui.Context) {
			if p.win != nil && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW)) {
				p.win.Close()
			}
			if p.bmp != nil {
				ui.Image(c, p.bmp).Size(float32(dipW), float32(dipH)).DragWindow()
			}
		}),
	})
	p.win.SetBounds(mygo.Rectangle{X: at.X, Y: at.Y, Width: dipW, Height: dipH})
	p.win.OnClosed(func() { a.dropPin(p) })
	a.pins = append(a.pins, p)
	p.win.ShowInactive()
}

func (a *app) dropPin(p *pinWin) {
	for i, q := range a.pins {
		if q == p {
			a.pins = append(a.pins[:i], a.pins[i+1:]...)
			return
		}
	}
}

func localSelection(bounds, sel image.Rectangle) image.Rectangle {
	return image.Rect(
		sel.Min.X-bounds.Min.X,
		sel.Min.Y-bounds.Min.Y,
		sel.Max.X-bounds.Min.X,
		sel.Max.Y-bounds.Min.Y,
	)
}

// dimBands 返回盖住 win、露出 hole 的色块，以及 hole 落在 win 里的边框。
// hole 在窗口外时整窗一块。边框是空矩形表示没有选区。
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
