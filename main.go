package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/egoist/mygo"

	"snipgo/internal/capture"
	"snipgo/internal/config"
	"snipgo/internal/crop"
	"snipgo/internal/session"
	"snipgo/internal/store"
)

const (
	appBundleID       = "app.snipgo"
	restoreShortcut   = "Shift+Alt+Cmd+2"
	unghostShortcut   = "Ctrl+Alt+Cmd+2"
	screenSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
)

// app 是菜单栏里的 SnipGo。关窗不退出，只从菜单退出。
type app struct {
	tray          *mygo.Tray
	session       *session.Session
	cfg           config.Settings
	userDataDir   string
	picturesDir   string
	history       *store.HistoryStore
	shortcutErr   error
	ocrShortErr   error
	freezeShortEr error
	pinShortErr   error
	histShortErr  error
	permDenied    bool
	alertShowing  atomic.Bool
	redacting     bool
	extraStatus   string
	overlays      []*overlay
	pins          []*pinWin
	closedPins    []closedPinRecord
	ocr           *ocrWindow
	histWin       *historyWindow
	setWin        *settingsWindow
	countdown     *countdownSession
	scrollCap     *scrollCaptureSession

	// 选区内文字批注临时编辑态
	textEditing bool
	textEditX   float64
	textEditY   float64
	textEditBuf string

	quitting bool
}

func main() {
	initLogFile()
	a := &app{}
	mygo.App.SetName("SnipGo")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		if a.session != nil {
			a.session.Cancel()
		}
		a.stopCountdown()
		a.stopScrollCapture()
		a.closeOCRWindow()
		a.closeHistoryWindow()
		a.closeSettingsWindow()
		a.closeAllPins()
		mygo.GlobalShortcut.UnregisterAll()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) ready() {
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		a.userDataDir = dir
	} else if home, err := os.UserHomeDir(); err == nil {
		a.userDataDir = filepath.Join(home, "Library", "Application Support", "SnipGo")
	}
	if pDir, err := mygo.App.Path(mygo.PathPictures); err == nil {
		a.picturesDir = pDir
	}
	a.cfg = config.Load(a.userDataDir, a.picturesDir)
	a.cfg.OpenAtLogin = mygo.App.OpenAtLogin()
	if a.userDataDir != "" {
		if st, err := store.Open(a.userDataDir, store.DefaultMaxEntries); err == nil {
			a.history = st
		}
	}

	a.session = &session.Session{
		Live:          a.liveSnapshot,
		CaptureRect:   a.captureRect,
		CaptureWindow: a.captureWindow,
		Capture:       a.captureFrozen,
		Clip:          sysClip{},
		UI:            a,
		OnOutput:      a.recordHistory,
		OnMain:        mygo.RunOnMain,
	}
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID:             "snipgo",
		Icon:           trayIcon(),
		IconIsTemplate: true,
		ToolTip:        "SnipGo",
	})
	if err != nil {
		log.Fatal(err)
	}
	a.registerAllShortcuts()
	a.refreshMenu()
	if !capture.HasPermission() {
		go func() { _ = capture.RequestPermission() }()
	}
}

func (a *app) registerAllShortcuts() {
	mygo.GlobalShortcut.UnregisterAll()
	a.shortcutErr = mygo.GlobalShortcut.Register(a.cfg.SnipShortcut, func() {
		a.extraStatus = ""
		a.session.StartLive()
	})
	a.ocrShortErr = mygo.GlobalShortcut.Register(a.cfg.OCRShortcut, func() {
		a.extraStatus = ""
		a.session.StartLiveOCR()
	})
	a.freezeShortEr = mygo.GlobalShortcut.Register(a.cfg.FreezeShortcut, func() {
		a.extraStatus = ""
		a.session.StartFrozen()
	})
	a.pinShortErr = mygo.GlobalShortcut.Register(a.cfg.PinShortcut, a.PasteClipboardPin)
	a.histShortErr = mygo.GlobalShortcut.Register(a.cfg.HistoryShortcut, a.toggleHistoryWindow)
	_ = mygo.GlobalShortcut.Register(a.cfg.DelayShortcut, func() {
		a.startDelayedCapture(a.cfg.DelaySeconds)
	})
	_ = mygo.GlobalShortcut.Register(restoreShortcut, a.RestoreLastPin)
	_ = mygo.GlobalShortcut.Register(unghostShortcut, a.restorePassthroughPins)
}

func (a *app) currentDisplays() ([]capture.Display, error) {
	list := mygo.Screen.Displays()
	displays := make([]capture.Display, 0, len(list))
	for _, d := range list {
		b := d.Bounds
		if b.Width <= 0 || b.Height <= 0 || d.ScaleFactor <= 0 {
			return nil, fmt.Errorf("显示器 %d 的尺寸无效", d.ID)
		}
		displays = append(displays, capture.Display{
			ID:     d.ID,
			Bounds: image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height),
			Scale:  d.ScaleFactor,
		})
	}
	return displays, nil
}

func toWinTargets(infos []capture.WindowInfo) ([]image.Rectangle, []session.WinTarget) {
	rects := make([]image.Rectangle, len(infos))
	targets := make([]session.WinTarget, len(infos))
	for i, wi := range infos {
		rects[i] = wi.Bounds
		targets[i] = session.WinTarget{ID: wi.ID, Bounds: wi.Bounds}
	}
	return rects, targets
}

func (a *app) liveSnapshot() (session.Snapshot, error) {
	displays, err := a.currentDisplays()
	if err != nil {
		return session.Snapshot{}, err
	}
	frames := make([]crop.Screen, len(displays))
	for i, d := range displays {
		frames[i] = crop.Screen{Bounds: d.Bounds, Scale: d.Scale}
	}
	cur := mygo.Screen.CursorScreenPoint()
	frontInfo, winInfos := capture.VisibleWindowInfos(image.Pt(cur.X, cur.Y), displays, overlayTitle)
	rects, targets := toWinTargets(winInfos)
	return session.Snapshot{
		Frames:      frames,
		FrontWindow: frontInfo.Bounds,
		FrontWinID:  frontInfo.ID,
		Windows:     rects,
		WindowInfos: targets,
	}, nil
}

func (a *app) captureRect(ctx context.Context, sel image.Rectangle) (*image.NRGBA, error) {
	var displays []capture.Display
	var dispErr error
	done := make(chan struct{})
	mygo.RunOnMain(func() {
		displays, dispErr = a.currentDisplays()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if dispErr != nil {
		return nil, dispErr
	}
	img, err := capture.GrabRect(ctx, displays, sel, overlayTitle)
	a.trackPerm(err)
	return img, err
}

func (a *app) captureWindow(ctx context.Context, winID uint32, scale float64) (*image.NRGBA, error) {
	img, err := capture.GrabWindow(ctx, winID, scale)
	a.trackPerm(err)
	return img, err
}

func (a *app) captureFrozen(ctx context.Context) (session.Snapshot, error) {
	var displays []capture.Display
	var cursor mygo.Point
	var dispErr error
	done := make(chan struct{})
	mygo.RunOnMain(func() {
		displays, dispErr = a.currentDisplays()
		cursor = mygo.Screen.CursorScreenPoint()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
		return session.Snapshot{}, ctx.Err()
	}
	if dispErr != nil {
		return session.Snapshot{}, dispErr
	}
	frontInfo, winInfos := capture.VisibleWindowInfos(image.Pt(cursor.X, cursor.Y), displays, overlayTitle)
	rects, targets := toWinTargets(winInfos)
	frames, err := capture.Grab(ctx, displays, overlayTitle)
	a.trackPerm(err)
	if err != nil {
		return session.Snapshot{}, err
	}
	return session.Snapshot{
		Frames:      frames,
		FrontWindow: frontInfo.Bounds,
		FrontWinID:  frontInfo.ID,
		Windows:     rects,
		WindowInfos: targets,
	}, nil
}

func initLogFile() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, "Library", "Logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "SnipGo.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("SnipGo starting (pid=%d)", os.Getpid())
}

// deferUI 把会关闭蒙层窗口或创建贴图窗口的操作放到当前 ui.View 渲染帧结束后的下一轮主循环执行。
func (a *app) deferUI(fn func()) {
	if len(a.overlays) > 0 && a.overlays[0].win == nil {
		fn()
		return
	}
	go mygo.RunOnMain(fn)
}

func (a *app) trackPerm(err error) {
	if err != nil {
		log.Printf("capture error: %v", err)
		if errors.Is(err, capture.ErrPermissionDenied) {
			go a.promptPermission()
		}
		return
	}
	mygo.RunOnMain(func() {
		if a.permDenied {
			a.permDenied = false
			a.refreshMenu()
		}
	})
}

func (a *app) promptPermission() {
	if !a.alertShowing.CompareAndSwap(false, true) {
		return
	}
	defer a.alertShowing.Store(false)

	mygo.RunOnMain(func() {
		a.permDenied = true
		a.refreshMenu()
	})
	mygo.App.Focus()
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Title:         "需要更新屏幕录制授权",
		Message:       "开发版 SnipGo 重新编译后签名哈希已变化，macOS 里旧的授权记录不再匹配当前版本。",
		Detail:        "直接开关旧记录不会更新签名哈希。请点击「重置旧记录并重新授权」，在弹出的系统窗口或「录屏与系统录音」中打开 SnipGo 开关，再点「重启 SnipGo」即可正常截图与贴图。",
		Buttons:       []string{"重置旧记录并重新授权", "已开启，重启 SnipGo", "取消"},
		DefaultButton: 0,
		CancelButton:  2,
	})
	if err != nil {
		return
	}
	switch res.Button {
	case 0:
		if runtime.GOOS == "darwin" {
			if out, resetErr := exec.Command("/usr/bin/tccutil", "reset", "ScreenCapture", appBundleID).CombinedOutput(); resetErr != nil {
				log.Printf("tccutil reset failed: %v (%s)", resetErr, string(out))
			} else {
				log.Printf("tccutil reset ScreenCapture %s succeeded", appBundleID)
			}
		}
		_ = capture.RequestPermission()
		_ = mygo.Shell.OpenExternal(screenSettingsURL)
		mygo.App.Focus()
		next, nextErr := mygo.Dialog.Message(mygo.MessageOptions{
			Title:         "请开启开关后重启 SnipGo",
			Message:       "已清除旧签名记录并发起新授权请求。",
			Detail:        "请在系统弹窗点「允许」或在系统设置「录屏与系统录音」中打开新的 SnipGo 开关，完成后点击下方「重启 SnipGo」。",
			Buttons:       []string{"重启 SnipGo", "稍后"},
			DefaultButton: 0,
			CancelButton:  1,
		})
		if nextErr == nil && next.Button == 0 {
			mygo.App.Relaunch()
		}
	case 1:
		mygo.App.Relaunch()
	}
}

func (a *app) showErrorAlert(title, detail string) {
	if !a.alertShowing.CompareAndSwap(false, true) {
		return
	}
	defer a.alertShowing.Store(false)
	mygo.App.Focus()
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Title:   title,
		Message: detail,
		Buttons: []string{"好"},
	})
}

func (a *app) SetStatus(msg string) {
	if msg != "" {
		log.Printf("session status: %s", msg)
		if msg != capture.ErrPermissionDenied.Error() {
			go a.showErrorAlert("截图或贴图失败", msg)
		}
	}
	a.extraStatus = ""
	a.refreshMenu()
}

func (a *app) refreshMenu() {
	if a.tray == nil || a.session == nil {
		return
	}
	snipAcc := a.cfg.SnipShortcut
	if a.shortcutErr != nil {
		snipAcc = ""
	}
	ocrAcc := a.cfg.OCRShortcut
	if a.ocrShortErr != nil {
		ocrAcc = ""
	}
	freezeAcc := a.cfg.FreezeShortcut
	if a.freezeShortEr != nil {
		freezeAcc = ""
	}
	pinAcc := a.cfg.PinShortcut
	if a.pinShortErr != nil {
		pinAcc = ""
	}
	histAcc := a.cfg.HistoryShortcut
	if a.histShortErr != nil {
		histAcc = ""
	}
	delaySec := a.cfg.DelaySeconds
	if delaySec <= 0 {
		delaySec = 3
	}
	var histBadge string
	if a.history != nil && a.history.Count() > 0 {
		histBadge = strconv.Itoa(a.history.Count())
	}

	items := []*mygo.MenuItem{
		{
			Label:       "区域截图",
			Accelerator: snipAcc,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.extraStatus = ""
				a.session.StartLiveFromMenu()
			},
		},
		{
			Label:       "截图识图 (OCR)",
			Accelerator: ocrAcc,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.extraStatus = ""
				a.session.StartLiveOCRFromMenu()
			},
		},
		{
			Label:       "定格截图与标尺",
			Accelerator: freezeAcc,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.extraStatus = ""
				a.session.StartFrozenFromMenu()
			},
		},
		{
			Label:       fmt.Sprintf("%d 秒延时定格截图", delaySec),
			Accelerator: a.cfg.DelayShortcut,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.startDelayedCapture(delaySec)
			},
		},
		mygo.Separator(),
		{
			Label:       "从剪贴板贴图",
			Accelerator: pinAcc,
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.PasteClipboardPin() },
		},
		{
			Label:       "最近截图历史",
			Accelerator: histAcc,
			Badge:       histBadge,
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.openHistoryWindow() },
		},
	}
	if ghostN := a.passthroughPinCount(); ghostN > 0 {
		items = append(items, &mygo.MenuItem{
			Label:       "恢复穿透贴图交互",
			Accelerator: unghostShortcut,
			Badge:       strconv.Itoa(ghostN),
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.restorePassthroughPins() },
		})
	}
	if len(a.closedPins) > 0 {
		items = append(items, &mygo.MenuItem{
			Label:       "恢复刚关闭的贴图",
			Accelerator: restoreShortcut,
			Badge:       strconv.Itoa(len(a.closedPins)),
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.RestoreLastPin() },
		})
	}
	if len(a.pins) > 0 {
		items = append(items, &mygo.MenuItem{
			Label: "关闭全部贴图",
			Badge: strconv.Itoa(len(a.pins)),
			Click: func(*mygo.MenuItem, *mygo.Window) { a.closeAllPins() },
		})
	}
	status := a.session.Status()
	if status == "" {
		status = a.extraStatus
	}
	if status != "" {
		items = append(items, &mygo.MenuItem{Label: status, Disabled: true})
	}
	if a.shortcutErr != nil {
		items = append(items, &mygo.MenuItem{Label: "快捷键 " + a.cfg.SnipShortcut + " 注册失败", Disabled: true})
	}
	if a.permDenied {
		items = append(items,
			&mygo.MenuItem{
				Label: "重新授权屏幕录制...",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					go a.promptPermission()
				},
			},
			&mygo.MenuItem{
				Label: "重启 SnipGo",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					mygo.App.Relaunch()
				},
			},
		)
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{
			Label:       "偏好设置...",
			Accelerator: "Cmd+,",
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.openSettingsWindow() },
		},
		&mygo.MenuItem{Label: "退出", Role: mygo.RoleQuit},
	)
	a.tray.SetMenu(mygo.NewMenu(items))
}

type sysClip struct{}

func (sysClip) WritePNG(b []byte) error {
	return mygo.Clipboard.WriteImage(b)
}

func (sysClip) WriteText(s string) {
	mygo.Clipboard.WriteText(s)
}

// trayIcon 画四角取景框，黑底透明。macOS 会按模板给它着色。
func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := image.NewUniform(color.NRGBA{A: 255})
	for _, r := range []image.Rectangle{
		image.Rect(5, 5, 13, 8),
		image.Rect(5, 5, 8, 13),
		image.Rect(19, 5, 27, 8),
		image.Rect(24, 5, 27, 13),
		image.Rect(5, 24, 13, 27),
		image.Rect(5, 19, 8, 27),
		image.Rect(19, 24, 27, 27),
		image.Rect(24, 19, 27, 27),
	} {
		draw.Draw(img, r, ink, image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
