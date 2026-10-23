package main

import (
	"os"
	"slices"

	"mayoi/internal/git"
	"mayoi/internal/remote"
)

// maxClosed is how many closed tabs the window remembers.
const maxClosed = 30

// closedTab is a tab closed, as ⇧⌘T opens it again: a file, an image or a
// terminal, where it was, and where an editor was in its text.
type closedTab struct {
	path    string // the file or image; "" for a terminal
	termDir string // where the terminal started
	col, at int    // the column's id and the tab's place there
	// colAt and weight are the column's place among the columns and its
	// share of their width, to make it again once it is gone.
	colAt   int
	weight  float32
	preview bool // a document showed as a rendered preview
	// diff is set for the tab of the changes, laid out as layout; full is
	// set for every unchanged line shown.
	diff, full bool
	layout     diffLayout
	// commit is the commit whose changes the tab showed, "" for those of
	// the work tree.
	commit  string
	sel     [2]int
	caret   int
	x, y    float32
	wrap    *bool
	wrapGen int
}

// rememberClosed keeps a tab about to close, to open again. An untitled
// document's tab is not kept: its text goes with it.
func (a *app) rememberClosed(t *colTab) {
	col, i := a.locate(t)
	if col == nil {
		return
	}
	c := closedTab{col: col.id, at: i, colAt: slices.Index(a.cols, col), weight: col.weight}
	switch {
	case t.term != nil:
		c.termDir = t.term.dir
	case t.diff != nil:
	case t.path() == "":
		return
	default:
		c.path = t.path()
	}
	if v := t.diff; v != nil {
		c.diff, c.layout, c.full = true, v.layout, v.full
		if v.commit != nil {
			c.commit = v.commit.key()
		}
		c.y, c.wrap, c.wrapGen = v.scroll.Y, v.wrap, v.wrapGen
	}
	if ed := t.ed; ed != nil {
		c.preview = ed.preview != nil
		c.sel, c.caret = ed.sel, ed.caret
		c.x, c.y = ed.hscroll.X, ed.vscroll.Y
		c.wrap, c.wrapGen = ed.wrap, ed.wrapGen
	}
	a.closed = append(a.closed, c)
	if len(a.closed) > maxClosed {
		a.closed = slices.Delete(a.closed, 0, len(a.closed)-maxClosed)
	}
}

// reopenClosed opens the tab closed last again, in its column and place,
// making the column again where it was when its last tab closed. Files
// gone from the disk are passed over.
func (a *app) reopenClosed() {
	for len(a.closed) > 0 {
		c := a.closed[len(a.closed)-1]
		a.closed = a.closed[:len(a.closed)-1]
		col, made := a.closedColumn(c)
		if c.diff {
			other, i := a.changesTab()
			if c.commit != "" {
				other, i = a.commitTab(c.commit)
			}
			if other != nil {
				a.dropColumn(col, made)
				a.activate(other, i, true)
				return
			}
			v := newDiffView(c.layout)
			if c.commit != "" {
				v = newCommitView(c.layout, c.commit, git.Commit{})
			}
			v.scroll.Y, v.full, v.wrap, v.wrapGen = c.y, c.full, c.wrap, c.wrapGen
			at := min(max(c.at, 0), len(col.tabs))
			a.addTab(col, at, &colTab{diff: v})
			a.activate(col, at, true)
			return
		}
		if c.path == "" {
			if a.newTerminalTabAt(col, c.at, c.termDir, true) {
				return
			}
			a.dropColumn(col, made)
			continue
		}
		if _, err := os.Stat(c.path); err != nil && !remote.Is(c.path) {
			a.dropColumn(col, made)
			continue
		}
		n := len(col.tabs)
		// A file shown already keeps the preview it has; a fresh open is
		// text first, its saved preview restored below.
		if col.showingFile(c.path) < 0 {
			a.openText(func() { a.openFileIn(col, c.at, c.path, true) })
		} else {
			a.openFileIn(col, c.at, c.path, true)
		}
		if len(col.tabs) == n {
			a.dropColumn(col, made)
			// The column showed it already, or it could not open.
			return
		}
		if ed := col.tab().ed; ed != nil {
			ed.sel, ed.caret = c.sel, c.caret
			ed.vscroll.Y, ed.hscroll.X = c.y, c.x
			ed.wrap, ed.wrapGen = c.wrap, c.wrapGen
			a.setPreview(ed, c.preview)
			a.focusTab(col.tab())
		}
		return
	}
}

// closedColumn returns the column a closed tab goes back to: its own, or
// one made again in its place when it went with its last tab, which the
// other tabs closed from it go back to too. made reports the latter.
func (a *app) closedColumn(c closedTab) (col *column, made bool) {
	if i := slices.IndexFunc(a.cols, func(k *column) bool { return k.id == c.col }); i >= 0 {
		return a.cols[i], false
	}
	if c.colAt < 0 || len(a.cols) == 1 && len(a.cols[0].tabs) == 0 {
		// The window's only column is empty: it takes the column's place.
		col = a.column()
	} else {
		col = a.newColumn()
		col.weight = c.weight
		at := min(c.colAt, len(a.cols))
		a.cols = slices.Insert(a.cols, at, col)
		if a.col >= at {
			a.col++
		}
		made = true
	}
	for i := range a.closed {
		if a.closed[i].col == c.col {
			a.closed[i].col = col.id
		}
	}
	return col, made
}

// dropColumn takes away a column made again for a tab that did not open.
func (a *app) dropColumn(col *column, made bool) {
	if made && len(col.tabs) == 0 {
		a.removeColumn(col)
	}
}
