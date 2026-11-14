// Package session 管一轮截图：权限、采集、框选、复制和贴图。
// 选区窗口出现之前画面已经冻结，裁剪只读这些像素。
package session

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"time"

	"snipgo/internal/crop"
)

const minDIP = 2

// UI 是选区、贴图和菜单栏状态。方法在主线程调用。
type UI interface {
	SetStatus(text string)
	ShowSelection(frames []crop.Screen)
	HideSelection()
	ShowPin(img *image.NRGBA, at image.Point, dipW, dipH int)
}

// Clipboard 写入 PNG。
type Clipboard interface {
	WritePNG(png []byte) error
}

// Session 同一时间只进行一轮截图。
type Session struct {
	Allowed func() (ok bool, reason string)
	Capture func(context.Context) ([]crop.Screen, error)
	Clip    Clipboard
	UI      UI
	// OnMain 把采集结果送回主线程。测试里直接执行。
	OnMain func(func())
	// Launch 替换默认的后台采集。测试用它观察有没有开始采集。
	Launch func(gen int)

	gen    int
	phase  phase
	frames []crop.Screen
	drag   drag
	status string
}

type phase int

const (
	phaseIdle phase = iota
	phaseCapturing
	phaseSelecting
)

type drag struct {
	active bool
	owner  int
	x0, y0 float64
	x1, y1 float64
}

// Hotkey 在主线程调用。选区还在时先关掉，再开始新一轮。
func (s *Session) Hotkey() {
	if s.phase == phaseSelecting {
		s.clearSelection()
	}
	s.gen++
	gen := s.gen
	ok, reason := s.Allowed()
	if !ok {
		s.fail(reason)
		return
	}
	s.phase = phaseCapturing
	s.kick(gen)
}

func (s *Session) kick(gen int) {
	if s.Launch != nil {
		s.Launch(gen)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		frames, err := s.Capture(ctx)
		s.OnMain(func() { s.Deliver(gen, frames, err) })
	}()
}

// Deliver 在主线程应用某一轮的采集结果。过期的轮次直接丢掉。
func (s *Session) Deliver(gen int, frames []crop.Screen, err error) {
	if gen != s.gen || s.phase != phaseCapturing {
		return
	}
	if err != nil {
		s.fail(err.Error())
		return
	}
	if len(frames) == 0 {
		s.fail("没有截到屏幕")
		return
	}
	s.frames = frames
	s.drag = drag{owner: -1}
	s.phase = phaseSelecting
	s.status = ""
	s.UI.SetStatus("")
	s.UI.ShowSelection(frames)
}

// Cancel 关掉选区，不写剪贴板，也不贴图。进行中的采集结果会被丢弃。
func (s *Session) Cancel() {
	if s.phase == phaseIdle {
		return
	}
	s.gen++
	s.clearSelection()
}

// BeginDrag 从 owner 这块屏上的全局 DIP 点开始框选。
func (s *Session) BeginDrag(owner int, x, y float64) {
	if s.phase != phaseSelecting {
		return
	}
	s.drag = drag{active: true, owner: owner, x0: x, y0: y, x1: x, y1: y}
}

// MoveDrag 更新同一块屏上的拖动终点。
func (s *Session) MoveDrag(owner int, x, y float64) {
	if !s.drag.active || s.drag.owner != owner {
		return
	}
	s.drag.x1, s.drag.y1 = x, y
}

// Drag 返回正在画的选区，以及它从哪块屏开始。
func (s *Session) Drag() (owner int, rect image.Rectangle, ok bool) {
	if !s.drag.active {
		return -1, image.Rectangle{}, false
	}
	return s.drag.owner, normRect(s.drag.x0, s.drag.y0, s.drag.x1, s.drag.y1), true
}

// EndDrag 结束框选。小于 2 DIP 的矩形视为取消。
func (s *Session) EndDrag() {
	if !s.drag.active || s.phase != phaseSelecting {
		return
	}
	r := normRect(s.drag.x0, s.drag.y0, s.drag.x1, s.drag.y1)
	s.drag = drag{owner: -1}
	if r.Dx() < minDIP || r.Dy() < minDIP {
		s.Cancel()
		return
	}
	s.confirm(r)
}

func (s *Session) confirm(r image.Rectangle) {
	img, err := crop.Crop(s.frames, r)
	if err != nil {
		s.fail(err.Error())
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		s.fail(err.Error())
		return
	}
	if err := s.Clip.WritePNG(buf.Bytes()); err != nil {
		s.status = "已贴图，复制到剪贴板失败"
		s.UI.SetStatus(s.status)
	} else {
		s.status = ""
		s.UI.SetStatus("")
	}
	s.UI.ShowPin(img, r.Min, r.Dx(), r.Dy())
	s.clearSelection()
}

func (s *Session) fail(msg string) {
	s.phase = phaseIdle
	s.dropFrames()
	s.drag = drag{owner: -1}
	s.status = msg
	s.UI.SetStatus(msg)
	s.UI.HideSelection()
}

func (s *Session) clearSelection() {
	s.phase = phaseIdle
	s.dropFrames()
	s.drag = drag{owner: -1}
	s.UI.HideSelection()
}

func (s *Session) dropFrames() {
	for i := range s.frames {
		s.frames[i].Pixels = nil
	}
	s.frames = nil
}

// Status 是菜单栏上的说明。
func (s *Session) Status() string { return s.status }

func normRect(x0, y0, x1, y1 float64) image.Rectangle {
	ax, ay := roundDIP(x0), roundDIP(y0)
	bx, by := roundDIP(x1), roundDIP(y1)
	if ax > bx {
		ax, bx = bx, ax
	}
	if ay > by {
		ay, by = by, ay
	}
	return image.Rect(ax, ay, bx, by)
}

func roundDIP(v float64) int {
	if v < 0 {
		return int(v - 0.5)
	}
	return int(v + 0.5)
}
