package git

import (
	"errors"
	"mayoi/internal/remotefs"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeForBranch(t *testing.T) {
	r, git := repo(t)
	path := filepath.Join(t.TempDir(), "work tree\nquoted\"name")
	git("worktree", "add", "-b", "topic", path)
	// Git canonicalizes the temporary directory's symlink on macOS.
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.WorktreeForBranch("topic")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	got, err = r.WorktreeForBranch("missing")
	if err != nil || got != "" {
		t.Fatalf("missing: %q, %v", got, err)
	}
	var occupied *WorktreeInUse
	if switchErr := r.Switch("topic", false); !errors.As(switchErr, &occupied) || occupied.Path != want {
		t.Fatalf("switch occupied branch: %v", switchErr)
	}
	current, err := r.run(nil, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(current)) != "main" {
		t.Fatalf("original branch changed: %q, %v", current, err)
	}
}

func TestWorktreeRemoteNamespace(t *testing.T) {
	target := remotefs.Target{Host: "user@example", Port: "2222"}
	out := "worktree /repo\x00HEAD abc\x00branch refs/heads/main\x00\x00worktree /work trees/topic\x00HEAD abc\x00branch refs/heads/topic\x00locked reason\x00\x00"
	got := worktreeForBranch(target.Virtual("/repo"), out, "topic")
	if want := target.Virtual("/work trees/topic"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := worktreeForBranch("/repo", "worktree /detached\x00HEAD abc\x00detached\x00\x00", "topic"); got != "" {
		t.Fatalf("detached matched: %q", got)
	}
}
