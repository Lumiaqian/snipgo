// Package videocap bridges regional video capture and trimmed movie export.
package videocap

import "context"

// Studio exports a trimmed recording.
type Studio interface {
	Close() error
	Export(ctx context.Context, src string, dst string, start float64, end float64, width int64, fps int64, mute bool, progress func(int64)) error
}

// Frames decodes one trimmed video sequentially. Open starts a fresh pass.
type Frames interface {
	Close() error
	Open(ctx context.Context, path string, start float64, end float64, width int64, fps int64) ([]int64, error)
	Next() ([]byte, error)
}

// Recorder owns a single region stream on one display.
type Recorder interface {
	StartWindow(ctx context.Context, path string, id uint32, display int64, scale float64, appAudio bool) error
	Close() error
	Start(ctx context.Context, path string, displayID int64, x int64, y int64, width int64, height int64, scale float64) error
	SetSystem(ctx context.Context, enabled bool) error
	SetMic(ctx context.Context, enabled bool) error
	Level(ctx context.Context) (float64, error)
	Pause(ctx context.Context, paused bool) error
	Stop(ctx context.Context) error
}
