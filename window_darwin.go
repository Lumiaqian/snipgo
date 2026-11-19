//go:build darwin

package main

import (
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// kCGStatusWindowLevel(25)+2 高于菜单栏（24）、状态栏（25）、Dock（20）和浮动贴图（3），
// 同时低于系统输入法候选词浮窗（kCGPopUpMenuWindowLevel=101），确保文字批注输入中文时候选词框不被遮挡。
const (
	nsOverlayWindowLevel    = 27
	styleNonactivatingPanel = 1 << 7
)

type nsPoint struct{ X, Y float64 }
type nsSize struct{ Width, Height float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

var (
	panelMethodMu    sync.Mutex
	patchedPanelClss = map[objc.Class]bool{}
	constrainIMPOnce sync.Once
	constrainIMP     objc.IMP

	selSetLevel           = objc.RegisterName("setLevel:")
	selSetStyleMask       = objc.RegisterName("setStyleMask:")
	selFirstResponder     = objc.RegisterName("firstResponder")
	selMakeFirstResponder = objc.RegisterName("makeFirstResponder:")
	selConstrainFrameRect = objc.RegisterName("constrainFrameRect:toScreen:")
	selAlloc              = objc.RegisterName("alloc")
	selInitBytesLenEnc    = objc.RegisterName("initWithBytes:length:encoding:")
	selRelease            = objc.RegisterName("release")
	selAppearanceNamed    = objc.RegisterName("appearanceNamed:")
	selSetAppearance      = objc.RegisterName("setAppearance:")
)

const nsUTF8StringEncoding = uintptr(4)

const constrainFrameRectEncoding = "{CGRect={CGPoint=dd}{CGSize=dd}}@:{CGRect={CGPoint=dd}{CGSize=dd}}@"

// installUnconstrainedFrame 在面板类（如 MyGoPanel）方法表上直接挂载 constrainFrameRect:toScreen:，
// 不修改实例 isa 指针（避免破坏 AppKit NSTitlebarView 的 KVO NSKVONotifying_* 动态子类）。
func installUnconstrainedFrame(cls objc.Class) {
	if cls == 0 {
		return
	}
	constrainIMPOnce.Do(func() {
		constrainIMP = objc.NewIMP(func(_ objc.ID, _ objc.SEL, frameRect nsRect, _ objc.ID) nsRect {
			return frameRect
		})
	})
	if constrainIMP == 0 {
		return
	}
	panelMethodMu.Lock()
	defer panelMethodMu.Unlock()
	if patchedPanelClss[cls] {
		return
	}
	_ = cls.AddMethod(selConstrainFrameRect, constrainIMP, constrainFrameRectEncoding)
	patchedPanelClss[cls] = true
}

func makeBorderlessUnconstrained(w *mygo.Window) objc.ID {
	if w == nil {
		return 0
	}
	h := w.NativeHandle()
	if h == 0 {
		return 0
	}
	installUnconstrainedFrame(objc.GetClass("MyGoPanel"))
	win := objc.ID(h)
	// 去掉 MyGo 默认附带的 NSWindowStyleMaskTitled，消除 macOS 26+ 标题窗口强制的 16pt 圆角裁切，
	// 同时恢复原 firstResponder 以保持键盘快捷键响应。
	prevFR := win.Send(selFirstResponder)
	win.Send(selSetStyleMask, uintptr(styleNonactivatingPanel))
	if prevFR != 0 && win.Send(selFirstResponder) != prevFR {
		win.Send(selMakeFirstResponder, prevFR)
	}
	return win
}

func prepareOverlayWindow(w *mygo.Window) {
	if win := makeBorderlessUnconstrained(w); win != 0 {
		win.Send(selSetLevel, uintptr(nsOverlayWindowLevel))
	}
}

func preparePinWindow(w *mygo.Window) {
	_ = makeBorderlessUnconstrained(w)
}

// applyWindowAppearance 为原生 NSWindow/NSPanel 设置浅色、深色或跟随系统的 NSAppearance，
// 使系统 NSGlassEffectView 玻璃材质同步切换到对应的亮/暗外观。
func applyWindowAppearance(w *mygo.Window, appearance string) {
	if w == nil || w.IsDestroyed() {
		return
	}
	h := w.NativeHandle()
	if h == 0 {
		return
	}
	win := objc.ID(h)
	var name string
	switch strings.ToLower(strings.TrimSpace(appearance)) {
	case "light":
		name = "NSAppearanceNameAqua"
	case "dark":
		name = "NSAppearanceNameDarkAqua"
	default:
		win.Send(selSetAppearance, 0)
		return
	}
	clsStr := objc.GetClass("NSString")
	clsApp := objc.GetClass("NSAppearance")
	if clsStr == 0 || clsApp == 0 {
		return
	}
	b := unsafe.StringData(name)
	nsName := objc.ID(clsStr).Send(selAlloc).Send(selInitBytesLenEnc,
		uintptr(unsafe.Pointer(b)), uintptr(len(name)), nsUTF8StringEncoding)
	runtime.KeepAlive(name)
	if nsName == 0 {
		return
	}
	defer nsName.Send(selRelease)
	nsApp := objc.ID(clsApp).Send(selAppearanceNamed, uintptr(nsName))
	if nsApp != 0 {
		win.Send(selSetAppearance, uintptr(nsApp))
	}
}
