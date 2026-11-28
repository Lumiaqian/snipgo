package videocap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// The signed app executes its bundled Swift helper. No third-party library is
// mapped into the hardened Go host or into the helper.
type helper struct {
	cmd          *exec.Cmd
	input        io.WriteCloser
	output       *bufio.Reader
	mu           sync.Mutex
	closed       bool
	architecture string
}

type request struct {
	WindowID  uint32  `json:"windowID,omitempty"`
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	Dst       string  `json:"dst"`
	DisplayID int64   `json:"displayID"`
	X         int64   `json:"x"`
	Y         int64   `json:"y"`
	Width     int64   `json:"width"`
	Height    int64   `json:"height"`
	FPS       int64   `json:"fps"`
	Scale     float64 `json:"scale"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Enabled   bool    `json:"enabled"`
}
type response struct {
	Architecture string
	Error        string
	Progress     *int64
	Dimensions   []int64
	Level        float64
	Bytes        int
	pixels       []byte
}

func helperPath() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("recording requires macOS")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		return filepath.Join(filepath.Dir(dir), "Resources", "bin", "SnipGoVideo"), nil
	}
	// go run and package tests run outside the app bundle.
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return "", errors.New("cannot locate SnipGoVideo outside app bundle")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "resources", "darwin-"+runtime.GOARCH, "bin", "SnipGoVideo"), nil
}

func newHelper(role string) (*helper, error) {
	path, err := helperPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, role)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	h := &helper{cmd: cmd, input: in, output: bufio.NewReader(out)}
	ready, err := h.readReply(nil)
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("create video %s: %w", role, err)
	}
	h.architecture = ready.Architecture
	return h, nil
}

func (h *helper) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.input.Close()
	return h.cmd.Wait()
}

func (h *helper) readReply(progress func(int64)) (response, error) {
	for {
		var r response
		line, err := h.output.ReadBytes('\n')
		if err != nil {
			return r, err
		}
		if err = json.Unmarshal(line, &r); err != nil {
			return r, err
		}
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		if r.Progress != nil {
			if progress != nil {
				progress(*r.Progress)
			}
			continue
		}
		if r.Bytes < 0 || r.Bytes > 512<<20 {
			return r, fmt.Errorf("invalid video frame length: %d", r.Bytes)
		}
		if r.Bytes > 0 {
			r.pixels = make([]byte, r.Bytes)
			_, err = io.ReadFull(h.output, r.pixels)
		}
		return r, err
	}
}
func (h *helper) call(ctx context.Context, r request, progress func(int64)) (response, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return response{}, err
	}
	if h.closed {
		return response{}, os.ErrClosed
	}
	cancel := context.AfterFunc(ctx, func() { _ = h.cmd.Process.Kill() })
	defer cancel()
	if err := json.NewEncoder(h.input).Encode(r); err != nil {
		return response{}, err
	}
	reply, err := h.readReply(progress)
	if ctx.Err() != nil {
		return response{}, ctx.Err()
	}
	return reply, err
}

func NewRecorder() (Recorder, error) { return newHelper("recorder") }
func NewStudio() (Studio, error)     { return newHelper("studio") }
func NewFrames() (Frames, error)     { return newHelper("frames") }
func (h *helper) Start(ctx context.Context, path string, id, x, y, w, height int64, scale float64) error {
	_, err := h.call(ctx, request{Method: "start", Path: path, DisplayID: id, X: x, Y: y, Width: w, Height: height, Scale: scale}, nil)
	return err
}
func (h *helper) StartWindow(ctx context.Context, path string, id uint32, display int64, scale float64) error {
	_, err := h.call(ctx, request{Method: "start", Path: path, WindowID: id, DisplayID: display, Scale: scale}, nil)
	return err
}
func (h *helper) SetSystem(ctx context.Context, on bool) error {
	_, err := h.call(ctx, request{Method: "system", Enabled: on}, nil)
	return err
}
func (h *helper) SetMic(ctx context.Context, on bool) error {
	_, err := h.call(ctx, request{Method: "mic", Enabled: on}, nil)
	return err
}
func (h *helper) Pause(ctx context.Context, on bool) error {
	_, err := h.call(ctx, request{Method: "pause", Enabled: on}, nil)
	return err
}
func (h *helper) Level(ctx context.Context) (float64, error) {
	r, err := h.call(ctx, request{Method: "level"}, nil)
	return r.Level, err
}
func (h *helper) Stop(ctx context.Context) error {
	_, err := h.call(ctx, request{Method: "stop"}, nil)
	return err
}
func (h *helper) Export(ctx context.Context, src, dst string, start, end float64, width, fps int64, mute bool, progress func(int64)) error {
	_, err := h.call(ctx, request{Method: "export", Path: src, Dst: dst, Start: start, End: end, Width: width, FPS: fps, Enabled: mute}, progress)
	return err
}
func (h *helper) Open(ctx context.Context, path string, start, end float64, width, fps int64) ([]int64, error) {
	r, err := h.call(ctx, request{Method: "open", Path: path, Start: start, End: end, Width: width, FPS: fps}, nil)
	return r.Dimensions, err
}
func (h *helper) Next() ([]byte, error) {
	r, err := h.call(context.Background(), request{Method: "next"}, nil)
	return r.pixels, err
}

// CheckRecorder exercises the same constructor as the recording button without
// requesting capture access. Report the helper slice actually selected by macOS.
func CheckRecorder() (string, error) {
	r, err := NewRecorder()
	if err != nil {
		return "", err
	}
	h := r.(*helper)
	return h.architecture, h.Close()
}
