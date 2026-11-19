//go:build darwin

package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"

	"snipgo/internal/crop"
)

// macOS 14 的 SCScreenshotManager.captureImageWithFilter。
// 不用 15.2 的 captureImageInRect，也不用 macOS 26 的 SCScreenshotConfiguration。
// 完成回调在后台队列执行。超时后由后台 goroutine 等回调结束，再释放块和迟到的结果。

const captureResolutionBest = 1

var (
	loadOnce sync.Once
	loadErr  error

	cgImageGetWidth                func(uintptr) uintptr
	cgImageGetHeight               func(uintptr) uintptr
	cgImageGetBytesPerRow          func(uintptr) uintptr
	cgImageGetBitsPerPixel         func(uintptr) uintptr
	cgImageGetBitmapInfo           func(uintptr) uint32
	cgImageGetDataProvider         func(uintptr) uintptr
	cgDataProviderCopyData         func(uintptr) uintptr
	cfDataGetBytePtr               func(uintptr) *byte
	cfDataGetLength                func(uintptr) int
	cfRetain                       func(uintptr) uintptr
	cfRelease                      func(uintptr)
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool
	msgSetRect                     func(objc.ID, objc.SEL, cgRect)

	selRetain         = objc.RegisterName("retain")
	selRelease        = objc.RegisterName("release")
	selAlloc          = objc.RegisterName("alloc")
	selInit           = objc.RegisterName("init")
	selDrain          = objc.RegisterName("drain")
	selCount          = objc.RegisterName("count")
	selObjectAtIndex  = objc.RegisterName("objectAtIndex:")
	selArray          = objc.RegisterName("array")
	selDisplays       = objc.RegisterName("displays")
	selDisplayID      = objc.RegisterName("displayID")
	selSetWidth       = objc.RegisterName("setWidth:")
	selSetHeight      = objc.RegisterName("setHeight:")
	selSetSourceRect  = objc.RegisterName("setSourceRect:")
	selSetShowsCursor = objc.RegisterName("setShowsCursor:")
	selSetResolution  = objc.RegisterName("setCaptureResolution:")
	selShareable      = objc.RegisterName("getShareableContentWithCompletionHandler:")
	selCapture        = objc.RegisterName("captureImageWithFilter:configuration:completionHandler:")
	selInitFilter     = objc.RegisterName("initWithDisplay:excludingWindows:")
	selInitIndepWin   = objc.RegisterName("initWithDesktopIndependentWindow:")
	selWindowID       = objc.RegisterName("windowID")
	selFrame          = objc.RegisterName("frame")
	selLocalized      = objc.RegisterName("localizedDescription")
	selCode           = objc.RegisterName("code")
	selWindows        = objc.RegisterName("windows")
	selOwningApp      = objc.RegisterName("owningApplication")
	selProcessID      = objc.RegisterName("processID")
	selTitle          = objc.RegisterName("title")
	selAddObject      = objc.RegisterName("addObject:")
)

func loadFrameworks() error {
	loadOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("绑定屏幕采集库失败: %v", r)
			}
		}()
		for _, path := range []string{
			"/usr/lib/libobjc.A.dylib",
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit",
		} {
			if _, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
				loadErr = err
				return
			}
		}
		msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
		if err != nil {
			loadErr = err
			return
		}
		purego.RegisterFunc(&msgSetRect, msgSend)
		purego.RegisterLibFunc(&cgImageGetWidth, purego.RTLD_DEFAULT, "CGImageGetWidth")
		purego.RegisterLibFunc(&cgImageGetHeight, purego.RTLD_DEFAULT, "CGImageGetHeight")
		purego.RegisterLibFunc(&cgImageGetBytesPerRow, purego.RTLD_DEFAULT, "CGImageGetBytesPerRow")
		purego.RegisterLibFunc(&cgImageGetBitsPerPixel, purego.RTLD_DEFAULT, "CGImageGetBitsPerPixel")
		purego.RegisterLibFunc(&cgImageGetBitmapInfo, purego.RTLD_DEFAULT, "CGImageGetBitmapInfo")
		purego.RegisterLibFunc(&cgImageGetDataProvider, purego.RTLD_DEFAULT, "CGImageGetDataProvider")
		purego.RegisterLibFunc(&cgDataProviderCopyData, purego.RTLD_DEFAULT, "CGDataProviderCopyData")
		purego.RegisterLibFunc(&cfDataGetBytePtr, purego.RTLD_DEFAULT, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&cfDataGetLength, purego.RTLD_DEFAULT, "CFDataGetLength")
		purego.RegisterLibFunc(&cfRetain, purego.RTLD_DEFAULT, "CFRetain")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
		purego.RegisterLibFunc(&cgPreflightScreenCaptureAccess, purego.RTLD_DEFAULT, "CGPreflightScreenCaptureAccess")
		purego.RegisterLibFunc(&cgRequestScreenCaptureAccess, purego.RTLD_DEFAULT, "CGRequestScreenCaptureAccess")
	})
	return loadErr
}

// HasPermission 检查当前进程是否已获授屏幕录制权限（不会触发系统弹窗）。
func HasPermission() bool {
	if loadFrameworks() != nil || cgPreflightScreenCaptureAccess == nil {
		return false
	}
	return cgPreflightScreenCaptureAccess()
}

// RequestPermission 请求屏幕录制权限：首次调用会把当前应用注册进「系统设置 → 录屏与系统录音」并弹出系统授权框。
func RequestPermission() bool {
	if loadFrameworks() != nil || cgRequestScreenCaptureAccess == nil {
		return false
	}
	if cgPreflightScreenCaptureAccess != nil && cgPreflightScreenCaptureAccess() {
		return true
	}
	return cgRequestScreenCaptureAccess()
}

// Grab 按 displays 各截一张冻结图。
// 本进程里标题为 excludeTitle 的窗口不入镜，用来挡掉还没关完的选区蒙层；本进程其他窗口（贴图）照常截入。
// 像素尺寸必须等于 crop.DIPToPixel 按该屏缩放算出的值，对不上就返回错误。
func Grab(ctx context.Context, displays []Display, excludeTitle string) ([]crop.Screen, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	if objc.GetClass("SCScreenshotManager") == 0 {
		return nil, errors.New("系统没有 SCScreenshotManager，需要 macOS 14 或更新版本")
	}
	if len(displays) == 0 {
		return nil, errors.New("没有显示器")
	}
	// 自动释放池只能在创建它的线程上 drain，等回调期间 goroutine 不能换线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	content, err := shareableContent(ctx)
	if err != nil {
		return nil, err
	}
	defer content.Send(selRelease)

	found := map[int64]objc.ID{}
	arr := content.Send(selDisplays)
	n := int(objc.Send[uintptr](arr, selCount))
	for i := range n {
		d := arr.Send(selObjectAtIndex, uintptr(i))
		d.Send(selRetain)
		defer d.Send(selRelease)
		found[int64(objc.Send[uint32](d, selDisplayID))] = d
	}

	excluded := ownWindows(content, excludeTitle)
	if excluded == 0 {
		return nil, errors.New("建不了排除窗口列表")
	}

	out := make([]crop.Screen, len(displays))
	for i, s := range displays {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := found[s.ID]
		if d == 0 {
			return nil, fmt.Errorf("找不到显示器 %d", s.ID)
		}
		if s.Scale <= 0 || math.IsNaN(s.Scale) || math.IsInf(s.Scale, 0) {
			return nil, fmt.Errorf("显示器 %d 的缩放无效", s.ID)
		}
		b := s.Bounds.Canon()
		wantW := crop.DIPToPixel(b.Max.X, b.Min.X, s.Scale)
		wantH := crop.DIPToPixel(b.Max.Y, b.Min.Y, s.Scale)
		if wantW <= 0 || wantH <= 0 {
			return nil, fmt.Errorf("显示器 %d 的尺寸无效", s.ID)
		}
		img, err := captureDisplay(ctx, d, excluded, nil, wantW, wantH)
		if err != nil {
			return nil, err
		}
		if img.Bounds().Dx() != wantW || img.Bounds().Dy() != wantH {
			return nil, fmt.Errorf("显示器 %d 的画面是 %dx%d，按缩放应为 %dx%d", s.ID, img.Bounds().Dx(), img.Bounds().Dy(), wantW, wantH)
		}
		out[i] = crop.Screen{Bounds: b, Scale: s.Scale, Pixels: img}
	}
	return out, nil
}

// GrabRect 只采集选区 sel 命中的显示器局部区域（使用 SCStreamConfiguration.sourceRect），
// 并拼接成最终裁剪图像，避免整屏采集带来的启动延迟和内存开销。
func GrabRect(ctx context.Context, displays []Display, sel image.Rectangle, excludeTitle string) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sel = sel.Canon()
	if sel.Empty() {
		return nil, errors.New("选区为空")
	}
	var hits []Display
	for _, s := range displays {
		if !sel.Intersect(s.Bounds.Canon()).Empty() {
			hits = append(hits, s)
		}
	}
	if len(hits) == 0 {
		return nil, errors.New("选区不在屏幕上")
	}
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	if objc.GetClass("SCScreenshotManager") == 0 {
		return nil, errors.New("系统没有 SCScreenshotManager，需要 macOS 14 或更新版本")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	content, err := shareableContent(ctx)
	if err != nil {
		return nil, err
	}
	defer content.Send(selRelease)

	found := map[int64]objc.ID{}
	arr := content.Send(selDisplays)
	n := int(objc.Send[uintptr](arr, selCount))
	for i := range n {
		d := arr.Send(selObjectAtIndex, uintptr(i))
		d.Send(selRetain)
		defer d.Send(selRelease)
		found[int64(objc.Send[uint32](d, selDisplayID))] = d
	}

	excluded := ownWindows(content, excludeTitle)
	if excluded == 0 {
		return nil, errors.New("建不了排除窗口列表")
	}

	tiles := make([]crop.Tile, 0, len(hits))
	for _, s := range hits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := found[s.ID]
		if d == 0 {
			return nil, fmt.Errorf("找不到显示器 %d", s.ID)
		}
		if s.Scale <= 0 || math.IsNaN(s.Scale) || math.IsInf(s.Scale, 0) {
			return nil, fmt.Errorf("显示器 %d 的缩放无效", s.ID)
		}
		b := s.Bounds.Canon()
		inter := sel.Intersect(b)
		wantW := crop.DIPToPixel(inter.Max.X, b.Min.X, s.Scale) - crop.DIPToPixel(inter.Min.X, b.Min.X, s.Scale)
		wantH := crop.DIPToPixel(inter.Max.Y, b.Min.Y, s.Scale) - crop.DIPToPixel(inter.Min.Y, b.Min.Y, s.Scale)
		if wantW <= 0 || wantH <= 0 {
			continue
		}
		src := cgRect{
			X: float64(inter.Min.X - b.Min.X),
			Y: float64(inter.Min.Y - b.Min.Y),
			W: float64(inter.Dx()),
			H: float64(inter.Dy()),
		}
		img, err := captureDisplay(ctx, d, excluded, &src, wantW, wantH)
		if err != nil || img == nil || img.Bounds().Dx() != wantW || img.Bounds().Dy() != wantH {
			if errors.Is(err, ErrPermissionDenied) || ctx.Err() != nil {
				if err != nil {
					return nil, err
				}
				return nil, ctx.Err()
			}
			// 若 SCStreamConfiguration 对奇数/极小像素尺寸或局部 sourceRect 返回参数错误，
			// 自动回退为采集当前单屏整帧并精确裁剪 inter，确保任何尺寸选区都不会采集失败。
			fullW := crop.DIPToPixel(b.Max.X, b.Min.X, s.Scale)
			fullH := crop.DIPToPixel(b.Max.Y, b.Min.Y, s.Scale)
			fullImg, fullErr := captureDisplay(ctx, d, excluded, nil, fullW, fullH)
			if fullErr != nil {
				return nil, fullErr
			}
			cropped, cropErr := crop.Crop([]crop.Screen{{Bounds: b, Scale: s.Scale, Pixels: fullImg}}, inter)
			if cropErr != nil {
				return nil, cropErr
			}
			img = cropped
		}
		tiles = append(tiles, crop.Tile{Bounds: inter, Scale: s.Scale, Pixels: img})
	}
	return crop.Stitch(tiles, sel)
}

// GrabWindow 使用 SCContentFilter(initWithDesktopIndependentWindow:) 单独抠出指定窗口本体（自动排除桌面壁纸与其他遮挡窗口）。
func GrabWindow(ctx context.Context, winID uint32, scale float64) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if winID == 0 {
		return nil, errors.New("无效窗口 ID")
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 2.0
	}
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	if objc.GetClass("SCScreenshotManager") == 0 {
		return nil, errors.New("系统没有 SCScreenshotManager，需要 macOS 14 或更新版本")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	content, err := shareableContent(ctx)
	if err != nil {
		return nil, err
	}
	defer content.Send(selRelease)

	var scWin objc.ID
	wins := content.Send(selWindows)
	n := int(objc.Send[uintptr](wins, selCount))
	for i := range n {
		w := wins.Send(selObjectAtIndex, uintptr(i))
		if objc.Send[uint32](w, selWindowID) == winID {
			scWin = w
			break
		}
	}
	if scWin == 0 {
		return nil, fmt.Errorf("找不到窗口 %d", winID)
	}
	frame := objc.Send[cgRect](scWin, selFrame)
	wantW := max(2, int(math.Round(frame.W*scale)))
	wantH := max(2, int(math.Round(frame.H*scale)))

	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(selAlloc).Send(selInitIndepWin, scWin)
	if filter == 0 {
		return nil, errors.New("建不了独立窗口过滤器")
	}
	config := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(selAlloc).Send(selInit)
	if config == 0 {
		filter.Send(selRelease)
		return nil, errors.New("建不了截图配置")
	}
	config.Send(selSetWidth, uintptr(wantW))
	config.Send(selSetHeight, uintptr(wantH))
	config.Send(selSetShowsCursor, false)
	config.Send(selSetResolution, captureResolutionBest)

	var once sync.Once
	releaseCfg := func() {
		once.Do(func() {
			filter.Send(selRelease)
			config.Send(selRelease)
		})
	}
	ch := make(chan objcResult, 1)
	done := make(chan struct{})
	var block objc.Block
	block = objc.NewBlock(func(_ objc.Block, cg unsafe.Pointer, nsErr objc.ID) {
		defer close(done)
		defer releaseCfg()
		ch <- shotResult(cg, nsErr)
	})
	objc.ID(objc.GetClass("SCScreenshotManager")).Send(selCapture, filter, config, block)
	r, err := waitObjC(ctx, ch, done, block)
	if err != nil {
		return nil, err
	}
	return r.img, r.err
}

func shareableContent(ctx context.Context) (objc.ID, error) {
	ch := make(chan objcResult, 1)
	done := make(chan struct{})
	var block objc.Block
	block = objc.NewBlock(func(_ objc.Block, content objc.ID, nsErr objc.ID) {
		defer close(done)
		if nsErr != 0 {
			if objc.Send[int](nsErr, selCode) == -3801 {
				ch <- objcResult{err: ErrPermissionDenied}
				return
			}
			ch <- objcResult{err: errors.New(errorText(nsErr))}
			return
		}
		if content == 0 {
			ch <- objcResult{err: errors.New("没有可截的屏幕")}
			return
		}
		content.Send(selRetain)
		ch <- objcResult{content: content}
	})
	objc.ID(objc.GetClass("SCShareableContent")).Send(selShareable, block)
	r, err := waitObjC(ctx, ch, done, block)
	if err != nil {
		return 0, err
	}
	return r.content, r.err
}

// ownWindows 返回本进程中标题为 title 的 SCWindow 列表（自动释放，过滤器会 retain）。
func ownWindows(content objc.ID, title string) objc.ID {
	list := objc.ID(objc.GetClass("NSMutableArray")).Send(selArray)
	if list == 0 || title == "" {
		return list
	}
	pid := int32(os.Getpid())
	wins := content.Send(selWindows)
	n := int(objc.Send[uintptr](wins, selCount))
	for i := range n {
		w := wins.Send(selObjectAtIndex, uintptr(i))
		app := w.Send(selOwningApp)
		if app == 0 || objc.Send[int32](app, selProcessID) != pid {
			continue
		}
		t := w.Send(selTitle)
		if t != 0 && cstrings.NSStringToString(t) == title {
			list.Send(selAddObject, w)
		}
	}
	return list
}

func captureDisplay(ctx context.Context, display, excluded objc.ID, srcRect *cgRect, width, height int) (*image.NRGBA, error) {
	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(selAlloc).Send(selInitFilter, display, excluded)
	if filter == 0 {
		return nil, errors.New("建不了屏幕过滤器")
	}
	config := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(selAlloc).Send(selInit)
	if config == 0 {
		filter.Send(selRelease)
		return nil, errors.New("建不了截图配置")
	}
	if srcRect != nil {
		msgSetRect(config, selSetSourceRect, *srcRect)
	}
	// width 和 height 是像素。默认 1920×1080，不设的话 Retina 尺寸是错的。
	config.Send(selSetWidth, uintptr(width))
	config.Send(selSetHeight, uintptr(height))
	// Go 的 false 会放进寄存器的 0，也就是 BOOL 的 NO。默认是显示光标。
	config.Send(selSetShowsCursor, false)
	config.Send(selSetResolution, captureResolutionBest)

	var once sync.Once
	releaseCfg := func() {
		once.Do(func() {
			filter.Send(selRelease)
			config.Send(selRelease)
		})
	}

	ch := make(chan objcResult, 1)
	done := make(chan struct{})
	var block objc.Block
	block = objc.NewBlock(func(_ objc.Block, cg unsafe.Pointer, nsErr objc.ID) {
		defer close(done)
		defer releaseCfg()
		ch <- shotResult(cg, nsErr)
	})
	objc.ID(objc.GetClass("SCScreenshotManager")).Send(selCapture, filter, config, block)
	r, err := waitObjC(ctx, ch, done, block)
	if err != nil {
		// 回调还没来。晚到的回调自己释放过滤器和配置，块由 waitObjC 的后台 goroutine 释放。
		return nil, err
	}
	return r.img, r.err
}

type objcResult struct {
	content objc.ID
	img     *image.NRGBA
	err     error
}

// waitObjC 等回调把结果放进 ch，并等回调函数返回后再释放块。
// 超时则先返回，回调晚到时由后台 goroutine 收尾：释放已 retain 的 content，再释放块。
func waitObjC(ctx context.Context, ch <-chan objcResult, done <-chan struct{}, block objc.Block) (objcResult, error) {
	select {
	case r := <-ch:
		<-done
		block.Release()
		return r, nil
	case <-ctx.Done():
		go func() {
			r := <-ch
			<-done
			if r.content != 0 {
				r.content.Send(selRelease)
			}
			block.Release()
		}()
		return objcResult{}, ctx.Err()
	}
}

// shotResult 在回调里复制像素。复制过程 panic 时变成错误，避免块回调把进程带走。
func shotResult(cg unsafe.Pointer, nsErr objc.ID) (r objcResult) {
	defer func() {
		if p := recover(); p != nil {
			r = objcResult{err: fmt.Errorf("截图回调失败: %v", p)}
		}
	}()
	if nsErr != 0 {
		return objcResult{err: errors.New(errorText(nsErr))}
	}
	img, err := copyCGImage(uintptr(cg))
	return objcResult{img: img, err: err}
}

func errorText(id objc.ID) (msg string) {
	if id == 0 {
		return "截图失败"
	}
	// localizedDescription 应是 NSString。不是的话 purego 会 panic，这里接住，避免回调把进程带走。
	defer func() {
		if recover() != nil {
			msg = "截图失败"
		}
	}()
	s := id.Send(selLocalized)
	if s == 0 {
		return "截图失败"
	}
	text := cstrings.NSStringToString(s)
	if text == "" {
		return "截图失败"
	}
	return text
}

// copyCGImage 在回调里把像素拷进 Go 内存。CGImage 只在回调期间有效，先 retain 再复制。
func copyCGImage(img uintptr) (*image.NRGBA, error) {
	if img == 0 {
		return nil, errors.New("没有截到图像")
	}
	cfRetain(img)
	defer cfRelease(img)
	w := int(cgImageGetWidth(img))
	h := int(cgImageGetHeight(img))
	bpp := int(cgImageGetBitsPerPixel(img))
	if bpp != 32 {
		return nil, fmt.Errorf("每像素 %d 位，需要 32 位 BGRA", bpp)
	}
	stride := int(cgImageGetBytesPerRow(img))
	info := cgImageGetBitmapInfo(img)
	provider := cgImageGetDataProvider(img)
	if provider == 0 {
		return nil, errors.New("图像没有像素")
	}
	data := cgDataProviderCopyData(provider)
	if data == 0 {
		return nil, errors.New("读不到像素")
	}
	defer cfRelease(data)
	ptr := cfDataGetBytePtr(data)
	n := cfDataGetLength(data)
	if ptr == nil || n <= 0 {
		return nil, errors.New("像素是空的")
	}
	raw := unsafe.Slice(ptr, n)
	return imageFromBGRA(raw, w, h, stride, info)
}
