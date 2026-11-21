package main

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"snipgo/internal/beautify"
)

// fakeAsync 把后台任务收集起来由测试手动执行，主线程调度同步执行。
type fakeAsync struct{ jobs []func() }

func installFakeAsync(t *testing.T) *fakeAsync {
	t.Helper()
	f := &fakeAsync{}
	prevBG, prevMain := runBackground, runOnMain
	t.Cleanup(func() { runBackground, runOnMain = prevBG, prevMain })
	runBackground = func(fn func()) { f.jobs = append(f.jobs, fn) }
	runOnMain = func(fn func()) { fn() }
	return f
}

func (f *fakeAsync) runAll() {
	jobs := f.jobs
	f.jobs = nil
	for _, j := range jobs {
		j()
	}
}

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func TestTemplateExportRunsInBackgroundAndIgnoresRepeatClicks(t *testing.T) {
	f := installFakeAsync(t)
	tw := &templateWindow{srcImg: solidNRGBA(80, 50, color.NRGBA{200, 80, 40, 255}), srcScale: 2, opts: beautify.DefaultTemplateOptions()}
	a := &app{tplWin: tw}

	calls := 0
	done := func(*image.NRGBA, []byte) { calls++ }
	a.withFullExport(tw, done)
	if calls != 0 || !tw.exporting || len(f.jobs) != 1 {
		t.Fatalf("首次导出应只排队后台任务: calls=%d exporting=%v jobs=%d", calls, tw.exporting, len(f.jobs))
	}
	a.withFullExport(tw, done) // 重复点击
	a.withFullExport(tw, done)
	if len(f.jobs) != 1 {
		t.Fatalf("导出期间重复点击不应再排队: jobs=%d", len(f.jobs))
	}
	f.runAll()
	if calls != 1 || tw.exporting {
		t.Fatalf("导出完成后应回调一次: calls=%d exporting=%v", calls, tw.exporting)
	}
	// 缓存命中：同步回调，不再起后台任务。
	a.withFullExport(tw, done)
	if calls != 2 || len(f.jobs) != 0 {
		t.Fatalf("缓存命中应同步回调: calls=%d jobs=%d", calls, len(f.jobs))
	}
}

func TestTemplateExportStaleResultDropped(t *testing.T) {
	f := installFakeAsync(t)
	tw := &templateWindow{srcImg: solidNRGBA(80, 50, color.NRGBA{20, 80, 200, 255}), srcScale: 2, opts: beautify.DefaultTemplateOptions()}
	a := &app{tplWin: tw}
	calls := 0
	done := func(*image.NRGBA, []byte) { calls++ }

	// 导出期间参数变化：结果不执行动作，但 exporting 复位，且新参数可再次导出。
	a.withFullExport(tw, done)
	tw.opts.Padding = beautify.PadNormal + 1
	f.runAll()
	if calls != 0 || tw.exporting {
		t.Fatalf("参数变化后应丢弃结果: calls=%d exporting=%v", calls, tw.exporting)
	}
	a.withFullExport(tw, done)
	if len(f.jobs) != 1 {
		t.Fatal("参数变化后应能重新导出")
	}
	f.runAll()
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}

	// 导出期间窗口被替换/关闭：丢弃。
	tw.opts.Padding = beautify.PadNormal
	a.withFullExport(tw, done)
	a.tplWin = nil
	f.runAll()
	if calls != 1 {
		t.Fatalf("窗口关闭后不应执行动作: calls=%d", calls)
	}
}

func newComparePins() (*app, *pinWin, *pinWin) {
	p := &pinWin{img: solidNRGBA(40, 30, color.NRGBA{255, 0, 0, 255}), origW: 40, origH: 30, compareSplit: 0.5}
	q := &pinWin{img: solidNRGBA(40, 30, color.NRGBA{0, 0, 255, 255}), origW: 40, origH: 30, compareSplit: 0.5}
	return &app{pins: []*pinWin{p, q}}, p, q
}

func TestPinCompareDiffComputedInBackground(t *testing.T) {
	f := installFakeAsync(t)
	a, p, _ := newComparePins()
	a.togglePinCompare(p)
	if !p.compareMode || !p.comparePending || p.compareDiffBmp != nil || len(f.jobs) != 1 {
		t.Fatalf("进入对比时 diff 应在后台排队: mode=%v pending=%v bmp=%v jobs=%d", p.compareMode, p.comparePending, p.compareDiffBmp != nil, len(f.jobs))
	}
	f.runAll()
	if p.comparePending || p.compareDiffBmp == nil || p.compareDiffRes.DiffPercent < 99 {
		t.Fatalf("diff 完成后应应用结果: pending=%v bmp=%v pct=%.1f", p.comparePending, p.compareDiffBmp != nil, p.compareDiffRes.DiffPercent)
	}
}

func TestPinCompareStaleDiffDropped(t *testing.T) {
	f := installFakeAsync(t)
	a, p, _ := newComparePins()

	// 对比在 diff 完成前就退出：结果不得写回。
	a.togglePinCompare(p)
	p.exitCompareMode()
	f.runAll()
	if p.compareMode || p.compareDiffBmp != nil || p.compareDiffRes.Image != nil || p.comparePending {
		t.Fatalf("退出对比后过期 diff 不应改变状态: bmp=%v res=%v", p.compareDiffBmp != nil, p.compareDiffRes.Image != nil)
	}

	// 退出后立刻重进：旧一轮的结果（gen 不同）也要丢弃，只应用新一轮。
	a.togglePinCompare(p)
	oldJobs := f.jobs
	f.jobs = nil
	p.exitCompareMode()
	a.togglePinCompare(p)
	for _, j := range oldJobs {
		j()
	}
	if p.compareDiffBmp != nil || !p.comparePending {
		t.Fatal("上一轮对比的结果不应应用到新一轮")
	}
	f.runAll()
	if p.compareDiffBmp == nil || p.comparePending {
		t.Fatal("新一轮结果应被应用")
	}
}

func TestMergeAllPinsIgnoresRepeatWhileBusy(t *testing.T) {
	f := installFakeAsync(t)
	a, _, _ := newComparePins()
	mergePinsBusy.Store(false)
	t.Cleanup(func() { mergePinsBusy.Store(false) })
	a.mergeAllPins(true)
	a.mergeAllPins(true)
	if len(f.jobs) != 1 {
		t.Fatalf("拼接进行中重复触发应被忽略: jobs=%d", len(f.jobs))
	}
}
