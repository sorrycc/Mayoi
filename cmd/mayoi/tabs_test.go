package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// tabBox is a tab as the last frame laid it out, at its own origin.
var tabBox = ui.Rect{W: 120, H: tabStripH}

// The middle button closes the tab it is on: the press does not show it,
// and its release closes it, wherever in the tab it is.
func TestMiddleButtonClosesATab(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	col := a.column()
	if len(col.tabs) != 2 {
		t.Fatalf("%d tabs, want 2", len(col.tabs))
	}
	shown := a.activeTab()
	other := col.tabs[0]
	if other == shown {
		other = col.tabs[1]
	}
	was := col.active
	if !a.tabMiddle(other, ui.InputEvent{Kind: ui.InputPointerDown, Button: middleButton, X: 10, Y: 10}, tabBox) {
		t.Fatal("the press was not taken")
	}
	hs.settle()
	if col.active != was {
		t.Errorf("the press showed the tab: active %d, want %d", col.active, was)
	}
	if at, _ := a.locate(other); at == nil {
		t.Fatal("the tab closed as it was pressed, not as it was let go of")
	}
	if !a.tabMiddle(other, ui.InputEvent{Kind: ui.InputPointerUp, Button: middleButton, X: 10, Y: 10}, tabBox) {
		t.Fatal("the release was not taken")
	}
	hs.settle()
	if at, _ := a.locate(other); at != nil {
		t.Error("the tab is still open")
	}
	if a.activeTab() != shown {
		t.Error("the tab closed took the one shown with it")
	}
	if a.middle != nil {
		t.Error("the press is still remembered")
	}
}

// The input of the other buttons and of the keys goes on as it would
// without the handler.
func TestTabMiddleLeavesOtherInputAlone(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	tab := a.activeTab()
	for _, ev := range []ui.InputEvent{
		{Kind: ui.InputPointerDown, Button: 0},
		{Kind: ui.InputPointerUp, Button: 0},
		{Kind: ui.InputPointerUp, Button: 1},
		{Kind: ui.InputPointerMove, Button: 0},
		{Kind: ui.InputKeyDown, Key: ui.KeyS},
	} {
		if a.tabMiddle(tab, ev, tabBox) {
			t.Errorf("input %v of button %d was taken", ev.Kind, ev.Button)
		}
	}
	if col, _ := a.locate(tab); col == nil {
		t.Error("the tab was closed by the input of another button")
	}
	if a.middle != nil {
		t.Error("a press of another button is remembered")
	}
}

// Let go of off the tab, the middle button closes nothing, as in a
// browser; and a press whose release never came does not keep a click
// from showing the tab.
func TestTabMiddleReleasedOffTheTab(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	tab := a.activeTab()
	a.tabMiddle(tab, ui.InputEvent{Kind: ui.InputPointerDown, Button: middleButton, X: 10, Y: 10}, tabBox)
	a.tabMiddle(tab, ui.InputEvent{Kind: ui.InputPointerUp, Button: middleButton, X: 10, Y: 200}, tabBox)
	hs.settle()
	if col, _ := a.locate(tab); col == nil {
		t.Error("released below the strip, the tab closed")
	}
	if a.middle != nil {
		t.Error("the press is still remembered")
	}
	a.tabMiddle(tab, ui.InputEvent{Kind: ui.InputPointerDown, Button: middleButton}, tabBox)
	a.tabMiddle(tab, ui.InputEvent{Kind: ui.InputPointerDown, Button: 0}, tabBox)
	if a.middle != nil {
		t.Error("a primary press left the middle one remembered")
	}
}

// A plain wheel scrolls the tab strip sideways, as browsers scroll their
// tabs: the container scrolls sideways only, so the wheel's own way down
// would leave it still.
func TestWheelScrollsTheTabStrip(t *testing.T) {
	hs, col, x, y := crowdedTabs(t)
	// Over the first tab's cross, a part of the tab that takes its input
	// itself, rather than the strip's: the wheel scrolls it all the same.
	hs.tt.Wheel(x, y, 0, 120)
	hs.settle()
	if col.tabScroll.X <= 0 {
		t.Errorf("the wheel left the tabs at X %v", col.tabScroll.X)
	}
}

// A wheel of notches of any size stops at the last tab.
func TestWheelStopsTheTabStripAtItsEnd(t *testing.T) {
	hs, col, x, y := crowdedTabs(t)
	hs.tt.Wheel(x, y, 0, 100000)
	hs.settle()
	if col.tabScroll.X != col.tabScroll.MaxX {
		t.Errorf("the wheel left the tabs at X %v, not at the end %v", col.tabScroll.X, col.tabScroll.MaxX)
	}
}

// A wheel that goes the way the tabs go, sideways or with Shift, which
// turns a wheel sideways, is the container's own: it scrolls the strip
// there and leaves the wheel of the tabs not to take it.
func TestWheelLeavesSidewaysToTheTabs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mods   ui.Modifiers
		dx, dy float32
	}{
		{"sideways", 0, 40, 0},
		{"sideways with noise", 0, 40, 3},
		{"with Shift", ui.Shift, 0, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs, col, x, y := crowdedTabs(t)
			ev := ui.InputEvent{Kind: ui.InputScroll, DX: tc.dx, DY: tc.dy, Mods: tc.mods}
			if hs.a.tabWheel(col, ev) {
				t.Errorf("the tabs took a wheel %s", tc.name)
			}
			hs.tt.HoldModifiers(tc.mods)
			hs.tt.Wheel(x, y, tc.dx, tc.dy)
			hs.settle()
			if col.tabScroll.X <= 0 {
				t.Errorf("a wheel %s left the tabs at X %v", tc.name, col.tabScroll.X)
			}
		})
	}
}

// A touchpad's swipe goes down with a little of the other way from finger
// to finger, and still scrolls the tabs.
func TestWheelTakesASwipeMostlyDown(t *testing.T) {
	hs, col, _, _ := crowdedTabs(t)
	ev := ui.InputEvent{Kind: ui.InputScroll, DX: 2, DY: 40, Precise: true}
	if !hs.a.tabWheel(col, ev) {
		t.Fatal("the tabs left a swipe mostly down to the container")
	}
	hs.settle()
	if col.tabScroll.X != 40 {
		t.Errorf("a swipe mostly down left the tabs at X %v, want 40", col.tabScroll.X)
	}
}

// crowdedTabs makes a strip with more tabs than fit, at its start, and
// returns its column and the middle of its first tab's cross.
func crowdedTabs(t *testing.T) (*harness, *column, float32, float32) {
	t.Helper()
	hs := newHarness(t, true)
	a := hs.a
	for i := range 20 {
		p := hs.path(fmt.Sprintf("scroll-%02d.go", i))
		if err := os.WriteFile(p, []byte("package scroll\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		a.openFile(p, true)
	}
	hs.settle()
	col := a.column()
	if len(col.tabs) != 20 {
		t.Fatalf("%d tabs, want 20", len(col.tabs))
	}
	// Show the first, so that the strip starts at its start.
	a.activate(col, 0, true)
	hs.settle()
	if col.tabScroll.MaxX <= 0 {
		t.Fatalf("the tabs fit the strip: MaxX %v", col.tabScroll.MaxX)
	}
	if col.tabScroll.X != 0 {
		t.Fatalf("the strip starts at X %v, want 0", col.tabScroll.X)
	}
	box, ok := hs.tt.Find(col.tabs[0].closeLabel())
	if !ok {
		t.Fatalf("the first tab's cross is not shown: %q", hs.tt.Texts())
	}
	return hs, col, box.X + box.W/2, box.Y + box.H/2
}
