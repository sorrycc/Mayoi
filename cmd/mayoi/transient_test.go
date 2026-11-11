package main

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// tabPaths are the files the tabs of the column that has the keyboard
// show, a preview's marked with a star.
func tabPaths(hs *harness) []string {
	var out []string
	for _, t := range hs.a.column().tabs {
		name := t.name()
		if t.transient {
			name += "*"
		}
		out = append(out, name)
	}
	return out
}

func wantTabs(t *testing.T, hs *harness, want ...string) {
	t.Helper()
	if got := tabPaths(hs); !slices.Equal(got, want) {
		t.Fatalf("tabs %q; want %q", got, want)
	}
}

// Walking search's results shows each match in one preview tab, which a
// file open already, Enter and an edit leave alone.
func TestSearchPreviewTab(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	for i := range 3 {
		os.WriteFile(hs.path(fmt.Sprintf("n%d.txt", i)), []byte("line 0 needle\nline 1 needle\n"), 0o644)
	}
	a.indexRead.Invalidate()
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	hs.searchFor("needle")
	s := &a.inFiles

	// Down from the query shows the first match, the keyboard staying.
	tt.Key(0, ui.KeyDown)
	hs.settle()
	wantTabs(t, hs, "README.md", "n0.txt*")
	d := a.activeEditor()
	if got := d.buf.Slice(d.sel[0], d.sel[1]); d.path != hs.path("n0.txt") || got != "needle" {
		t.Fatalf("selected %q in %s", got, d.path)
	}
	// The next match of the file, then the next file in its place.
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if d := a.activeEditor(); d.path != hs.path("n0.txt") || d.sel[0] < 14 {
		t.Fatalf("second match: %s at %v", d.path, d.sel)
	}
	tt.Key(0, ui.KeyDown)
	hs.settle()
	wantTabs(t, hs, "README.md", "n1.txt*")
	if a.docIndex(hs.path("n0.txt")) >= 0 {
		t.Error("the document of the preview replaced stayed open")
	}
	if slices.Contains(a.recent, hs.path("n1.txt")) {
		t.Error("a preview went among the recent files")
	}

	// A click chooses its row, and the arrows go on from there.
	if err := tt.Click("line 1 needle"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	rows := a.searchRows()
	if r := rows[s.row]; s.row != 2 || r.match != 1 || s.files[r.file].rel != "n0.txt" {
		t.Fatalf("click chose row %d", s.row)
	}
	wantTabs(t, hs, "README.md", "n0.txt*")
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if s.row != 4 {
		t.Fatalf("Down after the click: row %d; want 4", s.row)
	}
	wantTabs(t, hs, "README.md", "n1.txt*")

	// Enter keeps it, with the keyboard; the next match opens another.
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	wantTabs(t, hs, "README.md", "n1.txt")
	if !slices.Contains(a.recent, hs.path("n1.txt")) {
		t.Error("the tab kept is not among the recent files")
	}
	s.row = 7
	a.previewRow()
	hs.settle()
	wantTabs(t, hs, "README.md", "n1.txt", "n2.txt*")

	// A file kept shows in its own tab, the preview staying.
	s.row = 5
	a.previewRow()
	hs.settle()
	wantTabs(t, hs, "README.md", "n1.txt", "n2.txt*")
	if d := a.activeEditor(); d.path != hs.path("n1.txt") {
		t.Fatalf("showing %s", d.path)
	}

	// An edit keeps the preview.
	s.row = 8
	a.previewRow()
	hs.settle()
	d = a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, "x"})
	hs.settle()
	wantTabs(t, hs, "README.md", "n1.txt", "n2.txt")
}

// A click in the Explorer previews the file, Enter keeps it, and
// a preview comes back as one at the next start.
func TestExplorerPreviewTab(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	tt.Click("main.go")
	hs.settle()
	wantTabs(t, hs, "main.go*")
	tt.Click("go.mod")
	hs.settle()
	wantTabs(t, hs, "go.mod*")
	// Enter, as a double click does.
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	wantTabs(t, hs, "go.mod")
	tt.Click("main.go")
	hs.settle()
	wantTabs(t, hs, "go.mod", "main.go*")
	if got := a.window.Columns[0].Tabs; len(got) != 2 || got[0].Transient || !got[1].Transient {
		t.Fatalf("tabs kept %+v", got)
	}

	// The next start.
	b := newApp(hs.h, a.settings, a.window)
	t.Cleanup(b.shutdown)
	tb := ui.NewTester(b.view, 1200, 760)
	for range 5 {
		step(tb)
	}
	var got []string
	for _, t := range b.column().tabs {
		got = append(got, fmt.Sprintf("%s %v", t.name(), t.transient))
	}
	if !slices.Equal(got, []string{"go.mod false", "main.go true"}) {
		t.Errorf("restored %q", got)
	}

	// A tab dragged to another column is kept.
	col := a.column()
	a.moveTab(col.tabs[1], a.splitColumn(col, true), 0)
	if a.cols[1].tabs[0].transient {
		t.Error("the preview dragged to another column is still one")
	}
}

// The whole Explorer row previews its file, including the space beside its name.
func TestExplorerWholeRowPreviewsFile(t *testing.T) {
	for _, side := range []string{"leading", "trailing"} {
		t.Run(side, func(t *testing.T) {
			hs := newHarness(t, true)
			a, tt := hs.a, hs.tt
			a.openFile(hs.path("README.md"), true)
			a.openFile(hs.path("main.go"), true)
			hs.settle()
			for _, name := range []string{"README.md", "main.go"} {
				// Find the Explorer label before the tab with the same name.
				r, ok := tt.Find(name)
				if !ok {
					t.Fatalf("no row %s", name)
				}
				x := r.X - 24
				if side == "trailing" {
					x = a.leftPanel().Size - 12
				}
				tt.ClickAt(x, r.Y+r.H/2)
				hs.settle()
				if d := a.activeEditor(); d == nil || d.path != hs.path(name) {
					t.Fatalf("clicking %s space of %s did not switch editors", side, name)
				}
				wantTabs(t, hs, "README.md", "main.go")
			}
		})
	}
}

// Walking the Explorer with the arrows shows each file in one preview
// tab, the keyboard staying in the Explorer; a folder leaves it shown.
func TestExplorerArrowsPreviewFiles(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	tt.Click("README.md")
	hs.settle()
	wantTabs(t, hs, "README.md*")
	tt.Key(0, ui.KeyUp)
	hs.settle()
	wantTabs(t, hs, "main.go*")
	tt.Key(0, ui.KeyUp)
	hs.settle()
	wantTabs(t, hs, "go.mod*")
	tt.Key(0, ui.KeyUp)
	hs.settle()
	wantTabs(t, hs, "go.mod*")
	if got := hs.a.explorerChoice(); got != hs.path("web") {
		t.Fatalf("chose %s; want the folder web", got)
	}
}

// A double click in the Explorer keeps the file open, with the keyboard.
func TestExplorerDoubleClick(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	// The rows of the tree, before tabs show the names too.
	row := func(name string) func() {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("no row %s", name)
		}
		return func() { tt.ClickAt(r.X+r.W/2, r.Y+r.H/2) }
	}
	readme, main, mod := row("README.md"), row("main.go"), row("go.mod")
	readme()
	hs.settle()
	wantTabs(t, hs, "README.md*")
	// The second click of the double click.
	readme()
	hs.settle()
	wantTabs(t, hs, "README.md")
	if !tt.Focused("Editor README.md") {
		t.Error("the file double-clicked does not have the keyboard")
	}
	// From scratch, too, and the next file clicked opens beside it.
	main()
	main()
	hs.settle()
	wantTabs(t, hs, "README.md", "main.go")
	mod()
	hs.settle()
	wantTabs(t, hs, "README.md", "main.go", "go.mod*")
}

// A click on a commit previews its changes, in the tab a file's preview
// has too; Enter keeps it, and a preview comes back as one.
func TestCommitPreviewTab(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a, tt := hs.a, hs.tt
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	git("commit", "-qam", "Trim main")
	tt.Click("go.mod")
	hs.settle()
	wantTabs(t, hs, "go.mod*")
	a.run("view.scm")
	hs.settleGit()
	if err := tt.Click("Trim main"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	wantTabs(t, hs, "Trim main*")
	if err := tt.Click("first"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	wantTabs(t, hs, "first*")

	// Enter, as a double click does, keeps it: the keyboard stayed in
	// the list.
	tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	wantTabs(t, hs, "first")
	if err := tt.Click("Trim main"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	wantTabs(t, hs, "first", "Trim main*")
	if got := a.window.Columns[0].Tabs; len(got) != 2 || got[0].Transient || !got[1].Transient {
		t.Fatalf("tabs kept %+v", got)
	}

	// The next start.
	b := newApp(hs.h, a.settings, a.window)
	t.Cleanup(b.shutdown)
	tb := ui.NewTester(b.view, 1200, 760)
	for range 5 {
		step(tb)
	}
	if tabs := b.column().tabs; len(tabs) != 2 || tabs[0].transient || tabs[1].diff == nil || !tabs[1].transient {
		t.Errorf("restored %+v", tabs)
	}
}
