// Package vision 封装 macOS 原生离线 OCR 文字识别与二维码/条码检测。
package vision

import (
	"cmp"
	"image"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextBox 表示识别到的单段文本及其在原图中的归一化矩形 [0,1]（原点左上角，y 向下）。
type TextBox struct {
	Text string
	X    float64
	Top  float64
	W    float64
	H    float64
}

// Result 是一次图像视觉分析输出。
type Result struct {
	Text     string    // 按阅读顺序拼接的识别文本
	Barcodes []string  // 检测到的二维码/条形码内容（去重保序）
	Boxes    []TextBox // 各文本行在原图中的精确归一化位置
}

var (
	urlPattern = regexp.MustCompile(`https?://[^\s<>"'()（）【】]+`)

	sensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{10,}\b`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{12,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{16,}\b`),
		regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
		regexp.MustCompile(`(?:\+?86[-\s]?)?1[3-9]\d{9}\b`),
		regexp.MustCompile(`\b\d{17}[\dXx]\b`),
		regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
	}
	kvSecretPattern = regexp.MustCompile(`(?i)(?:token|secret|password|passwd|api[_-]?key|access[_-]?key|bearer)\s*[:=]\s*(\S+)`)
)

// SensitiveBoxes 自动扫描识别结果中的敏感凭证/隐私字段（API Key、Token、邮箱、手机号、身份证、IP），
// 按字符跨度在单行内精确定位需脱敏的子矩形 [0,1]。
func SensitiveBoxes(res Result) []TextBox {
	var out []TextBox
	for _, bx := range res.Boxes {
		if bx.Text == "" || bx.W <= 0 || bx.H <= 0 {
			continue
		}
		totalRunes := max(1, utf8.RuneCountInString(bx.Text))
		type span struct{ lo, hi int }
		var spans []span
		addByteSpan := func(b0, b1 int) {
			r0 := utf8.RuneCountInString(bx.Text[:b0])
			r1 := utf8.RuneCountInString(bx.Text[:b1])
			if r1 > r0 {
				spans = append(spans, span{r0, r1})
			}
		}
		for _, re := range sensitivePatterns {
			for _, idx := range re.FindAllStringIndex(bx.Text, -1) {
				addByteSpan(idx[0], idx[1])
			}
		}
		for _, sub := range kvSecretPattern.FindAllStringSubmatchIndex(bx.Text, -1) {
			if len(sub) >= 4 && sub[2] >= 0 {
				addByteSpan(sub[2], sub[3])
			}
		}
		if len(spans) == 0 {
			continue
		}
		// 合并同一行内重叠或紧邻的脱敏区间。
		slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.lo, b.lo) })
		merged := []span{spans[0]}
		for _, sp := range spans[1:] {
			last := &merged[len(merged)-1]
			if sp.lo <= last.hi+1 {
				if sp.hi > last.hi {
					last.hi = sp.hi
				}
			} else {
				merged = append(merged, sp)
			}
		}
		for _, sp := range merged {
			f0 := max(0, float64(sp.lo)/float64(totalRunes)-0.015)
			f1 := min(1, float64(sp.hi)/float64(totalRunes)+0.015)
			out = append(out, TextBox{
				Text: bx.Text,
				X:    max(0, bx.X+bx.W*f0),
				Top:  max(0, bx.Top-bx.H*0.08),
				W:    min(1, bx.W*(f1-f0)),
				H:    min(1, bx.H*1.16),
			})
		}
	}
	return out
}

// ExtractLinks 从二维码结果与识别文本中提取去重保序的网页链接或二维码内容。
func ExtractLinks(res Result) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ".,;:!?，。；：！？")
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, b := range res.Barcodes {
		add(b)
	}
	for _, m := range urlPattern.FindAllString(res.Text, -1) {
		add(m)
	}
	return out
}

type textBlock struct {
	text string
	x    float64 // 归一化左边界 [0,1]
	top  float64 // 归一化顶边 [0,1]，越小越靠上
	h    float64 // 归一化高度
}

// layoutBlocks 把 Vision 返回的归一化文本框按自然阅读顺序（从上到下、同行从左到右）拼接成多行文本。
func layoutBlocks(blocks []textBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	slices.SortFunc(blocks, func(a, b textBlock) int {
		return cmp.Compare(a.top, b.top)
	})

	type line struct {
		midY  float64
		avgH  float64
		items []textBlock
	}
	var lines []line
	for _, blk := range blocks {
		t := strings.TrimSpace(blk.text)
		if t == "" {
			continue
		}
		blk.text = t
		cy := blk.top + blk.h*0.5
		placed := false
		for i := range lines {
			tol := max(lines[i].avgH, blk.h) * 0.55
			if abs(cy-lines[i].midY) <= tol {
				n := float64(len(lines[i].items))
				lines[i].midY = (lines[i].midY*n + cy) / (n + 1)
				lines[i].avgH = (lines[i].avgH*n + blk.h) / (n + 1)
				lines[i].items = append(lines[i].items, blk)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, line{midY: cy, avgH: max(0.01, blk.h), items: []textBlock{blk}})
		}
	}

	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		slices.SortFunc(ln.items, func(a, b textBlock) int {
			return cmp.Compare(a.x, b.x)
		})
		var sb strings.Builder
		for i, it := range ln.items {
			if i > 0 && needsSpaceBetween(ln.items[i-1].text, it.text) {
				sb.WriteByte(' ')
			}
			sb.WriteString(it.text)
		}
		out = append(out, sb.String())
	}
	return strings.Join(out, "\n")
}

func needsSpaceBetween(left, right string) bool {
	lr, _ := utf8.DecodeLastRuneInString(left)
	rr, _ := utf8.DecodeRuneInString(right)
	if lr == utf8.RuneError || rr == utf8.RuneError {
		return false
	}
	if isCJK(lr) && isCJK(rr) {
		return false
	}
	return true
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TrimTransparent 裁去主体抠图四周多余的全透明空白边（保留 pad 像素呼吸留白）。
func TrimTransparent(src *image.NRGBA, pad int) *image.NRGBA {
	if src == nil || src.Bounds().Empty() {
		return src
	}
	b := src.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		off := src.PixOffset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x++ {
			if src.Pix[off+3] > 8 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
			off += 4
		}
	}
	if maxX < minX || maxY < minY {
		return src
	}
	if pad < 0 {
		pad = 0
	}
	r := image.Rect(
		max(b.Min.X, minX-pad),
		max(b.Min.Y, minY-pad),
		min(b.Max.X, maxX+1+pad),
		min(b.Max.Y, maxY+1+pad),
	)
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := range r.Dy() {
		sOff := src.PixOffset(r.Min.X, r.Min.Y+y)
		dOff := out.PixOffset(0, y)
		copy(out.Pix[dOff:dOff+r.Dx()*4], src.Pix[sOff:sOff+r.Dx()*4])
	}
	return out
}
