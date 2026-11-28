package agentapi

import (
	"context"
	"errors"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/pinops"
	"snipgo/internal/vision"
)

var ErrInvalidArgs = errors.New("invalid arguments")

type ImageOptions struct {
	Command, Out, Format, Style, Kind, Padding, Mode, Title, Caption string
	Date                                                             bool
	Theme, Trim, Threshold                                           int
	Ratio, Limit, Scale                                              float64
}

func ProcessImage(ctx context.Context, paths []string, opts ImageOptions) (map[string]any, error) {
	if len(paths) == 0 || (opts.Command == "diff" && len(paths) != 2) {
		return nil, ErrInvalidArgs
	}
	img, err := readImage(paths[0])
	if err != nil {
		return nil, err
	}
	data := map[string]any{"out": opts.Out}
	switch opts.Command {
	case "ocr", "redact":
		if opts.Command == "ocr" && !slices.Contains([]string{"text", "tsv", "markdown"}, opts.Format) || opts.Command == "redact" && opts.Style != "erase" && opts.Style != "mosaic" {
			return nil, ErrInvalidArgs
		}
		res, err := vision.Recognize(img)
		if err != nil {
			return nil, errors.New("OCR processing failed")
		}
		if opts.Command == "ocr" {
			table, ok := vision.ExtractTable(res.Boxes)
			return map[string]any{"text": res.Text, "format": opts.Format, "has_table": ok, "table_markdown": table.Markdown(), "table_tsv": table.TSV(), "links": vision.ExtractLinks(res), "box_count": len(res.Boxes)}, nil
		}
		w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
		var boxes []annotate.RectF
		for _, b := range vision.SensitiveBoxes(res) {
			boxes = append(boxes, annotate.RectF{X: b.X * w, Y: b.Top * h, W: b.W * w, H: b.H * h})
		}
		canvas := annotate.NewCanvas()
		data["redacted_count"] = canvas.AddRedactions(boxes, opts.Style == "erase")
		annotate.Render(img, img.Rect, canvas.Items())
	case "cutout":
		if opts.Trim < -1 {
			return nil, ErrInvalidArgs
		}
		img, err = vision.Cutout(img)
		if err != nil {
			return nil, err
		}
		if opts.Trim >= 0 {
			img = vision.TrimTransparent(img, opts.Trim)
		}
	case "template":
		k, ok := beautify.ParseTemplateKind(opts.Kind)
		if !ok || opts.Theme < 0 || opts.Theme >= len(beautify.TemplateThemes(k)) || !(opts.Scale > 0 && opts.Scale <= 4) || !slices.Contains([]string{"normal", "compact", "wide"}, opts.Padding) {
			return nil, ErrInvalidArgs
		}
		img = beautify.RenderTemplate(img, opts.Scale, beautify.TemplateOptions{Kind: k, Theme: opts.Theme, Title: opts.Title, Caption: opts.Caption, Padding: beautify.ParseTemplatePadding(opts.Padding), ShowDate: opts.Date})
	case "diff":
		if opts.Threshold < 0 || opts.Threshold > 255 || !(opts.Ratio >= 0 && opts.Ratio <= 1) || !(opts.Limit >= 0 && opts.Limit <= 100) || opts.Mode != "split" && opts.Mode != "heatmap" {
			return nil, ErrInvalidArgs
		}
		other, err := readImage(paths[1])
		if err != nil {
			return nil, err
		}
		if max(img.Rect.Dx(), other.Rect.Dx()) > 40000000/max(img.Rect.Dy(), other.Rect.Dy()) {
			return nil, ErrInvalidArgs
		}
		diff := pinops.RenderPixelDiff(img, other, uint8(opts.Threshold))
		data["mode"] = opts.Mode
		data["diff_percent"] = diff.DiffPercent
		data["diff_pixels"] = diff.DiffPixels
		data["total_pixels"] = diff.TotalPixels
		data["passed"] = diff.DiffPercent <= opts.Limit
		if opts.Mode == "split" {
			img = pinops.RenderSplitCompare(img, other, opts.Ratio, 1)
		} else {
			img = diff.Image
		}
	}
	data["width"] = img.Rect.Dx()
	data["height"] = img.Rect.Dy()
	tmp, err := os.CreateTemp(filepath.Dir(opts.Out), ".snipgo-*.png")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	err = png.Encode(tmp, img)
	closeErr := tmp.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp.Name(), opts.Out); err != nil {
		return nil, err
	}
	if data["passed"] == false {
		return nil, errors.New("maximum difference exceeded")
	}
	return data, nil
}
func readImage(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Height <= 0 || cfg.Width <= 0 || cfg.Width > 40000000/cfg.Height {
		return nil, ErrInvalidArgs
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(src.Bounds())
	draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
	return img, nil
}
