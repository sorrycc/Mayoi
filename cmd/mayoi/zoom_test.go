package main

import (
	"math"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// Fractional window zoom scales the rail and sidebar together, and their
// buttons still take clicks at the positions where they are drawn.
func TestWindowZoomScalesRailAndSidebar(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	a := hs[0].a
	for range 30 {
		step(tt)
	}
	labels := []string{filepath.Base(other), a.withKey(locales.SidebarScm(), "view.scm")}
	before := make([]ui.Rect, len(labels))
	for i, label := range labels {
		var ok bool
		before[i], ok = tt.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
	}
	p := a.settings.Prefs
	p.ZoomLevel, p.TerminalFontSize = 1.5, 11
	a.settings.SetPrefs(p)
	step(tt)
	factor := float32(math.Pow(1.2, 1.5))
	for i, label := range labels {
		after, ok := tt.Find(label)
		if !ok || math.Abs(float64(after.W-before[i].W*factor)) > 0.1 || math.Abs(float64(after.H-before[i].H*factor)) > 0.1 {
			t.Errorf("%q: before %+v, after %+v, scale %v", label, before[i], after, factor)
		}
	}
	if a.settings.FontSize != 13 || termFont(a.settings).Size != 11 {
		t.Fatal("window zoom changed a base font size")
	}
	if err := tt.Click(labels[1]); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if a.window.SidebarView != "git" {
		t.Fatal("the zoomed sidebar button did not take the click")
	}
	if err := tt.RightClick(labels[0]); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "Close Project") {
		t.Fatal("the zoomed rail did not open the project's menu")
	}
	tt.CloseMenu()
	// The headless frame has no native window; select the next project
	// directly, as the other rail tests do.
	f.active = hs[1]
	for range 30 {
		step(tt)
	}
	// The inactive project's name identifies its rail button, without
	// also matching the active folder's name in the title bar.
	railLabel := filepath.Base(root)
	after, ok := tt.Find(railLabel)
	if !ok || math.Abs(float64(after.W-before[0].W*factor)) > 0.1 {
		t.Fatalf("switching projects: rail %+v, before %+v, found %v, zoom %v", after, before[0], ok, a.settings.ZoomLevel)
	}
	hs[1].a.run("view.zoomReset")
	step(tt)
	after, ok = tt.Find(railLabel)
	if !ok || after.W != before[0].W || after.H != before[0].H {
		t.Fatalf("reset rail: %+v, want %+v", after, before[0])
	}
}

func TestWindowZoomCommands(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.ZoomLevel = 0.5
	a.runBinding("workbench.action.zoomIn", nil, placeEditor)
	if a.settings.ZoomLevel != 1.5 || a.settings.FontSize != 13 {
		t.Fatal("window zoom did not preserve the fractional level and base font")
	}
	a.runBinding("editor.action.fontZoomIn", nil, placeEditor)
	if a.settings.ZoomLevel != 1.5 || a.settings.FontSize != 14 {
		t.Fatal("editor font zoom was not separate from window zoom")
	}
	a.runBinding("editor.action.fontZoomReset", nil, placeEditor)
	a.run("view.zoomOut")
	if a.settings.ZoomLevel != 0.5 || a.settings.FontSize != 13 {
		t.Fatal("zoom out or font reset failed")
	}
	for range 20 {
		a.run("view.zoomIn")
	}
	if a.settings.ZoomLevel != settings.MaxZoomLevel {
		t.Fatal("zoom exceeded its upper bound")
	}
	for range 20 {
		a.run("view.zoomOut")
	}
	if a.settings.ZoomLevel != settings.MinZoomLevel {
		t.Fatal("zoom exceeded its lower bound")
	}
	a.run("view.zoomReset")
	if a.settings.ZoomLevel != 0 {
		t.Fatal("zoom reset failed")
	}
}

// Windows and Linux route Ctrl shortcuts through the view rather than
// the native menu. They must still work while the editor has focus, and
// terminals must leave them to the window instead of sending them on.
func TestWindowZoomPlatformKeys(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		t.Run(goos, func(t *testing.T) {
			onOS(t, goos)
			hs := newHarness(t, false)
			a := hs.a
			a.openFile(hs.path("main.go"), true)
			hs.settle()
			for _, tc := range []struct {
				id, mac, other string
				key            ui.Key
				level          float32
			}{
				{"view.zoomIn", "⌘=", "Ctrl+=", ui.KeyEqual, 1},
				{"view.zoomOut", "⌘-", "Ctrl+-", ui.KeyMinus, 0},
				{"view.zoomOut", "⌘-", "Ctrl+-", ui.KeyMinus, -1},
				{"view.zoomReset", "⌘0", "Ctrl+0", ui.Key0, 0},
			} {
				want := tc.other
				if goos == "darwin" {
					want = tc.mac
				}
				if got := a.keyHint(tc.id); got != want {
					t.Fatalf("%s shortcut %q, want %q", tc.id, got, want)
				}
				if goos == "darwin" {
					// The native menu dispatches the command on macOS.
					a.run(tc.id)
				} else {
					hs.tt.Key(ui.Ctrl, tc.key)
					id, ok := a.shellSkip(keymap.Stroke{Mods: ui.Ctrl, Key: tc.key})
					if !ok || id != tc.id {
						t.Fatalf("terminal did not leave %s to the window", tc.other)
					}
				}
				hs.settle()
				if a.settings.ZoomLevel != tc.level || a.settings.FontSize != 13 {
					t.Fatalf("%s: zoom %v, font %v", want, a.settings.ZoomLevel, a.settings.FontSize)
				}
			}
		})
	}
}
