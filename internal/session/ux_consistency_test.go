package session

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"testing"

	"snipgo/internal/crop"
)

func TestSaveAsCallbackAndFallback(t *testing.T) {
	ui := &fakeUI{}
	snap := Snapshot{Frames: []crop.Screen{redScreen()}}
	var saveAsCalls [][]byte
	var s *Session
	s = &Session{
		UI:     ui,
		OnMain: func(fn func()) { fn() },
		Background: func(fn func()) {
			fn()
		},
		Launch: func(gen int) { s.Deliver(gen, snap, nil) },
		OnSaveAs: func(b []byte) {
			saveAsCalls = append(saveAsCalls, append([]byte(nil), b...))
		},
	}

	s.StartFrozen()
	s.BeginDrag(0, 1, 1)
	s.MoveDrag(0, 12, 12)
	s.EndDrag()
	s.SaveAs()

	if len(saveAsCalls) != 1 || len(ui.saved) != 0 {
		t.Fatalf("OnSaveAs=%d ui.saved=%d, want 1 and 0", len(saveAsCalls), len(ui.saved))
	}

	// Default Save keeps calling UI.SavePNG.
	var s2 *Session
	s2 = &Session{
		UI:         ui,
		OnMain:     func(fn func()) { fn() },
		Background: func(fn func()) { fn() },
		Launch:     func(gen int) { s2.Deliver(gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil) },
		OnSaveAs: func(b []byte) {
			t.Fatalf("Save() should not call OnSaveAs")
		},
	}
	s2.StartFrozen()
	s2.BeginDrag(0, 1, 1)
	s2.MoveDrag(0, 12, 12)
	s2.EndDrag()
	s2.Save()
	if len(ui.saved) != 1 {
		t.Fatalf("Save() ui.saved=%d, want 1", len(ui.saved))
	}

	// When OnSaveAs is nil, SaveAs falls back to UI.SavePNG.
	var s3 *Session
	s3 = &Session{
		UI:         ui,
		OnMain:     func(fn func()) { fn() },
		Background: func(fn func()) { fn() },
		Launch:     func(gen int) { s3.Deliver(gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil) },
	}
	s3.StartFrozen()
	s3.BeginDrag(0, 1, 1)
	s3.MoveDrag(0, 12, 12)
	s3.EndDrag()
	s3.SaveAs()
	if len(ui.saved) != 2 || !bytes.Equal(ui.saved[1], saveAsCalls[0]) {
		t.Fatalf("SaveAs fallback ui.saved=%d", len(ui.saved))
	}
}

func TestSwitchLiveToFrozenForMeasureAndRulerCallback(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	liveSnap := Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 20, 20), Scale: 2}}}
	var rulerCopied []string

	var s *Session
	s = &Session{
		Clip:   clip,
		UI:     ui,
		OnMain: func(fn func()) { fn() },
		Live:   func() (Snapshot, error) { return liveSnap, nil },
		CaptureRect: func(context.Context, image.Rectangle) (*image.NRGBA, error) {
			img := image.NewNRGBA(image.Rect(0, 0, 3, 3))
			img.SetNRGBA(1, 1, color.NRGBA{R: 14, G: 165, B: 233, A: 255})
			return img, nil
		},
		Launch: func(gen int) { s.Deliver(gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil) },
		OnRulerCopied: func(txt string) {
			rulerCopied = append(rulerCopied, txt)
		},
	}

	s.StartLive()
	s.Hover(5, 5)
	added := make(chan bool, 1)
	if !s.SampleColorToPaletteEx(func(_ color.NRGBA, ok bool) { added <- ok }) {
		t.Fatal("SampleColorToPaletteEx failed")
	}
	if !<-added {
		t.Fatal("expected color added")
	}
	if !s.SwitchLiveToFrozenForMeasure() {
		t.Fatal("SwitchLiveToFrozenForMeasure returned false")
	}
	if !s.MeasureMode() || s.ColorPickerMode() {
		t.Fatalf("post-switch state: measure=%v picker=%v, want true/false", s.MeasureMode(), s.ColorPickerMode())
	}
	if len(s.PickedColors()) != 1 {
		t.Fatalf("expected picked colors preserved across switch, got %d", len(s.PickedColors()))
	}
	if s.SwitchLiveToFrozenForMeasure() {
		t.Fatal("expected SwitchLiveToFrozenForMeasure to return false when already frozen")
	}

	// Clicking in MeasureMode copies W × H px and invokes OnRulerCopied.
	s.BeginDrag(0, 10, 10)
	if clip.text == "" || len(rulerCopied) != 1 || rulerCopied[0] != clip.text {
		t.Fatalf("ruler copy = clip:%q cb:%v", clip.text, rulerCopied)
	}
}
