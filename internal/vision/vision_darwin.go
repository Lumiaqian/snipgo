//go:build darwin

package vision

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

type cgRect struct {
	X, Y, W, H float64
}

const (
	nsUTF8StringEncoding     = 4
	cgImageAlphaNoneSkipLast = 5 // 24-bit 不透明 RGBX，避免半透明圆角/窗口阴影干扰 Vision 对比度归一化
	cgRenderingIntentDef     = 0

	cvPixelBufferLockReadOnly = 1
	cvPixelFormatOneComp32F   = 0x4C303066 // 'L00f' kCVPixelFormatType_OneComponent32Float
	cvPixelFormatOneComp8     = 0x4C303038 // 'L008' kCVPixelFormatType_OneComponent8
)

var (
	visOnce sync.Once
	visErr  error

	cgColorSpaceCreateDeviceRGB func() uintptr
	cgColorSpaceRelease         func(uintptr)
	cgDataProviderCreateWithCF  func(uintptr) uintptr
	cgDataProviderRelease       func(uintptr)
	cfDataCreate                func(uintptr, unsafe.Pointer, int) uintptr
	cfRelease                   func(uintptr)
	cgImageCreate               func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uint32, uintptr, uintptr, bool, int32) uintptr
	cgImageRelease              func(uintptr)
	cvPixelBufferLock           func(uintptr, uint64) int32
	cvPixelBufferUnlock         func(uintptr, uint64) int32
	cvPixelBufferGetBaseAddress func(uintptr) unsafe.Pointer
	cvPixelBufferGetWidth       func(uintptr) uintptr
	cvPixelBufferGetHeight      func(uintptr) uintptr
	cvPixelBufferGetBytesPerRow func(uintptr) uintptr
	cvPixelBufferGetFormatType  func(uintptr) uint32
	cvPixelBufferRelease        func(uintptr)
	msgBoundingBox              func(objc.ID, objc.SEL) cgRect
	msgSetFloat32               func(objc.ID, objc.SEL, float32)

	selAlloc            = objc.RegisterName("alloc")
	selInit             = objc.RegisterName("init")
	selDrain            = objc.RegisterName("drain")
	selAutorelease      = objc.RegisterName("autorelease")
	selInitBytes        = objc.RegisterName("initWithBytes:length:encoding:")
	selDictionary       = objc.RegisterName("dictionary")
	selArrayWithObjs    = objc.RegisterName("arrayWithObjects:count:")
	selInitCGImage      = objc.RegisterName("initWithCGImage:options:")
	selPerformReqs      = objc.RegisterName("performRequests:error:")
	selSetRecLevel      = objc.RegisterName("setRecognitionLevel:")
	selSetLangCorr      = objc.RegisterName("setUsesLanguageCorrection:")
	selSetRecLangs      = objc.RegisterName("setRecognitionLanguages:")
	selSetMinTextHeight = objc.RegisterName("setMinimumTextHeight:")
	selSetSymbologies   = objc.RegisterName("setSymbologies:")
	selResults          = objc.RegisterName("results")
	selCount            = objc.RegisterName("count")
	selObjectAtIndex    = objc.RegisterName("objectAtIndex:")
	selTopCandidates    = objc.RegisterName("topCandidates:")
	selString           = objc.RegisterName("string")
	selBoundingBox      = objc.RegisterName("boundingBox")
	selPayloadStringVal = objc.RegisterName("payloadStringValue")
	selLocalizedDesc    = objc.RegisterName("localizedDescription")
	selAllInstances     = objc.RegisterName("allInstances")
	selGenScaledMask    = objc.RegisterName("generateScaledMaskForImageForInstances:fromRequestHandler:error:")
)

func loadVision() error {
	visOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				visErr = fmt.Errorf("加载 Vision 框架失败: %v", r)
			}
		}()
		for _, lib := range []string{
			"/usr/lib/libobjc.A.dylib",
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/CoreVideo.framework/CoreVideo",
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/Vision.framework/Vision",
		} {
			if _, err := purego.Dlopen(lib, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
				visErr = err
				return
			}
		}
		purego.RegisterLibFunc(&cgColorSpaceCreateDeviceRGB, purego.RTLD_DEFAULT, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cgColorSpaceRelease, purego.RTLD_DEFAULT, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&cgDataProviderCreateWithCF, purego.RTLD_DEFAULT, "CGDataProviderCreateWithCFData")
		purego.RegisterLibFunc(&cgDataProviderRelease, purego.RTLD_DEFAULT, "CGDataProviderRelease")
		purego.RegisterLibFunc(&cfDataCreate, purego.RTLD_DEFAULT, "CFDataCreate")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
		purego.RegisterLibFunc(&cgImageCreate, purego.RTLD_DEFAULT, "CGImageCreate")
		purego.RegisterLibFunc(&cgImageRelease, purego.RTLD_DEFAULT, "CGImageRelease")
		purego.RegisterLibFunc(&cvPixelBufferLock, purego.RTLD_DEFAULT, "CVPixelBufferLockBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferUnlock, purego.RTLD_DEFAULT, "CVPixelBufferUnlockBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferGetBaseAddress, purego.RTLD_DEFAULT, "CVPixelBufferGetBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferGetWidth, purego.RTLD_DEFAULT, "CVPixelBufferGetWidth")
		purego.RegisterLibFunc(&cvPixelBufferGetHeight, purego.RTLD_DEFAULT, "CVPixelBufferGetHeight")
		purego.RegisterLibFunc(&cvPixelBufferGetBytesPerRow, purego.RTLD_DEFAULT, "CVPixelBufferGetBytesPerRow")
		purego.RegisterLibFunc(&cvPixelBufferGetFormatType, purego.RTLD_DEFAULT, "CVPixelBufferGetPixelFormatType")
		purego.RegisterLibFunc(&cvPixelBufferRelease, purego.RTLD_DEFAULT, "CVPixelBufferRelease")

		msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
		if err != nil {
			visErr = err
			return
		}
		purego.RegisterFunc(&msgSetFloat32, msgSend)
		stret := msgSend
		if runtime.GOARCH == "amd64" {
			stret, err = purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend_stret")
			if err != nil {
				visErr = err
				return
			}
		}
		purego.RegisterFunc(&msgBoundingBox, stret)
	})
	return visErr
}

// Recognize 对 NRGBA 图像执行离线中英混排 OCR 与二维码/条码检测。
func Recognize(img *image.NRGBA) (Result, error) {
	if img == nil || img.Bounds().Empty() {
		return Result{}, errors.New("图像为空")
	}
	if err := loadVision(); err != nil {
		return Result{}, err
	}
	prepared, pad, workW, workH := prepareForVision(img)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	cgImg := nrgbaToCGImage(prepared)
	if cgImg == 0 {
		return Result{}, errors.New("创建 CGImage 失败")
	}
	defer cgImageRelease(cgImg)

	emptyDict := objc.ID(objc.GetClass("NSDictionary")).Send(selDictionary)
	handler := objc.ID(objc.GetClass("VNImageRequestHandler")).Send(selAlloc).Send(selInitCGImage, cgImg, emptyDict)
	if handler == 0 {
		return Result{}, errors.New("创建 VNImageRequestHandler 失败")
	}
	handler.Send(selAutorelease)

	textReq := objc.ID(objc.GetClass("VNRecognizeTextRequest")).Send(selAlloc).Send(selInit)
	if textReq == 0 {
		return Result{}, errors.New("创建 VNRecognizeTextRequest 失败")
	}
	textReq.Send(selAutorelease)
	textReq.Send(selSetRecLevel, uintptr(0)) // Accurate
	textReq.Send(selSetLangCorr, true)
	msgSetFloat32(textReq, selSetMinTextHeight, 0.0)
	langs := [3]objc.ID{nsStr("zh-Hans"), nsStr("zh-Hant"), nsStr("en-US")}
	langArr := objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObjs, uintptr(unsafe.Pointer(&langs[0])), uintptr(len(langs)))
	textReq.Send(selSetRecLangs, langArr)

	barcodeReq := objc.ID(objc.GetClass("VNDetectBarcodesRequest")).Send(selAlloc).Send(selInit)
	if barcodeReq != 0 {
		barcodeReq.Send(selAutorelease)
		qrSym := [1]objc.ID{nsStr("VNBarcodeSymbologyQR")}
		qrArr := objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObjs, uintptr(unsafe.Pointer(&qrSym[0])), uintptr(1))
		barcodeReq.Send(selSetSymbologies, qrArr)
	}

	reqs := [2]objc.ID{textReq, barcodeReq}
	reqCount := 1
	if barcodeReq != 0 {
		reqCount = 2
	}
	reqArr := objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObjs, uintptr(unsafe.Pointer(&reqs[0])), uintptr(reqCount))

	var nsErr objc.ID
	ok := objc.Send[bool](handler, selPerformReqs, reqArr, uintptr(unsafe.Pointer(&nsErr)))
	if !ok {
		if nsErr != 0 {
			if desc := nsErr.Send(selLocalizedDesc); desc != 0 {
				return Result{}, errors.New(cstrings.NSStringToString(desc))
			}
		}
		return Result{}, errors.New("Vision 识别执行失败")
	}

	prepW := float64(prepared.Bounds().Dx())
	prepH := float64(prepared.Bounds().Dy())
	var blocks []textBlock
	var outBoxes []TextBox
	resArr := textReq.Send(selResults)
	if resArr != 0 {
		n := int(objc.Send[uintptr](resArr, selCount))
		blocks = make([]textBlock, 0, n)
		outBoxes = make([]TextBox, 0, n)
		for i := range n {
			obs := resArr.Send(selObjectAtIndex, uintptr(i))
			cands := obs.Send(selTopCandidates, uintptr(1))
			if cands == 0 || int(objc.Send[uintptr](cands, selCount)) == 0 {
				continue
			}
			top := cands.Send(selObjectAtIndex, uintptr(0))
			s := top.Send(selString)
			if s == 0 {
				continue
			}
			txt := cstrings.NSStringToString(s)
			box := msgBoundingBox(obs, selBoundingBox)
			topY := 1.0 - (box.Y + box.H)
			// Vision 的 boundingBox 原点在左下角（y 向上），转换为顶边向下 [0,1]。
			blocks = append(blocks, textBlock{
				text: txt,
				x:    box.X,
				top:  topY,
				h:    box.H,
			})
			if workW > 0 && workH > 0 {
				ox := (box.X*prepW - float64(pad)) / float64(workW)
				oy := (topY*prepH - float64(pad)) / float64(workH)
				ow := (box.W * prepW) / float64(workW)
				oh := (box.H * prepH) / float64(workH)
				outBoxes = append(outBoxes, TextBox{
					Text: txt,
					X:    max(0, min(1, ox)),
					Top:  max(0, min(1, oy)),
					W:    max(0, min(1, ow)),
					H:    max(0, min(1, oh)),
				})
			}
		}
	}

	var barcodes []string
	seenQR := map[string]bool{}
	if barcodeReq != 0 {
		bArr := barcodeReq.Send(selResults)
		if bArr != 0 {
			n := int(objc.Send[uintptr](bArr, selCount))
			for i := range n {
				obs := bArr.Send(selObjectAtIndex, uintptr(i))
				val := obs.Send(selPayloadStringVal)
				if val == 0 {
					continue
				}
				payload := cstrings.NSStringToString(val)
				if payload != "" && !seenQR[payload] {
					seenQR[payload] = true
					barcodes = append(barcodes, payload)
				}
			}
		}
	}

	return Result{
		Text:     layoutBlocks(blocks),
		Barcodes: barcodes,
		Boxes:    outBoxes,
	}, nil
}

// Cutout 调用 macOS 14+ 原生 Vision 前景实例分割（VNGenerateForegroundInstanceMaskRequest），
// 离线识别主体并生成透明背景 PNG（自动裁剪四周多余透明边，保留 4px 呼吸留白）。
func Cutout(img *image.NRGBA) (*image.NRGBA, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, errors.New("图像为空")
	}
	if err := loadVision(); err != nil {
		return nil, err
	}
	reqCls := objc.GetClass("VNGenerateForegroundInstanceMaskRequest")
	if reqCls == 0 {
		return nil, errors.New("当前系统不支持离线主体抠图（需要 macOS 14+）")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	cgImg := nrgbaToCGImage(img)
	if cgImg == 0 {
		return nil, errors.New("创建 CGImage 失败")
	}
	defer cgImageRelease(cgImg)

	emptyDict := objc.ID(objc.GetClass("NSDictionary")).Send(selDictionary)
	handler := objc.ID(objc.GetClass("VNImageRequestHandler")).Send(selAlloc).Send(selInitCGImage, cgImg, emptyDict)
	if handler == 0 {
		return nil, errors.New("创建 VNImageRequestHandler 失败")
	}
	handler.Send(selAutorelease)

	req := objc.ID(reqCls).Send(selAlloc).Send(selInit)
	if req == 0 {
		return nil, errors.New("创建主体分割请求失败")
	}
	req.Send(selAutorelease)

	reqs := [1]objc.ID{req}
	reqArr := objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObjs, uintptr(unsafe.Pointer(&reqs[0])), uintptr(1))
	var nsErr objc.ID
	if !objc.Send[bool](handler, selPerformReqs, reqArr, uintptr(unsafe.Pointer(&nsErr))) {
		if nsErr != 0 {
			if desc := nsErr.Send(selLocalizedDesc); desc != 0 {
				return nil, errors.New(cstrings.NSStringToString(desc))
			}
		}
		return nil, errors.New("主体抠图分析失败")
	}

	resArr := req.Send(selResults)
	if resArr == 0 || int(objc.Send[uintptr](resArr, selCount)) == 0 {
		return nil, errors.New("未检测到明显前景主体")
	}
	obs := resArr.Send(selObjectAtIndex, uintptr(0))
	instances := obs.Send(selAllInstances)
	if instances == 0 || int(objc.Send[uintptr](instances, selCount)) == 0 {
		return nil, errors.New("未检测到明显前景主体")
	}

	var maskErr objc.ID
	pb := uintptr(obs.Send(selGenScaledMask, instances, handler, uintptr(unsafe.Pointer(&maskErr))))
	if pb == 0 {
		if maskErr != 0 {
			if desc := maskErr.Send(selLocalizedDesc); desc != 0 {
				return nil, errors.New(cstrings.NSStringToString(desc))
			}
		}
		return nil, errors.New("生成前景透明度遮罩失败")
	}
	defer cvPixelBufferRelease(pb)

	if cvPixelBufferLock(pb, cvPixelBufferLockReadOnly) != 0 {
		return nil, errors.New("锁定前景遮罩缓冲失败")
	}
	defer cvPixelBufferUnlock(pb, cvPixelBufferLockReadOnly)

	base := cvPixelBufferGetBaseAddress(pb)
	mw := int(cvPixelBufferGetWidth(pb))
	mh := int(cvPixelBufferGetHeight(pb))
	bpr := int(cvPixelBufferGetBytesPerRow(pb))
	pixFmt := cvPixelBufferGetFormatType(pb)
	if base == nil || mw <= 0 || mh <= 0 || bpr <= 0 {
		return nil, errors.New("前景遮罩数据无效")
	}

	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	rawBytes := unsafe.Slice((*byte)(base), mh*bpr)

	opaqueCount := 0
	for y := range sh {
		my := y * mh / sh
		rowOff := my * bpr
		for x := range sw {
			mx := x * mw / sw
			var maskA float32
			switch pixFmt {
			case cvPixelFormatOneComp32F:
				off := rowOff + mx*4
				if off+4 <= len(rawBytes) {
					maskA = *(*float32)(unsafe.Pointer(&rawBytes[off]))
				}
			case cvPixelFormatOneComp8:
				off := rowOff + mx
				if off < len(rawBytes) {
					maskA = float32(rawBytes[off]) / 255.0
				}
			default:
				// 兜底按 32F 读取（Vision 默认输出 OneComponent32Float）
				off := rowOff + mx*4
				if off+4 <= len(rawBytes) {
					maskA = *(*float32)(unsafe.Pointer(&rawBytes[off]))
				}
			}
			if maskA < 0 {
				maskA = 0
			} else if maskA > 1 {
				maskA = 1
			}
			c := img.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			na := uint8(float32(c.A)*maskA + 0.5)
			if na > 16 {
				opaqueCount++
			}
			out.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: na})
		}
	}
	if opaqueCount == 0 {
		return nil, errors.New("未检测到明显前景主体")
	}
	return TrimTransparent(out, 4), nil
}

// prepareForVision 对极小/贴边裁剪区域补充静区留白（并按需 2x 双线性放大），
// 同时将透明像素合成到边缘底色上，显著提升单行小字与贴边二维码的识别率。
func prepareForVision(src *image.NRGBA) (dst *image.NRGBA, pad, workW, workH int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src, 0, w, h
	}
	bg := estimateEdgeBackground(src)
	work := src
	if w < 180 || h < 48 {
		work = upscale2x(src, bg)
		w, h = work.Bounds().Dx(), work.Bounds().Dy()
	}
	pad = 0
	if w < 1000 || h < 600 {
		pad = 24
	}
	if pad == 0 && work != src {
		return work, 0, w, h
	}
	wb := work.Bounds()
	dst = image.NewNRGBA(image.Rect(0, 0, w+pad*2, h+pad*2))
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i] = bg.R
		dst.Pix[i+1] = bg.G
		dst.Pix[i+2] = bg.B
		dst.Pix[i+3] = 255
	}
	for y := range h {
		sRow := work.Pix[work.PixOffset(wb.Min.X, wb.Min.Y+y):]
		dRow := dst.Pix[dst.PixOffset(pad, pad+y):]
		for x := range w {
			si := x * 4
			di := x * 4
			a := uint32(sRow[si+3])
			if a == 255 {
				dRow[di] = sRow[si]
				dRow[di+1] = sRow[si+1]
				dRow[di+2] = sRow[si+2]
			} else if a > 0 {
				inv := 255 - a
				dRow[di] = uint8((uint32(sRow[si])*a + uint32(bg.R)*inv + 127) / 255)
				dRow[di+1] = uint8((uint32(sRow[si+1])*a + uint32(bg.G)*inv + 127) / 255)
				dRow[di+2] = uint8((uint32(sRow[si+2])*a + uint32(bg.B)*inv + 127) / 255)
			}
			dRow[di+3] = 255
		}
	}
	return dst, pad, w, h
}

func estimateEdgeBackground(img *image.NRGBA) color.NRGBA {
	b := img.Bounds()
	var sumR, sumG, sumB, n uint64
	sample := func(x, y int) {
		c := img.NRGBAAt(x, y)
		if c.A < 32 {
			return
		}
		sumR += uint64(c.R)
		sumG += uint64(c.G)
		sumB += uint64(c.B)
		n++
	}
	stepX := max(1, b.Dx()/32)
	stepY := max(1, b.Dy()/32)
	for x := b.Min.X; x < b.Max.X; x += stepX {
		sample(x, b.Min.Y)
		sample(x, b.Max.Y-1)
	}
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		sample(b.Min.X, y)
		sample(b.Max.X-1, y)
	}
	if n == 0 {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.NRGBA{
		R: uint8(sumR / n),
		G: uint8(sumG / n),
		B: uint8(sumB / n),
		A: 255,
	}
}

func upscale2x(src *image.NRGBA, bg color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw*2, sh*2
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	at := func(x, y int) (r, g, bl float64) {
		x = max(0, min(sw-1, x))
		y = max(0, min(sh-1, y))
		c := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
		if c.A == 255 {
			return float64(c.R), float64(c.G), float64(c.B)
		}
		a := float64(c.A) / 255.0
		return float64(c.R)*a + float64(bg.R)*(1-a),
			float64(c.G)*a + float64(bg.G)*(1-a),
			float64(c.B)*a + float64(bg.B)*(1-a)
	}
	for dy := range dh {
		fy := (float64(dy)+0.5)*0.5 - 0.5
		y0 := int(fy)
		if fy < 0 {
			y0 = -1
		}
		wy := max(0, min(1, fy-float64(y0)))
		for dx := range dw {
			fx := (float64(dx)+0.5)*0.5 - 0.5
			x0 := int(fx)
			if fx < 0 {
				x0 = -1
			}
			wx := max(0, min(1, fx-float64(x0)))
			r00, g00, b00 := at(x0, y0)
			r10, g10, b10 := at(x0+1, y0)
			r01, g01, b01 := at(x0, y0+1)
			r11, g11, b11 := at(x0+1, y0+1)
			r := (r00*(1-wx)+r10*wx)*(1-wy) + (r01*(1-wx)+r11*wx)*wy
			g := (g00*(1-wx)+g10*wx)*(1-wy) + (g01*(1-wx)+g11*wx)*wy
			bl := (b00*(1-wx)+b10*wx)*(1-wy) + (b01*(1-wx)+b11*wx)*wy
			di := dst.PixOffset(dx, dy)
			dst.Pix[di] = uint8(max(0, min(255, r+0.5)))
			dst.Pix[di+1] = uint8(max(0, min(255, g+0.5)))
			dst.Pix[di+2] = uint8(max(0, min(255, bl+0.5)))
			dst.Pix[di+3] = 255
		}
	}
	return dst
}

func nrgbaToCGImage(img *image.NRGBA) uintptr {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return 0
	}
	tight := make([]byte, w*h*4)
	for y := range h {
		srcOff := img.PixOffset(b.Min.X, b.Min.Y+y)
		copy(tight[y*w*4:(y+1)*w*4], img.Pix[srcOff:srcOff+w*4])
	}
	cfData := cfDataCreate(0, unsafe.Pointer(&tight[0]), len(tight))
	runtime.KeepAlive(tight)
	if cfData == 0 {
		return 0
	}
	defer cfRelease(cfData)

	provider := cgDataProviderCreateWithCF(cfData)
	if provider == 0 {
		return 0
	}
	defer cgDataProviderRelease(provider)

	cs := cgColorSpaceCreateDeviceRGB()
	if cs == 0 {
		return 0
	}
	defer cgColorSpaceRelease(cs)

	return cgImageCreate(
		uintptr(w),
		uintptr(h),
		8,
		32,
		uintptr(w*4),
		cs,
		cgImageAlphaNoneSkipLast,
		provider,
		0,
		false,
		cgRenderingIntentDef,
	)
}

func nsStr(s string) objc.ID {
	b := unsafe.StringData(s)
	id := objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(
		selInitBytes,
		uintptr(unsafe.Pointer(b)),
		uintptr(len(s)),
		uintptr(nsUTF8StringEncoding),
	)
	runtime.KeepAlive(s)
	if id != 0 {
		id.Send(selAutorelease)
	}
	return id
}
