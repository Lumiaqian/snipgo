package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/store"
)

const (
	historyWinW = 540
	historyWinH = 440
)

type historyWindow struct {
	win         *mygo.Window
	entries     []store.Entry
	thumbs      map[string]*ui.Bitmap
	statusBadge string
}

// tempPNGDragData 将内存 PNG 写入临时目录并返回原生文件拖拽载荷（供贴图直接拖入微信/Slack/浏览器）。
func tempPNGDragData(pngBytes []byte, prefix string) transfer.Data {
	if len(pngBytes) == 0 {
		return transfer.Data{}
	}
	dir := filepath.Join(os.TempDir(), "snipgo-drag")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("%s-%s.png", prefix, time.Now().Format("20060102-150405.000"))
	full := filepath.Join(dir, name)
	if os.WriteFile(full, pngBytes, 0o644) != nil {
		return transfer.Data{}
	}
	d, err := transfer.FileData(full)
	if err != nil {
		return transfer.Data{}
	}
	return d
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
		CornerRadius:    12,
		Panel:           true,
		AlwaysOnTop:     true,
		DisableMinimize: true,
		DisableMaximize: true,
		MinWidth:        460,
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
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if hw.win != nil {
			go mygo.RunOnMain(func() { a.closeHistoryWindow() })
		}
		return
	}

	card := ui.Column(c).
		Fill().
		Radius(12).
		Background(ui.RGBA(22, 23, 28, 0.97)).
		Border(1, ui.RGBA(255, 255, 255, 0.13))

	card.Children(func() {
		// 标题栏
		header := ui.Row(c).
			Height(46).
			Padding(0, 14).
			Gap(8).
			AlignItems(ui.Center).
			DragWindow()
		header.Children(func() {
			ui.Icon(c, iconHistory).FontSize(15).TextColor(ui.RGB(64, 172, 255))
			ui.Text(c, fmt.Sprintf("最近截图历史（%d）", len(hw.entries))).
				FontSize(13).FontWeight(600).NoWrap().TextColor(ui.RGB(244, 245, 248))
			if hw.statusBadge != "" {
				ui.Row(c).Height(20).Padding(0, 8).AlignItems(ui.Center).Radius(10).
					Background(ui.RGBA(46, 204, 113, 0.18)).
					Children(func() {
						ui.Text(c, hw.statusBadge).FontSize(11).NoWrap().TextColor(ui.RGB(118, 236, 166))
					})
			}
			ui.Spacer(c)
			if len(hw.entries) > 0 {
				if miniPillBtn(c, iconTrash, "清空全部", false) {
					if a.history != nil {
						a.history.Clear()
						a.reloadHistoryWindow()
						a.refreshMenu()
					}
				}
			}
			if iconToolBtn(c, iconClose, "关闭历史", "关闭 (Esc)", false, false) {
				if hw.win != nil {
					go mygo.RunOnMain(func() { a.closeHistoryWindow() })
				}
			}
		})
		ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))

		// 双列滚动缩略图网格
		if len(hw.entries) == 0 {
			ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, iconHistory).FontSize(26).TextColor(ui.RGBA(255, 255, 255, 0.25))
				ui.Text(c, "暂无截图记录").FontSize(13.5).TextColor(ui.RGBA(255, 255, 255, 0.72))
				ui.Text(c, "完成复制、贴图或保存后会自动归档到这里（最多保留 20 张）").
					FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.42))
			})
			return
		}

		scroller := ui.Scroll(c).Grow(1).Padding(12, 14)
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

func (a *app) paintHistoryCard(c *ui.Context, hw *historyWindow, e store.Entry) {
	bmp := hw.thumbs[e.ID]
	itemCard := ui.Column(c).
		Grow(1).
		Padding(8, 8).
		Gap(7).
		Radius(9).
		Background(ui.RGBA(32, 34, 42, 0.88)).
		Border(1, ui.RGBA(255, 255, 255, 0.09))

	itemCard.Children(func() {
		// 缩略图区域：直接按住即可作为原生 PNG 文件拖进微信/Slack/浏览器！
		thumbBox := ui.Box(c).
			Height(116).
			Radius(6).
			Clip().
			Background(ui.RGBA(14, 15, 19, 0.92)).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Tooltip("按住可直接将此截图作为 PNG 文件拖入微信 / 浏览器")
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
				ui.Image(c, bmp).Height(116)
			}
		})

		// 尺寸与时间戳
		metaRow := ui.Row(c).AlignItems(ui.Center).Gap(6)
		metaRow.Children(func() {
			ui.Text(c, fmt.Sprintf("%d×%d", e.Width, e.Height)).
				FontSize(11).FontFeatures("tnum").NoWrap().TextColor(ui.RGBA(232, 236, 244, 0.88))
			ui.Text(c, e.CreatedAt.Format("15:04:05")).
				FontSize(10.5).FontFeatures("tnum").NoWrap().TextColor(ui.RGBA(255, 255, 255, 0.42))
			ui.Spacer(c)
			if pinMiniIconBtn(c, iconCopy, "复制到剪贴板") {
				if _, raw, err := store.Load(e); err == nil {
					_ = mygo.Clipboard.WriteImage(raw)
					hw.statusBadge = "已复制到剪贴板"
				}
			}
			if pinMiniIconBtn(c, iconPin, "转为悬浮贴图") {
				if img, raw, err := store.Load(e); err == nil {
					dipW := max(80, e.Width/2)
					dipH := max(60, e.Height/2)
					cur := mygo.Screen.CursorScreenPoint()
					at := image.Pt(max(24, cur.X-dipW/2), max(40, cur.Y-dipH/2))
					go mygo.RunOnMain(func() { a.ShowPin(img, raw, at, dipW, dipH) })
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
