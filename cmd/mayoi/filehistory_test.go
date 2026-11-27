package main

import (
	"fmt"
	"os"
	"slices"
	"testing"
)

// TestFileHistory scopes source control's commits to the commits that
// changed a file: its row says which file, a click shows that file's
// changes of the commit alone, its toolbar every file of the commit, and
// the button beside the path the branch's history again.
func TestFileHistory(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	zz, aa := hs.path("zz.txt"), hs.path("aa.txt")
	os.WriteFile(zz, []byte("z\n"), 0o644)
	os.WriteFile(aa, []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "add both")
	os.WriteFile(zz, []byte("z\nzz\n"), 0o644)
	os.WriteFile(aa, []byte("a\naa\n"), 0o644)
	git("commit", "-qam", "change both")
	os.WriteFile(aa, []byte("a\naa\naaa\n"), 0o644)
	git("commit", "-qam", "change aa")

	// Many changes above the commits: the view scrolls to the file's.
	for i := range 12 {
		os.WriteFile(hs.path(fmt.Sprintf("scratch%d.txt", i)), []byte("x\n"), 0o644)
	}
	hs.settleGit()
	a.openPath(zz, true)
	hs.settle()
	a.run("git.fileHistory")
	hs.settleGit()
	hs.settle()
	if got := a.window.SidebarView; got != "git" {
		t.Fatalf("the sidebar shows %q, not source control", got)
	}
	g := a.repositoryForPath(zz).git
	if g.historyPath != zz {
		t.Fatalf("history of %q", g.historyPath)
	}
	var subjects []string
	for _, c := range g.history {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"change both", "add both"}; !slices.Equal(subjects, want) {
		t.Fatalf("commits %v, want %v", subjects, want)
	}
	ctx := a.repositoryForPath(zz)
	if rows := ctx.scmRows(); ctx.scm.row < 0 || ctx.scm.row >= len(rows) || !rows[ctx.scm.row].scope {
		t.Errorf("the view does not scroll to the file's history: row %d of %d", ctx.scm.row, len(rows))
	}
	if hs.tt.HasText("change aa") {
		t.Errorf("a file's history lists a commit of another file")
	}
	if !hs.tt.HasText("zz.txt") {
		t.Errorf("the view does not name the file: %q", hs.tt.Texts())
	}
	// A click on a commit shows the file's changes of it alone.
	if err := hs.tt.Click("change both"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	hs.settle()
	tab := a.activeTab()
	if tab == nil || tab.diff == nil || tab.diff.commit == nil || tab.diff.commit.info.Subject != "change both" {
		t.Fatalf("tab %+v", tab)
	}
	v := tab.diff
	if v.only != zz || len(v.files) != 1 || v.files[0].path != zz {
		t.Fatalf("the commit shows %+v, only %q", v.files, v.only)
	}
	if !v.files[0].loaded || len(v.files[0].hunks) == 0 {
		t.Fatalf("the file of the commit %+v", v.files[0])
	}
	// The toggle beside the panel shows every file of the commit again,
	// at the one it showed.
	if err := hs.tt.Click("Only This File"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.only != "" || len(v.files) != 2 {
		t.Fatalf("every file of the commit %+v, only %q", v.files, v.only)
	}
	if v.cursor < 0 || v.cursor >= len(v.rows) || v.files[v.rows[v.cursor].file].path != zz {
		t.Errorf("the commit opens at %d, not at zz.txt", v.cursor)
	}
	// Once more, for the file at its top.
	if err := hs.tt.Click("Only This File"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.only != zz || len(v.files) != 1 {
		t.Errorf("only %q of %+v", v.only, v.files)
	}
	// The branch's history comes back, without the file's commits alone.
	if err := hs.tt.Click("Clear File History"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	hs.settle()
	if got := a.repositoryForPath(zz).git.historyPath; got != "" {
		t.Fatalf("still scoped to %q", got)
	}
	if !hs.tt.HasText("change aa") || !hs.tt.HasText("change both") {
		t.Errorf("the branch's history stays scoped: %q", hs.tt.Texts())
	}
	if hs.tt.HasText("Clear File History") {
		t.Errorf("the scope row stays")
	}
	// A file no commit changed has no history, and says so.
	fresh := hs.path("fresh.txt")
	os.WriteFile(fresh, []byte("fresh\n"), 0o644)
	a.showFileHistory(fresh)
	hs.settleGit()
	hs.settle()
	if n := len(a.repositoryForPath(fresh).git.history); n != 0 {
		t.Fatalf("the untracked file's history %d commits", n)
	}
	if !hs.tt.HasText("No commit changed this file.") {
		t.Errorf("no empty state: %q", hs.tt.Texts())
	}
}

// TestFileHistoryFollowsRenames: a commit made before the file was
// renamed shows the file by the name it had then, alone.
func TestFileHistoryFollowsRenames(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	old, renamed, other := hs.path("old.txt"), hs.path("new.txt"), hs.path("other.txt")
	os.WriteFile(old, []byte("one\n"), 0o644)
	os.WriteFile(other, []byte("o\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "add both")
	os.WriteFile(old, []byte("one\ntwo\n"), 0o644)
	os.WriteFile(other, []byte("o\np\n"), 0o644)
	git("commit", "-qam", "change both")
	git("mv", "old.txt", "new.txt")
	git("commit", "-qm", "rename it")

	a.showFileHistory(renamed)
	hs.settleGit()
	hs.settle()
	g := a.repositoryForPath(renamed).git
	if g.historyPath != renamed || len(g.history) != 3 {
		t.Fatalf("history of %q: %d commits", g.historyPath, len(g.history))
	}
	// The rename's commit is newest, then those of the name before it.
	if err := hs.tt.Click("change both"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	hs.settle()
	tab := a.activeTab()
	if tab == nil || tab.diff == nil || tab.diff.commit == nil || tab.diff.commit.info.Subject != "change both" {
		t.Fatalf("tab %+v", tab)
	}
	v := tab.diff
	if v.only != old || len(v.files) != 1 || v.files[0].path != old {
		t.Fatalf("the commit shows %+v, only %q, not %q", v.files, v.only, old)
	}
	if !v.files[0].loaded || len(v.files[0].hunks) == 0 {
		t.Fatalf("the file of the commit %+v", v.files[0])
	}
	// The other file of the commit comes back with the toggle.
	if err := hs.tt.Click("Only This File"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.only != "" || len(v.files) != 2 {
		t.Fatalf("every file of the commit %+v, only %q", v.files, v.only)
	}
}

// TestFileHistoryClearsWhenTheFileLeaves: the scope is of a work tree, so
// another folder's history is the branch's own again.
func TestFileHistoryClearsWhenTheFileLeaves(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	path := hs.path("zz.txt")
	os.WriteFile(path, []byte("z\n"), 0o644)
	a.showFileHistory(path)
	hs.settleGit()
	if a.git.historyPath != path {
		t.Fatalf("history of %q", a.git.historyPath)
	}
	other := t.TempDir()
	a.openFolder(other)
	hs.settle()
	if a.git.historyPath != "" || len(a.git.history) != 0 {
		t.Errorf("another folder's history is scoped to %q", a.git.historyPath)
	}
}
