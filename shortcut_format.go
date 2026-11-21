package main

import (
	"fmt"
	"snipgo/internal/config"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *app) shortcutRecorderRow(c *ui.Context, p palette, sw *settingsWindow, def shortcutItemDef, defCfg config.Settings) {
	valPtr := def.ptr(&sw.draft)
	defVal := *def.ptr(&defCfg)
	recording := sw.recordingID == def.id
	regErr := a.shortcutError(def.id)

	row := ui.Row(c.Key("sc-row-" + def.id)).Height(30).AlignItems(ui.Center).Gap(8)
	row.Children(func() {
		ui.Text(c, def.label).Width(134).FontSize(12).NoWrap().TextColor(p.Text)
		if regErr != nil && !recording {
			ui.Text(c, "占用").FontSize(10.5).FontWeight(600).TextColor(ui.RGB(235, 68, 60))
		}
		ui.Spacer(c)

		if recording {
			recBtn := ui.Row(c.Key("sc-rec-"+def.id)).
				Label("录制"+def.label+"快捷键").
				Tooltip("按下新组合键，按 Esc 取消，点击退出录制").
				Height(28).
				Padding(0, 10).
				Gap(5).
				AlignItems(ui.Center).
				Radius(11).
				Background(p.ActiveBg).
				Border(1.5, p.Accent).
				InsetShadow(0, 1, 0, 0, p.CardTopRim).
				Cursor(ui.CursorPointer).
				Focusable().
				AutoFocus()
			recBtn.HandleInput(func(ev ui.InputEvent) bool {
				switch ev.Kind {
				case ui.InputText:
					return true
				case ui.InputKeyUp:
					sw.recordingMods = ev.Mods
					c.Invalidate()
					return true
				case ui.InputKeyDown:
					sw.recordingMods = ev.Mods
					if ev.Mods == 0 && (ev.Key == ui.KeyEscape || ev.PhysicalKey == ui.KeyEscape) {
						sw.recordingErr = ""
						a.stopShortcutRecording(sw, true)
						c.Invalidate()
						return true
					}
					if shortcutKeyToken(ev.PhysicalKey) == "" && shortcutKeyToken(ev.Key) == "" {
						c.Invalidate()
						return true
					}
					acc, ok := formatRecordedShortcut(ev.Mods, ev.Key, ev.PhysicalKey)
					if !ok {
						sw.recordingErr = "请搭配 ⌘ / ⌃ / ⌥ 修饰键或使用 F1–F12"
						sw.saved = ""
						c.Invalidate()
						return true
					}
					if conflict := a.findShortcutConflict(sw.draft, def.id, acc); conflict != "" {
						sw.recordingErr = "与「" + conflict + "」冲突，请换一组"
						sw.saved = ""
						c.Invalidate()
						return true
					}
					*valPtr = acc
					sw.recordingErr = ""
					a.stopShortcutRecording(sw, false)
					a.applyAndSaveSettings(sw.draft)
					if err := a.shortcutError(def.id); err != nil {
						sw.recordingErr = fmt.Sprintf("快捷键 %s 可能已被系统或其他应用占用，请换一组", strings.Join(formatMacShortcutSymbols(acc), ""))
						sw.setSavedBadge("快捷键冲突")
					} else {
						sw.setSavedBadge("✓ 快捷键已生效")
					}
					c.Invalidate()
					return true
				}
				return false
			})
			recBtn.Children(func() {
				if sw.recordingMods != 0 {
					for _, sym := range formatModSymbols(sw.recordingMods) {
						paintKeycap(c, p, sym, true)
					}
					ui.Text(c, "…").FontSize(12).FontWeight(600).TextColor(p.ActiveText)
				} else {
					ui.Text(c, "按下组合键… (Esc 取消)").FontSize(11).FontWeight(500).NoWrap().TextColor(p.ActiveText)
				}
			})
			if recBtn.Clicked() {
				sw.recordingErr = ""
				a.stopShortcutRecording(sw, true)
			}
			return
		}

		if *valPtr != defVal {
			resetBtn := ui.Box(c.Key("sc-rst-"+def.id)).
				Label("重置"+def.label+"快捷键").
				Tooltip("恢复默认："+strings.Join(formatMacShortcutSymbols(defVal), " ")).
				Size(24, 24).
				Radius(12).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Cursor(ui.CursorPointer)
			if resetBtn.Hovered() {
				resetBtn.Background(p.SubtleBtnHover).Border(1, p.SubtleBorder)
			}
			resetBtn.Children(func() {
				ui.Icon(c, iconUndo).FontSize(12).TextColor(p.TextSecondary)
			})
			if resetBtn.Clicked() {
				*valPtr = defVal
				sw.recordingErr = ""
				a.applyAndSaveSettings(sw.draft)
				sw.setSavedBadge("✓ 已恢复默认快捷键")
			}
		}

		idleBtn := ui.Row(c.Key("sc-idle-"+def.id)).
			Label("录制"+def.label).
			Tooltip("点击后直接按下新组合键").
			Height(28).
			Padding(0, 8).
			Gap(4).
			AlignItems(ui.Center).
			Radius(11).
			Cursor(ui.CursorPointer)
		if regErr != nil {
			idleBtn.Background(ui.RGBA(235, 68, 60, 0.14)).Border(1, ui.RGBA(235, 68, 60, 0.45))
		} else {
			bg := p.InputBg
			bd := p.InputBorder
			if idleBtn.Hovered() {
				bg = p.CardBgHover
				bd = p.ActiveBorder
			}
			idleBtn.Background(bg).Border(1, bd)
		}
		idleBtn.Children(func() {
			for _, sym := range formatMacShortcutSymbols(*valPtr) {
				paintKeycap(c, p, sym, false)
			}
		})
		if idleBtn.Clicked() {
			a.startShortcutRecording(sw, def.id)
		}
	})
}

func paintKeycap(c *ui.Context, p palette, label string, active bool) {
	bg := p.CardBg
	bd := p.SubtleBorder
	fg := p.Text
	if active {
		bg = p.ActiveBg
		bd = p.ActiveBorder
		fg = p.ActiveText
	}
	ui.Row(c).
		MinWidth(20).
		Height(20).
		Padding(0, 6).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Radius(6).
		Background(bg).
		Border(1, bd).
		InsetShadow(0, 1, 0, 0, p.CardTopRim).
		Children(func() {
			ui.Text(c, label).FontSize(11).FontWeight(600).NoWrap().TextColor(fg)
		})
}

func (a *app) findShortcutConflict(cfg config.Settings, selfID, candidate string) string {
	want := canonicalShortcutKey(candidate)
	if want == "" {
		return ""
	}
	for _, def := range shortcutDefs {
		if def.id == selfID {
			continue
		}
		if canonicalShortcutKey(*def.ptr(&cfg)) == want {
			return def.label
		}
	}
	if canonicalShortcutKey(restoreShortcut) == want {
		return "恢复刚关闭的贴图"
	}
	if canonicalShortcutKey(unghostShortcut) == want {
		return "恢复穿透贴图交互"
	}
	return ""
}

func formatRecordedShortcut(mods ui.Modifiers, key, physical ui.Key) (string, bool) {
	tok := shortcutKeyToken(physical)
	if tok == "" {
		tok = shortcutKeyToken(key)
	}
	if tok == "" {
		return "", false
	}
	isFnKey := isFunctionKeyToken(tok)
	if !isFnKey && mods&(ui.Cmd|ui.Ctrl|ui.Alt) == 0 {
		return "", false
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Cmd != 0 {
		parts = append(parts, "Cmd")
	}
	parts = append(parts, tok)
	return strings.Join(parts, "+"), true
}

func isFunctionKeyToken(tok string) bool {
	if len(tok) < 2 || tok[0] != 'F' {
		return false
	}
	var n int
	_, err := fmt.Sscanf(tok[1:], "%d", &n)
	return err == nil && n >= 1 && n <= 12 && fmt.Sprintf("F%d", n) == tok
}

func shortcutKeyToken(k ui.Key) string {
	if k >= ui.KeyA && k <= ui.KeyZ {
		return string('A' + byte(k-ui.KeyA))
	}
	if k >= ui.Key0 && k <= ui.Key9 {
		return string('0' + byte(k-ui.Key0))
	}
	if k >= ui.KeyF1 && k <= ui.KeyF12 {
		return fmt.Sprintf("F%d", int(k-ui.KeyF1)+1)
	}
	switch k {
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyTab:
		return "Tab"
	case ui.KeySpace:
		return "Space"
	case ui.KeyBackspace:
		return "Backspace"
	case ui.KeyDelete:
		return "Delete"
	case ui.KeyInsert:
		return "Insert"
	case ui.KeyHome:
		return "Home"
	case ui.KeyEnd:
		return "End"
	case ui.KeyPageUp:
		return "PageUp"
	case ui.KeyPageDown:
		return "PageDown"
	case ui.KeyLeft:
		return "Left"
	case ui.KeyRight:
		return "Right"
	case ui.KeyUp:
		return "Up"
	case ui.KeyDown:
		return "Down"
	case ui.KeyMinus:
		return "-"
	case ui.KeyEqual:
		return "="
	case ui.KeyComma:
		return ","
	case ui.KeyPeriod:
		return "."
	case ui.KeySlash:
		return "/"
	case ui.KeySemicolon:
		return ";"
	case ui.KeyQuote:
		return "'"
	case ui.KeyBracketLeft:
		return "["
	case ui.KeyBracketRight:
		return "]"
	case ui.KeyBackslash:
		return "\\"
	case ui.KeyBackquote:
		return "`"
	default:
		return ""
	}
}

func splitAcceleratorParts(acc string) ([]string, string) {
	s := strings.TrimSpace(acc)
	if s == "" {
		return nil, ""
	}
	var parts []string
	if strings.HasSuffix(s, "++") {
		raw := strings.Split(s[:len(s)-2], "+")
		parts = append(raw, "+")
	} else {
		parts = strings.Split(s, "+")
	}
	if len(parts) == 0 {
		return nil, ""
	}
	mods := make([]string, 0, len(parts)-1)
	for _, p := range parts[:len(parts)-1] {
		mods = append(mods, strings.TrimSpace(p))
	}
	return mods, strings.TrimSpace(parts[len(parts)-1])
}

func parseModFlags(mods []string) (ctrl, alt, shift, cmd bool) {
	for _, m := range mods {
		switch strings.ToLower(m) {
		case "ctrl", "control":
			ctrl = true
		case "alt", "option", "altgr":
			alt = true
		case "shift":
			shift = true
		case "cmd", "command", "super", "meta", "win", "cmdorctrl", "commandorcontrol":
			cmd = true
		}
	}
	return
}

func canonicalShortcutKey(acc string) string {
	mods, key := splitAcceleratorParts(acc)
	if key == "" {
		return ""
	}
	ctrl, alt, shift, cmd := parseModFlags(mods)
	mask := 0
	if ctrl {
		mask |= 1
	}
	if alt {
		mask |= 2
	}
	if shift {
		mask |= 4
	}
	if cmd {
		mask |= 8
	}
	return fmt.Sprintf("%d:%s", mask, strings.ToUpper(key))
}

func formatModSymbols(mods ui.Modifiers) []string {
	var out []string
	if mods&ui.Ctrl != 0 {
		out = append(out, "⌃")
	}
	if mods&ui.Alt != 0 {
		out = append(out, "⌥")
	}
	if mods&ui.Shift != 0 {
		out = append(out, "⇧")
	}
	if mods&ui.Cmd != 0 {
		out = append(out, "⌘")
	}
	return out
}

func formatMacShortcutSymbols(acc string) []string {
	mods, key := splitAcceleratorParts(acc)
	if key == "" {
		return nil
	}
	ctrl, alt, shift, cmd := parseModFlags(mods)
	var out []string
	if ctrl {
		out = append(out, "⌃")
	}
	if alt {
		out = append(out, "⌥")
	}
	if shift {
		out = append(out, "⇧")
	}
	if cmd {
		out = append(out, "⌘")
	}
	switch strings.ToLower(key) {
	case "enter", "return":
		out = append(out, "↩")
	case "tab":
		out = append(out, "⇥")
	case "backspace":
		out = append(out, "⌫")
	case "delete", "del":
		out = append(out, "⌦")
	case "escape", "esc":
		out = append(out, "⎋")
	case "space":
		out = append(out, "Space")
	case "up":
		out = append(out, "↑")
	case "down":
		out = append(out, "↓")
	case "left":
		out = append(out, "←")
	case "right":
		out = append(out, "→")
	default:
		out = append(out, strings.ToUpper(key))
	}
	return out
}
