package main

import (
	"mayoi/internal/settings"
	"mayoi/locales"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestSplitPaths(t *testing.T) {
	root := workspaceFixture(t)
	got := argPaths([]string{"-psn_0_1", ".", "main.go", "internal/", root, "gone.go", ""}, root)
	folders, files := splitPaths(got)
	if !slices.Equal(folders, []string{root, filepath.Join(root, "internal")}) {
		t.Errorf("folders %v", folders)
	}
	if !slices.Equal(files, []string{filepath.Join(root, "main.go")}) {
		t.Errorf("files %v", files)
	}
}

// A window that shows nothing takes a folder, also before it starts; one
// with tabs to restore does not.
func TestBlankWindow(t *testing.T) {
	s := settings.Default()
	w := s.NewWindow()
	a := newApp(&fakeHost{}, s, w)
	t.Cleanup(a.shutdown)
	if !a.blank() {
		t.Error("an empty window is not blank")
	}
	w.Columns = []settings.Column{{Tabs: []settings.Tab{{File: "/tmp/a.go"}}}}
	if a.blank() {
		t.Error("a window with tabs to restore is blank")
	}
	// The folder as the system spells it, with backslashes on Windows.
	w.Columns, w.Folder = nil, "/tmp/x/"
	if a.blank() || a.folder() != filepath.FromSlash("/tmp/x") {
		t.Errorf("a window of a folder: blank %v, folder %q", a.blank(), a.folder())
	}
}

// Recent folders open in the window, or with Cmd in a window of their
// own; a folder another window has open brings that one to the front.
func TestRecentFolderOpensInWhichWindow(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	s.Recent = []string{root, other}
	h := &fakeHost{others: []string{other}}
	a := newApp(h, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	if err := tt.ClickWith(ui.Cmd, "Open "+root); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if !slices.Equal(h.windows, []string{root}) || a.root != "" {
		t.Fatalf("Cmd-click: windows %v, folder %q", h.windows, a.root)
	}
	tt.Click("Open " + other)
	step(tt)
	if a.root != "" || len(h.windows) != 1 {
		t.Fatalf("a folder of another window opened here: %q", a.root)
	}
	tt.Click("Open " + root)
	step(tt)
	if a.root != root || a.window.Folder != root {
		t.Fatalf("click: folder %q", a.root)
	}
}

func TestRecentFolderCmdEnter(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Default()
	s.Recent = []string{root}
	h := &fakeHost{}
	a := newApp(h, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	label := "Open " + root
	// Past every button of the sidebar and of START, however many it has.
	for range 30 {
		if tt.Focused(label) {
			break
		}
		tt.Key(0, ui.KeyTab)
		step(tt)
	}
	if !tt.Focused(label) {
		t.Fatal("Tab never reached the recent folder")
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	step(tt)
	step(tt)
	if !slices.Equal(h.windows, []string{root}) || a.root != "" {
		t.Fatalf("Cmd+Enter: windows %v, folder %q", h.windows, a.root)
	}
}

// The welcome page lists six recent folders, and More… opens the picker
// of them all.
func TestWelcomeMoreRecent(t *testing.T) {
	s := settings.Default()
	for range 8 {
		s.Recent = append(s.Recent, t.TempDir())
	}
	a := newApp(&fakeHost{}, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	if _, ok := tt.Find("Open " + s.Recent[6]); ok {
		t.Fatal("the welcome page lists a seventh recent folder")
	}
	if err := tt.Click(locales.MenuOpenRecentMore()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if !a.recents.open {
		t.Fatal("More… does not open the recent folders")
	}
}

// ⌃R shows the recent folders but the window's: ⏎ opens the one typed in
// the window, ⌘⏎ in a window of its own.
func TestOpenRecentPicker(t *testing.T) {
	root, other, third := workspaceFixture(t), workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	s.Recent = []string{third, other}
	h := &fakeHost{}
	a := newApp(h, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	a.openFolder(root)
	if h.recentChanges != 1 || s.Recent[0] != root {
		t.Fatalf("opening a folder: %d changes, recent %v", h.recentChanges, s.Recent)
	}
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	tt.Key(ui.Ctrl, ui.KeyR)
	step(tt)
	step(tt)
	if !a.recents.open || !slices.Equal(a.recents.dirs, []string{third, other}) {
		t.Fatalf("⌃R: open %v, folders %v", a.recents.open, a.recents.dirs)
	}
	tt.Type(filepath.Base(other))
	step(tt)
	tt.Key(ui.Cmd, ui.KeyEnter)
	step(tt)
	step(tt)
	if a.recents.open || !slices.Equal(h.windows, []string{other}) || a.root != root {
		t.Fatalf("⌘⏎: open %v, windows %v, folder %q", a.recents.open, h.windows, a.root)
	}
	tt.Key(ui.Ctrl, ui.KeyR)
	step(tt)
	step(tt)
	tt.Key(0, ui.KeyEnter)
	step(tt)
	step(tt)
	if a.recents.open || a.root != third || len(h.windows) != 1 {
		t.Fatalf("⏎: open %v, folder %q, windows %v", a.recents.open, a.root, h.windows)
	}
}

// File → Open Recent lists the recent folders but the window's, and runs
// the command that opens one in the window it was chosen in.
func TestOpenRecentMenu(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	s.Recent = []string{other, filepath.Join(t.TempDir(), "gone")}
	h := &fakeHost{}
	a := newApp(h, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	a.openFolder(root)
	var ran []string
	m := a.menu(false, func(id string, _ *mygo.Window) { ran = append(ran, id) })
	var recent []*mygo.MenuItem
	for _, top := range m.Items() {
		for _, it := range top.Submenu {
			if it.Label == "Open Recent" {
				recent = it.Submenu
			}
		}
	}
	var labels []string
	for _, it := range recent {
		labels = append(labels, it.Label)
	}
	want := []string{shortenHome(other), "", "More…", "", "Clear Recently Opened"}
	if !slices.Equal(labels, want) {
		t.Fatalf("Open Recent: %q, want %q", labels, want)
	}
	recent[0].Click(recent[0], nil)
	if !slices.Equal(ran, []string{recentCommand + other}) {
		t.Fatalf("ran %v", ran)
	}
	a.run(ran[0])
	if a.root != other {
		t.Errorf("the recent folder's command opened %q", a.root)
	}
	a.run("file.clearRecent")
	if len(s.Recent) != 0 || h.recentChanges != 3 {
		t.Errorf("clear: recent %v, %d changes", s.Recent, h.recentChanges)
	}
}

// A terminal keeps ⌃R, with which shells search their history.
func TestOpenRecentLeavesTerminalCtrlR(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminal")
	hs.settle()
	tt.Key(ui.Ctrl, ui.KeyR)
	hs.settle()
	if a.recents.open {
		t.Error("⌃R in the terminal opened the recent folders")
	}
	a.focusColumn()
	hs.settle()
	tt.Key(ui.Ctrl, ui.KeyR)
	hs.settle()
	if !a.recents.open {
		t.Error("⌃R outside the terminal did not open the recent folders")
	}
	a.closeTerminals()
}
