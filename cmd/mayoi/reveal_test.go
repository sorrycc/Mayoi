package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// A tab's Reveal in Explorer shows the explorer in place of source
// control, opens the folders above the file, chooses it and scrolls to it.
func TestTabRevealsItsFileInTheExplorer(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	dir := hs.path("many")
	os.MkdirAll(dir, 0o755)
	for i := range 80 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), nil, 0o644)
	}
	a.tree.Refresh()
	path := filepath.Join(dir, "f79.txt")
	a.openFile(path, true)
	hs.settle()

	a.showSourceControl()
	a.files.Open.Clear()
	a.fileRow = -1
	a.files.List.ScrollTo(0, ui.Start)
	hs.settle()
	rightClickTab(t, tt, "f79.txt")
	if err := tt.ChooseMenuItem("Reveal in Explorer"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	hs.settle()
	if a.leftPanel().Shown() != settings.ViewExplorer || !a.leftPanel().Open {
		t.Fatalf("the left panel shows %q, open %v", a.leftPanel().Shown(), a.leftPanel().Open)
	}
	if !a.files.Open.Has(dir) {
		t.Error("the folder above the file did not open")
	}
	if a.fileRow < 0 || a.fileRow >= a.files.Rows() || a.files.Item(a.fileRow) != path {
		t.Fatalf("tree chose row %d, not %s", a.fileRow, path)
	}
	if first, last := a.files.List.Visible(); a.fileRow < first || a.fileRow > last {
		t.Errorf("the tree shows rows %d to %d, not row %d", first, last, a.fileRow)
	}

	rightClickTab(t, tt, "f79.txt")
	if err := tt.ChooseMenuItem("Reveal in Finder"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	if !slices.Equal(hs.h.revealed, []string{path}) {
		t.Errorf("revealed in the Finder %q", hs.h.revealed)
	}
}

// rightClickTab opens the context menu of the tab named name: the title
// bar shows the name first, so it clicks beside the tab's close button.
func rightClickTab(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	r, ok := tt.Find("Close " + name)
	if !ok {
		t.Fatalf("no tab %q", name)
	}
	tt.RightClickAt(r.X-12, r.Y+r.H/2)
}

// Rename Active File renames the tab's file, whatever the explorer has
// chosen, and a file from outside the folder leaves none chosen.
func TestRenameActiveFileIsTheTabs(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	if a.explorerChoice() != hs.path("main.go") {
		t.Fatalf("chosen %q", a.explorerChoice())
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("x"), 0o644)
	a.openFile(outside, true)
	hs.settle()
	if got := a.explorerChoice(); got != "" {
		t.Errorf("chosen %q with a file from outside open", got)
	}

	for i := range a.files.Rows() {
		if a.files.Item(i) == hs.path("go.mod") {
			a.fileRow = i
		}
	}
	a.run("file.rename")
	hs.settle()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("renamed.txt")
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	to := filepath.Join(filepath.Dir(outside), "renamed.txt")
	if _, err := os.Stat(to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hs.path("go.mod")); err != nil {
		t.Errorf("the explorer's choice was renamed: %v", err)
	}
	if d := a.activeEditor(); d.path != to {
		t.Errorf("the tab shows %s", d.path)
	}
}
