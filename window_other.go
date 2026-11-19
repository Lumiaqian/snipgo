//go:build !darwin

package main

import "github.com/egoist/mygo"

func prepareOverlayWindow(*mygo.Window) {}
func preparePinWindow(*mygo.Window)     {}
