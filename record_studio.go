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
	preferredGIF              bool
	win                       *mygo.Window
	player                    *ui.VideoPlayer
	source, output, status    string
	start, end, duration      float64
	format, size, width, rate int
	mute, hadAudio, closed    bool
	cancel                    context.CancelFunc
	progress                  int
}

var gifWidths = []int{640, 960, 1280}
var gifRates = []int{10, 15, 24, 30}

func (a *app) openRecordStudio(r *regionRecording) {
	s := &recordStudio{enter: panelEnterState{enabled: true}, preferredGIF: a.cfg.RecordFormat == 1, source: r.path, output: r.path, hadAudio: r.hadAudio, width: min(max(a.cfg.RecordWidth, 0), 2), rate: min(max(a.cfg.RecordRate, 0), 3)}
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
		Width:     760,
		Height:    560,
		MinWidth:  680,
		MinHeight: 500,
		X: bounds.Min.X + max(0,
			(bounds.Dx()-760)/2),
		Y: bounds.Min.Y + max(0,
			(bounds.Dy()-560)/2),
		Frameless:    true,
		Transparent:  true,
		AlwaysOnTop:  true,
		Vibrancy:     vibrancyForGlassLevel(a.cfg.GlassLevel),
		CornerRadius: panelCornerRadius,
		Content:      ui.View(func(c *ui.Context) { a.paintRecordStudio(c, s) }),
	})
	s.win.OnClosed(func() {
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
		if s.player != nil {
			s.player.Close()
		}
		a.cfg.RecordWidth = s.width
		a.cfg.RecordRate = s.rate
		if s.preferredGIF {
			a.cfg.RecordFormat = 1
		} else {
			a.cfg.RecordFormat = 0
		}
		if a.userDataDir != "" {
			_ = config.Save(a.userDataDir, a.cfg)
		}
		dir := filepath.Dir(s.source)
		time.AfterFunc(24*time.Hour, func() { os.RemoveAll(dir) })
	})
	applyWindowTheme(s.win, themeStateFromConfig(a.cfg))
	s.win.Focus()
}

func (s *recordStudio) dirty() {
	s.output = ""
	s.status = ""
	if s.format == 0 && s.start == 0 && s.end == s.duration && s.size == 0 && !s.mute {
		s.output = s.source
		s.status = ""
	}
}

// compactSegmented reports -1 on a frame without a selection change.
func recordingSegment(c *ui.Context, name string, selected int, labels []string) int {
	if next := compactSegmented(c, name, selected, labels); next >= 0 {
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
	busy := s.cancel != nil
	ext := "MP4"
	if s.format == 1 {
		ext = "GIF"
	}
	liquidPanelEnter(liquidGlassPanel(c, ui.Column(c)), &s.enter).Padding(16).Gap(8).Children(func() {
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
				if info, err := os.Stat(s.output); err == nil {
					label += fmt.Sprintf(" · %.1fs · %.1f MB", s.end-s.start, float64(info.Size())/1e6)
				}
			} else if label == "" {
				label = "待生成 " + ext
			}
			ui.Text(c, label)
		})
		if s.player != nil {
			ui.Video(c, s.player).Fit(ui.Contain).FillWidth().Grow(1)
			if s.player.Ready() {
				if s.duration == 0 {
					s.duration = s.player.Duration().Seconds()
					s.end = s.duration
				}
				pos := s.player.Position().Seconds()
				if s.player.Playing() && (pos >= s.end || pos+.1 < s.start) {
					s.player.Seek(time.Duration(s.start * float64(time.Second)))
				}
			}
			c.After(100 * time.Millisecond)
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "播放 / 暂停").Clicked() || c.Shortcut(0, ui.KeySpace) {
				s.playPause()
			}
			ui.Text(c, fmt.Sprintf("%.1fs – %.1fs", s.start, s.end))
			if s.duration > 0 && !busy {
				old := s.start
				slider := ui.RangeSlider(c.Key("trim"), &s.start, &s.end, 0, s.duration, .1).Grow(1).Label("裁切区间")
				if slider.Changed() {
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
				if s.scrubbing && !slider.Pressed() && s.player != nil {
					s.scrubbing = false
					s.player.Seek(time.Duration(s.start * float64(time.Second)))
					s.player.Play()
				}
				if (s.start != 0 || s.end != s.duration) && ui.Button(c, "重置").Clicked() {
					s.start = 0
					s.end = s.duration
					s.dirty()
				}
			}
		})
		if !busy {
			ui.Row(c).Gap(8).Children(func() {
				oldFormat, oldWidth, oldRate, oldSize := s.format, s.width, s.rate, s.size
				s.format = recordingSegment(c, "录屏格式", s.format, []string{"MP4", "GIF"})
				if s.format == 1 {
					s.width = recordingSegment(c, "GIF 宽度", s.width, []string{"640 px", "960 px", "1280 px"})
					s.rate = recordingSegment(c, "GIF 帧率", s.rate, []string{"10 fps", "15 fps", "24 fps", "30 fps"})
				} else {
					s.size = recordingSegment(c, "MP4 尺寸", s.size, []string{"原始", "1080p"})
					if s.hadAudio && liquidSwitch(c, "mute-export", &s.mute, "静音导出") {
						s.dirty()
					}
				}
				if oldFormat != s.format {
					s.preferredGIF = s.format == 1
				}
				if oldFormat != s.format || oldWidth != s.width || oldRate != s.rate || oldSize != s.size {
					s.dirty()
				}
			})
		}
		ui.Row(c).Gap(8).Children(func() {
			drag := ui.Button(c, "拖出 "+ext).Disabled(s.output == "")
			if s.output != "" {
				if data, e := transfer.FileData(s.output); e == nil {
					drag.DragData(data)
				}
			}
			if (ui.Button(c, "复制文件").Disabled(s.output == "").Clicked() || c.Shortcut(ui.Cmd, ui.KeyC)) && s.output != "" {
				if e := mygo.Clipboard.WriteFiles(s.output); e != nil {
					s.status = e.Error()
				}
			}
			saveAs := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS)
			if (ui.Button(c, "保存").Disabled(s.output == "").Clicked() || saveAs || c.Shortcut(ui.Cmd, ui.KeyS)) && s.output != "" {
				a.saveRecording(s, saveAs)
			}
			if s.preferredGIF && s.output == s.source && s.format == 0 && s.duration > 0 {
				if ui.Button(c, "生成上次格式 GIF").Clicked() {
					s.format = 1
					s.dirty()
					s.export()
				}
			}
			if busy {
				ui.Progress(c, float64(s.progress)/100).Width(100).Label("导出进度")
				if ui.Button(c, "取消导出").Clicked() {
					s.cancel()
				}
			} else if s.output == "" {
				label := "应用裁切"
				if s.format == 1 {
					label = "生成 GIF"
				}
				if ui.Button(c, label).Clicked() || c.Shortcut(0, ui.KeyEnter) {
					s.export()
				}
			}
		})
		if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
			s.win.Close()
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
			if err != nil {
				s.status = err.Error()
			} else {
				s.output = dst
				s.status = ""
				if info, e := os.Stat(dst); e == nil && o.gif && info.Size() > 10_000_000 {
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
				s.status = "已保存 " + filepath.Base(dst)
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
