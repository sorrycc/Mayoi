package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// allTabNames are the names of the tabs of every column, a column at a time.
func allTabNames(a *app) []string {
	var out []string
	for _, col := range a.cols {
		for _, t := range col.tabs {
			out = append(out, t.name())
		}
	}
	return out
}

// Switching a window's folder keeps the tabs of the one it had, where
// they were, and the folders open in its explorer, which come back as it
// opens that folder again.
func TestSwitchFolderSwapsSessions(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	a := newApp(&fakeHost{}, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	a.openFolder(root)
	a.openFile(filepath.Join(root, "main.go"), true)
	a.openFile(filepath.Join(root, "README.md"), true)
	step(tt)
	a.activeEditor().pending = &[2]int{2, 6}
	a.files.Open.Add(filepath.Join(root, "internal"))
	step(tt)
	step(tt)

	a.switchFolder(other)
	step(tt)
	if a.root != other || len(allTabNames(a)) != 0 {
		t.Fatalf("switched: folder %q, tabs %v", a.root, allTabNames(a))
	}
	ses := s.Sessions[root]
	if ses == nil || len(ses.Columns) != 1 || len(ses.Columns[0].Tabs) != 2 || !slices.Equal(ses.Expanded, []string{"internal"}) {
		t.Fatalf("kept %+v", ses)
	}
	a.openFile(filepath.Join(other, "go.mod"), true)
	step(tt)

	a.switchFolder(root)
	step(tt)
	step(tt)
	if a.root != root || !slices.Equal(allTabNames(a), []string{"main.go", "README.md"}) || a.activeEditor().name() != "README.md" {
		t.Fatalf("back: folder %q, tabs %v", a.root, allTabNames(a))
	}
	if sel := a.activeEditor().sel; sel != [2]int{2, 6} {
		t.Errorf("selection %v", sel)
	}
	if !a.files.Open.Has(filepath.Join(root, "internal")) {
		t.Error("the explorer's folder is closed")
	}
	if s.Sessions[root] != nil || s.Sessions[other] == nil || s.Sessions[other].Columns[0].Tabs[0].File != filepath.Join(other, "go.mod") {
		t.Errorf("sessions %v", s.Sessions)
	}
}

// Closing the folder keeps its tabs, which come back as it opens again.
func TestCloseFolderKeepsSession(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Default()
	a := newApp(&fakeHost{}, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	a.openFolder(root)
	a.openFile(filepath.Join(root, "go.mod"), true)
	step(tt)
	a.run("file.closeFolder")
	step(tt)
	if a.root != "" || len(allTabNames(a)) != 0 || s.Sessions[root] == nil {
		t.Fatalf("closed: folder %q, tabs %v, sessions %v", a.root, allTabNames(a), s.Sessions)
	}
	a.switchFolder(root)
	step(tt)
	if !slices.Equal(allTabNames(a), []string{"go.mod"}) {
		t.Fatalf("opened again: tabs %v", allTabNames(a))
	}
}

// A window whose documents close as it closes, after asking about their
// changes, keeps the tabs it had before.
func TestClosingWindowKeepsItsTabs(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Default()
	s.AutoSave = false
	h := &fakeHost{choice: 1} // don't save
	w := s.NewWindow()
	a := newApp(h, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	a.openFolder(root)
	a.openFile(filepath.Join(root, "main.go"), true)
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "// changed\n"})
	step(tt)
	closed := false
	a.closeKeeping(func() { closed = true })
	step(tt)
	if !closed || len(allTabNames(a)) != 0 {
		t.Fatalf("closed %v, tabs %v", closed, allTabNames(a))
	}
	a.keepSession()
	s.Forget(w)
	ses := s.TakeSession(root)
	if ses == nil || len(ses.Columns) != 1 || ses.Columns[0].Tabs[0].File != filepath.Join(root, "main.go") {
		t.Fatalf("kept %+v", ses)
	}
}

// The tabs of the last session come back where they were scrolled, with
// their caret and their own choice to wrap, and a diff showing every
// line still does.
func TestRestoresWhereTabsWere(t *testing.T) {
	root := workspaceFixture(t)
	wrap := true
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Columns = []settings.Column{{Tabs: []settings.Tab{
		{File: filepath.Join(root, "main.go"), Sel: [2]int{10, 14}, Wrap: &wrap},
		{File: filepath.Join(root, "go.mod"), Sel: [2]int{999, 999}},
		{Diff: "all", Full: true},
	}, Active: 0, Weight: 1}}
	w.Expanded = []string{"internal/a", "gone"}
	a := newApp(&fakeHost{folder: root}, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	step(tt)
	col := a.cols[0]
	if len(col.tabs) != 3 {
		t.Fatalf("tabs %v", allTabNames(a))
	}
	if ed := col.tabs[0].ed; ed.sel != [2]int{10, 14} || !a.wraps(ed) {
		t.Errorf("main.go: selection %v, wraps %v", ed.sel, a.wraps(ed))
	}
	if ed := col.tabs[1].ed; ed.sel[1] != ed.buf.Len() {
		t.Errorf("go.mod: selection %v past its %d characters", ed.sel, ed.buf.Len())
	}
	if !col.tabs[2].diff.full {
		t.Error("the diff folds its unchanged lines")
	}
	if !a.files.Open.Has(filepath.Join(root, "internal", "a")) || a.files.Open.Has(filepath.Join(root, "gone")) {
		t.Error("the explorer's folders")
	}
	a.rememberTabs()
	got := w.Columns[0].Tabs
	if got[0].Sel != [2]int{10, 14} || got[0].Wrap == nil || !*got[0].Wrap || !got[2].Full ||
		!slices.Equal(w.Expanded, []string{"internal/a"}) {
		t.Errorf("remembered %+v, expanded %v", got, w.Expanded)
	}
}

// A file with a line too long to lay out is not opened again as the
// window starts, which it would hang.
func TestRestoreSkipsLongLines(t *testing.T) {
	root := workspaceFixture(t)
	long := filepath.Join(root, "index.js.map")
	os.WriteFile(long, []byte(strings.Repeat("x", maxLineBytes+1)), 0o644)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Columns = []settings.Column{{Tabs: []settings.Tab{
		{File: long},
		{File: filepath.Join(root, "main.go")},
	}, Active: 0, Weight: 1}}
	a := newApp(&fakeHost{folder: root}, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	if names := allTabNames(a); !slices.Equal(names, []string{"main.go"}) {
		t.Fatalf("tabs %v", names)
	}
}

// A tab restored shows its text where it was scrolled, not at its caret.
func TestRestoresTheScroll(t *testing.T) {
	root := workspaceFixture(t)
	long := filepath.Join(root, "long.go")
	os.WriteFile(long, []byte(strings.Repeat("// a line\n", 400)), 0o644)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Columns = []settings.Column{{Tabs: []settings.Tab{
		{File: long, ScrollY: 1200}, {File: filepath.Join(root, "go.mod")},
	}, Active: 0, Weight: 1}}
	a := newApp(&fakeHost{folder: root}, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	for range 3 {
		step(tt)
	}
	if y := a.cols[0].tabs[0].ed.vscroll.Y; y != 1200 {
		t.Errorf("scrolled to %v", y)
	}
}
