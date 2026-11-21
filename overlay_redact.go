package main

import (
	"context"
	"fmt"
	"image"
	"snipgo/internal/annotate"
	"snipgo/internal/session"
	"snipgo/internal/vision"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// previewSnapReady / previewCaptureRect 是预览快照的注入点：生产环境为 mygo 就绪判断与 ScreenCaptureKit 采集，测试可替换。
var (
	previewSnapReady   = func() bool { return mygo.App.IsReady() }
	previewCaptureRect = func(a *app, ctx context.Context, sel image.Rectangle) (*image.NRGBA, error) {
		return a.captureRect(ctx, sel)
	}
)

// cancelPreviewSnap 取消仍在途的预览快照采集。
func (a *app) cancelPreviewSnap() {
	if a.previewSnapCancel != nil {
		a.previewSnapCancel()
		a.previewSnapCancel = nil
	}
}

func (a *app) ensurePreviewSnapshot(sel image.Rectangle) {
	sel = sel.Canon()
	if sel.Empty() || a.session == nil || !a.session.IsLive() || !previewSnapReady() {
		return
	}
	// 拖动/缩放选区期间每帧选区都在变，不启动采集；松手后的下一帧绘制会再调用到这里。
	if a.session.Dragging() {
		return
	}
	if a.previewSnapRect == sel || a.previewSnapPending == sel {
		return
	}
	// 选区已变：先取消上一次仍在途的采集，避免并发堆积多次整屏采集。
	a.cancelPreviewSnap()
	a.previewSnapGen++
	gen := a.previewSnapGen
	a.previewSnapPending = sel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	a.previewSnapCancel = cancel
	go func() {
		defer cancel()
		img, err := previewCaptureRect(a, ctx, sel)
		runOnMain(func() {
			if a.previewSnapGen != gen {
				return
			}
			a.previewSnapCancel = nil
			a.previewSnapPending = image.Rectangle{}
			if a.session == nil {
				return
			}
			curSel, locked, ok := a.session.ActiveRect()
			if !ok || !locked || curSel.Canon() != sel {
				return
			}
			a.previewSnapRect = sel
			if err != nil || img == nil || img.Bounds().Empty() {
				a.previewSnapImg = nil
				a.previewSnapBmp = nil
				return
			}
			a.previewSnapImg = img
			a.previewSnapBmp = ui.NewBitmap(img)
			a.invalidateAll()
		})
	}()
}

// autoRedactSelection 自动离线扫描当前选区中的敏感凭证（API Key / 手机 / 邮箱 / 身份证 / IP）并生成可编辑无痕脱敏块。
func (a *app) autoRedactSelection() {
	if a.redacting || a.session == nil || a.session.Phase() != session.PhaseAdjusting {
		return
	}
	sel, locked, ok := a.session.ActiveRect()
	if !ok || !locked || sel.Empty() {
		return
	}
	gen := a.session.Gen()
	if frozenImg, frozenSel, ok := a.session.CropFrozenSelection(); ok {
		a.redacting = true
		go a.runVisionAutoRedact(frozenImg, frozenSel, gen)
		return
	}
	a.redacting = true
	a.invalidateAll()
	// 已有与选区一致的预览快照：直接复用，识别、预览与最终输出用同一份像素。
	if a.previewSnapImg != nil && a.previewSnapRect == sel.Canon() {
		go a.runVisionAutoRedact(a.previewSnapImg, sel, gen)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		img, err := a.captureRect(ctx, sel)
		mygo.RunOnMain(func() {
			if err != nil || img == nil || img.Bounds().Empty() || !a.redactionStillValid(gen, sel) {
				a.redacting = false
				a.invalidateAll()
				return
			}
			// 把这一帧装为预览快照：打码预览与最终输出（确认时直接用它）都基于识别所用像素。
			a.cancelPreviewSnap()
			a.previewSnapGen++ // 作废仍在途的旧快照采集，避免其覆盖本帧
			a.previewSnapPending = image.Rectangle{}
			a.previewSnapRect = sel.Canon()
			a.previewSnapImg = img
			a.previewSnapBmp = ui.NewBitmap(img)
			go a.runVisionAutoRedact(img, sel, gen)
		})
	}()
}

// redactionStillValid 判断自动脱敏的会话仍是发起时的那一轮，且选区未变。
func (a *app) redactionStillValid(gen int, sel image.Rectangle) bool {
	if a.session == nil || a.session.Gen() != gen || a.session.Phase() != session.PhaseAdjusting {
		return false
	}
	cur, locked, ok := a.session.ActiveRect()
	return ok && locked && cur.Canon() == sel.Canon()
}

func (a *app) runVisionAutoRedact(img *image.NRGBA, sel image.Rectangle, gen int) {
	res, err := vision.Recognize(img)
	var boxes []annotate.RectF
	if err == nil {
		for _, sb := range vision.SensitiveBoxes(res) {
			boxes = append(boxes, annotate.RectF{
				X: float64(sel.Min.X) + sb.X*float64(sel.Dx()),
				Y: float64(sel.Min.Y) + sb.Top*float64(sel.Dy()),
				W: max(8, sb.W*float64(sel.Dx())),
				H: max(8, sb.H*float64(sel.Dy())),
			})
		}
	}
	mygo.RunOnMain(func() { a.applyRedactionResult(gen, boxes) })
}

// applyRedactionResult 在主线程应用自动脱敏结果；会话已换代或不在调整阶段时丢弃，但始终复位 redacting。
func (a *app) applyRedactionResult(gen int, boxes []annotate.RectF) {
	a.redacting = false
	if a.session != nil && a.session.Gen() == gen && a.session.Phase() == session.PhaseAdjusting && len(boxes) > 0 {
		n := a.session.Annotations().AddRedactions(boxes, true)
		a.sessionStatus(fmt.Sprintf("已自动脱敏 %d 处敏感信息", n))
	}
	a.invalidateAll()
}
