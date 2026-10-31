package main

import (
	"os"
	"testing"
)

func TestStashPopAndDrop(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(hs.path("notes.txt"), []byte("hi\n"), 0o644)
	a.run("view.scm")
	hs.settleGit()
	a.run("git.stash")
	if a.prompt == nil || !a.prompt.optional {
		t.Fatalf("stash did not ask for a message: %+v", a.prompt)
	}
	if msg := a.prompt.done("wip"); msg != "" {
		t.Fatal(msg)
	}
	a.prompt = nil
	hs.settleGit()
	if g := a.groupEntries(); len(g[groupChanges]) != 0 || len(g[groupUntracked]) != 1 {
		t.Fatalf("groups after stashing %v", g)
	}
	if len(a.git.stashes) != 1 || a.git.stashes[0].Message != "wip" {
		t.Fatalf("stashes %+v", a.git.stashes)
	}
	if !hs.tt.HasText("Changes stashed") || !hs.tt.HasText("Pop") {
		t.Fatalf("no toast to pop with: %q", hs.tt.Texts())
	}
	a.repository().popStash(a.git.stashes[0].Hash)
	hs.settleGit()
	if g := a.groupEntries(); len(g[groupChanges]) != 1 || len(a.git.stashes) != 0 {
		t.Fatalf("after popping: %v, %+v", g, a.git.stashes)
	}

	// An unnamed stash, of the untracked file too, shows in Stashes….
	a.run("git.stashUntracked")
	a.prompt.done("")
	a.prompt = nil
	hs.settleGit()
	if n := a.changeCount(); n != 0 || len(a.git.stashes) != 1 {
		t.Fatalf("%d changes, stashes %+v", n, a.git.stashes)
	}
	a.run("git.stashes")
	hs.settle()
	if !a.stashes.open || !hs.tt.HasText(a.git.stashes[0].Message) {
		t.Fatalf("no stash in the box: %q", hs.tt.Texts())
	}
	a.stashes.open = false
	// Dropping asks first.
	a.repository().dropStash(a.git.stashes[0])
	if a.confirm == nil || !a.confirm.danger {
		t.Fatalf("drop did not ask: %+v", a.confirm)
	}
	hs.settleGit()
	if len(a.git.stashes) != 1 {
		t.Fatal("dropped before asked")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if len(a.git.stashes) != 0 {
		t.Fatalf("stashes after dropping %+v", a.git.stashes)
	}
}
