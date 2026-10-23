package main

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/locales"
)

// indentStyle is how a document indents: with tabs, or with size spaces.
type indentStyle struct {
	tabs bool
	size int
}

// unit is the text of one level of indentation.
func (s indentStyle) unit() string {
	if s.tabs {
		return "\t"
	}
	return strings.Repeat(" ", s.size)
}

// cols is how many columns one level takes.
func (s indentStyle) cols() int {
	if s.tabs {
		return tabWidth
	}
	return s.size
}

func (s indentStyle) String() string {
	if s.tabs {
		return locales.IndentTabs()
	}
	return locales.IndentSpaces(s.size)
}

// detectIndent finds how a document indents from its first lines, or,
// when they do not tell, from what its kind of file mostly uses.
func detectIndent(snap ui.TextSnapshot, path string) indentStyle {
	tabs, spaces := 0, 0
	var widths [9]int // how often an indent grows by n spaces
	prev := 0
	for i := range min(snap.LineCount(), 2000) {
		s, e := snap.LineRange(i)
		line := snap.Slice(s, min(e, s+80))
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch line[0] {
		case '\t':
			tabs++
			prev = 0
			continue
		case ' ':
			spaces++
		}
		n := len(line) - len(strings.TrimLeft(line, " "))
		if d := n - prev; d >= 2 && d <= 8 {
			widths[d]++
		}
		prev = n
	}
	if tabs > spaces && tabs > 0 {
		return indentStyle{tabs: true, size: tabWidth}
	}
	if spaces > 0 {
		best := 0
		for n := 2; n <= 8; n++ {
			if widths[n] > widths[best] {
				best = n
			}
		}
		if best != 0 {
			return indentStyle{size: best}
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".mod":
		return indentStyle{tabs: true, size: tabWidth}
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".json", ".yaml", ".yml", ".html", ".css", ".scss", ".vue", ".svelte", ".rb", ".lua", ".dart", ".nix":
		return indentStyle{size: 2}
	}
	if strings.EqualFold(filepath.Base(path), "makefile") {
		return indentStyle{tabs: true, size: tabWidth}
	}
	return indentStyle{size: 4}
}

// lineComments are the line comments of languages, by Chroma's name.
var lineComments = map[string]string{
	"Go": "//", "C": "//", "C++": "//", "C#": "//", "Java": "//", "JavaScript": "//", "TypeScript": "//",
	"TSX": "//", "JSX": "//", "Rust": "//", "Swift": "//", "Kotlin": "//", "Scala": "//", "Dart": "//",
	"Zig": "//", "Objective-C": "//", "PHP": "//", "Groovy": "//", "Protocol Buffer": "//", "GLSL": "//",
	"JSON5": "//", "SCSS": "//", "Sass": "//", "Less": "//", "Odin": "//", "V": "//", "Gleam": "//",
	"Python": "#", "Python 2": "#", "Bash": "#", "Zsh": "#", "Fish": "#", "Ruby": "#", "Perl": "#",
	"YAML": "#", "TOML": "#", "Makefile": "#", "Docker": "#", "R": "#", "Elixir": "#", "Nix": "#",
	"PowerShell": "#", "CMake": "#", "Nim": "#", "Julia": "#", "Crystal": "#", "INI": ";", "Terraform": "#",
	"HCL": "#", "GDScript": "#", "Starlark": "#",
	"SQL": "--", "PostgreSQL SQL dialect": "--", "MySQL": "--", "Lua": "--", "Haskell": "--", "Elm": "--",
	"Common Lisp": ";", "Clojure": ";", "Scheme": ";", "Racket": ";", "Emacs Lisp": ";", "Fennel": ";",
	"VimL": `"`, "Erlang": "%", "TeX": "%", "MATLAB": "%", "Fortran": "!",
}

// commentFor returns the line comment of a document's language, "" when
// it has none.
func commentFor(d *document) string {
	if d.hl == nil {
		return ""
	}
	return lineComments[highlight.Name(d.hl.Lexer())]
}

// leading returns the whitespace that starts s.
func leading(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// lineSpan returns the lines a selection covers: a selection of several
// lines that ends at the start of one leaves that line out.
func lineSpan(snap ui.TextSnapshot, sel [2]int) (first, last int) {
	a, b := min(sel[0], sel[1]), max(sel[0], sel[1])
	n := snap.Len()
	a, b = min(a, n), min(b, n)
	first, last = snap.LineAt(a), snap.LineAt(b)
	if last > first {
		if s, _ := snap.LineRange(last); s == b {
			last--
		}
	}
	return first, last
}

// shiftSel moves the offsets of a selection at or after at by delta, and
// those inside a removed run [at, at-delta) to at.
func shiftSel(sel [2]int, at, delta int) [2]int {
	for i, x := range sel {
		switch {
		case x < at:
		case delta < 0 && x < at-delta:
			sel[i] = at
		default:
			sel[i] = x + delta
		}
	}
	return sel
}

// indentLines indents the lines of a selection one level, or outdents
// them, keeping the selection on the same text.
func indentLines(d *editor, outdent bool) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	unit := d.indent.unit()
	sel := d.sel
	var edits []textEdit
	// From the last line, so that the offsets of the others stay.
	for i := last; i >= first; i-- {
		s, e := snap.LineRange(i)
		line := snap.Slice(s, e)
		if outdent {
			n := outdentWidth(line, d.indent)
			if n > 0 {
				edits = append(edits, textEdit{s, s + n, ""})
				sel = shiftSel(sel, s, -n)
			}
			continue
		}
		if strings.TrimSpace(line) == "" && first != last {
			continue // blank lines stay blank
		}
		edits = append(edits, textEdit{s, s, unit})
		// A caret at the start of the line stays before the indent when
		// it is the selection's start.
		for k, x := range sel {
			if x > s || x == s && (sel[0] == sel[1] || x != min(sel[0], sel[1])) {
				sel[k] = x + len(unit)
			}
		}
	}
	if len(edits) > 0 {
		d.edit(sel, edits...)
	}
}

// outdentWidth is how many runes of a line's indent one level is: a tab,
// or up to a level of spaces.
func outdentWidth(line string, style indentStyle) int {
	if strings.HasPrefix(line, "\t") {
		return 1
	}
	n := 0
	for n < len(line) && n < style.cols() && line[n] == ' ' {
		n++
	}
	if n < len(line) && n < style.cols() && line[n] == '\t' {
		n++
	}
	return n
}

// tab indents: the lines of a selection over several, or else the caret
// to the next stop.
func tab(d *editor, outdent bool) {
	snap := d.buf.Snapshot()
	a, b := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	if outdent || snap.LineAt(min(a, snap.Len())) != snap.LineAt(min(b, snap.Len())) {
		indentLines(d, outdent)
		return
	}
	text := "\t"
	if !d.indent.tabs {
		s, _ := snap.LineRange(snap.LineAt(a))
		col := columns(snap.Slice(s, a))
		text = strings.Repeat(" ", d.indent.size-col%d.indent.size)
	}
	at := a + utf8.RuneCountInString(text)
	d.edit([2]int{at, at}, textEdit{a, b, text})
}

// pairs are the brackets an indented block goes between.
var pairs = map[byte]byte{'{': '}', '[': ']', '(': ')'}

// newline breaks the line at the caret, indenting the new line as the
// one it leaves, and a level more after an opening bracket or a colon
// that starts a block. Between brackets, the closing one goes to a line
// of its own.
func newline(d *editor) {
	snap := d.buf.Snapshot()
	a, b := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	line := snap.LineAt(a)
	s, e := snap.LineRange(line)
	before := snap.Slice(s, a)
	after := snap.Slice(min(b, e), e)
	indent := leading(before)
	if strings.TrimSpace(before) == "" {
		// On a line of only indent the caret keeps it all.
		indent = before
	}
	trimmed := strings.TrimRight(before, " \t")
	text := "\n" + indent
	caret := a + utf8.RuneCountInString(text)
	if trimmed != "" {
		last := trimmed[len(trimmed)-1]
		closer, opens := pairs[last]
		block := opens || last == ':' && opensColonBlock(d.document)
		if block {
			text += d.indent.unit()
			caret = a + utf8.RuneCountInString(text)
			if rest := strings.TrimLeft(after, " \t"); opens && rest != "" && rest[0] == closer {
				text += "\n" + indent
			}
		}
	}
	// The spaces the caret leaves at the end of the line go.
	trim := utf8.RuneCountInString(before) - utf8.RuneCountInString(trimmed)
	if trimmed == "" {
		trim = 0
	}
	start := a - trim
	d.edit([2]int{caret - trim, caret - trim}, textEdit{start, b, text})
}

// opensColonBlock reports whether a colon at the end of a line starts a
// block in the document's language.
func opensColonBlock(d *document) bool {
	if d.hl == nil {
		return false
	}
	switch name := highlight.Name(d.hl.Lexer()); name {
	case "Python", "Python 2", "YAML", "Nim", "GDScript", "Starlark", "CoffeeScript":
		return true
	}
	return false
}

// toggleComment comments the lines of the selection out, or in when all
// of them are.
func toggleComment(d *editor) {
	mark := commentFor(d.document)
	if mark == "" {
		return
	}
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	type ln struct {
		s      int
		indent int // runes
		text   string
	}
	var lines []ln
	all := true
	minIndent := -1
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		if strings.TrimSpace(text) == "" {
			continue
		}
		ind := utf8.RuneCountInString(leading(text))
		lines = append(lines, ln{s, ind, text})
		if !strings.HasPrefix(strings.TrimLeft(text, " \t"), mark) {
			all = false
		}
		if minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if len(lines) == 0 {
		return
	}
	sel := d.sel
	var edits []textEdit
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if all {
			at := l.s + l.indent
			n := utf8.RuneCountInString(mark)
			if strings.HasPrefix(strings.TrimLeft(l.text, " \t")[len(mark):], " ") {
				n++
			}
			edits = append(edits, textEdit{at, at + n, ""})
			sel = shiftSel(sel, at, -n)
		} else {
			at := l.s + minIndent
			edits = append(edits, textEdit{at, at, mark + " "})
			n := utf8.RuneCountInString(mark) + 1
			for k, x := range sel {
				if x >= at && !(x == at && sel[0] != sel[1] && x == min(sel[0], sel[1]) && minIndent == 0) {
					sel[k] = x + n
				}
			}
		}
	}
	d.edit(sel, edits...)
}

// moveLines moves the lines of the selection up a line, or down.
func moveLines(d *editor, down bool) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	lines := snap.LineCount()
	if !down && first == 0 || down && last == lines-1 {
		return
	}
	bs, _ := snap.LineRange(first)
	_, be := snap.LineRange(last)
	block := snap.Slice(bs, be)
	if down {
		ns, ne := snap.LineRange(last + 1)
		next := snap.Slice(ns, ne)
		d.edit(shiftSel(d.sel, 0, utf8.RuneCountInString(next)+1),
			textEdit{bs, ne, next + "\n" + block})
		return
	}
	ps, _ := snap.LineRange(first - 1)
	prev := snap.Slice(ps, bs-1)
	n := utf8.RuneCountInString(prev) + 1
	d.edit(shiftSel(d.sel, 0, -n), textEdit{ps, be, block + "\n" + prev})
}

// copyLines puts a copy of the lines of the selection above them, or
// below, selecting the copy.
func copyLines(d *editor, down bool) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	bs, _ := snap.LineRange(first)
	_, be := snap.LineRange(last)
	block := snap.Slice(bs, be)
	n := utf8.RuneCountInString(block) + 1
	if down {
		d.edit(shiftSel(d.sel, 0, n), textEdit{be, be, "\n" + block})
		return
	}
	d.edit(d.sel, textEdit{bs, bs, block + "\n"})
}

// deleteLines deletes the lines of the selection.
func deleteLines(d *editor) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	bs, _ := snap.LineRange(first)
	_, be := snap.LineRange(last)
	start, end := bs, be
	if last+1 < snap.LineCount() {
		end++ // and the newline after them
	} else if first > 0 {
		start-- // the last lines: the newline before them
	}
	// The caret goes to the same column of the line that takes their
	// place, or to the end of it.
	caret := min(d.sel[1], snap.Len())
	cs, _ := snap.LineRange(snap.LineAt(caret))
	col := caret - cs
	d.edit([2]int{start, start}, textEdit{start, end, ""})
	after := d.buf.Snapshot()
	s, e := after.LineRange(min(first, after.LineCount()-1))
	c := s + min(col, e-s)
	d.selectRange(c, c, false)
	d.sel = [2]int{c, c}
}

// cut cuts the selection, or the caret's line when there is none, as in
// VS Code.
func (a *app) cut(d *editor) {
	if d.locked() || a.settings.Vim && d.vim.mode != vimInsert {
		return
	}
	snap := d.buf.Snapshot()
	if snap.Len() == 0 {
		return
	}
	if start, end := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1]); start != end {
		a.services.WriteClipboard(snap.Slice(start, end))
		d.edit([2]int{start, start}, textEdit{start, end, ""})
		return
	}
	// The line with its newline, the last one too, so that it pastes back
	// as a line at the start of another.
	start, end := snap.LineRange(snap.LineAt(min(d.sel[1], snap.Len())))
	a.services.WriteClipboard(snap.Slice(start, end) + "\n")
	deleteLines(d)
}

// insertLine opens a line below the caret's, or above, with its indent.
func insertLine(d *editor, above bool) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	line := snap.LineAt(caret)
	s, e := snap.LineRange(line)
	text := snap.Slice(s, e)
	indent := leading(text)
	if t := strings.TrimRight(text, " \t"); !above && t != "" {
		if _, opens := pairs[t[len(t)-1]]; opens {
			indent += d.indent.unit()
		}
	}
	n := utf8.RuneCountInString(indent)
	if above {
		d.edit([2]int{s + n, s + n}, textEdit{s, s, indent + "\n"})
		return
	}
	d.edit([2]int{e + 1 + n, e + 1 + n}, textEdit{e, e, "\n" + indent})
}

// selectLines selects the lines of the selection, newline and all; again,
// the line below as well.
func selectLines(d *editor) {
	snap := d.buf.Snapshot()
	first, last := lineSpan(snap, d.sel)
	bs, _ := snap.LineRange(first)
	_, be := snap.LineRange(last)
	if a, b := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1]); a == bs && b == be+1 && last+1 < snap.LineCount() {
		_, be = snap.LineRange(last + 1)
	}
	if be < snap.Len() {
		be++
	}
	d.selectRange(bs, be, false)
}

// home moves the caret to the first character of its line, or to the
// start of the line when it is there, selecting as it goes with shift.
func home(d *editor, extend bool) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	s, e := snap.LineRange(snap.LineAt(caret))
	text := snap.Slice(s, e)
	first := s + utf8.RuneCountInString(leading(text))
	to := first
	if caret == first {
		to = s
	}
	anchor := to
	if extend {
		anchor = d.sel[0]
	}
	d.selectRange(anchor, to, false)
}

// swipeSideways takes a swipe mostly sideways for the scroll state s:
// a trackpad's sideways push moves the text sideways only, as in VS
// Code — the text area would take the whole of it for the little it
// goes down, and the text stop and drift. Taken at an edge too, or
// wrapped, so that the text stays put. Shift is left out: a mouse
// scrolls sideways with Shift+wheel, which the caller may route its own
// way. Reports whether the swipe was taken.
func (a *app) swipeSideways(s *ui.ScrollState, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll || ev.Mods&ui.Shift != 0 || max(ev.DX, -ev.DX) <= max(ev.DY, -ev.DY) {
		return false
	}
	if x := max(0, min(s.X+ev.DX, s.MaxX)); x != s.X {
		s.X = x
		a.invalidate()
	}
	return true
}

// editorKey takes the keys of the editor that the app handles itself,
// before its text area: they queue an edit for the next frame.
func (a *app) editorKey(d *editor, ev ui.InputEvent) bool {
	if a.swipeSideways(&d.hscroll, ev) {
		return true
	}
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 && len(d.sticky) > 0 && a.font.lineH > 0 {
		// A click on a line stuck at the top goes to it.
		if k := int(ev.Y / a.font.lineH); ev.Y >= 0 && k < len(d.sticky) {
			line := d.sticky[k]
			d.cmds = append(d.cmds, func(d *editor) { a.jumpToScope(d, line) })
			return true
		}
	}
	// A click on the mark of lines changed since the last commit shows
	// what they were.
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 && ev.Clicks == 1 && a.gutterClick(d, ev.X, ev.Y) {
		a.invalidate()
		return true
	}
	if ev.Kind == ui.InputPointerDown && a.settings.Vim && d.vim.mode.visual() {
		// A click ends visual mode; a drag selects anew.
		d.vim.lastVisual = vimVisualArea{d.vim.anchor, d.vim.cursor, d.vim.mode}
		d.vim.mode, d.vim.keys = vimNormal, d.vim.keys[:0]
	}
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if ev.Key == ui.KeyEscape && ev.Mods == 0 && d.peek >= 0 {
		d.peek = -1
		return true
	}
	if a.settings.Vim && a.vimKey(d, ev) {
		return true
	}
	var cmd func(d *editor)
	cmdKey := ui.Cmd
	switch m := ev.Mods; {
	case ev.Key == ui.KeyX && m == cmdKey:
		cmd = a.cut
	case ev.Key == ui.KeyTab && (m == 0 || m == ui.Shift):
		outdent := m == ui.Shift
		cmd = func(d *editor) { tab(d, outdent) }
	case ev.Key == ui.KeyEnter && m == 0:
		cmd = newline
	case ev.Key == ui.KeyEnter && (m == cmdKey || m == cmdKey|ui.Shift):
		above := m&ui.Shift != 0
		cmd = func(d *editor) { insertLine(d, above) }
	case ev.Key == ui.KeyZ && m == cmdKey:
		cmd = func(d *editor) { d.undo() }
	case ev.Key == ui.KeyZ && m == cmdKey|ui.Shift, ev.Key == ui.KeyY && m == ui.Ctrl && cmdKey == ui.Ctrl:
		cmd = func(d *editor) { d.redo() }
	case (ev.Key == ui.KeyUp || ev.Key == ui.KeyDown) && (m == ui.Alt || m == ui.Alt|ui.Shift):
		down := ev.Key == ui.KeyDown
		if m&ui.Shift != 0 {
			cmd = func(d *editor) { copyLines(d, down) }
		} else {
			cmd = func(d *editor) { moveLines(d, down) }
		}
	case ev.Key == ui.KeyHome && (m == 0 || m == ui.Shift),
		ev.Key == ui.KeyLeft && cmdKey == ui.Super && (m == ui.Super || m == ui.Super|ui.Shift):
		extend := m&ui.Shift != 0
		cmd = func(d *editor) { home(d, extend) }
	default:
		return false
	}
	d.cmds = append(d.cmds, cmd)
	return true
}

// editActive queues an edit of the editor shown for its next frame.
func (a *app) editActive(cmd func(d *editor)) func() {
	return func() {
		if d := a.textEditor(); d != nil {
			a.queueEdit(d, cmd)
		}
	}
}

// queueEdit queues an edit of an editor for its next frame.
func (a *app) queueEdit(d *editor, cmd func(d *editor)) {
	d.cmds = append(d.cmds, cmd)
	a.invalidate()
}

// jumpToScope puts the caret at the start of the text of a line opening
// a block, scrolling it to the top of the editor, under the lines that
// stick above it.
func (a *app) jumpToScope(d *editor, line int) {
	snap := d.buf.Snapshot()
	line = min(max(line, 0), snap.LineCount()-1)
	s, e := snap.LineRange(line)
	at := s + utf8.RuneCountInString(leading(snap.Slice(s, e)))
	above := float32(len(scopes(snap, line)))
	d.vscroll.Y = max(0, a.geometry(d, snap.LineCount()).top(line)-above*a.font.lineH)
	d.selectRange(at, at, false)
	d.vscroll.Focus()
}

// reindent converts the indent of every line to a style, keeping how
// deep each is, and takes the style for the document.
func reindent(d *editor, style indentStyle) {
	snap := d.buf.Snapshot()
	from := d.indent.cols()
	var edits []textEdit
	for i := range snap.LineCount() {
		s, e := snap.LineRange(i)
		lead := leading(snap.Slice(s, min(e, s+400)))
		if lead == "" {
			continue
		}
		// Each level of the old style becomes one of the new; what is left
		// over stays spaces.
		cols := columns(lead)
		levels, rest := cols/max(from, 1), cols%max(from, 1)
		want := strings.Repeat(style.unit(), levels) + strings.Repeat(" ", rest)
		if want != lead {
			edits = append(edits, textEdit{s, s + len(lead), want})
		}
	}
	d.indent = style
	if len(edits) > 0 {
		d.edit(d.sel, edits...)
	}
}

// goToBracket moves the caret to the bracket paired with the one beside
// it.
func goToBracket(d *editor) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	at, pair, ok := matchBracket(snap, caret)
	if !ok {
		return
	}
	// To the same side of the pair as the caret is of the bracket.
	to := pair
	if caret > at {
		to = pair + 1
	}
	d.selectRange(to, to, false)
}
