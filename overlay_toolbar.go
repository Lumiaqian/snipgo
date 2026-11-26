package main

import (
	"fmt"
	"image"
	"math"
	"snipgo/internal/annotate"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo/ui"
)

func (a *app) paintToolbar(c *ui.Context, o *overlay) func() {
	sel, locked, ok := a.session.ActiveRect()
	if !ok || !locked || a.session.Dragging() || a.session.AnnotatingDrag() {
		return nil
	}
	anchorPt := image.Pt(sel.Max.X-1, sel.Max.Y-1)
	if !a.ownsPoint(o, anchorPt) {
		return nil
	}
	win := image.Rect(0, 0, o.bounds.Dx(), o.bounds.Dy())
	_, border := dimBands(win, localSelection(o.bounds, sel))
	if border.Empty() {
		return nil
	}

	ac := a.session.Annotations()
	hasSelected := ac.Selected >= 0
	showStyleControls := ac.Tool != annotate.ToolNone || hasSelected
	showSubBar := showStyleControls
	const (
		mainBarH = 40
		subBarH  = 38
		estBarW  = 740
		edgeGap  = 8
	)
	totalH := mainBarH
	if showSubBar {
		totalH = mainBarH + 6 + subBarH
	}
	pos := toolbarTopLeft(win, border, estBarW, totalH)
	anchorRight := border.Max.X >= min(win.Max.X-edgeGap, estBarW+edgeGap*2)

	var chosen func()
	wrap := ui.Column(c.Key("overlay-toolbar-wrap")).Absolute().
		Top(float32(pos.Y)).
		ZIndex(10).
		Gap(6)
	if anchorRight {
		wrap.Right(float32(max(edgeGap, win.Max.X-border.Max.X))).AlignItems(ui.End)
	} else {
		leftX := max(edgeGap, min(border.Min.X, win.Max.X-estBarW-edgeGap))
		wrap.Left(float32(leftX)).AlignItems(ui.Start)
	}

	nativeGlass := o.bitmap == nil
	wrap.Children(func() {
		mainTB := behaviors.Toolbar(c.Key("overlay-main-toolbar"), behaviors.ToolbarOptions{
			Label: "标注工具栏",
		})
		bar := liquidGlassCapsule(
			c,
			mainTB.Root.
				Height(mainBarH).
				Padding(4, 8).
				Gap(1.5).
				AlignItems(ui.Center).
				Shrink(0),
			20,
			nativeGlass,
		)
		_ = bar.Clicked()
		bar.Children(func() {
			tools := [...]struct {
				label string
				tip   string
				icon  *ui.SVG
				tool  annotate.Tool
			}{
				{"框", "矩形框 (1)", iconRect, annotate.ToolRect},
				{"圆", "椭圆框 (2)", iconEllipse, annotate.ToolEllipse},
				{"箭头", "箭头指示 (3)", iconArrow, annotate.ToolArrow},
				{"荧光", "荧光马克笔 (4)", iconHighlighter, annotate.ToolPen},
				{"序号", "自动步骤序号 (5)", iconStep, annotate.ToolStep},
				{"打码", "像素马赛克 (6)", iconMosaic, annotate.ToolMosaic},
				{"擦除", "无痕擦除文字 (9)", iconErase, annotate.ToolErase},
				{"聚光", "聚光灯高亮 (8)", iconSpotlight, annotate.ToolSpotlight},
				{"文字", "文字批注 (7)", iconText, annotate.ToolText},
				{"放大", "局部放大镜特写 (0)", iconLoupe, annotate.ToolLoupe},
			}
			for i, t := range tools {
				if iconToolBtn(c.Key(100+i), t.icon, t.label, t.tip, ac.Tool == t.tool, false) {
					a.commitOrCloseTextEdit()
					ac.ClearSelection()
					if ac.Tool == t.tool {
						ac.Tool = annotate.ToolNone
					} else {
						ac.Tool = t.tool
					}
					a.invalidateAll()
				}
			}
			// 撤销槽位保持固定 30×30 宽度，防止绘制首笔后整个工具栏发生水平跳动。
			canUndo := ac.CanUndo()
			if iconToolBtn(c, iconUndo, "撤销", "撤销上一笔 (⌘Z)", false, !canUndo) && canUndo {
				ac.Undo()
				a.invalidateAll()
			}

			toolbarDivider(c)

			if iconToolBtn(c, iconShield, "脱敏", "自动识别并遮盖 API Key / 手机 / 邮箱 / IP (X)", false, a.redacting) && !a.redacting {
				a.commitOrCloseTextEdit()
				a.autoRedactSelection()
			}
			if iconToolBtn(c, iconCutout, "抠图", "离线 AI 主体抠图并贴为异形贴纸 (K)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Cutout
			}
			if a.session.CanWindowOnly() {
				if iconToolBtn(c, iconWindowOnly, "单窗", "纯净单窗抠图（去壁纸遮挡）(W)", a.session.WindowOnly(), false) {
					a.session.ToggleWindowOnly()
					a.invalidateAll()
				}
			}
			if iconToolBtn(c, iconTemplate, "模板", "套用便签 / 拍立得 / 浏览器 / 分享卡等模板 (B)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Template
			}
			if !a.session.IsLive() {
				if iconToolBtn(c, iconRuler, "标尺", "像素间距测量尺 (R)", a.session.MeasureMode(), false) {
					a.session.ToggleMeasure()
					a.invalidateAll()
				}
			}
			if iconToolBtn(c, iconScrollCap, "长图", "滚动长截图 (L)", false, false) {
				a.commitOrCloseTextEdit()
				selCopy := sel
				chosen = func() { a.startScrollCapture(selCopy) }
			}

			toolbarDivider(c)

			if actionPillBtn(c, iconOCR, "识图", "提取图中文字与二维码 (O)", false, true) {
				a.commitOrCloseTextEdit()
				chosen = a.session.OCR
			}
			if actionPillBtn(c, iconPin, "贴图", "钉在桌面悬浮贴图 (T)", false, true) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Pin
			}
			if iconToolBtn(c, iconSave, "保存", "保存为 PNG (⌘S)", false, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Save
			}
			if iconToolBtn(c, iconClose, "取消", "取消 (Esc)", false, false) {
				chosen = a.session.Cancel
			}
			if actionPillBtn(c, iconCheck, "复制", "复制到剪贴板 (Enter)", true, false) {
				a.commitOrCloseTextEdit()
				chosen = a.session.Copy
			}
		})

		if showSubBar {
			subTB := behaviors.Toolbar(c.Key("overlay-style-toolbar"), behaviors.ToolbarOptions{
				Label: "标注样式栏",
			})
			sub := liquidGlassCapsule(
				c,
				subTB.Root.
					Height(subBarH).
					Padding(4, 10).
					Gap(7).
					AlignItems(ui.Center).
					Shrink(0),
				19,
				nativeGlass,
			)
			_ = sub.Clicked()
			sub.Children(func() {
				if showStyleControls {
					paintAnnotStyleControls(
						c,
						subTB,
						ac,
						hasSelected,
						"palette",
						200,
						"删除标注",
						"loupe-zoom",
						a.invalidateAll,
					)
				}
			})
		}
	})
	return chosen
}

func paintAnnotStyleControls(
	c *ui.Context,
	tb behaviors.ToolbarParts,
	ac *annotate.Canvas,
	hasSelected bool,
	palettePrefix string,
	strokeKeyBase int,
	delLabel string,
	loupeKey string,
	onChange func(),
) {
	uiPal := uiPalette(c)
	curCol := ac.Color
	curStroke := ac.Stroke
	if selIt, ok := ac.SelectedItem(); ok {
		curCol = selIt.Color
		curStroke = selIt.Stroke
	}
	for i, pal := range annotate.Palette {
		pal := pal
		active := curCol == pal
		dot := tb.RadioItem(
			c.Key(fmt.Sprintf("%s-%d", palettePrefix, i)),
			fmt.Sprintf("颜色 %d", i+1),
			active,
			behaviors.ToolbarItemOptions{
				Tooltip: fmt.Sprintf("颜色 #%02X%02X%02X", pal.R, pal.G, pal.B),
			},
		).
			Size(18, 18).Radius(9).
			Shrink(0).
			Background(ui.RGB(pal.R, pal.G, pal.B)).
			Cursor(ui.CursorPointer)
		if dot.Pressed() {
			dot.Scale(0.92, 0.92)
		}
		if active {
			dot.Border(2, uiPal.Accent).
				InsetShadow(0, 0, 0, 1.5, ui.RGB(255, 255, 255))
		} else {
			dot.Border(1, uiPal.SubtleBorder)
		}
		if dot.Clicked() {
			ac.SetColor(pal)
			if onChange != nil {
				onChange()
			}
		}
	}
	toolbarDivider(c)
	for i, st := range [...]struct {
		label string
		tip   string
		w     float64
		dotR  float32
	}{
		{"细", "细笔画", 2.0, 2.0},
		{"中", "中笔画", 3.0, 3.5},
		{"粗", "粗笔画", 6.0, 5.2},
	} {
		if strokeDotBtn(c.Key(strokeKeyBase+i), st.label, st.tip, st.dotR, math.Abs(curStroke-st.w) < 0.4) {
			ac.SetStroke(st.w)
			if onChange != nil {
				onChange()
			}
		}
	}
	if hasSelected {
		toolbarDivider(c)
		if iconToolBtn(c, iconTrash, delLabel, "删除当前选中的标注 (⌫)", false, false) {
			ac.DeleteSelected()
			if onChange != nil {
				onChange()
			}
		}
	}
	selIt, hasSelIt := ac.SelectedItem()
	if ac.Tool == annotate.ToolLoupe || (hasSelIt && selIt.Tool == annotate.ToolLoupe) {
		toolbarDivider(c)
		curZoom := ac.LoupeZoom
		if hasSelIt && selIt.Tool == annotate.ToolLoupe && selIt.Zoom > 0 {
			curZoom = selIt.Zoom
		}
		zoomVals := [...]float64{2.0, 2.5, 3.0, 4.0}
		zoomSel := 0
		for i, z := range zoomVals {
			if math.Abs(curZoom-z) < 0.15 {
				zoomSel = i
				break
			}
		}
		if picked := compactSegmented(c, loupeKey, zoomSel, []string{"2×", "2.5×", "3×", "4×"}); picked >= 0 && picked != zoomSel {
			ac.SetLoupeZoom(zoomVals[picked])
			if onChange != nil {
				onChange()
			}
		}
	}
}
