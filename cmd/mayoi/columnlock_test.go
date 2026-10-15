package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
	"mayoi/internal/settings"
)

// lockedHarness leaves a file workspace beside an empty locked column.
func lockedHarness(t *testing.T) (*harness, *column, *column) {
	t.Helper()
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	left := a.column()
	right := a.splitColumn(left, true)
	a.toggleColumnLock(right)
	return hs, left, right
}

func TestLockedColumnRoutesFiles(t *testing.T) {
	hs, left, right := lockedHarness(t)
	a := hs.a
	image := hs.path("image.svg")
	if err := os.WriteFile(image, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		open func()
	}{
		{"file", func() { a.openFile(hs.path("README.md"), true) }},
		{"preview", func() { a.previewFile(hs.path("go.mod")) }},
		{"image", func() { a.openFile(image, true) }},
		{"new file", a.newFile},
		{"file at line", func() { a.openFileAt(hs.path("main.go"), 3, true) }},
		{"changes", func() { a.openChangesTab(true) }},
		{"commit", func() { a.showCommit(git.Commit{Hash: "abc", Subject: "A commit"}, true) }},
		{"commit preview", func() { a.previewCommit(git.Commit{Hash: "def", Subject: "Another commit"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.col = slices.Index(a.cols, right)
			tc.open()
			if a.column() != left || left.tab() == nil || len(right.tabs) != 0 {
				t.Fatalf("opened in column %d, %d tabs in locked column", a.col, len(right.tabs))
			}
		})
	}
	// Explicit drops can still target a locked column. Implicit opens of
	// that same file subsequently use the workspace, leaving it there too.
	a.dropFiles([]string{hs.path("README.md")}, right, 0)
	if right.tab() == nil || right.tab().path() != hs.path("README.md") {
		t.Fatal("an explicit drop did not use the locked column")
	}
	a.openFile(hs.path("README.md"), true)
	if a.column() != left || len(right.tabs) != 1 {
		t.Fatal("opening an existing file returned to the locked column")
	}
	// Even an existing diff explicitly moved into the locked column must
	// not capture later opens from Source Control.
	a.col = 0
	v := a.openChangesTab(true)
	a.moveTab(left.tab(), right, len(right.tabs))
	if next := a.openChangesTab(true); next == v || a.column() != left {
		t.Fatal("an existing locked diff captured a Source Control open")
	}
	a.showCommit(git.Commit{Hash: "abc", Subject: "A commit"}, true)
	a.moveTab(left.tab(), right, len(right.tabs))
	a.previewCommit(git.Commit{Hash: "abc", Subject: "A commit"})
	if a.column() != left {
		t.Fatal("an existing locked commit captured a history open")
	}
}

func TestLockedColumnKeepsWorkspaceAndWidth(t *testing.T) {
	hs, left, right := lockedHarness(t)
	a := hs.a
	a.openFileIn(right, 0, hs.path("README.md"), true)
	left.weight, right.weight = 3, 2
	hs.settle()
	before, ok := hs.tt.Find("Editor README.md")
	if !ok {
		t.Fatal("no right editor")
	}
	a.closeTab(left.tab(), nil)
	hs.settle()
	after, ok := hs.tt.Find("Editor README.md")
	if !ok || len(a.cols) != 2 || left.tab() != nil || math.Abs(float64(before.W-after.W)) > 1 || before.X != after.X {
		t.Fatalf("closing the workspace changed layout: %v -> %v, %d columns", before, after, len(a.cols))
	}
	// Another close in the empty workspace must not close the window.
	a.col = 0
	a.closeActive()
	if hs.h.closed {
		t.Fatal("closing an empty file workspace closed the window")
	}
	a.openFile(hs.path("go.mod"), true)
	if a.column() != left || a.cols[1] != right {
		t.Fatal("the next file did not reopen on the left")
	}
	// With multiple file columns, an empty one can go. Its width goes to
	// another file column, preserving the locked column's fraction.
	mid := a.splitColumn(left, true)
	a.openFileIn(mid, 0, hs.path("main.go"), true)
	a.closeTab(mid.tab(), nil)
	if len(a.cols) != 2 || left.weight != 3 || right.weight != 2 {
		t.Fatalf("after closing another column: %d columns, weights %v and %v", len(a.cols), left.weight, right.weight)
	}
	// Moving the last file out of the workspace must preserve it too.
	a.moveTab(left.tab(), right, len(right.tabs))
	if len(a.cols) != 2 || left.tab() != nil {
		t.Fatal("moving the last file collapsed the workspace")
	}
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	r, ok := hs.tt.Find("Editor go.mod")
	if !ok {
		t.Fatal("no right editor to resize")
	}
	hs.drag(r.X+1, 300, r.X+81, 300)
	r2, _ := hs.tt.Find("Editor go.mod")
	if d := r.W - r2.W; d < 70 || d > 90 {
		t.Fatalf("a locked column cannot be resized: %v -> %v", r, r2)
	}
}

func TestLockedColumnMenuAndEmptyView(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	x, y := hs.tabCenter("main.go")
	tt.RightClickAt(x, y)
	hs.settle()
	if err := tt.ChooseMenuItem("Lock Column"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.cols) != 2 || !a.cols[1].locked || a.cols[0].locked {
		t.Fatal("locking the only column did not create a left file workspace")
	}
	right := a.cols[1]
	a.closeTab(right.tab(), nil)
	hs.settle()
	if !tt.HasText("Locked Column") {
		t.Fatal("the empty locked column disappeared")
	}
	if _, ok := tt.Find("New Terminal"); !ok {
		t.Fatal("the empty locked column offers no new terminal")
	}
	hs.shot("column-lock-empty")
	if err := tt.Click("Unlock Column"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.cols) != 1 || a.cols[0] == right {
		t.Fatal("unlocking the empty column did not collapse it")
	}
}

func TestLockedColumnRemembersLastFileColumn(t *testing.T) {
	hs, left, right := lockedHarness(t)
	a := hs.a
	middle := a.splitColumn(left, true)
	a.openFileIn(middle, 0, hs.path("go.mod"), true)
	for _, target := range []*column{left, middle, left} {
		a.activate(target, 0, true)
		a.col = slices.Index(a.cols, right)
		a.previewFile(hs.path("README.md"))
		if a.column() != target {
			t.Fatal("opening from a locked column forgot the last file column")
		}
	}
	// Locking every existing column leaves a new unlocked column on the left.
	a.toggleColumnLock(left)
	a.toggleColumnLock(middle)
	if len(a.cols) != 4 || a.cols[0].locked || a.fileColumn() != a.cols[0] {
		t.Fatal("locking the last file column left nowhere to open files")
	}
}

func TestLockedColumnSessionRoundTrip(t *testing.T) {
	hs, left, right := lockedHarness(t)
	a := hs.a
	middle := a.splitColumn(left, true)
	a.openFileIn(middle, 0, hs.path("go.mod"), true)
	a.openFileIn(right, 0, hs.path("README.md"), true) // explicit placement
	left.weight, middle.weight, right.weight = 1, 2, 3
	a.rememberTabs()
	// Exercise the persisted representation, including last file-column index.
	data, err := json.Marshal(a.window.Session)
	if err != nil {
		t.Fatal(err)
	}
	w := a.settings.NewWindow()
	w.Folder = hs.root
	if err := json.Unmarshal(data, &w.Session); err != nil {
		t.Fatal(err)
	}
	b := newApp(hs.h, a.settings, w)
	tb := ui.NewTester(b.view, 1200, 760)
	tb.Frame()
	tb.Frame()
	if len(b.cols) != 3 || !b.cols[2].locked || b.cols[2].tab().path() != hs.path("README.md") || b.cols[2].weight != 3 {
		t.Fatalf("restored locked layout: %+v", w.Columns)
	}
	if b.column() != b.cols[2] || b.fileColumn() != b.cols[1] || b.cols[0].weight != 1 || b.cols[1].weight != 2 {
		t.Fatal("restoration lost the focus, file target or width")
	}
	b.openFile(hs.path("main.go"), true)
	if b.column() != b.cols[1] {
		t.Fatal("file after restart went to the locked column")
	}
}

func TestLockedColumnRestoresEmptyWorkspace(t *testing.T) {
	for _, lockedTabs := range [][]settings.Tab{nil, {{File: "README.md"}}} {
		hs, left, _ := lockedHarness(t)
		a := hs.a
		a.closeTab(left.tab(), nil)
		w := a.settings.NewWindow()
		w.Folder = hs.root
		w.Columns = []settings.Column{
			{Active: -1, Weight: 2},
			{Tabs: lockedTabs, Active: 0, Weight: 3, Locked: true},
		}
		if len(lockedTabs) > 0 {
			w.Columns[1].Tabs[0].File = filepath.Join(hs.root, lockedTabs[0].File)
		}
		w.Column = 1
		b := newApp(hs.h, a.settings, w)
		tb := ui.NewTester(b.view, 1200, 760)
		tb.Frame()
		tb.Frame()
		if len(b.cols) != 2 || b.cols[0].tab() != nil || b.cols[0].weight != 2 || !b.cols[1].locked || b.cols[1].weight != 3 {
			t.Fatalf("empty layout collapsed: %+v", w.Columns)
		}
	}
}

func TestLockedColumnRestoresFileTargetAfterCleanup(t *testing.T) {
	for _, savedTarget := range []int{0, 1, 2, -1, 99} {
		t.Run(fmt.Sprint(savedTarget), func(t *testing.T) {
			root := workspaceFixture(t)
			s := settings.Default()
			w := s.NewWindow()
			w.Folder = root
			w.Columns = []settings.Column{
				{Tabs: []settings.Tab{{File: filepath.Join(root, "main.go")}}, Active: 0, Weight: 1},
				{Tabs: []settings.Tab{{File: filepath.Join(root, "missing.go")}}, Active: 0, Weight: 2},
				{Active: -1, Weight: 3, Locked: true},
			}
			w.Column, w.FileColumn = 2, savedTarget
			a := newApp(&fakeHost{}, s, w)
			tt := ui.NewTester(a.view, 1200, 760)
			tt.Frame()
			tt.Frame()
			if len(a.cols) != 2 || a.column() != a.cols[1] || a.fileCol != a.cols[0] || a.fileCol.locked {
				t.Fatalf("saved target %d did not resolve to a surviving file column", savedTarget)
			}
			if w.FileColumn != 0 || w.Column != 1 || a.cols[0].weight != 3 || a.cols[1].weight != 3 {
				t.Fatalf("cleanup changed locked width or saved stale indices: %+v", w.Session)
			}
			a.openFile(filepath.Join(root, "README.md"), true)
			if a.column() != a.cols[0] || a.cols[1].tab() != nil {
				t.Fatal("the next file used a removed or locked column")
			}
		})
	}
}

func TestCloseEmptyColumn(t *testing.T) {
	for _, withLock := range []bool{false, true} {
		t.Run(fmt.Sprint(withLock), func(t *testing.T) {
			hs := newHarness(t, true)
			a := hs.a
			a.openFile(hs.path("main.go"), true)
			left := a.column()
			empty := a.splitColumn(left, true)
			var locked *column
			var lockedWeight float32
			if withLock {
				locked = a.splitColumn(empty, true)
				a.toggleColumnLock(locked)
				lockedWeight = locked.weight
			}
			a.col = slices.Index(a.cols, empty)
			a.run("file.close")
			if slices.Contains(a.cols, empty) || hs.h.closed || len(a.window.Columns) != len(a.cols) {
				t.Fatal("closing a redundant empty column kept it or closed the window")
			}
			if a.fileCol != nil && !slices.Contains(a.cols, a.fileCol) {
				t.Fatal("closing the empty column left a stale file target")
			}
			if withLock {
				a.col = slices.Index(a.cols, locked)
				a.run("file.close")
				if !slices.Contains(a.cols, locked) || locked.weight != lockedWeight {
					t.Fatal("closing the empty locked column removed or resized it")
				}
			}
			a.closeTab(left.tab(), nil)
			a.col = slices.Index(a.cols, left)
			a.run("file.close")
			if withLock {
				if hs.h.closed || len(a.cols) != 2 || a.cols[0] != left || locked.weight != lockedWeight {
					t.Fatal("closing the last empty workspace collapsed the locked layout")
				}
			} else if !hs.h.closed {
				t.Fatal("closing a lone empty ordinary column did not close the window")
			}
		})
	}
}

func TestLockedColumnProjectLayout(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	a, b := hs[0].a, hs[1].a
	a.openFile(filepath.Join(root, "main.go"), true)
	left := a.column()
	right := a.splitColumn(left, true)
	a.toggleColumnLock(right)
	left.weight, right.weight = 2, 3
	a.rememberTabs()
	f.active = hs[1]
	tt.Frame()
	tt.Frame()
	if len(b.cols) != 1 || b.cols[0].locked {
		t.Fatal("the second project inherited the column lock")
	}
	b.openFile(filepath.Join(other, "README.md"), true)
	f.active = hs[0]
	tt.Frame()
	tt.Frame()
	if len(a.cols) != 2 || !right.locked || left.weight != 2 || right.weight != 3 {
		t.Fatal("switching projects lost the locked layout")
	}
}

func TestTerminalTabLocksInPlace(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(a.closeTerminals)
	a.run("view.terminalTab")
	if len(a.terms) != 1 {
		t.Fatal("could not start terminal tab")
	}
	s := a.terms[0]
	original := s.tab
	a.run("view.terminalLockRight")
	if len(a.cols) != 2 || !a.cols[1].locked || a.cols[1].tab() != original || a.cols[0].tab() != nil {
		t.Fatal("locking the only terminal did not keep it beside an empty workspace")
	}
	a.run("view.lockColumn")
	if a.cols[1].locked || a.cols[1].tab() != original {
		t.Fatal("unlocking a populated column changed its terminal")
	}
}

func TestTerminalMovesToLockedRightColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		// Exercise the PTY without depending on a user's interactive setup.
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	a.openFile(hs.path("main.go"), true)
	a.newTerminal()
	hs.settle()
	s := a.panelTerm
	process := s.term
	if err := tt.RightClick("1: " + s.title); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move to Locked Right Column"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.cols) != 2 || !a.cols[1].locked || a.cols[1].tab().term != s || s.term != process || a.window.PanelOpen {
		t.Fatal("the panel terminal did not move intact to a locked right column")
	}
	right := a.cols[1]
	tt.Type("echo locked-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.term.Text(), "locked-42") && time.Now().Before(deadline) {
		hs.settle()
	}
	if !strings.Contains(s.term.Text(), "locked-42") {
		t.Fatalf("the moved terminal no longer accepts input: column %d, focus %d, text %q", a.col, a.focus, s.term.Text())
	}
	a.closeTab(a.cols[0].tab(), nil)
	hs.settle()
	if len(a.cols) != 2 || a.cols[0].tab() != nil {
		t.Fatal("closing the last file expanded the terminal")
	}
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	if a.col != 0 || len(right.tabs) != 1 {
		t.Fatal("opening a file after terminal focus added a terminal-column tab")
	}
	hs.shot("column-lock-terminal")
	// Further panel terminals reuse the same locked right column.
	a.newTerminal()
	a.moveTerminalToLockedRight()
	if len(a.cols) != 2 || len(right.tabs) != 2 {
		t.Fatal("a second terminal did not reuse the locked column")
	}
	a.removeTerminal(a.terms[1])
	// A shell exiting takes its tab but leaves the locked column and action.
	a.activate(right, 0, true)
	hs.settle()
	tt.Type("exit")
	tt.Key(0, ui.KeyEnter)
	deadline = time.Now().Add(5 * time.Second)
	for len(a.terms) != 0 && time.Now().Before(deadline) {
		hs.settle()
	}
	hs.settle()
	if len(a.terms) != 0 || len(a.cols) != 2 || !tt.HasText("Locked Column") {
		t.Fatal("shell exit removed the locked column")
	}
	if err := tt.Click("New Terminal"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.terms) != 1 || right.tab() == nil || right.tab().term != a.terms[0] {
		t.Fatal("the empty-column action did not start a terminal in place")
	}
}
