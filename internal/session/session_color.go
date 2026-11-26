package session

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"snipgo/internal/beautify"
)

// ColorFormat 返回当前取色格式（HEX / RGB / OKLCH / Tailwind）。
func (s *Session) ColorFormat() beautify.ColorFormat { return s.colorFmt }

// SetColorFormat 设置初始/当前取色格式（不触发变更通知）。
func (s *Session) SetColorFormat(f beautify.ColorFormat) {
	if f < beautify.ColorHEX || f > beautify.ColorTailwind {
		f = beautify.ColorHEX
	}
	s.colorFmt = f
}

// SelectColorFormat 切换取色格式并触发 OnColorFormatChanged 回调。
func (s *Session) SelectColorFormat(f beautify.ColorFormat) beautify.ColorFormat {
	s.SetColorFormat(f)
	if s.OnColorFormatChanged != nil {
		s.OnColorFormatChanged(s.colorFmt)
	}
	return s.colorFmt
}

// CycleColorFormat 循环切换取色输出格式，并通知上层持久化。
func (s *Session) CycleColorFormat() beautify.ColorFormat {
	return s.SelectColorFormat(beautify.NextColorFormat(s.colorFmt))
}

// ColorPickerMode 报告当前是否处于独立屏幕取色模式。
func (s *Session) ColorPickerMode() bool { return s.colorPickerMode }

// RemovePickedColor 从多色板中移除指定索引的颜色。
func (s *Session) RemovePickedColor(idx int) bool {
	if idx < 0 || idx >= len(s.pickedColors) {
		return false
	}
	s.pickedColors = append(s.pickedColors[:idx], s.pickedColors[idx+1:]...)
	return true
}

// PopPickedColor 移除多色板中最后采集的颜色。
func (s *Session) PopPickedColor() bool {
	if len(s.pickedColors) == 0 {
		return false
	}
	s.pickedColors = s.pickedColors[:len(s.pickedColors)-1]
	return true
}

// PickedColors 返回本轮取色已采集的多色板颜色副本。
func (s *Session) PickedColors() []color.NRGBA {
	if len(s.pickedColors) == 0 {
		return nil
	}
	out := make([]color.NRGBA, len(s.pickedColors))
	copy(out, s.pickedColors)
	return out
}

// ClearPickedColors 清空暂存的多色板。
func (s *Session) ClearPickedColors() {
	s.pickedColors = nil
}

// SampleColorToPaletteEx 在不退出选区的前提下采样当前光标色值并加入多色板（最多 8 色，RGB 去重）。
// 冻结模式同步采样；实时模式异步采样并在回调中通知刷新与是否实际新增。
func (s *Session) SampleColorToPaletteEx(onResult func(col color.NRGBA, added bool)) bool {
	if !s.hasCursor {
		return false
	}
	appendCol := func(c color.NRGBA) {
		c.A = 255
		for _, existing := range s.pickedColors {
			if existing.R == c.R && existing.G == c.G && existing.B == c.B {
				if onResult != nil {
					onResult(c, false)
				}
				return
			}
		}
		if len(s.pickedColors) >= 8 {
			s.pickedColors = s.pickedColors[1:]
		}
		s.pickedColors = append(s.pickedColors, c)
		if onResult != nil {
			onResult(c, true)
		}
	}
	if !s.live {
		loupe, ok := s.SampleLoupe(1)
		if !ok {
			return false
		}
		appendCol(loupe.Center)
		return true
	}
	if s.CaptureRect == nil {
		return false
	}
	cx, cy := int(math.Floor(s.cursorX)), int(math.Floor(s.cursorY))
	patch := image.Rect(cx-1, cy-1, cx+2, cy+2)
	if union := screensUnion(s.frames); !union.Empty() {
		patch = patch.Intersect(union)
	}
	if patch.Empty() {
		return false
	}
	gen := s.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		img, err := s.CaptureRect(ctx, patch)
		s.OnMain(func() {
			if gen != s.gen || (s.phase != PhaseSelecting && s.phase != PhaseAdjusting) || err != nil || img == nil || img.Bounds().Empty() {
				return
			}
			b := img.Bounds()
			appendCol(img.NRGBAAt(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2))
		})
	}()
	return true
}

// SampleColorToPalette 在不退出选区的前提下采样当前光标色值并加入多色板（最多 8 色）。
// 冻结模式同步采样；实时模式异步采样并在回调中通知刷新。
func (s *Session) SampleColorToPalette(onAdded func(col color.NRGBA)) bool {
	return s.SampleColorToPaletteEx(func(col color.NRGBA, added bool) {
		if added && onAdded != nil {
			onAdded(col)
		}
	})
}

// PinPickedPalette 将当前已采集的多色板渲染为高分屏配色卡，复制到剪贴板并原地贴图，同时结束选区。
func (s *Session) PinPickedPalette() bool {
	if len(s.pickedColors) == 0 {
		return false
	}
	colors := s.PickedColors()
	scale := 2.0
	if s.hasCursor {
		pt := image.Pt(int(math.Floor(s.cursorX)), int(math.Floor(s.cursorY)))
		scale = s.scaleForRect(image.Rect(pt.X, pt.Y, pt.X+1, pt.Y+1))
	}
	card := beautify.RenderPaletteCard(colors, scale)
	if card == nil || card.Bounds().Empty() {
		return false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, card); err != nil {
		return false
	}
	dipW := max(120, int(math.Round(float64(card.Bounds().Dx())/scale)))
	dipH := max(64, int(math.Round(float64(card.Bounds().Dy())/scale)))
	at := image.Pt(120, 120)
	if s.hasCursor {
		at = s.palettePlacement(dipW, dipH)
	}
	s.clearSelection()
	if s.Clip != nil {
		_ = s.Clip.WritePNG(buf.Bytes())
	}
	if s.OnOutput != nil {
		s.OnOutput(buf.Bytes(), card.Bounds().Dx(), card.Bounds().Dy(), scale)
	}
	if s.OnPalettePinned != nil {
		s.OnPalettePinned(colors, card, buf.Bytes(), at, dipW, dipH)
	} else {
		s.UI.ShowPin(card, buf.Bytes(), at, dipW, dipH)
	}
	return true
}

// palettePlacement 以光标为中心放置配色卡，并限制在光标所在显示器的 DIP 范围内
// （左/上保留 20/40 的边距）；负坐标显示器同样适用。
func (s *Session) palettePlacement(dipW, dipH int) image.Point {
	cx, cy := int(math.Floor(s.cursorX)), int(math.Floor(s.cursorY))
	at := image.Pt(cx-dipW/2, cy-dipH/2)
	var b image.Rectangle
	for _, f := range s.frames {
		if fb := f.Bounds.Canon(); image.Pt(cx, cy).In(fb) {
			b = fb
			break
		}
	}
	if b.Empty() {
		return at
	}
	minX, minY := b.Min.X+20, b.Min.Y+40
	maxX, maxY := max(minX, b.Max.X-dipW), max(minY, b.Max.Y-dipH)
	return image.Pt(min(max(at.X, minX), maxX), min(max(at.Y, minY), maxY))
}

// CopyColor 复制光标处像素色值到剪贴板并退出截图。
// rawFormat 为 true（Shift+C）输出去前缀纯色值；否则按当前取色格式（HEX / RGB / OKLCH / Tailwind）输出完整色值。
func (s *Session) CopyColor(rawFormat bool) bool {
	if s.Clip == nil || !s.hasCursor {
		return false
	}
	colorFmt := s.colorFmt
	formatPx := func(px color.NRGBA) string {
		if rawFormat {
			return beautify.FormatColorRaw(px, colorFmt)
		}
		return beautify.FormatColor(px, colorFmt)
	}
	if !s.live {
		loupe, ok := s.SampleLoupe(1)
		if !ok {
			return false
		}
		txt := formatPx(loupe.Center)
		s.Clip.WriteText(txt)
		s.status = ""
		s.UI.SetStatus("")
		s.clearSelection()
		if s.OnColorCopied != nil {
			s.OnColorCopied(txt, loupe.Center)
		}
		return true
	}
	if s.CaptureRect == nil {
		return false
	}
	cx, cy := int(math.Floor(s.cursorX)), int(math.Floor(s.cursorY))
	patch := image.Rect(cx-1, cy-1, cx+2, cy+2)
	if union := screensUnion(s.frames); !union.Empty() {
		patch = patch.Intersect(union)
	}
	if patch.Empty() {
		return false
	}
	s.clearSelection()
	s.gen++
	gen := s.gen
	s.phase = PhaseCapturing
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		img, err := s.CaptureRect(ctx, patch)
		s.OnMain(func() {
			if gen != s.gen || s.phase != PhaseCapturing {
				return
			}
			s.phase = PhaseIdle
			if err != nil {
				s.fail(err.Error())
				return
			}
			if img == nil || img.Bounds().Empty() {
				return
			}
			b := img.Bounds()
			px := img.NRGBAAt(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
			txt := formatPx(px)
			s.Clip.WriteText(txt)
			s.status = ""
			s.UI.SetStatus("")
			if s.OnColorCopied != nil {
				s.OnColorCopied(txt, px)
			}
		})
	}()
	return true
}
