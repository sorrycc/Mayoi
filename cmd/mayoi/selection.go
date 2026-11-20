package main

import (
	"slices"
	"sort"

	"github.com/egoist/mygo/ui"
)

// selRadius rounds the corners of the selection.
const selRadius = 3

// selectionShape is the selection on the lines in view, which the editor
// paints itself, in place of the text area: a box for each line, with the
// corners rounded where the line above or below does not go on past them.
// An editor keeps the last it found, as its decorations.
type selectionShape struct {
	version     uint64
	sel         [2]int
	cursors     []cursor
	first, last int
	font        codeFont
	found       bool

	rects []runeRect
	// native is set when a line selected runs from right to left, which
	// only the text area knows the places of: it paints the selection.
	native bool
	boxes  []rowBox
}

// selection finds the shape of the selections on lines first up to last.
func (a *app) selection(d *editor, snap ui.TextSnapshot, first, last int) *selectionShape {
	sh := &d.selShape
	if sh.found && sh.version == snap.Version() && sh.sel == d.sel && slices.Equal(sh.cursors, d.cursors) &&
		sh.first == first && sh.last == last && sh.font == a.font {
		return sh
	}
	sh.version, sh.sel, sh.first, sh.last, sh.font, sh.found = snap.Version(), d.sel, first, last, a.font, true
	sh.cursors = append(sh.cursors[:0], d.cursors...)
	sh.rects, sh.native = sh.rects[:0], false
	if len(d.cursors) == 0 {
		n := snap.Len()
		sh.add(snap, min(d.sel[0], d.sel[1], n), min(max(d.sel[0], d.sel[1]), n), first, last)
		return sh
	}
	// The selections that reach into the lines, by their order.
	lo, _ := snap.LineRange(first)
	_, hi := snap.LineRange(last)
	all, _ := d.spans()
	k := sort.Search(len(all), func(i int) bool { return all[i].end() >= lo })
	for ; k < len(all) && all[k].start() <= hi; k++ {
		if !sh.add(snap, all[k].start(), all[k].end(), first, last) {
			break
		}
	}
	return sh
}

// add adds the boxes of the selection of runes sa up to sb on lines first
// up to last, reporting false when a line runs from right to left, which
// only the text area knows the places of: it paints the selection then.
func (sh *selectionShape) add(snap ui.TextSnapshot, sa, sb, first, last int) bool {
	if sa == sb {
		return true
	}
	f := sh.font
	for i := max(first, snap.LineAt(sa)); i <= min(last, snap.LineAt(sb)); i++ {
		s, e := snap.LineRange(i)
		from, to := max(sa, s), min(sb, e)
		var x0, x1 float32
		if e-s > highlightRunes {
			// A line too long to measure for a box: by its cells.
			x0, x1 = float32(from-s)*f.charW, float32(to-s)*f.charW
		} else {
			text := snap.Slice(s, to)
			if hasRTL(text) {
				sh.rects, sh.native = sh.rects[:0], true
				return false
			}
			x1 = f.width(text)
			if from > s {
				x0 = f.width(snap.Slice(s, from))
			}
		}
		if sb > e {
			x1 += f.charW / 2 // the line's end is selected too
		}
		if x1 > x0 {
			sh.rects = append(sh.rects, runeRect{line: i, x0: x0, x1: x1})
		}
	}
	return true
}

// highlightRunes is the longest line the editor measures to place what it
// paints on it.
const highlightRunes = 4000

// hasRTL reports whether s has runes of a script written from right to
// left: Hebrew, Arabic and their neighbors.
func hasRTL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0xD6 {
			continue // below U+0590 in UTF-8
		}
		for _, r := range s[i:] {
			if r >= 0x0590 && r <= 0x08FF || r >= 0xFB1D && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFF {
				return true
			}
		}
		return false
	}
	return false
}

// paint fills the selection, with place calling fn with the part on each
// row of a run along a line: its top and its x's. The color is opaque, so
// that the boxes may overlap.
func (sh *selectionShape) paint(pt *ui.Painter, place func(line int, x0, x1 float32, fn func(y, x0, x1 float32)), lineH float32, col ui.Color) {
	const r = selRadius
	boxes := sh.rowBoxes(place)
	for k, b := range boxes {
		pt.Fill(ui.Rect{X: b.x0, Y: b.y, W: b.x1 - b.x0, H: lineH}, col, r)
		// Square the corners the rows above and below go on past.
		square := func(o rowBox, cy float32) {
			if o.x0 <= b.x0 && o.x1 > b.x0 {
				pt.Fill(ui.Rect{X: b.x0, Y: cy, W: min(r, b.x1-b.x0), H: r}, col, 0)
			}
			if o.x0 < b.x1 && o.x1 >= b.x1 {
				w := min(r, b.x1-b.x0)
				pt.Fill(ui.Rect{X: b.x1 - w, Y: cy, W: w, H: r}, col, 0)
			}
		}
		if k > 0 && near(boxes[k-1].y+lineH, b.y) {
			square(boxes[k-1], b.y)
		}
		if k+1 < len(boxes) && near(b.y+lineH, boxes[k+1].y) {
			square(boxes[k+1], b.y+lineH-r)
		}
	}
}

// rowBoxes returns the boxes of the selection on each row, as place puts
// the runs along its lines.
func (sh *selectionShape) rowBoxes(place func(line int, x0, x1 float32, fn func(y, x0, x1 float32))) []rowBox {
	boxes := sh.boxes[:0]
	for _, b := range sh.rects {
		place(b.line, b.x0, b.x1, func(y, x0, x1 float32) {
			if x1 > x0 {
				boxes = append(boxes, rowBox{y, x0, x1})
			}
		})
	}
	sh.boxes = boxes
	return boxes
}

// rowBox is the selection on a row, as painted.
type rowBox struct{ y, x0, x1 float32 }

func near(a, b float32) bool { return a-b < 0.5 && b-a < 0.5 }
