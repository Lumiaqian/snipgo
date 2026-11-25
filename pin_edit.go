package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"snipgo/internal/annotate"
	"snipgo/internal/measure"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo/ui"
)

func (p *pinWin) togglePinEdit() {
	if p.editMode {
		p.finishPinEdit()
		return
	}
	p.rulerMode = false
	p.editMode = true
	if p.annot.Tool == annotate.ToolNone && !p.annot.HasItems() {
		p.annot.Tool = annotate.ToolArrow
	}
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Focus()
		p.win.Invalidate()
	}
}

func (p *pinWin) commitOrClosePinTextEdit() {
	if !p.textEditing {
		return
	}
	if p.textEditIdx >= 0 {
		p.annot.UpdateText(p.textEditIdx, p.textEditBuf)
	} else if p.textEditBuf != "" {
		p.annot.AddText(p.textEditX, p.textEditY, p.textEditBuf)
	}
	p.textEditing = false
	p.textEditIdx = -1
	p.textEditBuf = ""
}

func (p *pinWin) finishPinEdit() {
	p.commitOrClosePinTextEdit()
	p.editMode = false
	p.annotDrag = false
	p.annotEditDrg = false
	p.annot.CancelPointer()
	p.annot.ClearSelection()
	p.flattenEdits()
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

// flattenEdits 把 baseImg 与当前标注合成为 img/bmp/png，不退出编辑模式。
func (p *pinWin) flattenEdits() {
	if p.baseImg == nil {
		return
	}
	items := p.annot.Items()
	if len(items) == 0 {
		p.img = p.baseImg
		p.bmp = p.baseBmp
	} else {
		b := p.baseImg.Bounds()
		merged := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(merged, merged.Bounds(), p.baseImg, b.Min, draw.Src)
		annotate.Render(merged, image.Rect(0, 0, p.origW, p.origH), items)
		p.img = merged
		p.bmp = ui.NewBitmap(merged)
	}
	var buf bytes.Buffer
	if png.Encode(&buf, p.img) == nil {
		p.png = buf.Bytes()
	}
}

// exportPNG 返回用于复制/保存/拖出的 PNG；编辑模式下先合成进行中的标注。
func (p *pinWin) exportPNG() []byte {
	if p.editMode {
		p.flattenEdits()
	}
	return p.png
}

func (p *pinWin) paintPinEditLayer(c *ui.Context, w, h float32, hovered bool) {
	z := float32(max(0.05, p.zoom))
	items := p.annot.Items()
	selIdx := p.annot.Selected
	if d := p.annot.Drawing(); d != nil {
		items = append(items, *d)
	}
	var hoverLoupe *annotate.Item
	if hovered && p.hasHover && p.annot.Tool == annotate.ToolLoupe && !p.annotDrag && !p.annotEditDrg {
		ox := max(0, min(float64(p.origW), p.hoverX/float64(z)))
		oy := max(0, min(float64(p.origH), p.hoverY/float64(z)))
		if idx, _ := p.annot.HitTest(ox, oy); !(idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe) {
			zoom := p.annot.LoupeZoom
			if zoom <= 0 {
				zoom = 2.5
			}
			hoverLoupe = &annotate.Item{
				Tool:   annotate.ToolLoupe,
				Color:  p.annot.Color,
				Stroke: p.annot.Stroke,
				P0:     annotate.PointF{X: ox, Y: oy},
				P1:     annotate.PointF{X: ox, Y: oy},
				Zoom:   zoom,
			}
		}
	}
	if len(items) > 0 || hoverLoupe != nil {
		src := canvasPreviewSource{
			bmp:  p.baseBmp,
			img:  p.baseImg,
			rect: image.Rect(0, 0, p.origW, p.origH),
		}
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				paintCanvasAnnotations(pt, r, items, selIdx, 0, 0, r.X, r.Y, z, src, hoverLoupe)
			})
	}

	if p.textEditing {
		paintAnnotTextEditor(
			c,
			"pin-text-editor",
			"pin-annot-text-input",
			float32(p.textEditX*float64(z)),
			float32(p.textEditY*float64(z)),
			w,
			h,
			p.annot.Color,
			&p.textEditBuf,
			false,
			func() {
				p.commitOrClosePinTextEdit()
				if p.win != nil {
					p.win.Invalidate()
				}
			},
		)
	}

	if p.annot.Tool != annotate.ToolNone || p.annot.Selected >= 0 {
		subTB := behaviors.Toolbar(c.Key("pin-edit-style-toolbar"), behaviors.ToolbarOptions{
			Label: "贴图样式栏",
		})
		sub := liquidGlassCapsule(
			c,
			subTB.Root.Absolute().
				Right(8).Bottom(50).
				Height(32).
				Padding(0, 8).
				Gap(6).
				AlignItems(ui.Center).
				ZIndex(8),
			16,
			false,
		)
		_ = sub.Clicked()
		sub.Children(func() {
			paintAnnotStyleControls(
				c,
				subTB,
				p.annot,
				p.annot.Selected >= 0,
				"pin-palette",
				920,
				"删除贴图标注",
				"pin-loupe-zoom",
				func() {
					if p.win != nil {
						p.win.Invalidate()
					}
				},
			)
		})
	}

	// 底部内嵌紧凑标注工具栏
	tb := behaviors.Toolbar(c.Key("pin-edit-toolbar"), behaviors.ToolbarOptions{
		Label: "贴图标注栏",
	})
	bar := liquidGlassCapsule(
		c,
		tb.Root.Absolute().
			Right(8).Bottom(8).
			Height(36).
			Padding(0, 6).
			Gap(2).
			AlignItems(ui.Center).
			ZIndex(8),
		18,
		false,
	)
	_ = bar.Clicked()
	bar.Children(func() {
		for i, t := range [...]struct {
			ic    *ui.SVG
			label string
			tip   string
			tool  annotate.Tool
		}{
			{iconRect, "矩形", "矩形 (1)", annotate.ToolRect},
			{iconEllipse, "椭圆", "椭圆 (2)", annotate.ToolEllipse},
			{iconArrow, "箭头", "箭头 (3)", annotate.ToolArrow},
			{iconHighlighter, "荧光笔", "荧光笔 (4)", annotate.ToolPen},
			{iconStep, "序号", "序号 (5)", annotate.ToolStep},
			{iconMosaic, "马赛克", "马赛克 (6)", annotate.ToolMosaic},
			{iconText, "文字", "文字批注 (7)", annotate.ToolText},
			{iconSpotlight, "聚光", "聚光灯高亮 (8)", annotate.ToolSpotlight},
			{iconErase, "无痕擦除", "无痕擦除 (9)", annotate.ToolErase},
			{iconLoupe, "放大镜特写", "放大镜特写 (0)", annotate.ToolLoupe},
		} {
			active := p.annot.Tool == t.tool
			if iconToolBtn(c.Key(900+i), t.ic, t.label, t.tip, active, false) {
				p.commitOrClosePinTextEdit()
				if active {
					p.annot.Tool = annotate.ToolNone
				} else {
					p.annot.Tool = t.tool
				}
				p.annot.ClearSelection()
				if p.win != nil {
					p.win.Invalidate()
				}
			}
		}
		canUndo := p.annot.CanUndo()
		if iconToolBtn(c, iconUndo, "撤销贴图标注", "撤销 (⌘Z)", false, !canUndo) && canUndo {
			p.commitOrClosePinTextEdit()
			p.annot.Undo()
			if p.win != nil {
				p.win.Invalidate()
			}
		}
		if miniPillBtn(c, iconCheck, "完成", true) {
			p.finishPinEdit()
		}
	})
}

func (p *pinWin) sampleRuler() (measure.Span, bool) {
	if p.img == nil || !p.hasHover || p.curW <= 0 || p.curH <= 0 {
		return measure.Span{}, false
	}
	ib := p.img.Bounds()
	px := ib.Min.X + int(math.Floor(p.hoverX*float64(ib.Dx())/float64(p.curW)))
	py := ib.Min.Y + int(math.Floor(p.hoverY*float64(ib.Dy())/float64(p.curH)))
	return measure.Cast(p.img, px, py, measure.DefaultTolerance)
}

func (p *pinWin) paintPinRuler(c *ui.Context, w, h float32) {
	sp, ok := p.sampleRuler()
	if !ok {
		return
	}
	ib := p.img.Bounds()
	sx := float32(p.curW) / float32(max(1, ib.Dx()))
	sy := float32(p.curH) / float32(max(1, ib.Dy()))
	cx := float32(p.hoverX)
	cy := float32(p.hoverY)
	x0 := float32(sp.Left-ib.Min.X) * sx
	x1 := float32(sp.Right+1-ib.Min.X) * sx
	y0 := float32(sp.Top-ib.Min.Y) * sy
	y1 := float32(sp.Bottom+1-ib.Min.Y) * sy

	ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).IgnoreInput(true).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			col := ui.RGB(255, 69, 58)
			pt.Line(r.X+x0, r.Y+cy, r.X+x1, r.Y+cy, 1.5, col)
			pt.Line(r.X+cx, r.Y+y0, r.X+cx, r.Y+y1, 1.5, col)
			pt.Line(r.X+x0, r.Y+cy-4, r.X+x0, r.Y+cy+4, 1.5, col)
			pt.Line(r.X+x1, r.Y+cy-4, r.X+x1, r.Y+cy+4, 1.5, col)
			pt.Line(r.X+cx-4, r.Y+y0, r.X+cx+4, r.Y+y0, 1.5, col)
			pt.Line(r.X+cx-4, r.Y+y1, r.X+cx+4, r.Y+y1, 1.5, col)
		})

	txt := fmt.Sprintf("↔ %d px  ↕ %d px", sp.WidthPx(), sp.HeightPx())
	bx := max(float32(4), min(cx+12, w-148))
	by := max(float32(4), min(cy+12, h-28))
	pal := uiPalette(c)
	liquidGlassCapsule(
		c,
		ui.Box(c).Absolute().Left(bx).Top(by).
			Padding(4, 9).
			IgnoreInput(true),
		12,
		false,
	).Children(func() {
		ui.Text(c, txt).FontSize(11).FontFeatures("tnum").TextColor(pal.Text)
	})
}
