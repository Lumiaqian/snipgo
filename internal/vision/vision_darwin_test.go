//go:build darwin

package vision

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"snipgo/internal/textrender"
)

func TestRecognizeChineseAndEnglishOffline(t *testing.T) {
	txtImg := textrender.Render("SnipGo 极速识字 2026\nHello Vision OCR", textrender.Options{
		FontSize: 36,
		Weight:   0.3,
		Color:    color.NRGBA{R: 16, G: 16, B: 20, A: 255},
	})
	if txtImg == nil {
		t.Fatal("textrender failed")
	}
	tb := txtImg.Bounds()
	canvas := image.NewNRGBA(image.Rect(0, 0, tb.Dx()+80, tb.Dy()+80))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, tb.Add(image.Pt(40, 40)), txtImg, tb.Min, draw.Over)

	res, err := Recognize(canvas)
	if err != nil {
		t.Fatalf("Recognize failed: %v", err)
	}
	if !strings.Contains(res.Text, "SnipGo") || !strings.Contains(res.Text, "2026") || !strings.Contains(res.Text, "Vision") {
		t.Fatalf("OCR output missing expected words: %q", res.Text)
	}
	if !strings.Contains(res.Text, "极速") {
		t.Fatalf("Chinese OCR missing expected characters: %q", res.Text)
	}
}

func TestRecognizeSmallUITextInFullWindow(t *testing.T) {
	// 模拟在 2400x1600 全屏窗口中识别 22px (11pt @2x) 深色模式小字。
	txtImg := textrender.Render("设置里的屏幕录制权限已开启\nSmall UI caption 12pt", textrender.Options{
		FontSize: 22,
		Color:    color.NRGBA{R: 230, G: 232, B: 238, A: 255},
	})
	if txtImg == nil {
		t.Fatal("textrender failed")
	}
	tb := txtImg.Bounds()
	canvas := image.NewNRGBA(image.Rect(0, 0, 2400, 1600))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 28, G: 28, B: 32, A: 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, tb.Add(image.Pt(200, 300)), txtImg, tb.Min, draw.Over)

	res, err := Recognize(canvas)
	if err != nil {
		t.Fatalf("Recognize failed: %v", err)
	}
	t.Logf("Recognized full-window small text:\n%s", res.Text)
	if !strings.Contains(res.Text, "屏幕录制") || !strings.Contains(res.Text, "Small") {
		t.Fatalf("failed on small dark-mode text in large window: %q", res.Text)
	}
}

func TestRecognizeTightCroppedTinyTextAndExtractLinks(t *testing.T) {
	// 模拟用户紧贴文字边缘裁剪极小 1x 验证码/链接（无留白、高度 < 36px）。
	txtImg := textrender.Render("验证码 849201 https://snipgo.dev/docs", textrender.Options{
		FontSize: 15,
		Color:    color.NRGBA{R: 20, G: 22, B: 26, A: 255},
	})
	if txtImg == nil {
		t.Fatal("textrender failed")
	}
	tb := txtImg.Bounds()
	tight := image.NewNRGBA(image.Rect(0, 0, tb.Dx(), tb.Dy()))
	draw.Draw(tight, tight.Bounds(), image.NewUniform(color.NRGBA{R: 250, G: 250, B: 252, A: 255}), image.Point{}, draw.Src)
	draw.Draw(tight, tight.Bounds(), txtImg, tb.Min, draw.Over)

	res, err := Recognize(tight)
	if err != nil {
		t.Fatalf("Recognize tight crop failed: %v", err)
	}
	if !strings.Contains(res.Text, "849201") {
		t.Fatalf("tight crop missed digits: %q", res.Text)
	}
	links := ExtractLinks(res)
	if len(links) == 0 || !strings.Contains(links[0], "snipgo.dev") {
		t.Fatalf("expected extracted link, got links=%v text=%q", links, res.Text)
	}
}

func TestSensitiveBoxesLocatesSecretsAndPII(t *testing.T) {
	txtImg := textrender.Render("Key: sk-proj-AbCdEf1234567890\nMail: dev@snipgo.app  Tel: 13812345678", textrender.Options{
		FontSize: 28,
		Color:    color.NRGBA{R: 20, G: 22, B: 26, A: 255},
	})
	tb := txtImg.Bounds()
	canvas := image.NewNRGBA(image.Rect(0, 0, tb.Dx()+80, tb.Dy()+80))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, tb.Add(image.Pt(40, 40)), txtImg, tb.Min, draw.Over)

	res, err := Recognize(canvas)
	if err != nil {
		t.Fatalf("Recognize failed: %v", err)
	}
	boxes := SensitiveBoxes(res)
	if len(boxes) < 2 {
		t.Fatalf("expected >=2 sensitive spans (API key + email/phone), got %d (%+v), text=%q", len(boxes), boxes, res.Text)
	}
}
