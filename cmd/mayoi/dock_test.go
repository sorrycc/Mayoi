package main

import (
	"slices"
	"testing"

	"mayoi/internal/settings"
)

// The Dock icon's menu opens a window that shows nothing, above the recent
// folders, which open in the window in front.
func TestDockMenu(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	s.Recent = []string{root, other}
	m := &mayoi{s: s}
	var labels []string
	for _, it := range m.dockMenu().Items() {
		labels = append(labels, it.Label)
	}
	want := []string{"New Window", "", shortenHome(root), shortenHome(other)}
	if !slices.Equal(labels, want) {
		t.Errorf("the Dock menu: %q, want %q", labels, want)
	}
}

// File → New Window, ⇧⌘N, asks for a window that shows nothing.
func TestNewWindowCommand(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.run("file.newWindow")
	if h := hs.h; h.blankWindows != 1 || len(h.windows) != 0 {
		t.Errorf("New Window opened %d blank windows and %q", h.blankWindows, h.windows)
	}
}

// A Dock menu with no recent folder opens a window all the same, and says
// only that: the menu is the app's, not the list's.
func TestDockMenuWithoutRecentFolders(t *testing.T) {
	m := &mayoi{s: settings.Default()}
	items := m.dockMenu().Items()
	if len(items) != 1 || items[0].Label != "New Window" {
		t.Errorf("the Dock menu: %d items, first %q", len(items), items[0].Label)
	}
}
