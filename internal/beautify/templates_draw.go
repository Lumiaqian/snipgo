package beautify

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"
)

func drawCornerTicks(dst *image.NRGBA, x0, y0, w, h, arm, thick int, col color.NRGBA) {
	fillSeg := func(ax, ay, bx, by int) {
		for y := min(ay, by); y <= max(ay, by); y++ {
			for x := min(ax, bx); x <= max(ax, bx); x++ {
				if x >= dst.Bounds().Min.X && x < dst.Bounds().Max.X && y >= dst.Bounds().Min.Y && y < dst.Bounds().Max.Y {
					blendPixel(dst, x, y, col, 0.88)
				}
			}
		}
	}
	x1, y1 := x0+w-1, y0+h-1
	// TL, TR, BL, BR
	fillSeg(x0, y0, x0+arm, y0+thick-1)
	fillSeg(x0, y0, x0+thick-1, y0+arm)
	fillSeg(x1-arm, y0, x1, y0+thick-1)
	fillSeg(x1-thick+1, y0, x1, y0+arm)
	fillSeg(x0, y1-thick+1, x0+arm, y1)
	fillSeg(x0, y1-arm, x0+thick-1, y1)
	fillSeg(x1-arm, y1-thick+1, x1, y1)
	fillSeg(x1-thick+1, y1-arm, x1, y1)
}

func drawPerforatedStamp(dst *image.NRGBA, x0, y0, w, h int, s float64, paper, stampCol, border color.NRGBA) {
	drawRoundedBox(dst, x0, y0, w, h, 2*s, color.NRGBA{R: 255, G: 255, B: 255, A: 245}, border, 0.8)
	// 邮票四周齿轮穿孔（沿边挖出均匀半圆缺口）
	notchR := max(1.6, 2.2*s)
	step := max(5, int(math.Round(6.5*s)))
	for x := x0 + step/2; x <= x0+w-step/2; x += step {
		fillCircleAA(dst, float64(x), float64(y0), notchR, paper)
		fillCircleAA(dst, float64(x), float64(y0+h), notchR, paper)
	}
	for y := y0 + step/2; y <= y0+h-step/2; y += step {
		fillCircleAA(dst, float64(x0), float64(y), notchR, paper)
		fillCircleAA(dst, float64(x0+w), float64(y), notchR, paper)
	}
	pad := max(4, int(math.Round(5*s)))
	iw, ih := w-pad*2, h-pad*2
	if iw > 6 && ih > 6 {
		drawRoundedBox(dst, x0+pad, y0+pad, iw, ih, 1.5*s, stampCol, stampCol, 0)
		// 邮票内简约白色取景角符号
		tk := max(4, min(iw, ih)/4)
		thk := max(1, int(math.Round(1.4*s)))
		drawCornerTicks(dst, x0+pad+iw/5, y0+pad+ih/4, iw*3/5, ih/2, tk, thk, color.NRGBA{R: 255, G: 255, B: 255, A: 225})
	}
}

func drawPostmark(dst *image.NRGBA, cx, cy, s float64, ink color.NRGBA) {
	rOuter := 21.0 * s
	rInner := 14.5 * s
	strokeW := max(1.0, 1.3*s)
	b := dst.Bounds()
	reach := int(math.Ceil(rOuter + 4*s))
	baseA := float64(ink.A) / 255.0
	for y := max(b.Min.Y, int(cy)-reach); y <= min(b.Max.Y-1, int(cy)+reach); y++ {
		for x := max(b.Min.X, int(cx)-reach); x <= min(b.Max.X-1, int(cx)+reach); x++ {
			dist := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			dOut := math.Abs(dist-rOuter) - strokeW*0.5
			dIn := math.Abs(dist-rInner) - strokeW*0.5
			d := min(dOut, dIn)
			if d < 0.6 {
				blendPixel(dst, x, y, ink, min(1, 0.6-d)*baseA)
			}
		}
	}
	// 三条平行波浪销票线向左延伸
	waveLen := 38.0 * s
	for k := -1; k <= 1; k++ {
		baseY := cy + float64(k)*5.5*s
		for x := int(math.Round(cx - rOuter - waveLen)); x <= int(math.Round(cx-rInner*0.6)); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			wy := baseY + math.Sin(float64(x)/(5.0*s))*1.8*s
			iy := int(math.Round(wy))
			if iy >= b.Min.Y && iy < b.Max.Y {
				blendPixel(dst, x, iy, ink, baseA*0.85)
			}
		}
	}
}

// ================= 共享抗锯齿几何绘制原语 =================

func drawCardShadow(dst *image.NRGBA, x0, y0, w, h int, radius, shBlur, shOffY, strength float64) {
	b := dst.Bounds()
	ix0, iy0 := float64(x0), float64(y0)
	ix1, iy1 := float64(x0+w), float64(y0+h)
	cx, cy := (ix0+ix1)*0.5, (iy0+iy1)*0.5
	hx, hy := float64(w)*0.5, float64(h)*0.5
	box := image.Rect(
		max(b.Min.X, int(ix0-shBlur-3)),
		max(b.Min.Y, int(iy0+shOffY-shBlur-3)),
		min(b.Max.X, int(math.Ceil(ix1+shBlur+3))),
		min(b.Max.Y, int(math.Ceil(iy1+shOffY+shBlur+3))),
	)
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := box.Min.X; x < box.Max.X; x++ {
			px := float64(x) + 0.5
			d := roundedRectSDF(px-cx, py-(cy+shOffY), hx, hy, radius)
			if d >= shBlur {
				continue
			}
			t := max(0, min(1, 1.0-max(0, d)/shBlur))
			alpha := strength * t * t * (3 - 2*t)
			blendPixel(dst, x, y, black, alpha)
		}
	}
}

func drawRoundedBox(dst *image.NRGBA, x0, y0, w, h int, rad float64, fill, stroke color.NRGBA, strokeW float64) {
	b := dst.Bounds()
	minX := max(b.Min.X, x0-2)
	maxX := min(b.Max.X, x0+w+2)
	minY := max(b.Min.Y, y0-2)
	maxY := min(b.Max.Y, y0+h+2)
	cx := float64(x0) + float64(w)*0.5
	cy := float64(y0) + float64(h)*0.5
	hx := float64(w) * 0.5
	hy := float64(h) * 0.5
	rad = min(rad, min(hx, hy))
	for y := minY; y < maxY; y++ {
		py := float64(y) + 0.5
		for x := minX; x < maxX; x++ {
			px := float64(x) + 0.5
			d := roundedRectSDF(px-cx, py-cy, hx, hy, rad)
			if d < 0.5 {
				cov := min(1, 0.5-d) * (float64(fill.A) / 255.0)
				blendPixel(dst, x, y, fill, cov)
			}
			if strokeW > 0 {
				sd := math.Abs(d+strokeW*0.5) - strokeW*0.5
				if sd < 0.5 {
					sc := min(1, 0.5-sd) * (float64(stroke.A) / 255.0)
					blendPixel(dst, x, y, stroke, sc)
				}
			}
		}
	}
}

func stampRoundedImage(dst, src *image.NRGBA, ox, oy int, rad float64, rimCol color.NRGBA, rimW float64) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	cx := float64(ox) + float64(sw)*0.5
	cy := float64(oy) + float64(sh)*0.5
	hx := float64(sw) * 0.5
	hy := float64(sh) * 0.5
	rad = min(rad, min(hx, hy))
	db := dst.Bounds()
	for y := range sh {
		dy := oy + y
		if dy < db.Min.Y || dy >= db.Max.Y {
			continue
		}
		py := float64(dy) + 0.5
		for x := range sw {
			dx := ox + x
			if dx < db.Min.X || dx >= db.Max.X {
				continue
			}
			px := float64(dx) + 0.5
			d := roundedRectSDF(px-cx, py-cy, hx, hy, rad)
			if d >= 0.5 {
				continue
			}
			cov := min(1, 0.5-d)
			c := src.NRGBAAt(sb.Min.X+x, sb.Min.Y+y)
			if c.A > 0 {
				blendPixel(dst, dx, dy, c, cov*(float64(c.A)/255.0))
			}
			if rimW > 0 {
				rd := math.Abs(d+rimW*0.5) - rimW*0.5
				if rd < 0.5 {
					blendPixel(dst, dx, dy, rimCol, min(1, 0.5-rd)*0.55)
				}
			}
		}
	}
}

func fillCircleAA(dst *image.NRGBA, cx, cy, r float64, col color.NRGBA) {
	b := dst.Bounds()
	minX := max(b.Min.X, int(math.Floor(cx-r-2)))
	maxX := min(b.Max.X, int(math.Ceil(cx+r+2)))
	minY := max(b.Min.Y, int(math.Floor(cy-r-2)))
	maxY := min(b.Max.Y, int(math.Ceil(cy+r+2)))
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) - r
			if d < 0.5 {
				blendPixel(dst, x, y, col, min(1, 0.5-d)*(float64(col.A)/255.0))
			}
		}
	}
}

func drawDogEarCorner(dst *image.NRGBA, px1, py1 int, fold float64, backCol, edgeCol color.NRGBA) {
	// 翻卷三角顶点：(px1-fold, py1-fold), (px1-fold, py1), (px1, py1-fold)
	fx0 := float64(px1) - fold
	fy0 := float64(py1) - fold
	b := dst.Bounds()
	minX := max(b.Min.X, int(math.Floor(fx0-6)))
	maxX := min(b.Max.X, px1)
	minY := max(b.Min.Y, int(math.Floor(fy0-6)))
	maxY := min(b.Max.Y, py1)
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if fx < fx0-0.5 || fy < fy0-0.5 {
				continue
			}
			// 右上主对角线（内侧纸折区）
			diag := float64(px1) - fx + float64(py1) - fy - fold
			if diag <= -0.5 {
				continue
			}
			// 折起的背面三角形：fx >= fx0 && fy >= fy0 && diag >= 0
			inFold := fx >= fx0 && fy >= fy0
			if inFold {
				cov := min(1, fx-fx0+0.5) * min(1, fy-fy0+0.5) * min(1, diag+0.5)
				blendPixel(dst, x, y, backCol, cov)
				// 对角折痕高光边
				if math.Abs(diag) < 1.3 {
					blendPixel(dst, x, y, edgeCol, (1.0-math.Abs(diag)/1.3)*0.7)
				}
			} else {
				// 翻卷背面投在左边/上边纸面的柔和投影
				dLeft := fx0 - fx
				dTop := fy0 - fy
				dEdge := max(dLeft, dTop)
				if dEdge >= 0 && dEdge < 6 {
					shAlpha := 0.14 * (1.0 - dEdge/6.0)
					blendPixel(dst, x, y, color.NRGBA{R: 40, G: 32, B: 18, A: 255}, shAlpha)
				}
			}
		}
	}
}

func drawWashiTape(dst *image.NRGBA, cx, cy, w, h, angle, s float64, col color.NRGBA) {
	cosA, sinA := math.Cos(angle), math.Sin(angle)
	hw, hh := w*0.5, h*0.5
	b := dst.Bounds()
	reach := math.Hypot(hw, hh) + 6*s
	minX := max(b.Min.X, int(math.Floor(cx-reach)))
	maxX := min(b.Max.X, int(math.Ceil(cx+reach)))
	minY := max(b.Min.Y, int(math.Floor(cy-reach)))
	maxY := min(b.Max.Y, int(math.Ceil(cy+reach)))
	baseAlpha := float64(col.A) / 255.0

	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			// 旋转到胶带局部坐标
			lx := dx*cosA + dy*sinA
			ly := -dx*sinA + dy*cosA
			if math.Abs(ly) > hh+1.5 {
				continue
			}
			// 左右锯齿撕边偏移
			zig := math.Sin((ly/max(2.0, s))*1.6) * (2.6 * s)
			if lx < -hw-zig-1 || lx > hw+zig+1 {
				continue
			}
			edgeX := min(lx-(-hw-zig), (hw+zig)-lx)
			edgeY := hh - math.Abs(ly)
			cov := max(0, min(1, edgeX+0.5)) * max(0, min(1, edgeY+0.5))
			if cov <= 0 {
				continue
			}
			// 细微纵向织物纹理
			stripe := 0.94 + 0.06*math.Sin(lx*0.55)
			blendPixel(dst, x, y, col, cov*baseAlpha*stripe)
		}
	}
}

func fillDotGrid(dst *image.NRGBA, bg, dot color.NRGBA, s float64) {
	fillSolid(dst, bg)
	step := max(14, int(math.Round(18*s)))
	r := max(0.8, 1.0*s)
	b := dst.Bounds()
	for y := b.Min.Y + step/2; y < b.Max.Y; y += step {
		for x := b.Min.X + step/2; x < b.Max.X; x += step {
			fillCircleAA(dst, float64(x), float64(y), r, dot)
		}
	}
}

func drawDashedHLine(dst *image.NRGBA, x0, x1, y int, s float64, col color.NRGBA) {
	b := dst.Bounds()
	dashLen := max(4, int(math.Round(6*s)))
	gapLen := max(3, int(math.Round(4*s)))
	thick := max(1, int(math.Round(s)))
	period := dashLen + gapLen
	for row := y; row < y+thick && row < b.Max.Y; row++ {
		if row < b.Min.Y {
			continue
		}
		for x := max(b.Min.X, x0); x < min(b.Max.X, x1); x++ {
			if ((x - x0) % period) < dashLen {
				blendPixel(dst, x, row, col, 0.9)
			}
		}
	}
}

func drawDecorativeBarcode(dst *image.NRGBA, centerX, topY, width, height int, seedStr string, ink color.NRGBA) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seedStr))
	state := h.Sum32() | 0x9E3779B9
	nextBit := func() uint32 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return state
	}
	b := dst.Bounds()
	x0 := max(b.Min.X, centerX-width/2)
	x1 := min(b.Max.X, x0+width)
	x := x0
	for x < x1 {
		v := nextBit()
		barW := 1 + int(v%3)
		gapW := 1 + int((v>>4)%3)
		for bx := x; bx < min(x1, x+barW); bx++ {
			for by := max(b.Min.Y, topY); by < min(b.Max.Y, topY+height); by++ {
				blendPixel(dst, bx, by, ink, 0.85)
			}
		}
		x += barW + gapW
	}
}
