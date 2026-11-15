//go:build !linux

package main

import "github.com/egoist/mygo/ui"

func (a *app) linuxWindowControls(*ui.Context) {}
