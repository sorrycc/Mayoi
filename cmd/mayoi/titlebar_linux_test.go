//go:build linux

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

type testControlWindow struct {
	maximized, fullscreen bool
	minimized, closed     bool
}

func (w *testControlWindow) IsFullScreen() bool { return w.fullscreen }
func (w *testControlWindow) IsMaximized() bool  { return w.maximized }
func (w *testControlWindow) Minimize()          { w.minimized = true }
func (w *testControlWindow) Maximize()          { w.maximized = true }
func (w *testControlWindow) Unmaximize()        { w.maximized = false }
func (w *testControlWindow) Close()             { w.closed = true }

func TestLinuxWindowControls(t *testing.T) {
	w := &testControlWindow{}
	a := &app{pal: paletteFor("", true)}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Children(func() { a.drawLinuxWindowControls(c, w) })
	}, 200, 40)
	step(tt)
	if err := tt.Click(locales.WindowMinimize()); err != nil || !w.minimized {
		t.Fatalf("minimize: %v, minimized %v", err, w.minimized)
	}
	if err := tt.Click(locales.WindowMaximize()); err != nil || !w.maximized {
		t.Fatalf("maximize: %v, maximized %v", err, w.maximized)
	}
	step(tt)
	if err := tt.Click(locales.WindowRestore()); err != nil || w.maximized {
		t.Fatalf("restore: %v, maximized %v", err, w.maximized)
	}
	if err := tt.Click(locales.WindowClose()); err != nil || !w.closed {
		t.Fatalf("close: %v, closed %v", err, w.closed)
	}
	w.fullscreen = true
	step(tt)
	if _, ok := tt.Find(locales.WindowClose()); ok {
		t.Error("window controls are visible in full screen")
	}
}
