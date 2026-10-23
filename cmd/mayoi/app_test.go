package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"mayoi/internal/bg"
	"mayoi/internal/mon"
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
	folder string
	// signedIn is the name settings sync syncs for, and signIns, syncs
	// and signOuts count the commands of settings sync.
	signedIn                 string
	signIns, syncs, signOuts int
	// sync is how settings sync shows, but for the name, and resolved
	// counts the conflicts said to be resolved.
	sync     syncInfo
	resolved int
	saveTo   string
	choice   int
	asked    []string
	title    string
	locale   string // the language setLocale chose
	closed   bool
	trashed  []string
	// revealed are the paths asked to show in the Finder.
	revealed []string
	// others are the folders other windows have open, and windows those
	// asked to open in a window of their own.
	others  []string
	windows []string
	// blankWindows counts the windows asked for that show nothing.
	blankWindows int
	// folders are those asked to open as `mayoi <dir>` opens them.
	folders []string
	// recentChanges counts the changes of the recent folders.
	recentChanges int
	// clipboard are the files on the clipboard, and image its image.
	clipboard []string
	image     []byte
	// chosen are the paths the dialog of choosePaths answers.
	chosen []string
	// aiClient is what ai returns, and aiSignOuts counts the tokens Mon
	// refused.
	aiClient   *mon.Client
	aiSignOuts int
	// newVersion is the update a check found, and updateOpens counts the
	// update windows opened.
	newVersion  string
	updateOpens int
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
func (h *fakeHost) clipboardImage() []byte   { return h.image }
func (h *fakeHost) choosePaths(title, _, _ string, _ bool, done func([]string)) {
	h.asked = append(h.asked, title)
	if len(h.chosen) > 0 {
		done(h.chosen)
	}
}
func (h *fakeHost) closeWindow()         { h.closed = true }
func (h *fakeHost) setTheme(_, _ string) {}
func (h *fakeHost) setLocale(tag string) { h.locale = tag }
func (h *fakeHost) showFolder(dir string) bool {
	return slices.Contains(h.others, dir)
}
func (h *fakeHost) newWindow(dir string)      { h.windows = append(h.windows, dir) }
func (h *fakeHost) newBlankWindow()           { h.blankWindows++ }
func (h *fakeHost) openFolders(dirs []string) { h.folders = append(h.folders, dirs...) }
func (h *fakeHost) recentChanged()            { h.recentChanges++ }
func (h *fakeHost) menuChanged()              {}
func (h *fakeHost) fontChanged()              {}
func (h *fakeHost) installCLI()               {}
func (h *fakeHost) uninstallCLI()             {}
func (h *fakeHost) account() string           { return h.signedIn }
func (h *fakeHost) signIn()                   { h.signIns++ }
func (h *fakeHost) signOut()                  { h.signOuts++ }
func (h *fakeHost) syncNow()                  { h.syncs++ }
func (h *fakeHost) syncInfo() syncInfo {
	if h.signedIn == "" {
		return syncInfo{}
	}
	si := h.sync
	si.name = h.signedIn
	return si
}
func (h *fakeHost) syncPicture() *ui.Bitmap { return nil }
func (h *fakeHost) syncConflictResolved()   { h.resolved++ }
func (h *fakeHost) ai() *mon.Client         { return h.aiClient }
func (h *fakeHost) aiSignedOut()            { h.aiSignOuts++ }
func (h *fakeHost) update() string          { return h.newVersion }
func (h *fakeHost) installUpdate()          { h.updateOpens++ }
func (h *fakeHost) stepProject(int)         {}
func (h *fakeHost) showProject(int)         {}
func (h *fakeHost) closeProject()           {}
func (h *fakeHost) projectTabsChanged()     {}

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
	// held counts the background work a test holds on purpose, which
	// settle does not wait for.
	held int64
}

func newHarness(t testing.TB, dark bool) *harness {
	root := workspaceFixture(t)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	h := &fakeHost{folder: root}
	a := newApp(h, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	tt.SetDark(dark)
	hs := &harness{t: t, a: a, h: h, tt: tt, root: root}
	hs.settle()
	return hs
}

// step draws a frame of a test a frame's time, 16 ms, after the last:
// the tester's clock stands still otherwise, and the app counts its frames
// by the time they are drawn at.
func step(tt *ui.Tester) {
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
}

// settle draws frames until the background work (reading directories,
// highlighting, indexing, git) is done: half a second of the tester's
// clock, which the animations, the double clicks and the waits of the app
// count by, and frames at the same time for as long as some is running or
// posted, which on a busy machine is longer than on an idle one. While
// work only runs, no frame is drawn, to leave the machine to it. Idle
// counts for two frames in a row, as a frame can start work that one
// finished asks for.
func (hs *harness) settle() {
	for range 30 {
		step(hs.tt)
	}
	for idle, deadline := 0, time.Now().Add(10*time.Second); idle < 2 && time.Now().Before(deadline); {
		running, queued := hs.busy()
		switch {
		case queued:
			idle = 0
			hs.tt.Frame()
		case running:
			idle = 0
			time.Sleep(time.Millisecond)
		default:
			idle++
			hs.tt.Frame()
		}
	}
	if !hs.idle() {
		hs.t.Logf("settle: %d background jobs still run after 10s", bg.Running()-hs.held)
	}
	step(hs.tt)
}

// busy reports whether background work runs, and whether some it posted
// waits for a frame. Whether the view asks for a frame does not count: it
// always does while something on screen moves.
func (hs *harness) busy() (running, queued bool) {
	hs.a.mu.Lock()
	n := len(hs.a.queue)
	hs.a.mu.Unlock()
	return bg.Running() > hs.held, n > 0
}

// idle reports whether no background work runs and none posted waits.
func (hs *harness) idle() bool {
	running, queued := hs.busy()
	return !running && !queued
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

// TestStatusBarDropsTheLeastNeededFirst: as the window narrows, the
// status bar's parts go one by one, the least needed first, each only
// once the bar would not hold it, and those left fit.
func TestStatusBarDropsTheLeastNeededFirst(t *testing.T) {
	skipOnWindows(t, "Windows' narrower font holds the branch at 300")
	hs := newHarness(t, true)
	git := hs.gitRepo()
	branch := "feature/responsive-title-bar-polish"
	git("checkout", "-q", "-b", branch)
	hs.a.openFile(hs.path("main.go"), true)
	hs.settleGit()
	// In the order they go.
	parts := []string{"UTF-8", revealFolderLabel(shortenHome(hs.root)), "9 lines", "LF", "Tabs", "Go", branch}
	gone := 0
	for w := 1200; w >= 300; w -= 20 {
		hs.tt.SetSize(w, 400)
		hs.settle()
		for i, s := range parts {
			box, shown := hs.tt.Find(s)
			switch {
			case !shown:
				gone = max(gone, i+1)
			case i < gone:
				t.Errorf("at %d, %q is back", w, s)
			case box.X+box.W > float32(w):
				t.Errorf("at %d, %q does not fit", w, s)
			}
		}
		if box, ok := hs.tt.Find("Ln 1, Col 1"); !ok || box.X+box.W > float32(w) {
			t.Fatalf("at %d the position does not fit: %v", w, box)
		}
		switch {
		case w == 760 && gone > 0:
			t.Errorf("at 760, with room for it, the bar leaves out %q", parts[gone-1])
		case w == 300 && gone < len(parts):
			t.Errorf("at 300 the bar keeps %q", parts[gone:])
		}
	}
}

// TestCommandCenterFitsNarrowWindow: the command center fits the title
// bar, and leaves out the folder before it cuts the name short.
func TestCommandCenterFitsNarrowWindow(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.openFile(hs.path("main.go"), true)
	folder := filepath.Base(hs.root)
	hs.tt.SetSize(760, 400)
	hs.settle()
	if _, ok := hs.tt.Find(folder); !ok {
		t.Error("the command center leaves out the folder though it has room")
	}
	hs.tt.SetSize(360, 400)
	hs.settle()
	box, ok := hs.tt.Find("Quick Open")
	if !ok || box.X < 0 || box.X+box.W > 360 {
		t.Errorf("the command center does not fit the title bar: %v", box)
	}
	name, ok := hs.tt.Find("main.go")
	if !ok || name.X < box.X || name.X+name.W > box.X+box.W {
		t.Errorf("the name is not in the command center: %v in %v", name, box)
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
	// As the Explorer spells it, with Windows' backslashes there, so that
	// the file opens once whichever way it is opened.
	if d := a.activeEditor(); d.path != filepath.Clean(d.path) {
		t.Errorf("quick open opened %q, not %q", d.path, filepath.Clean(d.path))
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
	step(tt)
	t.Logf("open 100k lines: %v", time.Since(start))
	hs.settle()
	start = time.Now()
	const n = 100
	for range n {
		tt.Scroll(700, 400, 0, 120)
		step(tt)
	}
	t.Logf("scroll: %.2f ms/frame", float64(time.Since(start).Microseconds())/1000/n)
	start = time.Now()
	for range n {
		tt.Type("a")
		step(tt)
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
	w := a.window.SidebarWidth
	x := w - 3 // the sidebar's right edge
	tt.Press(x, 300)
	tt.Move(x+40, 300)
	tt.Move(x+60, 300)
	tt.Release(x+60, 300)
	hs.settle()
	if got := a.window.SidebarWidth; got < w+50 || got > w+70 {
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
	w := a.window.SidebarWidth
	x := 1200 - w + 2 // the sidebar's left edge
	tt.Press(x, 300)
	tt.Move(x-40, 300)
	tt.Move(x-60, 300)
	tt.Release(x-60, 300)
	hs.settle()
	if got := a.window.SidebarWidth; got < w+50 || got > w+70 {
		t.Errorf("sidebar %v after dragging 60 left from %v", got, w)
	}
}

// The terminal panel moves to either side of the window, past the
// sidebar, so that with both on a side the panel is the outer one, and
// its splitter, along the edge facing the editors, resizes it.
func TestTerminalPanelAtASide(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	at := func(name string) ui.Rect {
		box, ok := tt.Find(name)
		if !ok {
			t.Fatalf("%s is not shown", name)
		}
		return box
	}
	// The title bar's buttons: the terminal's follows the panel's side,
	// past the sidebar's button when the sidebar is on that side too.
	termBtn, sideBtn := a.withKey("Toggle Terminal", "view.terminal"), a.withKey("Toggle Sidebar", "view.sidebar")
	buttons := func() (term, side ui.Rect) { return at(termBtn), at(sideBtn) }
	width := float32(1200)
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminal")
	hs.settle()
	// At the bottom, the panel is under the editors, right of the sidebar,
	// and its button is at the right of the bar, the sidebar's at the left.
	if panel, explorer := at("TERMINAL"), at("EXPLORER"); panel.Y < explorer.Y || panel.X < explorer.X {
		t.Errorf("the panel at the bottom: %v, the sidebar %v", panel, explorer)
	}
	if term, side := buttons(); term.X < width/2 || side.X > width/2 {
		t.Errorf("the buttons at the bottom: the terminal's %v, the sidebar's %v", term, side)
	}
	// On the left, past the sidebar, at the window's own edge, its button
	// left of the sidebar's.
	a.run("view.panelPosition.left")
	hs.settle()
	if panel, explorer := at("TERMINAL"), at("EXPLORER"); panel.X > explorer.X || panel.X > 40 {
		t.Errorf("the panel on the left: %v, the sidebar %v", panel, explorer)
	}
	if term, side := buttons(); term.X > side.X {
		t.Errorf("the buttons with the panel on the left: the terminal's %v, the sidebar's %v", term, side)
	}
	// Its splitter is on its right, and dragging it right widens it.
	size := a.window.PanelWidth
	edge := size - 2
	tt.Press(edge, 300)
	tt.Move(edge+40, 300)
	tt.Move(edge+60, 300)
	tt.Release(edge+60, 300)
	hs.settle()
	if got := a.window.PanelWidth; got < size+50 || got > size+70 {
		t.Errorf("the panel %v after dragging 60 from %v", got, size)
	}
	// With the sidebar on the right, the panel on the left keeps the left
	// of the bar, and its button with it.
	a.run("view.sidebarRight")
	hs.settle()
	if term, side := buttons(); term.X > width/2 || side.X < width/2 {
		t.Errorf("the buttons with the panel left, the sidebar right: the terminal's %v, the sidebar's %v", term, side)
	}
	// The panel on the right, with the sidebar there too, is still the
	// outer one, at the window's own edge, and its button is at the right.
	a.run("view.panelPosition.right")
	hs.settle()
	panel, explorer := at("TERMINAL"), at("EXPLORER")
	if panel.X < explorer.X || panel.X < width-a.window.PanelWidth || panel.X > width-a.window.PanelWidth+40 {
		t.Errorf("the panel on the right: %v, the sidebar %v, width %v", panel, explorer, a.window.PanelWidth)
	}
	if term, _ := buttons(); term.X < width/2 {
		t.Errorf("the terminal's button with the panel on the right: %v", term)
	}
	// In a narrow window it gives way, so the editors keep 320 points
	// beside the sidebar, and takes its width back as the window widens.
	a.run("view.sidebarRight")
	a.run("view.panelPosition.left")
	hs.tt.SetSize(760, 800)
	hs.settle()
	if hide := at("Hide Panel"); hide.X+hide.W > 760-320-a.window.SidebarWidth {
		t.Errorf("the panel in a narrow window reaches %v, past %v", hide.X+hide.W, 760-320-a.window.SidebarWidth)
	}
	hs.tt.SetSize(int(width), 800)
	hs.settle()
	if hide := at("Hide Panel"); hide.X+hide.W < a.window.PanelWidth-40 {
		t.Errorf("the panel widened back to %v, not %v", hide.X+hide.W, a.window.PanelWidth)
	}
	// Hidden, moving it shows it again.
	a.hidePanel()
	hs.settle()
	if a.window.PanelOpen {
		t.Fatal("the panel stays open after hiding it")
	}
	a.run("view.panelPosition.left")
	hs.settle()
	if !a.window.PanelOpen || a.settings.PanelPosition != "left" {
		t.Fatalf("a hidden panel moved: open %v, position %q", a.window.PanelOpen, a.settings.PanelPosition)
	}
}

// Maximized, the panel takes the editors' room wherever it is, past the
// sidebar: at the bottom too, where it is in the editors' column.
func TestMaximizedPanelFillsTheWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	width := float32(1200)
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminal")
	hs.settle()
	for _, pos := range []string{"bottom", "left", "right"} {
		a.run("view.panelPosition." + pos)
		a.panelMax = true
		hs.settle()
		title, ok := tt.Find("TERMINAL")
		hide, ok2 := tt.Find("Hide Panel")
		if !ok || !ok2 {
			t.Fatalf("the panel maximized at the %s is not shown", pos)
		}
		// The sidebar is on the left, between a panel there and the
		// editors' room.
		left, right := a.window.SidebarWidth, width
		if pos == "left" {
			left, right = 0, width-a.window.SidebarWidth
		}
		if title.X < left || title.X > left+40 || hide.X+hide.W < right-40 || hide.X+hide.W > right {
			t.Errorf("the panel maximized at the %s: its title %v, its last button %v", pos, title, hide)
		}
		a.panelMax = false
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
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	step(tt)
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

// With terminalLocation "editor", New Terminal opens a tab of the editor
// area, while Toggle Terminal still shows the panel. Set back, New
// Terminal opens in the panel again.
func TestTerminalLocation(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminalLocation")
	if a.settings.TerminalLocation != "editor" {
		t.Fatalf("terminalLocation %q after the toggle", a.settings.TerminalLocation)
	}
	a.run("view.newTerminal")
	hs.settle()
	tabs := a.column().tabs
	if len(tabs) != 2 || tabs[1].term == nil || tabs[1].term.dir != hs.root || a.column().active != 1 {
		t.Fatalf("no terminal tab: %d tabs", len(tabs))
	}
	if a.panelTerm != nil || a.window.PanelOpen {
		t.Errorf("the panel opened too: open %v", a.window.PanelOpen)
	}
	a.openTerminal(filepath.Join(hs.root, "web"))
	hs.settle()
	if tabs := a.column().tabs; len(tabs) != 3 || tabs[2].term == nil || tabs[2].term.dir != filepath.Join(hs.root, "web") {
		t.Fatalf("Open in Terminal: %d tabs", len(tabs))
	}
	a.run("view.terminal")
	hs.settle()
	if a.panelTerm == nil || !a.window.PanelOpen {
		t.Fatal("Toggle Terminal didn't show the panel")
	}
	a.run("view.terminalLocation")
	panel := a.panelTerm
	a.run("view.newTerminal")
	hs.settle()
	if a.settings.TerminalLocation != "" || a.panelTerm == panel || len(a.column().tabs) != 3 {
		t.Errorf("New Terminal with terminalLocation %q: %d tabs", a.settings.TerminalLocation, len(a.column().tabs))
	}
	a.closeTerminals()
}

// The panel's header has a menu that moves the panel to a side, and back.
func TestTerminalPanelHeaderMenu(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.run("view.terminal")
	hs.settle()
	if err := tt.RightClick("TERMINAL"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "Panel Position") || !slices.Contains(menu, "Hide Panel") {
		t.Fatalf("the panel's menu: %q", menu)
	}
	if err := tt.ChooseMenuItem("Panel Position", "Right"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.panelSide() != "right" || !a.window.PanelOpen {
		t.Fatalf("the panel is at %s, open %v", a.panelSide(), a.window.PanelOpen)
	}
	if err := tt.RightClick("TERMINAL"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Panel Position", "Bottom"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.panelSide() != "bottom" || a.settings.PanelPosition != "" {
		t.Fatalf("the panel is at %s (%q)", a.panelSide(), a.settings.PanelPosition)
	}
}

// Off macOS the title bar's menu has the menu bar's menus, without the
// edit roles it cannot send to the focused element, and runs their items.
func TestTitleBarMenu(t *testing.T) {
	onOS(t, "linux")
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	if err := tt.Click("Menu"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "File") || !slices.Contains(menu, "View") {
		t.Fatalf("the title bar's menu: %q", menu)
	}
	if err := tt.ChooseMenuItem("Edit", "Copy"); err == nil {
		t.Error("Copy is in the Edit menu")
	}
	if err := tt.Click("Menu"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("View", "Word Wrap"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.settings.WordWrap {
		t.Error("Word Wrap is not on")
	}
}
