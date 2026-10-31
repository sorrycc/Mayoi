package main

import (
	"errors"
	"testing"
	"time"

	gitpkg "mayoi/internal/git"
)

// Wait without drawing a frame: the old result must remain queued until
// the new repository has started its own work.
func waitGitPost(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		n := len(a.queue)
		a.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Git result was not posted")
}

func TestRepositorySwitchKeepsNewStatusReadRunning(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.drain()
	a.git.checked = a.gitNow()
	a.git.status = nil // only start the status read
	a.git.stale = true
	a.refreshGit()
	waitGitPost(t, a)
	next := &gitpkg.Repo{}
	read := time.Now().Add(-time.Minute)
	a.git = gitState{repo: next, running: true, read: read}
	a.drain()
	if !a.git.running || !a.git.read.Equal(read) || a.git.status != nil {
		t.Fatalf("old status result changed new read: running=%v read=%v status=%v", a.git.running, a.git.read, a.git.status)
	}
}

func TestRepositorySwitchKeepsNewHistoryReadRunning(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.drain()
	a.git.historyOf = ""
	a.refreshHistory()
	waitGitPost(t, a)
	a.git = gitState{repo: &gitpkg.Repo{}, loadingHistory: true, historyOf: "new history"}
	a.drain()
	if !a.git.loadingHistory || a.git.historyOf != "new history" || len(a.git.history) != 0 {
		t.Fatalf("old history result changed new read: %+v", a.git)
	}
}

func TestRepositorySwitchKeepsNewCommandPending(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			hs := newHarness(t, true)
			hs.gitRepo()
			a := hs.a
			a.drain()
			oldCalled := false
			a.gitDo("Old command…", func(*gitpkg.Repo) error {
				if fail {
					return errors.New("old repository failure")
				}
				return nil
			}, func() { oldCalled = true })
			waitGitPost(t, a)
			a.git = gitState{repo: &gitpkg.Repo{}}
			release := make(chan struct{})
			defer close(release)
			a.gitDo("New command…", func(*gitpkg.Repo) error { <-release; return nil }, nil)
			a.drain()
			if oldCalled || a.git.pending != 1 || a.git.busy != "New command…" || a.git.stale {
				t.Fatalf("old command changed new state: callback=%v pending=%d busy=%q stale=%v", oldCalled, a.git.pending, a.git.busy, a.git.stale)
			}
		})
	}
}
