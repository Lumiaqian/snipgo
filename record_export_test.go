package main

import (
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"snipgo/internal/videocap"
)

// The native-media fixture is the pinned MyGo module's red/green/blue 10 fps movie.
func TestNativeTrimAndGIF(t *testing.T) {
	src := os.Getenv("SNIPGO_RGB_FIXTURE")
	if src == "" {
		t.Skip("set SNIPGO_RGB_FIXTURE to run the native media test")
	}
	studio, e := videocap.NewStudio()
	if e != nil {
		t.Fatal(e)
	}
	defer studio.Close()
	o := exportOptions{source: src, start: 0.4, end: 0.8, width: 32, fps: 10, mute: true, destination: filepath.Join(t.TempDir(), "clip.mp4")}
	progress := func(int) {}
	if e := exportMovie(t.Context(), studio, o, progress); e != nil {
		t.Fatal(e)
	}
	o.source = o.destination
	o.destination = filepath.Join(t.TempDir(), "clip.gif")
	o.start = 0
	o.end = 0.4
	o.gif = true
	if e := exportMovie(t.Context(), studio, o, progress); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(o.destination)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	g, e := gif.DecodeAll(f)
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Image) != 4 || g.Config.Width != 32 || g.Config.Height != 24 {
		t.Fatalf("frames=%d size=%dx%d", len(g.Image), g.Config.Width, g.Config.Height)
	}
	r, green, b, _ := g.Image[0].At(16, 12).RGBA()
	if green <= r*2 || green <= b*2 {
		t.Fatalf("trim should begin on green, got %d %d %d", r, green, b)
	}
}
