package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	gitpkg "mayoi/internal/git"
)

func TestCommitToolsMenuAndComparison(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	os.WriteFile(hs.path("main.go"), []byte("package main\n// comparison result\n"), 0o644)
	run("commit", "-qam", "second commit\n\nFull message body")
	a, tt := hs.a, hs.tt
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[0]
	if err := tt.RightClick(cm.Subject); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Create Patch…", "Copy Changes (Patch)", "Compare HEAD to Here", "Compare Here to HEAD", "Select for Compare", "Open Commit on Remote"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Errorf("missing %q from menu %q", label, tt.Menu())
		}
	}
	for _, label := range []string{"Inspect Details", "Open in Commit Graph", "Reveal Commit in Side Bar", "Recompose Commits from Here…", "Create Worktree…", "Copy SHA", "Copy Message", "Collapse", "Expand", "Refresh"} {
		if slices.Contains(tt.Menu(), label) {
			t.Errorf("unexpected %q in commit menu", label)
		}
	}
	if slices.Index(tt.Menu(), "Create Patch…") < slices.Index(tt.Menu(), "Undo Last Commit") {
		t.Fatal("tools precede existing commit actions")
	}

	if err := tt.ChooseMenuItem("Copy Commit Message"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := tt.Clipboard(); got != "second commit\n\nFull message body" {
		t.Fatalf("message %q", got)
	}
	if err := tt.RightClick(cm.Subject); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Select for Compare"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.commitTools.selected != cm.Hash || a.commitTools.repo != a.git.repo {
		t.Fatal("comparison selection not saved")
	}
	parent := a.git.history[1]
	if err := tt.RightClick(parent.Subject); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Compare with Selected (" + cm.Short() + ")"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	// The comparison opens in a diff of its own, from the selected commit
	// to the one chosen, the card naming them.
	tab := a.activeTab()
	if tab == nil || tab.diff == nil || tab.diff.commit == nil {
		t.Fatal("the comparison is not shown in a diff")
	}
	v := tab.diff
	if v.commit.from != cm.Hash || v.commit.hash != parent.Hash {
		t.Fatalf("compared %s..%s, want %s..%s", v.commit.from, v.commit.hash, cm.Hash, parent.Hash)
	}
	for range 20 {
		hs.settle()
	}
	if len(v.files) != 1 || v.files[0].path != hs.path("main.go") {
		t.Fatalf("compared files %v", v.files)
	}
	if got, want := v.name(), cm.Short()+" → "+parent.Short(); got != want {
		t.Fatalf("comparison named %q, want %q", got, want)
	}
	if !hs.tt.HasText("Changes from HEAD to " + parent.Short()) {
		t.Fatalf("the comparison's card: %q", hs.tt.Texts())
	}
	// It is kept by its two commits, and comes back as it was.
	key := cm.Hash + ".." + parent.Hash
	a.rememberTabs()
	kept := false
	for _, ts := range a.window.Columns[0].Tabs {
		kept = kept || ts.Diff == "commit:"+key
	}
	if !kept {
		t.Errorf("tabs kept %+v", a.window.Columns)
	}
	a.closeActive()
	hs.settle()
	if col, _ := a.commitTab(key); col != nil {
		t.Fatal("the comparison did not close")
	}
	a.reopenClosed()
	hs.settleGit()
	hs.settle()
	if tab := a.activeTab(); tab == nil || tab.diff == nil || tab.diff.commit == nil || tab.diff.commit.key() != key {
		t.Fatalf("reopened %+v", tab)
	}
	v = a.activeTab().diff
	// Its commit's own tab is another.
	a.showCommit(parent, true)
	hs.settle()
	if a.activeTab().diff == v {
		t.Fatal("the commit's changes opened in the comparison's tab")
	}
	a.commitPatch(cm, false)
	hs.settleGit()
	if got := tt.Clipboard(); !strings.Contains(got, "+// comparison result") {
		t.Fatalf("patch %q", got)
	}
}

func TestCommitPatchExportAndRemote(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[0]
	hs.h.saveTo = hs.path("export.patch")
	a.commitPatch(cm, true)
	hs.settleGit()
	patch, err := os.ReadFile(hs.h.saveTo)
	if err != nil || !strings.Contains(string(patch), "new file mode") {
		t.Fatalf("exported patch %q: %v", patch, err)
	}
	run("remote", "add", "origin", "git@github.com:owner/repo.git")
	a.commitRemote(cm)
	hs.settleGit()
	if got := hs.tt.OpenedURLs(); !slices.Equal(got, []string{"https://github.com/owner/repo/commit/" + cm.Hash}) {
		t.Fatalf("opened URLs %q", got)
	}
}

func TestGitReadDiscardsOldRepositoryResultWithoutChangingNewTask(t *testing.T) {
	old := &gitpkg.Repo{}
	a := &app{git: gitState{repo: old}}
	release := make(chan struct{})
	called := false
	a.gitRead("Old read", func(r *gitpkg.Repo) error {
		if r != old {
			t.Error("read received wrong repository")
		}
		<-release
		return nil
	}, func() { called = true })
	a.git = gitState{repo: &gitpkg.Repo{}, pending: 1, busy: "New read"}
	close(release)
	deadline := time.Now().Add(settleTimeout)
	for {
		a.mu.Lock()
		queue := a.queue
		a.queue = nil
		a.mu.Unlock()
		if len(queue) != 0 {
			for _, fn := range queue {
				fn()
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("read never posted its result")
		}
		time.Sleep(time.Millisecond)
	}
	if called || a.git.pending != 1 || a.git.busy != "New read" {
		t.Fatalf("old read changed new task: callback %v, pending %d, busy %q", called, a.git.pending, a.git.busy)
	}
}
