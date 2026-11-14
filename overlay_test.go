package main

import (
	"image"
	"testing"
)

func TestDimBandsCoverWindowOnce(t *testing.T) {
	win := image.Rect(0, 0, 100, 80)
	// 选区从这块屏左边 20 DIP 处进来，本地 x 从 -20 到 30。
	hole := image.Rect(-20, 10, 30, 40)
	bands, border := dimBands(win, hole)
	if border != image.Rect(0, 10, 30, 40) {
		t.Fatalf("border %v", border)
	}
	seen := map[image.Point]int{}
	mark := func(r image.Rectangle) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				seen[image.Pt(x, y)]++
			}
		}
	}
	for _, band := range bands {
		mark(band)
	}
	mark(border)
	if len(seen) != win.Dx()*win.Dy() {
		t.Fatalf("covered %d, want %d", len(seen), win.Dx()*win.Dy())
	}
	for p, n := range seen {
		if n != 1 {
			t.Fatalf("pixel %v covered %d times", p, n)
		}
	}
}

func TestDimBandsMissCoversAll(t *testing.T) {
	win := image.Rect(0, 0, 40, 20)
	bands, border := dimBands(win, image.Rect(100, 100, 110, 110))
	if len(bands) != 1 || bands[0] != win || !border.Empty() {
		t.Fatalf("bands %v border %v", bands, border)
	}
}

func TestLocalSelectionUsesDisplayOrigin(t *testing.T) {
	bounds := image.Rect(-200, 0, 0, 100)
	sel := image.Rect(-50, 10, -30, 30)
	got := localSelection(bounds, sel)
	if got != image.Rect(150, 10, 170, 30) {
		t.Fatalf("local %v", got)
	}
}
