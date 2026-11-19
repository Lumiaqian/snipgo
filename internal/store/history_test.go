package store

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestHistoryStoreRingEvictionAndReload(t *testing.T) {
	tmp := t.TempDir()
	st, err := Open(tmp, 3)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	makePNG := func(w int) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, w, 10))
		img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}

	var first Entry
	for i := 1; i <= 5; i++ {
		e, err := st.Add(makePNG(10*i), 10*i, 10)
		if err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
		if i == 1 {
			first = e
		}
	}

	if st.Count() != 3 {
		t.Fatalf("Count = %d, want 3", st.Count())
	}
	// 最早的第 1 张应已被物理删除。
	if _, err := os.Stat(first.FilePath); !os.IsNotExist(err) {
		t.Fatalf("expected evicted file %s to be deleted", first.FilePath)
	}

	// 重新打开同一目录应完整恢复最新 3 张（50, 40, 30）。
	st2, err := Open(tmp, 3)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	list := st2.List()
	if len(list) != 3 || list[0].Width != 50 || list[2].Width != 30 {
		t.Fatalf("unexpected reloaded list: %+v", list)
	}
	img, raw, err := Load(list[0])
	if err != nil || len(raw) == 0 || img.Bounds().Dx() != 50 {
		t.Fatalf("Load failed: img=%v len=%d err=%v", img, len(raw), err)
	}
}
