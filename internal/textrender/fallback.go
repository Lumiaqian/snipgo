package textrender

import (
	"image"
	"math"
	"strings"
)

// renderFallback 在无原生字体环境时提供一个紧凑的像素块兜底，保证跨平台单测可运行。
func renderFallback(text string, opts Options) *image.NRGBA {
	scale := max(1, int(math.Round(opts.FontSize/12)))
	charW := 7 * scale
	charH := 12 * scale
	lines := strings.Split(text, "\n")
	maxRunes := 1
	for _, ln := range lines {
		if n := len([]rune(ln)); n > maxRunes {
			maxRunes = n
		}
	}
	w := maxRunes*charW + 4
	h := len(lines)*charH + 4
	if opts.MaxWidth > 0 && w > opts.MaxWidth {
		w = opts.MaxWidth
	}
	img := image.NewNRGBA(image.Rect(0, 0, max(4, w), max(4, h)))
	for li, ln := range lines {
		y0 := 2 + li*charH
		for ci, r := range []rune(ln) {
			if r == ' ' || r == '\t' {
				continue
			}
			x0 := 2 + ci*charW
			for dy := scale; dy < charH-scale; dy++ {
				for dx := scale; dx < charW-scale; dx++ {
					px, py := x0+dx, y0+dy
					if px < img.Bounds().Dx() && py < img.Bounds().Dy() {
						img.SetNRGBA(px, py, opts.Color)
					}
				}
			}
		}
	}
	return img
}
