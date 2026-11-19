//go:build !darwin

package capture

import (
	"context"
	"errors"
	"image"

	"snipgo/internal/crop"
)

func HasPermission() bool     { return false }
func RequestPermission() bool { return false }

// Grab 在非 macOS 上没有实现。
func Grab(context.Context, []Display, string) ([]crop.Screen, error) {
	return nil, errors.ErrUnsupported
}

// GrabRect 在非 macOS 上没有实现。
func GrabRect(context.Context, []Display, image.Rectangle, string) (*image.NRGBA, error) {
	return nil, errors.ErrUnsupported
}

// GrabWindow 在非 macOS 上没有实现。
func GrabWindow(context.Context, uint32, float64) (*image.NRGBA, error) {
	return nil, errors.ErrUnsupported
}
