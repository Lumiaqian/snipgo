package capture

import (
	"image"
	"testing"
)

func TestMenuOwnerOnDisplay(t *testing.T) {
	ds := []Display{{Bounds: image.Rect(0, 0, 500, 500)}}
	rect := image.Rect(10, 20, 87, 54)
	for _, tc := range []struct {
		path string
		r    image.Rectangle
		want bool
	}{
		{"/Applications/Fixture.app", rect, true},
		{"/Library/Input Methods/Input.app", rect, false},
		{"/System/Library/CoreServices/Spotlight.app", rect, false},
		{"", rect, false},
		{"/Applications/Fixture.app", rect.Add(image.Pt(500, 0)), false},
	} {
		if got := menuOwnerOnDisplay(tc.path, tc.r, ds); got != tc.want {
			t.Errorf("%s %v: got %v", tc.path, tc.r, got)
		}
	}
}
