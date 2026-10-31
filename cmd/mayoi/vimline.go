package main

import (
	"mayoi/internal/remote"
	"mayoi/locales"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// vimLine is vim mode's command line, which / and ? open to search and :
// to run a command, in a bar over the bottom of the editor.
type vimLine struct {
	d     *editor // the editor it is for, nil while closed
	kind  rune    // '/', '?' or ':'
	text  string
	input ui.Handle
	// cmd is the count and the operator typed before / or ?, which the
	// search ends; mode the mode it was opened in.
	cmd  vimCmd
	mode vimMode
	// from is the selection it was opened at, which the cursor goes back
	// to as the search typed moves it, and as it is closed.
	from [2]int
	// hist is the entry of the history shown, as the arrows go through it:
	// len(history) for the text typed.
	hist int
	// shook is when Return found nothing, which shakes the bar.
	shook time.Time
}

// vimSearch is the pattern searched last, which n and N search again.
type vimSearch struct {
	pattern string
	back    bool
	// show is set while its matches are highlighted: from a search until
	// Esc in normal mode, or :noh.
	show bool
	// re is pattern compiled, of key.
	key string
	re  *regexp.Regexp
}

// vimMessage is what a command of vim mode says in the status bar for a
// while, after the mode: a search that went round, or one that failed.
type vimMessage struct {
	text string
	err  bool
	at   time.Time
}

// vimCounted is the count of the matches last found, which the command
// line shows every frame: of a pattern, in a version of a document, at a
// match.
type vimCounted struct {
	pattern    string
	doc        int
	version    uint64
	at         int
	cur, total int
}

// vimCountOf counts the matches of re, as vimMatchCount does, again only
// when the pattern, the text or the match changed.
func (a *app) vimCountOf(d *editor, snap ui.TextSnapshot, pattern string, re *regexp.Regexp, at int) (cur, total int) {
	k := &a.vimCount
	if k.pattern != pattern || k.doc != d.id || k.version != snap.Version() || k.at != at {
		cur, total := vimMatchCount(snap, re, at)
		*k = vimCounted{pattern, d.id, snap.Version(), at, cur, total}
	}
	return k.cur, k.total
}

// vimMessageFor is how long a message of vim mode shows.
const vimMessageFor = 4 * time.Second

// vimSay shows a message of vim mode in the status bar.
func (a *app) vimSay(text string, err bool) {
	if err {
		a.vimFailed = true // which ends the keys playing
	}
	a.vimMsg = vimMessage{text: text, err: err, at: time.Now()}
	a.invalidate()
	// A frame once it is over, which leaves it out.
	time.AfterFunc(vimMessageFor+10*time.Millisecond, func() { a.post(func() {}) })
}

// vimHistMax is how many searches, and commands, the line remembers.
const vimHistMax = 100

// vimHistory is the history of the searches, or of the commands.
func (a *app) vimHistory(kind rune) *[]string {
	if kind == ':' {
		return &a.vimHist[1]
	}
	return &a.vimHist[0]
}

// vimOpenLine opens the command line of kind for the editor, with the
// count and operator of a search typed before it.
func (a *app) vimOpenLine(d *editor, kind rune, cmd vimCmd, text string) {
	l := &a.vimLine
	*l = vimLine{d: d, kind: kind, text: text, cmd: cmd, mode: d.vim.mode, from: d.sel, hist: len(*a.vimHistory(kind))}
	l.input.Focus()
	a.invalidate()
}

// vimCloseLine closes the command line, the keyboard back to its editor,
// the cursor back where it was unless keep.
func (a *app) vimCloseLine(keep bool) {
	l := &a.vimLine
	d := l.d
	if d == nil {
		return
	}
	if !keep && d.sel != l.from {
		d.selectRange(l.from[0], l.from[1], false)
		d.sel = l.from
	}
	l.d = nil
	d.focusNext = true
	a.invalidate()
}

// vimSubmit runs what the command line has: a search, as the motion of
// the operator before it, or a command.
func (a *app) vimSubmit() {
	l := &a.vimLine
	d, kind, text := l.d, l.kind, l.text
	if d == nil {
		return
	}
	a.vimRecordText(text + "\r")
	if text != "" {
		h := a.vimHistory(kind)
		*h = slices.DeleteFunc(*h, func(s string) bool { return s == text })
		*h = append(*h, text)
		if len(*h) > vimHistMax {
			*h = (*h)[len(*h)-vimHistMax:]
		}
	}
	if kind == ':' {
		a.vimCloseLine(false)
		a.queueEdit(d, func(d *editor) { a.vimEx(d, text) })
		return
	}
	pattern := text
	if pattern == "" {
		pattern = a.vimSearch.pattern // the last, as vim searches again
	}
	if pattern == "" {
		a.vimCloseLine(false)
		return
	}
	if _, err := a.vimCompile(pattern); err != nil {
		l.shook = time.Now()
		a.vimSay(locales.FindInvalid(), true)
		return
	}
	a.vimCloseLine(false)
	a.vimSearch.pattern, a.vimSearch.back, a.vimSearch.show = pattern, kind == '?', true
	cmd := l.cmd
	cmd.key = "n"
	a.vimRunCmd(d, cmd, l.mode)
}

// vimRunCmd runs a command typed whole in mode, as vimKey does: in the
// next frame, with the mode it leaves set now.
func (a *app) vimRunCmd(d *editor, cmd vimCmd, mode vimMode) {
	v := &d.vim
	next := cmd.next(mode)
	if cmd.key == "gv" && v.lastVisual.mode.visual() {
		next = v.lastVisual.mode
	}
	if mode.visual() && !next.visual() {
		v.lastVisual = vimVisualArea{v.anchor, v.cursor, mode}
	}
	v.mode = next
	a.queueEdit(d, func(d *editor) {
		if mode.visual() {
			a.vimVisualRun(d, cmd, mode, next)
		} else {
			a.vimNormalRun(d, cmd, next)
		}
		if next == vimNormal {
			vimSettle(d)
		}
	})
}

// vimCompile compiles a pattern of vim's, kept for the next search of it.
func (a *app) vimCompile(pattern string) (*regexp.Regexp, error) {
	s := &a.vimSearch
	if s.re != nil && s.key == pattern {
		return s.re, nil
	}
	re, err := vimRegexp(pattern)
	if err != nil {
		return nil, err
	}
	s.key, s.re = pattern, re
	return re, nil
}

// vimRegexp compiles a pattern as vim reads it, magic, into Go's syntax:
// \( \) \| \+ \? \= and \{ are groups, choices and counts, which ( ) | +
// ? and { are not; \< and \> are the ends of words; \v reads the rest as
// Go does. Case is ignored unless the pattern has a capital, or \C.
func vimRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	rs := []rune(pattern)
	fold, very, brace, class := true, false, false, false
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if class {
			// Inside [...] as it is, to the bracket that ends it.
			b.WriteRune(r)
			if r == '\\' && i+1 < len(rs) {
				i++
				b.WriteRune(rs[i])
			} else if r == ']' {
				class = false
			}
			continue
		}
		if r == '\\' && i+1 < len(rs) {
			i++
			n := rs[i]
			switch n {
			case 'c':
				fold = true
				continue
			case 'C':
				fold = false
				continue
			case 'v':
				very = true
				continue
			case 'm', 'M', 'V':
				very = false
				continue
			case '<', '>':
				b.WriteString(`\b`)
			case '=':
				if very {
					b.WriteString(`=`)
				} else {
					b.WriteString(`?`)
				}
			case '(', ')', '|', '+', '?', '{', '}':
				if very {
					b.WriteString(`\` + string(n))
				} else {
					brace = brace || n == '{'
					if n == '}' {
						brace = false
					}
					b.WriteRune(n)
				}
			default:
				b.WriteRune('\\')
				b.WriteRune(n)
			}
			continue
		}
		switch r {
		case '(', ')', '|', '+', '?', '{', '}':
			switch {
			case very:
				b.WriteRune(r)
			case r == '}' && brace:
				brace = false
				b.WriteRune(r)
			default:
				b.WriteString(`\` + string(r))
			}
		case '<', '>':
			if very {
				b.WriteString(`\b`)
			} else {
				b.WriteRune(r)
			}
		case '[':
			class = true
			b.WriteRune(r)
			if i+1 < len(rs) && rs[i+1] == '^' {
				i++
				b.WriteRune('^')
			}
			if i+1 < len(rs) && rs[i+1] == ']' {
				i++
				b.WriteRune(']') // a ] first is in the class
			}
		default:
			b.WriteRune(r)
		}
	}
	if fold && vimHasUpper(rs) {
		fold = false
	}
	expr := "(?m)" + b.String()
	if fold {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// vimHasUpper reports whether a pattern has a capital of its own, not of
// an escape as \S or \C, which makes a search match case.
func vimHasUpper(rs []rune) bool {
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' {
			i++
			continue
		}
		if unicode.IsUpper(rs[i]) {
			return true
		}
	}
	return false
}

// vimLineMatches calls fn with the start and end of each match of re in
// line l, in runes of the text, until fn returns false.
func vimLineMatches(snap ui.TextSnapshot, re *regexp.Regexp, l int, fn func(s, e int) bool) bool {
	ls, le := snap.LineRange(l)
	text := snap.Slice(ls, le)
	runes, at := ls, 0
	for _, m := range re.FindAllStringIndex(text, -1) {
		runes += utf8.RuneCountInString(text[at:m[0]])
		s := runes
		runes += utf8.RuneCountInString(text[m[0]:m[1]])
		at = m[1]
		if !fn(s, runes) {
			return false
		}
	}
	return true
}

// vimFind finds the match of re after from, or before it, round the ends
// of the text: where it starts, and whether the search went round.
func vimFind(snap ui.TextSnapshot, re *regexp.Regexp, from int, back bool) (at int, wrapped, ok bool) {
	n := snap.LineCount()
	line := snap.LineAt(min(from, snap.Len()))
	for k := 0; k <= n; k++ {
		l := line + k
		if back {
			l = line - k
		}
		wrapped = l < 0 || l >= n
		l = (l%n + n) % n
		// Past from on its line, then any, then up to from round the ends;
		// the first of a line going on, the last going back.
		found := -1
		vimLineMatches(snap, re, l, func(s, e int) bool {
			ok := true
			switch k {
			case 0:
				ok = back && s < from || !back && s > from
			case n:
				ok = back && s >= from || !back && s <= from
			}
			if ok {
				found = s
				return back
			}
			return true
		})
		if found >= 0 {
			return found, wrapped, true
		}
	}
	return 0, false, false
}

// vimMatchCount is the number of the match at at among those of re, and
// how many there are, unless the text is too long to count them as keys
// are typed: 0, -1.
func vimMatchCount(snap ui.TextSnapshot, re *regexp.Regexp, at int) (cur, total int) {
	if snap.ByteLen() > asyncSearchBytes {
		return 0, -1
	}
	for l := range snap.LineCount() {
		if !vimLineMatches(snap, re, l, func(s, e int) bool {
			total++
			if s <= at {
				cur = total
			}
			return total < maxMatches
		}) {
			break
		}
	}
	return cur, total
}

// vimMatches are the matches of the search of vim mode on lines first up
// to last, for the editor to highlight: of what the command line has as
// it is typed, else of the last search, while it shows.
func (a *app) vimMatches(d *editor, snap ui.TextSnapshot, first, last int) []matchRect {
	if !a.prefs().Vim || a.find.open && a.find.doc == d.id {
		return nil
	}
	pattern := ""
	switch l := &a.vimLine; {
	case l.d == d && l.kind != ':':
		pattern = l.text
	case a.vimSearch.show:
		pattern = a.vimSearch.pattern
	}
	if pattern == "" {
		return nil
	}
	re, err := a.vimCompile(pattern)
	if err != nil {
		return nil
	}
	caret := vimCursor(d, snap)
	// The matches found for the frame before, as long as nothing they
	// follow from changed: most frames draw the same.
	k := &d.vimShown
	key := vimShownKey{pattern, snap.Version(), first, last, caret, a.font}
	if k.found && k.key == key {
		return k.rects
	}
	out := k.rects[:0]
	for l := first; l <= last && l < snap.LineCount() && len(out) < 500; l++ {
		ls, le := snap.LineRange(l)
		long := le-ls > highlightRunes
		vimLineMatches(snap, re, l, func(s, e int) bool {
			if e == s {
				return true // nothing to show of an empty match, as ^
			}
			var x0, x1 float32
			if long {
				// Cells rather than the text laid out, as for the
				// cursor, along a line too long to measure every frame.
				x0, x1 = float32(s-ls)*a.font.charW, float32(min(e, le)-ls)*a.font.charW
			} else {
				x0 = a.textWidth(snap.Slice(ls, s))
				x1 = a.textWidth(snap.Slice(ls, min(e, le)))
			}
			out = append(out, matchRect{line: l, x0: x0, x1: max(x1, x0+2), cur: s == caret})
			return len(out) < 500
		})
	}
	k.key, k.rects, k.found = key, out, true
	return out
}

// vimShownKey is what the matches an editor highlights follow from.
type vimShownKey struct {
	pattern     string
	version     uint64
	first, last int
	caret       int
	font        codeFont
}

// vimShownMatches are the matches an editor highlighted last, and what
// they followed from.
type vimShownMatches struct {
	key   vimShownKey
	rects []matchRect
	found bool
}

// vimSearchMotion is the motion of n, N, * and #: the match of the last
// search after the cursor, or before it, as the search went or the other
// way.
func (a *app) vimSearchMotion(d *editor, snap ui.TextSnapshot, cur int, key string, cnt int) (vimTarget, bool) {
	s := &a.vimSearch
	if key == "*" || key == "#" {
		word, start := vimWordAt(snap, cur)
		if word == "" {
			return vimTarget{}, false
		}
		s.pattern, s.back = `\<`+regexp.QuoteMeta(word)+`\>\C`, key == "#"
		cur = start
	}
	if s.pattern == "" {
		return vimTarget{}, false
	}
	re, err := a.vimCompile(s.pattern)
	if err != nil {
		return vimTarget{}, false
	}
	s.show = true
	back := s.back != (key == "N")
	to, went := cur, false
	for range cnt {
		at, wrapped, ok := vimFind(snap, re, to, back)
		if !ok {
			a.vimSay(locales.VimPatternNotFound(vimShownPattern(s.pattern)), true)
			return vimTarget{}, false
		}
		to, went = at, went || wrapped
	}
	text := "/" + vimShownPattern(s.pattern)
	if back {
		text = "?" + vimShownPattern(s.pattern)
	}
	if n, total := a.vimCountOf(d, snap, s.pattern, re, to); total > 0 {
		text += "  " + locales.VimMatchCount(n, total)
	}
	switch {
	case went && back:
		a.vimSay(locales.VimWrappedBottom()+"  "+text, false)
	case went:
		a.vimSay(locales.VimWrappedTop()+"  "+text, false)
	default:
		a.vimSay(text, false)
	}
	return vimTarget{to: to}, to != cur || went
}

// vimShownPattern is a pattern as the status bar shows it: that of * and
// # without the \< \> and \C put around the word.
func vimShownPattern(p string) string {
	if w, ok := strings.CutPrefix(p, `\<`); ok {
		if w, ok := strings.CutSuffix(w, `\>\C`); ok {
			return strings.ReplaceAll(w, `\`, "")
		}
	}
	return p
}

// vimWordAt is the word under the cursor, or the first after it on its
// line, and where it starts.
func vimWordAt(snap ui.TextSnapshot, cur int) (string, int) {
	ls, le := snap.LineRange(snap.LineAt(cur))
	runes := []rune(snap.Slice(ls, le))
	i := cur - ls
	for i < len(runes) && vimClass(runes[i], false) != 1 {
		i++
	}
	if i >= len(runes) {
		return "", 0
	}
	s, e := i, i
	for s > 0 && vimClass(runes[s-1], false) == 1 {
		s--
	}
	for e < len(runes) && vimClass(runes[e], false) == 1 {
		e++
	}
	return string(runes[s:e]), ls + s
}

// vimPreview moves the cursor to the first match of the search typed so
// far, as vim's incsearch does, or back where the search started.
func (a *app) vimPreview() {
	l := &a.vimLine
	d := l.d
	if d == nil || l.kind == ':' || l.mode != vimNormal || l.cmd.op != "" {
		return
	}
	to := l.from
	if re, err := a.vimCompile(l.text); l.text != "" && err == nil {
		snap := d.buf.Snapshot()
		if at, _, ok := vimFind(snap, re, vimClamp(snap, l.from[1]), l.kind == '?'); ok {
			to = [2]int{at, at}
		}
	}
	if to != d.sel {
		d.selectRange(to[0], to[1], false)
		d.sel = to
	}
}

// vimLineView is the command line, over the bottom of the editor while
// it is open for d.
func (a *app) vimLineView(c *ui.Context, d *editor) {
	l := &a.vimLine
	if l.d != d {
		return
	}
	p := a.pal
	f := a.font
	bar := a.floating(c, ui.Row(c.Key("vim-line")).Absolute().Bottom(12).Left(16).Right(16).MaxWidth(640).Height(34).
		Padding(0, 6, 0, 6).Gap(8).AlignItems(ui.Center).Radius(8), false).
		Transition(ui.ElementTransition{Duration: 130 * time.Millisecond, Colors: true, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{Y: 6}})
	shake(c, bar, l.shook)
	bar.Role(ui.RoleToolbar).Label(locales.VimCommandLine())
	bar.Children(func() {
		// The kind of line, as vim shows it first: / ? or :.
		ui.Text(c, string(l.kind)).Font(f.family).FontSize(f.size).FontWeight(650).TextColor(p.accent).
			Width(22).Height(22).TextAlign(ui.Center).Radius(5).Background(p.accent.Alpha(0.12))
		in := ui.TextInputBase(c.Key("vim-line-input"), &l.text)
		if a.chording() {
			in.ReadOnly(true)
		}
		// Before the field's own keys: the arrows go through the history,
		// and Backspace with nothing typed closes the line, as in vim.
		a.takeKeys(in, placeInput, func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			h := *a.vimHistory(l.kind)
			switch {
			case ev.Mods == 0 && ev.Key == ui.KeyUp:
				if l.hist > 0 {
					l.hist--
					l.text = h[l.hist]
					a.vimPreview()
				}
			case ev.Mods == 0 && ev.Key == ui.KeyDown:
				if l.hist < len(h) {
					l.hist++
					l.text = ""
					if l.hist < len(h) {
						l.text = h[l.hist]
					}
					a.vimPreview()
				}
			case ev.Mods == 0 && ev.Key == ui.KeyBackspace && l.text == "",
				ev.Mods == ui.Ctrl && (ev.Key == ui.KeyBracketLeft || ev.Key == ui.KeyC && ui.Cmd != ui.Ctrl):
				a.vimRecordText(l.text + "\x1b")
				a.vimCloseLine(false)
			default:
				return false
			}
			a.invalidate()
			return true
		})
		in.Bind(&l.input).Grow(1).Font(f.family).FontSize(f.size).Label(locales.VimCommandLine())
		if l.kind == ':' {
			in.Placeholder(locales.VimCommandHint())
		} else {
			in.Placeholder(locales.VimSearchHint())
		}
		if in.Changed() {
			l.hist = len(*a.vimHistory(l.kind))
			a.vimPreview()
		}
		if in.Submitted() {
			a.vimSubmit()
		}
		if l.kind != ':' && l.text != "" {
			// How many the search finds, or that it finds none.
			label, col := "", p.muted
			if re, err := a.vimCompile(l.text); err != nil {
				label, col = locales.FindInvalid(), p.dangerFg
			} else if n, total := a.vimCountOf(d, d.buf.Snapshot(), l.text, re, vimClamp(d.buf.Snapshot(), d.sel[1])); total > 0 {
				label = locales.VimMatchCount(n, total)
			} else if total == 0 {
				label, col = locales.FindNoResults(), p.dangerFg
			}
			if label != "" {
				ui.Text(c, label).FontSize(11.5).TextColor(col).SingleLine().Padding(0, 4, 0, 0)
			}
		}
	})
	if bar.OverlayShortcut(0, ui.KeyEscape) || bar.Shortcut(0, ui.KeyEscape) {
		a.vimRecordText(l.text + "\x1b")
		a.vimCloseLine(false)
	}
}

// vimRangeOf reads the lines a command goes over, as vim writes them
// before it: a line (a number, . $ or '< '>, then +n or -n), two with a
// comma between, or % for all. It reports false when there is none.
func vimRangeOf(s string, snap ui.TextSnapshot, cur int, vis vimVisualArea, marks map[rune]int) (l1, l2 int, rest string, has bool) {
	last := vimLast(snap)
	if r, ok := strings.CutPrefix(s, "%"); ok {
		return 0, last, r, true
	}
	addr := func(s string) (int, string, bool) {
		l, got := cur, false
		switch {
		case s == "":
		case s[0] >= '0' && s[0] <= '9':
			i := 0
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			n, _ := strconv.Atoi(s[:i])
			l, s, got = n-1, s[i:], true
		case s[0] == '.':
			s, got = s[1:], true
		case s[0] == '$':
			l, s, got = last, s[1:], true
		case strings.HasPrefix(s, "'<"), strings.HasPrefix(s, "'>"):
			lo, hi := min(vis.anchor, vis.cursor), max(vis.anchor, vis.cursor)
			l = snap.LineAt(min(lo, snap.Len()))
			if s[1] == '>' {
				l = snap.LineAt(min(hi, snap.Len()))
			}
			s, got = s[2:], true
		case len(s) > 1 && s[0] == '\'' && marks != nil:
			// A mark's line, as 'a.
			if at, ok := marks[rune(s[1])]; ok {
				l, s, got = snap.LineAt(min(at, snap.Len())), s[2:], true
			}
		}
		for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
			sign := 1
			if s[0] == '-' {
				sign = -1
			}
			i := 1
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			n := 1
			if i > 1 {
				n, _ = strconv.Atoi(s[1:i])
			}
			l, s, got = l+sign*n, s[i:], true
		}
		return min(max(l, 0), last), s, got
	}
	cur = snap.LineAt(min(cur, snap.Len()))
	l1, s, has = addr(s)
	l2 = l1
	if r, ok := strings.CutPrefix(s, ","); ok {
		var got bool
		l2, s, got = addr(r)
		if !got {
			l2 = cur
		}
		has = true
	}
	if l2 < l1 {
		l1, l2 = l2, l1
	}
	return l1, l2, s, has
}

// vimEx runs a command typed after :.
func (a *app) vimEx(d *editor, text string) {
	snap := d.buf.Snapshot()
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, ":")
	if text != "" {
		a.vimLastEx = text
	}
	l1, l2, rest, has := vimRangeOf(text, snap, d.sel[1], d.vim.lastVisual, d.marks)
	rest = strings.TrimSpace(rest)
	name := rest
	for i, r := range rest {
		if !unicode.IsLetter(r) {
			name = rest[:i]
			break
		}
	}
	arg := strings.TrimSpace(rest[len(name):])
	bang := strings.HasPrefix(arg, "!")
	if bang {
		arg = strings.TrimSpace(arg[1:])
	}
	if name == "" && rest != "" {
		// A command of a sign, as :> or :&.
		name, arg = rest[:1], strings.TrimSpace(rest[1:])
	}
	is := func(names ...string) bool { return slices.Contains(names, name) }
	if !has {
		// The cursor's line, but for the commands of the whole text.
		l1 = snap.LineAt(min(d.sel[1], snap.Len()))
		l2 = l1
	}
	// The register of :d, :y and :pu, as :d a.
	d.vim.reg = 0
	if r, n := utf8.DecodeRuneInString(arg); n == len(arg) && n > 0 && vimRegisterName(r) && !unicode.IsDigit(r) &&
		is("d", "de", "del", "delete", "y", "ya", "yank", "pu", "put") {
		d.vim.reg = r
	}
	switch {
	case name == "" && has:
		if snap.LineAt(min(d.sel[1], snap.Len())) != l2 {
			vimPushJump(d, d.sel[1])
		}
		vimPut(d, firstNonBlank(snap, l2))
	case name == "":
	case is("w", "write", "up", "update"):
		a.saveDoc(d.document, nil)
	case is("wa", "wall"):
		a.saveAll()
	case is("q", "quit", "clo", "close"):
		a.closeEditor(d)
	case is("wq", "x", "xi", "xit", "exi", "exit"):
		a.saveDoc(d.document, func() { a.closeEditor(d) })
	case is("qa", "qall", "quita", "quitall"):
		a.closeAll(nil)
	case is("wqa", "wqall", "xa", "xall"):
		a.saveAll()
		a.closeAll(nil)
	case is("noh", "nohl", "nohlsearch"):
		a.vimSearch.show = false
	case is("s", "su", "substitute"):
		a.vimSubstitute(d, l1, l2, arg)
	case is("&", "&&"):
		a.vimRepeatSub(d, d.sel[1], false)
	case is("d", "de", "del", "delete"):
		a.vimApply(d, "d", vimRange{linewise: true, l1: l1, l2: l2}, d.sel[1])
	case is("y", "ya", "yank"):
		a.vimYank(d, snap, vimRange{linewise: true, l1: l1, l2: l2}, false)
	case is("pu", "put"):
		a.vimPutLines(d, d.vim.reg, l2, bang)
	case is(">", "<"):
		a.vimApply(d, name, vimRange{linewise: true, l1: l1, l2: l2}, d.sel[1])
	case is("j", "join"):
		if !has {
			l2 = l1 + 1
		}
		vimJoin(d, l1, max(l2-l1+1, 2))
		vimPut(d, firstNonBlank(d.buf.Snapshot(), l1))
	case is("m", "mo", "move", "t", "co", "copy"):
		to, ok := vimAddress(arg, snap, d.sel[1], d.vim.lastVisual, d.marks)
		if !ok {
			a.vimSay(locales.VimNotCommand(text), true)
			return
		}
		vimMoveLines(d, l1, l2, to, !is("m", "mo", "move"))
	case is("norm", "normal"):
		a.vimNormalEx(d, l1, l2, has, strings.TrimLeft(rest[len(name):], " !"))
	case is("g", "global", "v", "vglobal"):
		if !has {
			l1, l2 = 0, vimLast(snap)
		}
		a.vimGlobal(d, l1, l2, arg, bang || name[0] == 'v')
	case is("reg", "registers", "di", "display"):
		a.vimRegisters(arg)
	case is("marks"):
		a.vimMarks(d)
	case is("delm", "delmarks"):
		for _, r := range arg {
			delete(d.marks, r)
		}
		if bang {
			clear(d.marks)
		}
	case is("sor", "sort"):
		if !has {
			l1, l2 = 0, vimLast(snap)
		}
		vimSort(d, l1, l2, arg, bang)
	case is("e", "edit") && arg != "":
		a.openFile(a.resolvePath(arg), true)
	case is("sp", "split", "vs", "vsp", "vsplit"):
		a.splitRight()
	case is("bn", "bnext", "tabn", "tabnext"):
		a.cycleTab(1)
	case is("bp", "bprevious", "bN", "bNext", "tabp", "tabprevious", "tabN", "tabNext"):
		a.cycleTab(-1)
	default:
		a.vimSay(locales.VimNotCommand(text), true)
	}
}

// closeEditor closes the tab of the editor, asking first when it has
// changes, as :q does.
func (a *app) closeEditor(d *editor) {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.ed == d {
				a.closeTab(t, nil)
				return
			}
		}
	}
}

// resolvePath is a path typed, as after :e, in the folder unless it is
// whole.
func (a *app) resolvePath(p string) string {
	if strings.HasPrefix(p, "~/") && !remote.Is(a.root) {
		return inDir(homeDir(), p[2:])
	}
	if path.IsAbs(filepath.ToSlash(p)) || filepath.IsAbs(p) || a.root == "" {
		return p
	}
	return inDir(a.root, filepath.ToSlash(p))
}

// vimSubstitute runs :s/pattern/replacement/flags over lines l1 to l2.
func (a *app) vimSubstitute(d *editor, l1, l2 int, arg string) {
	snap := d.buf.Snapshot()
	if arg == "" {
		return
	}
	delim, _ := utf8.DecodeRuneInString(arg)
	if unicode.IsLetter(delim) || unicode.IsDigit(delim) || delim == '\\' || delim == ' ' {
		a.vimSay(locales.VimNotCommand("s"+arg), true)
		return
	}
	parts := vimSplit(arg[utf8.RuneLen(delim):], delim)
	pattern := parts[0]
	if pattern == "" {
		pattern = a.vimSearch.pattern
	}
	var rep, flags string
	if len(parts) > 1 {
		rep = parts[1]
	}
	if len(parts) > 2 {
		flags = parts[2]
	}
	if strings.Contains(flags, "i") {
		pattern += `\c`
	} else if strings.Contains(flags, "I") {
		pattern += `\C`
	}
	re, err := vimRegexp(pattern)
	if err != nil || pattern == "" {
		a.vimSay(locales.FindInvalid(), true)
		return
	}
	a.vimSearch.pattern, a.vimSearch.back = strings.TrimSuffix(strings.TrimSuffix(pattern, `\c`), `\C`), false
	a.vimLastSub = arg
	all := strings.Contains(flags, "g")
	tmpl := vimTemplate(rep)
	var edits []textEdit
	count, lastLine := 0, -1
	for l := l1; l <= l2; l++ {
		ls, le := snap.LineRange(l)
		text := snap.Slice(ls, le)
		ms := re.FindAllStringSubmatchIndex(text, -1)
		if !all && len(ms) > 1 {
			ms = ms[:1]
		}
		if len(ms) == 0 {
			continue
		}
		var out []byte
		at := 0
		for _, m := range ms {
			out = append(out, text[at:m[0]]...)
			out = re.ExpandString(out, tmpl, text, m)
			at = m[1]
		}
		out = append(out, text[at:]...)
		count += len(ms)
		lastLine = l
		edits = append(edits, textEdit{ls, le, string(out)})
	}
	if count == 0 {
		a.vimSay(locales.VimPatternNotFound(pattern), true)
		return
	}
	d.edit(d.sel, edits...)
	after := d.buf.Snapshot()
	// The cursor on the last line changed, by as many lines as it grew.
	grown := after.LineCount() - snap.LineCount()
	vimPut(d, firstNonBlank(after, min(max(lastLine+grown, 0), vimLast(after))))
	a.vimSay(locales.VimSubstituted(count, len(edits)), false)
}

// vimSplit splits the text after :s at each delim not escaped with \,
// into three parts at most: the pattern, the replacement and the flags.
// An escaped delim is left as the delim alone.
func vimSplit(s string, delim rune) []string {
	var parts []string
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < len(rs) && rs[i+1] == delim:
			b.WriteRune(delim)
			i++
		case r == '\\' && i+1 < len(rs):
			b.WriteRune(r)
			b.WriteRune(rs[i+1])
			i++
		case r == delim && len(parts) < 2:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(parts, b.String())
}

// vimTemplate is a replacement of :s as regexp's Expand takes it: & and
// \0 for the match, \1 to \9 for its groups, \n and \r for a line break,
// \t for a tab, and the rest as it is.
func vimTemplate(rep string) string {
	var b strings.Builder
	rs := []rune(rep)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '$':
			b.WriteString("$$")
		case r == '&':
			b.WriteString("${0}")
		case r == '\\' && i+1 < len(rs):
			i++
			switch n := rs[i]; {
			case n >= '0' && n <= '9':
				b.WriteString("${" + string(n) + "}")
			case n == 'n' || n == 'r':
				b.WriteRune('\n')
			case n == 't':
				b.WriteRune('\t')
			case n == '$':
				b.WriteString("$$")
			default:
				b.WriteRune(n)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// vimSort sorts lines l1 to l2, as :sort does: backwards with !, with i
// ignoring case, n by the first number of each, and u keeping one of
// lines alike.
func vimSort(d *editor, l1, l2 int, flags string, reverse bool) {
	snap := d.buf.Snapshot()
	if l2 <= l1 {
		return
	}
	s, _ := snap.LineRange(l1)
	_, e := snap.LineRange(l2)
	lines := strings.Split(snap.Slice(s, e), "\n")
	fold, numeric, unique := strings.Contains(flags, "i"), strings.Contains(flags, "n"), strings.Contains(flags, "u")
	key := func(line string) string {
		if fold {
			return strings.ToLower(line)
		}
		return line
	}
	number := func(line string) (int, bool) {
		i := strings.IndexFunc(line, unicode.IsDigit)
		if i < 0 {
			return 0, false
		}
		j := i
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		n, _ := strconv.Atoi(line[i:j])
		if i > 0 && line[i-1] == '-' {
			n = -n
		}
		return n, true
	}
	slices.SortStableFunc(lines, func(x, y string) int {
		if numeric {
			nx, okx := number(x)
			ny, oky := number(y)
			switch {
			case !okx && oky:
				return -1
			case okx && !oky:
				return 1
			case nx != ny:
				return nx - ny
			}
			return 0
		}
		return strings.Compare(key(x), key(y))
	})
	if reverse {
		slices.Reverse(lines)
	}
	if unique {
		lines = slices.CompactFunc(lines, func(x, y string) bool { return key(x) == key(y) })
	}
	text := strings.Join(lines, "\n")
	if text != snap.Slice(s, e) {
		d.edit([2]int{s, s}, textEdit{s, e, text})
	}
	vimPut(d, s)
}
