package main

import (
	"os"
	"path/filepath"
	"testing"

	"mayoi/internal/bg"
	"mayoi/internal/git"
)

// chainWaits reports whether the chain waits for the command running in
// ctx, as pumpChain is called with one step still to run.
func chainWaits(a *app, ctx *gitContext) bool {
	a.git.busy, a.git.pending, a.git.statusRead = "", 0, bg.Refresher[*git.Repo]{}
	ctx.git.pending++
	defer func() { ctx.git.pending-- }()
	a.chain = []chainStep{{name: "mayoi.noSuchCommand"}}
	a.chainFails = a.fails
	a.pumpChain()
	waited := len(a.chain) == 1
	a.chain = nil
	return waited
}

// A chained shortcut waits for a command in the selected submodule, not
// only for those in the folder's own work tree.
func TestChainWaitsForSubmoduleCommand(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	a.selectRepository(sub.git.repo.Root)
	hs.settle()
	if a.repository() != sub {
		t.Fatal("the submodule is not selected")
	}
	if !chainWaits(a, sub) {
		t.Fatal("the chain ran on while the submodule's command ran")
	}
}

// A chained shortcut waits for a command in a work tree under a folder
// that is not one (#17).
func TestChainWaitsInFolderOfRepositories(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	a := hs.a
	root := hs.path("project")
	os.MkdirAll(root, 0o755)
	gitIn(t, root, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("one\n"), 0o644)
	a.repositories.dirty = true
	a.showSourceControl()
	settleNested(t, hs, 1)
	if a.git.repo != nil {
		t.Fatal("the folder is a work tree")
	}
	if !chainWaits(a, a.repository()) {
		t.Fatal("the chain ran on while the work tree's command ran")
	}
}

// While a merge is in progress, the branch picker does not open, as the
// panel's checkout does not, but committing still ends the merge.
func TestBranchPickerWaitsForMerge(t *testing.T) {
	hs := newHarness(t, false)
	hs.gitRepo()
	a := hs.a
	st := *a.git.status
	st.Op = git.OpMerge
	a.git.status = &st
	if a.git.canMutate() || a.git.commanding() {
		t.Fatalf("during a merge: canMutate %v, commanding %v", a.git.canMutate(), a.git.commanding())
	}
	a.openBranchPicker()
	if a.branches.open {
		t.Fatal("the branch picker opened during a merge")
	}
}
