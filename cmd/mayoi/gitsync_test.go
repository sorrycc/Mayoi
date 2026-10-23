package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "mayoi/internal/git"
	"mayoi/locales"
)

func TestGitSyncStatusBar(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	run := hs.gitRepo()
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	run("remote", "add", "origin", remote)
	run("push", "-qu", "origin", "main")
	run("branch", "-m", "feature_T12436745")
	for i := range 8 {
		run("commit", "-q", "--allow-empty", "-m", fmt.Sprintf("local %d", i))
	}
	hs.settleGit()
	a, tt := hs.a, hs.tt
	ctx := a.repository()
	if ctx.branch() != "feature_T12436745" || ctx.gitSyncLabel() != "0↓ 8↑" || !tt.HasText("0↓ 8↑") {
		t.Fatalf("branch/counts: %q, %q; UI: %q", ctx.branch(), ctx.gitSyncLabel(), tt.Texts())
	}
	for _, dark := range []bool{true, false} {
		tt.SetDark(dark)
		hs.settle()
		b, ok := tt.Find(locales.GitSync())
		if !ok || b.Y < 730 || b.W <= 0 {
			t.Fatalf("missing bottom sync button: %v %v", b, ok)
		}
		name := map[bool]string{true: "git-sync-dark", false: "git-sync-light"}[dark]
		hs.shotRect(name, image.Rect(0, 716, 1200, 760), 1)
	}
	a.openFile(hs.path("main.go"), false)
	// Force the counts to give way even with a platform's narrower font.
	run("branch", "-m", strings.Repeat("W", 64))
	tt.SetSize(300, 300)
	hs.settleGit()
	if b, ok := tt.Find(locales.GitSync()); !ok || b.X < 0 || b.X+b.W > 300 || b.Y+b.H > 300 {
		t.Fatalf("narrow sync button is clipped: %v %v", b, ok)
	}
	if tt.HasText("0↓ 8↑") {
		t.Fatal("sync counts did not yield space in the narrow status bar")
	}
	if b, ok := tt.Find(locales.StatusPos(1, 1)); !ok || b.X < 0 || b.X+b.W > 300 {
		t.Fatalf("narrow caret position is clipped: %v %v", b, ok)
	}
	hs.shot("git-sync-narrow")
	run("branch", "-m", "feature_T12436745")
	tt.SetSize(1200, 760)
	hs.settleGit()
	// Holding an existing Git operation makes repeated clicks deterministic.
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	ctx.gitDo(locales.BusySyncing(), func(*gitpkg.Repo) error { <-release; return nil }, nil)
	hs.held = 1
	hs.settle()
	if !tt.HasText(locales.BusySyncing()) {
		t.Fatalf("busy feedback missing: %q", tt.Texts())
	}
	for range 2 {
		if err := tt.Click(locales.GitSync()); err != nil {
			t.Fatal(err)
		}
	}
	if a.git.pending != 1 || a.git.busy != locales.BusySyncing() {
		t.Fatalf("busy button started another command: pending=%d busy=%q", a.git.pending, a.git.busy)
	}
	close(release)
	released, hs.held = true, 0
	hs.settleGit()
	if err := tt.Click(locales.GitSync()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if a.git.status.Ahead != 0 || a.git.status.Behind != 0 || strings.TrimSpace(gitIn(t, remote, "rev-parse", "main")) != strings.TrimSpace(run("rev-parse", "HEAD")) {
		t.Fatalf("sync did not push local commits: %+v; errors: %q", a.git.status, a.errors)
	}
	if !tt.HasText(locales.SyncedWith("origin/main")) {
		t.Fatalf("sync result not shown: %q", tt.Texts())
	}
	// Even 0/0 is actionable: the remote may have commits not fetched yet.
	peer := filepath.Join(t.TempDir(), "peer")
	gitIn(t, filepath.Dir(peer), "clone", "-q", remote, peer)
	gitIn(t, peer, "commit", "-q", "--allow-empty", "-m", "remote change")
	gitIn(t, peer, "push", "-q")
	if ctx.gitSyncLabel() != "" {
		t.Fatalf("zero counts should be omitted: %q", ctx.gitSyncLabel())
	}
	if err := tt.Click(locales.GitSync()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if strings.TrimSpace(run("rev-parse", "HEAD")) != strings.TrimSpace(gitIn(t, peer, "rev-parse", "HEAD")) {
		t.Fatal("clicking sync at 0/0 did not pull new remote commits")
	}
}

func TestGitSyncPublishAndVisibility(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	assertHidden := func() {
		t.Helper()
		for _, label := range []string{locales.GitSync(), locales.ScmPublishBranch()} {
			if _, ok := hs.tt.Find(label); ok {
				t.Fatalf("unexpected sync action %q", label)
			}
		}
	}
	assertHidden()
	gitIn(t, hs.root, "init", "-q", "-b", "main")
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	gitIn(t, hs.root, "remote", "add", "origin", remote)
	hs.a.openFolder(hs.root)
	hs.settleGit()
	assertHidden() // a remote alone cannot publish an unborn branch
	gitIn(t, hs.root, "add", ".")
	gitIn(t, hs.root, "commit", "-q", "-m", "first")
	hs.settleGit()
	if err := hs.tt.Click(locales.ScmPublishBranch()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if hs.a.git.status.Upstream != "origin/main" || strings.TrimSpace(gitIn(t, remote, "rev-parse", "main")) != strings.TrimSpace(gitIn(t, hs.root, "rev-parse", "HEAD")) {
		t.Fatalf("publish did not establish upstream: %+v", hs.a.git.status)
	}
	gitIn(t, hs.root, "checkout", "-q", "--detach")
	hs.settleGit()
	assertHidden()
	gitIn(t, hs.root, "switch", "-q", "main")
	gitIn(t, hs.root, "remote", "remove", "origin")
	hs.settleGit()
	assertHidden()
}

func TestGitSyncFailureReleasesBusyState(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	run := hs.gitRepo()
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	run("remote", "add", "origin", remote)
	run("push", "-qu", "origin", "main")
	run("commit", "-q", "--allow-empty", "-m", "pending push")
	run("remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	hs.settleGit()
	if err := hs.tt.Click(locales.GitSync()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if hs.a.fails == 0 || hs.a.git.pending != 0 || hs.a.git.busy != "" || hs.a.git.status.Ahead != 1 {
		t.Fatalf("failure not reported or state not released: fails=%d pending=%d busy=%q status=%+v", hs.a.fails, hs.a.git.pending, hs.a.git.busy, hs.a.git.status)
	}
	run("remote", "set-url", "origin", remote)
	if err := hs.tt.Click(locales.GitSync()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if hs.a.git.status.Ahead != 0 {
		t.Fatalf("sync could not retry after failure: %+v", hs.a.git.status)
	}
}

func TestGitSyncKeepsSelectedRepository(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	root := sub.git.repo.Root
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	gitIn(t, root, "switch", "-q", "-c", "sync-topic")
	gitIn(t, root, "remote", "set-url", "origin", remote)
	gitIn(t, root, "push", "-qu", "origin", "sync-topic")
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "module sync")
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	hs.a.selectRepository(root)
	hs.settle()
	before := strings.TrimSpace(parent("rev-parse", "HEAD"))
	if err := hs.tt.Click(locales.GitSync()); err != nil {
		t.Fatal(err)
	}
	hs.a.selectRepository(hs.a.git.repo.Root)
	settleRepositories(t, hs, 2)
	if strings.TrimSpace(gitIn(t, remote, "rev-parse", "sync-topic")) != strings.TrimSpace(gitIn(t, root, "rev-parse", "HEAD")) {
		t.Fatal("repository switch redirected the pending sync")
	}
	if strings.TrimSpace(parent("rev-parse", "HEAD")) != before || hs.a.git.pending != 0 || sub.git.pending != 0 || sub.git.busy != "" {
		t.Fatal("module sync changed parent or leaked pending state")
	}
}
