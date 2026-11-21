package session

import (
	"image"
	"testing"

	"snipgo/internal/annotate"
)

// adjustingSession 返回已框选 (10,10)-(100,100) 的实时会话。
func adjustingSession(t *testing.T) *Session {
	t.Helper()
	s := newWindowOnlySession(image.Rect(300, 300, 310, 310))
	s.StartLive()
	s.BeginDrag(0, 10, 10)
	s.MoveDrag(0, 100, 100)
	s.EndDrag()
	if s.Phase() != PhaseAdjusting || s.sel != image.Rect(10, 10, 100, 100) {
		t.Fatalf("前置选区失败 phase=%v sel=%v", s.Phase(), s.sel)
	}
	return s
}

func drawRect(s *Session) {
	s.Annotations().Tool = annotate.ToolRect
	s.BeginDrag(0, 20, 20)
	s.MoveDrag(0, 60, 50)
	s.EndDrag()
	s.Annotations().Tool = annotate.ToolNone
}

func TestCancelDragResizeKeepsSelectionAndAnnotations(t *testing.T) {
	s := adjustingSession(t)
	drawRect(s)
	if len(s.Annotations().Items()) != 1 {
		t.Fatal("前置标注失败")
	}
	s.BeginDrag(0, 100, 100) // SE 控制柄
	if !s.Dragging() {
		t.Fatal("应进入缩放拖拽")
	}
	s.MoveDrag(0, 140, 130)
	if s.sel == image.Rect(10, 10, 100, 100) {
		t.Fatal("拖拽中选区应已变化")
	}
	s.CancelDrag()
	if s.Phase() != PhaseAdjusting || s.sel != image.Rect(10, 10, 100, 100) || s.Dragging() {
		t.Fatalf("取消后应还原选区: phase=%v sel=%v", s.Phase(), s.sel)
	}
	if len(s.Annotations().Items()) != 1 {
		t.Fatal("取消拖拽不应丢标注")
	}
}

func TestCancelDragAnnotateDropsOnlyPendingItem(t *testing.T) {
	s := adjustingSession(t)
	drawRect(s)
	s.Annotations().Tool = annotate.ToolRect
	s.BeginDrag(0, 30, 60)
	s.MoveDrag(0, 80, 90)
	s.CancelDrag()
	if n := len(s.Annotations().Items()); n != 1 {
		t.Fatalf("应只保留已提交的 1 个图元, got %d", n)
	}
	if s.Annotations().Drawing() != nil || s.Phase() != PhaseAdjusting {
		t.Fatalf("临时图元应清除且仍在 Adjusting, phase=%v", s.Phase())
	}
}

func TestCancelDragEditRestoresItem(t *testing.T) {
	s := adjustingSession(t)
	drawRect(s)
	orig := s.Annotations().Items()[0]
	s.BeginDrag(0, 20, 20) // 命中图元起点
	if !s.AnnotatingDrag() {
		t.Fatal("应进入标注编辑拖拽")
	}
	s.MoveDrag(0, 35, 40)
	if got := s.Annotations().Items()[0]; got.P0 == orig.P0 && got.P1 == orig.P1 {
		t.Fatal("拖拽中图元应已变化")
	}
	s.CancelDrag()
	got := s.Annotations().Items()[0]
	if got.P0 != orig.P0 || got.P1 != orig.P1 || s.Annotations().Editing() {
		t.Fatalf("取消后应还原图元: %+v vs %+v", got, orig)
	}
}

func TestCancelDragRedrawRestoresPreviousSelection(t *testing.T) {
	s := adjustingSession(t)
	drawRect(s)
	s.BeginDrag(0, 150, 120) // 选区外按下
	s.MoveDrag(0, 190, 145)  // 越过阈值，旧选区被清空
	if s.Phase() != PhaseSelecting {
		t.Fatalf("前置：应已转为重新框选 phase=%v", s.Phase())
	}
	s.CancelDrag()
	if s.Phase() != PhaseAdjusting || s.sel != image.Rect(10, 10, 100, 100) || len(s.Annotations().Items()) != 1 {
		t.Fatalf("应找回旧选区与标注: phase=%v sel=%v items=%d", s.Phase(), s.sel, len(s.Annotations().Items()))
	}
}

func TestCancelDragNewBoxStaysSelecting(t *testing.T) {
	s := newWindowOnlySession(image.Rect(300, 300, 310, 310))
	s.StartLive()
	s.BeginDrag(0, 10, 10)
	s.MoveDrag(0, 100, 100)
	s.CancelDrag()
	if s.Phase() != PhaseSelecting || !s.sel.Empty() || s.Dragging() {
		t.Fatalf("新框选取消后应停留在 Selecting 且无选区: phase=%v sel=%v", s.Phase(), s.sel)
	}
}
