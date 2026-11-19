package beautify

import (
	"image"
	"image/color"
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
	if _, bad := ParseColor("hello"); bad {
		t.Fatal("ordinary word should not parse as color")
	}

	swatch := RenderTextPin("#00A8FF", 2.0)
	if swatch == nil || swatch.Bounds().Empty() {
		t.Fatal("expected swatch card image")
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
	if got := FormatColor(sky, ColorRGB); got != "rgb(14, 165, 233)" {
		t.Fatalf("RGB = %q", got)
	}
	oklch := FormatColor(sky, ColorOKLCH)
	if len(oklch) < 10 || oklch[:6] != "oklch(" {
		t.Fatalf("unexpected OKLCH: %q", oklch)
	}
	if tw := NearestTailwind(sky); tw != "sky-500" {
		t.Fatalf("NearestTailwind = %q, want sky-500", tw)
	}
	pal := RenderPaletteCard([]color.NRGBA{
		{R: 14, G: 165, B: 233, A: 255},
		{R: 239, G: 68, B: 68, A: 255},
		{R: 16, G: 185, B: 129, A: 255},
	}, 2.0)
	if pal == nil || pal.Bounds().Dx() < 600 || pal.Bounds().Dy() < 200 {
		t.Fatalf("unexpected palette card size: %v", pal.Bounds())
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
