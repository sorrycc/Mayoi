package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"mayoi/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// editHarness opens a file of text, its caret at the start.
func editHarness(t *testing.T, name, text string) (*harness, *editor) {
	t.Helper()
	hs := newHarness(t, false)
	os.WriteFile(hs.path(name), []byte(text), 0o644)
	hs.a.openFile(hs.path(name), true)
	hs.settle()
	return hs, hs.a.activeEditor()
}

// place selects anchor up to caret, as a click and a drag would.
func (hs *harness) place(d *editor, anchor, caret int) {
	d.selectRange(anchor, caret, false)
	hs.settle()
}

func (hs *harness) key(mods ui.Modifiers, key ui.Key) {
	hs.tt.Key(mods, key)
	hs.settle()
}

// command runs an Edit menu command, as Copy, which macOS sends in place
// of its key, ⌘C, to the element with the keyboard.
func (hs *harness) command(name string) {
	hs.tt.Command(name)
	hs.settle()
}

func expectText(t *testing.T, d *editor, want string) {
	t.Helper()
	if got := d.buf.String(); got != want {
		t.Fatalf("text:\n%q\nwant:\n%q", got, want)
	}
}

func expectSel(t *testing.T, d *editor, anchor, caret int) {
	t.Helper()
	if d.sel != [2]int{anchor, caret} {
		t.Fatalf("selection %v, want [%d %d]", d.sel, anchor, caret)
	}
}

func cutFromMenu(t *testing.T, hs *harness) {
	t.Helper()
	a := hs.a
	m := a.menu(false, func(id string, _ *mygo.Window) { a.run(id) })
	for _, menu := range m.Items() {
		if menu.Label != locales.MenuEdit() {
			continue
		}
		for _, item := range menu.Submenu {
			if item.Role == mygo.RoleCut {
				hs.tt.Command("cut")
				return
			}
			if item.Label == locales.CmdEditCut() && item.Role == "" && item.Click != nil {
				item.Click(item, nil)
				return
			}
		}
	}
	t.Fatal("no editor Cut menu command")
}

func TestCutCurrentLine(t *testing.T) {
	for _, menu := range []bool{false, true} {
		for _, tc := range []struct {
			name, text, want, clipboard string
			caret                       int
		}{
			{"first", "one\ntwo\nthree", "two\nthree", "one\n", 1},
			{"middle", "one\ntwo\nthree", "one\nthree", "two\n", 5},
			{"last", "one\ntwo", "one", "two\n", 5},
			{"single", "one", "", "one\n", 1},
			{"blank", "one\n\ntwo", "one\ntwo", "\n", 4},
			{"trailing blank", "one\n", "one", "\n", 4},
			{"unicode", "一行\n迷い 😀\n末行", "一行\n末行", "迷い 😀\n", 5},
		} {
			t.Run(fmt.Sprintf("%s/menu=%v", tc.name, menu), func(t *testing.T) {
				hs, d := editHarness(t, "cut.txt", tc.text)
				hs.place(d, tc.caret, tc.caret)
				if menu {
					cutFromMenu(t, hs)
					hs.settle()
				} else {
					hs.key(ui.Cmd, ui.KeyX)
				}
				expectText(t, d, tc.want)
				if got := hs.tt.Clipboard(); got != tc.clipboard {
					t.Fatalf("clipboard %q, want %q", got, tc.clipboard)
				}
				hs.key(ui.Cmd, ui.KeyZ)
				expectText(t, d, tc.text)
				expectSel(t, d, tc.caret, tc.caret)
				hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
				expectText(t, d, tc.want)
			})
		}
	}
}

func TestCutKeepsSelectionAndReadOnlyBehavior(t *testing.T) {
	for _, menu := range []bool{false, true} {
		for _, tc := range []struct {
			name, text, want, clipboard string
			selection                   [2]int
			locked                      bool
		}{
			{"selection", "one\ntwo", "oe\ntwo", "n", [2]int{1, 2}, false},
			{"backward selection", "one\ntwo", "oe\ntwo", "n", [2]int{2, 1}, false},
			{"empty", "", "", "keep", [2]int{}, false},
			{"read only", "one\ntwo", "one\ntwo", "keep", [2]int{1, 1}, true},
		} {
			t.Run(fmt.Sprintf("%s/menu=%v", tc.name, menu), func(t *testing.T) {
				hs, d := editHarness(t, "cut.txt", tc.text)
				hs.place(d, tc.selection[0], tc.selection[1])
				if tc.locked {
					d.loadErr = fmt.Errorf("read-only test document")
				}
				hs.settle()
				hs.tt.SetClipboard("keep")
				if menu {
					cutFromMenu(t, hs)
					hs.settle()
				} else {
					hs.key(ui.Cmd, ui.KeyX)
				}
				expectText(t, d, tc.want)
				if got := hs.tt.Clipboard(); got != tc.clipboard {
					t.Fatalf("clipboard %q, want %q", got, tc.clipboard)
				}
			})
		}
	}
}

func TestCutMenuPreservesTextFields(t *testing.T) {
	hs, d := editHarness(t, "cut.txt", "one\ntwo")
	hs.a.openQuick("")
	hs.settle()
	hs.tt.Type("needle")
	hs.settle()
	hs.key(ui.Cmd, ui.KeyA)
	cutFromMenu(t, hs)
	hs.settle()
	if hs.a.quick.query != "" || hs.tt.Clipboard() != "needle" {
		t.Fatalf("text field cut: query %q, clipboard %q", hs.a.quick.query, hs.tt.Clipboard())
	}
	expectText(t, d, "one\ntwo")
}

func TestTabIndents(t *testing.T) {
	hs, d := editHarness(t, "a.go", "func f() {\nx()\ny()\n}\n")
	if !d.indent.tabs {
		t.Fatalf("a Go file indents with %v", d.indent)
	}
	hs.place(d, 11, 11)
	hs.key(0, ui.KeyTab)
	expectText(t, d, "func f() {\n\tx()\ny()\n}\n")
	expectSel(t, d, 12, 12)
	if !hs.tt.Focused("Editor a.go") {
		t.Fatal("Tab took the focus out of the editor")
	}
	// Over several lines, each indents, and Shift outdents them again.
	hs.place(d, 12, 18)
	hs.key(0, ui.KeyTab)
	expectText(t, d, "func f() {\n\t\tx()\n\ty()\n}\n")
	hs.key(ui.Shift, ui.KeyTab)
	hs.key(ui.Shift, ui.KeyTab)
	expectText(t, d, "func f() {\nx()\ny()\n}\n")
}

func TestTabWithSpaces(t *testing.T) {
	hs, d := editHarness(t, "a.py", "def f():\n  x = 1\n  if x:\n    y = 2\n")
	if d.indent.tabs || d.indent.size != 2 {
		t.Fatalf("detected %v, want 2 spaces", d.indent)
	}
	hs.place(d, 9, 9)
	hs.key(0, ui.KeyTab)
	expectText(t, d, "def f():\n    x = 1\n  if x:\n    y = 2\n")
	hs.key(ui.Shift, ui.KeyTab)
	expectText(t, d, "def f():\n  x = 1\n  if x:\n    y = 2\n")
}

func TestEnterKeepsIndent(t *testing.T) {
	hs, d := editHarness(t, "a.go", "func f() {}\n\tx := 1\n")
	// Between braces, the closing one goes to its own line.
	hs.place(d, 10, 10)
	hs.key(0, ui.KeyEnter)
	expectText(t, d, "func f() {\n\t\n}\n\tx := 1\n")
	expectSel(t, d, 12, 12)
	// After an indented line, the next keeps its indent.
	end := len("func f() {\n\t\n}\n\tx := 1")
	hs.place(d, end, end)
	hs.key(0, ui.KeyEnter)
	expectText(t, d, "func f() {\n\t\n}\n\tx := 1\n\t\n")
	// Undo takes the newline and its indent back at once.
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "func f() {\n\t\n}\n\tx := 1\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "func f() {}\n\tx := 1\n")
	hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
	expectText(t, d, "func f() {\n\t\n}\n\tx := 1\n")
}

func TestUndoJoinsTypingAndCleansAtSaved(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "hello\n")
	hs.place(d, 5, 5)
	for _, s := range []string{" ", "w", "o"} {
		hs.tt.Type(s)
		hs.settle()
	}
	expectText(t, d, "hello wo\n")
	if !d.dirty() {
		t.Fatal("not dirty after typing")
	}
	// A move of the caret ends the step of typing.
	hs.place(d, 0, 0)
	hs.tt.Type(">")
	hs.settle()
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "hello wo\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "hello\n")
	if d.dirty() {
		t.Error("dirty after undoing back to the text on disk")
	}
	hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
	expectText(t, d, "hello wo\n")
	if !d.dirty() {
		t.Error("clean after redoing")
	}
}

func TestLineCommands(t *testing.T) {
	hs, d := editHarness(t, "a.go", "a()\nb()\nc()\n")
	a := hs.a
	hs.place(d, 4, 4)
	hs.key(ui.Alt, ui.KeyDown)
	expectText(t, d, "a()\nc()\nb()\n")
	expectSel(t, d, 8, 8)
	hs.key(ui.Alt, ui.KeyUp)
	hs.key(ui.Alt, ui.KeyUp)
	expectText(t, d, "b()\na()\nc()\n")
	hs.key(ui.Alt|ui.Shift, ui.KeyDown)
	expectText(t, d, "b()\nb()\na()\nc()\n")
	expectSel(t, d, 4, 4)

	a.run("sel.comment")
	hs.settle()
	expectText(t, d, "b()\n// b()\na()\nc()\n")
	a.run("sel.comment")
	hs.settle()
	expectText(t, d, "b()\nb()\na()\nc()\n")

	a.run("sel.deleteLine")
	hs.settle()
	expectText(t, d, "b()\na()\nc()\n")
	expectSel(t, d, 4, 4)

	hs.key(ui.Cmd, ui.KeyEnter)
	expectText(t, d, "b()\na()\n\nc()\n")
	expectSel(t, d, 8, 8)

	a.run("edit.undo")
	hs.settle()
	expectText(t, d, "b()\na()\nc()\n")
}

func TestCommentKeepsIndent(t *testing.T) {
	hs, d := editHarness(t, "a.go", "func f() {\n\tx()\n\n\t\ty()\n}\n")
	hs.place(d, 11, 21)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, "func f() {\n\t// x()\n\n\t// \ty()\n}\n")
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, "func f() {\n\tx()\n\n\t\ty()\n}\n")
}

func TestReplaceAllUndoes(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "cat cat dog cat\n")
	a := hs.a
	a.openFind(true)
	a.find.query, a.find.with = "cat", "cow"
	a.find.stale = true
	hs.settle()
	a.replaceAll(d)
	hs.settle()
	expectText(t, d, "cow cow dog cow\n")
	a.run("edit.undo")
	hs.settle()
	expectText(t, d, "cat cat dog cat\n")
}

// ⌘F selects the query the find box has, as VS Code does, so typing
// replaces it: when the box opens again, and when it has the focus already.
func TestFindSelectsQuery(t *testing.T) {
	hs, _ := editHarness(t, "a.txt", "token words\n")
	a := hs.a
	a.find.query = "token"
	a.run("edit.find")
	hs.settle()
	hs.tt.Type("w")
	hs.settle()
	if a.find.query != "w" {
		t.Fatalf("query after typing on reopen = %q, want w", a.find.query)
	}
	hs.tt.Type("ords")
	hs.settle()
	hs.key(0, ui.KeyEnd)
	a.run("edit.find")
	hs.settle()
	hs.tt.Type("t")
	hs.settle()
	if a.find.query != "t" {
		t.Fatalf("query after typing on ⌘F in the box = %q, want t", a.find.query)
	}
}

func TestSmartHome(t *testing.T) {
	hs, d := editHarness(t, "a.go", "\t\tx := 1\n")
	hs.place(d, 6, 6)
	hs.key(0, ui.KeyHome)
	expectSel(t, d, 2, 2)
	hs.key(0, ui.KeyHome)
	expectSel(t, d, 0, 0)
	hs.key(ui.Shift, ui.KeyHome)
	expectSel(t, d, 0, 2)
}

// A selection made backwards goes on from its anchor: ⌘⇧← takes it to the
// start of the line, as in VS Code, and does not drop what was selected.
func TestHomeExtendsBackwardSelection(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "abcde\n")
	hs.place(d, 5, 2)
	expectSel(t, d, 5, 2)
	hs.key(ui.Cmd|ui.Shift, ui.KeyLeft)
	expectSel(t, d, 5, 0)
	hs.key(ui.Shift, ui.KeyRight)
	expectSel(t, d, 5, 1)
}

func TestAutoClose(t *testing.T) {
	hs, d := editHarness(t, "a.go", "x\n")
	hs.place(d, 1, 1)
	typ := func(s string) {
		for _, r := range s {
			hs.tt.Type(string(r))
			hs.settle()
		}
	}
	typ("(")
	expectText(t, d, "x()\n")
	expectSel(t, d, 2, 2)
	typ("\"a")
	expectText(t, d, "x(\"a\")\n")
	// Typing the closing ones steps over them.
	typ("\")")
	expectText(t, d, "x(\"a\")\n")
	expectSel(t, d, 6, 6)
	// One undo takes it all back, as it was one run of typing.
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "x\n")

	// Before a word, a bracket does not close.
	hs.place(d, 0, 0)
	typ("[")
	expectText(t, d, "[x\n")
	// Deleting an opening bracket deletes its pair.
	hs.place(d, 3, 3)
	typ("{")
	expectText(t, d, "[x\n{}")
	hs.key(0, ui.KeyBackspace)
	expectText(t, d, "[x\n")
	// Typed over a selection, a bracket goes around it.
	hs.place(d, 1, 2)
	typ("(")
	expectText(t, d, "[(x)\n")
	expectSel(t, d, 2, 3)
	// An apostrophe after a word stays alone.
	hs.place(d, 4, 4)
	typ("'")
	expectText(t, d, "[(x)'\n")
}

func TestReindent(t *testing.T) {
	hs, d := editHarness(t, "a.py", "def f():\n    if x:\n        y()\n")
	if d.indent.tabs || d.indent.size != 4 {
		t.Fatalf("detected %v", d.indent)
	}
	hs.a.editActive(func(d *editor) { reindent(d, indentStyle{size: 2}) })()
	hs.settle()
	expectText(t, d, "def f():\n  if x:\n    y()\n")
	hs.a.editActive(func(d *editor) { reindent(d, indentStyle{tabs: true, size: 4}) })()
	hs.settle()
	expectText(t, d, "def f():\n\tif x:\n\t\ty()\n")
}

func TestGoToBracket(t *testing.T) {
	hs, d := editHarness(t, "a.go", "f(a, (b))\n")
	hs.place(d, 2, 2) // after the first (
	hs.a.run("go.bracket")
	hs.settle()
	expectSel(t, d, 9, 9) // after its )
	hs.a.run("go.bracket")
	hs.settle()
	expectSel(t, d, 2, 2)
}

// Selecting to the end of a document that fits in the view leaves it
// where it is: the text area would scroll it as if the room below the
// last line were text.
func TestShortDocumentDoesNotScroll(t *testing.T) {
	hs, d := editHarness(t, "s.py", "def f():\n    if x:\n        y = 1\n\treturn\n")
	hs.place(d, 0, d.buf.Len())
	hs.place(d, d.buf.Len(), d.buf.Len())
	if d.vscroll.Y != 0 {
		t.Fatalf("scrolled to %v", d.vscroll.Y)
	}
}

func TestActiveGuide(t *testing.T) {
	hs, d := editHarness(t, "a.go", "func f() {\n\tif x {\n\t\ty()\n\t}\n\tz()\n}\n")
	check := func(caret, level, from, to int) {
		t.Helper()
		hs.place(d, caret, caret)
		dc := &d.deco
		if dc.active != level || dc.activeFrom != from || dc.activeTo != to {
			t.Fatalf("caret %d: guide %d over %d-%d, want %d over %d-%d", caret, dc.active, dc.activeFrom, dc.activeTo, level, from, to)
		}
	}
	// The lines start at 0, 11, 19, 25, 28 and 33.
	check(10, 1, 1, 4) // on func's {: its body
	check(22, 2, 2, 2) // in if: its body
	check(18, 2, 2, 2) // on if's {: its body
	check(27, 2, 2, 2) // on if's }: the block it closes
	check(30, 1, 1, 4) // z(): func's body
	check(34, 1, 1, 4) // on func's }: the block it closes
}

// Deleting forward the bracket the editor put leaves typing one to type
// it.
func TestAutoCloseForgetsDeletedCloser(t *testing.T) {
	hs, d := editHarness(t, "a.go", "x\n")
	hs.place(d, 1, 1)
	hs.tt.Type("(")
	hs.settle()
	expectText(t, d, "x()\n")
	hs.key(0, ui.KeyDelete)
	expectText(t, d, "x(\n")
	hs.tt.Type(")")
	hs.settle()
	expectText(t, d, "x()\n")
	expectSel(t, d, 3, 3)
}

// longGo is Go source of n functions, each a few lines deep, for tests
// that scroll.
func longGo(n int) string {
	var b strings.Builder
	b.WriteString("package p\n")
	for i := range n {
		fmt.Fprintf(&b, "\nfunc f%d() {\n\tif x {\n\t\tfor {\n", i)
		for k := range 12 {
			fmt.Fprintf(&b, "\t\t\ty%d()\n", k)
		}
		b.WriteString("\t\t}\n\t}\n}\n")
	}
	return b.String()
}

func viewTop(hs *harness, d *editor) int {
	return int((d.vscroll.Y-editorPadY)/hs.a.font.lineH + 0.5)
}

// Moving the caret within the view leaves the view where it is, and
// moving it past the bottom scrolls a line at a time.
func TestCaretMovesKeepTheView(t *testing.T) {
	hs, d := editHarness(t, "long.go", longGo(40))
	d.vscroll.Focus()
	hs.settle()
	lineH := hs.a.font.lineH
	r, ok := hs.tt.TextCaret()
	if !ok {
		t.Fatal("no caret")
	}
	// The caret is on the first line: the view is from there to the
	// status bar.
	rows := int((760 - statusBarH - r.Y) / lineH)
	for i := range rows - 2 {
		hs.key(0, ui.KeyDown)
		if top := viewTop(hs, d); top != 0 {
			t.Fatalf("down %d: scrolled to line %d", i+1, top)
		}
	}
	for _, k := range []int{3, 10, 1, 20} {
		hs.tt.ClickAt(r.X+40, r.Y+float32(k)*lineH+lineH/2)
		hs.settle()
		if top, line := viewTop(hs, d), lineOf(d); top != 0 || line != k {
			t.Fatalf("click on row %d: caret on line %d, scrolled to line %d", k, line, top)
		}
	}
	for range rows {
		hs.key(0, ui.KeyDown)
	}
	for i := range 3 {
		was := viewTop(hs, d)
		hs.key(0, ui.KeyDown)
		if top := viewTop(hs, d); top != was+1 {
			t.Fatalf("down %d past the bottom: scrolled from line %d to %d", i+1, was, top)
		}
	}
}

// Moving the caret up past the lines that stick scrolls it out from
// under them.
func TestCaretStaysBelowStickyLines(t *testing.T) {
	hs, d := editHarness(t, "long.go", longGo(40))
	d.vscroll.Focus()
	lineH := hs.a.font.lineH
	// The view starts deep in a function, the caret further down.
	s := d.buf.Snapshot()
	at, _ := s.LineRange(19*5 + 14)
	hs.place(d, at, at)
	d.vscroll.Y = editorPadY + float32(lineOf(d)-6)*lineH
	hs.settle()
	for i := range 12 {
		hs.key(0, ui.KeyUp)
		if len(d.sticky) == 0 {
			continue
		}
		y := editorPadY + float32(lineOf(d))*lineH - d.vscroll.Y
		if y < float32(len(d.sticky))*lineH {
			t.Fatalf("up %d: caret on line %d under %d lines that stick, view at line %d", i+1, lineOf(d), len(d.sticky), viewTop(hs, d))
		}
	}
}

func lineOf(d *editor) int {
	s := d.buf.Snapshot()
	return s.LineAt(min(d.sel[1], s.Len()))
}

// A swipe mostly sideways, as a trackpad's goes a little down too, moves
// the text sideways on every event, and not down.
func TestSwipeSidewaysAlongItsAxis(t *testing.T) {
	var text strings.Builder
	for range 200 {
		text.WriteString(strings.Repeat("long line ", 40) + "\n")
	}
	hs, d := editHarness(t, "long.txt", text.String())
	hs.tt.Move(400, 300)
	for i := range 30 {
		x, y := d.hscroll.X, d.vscroll.Y
		hs.tt.Scroll(400, 300, 6, float32(i%2))
		step(hs.tt)
		if d.hscroll.X != x+6 || d.vscroll.Y != y {
			t.Fatalf("swipe %d: at %v, %v from %v, %v", i, d.hscroll.X, d.vscroll.Y, x, y)
		}
	}
	// Mostly down, it scrolls down.
	hs.tt.Scroll(400, 300, 1, 20)
	step(hs.tt)
	if d.vscroll.Y == 0 {
		t.Fatal("did not scroll down")
	}
}

// The app's edits, undo and redo among them, scroll as little as it
// takes: a caret on the last line in view leaves the view where it is,
// and with Word Wrap on too, as the area guesses the heights of the rows
// above again.
func TestEditsKeepTheView(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		hs, d := editHarness(t, "w.go", wrapFile(400))
		a := hs.a
		a.settings.WordWrap = wrap
		hs.settle()
		top := func() (int, float32) {
			g := a.geometry(d, d.buf.Snapshot().LineCount())
			i := g.at(d.vscroll.Y - editorPadY)
			return i, editorPadY + g.top(i) - d.vscroll.Y
		}
		// The caret on the last line wholly in view, and in the middle.
		for _, below := range []float32{a.font.lineH + editorPadY, d.vim.viewH / 2} {
			at := 150
			s, _ := d.buf.Snapshot().LineRange(at)
			hs.place(d, s, s)
			g := a.geometry(d, d.buf.Snapshot().LineCount())
			d.vscroll.Y = editorPadY + g.top(at) + below - d.vim.viewH
			hs.settle()
			last := below < d.vim.viewH/2
			line0, y0 := top()
			hs.tt.SetClipboard("x := 1\ny := 2\n")
			for _, k := range []struct {
				mods ui.Modifiers
				key  ui.Key
			}{{0, ui.KeyTab}, {ui.Cmd, ui.KeyZ}, {ui.Cmd, ui.KeyV}, {ui.Cmd, ui.KeyZ}, {ui.Cmd | ui.Shift, ui.KeyZ}, {ui.Cmd, ui.KeyZ}} {
				if last && k.key == ui.KeyV {
					break // pasted lines push the caret out of view
				}
				hs.key(k.mods, k.key)
				// Within what a float32 holds this far down the text.
				if line, y := top(); line != line0 || y < y0-0.01 || y > y0+0.01 {
					t.Fatalf("wrap %v, line %d, %v %v: line %d at %v at the top, was %d at %v", wrap, at, k.mods, k.key, line, y, line0, y0)
				}
			}
		}
	}
}

// menuItem runs the item of the Edit menu labeled label.
func menuItem(t *testing.T, hs *harness, label string) {
	t.Helper()
	a := hs.a
	m := a.menu(false, func(id string, _ *mygo.Window) { a.run(id) })
	for _, menu := range m.Items() {
		if menu.Label != locales.MenuEdit() {
			continue
		}
		for _, item := range menu.Submenu {
			if item.Label == label && item.Click != nil {
				item.Click(item, nil)
				return
			}
		}
	}
	t.Fatalf("no Edit menu item %q", label)
}

func TestSelectAllKeepsView(t *testing.T) {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	b.WriteString(strings.Repeat("wide ", 400))
	text := b.String()
	for _, menu := range []bool{false, true} {
		t.Run(fmt.Sprintf("menu=%v", menu), func(t *testing.T) {
			hs, d := editHarness(t, "all.txt", text)
			hs.place(d, 200*9, 200*9)
			y := d.vscroll.Y
			if y == 0 {
				t.Fatal("the caret's line did not scroll into view")
			}
			if menu {
				menuItem(t, hs, locales.CmdEditSelectAll())
				hs.settle()
			} else {
				hs.key(ui.Cmd, ui.KeyA)
			}
			expectSel(t, d, 0, d.buf.Snapshot().Len())
			if d.vscroll.Y != y || d.hscroll.X != 0 {
				t.Fatalf("scrolled to %v, %v; want %v, 0", d.hscroll.X, d.vscroll.Y, y)
			}
			// The caret is the selection's end: an arrow goes there.
			hs.key(0, ui.KeyRight)
			expectSel(t, d, d.buf.Snapshot().Len(), d.buf.Snapshot().Len())
		})
	}
}

func TestCutLineKeepsView(t *testing.T) {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	hs, d := editHarness(t, "cut.txt", b.String())
	hs.place(d, 200*9, 200*9)
	first, last := hs.a.linesInView(hs.a.geometry(d, d.buf.Snapshot().LineCount()), d.vscroll.Y, d.vim.viewH)
	for _, line := range []int{first + 1, (first + last) / 2, last - 1} {
		hs.place(d, line*9+2, line*9+2)
		y := d.vscroll.Y
		hs.key(ui.Cmd, ui.KeyX)
		if d.vscroll.Y != y {
			t.Fatalf("cutting line %d scrolled from %v to %v", line, y, d.vscroll.Y)
		}
	}
}

// proseFile writes lines of many lengths, which wrap to many rows, so that
// the heights the text area estimates are far from the ones it measures.
func (hs *harness) proseFile(name string) string {
	var b strings.Builder
	for i := range 800 {
		fmt.Fprintf(&b, "line %d %s\n", i, strings.Repeat("word ", 20+(i%7)*40))
	}
	p := hs.path(name)
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		hs.t.Fatal(err)
	}
	return p
}

// Select All leaves the view where it is in the frames it takes, rather
// than showing the end of the text for one.
func TestSelectAllLeavesTheView(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		hs := newHarness(t, true)
		a, tt := hs.a, hs.tt
		a.settings.WordWrap = wrap
		a.openFile(hs.proseFile("prose.txt"), true)
		hs.settle()
		d := a.activeEditor()
		s, _ := d.buf.Snapshot().LineRange(100)
		hs.place(d, s, s)
		// The line at the top, and how far above the view it starts.
		top := func() (int, float32) {
			g := a.geometry(d, d.buf.Snapshot().LineCount())
			l := g.at(d.vscroll.Y - editorPadY)
			return l, g.top(l) - d.vscroll.Y
		}
		line, off := top()
		a.run("edit.selectAll")
		for i := range 3 {
			step(tt)
			if l, o := top(); l != line || o != off {
				t.Errorf("wrap %v: frame %d shows line %d%+v at the top, not %d%+v", wrap, i, l, o, line, off)
			}
		}
		if n := d.buf.Snapshot().Len(); d.sel != [2]int{0, n} {
			t.Errorf("wrap %v: selection %v of %d", wrap, d.sel, n)
		}
		tt.Type("x")
		hs.settle()
		expectText(t, d, "x")
	}
}

// A line added or moved at an edge of the view scrolls it as little as
// it takes, rather than a third of the way down.
func TestEditsAtTheEdgeScrollALine(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.longFile("long.go", 400), true)
	hs.settle()
	d := a.activeEditor()
	box, ok := tt.Find("Editor long.go")
	if !ok {
		t.Fatal("no editor")
	}
	caretY := func() float32 {
		r, _ := tt.TextCaret()
		return r.Y
	}
	for caretY()+2*a.font.lineH <= box.Y+box.H {
		tt.Key(0, ui.KeyDown)
		hs.settle()
	}
	bottom := caretY()
	for _, key := range []struct {
		name string
		mods ui.Modifiers
		key  ui.Key
	}{
		{"Enter", 0, ui.KeyEnter},
		{"Enter", 0, ui.KeyEnter},
		{"move line down", ui.Alt, ui.KeyDown},
		{"copy line down", ui.Alt | ui.Shift, ui.KeyDown},
	} {
		y := d.vscroll.Y
		tt.Key(key.mods, key.key)
		hs.settle()
		if got := caretY(); got < bottom-2 || d.vscroll.Y-y > 2*a.font.lineH {
			t.Errorf("%s at the bottom: caret at y %v, scrolled from %v to %v", key.name, got, y, d.vscroll.Y)
		}
	}

	d.vscroll.Y = 2000
	hs.settle()
	s, _ := d.buf.Snapshot().LineRange(101)
	hs.place(d, s, s)
	for range 4 {
		y := d.vscroll.Y
		tt.Key(ui.Alt, ui.KeyUp)
		hs.settle()
		if got := caretY(); got > box.Y+3*a.font.lineH || y-d.vscroll.Y > 2*a.font.lineH {
			t.Errorf("move line up at the top: caret at y %v, scrolled from %v to %v", got, y, d.vscroll.Y)
		}
	}

	// An undo from far away still lands well into the view.
	far, _ := d.buf.Snapshot().LineRange(1500)
	d.selectRange(far, far, false)
	hs.settle()
	if got := caretY(); got < box.Y+box.H/4 || got > box.Y+box.H/2 {
		t.Errorf("a selection from far away shows at y %v in %+v", got, box)
	}
}

// Edits the app makes, which have the text area forget the heights of its
// rows, leave the lines where they show while the lines wrap.
func TestWrappedEditsKeepTheCaretOnScreen(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.settings.WordWrap = true
	a.openFile(hs.proseFile("prose.txt"), true)
	hs.settle()
	d := a.activeEditor()
	line := func(i int) int { s, _ := d.buf.Snapshot().LineRange(i); return s }
	hs.place(d, line(50), line(50))
	hs.place(d, line(52), line(52))
	caretY := func() float32 {
		r, _ := tt.TextCaret()
		return r.Y
	}
	y := caretY()
	for _, c := range []struct {
		name string
		do   func()
		dy   float32
	}{
		{"Enter", func() { tt.Key(0, ui.KeyEnter) }, a.font.lineH},
		{"Undo", func() { tt.Key(ui.Cmd, ui.KeyZ) }, 0},
		{"Redo", func() { tt.Key(ui.Cmd|ui.Shift, ui.KeyZ) }, a.font.lineH},
	} {
		c.do()
		hs.settle()
		if got := caretY(); got != y+c.dy {
			t.Errorf("%s: caret at y %v, want %v", c.name, got, y+c.dy)
		}
	}

	// At the last row of the view, the view makes room for the new line.
	box, ok := tt.Find("Editor prose.txt")
	if !ok {
		t.Fatal("no editor")
	}
	for caretY() < box.Y+box.H-3*a.font.lineH {
		tt.Key(0, ui.KeyDown)
		hs.settle()
	}
	for range 4 {
		tt.Key(0, ui.KeyEnter)
		hs.settle()
		if got := caretY(); got+a.font.lineH > box.Y+box.H {
			t.Errorf("Enter at the end of the view: caret at y %v in %+v", got, box)
		}
		if got := caretY(); got < box.Y+box.H-5*a.font.lineH {
			t.Errorf("Enter at the end of the view: caret jumped up to y %v in %+v", got, box)
		}
	}
}

// A click near the right edge leaves the text where it shows, for a second
// click to find the word the first was on; a drag past the edge scrolls.
func TestClickAtTheRightEdgeLeavesTheText(t *testing.T) {
	hs, d := editHarness(t, "wide.txt", strings.Repeat("abcdefghi ", 60)+"\nshort\n")
	tt := hs.tt
	box, ok := tt.Find("Editor wide.txt")
	if !ok {
		t.Fatal("no editor")
	}
	x, y := box.X+box.W-10, box.Y+editorPadY+hs.a.font.lineH/2
	tt.Press(x, y)
	hs.settle()
	if d.hscroll.X != 0 {
		t.Errorf("pressed: scrolled sideways to %v", d.hscroll.X)
	}
	tt.Release(x, y)
	hs.settle()
	if d.hscroll.X != 0 {
		t.Errorf("released: scrolled sideways to %v", d.hscroll.X)
	}
	tt.Press(x-40, y)
	hs.settle()
	tt.Move(x+30, y)
	for range 10 {
		step(tt)
	}
	if d.hscroll.X == 0 {
		t.Error("a drag past the right edge did not scroll sideways")
	}
	tt.Release(x+30, y)
	hs.settle()
}

// TestDeletedLongLineNarrowsTheText: once the longest line is gone, the
// text scrolls sideways no further than the lines left.
func TestDeletedLongLineNarrowsTheText(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "short\n")
	long := strings.Repeat("x", 600) + "\n"
	d.cmds = append(d.cmds, func(d *editor) { d.edit([2]int{0, 0}, textEdit{0, 0, long}) })
	hs.settle()
	if d.hscroll.MaxX <= 0 {
		t.Fatalf("a line of 600 scrolls no further than %v", d.hscroll.MaxX)
	}
	d.cmds = append(d.cmds, func(d *editor) { d.edit([2]int{0, 0}, textEdit{0, len(long), ""}) })
	hs.settle()
	if d.hscroll.MaxX > 0 {
		t.Errorf("without the long line the text still scrolls %v sideways", d.hscroll.MaxX)
	}
}
