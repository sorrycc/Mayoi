package gitmarks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/linediff"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// waitPost runs the next function the work in the background posts.
func waitPost(t *testing.T, posts chan func()) {
	t.Helper()
	select {
	case fn := <-posts:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was posted")
	}
}

func TestHeadMovedDuringBaseReadReadsAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	gitRun(t, dir, "init", "-q")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "one")

	posts := make(chan func(), 8)
	post := func(fn func()) { posts <- fn }
	snap := ui.NewTextBuffer("one\ntwo\n").Snapshot()
	var g Marks
	g.Refresh(path, snap, post)
	// A commit lands while the first base is read.
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-qam", "two")
	g.HeadMoved()
	g.Refresh(path, snap, post)
	waitPost(t, posts)
	if g.base == nil {
		t.Fatal("base read while HEAD moved was dropped")
	}
	g.Refresh(path, snap, post) // reads the base again, and compares the first
	for range 2 {
		waitPost(t, posts)
	}
	g.Refresh(path, snap, post)
	waitPost(t, posts)
	if len(g.Lines) == 0 {
		t.Fatal("no marks")
	}
	for _, k := range g.Lines {
		if k != linediff.Same {
			t.Fatalf("marks are not of the new HEAD: %v", g.Lines)
		}
	}
}
