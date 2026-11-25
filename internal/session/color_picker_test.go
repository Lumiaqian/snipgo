package session

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"snipgo/internal/beautify"
	"snipgo/internal/crop"
)

func TestColorPickerModeCopyAndExit(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	var copiedText string
	var copiedColor color.NRGBA
	var s *Session
	s = &Session{
		Clip:   clip,
		UI:     ui,
		OnMain: func(fn func()) { fn() },
		Launch: func(gen int) { s.Deliver(gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil) },
		OnColorCopied: func(text string, col color.NRGBA) {
			copiedText = text
			copiedColor = col
		},
	}

	s.StartColorPicker()
	if s.Phase() != PhaseSelecting || !s.ColorPickerMode() {
		t.Fatalf("StartColorPicker state = (%v, picker=%v), want (PhaseSelecting, true)", s.Phase(), s.ColorPickerMode())
	}

	// Pixel (1,1) DIP maps to (2,2) px in redScreen (scale 2), which is G=255.
	s.Hover(1, 1)
	s.BeginDrag(0, 1, 1)

	want := color.NRGBA{G: 255, A: 255}
	if clip.text != "#00FF00" || copiedText != "#00FF00" || copiedColor != want {
		t.Fatalf("left click copy = clip:%q cb:%q col:%+v", clip.text, copiedText, copiedColor)
	}
	if s.Phase() != PhaseIdle || s.ColorPickerMode() {
		t.Fatalf("expected exit after color picker click, got (%v, picker=%v)", s.Phase(), s.ColorPickerMode())
	}

	// Subsequent normal frozen capture resets ColorPickerMode to false.
	s.StartFrozen()
	if s.ColorPickerMode() {
		t.Fatal("StartFrozen should reset ColorPickerMode")
	}
}

func TestCopyColorFormatsAndRawModeSyncAndAsync(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}

	var notified []beautify.ColorFormat
	var s *Session
	s = &Session{
		Clip:   clip,
		UI:     ui,
		OnMain: func(fn func()) { fn() },
		Launch: func(gen int) { s.Deliver(gen, Snapshot{Frames: []crop.Screen{redScreen()}}, nil) },
		OnColorFormatChanged: func(f beautify.ColorFormat) {
			notified = append(notified, f)
		},
	}

	// Check raw output across all 4 formats in frozen mode.
	wantRaw := map[beautify.ColorFormat]string{
		beautify.ColorHEX:      "00FF00",
		beautify.ColorRGB:      "0, 255, 0",
		beautify.ColorTailwind: "green-500",
	}
	for _, f := range []beautify.ColorFormat{beautify.ColorHEX, beautify.ColorRGB, beautify.ColorOKLCH, beautify.ColorTailwind} {
		s.StartFrozen()
		s.SelectColorFormat(f)
		s.Hover(1, 1)
		if !s.CopyColor(true) {
			t.Fatalf("CopyColor(true) failed for %v", f)
		}
		if f == beautify.ColorOKLCH {
			if strings.HasPrefix(clip.text, "oklch(") || strings.HasSuffix(clip.text, ")") || len(clip.text) < 5 {
				t.Fatalf("unexpected raw OKLCH: %q", clip.text)
			}
		} else if clip.text != wantRaw[f] && f != beautify.ColorTailwind {
			t.Fatalf("raw %v = %q, want %q", f, clip.text, wantRaw[f])
		} else if f == beautify.ColorTailwind && clip.text != beautify.FormatColorRaw(color.NRGBA{G: 255, A: 255}, beautify.ColorTailwind) {
			t.Fatalf("raw tailwind = %q", clip.text)
		}
	}
	if len(notified) != 4 {
		t.Fatalf("OnColorFormatChanged count = %d, want 4", len(notified))
	}

	// Live async Shift+C raw copy triggers OnColorCopied.
	done := make(chan struct{})
	liveSnap := Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 20, 20), Scale: 2}}}
	s2 := &Session{
		Clip:   clip,
		UI:     ui,
		OnMain: func(fn func()) { fn() },
		Live:   func() (Snapshot, error) { return liveSnap, nil },
		CaptureRect: func(context.Context, image.Rectangle) (*image.NRGBA, error) {
			img := image.NewNRGBA(image.Rect(0, 0, 3, 3))
			img.SetNRGBA(1, 1, color.NRGBA{R: 14, G: 165, B: 233, A: 255})
			return img, nil
		},
		OnColorCopied: func(text string, col color.NRGBA) {
			if text != "0EA5E9" || col != (color.NRGBA{R: 14, G: 165, B: 233, A: 255}) {
				t.Errorf("async callback = (%q, %+v)", text, col)
			}
			close(done)
		},
	}
	s2.SetColorFormat(beautify.ColorHEX)
	s2.StartLive()
	s2.Hover(5, 5)
	if !s2.CopyColor(true) {
		t.Fatal("async CopyColor(true) returned false")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async OnColorCopied")
	}
}

func TestPaletteDeduplicationEditAndSwitchLiveToFrozen(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	liveSnap := Snapshot{Frames: []crop.Screen{{Bounds: image.Rect(0, 0, 20, 20), Scale: 2}}}
	frozenSnap := Snapshot{Frames: []crop.Screen{redScreen()}}

	var pinnedColors []color.NRGBA
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
		Launch: func(gen int) { s.Deliver(gen, frozenSnap, nil) },
		OnPalettePinned: func(colors []color.NRGBA, img *image.NRGBA, png []byte, at image.Point, dipW, dipH int) {
			pinnedColors = append([]color.NRGBA(nil), colors...)
		},
	}

	s.StartLive()
	s.SetColorFormat(beautify.ColorOKLCH)
	s.Hover(5, 5)

	addedDone := make(chan bool, 2)
	if !s.SampleColorToPaletteEx(func(_ color.NRGBA, added bool) { addedDone <- added }) {
		t.Fatal("live SampleColorToPaletteEx failed")
	}
	if !<-addedDone {
		t.Fatal("expected first sample added=true")
	}
	// Duplicate sample should trigger added=false and keep length at 1.
	if !s.SampleColorToPaletteEx(func(_ color.NRGBA, added bool) { addedDone <- added }) {
		t.Fatal("second SampleColorToPaletteEx failed")
	}
	if <-addedDone {
		t.Fatal("expected duplicate sample added=false")
	}
	if len(s.PickedColors()) != 1 {
		t.Fatalf("picked count after dedup = %d, want 1", len(s.PickedColors()))
	}

	// Switch live -> frozen keeping picked colors, format, and cursor.
	shownBefore := ui.shown
	hiddenBefore := ui.hidden
	if !s.SwitchLiveToFrozenForColor() {
		t.Fatal("SwitchLiveToFrozenForColor returned false")
	}
	if s.Phase() != PhaseSelecting || !s.ColorPickerMode() {
		t.Fatalf("post-switch phase=%v picker=%v", s.Phase(), s.ColorPickerMode())
	}
	if ui.hidden != hiddenBefore+1 || ui.shown != shownBefore+1 {
		t.Fatalf("expected hide+show transition, got hidden=%d shown=%d", ui.hidden-hiddenBefore, ui.shown-shownBefore)
	}
	if s.ColorFormat() != beautify.ColorOKLCH || len(s.PickedColors()) != 1 {
		t.Fatalf("lost state after switch: fmt=%v picked=%v", s.ColorFormat(), s.PickedColors())
	}
	if s.SwitchLiveToFrozenForColor() {
		t.Fatal("expected SwitchLiveToFrozenForColor to return false when already frozen")
	}

	// Move to (1,1) (G=255) and add second color, then test remove & pop & pin.
	s.Hover(1, 1)
	if !s.SampleColorToPaletteEx(func(_ color.NRGBA, added bool) {
		if !added {
			t.Fatal("expected second distinct frozen color to add")
		}
	}) {
		t.Fatal("frozen sample failed")
	}
	if len(s.PickedColors()) != 2 {
		t.Fatalf("picked len = %d, want 2", len(s.PickedColors()))
	}

	// Add third color (0,0 -> R=200) and test RemovePickedColor & PopPickedColor.
	s.Hover(0, 0)
	_ = s.SampleColorToPaletteEx(nil)
	if len(s.PickedColors()) != 3 {
		t.Fatalf("picked len = %d, want 3", len(s.PickedColors()))
	}
	if !s.RemovePickedColor(0) || len(s.PickedColors()) != 2 {
		t.Fatalf("RemovePickedColor(0) len = %d, want 2", len(s.PickedColors()))
	}
	if !s.PopPickedColor() || len(s.PickedColors()) != 1 {
		t.Fatalf("PopPickedColor len = %d, want 1", len(s.PickedColors()))
	}

	// Pin remaining palette and confirm OnPalettePinned fired instead of UI.ShowPin.
	if !s.PinPickedPalette() {
		t.Fatal("PinPickedPalette failed")
	}
	if len(pinnedColors) != 1 || ui.pins != 0 {
		t.Fatalf("pinnedColors=%v ui.pins=%d", pinnedColors, ui.pins)
	}
}
