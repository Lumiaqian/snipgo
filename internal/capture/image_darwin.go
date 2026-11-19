//go:build darwin

package capture

import (
	"fmt"
	"image"
)

const (
	alphaInfoMask = 0x1F
	byteOrderMask = 0x7000

	alphaPremultipliedFirst = 2
	alphaFirst              = 4
	alphaNoneSkipFirst      = 6
	byteOrder32Little       = 2 << 12
)

// imageFromBGRA 读取 ScreenCaptureKit 交回的 32 位小端 BGRA。
// bitmapInfo 不是这种排列时返回错误，避免把颜色通道悄悄对调。
func imageFromBGRA(raw []byte, width, height, stride int, bitmapInfo uint32) (*image.NRGBA, error) {
	alpha := bitmapInfo & alphaInfoMask
	order := bitmapInfo & byteOrderMask
	if order != byteOrder32Little || (alpha != alphaPremultipliedFirst && alpha != alphaFirst && alpha != alphaNoneSkipFirst) {
		return nil, fmt.Errorf("无法识别的画面格式 bitmapInfo=%#x", bitmapInfo)
	}
	premul := alpha == alphaPremultipliedFirst
	return bgraToNRGBA(raw, width, height, stride, premul)
}

// bgraToNRGBA 把 BGRA 行复制成从 (0,0) 起的 NRGBA。premul 为真时按 alpha 还原颜色。
func bgraToNRGBA(raw []byte, width, height, stride int, premul bool) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 || stride < width*4 {
		return nil, fmt.Errorf("画面缓冲区是 %dx%d stride %d，字节 %d", width, height, stride, len(raw))
	}
	minBytes := (height-1)*stride + width*4
	if len(raw) < minBytes {
		return nil, fmt.Errorf("画面缓冲区是 %dx%d stride %d，字节 %d", width, height, stride, len(raw))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := raw[y*stride : y*stride+width*4]
		out := dst.Pix[y*dst.Stride : y*dst.Stride+width*4]
		for x := 0; x < width; x++ {
			i := x * 4
			b, g, r, a := row[i], row[i+1], row[i+2], row[i+3]
			if premul && a != 0 && a != 255 {
				r = unpremul(r, a)
				g = unpremul(g, a)
				b = unpremul(b, a)
			}
			if a == 0 {
				r, g, b = 0, 0, 0
			}
			out[i] = r
			out[i+1] = g
			out[i+2] = b
			out[i+3] = a
		}
	}
	return dst, nil
}

// unpremul 还原预乘颜色。通道大于 alpha 的坏数据截到 255，不让它回绕成别的颜色。
func unpremul(c, a uint8) uint8 {
	return uint8(min(uint16(c)*255/uint16(a), 255))
}
