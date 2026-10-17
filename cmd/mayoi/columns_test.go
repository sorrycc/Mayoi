package main

import (
	"fmt"
	"image"
	"mayoi/internal/settings"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// longFile writes a Go file of n functions in the workspace, for views
// that scroll.
func (hs *harness) longFile(name string, n int) string {
	var b strings.Builder
	b.WriteString("package long\n\n")
	for i := range n {
		fmt.Fprintf(&b, "func f%d() int {\n\treturn %d\n}\n\n", i, i)
	}
	p := hs.path(name)
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		hs.t.Fatal(err)
	}
	return p
}

// splitHarness opens a file and splits it into two columns, the keyboard in the
// right one.
func splitHarness(t *testing.T, path func(hs *harness) string) (*harness, *editor, *editor) {
	t.Helper()
	hs := newHarness(t, true)
	hs.a.openFile(path(hs), true)
	hs.settle()
	hs.a.run("view.splitRight")
	hs.settle()
	if len(hs.a.cols) != 2 || hs.a.col != 1 {
		t.Fatalf("split: %d columns, the keyboard in %d", len(hs.a.cols), hs.a.col)
	}
	return hs, hs.a.cols[0].tab().ed, hs.a.cols[1].tab().ed
}

func TestSplitRightSharesTheDocument(t *testing.T) {
	hs, left, right := splitHarness(t, func(hs *harness) string { return hs.path("main.go") })
	a, tt := hs.a, hs.tt
	if left == right || left.document != right.document || len(a.docs) != 1 {
		t.Fatalf("the columns do not share a document: %d documents", len(a.docs))
	}
	// The caret of the left column, after the text typed in the right one,
	// moves with its text.
	at := strings.Index(left.buf.String(), "func main")
	hs.place(left, at, at+4)
	right.vscroll.Focus()
	hs.settle()
	tt.Type("// hi\n")
	hs.settle()
	if got := left.buf.String(); !strings.HasPrefix(got, "// hi\npackage main") {
		t.Fatalf("text %q", got[:20])
	}
	expectSel(t, left, at+6, at+10)
	expectSel(t, right, 6, 6)
	if got := left.buf.Slice(left.sel[0], left.sel[1]); got != "func" {
		t.Errorf("the left column selects %q", got)
	}
	// Undo is the document's: taken back from the left column, where the
	// selection goes to where the typing was.
	left.vscroll.Focus()
	hs.settle()
	hs.key(ui.Cmd, ui.KeyZ)
	if got := left.buf.String(); !strings.HasPrefix(got, "package main") {
		t.Fatalf("undone to %q", got[:20])
	}
	expectSel(t, left, 0, 0)
	expectSel(t, right, 0, 0)
	hs.shot("09-columns")
}

func TestOtherColumnStaysWhereItIsScrolled(t *testing.T) {
	hs, left, right := splitHarness(t, func(hs *harness) string { return hs.longFile("long.go", 400) })
	a, tt := hs.a, hs.tt
	// The left column reads far from its caret, as the wheel leaves it.
	left.vscroll.Y = 6000
	hs.settle()
	if left.vscroll.Y != 6000 {
		t.Fatalf("scrolled to %v", left.vscroll.Y)
	}
	for _, text := range []string{"x", "\n", "y"} {
		tt.Type(text)
		for range 4 {
			step(tt)
			if left.vscroll.Y != 6000 {
				t.Fatalf("typing %q in the right column scrolled the left to %v", text, left.vscroll.Y)
			}
		}
	}
	expectSel(t, left, 0, 0)
	expectSel(t, right, 3, 3)
	// Nor does the keyboard coming back scroll it; typing then does, to
	// the caret.
	a.run("view.prevColumn")
	hs.settle()
	if a.col != 0 || left.vscroll.Y != 6000 {
		t.Fatalf("back in column %d, scrolled to %v", a.col, left.vscroll.Y)
	}
	tt.Type("z")
	hs.settle()
	if !strings.HasPrefix(left.buf.String(), "zx\ny") || left.vscroll.Y != 0 {
		t.Fatalf("typed %q, scrolled to %v", left.buf.String()[:6], left.vscroll.Y)
	}
}

func TestHiddenTabStaysWhereItIsScrolled(t *testing.T) {
	hs, left, _ := splitHarness(t, func(hs *harness) string { return hs.longFile("long.go", 400) })
	a, tt := hs.a, hs.tt
	left.vscroll.Y = 6000
	hs.settle()
	// Another tab covers it in the left column, while the right one edits.
	a.col = 0
	a.openFile(hs.path("README.md"), false)
	a.col = 1
	a.focusColumn()
	hs.settle()
	tt.Type("x\n")
	hs.settle()
	a.activate(a.cols[0], 0, false)
	hs.settle()
	if left.vscroll.Y != 6000 {
		t.Fatalf("back at %v", left.vscroll.Y)
	}
}

// TestHiddenTabComesBackInPlace: a tab shown again builds a new text area,
// which scrolls to its caret before the editor puts it back: no frame
// shows the text anywhere else.
func TestHiddenTabComesBackInPlace(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.longFile("long.go", 400), true)
	hs.settle()
	d := a.activeEditor()
	d.vscroll.Y = 6000
	hs.settle()
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	a.activate(a.column(), 0, true)
	for range 3 {
		step(tt)
		if d.vscroll.Y == 6000 {
			return
		}
		box, ok := tt.Find("Editor long.go")
		if !ok {
			t.Fatal("no editor")
		}
		img := tt.Image()
		r := image.Rect(int(box.X), int(box.Y), int(box.X+box.W), int(box.Y+box.H)).Intersect(img.Bounds())
		first := img.At(r.Min.X, r.Min.Y)
		for y := r.Min.Y; y < r.Max.Y; y += 3 {
			for x := r.Min.X; x < r.Max.X; x += 3 {
				if img.At(x, y) != first {
					t.Fatalf("a frame shows the tab scrolled to %v", d.vscroll.Y)
				}
			}
		}
	}
	t.Fatalf("back at %v", d.vscroll.Y)
}

func TestClosingAsksForTheLastTabOfADocument(t *testing.T) {
	hs, _, right := splitHarness(t, func(hs *harness) string { return hs.path("README.md") })
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false // the prompts are for files that do not save themselves
	tt.Type("changed ")
	hs.settle()
	if !right.dirty() {
		t.Fatal("not changed")
	}
	// The other column still shows it: nothing is lost.
	hs.h.choice = 2
	a.run("file.close")
	hs.settle()
	if len(a.cols) != 1 || len(a.docs) != 1 || len(hs.h.asked) != 0 {
		t.Fatalf("%d columns, %d documents, asked %v", len(a.cols), len(a.docs), hs.h.asked)
	}
	a.run("file.close")
	hs.settle()
	if len(a.docs) != 1 || len(hs.h.asked) != 1 {
		t.Fatalf("the last tab: %d documents, asked %v", len(a.docs), hs.h.asked)
	}
	hs.h.choice = 1
	a.run("file.close")
	hs.settle()
	if len(a.docs) != 0 || len(a.cols) != 1 || len(a.cols[0].tabs) != 0 {
		t.Fatalf("closed: %d documents, %d columns", len(a.docs), len(a.cols))
	}
	if !tt.HasText("a small, quick code editor") {
		t.Error("no welcome page after the last tab")
	}
}

func TestColumnsKeepTheirTabs(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	// The tab moves to a column of its own, past the last.
	a.run("view.moveTabNext")
	hs.settle()
	if len(a.cols) != 2 || a.col != 1 || a.cols[0].tab().name() != "main.go" || a.cols[1].tab().name() != "README.md" {
		t.Fatalf("moved: %d columns, keyboard in %d", len(a.cols), a.col)
	}
	// Each column takes its own keys.
	tt.Type("r")
	hs.settle()
	a.run("view.prevColumn")
	hs.settle()
	tt.Type("m")
	hs.settle()
	if m, r := a.cols[0].tab().ed.buf.String(), a.cols[1].tab().ed.buf.String(); !strings.HasPrefix(m, "mpackage") || !strings.HasPrefix(r, "r# Mayoi") {
		t.Fatalf("typed %q and %q", m[:8], r[:8])
	}
	// A file opens in the column that has the keyboard, and the same file
	// in the other one too.
	a.openFile(hs.path("go.mod"), true)
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	if len(a.cols[0].tabs) != 3 || len(a.docs) != 3 || len(a.docs[1].views) != 2 {
		t.Fatalf("%d tabs, %d documents", len(a.cols[0].tabs), len(a.docs))
	}
	// Moved to a column that shows its document, a tab joins the one there.
	a.run("view.moveTabNext")
	hs.settle()
	if len(a.cols[0].tabs) != 2 || len(a.cols[1].tabs) != 1 || a.col != 1 || len(a.docs[1].views) != 1 {
		t.Fatalf("joined: %d and %d tabs, keyboard in %d", len(a.cols[0].tabs), len(a.cols[1].tabs), a.col)
	}
	if got := a.cols[1].tab().ed.buf.String(); !strings.HasPrefix(got, "r# Mayoi") {
		t.Errorf("the tab kept shows %q", got[:8])
	}
	// The column goes with its last tab, and the keyboard to the other.
	hs.h.choice = 1
	a.run("file.close")
	hs.settle()
	if len(a.cols) != 1 || a.col != 0 || len(a.docs) != 2 {
		t.Fatalf("closed: %d columns, %d documents", len(a.cols), len(a.docs))
	}
	tt.Type("k")
	hs.settle()
	if got := a.activeEditor().buf.String(); !strings.HasPrefix(got, "k") {
		t.Errorf("the keyboard is not in the column left: %q", got[:8])
	}
}

func TestColumnsShareTheWidth(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.openFile(hs.path("README.md"), true)
	a.run("view.moveTabNext")
	hs.settle()
	box := func(name string) ui.Rect {
		r, ok := tt.Find("Editor " + name)
		if !ok {
			t.Fatalf("no editor of %s: %q", name, tt.Texts())
		}
		return r
	}
	m, r := box("main.go"), box("README.md")
	if d := m.W - r.W; d < -2 || d > 2 || r.X < m.X+m.W {
		t.Fatalf("columns of %v and %v", m, r)
	}
	// The handle between them moves the width from one to the other.
	x := r.X + 1
	tt.Press(x, 300)
	tt.Move(x+50, 300)
	tt.Move(x+100, 300)
	tt.Release(x+100, 300)
	hs.settle()
	m2, r2 := box("main.go"), box("README.md")
	if d := m2.W - m.W; d < 90 || d > 110 || r2.W > r.W-90 {
		t.Fatalf("dragged 100: %v and %v, from %v and %v", m2, r2, m, r)
	}
	hs.shot("10-columns-resized")
}

func TestRestoresColumns(t *testing.T) {
	root := workspaceFixture(t)
	file := func(name string) string { return filepath.Join(root, name) }
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Columns = []settings.Column{
		{Tabs: []settings.Tab{{File: file("main.go")}, {File: file("README.md")}}, Active: 0, Weight: 2},
		{Tabs: []settings.Tab{{File: file("gone.go")}}, Active: 0, Weight: 1},
		{Tabs: []settings.Tab{{File: file("README.md")}, {File: file("go.mod")}}, Active: 1, Weight: 1},
	}
	w.Column = 2
	a := newApp(&fakeHost{folder: root}, s, w)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	step(tt)
	if len(a.cols) != 2 || a.col != 1 || len(a.docs) != 3 {
		t.Fatalf("%d columns, keyboard in %d, %d documents", len(a.cols), a.col, len(a.docs))
	}
	if l, r := a.cols[0], a.cols[1]; l.tab().name() != "main.go" || r.tab().name() != "go.mod" || l.weight != 2 || len(l.tabs) != 2 || len(r.tabs) != 2 {
		t.Fatalf("columns of %d and %d tabs, showing %s and %s", len(l.tabs), len(r.tabs), l.tab().name(), r.tab().name())
	}
	if got := w.Columns; len(got) != 2 || got[1].Tabs[1].File != file("go.mod") || got[1].Active != 1 || w.Column != 1 {
		t.Fatalf("remembered %+v", got)
	}
}

// A file the app is started with opens in the columns of the last
// session, in the one that had the keyboard.
func TestFileGivenAtStartJoinsTheSession(t *testing.T) {
	root := workspaceFixture(t)
	file := func(name string) string { return filepath.Join(root, name) }
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Columns = []settings.Column{
		{Tabs: []settings.Tab{{File: file("main.go")}}, Weight: 1},
		{Tabs: []settings.Tab{{File: file("go.mod")}}, Weight: 1},
	}
	a := newApp(&fakeHost{folder: root}, s, w)
	tt := ui.NewTester(a.view, 1200, 760)
	a.post(func() { a.openFile(file("main.go"), true) })
	a.post(func() { a.openFile(file("README.md"), true) })
	step(tt)
	step(tt)
	if len(a.cols) != 2 || len(a.cols[0].tabs) != 2 || len(a.cols[1].tabs) != 1 || a.activeEditor().name() != "README.md" {
		t.Fatalf("%d columns, of %d tabs first", len(a.cols), len(a.cols[0].tabs))
	}
}

// drag presses at one place, and releases at another, as dragging a tab
// does.
func (hs *harness) drag(x0, y0, x1, y1 float32) {
	tt := hs.tt
	tt.Press(x0, y0)
	step(tt)
	for i := 1; i <= 4; i++ {
		f := float32(i) / 4
		tt.Move(x0+(x1-x0)*f, y0+(y1-y0)*f)
		step(tt)
	}
	step(tt)
	tt.Release(x1, y1)
	hs.settle()
}

func TestDraggingTabs(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	for _, name := range []string{"main.go", "README.md", "go.mod"} {
		a.openFile(hs.path(name), true)
	}
	hs.settle()
	names := func(col *column) string {
		var out []string
		for _, t := range col.tabs {
			out = append(out, t.name())
		}
		return strings.Join(out, " ")
	}
	// Along the strip: the last tab before the first.
	x0, y0 := hs.tabCenter("go.mod")
	x1, y1 := hs.tabCenter("main.go")
	hs.drag(x0, y0, x1-20, y1)
	if got := names(a.cols[0]); got != "go.mod main.go README.md" || a.cols[0].tab().name() != "go.mod" {
		t.Fatalf("reordered to %q", got)
	}
	// To the right side of the editor: a column of its own.
	body, ok := hs.tt.Find("Editor go.mod")
	if !ok {
		t.Fatal("no editor")
	}
	x0, y0 = hs.tabCenter("go.mod")
	hs.drag(x0, y0, body.X+body.W-30, body.Y+body.H/2)
	if len(a.cols) != 2 || names(a.cols[0]) != "main.go README.md" || names(a.cols[1]) != "go.mod" || a.col != 1 {
		t.Fatalf("split to %d columns: %q", len(a.cols), names(a.cols[0]))
	}
	hs.shot("11-dragged")
	// Into the other column, between its tabs.
	x0, y0 = hs.tabCenter("go.mod")
	x1, y1 = hs.tabCenter("README.md")
	hs.drag(x0, y0, x1-10, y1)
	if len(a.cols) != 1 || names(a.cols[0]) != "main.go go.mod README.md" {
		t.Fatalf("joined to %d columns: %q", len(a.cols), names(a.cols[0]))
	}
	// Onto the middle of another column: its last tab.
	a.run("view.moveTabNext")
	hs.settle()
	body, _ = hs.tt.Find("Editor README.md")
	x0, y0 = hs.tabCenter("go.mod")
	hs.drag(x0, y0, body.X+body.W/2, body.Y+body.H/2)
	if len(a.cols) != 1 || names(a.cols[0]) != "main.go README.md go.mod" {
		t.Fatalf("dropped in the middle: %d columns, %q", len(a.cols), names(a.cols[0]))
	}
	if a.dragging {
		hs.settle()
		if a.dragging {
			t.Error("still dragging")
		}
	}
}

// tabCenter returns a place on the tab of a name, beside its cross: the
// explorer and the title bar show the name too.
func (hs *harness) tabCenter(name string) (x, y float32) {
	hs.t.Helper()
	r, ok := hs.tt.Find("Close " + name)
	if !ok {
		hs.t.Fatalf("no tab %q", name)
	}
	return r.X - 24, r.Y + r.H/2
}

func TestTerminalInAColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("view.terminalTab")
	hs.settle()
	col := a.column()
	if len(a.terms) != 1 || len(col.tabs) != 2 || col.tab().term != a.terms[0] || a.window.PanelOpen {
		t.Fatalf("%d terminals, %d tabs, panel %v", len(a.terms), len(col.tabs), a.window.PanelOpen)
	}
	s := a.terms[0]
	says := func(text string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(s.term.Text(), text) && time.Now().Before(deadline) {
			hs.settle()
		}
		if !strings.Contains(s.term.Text(), text) {
			t.Fatalf("terminal text %q lacks %q", s.term.Text(), text)
		}
	}
	tt.Type("echo tab-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	says("tab-42")
	hs.shot("12-terminal-tab")
	// To the panel, where it goes on, and back to a column of its own.
	a.run("view.terminalMove")
	hs.settle()
	if s.tab != nil || a.panelTerm != s || !a.window.PanelOpen || len(col.tabs) != 1 || a.focus != paneTerminal {
		t.Fatalf("in the panel: tab %v, panel %v, %d tabs", s.tab, a.window.PanelOpen, len(col.tabs))
	}
	tt.Type("echo panel-$((40+3))")
	tt.Key(0, ui.KeyEnter)
	says("panel-43")
	a.run("view.terminalMove")
	hs.settle()
	if s.tab == nil || a.panelTerm != nil || a.window.PanelOpen || len(col.tabs) != 2 {
		t.Fatalf("back in a tab: tab %v, panel %v, %d tabs", s.tab, a.window.PanelOpen, len(col.tabs))
	}
	a.run("view.moveTabNext")
	hs.settle()
	if len(a.cols) != 2 || a.cols[1].tab().term != s {
		t.Fatalf("%d columns", len(a.cols))
	}
	tt.Type("echo column-$((40+4))")
	tt.Key(0, ui.KeyEnter)
	says("column-44")
	hs.shot("13-terminal-column")
	// The editor beside it is as it was, through the tabs coming and going.
	if ed := a.cols[0].tab().ed; ed.sel != [2]int{} || ed.vscroll.Y != 0 {
		t.Errorf("the editor's selection is %v, scrolled to %v", ed.sel, ed.vscroll.Y)
	}
	// Closing its tab ends it, and its column.
	a.run("file.close")
	hs.settle()
	if len(a.terms) != 0 || len(a.cols) != 1 {
		t.Fatalf("closed: %d terminals, %d columns", len(a.terms), len(a.cols))
	}
	a.closeTerminals()
}

func TestFindFollowsTheColumn(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.openFile(hs.path("internal/a/a.go"), true)
	a.run("view.moveTabNext")
	hs.settle()
	a.run("edit.find")
	hs.settle()
	tt.Type("package")
	hs.settle()
	right := a.cols[1].tab().ed
	if n := len(a.find.matches); n != 1 || a.find.doc != right.id || right.sel != [2]int{0, 7} {
		t.Fatalf("%d matches in document %d, selecting %v", n, a.find.doc, right.sel)
	}
	// In the other column, find shows that document's matches, and leaves
	// its selection until asked for the next.
	left := a.cols[0].tab().ed
	hs.place(left, 20, 20)
	a.run("view.prevColumn")
	hs.settle()
	if a.find.doc != left.id || len(a.find.matches) != 1 || left.sel != [2]int{20, 20} {
		t.Fatalf("matches of document %d, selecting %v", a.find.doc, left.sel)
	}
	a.run("edit.findNext")
	hs.settle()
	expectSel(t, left, 0, 7)
	// Closing it gives the keyboard back to that column.
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	tt.Type("X")
	hs.settle()
	if got := left.buf.String(); !strings.HasPrefix(got, "X main") {
		t.Fatalf("typed into %q", got[:12])
	}
}

func TestDraggingTerminals(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	a.run("view.terminal")
	a.run("view.newTerminal")
	hs.settle()
	first, second := a.terms[0], a.terms[1]
	// A terminal of the panel, dropped on the side of the editor, shows in
	// a column of its own, and the panel shows the other.
	chip, ok := tt.Find("2: " + second.title)
	body, ok2 := tt.Find("Editor main.go")
	if !ok || !ok2 {
		t.Fatalf("no chip or no editor: %q", tt.Texts())
	}
	hs.drag(chip.X+chip.W/2, chip.Y+chip.H/2, body.X+body.W-20, body.Y+body.H/2)
	if len(a.cols) != 2 || a.cols[1].tab().term != second || a.panelTerm != first || !a.window.PanelOpen {
		t.Fatalf("%d columns, the panel open %v", len(a.cols), a.window.PanelOpen)
	}
	hs.shot("14-terminal-dragged")
	// And its tab, dropped on the panel, goes back.
	x, y := hs.tabCenter(second.title)
	panel, ok := tt.Find("TERMINAL")
	if !ok {
		t.Fatal("no panel")
	}
	hs.drag(x, y, panel.X+300, panel.Y+80)
	if len(a.cols) != 1 || second.tab != nil || a.panelTerm != second || len(a.panelTerms()) != 2 {
		t.Fatalf("%d columns, %d terminals in the panel", len(a.cols), len(a.panelTerms()))
	}
	// A document's tab is not the panel's to take.
	x, y = hs.tabCenter("main.go")
	hs.drag(x, y, panel.X+300, panel.Y+80)
	if len(a.cols) != 1 || len(a.cols[0].tabs) != 1 || len(a.docs) != 1 {
		t.Fatalf("a document dropped on the panel: %d tabs", len(a.cols[0].tabs))
	}
	a.closeTerminals()
}

// Closing the last tab of a column that another follows takes the column
// away, without the frame drawing the columns tripping on it: by its
// cross, and by Close in its menu.
func TestCloseLastTabOfColumn(t *testing.T) {
	for _, menu := range []bool{false, true} {
		hs := newHarness(t, true)
		a, tt := hs.a, hs.tt
		a.openFile(hs.path("main.go"), true)
		hs.settle()
		a.splitRight()
		hs.settle()
		a.openFile(hs.path("go.mod"), true)
		hs.settle()
		a.splitRight()
		hs.settle()
		a.openFile(hs.path("README.md"), true)
		hs.settle()
		if len(a.cols) != 3 {
			t.Fatalf("%d columns", len(a.cols))
		}
		// main.go shows in the first column only once its copies are gone.
		left, mid := a.cols[0], a.cols[1]
		for len(left.tabs) > 1 {
			a.closeTab(left.tabs[1], nil)
		}
		for len(mid.tabs) > 1 {
			a.closeTab(mid.tabs[0], nil)
		}
		hs.settle()
		name := mid.tab().name()
		if menu {
			x, y := hs.tabCenter(name)
			tt.RightClickAt(x, y)
			hs.settle()
			if err := tt.ChooseMenuItem("Close"); err != nil {
				t.Fatalf("%v: %q", err, tt.Menu())
			}
		} else if err := tt.Click("Close " + name); err != nil {
			t.Fatal(err)
		}
		hs.settle()
		hs.settle()
		if len(a.cols) != 2 || slices.Contains(a.cols, mid) || a.cols[0] != left {
			t.Errorf("menu %v: %d columns after closing %s", menu, len(a.cols), name)
		}
		if a.col < 0 || a.col >= len(a.cols) {
			t.Errorf("menu %v: column %d of %d has the keyboard", menu, a.col, len(a.cols))
		}
	}
}

// Splitting a diff to the right, twice, gives each column a scroll state
// of its own, not the one handle bound in both, which MyGo panics on (#20).
func TestSplitDiffRight(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\n// changed\nfunc main() {}\n"), 0o644)
	hs.settleGit()
	a.openFile(hs.path("main.go"), true)
	a.openChanges()
	hs.settleGit()
	if tab := a.activeTab(); tab == nil || tab.diff == nil {
		t.Fatalf("tab %+v", tab)
	}
	a.splitRight()
	hs.settle()
	a.splitRight()
	hs.settle()
	if len(a.cols) != 3 {
		t.Fatalf("%d columns", len(a.cols))
	}
	first, last := a.cols[0].tab().diff, a.column().tab().diff
	if first == nil || last == nil || first == last || first.scroll.Handle == last.scroll.Handle {
		t.Errorf("diffs share their scroll: %p %p", first, last)
	}
}
