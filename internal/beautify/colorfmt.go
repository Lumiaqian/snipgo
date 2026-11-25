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

// ColorFormatKey 返回用于配置持久化的稳定小写格式键名。
func ColorFormatKey(f ColorFormat) string {
	switch f {
	case ColorRGB:
		return "rgb"
	case ColorOKLCH:
		return "oklch"
	case ColorTailwind:
		return "tailwind"
	default:
		return "hex"
	}
}

// ParseColorFormat 将持久化键名解析为 ColorFormat（未知值默认回退 ColorHEX）。
func ParseColorFormat(s string) ColorFormat {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "rgb":
		return ColorRGB
	case "oklch":
		return ColorOKLCH
	case "tailwind":
		return ColorTailwind
	default:
		return ColorHEX
	}
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

// FormatColor 按指定格式将颜色格式化为可直接粘贴进代码的色值文本（含格式前缀）。
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

// FormatColorRaw 返回去掉函数/符号前缀的纯色值文本（用于 Shift+C 快捷复制）。
func FormatColorRaw(c color.NRGBA, f ColorFormat) string {
	switch f {
	case ColorRGB:
		return fmt.Sprintf("%d, %d, %d", c.R, c.G, c.B)
	case ColorOKLCH:
		l, ch, h := ToOKLCH(c)
		if ch < 0.0005 {
			return fmt.Sprintf("%.3f 0 0", l)
		}
		return fmt.Sprintf("%.3f %.3f %.1f", l, ch, h)
	case ColorTailwind:
		return NearestTailwind(c)
	default:
		return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
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

func srgbLinear(u uint8) float64 {
	v := float64(u) / 255.0
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGBByte(v float64) uint8 {
	v = max(0, min(1, v))
	var s float64
	if v <= 0.0031308 {
		s = 12.92 * v
	} else {
		s = 1.055*math.Pow(v, 1.0/2.4) - 0.055
	}
	return uint8(max(0, min(255, math.Round(s*255.0))))
}

func srgbToOKLab(c color.NRGBA) (L, a, b float64) {
	r, g, bl := srgbLinear(c.R), srgbLinear(c.G), srgbLinear(c.B)
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

func oklchToNRGBA(L, C, H, alpha float64) color.NRGBA {
	hRad := H * math.Pi / 180.0
	a := C * math.Cos(hRad)
	b := C * math.Sin(hRad)

	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	rLin := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	gLin := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bLin := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	return color.NRGBA{
		R: linearToSRGBByte(rLin),
		G: linearToSRGBByte(gLin),
		B: linearToSRGBByte(bLin),
		A: uint8(max(0, min(255, math.Round(max(0, min(1, alpha))*255.0)))),
	}
}

var oklchColorRe = regexp.MustCompile(`(?i)^oklch\(\s*([^\s/]+)\s+([^\s/]+)\s+([^\s/)]+)(?:\s*/\s*([^\s)]+))?\s*\)$`)

func parseOKLCH(s string) (color.NRGBA, bool) {
	m := oklchColorRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return color.NRGBA{}, false
	}
	L, okL := parseUnitFloat(m[1], 1.0)
	if !okL || L < 0 || L > 1 {
		return color.NRGBA{}, false
	}
	C, okC := parseUnitFloat(m[2], 0.4)
	if !okC || C < 0 {
		return color.NRGBA{}, false
	}
	H, okH := parseHueDeg(m[3])
	if !okH {
		return color.NRGBA{}, false
	}
	alpha := 1.0
	if m[4] != "" {
		a, okA := parseUnitFloat(m[4], 1.0)
		if !okA || a < 0 || a > 1 {
			return color.NRGBA{}, false
		}
		alpha = a
	}
	return oklchToNRGBA(L, C, H, alpha), true
}

func parseUnitFloat(tok string, pctScale float64) (float64, bool) {
	tok = strings.TrimSpace(strings.ToLower(tok))
	if tok == "none" {
		return 0, true
	}
	if num, ok := strings.CutSuffix(tok, "%"); ok {
		v, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, false
		}
		return (v / 100.0) * pctScale, true
	}
	v, err := strconv.ParseFloat(tok, 64)
	return v, err == nil
}

func parseHueDeg(tok string) (float64, bool) {
	tok = strings.TrimSpace(strings.ToLower(tok))
	if tok == "none" {
		return 0, true
	}
	scale := 1.0
	switch {
	case strings.HasSuffix(tok, "deg"):
		tok = strings.TrimSuffix(tok, "deg")
	case strings.HasSuffix(tok, "rad"):
		tok = strings.TrimSuffix(tok, "rad")
		scale = 180.0 / math.Pi
	case strings.HasSuffix(tok, "grad"):
		tok = strings.TrimSuffix(tok, "grad")
		scale = 0.9
	case strings.HasSuffix(tok, "turn"):
		tok = strings.TrimSuffix(tok, "turn")
		scale = 360.0
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, false
	}
	return v * scale, true
}

type twColor struct {
	name     string
	r, g, b  uint8
	l, a, bv float64
}

var (
	tailwindShades   = [11]string{"50", "100", "200", "300", "400", "500", "600", "700", "800", "900", "950"}
	tailwindFamilies = []struct {
		name string
		hex  [11]uint32
	}{
		{"slate", [11]uint32{0xf8fafc, 0xf1f5f9, 0xe2e8f0, 0xcbd5e1, 0x94a3b8, 0x64748b, 0x475569, 0x334155, 0x1e293b, 0x0f172a, 0x020617}},
		{"gray", [11]uint32{0xf9fafb, 0xf3f4f6, 0xe5e7eb, 0xd1d5db, 0x9ca3af, 0x6b7280, 0x4b5563, 0x374151, 0x1f2937, 0x111827, 0x030712}},
		{"zinc", [11]uint32{0xfafafa, 0xf4f4f5, 0xe4e4e7, 0xd4d4d8, 0xa1a1aa, 0x71717a, 0x52525b, 0x3f3f46, 0x27272a, 0x18181b, 0x09090b}},
		{"neutral", [11]uint32{0xfafafa, 0xf5f5f5, 0xe5e5e5, 0xd4d4d4, 0xa3a3a3, 0x737373, 0x525252, 0x404040, 0x262626, 0x171717, 0x0a0a0a}},
		{"stone", [11]uint32{0xfafaf9, 0xf5f5f4, 0xe7e5e4, 0xd6d3d1, 0xa8a29e, 0x78716c, 0x57534e, 0x44403c, 0x292524, 0x1c1917, 0x0c0a09}},
		{"red", [11]uint32{0xfef2f2, 0xfee2e2, 0xfecaca, 0xfca5a5, 0xf87171, 0xef4444, 0xdc2626, 0xb91c1c, 0x991b1b, 0x7f1d1d, 0x450a0a}},
		{"orange", [11]uint32{0xfff7ed, 0xffedd5, 0xfed7aa, 0xfdba74, 0xfb923c, 0xf97316, 0xea580c, 0xc2410c, 0x9a3412, 0x7c2d12, 0x431407}},
		{"amber", [11]uint32{0xfffbeb, 0xfef3c7, 0xfde68a, 0xfcd34d, 0xfbbf24, 0xf59e0b, 0xd97706, 0xb45309, 0x92400e, 0x78350f, 0x451a03}},
		{"yellow", [11]uint32{0xfefce8, 0xfef9c3, 0xfef08a, 0xfde047, 0xfacc15, 0xeab308, 0xca8a04, 0xa16207, 0x854d0e, 0x713f12, 0x422006}},
		{"lime", [11]uint32{0xf7fee7, 0xecfccb, 0xd9f99d, 0xbef264, 0xa3e635, 0x84cc16, 0x65a30d, 0x4d7c0f, 0x3f6212, 0x365314, 0x1a2e05}},
		{"green", [11]uint32{0xf0fdf4, 0xdcfce7, 0xbbf7d0, 0x86efac, 0x4ade80, 0x22c55e, 0x16a34a, 0x15803d, 0x166534, 0x14532d, 0x052e16}},
		{"emerald", [11]uint32{0xecfdf5, 0xd1fae5, 0xa7f3d0, 0x6ee7b7, 0x34d399, 0x10b981, 0x059669, 0x047857, 0x065f46, 0x064e3b, 0x022c22}},
		{"teal", [11]uint32{0xf0fdfa, 0xccfbf1, 0x99f6e4, 0x5eead4, 0x2dd4bf, 0x14b8a6, 0x0d9488, 0x0f766e, 0x115e59, 0x134e4a, 0x042f2e}},
		{"cyan", [11]uint32{0xecfeff, 0xcffafe, 0xa5f3fc, 0x67e8f9, 0x22d3ee, 0x06b6d4, 0x0891b2, 0x0e7490, 0x155e75, 0x164e63, 0x083344}},
		{"sky", [11]uint32{0xf0f9ff, 0xe0f2fe, 0xbae6fd, 0x7dd3fc, 0x38bdf8, 0x0ea5e9, 0x0284c7, 0x0369a1, 0x075985, 0x0c4a6e, 0x082f49}},
		{"blue", [11]uint32{0xeff6ff, 0xdbeafe, 0xbfdbfe, 0x93c5fd, 0x60a5fa, 0x3b82f6, 0x2563eb, 0x1d4ed8, 0x1e40af, 0x1e3a8a, 0x172554}},
		{"indigo", [11]uint32{0xeef2ff, 0xe0e7ff, 0xc7d2fe, 0xa5b4fc, 0x818cf8, 0x6366f1, 0x4f46e5, 0x4338ca, 0x3730a3, 0x312e81, 0x1e1b4b}},
		{"violet", [11]uint32{0xf5f3ff, 0xede9fe, 0xddd6fe, 0xc4b5fd, 0xa78bfa, 0x8b5cf6, 0x7c3aed, 0x6d28d9, 0x5b21b6, 0x4c1d95, 0x2e1065}},
		{"purple", [11]uint32{0xfaf5ff, 0xf3e8ff, 0xe9d5ff, 0xd8b4fe, 0xc084fc, 0xa855f7, 0x9333ea, 0x7e22ce, 0x6b21a8, 0x581c87, 0x3b0764}},
		{"fuchsia", [11]uint32{0xfdf4ff, 0xfae8ff, 0xf5d0fe, 0xf0abfc, 0xe879f9, 0xd946ef, 0xc026d3, 0xa21caf, 0x86198f, 0x701a75, 0x4a044e}},
		{"pink", [11]uint32{0xfdf2f8, 0xfce7f3, 0xfbcfe8, 0xf9a8d4, 0xf472b6, 0xec4899, 0xdb2777, 0xbe185d, 0x9d174d, 0x831843, 0x500724}},
		{"rose", [11]uint32{0xfff1f2, 0xffe4e6, 0xfecdd3, 0xfda4af, 0xfb7185, 0xf43f5e, 0xe11d48, 0xbe123c, 0x9f1239, 0x881337, 0x4c0519}},
	}
	tailwindPalette []twColor
	tailwindByName  map[string]color.NRGBA
)

func init() {
	tailwindPalette = make([]twColor, 0, 2+len(tailwindFamilies)*len(tailwindShades))
	tailwindByName = make(map[string]color.NRGBA, len(tailwindFamilies)*len(tailwindShades)+2)
	add := func(name string, r, g, b uint8) {
		l, a, bv := srgbToOKLab(color.NRGBA{R: r, G: g, B: b, A: 255})
		tailwindPalette = append(tailwindPalette, twColor{name: name, r: r, g: g, b: b, l: l, a: a, bv: bv})
		tailwindByName[name] = color.NRGBA{R: r, G: g, B: b, A: 255}
	}
	add("white", 255, 255, 255)
	add("black", 0, 0, 0)
	for _, fam := range tailwindFamilies {
		for i, sh := range tailwindShades {
			v := fam.hex[i]
			add(fam.name+"-"+sh, uint8(v>>16), uint8(v>>8), uint8(v))
		}
	}
}

func parseTailwindToken(s string) (color.NRGBA, bool) {
	tok := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "≈"))
	tok = strings.ToLower(strings.TrimSpace(tok))
	if !strings.Contains(tok, "-") {
		return color.NRGBA{}, false
	}
	col, ok := tailwindByName[tok]
	return col, ok
}

// MatchTailwind 在 OKLab 感知颜色空间中匹配最接近的官方 Tailwind 色阶，并返回是否精确命中与 OKLab 距离。
func MatchTailwind(c color.NRGBA) (name string, exact bool, dist float64) {
	l0, a0, b0 := srgbToOKLab(c)
	bestName := "blue-500"
	bestDistSq := math.MaxFloat64
	for _, tw := range tailwindPalette {
		if c.R == tw.r && c.G == tw.g && c.B == tw.b {
			return tw.name, true, 0
		}
		dl, da, db := l0-tw.l, a0-tw.a, b0-tw.bv
		d2 := dl*dl + da*da + db*db
		if d2 < bestDistSq {
			bestDistSq = d2
			bestName = tw.name
		}
	}
	dist = math.Sqrt(bestDistSq)
	return bestName, dist < 0.002, dist
}

// NearestTailwind 在 OKLab 感知颜色空间中匹配最接近的标准 Tailwind 色阶名（如 "sky-500"）。
func NearestTailwind(c color.NRGBA) string {
	name, _, _ := MatchTailwind(c)
	return name
}

// FormatTailwindLabel 返回带近似标识的 Tailwind 色阶展示文本（精确命中如 "sky-500"，近似如 "≈ sky-500"）。
func FormatTailwindLabel(c color.NRGBA) string {
	name, exact, _ := MatchTailwind(c)
	if exact {
		return name
	}
	return "≈ " + name
}

// RelativeLuminance 按 WCAG 2.1 定义返回颜色的相对亮度（0..1）。
func RelativeLuminance(c color.NRGBA) float64 {
	return 0.2126*srgbLinear(c.R) + 0.7152*srgbLinear(c.G) + 0.0722*srgbLinear(c.B)
}

// ContrastRatio 计算两色间的 WCAG 2.1 对比度（1.0..21.0）。
func ContrastRatio(a, b color.NRGBA) float64 {
	la := RelativeLuminance(a)
	lb := RelativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// WCAGGrade 返回给定对比度对应的 WCAG 等级（AAA / AA / AA Large / Fail）。
func WCAGGrade(ratio float64) string {
	switch {
	case ratio >= 7.0:
		return "AAA"
	case ratio >= 4.5:
		return "AA"
	case ratio >= 3.0:
		return "AA Large"
	default:
		return "Fail"
	}
}

// FormatContrastSummary 返回两色对比度摘要文本（如 "4.61:1 · AA"）。
func FormatContrastSummary(a, b color.NRGBA) string {
	ratio := ContrastRatio(a, b)
	return fmt.Sprintf("%.2f:1 · %s", ratio, WCAGGrade(ratio))
}

// FormatPaletteCSS 将多色板导出为可直接粘贴使用的 :root CSS 变量代码块。
func FormatPaletteCSS(colors []color.NRGBA, f ColorFormat) string {
	if len(colors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(":root {\n")
	used := make(map[string]int, len(colors))
	for i, col := range colors {
		if f == ColorTailwind {
			tok := NearestTailwind(col)
			used[tok]++
			varName := tok
			if used[tok] > 1 {
				varName = fmt.Sprintf("%s-%d", tok, used[tok])
			}
			fmt.Fprintf(&b, "  --%s: %s;\n", varName, FormatColor(col, ColorHEX))
			continue
		}
		fmt.Fprintf(&b, "  --color-%d: %s; /* %s */\n", i+1, FormatColor(col, f), NearestTailwind(col))
	}
	b.WriteString("}")
	return b.String()
}

const (
	paletteCardPadDIP      = 16
	paletteCardGapDIP      = 10
	paletteCardWidthDIP    = 106
	paletteSwatchHeightDIP = 66
	paletteLabelHeightDIP  = 46
	paletteContrastBarHDIP = 30
	paletteMaxColors       = 8
)

// PaletteCardLayout 返回 n 个颜色（n∈[2,8]）多色卡的 1x DIP 宽高与每个色块列的可点击矩形。
func PaletteCardLayout(n int) (dipW, dipH int, swatchRects []image.Rectangle) {
	if n <= 0 {
		return 0, 0, nil
	}
	if n > paletteMaxColors {
		n = paletteMaxColors
	}
	if n == 1 {
		dipW, dipH, _ = SwatchCardRowRects()
		return dipW, dipH, nil
	}
	dipW = paletteCardPadDIP*2 + n*paletteCardWidthDIP + (n-1)*paletteCardGapDIP
	dipH = paletteCardPadDIP*2 + paletteSwatchHeightDIP + paletteLabelHeightDIP
	if n == 2 {
		dipH += paletteContrastBarHDIP
	}
	swatchRects = make([]image.Rectangle, n)
	colH := paletteSwatchHeightDIP + paletteLabelHeightDIP
	for i := range n {
		x0 := paletteCardPadDIP + i*(paletteCardWidthDIP+paletteCardGapDIP)
		swatchRects[i] = image.Rect(x0, paletteCardPadDIP, x0+paletteCardWidthDIP, paletteCardPadDIP+colH)
	}
	return dipW, dipH, swatchRects
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
	if len(colors) > paletteMaxColors {
		colors = colors[:paletteMaxColors]
	}

	dipW, dipH, _ := PaletteCardLayout(len(colors))
	pad := int(math.Round(float64(paletteCardPadDIP) * scale))
	gap := int(math.Round(float64(paletteCardGapDIP) * scale))
	cardW := int(math.Round(float64(paletteCardWidthDIP) * scale))
	swatchH := int(math.Round(float64(paletteSwatchHeightDIP) * scale))
	labelH := int(math.Round(float64(paletteLabelHeightDIP) * scale))
	w := int(math.Round(float64(dipW) * scale))
	h := int(math.Round(float64(dipH) * scale))

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillSolid(out, color.NRGBA{R: 24, G: 25, B: 30, A: 255})

	swRad := 8.0 * scale
	for i, col := range colors {
		x0 := pad + i*(cardW+gap)
		y0 := pad
		drawRoundedSwatch(out, x0, y0, cardW, swatchH, swRad, col, scale)

		hexImg := textrender.Render(FormatColor(col, ColorHEX), textrender.Options{
			FontSize:  12.5 * scale,
			Weight:    0.4,
			Monospace: true,
			Color:     color.NRGBA{R: 246, G: 247, B: 252, A: 255},
		})
		twImg := textrender.Render(FormatTailwindLabel(col), textrender.Options{
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

	if len(colors) == 2 {
		divY := pad + swatchH + labelH + int(math.Round(2*scale))
		divCol := color.NRGBA{R: 255, G: 255, B: 255, A: 24}
		for y := divY; y < divY+max(1, int(math.Round(scale))); y++ {
			for x := pad; x < w-pad; x++ {
				blendPixel(out, x, y, divCol, float64(divCol.A)/255.0)
			}
		}
		ratio := ContrastRatio(colors[0], colors[1])
		grade := WCAGGrade(ratio)
		gradeCol := color.NRGBA{R: 248, G: 113, B: 113, A: 255}
		switch grade {
		case "AAA", "AA":
			gradeCol = color.NRGBA{R: 74, G: 222, B: 128, A: 255}
		case "AA Large":
			gradeCol = color.NRGBA{R: 251, G: 191, B: 36, A: 255}
		}
		labelY := divY + int(math.Round(9*scale))
		lblImg := textrender.Render(fmt.Sprintf("WCAG 对比度  %.2f:1", ratio), textrender.Options{
			FontSize:  10.5 * scale,
			Weight:    0.3,
			Monospace: true,
			Color:     color.NRGBA{R: 186, G: 194, B: 210, A: 255},
		})
		gradeImg := textrender.Render(grade, textrender.Options{
			FontSize:  10.5 * scale,
			Weight:    0.5,
			Monospace: true,
			Color:     gradeCol,
		})
		if lblImg != nil {
			stamp(out, lblImg, pad, labelY)
		}
		if gradeImg != nil {
			stamp(out, gradeImg, w-pad-gradeImg.Bounds().Dx(), labelY)
		}
	}

	return out
}
