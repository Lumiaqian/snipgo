package pinops

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestRenderPixelDiffAndSplitCompare(t *testing.T) {
	a := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	b := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	draw.Draw(a, a.Bounds(), image.NewUniform(color.NRGBA{R: 40, G: 44, B: 52, A: 255}), image.Point{}, draw.Src)
	draw.Draw(b, b.Bounds(), image.NewUniform(color.NRGBA{R: 40, G: 44, B: 52, A: 255}), image.Point{}, draw.Src)
	// 在 b 上修改一个 20x10 区块（200 像素 = 2.5%）
	draw.Draw(b, image.Rect(20, 20, 40, 30), image.NewUniform(color.NRGBA{R: 10, G: 132, B: 255, A: 255}), image.Point{}, draw.Src)

	res := RenderPixelDiff(a, b, 12)
	if res.DiffPixels != 200 || res.TotalPixels != 8000 {
		t.Fatalf("expected 200/8000 diff pixels, got %d/%d", res.DiffPixels, res.TotalPixels)
	}
	if res.DiffPercent < 2.4 || res.DiffPercent > 2.6 {
		t.Fatalf("expected ~2.5%% diff, got %.2f%%", res.DiffPercent)
	}
	if hot := res.Image.NRGBAAt(25, 25); hot.R < 200 {
		t.Fatalf("expected red highlight at changed pixel, got %+v", hot)
	}

	split := RenderSplitCompare(a, b, 0.3, 1.0)
	if split.Bounds().Dx() != 100 || split.Bounds().Dy() != 80 {
		t.Fatalf("unexpected split bounds: %v", split.Bounds())
	}
	// 左侧 x=22 属于 before (深灰)，右侧 x=35 属于 after (蓝色)
	if left := split.NRGBAAt(22, 25); left.B > 80 {
		t.Fatalf("expected before pixel on left of split, got %+v", left)
	}
	if right := split.NRGBAAt(35, 25); right.B < 200 {
		t.Fatalf("expected after pixel on right of split, got %+v", right)
	}
}

func TestSnapBoundsEdgeAndMergeImages(t *testing.T) {
	other := image.Rect(100, 100, 300, 260)
	moving := image.Rect(305, 104, 505, 264) // 距离 other 右边 5px，顶边差 4px
	snapped, sx, sy := SnapBounds(moving, []image.Rectangle{other}, 8)
	if !sx || !sy {
		t.Fatalf("expected both axes to snap, got sx=%v sy=%v", sx, sy)
	}
	if snapped.Min.X != 300 || snapped.Min.Y != 100 {
		t.Fatalf("expected snapped to (300,100), got %v", snapped.Min)
	}

	im1 := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	im2 := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	hMerged := MergeImages([]*image.NRGBA{im1, im2}, true, 12, color.NRGBA{R: 20, G: 22, B: 28, A: 255})
	if hMerged.Bounds().Dx() != 80+100+12*3 || hMerged.Bounds().Dy() != 60+12*2 {
		t.Fatalf("unexpected horizontal merge size: %v", hMerged.Bounds())
	}
	vMerged := MergeImages([]*image.NRGBA{im1, im2}, false, 10, color.NRGBA{R: 20, G: 22, B: 28, A: 255})
	if vMerged.Bounds().Dx() != 100+10*2 || vMerged.Bounds().Dy() != 60+40+10*3 {
		t.Fatalf("unexpected vertical merge size: %v", vMerged.Bounds())
	}
}
