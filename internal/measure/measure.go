// Package measure 实现十字射线边缘自动吸附像素测量尺（类似 Shottr / xScope）。
package measure

import (
	"image"
	"image/color"
)

// DefaultTolerance 是判定遇到 UI 边界/文字边缘的色差阈值。
const DefaultTolerance = 24

// Span 描述从一点向左右上下四向发射射线测得的边界坐标（像素坐标系，闭区间 [Left, Right] × [Top, Bottom]）。
type Span struct {
	X, Y      int
	Left      int
	Right     int
	Top       int
	Bottom    int
	HitLeft   bool
	HitRight  bool
	HitTop    bool
	HitBottom bool
}

// WidthPx 返回水平测得的像素宽度。
func (s Span) WidthPx() int { return max(0, s.Right-s.Left+1) }

// HeightPx 返回垂直测得的像素高度。
func (s Span) HeightPx() int { return max(0, s.Bottom-s.Top+1) }

// Cast 从像素点 (px, py) 向四个方向扫描，遇到色差超过 tol 的边界像素即停止并返回内侧跨度。
func Cast(img *image.NRGBA, px, py, tol int) (Span, bool) {
	if img == nil || img.Bounds().Empty() {
		return Span{}, false
	}
	b := img.Bounds()
	if px < b.Min.X || px >= b.Max.X || py < b.Min.Y || py >= b.Max.Y {
		return Span{}, false
	}
	if tol <= 0 {
		tol = DefaultTolerance
	}
	ref := img.NRGBAAt(px, py)
	sp := Span{X: px, Y: py, Left: px, Right: px, Top: py, Bottom: py}

	for x := px - 1; x >= b.Min.X; x-- {
		if colorDiff(ref, img.NRGBAAt(x, py)) > tol {
			sp.HitLeft = true
			break
		}
		sp.Left = x
	}
	for x := px + 1; x < b.Max.X; x++ {
		if colorDiff(ref, img.NRGBAAt(x, py)) > tol {
			sp.HitRight = true
			break
		}
		sp.Right = x
	}
	for y := py - 1; y >= b.Min.Y; y-- {
		if colorDiff(ref, img.NRGBAAt(px, y)) > tol {
			sp.HitTop = true
			break
		}
		sp.Top = y
	}
	for y := py + 1; y < b.Max.Y; y++ {
		if colorDiff(ref, img.NRGBAAt(px, y)) > tol {
			sp.HitBottom = true
			break
		}
		sp.Bottom = y
	}
	return sp, true
}

func colorDiff(a, b color.NRGBA) int {
	dr := absInt(int(a.R) - int(b.R))
	dg := absInt(int(a.G) - int(b.G))
	db := absInt(int(a.B) - int(b.B))
	da := absInt(int(a.A) - int(b.A))
	return max(max(dr, dg), max(db, da))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
