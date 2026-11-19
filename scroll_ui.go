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
	scrollCtrlW = 236
	scrollCtrlH = 340
)

type scrollCaptureSession struct {
	sel      image.Rectangle
	frameWin *mygo.Window
	ctrlWin  *mygo.Window
	cancel   context.CancelFunc

	mu      sync.Mutex
	stitch  *scrollcap.Stitcher
	preview *ui.Bitmap
	pxW     int
	pxH     int
	frames  int
	done    bool
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

func (a *app) runScrollCaptureLoop(ctx context.Context, sc *scrollCaptureSession) {
	captureOnce := func() {
		shotCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		img, err := a.captureRect(shotCtx, sc.sel)
		if err != nil || img == nil || img.Bounds().Empty() {
			return
		}
		sc.mu.Lock()
		if sc.done {
			sc.mu.Unlock()
			return
		}
		_, changed := sc.stitch.Push(img)
		if !changed {
			sc.mu.Unlock()
			return
		}
		res := sc.stitch.Result()
		w, h := sc.stitch.Size()
		frames := sc.stitch.Frames()
		sc.pxW, sc.pxH, sc.frames = w, h, frames
		sc.mu.Unlock()

		if res == nil {
			return
		}
		mygo.RunOnMain(func() {
			if a.scrollCap != sc {
				return
			}
			sc.mu.Lock()
			sc.preview = ui.NewBitmap(res)
			sc.mu.Unlock()
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
		a.recordHistory(pngBytes, res.Bounds().Dx(), res.Bounds().Dy())
		mygo.RunOnMain(func() {
			switch mode {
			case "copy":
				_ = mygo.Clipboard.WriteImage(pngBytes)
				a.sessionStatus(fmt.Sprintf("长截图已复制（%d×%d px）", res.Bounds().Dx(), res.Bounds().Dy()))
			case "pin":
				scale := max(1.0, float64(res.Bounds().Dx())/float64(max(1, sel.Dx())))
				dipW := max(80, int(float64(res.Bounds().Dx())/scale))
				dipH := max(80, min(680, int(float64(res.Bounds().Dy())/scale)))
				a.ShowPin(res, pngBytes, sel.Min, dipW, dipH)
			case "save":
				a.SavePNG(pngBytes)
			}
		})
	}()
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
		if c.Shortcut(0, ui.KeyEnter) {
			go mygo.RunOnMain(func() { a.finishScrollCapture("copy") })
			return
		}

		sc.mu.Lock()
		bmp := sc.preview
		pxW, pxH, frames := sc.pxW, sc.pxH, sc.frames
		sc.mu.Unlock()

		p := uiPalette(c)
		card := liquidGlassPanel(c, ui.Column(c))

		card.Children(func() {
			// 标题栏
			header := ui.Row(c).
				Height(44).
				Padding(0, 12).
				Gap(6).
				AlignItems(ui.Center).
				DragWindow()
			header.Children(func() {
				if macTrafficCloseBtn(c, "取消长截图", "取消 (⌘W / Esc)") {
					go mygo.RunOnMain(func() { a.stopScrollCapture() })
				}
				ui.Box(c).Width(2)
				ui.Box(c).Size(8, 8).Radius(4).Background(ui.RGB(52, 199, 89))
				ui.Text(c, "滚动长截图").FontSize(12.5).FontWeight(600).NoWrap().TextColor(p.Text)
				ui.Spacer(c)
			})
			panelHairline(c)

			// 实时缩略预览区
			previewArea := ui.Column(c).
				Grow(1).
				Padding(10, 10).
				Gap(6).
				AlignItems(ui.Center).
				Justify(ui.Center)
			previewArea.Children(func() {
				if bmp != nil && pxW > 0 && pxH > 0 {
					maxW, maxH := float32(scrollCtrlW-28), float32(186)
					aspect := float32(pxW) / float32(pxH)
					dispH := maxH
					dispW := dispH * aspect
					if dispW > maxW {
						dispW = maxW
						dispH = dispW / aspect
					}
					ui.Box(c).Size(dispW, dispH).Radius(10).Clip().
						Background(p.StageBg).
						Border(1, ui.RGBA(52, 199, 89, 0.55)).
						Children(func() {
							ui.Image(c, bmp).Size(dispW, dispH)
						})
				} else {
					ui.Text(c, "准备采集中…").FontSize(12).TextColor(p.TextMuted)
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

			panelHairline(c)

			// 底部完成按钮栏
			footer := ui.Row(c).
				Height(46).
				Padding(0, 10).
				Gap(5).
				AlignItems(ui.Center)
			footer.Children(func() {
				if iconToolBtn(c, iconPin, "长图贴图", "转为桌面贴图", false, false) {
					go mygo.RunOnMain(func() { a.finishScrollCapture("pin") })
				}
				if iconToolBtn(c, iconSave, "保存长图", "保存长图 PNG", false, false) {
					go mygo.RunOnMain(func() { a.finishScrollCapture("save") })
				}
				ui.Spacer(c)
				if actionPillBtn(c, iconCheck, "完成复制", "完成并复制到剪贴板 (Enter)", true, false) {
					go mygo.RunOnMain(func() { a.finishScrollCapture("copy") })
				}
			})
		})
	})
}
