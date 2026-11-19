// Package crop 把 DIP 选区裁成像素图。
// 坐标与 MyGo 一致：主屏左上角为原点，y 向下。
// 像素位置用 DIPToPixel 一次算完，半像素四舍五入，0.5 远离 0。
package crop

import (
	"errors"
	"fmt"
	"image"
	"math"
)

// Screen 是一块已经冻结的显示器。
// Bounds 是 DIP。Pixels 的宽高等于 DIPToPixel(Bounds 的两条对边)。
type Screen struct {
	Bounds image.Rectangle
	Scale  float64
	Pixels *image.NRGBA
}

var (
	errNoOverlap = errors.New("选区不在屏幕上")
	errBadScale  = errors.New("显示器缩放无效")
	errNoPixels  = errors.New("没有可裁剪的画面")
)

// DIPToPixel 把一条 DIP 边换到像素。origin 是该屏 Bounds.Min 的对应分量。
func DIPToPixel(dip, origin int, scale float64) int {
	return int(math.Round(float64(dip-origin) * scale))
}

// Tile 是一块已按屏幕交集裁好的局部像素块（用于实时框选确认后的局部采集拼接）。
type Tile struct {
	Bounds image.Rectangle // 该局部块覆盖的全局 DIP 矩形
	Scale  float64
	Pixels *image.NRGBA
}

// PixelSize 返回选区 sel 裁剪后将生成的像素宽高，不要求 Screen.Pixels 已加载。
// 选区无效或没有命中合法屏幕时返回 0, 0。
func PixelSize(screens []Screen, sel image.Rectangle) (int, int) {
	sel = sel.Canon()
	if sel.Empty() {
		return 0, 0
	}
	var hit []Screen
	for _, s := range screens {
		if sel.Intersect(s.Bounds.Canon()).Empty() {
			continue
		}
		if !validScale(s.Scale) {
			return 0, 0
		}
		hit = append(hit, s)
	}
	switch len(hit) {
	case 0:
		return 0, 0
	case 1:
		b := hit[0].Bounds.Canon()
		inter := sel.Intersect(b)
		w := DIPToPixel(inter.Max.X, b.Min.X, hit[0].Scale) - DIPToPixel(inter.Min.X, b.Min.X, hit[0].Scale)
		h := DIPToPixel(inter.Max.Y, b.Min.Y, hit[0].Scale) - DIPToPixel(inter.Min.Y, b.Min.Y, hit[0].Scale)
		if w <= 0 || h <= 0 {
			return 0, 0
		}
		return w, h
	default:
		canvas := maxScale(hit)
		w := DIPToPixel(sel.Max.X, sel.Min.X, canvas)
		h := DIPToPixel(sel.Max.Y, sel.Min.Y, canvas)
		if w <= 0 || h <= 0 {
			return 0, 0
		}
		return w, h
	}
}

// Stitch 把多块屏各自局部截出的 Tile 拼接成一张完整选区图。
// 单块屏直接返回原图；跨多块不同缩放屏时按最大缩放最近邻放大拼接。
func Stitch(tiles []Tile, sel image.Rectangle) (*image.NRGBA, error) {
	sel = sel.Canon()
	if sel.Empty() || len(tiles) == 0 {
		return nil, errNoOverlap
	}
	for _, t := range tiles {
		if !validScale(t.Scale) {
			return nil, errBadScale
		}
		if t.Pixels == nil || t.Pixels.Bounds().Empty() {
			return nil, errNoPixels
		}
	}
	if len(tiles) == 1 {
		p := tiles[0].Pixels
		if p.Bounds().Min == (image.Point{}) {
			return p, nil
		}
		return copyRect(p, p.Bounds()), nil
	}
	canvas := tiles[0].Scale
	for _, t := range tiles[1:] {
		if t.Scale > canvas {
			canvas = t.Scale
		}
	}
	outW := DIPToPixel(sel.Max.X, sel.Min.X, canvas)
	outH := DIPToPixel(sel.Max.Y, sel.Min.Y, canvas)
	if outW <= 0 || outH <= 0 {
		return nil, errNoOverlap
	}
	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for _, t := range tiles {
		inter := sel.Intersect(t.Bounds.Canon())
		if inter.Empty() {
			continue
		}
		dr := image.Rect(
			DIPToPixel(inter.Min.X, sel.Min.X, canvas),
			DIPToPixel(inter.Min.Y, sel.Min.Y, canvas),
			DIPToPixel(inter.Max.X, sel.Min.X, canvas),
			DIPToPixel(inter.Max.Y, sel.Min.Y, canvas),
		)
		blit(dst, dr, t.Pixels, t.Pixels.Bounds())
	}
	return dst, nil
}

// Crop 裁出 sel 覆盖到的像素。sel 可以是反向拖出的。
// 只碰到一块屏时按该屏的缩放取像素，不重采样。
// 碰到多块屏时，画布用其中最大的缩放，较稀的区域按最近邻放大。
func Crop(screens []Screen, sel image.Rectangle) (*image.NRGBA, error) {
	sel = sel.Canon()
	if sel.Empty() {
		return nil, errNoOverlap
	}
	var hit []Screen
	for _, s := range screens {
		if sel.Intersect(s.Bounds.Canon()).Empty() {
			continue
		}
		if err := check(s); err != nil {
			return nil, err
		}
		hit = append(hit, s)
	}
	if len(hit) == 0 {
		return nil, errNoOverlap
	}
	if len(hit) == 1 {
		return cropOne(hit[0], sel)
	}
	return cropMany(hit, sel)
}

func validScale(scale float64) bool {
	return scale > 0 && !math.IsNaN(scale) && !math.IsInf(scale, 0)
}

func check(s Screen) error {
	if !validScale(s.Scale) {
		return errBadScale
	}
	if s.Pixels == nil {
		return errNoPixels
	}
	b := s.Bounds.Canon()
	wantW := DIPToPixel(b.Max.X, b.Min.X, s.Scale)
	wantH := DIPToPixel(b.Max.Y, b.Min.Y, s.Scale)
	if s.Pixels.Bounds().Dx() != wantW || s.Pixels.Bounds().Dy() != wantH {
		return fmt.Errorf("画面是 %dx%d 像素，按缩放应为 %dx%d", s.Pixels.Bounds().Dx(), s.Pixels.Bounds().Dy(), wantW, wantH)
	}
	return nil
}

func cropOne(s Screen, sel image.Rectangle) (*image.NRGBA, error) {
	inter := sel.Intersect(s.Bounds.Canon())
	src := pixelRect(s, inter)
	if src.Empty() {
		return nil, errNoOverlap
	}
	return copyRect(s.Pixels, src), nil
}

func maxScale(screens []Screen) float64 {
	canvas := screens[0].Scale
	for _, s := range screens[1:] {
		if s.Scale > canvas {
			canvas = s.Scale
		}
	}
	return canvas
}

func cropMany(screens []Screen, sel image.Rectangle) (*image.NRGBA, error) {
	canvas := maxScale(screens)
	outW := DIPToPixel(sel.Max.X, sel.Min.X, canvas)
	outH := DIPToPixel(sel.Max.Y, sel.Min.Y, canvas)
	if outW <= 0 || outH <= 0 {
		return nil, errNoOverlap
	}
	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for _, s := range screens {
		inter := sel.Intersect(s.Bounds.Canon())
		src := pixelRect(s, inter)
		if src.Empty() {
			continue
		}
		dr := image.Rect(
			DIPToPixel(inter.Min.X, sel.Min.X, canvas),
			DIPToPixel(inter.Min.Y, sel.Min.Y, canvas),
			DIPToPixel(inter.Max.X, sel.Min.X, canvas),
			DIPToPixel(inter.Max.Y, sel.Min.Y, canvas),
		)
		blit(dst, dr, s.Pixels, src)
	}
	return dst, nil
}

func pixelRect(s Screen, inter image.Rectangle) image.Rectangle {
	b := s.Bounds.Canon()
	r := image.Rect(
		DIPToPixel(inter.Min.X, b.Min.X, s.Scale),
		DIPToPixel(inter.Min.Y, b.Min.Y, s.Scale),
		DIPToPixel(inter.Max.X, b.Min.X, s.Scale),
		DIPToPixel(inter.Max.Y, b.Min.Y, s.Scale),
	)
	return r.Intersect(s.Pixels.Bounds())
}

func copyRect(src *image.NRGBA, r image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	blit(dst, dst.Bounds(), src, r)
	return dst
}

// blit 在尺寸一致时按行复制。尺寸不同时用最近邻，保证放大结果可以逐像素预测。
func blit(dst *image.NRGBA, dr image.Rectangle, src *image.NRGBA, sr image.Rectangle) {
	dw, dh := dr.Dx(), dr.Dy()
	sw, sh := sr.Dx(), sr.Dy()
	if dw <= 0 || dh <= 0 || sw <= 0 || sh <= 0 {
		return
	}
	if dw == sw && dh == sh {
		for y := 0; y < dh; y++ {
			d0 := dst.PixOffset(dr.Min.X, dr.Min.Y+y)
			s0 := src.PixOffset(sr.Min.X, sr.Min.Y+y)
			copy(dst.Pix[d0:d0+dw*4], src.Pix[s0:s0+sw*4])
		}
		return
	}
	for y := 0; y < dh; y++ {
		sy := sr.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := sr.Min.X + x*sw/dw
			dst.SetNRGBA(dr.Min.X+x, dr.Min.Y+y, src.NRGBAAt(sx, sy))
		}
	}
}
