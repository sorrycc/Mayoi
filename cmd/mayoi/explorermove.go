package main

import (
	"errors"
	"path/filepath"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// A file or a folder dragged from the explorer onto a row of it moves
// into the row's folder, or the folder of the row's file; onto the
// folder's row at the top, or below the rows, into the workspace's
// folder. It moves within its project alone, never over a file or a
// folder of its name, nor into itself, and its open documents follow it
// as they follow a rename.

// treeRootDrop makes e move a file or a folder of the explorer dropped on
// it into the workspace's folder, setting action to, and reports whether
// one that would is over it.
func (a *app) treeRootDrop(e ui.Element, action *func()) bool {
	if v, ok := ui.Drop[treeTake](e); ok {
		if s := treeSource(v); s != nil {
			*action = func() { a.dropInTree(s, a.root) }
		}
	}
	v, ok := a.treeDragOver(e)
	return ok && a.canMove(treeSource(v), a.root)
}

// canMove reports whether s, dropped into dir, would move: it is of this
// explorer, not in dir already, and dir is neither it nor in it.
func (a *app) canMove(s *treeDrag, dir string) bool {
	return s != nil && s.from == a && dir != "" && dir != s.path && dir != remote.Dir(s.path) &&
		!remote.Contains(s.path, dir) && remote.HostOf(dir) == remote.HostOf(s.path)
}

// dropInTree moves s, dropped on the explorer, into dir. Dropped where it
// is, as on its own row or among its neighbors, it stays, saying nothing.
func (a *app) dropInTree(s *treeDrag, dir string) {
	if s == nil || s.from != a || dir == "" || dir == s.path || dir == remote.Dir(s.path) {
		return
	}
	if remote.Contains(s.path, dir) {
		a.fail("%s", locales.FailMove(filepath.Base(s.path), locales.ErrMoveIntoItself()))
		return
	}
	if remote.HostOf(dir) != remote.HostOf(s.path) {
		return
	}
	a.moveInto(s.path, dir)
}

// moveInto moves the file or the folder at path into dir, keeping its
// name, unless another of that name is there. On a host it moves in the
// background, once at a time.
func (a *app) moveInto(path, dir string) {
	to := remote.Join(dir, filepath.Base(path))
	if !remote.Is(path) {
		a.moved(path, to, moveEntry(path, to))
		return
	}
	if a.moving[path] {
		return
	}
	if a.moving == nil {
		a.moving = map[string]bool{}
	}
	a.moving[path] = true
	bg.Go(func() {
		err := moveEntry(path, to)
		a.post(func() {
			delete(a.moving, path)
			a.moved(path, to, err)
		})
	})
}

// moveEntry renames path to to, unless a file or a folder is there.
func moveEntry(path, to string) error {
	if _, err := vfs.Lstat(to); err == nil {
		return errors.New(locales.ErrNameExists())
	}
	return vfs.Rename(path, to)
}

// moved has the documents of path follow it to to, shows it there,
// chosen, in its folder opened, or says why it did not move.
func (a *app) moved(path, to string, err error) {
	if err != nil {
		a.fail("%s", locales.FailMove(filepath.Base(path), err.Error()))
		return
	}
	a.renamed(path, to)
	if dir := remote.Dir(to); dir != a.root {
		a.files.Open.Add(dir)
	}
	if a.files.Open.Has(path) {
		a.files.Open.Remove(path)
		a.files.Open.Add(to)
	}
	a.selectInTree = to
}
