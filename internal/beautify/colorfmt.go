package beautify

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"snipgo/internal/textrender"
)

// ColorFormat 表示取色器输出的开发者色值格式。
type ColorFormat int

const (
	ColorHEX ColorFormat = iota
	ColorRGB
	ColorOKLCH
	ColorTailwind
	colorFormatCount
)

// NextColorFormat 循环切换到下一种取色格式。
func NextColorFormat(f ColorFormat) ColorFormat {
	return (f + 1) % colorFormatCount
}

// ColorFormatName 返回取色格式短标签。
func ColorFormatName(f ColorFormat) string {
	switch f {
	case ColorRGB:
		return "RGB"
	case ColorOKLCH:
		return "OKLCH"
	case ColorTailwind:
		return "Tailwind"
	default:
		return "HEX"
	}
}

// FormatColor 按指定格式将颜色格式化为可直接粘贴进代码的色值文本。
func FormatColor(c color.NRGBA, f ColorFormat) string {
	switch f {
	case ColorRGB:
		return fmt.Sprintf("rgb(%d, %d, %d)", c.R, c.G, c.B)
	case ColorOKLCH:
		l, ch, h := ToOKLCH(c)
		if ch < 0.0005 {
			return fmt.Sprintf("oklch(%.3f 0 0)", l)
		}
		return fmt.Sprintf("oklch(%.3f %.3f %.1f)", l, ch, h)
	case ColorTailwind:
		return NearestTailwind(c)
	default:
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	}
}

// ToOKLCH 按 Björn Ottosson OKLab 规范将 sRGB 精确转换为感知均匀的 OKLCH (L∈[0,1], C≥0, H∈[0,360))。
func ToOKLCH(c color.NRGBA) (L, C, H float64) {
	l, a, b := srgbToOKLab(c)
	C = math.Hypot(a, b)
	H = math.Atan2(b, a) * 180.0 / math.Pi
	if H < 0 {
		H += 360.0
	}
	return l, C, H
}

func srgbToOKLab(c color.NRGBA) (L, a, b float64) {
	lin := func(u uint8) float64 {
		v := float64(u) / 255.0
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, bl := lin(c.R), lin(c.G), lin(c.B)
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*bl
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*bl
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*bl

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L = 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	a = 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	b = 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return L, a, b
}

type twColor struct {
	name    string
	r, g, b uint8
}

// 精简精选覆盖全部 Tailwind 色系与常用色阶，按 OKLab 感知距离匹配最接近色阶。
var tailwindPalette = []twColor{
	{"white", 255, 255, 255},
	{"black", 0, 0, 0},
	{"slate-50", 248, 250, 252}, {"slate-200", 226, 232, 240}, {"slate-400", 148, 163, 184}, {"slate-600", 71, 85, 105}, {"slate-800", 30, 41, 59}, {"slate-900", 15, 23, 42}, {"slate-950", 2, 6, 23},
	{"gray-100", 243, 244, 246}, {"gray-300", 209, 213, 219}, {"gray-500", 107, 114, 128}, {"gray-700", 55, 65, 81}, {"gray-900", 17, 24, 39},
	{"zinc-100", 244, 244, 245}, {"zinc-400", 161, 161, 170}, {"zinc-700", 63, 63, 70}, {"zinc-900", 24, 24, 27},
	{"red-300", 252, 165, 165}, {"red-400", 248, 113, 113}, {"red-500", 239, 68, 68}, {"red-600", 220, 38, 38}, {"red-700", 185, 28, 28},
	{"orange-300", 253, 186, 116}, {"orange-400", 251, 146, 60}, {"orange-500", 249, 115, 22}, {"orange-600", 234, 88, 12},
	{"amber-300", 252, 211, 77}, {"amber-400", 251, 191, 36}, {"amber-500", 245, 158, 11}, {"amber-600", 217, 119, 6},
	{"yellow-300", 253, 224, 71}, {"yellow-400", 250, 204, 21}, {"yellow-500", 234, 179, 8},
	{"lime-400", 163, 230, 53}, {"lime-500", 132, 204, 22}, {"lime-600", 101, 163, 13},
	{"green-300", 134, 239, 172}, {"green-400", 74, 222, 128}, {"green-500", 34, 197, 94}, {"green-600", 22, 163, 74}, {"green-700", 21, 128, 61},
	{"emerald-400", 52, 211, 153}, {"emerald-500", 16, 185, 129}, {"emerald-600", 5, 150, 105},
	{"teal-400", 45, 212, 191}, {"teal-500", 20, 184, 166}, {"teal-600", 13, 148, 136},
	{"cyan-400", 34, 211, 238}, {"cyan-500", 6, 182, 212}, {"cyan-600", 8, 145, 178},
	{"sky-300", 125, 211, 252}, {"sky-400", 56, 189, 248}, {"sky-500", 14, 165, 233}, {"sky-600", 2, 132, 199},
	{"blue-300", 147, 197, 253}, {"blue-400", 96, 165, 250}, {"blue-500", 59, 130, 246}, {"blue-600", 37, 99, 235}, {"blue-700", 29, 78, 216},
	{"indigo-400", 129, 140, 248}, {"indigo-500", 99, 102, 241}, {"indigo-600", 79, 70, 229},
	{"violet-400", 167, 139, 250}, {"violet-500", 139, 92, 246}, {"violet-600", 124, 58, 237},
	{"purple-400", 192, 132, 252}, {"purple-500", 168, 85, 247}, {"purple-600", 147, 51, 234},
	{"fuchsia-400", 232, 121, 249}, {"fuchsia-500", 217, 70, 239}, {"fuchsia-600", 192, 38, 211},
	{"pink-400", 244, 114, 182}, {"pink-500", 236, 72, 153}, {"pink-600", 219, 39, 119},
	{"rose-400", 251, 113, 133}, {"rose-500", 244, 63, 94}, {"rose-600", 225, 29, 72},
}

// NearestTailwind 在 OKLab 感知颜色空间中匹配最接近的标准 Tailwind 色阶名（如 "sky-500"）。
func NearestTailwind(c color.NRGBA) string {
	l0, a0, b0 := srgbToOKLab(c)
	bestName := "blue-500"
	bestDist := math.MaxFloat64
	for _, tw := range tailwindPalette {
		l1, a1, b1 := srgbToOKLab(color.NRGBA{R: tw.r, G: tw.g, B: tw.b, A: 255})
		dl, da, db := l0-l1, a0-a1, b0-b1
		d := dl*dl + da*da + db*db
		if d < bestDist {
			bestDist = d
			bestName = tw.name
		}
	}
	return bestName
}

// RenderPaletteCard 将多色板采集列表渲染为横向高分屏设计师配色卡（单色时展示全部 4 种开发者格式）。
func RenderPaletteCard(colors []color.NRGBA, scale float64) *image.NRGBA {
	if len(colors) == 0 {
		return nil
	}
	if scale <= 0 {
		scale = 2.0
	}
	if len(colors) == 1 {
		return renderColorSwatchCard(colors[0], scale)
	}
	if len(colors) > 8 {
		colors = colors[:8]
	}

	pad := int(math.Round(16 * scale))
	gap := int(math.Round(10 * scale))
	cardW := int(math.Round(106 * scale))
	swatchH := int(math.Round(66 * scale))
	labelH := int(math.Round(46 * scale))
	w := pad*2 + len(colors)*cardW + (len(colors)-1)*gap
	h := pad*2 + swatchH + labelH

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillSolid(out, color.NRGBA{R: 24, G: 25, B: 30, A: 255})

	swRad := 8.0 * scale
	for i, col := range colors {
		x0 := pad + i*(cardW+gap)
		y0 := pad
		cx := float64(x0) + float64(cardW)*0.5
		cy := float64(y0) + float64(swatchH)*0.5
		hx := float64(cardW) * 0.5
		hy := float64(swatchH) * 0.5
		for y := y0; y < y0+swatchH; y++ {
			for x := x0; x < x0+cardW; x++ {
				d := roundedRectSDF(float64(x)+0.5-cx, float64(y)+0.5-cy, hx, hy, swRad)
				if d < 0.5 {
					cov := 1.0
					if d > -0.5 {
						cov = 0.5 - d
					}
					blendPixel(out, x, y, col, cov)
				}
			}
		}

		hexImg := textrender.Render(FormatColor(col, ColorHEX), textrender.Options{
			FontSize:  12.5 * scale,
			Weight:    0.4,
			Monospace: true,
			Color:     color.NRGBA{R: 246, G: 247, B: 252, A: 255},
		})
		twImg := textrender.Render(NearestTailwind(col), textrender.Options{
			FontSize:  10.5 * scale,
			Weight:    0.2,
			Monospace: true,
			Color:     color.NRGBA{R: 150, G: 158, B: 174, A: 255},
		})
		if hexImg != nil {
			stamp(out, hexImg, x0+int(math.Round(2*scale)), y0+swatchH+int(math.Round(8*scale)))
		}
		if twImg != nil {
			stamp(out, twImg, x0+int(math.Round(2*scale)), y0+swatchH+int(math.Round(27*scale)))
		}
	}
	return out
}
