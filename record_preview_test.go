package main

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingPreviewSnapshots(t *testing.T) {
	dir := os.Getenv("SNIPGO_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set SNIPGO_PREVIEW_DIR to capture recording views")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Use a decoded fixture frame so the actual video widget appears in offscreen renders.
	frame := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { paintTahoeWallpaper(p, r, false) })
	}, 480, 240, 1)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, frame, nil); err != nil {
		t.Fatal(err)
	}
	dec, err := ui.NewGIFDecoder(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	player, err := ui.NewVideoPlayer(dec)
	if err != nil {
		t.Fatal(err)
	}
	defer player.Close()
	player.Pause()
	a := &app{}
	for _, dark := range []bool{false, true} {
		save := func(name string, w, h int, view func(*ui.Context)) {
			tt := ui.NewTester(view, w, h)
			tt.SetDark(dark)
			tt.Advance(time.Second)
			tt.Frame()
			suffix := "-light.png"
			if dark {
				suffix = "-dark.png"
			}
			savePNGFile(t, filepath.Join(dir, "record-"+name+suffix), tt.Image())
		}
		cd := &recordCountdown{deadline: time.Now().Add(3 * time.Second), fromFrozen: true}
		save("countdown", 364, 42, func(c *ui.Context) { paintRecordCountdown(c, cd) })
		r := &regionRecording{started: time.Now().Add(-8 * time.Second)}
		controls := func(c *ui.Context) { a.paintRecording(c, r) }
		save("silent", 360, 44, controls)
		r.mic = true
		r.system = true
		r.level = .2
		save("audio", 360, 44, controls)
		r.mic = false
		r.micDenied = true
		r.pausedAt = time.Now()
		r.confirmUntil = time.Now().Add(3 * time.Second)
		save("paused-denied", 360, 44, controls)
		s := &recordStudio{source: "fixture.mp4", output: "fixture.mp4", width: 1, rate: 1, duration: 8.4, end: 8.4}
		studio := func(c *ui.Context) { a.paintRecordStudio(c, s) }
		save("loading", 780, 580, studio)
		s.player = player
		save("mp4", 780, 580, studio)
		s.start = 1.2
		s.format = 1
		s.dirty()
		save("gif-700", 700, 520, studio)
		s.cancel = func() {}
		s.progress = 64
		save("encoding", 700, 520, studio)
	}
}
