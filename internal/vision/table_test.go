package vision

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestExtractTableMarkdownAndTSV(t *testing.T) {
	boxes := []TextBox{
		{Text: "模块", X: 0.05, Top: 0.10, W: 0.16, H: 0.06},
		{Text: "耗时", X: 0.42, Top: 0.10, W: 0.14, H: 0.06},
		{Text: "状态", X: 0.74, Top: 0.10, W: 0.16, H: 0.06},

		{Text: "Vision OCR", X: 0.05, Top: 0.30, W: 0.24, H: 0.06},
		{Text: "0.18s", X: 0.42, Top: 0.30, W: 0.14, H: 0.06},
		{Text: "正常", X: 0.74, Top: 0.30, W: 0.14, H: 0.06},

		{Text: "主体抠图", X: 0.05, Top: 0.50, W: 0.18, H: 0.06},
		{Text: "0.24s", X: 0.42, Top: 0.50, W: 0.14, H: 0.06},
		{Text: "已启用", X: 0.74, Top: 0.50, W: 0.16, H: 0.06},
	}

	tbl, ok := ExtractTable(boxes)
	if !ok {
		t.Fatal("expected structured table detection to succeed")
	}
	if len(tbl.Rows) != 3 || tbl.Cols != 3 {
		t.Fatalf("expected 3x3 table, got %dx%d: %+v", len(tbl.Rows), tbl.Cols, tbl.Rows)
	}
	md := tbl.Markdown()
	if !strings.Contains(md, "| 模块 | 耗时 | 状态 |") || !strings.Contains(md, "| Vision OCR | 0.18s | 正常 |") {
		t.Fatalf("unexpected markdown table:\n%s", md)
	}
	tsv := tbl.TSV()
	if !strings.Contains(tsv, "主体抠图\t0.24s\t已启用") {
		t.Fatalf("unexpected TSV output:\n%s", tsv)
	}
}

func TestExtractTableRejectsPlainParagraph(t *testing.T) {
	boxes := []TextBox{
		{Text: "这是一段普通的单列自然段落文本第一行", X: 0.05, Top: 0.10, W: 0.85, H: 0.06},
		{Text: "这是第二行紧接在下面的普通句子", X: 0.05, Top: 0.24, W: 0.82, H: 0.06},
		{Text: "这是第三行结尾。", X: 0.05, Top: 0.38, W: 0.40, H: 0.06},
	}
	if _, ok := ExtractTable(boxes); ok {
		t.Fatal("plain paragraph should not be misclassified as table")
	}
}

func TestTrimTransparentCropsTightly(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 30; y < 50; y++ {
		for x := 20; x < 60; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 80, B: 40, A: 255})
		}
	}
	trimmed := TrimTransparent(img, 2)
	if trimmed.Bounds().Dx() != 44 || trimmed.Bounds().Dy() != 24 {
		t.Fatalf("expected 44x24 trimmed bounds, got %v", trimmed.Bounds())
	}
}
