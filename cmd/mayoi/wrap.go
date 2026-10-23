package main

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// wraps reports whether an editor wraps its lines: as its tab chose, or
// else as the settings say.
func (a *app) wraps(d *editor) bool {
	if d.wrap != nil && d.wrapGen == a.settings.WrapGen {
		return *d.wrap
	}
	return a.settings.WordWrap
}

// wrapsMerge reports whether a merge wraps the lines of its panes: as its
// tab chose, or else as the settings say.
func (a *app) wrapsMerge(v *mergeView) bool {
	if v.wrap != nil && v.wrapGen == a.settings.WrapGen {
		return *v.wrap
	}
	return a.settings.WordWrap
}

// toggleWordWrap wraps the lines of every editor, or none: the tabs that
// chose otherwise follow it again.
func (a *app) toggleWordWrap() {
	s := a.settings
	s.WordWrap = !s.WordWrap
	s.WrapGen++
	s.Changed()
	a.invalidate()
}

// toggleTabWrap wraps the lines of the editor shown, or stops, until
// the settings' Word Wrap changes.
func (a *app) toggleTabWrap() {
	if t := a.activeTab(); t != nil {
		if t.diff != nil {
			a.toggleDiffWrap(t.diff)
			return
		}
		if t.merge != nil {
			a.toggleMergeWrap(t.merge)
			return
		}
	}
	d := a.activeEditor()
	if d == nil {
		return
	}
	w := !a.wraps(d)
	d.wrap, d.wrapGen = &w, a.settings.WrapGen
	a.invalidate()
}

// toggleMergeWrap wraps the lines of a merge's panes, or stops, until the
// settings' Word Wrap changes.
func (a *app) toggleMergeWrap(v *mergeView) {
	w := !a.wrapsMerge(v)
	v.wrap, v.wrapGen = &w, a.settings.WrapGen
	a.invalidate()
}

// geometry places the lines of an editor in the text area's content,
// below its padding: one row each, lineH apart, or where the area laid
// them out, as it wraps them.
type geometry struct {
	lines *ui.TextLines // nil while the editor does not wrap
	lineH float32
	n     int // the lines
}

func (a *app) geometry(d *editor, n int) geometry {
	g := geometry{lineH: a.font.lineH, n: n}
	if a.wraps(d) {
		g.lines = &d.lines
	}
	return g
}

// top returns the top of line i.
func (g geometry) top(i int) float32 {
	if g.lines != nil {
		return g.lines.Top(i)
	}
	return float32(i) * g.lineH
}

// at returns the line at y, within the text.
func (g geometry) at(y float32) int {
	if g.lines != nil {
		return g.lines.At(y)
	}
	return min(max(0, int(y/g.lineH)), g.n-1)
}

// topF returns the top of a fractional line i, between the tops of the
// lines its whole part falls between: where a line across from i stands.
// Below the first line it goes on by the first line's rows, as i*lineH
// does and as atF reads it back.
func (g geometry) topF(i float32) float32 {
	if g.lines == nil {
		return i * g.lineH
	}
	if i < 0 {
		return i * (g.lines.Top(1) - g.lines.Top(0))
	}
	lo, f := float32(int(i)), i-float32(int(i))
	t0 := g.lines.Top(int(lo))
	if f == 0 {
		return t0
	}
	return t0 + f*(g.lines.Top(int(lo)+1)-t0)
}

// atF returns the fractional line at y: the line y falls in, and how far
// into it, as the line across from y goes. Past the last line it stays
// at the last.
func (g geometry) atF(y float32) float32 {
	if g.lines == nil {
		return min(y/g.lineH, float32(g.n))
	}
	i := g.lines.At(y)
	t0 := g.lines.Top(i)
	t1 := g.lines.Top(i + 1)
	if t1 <= t0 {
		return float32(i)
	}
	return min(float32(i)+(y-t0)/(t1-t0), float32(g.n))
}

// height returns the height of all the lines.
func (g geometry) height() float32 { return g.top(g.n) }

// row returns the row of line i that rune off of the line is on, 0 where
// the editor does not wrap.
func (g geometry) row(i, off int) int {
	if g.lines == nil {
		return 0
	}
	rows := g.lines.Rows(i)
	k := 0
	for k+1 < len(rows) && rows[k+1] <= off {
		k++
	}
	return k
}

// rowSlack is how many lines beyond those in view the editor finds the
// rows of, for what it found to paint in the frame before.
const rowSlack = 16

// wrapRows are the rows of the lines in view of an editor that wraps: for
// each line, the x where each of its rows starts, measured along the line
// as if it did not wrap. What the editor paints is placed along the line,
// and these place it on its rows.
type wrapRows struct {
	version     uint64
	first, last int
	width       float32
	font        codeFont
	found       bool

	starts [][]float32 // of lines first up to last; nil for one row
	runes  []int
}

// rowsOf finds the rows of lines first up to last, as the text area,
// width wide, laid them out.
func (a *app) rowsOf(d *editor, snap ui.TextSnapshot, first, last int, width float32) *wrapRows {
	wr := &d.rows
	if wr.found && wr.version == snap.Version() && wr.first == first && wr.last == last && wr.width == width && wr.font == a.font {
		return wr
	}
	wr.version, wr.first, wr.last, wr.width, wr.font, wr.found = snap.Version(), first, last, width, a.font, true
	clear(wr.starts)
	wr.starts = wr.starts[:0]
	f := a.font
	for i := first; i <= last; i++ {
		wr.runes = d.lines.AppendRows(wr.runes[:0], i)
		if len(wr.runes) < 2 {
			wr.starts = append(wr.starts, nil)
			continue
		}
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		xs := make([]float32, 0, len(wr.runes))
		ascii := isASCII(text)
		var x float32
		k, at := 0, 0 // the rune, and the row next found
		for j, r := range text {
			for at < len(wr.runes) && wr.runes[at] == k {
				xs = append(xs, x)
				at++
			}
			if at == len(wr.runes) {
				break
			}
			switch {
			case r == '\t':
				x = f.afterTab(x)
			case ascii || e-s > highlightRunes:
				x += f.charW
			default:
				x = f.width(text[:j+utf8.RuneLen(r)])
			}
			k++
		}
		for len(xs) < len(wr.runes) {
			xs = append(xs, x)
		}
		wr.starts = append(wr.starts, xs)
	}
	return wr
}

// rowStarts returns where the rows of line i start along it, nil for a
// line on one row or out of view.
func (wr *wrapRows) rowStarts(i int) []float32 {
	if wr == nil || i < wr.first || i > wr.last || i-wr.first >= len(wr.starts) {
		return nil
	}
	return wr.starts[i-wr.first]
}

// rowCount returns how many rows line i takes.
func (wr *wrapRows) rowCount(i int) int {
	return max(len(wr.rowStarts(i)), 1)
}

// pieces calls fn with the part on each row of the run from x0 to x1
// along line i: the row, and the run's x's from the start of the row.
func (wr *wrapRows) pieces(i int, x0, x1 float32, fn func(row int, x0, x1 float32)) {
	xs := wr.rowStarts(i)
	if len(xs) < 2 {
		fn(0, x0, x1)
		return
	}
	for k, lo := range xs {
		hi := float32(1e30)
		if k+1 < len(xs) {
			hi = xs[k+1]
		}
		if x1 <= lo && k > 0 || x0 >= hi {
			continue
		}
		fn(k, max(x0, lo)-lo, min(x1, hi)-lo)
	}
}
