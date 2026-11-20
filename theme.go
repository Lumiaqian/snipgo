package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"snipgo/internal/config"
)

const (
	panelCornerRadius = 26
	cardCornerRadius  = 18
	inputCornerRadius = 12
)

// Apple 设计规范一致的流体物理弹簧与色彩渐变动效预设。
var (
	springPanel  = ui.Animation{Spring: ui.SpringOptions{Duration: 240 * time.Millisecond, Bounce: 0}}
	springSnappy = ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.08}}
	springSmooth = ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond, Bounce: 0}}
	springPop    = ui.Animation{Spring: ui.SpringOptions{Duration: 190 * time.Millisecond, Bounce: 0.08}}
	colorMorph   = ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Oklab: true, NonMotion: true}
)

// appThemeState 保存实时生效的外观模式、液态玻璃浓度与主题强调色。
type appThemeState struct {
	Appearance string // "system", "light", "dark"
	GlassLevel string // "clear", "regular", "frosted"
	Accent     string // "system" 或 accentOptions 中的 ID
}

type accentOption struct {
	ID    string
	Name  string
	Light ui.Color
	Dark  ui.Color
}

var accentOptions = []accentOption{
	{ID: "system", Name: "自动", Light: ui.RGB(0, 122, 255), Dark: ui.RGB(10, 132, 255)},
	{ID: "blue", Name: "海湾蓝", Light: ui.RGB(0, 122, 255), Dark: ui.RGB(10, 132, 255)},
	{ID: "purple", Name: "极光紫", Light: ui.RGB(142, 68, 224), Dark: ui.RGB(175, 108, 255)},
	{ID: "pink", Name: "珊瑚粉", Light: ui.RGB(232, 62, 126), Dark: ui.RGB(255, 92, 148)},
	{ID: "orange", Name: "暖日橙", Light: ui.RGB(235, 108, 12), Dark: ui.RGB(255, 149, 42)},
	{ID: "green", Name: "翡翠绿", Light: ui.RGB(24, 162, 76), Dark: ui.RGB(48, 209, 88)},
	{ID: "teal", Name: "松石青", Light: ui.RGB(0, 148, 168), Dark: ui.RGB(48, 198, 214)},
	{ID: "graphite", Name: "石墨灰", Light: ui.RGB(94, 102, 114), Dark: ui.RGB(142, 150, 164)},
}

var (
	themeMu     sync.RWMutex
	activeTheme = appThemeState{
		Appearance: "system",
		GlassLevel: "regular",
		Accent:     "system",
	}
)

func getActiveTheme() appThemeState {
	themeMu.RLock()
	st := activeTheme
	themeMu.RUnlock()
	return st
}

func setActiveTheme(st appThemeState) {
	if st.Appearance == "" {
		st.Appearance = "system"
	}
	if st.GlassLevel == "" {
		st.GlassLevel = "regular"
	}
	if st.Accent == "" {
		st.Accent = "system"
	}
	themeMu.Lock()
	activeTheme = st
	themeMu.Unlock()
}

func themeStateFromConfig(cfg config.Settings) appThemeState {
	st := appThemeState{
		Appearance: cfg.ThemeAppearance,
		GlassLevel: cfg.GlassLevel,
		Accent:     cfg.AccentColor,
	}
	if st.Appearance == "" {
		st.Appearance = "system"
	}
	if st.GlassLevel == "" {
		st.GlassLevel = "regular"
	}
	if st.Accent == "" {
		st.Accent = "system"
	}
	return st
}

func accentColorFor(id string, dark bool) (ui.Color, bool) {
	if id == "" || id == "system" {
		return ui.Color{}, false
	}
	for _, opt := range accentOptions {
		if opt.ID == id {
			if dark {
				return opt.Dark, true
			}
			return opt.Light, true
		}
	}
	return ui.Color{}, false
}

func vibrancyForGlassLevel(level string) mygo.Vibrancy {
	if level == "clear" {
		return mygo.VibrancyGlassClear
	}
	return mygo.VibrancyGlass
}

func activeGlassStyle() glass.Style {
	if getActiveTheme().GlassLevel == "clear" {
		return glass.Clear
	}
	return glass.Regular
}

func effectiveDarkMode(st appThemeState) bool {
	switch st.Appearance {
	case "dark":
		return true
	case "light":
		return false
	default:
		return mygo.App.IsReady() && mygo.Theme.IsDark()
	}
}

func vibrancyTintForMode(level string, dark bool) string {
	switch level {
	case "clear":
		return ""
	case "frosted":
		if dark {
			return "rgba(18, 20, 28, 0.46)"
		}
		return "rgba(248, 250, 255, 0.44)"
	default:
		if dark {
			return "rgba(18, 20, 28, 0.16)"
		}
		return ""
	}
}

func vibrancyTintFor(st appThemeState) string {
	return vibrancyTintForMode(st.GlassLevel, effectiveDarkMode(st))
}

// applyWindowTheme 为原生浮窗同步 macOS 26/27 玻璃浓度、玻璃着色与浅/深外观。
func applyWindowTheme(w *mygo.Window, st appThemeState) {
	if w == nil || w.IsDestroyed() {
		return
	}
	w.SetVibrancy(vibrancyForGlassLevel(st.GlassLevel))
	_ = w.SetVibrancyTint(vibrancyTintFor(st))
	w.SetActiveAppearance(true)
	applyWindowAppearance(w, st.Appearance)
	w.Invalidate()
}

// withAppTheme 将用户配置的外观（跟随系统/浅色/深色）与强调色注入子树上下文，
// 同时保持根容器背景透明以不遮挡底层壁纸或原生 NSGlassEffectView。
func withAppTheme(c *ui.Context, fn func()) {
	st := getActiveTheme()
	base := c.Theme()
	switch st.Appearance {
	case "light":
		base = ui.LightTheme()
	case "dark":
		base = ui.DarkTheme()
	}
	t := *base
	if custom, ok := accentColorFor(st.Accent, t.Dark); ok {
		black := ui.RGB(0, 0, 0)
		t.Accent = custom
		t.AccentHover = custom.Mix(black, 0.08)
		t.AccentPressed = custom.Mix(black, 0.16)
		t.Selection = custom.Alpha(0.24)
		t.Focus = custom.Alpha(0.55)
	}
	ui.Themed(c, &t).Fill().Background(ui.Transparent).Children(fn)
}

// palette 统一管理 macOS 26/27 (Tahoe) Liquid Glass 在浅色、深色、不同玻璃浓度与自定义主题色下的语义色值。
type palette struct {
	Dark           bool
	Text           ui.Color
	TextSecondary  ui.Color
	TextMuted      ui.Color
	Accent         ui.Color
	AccentText     ui.Color
	PanelRim       ui.Color
	CardBg         ui.Color
	CardBgHover    ui.Color
	CardBorder     ui.Color
	CardTopRim     ui.Color
	ActiveBg       ui.Color
	ActiveBorder   ui.Color
	ActiveText     ui.Color
	InputBg        ui.Color
	InputBorder    ui.Color
	Divider        ui.Color
	SubtleBtnBg    ui.Color
	SubtleBtnHover ui.Color
	SubtleBorder   ui.Color
	BadgeSuccessBg ui.Color
	BadgeSuccessBd ui.Color
	BadgeSuccessFg ui.Color
	BadgeInfoBg    ui.Color
	BadgeInfoBd    ui.Color
	BadgeInfoFg    ui.Color
	StageBg        ui.Color
	StageDot       ui.Color
}

func uiPalette(c *ui.Context) palette {
	t := c.Theme()
	st := getActiveTheme()
	dark := t.Dark

	accent := t.Accent
	if custom, ok := accentColorFor(st.Accent, dark); ok {
		accent = custom
	}
	if accent.A == 0 {
		if dark {
			accent = ui.RGB(10, 132, 255)
		} else {
			accent = ui.RGB(0, 122, 255)
		}
	}

	white := ui.RGB(255, 255, 255)
	black := ui.RGB(0, 0, 0)

	if dark {
		cardBg := ui.RGBA(255, 255, 255, 0.065)
		cardBgHover := ui.RGBA(255, 255, 255, 0.11)
		cardBorder := ui.RGBA(255, 255, 255, 0.10)
		inputBg := ui.RGBA(0, 0, 0, 0.22)
		stageBg := ui.RGBA(12, 14, 20, 0.48)
		switch st.GlassLevel {
		case "clear":
			cardBg = ui.RGBA(255, 255, 255, 0.045)
			cardBgHover = ui.RGBA(255, 255, 255, 0.085)
			cardBorder = ui.RGBA(255, 255, 255, 0.09)
			inputBg = ui.RGBA(0, 0, 0, 0.14)
			stageBg = ui.RGBA(12, 14, 20, 0.32)
		case "frosted":
			cardBg = ui.RGBA(255, 255, 255, 0.105)
			cardBgHover = ui.RGBA(255, 255, 255, 0.155)
			cardBorder = ui.RGBA(255, 255, 255, 0.14)
			inputBg = ui.RGBA(0, 0, 0, 0.30)
			stageBg = ui.RGBA(12, 14, 20, 0.62)
		}
		return palette{
			Dark:           true,
			Text:           ui.RGBA(246, 248, 252, 0.95),
			TextSecondary:  ui.RGBA(224, 228, 238, 0.76),
			TextMuted:      ui.RGBA(198, 206, 220, 0.54),
			Accent:         accent,
			AccentText:     white,
			PanelRim:       ui.RGBA(255, 255, 255, 0.11),
			CardBg:         cardBg,
			CardBgHover:    cardBgHover,
			CardBorder:     cardBorder,
			CardTopRim:     ui.RGBA(255, 255, 255, 0.10),
			ActiveBg:       accent.Alpha(0.30),
			ActiveBorder:   accent.Mix(white, 0.34).Alpha(0.72),
			ActiveText:     accent.Mix(white, 0.72),
			InputBg:        inputBg,
			InputBorder:    ui.RGBA(255, 255, 255, 0.10),
			Divider:        ui.RGBA(255, 255, 255, 0.08),
			SubtleBtnBg:    ui.RGBA(255, 255, 255, 0.07),
			SubtleBtnHover: ui.RGBA(255, 255, 255, 0.13),
			SubtleBorder:   ui.RGBA(255, 255, 255, 0.13),
			BadgeSuccessBg: ui.RGBA(48, 209, 88, 0.14),
			BadgeSuccessBd: ui.RGBA(48, 209, 88, 0.24),
			BadgeSuccessFg: ui.RGB(122, 242, 166),
			BadgeInfoBg:    accent.Alpha(0.16),
			BadgeInfoBd:    accent.Mix(white, 0.32).Alpha(0.32),
			BadgeInfoFg:    accent.Mix(white, 0.48),
			StageBg:        stageBg,
			StageDot:       ui.RGBA(255, 255, 255, 0.09),
		}
	}

	cardBg := ui.RGBA(255, 255, 255, 0.58)
	cardBgHover := ui.RGBA(255, 255, 255, 0.78)
	cardBorder := ui.RGBA(0, 0, 0, 0.08)
	inputBg := ui.RGBA(0, 0, 0, 0.055)
	stageBg := ui.RGBA(236, 240, 247, 0.58)
	switch st.GlassLevel {
	case "clear":
		cardBg = ui.RGBA(255, 255, 255, 0.36)
		cardBgHover = ui.RGBA(255, 255, 255, 0.58)
		cardBorder = ui.RGBA(0, 0, 0, 0.075)
		inputBg = ui.RGBA(255, 255, 255, 0.42)
		stageBg = ui.RGBA(236, 240, 247, 0.38)
	case "frosted":
		cardBg = ui.RGBA(255, 255, 255, 0.78)
		cardBgHover = ui.RGBA(255, 255, 255, 0.92)
		cardBorder = ui.RGBA(0, 0, 0, 0.10)
		inputBg = ui.RGBA(0, 0, 0, 0.065)
		stageBg = ui.RGBA(236, 240, 247, 0.78)
	}
	return palette{
		Dark:           false,
		Text:           ui.RGBA(20, 22, 28, 0.92),
		TextSecondary:  ui.RGBA(42, 46, 56, 0.72),
		TextMuted:      ui.RGBA(60, 64, 76, 0.56),
		Accent:         accent,
		AccentText:     white,
		PanelRim:       ui.RGBA(255, 255, 255, 0.45),
		CardBg:         cardBg,
		CardBgHover:    cardBgHover,
		CardBorder:     cardBorder,
		CardTopRim:     ui.RGBA(255, 255, 255, 0.65),
		ActiveBg:       accent.Alpha(0.16),
		ActiveBorder:   accent.Alpha(0.68),
		ActiveText:     accent.Mix(black, 0.18),
		InputBg:        inputBg,
		InputBorder:    ui.RGBA(0, 0, 0, 0.11),
		Divider:        ui.RGBA(0, 0, 0, 0.09),
		SubtleBtnBg:    ui.RGBA(0, 0, 0, 0.055),
		SubtleBtnHover: ui.RGBA(0, 0, 0, 0.105),
		SubtleBorder:   ui.RGBA(0, 0, 0, 0.09),
		BadgeSuccessBg: ui.RGBA(34, 197, 94, 0.15),
		BadgeSuccessBd: ui.RGBA(22, 163, 74, 0.30),
		BadgeSuccessFg: ui.RGB(18, 124, 62),
		BadgeInfoBg:    accent.Alpha(0.14),
		BadgeInfoBd:    accent.Alpha(0.28),
		BadgeInfoFg:    accent.Mix(black, 0.22),
		StageBg:        stageBg,
		StageDot:       ui.RGBA(0, 0, 0, 0.095),
	}
}

// liquidGlassPanel 为主浮窗面板应用 macOS 26/27 Liquid Glass 表面并响应“清透/标准/浓磨砂”三档浓度。
func liquidGlassPanel(c *ui.Context, el ui.Element) ui.Element {
	p := uiPalette(c)
	st := getActiveTheme()
	el.Fill().Radius(panelCornerRadius).TextColor(p.Text)
	if c.Vibrancy() {
		return el.Border(1, p.PanelRim)
	}
	switch st.GlassLevel {
	case "clear":
		return el.Material(glass.Glass{Style: glass.Clear, Clarity: 0.78}).
			Border(1, p.PanelRim).InsetShadow(0, 1, 0, 0, p.CardTopRim)
	case "frosted":
		tint := ui.RGBA(255, 255, 255, 0.44)
		if p.Dark {
			tint = ui.RGBA(20, 22, 30, 0.50)
		}
		return el.Material(glass.Glass{Style: glass.Regular, Clarity: 0, Tint: tint}).
			Border(1, p.PanelRim).InsetShadow(0, 1, 0, 0, p.CardTopRim)
	default:
		return el.Material(glass.Glass{Style: glass.Regular, Clarity: 0.30}).
			Border(1, p.PanelRim).InsetShadow(0, 1, 0, 0, p.CardTopRim)
	}
}

type panelEnterState struct {
	enabled bool
	started bool
}

// liquidPanelEnter 为真实弹窗提供 Apple 临界阻尼弹簧入场动效（未启用时直接以终态渲染）。
func liquidPanelEnter(el ui.Element, state *panelEnterState) ui.Element {
	if state == nil || !state.enabled {
		return el
	}
	if !state.started {
		state.started = true
		el.Jump("panel-enter", 0)
	}
	t := el.AnimateTo("panel-enter", 1, springPanel)
	sc := 0.975 + 0.025*t
	dy := (1 - t) * 6
	op := min(float32(1), max(float32(0), t))
	return el.Scale(sc, sc).Translate(0, dy).Opacity(op)
}

// liquidGlassCapsule 为浮动胶囊、工具栏、取色徽章与贴图浮层应用 macOS 26/27 液态玻璃胶囊。
func liquidGlassCapsule(c *ui.Context, el ui.Element, radius float32, native bool) ui.Element {
	el.Radius(radius).Material(glass.Glass{
		Style:  activeGlassStyle(),
		Native: native,
	})
	if !native {
		p := uiPalette(c)
		shadowCol := ui.RGBA(0, 0, 0, 0.14)
		if p.Dark {
			shadowCol = ui.RGBA(0, 0, 0, 0.34)
		}
		el.Border(1, p.PanelRim).Shadow(0, 4, 14, 0, shadowCol)
	}
	return el
}

// liquidGlassGroup 将相邻操作胶囊包裹在 macOS 26 风格的融合液态玻璃组内。
func liquidGlassGroup(c *ui.Context, radius, gap float32, fn func(add func(ui.Element) ui.Element)) {
	if c.Vibrancy() {
		row := ui.Row(c).Gap(gap).AlignItems(ui.Center)
		row.Children(func() {
			fn(func(e ui.Element) ui.Element { return e })
		})
		return
	}
	p := uiPalette(c)
	tint := ui.RGBA(255, 255, 255, 0.16)
	if p.Dark {
		tint = ui.RGBA(20, 22, 30, 0.24)
	}
	grp := glass.NewGroup(c)
	grp.Style = activeGlassStyle()
	grp.Tint = tint
	grp.Radius = radius
	grp.Merge = 22
	grp.Gap(gap).AlignItems(ui.Center).Children(func() {
		fn(func(e ui.Element) ui.Element { return grp.Pane(e) })
	})
}

// glassGroupBtn 绘制位于融合液态玻璃组内的次级操作按钮；在系统原生 vibrancy 浮窗下自动退化为原生 NSGlassEffectView 胶囊。
func glassGroupBtn(c *ui.Context, add func(ui.Element) ui.Element, ic *ui.SVG, label, tip string) bool {
	if c.Vibrancy() {
		return actionPillBtn(c, ic, label, tip, false, true)
	}
	p := uiPalette(c)
	btn := add(ui.Row(c).
		Label(label).
		Tooltip(tip).
		Height(26).
		Padding(0, 9).
		Gap(4).
		AlignItems(ui.Center).
		Radius(13).
		Shrink(0).
		Cursor(ui.CursorPointer))
	targetScale := float32(1.0)
	if btn.Pressed() {
		targetScale = 0.94
	}
	if sc := btn.AnimateTo("gb-sc", targetScale, springPop); sc != 1.0 {
		btn.Scale(sc, sc)
	}
	bgTarget := ui.Transparent
	if btn.Hovered() {
		bgTarget = p.SubtleBtnHover
	}
	btn.Background(btn.AnimateColor("gb-bg", bgTarget, colorMorph)).
		Border(1, p.SubtleBorder).
		InsetShadow(0, 1, 0, 0, p.CardTopRim).
		Children(func() {
			if ic != nil {
				ui.Icon(c, ic).FontSize(14).TextColor(p.Text)
			}
			ui.Text(c, label).FontSize(12).FontWeight(500).NoWrap().TextColor(p.Text)
		})
	return btn.Clicked()
}

// liquidPrimaryChrome 为主要确认操作应用 macOS 26/27 带交互弹性的着色 Liquid Glass 胶囊。
func liquidPrimaryChrome(c *ui.Context, el ui.Element, radius float32) ui.Element {
	p := uiPalette(c)
	col := el.AnimateColor("prim-bg", p.Accent, colorMorph)
	return el.Radius(radius).
		Material(glass.Glass{
			Style:       activeGlassStyle(),
			Tint:        col,
			Interactive: true,
		}).
		InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.24))
}

// liquidSecondaryChrome 为次级胶囊按钮应用真实 Liquid Glass（浮窗下启用原生 NSGlassEffectView）与弹簧高光。
func liquidSecondaryChrome(c *ui.Context, el ui.Element, hovered bool, radius float32) ui.Element {
	p := uiPalette(c)
	if c.Vibrancy() {
		tint := ui.RGBA(255, 255, 255, 0.28)
		if p.Dark {
			tint = ui.RGBA(255, 255, 255, 0.06)
		}
		if hovered {
			if p.Dark {
				tint = ui.RGBA(255, 255, 255, 0.14)
			} else {
				tint = ui.RGBA(255, 255, 255, 0.48)
			}
		}
		animTint := el.AnimateColor("sec-tint", tint, colorMorph)
		return el.Radius(radius).
			Material(glass.Glass{
				Style:  activeGlassStyle(),
				Tint:   animTint,
				Native: true,
			})
	}
	tint := ui.RGBA(255, 255, 255, 0.24)
	if p.Dark {
		tint = ui.RGBA(255, 255, 255, 0.08)
	}
	if hovered {
		if p.Dark {
			tint = ui.RGBA(255, 255, 255, 0.16)
		} else {
			tint = ui.RGBA(255, 255, 255, 0.42)
		}
	}
	return el.Radius(radius).
		Material(glass.Glass{
			Style:       activeGlassStyle(),
			Tint:        tint,
			Interactive: true,
		}).
		Border(1, p.SubtleBorder).
		InsetShadow(0, 1, 0, 0, p.CardTopRim)
}

type segMatchKey struct{ id string }

// liquidSegmented 绘制 macOS 26 风格可滑动 Liquid Glass 分段选择器，返回被点击项索引（未点击返回 -1）。
func liquidSegmented(c *ui.Context, groupID string, selected int, labels []string) int {
	return renderSegmented(c, groupID, selected, labels, false)
}

// compactSegmented 绘制紧凑型（26px 高）macOS 26 风格可滑动 Liquid Glass 分段选择器，用于二级属性栏与视图切换。
func compactSegmented(c *ui.Context, groupID string, selected int, labels []string) int {
	return renderSegmented(c, groupID, selected, labels, true)
}

func renderSegmented(c *ui.Context, groupID string, selected int, labels []string, compact bool) int {
	p := uiPalette(c)
	clicked := -1
	trackH, pad, optH, optPadX, fs := float32(30), float32(3), float32(24), float32(11), float32(11.5)
	if compact {
		trackH, pad, optH, optPadX, fs = 26, 2, 22, 9, 11
	}
	trackR := trackH * 0.5
	optR := optH * 0.5

	trackBg := ui.RGBA(0, 0, 0, 0.055)
	trackBd := ui.RGBA(0, 0, 0, 0.05)
	if p.Dark {
		trackBg = ui.RGBA(255, 255, 255, 0.07)
		trackBd = ui.RGBA(255, 255, 255, 0.07)
	}
	track := ui.Row(c.Key(groupID+"-track")).
		Height(trackH).
		Padding(pad, pad).
		Gap(2).
		AlignItems(ui.Center).
		Radius(trackR).
		Background(trackBg).
		Border(1, trackBd).
		Shrink(0)
	track.Children(func() {
		for i, lbl := range labels {
			i, lbl := i, lbl
			active := i == selected
			opt := ui.Box(c.Key(fmt.Sprintf("%s-opt-%d", groupID, i))).
				Label(lbl).
				Height(optH).
				Radius(optR).
				Cursor(ui.CursorPointer).
				Shrink(0)
			if opt.Pressed() {
				sc := opt.AnimateTo("seg-sc", 0.96, springPop)
				opt.Scale(sc, sc)
			} else {
				sc := opt.AnimateTo("seg-sc", 1.0, springPop)
				if sc < 0.999 {
					opt.Scale(sc, sc)
				}
			}
			if !active && opt.Hovered() {
				hovBg := ui.RGBA(0, 0, 0, 0.04)
				if p.Dark {
					hovBg = ui.RGBA(255, 255, 255, 0.07)
				}
				opt.Background(hovBg)
			}
			opt.Children(func() {
				if active {
					thumbBg := ui.RGBA(255, 255, 255, 0.96)
					thumbBd := ui.RGBA(0, 0, 0, 0.07)
					shadowCol := ui.RGBA(0, 0, 0, 0.10)
					topRim := ui.RGBA(255, 255, 255, 0.72)
					if p.Dark {
						thumbBg = ui.RGBA(255, 255, 255, 0.19)
						thumbBd = ui.RGBA(255, 255, 255, 0.15)
						shadowCol = ui.RGBA(0, 0, 0, 0.28)
						topRim = ui.RGBA(255, 255, 255, 0.22)
					}
					ui.Box(c.Key(fmt.Sprintf("%s-thumb-%d", groupID, i))).
						Absolute().Fill().
						Radius(optR).
						Background(thumbBg).
						Border(1, thumbBd).
						InsetShadow(0, 1, 0, 0, topRim).
						Shadow(0, 1.5, 4, 0, shadowCol).
						ZIndex(0).
						IgnoreInput(true).
						Transition(ui.ElementTransition{
							Match:     segMatchKey{id: groupID},
							Animation: &springSnappy,
						})
				}
				txtCol := p.TextSecondary
				fw := 500
				if active {
					if p.Dark {
						txtCol = ui.RGB(255, 255, 255)
					} else {
						txtCol = p.Text
					}
					fw = 600
				} else if opt.Hovered() {
					txtCol = p.Text
				}
				ui.Box(c).FillHeight().Padding(0, optPadX).AlignItems(ui.Center).Justify(ui.Center).ZIndex(1).Children(func() {
					ui.Text(c, lbl).FontSize(fs).FontWeight(fw).NoWrap().TextColor(txtCol)
				})
			})
			if opt.Clicked() {
				clicked = i
			}
		}
	})
	return clicked
}

// liquidSwitch 绘制带物理弹簧拉伸胶囊旋钮与 Oklab 变色的 macOS 26 开关控件。
func liquidSwitch(c *ui.Context, key string, on *bool, label string) bool {
	p := uiPalette(c)
	toggled := false
	row := ui.Row(c.Key("sw-row-" + key)).
		Label(label).
		AlignItems(ui.Center).
		Gap(12).
		Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Text(c, label).FontSize(12.5).Grow(1).TextColor(p.Text)
		val := on != nil && *on
		knobBox := ui.Box(c.Key("sw-knob-"+key)).Size(42, 24).Radius(12).Shrink(0)
		targetTrack := p.InputBg
		targetBorder := p.InputBorder
		if val {
			targetTrack = p.Accent
			targetBorder = p.Accent.Alpha(0.90)
		}
		trackCol := knobBox.AnimateColor("sw-tr", targetTrack, colorMorph)
		borderCol := knobBox.AnimateColor("sw-bd", targetBorder, colorMorph)
		pos := knobBox.AnimateTo("sw-pos", boolFloat(val), springSnappy)
		stretch := knobBox.AnimateTo("sw-press", boolFloat(row.Pressed()), springPop)
		knobBox.Background(trackCol).Border(1, borderCol).InsetShadow(0, 1, 0, 0, p.CardTopRim)
		knobBox.Draw(func(painter *ui.Painter, r ui.Rect) {
			const pad = float32(2.5)
			h := r.H - pad*2
			w := h + 5 + stretch*4
			travel := max(float32(0), r.W-pad*2-w)
			kx := r.X + pad + travel*min(1, max(0, pos))
			ky := r.Y + pad
			pill := ui.Rect{X: kx, Y: ky, W: w, H: h}
			painter.Shadow(pill, h/2, 0, 1.5, 4, 0, ui.RGBA(0, 0, 0, 0.24))
			fill := ui.RGB(255, 255, 255)
			if stretch > 0.1 {
				fill = ui.RGBA(255, 255, 255, 0.92)
			}
			painter.Fill(pill, fill, h/2)
		})
	})
	if row.Clicked() && on != nil {
		*on = !*on
		toggled = true
	}
	return toggled
}

func boolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// liquidActiveChipChrome 为激活态工具/标签胶囊应用高亮主题色边缘。
func liquidActiveChipChrome(c *ui.Context, el ui.Element, radius float32) ui.Element {
	p := uiPalette(c)
	return el.Radius(radius).
		Background(p.ActiveBg).
		Border(1, p.ActiveBorder).
		InsetShadow(0, 1, 0, 0, p.CardTopRim)
}

// liquidCard 为面板内部信息卡片应用自适应浅/深磨砂圆角卡片材质（16px 同心圆角）。
func liquidCard(c *ui.Context, el ui.Element) ui.Element {
	p := uiPalette(c)
	return el.
		Radius(cardCornerRadius).
		Background(p.CardBg).
		Border(1, p.CardBorder).
		InsetShadow(0, 1, 0, 0, p.CardTopRim)
}

func scrollEdgeBlurRadius() float32 {
	switch getActiveTheme().GlassLevel {
	case "clear":
		return 7
	case "frosted":
		return 14
	default:
		return 10
	}
}

// paintTopScrollEdge 在浮动标题栏下方铺设 macOS 26/27 渐进式滚动边缘模糊。
func paintTopScrollEdge(c *ui.Context, h float32) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(h).PassThrough().
		Material(glass.Blur{
			Radius: scrollEdgeBlurRadius(),
			Mask:   &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180, Start: 0.42, End: 1},
		})
}

// paintBottomScrollEdge 在浮动底部操作栏上方铺设渐进式滚动边缘模糊。
func paintBottomScrollEdge(c *ui.Context, h float32) {
	ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(h).PassThrough().
		Material(glass.Blur{
			Radius: scrollEdgeBlurRadius(),
			Mask:   &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 0, Start: 0.42, End: 1},
		})
}

// panelHairline 绘制 1px 自适应细分隔线。
func panelHairline(c *ui.Context) {
	ui.Box(c).Height(1).Background(uiPalette(c).Divider)
}

// panelVHairline 绘制纵向 1px 自适应细分隔线。
func panelVHairline(c *ui.Context) {
	ui.Box(c).Width(1).Background(uiPalette(c).Divider)
}

// macTrafficCloseBtn 绘制 macOS 原生风格左上角红色关闭圆钮（常显清晰 × 符号，带弹簧缩放与高光）。
func macTrafficCloseBtn(c *ui.Context, label, tip string) bool {
	btn := ui.Box(c).
		Label(label).
		Tooltip(tip).
		Size(20, 20).
		Shrink(0).
		Cursor(ui.CursorPointer)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	targetScale := float32(1.0)
	if pressed {
		targetScale = 0.93
	} else if hovered {
		targetScale = 1.06
	}
	sc := btn.AnimateTo("traffic-scale", targetScale, springPop)
	if sc != 1.0 {
		btn.Scale(sc, sc)
	}
	fillTarget := ui.RGB(255, 95, 87)
	glyphTarget := ui.RGB(128, 22, 18)
	if pressed {
		fillTarget = ui.RGB(224, 68, 62)
		glyphTarget = ui.RGB(66, 6, 4)
	} else if hovered {
		fillTarget = ui.RGB(255, 82, 74)
		glyphTarget = ui.RGB(66, 6, 4)
	}
	fill := btn.AnimateColor("traffic-fill", fillTarget, colorMorph)
	glyph := btn.AnimateColor("traffic-glyph", glyphTarget, colorMorph)
	btn.Draw(func(painter *ui.Painter, r ui.Rect) {
		const d = float32(14)
		rad := d * 0.5
		cx := r.X + r.W*0.5
		cy := r.Y + r.H*0.5
		circle := ui.Rect{X: cx - rad, Y: cy - rad, W: d, H: d}

		painter.Fill(circle, fill, rad)
		painter.Stroke(circle, ui.RGBA(0, 0, 0, 0.22), rad, 0.75)

		const off = float32(2.8)
		painter.StrokePolyline([][2]float32{
			{cx - off, cy - off},
			{cx + off, cy + off},
		}, 1.5, glyph)
		painter.StrokePolyline([][2]float32{
			{cx + off, cy - off},
			{cx - off, cy + off},
		}, 1.5, glyph)
	})
	return btn.Clicked()
}

// handlePanelWindowShortcuts 统一处理 macOS 面板快捷键：Cmd+Q 退出应用，Esc / Cmd+W 关闭当前窗口。
func handlePanelWindowShortcuts(c *ui.Context, onClose func()) bool {
	if c.Shortcut(ui.Cmd, ui.KeyQ) {
		go mygo.RunOnMain(func() { mygo.App.Quit() })
		return true
	}
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if onClose != nil {
			go mygo.RunOnMain(onClose)
		}
		return true
	}
	return false
}

// statusBadgePill 绘制自适应深浅模式的圆角胶囊状态徽标（带平滑内容形变与渐隐进出动效）。
func statusBadgePill(c *ui.Context, text string, success bool) {
	if text == "" {
		return
	}
	p := uiPalette(c)
	bg, bd, fg := p.BadgeInfoBg, p.BadgeInfoBd, p.BadgeInfoFg
	if success {
		bg, bd, fg = p.BadgeSuccessBg, p.BadgeSuccessBd, p.BadgeSuccessFg
	}
	pill := ui.Row(c.Key("status-badge-pill")).
		Height(22).Padding(0, 9).Gap(4).AlignItems(ui.Center).Radius(11).
		Transition(ui.ElementTransition{
			Animation: &springSnappy,
			Enter:     &ui.Motion{Scale: 0.88, Blur: 3},
			Exit:      &ui.Motion{Scale: 0.88, Blur: 3},
		})
	animBg := pill.AnimateColor("sb-bg", bg, colorMorph)
	animBd := pill.AnimateColor("sb-bd", bd, colorMorph)
	animFg := pill.AnimateColor("sb-fg", fg, colorMorph)
	pill.Background(animBg).
		Border(1, animBd).
		Children(func() {
			ui.Text(c, text).FontSize(11).FontWeight(500).NoWrap().
				TextColor(animFg).
				ContentTransition(ui.ContentTransition{Morph: true})
		})
}

// paintStudioDotBackdrop 在模板预览舞台背景铺设自适应浅/深建筑点阵网格。
func paintStudioDotBackdrop(p *ui.Painter, r ui.Rect) {
	const step = float32(16)
	dotCol := ui.RGBA(0, 0, 0, 0.095)
	if p.Theme().Dark {
		dotCol = ui.RGBA(255, 255, 255, 0.085)
	}
	for y := r.Y + 12; y < r.Y+r.H-8; y += step {
		for x := r.X + 12; x < r.X+r.W-8; x += step {
			p.Fill(ui.Rect{X: x, Y: y, W: 1.5, H: 1.5}, dotCol, 0.75)
		}
	}
}
