package main

import (
	"testing"

	"mayoi/internal/git"
)

// TestFolderOpenedIsListedAtOnce: the files of a folder opened are listed
// while the listing of the folder before still runs, which is dropped as
// it ends. It used to hold quick open's files back until it had ended.
func TestFolderOpenedIsListedAtOnce(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	hs.settle()
	before := folderKey{root: "/the/folder/before"}
	a.index = nil
	a.indexRead.Invalidate()
	a.indexRead.Start(before)
	a.indexRead.Invalidate()
	a.refreshIndex()
	if a.indexRead.Stale() {
		t.Fatal("the listing waits for the one of the folder before")
	}
	hs.settle()
	if len(a.index) == 0 {
		t.Fatal("the folder's files were not listed")
	}
	if a.indexRead.Done(before) {
		t.Error("the listing of the folder before was kept")
	}

	// What Git ignores is asked likewise.
	a.ignored = nil
	a.ignoredRead.Invalidate()
	a.ignoredRead.Start(before)
	a.ignoredRead.Invalidate()
	a.refreshIgnored()
	if a.ignoredRead.Stale() {
		t.Fatal("what Git ignores waits for the folder before")
	}
	hs.settle()
	if a.ignoredRead.Running() || a.ignoredRead.Done(before) {
		t.Error("the read for the folder before was kept")
	}
}

// TestWorkTreesLookedForAfterTheRootChanges: a look for the work trees
// that the folder's own work tree changed under, as Initialize Repository
// does, is dropped, and the next one runs. It used to leave the look
// marked as running for good.
func TestWorkTreesLookedForAfterTheRootChanges(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	hs.settle()
	state := &a.repositories
	before := repoScan{folder: a.folderKey(), root: &git.Repo{Root: "/the/work/tree/before"}}
	state.scan.Invalidate()
	state.scan.Start(before)
	scans := state.scans
	state.scan.Invalidate()
	a.discoverRepositories()
	if state.scans != scans+1 {
		t.Fatal("the look waits for the one of the work tree before")
	}
	hs.settle()
	if state.scan.Running() || state.scan.Stale() {
		t.Fatalf("the look did not end: %+v", state.scan)
	}
	if state.scan.Done(before) {
		t.Error("the look of the work tree before was kept")
	}
}
