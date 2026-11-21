package vision

import (
	"math"
	"testing"
)

func TestSensitiveBoxesFallsBackToWholeLine(t *testing.T) {
	line := TextBox{Text: "llllllllll 13812345678", X: 0.1, Top: 0.3, W: 0.6, H: 0.05}
	got := SensitiveBoxes(Result{Boxes: []TextBox{line}})
	if len(got) != 1 {
		t.Fatalf("boxes = %d, want 1", len(got))
	}
	b := got[0]
	// 无精确几何：必须覆盖整行宽度，而不是按字符比例估算的起点（约 0.1+0.6*0.45）。
	if b.X > line.X+1e-9 || b.X+b.W < line.X+line.W-1e-9 {
		t.Fatalf("box [%.3f,%.3f] does not cover whole line [%.3f,%.3f]", b.X, b.X+b.W, line.X, line.X+line.W)
	}
	if b.Top > line.Top || b.Top+b.H < line.Top+line.H {
		t.Fatalf("box does not cover line height: %+v", b)
	}
}

func TestSensitiveBoxesUsesPreciseRects(t *testing.T) {
	line := TextBox{
		Text: "llllllllll 13812345678", X: 0.1, Top: 0.3, W: 0.6, H: 0.05,
		Sensitive: []TextBox{{X: 0.52, Top: 0.31, W: 0.15, H: 0.03}},
	}
	got := SensitiveBoxes(Result{Boxes: []TextBox{line}})
	if len(got) != 1 {
		t.Fatalf("boxes = %d, want 1", len(got))
	}
	dx, dy := line.W*0.02, line.H*0.08
	want := TextBox{X: 0.52 - dx, Top: 0.31 - dy, W: 0.15 + 2*dx, H: 0.03 + 2*dy}
	b := got[0]
	for _, p := range [][2]float64{{b.X, want.X}, {b.Top, want.Top}, {b.W, want.W}, {b.H, want.H}} {
		if math.Abs(p[0]-p[1]) > 1e-9 {
			t.Fatalf("got %+v, want %+v", b, want)
		}
	}
	if b.X < 0.4 {
		t.Fatalf("precise box must not expand to whole line: %+v", b)
	}
}

func TestSensitiveBoxesSkipsPlainText(t *testing.T) {
	got := SensitiveBoxes(Result{Boxes: []TextBox{{Text: "hello world", X: 0, Top: 0, W: 1, H: 0.1}}})
	if len(got) != 0 {
		t.Fatalf("plain text produced boxes: %+v", got)
	}
}

func TestSensitiveSpansMergesAdjacent(t *testing.T) {
	spans := SensitiveSpans("token=abc a@b.cc")
	if len(spans) != 1 || spans[0] != [2]int{6, 16} {
		t.Fatalf("spans = %v, want [[6 16]]", spans)
	}
}
