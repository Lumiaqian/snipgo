//go:build darwin

package capture

import (
	"context"
	"image"
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"snipgo/internal/crop"
)

// TestGrabRealDisplays 连真机截一次屏。默认跳过，避免 go test 弹出屏幕录制权限框。
func TestGrabRealDisplays(t *testing.T) {
	if os.Getenv("SNIPGO_CAPTURE") != "1" {
		t.Skip("设置 SNIPGO_CAPTURE=1 才连真机截屏")
	}
	if err := loadFrameworks(); err != nil {
		t.Fatal(err)
	}
	var preflight func() bool
	purego.RegisterLibFunc(&preflight, purego.RTLD_DEFAULT, "CGPreflightScreenCaptureAccess")
	if !preflight() {
		t.Skip("没有屏幕录制权限，跳过以免弹出系统提示")
	}
	var pixelsWide func(uint32) uintptr
	var pixelsHigh func(uint32) uintptr
	purego.RegisterLibFunc(&pixelsWide, purego.RTLD_DEFAULT, "CGDisplayPixelsWide")
	purego.RegisterLibFunc(&pixelsHigh, purego.RTLD_DEFAULT, "CGDisplayPixelsHigh")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	content, err := shareableContent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer content.Send(selRelease)

	selWidth := objc.RegisterName("width")
	selHeight := objc.RegisterName("height")
	arr := content.Send(selDisplays)
	n := int(objc.Send[uintptr](arr, selCount))
	if n == 0 {
		t.Fatal("没有显示器")
	}
	displays := make([]Display, 0, n)
	for i := range n {
		d := arr.Send(selObjectAtIndex, uintptr(i))
		id := uint32(objc.Send[uint32](d, selDisplayID))
		w := int(objc.Send[int](d, selWidth))
		h := int(objc.Send[int](d, selHeight))
		pw := int(pixelsWide(id))
		ph := int(pixelsHigh(id))
		scale := float64(pw) / float64(w)
		if w <= 0 || h <= 0 || pw <= 0 || ph <= 0 {
			t.Fatalf("显示器 %d 点数 %dx%d 像素 %dx%d", id, w, h, pw, ph)
		}
		if crop.DIPToPixel(w, 0, scale) != pw || crop.DIPToPixel(h, 0, scale) != ph {
			t.Fatalf("显示器 %d 缩放 %v 把 %dx%d 点换成 %dx%d，像素是 %dx%d", id, scale, w, h, crop.DIPToPixel(w, 0, scale), crop.DIPToPixel(h, 0, scale), pw, ph)
		}
		displays = append(displays, Display{
			ID:     int64(id),
			Bounds: image.Rect(0, 0, w, h),
			Scale:  scale,
		})
	}

	frames, err := Grab(ctx, displays)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != len(displays) {
		t.Fatalf("截到 %d 块，要 %d 块", len(frames), len(displays))
	}
	for i, f := range frames {
		wantW := crop.DIPToPixel(f.Bounds.Max.X, f.Bounds.Min.X, f.Scale)
		wantH := crop.DIPToPixel(f.Bounds.Max.Y, f.Bounds.Min.Y, f.Scale)
		if f.Pixels == nil {
			t.Fatalf("显示器 %d 没有画面，要 %dx%d", displays[i].ID, wantW, wantH)
		}
		got := f.Pixels.Bounds()
		if got.Dx() != wantW || got.Dy() != wantH {
			t.Fatalf("显示器 %d 画面 %dx%d，要 %dx%d", displays[i].ID, got.Dx(), got.Dy(), wantW, wantH)
		}
	}
}
