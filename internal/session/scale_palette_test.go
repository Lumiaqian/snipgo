package session

import (
	"image"
	"image/color"
	"testing"

	"snipgo/internal/crop"
)

func TestScaleForRectUsesMaxScaleOfIntersectingFrames(t *testing.T) {
	s := &Session{frames: []crop.Screen{
		{Bounds: image.Rect(0, 0, 100, 100), Scale: 1},
		{Bounds: image.Rect(100, 0, 200, 100), Scale: 2},
	}}
	if got := s.scaleForRect(image.Rect(50, 10, 150, 50)); got != 2 {
		t.Fatalf("跨 1x/2x 显示器应取最大缩放 2, got %v", got)
	}
	if got := s.scaleForRect(image.Rect(10, 10, 50, 50)); got != 1 {
		t.Fatalf("仅 1x 显示器应为 1, got %v", got)
	}
	if got := s.scaleForRect(image.Rect(500, 500, 510, 510)); got != 2 {
		t.Fatalf("无相交显示器默认 2, got %v", got)
	}
}

func TestPalettePinStaysOnNegativeOriginDisplay(t *testing.T) {
	ui := &fakeUI{}
	s := &Session{
		frames: []crop.Screen{
			{Bounds: image.Rect(-1920, 0, 0, 1080), Scale: 1},
			{Bounds: image.Rect(0, 0, 1440, 900), Scale: 2},
		},
		pickedColors: []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}},
		hasCursor:    true,
		cursorX:      -1000,
		cursorY:      500,
		UI:           ui,
	}
	if !s.PinPickedPalette() {
		t.Fatal("PinPickedPalette 失败")
	}
	b := image.Rect(-1920, 0, 0, 1080)
	if !ui.at.In(b) || !image.Rect(ui.at.X, ui.at.Y, ui.at.X+ui.dip.X, ui.at.Y+ui.dip.Y).In(b) {
		t.Fatalf("配色卡 at=%v dip=%v 应整体位于光标所在显示器 %v", ui.at, ui.dip, b)
	}
	// 光标贴近左边缘时仍不能越出该显示器。
	s.pickedColors = []color.NRGBA{{R: 255, A: 255}}
	s.frames = []crop.Screen{{Bounds: b, Scale: 1}, {Bounds: image.Rect(0, 0, 1440, 900), Scale: 2}}
	s.hasCursor, s.cursorX, s.cursorY = true, -1915, 5
	s.PinPickedPalette()
	if !ui.at.In(b) {
		t.Fatalf("边缘光标下 at=%v 越出显示器 %v", ui.at, b)
	}
}

// applyOutput 回调 OnOutput 时必须带上选区所在显示器的缩放，历史贴图才能还原真实 DIP 尺寸。
func TestApplyOutputReportsDisplayScale(t *testing.T) {
	s := &Session{
		UI: &fakeUI{},
		frames: []crop.Screen{
			{Bounds: image.Rect(0, 0, 100, 100), Scale: 1},
			{Bounds: image.Rect(100, 0, 200, 100), Scale: 3},
		},
	}
	var got float64
	s.OnOutput = func(_ []byte, _, _ int, scale float64) { got = scale }
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	s.applyOutput(ActionSave, image.Rect(10, 10, 20, 20), img, []byte("p"))
	if got != 1 {
		t.Fatalf("1x 显示器 scale = %v, want 1", got)
	}
	s.applyOutput(ActionSave, image.Rect(110, 10, 120, 20), img, []byte("p"))
	if got != 3 {
		t.Fatalf("3x 显示器 scale = %v, want 3", got)
	}
}
