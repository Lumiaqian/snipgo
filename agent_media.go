package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"snipgo/internal/agentipc"
	"snipgo/internal/capture"
	"snipgo/internal/recordsess"
	"snipgo/internal/videocap"
)

func agentError(code, msg string) agentipc.Response {
	return agentipc.Response{ErrorCode: code, Message: msg}
}
func mediaError(err error) agentipc.Response {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return agentError("CANCELED", "request canceled")
	case errors.Is(err, capture.ErrPermissionDenied):
		return agentError("PERMISSION_DENIED", err.Error())
	default:
		return agentError("PROCESS_FAILED", err.Error())
	}
}
func selectionForAgent(p agentParams, displays []capture.Display) (image.Rectangle, agentipc.Response) {
	count := 0
	for _, present := range []bool{p.Window != 0, p.Display != 0, p.Rect != ""} {
		if present {
			count++
		}
	}
	if count != 1 {
		return image.Rectangle{}, agentError("INVALID_ARGS", "select one window, rectangle or display")
	}
	if p.Window != 0 {
		_, wins := capture.VisibleWindowInfos(image.Point{}, displays, overlayTitle)
		for _, w := range wins {
			if w.ID == p.Window {
				return w.Bounds, agentipc.Response{}
			}
		}
		return image.Rectangle{}, agentError("NOT_FOUND", "window not found")
	}
	if p.Display != 0 {
		for _, d := range displays {
			if p.Display == d.ID {
				return d.Bounds, agentipc.Response{}
			}
		}
		return image.Rectangle{}, agentError("NOT_FOUND", "display not found")
	}
	parts := strings.Split(p.Rect, ",")
	var v [4]int
	if len(parts) == 4 {
		for i, s := range parts {
			n, e := strconv.ParseInt(s, 10, 32)
			if e != nil {
				return image.Rectangle{}, agentError("INVALID_ARGS", "invalid rectangle")
			}
			v[i] = int(n)
		}
		if v[2] > 0 && v[3] > 0 && v[2] <= 32768 && v[3] <= 32768 && v[2]*v[3] <= 40000000 {
			return image.Rect(v[0], v[1], v[0]+v[2], v[1]+v[3]), agentipc.Response{}
		}
	}
	return image.Rectangle{}, agentError("INVALID_ARGS", "invalid rectangle")
}

// Called on the UI thread. The visible indicator is installed before starting capture.
func (a *app) beginAgentMedia(ctx context.Context, method string, p agentParams, done chan<- agentipc.Response) {
	if method == "record" && (p.Duration < 1 || p.Duration > 60 || p.Display != 0 || p.Extension != ".gif" && p.Extension != ".mp4" || p.Width < 1 || p.Width > 4096 || p.FPS < 1 || p.FPS > 60) {
		done <- agentError("INVALID_ARGS", "invalid recording options")
		return
	}
	displays, err := a.currentDisplays()
	if err != nil {
		done <- mediaError(err)
		return
	}
	rect, failure := selectionForAgent(p, displays)
	if failure.ErrorCode != "" {
		done <- failure
		return
	}
	if !capture.HasPermission() {
		done <- agentError("PERMISSION_DENIED", "SnipGo needs screen recording permission")
		return
	}
	if method == "capture" {
		a.showActionToast("Agent 正在截图")
		go func() {
			var img *image.NRGBA
			var err error
			if p.Window != 0 {
				scale := 1.0
				for _, d := range displays {
					if rect.Overlaps(d.Bounds) {
						scale = d.Scale
						break
					}
				}
				img, err = capture.GrabWindow(ctx, p.Window, scale)
			} else {
				img, err = capture.GrabRect(ctx, displays, rect, overlayTitle)
			}
			if err != nil {
				done <- mediaError(err)
				return
			}
			var buf bytes.Buffer
			if err = png.Encode(&buf, img); err != nil {
				done <- mediaError(err)
				return
			}
			done <- agentipc.Response{OK: true, Data: map[string]any{"media": buf.Bytes(), "width": img.Rect.Dx(), "height": img.Rect.Dy()}}
		}()
		return
	}
	if a.recording != nil {
		done <- agentError("BUSY", "a recording is already active")
		return
	}
	region, err := recordsess.Select(rect, displays)
	if err != nil {
		done <- agentError("INVALID_ARGS", err.Error())
		return
	}
	dir, err := os.MkdirTemp("", "snipgo-agent-record-")
	if err != nil {
		done <- mediaError(err)
		return
	}
	stream, err := videocap.NewRecorder()
	if err != nil {
		os.RemoveAll(dir)
		done <- mediaError(err)
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &regionRecording{region: region, path: filepath.Join(dir, "record.mp4"), stream: stream, agentCancel: cancel, busy: true}
	a.recording = r
	a.openRecordingControls(r)
	go func() {
		response := runAgentRecording(ctx, r, p)
		cancel()
		// Wait until the helper has released the recording before removing the private directory.
		stream.Close()
		os.RemoveAll(dir)
		mygo.RunOnMain(func() {
			if a.recording == r {
				a.closeRecording(r)
			}
			done <- response
		})
	}()
}

func runAgentRecording(ctx context.Context, r *regionRecording, p agentParams) agentipc.Response {
	region := r.region
	local := region.Local
	var err error
	if p.Window != 0 {
		err = r.stream.StartWindow(ctx, r.path, p.Window, region.Display.ID, region.Display.Scale)
	} else {
		err = r.stream.Start(ctx, r.path, region.Display.ID, int64(local.Min.X), int64(local.Min.Y), int64(local.Dx()), int64(local.Dy()), region.Display.Scale)
	}
	if err != nil {
		return mediaError(err)
	}
	timer := time.NewTimer(time.Duration(p.Duration) * time.Second)
	defer timer.Stop()
	if p.SystemAudio {
		if err = r.stream.SetSystem(ctx, true); err != nil {
			return mediaError(err)
		}
	}
	select {
	case <-ctx.Done():
		return mediaError(ctx.Err())
	case <-timer.C:
	}
	if err = r.stream.Stop(ctx); err != nil {
		return mediaError(err)
	}
	path := r.path
	if p.Extension == ".gif" {
		path = filepath.Join(filepath.Dir(path), "record.gif")
		if err := exportAgentGIF(ctx, r.path, path, 0, 0, p.Width, p.FPS); err != nil {
			return mediaError(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return mediaError(err)
	}
	if info.Size() > 180<<20 {
		return agentError("PROCESS_FAILED", "recording exceeds transport limit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mediaError(err)
	}
	return agentipc.Response{OK: true, Data: map[string]any{"media": data, "bytes": len(data), "duration": p.Duration}}
}
