//go:build darwin

package textrender

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type nsPoint struct{ X, Y float64 }
type nsSize struct{ W, H float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

const (
	nsUTF8StringEncoding          = 4
	cgImageAlphaPremultipliedLast = 1 // RGBA byte order
)

var (
	loadOnce sync.Once
	loadOK   bool

	cgColorSpaceCreateSRGB func() uintptr
	cgColorSpaceRelease    func(uintptr)
	cgBitmapContextCreate  func(unsafe.Pointer, uintptr, uintptr, uintptr, uintptr, uintptr, uint32) uintptr
	cgContextRelease       func(uintptr)
	cgContextTranslateCTM  func(uintptr, float64, float64)
	cgContextScaleCTM      func(uintptr, float64, float64)

	msgBoundRect  func(objc.ID, objc.SEL, nsSize, uintptr, objc.ID) nsRect
	msgDrawInRect func(objc.ID, objc.SEL, nsRect, uintptr, objc.ID)
	msgFont       func(objc.ID, objc.SEL, float64, float64) objc.ID
	msgColor      func(objc.ID, objc.SEL, float64, float64, float64, float64) objc.ID
	msgInitBytes  func(objc.ID, objc.SEL, *byte, uintptr, uintptr) objc.ID

	selAlloc        = objc.RegisterName("alloc")
	selInit         = objc.RegisterName("init")
	selDrain        = objc.RegisterName("drain")
	selInitBytes    = objc.RegisterName("initWithBytes:length:encoding:")
	selAutorelease  = objc.RegisterName("autorelease")
	selSetObjForKey = objc.RegisterName("setObject:forKey:")
	selSysFont      = objc.RegisterName("systemFontOfSize:weight:")
	selMonoFont     = objc.RegisterName("monospacedSystemFontOfSize:weight:")
	selSRGBColor    = objc.RegisterName("colorWithSRGBRed:green:blue:alpha:")
	selBoundRect    = objc.RegisterName("boundingRectWithSize:options:attributes:")
	selDrawInRect   = objc.RegisterName("drawWithRect:options:attributes:")
	selSaveState    = objc.RegisterName("saveGraphicsState")
	selRestoreState = objc.RegisterName("restoreGraphicsState")
	selCtxWithCG    = objc.RegisterName("graphicsContextWithCGContext:flipped:")
	selSetCurrent   = objc.RegisterName("setCurrentContext:")
)

func initAppKit() bool {
	loadOnce.Do(func() {
		defer func() {
			if recover() != nil {
				loadOK = false
			}
		}()
		for _, lib := range []string{
			"/usr/lib/libobjc.A.dylib",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			if _, err := purego.Dlopen(lib, purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
				return
			}
		}
		purego.RegisterLibFunc(&cgColorSpaceCreateSRGB, purego.RTLD_DEFAULT, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cgColorSpaceRelease, purego.RTLD_DEFAULT, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&cgBitmapContextCreate, purego.RTLD_DEFAULT, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cgContextRelease, purego.RTLD_DEFAULT, "CGContextRelease")
		purego.RegisterLibFunc(&cgContextTranslateCTM, purego.RTLD_DEFAULT, "CGContextTranslateCTM")
		purego.RegisterLibFunc(&cgContextScaleCTM, purego.RTLD_DEFAULT, "CGContextScaleCTM")

		msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
		if err != nil {
			return
		}
		stret := msgSend
		if runtime.GOARCH == "amd64" {
			stret, err = purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend_stret")
			if err != nil {
				return
			}
		}
		purego.RegisterFunc(&msgBoundRect, stret)
		purego.RegisterFunc(&msgDrawInRect, msgSend)
		purego.RegisterFunc(&msgFont, msgSend)
		purego.RegisterFunc(&msgColor, msgSend)
		purego.RegisterFunc(&msgInitBytes, msgSend)
		loadOK = true
	})
	return loadOK
}

// Options 控制原生文本栅格化样式。
type Options struct {
	FontSize  float64 // 像素字号
	Weight    float64 // 字重（0=常规, 0.23=Medium, 0.3=Semibold, 0.4=Bold）
	Monospace bool
	MaxWidth  int // 最大换行像素宽度（<=0 表示按自然换行宽度）
	Color     color.NRGBA
}

// Render 使用 macOS 原生 CoreGraphics + AppKit 字体把多行 UTF-8 文本栅格化为透明底 *image.NRGBA。
func Render(text string, opts Options) *image.NRGBA {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if opts.FontSize <= 0 {
		opts.FontSize = 24
	}
	if opts.Color.A == 0 {
		opts.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	img, ok := renderNative(text, opts)
	if !ok {
		return renderFallback(text, opts)
	}
	return img
}

func renderNative(text string, opts Options) (*image.NRGBA, bool) {
	if !initAppKit() {
		return nil, false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	nsStr := makeNSString(text)
	if nsStr == 0 {
		return nil, false
	}
	fontCls := objc.ID(objc.GetClass("NSFont"))
	fontSel := selSysFont
	if opts.Monospace {
		fontSel = selMonoFont
	}
	font := msgFont(fontCls, fontSel, opts.FontSize, opts.Weight)
	if font == 0 && opts.Monospace {
		font = msgFont(fontCls, selSysFont, opts.FontSize, opts.Weight)
	}
	fg := msgColor(
		objc.ID(objc.GetClass("NSColor")),
		selSRGBColor,
		float64(opts.Color.R)/255,
		float64(opts.Color.G)/255,
		float64(opts.Color.B)/255,
		float64(opts.Color.A)/255,
	)
	if font == 0 || fg == 0 {
		return nil, false
	}
	keyFont := makeNSString("NSFont")
	keyColor := makeNSString("NSColor")
	if keyFont == 0 || keyColor == 0 {
		return nil, false
	}
	attrs := objc.ID(objc.GetClass("NSMutableDictionary")).Send(selAlloc).Send(selInit)
	if attrs == 0 {
		return nil, false
	}
	attrs.Send(selAutorelease)
	attrs.Send(selSetObjForKey, font, keyFont)
	attrs.Send(selSetObjForKey, fg, keyColor)

	// NSStringDrawingUsesLineFragmentOrigin (1<<0) | NSStringDrawingUsesFontLeading (1<<1)
	const drawOpts = uintptr(1<<0 | 1<<1)
	boundW := 10000.0
	if opts.MaxWidth > 0 {
		boundW = float64(opts.MaxWidth)
	}
	br := msgBoundRect(nsStr, selBoundRect, nsSize{W: boundW, H: 10000}, drawOpts, attrs)
	w := max(2, int(math.Ceil(br.Size.W))+4)
	h := max(2, int(math.Ceil(br.Size.H))+4)

	raw := make([]byte, w*h*4)
	cs := cgColorSpaceCreateSRGB()
	if cs == 0 {
		return nil, false
	}
	defer cgColorSpaceRelease(cs)

	cgCtx := cgBitmapContextCreate(
		unsafe.Pointer(&raw[0]),
		uintptr(w),
		uintptr(h),
		8,
		uintptr(w*4),
		cs,
		cgImageAlphaPremultipliedLast,
	)
	if cgCtx == 0 {
		return nil, false
	}
	defer cgContextRelease(cgCtx)

	// CGBitmapContext 原点在左下角（y 向上），翻转到左上角原点（y 向下）再交给 flipped NSGraphicsContext。
	cgContextTranslateCTM(cgCtx, 0, float64(h))
	cgContextScaleCTM(cgCtx, 1, -1)

	gfxCls := objc.ID(objc.GetClass("NSGraphicsContext"))
	gfx := gfxCls.Send(selCtxWithCG, cgCtx, true)
	if gfx == 0 {
		return nil, false
	}
	gfxCls.Send(selSaveState)
	gfxCls.Send(selSetCurrent, gfx)
	msgDrawInRect(nsStr, selDrawInRect, nsRect{Origin: nsPoint{X: 2, Y: 2}, Size: nsSize{W: float64(w - 4), H: float64(h - 4)}}, drawOpts, attrs)
	gfxCls.Send(selRestoreState)

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(raw); i += 4 {
		r, g, b, a := raw[i], raw[i+1], raw[i+2], raw[i+3]
		if a == 0 {
			continue
		}
		if a != 255 {
			r = uint8(min(uint16(r)*255/uint16(a), 255))
			g = uint8(min(uint16(g)*255/uint16(a), 255))
			b = uint8(min(uint16(b)*255/uint16(a), 255))
		}
		out.Pix[i] = r
		out.Pix[i+1] = g
		out.Pix[i+2] = b
		out.Pix[i+3] = a
	}
	runtime.KeepAlive(raw)
	runtime.KeepAlive(text)
	return out, true
}

func makeNSString(s string) objc.ID {
	raw := objc.ID(objc.GetClass("NSString")).Send(selAlloc)
	if raw == 0 {
		return 0
	}
	var str objc.ID
	if len(s) == 0 {
		str = raw.Send(selInit)
	} else {
		str = msgInitBytes(raw, selInitBytes, unsafe.StringData(s), uintptr(len(s)), uintptr(nsUTF8StringEncoding))
		runtime.KeepAlive(s)
	}
	if str != 0 {
		str.Send(selAutorelease)
	}
	return str
}
