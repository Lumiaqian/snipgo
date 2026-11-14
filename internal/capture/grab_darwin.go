//go:build darwin

package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"

	"snipgo/internal/crop"
)

// macOS 14 的 SCScreenshotManager.captureImageWithFilter。
// 不用 15.2 的 captureImageInRect，也不用 macOS 26 的 SCScreenshotConfiguration。
// 完成回调在后台队列执行。超时只停止等待：块和过滤器留着，避免回调晚到时重复释放。

const captureResolutionBest = 1

var (
	loadOnce sync.Once
	loadErr  error

	cgImageGetWidth        func(uintptr) uintptr
	cgImageGetHeight       func(uintptr) uintptr
	cgImageGetBytesPerRow  func(uintptr) uintptr
	cgImageGetBitsPerPixel func(uintptr) uintptr
	cgImageGetBitmapInfo   func(uintptr) uint32
	cgImageGetDataProvider func(uintptr) uintptr
	cgDataProviderCopyData func(uintptr) uintptr
	cfDataGetBytePtr       func(uintptr) *byte
	cfDataGetLength        func(uintptr) int
	cfRetain               func(uintptr) uintptr
	cfRelease              func(uintptr)

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
	selSetShowsCursor = objc.RegisterName("setShowsCursor:")
	selSetResolution  = objc.RegisterName("setCaptureResolution:")
	selShareable      = objc.RegisterName("getShareableContentWithCompletionHandler:")
	selCapture        = objc.RegisterName("captureImageWithFilter:configuration:completionHandler:")
	selInitFilter     = objc.RegisterName("initWithDisplay:excludingWindows:")
	selLocalized      = objc.RegisterName("localizedDescription")
)

func loadFrameworks() error {
	loadOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("绑定屏幕采集库失败: %v", r)
			}
		}()
		for _, path := range []string{
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit",
		} {
			if _, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
				loadErr = err
				return
			}
		}
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
	})
	return loadErr
}

// Grab 按 displays 各截一张冻结图。调用时选区窗口还没出现。
// 像素尺寸必须等于 crop.DIPToPixel 按该屏缩放算出的值，对不上就返回错误。
func Grab(ctx context.Context, displays []Display) ([]crop.Screen, error) {
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
		img, err := captureDisplay(ctx, d, wantW, wantH)
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

func shareableContent(ctx context.Context) (objc.ID, error) {
	ch := make(chan objcResult, 1)
	done := make(chan struct{})
	var block objc.Block
	block = objc.NewBlock(func(_ objc.Block, content objc.ID, nsErr objc.ID) {
		defer close(done)
		if nsErr != 0 {
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

func captureDisplay(ctx context.Context, display objc.ID, width, height int) (*image.NRGBA, error) {
	empty := objc.ID(objc.GetClass("NSArray")).Send(selArray)
	if empty == 0 {
		return nil, errors.New("建不了空的窗口列表")
	}
	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(selAlloc).Send(selInitFilter, display, empty)
	if filter == 0 {
		return nil, errors.New("建不了屏幕过滤器")
	}
	config := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(selAlloc).Send(selInit)
	if config == 0 {
		filter.Send(selRelease)
		return nil, errors.New("建不了截图配置")
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
		// 回调还没来。过滤器、配置和块都留着，晚到的回调会自己释放过滤器。
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
// 超时则不释放：回调可能仍会执行，提前释放会变成重复释放。
func waitObjC(ctx context.Context, ch <-chan objcResult, done <-chan struct{}, block objc.Block) (objcResult, error) {
	select {
	case r := <-ch:
		<-done
		block.Release()
		return r, nil
	case <-ctx.Done():
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
