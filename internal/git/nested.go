package git

import (
	"path/filepath"
	"slices"
	"strings"

	"mayoi/internal/vfs"
)

// NestedDepth is how many folders deep Nested looks under a folder that is
// not a work tree, as workspace/project or workspace/group/project.
const NestedDepth = 2

// Nested opens the work trees in the folders under dir, depth deep: those
// with a .git of their own, a folder or a file, not looked into further,
// sorted by root. Hidden folders and node_modules are passed over. Each
// level is one round trip on a host.
func Nested(dir string, depth int) ([]*Repo, error) {
	var repos []*Repo
	level := []string{dir}
	listed, _, err := vfs.ReadDirs(level)
	if err != nil {
		return nil, err
	}
	for range depth {
		var next []string
		for _, parent := range level {
			for _, e := range listed[parent] {
				if !e.Dir || strings.HasPrefix(e.Name, ".") || e.Name == "node_modules" {
					continue
				}
				next = append(next, filepath.Join(parent, e.Name))
			}
		}
		if len(next) == 0 {
			break
		}
		// Whether each has a .git is read for all of them at once, as
		// their entries, which the next level needs anyway.
		children, _, err := vfs.ReadDirs(next)
		if err != nil {
			return nil, err
		}
		level, listed = nil, children
		for _, sub := range next {
			if !slices.ContainsFunc(children[sub], func(e vfs.Entry) bool { return e.Name == ".git" }) {
				level = append(level, sub)
				continue
			}
			// A .git that git does not take, a broken one, is no work tree.
			if r, err := Open(sub); err == nil && filepath.Clean(r.Root) == filepath.Clean(sub) {
				repos = append(repos, r)
			}
		}
	}
	slices.SortFunc(repos, func(a, b *Repo) int { return strings.Compare(a.Root, b.Root) })
	return repos, nil
}
