// Package beautify 提供截图圆角阴影渐变背景美化卡片，以及剪贴板文字/代码/色号悬浮卡片生成。
package beautify

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"snipgo/internal/textrender"
)

// Preset 表示美化背景预设风格。
type Preset int

const (
	PresetOcean Preset = iota
	PresetSunset
	PresetEmerald
	PresetSlate
)

type gradientSpec struct {
	c0, c1, c2 color.NRGBA
}

var presets = [...]gradientSpec{
	PresetOcean: {
		c0: color.NRGBA{R: 15, G: 118, B: 230, A: 255},
		c1: color.NRGBA{R: 79, G: 70, B: 229, A: 255},
		c2: color.NRGBA{R: 14, G: 165, B: 233, A: 255},
	},
	PresetSunset: {
		c0: color.NRGBA{R: 249, G: 115, B: 22, A: 255},
		c1: color.NRGBA{R: 236, G: 72, B: 153, A: 255},
		c2: color.NRGBA{R: 139, G: 92, B: 246, A: 255},
	},
	PresetEmerald: {
		c0: color.NRGBA{R: 16, G: 185, B: 129, A: 255},
		c1: color.NRGBA{R: 6, G: 148, B: 162, A: 255},
		c2: color.NRGBA{R: 30, G: 64, B: 175, A: 255},
	},
	PresetSlate: {
		c0: color.NRGBA{R: 39, G: 39, B: 46, A: 255},
		c1: color.NRGBA{R: 24, G: 24, B: 30, A: 255},
		c2: color.NRGBA{R: 52, G: 56, B: 68, A: 255},
	},
}

// PresetName 返回预设中文名称。
func PresetName(p Preset) string {
	switch p {
	case PresetSunset:
		return "落日霓虹"
	case PresetEmerald:
		return "翡翠极光"
	case PresetSlate:
		return "深空暗灰"
	default:
		return "海湾深蓝"
	}
}

// PresetColors 返回预设起止渐变色，供工具栏微缩色卡预览使用。
func PresetColors(p Preset) (color.NRGBA, color.NRGBA) {
	if int(p) < 0 || int(p) >= len(presets) {
		p = PresetOcean
	}
	sp := presets[p]
	return sp.c0, sp.c1
}

// RenderCard 给原图 src 添加平滑圆角、多层柔和投影与渐变衬底背景。
func RenderCard(src *image.NRGBA, scale float64, preset Preset) *image.NRGBA {
	if src == nil || src.Bounds().Empty() {
		return src
	}
	if scale <= 0 {
		scale = 2.0
	}
	if int(preset) < 0 || int(preset) >= len(presets) {
		preset = PresetOcean
	}
	spec := presets[preset]
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	pad := max(32, int(math.Round(44*scale)))
	extraBottom := int(math.Round(8 * scale))
	outW := sw + pad*2
	outH := sh + pad*2 + extraBottom
	radius := min(12.0*scale, float64(min(sw, sh))*0.2)

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	fillGradient(out, spec)

	ix0, iy0 := float64(pad), float64(pad)
	ix1, iy1 := float64(pad+sw), float64(pad+sh)
	cx, cy := (ix0+ix1)*0.5, (iy0+iy1)*0.5
	hx, hy := float64(sw)*0.5, float64(sh)*0.5

	// 柔和圆角阴影（基于圆角矩形 SDF 衰减）。
	shBlur := 26.0 * scale
	shOffY := 10.0 * scale
	shBox := image.Rect(
		max(0, int(ix0-shBlur-4)),
		max(0, int(iy0+shOffY-shBlur-4)),
		min(outW, int(math.Ceil(ix1+shBlur+4))),
		min(outH, int(math.Ceil(iy1+shOffY+shBlur+4))),
	)
	for y := shBox.Min.Y; y < shBox.Max.Y; y++ {
		py := float64(y) + 0.5
		for x := shBox.Min.X; x < shBox.Max.X; x++ {
			px := float64(x) + 0.5
			d := roundedRectSDF(px-cx, py-(cy+shOffY), hx, hy, radius)
			if d >= shBlur {
				continue
			}
			t := max(0, min(1, 1.0-max(0, d)/shBlur))
			alpha := 0.42 * t * t * (3 - 2*t)
			blendPixel(out, x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 255}, alpha)
		}
	}

	// 绘制圆角裁剪后的原图与 1px 高光细描边。
	for y := 0; y < sh; y++ {
		dy := pad + y
		py := float64(dy) + 0.5
		for x := 0; x < sw; x++ {
			dx := pad + x
			px := float64(dx) + 0.5
			d := roundedRectSDF(px-cx, py-cy, hx, hy, radius)
			if d >= 0.5 {
				continue
			}
			cov := 1.0
			if d > -0.5 {
				cov = 0.5 - d
			}
			pxCol := src.NRGBAAt(sb.Min.X+x, sb.Min.Y+y)
			blendPixel(out, dx, dy, pxCol, cov*(float64(pxCol.A)/255.0))
			// 边缘 1px 微弱白边提升精致感
			rimDist := math.Abs(d+0.5*scale) - 0.5*scale
			if rimDist < 0.5 {
				rimCov := max(0, min(1, 0.5-rimDist)) * 0.22
				blendPixel(out, dx, dy, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, rimCov)
			}
		}
	}
	return out
}

var (
	hexColorRe = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	rgbColorRe = regexp.MustCompile(`(?i)^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*[0-9.]+)?\s*\)$`)
)

// ParseColor 尝试把剪贴板字符串解析为颜色值（支持 #RGB / #RRGGBB / rgb(...)）。
func ParseColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if m := hexColorRe.FindStringSubmatch(s); m != nil {
		// 仅当带 # 前缀或刚好 6 位十六进制且全为常见色值格式时视为色号（带 # 强制识别，不带 # 要求带 # 避免误判普通单词）。
		if !strings.HasPrefix(s, "#") {
			return color.NRGBA{}, false
		}
		h := m[1]
		switch len(h) {
		case 3:
			r, _ := strconv.ParseUint(strings.Repeat(string(h[0]), 2), 16, 8)
			g, _ := strconv.ParseUint(strings.Repeat(string(h[1]), 2), 16, 8)
			b, _ := strconv.ParseUint(strings.Repeat(string(h[2]), 2), 16, 8)
			return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, true
		case 6, 8:
			r, _ := strconv.ParseUint(h[0:2], 16, 8)
			g, _ := strconv.ParseUint(h[2:4], 16, 8)
			b, _ := strconv.ParseUint(h[4:6], 16, 8)
			a := uint64(255)
			if len(h) == 8 {
				a, _ = strconv.ParseUint(h[6:8], 16, 8)
			}
			return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, true
		}
	}
	if m := rgbColorRe.FindStringSubmatch(s); m != nil {
		r, err1 := strconv.Atoi(m[1])
		g, err2 := strconv.Atoi(m[2])
		b, err3 := strconv.Atoi(m[3])
		if err1 == nil && err2 == nil && err3 == nil && r <= 255 && g <= 255 && b <= 255 {
			return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, true
		}
	}
	return color.NRGBA{}, false
}

// RenderTextPin 把剪贴板中的色号、代码片段或普通文本渲染成精致的高分屏悬浮贴图卡片。
func RenderTextPin(raw string, scale float64) *image.NRGBA {
	if scale <= 0 {
		scale = 2.0
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if col, ok := ParseColor(trimmed); ok {
		return renderColorSwatchCard(col, scale)
	}
	return renderNoteCard(raw, scale)
}

func renderColorSwatchCard(col color.NRGBA, scale float64) *image.NRGBA {
	w := int(math.Round(240 * scale))
	h := int(math.Round(96 * scale))
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	bg := color.NRGBA{R: 28, G: 28, B: 34, A: 255}
	fillSolid(out, bg)

	swSize := int(math.Round(64 * scale))
	pad := int(math.Round(16 * scale))
	for y := pad; y < pad+swSize; y++ {
		for x := pad; x < pad+swSize; x++ {
			out.SetNRGBA(x, y, col)
		}
	}

	hexStr := fmt.Sprintf("#%02X%02X%02X", col.R, col.G, col.B)
	rgbStr := fmt.Sprintf("rgb(%d, %d, %d)", col.R, col.G, col.B)
	title := textrender.Render(hexStr, textrender.Options{
		FontSize:  18 * scale,
		Weight:    0.4,
		Monospace: true,
		Color:     color.NRGBA{R: 250, G: 250, B: 252, A: 255},
	})
	sub := textrender.Render(rgbStr, textrender.Options{
		FontSize:  12.5 * scale,
		Weight:    0.2,
		Monospace: true,
		Color:     color.NRGBA{R: 168, G: 172, B: 184, A: 255},
	})
	tx := pad*2 + swSize
	if title != nil {
		stamp(out, title, tx, int(math.Round(22*scale)))
	}
	if sub != nil {
		stamp(out, sub, tx, int(math.Round(52*scale)))
	}
	return out
}

func renderNoteCard(raw string, scale float64) *image.NRGBA {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	lines := strings.Split(text, "\n")
	if len(lines) > 36 {
		lines = append(lines[:36], "…")
		text = strings.Join(lines, "\n")
	}
	isCode := looksLikeCode(text)
	txtImg := textrender.Render(text, textrender.Options{
		FontSize:  13.5 * scale,
		Weight:    0.2,
		Monospace: isCode,
		MaxWidth:  int(math.Round(560 * scale)),
		Color:     color.NRGBA{R: 238, G: 240, B: 245, A: 255},
	})
	if txtImg == nil {
		return nil
	}
	tb := txtImg.Bounds()
	padX := int(math.Round(18 * scale))
	padY := int(math.Round(16 * scale))
	topBar := int(math.Round(4 * scale))
	w := max(int(math.Round(160*scale)), tb.Dx()+padX*2)
	h := max(int(math.Round(56*scale)), tb.Dy()+padY*2+topBar)

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillSolid(out, color.NRGBA{R: 26, G: 27, B: 32, A: 255})

	accent := color.NRGBA{R: 0, G: 168, B: 255, A: 255}
	if isCode {
		accent = color.NRGBA{R: 52, G: 199, B: 89, A: 255}
	}
	for y := 0; y < topBar; y++ {
		for x := 0; x < w; x++ {
			out.SetNRGBA(x, y, accent)
		}
	}
	stamp(out, txtImg, padX, topBar+padY)
	return out
}

func looksLikeCode(s string) bool {
	for _, tok := range []string{"func ", "import ", "const ", "return ", "=>", "{", "}", "SELECT ", "git ", "go ", "npm ", "curl "} {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

func fillSolid(dst *image.NRGBA, c color.NRGBA) {
	b := dst.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.SetNRGBA(x, y, c)
		}
	}
}

func stamp(dst, src *image.NRGBA, ox, oy int) {
	sb := src.Bounds()
	db := dst.Bounds()
	for y := 0; y < sb.Dy(); y++ {
		dy := oy + y
		if dy < db.Min.Y || dy >= db.Max.Y {
			continue
		}
		for x := 0; x < sb.Dx(); x++ {
			dx := ox + x
			if dx < db.Min.X || dx >= db.Max.X {
				continue
			}
			c := src.NRGBAAt(sb.Min.X+x, sb.Min.Y+y)
			if c.A > 0 {
				blendPixel(dst, dx, dy, c, float64(c.A)/255.0)
			}
		}
	}
}

func fillGradient(dst *image.NRGBA, g gradientSpec) {
	b := dst.Bounds()
	w := float64(max(1, b.Dx()-1))
	h := float64(max(1, b.Dy()-1))
	for y := 0; y < b.Dy(); y++ {
		fy := float64(y) / h
		for x := 0; x < b.Dx(); x++ {
			fx := float64(x) / w
			t := (fx*0.65 + fy*0.35)
			var col color.NRGBA
			if t < 0.5 {
				col = lerpColor(g.c0, g.c1, t*2)
			} else {
				col = lerpColor(g.c1, g.c2, (t-0.5)*2)
			}
			dst.SetNRGBA(b.Min.X+x, b.Min.Y+y, col)
		}
	}
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	t = max(0, min(1, t))
	return color.NRGBA{
		R: uint8(math.Round(float64(a.R) + (float64(b.R)-float64(a.R))*t)),
		G: uint8(math.Round(float64(a.G) + (float64(b.G)-float64(a.G))*t)),
		B: uint8(math.Round(float64(a.B) + (float64(b.B)-float64(a.B))*t)),
		A: 255,
	}
}

func roundedRectSDF(dx, dy, hx, hy, r float64) float64 {
	qx := math.Abs(dx) - (hx - r)
	qy := math.Abs(dy) - (hy - r)
	return math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
}

func blendPixel(dst *image.NRGBA, x, y int, src color.NRGBA, alpha float64) {
	if alpha <= 0 {
		return
	}
	if alpha >= 0.999 {
		dst.SetNRGBA(x, y, color.NRGBA{R: src.R, G: src.G, B: src.B, A: 255})
		return
	}
	bg := dst.NRGBAAt(x, y)
	r := float64(src.R)*alpha + float64(bg.R)*(1-alpha)
	g := float64(src.G)*alpha + float64(bg.G)*(1-alpha)
	b := float64(src.B)*alpha + float64(bg.B)*(1-alpha)
	dst.SetNRGBA(x, y, color.NRGBA{
		R: uint8(min(255, max(0, math.Round(r)))),
		G: uint8(min(255, max(0, math.Round(g)))),
		B: uint8(min(255, max(0, math.Round(b)))),
		A: 255,
	})
}
