//go:build darwin

package capture

import (
	"image"
	"testing"
)

func TestIsFullDisplayOverlay(t *testing.T) {
	displays := []Display{
		{ID: 1, Bounds: image.Rect(0, 0, 1728, 1117), Scale: 2},
		{ID: 2, Bounds: image.Rect(1728, 0, 3648, 1080), Scale: 2},
	}
	if !isFullDisplayOverlay(image.Rect(0, 0, 1728, 1117), displays) {
		t.Fatal("primary full-display overlay should be detected")
	}
	if !isFullDisplayOverlay(image.Rect(1728, 0, 3648, 1080), displays) {
		t.Fatal("secondary full-display overlay should be detected")
	}
	if isFullDisplayOverlay(image.Rect(0, 33, 1728, 1056), displays) {
		t.Fatal("maximized window below menu bar/above Dock is not a full-display daemon overlay")
	}
	if isFullDisplayOverlay(image.Rect(724, 168, 1004, 655), displays) {
		t.Fatal("normal accessory dialog should not be treated as full-display overlay")
	}
}

func TestVisibleWindowsReturnsValidRects(t *testing.T) {
	displays := []Display{
		{ID: 1, Bounds: image.Rect(0, 0, 1728, 1117), Scale: 2},
		{ID: 2, Bounds: image.Rect(1728, 0, 3648, 1080), Scale: 2},
	}
	front, wins := VisibleWindows(image.Pt(500, 500), displays, "SnipGo 选区")
	for i, w := range wins {
		if w.Empty() || w.Dx() < minDetectWindowDIP || w.Dy() < minDetectWindowDIP {
			t.Fatalf("win[%d] invalid bounds: %v", i, w)
		}
	}
	if len(wins) > 0 && front.Empty() {
		t.Fatalf("expected non-empty front window when %d windows exist", len(wins))
	}
}
