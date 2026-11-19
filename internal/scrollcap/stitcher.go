// Package scrollcap 实现滚动长截图纵向特征匹配与去固定头尾无缝拼接。
package scrollcap

import (
	"image"
	"image/draw"
	"math"
)

// DefaultMaxHeight 限制单张长截图最大物理像素高度，防止极端无限滚动占满内存。
const DefaultMaxHeight = 24000

// Stitcher 接收同一屏幕选区连续采集的多帧位图，自动剥离上下固定栏并按滚动位移纵向拼接。
type Stitcher struct {
	MaxHeight int

	prev      *image.NRGBA
	headBody  *image.NRGBA // 已累积的「顶部 + 滚动正文」（不含上一帧底部固定栏）
	tail      *image.NRGBA // 最新一帧的底部固定栏（实时合成到 Result 末尾）
	frames    int
	totalGrow int
}

// New 创建长截图拼接器。
func New() *Stitcher {
	return &Stitcher{MaxHeight: DefaultMaxHeight}
}

// Frames 返回已成功采纳的有效帧数（含首帧）。
func (s *Stitcher) Frames() int { return s.frames }

// Size 返回当前拼接成品的物理像素宽高。
func (s *Stitcher) Size() (w, h int) {
	if s.headBody == nil {
		return 0, 0
	}
	w = s.headBody.Bounds().Dx()
	h = s.headBody.Bounds().Dy()
	if s.tail != nil {
		h += s.tail.Bounds().Dy()
	}
	return w, h
}

// Result 生成当前完整长截图副本（含首帧头部 + 全部滚动正文 + 最新底部固定栏）。
func (s *Stitcher) Result() *image.NRGBA {
	if s.headBody == nil {
		return nil
	}
	w, h := s.Size()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	hb := s.headBody.Bounds()
	draw.Draw(out, image.Rect(0, 0, w, hb.Dy()), s.headBody, hb.Min, draw.Src)
	if s.tail != nil && !s.tail.Bounds().Empty() {
		tb := s.tail.Bounds()
		draw.Draw(out, image.Rect(0, hb.Dy(), w, h), s.tail, tb.Min, draw.Src)
	}
	return out
}

// Push 送入最新一帧选区截图。返回本帧新增拼接的像素高度与画面是否发生有效向下滚动。
func (s *Stitcher) Push(frame *image.NRGBA) (addedPx int, ok bool) {
	if frame == nil || frame.Bounds().Empty() {
		return 0, false
	}
	fb := frame.Bounds()
	w, h := fb.Dx(), fb.Dy()
	if w < 16 || h < 32 {
		return 0, false
	}
	maxH := s.MaxHeight
	if maxH <= 0 {
		maxH = DefaultMaxHeight
	}

	if s.prev == nil {
		cp := cloneNRGBA(frame, fb)
		s.prev = cp
		s.headBody = cloneNRGBA(cp, cp.Bounds())
		s.tail = nil
		s.frames = 1
		return h, true
	}

	if s.prev.Bounds().Dx() != w || s.prev.Bounds().Dy() != h {
		return 0, false
	}
	_, curH := s.Size()
	if curH >= maxH {
		return 0, false
	}

	// 排除右侧滚动条区域（最多 24px）与左右 4px 边框，避免滚动条反向移动干扰特征匹配。
	x0 := min(4, w/8)
	x1 := max(x0+8, w-min(24, w/8))

	topAct, botAct, active := detectActiveBand(s.prev, frame, x0, x1)
	if !active || botAct-topAct < 24 {
		return 0, false
	}

	dy, matched := estimateDownwardScroll(s.prev, frame, topAct, botAct, x0, x1)
	if !matched || dy < 3 {
		return 0, false
	}

	// 首帧尚未剥离底部固定栏时，先将 prev 的 [botAct..h) 从 headBody 拆出作为动态尾栏。
	if s.tail == nil && botAct < h {
		hb := s.headBody.Bounds()
		trimH := hb.Dy() - (h - botAct)
		if trimH > 0 {
			s.headBody = cloneNRGBA(s.headBody, image.Rect(0, 0, w, trimH))
		}
	} else if s.tail != nil {
		// 若本帧检测到的底部固定栏高度变化，对齐到新的 botAct。
		oldTailH := s.tail.Bounds().Dy()
		newTailH := h - botAct
		if newTailH > oldTailH {
			// 需要从 headBody 末尾多拆一点，或直接按重叠位置衔接
			diff := newTailH - oldTailH
			if s.headBody.Bounds().Dy() > diff+16 {
				s.headBody = cloneNRGBA(s.headBody, image.Rect(0, 0, w, s.headBody.Bounds().Dy()-diff))
			}
		} else if newTailH < oldTailH {
			// 把之前暂存尾栏里滚入正文的部分补回
			recoverR := image.Rect(0, 0, w, oldTailH-newTailH)
			s.headBody = appendVertical(s.headBody, s.tail, recoverR, 0)
		}
	}

	avail := maxH - (s.headBody.Bounds().Dy() + (h - botAct))
	if avail <= 0 {
		return 0, false
	}
	useDY := min(dy, avail)
	newBand := image.Rect(fb.Min.X, fb.Min.Y+botAct-dy, fb.Min.X+w, fb.Min.Y+botAct-dy+useDY)
	feather := min(4, min(useDY/2, botAct-topAct-dy))
	s.headBody = appendVertical(s.headBody, frame, newBand, max(0, feather))

	if botAct < h {
		s.tail = cloneNRGBA(frame, image.Rect(fb.Min.X, fb.Min.Y+botAct, fb.Min.X+w, fb.Max.Y))
	} else {
		s.tail = nil
	}
	s.prev = cloneNRGBA(frame, fb)
	s.frames++
	s.totalGrow += useDY
	return useDY, true
}

func detectActiveBand(prev, curr *image.NRGBA, x0, x1 int) (top, bot int, ok bool) {
	h := prev.Bounds().Dy()
	pb := prev.Bounds()
	cb := curr.Bounds()
	rowDiff := func(y int) float64 {
		pRow := prev.Pix[prev.PixOffset(pb.Min.X, pb.Min.Y+y):]
		cRow := curr.Pix[curr.PixOffset(cb.Min.X, cb.Min.Y+y):]
		var sum int
		cnt := 0
		for x := x0; x < x1; x += 2 {
			i := x * 4
			sum += absInt(int(pRow[i])-int(cRow[i])) +
				absInt(int(pRow[i+1])-int(cRow[i+1])) +
				absInt(int(pRow[i+2])-int(cRow[i+2]))
			cnt += 3
		}
		if cnt == 0 {
			return 0
		}
		return float64(sum) / float64(cnt)
	}

	top = -1
	for y := range h {
		if rowDiff(y) > 2.5 {
			top = y
			break
		}
	}
	if top < 0 {
		return 0, 0, false
	}
	bot = top + 1
	for y := h - 1; y > top; y-- {
		if rowDiff(y) > 2.5 {
			bot = y + 1
			break
		}
	}
	return top, bot, bot-top >= 16
}

// estimateDownwardScroll 在活跃滚动区间 [topAct, botAct) 内寻找内容向上平移量 dy > 0（即用户向下滚屏 dy 像素）。
// prev[y] == curr[y - dy]。
func estimateDownwardScroll(prev, curr *image.NRGBA, topAct, botAct, x0, x1 int) (int, bool) {
	actH := botAct - topAct
	bandH := max(14, min(48, actH/5))
	if actH <= bandH+6 {
		return 0, false
	}

	// 在 prev 的底部 1/3 区域挑选纹理最丰富的水平条带作为匹配锚点：
	// 向下滚动时底部内容向上进入视口，锚点越靠下，可容忍的单步最大滚幅 maxDY（最高达视口 80%+）越大！
	bestVar := -1.0
	searchHi := botAct - bandH - 2
	searchLo := max(topAct+actH/2, searchHi-actH/4)
	anchorPrevY := searchHi
	for y := searchHi; y >= searchLo; y -= max(4, bandH/3) {
		v := bandTextureVariance(prev, y, bandH, x0, x1)
		if v > bestVar {
			bestVar = v
			anchorPrevY = y
		}
	}
	if bestVar < 1.2 {
		return 0, false
	}

	maxDY := anchorPrevY - topAct
	if maxDY < 3 {
		return 0, false
	}

	// 预计算一维行亮度均值以快速初筛候选 dy。
	prevLuma := rowLumaProfile(prev, topAct, botAct, x0, x1)
	currLuma := rowLumaProfile(curr, topAct, botAct, x0, x1)

	type cand struct {
		dy    int
		score float64
	}
	cands := make([]cand, 0, maxDY)
	for dy := 3; dy <= maxDY; dy++ {
		var diff float64
		for k := range bandH {
			py := anchorPrevY - topAct + k
			cy := py - dy
			diff += math.Abs(prevLuma[py] - currLuma[cy])
		}
		cands = append(cands, cand{dy: dy, score: diff / float64(bandH)})
	}

	// 对 1D 得分最优的前 12 个候选位置（及其邻域）执行二维 RGB SAD 精匹配。
	bestDY := 0
	bestSAD := math.MaxFloat64
	checked := map[int]bool{}
	for round := 0; round < min(12, len(cands)); round++ {
		pick := -1
		pickScore := math.MaxFloat64
		for i, c := range cands {
			if !checked[c.dy] && c.score < pickScore {
				pickScore = c.score
				pick = i
			}
		}
		if pick < 0 {
			break
		}
		baseDY := cands[pick].dy
		for delta := -1; delta <= 1; delta++ {
			dy := baseDY + delta
			if dy < 3 || dy > maxDY || checked[dy] {
				continue
			}
			checked[dy] = true
			sad := sadBand(prev, anchorPrevY, curr, anchorPrevY-dy, bandH, x0, x1)
			if sad < bestSAD {
				bestSAD = sad
				bestDY = dy
			}
		}
	}

	if bestDY < 3 || bestSAD > 18.0 {
		return 0, false
	}
	return bestDY, true
}

func bandTextureVariance(img *image.NRGBA, y0, bandH, x0, x1 int) float64 {
	b := img.Bounds()
	var sum, sumSq float64
	var n int
	for y := y0; y < y0+bandH; y += 2 {
		row := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):]
		for x := x0; x < x1; x += 4 {
			i := x * 4
			lum := 0.299*float64(row[i]) + 0.587*float64(row[i+1]) + 0.114*float64(row[i+2])
			sum += lum
			sumSq += lum * lum
			n++
		}
	}
	if n < 2 {
		return 0
	}
	mean := sum / float64(n)
	return max(0, sumSq/float64(n)-mean*mean)
}

func rowLumaProfile(img *image.NRGBA, top, bot, x0, x1 int) []float64 {
	b := img.Bounds()
	out := make([]float64, bot-top)
	for i := range out {
		row := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+top+i):]
		var sum float64
		cnt := 0
		for x := x0; x < x1; x += 4 {
			o := x * 4
			sum += 0.299*float64(row[o]) + 0.587*float64(row[o+1]) + 0.114*float64(row[o+2])
			cnt++
		}
		if cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

func sadBand(prev *image.NRGBA, py0 int, curr *image.NRGBA, cy0, bandH, x0, x1 int) float64 {
	pb := prev.Bounds()
	cb := curr.Bounds()
	var sum int
	cnt := 0
	for k := range bandH {
		pRow := prev.Pix[prev.PixOffset(pb.Min.X, pb.Min.Y+py0+k):]
		cRow := curr.Pix[curr.PixOffset(cb.Min.X, cb.Min.Y+cy0+k):]
		for x := x0; x < x1; x += 2 {
			i := x * 4
			sum += absInt(int(pRow[i])-int(cRow[i])) +
				absInt(int(pRow[i+1])-int(cRow[i+1])) +
				absInt(int(pRow[i+2])-int(cRow[i+2]))
			cnt += 3
		}
	}
	if cnt == 0 {
		return math.MaxFloat64
	}
	return float64(sum) / float64(cnt)
}

func appendVertical(top *image.NRGBA, src *image.NRGBA, band image.Rectangle, feather int) *image.NRGBA {
	tb := top.Bounds()
	w := tb.Dx()
	oldH := tb.Dy()
	addH := band.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, oldH+addH))
	draw.Draw(out, image.Rect(0, 0, w, oldH), top, tb.Min, draw.Src)
	draw.Draw(out, image.Rect(0, oldH, w, oldH+addH), src, band.Min, draw.Src)

	// 在接缝上方 feather 行执行平滑过渡，消除微弱渲染抗锯齿抖动缝隙。
	if feather > 0 && oldH > feather && band.Min.Y >= feather {
		for f := range feather {
			alpha := float64(f+1) / float64(feather+1)
			oy := oldH - feather + f
			sy := band.Min.Y - feather + f
			oRow := out.Pix[out.PixOffset(0, oy):]
			sRow := src.Pix[src.PixOffset(band.Min.X, sy):]
			for x := range w {
				i := x * 4
				oRow[i] = uint8(math.Round(float64(oRow[i])*(1-alpha) + float64(sRow[i])*alpha))
				oRow[i+1] = uint8(math.Round(float64(oRow[i+1])*(1-alpha) + float64(sRow[i+1])*alpha))
				oRow[i+2] = uint8(math.Round(float64(oRow[i+2])*(1-alpha) + float64(sRow[i+2])*alpha))
				oRow[i+3] = 255
			}
		}
	}
	return out
}

func cloneNRGBA(src *image.NRGBA, r image.Rectangle) *image.NRGBA {
	r = r.Intersect(src.Bounds())
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	return dst
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
