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

func TestLoupeMagnifiesTargetAndSupportsEndpointEditing(t *testing.T) {
	sel := image.Rect(0, 0, 260, 180)
	dst := image.NewNRGBA(image.Rect(0, 0, 260, 180))
	for y := range 180 {
		for x := range 260 {
			dst.SetNRGBA(x, y, color.NRGBA{R: 30, G: 34, B: 42, A: 255})
		}
	}
	// 在 (45, 50) 附近画一个小鲜绿色目标块
	for y := 44; y <= 56; y++ {
		for x := 39; x <= 51; x++ {
			dst.SetNRGBA(x, y, color.NRGBA{R: 48, G: 215, B: 96, A: 255})
		}
	}

	c := NewCanvas()
	c.Tool = ToolLoupe
	c.Stroke = 3.0
	c.SetLoupeZoom(3.0)
	c.BeginPointer(45, 50)
	c.EndPointer(170, 95)

	if len(c.Items()) != 1 || c.Items()[0].Tool != ToolLoupe {
		t.Fatalf("expected 1 loupe item, got %+v", c.Items())
	}
	// 选中镜心并拖动调整倍率
	c.Tool = ToolNone
	if !c.BeginEdit(170, 95) {
		t.Fatal("expected hit on loupe lens body")
	}
	c.EndEdit()
	c.SetLoupeZoom(2.5)

	Render(dst, sel, c.Items())
	// 镜心 (170, 95) 处应该采样放大到来自 (45, 50) 的鲜绿色！
	lensMid := dst.NRGBAAt(170, 95)
	if lensMid.G < 180 || lensMid.R > 90 {
		t.Fatalf("expected magnified green at lens center (170,95), got %+v", lensMid)
	}
}

func TestLoupeAutoSelectInPlaceMoveAndCleanWhiteRing(t *testing.T) {
	c := NewCanvas()
	c.Tool = ToolLoupe
	c.Stroke = 3.0
	c.SetLoupeZoom(2.5)

	// 1. 原地放大镜落点后应自动选中，切倍率直接作用于它，且圆心 HitTest 返回 EditMove。
	c.BeginPointer(80, 70)
	c.EndPointer(82, 71)
	if c.Selected != 0 {
		t.Fatalf("expected newly placed loupe to be selected (idx=0), got %d", c.Selected)
	}
	c.SetLoupeZoom(4.0)
	if got := c.Items()[0].Zoom; got != 4.0 {
		t.Fatalf("expected SetLoupeZoom to update selected loupe to 4.0, got %v", got)
	}
	idx, part := c.HitTest(80, 70)
	if idx != 0 || part != EditMove {
		t.Fatalf("in-place loupe HitTest center = (%d, %v), want (0, EditMove)", idx, part)
	}
	if !c.BeginEdit(80, 70) {
		t.Fatal("BeginEdit on in-place loupe failed")
	}
	c.MoveEdit(110, 95)
	c.EndEdit()
	moved := c.Items()[0]
	if moved.P0 != (PointF{X: 110, Y: 95}) || moved.P1 != (PointF{X: 110, Y: 95}) {
		t.Fatalf("expected in-place loupe to translate both P0 and P1, got %+v", moved)
	}

	// 2. 带引线放大镜保留首尾控制柄检测。
	c.BeginPointer(40, 40)
	c.EndPointer(180, 100)
	if c.Selected != 1 {
		t.Fatalf("expected callout loupe selected idx=1, got %d", c.Selected)
	}
	if idx, part := c.HitTest(40, 40); idx != 1 || part != EditP0 {
		t.Fatalf("callout loupe HitTest P0 = (%d, %v), want (1, EditP0)", idx, part)
	}
	if idx, part := c.HitTest(180, 100); idx != 1 || part != EditP1 {
		t.Fatalf("callout loupe HitTest P1 = (%d, %v), want (1, EditP1)", idx, part)
	}

	// 3. 白底渲染验证：白环与镜内接缝绝不渗入黑影灰缝。
	dst := image.NewNRGBA(image.Rect(0, 0, 260, 180))
	for y := range 180 {
		for x := range 260 {
			dst.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	c2 := NewCanvas()
	c2.Tool = ToolLoupe
	c2.Stroke = 3.0 // radPx = 52
	c2.BeginPointer(130, 90)
	c2.EndPointer(130, 90)
	Render(dst, image.Rect(0, 0, 260, 180), c2.Items())

	// 检查白环位置 (dist=53.3，在镜外缘白圈上) 四方位像素必须洁白无暗缝。
	for _, pt := range []image.Point{{130, 90 - 53}, {130, 90 + 53}, {130 - 53, 90}, {130 + 53, 90}} {
		px := dst.NRGBAAt(pt.X, pt.Y)
		if px.R < 245 || px.G < 245 || px.B < 245 {
			t.Fatalf("unexpected dark seam on white loupe ring at %v: %+v", pt, px)
		}
	}
}
