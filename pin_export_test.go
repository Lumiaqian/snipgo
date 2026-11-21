package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"snipgo/internal/annotate"
)

func TestPinExportPNGIncludesInProgressEdits(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	draw.Draw(base, base.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, base); err != nil {
		t.Fatal(err)
	}
	p := &pinWin{
		baseImg:  base,
		img:      base,
		png:      buf.Bytes(),
		annot:    annotate.NewCanvas(),
		origW:    100,
		origH:    80,
		zoom:     1,
		editMode: true,
	}
	p.annot.SetItems([]annotate.Item{{
		Tool:   annotate.ToolRect,
		Color:  color.NRGBA{R: 255, A: 255},
		Stroke: 6,
		P0:     annotate.PointF{X: 20, Y: 20},
		P1:     annotate.PointF{X: 80, Y: 60},
	}})
	out, err := png.Decode(bytes.NewReader(p.exportPNG()))
	if err != nil {
		t.Fatal(err)
	}
	if !p.editMode {
		t.Fatal("exportPNG 不应退出编辑模式")
	}
	// 矩形上边框中点
	got := color.NRGBAModel.Convert(out.At(50, 20)).(color.NRGBA)
	if got == (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("导出结果未包含标注: %v", got)
	}
}
