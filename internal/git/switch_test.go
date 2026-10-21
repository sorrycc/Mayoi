package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Use the real Git for the repository, but reproduce an older host's
// missing switch command at the same process boundary used over SSH.
func withoutGitSwitch(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Git wrapper needs a POSIX shell")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = switch ]; then\n" +
		"  echo \"git: 'switch' is not a git command. See 'git --help'.\" >&2\n" +
		"  exit 1\nfi\n" +
		"exec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSwitchWithoutGitSwitch(t *testing.T) {
	for _, create := range []bool{false, true} {
		name := "existing"
		if create {
			name = "new"
		}
		t.Run(name, func(t *testing.T) {
			r, git := repo(t)
			if !create {
				git("branch", "topic")
			}
			withoutGitSwitch(t)
			if err := r.Switch("topic", create); err != nil {
				t.Fatalf("switch branch on an older Git: %v", err)
			}
			if b := branchNamed(t, r, "topic"); !b.Current {
				t.Fatalf("branch was not selected: %+v", b)
			}
		})
	}
}

func TestCheckoutTrackingWithoutGitSwitch(t *testing.T) {
	r, git := repo(t)
	git("update-ref", "refs/remotes/origin/topic", "HEAD")
	git("config", "remote.origin.url", "unused")
	git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	withoutGitSwitch(t)
	if err := r.CheckoutTracking("origin/topic", "tracked"); err != nil {
		t.Fatalf("track branch on an older Git: %v", err)
	}
	if b := branchNamed(t, r, "tracked"); !b.Current || b.Upstream != "origin/topic" {
		t.Fatalf("tracking branch: %+v", b)
	}
	if err := r.CheckoutTracking("origin/topic", "main"); err == nil {
		t.Fatal("overwrote an existing branch")
	}
}

func TestSwitchGuardsWithoutGitSwitch(t *testing.T) {
	t.Run("file is not a branch", func(t *testing.T) {
		r, _ := repo(t)
		withoutGitSwitch(t)
		write(t, r.Root, "a.txt", "unsaved work\n")
		if err := r.Switch("a.txt", false); err == nil {
			t.Fatal("accepted a file as a branch")
		}
		body, err := os.ReadFile(r.Abs("a.txt"))
		if err != nil || string(body) != "unsaved work\n" {
			t.Fatalf("file changed: %q, %v", body, err)
		}
	})
	t.Run("conflicting changes", func(t *testing.T) {
		r, git := repo(t)
		git("checkout", "-qb", "topic")
		write(t, r.Root, "a.txt", "topic\n")
		git("commit", "-qam", "topic")
		git("checkout", "-q", "main")
		write(t, r.Root, "a.txt", "local work\n")
		withoutGitSwitch(t)
		if err := r.Switch("topic", false); err == nil {
			t.Fatal("overwrote conflicting local changes")
		}
		body, err := os.ReadFile(r.Abs("a.txt"))
		if err != nil || string(body) != "local work\n" {
			t.Fatalf("local changes lost: %q, %v", body, err)
		}
		if b := branchNamed(t, r, "main"); !b.Current {
			t.Fatalf("branch changed: %+v", b)
		}
	})
	t.Run("occupied worktree", func(t *testing.T) {
		r, git := repo(t)
		git("worktree", "add", "-b", "topic", filepath.Join(t.TempDir(), "other"))
		withoutGitSwitch(t)
		var occupied *WorktreeInUse
		if err := r.Switch("topic", false); !errors.As(err, &occupied) {
			t.Fatalf("occupied worktree not reported: %v", err)
		}
	})
}

func TestSwitchRemoteWithoutGitSwitch(t *testing.T) {
	r, git := repo(t)
	git("update-ref", "refs/remotes/origin/topic", "HEAD")
	git("config", "remote.origin.url", "unused")
	git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	withoutGitSwitch(t)
	if err := r.Switch("topic", false); err != nil {
		t.Fatal(err)
	}
	if b := branchNamed(t, r, "topic"); !b.Current || b.Upstream != "origin/topic" {
		t.Fatalf("remote branch not selected: %+v", b)
	}
}
