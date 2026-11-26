package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"math"
	"time"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/crop"
)

// Copy 把当前选区复制到剪贴板并结束本轮截图。
func (s *Session) Copy() { s.confirm(ActionCopy) }

// Pin 把当前选区原地悬浮贴图并结束本轮截图。
func (s *Session) Pin() { s.confirm(ActionPin) }

// Save 裁剪当前选区并执行默认保存流程。
func (s *Session) Save() { s.confirm(ActionSave) }

// SaveAs 裁剪当前选区并强制弹出“另存为”对话框。
func (s *Session) SaveAs() { s.confirm(ActionSaveAs) }

// OCR 识别当前选区文字或二维码并复制到剪贴板。
func (s *Session) OCR() { s.confirm(ActionOCR) }

// Cutout 对当前选区执行离线 AI 主体抠图并作为无底异形贴纸悬浮展示。
func (s *Session) Cutout() { s.confirm(ActionCutout) }

// Template 采集当前选区（保留已绘矢量标注）并进入所见即所得「套模板工作台」。
func (s *Session) Template() { s.confirm(ActionTemplate) }

func (s *Session) confirm(act ConfirmAction) {
	r, winID, ok := s.targetSelection()
	if !ok {
		return
	}
	if act == ActionOCR {
		s.lastOCRRect = r
	}
	items := s.Annotations().Items()
	useWinOnly := s.windowOnly && winID != 0 && s.CaptureWindow != nil
	useBeautify := s.beautify && act != ActionOCR && act != ActionCutout && act != ActionTemplate
	useChrome := s.beautifyChrome
	if act == ActionOCR || act == ActionCutout {
		items = nil
	}
	preset := s.beautifyPreset
	scale := s.scaleForRect(r)

	if !s.live && !useWinOnly {
		img, err := crop.Crop(s.frames, r)
		if err != nil {
			s.fail(err.Error())
			return
		}
		if act == ActionOCR || act == ActionCutout || act == ActionTemplate {
			if len(items) > 0 {
				annotate.Render(img, r, items)
			}
			s.clearSelection()
			s.applyOutput(act, r, img, nil)
			return
		}
		// 裁剪依赖冻结帧，必须同步完成；渲染标注/美化与 PNG 编码耗时可达数百毫秒，放到后台。
		s.clearSelection()
		s.gen++
		gen := s.gen
		s.pending = nil
		s.phase = PhaseCapturing
		bg := s.Background
		if bg == nil {
			if s.OnMain == nil {
				// 未配置主线程调度（纯逻辑测试）时保持同步执行。
				bg = func(fn func()) { fn() }
			} else {
				bg = func(fn func()) { go fn() }
			}
		}
		bg(func() {
			out, outR := postProcessImage(img, r, scale, items, useBeautify, preset, useChrome)
			b, err := s.encodePNG(out)
			deliver := func() { s.DeliverEncoded(gen, act, outR, out, b, err) }
			if s.OnMain == nil {
				deliver()
				return
			}
			s.OnMain(deliver)
		})
		return
	}
	if s.CaptureRect == nil && !useWinOnly {
		s.fail("未配置局部屏幕采集")
		return
	}
	// 实时模式或独立单窗模式：先立刻关掉透明选区蒙层，再后台采集像素。
	// 采集失败时要能找回选区与标注，所以先在清空前存好快照。
	// 选区取 confirm 解析出的 r（悬停窗口直接确认时 s.sel 为空），窗口 ID 一并带上。
	stash := &abortedSelection{
		act:            act,
		sel:            r,
		lockedWinRect:  r,
		lockedWinID:    winID,
		windowOnly:     s.windowOnly,
		beautify:       s.beautify,
		beautifyPreset: s.beautifyPreset,
		beautifyChrome: s.beautifyChrome,
		colorFmt:       s.colorFmt,
		preferFront:    s.preferFront,
		autoOCR:        s.autoOCR,
		items:          s.Annotations().Items(),
	}
	// 标注依赖预览快照（打码/擦除/放大镜）时，用同一份像素渲染，避免重新采集后内容已变导致漏打码。
	var preview *image.NRGBA
	if !useWinOnly && s.PreviewFrame != nil && itemsNeedPreview(items) {
		if p, ok := s.PreviewFrame(r); ok && p != nil && !p.Bounds().Empty() {
			if w, h := crop.PixelSize(s.frames, r); w > 0 && h > 0 && p.Bounds().Dx() == w && p.Bounds().Dy() == h {
				preview = cloneNRGBA(p)
			}
		}
	}
	s.clearSelection()
	s.gen++
	gen := s.gen
	stash.gen = gen
	s.pending = stash
	s.phase = PhaseCapturing
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var img *image.NRGBA
		var err error
		if useWinOnly {
			img, err = s.CaptureWindow(ctx, winID, scale)
			if err != nil && !errors.Is(err, context.DeadlineExceeded) && s.CaptureRect != nil {
				// 窗口刚刚关闭或重绘时自动回退矩形采集。
				img, err = s.CaptureRect(ctx, r)
			}
		} else if preview != nil {
			img = preview
		} else {
			img, err = s.CaptureRect(ctx, r)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("截图超时，请重试")
		}
		var outR = r
		if err == nil && img != nil && !img.Bounds().Empty() && act != ActionOCR && act != ActionCutout {
			img, outR = postProcessImage(img, r, scale, items, useBeautify, preset, useChrome)
		}
		// PNG 编码放在后台，避免在主线程上阻塞数百毫秒。
		var encoded []byte
		if err == nil && img != nil && !img.Bounds().Empty() && needsPNG(act) {
			encoded, err = s.encodePNG(img)
		}
		s.OnMain(func() { s.DeliverEncoded(gen, act, outR, img, encoded, err) })
	}()
}

// itemsNeedPreview 判断标注是否依赖底图像素（打码、擦除、放大镜）。
func itemsNeedPreview(items []annotate.Item) bool {
	for _, it := range items {
		switch it.Tool {
		case annotate.ToolMosaic, annotate.ToolErase, annotate.ToolLoupe:
			return true
		}
	}
	return false
}

// cloneNRGBA 深拷贝图像，保证渲染不污染预览快照。
func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+b.Dx()*4], src.Pix[(y+b.Min.Y-src.Rect.Min.Y)*src.Stride+(b.Min.X-src.Rect.Min.X)*4:])
	}
	return dst
}

func postProcessImage(img *image.NRGBA, r image.Rectangle, scale float64, items []annotate.Item, useBeautify bool, preset beautify.Preset, useChrome bool) (*image.NRGBA, image.Rectangle) {
	if len(items) > 0 {
		annotate.Render(img, r, items)
	}
	if useBeautify {
		img = beautify.RenderCardWithOptions(img, scale, beautify.CardOptions{
			Preset:       preset,
			WindowChrome: useChrome,
		})
		if scale <= 0 {
			scale = 2.0
		}
		dipW := max(16, int(math.Round(float64(img.Bounds().Dx())/scale)))
		dipH := max(16, int(math.Round(float64(img.Bounds().Dy())/scale)))
		cx := (r.Min.X + r.Max.X) / 2
		cy := (r.Min.Y + r.Max.Y) / 2
		r = image.Rect(cx-dipW/2, cy-dipH/2, cx-dipW/2+dipW, cy-dipH/2+dipH)
	}
	return img, r
}

// scaleForRect 返回与 r 相交的显示器中的最大缩放，与 crop.Crop/Stitch 的输出像素密度保持一致。
func (s *Session) scaleForRect(r image.Rectangle) float64 {
	best := 0.0
	for _, f := range s.frames {
		if !r.Intersect(f.Bounds.Canon()).Empty() && f.Scale > best {
			best = f.Scale
		}
	}
	if best <= 0 {
		return 2.0
	}
	return best
}

// needsPNG 判断动作是否需要 PNG 字节（识字/抠图/套模板直接使用像素）。
func needsPNG(act ConfirmAction) bool {
	return act != ActionOCR && act != ActionCutout && act != ActionTemplate
}

// encodePNG 按会话的 Encode 钩子编码，默认 png.DefaultCompression。
func (s *Session) encodePNG(img *image.NRGBA) ([]byte, error) {
	if s.Encode != nil {
		return s.Encode(img)
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DeliverRect 兼容入口：调用方线程内编码后交付。生产路径已在后台编码并直接调用 DeliverEncoded。
func (s *Session) DeliverRect(gen int, act ConfirmAction, r image.Rectangle, img *image.NRGBA, err error) {
	if gen != s.gen || s.phase != PhaseCapturing {
		return
	}
	var encoded []byte
	if err == nil && img != nil && !img.Bounds().Empty() && needsPNG(act) {
		encoded, err = s.encodePNG(img)
	}
	s.DeliverEncoded(gen, act, r, img, encoded, err)
}

// DeliverEncoded 在主线程交付已编码的截图结果：复制/贴图/保存/识字。
func (s *Session) DeliverEncoded(gen int, act ConfirmAction, r image.Rectangle, img *image.NRGBA, pngBytes []byte, err error) {
	if gen != s.gen || s.phase != PhaseCapturing {
		return
	}
	s.phase = PhaseIdle
	pending := s.pending
	s.pending = nil
	if err != nil || img == nil || img.Bounds().Empty() {
		// 采集失败：保留选区与标注供用户恢复，再向 UI 报错。
		if pending != nil && pending.gen == gen {
			s.aborted = pending
		}
		if err != nil {
			s.fail(err.Error())
		} else {
			s.fail("没有截到图像")
		}
		return
	}
	s.aborted = nil
	if act == ActionOCR {
		s.status = ""
		s.UI.SetStatus("")
		s.UI.RecognizeImage(img)
		return
	}
	if act == ActionCutout {
		s.status = ""
		s.UI.SetStatus("")
		if s.OnCutout != nil {
			s.OnCutout(img, r)
		}
		return
	}
	if act == ActionTemplate {
		s.status = ""
		s.UI.SetStatus("")
		if s.OnTemplate != nil {
			s.OnTemplate(img, r)
		}
		return
	}
	s.applyOutput(act, r, img, pngBytes)
}

// CropFrozenSelection 在冻结模式下裁剪当前选区原图（不关闭选区，供选区内一键自动脱敏使用）。
func (s *Session) CropFrozenSelection() (*image.NRGBA, image.Rectangle, bool) {
	if s.live || s.phase != PhaseAdjusting || s.sel.Empty() {
		return nil, image.Rectangle{}, false
	}
	img, err := crop.Crop(s.frames, s.sel)
	if err != nil || img == nil || img.Bounds().Empty() {
		return nil, image.Rectangle{}, false
	}
	return img, s.sel, true
}

func (s *Session) applyOutput(act ConfirmAction, r image.Rectangle, img *image.NRGBA, pngBytes []byte) {
	if len(pngBytes) > 0 && img != nil && s.OnOutput != nil && act != ActionOCR {
		s.OnOutput(pngBytes, img.Bounds().Dx(), img.Bounds().Dy(), s.scaleForRect(r))
	}
	switch act {
	case ActionCopy:
		if err := s.Clip.WritePNG(pngBytes); err != nil {
			s.fail("复制到剪贴板失败")
			return
		}
		s.status = ""
		s.UI.SetStatus("")
	case ActionPin:
		s.status = ""
		s.UI.SetStatus("")
		s.UI.ShowPin(img, pngBytes, r.Min, r.Dx(), r.Dy())
	case ActionSave:
		s.status = ""
		s.UI.SetStatus("")
		s.UI.SavePNG(pngBytes)
	case ActionSaveAs:
		s.status = ""
		s.UI.SetStatus("")
		if s.OnSaveAs != nil {
			s.OnSaveAs(pngBytes)
		} else {
			s.UI.SavePNG(pngBytes)
		}
	case ActionOCR:
		s.status = ""
		s.UI.SetStatus("")
		s.UI.RecognizeImage(img)
	case ActionCutout:
		s.status = ""
		s.UI.SetStatus("")
		if s.OnCutout != nil {
			s.OnCutout(img, r)
		}
	case ActionTemplate:
		s.status = ""
		s.UI.SetStatus("")
		if s.OnTemplate != nil {
			s.OnTemplate(img, r)
		}
	}
}

func (s *Session) targetSelection() (image.Rectangle, uint32, bool) {
	switch s.phase {
	case PhaseAdjusting:
		if !s.sel.Empty() {
			var wid uint32
			if s.sel == s.lockedWinRect {
				wid = s.lockedWinID
			}
			return s.sel, wid, true
		}
	case PhaseSelecting:
		if !s.hoverWin.Empty() {
			return s.hoverWin, s.winIDByRect[s.hoverWin], true
		}
	}
	return image.Rectangle{}, 0, false
}
