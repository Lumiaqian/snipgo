package main

import (
	"fmt"
	"image"
	"image/color"

	"snipgo/internal/beautify"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) paintPalettePinOverlay(c *ui.Context, p *pinWin, w, h float32) {
	if p == nil || len(p.paletteColors) == 0 || p.collapsed || p.editMode || p.rulerMode || p.compareMode || p.clickThrough {
		return
	}
	pal := uiPalette(c)
	copyAndToast := func(val string) {
		a.copyTextToClipboard(val)
		p.setPaletteNotice("已复制 " + val)
	}

	renderHitBtn := func(key string, r image.Rectangle, sx, sy float32, radius float32, label, tip, val string) {
		btn := ui.ButtonBase(c.Key(key)).
			Absolute().
			Left(float32(r.Min.X)*sx).
			Top(float32(r.Min.Y)*sy).
			Size(max(12, float32(r.Dx())*sx), max(12, float32(r.Dy())*sy)).
			Radius(radius).
			ZIndex(2).
			Label(label).
			Tooltip(tip).
			Cursor(ui.CursorPointer)
		if btn.Hovered() {
			btn.Background(ui.RGBA(255, 255, 255, 0.08)).
				Border(1.5, pal.Accent.Alpha(0.9))
		} else {
			btn.Border(1, ui.Transparent)
		}
		btn.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerDown && (ev.Button == 1 || (ev.Button == 0 && ev.Mods == ui.Ctrl)) {
				go mygo.RunOnMain(func() { a.popupPinMenu(p) })
				return true
			}
			return false
		})
		if btn.Clicked() {
			copyAndToast(val)
		}
	}

	if len(p.paletteColors) == 1 {
		col := p.paletteColors[0]
		dipW, dipH, rows := beautify.SwatchCardRowRects()
		sx := w / float32(max(1, dipW))
		sy := h / float32(max(1, dipH))
		fmts := [4]beautify.ColorFormat{beautify.ColorHEX, beautify.ColorRGB, beautify.ColorOKLCH, beautify.ColorTailwind}
		for i, f := range fmts {
			val := beautify.FormatColor(col, f)
			lbl := fmt.Sprintf("复制 %s 色值", beautify.ColorFormatName(f))
			renderHitBtn(fmt.Sprintf("pin-swatch-row-%d", i), rows[i], sx, sy, 8, lbl, "点击复制 "+val, val)
		}
	} else {
		dipW, dipH, rects := beautify.PaletteCardLayout(len(p.paletteColors))
		sx := w / float32(max(1, dipW))
		sy := h / float32(max(1, dipH))
		for i, r := range rects {
			if i >= len(p.paletteColors) {
				break
			}
			col := p.paletteColors[i]
			val := beautify.FormatColor(col, p.paletteFmt)
			lbl := fmt.Sprintf("复制色块 %d (%s)", i+1, val)
			tip := fmt.Sprintf("点击复制 %s · Tab 切换格式", val)
			renderHitBtn(fmt.Sprintf("pin-palette-swatch-%d", i), r, sx, sy, 10, lbl, tip, val)
		}
	}

	if toastTxt := p.paletteToast.Text(p.paletteToastFallback); toastTxt != "" {
		ui.Row(c.Key("pin-palette-toast-wrap")).
			Absolute().
			Left(0).Right(0).Bottom(8).
			Justify(ui.Center).
			IgnoreInput(true).
			ZIndex(4).
			Children(func() {
				statusBadgePill(c.Key("pin-palette-toast-badge"), toastTxt, true)
			})
	}
}

func (p *pinWin) setPaletteNotice(msg string) {
	if p == nil {
		return
	}
	var fallback *string
	if p.win == nil {
		fallback = &p.paletteToastFallback
	}
	p.paletteToast.Set(msg, p.win, fallback)
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

func (a *app) copyPaletteCSS(p *pinWin) {
	if p == nil || len(p.paletteColors) == 0 {
		return
	}
	a.copyTextToClipboard(beautify.FormatPaletteCSS(p.paletteColors, p.paletteFmt))
	p.setPaletteNotice("已复制 CSS")
}

func cloneColorSlice(colors []color.NRGBA) []color.NRGBA {
	if len(colors) == 0 {
		return nil
	}
	out := make([]color.NRGBA, len(colors))
	copy(out, colors)
	return out
}
