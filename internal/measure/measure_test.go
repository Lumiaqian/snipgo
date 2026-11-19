package measure

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestCastSnapsToInnerBoxEdges(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 30, B: 36, A: 255}), image.Point{}, draw.Src)
	// 在 (40, 25)-(140, 85) 画一块 100x60 的白色按钮。
	btn := image.Rect(40, 25, 140, 85)
	draw.Draw(img, btn, image.NewUniform(color.NRGBA{R: 245, G: 245, B: 250, A: 255}), image.Point{}, draw.Src)

	sp, ok := Cast(img, 80, 50, DefaultTolerance)
	if !ok {
		t.Fatal("Cast failed")
	}
	if !sp.HitLeft || !sp.HitRight || !sp.HitTop || !sp.HitBottom {
		t.Fatalf("expected all 4 edges hit: %+v", sp)
	}
	if sp.WidthPx() != 100 || sp.HeightPx() != 60 {
		t.Fatalf("measured %dx%d px, want 100x60 px (span=%+v)", sp.WidthPx(), sp.HeightPx(), sp)
	}
}
