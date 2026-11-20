package main

import (
	"cmp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// history is a document's undo and redo, which the app keeps rather than
// its text area, so that the edits the app makes (indenting, moving lines,
// replacing) undo as typing does. A step is the text before an edit and
// the selection then: the buffer is a persistent tree, so that a step
// shares all but the chunks edited with the text after it.
type history struct {
	undo, redo []histStep
	snap       ui.TextSnapshot // the text as last seen
	seen       bool
	// typing is set while the last step is typing that more typing joins:
	// it ends with a pause, a move of the caret, or an edit of the app's.
	typing bool
	at     time.Time
	// group joins the edits of an insert of vim mode into one step, with
	// the change that started it: groupOpen until its first edit, which
	// makes a step, groupJoin after, as the edits join the last step.
	group histGroup
}

type histGroup uint8

const (
	groupNone histGroup = iota
	groupOpen
	groupJoin
)

// grouped makes the step for an edit about to be made: a new one, or none
// while the edit joins the group's.
func (h *history) grouped(s histStep) {
	if h.group == groupJoin && len(h.undo) > 0 {
		clear(h.redo)
		h.redo = h.redo[:0]
		return
	}
	h.push(s)
	if h.group == groupOpen {
		h.group = groupJoin
	}
}

type histStep struct {
	snap ui.TextSnapshot
	sel  [2]int
	// A log document's step is not a snapshot but the edits that undo
	// it, in base runes, with the selection there in base runes too: its
	// buffer holds a run of the text, not all of it.
	logUndo []logEdit
	logSel  [2]int64
}

const (
	maxUndo     = 1000
	typingPause = 1200 * time.Millisecond
)

func (h *history) push(s histStep) {
	h.undo = append(h.undo, s)
	if len(h.undo) > maxUndo {
		h.undo = append(h.undo[:0], h.undo[len(h.undo)-maxUndo:]...)
	}
	clear(h.redo)
	h.redo = h.redo[:0]
}

// observe records the edits made to d's text since it was last seen,
// which are typing: typing without a pause joins the step before. The
// typing is the editor's that has the keyboard, whose selection the step
// keeps; the selections of the others move with the text.
func (d *document) observe() {
	h := &d.hist
	snap := d.buf.Snapshot()
	if !h.seen {
		h.snap, h.seen = snap, true
		return
	}
	if snap.Version() == h.snap.Version() {
		return
	}
	t := d.main()
	if t == nil {
		h.snap = snap
		return
	}
	now := time.Now()
	old, sel := h.snap, t.sel
	if d.log != nil {
		d.observeLog(t, old, snap, sel, now)
		return
	}
	switch {
	case h.group != groupNone:
		h.grouped(histStep{snap: h.snap, sel: t.sel})
	case !h.typing || now.Sub(h.at) > typingPause || len(h.undo) == 0:
		h.push(histStep{snap: h.snap, sel: t.sel})
	default:
		clear(h.redo)
		h.redo = h.redo[:0]
	}
	h.snap, h.typing, h.at = snap, true, now
	// Brackets and quotes typed close, in the same step.
	if t.autoClose(old, snap, sel) {
		h.snap = d.buf.Snapshot()
	}
	t.keepClosers(h.snap)
	t.followChange(old, h.snap, min(sel[0], sel[1]))
}

// observeLog is observe for a document in log mode: what typing changed
// of the window's text is one edit of the text as a whole.
func (d *document) observeLog(t *editor, old, snap ui.TextSnapshot, sel [2]int, now time.Time) {
	h := &d.hist
	l := d.log
	join := h.group != groupNone || h.typing && now.Sub(h.at) <= typingPause && len(h.undo) > 0
	h.snap, h.typing, h.at = snap, true, now
	// Brackets and quotes typed close, in the same step.
	if t.autoClose(old, snap, sel) {
		h.snap = d.buf.Snapshot()
	}
	off := l.win.start
	var u logEdit
	var applied bool
	var err error
	if start, end, ok := changedRunes(old, h.snap, min(sel[1], old.Len())); ok {
		// What the typing replaced, read as followChange reads it: the
		// old text's runes start up to end, the new one's the n that
		// follow start.
		n := end - start + h.snap.Len() - old.Len()
		u, applied, err = l.applyEdit(off+int64(start), off+int64(end), h.snap.Slice(start, start+n))
	} else {
		// The change reaches past where its ends are looked for: the
		// window's whole run is replaced, for the text cannot be left
		// other than what the buffer holds.
		u, applied, err = l.applyEdit(off, off+int64(old.Len()), h.snap.Slice(0, h.snap.Len()))
	}
	switch {
	case err != nil:
		// The file cannot be read to say what the typing replaced:
		// the document says so and keeps what it has.
		d.sync, d.loadErr = syncLoadFailed, err
	case applied:
		s := histStep{logUndo: []logEdit{u}, logSel: d.selBase(sel)}
		if join && len(h.undo) > 0 {
			last := &h.undo[len(h.undo)-1]
			last.logUndo = append(last.logUndo, u)
			clear(h.redo)
			h.redo = h.redo[:0]
		} else {
			h.push(s)
		}
	}
	t.keepClosers(h.snap)
	t.followChange(old, h.snap, min(sel[0], sel[1]))
}

// selBase returns where a selection of the window is in the base runes
// of a log document's text.
func (d *document) selBase(sel [2]int) [2]int64 {
	l := d.log
	n := int64(d.buf.Len())
	off := l.win.start
	edits := l.editsCopy()
	return [2]int64{
		baseOf(edits, off+min(max(int64(sel[0]), 0), n), nil),
		baseOf(edits, off+min(max(int64(sel[1]), 0), n), nil),
	}
}

// textEdit replaces the runes start up to end with text.
type textEdit struct {
	start, end int
	text       string
}

// edit makes edits of the app's as one step of undo, then selects sel.
// The edits must not overlap; they are made from the last.
func (d *editor) edit(sel [2]int, edits ...textEdit) {
	if d.locked() {
		return
	}
	if d.log != nil {
		d.editLog(sel, edits...)
		return
	}
	d.observe()
	h := &d.hist
	h.grouped(histStep{snap: h.snap, sel: d.sel})
	sortEdits(edits)
	before := h.snap.Len()
	for _, e := range edits {
		d.buf.Replace(e.start, e.end, e.text)
	}
	h.snap, h.typing = d.buf.Snapshot(), false
	d.closers = d.closers[:0]
	n := h.snap.Len()
	sel = [2]int{min(max(sel[0], 0), n), min(max(sel[1], 0), n)}
	d.selectRange(sel[0], sel[1], false)
	d.sel = sel
	if len(edits) == 1 && edits[0].start <= 0 && edits[0].end >= before {
		edits = nil // the whole text, as reloading it: the offsets stay
	}
	moved := func(x int) int {
		for _, e := range edits {
			x = movedBy(x, e.start, e.end, utf8.RuneCountInString(e.text))
		}
		return x
	}
	d.follow(moved)
	d.moveMarks(moved)
	d.foldsFollow(edits, moved, h.snap)
	if len(edits) > 0 {
		d.changed = min(edits[len(edits)-1].start, n)
	}
}

// editLog is edit for a document in log mode: the edits are made in the
// window's runes and in the text as a whole at once, so that a save
// writes what the window shows.
func (d *editor) editLog(sel [2]int, edits ...textEdit) {
	d.observe()
	h := &d.hist
	l := d.log
	sortEdits(edits)
	var undos []logEdit
	for _, e := range edits {
		off := l.win.start
		u, ok, err := l.applyEdit(off+int64(e.start), off+int64(e.end), e.text)
		if err != nil {
			// The file cannot be read to say what the edit replaces.
			d.sync, d.loadErr = syncLoadFailed, err
			return
		}
		if ok {
			undos = append(undos, u)
		}
		d.buf.Replace(e.start, e.end, e.text)
	}
	h.snap, h.typing = d.buf.Snapshot(), false
	d.closers = d.closers[:0]
	if len(undos) > 0 {
		slices.SortStableFunc(undos, func(a, b logEdit) int { return cmp.Compare(a.baseStart, b.baseStart) })
		h.push(histStep{logUndo: undos, logSel: d.document.selBase(d.sel)})
		if h.group == groupOpen {
			h.group = groupJoin
		}
	}
	n := h.snap.Len()
	sel = [2]int{min(max(sel[0], 0), n), min(max(sel[1], 0), n)}
	d.selectRange(sel[0], sel[1], false)
	d.sel = sel
	moved := func(x int) int {
		for _, e := range edits {
			x = movedBy(x, e.start, e.end, utf8.RuneCountInString(e.text))
		}
		return x
	}
	d.follow(moved)
	d.moveMarks(moved)
	if len(edits) > 0 {
		d.changed = min(edits[len(edits)-1].start, n)
	}
}

// sortEdits orders edits from the last in the text to the first, so that
// making each keeps the offsets of those before it.
func sortEdits(edits []textEdit) {
	slices.SortStableFunc(edits, func(a, b textEdit) int { return cmp.Compare(b.start, a.start) })
}

// movedBy returns where offset x of a text is once n runes take the place
// of its runes start up to end: where it was before them, after them as
// far as they grew or shrank, and at their start from inside them.
func movedBy(x, start, end, n int) int {
	switch {
	case x >= end && x > start:
		return x + n - (end - start)
	case x > start:
		return start
	}
	return x
}

// follow moves the selections of the other editors of d's document with
// an edit d made, by where the edit leaves an offset of the text before.
func (d *editor) follow(moved func(x int) int) {
	n := d.buf.Len()
	for _, o := range d.views {
		if o.hidden {
			o.hiddenAt = min(moved(o.hiddenAt), n)
		}
		if o == d {
			continue
		}
		o.sel = [2]int{min(moved(o.sel[0]), n), min(moved(o.sel[1]), n)}
		o.closers = o.closers[:0]
		o.resync = true
	}
}

// followChange is follow for an edit that is only known by the text
// before and after it, as typing is, made near offset hint.
func (d *editor) followChange(old, now ui.TextSnapshot, hint int) {
	if len(d.views) < 2 && !d.marked() && !d.hasFolds() {
		return
	}
	start, end, ok := changedRunes(old, now, hint)
	n := end - start + now.Len() - old.Len()
	moved := func(x int) int {
		if !ok {
			return x
		}
		return movedBy(x, start, end, n)
	}
	var edits []textEdit
	if ok {
		edits = []textEdit{{start, end, now.Slice(start, start+n)}}
	}
	d.foldsFollow(edits, moved, now)
	d.follow(moved)
	if ok {
		d.moveMarks(moved)
		d.changed = start
	}
}

// diffWindow is how far from where an edit was made its ends are looked
// for.
const diffWindow = 2048

// changedRunes finds the runes start up to end of old that an edit near
// offset hint replaced to make now, by the text the two share before and
// after it. It reports false for an edit that reaches further than the
// window around hint.
func changedRunes(old, now ui.TextSnapshot, hint int) (start, end int, ok bool) {
	n, m := old.Len(), now.Len()
	hint = min(max(hint, 0), n, m)
	lo, hi := max(0, hint-diffWindow), min(n, m, hint+diffWindow)
	a, b := []rune(old.Slice(lo, hi)), []rune(now.Slice(lo, hi))
	k := 0
	for k < len(a) && k < len(b) && a[k] == b[k] {
		k++
	}
	if k == 0 && lo > 0 {
		return 0, 0, false
	}
	start = lo + k
	// The texts line up again after the edit, as far apart as it grew
	// or shrank.
	delta := m - n
	to := min(n, max(start, start-delta, hint)+diffWindow)
	a, b = []rune(old.Slice(start, to)), []rune(now.Slice(start, to+delta))
	k = 0
	for k < len(a) && k < len(b) && a[len(a)-1-k] == b[len(b)-1-k] {
		k++
	}
	if k == 0 && to < n {
		return 0, 0, false
	}
	return start, to - k, true
}

// undo takes back the last step, reporting whether there was one.
func (d *editor) undo() bool {
	if d.locked() {
		return false
	}
	d.observe()
	h := &d.hist
	if len(h.undo) == 0 {
		return false
	}
	s := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	if h.group == groupJoin {
		h.group = groupOpen // what is typed next is a step of its own
	}
	if d.log != nil {
		again := histStep{logSel: d.document.selBase(d.sel)}
		undo, err := d.log.applyBase(s.logUndo)
		if err != nil {
			return false // the file cannot be read: the step stays
		}
		again.logUndo = undo
		h.redo = append(h.redo, again)
		d.restoreLog(s)
		return true
	}
	h.redo = append(h.redo, histStep{snap: d.buf.Snapshot(), sel: d.sel})
	d.restore(s)
	return true
}

// redo makes the step undone last again.
func (d *editor) redo() bool {
	if d.locked() {
		return false
	}
	d.observe()
	h := &d.hist
	if len(h.redo) == 0 {
		return false
	}
	s := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	if h.group == groupJoin {
		h.group = groupOpen
	}
	if d.log != nil {
		again := histStep{logSel: d.document.selBase(d.sel)}
		undo, err := d.log.applyBase(s.logUndo)
		if err != nil {
			return false // the file cannot be read: the step stays
		}
		again.logUndo = undo
		h.undo = append(h.undo, again)
		d.restoreLog(s)
		return true
	}
	h.undo = append(h.undo, histStep{snap: d.buf.Snapshot(), sel: d.sel})
	d.restore(s)
	return true
}

// restoreLog brings a log document's text back to a step's, and selects
// its selection: the window is built around it, and takes the selection
// as it lands.
func (d *editor) restoreLog(s histStep) {
	l := d.log
	edits := l.editsCopy()
	n := l.runeLen()
	sel := [2]int64{curOf(edits, s.logSel[0]), curOf(edits, s.logSel[1])}
	d.logSelect(min(max(sel[0], 0), n), min(max(sel[1], 0), n))
	d.hist.typing = false
	d.closers = d.closers[:0]
}

// restore brings back the text and the selection of a step. The text as
// it was on disk is clean again.
func (d *editor) restore(s histStep) {
	old := d.buf.Snapshot()
	d.buf.Restore(s.snap)
	if s.snap == d.savedSnap {
		d.markSaved()
	}
	d.hist.snap, d.hist.typing = d.buf.Snapshot(), false
	d.closers = d.closers[:0]
	n := s.snap.Len()
	d.selectRange(min(s.sel[0], n), min(s.sel[1], n), false)
	d.sel = [2]int{min(s.sel[0], n), min(s.sel[1], n)}
	d.measureWidest()
	d.followChange(old, d.hist.snap, min(s.sel[0], s.sel[1]))
}

// markSaved takes the text as it is for the one on disk.
func (d *document) markSaved() {
	d.savedSnap = d.buf.Snapshot()
	d.saved = d.savedSnap.Version()
}
