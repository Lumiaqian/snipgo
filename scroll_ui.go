package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/scrollcap"
)

const (
	scrollCtrlW = 264
	scrollCtrlH = 340

	// 预览位图宽度：面板内预览区约 208pt，按 2x 屏取 480px 足够清晰，避免把整张长图转成位图。
	scrollPreviewMaxW = 480
	// 预览位图更新的最小间隔，避免每帧都重建位图。
	scrollPreviewInterval = 250 * time.Millisecond
)

// scrollCappedText 达到长度上限时在控制面板显示的提示。
var scrollCappedText = fmt.Sprintf("已达长度上限（%d px）", scrollcap.DefaultMaxHeight)

type scrollCaptureSession struct {
	sel      image.Rectangle
	frameWin *mygo.Window
	ctrlWin  *mygo.Window
	cancel   context.CancelFunc

	mu         sync.Mutex
	stitch     *scrollcap.Stitcher
	preview    *ui.Bitmap
	pxW        int
	pxH        int
	frames     int
	capped     bool // 已达最大长度，后续帧被拒绝
	generation uint64
	paused     bool
	done       bool

	lastPreview time.Time // 上次生成预览的时间
	previewDue  bool      // 有尚未体现在预览中的新帧（节流期间累积）
}

func (a *app) startScrollCapture(sel image.Rectangle) {
	sel = sel.Canon()
	if sel.Dx() < 32 || sel.Dy() < 48 {
		return
	}
	a.stopScrollCapture()
	if a.session != nil {
		a.session.Cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	sc := &scrollCaptureSession{
		sel:    sel,
		cancel: cancel,
		stitch: scrollcap.New(),
	}
	a.scrollCap = sc

	// 1. 穿透式绿色取景虚线框窗口（鼠标事件完全穿透到底层应用供自由滚轮，且开启硬件截图保护绝不入镜）。
	pad := 3
	fx, fy := sel.Min.X-pad, sel.Min.Y-pad
	fw, fh := sel.Dx()+pad*2, sel.Dy()+pad*2
	sc.frameWin = mygo.NewWindow(mygo.WindowOptions{
		Title:             overlayTitle,
		Width:             fw,
		Height:            fh,
		X:                 40,
		Y:                 40,
		Frameless:         true,
		Transparent:       true,
		DisableResize:     true,
		DisableMaximize:   true,
		DisableShadow:     true,
		Panel:             true,
		AlwaysOnTop:       true,
		IgnoreMouseEvents: true,
		Content: ui.View(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Size(float32(fw), float32(fh)).
				Border(2, ui.RGB(52, 199, 89)).
				IgnoreInput(true)
		}),
	})
	sc.frameWin.SetContentProtection(true)
	preparePinWindow(sc.frameWin)
	sc.frameWin.SetIgnoreMouseEvents(true)
	sc.frameWin.SetBounds(mygo.Rectangle{X: fx, Y: fy, Width: fw, Height: fh})

	// 2. 右侧（或左侧）悬浮实时长图预览与完成控制面板
	ctrlPos := a.placeSidePanel(sel, scrollCtrlW, scrollCtrlH)
	sc.ctrlWin = mygo.NewWindow(mygo.WindowOptions{
		Title:           overlayTitle,
		Width:           scrollCtrlW,
		Height:          scrollCtrlH,
		X:               ctrlPos.X,
		Y:               ctrlPos.Y,
		Frameless:       true,
		Transparent:     true,
		Vibrancy:        vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius:    panelCornerRadius,
		Panel:           true,
		AlwaysOnTop:     true,
		DisableResize:   true,
		DisableMinimize: true,
		DisableMaximize: true,
		Content: ui.View(func(c *ui.Context) {
			a.paintScrollCtrlView(c, sc)
		}),
	})
	sc.ctrlWin.SetContentProtection(true)
	applyWindowTheme(sc.ctrlWin, themeStateFromConfig(a.cfg))
	sc.ctrlWin.OnClosed(func() {
		if a.scrollCap == sc {
			a.stopScrollCapture()
		}
	})

	// 3. 后台定时采样选区像素并增量拼接
	go a.runScrollCaptureLoop(ctx, sc)
}

func (a *app) placeSidePanel(anchor image.Rectangle, w, h int) image.Point {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		return image.Pt(anchor.Max.X+14, anchor.Min.Y)
	}
	screen := image.Rect(
		displays[0].WorkArea.X,
		displays[0].WorkArea.Y,
		displays[0].WorkArea.X+displays[0].WorkArea.Width,
		displays[0].WorkArea.Y+displays[0].WorkArea.Height,
	)
	mid := image.Pt((anchor.Min.X+anchor.Max.X)/2, (anchor.Min.Y+anchor.Max.Y)/2)
	for _, d := range displays {
		wa := image.Rect(d.WorkArea.X, d.WorkArea.Y, d.WorkArea.X+d.WorkArea.Width, d.WorkArea.Y+d.WorkArea.Height)
		if mid.In(wa) {
			screen = wa
			break
		}
	}
	x := anchor.Max.X + 14
	if x+w > screen.Max.X-8 {
		x = anchor.Min.X - w - 14
	}
	x = max(screen.Min.X+8, min(x, screen.Max.X-w-8))
	y := max(screen.Min.Y+8, min(anchor.Min.Y, screen.Max.Y-h-8))
	return image.Pt(x, y)
}

func (sc *scrollCaptureSession) togglePause() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.paused = !sc.paused
	sc.generation++
}

// Caller holds sc.mu while checking and committing the frame.
func (sc *scrollCaptureSession) acceptsFrame(generation uint64) bool {
	return !sc.done && !sc.paused && sc.generation == generation
}

func (a *app) runScrollCaptureLoop(ctx context.Context, sc *scrollCaptureSession) {
	captureOnce := func() {
		sc.mu.Lock()
		generation := sc.generation
		skip := !sc.acceptsFrame(generation)
		sc.mu.Unlock()
		if skip {
			return
		}
		shotCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		img, err := a.captureRect(shotCtx, sc.sel)
		if err != nil || img == nil || img.Bounds().Empty() {
			return
		}
		sc.mu.Lock()
		if !sc.acceptsFrame(generation) {
			sc.mu.Unlock()
			return
		}
		_, changed := sc.stitch.Push(img)
		if changed {
			sc.pxW, sc.pxH = sc.stitch.Size()
			sc.frames = sc.stitch.Frames()
			sc.previewDue = true
		}
		capped := sc.stitch.Capped()
		capChanged := capped != sc.capped
		sc.capped = capped
		// 预览只做缩小合成（增量、与新增高度成正比）并节流；完整拼图仅在完成时生成。
		var pv *image.NRGBA
		if sc.previewDue && time.Since(sc.lastPreview) >= scrollPreviewInterval {
			pv = sc.stitch.Preview(scrollPreviewMaxW)
			sc.lastPreview = time.Now()
			sc.previewDue = false
		}
		sc.mu.Unlock()

		if pv == nil && !changed && !capChanged {
			return
		}
		mygo.RunOnMain(func() {
			if a.scrollCap != sc {
				return
			}
			if pv != nil {
				bmp := ui.NewBitmap(pv)
				sc.mu.Lock()
				sc.preview = bmp
				sc.mu.Unlock()
			}
			if sc.ctrlWin != nil && !sc.ctrlWin.IsDestroyed() {
				sc.ctrlWin.Invalidate()
			}
		})
	}

	captureOnce()
	ticker := time.NewTicker(160 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			captureOnce()
		}
	}
}

func (a *app) finishScrollCapture(mode string) {
	sc := a.scrollCap
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.done = true
	res := sc.stitch.Result()
	sel := sc.sel
	sc.mu.Unlock()
	a.stopScrollCapture()

	if res == nil || res.Bounds().Empty() {
		return
	}
	go func() {
		var buf bytes.Buffer
		if png.Encode(&buf, res) != nil {
			return
		}
		pngBytes := buf.Bytes()
		a.recordHistory(pngBytes, res.Bounds().Dx(), res.Bounds().Dy(), 0)
		mygo.RunOnMain(func() {
			switch mode {
			case "copy":
				err := mygo.Clipboard.WriteImage(pngBytes)
				msg := copyResultMessage(err, fmt.Sprintf("长截图已复制（%d×%d px）", res.Bounds().Dx(), res.Bounds().Dy()))
				a.sessionStatus(msg)
				a.showActionToast(msg)
			case "pin":
				dipW, dipH, zoom := longPinGeometry(res.Bounds().Dx(), res.Bounds().Dy(), sel.Dx())
				a.showPinWithState(res, pngBytes, sel.Min, dipW, dipH, zoom, 100, false)
			case "saveAs":
				a.SavePNGAs(pngBytes)
			case "save":
				a.SavePNG(pngBytes)
			}
		})
	}()
}

// longPinGeometry 返回长截图贴图的自然 DIP 尺寸与初始缩放：
// 保持原始宽高比（编辑坐标与图像一致），用 zoom 让高度不超过 680。
func longPinGeometry(pxW, pxH, selDIPW int) (dipW, dipH int, zoom float64) {
	scale := max(1.0, float64(pxW)/float64(max(1, selDIPW)))
	dipW = max(80, int(float64(pxW)/scale))
	dipH = max(80, int(float64(pxH)/scale))
	zoom = min(1.0, 680.0/float64(dipH))
	return
}

func (a *app) stopScrollCapture() {
	sc := a.scrollCap
	if sc == nil {
		return
	}
	a.scrollCap = nil
	sc.mu.Lock()
	sc.done = true
	sc.mu.Unlock()
	if sc.cancel != nil {
		sc.cancel()
	}
	if sc.frameWin != nil && !sc.frameWin.IsDestroyed() {
		sc.frameWin.Close()
	}
	if sc.ctrlWin != nil && !sc.ctrlWin.IsDestroyed() {
		sc.ctrlWin.Close()
	}
}

func (a *app) paintScrollCtrlView(c *ui.Context, sc *scrollCaptureSession) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		if handlePanelWindowShortcuts(c, func() { a.stopScrollCapture() }) {
			return
		}
		if !c.FocusTakesText() {
			if c.Shortcut(0, ui.KeySpace) {
				sc.togglePause()
				if sc.ctrlWin != nil {
					sc.ctrlWin.Invalidate()
					return
				}
			} else if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
				go mygo.RunOnMain(func() { a.finishScrollCapture("saveAs") })
				return
			} else if c.Shortcut(ui.Cmd, ui.KeyS) {
				go mygo.RunOnMain(func() { a.finishScrollCapture("save") })
				return
			} else if c.Shortcut(ui.Cmd, ui.KeyT) || c.Shortcut(0, ui.KeyT) {
				go mygo.RunOnMain(func() { a.finishScrollCapture("pin") })
				return
			} else if c.Shortcut(0, ui.KeyEnter) {
				go mygo.RunOnMain(func() { a.finishScrollCapture("copy") })
				return
			}
		}

		sc.mu.Lock()
		bmp := sc.preview
		pxW, pxH, frames, capped, paused := sc.pxW, sc.pxH, sc.frames, sc.capped, sc.paused
		sc.mu.Unlock()

		p := uiPalette(c)
		card := liquidGlassPanel(c, ui.Column(c))

		card.Children(func() {
			// 无框拖拽标题栏 + 右上实时状态胶囊
			paintPanelHeader(c, iconScrollCap, "滚动长截图", "取消长截图", "取消 (⌘W / Esc)", func() {
				a.stopScrollCapture()
			}, func() {
				badgeBg, badgeBd, badgeFg := p.BadgeSuccessBg, p.BadgeSuccessBd, p.BadgeSuccessFg
				dotCol := ui.RGB(52, 199, 89)
				badgeLabel := "拼接中"
				if paused {
					badgeBg, badgeBd, badgeFg = p.BadgeInfoBg, p.BadgeInfoBd, p.BadgeInfoFg
					dotCol = ui.RGB(255, 159, 10)
					badgeLabel = "已暂停"
				}
				ui.Row(c).Height(20).Padding(0, 8).Gap(5).AlignItems(ui.Center).Radius(10).
					Background(badgeBg).Border(1, badgeBd).
					Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(dotCol)
						ui.Text(c, badgeLabel).FontSize(10.5).FontWeight(500).NoWrap().TextColor(badgeFg)
					})
			})

			// 固定宽度的实时视口：纵向长图自动居中满宽并锁定底部最新拼接帧
			previewArea := ui.Column(c).
				Grow(1).
				Padding(0, 14, 0, 14).
				Gap(8).
				AlignItems(ui.Center)
			previewArea.Children(func() {
				viewCard := ui.Box(c).
					Grow(1).
					FillWidth().
					Radius(14).
					Clip().
					Background(p.StageBg).
					Border(1, ui.RGBA(52, 199, 89, 0.45)).
					AlignItems(ui.Center).
					Justify(ui.Center)
				viewCard.Children(func() {
					if bmp != nil && pxW > 0 && pxH > 0 {
						if pxH > pxW {
							ui.Image(c, bmp).Fill().Fit(ui.Cover).ObjectPosition(0.5, 1.0)
						} else {
							ui.Image(c, bmp).Fill().Fit(ui.Contain)
						}
					} else {
						ui.Text(c, "准备采集中…").FontSize(12).TextColor(p.TextMuted)
					}
				})

				if capped {
					ui.Text(c, scrollCappedText).FontSize(11).NoWrap().TextColor(p.TextSecondary)
				}
				if frames > 1 {
					ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
						ui.Text(c, fmt.Sprintf("%d × %d px · 已拼接", pxW, pxH)).FontSize(11).FontFeatures("tnum").NoWrap().TextColor(p.TextSecondary)
						ui.Text(c, fmt.Sprintf("%d", frames)).FontSize(11).FontWeight(700).FontFeatures("tnum").NoWrap().TextColor(p.Accent).
							ContentTransition(ui.ContentTransition{Numeric: true})
						ui.Text(c, "帧").FontSize(11).NoWrap().TextColor(p.TextSecondary)
					})
				} else {
					statTxt := "请在绿框内缓慢向下滚动鼠标"
					if pxH > 0 {
						statTxt = fmt.Sprintf("%d × %d px · 向下滚轮开始拼接", pxW, pxH)
					}
					ui.Text(c, statTxt).FontSize(11).FontFeatures("tnum").NoWrap().TextColor(p.TextSecondary)
				}
			})

			// 底部操作栏
			footer := ui.Row(c).
				Height(48).
				Padding(0, 14).
				Gap(6).
				AlignItems(ui.Center)
			footer.Children(func() {
				liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
					pauseIc, pauseLabel, pauseTip := iconPause, "暂停拼接", "暂停自动拼接 (Space)"
					if paused {
						pauseIc, pauseLabel, pauseTip = iconPlay, "继续拼接", "继续自动拼接 (Space)"
					}
					if iconToolBtn(c, pauseIc, pauseLabel, pauseTip, paused, false) {
						sc.togglePause()
						if sc.ctrlWin != nil {
							sc.ctrlWin.Invalidate()
						}
					}
					if iconToolBtn(c, iconPin, "长图贴图", "转为桌面贴图 (T)", false, false) {
						go mygo.RunOnMain(func() { a.finishScrollCapture("pin") })
					}
					if iconToolBtn(c, iconSave, "保存长图", "保存长图 PNG (⌘S / ⇧⌘S)", false, false) {
						go mygo.RunOnMain(func() { a.finishScrollCapture("save") })
					}
				})
				ui.Spacer(c)
				if actionPillBtn(c, iconCheck, "完成复制", "完成并复制到剪贴板 (Enter)", true, false) {
					go mygo.RunOnMain(func() { a.finishScrollCapture("copy") })
				}
			})
		})
	})
}
