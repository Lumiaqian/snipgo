package capture

import "image"

// Display 是要截的一块屏。ID 是 CGDirectDisplayID。
// Bounds 用 DIP，原点在主屏左上角，y 向下。
type Display struct {
	ID     int64
	Bounds image.Rectangle
	Scale  float64
}
