package main

import (
	"mayoi/internal/settings"
	"mayoi/locales"
	"strconv"
	"strings"
	"time"
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
	// flash is the text yanked last, which shows for a moment from
	// flashAt: runes from up to to.
	flash   [2]int
	flashAt time.Time
	// lastInsert is where insert mode was left last, which gi inserts at.
	lastInsert int
	// indented is set while an insert goes on in a line that o, O, S or
	// cc gave the indent of the line it opened at, which Esc takes back
	// when nothing else was typed.
	indented bool
	// viewH is how high the editor was in the last frame, which ⌃D and
	// ⌃F scroll by.
	viewH float32
	// lastVisual is the selection of visual mode last left, which gv
	// selects again and '< and '> are the lines of.
	lastVisual vimVisualArea
	// reg is the register of the command running, 0 for the unnamed one.
	reg rune
	// insRepeat is how many times more the text of the insert going on is
	// typed as it ends, as 3i does; on lines of their own after o and O.
	insRepeat int
	insLines  bool
	// jumps are where the cursor jumped from, as G and searches do, which
	// ⌃O goes back through and ⌃I forward: jumpAt is the one gone back to,
	// len(jumps) when none is.
	jumps  []int
	jumpAt int
}

// vimVisualArea is a selection of visual mode: its ends and its mode.
type vimVisualArea struct {
	anchor, cursor int
	mode           vimMode
}

// vimFlashFor is how long the text yanked shows.
const vimFlashFor = 280 * time.Millisecond

// operatorPending reports whether an operator typed waits for its motion,
// as the cursor shows: half a block.
func (v *vimState) operatorPending() bool {
	if len(v.keys) == 0 || v.mode != vimNormal {
		return false
	}
	c, st := parseVim(v.keys, v.mode)
	return st == vimMore && c.op != ""
}

// pending is the keys of a command typed so far, as the status bar shows
// them after the mode: a control key as ^ and its letter.
func (v *vimState) pending() string {
	var b strings.Builder
	for _, r := range v.keys {
		if r < ' ' {
			b.WriteString("^" + string(r+'@'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
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
	// arg is the character of f, F, t, T and r, the mark of m, ' and `,
	// and the register of q and @.
	arg rune
	// reg is the register typed before the command, as "a; 0 for none.
	reg rune
	// visual is the mode of a change made in visual mode, which . makes
	// again over as many lines below the cursor, down to column col, or
	// as many runes after it on one line: span.
	visual    vimMode
	span, col int
}

const (
	vimMore = iota // the command goes on
	vimDone
	vimBad
)

var vimOps = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true, "gc": true, "g~": true, "gu": true, "gU": true}

// vimActions are the keys of normal mode that do something themselves.
const vimActions = "xXDCYpPoOJsS~u.iaIAvV:&\t\x0f\x04\x15\x06\x02\x05\x19\x12\x7f\x01\x18"

// vimPairActions are the commands of normal mode of two keys, after a g,
// a z or a Z.
var vimPairActions = map[string]bool{
	"gv": true, "gi": true, "gJ": true, "gp": true, "gP": true, "g&": true,
	"zz": true, "zt": true, "zb": true, "z.": true, "z\r": true, "z-": true,
	"ZZ": true, "ZQ": true,
}

// vimVisualActions are the keys of visual mode that do something to the
// selection, or with it.
var vimVisualActions = map[string]bool{
	"d": true, "x": true, "X": true, "D": true, "y": true, "Y": true, "c": true, "s": true, "C": true, "S": true,
	"R": true, ">": true, "<": true, "~": true, "u": true, "U": true, "J": true, "p": true, "P": true,
	"gc": true, "g~": true, "gu": true, "gU": true, "o": true, "v": true, "V": true, "\x1b": true,
	":": true, "zz": true, "zt": true, "zb": true, "z.": true, "z\r": true, "z-": true,
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
	if i < len(keys) && keys[i] == '"' {
		// A register, as "a, then perhaps a count.
		if i+1 == len(keys) {
			return c, vimMore
		}
		if !vimRegisterName(keys[i+1]) {
			return c, vimBad
		}
		c.reg = keys[i+1]
		i += 2
		if n := count(); n > 0 {
			n1 = max(n1, 1) * n
		}
	}
	c.count = n1
	if i == len(keys) {
		return c, vimMore
	}
	k := keys[i]
	i++
	name := string(k)
	if k == 'g' || k == 'z' || k == 'Z' {
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
		case k == 'z' || k == 'Z':
			return c, vimBad
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
		if keys[i] < ' ' && keys[i] != '\r' {
			return c, vimBad
		}
		c.key, c.arg = name, keys[i]
		return c, vimDone
	case name == "m", name == "q", name == "@":
		// A mark set, a register recorded into, or one played.
		if i == len(keys) {
			return c, vimMore
		}
		r := keys[i]
		switch {
		case name == "m" && !vimMarkName(r, true),
			name == "q" && !(r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '"')),
			name == "@" && !(vimRegisterName(r) || r == '@'):
			return c, vimBad
		}
		c.key, c.arg = name, r
		return c, vimDone
	case len(name) == 1 && strings.ContainsRune(vimActions, k), vimPairActions[name]:
		c.key = name
		return c, vimDone
	case k == 'z' || k == 'Z':
		return c, vimBad
	}
	return parseMotion(c, keys, i, k, name, false)
}

// parseMotion reads a motion that starts with key k, named name with the
// key after a g, and a text object too where objects are.
func parseMotion(c vimCmd, keys []rune, i int, k rune, name string, objects bool) (vimCmd, int) {
	switch k {
	case 'h', 'j', 'k', 'l', 'w', 'W', 'b', 'B', 'e', 'E', '0', '^', '$', 'G', '%', '{', '}', ';', ',',
		'\r', '-', '+', '_', ' ', '\b', 'H', 'M', 'L', 'n', 'N', '*', '#', '/', '?':
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
	case 'f', 'F', 't', 'T', '\'', '`':
		if i == len(keys) {
			return c, vimMore
		}
		if keys[i] < ' ' || (k == '\'' || k == '`') && !vimMarkName(keys[i], false) {
			return c, vimBad
		}
		c.key, c.arg = string(k), keys[i]
		return c, vimDone
	case '[', ']':
		// To the bracket that holds the cursor: [( [{ ]) ]}.
		if i == len(keys) {
			return c, vimMore
		}
		switch name := string(k) + string(keys[i]); name {
		case "[(", "[{", "])", "]}":
			c.key = name
			return c, vimDone
		}
	case 'i', 'a':
		if !objects {
			break
		}
		if i == len(keys) {
			return c, vimMore
		}
		if strings.ContainsRune("wWpt\"'`()b[]{}B<>", keys[i]) {
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
		case "ip", "ap":
			return vimVisualLine
		case ":":
			return vimNormal
		case "zz", "zt", "zb", "z.", "z\r", "z-":
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
	case "gi":
		return vimInsert
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
	case "x", "\x7f", "X", "D", "C", "s", "S", "r", "J", "gJ", "p", "P", "gp", "gP", "~", "o", "O", "i", "a", "I", "A",
		"gi", "&", "g&", "\x01", "\x18":
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
	if k == ui.KeyUnknown {
		// A layout of another script, as Cyrillic: the key where a US
		// keyboard has it, as vim's commands are learnt.
		k = ev.PhysicalKey
	}
	if m&(ui.Alt|ui.Super) != 0 {
		return 0, false
	}
	if m&ui.Ctrl != 0 {
		// Only the keys of vim's commands: the others stay the
		// platform's, where Control is the key of shortcuts.
		switch k {
		case ui.KeyF, ui.KeyB, ui.KeyR, ui.KeyA, ui.KeyX, ui.KeyO:
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
	return a.prefs().Vim && d != nil && d.preview == nil && d.vim.mode != vimInsert && d.vscroll.Focused(c)
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
		return a.vimInsertKey(d, ev)
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
	a.vimTake(d, r)
	return true
}

// vimInsertKey takes the keys of insert mode that are vim's rather than
// typing: Esc, and where Control is not the key of shortcuts, ⌃W and ⌃U.
func (a *app) vimInsertKey(d *editor, ev ui.InputEvent) bool {
	ctrl := ev.Mods == ui.Ctrl && ui.Cmd != ui.Ctrl
	switch {
	case ev.Mods == 0 && ev.Key == ui.KeyEscape, ev.Mods == ui.Ctrl && ev.Key == ui.KeyBracketLeft,
		ctrl && ev.Key == ui.KeyC:
		// ⌃C as well where it is not Copy.
		a.vimRecordInsert(d)
		d.vim.mode = vimNormal
		a.queueEdit(d, a.vimEndInsert)
		return true
	case ctrl && (ev.Key == ui.KeyW || ev.Key == ui.KeyU):
		word := ev.Key == ui.KeyW
		a.queueEdit(d, func(d *editor) { vimDeleteBack(d, word) })
		return true
	}
	return false
}

// vimDeleteBack deletes the word before the caret, as ⌃W does in insert
// mode, or the line before it back to its indent, as ⌃U does; at the
// start of a line, the line break.
func vimDeleteBack(d *editor, word bool) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[0], d.sel[1], snap.Len())
	if d.sel[0] != d.sel[1] || caret == 0 {
		return
	}
	t := newVText(snap)
	ls, _ := snap.LineRange(snap.LineAt(caret))
	from := caret - 1
	switch {
	case caret == ls:
	case word:
		from = caret
		for from > ls && vimSpace(t.at(from-1)) {
			from--
		}
		if from > ls {
			c := vimClass(t.at(from-1), false)
			for from > ls && vimClass(t.at(from-1), false) == c {
				from--
			}
		}
	default:
		from = firstNonBlank(snap, snap.LineAt(caret))
		if from >= caret {
			from = ls
		}
	}
	d.edit([2]int{from, from}, textEdit{from, caret, ""})
}

// vimTake takes a key of normal or visual mode, as vim names it.
func (a *app) vimTake(d *editor, r rune) {
	v := &d.vim
	if r == 'q' && a.vimRec != 0 && len(v.keys) == 0 {
		a.vimStopRecording()
		return
	}
	a.vimRecord(r)
	if r == '\x1b' && v.mode == vimNormal && len(v.keys) == 0 && a.vimSearch.show {
		// Esc with nothing typed stops showing the matches of the search.
		a.vimSearch.show = false
		a.invalidate()
		return
	}
	v.keys = append(v.keys, r)
	cmd, st := parseVim(v.keys, v.mode)
	switch st {
	case vimMore:
		return
	case vimBad:
		v.keys = v.keys[:0]
		a.vimFailed = true
		return
	}
	v.keys = v.keys[:0]
	switch cmd.key {
	case "q":
		a.vimRec, a.vimRecKeys = cmd.arg, nil
		a.invalidate()
		return
	case "@":
		a.vimPlay(d, cmd)
		return
	case "/", "?":
		a.vimOpenLine(d, rune(cmd.key[0]), cmd, "")
		return
	case ":":
		text := ""
		if v.mode.visual() {
			// Over the lines selected, as vim writes them.
			v.lastVisual = vimVisualArea{v.anchor, v.cursor, v.mode}
			v.mode = vimNormal
			vimPut(d, vimClamp(d.buf.Snapshot(), v.cursor))
			text = "'<,'>"
		} else if cmd.count > 0 {
			text = ".,.+" + strconv.Itoa(cmd.count-1)
		}
		a.vimOpenLine(d, ':', vimCmd{}, text)
		return
	}
	a.vimRunCmd(d, cmd, v.mode)
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
		// Past the last rune of a line too, after $.
		return min(max(d.vim.cursor, 0), snap.Len())
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
	case "n", "N", "*", "#":
		return a.vimSearchMotion(d, snap, cur, key, cnt)
	case "'", "`":
		at, ok := vimMarkAt(d, snap, arg)
		if !ok {
			a.vimSay(locales.VimMarkNotSet(string(arg)), true)
			return vimTarget{}, false
		}
		if key == "'" {
			return vimTarget{to: firstNonBlank(snap, snap.LineAt(at)), linewise: true}, true
		}
		return vimTarget{to: at}, true
	case "[(", "[{", "])", "]}":
		return vimUnmatched(t, cur, key, cnt)
	case "H", "M", "L":
		top, bottom := a.vimLinesInView(d, snap)
		tl := top + cnt - 1
		switch key {
		case "M":
			tl = (top + bottom) / 2
		case "L":
			tl = bottom - (cnt - 1)
		}
		tl = min(max(tl, top), bottom, last)
		return vimTarget{to: firstNonBlank(snap, max(tl, 0)), linewise: true}, true
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
		return vimObject(t, cur, key, cnt)
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

// vimObject finds a text object around cur: its runes from up to to,
// which are whole lines for a paragraph, and for a block over lines.
func vimObject(t *vtext, cur int, key string, cnt int) (vimTarget, bool) {
	cnt = max(cnt, 1)
	if key[1] == 'p' {
		return vimParagraph(t.snap, cur, key[0] == 'a', cnt)
	}
	from, to, ok := vimObjectRunes(t, cur, key, cnt)
	if !ok {
		return vimTarget{}, false
	}
	tg := vimTarget{from: from, to: to, object: true}
	if _, bracket := vimBrackets[key[1]]; bracket && key[0] == 'i' && to > from &&
		t.at(from-1) == '\n' && t.at(to-1) == '\n' {
		tg.linewise = true
	}
	return tg, true
}

// vimParagraph finds the lines of ip and ap: a run of lines with text, or
// of blank ones, and for ap the blank lines after it, or else before it.
func vimParagraph(snap ui.TextSnapshot, cur int, around bool, cnt int) (vimTarget, bool) {
	last := vimLast(snap)
	blank := func(l int) bool {
		s, e := snap.LineRange(l)
		return strings.TrimSpace(snap.Slice(s, e)) == ""
	}
	line := snap.LineAt(cur)
	l1 := line
	for l1 > 0 && blank(l1-1) == blank(line) {
		l1--
	}
	l2 := line - 1
	for k := range cnt {
		if l2 >= last {
			if k == 0 {
				break
			}
			return vimTarget{}, false
		}
		// A run, and with ap the run after it, of the other kind.
		runs := 1
		if around {
			runs = 2
		}
		for range runs {
			if l2 >= last {
				break
			}
			kind := blank(l2 + 1)
			for l2 < last && blank(l2+1) == kind {
				l2++
			}
		}
	}
	if around && !blank(line) && blank(l2) == blank(line) {
		// No blank lines after the paragraph: those before it go.
		for l1 > 0 && blank(l1-1) {
			l1--
		}
	}
	from, _ := snap.LineRange(l1)
	_, to := snap.LineRange(l2)
	return vimTarget{from: from, to: min(to+1, snap.Len()), object: true, linewise: true}, true
}

// vimObjectRunes finds the runes of a text object around cur: from up to
// to.
func vimObjectRunes(t *vtext, cur int, key string, cnt int) (from, to int, ok bool) {
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
				// The space after the quotes, or else that before them,
				// unless it is the line's indent.
				s, e := q1, q2+1
				for e < le && vimSpace(t.at(e)) {
					e++
				}
				if e == q2+1 {
					for s > ls && vimSpace(t.at(s-1)) {
						s--
					}
					if s == ls {
						s = q1
					}
				}
				return s, e, true
			}
		}
		return 0, 0, false
	}
	if key[1] == 't' {
		return vimTag(snap, cur, inner, cnt)
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
	if o < 0 && cnt == 1 {
		// Outside any: the next along the line, as vim does.
		for j := cur + 1; j < le; j++ {
			if t.at(j) == open {
				o = j
				break
			}
		}
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

// vimYank puts the text of a range in the register of the command, which
// del says deletes it.
func (a *app) vimYank(d *editor, snap ui.TextSnapshot, r vimRange, del bool) {
	var text string
	if r.linewise {
		s, _ := snap.LineRange(r.l1)
		_, e := snap.LineRange(r.l2)
		text = snap.Slice(s, e) + "\n"
	} else {
		text = snap.Slice(r.start, r.end)
	}
	a.vimStore(d.vim.reg, text, r.linewise, del)
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
			a.vimFailed = true
			return
		case tg.object && tg.linewise:
			r = vimRange{linewise: true, l1: snap.LineAt(tg.from), l2: snap.LineAt(max(tg.to-1, tg.from))}
			cur = tg.from // where y leaves the cursor
		case tg.object:
			r = vimRange{start: tg.from, end: tg.to}
		case tg.linewise:
			l1, l2 := snap.LineAt(cur), snap.LineAt(tg.to)
			r = vimRange{linewise: true, l1: min(l1, l2), l2: max(l1, l2)}
		default:
			r = vimRange{start: min(cur, tg.to), end: max(cur, tg.to)}
			if tg.inclusive {
				r.end = min(r.end+1, t.n)
			} else if l := snap.LineAt(r.end); r.end > r.start && l > snap.LineAt(r.start) {
				if s, _ := snap.LineRange(l); s == r.end {
					// An exclusive motion to the start of a line stops at
					// the end of the line before, and takes whole lines
					// from the indent of the first.
					_, r.end = snap.LineRange(l - 1)
					if r.start <= firstNonBlank(snap, snap.LineAt(r.start)) {
						r = vimRange{linewise: true, l1: snap.LineAt(r.start), l2: l - 1}
					}
				}
			}
			if c.op == "d" {
				r = vimDeleteLines(snap, r)
			}
		}
	}
	a.vimApply(d, c.op, r, cur)
}

// vimDeleteLines is the range d deletes: whole lines when it goes over
// lines from their indent to their end, as vim does, rather than leave a
// blank line.
func vimDeleteLines(snap ui.TextSnapshot, r vimRange) vimRange {
	if r.linewise || r.end <= r.start {
		return r
	}
	l1, l2 := snap.LineAt(r.start), snap.LineAt(r.end)
	if l1 == l2 || r.start > firstNonBlank(snap, l1) {
		return r
	}
	if _, e := snap.LineRange(l2); strings.TrimSpace(snap.Slice(r.end, e)) != "" {
		return r
	}
	return vimRange{linewise: true, l1: l1, l2: l2}
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
		a.vimYank(d, snap, r, false)
		// What was yanked lights up for a moment, as it goes.
		d.vim.flash, d.vim.flashAt = [2]int{r.start, min(r.end, snap.Len())}, time.Now()
		caret := r.start
		if r.linewise {
			caret = cur
			if snap.LineAt(cur) != r.l1 {
				caret = vimAtCol(snap, r.l1, vimCol(snap, cur))
			}
		}
		vimPut(d, caret)
	case "d":
		a.vimYank(d, snap, r, true)
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
		a.vimYank(d, snap, r, true)
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
		d.vim.indented = r.linewise && at > r.start
		if d.vim.replay && d.vim.indented {
			vimDropIndent(d)
		}
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
	v.indented = false
	if v.replay {
		if v.lastText != "" {
			if edited {
				d.hist.group = groupJoin
			}
			n := utf8.RuneCountInString(v.lastText)
			d.edit([2]int{at + n, at + n}, textEdit{at, at, v.lastText})
			d.hist.group = groupJoin
			vimRepeatInsert(d, v.lastText)
			d.hist.group = groupNone
			at = d.sel[1] - 1
		}
		v.insRepeat = 0
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
	if v.indented {
		vimDropIndent(d)
	}
	snap := d.buf.Snapshot()
	typed := ""
	if grown := snap.Len() - v.insLen; grown > 0 {
		if caret := d.sel[1]; caret >= v.insAt && caret <= v.insAt+grown && v.insAt+grown <= snap.Len() {
			typed = snap.Slice(v.insAt, v.insAt+grown)
		}
	}
	if v.recording {
		v.recording, v.lastText = false, typed
	}
	if typed != "" {
		if a.vimRegs == nil {
			a.vimRegs = map[rune]vimRegister{}
		}
		a.vimRegs['.'] = vimRegister{text: typed}
	}
	if v.insRepeat > 0 {
		vimRepeatInsert(d, typed)
		snap = d.buf.Snapshot()
	}
	d.hist.group, d.hist.typing = groupNone, false
	c := min(d.sel[0], d.sel[1], snap.Len())
	v.lastInsert = c
	if s, _ := snap.LineRange(snap.LineAt(c)); c > s {
		c--
	}
	vimPut(d, vimClamp(snap, c))
}

// vimRepeatInsert types text again as many times more as the command
// that started the insert asked, after the caret, or after o and O, its
// line again below it.
func vimRepeatInsert(d *editor, text string) {
	v := &d.vim
	times := v.insRepeat
	v.insRepeat = 0
	if times <= 0 || text == "" {
		return
	}
	snap := d.buf.Snapshot()
	at := min(d.sel[1], snap.Len())
	body := strings.Repeat(text, times)
	if v.insLines {
		s, e := snap.LineRange(snap.LineAt(at))
		at, body = e, strings.Repeat("\n"+snap.Slice(s, e), times)
	}
	end := at + utf8.RuneCountInString(body)
	d.edit([2]int{end, end}, textEdit{at, at, body})
}

// vimDropIndent takes back the indent that o, O, S or cc gave a line
// when nothing was typed after it, as vim does, rather than leave a line
// of blanks.
func vimDropIndent(d *editor) {
	d.vim.indented = false
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	s, e := snap.LineRange(snap.LineAt(caret))
	if caret != e || e == s || strings.TrimSpace(snap.Slice(s, e)) != "" {
		return
	}
	if d.hist.group == groupNone {
		d.hist.group = groupJoin
		defer func() { d.hist.group = groupNone }()
	}
	d.edit([2]int{s, s}, textEdit{s, e, ""})
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
	v.reg, v.insRepeat, v.insLines = c.reg, 0, false
	switch c.key {
	case "i", "a", "I", "A", "o", "O":
		// What is typed is typed as many times, as 3ix does.
		v.insRepeat, v.insLines = cnt-1, c.key == "o" || c.key == "O"
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
	case "D":
		_, e := snap.LineRange(min(line+cnt-1, last))
		a.vimApply(d, "d", vimDeleteLines(snap, vimRange{start: cur, end: e}), cur)
	case "C":
		_, e := snap.LineRange(min(line+cnt-1, last))
		a.vimApply(d, "c", vimRange{start: cur, end: e}, cur)
	case "s":
		a.vimApply(d, "c", vimRange{start: cur, end: min(cur+cnt, le)}, cur)
	case "S":
		a.vimApply(d, "c", vimRange{linewise: true, l1: line, l2: min(line+cnt-1, last)}, cur)
	case "Y":
		a.vimApply(d, "y", vimRange{linewise: true, l1: line, l2: min(line+cnt-1, last)}, cur)
	case "p", "P", "gp", "gP":
		a.vimPaste(d, cur, c.key == "P" || c.key == "gP", len(c.key) == 2, cnt)
	case "o", "O":
		vimPut(d, cur)
		insertLine(d, c.key == "O")
		after := d.buf.Snapshot()
		s, _ := after.LineRange(after.LineAt(d.sel[1]))
		indented := d.sel[1] > s
		a.vimStartInsert(d, d.sel[1], true)
		v.indented = indented
		if v.replay && indented {
			vimDropIndent(d)
		}
	case "J":
		vimJoin(d, line, max(cnt, 2))
	case "r":
		if c.arg == '\r' {
			// The runes give way to a line break, the cursor on the line
			// after it.
			// The line after it starts at the indent of the line, as
			// with autoindent.
			// The cursor goes on the last rune of the indent, as vim's does.
			if le > ls && cur+cnt <= le {
				rest := snap.Slice(cur+cnt, le)
				indent := leading(snap.Slice(ls, cur))
				trim := utf8.RuneCountInString(rest) - utf8.RuneCountInString(strings.TrimLeft(rest, " \t"))
				at := cur + 1 + max(utf8.RuneCountInString(indent)-1, 0)
				d.edit([2]int{at, at}, textEdit{cur, cur + cnt + trim, "\n" + indent})
			}
			return
		}
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
		if rc.visual != vimNormal {
			// Over as much text as the change in visual mode was.
			v.anchor, v.cursor = cur, min(cur+rc.span, max(le-1, ls))
			if rc.visual == vimVisualLine {
				v.cursor, _ = snap.LineRange(min(line+rc.span, last))
			} else if rc.col >= 0 {
				v.cursor = vimAtCol(snap, min(line+rc.span, last), rc.col)
			}
			v.replay = true
			a.vimVisualRun(d, rc, rc.visual, vimNormal)
			v.replay = false
			return
		}
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
		if c.count > 1 {
			// As many runes, or lines, as the count.
			if c.key == "v" {
				v.cursor = min(cur+c.count-1, max(le-1, ls))
			} else {
				v.cursor, _ = snap.LineRange(min(line+c.count-1, last))
			}
		}
		a.vimShowVisual(d, next)
	case "\x04", "\x15", "\x06", "\x02", "\x05", "\x19":
		a.vimScroll(d, snap, cur, c.key, c.count)
	case "zz", "zt", "zb", "z.", "z\r", "z-":
		a.vimScrollTo(d, snap, line, c.key)
		if c.key == "z." || c.key == "z\r" || c.key == "z-" {
			vimPut(d, firstNonBlank(snap, line))
		}
	case "gv":
		if lv := v.lastVisual; lv.mode.visual() {
			v.anchor, v.cursor = lv.anchor, lv.cursor
			a.vimShowVisual(d, next)
		}
	case "gi":
		a.vimStartInsert(d, min(v.lastInsert, snap.Len()), false)
	case "gJ":
		vimJoinRaw(d, line, max(cnt, 2))
	case "\x01", "\x18":
		by := cnt
		if c.key == "\x18" {
			by = -cnt
		}
		vimIncrement(d, snap, cur, by)
	case "ZZ":
		a.vimEx(d, "x")
	case "ZQ":
		a.vimEx(d, "q")
	case "m":
		vimSetMark(d, c.arg, cur)
	case "\x0f":
		a.vimJumpBy(d, cur, -cnt)
	case "\t":
		a.vimJumpBy(d, cur, cnt)
	case "&", "g&":
		a.vimRepeatSub(d, cur, c.key == "g&")
	default:
		tg, ok := a.vimMotion(d, t, cur, c.key, c.arg, c.count, "")
		if !ok {
			a.vimFailed = true
			return
		}
		if tg.object {
			return
		}
		if vimJumps(c.key) && tg.to != cur {
			vimPushJump(d, cur)
		}
		vimPut(d, tg.to)
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

// vimScrollTo scrolls the editor so that line shows at the middle of the
// view, for zz and z., at its top, for zt and z Enter, or at its bottom,
// for zb and z-.
func (a *app) vimScrollTo(d *editor, snap ui.TextSnapshot, line int, key string) {
	lineH := a.font.lineH
	if lineH <= 0 || d.vim.viewH <= 0 {
		return
	}
	g := a.geometry(d, snap.LineCount())
	top := editorPadY + g.top(line)
	h := g.top(line+1) - g.top(line)
	if line+1 >= snap.LineCount() {
		h = lineH
	}
	// Under the lines that stick at the top, as far as they go.
	stuck := float32(len(d.sticky)) * lineH
	y := top - stuck
	switch key {
	case "zz", "z.":
		y = top - (d.vim.viewH-h)/2
	case "zb", "z-":
		y = top + h + editorPadY - d.vim.viewH
	}
	y = max(0, y)
	if d.vscroll.MaxY > 0 {
		y = min(y, d.vscroll.MaxY)
	}
	d.vscroll.Y = y
	a.invalidate()
}

// vimLinesInView are the first and the last lines whole in view, which H
// and L go to.
func (a *app) vimLinesInView(d *editor, snap ui.TextSnapshot) (top, bottom int) {
	g := a.geometry(d, snap.LineCount())
	if a.font.lineH <= 0 || d.vim.viewH <= 0 {
		return 0, vimLast(snap)
	}
	top, bottom = a.linesInView(g, d.vscroll.Y, d.vim.viewH)
	stuck := float32(len(d.sticky)) * a.font.lineH
	for top < bottom && editorPadY+g.top(top) < d.vscroll.Y+stuck {
		top++ // under the lines that stick, or cut at the top
	}
	for bottom > top && editorPadY+g.top(bottom+1) > d.vscroll.Y+d.vim.viewH {
		bottom-- // cut at the bottom
	}
	return top, min(bottom, vimLast(snap))
}

// vimIncrement adds by to the number under the cursor, or the first
// after it on its line, as ⌃A and ⌃X do: decimal, with its minus, or hex
// after 0x. The cursor goes to its last digit.
func vimIncrement(d *editor, snap ui.TextSnapshot, cur, by int) {
	ls, le := snap.LineRange(snap.LineAt(cur))
	runes := []rune(snap.Slice(ls, le))
	digit := func(i int) bool { return i >= 0 && i < len(runes) && unicode.IsDigit(runes[i]) }
	isHex := func(i int) bool {
		return digit(i) || i >= 0 && i < len(runes) && strings.ContainsRune("abcdefABCDEF", runes[i])
	}
	s := cur - ls
	for digit(s) && digit(s-1) {
		s--
	}
	for s < len(runes) && !digit(s) {
		s++
	}
	if s >= len(runes) {
		return
	}
	if s >= 2 && (runes[s-1] == 'x' || runes[s-1] == 'X') && runes[s-2] == '0' {
		s -= 2 // in the digits of a hex number, after its 0x
	}
	e := s
	hex := runes[s] == '0' && s+2 < len(runes) && (runes[s+1] == 'x' || runes[s+1] == 'X') && isHex(s+2)
	if hex {
		e = s + 2
		for isHex(e) {
			e++
		}
	} else {
		for digit(e) {
			e++
		}
		if s > 0 && runes[s-1] == '-' {
			s--
		}
	}
	text := string(runes[s:e])
	var out string
	if hex {
		n, err := strconv.ParseUint(text[2:], 16, 64)
		if err != nil {
			return
		}
		digits := strconv.FormatUint(n+uint64(int64(by)), 16)
		if strings.ToUpper(text[2:]) == text[2:] && strings.ContainsAny(text[2:], "ABCDEF") {
			digits = strings.ToUpper(digits)
		}
		for len(digits) < len(text)-2 {
			digits = "0" + digits // as wide as it was
		}
		out = text[:2] + digits
	} else {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return
		}
		out = strconv.FormatInt(n+int64(by), 10)
	}
	at := ls + s + utf8.RuneCountInString(out) - 1
	d.edit([2]int{at, at}, textEdit{ls + s, ls + e, out})
}

// vimPaste pastes the register of the command cnt times after the
// cursor, or before: lines below its line or above. With past, as gp and
// gP, the cursor goes just past what was put.
func (a *app) vimPaste(d *editor, cur int, before, past bool, cnt int) {
	text, linewise := a.vimRead(d.vim.reg)
	if text == "" {
		return
	}
	snap := d.buf.Snapshot()
	line := snap.LineAt(cur)
	ls, le := snap.LineRange(line)
	if linewise {
		body := strings.TrimSuffix(strings.Repeat(text, cnt), "\n")
		first := line + 1
		if before {
			d.edit(d.sel, textEdit{ls, ls, body + "\n"})
			first = line
		} else {
			d.edit(d.sel, textEdit{le, le, "\n" + body})
		}
		after := d.buf.Snapshot()
		if past {
			// The line after those put.
			l := min(first+strings.Count(body, "\n")+1, after.LineCount()-1)
			s, _ := after.LineRange(l)
			vimPut(d, s)
			return
		}
		vimPut(d, firstNonBlank(after, first))
		return
	}
	body := strings.Repeat(text, cnt)
	at := cur
	if !before && le > ls {
		at = cur + 1
	}
	// The cursor on the last rune put, or on the first when they go over
	// lines; past it, for gp and gP.
	n := utf8.RuneCountInString(body)
	end := at + n - 1
	switch {
	case past:
		end = at + n
	case strings.Contains(body, "\n"):
		end = at
	}
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

// vimJoinRaw joins count lines from line as they are, as gJ does: no
// space between them, and their indent kept.
func vimJoinRaw(d *editor, line, count int) {
	snap := d.buf.Snapshot()
	l2 := min(line+count-1, vimLast(snap))
	if l2 <= line {
		return
	}
	var edits []textEdit
	at := 0
	for l := line; l < l2; l++ {
		_, e := snap.LineRange(l)
		edits = append(edits, textEdit{e, e + 1, ""})
		at = e - (l - line)
	}
	d.edit([2]int{at, at}, edits...)
	vimPut(d, at)
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
	if !v.replay && vimVisualChanges(c.key) {
		// For . to make again: the change, and how much it took.
		rc := vimCmd{key: c.key, arg: c.arg, visual: mode, col: -1}
		if l1, l2 := snap.LineAt(lo), snap.LineAt(hi); mode == vimVisualLine || l1 != l2 {
			rc.span = l2 - l1
			if mode != vimVisualLine {
				rc.col = vimCol(snap, hi)
			}
		} else {
			rc.span = hi - lo
		}
		v.last, v.hasLast, v.lastText = rc, true, ""
		v.recording = next == vimInsert
	}
	v.reg, v.insRepeat = c.reg, 0
	r := vimRange{start: lo, end: min(hi+1, n)}
	switch {
	case mode == vimVisualLine, c.key == "X", c.key == "D", c.key == "Y", c.key == "C", c.key == "S", c.key == "R":
		r = vimRange{linewise: true, l1: snap.LineAt(lo), l2: snap.LineAt(hi)}
	}
	switch c.key {
	case "zz", "zt", "zb", "z.", "z\r", "z-":
		a.vimScrollTo(d, snap, snap.LineAt(min(v.cursor, n)), c.key)
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
		text, linewise := a.vimRead(v.reg)
		if text == "" {
			a.vimApply(d, "d", r, lo)
			return
		}
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
		if c.key == "p" {
			// p yanks what it pastes over, as vim does; P keeps the
			// register.
			v.reg = 0
			a.vimYank(d, snap, r, true)
		}
		d.edit([2]int{s, s}, textEdit{s, e, body})
		after := d.buf.Snapshot()
		if r.linewise || linewise {
			l := snap.LineAt(s)
			if !r.linewise {
				l++ // the line put after the break
			}
			vimPut(d, firstNonBlank(after, min(l, vimLast(after))))
		} else if strings.Contains(body, "\n") {
			vimPut(d, s)
		} else {
			vimPut(d, s+utf8.RuneCountInString(body)-1)
		}
	default:
		if len(c.key) == 2 && (c.key[0] == 'i' || c.key[0] == 'a') {
			if tg, ok := vimObject(t, v.cursor, c.key, c.count); ok && tg.to > tg.from {
				v.anchor, v.cursor = tg.from, tg.to-1
			}
		} else if tg, ok := a.vimMotion(d, t, v.cursor, c.key, c.arg, c.count, ""); ok {
			if vimJumps(c.key) && tg.to != v.cursor {
				vimPushJump(d, v.cursor)
			}
			v.cursor = tg.to
			if c.key == "$" || (c.key == "j" || c.key == "k") && v.want >= 1<<30 {
				// The end of the line, its newline and all.
				_, v.cursor = snap.LineRange(snap.LineAt(tg.to))
			}
		} else {
			a.vimFailed = true
		}
		a.vimShowVisual(d, next)
	}
}

// vimVisualChanges reports whether a key of visual mode changes the
// text, as . repeats.
func vimVisualChanges(key string) bool {
	switch key {
	case "y", "Y", "o", "v", "V", "\x1b":
		return false
	}
	return vimVisualActions[key] || key == "r"
}

// toggleVim turns vim mode on, the editors in normal mode, or off.
func (a *app) toggleVim() {
	a.setPref("vim", func(p *settings.Prefs) { p.Vim = !p.Vim })
	a.invalidate()
}
