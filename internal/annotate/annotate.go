// Package annotate 管理选区或贴图内的矢量标注图层（矩形、椭圆、箭头、荧光笔、序号、马赛克、文字、聚光灯、无痕擦除），
// 支持图元点选、拖拽平移、首尾控制点微调、改色改粗细、单独删除与多步快照撤销/重做。
package annotate

import (
	"image/color"
	"math"
	"strings"
)

// Tool 表示当前激活的标注工具。ToolNone 表示指针选择/微调模式。
type Tool int

const (
	ToolNone Tool = iota
	ToolRect
	ToolEllipse
	ToolArrow
	ToolPen
	ToolStep
	ToolMosaic
	ToolText
	ToolSpotlight
	ToolErase
	ToolLoupe
)

// EditPart 表示命中已存在标注图元的哪个交互部位。
type EditPart int

const (
	EditNone EditPart = iota
	EditMove
	EditP0
	EditP1
)

// PointF 是全局（或画布局部）DIP 浮点坐标。
type PointF struct {
	X, Y float64
}

// RectF 是浮点矩形。
type RectF struct {
	X, Y, W, H float64
}

// Palette 是预设标注调色板（红、橙、绿、蓝、白）。
var Palette = []color.NRGBA{
	{R: 255, G: 59, B: 48, A: 255},   // 红
	{R: 255, G: 149, B: 0, A: 255},   // 橙
	{R: 52, G: 199, B: 89, A: 255},   // 绿
	{R: 0, G: 168, B: 255, A: 255},   // 蓝
	{R: 255, G: 255, B: 255, A: 255}, // 白
}

// Item 是一条矢量标注图元。
type Item struct {
	Tool   Tool
	Color  color.NRGBA
	Stroke float64  // 线宽（DIP）
	P0, P1 PointF   // 矩形 / 椭圆 / 箭头 / 马赛克 / 聚光灯 / 擦除 / 序号 / 文字锚点 / 放大镜采样点与镜心
	Points []PointF // 荧光笔自由曲线轨迹
	Step   int      // ToolStep 的序号（1, 2, 3...）
	Text   string   // ToolText 的文本内容
	Zoom   float64  // ToolLoupe 的局部放大倍率（默认 2.5，范围 1.5~5.0）
}

// LoupeRadius 根据三档粗细返回局部放大镜气泡的半径（DIP）。
func LoupeRadius(stroke float64) float64 {
	switch {
	case stroke <= 2.2:
		return 38
	case stroke >= 4.5:
		return 68
	default:
		return 52
	}
}

func cloneItems(src []Item) []Item {
	if len(src) == 0 {
		return nil
	}
	out := make([]Item, len(src))
	for i, it := range src {
		if len(it.Points) > 0 {
			pts := make([]PointF, len(it.Points))
			copy(pts, it.Points)
			it.Points = pts
		}
		out[i] = it
	}
	return out
}

const maxHistoryDepth = 64

// Canvas 维护一轮选区或贴图内的当前工具、样式、选中图元与快照撤销/重做栈。
type Canvas struct {
	Tool      Tool
	Color     color.NRGBA
	Stroke    float64
	LoupeZoom float64 // ToolLoupe 默认放大倍率（默认 2.5）
	Selected  int     // 当前选中的图元下标，未选中为 -1

	items   []Item
	undo    [][]Item
	redo    [][]Item
	drawing *Item

	editing     bool
	editPart    EditPart
	editStartX  float64
	editStartY  float64
	editOrig    Item
	editMoved   bool
	preEditSnap []Item
}

// NewCanvas 返回带默认红色与中等线宽的标注画布。
func NewCanvas() *Canvas {
	return &Canvas{
		Tool:      ToolNone,
		Color:     Palette[0],
		Stroke:    3.0,
		LoupeZoom: 2.5,
		Selected:  -1,
	}
}

// Reset 清空所有标注与编辑状态，保留上次选择的颜色和线宽。
func (c *Canvas) Reset() {
	c.Tool = ToolNone
	if c.Color.A == 0 {
		c.Color = Palette[0]
	}
	if c.Stroke <= 0 {
		c.Stroke = 3.0
	}
	if c.LoupeZoom <= 0 {
		c.LoupeZoom = 2.5
	}
	c.Selected = -1
	c.items = nil
	c.undo = nil
	c.redo = nil
	c.drawing = nil
	c.editing = false
}

// SetItems 用给定的标注切片初始化画布（供贴图二次编辑加载已有标注）。
func (c *Canvas) SetItems(items []Item) {
	c.items = cloneItems(items)
	c.undo = nil
	c.redo = nil
	c.Selected = -1
	c.drawing = nil
	c.editing = false
}

func (c *Canvas) pushUndo() {
	if len(c.undo) >= maxHistoryDepth {
		c.undo = c.undo[1:]
	}
	c.undo = append(c.undo, cloneItems(c.items))
	c.redo = nil
}

// Items 返回已提交的标注列表副本。
func (c *Canvas) Items() []Item {
	return cloneItems(c.items)
}

// Drawing 返回当前正在按住鼠标新画的临时图元（无则为 nil）。
func (c *Canvas) Drawing() *Item {
	return c.drawing
}

// Editing 报告是否正在按住鼠标拖拽已有图元或其控制点。
func (c *Canvas) Editing() bool {
	return c.editing
}

// SelectedItem 返回当前选中的图元副本与是否选中。
func (c *Canvas) SelectedItem() (Item, bool) {
	if c.Selected < 0 || c.Selected >= len(c.items) {
		return Item{}, false
	}
	return c.items[c.Selected], true
}

// ClearSelection 取消当前选中的图元。
func (c *Canvas) ClearSelection() {
	c.Selected = -1
	c.editing = false
}

// SetColor 设置画布颜色；若当前选中了某图元，同时实时修改该图元颜色并记录撤销快照。
func (c *Canvas) SetColor(col color.NRGBA) {
	c.Color = col
	if c.Selected >= 0 && c.Selected < len(c.items) && c.items[c.Selected].Color != col {
		c.pushUndo()
		c.items[c.Selected].Color = col
	}
}

// SetStroke 设置画布线宽；若当前选中了某图元，同时实时修改该图元线宽并记录撤销快照。
func (c *Canvas) SetStroke(sw float64) {
	c.Stroke = sw
	if c.Selected >= 0 && c.Selected < len(c.items) && math.Abs(c.items[c.Selected].Stroke-sw) > 0.05 {
		c.pushUndo()
		if c.items[c.Selected].Tool == ToolPen {
			c.items[c.Selected].Stroke = max(10, sw*4)
		} else {
			c.items[c.Selected].Stroke = sw
		}
	}
}

// SetLoupeZoom 设置局部放大镜倍率（1.5x ~ 5.0x）；若当前选中了放大镜图元则实时更新并入撤销栈。
func (c *Canvas) SetLoupeZoom(z float64) {
	z = max(1.5, min(5.0, z))
	c.LoupeZoom = z
	if c.Selected >= 0 && c.Selected < len(c.items) && c.items[c.Selected].Tool == ToolLoupe {
		if math.Abs(c.items[c.Selected].Zoom-z) > 0.05 {
			c.pushUndo()
			c.items[c.Selected].Zoom = z
		}
	}
}

// DeleteSelected 删除当前选中的图元（支持 ⌘Z 撤销恢复）。
func (c *Canvas) DeleteSelected() bool {
	if c.Selected < 0 || c.Selected >= len(c.items) {
		return false
	}
	c.pushUndo()
	c.items = append(c.items[:c.Selected], c.items[c.Selected+1:]...)
	c.Selected = -1
	c.editing = false
	return true
}

// HasItems 报告是否已有标注。
func (c *Canvas) HasItems() bool {
	return len(c.items) > 0
}

// CanUndo 报告是否可撤销。
func (c *Canvas) CanUndo() bool {
	return len(c.undo) > 0
}

// CanRedo 报告是否可重做。
func (c *Canvas) CanRedo() bool {
	return len(c.redo) > 0
}

// NextStep 计算下一个步骤序号（撤销后会自动回退编号）。
func (c *Canvas) NextStep() int {
	n := 1
	for _, it := range c.items {
		if it.Tool == ToolStep && it.Step >= n {
			n = it.Step + 1
		}
	}
	return n
}

// Undo 撤销最近一步标注新增、移动、改色或删除。
func (c *Canvas) Undo() bool {
	if len(c.undo) == 0 {
		return false
	}
	prev := c.undo[len(c.undo)-1]
	c.undo = c.undo[:len(c.undo)-1]
	c.redo = append(c.redo, cloneItems(c.items))
	c.items = prev
	if c.Selected >= len(c.items) {
		c.Selected = -1
	}
	return true
}

// Redo 重做最近撤销的一步操作。
func (c *Canvas) Redo() bool {
	if len(c.redo) == 0 {
		return false
	}
	next := c.redo[len(c.redo)-1]
	c.redo = c.redo[:len(c.redo)-1]
	c.undo = append(c.undo, cloneItems(c.items))
	c.items = next
	if c.Selected >= len(c.items) {
		c.Selected = -1
	}
	return true
}

// HitTest 从顶层到底层检测点 (x, y) 是否命中已有标注图元或其端点手柄。
func (c *Canvas) HitTest(x, y float64) (idx int, part EditPart) {
	pt := PointF{X: x, Y: y}
	// 若已有选中图元，优先检测其首尾控制手柄（半径 8 DIP）。
	if c.Selected >= 0 && c.Selected < len(c.items) {
		sel := c.items[c.Selected]
		if hasTwoEndpoints(sel.Tool) {
			if math.Hypot(pt.X-sel.P0.X, pt.Y-sel.P0.Y) <= 8.5 {
				return c.Selected, EditP0
			}
			if math.Hypot(pt.X-sel.P1.X, pt.Y-sel.P1.Y) <= 8.5 {
				return c.Selected, EditP1
			}
		}
	}
	for i := len(c.items) - 1; i >= 0; i-- {
		if hitItemBody(pt, c.items[i]) {
			return i, EditMove
		}
	}
	return -1, EditNone
}

func hasTwoEndpoints(t Tool) bool {
	switch t {
	case ToolRect, ToolEllipse, ToolArrow, ToolMosaic, ToolSpotlight, ToolErase, ToolLoupe:
		return true
	default:
		return false
	}
}

func hitItemBody(pt PointF, it Item) bool {
	tol := max(6.0, it.Stroke*0.8+4.0)
	minX, maxX := min(it.P0.X, it.P1.X), max(it.P0.X, it.P1.X)
	minY, maxY := min(it.P0.Y, it.P1.Y), max(it.P0.Y, it.P1.Y)

	switch it.Tool {
	case ToolMosaic, ToolSpotlight, ToolErase:
		return pt.X >= minX-4 && pt.X <= maxX+4 && pt.Y >= minY-4 && pt.Y <= maxY+4
	case ToolRect:
		if pt.X < minX-tol || pt.X > maxX+tol || pt.Y < minY-tol || pt.Y > maxY+tol {
			return false
		}
		inInner := pt.X > minX+tol && pt.X < maxX-tol && pt.Y > minY+tol && pt.Y < maxY-tol
		return !inInner
	case ToolEllipse:
		rx := (maxX - minX) * 0.5
		ry := (maxY - minY) * 0.5
		if rx < 2 || ry < 2 {
			return false
		}
		cx, cy := (minX+maxX)*0.5, (minY+maxY)*0.5
		nx, ny := (pt.X-cx)/rx, (pt.Y-cy)/ry
		k1 := math.Hypot(nx, ny)
		if k1 < 1e-5 {
			return false
		}
		k2 := math.Hypot(nx/rx, ny/ry)
		sdf := math.Abs(k1-1.0) * (k1 / k2)
		return sdf <= tol
	case ToolArrow:
		if poly, ok := ArrowPolygon(it.P0, it.P1, max(1.5, it.Stroke)); ok {
			if sdfPolygon(pt, poly[:]) <= 5.0 {
				return true
			}
		}
		return distToSegment(pt, it.P0, it.P1) <= tol
	case ToolPen:
		for i := 0; i+1 < len(it.Points); i++ {
			if distToSegment(pt, it.Points[i], it.Points[i+1]) <= max(8.0, it.Stroke*0.5+3) {
				return true
			}
		}
		if len(it.Points) == 1 {
			return math.Hypot(pt.X-it.Points[0].X, pt.Y-it.Points[0].Y) <= max(8.0, it.Stroke*0.5)
		}
		return false
	case ToolStep:
		return math.Hypot(pt.X-it.P0.X, pt.Y-it.P0.Y) <= 16.0
	case ToolText:
		fSize := 13 + it.Stroke*2
		estW := max(36.0, float64(len([]rune(it.Text)))*fSize*0.75)
		estH := fSize * 1.45
		return pt.X >= it.P0.X-4 && pt.X <= it.P0.X+estW+4 && pt.Y >= it.P0.Y-4 && pt.Y <= it.P0.Y+estH+4
	case ToolLoupe:
		r := LoupeRadius(it.Stroke)
		if math.Hypot(pt.X-it.P1.X, pt.Y-it.P1.Y) <= r+5 {
			return true
		}
		if math.Hypot(pt.X-it.P0.X, pt.Y-it.P0.Y) <= 12 {
			return true
		}
		return distToSegment(pt, it.P0, it.P1) <= 6
	}
	return false
}

// BeginEdit 尝试点选并开始拖拽已有图元（平移或拉动端点手柄）。
func (c *Canvas) BeginEdit(x, y float64) bool {
	idx, part := c.HitTest(x, y)
	if idx < 0 || part == EditNone {
		c.Selected = -1
		c.editing = false
		return false
	}
	c.Selected = idx
	c.editing = true
	c.editPart = part
	c.editStartX = x
	c.editStartY = y
	c.editOrig = cloneItems(c.items[idx : idx+1])[0]
	c.editMoved = false
	c.preEditSnap = cloneItems(c.items)
	return true
}

// MoveEdit 更新正在拖拽编辑的选中图元。
func (c *Canvas) MoveEdit(x, y float64) bool {
	if !c.editing || c.Selected < 0 || c.Selected >= len(c.items) {
		return false
	}
	dx := x - c.editStartX
	dy := y - c.editStartY
	if !c.editMoved && math.Hypot(dx, dy) >= 1.0 {
		c.editMoved = true
	}
	it := cloneItems([]Item{c.editOrig})[0]
	switch c.editPart {
	case EditP0:
		it.P0 = PointF{X: c.editOrig.P0.X + dx, Y: c.editOrig.P0.Y + dy}
	case EditP1:
		it.P1 = PointF{X: c.editOrig.P1.X + dx, Y: c.editOrig.P1.Y + dy}
	case EditMove:
		it.P0 = PointF{X: c.editOrig.P0.X + dx, Y: c.editOrig.P0.Y + dy}
		it.P1 = PointF{X: c.editOrig.P1.X + dx, Y: c.editOrig.P1.Y + dy}
		for i := range it.Points {
			it.Points[i].X += dx
			it.Points[i].Y += dy
		}
	}
	c.items[c.Selected] = it
	return true
}

// EndEdit 结束图元拖拽编辑，若发生实际位移则写入撤销栈。
func (c *Canvas) EndEdit() bool {
	if !c.editing {
		return false
	}
	moved := c.editMoved
	c.editing = false
	if moved && c.preEditSnap != nil {
		if len(c.undo) >= maxHistoryDepth {
			c.undo = c.undo[1:]
		}
		c.undo = append(c.undo, c.preEditSnap)
		c.redo = nil
	}
	c.preEditSnap = nil
	return moved
}

// BeginPointer 在当前工具下启动一次新标注绘制。ToolStep 单击即落点。
func (c *Canvas) BeginPointer(x, y float64) bool {
	pt := PointF{X: x, Y: y}
	c.Selected = -1
	switch c.Tool {
	case ToolNone, ToolText:
		return false
	case ToolStep:
		c.pushUndo()
		c.items = append(c.items, Item{
			Tool:   ToolStep,
			Color:  c.Color,
			Stroke: c.Stroke,
			P0:     pt,
			P1:     pt,
			Step:   c.NextStep(),
		})
		c.Selected = len(c.items) - 1
		return true
	case ToolPen:
		c.drawing = &Item{
			Tool:   ToolPen,
			Color:  c.Color,
			Stroke: max(10, c.Stroke*4),
			P0:     pt,
			P1:     pt,
			Points: []PointF{pt},
		}
		return true
	case ToolLoupe:
		zoom := c.LoupeZoom
		if zoom <= 0 {
			zoom = 2.5
		}
		c.drawing = &Item{
			Tool:   ToolLoupe,
			Color:  c.Color,
			Stroke: c.Stroke,
			P0:     pt,
			P1:     pt,
			Zoom:   zoom,
		}
		return true
	default:
		c.drawing = &Item{
			Tool:   c.Tool,
			Color:  c.Color,
			Stroke: c.Stroke,
			P0:     pt,
			P1:     pt,
		}
		return true
	}
}

// MovePointer 更新正在拖拽的新图元终点或轨迹。
func (c *Canvas) MovePointer(x, y float64) bool {
	if c.drawing == nil {
		return false
	}
	pt := PointF{X: x, Y: y}
	c.drawing.P1 = pt
	if c.drawing.Tool == ToolPen {
		last := c.drawing.Points[len(c.drawing.Points)-1]
		if math.Hypot(pt.X-last.X, pt.Y-last.Y) >= 1.5 {
			c.drawing.Points = append(c.drawing.Points, pt)
		}
	}
	return true
}

// EndPointer 结束新图元拖拽并提交到图层。
func (c *Canvas) EndPointer(x, y float64) bool {
	if c.drawing == nil {
		return false
	}
	c.MovePointer(x, y)
	it := *c.drawing
	c.drawing = nil
	switch it.Tool {
	case ToolPen:
		if len(it.Points) == 1 {
			it.Points = append(it.Points, PointF{X: it.P0.X + 0.5, Y: it.P0.Y})
		}
	case ToolLoupe:
		// 拖拽距离小于 12 DIP 时原地居中放置放大镜；拉出更远则形成带引线的特写气泡。
		if math.Hypot(it.P1.X-it.P0.X, it.P1.Y-it.P0.Y) < 12 {
			it.P1 = it.P0
		}
		if it.Zoom <= 0 {
			it.Zoom = 2.5
		}
	default:
		if math.Hypot(it.P1.X-it.P0.X, it.P1.Y-it.P0.Y) < 2 {
			return false
		}
	}
	c.pushUndo()
	c.items = append(c.items, it)
	return true
}

// CancelPointer 放弃未完成的拖拽操作。
func (c *Canvas) CancelPointer() {
	c.drawing = nil
	if c.editing && c.preEditSnap != nil {
		c.items = c.preEditSnap
		c.preEditSnap = nil
	}
	c.editing = false
}

// AddText 提交一条文字批注。
func (c *Canvas) AddText(x, y float64, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	c.pushUndo()
	c.items = append(c.items, Item{
		Tool:   ToolText,
		Color:  c.Color,
		Stroke: c.Stroke,
		P0:     PointF{X: x, Y: y},
		P1:     PointF{X: x, Y: y},
		Text:   text,
	})
	c.Selected = len(c.items) - 1
	return true
}

// AddRedactions 批量添加无痕擦除或马赛克脱敏区域（合并为单步撤销，供「一键敏感信息脱敏」使用）。
func (c *Canvas) AddRedactions(boxes []RectF, useErase bool) int {
	if len(boxes) == 0 {
		return 0
	}
	tool := ToolErase
	if !useErase {
		tool = ToolMosaic
	}
	c.pushUndo()
	added := 0
	for _, b := range boxes {
		if b.W < 2 || b.H < 2 {
			continue
		}
		c.items = append(c.items, Item{
			Tool:   tool,
			Color:  c.Color,
			Stroke: c.Stroke,
			P0:     PointF{X: b.X, Y: b.Y},
			P1:     PointF{X: b.X + b.W, Y: b.Y + b.H},
		})
		added++
	}
	return added
}
