package git

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// WorktreeInUse reports a branch checked out in another worktree.
type WorktreeInUse struct{ Branch, Path string }

func (e *WorktreeInUse) Error() string {
	return fmt.Sprintf("branch %q is checked out in %s", e.Branch, e.Path)
}

// WorktreeExists reports the folder a new worktree would be made in, which
// is taken already.
type WorktreeExists struct{ Path string }

func (e *WorktreeExists) Error() string {
	return fmt.Sprintf("%s already exists", e.Path)
}

// Worktree is a checkout of the repository: the main one, or one made
// beside it. Branch is "" when HEAD is detached, and Head the commit it is
// at, "" for a bare repository.
type Worktree struct {
	Path, Branch, Head                        string
	Detached, Bare, Locked, Prunable, Current bool
}

// Worktrees lists the checkouts of the repository, the main one first as
// Git lists them. Current is the one this Repo is in, which is found by
// the folder Git resolves, not by its name.
func (r *Repo) Worktrees() ([]Worktree, error) {
	ws, err := r.listWorktrees()
	if err != nil {
		return nil, err
	}
	top, err := r.run(nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	here := strings.TrimSuffix(string(top), "\n")
	for i := range ws {
		if ws[i].Path == here {
			ws[i].Current, ws[i].Path = true, r.Root
			continue
		}
		ws[i].Path = remote.On(r.Root, ws[i].Path)
		if !remote.Is(r.Root) {
			ws[i].Path = filepath.FromSlash(ws[i].Path)
		}
	}
	return ws, nil
}

// listWorktrees reads the worktrees with their folders as the host spells
// them, which Git takes as arguments.
func (r *Repo) listWorktrees() ([]Worktree, error) {
	out, err := r.worktreeList()
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

// worktreeList is the NUL records of git worktree list.
func (r *Repo) worktreeList() (string, error) {
	out, err := r.run(nil, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		// Git before 2.36, as Ubuntu 22.04's, has no -z.
		if out, err = r.run(nil, "worktree", "list", "--porcelain"); err != nil {
			return "", err
		}
		return nulRecords(string(out)), nil
	}
	return string(out), nil
}

// AddWorktree makes a worktree on a new branch name, from base, beside the
// main one in <main>.worktrees/<name>, with "/" in the name made "-" in its
// folder. It returns the folder in the editor's spelling. It does not
// remove anything when Git fails.
func (r *Repo) AddWorktree(name, base string) (string, error) {
	ws, err := r.listWorktrees()
	if err != nil {
		return "", err
	}
	if len(ws) == 0 {
		return "", errors.New("no worktree to add beside")
	}
	main := ws[0].Path
	path := worktreeFolder(remote.Is(r.Root), main, name)
	if _, err := vfs.Stat(remote.On(r.Root, path)); err == nil {
		return "", &WorktreeExists{Path: remote.On(r.Root, path)}
	}
	if _, err := r.run(nil, "worktree", "add", "-b", name, path, base); err != nil {
		return "", err
	}
	return remote.On(r.Root, path), nil
}

// WorktreeForBranch returns the checkout owning a local branch, in the same
// filesystem namespace as the repository. NUL records preserve unusual paths.
func (r *Repo) WorktreeForBranch(branch string) (string, error) {
	out, err := r.worktreeList()
	if err != nil {
		return "", err
	}
	return remote.On(r.Root, worktreeForBranch(out, branch)), nil
}

// worktreeFolder is the folder of a worktree made of name beside the main
// worktree at main: <main>.worktrees/<name>, a "/" in the name made "-". A
// folder of a remote host is joined as its posix paths are, whatever this
// machine's separator.
func worktreeFolder(onRemote bool, main, name string) string {
	join, dir, base := filepath.Join, filepath.Dir, filepath.Base
	if onRemote {
		join, dir, base = path.Join, path.Dir, path.Base
	}
	return join(dir(main), base(main)+".worktrees", strings.ReplaceAll(name, "/", "-"))
}

func worktreeForBranch(out, branch string) string {
	for _, w := range parseWorktrees(out) {
		if branch != "" && w.Branch == branch {
			return w.Path
		}
	}
	return ""
}

// nulRecords writes the porcelain list of worktrees, a field a line, as -z
// does, a field ended by a NUL: a path with a newline goes on over the
// lines up to its HEAD, or bare.
func nulRecords(out string) string {
	var b strings.Builder
	inPath := false
	for i, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if inPath && line != "" && !strings.HasPrefix(line, "HEAD ") && line != "bare" {
			b.WriteString("\n" + line)
			continue
		}
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(line)
		inPath = strings.HasPrefix(line, "worktree ")
	}
	b.WriteByte(0)
	return b.String()
}

// parseWorktrees reads the NUL records of worktree list, each worktree
// from its "worktree" field to the empty field that ends it.
func parseWorktrees(out string) []Worktree {
	var ws []Worktree
	var cur Worktree
	open := false
	flush := func() {
		if open {
			ws = append(ws, cur)
		}
		cur, open = Worktree{}, false
	}
	for _, field := range strings.Split(out, "\x00") {
		key, val, _ := strings.Cut(field, " ")
		switch key {
		case "":
			flush()
		case "worktree":
			flush()
			cur.Path, open = val, true
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return ws
}

// PruneWorktrees forgets the worktrees whose folders are gone, which keep
// their branches checked out until it does.
func (r *Repo) PruneWorktrees() error {
	_, err := r.run(nil, "worktree", "prune")
	return err
}
