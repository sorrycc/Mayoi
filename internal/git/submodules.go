package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// Submodules opens the initialized gitlinks in this work tree's index, of
// the paths .gitmodules names, without scanning unrelated folders.
// Open alone is insufficient: in an uninitialized module it finds the parent.
func (r *Repo) Submodules() ([]*Repo, error) {
	// Most repositories have no modules. In particular, never enumerate their
	// index (which may contain hundreds of thousands of files) just to check.
	if _, err := vfs.Stat(r.Abs(".gitmodules")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	// Only the paths .gitmodules names are listed from the index: listing it
	// whole costs as much as the repository is large, on every change to it.
	out, err := r.run(nil, "config", "-z", "--file", ".gitmodules", "--get-regexp", `^submodule\..*\.path$`)
	var gerr *Error
	if err != nil && errors.As(err, &gerr) && gerr.Stderr == "" && !errors.Is(err, remote.ErrOffline) {
		return nil, nil // no module has a path
	}
	if err != nil {
		return nil, err
	}
	args := []string{"ls-files", "--stage", "-z", "--"}
	for rec := range strings.SplitSeq(string(out), "\x00") {
		if _, path, ok := strings.Cut(rec, "\n"); ok && path != "" {
			args = append(args, ":(literal)"+path)
		}
	}
	if len(args) == 4 {
		return nil, nil
	}
	out, err = r.run(nil, args...)
	if err != nil {
		return nil, err
	}
	var repos []*Repo
	seen := map[string]bool{}
	for rec := range strings.SplitSeq(string(out), "\x00") {
		fields, path, ok := strings.Cut(rec, "\t")
		if !ok || !strings.HasPrefix(fields, "160000 ") {
			continue
		}
		root := r.Abs(path)
		if root == r.Root || !r.Contains(root) || seen[root] {
			continue
		}
		seen[root] = true
		sub, err := Open(root)
		if err == nil && filepath.Clean(sub.Root) == filepath.Clean(root) {
			repos = append(repos, sub)
		}
	}
	return repos, nil
}
