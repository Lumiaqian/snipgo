package main

import (
	"bytes"
	"image"
	"image/png"
	"sync"
	"testing"
	"time"

	"snipgo/internal/store"
)

func testHistoryPNG(w, h int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

// reloadHistoryWindow 必须只入队、不在调用方 goroutine 解码；缩略图由后台 worker 加载、经主线程投递。
func TestReloadHistoryWindowLoadsThumbsInBackground(t *testing.T) {
	st, err := store.Open(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	e1, _ := st.Add(testHistoryPNG(40, 30), 40, 30, 0)
	e2, _ := st.Add(testHistoryPNG(50, 30), 50, 30, 0)

	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	mainQ := make(chan func(), 8)
	prevLoad, prevRun := historyThumbLoader, runOnMain
	t.Cleanup(func() { historyThumbLoader, runOnMain = prevLoad, prevRun })
	historyThumbLoader = func(e store.Entry, maxSide int) (*image.NRGBA, error) {
		<-release // 模拟慢速整图解码
		mu.Lock()
		order = append(order, e.ID)
		mu.Unlock()
		return image.NewNRGBA(image.Rect(0, 0, 8, 6)), nil
	}
	runOnMain = func(fn func()) { mainQ <- fn }

	a := &app{history: st}
	hw := &historyWindow{}
	a.histWin = hw

	done := make(chan struct{})
	go func() { a.reloadHistoryWindow(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reloadHistoryWindow 被缩略图加载阻塞（仍在同步解码）")
	}
	if len(hw.entries) != 2 || len(hw.thumbs) != 0 {
		t.Fatalf("reload 返回时不应已有缩略图: entries=%d thumbs=%d", len(hw.entries), len(hw.thumbs))
	}

	// 重复 reload 不应重复入队同一条记录。
	a.reloadHistoryWindow()
	close(release)
	for range 2 {
		select {
		case fn := <-mainQ:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("等待缩略图投递超时")
		}
	}
	if len(hw.thumbs) != 2 || hw.thumbs[e1.ID] == nil || hw.thumbs[e2.ID] == nil {
		t.Fatalf("thumbs = %v", hw.thumbs)
	}
	mu.Lock()
	// List 为最新在前，worker 按序加载。
	if len(order) != 2 || order[0] != e2.ID || order[1] != e1.ID {
		t.Fatalf("加载次数/顺序错误: %v", order)
	}
	mu.Unlock()
}

// 窗口被关闭或替换后，迟到的缩略图结果必须被丢弃。
func TestHistoryThumbResultDroppedWhenWindowReplaced(t *testing.T) {
	st, _ := store.Open(t.TempDir(), 5)
	st.Add(testHistoryPNG(40, 30), 40, 30, 0)

	mainQ := make(chan func(), 4)
	prevLoad, prevRun := historyThumbLoader, runOnMain
	t.Cleanup(func() { historyThumbLoader, runOnMain = prevLoad, prevRun })
	historyThumbLoader = func(store.Entry, int) (*image.NRGBA, error) {
		return image.NewNRGBA(image.Rect(0, 0, 8, 6)), nil
	}
	runOnMain = func(fn func()) { mainQ <- fn }

	a := &app{history: st}
	old := &historyWindow{}
	a.histWin = old
	a.reloadHistoryWindow()
	fn := <-mainQ

	a.closeHistoryWindow() // 关闭并（随后）打开新窗口
	a.histWin = &historyWindow{}
	fn()
	if len(old.thumbs) != 0 {
		t.Fatalf("旧窗口收到了过期缩略图: %v", old.thumbs)
	}
	if !old.dead.Load() {
		t.Fatal("关闭窗口应标记 dead 使 worker 退出")
	}
}

// recordHistory 在后台写盘，完成后才通过主线程调度刷新界面。
func TestRecordHistoryWritesOffMainThread(t *testing.T) {
	st, _ := store.Open(t.TempDir(), 5)
	mainQ := make(chan func(), 2)
	prevRun := runOnMain
	t.Cleanup(func() { runOnMain = prevRun })
	runOnMain = func(fn func()) { mainQ <- fn }

	a := &app{history: st}
	a.recordHistory(testHistoryPNG(20, 20), 20, 20, 0)
	select {
	case <-mainQ:
	case <-time.After(2 * time.Second):
		t.Fatal("recordHistory 未在写盘后调度主线程刷新")
	}
	if st.Count() != 1 {
		t.Fatalf("Count = %d, want 1", st.Count())
	}
}

// 关闭「保存最近截图历史」后不再落盘；默认开启时照常记录。
func TestRecordHistoryRespectsDisableHistory(t *testing.T) {
	st, _ := store.Open(t.TempDir(), 5)
	mainQ := make(chan func(), 2)
	prevRun := runOnMain
	t.Cleanup(func() { runOnMain = prevRun })
	runOnMain = func(fn func()) { mainQ <- fn }

	a := &app{history: st}
	a.cfg.DisableHistory = true
	a.recordHistory(testHistoryPNG(20, 20), 20, 20, 0)
	select {
	case <-mainQ:
		t.Fatal("关闭历史后不应调度刷新")
	case <-time.After(200 * time.Millisecond):
	}
	if st.Count() != 0 {
		t.Fatalf("关闭历史后 Count = %d, want 0", st.Count())
	}
}
