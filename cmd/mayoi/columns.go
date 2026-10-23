package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// column is a column of the editor area: its tabs, and the one it shows.
// The columns are side by side, and a tab moves from one to another.
type column struct {
	id     int
	tabs   []*colTab
	active int // the tab shown, -1 for none
	// weight is the column's share of the width of the editor area, among
	// the weights of the others.
	weight float32
	locked bool // automatic file opens avoid it; it stays when empty
	// revealed is the tab the strip last scrolled into view.
	revealed int
	// body is where the tab shown is, which a tab dragged is dropped on.
	body ui.Handle
	// filesOver is set while files dragged from another app are over the
	// column, as the frame before found.
	filesOver bool
}

// colTab is a tab of a column: an editor of a document, a viewer of an
// image, the changes of a file, the three-way merge of a conflicted one,
// or a terminal.
type colTab struct {
	id    int
	ed    *editor
	view  *viewer
	diff  *diffView
	merge *mergeView
	term  *session
	// transient marks the preview tab of its column, as a file is chosen
	// in the Explorer or in search's results, or a commit in source
	// control: the next one chosen replaces it, until it is edited or
	// kept.
	transient bool
}

func (t *colTab) name() string {
	switch {
	case t.term != nil:
		return t.term.title
	case t.view != nil:
		return t.view.name()
	case t.diff != nil:
		return t.diff.name()
	case t.merge != nil:
		return t.merge.name()
	}
	return t.ed.name()
}

// path is the file the tab shows, or the changes of, "" for a terminal
// or an untitled document.
func (t *colTab) path() string {
	switch {
	case t.view != nil:
		return t.view.path
	case t.ed != nil:
		return t.ed.path
	case t.merge != nil:
		return t.merge.path
	}
	return ""
}

// tab returns the tab the column shows, nil when it has none.
func (col *column) tab() *colTab {
	if col.active < 0 || col.active >= len(col.tabs) {
		return nil
	}
	return col.tabs[col.active]
}

// showing returns the place of the tab of the column that shows doc, -1
// when none does.
func (col *column) showing(doc *document) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.ed != nil && t.ed.document == doc })
}

// showingImage returns the place of the tab of the column that shows the
// image file path, -1 when none does.
func (col *column) showingImage(path string) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.view != nil && t.view.path == path })
}

// showingMerge returns the place of the tab of the column that shows the
// three-way merge of a file, -1 when none does.
func (col *column) showingMerge(path string) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.merge != nil && t.merge.path == path })
}

// take takes the tab at place i out of the column, which shows the one
// after it, or the one before the last.
func (col *column) take(i int) {
	col.tabs = slices.Delete(col.tabs, i, i+1)
	switch {
	case len(col.tabs) == 0:
		col.active = -1
	case col.active > i || col.active == len(col.tabs):
		col.active--
	}
}

func (a *app) newColumn() *column {
	a.nextCol++
	return &column{id: a.nextCol, active: -1, weight: 1}
}

// column returns the column that has the keyboard, or had it last, and
// remembers an unlocked one as the target for subsequent file opens.
func (a *app) column() *column {
	a.col = min(max(a.col, 0), len(a.cols)-1)
	col := a.cols[a.col]
	if !col.locked {
		a.fileCol = col
	}
	return col
}

// activeTab returns the tab shown in the column that has the keyboard.
func (a *app) activeTab() *colTab { return a.column().tab() }

// activeEditor returns the editor shown in the column that has the
// keyboard, nil when it shows a terminal, an image or nothing. It may
// show a rendered preview rather than its text.
func (a *app) activeEditor() *editor {
	if t := a.activeTab(); t != nil {
		return t.ed
	}
	return nil
}

// activeViewer returns the viewer shown in the column that has the
// keyboard: an image or a document's preview. Nil when it shows none.
func (a *app) activeViewer() *viewer {
	switch t := a.activeTab(); {
	case t == nil:
	case t.view != nil:
		return t.view
	case t.ed != nil:
		return t.ed.preview
	}
	return nil
}

// locate returns the column of a tab and its place there, nil for a tab
// that was closed.
func (a *app) locate(t *colTab) (*column, int) {
	for _, col := range a.cols {
		if i := slices.Index(col.tabs, t); i >= 0 {
			return col, i
		}
	}
	return nil, -1
}

// editorTabs returns the tabs of every column that show a file, a
// document or an image.
func (a *app) editorTabs() []*colTab {
	var out []*colTab
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.term == nil {
				out = append(out, t)
			}
		}
	}
	return out
}

// addTab puts a new tab at place at of a column.
func (a *app) addTab(col *column, at int, t *colTab) {
	a.nextTab++
	t.id = a.nextTab
	at = min(max(at, 0), len(col.tabs))
	col.tabs = slices.Insert(col.tabs, at, t)
	if col.active >= at {
		col.active++
	}
}

func (a *app) docIndex(path string) int {
	if path == "" {
		return -1
	}
	return slices.IndexFunc(a.docs, func(d *document) bool { return d.path == path })
}

// openFile shows a file in a tab of the file column,
// opening one unless the column has one. A file open in another column
// shows in both, which share its text.
func (a *app) openFile(path string, focus bool) {
	col := a.fileColumn()
	a.openFileIn(col, col.active+1, path, focus)
}

// openText has open run with openingText set, so a file it opens shows
// its text, whatever markdownPreview says. The flag goes back to what it
// was when open returns, a nested call included.
func (a *app) openText(open func()) {
	was := a.openingText
	a.openingText = true
	defer func() { a.openingText = was }()
	open()
}

// openFileIn shows a file in a tab of a column, opening one at place at
// unless the column has one.
func (a *app) openFileIn(col *column, at int, path string, focus bool) {
	if isImage(path) {
		a.openImage(col, at, path, focus)
		return
	}
	var doc *document
	if i := a.docIndex(path); i >= 0 {
		doc = a.docs[i]
		if at := col.showing(doc); at >= 0 {
			a.keepTab(col.tabs[at])
			a.activate(col, at, focus)
			if a.openingText {
				if ed := col.tabs[at].ed; ed != nil && ed.preview != nil {
					a.setPreview(ed, false)
				}
			}
			return
		}
	} else if remote.Is(path) {
		// Read in the background, the tab showing that it is.
		a.nextID++
		doc = newDocument(a.nextID, path, "", highlight.New(highlight.Lexer(path), a.invalidate))
		a.docs = append(a.docs, doc)
		a.loadRemote(doc)
		if a.tree != nil {
			a.tree.Watch(path)
		}
	} else {
		a.nextID++
		d, err := readDocument(a.nextID, path, a.invalidate)
		if errors.Is(err, errBinary) {
			a.fail("%s", locales.FailBinary(filepath.Base(path)))
			return
		}
		if err != nil {
			a.fail("%s", locales.FailOpen(filepath.Base(path), err.Error()))
			return
		}
		doc = d
		a.docs = append(a.docs, doc)
		if a.tree != nil {
			a.tree.Watch(path)
		}
	}
	at = min(max(at, 0), len(col.tabs))
	t := &colTab{ed: newEditor(doc), transient: a.opening}
	a.addTab(col, at, t)
	a.activate(col, at, focus)
	if a.settings.MarkdownPreview && !a.openingText && isMarkdown(t.ed.document) {
		a.setPreview(t.ed, true)
	}
}

// showingFile returns the place of the tab of the column that shows the
// file path, as text or as an image, -1 when none does.
func (col *column) showingFile(path string) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool {
		return t.term == nil && t.diff == nil && t.merge == nil && t.path() == path
	})
}

// previewFile shows a file in the file column's preview tab, without
// giving it the keyboard: in the tab that shows it
// already, else in place of the preview tab, else in a new one.
func (a *app) previewFile(path string) {
	col := a.fileColumn()
	if i := col.showingFile(path); i >= 0 {
		a.activate(col, i, false)
		return
	}
	a.previewIn(col, func(at int) {
		a.opening = true
		a.openFileIn(col, at, path, false)
		a.opening = false
	})
}

// previewIn has open put a preview tab in a column at the place it is
// given: that of the column's preview tab, which then goes, else after
// the tab shown.
func (a *app) previewIn(col *column, open func(at int)) {
	old := slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.transient })
	if old >= 0 && col.tabs[old].ed != nil && col.tabs[old].ed.dirty() {
		// Edited since the frame began: it stays.
		a.keepTab(col.tabs[old])
		old = -1
	}
	at := col.active + 1
	if old >= 0 {
		at = old
	}
	n := len(col.tabs)
	open(at)
	if len(col.tabs) == n {
		return // it could not be read
	}
	if old >= 0 {
		// The new tab went in before it.
		a.dropTab(col.tabs[old+1])
	}
	a.rememberTabs()
}

// keepTab makes a preview tab a tab of its own, which the next file
// previewed leaves open.
func (a *app) keepTab(t *colTab) {
	if !t.transient {
		return
	}
	t.transient = false
	a.addRecent(t.path())
	a.rememberTabs()
}

// keepEdited keeps the preview tabs whose documents were edited. It runs
// once a frame.
func (a *app) keepEdited() {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.transient && t.ed != nil && t.ed.dirty() {
				a.keepTab(t)
			}
		}
	}
}

// openImage shows an image file in a tab of a column, opening one at
// place at unless the column has one.
func (a *app) openImage(col *column, at int, path string, focus bool) {
	if at := col.showingImage(path); at >= 0 {
		a.keepTab(col.tabs[at])
		a.activate(col, at, focus)
		return
	}
	// A remote image says itself why it could not be read.
	if _, err := os.Stat(path); err != nil && !remote.Is(path) {
		a.fail("%s", locales.FailOpen(filepath.Base(path), err.Error()))
		return
	}
	v := newImageViewer(path)
	a.loadImage(v)
	if a.tree != nil {
		a.tree.Watch(path)
	}
	at = min(max(at, 0), len(col.tabs))
	a.addTab(col, at, &colTab{view: v, transient: a.opening})
	a.activate(col, at, focus)
}

// newFile opens an untitled document.
func (a *app) newFile() {
	a.untitled++
	col := a.fileColumn()
	a.addUntitled(col, a.untitled, "")
	a.activate(col, len(col.tabs)-1, true)
}

// activate shows tab i of a column, which becomes the column commands
// are for.
func (a *app) activate(col *column, i int, focus bool) {
	if i < 0 || i >= len(col.tabs) {
		return
	}
	col.active = i
	if at := slices.Index(a.cols, col); at >= 0 {
		a.col = at
	}
	if !col.locked {
		a.fileCol = col
	}
	t := col.tabs[i]
	if !t.transient {
		a.addRecent(t.path())
	}
	if focus {
		a.focusTab(t)
	}
	a.find.stale = true
	a.rememberTabs()
}

// addRecent puts a file first among the recent files quick open lists.
func (a *app) addRecent(p string) {
	if p == "" || a.tree == nil {
		return
	}
	a.recent = slices.Insert(slices.DeleteFunc(a.recent, func(r string) bool { return r == p }), 0, p)
	if len(a.recent) > 30 {
		a.recent = a.recent[:30]
	}
}

// focusTab gives the keyboard to a tab's editor, its viewer, or its
// terminal.
func (a *app) focusTab(t *colTab) {
	for _, doc := range a.docs {
		for _, ed := range doc.views {
			ed.focusNext = false
			ed.vscroll.CancelFocus()
		}
	}
	switch {
	case t.term != nil:
		t.term.view.Focus()
	case t.view != nil:
		t.view.pane.Focus()
	case t.diff != nil:
		t.diff.pane.Focus()
	case t.merge != nil:
		if t.merge.ed != nil {
			t.merge.ed.focusNext = true
			t.merge.ed.vscroll.Focus()
		}
	case t.ed.preview != nil:
		t.ed.preview.pane.Focus()
	default:
		t.ed.focusNext = true
		t.ed.vscroll.Focus()
	}
	a.focus = paneEditor
}

// focusColumn gives the keyboard to the tab shown in the column that had
// it last.
func (a *app) focusColumn() {
	if t := a.activeTab(); t != nil {
		a.focusTab(t)
	}
}

func (a *app) cycleTab(step int) {
	col := a.column()
	if len(col.tabs) == 0 {
		return
	}
	a.activate(col, (col.active+step+len(col.tabs))%len(col.tabs), true)
}

// focusColumnBy gives the keyboard to the column step columns after the
// one that has it, round the ends.
func (a *app) focusColumnBy(step int) {
	a.col = (a.col + step + len(a.cols)) % len(a.cols)
	a.find.stale = true
	a.focusColumn()
	a.rememberTabs()
}

// closeTab closes a tab, asking first to save the changes of a document
// that no other tab shows, then calls then unless it stayed open.
func (a *app) closeTab(t *colTab, then func()) {
	done := func() {
		if then != nil {
			then()
		}
	}
	if col, _ := a.locate(t); col == nil {
		done()
		return
	}
	if t.term != nil {
		a.rememberClosed(t)
		a.removeTerminal(t.term)
		done()
		return
	}
	if t.merge != nil {
		remove := func() {
			if col, _ := a.locate(t); col != nil {
				a.rememberClosed(t)
				a.dropTab(t)
				a.focusColumn()
				a.rememberTabs()
			}
			done()
		}
		if doc := t.merge.doc; doc != nil && doc.dirty() && len(doc.views) <= 1 {
			if a.autoSaves(doc) {
				a.saveDoc(doc, remove)
				return
			}
			a.host.confirmClose(doc.name(), func(choice int) {
				switch choice {
				case 0:
					a.saveDoc(doc, remove)
				case 1:
					remove()
				}
			})
			return
		}
		remove()
		return
	}
	remove := func() {
		if col, _ := a.locate(t); col != nil {
			a.rememberClosed(t)
			a.dropTab(t)
			a.focusColumn()
			a.rememberTabs()
		}
		done()
	}
	if t.view != nil || t.diff != nil {
		remove()
		return
	}
	doc := t.ed.document
	if !doc.dirty() || len(doc.views) > 1 {
		remove()
		return
	}
	if a.autoSaves(doc) {
		a.saveDoc(doc, remove)
		return
	}
	a.host.confirmClose(doc.name(), func(choice int) {
		switch choice {
		case 0:
			a.saveDoc(doc, remove)
		case 1:
			a.dropKept(doc)
			remove()
		}
	})
}

// closeTabs closes tabs from the last, asking about each changed
// document, then calls then unless one stayed open.
func (a *app) closeTabs(tabs []*colTab, then func()) {
	if len(tabs) == 0 {
		if then != nil {
			then()
		}
		return
	}
	n := len(tabs) - 1
	a.closeTab(tabs[n], func() { a.closeTabs(tabs[:n], then) })
}

// closeAll closes every document, asking about each changed one, then
// calls then unless one stayed open. Terminals stay.
func (a *app) closeAll(then func()) { a.closeTabs(a.editorTabs(), then) }

// closeOthers closes the tabs of a column other than keep, but those
// whose changes would be lost.
func (a *app) closeOthers(col *column, keep *colTab) {
	for _, t := range slices.Clone(col.tabs) {
		if t != keep && (t.ed == nil || !t.ed.dirty() || len(t.ed.views) > 1 || a.autoSaves(t.ed.document)) {
			a.closeTab(t, nil)
		}
	}
	if col, i := a.locate(keep); col != nil {
		a.activate(col, i, true)
	}
}

// dropTab takes a tab out of its column, and the column with its last
// tab unless it is the only one. A document goes with its last editor.
func (a *app) dropTab(t *colTab) {
	col, i := a.locate(t)
	if col == nil {
		return
	}
	col.take(i)
	if t.view != nil {
		t.view.stop()
	}
	if t.diff != nil {
		t.diff.stop()
	}
	if t.merge != nil {
		v := t.merge
		v.stop()
		if v.doc != nil && len(v.doc.views) == 0 && a.docIndex(v.path) >= 0 {
			a.docs = slices.DeleteFunc(a.docs, func(d *document) bool { return d == v.doc })
			if a.tree != nil {
				a.tree.Unwatch(v.path)
			}
		}
	}
	if t.ed != nil {
		doc := t.ed.document
		if t.ed.preview != nil {
			t.ed.preview.stop()
		}
		t.ed.release()
		if len(doc.views) == 0 {
			doc.hl.Close()
			a.docs = slices.DeleteFunc(a.docs, func(d *document) bool { return d == doc })
			a.docClosed(doc)
			if a.tree != nil && doc.path != "" {
				a.tree.Unwatch(doc.path)
			}
		}
	}
	if len(col.tabs) == 0 {
		a.removeColumn(col)
	}
}

// removeColumn takes a column away, keeping locked columns and the last
// file column. Unlocked columns share its room so locked widths stay put.
func (a *app) removeColumn(col *column) {
	i := slices.Index(a.cols, col)
	if i < 0 || len(a.cols) < 2 || col.locked {
		return
	}
	if slices.ContainsFunc(a.cols, func(c *column) bool { return c.locked }) {
		var other *column
		for _, c := range a.cols {
			if c != col && !c.locked && (other == nil || c == a.fileCol) {
				other = c
			}
		}
		if other == nil {
			return
		}
		other.weight += col.weight
	}
	a.cols = slices.Delete(a.cols, i, i+1)
	if a.fileCol == col {
		a.fileCol = nil
	}
	if a.col > i || a.col == len(a.cols) {
		a.col--
	}
}

// splitColumn opens a column beside col, before it or after, with half
// of its room.
func (a *app) splitColumn(col *column, after bool) *column {
	nc := a.newColumn()
	col.weight /= 2
	nc.weight = col.weight
	at := max(slices.Index(a.cols, col), 0)
	if after {
		at++
	}
	a.cols = slices.Insert(a.cols, at, nc)
	if a.col >= at {
		a.col++
	}
	return nc
}

// splitRight opens a column to the right of the one that has the
// keyboard, showing its document again where it is scrolled, or a new
// terminal beside a terminal.
func (a *app) splitRight() {
	col := a.column()
	t := col.tab()
	if t == nil {
		return
	}
	nc := a.splitColumn(col, true)
	switch {
	case t.term != nil:
		if !a.newTerminalTab(nc, t.term.dir, true) {
			col.weight *= 2
			a.removeColumn(nc)
		}
		return
	case t.view != nil:
		// The same picture, fitted afresh.
		v := newImageViewer(t.view.path)
		v.src, v.w, v.h, v.size, v.err, v.loaded = t.view.src, t.view.w, t.view.h, t.view.size, t.view.err, t.view.loaded
		a.addTab(nc, 0, &colTab{view: v})
		a.activate(nc, 0, true)
		return
	case t.diff != nil:
		v := newDiffView(t.diff.layout)
		if cm := t.diff.commit; cm != nil {
			v = newCommitView(t.diff.layout, cm.key(), cm.info)
		}
		v.repoRoot = t.diff.repoRoot
		// The offsets only: the scroll's handle binds one view's control,
		// and MyGo panics on a handle bound in two columns.
		v.scroll.X, v.scroll.Y = t.diff.scroll.X, t.diff.scroll.Y
		v.full, v.wrap, v.wrapGen = t.diff.full, t.diff.wrap, t.diff.wrapGen
		a.addTab(nc, 0, &colTab{diff: v})
		a.activate(nc, 0, true)
		return
	case t.merge != nil:
		// The same merge, whose result the two views share.
		v := &mergeView{path: t.merge.path, entry: t.merge.entry, op: t.merge.op, chosen: -1, current: -1,
			loaded: t.merge.loaded, loading: t.merge.loading, err: t.merge.err,
			ours: t.merge.ours, theirs: t.merge.theirs,
			base: t.merge.base, oursT: t.merge.oursT, theirsT: t.merge.theirsT,
			baseLines: t.merge.baseLines, oursLines: t.merge.oursLines, theirsLines: t.merge.theirsLines,
			regions: t.merge.regions, binary: t.merge.binary, crlf: t.merge.crlf,
			doc: t.merge.doc}
		// A merge still being read, or a binary one, has no result yet,
		// nor ever: the one still being read is read again for the view.
		fresh := v.doc == nil && !v.binary && v.err == ""
		if v.doc != nil {
			v.ed = newEditor(v.doc)
			v.syncRegions()
		} else if fresh {
			v.loaded, v.loading = false, false
		}
		a.addTab(nc, 0, &colTab{merge: v})
		a.activate(nc, 0, true)
		if fresh {
			a.refreshMerge(v)
		}
		return
	}
	ed := newEditor(t.ed.document)
	ed.sel, ed.caret = t.ed.sel, t.ed.caret
	ed.vscroll.Y, ed.hscroll.X = t.ed.vscroll.Y, t.ed.hscroll.X
	ed.wrap, ed.wrapGen = t.ed.wrap, t.ed.wrapGen
	a.addTab(nc, 0, &colTab{ed: ed})
	a.activate(nc, 0, true)
}

// moveTab moves a tab to place at of a column, which may be its own, and
// shows it there. A column that shows the tab's file already keeps
// its own tab of it, and the one moved goes.
func (a *app) moveTab(t *colTab, to *column, at int) {
	from, i := a.locate(t)
	if from == nil || slices.Index(a.cols, to) < 0 {
		return
	}
	if from != to {
		// A tab dragged to another column is kept.
		t.transient = false
	}
	if from != to && t.term == nil {
		var j int
		switch {
		case t.ed != nil:
			j = to.showing(t.ed.document)
		case t.diff != nil:
			j = to.showingDiff(t.diff)
		case t.merge != nil:
			j = to.showingMerge(t.merge.path)
		default:
			j = to.showingImage(t.view.path)
		}
		if j >= 0 {
			keep := to.tabs[j]
			a.dropTab(t)
			if col, k := a.locate(keep); col != nil {
				a.activate(col, k, true)
			}
			return
		}
	}
	from.take(i)
	if from == to && at > i {
		at--
	}
	at = min(max(at, 0), len(to.tabs))
	to.tabs = slices.Insert(to.tabs, at, t)
	if from != to {
		if t.ed != nil {
			t.ed.caretSeen = false // its text area is a new one there
		}
		if len(from.tabs) == 0 {
			a.removeColumn(from)
		}
	}
	a.activate(to, at, true)
}

// moveTabBy moves the tab shown to the column step columns after its
// own, or to a new one past the last or before the first.
func (a *app) moveTabBy(step int) {
	col := a.column()
	t := col.tab()
	if t == nil {
		return
	}
	switch to := a.col + step; {
	case to >= 0 && to < len(a.cols):
		a.moveTab(t, a.cols[to], len(a.cols[to].tabs))
	case len(col.tabs) > 1:
		a.moveTab(t, a.splitColumn(col, step > 0), 0)
	}
}

// rememberTabs keeps the columns and the files open in them in the
// settings, where each was scrolled and its caret, and the folders open
// in the explorer.
func (a *app) rememberTabs() {
	cols := make([]settings.Column, 0, len(a.cols))
	for _, col := range a.cols {
		cs := settings.Column{Tabs: []settings.Tab{}, Active: -1, Weight: col.weight, Locked: col.locked}
		for i, t := range col.tabs {
			var ts settings.Tab
			switch {
			case t.term != nil:
				ts = settings.Tab{Terminal: true}
			case t.diff != nil && t.diff.commit != nil:
				ts = settings.Tab{Diff: "commit:" + t.diff.commit.key(), Layout: t.diff.layout.key(), Transient: t.transient}
			case t.diff != nil:
				ts = settings.Tab{Diff: "all", Layout: t.diff.layout.key()}
			case t.merge != nil:
				continue // a merge opens again from its conflict, not a session
			case t.path() != "":
				ts = settings.Tab{File: t.path(), Preview: t.ed != nil && t.ed.preview != nil, Transient: t.transient}
			case t.ed != nil && t.ed.kept != "":
				ts = settings.Tab{Untitled: t.ed.kept, Number: t.ed.number}
			default:
				continue
			}
			if v := t.diff; v != nil {
				ts.RepoRoot = v.repoRoot
				if a.git.repo != nil && ts.RepoRoot == a.git.repo.Root {
					ts.RepoRoot = ""
				} else if ts.RepoRoot != "" {
					if rel, err := filepath.Rel(a.root, ts.RepoRoot); err == nil {
						ts.RepoRoot = filepath.ToSlash(rel)
					}
				}
				ts.Full, ts.ScrollY = v.full, v.scroll.Y
				ts.Wrap = a.ownWrap(v.wrap, v.wrapGen)
			}
			if ed := t.ed; ed != nil {
				if saved := ed.restoreTab; saved != nil {
					ts.Preview, ts.Sel = saved.Preview, saved.Sel
					ts.ScrollX, ts.ScrollY, ts.Wrap = saved.ScrollX, saved.ScrollY, saved.Wrap
				} else {
					ts.Sel, ts.ScrollX, ts.ScrollY = ed.sel, ed.hscroll.X, ed.vscroll.Y
					if ed.hidden {
						ts.ScrollY = ed.hiddenY
					}
					ts.Wrap = a.ownWrap(ed.wrap, ed.wrapGen)
				}
			}
			cs.Tabs = append(cs.Tabs, ts)
			if i == col.active {
				cs.Active = len(cs.Tabs) - 1
			}
		}
		cols = append(cols, cs)
	}
	a.window.Columns, a.window.Column = cols, a.col
	a.window.FileColumn = 0
	if i := slices.Index(a.cols, a.fileCol); i >= 0 {
		a.window.FileColumn = i
	}
	if a.tree != nil {
		var open []string
		for dir := range a.files.Open.All() {
			if rel, err := filepath.Rel(a.root, dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				open = append(open, filepath.ToSlash(rel))
			}
		}
		slices.Sort(open)
		a.window.Expanded = open
	}
	a.window.Review = a.keptReview()
	a.settings.Changed()
}

// ownWrap is a tab's own choice to wrap its lines, made while the
// settings' WordWrap was of generation gen, as the settings keep it: nil
// when it follows WordWrap.
func (a *app) ownWrap(wrap *bool, gen int) *bool {
	if wrap == nil || gen != a.settings.WrapGen {
		return nil
	}
	w := *wrap
	return &w
}

// restoreExpanded opens the explorer's folders that were open, those
// still there.
func (a *app) restoreExpanded(rels []string) {
	if a.tree == nil {
		return
	}
	for _, rel := range rels {
		dir := filepath.Join(a.root, filepath.FromSlash(rel))
		// A remote folder is looked at as it is read.
		if remote.Is(dir) {
			a.files.Open.Add(dir)
			a.tree.Load(dir)
		} else if info, err := os.Stat(dir); err == nil && info.IsDir() {
			a.files.Open.Add(dir)
			a.tree.Load(dir)
		}
	}
}

// restoreTab puts back where a tab restored was scrolled, its caret and
// its own choice to wrap.
func (a *app) restoreTab(t *colTab, ts settings.Tab) {
	if v := t.diff; v != nil {
		v.full, v.scroll.Y = ts.Full, ts.ScrollY
		if ts.Wrap != nil {
			w := *ts.Wrap
			v.wrap, v.wrapGen = &w, a.settings.WrapGen
		}
	}
	if ed := t.ed; ed != nil {
		if ed.loading || ed.loadErr != nil {
			// Once it is read.
			ed.restored = true
			ed.restoreTab = &ts
			return
		}
		a.restoreEditor(ed, ts)
	}
}

// restoreEditor puts back where an editor of a tab restored was scrolled,
// its caret and its own choice to wrap.
func (a *app) restoreEditor(ed *editor, ts settings.Tab) {
	// The file may be shorter than it was.
	n := ed.buf.Len()
	ed.sel = [2]int{min(max(ts.Sel[0], 0), n), min(max(ts.Sel[1], 0), n)}
	ed.caret = ed.sel[1]
	ed.vscroll.Y, ed.hscroll.X = ts.ScrollY, ts.ScrollX
	if ed.hidden {
		ed.hiddenY = ts.ScrollY
	}
	if ts.Wrap != nil {
		w := *ts.Wrap
		ed.wrap, ed.wrapGen = &w, a.settings.WrapGen
	}
	a.setPreview(ed, ts.Preview)
}

// restoreTabs opens the columns of the last session and their files, as
// they were scrolled, starts a terminal where one was, and opens the
// explorer's folders that were open.
func (a *app) restoreTabs() {
	saved, focused, fileFocused := a.window.Columns, a.window.Column, a.window.FileColumn
	a.restoreExpanded(a.window.Expanded)
	a.restoreReview(a.window.Review)
	// Allocate each saved column before opening tabs. Empty locked columns
	// and the file workspace beside them are part of the saved layout too.
	restored := make([]*column, len(saved))
	for i, cs := range saved {
		col := a.cols[len(a.cols)-1]
		if i > 0 || len(col.tabs) > 0 {
			col = a.newColumn()
			a.cols = append(a.cols, col)
		}
		col.locked = cs.Locked
		if cs.Weight > 0 {
			col.weight = cs.Weight
		}
		restored[i] = col
	}
	for i, cs := range saved {
		col := restored[i]
		a.col = slices.Index(a.cols, col)
		// Opening each makes it the tab shown: keep the one to show.
		var shown *colTab
		for k, ts := range cs.Tabs {
			// Relative roots move with the workspace; absolute roots from older
			// settings remain readable.
			if ts.RepoRoot != "" && !filepath.IsAbs(ts.RepoRoot) {
				ts.RepoRoot = filepath.Join(a.root, filepath.FromSlash(ts.RepoRoot))
			}
			n := len(col.tabs)
			switch {
			case ts.Terminal:
				if a.root != "" {
					a.newTerminalTab(col, a.root, false)
				}
			case strings.HasPrefix(ts.Diff, "commit:"):
				hash := strings.TrimPrefix(ts.Diff, "commit:")
				if c, _ := a.commitTab(hash, ts.RepoRoot); c == nil && hash != "" {
					v := newCommitView(parseDiffLayout(ts.Layout), hash, git.Commit{})
					v.repoRoot = ts.RepoRoot
					a.addTab(col, len(col.tabs), &colTab{diff: v, transient: ts.Transient})
					a.activate(col, len(col.tabs)-1, false)
				}
			case ts.Diff != "":
				// The tabs of the changes of a file an older version kept are
				// the one tab of them all.
				if c, _ := a.changesTab(ts.RepoRoot); c == nil {
					layout := parseDiffLayout(ts.Layout)
					if ts.Split && ts.Layout == "" {
						layout = layoutSplit
					}
					v := newDiffView(layout)
					v.repoRoot = ts.RepoRoot
					a.addTab(col, len(col.tabs), &colTab{diff: v})
					a.activate(col, len(col.tabs)-1, false)
				}
			case ts.File != "":
				// A remote file that is gone closes its tab once read.
				if _, err := os.Stat(ts.File); err == nil || remote.Is(ts.File) {
					a.opening = ts.Transient
					a.openText(func() { a.openFileIn(col, len(col.tabs), ts.File, false) })
					a.opening = false
				}
			case ts.Untitled != "":
				a.restoreUntitled(col, ts)
			}
			if len(col.tabs) > n {
				a.restoreTab(col.tab(), ts)
				if k == cs.Active {
					shown = col.tab()
				}
			}
		}
		if _, k := a.locate(shown); k >= 0 {
			a.activate(col, k, false)
		}
	}
	// Missing files may leave an ordinary column empty, but a locked
	// column and its last file workspace must survive restoration.
	for _, col := range slices.Clone(a.cols) {
		if len(col.tabs) == 0 {
			a.removeColumn(col)
		}
	}
	// The saved index refers to restored, before empty columns were removed.
	// Resolve it only after cleanup, falling back to fileColumn if it went.
	a.fileCol = nil
	if fileFocused >= 0 && fileFocused < len(restored) {
		col := restored[fileFocused]
		if !col.locked && slices.Contains(a.cols, col) {
			a.fileCol = col
		}
	}
	if focused >= 0 && focused < len(restored) {
		if at := slices.Index(a.cols, restored[focused]); at >= 0 {
			a.col = at
		}
	}
	a.fileColumn()
	a.rememberTabs()
}
