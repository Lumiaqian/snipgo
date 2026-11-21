package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"snipgo/internal/beautify"
	"snipgo/internal/pinops"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
)

// mergeAllPins 将当前桌面所有未折叠贴图按空间位置横向或纵向一键拼接为一张图，复制并新建贴图展示。
func (a *app) mergeAllPins(horizontal bool) {
	type entry struct {
		pin *pinWin
		x   int
		y   int
	}
	var list []entry
	for _, p := range a.pins {
		if p.collapsed || p.img == nil {
			continue
		}
		var x, y int
		if p.win != nil && !p.win.IsDestroyed() {
			b := p.win.Bounds()
			x, y = b.X, b.Y
		}
		list = append(list, entry{pin: p, x: x, y: y})
	}
	if len(list) < 2 {
		return
	}
	slices.SortFunc(list, func(i, j entry) int {
		if horizontal {
			if c := cmp.Compare(i.x, j.x); c != 0 {
				return c
			}
			return cmp.Compare(i.y, j.y)
		}
		if c := cmp.Compare(i.y, j.y); c != 0 {
			return c
		}
		return cmp.Compare(i.x, j.x)
	})
	imgs := make([]*image.NRGBA, len(list))
	for i, e := range list {
		imgs[i] = e.pin.img
	}
	if !mergePinsBusy.CompareAndSwap(false, true) {
		return // 上一次拼接仍在后台进行，忽略重复触发
	}
	a.sessionStatus("正在拼接贴图…")
	// 拼接与 PNG 编码在后台完成，结果回到主线程后再写剪贴板、历史并新建贴图。
	runBackground(func() {
		merged := pinops.MergeImages(imgs, horizontal, 16, color.NRGBA{R: 22, G: 24, B: 30, A: 255})
		var data []byte
		if merged != nil && !merged.Bounds().Empty() {
			var buf bytes.Buffer
			if png.Encode(&buf, merged) == nil {
				data = buf.Bytes()
			}
		}
		runOnMain(func() {
			defer mergePinsBusy.Store(false)
			if len(data) == 0 {
				return
			}
			_ = mygo.Clipboard.WriteImage(data)
			a.recordHistory(data, merged.Bounds().Dx(), merged.Bounds().Dy(), 0)
			dipW := max(120, min(1100, merged.Bounds().Dx()/2))
			dipH := max(80, min(800, merged.Bounds().Dy()/2))
			a.ShowPin(merged, data, pinPlacementNearCursorOnScreen(dipW, dipH), dipW, dipH)
			a.sessionStatus(fmt.Sprintf("已拼接 %d 张贴图并复制到剪贴板", len(imgs)))
		})
	})
}

// mergePinsBusy 标记一键拼接是否正在后台进行。
var mergePinsBusy atomic.Bool

// PasteClipboardPin 把剪贴板里的图像、图片文件、代码/文字或 #RRGGBB 色号直接转为悬浮贴图。
func (a *app) PasteClipboardPin() {
	cursor := mygo.Screen.CursorScreenPoint()
	scale := 2.0
	for _, d := range mygo.Screen.Displays() {
		db := image.Rect(d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.Width, d.Bounds.Y+d.Bounds.Height)
		if image.Pt(cursor.X, cursor.Y).In(db) && d.ScaleFactor > 0 {
			scale = d.ScaleFactor
			break
		}
	}

	nrgba, err := readClipboardContentAsNRGBA(scale)
	if err != nil {
		log.Printf("PasteClipboardPin: %v", err)
		a.sessionStatus(err.Error())
		go a.showErrorAlert("剪贴板贴图失败", err.Error())
		return
	}
	b := nrgba.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, nrgba); err != nil {
		log.Printf("PasteClipboardPin encode png: %v", err)
		return
	}
	dipW := max(16, int(math.Round(float64(b.Dx())/scale)))
	dipH := max(16, int(math.Round(float64(b.Dy())/scale)))
	at := image.Pt(cursor.X-dipW/2, cursor.Y-dipH/2)
	a.ShowPin(nrgba, pngBuf.Bytes(), at, dipW, dipH)
}

func readClipboardContentAsNRGBA(scale float64) (*image.NRGBA, error) {
	// 1. 优先读取 Finder / 聊天软件复制的图片文件（避免读到 Finder 的通用文件图标 TIFF）。
	files, _ := mygo.Clipboard.ReadFiles()
	for _, p := range files {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif":
			if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
				img, decErr := decodeImageLimited(data)
				if decErr == nil {
					return toNRGBA(img), nil
				}
				if errors.Is(decErr, errImageTooLarge) {
					return nil, decErr
				}
			}
		}
	}
	// 2. 剪贴板位图图像。
	if raw := mygo.Clipboard.ReadImage(); len(raw) > 0 {
		decoded, err := decodeImageLimited(raw)
		if err != nil {
			if errors.Is(err, errImageTooLarge) {
				return nil, err
			}
			return nil, fmt.Errorf("无法解析剪贴板图像: %v", err)
		}
		return toNRGBA(decoded), nil
	}
	// 3. 剪贴板文本 / 代码片段 / #HEX 色号直接渲染为高分屏悬浮便签或色卡！
	if txt := strings.TrimSpace(mygo.Clipboard.ReadText()); txt != "" {
		if card := beautify.RenderTextPin(txt, scale); card != nil {
			return card, nil
		}
	}
	return nil, fmt.Errorf("剪贴板为空（支持图片、图片文件、文字、代码或 #HEX 色号贴图）")
}

var errImageTooLarge = errors.New("图片尺寸超限")

const (
	maxImageSide   = 32768
	maxImagePixels = 120_000_000
)

type imageTooLargeError struct{ w, h int }

func (e *imageTooLargeError) Error() string {
	return fmt.Sprintf("图片尺寸过大（%d×%d），已拒绝贴图", e.w, e.h)
}

func (e *imageTooLargeError) Is(target error) bool { return target == errImageTooLarge }

// decodeImageLimited 先读头部校验尺寸再解码，防止超大图（解压炸弹）耗尽内存。
func decodeImageLimited(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageSide || cfg.Height > maxImageSide ||
		cfg.Width*cfg.Height > maxImagePixels {
		return nil, &imageTooLargeError{cfg.Width, cfg.Height}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}
