package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 拖拽临时目录 0o700、文件 0o600；已存在的 0o755 目录会被收紧。
func TestWriteDragTempPNGPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snipgo-drag")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	full, ok := writeDragTempPNG(dir, []byte("png"), "t")
	if !ok {
		t.Fatal("写入失败")
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", st.Mode().Perm())
	}
	if st, _ := os.Stat(full); st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", st.Mode().Perm())
	}
}
