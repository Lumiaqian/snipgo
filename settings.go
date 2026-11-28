package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
)

const (
	settingsWinW = 500
	settingsWinH = 488
)

type settingsWindow struct {
	win           *mygo.Window
	draft         config.Settings
	tab           int
	saved         string
	badge         badgeState
	saveFailed    bool // 最近一次写入 settings.json 失败
	enter         panelEnterState
	recordingID   string
	recordingMods ui.Modifiers
	recordingErr  string
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
	{id: "colorpicker", label: "屏幕取色器", ptr: func(s *config.Settings) *string { return &s.ColorPickerShortcut }},
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
	a.syncSessionColorFormat()
	if a.userDataDir != "" {
		saveErr := config.Save(a.userDataDir, a.cfg)
		if saveErr != nil {
			log.Printf("save settings: %v", saveErr)
		}
		// 记录保存结果，setSavedBadge 据此把随后的“✓ 已保存”改为失败提示。
		if a.setWin != nil {
			a.setWin.saveFailed = saveErr != nil
		}
	}
	a.registerAllShortcuts()
	a.refreshMenu()
	if a.upd != nil && (prevAuto != a.cfg.AutoCheckUpdates || prevPre != a.cfg.IncludePrereleases) {
		a.restartUpdateScheduler()
	}
}

// historyEnabled 报告是否保存最近截图历史（默认开启，设置里可关闭）。
func (a *app) historyEnabled() bool { return !a.cfg.DisableHistory }

// savedBadgeText 在最近一次保存失败时，把“✓ …”成功文案换成失败提示，避免误导用户。
func savedBadgeText(msg string, saveFailed bool) string {
	if saveFailed && strings.HasPrefix(msg, "✓") {
		return "⚠ 设置保存失败"
	}
	return msg
}

func (sw *settingsWindow) setSavedBadge(msg string) {
	msg = savedBadgeText(msg, sw.saveFailed)
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
				if picked := liquidSegmented(c, "set-tab", sw.tab, []string{"外观与主题", "全局快捷键", "通用与更新", "命令行与 Agent"}); picked >= 0 && picked != sw.tab {
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
				case 3:
					ui.Column(c).FillWidth().Gap(10).Children(func() { a.paintCommandLineCard(c, p, sw) })
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
				accentGroup := behaviors.RadioGroup(c.Key("set-accent-group"), behaviors.RadioGroupOptions{
					Label: "主题色",
				})
				accentGroup.Root.AlignItems(ui.Center).Gap(7).Children(func() {
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
						swBtn := accentGroup.Item(c.Key(840+i), opt.Name, active, behaviors.RadioItemOptions{
							Tooltip: opt.Name,
						}).
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
				paintPreviewPillChip(c, iconOCR, "识图", false)
				paintPreviewPillChip(c, iconPin, "贴图", false)
				ui.Spacer(c)
				paintPreviewPillChip(c, iconCheck, "复制选中", true)
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
				saveHistory := !sw.draft.DisableHistory
				if liquidSwitch(c, "savehistory", &saveHistory, "保存最近截图历史（关闭后不再记录新的截图）") {
					sw.draft.DisableHistory = !saveHistory
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

				if liquidSwitch(c, "record-countdown", &sw.draft.RecordCountdown, "录屏前 3 秒倒计时") {
					a.applyAndSaveSettings(sw.draft)
					sw.setSavedBadge("✓ 已保存")
				}
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

				fmtVals := []string{"hex", "rgb", "oklch", "tailwind"}
				fmtSel := 0
				for i, key := range fmtVals {
					if sw.draft.ColorFormat == key {
						fmtSel = i
						break
					}
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "默认取色格式：").FontSize(12).TextColor(p.TextSecondary)
					ui.Spacer(c)
					if picked := liquidSegmented(c, "set-colorfmt", fmtSel, []string{"HEX", "RGB", "OKLCH", "Tailwind"}); picked >= 0 && fmtVals[picked] != sw.draft.ColorFormat {
						sw.draft.ColorFormat = fmtVals[picked]
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

func paintPreviewPillChip(c *ui.Context, ic *ui.SVG, label string, primary bool) {
	p := uiPalette(c)
	el := ui.Row(c).Height(26).Padding(0, 9).Gap(4).AlignItems(ui.Center).Radius(13).Shrink(0)
	if primary {
		liquidPrimaryChrome(c, el, 13)
	} else {
		liquidSecondaryChrome(c, el, false, 13)
	}
	el.Children(func() {
		col := p.Text
		if primary {
			col = p.AccentText
		}
		if ic != nil {
			ui.Icon(c, ic).FontSize(14).TextColor(col)
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).NoWrap().TextColor(col)
	})
}

func (a *app) paintCommandLineCard(c *ui.Context, p palette, sw *settingsWindow) {
	sectionTitle(c, "命令行与 Agent")
	settingsCard(c).Children(func() {
		home, homeErr := os.UserHomeDir()
		exe, exeErr := os.Executable()
		link := filepath.Join(home, ".local", "bin", "snipgo")
		status := "未安装"
		target, err := os.Readlink(link)
		installed := err == nil && target == exe
		if installed {
			status = "已安装 · ~/.local/bin/snipgo"
			if found, _ := exec.LookPath("snipgo"); found != link {
				status += " · 请将 ~/.local/bin 加入 PATH"
			}
		}
		ui.Text(c, status).FontSize(12).TextColor(p.TextSecondary)
		if miniPillBtn(c, nil, "安装命令行工具", false) {
			switch {
			case installed:
				sw.setSavedBadge("命令行工具已安装")
			case homeErr != nil || exeErr != nil:
				sw.setSavedBadge("无法定位用户目录或 SnipGo")
			default:
				err := os.MkdirAll(filepath.Dir(link), 0755)
				if err == nil {
					err = os.Symlink(exe, link)
				}
				if err != nil {
					sw.setSavedBadge("安装失败：" + err.Error())
				} else {
					sw.setSavedBadge("已安装命令行工具")
				}
			}
		}
	})
}
