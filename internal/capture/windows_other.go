//go:build !darwin

package capture

import "image"

// VisibleWindows 在非 macOS 上返回空结果。
func VisibleWindows(image.Point, []Display, string) (image.Rectangle, []image.Rectangle) {
	return image.Rectangle{}, nil
}

// VisibleWindowInfos 在非 macOS 上返回空结果。
func VisibleWindowInfos(image.Point, []Display, string) (WindowInfo, []WindowInfo) {
	return WindowInfo{}, nil
}
