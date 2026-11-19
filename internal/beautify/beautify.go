// Package beautify 提供截图圆角阴影渐变背景美化卡片，以及剪贴板文字/代码/色号悬浮卡片生成。
package beautify

import (
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
	PresetAuto
)

// CardOptions 控制美化卡片的渐变风格与仿真 macOS 窗口顶栏装饰。
type CardOptions struct {
	Preset       Preset
	WindowChrome bool
}

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
	PresetAuto: {
		c0: color.NRGBA{R: 56, G: 189, B: 248, A: 255},
		c1: color.NRGBA{R: 168, G: 85, B: 247, A: 255},
		c2: color.NRGBA{R: 244, G: 114, B: 182, A: 255},
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
	case PresetAuto:
		return "主色自适应"
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
	return RenderCardWithOptions(src, scale, CardOptions{Preset: preset})
}

// RenderCardWithOptions 支持主色调自适应流体渐变（PresetAuto）与仿真 macOS 红黄绿交通灯顶栏 + 1px 玻璃内描边。
func RenderCardWithOptions(src *image.NRGBA, scale float64, opts CardOptions) *image.NRGBA {
	if src == nil || src.Bounds().Empty() {
		return src
	}
	if scale <= 0 {
		scale = 2.0
	}
	preset := opts.Preset
	if int(preset) < 0 || int(preset) >= len(presets) {
		preset = PresetOcean
	}
	var spec gradientSpec
	if preset == PresetAuto {
		spec = ExtractDominantGradient(src)
	} else {
		spec = presets[preset]
	}

	content := src
	if opts.WindowChrome {
		content = wrapWithMacOSChrome(src, scale)
	}
	sb := content.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	pad := max(32, int(math.Round(44*scale)))
	extraBottom := int(math.Round(8 * scale))
	outW := sw + pad*2
	outH := sh + pad*2 + extraBottom
	radius := min(12.0*scale, float64(min(sw, sh))*0.2)

	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	if preset == PresetAuto {
		fillFluidGradient(out, spec)
	} else {
		fillGradient(out, spec)
	}

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

	// 绘制圆角裁剪后的内容与 1px 玻璃高光内描边。
	rimAlpha := 0.24
	if opts.WindowChrome {
		rimAlpha = 0.34
	}
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
			pxCol := content.NRGBAAt(sb.Min.X+x, sb.Min.Y+y)
			blendPixel(out, dx, dy, pxCol, cov*(float64(pxCol.A)/255.0))
			rimDist := math.Abs(d+0.5*scale) - 0.5*scale
			if rimDist < 0.5 {
				// 顶部边缘反射略强，模拟 macOS 玻璃窗体顶缘受光。
				topBoost := 1.0
				if float64(y) < float64(sh)*0.22 {
					topBoost = 1.35
				}
				rimCov := max(0, min(1, 0.5-rimDist)) * rimAlpha * topBoost
				blendPixel(out, dx, dy, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, min(0.55, rimCov))
			}
		}
	}
	return out
}

// ExtractDominantGradient 从截图中采样高饱和主色与明度，生成和谐且有层次的 3 色流体渐变。
func ExtractDominantGradient(src *image.NRGBA) gradientSpec {
	if src == nil || src.Bounds().Empty() {
		return presets[PresetAuto]
	}
	b := src.Bounds()
	stepX := max(1, b.Dx()/28)
	stepY := max(1, b.Dy()/28)

	// 按 12 个色相桶累计饱和度加权 RGB，优先挑选画面中最具代表性的品牌/重点色。
	type bucket struct {
		weight  float64
		r, g, b float64
	}
	var buckets [12]bucket
	var avgLuma float64
	var totalN float64

	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			c := src.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			r, g, bl := float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0
			mx := max(r, max(g, bl))
			mn := min(r, min(g, bl))
			luma := 0.299*r + 0.587*g + 0.114*bl
			avgLuma += luma
			totalN++

			sat := 0.0
			if mx > 0.001 {
				sat = (mx - mn) / mx
			}
			if sat < 0.14 || mx < 0.12 || mn > 0.92 {
				continue
			}
			hue := rgbHue(r, g, bl, mx, mn)
			bi := int(hue/30.0) % 12
			w := sat * sat * (0.4 + 0.6*(1.0-math.Abs(luma-0.5)*1.4))
			if w > 0 {
				buckets[bi].weight += w
				buckets[bi].r += float64(c.R) * w
				buckets[bi].g += float64(c.G) * w
				buckets[bi].b += float64(c.B) * w
			}
		}
	}
	if totalN > 0 {
		avgLuma /= totalN
	}
	bestIdx := -1
	bestW := 0.0
	for i, bk := range buckets {
		if bk.weight > bestW {
			bestW = bk.weight
			bestIdx = i
		}
	}
	// 无明显彩色像素（纯黑白灰界面）时，根据明暗生成雅致冷调极光渐变。
	if bestIdx < 0 || bestW < 0.5 {
		if avgLuma < 0.45 {
			return gradientSpec{
				c0: color.NRGBA{R: 56, G: 112, B: 235, A: 255},
				c1: color.NRGBA{R: 99, G: 72, B: 224, A: 255},
				c2: color.NRGBA{R: 24, G: 168, B: 204, A: 255},
			}
		}
		return gradientSpec{
			c0: color.NRGBA{R: 120, G: 158, B: 240, A: 255},
			c1: color.NRGBA{R: 158, G: 130, B: 238, A: 255},
			c2: color.NRGBA{R: 96, G: 196, B: 222, A: 255},
		}
	}

	bk := buckets[bestIdx]
	baseR, baseG, baseB := bk.r/bk.weight/255.0, bk.g/bk.weight/255.0, bk.b/bk.weight/255.0
	mx := max(baseR, max(baseG, baseB))
	mn := min(baseR, min(baseG, baseB))
	h0 := rgbHue(baseR, baseG, baseB, mx, mn)

	// 围绕提取主色相做 ±34° 邻近和谐色偏移，并自动调节明度与截图形成通透反差。
	litMain := 0.54
	litDeep := 0.36
	if avgLuma > 0.65 {
		litMain = 0.48
		litDeep = 0.32
	}
	return gradientSpec{
		c0: hslToNRGBA(math.Mod(h0-28+360, 360), 0.72, litMain),
		c1: hslToNRGBA(h0, 0.68, litDeep),
		c2: hslToNRGBA(math.Mod(h0+42, 360), 0.76, litMain+0.06),
	}
}

func wrapWithMacOSChrome(src *image.NRGBA, scale float64) *image.NRGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	barH := max(24, int(math.Round(28*scale)))

	// 采样原图顶部边缘明度，自动决定浅色或深色 macOS 窗口顶栏。
	var sumLuma float64
	var n int
	step := max(1, sw/32)
	for x := sb.Min.X; x < sb.Max.X; x += step {
		c := src.NRGBAAt(x, sb.Min.Y)
		sumLuma += 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
		n++
	}
	isDark := n == 0 || (sumLuma/float64(n)) < 138

	barTop := color.NRGBA{R: 46, G: 48, B: 56, A: 255}
	barBot := color.NRGBA{R: 36, G: 38, B: 46, A: 255}
	divider := color.NRGBA{R: 255, G: 255, B: 255, A: 22}
	if !isDark {
		barTop = color.NRGBA{R: 244, G: 245, B: 248, A: 255}
		barBot = color.NRGBA{R: 232, G: 234, B: 240, A: 255}
		divider = color.NRGBA{R: 0, G: 0, B: 0, A: 24}
	}

	out := image.NewNRGBA(image.Rect(0, 0, sw, sh+barH))
	for y := range barH {
		t := float64(y) / float64(max(1, barH-1))
		rowCol := lerpColor(barTop, barBot, t)
		for x := range sw {
			out.SetNRGBA(x, y, rowCol)
		}
	}
	// 1px 分割细线
	divH := max(1, int(math.Round(scale*0.6)))
	for y := barH - divH; y < barH; y++ {
		for x := range sw {
			blendPixel(out, x, y, divider, float64(divider.A)/255.0)
		}
	}
	// 绘制抗锯齿红/黄/绿交通灯圆点
	lights := [3]struct {
		fill   color.NRGBA
		stroke color.NRGBA
	}{
		{color.NRGBA{R: 255, G: 95, B: 86, A: 255}, color.NRGBA{R: 224, G: 68, B: 62, A: 255}},
		{color.NRGBA{R: 255, G: 189, B: 46, A: 255}, color.NRGBA{R: 222, G: 161, B: 35, A: 255}},
		{color.NRGBA{R: 39, G: 201, B: 63, A: 255}, color.NRGBA{R: 26, G: 171, B: 41, A: 255}},
	}
	dotR := 5.6 * scale
	dotY := float64(barH) * 0.5
	startX := 16.0 * scale
	spacing := 19.0 * scale
	for i, lt := range lights {
		cx := startX + float64(i)*spacing
		minX := max(0, int(math.Floor(cx-dotR-2)))
		maxX := min(sw, int(math.Ceil(cx+dotR+2)))
		minY := max(0, int(math.Floor(dotY-dotR-2)))
		maxY := min(barH, int(math.Ceil(dotY+dotR+2)))
		for y := minY; y < maxY; y++ {
			for x := minX; x < maxX; x++ {
				d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-dotY) - dotR
				if d < 0.5 {
					cov := min(1, 0.5-d)
					blendPixel(out, x, y, lt.fill, cov)
					if d > -1.0*scale {
						blendPixel(out, x, y, lt.stroke, cov*0.45)
					}
				}
			}
		}
	}
	// 原图拷贝到顶栏下方
	for y := range sh {
		sOff := src.PixOffset(sb.Min.X, sb.Min.Y+y)
		dOff := out.PixOffset(0, barH+y)
		copy(out.Pix[dOff:dOff+sw*4], src.Pix[sOff:sOff+sw*4])
	}
	return out
}

func fillFluidGradient(dst *image.NRGBA, g gradientSpec) {
	b := dst.Bounds()
	w := float64(max(1, b.Dx()-1))
	h := float64(max(1, b.Dy()-1))
	for y := 0; y < b.Dy(); y++ {
		fy := float64(y) / h
		for x := 0; x < b.Dx(); x++ {
			fx := float64(x) / w
			// 三个柔和径向光斑混合模拟 Apple 流体渐变背景。
			d0 := math.Hypot(fx-0.18, fy-0.22)
			d1 := math.Hypot(fx-0.84, fy-0.28)
			d2 := math.Hypot(fx-0.52, fy-0.88)
			w0 := math.Exp(-d0 * d0 * 2.1)
			w1 := math.Exp(-d1 * d1 * 2.1)
			w2 := math.Exp(-d2 * d2 * 1.9)
			ws := w0 + w1 + w2
			r := (float64(g.c0.R)*w0 + float64(g.c1.R)*w1 + float64(g.c2.R)*w2) / ws
			gr := (float64(g.c0.G)*w0 + float64(g.c1.G)*w1 + float64(g.c2.G)*w2) / ws
			bl := (float64(g.c0.B)*w0 + float64(g.c1.B)*w1 + float64(g.c2.B)*w2) / ws
			dst.SetNRGBA(b.Min.X+x, b.Min.Y+y, color.NRGBA{
				R: uint8(max(0, min(255, math.Round(r)))),
				G: uint8(max(0, min(255, math.Round(gr)))),
				B: uint8(max(0, min(255, math.Round(bl)))),
				A: 255,
			})
		}
	}
}

func rgbHue(r, g, b, mx, mn float64) float64 {
	d := mx - mn
	if d < 1e-6 {
		return 0
	}
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

func hslToNRGBA(h, s, l float64) color.NRGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60.0, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.NRGBA{
		R: uint8(max(0, min(255, math.Round((r+m)*255)))),
		G: uint8(max(0, min(255, math.Round((g+m)*255)))),
		B: uint8(max(0, min(255, math.Round((b+m)*255)))),
		A: 255,
	}
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
	w := int(math.Round(308 * scale))
	h := int(math.Round(114 * scale))
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	bg := color.NRGBA{R: 26, G: 27, B: 32, A: 255}
	fillSolid(out, bg)

	swSize := int(math.Round(82 * scale))
	pad := int(math.Round(16 * scale))
	cx := float64(pad) + float64(swSize)*0.5
	cy := float64(pad) + float64(swSize)*0.5
	half := float64(swSize) * 0.5
	swRad := 10.0 * scale
	for y := pad; y < pad+swSize; y++ {
		for x := pad; x < pad+swSize; x++ {
			d := roundedRectSDF(float64(x)+0.5-cx, float64(y)+0.5-cy, half, half, swRad)
			if d < 0.5 {
				blendPixel(out, x, y, col, min(1, 0.5-d))
			}
		}
	}

	hexStr := FormatColor(col, ColorHEX)
	rgbStr := FormatColor(col, ColorRGB)
	oklchStr := FormatColor(col, ColorOKLCH)
	twStr := "Tailwind: " + NearestTailwind(col)
	title := textrender.Render(hexStr, textrender.Options{
		FontSize:  17.5 * scale,
		Weight:    0.4,
		Monospace: true,
		Color:     color.NRGBA{R: 250, G: 250, B: 252, A: 255},
	})
	sub1 := textrender.Render(rgbStr, textrender.Options{
		FontSize:  11.5 * scale,
		Weight:    0.2,
		Monospace: true,
		Color:     color.NRGBA{R: 184, G: 190, B: 204, A: 255},
	})
	sub2 := textrender.Render(oklchStr, textrender.Options{
		FontSize:  11 * scale,
		Weight:    0.2,
		Monospace: true,
		Color:     color.NRGBA{R: 148, G: 162, B: 186, A: 255},
	})
	sub3 := textrender.Render(twStr, textrender.Options{
		FontSize:  11 * scale,
		Weight:    0.3,
		Monospace: true,
		Color:     color.NRGBA{R: 112, G: 206, B: 255, A: 255},
	})
	tx := pad*2 + swSize
	if title != nil {
		stamp(out, title, tx, int(math.Round(15*scale)))
	}
	if sub1 != nil {
		stamp(out, sub1, tx, int(math.Round(42*scale)))
	}
	if sub2 != nil {
		stamp(out, sub2, tx, int(math.Round(62*scale)))
	}
	if sub3 != nil {
		stamp(out, sub3, tx, int(math.Round(82*scale)))
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
