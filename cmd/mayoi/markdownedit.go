package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// mdLine is a line of Markdown taken apart: its quotes' >, the indent
// after them, and a heading's # or a list's marker and a task's box,
// before its text.
type mdLine struct {
	quote, indent string
	heading       int
	marker, box   string
	n             int // runes of what comes before body
	body          string
}

// mdLineHead is what starts a line of Markdown, in mdLine's parts.
var mdLineHead = regexp.MustCompile(`^((?:[ \t]*>[ \t]?)*)([ \t]*)(?:(#{1,6})(?:[ \t]+|$)|([-*+]|\d{1,9}[.)])(?:[ \t]+|$)(?:(\[[ xX]\])(?:[ \t]+|$))?)?`)

func parseMdLine(text string) mdLine {
	m := mdLineHead.FindStringSubmatchIndex(text)
	group := func(g int) string {
		if m[2*g] < 0 {
			return ""
		}
		return text[m[2*g]:m[2*g+1]]
	}
	l := mdLine{quote: group(1), indent: group(2), marker: group(4), box: group(5)}
	l.heading = len(group(3))
	l.n = utf8.RuneCountInString(text[:m[1]])
	l.body = text[m[1]:]
	return l
}

// prefix is the text of a line before its body, one space after each
// mark.
func (l mdLine) prefix() string {
	p := l.quote + l.indent
	if l.heading > 0 {
		p += strings.Repeat("#", l.heading) + " "
	}
	if l.marker != "" {
		p += l.marker + " "
	}
	if l.box != "" {
		p += l.box + " "
	}
	return p
}

func (l mdLine) list() bool { return l.marker != "" }

func (l mdLine) ordered() bool { return l.marker != "" && !strings.ContainsAny(l.marker, "-*+") }

// nextMarker is a list's marker for the item step after one with marker
// m: the same bullet, or the number step on, 1 at least.
func nextMarker(m string, step int) string {
	if !strings.ContainsAny(m, ".)") {
		return m
	}
	n, _ := strconv.Atoi(m[:len(m)-1])
	return strconv.Itoa(max(1, n+step)) + m[len(m)-1:]
}

// inFence reports whether a line of Markdown is inside a fenced code
// block, where its marks are code.
func inFence(snap ui.TextSnapshot, line int) bool {
	fence := ""
	for i := range line {
		t := strings.TrimLeft(snap.Line(i), " \t")
		switch {
		case fence == "":
			if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				fence = t[:len(t)-len(strings.TrimLeft(t, t[:1]))]
			}
		case strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]+" \t") == "":
			fence = ""
		}
	}
	return fence != ""
}

// mdItemLine returns the line of the caret when it is an item of a list
// or a quote outside code, unless the caret is in its marks and after is
// set.
func mdItemLine(d *editor, after bool) (s, e int, l mdLine, ok bool) {
	if !isMarkdown(d.document) {
		return 0, 0, l, false
	}
	snap := d.buf.Snapshot()
	a, b := min(d.sel[0], d.sel[1], snap.Len()), min(max(d.sel[0], d.sel[1]), snap.Len())
	line := snap.LineAt(a)
	if snap.LineAt(b) != line {
		return 0, 0, l, false
	}
	s, e = snap.LineRange(line)
	text := snap.Slice(s, e)
	l = parseMdLine(text)
	if !l.list() && l.quote == "" || after && a-s < l.n || liveRule.MatchString(text) || inFence(snap, line) {
		return 0, 0, l, false
	}
	return s, e, l, true
}

// mdNewline breaks an item of a list or a quote at the caret, the new
// line starting a next item, or on an empty item ends the list, or the
// quote, a level at a time. It reports whether it did.
func mdNewline(d *editor) bool {
	s, e, l, ok := mdItemLine(d, true)
	if !ok {
		return false
	}
	if strings.TrimSpace(l.body) == "" {
		var p string
		switch {
		case l.list() && l.indent != "":
			l.indent = l.indent[outdentWidth(l.indent, d.indent):]
			p = l.prefix()
		case l.list():
			p = l.quote
		default:
			q := strings.TrimRight(l.quote, " \t")
			p = strings.TrimRight(q[:len(q)-1], " \t")
			if p != "" {
				p += " "
			}
		}
		at := s + utf8.RuneCountInString(p)
		d.edit([2]int{at, at}, textEdit{s, e, p})
		return true
	}
	next := l
	next.heading = 0
	next.marker = nextMarker(l.marker, 1)
	if l.box != "" {
		next.box = "[ ]"
	}
	text := "\n" + next.prefix()
	a, b := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	caret := a + utf8.RuneCountInString(text)
	d.edit([2]int{caret, caret}, textEdit{a, b, text})
	return true
}

// mdInsertLine opens a line below an item of a list or a quote, or
// above, as the next item, or the one before. It reports whether it did.
func mdInsertLine(d *editor, above bool) bool {
	s, e, l, ok := mdItemLine(d, false)
	if !ok {
		return false
	}
	step := 1
	if above {
		step = -1
	}
	l.marker = nextMarker(l.marker, step)
	if l.box != "" {
		l.box = "[ ]"
	}
	p := l.prefix()
	n := utf8.RuneCountInString(p)
	if above {
		d.edit([2]int{s + n, s + n}, textEdit{s, s, p + "\n"})
		return true
	}
	d.edit([2]int{e + 1 + n, e + 1 + n}, textEdit{e, e, "\n" + p})
	return true
}

// mdIndent indents the items of a list a selection covers a level, or
// outdents them, after their quotes' >, wherever the caret is in them.
// It reports whether it did: not unless every line is an item.
func mdIndent(d *editor, outdent bool) bool {
	if !isMarkdown(d.document) {
		return false
	}
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	if inFence(snap, first) {
		return false
	}
	type item struct {
		s int
		l mdLine
	}
	var items []item
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		if strings.TrimSpace(text) == "" {
			continue
		}
		l := parseMdLine(text)
		if !l.list() {
			return false
		}
		items = append(items, item{s, l})
	}
	if len(items) == 0 {
		return false
	}
	unit := d.indent.unit()
	sel := d.sel
	var edits []textEdit
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		at := it.s + utf8.RuneCountInString(it.l.quote)
		if outdent {
			if n := outdentWidth(it.l.indent, d.indent); n > 0 {
				edits = append(edits, textEdit{at, at + n, ""})
				sel = shiftSel(sel, at, -n)
			}
			continue
		}
		edits = append(edits, textEdit{at, at, unit})
		sel = shiftSel(sel, at, utf8.RuneCountInString(unit))
	}
	if len(edits) > 0 {
		d.edit(sel, edits...)
	}
	return true
}

// mdRow is a line of a selection being rewritten.
type mdRow struct {
	s     int
	blank bool
	l     mdLine
}

// rewriteMd gives the lines of a selection of Markdown new marks, those
// rewrite returns for them, as one step of undo, keeping the selection on
// their text. rows are the lines but blank ones, which rewrite only gets
// when the selection is of one line, or when blanks is set.
func rewriteMd(d *editor, blanks bool, rewrite func(rows []mdRow) []string) {
	if !isMarkdown(d.document) {
		return
	}
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	var rows []mdRow
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		blank := strings.TrimSpace(text) == ""
		if blank && first != last && !blanks {
			continue
		}
		rows = append(rows, mdRow{s, blank, parseMdLine(text)})
	}
	if len(rows) == 0 {
		return
	}
	prefixes := rewrite(rows)
	sel := d.sel
	ranged := sel[0] != sel[1]
	var edits []textEdit
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		p := prefixes[i]
		old := snap.Slice(r.s, r.s+r.l.n)
		if p == old {
			continue
		}
		edits = append(edits, textEdit{r.s, r.s + r.l.n, p})
		n := utf8.RuneCountInString(p)
		for k, x := range sel {
			switch {
			case x < r.s:
			case ranged && x == r.s && x == min(sel[0], sel[1]):
				// A selection from the start of a line keeps its marks.
			case x >= r.s+r.l.n:
				sel[k] = x + n - r.l.n
			default:
				sel[k] = r.s + min(x-r.s, n)
			}
		}
	}
	if len(edits) > 0 {
		d.edit(sel, edits...)
	}
}

// toggleMdTask makes the lines of a selection tasks, checks them, then
// makes them items of a list again.
func toggleMdTask(d *editor) {
	ticked := -1
	rewriteMd(d, false, func(rows []mdRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			l := r.l
			switch l.box {
			case "":
				if !l.list() {
					l.marker = "-"
				}
				l.heading, l.box = 0, "[ ]"
			case "[ ]":
				l.box = "[x]"
			default:
				l.box = ""
			}
			if len(rows) == 1 && r.l.box != "" && l.box != "" {
				ticked = r.s + utf8.RuneCountInString(l.quote+l.indent+l.marker+" ")
			}
			out[i] = l.prefix()
		}
		return out
	})
	if ticked >= 0 {
		d.liveTicked, d.tickNow = ticked, true
	}
}

// mdTaskKey reports whether ⌘Enter toggles a task in an editor: in
// Markdown, its caret outside code.
func mdTaskKey(d *editor) bool {
	if !isMarkdown(d.document) {
		return false
	}
	snap := d.buf.Snapshot()
	return !inFence(snap, snap.LineAt(min(d.sel[1], snap.Len())))
}

// toggleMdList makes the lines of a selection items of a bulleted list,
// those of one a numbered list, and those of a numbered list text, as
// its first line is.
func toggleMdList(d *editor) {
	rewriteMd(d, false, func(rows []mdRow) []string {
		first := rows[0].l
		out := make([]string, len(rows))
		for i, r := range rows {
			l := r.l
			l.heading = 0
			switch {
			case first.ordered():
				l.marker, l.box = "", ""
			case first.list():
				l.marker = strconv.Itoa(i+1) + "."
			default:
				l.marker = "-"
			}
			out[i] = l.prefix()
		}
		return out
	})
}

// toggleMdQuote quotes the lines of a selection, or takes a level of
// quote off them when all are quoted.
func toggleMdQuote(d *editor) {
	rewriteMd(d, true, func(rows []mdRow) []string {
		all := true
		for _, r := range rows {
			if !r.blank && r.l.quote == "" {
				all = false
			}
		}
		out := make([]string, len(rows))
		for i, r := range rows {
			l := r.l
			switch {
			case all && l.quote != "":
				l.quote = strings.TrimLeft(l.quote, " \t")[1:]
				l.quote = strings.TrimPrefix(strings.TrimPrefix(l.quote, " "), "\t")
			case all:
			case r.blank && len(rows) > 1:
				l.quote = ">" + l.quote
			default:
				l.quote = "> " + l.quote
			}
			out[i] = l.prefix()
		}
		return out
	})
}

// toggleMdHeading makes the lines of a selection headings of a level, or
// text when they are.
func toggleMdHeading(d *editor, level int) {
	rewriteMd(d, false, func(rows []mdRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			l := r.l
			l.marker, l.box = "", ""
			if l.heading == level {
				l.heading = 0
			} else {
				l.heading = level
			}
			out[i] = l.prefix()
		}
		return out
	})
}

// clearMdFormatting takes the quotes, heading, list marker and box off
// the lines of a selection.
func clearMdFormatting(d *editor) {
	rewriteMd(d, false, func(rows []mdRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			l := r.l
			if l.quote != "" || l.heading > 0 || l.list() {
				l = mdLine{}
			}
			out[i] = l.prefix()
		}
		return out
	})
}

// wikiOpen is the [[ that opens a wiki link, with the ! of an embed.
var wikiOpen = regexp.MustCompile(`!?\[\[`)

// toggleMdEmbed makes the wiki links of the lines of a selection embeds,
// or links again when one of a line's is an embed.
func toggleMdEmbed(d *editor) {
	if !isMarkdown(d.document) {
		return
	}
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	sel := d.sel
	var edits []textEdit
	for i := last; i >= first; i-- {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		ms := wikiOpen.FindAllStringIndex(text, -1)
		embed := strings.Contains(text, "![[")
		for k := len(ms) - 1; k >= 0; k-- {
			at := s + utf8.RuneCountInString(text[:ms[k][0]])
			bang := text[ms[k][0]] == '!'
			switch {
			case embed && bang:
				edits = append(edits, textEdit{at, at + 1, ""})
				sel = shiftSel(sel, at, -1)
			case !embed:
				edits = append(edits, textEdit{at, at, "!"})
				sel = shiftSel(sel, at, 1)
			}
		}
	}
	if len(edits) > 0 {
		d.edit(sel, edits...)
	}
}

// joinSelLines joins the lines of a selection into one, or the caret's
// line with the next, a space between their trimmed text, runs of spaces
// one.
func joinSelLines(d *editor) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	if first == last {
		if last+1 >= snap.LineCount() {
			return
		}
		last++
	}
	bs, _ := snap.LineRange(first)
	_, be := snap.LineRange(last)
	lines := strings.Split(snap.Slice(bs, be), "\n")
	indent := leading(lines[0])
	var parts []string
	for _, l := range lines {
		if t := strings.Join(strings.Fields(l), " "); t != "" {
			parts = append(parts, t)
		}
	}
	text := indent + strings.Join(parts, " ")
	end := bs + utf8.RuneCountInString(text)
	if d.sel[0] != d.sel[1] {
		d.edit([2]int{bs, end}, textEdit{bs, be, text})
		return
	}
	// The caret goes where the first line meets the next.
	join := bs + utf8.RuneCountInString(indent)
	if len(parts) > 0 {
		join += utf8.RuneCountInString(parts[0])
	}
	d.edit([2]int{join, join}, textEdit{bs, be, text})
}
