package main

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

// maxSticky is the most lines that stick at the top of the editor, and
// maxScopeScan how far above a line the lines opening its blocks are
// looked for.
const (
	maxSticky    = 5
	maxScopeScan = 4 * maxLineScan
)

// scopes returns the lines that open the blocks line is in, outermost
// first: the nearest line above each that is indented less, as a function
// or an if is above its body.
func scopes(snap ui.TextSnapshot, line int) []int {
	if line <= 0 || line >= snap.LineCount() {
		return nil
	}
	return scopesIn(snap.LineCount(), line, newLineWindow(snap, line-maxScopeScan, line+maxLineScan))
}

// scopesIn is scopes of a text of n lines, read from w, which has those
// above line as far as the scan goes, and a few below for a blank line.
func scopesIn(n, line int, w *lineWindow) []int {
	out, _ := scopesWithin(n, line, w)
	return out
}

// scopesWithin is scopesIn, and whether the scan stayed within w: false
// when it needed a line of the text that w does not have.
func scopesWithin(n, line int, w *lineWindow) ([]int, bool) {
	whole := true
	indentOf := func(i int) (int, string) {
		if !w.has(i) {
			whole = whole && (i < 0 || i >= n)
			return -1, ""
		}
		return w.indent(i), w.line(i)
	}
	// A blank line is as deep as the line below it.
	cur := -1
	for i := line; i < min(n, line+maxLineScan) && cur < 0; i++ {
		cur, _ = indentOf(i)
	}
	if cur <= 0 {
		return nil, whole
	}
	var out []int
	for j := line - 1; j >= max(0, line-maxScopeScan) && cur > 0; j-- {
		ind, text := indentOf(j)
		if ind < 0 || ind >= cur {
			continue
		}
		cur = ind
		t := strings.TrimSpace(text)
		if strings.ContainsAny(t[:1], ")]}") {
			if !strings.ContainsAny(t[len(t)-1:], "{[(:") {
				continue // the end of a block, as of a function's arguments
			}
			// A block opened after a list over several lines, as the
			// arguments of a function: the line the list starts on names it.
			if !strings.HasPrefix(t, "}") {
				for k := j - 1; k >= max(0, j-maxLineScan); k-- {
					ki, kt := indentOf(k)
					if ki < 0 || ki > ind {
						continue
					}
					if ki == ind && !strings.ContainsAny(strings.TrimSpace(kt)[:1], ")]}") {
						j = k
					}
					break
				}
			}
		}
		out = append(out, j)
	}
	for i, k := 0, len(out)-1; i < k; i, k = i+1, k-1 {
		out[i], out[k] = out[k], out[i]
	}
	return out, whole
}

// scopeProbe is how many lines above a line its scopes are looked for
// first, and below it for a blank line: most blocks open within them,
// and reading them costs far less than the thousands of lines the scan
// may go over.
const (
	scopeProbe      = 600
	scopeProbeBelow = 100
)

// scopeCache keeps the scopes of a few lines of one version of a text,
// which each frame asks for again: those of the caret's line and of the
// lines at the top of the view.
type scopeCache struct {
	version uint64
	lines   map[int][]int
	// near is the lines read around a line asked for, of version, which
	// the lines near it, as the view scrolls, read theirs from too.
	near *lineWindow
}

// probe returns the lines around line that its scopes are looked for in
// first, of the cache's version of the text.
func (sc *scopeCache) probe(snap ui.TextSnapshot, line int) *lineWindow {
	if w := sc.near; w != nil && w.has(line-min(line, scopeProbe/2)) && w.has(min(line+scopeProbeBelow/2, snap.LineCount()-1)) {
		return w
	}
	sc.near = newLineWindow(snap, line-scopeProbe, line+scopeProbeBelow)
	return sc.near
}

// scopesOf returns scopes of a line of d, from its cache when the text is
// the same.
func (d *editor) scopesOf(snap ui.TextSnapshot, line int) []int {
	sc := &d.scopes
	if sc.version != snap.Version() {
		sc.near = nil
	}
	if sc.version != snap.Version() || sc.lines == nil || len(sc.lines) > 64 {
		sc.version, sc.lines = snap.Version(), map[int][]int{}
	}
	if s, ok := sc.lines[line]; ok {
		return s
	}
	var s []int
	if n := snap.LineCount(); line > 0 && line < n {
		// A line typed in reads the lines around it again: those near it
		// first, and as far as the scan goes only when they do not tell.
		from, to := line-maxScopeScan, line+maxLineScan
		if w := d.win; w != nil && d.winVersion == snap.Version() && w.covers(max(from, 0), min(to, n)) {
			s = scopesIn(n, line, w)
		} else if probe, whole := scopesWithin(n, line, sc.probe(snap, line)); whole {
			s = probe
		} else {
			s = scopesIn(n, line, d.window(snap, from, to))
		}
	}
	sc.lines[line] = s
	return s
}

// stickyLines returns the lines that stick at the top of a view whose
// first line is top: the openers of the blocks it is in, each above its
// own place.
func stickyLines(snap ui.TextSnapshot, top, room int, scopes func(ui.TextSnapshot, int) []int) []int {
	var lines []int
	// The lines that stick cover the lines below them: those decide
	// which stick, until the two agree.
	for range 3 {
		next := slices.Clone(scopes(snap, top+len(lines)))
		if len(next) > room {
			next = next[len(next)-room:]
		}
		k := 0
		for _, j := range next {
			if j < top+k {
				next[k] = j
				k++
			}
		}
		next = next[:k]
		if len(next) == len(lines) {
			lines = next
			break
		}
		lines = next
	}
	return lines
}

// stickyRow is a line stuck at the top of the editor, to paint: its
// number, the width of its indent, and its text in the colors of its
// tokens.
type stickyRow struct {
	line   int
	indent float32
	spans  []ui.Span
}

// stickyRows builds the rows of the lines that stick.
func (a *app) stickyRows(d *editor, snap ui.TextSnapshot, lines []int) []stickyRow {
	p, f := a.pal, a.font
	rows := make([]stickyRow, 0, len(lines))
	for _, i := range lines {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, min(e, s+300))
		lead := leading(text)
		row := stickyRow{line: i, indent: f.width(lead)}
		var toks []tokenSpan
		if d.hl != nil {
			for _, t := range d.hl.Line(i, snap.Slice(s, e)) {
				toks = append(toks, tokenSpan{t.Start, t.End, p.syntax[t.Kind]})
			}
		}
		// Split the text after the indent at the tokens' bounds.
		runes := []rune(text)
		at := utf8.RuneCountInString(lead)
		span := func(from, to int, c ui.Color) {
			if to > from {
				txt := strings.ReplaceAll(string(runes[from:to]), "\t", "    ")
				row.spans = append(row.spans, f.span(txt, c))
			}
		}
		for _, t := range toks {
			if t.end <= at || t.start >= len(runes) {
				continue
			}
			span(at, max(at, t.start), p.text)
			from := max(at, t.start)
			to := min(t.end, len(runes))
			span(from, to, t.color)
			at = to
		}
		span(at, len(runes), p.text)
		rows = append(rows, row)
	}
	return rows
}

// stickyCache keeps the rows of the lines that stick, which are the same
// from frame to frame until the lines, their text or their colors change.
type stickyCache struct {
	version, gen uint64
	pal          *palette
	font         codeFont
	lines        []int
	out          []stickyRow
}

func (sc *stickyCache) rows(a *app, d *editor, snap ui.TextSnapshot, lines []int) []stickyRow {
	gen := d.hl.Gen()
	if sc.version == snap.Version() && sc.gen == gen && sc.pal == a.pal && sc.font == a.font && slices.Equal(sc.lines, lines) {
		return sc.out
	}
	sc.version, sc.gen, sc.pal, sc.font = snap.Version(), gen, a.pal, a.font
	sc.lines = append(sc.lines[:0], lines...)
	sc.out = a.stickyRows(d, snap, lines)
	return sc.out
}

type tokenSpan struct {
	start, end int
	color      ui.Color
}

// scopeLabel is how the breadcrumbs name a block by the line opening it.
func scopeLabel(snap ui.TextSnapshot, line int) string {
	s, e := snap.LineRange(line)
	t := strings.TrimSpace(snap.Slice(s, min(e, s+200)))
	t = strings.TrimSpace(strings.TrimRight(t, "{(:[ "))
	if utf8.RuneCountInString(t) > 48 {
		t = string([]rune(t)[:47]) + "…"
	}
	return t
}

// isCode reports whether a document is in a language whose indentation
// shows its blocks, as Markdown's and plain text's do not.
func isCode(d *document) bool {
	if d.hl == nil || d.hl.Lexer() == nil {
		return false
	}
	switch highlight.Name(d.hl.Lexer()) {
	case "markdown", "Markdown", "reStructuredText", "TeX", "plaintext":
		return false
	}
	return true
}
