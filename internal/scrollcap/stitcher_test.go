package scrollcap

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestStitcherStripsStickyHeaderFooterAndScrollbar(t *testing.T) {
	// 构造一张高 600px、宽 240px 的长文档原图，透过 240x240 的视口（含 30px 固定顶栏、26px 固定底栏和右侧滚动条）分多帧滚屏采集。
	const (
		docW   = 240
		docH   = 600
		viewH  = 240
		headH  = 30
		footH  = 26
		innerH = viewH - headH - footH // 184px 可滚视口
	)
	doc := image.NewNRGBA(image.Rect(0, 0, docW, docH))
	for y := range docH {
		for x := range docW {
			// 生成不重复的行级条纹与色块纹理模拟聊天消息/代码行
			r := uint8((y*7 + x*3) % 220)
			g := uint8(((y/12)*37 + (x/20)*19) % 220)
			b := uint8(((y/5)*53 + x) % 220)
			doc.SetNRGBA(x, y, color.NRGBA{R: 25 + r, G: 25 + g, B: 25 + b, A: 255})
		}
	}

	renderViewport := func(scrollY int) *image.NRGBA {
		vp := image.NewNRGBA(image.Rect(0, 0, docW, viewH))
		// 固定顶栏
		draw.Draw(vp, image.Rect(0, 0, docW, headH), image.NewUniform(color.NRGBA{R: 32, G: 36, B: 46, A: 255}), image.Point{}, draw.Src)
		// 滚动正文
		draw.Draw(vp, image.Rect(0, headH, docW, headH+innerH), doc, image.Pt(0, scrollY), draw.Src)
		// 固定底栏
		draw.Draw(vp, image.Rect(0, viewH-footH, docW, viewH), image.NewUniform(color.NRGBA{R: 24, G: 26, B: 34, A: 255}), image.Point{}, draw.Src)
		// 反向移动的右侧滚动条滑块（宽度 12px）
		thumbY := headH + scrollY*100/max(1, docH-innerH)
		draw.Draw(vp, image.Rect(docW-14, thumbY, docW-4, min(viewH-footH, thumbY+40)), image.NewUniform(color.NRGBA{R: 180, G: 180, B: 190, A: 255}), image.Point{}, draw.Src)
		return vp
	}

	st := New()
	scrollOffsets := []int{0, 0, 52, 115, 115, 180, 250, 316}
	for _, off := range scrollOffsets {
		st.Push(renderViewport(off))
	}

	res := st.Result()
	if res == nil {
		t.Fatal("expected stitched result")
	}
	// 期望最终高度精确等于：固定顶栏(30) + 滚动正文(innerH 184 + 最后滚幅 316 = 500) + 固定底栏(26) = 556px！
	wantH := headH + innerH + 316 + footH
	gotH := res.Bounds().Dy()
	if gotH < wantH-2 || gotH > wantH+2 {
		t.Fatalf("stitched height = %d, want ~%d (frames=%d)", gotH, wantH, st.Frames())
	}
	if st.Frames() != 6 {
		t.Fatalf("accepted frames = %d, want 6 (stationary duplicates should be skipped)", st.Frames())
	}
}
