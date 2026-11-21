package main

import (
	"image/color"
	"snipgo/internal/annotate"
	"snipgo/internal/session"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) handleSelectionKeys(c *ui.Context) bool {
	esc := c.Shortcut(0, ui.KeyEscape)
	if a.textEditing {
		if esc {
			a.textEditing = false
			a.textEditBuf = ""
			a.invalidateAll()
		}
		return false
	}

	cmdQ := c.Shortcut(ui.Cmd, ui.KeyQ)
	cmdW := c.Shortcut(ui.Cmd, ui.KeyW)
	space := c.Shortcut(0, ui.KeySpace)
	enter := c.Shortcut(0, ui.KeyEnter)
	cmdC := c.Shortcut(ui.Cmd, ui.KeyC)
	cmdZ := c.Shortcut(ui.Cmd, ui.KeyZ)
	cmdShiftZ := c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ)
	keyT := c.Shortcut(0, ui.KeyT)
	cmdT := c.Shortcut(ui.Cmd, ui.KeyT)
	cmdS := c.Shortcut(ui.Cmd, ui.KeyS)
	keyO := c.Shortcut(0, ui.KeyO)
	keyW := c.Shortcut(0, ui.KeyW)
	keyB := c.Shortcut(0, ui.KeyB)
	keyR := c.Shortcut(0, ui.KeyR)
	keyL := c.Shortcut(0, ui.KeyL)
	keyX := c.Shortcut(0, ui.KeyX)
	keyK := c.Shortcut(0, ui.KeyK)
	keyP := c.Shortcut(0, ui.KeyP)
	shiftP := c.Shortcut(ui.Shift, ui.KeyP)
	tabKey := c.Shortcut(0, ui.KeyTab)
	keyC := c.Shortcut(0, ui.KeyC)
	shiftC := c.Shortcut(ui.Shift, ui.KeyC)
	delKey := c.Shortcut(0, ui.KeyBackspace) || c.Shortcut(0, ui.KeyDelete)

	toolKeys := [...]struct {
		key  ui.Key
		tool annotate.Tool
	}{
		{ui.Key1, annotate.ToolRect},
		{ui.Key2, annotate.ToolEllipse},
		{ui.Key3, annotate.ToolArrow},
		{ui.Key4, annotate.ToolPen},
		{ui.Key5, annotate.ToolStep},
		{ui.Key6, annotate.ToolMosaic},
		{ui.Key7, annotate.ToolText},
		{ui.Key8, annotate.ToolSpotlight},
		{ui.Key9, annotate.ToolErase},
		{ui.Key0, annotate.ToolLoupe},
	}
	var pickedTool annotate.Tool = -1
	for _, tk := range toolKeys {
		if c.Shortcut(0, tk.key) && pickedTool < 0 {
			pickedTool = tk.tool
		}
	}

	type nudgeKey struct {
		mods   ui.Modifiers
		key    ui.Key
		dx, dy int
		resize bool
	}
	nudges := [...]nudgeKey{
		{0, ui.KeyLeft, -1, 0, false},
		{0, ui.KeyRight, 1, 0, false},
		{0, ui.KeyUp, 0, -1, false},
		{0, ui.KeyDown, 0, 1, false},
		{ui.Shift, ui.KeyLeft, -10, 0, false},
		{ui.Shift, ui.KeyRight, 10, 0, false},
		{ui.Shift, ui.KeyUp, 0, -10, false},
		{ui.Shift, ui.KeyDown, 0, 10, false},
		{ui.Alt, ui.KeyLeft, -1, 0, true},
		{ui.Alt, ui.KeyRight, 1, 0, true},
		{ui.Alt, ui.KeyUp, 0, -1, true},
		{ui.Alt, ui.KeyDown, 0, 1, true},
		{ui.Shift | ui.Alt, ui.KeyLeft, -10, 0, true},
		{ui.Shift | ui.Alt, ui.KeyRight, 10, 0, true},
		{ui.Shift | ui.Alt, ui.KeyUp, 0, -10, true},
		{ui.Shift | ui.Alt, ui.KeyDown, 0, 10, true},
	}
	var firedNudge *nudgeKey
	for i := range nudges {
		if c.Shortcut(nudges[i].mods, nudges[i].key) && firedNudge == nil {
			firedNudge = &nudges[i]
		}
	}

	switch {
	case cmdQ:
		a.deferUI(func() { mygo.App.Quit() })
		return true
	case esc || cmdW:
		a.deferUI(a.session.Cancel)
		return true
	case cmdShiftZ:
		if a.session.Annotations().Redo() {
			a.invalidateAll()
		}
		return false
	case cmdZ:
		if a.session.Annotations().Undo() {
			a.invalidateAll()
		}
		return false
	case delKey:
		if a.session.Annotations().DeleteSelected() {
			a.invalidateAll()
		}
		return false
	case pickedTool >= 0 && a.session.Phase() == session.PhaseAdjusting:
		ac := a.session.Annotations()
		ac.ClearSelection()
		if ac.Tool == pickedTool {
			ac.Tool = annotate.ToolNone
		} else {
			ac.Tool = pickedTool
		}
		a.invalidateAll()
		return false
	case keyX && a.session.Phase() == session.PhaseAdjusting:
		a.autoRedactSelection()
		return false
	case keyK:
		a.commitOrCloseTextEdit()
		a.deferUI(a.session.Cutout)
		return true
	case tabKey:
		a.session.CycleColorFormat()
		a.invalidateAll()
		return false
	case shiftP:
		if len(a.session.PickedColors()) > 0 {
			a.deferUI(func() { a.session.PinPickedPalette() })
			return true
		}
		return false
	case keyP:
		if a.session.SampleColorToPalette(func(color.NRGBA) { a.invalidateAll() }) {
			a.invalidateAll()
		}
		return false
	case keyL && a.session.Phase() == session.PhaseAdjusting:
		if sel, locked, ok := a.session.ActiveRect(); ok && locked {
			a.deferUI(func() { a.startScrollCapture(sel) })
			return true
		}
		return false
	case space:
		if a.session.ToggleWindowSnap() {
			a.invalidateAll()
		}
		return false
	case enter || cmdC:
		a.deferUI(a.session.Copy)
		return true
	case keyT || cmdT:
		a.deferUI(a.session.Pin)
		return true
	case cmdS:
		a.deferUI(a.session.Save)
		return true
	case keyO:
		a.deferUI(a.session.OCR)
		return true
	case keyW:
		if a.session.ToggleWindowOnly() {
			a.invalidateAll()
		}
		return false
	case keyB:
		a.commitOrCloseTextEdit()
		a.deferUI(a.session.Template)
		return true
	case keyR:
		if a.session.ToggleMeasure() {
			a.invalidateAll()
		}
		return false
	case keyC:
		a.deferUI(func() { a.session.CopyColor(false) })
		return true
	case shiftC:
		a.deferUI(func() { a.session.CopyColor(true) })
		return true
	case firedNudge != nil:
		if a.session.Nudge(firedNudge.dx, firedNudge.dy, firedNudge.resize) {
			a.invalidateAll()
		}
	}
	return false
}

func cursorForHandle(h session.Handle) ui.Cursor {
	switch h {
	case session.HandleMove:
		return ui.CursorMove
	case session.HandleNW, session.HandleSE:
		return ui.CursorResizeNWSE
	case session.HandleNE, session.HandleSW:
		return ui.CursorResizeNESW
	case session.HandleN, session.HandleS:
		return ui.CursorResizeNS
	case session.HandleE, session.HandleW:
		return ui.CursorResizeEW
	default:
		return ui.CursorCrosshair
	}
}
