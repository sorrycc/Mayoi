//go:build linux

package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

var (
	iconWindowMinimize = lucide(`<path d="M5 12h14"/>`)
	iconWindowMaximize = lucide(`<rect x="5" y="5" width="14" height="14" rx="1"/>`)
	iconWindowRestore  = lucide(`<path d="M8 5V3h13v13h-2"/><rect x="3" y="8" width="13" height="13" rx="1"/>`)
)

type linuxControlWindow interface {
	IsFullScreen() bool
	IsMaximized() bool
	Minimize()
	Maximize()
	Unmaximize()
	Close()
}

// linuxWindowControls draws the window controls in the title bar. Close
// goes through the frame so all projects can settle their changes first.
func (a *app) linuxWindowControls(c *ui.Context) {
	h, ok := a.host.(*desktop)
	if !ok || h.f == nil || h.f.win == nil {
		return
	}
	a.drawLinuxWindowControls(c, h.f.win)
}

func (a *app) drawLinuxWindowControls(c *ui.Context, win linuxControlWindow) {
	if win.IsFullScreen() {
		return
	}
	if a.iconButton(c, iconWindowMinimize, locales.WindowMinimize(), false).Clicked() {
		win.Minimize()
	}
	icon, text := iconWindowMaximize, locales.WindowMaximize()
	maximized := win.IsMaximized()
	if maximized {
		icon, text = iconWindowRestore, locales.WindowRestore()
	}
	if a.iconButton(c, icon, text, false).Clicked() {
		if maximized {
			win.Unmaximize()
		} else {
			win.Maximize()
		}
	}
	if a.iconButton(c, iconX, locales.WindowClose(), false).Clicked() {
		win.Close()
	}
}
