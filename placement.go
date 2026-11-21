package main

import (
	"image"

	"github.com/egoist/mygo"
)

// pinPlacementNearCursor 让贴图以光标为中心落点，并限制在光标所在显示器的工作区内；
// 左/上保留 24/40 的边距（相对该显示器左上角），右/下不超出工作区。
// 没有显示器包含光标时回退到旧行为（仅按 24/40 下限夹紧），以免在负坐标显示器上被推回主屏。
func pinPlacementNearCursor(cur image.Point, dipW, dipH int, displays []image.Rectangle) image.Point {
	x, y := cur.X-dipW/2, cur.Y-dipH/2
	for _, wa := range displays {
		if !cur.In(wa) {
			continue
		}
		minX, minY := wa.Min.X+24, wa.Min.Y+40
		maxX, maxY := max(minX, wa.Max.X-dipW), max(minY, wa.Max.Y-dipH)
		return image.Pt(min(max(x, minX), maxX), min(max(y, minY), maxY))
	}
	return image.Pt(max(24, x), max(40, y))
}

// pinPlacementNearCursorOnScreen 读取当前光标与各显示器工作区后调用 pinPlacementNearCursor（须在主线程调用）。
func pinPlacementNearCursorOnScreen(dipW, dipH int) image.Point {
	cur := mygo.Screen.CursorScreenPoint()
	var areas []image.Rectangle
	for _, d := range mygo.Screen.Displays() {
		wa := d.WorkArea
		areas = append(areas, image.Rect(wa.X, wa.Y, wa.X+wa.Width, wa.Y+wa.Height))
	}
	return pinPlacementNearCursor(image.Pt(cur.X, cur.Y), dipW, dipH, areas)
}
