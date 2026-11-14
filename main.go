package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"

	"snipgo/internal/capture"
	"snipgo/internal/crop"
	"snipgo/internal/session"
)

const shortcut = "Cmd+Shift+2"

// app 是菜单栏里的 SnipGo。关窗不退出，只从菜单退出。
type app struct {
	tray        *mygo.Tray
	session     *session.Session
	shortcutErr error
	overlays    []*overlay
	pins        []*pinWin
	quitting    bool
}

func main() {
	a := &app{}
	mygo.App.SetName("SnipGo")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		if a.session != nil {
			a.session.Cancel()
		}
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
	a.session = &session.Session{
		Allowed: a.allowed,
		Capture: a.capture,
		Clip:    pngClip{},
		UI:      a,
		OnMain:  mygo.RunOnMain,
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
	if err := mygo.GlobalShortcut.Register(shortcut, a.session.Hotkey); err != nil {
		a.shortcutErr = err
	}
	a.refreshMenu()
}

func (a *app) allowed() (bool, string) {
	st, err := permissionflow.Check(permissionflow.ScreenRecording)
	if err != nil {
		return false, "无法检查屏幕录制权限"
	}
	if st != permissionflow.Granted {
		return false, "需要屏幕录制权限"
	}
	return true, ""
}

func (a *app) capture(ctx context.Context) ([]crop.Screen, error) {
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
	return capture.Grab(ctx, displays)
}

func (a *app) SetStatus(string) { a.refreshMenu() }

func (a *app) refreshMenu() {
	if a.tray == nil || a.session == nil {
		return
	}
	items := []*mygo.MenuItem{{
		Label: "截图",
		Click: func(*mygo.MenuItem, *mygo.Window) { a.session.Hotkey() },
	}}
	if text := a.session.Status(); text != "" {
		items = append(items, &mygo.MenuItem{Label: text, Disabled: true})
	}
	if a.shortcutErr != nil {
		items = append(items, &mygo.MenuItem{Label: "快捷键 " + shortcut + " 注册失败", Disabled: true})
	}
	st, err := permissionflow.Check(permissionflow.ScreenRecording)
	if err != nil || st != permissionflow.Granted {
		items = append(items, &mygo.MenuItem{
			Label:    "打开屏幕录制设置",
			Subtitle: "允许后需要重新打开 SnipGo",
			Click:    func(*mygo.MenuItem, *mygo.Window) { a.guide() },
		})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出", Role: mygo.RoleQuit},
	)
	a.tray.SetMenu(mygo.NewMenu(items))
}

// guide 不能在主线程调用，它会等到设置窗口关掉。
func (a *app) guide() {
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, _ = permissionflow.Guide(ctx, permissionflow.ScreenRecording, permissionflow.GuideOptions{Name: "SnipGo"})
		mygo.RunOnMain(a.refreshMenu)
	}()
}

type pngClip struct{}

func (pngClip) WritePNG(b []byte) error {
	return mygo.Clipboard.WriteImage(b)
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
