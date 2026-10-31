package main

import (
	"os"
	"strings"
	"testing"

	"mayoi/internal/git"
)

func TestPRKey(t *testing.T) {
	key := prKey(12, "aaa", "bbb")
	n, from, to, ok := parsePRKey(key)
	if !ok || n != 12 || from != "aaa" || to != "bbb" {
		t.Fatalf("parsePRKey(%q) = %d %q %q %v", key, n, from, to, ok)
	}
	for _, bad := range []string{"aaa..bbb", "pr:x:a..b", "pr:0:a..b", "pr:3:ab", "pr:3:..b", "pr:3"} {
		if _, _, _, ok := parsePRKey(bad); ok {
			t.Errorf("parsePRKey(%q) took it", bad)
		}
	}
	v := newCommitView(layoutAuto, key, git.Commit{})
	if v.commit.pr == nil || v.commit.pr.number != 12 || v.commit.from != "aaa" || v.commit.hash != "bbb" || v.commit.key() != key {
		t.Fatalf("view of %q: %+v", key, v.commit)
	}
	if v.name() != "#12" {
		t.Fatalf("name %q", v.name())
	}
	note := &reviewNote{path: "/r/a.go", commit: key, from: 0, to: 1}
	if ref := note.ref(func(p string) string { return strings.TrimPrefix(p, "/r/") }); !strings.Contains(ref, "in pull request #12") {
		t.Fatalf("ref %q", ref)
	}
}

// A pull request's tab shows the changes from where its head left its
// base, folds the files viewed, and is the one tab of it as its head moves.
func TestShowPullRequest(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("bee\n"), 0o644)
	run := hs.gitRepo()
	rev := func(r string) string { return strings.TrimSpace(run("rev-parse", r)) }
	base := rev("HEAD")
	run("switch", "-q", "-c", "feature")
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("bee 2\n"), 0o644)
	run("commit", "-qam", "feature")
	head := rev("HEAD")
	root := a.git.repo.Root

	info := &git.PRInfo{Number: 5, ID: "PR_x", Title: "Fix things", URL: "https://github.com/o/r/pull/5", State: "OPEN",
		BaseRef: "main", HeadRef: "feature", BaseOid: base, HeadOid: head}
	a.showPullRequest(info, base, map[string]bool{"a.txt": true}, root)
	v := a.column().tabs[a.column().active].diff
	if v == nil || v.commit == nil || v.commit.pr == nil {
		t.Fatal("no pull request tab")
	}
	hs.settleDiff(v)
	if v.name() != "#5 Fix things" || len(v.files) != 2 {
		t.Fatalf("tab %q, files %d", v.name(), len(v.files))
	}
	fa, fb := v.files[0], v.files[1]
	if fa.prPath != "a.txt" || !fa.folded || !v.fileViewed(fa) {
		t.Fatalf("a.txt viewed %+v", fa)
	}
	if fb.prPath != "b.txt" || fb.folded || v.fileViewed(fb) || v.viewedCount() != 1 {
		t.Fatalf("b.txt %+v, viewed %d", fb, v.viewedCount())
	}
	hs.settle()
	hs.shot("pull-request")
	tabs := len(a.column().tabs)

	// The head moves: the tab shows the new commits, in its place.
	os.WriteFile(hs.path("b.txt"), []byte("bee 3\n"), 0o644)
	run("commit", "-qam", "more")
	newer := *info
	newer.HeadOid = rev("HEAD")
	a.showPullRequest(&newer, base, nil, root)
	if len(a.column().tabs) != tabs {
		t.Fatalf("tabs %d, want %d", len(a.column().tabs), tabs)
	}
	w := a.column().tabs[a.column().active].diff
	if w == v || w.commit.hash != newer.HeadOid || w.commit.pr.viewedOK {
		t.Fatalf("reloaded tab %+v", w.commit)
	}
	hs.settleDiff(w)
	if len(w.files) != 2 {
		t.Fatalf("files %d", len(w.files))
	}
}

// The picker lists the number typed first, then the pull requests open
// whose words match.
func TestPRChoices(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.prs = prPicker{open: true, prs: []git.PRListed{
		{Number: 7, Title: "Add tabs", Author: "ann", HeadRef: "tabs"},
		{Number: 9, Title: "Fix crash", Author: "bob", HeadRef: "crash", Draft: true},
	}}
	ctx := a.repository()
	if got := ctx.prChoices(); len(got) != 2 || got[0].number != 7 || !strings.Contains(got[1].detail, "bob") {
		t.Fatalf("all: %+v", got)
	}
	a.prs.query = "crash"
	if got := ctx.prChoices(); len(got) != 1 || got[0].number != 9 {
		t.Fatalf("crash: %+v", got)
	}
	a.prs.query = "#42"
	if got := ctx.prChoices(); len(got) != 1 || got[0].number != 42 || !got[0].typed {
		t.Fatalf("number: %+v", got)
	}
}
