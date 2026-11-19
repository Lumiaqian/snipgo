package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
	"snipgo/internal/updater"
)

//go:embed resources/icon.png
var appIconPNG []byte

const (
	defaultAppVersion = "0.1.0"
	updateWinW        = 500
	updateWinH        = 440
	autoCheckDelay    = 12 * time.Second
	autoCheckInterval = 24 * time.Hour
)

var buildVersion string

func currentAppVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" {
		return v
	}
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	return defaultAppVersion
}

type updateState int

const (
	updIdle updateState = iota
	updChecking
	updAvailable
	updDownloading
	updVerifying
	updExtracting
	updReady
	updUpToDate
	updError
)

type updateManager struct {
	client *updater.Client
	win    *mygo.Window

	iconBmp *ui.Bitmap
	state   updateState
	release *updater.Release
	staged  *updater.StagedUpdate

	downloaded  int64
	total       int64
	errMsg      string
	lastChecked time.Time
	notifiedVer string

	bundlePath     string
	canSelfUpdate  bool
	selfUpdateHint string

	opSeq    uint64
	opCancel context.CancelFunc
	bgCancel context.CancelFunc
	enter    panelEnterState
}

func (a *app) initUpdater() {
	u := &updateManager{
		client: updater.NewClient(),
	}
	if bmp, err := ui.DecodeBitmap(appIconPNG); err == nil {
		u.iconBmp = bmp
	}
	if exe, err := os.Executable(); err == nil {
		if bundle, bErr := updater.DetectAppBundle(exe); bErr == nil {
			u.bundlePath = bundle
			if wErr := updater.CheckWritableBundle(bundle); wErr == nil {
				u.canSelfUpdate = true
			} else {
				u.selfUpdateHint = wErr.Error()
			}
		} else {
			u.selfUpdateHint = bErr.Error()
		}
	} else {
		u.selfUpdateHint = "无法定位应用运行路径"
	}
	a.upd = u
	a.restartUpdateScheduler()
}

func (a *app) restartUpdateScheduler() {
	u := a.upd
	if u == nil {
		return
	}
	if u.bgCancel != nil {
		u.bgCancel()
		u.bgCancel = nil
	}
	if !a.cfg.AutoCheckUpdates {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.bgCancel = cancel
	go func() {
		timer := time.NewTimer(autoCheckDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		mygo.RunOnMain(func() {
			if a.quitting || a.upd != u {
				return
			}
			a.checkForUpdates(false)
		})

		ticker := time.NewTicker(autoCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mygo.RunOnMain(func() {
					if a.quitting || a.upd != u {
						return
					}
					a.checkForUpdates(false)
				})
			}
		}
	}()
}

func (a *app) shutdownUpdater() {
	u := a.upd
	if u == nil {
		return
	}
	if u.bgCancel != nil {
		u.bgCancel()
		u.bgCancel = nil
	}
	if u.opCancel != nil {
		u.opCancel()
		u.opCancel = nil
	}
	a.closeUpdateWindow()
}

func (a *app) availableUpdate() *updater.Release {
	u := a.upd
	if u == nil || u.release == nil {
		return nil
	}
	if u.release.Version == a.cfg.SkippedVersion && u.state == updIdle {
		return nil
	}
	return u.release
}

func (u *updateManager) isBusyDownloading() bool {
	return u.state == updDownloading || u.state == updVerifying || u.state == updExtracting
}

func (u *updateManager) invalidateUI() {
	if u.win != nil && !u.win.IsDestroyed() {
		u.win.Invalidate()
	}
}

func (a *app) invalidateSettingsWin() {
	if a.setWin != nil && a.setWin.win != nil && !a.setWin.win.IsDestroyed() {
		a.setWin.win.Invalidate()
	}
}

func (a *app) checkForUpdates(manual bool) {
	u := a.upd
	if u == nil {
		return
	}
	if u.isBusyDownloading() || u.state == updReady {
		if manual {
			a.openUpdateWindow()
		}
		return
	}
	if u.opCancel != nil {
		u.opCancel()
		u.opCancel = nil
	}

	u.opSeq++
	seq := u.opSeq
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	u.opCancel = cancel
	u.state = updChecking
	u.errMsg = ""
	if manual {
		a.openUpdateWindow()
	} else {
		u.invalidateUI()
	}

	client := u.client
	curVer := currentAppVersion()
	includePre := a.cfg.IncludePrereleases

	go func() {
		rel, err := client.Check(ctx, curVer, includePre)
		mygo.RunOnMain(func() {
			if a.quitting || a.upd != u || u.opSeq != seq {
				return
			}
			u.opCancel = nil
			u.lastChecked = time.Now()
			winVisible := u.win != nil && !u.win.IsDestroyed()
			if err != nil {
				log.Printf("updater check failed: %v", err)
				if !manual && !winVisible {
					u.state = updIdle
					return
				}
				u.errMsg = err.Error()
				u.state = updError
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}
			if rel == nil {
				u.release = nil
				u.state = updUpToDate
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}

			u.release = rel
			u.state = updAvailable
			u.errMsg = ""
			a.refreshMenu()
			a.invalidateSettingsWin()

			if manual || winVisible {
				a.openUpdateWindow()
				return
			}
			if rel.Version != a.cfg.SkippedVersion && rel.Version != u.notifiedVer {
				u.notifiedVer = rel.Version
				a.openUpdateWindow()
			}
		})
	}()
}

func (a *app) startUpdateDownload() {
	u := a.upd
	if u == nil || u.release == nil || u.isBusyDownloading() {
		return
	}
	if !u.canSelfUpdate || strings.TrimSpace(u.release.AssetURL) == "" {
		a.openReleaseDownload(u.release)
		return
	}
	if u.opCancel != nil {
		u.opCancel()
		u.opCancel = nil
	}

	u.opSeq++
	seq := u.opSeq
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	u.opCancel = cancel
	u.state = updDownloading
	u.downloaded = 0
	u.total = u.release.AssetSize
	u.errMsg = ""
	u.invalidateUI()

	relCopy := *u.release
	client := u.client
	bundlePath := u.bundlePath

	go func() {
		staged, err := client.DownloadAndStage(ctx, &relCopy, bundlePath, func(phase updater.StagePhase, dl, total int64) {
			mygo.RunOnMain(func() {
				if a.quitting || a.upd != u || u.opSeq != seq {
					return
				}
				switch phase {
				case updater.PhaseDownloading:
					u.state = updDownloading
				case updater.PhaseVerifying:
					u.state = updVerifying
				case updater.PhaseExtracting, updater.PhaseStaging:
					u.state = updExtracting
				}
				u.downloaded = dl
				u.total = total
				u.invalidateUI()
			})
		})
		mygo.RunOnMain(func() {
			if a.quitting || a.upd != u || u.opSeq != seq {
				if staged != nil {
					staged.Cancel()
				}
				return
			}
			u.opCancel = nil
			if err != nil {
				if errors.Is(err, context.Canceled) {
					u.state = updAvailable
					u.errMsg = ""
				} else {
					log.Printf("updater download/stage failed: %v", err)
					u.state = updError
					u.errMsg = err.Error()
				}
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}

			if old := u.staged; old != nil {
				old.Cancel()
			}
			u.staged = staged
			u.state = updReady
			u.errMsg = ""
			u.invalidateUI()
			a.refreshMenu()
			a.invalidateSettingsWin()
		})
	}()
}

func (a *app) cancelUpdateOp() {
	u := a.upd
	if u == nil {
		return
	}
	if u.opCancel != nil {
		u.opCancel()
		u.opCancel = nil
	}
	if u.isBusyDownloading() {
		u.opSeq++
		u.state = updAvailable
		u.invalidateUI()
	} else if u.state == updChecking {
		u.opSeq++
		u.state = updIdle
		a.closeUpdateWindow()
	}
}

func (a *app) relaunchWithUpdate() {
	u := a.upd
	if u == nil || u.staged == nil {
		return
	}
	if err := u.staged.MarkRelaunch(); err != nil {
		u.errMsg = err.Error()
		u.state = updError
		u.invalidateUI()
		return
	}
	mygo.App.Quit()
}

func (a *app) openReleaseDownload(rel *updater.Release) {
	target := fmt.Sprintf("https://github.com/%s/releases/latest", updater.DefaultRepo)
	if rel != nil {
		if strings.TrimSpace(rel.DMGURL) != "" {
			target = rel.DMGURL
		} else if strings.TrimSpace(rel.HTMLURL) != "" {
			target = rel.HTMLURL
		}
	}
	go func() { _ = mygo.Shell.OpenExternal(target) }()
}

func (a *app) openUpdateWindow() {
	u := a.upd
	if u == nil {
		return
	}
	if u.win != nil && !u.win.IsDestroyed() {
		if mygo.App.IsReady() {
			mygo.App.Focus()
		}
		u.win.Focus()
		u.win.Invalidate()
		return
	}
	u.enter = panelEnterState{enabled: true}
	pos := a.placeCenteredPanel(updateWinW, updateWinH)
	u.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "SnipGo 软件更新",
		Width:           updateWinW,
		Height:          updateWinH,
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
		MinWidth:        440,
		MinHeight:       360,
		Content: ui.View(func(c *ui.Context) {
			a.paintUpdateView(c, u)
		}),
	})
	u.win.OnClosed(func() {
		if a.upd == u {
			u.win = nil
		}
	})
	applyWindowTheme(u.win, themeStateFromConfig(a.cfg))
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	u.win.Focus()
}

func (a *app) closeUpdateWindow() {
	u := a.upd
	if u != nil && u.win != nil && !u.win.IsDestroyed() {
		old := u.win
		u.win = nil
		old.Close()
	}
}

func (a *app) paintUpdateView(c *ui.Context, u *updateManager) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.closeUpdateWindow() }) {
			return
		}

		p := uiPalette(c)
		card := liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &u.enter)

		card.Children(func() {
			// 1. 顶部浮动标题栏
			header := ui.Row(c).
				Height(50).
				Padding(0, 16).
				Gap(8).
				AlignItems(ui.Center).
				ZIndex(2).
				DragWindow()
			header.Children(func() {
				if macTrafficCloseBtn(c, "关闭更新窗口", "关闭 (⌘W / Esc)") {
					if u.win != nil {
						go mygo.RunOnMain(func() { a.closeUpdateWindow() })
					}
				}
				ui.Box(c).Width(2)
				ui.Icon(c, iconUpdate).FontSize(15).TextColor(p.Accent)
				ui.Text(c, "SnipGo 软件更新").FontSize(13.5).FontWeight(600).NoWrap().TextColor(p.Text)
				switch u.state {
				case updReady:
					statusBadgePill(c, "✓ 更新就绪", true)
				case updAvailable:
					if u.release != nil && u.release.Prerelease {
						statusBadgePill(c, "Beta 预览版", false)
					} else {
						statusBadgePill(c, "发现新版本", false)
					}
				case updUpToDate:
					statusBadgePill(c, "已是最新", true)
				}
				ui.Spacer(c)
			})
			panelHairline(c)

			// 2. 主内容区
			body := ui.Column(c).Grow(1).Padding(14, 18).Gap(12)
			body.Children(func() {
				a.paintUpdateHero(c, p, u)
				a.paintUpdateBody(c, p, u)
			})

			panelHairline(c)

			// 3. 底部操作栏
			a.paintUpdateFooter(c, p, u)
		})
	})
}

func (a *app) paintUpdateHero(c *ui.Context, p palette, u *updateManager) {
	headline, sub := a.updateHeroText(u)
	row := ui.Row(c).Gap(14).AlignItems(ui.Center)
	row.Children(func() {
		iconBox := ui.Box(c).
			Size(52, 52).
			Radius(12).
			Clip().
			Shrink(0).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.22))
		if u.iconBmp != nil {
			iconBox.Children(func() {
				ui.Image(c, u.iconBmp).Size(52, 52)
			})
		} else {
			iconBox.Background(p.BadgeInfoBg).Border(1, p.BadgeInfoBd).Children(func() {
				ui.Icon(c, iconUpdate).FontSize(24).TextColor(p.Accent)
			})
		}

		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, headline).FontSize(15).FontWeight(600).NoWrap().MaxLines(1).Ellipsis("…").TextColor(p.Text)
			ui.Text(c, sub).FontSize(12).FontFeatures("tnum").MaxLines(2).TextColor(p.TextSecondary)
		})
	})
}

func (a *app) updateHeroText(u *updateManager) (string, string) {
	cur := updater.FormatVersion(currentAppVersion())
	target := ""
	if u.release != nil {
		target = updater.FormatVersion(u.release.Version)
	} else if u.staged != nil {
		target = updater.FormatVersion(u.staged.Version)
	}

	switch u.state {
	case updChecking:
		return "正在检查 SnipGo 更新…", fmt.Sprintf("当前运行版本：%s", cur)
	case updAvailable:
		sizeStr := ""
		if u.release != nil && u.release.AssetSize > 0 {
			sizeStr = " · 约 " + updater.FormatBytes(u.release.AssetSize)
		}
		return fmt.Sprintf("SnipGo %s 现已可用", target), fmt.Sprintf("当前 %s → 最新 %s%s", cur, target, sizeStr)
	case updDownloading:
		return fmt.Sprintf("正在下载 SnipGo %s…", target), fmt.Sprintf("当前 %s → %s · 全程 SHA-256 校验", cur, target)
	case updVerifying:
		return "正在验证更新包完整性…", "核对 SHA-256 哈希与 App Bundle 身份签名"
	case updExtracting:
		return "正在解包并准备原子替换…", "准备就绪后可一键平滑重启"
	case updReady:
		return fmt.Sprintf("SnipGo %s 已下载并验证", target), "重启后立即切换新版，保留全部快捷键与截图历史"
	case updUpToDate:
		return "SnipGo 已是最新版本", fmt.Sprintf("当前版本 %s（macOS 原生纯 Go 构建）", cur)
	case updError:
		return "更新过程中遇到问题", fmt.Sprintf("当前版本 %s · 可重试或直接打开 GitHub 发布页下载", cur)
	default:
		return "SnipGo 软件更新", fmt.Sprintf("当前版本 %s", cur)
	}
}

func (a *app) paintUpdateBody(c *ui.Context, p palette, u *updateManager) {
	switch u.state {
	case updAvailable:
		notesText := "本次更新包含稳定性改进与性能优化。"
		if u.release != nil && strings.TrimSpace(u.release.Notes) != "" {
			notesText = u.release.Notes
		}
		notesCard := liquidCard(c, ui.Box(c).Grow(1).Clip())
		notesCard.Children(func() {
			sc := ui.Scroll(c).Fill().Padding(11, 13).ScrollbarInsets(8, 0, 8)
			sc.Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, "更新日志").FontSize(11).FontWeight(600).TextColor(p.TextMuted)
					for i, ln := range parseReleaseNoteLines(notesText) {
						cKey := c.Key(9000 + i)
						switch ln.kind {
						case noteHeading:
							ui.Text(cKey, ln.text).FontSize(12.5).FontWeight(600).Padding(2, 0, 0, 0).TextColor(p.Text)
						case noteBullet:
							ui.Row(cKey).Gap(6).AlignItems(ui.Start).Children(func() {
								ui.Box(c).Size(5, 5).Radius(2.5).Margin(6, 0, 0, 0).Shrink(0).Background(p.Accent)
								ui.Text(c, ln.text).FontSize(12).LineHeight(1.45).Grow(1).TextColor(p.Text)
							})
						default:
							ui.Text(cKey, ln.text).FontSize(12).LineHeight(1.45).TextColor(p.TextSecondary)
						}
					}
				})
			})
			paintTopScrollEdge(c, 12)
			paintBottomScrollEdge(c, 12)
		})

		if !u.canSelfUpdate {
			hint := u.selfUpdateHint
			if hint == "" {
				hint = "当前安装位置不可直接原地替换，点击右侧按钮可在浏览器下载 DMG 安装包"
			}
			ui.Row(c).Padding(7, 11).Gap(6).AlignItems(ui.Center).Radius(10).
				Background(p.BadgeInfoBg).Border(1, p.BadgeInfoBd).
				Children(func() {
					ui.Text(c, hint).FontSize(11).TextColor(p.BadgeInfoFg)
				})
		}

	case updDownloading, updVerifying, updExtracting:
		progCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 18).Gap(12).Justify(ui.Center))
		progCard.Children(func() {
			var frac float32
			stageLabel := "正在连接下载服务器…"
			rightLabel := ""
			switch u.state {
			case updDownloading:
				if u.total > 0 {
					frac = max(0.03, min(1.0, float32(u.downloaded)/float32(u.total)))
					pct := int(frac * 100)
					stageLabel = fmt.Sprintf("已下载 %s / %s", updater.FormatBytes(u.downloaded), updater.FormatBytes(u.total))
					rightLabel = fmt.Sprintf("%d%%", pct)
				} else {
					frac = 0.35
					stageLabel = fmt.Sprintf("已下载 %s…", updater.FormatBytes(u.downloaded))
				}
			case updVerifying:
				frac = 0.90
				stageLabel = "正在校验 SHA-256 哈希摘要…"
				rightLabel = "90%"
			case updExtracting:
				frac = 0.97
				stageLabel = "正在解包并校验 App Bundle…"
				rightLabel = "97%"
			}

			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, stageLabel).FontSize(12.5).FontWeight(500).FontFeatures("tnum").TextColor(p.Text)
				ui.Spacer(c)
				if rightLabel != "" {
					ui.Text(c, rightLabel).FontSize(12.5).FontWeight(600).FontFeatures("tnum").TextColor(p.ActiveText).
						ContentTransition(ui.ContentTransition{Numeric: true})
				}
			})

			progBar := ui.Box(c.Key("upd-prog-bar")).Height(9).Radius(4.5).Clip().
				Background(p.InputBg).
				Border(1, p.InputBorder)
			animFrac := progBar.AnimateTo("dl-pct", frac, springSmooth)
			progBar.Draw(func(painter *ui.Painter, r ui.Rect) {
				w := max(float32(8), r.W*min(1, max(0, animFrac)))
				painter.FillGradient(
					ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H},
					ui.LinearGradient{
						From:  p.Accent,
						To:    ui.RGB(94, 198, 255),
						Angle: 90,
						Oklab: true,
					},
					r.H/2,
				)
			})

			ui.Text(c, "下载完成后才会在退出时原子替换 .app，不会中断当前贴图或录屏工作。").
				FontSize(11.5).TextColor(p.TextMuted)
		})

	case updReady:
		readyCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 20).Gap(10).Justify(ui.Center).AlignItems(ui.Center))
		readyCard.Children(func() {
			ui.Row(c).Height(28).Padding(0, 12).Gap(6).AlignItems(ui.Center).Radius(14).
				Background(p.BadgeSuccessBg).Border(1, p.BadgeSuccessBd).
				Children(func() {
					ui.Icon(c, iconCheck).FontSize(13.5).TextColor(p.BadgeSuccessFg)
					ui.Text(c, "SHA-256 与应用身份核验已通过").FontSize(12).FontWeight(600).TextColor(p.BadgeSuccessFg)
				})
			ui.Text(c, "新版 SnipGo 已准备就绪，点击下方「立即重启应用」即可完成替换并重新打开。").
				FontSize(12.5).LineHeight(1.5).TextColor(p.Text)
			ui.Text(c, "若选择「下次启动生效」，新版本会在你下一次退出 SnipGo 后自动无感完成替换。").
				FontSize(11.5).TextColor(p.TextMuted)
		})

	default:
		statusCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 20).Gap(8).Justify(ui.Center).AlignItems(ui.Center))
		statusCard.Children(func() {
			switch u.state {
			case updChecking:
				ui.Icon(c, iconUpdate).FontSize(26).TextColor(p.Accent)
				ui.Text(c, "正在向 GitHub Releases 查询最新版本…").FontSize(12.5).TextColor(p.Text)
			case updUpToDate:
				ui.Icon(c, iconCheck).FontSize(26).TextColor(p.BadgeSuccessFg)
				ui.Text(c, "当前安装的 SnipGo 已是最新正式版，无需更新。").FontSize(12.5).TextColor(p.Text)
			case updError:
				ui.Icon(c, iconClose).FontSize(24).TextColor(ui.RGB(235, 68, 60))
				msg := u.errMsg
				if msg == "" {
					msg = "网络请求失败，请检查网络连接或系统代理设置"
				}
				ui.Text(c, msg).FontSize(12).LineHeight(1.45).TextColor(ui.RGB(235, 68, 60))
			}
			if !u.lastChecked.IsZero() {
				ui.Text(c, "上次检查时间："+u.lastChecked.Format("15:04:05")).
					FontSize(11).FontFeatures("tnum").TextColor(p.TextMuted)
			}
		})
	}
}

func (a *app) paintUpdateFooter(c *ui.Context, p palette, u *updateManager) {
	footer := ui.Row(c).Height(54).Padding(0, 16).Gap(8).AlignItems(ui.Center).ZIndex(2)
	footer.Children(func() {
		switch u.state {
		case updAvailable:
			liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
				if glassGroupBtn(c, add, nil, "跳过此版本", "跳过此版本，以后不再自动弹窗提示") {
					if u.release != nil {
						a.cfg.SkippedVersion = u.release.Version
						if a.userDataDir != "" {
							_ = config.Save(a.userDataDir, a.cfg)
						}
						u.release = nil
						u.state = updIdle
						a.refreshMenu()
						a.invalidateSettingsWin()
					}
					go mygo.RunOnMain(func() { a.closeUpdateWindow() })
				}
				if glassGroupBtn(c, add, nil, "稍后提醒", "暂时关闭更新窗口") {
					go mygo.RunOnMain(func() { a.closeUpdateWindow() })
				}
			})
			ui.Spacer(c)
			if actionPillBtn(c, iconExternal, "发布页", "在浏览器中查看发布详情", false, true) {
				a.openReleaseDownload(u.release)
			}
			if u.canSelfUpdate && u.release != nil && u.release.AssetURL != "" {
				if actionPillBtn(c, iconUpdate, "立即更新", "下载、校验并准备重启升级", true, false) {
					a.startUpdateDownload()
				}
			} else {
				if actionPillBtn(c, iconExternal, "前往下载", "在浏览器下载最新版 DMG", true, false) {
					a.openReleaseDownload(u.release)
				}
			}

		case updDownloading, updVerifying, updExtracting:
			ui.Text(c, "更新包下载并核验完成后才会生效").FontSize(11).TextColor(p.TextMuted)
			ui.Spacer(c)
			if u.state == updDownloading {
				if actionPillBtn(c, iconClose, "取消下载", "中止本次下载", false, true) {
					a.cancelUpdateOp()
				}
			}

		case updReady:
			if miniPillBtn(c, nil, "下次启动生效", false) {
				go mygo.RunOnMain(func() { a.closeUpdateWindow() })
			}
			ui.Spacer(c)
			if actionPillBtn(c, iconCheck, "立即重启应用", "退出并原子替换到新版本后自动重开", true, false) {
				a.relaunchWithUpdate()
			}

		case updUpToDate:
			if miniPillBtn(c, iconExternal, "GitHub 发布页", false) {
				a.openReleaseDownload(nil)
			}
			ui.Spacer(c)
			if actionPillBtn(c, iconCheck, "好", "关闭窗口", true, false) {
				go mygo.RunOnMain(func() { a.closeUpdateWindow() })
			}

		case updError:
			if miniPillBtn(c, iconExternal, "手动下载", false) {
				a.openReleaseDownload(u.release)
			}
			ui.Spacer(c)
			if actionPillBtn(c, iconUpdate, "重新检查", "重试检查更新", true, false) {
				a.checkForUpdates(true)
			}

		default: // updChecking / updIdle
			ui.Text(c, "正在连接 GitHub Releases…").FontSize(11).TextColor(p.TextMuted)
			ui.Spacer(c)
			if miniPillBtn(c, nil, "取消", false) {
				a.cancelUpdateOp()
			}
		}
	})
}

type noteKind int

const (
	noteParagraph noteKind = iota
	noteHeading
	noteBullet
)

type noteLine struct {
	kind noteKind
	text string
}

func parseReleaseNoteLines(md string) []noteLine {
	var out []noteLine
	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || s == "---" || s == "***" {
			continue
		}
		kind := noteParagraph
		switch {
		case strings.HasPrefix(s, "#"):
			s = strings.TrimSpace(strings.TrimLeft(s, "#"))
			kind = noteHeading
		case strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* "):
			s = strings.TrimSpace(s[2:])
			kind = noteBullet
		case strings.HasPrefix(s, ">"):
			s = strings.TrimSpace(strings.TrimLeft(s, ">"))
		}
		s = strings.ReplaceAll(s, "**", "")
		s = strings.ReplaceAll(s, "`", "")
		if s != "" {
			out = append(out, noteLine{kind: kind, text: s})
		}
	}
	return out
}
