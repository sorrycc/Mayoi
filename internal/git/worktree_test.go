package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWorktreeForBranch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows takes no newline or quote in a name")
	}
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

func TestPruneMissingWorktree(t *testing.T) {
	r, git := repo(t)
	path := filepath.Join(t.TempDir(), "gone")
	git("worktree", "add", "-b", "topic", path)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	var occupied *WorktreeInUse
	if err := r.Switch("topic", false); !errors.As(err, &occupied) {
		t.Fatalf("switch to the branch of a missing worktree: %v", err)
	}
	if err := r.PruneWorktrees(); err != nil {
		t.Fatal(err)
	}
	if err := r.Switch("topic", false); err != nil {
		t.Fatalf("switch after pruning: %v", err)
	}
}

// Git before 2.36 lists worktrees a field a line, with no -z, and a path
// with a newline in it over two lines.
func TestNulRecords(t *testing.T) {
	out := "worktree /r\nHEAD 8d66\nbranch refs/heads/main\n\n" +
		"worktree /w/plain\nHEAD 8d66\nbranch refs/heads/plain\n\n" +
		"worktree /w/work tree\nquoted\"name\nHEAD 8d66\nbranch refs/heads/topic\n\n" +
		"worktree /bare\nbare\n\n"
	want := "worktree /r\x00HEAD 8d66\x00branch refs/heads/main\x00\x00" +
		"worktree /w/plain\x00HEAD 8d66\x00branch refs/heads/plain\x00\x00" +
		"worktree /w/work tree\nquoted\"name\x00HEAD 8d66\x00branch refs/heads/topic\x00\x00" +
		"worktree /bare\x00bare\x00\x00"
	got := nulRecords(out)
	if got != want {
		t.Fatalf("nulRecords:\n%q\nwant\n%q", got, want)
	}
	if p := worktreeForBranch(got, "topic"); p != "/w/work tree\nquoted\"name" {
		t.Errorf("topic is in %q", p)
	}
	if p := worktreeForBranch(got, "plain"); p != "/w/plain" {
		t.Errorf("plain is in %q", p)
	}
}

// A worktree record is its fields up to the empty field that ends it, and
// a bare or detached one has no branch.
func TestParseWorktrees(t *testing.T) {
	out := "worktree /r\x00HEAD 8d66\x00branch refs/heads/main\x00\x00" +
		"worktree /w/det\x00HEAD 8d66\x00detached\x00\x00" +
		"worktree /bare\x00bare\x00\x00" +
		"worktree /w/lock\x00HEAD 8d66\x00branch refs/heads/topic\x00locked why\x00\x00" +
		"worktree /w/gone\x00HEAD 8d66\x00branch refs/heads/gone\x00prunable moved\x00\x00"
	got := parseWorktrees(out)
	want := []Worktree{
		{Path: "/r", Head: "8d66", Branch: "main"},
		{Path: "/w/det", Head: "8d66", Detached: true},
		{Path: "/bare", Bare: true},
		{Path: "/w/lock", Head: "8d66", Branch: "topic", Locked: true},
		{Path: "/w/gone", Head: "8d66", Branch: "gone", Prunable: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d worktrees, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("worktree %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWorktreesCurrent(t *testing.T) {
	r, git := repo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	git("worktree", "add", "-b", "topic", linked)
	detached := filepath.Join(t.TempDir(), "detached")
	git("worktree", "add", "--detach", detached)
	// A linked folder of this machine is named in its own spelling, which is
	// not Git's slashes on Windows.
	local, err := r.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range local {
		if w.Branch == "topic" && w.Path != want {
			t.Fatalf("linked worktree named %q, want %q", w.Path, want)
		}
	}
	// A symlink to the main work tree: Git names the folder it resolves,
	// and the current one must still be marked in the spelling of the link.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(r.Root, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	lr, err := Open(link)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := lr.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 {
		t.Fatalf("got %d worktrees: %+v", len(ws), ws)
	}
	if ws[0].Path != link || !ws[0].Current || ws[0].Branch != "main" || ws[1].Current || ws[2].Current {
		t.Fatalf("current is not the main work tree in its spelling: %+v", ws)
	}
	if !ws[2].Detached || ws[2].Branch != "" || ws[2].Head == "" {
		t.Fatalf("detached worktree: %+v", ws[2])
	}
	ll, err := Open(linked)
	if err != nil {
		t.Fatal(err)
	}
	ws, err = ll.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	if !ws[1].Current || ws[1].Branch != "topic" || ws[1].Path != linked || ws[0].Current {
		t.Fatalf("linked work tree is not current: %+v", ws)
	}
}

func TestAddWorktree(t *testing.T) {
	r, _ := repo(t)
	head := r.Head()
	write(t, r.Root, "a.txt", "changed\n")
	path, err := r.AddWorktree("feature/x", head)
	if err != nil {
		t.Fatal(err)
	}
	// Beside the main work tree, its folder named with dashes for slashes.
	parent, err := filepath.EvalSymlinks(filepath.Dir(r.Root))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(parent, filepath.Base(r.Root)+".worktrees", "feature-x")
	if path != want {
		t.Fatalf("worktree in %q, want %q", path, want)
	}
	if got := r.Head(); got != head {
		t.Fatalf("HEAD moved to %s", got)
	}
	if b, err := os.ReadFile(filepath.Join(r.Root, "a.txt")); err != nil || string(b) != "changed\n" {
		t.Fatalf("changed file of the main work tree: %q, %v", b, err)
	}
	if b, _ := r.Branches(); !slices.Contains(b, "feature/x") {
		t.Fatalf("branches %v lack feature/x", b)
	}
	// From a linked work tree the folder is beside the same main one.
	lr, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lr.AddWorktree("second", head)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(second) != filepath.Dir(path) {
		t.Fatalf("second worktree in %q, not beside %q", second, path)
	}
	// A folder taken fails by name, and is kept.
	var exists *WorktreeExists
	if _, err := r.AddWorktree("feature/x", head); !errors.As(err, &exists) || exists.Path != want {
		t.Fatalf("taken folder: %v", err)
	}
	// The folder is the same for a name that only spells it another way.
	if _, err := r.AddWorktree("feature-x", head); !errors.As(err, &exists) || exists.Path != want {
		t.Fatalf("taken folder of another spelling: %v", err)
	}
	// Git checks the name itself, and makes nothing for a name it refuses.
	bad := filepath.Join(parent, filepath.Base(r.Root)+".worktrees", "bad..name")
	if _, err := r.AddWorktree("bad..name", head); err == nil {
		t.Fatal("invalid branch name was accepted")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("folder made for an invalid name: %v", err)
	}
}

func TestAddWorktreeWithoutCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddWorktree("topic", "HEAD"); err == nil {
		t.Fatal("made a worktree without a commit")
	}
}

// A worktree's folder is beside the main one, named by the branch with its
// slashes made dashes; a folder on a remote host keeps its posix separators.
func TestWorktreeFolder(t *testing.T) {
	for _, c := range []struct {
		name, main string
		remote     bool
		want       string
	}{
		{"feature/x", filepath.FromSlash("/w/repo"), false, filepath.FromSlash("/w/repo.worktrees/feature-x")},
		{"topic", filepath.FromSlash("/w/main/repo"), false, filepath.FromSlash("/w/main/repo.worktrees/topic")},
		{"a/b/c", "/srv/repo", true, "/srv/repo.worktrees/a-b-c"},
		{"topic", "/srv/repo", true, "/srv/repo.worktrees/topic"},
	} {
		if got := worktreeFolder(c.remote, c.main, c.name); got != c.want {
			t.Errorf("worktreeFolder(%v, %q, %q) = %q, want %q", c.remote, c.main, c.name, got, c.want)
		}
	}
}
