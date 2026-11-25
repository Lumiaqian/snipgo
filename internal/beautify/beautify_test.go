package beautify

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

func TestRenderCardAddsGradientPaddingAndKeepsCenter(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	white := color.NRGBA{R: 250, G: 250, B: 250, A: 255}
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			src.SetNRGBA(x, y, white)
		}
	}
	card := RenderCard(src, 1.0, PresetOcean)
	if card == nil || card.Bounds().Dx() <= 120 || card.Bounds().Dy() <= 80 {
		t.Fatalf("unexpected card bounds: %v", card.Bounds())
	}
	// 中心像素仍保持原图亮白，四角为海湾蓝背景。
	mid := card.NRGBAAt(card.Bounds().Dx()/2, card.Bounds().Dy()/2)
	if mid.R < 245 || mid.G < 245 || mid.B < 245 {
		t.Fatalf("center pixel altered: %+v", mid)
	}
	corner := card.NRGBAAt(4, 4)
	if corner.B < 180 || corner.R > 80 {
		t.Fatalf("corner pixel should be ocean gradient, got %+v", corner)
	}
	// 验证底边正中下方阴影区比顶边正中上方更暗（确认圆角投影正确以卡片中心 cx 对齐而非偏移到左缘）。
	topMid := card.NRGBAAt(card.Bounds().Dx()/2, 16)
	botShadow := card.NRGBAAt(card.Bounds().Dx()/2, card.Bounds().Dy()-36)
	if int(botShadow.R)+int(botShadow.G)+int(botShadow.B) >= int(topMid.R)+int(topMid.G)+int(topMid.B)-20 {
		t.Fatalf("expected centered drop shadow under card bottom-middle: bot=%+v vs top=%+v", botShadow, topMid)
	}
}

func TestParseColorAndRenderTextPin(t *testing.T) {
	c, ok := ParseColor("#00A8FF")
	if !ok || c != (color.NRGBA{R: 0, G: 168, B: 255, A: 255}) {
		t.Fatalf("ParseColor #00A8FF = %+v, %v", c, ok)
	}
	c2, ok2 := ParseColor("rgb(255, 59, 48)")
	if !ok2 || c2 != (color.NRGBA{R: 255, G: 59, B: 48, A: 255}) {
		t.Fatalf("ParseColor rgb = %+v, %v", c2, ok2)
	}
	sky := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	skyOKLCH := FormatColor(sky, ColorOKLCH)
	c3, ok3 := ParseColor(skyOKLCH)
	if !ok3 {
		t.Fatalf("ParseColor(%q) failed", skyOKLCH)
	}
	if math.Abs(float64(c3.R)-float64(sky.R)) > 2 ||
		math.Abs(float64(c3.G)-float64(sky.G)) > 2 ||
		math.Abs(float64(c3.B)-float64(sky.B)) > 2 {
		t.Fatalf("ParseColor(%q) roundtrip = %+v, want near %+v", skyOKLCH, c3, sky)
	}
	if c3a, ok3a := ParseColor("oklch(68.5% 0.169 237.3deg / 50%)"); !ok3a || c3a.A != 128 {
		t.Fatalf("ParseColor oklch with units/alpha = %+v, %v", c3a, ok3a)
	}
	c4, ok4 := ParseColor("sky-500")
	if !ok4 || c4 != sky {
		t.Fatalf("ParseColor sky-500 = %+v, %v", c4, ok4)
	}
	if _, ok5 := ParseColor("neutral-500"); !ok5 {
		t.Fatal("expected full Tailwind neutral-500 token to parse")
	}
	if _, ok6 := ParseColor("stone-900"); !ok6 {
		t.Fatal("expected full Tailwind stone-900 token to parse")
	}
	for _, reject := range []string{"hello", "white", "black"} {
		if _, bad := ParseColor(reject); bad {
			t.Fatalf("ordinary word %q should not parse as color", reject)
		}
	}

	swatch := RenderTextPin("#00A8FF", 2.0)
	if swatch == nil || swatch.Bounds().Empty() {
		t.Fatal("expected swatch card image")
	}
	dw, dh, rowRects := SwatchCardRowRects()
	if dw <= 0 || dh <= 0 {
		t.Fatalf("unexpected swatch card layout: %dx%d", dw, dh)
	}
	for i := range rowRects {
		if rowRects[i].Empty() {
			t.Fatalf("rowRects[%d] is empty", i)
		}
		if i > 0 && rowRects[i].Min.Y <= rowRects[i-1].Min.Y {
			t.Fatalf("rowRects not strictly descending: %v", rowRects)
		}
	}

	note := RenderTextPin("func main() {\n\tprintln(\"SnipGo\")\n}", 2.0)
	if note == nil || note.Bounds().Empty() {
		t.Fatal("expected code note card image")
	}
}

func TestDeveloperColorFormatsAndPaletteCard(t *testing.T) {
	sky := color.NRGBA{R: 14, G: 165, B: 233, A: 255}
	if got := FormatColor(sky, ColorHEX); got != "#0EA5E9" {
		t.Fatalf("HEX = %q, want #0EA5E9", got)
	}
	if got := FormatColorRaw(sky, ColorHEX); got != "0EA5E9" {
		t.Fatalf("Raw HEX = %q, want 0EA5E9", got)
	}
	if got := FormatColor(sky, ColorRGB); got != "rgb(14, 165, 233)" {
		t.Fatalf("RGB = %q", got)
	}
	if got := FormatColorRaw(sky, ColorRGB); got != "14, 165, 233" {
		t.Fatalf("Raw RGB = %q", got)
	}
	oklch := FormatColor(sky, ColorOKLCH)
	if len(oklch) < 10 || oklch[:6] != "oklch(" {
		t.Fatalf("unexpected OKLCH: %q", oklch)
	}
	rawOKLCH := FormatColorRaw(sky, ColorOKLCH)
	if strings.HasPrefix(rawOKLCH, "oklch(") || strings.HasSuffix(rawOKLCH, ")") || len(rawOKLCH) < 5 {
		t.Fatalf("unexpected Raw OKLCH: %q", rawOKLCH)
	}
	if got := FormatColorRaw(sky, ColorTailwind); got != FormatColor(sky, ColorTailwind) {
		t.Fatalf("Raw Tailwind = %q, formatted = %q", got, FormatColor(sky, ColorTailwind))
	}

	for _, fmtVal := range []ColorFormat{ColorHEX, ColorRGB, ColorOKLCH, ColorTailwind} {
		if got := ParseColorFormat(ColorFormatKey(fmtVal)); got != fmtVal {
			t.Fatalf("ColorFormat roundtrip %v -> %q -> %v", fmtVal, ColorFormatKey(fmtVal), got)
		}
	}

	name, exact, _ := MatchTailwind(sky)
	if name != "sky-500" || !exact {
		t.Fatalf("MatchTailwind(sky) = (%q, %v), want (sky-500, true)", name, exact)
	}
	if lbl := FormatTailwindLabel(sky); lbl != "sky-500" {
		t.Fatalf("FormatTailwindLabel(sky) = %q, want sky-500", lbl)
	}
	approxCol := color.NRGBA{R: 15, G: 166, B: 232, A: 255}
	if _, exactApprox, _ := MatchTailwind(approxCol); exactApprox {
		t.Fatal("expected non-exact Tailwind match for shifted color")
	}
	if lbl := FormatTailwindLabel(approxCol); !strings.HasPrefix(lbl, "≈ ") {
		t.Fatalf("expected ≈ prefix for approximate match, got %q", lbl)
	}

	ratio := ContrastRatio(color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	if math.Abs(ratio-21.0) > 0.05 {
		t.Fatalf("white/black contrast = %.2f, want ~21.0", ratio)
	}
	if WCAGGrade(21.0) != "AAA" || WCAGGrade(4.5) != "AA" || WCAGGrade(3.0) != "AA Large" || WCAGGrade(2.0) != "Fail" {
		t.Fatalf("unexpected WCAGGrade outputs")
	}
	if summary := FormatContrastSummary(color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{R: 0, G: 0, B: 0, A: 255}); !strings.Contains(summary, "21.00:1") || !strings.Contains(summary, "AAA") {
		t.Fatalf("unexpected contrast summary: %q", summary)
	}

	cssHex := FormatPaletteCSS([]color.NRGBA{sky, sky}, ColorHEX)
	if !strings.Contains(cssHex, ":root {") || !strings.Contains(cssHex, "--color-1: #0EA5E9;") || !strings.Contains(cssHex, "--color-2: #0EA5E9;") {
		t.Fatalf("unexpected CSS hex export:\n%s", cssHex)
	}
	cssTW := FormatPaletteCSS([]color.NRGBA{sky, sky}, ColorTailwind)
	if !strings.Contains(cssTW, "--sky-500: #0EA5E9;") || !strings.Contains(cssTW, "--sky-500-2: #0EA5E9;") {
		t.Fatalf("unexpected CSS tailwind export:\n%s", cssTW)
	}

	pal2Colors := []color.NRGBA{
		{R: 24, G: 24, B: 27, A: 255}, // #18181B dark swatch near card bg
		{R: 255, G: 255, B: 255, A: 255},
	}
	dipW, dipH, swatchRects := PaletteCardLayout(len(pal2Colors))
	if len(swatchRects) != 2 || dipW <= 0 || dipH <= 0 {
		t.Fatalf("unexpected PaletteCardLayout(2): %dx%d rects=%d", dipW, dipH, len(swatchRects))
	}
	pal2 := RenderPaletteCard(pal2Colors, 2.0)
	if pal2 == nil || pal2.Bounds().Dx() != int(math.Round(float64(dipW)*2.0)) || pal2.Bounds().Dy() != int(math.Round(float64(dipH)*2.0)) {
		t.Fatalf("pal2 size = %v, want %dx%d", pal2.Bounds(), int(math.Round(float64(dipW)*2)), int(math.Round(float64(dipH)*2)))
	}
	// Verify dark swatch #18181B has a visible inner border above/along its top edge.
	cx := (swatchRects[0].Min.X + swatchRects[0].Dx()/2) * 2
	topY := swatchRects[0].Min.Y * 2
	hasBorderHighlight := false
	for y := topY; y <= topY+4; y++ {
		px := color.NRGBAModel.Convert(pal2.At(cx, y)).(color.NRGBA)
		if px.R > 35 {
			hasBorderHighlight = true
			break
		}
	}
	if !hasBorderHighlight {
		t.Fatalf("expected adaptive inner border highlight on dark swatch at x=%d y=%d..%d", cx, topY, topY+4)
	}

	pal3 := RenderPaletteCard([]color.NRGBA{
		sky,
		{R: 239, G: 68, B: 68, A: 255},
		{R: 16, G: 185, B: 129, A: 255},
	}, 2.0)
	if pal3 == nil || pal3.Bounds().Dx() < 600 || pal3.Bounds().Dy() < 200 {
		t.Fatalf("unexpected palette card size: %v", pal3.Bounds())
	}
}

func TestAutoDominantGradientAndMacOSChrome(t *testing.T) {
	// 构造以深青绿色为主的界面截图
	src := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	for y := range 120 {
		for x := range 200 {
			c := color.NRGBA{R: 24, G: 30, B: 38, A: 255}
			if x > 30 && x < 170 && y > 30 && y < 90 {
				c = color.NRGBA{R: 16, G: 185, B: 129, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	card := RenderCardWithOptions(src, 1.0, CardOptions{
		Preset:       PresetAuto,
		WindowChrome: true,
	})
	if card == nil || card.Bounds().Dy() <= 120+28+64 {
		t.Fatalf("expected taller card with macOS window chrome, got %v", card.Bounds())
	}
	// 验证红灯交通灯中心 (pad+16, pad+14) 确实呈红色
	pad := 44
	redDot := card.NRGBAAt(pad+16, pad+14)
	if redDot.R < 220 || redDot.G > 120 {
		t.Fatalf("expected macOS red traffic light at (%d,%d), got %+v", pad+16, pad+14, redDot)
	}
}
