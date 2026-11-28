//go:build darwin

package capture

import (
	"image"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

const (
	cgWindowListOptionOnScreenOnly     = 1 << 0
	cgWindowListExcludeDesktopElements = 1 << 4
	cfNumberSInt32Type                 = 3
	cfNumberFloat64Type                = 6
	cfStringEncodingUTF8               = 0x08000100
	minDetectWindowDIP                 = 64
	maxSelectableWindowLayer           = 20 // 排除 Dock (20)、菜单栏 (24)、状态栏 (25)
	nsActivationPolicyRegular          = 0
	nsActivationPolicyAccessory        = 1
)

type cgRect struct {
	X, Y, W, H float64
}

var (
	winLoadOnce sync.Once
	winLoadOK   bool
	popupLayer  int32

	cgWindowListCopyWindowInfo             func(uint32, uint32) uintptr
	cfArrayGetCount                        func(uintptr) int
	cfArrayGetValueAtIndex                 func(uintptr, int) uintptr
	cfDictionaryGetValueIfPresent          func(uintptr, uintptr, *uintptr) bool
	cfNumberGetValue                       func(uintptr, int, unsafe.Pointer) bool
	cfBooleanGetValue                      func(uintptr) bool
	cfStringCreateWithCString              func(uintptr, string, uint32) uintptr
	cgRectMakeWithDictionaryRepresentation func(uintptr, *cgRect) bool

	keyWindowOwnerPID  uintptr
	keyWindowNumber    uintptr
	keyWindowLayer     uintptr
	keyWindowBounds    uintptr
	keyWindowAlpha     uintptr
	keyWindowIsOnscr   uintptr
	keyWindowStoreType uintptr
	keyWindowName      uintptr

	selRunningAppWithPID = objc.RegisterName("runningApplicationWithProcessIdentifier:")
	selActivationPolicy  = objc.RegisterName("activationPolicy")
	selSharedWorkspace   = objc.RegisterName("sharedWorkspace")
	selFrontmostApp      = objc.RegisterName("frontmostApplication")
	selProcessIdentifier = objc.RegisterName("processIdentifier")
)

func initWindowSymbols() bool {
	winLoadOnce.Do(func() {
		if loadFrameworks() != nil {
			return
		}
		defer func() {
			if recover() != nil {
				winLoadOK = false
			}
		}()
		_, _ = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
		purego.RegisterLibFunc(&cgWindowListCopyWindowInfo, purego.RTLD_DEFAULT, "CGWindowListCopyWindowInfo")
		purego.RegisterLibFunc(&cfArrayGetCount, purego.RTLD_DEFAULT, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayGetValueAtIndex, purego.RTLD_DEFAULT, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfDictionaryGetValueIfPresent, purego.RTLD_DEFAULT, "CFDictionaryGetValueIfPresent")
		purego.RegisterLibFunc(&cfNumberGetValue, purego.RTLD_DEFAULT, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfBooleanGetValue, purego.RTLD_DEFAULT, "CFBooleanGetValue")
		purego.RegisterLibFunc(&cfStringCreateWithCString, purego.RTLD_DEFAULT, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cgRectMakeWithDictionaryRepresentation, purego.RTLD_DEFAULT, "CGRectMakeWithDictionaryRepresentation")

		var levelForKey func(int32) int32
		purego.RegisterLibFunc(&levelForKey, purego.RTLD_DEFAULT, "CGWindowLevelForKey")
		popupLayer = levelForKey(11) // kCGPopUpMenuWindowLevelKey
		keyWindowOwnerPID = cfStringCreateWithCString(0, "kCGWindowOwnerPID", cfStringEncodingUTF8)
		keyWindowNumber = cfStringCreateWithCString(0, "kCGWindowNumber", cfStringEncodingUTF8)
		keyWindowLayer = cfStringCreateWithCString(0, "kCGWindowLayer", cfStringEncodingUTF8)
		keyWindowBounds = cfStringCreateWithCString(0, "kCGWindowBounds", cfStringEncodingUTF8)
		keyWindowAlpha = cfStringCreateWithCString(0, "kCGWindowAlpha", cfStringEncodingUTF8)
		keyWindowIsOnscr = cfStringCreateWithCString(0, "kCGWindowIsOnscreen", cfStringEncodingUTF8)
		keyWindowStoreType = cfStringCreateWithCString(0, "kCGWindowStoreType", cfStringEncodingUTF8)
		keyWindowName = cfStringCreateWithCString(0, "kCGWindowName", cfStringEncodingUTF8)
		winLoadOK = keyWindowOwnerPID != 0 && keyWindowLayer != 0 && keyWindowBounds != 0 && keyWindowAlpha != 0
	})
	return winLoadOK
}

// VisibleWindows 返回按严格 Z 序（由前到后）排列的可见窗口矩形列表，以及当前前台活跃应用的主窗口（front）。
func VisibleWindows(cursor image.Point, displays []Display, excludeTitle string) (front image.Rectangle, windows []image.Rectangle) {
	fi, list := VisibleWindowInfos(cursor, displays, excludeTitle)
	windows = make([]image.Rectangle, len(list))
	for i, w := range list {
		windows[i] = w.Bounds
	}
	return fi.Bounds, windows
}

// VisibleWindowInfos 返回带 CGWindowID 的可见窗口信息列表（供独立单窗无背景抠图使用）。
func VisibleWindowInfos(cursor image.Point, displays []Display, excludeTitle string) (front WindowInfo, windows []WindowInfo) {
	// 可能在后台 goroutine 调用：NSRunningApplication/NSWorkspace 返回 autoreleased 对象，
	// 必须固定线程并自建 autorelease pool，否则对象在线程上一直不被释放。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	if !initWindowSymbols() {
		return WindowInfo{}, nil
	}
	opts := uint32(cgWindowListOptionOnScreenOnly | cgWindowListExcludeDesktopElements)
	list := cgWindowListCopyWindowInfo(opts, 0)
	if list == 0 {
		return WindowInfo{}, nil
	}
	defer cfRelease(list)

	myPID := int32(os.Getpid())
	frontPID := frontmostAppPID(myPID)

	policyByPID := map[int32]int{}
	nsRunningAppCls := objc.ID(objc.GetClass("NSRunningApplication"))
	appPolicy := func(pid int32) int {
		if pol, seen := policyByPID[pid]; seen {
			return pol
		}
		pol := -1
		if nsRunningAppCls != 0 && pid > 0 {
			if ra := nsRunningAppCls.Send(selRunningAppWithPID, pid); ra != 0 {
				pol = objc.Send[int](ra, selActivationPolicy)
			}
		}
		policyByPID[pid] = pol
		return pol
	}

	n := cfArrayGetCount(list)
	windows = make([]WindowInfo, 0, n)
	var frontFallback WindowInfo

	for i := range n {
		dict := cfArrayGetValueAtIndex(list, i)
		if dict == 0 {
			continue
		}
		layer, ok := dictInt32(dict, keyWindowLayer)
		if !ok {
			continue
		}
		onScreen, _ := dictBool(dict, keyWindowIsOnscr)
		if !onScreen {
			continue
		}
		if store, hasStore := dictInt32(dict, keyWindowStoreType); hasStore && store == 0 {
			continue
		}
		alpha, _ := dictFloat64(dict, keyWindowAlpha)
		if alpha <= 0.05 {
			continue
		}

		pid, _ := dictInt32(dict, keyWindowOwnerPID)
		menu := layer == popupLayer && pid > 0 && pid != myPID
		if pid == myPID {
			if layer != 3 {
				continue
			}
			if excludeTitle != "" && dictString(dict, keyWindowName) == excludeTitle {
				continue
			}
		} else if layer < 0 || (layer >= maxSelectableWindowLayer && !menu) {
			continue
		}

		var boundsDict uintptr
		if !cfDictionaryGetValueIfPresent(dict, keyWindowBounds, &boundsDict) || boundsDict == 0 {
			continue
		}
		var r cgRect
		if !cgRectMakeWithDictionaryRepresentation(boundsDict, &r) {
			continue
		}
		minSize := minDetectWindowDIP
		if menu {
			minSize = 16
		}
		if r.W < float64(minSize) || r.H < float64(minSize) {
			continue
		}
		rect := image.Rect(
			int(math.Round(r.X)),
			int(math.Round(r.Y)),
			int(math.Round(r.X+r.W)),
			int(math.Round(r.Y+r.H)),
		).Canon()
		if menu && !selectableMenu(nsRunningAppCls.Send(selRunningAppWithPID, pid), rect, displays) {
			continue
		}

		if pid != myPID {
			switch appPolicy(pid) {
			case nsActivationPolicyRegular:
			case nsActivationPolicyAccessory:
				if isFullDisplayOverlay(rect, displays) {
					continue
				}
			default:
				continue
			}
		}

		wid, _ := dictInt32(dict, keyWindowNumber)
		info := WindowInfo{ID: uint32(wid), Bounds: rect, PopupMenu: menu}
		windows = append(windows, info)
		if frontPID > 0 && pid == frontPID {
			if frontFallback.Bounds.Empty() {
				frontFallback = info
			}
			if front.Bounds.Empty() && cursor.In(rect) {
				front = info
			}
		}
	}
	if front.Bounds.Empty() {
		front = frontFallback
	}
	if front.Bounds.Empty() && len(windows) > 0 {
		front = windows[0]
	}
	return front, windows
}

func selectableMenu(app objc.ID, rect image.Rectangle, displays []Display) bool {
	if app == 0 || isFullDisplayOverlay(rect, displays) {
		return false
	}
	path := cstrings.NSStringToString(app.Send(objc.RegisterName("bundleURL")).Send(objc.RegisterName("path")))
	return menuOwnerOnDisplay(path, rect, displays)
}

func menuOwnerOnDisplay(path string, rect image.Rectangle, displays []Display) bool {
	if path == "" || strings.Contains(path, "/Input Methods/") || (strings.HasPrefix(path, "/System/Library/CoreServices/") && path != "/System/Library/CoreServices/Finder.app") {
		return false
	}
	for _, d := range displays {
		if rect.Overlaps(d.Bounds) {
			return true
		}
	}
	return false
}

func isFullDisplayOverlay(rect image.Rectangle, displays []Display) bool {
	for _, d := range displays {
		b := d.Bounds.Canon()
		if !b.Empty() && rect.Dx() >= b.Dx()-4 && rect.Dy() >= b.Dy()-4 {
			inter := rect.Intersect(b)
			if inter.Dx() >= b.Dx()-4 && inter.Dy() >= b.Dy()-4 {
				return true
			}
		}
	}
	return false
}

func frontmostAppPID(selfPID int32) int32 {
	wsCls := objc.ID(objc.GetClass("NSWorkspace"))
	if wsCls == 0 {
		return 0
	}
	ws := wsCls.Send(selSharedWorkspace)
	if ws == 0 {
		return 0
	}
	app := ws.Send(selFrontmostApp)
	if app == 0 {
		return 0
	}
	pid := objc.Send[int32](app, selProcessIdentifier)
	if pid == selfPID {
		return 0
	}
	return pid
}

func dictInt32(dict, key uintptr) (int32, bool) {
	if key == 0 {
		return 0, false
	}
	var num uintptr
	if !cfDictionaryGetValueIfPresent(dict, key, &num) || num == 0 {
		return 0, false
	}
	var v int32
	if !cfNumberGetValue(num, cfNumberSInt32Type, unsafe.Pointer(&v)) {
		return 0, false
	}
	return v, true
}

func dictFloat64(dict, key uintptr) (float64, bool) {
	if key == 0 {
		return 0, false
	}
	var num uintptr
	if !cfDictionaryGetValueIfPresent(dict, key, &num) || num == 0 {
		return 0, false
	}
	var v float64
	if !cfNumberGetValue(num, cfNumberFloat64Type, unsafe.Pointer(&v)) {
		return 0, false
	}
	return v, true
}

func dictBool(dict, key uintptr) (bool, bool) {
	if key == 0 || cfBooleanGetValue == nil {
		return false, false
	}
	var val uintptr
	if !cfDictionaryGetValueIfPresent(dict, key, &val) || val == 0 {
		return false, false
	}
	return cfBooleanGetValue(val), true
}

func dictString(dict, key uintptr) (out string) {
	if key == 0 {
		return ""
	}
	var val uintptr
	if !cfDictionaryGetValueIfPresent(dict, key, &val) || val == 0 {
		return ""
	}
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	return cstrings.NSStringToString(objc.ID(val))
}
