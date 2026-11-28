// Package session 管一轮截图会话：
// 默认模式（Hotkey / StartLive）0ms 直接弹出透明蒙层，确认（复制 / 贴图 / 保存 / 识字）时才按选区采集屏幕；
// 可选冻结模式（StartFrozen）先定格全屏画面再框选，支持放大镜逐像素取色与十字测量尺；
// 在 Adjusting 阶段内置矢量标注、单窗无背景抠图与圆角渐变美化卡片处理。
package session

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"snipgo/internal/annotate"
	"snipgo/internal/beautify"
	"snipgo/internal/crop"
	"snipgo/internal/measure"
)

const minDIP = 2

// Phase 表示当前截图会话所处阶段。
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseCapturing
	PhaseSelecting
	PhaseAdjusting
)

// ConfirmAction 表示确认选区后要执行的动作。
type ConfirmAction int

const (
	ActionCopy ConfirmAction = iota
	ActionPin
	ActionSave
	ActionSaveAs
	ActionOCR
	ActionCutout
	ActionTemplate
)

// WinTarget 记录单个可见窗口的 CGWindowID 与边界。
type WinTarget struct {
	ID     uint32
	Bounds image.Rectangle
}

// Snapshot 是一次屏幕几何（或冻结画面）、当前前台应用窗口与可见窗口列表的快照。
type Snapshot struct {
	HasPopupMenu bool
	Frames       []crop.Screen
	FrontWindow  image.Rectangle
	FrontWinID   uint32
	Windows      []image.Rectangle
	WindowInfos  []WinTarget
}

// RulerReading 是一次像素测量尺读数（全局 DIP 边界 + 物理像素宽高）。
type RulerReading struct {
	LeftDIP, RightDIP   float64
	TopDIP, BottomDIP   float64
	OriginX, OriginY    float64
	WidthDIP, HeightDIP int
	WidthPx, HeightPx   int
}

// UI 是选区蒙层、贴图、保存对话框、OCR 回调和菜单栏状态。方法均在主线程调用。
type UI interface {
	SetStatus(text string)
	ShowSelection(frames []crop.Screen)
	HideSelection()
	ShowPin(img *image.NRGBA, png []byte, at image.Point, dipW, dipH int)
	SavePNG(png []byte)
	RecognizeImage(img *image.NRGBA)
}

// Clipboard 读写系统剪贴板。
type Clipboard interface {
	WritePNG(png []byte) error
	WriteText(text string)
}

type dragKind int

const (
	dragNone          dragKind = iota
	dragBox                    // 新画选区
	dragMove                   // 平移已有选区
	dragResize                 // 拖拽控制柄缩放
	dragPendingRedraw          // 在已有选区外按下，拖够 minDIP 才替换选区
	dragAnnotate               // 在选区内绘制矢量标注
	dragAnnotateEdit           // 拖拽平移或调整已选中的标注图元
)

type dragState struct {
	kind   dragKind
	owner  int
	handle Handle
	start  image.Rectangle
	x0, y0 float64
	x1, y1 float64
	// restore 仅在 dragPendingRedraw 越过阈值、已清空旧选区后记录，供取消时找回。
	restore *redrawBackup
}

// redrawBackup 保存「在选区外重画」开始前被清空的选区状态。
type redrawBackup struct {
	lockedWinID   uint32
	lockedWinRect image.Rectangle
	windowOnly    bool
	items         []annotate.Item
}

// Session 同一时间只进行一轮截图。
type Session struct {
	Allowed func() (ok bool, reason string)
	// Live 同步返回当前各显示器的 DIP 几何（Pixels 为 nil）与可见窗口列表，供实时透明蒙层 0ms 启动。
	Live func() (Snapshot, error)
	// CaptureRect 在实时透明蒙层确认后只采集选区矩形对应的屏幕像素。
	CaptureRect func(ctx context.Context, sel image.Rectangle) (*image.NRGBA, error)
	// PreviewFrame 返回蒙层打码/擦除/放大镜预览所用的像素快照（仅当其矩形恰为 sel 时返回）。
	// 确认时若标注依赖该快照，直接用它渲染最终图，保证「所见即所得」，不再重新采集（内容可能已变化）。
	PreviewFrame func(sel image.Rectangle) (*image.NRGBA, bool)
	// CaptureWindow 使用 desktopIndependentWindow 单独采集指定窗口本体（无背景壁纸与其他窗口干扰）。
	CaptureWindow func(ctx context.Context, winID uint32, scale float64) (*image.NRGBA, error)
	// Capture 在冻结模式下预先采集所有显示器的完整像素。
	Capture func(context.Context) (Snapshot, error)
	Clip    Clipboard
	UI      UI
	// OnOutput 在每次成功生成截图 PNG（复制 / 贴图 / 保存）时回调，供本地历史记录归档；scale 为输出的像素/DIP 比例（0 表示未知）。
	OnOutput func(png []byte, pxW, pxH int, scale float64)
	// OnCutout 在触发选区主体抠图（K）并采集完像素后回调，交由 Vision 离线去背景并生成异形贴纸。
	OnCutout func(img *image.NRGBA, r image.Rectangle)
	// OnTemplate 在触发套模板（B）并采集/合成完选区标注后回调，打开所见即所得「套模板工作台」。
	OnTemplate func(img *image.NRGBA, r image.Rectangle)
	// OnSaveAs 在触发 ⇧⌘S 另存为并编码 PNG 后回调，强制弹出保存对话框。
	OnSaveAs func(png []byte)
	// OnMain 把后台采集结果送回主线程。测试里直接同步执行。
	OnMain func(func())
	// Launch 替换默认的后台整屏冻结采集（测试钩子）。
	Launch func(gen int)
	// Encode 把最终图像编码为 PNG（默认 png.DefaultCompression）；始终在后台 goroutine 调用，测试可替换以断言线程。
	Encode func(*image.NRGBA) ([]byte, error)
	// Background 在后台执行冻结模式的后处理与编码（默认 go fn()，测试可替换为同步执行）。
	Background func(func())

	gen                   int
	phase                 Phase
	live                  bool
	autoOCR               bool
	preferFront           bool
	windowSnap            bool
	windowOnly            bool
	beautify              bool
	beautifyPreset        beautify.Preset
	beautifyChrome        bool
	colorFmt              beautify.ColorFormat
	pickedColors          []color.NRGBA
	colorPickerMode       bool
	preservePicked        bool
	enterMeasureOnDeliver bool

	// OnColorFormatChanged 在用户在选区或色板栏切换取色格式时触发，便于上层持久化配置。
	OnColorFormatChanged func(beautify.ColorFormat)
	// OnColorCopied 在取色复制到剪贴板后触发（供轻提示浮层显示反馈）。
	OnColorCopied func(text string, col color.NRGBA)
	// OnRulerCopied 在测量尺单击复制尺寸后触发（供轻提示浮层显示反馈）。
	OnRulerCopied func(text string)
	// OnPalettePinned 在贴出色板卡时触发；为空时回退到 UI.ShowPin。
	OnPalettePinned func(colors []color.NRGBA, img *image.NRGBA, png []byte, at image.Point, dipW, dipH int)
	measureMode     bool
	lastOCRRect     image.Rectangle
	frames          []crop.Screen
	windows         []image.Rectangle
	winIDByRect     map[image.Rectangle]uint32
	hoverWin        image.Rectangle
	lockedWinRect   image.Rectangle
	lockedWinID     uint32
	sel             image.Rectangle
	annot           *annotate.Canvas
	drag            dragState
	initX           float64
	initY           float64
	hasInit         bool
	cursorX         float64
	cursorY         float64
	hasCursor       bool
	status          string

	// pending 是当前后台采集对应的恢复快照；仅在采集失败时转为 aborted。
	pending *abortedSelection
	// aborted 是最近一次实时采集失败后保留的选区与标注，可经 RestoreAborted 找回。
	aborted *abortedSelection
}

// abortedSelection 保存实时/单窗采集失败前的选区状态，避免失败时丢掉用户已画的标注。
type abortedSelection struct {
	gen            int
	act            ConfirmAction
	sel            image.Rectangle
	lockedWinRect  image.Rectangle
	lockedWinID    uint32
	windowOnly     bool
	beautify       bool
	beautifyPreset beautify.Preset
	beautifyChrome bool
	colorFmt       beautify.ColorFormat
	preferFront    bool
	autoOCR        bool
	items          []annotate.Item
}

// HasAborted 返回是否存在采集失败后可恢复的选区。
func (s *Session) HasAborted() bool { return s.aborted != nil }

// AbortedItemCount 返回可恢复选区里的标注数量。
func (s *Session) AbortedItemCount() int {
	if s.aborted == nil {
		return 0
	}
	return len(s.aborted.items)
}

// DiscardAborted 放弃可恢复的选区。
func (s *Session) DiscardAborted() { s.aborted = nil }

// RestoreAborted 重新进入实时选区并带回失败前的选区、标注与选项；无可恢复内容或无法恢复时返回 false。
func (s *Session) RestoreAborted() bool {
	ab := s.aborted
	if ab == nil || s.phase == PhaseCapturing || s.Live == nil {
		return false
	}
	snap, err := s.Live()
	if err != nil || len(snap.Frames) == 0 {
		return false
	}
	sel := ab.sel.Canon().Intersect(screensUnion(snap.Frames))
	if sel.Empty() {
		return false
	}
	if s.phase == PhaseSelecting || s.phase == PhaseAdjusting {
		s.clearSelection()
	}
	s.gen++
	s.aborted = nil
	s.live = true
	s.autoOCR = ab.autoOCR
	s.preferFront = ab.preferFront
	s.enterSelecting(snap)
	s.sel = sel
	if ab.lockedWinID != 0 && sel == ab.lockedWinRect {
		s.lockedWinRect = ab.lockedWinRect
		s.lockedWinID = ab.lockedWinID
	}
	s.windowOnly = ab.windowOnly
	s.beautify = ab.beautify
	s.beautifyPreset = ab.beautifyPreset
	s.beautifyChrome = ab.beautifyChrome
	s.colorFmt = ab.colorFmt
	s.Annotations().SetItems(ab.items)
	s.phase = PhaseAdjusting
	return true
}

// Annotations 返回当前选区的矢量标注画布（惰性初始化）。
func (s *Session) Annotations() *annotate.Canvas {
	if s.annot == nil {
		s.annot = annotate.NewCanvas()
	}
	return s.annot
}

// Phase 返回当前阶段。
func (s *Session) Phase() Phase { return s.phase }

// Gen 返回会话代数；每次开始/取消/确认都会递增，异步结果用它判断是否过期。
func (s *Session) Gen() int { return s.gen }

// IsLive 报告当前是否为实时透明蒙层模式（未预截全屏）。
func (s *Session) IsLive() bool { return s.live }

// AutoOCR 报告当前是否为「松手即刻识图」快捷模式。
func (s *Session) AutoOCR() bool { return s.autoOCR }

// LastOCRRect 返回最近一次触发识图时的屏幕选区 DIP 矩形，供结果面板就近定位。
func (s *Session) LastOCRRect() image.Rectangle { return s.lastOCRRect }

// WindowSnap 报告当前是否开启了窗口点选高亮模式（按空格键切换）。
func (s *Session) WindowSnap() bool { return s.windowSnap }

// CanWindowOnly 报告当前已锁定选区是否正好对应一个可独立抠图的原生窗口。
func (s *Session) CanWindowOnly() bool {
	return s.phase == PhaseAdjusting && s.CaptureWindow != nil && s.lockedWinID != 0 && s.sel == s.lockedWinRect
}

// WindowOnly 报告是否开启了「仅截独立窗口本体（剔除桌面与遮挡窗）」模式。
func (s *Session) WindowOnly() bool { return s.CanWindowOnly() && s.windowOnly }

// ToggleWindowOnly 切换独立单窗抠图模式。
func (s *Session) ToggleWindowOnly() bool {
	if !s.CanWindowOnly() {
		return false
	}
	s.windowOnly = !s.windowOnly
	return true
}

// Beautify 报告是否开启圆角阴影渐变背景美化。
func (s *Session) Beautify() bool { return s.beautify }

// BeautifyPreset 返回当前选中的渐变预设。
func (s *Session) BeautifyPreset() beautify.Preset { return s.beautifyPreset }

// ToggleBeautify 开关美化背景卡片。
func (s *Session) ToggleBeautify() bool {
	s.beautify = !s.beautify
	return s.beautify
}

// SetBeautifyPreset 设置美化背景渐变预设并自动开启美化。
func (s *Session) SetBeautifyPreset(p beautify.Preset) {
	s.beautifyPreset = p
	s.beautify = true
}

// BeautifyChrome 报告美化卡片是否附加仿真 macOS 红黄绿交通灯顶栏。
func (s *Session) BeautifyChrome() bool { return s.beautifyChrome }

// ToggleBeautifyChrome 开关美化卡片的仿真 macOS 窗口顶栏。
func (s *Session) ToggleBeautifyChrome() bool {
	s.beautifyChrome = !s.beautifyChrome
	if s.beautifyChrome {
		s.beautify = true
	}
	return s.beautifyChrome
}

// MeasureMode 报告冻结模式下是否开启了像素测量尺。
func (s *Session) MeasureMode() bool { return !s.live && s.measureMode }

// ToggleMeasure 切换冻结模式下的十字像素测量尺。
func (s *Session) ToggleMeasure() bool {
	if s.live {
		return false
	}
	s.measureMode = !s.measureMode
	return true
}

// ToggleWindowSnap 在 Selecting 阶段切换窗口自动高亮模式（对齐 macOS Cmd+Shift+4 按空格选窗口）。
func (s *Session) ToggleWindowSnap() bool {
	if s.phase != PhaseSelecting || s.drag.kind != dragNone {
		return false
	}
	s.windowSnap = !s.windowSnap
	if s.windowSnap && s.hasCursor {
		s.hoverWin = detectWindow(s.windows, s.frames, s.cursorX, s.cursorY)
	} else {
		s.hoverWin = image.Rectangle{}
	}
	return true
}

// Status 是菜单栏上的说明。
func (s *Session) Status() string { return s.status }

// Dragging 报告当前是否正在按住鼠标拖拽选区。
func (s *Session) Dragging() bool {
	return s.drag.kind != dragNone && s.drag.kind != dragAnnotate && s.drag.kind != dragAnnotateEdit
}

// AnnotatingDrag 报告当前是否正在按住鼠标绘制或编辑标注图元。
func (s *Session) AnnotatingDrag() bool {
	return s.drag.kind == dragAnnotate || s.drag.kind == dragAnnotateEdit
}

// DragOwner 返回当前按住拖拽起始于哪块屏（未拖拽时为 -1）。
func (s *Session) DragOwner() int {
	if s.drag.kind == dragNone {
		return -1
	}
	return s.drag.owner
}

// Hotkey 默认启动实时透明框选；若未注入 Live（如纯冻结测试），回退到 StartFrozen。
func (s *Session) Hotkey() {
	if s.Live != nil {
		s.StartLive()
		return
	}
	s.StartFrozen()
}

// StartLive 立即弹出实时透明选区蒙层（无框启动：只展示十字准星与透明蒙层，首次拖拽或单击窗口后再出现选区框）。
func (s *Session) StartLive() {
	s.startLive(false, false)
}

// StartLiveFromMenu 从菜单栏点击启动实时透明选区蒙层（无框启动：只展示十字准星与透明蒙层，首次拖拽或单击窗口后再出现选区框）。
func (s *Session) StartLiveFromMenu() {
	s.startLive(true, false)
}

// StartLiveOCR 启动一键框选识图模式：拖出选区（或单击窗口）松手后无需再点按钮，直接提取文字与二维码。
func (s *Session) StartLiveOCR() {
	s.startLive(false, true)
}

// StartLiveOCRFromMenu 从菜单栏启动一键框选识图模式。
func (s *Session) StartLiveOCRFromMenu() {
	s.startLive(true, true)
}

func (s *Session) startLive(preferFront, autoOCR bool) {
	if s.phase == PhaseCapturing {
		return
	}
	if s.phase == PhaseSelecting || s.phase == PhaseAdjusting {
		s.clearSelection()
	}
	s.gen++
	if s.Allowed != nil {
		if ok, reason := s.Allowed(); !ok {
			s.fail(reason)
			return
		}
	}
	if s.Live == nil {
		s.fail("未配置实时屏幕信息")
		return
	}
	snap, err := s.Live()
	if err != nil {
		s.fail(err.Error())
		return
	}
	if len(snap.Frames) == 0 {
		s.fail("没有检测到显示器")
		return
	}
	if snap.HasPopupMenu && !preferFront && !autoOCR && (s.Launch != nil || s.Capture != nil) {
		s.beginFrozen(false, false)
		return
	}
	s.live = true
	s.autoOCR = autoOCR
	s.colorPickerMode = false
	s.preservePicked = false
	s.enterMeasureOnDeliver = false
	s.preferFront = preferFront
	s.enterSelecting(snap)
}

// StartFrozen 先冻结所有屏幕画面再弹出选区蒙层（适合定格瞬时弹窗/动画及逐像素取色、像素测量尺）。
func (s *Session) StartFrozen() {
	s.startFrozen(false, false)
}

// StartFrozenFromMenu 从菜单栏点击启动冻结屏幕截图。
func (s *Session) StartFrozenFromMenu() {
	s.startFrozen(true, false)
}

// StartColorPicker 启动独立冻结屏幕取色模式（单击/C 复制色值，P 采集多色卡）。
func (s *Session) StartColorPicker() {
	s.startFrozen(false, true)
}

// StartColorPickerFromMenu 从菜单栏点击启动独立冻结屏幕取色模式。
func (s *Session) StartColorPickerFromMenu() {
	s.startFrozen(true, true)
}

// SwitchLiveToFrozenForColor 在实时选区阶段按 Tab 切入冻结取色模式，保留已采集色板与光标位置。
func (s *Session) SwitchLiveToFrozenForColor() bool {
	return s.switchLiveToFrozen(true, false)
}

// SwitchLiveToFrozenForMeasure 在实时选区阶段按 R 切入冻结像素测量尺模式，保留已采集色板与光标位置。
func (s *Session) SwitchLiveToFrozenForMeasure() bool {
	return s.switchLiveToFrozen(false, true)
}

func (s *Session) switchLiveToFrozen(colorPicker, measure bool) bool {
	if !s.live || s.phase != PhaseSelecting {
		return false
	}
	if s.Allowed != nil {
		if ok, reason := s.Allowed(); !ok {
			s.fail(reason)
			return false
		}
	}
	if s.Launch == nil && s.Capture == nil {
		return false
	}
	s.gen++
	gen := s.gen
	s.preservePicked = true
	s.colorPickerMode = colorPicker
	s.enterMeasureOnDeliver = measure
	s.live = false
	s.autoOCR = false
	s.preferFront = false
	s.dropFrames()
	s.drag = dragState{owner: -1}
	s.phase = PhaseCapturing
	s.UI.HideSelection()
	s.kickFrozen(gen)
	return true
}

func (s *Session) startFrozen(preferFront, colorPicker bool) {
	if s.phase == PhaseCapturing {
		return
	}
	if s.phase == PhaseSelecting || s.phase == PhaseAdjusting {
		s.clearSelection()
	}
	s.gen++
	if s.Allowed != nil {
		if ok, reason := s.Allowed(); !ok {
			s.fail(reason)
			return
		}
	}
	s.beginFrozen(preferFront, colorPicker)
}

func (s *Session) beginFrozen(preferFront, colorPicker bool) {
	s.live = false
	s.autoOCR = false
	s.colorPickerMode = colorPicker
	s.preservePicked = false
	s.enterMeasureOnDeliver = false
	s.preferFront = preferFront
	s.phase = PhaseCapturing
	s.kickFrozen(s.gen)
}

func (s *Session) kickFrozen(gen int) {
	if s.Launch != nil {
		s.Launch(gen)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		snap, err := s.Capture(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("截图超时，请重试")
		}
		s.OnMain(func() { s.Deliver(gen, snap, err) })
	}()
}

// Deliver 在主线程应用冻结采集结果。过期的轮次直接丢掉。
func (s *Session) Deliver(gen int, snap Snapshot, err error) {
	if gen != s.gen || s.phase != PhaseCapturing {
		return
	}
	if err != nil {
		s.fail(err.Error())
		return
	}
	if len(snap.Frames) == 0 {
		s.fail("没有截到屏幕")
		return
	}
	s.live = false
	s.enterSelecting(snap)
}

func (s *Session) enterSelecting(snap Snapshot) {
	s.frames = snap.Frames
	s.windows = snap.Windows
	s.winIDByRect = make(map[image.Rectangle]uint32, len(snap.WindowInfos)+1)
	union := screensUnion(snap.Frames)
	for _, wi := range snap.WindowInfos {
		b := wi.Bounds.Canon()
		// 仅登记完全位于显示器并集内的窗口：部分在屏外的窗口整窗采集会与裁剪后的选区错位，
		// 此时退化为普通区域截图（CanWindowOnly 为 false）。
		if !union.Empty() && b.Intersect(union) != b {
			continue
		}
		if !b.Empty() && wi.ID != 0 {
			if _, exists := s.winIDByRect[b]; !exists {
				s.winIDByRect[b] = wi.ID
			}
		}
	}
	if len(s.windows) == 0 && len(snap.WindowInfos) > 0 {
		s.windows = make([]image.Rectangle, len(snap.WindowInfos))
		for i, wi := range snap.WindowInfos {
			s.windows[i] = wi.Bounds
		}
	}

	preservePicked := s.preservePicked
	s.preservePicked = false
	enterMeasure := s.enterMeasureOnDeliver
	s.enterMeasureOnDeliver = false

	s.windowSnap = false
	s.windowOnly = false
	s.beautify = false
	s.measureMode = enterMeasure
	if !preservePicked {
		s.pickedColors = nil
		s.hasCursor = false
	}
	s.lockedWinID = 0
	s.lockedWinRect = image.Rectangle{}
	s.sel = image.Rectangle{}
	s.Annotations().Reset()
	s.drag = dragState{owner: -1}
	s.hasInit = false
	s.phase = PhaseSelecting
	s.status = ""
	s.UI.SetStatus("")

	front := snap.FrontWindow.Canon()
	frontInside := union.Empty() || front.Intersect(union) == front
	if !union.Empty() && !front.Empty() {
		front = front.Intersect(union)
	}
	if !front.Empty() && snap.FrontWinID != 0 && frontInside {
		if _, exists := s.winIDByRect[front]; !exists {
			s.winIDByRect[front] = snap.FrontWinID
		}
	}
	if len(s.windows) == 0 && front.Dx() >= minDIP && front.Dy() >= minDIP {
		s.windows = []image.Rectangle{front}
	}
	s.hoverWin = image.Rectangle{}
	s.UI.ShowSelection(snap.Frames)
}

// InitCursor 记录截图刚启动时的初始光标位置。
func (s *Session) InitCursor(gx, gy float64) {
	if s.phase != PhaseSelecting {
		return
	}
	s.cursorX, s.cursorY, s.hasCursor = gx, gy, true
	if !s.windowSnap {
		s.hasInit = false
		return
	}
	if !s.preferFront {
		if hit := detectWindow(s.windows, s.frames, gx, gy); !hit.Empty() {
			s.hoverWin = hit
			s.hasInit = false
			return
		}
	}
	if !s.hoverWin.Empty() {
		s.initX, s.initY, s.hasInit = gx, gy, true
		return
	}
	s.hoverWin = detectWindow(s.windows, s.frames, gx, gy)
	s.hasInit = false
}

// Cancel 关掉选区，不写剪贴板，也不贴图。进行中的采集结果会被丢弃。
func (s *Session) Cancel() {
	if s.phase == PhaseIdle {
		return
	}
	s.gen++
	s.clearSelection()
}

// CancelDrag 仅中止当前拖拽（例如系统取消了指针），不丢弃已有选区与标注。
func (s *Session) CancelDrag() {
	d := s.drag
	if d.kind == dragNone {
		return
	}
	s.drag = dragState{owner: -1}
	switch d.kind {
	case dragAnnotate, dragAnnotateEdit:
		// CancelPointer 会丢弃未完成图元，编辑中则还原到编辑前快照。
		s.Annotations().CancelPointer()
	case dragMove, dragResize:
		s.sel = d.start
	case dragBox:
		// 由「选区外重画」转来的拖拽：找回被清空的旧选区、窗口锁定与标注。
		if d.restore != nil && !d.start.Empty() {
			s.sel = d.start
			s.lockedWinID = d.restore.lockedWinID
			s.lockedWinRect = d.restore.lockedWinRect
			s.windowOnly = d.restore.windowOnly
			s.Annotations().SetItems(d.restore.items)
			s.phase = PhaseAdjusting
		}
		// 全新框选：保持 PhaseSelecting，不产生选区。
	}
}

// RightClick 实现 Snipaste 的多级右键：
// 1. 若当前正在使用某个标注工具（Tool != ToolNone），右键先退出标注工具回到选区微调；
// 2. 已有选区（Adjusting）时右键退回重新框选/自动识别窗口；
// 3. 还没有选区（Selecting）时右键退出截图。
func (s *Session) RightClick() {
	switch s.phase {
	case PhaseAdjusting:
		if s.annot != nil && s.annot.Tool != annotate.ToolNone {
			s.annot.CancelPointer()
			s.annot.Tool = annotate.ToolNone
			return
		}
		if s.annot != nil && s.annot.Selected >= 0 {
			s.annot.ClearSelection()
			return
		}
		s.sel = image.Rectangle{}
		s.lockedWinID = 0
		s.lockedWinRect = image.Rectangle{}
		s.windowOnly = false
		s.Annotations().Reset()
		s.windowSnap = false
		s.hoverWin = image.Rectangle{}
		s.drag = dragState{owner: -1}
		s.phase = PhaseSelecting
	case PhaseSelecting:
		s.Cancel()
	}
}

// Hover 更新未按下左键时的光标位置；若按 Space 开启了窗口悬停高亮，则按光标位置实时高亮所在应用窗口。
func (s *Session) Hover(gx, gy float64) bool {
	if s.phase != PhaseSelecting && s.phase != PhaseAdjusting {
		return false
	}
	s.cursorX, s.cursorY, s.hasCursor = gx, gy, true
	if s.hasInit {
		dx := gx - s.initX
		dy := gy - s.initY
		if dx*dx+dy*dy < 16 {
			return false
		}
		s.hasInit = false
	}
	if s.phase == PhaseSelecting && s.drag.kind == dragNone && s.windowSnap {
		next := detectWindow(s.windows, s.frames, gx, gy)
		if next != s.hoverWin {
			s.hoverWin = next
			return true
		}
	}
	return false
}

// HitTest 判断全局 DIP 点在当前选区上的交互部位。若开启了标注工具且点在选区内部，优先交给标注画笔（返回 HandleNone -> 十字准星）。
func (s *Session) HitTest(gx, gy float64) Handle {
	if s.phase != PhaseAdjusting || s.sel.Empty() {
		return HandleNone
	}
	if s.drag.kind == dragAnnotate {
		return HandleNone
	}
	if s.drag.kind == dragAnnotateEdit {
		return HandleMove
	}
	if s.drag.kind == dragResize {
		return s.drag.handle
	}
	if s.drag.kind == dragMove {
		return HandleMove
	}
	ac := s.Annotations()
	pt := image.Pt(int(math.Floor(gx)), int(math.Floor(gy)))
	if pt.In(s.sel) {
		if _, part := ac.HitTest(gx, gy); part != annotate.EditNone {
			if ac.Tool == annotate.ToolNone || part == annotate.EditP0 || part == annotate.EditP1 {
				if part == annotate.EditP0 || part == annotate.EditP1 {
					return HandleSE
				}
				return HandleMove
			}
		}
	}
	h := hitHandle(s.sel, gx, gy)
	if h == HandleMove && ac.Tool != annotate.ToolNone {
		return HandleNone
	}
	return h
}

// ActiveRect 返回当前应高亮展示的矩形（框选中、已锁定选区或开启窗口高亮时悬停识别到的窗口），以及是否为已锁定选区。
func (s *Session) ActiveRect() (rect image.Rectangle, locked bool, ok bool) {
	switch s.phase {
	case PhaseSelecting:
		if s.drag.kind == dragBox {
			r := normRect(s.drag.x0, s.drag.y0, s.drag.x1, s.drag.y1)
			if r.Dx() >= minDIP && r.Dy() >= minDIP {
				return r, false, true
			}
		}
		if !s.hoverWin.Empty() {
			return s.hoverWin, false, true
		}
	case PhaseAdjusting:
		if !s.sel.Empty() {
			return s.sel, true, true
		}
	}
	return image.Rectangle{}, false, false
}

// ShowLoupe 报告当前是否应在光标旁显示像素放大镜（仅在拥有冻结像素且未处于测量尺模式时显示）。
func (s *Session) ShowLoupe() bool {
	if s.live || !s.hasCursor || s.measureMode {
		return false
	}
	if s.phase == PhaseSelecting {
		return true
	}
	return s.phase == PhaseAdjusting && s.drag.kind == dragResize
}

// SampleRuler 在冻结模式测量尺开启时，从当前光标向四周发射射线测算间距。
func (s *Session) SampleRuler() (RulerReading, bool) {
	if s.live || !s.measureMode || !s.hasCursor {
		return RulerReading{}, false
	}
	pt := image.Pt(int(math.Floor(s.cursorX)), int(math.Floor(s.cursorY)))
	for i := range s.frames {
		b := s.frames[i].Bounds.Canon()
		sc := s.frames[i].Scale
		if !pt.In(b) || s.frames[i].Pixels == nil || sc <= 0 {
			continue
		}
		ib := s.frames[i].Pixels.Bounds()
		px := int(math.Floor((s.cursorX - float64(b.Min.X)) * sc))
		py := int(math.Floor((s.cursorY - float64(b.Min.Y)) * sc))
		sp, ok := measure.Cast(s.frames[i].Pixels, ib.Min.X+px, ib.Min.Y+py, measure.DefaultTolerance)
		if !ok {
			return RulerReading{}, false
		}
		leftDIP := float64(b.Min.X) + float64(sp.Left-ib.Min.X)/sc
		rightDIP := float64(b.Min.X) + float64(sp.Right+1-ib.Min.X)/sc
		topDIP := float64(b.Min.Y) + float64(sp.Top-ib.Min.Y)/sc
		botDIP := float64(b.Min.Y) + float64(sp.Bottom+1-ib.Min.Y)/sc
		return RulerReading{
			LeftDIP:   leftDIP,
			RightDIP:  rightDIP,
			TopDIP:    topDIP,
			BottomDIP: botDIP,
			OriginX:   s.cursorX,
			OriginY:   s.cursorY,
			WidthDIP:  max(1, int(math.Round(rightDIP-leftDIP))),
			HeightDIP: max(1, int(math.Round(botDIP-topDIP))),
			WidthPx:   sp.WidthPx(),
			HeightPx:  sp.HeightPx(),
		}, true
	}
	return RulerReading{}, false
}

// Cursor 返回最近一次记录的全局 DIP 光标位置。
func (s *Session) Cursor() (gx, gy float64, ok bool) {
	return s.cursorX, s.cursorY, s.hasCursor
}

// SampleLoupe 采样最近一次光标位置附近的冻结像素网格。
func (s *Session) SampleLoupe(radius int) (Loupe, bool) {
	if s.live || !s.hasCursor || (s.phase != PhaseSelecting && s.phase != PhaseAdjusting) {
		return Loupe{}, false
	}
	return sampleLoupe(s.frames, s.cursorX, s.cursorY, radius)
}

// PixelSize 返回选区裁剪后的输出像素宽高。
func (s *Session) PixelSize(sel image.Rectangle) (int, int) {
	if s.phase != PhaseSelecting && s.phase != PhaseAdjusting {
		return 0, 0
	}
	return crop.PixelSize(s.frames, sel)
}

// BeginDrag 从 owner 屏上的全局 DIP 点按下左键。
func (s *Session) BeginDrag(owner int, x, y float64) {
	s.hasInit = false
	s.cursorX, s.cursorY, s.hasCursor = x, y, true
	if s.MeasureMode() {
		if rd, ok := s.SampleRuler(); ok && s.Clip != nil {
			txt := fmt.Sprintf("%d × %d px", rd.WidthPx, rd.HeightPx)
			s.Clip.WriteText(txt)
			if s.OnRulerCopied != nil {
				s.OnRulerCopied(txt)
			}
		}
		return
	}
	switch s.phase {
	case PhaseSelecting:
		if s.colorPickerMode {
			s.CopyColor(false)
			return
		}
		if s.windowSnap {
			s.hoverWin = detectWindow(s.windows, s.frames, x, y)
		}
		s.drag = dragState{kind: dragBox, owner: owner, x0: x, y0: y, x1: x, y1: y}
	case PhaseAdjusting:
		pt := image.Pt(int(math.Floor(x)), int(math.Floor(y)))
		ac := s.Annotations()
		if pt.In(s.sel) {
			// 若处于选择模式（ToolNone）、点击了当前选中图元的首尾控制柄，或点击在已有放大镜内部，优先进入图元二次编辑。
			if idx, part := ac.HitTest(x, y); part != annotate.EditNone {
				items := ac.Items()
				hitExistingLoupe := ac.Tool == annotate.ToolLoupe && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolLoupe
				hitExistingText := ac.Tool == annotate.ToolText && idx >= 0 && idx < len(items) && items[idx].Tool == annotate.ToolText
				if ac.Tool == annotate.ToolNone || part == annotate.EditP0 || part == annotate.EditP1 || hitExistingLoupe || hitExistingText {
					if ac.BeginEdit(x, y) {
						s.drag = dragState{kind: dragAnnotateEdit, owner: owner, x0: x, y0: y, x1: x, y1: y}
						return
					}
				}
			}
			if ac.Tool == annotate.ToolNone {
				ac.ClearSelection()
			}
			if ac.Tool != annotate.ToolNone {
				if ac.Tool == annotate.ToolStep {
					ac.BeginPointer(x, y)
					return
				}
				if ac.BeginPointer(x, y) {
					s.drag = dragState{kind: dragAnnotate, owner: owner, x0: x, y0: y, x1: x, y1: y}
					return
				}
			}
		} else {
			ac.ClearSelection()
		}
		h := hitHandle(s.sel, x, y)
		switch h {
		case HandleMove:
			s.drag = dragState{kind: dragMove, owner: owner, start: s.sel, x0: x, y0: y, x1: x, y1: y}
		case HandleNone:
			s.drag = dragState{kind: dragPendingRedraw, owner: owner, start: s.sel, x0: x, y0: y, x1: x, y1: y}
		default:
			s.drag = dragState{kind: dragResize, owner: owner, handle: h, start: s.sel, x0: x, y0: y, x1: x, y1: y}
		}
	}
}

// MoveDrag 更新同一块屏发起的拖拽位置。
func (s *Session) MoveDrag(owner int, x, y float64) {
	if s.drag.kind == dragNone || s.drag.owner != owner {
		return
	}
	s.cursorX, s.cursorY, s.hasCursor = x, y, true
	s.drag.x1, s.drag.y1 = x, y

	if s.drag.kind == dragAnnotate {
		cx, cy := clampPointToRect(x, y, s.sel)
		s.Annotations().MovePointer(cx, cy)
		return
	}
	if s.drag.kind == dragAnnotateEdit {
		cx, cy := clampPointToRect(x, y, s.sel)
		s.Annotations().MoveEdit(cx, cy)
		return
	}

	dx := roundDIP(x - s.drag.x0)
	dy := roundDIP(y - s.drag.y0)
	union := screensUnion(s.frames)

	switch s.drag.kind {
	case dragMove:
		s.sel = moveRect(s.drag.start, dx, dy, union)
	case dragResize:
		s.sel = resizeByHandle(s.drag.start, s.drag.handle, dx, dy, union)
	case dragPendingRedraw:
		r := normRect(s.drag.x0, s.drag.y0, x, y)
		if r.Dx() >= minDIP && r.Dy() >= minDIP {
			s.drag.restore = &redrawBackup{
				lockedWinID:   s.lockedWinID,
				lockedWinRect: s.lockedWinRect,
				windowOnly:    s.windowOnly,
				items:         s.Annotations().Items(),
			}
			s.phase = PhaseSelecting
			s.sel = image.Rectangle{}
			s.lockedWinID = 0
			s.lockedWinRect = image.Rectangle{}
			s.windowOnly = false
			s.Annotations().Reset()
			s.drag.kind = dragBox
		}
	}
}

// EndDrag 结束左键拖拽。
func (s *Session) EndDrag() {
	if s.drag.kind == dragNone {
		return
	}
	d := s.drag
	s.drag = dragState{owner: -1}
	union := screensUnion(s.frames)

	switch d.kind {
	case dragAnnotate:
		cx, cy := clampPointToRect(d.x1, d.y1, s.sel)
		s.Annotations().EndPointer(cx, cy)
	case dragAnnotateEdit:
		cx, cy := clampPointToRect(d.x1, d.y1, s.sel)
		s.Annotations().MoveEdit(cx, cy)
		s.Annotations().EndEdit()
	case dragBox:
		r := normRect(d.x0, d.y0, d.x1, d.y1)
		if !union.Empty() {
			r = r.Intersect(union)
		}
		if r.Dx() >= minDIP && r.Dy() >= minDIP {
			s.sel = r
			s.lockedWinID = 0
			s.lockedWinRect = image.Rectangle{}
			s.windowOnly = false
			s.phase = PhaseAdjusting
			if s.autoOCR {
				s.OCR()
			}
			return
		}
		win := s.hoverWin
		if !s.windowSnap {
			win = detectWindow(s.windows, s.frames, d.x0, d.y0)
		}
		if !win.Empty() {
			s.sel = win
			s.lockedWinRect = win
			s.lockedWinID = s.winIDByRect[win]
			s.phase = PhaseAdjusting
			if s.autoOCR {
				s.OCR()
			}
			return
		}
	case dragMove, dragResize, dragPendingRedraw:
		if s.sel.Empty() {
			s.Cancel()
		}
	}
}

func clampPointToRect(x, y float64, r image.Rectangle) (float64, float64) {
	if r.Empty() {
		return x, y
	}
	return max(float64(r.Min.X), min(x, float64(r.Max.X))),
		max(float64(r.Min.Y), min(y, float64(r.Max.Y)))
}

// Nudge 用方向键微调选区：resize 为假时整体平移，为真时调整右下角尺寸。
func (s *Session) Nudge(dx, dy int, resize bool) bool {
	if s.phase != PhaseAdjusting || s.sel.Empty() {
		return false
	}
	union := screensUnion(s.frames)
	if resize {
		s.sel = resizeByHandle(s.sel, HandleSE, dx, dy, union)
	} else {
		s.sel = moveRect(s.sel, dx, dy, union)
	}
	return true
}

func (s *Session) fail(msg string) {
	s.phase = PhaseIdle
	s.live = false
	s.colorPickerMode = false
	s.preservePicked = false
	s.enterMeasureOnDeliver = false
	s.dropFrames()
	s.drag = dragState{owner: -1}
	s.status = msg
	s.UI.SetStatus(msg)
	s.UI.HideSelection()
}

func (s *Session) clearSelection() {
	s.phase = PhaseIdle
	s.live = false
	s.autoOCR = false
	s.measureMode = false
	s.colorPickerMode = false
	s.preservePicked = false
	s.enterMeasureOnDeliver = false
	s.pickedColors = nil
	s.dropFrames()
	s.hoverWin = image.Rectangle{}
	s.lockedWinID = 0
	s.lockedWinRect = image.Rectangle{}
	s.sel = image.Rectangle{}
	if s.annot != nil {
		s.annot.Reset()
	}
	s.drag = dragState{owner: -1}
	s.hasInit = false
	s.hasCursor = false
	s.UI.HideSelection()
}

func (s *Session) dropFrames() {
	for i := range s.frames {
		s.frames[i].Pixels = nil
	}
	s.frames = nil
	s.windows = nil
	s.winIDByRect = nil
}

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
