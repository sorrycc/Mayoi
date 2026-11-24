package main

import (
	"slices"
	"strconv"
	"strings"
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
	// at is a rune of the table's header, past its start, which the
	// edits above the table move, from any view of the note: first is
	// its line.
	at int
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
	// reveal is how many frames more the view scrolls the cell into view,
	// as it moved, until its row shows whole: the table may grow or
	// shrink meanwhile, as a row is added at the end of the text or its
	// column widens for the source. shown is set while the last frame
	// showed its row whole.
	reveal int
	shown  bool
	// settling is how many frames more, after an edit made from the cell,
	// the text area may measure its lines again: what it takes for their
	// heights until then makes the text shorter, which scrolls the view,
	// until it is as high as it was, maxY as far as it scrolled last. The
	// cell is shown again as long as that changes.
	settling int
	maxY     float32
	// typed is the input's caret as last built, seen its text then, and
	// wait the timer that writes what was typed once typing pauses.
	typed int
	seen  string
	wait  *time.Timer
	// parsed is the drawing of the table parsed from its source, src,
	// while the last parse has other rows or columns.
	parsed []markdownBlock
	src    string
	// width is how wide its column is at least while it is edited, for
	// its source to show, which is wider than its text drawn: measured as
	// it opens, 0 until then, and kept as it is typed in.
	width float32
}

// liveReveal is how many frames at most a cell that moved is scrolled
// into view, while its row does not show whole yet.
const liveReveal = 10

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

// cols is how many columns the table's header has.
func (cell *liveCell) cols(snap ui.TextSnapshot) int {
	if cell.first >= snap.LineCount() {
		return 1
	}
	cells, _ := tableRowCells(snap.Line(cell.first))
	return max(1, len(cells))
}

// liveTableEnd is the last line of the table whose header is line first,
// last at least: the rows added after it since the note was parsed too,
// each a line with a pipe, quoted as the header.
func liveTableEnd(snap ui.TextSnapshot, first, last int) int {
	if first >= snap.LineCount() {
		return last
	}
	quote := liveQuoteMarks.FindString(snap.Line(first))
	for last+1 < snap.LineCount() {
		l := snap.Line(last + 1)
		if liveQuoteMarks.FindString(l) != quote || !strings.Contains(l, "|") {
			break
		}
		last++
	}
	return last
}

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
	d.liveCell = liveCell{on: true, first: first, last: last, row: r, col: k, key: key, focus: true, caret: -1, reveal: liveReveal}
	cell := &d.liveCell
	snap := d.buf.Snapshot()
	line, _, ok := liveCellLine(snap, cell)
	if !ok {
		d.liveCell = liveCell{}
		return
	}
	cell.text = tableCellText(line, k)
	cell.source, cell.seen, cell.caret = cell.text, cell.text, utf8.RuneCountInString(cell.text)
	hs, he := snap.LineRange(first)
	cell.at = min(hs+1, he)
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
	cols := cell.cols(d.buf.Snapshot())
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

// liveCellBlocks is the drawing of the table of the cell edited, parsed
// from its source as it is now, while its rows or its columns are not the
// ones the last parse drew, as a row or a column added from a cell, so
// that the cell shows at once and what is typed reaches it: nil once the
// parse drew it so, as it does in a moment.
func (a *app) liveCellBlocks(d *editor, snap ui.TextSnapshot, drawn []markdownBlock) []markdownBlock {
	cell := &d.liveCell
	rows := 0
	for _, b := range drawn {
		if b.kind == markdownTable || b.kind == markdownTableHeader {
			rows++
		}
	}
	if len(drawn) > 0 && rows == cell.rows() && len(drawn[0].cells) == cell.cols(snap) {
		cell.parsed, cell.src = nil, ""
		return nil
	}
	s, _ := snap.LineRange(cell.first)
	_, e := snap.LineRange(cell.last)
	if src := snap.Slice(s, e); cell.parsed == nil || src != cell.src {
		cell.parsed = parseMarkdown([]byte(src), d.path, markdownRoot(d.path, a.folder()), nil)
		cell.src = src
	}
	if len(cell.parsed) == 0 || cell.parsed[0].kind != markdownTableHeader {
		return nil
	}
	return cell.parsed
}

// liveCellKey takes a key pressed in the input of a cell: Tab and Shift-Tab
// move across cells, Return and the arrows up and down move between rows,
// Escape leaves, and undo and redo are the text's. Tab after the last cell
// adds a row, and so does Return in the last row, but for an empty one,
// which it takes away to leave the table.
func (a *app) liveCellKey(d *editor, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	m := ev.Mods
	cell := &d.liveCell
	lastRow := cell.row == cell.rows()-1
	switch {
	case ev.Key == ui.KeyTab && m == 0:
		if lastRow && cell.col >= cell.cols(d.buf.Snapshot())-1 {
			if a.liveTableEdit(d, cell.first, cell.last, tableRowBelow, cell.row, cell.col) {
				a.openLiveCell(d, cell.first, cell.last, cell.row, 0)
				break
			}
		}
		a.moveLiveCell(d, 1, 0)
	case ev.Key == ui.KeyTab && m == ui.Shift:
		a.moveLiveCell(d, -1, 0)
	case ev.Key == ui.KeyEnter && m == 0 && lastRow:
		a.flushLiveCell(d)
		if cell.row > 0 && a.liveRowEmpty(d) {
			if a.liveTableEdit(d, cell.first, cell.last, tableRowDelete, cell.row, cell.col) {
				a.leaveLiveCell(d, false)
				break
			}
		}
		if !a.liveTableEdit(d, cell.first, cell.last, tableRowBelow, cell.row, cell.col) {
			a.moveLiveCell(d, 0, 1)
		}
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
		cell.settle()
	case ev.Key == ui.KeyZ && m == ui.Cmd|ui.Shift:
		a.flushLiveCell(d)
		d.redo()
		d.quiet = true
		cell.settle()
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
	cell.last = liveTableEnd(snap, cell.first, last)
	if r, k := min(cell.row, cell.rows()-1), min(cell.col, cell.cols(snap)-1); r != cell.row || k != cell.col {
		// Its row or its column gone, as undo took it back: the nearest
		// one, whose input keeps the keyboard.
		cell.row, cell.col, cell.width = r, k, 0
		cell.focus = cell.focus || cell.focused
		cell.reveal = liveReveal
	}
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
	cell.settle()
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
	h.pushTyping(histStep{snap: h.snap, sel: d.sel, cursors: slices.Clone(d.cursors)}, now)
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
	m.cellMenu = func(r, k int, menu *ui.Menu) { a.liveTableMenu(menu, d, first, last, r, k) }
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
		r, k := cell.row, cell.col
		in.ContextMenu(func(menu *ui.Menu) {
			a.liveTableMenu(menu, d, first, last, r, k)
			menu.Separator()
			menu.EditItems()
		})
		in.HandleInput(func(ev ui.InputEvent) bool {
			if a.liveCellKey(d, ev) || m.passScroll(ev) {
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

// liveRowEmpty reports whether the row of the cell edited has nothing in
// any of its cells.
func (a *app) liveRowEmpty(d *editor) bool {
	line, _, ok := liveCellLine(d.buf.Snapshot(), &d.liveCell)
	if !ok {
		return false
	}
	cells, _ := tableRowCells(line)
	for _, c := range cells {
		if strings.TrimSpace(line[c[0]:c[1]]) != "" {
			return false
		}
	}
	return true
}

// liveTableEdit changes the rows or the columns of the table of lines
// first up to last by op, at its cell of row r, 0 its header, and column
// k: what was typed in a cell written first, then the change as one step
// of undo. The cell that stands for it is edited then, the one added or
// the one beside the one taken away, and a cell another view edits keeps
// to its own. It reports whether it changed the table: not one that is
// gone, or locked, or for a change that would leave it no table.
func (a *app) liveTableEdit(d *editor, first, last int, op tableOp, r, k int) bool {
	for _, o := range d.views {
		if o.liveCell.on && o.liveCell.first == first {
			a.flushLiveCell(o) // written to its row before rows move
		}
	}
	snap := d.buf.Snapshot()
	if d.locked() || first < 0 || first+1 >= snap.LineCount() {
		return false
	}
	last = liveTableEnd(snap, first, max(last, first+1))
	s, _ := snap.LineRange(first)
	_, e := snap.LineRange(last)
	src := snap.Slice(s, e)
	rows, cols := tableShape(strings.Split(src, "\n"))
	edits, ok := tableStructure(src, op, r, k)
	if !ok {
		return false
	}
	te := make([]textEdit, len(edits))
	for i, x := range edits {
		start := s + utf8.RuneCountInString(src[:x.start])
		te[i] = textEdit{start, start + utf8.RuneCountInString(src[x.start:x.end]), x.text}
	}
	sortEdits(te)
	moved := movedByEdits(te)
	d.hist.typing = false
	d.edit([2]int{moved(d.sel[0]), moved(d.sel[1])}, te...)
	d.quiet = true

	// The rows and the columns after the change, and where the cell goes.
	nr, nk, dr, dk, from := r, k, 0, 0, 0
	switch op {
	case tableRowAbove:
		dr, from = 1, r
	case tableRowBelow:
		nr, dr, from = r+1, 1, r+1
	case tableRowDelete:
		dr, from = -1, r
		if r == rows-1 {
			nr = r - 1
		}
	case tableColLeft:
		dk, from = 1, k
	case tableColRight:
		nk, dk, from = k+1, 1, k+1
	case tableColDelete:
		dk, from, nk = -1, k, min(k, cols-2)
	}
	for _, o := range d.views {
		c := &o.liveCell
		if o == d || !c.on || c.first != first {
			continue
		}
		at := &c.row
		if dk != 0 {
			at = &c.col
		}
		switch {
		case dr+dk < 0 && *at == from:
			a.closeLiveCell(o) // its row or column taken away
		case *at >= from:
			*at += dr + dk
		}
		c.last += dr
	}
	if l := d.document.live; l != nil {
		l.soon = true // the table drawn as it is now, not in a moment
	}
	if lv := d.livePane.views[d.liveCell.key]; lv != nil && d.liveCell.first == first {
		lv.v.markdown.tables = nil
	}
	a.openLiveCell(d, first, last+dr, nr, nk)
	d.liveCell.settle()
	return true
}

// settle keeps the cell in view while the text area measures its lines
// again after an edit made from it.
func (cell *liveCell) settle() { cell.settling, cell.maxY = liveSettle, -1 }

// liveSettle is how many frames an edit made from a cell is followed by,
// at least, for the text area to measure its lines again: as many again
// after each frame it scrolls further or less far.
const liveSettle = 4

// liveTableTarget is the table and the cell a menu of a table's cell
// opened on: by its header's line, its row and its column, and how many
// rows and columns it had then.
type liveTableTarget struct {
	first, r, k, rows, cols int
}

// liveTableShape is the table whose header is line first, as its last
// line, its rows and its columns: 0 rows for one that is not a table.
func liveTableShape(snap ui.TextSnapshot, first, last int) (end, rows, cols int) {
	if first < 0 || first+1 >= snap.LineCount() {
		return last, 0, 0
	}
	end = liveTableEnd(snap, first, max(last, first+1))
	s, _ := snap.LineRange(first)
	_, e := snap.LineRange(end)
	rows, cols = tableShape(strings.Split(snap.Slice(s, e), "\n"))
	return end, rows, cols
}

// liveTableMenu fills the menu of the cell of row r and column k of the
// table of lines first up to last, which adds and takes away its rows
// and its columns. The change chosen is made to the table the menu opened
// on, or not at all: one whose rows or columns came or went since, as
// from another view, is another table now.
func (a *app) liveTableMenu(m *ui.Menu, d *editor, first, last, r, k int) {
	end, rows, cols := liveTableShape(d.buf.Snapshot(), first, last)
	chosen := tableOp(-1)
	for i, it := range []struct {
		op    tableOp
		label string
	}{
		{tableRowAbove, locales.LiveTableRowAbove()},
		{tableRowBelow, locales.LiveTableRowBelow()},
		{tableRowDelete, locales.LiveTableRowDelete()},
		{tableColLeft, locales.LiveTableColLeft()},
		{tableColRight, locales.LiveTableColRight()},
		{tableColDelete, locales.LiveTableColDelete()},
	} {
		if i == 3 {
			m.Separator()
		}
		if m.Item(it.label).Disabled(!tableCanDo(it.op, rows, cols, r, k)).Chosen() {
			chosen = it.op
		}
	}
	now := liveTableTarget{first, r, k, rows, cols}
	if chosen < 0 {
		d.liveMenu = now // the menu opening
		return
	}
	if d.liveMenu != now {
		return
	}
	d.liveMenu = liveTableTarget{}
	d.cmds = append(d.cmds, func(d *editor) {
		if _, nr, nc := liveTableShape(d.buf.Snapshot(), first, end); nr != rows || nc != cols {
			return
		}
		a.liveTableEdit(d, first, end, chosen, r, k)
	})
	a.invalidate()
}
