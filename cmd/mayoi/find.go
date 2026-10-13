package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// maxMatches is the most matches find counts.
const maxMatches = 20000

// findState is the find and replace widget of the editor.
type findState struct {
	open, replacing bool
	query, with     string
	matchCase, word bool
	regex           bool

	matches [][2]int // rune offsets of the matches in the document
	cur     int      // the current match, -1 for none
	err     string

	// What the matches are of: the query and options, the document and
	// its version.
	key     string
	doc     int
	version uint64
	stale   bool
	re      *regexp.Regexp
	// pending is the version of the document pendingDoc that matches are
	// being found for in the background, 0 for none.
	pending    uint64
	pendingDoc int

	input  ui.Handle
	reveal bool // select the current match
}

func (f *findState) pattern() string {
	return fmt.Sprintf("%v|%v|%v|%s", f.matchCase, f.word, f.regex, f.query)
}

// openFind opens the find widget, with the selection's text as the query
// when it is on one line.
func (a *app) openFind(replace bool) {
	f := &a.find
	if d := a.textEditor(); d != nil {
		snap := d.buf.Snapshot()
		if s, e := d.sel[0], d.sel[1]; s != e {
			s, e = min(s, e), max(s, e)
			if text := snap.Slice(s, min(e, snap.Len())); !strings.Contains(text, "\n") && len(text) < 200 {
				f.query = text
			}
		}
	}
	f.open = true
	f.replacing = replace || f.replacing && f.open
	f.stale = true
	f.input.Focus()
}

func (a *app) closeFind() {
	a.find.open = false
	a.focusColumn()
}

// asyncSearchBytes is the size of a document past which find looks for
// its matches in the background as it is edited, keeping typing quick.
const asyncSearchBytes = 256 << 10

// compile makes the regular expression of the query and its options.
func (f *findState) compile() (*regexp.Regexp, error) {
	return compileQuery(f.query, f.matchCase, f.word, f.regex)
}

// compileQuery makes the regular expression of a query of find or
// search and its options.
func compileQuery(query string, matchCase, word, regex bool) (*regexp.Regexp, error) {
	expr := query
	if !regex {
		expr = regexp.QuoteMeta(expr)
	}
	if word {
		expr = `\b` + expr + `\b`
	}
	if !matchCase {
		expr = "(?i)" + expr
	}
	return regexp.Compile("(?m)" + expr)
}

// scanMatches returns the rune offsets of re's matches in a snapshot. It
// may run on any goroutine.
func scanMatches(re *regexp.Regexp, snap ui.TextSnapshot) [][2]int {
	text := snap.String()
	var matches [][2]int
	runes, at := 0, 0 // the rune offset of byte at
	for _, m := range re.FindAllStringIndex(text, maxMatches) {
		if m[0] == m[1] {
			continue // empty matches, as of ^ or a*
		}
		runes += utf8.RuneCountInString(text[at:m[0]])
		start := runes
		runes += utf8.RuneCountInString(text[m[0]:m[1]])
		at = m[1]
		matches = append(matches, [2]int{start, runes})
	}
	return matches
}

// search finds the matches of the query in d, unless they were found for
// its text as it is.
func (a *app) search(d *editor) {
	a.searchIn(d, false)
}

// searchIn is search, which in the background, when async is set and d
// is large, only changes the matches when they are found: until then the
// matches of the text before the edit show.
func (a *app) searchIn(d *editor, async bool) {
	f := &a.find
	snap := d.buf.Snapshot()
	key := f.pattern()
	if !f.stale && f.key == key && f.doc == d.id && f.version == snap.Version() {
		return
	}
	queryChanged := f.key != key || f.doc != d.id
	// Another document shown, as in the column the keyboard moves to,
	// shows its matches and keeps its selection.
	sameQuery := f.key == key
	if async && !queryChanged && f.re != nil && snap.ByteLen() > asyncSearchBytes {
		if f.pending == snap.Version() && f.pendingDoc == d.id {
			return // on its way
		}
		f.pending, f.pendingDoc = snap.Version(), d.id
		re, id := f.re, d.id
		go func() {
			matches := scanMatches(re, snap)
			a.post(func() {
				if f.pending != snap.Version() || f.pendingDoc != id {
					return // superseded
				}
				f.pending, f.pendingDoc = 0, 0
				if f.key != key || f.doc != id || d.buf.Version() != snap.Version() {
					return // the query or the text changed meanwhile
				}
				a.applyMatches(d, matches, snap.Version(), false)
			})
		}()
		return
	}
	f.key, f.doc, f.stale = key, d.id, false
	f.pending, f.pendingDoc = 0, 0
	f.matches, f.err, f.re = f.matches[:0], "", nil
	if f.query == "" {
		f.version, f.cur = snap.Version(), -1
		return
	}
	re, err := f.compile()
	if err != nil {
		f.version, f.err, f.cur = snap.Version(), "Invalid expression", -1
		return
	}
	f.re = re
	a.applyMatches(d, scanMatches(re, snap), snap.Version(), queryChanged)
	if sameQuery {
		f.reveal = false
	}
}

// applyMatches takes the matches found in a version of d's text, keeping
// the current match in place, or choosing the first from the caret when
// the query changed.
func (a *app) applyMatches(d *editor, matches [][2]int, version uint64, queryChanged bool) {
	f := &a.find
	f.matches, f.version = matches, version
	if queryChanged || f.cur >= len(f.matches) {
		// The first match from the caret.
		caret := min(d.sel[0], d.sel[1])
		f.cur = sort.Search(len(f.matches), func(i int) bool { return f.matches[i][1] > caret })
		if f.cur == len(f.matches) {
			f.cur = 0
		}
		if len(f.matches) == 0 {
			f.cur = -1
		}
		f.reveal = queryChanged && f.cur >= 0
	}
}

// step moves to the next match, or the previous, round the ends.
func (a *app) step(d *editor, by int) {
	f := &a.find
	a.search(d)
	if len(f.matches) == 0 {
		return
	}
	if f.cur < 0 {
		f.cur = 0
	} else {
		f.cur = (f.cur + by + len(f.matches)) % len(f.matches)
	}
	f.reveal = true
}

// replaceOne replaces the current match, and goes to the next.
func (a *app) replaceOne(d *editor) {
	f := &a.find
	a.search(d)
	if f.cur < 0 || f.cur >= len(f.matches) || f.re == nil {
		return
	}
	m := f.matches[f.cur]
	old := d.buf.Slice(m[0], m[1])
	with := f.re.ReplaceAllString(old, a.replacement())
	end := m[0] + utf8.RuneCountInString(with)
	d.edit([2]int{end, end}, textEdit{m[0], m[1], with})
	a.search(d)
	if f.cur >= 0 {
		// The next match is the one after the text put in.
		f.cur = sort.Search(len(f.matches), func(i int) bool { return f.matches[i][0] >= end })
		if f.cur == len(f.matches) {
			f.cur = min(0, len(f.matches)-1)
		}
		f.reveal = f.cur >= 0
	}
}

// replaceAll replaces every match.
func (a *app) replaceAll(d *editor) {
	f := &a.find
	a.search(d)
	if len(f.matches) == 0 || f.re == nil {
		return
	}
	n := len(f.matches)
	with := a.replacement()
	// One step of undo: an edit per match, or of the whole text when
	// there are many.
	if n <= 500 {
		snap := d.buf.Snapshot()
		edits := make([]textEdit, n)
		for i, m := range f.matches {
			edits[i] = textEdit{m[0], m[1], f.re.ReplaceAllString(snap.Slice(m[0], m[1]), with)}
		}
		d.edit(d.sel, edits...)
	} else {
		text := d.buf.String()
		d.edit(d.sel, textEdit{0, d.buf.Len(), f.re.ReplaceAllString(text, with)})
	}
	a.search(d)
	a.fail("Replaced %d occurrences", n)
}

// replacement is the text a match is replaced with: as typed, or with
// $1 and the like expanded for a regular expression.
func (a *app) replacement() string { return replaceTemplate(a.find.with, a.find.regex) }

// replaceTemplate is the template a match is replaced by, for
// regexp.Expand and ReplaceAllString: with as typed, or with its $1 and
// the like when it replaces matches of a regular expression.
func replaceTemplate(with string, regex bool) string {
	if regex {
		return with
	}
	return strings.ReplaceAll(with, "$", "$$")
}

// matchRect is a match in view, in the content of the editor: its line,
// and where it starts and ends along it.
type matchRect struct {
	line   int
	x0, x1 float32
	cur    bool
}

// visibleMatches returns the matches on lines first up to last.
func (a *app) visibleMatches(d *editor, first, last int) []matchRect {
	f := &a.find
	if !f.open || f.doc != d.id || len(f.matches) == 0 {
		return nil
	}
	snap := d.buf.Snapshot()
	from, _ := snap.LineRange(first)
	_, to := snap.LineRange(last)
	i := sort.Search(len(f.matches), func(i int) bool { return f.matches[i][1] > from })
	var out []matchRect
	for ; i < len(f.matches) && f.matches[i][0] <= to && len(out) < 500; i++ {
		m := f.matches[i]
		if m[1] > snap.Len() {
			break // found in a longer text, before an edit
		}
		line := snap.LineAt(m[0])
		s, e := snap.LineRange(line)
		x0 := a.textWidth(snap.Slice(s, m[0]))
		x1 := a.textWidth(snap.Slice(s, min(m[1], e)))
		out = append(out, matchRect{line: line, x0: x0, x1: max(x1, x0+2), cur: i == f.cur})
	}
	return out
}

// textWidth measures a run of a line as the editor lays it out.
func (a *app) textWidth(s string) float32 { return a.font.width(s) }

// findCountW is the width of find's count of matches.
const findCountW = 74

// findView is the find widget, over the top right of the editor.
func (a *app) findView(c *ui.Context, d *editor) {
	f := &a.find
	if !f.open {
		return
	}
	p := a.pal
	a.searchIn(d, true)
	if f.reveal && f.cur >= 0 {
		m := f.matches[f.cur]
		d.selectRange(m[0], m[1], true)
		f.reveal = false
	}
	toggle := func(icon *ui.SVG, label string, on *bool) {
		if a.optionToggle(c, icon, label, on) {
			f.stale = true
		}
	}
	box := a.floating(c, ui.Column(c).Absolute().Top(4).Right(22).Width(430).Padding(5, 6).Gap(4).Radius(8), false).Transition(popIn).
		Role(ui.RoleToolbar).Label("Find")
	box.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			arrow := iconChevronRight
			if f.replacing {
				arrow = iconChevronDown
			}
			b := ui.ButtonBase(c).Size(18, 22).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label("Toggle Replace")
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Children(func() { ui.Icon(c, arrow).FontSize(13).TextColor(p.muted) })
			if b.Clicked() {
				f.replacing = !f.replacing
			}
			field := ui.Row(c).Grow(1).Height(26).Padding(0, 4, 0, 8).Gap(2).AlignItems(ui.Center).Radius(5).
				Background(p.editor).Border(1, p.border)
			field.Children(func() {
				in := a.textKeys(ui.TextInputBase(c.Key("find-query"), &f.query)).Bind(&f.input).Grow(1).FontSize(12.5).
					Placeholder("Find").Label("Find")
				if in.Focused() {
					field.Border(1, p.accentDim)
				}
				if in.Changed() {
					f.stale = true
				}
				if in.Shortcut(ui.Shift, ui.KeyEnter) {
					a.step(d, -1)
				} else if in.Submitted() {
					a.step(d, 1)
				}
				toggle(iconCase, "Match Case", &f.matchCase)
				toggle(iconWord, "Match Whole Word", &f.word)
				toggle(iconRegex, "Use Regular Expression", &f.regex)
			})
			count := "No results"
			switch {
			case f.err != "":
				count = f.err
			case f.query == "":
				count = ""
			case len(f.matches) >= maxMatches:
				count = fmt.Sprintf("%d of %d+", f.cur+1, maxMatches)
			case len(f.matches) > 0:
				count = fmt.Sprintf("%d of %d", f.cur+1, len(f.matches))
			}
			col := p.muted
			if count == "No results" || f.err != "" {
				col = p.danger
			}
			ui.Text(c, count).FontSize(11.5).TextColor(col).Width(findCountW).SingleLine().TextAlign(ui.Center)
			if a.iconButton(c, iconArrowUp, "Previous Match ("+keyText("Shift+Enter")+")", false).Disabled(len(f.matches) == 0).Clicked() {
				a.step(d, -1)
			}
			if a.iconButton(c, iconArrowDown, "Next Match (⏎)", false).Disabled(len(f.matches) == 0).Clicked() {
				a.step(d, 1)
			}
			if a.iconButton(c, iconX, "Close (Esc)", false).Clicked() {
				a.closeFind()
			}
		})
		if !f.replacing {
			return
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(0, 0, 0, 22).Children(func() {
			field := ui.Row(c).Grow(1).Height(26).Padding(0, 8).AlignItems(ui.Center).Radius(5).
				Background(p.editor).Border(1, p.border)
			field.Children(func() {
				in := a.textKeys(ui.TextInputBase(c.Key("find-with"), &f.with)).Grow(1).FontSize(12.5).Placeholder("Replace").Label("Replace")
				if in.Focused() {
					field.Border(1, p.accentDim)
				}
				if in.Submitted() {
					a.replaceOne(d)
				}
			})
			if a.iconButton(c, iconReplace, "Replace (⏎)", false).Disabled(len(f.matches) == 0).Clicked() {
				a.replaceOne(d)
			}
			if a.iconButton(c, iconReplaceAll, "Replace All", false).Disabled(len(f.matches) == 0).Clicked() {
				a.replaceAll(d)
			}
			// Under the count and the buttons after it, so that the fields
			// end alike: what is left of them past these two buttons.
			ui.Box(c).Width(findCountW + 3*26 + 4*4 - (2*26 + 3*4))
		})
	})
	if box.OverlayShortcut(0, ui.KeyEscape) || box.Shortcut(0, ui.KeyEscape) {
		a.closeFind()
	}
}

// optionToggle is a button of an option of find or search, as Match
// Case, shown pressed while *on is set. A click flips it, and reports so.
func (a *app) optionToggle(c *ui.Context, icon *ui.SVG, label string, on *bool) bool {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Size(22, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	if *on {
		b.Background(p.active).Border(1, p.accentDim)
		col = p.text
	} else if b.Hovered() {
		b.Background(p.hover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(col) })
	if b.Clicked() {
		*on = !*on
		return true
	}
	return false
}
