// Package pinops 提供悬浮贴图之间的像素级 Diff 热力图、Before/After 拉帘合成、边缘磁吸对齐与多贴图拼接算法。
package pinops

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

// DiffResult 是两张贴图的像素级差异分析结果。
type DiffResult struct {
	Image       *image.NRGBA
	DiffPixels  int
	TotalPixels int
	DiffPercent float64
}

// RenderPixelDiff 逐像素对比 a 与 b，相同区域降饱和弱化为参考底图，差异像素以高对比洋红色高亮，
// 并统计精确差异像素数与百分比（threshold 为单通道容差，默认 12）。
func RenderPixelDiff(a, b *image.NRGBA, threshold uint8) DiffResult {
	if a == nil || b == nil || a.Bounds().Empty() || b.Bounds().Empty() {
		return DiffResult{}
	}
	if threshold == 0 {
		threshold = 12
	}
	ba := a.Bounds()
	bb := b.Bounds()
	w := max(ba.Dx(), bb.Dx())
	h := max(ba.Dy(), bb.Dy())
	out := image.NewNRGBA(image.Rect(0, 0, w, h))

	diffCount := 0
	total := w * h
	thr := int(threshold)

	for y := range h {
		inAY := y < ba.Dy()
		inBY := y < bb.Dy()
		for x := range w {
			inA := inAY && x < ba.Dx()
			inB := inBY && x < bb.Dx()
			if !inA || !inB {
				diffCount++
				// 尺寸不一致的外扩区域用琥珀红半透提示。
				var base color.NRGBA
				if inA {
					base = a.NRGBAAt(ba.Min.X+x, ba.Min.Y+y)
				} else {
					base = b.NRGBAAt(bb.Min.X+x, bb.Min.Y+y)
				}
				out.SetNRGBA(x, y, color.NRGBA{
					R: uint8(min(255, int(base.R)/3+175)),
					G: uint8(int(base.G)/3 + 52),
					B: uint8(int(base.B)/3 + 18),
					A: 255,
				})
				continue
			}
			ca := a.NRGBAAt(ba.Min.X+x, ba.Min.Y+y)
			cb := b.NRGBAAt(bb.Min.X+x, bb.Min.Y+y)
			dr := absInt(int(ca.R) - int(cb.R))
			dg := absInt(int(ca.G) - int(cb.G))
			db := absInt(int(ca.B) - int(cb.B))
			da := absInt(int(ca.A) - int(cb.A))
			maxD := max(max(dr, dg), max(db, da))

			if maxD > thr {
				diffCount++
				// 差异强度映射到高饱和洋红/玫红热力色。
				strength := min(1.0, float64(maxD-thr)/96.0)
				r := uint8(215 + 40*strength)
				g := uint8(max(24, 92-68*strength))
				bl := uint8(96 + 48*strength)
				out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: bl, A: 255})
			} else {
				// 相同像素保留低对比灰度上下文，便于一眼定位差异所在 UI 区域。
				luma := (299*int(ca.R) + 587*int(ca.G) + 114*int(ca.B)) / 1000
				v := uint8(18 + (luma*48)/100)
				out.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: uint8(min(255, int(v)+4)), A: 255})
			}
		}
	}

	pct := 0.0
	if total > 0 {
		pct = float64(diffCount) * 100.0 / float64(total)
	}
	return DiffResult{
		Image:       out,
		DiffPixels:  diffCount,
		TotalPixels: total,
		DiffPercent: pct,
	}
}

// RenderSplitCompare 渲染左右拉帘对比图（左侧 ratio 为 before，右侧为 after，附带居中分割线与圆钮）。
func RenderSplitCompare(before, after *image.NRGBA, ratio, scale float64) *image.NRGBA {
	if before == nil || after == nil || before.Bounds().Empty() || after.Bounds().Empty() {
		return before
	}
	if scale <= 0 {
		scale = 2.0
	}
	ratio = max(0.02, min(0.98, ratio))
	ba := before.Bounds()
	bb := after.Bounds()
	w := max(ba.Dx(), bb.Dx())
	h := max(ba.Dy(), bb.Dy())
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{R: 20, G: 22, B: 28, A: 255}), image.Point{}, draw.Src)

	splitX := int(math.Round(float64(w) * ratio))
	if splitX > 0 {
		leftR := image.Rect(0, 0, min(splitX, ba.Dx()), min(h, ba.Dy()))
		draw.Draw(out, leftR, before, ba.Min, draw.Src)
	}
	if splitX < w && splitX < bb.Dx() {
		rightR := image.Rect(splitX, 0, min(w, bb.Dx()), min(h, bb.Dy()))
		draw.Draw(out, rightR, after, image.Pt(bb.Min.X+splitX, bb.Min.Y), draw.Src)
	}

	// 垂直白色分割线 + 居中抓手圆钮
	lineHalf := max(1, int(math.Round(1.2*scale)))
	for y := range h {
		for x := max(0, splitX-lineHalf); x <= min(w-1, splitX+lineHalf); x++ {
			out.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 245})
		}
	}
	cy := float64(h) * 0.5
	cx := float64(splitX)
	knobR := 12.0 * scale
	for y := max(0, int(cy-knobR-2)); y <= min(h-1, int(cy+knobR+2)); y++ {
		for x := max(0, int(cx-knobR-2)); x <= min(w-1, int(cx+knobR+2)); x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			if d <= knobR {
				out.SetNRGBA(x, y, color.NRGBA{R: 24, G: 26, B: 32, A: 240})
			}
			if math.Abs(d-knobR) <= 1.5*scale {
				out.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	return out
}

// SnapBounds 计算拖拽中的贴图矩形 moving 相对于其他贴图 others 的边缘/中心线磁吸对齐结果（threshold 为吸合阈值 DIP）。
func SnapBounds(moving image.Rectangle, others []image.Rectangle, threshold int) (snapped image.Rectangle, snappedX, snappedY bool) {
	snapped = moving
	if threshold <= 0 || len(others) == 0 {
		return snapped, false, false
	}
	w := moving.Dx()
	h := moving.Dy()
	bestDX := threshold + 1
	bestDY := threshold + 1
	dxApply := 0
	dyApply := 0

	tryX := func(delta int) {
		ad := absInt(delta)
		if ad < bestDX {
			bestDX = ad
			dxApply = delta
		}
	}
	tryY := func(delta int) {
		ad := absInt(delta)
		if ad < bestDY {
			bestDY = ad
			dyApply = delta
		}
	}

	mCX := moving.Min.X + w/2
	mCY := moving.Min.Y + h/2

	for _, o := range others {
		oCX := o.Min.X + o.Dx()/2
		oCY := o.Min.Y + o.Dy()/2
		// 仅在另一方向存在合理接近范围时触发磁吸，避免相隔极远的贴图互相拉扯。
		nearY := moving.Max.Y+180 >= o.Min.Y && moving.Min.Y-180 <= o.Max.Y
		nearX := moving.Max.X+180 >= o.Min.X && moving.Min.X-180 <= o.Max.X
		if nearY {
			tryX(o.Min.X - moving.Min.X) // 左对齐左
			tryX(o.Max.X - moving.Min.X) // 左贴右（紧邻拼图）
			tryX(o.Min.X - moving.Max.X) // 右贴左
			tryX(o.Max.X - moving.Max.X) // 右对齐右
			tryX(oCX - mCX)              // 水平中线对齐
		}
		if nearX {
			tryY(o.Min.Y - moving.Min.Y) // 顶对齐顶
			tryY(o.Max.Y - moving.Min.Y) // 顶贴底
			tryY(o.Min.Y - moving.Max.Y) // 底贴顶
			tryY(o.Max.Y - moving.Max.Y) // 底对齐底
			tryY(oCY - mCY)              // 垂直中线对齐
		}
	}

	if bestDX <= threshold && dxApply != 0 {
		snapped = snapped.Add(image.Pt(dxApply, 0))
		snappedX = true
	}
	if bestDY <= threshold && dyApply != 0 {
		snapped = snapped.Add(image.Pt(0, dyApply))
		snappedY = true
	}
	return snapped, snappedX, snappedY
}

// MergeImages 将多张贴图按横向（horizontal=true）或纵向（horizontal=false）整齐拼接为一张对比图。
func MergeImages(imgs []*image.NRGBA, horizontal bool, gap int, bg color.NRGBA) *image.NRGBA {
	valid := make([]*image.NRGBA, 0, len(imgs))
	for _, im := range imgs {
		if im != nil && !im.Bounds().Empty() {
			valid = append(valid, im)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	if gap < 0 {
		gap = 0
	}
	pad := gap
	var outW, outH int
	if horizontal {
		outW = pad * 2
		for i, im := range valid {
			b := im.Bounds()
			outW += b.Dx()
			if i > 0 {
				outW += gap
			}
			outH = max(outH, b.Dy()+pad*2)
		}
	} else {
		outH = pad * 2
		for i, im := range valid {
			b := im.Bounds()
			outH += b.Dy()
			if i > 0 {
				outH += gap
			}
			outW = max(outW, b.Dx()+pad*2)
		}
	}

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	if bg.A > 0 {
		draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}
	cursor := pad
	for _, im := range valid {
		b := im.Bounds()
		if horizontal {
			oy := (outH - b.Dy()) / 2
			draw.Draw(out, image.Rect(cursor, oy, cursor+b.Dx(), oy+b.Dy()), im, b.Min, draw.Over)
			cursor += b.Dx() + gap
		} else {
			ox := (outW - b.Dx()) / 2
			draw.Draw(out, image.Rect(ox, cursor, ox+b.Dx(), cursor+b.Dy()), im, b.Min, draw.Over)
			cursor += b.Dy() + gap
		}
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
