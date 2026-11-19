package main

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/width"

	"mayoi/internal/highlight"
)

// An editor has one selection its text area holds, d.sel, with the
// area's caret, input methods and accessibility, and may have more,
// d.cursors: carets and ranges the editor keeps, paints and edits with
// it. d.sel is the primary, the one added last. Typing goes to the area,
// at d.sel; the editor finds what it typed there and types it at all of
// them, in the same step of undo (typeAtAll). The keys that move and
// delete are taken for all of them (multiKey); what else edits goes back
// to one selection first. Vim mode and Live Preview keep one (multiOK).

// cursor is a selection: anchor up to caret, in runes, and the display
// column moves up and down keep, -1 for none.
type cursor struct {
	anchor, caret int
	goal          int
}

func (s cursor) start() int  { return min(s.anchor, s.caret) }
func (s cursor) end() int    { return max(s.anchor, s.caret) }
func (s cursor) empty() bool { return s.anchor == s.caret }

func caretSel(at int) cursor { return cursor{at, at, -1} }

// maxSpans is how many selections an editor keeps at most, as Select All
// Occurrences makes them: typing at all of them stays quick.
const maxSpans = 10000

// columnSel is a rectangle a column selection grows by its keys, from
// line, col to toLine, toCol, while the selections are those it made
// (made).
type columnSel struct {
	line, col, toLine, toCol int
	made                     spansMark
}

// addRun is the carets Add Cursor Above or Below added in a row, going
// dir, which the other way takes back, while the selections are those
// they made.
type addRun struct {
	dir  int
	n    int
	made spansMark
}

// spansMark tells whether the selections are still those a command made.
type spansMark struct {
	version uint64
	sel     [2]int
	n       int
}

func (d *editor) mark() spansMark {
	return spansMark{d.buf.Snapshot().Version(), d.sel, len(d.cursors)}
}

// multiDrag is a press that adds a caret, with Alt, or selects a column,
// with Shift and Alt or the middle button, as the pointer moves.
type multiDrag struct {
	column        bool
	anchor        int // where an added caret's selection starts
	line, col     int // where a column starts
	toLine, toCol int
	to            int
	// queued is set while a move waits for the next frame to follow it,
	// done once the button is let go.
	queued, done bool
}

// multiOK reports whether the editor takes several selections: not in
// vim mode, Live Preview or a preview, which have one caret of their own.
func (a *app) multiOK(d *editor) bool {
	return d != nil && !a.prefs().Vim && !a.livePreview(d) && d.preview == nil
}

// spans returns the selections sorted by their starts, the primary
// among them at index p.
func (d *editor) spans() (all []cursor, p int) {
	n := d.buf.Len()
	ps := cursor{min(d.sel[0], n), min(d.sel[1], n), -1}
	if d.goalSel == d.sel {
		ps.goal = d.goal
	}
	all = make([]cursor, 0, len(d.cursors)+1)
	p = sort.Search(len(d.cursors), func(i int) bool { return d.cursors[i].start() > ps.start() })
	all = append(all, d.cursors[:p]...)
	all = append(all, ps)
	all = append(all, d.cursors[p:]...)
	for i := range all {
		all[i].anchor, all[i].caret = min(all[i].anchor, n), min(all[i].caret, n)
	}
	return all, p
}

// setSpans makes all the selections, all[p] the primary, as the next
// frame shows them: the view follows the primary as it follows typing,
// or goes to it as to a match with jump.
func (d *editor) setSpans(all []cursor, p int, jump bool) {
	n := d.buf.Len()
	for i := range all {
		all[i].anchor, all[i].caret = min(max(all[i].anchor, 0), n), min(max(all[i].caret, 0), n)
	}
	all, p = mergeSpans(all, p)
	ps := all[p]
	if len(all) == 1 {
		d.cursors = nil
	} else {
		d.cursors = make([]cursor, 0, len(all)-1)
		d.cursors = append(append(d.cursors, all[:p]...), all[p+1:]...)
	}
	d.goal, d.goalSel = ps.goal, [2]int{ps.anchor, ps.caret}
	if jump {
		d.selectRange(ps.anchor, ps.caret, false)
		d.sel = [2]int{ps.anchor, ps.caret}
	} else {
		d.selectTyped(ps.anchor, ps.caret)
	}
}

// selectSpans makes the selections, as setSpans does, for a command
// that moves them: typing after it starts a step of undo of its own.
func (d *editor) selectSpans(all []cursor, p int, jump bool) {
	d.hist.typing = false
	d.setSpans(all, p, jump)
}

// mergeSpans sorts selections and joins those that overlap, or a caret
// and what it touches, returning where the primary, all[p], went; p may
// be -1 for none.
func mergeSpans(all []cursor, p int) ([]cursor, int) {
	order := make([]int, len(all))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		return cmp.Or(cmp.Compare(all[i].start(), all[j].start()), cmp.Compare(all[i].end(), all[j].end()))
	})
	out := make([]cursor, 0, len(all))
	prim := -1
	for _, i := range order {
		s := all[i]
		if k := len(out) - 1; k >= 0 && touches(out[k], s) {
			m := out[k]
			lo, hi := min(m.start(), s.start()), max(m.end(), s.end())
			back := m.caret < m.anchor
			if i == p {
				back = s.caret < s.anchor
				m.goal = s.goal
				prim = k
			}
			if back {
				m.anchor, m.caret = hi, lo
			} else {
				m.anchor, m.caret = lo, hi
			}
			out[k] = m
			continue
		}
		if i == p {
			prim = len(out)
		}
		out = append(out, s)
	}
	if prim < 0 {
		prim = 0
	}
	return out, prim
}

// touches reports whether b, which starts at or after a does, overlaps a,
// or touches it while one of them is a caret.
func touches(a, b cursor) bool {
	return b.start() < a.end() || b.start() == a.end() && (a.empty() || b.empty()) || b.start() == a.start()
}

// keepCursors returns the editor's other selections moved as moved says
// an edit moved the text, without those the primary now meets.
func (d *editor) keepCursors(moved func(int) int) []cursor {
	if len(d.cursors) == 0 {
		return d.cursors
	}
	n := d.buf.Len()
	out := make([]cursor, 0, len(d.cursors))
	for _, c := range d.cursors {
		out = append(out, cursor{min(moved(c.anchor), n), min(moved(c.caret), n), -1})
	}
	return d.apartFromPrimary(out)
}

// restoreCursors gives the editor the other selections an undo step kept.
func (d *editor) restoreCursors(cs []cursor) {
	d.column, d.added = nil, nil
	if len(cs) == 0 {
		d.cursors = nil
		return
	}
	n := d.buf.Len()
	out := make([]cursor, 0, len(cs))
	for _, c := range cs {
		out = append(out, cursor{min(c.anchor, n), min(c.caret, n), -1})
	}
	d.cursors = d.apartFromPrimary(out)
}

// apartFromPrimary sorts and joins selections, leaving out those that
// meet the primary; nil for none.
func (d *editor) apartFromPrimary(cs []cursor) []cursor {
	cs, _ = mergeSpans(cs, -1)
	ps := cursor{d.sel[0], d.sel[1], -1}
	cs = slices.DeleteFunc(cs, func(c cursor) bool {
		if c.start() <= ps.start() {
			return touches(c, ps)
		}
		return touches(ps, c)
	})
	if len(cs) == 0 {
		return nil
	}
	return cs
}

// collapse goes back to one selection, the primary.
func (d *editor) collapse() {
	d.cursors, d.column, d.added, d.mdrag, d.held = nil, nil, nil, nil, false
}

// columnOf returns the display column of rune i of a line: a tab goes to
// the next stop, a wide rune takes two.
func columnOf(line string, i int) int {
	col, k := 0, 0
	for _, r := range line {
		if k == i {
			break
		}
		col += runeCols(r, col)
		k++
	}
	return col
}

func runeCols(r rune, col int) int {
	switch {
	case r == '\t':
		return tabWidth() - col%tabWidth()
	case r < 0x300:
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me) || r == 0x200D || r >= 0xFE00 && r <= 0xFE0F:
		return 0
	}
	if k := width.LookupRune(r).Kind(); k == width.EastAsianWide || k == width.EastAsianFullwidth {
		return 2
	}
	return 1
}

// runeAtColumn returns the rune of a line at display column col, the
// nearer edge of a tab or a wide rune that col falls in, or the line's
// end, and whether the line reaches col.
func runeAtColumn(line string, col int) (int, bool) {
	c, k := 0, 0
	for _, r := range line {
		if c >= col {
			return k, true
		}
		w := runeCols(r, c)
		if c+w > col {
			if col-c <= c+w-col {
				return k, true
			}
			return k + 1, true
		}
		c += w
		k++
	}
	return k, c >= col
}

// lineAround returns the line of offset at, its start and its text.
func lineAround(snap ui.TextSnapshot, at int) (line, start int, text string) {
	line = snap.LineAt(min(at, snap.Len()))
	s, e := snap.LineRange(line)
	return line, s, snap.Slice(s, e)
}

// joins reports whether r goes with the rune before it, prev, as one
// character: a combining mark, a variation selector, a skin tone or a
// joiner and what it joins.
func joins(prev, r rune) bool {
	return r == 0x200D || prev == 0x200D || unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) ||
		r >= 0xFE00 && r <= 0xFE0F || r >= 0xE0100 && r <= 0xE01EF || r >= 0x1F3FB && r <= 0x1F3FF || r >= 0xE0020 && r <= 0xE007F
}

func regional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// charStep returns the offset one character from at, going dir: past a
// rune and the marks and joined runes that go with it, or a flag's pair.
func charStep(snap ui.TextSnapshot, at, dir int) int {
	n := snap.Len()
	if dir > 0 {
		if at >= n {
			return n
		}
		rs := []rune(snap.Slice(at, min(n, at+32)))
		i := 1
		if regional(rs[0]) && len(rs) > 1 && regional(rs[1]) {
			return at + 2
		}
		for i < len(rs) && joins(rs[i-1], rs[i]) {
			i++
		}
		return at + i
	}
	if at <= 0 {
		return 0
	}
	lo := max(0, at-32)
	rs := []rune(snap.Slice(lo, at))
	i := len(rs) - 1
	if regional(rs[i]) {
		k := 0
		for j := i; j >= 0 && regional(rs[j]); j-- {
			k++
		}
		if k%2 == 0 {
			return lo + i - 1
		}
		return lo + i
	}
	for i > 0 && joins(rs[i-1], rs[i]) {
		i--
	}
	return lo + i
}

// wordClass is what kind of rune r is to a move by words: a space, of a
// word, or other.
func wordClass(r rune) int {
	switch {
	case r == ' ' || r == '\t':
		return 0
	case isWordRune(r):
		return 1
	}
	return 2
}

// wordStep returns the offset a move by a word from at goes to, going
// dir: past the spaces, then the run of runes of a kind; from an end of
// a line, to the other line.
func wordStep(snap ui.TextSnapshot, at, dir int) int {
	_, s, text := lineAround(snap, at)
	e := s + utf8.RuneCountInString(text)
	if dir < 0 {
		if at <= s {
			return max(at-1, 0)
		}
		rs := []rune(snap.Slice(s, at))
		i := len(rs)
		for i > 0 && wordClass(rs[i-1]) == 0 {
			i--
		}
		if i > 0 {
			c := wordClass(rs[i-1])
			for i > 0 && wordClass(rs[i-1]) == c {
				i--
			}
		}
		return s + i
	}
	if at >= e {
		return min(at+1, snap.Len())
	}
	rs := []rune(snap.Slice(at, e))
	i := 0
	for i < len(rs) && wordClass(rs[i]) == 0 {
		i++
	}
	if i < len(rs) {
		c := wordClass(rs[i])
		for i < len(rs) && wordClass(rs[i]) == c {
			i++
		}
	}
	return at + i
}

// wordAtCaret returns the word at or just before offset at, empty when
// there is none.
func wordAtCaret(snap ui.TextSnapshot, at int) (int, int) {
	_, s, text := lineAround(snap, at)
	rs := []rune(text)
	i := at - s
	a, z := i, i
	for a > 0 && isWordRune(rs[a-1]) {
		a--
	}
	for z < len(rs) && isWordRune(rs[z]) {
		z++
	}
	return s + a, s + z
}

// moveKind is how a key moves a selection.
type moveKind uint8

const (
	moveChar moveKind = iota
	moveWord
	moveLine // to the line's first character or its start, or its end
	moveVertical
)

// moveSpans moves each selection's caret by how, going dir, carrying its
// anchor along unless extend; a selection a key moves off collapses to
// its edge that way first, as one does.
func (a *app) moveSpans(d *editor, how moveKind, dir int, extend bool) {
	snap := d.buf.Snapshot()
	n := snap.Len()
	runs := a.hiddenRuns(d, snap)
	all, p := d.spans()
	for i, s := range all {
		if !extend && !s.empty() && how == moveChar {
			at := s.start()
			if dir > 0 {
				at = s.end()
			}
			all[i] = caretSel(at)
			continue
		}
		to, goal := s.caret, -1
		switch how {
		case moveChar:
			to = charStep(snap, s.caret, dir)
		case moveWord:
			to = wordStep(snap, s.caret, dir)
		case moveLine:
			_, ls, text := lineAround(snap, s.caret)
			if dir > 0 {
				to = ls + utf8.RuneCountInString(text)
			} else {
				first := ls + utf8.RuneCountInString(leading(text))
				to = first
				if s.caret == first {
					to = ls
				}
			}
		case moveVertical:
			line, ls, text := lineAround(snap, s.caret)
			goal = s.goal
			if goal < 0 {
				goal = columnOf(text, s.caret-ls)
			}
			next := line + dir
			if r, ok := runAt(runs, next); ok {
				next = r.to + 1
				if dir < 0 {
					next = r.from - 1
				}
			}
			switch {
			case next < 0:
				to = 0
			case next >= snap.LineCount():
				to = n
			default:
				ns, ne := snap.LineRange(next)
				k, _ := runeAtColumn(snap.Slice(ns, ne), goal)
				to = ns + k
			}
		}
		if extend {
			all[i] = cursor{s.anchor, to, goal}
		} else {
			all[i] = cursor{to, to, goal}
		}
	}
	d.hist.typing = false
	d.column, d.added = nil, nil
	d.selectSpans(all, p, false)
}

// spanEdit is what an edit at a selection replaces, runes start up to
// end, with text, and the selection after it, as offsets from its start
// in the text after; closer marks a closing bracket put after the caret.
type spanEdit struct {
	start, end    int
	text          string
	anchor, caret int
	closer        bool
	// stepped is set when the caret steps over a closing bracket the
	// editor put at start, which goes from the closers.
	stepped bool
}

// stepping is how an edit of all the selections makes its step of undo.
type stepping uint8

const (
	stepMade   stepping = iota // made already, as typing's
	stepTyping                 // joins typing, as deleting does
	stepOwn                    // a step of its own, as pasting
)

// editSpans edits the text at each selection as fn says, together, and
// selects what fn leaves. Edits that overlap are cut to where the one
// before ends.
func (d *editor) editSpans(all []cursor, p int, step stepping, fn func(s cursor) spanEdit) {
	if d.locked() {
		return
	}
	h := &d.hist
	now := time.Now()
	switch step {
	case stepTyping:
		d.observe()
		h.pushTyping(histStep{h.snap, d.sel, slices.Clone(d.cursors)}, now)
	case stepOwn:
		d.observe()
		h.grouped(histStep{h.snap, d.sel, slices.Clone(d.cursors)})
	}
	out := make([]spanEdit, len(all))
	for i, s := range all {
		out[i] = fn(s)
	}
	edits := make([]textEdit, 0, len(all))
	end := 0
	for i := range out {
		e := &out[i]
		if e.start < end {
			e.start = end
			e.end = max(e.end, e.start)
		}
		end = max(end, e.end)
		if e.start != e.end || e.text != "" {
			edits = append(edits, textEdit{e.start, e.end, e.text})
		}
	}
	after := make([]cursor, len(all))
	var closers []int
	stepped := map[int]bool{}
	delta := 0
	for i, e := range out {
		at := e.start + delta
		after[i] = cursor{at + e.anchor, at + e.caret, -1}
		if e.closer {
			closers = append(closers, at+e.caret)
		}
		if e.stepped {
			stepped[e.start] = true
		}
		delta += utf8.RuneCountInString(e.text) - (e.end - e.start)
	}
	old := slices.DeleteFunc(slices.Clone(d.closers), func(c int) bool { return stepped[c] })
	// The other selections are made anew: moving them with the edits
	// would take as long as there are of them, for each.
	d.cursors, d.column, d.added = nil, nil, nil
	d.applyEdits([2]int{after[p].anchor, after[p].caret}, edits)
	if step != stepOwn {
		h.typing, h.at = true, now
	}
	// The closing brackets put before stay after the caret, as the edits
	// moved them: text put in where one is goes before it.
	for _, c := range old {
		closers = append(closers, closerMoved(edits, c))
	}
	slices.Sort(closers)
	d.closers = slices.Compact(closers)
	d.keepClosers(h.snap)
	d.setSpans(after, p, false)
}

// closerMoved returns where a closing bracket at offset c is after edits,
// which text put in at c pushes along.
func closerMoved(edits []textEdit, c int) int {
	x := c
	for _, e := range edits {
		switch {
		case c >= e.end && (c > e.start || e.start == e.end):
			x += utf8.RuneCountInString(e.text) - (e.end - e.start)
		case c > e.start:
			x = e.start
		}
	}
	return x
}

// holdTyping reports whether what the text area typed at the primary,
// from text old to now, waits before the others type it: while an input
// method composes there, and for a frame when it took out the selection,
// as it does to compose in its place.
func (d *editor) holdTyping(old, now ui.TextSnapshot) bool {
	if d.composing {
		if !d.held {
			d.held, d.heldSel = true, d.sel
		}
		return true
	}
	start, end, ins, ok := typedAt(old, now, d.sel)
	// An edit that changed nothing, as an input method's that starts at
	// a caret: editing the text now would end the composition.
	if ok && ins == "" && start == end {
		return true
	}
	if ok && ins == "" && !d.held && start < end && start == min(d.sel[0], d.sel[1]) && end == max(d.sel[0], d.sel[1]) {
		d.held, d.heldSel = true, d.sel
		return true
	}
	return false
}

// typedAt finds what typing at selection sel replaced to make text now
// of old: the selection, unless the edit reached around it, as an input
// method that replaces what was typed before the caret does.
func typedAt(old, now ui.TextSnapshot, sel [2]int) (start, end int, ins string, ok bool) {
	a, b := min(sel[0], sel[1]), max(sel[0], sel[1])
	n0 := old.Len()
	if b > n0 {
		return 0, 0, "", false
	}
	start, end, ok = changedRunes(old, now, a)
	if !ok {
		return 0, 0, "", false
	}
	delta := now.Len() - n0
	same := func(from, to, shift int) bool {
		return from >= to || old.Slice(from, to) == now.Slice(from+shift, to+shift)
	}
	if b+delta >= a && same(start, a, 0) && same(b, end, delta) {
		return a, b, now.Slice(a, b+delta), true
	}
	lo, hi := min(start, a), max(end, b)
	return lo, hi, now.Slice(lo, hi+delta), true
}

// typeAtAll types at every selection what the text area typed at the
// primary, sel then, making text now of old, in the step of undo made
// for it. It reports false, back to one selection, when it cannot tell
// what was typed.
func (d *editor) typeAtAll(old, now ui.TextSnapshot, sel [2]int) bool {
	if d.held {
		sel = d.heldSel
	}
	d.held = false
	start, end, ins, ok := typedAt(old, now, sel)
	if !ok || d.locked() {
		d.collapse()
		return false
	}
	a, b := min(sel[0], sel[1]), max(sel[0], sel[1])
	left, right := a-start, end-b
	d.sel = sel
	all, p := d.spans()
	d.buf.Restore(old)
	d.hist.snap = d.buf.Snapshot()
	d.typeSpans(all, p, left, right, ins, stepMade)
	return true
}

// typeSpans types ins at every selection, taking left runes before it and
// right after it too, as the brackets close at a caret, go around a
// selection and are stepped over.
func (d *editor) typeSpans(all []cursor, p int, left, right int, ins string, step stepping) {
	snap := d.buf.Snapshot()
	n := snap.Len()
	r, size := utf8.DecodeRuneInString(ins)
	pairs := left == 0 && right == 0 && size == len(ins) && size > 0 && d.hl != nil && d.hl.Lexer() != nil
	md := pairs && strings.EqualFold(highlight.Name(d.hl.Lexer()), "markdown")
	closers := slices.Clone(d.closers)
	k := utf8.RuneCountInString(ins)
	d.editSpans(all, p, step, func(s cursor) spanEdit {
		a, b := s.start(), s.end()
		if pairs && a == b {
			if slices.Contains(closers, a) && runeAt(snap, a) == r {
				return spanEdit{start: a, end: a, anchor: 1, caret: 1, stepped: true}
			}
			var prev rune
			if a > 0 {
				prev = runeAt(snap, a-1)
			}
			if c, ok := closeFor(d, prev, runeAt(snap, a), r); ok {
				return spanEdit{start: a, end: a, text: string(r) + string(c), anchor: 1, caret: 1, closer: true}
			}
		}
		if pairs && a < b {
			c, ok := autoPairs[r]
			if !ok && md && strings.ContainsRune(markdownWraps, r) {
				c, ok = r, true
			}
			if ok && (snap.LineAt(a) == snap.LineAt(b) || r == '(' || r == '[' || r == '{') {
				inner := snap.Slice(a, b)
				m := utf8.RuneCountInString(inner)
				return spanEdit{start: a, end: b, text: string(r) + inner + string(c), anchor: 1, caret: 1 + m}
			}
		}
		from, to := max(a-left, 0), min(b+right, n)
		return spanEdit{start: from, end: to, text: ins, anchor: k, caret: k}
	})
}

// deleteSpans deletes at every selection: a selection, or from each caret
// to where how moves it, going dir. Deleting the opening bracket of a
// pair the editor closed deletes its closing one.
func (a *app) deleteSpans(d *editor, how moveKind, dir int) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	closers := slices.Clone(d.closers)
	d.editSpans(all, p, stepTyping, func(s cursor) spanEdit {
		if !s.empty() {
			return spanEdit{start: s.start(), end: s.end()}
		}
		at := s.caret
		var to int
		switch how {
		case moveWord:
			to = wordStep(snap, at, dir)
		case moveLine:
			_, ls, text := lineAround(snap, at)
			to = ls + utf8.RuneCountInString(text)
			if dir < 0 {
				to = ls
			}
			if to == at {
				to = charStep(snap, at, dir)
			}
		default:
			to = charStep(snap, at, dir)
		}
		if dir < 0 && how == moveChar && to == at-1 && slices.Contains(closers, at) {
			if c, ok := autoPairs[runeAt(snap, at-1)]; ok && runeAt(snap, at) == c {
				return spanEdit{start: at - 1, end: at + 1}
			}
		}
		return spanEdit{start: min(at, to), end: max(at, to)}
	})
}

// newlineSpans breaks the line at every selection, as Enter does at one.
func (a *app) newlineSpans(d *editor) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	d.editSpans(all, p, stepOwn, func(s cursor) spanEdit {
		start, end, text, caret := newlineAt(d, snap, s.start(), s.end())
		return spanEdit{start: start, end: end, text: text, anchor: caret - start, caret: caret - start}
	})
}

// tabSpans indents at every caret, or the lines of every selection when
// one spans lines or outdent is set.
func (a *app) tabSpans(d *editor, outdent bool) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	several := slices.ContainsFunc(all, func(s cursor) bool { return snap.LineAt(s.start()) != snap.LineAt(s.end()) })
	if !outdent && !several {
		d.editSpans(all, p, stepOwn, func(s cursor) spanEdit {
			text := tabAt(d, snap, s.start())
			k := utf8.RuneCountInString(text)
			return spanEdit{start: s.start(), end: s.end(), text: text, anchor: k, caret: k}
		})
		return
	}
	if d.locked() {
		return
	}
	lines := map[int]bool{}
	for _, s := range all {
		first, last := lineSpan(snap, [2]int{s.anchor, s.caret})
		for l := first; l <= last; l++ {
			lines[l] = true
		}
	}
	order := make([]int, 0, len(lines))
	for l := range lines {
		order = append(order, l)
	}
	slices.Sort(order)
	unit := d.indent.unit()
	var edits []textEdit
	for k := len(order) - 1; k >= 0; k-- {
		s, e := snap.LineRange(order[k])
		line := snap.Slice(s, e)
		if outdent {
			if w := outdentWidth(line, d.indent); w > 0 {
				edits = append(edits, textEdit{s, s + w, ""})
			}
		} else if strings.TrimSpace(line) != "" || len(order) == 1 {
			edits = append(edits, textEdit{s, s, unit})
		}
	}
	if len(edits) == 0 {
		return
	}
	moved := movedByEdits(edits)
	k := utf8.RuneCountInString(unit)
	starts := map[int]bool{}
	for _, e := range edits {
		if e.text != "" {
			starts[e.start] = true
		}
	}
	after := make([]cursor, len(all))
	for i, s := range all {
		ns := cursor{moved(s.anchor), moved(s.caret), -1}
		if s.empty() && starts[s.caret] {
			ns.anchor += k
			ns.caret += k
		}
		after[i] = ns
	}
	d.cursors = nil
	d.edit([2]int{after[p].anchor, after[p].caret}, edits...)
	d.setSpans(after, p, false)
}

// spansText is what copying the selections takes: their texts, a line
// each, or the lines of the carets, each with its newline, when all are
// carets.
func spansText(snap ui.TextSnapshot, all []cursor) (text string, lines bool) {
	var b strings.Builder
	if !slices.ContainsFunc(all, func(s cursor) bool { return !s.empty() }) {
		last := -1
		for _, s := range all {
			l := snap.LineAt(s.caret)
			if l == last {
				continue
			}
			last = l
			ls, le := snap.LineRange(l)
			b.WriteString(snap.Slice(ls, le))
			b.WriteByte('\n')
		}
		return b.String(), true
	}
	first := true
	for _, s := range all {
		if s.empty() {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(snap.Slice(s.start(), s.end()))
	}
	return b.String(), false
}

// copySpans copies the selections.
func (a *app) copySpans(d *editor) {
	all, _ := d.spans()
	text, _ := spansText(d.buf.Snapshot(), all)
	a.services.WriteClipboard(text)
}

// cutSpans cuts the selections, or the lines of the carets when all are
// carets, in one step of undo.
func (a *app) cutSpans(d *editor) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	text, lines := spansText(snap, all)
	if d.locked() {
		return
	}
	a.services.WriteClipboard(text)
	if !lines {
		d.editSpans(all, p, stepOwn, func(s cursor) spanEdit { return spanEdit{start: s.start(), end: s.end()} })
		return
	}
	n := snap.Len()
	var edits []textEdit
	last := -1
	for _, s := range all {
		l := snap.LineAt(s.caret)
		if l == last {
			continue
		}
		last = l
		ls, le := snap.LineRange(l)
		switch {
		case le < n:
			le++
		case ls > 0:
			ls--
		}
		if k := len(edits) - 1; k >= 0 && edits[k].end >= ls {
			edits[k].end = le
			continue
		}
		edits = append(edits, textEdit{ls, le, ""})
	}
	slices.Reverse(edits)
	moved := movedByEdits(edits)
	after := make([]cursor, len(all))
	for i, s := range all {
		at := moved(s.caret)
		after[i] = caretSel(at)
	}
	d.cursors = nil
	d.edit([2]int{after[p].anchor, after[p].caret}, edits...)
	d.setSpans(after, p, false)
}

// pasteSpans pastes text at every selection: a line of it at each, when
// it has as many lines as there are selections, or all of it at each.
func (a *app) pasteSpans(d *editor, text string) {
	all, p := d.spans()
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) != len(all) {
		parts = nil
	}
	i := 0
	d.editSpans(all, p, stepOwn, func(s cursor) spanEdit {
		t := text
		if parts != nil {
			t = parts[i]
		}
		i++
		k := utf8.RuneCountInString(t)
		return spanEdit{start: s.start(), end: s.end(), text: t, anchor: k, caret: k}
	})
}

// multiKey takes a key pressed while the editor has several selections:
// those that move and delete act at all of them; Escape goes back to the
// primary, as does any other key that edits but text typed.
func (a *app) multiKey(d *editor, ev ui.InputEvent) bool {
	m, k := ev.Mods, ev.Key
	word := ui.Ctrl
	if macKeys() {
		word = ui.Alt
	}
	base, extend := m&^ui.Shift, m&ui.Shift != 0
	queue := func(fn func(d *editor)) bool {
		d.cmds = append(d.cmds, fn)
		a.invalidate()
		return true
	}
	lineKey := macKeys() && base == ui.Super
	switch {
	case k == ui.KeyEscape && m == 0:
		return queue(func(d *editor) { d.collapse(); d.hist.typing = false; d.selectTyped(d.sel[0], d.sel[1]) })
	case k == ui.KeyLeft || k == ui.KeyRight:
		dir := 1
		if k == ui.KeyLeft {
			dir = -1
		}
		switch {
		case base == 0:
			return queue(func(d *editor) { a.moveSpans(d, moveChar, dir, extend) })
		case base == word:
			return queue(func(d *editor) { a.moveSpans(d, moveWord, dir, extend) })
		case lineKey:
			return queue(func(d *editor) { a.moveSpans(d, moveLine, dir, extend) })
		}
	case (k == ui.KeyUp || k == ui.KeyDown) && base == 0:
		dir := 1
		if k == ui.KeyUp {
			dir = -1
		}
		return queue(func(d *editor) { a.moveSpans(d, moveVertical, dir, extend) })
	case (k == ui.KeyHome || k == ui.KeyEnd) && base == 0:
		dir := 1
		if k == ui.KeyHome {
			dir = -1
		}
		return queue(func(d *editor) { a.moveSpans(d, moveLine, dir, extend) })
	case k == ui.KeyBackspace || k == ui.KeyDelete:
		dir := 1
		if k == ui.KeyBackspace {
			dir = -1
		}
		switch {
		case m == 0:
			return queue(func(d *editor) { a.deleteSpans(d, moveChar, dir) })
		case m == word:
			return queue(func(d *editor) { a.deleteSpans(d, moveWord, dir) })
		case macKeys() && m == ui.Super:
			return queue(func(d *editor) { a.deleteSpans(d, moveLine, dir) })
		}
	case k == ui.KeyEnter && m == 0:
		return queue(a.newlineSpans)
	case k == ui.KeyTab && (m == 0 || m == ui.Shift):
		return queue(func(d *editor) { a.tabSpans(d, extend) })
	case k == ui.KeyC && m == ui.Cmd:
		return queue(a.copySpans)
	case (k == ui.KeyX || k == ui.KeyV || k == ui.KeyZ || k == ui.KeyY) && m&ui.Cmd != 0:
		return false // cut, paste and undo take all of them
	case textKey(k) && base&^ui.Alt == 0:
		return false // typed, at all of them
	}
	// What else the editor takes acts at one selection.
	d.collapse()
	return false
}

// textKey reports whether k types text, rather than moving or editing.
func textKey(k ui.Key) bool {
	switch k {
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd, ui.KeyPageUp, ui.KeyPageDown,
		ui.KeyEscape, ui.KeyTab, ui.KeyEnter, ui.KeyBackspace, ui.KeyDelete:
		return false
	}
	return !(k >= ui.KeyF1 && k <= ui.KeyF12)
}

// multiEditor returns the editor a command for several selections acts
// on: the one shown, unless a terminal has the keyboard.
func (a *app) multiCmd(fn func(d *editor)) func() {
	return func() {
		if a.keyFocus == placeTerminal {
			return
		}
		d := a.textEditor()
		if !a.multiOK(d) {
			return
		}
		a.queueEdit(d, fn)
	}
}

// addCursor adds a caret on the line above the top selection, or below
// the bottom one, at the primary's column, and makes it the primary;
// after some were added going one way, going the other takes the last
// back.
func (a *app) addCursor(d *editor, dir int) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	if r := d.added; r != nil && r.made == d.mark() && r.dir == -dir && r.n > 0 && len(all) > 1 {
		// The last added is the primary, at the edge it went to.
		all = slices.Delete(all, p, p+1)
		np := 0
		if dir < 0 {
			np = len(all) - 1
		}
		d.selectSpans(all, np, false)
		d.column = nil
		d.added = &addRun{dir: r.dir, n: r.n - 1, made: d.mark()}
		if r.n == 1 {
			d.added = nil
		}
		return
	}
	edge := all[0]
	if dir > 0 {
		edge = all[len(all)-1]
	}
	ps := all[p]
	_, ls, text := lineAround(snap, ps.caret)
	goal := ps.goal
	if goal < 0 {
		goal = columnOf(text, ps.caret-ls)
	}
	line := snap.LineAt(edge.caret) + dir
	if r, ok := runAt(a.hiddenRuns(d, snap), line); ok {
		line = r.to + 1
		if dir < 0 {
			line = r.from - 1
		}
	}
	if line < 0 || line >= snap.LineCount() {
		return
	}
	s, e := snap.LineRange(line)
	k, _ := runeAtColumn(snap.Slice(s, e), goal)
	all[p].goal = goal
	all = append(all, cursor{s + k, s + k, goal})
	n := 0
	if r := d.added; r != nil && r.made == d.mark() && r.dir == dir {
		n = r.n
	}
	d.selectSpans(all, len(all)-1, false)
	d.column = nil
	d.added = &addRun{dir: dir, n: n + 1, made: d.mark()}
}

// columnSelect grows the column selection, or starts one at the primary,
// by dl lines and dc columns.
func (a *app) columnSelect(d *editor, dl, dc int) {
	snap := d.buf.Snapshot()
	cs := d.column
	if cs == nil || cs.made != d.mark() {
		al, as, at := lineAround(snap, d.sel[0])
		cl, cs0, ct := lineAround(snap, d.sel[1])
		cs = &columnSel{line: al, col: columnOf(at, d.sel[0]-as), toLine: cl, toCol: columnOf(ct, d.sel[1]-cs0)}
	}
	line := cs.toLine + dl
	if r, ok := runAt(a.hiddenRuns(d, snap), line); ok && dl != 0 {
		line = r.to + 1
		if dl < 0 {
			line = r.from - 1
		}
	}
	line = min(max(line, 0), snap.LineCount()-1)
	a.columnTo(d, cs.line, cs.col, line, max(0, cs.toCol+dc))
}

// columnTo selects the column from line l0, column c0 to line l1, column
// c1: on each line between, those columns of it, the primary on l1.
// Lines that end before both columns are left out of a column of some
// width, as are those folded.
func (a *app) columnTo(d *editor, l0, c0, l1, c1 int) {
	snap := d.buf.Snapshot()
	runs := a.hiddenRuns(d, snap)
	lo, hi := min(l0, l1), max(l0, l1)
	out := make([]cursor, 0, min(hi-lo+1, maxSpans))
	p := -1
	for l := lo; l <= hi && len(out) < maxSpans; l++ {
		if _, ok := runAt(runs, l); ok {
			continue
		}
		s, e := snap.LineRange(l)
		text := snap.Slice(s, e)
		ia, okA := runeAtColumn(text, c0)
		ic, okC := runeAtColumn(text, c1)
		if c0 != c1 && !okA && !okC && l != l1 {
			continue
		}
		if l == l1 {
			p = len(out)
		}
		out = append(out, cursor{s + ia, s + ic, c1})
	}
	if len(out) == 0 {
		return
	}
	if p < 0 {
		p = len(out) - 1
	}
	d.added = nil
	d.selectSpans(out, p, false)
	d.column = &columnSel{l0, c0, l1, c1, d.mark()}
	d.hist.typing = false
}

// findOccurrence finds query in text, from rune at, going round to the start,
// as a whole word if whole: the runes of the first match ok takes, or -1.
func findOccurrence(snap ui.TextSnapshot, query string, at int, whole bool, ok func(start, end int) bool) int {
	if query == "" {
		return -1
	}
	text := snap.String()
	ascii := len(text) == snap.Len()
	qn := utf8.RuneCountInString(query)
	// byteAt converts rune offsets to bytes, and runeAt back, counting on
	// from where they last were, as matches come in order.
	byteOf := func(r int) int {
		if ascii {
			return r
		}
		b := 0
		for i := 0; i < r && b < len(text); i++ {
			_, size := utf8.DecodeRuneInString(text[b:])
			b += size
		}
		return b
	}
	var lastB, lastR int
	runeOf := func(b int) int {
		if ascii {
			return b
		}
		if b < lastB {
			lastB, lastR = 0, 0
		}
		lastR += utf8.RuneCountInString(text[lastB:b])
		lastB = b
		return lastR
	}
	scan := func(from, limit int) int {
		for from <= len(text) {
			i := strings.Index(text[from:], query)
			if i < 0 || from+i >= limit {
				return -1
			}
			i += from
			r := runeOf(i)
			if (!whole || wholeWordAt(text, i, i+len(query))) && ok(r, r+qn) {
				return r
			}
			_, size := utf8.DecodeRuneInString(text[i:])
			from = i + max(size, 1)
		}
		return -1
	}
	from := byteOf(at)
	if r := scan(from, len(text)+1); r >= 0 {
		return r
	}
	lastB, lastR = 0, 0
	return scan(0, from)
}

// wholeWordAt reports whether bytes a up to b of text are not inside a
// longer word.
func wholeWordAt(text string, a, b int) bool {
	if a > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:a])
		first, _ := utf8.DecodeRuneInString(text[a:])
		if isWordRune(r) && isWordRune(first) {
			return false
		}
	}
	if b < len(text) {
		r, _ := utf8.DecodeRuneInString(text[b:])
		last, _ := utf8.DecodeLastRuneInString(text[:b])
		if isWordRune(r) && isWordRune(last) {
			return false
		}
	}
	return true
}

// overlapsAny reports whether runes start up to end meet one of all,
// which are sorted.
func overlapsAny(all []cursor, start, end int) bool {
	k := sort.Search(len(all), func(i int) bool { return all[i].end() >= start })
	for ; k < len(all) && all[k].start() <= end; k++ {
		s := all[k]
		if s.start() < end && s.end() > start || s.start() == start {
			return true
		}
	}
	return false
}

// addNextOccurrence selects the word at each caret, or adds the next
// match of the primary's text after it to the selections, going round,
// as the primary: a whole word when the first was a word.
func (a *app) addNextOccurrence(d *editor) {
	snap := d.buf.Snapshot()
	all, p := d.spans()
	if all[p].empty() {
		changed := false
		for i, s := range all {
			if s.empty() {
				if w0, w1 := wordAtCaret(snap, s.caret); w0 < w1 {
					all[i] = cursor{w0, w1, -1}
					changed = true
				}
			}
		}
		if changed {
			d.matchWhole = snap.Slice(all[p].start(), all[p].end())
			d.column, d.added = nil, nil
			d.selectSpans(all, p, false)
		}
		return
	}
	if len(all) >= maxSpans {
		return
	}
	query := snap.Slice(all[p].start(), all[p].end())
	whole := d.matchWhole == query
	at := findOccurrence(snap, query, all[p].end(), whole, func(s, e int) bool { return !overlapsAny(all, s, e) })
	if at < 0 {
		return
	}
	all = append(all, cursor{at, at + utf8.RuneCountInString(query), -1})
	d.column, d.added = nil, nil
	d.selectSpans(all, len(all)-1, true)
}

// allOccurrences returns the runes of the matches of query in the text,
// apart, as whole words if whole, at most limit of them.
func allOccurrences(snap ui.TextSnapshot, query string, whole bool, limit int) []int {
	if query == "" {
		return nil
	}
	text := snap.String()
	ascii := len(text) == snap.Len()
	var out []int
	lastB, lastR := 0, 0
	for from := 0; from <= len(text) && len(out) < limit; {
		i := strings.Index(text[from:], query)
		if i < 0 {
			break
		}
		i += from
		if whole && !wholeWordAt(text, i, i+len(query)) {
			_, size := utf8.DecodeRuneInString(text[i:])
			from = i + max(size, 1)
			continue
		}
		r := i
		if !ascii {
			lastR += utf8.RuneCountInString(text[lastB:i])
			lastB, r = i, lastR
		}
		out = append(out, r)
		from = i + len(query)
	}
	return out
}

// selectAllOccurrences selects every match of the primary's text, or of
// the word at the caret, keeping the primary where it is.
func (a *app) selectAllOccurrences(d *editor) {
	snap := d.buf.Snapshot()
	ps := cursor{d.sel[0], d.sel[1], -1}
	whole := false
	if ps.empty() {
		w0, w1 := wordAtCaret(snap, ps.caret)
		if w0 == w1 {
			return
		}
		ps = cursor{w0, w1, -1}
		whole = true
	}
	query := snap.Slice(ps.start(), ps.end())
	if !whole {
		whole = d.matchWhole == query
	}
	qn := utf8.RuneCountInString(query)
	at := allOccurrences(snap, query, whole, maxSpans)
	out := make([]cursor, 0, len(at)+1)
	p := -1
	for _, i := range at {
		if i == ps.start() {
			p = len(out)
			out = append(out, ps)
			continue
		}
		out = append(out, cursor{i, i + qn, -1})
	}
	if p < 0 {
		out = append(out, ps)
		p = len(out) - 1
	}
	if whole {
		d.matchWhole = query
	}
	d.column, d.added = nil, nil
	d.selectSpans(out, p, false)
}

// pointAt returns the line at y in the editor's text area and the x along
// it, as if it did not wrap, of x there.
func (a *app) pointAt(d *editor, x, y float32) (line int, lx float32) {
	snap := d.buf.Snapshot()
	g := a.geometry(d, snap.LineCount())
	at := y + d.vscroll.Y - editorPadY
	line = min(max(g.at(at), 0), snap.LineCount()-1)
	lx = x - d.textX
	if xs := d.rows.rowStarts(line); len(xs) > 1 && a.font.lineH > 0 {
		row := min(max(int((at-g.top(line))/a.font.lineH), 0), len(xs)-1)
		lx += xs[row]
	}
	return line, max(lx, 0)
}

// runeAtX returns the rune of a line nearest x along it.
func (a *app) runeAtX(text string, x float32) int {
	f := a.font
	var at float32
	i := 0
	for _, r := range text {
		var next float32
		switch {
		case r == '\t':
			next = f.afterTab(at)
		case r < 0x80 || i > highlightRunes:
			next = at + f.charW
		default:
			next = f.widthFrom(at, string(r))
		}
		if x < (at+next)/2 {
			return i
		}
		at = next
		i++
	}
	return i
}

// multiPointer takes a press on the text that adds a caret, with Alt, or
// selects a column, with Shift and Alt or the middle button, and the
// pointer's moves as it drags them; a plain press goes back to one
// selection.
func (a *app) multiPointer(d *editor, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		d.mdrag = nil
		inText := ev.X-d.hscroll.X >= d.gutterW && ev.Y >= float32(len(d.sticky))*a.font.lineH
		add := ev.Button == 0 && ev.Mods == ui.Alt
		column := ev.Button == 0 && ev.Mods == ui.Shift|ui.Alt || ev.Button == 2 && ev.Mods&^ui.Shift == 0
		if !inText || !(add || column) || !a.multiOK(d) {
			if ev.Button == 0 && len(d.cursors) > 0 {
				d.collapse()
			}
			return false
		}
		line, lx := a.pointAt(d, ev.X, ev.Y)
		snap := d.buf.Snapshot()
		s, e := snap.LineRange(line)
		if add {
			at := s + a.runeAtX(snap.Slice(s, e), lx)
			d.mdrag = &multiDrag{anchor: at, to: at}
			d.cmds = append(d.cmds, func(d *editor) { a.toggleCaret(d, at) })
		} else {
			col := int(lx/a.font.charW + 0.5)
			d.mdrag = &multiDrag{column: true, line: line, col: col, toLine: line, toCol: col}
			d.cmds = append(d.cmds, func(d *editor) { a.columnTo(d, line, col, line, col) })
		}
		d.focusNext = true
		a.invalidate()
		return true
	case ui.InputPointerMove:
		md := d.mdrag
		if md == nil {
			return false
		}
		if ev.Button < 0 {
			d.mdrag = nil
			return false
		}
		line, lx := a.pointAt(d, ev.X, ev.Y)
		if md.column {
			md.toLine, md.toCol = line, int(lx/a.font.charW+0.5)
		} else {
			snap := d.buf.Snapshot()
			s, e := snap.LineRange(line)
			md.to = s + a.runeAtX(snap.Slice(s, e), lx)
		}
		if !md.queued {
			md.queued = true
			d.cmds = append(d.cmds, func(d *editor) { a.dragTo(d) })
			a.invalidate()
		}
		return true
	case ui.InputPointerUp:
		md := d.mdrag
		if md == nil {
			return false
		}
		// A move not followed yet still is, as the drag ends.
		if md.queued {
			md.done = true
		} else {
			d.mdrag = nil
		}
		return true
	}
	return false
}

// dragTo follows the pointer that drags a caret added or a column.
func (a *app) dragTo(d *editor) {
	md := d.mdrag
	if md == nil {
		return
	}
	md.queued = false
	if md.done {
		d.mdrag = nil
	}
	if md.column {
		a.columnTo(d, md.line, md.col, md.toLine, md.toCol)
		return
	}
	all, p := d.spans()
	all[p] = cursor{md.anchor, md.to, -1}
	d.selectSpans(all, p, false)
}

// toggleCaret adds a caret at the primary at, or takes away the selection
// there, unless it is the only one.
func (a *app) toggleCaret(d *editor, at int) {
	all, p := d.spans()
	d.column, d.added = nil, nil
	for i, s := range all {
		if s.start() <= at && at <= s.end() {
			if len(all) == 1 {
				d.selectSpans([]cursor{caretSel(at)}, 0, false)
				return
			}
			d.mdrag = nil
			all = slices.Delete(all, i, i+1)
			if i < p || i == p && p == len(all) {
				p--
			}
			d.selectSpans(all, max(p, 0), false)
			return
		}
	}
	all = append(all, caretSel(at))
	d.selectSpans(all, len(all)-1, false)
	d.hist.typing = false
}

// multiCommands are the commands of several selections, their keys on
// macOS: Command for CmdOrCtrl.
var multiCommands = []struct{ id, accel string }{
	{"sel.addCursorAbove", "Alt+CmdOrCtrl+Up"},
	{"sel.addCursorBelow", "Alt+CmdOrCtrl+Down"},
	{"sel.addNextOccurrence", "CmdOrCtrl+D"},
	{"sel.selectAllOccurrences", "Shift+CmdOrCtrl+L"},
	{"sel.columnUp", "Shift+Alt+CmdOrCtrl+Up"},
	{"sel.columnDown", "Shift+Alt+CmdOrCtrl+Down"},
	{"sel.columnLeft", "Shift+Alt+CmdOrCtrl+Left"},
	{"sel.columnRight", "Shift+Alt+CmdOrCtrl+Right"},
	{"sel.removeCursors", ""},
}

// isMultiCommand reports whether id is a command of several selections,
// whose keys a terminal keeps.
func isMultiCommand(id string) bool {
	return slices.ContainsFunc(multiCommands, func(c struct{ id, accel string }) bool { return c.id == id })
}

// multiStroke runs the command of several selections whose key ev is, as
// its menu item would: the text area takes arrows before the menus'
// shortcuts.
func (a *app) multiStroke(ev ui.InputEvent) bool {
	for _, mc := range multiCommands {
		cmd := command{id: mc.id, accel: platformAccel(mc.id, mc.accel)}
		st, ok := parseAccel(a.menuAccel(cmd))
		if ok && st.Mods == ev.Mods && st.Key == ev.Key {
			a.run(mc.id)
			return true
		}
	}
	return false
}
