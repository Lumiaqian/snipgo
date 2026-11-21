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
