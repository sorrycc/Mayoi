package main

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// explorerTree is the visible tree. A compact row keeps the last folder's
// real path as its identity, so every file action uses that folder.
type explorerTree struct {
	compact  bool
	roots    []string
	children func(string) []string
	rows     map[string]explorerRow
	// members maps every folder in a compact row to that row's path.
	members map[string]string
	// inspected includes collapsed folders whose entries decide whether
	// a row can be compacted. Remote polling must keep these current too.
	inspected map[string]bool
	// pending bounds automatic reads across frames, especially over SSH.
	pending map[string]bool
}

type explorerRow struct {
	first, name, parent string
	depth               int
	visible             bool
}

func (a *app) toggleCompactFolders() {
	a.settings.ExplorerCompactFolders = !a.settings.ExplorerCompactFolders
	a.settings.Changed()
}

// explorerRows projects only the visible branches, reading each single
// folder chain in the background even when its row is collapsed.
func (a *app) explorerRows() explorerTree {
	previous := a.explorer
	// Arrows and keyboard navigation change the last folder's state.
	// Carry it to the hidden ancestors, including before uncompacting or
	// a disk change splits a chain, so those folders keep the same state.
	if previous.compact {
		for path, row := range previous.rows {
			for dir := remote.Dir(path); dir != path && len(dir) >= len(row.first); {
				if a.files.Open.Has(path) {
					a.files.Open.Add(dir)
				} else if !remote.Contains(dir, a.selectInTree) || remote.Clean(a.selectInTree) == remote.Clean(dir) {
					// A reveal waiting for a file new in a hidden folder
					// keeps that folder open until the tree lists the file.
					a.files.Open.Remove(dir)
				}
				next := remote.Dir(dir)
				if next == dir {
					break
				}
				dir = next
			}
		}
	}
	v := explorerTree{
		compact: a.settings.ExplorerCompactFolders,
		rows:    make(map[string]explorerRow, len(previous.rows)),
		members: make(map[string]string, len(previous.members)),
		pending: previous.pending,
	}
	wasCompact, previousMembers := previous.compact, previous.members
	t := a.tree
	if v.compact {
		v.inspected = map[string]bool{}
		if v.pending == nil {
			v.pending = map[string]bool{}
		}
		for path := range v.pending {
			if !t.Loading(path) {
				delete(v.pending, path)
			}
		}
	}
	probe := func(path string) {
		v.inspected[path] = true
		if t.Loaded(path) || t.Loading(path) || len(v.pending) >= 8 {
			return
		}
		t.Load(path)
		if t.Loading(path) {
			v.pending[path] = true
		}
	}
	v.children = func(parent string) []string {
		if !t.IsDir(parent) {
			return nil
		}
		row := v.rows[parent]
		visible := parent == a.root || row.visible && a.files.Open.Has(parent)
		depth := row.depth + 1
		if parent == a.root {
			depth = 0
		}
		if visible {
			t.Load(parent)
		}
		kids := t.Children(parent)
		if kids == nil {
			return nil
		}
		paths := make([]string, 0, len(kids))
		for _, first := range kids {
			path, name := first, filepath.Base(first)
			if v.compact && (parent == a.root || !workspace.Skipped(filepath.Base(parent))) {
				// Bound automatic traversal, including directory symlink cycles.
				// Longer chains continue on another row when expanded.
				for count := 1; count < 64 && t.IsDir(path) && !workspace.Skipped(filepath.Base(path)); count++ {
					if visible {
						probe(path)
					}
					children := t.Children(path)
					if len(children) != 1 || !t.IsDir(children[0]) || workspace.Skipped(filepath.Base(children[0])) {
						break
					}
					path = children[0]
					// A slash on every system, as VS Code shows chains.
					name += "/" + filepath.Base(path)
				}
				// A newly compacted chain keeps its first folder's state,
				// including branches first shown after the setting changed.
				if (!wasCompact || previousMembers[first] == "") && a.files.Open.Has(first) {
					a.files.Open.Add(path)
				}
				// A chain read in several frames keeps an expansion made
				// before its final folder was known.
				if visible && wasCompact {
					if old := previousMembers[first]; old != "" && old != path && a.files.Open.Has(old) {
						a.files.Open.Add(path)
					}
				}
			}
			if visible {
				for dir := path; len(dir) >= len(first); {
					v.members[dir] = path
					next := remote.Dir(dir)
					if next == dir {
						break
					}
					dir = next
				}
			}
			v.rows[path] = explorerRow{first: first, name: name, parent: parent, depth: depth, visible: visible}
			paths = append(paths, path)
		}
		return paths
	}
	v.roots = v.children(a.root)
	return v
}

// chainLetter is what Git has of the folders of a row, from first to its
// last, path: a file deleted from a folder hidden in the row marks it too.
func (a *app) chainLetter(path, first string) byte {
	var letter byte
	for dir := path; len(dir) >= len(first); {
		for _, ctx := range a.rootRepositoryList() {
			if next := ctx.git.dirs[dir]; letterRank(next) > letterRank(letter) {
				letter = next
			}
		}
		next := remote.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return letter
}

// chainLink reports whether a row is a symbolic link, or has one among
// the folders of its chain, from first to its last, path.
func (a *app) chainLink(path, first string) bool {
	for dir := path; len(dir) >= len(first); {
		if a.tree.IsLink(dir) {
			return true
		}
		next := remote.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return false
}

// linkMark marks a symbolic link after its name.
func linkMark(c *ui.Context, p *palette) {
	ui.Icon(c, iconLink).FontSize(11).TextColor(p.faint).Shrink(0).Tooltip(locales.SymbolicLink())
}

func (a *app) explorerDirVisible(dir string) bool {
	return a.files.Open.Has(dir) || a.settings.ExplorerCompactFolders && a.explorer.inspected[dir]
}

// selection finds the row containing a path, or its nearest
// visible parent when a change on disk splits a collapsed compact row.
func (v explorerTree) selection(path string) string {
	for path != "" {
		if row := v.members[path]; row != "" {
			return row
		}
		parent := remote.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}
