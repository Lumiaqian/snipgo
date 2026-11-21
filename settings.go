package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
)

const (
	settingsWinW = 500
	settingsWinH = 448
)

type settingsWindow struct {
	win           *mygo.Window
	draft         config.Settings
	tab           int
	saved         string
	badge         badgeState
	enter         panelEnterState
	recordingID   string
	recordingMods ui.Modifiers
	recordingErr  string
}

type countdownSession struct {
	win       *mygo.Window
	cancel    context.CancelFunc
	remaining int
}

type shortcutItemDef struct {
	id    string
	label string
	ptr   func(*config.Settings) *string
}

var shortcutDefs = []shortcutItemDef{
	{id: "snip", label: "区域截图", ptr: func(s *config.Settings) *string { return &s.SnipShortcut }},
	{id: "ocr", label: "截图识图 (OCR)", ptr: func(s *config.Settings) *string { return &s.OCRShortcut }},
	{id: "freeze", label: "定格与像素标尺", ptr: func(s *config.Settings) *string { return &s.FreezeShortcut }},
	{id: "delay", label: "延时定格截图", ptr: func(s *config.Settings) *string { return &s.DelayShortcut }},
	{id: "pin", label: "从剪贴板贴图", ptr: func(s *config.Settings) *string { return &s.PinShortcut }},
	{id: "history", label: "最近截图历史", ptr: func(s *config.Settings) *string { return &s.HistoryShortcut }},
}

func (a *app) openSettingsWindow() {
	if a.setWin != nil && a.setWin.win != nil && !a.setWin.win.IsDestroyed() {
		if mygo.App.IsReady() {
			mygo.App.Focus()
		}
		a.setWin.win.Focus()
		return
	}
	sw := &settingsWindow{
		draft: a.cfg,
		enter: panelEnterState{enabled: true},
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
		Vibrancy:        vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius:    panelCornerRadius,
		Panel:           false,
		AlwaysOnTop:     true,
		DisableResize:   true,
		DisableMinimize: true,
		DisableMaximize: true,
		Content: ui.View(func(c *ui.Context) {
			a.paintSettingsView(c, sw)
		}),
	})
	sw.win.OnClosed(func() {
		wasRecording := sw.recordingID != ""
		sw.recordingID = ""
		sw.recordingMods = 0
		if a.setWin == sw {
			a.setWin = nil
			a.applyThemeLive(a.cfg)
		}
		if wasRecording {
			a.registerAllShortcuts()
		}
	})
	applyWindowTheme(sw.win, themeStateFromConfig(a.cfg))
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	sw.win.Focus()
}

func (a *app) closeSettingsWindow() {
	if a.setWin != nil {
		old := a.setWin
		a.setWin = nil
		wasRecording := old.recordingID != ""
		old.recordingID = ""
		old.recordingMods = 0
		if old.win != nil && !old.win.IsDestroyed() {
			old.win.Close()
		}
		if wasRecording {
			a.registerAllShortcuts()
		}
		a.applyThemeLive(a.cfg)
	}
}

func (a *app) startShortcutRecording(sw *settingsWindow, id string) {
	sw.recordingID = id
	sw.recordingMods = 0
	sw.recordingErr = ""
	if mygo.App.IsReady() {
		mygo.GlobalShortcut.UnregisterAll()
		mygo.App.Focus()
	}
	if sw.win != nil && !sw.win.IsDestroyed() {
		sw.win.Focus()
		sw.win.Invalidate()
	}
}

func (a *app) stopShortcutRecording(sw *settingsWindow, restoreHotkeys bool) {
	sw.recordingID = ""
	sw.recordingMods = 0
	if restoreHotkeys {
		a.registerAllShortcuts()
	}
	if sw.win != nil && !sw.win.IsDestroyed() {
		sw.win.Invalidate()
	}
}

func (a *app) applyAndSaveSettings(next config.Settings) {
	next.Normalize(a.picturesDir)
	if next.OpenAtLogin != a.cfg.OpenAtLogin && mygo.App.IsReady() {
		_ = mygo.App.SetOpenAtLogin(next.OpenAtLogin)
	}
	prevAuto := a.cfg.AutoCheckUpdates
	prevPre := a.cfg.IncludePrereleases
	a.cfg = next
	a.applyThemeLive(a.cfg)
	if a.userDataDir != "" {
		_ = config.Save(a.userDataDir, a.cfg)
	}
	a.registerAllShortcuts()
	a.refreshMenu()
	if a.upd != nil && (prevAuto != a.cfg.AutoCheckUpdates || prevPre != a.cfg.IncludePrereleases) {
		a.restartUpdateScheduler()
	}
}

func (sw *settingsWindow) setSavedBadge(msg string) {
	var fallback *string
	if sw.win == nil {
		fallback = &sw.saved
	}
	sw.badge.Set(msg, sw.win, fallback)
}

func (a *app) paintSettingsView(c *ui.Context, sw *settingsWindow) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if sw.recordingID == "" && handlePanelWindowShortcuts(c, func() { a.closeSettingsWindow() }) {
			return
		}

		p := uiPalette(c)
		card := liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &sw.enter)
		defCfg := config.Default(a.picturesDir)

		card.Children(func() {
			paintPanelHeader(c, iconSettings, "SnipGo 偏好设置", "关闭设置", "关闭 (⌘W / Esc)", func() {
				if sw.win != nil {
					a.closeSettingsWindow()
				}
			}, func() {
				txt := sw.badge.Text(sw.saved)
				statusBadgePill(c, txt, !strings.Contains(txt, "冲突"))
			})

			ui.Row(c).Padding(0, 18, 12, 18).AlignItems(ui.Center).Children(func() {
				if picked := liquidSegmented(c, "set-tab", sw.tab, []string{"外观与主题", "全局快捷键", "通用与更新"}); picked >= 0 && picked != sw.tab {
					sw.tab = picked
					if sw.recordingID != "" {
						a.stopShortcutRecording(sw, true)
					}
				}
			})

			tabBody := ui.Box(c).Grow(1).FillWidth().Padding(0, 18, 0, 18).Clip()
			tabBody.Children(func() {
				switch sw.tab {
				case 0:
					a.paintSettingsAppearanceTab(c, p, sw)
				case 1:
					a.paintSettingsShortcutTab(c, p, sw, defCfg)
				default:
					a.paintSettingsGeneralTab(c, p, sw)
				}
			})

			footer := ui.Row(c).
				Height(54).
				Padding(0, 18).
				Gap(8).
				AlignItems(ui.Center).
				ZIndex(2)
			footer.Children(func() {
				if actionPillBtn(c, iconUndo, "恢复默认", "恢复所有默认快捷键、主题和通用设置", false, true) {
					sw.recordingID = ""
					sw.recordingMods = 0
					sw.recordingErr = ""
					sw.draft = config.Default(a.picturesDir)
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已恢复默认")
				}
				ui.Spacer(c)
				if actionPillBtn(c, iconCheck, "完成", "保存并关闭设置 (⌘W / Esc)", true, false) {
					a.applyAndSaveSettings(sw.draft)
					if sw.win != nil {
						go mygo.RunOnMain(func() { a.closeSettingsWindow() })
					}
				}
			})
		})
	})
}

func (a *app) paintSettingsAppearanceTab(c *ui.Context, p palette, sw *settingsWindow) {
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		secTheme := settingsCard(c)
		secTheme.Children(func() {
			appModes := []struct{ id, label string }{
				{"system", "跟随系统"},
				{"light", "浅色"},
				{"dark", "深色"},
			}
			appSel := 0
			for i, m := range appModes {
				if sw.draft.ThemeAppearance == m.id {
					appSel = i
					break
				}
			}
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "外观模式").Width(68).FontSize(12).NoWrap().TextColor(p.TextSecondary)
				if picked := liquidSegmented(c, "set-app", appSel, []string{"跟随系统", "浅色", "深色"}); picked >= 0 && appModes[picked].id != sw.draft.ThemeAppearance {
					sw.draft.ThemeAppearance = appModes[picked].id
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已实时生效")
				}
			})

			glassModes := []struct{ id, label string }{
				{"clear", "清透"},
				{"regular", "标准"},
				{"frosted", "浓磨砂"},
			}
			glassSel := 1
			for i, g := range glassModes {
				if sw.draft.GlassLevel == g.id {
					glassSel = i
					break
				}
			}
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "液态玻璃").Width(68).FontSize(12).NoWrap().TextColor(p.TextSecondary)
				if picked := liquidSegmented(c, "set-glass", glassSel, []string{"清透", "标准", "浓磨砂"}); picked >= 0 && glassModes[picked].id != sw.draft.GlassLevel {
					sw.draft.GlassLevel = glassModes[picked].id
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已实时生效")
				}
			})

			accentRow := ui.Row(c).AlignItems(ui.Center).Gap(7)
			accentRow.Children(func() {
				ui.Text(c, "主题色").Width(68).FontSize(12).NoWrap().TextColor(p.TextSecondary)
				autoActive := sw.draft.AccentColor == "" || sw.draft.AccentColor == "system"
				if miniPillBtn(c.Key("acc-system"), nil, "自动", autoActive) && !autoActive {
					sw.draft.AccentColor = "system"
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已实时生效")
				}
				for i, opt := range accentOptions {
					if opt.ID == "system" {
						continue
					}
					opt := opt
					active := sw.draft.AccentColor == opt.ID
					dotCol := opt.Light
					if p.Dark {
						dotCol = opt.Dark
					}
					swBtn := ui.Box(c.Key(840+i)).
						Label(opt.Name).
						Tooltip(opt.Name).
						Size(24, 24).
						Radius(12).
						Shrink(0).
						Padding(2.5, 2.5).
						Cursor(ui.CursorPointer)
					targetScale := float32(1.0)
					if swBtn.Pressed() {
						targetScale = 0.94
					} else if active || swBtn.Hovered() {
						targetScale = 1.08
					}
					sc := swBtn.AnimateTo("acc-pop", targetScale, springPop)
					if sc != 1.0 {
						swBtn.Scale(sc, sc)
					}
					ringCol := ui.Transparent
					if active {
						ringCol = dotCol
					} else if swBtn.Hovered() {
						ringCol = p.TextSecondary
					}
					animRing := swBtn.AnimateColor("acc-ring", ringCol, colorMorph)
					swBtn.Border(1.5, animRing).Children(func() {
						ui.Box(c).Fill().Radius(9).Background(dotCol).Border(1, ui.RGBA(255, 255, 255, 0.38))
					})
					if swBtn.Clicked() && !active {
						sw.draft.AccentColor = opt.ID
						a.applyAndSaveSettings(sw.draft)
						sw.setSavedBadge("✓ 已实时生效")
					}
				}
			})
		})

		sectionTitle(c, "实时外观预览")
		previewCard := settingsCard(c)
		previewCard.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(p.Accent)
				ui.Text(c, "所有浮窗实时同步 Liquid Glass 材质与主题强调色").
					FontSize(12).Grow(1).TextColor(p.TextSecondary)
				statusBadgePill(c, "效果就绪", true)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).IgnoreInput(true).Children(func() {
				_ = actionPillBtn(c, iconOCR, "识图", "", false, true)
				_ = actionPillBtn(c, iconPin, "贴图", "", false, true)
				ui.Spacer(c)
				_ = actionPillBtn(c, iconCheck, "复制选中", "", true, false)
			})
		})
	})
}

func (a *app) paintSettingsShortcutTab(c *ui.Context, p palette, sw *settingsWindow, defCfg config.Settings) {
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		sec2 := settingsCard(c)
		sec2.Children(func() {
			for _, def := range shortcutDefs {
				a.shortcutRecorderRow(c, p, sw, def, defCfg)
			}
			if sw.recordingErr != "" {
				ui.Row(c.Key("sc-err-bar")).
					Padding(6, 10).Radius(10).Gap(6).AlignItems(ui.Center).
					Background(ui.RGBA(235, 68, 60, 0.14)).
					Border(1, ui.RGBA(235, 68, 60, 0.42)).
					Children(func() {
						ui.Text(c, sw.recordingErr).FontSize(11).TextColor(ui.RGB(235, 68, 60))
					})
			}
		})
		ui.Row(c).Padding(0, 4).Children(func() {
			ui.Text(c, "点击右侧按键后直接按下新组合键即可保存 · 按 Esc 取消录制").
				FontSize(11).TextColor(p.TextMuted)
		})
	})
}

func (a *app) paintSettingsGeneralTab(c *ui.Context, p palette, sw *settingsWindow) {
	sc := ui.Scroll(c).Fill().ScrollbarInsets(4, 0, 4)
	sc.Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			sectionTitle(c, "快存与通用工作流")
			sec1 := settingsCard(c)
			sec1.Children(func() {
				if liquidSwitch(c, "quicksave", &sw.draft.QuickSave, "按 ⌘S 免弹窗一键快存到默认文件夹（⇧⌘S 另存为）") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}
				if liquidSwitch(c, "copyonsave", &sw.draft.CopyOnSave, "保存 PNG 文件时自动同时复制图像到剪贴板") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}
				if liquidSwitch(c, "openatlogin", &sw.draft.OpenAtLogin, "登录 macOS 时自动启动 SnipGo") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}

				dirRow := ui.Row(c).AlignItems(ui.Center).Gap(6)
				dirRow.Children(func() {
					ui.Icon(c, iconFolder).FontSize(14).TextColor(p.TextSecondary)
					ui.Text(c, sw.draft.QuickSaveDir).FontSize(11.5).MaxLines(1).Ellipsis("…").Grow(1).
						TextColor(p.TextSecondary)
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
									a.applyAndSaveSettings(sw.draft)
									sw.setSavedBadge("✓ 目录已更新")
									if sw.win != nil && !sw.win.IsDestroyed() {
										sw.win.Invalidate()
									}
								})
							}
						}()
					}
				})

				delayVals := []int{3, 5, 10}
				delaySel := 0
				for i, sec := range delayVals {
					if sw.draft.DelaySeconds == sec {
						delaySel = i
						break
					}
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "延时定格倒计时：").FontSize(12).TextColor(p.TextSecondary)
					ui.Spacer(c)
					if picked := liquidSegmented(c, "set-delay", delaySel, []string{"3 秒", "5 秒", "10 秒"}); picked >= 0 && delayVals[picked] != sw.draft.DelaySeconds {
						sw.draft.DelaySeconds = delayVals[picked]
						a.applyAndSaveSettings(sw.draft)
						sw.setSavedBadge("✓ 已保存")
					}
				})
			})

			sectionTitle(c, "软件更新")
			sec3 := settingsCard(c)
			sec3.Children(func() {
				verRow := ui.Row(c).AlignItems(ui.Center).Gap(8)
				verRow.Children(func() {
					ui.Text(c, "当前版本："+formatVersion(currentAppVersion())).
						FontSize(12).FontWeight(500).FontFeatures("tnum").NoWrap().TextColor(p.Text)
					if a.upd != nil && a.upd.stagedVersion != "" {
						statusBadgePill(c, "可重启更新 "+formatVersion(a.upd.stagedVersion), true)
					} else if rel := a.availableUpdate(); rel != nil {
						statusBadgePill(c, "有新版本 "+formatVersion(rel.Version), false)
					} else if a.upd != nil && a.upd.state == updUpToDate {
						statusBadgePill(c, "已是最新", true)
					} else if sw.draft.AutoCheckUpdates {
						statusBadgePill(c, "自动检测", false)
					}
					ui.Spacer(c)
					if miniPillBtn(c, iconUpdate, "检查更新", false) {
						a.checkForUpdates(true)
					}
				})
				if liquidSwitch(c, "autoupd", &sw.draft.AutoCheckUpdates, "启动时自动检查更新（每 24 小时后台静默检测）") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}
				if liquidSwitch(c, "preupd", &sw.draft.IncludePrereleases, "接收测试预览版（Beta / Pre-release）") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}
				if sw.draft.SkippedVersion != "" {
					skipRow := ui.Row(c).AlignItems(ui.Center).Gap(8)
					skipRow.Children(func() {
						ui.Text(c, "已跳过版本："+formatVersion(sw.draft.SkippedVersion)).
							FontSize(11.5).FontFeatures("tnum").TextColor(p.TextSecondary)
						ui.Spacer(c)
						if miniPillBtn(c, nil, "清除跳过", false) {
							sw.draft.SkippedVersion = ""
							a.applyAndSaveSettings(sw.draft)
							sw.setSavedBadge("✓ 已清除跳过版本")
						}
					})
				}
			})
		})
	})
}

func sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(uiPalette(c).TextSecondary)
}

func settingsCard(c *ui.Context) ui.Element {
	return liquidCard(c, ui.Column(c).Padding(12, 14).Gap(9))
}

func (a *app) shortcutRecorderRow(c *ui.Context, p palette, sw *settingsWindow, def shortcutItemDef, defCfg config.Settings) {
	valPtr := def.ptr(&sw.draft)
	defVal := *def.ptr(&defCfg)
	recording := sw.recordingID == def.id
	regErr := a.shortcutError(def.id)

	row := ui.Row(c.Key("sc-row-" + def.id)).Height(30).AlignItems(ui.Center).Gap(8)
	row.Children(func() {
		ui.Text(c, def.label).Width(134).FontSize(12).NoWrap().TextColor(p.Text)
		if regErr != nil && !recording {
			ui.Text(c, "占用").FontSize(10.5).FontWeight(600).TextColor(ui.RGB(235, 68, 60))
		}
		ui.Spacer(c)

		if recording {
			recBtn := ui.Row(c.Key("sc-rec-"+def.id)).
				Label("录制"+def.label+"快捷键").
				Tooltip("按下新组合键，按 Esc 取消，点击退出录制").
				Height(28).
				Padding(0, 10).
				Gap(5).
				AlignItems(ui.Center).
				Radius(11).
				Background(p.ActiveBg).
				Border(1.5, p.Accent).
				InsetShadow(0, 1, 0, 0, p.CardTopRim).
				Cursor(ui.CursorPointer).
				Focusable().
				AutoFocus()
			recBtn.HandleInput(func(ev ui.InputEvent) bool {
				switch ev.Kind {
				case ui.InputText:
					return true
				case ui.InputKeyUp:
					sw.recordingMods = ev.Mods
					c.Invalidate()
					return true
				case ui.InputKeyDown:
					sw.recordingMods = ev.Mods
					if ev.Mods == 0 && (ev.Key == ui.KeyEscape || ev.PhysicalKey == ui.KeyEscape) {
						sw.recordingErr = ""
						a.stopShortcutRecording(sw, true)
						c.Invalidate()
						return true
					}
					if shortcutKeyToken(ev.PhysicalKey) == "" && shortcutKeyToken(ev.Key) == "" {
						c.Invalidate()
						return true
					}
					acc, ok := formatRecordedShortcut(ev.Mods, ev.Key, ev.PhysicalKey)
					if !ok {
						sw.recordingErr = "请搭配 ⌘ / ⌃ / ⌥ 修饰键或使用 F1–F12"
						sw.saved = ""
						c.Invalidate()
						return true
					}
					if conflict := a.findShortcutConflict(sw.draft, def.id, acc); conflict != "" {
						sw.recordingErr = "与「" + conflict + "」冲突，请换一组"
						sw.saved = ""
						c.Invalidate()
						return true
					}
					*valPtr = acc
					sw.recordingErr = ""
					a.stopShortcutRecording(sw, false)
					a.applyAndSaveSettings(sw.draft)
					if err := a.shortcutError(def.id); err != nil {
						sw.recordingErr = fmt.Sprintf("快捷键 %s 可能已被系统或其他应用占用，请换一组", strings.Join(formatMacShortcutSymbols(acc), ""))
						sw.setSavedBadge("快捷键冲突")
					} else {
						sw.setSavedBadge("✓ 快捷键已生效")
					}
					c.Invalidate()
					return true
				}
				return false
			})
			recBtn.Children(func() {
				if sw.recordingMods != 0 {
					for _, sym := range formatModSymbols(sw.recordingMods) {
						paintKeycap(c, p, sym, true)
					}
					ui.Text(c, "…").FontSize(12).FontWeight(600).TextColor(p.ActiveText)
				} else {
					ui.Text(c, "按下组合键… (Esc 取消)").FontSize(11).FontWeight(500).NoWrap().TextColor(p.ActiveText)
				}
			})
			if recBtn.Clicked() {
				sw.recordingErr = ""
				a.stopShortcutRecording(sw, true)
			}
			return
		}

		if *valPtr != defVal {
			resetBtn := ui.Box(c.Key("sc-rst-"+def.id)).
				Label("重置"+def.label+"快捷键").
				Tooltip("恢复默认："+strings.Join(formatMacShortcutSymbols(defVal), " ")).
				Size(24, 24).
				Radius(12).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Cursor(ui.CursorPointer)
			if resetBtn.Hovered() {
				resetBtn.Background(p.SubtleBtnHover).Border(1, p.SubtleBorder)
			}
			resetBtn.Children(func() {
				ui.Icon(c, iconUndo).FontSize(12).TextColor(p.TextSecondary)
			})
			if resetBtn.Clicked() {
				*valPtr = defVal
				sw.recordingErr = ""
				a.applyAndSaveSettings(sw.draft)
				sw.setSavedBadge("✓ 已恢复默认快捷键")
			}
		}

		idleBtn := ui.Row(c.Key("sc-idle-"+def.id)).
			Label("录制"+def.label).
			Tooltip("点击后直接按下新组合键").
			Height(28).
			Padding(0, 8).
			Gap(4).
			AlignItems(ui.Center).
			Radius(11).
			Cursor(ui.CursorPointer)
		if regErr != nil {
			idleBtn.Background(ui.RGBA(235, 68, 60, 0.14)).Border(1, ui.RGBA(235, 68, 60, 0.45))
		} else {
			bg := p.InputBg
			bd := p.InputBorder
			if idleBtn.Hovered() {
				bg = p.CardBgHover
				bd = p.ActiveBorder
			}
			idleBtn.Background(bg).Border(1, bd)
		}
		idleBtn.Children(func() {
			for _, sym := range formatMacShortcutSymbols(*valPtr) {
				paintKeycap(c, p, sym, false)
			}
		})
		if idleBtn.Clicked() {
			a.startShortcutRecording(sw, def.id)
		}
	})
}

func paintKeycap(c *ui.Context, p palette, label string, active bool) {
	bg := p.CardBg
	bd := p.SubtleBorder
	fg := p.Text
	if active {
		bg = p.ActiveBg
		bd = p.ActiveBorder
		fg = p.ActiveText
	}
	ui.Row(c).
		MinWidth(20).
		Height(20).
		Padding(0, 6).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Radius(6).
		Background(bg).
		Border(1, bd).
		InsetShadow(0, 1, 0, 0, p.CardTopRim).
		Children(func() {
			ui.Text(c, label).FontSize(11).FontWeight(600).NoWrap().TextColor(fg)
		})
}

func (a *app) findShortcutConflict(cfg config.Settings, selfID, candidate string) string {
	want := canonicalShortcutKey(candidate)
	if want == "" {
		return ""
	}
	for _, def := range shortcutDefs {
		if def.id == selfID {
			continue
		}
		if canonicalShortcutKey(*def.ptr(&cfg)) == want {
			return def.label
		}
	}
	if canonicalShortcutKey(restoreShortcut) == want {
		return "恢复刚关闭的贴图"
	}
	if canonicalShortcutKey(unghostShortcut) == want {
		return "恢复穿透贴图交互"
	}
	return ""
}

func formatRecordedShortcut(mods ui.Modifiers, key, physical ui.Key) (string, bool) {
	tok := shortcutKeyToken(physical)
	if tok == "" {
		tok = shortcutKeyToken(key)
	}
	if tok == "" {
		return "", false
	}
	isFnKey := isFunctionKeyToken(tok)
	if !isFnKey && mods&(ui.Cmd|ui.Ctrl|ui.Alt) == 0 {
		return "", false
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Cmd != 0 {
		parts = append(parts, "Cmd")
	}
	parts = append(parts, tok)
	return strings.Join(parts, "+"), true
}

func isFunctionKeyToken(tok string) bool {
	if len(tok) < 2 || tok[0] != 'F' {
		return false
	}
	var n int
	_, err := fmt.Sscanf(tok[1:], "%d", &n)
	return err == nil && n >= 1 && n <= 12 && fmt.Sprintf("F%d", n) == tok
}

func shortcutKeyToken(k ui.Key) string {
	if k >= ui.KeyA && k <= ui.KeyZ {
		return string('A' + byte(k-ui.KeyA))
	}
	if k >= ui.Key0 && k <= ui.Key9 {
		return string('0' + byte(k-ui.Key0))
	}
	if k >= ui.KeyF1 && k <= ui.KeyF12 {
		return fmt.Sprintf("F%d", int(k-ui.KeyF1)+1)
	}
	switch k {
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyTab:
		return "Tab"
	case ui.KeySpace:
		return "Space"
	case ui.KeyBackspace:
		return "Backspace"
	case ui.KeyDelete:
		return "Delete"
	case ui.KeyInsert:
		return "Insert"
	case ui.KeyHome:
		return "Home"
	case ui.KeyEnd:
		return "End"
	case ui.KeyPageUp:
		return "PageUp"
	case ui.KeyPageDown:
		return "PageDown"
	case ui.KeyLeft:
		return "Left"
	case ui.KeyRight:
		return "Right"
	case ui.KeyUp:
		return "Up"
	case ui.KeyDown:
		return "Down"
	case ui.KeyMinus:
		return "-"
	case ui.KeyEqual:
		return "="
	case ui.KeyComma:
		return ","
	case ui.KeyPeriod:
		return "."
	case ui.KeySlash:
		return "/"
	case ui.KeySemicolon:
		return ";"
	case ui.KeyQuote:
		return "'"
	case ui.KeyBracketLeft:
		return "["
	case ui.KeyBracketRight:
		return "]"
	case ui.KeyBackslash:
		return "\\"
	case ui.KeyBackquote:
		return "`"
	default:
		return ""
	}
}

func splitAcceleratorParts(acc string) ([]string, string) {
	s := strings.TrimSpace(acc)
	if s == "" {
		return nil, ""
	}
	var parts []string
	if strings.HasSuffix(s, "++") {
		raw := strings.Split(s[:len(s)-2], "+")
		parts = append(raw, "+")
	} else {
		parts = strings.Split(s, "+")
	}
	if len(parts) == 0 {
		return nil, ""
	}
	mods := make([]string, 0, len(parts)-1)
	for _, p := range parts[:len(parts)-1] {
		mods = append(mods, strings.TrimSpace(p))
	}
	return mods, strings.TrimSpace(parts[len(parts)-1])
}

func parseModFlags(mods []string) (ctrl, alt, shift, cmd bool) {
	for _, m := range mods {
		switch strings.ToLower(m) {
		case "ctrl", "control":
			ctrl = true
		case "alt", "option", "altgr":
			alt = true
		case "shift":
			shift = true
		case "cmd", "command", "super", "meta", "win", "cmdorctrl", "commandorcontrol":
			cmd = true
		}
	}
	return
}

func canonicalShortcutKey(acc string) string {
	mods, key := splitAcceleratorParts(acc)
	if key == "" {
		return ""
	}
	ctrl, alt, shift, cmd := parseModFlags(mods)
	mask := 0
	if ctrl {
		mask |= 1
	}
	if alt {
		mask |= 2
	}
	if shift {
		mask |= 4
	}
	if cmd {
		mask |= 8
	}
	return fmt.Sprintf("%d:%s", mask, strings.ToUpper(key))
}

func formatModSymbols(mods ui.Modifiers) []string {
	var out []string
	if mods&ui.Ctrl != 0 {
		out = append(out, "⌃")
	}
	if mods&ui.Alt != 0 {
		out = append(out, "⌥")
	}
	if mods&ui.Shift != 0 {
		out = append(out, "⇧")
	}
	if mods&ui.Cmd != 0 {
		out = append(out, "⌘")
	}
	return out
}

func formatMacShortcutSymbols(acc string) []string {
	mods, key := splitAcceleratorParts(acc)
	if key == "" {
		return nil
	}
	ctrl, alt, shift, cmd := parseModFlags(mods)
	var out []string
	if ctrl {
		out = append(out, "⌃")
	}
	if alt {
		out = append(out, "⌥")
	}
	if shift {
		out = append(out, "⇧")
	}
	if cmd {
		out = append(out, "⌘")
	}
	switch strings.ToLower(key) {
	case "enter", "return":
		out = append(out, "↩")
	case "tab":
		out = append(out, "⇥")
	case "backspace":
		out = append(out, "⌫")
	case "delete", "del":
		out = append(out, "⌦")
	case "escape", "esc":
		out = append(out, "⎋")
	case "space":
		out = append(out, "Space")
	case "up":
		out = append(out, "↑")
	case "down":
		out = append(out, "↓")
	case "left":
		out = append(out, "←")
	case "right":
		out = append(out, "→")
	default:
		out = append(out, strings.ToUpper(key))
	}
	return out
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
			full, err := writeUniqueFile(targetDir, name, data)
			if err != nil {
				a.savePNGDialog(data, filepath.Join(targetDir, name))
				return
			}
			saved := filepath.Base(full)
			mygo.RunOnMain(func() {
				a.sessionStatus("已快存: " + saved)
			})
		}()
		return
	}
	go a.savePNGDialog(data, filepath.Join(targetDir, name))
}

// writeUniqueFile 以 O_EXCL 写入 dir/name，重名时在扩展名前追加 -1、-2… 后缀，返回实际路径。
func writeUniqueFile(dir, name string, data []byte) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		full := filepath.Join(dir, n)
		f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(full)
			return "", werr
		}
		return full, nil
	}
	return "", fmt.Errorf("文件名冲突过多: %s", name)
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
	if err != nil {
		log.Printf("保存对话框失败: %v", err)
		return
	}
	if path == "" {
		return
	}
	// 用户在系统对话框中已确认覆盖，这里允许直接写入
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
		Vibrancy:        vibrancyForGlassLevel(a.cfg.GlassLevel),
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
	applyWindowTheme(cd.win, themeStateFromConfig(a.cfg))

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mygo.RunOnMain(func() {
					if a.countdown != cd {
						return
					}
					cd.remaining--
					if cd.remaining <= 0 {
						a.stopCountdown()
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
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.stopCountdown() }) {
			return
		}
		p := uiPalette(c)
		pill := ui.Row(c).
			Fill().
			Padding(0, 14).
			Gap(8).
			AlignItems(ui.Center).
			Radius(21).
			Border(1, p.ActiveBorder)
		if c.Vibrancy() {
			if wash := panelSurfaceWash(getActiveTheme().GlassLevel, p.Dark); wash.A > 0 {
				pill.Background(wash)
			}
		} else {
			pill = liquidGlassCapsule(c, pill, 21, false).Border(1, p.ActiveBorder)
		}
		pill.Children(func() {
			ui.Icon(c, iconTimer).FontSize(15).TextColor(p.Accent)
			ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
				ui.Text(c, fmt.Sprintf("%d", cd.remaining)).
					FontSize(12.5).FontWeight(700).FontFeatures("tnum").NoWrap().TextColor(p.Accent).
					ContentTransition(ui.ContentTransition{Numeric: true, Down: true})
				ui.Text(c, "秒后定格屏幕…").
					FontSize(12.5).FontWeight(600).NoWrap().TextColor(p.Text)
			})
			ui.Spacer(c)
			if miniPillBtn(c, nil, "取消", false) {
				go mygo.RunOnMain(func() { a.stopCountdown() })
			}
		})
	})
}
