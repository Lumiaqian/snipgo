//go:build !darwin

package textrender

import (
	"image"
	"image/color"
	"strings"
)

// Options 控制文本栅格化样式。
type Options struct {
	FontSize  float64
	Weight    float64
	Monospace bool
	MaxWidth  int
	Color     color.NRGBA
}

// Render 在非 macOS 平台上使用纯 Go 兜底栅格化。
func Render(text string, opts Options) *image.NRGBA {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if opts.FontSize <= 0 {
		opts.FontSize = 24
	}
	if opts.Color.A == 0 {
		opts.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return renderFallback(text, opts)
}
