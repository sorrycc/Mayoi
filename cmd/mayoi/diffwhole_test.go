package main

import (
	"os"
	"testing"
)

func TestRestoreDeletedFileFromDiff(t *testing.T) {
	hs := newHarness(t, true)
	gitCmd := hs.gitRepo()
	a := hs.a
	p := hs.path("gone.txt")
	os.WriteFile(p, []byte("kept\n"), 0o644)
	gitCmd("add", ".")
	gitCmd("commit", "-qm", "base")
	os.Remove(p)
	hs.settleGit()
	a.showChanges(diffTarget{path: p, base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	f := a.activeTab().diff.files[0]
	if f.new.ok || !f.old.ok {
		t.Fatalf("sides: old=%v new=%v", f.old.ok, f.new.ok)
	}
	a.restoreDeletedFile(f.entry)
	if a.confirm == nil {
		t.Fatal("restore did not ask for confirmation")
	}
	run := a.confirm.run
	a.confirm = nil
	run()
	hs.settleGit()
	if data, err := os.ReadFile(p); err != nil || string(data) != "kept\n" {
		t.Fatalf("restored file: %q, %v", data, err)
	}
}
