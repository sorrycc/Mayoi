package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// A folder of many work trees keeps the frames quick: a frame looks at
// none of their Git metadata itself, and their status is read a few at a
// time, not all at once (#149).
func TestManyRepositoriesKeepFramesQuick(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	a := hs.a
	const n = 60
	for i := range n {
		root := hs.path(fmt.Sprintf("p%03d", i))
		os.MkdirAll(root, 0o755)
		gitIn(t, root, "init", "-q", "-b", "main")
	}
	a.repositories.scan.Invalidate()
	a.showSourceControl()
	settleNested(t, hs, n)
	// Every work tree is due for its metadata check and its status read,
	// and the reads may not start.
	for range cap(gitReads) {
		gitReads <- struct{}{}
	}
	for _, ctx := range a.repositories.contexts[1:] {
		ctx.git.checked = time.Time{}
		ctx.git.read = time.Time{}
		ctx.git.status = nil
	}
	start := time.Now()
	a.refreshGit()
	took := time.Since(start)
	// The reads a status takes are done well within the wait, were they
	// let run.
	read := 0
	for deadline := time.Now().Add(2 * time.Second); read == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		step(hs.tt)
		for _, ctx := range a.repositories.contexts[1:] {
			if ctx.git.status != nil {
				read++
			}
		}
	}
	for range cap(gitReads) {
		<-gitReads
	}
	if took > 10*time.Millisecond {
		t.Errorf("one frame spent %v on Git", took)
	}
	if read != 0 {
		t.Errorf("%d status reads did not wait their turn", read)
	}
	settleNested(t, hs, n)
}
