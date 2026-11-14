//go:build !darwin

package capture

import (
	"context"
	"errors"

	"snipgo/internal/crop"
)

// Grab 在非 macOS 上没有实现。
func Grab(context.Context, []Display) ([]crop.Screen, error) {
	return nil, errors.ErrUnsupported
}
