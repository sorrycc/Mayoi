package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func TestSourceControlBranchPickerCreate(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	before := strings.TrimSpace(run("rev-parse", "HEAD"))
	openSCM(hs)
	if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.branches.open || !hs.tt.HasText(locales.CreateBranchEllipsis()) {
		t.Fatal("source control did not open the shared picker with a create entry")
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
	// A running Git command disables the branch button.
	a.git.busy = "held operation"
	hs.settle()
	if err := hs.tt.Click(locales.BranchLabel(a.branch())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.branches.open {
		t.Fatal("busy source control opened the picker")
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
		t.Fatal("branch picker belongs to the wrong repository")
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

// Without a query the branches come in two groups, the local ones first and
// the current one at their top, and each row says who made its last commit.
func TestBranchPickerGroupsAndAuthors(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("switch", "-q", "-c", "topic")
	run("commit", "-q", "--allow-empty", "--author=Ann <ann@t>", "-m", "Add topic")
	run("switch", "-q", "main")
	run("update-ref", "refs/remotes/origin/remote-only", "HEAD")
	hs.a.git.checked = time.Time{}
	hs.settleGit()
	openSCM(hs)
	if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	deadline := time.Now().Add(8 * time.Second)
	for hs.a.branches.loading && time.Now().Before(deadline) {
		hs.settle()
	}
	b := &hs.a.branches
	if len(b.branches) != 3 || b.branches[0].Name != "main" || b.branches[1].Name != "topic" || !b.branches[2].Remote {
		t.Fatalf("branches in order %+v, want main, topic, then the remote's", b.branches)
	}
	if b.branches[1].Author != "Ann" {
		t.Fatalf("author of topic %q, want Ann", b.branches[1].Author)
	}
	if !hs.tt.HasText(locales.BranchLocalGroup()) || !hs.tt.HasText(locales.BranchRemoteGroup()) {
		t.Fatal("empty query does not show the two groups")
	}
	// Headings are not rows of the box: the keys move among the branches only.
	choices := hs.a.repository().branchChoices()
	if len(choices) != 4 {
		t.Fatalf("%d rows, want the create row and three branches", len(choices))
	}
	if g := b.groupOf(choices[1]); g != locales.BranchLocalGroup() {
		t.Fatalf("first branch is in %q", g)
	}
	if g := b.groupOf(choices[3]); g != locales.BranchRemoteGroup() {
		t.Fatalf("remote branch is in %q", g)
	}
	// A query keeps the matches in one ranking, without headings.
	hs.tt.Type("topic")
	hs.settle()
	if hs.tt.HasText(locales.BranchRemoteGroup()) || hs.tt.HasText(locales.BranchLocalGroup()) {
		t.Fatal("a query still shows the groups")
	}
	if !hs.tt.HasText("Ann") {
		t.Fatal("row of topic does not show its author")
	}
	// A commit with no author leaves no separator dangling at the row's start.
	hs.a.branches.branches[1].Author = ""
	hs.settle()
	for _, s := range hs.tt.Texts() {
		if s = strings.TrimSpace(s); strings.HasPrefix(s, "•") || strings.HasSuffix(s, "•") || strings.Contains(s, "• •") {
			t.Fatalf("row metadata %q has a dangling separator", s)
		}
	}
}

// With no branch to move among, the arrows and Enter do nothing.
func TestBranchPickerNoRowsArrowsAndEnter(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	openSCM(hs)
	if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Type("bad..no-match-xyz")
	hs.settle()
	for _, k := range []ui.Key{ui.KeyDown, ui.KeyUp, ui.KeyDown} {
		hs.tt.Key(0, k)
		hs.settle()
	}
	if !hs.a.branches.open || hs.a.fails != 0 {
		t.Fatalf("no rows: failures %d, open %v", hs.a.fails, hs.a.branches.open)
	}
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if hs.a.fails != 0 {
		t.Fatalf("Enter with no rows: failures %d", hs.a.fails)
	}
}
