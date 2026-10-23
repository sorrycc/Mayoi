package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func TestStatusBarBranchPickerCreate(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	before := strings.TrimSpace(run("rev-parse", "HEAD"))
	// The status bar works without first opening Source Control.
	if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.branches.open || !hs.tt.HasText(locales.CreateBranchEllipsis()) {
		t.Fatal("status bar did not open the shared picker with a create entry")
	}
	// The footer names what Enter does on the selected row.
	if !hs.tt.HasText(locales.QuickSwitchAction()) {
		t.Fatal("footer does not say Enter switches")
	}
	hs.tt.Key(0, ui.KeyUp)
	hs.settle()
	if !hs.tt.HasText(locales.QuickCreateAction()) {
		t.Fatal("footer does not say Enter creates")
	}
	if err := hs.tt.Click(locales.CreateBranchEllipsis()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.branches.open || a.prompt == nil || a.prompt.hint != locales.BranchFrom("main") {
		t.Fatal("create entry did not open the name prompt for main")
	}
	hs.tt.Type("bad..name")
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if a.prompt == nil || a.prompt.err != locales.BranchInvalidName() || a.branch() != "main" {
		t.Fatal("invalid name was not kept in the prompt")
	}
	hs.tt.Key(ui.Cmd, ui.KeyA)
	hs.tt.Type("main")
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if a.prompt == nil || a.prompt.err != locales.RefExists("false") {
		t.Fatal("existing branch was not rejected")
	}
	hs.tt.Key(ui.Cmd, ui.KeyA)
	hs.tt.Type("feature/from-picker")
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if a.prompt != nil || a.branch() != "feature/from-picker" || strings.TrimSpace(run("rev-parse", "HEAD")) != before {
		t.Fatal("prompt did not create and switch from the current HEAD")
	}
	// A running Git command disables the status-bar entry as well as SCM's.
	a.git.busy = "held operation"
	hs.settle()
	if err := hs.tt.Click(locales.BranchLabel(a.branch())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.branches.open {
		t.Fatal("busy status bar opened the picker")
	}
	a.git.busy = ""
}

func TestBranchNamesForCreation(t *testing.T) {
	for _, name := range []string{"", "HEAD", "-option", "bad name", "bad\tname", "bad\x7fname", "bad..name", "bad@{name", "bad~name", "bad:name", "bad\\name", ".hidden", "feature/.hidden", "feature.lock", "feature.lock/topic", "feature/", "/feature", "feature//topic", "feature."} {
		if validBranch(name) {
			t.Errorf("accepts invalid branch name %q", name)
		}
	}
	for _, name := range []string{"topic", "feature/topic", "feature/v1.2", "功能/分支"} {
		if !validBranch(name) {
			t.Errorf("rejects valid branch name %q", name)
		}
	}
}

func TestSubmoduleBranchPickerCreate(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	root := sub.git.repo.Root
	parentHead := strings.TrimSpace(parent("rev-parse", "HEAD"))
	a.selectRepository(root)
	hs.settle()
	if err := hs.tt.Click(locales.BranchLabel(sub.branch())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.branches.repoRoot != root {
		t.Fatal("status-bar picker belongs to the wrong repository")
	}
	// BranchList runs in the background; a fixed number of frames does
	// not guarantee it has completed when the full suite is busy.
	deadline := time.Now().Add(8 * time.Second)
	for a.branches.loading && time.Now().Before(deadline) {
		hs.settle()
	}
	if a.branches.loading {
		t.Fatal("the branch picker never finished reading branches")
	}
	if err := hs.tt.Click(locales.CreateBranchEllipsis()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// Changing the selected repository must not redirect the open prompt.
	a.selectRepository(a.git.repo.Root)
	hs.settle()
	hs.tt.Type("module-work")
	hs.tt.Key(0, ui.KeyEnter)
	settleRepositories(t, hs, 2)
	if sub.branch() != "module-work" || a.git.status.Branch != "main" || strings.TrimSpace(parent("rev-parse", "HEAD")) != parentHead {
		t.Fatal("creating the module branch changed the parent repository")
	}
}
