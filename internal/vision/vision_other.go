//go:build !darwin

package vision

import (
	"errors"
	"image"
)

// Recognize 在非 macOS 平台返回不支持错误。
func Recognize(*image.NRGBA) (Result, error) {
	return Result{}, errors.New("当前平台不支持离线 OCR")
}
