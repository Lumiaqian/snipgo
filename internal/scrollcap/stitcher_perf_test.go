package scrollcap

import (
	"image"
	"image/color"
	"image/draw"
	"math/rand"
	"runtime"
	"testing"
)

// randomDoc 生成带逐像素随机纹理的长文档（保证滚动匹配唯一）。
func randomDoc(w, h int, seed int64) *image.NRGBA {
	rng := rand.New(rand.NewSource(seed))
	doc := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng.Read(doc.Pix)
	for i := 3; i < len(doc.Pix); i += 4 {
		doc.Pix[i] = 255
	}
	return doc
}

// 复现评审基准：1600 宽、1200 视口、每帧滚 150px 直到 DefaultMaxHeight。
// 旧实现每帧整体复制已累积图像（appendVertical + 每帧 Result），累计分配 >10 GB。
func TestStitcherLongScrollAllocBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("长滚动基准较慢")
	}
	const (
		w, viewH, step = 1600, 1200, 150
	)
	docH := DefaultMaxHeight + viewH
	doc := randomDoc(w, docH, 1)
	frame := image.NewNRGBA(image.Rect(0, 0, w, viewH))

	st := New()
	var ms runtime.MemStats
	var total uint64
	measure := func(fn func()) {
		runtime.ReadMemStats(&ms)
		before := ms.TotalAlloc
		fn()
		runtime.ReadMemStats(&ms)
		total += ms.TotalAlloc - before
	}

	pushes := 0
	for off := 0; off+viewH <= docH; off += step {
		draw.Draw(frame, frame.Bounds(), doc, image.Pt(0, off), draw.Src)
		measure(func() { st.Push(frame) })
		pushes++
		// UI 以 ≥250ms 节流更新预览（约每 1~2 帧一次），这里按每 2 帧取一次预览。
		if pushes%2 == 0 {
			measure(func() { st.Preview(480) })
		}
		if st.Capped() {
			break
		}
	}
	_, h := st.Size()
	if h != DefaultMaxHeight {
		t.Fatalf("stitched height = %d, want %d (frames=%d)", h, DefaultMaxHeight, st.Frames())
	}
	const budget = 3 << 30
	t.Logf("TotalAlloc(Push+Preview) = %.2f GB, frames=%d", float64(total)/(1<<30), st.Frames())
	if total > budget {
		t.Fatalf("Push+Preview 累计分配 %.2f GB，超过预算 %.2f GB", float64(total)/(1<<30), float64(budget)/(1<<30))
	}
}

// Result 必须与直接从源文档按行拼出的参考完全一致（含固定头尾，滚动条区域内容恒定）。
func TestStitcherResultMatchesReference(t *testing.T) {
	const (
		w, viewH, headH, footH = 200, 220, 24, 20
		inner                  = viewH - headH - footH
	)
	doc := randomDoc(w, 900, 7)
	head := image.NewNRGBA(image.Rect(0, 0, w, headH))
	foot := image.NewNRGBA(image.Rect(0, 0, w, footH))
	draw.Draw(head, head.Bounds(), image.NewUniform(color.NRGBA{30, 34, 44, 255}), image.Point{}, draw.Src)
	draw.Draw(foot, foot.Bounds(), image.NewUniform(color.NRGBA{22, 24, 32, 255}), image.Point{}, draw.Src)
	render := func(off int) *image.NRGBA {
		vp := image.NewNRGBA(image.Rect(0, 0, w, viewH))
		draw.Draw(vp, image.Rect(0, 0, w, headH), head, image.Point{}, draw.Src)
		draw.Draw(vp, image.Rect(0, headH, w, headH+inner), doc, image.Pt(0, off), draw.Src)
		draw.Draw(vp, image.Rect(0, viewH-footH, w, viewH), foot, image.Point{}, draw.Src)
		return vp
	}

	st := New()
	offs := []int{0, 40, 95, 150, 150, 230, 301, 360}
	for _, off := range offs {
		st.Push(render(off))
	}
	last := offs[len(offs)-1]
	want := image.NewNRGBA(image.Rect(0, 0, w, headH+inner+last+footH))
	draw.Draw(want, image.Rect(0, 0, w, headH), head, image.Point{}, draw.Src)
	draw.Draw(want, image.Rect(0, headH, w, headH+inner+last), doc, image.Pt(0, 0), draw.Src)
	draw.Draw(want, image.Rect(0, headH+inner+last, w, want.Bounds().Dy()), foot, image.Point{}, draw.Src)

	got := st.Result()
	if got == nil || got.Bounds() != want.Bounds() {
		t.Fatalf("size = %v, want %v", got.Bounds(), want.Bounds())
	}
	for i := range want.Pix {
		if got.Pix[i] != want.Pix[i] {
			y := i / want.Stride
			t.Fatalf("pixel mismatch at row %d (byte %d)", y, i)
		}
	}
	// Preview 宽度受限、高度成比例，且随后续 Push 增量更新后与整图缩小近似。
	pv := st.Preview(100)
	if pv.Bounds().Dx() != 100 {
		t.Fatalf("preview width = %d, want 100", pv.Bounds().Dx())
	}
	wantPvH := want.Bounds().Dy() / 2
	if d := pv.Bounds().Dy() - wantPvH; d < -len(offs) || d > len(offs) {
		t.Fatalf("preview height = %d, want ~%d", pv.Bounds().Dy(), wantPvH)
	}
	st.Push(render(420))
	pv2 := st.Preview(100)
	if pv2.Bounds().Dy() <= pv.Bounds().Dy() {
		t.Fatalf("preview should grow after push: %d -> %d", pv.Bounds().Dy(), pv2.Bounds().Dy())
	}
	if sw, _ := st.Size(); sw != w {
		t.Fatalf("Size width = %d", sw)
	}
}

func TestStitcherCapped(t *testing.T) {
	const w, viewH = 200, 200
	doc := randomDoc(w, 1200, 3)
	st := New()
	st.MaxHeight = 320
	if st.Capped() {
		t.Fatal("new stitcher must not be capped")
	}
	for off := 0; off+viewH <= 1200 && off < 400; off += 40 {
		st.Push(doc.SubImage(image.Rect(0, off, w, off+viewH)).(*image.NRGBA))
		if _, h := st.Size(); h < st.MaxHeight && st.Capped() {
			t.Fatalf("capped before reaching MaxHeight (h=%d)", h)
		}
	}
	if _, h := st.Size(); h != 320 {
		t.Fatalf("height = %d, want 320", h)
	}
	if !st.Capped() {
		t.Fatal("expected Capped after refused push at MaxHeight")
	}
}
