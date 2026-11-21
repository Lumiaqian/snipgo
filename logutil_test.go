package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotateLogIfLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SnipGo.log")

	if rotateLogIfLarge(path, 10) {
		t.Fatal("文件不存在不应轮转")
	}
	os.WriteFile(path, []byte("small"), 0o600)
	if rotateLogIfLarge(path, 10) {
		t.Fatal("未超限不应轮转")
	}
	os.WriteFile(path+".1", []byte("older"), 0o600)
	big := []byte("0123456789ABCDEF")
	os.WriteFile(path, big, 0o600)
	if !rotateLogIfLarge(path, 10) {
		t.Fatal("超限应轮转")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("轮转后原路径应不存在，由调用方新建")
	}
	if b, _ := os.ReadFile(path + ".1"); string(b) != string(big) {
		t.Fatalf(".1 应被替换为刚轮转的内容, got %q", b)
	}
}

func TestLogThrottle(t *testing.T) {
	now := time.Unix(1000, 0)
	th := newLogThrottle(10*time.Second, func() time.Time { return now })

	if ok, n := th.Allow("A"); !ok || n != 0 {
		t.Fatalf("首条应输出, ok=%v n=%d", ok, n)
	}
	now = now.Add(160 * time.Millisecond)
	for i := 0; i < 3; i++ {
		if ok, _ := th.Allow("A"); ok {
			t.Fatal("窗口内相同消息应被压制")
		}
	}
	// 消息变化：输出，并汇报此前压制的 3 条。
	if ok, n := th.Allow("B"); !ok || n != 3 {
		t.Fatalf("变化后应输出并汇报 3 条, ok=%v n=%d", ok, n)
	}
	// 同一条消息超过窗口后再次输出。
	now = now.Add(11 * time.Second)
	if ok, n := th.Allow("B"); !ok || n != 0 {
		t.Fatalf("窗口过期后应再次输出, ok=%v n=%d", ok, n)
	}
}
