package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
)

const (
	settingsWinW = 480
	settingsWinH = 460
)

type settingsWindow struct {
	win   *mygo.Window
	draft config.Settings
	saved string
}

type countdownSession struct {
	win       *mygo.Window
	cancel    context.CancelFunc
	remaining int
}

func (a *app) openSettingsWindow() {
	if a.setWin != nil && a.setWin.win != nil && !a.setWin.win.IsDestroyed() {
		a.setWin.win.Focus()
		return
	}
	sw := &settingsWindow{
		draft: a.cfg,
	}
	a.setWin = sw
	pos := a.placeCenteredPanel(settingsWinW, settingsWinH)
	sw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "SnipGo 偏好设置",
		Width:           settingsWinW,
		Height:          settingsWinH,
		X:               pos.X,
		Y:               pos.Y,
		Frameless:       true,
		Transparent:     true,
		CornerRadius:    12,
		Panel:           true,
		AlwaysOnTop:     true,
		DisableResize:   true,
		DisableMinimize: true,
		DisableMaximize: true,
		Content: ui.View(func(c *ui.Context) {
			a.paintSettingsView(c, sw)
		}),
	})
	sw.win.OnClosed(func() {
		if a.setWin == sw {
			a.setWin = nil
		}
	})
	sw.win.Focus()
}

func (a *app) closeSettingsWindow() {
	if a.setWin != nil {
		old := a.setWin
		a.setWin = nil
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
	}
}

func (a *app) applyAndSaveSettings(next config.Settings) {
	next.Normalize(a.picturesDir)
	if next.OpenAtLogin != a.cfg.OpenAtLogin {
		_ = mygo.App.SetOpenAtLogin(next.OpenAtLogin)
	}
	a.cfg = next
	if a.userDataDir != "" {
		_ = config.Save(a.userDataDir, a.cfg)
	}
	a.registerAllShortcuts()
	a.refreshMenu()
}

func (a *app) paintSettingsView(c *ui.Context, sw *settingsWindow) {
	c.Root().Background(ui.Transparent)
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if sw.win != nil {
			go mygo.RunOnMain(func() { a.closeSettingsWindow() })
		}
		return
	}

	card := ui.Column(c).
		Fill().
		Radius(12).
		Background(ui.RGBA(22, 23, 28, 0.98)).
		Border(1, ui.RGBA(255, 255, 255, 0.13)).
		TextColor(ui.RGB(238, 240, 246))

	card.Children(func() {
		header := ui.Row(c).
			Height(46).
			Padding(0, 14).
			Gap(8).
			AlignItems(ui.Center).
			DragWindow()
		header.Children(func() {
			ui.Icon(c, iconSettings).FontSize(15).TextColor(ui.RGB(64, 172, 255))
			ui.Text(c, "SnipGo 偏好设置").FontSize(13.5).FontWeight(600).NoWrap().TextColor(ui.RGB(244, 245, 248))
			if sw.saved != "" {
				ui.Row(c).Height(20).Padding(0, 8).AlignItems(ui.Center).Radius(10).
					Background(ui.RGBA(46, 204, 113, 0.18)).
					Children(func() {
						ui.Text(c, sw.saved).FontSize(11).NoWrap().TextColor(ui.RGB(118, 236, 166))
					})
			}
			ui.Spacer(c)
			if iconToolBtn(c, iconClose, "关闭设置", "关闭 (Esc)", false, false) {
				if sw.win != nil {
					go mygo.RunOnMain(func() { a.closeSettingsWindow() })
				}
			}
		})
		ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))

		body := ui.Scroll(c).Grow(1).Padding(12, 16)
		body.Children(func() {
			col := ui.Column(c).Gap(14)
			col.Children(func() {
				// 分组 1：保存与通用工作流
				sectionTitle(c, "快存与通用工作流")
				sec1 := settingsCard(c)
				sec1.Children(func() {
					ui.Checkbox(c, &sw.draft.QuickSave, "按 ⌘S 免弹窗一键快存到默认文件夹（⇧⌘S 另存为）").TextColor(ui.RGB(236, 238, 245))
					ui.Checkbox(c, &sw.draft.CopyOnSave, "保存 PNG 文件时自动同时复制图像到剪贴板").TextColor(ui.RGB(236, 238, 245))
					ui.Checkbox(c, &sw.draft.OpenAtLogin, "登录 macOS 时自动启动 SnipGo").TextColor(ui.RGB(236, 238, 245))

					dirRow := ui.Row(c).AlignItems(ui.Center).Gap(6)
					dirRow.Children(func() {
						ui.Icon(c, iconFolder).FontSize(14).TextColor(ui.RGBA(255, 255, 255, 0.6))
						ui.Text(c, sw.draft.QuickSaveDir).FontSize(11.5).MaxLines(1).Ellipsis("…").Grow(1).
							TextColor(ui.RGBA(220, 224, 234, 0.88))
						if miniPillBtn(c, nil, "更改目录…", false) {
							curDir := sw.draft.QuickSaveDir
							go func() {
								dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
									Title:             "选择默认快存目录",
									DefaultPath:       curDir,
									Directory:         true,
									CreateDirectories: true,
								})
								if err == nil && len(dirs) > 0 && dirs[0] != "" {
									mygo.RunOnMain(func() {
										sw.draft.QuickSaveDir = dirs[0]
										if sw.win != nil && !sw.win.IsDestroyed() {
											sw.win.Invalidate()
										}
									})
								}
							}()
						}
					})

					delayRow := ui.Row(c).AlignItems(ui.Center).Gap(8)
					delayRow.Children(func() {
						ui.Text(c, "延时定格倒计时：").FontSize(12).TextColor(ui.RGBA(230, 234, 242, 0.85))
						for i, sec := range []int{3, 5, 10} {
							sec := sec
							active := sw.draft.DelaySeconds == sec
							if miniPillBtn(c.Key(700+i), nil, fmt.Sprintf("%d 秒", sec), active) {
								sw.draft.DelaySeconds = sec
							}
						}
					})
				})

				// 分组 2：全局快捷键
				sectionTitle(c, "全局快捷键")
				sec2 := settingsCard(c)
				sec2.Children(func() {
					shortcutField(c, "区域截图", &sw.draft.SnipShortcut)
					shortcutField(c, "截图识图 (OCR)", &sw.draft.OCRShortcut)
					shortcutField(c, "定格与像素标尺", &sw.draft.FreezeShortcut)
					shortcutField(c, "从剪贴板贴图", &sw.draft.PinShortcut)
					shortcutField(c, "最近截图历史", &sw.draft.HistoryShortcut)
				})
			})
		})

		ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.08))
		footer := ui.Row(c).Height(46).Padding(0, 14).Gap(8).AlignItems(ui.Center)
		footer.Children(func() {
			if miniPillBtn(c, nil, "恢复默认", false) {
				sw.draft = config.Default(a.picturesDir)
				sw.saved = "已恢复默认（点保存生效）"
			}
			ui.Spacer(c)
			if actionPillBtn(c, iconCheck, "保存并生效", "保存设置并立即更新全局快捷键", true, false) {
				a.applyAndSaveSettings(sw.draft)
				sw.saved = "✓ 设置已生效"
			}
		})
	})
}

func sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(ui.RGBA(160, 172, 192, 0.9))
}

func settingsCard(c *ui.Context) ui.Element {
	return ui.Column(c).
		Padding(10, 12).
		Gap(9).
		Radius(9).
		Background(ui.RGBA(32, 34, 42, 0.78)).
		Border(1, ui.RGBA(255, 255, 255, 0.08))
}

func shortcutField(c *ui.Context, label string, val *string) {
	row := ui.Row(c).Height(28).AlignItems(ui.Center).Gap(10)
	row.Children(func() {
		ui.Text(c, label).Width(130).FontSize(12).NoWrap().TextColor(ui.RGBA(232, 235, 242, 0.9))
		ui.TextInput(c.Key("sc-"+label), val).
			Grow(1).
			Height(26).
			Padding(0, 8).
			Radius(6).
			FontSize(12).
			Background(ui.RGBA(14, 15, 19, 0.85)).
			Border(1, ui.RGBA(255, 255, 255, 0.12)).
			TextColor(ui.RGB(240, 242, 246))
	})
}

// SavePNG 根据偏好设置执行免弹窗一键快存（或弹出另存为对话框），并可选自动同时复制到剪贴板。
func (a *app) SavePNG(data []byte) {
	a.savePNGWithMode(data, !a.cfg.QuickSave)
}

func (a *app) SavePNGAs(data []byte) {
	a.savePNGWithMode(data, true)
}

func (a *app) savePNGWithMode(data []byte, forceDialog bool) {
	if len(data) == 0 {
		return
	}
	if a.cfg.CopyOnSave {
		_ = mygo.Clipboard.WriteImage(data)
	}
	name := "SnipGo-" + time.Now().Format("2006-01-02-150405") + ".png"
	targetDir := a.cfg.QuickSaveDir
	if targetDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			targetDir = filepath.Join(home, "Desktop")
		}
	}

	if !forceDialog && targetDir != "" {
		go func() {
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				a.savePNGDialog(data, filepath.Join(targetDir, name))
				return
			}
			full := filepath.Join(targetDir, name)
			if err := os.WriteFile(full, data, 0o644); err != nil {
				a.savePNGDialog(data, full)
				return
			}
			mygo.RunOnMain(func() {
				a.sessionStatus("已快存: " + name)
			})
		}()
		return
	}
	go a.savePNGDialog(data, filepath.Join(targetDir, name))
}

func (a *app) savePNGDialog(data []byte, defaultPath string) {
	mygo.App.Focus()
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:             "保存截图",
		DefaultPath:       defaultPath,
		CreateDirectories: true,
		Filters: []mygo.FileFilter{
			{Name: "PNG 图像", Extensions: []string{"png"}},
		},
	})
	if err != nil || path == "" {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		mygo.RunOnMain(func() { a.sessionStatus("保存失败: " + err.Error()) })
	}
}

// startDelayedCapture 启动倒计时延时定格截图（方便从容展开二级悬停菜单或 Tooltip）。
func (a *app) startDelayedCapture(seconds int) {
	if seconds <= 0 {
		seconds = a.cfg.DelaySeconds
	}
	if seconds <= 0 {
		seconds = 3
	}
	a.stopCountdown()
	if a.session != nil {
		a.session.Cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cd := &countdownSession{
		cancel:    cancel,
		remaining: seconds,
	}
	a.countdown = cd

	pos := a.placeTopCenterPill(246, 42)
	cd.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           overlayTitle,
		Width:           246,
		Height:          42,
		X:               pos.X,
		Y:               pos.Y,
		Frameless:       true,
		Transparent:     true,
		CornerRadius:    21,
		Panel:           true,
		AlwaysOnTop:     true,
		DisableResize:   true,
		DisableMinimize: true,
		DisableMaximize: true,
		Content: ui.View(func(c *ui.Context) {
			a.paintCountdownView(c, cd)
		}),
	})
	cd.win.SetContentProtection(true)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				done := false
				mygo.RunOnMain(func() {
					if a.countdown != cd {
						done = true
						return
					}
					cd.remaining--
					if cd.remaining <= 0 {
						a.stopCountdown()
						done = true
						// 预留一帧让倒计时胶囊彻底从合成器消失后定格桌面。
						go func() {
							time.Sleep(45 * time.Millisecond)
							mygo.RunOnMain(func() {
								if a.session != nil {
									a.session.StartFrozen()
								}
							})
						}()
						return
					}
					if cd.win != nil && !cd.win.IsDestroyed() {
						cd.win.Invalidate()
					}
				})
				if done {
					return
				}
			}
		}
	}()
}

func (a *app) placeTopCenterPill(w, h int) image.Point {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		return image.Pt(400, 48)
	}
	wa := displays[0].WorkArea
	cur := mygo.Screen.CursorScreenPoint()
	pt := image.Pt(cur.X, cur.Y)
	for _, d := range displays {
		r := image.Rect(d.WorkArea.X, d.WorkArea.Y, d.WorkArea.X+d.WorkArea.Width, d.WorkArea.Y+d.WorkArea.Height)
		if pt.In(r) {
			wa = d.WorkArea
			break
		}
	}
	return image.Pt(wa.X+(wa.Width-w)/2, wa.Y+18)
}

func (a *app) stopCountdown() {
	cd := a.countdown
	if cd == nil {
		return
	}
	a.countdown = nil
	if cd.cancel != nil {
		cd.cancel()
	}
	if cd.win != nil && !cd.win.IsDestroyed() {
		cd.win.Close()
	}
}

func (a *app) paintCountdownView(c *ui.Context, cd *countdownSession) {
	c.Root().Background(ui.Transparent)
	if c.Shortcut(0, ui.KeyEscape) {
		go mygo.RunOnMain(func() { a.stopCountdown() })
		return
	}
	pill := ui.Row(c).
		Fill().
		Padding(0, 14).
		Gap(8).
		AlignItems(ui.Center).
		Radius(21).
		Background(ui.RGBA(20, 21, 26, 0.95)).
		Border(1, ui.RGBA(64, 172, 255, 0.5))
	pill.Children(func() {
		ui.Icon(c, iconTimer).FontSize(15).TextColor(ui.RGB(64, 172, 255))
		ui.Text(c, fmt.Sprintf("%d 秒后定格屏幕…", cd.remaining)).
			FontSize(12.5).FontWeight(600).FontFeatures("tnum").NoWrap().TextColor(ui.RGB(244, 245, 248))
		ui.Spacer(c)
		if miniPillBtn(c, nil, "取消", false) {
			go mygo.RunOnMain(func() { a.stopCountdown() })
		}
	})
}
