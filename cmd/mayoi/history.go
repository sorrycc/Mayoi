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
	// cursors are the editor's other selections then (multicursor.go).
	cursors []cursor
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
	// Typed with several selections: the text area typed at one, which
	// the others wait for while an input method composes (multicursor.go).
	if len(t.cursors) > 0 && t.holdTyping(h.snap, snap) {
		return
	}
	now := time.Now()
	old, sel := h.snap, t.sel
	h.pushTyping(histStep{h.snap, t.sel, slices.Clone(t.cursors)}, now)
	h.snap, h.typing, h.at = snap, true, now
	if len(t.cursors) > 0 && t.typeAtAll(old, snap, sel) {
		return
	}
	// Brackets and quotes typed close, in the same step.
	if t.autoClose(old, snap, sel) {
		h.snap = d.buf.Snapshot()
	}
	t.keepClosers(h.snap)
	t.followChange(old, h.snap, min(sel[0], sel[1]))
}

// pushTyping makes the step of undo for typing at now: a new one, unless
// it joins the typing before it or the group's.
func (h *history) pushTyping(s histStep, now time.Time) {
	switch {
	case h.group != groupNone:
		h.grouped(s)
	case !h.typing || now.Sub(h.at) > typingPause || len(h.undo) == 0:
		h.push(s)
	default:
		clear(h.redo)
		h.redo = h.redo[:0]
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
	d.observe()
	h := &d.hist
	h.grouped(histStep{h.snap, d.sel, slices.Clone(d.cursors)})
	d.applyEdits(sel, edits)
}

// applyEdits makes edits, which must not overlap, and selects sel, after
// the step of undo they belong to was made. The other selections of the
// editor, of its other views, its marks and its folds move with them.
func (d *editor) applyEdits(sel [2]int, edits []textEdit) {
	h := &d.hist
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
	moved := movedByEdits(edits)
	d.cursors = d.keepCursors(moved)
	d.follow(moved)
	d.moveMarks(moved)
	d.foldsFollow(edits, moved, h.snap)
	if len(edits) > 0 {
		d.changed = min(edits[len(edits)-1].start, n)
	}
}

// movedByEdits returns where an offset of the text before edits, sorted
// from the last, is after them.
func movedByEdits(edits []textEdit) func(x int) int {
	return func(x int) int {
		for _, e := range edits {
			x = movedBy(x, e.start, e.end, utf8.RuneCountInString(e.text))
		}
		return x
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
		if o.liveCell.on {
			// The table of the cell edited in place, by its header.
			o.liveCell.at = min(moved(o.liveCell.at), n)
		}
		if o.hidden {
			o.hiddenAt = min(moved(o.hiddenAt), n)
		}
		if o == d {
			continue
		}
		o.sel = [2]int{min(moved(o.sel[0]), n), min(moved(o.sel[1]), n)}
		o.cursors = o.keepCursors(moved)
		o.closers = o.closers[:0]
		o.resync = true
	}
}

// followChange is follow for an edit that is only known by the text
// before and after it, as typing is, made near offset hint.
func (d *editor) followChange(old, now ui.TextSnapshot, hint int) {
	if len(d.views) < 2 && !d.marked() && !d.hasFolds() && !d.liveCell.on {
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
	h.redo = append(h.redo, histStep{d.buf.Snapshot(), d.sel, slices.Clone(d.cursors)})
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
	h.undo = append(h.undo, histStep{d.buf.Snapshot(), d.sel, slices.Clone(d.cursors)})
	d.restore(s)
	return true
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
	// The other selections come back too: the frame drops them where
	// the editor takes only one (multiOK).
	d.restoreCursors(s.cursors)
	d.measureWidest()
	d.followChange(old, d.hist.snap, min(s.sel[0], s.sel[1]))
}

// markSaved takes the text as it is for the one on disk.
func (d *document) markSaved() {
	d.savedSnap = d.buf.Snapshot()
	d.saved = d.savedSnap.Version()
}
