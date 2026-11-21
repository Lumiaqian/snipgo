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
	// Sensitive 是该行内敏感子串由识别引擎给出的精确归一化矩形（坐标系同上）。
	// 仅当该行所有敏感片段都拿到了几何时才填充；为空表示没有精确几何，
	// SensitiveBoxes 会退化为整行脱敏。
	Sensitive []TextBox
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

// SensitiveSpans 返回文本中需要脱敏的字节区间 [lo,hi)（已排序并合并重叠或紧邻的区间）。
func SensitiveSpans(text string) [][2]int {
	var spans [][2]int
	for _, re := range sensitivePatterns {
		for _, idx := range re.FindAllStringIndex(text, -1) {
			if idx[1] > idx[0] {
				spans = append(spans, [2]int{idx[0], idx[1]})
			}
		}
	}
	for _, sub := range kvSecretPattern.FindAllStringSubmatchIndex(text, -1) {
		if len(sub) >= 4 && sub[2] >= 0 && sub[3] > sub[2] {
			spans = append(spans, [2]int{sub[2], sub[3]})
		}
	}
	if len(spans) == 0 {
		return nil
	}
	slices.SortFunc(spans, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	merged := [][2]int{spans[0]}
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp[0] <= last[1]+1 {
			if sp[1] > last[1] {
				last[1] = sp[1]
			}
		} else {
			merged = append(merged, sp)
		}
	}
	return merged
}

// SensitiveBoxes 自动扫描识别结果中的敏感凭证/隐私字段（API Key、Token、邮箱、手机号、身份证、IP），
// 返回需脱敏的归一化矩形 [0,1]。
// 优先使用识别引擎给出的精确子串矩形（TextBox.Sensitive，四周留少量余量）；
// 没有精确几何时不再按字符数比例估算（窄字符会让框整体偏移而漏掉内容），而是隐私优先地整行脱敏。
func SensitiveBoxes(res Result) []TextBox {
	var out []TextBox
	for _, bx := range res.Boxes {
		if bx.Text == "" || bx.W <= 0 || bx.H <= 0 {
			continue
		}
		if len(SensitiveSpans(bx.Text)) == 0 {
			continue
		}
		if len(bx.Sensitive) > 0 {
			for _, sb := range bx.Sensitive {
				if sb.W <= 0 || sb.H <= 0 {
					continue
				}
				out = append(out, padBox(bx.Text, sb, bx.W*0.02, bx.H*0.08))
			}
			continue
		}
		out = append(out, padBox(bx.Text, bx, bx.W*0.01, bx.H*0.08))
	}
	return out
}

// padBox 向四周外扩 (dx,dy) 并夹紧到 [0,1]。
func padBox(text string, b TextBox, dx, dy float64) TextBox {
	x0 := max(0, b.X-dx)
	y0 := max(0, b.Top-dy)
	x1 := min(1, b.X+b.W+dx)
	y1 := min(1, b.Top+b.H+dy)
	return TextBox{Text: text, X: x0, Top: y0, W: x1 - x0, H: y1 - y0}
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
