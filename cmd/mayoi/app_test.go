package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"mayoi/internal/settings"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeHost answers the app's dialogs as a test says.
type fakeHost struct {
	folder  string
	saveTo  string
	choice  int
	asked   []string
	title   string
	closed  bool
	trashed []string
	// revealed are the paths asked to show in the Finder.
	revealed []string
	// others are the folders other windows have open, and windows those
	// asked to open in a window of their own.
	others  []string
	windows []string
	// folders are those asked to open as `mayoi <dir>` opens them.
	folders []string
	// recentChanges counts the changes of the recent folders.
	recentChanges int
	// clipboard are the files on the clipboard.
	clipboard []string
}

func (h *fakeHost) openFolder(done func(string)) { done(h.folder) }
func (h *fakeHost) saveAs(name string, done func(string)) {
	h.asked = append(h.asked, "save "+name)
	done(h.saveTo)
}
func (h *fakeHost) confirmClose(name string, done func(int)) {
	h.asked = append(h.asked, "close "+name)
	done(h.choice)
}
func (h *fakeHost) trash(path string) error {
	h.trashed = append(h.trashed, path)
	return os.RemoveAll(path)
}
func (h *fakeHost) reveal(path string)    { h.revealed = append(h.revealed, path) }
func (h *fakeHost) setTitle(title string) { h.title = title }
func (h *fakeHost) copyFiles(paths []string) error {
	h.clipboard = paths
	return nil
}
func (h *fakeHost) clipboardFiles() []string { return h.clipboard }
func (h *fakeHost) closeWindow()             { h.closed = true }
func (h *fakeHost) setTheme(_, _ string)     {}
func (h *fakeHost) showFolder(dir string) bool {
	return slices.Contains(h.others, dir)
}
func (h *fakeHost) newWindow(dir string)      { h.windows = append(h.windows, dir) }
func (h *fakeHost) openFolders(dirs []string) { h.folders = append(h.folders, dirs...) }
func (h *fakeHost) recentChanged()            { h.recentChanges++ }
func (h *fakeHost) fontChanged()              {}
func (h *fakeHost) installCLI()               {}
func (h *fakeHost) uninstallCLI()             {}

// workspaceFixture makes a folder of a few files.
func workspaceFixture(t testing.TB) string {
	root := t.TempDir()
	files := map[string]string{
		"main.go":             "package main\n\nimport \"fmt\"\n\n// main says hello.\nfunc main() {\n\tfmt.Println(\"hello, mayoi\")\n}\n",
		"README.md":           "# Mayoi\n\nA small editor.\n",
		"internal/a/a.go":     "package a\n\nconst Answer = 42\n",
		"internal/b/b.ts":     "export const greet = (name: string) => `hi ${name}`;\n",
		"web/index.html":      "<!doctype html>\n<title>Mayoi</title>\n",
		"web/style.css":       "body { color: #333; }\n",
		"node_modules/x/x.js": "module.exports = 1\n",
		"go.mod":              "module example\n\ngo 1.27\n",
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type harness struct {
	t    testing.TB
	a    *app
	h    *fakeHost
	tt   *ui.Tester
	root string
}

func newHarness(t testing.TB, dark bool) *harness {
	root := workspaceFixture(t)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	h := &fakeHost{folder: root}
	a := newApp(h, s, w)
	tt := ui.NewTester(a.view, 1200, 760)
	tt.SetDark(dark)
	hs := &harness{t: t, a: a, h: h, tt: tt, root: root}
	hs.settle()
	return hs
}

// settle draws frames until the background work (reading directories,
// highlighting, indexing) is done.
func (hs *harness) settle() {
	for range 30 {
		hs.tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	hs.tt.Frame()
}

func (hs *harness) shot(name string) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		hs.t.Fatal(err)
	}
	png.Encode(f, hs.tt.Image())
	f.Close()
}

func (hs *harness) path(rel string) string { return filepath.Join(hs.root, filepath.FromSlash(rel)) }

func TestStatusVisibilityPrioritizesNarrowWindows(t *testing.T) {
	for _, test := range []struct {
		width float32
		want  statusVisibility
	}{
		{500, statusVisibility{}},
		{640, statusVisibility{branchLabel: true, language: true}},
		{700, statusVisibility{branchLabel: true, indent: true, language: true}},
		{800, statusVisibility{branchLabel: true, terminal: true, lines: true, indent: true, eol: true, language: true}},
		{1000, statusVisibility{branchLabel: true, folder: true, terminal: true, lines: true, indent: true, encoding: true, eol: true, language: true}},
	} {
		if got := statusVisibilityFor(test.width); got != test.want {
			t.Errorf("at %.0f: got %+v, want %+v", test.width, got, test.want)
		}
	}
}

func TestChromeAdaptsToNarrowWindow(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.openFile(hs.path("main.go"), true)
	hs.tt.SetSize(640, 760)
	hs.settle()
	if !hs.tt.HasText("Ln 1, Col 1") {
		t.Fatalf("position is missing from narrow status bar: %q", hs.tt.Texts())
	}
	if hs.tt.HasText("UTF-8") {
		t.Error("encoding crowds the narrow status bar")
	}
	if box, ok := hs.tt.Find("Quick Open"); !ok || box.X < 0 || box.X+box.W > 640 {
		t.Errorf("command center does not fit the narrow title bar: %v, found %v", box, ok)
	}
	hs.tt.SetSize(1200, 760)
	hs.settle()
	if !hs.tt.HasText("UTF-8") {
		t.Error("wide status bar does not show encoding")
	}
}

func TestOpenFolderShowsTreeAndOpensFiles(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	for _, name := range []string{"internal", "web", "main.go", "README.md", "go.mod"} {
		if !tt.HasText(name) {
			t.Errorf("tree lacks %q: %q", name, tt.Texts())
		}
	}
	if tt.HasText("node_modules") == false {
		t.Error("node_modules is hidden from the tree; it should show (only quick open skips it)")
	}
	hs.shot("01-welcome")

	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.path != hs.path("main.go") {
		t.Fatalf("active doc %v after clicking main.go", d)
	}
	// A folder opens with a click, and shows its children.
	if err := tt.Click("internal"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !tt.HasText("a") || !tt.HasText("b") {
		t.Errorf("internal did not open: %q", tt.Texts())
	}
	hs.shot("02-editor-dark")
	if !strings.Contains(hs.h.title, "main.go") {
		t.Errorf("title %q", hs.h.title)
	}
}

func TestEditSaveAndDirtyClose(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false // the prompts are for files that do not save themselves
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	tt.Type("Hello ")
	hs.settle()
	d := a.activeEditor()
	if !d.dirty() || !strings.HasPrefix(d.buf.String(), "Hello # Mayoi") {
		t.Fatalf("typed: dirty %v text %q", d.dirty(), d.buf.String())
	}
	if !strings.Contains(hs.h.title, "●") {
		t.Errorf("title %q lacks the dot of changes", hs.h.title)
	}
	a.run("file.save")
	hs.settle()
	data, _ := os.ReadFile(hs.path("README.md"))
	if !strings.HasPrefix(string(data), "Hello # Mayoi") || d.dirty() {
		t.Fatalf("saved %q, dirty %v", data, d.dirty())
	}
	// Our own save comes back from the watcher without changing anything.
	hs.settle()
	if d.dirty() {
		t.Error("dirty after the watcher saw the save")
	}

	// Closing a changed tab asks; Cancel keeps it.
	tt.Type("more ")
	hs.settle()
	hs.h.choice = 2
	a.run("file.close")
	hs.settle()
	if len(a.docs) != 1 || len(hs.h.asked) != 1 {
		t.Fatalf("cancel: %d docs, asked %v", len(a.docs), hs.h.asked)
	}
	// Don't Save closes it, and leaves the file.
	hs.h.choice = 1
	a.run("file.close")
	hs.settle()
	if len(a.docs) != 0 {
		t.Fatalf("don't save: %d docs", len(a.docs))
	}
	data, _ = os.ReadFile(hs.path("README.md"))
	if strings.Contains(string(data), "more") {
		t.Error("discarded changes were saved")
	}
}

func TestExternalChangeReloadsCleanDocument(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.openFile(hs.path("go.mod"), true)
	hs.settle()
	os.WriteFile(hs.path("go.mod"), []byte("module changed\n"), 0o644)
	deadline := time.Now().Add(5 * time.Second)
	for a.activeEditor().buf.String() != "module changed\n" && time.Now().Before(deadline) {
		hs.settle()
	}
	if got := a.activeEditor().buf.String(); got != "module changed\n" {
		t.Errorf("not reloaded: %q", got)
	}
}

func TestQuickOpenAndGoToLine(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.run("go.quickOpen")
	deadline := time.Now().Add(5 * time.Second)
	for a.index == nil && time.Now().Before(deadline) {
		hs.settle()
	}
	for _, f := range a.index {
		if strings.HasPrefix(f, "node_modules/") {
			t.Errorf("index has %s", f)
		}
	}
	tt.Type("bts")
	hs.settle()
	hs.shot("03-quick-open")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if d := a.activeEditor(); d == nil || filepath.Base(d.path) != "b.ts" {
		t.Fatalf("quick open opened %v", d)
	}
	if a.quick.open {
		t.Error("quick open stayed open")
	}

	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("go.line")
	hs.settle()
	tt.Type("6")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	d := a.activeEditor()
	if line := d.buf.Snapshot().LineAt(d.caret); line != 5 {
		t.Errorf("caret on line %d, want 5", line)
	}
}

func TestFindAndReplace(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("edit.replace")
	hs.settle()
	tt.Type("main")
	hs.settle()
	if n := len(a.find.matches); n != 3 {
		t.Fatalf("%d matches of main, want 3", n)
	}
	hs.shot("04-find")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if a.find.cur != 1 {
		t.Errorf("current match %d after Enter", a.find.cur)
	}
	a.find.with = "entry"
	a.replaceAll(a.activeEditor())
	hs.settle()
	if got := a.activeEditor().buf.String(); strings.Contains(got, "main") || strings.Count(got, "entry") != 3 {
		t.Errorf("after replace all: %q", got)
	}
	// Regular expressions, with groups.
	a.find.regex, a.find.query, a.find.with, a.find.stale = true, `fmt\.(\w+)`, "log.$1", true
	a.replaceAll(a.activeEditor())
	if got := a.activeEditor().buf.String(); !strings.Contains(got, "log.Println") {
		t.Errorf("regexp replace: %q", got)
	}
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.find.open {
		t.Error("Escape left find open")
	}
}

func TestNewFileAndRenameFromTheTree(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.askNewEntry(hs.root, false)
	hs.settle()
	tt.Type("notes/todo.md")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if _, err := os.Stat(hs.path("notes/todo.md")); err != nil {
		t.Fatal(err)
	}
	if d := a.activeEditor(); d == nil || d.path != hs.path("notes/todo.md") {
		t.Fatalf("new file not open: %v", d)
	}
	a.askRename(hs.path("notes/todo.md"))
	hs.settle()
	hs.shot("05-rename")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("done.md")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if _, err := os.Stat(hs.path("notes/done.md")); err != nil {
		t.Fatal(err)
	}
	if d := a.activeEditor(); d.path != hs.path("notes/done.md") {
		t.Errorf("open document did not follow the rename: %s", d.path)
	}
}

func TestTabsKeepTheirState(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	tt.Type("x")
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	tt.Type("y")
	a.activate(a.column(), 0, true)
	hs.settle()
	// Undo in the first tab undoes its own edit, after the other tab's.
	tt.Key(ui.Cmd, ui.KeyZ)
	hs.settle()
	if got := a.docs[0].buf.String(); strings.HasPrefix(got, "x") {
		t.Errorf("undo in the first tab: %q", got[:10])
	}
	if !strings.HasPrefix(a.docs[1].buf.String(), "y") {
		t.Error("the second tab lost its edit")
	}
	a.cycleTab(1)
	if got := a.column().active; got != 1 {
		t.Errorf("active %d after the next tab", got)
	}
	hs.shot("06-tabs")
}

func TestPanelWithTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminal")
	hs.settle()
	if len(a.terms) != 1 || !a.window.PanelOpen {
		t.Fatalf("terminal: %d sessions, panel %v", len(a.terms), a.window.PanelOpen)
	}
	tt.Type("echo mayoi-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(a.terms[0].term.Text(), "mayoi-42") && time.Now().Before(deadline) {
		hs.settle()
	}
	if !strings.Contains(a.terms[0].term.Text(), "mayoi-42") {
		t.Errorf("terminal text %q", a.terms[0].term.Text())
	}
	a.run("view.newTerminal")
	hs.settle()
	if len(a.terms) != 2 || a.panelTerm != a.terms[1] {
		t.Errorf("second terminal: %d, the panel shows the first", len(a.terms))
	}
	hs.shot("07-terminal")
	a.closeTerminals()
}

func TestLargeFileStaysQuick(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	var b strings.Builder
	for i := range 100_000 {
		fmt.Fprintf(&b, "func f%d(x int) int { return x * %d } // line %d\n", i, i, i)
	}
	big := hs.path("big.go")
	os.WriteFile(big, []byte(b.String()), 0o644)
	start := time.Now()
	a.openFile(big, true)
	tt.Frame()
	t.Logf("open 100k lines: %v", time.Since(start))
	hs.settle()
	start = time.Now()
	const n = 100
	for range n {
		tt.Scroll(700, 400, 0, 120)
		tt.Frame()
	}
	t.Logf("scroll: %.2f ms/frame", float64(time.Since(start).Microseconds())/1000/n)
	start = time.Now()
	for range n {
		tt.Type("a")
		tt.Frame()
	}
	t.Logf("type: %.2f ms/frame", float64(time.Since(start).Microseconds())/1000/n)
	hs.settle()
	hs.shot("08-big-file")
}

// Showing and hiding the sidebar and the panel keeps the editor's state:
// its caret, its place and its undo history.
func TestLayoutChangesKeepEditorState(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	tt.Type("// edit\n")
	hs.settle()
	caret := a.activeEditor().caret
	a.run("view.sidebar")
	hs.settle()
	a.window.PanelOpen = true
	hs.settle()
	a.panelMax = true
	hs.settle()
	a.panelMax, a.window.PanelOpen = false, false
	a.run("view.sidebar")
	hs.settle()
	a.run("view.sidebarRight")
	hs.settle()
	a.run("view.sidebarRight")
	hs.settle()
	d := a.activeEditor()
	if d.caret != caret {
		t.Errorf("caret %d after layout changes, was %d", d.caret, caret)
	}
	d.vscroll.Focus()
	hs.settle()
	tt.Key(ui.Cmd, ui.KeyZ)
	hs.settle()
	if strings.Contains(d.buf.String(), "// edit") {
		t.Error("undo history lost across layout changes")
	}
}

func TestSplittersResize(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	w := a.settings.SidebarWidth
	x := w - 3 // the sidebar's right edge
	tt.Press(x, 300)
	tt.Move(x+40, 300)
	tt.Move(x+60, 300)
	tt.Release(x+60, 300)
	hs.settle()
	if got := a.settings.SidebarWidth; got < w+50 || got > w+70 {
		t.Errorf("sidebar %v after dragging 60 from %v", got, w)
	}
}

// On the right, the sidebar's splitter is along its left edge, and
// dragging it left widens it.
func TestSidebarOnTheRight(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.run("view.sidebarRight")
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	hs.shot("sidebar-right")
	w := a.settings.SidebarWidth
	x := 1200 - w + 2 // the sidebar's left edge
	tt.Press(x, 300)
	tt.Move(x-40, 300)
	tt.Move(x-60, 300)
	tt.Release(x-60, 300)
	hs.settle()
	if got := a.settings.SidebarWidth; got < w+50 || got > w+70 {
		t.Errorf("sidebar %v after dragging 60 left from %v", got, w)
	}
}

func TestRestoresTheActiveTab(t *testing.T) {
	root := workspaceFixture(t)
	// The tabs as a version without columns kept them.
	files := []string{filepath.Join(root, "main.go"), filepath.Join(root, "README.md"), filepath.Join(root, "go.mod")}
	data, _ := json.Marshal(map[string]any{"folder": root, "openFiles": files, "activeFile": files[1]})
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, data, 0o644)
	s := settings.Load(path)
	a := newApp(&fakeHost{folder: root}, s, s.Windows[0])
	tt := ui.NewTester(a.view, 1200, 760)
	tt.Frame()
	tt.Frame()
	if d := a.activeEditor(); d == nil || d.path != files[1] {
		t.Fatalf("active %v, want README.md", d)
	}
	tabs := a.column().tabs
	if len(a.cols) != 1 || len(tabs) != 3 || tabs[0].ed.path != files[0] || tabs[2].ed.path != files[2] {
		t.Fatalf("tabs out of order")
	}
}

func TestReopenClosedTab(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("README.md"), []byte(strings.Repeat("a line of text\n", 300)), 0o644)
	for _, f := range []string{"main.go", "README.md", "go.mod"} {
		a.openFile(hs.path(f), true)
	}
	hs.settle()
	a.activate(a.column(), 1, true)
	a.activeEditor().selectRange(3000, 3000, true)
	hs.settle()
	d := a.activeEditor()
	y, caret := d.vscroll.Y, d.caret
	if y == 0 || caret != 3000 {
		t.Fatalf("scrolled to %v, caret %d before closing", y, caret)
	}
	a.closeActive()
	hs.settle()
	if n := len(a.column().tabs); n != 2 {
		t.Fatalf("%d tabs after closing", n)
	}
	a.run("file.reopenClosed")
	hs.settle()
	tabs := a.column().tabs
	if len(tabs) != 3 || tabs[1].path() != hs.path("README.md") || a.column().active != 1 {
		t.Fatalf("reopened at %d of %d tabs", a.column().active, len(tabs))
	}
	if d := a.activeEditor(); d.vscroll.Y != y || d.caret != caret {
		t.Errorf("reopened scrolled to %v, caret %d; want %v, %d", d.vscroll.Y, d.caret, y, caret)
	}

	// Close All, then each comes back, the last closed first.
	a.run("file.reopenClosed") // nothing left: nothing happens
	a.closeTabs(slices.Clone(a.column().tabs), nil)
	hs.settle()
	if n := len(a.editorTabs()); n != 0 {
		t.Fatalf("%d tabs after Close All", n)
	}
	// A file gone from the disk is passed over.
	os.Remove(hs.path("go.mod"))
	a.run("file.reopenClosed")
	a.run("file.reopenClosed")
	a.run("file.reopenClosed")
	hs.settle()
	var got []string
	for _, t := range a.column().tabs {
		got = append(got, filepath.Base(t.path()))
	}
	if strings.Join(got, " ") != "main.go README.md" {
		t.Errorf("tabs %v after reopening", got)
	}

	// Another folder forgets them.
	a.closeActive()
	a.openFolder(t.TempDir())
	a.run("file.reopenClosed")
	if len(a.closed) != 0 || len(a.column().tabs) != 1 {
		t.Errorf("closed tabs kept across folders: %d", len(a.closed))
	}
}

func TestReopenClosedTabInItsColumn(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.splitRight()
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	right := a.column()
	a.closeActive()
	a.activate(a.cols[0], 0, true)
	a.run("file.reopenClosed")
	hs.settle()
	if a.column() != right || right.tab().path() != hs.path("README.md") || len(right.tabs) != 2 {
		t.Errorf("reopened in column %d, not the right one", a.col)
	}
}

func TestReopenClosedTabBringsBackItsColumn(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.openFile(hs.path("go.mod"), true)
	a.splitRight()
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	right := a.column()
	weight := right.weight
	// The right column goes with its last tab.
	a.closeActive()
	a.closeActive()
	hs.settle()
	if len(a.cols) != 1 {
		t.Fatalf("%d columns after closing the right one's tabs", len(a.cols))
	}
	a.run("file.reopenClosed")
	hs.settle()
	if len(a.cols) != 2 || a.col != 1 || a.column().weight != weight {
		t.Fatalf("%d columns, column %d has the keyboard", len(a.cols), a.col)
	}
	// The other tab closed from it goes back to it too, where it was.
	a.run("file.reopenClosed")
	hs.settle()
	var got []string
	for _, t := range a.cols[1].tabs {
		got = append(got, filepath.Base(t.path()))
	}
	if len(a.cols) != 2 || strings.Join(got, " ") != "go.mod README.md" {
		t.Errorf("%d columns, the right one has %v", len(a.cols), got)
	}
}

func TestReopenClosedTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminalTab")
	hs.settle()
	a.activate(a.column(), 0, true)
	a.closeTab(a.column().tabs[1], nil)
	hs.settle()
	a.run("file.reopenClosed")
	hs.settle()
	tabs := a.column().tabs
	if len(tabs) != 2 || tabs[1].term == nil || tabs[1].term.dir != hs.root {
		t.Errorf("terminal not reopened: %d tabs", len(tabs))
	}
	a.closeTerminals()
}
