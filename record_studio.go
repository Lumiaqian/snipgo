package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
	"snipgo/internal/config"
	"snipgo/internal/videocap"
)

type recordStudio struct {
	enter                     panelEnterState
	scrubbing                 bool
	badge                     badgeState
	win                       *mygo.Window
	player                    *ui.VideoPlayer
	source, output, status    string
	outputSize                int64
	start, end, duration      float64
	format, size, width, rate int
	mute, hadAudio, closed    bool
	cancel                    context.CancelFunc
	progress                  int
}

var gifWidths = []int{640, 960, 1280}
var gifRates = []int{10, 15, 24, 30}

func (a *app) openRecordStudio(r *regionRecording) {
	a.closeRecordStudio()
	s := &recordStudio{enter: panelEnterState{enabled: true}, source: r.path, hadAudio: r.hadAudio, width: min(max(a.cfg.RecordWidth, 0), 2), rate: min(max(a.cfg.RecordRate, 0), 3)}
	s.setOutput(r.path)
	if r.interrupted {
		s.status = "屏幕变化，已自动保存已录片段"
	}
	var err error
	s.player, err = video.Open(s.source)
	if err != nil {
		s.status = err.Error()
	} else {
		s.player.SetLoop(true)
		s.player.Play()
	}
	bounds := r.region.Display.Bounds
	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     "SnipGo 录屏",
		Width:     780,
		Height:    580,
		MinWidth:  700,
		MinHeight: 520,
		X: bounds.Min.X + max(0,
			(bounds.Dx()-780)/2),
		Y: bounds.Min.Y + max(0,
			(bounds.Dy()-580)/2),
		Frameless:    true,
		Transparent:  true,
		AlwaysOnTop:  true,
		Vibrancy:     vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius: panelCornerRadius,
		Content:      ui.View(func(c *ui.Context) { a.paintRecordStudio(c, s) }),
	})
	a.recStudio = s
	s.win.OnClosed(func() {
		if a.recStudio == s {
			a.recStudio = nil
		}
		s.badge.Set("", nil, nil)
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
		if s.player != nil {
			s.player.Close()
		}
		a.cfg.RecordWidth = s.width
		a.cfg.RecordRate = s.rate
		if a.userDataDir != "" {
			_ = config.Save(a.userDataDir, a.cfg)
		}
		dir := filepath.Dir(s.source)
		time.AfterFunc(24*time.Hour, func() { os.RemoveAll(dir) })
	})
	applyWindowTheme(s.win, themeStateFromConfig(a.cfg))
	s.win.Focus()
}

func (a *app) closeRecordStudio() {
	if s := a.recStudio; s != nil && !s.win.IsDestroyed() {
		s.win.Close()
	}
}

func (s *recordStudio) setOutput(path string) {
	s.output, s.outputSize = path, 0
	if path != "" {
		if info, err := os.Stat(path); err == nil {
			s.outputSize = info.Size()
		}
	}
}

func (s *recordStudio) dirty() {
	s.badge.Set("", nil, nil)
	s.status = ""
	if s.format == 0 && s.start == 0 && s.end == s.duration && s.size == 0 && !s.mute {
		s.setOutput(s.source)
	} else {
		s.setOutput("")
	}
}

// liquidSegmented reports -1 on a frame without a selection change.
func recordingSegment(c *ui.Context, name string, selected int, labels []string) int {
	if next := liquidSegmented(c, name, selected, labels); next >= 0 {
		return next
	}
	return selected
}

func (s *recordStudio) playPause() {
	if s.player == nil {
		return
	}
	if s.player.Playing() {
		s.player.Pause()
	} else {
		s.player.Play()
	}
}

func (a *app) paintRecordStudio(c *ui.Context, s *recordStudio) {
	c.Root().Background(ui.Transparent)
	withAppTheme(c, func() {
		ext := "MP4"
		if s.format == 1 {
			ext = "GIF"
		}
		liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &s.enter).Padding(16).Gap(8).Children(func() {
			s.paintHeader(c, ext)
			s.paintStage(c)
			s.paintTrim(c)
			s.paintConfig(c)
			a.paintRecordFooter(c, s, ext)
			if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
				s.win.Close()
			}
		})
	})
}

func (s *recordStudio) paintHeader(c *ui.Context, ext string) {
	p := uiPalette(c)
	busy := s.cancel != nil
	paintPanelHeader(c, iconRecord, "SnipGo 录屏", "关闭录屏预览", "关闭 (Esc)", func() { s.win.Close() }, func() {
		label := s.status
		if busy {
			label = fmt.Sprintf("正在生成 %s · %d%%", ext, s.progress)
		} else if label == "" && s.output != "" {
			label = ext + " 就绪 · 可直接拖出"
			if s.output == s.source && s.player != nil && s.player.Ready() {
				w, h := s.player.Size()
				label += fmt.Sprintf(" · %d×%d", w, h)
			}
			if s.outputSize > 0 {
				label += fmt.Sprintf(" · %.1fs · %.1f MB", s.end-s.start, float64(s.outputSize)/1e6)
			}
		} else if label == "" {
			label = "待生成 " + ext
		}
		if feedback := s.badge.Text(""); feedback != "" && !busy {
			statusBadgePill(c, feedback, true)
		} else if s.status != "" && !busy {
			ui.Row(c).Radius(12).Padding(4, 10).Background(ui.RGBA(240, 155, 50, .16)).Children(func() { ui.Text(c, label).FontSize(11.5).MaxLines(1).Ellipsis("…").TextColor(p.Text) })
		} else {
			statusBadgePill(c, label, !busy && s.output != "")
		}
	})
}

func (s *recordStudio) paintStage(c *ui.Context) {
	p := uiPalette(c)
	ui.Box(c).FillWidth().Grow(1).Radius(cardCornerRadius).Border(1, p.CardBorder).Background(p.StageBg).Padding(12).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		if s.player != nil {
			ui.Video(c, s.player).Fit(ui.Contain).Fill()
			if s.player.Ready() {
				if s.duration == 0 {
					s.duration = s.player.Duration().Seconds()
					s.end = s.duration
				}
				pos := s.player.Position().Seconds()
				if s.player.Playing() && (pos >= s.end || pos+.1 < s.start) {
					s.player.Seek(time.Duration(s.start * float64(time.Second)))
				}
				ui.Row(c).Absolute().Right(16).Bottom(16).Padding(3, 7).Radius(9).Background(ui.RGBA(0, 0, 0, .6)).IgnoreInput(true).Children(func() {
					ui.Text(c, fmt.Sprintf("%.1f / %.1fs", s.player.Position().Seconds(), s.duration)).FontFeatures("tnum").FontSize(11).TextColor(ui.RGB(255, 255, 255))
				})
			}
			c.After(100 * time.Millisecond)
		}
		if s.player == nil || !s.player.Ready() {
			label := "正在加载预览…"
			if s.player == nil && s.status != "" {
				label = "预览不可用"
			}
			ui.Box(c).Absolute().Fill().AlignItems(ui.Center).Justify(ui.Center).IgnoreInput(true).Children(func() { ui.Text(c, label).FontSize(12).TextColor(p.TextMuted) })
		}
	})
}

func recordEditCard(c *ui.Context, row ui.Element, busy bool) ui.Element {
	card := liquidCard(c, row)
	if busy {
		card.Disabled(true).DimDisabled(false).IgnoreInput(true).Opacity(.55)
	}
	return card
}

func (s *recordStudio) paintTrim(c *ui.Context) {
	busy := s.cancel != nil
	recordEditCard(c, ui.Row(c).Padding(8, 12).Gap(10).AlignItems(ui.Center), busy).Children(func() {
		ic := iconPlay
		if s.player != nil && s.player.Playing() {
			ic = iconPause
		}
		play := toolIconControl(c, ic, "播放 / 暂停", "播放或暂停预览 (Space)", false, busy, ui.Transparent).Size(28, 28)
		if !busy && (play.Clicked() || c.Shortcut(0, ui.KeySpace)) {
			s.playPause()
		}
		if s.duration > 0 {
			start, end := s.start, s.end
			slider := ui.RangeSlider(c.Key("trim"), &start, &end, 0, s.duration, .1).Grow(1).Label("裁切区间").Disabled(busy)
			if !busy && slider.Changed() {
				old := s.start
				s.start, s.end = start, end
				s.dirty()
				pos := s.end
				if old != s.start {
					pos = s.start
				}
				if s.player != nil {
					s.player.Pause()
					s.scrubbing = true
					s.player.Seek(time.Duration(pos * float64(time.Second)))
				}
			}
			if !busy && s.scrubbing && !slider.Pressed() && s.player != nil {
				s.scrubbing = false
				s.player.Seek(time.Duration(s.start * float64(time.Second)))
				s.player.Play()
			}
		}
		ui.Text(c, fmt.Sprintf("%.1f–%.1fs", s.start, s.end)).FontFeatures("tnum").FontSize(12).NoWrap()
		statusBadgePill(c, fmt.Sprintf("%.1fs", s.end-s.start), false)
		if (s.start != 0 || s.end != s.duration) && miniPillBtn(c, nil, "重置", false) && !busy {
			s.start = 0
			s.end = s.duration
			s.dirty()
		}
	})
}

func (s *recordStudio) paintConfig(c *ui.Context) {
	p := uiPalette(c)
	busy := s.cancel != nil
	recordEditCard(c, ui.Row(c).AlignItems(ui.Center).Padding(8, 12).Gap(8), busy).Children(func() {
		label := func(v string) { ui.Text(c, v).FontSize(11.5).TextColor(p.TextMuted).NoWrap() }
		label("格式")
		width, rate, size, mute := s.width, s.rate, s.size, s.mute
		format := recordingSegment(c, "录屏格式", s.format, []string{"MP4", "GIF"})
		if format == 1 {
			label("宽度 px")
			width = recordingSegment(c, "GIF 宽度", s.width, []string{"640", "960", "1280"})
			label("帧率 fps")
			rate = recordingSegment(c, "GIF 帧率", s.rate, []string{"10", "15", "24", "30"})
		} else {
			label("尺寸")
			size = recordingSegment(c, "MP4 尺寸", s.size, []string{"原始", "1080p"})
			if s.hadAudio {
				ui.Spacer(c)
				liquidSwitch(c, "mute-export", &mute, "静音导出")
			}
		}

		if !busy && (s.format != format || s.width != width || s.rate != rate || s.size != size || s.mute != mute) {
			s.format, s.width, s.rate, s.size, s.mute = format, width, rate, size, mute
			s.dirty()
		}
	})
}

func (a *app) paintRecordFooter(c *ui.Context, s *recordStudio, ext string) {
	p := uiPalette(c)
	busy := s.cancel != nil
	ui.Row(c).Height(44).Gap(8).AlignItems(ui.Center).Children(func() {
		ready := s.output != "" && !busy
		label := "先生成后可拖出"
		if ready {
			label = "拖出 " + ext
		}
		drag := ui.ButtonBase(c).Height(28).PaddingX(11).Gap(5).AlignItems(ui.Center).Label(label).Disabled(!ready).DimDisabled(false)
		liquidSecondaryChrome(c, drag, ready && drag.Hovered(), 14)
		if ready {
			drag.Cursor(ui.CursorGrab)
			if data, e := transfer.FileData(s.output); e == nil {
				drag.DragData(data)
			}
		} else {
			drag.Cursor(ui.CursorDefault).Opacity(.55)
		}
		drag.Children(func() {
			ui.Icon(c, iconDragFile).FontSize(14).TextColor(p.Accent)
			ui.Text(c, label).FontSize(12).NoWrap()
		})
		ui.Spacer(c)
		if busy {
			ui.Progress(c, float64(s.progress)/100).Width(120).Label("导出进度")
			if miniPillBtn(c, nil, "取消导出", false) {
				s.cancel()
			}
		} else if !ready {
			action := "应用裁切"
			if s.format == 1 {
				action = "生成 GIF"
			}
			if actionPillBtn(c, iconCheck, action, "回车立即生成 (Enter)", true, false) || c.Shortcut(0, ui.KeyEnter) {
				s.export()
			}
		} else {
			saveAs := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS)
			liquidGlassGroup(c, 13, 6, func(add func(ui.Element) ui.Element) {
				if glassGroupBtn(c, add, iconSave, "保存", "保存 (⌘S) / 另存为 (⇧⌘S)") || saveAs || c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveRecording(s, saveAs)
				}
			})
			if actionPillBtn(c, iconCheck, "复制文件", "将文件复制到剪贴板 (⌘C)", true, false) || c.Shortcut(ui.Cmd, ui.KeyC) {
				if err := mygo.Clipboard.WriteFiles(s.output); err != nil {
					s.status = err.Error()
				} else {
					s.status = ""
					s.badge.setFor("已复制文件", s.win, nil, 2500*time.Millisecond)
				}
			}
		}
	})
}

func (s *recordStudio) export() {
	if s.end <= s.start || s.cancel != nil {
		return
	}
	ext := ".mp4"
	width := 0
	fps := 60
	if s.size == 1 {
		width = 1920
	}
	if s.format == 1 {
		ext = ".gif"
		width = gifWidths[s.width]
		fps = gifRates[s.rate]
	}
	dst := filepath.Join(filepath.Dir(s.source), "SnipGo-trimmed"+ext)
	o := exportOptions{s.source, dst, s.start, s.end, width, fps, s.format == 1, s.mute}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.progress = 0
	go func() {
		studio, err := videocap.NewStudio()
		if err == nil {
			defer studio.Close()
			err = exportMovie(ctx, studio, o, func(n int) {
				runOnMain(func() {
					if !s.closed {
						s.progress = n
						s.win.Invalidate()
					}
				})
			})
		}
		cancel()
		runOnMain(func() {
			s.cancel = nil
			if s.closed {
				return
			}
			if errors.Is(err, context.Canceled) {
				s.status = ""
			} else if err != nil {
				s.status = err.Error()
			} else {
				s.setOutput(dst)
				s.status = ""
				if o.gif && s.outputSize > 10_000_000 {
					s.status = "GIF 就绪 · 超过 10 MB，建议调低宽度或帧率"
				}
			}
			s.win.Invalidate()
		})
	}()
}

func (a *app) saveRecording(s *recordStudio, ask bool) {
	src := s.output
	dir := a.cfg.QuickSaveDir
	name := "SnipGo-" + time.Now().Format("20060102-150405.000") + filepath.Ext(src)
	dst := filepath.Join(dir, name)
	dialog := ask || !a.cfg.QuickSave || dir == ""
	go func() {
		var err error
		if dialog {
			dst, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "保存录屏", DefaultPath: dst})
			if dst == "" && err == nil {
				return
			}
		} else {
			err = os.MkdirAll(dir, 0755)
		}
		if err == nil && filepath.Clean(src) != filepath.Clean(dst) {
			err = copyMovie(src, dst)
		}
		runOnMain(func() {
			if s.closed {
				return
			}
			if err != nil {
				s.status = err.Error()
			} else {
				s.status = ""
				s.badge.setFor("已保存 "+filepath.Base(dst), s.win, nil, 2500*time.Millisecond)
			}
			s.win.Invalidate()
		})
	}()
}

func copyMovie(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".snipgo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, in)
	err = errors.Join(err, tmp.Close())
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
