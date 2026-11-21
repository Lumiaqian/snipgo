package main

import (
	"math"
	"testing"
)

func TestLongPinGeometry(t *testing.T) {
	w, h, z := longPinGeometry(1600, 12000, 800)
	if w != 800 || h != 6000 {
		t.Fatalf("dip = %dx%d, want 800x6000", w, h)
	}
	if math.Abs(float64(h)*z-680) > 1 {
		t.Fatalf("显示高度 %v, want ≈680", float64(h)*z)
	}
	ratio := (float64(w) * z) / (float64(h) * z)
	want := 1600.0 / 12000.0
	if math.Abs(ratio-want)/want > 0.01 {
		t.Fatalf("宽高比 %v, want %v", ratio, want)
	}
	// 短图不缩放
	if _, _, z := longPinGeometry(400, 300, 400); z != 1 {
		t.Fatalf("短图 zoom = %v, want 1", z)
	}
}
