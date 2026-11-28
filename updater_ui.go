package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
	"snipgo/internal/session"
)

//go:embed resources/icon.png
var appIconPNG []byte

const (
	defaultAppVersion = "0.1.12"
	updateGitHubRepo  = "Lumiaqian/snipgo"
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

type updateCandidate struct {
	Version    string
	Notes      string
	Date       time.Time
	Prerelease bool
	Size       int64

	raw *mygo.Update
}

type updateBackend interface {
	Enabled() bool
	SetPrereleases(on bool)
	Check(ctx context.Context) (*updateCandidate, error)
	Install(ctx context.Context, cand *updateCandidate, progress func(downloaded, total int64)) error
	Relaunch()
}

type mygoUpdateBackend struct{}

func (mygoUpdateBackend) Enabled() bool {
	return mygo.Updater.Enabled()
}

func (mygoUpdateBackend) SetPrereleases(on bool) {
	mygo.Updater.SetPrereleases(on)
}

func (mygoUpdateBackend) Check(ctx context.Context) (*updateCandidate, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return &updateCandidate{
		Version:    up.Version,
		Notes:      strings.TrimSpace(up.Notes),
		Date:       up.Date,
		Prerelease: strings.Contains(up.Version, "-"),
		raw:        up,
	}, nil
}

func (mygoUpdateBackend) Install(ctx context.Context, cand *updateCandidate, progress func(downloaded, total int64)) error {
	if cand == nil || cand.raw == nil {
		return errors.New("缺少签名更新元数据")
	}
	return cand.raw.Install(ctx, progress)
}

func (mygoUpdateBackend) Relaunch() {
	mygo.App.Relaunch()
}

type updateManager struct {
	backend       updateBackend
	win           *mygo.Window
	iconBmp       *ui.Bitmap
	state         updateState
	release       *updateCandidate
	stagedVersion string

	downloaded  int64
	total       int64
	errMsg      string
	lastChecked time.Time
	notifiedVer string

	canSelfUpdate bool

	opSeq    uint64
	opCancel context.CancelFunc
	bgCancel context.CancelFunc
	enter    panelEnterState
	onMain   func(func())
	onOpen   func()
}

func (u *updateManager) runMain(fn func()) {
	if u != nil && u.onMain != nil {
		u.onMain(fn)
		return
	}
	mygo.RunOnMain(fn)
}

func (u *updateManager) activeBackend() updateBackend {
	if u != nil && u.backend != nil {
		return u.backend
	}
	return mygoUpdateBackend{}
}

func (a *app) initUpdater() {
	u := &updateManager{
		backend: mygoUpdateBackend{},
	}
	if bmp, err := ui.DecodeBitmap(appIconPNG); err == nil {
		u.iconBmp = bmp
	}
	u.canSelfUpdate = u.backend.Enabled()
	a.upd = u
	a.restartUpdateScheduler()
}

func (a *app) restartUpdateScheduler() {
	u := a.upd
	if u == nil {
		return
	}
	u.activeBackend().SetPrereleases(a.cfg.IncludePrereleases)
	if u.bgCancel != nil {
		u.bgCancel()
		u.bgCancel = nil
	}
	if !a.cfg.AutoCheckUpdates || !u.activeBackend().Enabled() {
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
		u.runMain(func() {
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
				u.runMain(func() {
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

func (a *app) availableUpdate() *updateCandidate {
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
	backend := u.activeBackend()
	u.canSelfUpdate = backend.Enabled()
	backend.SetPrereleases(a.cfg.IncludePrereleases)

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

	go func() {
		cand, err := backend.Check(ctx)
		u.runMain(func() {
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
				u.errMsg = friendlyUpdateError(err)
				u.state = updError
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}
			if cand == nil {
				u.release = nil
				u.state = updUpToDate
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}

			u.release = cand
			u.state = updAvailable
			u.errMsg = ""
			a.refreshMenu()
			a.invalidateSettingsWin()

			if manual || winVisible {
				a.openUpdateWindow()
				return
			}
			// 用户正在截图/标注/贴图编辑等操作时不抢焦点：保留菜单角标，且不记 notifiedVer，下次检查仍可弹出。
			if a.userBusy() {
				return
			}
			if cand.Version != a.cfg.SkippedVersion && cand.Version != u.notifiedVer {
				u.notifiedVer = cand.Version
				a.openUpdateWindow()
			}
		})
	}()
}

// userBusy 报告用户是否正处于截图选区、文字输入、贴图编辑、长截图或倒计时中；
// 这些时刻后台弹窗会抢走焦点打断操作。
func (a *app) userBusy() bool {
	if a.session != nil && a.session.Phase() != session.PhaseIdle {
		return true
	}
	if a.textEditing || a.scrollCap != nil || a.countdown != nil {
		return true
	}
	for _, p := range a.pins {
		if p != nil && p.editMode {
			return true
		}
	}
	return false
}

func (a *app) startUpdateDownload() {
	u := a.upd
	if u == nil || u.release == nil || u.isBusyDownloading() {
		return
	}
	backend := u.activeBackend()
	u.canSelfUpdate = backend.Enabled()
	if !u.canSelfUpdate {
		a.openReleasePage(u.release)
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
	u.total = u.release.Size
	u.errMsg = ""
	u.invalidateUI()

	candCopy := *u.release

	go func() {
		err := backend.Install(ctx, &candCopy, func(dl, total int64) {
			u.runMain(func() {
				if a.quitting || a.upd != u || u.opSeq != seq {
					return
				}
				if total > 0 && dl >= total {
					u.state = updVerifying
				} else {
					u.state = updDownloading
				}
				u.downloaded = dl
				u.total = total
				u.invalidateUI()
			})
		})
		u.runMain(func() {
			if a.quitting || a.upd != u || u.opSeq != seq {
				return
			}
			u.opCancel = nil
			if err != nil {
				if errors.Is(err, context.Canceled) {
					u.state = updAvailable
					u.errMsg = ""
				} else {
					log.Printf("updater download/install failed: %v", err)
					u.state = updError
					u.errMsg = friendlyUpdateError(err)
				}
				u.invalidateUI()
				a.refreshMenu()
				a.invalidateSettingsWin()
				return
			}

			u.stagedVersion = candCopy.Version
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
	if u == nil || u.stagedVersion == "" {
		return
	}
	u.activeBackend().Relaunch()
}

func (a *app) openReleasePage(rel *updateCandidate) {
	target := fmt.Sprintf("https://github.com/%s/releases/latest", updateGitHubRepo)
	if rel != nil && strings.TrimSpace(rel.Version) != "" {
		target = fmt.Sprintf("https://github.com/%s/releases/tag/%s", updateGitHubRepo, formatVersion(rel.Version))
	}
	go func() { _ = mygo.Shell.OpenExternal(target) }()
}

func (a *app) openUpdateWindow() {
	u := a.upd
	if u == nil {
		return
	}
	if u.onOpen != nil {
		u.onOpen()
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
			// 1. 顶部无框拖拽标题栏（状态徽标右对齐）
			paintPanelHeader(c, iconUpdate, "SnipGo 软件更新", "关闭更新窗口", "关闭 (⌘W / Esc)", func() {
				if u.win != nil {
					a.closeUpdateWindow()
				}
			}, func() {
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
			})

			// 2. 主内容区
			body := ui.Column(c).Grow(1).Padding(4, 18, 4, 18).Gap(12)
			body.Children(func() {
				a.paintUpdateHero(c, p, u)
				a.paintUpdateBody(c, p, u)
			})

			// 3. 底部浮动操作栏
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
	cur := formatVersion(currentAppVersion())
	target := ""
	if u.release != nil {
		target = formatVersion(u.release.Version)
	} else if u.stagedVersion != "" {
		target = formatVersion(u.stagedVersion)
	}

	switch u.state {
	case updChecking:
		return "正在检查 SnipGo 更新…", fmt.Sprintf("当前运行版本：%s", cur)
	case updAvailable:
		sizeStr := ""
		if u.release != nil && u.release.Size > 0 {
			sizeStr = " · 约 " + formatBytes(u.release.Size)
		}
		return fmt.Sprintf("SnipGo %s 现已可用", target), fmt.Sprintf("当前 %s → 最新 %s%s", cur, target, sizeStr)
	case updDownloading:
		return fmt.Sprintf("正在下载 SnipGo %s…", target), fmt.Sprintf("当前 %s → %s · Ed25519 签名与增量更新校验", cur, target)
	case updVerifying:
		return "正在校验签名并应用更新…", "核对 Ed25519 数字签名与安装包完整性"
	case updExtracting:
		return "正在解包并准备原子替换…", "准备就绪后可一键平滑重启"
	case updReady:
		return fmt.Sprintf("SnipGo %s 已下载并验签", target), "重启后立即切换新版，保留全部快捷键与截图历史"
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
			sc := ui.Scroll(c).Fill().Padding(12, 14).ScrollbarInsets(8, 0, 8)
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
		})

		if !u.canSelfUpdate {
			ui.Row(c).Padding(7, 11).Gap(6).AlignItems(ui.Center).Radius(10).
				Background(p.BadgeInfoBg).Border(1, p.BadgeInfoBd).
				Children(func() {
					ui.Text(c, "当前安装位置不可直接原地替换（或未签名启用自动更新），点击右侧按钮可在浏览器下载安装").
						FontSize(11).TextColor(p.BadgeInfoFg)
				})
		}

	case updDownloading, updVerifying, updExtracting:
		progCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 18).Gap(12).Justify(ui.Center))
		progCard.Children(func() {
			var frac float32
			stageLabel := "正在连接更新下载通道…"
			rightLabel := ""
			switch u.state {
			case updDownloading:
				if u.total > 0 {
					frac = max(0.03, min(1.0, float32(u.downloaded)/float32(u.total)))
					pct := int(frac * 100)
					stageLabel = fmt.Sprintf("已下载 %s / %s", formatBytes(u.downloaded), formatBytes(u.total))
					rightLabel = fmt.Sprintf("%d%%", pct)
				} else {
					frac = 0.35
					stageLabel = fmt.Sprintf("已下载 %s…", formatBytes(u.downloaded))
				}
			case updVerifying:
				frac = 0.92
				stageLabel = "正在验证 Ed25519 签名并准备替换 .app…"
				rightLabel = "92%"
			case updExtracting:
				frac = 0.97
				stageLabel = "正在解包并完成替换准备…"
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

			ui.Text(c, "更新包通过 Ed25519 签名校验后才会在退出时原子替换 .app，不会中断当前贴图或录屏工作。").
				FontSize(11.5).TextColor(p.TextMuted)
		})

	case updReady:
		readyCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 20).Gap(10).Justify(ui.Center).AlignItems(ui.Center))
		readyCard.Children(func() {
			ui.Row(c).Height(28).Padding(0, 12).Gap(6).AlignItems(ui.Center).Radius(14).
				Background(p.BadgeSuccessBg).Border(1, p.BadgeSuccessBd).
				Children(func() {
					ui.Icon(c, iconCheck).FontSize(13.5).TextColor(p.BadgeSuccessFg)
					ui.Text(c, "Ed25519 签名与安装包校验已通过").FontSize(12).FontWeight(600).TextColor(p.BadgeSuccessFg)
				})
			ui.Text(c, "新版 SnipGo 已准备就绪，点击下方「立即重启应用」即可完成替换并重新打开。").
				FontSize(12.5).LineHeight(1.5).TextColor(p.Text)
			ui.Text(c, "若选择「下次启动生效」，新版本会在你下一次退出 SnipGo 后自动无感完成替换。").
				FontSize(11.5).TextColor(p.TextMuted)
		})

	default:
		statusCard := liquidCard(c, ui.Column(c).Grow(1).Padding(18, 20).Gap(10).Justify(ui.Center).AlignItems(ui.Center))
		statusCard.Children(func() {
			var ic *ui.SVG
			var col ui.Color
			var headline, sub string
			switch u.state {
			case updChecking:
				ic, col = iconUpdate, p.Accent
				headline = "正在拉取签名更新清单…"
				sub = "读取 GitHub Releases 静态签名索引"
			case updUpToDate:
				ic, col = iconCheck, p.BadgeSuccessFg
				headline = "当前安装的 SnipGo 已是最新正式版"
				sub = "无需更新，所有功能均处于最新状态"
			case updError:
				ic, col = iconClose, ui.RGB(235, 68, 60)
				headline = "检查或下载更新失败"
				sub = u.errMsg
				if sub == "" {
					sub = "网络请求失败，请检查网络连接或系统代理设置"
				}
			}
			if ic != nil {
				ui.Box(c).Size(44, 44).Radius(22).Background(col.Alpha(0.14)).
					AlignItems(ui.Center).Justify(ui.Center).
					Children(func() {
						ui.Icon(c, ic).FontSize(20).TextColor(col)
					})
			}
			ui.Text(c, headline).FontSize(13).FontWeight(600).TextColor(p.Text)
			if sub != "" {
				subCol := p.TextMuted
				if u.state == updError {
					subCol = ui.RGB(235, 68, 60)
				}
				ui.Text(c, sub).FontSize(11.5).LineHeight(1.45).TextColor(subCol)
			}
			if !u.lastChecked.IsZero() {
				ui.Text(c, "上次检查时间："+u.lastChecked.Format("15:04:05")).
					FontSize(11).FontFeatures("tnum").TextColor(p.TextMuted)
			}
		})
	}
}

func (a *app) paintUpdateFooter(c *ui.Context, p palette, u *updateManager) {
	footer := ui.Row(c).Height(54).Padding(0, 18).Gap(8).AlignItems(ui.Center).ZIndex(2)
	footer.Children(func() {
		switch u.state {
		case updAvailable:
			liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
				if glassGroupBtn(c, add, nil, "跳过", "跳过此版本，以后不再自动弹窗提示") {
					if u.release != nil {
						a.cfg.SkippedVersion = u.release.Version
						if a.userDataDir != "" {
							if err := config.Save(a.userDataDir, a.cfg); err != nil {
								log.Printf("save settings: %v", err)
							}
						}
						u.release = nil
						u.state = updIdle
						a.refreshMenu()
						a.invalidateSettingsWin()
					}
					go mygo.RunOnMain(func() { a.closeUpdateWindow() })
				}
				if glassGroupBtn(c, add, nil, "稍后", "暂时关闭更新窗口") {
					go mygo.RunOnMain(func() { a.closeUpdateWindow() })
				}
			})
			ui.Spacer(c)
			if actionPillBtn(c, iconExternal, "发布页", "在浏览器中查看发布详情", false, true) {
				a.openReleasePage(u.release)
			}
			if u.canSelfUpdate && u.release != nil {
				if actionPillBtn(c, iconUpdate, "立即更新", "下载、验签并准备重启升级", true, false) {
					a.startUpdateDownload()
				}
			} else {
				if actionPillBtn(c, iconExternal, "前往下载", "在浏览器打开 GitHub 发布页", true, false) {
					a.openReleasePage(u.release)
				}
			}

		case updDownloading, updVerifying, updExtracting:
			ui.Text(c, "更新包通过 Ed25519 验签完成后才会生效").FontSize(11).TextColor(p.TextMuted)
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
				a.openReleasePage(nil)
			}
			ui.Spacer(c)
			if actionPillBtn(c, iconCheck, "好", "关闭窗口", true, false) {
				go mygo.RunOnMain(func() { a.closeUpdateWindow() })
			}

		case updError:
			if miniPillBtn(c, iconExternal, "手动下载", false) {
				a.openReleasePage(u.release)
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

func formatVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + v[1:]
	}
	return "v" + v
}

func formatBytes(n int64) string {
	if n < 0 {
		return "0 B"
	}
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
}

func friendlyUpdateError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, mygo.ErrUpdatesDisabled) {
		return "当前构建未启用应用内签名更新（如开发模式或无写入权限目录），请前往 GitHub Releases 手动下载安装。"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "检查或下载更新超时，请检查网络连接或系统代理设置后重试。"
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "404") || strings.Contains(low, "not found"):
		return "最新 Release 尚未附带签名更新清单（update-*.json），可点击「手动下载」前往发布页安装。"
	case strings.Contains(low, "403"):
		return "预览版检索请求受限（HTTP 403）；关闭「接收测试预览版」可直接走稳定版静态通道，或稍后重试。"
	case strings.Contains(low, "not signed") || strings.Contains(low, "signature"):
		return "更新包 Ed25519 数字签名校验未通过，已中止更新以确保安全。"
	case strings.Contains(low, "timeout") || strings.Contains(low, "timed out"):
		return "网络连接超时，请检查代理或网络状况后重试。"
	default:
		return msg
	}
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
