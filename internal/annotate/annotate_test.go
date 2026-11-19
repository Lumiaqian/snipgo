package annotate

import (
	"image"
	"image/color"
	"testing"
)

func TestCanvasUndoRedoAndStepNumbering(t *testing.T) {
	c := NewCanvas()
	c.Tool = ToolStep
	if !c.BeginPointer(10, 20) || !c.BeginPointer(30, 40) {
		t.Fatal("ToolStep BeginPointer failed")
	}
	items := c.Items()
	if len(items) != 2 || items[0].Step != 1 || items[1].Step != 2 {
		t.Fatalf("unexpected steps: %+v", items)
	}
	if !c.Undo() {
		t.Fatal("Undo failed")
	}
	// 撤销第 2 步后，再次点击应重新从 2 开始编号。
	if got := c.NextStep(); got != 2 {
		t.Fatalf("NextStep after undo = %d, want 2", got)
	}
	if !c.Redo() || len(c.Items()) != 2 {
		t.Fatal("Redo failed")
	}
}

func TestRasterAllToolsAtRetinaScale(t *testing.T) {
	// 100x80 DIP 选区，2x Retina = 200x160 像素。
	sel := image.Rect(50, 60, 150, 140)
	dst := image.NewNRGBA(image.Rect(0, 0, 200, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 200; x++ {
			if (x/4+y/4)%2 == 0 {
				dst.SetNRGBA(x, y, color.NRGBA{R: 240, G: 240, B: 240, A: 255})
			} else {
				dst.SetNRGBA(x, y, color.NRGBA{R: 40, G: 40, B: 40, A: 255})
			}
		}
	}

	c := NewCanvas()
	c.Tool = ToolMosaic
	c.BeginPointer(55, 65)
	c.EndPointer(85, 95)

	c.Tool = ToolRect
	c.Color = Palette[0]
	c.BeginPointer(60, 70)
	c.EndPointer(140, 130)

	c.Tool = ToolArrow
	c.Color = Palette[3]
	c.BeginPointer(65, 75)
	c.EndPointer(120, 115)

	c.Tool = ToolEllipse
	c.Color = Palette[2]
	c.BeginPointer(70, 80)
	c.EndPointer(110, 110)

	c.Tool = ToolPen
	c.Color = Palette[1]
	c.BeginPointer(60, 120)
	c.MovePointer(90, 120)
	c.EndPointer(120, 120)

	c.Tool = ToolStep
	c.BeginPointer(130, 80)

	c.AddText(65, 95, "批注 OK")

	Render(dst, sel, c.Items())

	// 验证马赛克区域内部 (15, 15) 已将原本黑白棋盘(40 / 240)均值模糊为中间灰度。
	mPx := dst.NRGBAAt(15, 15)
	if mPx.R < 80 || mPx.R > 200 {
		t.Fatalf("mosaic pixel at (15,15) was not averaged: %+v", mPx)
	}
	// 验证矩形红框左边缘 (60-50)*2 = 20px, y=(100-60)*2 = 80px 处已着红色。
	rPx := dst.NRGBAAt(20, 80)
	if rPx.R < 200 || rPx.G > 100 {
		t.Fatalf("rect border pixel at (20,80) not red: %+v", rPx)
	}
	// 验证序号圆标中心 (130-50)*2 = 160, (80-60)*2 = 40 附近有橙色填充。
	badgeRim := dst.NRGBAAt(160+16, 40)
	if badgeRim.R < 200 {
		t.Fatalf("step badge rim at (176,40) not colored: %+v", badgeRim)
	}
}

func TestArrowSolidFillTaperAndHighlighterUniformity(t *testing.T) {
	sel := image.Rect(0, 0, 200, 120)
	dst := image.NewNRGBA(image.Rect(0, 0, 400, 240)) // 2x Retina
	bg := color.NRGBA{R: 30, G: 32, B: 38, A: 255}
	for y := range 240 {
		for x := range 400 {
			dst.SetNRGBA(x, y, bg)
		}
	}

	c := NewCanvas()
	c.Tool = ToolArrow
	c.Color = Palette[0] // 红
	c.Stroke = 3.5
	c.BeginPointer(20, 40)
	c.EndPointer(170, 40) // 水平向右箭头：像素起点 (40,80) -> 尖端 (340,80)

	c.Tool = ToolPen
	c.Color = Palette[1] // 橙
	c.Stroke = 3.0
	c.BeginPointer(30, 90)
	c.MovePointer(100, 90)
	c.EndPointer(170, 90) // 拐点像素 (200,180) 与中点像素 (130,180)

	Render(dst, sel, c.Items())

	// 1. 箭头头部内部中心像素 (326, 80) 必须是实心纯红（绝不能是空心线框！）。
	headInterior := dst.NRGBAAt(326, 80)
	if headInterior != Palette[0] {
		t.Fatalf("arrowhead interior at (326,80) = %+v, want solid %+v", headInterior, Palette[0])
	}

	// 2. 验证流线型锥度：尾部宽 < 颈部宽 < 倒刺翼展宽。
	spanAtX := func(px int) int {
		n := 0
		for y := 40; y < 120; y++ {
			if dst.NRGBAAt(px, y).R > 150 {
				n++
			}
		}
		return n
	}
	tailSpan := spanAtX(60)
	neckSpan := spanAtX(280)
	wingSpan := spanAtX(305)
	if !(tailSpan >= 2 && tailSpan < neckSpan && neckSpan < wingSpan) {
		t.Fatalf("arrow taper broken: tail=%d neck=%d wing=%d", tailSpan, neckSpan, wingSpan)
	}

	// 3. 验证荧光笔折点处 (200, 180) 不会因重复叠加比线段中点 (130, 180) 颜色更重。
	midPen := dst.NRGBAAt(130, 180)
	jointPen := dst.NRGBAAt(200, 180)
	if jointPen != midPen {
		t.Fatalf("highlighter joint double-blended: joint=%+v vs mid=%+v", jointPen, midPen)
	}
}

func TestSpotlightSmartEraseAndObjectSecondaryEditing(t *testing.T) {
	c := NewCanvas()
	c.Tool = ToolArrow
	c.Color = Palette[0]
	c.BeginPointer(20, 30)
	c.EndPointer(120, 30)

	// 1. 点选刚画的箭头，平移 (+15, +20)，再改颜色为绿色，再单独删除，并逐级 Undo 验证。
	if !c.BeginEdit(70, 30) {
		t.Fatal("BeginEdit failed to hit arrow")
	}
	c.MoveEdit(85, 50)
	if !c.EndEdit() {
		t.Fatal("EndEdit expected moved=true")
	}
	selIt, ok := c.SelectedItem()
	if !ok || selIt.P0.X != 35 || selIt.P1.Y != 50 {
		t.Fatalf("unexpected moved arrow: %+v", selIt)
	}
	c.SetColor(Palette[2])
	if selIt, _ = c.SelectedItem(); selIt.Color != Palette[2] {
		t.Fatalf("SetColor did not update selected item: %+v", selIt)
	}
	if !c.DeleteSelected() || len(c.Items()) != 0 {
		t.Fatal("DeleteSelected failed")
	}
	if !c.Undo() || len(c.Items()) != 1 || c.Items()[0].Color != Palette[2] {
		t.Fatal("Undo after delete failed")
	}
	if !c.Undo() || c.Items()[0].Color != Palette[0] {
		t.Fatal("Undo after recolor failed")
	}

	// 2. 验证无痕擦除 (ToolErase) 与聚光灯 (ToolSpotlight) 像素渲染。
	sel := image.Rect(0, 0, 200, 120)
	dst := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	bright := color.NRGBA{R: 220, G: 224, B: 232, A: 255}
	for y := range 120 {
		for x := range 200 {
			dst.SetNRGBA(x, y, bright)
		}
	}
	// 在 (20,20)-(50,35) 画一块黑斑模拟脏字，用 ToolErase 无痕抹掉。
	for y := 20; y < 35; y++ {
		for x := 20; x < 50; x++ {
			dst.SetNRGBA(x, y, color.NRGBA{R: 10, G: 10, B: 10, A: 255})
		}
	}

	c.Reset()
	c.Tool = ToolErase
	c.BeginPointer(18, 18)
	c.EndPointer(52, 37)

	c.Tool = ToolSpotlight
	c.BeginPointer(100, 20)
	c.EndPointer(170, 90)

	Render(dst, sel, c.Items())

	// 聚光灯内部 (135, 55) 必须保持原始亮白，聚光灯外部无痕擦除区 (35, 26) 已抹除黑字并被聚光灯统一压暗 (~92)。
	insideSpot := dst.NRGBAAt(135, 55)
	if insideSpot != bright {
		t.Fatalf("inside spotlight = %+v, want untouched %+v", insideSpot, bright)
	}
	erasedAndDimmed := dst.NRGBAAt(35, 26)
	if erasedAndDimmed.R < 75 || erasedAndDimmed.R > 110 {
		t.Fatalf("expected smart-erased bright bg dimmed by spotlight (~92), got %+v", erasedAndDimmed)
	}
}
