package main

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// tile is a place of the editor area's layout: a column, or a group of
// panes side by side, or one above the other when down. The root is a
// row. A group never has one pane but the root, nor a pane of its own
// direction, so that a split along a group adds to it.
type tile struct {
	id     int
	col    *column
	down   bool
	kids   []*tile
	parent *tile
	// weight is a group's share of its parent's room, among the weights of
	// the others: a column's is the column's own.
	weight float32
	// view is where a group is, whose room its splitters share out.
	view ui.Handle
}

func (p *tile) share() float32 {
	if p.col != nil {
		return p.col.weight
	}
	return p.weight
}

func (p *tile) setShare(w float32) {
	if p.col != nil {
		p.col.weight = w
	} else {
		p.weight = w
	}
}

// at is the place of kid among p's panes.
func (p *tile) at(kid *tile) int { return slices.Index(p.kids, kid) }

// leaves appends the columns of p, in the order they read, to out.
func (p *tile) leaves(out []*column) []*column {
	if p.col != nil {
		return append(out, p.col)
	}
	for _, k := range p.kids {
		out = k.leaves(out)
	}
	return out
}

// flat reports whether the layout is one row of columns, as an older
// version kept it.
func (p *tile) flat() bool {
	return !slices.ContainsFunc(p.kids, func(k *tile) bool { return k.col == nil })
}

func (a *app) newTile(col *column) *tile {
	a.nextTile++
	p := &tile{id: a.nextTile, col: col, weight: 1}
	if col != nil {
		col.node = p
	}
	return p
}

// rowOf makes a root of columns side by side.
func (a *app) rowOf(cols []*column) *tile {
	root := a.newTile(nil)
	for _, col := range cols {
		k := a.newTile(col)
		k.parent = root
		root.kids = append(root.kids, k)
	}
	return root
}

// syncCols makes a.cols the columns of the layout as they read, keeping
// the keyboard in the column that had it, else in the one that took the
// place of a column taken away.
func (a *app) syncCols(had *column) {
	i := a.col
	a.cols = a.layout.leaves(a.cols[:0:0])
	if at := slices.Index(a.cols, had); at >= 0 {
		a.col = at
		return
	}
	a.col = min(max(i, 0), len(a.cols)-1)
}

// hadKeys is the column that has the keyboard, nil for none.
func (a *app) hadKeys() *column {
	if a.col >= 0 && a.col < len(a.cols) {
		return a.cols[a.col]
	}
	return nil
}

// splitTile opens a column beside p, after it or before, along a row, or
// below or above it with down, with half of its room. A pane in a group
// of the other direction becomes a group of its own with the new one.
func (a *app) splitTile(p *tile, after, down bool) *column {
	nc := a.newColumn()
	had := a.hadKeys()
	parent := p.parent
	if parent.down != down {
		g := a.newTile(nil)
		g.down, g.weight, g.parent = down, p.share(), parent
		parent.kids[parent.at(p)] = g
		p.parent, g.kids = g, []*tile{p}
		a.tileMoved(p)
		p.setShare(1)
		parent = g
	}
	p.setShare(p.share() / 2)
	nc.weight = p.share()
	n := a.newTile(nc)
	n.parent = parent
	at := parent.at(p)
	if after {
		at++
	}
	parent.kids = slices.Insert(parent.kids, at, n)
	a.syncCols(had)
	return nc
}

// placeColumn puts col, with its weight, in the layout before ref, or
// after it, among ref's neighbours: as the last of the root for no ref.
func (a *app) placeColumn(col, ref *column, after bool) {
	had := a.hadKeys()
	n := a.newTile(col)
	parent, at := a.layout, len(a.layout.kids)
	if ref != nil && ref.node != nil {
		parent = ref.node.parent
		at = parent.at(ref.node)
		if after {
			at++
		}
	}
	n.parent = parent
	parent.kids = slices.Insert(parent.kids, at, n)
	a.syncCols(had)
}

// unplace takes a column's pane out of the layout, and tidies the group it
// was in. to, when not nil, takes its room. The keyboard stays where it
// was, or goes to the column that takes the place of the one taken.
func (a *app) unplace(col *column, to *tile) {
	n := col.node
	if n == nil || n.parent == nil {
		return
	}
	had := a.hadKeys()
	parent := n.parent
	if to != nil {
		to.setShare(to.share() + n.share())
	}
	parent.kids = slices.Delete(parent.kids, parent.at(n), parent.at(n)+1)
	n.parent, col.node = nil, nil
	a.tidy(parent)
	a.syncCols(had)
}

// tidy keeps the rules of the layout after a group lost a pane: a group
// of one gives its place to that pane, which joins its new group when of
// its direction.
func (a *app) tidy(g *tile) {
	if g.parent == nil {
		// The root is a row, whatever it holds.
		if len(g.kids) == 1 && g.kids[0].col == nil && !g.kids[0].down {
			a.splice(g, g.kids[0])
		}
		return
	}
	parent := g.parent
	switch len(g.kids) {
	case 0:
		parent.kids = slices.Delete(parent.kids, parent.at(g), parent.at(g)+1)
		a.tidy(parent)
	case 1:
		k := g.kids[0]
		k.setShare(g.share())
		k.parent = parent
		parent.kids[parent.at(g)] = k
		a.tileMoved(k)
		if k.col == nil && k.down == parent.down {
			a.splice(parent, k)
		}
	}
}

// splice puts the panes of group k in its place in parent, their room
// that of k.
func (a *app) splice(parent, k *tile) {
	var total float32
	for _, kk := range k.kids {
		total += kk.share()
	}
	at := parent.at(k)
	a.tileMoved(k)
	for _, kk := range k.kids {
		if total > 0 {
			kk.setShare(kk.share() / total * k.share())
		}
		kk.parent = parent
	}
	parent.kids = slices.Insert(slices.Delete(parent.kids, at, at+1), at, k.kids...)
}

// tileMoved has the editors of the columns of p, whose elements go to another
// place in the tree as the layout changes, come back as a tab shown again
// does, where they were: their text areas there are new ones.
func (a *app) tileMoved(p *tile) {
	for _, col := range p.leaves(nil) {
		for _, t := range col.tabs {
			if d := t.ed; d != nil && d.areaCol != nil {
				a.hideEditor(d)
				d.areaCol = nil
			}
		}
	}
}

// keptPanes is the layout as the settings keep it, nil for one row.
func (a *app) keptPanes() *settings.Pane {
	if a.layout.flat() {
		return nil
	}
	var keep func(p *tile) settings.Pane
	keep = func(p *tile) settings.Pane {
		if p.col != nil {
			return settings.Pane{Column: slices.Index(a.cols, p.col), Weight: p.col.weight}
		}
		ps := settings.Pane{Column: -1, Down: p.down, Weight: p.weight, Kids: []settings.Pane{}}
		for _, k := range p.kids {
			ps.Kids = append(ps.Kids, keep(k))
		}
		return ps
	}
	ps := keep(a.layout)
	return &ps
}

// restorePanes lays out the columns restored as the settings kept them,
// after those there already: one row when saved is nil or does not name
// each of them once.
func (a *app) restorePanes(before, restored []*column, saved *settings.Pane) {
	a.layout = a.rowOf(before)
	used := make([]bool, len(restored))
	var build func(ps settings.Pane, parent *tile) *tile
	build = func(ps settings.Pane, parent *tile) *tile {
		if len(ps.Kids) == 0 {
			if ps.Column < 0 || ps.Column >= len(restored) || used[ps.Column] {
				return nil
			}
			used[ps.Column] = true
			n := a.newTile(restored[ps.Column])
			n.parent = parent
			return n
		}
		g := a.newTile(nil)
		g.down, g.parent = ps.Down, parent
		if ps.Weight > 0 {
			g.weight = ps.Weight
		}
		for _, k := range ps.Kids {
			n := build(k, g)
			if n == nil {
				return nil
			}
			g.kids = append(g.kids, n)
		}
		return g
	}
	var tree *tile
	if saved != nil && len(saved.Kids) > 0 {
		tree = build(*saved, nil)
		if tree != nil && slices.Contains(used, false) {
			tree = nil
		}
	}
	if tree == nil {
		tree = a.rowOf(restored)
	}
	if tree.down {
		// The root is a row.
		row := a.newTile(nil)
		tree.parent, row.kids = row, []*tile{tree}
		tree = row
	}
	for _, k := range tree.kids {
		k.parent = a.layout
		a.layout.kids = append(a.layout.kids, k)
	}
	a.normalize(a.layout)
	a.syncCols(nil)
}

// normalize keeps the rules of the layout throughout p, as a layout read
// from the settings may not.
func (a *app) normalize(p *tile) {
	if p.col != nil {
		return
	}
	for i := 0; i < len(p.kids); i++ {
		k := p.kids[i]
		a.normalize(k)
		switch {
		case k.col != nil:
		case len(k.kids) == 0:
			p.kids = slices.Delete(p.kids, i, i+1)
			i--
		case len(k.kids) == 1:
			only := k.kids[0]
			only.setShare(k.share())
			only.parent = p
			p.kids[i] = only
			i--
		case k.down == p.down:
			a.splice(p, k)
			i--
		}
	}
}
