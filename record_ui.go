package main

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/recordsess"
	"snipgo/internal/videocap"
	"strings"
)

type regionRecording struct {
	countdown                              *recordCountdown
	region                                 recordsess.Region
	stream                                 videocap.Recorder
	frame, ctrl                            *mygo.Window
	path                                   string
	level                                  float64
	interrupted                            bool
	started, pausedAt, confirmUntil        time.Time
	removed                                time.Duration
	busy, system, mic, hadAudio, micDenied bool
}

func (r *regionRecording) elapsed() time.Duration {
	if r.started.IsZero() {
		return 0
	}
	now := time.Now()
	if !r.pausedAt.IsZero() {
		now = r.pausedAt
	}
	return now.Sub(r.started) - r.removed
}

func (r *regionRecording) clock() string {
	sec := int(r.elapsed().Seconds())
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func (r *regionRecording) pauseLabel() string {
	if !r.pausedAt.IsZero() {
		return "继续"
	}
	return "暂停"
}

func (r *regionRecording) discardLabel() string {
	if time.Now().Before(r.confirmUntil) {
		return "确认丢弃?"
	}
	return "取消"
}

func (r *regionRecording) menu(a *app) []*mygo.MenuItem {
	if cd := r.countdown; cd != nil {
		return []*mygo.MenuItem{{Label: "立即开始录屏", Click: func(*mygo.MenuItem, *mygo.Window) { cd.resolve(true) }}, {Label: "取消录屏倒计时", Click: func(*mygo.MenuItem, *mygo.Window) { cd.resolve(false) }}}
	}
	return []*mygo.MenuItem{{Label: "停止录制（" + r.clock() + "）", Click: func(*mygo.MenuItem, *mygo.Window) { a.endRecording(false) }}, {Label: r.pauseLabel() + "录制", Click: func(*mygo.MenuItem, *mygo.Window) { a.changeRecording("pause") }}, {Label: r.discardLabel() + "录制", Click: func(*mygo.MenuItem, *mygo.Window) { a.endRecording(true) }}}
}

func (a *app) startRecording(sel image.Rectangle) {
	if a.recording != nil {
		return
	}
	if allowed, _ := a.allowCapture(); !allowed {
		return
	}
	displays, err := a.currentDisplays()
	if err != nil {
		a.showActionToast(err.Error())
		return
	}
	region, err := recordsess.Select(sel, displays)
	if err != nil {
		a.showActionToast(err.Error())
		return
	}
	stream, err := videocap.NewRecorder()
	if err != nil {
		a.showActionToast(err.Error())
		return
	}
	root := filepath.Join(os.TempDir(), "snipgo-recordings")
	if err = os.MkdirAll(root, 0700); err != nil {
		a.showActionToast(err.Error())
		stream.Close()
		return
	}
	dir, err := os.MkdirTemp(root, "clip-")
	if err != nil {
		stream.Close()
		a.showActionToast(err.Error())
		return
	}
	r := &regionRecording{region: region, stream: stream, path: filepath.Join(dir, "SnipGo.mp4"), busy: true}
	a.recording = r
	a.session.Cancel()
	if a.cfg.RecordCountdown {
		r.countdown = &recordCountdown{deadline: time.Now().Add(3 * time.Second)}
		r.countdown.finish = func(start bool) {
			a.deferUI(func() {
				r.ctrl.Hide()
				r.countdown = nil
				if !start {
					a.closeRecording(r)
					os.RemoveAll(dir)
					return
				}
				a.beginRegionStream(r)
			})
		}
	}
	a.openRecordingControls(r)
	if region.Global != sel {
		a.showActionToast("将录制选区覆盖面积最大的显示器")
	}
	if cd := r.countdown; cd != nil {
		cd.timer = time.AfterFunc(time.Until(cd.deadline), func() { runOnMain(func() { cd.resolve(true) }) })
		r.ctrl.Focus()
		return
	}
	a.beginRegionStream(r)
}

type recordCountdown struct {
	deadline time.Time
	timer    *time.Timer
	finish   func(bool)
}

// Timer and input events resolve on the main thread. A queued event may outlive its window.
func (cd *recordCountdown) resolve(start bool) {
	if cd.finish == nil {
		return
	}
	finish := cd.finish
	cd.finish = nil
	if cd.timer != nil {
		cd.timer.Stop()
	}
	finish(start)
}

func (a *app) beginRegionStream(r *regionRecording) {
	region := r.region
	go func() {
		l := region.Local
		err := r.stream.Start(context.Background(), r.path, region.Display.ID, int64(l.Min.X), int64(l.Min.Y), int64(l.Dx()), int64(l.Dy()), region.Display.Scale)
		runOnMain(func() {
			r.busy = false
			if err != nil {
				a.closeRecording(r)
				os.RemoveAll(filepath.Dir(r.path))
				a.showActionToast(err.Error())
				return
			}
			r.started = time.Now()
			a.updateRecordingStatus(r)
			r.ctrl.Invalidate()
			r.ctrl.ShowInactive()
			a.refreshMenu()
		})
	}()
}

func (a *app) openRecordingControls(r *regionRecording) {
	b := r.region.Global.Inset(-3)
	r.frame = mygo.NewWindow(mygo.WindowOptions{
		Title:             overlayTitle,
		Width:             b.Dx(),
		Height:            b.Dy(),
		Frameless:         true,
		Transparent:       true,
		AlwaysOnTop:       true,
		Panel:             true,
		DisableShadow:     true,
		IgnoreMouseEvents: true,
		Content: ui.View(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			col := ui.RGB(255, 69, 58)
			if !r.pausedAt.IsZero() {
				col = ui.RGB(255, 159, 10)
			}
			ui.Box(c).Size(float32(b.Dx()), float32(b.Dy())).Border(2, col).IgnoreInput(true)
		})})
	r.frame.SetContentProtection(true)
	preparePinWindow(r.frame)
	r.frame.SetIgnoreMouseEvents(true)
	r.frame.SetBounds(mygo.Rectangle{X: b.Min.X, Y: b.Min.Y, Width: b.Dx(), Height: b.Dy()})
	work := r.region.Display.Bounds
	for _, d := range mygo.Screen.Displays() {
		if d.ID == r.region.Display.ID {
			w := d.WorkArea
			work = image.Rect(w.X, w.Y, w.X+w.Width, w.Y+w.Height)
			break
		}
	}
	pos := behaviors.PlaceFloating(work, r.region.Global, image.Pt(370, 44), behaviors.PlacementOptions{PreferredSide: behaviors.SideBottom, Align: behaviors.AlignCenter, Gap: 10}).Point
	r.ctrl = mygo.NewWindow(mygo.WindowOptions{
		Title:         "SnipGo 录制控制",
		Width:         370,
		Height:        44,
		X:             pos.X,
		Y:             pos.Y,
		Frameless:     true,
		Panel:         true,
		AlwaysOnTop:   true,
		DisableResize: true,
		Transparent:   true,
		Vibrancy:      vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius:  22,
		Hidden:        true,
		Content: ui.View(func(c *ui.Context) {
			if cd := r.countdown; cd != nil {
				paintRecordCountdown(c, cd)
				return
			}
			a.paintRecording(c, r)
		}),
	})
	r.ctrl.OnClose(func(e *mygo.CloseEvent) {
		if a.recording == r {
			e.PreventDefault()
			a.endRecording(true)
		}
	})
	r.ctrl.SetContentProtection(true)
	applyWindowTheme(r.ctrl, themeStateFromConfig(a.cfg))
	r.ctrl.Show()
	if a.tray != nil {
		a.tray.SetTitle("🔴")
		a.refreshMenu()
	}
}

func paintRecordCountdown(c *ui.Context, cd *recordCountdown) {
	remaining := max(1, int(math.Ceil(time.Until(cd.deadline).Seconds())))
	c.After(100 * time.Millisecond)
	liquidGlassCapsule(c, ui.Row(c).Height(44).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).PaddingX(12), 22, c.Vibrancy()).Children(func() {
		ui.Text(c, fmt.Sprintf("%d 秒后开始录屏", remaining))
		start := ui.Button(c, "立即开始").Clicked()
		cancel := ui.Button(c, "取消").Clicked()
		if cancel || c.Shortcut(0, ui.KeyEscape) {
			cd.resolve(false)
			return
		}
		if start || c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeySpace) {
			cd.resolve(true)
		}
	})
}

func (a *app) paintRecording(c *ui.Context, r *regionRecording) {
	c.After(time.Second)
	bar := behaviors.Toolbar(c.Key("record-controls"), behaviors.ToolbarOptions{Label: "录制控制"})
	liquidGlassCapsule(c, bar.Root.Height(44).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).PaddingX(8), 22, c.Vibrancy()).Children(func() {
		col := ui.RGB(255, 69, 58)
		label := r.clock()
		if !r.pausedAt.IsZero() {
			col = ui.RGB(255, 159, 10)
			label = "已暂停"
		}
		alpha := float32(1)
		if r.pausedAt.IsZero() {
			alpha = float32(.8 + .2*math.Sin(float64(time.Now().UnixMilli()%1600)*2*math.Pi/1600))
			c.After(80 * time.Millisecond)
		}
		ui.Text(c, "● "+label).DragWindow().TextColor(col).Opacity(alpha)
		pauseIcon := iconPause
		if !r.pausedAt.IsZero() {
			pauseIcon = iconPlay
		}
		if iconToolBtn(c, pauseIcon, r.pauseLabel(), r.pauseLabel()+" (Space)", false, r.busy) || c.Shortcut(0, ui.KeySpace) {
			a.changeRecording("pause")
		}
		if iconToolBtn(c, iconSystemSound, "系统音", "开关系统声音 (A)", r.system, r.busy) || c.Shortcut(0, ui.KeyA) {
			a.changeRecording("system")
		}
		if iconToolBtn(c, iconMicrophone, "麦克风", "开关麦克风 (M)", r.mic, r.busy) || c.Shortcut(0, ui.KeyM) {
			a.changeRecording("mic")
		}
		if r.mic && r.level > 0.01 {
			ui.Text(c, "●").TextColor(ui.RGB(52, 199, 89))
		}
		if r.micDenied && ui.Button(c, "设置").Clicked() {
			go mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")
		}
		if ui.Button(c, r.discardLabel()).Disabled(r.busy).Clicked() || c.Shortcut(0, ui.KeyEscape) {
			a.endRecording(true)
		}
		if ui.Button(c, "停止").Background(ui.RGB(210, 48, 42)).Radius(15).Disabled(r.busy).Clicked() || c.Shortcut(0, ui.KeyEnter) {
			a.endRecording(false)
		}
	})
}

func (a *app) changeRecording(action string) {
	r := a.recording
	if r == nil || r.busy {
		return
	}
	r.busy = true
	pause, system, mic := r.pausedAt.IsZero(), !r.system, !r.mic
	go func() {
		ctx := context.Background()
		var err error
		switch action {
		case "pause":
			err = r.stream.Pause(ctx, pause)
		case "system":
			err = r.stream.SetSystem(ctx, system)
		case "mic":
			err = r.stream.SetMic(ctx, mic)
		}
		runOnMain(func() {
			r.busy = false
			if err != nil {
				if action == "mic" {
					r.micDenied = true
				}
				a.showActionToast(err.Error())
			} else {
				switch action {
				case "pause":
					if pause {
						r.pausedAt = time.Now()
					} else {
						r.removed += time.Since(r.pausedAt)
						r.pausedAt = time.Time{}
					}
				case "system":
					r.system = system
				case "mic":
					r.mic = mic
					r.micDenied = false
				}
				r.hadAudio = r.hadAudio || r.system || r.mic
			}
			r.frame.Invalidate()
			r.ctrl.Invalidate()
			a.refreshMenu()
		})
	}()
}

func (a *app) closeRecording(r *regionRecording) {
	a.recording = nil
	r.frame.Destroy()
	r.ctrl.Destroy()
	r.stream.Close()
	if a.tray != nil {
		a.tray.SetTitle("")
		a.refreshMenu()
	}
}

func (a *app) endRecording(discard bool) {
	r := a.recording
	if r != nil && r.countdown != nil {
		r.countdown.resolve(false)
		return
	}
	if r == nil || r.busy {
		return
	}
	if discard && r.elapsed() >= 3*time.Second && !time.Now().Before(r.confirmUntil) {
		r.confirmUntil = time.Now().Add(3 * time.Second)
		r.ctrl.Invalidate()
		a.refreshMenu()
		return
	}
	r.busy = true
	go func() {
		err := r.stream.Stop(context.Background())
		runOnMain(func() {
			a.closeRecording(r)
			if discard {
				os.RemoveAll(filepath.Dir(r.path))
				return
			}
			if err != nil {
				a.showActionToast(err.Error())
				return
			}
			a.openRecordStudio(r)
		})
	}()
}

// Sample on the UI timer so stop cannot close a native object during a poll.
func (a *app) updateRecordingStatus(r *regionRecording) {
	time.AfterFunc(time.Second, func() {
		runOnMain(func() {
			if a.recording != r {
				return
			}
			if !r.busy {
				level, err := r.stream.Level(context.Background())
				if err == nil {
					if level < 0 {
						r.interrupted = true
						a.endRecording(false)
						return
					}
					r.level = level
				}
				r.ctrl.Invalidate()
				a.refreshMenu()
			}
			a.updateRecordingStatus(r)
		})
	})
}

func pruneOldRecordings() {
	root := filepath.Join(os.TempDir(), "snipgo-recordings")
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "clip-") {
			continue
		}
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}
