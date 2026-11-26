package main

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"snipgo/internal/vision"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// cutoutEncodeFailedStatus 为抠图结果 PNG 编码失败时的托盘状态文案。
const cutoutEncodeFailedStatus = "抠图失败: 编码 PNG 出错"

// cutoutEncode 把抠图结果编码为 PNG；做成变量以便测试注入失败。
var cutoutEncode = func(cut *image.NRGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, cut); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// cutoutSelection 对选区截图执行离线 Vision 前景实例分割，复制透明 PNG 并贴为无边框异形贴纸。
func (a *app) cutoutSelection(img *image.NRGBA, r image.Rectangle) {
	if img == nil || img.Bounds().Empty() {
		return
	}
	scale := 2.0
	if r.Dx() > 0 {
		scale = max(1.0, float64(img.Bounds().Dx())/float64(r.Dx()))
	}
	a.sessionStatus("正在离线 AI 抠取主体…")
	a.showActionToast("正在离线 AI 抠图…")
	go func() {
		cut, err := vision.Cutout(img)
		if err != nil {
			mygo.RunOnMain(func() {
				a.sessionStatus("抠图失败: " + err.Error())
				a.showActionToast("抠图失败")
				go a.showErrorAlert("主体抠图未检测到清晰主体", err.Error())
			})
			return
		}
		a.finishCutoutSelection(cut, r, scale)
	}()
}

// finishCutoutSelection 在后台线程编码抠图结果；编码失败时在主线程上给出失败状态，
// 否则复制到剪贴板、写入历史并贴为透明贴纸。
func (a *app) finishCutoutSelection(cut *image.NRGBA, r image.Rectangle, scale float64) {
	data, err := cutoutEncode(cut)
	if err != nil {
		runOnMain(func() {
			a.sessionStatus(cutoutEncodeFailedStatus)
			a.showActionToast("抠图失败")
		})
		return
	}
	dipW := max(24, int(math.Round(float64(cut.Bounds().Dx())/scale)))
	dipH := max(24, int(math.Round(float64(cut.Bounds().Dy())/scale)))
	at := image.Pt(r.Min.X+(r.Dx()-dipW)/2, r.Min.Y+(r.Dy()-dipH)/2)
	runOnMain(func() {
		_ = mygo.Clipboard.WriteImage(data)
		a.recordHistory(data, cut.Bounds().Dx(), cut.Bounds().Dy(), scale)
		a.sessionStatus("主体抠图已复制并贴为透明贴纸")
		a.showActionToast("已抠出透明主体并复制")
		a.showPinWithState(cut, data, at, dipW, dipH, 1.0, 100, true)
	})
}

// cutoutPinInPlace 原地将悬浮贴图主体从背景中抠出，变为无边框透明异形贴纸并同步写入剪贴板。
func (a *app) cutoutPinInPlace(p *pinWin) {
	if p == nil || p.img == nil || p.cutting {
		return
	}
	p.cutting = true
	src := p.img
	scale := max(1.0, float64(src.Bounds().Dx())/float64(max(1, p.origW)))
	a.sessionStatus("正在抠取贴图主体…")
	p.setPinNotice("正在抠图…")
	go func() {
		cut, err := vision.Cutout(src)
		if err != nil {
			mygo.RunOnMain(func() {
				p.cutting = false
				a.sessionStatus("抠图失败: " + err.Error())
				p.setPinNotice("抠图失败")
				go a.showErrorAlert("主体抠图失败", err.Error())
			})
			return
		}
		data, err := cutoutEncode(cut)
		if err != nil {
			runOnMain(func() {
				p.cutting = false
				a.sessionStatus(cutoutEncodeFailedStatus)
				p.setPinNotice("抠图失败")
			})
			return
		}
		mygo.RunOnMain(func() {
			p.cutting = false
			if p.win == nil || p.win.IsDestroyed() {
				return
			}
			newOrigW := max(24, int(math.Round(float64(cut.Bounds().Dx())/scale)))
			newOrigH := max(24, int(math.Round(float64(cut.Bounds().Dy())/scale)))
			newCurW := max(24, int(math.Round(float64(newOrigW)*p.zoom)))
			newCurH := max(24, int(math.Round(float64(newOrigH)*p.zoom)))
			b := p.win.Bounds()
			cx := b.X + b.Width/2
			cy := b.Y + b.Height/2
			bmp := ui.NewBitmap(cut)
			p.baseImg = cut
			p.baseBmp = bmp
			p.img = cut
			p.bmp = bmp
			p.png = data
			p.annot.Reset()
			p.cutout = true
			p.origW = newOrigW
			p.origH = newOrigH
			p.curW = newCurW
			p.curH = newCurH
			p.win.SetBounds(mygo.Rectangle{X: cx - newCurW/2, Y: cy - newCurH/2, Width: newCurW, Height: newCurH})
			p.win.Invalidate()
			p.refreshShadowSoon()
			_ = mygo.Clipboard.WriteImage(data)
			a.recordHistory(data, cut.Bounds().Dx(), cut.Bounds().Dy(), scale)
			a.sessionStatus("贴图已抠出透明主体并复制")
			p.setPinNotice("已抠出并复制")
		})
	}()
}
