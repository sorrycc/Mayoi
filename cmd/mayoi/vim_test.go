package main

import (
	"image"
	"image/color"
	"mayoi/locales"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// vimHarness opens a file of text in vim mode, its cursor at the start.
func vimHarness(t *testing.T, name, text string) (*harness, *editor) {
	t.Helper()
	hs, d := editHarness(t, name, text)
	hs.a.settings.Vim = true
	hs.frames()
	return hs, d
}

func (hs *harness) frames() {
	for range 4 {
		step(hs.tt)
	}
}

// vim presses keys as vim names them: a character, or <Esc>, <CR>, <BS>,
// <Tab> and <C-x>.
func (hs *harness) vim(keys string) {
	hs.t.Helper()
	for len(keys) > 0 {
		var mods ui.Modifiers
		var key ui.Key
		if end := strings.IndexByte(keys, '>'); keys[0] == '<' && end > 2 && vimKeyName(keys[1:end]) {
			switch name := keys[1:end]; {
			case name == "Esc":
				key = ui.KeyEscape
			case name == "CR":
				key = ui.KeyEnter
			case name == "BS":
				key = ui.KeyBackspace
			case name == "Tab":
				key = ui.KeyTab
			case strings.HasPrefix(name, "C-") && len(name) == 3:
				mods, key = ui.Ctrl, ui.KeyA+ui.Key(name[2]-'a')
			default:
				hs.t.Fatalf("unknown key <%s>", name)
			}
			keys = keys[end+1:]
		} else {
			mods, key = vimKeyOf(hs.t, rune(keys[0]))
			keys = keys[1:]
		}
		hs.tt.Key(mods, key)
		hs.frames()
	}
}

// vimKeyName reports whether name is that of a key, as in <Esc>.
func vimKeyName(name string) bool {
	switch name {
	case "Esc", "CR", "BS", "Tab":
		return true
	}
	return strings.HasPrefix(name, "C-") && len(name) == 3
}

// vimKeyOf is the key that types r, as vimRune reads it.
func vimKeyOf(t testing.TB, r rune) (ui.Modifiers, ui.Key) {
	switch {
	case r >= 'a' && r <= 'z':
		return 0, ui.KeyA + ui.Key(r-'a')
	case r >= 'A' && r <= 'Z':
		return ui.Shift, ui.KeyA + ui.Key(r-'A')
	case r >= '0' && r <= '9':
		return 0, ui.Key0 + ui.Key(r-'0')
	case r == ' ':
		return 0, ui.KeySpace
	}
	if i := strings.IndexRune(")!@#$%^&*(", r); i >= 0 {
		return ui.Shift, ui.Key0 + ui.Key(i)
	}
	for k, p := range vimPunct {
		if rune(p[0]) == r {
			return 0, k
		}
		if rune(p[1]) == r {
			return ui.Shift, k
		}
	}
	t.Fatalf("no key types %q", r)
	return 0, 0
}

// insert types text in insert mode.
func (hs *harness) insert(text string) {
	hs.tt.Type(text)
	hs.frames()
}

func expectCursor(t *testing.T, d *editor, at int) {
	t.Helper()
	if d.sel != [2]int{at, at} {
		t.Fatalf("cursor %v, want %d", d.sel, at)
	}
}

func TestVimNormalModeTypesNothing(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "hello\n")
	if !hs.tt.HasText("NORMAL") {
		t.Fatal("the status bar does not show normal mode")
	}
	// A key that types text, as a keyboard sends it: j moves down a line,
	// but not onto the empty one after the last.
	hs.tt.TypeKey(0, ui.KeyL, "l")
	hs.tt.TypeKey(0, ui.KeyJ, "j")
	hs.frames()
	expectText(t, d, "hello\n")
	expectCursor(t, d, 1)
	hs.vim("0i")
	if !hs.tt.HasText("INSERT") {
		t.Fatal("i does not show insert mode")
	}
	hs.insert("oh ")
	expectText(t, d, "oh hello\n")
	hs.vim("<Esc>")
	if d.vim.mode != vimNormal {
		t.Fatalf("Esc left mode %v", d.vim.mode)
	}
	expectCursor(t, d, 2)
}

func TestVimMotions(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two.three  four\n  five (six [x]) end\n\nlast line\n")
	steps := []struct {
		keys string
		at   int
	}{
		{"w", 4}, {"w", 7}, {"w", 8}, {"W", 15}, {"b", 8}, {"B", 4}, {"e", 6}, {"E", 12}, {"$", 18}, {"0", 0},
		{"j", 20}, {"^", 22}, {"fx", 30}, {"Fs", 28}, {"tx", 29}, {";", 32}, {",", 31}, {"0", 20}, {"f(", 27},
		{"%", 35}, {"%", 27}, {"}", 41}, {"{", 0}, {"G", 42}, {"gg", 0}, {"2G", 22}, {"k", 2}, {"3l", 5}, {"h", 4},
		{"ge", 2}, {"2w", 7},
	}
	for _, s := range steps {
		hs.vim(s.keys)
		if d.sel != [2]int{s.at, s.at} {
			t.Fatalf("after %q: cursor %v, want %d", s.keys, d.sel, s.at)
		}
	}
	// j and k keep the column across a shorter line.
	hs.place(d, 18, 18)
	hs.vim("jjj")
	expectCursor(t, d, 50)
	hs.vim("kk")
	expectCursor(t, d, 38)
}

func TestVimOperators(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "alpha beta gamma\nsecond line\nthird\n")
	hs.vim("dw")
	expectText(t, d, "beta gamma\nsecond line\nthird\n")
	// cw types over the word, and Esc leaves the cursor on its last rune.
	hs.vim("cw")
	hs.insert("BETA")
	hs.vim("<Esc>")
	expectText(t, d, "BETA gamma\nsecond line\nthird\n")
	expectCursor(t, d, 3)
	hs.vim("u")
	expectText(t, d, "beta gamma\nsecond line\nthird\n")
	hs.vim("0")
	hs.vim("x")
	expectText(t, d, "eta gamma\nsecond line\nthird\n")
	hs.vim("3x")
	expectText(t, d, " gamma\nsecond line\nthird\n")
	hs.vim("dd")
	expectText(t, d, "second line\nthird\n")
	hs.vim("p")
	expectText(t, d, "second line\n gamma\nthird\n")
	expectCursor(t, d, 13)
	hs.vim("ggd$")
	expectText(t, d, "\n gamma\nthird\n")
	hs.vim("jJ")
	expectText(t, d, "\n gamma third\n")
	hs.vim("u")
	expectText(t, d, "\n gamma\nthird\n")
	hs.vim("<C-r>")
	expectText(t, d, "\n gamma third\n")
	if hs.a.recents.open {
		t.Fatal("⌃R opened Open Recent rather than redoing")
	}
	hs.vim("gg2dd")
	expectText(t, d, "")
}

func TestVimChangeUndoesAtOnce(t *testing.T) {
	hs, d := vimHarness(t, "a.go", "f(alpha, beta)\n")
	hs.vim("fbciw")
	hs.insert("g")
	hs.insert("amma")
	hs.vim("<Esc>")
	expectText(t, d, "f(alpha, gamma)\n")
	expectCursor(t, d, 13)
	hs.vim("u")
	expectText(t, d, "f(alpha, beta)\n")
	hs.place(d, 0, 0)
	hs.vim("w")
	hs.vim("ci(")
	expectText(t, d, "f()\n")
	hs.insert("a, b")
	hs.vim("<Esc>u")
	expectText(t, d, "f(alpha, beta)\n")
}

func TestVimDotRepeats(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two three four\n")
	hs.vim("ciw")
	hs.insert("1")
	hs.vim("<Esc>w.w.")
	expectText(t, d, "1 1 1 four\n")
	hs.vim("0x..")
	expectText(t, d, " 1 four\n")
	hs.vim("u")
	expectText(t, d, "1 1 four\n")
	hs.vim("A")
	hs.insert("!")
	hs.vim("<Esc>0.")
	expectText(t, d, "1 1 four!!\n")
}

func TestVimTextObjects(t *testing.T) {
	hs, d := vimHarness(t, "a.go", "x := f(\"a b\", [1, 2])\nif ok {\n\tbody()\n}\n")
	hs.vim("f\"di\"")
	expectText(t, d, "x := f(\"\", [1, 2])\nif ok {\n\tbody()\n}\n")
	hs.vim("f1da[")
	expectText(t, d, "x := f(\"\", )\nif ok {\n\tbody()\n}\n")
	hs.vim("jjdi{")
	expectText(t, d, "x := f(\"\", )\nif ok {\n}\n")
	hs.vim("ggdaw")
	expectText(t, d, ":= f(\"\", )\nif ok {\n}\n")
}

func TestVimVisual(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two three\nfour\nfive\n")
	hs.vim("wv")
	if !hs.tt.HasText("VISUAL") {
		t.Fatal("v does not show visual mode")
	}
	hs.vim("e")
	if d.sel != [2]int{4, 7} {
		t.Fatalf("visual selection %v, want [4 7]", d.sel)
	}
	hs.vim("y")
	if got := hs.tt.Clipboard(); got != "two" {
		t.Fatalf("visual y copied %q", got)
	}
	expectCursor(t, d, 4)
	hs.vim("veU")
	expectText(t, d, "one TWO three\nfour\nfive\n")
	hs.vim("jVjd")
	expectText(t, d, "one TWO three\n")
	if got := hs.tt.Clipboard(); got != "four\nfive\n" {
		t.Fatalf("V d cut %q", got)
	}
	hs.vim("P")
	expectText(t, d, "four\nfive\none TWO three\n")
	// o goes to the other end; c changes the selection.
	hs.vim("vlohc")
	hs.insert("X")
	hs.vim("<Esc>")
	expectText(t, d, "Xur\nfive\none TWO three\n")
	hs.vim("Vj>")
	expectText(t, d, "    Xur\n    five\none TWO three\n")
}

func TestVimClipboard(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "ab\ncd\n")
	hs.vim("yy")
	if got := hs.tt.Clipboard(); got != "ab\n" {
		t.Fatalf("yy copied %q", got)
	}
	// Text copied elsewhere pastes after the cursor, not as a line.
	hs.tt.SetClipboard("XY")
	hs.vim("p")
	expectText(t, d, "aXYb\ncd\n")
	expectCursor(t, d, 2)
	hs.tt.SetClipboard("line\n")
	hs.vim("jP")
	expectText(t, d, "aXYb\nline\ncd\n")
}

func TestVimIndentAndComment(t *testing.T) {
	hs, d := vimHarness(t, "a.go", "a()\nb()\nc()\n")
	hs.vim("2>>")
	expectText(t, d, "\ta()\n\tb()\nc()\n")
	hs.vim("<j")
	expectText(t, d, "a()\nb()\nc()\n")
	hs.vim("Ggcc")
	expectText(t, d, "a()\nb()\n// c()\n")
	hs.vim("ggo")
	hs.insert("x")
	hs.vim("<Esc>")
	expectText(t, d, "a()\nx\nb()\n// c()\n")
	hs.vim("u")
	expectText(t, d, "a()\nb()\n// c()\n")
}

func TestVimScrolls(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", strings.Repeat("line\n", 300))
	hs.vim("<C-d>")
	snap := d.buf.Snapshot()
	if l := snap.LineAt(d.sel[1]); l < 5 || d.vscroll.Y <= 0 {
		t.Fatalf("⌃D went to line %d, scrolled to %v", l, d.vscroll.Y)
	}
	hs.vim("<C-u>")
	if l := snap.LineAt(d.sel[1]); l != 0 {
		t.Fatalf("⌃U went to line %d", l)
	}
	hs.vim("G")
	if l := snap.LineAt(d.sel[1]); l != 299 {
		t.Fatalf("G went to line %d, want 299", l)
	}
}

func TestVimOff(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "hello\n")
	hs.a.toggleVim()
	hs.frames()
	hs.place(d, 0, 0)
	hs.tt.Type("j")
	hs.frames()
	expectText(t, d, "jhello\n")
	if hs.tt.HasText("NORMAL") {
		t.Fatal("the status bar shows a mode with vim mode off")
	}
}

func TestVimSearchLine(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two\nthree two\nfour\n")
	hs.vim("/")
	if hs.a.vimLine.d != d || !hs.tt.HasText("/") {
		t.Fatal("/ does not open the command line")
	}
	// The cursor goes to the first match as it is typed, and back as the
	// line is closed with Esc.
	hs.tt.Type("thr")
	hs.frames()
	expectCursor(t, d, 8)
	if !hs.tt.HasText(locales.VimMatchCount(1, 1)) {
		t.Fatal("the command line does not count the matches")
	}
	if len(hs.a.vimMatches(d, d.buf.Snapshot(), 0, 2)) != 1 {
		t.Fatal("the matches of the search typed are not highlighted")
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.frames()
	if hs.a.vimLine.d != nil {
		t.Fatal("Esc did not close the command line")
	}
	expectCursor(t, d, 0)
	if len(hs.a.vimMatches(d, d.buf.Snapshot(), 0, 2)) != 0 {
		t.Fatal("a search given up still shows its matches")
	}
	// Backspace with nothing typed closes it too.
	hs.vim("/")
	hs.tt.Key(0, ui.KeyBackspace)
	hs.frames()
	if hs.a.vimLine.d != nil {
		t.Fatal("Backspace on an empty line did not close it")
	}
	// Return goes; the matches show until Esc in normal mode.
	hs.vim("/")
	hs.tt.Type("two")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	expectCursor(t, d, 4)
	if n := len(hs.a.vimMatches(d, d.buf.Snapshot(), 0, 2)); n != 2 {
		t.Fatalf("%d matches highlighted after the search, want 2", n)
	}
	hs.vim("n")
	expectCursor(t, d, 14)
	hs.vim("n")
	expectCursor(t, d, 4)
	if !hs.tt.HasText(locales.VimWrappedTop()) {
		t.Fatal("a search that went round does not say so")
	}
	hs.vim("<Esc>")
	if len(hs.a.vimMatches(d, d.buf.Snapshot(), 0, 2)) != 0 {
		t.Fatal("Esc did not hide the matches")
	}
	// Up brings back the last search.
	hs.vim("gg/")
	hs.tt.Key(0, ui.KeyUp)
	hs.frames()
	if hs.a.vimLine.text != "two" {
		t.Fatalf("Up brought %q back, want two", hs.a.vimLine.text)
	}
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	expectCursor(t, d, 4)
	// Nothing found: the bar shakes and stays.
	hs.vim("/")
	hs.tt.Type("[")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	if hs.a.vimLine.d == nil || hs.a.vimLine.shook.IsZero() {
		t.Fatal("an invalid pattern did not shake the command line")
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.frames()
	hs.vim("/")
	hs.tt.Type("nothing")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	if !hs.tt.HasText(locales.VimPatternNotFound("nothing")) {
		t.Fatal("a search that finds nothing does not say so")
	}
	expectCursor(t, d, 4)
}

func TestVimCommands(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "b\na\nc\n")
	hs.vim(":")
	hs.tt.Type("sort")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	expectText(t, d, "a\nb\nc\n")
	hs.vim(":")
	hs.tt.Type("nope")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	if !hs.tt.HasText(locales.VimNotCommand("nope")) {
		t.Fatal("an unknown command does not say so")
	}
	hs.vim(":")
	hs.tt.Type("w")
	hs.tt.Key(0, ui.KeyEnter)
	hs.frames()
	if data, _ := os.ReadFile(hs.path("a.txt")); string(data) != "a\nb\nc\n" {
		t.Fatalf(":w saved %q", data)
	}
	hs.vim(":")
	hs.tt.Type("q")
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if hs.a.activeEditor() != nil {
		t.Fatal(":q did not close the tab")
	}
}

func TestVimScrollsToCursor(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", strings.Repeat("line\n", 300))
	hs.vim("150G")
	hs.vim("zt")
	lineH := hs.a.font.lineH
	if top := (d.vscroll.Y - editorPadY) / lineH; top < 148 || top > 150 {
		t.Fatalf("zt scrolled to line %v, want 149", top)
	}
	hs.vim("H")
	if l := d.buf.Snapshot().LineAt(d.sel[1]); l < 149 || l > 151 {
		t.Fatalf("H went to line %d", l)
	}
	hs.vim("zb")
	hs.vim("L")
	if l := d.buf.Snapshot().LineAt(d.sel[1]); l != 149 {
		t.Fatalf("L after zb went to line %d, want 149", l)
	}
	hs.vim("zz")
	mid := d.vscroll.Y + d.vim.viewH/2
	if y := editorPadY + 149.5*lineH; y < mid-lineH || y > mid+lineH {
		t.Fatalf("zz left the cursor at %v, the middle at %v", y, mid)
	}
}

func TestVimCursorAndFlash(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two\n")
	hs.vim("d")
	if !d.vim.operatorPending() {
		t.Fatal("d does not wait for a motion")
	}
	hs.vim("<Esc>")
	if d.vim.operatorPending() {
		t.Fatal("Esc left the operator waiting")
	}
	hs.vim("yiw")
	if d.vim.flash != [2]int{0, 3} || d.vim.flashAt.IsZero() {
		t.Fatalf("yiw lit %v, want [0 3]", d.vim.flash)
	}
	hs.vim("wdw")
	if d.vim.flash != [2]int{0, 3} {
		t.Fatal("a delete lit the text it took")
	}
}

// From mode to mode, the badge shows the name of the mode it is in, not
// the one it left fading out over it: the white of INSERT broke up the
// strokes of NORMAL as it went.
func TestVimBadgeShowsOneMode(t *testing.T) {
	skipOnWindows(t, "a pixel of the badge drawn by Windows' text is lighter than its ground, since the test came in")
	hs, _ := vimHarness(t, "a.txt", "one two\n")
	hs.vim("i")
	hs.settle()
	hs.tt.Key(0, ui.KeyEscape)
	// Past the colors' crossing over, 140 ms.
	for range 9 {
		step(hs.tt)
	}
	box, ok := hs.tt.Find("NORMAL")
	if !ok {
		t.Fatal("no NORMAL in the status bar")
	}
	img := hs.tt.Image()
	r := image.Rect(int(box.X), int(box.Y), int(box.X+box.W), int(box.Y+box.H)).Intersect(img.Bounds())
	// The badge's ground, left of its name; nothing on it is lighter.
	ground := luma(img.RGBAAt(r.Min.X+3, (r.Min.Y+r.Max.Y)/2))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X + 3; x < r.Max.X-3; x++ {
			if c := img.RGBAAt(x, y); luma(c) > ground+3 {
				t.Fatalf("the badge shows %v at (%d, %d), lighter than its ground", c, x, y)
			}
		}
	}
}

func luma(c color.RGBA) float32 {
	return 0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B)
}

// On a layout of another script, the keys are where a US keyboard has
// them.
func TestVimOtherLayout(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one two\n")
	hs.tt.KeyAt(0, ui.KeyW)
	hs.frames()
	expectCursor(t, d, 4)
}
