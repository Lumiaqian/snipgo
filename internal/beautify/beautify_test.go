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
