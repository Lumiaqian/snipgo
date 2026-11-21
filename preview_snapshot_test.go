package main

import (
	"context"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"snipgo/internal/crop"
	"snipgo/internal/session"
)

// newPreviewSnapApp 构造一个实时会话与无窗口的 app，并注入可观测的采集函数。
func newPreviewSnapApp(t *testing.T, capture func(ctx context.Context, sel image.Rectangle) (*image.NRGBA, error)) *app {
	t.Helper()
	prevReady, prevCap, prevMain := previewSnapReady, previewCaptureRect, runOnMain
	t.Cleanup(func() { previewSnapReady, previewCaptureRect, runOnMain = prevReady, prevCap, prevMain })
	previewSnapReady = func() bool { return true }
	var wg sync.WaitGroup
	previewCaptureRect = func(_ *app, ctx context.Context, sel image.Rectangle) (*image.NRGBA, error) {
		wg.Add(1)
		return capture(ctx, sel)
	}
	runOnMain = func(func()) { wg.Done() } // 不关心结果回主线程，只观察采集的启动与取消
	// 清理时等所有采集 goroutine 走完，避免其读取全局注入点与恢复动作竞争（后注册先执行）。
	t.Cleanup(func() { wg.Wait() })
	a := &app{}
	a.session = &session.Session{
		Live: func() (session.Snapshot, error) {
			return session.Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 800, 600), Scale: 2}}}, nil
		},
		Clip:   testClip{},
		UI:     testSessionUI{},
		OnMain: func(fn func()) { fn() },
	}
	a.session.StartLive()
	return a
}

func TestPreviewSnapshotSkippedWhileDragging(t *testing.T) {
	var started atomic.Int32
	a := newPreviewSnapApp(t, func(ctx context.Context, sel image.Rectangle) (*image.NRGBA, error) {
		started.Add(1)
		return nil, ctx.Err()
	})
	a.session.BeginDrag(0, 10, 10)
	for i := 1; i <= 20; i++ {
		a.session.MoveDrag(0, float64(10+i*5), float64(10+i*3))
		if !a.session.Dragging() {
			t.Fatal("setup: session should be dragging")
		}
		sel, _, _ := a.session.ActiveRect()
		a.ensurePreviewSnapshot(sel)
	}
	time.Sleep(20 * time.Millisecond)
	if n := started.Load(); n != 0 {
		t.Fatalf("captures started while dragging: %d, want 0", n)
	}
	if a.previewSnapPending != (image.Rectangle{}) {
		t.Fatalf("pending %v while dragging", a.previewSnapPending)
	}
}

func TestPreviewSnapshotCancelsPreviousCapture(t *testing.T) {
	var (
		mu        sync.Mutex
		inFlight  int
		maxFlight int
		started   int
		canceled  int
	)
	a := newPreviewSnapApp(t, func(ctx context.Context, sel image.Rectangle) (*image.NRGBA, error) {
		mu.Lock()
		inFlight++
		started++
		maxFlight = max(maxFlight, inFlight)
		mu.Unlock()
		<-ctx.Done() // 阻塞到被取消或超时
		mu.Lock()
		inFlight--
		if ctx.Err() == context.Canceled {
			canceled++
		}
		mu.Unlock()
		return nil, ctx.Err()
	})
	a.session.BeginDrag(0, 10, 10)
	a.session.MoveDrag(0, 100, 100)
	a.session.EndDrag()
	if a.session.Dragging() {
		t.Fatal("setup: drag should have ended")
	}
	base, locked, ok := a.session.ActiveRect()
	if !ok || !locked {
		t.Fatalf("setup: rect %v locked %v ok %v", base, locked, ok)
	}
	for i := 0; i < 20; i++ {
		a.ensurePreviewSnapshot(image.Rect(10, 10, 100+i, 100+i))
		// 给上一次采集 goroutine 一点时间进入阻塞，保证计数可观察。
		time.Sleep(2 * time.Millisecond)
	}
	// 等被取消的采集全部退出。
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		cur := inFlight
		mu.Unlock()
		if cur <= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	gotStarted, gotInFlight, gotCanceled, gotMax := started, inFlight, canceled, maxFlight
	mu.Unlock()
	if gotStarted != 20 || gotInFlight > 1 || gotCanceled != 19 {
		t.Fatalf("started %d inFlight %d canceled %d, want 20/<=1/19", gotStarted, gotInFlight, gotCanceled)
	}
	if gotMax > 3 {
		t.Fatalf("max concurrent in flight %d: previous captures are not canceled promptly", gotMax)
	}

	// 取消（HideSelection 同路径）后最后一次在途采集也应退出；等其完全退出，避免泄漏到其他测试。
	a.cancelPreviewSnap()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		cur := inFlight
		mu.Unlock()
		if cur == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("last capture still in flight after cancelPreviewSnap")
}
