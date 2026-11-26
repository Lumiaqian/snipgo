package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteUniqueFileNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	p1, err := writeUniqueFile(dir, "a.png", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := writeUniqueFile(dir, "a.png", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatalf("两次写入路径相同: %s", p1)
	}
	if filepath.Base(p2) != "a-1.png" {
		t.Fatalf("后缀应为 a-1.png, got %s", filepath.Base(p2))
	}
	if b, _ := os.ReadFile(p1); string(b) != "one" {
		t.Fatalf("第一份被覆盖: %q", b)
	}
	if b, _ := os.ReadFile(p2); string(b) != "two" {
		t.Fatalf("第二份内容错误: %q", b)
	}
}

func TestPNGSaveOutcomes(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, dir, choice     string
		dialog, quick, failed bool
	}{
		{"quick", dir, "", false, true, false},
		{"dialog", dir, filepath.Join(dir, "chosen.png"), true, false, false},
		{"fallback-cancel", blocked, "", false, false, false},
		{"write-error", blocked, dir, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			path, quick, err := writePNG([]byte("image"), tc.dir, "quick.png", tc.dialog, func(string) (string, error) { called = true; return tc.choice, nil })
			if called == tc.quick || quick != tc.quick || (err != nil) != tc.failed {
				t.Fatalf("dialog=%v quick=%v err=%v", called, quick, err)
			}
			if !tc.quick && path != tc.choice {
				t.Fatalf("path %q", path)
			}
			if path != "" && err == nil {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "image" {
					t.Fatalf("saved %q: %v", data, err)
				}
			}
		})
	}
}
