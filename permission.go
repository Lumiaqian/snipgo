package main

import (
	"context"
	"errors"
	"image"
	"time"

	"github.com/egoist/mygo"

	"snipgo/internal/capture"
)

// 实时蒙层本身不需要屏幕录制权限，用户可能标注很久才在确认时失败。
// 所以启动实时截图前先做一次权限预检；下面三个变量供测试替换。
var (
	// hasScreenPermission 是不弹窗的权限预检（CGPreflightScreenCaptureAccess），可能有假阴性。
	hasScreenPermission = capture.HasPermission
	// probeScreenCapture 在预检为 false 时真实采集一个极小区域来确认权限。
	probeScreenCapture = defaultProbeScreenCapture
	// promptPermissionAsync 弹出重新授权对话框。
	promptPermissionAsync = func(a *app) { go a.promptPermission() }
)

const probeTimeout = 1500 * time.Millisecond

// allowCapture 是 Session.Allowed：有权限（或探测未明确被拒）才放行。
func (a *app) allowCapture() (bool, string) {
	if hasScreenPermission() {
		return true, ""
	}
	if err := probeScreenCapture(a); errors.Is(err, capture.ErrPermissionDenied) {
		promptPermissionAsync(a)
		return false, capture.ErrPermissionDenied.Error()
	}
	return true, ""
}

// defaultProbeScreenCapture 在光标处（落在某块显示器内）采集 2×2 DIP，超时 1.5s。
func defaultProbeScreenCapture(a *app) error {
	displays, err := a.currentDisplays()
	if err != nil || len(displays) == 0 {
		return err
	}
	cur := mygo.Screen.CursorScreenPoint()
	rect := image.Rect(cur.X, cur.Y, cur.X+2, cur.Y+2)
	target := displays[0]
	for _, d := range displays {
		if image.Pt(cur.X, cur.Y).In(d.Bounds) {
			target = d
			break
		}
	}
	rect = rect.Intersect(target.Bounds)
	if rect.Empty() {
		rect = image.Rect(target.Bounds.Min.X, target.Bounds.Min.Y, target.Bounds.Min.X+2, target.Bounds.Min.Y+2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_, err = capture.GrabRect(ctx, displays, rect, overlayTitle)
	return err
}
