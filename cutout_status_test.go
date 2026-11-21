package main

import (
	"image"
	"os"
	"testing"
)

// 抠图结果编码失败时必须给出失败状态，而不是停留在“正在抠取”。
func TestFinishCutoutSelectionEncodeFailureSetsStatus(t *testing.T) {
	prevEnc, prevRun := cutoutEncode, runOnMain
	t.Cleanup(func() { cutoutEncode, runOnMain = prevEnc, prevRun })
	cutoutEncode = func(*image.NRGBA) ([]byte, error) { return nil, os.ErrInvalid }
	runOnMain = func(fn func()) { fn() }

	a := &app{extraStatus: "正在离线 AI 抠取主体…"}
	a.finishCutoutSelection(image.NewNRGBA(image.Rect(0, 0, 4, 4)), image.Rect(0, 0, 4, 4), 2)
	if a.extraStatus != cutoutEncodeFailedStatus {
		t.Fatalf("extraStatus = %q, want %q", a.extraStatus, cutoutEncodeFailedStatus)
	}
}
