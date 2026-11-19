package textrender

import (
	"image/color"
	"testing"
)

func TestRenderChineseAndASCII(t *testing.T) {
	img := Render("你好 SnipGo 123\n第二行测试", Options{
		FontSize: 28,
		Weight:   0.3,
		Color:    color.NRGBA{R: 255, G: 60, B: 48, A: 255},
	})
	if img == nil || img.Bounds().Empty() {
		t.Fatal("expected non-empty rendered image")
	}
	nonZero := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A > 0 {
				nonZero++
			}
		}
	}
	if nonZero < 50 {
		t.Fatalf("too few rendered pixels: %d in %v", nonZero, b)
	}
	// 验证顶部与底部都有墨迹（两行方向正确且未被截断）。
	topInk, botInk := false, false
	midY := b.Dy() / 2
	for y := 0; y < midY; y++ {
		for x := 0; x < b.Dx(); x++ {
			if img.NRGBAAt(x, y).A > 32 {
				topInk = true
			}
		}
	}
	for y := midY; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if img.NRGBAAt(x, y).A > 32 {
				botInk = true
			}
		}
	}
	if !topInk || !botInk {
		t.Fatalf("expected ink in both top and bottom halves: top=%v bot=%v size=%v", topInk, botInk, b)
	}

	// 验证单侧非对称文本没有上下颠倒：首行有字、末三行空行时，墨迹必须在上半部而非下半部。
	asym := Render("顶部标题测试\n \n \n ", Options{
		FontSize: 24,
		Weight:   0.3,
		Color:    color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	})
	ab := asym.Bounds()
	topCount, botCount := 0, 0
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			if asym.NRGBAAt(x, y).A > 32 {
				if y < ab.Dy()/2 {
					topCount++
				} else {
					botCount++
				}
			}
		}
	}
	if topCount == 0 || botCount > topCount/4 {
		t.Fatalf("text rendered upside-down! topCount=%d botCount=%d size=%v", topCount, botCount, ab)
	}
}
