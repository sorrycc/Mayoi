package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/settings"
	"mayoi/locales"
)

// settleNested waits for the work trees under the folder to be found and read.
func settleNested(t *testing.T, hs *harness, count int) {
	t.Helper()
	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		hs.settle()
		contexts := hs.a.repositories.contexts
		ready := len(contexts) == count+1 && !hs.a.repositories.scan.Running()
		for _, ctx := range contexts[min(1, len(contexts)):] {
			g := ctx.git
			ready = ready && !g.statusRead.Running() && !g.statusRead.Stale() && g.pending == 0 && g.busy == "" && g.status != nil
		}
		if ready {
			return
		}
	}
	t.Fatalf("work trees not found: want %d, got %+v", count, hs.a.repositories.contexts)
}

func TestNestedRepositoriesInWorktree(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	a := hs.a
	gitIn(t, hs.root, "init", "-q", "-b", "main")
	deep := hs.path("reference-projects/group/project-b")
	shallow := hs.path("reference-projects/project-a")
	for _, dir := range []string{shallow, deep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q", "-b", "main")
	}
	// The enclosing repository ignores the clones.
	if err := os.WriteFile(hs.path(".gitignore"), []byte("reference-projects/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFolder(hs.root)
	a.showSourceControl()
	settleNested(t, hs, 0)
	if a.git.repo == nil || a.git.repo.Root != hs.root {
		t.Fatal("the workspace repository is missing")
	}

	setDepth := func(value string) {
		t.Helper()
		if err := settings.WriteFile(settings.FolderPath(hs.root), []byte(value)); err != nil {
			t.Fatal(err)
		}
		a.folderPrefs.f.Reload()
		a.folderPrefsChanged()
	}
	setDepth(`{"gitScanDepth": 2}`)
	settleNested(t, hs, 1)
	if !hs.tt.HasText("project-a") || hs.tt.HasText("project-b") {
		t.Fatal("Source Control should show project-a at depth 2")
	}
	setDepth(`{"gitScanDepth": 3}`)
	settleNested(t, hs, 2)
	file := filepath.Join(deep, "f.txt")
	if ctx := a.repositoryForPath(file); ctx.git.repo == nil || ctx.git.repo.Root != deep {
		t.Fatal("the file should belong to project-b")
	}
	if !hs.tt.HasText("project-b") {
		t.Fatal("Source Control should show project-b at depth 3")
	}
	a.diskChanged([]string{hs.path(".gitignore"), file}, nil)
	if a.repositories.scan.Stale() {
		t.Fatal("saving ordinary files should not rescan")
	}

	later := hs.path("reference-projects/group/project-c")
	if err := os.MkdirAll(later, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, later, "init", "-q", "-b", "main")
	a.diskChanged([]string{later}, nil)
	settleNested(t, hs, 3)
	setDepth("{}")
	settleNested(t, hs, 0)
}

// A folder of projects, each its own work tree, lists them all in Source
// Control and commits in each (#17).
func TestNestedRepositories(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	a := hs.a
	var roots []string
	for _, name := range []string{"project-a", "project-b"} {
		root := hs.path(name)
		os.MkdirAll(root, 0o755)
		gitIn(t, root, "init", "-q", "-b", "main")
		gitIn(t, root, "config", "user.name", "t")
		gitIn(t, root, "config", "user.email", "t@t")
		os.WriteFile(filepath.Join(root, "f.txt"), []byte("one\n"), 0o644)
		gitIn(t, root, "add", ".")
		gitIn(t, root, "commit", "-q", "-m", "first")
		roots = append(roots, root)
	}
	a.repositories.scan.Invalidate()
	a.showSourceControl()
	settleNested(t, hs, 2)
	if a.git.repo != nil {
		t.Fatalf("the folder is a work tree: %v", a.git.repo.Root)
	}
	file := filepath.Join(roots[1], "f.txt")
	os.WriteFile(file, []byte("two\n"), 0o644)
	a.gitChanged()
	settleNested(t, hs, 2)
	b := a.repositoryForPath(file)
	if b.git.repo == nil || b.git.repo.Root != roots[1] || b.changeCount() != 1 {
		t.Fatalf("project-b: %+v", b.git.status)
	}
	if a.repository().git.repo == nil {
		t.Fatal("no repository for window commands")
	}
	if hs.tt.HasText(locales.ScmNoRepo()) || !hs.tt.HasText("project-a") || !hs.tt.HasText("project-b") {
		t.Fatalf("source control: %q", hs.tt.Texts())
	}
	hs.shot("nested-repositories")
	b.git.message = "in b"
	b.stage(file)
	settleNested(t, hs, 2)
	b.commit(false)
	settleNested(t, hs, 2)
	if got := strings.TrimSpace(gitIn(t, roots[1], "log", "-1", "--format=%s")); got != "in b" {
		t.Fatalf("project-b log: %q", got)
	}
	if got := strings.TrimSpace(gitIn(t, roots[0], "log", "-1", "--format=%s")); got != "first" {
		t.Fatalf("project-a log: %q", got)
	}
	// Saving a file in a project does not look for work trees again.
	a.diskChanged([]string{file}, nil)
	if a.repositories.scan.Stale() {
		t.Fatal("a file saved in a project rescans")
	}
	// A project cloned later is found: the watcher reports its folder,
	// whose .git it does not watch yet.
	c := hs.path("project-c")
	os.MkdirAll(c, 0o755)
	gitIn(t, c, "init", "-q")
	a.diskChanged([]string{c}, nil)
	if !a.repositories.scan.Stale() {
		t.Fatal("a project cloned does not rescan")
	}
	settleNested(t, hs, 3)
	// One removed is dropped.
	os.RemoveAll(c)
	a.diskChanged(nil, []string{c})
	if !a.repositories.scan.Stale() {
		t.Fatal("a project removed does not rescan")
	}
	settleNested(t, hs, 2)
}

// A folder with no work tree in or under it still offers to make one.
func TestNoNestedRepositories(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.showSourceControl()
	hs.settle()
	if !hs.tt.HasText(locales.ScmNoRepo()) {
		t.Fatalf("source control: %q", hs.tt.Texts())
	}
}
