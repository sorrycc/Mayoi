package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
)

// explorerRowOf returns the row of the explorer showing path, -1 for none.
func (a *app) explorerRowOf(path string) int {
	if path == "" {
		return -1
	}
	for i := range a.files.Rows() {
		if a.files.Item(i) == path {
			return i
		}
	}
	return -1
}

// selectExplorerRow chooses row i of the explorer as a click or a key with
// mods does: with Shift, the rows from the anchor to it, in the order the
// tree shows them; with Cmd (Ctrl on Linux and Windows), it as well as
// those chosen, or no longer; else it alone. The row chosen, fileRow, is
// always one of several chosen, which explorerSel holds by their paths
// only while there are two or more.
func (a *app) selectExplorerRow(i int, mods ui.Modifiers) {
	if i < 0 || i >= a.files.Rows() {
		return
	}
	sel, path, cur := &a.explorerSel, a.files.Item(i), a.explorerChoice()
	anchor := -1
	if sel.Len() > 0 {
		anchor = a.explorerRowOf(a.explorerAnchor)
	}
	if anchor < 0 && cur != "" {
		anchor = a.fileRow
	}
	switch {
	case mods&ui.Shift != 0 && anchor >= 0:
		sel.Clear()
		for j := min(anchor, i); j <= max(anchor, i); j++ {
			sel.Add(a.files.Item(j))
		}
		a.explorerAnchor = a.files.Item(anchor)
	case mods&ui.Cmd != 0:
		if sel.Len() == 0 && cur != "" {
			sel.Add(cur)
		}
		if !sel.Has(path) {
			sel.Add(path)
			a.explorerAnchor = path
			break
		}
		// Taken out, the choice stays, or goes to the nearest row still
		// chosen.
		sel.Remove(path)
		from := i
		i = -1
		if sel.Has(cur) {
			i = a.fileRow
		}
		for d := 1; d < a.files.Rows() && i < 0; d++ {
			for _, j := range []int{from + d, from - d} {
				if j >= 0 && j < a.files.Rows() && sel.Has(a.files.Item(j)) {
					i = j
					break
				}
			}
		}
		a.explorerAnchor = ""
		if i >= 0 {
			a.explorerAnchor = a.files.Item(i)
		}
	default:
		sel.Clear()
		a.explorerAnchor = path
	}
	if sel.Len() < 2 {
		sel.Clear()
	}
	a.fileRow = i
}

// pruneExplorerSelection keeps the rows chosen together to those the tree
// still shows, as folders close, files go, and a compact row takes another
// last folder; fewer than two left, the row chosen is chosen alone.
func (a *app) pruneExplorerSelection(view explorerTree) {
	sel := &a.explorerSel
	if sel.Len() == 0 {
		return
	}
	if a.fileRow < 0 || a.fileRow >= a.files.Rows() {
		sel.Clear()
		a.explorerAnchor = ""
		return
	}
	rows := make(map[string]int, a.files.Rows())
	for i := range a.files.Rows() {
		rows[a.files.Item(i)] = i
	}
	shown := func(path string) string {
		if _, ok := rows[path]; ok {
			return path
		}
		// A folder of a compact row read again or split stays on the row
		// that holds it now, as the row chosen does.
		row := view.members[path]
		if row == "" && view.compact && a.tree.IsDir(path) {
			row = view.selection(path)
		}
		if _, ok := rows[row]; ok && row != "" {
			return row
		}
		return ""
	}
	var keep []string
	for path := range sel.All() {
		if row := shown(path); row != "" {
			keep = append(keep, row)
		} else if a.tree.Loading(remote.Dir(path)) {
			// Its folder is read again, as after a rename: the new
			// name shows once the read lands.
			keep = append(keep, path)
		}
	}
	sel.Clear()
	for _, path := range keep {
		sel.Add(path)
	}
	a.explorerAnchor = shown(a.explorerAnchor)
	if sel.Len() < 2 {
		sel.Clear()
		return
	}
	if !sel.Has(a.files.Item(a.fileRow)) {
		// The row chosen closed or went: the last row chosen takes its place.
		last := -1
		for path := range sel.All() {
			if i, ok := rows[path]; ok {
				last = max(last, i)
			}
		}
		if last >= 0 {
			a.fileRow = last
		}
	}
}

// explorerPicked are the files and folders an action on path acts on: all
// those chosen, in the tree's order, when path is one of several chosen,
// and else path alone.
func (a *app) explorerPicked(path string) []string {
	if !a.explorerMulti(path) {
		return []string{path}
	}
	picked := make([]string, 0, a.explorerSel.Len())
	for i := range a.files.Rows() {
		if p := a.files.Item(i); a.explorerSel.Has(p) {
			picked = append(picked, p)
		}
	}
	return picked
}

// explorerMulti reports whether path is one of several rows chosen.
func (a *app) explorerMulti(path string) bool {
	return a.explorerSel.Len() >= 2 && a.explorerSel.Has(path)
}

// clearExplorerSelection lets the rows chosen together go, the row chosen
// staying.
func (a *app) clearExplorerSelection() {
	a.explorerSel.Clear()
	a.explorerAnchor = ""
}

// explorerSelectionMoved has the rows chosen follow a file or a folder
// renamed or moved from path to to.
func (a *app) explorerSelectionMoved(path, to string) {
	if a.explorerSel.Len() == 0 {
		return
	}
	var moved []string
	for p := range a.explorerSel.All() {
		moved = append(moved, movedPath(p, path, to))
	}
	a.explorerSel.Clear()
	for _, p := range moved {
		a.explorerSel.Add(p)
	}
	a.explorerAnchor = movedPath(a.explorerAnchor, path, to)
}

// movedPath is where p is once path moved to to: there, or below it, or
// where it was.
func movedPath(p, path, to string) string {
	sep := string(filepath.Separator)
	if remote.Is(path) || remote.Is(p) {
		sep = "/"
	}
	if p == path || strings.HasPrefix(p, path+sep) {
		return to + p[len(path):]
	}
	return p
}

// explorerSelectionDraw paints a row chosen with others as the list
// paints the row chosen, across the row's arrow and indentation, which r,
// the box of the row's content depth deep, leaves out.
func (a *app) explorerSelectionDraw(pt *ui.Painter, r ui.Rect, depth int) {
	th := pt.Theme()
	arrow, gap, pad := th.Space(4), th.Space(1), th.Space(2)
	left := r.X - arrow - gap
	if depth > 0 {
		left -= float32(depth)*treeStep + gap
	}
	left -= pad
	pt.Fill(ui.Rect{X: left, Y: r.Y - gap, W: r.X + r.W + pad - left, H: r.H + 2*gap}, a.pal.active, th.Radius)
}

// explorerSelectionInput takes what chooses several rows in a frame of the
// explorer: a row Shift or Cmd clicked, with mods, which gives the tree
// the keyboard; Shift with Up, Down, Home and End, from the anchor; and a
// row dragged, which alone moves, and so is chosen alone. The list's own
// keys and a plain click choose a row alone.
func (a *app) explorerSelectionInput(out ui.Element, view explorerTree, picked string, mods ui.Modifiers, dragged string) {
	if out.Changed() {
		a.clearExplorerSelection()
	}
	if picked != "" {
		a.selectExplorerRow(a.explorerRowOf(picked), mods)
		a.filesView.Focus()
	}
	for _, k := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd} {
		if !out.Shortcut(ui.Shift, k) {
			continue
		}
		n, to := a.files.Rows(), a.fileRow
		switch k {
		case ui.KeyUp:
			to--
		case ui.KeyDown:
			to++
		case ui.KeyHome:
			to = 0
		case ui.KeyEnd:
			to = n - 1
		}
		if to = max(0, min(to, n-1)); to != a.fileRow || a.fileRow < 0 {
			a.selectExplorerRow(to, ui.Shift)
			a.files.List.ScrollIntoView(to)
		}
	}
	if dragged != "" && a.explorerSel.Len() > 0 {
		a.clearExplorerSelection()
		if i := a.explorerRowOf(dragged); i >= 0 {
			a.fileRow = i
		}
	}
	a.pruneExplorerSelection(view)
}

// joinPaths is each of paths as as says, one a line.
func joinPaths(paths []string, as func(string) string) string {
	lines := make([]string, len(paths))
	for i, p := range paths {
		lines[i] = as(p)
	}
	return strings.Join(lines, "\n")
}
