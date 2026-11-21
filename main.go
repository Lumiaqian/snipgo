package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/beautify"
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
	delayShortErr error
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
	tplWin        *templateWindow
	lastTplOpts   beautify.TemplateOptions
	hasTplOpts    bool
	countdown     *countdownSession
	scrollCap     *scrollCaptureSession
	snappingPin   bool
	upd           *updateManager

	// 选区内文字批注临时编辑态
	textEditing bool
	textEditX   float64
	textEditY   float64
	textEditBuf string

	// 实时模式下给放大镜 / 马赛克 / 无痕擦除预览提供局部像素快照
	previewSnapGen     uint64
	previewSnapRect    image.Rectangle
	previewSnapPending image.Rectangle
	previewSnapImg     *image.NRGBA
	previewSnapBmp     *ui.Bitmap

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
		a.shutdownUpdater()
		a.closeOCRWindow()
		a.closeHistoryWindow()
		a.closeSettingsWindow()
		a.closeTemplateWindow()
		a.closeAllPins()
		cleanupAllDragTempFiles()
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
	a.applyThemeLive(a.cfg)
	mygo.Theme.OnUpdated(func() {
		if a.cfg.ThemeAppearance == "" || a.cfg.ThemeAppearance == "system" {
			a.applyThemeLive(a.cfg)
		}
	})
	if a.userDataDir != "" {
		if st, err := store.Open(a.userDataDir, store.DefaultMaxEntries); err == nil {
			a.history = st
		}
	}

	a.session = &session.Session{
		Allowed:       a.allowCapture,
		Live:          a.liveSnapshot,
		CaptureRect:   a.captureRect,
		PreviewFrame:  a.previewFrame,
		CaptureWindow: a.captureWindow,
		Capture:       a.captureFrozen,
		Clip:          sysClip{},
		UI:            a,
		OnOutput:      a.recordHistory,
		OnCutout:      a.cutoutSelection,
		OnTemplate:    a.openTemplateWindow,
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
	a.initUpdater()
	if !capture.HasPermission() {
		go func() { _ = capture.RequestPermission() }()
	}
}

func (a *app) registerAllShortcuts() {
	if !mygo.App.IsReady() {
		return
	}
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
	a.delayShortErr = mygo.GlobalShortcut.Register(a.cfg.DelayShortcut, func() {
		a.startDelayedCapture(a.cfg.DelaySeconds)
	})
	_ = mygo.GlobalShortcut.Register(restoreShortcut, a.RestoreLastPin)
	_ = mygo.GlobalShortcut.Register(unghostShortcut, a.restorePassthroughPins)
}

func (a *app) shortcutError(id string) error {
	switch id {
	case "snip":
		return a.shortcutErr
	case "ocr":
		return a.ocrShortErr
	case "freeze":
		return a.freezeShortEr
	case "delay":
		return a.delayShortErr
	case "pin":
		return a.pinShortErr
	case "history":
		return a.histShortErr
	default:
		return nil
	}
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

// showAbortedAlert 在实时采集失败且保留了选区时，让用户选择恢复重试或放弃。
func (a *app) showAbortedAlert(msg string) {
	if !a.alertShowing.CompareAndSwap(false, true) {
		return
	}
	defer a.alertShowing.Store(false)
	mygo.App.Focus()
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Title:         "截图或贴图失败",
		Message:       msg,
		Detail:        "已保留刚才的选区和标注，可以恢复后重试。",
		Buttons:       []string{"恢复截图并重试", "放弃"},
		DefaultButton: 0,
		CancelButton:  1,
	})
	// 对话框出错时保留快照，仍可从菜单栏恢复。
	if err != nil {
		return
	}
	mygo.RunOnMain(func() {
		if res.Button == 0 {
			if !a.session.RestoreAborted() {
				a.session.DiscardAborted()
				a.extraStatus = "无法恢复选区"
			}
		} else {
			a.session.DiscardAborted()
		}
		a.refreshMenu()
	})
}

func (a *app) SetStatus(msg string) {
	if msg != "" {
		log.Printf("session status: %s", msg)
		if msg != capture.ErrPermissionDenied.Error() {
			if a.session != nil && a.session.HasAborted() {
				go a.showAbortedAlert(msg)
			} else {
				go a.showErrorAlert("截图或贴图失败", msg)
			}
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
	delayAcc := a.cfg.DelayShortcut
	if a.delayShortErr != nil {
		delayAcc = ""
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
			Accelerator: delayAcc,
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
	if a.session.HasAborted() {
		items = append(items, &mygo.MenuItem{
			Label: fmt.Sprintf("恢复未完成的截图（%d 个标注）", a.session.AbortedItemCount()),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if !a.session.RestoreAborted() {
					a.session.DiscardAborted()
					a.extraStatus = "无法恢复选区"
					a.refreshMenu()
				}
			},
		})
	}
	status := a.session.Status()
	if status == "" {
		status = a.extraStatus
	}
	if status != "" {
		items = append(items, &mygo.MenuItem{Label: status, Disabled: true})
	}
	var conflictLabels []string
	for _, def := range shortcutDefs {
		if a.shortcutError(def.id) != nil {
			conflictLabels = append(conflictLabels, def.label)
		}
	}
	if len(conflictLabels) > 0 {
		items = append(items, &mygo.MenuItem{Label: "快捷键被占用：" + strings.Join(conflictLabels, "、"), Disabled: true})
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
	items = append(items, mygo.Separator())
	if a.upd != nil && a.upd.stagedVersion != "" {
		items = append(items, &mygo.MenuItem{
			Label: fmt.Sprintf("重启并更新至 %s...", formatVersion(a.upd.stagedVersion)),
			Badge: "就绪",
			Click: func(*mygo.MenuItem, *mygo.Window) { a.openUpdateWindow() },
		})
	} else if rel := a.availableUpdate(); rel != nil {
		items = append(items, &mygo.MenuItem{
			Label: fmt.Sprintf("发现新版本 %s...", formatVersion(rel.Version)),
			Badge: "新",
			Click: func(*mygo.MenuItem, *mygo.Window) { a.openUpdateWindow() },
		})
	}
	items = append(items,
		&mygo.MenuItem{
			Label: "检查更新...",
			Click: func(*mygo.MenuItem, *mygo.Window) { a.checkForUpdates(true) },
		},
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

func (a *app) applyThemeLive(cfg config.Settings) {
	st := themeStateFromConfig(cfg)
	setActiveTheme(st)
	if a.tplWin != nil {
		applyWindowTheme(a.tplWin.win, st)
	}
	if a.setWin != nil {
		applyWindowTheme(a.setWin.win, st)
	}
	if a.histWin != nil {
		applyWindowTheme(a.histWin.win, st)
	}
	if a.ocr != nil {
		applyWindowTheme(a.ocr.win, st)
	}
	if a.upd != nil {
		applyWindowTheme(a.upd.win, st)
	}
	if a.scrollCap != nil {
		applyWindowTheme(a.scrollCap.ctrlWin, st)
	}
	if a.countdown != nil {
		applyWindowTheme(a.countdown.win, st)
	}
	for _, o := range a.overlays {
		if o != nil && o.win != nil && !o.win.IsDestroyed() {
			applyWindowAppearance(o.win, st.Appearance)
			o.win.Invalidate()
		}
	}
	for _, p := range a.pins {
		if p != nil && p.win != nil && !p.win.IsDestroyed() {
			applyWindowAppearance(p.win, st.Appearance)
			p.win.Invalidate()
		}
	}
}

// trayIcon 绘制 36×36 (@2x) 抗锯齿纯黑模板取景框图标（圆角 L 形四角 + 中心对焦点）。macOS 会按模板自动适配深/浅菜单栏着色。
func trayIcon() []byte {
	const (
		size       = 36
		center     = 17.5
		midHalf    = 11.2 // stroke centerline: outer edge = 11.2 + 1.4 = 12.6
		rMid       = 3.8  // outer corner radius = 3.8 + 1.4 = 5.2px
		gap        = 4.2
		halfStroke = 1.4 // 2.8px stroke width
		dotRadius  = 2.6
	)
	cx0 := midHalf - rMid
	distSeg := func(px, py, ax, ay, bx, by float64) float64 {
		vx, vy := bx-ax, by-ay
		wx, wy := px-ax, py-ay
		l2 := vx*vx + vy*vy
		if l2 <= 0 {
			return math.Hypot(wx, wy)
		}
		t := max(0.0, min(1.0, (wx*vx+wy*vy)/l2))
		return math.Hypot(px-(ax+t*vx), py-(ay+t*vy))
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			ax := math.Abs(float64(x) - center)
			ay := math.Abs(float64(y) - center)

			dH := distSeg(ax, ay, gap, midHalf, cx0, midHalf)
			dV := distSeg(ax, ay, midHalf, gap, midHalf, cx0)
			var dArc float64
			if ax >= cx0 && ay >= cx0 {
				dArc = math.Abs(math.Hypot(ax-cx0, ay-cx0) - rMid)
			} else {
				dArc = min(math.Hypot(ax-cx0, ay-midHalf), math.Hypot(ax-midHalf, ay-cx0))
			}
			dBracket := min(dH, min(dV, dArc)) - halfStroke
			dDot := math.Hypot(ax, ay) - dotRadius
			d := min(dBracket, dDot)

			cov := max(0.0, min(1.0, 0.5-d/1.1))
			if cov > 0 {
				img.SetNRGBA(x, y, color.NRGBA{A: uint8(math.Round(cov * 255))})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// previewFrame 返回与选区一致的预览像素快照，供确认时按「所见即所得」渲染打码/擦除/放大镜。
func (a *app) previewFrame(sel image.Rectangle) (*image.NRGBA, bool) {
	if a.previewSnapImg == nil || a.previewSnapRect.Empty() || a.previewSnapRect != sel.Canon() {
		return nil, false
	}
	return a.previewSnapImg, true
}
