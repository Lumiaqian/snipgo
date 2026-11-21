// Package scrollcap 实现滚动长截图纵向特征匹配与去固定头尾无缝拼接。
package scrollcap

import (
	"image"
	"image/draw"
	"math"
)

// DefaultMaxHeight 限制单张长截图最大物理像素高度，防止极端无限滚动占满内存。
const DefaultMaxHeight = 24000

// strip 是滚动正文中的一段横条。pv 是按预览宽度缩小后的缓存（nil 表示需重算）。
type strip struct {
	img *image.NRGBA
	pv  *image.NRGBA
}

// Stitcher 接收同一屏幕选区连续采集的多帧位图，自动剥离上下固定栏并按滚动位移纵向拼接。
//
// 滚动正文以横条列表存储：每帧只追加新增的那一条，不再整体复制已累积内容（O(n) 而非 O(n²)）。
// 非并发安全，调用方需自行加锁。
type Stitcher struct {
	MaxHeight int

	prev      *image.NRGBA
	body      []*strip // 已累积的「顶部 + 滚动正文」（不含上一帧底部固定栏）
	bodyW     int
	bodyH     int
	tail      *image.NRGBA // 最新一帧的底部固定栏（实时合成到 Result 末尾）
	tailPv    *image.NRGBA
	pvW       int // 预览缓存对应的目标宽度
	frames    int
	totalGrow int
	capped    bool
}

// New 创建长截图拼接器。
func New() *Stitcher {
	return &Stitcher{MaxHeight: DefaultMaxHeight}
}

// Frames 返回已成功采纳的有效帧数（含首帧）。
func (s *Stitcher) Frames() int { return s.frames }

// Capped 返回是否已达到 MaxHeight 且有新帧因此被拒绝。
func (s *Stitcher) Capped() bool { return s.capped }

// Size 返回当前拼接成品的物理像素宽高。
func (s *Stitcher) Size() (w, h int) {
	if len(s.body) == 0 {
		return 0, 0
	}
	h = s.bodyH
	if s.tail != nil {
		h += s.tail.Bounds().Dy()
	}
	return s.bodyW, h
}

// Result 生成当前完整长截图副本（含首帧头部 + 全部滚动正文 + 最新底部固定栏）。
// 会整体分配一次，仅应在完成时调用；实时预览请用 Preview。
func (s *Stitcher) Result() *image.NRGBA {
	if len(s.body) == 0 {
		return nil
	}
	w, h := s.Size()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	y := 0
	for _, st := range s.body {
		y += copyRows(out, y, st.img)
	}
	if s.tail != nil && !s.tail.Bounds().Empty() {
		copyRows(out, y, s.tail)
	}
	return out
}

// Preview 返回宽度不超过 maxW 的缩小合成图（面积平均），用于实时预览。
// 每条横条的缩小结果被缓存，只有新增/被修改的条带需要重算，单次开销与新增高度成正比。
func (s *Stitcher) Preview(maxW int) *image.NRGBA {
	if len(s.body) == 0 || maxW <= 0 {
		return nil
	}
	if maxW != s.pvW {
		s.pvW = maxW
		s.tailPv = nil
		for _, st := range s.body {
			st.pv = nil
		}
	}
	scaled := func(img *image.NRGBA) *image.NRGBA {
		return scaleToWidth(img, maxW)
	}
	totalH := 0
	for _, st := range s.body {
		if st.pv == nil {
			st.pv = scaled(st.img)
		}
		totalH += st.pv.Bounds().Dy()
	}
	if s.tail != nil {
		if s.tailPv == nil {
			s.tailPv = scaled(s.tail)
		}
		totalH += s.tailPv.Bounds().Dy()
	}
	out := image.NewNRGBA(image.Rect(0, 0, s.body[0].pv.Bounds().Dx(), totalH))
	y := 0
	for _, st := range s.body {
		y += copyRows(out, y, st.pv)
	}
	if s.tail != nil {
		copyRows(out, y, s.tailPv)
	}
	return out
}

// copyRows 把 src 整体复制到 dst 的第 y 行起，返回复制的行数。
func copyRows(dst *image.NRGBA, y int, src *image.NRGBA) int {
	sb := src.Bounds()
	rows := sb.Dy()
	rowLen := min(sb.Dx(), dst.Bounds().Dx()) * 4
	for r := range rows {
		so := src.PixOffset(sb.Min.X, sb.Min.Y+r)
		do := dst.PixOffset(0, y+r)
		copy(dst.Pix[do:do+rowLen], src.Pix[so:so+rowLen])
	}
	return rows
}

// scaleToWidth 用面积平均把 img 缩到宽度 maxW（等比）；宽度不超过 maxW 时原样返回（只读共享）。
func scaleToWidth(img *image.NRGBA, maxW int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW || w == 0 || h == 0 {
		return img
	}
	ow := maxW
	oh := max(1, int(math.Round(float64(h)*float64(ow)/float64(w))))
	return boxScale(img, ow, oh)
}

// boxScale 面积平均缩放（仅缩小）。
func boxScale(src *image.NRGBA, ow, oh int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for oy := range oh {
		y0 := oy * h / oh
		y1 := max(y0+1, (oy+1)*h/oh)
		for ox := range ow {
			x0 := ox * w / ow
			x1 := max(x0+1, (ox+1)*w/ow)
			var r, g, bl, n int
			for y := y0; y < y1; y++ {
				row := src.Pix[src.PixOffset(b.Min.X+x0, b.Min.Y+y):]
				for x := 0; x < (x1-x0)*4; x += 4 {
					r += int(row[x])
					g += int(row[x+1])
					bl += int(row[x+2])
					n++
				}
			}
			o := dst.PixOffset(ox, oy)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(bl / n)
			dst.Pix[o+3] = 255
		}
	}
	return dst
}

// trimBottom 从正文末尾去掉 n 行。
func (s *Stitcher) trimBottom(n int) {
	for n > 0 && len(s.body) > 0 {
		last := s.body[len(s.body)-1]
		h := last.img.Bounds().Dy()
		if h <= n {
			s.body = s.body[:len(s.body)-1]
			s.bodyH -= h
			n -= h
			continue
		}
		keep := h - n
		img := last.img
		last.img = &image.NRGBA{Pix: img.Pix[:keep*img.Stride], Stride: img.Stride, Rect: image.Rect(0, 0, img.Bounds().Dx(), keep)}
		last.pv = nil
		s.bodyH -= n
		return
	}
}

// pushStrip 追加一条已独立持有像素的横条。
func (s *Stitcher) pushStrip(img *image.NRGBA) {
	s.body = append(s.body, &strip{img: img})
	s.bodyH += img.Bounds().Dy()
}

// blendTail 在正文末尾 feather 行与 src 的接缝上方 feather 行之间做平滑过渡，消除抗锯齿抖动缝隙。
func (s *Stitcher) blendTail(src *image.NRGBA, bandMinY, feather int) {
	// 从末尾向前收集需要修改的行（至多 feather 行，可能跨条）。
	rows := make([][]uint8, feather)
	need := feather
	for i := len(s.body) - 1; i >= 0 && need > 0; i-- {
		st := s.body[i]
		sh := st.img.Bounds().Dy()
		took := false
		for r := sh - 1; r >= 0 && need > 0; r-- {
			need--
			rows[need] = st.img.Pix[st.img.PixOffset(0, r):]
			took = true
		}
		if took {
			st.pv = nil
		}
	}
	w := s.bodyW
	for f := range feather {
		alpha := float64(f+1) / float64(feather+1)
		sy := bandMinY - feather + f
		oRow := rows[f]
		sRow := src.Pix[src.PixOffset(src.Bounds().Min.X, sy):]
		for x := range w {
			i := x * 4
			oRow[i] = uint8(math.Round(float64(oRow[i])*(1-alpha) + float64(sRow[i])*alpha))
			oRow[i+1] = uint8(math.Round(float64(oRow[i+1])*(1-alpha) + float64(sRow[i+1])*alpha))
			oRow[i+2] = uint8(math.Round(float64(oRow[i+2])*(1-alpha) + float64(sRow[i+2])*alpha))
			oRow[i+3] = 255
		}
	}
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
		s.prev = cloneNRGBA(frame, fb)
		s.body = nil
		s.bodyH = 0
		s.bodyW = w
		s.pushStrip(cloneNRGBA(frame, fb))
		s.tail, s.tailPv = nil, nil
		s.frames = 1
		return h, true
	}

	if s.prev.Bounds().Dx() != w || s.prev.Bounds().Dy() != h {
		return 0, false
	}
	_, curH := s.Size()
	if curH >= maxH {
		s.capped = true
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

	// 首帧尚未剥离底部固定栏时，先将 prev 的 [botAct..h) 从正文拆出作为动态尾栏。
	if s.tail == nil && botAct < h {
		if trimH := s.bodyH - (h - botAct); trimH > 0 {
			s.trimBottom(h - botAct)
		}
	} else if s.tail != nil {
		// 若本帧检测到的底部固定栏高度变化，对齐到新的 botAct。
		oldTailH := s.tail.Bounds().Dy()
		newTailH := h - botAct
		if newTailH > oldTailH {
			// 需要从正文末尾多拆一点，或直接按重叠位置衔接
			diff := newTailH - oldTailH
			if s.bodyH > diff+16 {
				s.trimBottom(diff)
			}
		} else if newTailH < oldTailH {
			// 把之前暂存尾栏里滚入正文的部分补回
			s.pushStrip(cloneNRGBA(s.tail, image.Rect(0, 0, w, oldTailH-newTailH)))
		}
	}

	avail := maxH - (s.bodyH + (h - botAct))
	if avail <= 0 {
		s.capped = true
		return 0, false
	}
	useDY := min(dy, avail)
	newBand := image.Rect(fb.Min.X, fb.Min.Y+botAct-dy, fb.Min.X+w, fb.Min.Y+botAct-dy+useDY)
	feather := max(0, min(4, min(useDY/2, botAct-topAct-dy)))
	if feather > 0 && s.bodyH > feather && newBand.Min.Y >= feather {
		s.blendTail(frame, newBand.Min.Y, feather)
	}
	s.pushStrip(cloneNRGBA(frame, newBand))

	if botAct < h {
		s.tail = cloneNRGBA(frame, image.Rect(fb.Min.X, fb.Min.Y+botAct, fb.Min.X+w, fb.Max.Y))
	} else {
		s.tail = nil
	}
	s.tailPv = nil
	copyRows(s.prev, 0, frame) // 复用上一帧缓冲，避免每帧整帧分配
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
