package main

import (
	"mayoi/locales"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// vimRegisterName reports whether r names a register, as after ": a to z,
// A to Z to append to them, 0 to 9, " the unnamed one, - small deletes, _
// the black hole, + and * the clipboard, and . : / to read what was typed
// last, run last and searched last.
func vimRegisterName(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) || strings.ContainsRune(`"-_+*.:/`, r)
}

// vimMarkName reports whether r names a mark, as after m when set, and
// after ' and ` otherwise: a to z and A to Z, and to go to, ' and ` the
// place jumped from, < and > the ends of the last selection, . the last
// change and ^ the last insert.
func vimMarkName(r rune, set bool) bool {
	if r < utf8.RuneSelf && unicode.IsLetter(r) || r == '\'' || r == '`' {
		return true
	}
	return !set && strings.ContainsRune("<>.^", r)
}

// vimStore puts text in register reg as vim does. The unnamed register,
// + and * are the clipboard; a yank also goes in 0, and a delete in 1, the
// others moving down to 9, or in - when it is less than a line. A capital
// appends to its letter's register, and _ keeps nothing.
func (a *app) vimStore(reg rune, text string, linewise, del bool) {
	if a.vimRegs == nil {
		a.vimRegs = map[rune]vimRegister{}
	}
	switch {
	case reg == '_':
		return
	case reg >= 'a' && reg <= 'z':
		a.vimRegs[reg] = vimRegister{text, linewise}
		return
	case reg >= 'A' && reg <= 'Z':
		lo := unicode.ToLower(reg)
		old := a.vimRegs[lo]
		switch {
		case old.text == "":
		case old.linewise && !linewise:
			text += "\n"
		case !old.linewise && linewise:
			old.text += "\n"
		}
		a.vimRegs[lo] = vimRegister{old.text + text, old.linewise || linewise}
		return
	}
	a.vimReg = vimRegister{text, linewise}
	a.services.WriteClipboard(text)
	switch {
	case !del:
		a.vimRegs['0'] = a.vimReg
	case linewise || strings.Contains(text, "\n"):
		for r := '9'; r > '1'; r-- {
			a.vimRegs[r] = a.vimRegs[r-1]
		}
		a.vimRegs['1'] = a.vimReg
	default:
		a.vimRegs['-'] = a.vimReg
	}
}

// vimRead is the text of register reg, and whether it pastes as lines.
func (a *app) vimRead(reg rune) (string, bool) {
	switch {
	case reg == 0 || reg == '"' || reg == '+' || reg == '*':
		return a.vimClip()
	case reg == '_':
		return "", false
	case reg == '/':
		return a.vimSearch.pattern, false
	case reg == ':':
		return a.vimLastEx, false
	case reg >= 'A' && reg <= 'Z':
		reg = unicode.ToLower(reg)
	}
	r := a.vimRegs[reg]
	return r.text, r.linewise
}

// vimSetMark sets mark r of d's document at offset at.
func vimSetMark(d *editor, r rune, at int) {
	if d.marks == nil {
		d.marks = map[rune]int{}
	}
	if r == '`' {
		r = '\''
	}
	d.marks[r] = at
}

// vimMarkAt is the offset mark r is at, reporting false for one not set.
func vimMarkAt(d *editor, snap ui.TextSnapshot, r rune) (int, bool) {
	at, ok := 0, true
	switch r {
	case '<', '>':
		lv := d.vim.lastVisual
		if !lv.mode.visual() {
			return 0, false
		}
		at = min(lv.anchor, lv.cursor)
		if r == '>' {
			at = max(lv.anchor, lv.cursor)
		}
	case '^':
		at = d.vim.lastInsert
	case '.':
		at = d.changed
	case '\'', '`':
		at, ok = d.marks['\'']
		if !ok {
			at, ok = 0, true // the start, before any jump
		}
	default:
		at, ok = d.marks[r]
	}
	return min(max(at, 0), snap.Len()), ok
}

// moveMarks moves what points into the text with an edit, by where moved
// leaves an offset of the text before it: the marks, the lines a command
// goes over, the places of Go Back's history, and each editor's jumps,
// last selection and last insert.
func (doc *document) moveMarks(moved func(x int) int) {
	for k, x := range doc.marks {
		doc.marks[k] = moved(x)
	}
	for i, x := range doc.tracked {
		doc.tracked[i] = moved(x)
	}
	for _, p := range doc.navs {
		p.at = moved(p.at)
	}
	doc.changed = moved(doc.changed)
	for _, o := range doc.views {
		v := &o.vim
		for i, x := range v.jumps {
			v.jumps[i] = moved(x)
		}
		if v.lastVisual.mode.visual() {
			v.lastVisual.anchor, v.lastVisual.cursor = moved(v.lastVisual.anchor), moved(v.lastVisual.cursor)
		}
		v.lastInsert = moved(v.lastInsert)
	}
}

// marked reports whether anything points into the text that moveMarks
// would move.
func (doc *document) marked() bool {
	if len(doc.marks) > 0 || len(doc.tracked) > 0 || len(doc.navs) > 0 {
		return true
	}
	for _, o := range doc.views {
		if len(o.vim.jumps) > 0 || o.vim.lastVisual.mode.visual() || o.vim.lastInsert > 0 {
			return true
		}
	}
	return false
}

// vimJumpsMax is how many places the jump list keeps.
const vimJumpsMax = 100

// vimJumps reports whether a motion is a jump, which the jump list keeps
// where it jumped from.
func vimJumps(key string) bool {
	switch key {
	case "G", "gg", "n", "N", "*", "#", "%", "{", "}", "H", "M", "L", "'", "`", "[(", "[{", "])", "]}":
		return true
	}
	return false
}

// vimPushJump keeps from in the jump list, as a jump leaves it, and as the
// mark ' and `; the places after the one gone back to go, as the line of
// from did.
func vimPushJump(d *editor, from int) {
	v := &d.vim
	snap := d.buf.Snapshot()
	line := snap.LineAt(min(from, snap.Len()))
	v.jumps = slices.DeleteFunc(v.jumps[:min(v.jumpAt, len(v.jumps))], func(x int) bool {
		return snap.LineAt(min(x, snap.Len())) == line
	})
	v.jumps = append(v.jumps, from)
	if len(v.jumps) > vimJumpsMax {
		v.jumps = slices.Delete(v.jumps, 0, len(v.jumps)-vimJumpsMax)
	}
	v.jumpAt = len(v.jumps)
	vimSetMark(d, '\'', from)
}

// vimJumpBy goes back through the jump list by n, as ⌃O does, or forward,
// as ⌃I does, from cur.
func (a *app) vimJumpBy(d *editor, cur, n int) {
	v := &d.vim
	v.jumpAt = min(v.jumpAt, len(v.jumps))
	if n < 0 && v.jumpAt == len(v.jumps) {
		// Kept, for ⌃I to come back to.
		vimPushJump(d, cur)
		v.jumpAt--
	}
	to := v.jumpAt + n
	if to < 0 || to >= len(v.jumps) || n == 0 {
		a.vimFailed = true
		return
	}
	v.jumpAt = to
	vimPut(d, vimClamp(d.buf.Snapshot(), v.jumps[to]))
}

// vimRecord keeps a key typed while a register is recorded, as q does.
func (a *app) vimRecord(r rune) {
	if a.vimRec != 0 && a.vimPlaying == 0 {
		a.vimRecKeys = append(a.vimRecKeys, r)
	}
}

// vimRecordText keeps the text typed in the command line, or in insert
// mode, while a register is recorded.
func (a *app) vimRecordText(text string) {
	if a.vimRec != 0 && a.vimPlaying == 0 {
		a.vimRecKeys = append(a.vimRecKeys, []rune(text)...)
	}
}

// vimRecordInsert keeps the text of the insert going on, and the Esc that
// ends it, while a register is recorded.
func (a *app) vimRecordInsert(d *editor) {
	if a.vimRec == 0 || a.vimPlaying > 0 {
		return
	}
	v := &d.vim
	if grown := d.buf.Len() - v.insLen; grown > 0 && v.insAt+grown <= d.buf.Len() {
		a.vimRecordText(d.buf.Snapshot().Slice(v.insAt, v.insAt+grown))
	}
	a.vimRecord('\x1b')
}

// vimStopRecording ends the recording of a register, which keeps the keys
// typed.
func (a *app) vimStopRecording() {
	if a.vimRec == 0 {
		return
	}
	reg := a.vimRec
	if reg == '"' {
		reg = 0
	}
	a.vimStore(reg, string(a.vimRecKeys), false, false)
	a.vimRec, a.vimRecKeys = 0, nil
	a.invalidate()
}

// vimFeedMax is how many keys a register played, or :normal, types at
// most, which ends a register that plays itself without end.
const vimFeedMax = 200000

// vimPlay plays a register as keys, as @ does: @@ the one played last,
// and @: the last command again.
func (a *app) vimPlay(d *editor, c vimCmd) {
	reg := c.arg
	if reg == '@' {
		if reg = a.vimPlayed; reg == 0 {
			return
		}
	}
	a.vimPlayed = reg
	cnt := max(c.count, 1)
	if reg == ':' {
		if a.vimLastEx == "" {
			return
		}
		ex := a.vimLastEx
		for range cnt {
			a.queueEdit(d, func(d *editor) { a.vimEx(d, ex) })
		}
		return
	}
	text, _ := a.vimRead(reg)
	if text == "" {
		return
	}
	if a.vimPlaying == 0 {
		a.vimFailed = false
	}
	a.vimFeed(d, []rune(strings.Repeat(text, cnt)))
}

// vimFeed types keys in d as vim would take them, one at a time, each
// command made before the next key: in normal and visual mode the keys
// of commands, in insert mode text, to Esc, and in the command line its
// text, to Enter or Esc. It stops where a command fails, as vim does.
func (a *app) vimFeed(d *editor, keys []rune) {
	if a.vimPlaying == 0 {
		a.vimBudget = vimFeedMax
	}
	a.vimPlaying++
	defer func() { a.vimPlaying-- }()
	v := &d.vim
	a.vimDrain(d)
	for i := 0; i < len(keys) && !a.vimFailed && a.vimBudget > 0; i++ {
		a.vimBudget--
		r := keys[i]
		switch l := &a.vimLine; {
		case l.d == d:
			switch r {
			case '\r', '\n':
				a.vimSubmit()
			case '\x1b':
				a.vimCloseLine(false)
			case '\b':
				if l.text == "" {
					a.vimCloseLine(false)
				} else {
					_, n := utf8.DecodeLastRuneInString(l.text)
					l.text = l.text[:len(l.text)-n]
				}
			default:
				l.text += string(r)
			}
		case v.mode == vimInsert:
			j := i
			for j < len(keys) && keys[j] != '\x1b' {
				j++
			}
			a.vimBudget -= j - i
			if text := string(keys[i:j]); text != "" {
				a.queueEdit(d, func(d *editor) { vimType(d, text) })
			}
			if j < len(keys) {
				v.mode = vimNormal
				a.queueEdit(d, a.vimEndInsert)
			}
			i = j
		default:
			if r == '\n' {
				r = '\r'
			}
			a.vimTake(d, r)
		}
		a.vimDrain(d)
	}
}

// vimDrain makes the edits queued for d now, rather than in its next
// frame, as keys played make them.
func (a *app) vimDrain(d *editor) {
	for len(d.cmds) > 0 {
		cmds := d.cmds
		d.cmds = nil
		for _, cmd := range cmds {
			cmd(d)
		}
	}
}

// vimType types text at the caret as it is, as a register played in
// insert mode does: a backspace deletes the rune before the caret, and a
// carriage return breaks the line.
func vimType(d *editor, text string) {
	for {
		chunk, rest, back := strings.Cut(text, "\b")
		chunk = strings.ReplaceAll(chunk, "\r", "\n")
		if chunk != "" {
			caret := min(d.sel[1], d.buf.Len())
			n := utf8.RuneCountInString(chunk)
			d.edit([2]int{caret + n, caret + n}, textEdit{caret, caret, chunk})
		}
		if !back {
			return
		}
		if caret := min(d.sel[1], d.buf.Len()); caret > 0 {
			d.edit([2]int{caret - 1, caret - 1}, textEdit{caret - 1, caret, ""})
		}
		text = rest
	}
}

// vimFinish ends what keys played left begun, as :normal does: a command
// half typed, the command line, insert mode and visual mode.
func (a *app) vimFinish(d *editor) {
	v := &d.vim
	v.keys = v.keys[:0]
	if a.vimLine.d == d {
		a.vimCloseLine(false)
	}
	switch {
	case v.mode == vimInsert:
		v.mode = vimNormal
		a.vimEndInsert(d)
	case v.mode.visual():
		v.lastVisual = vimVisualArea{v.anchor, v.cursor, v.mode}
		v.mode = vimNormal
		vimPut(d, v.cursor)
	}
	a.vimDrain(d)
	vimSettle(d)
}

// vimSquash makes the steps of undo since there were from one, which takes
// the text back to before, as a command made of many, as :g, undoes.
func (d *editor) vimSquash(from int, before histStep) {
	d.observe()
	h := &d.hist
	h.group = groupNone
	if len(h.undo) > from {
		h.undo = append(h.undo[:min(from, len(h.undo))], before)
	}
}

// vimEachLine runs fn with the cursor at the start of each of lines, as
// they move with what fn changes; the changes undo as one.
func (a *app) vimEachLine(d *editor, lines []int, fn func()) {
	d.observe()
	from, before := len(d.hist.undo), histStep{snap: d.hist.snap, sel: d.sel}
	snap := d.buf.Snapshot()
	old := d.tracked
	starts := make([]int, len(lines))
	for i, l := range lines {
		starts[i], _ = snap.LineRange(l)
	}
	d.tracked = starts
	for i := range starts {
		at := d.tracked[i]
		now := d.buf.Snapshot()
		if at > now.Len() {
			continue // its line went
		}
		if s, _ := now.LineRange(now.LineAt(at)); s != at {
			continue
		}
		vimPut(d, at)
		fn()
	}
	d.tracked = old
	d.vimSquash(from, before)
}

// vimNormalEx types keys as :normal does: at the cursor, or at the start
// of each of lines l1 to l2.
func (a *app) vimNormalEx(d *editor, l1, l2 int, has bool, keys string) {
	if keys == "" {
		return
	}
	run := func() {
		if a.vimPlaying == 0 {
			a.vimFailed = false
		}
		a.vimFeed(d, []rune(keys))
		a.vimFinish(d)
	}
	if !has {
		run()
		return
	}
	var lines []int
	for l := l1; l <= l2; l++ {
		lines = append(lines, l)
	}
	a.vimEachLine(d, lines, run)
}

// vimGlobal runs a command on lines l1 to l2 that match a pattern, or
// with inverse that do not, as :g and :v do; :d on all of them at once.
func (a *app) vimGlobal(d *editor, l1, l2 int, arg string, inverse bool) {
	if a.vimGlobalOn {
		a.vimSay(locales.VimNotCommand("g"+arg), true)
		return
	}
	delim, size := utf8.DecodeRuneInString(arg)
	if arg == "" || unicode.IsLetter(delim) || unicode.IsDigit(delim) || delim == '\\' || delim == ' ' || delim == '"' {
		a.vimSay(locales.VimNotCommand("g"+arg), true)
		return
	}
	pattern, cmd := vimCutPattern(arg[size:], delim)
	if pattern == "" {
		pattern = a.vimSearch.pattern
	}
	re, err := vimRegexp(pattern)
	if err != nil || pattern == "" {
		a.vimSay(locales.FindInvalid(), true)
		return
	}
	a.vimSearch.pattern, a.vimSearch.back = pattern, false
	snap := d.buf.Snapshot()
	var lines []int
	for l := l1; l <= l2; l++ {
		found := !vimLineMatches(snap, re, l, func(s, e int) bool { return false })
		if found != inverse {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		a.vimSay(locales.VimPatternNotFound(pattern), true)
		return
	}
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "", "p", "print":
		// The lines show by their matches.
		a.vimSearch.show = !inverse
		a.invalidate()
		return
	case "d", "de", "del", "delete":
		vimDeleteEach(d, lines)
		return
	}
	a.vimGlobalOn = true
	defer func() { a.vimGlobalOn = false }()
	a.vimEachLine(d, lines, func() { a.vimEx(d, cmd) })
}

// vimCutPattern cuts the pattern of :g, to the first delim not escaped
// with \, from the command after it.
func vimCutPattern(s string, delim rune) (pattern, rest string) {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\\' && i+n < len(s):
			next, m := utf8.DecodeRuneInString(s[i+n:])
			if next != delim {
				b.WriteRune(r)
			}
			b.WriteRune(next)
			i += n + m
			continue
		case r == delim:
			return b.String(), s[i+n:]
		}
		b.WriteRune(r)
		i += n
	}
	return b.String(), ""
}

// vimDeleteEach deletes lines, in order, in one edit, the cursor on the
// line after the last.
func vimDeleteEach(d *editor, lines []int) {
	snap := d.buf.Snapshot()
	first, last := lines[0], lines[len(lines)-1]
	gone := make(map[int]bool, len(lines))
	for _, l := range lines {
		gone[l] = true
	}
	var kept []string
	for l := first; l <= last; l++ {
		if !gone[l] {
			s, e := snap.LineRange(l)
			kept = append(kept, snap.Slice(s, e))
		}
	}
	s, _ := snap.LineRange(first)
	_, e := snap.LineRange(last)
	ed := textEdit{s, e, strings.Join(kept, "\n")}
	if len(kept) == 0 {
		switch {
		case last+1 < snap.LineCount():
			ed.end++
		case first > 0:
			ed.start--
		}
	}
	d.edit([2]int{ed.start, ed.start}, ed)
	after := d.buf.Snapshot()
	vimPut(d, firstNonBlank(after, min(first+len(kept), vimLast(after))))
}

// vimMoveLines moves lines l1 to l2 below line to, -1 for above the
// first, as :m does, or copies them there, as :t does.
func vimMoveLines(d *editor, l1, l2, to int, copy bool) {
	snap := d.buf.Snapshot()
	if !copy && to >= l1-1 && to <= l2 {
		return // where they are
	}
	s, _ := snap.LineRange(l1)
	_, e := snap.LineRange(l2)
	block := snap.Slice(s, e)
	n := l2 - l1 + 1
	if copy {
		if to < 0 {
			d.edit(d.sel, textEdit{0, 0, block + "\n"})
		} else {
			_, te := snap.LineRange(to)
			d.edit(d.sel, textEdit{te, te, "\n" + block})
		}
		vimPut(d, firstNonBlank(d.buf.Snapshot(), to+n))
		return
	}
	// The lines from the first of either to the last, in their new order.
	lo, hi := min(l1, to+1), max(l2, to)
	var lines []string
	for l := lo; l <= hi; l++ {
		ls, le := snap.LineRange(l)
		lines = append(lines, snap.Slice(ls, le))
	}
	moved := slices.Clone(lines[l1-lo : l2-lo+1])
	rest := slices.Delete(slices.Clone(lines), l1-lo, l2-lo+1)
	at := to + 1 - lo
	if to > l2 {
		at -= n
	}
	out := slices.Insert(rest, at, moved...)
	rs, _ := snap.LineRange(lo)
	_, re := snap.LineRange(hi)
	d.edit(d.sel, textEdit{rs, re, strings.Join(out, "\n")})
	vimPut(d, firstNonBlank(d.buf.Snapshot(), lo+at+n-1))
}

// vimAddress reads the line of an address, as after :m and :t: 0 for
// above the first line, -1 then.
func vimAddress(s string, snap ui.TextSnapshot, cur int, vis vimVisualArea, marks map[rune]int) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return -1, true
	}
	_, l, rest, has := vimRangeOf(s, snap, cur, vis, marks)
	return l, has && strings.TrimSpace(rest) == ""
}

// vimPutLines puts the text of register reg as lines below line l, or
// above it with above, as :put does.
func (a *app) vimPutLines(d *editor, reg rune, l int, above bool) {
	text, _ := a.vimRead(reg)
	if text == "" {
		return
	}
	body := strings.TrimSuffix(text, "\n")
	snap := d.buf.Snapshot()
	s, e := snap.LineRange(l)
	first := l + 1
	if above {
		d.edit(d.sel, textEdit{s, s, body + "\n"})
		first = l
	} else {
		d.edit(d.sel, textEdit{e, e, "\n" + body})
	}
	vimPut(d, firstNonBlank(d.buf.Snapshot(), first+strings.Count(body, "\n")))
}

// vimRepeatSub runs the last :s again on the cursor's line, as & does,
// without its flags, or with g& on every line, with them.
func (a *app) vimRepeatSub(d *editor, cur int, all bool) {
	arg := a.vimLastSub
	if arg == "" {
		a.vimFailed = true
		return
	}
	snap := d.buf.Snapshot()
	if all {
		a.vimSubstitute(d, 0, vimLast(snap), arg)
		return
	}
	delim, size := utf8.DecodeRuneInString(arg)
	parts := vimSplit(arg[size:], delim)
	plain := string(delim) + strings.ReplaceAll(parts[0], string(delim), `\`+string(delim))
	if len(parts) > 1 {
		plain += string(delim) + strings.ReplaceAll(parts[1], string(delim), `\`+string(delim))
	}
	l := snap.LineAt(cur)
	a.vimSubstitute(d, l, l, plain)
}

// vimUnmatched finds the cnt-th bracket before cur that holds it, for [(
// and [{, or after it, for ]) and ]}.
func vimUnmatched(t *vtext, cur int, key string, cnt int) (vimTarget, bool) {
	open, close := '(', ')'
	if key[1] == '{' || key[1] == '}' {
		open, close = '{', '}'
	}
	back := key[0] == '['
	i := cur
	for range cnt {
		depth, found := 0, false
		for !found {
			if back {
				i--
				if i < 0 || i < cur-maxBracketScan {
					return vimTarget{}, false
				}
			} else {
				i++
				if i >= t.n || i > cur+maxBracketScan {
					return vimTarget{}, false
				}
			}
			switch r := t.at(i); {
			case back && r == close, !back && r == open:
				depth++
			case back && r == open, !back && r == close:
				if depth == 0 {
					found = true
				}
				depth--
			}
		}
	}
	return vimTarget{to: i}, true
}

// vimTagRe matches a tag of markup: < or </, its name, its attributes,
// and > or />.
var vimTagRe = regexp.MustCompile(`<(/?)([A-Za-z][\w:.-]*)(?:\s[^<>]*?)?(/?)>`)

// vimTag finds the runes of it and at around cur: the text between the
// cnt-th pair of tags that holds it, or the tags too.
func vimTag(snap ui.TextSnapshot, cur int, inner bool, cnt int) (from, to int, ok bool) {
	// In the indent before a tag, the tag, as vim takes it.
	if l := snap.LineAt(cur); cur < firstNonBlank(snap, l) {
		if at := firstNonBlank(snap, l); at < snap.Len() && snap.Slice(at, at+1) == "<" {
			cur = at
		}
	}
	lo, hi := max(0, cur-maxBracketScan), min(snap.Len(), cur+maxBracketScan)
	text := snap.Slice(lo, hi)
	type tag struct {
		s, e int
		name string
	}
	type pair struct{ os, oe, cs, ce int }
	var open []tag
	var pairs []pair
	runes, at := lo, 0
	for _, m := range vimTagRe.FindAllStringSubmatchIndex(text, -1) {
		runes += utf8.RuneCountInString(text[at:m[0]])
		s := runes
		runes += utf8.RuneCountInString(text[m[0]:m[1]])
		at = m[1]
		name := text[m[4]:m[5]]
		switch {
		case m[7] > m[6]:
			// Closed itself, as <br/>.
		case m[3] == m[2]:
			open = append(open, tag{s, runes, name})
		default:
			for k := len(open) - 1; k >= 0; k-- {
				if open[k].name == name {
					pairs = append(pairs, pair{open[k].s, open[k].e, s, runes})
					open = open[:k]
					break
				}
			}
		}
	}
	// The pairs that hold the cursor, the innermost first.
	pairs = slices.DeleteFunc(pairs, func(p pair) bool { return cur < p.os || cur >= p.ce })
	slices.SortFunc(pairs, func(x, y pair) int { return (x.ce - x.os) - (y.ce - y.os) })
	if len(pairs) < cnt {
		return 0, 0, false
	}
	p := pairs[cnt-1]
	if inner {
		return p.oe, p.cs, true
	}
	return p.os, p.ce, true
}

// vimRegisterShown is the text of a register as the status bar says it:
// on one line, control keys as ^ and a letter, and cut short.
func vimRegisterShown(text string) string {
	var b strings.Builder
	n := 0
	for _, r := range text {
		if n >= 60 {
			b.WriteString("…")
			break
		}
		switch {
		case r == '\n':
			b.WriteString("^J")
		case r < ' ':
			b.WriteString("^" + string(r+'@'))
		default:
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}

// vimRegisters says what the registers hold, as :reg does, in the status
// bar: those of names, or all that hold something.
func (a *app) vimRegisters(names string) {
	if names == "" {
		names = `"0123456789-abcdefghijklmnopqrstuvwxyz.:/`
	}
	var parts []string
	for _, r := range names {
		if r == ' ' {
			continue
		}
		reg := r
		if r == '"' {
			reg = 0
		}
		if text, _ := a.vimRead(reg); text != "" {
			parts = append(parts, `"`+string(r)+" "+vimRegisterShown(text))
		}
	}
	if len(parts) == 0 {
		a.vimSay(locales.VimRegistersEmpty(), false)
		return
	}
	a.vimSay(strings.Join(parts, "   "), false)
}

// vimMarks says where the marks of the letters are, as :marks does: each
// by its line and column.
func (a *app) vimMarks(d *editor) {
	snap := d.buf.Snapshot()
	var keys []rune
	for r := range d.marks {
		if r < utf8.RuneSelf && unicode.IsLetter(r) {
			keys = append(keys, r)
		}
	}
	slices.Sort(keys)
	var parts []string
	for _, r := range keys {
		at := min(d.marks[r], snap.Len())
		l := snap.LineAt(at)
		s, _ := snap.LineRange(l)
		parts = append(parts, string(r)+" "+strconv.Itoa(l+1)+":"+strconv.Itoa(at-s+1))
	}
	if len(parts) == 0 {
		a.vimSay(locales.VimMarksEmpty(), false)
		return
	}
	a.vimSay(strings.Join(parts, "   "), false)
}
