package recordsess

import (
	"image"
	"snipgo/internal/capture"
	"testing"
)

func TestCrossDisplayUsesLargestIntersectionInLocalDIP(t *testing.T) {
	displays := []capture.Display{{ID: 1, Bounds: image.Rect(0, 0, 1440, 900), Scale: 2}, {ID: 2, Bounds: image.Rect(-1280, -100, 0, 800), Scale: 1}}
	r, err := Select(image.Rect(-150, 50, 50, 150), displays)
	if err != nil || r.Display.ID != 2 || r.Local != image.Rect(1130, 150, 1280, 250) {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestMinimumAppliesAfterClipping(t *testing.T) {
	_, err := Select(image.Rect(-100, 10, 63, 58), []capture.Display{{ID: 1, Bounds: image.Rect(0, 0, 800, 600), Scale: 2}})
	if err == nil {
		t.Fatal("accepted 63 DIP wide capture")
	}
}
