package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// explorerSelected are the paths of the rows chosen together, in the
// tree's order, relative to the workspace.
func explorerSelected(hs *harness) []string {
	var got []string
	for i := range hs.a.files.Rows() {
		if p := hs.a.files.Item(i); hs.a.explorerSel.Has(p) {
			got = append(got, hs.a.tree.Rel(p))
		}
	}
	return got
}

func activePath(a *app) string {
	if t := a.activeTab(); t != nil {
		return t.path()
	}
	return ""
}

func mustClick(t *testing.T, hs *harness, mods ui.Modifiers, name string) {
	t.Helper()
	if err := hs.tt.ClickWith(mods, name); err != nil {
		t.Fatal(err)
	}
	hs.settle()
}

// Shift chooses the rows from the anchor in the tree's order, Cmd adds
// one or takes it out, and neither opens a file or a folder; a plain
// click or Escape chooses one row again.
func TestExplorerRangeAndToggle(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	mustClick(t, hs, 0, "go.mod")
	if activePath(a) != hs.path("go.mod") {
		t.Fatalf("a plain click showed %q", activePath(a))
	}
	mustClick(t, hs, ui.Shift, "README.md")
	if got, want := explorerSelected(hs), []string{"go.mod", "main.go", "README.md"}; !slices.Equal(got, want) {
		t.Fatalf("Shift-click chose %q, want %q", got, want)
	}
	if activePath(a) != hs.path("go.mod") || a.explorerChoice() != hs.path("README.md") {
		t.Fatalf("Shift-click showed %q, chose %q", activePath(a), a.explorerChoice())
	}
	mustClick(t, hs, ui.Cmd, "main.go")
	mustClick(t, hs, ui.Cmd, "web")
	if got, want := explorerSelected(hs), []string{"web", "go.mod", "README.md"}; !slices.Equal(got, want) {
		t.Fatalf("Cmd-click chose %q, want %q", got, want)
	}
	if a.files.Open.Has(hs.path("web")) || activePath(a) != hs.path("go.mod") {
		t.Fatal("Cmd-click opened the folder or a file")
	}
	// Shift goes from the row Cmd chose last.
	mustClick(t, hs, ui.Shift, "internal")
	if got, want := explorerSelected(hs), []string{"internal", "node_modules", "web"}; !slices.Equal(got, want) {
		t.Fatalf("Shift-click after Cmd chose %q, want %q", got, want)
	}
	// Shift with the keys goes on from the same anchor.
	hs.key(ui.Shift, ui.KeyDown)
	if got, want := explorerSelected(hs), []string{"node_modules", "web"}; !slices.Equal(got, want) {
		t.Fatalf("Shift+Down chose %q, want %q", got, want)
	}
	hs.key(0, ui.KeyEscape)
	if a.explorerSel.Len() != 0 || a.fileRow != -1 {
		t.Fatalf("Escape left %q chosen", explorerSelected(hs))
	}
	// A plain click chooses one again, and shows its file.
	mustClick(t, hs, ui.Cmd, "internal")
	mustClick(t, hs, ui.Cmd, "main.go")
	if a.explorerSel.Len() != 2 {
		t.Fatalf("Cmd-click chose %q", explorerSelected(hs))
	}
	mustClick(t, hs, 0, "README.md")
	if a.explorerSel.Len() != 0 || activePath(a) != hs.path("README.md") {
		t.Fatalf("a plain click left %q chosen, showed %q", explorerSelected(hs), activePath(a))
	}
	// So does a key that moves the choice.
	mustClick(t, hs, ui.Shift, "main.go")
	hs.key(0, ui.KeyUp)
	if a.explorerSel.Len() != 0 {
		t.Fatalf("Up left %q chosen", explorerSelected(hs))
	}
}

// Shift chooses rows of the expanded tree scrolled out of view.
func TestExplorerRangeOffscreen(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	for i := range 80 {
		os.MkdirAll(hs.path("many"), 0o755)
		os.WriteFile(hs.path(fmt.Sprintf("many/f%02d.txt", i)), nil, 0o644)
	}
	a.tree.Refresh()
	hs.settle()
	mustClick(t, hs, 0, "many")
	hs.until("the folder's files", func() bool { return hs.tt.HasText("f00.txt") })
	mustClick(t, hs, ui.Cmd, "f00.txt")
	last := a.explorerRowOf(hs.path("many/f79.txt"))
	a.files.List.ScrollIntoView(last)
	hs.settle()
	if hs.tt.HasText("f00.txt") {
		t.Fatal("the first file still shows")
	}
	mustClick(t, hs, ui.Shift, "f79.txt")
	if n := a.explorerSel.Len(); n != 80 {
		t.Fatalf("Shift-click chose %d rows, want 80", n)
	}
	if activePath(a) != "" {
		t.Fatalf("Shift-click showed %q", activePath(a))
	}
}

// Rows that close or go stop being chosen, a rename takes its row along,
// and a compact row split by a new folder keeps its choice.
func TestExplorerSelectionFollowsTheTree(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	mustClick(t, hs, 0, "web")
	hs.until("web's files", func() bool { return hs.tt.HasText("style.css") })
	mustClick(t, hs, 0, "index.html")
	mustClick(t, hs, ui.Cmd, "style.css")
	mustClick(t, hs, ui.Cmd, "go.mod")
	mustClick(t, hs, ui.Cmd, "main.go")
	a.files.Open.Remove(hs.path("web"))
	hs.settle()
	if got, want := explorerSelected(hs), []string{"go.mod", "main.go"}; !slices.Equal(got, want) {
		t.Fatalf("after closing web %q, want %q", got, want)
	}
	// Renamed, it stays chosen.
	if err := os.Rename(hs.path("go.mod"), hs.path("go.sum")); err != nil {
		t.Fatal(err)
	}
	a.renamed(hs.path("go.mod"), hs.path("go.sum"))
	hs.until("the renamed file", func() bool { return hs.tt.HasText("go.sum") })
	if got, want := explorerSelected(hs), []string{"go.sum", "main.go"}; !slices.Equal(got, want) {
		t.Fatalf("after the rename %q, want %q", got, want)
	}
	// Deleted on disk, it goes; one left is chosen alone.
	os.Remove(hs.path("go.sum"))
	a.tree.Refresh()
	hs.until("the file gone", func() bool { return !hs.tt.HasText("go.sum") })
	if a.explorerSel.Len() != 0 || a.explorerChoice() != hs.path("main.go") {
		t.Fatalf("after the delete %q chosen with %q", explorerSelected(hs), a.explorerChoice())
	}
	// Opening another folder lets the choice go.
	mustClick(t, hs, ui.Cmd, "README.md")
	a.openFolder(a.root)
	hs.settle()
	if a.explorerSel.Len() != 0 {
		t.Fatalf("another folder kept %q", explorerSelected(hs))
	}
}

func TestCompactExplorerSelection(t *testing.T) {
	hs := compactExplorerHarness(t)
	a := hs.a
	mustClick(t, hs, 0, "mixed")
	mustClick(t, hs, ui.Cmd, packageChain)
	if !a.explorerSel.Has(hs.path(packageChain)) || !a.explorerSel.Has(hs.path("mixed")) {
		t.Fatalf("chose %q", explorerSelected(hs))
	}
	if a.files.Open.Has(hs.path(packageChain)) {
		t.Fatal("Cmd-click opened the package")
	}
	// A new file splits the chain: its row, now src/main/java, stays chosen.
	if err := os.WriteFile(hs.path("src/main/java/README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	hs.until("the split package", func() bool { return hs.tt.HasText("src/main/java") && !hs.tt.HasText(packageChain) })
	if got, want := explorerSelected(hs), []string{"mixed", "src/main/java"}; !slices.Equal(got, want) {
		t.Fatalf("after the split %q, want %q", got, want)
	}
	if a.explorerChoice() != hs.path("src/main/java") {
		t.Fatalf("chose %q", a.explorerChoice())
	}
}

// ⌘C and the menu of a row chosen with others copy them all, the menu's
// Rename and Delete are off, and a row not chosen has its own menu.
func TestExplorerCopiesSelection(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	// Cmd alone chooses, so no tab shows a name the clicks could find.
	mustClick(t, hs, ui.Cmd, "README.md")
	mustClick(t, hs, ui.Cmd, "go.mod")
	if activePath(a) != "" {
		t.Fatalf("Cmd-click showed %q", activePath(a))
	}
	hs.command("copy")
	if want := []string{hs.path("go.mod"), hs.path("README.md")}; !slices.Equal(hs.h.clipboard, want) {
		t.Fatalf("⌘C copied %q, want %q", hs.h.clipboard, want)
	}
	hs.h.clipboard = nil
	if err := tt.RightClick("README.md"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := tt.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if want := []string{hs.path("go.mod"), hs.path("README.md")}; !slices.Equal(hs.h.clipboard, want) {
		t.Fatalf("Copy copied %q, want %q", hs.h.clipboard, want)
	}
	tt.RightClick("go.mod")
	hs.settle()
	if err := tt.ChooseMenuItem("Copy Relative Path"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := a.services.ReadClipboard(); got != "go.mod\nREADME.md" {
		t.Fatalf("Copy Relative Path copied %q", got)
	}
	for _, item := range []string{"Rename…", a.trashLabel()} {
		tt.RightClick("go.mod")
		hs.settle()
		if err := tt.ChooseMenuItem(item); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("%s with several chosen: %v", item, err)
		}
	}
	a.runBinding("renameFile", nil, placeExplorer)
	if a.prompt != nil {
		t.Fatal("F2 renamed one of several")
	}
	// A row not chosen: its menu is its own, and the others go.
	hs.h.clipboard = nil
	tt.RightClick("main.go")
	hs.settle()
	if err := tt.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if want := []string{hs.path("main.go")}; !slices.Equal(hs.h.clipboard, want) || a.explorerSel.Len() != 0 {
		t.Fatalf("Copy copied %q, %q still chosen", hs.h.clipboard, explorerSelected(hs))
	}
}

// A row dragged moves alone, chosen with others or not, which then stop
// being chosen; the rows chosen show it.
func TestExplorerDragWithSelection(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	mustClick(t, hs, ui.Cmd, "main.go")
	mustClick(t, hs, ui.Cmd, "README.md")
	if a.explorerSel.Len() != 2 {
		t.Fatalf("chose %q", explorerSelected(hs))
	}
	if !hs.tt.HasText("main.go") {
		t.Fatal("no row")
	}
	dragRow(t, hs, "main.go", "web")
	if !onDisk(hs.path("web/main.go")) || !onDisk(hs.path("README.md")) {
		t.Fatal("the drag did not move main.go alone")
	}
	if a.explorerSel.Len() != 0 {
		t.Fatalf("still chosen %q", explorerSelected(hs))
	}
	if a.fails != 0 {
		t.Fatalf("%d failures", a.fails)
	}
}
