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

func TestBGRAPremultipliedClampsBadChannel(t *testing.T) {
	// 预乘数据里通道不该大于 alpha，坏数据截到 255 而不是回绕。
	raw := []byte{200, 0, 0, 100}
	img, err := bgraToNRGBA(raw, 1, 1, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(0, 0); c.B != 255 {
		t.Fatalf("pixel %v, want blue 255", c)
	}
}

func TestBGRATrimmedFinalRowStridePadding(t *testing.T) {
	// 2 行 × 1 像素，stride=64，但最后一行没有尾部对齐填充（总长 64 + 4 = 68 < 128）。
	raw := make([]byte, 68)
	raw[0], raw[1], raw[2], raw[3] = 1, 2, 3, 255
	raw[64], raw[65], raw[66], raw[67] = 4, 5, 6, 255
	img, err := bgraToNRGBA(raw, 1, 2, 64, false)
	if err != nil {
		t.Fatalf("unexpected error on trimmed final row: %v", err)
	}
	if c := img.NRGBAAt(0, 1); c != (color.NRGBA{R: 6, G: 5, B: 4, A: 255}) {
		t.Fatalf("row 1 pixel = %v", c)
	}
}
