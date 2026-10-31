package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStash(t *testing.T) {
	r, _ := repo(t)
	if _, err := r.Stash("", false); !errors.Is(err, ErrNothingToStash) {
		t.Fatalf("stash of a clean work tree: %v", err)
	}
	write(t, r.Root, "a.txt", "changed\n")
	write(t, r.Root, "new.txt", "new\n")
	first, err := r.Stash("first", false)
	if err != nil || first == "" {
		t.Fatalf("stash %q, %v", first, err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "new.txt")); err != nil {
		t.Fatal("an untracked file was stashed without asking")
	}
	if _, err := r.Stash("", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked file was not stashed")
	}
	list, err := r.StashList(10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v, %v", list, err)
	}
	if list[1].Hash != first || list[1].Message != "first" || list[1].Branch != "main" || list[1].Ref() != "stash@{1}" || list[1].When.IsZero() {
		t.Fatalf("named stash %+v", list[1])
	}
	if list[0].Message != "WIP: first" || list[0].Branch != "main" {
		t.Fatalf("unnamed stash %+v", list[0])
	}
	if err := r.StashPop(list[0].Hash); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Root, "new.txt")); string(b) != "new\n" {
		t.Fatalf("pop left new.txt %q", b)
	}
	if err := r.StashDrop(list[0].Hash); err == nil {
		t.Fatal("dropped a stash already popped")
	}
	// The other stash moved to stash@{0}; its hash still finds it.
	if err := r.StashApply(first); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Root, "a.txt")); string(b) != "changed\n" {
		t.Fatalf("apply left a.txt %q", b)
	}
	if err := r.StashDrop(first); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.StashList(10); len(list) != 0 {
		t.Fatalf("left %+v", list)
	}
}

func TestStashSubject(t *testing.T) {
	for _, tc := range []struct{ in, branch, msg string }{
		{"On main: wip", "main", "wip"},
		{"On feat/x: a: b", "feat/x", "a: b"},
		{"WIP on main: 1234567 fix it", "main", "WIP: fix it"},
		{"autostash", "", "autostash"},
	} {
		if b, m := stashSubject(tc.in); b != tc.branch || m != tc.msg {
			t.Errorf("%q: %q %q", tc.in, b, m)
		}
	}
}
