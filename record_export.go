package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"snipgo/internal/gifenc"
	"snipgo/internal/videocap"
)

type exportOptions struct {
	source, destination string
	start, end          float64
	width, fps          int
	gif, mute           bool
}

func exportMovie(ctx context.Context, studio videocap.Studio, o exportOptions, progress func(int)) error {
	// Keep partial output beside the destination so the final rename is atomic.
	dir, e := os.MkdirTemp(filepath.Dir(o.destination), ".snipgo-export-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, filepath.Base(o.destination))
	if o.gif {
		e = writeGIF(ctx, tmp, o, progress)
	} else {
		e = studio.Export(ctx, o.source, tmp, o.start, o.end, int64(o.width), int64(o.fps), o.mute, func(p int64) { progress(int(p)) })
	}
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return os.Rename(tmp, o.destination)
}
func writeGIF(ctx context.Context, path string, o exportOptions, progress func(int)) error {
	decoder, e := videocap.NewFrames()
	if e != nil {
		return e
	}
	defer decoder.Close()
	file, e := os.Create(path)
	if e != nil {
		return e
	}
	pass := 0
	source := func(yield func(*image.RGBA) error) error {
		dims, e := decoder.Open(ctx, o.source, o.start, o.end, int64(o.width), int64(o.fps))
		if e != nil {
			return e
		}
		if len(dims) != 3 || dims[0] < 1 || dims[1] < 1 || dims[2] < 1 {
			return fmt.Errorf("无可导出的画面")
		}
		w, h, count := int(dims[0]), int(dims[1]), int(dims[2])
		last := -1
		for i := 0; i < count; i++ {
			if e := ctx.Err(); e != nil {
				return e
			}
			pixels, e := decoder.Next()
			if e != nil {
				return e
			}
			if len(pixels) != w*h*4 {
				return fmt.Errorf("视频帧尺寸不一致")
			}
			if e = yield(&image.RGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}); e != nil {
				return e
			}
			pct := pass*50 + (i+1)*50/count
			if pct != last {
				progress(pct)
				last = pct
			}
		}
		pass++
		return nil
	}
	err := gifenc.Encode(ctx, file, o.fps, source)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func exportAgentGIF(ctx context.Context, source, dest string, start, end float64, width, fps int) error {
	return exportMovie(ctx, nil, exportOptions{source: source, destination: dest, start: start, end: end, width: width, fps: fps, gif: true}, func(int) {})
}
