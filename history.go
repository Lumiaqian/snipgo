package main

import (
	"fmt"
	"image"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/store"
)

const (
	historyWinW = 580
	historyWinH = 448
)

type historyWindow struct {
	win         *mygo.Window
	entries     []store.Entry
	thumbs      map[string]*ui.Bitmap
	statusBadge string
	badge       badgeState
	enter       panelEnterState
}

func (hw *historyWindow) setStatusBadge(msg string) {
	var fallback *string
	if hw.win == nil {
		fallback = &hw.statusBadge
	}
	hw.badge.Set(msg, hw.win, fallback)
}

func (a *app) recordHistory(pngBytes []byte, pxW, pxH int) {
	if a.history == nil || len(pngBytes) == 0 {
		return
	}
	_, _ = a.history.Add(pngBytes, pxW, pxH)
	mygo.RunOnMain(func() {
		a.refreshMenu()
		if a.histWin != nil {
			a.reloadHistoryWindow()
		}
	})
}

func (a *app) toggleHistoryWindow() {
	if a.histWin != nil && a.histWin.win != nil && !a.histWin.win.IsDestroyed() {
		a.closeHistoryWindow()
		return
	}
	a.openHistoryWindow()
}

func (a *app) closeHistoryWindow() {
	if a.histWin != nil {
		old := a.histWin
		a.histWin = nil
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
	}
}

func (a *app) reloadHistoryWindow() {
	if a.histWin == nil || a.history == nil {
		return
	}
	a.histWin.entries = a.history.List()
	if a.histWin.thumbs == nil {
		a.histWin.thumbs = map[string]*ui.Bitmap{}
	}
	active := map[string]bool{}
	for _, e := range a.histWin.entries {
		active[e.ID] = true
		if _, ok := a.histWin.thumbs[e.ID]; !ok {
			if img, _, err := store.Load(e); err == nil && img != nil {
				a.histWin.thumbs[e.ID] = ui.NewBitmap(img)
			}
		}
	}
	for id := range a.histWin.thumbs {
		if !active[id] {
			delete(a.histWin.thumbs, id)
		}
	}
	if a.histWin.win != nil && !a.histWin.win.IsDestroyed() {
		a.histWin.win.Invalidate()
	}
}

func (a *app) openHistoryWindow() {
	a.closeHistoryWindow()
	hw := &historyWindow{
		thumbs: map[string]*ui.Bitmap{},
		enter:  panelEnterState{enabled: true},
	}
	a.histWin = hw
	a.reloadHistoryWindow()

	pos := a.placeCenteredPanel(historyWinW, historyWinH)
	hw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "SnipGo 截图历史",
		Width:           historyWinW,
		Height:          historyWinH,
		X:               pos.X,
		Y:               pos.Y,
		Frameless:       true,
		Transparent:     true,
		Vibrancy:        vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius:    panelCornerRadius,
		Panel:           false,
		AlwaysOnTop:     true,
		DisableMinimize: true,
		DisableMaximize: true,
		MinWidth:        500,
		MinHeight:       320,
		Content: ui.View(func(c *ui.Context) {
			a.paintHistoryView(c, hw)
		}),
	})
	hw.win.OnClosed(func() {
		if a.histWin == hw {
			a.histWin = nil
		}
	})
	applyWindowTheme(hw.win, themeStateFromConfig(a.cfg))
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	hw.win.Focus()
}

func (a *app) placeCenteredPanel(w, h int) image.Point {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		return image.Pt(160, 140)
	}
	cur := mygo.Screen.CursorScreenPoint()
	pt := image.Pt(cur.X, cur.Y)
	wa := displays[0].WorkArea
	for _, d := range displays {
		r := image.Rect(d.WorkArea.X, d.WorkArea.Y, d.WorkArea.X+d.WorkArea.Width, d.WorkArea.Y+d.WorkArea.Height)
		if pt.In(r) {
			wa = d.WorkArea
			break
		}
	}
	return image.Pt(
		wa.X+max(16, (wa.Width-w)/2),
		wa.Y+max(16, (wa.Height-h)/2),
	)
}

func (a *app) paintHistoryView(c *ui.Context, hw *historyWindow) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.closeHistoryWindow() }) {
			return
		}

		p := uiPalette(c)
		card := liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &hw.enter)

		card.Children(func() {
			paintPanelHeader(c, iconHistory, fmt.Sprintf("最近截图历史（%d）", len(hw.entries)), "关闭历史", "关闭 (⌘W / Esc)", func() {
				if hw.win != nil {
					a.closeHistoryWindow()
				}
			}, func() {
				statusBadgePill(c, hw.badge.Text(hw.statusBadge), true)
				if len(hw.entries) > 0 {
					if miniPillBtn(c, iconTrash, "清空全部", false) {
						if a.history != nil {
							a.history.Clear()
							a.reloadHistoryWindow()
							a.refreshMenu()
						}
					}
				}
			})

			if len(hw.entries) == 0 {
				ui.Column(c).Grow(1).Padding(16, 18, 24, 18).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
					ui.Icon(c, iconHistory).FontSize(26).TextColor(p.TextMuted)
					ui.Text(c, "暂无截图记录").FontSize(13.5).TextColor(p.TextSecondary)
					ui.Text(c, "完成复制、贴图或保存后会自动归档到这里（最多保留 20 张）").
						FontSize(11.5).TextColor(p.TextMuted)
				})
			} else {
				ui.Row(c).Height(22).Padding(0, 18, 4, 18).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "按住缩略图可拖出 PNG · 单击复制 · 双击贴图").FontSize(11).TextColor(p.TextMuted)
				})
				scrollWrap := ui.Box(c).Grow(1).FillWidth().Clip()
				scrollWrap.Children(func() {
					scroller := ui.Scroll(c).Fill().Padding(4, 18, 16, 18).ScrollbarInsets(4, 0, 8)
					scroller.Children(func() {
						colWrap := ui.Column(c).Gap(10)
						colWrap.Children(func() {
							for rowIdx := 0; rowIdx < len(hw.entries); rowIdx += 2 {
								rIdx := rowIdx
								rowEl := ui.Row(c.Key(1000 + rIdx)).Gap(10)
								rowEl.Children(func() {
									a.paintHistoryCard(c.Key(2000+rIdx), hw, hw.entries[rIdx])
									if rIdx+1 < len(hw.entries) {
										a.paintHistoryCard(c.Key(2000+rIdx+1), hw, hw.entries[rIdx+1])
									} else {
										ui.Box(c).Grow(1)
									}
								})
							}
						})
					})
				})
			}
		})
	})
}

func (a *app) paintHistoryCard(c *ui.Context, hw *historyWindow, e store.Entry) {
	p := uiPalette(c)
	bmp := hw.thumbs[e.ID]
	cardCol := ui.Column(c.Key("hc-"+e.ID)).
		Grow(1).
		Padding(8, 8).
		Gap(7).
		Transition(ui.ElementTransition{
			Animation: &springSnappy,
			Enter:     &ui.Motion{Scale: 0.94},
			Exit:      &ui.Motion{Scale: 0.90},
		})
	bgTarget := p.CardBg
	bdTarget := p.CardBorder
	if cardCol.Hovered() {
		bgTarget = p.CardBgHover
		bdTarget = p.ActiveBorder
	}
	itemCard := cardCol.
		Radius(cardCornerRadius).
		Background(cardCol.AnimateColor("hc-bg", bgTarget, colorMorph)).
		Border(1, cardCol.AnimateColor("hc-bd", bdTarget, colorMorph)).
		InsetShadow(0, 1, 0, 0, p.CardTopRim)

	itemCard.Children(func() {
		// 缩略图区域：拖拽导 PNG、单击复制、双击直接转为贴图
		thumbBox := ui.Box(c).
			Height(116).
			Radius(10).
			Clip().
			Background(p.StageBg).
			Border(1, p.CardBorder).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Cursor(ui.CursorPointer).
			Tooltip("拖出 PNG · 单击复制 · 双击贴图")
		if e.FilePath != "" {
			fpath := e.FilePath
			thumbBox.DragDataFrom(func() transfer.Data {
				d, err := transfer.FileData(fpath)
				if err != nil {
					return transfer.Data{}
				}
				return d
			})
		}
		thumbBox.Children(func() {
			if bmp != nil {
				ui.Image(c, bmp).Height(116).Fit(ui.Contain)
			}
		})
		if thumbBox.DoubleClicked() {
			if img, raw, err := store.Load(e); err == nil {
				dipW := max(80, e.Width/2)
				dipH := max(60, e.Height/2)
				cur := mygo.Screen.CursorScreenPoint()
				at := image.Pt(max(24, cur.X-dipW/2), max(40, cur.Y-dipH/2))
				go mygo.RunOnMain(func() { a.ShowPin(img, raw, at, dipW, dipH) })
				hw.setStatusBadge("已转为悬浮贴图")
			}
		} else if thumbBox.Clicked() {
			if _, raw, err := store.Load(e); err == nil {
				_ = mygo.Clipboard.WriteImage(raw)
				hw.setStatusBadge("已复制到剪贴板")
			}
		}

		// 尺寸与时间戳
		metaRow := ui.Row(c).AlignItems(ui.Center).Gap(3)
		metaRow.Children(func() {
			ui.Text(c, fmt.Sprintf("%d×%d", e.Width, e.Height)).
				FontSize(11).FontFeatures("tnum").NoWrap().TextColor(p.Text)
			ui.Text(c, e.CreatedAt.Format("15:04")).
				FontSize(10.5).FontFeatures("tnum").NoWrap().TextColor(p.TextMuted)
			ui.Spacer(c)
			if pinMiniIconBtn(c, iconCopy, "复制到剪贴板") {
				if _, raw, err := store.Load(e); err == nil {
					_ = mygo.Clipboard.WriteImage(raw)
					hw.setStatusBadge("已复制到剪贴板")
				}
			}
			if pinMiniIconBtn(c, iconPin, "转为悬浮贴图") {
				if img, raw, err := store.Load(e); err == nil {
					dipW := max(80, e.Width/2)
					dipH := max(60, e.Height/2)
					cur := mygo.Screen.CursorScreenPoint()
					at := image.Pt(max(24, cur.X-dipW/2), max(40, cur.Y-dipH/2))
					go mygo.RunOnMain(func() { a.ShowPin(img, raw, at, dipW, dipH) })
					hw.setStatusBadge("已转为悬浮贴图")
				}
			}
			if pinMiniIconBtn(c, iconTemplate, "套模板（便签 / 拍立得 / 浏览器…）") {
				if img, _, err := store.Load(e); err == nil {
					go mygo.RunOnMain(func() { a.openTemplateWindow(img, image.Rectangle{}) })
				}
			}
			if pinMiniIconBtn(c, iconOCR, "识图提取文字") {
				if img, _, err := store.Load(e); err == nil {
					go mygo.RunOnMain(func() { a.RecognizeImage(img) })
				}
			}
			if pinMiniIconBtn(c, iconTrash, "删除此记录") {
				if a.history != nil {
					a.history.Delete(e.ID)
					a.reloadHistoryWindow()
					a.refreshMenu()
				}
			}
		})
	})
}
