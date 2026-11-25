package main

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"snipgo/internal/beautify"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type colorToastWin struct {
	win  *mygo.Window
	gen  int
	text string
	col  color.NRGBA
}

func (a *app) copyTextToClipboard(text string) {
	if text == "" {
		return
	}
	if a.clipboardTextHook != nil {
		a.clipboardTextHook(text)
		return
	}
	if mygo.App.IsReady() {
		mygo.Clipboard.WriteText(text)
	}
}

func swatchAdaptiveBorder(col color.NRGBA) ui.Color {
	if beautify.RelativeLuminance(col) < 0.2 {
		return ui.RGBA(255, 255, 255, 0.42)
	}
	return ui.RGBA(0, 0, 0, 0.22)
}

func paintColorToast(c *ui.Context, text string, col color.NRGBA) {
	pal := uiPalette(c)
	liquidGlassCapsule(
		c,
		ui.Row(c.Key("color-copy-toast")).
			Fill().
			Padding(0, 12).
			Gap(8).
			AlignItems(ui.Center),
		18,
		false,
	).Children(func() {
		ui.Box(c.Key("color-toast-swatch")).
			Size(14, 14).
			Radius(7).
			Shrink(0).
			Background(ui.RGB(col.R, col.G, col.B)).
			Border(1, swatchAdaptiveBorder(col))
		ui.Text(c, fmt.Sprintf("已复制 %s", text)).
			FontSize(12).
			FontWeight(600).
			FontFeatures("tnum").
			NoWrap().
			MaxLines(1).
			Ellipsis("…").
			Grow(1).
			TextColor(pal.Text)
	})
}

func (a *app) showColorToast(text string, col color.NRGBA) {
	if text == "" || !mygo.App.IsReady() {
		return
	}
	gen := 1
	if a.colorToast != nil {
		gen = a.colorToast.gen + 1
	}
	a.closeColorToast()

	w := max(168, min(320, 96+len(text)*8))
	h := 36
	pt := mygo.Screen.CursorScreenPoint()
	x, y := pt.X+16, pt.Y+18
	for _, d := range mygo.Screen.Displays() {
		db := image.Rect(d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.Width, d.Bounds.Y+d.Bounds.Height)
		if image.Pt(pt.X, pt.Y).In(db) {
			if x+w > db.Max.X-8 {
				x = pt.X - w - 12
			}
			if y+h > db.Max.Y-8 {
				y = pt.Y - h - 12
			}
			x = max(db.Min.X+8, min(x, db.Max.X-w-8))
			y = max(db.Min.Y+8, min(y, db.Max.Y-h-8))
			break
		}
	}

	col.A = 255
	toast := &colorToastWin{gen: gen, text: text, col: col}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "取色提示",
		Width:           w,
		Height:          h,
		X:               x,
		Y:               y,
		Hidden:          true,
		Frameless:       true,
		Transparent:     true,
		Panel:           true,
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableResize:   true,
		DisableMaximize: true,
		Content:         ui.View(func(c *ui.Context) { paintColorToast(c, toast.text, toast.col) }),
	})
	toast.win = win
	a.colorToast = toast
	preparePinWindow(win)
	applyWindowAppearance(win, a.cfg.ThemeAppearance)
	win.SetIgnoreMouseEvents(true)
	win.ShowInactive()

	time.AfterFunc(1400*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			if a.quitting || a.colorToast == nil || a.colorToast.gen != gen {
				return
			}
			a.closeColorToast()
		})
	})
}

func (a *app) closeColorToast() {
	if a.colorToast == nil {
		return
	}
	w := a.colorToast.win
	a.colorToast.win = nil
	if w != nil && !w.IsDestroyed() {
		w.Close()
	}
}
