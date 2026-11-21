package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"snipgo/internal/pinops"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// togglePinCompare 开关 Before/After 拉帘对比与像素 Diff 热力图模式（自动配对最近/重叠的另一张贴图）。
func (a *app) togglePinCompare(p *pinWin) {
	if p == nil || p.collapsed {
		return
	}
	if p.compareMode {
		p.exitCompareMode()
		return
	}
	peer := a.findComparePeer(p)
	if peer == nil || peer.img == nil || p.img == nil {
		a.sessionStatus("需要至少 2 张贴图才能开启拉帘/像素对比")
		return
	}
	if p.editMode {
		p.finishPinEdit()
	}
	p.rulerMode = false
	p.compareMode = true
	p.compareDiff = false
	p.comparePeer = peer.img
	p.comparePeerBmp = ui.NewBitmap(peer.img)
	p.compareDiffRes = pinops.DiffResult{}
	p.compareDiffBmp = nil
	p.compareGen++
	p.startPixelDiff(peer.img, p.img)
	if p.compareSplit <= 0.02 || p.compareSplit >= 0.98 {
		p.compareSplit = 0.5
	}
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Focus()
		p.win.Invalidate()
	}
}

// startPixelDiff 在后台计算整图像素 Diff，完成后回到主线程应用；
// 对比已退出/重进、或配对图与当前图已变化时丢弃结果。
func (p *pinWin) startPixelDiff(peerImg, selfImg *image.NRGBA) {
	gen := p.compareGen
	p.comparePending = true
	runBackground(func() {
		res := pinops.RenderPixelDiff(peerImg, selfImg, 12)
		runOnMain(func() {
			if !p.compareMode || p.compareGen != gen || p.comparePeer != peerImg || p.img != selfImg {
				return
			}
			p.comparePending = false
			p.compareDiffRes = res
			if res.Image != nil {
				p.compareDiffBmp = ui.NewBitmap(res.Image)
			}
			if p.win != nil && !p.win.IsDestroyed() {
				p.win.Invalidate()
			}
		})
	})
}

func (p *pinWin) exitCompareMode() {
	p.compareGen++
	p.comparePending = false
	p.compareDiffRes = pinops.DiffResult{}
	p.compareMode = false
	p.compareDiff = false
	p.compareDrag = false
	p.comparePeer = nil
	p.comparePeerBmp = nil
	p.compareDiffBmp = nil
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

func (a *app) findComparePeer(self *pinWin) *pinWin {
	if self == nil {
		return nil
	}
	var selfR image.Rectangle
	if self.win != nil && !self.win.IsDestroyed() {
		b := self.win.Bounds()
		selfR = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	}
	var best *pinWin
	bestScore := math.MaxFloat64
	for _, q := range a.pins {
		if q == self || q.collapsed || q.img == nil {
			continue
		}
		score := 0.0
		if q.win != nil && !q.win.IsDestroyed() && !selfR.Empty() {
			qb := q.win.Bounds()
			qr := image.Rect(qb.X, qb.Y, qb.X+qb.Width, qb.Y+qb.Height)
			inter := selfR.Intersect(qr)
			if !inter.Empty() {
				// 有重叠的贴图最优先配对（重叠面积越大越优先）。
				score = -float64(inter.Dx() * inter.Dy())
			} else {
				dx := float64((selfR.Min.X+selfR.Max.X)/2 - (qr.Min.X+qr.Max.X)/2)
				dy := float64((selfR.Min.Y+selfR.Max.Y)/2 - (qr.Min.Y+qr.Max.Y)/2)
				score = math.Hypot(dx, dy)
			}
		}
		if score < bestScore {
			bestScore = score
			best = q
		}
	}
	return best
}

func (a *app) paintPinCompareLayer(c *ui.Context, p *pinWin, w, h float32) {
	if p.compareDiff && p.compareDiffBmp != nil {
		ui.Image(c, p.compareDiffBmp).Size(w, h)
	} else {
		// 右侧底层展示当前贴图 B，左侧按 splitX 裁剪覆盖展示对比贴图 A。
		if p.bmp != nil {
			ui.Image(c, p.bmp).Size(w, h)
		}
		splitX := max(float32(2), min(w-2, float32(p.compareSplit)*w))
		if p.comparePeerBmp != nil {
			ui.Box(c).Absolute().Left(0).Top(0).Size(splitX, h).Clip().IgnoreInput(true).Children(func() {
				ui.Image(c, p.comparePeerBmp).Size(w, h)
			})
		}
		// 垂直拉帘分割线与中央抓手圆钮
		ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).ZIndex(4).IgnoreInput(true).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				sx := r.X + splitX
				pt.Line(sx, r.Y, sx, r.Y+r.H, 2, ui.RGBA(255, 255, 255, 0.95))
				cy := r.Y + r.H*0.5
				const kr = float32(13)
				pt.Shadow(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, kr, 0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.45))
				pt.Fill(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, ui.RGBA(22, 24, 30, 0.92), kr)
				pt.Stroke(ui.Rect{X: sx - kr, Y: cy - kr, W: kr * 2, H: kr * 2}, ui.RGB(255, 255, 255), kr, 1.5)
				sp := ui.Span{Text: "↔", Size: 12, Weight: 700, Color: ui.RGB(255, 255, 255)}
				tw, th := pt.MeasureText(0, sp)
				pt.RichText(sx-tw*0.5, cy-th*0.5, 0, sp)
			})
		// 左上 / 右上极简角标
		pal := uiPalette(c)
		liquidGlassCapsule(
			c,
			ui.Row(c).Absolute().Left(8).Top(8).Height(20).Padding(0, 8).AlignItems(ui.Center).ZIndex(5).
				IgnoreInput(true),
			10,
			false,
		).Children(func() {
			ui.Text(c, "对比 A").FontSize(10.5).FontWeight(500).NoWrap().TextColor(pal.Text)
		})
		liquidGlassCapsule(
			c,
			ui.Row(c).Absolute().Right(8).Top(8).Height(20).Padding(0, 8).AlignItems(ui.Center).ZIndex(5).
				IgnoreInput(true),
			10,
			false,
		).Children(func() {
			ui.Text(c, "当前 B").FontSize(10.5).FontWeight(500).NoWrap().TextColor(pal.ActiveText)
		})
	}

	// 底部对比控制条
	bar := liquidGlassCapsule(
		c,
		ui.Row(c).Absolute().
			Right(8).Bottom(8).
			Height(34).
			Padding(0, 6).
			Gap(4).
			AlignItems(ui.Center).
			ZIndex(8),
		17,
		false,
	)
	_ = bar.Clicked()
	bar.Children(func() {
		diffLabel := fmt.Sprintf("热力图 · %.2f%%", p.compareDiffRes.DiffPercent)
		if p.comparePending {
			diffLabel = "热力图 · 计算中…"
		}
		cmpSel := 0
		if p.compareDiff {
			cmpSel = 1
		}
		if picked := compactSegmented(c, "pin-cmp", cmpSel, []string{"拉帘", diffLabel}); picked >= 0 {
			p.compareDiff = picked == 1
		}
		if miniPillBtn(c, iconCopy, "复制", true) {
			a.copyCompareComposite(p)
		}
		if miniPillBtn(c, iconClose, "退出", false) {
			p.exitCompareMode()
		}
	})
}

// copyCompareComposite 复制当前对比结果；合成与 PNG 编码在后台完成，剪贴板与历史在主线程写入。
func (a *app) copyCompareComposite(p *pinWin) {
	if p == nil || !p.compareMode || p.img == nil {
		return
	}
	diff := p.compareDiff
	if diff && p.comparePending {
		a.sessionStatus("热力图仍在计算，请稍后再复制")
		return
	}
	diffImg := p.compareDiffRes.Image
	peer, cur, split := p.comparePeer, p.img, p.compareSplit
	scale := max(1.0, float64(cur.Bounds().Dx())/float64(max(1, p.origW)))
	runBackground(func() {
		out := diffImg
		if !diff {
			out = pinops.RenderSplitCompare(peer, cur, split, scale)
		}
		if out == nil || out.Bounds().Empty() {
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, out); err != nil {
			return
		}
		data := buf.Bytes()
		w, h := out.Bounds().Dx(), out.Bounds().Dy()
		runOnMain(func() {
			_ = mygo.Clipboard.WriteImage(data)
			a.recordHistory(data, w, h, 0)
			a.sessionStatus("已复制贴图对比结果")
		})
	})
}
