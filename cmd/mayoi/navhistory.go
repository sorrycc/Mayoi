package main

import (
	"os"
	"slices"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// Go Back and Go Forward: the places that jumps to other places left, as
// Go to Definition and opening a diagnostic do, in the files of a window's
// folder, from one file to another and from column to column. Vim's ⌃O
// and ⌃I keep their own list, of the places in a file.

// navPlace is a place the caret was: an offset of a file, which the edits
// of its document move while it is open, and the column it was in.
type navPlace struct {
	path string
	at   int
	col  int // the column's id
}

// navHistory are the places jumps left, the oldest first; at is the one
// gone back to, len(places) when none is.
type navHistory struct {
	places []*navPlace
	at     int
}

// navMax is how many places the history keeps.
const navMax = 100

// navHere is the place of the caret in the editor that has the keyboard,
// nil when it shows no file.
func (a *app) navHere() *navPlace {
	d := a.activeEditor()
	if d == nil || d.path == "" || d.unread() {
		return nil
	}
	col := a.editorColumn(d)
	if col == nil {
		return nil
	}
	return &navPlace{path: d.path, at: min(d.sel[1], d.buf.Snapshot().Len()), col: col.id}
}

// navDoc is the document open at a place's file, nil for none.
func (a *app) navDoc(p *navPlace) *document {
	if i := a.docIndex(p.path); i >= 0 {
		return a.docs[i]
	}
	return nil
}

// navTrack has the edits of the document open at p's file move it, or
// stop moving it.
func (a *app) navTrack(p *navPlace, on bool) {
	doc := a.navDoc(p)
	if doc == nil {
		return
	}
	doc.navs = slices.DeleteFunc(doc.navs, func(x *navPlace) bool { return x == p })
	if on {
		doc.navs = append(doc.navs, p)
	}
}

// navSameLine reports whether two places are on one line of a file.
func (a *app) navSameLine(x, y *navPlace) bool {
	if x.path != y.path {
		return false
	}
	doc := a.navDoc(x)
	if doc == nil {
		return x.at == y.at
	}
	snap := doc.buf.Snapshot()
	return snap.LineAt(min(x.at, snap.Len())) == snap.LineAt(min(y.at, snap.Len()))
}

// navPush keeps from in the history, as a jump leaves it: the places gone
// back past go, as in a browser, and one on its line takes its place.
func (a *app) navPush(from *navPlace) {
	h := &a.nav
	h.at = min(h.at, len(h.places))
	gone := slices.Clone(h.places[h.at:])
	keep := h.places[:h.at:h.at]
	if n := len(keep); n > 0 && a.navSameLine(keep[n-1], from) {
		gone = append(gone, keep[n-1])
		keep = keep[:n-1]
	}
	keep = append(keep, from)
	if over := len(keep) - navMax; over > 0 {
		gone = append(gone, keep[:over]...)
		keep = slices.Delete(keep, 0, over)
	}
	for _, p := range gone {
		a.navTrack(p, false)
	}
	a.navTrack(from, true)
	h.places, h.at = keep, len(keep)
}

// navGo goes back through the history by n, as Go Back does, or forward,
// as Go Forward does. A place whose file is gone is passed over, and told
// of.
func (a *app) navGo(n int) {
	h := &a.nav
	for {
		h.at = min(h.at, len(h.places))
		if n < 0 && h.at == len(h.places) {
			// Kept, for Go Forward to come back to.
			if cur := a.navHere(); cur != nil {
				a.navPush(cur)
				h.at--
			}
		}
		to := h.at + n
		if n == 0 || to < 0 || to >= len(h.places) {
			return
		}
		p := h.places[to]
		if a.navOpen(p) {
			h.at = to
			return
		}
		a.tell(note{message: locales.NavGone(p.path)})
		a.navTrack(p, false)
		h.places = slices.Delete(h.places, to, to+1)
		if n > 0 {
			h.at = to - 1
		} else {
			h.at = to
		}
	}
}

// navOpen shows a place of the history: in its column while that is open
// and not locked, with the caret at it. It reports false when its file is
// gone.
func (a *app) navOpen(p *navPlace) bool {
	if !remote.Is(p.path) {
		if _, err := os.Stat(p.path); err != nil {
			return false
		}
	}
	col := a.fileColumn()
	for _, c := range a.cols {
		if c.id == p.col && !c.locked {
			col = c
		}
	}
	a.openText(func() { a.openFileIn(col, col.active+1, p.path, true) })
	d := a.activeEditor()
	if d == nil || d.path != p.path {
		return false
	}
	// A file opened again is a new document, whose edits move it now.
	a.navTrack(p, true)
	d.whenRead(func(ed *editor) {
		if ed.log != nil {
			n := ed.log.runeLen()
			ed.logSelect(min(max(int64(p.at), 0), n), min(max(int64(p.at), 0), n))
			ed.lightLanding()
			return
		}
		at := min(p.at, ed.buf.Snapshot().Len())
		ed.selectRange(at, at, true)
		ed.lightLanding()
	})
	return true
}
