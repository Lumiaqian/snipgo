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
	win      *mygo.Window
	gen      int
	text     string
	hasColor bool
	col      color.NRGBA
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

func toastWidthForText(displayText string) int {
	textW := 0
	for _, r := range displayText {
		if r > 127 {
			textW += 13
		} else {
			textW += 8
		}
	}
	return max(144, min(360, 56+textW))
}

func paintActionToast(c *ui.Context, text string) {
	pal := uiPalette(c)
	liquidGlassCapsule(
		c,
		ui.Row(c.Key("action-copy-toast")).
			Fill().
			Padding(0, 12).
			Gap(7).
			AlignItems(ui.Center),
		18,
		false,
	).Children(func() {
		ui.Icon(c, iconCheck).
			FontSize(13.5).
			TextColor(pal.Accent)
		ui.Text(c, text).
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
	if text == "" {
		return
	}
	a.showToastWindow(text, true, col, nil)
}

func (a *app) showActionToast(text string) {
	if text == "" {
		return
	}
	a.showToastWindow(text, false, color.NRGBA{}, nil)
}

func (a *app) showMicrophoneSettingsToast() {
	a.showToastWindow("麦克风未开启", false, color.NRGBA{}, func() {
		go mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")
	})
}

func (a *app) showToastWindow(text string, hasColor bool, col color.NRGBA, settings func()) {
	if text == "" || !mygo.App.IsReady() {
		return
	}
	gen := 1
	if a.colorToast != nil {
		gen = a.colorToast.gen + 1
	}
	a.closeColorToast()

	displayText := text
	if hasColor {
		displayText = "已复制 " + text
	}
	w := toastWidthForText(displayText)
	lifetime := 1400 * time.Millisecond
	if settings != nil {
		w += 90
		lifetime = 8 * time.Second
	}
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
	toast := &colorToastWin{gen: gen, text: text, hasColor: hasColor, col: col}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "操作提示",
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
		Content: ui.View(func(c *ui.Context) {
			if settings != nil {
				liquidGlassCapsule(c, ui.Row(c).Fill().PaddingX(10).AlignItems(ui.Center).Gap(8), 18, false).Children(func() {
					ui.Text(c, toast.text).FontSize(12)
					ui.Spacer(c)
					if miniPillBtn(c, nil, "打开设置", false) {
						settings()
					}
				})
			} else if toast.hasColor {
				paintColorToast(c, toast.text, toast.col)
			} else {
				paintActionToast(c, toast.text)
			}
		}),
	})
	toast.win = win
	a.colorToast = toast
	preparePinWindow(win)
	applyWindowAppearance(win, a.cfg.ThemeAppearance)
	win.SetIgnoreMouseEvents(settings == nil)
	if settings != nil {
		win.SetContentProtection(true)
	}
	win.ShowInactive()

	time.AfterFunc(lifetime, func() {
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
