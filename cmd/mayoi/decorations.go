package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/linediff"
)

// decorations are what the editor paints around the text of the lines in
// view: the indent guides, the brackets paired at the caret and the other
// occurrences of the word at it. An editor keeps the last it found, and
// finds them again only when its text, its caret or its view changes.
type decorations struct {
	version     uint64
	sel         [2]int
	first, last int

	levels []int // the indent levels of lines first up to last
	// The guide of the block the caret is in: its level, and its lines.
	active, activeFrom, activeTo int
	brackets                     []runeRect
	words                        []runeRect
}

// runeRect is a run of runes on a line, as x offsets from the start of the
// text.
type runeRect struct {
	line   int
	x0, x1 float32
}

// maxScan is how far the editor looks for a bracket's pair, and for the
// lines around a blank one or the caret's block.
const (
	maxBracketScan = 40_000
	maxLineScan    = 1_000
)

// decorate finds the decorations of lines first up to last of a snapshot.
func (a *app) decorate(d *editor, snap ui.TextSnapshot, first, last int) *decorations {
	dc := &d.deco
	if dc.version == snap.Version() && dc.sel == d.sel && dc.first == first && dc.last == last && dc.levels != nil {
		return dc
	}
	dc.version, dc.sel, dc.first, dc.last = snap.Version(), d.sel, first, last
	unit := max(d.indent.cols(), 1)
	// The lines in view and those around them that blank lines are
	// measured against, read at once.
	w := d.window(snap, first-maxLineScan, last+maxLineScan+1)
	// indentOf returns a line's indent in columns, -1 for a blank line.
	indentOf := w.indent
	lines := snap.LineCount()
	// level returns a line's indent level; a blank line takes the deeper
	// of the lines around it, so that the guides go on through it.
	level := func(i int) int {
		if c := indentOf(i); c >= 0 {
			return (c + unit - 1) / unit
		}
		above, below := 0, 0
		for j := i - 1; j >= max(0, i-maxLineScan); j-- {
			if c := indentOf(j); c >= 0 {
				above = (c + unit - 1) / unit
				break
			}
		}
		for j := i + 1; j < min(lines, i+maxLineScan); j++ {
			if c := indentOf(j); c >= 0 {
				below = (c + unit - 1) / unit
				break
			}
		}
		return max(above, below)
	}
	dc.levels = dc.levels[:0]
	for i := first; i <= last; i++ {
		dc.levels = append(dc.levels, level(i))
	}

	// The caret's block: the lines around it as deep as it, or as the line
	// after it when it opens one. Its guide shows only in view, so only the
	// lines in view are looked at.
	caret := min(d.sel[1], snap.Len())
	cl := snap.LineAt(caret)
	dc.active = 0
	inView := cl >= first-1 && cl <= last+1
	if inView {
		dc.active = level(cl)
	}
	if inView && indentOf(cl) >= 0 {
		for j := cl + 1; j < min(lines, cl+maxLineScan); j++ {
			if c := indentOf(j); c >= 0 {
				dc.active = max(dc.active, (c+unit-1)/unit)
				break
			}
		}
		// On a line that closes a block, the block it closes.
		if l := (indentOf(cl) + unit - 1) / unit; dc.active == l && cl > 0 && level(cl-1) > l {
			dc.active = l + 1
			cl--
		}
	}
	dc.activeFrom, dc.activeTo = cl, cl
	if dc.active > 0 {
		if level(cl) < dc.active {
			cl++ // the opener: the block starts below it
			dc.activeFrom, dc.activeTo = cl, cl
		}
		for dc.activeFrom > max(0, first) && level(dc.activeFrom-1) >= dc.active {
			dc.activeFrom--
		}
		for dc.activeTo < min(lines-1, last) && level(dc.activeTo+1) >= dc.active {
			dc.activeTo++
		}
	}

	dc.brackets = dc.brackets[:0]
	dc.words = dc.words[:0]
	if d.sel[0] == d.sel[1] {
		if at, pair, ok := matchBracket(snap, caret); ok {
			dc.brackets = append(dc.brackets, a.runeRectOf(snap, at, at+1), a.runeRectOf(snap, pair, pair+1))
		}
	}
	a.findWords(d, snap, first, last)
	return dc
}

// runeRectOf returns where the runes start up to end, on one line, are.
func (a *app) runeRectOf(snap ui.TextSnapshot, start, end int) runeRect {
	line := snap.LineAt(start)
	s, e := snap.LineRange(line)
	x0 := a.textWidth(snap.Slice(s, start))
	x1 := a.textWidth(snap.Slice(s, min(end, e)))
	return runeRect{line, x0, max(x1, x0+2)}
}

var closers = map[rune]rune{'(': ')', '[': ']', '{': '}'}
var openers = map[rune]rune{')': '(', ']': '[', '}': '{'}

// matchBracket finds the bracket beside the caret, before it first, and its
// pair: the offsets of both.
func matchBracket(snap ui.TextSnapshot, caret int) (at, pair int, ok bool) {
	n := snap.Len()
	for _, i := range []int{caret - 1, caret} {
		if i < 0 || i >= n {
			continue
		}
		r, _ := utf8.DecodeRuneInString(snap.Slice(i, i+1))
		if c, open := closers[r]; open {
			end := min(n, i+1+maxBracketScan)
			text := snap.Slice(i+1, end)
			depth, off := 0, i+1
			for _, x := range text {
				switch x {
				case r:
					depth++
				case c:
					if depth == 0 {
						return i, off, true
					}
					depth--
				}
				off++
			}
			return 0, 0, false
		}
		if o, close := openers[r]; close {
			start := max(0, i-maxBracketScan)
			runes := []rune(snap.Slice(start, i))
			depth := 0
			for k := len(runes) - 1; k >= 0; k-- {
				switch runes[k] {
				case r:
					depth++
				case o:
					if depth == 0 {
						return i, start + k, true
					}
					depth--
				}
			}
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// findWords finds, on the lines in view, the other occurrences of the
// word at the caret, or of the text selected on one line.
func (a *app) findWords(d *editor, snap ui.TextSnapshot, first, last int) {
	dc := &d.deco
	if a.find.open && len(a.find.matches) > 0 {
		return // find shows its matches instead
	}
	a0, b0 := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	n := snap.Len()
	a0, b0 = min(a0, n), min(b0, n)
	var word string
	whole := true
	if a0 == b0 {
		line := snap.LineAt(a0)
		s, e := snap.LineRange(line)
		if e-s > 2000 {
			return
		}
		runes := []rune(snap.Slice(s, e))
		i, j := a0-s, a0-s
		for i > 0 && isWordRune(runes[i-1]) {
			i--
		}
		for j < len(runes) && isWordRune(runes[j]) {
			j++
		}
		if j-i < 2 || j-i > 80 || unicode.IsDigit(runes[i]) {
			return
		}
		word = string(runes[i:j])
	} else {
		if b0-a0 > 200 || snap.LineAt(a0) != snap.LineAt(b0) {
			return
		}
		word = snap.Slice(a0, b0)
		if strings.TrimSpace(word) == "" {
			return
		}
		whole = false
	}
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		if e-s > 2000 {
			continue
		}
		text := snap.Slice(s, e)
		for at := 0; ; {
			k := strings.Index(text[at:], word)
			if k < 0 {
				break
			}
			k += at
			end := k + len(word)
			at = end
			if whole {
				before, _ := utf8.DecodeLastRuneInString(text[:k])
				after, _ := utf8.DecodeRuneInString(text[end:])
				if k > 0 && isWordRune(before) || end < len(text) && isWordRune(after) {
					continue
				}
			}
			dc.words = append(dc.words, runeRect{
				line: i,
				x0:   a.textWidth(text[:k]),
				x1:   a.textWidth(text[:end]),
			})
			if len(dc.words) > 400 {
				return
			}
		}
	}
	if len(dc.words) == 1 {
		dc.words = dc.words[:0] // only itself
	}
}

// overview is what the strip along the editor's right edge marks: the
// lines of find's matches, as fractions of the document.
type overview struct {
	key     string
	version uint64
	rows    []float32

	// The runs of lines changed since the last commit, of the marks of
	// one version.
	gitVersion uint64
	gitLines   int
	git        []gitRun
}

// gitRun is a run of lines of one kind of change, as fractions of the
// document.
type gitRun struct {
	from, to float32
	kind     linediff.Kind
}

// gitRuns returns the runs of the lines changed since the last commit.
func (d *editor) gitRuns() []gitRun {
	ov, g := &d.overview, &d.git
	if ov.gitVersion == g.Version && ov.gitLines == len(g.Lines) && ov.git != nil {
		return ov.git
	}
	ov.gitVersion, ov.gitLines, ov.git = g.Version, len(g.Lines), ov.git[:0]
	n := float32(max(len(g.Lines), 1))
	for i := 0; i < len(g.Lines); {
		k := g.Lines[i]
		j := i + 1
		for j < len(g.Lines) && g.Lines[j] == k && k != linediff.Deleted && k != linediff.DeletedBelow {
			j++
		}
		if k != linediff.Same {
			ov.git = append(ov.git, gitRun{float32(i) / n, float32(j) / n, k})
		}
		i = j
	}
	if ov.git == nil {
		ov.git = []gitRun{}
	}
	return ov.git
}

// overviewRows returns where find's matches are, as fractions of the
// document's height, one per pixel row of a strip h high.
func (a *app) overviewRows(d *editor, snap ui.TextSnapshot, h float32) []float32 {
	f := &a.find
	ov := &d.overview
	if !f.open || f.doc != d.id || len(f.matches) == 0 || h <= 0 {
		return nil
	}
	key := f.key
	if ov.key == key && ov.version == f.version && ov.rows != nil {
		return ov.rows
	}
	ov.key, ov.version, ov.rows = key, f.version, ov.rows[:0]
	// The matches are in order: one per row of the strip is enough.
	lines := float32(max(snap.LineCount(), 1))
	last := float32(-1)
	for _, m := range f.matches {
		if m[0] > snap.Len() {
			break
		}
		y := float32(snap.LineAt(m[0])) / lines
		if (y-last)*h >= 1 {
			ov.rows = append(ov.rows, y)
			last = y
		}
	}
	return ov.rows
}

// whitespaceMark is a space or a tab in the selection, which the editor
// shows faint: a dot, or an arrow as wide as the tab.
type whitespaceMark struct {
	line   int
	x0, x1 float32
	tab    bool
}

// selectedWhitespace returns the spaces and tabs selected on lines first
// up to last.
func (a *app) selectedWhitespace(d *editor, snap ui.TextSnapshot, first, last int) []whitespaceMark {
	sa, sb := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	n := snap.Len()
	sa, sb = min(sa, n), min(sb, n)
	if sa == sb {
		return nil
	}
	var out []whitespaceMark
	for i := max(first, snap.LineAt(sa)); i <= min(last, snap.LineAt(sb)); i++ {
		s, e := snap.LineRange(i)
		if e-s > 2000 {
			continue
		}
		text := snap.Slice(s, e)
		from, to := max(sa, s)-s, min(sb, e)-s
		x := float32(0)
		ascii := isASCII(text)
		k := 0 // the rune
		for j := 0; j < len(text); {
			if k >= to {
				break
			}
			// size is the bytes the rune takes in text: one for a byte
			// that isn't UTF-8, which a file may hold past its first 8 KB.
			r, size := utf8.DecodeRuneInString(text[j:])
			j += size
			var w float32
			switch {
			case r == '\t':
				w = a.font.afterTab(x) - x
			case ascii:
				w = a.font.charW
			default:
				w = a.textWidth(text[:j]) - x
			}
			if k >= from && (r == ' ' || r == '\t') && len(out) < 4000 {
				out = append(out, whitespaceMark{line: i, x0: x, x1: x + w, tab: r == '\t'})
			}
			x += w
			k++
		}
	}
	return out
}
