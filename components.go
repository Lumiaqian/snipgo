package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const dragTempDirName = "snipgo-drag"

type badgeState struct {
	mu    sync.Mutex
	text  string
	gen   uint64
	timer *time.Timer
}

func (b *badgeState) Text(fallback string) string {
	b.mu.Lock()
	s := b.text
	b.mu.Unlock()
	if s != "" {
		return s
	}
	return fallback
}

// Set 更新状态胶囊文案，并在真实弹窗窗口下于 2.8 秒后自动淡出清空。
func (b *badgeState) Set(text string, win *mygo.Window, fallback *string) {
	if fallback != nil {
		*fallback = text
	}
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.text = text
	b.gen++
	gen := b.gen
	if text != "" && win != nil && !win.IsDestroyed() && mygo.App.IsReady() {
		b.timer = time.AfterFunc(2800*time.Millisecond, func() {
			mygo.RunOnMain(func() {
				b.mu.Lock()
				if b.gen != gen {
					b.mu.Unlock()
					return
				}
				b.text = ""
				b.timer = nil
				b.mu.Unlock()
				if fallback != nil {
					*fallback = ""
				}
				if !win.IsDestroyed() {
					win.Invalidate()
				}
			})
		})
	}
	b.mu.Unlock()
}

// paintPanelHeader 绘制所有浮窗统一的 macOS 26 无框拖拽标题栏（左：红灯关闭+图标+标题；右：自定义状态/操作区）。
func paintPanelHeader(c *ui.Context, ic *ui.SVG, title, closeLabel, closeTip string, onClose func(), rightContent func()) {
	p := uiPalette(c)
	header := ui.Row(c).
		Height(48).
		Padding(0, 18).
		Gap(8).
		AlignItems(ui.Center).
		ZIndex(2).
		DragWindow()
	header.Children(func() {
		if macTrafficCloseBtn(c, closeLabel, closeTip) && onClose != nil {
			go mygo.RunOnMain(onClose)
		}
		ui.Box(c).Width(2)
		if ic != nil {
			ui.Icon(c, ic).FontSize(15).TextColor(p.Accent)
		}
		ui.Text(c, title).FontSize(13.5).FontWeight(600).NoWrap().TextColor(p.Text)
		ui.Spacer(c)
		if rightContent != nil {
			rightContent()
		}
	})
}

// baseTextInput 创建基于 MyGo TextInputBase 的单行输入框，避免内置样式回调覆盖主题聚焦边框。
func baseTextInput(c *ui.Context, value *string) ui.Element {
	p := uiPalette(c)
	inp := ui.TextInputBase(c, value).
		Padding(5, 9).
		Radius(inputCornerRadius).
		Background(p.InputBg).
		TextColor(p.Text).
		CaretColor(p.Accent)
	bd := p.InputBorder
	if inp.Focused() {
		bd = p.Accent
	} else if inp.Hovered() {
		bd = p.SubtleBorder
	}
	return inp.Border(1, bd)
}

func toolbarDivider(c *ui.Context) {
	ui.Box(c).Size(1, 18).Margin(0, 3).Shrink(0).Background(uiPalette(c).Divider)
}

func iconToolBtn(c *ui.Context, ic *ui.SVG, label, tip string, active, disabled bool) bool {
	p := uiPalette(c)
	btn := ui.ButtonBase(c).
		Role(ui.RoleToggleButton).
		Checked(active).
		Disabled(disabled).
		DimDisabled(false).
		Label(label).
		Tooltip(tip).
		Size(26, 26).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Radius(13).
		Shrink(0)
	if !disabled {
		btn.Cursor(ui.CursorPointer)
		targetScale := float32(1.0)
		if btn.Pressed() {
			targetScale = 0.96
		}
		if sc := btn.AnimateTo("btn-sc", targetScale, springPop); sc != 1.0 {
			btn.Scale(sc, sc)
		}
	}
	hovered := !disabled && btn.Hovered()
	switch {
	case active:
		liquidActiveChipChrome(c, btn, 13)
	case hovered:
		liquidSecondaryChrome(c, btn, true, 13)
	}
	btn.Children(func() {
		col := p.Text
		if disabled {
			col = ui.Color{R: p.TextMuted.R, G: p.TextMuted.G, B: p.TextMuted.B, A: uint8(float32(p.TextMuted.A) * 0.55)}
		} else if active {
			col = p.ActiveText
		}
		ui.Icon(c, ic).FontSize(15.5).TextColor(col)
	})
	if disabled {
		return false
	}
	return btn.Clicked()
}

func pillButton(c *ui.Context, ic *ui.SVG, label, tip string, h, padX, iconSize, fontSize float32, primary, subtleBg bool) bool {
	p := uiPalette(c)
	rad := h * 0.5
	el := ui.ButtonBase(c).
		Label(label).
		Height(h).
		Padding(0, padX).
		Gap(4).
		AlignItems(ui.Center).
		Radius(rad).
		Shrink(0).
		Cursor(ui.CursorPointer)
	if tip != "" {
		el.Tooltip(tip)
	}
	targetScale := float32(1.0)
	if el.Pressed() {
		targetScale = 0.96
	}
	if sc := el.AnimateTo("btn-sc", targetScale, springPop); sc != 1.0 {
		el.Scale(sc, sc)
	}
	hovered := el.Hovered()
	switch {
	case primary:
		liquidPrimaryChrome(c, el, rad)
	case hovered || subtleBg:
		liquidSecondaryChrome(c, el, hovered, rad)
	}
	el.Children(func() {
		col := p.Text
		if primary {
			col = p.AccentText
		}
		if ic != nil {
			ui.Icon(c, ic).FontSize(iconSize).TextColor(col)
		}
		ui.Text(c, label).FontSize(fontSize).FontWeight(500).NoWrap().TextColor(col)
	})
	return el.Clicked()
}

func actionPillBtn(c *ui.Context, ic *ui.SVG, label, tip string, primary, subtleBg bool) bool {
	return pillButton(c, ic, label, tip, 26, 9, 14, 12, primary, subtleBg)
}

func miniPillBtn(c *ui.Context, ic *ui.SVG, label string, primary bool) bool {
	return pillButton(c, ic, label, "", 24, 10, 11.5, 11, primary, true)
}

func strokeDotBtn(c *ui.Context, label, tip string, dotR float32, active bool) bool {
	p := uiPalette(c)
	btn := ui.ButtonBase(c).
		Role(ui.RoleRadio).
		Checked(active).
		Label(label).
		Tooltip(tip).
		Size(24, 24).
		Radius(12).
		Shrink(0).
		Cursor(ui.CursorPointer)
	targetScale := float32(1.0)
	if btn.Pressed() {
		targetScale = 0.96
	}
	if sc := btn.AnimateTo("btn-sc", targetScale, springPop); sc != 1.0 {
		btn.Scale(sc, sc)
	}
	hovered := btn.Hovered()
	switch {
	case active:
		liquidActiveChipChrome(c, btn, 12)
	case hovered:
		liquidSecondaryChrome(c, btn, true, 12)
	}
	btn.Draw(func(painter *ui.Painter, r ui.Rect) {
		col := p.TextSecondary
		if active {
			col = p.ActiveText
		}
		cx := r.X + r.W*0.5
		cy := r.Y + r.H*0.5
		painter.Fill(ui.Rect{X: cx - dotR, Y: cy - dotR, W: dotR * 2, H: dotR * 2}, col, dotR)
	})
	return btn.Clicked()
}

func pinMiniIconBtn(c *ui.Context, ic *ui.SVG, tip string) bool {
	p := uiPalette(c)
	btn := ui.Row(c).
		Tooltip(tip).
		Size(24, 24).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Radius(12).
		Shrink(0).
		Cursor(ui.CursorPointer)
	if btn.Pressed() {
		btn.Scale(0.96, 0.96)
	}
	if btn.Hovered() {
		liquidSecondaryChrome(c, btn, true, 12)
	}
	btn.Children(func() {
		ui.Icon(c, ic).FontSize(13.5).TextColor(p.Text)
	})
	return btn.Clicked()
}

func dragTempDir() string {
	return filepath.Join(os.TempDir(), dragTempDirName)
}

// tempPNGDragData 将内存 PNG 写入临时目录并返回原生文件拖拽载荷，同时定期清理过期临时文件防止磁盘堆积。
func tempPNGDragData(pngBytes []byte, prefix string) transfer.Data {
	if len(pngBytes) == 0 {
		return transfer.Data{}
	}
	full, ok := writeDragTempPNG(dragTempDir(), pngBytes, prefix)
	if !ok {
		return transfer.Data{}
	}
	d, err := transfer.FileData(full)
	if err != nil {
		return transfer.Data{}
	}
	return d
}

// writeDragTempPNG 在 dir 中写入拖拽用临时 PNG：目录 0o700、文件 0o600（截图内容不对其他用户可读），
// 并顺带清理过期文件。返回写入后的完整路径。
func writeDragTempPNG(dir string, pngBytes []byte, prefix string) (string, bool) {
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700) // 目录可能由旧版本以 0o755 创建，尽力收紧
	pruneDragTempDirIn(dir, time.Now(), 30*time.Minute, 16)
	name := fmt.Sprintf("%s-%s.png", prefix, time.Now().Format("20060102-150405.000"))
	full := filepath.Join(dir, name)
	if os.WriteFile(full, pngBytes, 0o600) != nil {
		return "", false
	}
	return full, true
}

func pruneDragTempDirIn(dir string, now time.Time, maxAge time.Duration, maxFiles int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		path    string
		modTime time.Time
	}
	var kept []item
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".png") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, de.Name())
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(full)
			continue
		}
		kept = append(kept, item{path: full, modTime: info.ModTime()})
	}
	if maxFiles <= 0 || len(kept) < maxFiles {
		return
	}
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].modTime.After(kept[j].modTime)
	})
	for i := maxFiles - 1; i < len(kept); i++ {
		_ = os.Remove(kept[i].path)
	}
}

func cleanupAllDragTempFiles() {
	_ = os.RemoveAll(dragTempDir())
}
