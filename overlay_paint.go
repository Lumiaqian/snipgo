package main

import (
	"fmt"
	"image"
	"math"
	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/session"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) paintMask(c *ui.Context, o *overlay, canvasHovered bool) {
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
			a.paintAnnotations(c, o, border, canvasHovered)
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
	boxW := max(gridPx+pad*2, 214)
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
