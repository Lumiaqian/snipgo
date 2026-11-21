package main

import (
	"image"
	"testing"
)

// 负坐标显示器（主屏左侧 / 上方）上，贴图必须落在光标所在显示器内。
func TestPinPlacementNearCursorNegativeOriginDisplay(t *testing.T) {
	main := image.Rect(0, 0, 1920, 1080)
	left := image.Rect(-1440, -200, 0, 700)
	displays := []image.Rectangle{main, left}

	// 光标在左屏中部：居中于光标，不被推到主屏
	got := pinPlacementNearCursor(image.Pt(-700, 300), 400, 200, displays)
	if want := image.Pt(-900, 200); got != want {
		t.Fatalf("居中: got %v want %v", got, want)
	}
	// 光标靠左上角：夹紧到该屏左上 +24/+40
	got = pinPlacementNearCursor(image.Pt(-1430, -190), 400, 200, displays)
	if want := image.Pt(-1440+24, -200+40); got != want {
		t.Fatalf("左上夹紧: got %v want %v", got, want)
	}
	// 光标靠右下角：不超出该屏右/下
	got = pinPlacementNearCursor(image.Pt(-5, 695), 400, 200, displays)
	if want := image.Pt(0-400, 700-200); got != want {
		t.Fatalf("右下夹紧: got %v want %v", got, want)
	}
	if !got.In(left) {
		t.Fatalf("结果 %v 不在左屏内", got)
	}
}

// 没有显示器包含光标时回退到旧行为。
func TestPinPlacementNearCursorFallback(t *testing.T) {
	got := pinPlacementNearCursor(image.Pt(10, 10), 400, 200, nil)
	if want := image.Pt(24, 40); got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}
