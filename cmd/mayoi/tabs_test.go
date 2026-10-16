package main

import (
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
