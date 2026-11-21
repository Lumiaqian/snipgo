package session

import (
	"context"
	"image"
	"testing"

	"snipgo/internal/crop"
)

// newWindowOnlySession 构造带单个窗口目标的实时会话，前台窗口即该窗口。
func newWindowOnlySession(winRect image.Rectangle) *Session {
	return &Session{
		Live: func() (Snapshot, error) {
			return Snapshot{
				Frames:      []crop.Screen{{Bounds: image.Rect(0, 0, 200, 150), Scale: 2}},
				FrontWindow: winRect,
				FrontWinID:  4242,
				Windows:     []image.Rectangle{winRect},
				WindowInfos: []WinTarget{{ID: 4242, Bounds: winRect}},
			}, nil
		},
		CaptureRect: func(_ context.Context, sel image.Rectangle) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, sel.Dx()*2, sel.Dy()*2)), nil
		},
		CaptureWindow: func(_ context.Context, _ uint32, scale float64) (*image.NRGBA, error) {
			return image.NewNRGBA(image.Rect(0, 0, int(scale*10), int(scale*10))), nil
		},
		Clip:   &fakeClip{},
		UI:     &fakeUI{},
		OnMain: func(fn func()) { fn() },
	}
}

func TestWindowOnlyUnavailableForPartlyOffscreenWindow(t *testing.T) {
	cases := []struct {
		name string
		win  image.Rectangle
		want bool
	}{
		{"完全在屏内", image.Rect(10, 10, 90, 70), true},
		{"右侧伸出屏外", image.Rect(150, 10, 260, 70), false},
		{"左上伸出屏外", image.Rect(-40, -20, 90, 70), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newWindowOnlySession(c.win)
			s.StartLive()
			s.InitCursor(float64(c.win.Min.X+20), 40)
			// 前台窗口默认被悬停；单击锁定。
			s.BeginDrag(0, float64(c.win.Min.X+20), 40)
			s.EndDrag()
			if s.Phase() != PhaseAdjusting {
				t.Fatalf("phase=%v, 期望 Adjusting", s.Phase())
			}
			if got := s.CanWindowOnly(); got != c.want {
				t.Fatalf("CanWindowOnly=%v, 期望 %v (sel=%v)", got, c.want, s.sel)
			}
		})
	}
}
