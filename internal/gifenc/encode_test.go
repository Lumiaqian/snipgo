package gifenc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"testing"
)

func demo(yield func(*image.RGBA) error) error {
	f := image.NewRGBA(image.Rect(0, 0, 960, 64))
	for y := range 64 {
		for x := range 960 {
			v := uint8(x * 255 / 959)
			f.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	for i := range 150 {
		f.SetRGBA(i, 0, color.RGBA{255, 0, 0, 255})
		if err := yield(f); err != nil {
			return err
		}
	}
	return nil
}
func TestDemo(t *testing.T) {
	var b bytes.Buffer
	if err := Encode(t.Context(), &b, 15, demo); err != nil {
		t.Fatal(err)
	}
	size := b.Len()
	g, err := gif.DecodeAll(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 150 || size > 100000 || g.LoopCount != 0 {
		t.Fatalf("frames=%d bytes=%d loops=%d", len(g.Image), size, g.LoopCount)
	}
	total := 0
	for i, d := range g.Delay {
		total += d
		if i > 0 && g.Image[i].Rect != image.Rect(i, 0, i+1, 1) {
			t.Fatal(i, g.Image[i].Rect)
		}
	}
	if total != 1000 {
		t.Fatal(total)
	}
	for x := range 960 {
		r, _, _, _ := g.Image[0].At(x, 1).RGBA()
		delta := int(r>>8) - x*255/959
		if delta < -5 || delta > 5 {
			t.Fatal(x, delta)
		}
	}
	t.Logf("%d bytes", size)
}
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Encode(ctx, io.Discard, 15, demo); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSingle(t *testing.T) {
	var b bytes.Buffer
	src := func(y func(*image.RGBA) error) error { return y(image.NewRGBA(image.Rect(0, 0, 1, 1))) }
	if err := Encode(t.Context(), &b, 100, src); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&b)
	if err != nil || len(g.Image) != 1 {
		t.Fatal(err)
	}
}
func BenchmarkDemo(b *testing.B) {
	for b.Loop() {
		if err := Encode(b.Context(), io.Discard, 15, demo); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTransparentInterior(t *testing.T) {
	var b bytes.Buffer
	source := func(yield func(*image.RGBA) error) error {
		frame := image.NewRGBA(image.Rect(0, 0, 9, 1))
		if err := yield(frame); err != nil {
			return err
		}
		for _, x := range []int{0, 8} {
			frame.SetRGBA(x, 0, color.RGBA{255, 255, 255, 255})
		}
		return yield(frame)
	}
	if err := Encode(t.Context(), &b, 30, source); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&b)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha := g.Image[1].At(4, 0).RGBA()
	if alpha != 0 || g.Image[1].Rect != g.Image[0].Rect {
		t.Fatal("unchanged interior should preserve previous frame")
	}
}
