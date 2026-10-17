package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"mayoi/internal/vfs"
)

// Submodules opens initialized gitlinks in this work tree's index. Reading
// the index also finds newly added modules and does not scan unrelated folders.
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
	out, err := r.run(nil, "ls-files", "--stage", "-z")
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
