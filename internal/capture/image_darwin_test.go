//go:build darwin

package capture

import (
	"image/color"
	"testing"
)

func TestBGRAStraight(t *testing.T) {
	raw := []byte{10, 20, 30, 255, 0, 0, 0, 0}
	img, err := bgraToNRGBA(raw, 1, 1, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(0, 0); c != (color.NRGBA{R: 30, G: 20, B: 10, A: 255}) {
		t.Fatalf("pixel %v", c)
	}
}

func TestBGRAPremultiplied(t *testing.T) {
	// B=10 在 alpha 128 下还原为 10*255/128 = 19。
	raw := []byte{10, 0, 0, 128}
	img, err := bgraToNRGBA(raw, 1, 1, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(0, 0); c != (color.NRGBA{R: 0, G: 0, B: 19, A: 128}) {
		t.Fatalf("pixel %v, want blue 19", c)
	}
}

func TestImageFromBGRARejectsOtherLayouts(t *testing.T) {
	raw := []byte{1, 2, 3, 255}
	if _, err := imageFromBGRA(raw, 1, 1, 4, alphaPremultipliedFirst); err == nil {
		t.Fatal("missing little-endian flag")
	}
	info := uint32(byteOrder32Little | alphaPremultipliedFirst)
	img, err := imageFromBGRA(raw, 1, 1, 4, info)
	if err != nil {
		t.Fatal(err)
	}
	// alpha 是 255，预乘还原不改变通道。内存顺序是 B,G,R,A。
	if c := img.NRGBAAt(0, 0); c != (color.NRGBA{R: 3, G: 2, B: 1, A: 255}) {
		t.Fatalf("pixel %v", c)
	}
}

func TestBGRAZeroAlphaClearsColor(t *testing.T) {
	raw := []byte{10, 20, 30, 0}
	img, err := bgraToNRGBA(raw, 1, 1, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(0, 0); c != (color.NRGBA{}) {
		t.Fatalf("pixel %v", c)
	}
}
