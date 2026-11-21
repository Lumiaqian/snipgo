package main

import (
	"context"
	"fmt"
	"image"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type countdownSession struct {
	win       *mygo.Window
	cancel    context.CancelFunc
	remaining int
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
