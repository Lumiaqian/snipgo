package crop

import (
	"image"
	"image/color"
	"testing"
)

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestDIPToPixelHalfAwayFromZero(t *testing.T) {
	if got := DIPToPixel(1, 0, 2.5); got != 3 {
		t.Fatalf("DIPToPixel(1, 0, 2.5) = %d, want 3", got)
	}
	if got := DIPToPixel(2, 0, 2.5); got != 5 {
		t.Fatalf("DIPToPixel(2, 0, 2.5) = %d, want 5", got)
	}
}

func TestCropRetina(t *testing.T) {
	red := color.NRGBA{R: 200, A: 255}
	blue := color.NRGBA{B: 180, A: 255}
	img := solid(20, 20, red)
	img.SetNRGBA(4, 6, blue) // DIP (2,3) at scale 2
	screens := []Screen{{
		Bounds: image.Rect(0, 0, 10, 10),
		Scale:  2,
		Pixels: img,
	}}
	got, err := Crop(screens, image.Rect(2, 3, 6, 8))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 8 || got.Bounds().Dy() != 10 {
		t.Fatalf("size %v, want 8x10", got.Bounds().Size())
	}
	if c := got.NRGBAAt(0, 0); c != blue {
		t.Fatalf("origin pixel %v, want the marked blue pixel", c)
	}
	if c := got.NRGBAAt(1, 0); c != red {
		t.Fatalf("neighbor %v, want red", c)
	}
}

func TestCropNegativeOrigin(t *testing.T) {
	mark := color.NRGBA{G: 90, A: 255}
	img := solid(20, 10, color.NRGBA{R: 1, A: 255})
	// DIP x=-8 is pixel round((-8-(-10))*2) = 4
	img.SetNRGBA(4, 2, mark)
	screens := []Screen{{
		Bounds: image.Rect(-10, 0, 0, 5),
		Scale:  2,
		Pixels: img,
	}}
	got, err := Crop(screens, image.Rect(-8, 1, -6, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 4 {
		t.Fatalf("size %v", got.Bounds().Size())
	}
	if c := got.NRGBAAt(0, 0); c != mark {
		t.Fatalf("pixel %v, want mark", c)
	}
}

func TestCropReversedDrag(t *testing.T) {
	img := solid(10, 10, color.NRGBA{R: 7, A: 255})
	got, err := Crop([]Screen{{
		Bounds: image.Rect(0, 0, 10, 10),
		Scale:  1,
		Pixels: img,
	}}, image.Rect(6, 5, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 4 {
		t.Fatalf("size %v, want 4x4", got.Bounds().Size())
	}
}

func TestCropAcrossScales(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	// 100x100 DIP at 2x, and the screen to its right 100x100 DIP at 1x.
	screens := []Screen{
		{Bounds: image.Rect(0, 0, 100, 100), Scale: 2, Pixels: solid(200, 200, red)},
		{Bounds: image.Rect(100, 0, 200, 100), Scale: 1, Pixels: solid(100, 100, blue)},
	}
	sel := image.Rect(90, 10, 110, 20)
	got, err := Crop(screens, sel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 40 || got.Bounds().Dy() != 20 {
		t.Fatalf("size %v, want 40x20 at the larger scale", got.Bounds().Size())
	}
	if c := got.NRGBAAt(0, 0); c != red {
		t.Fatalf("left %v, want red", c)
	}
	if c := got.NRGBAAt(19, 19); c != red {
		t.Fatalf("end of retina half %v, want red", c)
	}
	if c := got.NRGBAAt(20, 0); c != blue {
		t.Fatalf("right %v, want blue doubled from the 1x screen", c)
	}
	if c := got.NRGBAAt(39, 19); c != blue {
		t.Fatalf("bottom-right %v, want blue", c)
	}
}

func TestCropRejectsEmptyAndBadScreen(t *testing.T) {
	img := solid(10, 10, color.NRGBA{A: 255})
	ok := Screen{Bounds: image.Rect(0, 0, 10, 10), Scale: 1, Pixels: img}
	if _, err := Crop([]Screen{ok}, image.Rectangle{}); err == nil {
		t.Fatal("empty selection")
	}
	if _, err := Crop([]Screen{ok}, image.Rect(20, 20, 30, 30)); err == nil {
		t.Fatal("selection outside the screen")
	}
	bad := ok
	bad.Pixels = nil
	if _, err := Crop([]Screen{bad}, image.Rect(0, 0, 2, 2)); err == nil {
		t.Fatal("nil pixels")
	}
	bad = ok
	bad.Scale = 0
	if _, err := Crop([]Screen{bad}, image.Rect(0, 0, 2, 2)); err == nil {
		t.Fatal("zero scale")
	}
	bad = ok
	bad.Pixels = solid(9, 10, color.NRGBA{A: 255})
	if _, err := Crop([]Screen{bad}, image.Rect(0, 0, 2, 2)); err == nil {
		t.Fatal("pixel size does not match the scale")
	}
}
