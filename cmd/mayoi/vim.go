package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// vimMode is the mode of an editor in vim mode.
type vimMode uint8

const (
	vimNormal vimMode = iota
	vimInsert
	vimVisual
	vimVisualLine
)

func (m vimMode) String() string {
	switch m {
	case vimInsert:
		return "INSERT"
	case vimVisual:
		return "VISUAL"
	case vimVisualLine:
		return "V-LINE"
	}
	return "NORMAL"
}

func (m vimMode) visual() bool { return m == vimVisual || m == vimVisualLine }

// vimState is an editor's state of vim mode. Its zero value is normal
// mode, with nothing typed.
type vimState struct {
	mode vimMode
	// keys are the keys of a command typed so far.
	keys []rune
	// anchor and cursor are the ends of the selection in visual mode, the
	// runes at either end selected.
	anchor, cursor int
	// want is the column that j and k keep to, while hasWant.
	want    int
	hasWant bool
	// find is the last f, F, t or T, which ; and , repeat.
	find vimCmd
	// last is the last change, which . repeats, and lastText what was
	// typed in the insert it started.
	last     vimCmd
	hasLast  bool
	lastText string
	// recording is set while an insert started by last goes on; it
	// started at insAt, with the text insLen long.
	recording     bool
	insAt, insLen int
	// replay is set while . makes the last change again.
	replay bool
	// viewH is how high the editor was in the last frame, which ⌃D and
	// ⌃F scroll by.
	viewH float32
}

// label is what the status bar shows of the state.
func (v *vimState) label() string {
	s := "-- " + v.mode.String() + " --"
	if len(v.keys) > 0 {
		var b strings.Builder
		for _, r := range v.keys {
			if r < ' ' {
				b.WriteString("^" + string(r+'@'))
			} else {
				b.WriteRune(r)
			}
		}
		s += "  " + b.String()
	}
	return s
}

// vimRegister is the text vim mode yanked last, and whether it is lines.
type vimRegister struct {
	text     string
	linewise bool
}

// vimCmd is a command of vim mode: an operator and a motion, or a key that
// does something itself, with its count, 0 for none.
type vimCmd struct {
	count int
	// op is the operator: d, c, y, >, <, gc, g~, gu or gU; "" for none.
	op string
	// key is the motion, the text object or the command, "line" for an
	// operator typed twice, as dd.
	key string
	// arg is the character of f, F, t, T and r.
	arg rune
}

const (
	vimMore = iota // the command goes on
	vimDone
	vimBad
)

var vimOps = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true, "gc": true, "g~": true, "gu": true, "gU": true}

// vimActions are the keys of normal mode that do something themselves.
const vimActions = "xXDCYpPoOJsS~u.iaIAvV\x04\x15\x06\x02\x05\x19\x12\x7f"

// vimVisualActions are the keys of visual mode that do something to the
// selection, or with it.
var vimVisualActions = map[string]bool{
	"d": true, "x": true, "X": true, "D": true, "y": true, "Y": true, "c": true, "s": true, "C": true, "S": true,
	"R": true, ">": true, "<": true, "~": true, "u": true, "U": true, "J": true, "p": true, "P": true,
	"gc": true, "g~": true, "gu": true, "gU": true, "o": true, "v": true, "V": true, "\x1b": true,
}

// parseVim reads the keys typed in a mode as a command: whole, begun, or
// not one.
func parseVim(keys []rune, mode vimMode) (vimCmd, int) {
	var c vimCmd
	i := 0
	count := func() int {
		n := 0
		for i < len(keys) && (keys[i] >= '1' && keys[i] <= '9' || n > 0 && keys[i] == '0') {
			n = n*10 + int(keys[i]-'0')
			i++
		}
		return n
	}
	n1 := count()
	c.count = n1
	if i == len(keys) {
		return c, vimMore
	}
	k := keys[i]
	i++
	name := string(k)
	if k == 'g' {
		if i == len(keys) {
			return c, vimMore
		}
		name += string(keys[i])
		i++
	}
	if mode.visual() {
		switch {
		case vimVisualActions[name]:
			c.key = name
			return c, vimDone
		case name == "r":
			if i == len(keys) {
				return c, vimMore
			}
			if keys[i] < ' ' {
				return c, vimBad
			}
			c.key, c.arg = name, keys[i]
			return c, vimDone
		}
		return parseMotion(c, keys, i, k, name, true)
	}
	switch {
	case k == '\x1b':
		return c, vimBad
	case vimOps[name]:
		c.op = name
		n2 := count()
		if n1 > 0 || n2 > 0 {
			c.count = max(n1, 1) * max(n2, 1)
		}
		if i == len(keys) {
			return c, vimMore
		}
		k2 := keys[i]
		i++
		if last, _ := utf8.DecodeLastRuneInString(name); k2 == last {
			c.key = "line"
			return c, vimDone
		}
		return parseMotion(c, keys, i, k2, string(k2), true)
	case name == "r":
		if i == len(keys) {
			return c, vimMore
		}
		if keys[i] < ' ' {
			return c, vimBad
		}
		c.key, c.arg = name, keys[i]
		return c, vimDone
	case len(name) == 1 && strings.ContainsRune(vimActions, k):
		c.key = name
		return c, vimDone
	}
	return parseMotion(c, keys, i, k, name, false)
}

// parseMotion reads a motion that starts with key k, named name with the
// key after a g, and a text object too where objects are.
func parseMotion(c vimCmd, keys []rune, i int, k rune, name string, objects bool) (vimCmd, int) {
	switch k {
	case 'h', 'j', 'k', 'l', 'w', 'W', 'b', 'B', 'e', 'E', '0', '^', '$', 'G', '%', '{', '}', ';', ',',
		'\r', '-', '+', '_', ' ', '\b':
		c.key = string(k)
		return c, vimDone
	case 'g':
		if len(name) < 2 {
			// The g of an operator's motion.
			if i == len(keys) {
				return c, vimMore
			}
			name += string(keys[i])
		}
		switch name {
		case "gg", "ge", "gE", "g_":
			c.key = name
			return c, vimDone
		}
	case 'f', 'F', 't', 'T':
		if i == len(keys) {
			return c, vimMore
		}
		if keys[i] < ' ' {
			return c, vimBad
		}
		c.key, c.arg = string(k), keys[i]
		return c, vimDone
	case 'i', 'a':
		if !objects {
			break
		}
		if i == len(keys) {
			return c, vimMore
		}
		if strings.ContainsRune("wW\"'`()b[]{}B<>", keys[i]) {
			c.key = string(k) + string(keys[i])
			return c, vimDone
		}
	}
	return c, vimBad
}

// next is the mode a command typed in mode leaves the editor in.
func (c vimCmd) next(mode vimMode) vimMode {
	if mode.visual() {
		switch c.key {
		case "v":
			if mode == vimVisual {
				return vimNormal
			}
			return vimVisual
		case "V":
			if mode == vimVisualLine {
				return vimNormal
			}
			return vimVisualLine
		case "c", "s", "C", "S", "R":
			return vimInsert
		case "o":
			return mode
		}
		if vimVisualActions[c.key] || c.key == "r" {
			return vimNormal
		}
		return mode
	}
	switch {
	case c.op == "c":
		return vimInsert
	case c.op != "":
		return vimNormal
	}
	switch c.key {
	case "i", "a", "I", "A", "o", "O", "s", "S", "C":
		return vimInsert
	case "v":
		return vimVisual
	case "V":
		return vimVisualLine
	}
	return vimNormal
}

// changes reports whether a command of normal mode changes the text, as
// . repeats.
func (c vimCmd) changes() bool {
	if c.op != "" {
		return c.op != "y"
	}
	switch c.key {
	case "x", "\x7f", "X", "D", "C", "s", "S", "r", "J", "p", "P", "~", "o", "O", "i", "a", "I", "A":
		return true
	}
	return false
}

// vimRune is the key of an event as vim names it: the character it types,
// a control character with Ctrl, and the motion of an arrow. Without
// allCtrl, ⌃F, ⌃B and ⌃R are left to the shortcuts of Find, Toggle Sidebar
// and Open Recent.
func vimRune(ev ui.InputEvent, allCtrl bool) (rune, bool) {
	m, k := ev.Mods, ev.Key
	if m&(ui.Alt|ui.Super) != 0 {
		return 0, false
	}
	if m&ui.Ctrl != 0 {
		// Only the keys of vim's commands: the others stay the
		// platform's, where Control is the key of shortcuts.
		switch k {
		case ui.KeyF, ui.KeyB, ui.KeyR:
			if !allCtrl {
				return 0, false
			}
			return rune(k-ui.KeyA) + 1, true
		case ui.KeyD, ui.KeyU, ui.KeyE, ui.KeyY:
			return rune(k-ui.KeyA) + 1, true
		case ui.KeyBracketLeft:
			return '\x1b', true
		}
		return 0, false
	}
	shift := m&ui.Shift != 0
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		r := 'a' + rune(k-ui.KeyA)
		if shift {
			r = unicode.ToUpper(r)
		}
		return r, true
	case k >= ui.Key0 && k <= ui.Key9:
		if shift {
			return rune(")!@#$%^&*("[k-ui.Key0]), true
		}
		return '0' + rune(k-ui.Key0), true
	}
	if p, ok := vimPunct[k]; ok {
		if shift {
			return rune(p[1]), true
		}
		return rune(p[0]), true
	}
	switch k {
	case ui.KeySpace:
		return ' ', true
	case ui.KeyEnter:
		return '\r', true
	case ui.KeyEscape:
		return '\x1b', true
	case ui.KeyBackspace:
		return '\b', true
	case ui.KeyTab:
		return '\t', true
	case ui.KeyDelete:
		return '\x7f', true
	case ui.KeyLeft:
		return 'h', true
	case ui.KeyRight:
		return 'l', true
	case ui.KeyUp:
		return 'k', true
	case ui.KeyDown:
		return 'j', true
	case ui.KeyHome:
		return '0', true
	case ui.KeyEnd:
		return '$', true
	case ui.KeyPageDown:
		return '\x06', true
	case ui.KeyPageUp:
		return '\x02', true
	}
	return 0, false
}

// vimPunct are the characters of the punctuation keys, without Shift and
// with it, as a US keyboard types them.
var vimPunct = map[ui.Key]string{
	ui.KeyMinus: "-_", ui.KeyEqual: "=+", ui.KeyComma: ",<", ui.KeyPeriod: ".>", ui.KeySlash: "/?",
	ui.KeySemicolon: ";:", ui.KeyQuote: `'"`, ui.KeyBracketLeft: "[{", ui.KeyBracketRight: "]}",
	ui.KeyBackslash: `\|`, ui.KeyBackquote: "`~",
}

// vimTakesKeys reports whether the editor with the keyboard takes its
// keys as vim's commands, rather than as typing: Control's keys go to it
// before the view's shortcuts.
func (a *app) vimTakesKeys(c *ui.Context) bool {
	d := a.activeEditor()
	return a.settings.Vim && d != nil && d.preview == nil && d.vim.mode != vimInsert && d.vscroll.Focused(c)
}

// vimKey takes the keys of an editor in vim mode, reporting whether it
// took ev. A command typed whole runs in the next frame, once the
// selection is known; the mode it leaves changes now, so that the keys
// after it are taken as they should.
func (a *app) vimKey(d *editor, ev ui.InputEvent) bool {
	v := &d.vim
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if v.mode == vimInsert {
		// ⌃C as well where it is not Copy.
		esc := ev.Mods == 0 && ev.Key == ui.KeyEscape ||
			ev.Mods == ui.Ctrl && (ev.Key == ui.KeyBracketLeft || ev.Key == ui.KeyC && ui.Cmd != ui.Ctrl)
		if !esc {
			return false
		}
		v.mode = vimNormal
		a.queueEdit(d, a.vimEndInsert)
		return true
	}
	r, ok := vimRune(ev, a.vimCtrl())
	if !ok {
		return false
	}
	if v.mode == vimNormal && len(d.cmds) == 0 && d.sel[0] != d.sel[1] {
		// A selection the pointer made: it is visual mode's.
		v.mode = vimVisual
		a.queueEdit(d, func(d *editor) {
			lo, hi := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
			if d.sel[1] < d.sel[0] {
				d.vim.anchor, d.vim.cursor = hi-1, lo
			} else {
				d.vim.anchor, d.vim.cursor = lo, hi-1
			}
		})
	}
	v.keys = append(v.keys, r)
	cmd, st := parseVim(v.keys, v.mode)
	switch st {
	case vimMore:
		return true
	case vimBad:
		v.keys = v.keys[:0]
		return true
	}
	v.keys = v.keys[:0]
	mode := v.mode
	next := cmd.next(mode)
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
	return true
}

// vimReset leaves vim mode's state, as vim mode is turned off.
func vimReset(d *editor) {
	if d.vim.mode != vimNormal || len(d.vim.keys) > 0 || d.hist.group != groupNone {
		d.vim.mode, d.vim.keys = vimNormal, nil
		d.vim.recording = false
		d.hist.group = groupNone
	}
}

// vimPut puts the caret at c.
func vimPut(d *editor, c int) {
	d.selectRange(c, c, false)
	d.sel = [2]int{c, c}
}

// vimLast is the last line as vim counts them: the empty line after the
// newline that ends a file is not one.
func vimLast(snap ui.TextSnapshot) int {
	last := snap.LineCount() - 1
	if s, e := snap.LineRange(last); last > 0 && s == e {
		last--
	}
	return last
}

// vimClamp keeps the cursor of normal mode on a character of a line, off
// the newline at its end and the empty line after the last.
func vimClamp(snap ui.TextSnapshot, c int) int {
	c = min(max(c, 0), snap.Len())
	l := min(snap.LineAt(c), vimLast(snap))
	s, e := snap.LineRange(l)
	return min(max(c, s), max(e-1, s))
}

// vimSettle puts the caret of normal mode on a character.
func vimSettle(d *editor) {
	snap := d.buf.Snapshot()
	c := vimClamp(snap, d.sel[1])
	if d.sel != [2]int{c, c} {
		vimPut(d, c)
	}
}

// vimCursor is where the block cursor shows: the caret in normal mode,
// the moving end of the selection in visual mode.
func vimCursor(d *editor, snap ui.TextSnapshot) int {
	if d.vim.mode.visual() {
		return vimClamp(snap, d.vim.cursor)
	}
	return vimClamp(snap, d.sel[1])
}

// vtext reads the runes of a text by its lines, keeping the one read last.
type vtext struct {
	snap   ui.TextSnapshot
	n      int
	line   int
	ls, le int
	runes  []rune
}

func newVText(snap ui.TextSnapshot) *vtext { return &vtext{snap: snap, n: snap.Len(), line: -1} }

// at is the rune at i: '\n' at the end of a line, 0 past the text.
func (t *vtext) at(i int) rune {
	if i < 0 || i >= t.n {
		return 0
	}
	if t.line < 0 || i < t.ls || i > t.le {
		t.line = t.snap.LineAt(i)
		t.ls, t.le = t.snap.LineRange(t.line)
		t.runes = []rune(t.snap.Slice(t.ls, t.le))
	}
	if i >= t.le {
		return '\n'
	}
	return t.runes[i-t.ls]
}

func vimSpace(r rune) bool { return r == 0 || unicode.IsSpace(r) }

// vimClass is the kind of a rune, which words are runs of: 0 space, 1 a
// letter, a digit or _, 2 anything else; for WORDs, 1 anything not space.
func vimClass(r rune, big bool) int {
	switch {
	case vimSpace(r):
		return 0
	case big || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	}
	return 2
}

// wordFwd is where the word after i starts, or an empty line.
func wordFwd(t *vtext, i int, big bool) int {
	if i >= t.n {
		return t.n
	}
	if c := vimClass(t.at(i), big); c != 0 {
		for i < t.n && vimClass(t.at(i), big) == c {
			i++
		}
	}
	for i < t.n {
		r := t.at(i)
		if r == '\n' {
			i++
			if i < t.n && t.at(i) == '\n' {
				return i
			}
			continue
		}
		if !vimSpace(r) {
			break
		}
		i++
	}
	return i
}

// wordEnd is the last rune of the word that ends after i.
func wordEnd(t *vtext, i int, big bool) int {
	if i >= t.n-1 {
		return i
	}
	i++
	for i < t.n-1 && vimSpace(t.at(i)) {
		i++
	}
	c := vimClass(t.at(i), big)
	for i+1 < t.n && c != 0 && vimClass(t.at(i+1), big) == c {
		i++
	}
	return i
}

// wordBack is where the word before i starts, or an empty line.
func wordBack(t *vtext, i int, big bool) int {
	if i <= 0 {
		return 0
	}
	i--
	for i > 0 && vimSpace(t.at(i)) {
		if t.at(i) == '\n' && t.at(i-1) == '\n' {
			return i
		}
		i--
	}
	c := vimClass(t.at(i), big)
	for i > 0 && c != 0 && vimClass(t.at(i-1), big) == c {
		i--
	}
	return i
}

// wordEndBack is the last rune of the word before the one at i.
func wordEndBack(t *vtext, i int, big bool) int {
	if c := vimClass(t.at(i), big); c != 0 {
		for i > 0 && vimClass(t.at(i), big) == c {
			i--
		}
	}
	for i > 0 && vimSpace(t.at(i)) {
		i--
	}
	return i
}

// firstNonBlank is the offset of the first rune of line l that is not
// indent.
func firstNonBlank(snap ui.TextSnapshot, l int) int {
	s, e := snap.LineRange(l)
	return s + utf8.RuneCountInString(leading(snap.Slice(s, e)))
}

// vimCol is the column of offset i in its line.
func vimCol(snap ui.TextSnapshot, i int) int {
	s, _ := snap.LineRange(snap.LineAt(i))
	return columns(snap.Slice(s, i))
}

// vimAtCol is the offset of line l at column col, or of its last rune when
// it is shorter.
func vimAtCol(snap ui.TextSnapshot, l, col int) int {
	s, e := snap.LineRange(l)
	c, i := 0, s
	for _, r := range snap.Slice(s, e) {
		w := 1
		if r == '\t' {
			w = tabWidth - c%tabWidth
		}
		if c+w > col {
			break
		}
		c += w
		i++
	}
	if i >= e && e > s {
		i = e - 1
	}
	return i
}

// vimTarget is where a motion goes: to, or the runes from up to to of a
// text object.
type vimTarget struct {
	from, to  int
	linewise  bool
	inclusive bool
	object    bool
}

// vimMotion moves from cur by a motion, for an operator op or "" to move
// the cursor, reporting false where it cannot.
func (a *app) vimMotion(d *editor, t *vtext, cur int, key string, arg rune, count int, op string) (vimTarget, bool) {
	v := &d.vim
	snap := t.snap
	cnt := max(count, 1)
	line := snap.LineAt(cur)
	ls, le := snap.LineRange(line)
	last := vimLast(snap)
	if key != "j" && key != "k" {
		v.hasWant = false
	}
	switch key {
	case "h", "\b":
		return vimTarget{to: max(ls, cur-cnt)}, cur > ls
	case "l", " ":
		lim := le
		if op == "" {
			lim = max(le-1, ls)
		}
		return vimTarget{to: min(cur+cnt, lim)}, cur < lim
	case "j", "k", "+", "-", "\r", "_":
		by := cnt
		switch key {
		case "k", "-":
			by = -cnt
		case "_":
			by = cnt - 1
		}
		to := min(max(line+by, 0), last)
		if to == line && by != 0 {
			return vimTarget{}, false
		}
		if key == "j" || key == "k" {
			if !v.hasWant {
				v.want, v.hasWant = vimCol(snap, cur), true
			}
			return vimTarget{to: vimAtCol(snap, to, v.want), linewise: true}, true
		}
		return vimTarget{to: firstNonBlank(snap, to), linewise: true}, true
	case "w", "W":
		big := key == "W"
		if op == "c" && !vimSpace(t.at(cur)) {
			// cw changes to the end of the word, as ce.
			to := cur
			for k := range cnt {
				if k > 0 || vimClass(t.at(to+1), big) == vimClass(t.at(to), big) {
					to = wordEnd(t, to, big)
				}
			}
			return vimTarget{to: to, inclusive: true}, true
		}
		to := cur
		for range cnt {
			to = wordFwd(t, to, big)
		}
		if op != "" {
			// An operator stops at the end of the line of the last word
			// rather than going on to the next.
			if tl := snap.LineAt(to); tl > line {
				if s, _ := snap.LineRange(tl); strings.TrimSpace(snap.Slice(s, to)) == "" {
					_, pe := snap.LineRange(tl - 1)
					to = max(pe, cur)
				}
			}
		}
		return vimTarget{to: to}, to != cur
	case "b", "B":
		to := cur
		for range cnt {
			to = wordBack(t, to, key == "B")
		}
		return vimTarget{to: to}, to != cur
	case "e", "E":
		to := cur
		for range cnt {
			to = wordEnd(t, to, key == "E")
		}
		return vimTarget{to: to, inclusive: true}, to != cur
	case "ge", "gE":
		to := cur
		for range cnt {
			to = wordEndBack(t, to, key == "gE")
		}
		return vimTarget{to: to, inclusive: true}, to != cur
	case "0":
		return vimTarget{to: ls}, true
	case "^":
		return vimTarget{to: firstNonBlank(snap, line)}, true
	case "$":
		tl := min(line+cnt-1, last)
		s, e := snap.LineRange(tl)
		to := e
		if op == "" {
			to = max(e-1, s)
		}
		v.want, v.hasWant = 1<<30, true
		return vimTarget{to: to}, true
	case "g_":
		tl := min(line+cnt-1, last)
		s, e := snap.LineRange(tl)
		text := snap.Slice(s, e)
		to := s + utf8.RuneCountInString(strings.TrimRight(text, " \t")) - 1
		return vimTarget{to: max(to, s), inclusive: true}, true
	case "G", "gg":
		tl := last
		if key == "gg" {
			tl = 0
		}
		if count > 0 {
			tl = min(count-1, last)
		}
		return vimTarget{to: firstNonBlank(snap, tl), linewise: true}, true
	case "f", "F", "t", "T":
		v.find = vimCmd{key: key, arg: arg}
		return vimFindChar(snap, cur, key, arg, cnt)
	case ";", ",":
		if v.find.key == "" {
			return vimTarget{}, false
		}
		k := v.find.key
		if key == "," {
			k = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}[k]
		}
		return vimFindChar(snap, cur, k, v.find.arg, cnt)
	case "%":
		for i := cur; i < le; i++ {
			switch t.at(i) {
			case '(', ')', '[', ']', '{', '}':
				if _, pair, ok := matchBracket(snap, i+1); ok {
					return vimTarget{to: pair, inclusive: true}, true
				}
				return vimTarget{}, false
			}
		}
		return vimTarget{}, false
	case "{", "}":
		l := line
		blank := func(l int) bool { s, e := snap.LineRange(l); return s == e }
		for range cnt {
			if key == "}" {
				for l < last && blank(l) {
					l++
				}
				for l < last && !blank(l) {
					l++
				}
			} else {
				for l > 0 && blank(l) {
					l--
				}
				for l > 0 && !blank(l) {
					l--
				}
			}
		}
		to, _ := snap.LineRange(l)
		if key == "}" && !blank(l) {
			_, to = snap.LineRange(l)
		}
		return vimTarget{to: to}, to != cur
	}
	if len(key) == 2 && (key[0] == 'i' || key[0] == 'a') {
		from, to, ok := vimObject(t, cur, key, cnt)
		return vimTarget{from: from, to: to, object: true}, ok
	}
	return vimTarget{}, false
}

// vimFindChar finds the cnt-th ch along the cursor's line, after it for f
// and t, before it for F and T.
func vimFindChar(snap ui.TextSnapshot, cur int, key string, ch rune, cnt int) (vimTarget, bool) {
	ls, le := snap.LineRange(snap.LineAt(cur))
	runes := []rune(snap.Slice(ls, le))
	i := cur - ls
	switch key {
	case "f", "t":
		j := i
		if key == "t" && j+1 < len(runes) && runes[j+1] == ch {
			j++ // as ; repeats t: past the one it stopped before
		}
		for range cnt {
			j++
			for j < len(runes) && runes[j] != ch {
				j++
			}
			if j >= len(runes) {
				return vimTarget{}, false
			}
		}
		to := ls + j
		if key == "t" {
			to--
		}
		return vimTarget{to: to, inclusive: true}, true
	default:
		j := i
		if key == "T" && j-1 >= 0 && runes[j-1] == ch {
			j--
		}
		for range cnt {
			j--
			for j >= 0 && runes[j] != ch {
				j--
			}
			if j < 0 {
				return vimTarget{}, false
			}
		}
		to := ls + j
		if key == "T" {
			to++
		}
		return vimTarget{to: to}, true
	}
}

// vimBrackets are the brackets of the text objects that name them.
var vimBrackets = map[byte][2]rune{
	'(': {'(', ')'}, ')': {'(', ')'}, 'b': {'(', ')'},
	'[': {'[', ']'}, ']': {'[', ']'},
	'{': {'{', '}'}, '}': {'{', '}'}, 'B': {'{', '}'},
	'<': {'<', '>'}, '>': {'<', '>'},
}

// vimObject finds the runes of a text object around cur: from up to to.
func vimObject(t *vtext, cur int, key string, cnt int) (from, to int, ok bool) {
	snap := t.snap
	inner := key[0] == 'i'
	ls, le := snap.LineRange(snap.LineAt(cur))
	switch k := key[1]; k {
	case 'w', 'W':
		if ls == le {
			return 0, 0, false
		}
		big := k == 'W'
		pos := min(cur, le-1)
		cls := vimClass(t.at(pos), big)
		s, e := pos, pos+1
		for s > ls && vimClass(t.at(s-1), big) == cls {
			s--
		}
		for e < le && vimClass(t.at(e), big) == cls {
			e++
		}
		if inner {
			return s, e, true
		}
		if cls == 0 {
			// Space, and the word after it.
			if e < le {
				c := vimClass(t.at(e), big)
				for e < le && vimClass(t.at(e), big) == c {
					e++
				}
			}
			return s, e, true
		}
		// The word and the space after it, or else the space before it.
		after := e
		for after < le && vimSpace(t.at(after)) {
			after++
		}
		if after > e {
			return s, after, true
		}
		for s > ls && vimSpace(t.at(s-1)) {
			s--
		}
		return s, e, true
	case '"', '\'', '`':
		q := rune(k)
		var quotes []int
		for i := ls; i < le; i++ {
			if t.at(i) == q && (i == ls || t.at(i-1) != '\\') {
				quotes = append(quotes, i)
			}
		}
		for p := 0; p+1 < len(quotes); p += 2 {
			q1, q2 := quotes[p], quotes[p+1]
			if cur <= q2 {
				if inner {
					return q1 + 1, q2, true
				}
				e := q2 + 1
				for e < le && vimSpace(t.at(e)) {
					e++
				}
				return q1, e, true
			}
		}
		return 0, 0, false
	}
	pair, isBracket := vimBrackets[key[1]]
	if !isBracket {
		return 0, 0, false
	}
	open, close := pair[0], pair[1]
	floor := max(0, cur-maxBracketScan)
	back := func(from int) int {
		depth := 0
		for j := from; j >= floor; j-- {
			switch t.at(j) {
			case close:
				depth++
			case open:
				if depth == 0 {
					return j
				}
				depth--
			}
		}
		return -1
	}
	o := -1
	if t.at(cur) == open {
		o = cur
	} else {
		o = back(cur - 1)
	}
	for k := 1; k < cnt && o >= 0; k++ {
		o = back(o - 1)
	}
	if o < 0 {
		return 0, 0, false
	}
	c, depth := -1, 0
	for j := o + 1; j < min(t.n, o+1+maxBracketScan); j++ {
		r := t.at(j)
		if r == open {
			depth++
		} else if r == close {
			if depth == 0 {
				c = j
				break
			}
			depth--
		}
	}
	if c < 0 {
		return 0, 0, false
	}
	if !inner {
		return o, c + 1, true
	}
	from, to = o+1, c
	// Inside a block over lines, the lines between the brackets.
	if t.at(from) == '\n' {
		from++
	}
	if cl := snap.LineAt(c); cl > snap.LineAt(o) {
		if s, _ := snap.LineRange(cl); strings.TrimSpace(snap.Slice(s, c)) == "" {
			to = max(s, from)
		}
	}
	return from, max(to, from), true
}

// vimRange is the text an operator works on: runes start up to end, or
// lines l1 to l2.
type vimRange struct {
	start, end int
	linewise   bool
	l1, l2     int
}

// vimYank puts the text of a range on the clipboard.
func (a *app) vimYank(snap ui.TextSnapshot, r vimRange) {
	var text string
	if r.linewise {
		s, _ := snap.LineRange(r.l1)
		_, e := snap.LineRange(r.l2)
		text = snap.Slice(s, e) + "\n"
	} else {
		text = snap.Slice(r.start, r.end)
	}
	a.vimReg = vimRegister{text, r.linewise}
	a.services.WriteClipboard(text)
}

// vimClip is the clipboard's text, and whether it pastes as lines: as
// vim mode yanked it, or when it ends with a newline.
func (a *app) vimClip() (string, bool) {
	text := strings.ReplaceAll(a.services.ReadClipboard(), "\r\n", "\n")
	if text == a.vimReg.text {
		return text, a.vimReg.linewise
	}
	return text, strings.HasSuffix(text, "\n")
}

// vimOperate applies an operator to the text from cur to where a motion
// goes.
func (a *app) vimOperate(d *editor, t *vtext, cur int, c vimCmd) {
	snap := t.snap
	var r vimRange
	if c.key == "line" {
		l := snap.LineAt(cur)
		r = vimRange{linewise: true, l1: l, l2: min(l+max(c.count, 1)-1, vimLast(snap))}
	} else {
		tg, ok := a.vimMotion(d, t, cur, c.key, c.arg, c.count, c.op)
		switch {
		case !ok:
			if c.op == "c" {
				d.vim.mode, d.vim.recording = vimNormal, false
			}
			return
		case tg.object:
			r = vimRange{start: tg.from, end: tg.to}
		case tg.linewise:
			l1, l2 := snap.LineAt(cur), snap.LineAt(tg.to)
			r = vimRange{linewise: true, l1: min(l1, l2), l2: max(l1, l2)}
		default:
			r = vimRange{start: min(cur, tg.to), end: max(cur, tg.to)}
			if tg.inclusive {
				r.end = min(r.end+1, t.n)
			}
		}
	}
	a.vimApply(d, c.op, r, cur)
}

// vimApply applies an operator to a range, with the cursor at cur.
func (a *app) vimApply(d *editor, op string, r vimRange, cur int) {
	snap := d.buf.Snapshot()
	if r.linewise {
		r.start, _ = snap.LineRange(r.l1)
		_, r.end = snap.LineRange(r.l2)
	} else {
		r.l1, r.l2 = snap.LineAt(r.start), snap.LineAt(max(r.end-1, r.start))
	}
	switch op {
	case "y":
		a.vimYank(snap, r)
		caret := r.start
		if r.linewise {
			caret = cur
			if snap.LineAt(cur) != r.l1 {
				caret = vimAtCol(snap, r.l1, vimCol(snap, cur))
			}
		}
		vimPut(d, caret)
	case "d":
		a.vimYank(snap, r)
		if !r.linewise {
			if r.end > r.start {
				d.edit([2]int{r.start, r.start}, textEdit{r.start, r.end, ""})
			}
			return
		}
		start, end := r.start, r.end
		if r.l2+1 < snap.LineCount() {
			end++ // and the newline after them
		} else if r.l1 > 0 {
			start-- // the last lines: the newline before them
		}
		d.edit([2]int{start, start}, textEdit{start, end, ""})
		after := d.buf.Snapshot()
		vimPut(d, firstNonBlank(after, min(r.l1, vimLast(after))))
	case "c":
		a.vimYank(snap, r)
		at := r.start
		if r.linewise {
			// The lines go, but for the indent of the first.
			s, e := snap.LineRange(r.l1)
			at = s + utf8.RuneCountInString(leading(snap.Slice(s, e)))
		}
		edited := r.end > at
		if edited {
			d.edit([2]int{at, at}, textEdit{at, r.end, ""})
		}
		a.vimStartInsert(d, at, edited)
	case ">", "<", "gc":
		s, _ := snap.LineRange(r.l1)
		_, e := snap.LineRange(r.l2)
		if r.l1 == r.l2 {
			e = s
		}
		d.sel = [2]int{s, e}
		if op == "gc" {
			toggleComment(d)
		} else {
			indentLines(d, op == "<")
		}
		vimPut(d, firstNonBlank(d.buf.Snapshot(), r.l1))
	case "g~", "gu", "gU":
		if r.linewise {
			r.end = min(r.end, snap.Len())
		}
		text := snap.Slice(r.start, r.end)
		var conv string
		switch op {
		case "gu":
			conv = strings.ToLower(text)
		case "gU":
			conv = strings.ToUpper(text)
		default:
			conv = toggleCase(text)
		}
		if conv != text {
			d.edit([2]int{r.start, r.start}, textEdit{r.start, r.end, conv})
		}
		vimPut(d, r.start)
	}
}

func toggleCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// vimStartInsert starts insert mode at at, joining what is typed to the
// step of the edit the command made, if it edited. As . makes a change
// again, it types the text of the last insert instead.
func (a *app) vimStartInsert(d *editor, at int, edited bool) {
	v := &d.vim
	if v.replay {
		if v.lastText != "" {
			if edited {
				d.hist.group = groupJoin
			}
			n := utf8.RuneCountInString(v.lastText)
			d.edit([2]int{at + n, at + n}, textEdit{at, at, v.lastText})
			d.hist.group = groupNone
			at += n - 1
		}
		vimPut(d, at)
		return
	}
	vimPut(d, at)
	v.insAt, v.insLen = at, d.buf.Len()
	if edited {
		d.hist.group = groupJoin
	} else {
		d.hist.group = groupOpen
	}
}

// vimEndInsert ends insert mode: the step of undo, and the caret back on
// the last rune typed. The text typed is kept for . to type again.
func (a *app) vimEndInsert(d *editor) {
	d.observe()
	v := &d.vim
	snap := d.buf.Snapshot()
	if v.recording {
		v.recording, v.lastText = false, ""
		grown := snap.Len() - v.insLen
		if caret := d.sel[1]; grown > 0 && caret >= v.insAt && caret <= v.insAt+grown && v.insAt+grown <= snap.Len() {
			v.lastText = snap.Slice(v.insAt, v.insAt+grown)
		}
	}
	d.hist.group, d.hist.typing = groupNone, false
	c := min(d.sel[0], d.sel[1], snap.Len())
	if s, _ := snap.LineRange(snap.LineAt(c)); c > s {
		c--
	}
	vimPut(d, vimClamp(snap, c))
}

// vimNormalRun runs a command of normal mode, which leaves the editor in
// mode next.
func (a *app) vimNormalRun(d *editor, c vimCmd, next vimMode) {
	v := &d.vim
	snap := d.buf.Snapshot()
	t := newVText(snap)
	cur := vimClamp(snap, min(d.sel[0], d.sel[1]))
	if d.sel[0] == d.sel[1] {
		cur = vimClamp(snap, d.sel[1])
	}
	cnt := max(c.count, 1)
	line := snap.LineAt(cur)
	ls, le := snap.LineRange(line)
	last := vimLast(snap)
	if !v.replay && c.changes() {
		v.last, v.hasLast, v.lastText = c, true, ""
		v.recording = next == vimInsert
	}
	if c.op != "" {
		a.vimOperate(d, t, cur, c)
		return
	}
	switch c.key {
	case "x", "\x7f":
		if le > ls {
			a.vimApply(d, "d", vimRange{start: cur, end: min(cur+cnt, le)}, cur)
		}
	case "X":
		if cur > ls {
			a.vimApply(d, "d", vimRange{start: max(ls, cur-cnt), end: cur}, cur)
		}
	case "D", "C":
		_, e := snap.LineRange(min(line+cnt-1, last))
		op := map[string]string{"D": "d", "C": "c"}[c.key]
		a.vimApply(d, op, vimRange{start: cur, end: e}, cur)
	case "s":
		a.vimApply(d, "c", vimRange{start: cur, end: min(cur+cnt, le)}, cur)
	case "S":
		a.vimApply(d, "c", vimRange{linewise: true, l1: line, l2: min(line+cnt-1, last)}, cur)
	case "Y":
		a.vimApply(d, "y", vimRange{linewise: true, l1: line, l2: min(line+cnt-1, last)}, cur)
	case "p", "P":
		a.vimPaste(d, cur, c.key == "P", cnt)
	case "o", "O":
		vimPut(d, cur)
		insertLine(d, c.key == "O")
		a.vimStartInsert(d, d.sel[1], true)
	case "J":
		vimJoin(d, line, max(cnt, 2))
	case "r":
		if le > ls && cur+cnt <= le {
			d.edit([2]int{cur + cnt - 1, cur + cnt - 1}, textEdit{cur, cur + cnt, strings.Repeat(string(c.arg), cnt)})
		}
	case "~":
		if e := min(cur+cnt, le); e > cur {
			text := snap.Slice(cur, e)
			to := min(e, le-1)
			if conv := toggleCase(text); conv != text {
				d.edit([2]int{to, to}, textEdit{cur, e, conv})
			}
			vimPut(d, to)
		}
	case "u", "\x12":
		for range cnt {
			if c.key == "u" && !d.undo() || c.key == "\x12" && !d.redo() {
				break
			}
		}
		vimPut(d, min(d.sel[0], d.sel[1]))
	case ".":
		if !v.hasLast {
			return
		}
		rc := v.last
		if c.count > 0 {
			rc.count, v.last.count = c.count, c.count
		}
		v.replay = true
		a.vimNormalRun(d, rc, vimNormal)
		v.replay = false
	case "i":
		a.vimStartInsert(d, cur, false)
	case "a":
		a.vimStartInsert(d, min(cur+1, le), false)
	case "I":
		a.vimStartInsert(d, firstNonBlank(snap, line), false)
	case "A":
		a.vimStartInsert(d, le, false)
	case "v", "V":
		v.anchor, v.cursor = cur, cur
		a.vimShowVisual(d, next)
	case "\x04", "\x15", "\x06", "\x02", "\x05", "\x19":
		a.vimScroll(d, snap, cur, c.key, c.count)
	default:
		if tg, ok := a.vimMotion(d, t, cur, c.key, c.arg, c.count, ""); ok && !tg.object {
			vimPut(d, tg.to)
		}
	}
}

// vimScroll scrolls the editor by half a view, a view or lines, and the
// cursor with it.
func (a *app) vimScroll(d *editor, snap ui.TextSnapshot, cur int, key string, count int) {
	v := &d.vim
	lineH := a.font.lineH
	if lineH <= 0 {
		return
	}
	rows := max(int(v.viewH/lineH), 1)
	var by int
	switch key {
	case "\x04":
		by = max(rows/2, 1)
	case "\x15":
		by = -max(rows/2, 1)
	case "\x06":
		by = max(rows-2, 1)
	case "\x02":
		by = -max(rows-2, 1)
	case "\x05":
		by = max(count, 1)
	case "\x19":
		by = -max(count, 1)
	}
	d.vscroll.Y = max(0, d.vscroll.Y+float32(by)*lineH)
	if d.vscroll.MaxY > 0 {
		d.vscroll.Y = min(d.vscroll.Y, d.vscroll.MaxY)
	}
	if key == "\x05" || key == "\x19" {
		return
	}
	line := snap.LineAt(cur)
	to := min(max(line+by, 0), vimLast(snap))
	if !v.hasWant {
		v.want, v.hasWant = vimCol(snap, cur), true
	}
	vimPut(d, vimAtCol(snap, to, v.want))
}

// vimPaste pastes the clipboard cnt times after the cursor, or before:
// lines below its line or above.
func (a *app) vimPaste(d *editor, cur int, before bool, cnt int) {
	text, linewise := a.vimClip()
	if text == "" {
		return
	}
	snap := d.buf.Snapshot()
	line := snap.LineAt(cur)
	ls, le := snap.LineRange(line)
	if linewise {
		body := strings.TrimSuffix(strings.Repeat(text, cnt), "\n")
		if before {
			d.edit(d.sel, textEdit{ls, ls, body + "\n"})
			vimPut(d, firstNonBlank(d.buf.Snapshot(), line))
		} else {
			d.edit(d.sel, textEdit{le, le, "\n" + body})
			vimPut(d, firstNonBlank(d.buf.Snapshot(), line+1))
		}
		return
	}
	body := strings.Repeat(text, cnt)
	at := cur
	if !before && le > ls {
		at = cur + 1
	}
	end := at + utf8.RuneCountInString(body) - 1
	d.edit([2]int{end, end}, textEdit{at, at, body})
}

// vimJoin joins count lines from line, with a space between each, but
// for the indent of those after the first.
func vimJoin(d *editor, line, count int) {
	snap := d.buf.Snapshot()
	l2 := min(line+count-1, vimLast(snap))
	if l2 <= line {
		return
	}
	s, e := snap.LineRange(line)
	text := snap.Slice(s, e)
	at := 0
	for l := line + 1; l <= l2; l++ {
		ls, le := snap.LineRange(l)
		next := strings.TrimLeft(snap.Slice(ls, le), " \t")
		sep := " "
		if text == "" || strings.HasSuffix(text, " ") || strings.HasSuffix(text, "\t") || next == "" || strings.HasPrefix(next, ")") {
			sep = ""
		}
		at = utf8.RuneCountInString(text)
		text += sep + next
	}
	_, end := snap.LineRange(l2)
	d.edit([2]int{s + at, s + at}, textEdit{s, end, text})
}

// vimShowVisual selects what visual mode does: the runes from the anchor
// to the cursor, or their lines, the cursor at the end that moves.
func (a *app) vimShowVisual(d *editor, mode vimMode) {
	v := &d.vim
	snap := d.buf.Snapshot()
	n := snap.Len()
	v.anchor, v.cursor = min(max(v.anchor, 0), n), min(max(v.cursor, 0), n)
	lo, hi := min(v.anchor, v.cursor), max(v.anchor, v.cursor)
	var s, e int
	if mode == vimVisualLine {
		s, _ = snap.LineRange(snap.LineAt(lo))
		_, e = snap.LineRange(snap.LineAt(hi))
		e = min(e+1, n)
	} else {
		s, e = lo, min(hi+1, n)
	}
	sel := [2]int{s, e}
	if v.cursor < v.anchor {
		sel = [2]int{e, s}
	}
	d.selectRange(sel[0], sel[1], false)
	d.sel = sel
}

// vimVisualRun runs a command of visual mode, typed in mode, which
// leaves the editor in mode next.
func (a *app) vimVisualRun(d *editor, c vimCmd, mode, next vimMode) {
	v := &d.vim
	snap := d.buf.Snapshot()
	t := newVText(snap)
	n := snap.Len()
	lo, hi := min(v.anchor, v.cursor, n), min(max(v.anchor, v.cursor), n)
	r := vimRange{start: lo, end: min(hi+1, n)}
	switch {
	case mode == vimVisualLine, c.key == "X", c.key == "D", c.key == "Y", c.key == "C", c.key == "S", c.key == "R":
		r = vimRange{linewise: true, l1: snap.LineAt(lo), l2: snap.LineAt(hi)}
	}
	switch c.key {
	case "\x1b", "v", "V":
		if next == vimNormal {
			vimPut(d, v.cursor)
		} else {
			a.vimShowVisual(d, next)
		}
	case "o":
		v.anchor, v.cursor = v.cursor, v.anchor
		a.vimShowVisual(d, next)
	case "d", "x", "X", "D":
		a.vimApply(d, "d", r, lo)
	case "y", "Y":
		a.vimApply(d, "y", r, lo)
	case "c", "s", "C", "S", "R":
		a.vimApply(d, "c", r, lo)
	case ">", "<", "gc", "g~", "gu", "gU":
		a.vimApply(d, c.key, r, lo)
	case "~":
		a.vimApply(d, "g~", r, lo)
	case "u":
		a.vimApply(d, "gu", r, lo)
	case "U":
		a.vimApply(d, "gU", r, lo)
	case "J":
		l1, l2 := snap.LineAt(lo), snap.LineAt(hi)
		vimJoin(d, l1, max(l2-l1+1, 2))
	case "r":
		s, e := r.start, r.end
		if r.linewise {
			s, _ = snap.LineRange(r.l1)
			_, e = snap.LineRange(r.l2)
		}
		text := snap.Slice(s, e)
		repl := strings.Map(func(x rune) rune {
			if x == '\n' {
				return x
			}
			return c.arg
		}, text)
		d.edit([2]int{s, s}, textEdit{s, e, repl})
	case "p", "P":
		text, linewise := a.vimClip()
		s, e := r.start, r.end
		body := text
		if r.linewise {
			s, _ = snap.LineRange(r.l1)
			_, e = snap.LineRange(r.l2)
			if linewise {
				body = strings.TrimSuffix(text, "\n")
			}
		} else if linewise {
			body = "\n" + strings.TrimSuffix(text, "\n") + "\n"
		}
		d.edit([2]int{s, s}, textEdit{s, e, body})
	default:
		if len(c.key) == 2 && (c.key[0] == 'i' || c.key[0] == 'a') {
			if from, to, ok := vimObject(t, v.cursor, c.key, c.count); ok && to > from {
				v.anchor, v.cursor = from, to-1
			}
		} else if tg, ok := a.vimMotion(d, t, v.cursor, c.key, c.arg, c.count, ""); ok {
			v.cursor = tg.to
		}
		a.vimShowVisual(d, next)
	}
}

// toggleVim turns vim mode on, the editors in normal mode, or off.
func (a *app) toggleVim() {
	a.settings.Vim = !a.settings.Vim
	a.settings.Changed()
	a.invalidate()
}
