package capture

import (
	"errors"
	"image"
)

// ErrPermissionDenied 表示系统拒绝了当前进程的屏幕录制权限（ScreenCaptureKit -3801）。
var ErrPermissionDenied = errors.New("需要屏幕录制权限")

// Display 是要截的一块屏。ID 是 CGDirectDisplayID。
// Bounds 用 DIP，原点在主屏左上角，y 向下。
type Display struct {
	ID     int64
	Bounds image.Rectangle
	Scale  float64
}

// WindowInfo 描述一个可见窗口的 CGWindowID 与全局 DIP 边界。
type WindowInfo struct {
	PopupMenu bool
	ID        uint32
	Bounds    image.Rectangle
}
