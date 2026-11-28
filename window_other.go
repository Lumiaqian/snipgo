//go:build !darwin

package main

import "github.com/egoist/mygo"

func setOverlayEditing(*mygo.Window, bool)       {}
func prepareOverlayWindow(*mygo.Window)          {}
func preparePinWindow(*mygo.Window)              {}
func invalidatePinShadow(*mygo.Window)           {}
func applyWindowAppearance(*mygo.Window, string) {}
