package annotate

import (
	"image"
	"image/color"
	"math"
	"strconv"

	"snipgo/internal/textrender"
)

// Render 把全局 DIP 坐标系下的标注列表 items 原地合成到选区位图 dst 上。
// selDIP 是 dst 对应的全局 DIP 选区矩形。
func Render(dst *image.NRGBA, selDIP image.Rectangle, items []Item) {
	selDIP = selDIP.Canon()
	if dst == nil || dst.Bounds().Empty() || selDIP.Empty() || len(items) == 0 {
		return
	}
	b := dst.Bounds()
	sx := float64(b.Dx()) / float64(selDIP.Dx())
	sy := float64(b.Dy()) / float64(selDIP.Dy())
	scale := (sx + sy) * 0.5

	toPx := func(p PointF) PointF {
		return PointF{
			X: float64(b.Min.X) + (p.X-float64(selDIP.Min.X))*sx,
			Y: float64(b.Min.Y) + (p.Y-float64(selDIP.Min.Y))*sy,
		}
	}

	// 第一趟：先在底图原像素上执行无痕擦除与马赛克打码（避免把后续叠加的箭头/红框打糊）。
	var spotlights [][2]PointF
	for _, it := range items {
		p0 := toPx(it.P0)
		p1 := toPx(it.P1)
		switch it.Tool {
		case ToolErase:
			rasterSmartErase(dst, p0, p1)
		case ToolMosaic:
			rasterMosaic(dst, p0, p1, max(6, int(math.Round(10*scale))))
		case ToolSpotlight:
			spotlights = append(spotlights, [2]PointF{p0, p1})
		}
	}

	// 第二趟：若存在聚光灯，合并所有聚光灯高亮窗口，将窗口外底图统一压暗 58%（多个聚光灯绝不重复变黑）。
	if len(spotlights) > 0 {
		rasterSpotlights(dst, spotlights, scale)
	}

	// 第二趟半：局部放大镜特写（从放大前干净底图快照双线性采样，避免多放大镜互相递归重影）。
	hasLoupe := false
	for _, it := range items {
		if it.Tool == ToolLoupe {
			hasLoupe = true
			break
		}
	}
	if hasLoupe {
		loupeSrc := cloneNRGBA(dst)
		for _, it := range items {
			if it.Tool != ToolLoupe {
				continue
			}
			p0 := toPx(it.P0)
			p1 := toPx(it.P1)
			radPx := max(16, LoupeRadius(it.Stroke)*scale)
			zoom := it.Zoom
			if zoom <= 0 {
				zoom = 2.5
			}
			rasterLoupe(dst, loupeSrc, p0, p1, radPx, zoom, scale, it.Color)
		}
	}

	// 第三趟：在最上层绘制高对比矢量标注（箭头、框、椭圆、荧光笔、步骤序号、文字批注保持 100% 明亮清晰）。
	for _, it := range items {
		p0 := toPx(it.P0)
		p1 := toPx(it.P1)
		sw := max(1.5, it.Stroke*scale)
		switch it.Tool {
		case ToolRect:
			rasterRoundedRect(dst, p0, p1, max(2, 4*scale), sw, it.Color)
		case ToolEllipse:
			rasterEllipse(dst, p0, p1, sw, it.Color)
		case ToolArrow:
			rasterArrow(dst, p0, p1, sw, it.Color)
		case ToolPen:
			pts := make([]PointF, len(it.Points))
			for i, p := range it.Points {
				pts[i] = toPx(p)
			}
			rasterHighlighter(dst, pts, max(8, it.Stroke*scale), it.Color)
		case ToolStep:
			rasterStepBadge(dst, p0, scale, it.Step, it.Color)
		case ToolText:
			rasterText(dst, p0, scale, it.Stroke, it.Text, it.Color)
		}
	}
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	copy(dst.Pix, src.Pix)
	return dst
}

func sampleBilinear(src *image.NRGBA, fx, fy float64) color.NRGBA {
	b := src.Bounds()
	fx = max(float64(b.Min.X), min(float64(b.Max.X-1), fx))
	fy = max(float64(b.Min.Y), min(float64(b.Max.Y-1), fy))
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	x1 := min(b.Max.X-1, x0+1)
	y1 := min(b.Max.Y-1, y0+1)
	wx := fx - float64(x0)
	wy := fy - float64(y0)
	c00 := src.NRGBAAt(x0, y0)
	c10 := src.NRGBAAt(x1, y0)
	c01 := src.NRGBAAt(x0, y1)
	c11 := src.NRGBAAt(x1, y1)
	lerp := func(a, b, c, d uint8) uint8 {
		top := float64(a)*(1-wx) + float64(b)*wx
		bot := float64(c)*(1-wx) + float64(d)*wx
		return uint8(max(0, min(255, math.Round(top*(1-wy)+bot*wy))))
	}
	return color.NRGBA{
		R: lerp(c00.R, c10.R, c01.R, c11.R),
		G: lerp(c00.G, c10.G, c01.G, c11.G),
		B: lerp(c00.B, c10.B, c01.B, c11.B),
		A: lerp(c00.A, c10.A, c01.A, c11.A),
	}
}

// rasterLoupe 绘制圆形局部放大镜特写气泡（支持原地放大或拉出带引线特写，附带高光玻璃边框与柔和投影）。
func rasterLoupe(dst, src *image.NRGBA, p0, p1 PointF, radPx, zoom, scale float64, accent color.NRGBA) {
	if zoom < 1.2 {
		zoom = 2.5
	}
	if accent.A == 0 {
		accent = Palette[0]
	}
	ringW := max(2.0, 2.6*scale)
	outerR := radPx + ringW
	b := dst.Bounds()

	// 1. 当采样点与镜心分离时，绘制目标小圈与连接引线。
	distCenters := math.Hypot(p1.X-p0.X, p1.Y-p0.Y)
	if distCenters > 10*scale {
		targetR := max(5.0, 7.5*scale)
		ux := (p1.X - p0.X) / distCenters
		uy := (p1.Y - p0.Y) / distCenters
		lineStart := PointF{X: p0.X + ux*targetR, Y: p0.Y + uy*targetR}
		lineEnd := PointF{X: p1.X - ux*(outerR+1), Y: p1.Y - uy*(outerR+1)}
		if distCenters > targetR+outerR+2 {
			stemW := max(1.5, 2.2*scale)
			stemBox := clampBounds(b, min(lineStart.X, lineEnd.X)-stemW-3, min(lineStart.Y, lineEnd.Y)-stemW-3, max(lineStart.X, lineEnd.X)+stemW+3, max(lineStart.Y, lineEnd.Y)+stemW+3)
			white := color.NRGBA{R: 255, G: 255, B: 255, A: 235}
			for y := stemBox.Min.Y; y < stemBox.Max.Y; y++ {
				py := float64(y) + 0.5
				for x := stemBox.Min.X; x < stemBox.Max.X; x++ {
					px := float64(x) + 0.5
					d := distToSegment(PointF{X: px, Y: py}, lineStart, lineEnd)
					if cov := smoothCoverage(d - (stemW*0.5 + 1.0*scale)); cov > 0 {
						blendOver(dst, x, y, white, cov*0.85)
					}
					if cov := smoothCoverage(d - stemW*0.5); cov > 0 {
						blendOver(dst, x, y, accent, cov)
					}
				}
			}
		}
		// 目标锚点小圆圈
		tBox := clampBounds(b, p0.X-targetR-4*scale, p0.Y-targetR-4*scale, p0.X+targetR+4*scale, p0.Y+targetR+4*scale)
		tStroke := max(1.5, 2.0*scale)
		for y := tBox.Min.Y; y < tBox.Max.Y; y++ {
			py := float64(y) + 0.5
			for x := tBox.Min.X; x < tBox.Max.X; x++ {
				px := float64(x) + 0.5
				dc := math.Hypot(px-p0.X, py-p0.Y)
				if cov := smoothCoverage(math.Abs(dc-targetR) - tStroke*0.5); cov > 0 {
					blendOver(dst, x, y, accent, cov)
				}
				if cov := smoothCoverage(dc - max(1.5, 2.0*scale)); cov > 0 {
					blendOver(dst, x, y, accent, cov)
				}
			}
		}
	}

	// 2. 柔和投影 + 放大镜内部双线性放大采样 + 玻璃高光边框。
	shBlur := 16.0 * scale
	shOffY := 5.0 * scale
	box := clampBounds(b, p1.X-outerR-shBlur-2, p1.Y-outerR-shBlur-2, p1.X+outerR+shBlur+2, p1.Y+outerR+shBlur+shOffY+2)
	shadowCol := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	rimOuter := outerR + max(1.0, 1.2*scale)

	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			dSh := math.Hypot(px-p1.X, py-(p1.Y+shOffY)) - outerR
			if dSh < shBlur {
				t := max(0, min(1, 1.0-max(0, dSh)/shBlur))
				blendOver(dst, x, y, shadowCol, 0.38*t*t*(3-2*t))
			}

			dx, dy := px-p1.X, py-p1.Y
			dist := math.Hypot(dx, dy)
			if dist > rimOuter+1.5 {
				continue
			}

			// 镜内放大像素
			if cov := smoothCoverage(dist - radPx); cov > 0 {
				sx := p0.X + dx/zoom
				sy := p0.Y + dy/zoom
				col := sampleBilinear(src, sx, sy)
				blendOver(dst, x, y, col, cov)
				// 顶部轻微弧形玻璃高光，增强放大镜立体质感
				if dy < -radPx*0.18 && dist < radPx*0.92 {
					spec := max(0, min(1, (-dy/radPx-0.18)/0.82)) * 0.14
					blendOver(dst, x, y, white, cov*spec)
				}
			}

			// 白色主镜框
			dWhite := math.Abs(dist-(radPx+ringW*0.5)) - ringW*0.5
			if cov := smoothCoverage(dWhite); cov > 0 {
				blendOver(dst, x, y, white, cov*0.96)
			}
			// 外沿主题色细描边
			accentW := max(1.0, 1.3*scale)
			dAccent := math.Abs(dist-(outerR+accentW*0.4)) - accentW*0.5
			if cov := smoothCoverage(dAccent); cov > 0 {
				blendOver(dst, x, y, accent, cov*0.92)
			}
		}
	}
}

// rasterSmartErase 采样选框四周边外侧像素，按反向距离加权插值平滑填补背景，无痕抹除文字或杂物。
func rasterSmartErase(dst *image.NRGBA, p0, p1 PointF) {
	x0 := int(math.Round(min(p0.X, p1.X)))
	y0 := int(math.Round(min(p0.Y, p1.Y)))
	x1 := int(math.Round(max(p0.X, p1.X)))
	y1 := int(math.Round(max(p0.Y, p1.Y)))
	b := dst.Bounds()
	r := image.Rect(x0, y0, x1, y1).Intersect(b)
	if r.Empty() {
		return
	}
	sampleClamped := func(x, y int) color.NRGBA {
		cx := max(b.Min.X, min(b.Max.X-1, x))
		cy := max(b.Min.Y, min(b.Max.Y-1, y))
		return dst.NRGBAAt(cx, cy)
	}
	topEdge := make([]color.NRGBA, r.Dx())
	botEdge := make([]color.NRGBA, r.Dx())
	for i := range r.Dx() {
		sx := r.Min.X + i
		topEdge[i] = sampleClamped(sx, r.Min.Y-2)
		botEdge[i] = sampleClamped(sx, r.Max.Y+1)
	}
	leftEdge := make([]color.NRGBA, r.Dy())
	rightEdge := make([]color.NRGBA, r.Dy())
	for j := range r.Dy() {
		sy := r.Min.Y + j
		leftEdge[j] = sampleClamped(r.Min.X-2, sy)
		rightEdge[j] = sampleClamped(r.Max.X+1, sy)
	}

	wF := float64(max(1, r.Dx()))
	hF := float64(max(1, r.Dy()))
	for j := range r.Dy() {
		dTop := float64(j) + 1.0
		dBot := hF - float64(j)
		wTop := 1.0 / dTop
		wBot := 1.0 / dBot
		cL := leftEdge[j]
		cR := rightEdge[j]
		for i := range r.Dx() {
			dLeft := float64(i) + 1.0
			dRight := wF - float64(i)
			wLeft := 1.0 / dLeft
			wRight := 1.0 / dRight
			wSum := wTop + wBot + wLeft + wRight
			cT := topEdge[i]
			cB := botEdge[i]

			rr := (float64(cT.R)*wTop + float64(cB.R)*wBot + float64(cL.R)*wLeft + float64(cR.R)*wRight) / wSum
			gg := (float64(cT.G)*wTop + float64(cB.G)*wBot + float64(cL.G)*wLeft + float64(cR.G)*wRight) / wSum
			bb := (float64(cT.B)*wTop + float64(cB.B)*wBot + float64(cL.B)*wLeft + float64(cR.B)*wRight) / wSum
			dst.SetNRGBA(r.Min.X+i, r.Min.Y+j, color.NRGBA{
				R: uint8(max(0, min(255, math.Round(rr)))),
				G: uint8(max(0, min(255, math.Round(gg)))),
				B: uint8(max(0, min(255, math.Round(bb)))),
				A: 255,
			})
		}
	}
}

func rasterSpotlights(dst *image.NRGBA, spots [][2]PointF, scale float64) {
	b := dst.Bounds()
	type spotSpec struct {
		cx, cy, hx, hy, rad float64
	}
	specs := make([]spotSpec, 0, len(spots))
	radBase := max(3.0, 6.0*scale)
	for _, sp := range spots {
		minX, maxX := min(sp[0].X, sp[1].X), max(sp[0].X, sp[1].X)
		minY, maxY := min(sp[0].Y, sp[1].Y), max(sp[0].Y, sp[1].Y)
		hx := (maxX - minX) * 0.5
		hy := (maxY - minY) * 0.5
		if hx < 1 || hy < 1 {
			continue
		}
		specs = append(specs, spotSpec{
			cx:  (minX + maxX) * 0.5,
			cy:  (minY + maxY) * 0.5,
			hx:  hx,
			hy:  hy,
			rad: min(radBase, min(hx, hy)),
		})
	}
	if len(specs) == 0 {
		return
	}
	scrim := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	rimCol := color.NRGBA{R: 255, G: 255, B: 255, A: 185}
	rimHalf := max(0.75, 0.8*scale)

	for y := b.Min.Y; y < b.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := b.Min.X; x < b.Max.X; x++ {
			px := float64(x) + 0.5
			minSDF := math.MaxFloat64
			for _, s := range specs {
				dx := math.Abs(px-s.cx) - (s.hx - s.rad)
				dy := math.Abs(py-s.cy) - (s.hy - s.rad)
				d := math.Hypot(max(dx, 0), max(dy, 0)) + min(max(dx, dy), 0) - s.rad
				if d < minSDF {
					minSDF = d
				}
			}
			// minSDF > 0 在所有聚光灯外部：平滑压暗 58%。
			dimCov := smoothCoverage(-minSDF) * 0.58
			if dimCov > 0 {
				blendOver(dst, x, y, scrim, dimCov)
			}
			// 聚光灯外沿 1.5px 柔和亮边，增强聚焦层次。
			if rimCov := smoothCoverage(math.Abs(minSDF-rimHalf) - rimHalf); rimCov > 0 {
				blendOver(dst, x, y, rimCol, rimCov*0.75)
			}
		}
	}
}

func rasterMosaic(dst *image.NRGBA, p0, p1 PointF, cell int) {
	x0 := int(math.Round(min(p0.X, p1.X)))
	y0 := int(math.Round(min(p0.Y, p1.Y)))
	x1 := int(math.Round(max(p0.X, p1.X)))
	y1 := int(math.Round(max(p0.Y, p1.Y)))
	r := image.Rect(x0, y0, x1, y1).Intersect(dst.Bounds())
	if r.Empty() || cell < 2 {
		return
	}
	for by := r.Min.Y; by < r.Max.Y; by += cell {
		ey := min(by+cell, r.Max.Y)
		for bx := r.Min.X; bx < r.Max.X; bx += cell {
			ex := min(bx+cell, r.Max.X)
			var sr, sg, sb, sa, count int
			for y := by; y < ey; y++ {
				for x := bx; x < ex; x++ {
					c := dst.NRGBAAt(x, y)
					sr += int(c.R)
					sg += int(c.G)
					sb += int(c.B)
					sa += int(c.A)
					count++
				}
			}
			if count == 0 {
				continue
			}
			avg := color.NRGBA{
				R: uint8(sr / count),
				G: uint8(sg / count),
				B: uint8(sb / count),
				A: uint8(sa / count),
			}
			for y := by; y < ey; y++ {
				for x := bx; x < ex; x++ {
					dst.SetNRGBA(x, y, avg)
				}
			}
		}
	}
}

func rasterRoundedRect(dst *image.NRGBA, p0, p1 PointF, radius, sw float64, col color.NRGBA) {
	minX, maxX := min(p0.X, p1.X), max(p0.X, p1.X)
	minY, maxY := min(p0.Y, p1.Y), max(p0.Y, p1.Y)
	cx := (minX + maxX) * 0.5
	cy := (minY + maxY) * 0.5
	hx := (maxX - minX) * 0.5
	hy := (maxY - minY) * 0.5
	rad := min(radius, min(hx, hy))
	halfW := sw * 0.5

	box := clampBounds(dst.Bounds(), minX-halfW-2, minY-halfW-2, maxX+halfW+2, maxY+halfW+2)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			dx := math.Abs(px-cx) - (hx - rad)
			dy := math.Abs(py-cy) - (hy - rad)
			outD := math.Hypot(max(dx, 0), max(dy, 0)) + min(max(dx, dy), 0) - rad
			dist := math.Abs(outD) - halfW
			if cov := smoothCoverage(dist); cov > 0 {
				blendOver(dst, x, y, col, cov)
			}
		}
	}
}

func rasterEllipse(dst *image.NRGBA, p0, p1 PointF, sw float64, col color.NRGBA) {
	minX, maxX := min(p0.X, p1.X), max(p0.X, p1.X)
	minY, maxY := min(p0.Y, p1.Y), max(p0.Y, p1.Y)
	rx := (maxX - minX) * 0.5
	ry := (maxY - minY) * 0.5
	if rx < 1 || ry < 1 {
		return
	}
	cx := (minX + maxX) * 0.5
	cy := (minY + maxY) * 0.5
	halfW := sw * 0.5

	box := clampBounds(dst.Bounds(), minX-halfW-2, minY-halfW-2, maxX+halfW+2, maxY+halfW+2)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			nx := (px - cx) / rx
			ny := (py - cy) / ry
			k1 := math.Hypot(nx, ny)
			if k1 < 1e-6 {
				continue
			}
			k2 := math.Hypot(nx/rx, ny/ry)
			sdf := math.Abs(k1-1.0) * (k1 / k2)
			if cov := smoothCoverage(sdf - halfW); cov > 0 {
				blendOver(dst, x, y, col, cov)
			}
		}
	}
}

// ArrowPolygon 计算 Snipaste 风格流线型箭头的 7 顶点闭合多边形（细尾渐粗轴身 + 内凹燕尾倒刺箭头），
// 所有尺寸均与线宽 sw 及总长成正比，保证 1x 实时预览与 2x Retina 导出几何完全一致。
func ArrowPolygon(p0, p1 PointF, sw float64) ([7]PointF, bool) {
	var poly [7]PointF
	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	length := math.Hypot(dx, dy)
	if length < 1.5 {
		return poly, false
	}
	ux, uy := dx/length, dy/length
	nx, ny := -uy, ux

	headLen := min(length*0.62, max(sw*5.6, sw*3.2))
	wingHalfW := headLen * 0.45
	neckDist := headLen * 0.76
	neckHalfW := min(wingHalfW*0.40, max(sw*0.92, 1.2))
	tailHalfW := max(0.75, sw*0.24)
	if length < sw*3.5 {
		neckHalfW = min(neckHalfW, wingHalfW*0.35)
		tailHalfW = min(tailHalfW, neckHalfW*0.6)
	}

	neckX := p1.X - ux*neckDist
	neckY := p1.Y - uy*neckDist
	wingX := p1.X - ux*headLen
	wingY := p1.Y - uy*headLen

	poly[0] = PointF{X: p0.X + nx*tailHalfW, Y: p0.Y + ny*tailHalfW}
	poly[1] = PointF{X: neckX + nx*neckHalfW, Y: neckY + ny*neckHalfW}
	poly[2] = PointF{X: wingX + nx*wingHalfW, Y: wingY + ny*wingHalfW}
	poly[3] = p1
	poly[4] = PointF{X: wingX - nx*wingHalfW, Y: wingY - ny*wingHalfW}
	poly[5] = PointF{X: neckX - nx*neckHalfW, Y: neckY - ny*neckHalfW}
	poly[6] = PointF{X: p0.X - nx*tailHalfW, Y: p0.Y - ny*tailHalfW}
	return poly, true
}

func rasterArrow(dst *image.NRGBA, p0, p1 PointF, sw float64, col color.NRGBA) {
	poly, ok := ArrowPolygon(p0, p1, sw)
	if !ok {
		return
	}
	minX, maxX := poly[0].X, poly[0].X
	minY, maxY := poly[0].Y, poly[0].Y
	for _, v := range poly[1:] {
		minX = min(minX, v.X)
		maxX = max(maxX, v.X)
		minY = min(minY, v.Y)
		maxY = max(maxY, v.Y)
	}
	tailR := math.Hypot(poly[0].X-poly[6].X, poly[0].Y-poly[6].Y) * 0.5
	box := clampBounds(dst.Bounds(), minX-tailR-3, minY-tailR-3, maxX+tailR+3, maxY+tailR+3)

	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			pt := PointF{X: px, Y: py}
			dPoly := sdfPolygon(pt, poly[:])
			dTailCap := math.Hypot(px-p0.X, py-p0.Y) - tailR
			d := min(dPoly, dTailCap)
			if cov := smoothCoverage(d); cov > 0 {
				blendOver(dst, x, y, col, cov)
			}
		}
	}
}

func rasterHighlighter(dst *image.NRGBA, pts []PointF, sw float64, col color.NRGBA) {
	if len(pts) == 0 {
		return
	}
	minX, maxX := pts[0].X, pts[0].X
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts[1:] {
		minX = min(minX, p.X)
		maxX = max(maxX, p.X)
		minY = min(minY, p.Y)
		maxY = max(maxY, p.Y)
	}
	halfW := sw * 0.5
	box := clampBounds(dst.Bounds(), minX-halfW-2, minY-halfW-2, maxX+halfW+2, maxY+halfW+2)
	if box.Empty() {
		return
	}
	// 同一笔荧光笔内部取最大覆盖度，只叠加一次半透明（0.42），防止拐角颜色变脏加深。
	ink := col
	ink.A = uint8(float64(col.A) * 0.42)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			pt := PointF{X: px, Y: py}
			best := math.MaxFloat64
			if len(pts) == 1 {
				best = math.Hypot(pt.X-pts[0].X, pt.Y-pts[0].Y) - halfW
			} else {
				for i := 0; i+1 < len(pts); i++ {
					if d := distToSegment(pt, pts[i], pts[i+1]) - halfW; d < best {
						best = d
					}
				}
			}
			if cov := smoothCoverage(best); cov > 0 {
				blendOver(dst, x, y, ink, cov)
			}
		}
	}
}

func rasterStepBadge(dst *image.NRGBA, center PointF, scale float64, step int, col color.NRGBA) {
	r := 13.0 * scale
	ring := 2.0 * scale
	box := clampBounds(dst.Bounds(), center.X-r-ring-4, center.Y-r-ring-4, center.X+r+ring+4, center.Y+r+ring+4)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	shadowCol := color.NRGBA{R: 0, G: 0, B: 0, A: 90}

	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			// 柔和投影
			dSh := math.Hypot(px-center.X, py-(center.Y+1.5*scale)) - (r + ring)
			if cov := smoothCoverage(dSh); cov > 0 {
				blendOver(dst, x, y, shadowCol, cov*0.5)
			}
			// 白色外圈
			dOuter := math.Hypot(px-center.X, py-center.Y) - (r + ring)
			if cov := smoothCoverage(dOuter); cov > 0 {
				blendOver(dst, x, y, white, cov)
			}
			// 彩色圆底
			dInner := math.Hypot(px-center.X, py-center.Y) - r
			if cov := smoothCoverage(dInner); cov > 0 {
				blendOver(dst, x, y, col, cov)
			}
		}
	}

	numCol := white
	if int(col.R)+int(col.G)+int(col.B) > 650 {
		numCol = color.NRGBA{R: 24, G: 24, B: 28, A: 255}
	}
	label := textrender.Render(strconv.Itoa(step), textrender.Options{
		FontSize: 13.5 * scale,
		Weight:   0.4,
		Color:    numCol,
	})
	if label != nil {
		lb := label.Bounds()
		ox := int(math.Round(center.X - float64(lb.Dx())*0.5))
		oy := int(math.Round(center.Y - float64(lb.Dy())*0.5))
		stampNRGBA(dst, label, ox, oy)
	}
}

func rasterText(dst *image.NRGBA, anchor PointF, scale, stroke float64, text string, col color.NRGBA) {
	fontPx := max(14, (13+stroke*2)*scale)
	shadow := textrender.Render(text, textrender.Options{
		FontSize: fontPx,
		Weight:   0.3,
		Color:    color.NRGBA{R: 0, G: 0, B: 0, A: 150},
	})
	fg := textrender.Render(text, textrender.Options{
		FontSize: fontPx,
		Weight:   0.3,
		Color:    col,
	})
	if fg == nil {
		return
	}
	ox := int(math.Round(anchor.X))
	oy := int(math.Round(anchor.Y))
	off := max(1, int(math.Round(scale)))
	if shadow != nil {
		stampNRGBA(dst, shadow, ox+off, oy+off)
	}
	stampNRGBA(dst, fg, ox, oy)
}

func stampNRGBA(dst, src *image.NRGBA, ox, oy int) {
	sb := src.Bounds()
	db := dst.Bounds()
	for sy := sb.Min.Y; sy < sb.Max.Y; sy++ {
		dy := oy + (sy - sb.Min.Y)
		if dy < db.Min.Y || dy >= db.Max.Y {
			continue
		}
		for sx := sb.Min.X; sx < sb.Max.X; sx++ {
			dx := ox + (sx - sb.Min.X)
			if dx < db.Min.X || dx >= db.Max.X {
				continue
			}
			c := src.NRGBAAt(sx, sy)
			if c.A == 0 {
				continue
			}
			blendOver(dst, dx, dy, c, 1.0)
		}
	}
}

func clampBounds(b image.Rectangle, minX, minY, maxX, maxY float64) image.Rectangle {
	r := image.Rect(
		int(math.Floor(minX)),
		int(math.Floor(minY)),
		int(math.Ceil(maxX)),
		int(math.Ceil(maxY)),
	)
	return r.Intersect(b)
}

func smoothCoverage(sdf float64) float64 {
	if sdf <= -0.5 {
		return 1.0
	}
	if sdf >= 0.5 {
		return 0.0
	}
	return 0.5 - sdf
}

func distToSegment(p, a, b PointF) float64 {
	vx, vy := b.X-a.X, b.Y-a.Y
	wx, wy := p.X-a.X, p.Y-a.Y
	l2 := vx*vx + vy*vy
	if l2 < 1e-9 {
		return math.Hypot(wx, wy)
	}
	t := max(0, min(1, (wx*vx+wy*vy)/l2))
	projX := a.X + t*vx
	projY := a.Y + t*vy
	return math.Hypot(p.X-projX, p.Y-projY)
}

// sdfPolygon 计算点 p 到任意简单闭合多边形（支持凹多边形且不依赖顶点顺逆时针顺序）的有符号距离：内部为负，外部为正。
func sdfPolygon(p PointF, v []PointF) float64 {
	n := len(v)
	if n < 3 {
		return math.MaxFloat64
	}
	minDist := math.MaxFloat64
	inside := false
	j := n - 1
	for i := range n {
		vi, vj := v[i], v[j]
		if d := distToSegment(p, vi, vj); d < minDist {
			minDist = d
		}
		if (vi.Y > p.Y) != (vj.Y > p.Y) {
			xCross := (vj.X-vi.X)*(p.Y-vi.Y)/(vj.Y-vi.Y) + vi.X
			if p.X < xCross {
				inside = !inside
			}
		}
		j = i
	}
	if inside {
		return -minDist
	}
	return minDist
}

func blendOver(dst *image.NRGBA, x, y int, src color.NRGBA, coverage float64) {
	sa := (float64(src.A) / 255.0) * coverage
	if sa <= 0 {
		return
	}
	bg := dst.NRGBAAt(x, y)
	if sa >= 0.999 && bg.A == 255 {
		dst.SetNRGBA(x, y, color.NRGBA{R: src.R, G: src.G, B: src.B, A: 255})
		return
	}
	ba := float64(bg.A) / 255.0
	outA := sa + ba*(1-sa)
	if outA <= 1e-6 {
		return
	}
	r := (float64(src.R)*sa + float64(bg.R)*ba*(1-sa)) / outA
	g := (float64(src.G)*sa + float64(bg.G)*ba*(1-sa)) / outA
	b := (float64(src.B)*sa + float64(bg.B)*ba*(1-sa)) / outA
	dst.SetNRGBA(x, y, color.NRGBA{
		R: uint8(min(255, max(0, math.Round(r)))),
		G: uint8(min(255, max(0, math.Round(g)))),
		B: uint8(min(255, max(0, math.Round(b)))),
		A: uint8(min(255, max(0, math.Round(outA*255)))),
	})
}
