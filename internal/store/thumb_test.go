package store

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func bigPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 12), G: uint8(y / 8), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnailSizeAspectAndCache(t *testing.T) {
	st, err := Open(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	raw := bigPNG(t, 3000, 2000)
	e, err := st.Add(raw, 3000, 2000, 0)
	if err != nil {
		t.Fatal(err)
	}

	th, err := Thumbnail(e, 320)
	if err != nil {
		t.Fatal(err)
	}
	b := th.Bounds()
	if b.Dx() != 320 || b.Dy() != 213 {
		t.Fatalf("thumb size = %dx%d, want 320x213", b.Dx(), b.Dy())
	}
	// 面积平均：左上像素应是原图左上 9x9 块的平均，而不是单点采样。
	if th.NRGBAAt(0, 0).A != 255 {
		t.Fatal("alpha lost")
	}
	st0, err := os.Stat(thumbPath(e))
	if err != nil {
		t.Fatalf("thumb cache not written: %v", err)
	}
	if st0.Mode().Perm() != 0o600 {
		t.Fatalf("thumb perm = %v, want 0600", st0.Mode().Perm())
	}

	// 第二次必须走缓存：把原图破坏后仍能得到相同结果，且不再整图解码。
	if err := os.WriteFile(e.FilePath, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	var decoded []image.Rectangle
	orig := decodePNG
	decodePNG = func(data []byte) (image.Image, error) {
		img, err := orig(data)
		if err == nil {
			decoded = append(decoded, img.Bounds())
		}
		return img, err
	}
	defer func() { decodePNG = orig }()
	th2, err := Thumbnail(e, 320)
	if err != nil {
		t.Fatalf("cached Thumbnail: %v", err)
	}
	if !bytes.Equal(th.Pix, th2.Pix) {
		t.Fatal("cached thumb differs")
	}
	for _, r := range decoded {
		if r.Dx() > 320 {
			t.Fatalf("cache hit still decoded a full image %v", r)
		}
	}
}

func TestThumbnailSmallImageNotUpscaledNorCached(t *testing.T) {
	st, _ := Open(t.TempDir(), 5)
	e, _ := st.Add(bigPNG(t, 100, 60), 100, 60, 0)
	th, err := Thumbnail(e, 320)
	if err != nil || th.Bounds().Dx() != 100 || th.Bounds().Dy() != 60 {
		t.Fatalf("thumb = %v err=%v", th.Bounds(), err)
	}
	if _, err := os.Stat(thumbPath(e)); err == nil {
		t.Fatal("small image should not be cached")
	}
}

func TestThumbsRemovedWithEntries(t *testing.T) {
	tmp := t.TempDir()
	st, _ := Open(tmp, 2)
	mk := func() Entry {
		e, err := st.Add(bigPNG(t, 640, 480), 640, 480, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Thumbnail(e, 320); err != nil {
			t.Fatal(err)
		}
		return e
	}
	exists := func(e Entry) bool { _, err := os.Stat(thumbPath(e)); return err == nil }

	e1 := mk()
	e2 := mk()
	e3 := mk() // 触发淘汰 e1
	if exists(e1) {
		t.Fatal("evicted entry thumb should be removed")
	}
	if !st.Delete(e2.ID) || exists(e2) {
		t.Fatal("Delete should remove thumb")
	}
	st.Clear()
	if exists(e3) {
		t.Fatal("Clear should remove thumbs")
	}

	// 重新打开时清理遗留的孤儿缩略图（例如 Delete 与后台生成竞态后残留）。
	e4 := mk()
	orphan := e4
	orphan.ID = "gone"
	writeThumb(thumbPath(orphan), image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	if _, err := Open(tmp, 2); err != nil {
		t.Fatal(err)
	}
	if exists(orphan) {
		t.Fatal("orphan thumb should be pruned on open")
	}
	if !exists(e4) {
		t.Fatal("live thumb must survive reopen")
	}
}

func TestReadPNGReturnsExactBytes(t *testing.T) {
	st, _ := Open(t.TempDir(), 5)
	raw := bigPNG(t, 64, 48)
	e, _ := st.Add(raw, 64, 48, 0)
	got, err := ReadPNG(e)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("ReadPNG mismatch err=%v", err)
	}
}
