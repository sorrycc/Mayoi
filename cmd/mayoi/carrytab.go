package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/locales"
)

// A tab of a file goes to another project of the rail: dropped on its
// square, or, once the drag rests there and the project shows, where it
// is dropped in that project, as from its sidebar. The project opens the
// file anew, with the changes not yet saved, and the tab leaves the one
// it was in, unless Alt is held, which copies it.

// carriable reports whether a tab can go to another project: one of a
// file, as text or as an image.
func carriable(t *colTab) bool {
	return t != nil && (t.ed != nil || t.view != nil) && t.path() != ""
}

// dragOver is ui.DragOver of what is dragged to a column, which notes
// that the project still sees the drag, as a tab carried in from another
// one needs.
func (a *app) dragOver(e ui.Element) (dragItem, bool) {
	it, ok := ui.DragOver[dragItem](e)
	if ok {
		a.dragSeen = true
	}
	return it, ok
}

// carrier is the project of a tab dragged here from another, nil for one
// of this project, or what does not go across.
func (a *app) carrier(it dragItem) *app {
	if !carriable(it.tab) || a.owns(it) {
		return nil
	}
	return a.host.projectOf(it.tab)
}

// takeTab opens the file of t, a tab of the project from, at place at
// of col, and closes t there unless copy is set. Changes of its text not
// yet saved come along, but where the file is open here already: that
// text stays, and so does t, with them.
func (a *app) takeTab(from *app, t *colTab, col *column, at int, copy bool) {
	a.carried = nil
	if fc, _ := from.locate(t); fc == nil || !carriable(t) {
		return
	}
	path := t.path()
	var src *document
	if t.ed != nil {
		src = t.ed.document
	}
	edits := src != nil && src.dirty() && !src.locked()
	if edits && a.docIndex(path) >= 0 {
		a.openFileIn(col, at, path, true)
		name, _ := railName(from.folder())
		a.tell(note{message: locales.TabEditsStay(t.name(), name)})
		return
	}
	if edits {
		a.adoptDocument(src)
	}
	a.openFileIn(col, at, path, true)
	if col.showingFile(path) < 0 {
		// It could not be opened: it stays where it was.
		return
	}
	if !copy {
		from.dropTab(t)
		from.invalidate()
	}
}

// adoptDocument opens here a copy of src, a document of another project,
// with its changes: changed still against the same text on disk.
func (a *app) adoptDocument(src *document) {
	a.nextID++
	doc := newDocument(a.nextID, src.path, src.savedSnap.String(), highlight.New(highlight.Lexer(src.path), a.invalidate))
	doc.buf.Set(src.buf.String())
	doc.crlf, doc.diskCRLF, doc.stamp, doc.indent = src.crlf, src.diskCRLF, src.stamp, src.indent
	doc.measureWidest()
	a.docs = append(a.docs, doc)
	if a.tree != nil {
		a.tree.Watch(doc.path)
	}
}

// moveToProjectMenu offers the other projects of the rail for a tab to
// move to.
func (a *app) moveToProjectMenu(m *ui.Menu, t *colTab) {
	others := a.host.otherProjects()
	if !carriable(t) || len(others) == 0 {
		return
	}
	m.Submenu(locales.MoveToProject(), func(sub *ui.Menu) {
		for _, to := range others {
			name, _ := railName(to.folder())
			if sub.Item(name).Chosen() {
				a.post(func() { a.host.carryTab(t, to, false) })
			}
		}
	})
}
