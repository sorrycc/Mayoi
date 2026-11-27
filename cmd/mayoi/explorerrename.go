package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// A file or a folder of the explorer is renamed in its row, as in VS Code
// and Finder: Enter on macOS and F2 elsewhere, the row's menu, the
// keybinding and the command show its name in a text field there, the
// name before its extension selected. Enter keeps the name typed, as
// moving the focus away does, and Escape keeps the old one.

// treeRename is the name of a row of the explorer being edited.
type treeRename struct {
	// path is the file or folder renamed, draft the name typed, and err
	// why that name cannot be kept.
	path, draft, err string
	// fresh is set until the field shows, which then takes the focus and
	// selects the name; focused once it has had the focus, which it then
	// leaves only as editing ends.
	fresh, focused bool
	// errOpen keeps the message of err shown below the field.
	errOpen bool
}

// enterRenames is whether Enter renames the row chosen, as in Finder and
// in VS Code on macOS, where ⌘↓ keeps its file open; F2 renames it on
// every system, and Enter elsewhere keeps the file open.
var enterRenames = runtime.GOOS == "darwin"

// treeRenameKeys starts editing the name of the row chosen alone, by its
// key, as the tree is built, and ends it where its row went: one scrolled
// away keeps the name typed, as the focus leaving does, and one gone keeps
// the old. submitted is whether the list had Enter, shown whether the row
// showed, action what runs after.
func (a *app) treeRenameKeys(c *ui.Context, submitted, shown bool, action *func()) {
	enter := enterRenames && submitted
	f2 := a.files.List.Shortcut(c, 0, ui.KeyF2)
	r := a.treeRename
	switch {
	case r == nil:
		if !enter && !f2 {
			return
		}
		if path := a.explorerChoice(); path != "" && !a.explorerMulti(path) {
			*action = func() { a.renameInTree(path) }
		}
	case shown || r.fresh:
		// Shown, or on its way: a folder read for it, a row scrolled to.
	case a.explorerRowOf(r.path) >= 0:
		*action = func() { a.endTreeRename(true, true) }
	default:
		*action = func() { a.endTreeRename(false, true) }
	}
}

// explorerShown reports whether the sidebar shows the explorer.
func (a *app) explorerShown() bool {
	return a.tree != nil && a.root != "" && !a.window.SidebarClosed && a.window.SidebarView == ""
}

// renameInTree edits the name of path in its row of the explorer, or asks
// for it in a dialog where the explorer cannot show it.
func (a *app) renameInTree(path string) {
	if !a.explorerShown() || !remote.Contains(a.root, path) || remote.Clean(path) == remote.Clean(a.root) {
		a.askRename(path)
		return
	}
	a.revealInTree(path)
	a.treeRename = &treeRename{path: path, draft: filepath.Base(path), fresh: true}
	a.invalidate()
}

// endTreeRename ends editing the name, keeping what was typed or not, and
// gives the explorer back the keyboard. A name kept that cannot be leaves
// the field open with why, unless the focus went elsewhere.
func (a *app) endTreeRename(keep, focusGone bool) {
	r := a.treeRename
	if r == nil {
		return
	}
	if keep {
		if msg := a.renameEntry(r.path, r.draft, !focusGone); msg != "" && !focusGone {
			r.err, r.errOpen = msg, true
			return
		}
	}
	a.treeRename = nil
	if !focusGone {
		a.focusFiles = true
	}
	a.invalidate()
}

// renameEntry renames path to name in its folder, and has the open
// documents and the explorer follow it; reselect chooses its row. It
// returns why the name cannot be kept, "" once renamed, or for the same
// name.
func (a *app) renameEntry(path, name string, reselect bool) string {
	name = strings.TrimSpace(name)
	if name == "" || hasSlash(name) {
		return locales.EnterNameNoSlashes()
	}
	to := remote.Join(remote.Dir(path), name)
	if to == path {
		return ""
	}
	if remote.Is(path) {
		go func() {
			err := vfs.Rename(path, to)
			a.post(func() {
				if err != nil {
					a.fail("%s", locales.FailRename(filepath.Base(path), err.Error()))
					return
				}
				a.renamed(path, to)
				a.renamedInTree(to, reselect)
			})
		}()
		return ""
	}
	// A name that differs only in case is the same file on a disk that
	// ignores case, which is renamed all the same.
	if fi, err := os.Lstat(to); err == nil {
		if same, err := os.Lstat(path); err != nil || !os.SameFile(fi, same) {
			return locales.NameExists()
		}
	}
	if err := os.Rename(path, to); err != nil {
		return err.Error()
	}
	a.renamed(path, to)
	a.renamedInTree(to, reselect)
	return ""
}

// renamedInTree shows the new name in the explorer at once, rather than
// once the folder's watcher tells, and chooses its row with reselect.
func (a *app) renamedInTree(to string, reselect bool) {
	if a.tree == nil || !remote.Contains(a.root, to) {
		return
	}
	a.tree.Refresh()
	a.indexRead.Invalidate()
	if reselect {
		a.selectInTree = to
	}
}

// treeRenameField shows the name of the row of path being edited, after
// the folders of a compact row that lead to it, in place of the row's
// label: it keeps the row's height and the place of its name. It returns
// what ends editing, to run once the tree is built, nil for nothing.
func (a *app) treeRenameField(c *ui.Context, r *treeRename, lead string) func() {
	p := a.pal
	if lead != "" {
		ui.Text(c, lead).FontSize(13).SingleLine().Shrink(1).TextColor(p.muted)
	}
	// Where the label was, within the field's padding and border.
	const padY, padX = 2, 4
	in := a.textKeys(ui.TextInputBase(c.Key("tree-rename"), &r.draft)).FontSize(13).Grow(1).Shrink(1).MinWidth(40).
		Padding(padY, padX).Margin(-padY-1, 0, -padY-1, -padX-1).Radius(4).Background(p.editor).
		Border(1, p.accent).TextColor(p.text).Label(locales.RenameTitle())
	switch {
	case r.fresh:
		r.fresh = false
		in.Focus()
		in.SetTextSelection(0, stemLen(r.draft))
	case in.Focused():
		r.focused = true
	}
	if in.Changed() {
		r.err, r.errOpen = "", false
	}
	if r.err != "" {
		in.Error(r.err).Border(1, p.dangerFg)
		ui.PopoverBase(c, in, &r.errOpen, func(panel ui.Element) {
			panel.Margin(2, 0, 0, 0).Padding(4, 8).Radius(4).MaxWidth(320).Background(p.editor).Border(1, p.dangerFg).
				Children(func() {
					ui.Text(c, r.err).FontSize(12).TextColor(p.dangerFg)
				})
		})
		if !r.errOpen {
			// Escape, or a press elsewhere: the old name stays.
			return func() { a.endTreeRename(false, false) }
		}
	}
	switch {
	case in.Submitted():
		return func() { a.endTreeRename(true, false) }
	case in.Shortcut(0, ui.KeyEscape):
		return func() { a.endTreeRename(false, false) }
	case r.focused && !in.Focused():
		// The focus went elsewhere: the name typed is kept, if it can be.
		return func() { a.endTreeRename(true, true) }
	}
	return nil
}

// stemLen is the length in runes of name before its extension, as Finder
// selects it to rename: all of it for a name with none, or one that
// starts with its dot.
func stemLen(name string) int {
	n := len([]rune(name))
	if ext := filepath.Ext(name); ext != "" && ext != name {
		n -= len([]rune(ext))
	}
	return n
}
