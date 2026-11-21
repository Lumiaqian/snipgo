package store

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
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
		e, err := st.Add(makePNG(10*i), 10*i, 10, 0)
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

func TestHistoryStoreReopenNoIDCollision(t *testing.T) {
	tmp := t.TempDir()
	s1, err := Open(tmp, 10)
	if err != nil {
		t.Fatal(err)
	}
	e1, err := s1.Add([]byte("first"), 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟重启：seq 归零，同一秒内再次写入
	s2, err := Open(tmp, 10)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s2.Add([]byte("second"), 2, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e1.ID == e2.ID || e1.FilePath == e2.FilePath {
		t.Fatalf("冲突: %q vs %q", e1.ID, e2.ID)
	}
	if got, _ := os.ReadFile(e1.FilePath); string(got) != "first" {
		t.Fatalf("第一张被覆盖: %q", got)
	}
	if got, _ := os.ReadFile(e2.FilePath); string(got) != "second" {
		t.Fatalf("第二张内容错误: %q", got)
	}
	if n := s2.Count(); n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}
	if !s2.Delete(e2.ID) {
		t.Fatal("Delete 失败")
	}
	if got, err := os.ReadFile(e1.FilePath); err != nil || string(got) != "first" {
		t.Fatalf("删除第二张后第一张应保留: %v %q", err, got)
	}
	if !strings.HasPrefix(e1.FileName, "SnipGo-") || !strings.HasSuffix(e1.FileName, ".png") {
		t.Fatalf("文件名格式: %s", e1.FileName)
	}
}

func TestHistoryStoreLoadIndexDropsDuplicateIDs(t *testing.T) {
	tmp := t.TempDir()
	s1, err := Open(tmp, 10)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s1.Add([]byte("x"), 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]Entry{e, e})
	if err := os.WriteFile(s1.indexPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(tmp, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n := s2.Count(); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}
}
func TestEntryScaleRoundTripAndFallback(t *testing.T) {
	base := t.TempDir()
	s, _ := Open(base, 3)
	e, err := s.Add([]byte("x"), 200, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := e.DipSize(); w != 200 || h != 100 {
		t.Fatalf("Scale=1 DipSize = %d,%d want 200,100", w, h)
	}
	s2, _ := Open(base, 3)
	if got := s2.List()[0]; got.Scale != 1 {
		t.Fatalf("重新加载后 Scale = %v, want 1", got.Scale)
	}
	old := Entry{Width: 200, Height: 100}
	if w, h := old.DipSize(); w != 100 || h != 50 {
		t.Fatalf("旧记录 DipSize = %d,%d want 100,50", w, h)
	}
}
