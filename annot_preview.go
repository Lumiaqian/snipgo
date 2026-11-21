package main

import (
	"image"
	"image/color"
	"math"
	"snipgo/internal/annotate"
	"strconv"

	"github.com/egoist/mygo/ui"
)

type canvasPreviewSource struct {
	bmp  *ui.Bitmap
	img  *image.NRGBA
	rect image.Rectangle
}

func (s canvasPreviewSource) scaleXY() (float64, float64, bool) {
	if s.rect.Empty() {
		return 0, 0, false
	}
	var bw, bh int
	if s.bmp != nil {
		bw, bh = s.bmp.Size()
	} else if s.img != nil && !s.img.Bounds().Empty() {
		bw, bh = s.img.Bounds().Dx(), s.img.Bounds().Dy()
	}
	if bw <= 0 || bh <= 0 {
		return 0, 0, false
	}
	return float64(bw) / float64(s.rect.Dx()), float64(bh) / float64(s.rect.Dy()), true
}

func needsPreviewPixels(t annotate.Tool) bool {
	return t == annotate.ToolLoupe || t == annotate.ToolMosaic || t == annotate.ToolErase
}

func (a *app) paintAnnotations(c *ui.Context, o *overlay, border image.Rectangle, canvasHovered bool) {
	ac := a.session.Annotations()
	items := ac.Items()
	selectedIdx := ac.Selected
	if d := ac.Drawing(); d != nil {
		items = append(items, *d)
	}
	sel, locked, hasSel := a.session.ActiveRect()
	wantPixels := needsPreviewPixels(ac.Tool)
	if !wantPixels {
		for _, it := range items {
			if needsPreviewPixels(it.Tool) {
				wantPixels = true
				break
			}
		}
	}
	if wantPixels && o.bitmap == nil && hasSel && locked {
		a.ensurePreviewSnapshot(sel)
	}

	var hoverLoupe *annotate.Item
	if canvasHovered && hasSel && locked && ac.Tool == annotate.ToolLoupe && !a.session.Dragging() && !a.session.AnnotatingDrag() && !a.textEditing {
		if gx, gy, ok := a.session.Cursor(); ok {
			pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
			if pt.In(sel) && a.ownsPoint(o, pt) {
				if idx, _ := ac.HitTest(gx, gy); !(idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe) {
					zoom := ac.LoupeZoom
					if zoom <= 0 {
						zoom = 2.5
					}
					hoverLoupe = &annotate.Item{
						Tool:   annotate.ToolLoupe,
						Color:  ac.Color,
						Stroke: ac.Stroke,
						P0:     annotate.PointF{X: gx, Y: gy},
						P1:     annotate.PointF{X: gx, Y: gy},
						Zoom:   zoom,
					}
				}
			}
		}
	}

	if len(items) == 0 && hoverLoupe == nil {
		return
	}

	var src canvasPreviewSource
	if o.bitmap != nil || o.pixels != nil {
		src = canvasPreviewSource{bmp: o.bitmap, img: o.pixels, rect: o.bounds}
	} else if a.previewSnapBmp != nil && hasSel && a.previewSnapRect == sel.Canon() {
		src = canvasPreviewSource{bmp: a.previewSnapBmp, img: a.previewSnapImg, rect: a.previewSnapRect}
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
			paintCanvasAnnotations(p, r, items, selectedIdx, ox, oy, baseX, baseY, 1.0, src, hoverLoupe)
		})
}

func paintCanvasAnnotations(p *ui.Painter, clipR ui.Rect, items []annotate.Item, selectedIdx int, ox, oy float64, baseX, baseY, scale float32, src canvasPreviewSource, hoverLoupe *annotate.Item) {
	p.Clip(clipR, 0, func() {
		toUI := func(pt annotate.PointF) (float32, float32) {
			return baseX + float32(pt.X-ox)*scale, baseY + float32(pt.Y-oy)*scale
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
				drawErasePreview(p, ui.Rect{X: rx, Y: ry, W: rw, H: rh}, it.P0, it.P1, src)
			case annotate.ToolMosaic:
				drawMosaicPreview(p, ui.Rect{X: rx, Y: ry, W: rw, H: rh}, it.P0, it.P1, scale, src)
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
			origStroke := max(1.5, it.Stroke)
			sw := float32(origStroke) * scale
			x0, y0 := toUI(it.P0)
			x1, y1 := toUI(it.P1)
			rx, ry := min(x0, x1), min(y0, y1)
			rw, rh := float32(math.Abs(float64(x1-x0))), float32(math.Abs(float64(y1-y0)))

			switch it.Tool {
			case annotate.ToolRect:
				p.Stroke(ui.Rect{X: rx, Y: ry, W: rw, H: rh}, col, 4*scale, sw)
			case annotate.ToolEllipse:
				drawPreviewEllipse(p, rx, ry, rw, rh, sw, col)
			case annotate.ToolArrow:
				drawPreviewArrow(p, it.P0, it.P1, toUI, origStroke, scale, col)
			case annotate.ToolPen:
				drawPreviewHighlighter(p, it.Points, toUI, float32(max(10, it.Stroke))*scale, ui.RGBA(it.Color.R, it.Color.G, it.Color.B, 0.42))
			case annotate.ToolStep:
				drawPreviewStepBadge(p, x0, y0, it.Step, it.Color, col, scale)
			case annotate.ToolText:
				fSize := float32(13+it.Stroke*2) * scale
				p.RichText(x0+scale, y0+scale, 0, ui.Span{Text: it.Text, Size: fSize, Weight: 600, Color: ui.RGBA(0, 0, 0, 0.65)})
				p.RichText(x0, y0, 0, ui.Span{Text: it.Text, Size: fSize, Weight: 600, Color: col})
			case annotate.ToolLoupe:
				zoom := it.Zoom
				if zoom <= 0 {
					zoom = 2.5
				}
				rad := float32(annotate.LoupeRadius(it.Stroke)) * scale
				drawPreviewLoupe(p, it.P0, x0, y0, x1, y1, rad, scale, zoom, col, src, false)
			}
		}

		// 悬停跟随真·实时放大瞄准镜
		if hoverLoupe != nil {
			col := ui.RGBA(hoverLoupe.Color.R, hoverLoupe.Color.G, hoverLoupe.Color.B, float32(hoverLoupe.Color.A)/255)
			hx, hy := toUI(hoverLoupe.P0)
			rad := float32(annotate.LoupeRadius(hoverLoupe.Stroke)) * scale
			zoom := hoverLoupe.Zoom
			if zoom <= 0 {
				zoom = 2.5
			}
			drawPreviewLoupe(p, hoverLoupe.P0, hx, hy, hx, hy, rad, scale, zoom, col, src, true)
		}

		// 第四趟：已点选标注图元的编辑手柄高亮
		if selectedIdx >= 0 && selectedIdx < len(items) {
			sel := items[selectedIdx]
			sx0, sy0 := toUI(sel.P0)
			sx1, sy1 := toUI(sel.P1)
			drawSelectionHandles(p, sel, sx0, sy0, sx1, sy1, scale)
		}
	})
}

func samplePerimeterColor(src canvasPreviewSource, p0, p1 annotate.PointF) (color.NRGBA, bool) {
	sx, sy, ok := src.scaleXY()
	if !ok || src.img == nil {
		return color.NRGBA{}, false
	}
	ib := src.img.Bounds()
	minX, maxX := min(p0.X, p1.X), max(p0.X, p1.X)
	minY, maxY := min(p0.Y, p1.Y), max(p0.Y, p1.Y)
	ix0 := ib.Min.X + int(math.Round((minX-float64(src.rect.Min.X))*sx))
	ix1 := ib.Min.X + int(math.Round((maxX-float64(src.rect.Min.X))*sx))
	iy0 := ib.Min.Y + int(math.Round((minY-float64(src.rect.Min.Y))*sy))
	iy1 := ib.Min.Y + int(math.Round((maxY-float64(src.rect.Min.Y))*sy))
	clampX := func(x int) int { return max(ib.Min.X, min(ib.Max.X-1, x)) }
	clampY := func(y int) int { return max(ib.Min.Y, min(ib.Max.Y-1, y)) }

	var sr, sg, sb, count int
	sample := func(x, y int) {
		c := src.img.NRGBAAt(clampX(x), clampY(y))
		sr += int(c.R)
		sg += int(c.G)
		sb += int(c.B)
		count++
	}
	const taps = 4
	for i := range taps {
		t := float64(i+1) / float64(taps+1)
		px := int(math.Round(float64(ix0) + t*float64(max(1, ix1-ix0))))
		py := int(math.Round(float64(iy0) + t*float64(max(1, iy1-iy0))))
		sample(px, iy0-2)
		sample(px, iy1+2)
		sample(ix0-2, py)
		sample(ix1+2, py)
	}
	if count == 0 {
		return color.NRGBA{}, false
	}
	return color.NRGBA{
		R: uint8(sr / count),
		G: uint8(sg / count),
		B: uint8(sb / count),
		A: 240,
	}, true
}

func drawErasePreview(p *ui.Painter, r ui.Rect, p0, p1 annotate.PointF, src canvasPreviewSource) {
	fillCol := ui.RGBA(236, 238, 244, 0.85)
	if col, ok := samplePerimeterColor(src, p0, p1); ok {
		fillCol = ui.RGBA(col.R, col.G, col.B, 0.94)
	}
	p.Fill(r, fillCol, 3)
	p.StrokeDashed(r, ui.RGBA(10, 132, 255, 0.55), 3, 1.1)
}

func drawPreviewLoupe(p *ui.Painter, p0Logical annotate.PointF, x0, y0, x1, y1, rad, scale float32, zoom float64, accent ui.Color, src canvasPreviewSource, isGhost bool) {
	if rad < 10 {
		rad = 48 * scale
	}
	ringW := max(float32(2.0), 2.6*scale)
	accentW := max(float32(1.0), 1.35*scale)
	outerR := rad + ringW
	rimOuter := outerR + accentW

	dist := float32(math.Hypot(float64(x1-x0), float64(y1-y0)))
	if dist < 12*scale {
		x1, y1 = x0, y0
	} else if !isGhost {
		ux, uy := (x1-x0)/dist, (y1-y0)/dist
		tr := 7 * scale
		if dist > tr+rimOuter+3*scale {
			lx0, ly0 := x0+ux*tr, y0+uy*tr
			lx1, ly1 := x1-ux*(rimOuter+scale), y1-uy*(rimOuter+scale)
			var stem ui.Path
			stem.Cap(ui.RoundCap).MoveTo(lx0, ly0).LineTo(lx1, ly1)
			p.StrokePath(&stem, 3.6*scale, ui.RGBA(255, 255, 255, 0.85))
			p.StrokePath(&stem, 2.0*scale, accent)
		}
		p.Stroke(ui.Rect{X: x0 - tr, Y: y0 - tr, W: tr * 2, H: tr * 2}, accent, tr, 2*scale)
		dotR := 2 * scale
		p.Fill(ui.Rect{X: x0 - dotR, Y: y0 - dotR, W: dotR * 2, H: dotR * 2}, accent, dotR)
	}

	lensR := ui.Rect{X: x1 - rad, Y: y1 - rad, W: rad * 2, H: rad * 2}
	whiteRingR := ui.Rect{X: x1 - outerR, Y: y1 - outerR, W: outerR * 2, H: outerR * 2}
	accentRingR := ui.Rect{X: x1 - rimOuter, Y: y1 - rimOuter, W: rimOuter * 2, H: rimOuter * 2}

	shAlpha := float32(0.20)
	if isGhost {
		shAlpha = 0.14
	}
	p.Shadow(whiteRingR, outerR, 0, 3.5*scale, 11*scale, 0, ui.RGBA(0, 0, 0, shAlpha))

	hasLiveRegion := false
	if sx, sy, ok := src.scaleXY(); ok && src.bmp != nil {
		bw, bh := src.bmp.Size()
		radLogical := float64(rad) / float64(max(0.05, scale))
		halfLogical := radLogical / max(1.2, zoom)
		cxPx := (p0Logical.X - float64(src.rect.Min.X)) * sx
		cyPx := (p0Logical.Y - float64(src.rect.Min.Y)) * sy
		hxPx := halfLogical * sx
		hyPx := halfLogical * sy
		ux0, uy0 := cxPx-hxPx, cyPx-hyPx
		ux1, uy1 := cxPx+hxPx, cyPx+hyPx
		cx0, cy0 := max(0.0, ux0), max(0.0, uy0)
		cx1, cy1 := min(float64(bw), ux1), min(float64(bh), uy1)
		if cx1 > cx0 && cy1 > cy0 && ux1 > ux0 && uy1 > uy0 {
			uW := ux1 - ux0
			uH := uy1 - uy0
			dx0 := lensR.X + float32((cx0-ux0)/uW)*lensR.W
			dy0 := lensR.Y + float32((cy0-uy0)/uH)*lensR.H
			dx1 := lensR.X + float32((cx1-ux0)/uW)*lensR.W
			dy1 := lensR.Y + float32((cy1-uy0)/uH)*lensR.H
			srcR := ui.Rect{X: float32(cx0), Y: float32(cy0), W: float32(cx1 - cx0), H: float32(cy1 - cy0)}
			dstR := ui.Rect{X: dx0, Y: dy0, W: dx1 - dx0, H: dy1 - dy0}
			p.Fill(lensR, ui.RGB(255, 255, 255), rad)
			p.Clip(lensR, rad, func() {
				p.ImageRegion(src.bmp, srcR, dstR, ui.ImageOptions{})
			})
			hasLiveRegion = true
		}
	}
	if isGhost || !hasLiveRegion {
		crossLen := 6 * scale
		crossCol := accent.Alpha(0.65)
		p.Line(x1-crossLen, y1, x1+crossLen, y1, 1.2, crossCol)
		p.Line(x1, y1-crossLen, x1, y1+crossLen, 1.2, crossCol)
	}

	p.Stroke(whiteRingR, ui.RGB(255, 255, 255), outerR, ringW+0.5)
	p.Stroke(accentRingR, accent, rimOuter, accentW+0.4)
}

func drawSelectionHandles(p *ui.Painter, it annotate.Item, x0, y0, x1, y1, scale float32) {
	hi := ui.RGB(10, 132, 255)
	dot := func(cx, cy float32) {
		p.Fill(ui.Rect{X: cx - 5, Y: cy - 5, W: 10, H: 10}, ui.RGB(255, 255, 255), 5)
		p.Stroke(ui.Rect{X: cx - 5, Y: cy - 5, W: 10, H: 10}, hi, 5, 2)
	}
	switch it.Tool {
	case annotate.ToolStep, annotate.ToolText:
		dot(x0, y0)
	case annotate.ToolLoupe:
		selR := float32(annotate.LoupeRadius(it.Stroke))*scale + 4.5*scale
		p.StrokeDashed(ui.Rect{X: x1 - selR, Y: y1 - selR, W: selR * 2, H: selR * 2}, hi.Alpha(0.85), selR, 1.5)
		if math.Hypot(float64(x1-x0), float64(y1-y0)) >= float64(12*scale) {
			dot(x0, y0)
		}
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

func drawPreviewArrow(p *ui.Painter, p0, p1 annotate.PointF, toUI func(annotate.PointF) (float32, float32), origStroke float64, scale float32, col ui.Color) {
	poly, ok := annotate.ArrowPolygon(p0, p1, origStroke)
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
	tailR := float32(math.Hypot(poly[0].X-poly[6].X, poly[0].Y-poly[6].Y)*0.5) * scale
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

func drawPreviewStepBadge(p *ui.Painter, cx, cy float32, step int, rawCol color.NRGBA, col ui.Color, scale float32) {
	rad := 13 * scale
	rim := 2 * scale
	p.Shadow(ui.Rect{X: cx - rad - rim, Y: cy - rad - rim, W: (rad + rim) * 2, H: (rad + rim) * 2}, rad+rim, 0, 1.5*scale, 4*scale, 0, ui.RGBA(0, 0, 0, 0.38))
	p.Fill(ui.Rect{X: cx - rad - rim, Y: cy - rad - rim, W: (rad + rim) * 2, H: (rad + rim) * 2}, ui.RGB(255, 255, 255), rad+rim)
	p.Fill(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, col, rad)

	numCol := ui.RGB(255, 255, 255)
	if int(rawCol.R)+int(rawCol.G)+int(rawCol.B) > 650 {
		numCol = ui.RGB(24, 24, 28)
	}
	span := ui.Span{
		Text:     strconv.Itoa(step),
		Size:     13.5 * scale,
		Weight:   700,
		Features: "tnum",
		Color:    numCol,
	}
	tw, th := p.MeasureText(0, span)
	p.RichText(cx-tw*0.5, cy-th*0.5, 0, span)
}

func drawMosaicPreview(p *ui.Painter, r ui.Rect, p0, p1 annotate.PointF, scale float32, src canvasPreviewSource) {
	cell := max(float32(4), 10*scale)
	sx, sy, hasImg := src.scaleXY()
	if !hasImg || src.img == nil || r.W <= 0 || r.H <= 0 {
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
		return
	}

	ib := src.img.Bounds()
	minX, maxX := min(p0.X, p1.X), max(p0.X, p1.X)
	minY, maxY := min(p0.Y, p1.Y), max(p0.Y, p1.Y)
	clampX := func(x int) int { return max(ib.Min.X, min(ib.Max.X-1, x)) }
	clampY := func(y int) int { return max(ib.Min.Y, min(ib.Max.Y-1, y)) }

	for y := r.Y; y < r.Y+r.H; y += cell {
		h := min(cell, r.Y+r.H-y)
		v := float64((y+h*0.5)-r.Y) / float64(max(1, r.H))
		ly := minY + v*(maxY-minY)
		py := ib.Min.Y + int(math.Round((ly-float64(src.rect.Min.Y))*sy))
		for x := r.X; x < r.X+r.W; x += cell {
			w := min(cell, r.X+r.W-x)
			u := float64((x+w*0.5)-r.X) / float64(max(1, r.W))
			lx := minX + u*(maxX-minX)
			px := ib.Min.X + int(math.Round((lx-float64(src.rect.Min.X))*sx))
			c00 := src.img.NRGBAAt(clampX(px), clampY(py))
			c10 := src.img.NRGBAAt(clampX(px+1), clampY(py))
			c01 := src.img.NRGBAAt(clampX(px), clampY(py+1))
			c11 := src.img.NRGBAAt(clampX(px+1), clampY(py+1))
			avg := ui.RGB(
				uint8((int(c00.R)+int(c10.R)+int(c01.R)+int(c11.R))/4),
				uint8((int(c00.G)+int(c10.G)+int(c01.G)+int(c11.G))/4),
				uint8((int(c00.B)+int(c10.B)+int(c01.B)+int(c11.B))/4),
			)
			p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, avg, 0)
		}
	}
	p.StrokeDashed(r, ui.RGBA(255, 255, 255, 0.45), 2, 1)
}
