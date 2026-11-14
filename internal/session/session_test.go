package session

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"snipgo/internal/crop"
)

type fakeUI struct {
	status string
	shown  int
	hidden int
	pins   int
	at     image.Point
	dip    image.Point
}

func (f *fakeUI) SetStatus(text string)       { f.status = text }
func (f *fakeUI) ShowSelection([]crop.Screen) { f.shown++ }
func (f *fakeUI) HideSelection()              { f.hidden++ }
func (f *fakeUI) ShowPin(_ *image.NRGBA, at image.Point, dipW, dipH int) {
	f.pins++
	f.at = at
	f.dip = image.Pt(dipW, dipH)
}

type fakeClip struct {
	data []byte
	err  error
}

func (f *fakeClip) WritePNG(b []byte) error {
	if f.err != nil {
		return f.err
	}
	f.data = append([]byte(nil), b...)
	return nil
}

func redScreen() crop.Screen {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	img.SetNRGBA(2, 2, color.NRGBA{G: 255, A: 255})
	return crop.Screen{Bounds: image.Rect(0, 0, 10, 10), Scale: 2, Pixels: img}
}

func TestHotkeyDeniedDoesNotCapture(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	launches := 0
	s := &Session{
		Allowed: func() (bool, string) { return false, "需要屏幕录制权限" },
		Clip:    clip,
		UI:      ui,
		Launch:  func(int) { launches++ },
	}
	s.Hotkey()
	if launches != 0 || clip.data != nil || ui.shown != 0 {
		t.Fatalf("launches %d clip %d shown %d", launches, len(clip.data), ui.shown)
	}
	if ui.status != "需要屏幕录制权限" || s.Status() != ui.status {
		t.Fatalf("status %q", ui.status)
	}
}

func TestHotkeyCapturesAndSelects(t *testing.T) {
	ui := &fakeUI{}
	frames := []crop.Screen{redScreen()}
	calls := 0
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		Capture: func(context.Context) ([]crop.Screen, error) {
			calls++
			return frames, nil
		},
		Clip:   &fakeClip{},
		UI:     ui,
		OnMain: func(fn func()) { fn() },
	}
	done := make(chan struct{})
	s.OnMain = func(fn func()) {
		fn()
		close(done)
	}
	s.Hotkey()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture did not return")
	}
	if calls != 1 || ui.shown != 1 {
		t.Fatalf("calls %d shown %d", calls, ui.shown)
	}
}

func TestStaleCaptureIsDropped(t *testing.T) {
	ui := &fakeUI{}
	var gens []int
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      ui,
		Clip:    &fakeClip{},
		Launch:  func(gen int) { gens = append(gens, gen) },
	}
	s.Hotkey()
	s.Deliver(gens[0], []crop.Screen{redScreen()}, nil)
	if ui.shown != 1 {
		t.Fatal("first capture")
	}
	s.Hotkey()
	if ui.hidden != 1 {
		t.Fatal("second hotkey should close the selection")
	}
	s.Deliver(gens[0], []crop.Screen{redScreen()}, nil)
	if ui.shown != 1 {
		t.Fatalf("stale capture shown again: %d", ui.shown)
	}
	s.Deliver(gens[1], []crop.Screen{redScreen()}, nil)
	if ui.shown != 2 {
		t.Fatalf("fresh capture shown %d", ui.shown)
	}
}

func TestCancelWritesNothing(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	frames := []crop.Screen{redScreen()}
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      ui,
		Clip:    clip,
		Launch:  func(int) {},
	}
	s.Hotkey()
	s.Deliver(s.gen, frames, nil)
	s.BeginDrag(0, 1, 1)
	s.Cancel()
	s.EndDrag()
	if clip.data != nil || ui.pins != 0 {
		t.Fatalf("clip %d pins %d", len(clip.data), ui.pins)
	}
	if frames[0].Pixels != nil {
		t.Fatal("frozen frame still held after cancel")
	}
}

func TestSmallDragCancels(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	s := ready(ui, clip)
	s.BeginDrag(0, 4, 4)
	s.MoveDrag(0, 4.4, 5)
	s.EndDrag()
	if clip.data != nil || ui.pins != 0 {
		t.Fatal("small selection should cancel")
	}
}

func TestConfirmCopiesAndPins(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{}
	frames := readyFrames()
	s := ready(ui, clip)
	s.frames = frames
	s.phase = phaseSelecting
	s.BeginDrag(0, 3.2, 4)
	s.MoveDrag(0, 1, 1) // 反向拖动，松开后规范化成 (1,1)-(3,4)
	s.EndDrag()
	if ui.pins != 1 || ui.at != image.Pt(1, 1) || ui.dip != image.Pt(2, 3) {
		t.Fatalf("pin at %v dip %v pins %d", ui.at, ui.dip, ui.pins)
	}
	if frames[0].Pixels != nil {
		t.Fatal("frozen frame still held after confirm")
	}
	got, err := png.Decode(bytes.NewReader(clip.data))
	if err != nil {
		t.Fatal(err)
	}
	// 2×3 DIP，2 倍缩放，4×6 像素。起点 DIP (1,1) 是冻结图里标出的绿点。
	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 6 {
		t.Fatalf("png %v, want 4x6", got.Bounds().Size())
	}
	if c := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA); c != (color.NRGBA{G: 255, A: 255}) {
		t.Fatalf("png origin %v, want the marked pixel", c)
	}
}

func TestClipboardErrorStillPins(t *testing.T) {
	ui := &fakeUI{}
	clip := &fakeClip{err: errCopy}
	s := ready(ui, clip)
	s.BeginDrag(0, 0, 0)
	s.MoveDrag(0, 4, 4)
	s.EndDrag()
	if ui.pins != 1 || ui.status == "" {
		t.Fatalf("pins %d status %q", ui.pins, ui.status)
	}
}

var errCopy = errString("copy failed")

type errString string

func (e errString) Error() string { return string(e) }

func ready(ui *fakeUI, clip *fakeClip) *Session {
	s := &Session{
		Allowed: func() (bool, string) { return true, "" },
		UI:      ui,
		Clip:    clip,
		Launch:  func(int) {},
	}
	s.Hotkey()
	s.Deliver(s.gen, readyFrames(), nil)
	return s
}

func readyFrames() []crop.Screen {
	return []crop.Screen{redScreen()}
}
