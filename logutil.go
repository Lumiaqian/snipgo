package main

import (
	"os"
	"sync"
	"time"
)

const (
	maxLogBytes      = 5 << 20 // 日志超过 5 MB 则在启动时轮转
	captureErrLogGap = 10 * time.Second
)

// captureErrLog 限流「capture error」日志。
var captureErrLog = newLogThrottle(captureErrLogGap, time.Now)

// rotateLogIfLarge 在日志超过 limit 字节时把它改名为 path+".1"（覆盖旧的 .1），
// 让调用方随后打开的是全新的空文件。返回是否发生了轮转。
func rotateLogIfLarge(path string, limit int64) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= limit {
		return false
	}
	_ = os.Remove(path + ".1")
	return os.Rename(path, path+".1") == nil
}

// logThrottle 抑制窗口期内重复出现的同一条日志，并在文本变化或窗口过期后汇报被抑制的次数。
type logThrottle struct {
	mu         sync.Mutex
	gap        time.Duration
	now        func() time.Time
	last       string
	lastAt     time.Time
	suppressed int
}

func newLogThrottle(gap time.Duration, now func() time.Time) *logThrottle {
	return &logThrottle{gap: gap, now: now}
}

// Allow 报告这条日志是否应输出；suppressed 是此前被压制的相同日志条数（仅在 ok 时有意义）。
func (t *logThrottle) Allow(msg string) (ok bool, suppressed int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if t.last == msg && now.Sub(t.lastAt) < t.gap {
		t.suppressed++
		return false, 0
	}
	suppressed = t.suppressed
	t.last, t.lastAt, t.suppressed = msg, now, 0
	return true, suppressed
}
