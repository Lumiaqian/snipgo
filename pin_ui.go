package main

import (
	"fmt"
	"image"
	"math"
	"snipgo/internal/beautify"
	"snipgo/internal/pinops"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func (a *app) recognizePin(p *pinWin) {
	if p == nil || p.img == nil {
		return
	}
	var anchor image.Rectangle
	if p.win != nil && !p.win.IsDestroyed() {
		b := p.win.Bounds()
		anchor = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	}
	a.RecognizeImageAt(p.img, anchor)
}

func (a *app) paintPinHoverBar(c *ui.Context, p *pinWin) {
	pal := uiPalette(c)
	tb := behaviors.Toolbar(c.Key("pin-hover-toolbar"), behaviors.ToolbarOptions{
		Label: "贴图工具栏",
	})
	bar := liquidGlassCapsule(
		c,
		tb.Root.Absolute().
			Right(8).Top(8).
			Height(32).
			Padding(0, 6).
			Gap(2).
			AlignItems(ui.Center).
			ZIndex(5),
		16,
		false,
	)
	_ = bar.Clicked()
	bar.Children(func() {
		// 1. 原生文件拖出手柄：按住直接将贴图作为 .png 文件拖进微信/Slack/浏览器/Finder！
		if len(p.png) > 0 {
			dragBtn := ui.Row(c).
				Label("拖出文件").
				Tooltip("按住直接拖出为 PNG 文件（可拖入微信 / Slack / 浏览器）").
				Size(24, 24).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Radius(12).
				Shrink(0).
				Cursor(ui.CursorGrab)
			dragBtn.DragDataFrom(func() transfer.Data {
				// 拖出瞬间才导出，编辑中也包含未完成的标注
				return tempPNGDragData(p.exportPNG(), "SnipGo-Pin")
			})
			if dragBtn.Hovered() {
				liquidSecondaryChrome(c, dragBtn, true, 12)
			}
			dragBtn.Children(func() {
				ui.Icon(c, iconDragFile).FontSize(13.5).TextColor(pal.Accent)
			})
		}
		// 2. 贴图原地二次矢量标注 / 一键主体抠图 / 套模板 / 像素对比
		if pinMiniIconBtn(c, iconEdit, "原地标注", "原地编辑标注 (E)") {
			p.togglePinEdit()
		}
		if pinMiniIconBtn(c, iconTemplate, "套模板", "套模板（便签 / 拍立得 / 浏览器…）(B)") {
			a.openPinTemplate(p)
		}
		if pinMiniIconBtn(c, iconCutout, "主体抠图", "离线 AI 主体抠图去背景 (K)") && !p.cutting {
			a.cutoutPinInPlace(p)
		}
		if len(a.pins) >= 2 && pinMiniIconBtn(c, iconCompare, "拉帘对比", "拉帘对比 / 像素 Diff (D)") {
			a.togglePinCompare(p)
		}
		if pinMiniIconBtn(c, iconOCR, "识图", "识图提取文字 (O)") {
			a.recognizePin(p)
		}
		if pinMiniIconBtn(c, iconGhost, "点击穿透", "鼠标点击穿透 (P)") {
			go mygo.RunOnMain(func() { a.togglePinPassthrough(p) })
		}
		if pinMiniIconBtn(c, iconCollapse, "折叠胶囊", "折叠为胶囊 (M)") {
			go mygo.RunOnMain(func() { p.toggleCollapsed() })
		}
		if len(p.paletteColors) > 0 && pinMiniIconBtn(c, iconPalette, "复制 CSS", "复制色板 CSS 变量") {
			a.copyPaletteCSS(p)
		}
		if len(p.png) > 0 && pinMiniIconBtn(c, iconCopy, "复制贴图", "复制图像 (⌘C)") {
			pngCopy := p.exportPNG()
			if mygo.App.IsReady() {
				go mygo.RunOnMain(func() { _ = mygo.Clipboard.WriteImage(pngCopy) })
			}
			p.setPinNotice("已复制图像")
		}
		if pinMiniIconBtn(c, iconClose, "关闭贴图", "关闭贴图 (Esc)") {
			go mygo.RunOnMain(func() {
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Close()
				}
			})
		}
	})
}

func (a *app) paintCollapsedPill(c *ui.Context, p *pinWin) {
	pal := uiPalette(c)
	pill := liquidGlassCapsule(
		c,
		ui.Row(c).
			Size(collapsedPillW, collapsedPillH).
			Padding(0, 12).
			Gap(6).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			DragWindow(),
		16,
		true,
	).Border(1, pal.PanelRim)
	if pill.WindowDoubleClicked() {
		go mygo.RunOnMain(func() { p.toggleCollapsed() })
	}
	pill.Children(func() {
		ui.Text(c, fmt.Sprintf("贴图 %d×%d", p.origW, p.origH)).
			FontSize(11.5).
			FontFeatures("tnum").
			TextColor(pal.Text)
		if miniPillBtn(c, nil, "展开", false) {
			go mygo.RunOnMain(func() { p.toggleCollapsed() })
		}
	})
}

func (p *pinWin) toggleCollapsed() {
	if p.win == nil || p.win.IsDestroyed() {
		return
	}
	b := p.win.Bounds()
	p.collapsed = !p.collapsed
	if p.collapsed {
		p.rulerMode = false
		p.win.SetBounds(mygo.Rectangle{X: b.X, Y: b.Y, Width: collapsedPillW, Height: collapsedPillH})
	} else {
		p.win.SetBounds(mygo.Rectangle{X: b.X, Y: b.Y, Width: p.curW, Height: p.curH})
	}
	p.win.Invalidate()
	p.refreshShadowSoon()
}

func (a *app) togglePinPassthrough(p *pinWin) {
	if p == nil || p.win == nil || p.win.IsDestroyed() {
		return
	}
	p.clickThrough = !p.clickThrough
	if p.clickThrough {
		p.prevOpacity = p.opacity
		if p.opacity > 65 {
			p.opacity = 55
			p.win.SetOpacity(0.55)
		}
		p.win.SetIgnoreMouseEvents(true)
	} else {
		p.win.SetIgnoreMouseEvents(false)
		if p.prevOpacity > 0 {
			p.opacity = p.prevOpacity
			p.win.SetOpacity(float64(p.opacity) / 100)
		}
	}
	p.win.Invalidate()
	a.refreshMenu()
}

func (a *app) passthroughPinCount() int {
	n := 0
	for _, p := range a.pins {
		if p.clickThrough {
			n++
		}
	}
	return n
}

func (a *app) restorePassthroughPins() {
	for _, p := range a.pins {
		if p.clickThrough && p.win != nil && !p.win.IsDestroyed() {
			p.clickThrough = false
			p.win.SetIgnoreMouseEvents(false)
			if p.prevOpacity > 0 {
				p.opacity = p.prevOpacity
				p.win.SetOpacity(float64(p.opacity) / 100)
			}
			p.win.Invalidate()
		}
	}
	a.refreshMenu()
}

func (a *app) openPinTemplate(p *pinWin) {
	if p == nil || p.img == nil {
		return
	}
	var anchor image.Rectangle
	if p.win != nil && !p.win.IsDestroyed() {
		b := p.win.Bounds()
		anchor = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	} else {
		anchor = image.Rect(0, 0, p.origW, p.origH)
	}
	imgCopy := p.img
	go mygo.RunOnMain(func() {
		a.openTemplateWindow(imgCopy, anchor)
	})
}

func (p *pinWin) applyZoom(targetZoom, anchorX, anchorY float64) {
	if p.win == nil || p.win.IsDestroyed() || p.collapsed {
		return
	}
	// 长图贴图初始缩放可能低于 0.2，下限取当前值以免滚轮时跳变
	nextZoom := max(min(0.2, p.zoom), min(5.0, targetZoom))
	newW := max(24, int(math.Round(float64(p.origW)*nextZoom)))
	newH := max(24, int(math.Round(float64(p.origH)*nextZoom)))
	if newW == p.curW && newH == p.curH {
		return
	}
	b := p.win.Bounds()
	rx := anchorX / float64(max(1, p.curW))
	ry := anchorY / float64(max(1, p.curH))
	newX := b.X + int(math.Round(anchorX-rx*float64(newW)))
	newY := b.Y + int(math.Round(anchorY-ry*float64(newH)))
	p.zoom = nextZoom
	p.curW = newW
	p.curH = newH
	p.win.SetBounds(mygo.Rectangle{X: newX, Y: newY, Width: newW, Height: newH})
	p.win.Invalidate()
	p.refreshShadowSoon()
}

func (a *app) popupPinMenu(p *pinWin) {
	if p == nil || p.win == nil || p.win.IsDestroyed() {
		return
	}
	opacityItems := make([]*mygo.MenuItem, 0, 4)
	for _, pct := range []int{100, 80, 55, 30} {
		pct := pct
		opacityItems = append(opacityItems, &mygo.MenuItem{
			Label:   fmt.Sprintf("%d%%", pct),
			Type:    mygo.MenuItemCheckbox,
			Checked: p.opacity == pct,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if p.win != nil && !p.win.IsDestroyed() {
					p.opacity = pct
					p.win.SetOpacity(float64(pct) / 100)
				}
			},
		})
	}
	zoomPct := int(math.Round(p.zoom * 100))
	moreItems := []*mygo.MenuItem{
		{
			Label:       "像素测量尺",
			Accelerator: "R",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.rulerMode,
			Disabled:    p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.rulerMode = !p.rulerMode
				if p.win != nil && !p.win.IsDestroyed() {
					p.win.Invalidate()
				}
			},
		},
		{
			Label:       "套模板（便签 / 拍立得 / 浏览器…）",
			Accelerator: "B",
			Disabled:    p.img == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.openPinTemplate(p)
			},
		},
		{
			Label:    "恢复原始尺寸 (100%)",
			Badge:    fmt.Sprintf("%d%%", zoomPct),
			Disabled: zoomPct == 100 || p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.applyZoom(1.0, float64(p.curW)/2, float64(p.curH)/2)
			},
		},
	}

	var items []*mygo.MenuItem
	if len(p.paletteColors) > 0 {
		fmtSub := make([]*mygo.MenuItem, 0, 4)
		for _, f := range []beautify.ColorFormat{beautify.ColorHEX, beautify.ColorRGB, beautify.ColorOKLCH, beautify.ColorTailwind} {
			f := f
			fmtSub = append(fmtSub, &mygo.MenuItem{
				Label:   beautify.ColorFormatName(f),
				Type:    mygo.MenuItemCheckbox,
				Checked: p.paletteFmt == f,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					p.paletteFmt = f
					p.setPinNotice("格式: " + beautify.ColorFormatName(f))
				},
			})
		}
		items = append(items,
			&mygo.MenuItem{
				Label: "复制色板 CSS 变量",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.copyPaletteCSS(p)
				},
			},
			&mygo.MenuItem{
				Label:   "点击复制格式",
				Submenu: fmtSub,
			},
			mygo.Separator(),
		)
	}

	baseItems := []*mygo.MenuItem{
		{
			Label:       "复制图像",
			Accelerator: "Cmd+C",
			Disabled:    len(p.png) == 0,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				_ = mygo.Clipboard.WriteImage(p.exportPNG())
				p.setPinNotice("已复制图像")
			},
		},
		{
			Label:       "原地编辑标注",
			Accelerator: "E",
			Disabled:    p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.togglePinEdit()
			},
		},
		{
			Label:       "离线 AI 主体抠图（去背景）",
			Accelerator: "K",
			Disabled:    p.collapsed || p.img == nil || p.cutting,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.cutoutPinInPlace(p)
			},
		},
		{
			Label:       "与相邻贴图拉帘对比 / 像素 Diff",
			Accelerator: "D",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.compareMode,
			Disabled:    p.collapsed || (!p.compareMode && len(a.pins) < 2),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.togglePinCompare(p)
			},
		},
		{
			Label:       "识图提取文字",
			Accelerator: "O",
			Disabled:    p.img == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.recognizePin(p)
			},
		},
		{
			Label:       "保存为 PNG...",
			Accelerator: "Cmd+S",
			Disabled:    len(p.png) == 0,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.SavePNG(p.exportPNG())
			},
		},
		mygo.Separator(),
		{
			Label:       "鼠标穿透比对",
			Accelerator: "P",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.clickThrough,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.togglePinPassthrough(p)
			},
		},
		{
			Label:       "折叠为迷你胶囊",
			Accelerator: "M",
			Type:        mygo.MenuItemCheckbox,
			Checked:     p.collapsed,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.toggleCollapsed()
			},
		},
		{
			Label:   "工具与缩放",
			Submenu: moreItems,
		},
		{
			Label:   "不透明度",
			Submenu: opacityItems,
		},
		mygo.Separator(),
		{
			Label:       "关闭",
			Accelerator: "Esc",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if p.win != nil {
					p.win.Close()
				}
			},
		},
	}
	items = append(items, baseItems...)
	if len(a.pins) > 1 {
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{
				Label: "横向拼接所有贴图为一张图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.mergeAllPins(true) },
			},
			&mygo.MenuItem{
				Label: "纵向拼接所有贴图为一张图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.mergeAllPins(false) },
			},
			&mygo.MenuItem{
				Label: "关闭全部贴图",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.closeAllPins() },
			},
		)
	}
	mygo.NewMenu(items).Popup(p.win)
}

// snapPinWindow 在拖拽移动贴图时自动与相邻贴图的四边/中线进行 8px 磁吸对齐。
func (a *app) snapPinWindow(p *pinWin) {
	if a.snappingPin || p == nil || p.win == nil || p.win.IsDestroyed() || len(a.pins) < 2 {
		return
	}
	b := p.win.Bounds()
	moving := image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	others := make([]image.Rectangle, 0, len(a.pins)-1)
	for _, q := range a.pins {
		if q == p || q.win == nil || q.win.IsDestroyed() {
			continue
		}
		qb := q.win.Bounds()
		others = append(others, image.Rect(qb.X, qb.Y, qb.X+qb.Width, qb.Y+qb.Height))
	}
	snapped, sx, sy := pinops.SnapBounds(moving, others, 8)
	if !sx && !sy {
		return
	}
	a.snappingPin = true
	p.win.SetBounds(mygo.Rectangle{X: snapped.Min.X, Y: snapped.Min.Y, Width: b.Width, Height: b.Height})
	a.snappingPin = false
}
