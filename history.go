package main

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/store"
)

const (
	historyWinW = 580
	historyWinH = 448
)

// historyThumbMaxSide 历史缩略图长边上限（卡片缩略区高 116pt，2x 屏约 232px，留有余量）。
const historyThumbMaxSide = 320

// 以下变量供测试替换：缩略图加载器与整图加载器。
var (
	historyThumbLoader = store.Thumbnail
	historyFullLoader  = store.Load
)

type historyWindow struct {
	win         *mygo.Window
	entries     []store.Entry
	thumbs      map[string]*ui.Bitmap // 仅主线程访问
	dead        atomic.Bool           // 窗口已关闭/被替换，后台 worker 应尽快退出
	thumbMu     sync.Mutex            // 保护以下 worker 状态
	thumbQueue  []store.Entry
	thumbSeen   map[string]bool // 已入队（含失败）的记录，避免重复加载
	thumbWorker bool
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

// scale 为像素/DIP 比例，0 表示未知（贴图时回退按 2x 估算）。
func (a *app) recordHistory(pngBytes []byte, pxW, pxH int, scale float64) {
	if a.history == nil || len(pngBytes) == 0 || !a.historyEnabled() {
		return
	}
	hist := a.history
	// 写 PNG 与索引放到后台，避免阻塞主线程；store 自带互斥保证并发安全。
	go func() {
		if _, err := hist.Add(pngBytes, pxW, pxH, scale); err != nil {
			return
		}
		runOnMain(func() {
			a.refreshMenu()
			if a.histWin != nil {
				a.reloadHistoryWindow()
			}
		})
	}()
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
		old.dead.Store(true)
		a.histWin = nil
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
	}
}

// reloadHistoryWindow 刷新条目列表（主线程）。缩略图在后台单 worker 中按序加载，不在此处解码。
func (a *app) reloadHistoryWindow() {
	hw := a.histWin
	if hw == nil || a.history == nil {
		return
	}
	hw.entries = a.history.List()
	if hw.thumbs == nil {
		hw.thumbs = map[string]*ui.Bitmap{}
	}
	active := map[string]bool{}
	var missing []store.Entry
	for _, e := range hw.entries {
		active[e.ID] = true
		if _, ok := hw.thumbs[e.ID]; !ok {
			missing = append(missing, e)
		}
	}
	for id := range hw.thumbs {
		if !active[id] {
			delete(hw.thumbs, id)
		}
	}
	a.enqueueHistoryThumbs(hw, missing)
	if hw.win != nil && !hw.win.IsDestroyed() {
		hw.win.Invalidate()
	}
}

// enqueueHistoryThumbs 把缺失缩略图的记录加入队列，并确保只有一个后台 worker 在处理。
func (a *app) enqueueHistoryThumbs(hw *historyWindow, entries []store.Entry) {
	hw.thumbMu.Lock()
	if hw.thumbSeen == nil {
		hw.thumbSeen = map[string]bool{}
	}
	for _, e := range entries {
		if !hw.thumbSeen[e.ID] {
			hw.thumbSeen[e.ID] = true
			hw.thumbQueue = append(hw.thumbQueue, e)
		}
	}
	start := !hw.thumbWorker && len(hw.thumbQueue) > 0
	if start {
		hw.thumbWorker = true
	}
	hw.thumbMu.Unlock()
	if start {
		go a.runHistoryThumbWorker(hw)
	}
}

func (a *app) runHistoryThumbWorker(hw *historyWindow) {
	for {
		hw.thumbMu.Lock()
		if hw.dead.Load() || len(hw.thumbQueue) == 0 {
			hw.thumbWorker = false
			hw.thumbQueue = nil
			hw.thumbMu.Unlock()
			return
		}
		e := hw.thumbQueue[0]
		hw.thumbQueue = hw.thumbQueue[1:]
		hw.thumbMu.Unlock()

		thumb, err := historyThumbLoader(e, historyThumbMaxSide)
		if err != nil || thumb == nil {
			continue
		}
		runOnMain(func() {
			// 窗口已关闭/重建，或记录已被删除时丢弃结果。
			if a.histWin != hw {
				return
			}
			for _, cur := range hw.entries {
				if cur.ID == e.ID {
					hw.thumbs[e.ID] = ui.NewBitmap(thumb)
					if hw.win != nil && !hw.win.IsDestroyed() {
						hw.win.Invalidate()
					}
					return
				}
			}
		})
	}
}

// historyDecodeAsync 在后台完整解码某条历史记录，再回到主线程交给 use（用于贴图/套模板/OCR）。
func (a *app) historyDecodeAsync(hw *historyWindow, e store.Entry, use func(img *image.NRGBA, raw []byte)) {
	go func() {
		img, raw, err := historyFullLoader(e)
		if err != nil || img == nil {
			return
		}
		runOnMain(func() { use(img, raw) })
	}()
}

// historyCopy 只读 PNG 字节复制到剪贴板，不解码。
func (hw *historyWindow) historyCopy(e store.Entry) {
	if raw, err := store.ReadPNG(e); err == nil {
		_ = mygo.Clipboard.WriteImage(raw)
		hw.setStatusBadge("已复制到剪贴板")
	}
}

// historyPin 后台解码后转为悬浮贴图；落点在点击时（主线程）确定。
func (a *app) historyPin(hw *historyWindow, e store.Entry) {
	ew, eh := e.DipSize()
	dipW := max(80, ew)
	dipH := max(60, eh)
	at := pinPlacementNearCursorOnScreen(dipW, dipH)
	a.historyDecodeAsync(hw, e, func(img *image.NRGBA, raw []byte) {
		a.ShowPin(img, raw, at, dipW, dipH)
		hw.setStatusBadge("已转为悬浮贴图")
	})
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
		hw.dead.Store(true)
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
									a.paintHistoryCard(c.Key(2000+rIdx), hw, rIdx+1, hw.entries[rIdx])
									if rIdx+1 < len(hw.entries) {
										a.paintHistoryCard(c.Key(2000+rIdx+1), hw, rIdx+2, hw.entries[rIdx+1])
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

func (a *app) paintHistoryCard(c *ui.Context, hw *historyWindow, idx int, e store.Entry) {
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
		thumbBox := ui.ButtonBase(c.Key("hc-thumb-"+e.ID)).
			Label(fmt.Sprintf("历史截图 %d", idx)).
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
			a.historyPin(hw, e)
		} else if thumbBox.Clicked() {
			hw.historyCopy(e)
		}

		// 尺寸与时间戳
		metaRow := ui.Row(c).AlignItems(ui.Center).Gap(3)
		metaRow.Children(func() {
			ui.Text(c, fmt.Sprintf("%d×%d", e.Width, e.Height)).
				FontSize(11).FontFeatures("tnum").NoWrap().TextColor(p.Text)
			ui.Text(c, e.CreatedAt.Format("15:04")).
				FontSize(10.5).FontFeatures("tnum").NoWrap().TextColor(p.TextMuted)
			ui.Spacer(c)
			if pinMiniIconBtn(c, iconCopy, fmt.Sprintf("复制历史 %d", idx), "复制到剪贴板") {
				hw.historyCopy(e)
			}
			if pinMiniIconBtn(c, iconPin, fmt.Sprintf("贴图历史 %d", idx), "转为悬浮贴图") {
				a.historyPin(hw, e)
			}
			if pinMiniIconBtn(c, iconTemplate, fmt.Sprintf("套模板历史 %d", idx), "套模板（便签 / 拍立得 / 浏览器…）") {
				a.historyDecodeAsync(hw, e, func(img *image.NRGBA, _ []byte) {
					a.openTemplateWindow(img, image.Rectangle{})
				})
			}
			if pinMiniIconBtn(c, iconOCR, fmt.Sprintf("识图历史 %d", idx), "识图提取文字") {
				a.historyDecodeAsync(hw, e, func(img *image.NRGBA, _ []byte) {
					a.RecognizeImage(img)
				})
			}
			if pinMiniIconBtn(c, iconTrash, fmt.Sprintf("删除历史 %d", idx), "删除此记录") {
				if a.history != nil {
					a.history.Delete(e.ID)
					a.reloadHistoryWindow()
					a.refreshMenu()
				}
			}
		})
	})
}
