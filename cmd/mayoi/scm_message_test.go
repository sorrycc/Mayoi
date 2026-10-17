package main

import (
	"strings"
	"testing"

	"mayoi/locales"
)

func TestScmCommitMessageSizesToTextAndHint(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	hs.a.showSourceControl()
	hs.a.window.SidebarWidth = 360
	hs.settle()
	short, ok := hs.tt.Find(locales.ScmCommitMessage())
	if !ok || short.H > 32 {
		t.Fatalf("short message field: %v %v", short, ok)
	}
	git("checkout", "-qb", "feature/a-long-branch-name-that-wraps-the-commit-hint")
	hs.a.window.SidebarWidth = 240
	hs.settleGit()
	wrapped, _ := hs.tt.Find(locales.ScmCommitMessage())
	if wrapped.H <= short.H {
		t.Fatalf("wrapped placeholder did not grow: %v -> %v", short, wrapped)
	}
	hs.shot("compact-commit-wrapped-hint")
	hs.a.git.message = "subject\n\nbody"
	hs.settle()
	multiline, _ := hs.tt.Find(locales.ScmCommitMessage())
	if multiline.H <= short.H {
		t.Fatalf("multiline field did not grow: %v", multiline)
	}
	hs.a.git.message = strings.Repeat("line\n", 20)
	hs.settle()
	capped, _ := hs.tt.Find(locales.ScmCommitMessage())
	if capped.H > 160 {
		t.Fatalf("long message exceeded height limit: %v", capped)
	}
}
