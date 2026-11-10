package main

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// diffPoint is a place in the text of a diff: before rune col of the line
// row shows.
type diffPoint struct{ row, col int }

func (p diffPoint) before(o diffPoint) bool {
	return p.row < o.row || p.row == o.row && p.col < o.col
}

// diffTextSel is text of a diff selected as an editor's is, from where the
// pointer went down to where it went: of one side of a split diff, 0 the
// old and 1 the new, or of the lines as the unified layout shows them, -1.
type diffTextSel struct {
	side     int
	from, to diffPoint
}

// ordered returns the ends of the selection, the first first.
func (s *diffTextSel) ordered() (lo, hi diffPoint) {
	if s.to.before(s.from) {
		return s.to, s.from
	}
	return s.from, s.to
}

// selText returns the line row i of a diff shows on side, -1 for the one
// the unified layout shows, and whether it shows one.
func (v *diffView) selText(i, side int) (string, bool) {
	if i < 0 || i >= len(v.rows) {
		return "", false
	}
	r := &v.rows[i]
	if r.kind != rowLine || r.file >= len(v.files) {
		return "", false
	}
	f := v.files[r.file]
	if !f.loaded || f.binary {
		return "", false
	}
	n, newSide := r.line()
	if side >= 0 {
		n, newSide = r.old, side == 1
		if newSide {
			n = r.new
		}
	}
	s := f.side(newSide)
	if n < 0 || n >= len(s.lines) {
		return "", false
	}
	return s.lines[n], true
}

// selectedText returns the text selected in a diff, "" for none: the
// lines as they are, with their indent and without their numbers or
// marks, those the unified layout shows of each side in the order it
// shows them.
func (v *diffView) selectedText() string {
	s := v.textSel
	if s == nil {
		return ""
	}
	lo, hi := s.ordered()
	if lo == hi {
		return ""
	}
	var b strings.Builder
	first := true
	for i := lo.row; i <= hi.row && i < len(v.rows); i++ {
		text, ok := v.selText(i, s.side)
		if !ok {
			continue
		}
		from, to := 0, utf8.RuneCountInString(text)
		if i == lo.row {
			from = min(lo.col, to)
		}
		if i == hi.row {
			to = min(hi.col, to)
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(text[runeByte(text, from):runeByte(text, max(from, to))])
	}
	return b.String()
}

// clearTextSel lets go of the text selected in a diff.
func (v *diffView) clearTextSel() {
	v.textSel, v.textDrag = nil, false
}

// onDiffText returns the side of a diff whose text x, in its view, is
// over, -1 for the unified layout's one, and whether it is over text
// rather than the line numbers or the gutter between the sides.
func onDiffText(v *diffView, l diffGeometry, x float32) (side int, ok bool) {
	if !v.split {
		return -1, x >= l.textX-4
	}
	side = btoi(l.onRight(x))
	if side == 0 && x >= l.half {
		return 0, false
	}
	return side, x >= float32(side)*l.right()+l.textX-4
}

// overDiffControl reports whether x, y in a diff's view are on one of the
// buttons over its rows, or on the header of the file at its top.
func (a *app) overDiffControl(v *diffView, x, y float32) bool {
	cx, cy := x+v.scroll.X, y+v.scroll.Y
	if slices.ContainsFunc(v.controls, func(r ui.Rect) bool { return r.Contains(cx, cy) }) {
		return true
	}
	lineH := a.font.lineH
	sticky, stickyY := v.stickyHeader(lineH)
	return sticky >= 0 && cy >= stickyY && cy < stickyY+lineH
}

// diffTextRow returns the row of side of a diff at y of its view.
func (a *app) diffTextRow(v *diffView, y float32, side int) int {
	lineH := a.font.lineH
	y += v.scroll.Y
	if side == 1 {
		y -= a.rightShift(v)
	}
	if a.streams(v) {
		return v.rowAtStream(v.sideTops[max(side, 0)], y, lineH)
	}
	return v.rowAt(y, lineH)
}

// diffTextAt returns the place in the text of side of a diff at x, y of
// its view, and whether a line of the side is there.
func (a *app) diffTextAt(v *diffView, l diffGeometry, x, y float32, side int) (diffPoint, bool) {
	row := a.diffTextRow(v, y, side)
	text, ok := v.selText(row, side)
	if !ok {
		return diffPoint{row, 0}, false
	}
	lineH := a.font.lineH
	textX := l.textX
	top := editorPadY + float32(v.tops[row])*lineH
	cols := l.cols[0]
	if side >= 0 {
		textX += float32(side) * l.right()
		top, _ = a.sideRowY(v, side, row)
		cols = l.cols[side]
	}
	col := diffCaretCol(a.font, clipLine(text), x+v.scroll.X-textX, y+v.scroll.Y-top, lineH, cols, v.built.wrap)
	return diffPoint{row, col}, true
}

// diffDragPoint returns the place in the text of the selection's side at
// x, y of a diff's view, as a drag extends it: past the rows of lines, the
// end of the nearest line toward where the selection began.
func (a *app) diffDragPoint(v *diffView, l diffGeometry, x, y float32) (diffPoint, bool) {
	s := v.textSel
	p, ok := a.diffTextAt(v, l, x, y, s.side)
	if ok {
		return p, true
	}
	if p.row > s.from.row {
		for k := p.row; k >= s.from.row; k-- {
			if text, ok := v.selText(k, s.side); ok {
				return diffPoint{k, utf8.RuneCountInString(text)}, true
			}
		}
	} else {
		for k := p.row; k <= s.from.row; k++ {
			if _, ok := v.selText(k, s.side); ok {
				return diffPoint{k, 0}, true
			}
		}
	}
	return p, false
}

// wordAround returns the runes of the word of text at rune col, from up
// to to: a run of the letters, digits and underscores of words, of
// spaces, or else the one rune.
func wordAround(text string, col int) (from, to int) {
	rs := []rune(text)
	col = min(max(col, 0), len(rs))
	if len(rs) == 0 {
		return 0, 0
	}
	at := col
	if at == len(rs) {
		at--
	}
	class := func(r rune) int {
		switch {
		case isWordRune(r):
			return 1
		case unicode.IsSpace(r):
			return 2
		}
		return 0
	}
	k := class(rs[at])
	if k == 0 && at > 0 && col == at && class(rs[at-1]) == 1 {
		// Just past a word, it is the word.
		at--
		k = 1
	}
	if k == 0 {
		return at, at + 1
	}
	from, to = at, at+1
	for from > 0 && class(rs[from-1]) == k {
		from--
	}
	for to < len(rs) && class(rs[to]) == k {
		to++
	}
	return from, to
}

// diffTextInput takes the pointer pressed on the text of a diff's lines,
// which selects text as an editor does: a drag the runes it goes over, two
// clicks a word, three the line, and with Shift, the selection there is
// goes to the pointer. The line numbers choose rows as they did. It
// reports whether it took the event.
func (a *app) diffTextInput(v *diffView, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || len(v.rows) == 0 || v.tops == nil {
			return false
		}
		l := a.diffGeometry(v, v.split, v.viewW)
		side, ok := onDiffText(v, l, ev.X)
		if !ok || a.overDiffControl(v, ev.X, ev.Y) {
			return false
		}
		s := v.textSel
		extend := ev.Mods&ui.Shift != 0 && s != nil && ev.Clicks < 2
		if extend {
			side = s.side
		} else if ev.Mods&ui.Shift != 0 {
			return false // the rows from the chosen one, as before
		}
		p, ok := a.diffTextAt(v, l, ev.X, ev.Y, side)
		if !ok {
			return false
		}
		v.pane.Focus()
		v.anchor = -1
		v.setCursor(p.row)
		if extend {
			s.to = p
		} else {
			s = &diffTextSel{side: side, from: p, to: p}
			if ev.Clicks >= 2 {
				text, _ := v.selText(p.row, side)
				from, to := 0, utf8.RuneCountInString(text)
				if ev.Clicks == 2 {
					from, to = wordAround(text, p.col)
				}
				s.from.col, s.to.col = from, to
			}
			v.textSel = s
		}
		if e := v.editing; e != nil {
			// The edit's own selection goes, as only one shows.
			e.ed.sel[0] = e.ed.sel[1]
		}
		v.textDrag = true
		a.invalidate()
		return true
	case ui.InputPointerMove:
		if !v.textDrag || v.textSel == nil {
			return false
		}
		// Past the top or the bottom of the view, it scrolls along.
		lineH := a.font.lineH
		maxY := max(0, 2*editorPadY+float32(v.tops[len(v.rows)])*lineH-v.viewH)
		switch {
		case ev.Y < 0:
			v.scroll.Y = clamp(v.scroll.Y+ev.Y, 0, maxY)
		case ev.Y > v.viewH:
			v.scroll.Y = clamp(v.scroll.Y+ev.Y-v.viewH, 0, maxY)
		}
		l := a.diffGeometry(v, v.split, v.viewW)
		if p, ok := a.diffDragPoint(v, l, ev.X, clamp(ev.Y, 0, v.viewH-1)); ok {
			v.textSel.to = p
			v.cursor = p.row
		}
		a.invalidate()
		return true
	case ui.InputPointerUp:
		if !v.textDrag {
			return false
		}
		v.textDrag = false
		return true
	}
	return false
}

// paintDiffSel paints the text selected of the line row i of a diff shows
// on side, -1 for the unified layout's one, under its text at x, y.
func (a *app) paintDiffSel(pt *ui.Painter, v *diffView, i, side int, x, y float32, cols int, focused bool) {
	s := v.textSel
	if s == nil || s.side != side {
		return
	}
	lo, hi := s.ordered()
	if lo == hi || i < lo.row || i > hi.row {
		return
	}
	text, ok := v.selText(i, side)
	if !ok {
		return
	}
	text = clipLine(text)
	n := utf8.RuneCountInString(text)
	from, to := 0, n
	if i == lo.row {
		from = min(lo.col, n)
	}
	if i == hi.row {
		to = min(hi.col, n)
	}
	col := a.pal.selected
	if !focused {
		col = a.pal.selectedDim
	}
	for _, rc := range diffSelRects(a.font, text, from, to, x, y, cols, v.built.wrap) {
		pt.Fill(rc, col, 0)
	}
	if i < hi.row {
		// The selection goes on past the line: its break shows selected.
		cx, k := diffCaretX(a.font, text, n, cols, v.built.wrap)
		pt.Fill(ui.Rect{X: x + cx, Y: y + float32(k)*a.font.lineH, W: a.font.charW / 2, H: a.font.lineH}, col, 0)
	}
}

// remapTextSel finds the rows of the text selected in a diff again, as
// they were before its rows were laid out again: from and to are where
// the ends of the selection were. It lets go of the selection whose lines
// no longer show.
func (v *diffView) remapTextSel(from, to diffAnchor) {
	s := v.textSel
	if s == nil {
		return
	}
	s.from.row, s.to.row = -1, -1
	for i := range v.rows {
		at := v.anchorOf(i)
		if at.kind != rowLine {
			continue
		}
		if at == from {
			s.from.row = i
		}
		if at == to {
			s.to.row = i
		}
	}
	if s.from.row < 0 || s.to.row < 0 {
		v.clearTextSel()
	}
}
