package main

import (
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// liveCell is the cell of a table that Live Preview edits in place, in
// one editor: the table by its first line and its last, as its source
// changes with every edit, and the cell by its row, 0 the header, and its
// column. The text stays the source of truth: what is typed in the cell
// is written to it as typing pauses, or as the cell is left, an edit of
// the text that undoes as typing does. Written at each key, it would have
// the text area lay out all of the text again for each.
type liveCell struct {
	on          bool
	first, last int
	row, col    int
	// text is what the input shows, and source the cell's source as last
	// written or read, by which an edit made elsewhere, as undo or one in
	// another column, shows in the input.
	text, source string
	// key is the key of the table's view, which the view keeps as the
	// table's source changes under the cell.
	key string
	box ui.Handle
	// focus asks the input for the keyboard, focused is set once it has
	// it, and caret is where its caret goes, -1 for nowhere new.
	focus, focused bool
	caret          int
	// typed is the input's caret as last built, seen its text then, and
	// wait the timer that writes what was typed once typing pauses.
	typed int
	seen  string
	wait  *time.Timer
	// width is how wide its column is at least while it is edited, for
	// its source to show, which is wider than its text drawn: measured as
	// it opens, 0 until then, and kept as it is typed in.
	width float32
}

// rowLine is the line of row r of the table, after its line of
// alignments.
func (cell *liveCell) rowLine(r int) int {
	if r == 0 {
		return cell.first
	}
	return cell.first + 1 + r
}

// rows is how many rows the table has, its header one.
func (cell *liveCell) rows() int { return max(1, cell.last-cell.first) }

// liveCellLine is the line of the row the cell is of and where it starts.
func liveCellLine(snap ui.TextSnapshot, cell *liveCell) (line string, start int, ok bool) {
	ln := cell.rowLine(cell.row)
	if ln > cell.last || ln >= snap.LineCount() {
		return "", 0, false
	}
	s, e := snap.LineRange(ln)
	return snap.Slice(s, e), s, true
}

// liveTable reports whether a block of Live Preview is a table, whose
// cells edit in place.
func liveTable(b *liveBlock) bool {
	return !b.embed && len(b.blocks) > 0 && b.blocks[0].kind == markdownTableHeader
}

// openLiveCell edits cell k of row r of the table of lines first up to
// last in place, its source in the input, the caret at its end. The
// editor's caret waits after the table, out of the way, without moving
// the view.
func (a *app) openLiveCell(d *editor, first, last, r, k int) {
	a.flushLiveCell(d)
	key := ""
	if d.liveCell.on && d.liveCell.first == first {
		key = d.liveCell.key
	}
	d.liveCell = liveCell{on: true, first: first, last: last, row: r, col: k, key: key, focus: true, caret: -1}
	cell := &d.liveCell
	snap := d.buf.Snapshot()
	line, _, ok := liveCellLine(snap, cell)
	if !ok {
		d.liveCell = liveCell{}
		return
	}
	cell.text = tableCellText(line, k)
	cell.source, cell.seen, cell.caret = cell.text, cell.text, utf8.RuneCountInString(cell.text)
	if !d.selOutside(snap, first, last) {
		at := liveAfter(snap, last)
		d.selectRange(at, at, false)
		d.quiet = true
	}
	// What is typed in the cell is a step of undo of its own.
	d.hist.typing = false
	a.invalidate()
}

// selOutside reports whether the editor's selection is outside the
// lines first up to last.
func (d *editor) selOutside(snap ui.TextSnapshot, first, last int) bool {
	n := snap.Len()
	s, e := snap.LineAt(min(d.sel[0], d.sel[1], n)), snap.LineAt(min(max(d.sel[0], d.sel[1]), n))
	return e < first || s > last
}

// liveAfter is where the caret goes after a table whose last line is
// last: the start of the line after it.
func liveAfter(snap ui.TextSnapshot, last int) int {
	if last+1 < snap.LineCount() {
		s, _ := snap.LineRange(last + 1)
		return s
	}
	_, e := snap.LineRange(min(last, snap.LineCount()-1))
	return e
}

// closeLiveCell stops editing the cell in place; the table measures its
// columns again for what it holds now.
func (a *app) closeLiveCell(d *editor) {
	a.flushLiveCell(d)
	if lv := d.livePane.views[d.liveCell.key]; lv != nil {
		lv.v.markdown.tables = nil
	}
	if d.liveCell.wait != nil {
		d.liveCell.wait.Stop()
	}
	d.liveCell = liveCell{}
	a.invalidate()
}

// leaveLiveCell stops editing the cell in place and gives the editor the
// keyboard, its caret before the table or after it.
func (a *app) leaveLiveCell(d *editor, before bool) {
	cell := d.liveCell
	a.closeLiveCell(d)
	snap := d.buf.Snapshot()
	at := liveAfter(snap, cell.last)
	if before {
		at = 0
		if cell.first > 0 && cell.first-1 < snap.LineCount() {
			_, at = snap.LineRange(cell.first - 1)
		}
	}
	d.selectRange(at, at, false)
	d.focusNext = true
}

// moveLiveCell edits the cell by the one edited across columns, as Tab
// moves, or down rows, wrapping at the ends of the rows; past the table
// the editor takes the keyboard.
func (a *app) moveLiveCell(d *editor, across, down int) {
	cell := &d.liveCell
	snap := d.buf.Snapshot()
	cols := 1
	if cell.first < snap.LineCount() {
		s, e := snap.LineRange(cell.first)
		cells, _ := tableRowCells(snap.Slice(s, e))
		cols = max(1, len(cells))
	}
	r, k := cell.row+down, cell.col+across
	switch {
	case k >= cols:
		r, k = r+1, 0
	case k < 0:
		r, k = r-1, cols-1
	}
	if r < 0 || r >= cell.rows() {
		a.leaveLiveCell(d, r < 0)
		return
	}
	a.openLiveCell(d, cell.first, cell.last, r, k)
}

// liveCellKey takes a key pressed in the input of a cell: Tab and Shift-Tab
// move across cells, Return and the arrows up and down move between rows,
// Escape leaves, and undo and redo are the text's.
func (a *app) liveCellKey(d *editor, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	m := ev.Mods
	switch {
	case ev.Key == ui.KeyTab && m == 0:
		a.moveLiveCell(d, 1, 0)
	case ev.Key == ui.KeyTab && m == ui.Shift:
		a.moveLiveCell(d, -1, 0)
	case (ev.Key == ui.KeyEnter || ev.Key == ui.KeyDown) && m == 0:
		a.moveLiveCell(d, 0, 1)
	case ev.Key == ui.KeyUp && m == 0:
		a.moveLiveCell(d, 0, -1)
	case ev.Key == ui.KeyEscape && m == 0:
		a.leaveLiveCell(d, false)
	case ev.Key == ui.KeyZ && m == ui.Cmd:
		a.flushLiveCell(d)
		d.undo()
		d.quiet = true
	case ev.Key == ui.KeyZ && m == ui.Cmd|ui.Shift:
		a.flushLiveCell(d)
		d.redo()
		d.quiet = true
	default:
		return false
	}
	a.invalidate()
	return true
}

// followLiveCell keeps the cell edited in place with its table, whose last
// line is last, as a frame finds it: the input shows the
// cell's source edited elsewhere, and a cell the table lost stops being
// edited.
func (a *app) followLiveCell(d *editor, snap ui.TextSnapshot, last int) {
	cell := &d.liveCell
	cell.last = last
	line, _, ok := liveCellLine(snap, cell)
	if !ok {
		a.closeLiveCell(d)
		return
	}
	if src := tableCellText(line, cell.col); src != cell.source {
		cell.text, cell.source, cell.seen, cell.caret = src, src, src, utf8.RuneCountInString(src)
	}
}

// flushLiveCell writes what was typed into the cell to its source, its
// pipes escaped, as typing in the text, whose caret stays where it is.
func (a *app) flushLiveCell(d *editor) {
	cell := &d.liveCell
	if !cell.on {
		return
	}
	if cell.wait != nil {
		cell.wait.Stop()
		cell.wait = nil
	}
	src := tableCellSource(cell.text)
	if src == cell.source {
		return
	}
	snap := d.buf.Snapshot()
	line, ls, ok := liveCellLine(snap, cell)
	if !ok || d.locked() {
		cell.text, cell.seen, cell.caret = cell.source, cell.source, utf8.RuneCountInString(cell.source)
		return
	}
	bs, be, with := tableCellEdit(line, cell.col, src)
	start, end := ls+utf8.RuneCountInString(line[:bs]), ls+utf8.RuneCountInString(line[:be])
	d.typeEdit(textEdit{start: start, end: end, text: with})
	cell.source = src
	if src != cell.text {
		// A pipe typed is escaped: the caret goes after its backslash.
		typed := []rune(cell.text)
		cell.caret = utf8.RuneCountInString(tableCellSource(string(typed[:min(cell.typed, len(typed))])))
		cell.text, cell.seen = src, src
	}
	a.invalidate()
}

// typeEdit makes an edit as typing: a step of undo of its own, or joined
// to the typing before it without a pause. The view stays where it is.
func (d *editor) typeEdit(e textEdit) {
	d.observe()
	h := &d.hist
	now := time.Now()
	h.pushTyping(histStep{h.snap, d.sel, slices.Clone(d.cursors)}, now)
	moved := movedByEdits([]textEdit{e})
	d.applyEdits([2]int{moved(d.sel[0]), moved(d.sel[1])}, []textEdit{e})
	d.quiet = true
	h.typing, h.at = true, now
}

// liveCellHooks has the table of a view of Live Preview, of lines first up
// to last, edit its cells in place: a press on a cell edits it, and the
// cell edited draws its input.
func (a *app) liveCellHooks(d *editor, m *markdownPreview, first, last int) {
	m.cellPress = func(r, k int, ev ui.InputEvent) bool {
		// ⌘ with a click is a link's, as elsewhere in Live Preview.
		if ev.Kind != ui.InputPointerDown || ev.Button != 0 || ev.Clicks != 1 || ev.Mods&ui.Cmd != 0 {
			return false
		}
		if cell := &d.liveCell; cell.on && cell.first == first && cell.row == r && cell.col == k {
			cell.focus = true // its padding: the input keeps the keyboard
			a.invalidate()
			return true
		}
		d.cmds = append(d.cmds, func(d *editor) { a.openLiveCell(d, first, last, r, k) })
		a.invalidate()
		return true
	}
	m.cellEditing = func(r, k int) bool {
		cell := &d.liveCell
		return cell.on && cell.first == first && cell.row == r && cell.col == k
	}
	m.cellWidth = func(font ui.Font) (int, float32) {
		cell := &d.liveCell
		if !cell.on || cell.first != first {
			return -1, 0
		}
		if cell.width == 0 {
			f := font
			if cell.row == 0 {
				f.Weight = 700
			}
			w, _ := ui.ShapeText(cell.source+" ", f, 0).Size()
			size := font.Size
			cell.width = clamp(w, size*2, size*24) + size*1.5 + 2
		}
		return cell.col, cell.width
	}
	m.cellInput = func(c *ui.Context, align ui.Align, size float32) {
		cell := &d.liveCell
		label := locales.LiveCellLabel(strconv.Itoa(cell.row+1), strconv.Itoa(cell.col+1))
		in := ui.TextInputBase(c.Key("live-cell"), &cell.text).Bind(&cell.box).WidthPercent(100).FontSize(size).
			LineHeight(a.settings.LineHeight).TextAlign(align).TextColor(a.pal.text).Label(label)
		if a.chording() {
			in.ReadOnly(true)
		}
		if in.Focused() {
			a.keyFocus = placeInput
		}
		in.HandleInput(func(ev ui.InputEvent) bool {
			if a.liveCellKey(d, ev) {
				return true
			}
			if ev.Kind == ui.InputKeyDown && ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
				// A shortcut, as Save, finds the text with the cell in it.
				a.flushLiveCell(d)
			}
			return a.keyEvent(ev, placeInput)
		})
		if cell.focus {
			cell.focus = false
			cell.box.Focus(c)
		}
		if cell.caret >= 0 {
			in.SetTextSelection(cell.caret, cell.caret)
			cell.caret = -1
		}
		cell.typed, _ = in.TextSelection()
		if cell.text != cell.seen {
			cell.seen = cell.text
			if cell.wait != nil {
				cell.wait.Stop()
			}
			cell.wait = time.AfterFunc(liveDelay, func() { a.post(func() { a.flushLiveCell(d) }) })
		}
	}
}

// keepLiveCell notes whether the input of the cell edited has the keyboard,
// and stops editing the cell once it lost it, as to a click elsewhere.
func (a *app) keepLiveCell(c *ui.Context, d *editor) {
	cell := &d.liveCell
	if !cell.on || cell.focus {
		return
	}
	switch {
	case cell.box.Focused(c):
		cell.focused = true
	case cell.focused:
		a.closeLiveCell(d)
	}
}
