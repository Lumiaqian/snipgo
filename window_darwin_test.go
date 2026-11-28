//go:build darwin

package main

import (
	"math"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var mainThreadTasks = make(chan func())

func init() {
	runtime.LockOSThread()
}

func TestMain(m *testing.M) {
	done := make(chan int, 1)
	go func() {
		done <- m.Run()
	}()
	for {
		select {
		case fn := <-mainThreadTasks:
			fn()
		case code := <-done:
			os.Exit(code)
		}
	}
}

func runOnMacMainThread(fn func()) {
	ch := make(chan struct{})
	mainThreadTasks <- func() {
		defer close(ch)
		fn()
	}
	<-ch
}

func TestUnconstrainedPanelNotShiftedByMenuBarOrNotch(t *testing.T) {
	_, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
	if err != nil {
		t.Skipf("AppKit unavailable: %v", err)
	}
	msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
	if err != nil {
		t.Fatalf("objc_msgSend: %v", err)
	}
	stret := msgSend
	if runtime.GOARCH == "amd64" {
		stret, err = purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend_stret")
		if err != nil {
			t.Fatalf("objc_msgSend_stret: %v", err)
		}
	}

	var (
		msgRect       func(objc.ID, objc.SEL) nsRect
		msgSetRectB   func(objc.ID, objc.SEL, nsRect, bool)
		msgInitWindow func(objc.ID, objc.SEL, nsRect, uintptr, uintptr, bool) objc.ID
		msgFloat      func(objc.ID, objc.SEL) float64
	)
	purego.RegisterFunc(&msgRect, stret)
	purego.RegisterFunc(&msgSetRectB, msgSend)
	purego.RegisterFunc(&msgInitWindow, msgSend)
	purego.RegisterFunc(&msgFloat, msgSend)

	var skipped string
	runOnMacMainThread(func() {
		app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
		app.Send(objc.RegisterName("setActivationPolicy:"), uintptr(1)) // Accessory

		testCls, err := objc.RegisterClass("SnipGoTestPanel", objc.GetClass("NSPanel"), nil, nil, nil)
		if err != nil {
			t.Errorf("RegisterClass SnipGoTestPanel: %v", err)
			return
		}
		installUnconstrainedFrame(testCls)

		screens := objc.ID(objc.GetClass("NSScreen")).Send(objc.RegisterName("screens"))
		count := int(objc.Send[uintptr](screens, objc.RegisterName("count")))
		if count == 0 {
			skipped = "no NSScreen available"
			return
		}

		const (
			styleTitled          = 1 << 0
			styleFullSizeContent = 1 << 15
			backingBuffered      = 2
		)
		selFrame := objc.RegisterName("frame")
		selSetFrame := objc.RegisterName("setFrame:display:")
		selOrderFront := objc.RegisterName("makeKeyAndOrderFront:")
		selOrderOut := objc.RegisterName("orderOut:")
		selClose := objc.RegisterName("close")
		selCornerRadius := objc.RegisterName("_cornerRadius")
		selResponds := objc.RegisterName("respondsToSelector:")

		for i := range count {
			scr := screens.Send(objc.RegisterName("objectAtIndex:"), uintptr(i))
			target := msgRect(scr, selFrame)

			win := msgInitWindow(
				objc.ID(testCls).Send(objc.RegisterName("alloc")),
				objc.RegisterName("initWithContentRect:styleMask:backing:defer:"),
				nsRect{Origin: nsPoint{40, 40}, Size: target.Size},
				styleTitled|styleFullSizeContent|styleNonactivatingPanel,
				backingBuffered,
				false,
			)
			win.Send(objc.RegisterName("setReleasedWhenClosed:"), uintptr(0))
			win.Send(objc.RegisterName("setTitlebarAppearsTransparent:"), uintptr(1))
			win.Send(objc.RegisterName("setTitleVisibility:"), uintptr(1))

			// 完整模拟 makeBorderlessUnconstrained + prepareOverlayWindow 的真实调用序列（含 KVO 标题栏移除）。
			win.Send(selSetStyleMask, uintptr(styleNonactivatingPanel))
			win.Send(selSetLevel, uintptr(nsOverlayWindowLevel))
			msgSetRectB(win, selSetFrame, target, true)
			win.Send(selOrderFront, uintptr(0))

			got := msgRect(win, selFrame)
			var corner float64
			if objc.Send[bool](win, selResponds, selCornerRadius) {
				corner = msgFloat(win, selCornerRadius)
			}
			win.Send(selOrderOut, uintptr(0))
			win.Send(selClose)

			if math.Abs(got.Origin.X-target.Origin.X) > 0.01 ||
				math.Abs(got.Origin.Y-target.Origin.Y) > 0.01 ||
				math.Abs(got.Size.Width-target.Size.Width) > 0.01 ||
				math.Abs(got.Size.Height-target.Size.Height) > 0.01 {
				t.Errorf("screen[%d] frame clamped by menu bar/notch: got %+v, want %+v", i, got, target)
				return
			}
			if corner > 0.01 {
				t.Errorf("screen[%d] borderless panel still has corner radius %.2f, want 0", i, corner)
				return
			}
		}
	})
	if skipped != "" {
		t.Skip(skipped)
	}
}

func TestOverlayEditingLevel(t *testing.T) {
	for _, editing := range []bool{false, true, false} {
		want := uintptr(102)
		if editing {
			want = 27
		}
		if got := overlayLevel(editing); got != want {
			t.Fatalf("editing=%v level=%d want %d", editing, got, want)
		}
	}
}
