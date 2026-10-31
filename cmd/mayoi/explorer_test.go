package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

const packageChain = "src/main/java/com/example"

func compactExplorerHarness(t *testing.T) *harness {
	t.Helper()
	hs := newHarness(t, true)
	hs.root = filepath.Join(t.TempDir(), "java-project")
	for _, name := range []string{
		packageChain + "/App.java", "mixed/child/A.java", "mixed/README",
		"branch/a/more/deep/A.java", "branch/b/B.java", "代码/包/Main.java",
	} {
		path := hs.path(name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("class Example {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(hs.path("empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	hs.a.openFolder(hs.root)
	hs.a.run("view.compactFolders")
	hs.until("the compact package", func() bool { return hs.tt.HasText(packageChain) })
	return hs
}

func explorerDepth(a *app, path string) int {
	for i := range a.files.Rows() {
		if a.files.Item(i) == path {
			return a.files.Depth(i)
		}
	}
	return -1
}

func TestCompactExplorerFolders(t *testing.T) {
	hs := compactExplorerHarness(t)
	a, tt := hs.a, hs.tt
	leaf := hs.path(packageChain)
	for _, label := range []string{packageChain, "mixed", "branch", "empty", "代码/包"} {
		if !tt.HasText(label) {
			t.Errorf("missing row %q", label)
		}
	}
	if depth := explorerDepth(a, leaf); depth != 0 || tt.HasText("App.java") {
		t.Fatalf("collapsed package: depth %d, texts %q", depth, tt.Texts())
	}
	if err := tt.Click(packageChain); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.explorerChoice() != leaf || a.targetDir() != leaf || explorerDepth(a, hs.path(packageChain+"/App.java")) != 1 {
		t.Fatalf("choice %q, target %q; package child did not show at depth 1", a.explorerChoice(), a.targetDir())
	}
	tt.Command("copy")
	if !slices.Equal(hs.h.clipboard, []string{leaf}) {
		t.Fatalf("copied %q, want the last folder", hs.h.clipboard)
	}
	if err := tt.RightClick(packageChain); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename…"); err != nil {
		t.Fatal(err)
	}
	if a.prompt == nil || a.prompt.value != "example" {
		t.Fatalf("rename did not target the last folder: %+v", a.prompt)
	}
	a.prompt = nil
	hs.settle()
	if err := tt.RightClick(packageChain); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("New File…"); err != nil {
		t.Fatal(err)
	}
	if a.prompt == nil {
		t.Fatal("no new file prompt")
	}
	if err := a.prompt.done("Added.java"); err != "" {
		t.Fatal(err)
	}
	a.prompt = nil
	hs.until("the new file", func() bool { return a.explorerChoice() == hs.path(packageChain+"/Added.java") })
	if _, err := os.Stat(hs.path(packageChain + "/Added.java")); err != nil {
		t.Fatal(err)
	}
	hs.shot("compact-folders")
	// The header menu restores separate rows and keeps the file selected.
	if err := tt.RightClick(strings.ToUpper(filepath.Base(a.root))); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Compact Folders"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.ExplorerCompactFolders || explorerDepth(a, hs.path(packageChain+"/Added.java")) != 5 {
		t.Fatalf("uncompacted depth %d", explorerDepth(a, hs.path(packageChain+"/Added.java")))
	}
	if a.explorerChoice() != hs.path(packageChain+"/Added.java") {
		t.Fatalf("lost the selection: %q", a.explorerChoice())
	}
	// A settings edit applies just as the menu does, without reopening.
	p := a.settings.Prefs
	p.ExplorerCompactFolders = true
	a.settings.SetPrefs(p)
	hs.settle()
	if !tt.HasText(packageChain) || explorerDepth(a, hs.path(packageChain+"/Added.java")) != 1 {
		t.Fatal("a settings edit did not compact the tree")
	}
}

func TestCompactExplorerRefreshAndKeyboard(t *testing.T) {
	hs := compactExplorerHarness(t)
	a, tt := hs.a, hs.tt
	leaf := hs.path(packageChain)
	if err := tt.Click(packageChain); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	tt.Key(0, ui.KeyLeft)
	hs.settle()
	if a.files.Open.Has(leaf) || tt.HasText("App.java") {
		t.Fatal("Left did not collapse the package")
	}
	// A new file at an intermediate level splits a collapsed chain too.
	if err := os.WriteFile(hs.path("src/main/java/README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	hs.until("the split package", func() bool { return tt.HasText("src/main/java") && !tt.HasText(packageChain) })
	if a.explorerChoice() != hs.path("src/main/java") || tt.HasText("README") {
		t.Fatalf("split row lost its collapsed selection: %q", a.explorerChoice())
	}
	for _, dir := range []string{"src", "src/main", "src/main/java"} {
		if !a.explorerDirVisible(hs.path(dir)) {
			t.Errorf("remote refresh would miss %q", dir)
		}
	}
	if err := os.Remove(hs.path("src/main/java/README")); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	hs.until("the recombined package", func() bool { return tt.HasText(packageChain) })
	if a.explorerChoice() != leaf {
		t.Fatalf("recombined selection %q", a.explorerChoice())
	}
	tt.Key(0, ui.KeyRight)
	hs.settle()
	if !tt.HasText("App.java") {
		t.Fatal("Right did not expand the package")
	}
	tt.Key(0, ui.KeyRight)
	hs.settle()
	if a.explorerChoice() != hs.path(packageChain+"/App.java") {
		t.Fatalf("Right did not choose the child: %q", a.explorerChoice())
	}
	// Reveal traverses the actual folders after collapsing everything.
	a.files.Open.Clear()
	hs.settle()
	a.revealInTree(hs.path(packageChain + "/App.java"))
	hs.until("the revealed file", func() bool { return a.explorerChoice() == hs.path(packageChain+"/App.java") })
	a.rememberTabs()
	if !slices.Contains(a.window.Expanded, packageChain) || !slices.Contains(a.window.Expanded, "src/main") {
		t.Fatalf("session lost the intermediate folders: %q", a.window.Expanded)
	}
	open := slices.Clone(a.window.Expanded)
	a.openFolder(a.root)
	a.restoreExpanded(open)
	hs.until("the restored package", func() bool { return tt.HasText(packageChain) && tt.HasText("App.java") })
}

func TestCompactExplorerExpandAll(t *testing.T) {
	hs := compactExplorerHarness(t)
	a, tt := hs.a, hs.tt
	// Load the two branches, then close the parent. Recursive expansion
	// must still visit children that were not visible in the last frame.
	a.revealInTree(hs.path("branch/a/more/deep/A.java"))
	a.revealInTree(hs.path("branch/b/B.java"))
	hs.settle()
	if err := tt.Click("branch"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	mod := ui.Shift
	if runtime.GOOS == "darwin" {
		mod = ui.Alt
	}
	tt.Key(mod, ui.KeyLeft)
	hs.settle()
	tt.Key(mod, ui.KeyRight)
	hs.settle()
	if !a.files.Open.Has(hs.path("branch/a/more/deep")) || !a.files.Open.Has(hs.path("branch/b")) || !tt.HasText("A.java") || !tt.HasText("B.java") {
		t.Fatal("recursive expansion did not open both branches")
	}
	tt.Key(mod, ui.KeyLeft)
	hs.settle()
	if a.files.Open.Has(hs.path("branch/a")) || a.files.Open.Has(hs.path("branch/a/more")) || a.files.Open.Has(hs.path("branch/a/more/deep")) || a.files.Open.Has(hs.path("branch/b")) {
		t.Fatal("recursive collapse left children expanded")
	}
}

func TestCompactExplorerPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := settings.Load(path)
	if s.ExplorerCompactFolders {
		t.Fatal("existing folders should stay separate by default")
	}
	s.ExplorerCompactFolders = true
	s.Flush()
	if !settings.Load(path).ExplorerCompactFolders {
		t.Fatal("compact folders was not saved")
	}
	p, err := settings.Default().Prefs.WithSynced(s.Prefs.Synced())
	if err != nil || !p.ExplorerCompactFolders {
		t.Fatalf("compact folders did not sync: %v", err)
	}
}

func TestCompactExplorerKeepsPartialExpansion(t *testing.T) {
	hs := compactExplorerHarness(t)
	a := hs.a
	a.run("view.compactFolders")
	hs.settle()
	a.files.Open.Add(hs.path("src"))
	a.files.Open.Add(hs.path("src/main"))
	hs.settle()
	a.run("view.compactFolders")
	hs.settle()
	if !a.commandOn("view.compactFolders") || !a.files.Open.Has(hs.path(packageChain)) {
		t.Fatal("enabling compact folders lost a partly expanded chain")
	}
	a.run("view.compactFolders")
	hs.settle()
	if a.commandOn("view.compactFolders") || !a.files.Open.Has(hs.path("src")) || !a.files.Open.Has(hs.path("src/main")) {
		t.Fatal("uncompacting did not keep the original folders expanded")
	}
	// A deeper branch may not be visited at all in the frame that enables
	// compaction. Its first folder's expansion still carries over later.
	if err := os.WriteFile(hs.path("branch/a/README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	a.revealInTree(hs.path("branch/a/more/deep/A.java"))
	hs.until("the deeper branch", func() bool { return hs.tt.HasText("deep") && hs.tt.HasText("A.java") })
	a.files.Open.Remove(hs.path("branch"))
	a.files.Open.Remove(hs.path("branch/a"))
	a.files.Open.Remove(hs.path("branch/a/more/deep"))
	hs.settle()
	a.run("view.compactFolders")
	hs.settle()
	a.files.Open.Add(hs.path("branch"))
	a.files.Open.Add(hs.path("branch/a"))
	hs.settle()
	if !a.files.Open.Has(hs.path("branch/a/more/deep")) || !hs.tt.HasText("A.java") {
		t.Fatal("a branch first shown later lost its partial expansion")
	}
}

func TestExplorerDeletedSelectionDoesNotChooseParent(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			hs := compactExplorerHarness(t)
			a := hs.a
			a.settings.ExplorerCompactFolders = compact
			path := hs.path("mixed/README")
			a.revealInTree(path)
			hs.until("the selected file", func() bool { return a.explorerChoice() == path })
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			a.tree.Refresh()
			hs.until("the deleted row", func() bool { return explorerDepth(a, path) < 0 })
			if choice := a.explorerChoice(); choice != "" && a.tree.IsDir(choice) {
				t.Fatalf("deleting a file chose a folder: %q", choice)
			}
		})
	}
}

func TestCompactExplorerLimitsBackgroundReads(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	for i := range 24 {
		if err := os.MkdirAll(hs.path(fmt.Sprintf("package%02d/nested", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"node_modules", ".venv", "target"} {
		if err := os.MkdirAll(hs.path(name+"/dependency/child"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.tree.Refresh()
	hs.until("the folders", func() bool { return hs.tt.HasText("package00") })
	a.settings.ExplorerCompactFolders = true
	// Without draining completions, only a bounded set of automatic
	// directory reads may start, even when many folders are visible.
	view := a.explorerRows()
	if len(view.pending) == 0 || len(view.pending) > 8 {
		t.Fatalf("started %d background reads", len(view.pending))
	}
	a.explorer = view
	hs.until("all compact rows", func() bool {
		for i := range 24 {
			if a.explorer.members[hs.path(fmt.Sprintf("package%02d", i))] != hs.path(fmt.Sprintf("package%02d/nested", i)) {
				return false
			}
		}
		return true
	})
	for _, name := range []string{"node_modules", ".venv", "target"} {
		if a.tree.Loaded(hs.path(name)) || a.explorer.inspected[hs.path(name)] {
			t.Errorf("automatically inspected %s", name)
		}
	}
	// An explicit expansion still reads a skipped directory normally.
	a.files.Open.Add(hs.path("node_modules"))
	hs.until("the dependency folder", func() bool { return a.tree.Loaded(hs.path("node_modules")) })
	if a.tree.Loaded(hs.path("node_modules/dependency")) {
		t.Fatal("opening dependencies automatically read their children")
	}
}

func TestCompactExplorerRemoteRefresh(t *testing.T) {
	hs, conn, root := remoteHarness(t)
	a, tt := hs.a, hs.tt
	if _, err := conn.Run(nil, `mkdir -p "$1/java/com/example" && printf 'class App {}\n' > "$1/java/com/example/App.java"`, root); err != nil {
		t.Fatal(err)
	}
	a.run("view.compactFolders")
	a.tree.Refresh()
	hs.untilLong("the compact remote package", func() bool { return tt.HasText("java/com/example") })
	// Even collapsed rows must refresh their intermediate directories.
	if _, err := conn.Run(nil, `printf 'new sibling\n' > "$1/java/com/README"`, root); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	hs.untilLong("the split remote package", func() bool { return tt.HasText("java/com") && !tt.HasText("java/com/example") })
	if err := tt.Click("java/com"); err != nil {
		t.Fatal(err)
	}
	hs.untilLong("the remote sibling", func() bool { return tt.HasText("README") && tt.HasText("example") })
}

func TestCompactExplorerBoundsAutomaticTraversal(t *testing.T) {
	hs := newHarness(t, true)
	chain := strings.TrimSuffix(strings.Repeat("pkg/", 70), "/")
	if err := os.MkdirAll(hs.path(chain), 0o755); err != nil {
		t.Fatal(err)
	}
	hs.a.run("view.compactFolders")
	hs.a.tree.Refresh()
	label := strings.TrimSuffix(strings.Repeat("pkg/", 64), "/")
	hs.until("the bounded chain", func() bool { return hs.tt.HasText(label) })
	hs.settle()
	if hs.a.tree.Loaded(hs.path(label)) {
		t.Fatal("compaction kept reading beyond the bounded chain")
	}
	// Expansion continues from the real folder where this row ends.
	if err := hs.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	hs.until("the rest of the chain", func() bool { return hs.tt.HasText("pkg/pkg/pkg/pkg/pkg/pkg") })
}

// A change in a folder hidden in a compact row shows on the row.
func TestCompactExplorerMarksChangesInHiddenFolders(t *testing.T) {
	hs := newHarness(t, true)
	hs.root = t.TempDir()
	for _, name := range []string{"a/b/gone.txt", "a/b/c/kept.txt"} {
		path := hs.path(name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("text\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = hs.root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if err := os.Remove(hs.path("a/b/gone.txt")); err != nil {
		t.Fatal(err)
	}
	a := hs.a
	a.settings.ExplorerCompactFolders = true
	a.openFolder(hs.root)
	hs.until("the compact chain", func() bool { return hs.tt.HasText("a/b/c") })
	hs.until("the deleted file's mark", func() bool {
		return a.chainLetter(hs.path("a/b/c"), hs.path("a")) != 0
	})
	if letter := a.chainLetter(hs.path("a/b/c"), hs.path("a/b/c")); letter != 0 {
		t.Fatalf("the last folder alone has %q", letter)
	}
}

// A file new in a folder hidden in a collapsed row splits the row as the
// watcher sees it, and shows in the folder revealed before that.
func TestCompactExplorerRevealsFileThatSplitsAChain(t *testing.T) {
	hs := compactExplorerHarness(t)
	a := hs.a
	file := hs.path("src/main/new.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.revealInTree(file)
	hs.until("the revealed file", func() bool { return a.explorerChoice() == file })
	if !a.files.Open.Has(hs.path("src/main")) || a.explorer.rows[hs.path("src/main")].name != "src/main" {
		t.Fatalf("row %+v", a.explorer.rows[hs.path("src/main")])
	}
	if a.explorer.rows[hs.path(packageChain)].name != "java/com/example" {
		t.Fatalf("row %+v", a.explorer.rows[hs.path(packageChain)])
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	hs.until("the chain again", func() bool { return a.explorer.rows[hs.path(packageChain)].name == packageChain })
}
