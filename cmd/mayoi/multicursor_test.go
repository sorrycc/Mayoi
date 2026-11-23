package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/fold"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// expectSpans checks every selection of d, anchor and caret, in order.
func expectSpans(t *testing.T, d *editor, want ...[2]int) {
	t.Helper()
	all, _ := d.spans()
	got := make([][2]int, len(all))
	for i, s := range all {
		got[i] = [2]int{s.anchor, s.caret}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("selections %v, want %v", got, want)
	}
}

// setSpans gives d the selections, the primary the last, as gestures
// would.
func (hs *harness) setSpans(d *editor, spans ...[2]int) {
	all := make([]cursor, len(spans))
	for i, s := range spans {
		all[i] = cursor{s[0], s[1], -1}
	}
	d.cmds = append(d.cmds, func(d *editor) { d.selectSpans(all, len(all)-1, false) })
	hs.a.invalidate()
	hs.settle()
}

// multiKey is macOS's ⌘ for the keys of the commands of several
// selections, which the tests run with, on any machine.
const multiKey = ui.Super

func TestAddCursorBelowAndAbove(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "abc\nde\nfghij\n")
	hs.place(d, 2, 2)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	expectSpans(t, d, [2]int{2, 2}, [2]int{6, 6})
	// A short line keeps the column for the next.
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	expectSpans(t, d, [2]int{2, 2}, [2]int{6, 6}, [2]int{9, 9})
	expectSel(t, d, 9, 9) // the one added last is the primary
	hs.tt.Type("X")
	hs.settle()
	expectText(t, d, "abXc\ndeX\nfgXhij\n")
	expectSpans(t, d, [2]int{3, 3}, [2]int{8, 8}, [2]int{12, 12})
	// One step of undo takes back what was typed at all of them, and the
	// selections come back with it.
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "abc\nde\nfghij\n")
	expectSpans(t, d, [2]int{2, 2}, [2]int{6, 6}, [2]int{9, 9})
	hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
	expectText(t, d, "abXc\ndeX\nfgXhij\n")

	// Going back up takes back the last added.
	hs, d = editHarness(t, "b.txt", "one\ntwo\nthree\nfour\n")
	hs.place(d, 1, 1)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	hs.key(multiKey|ui.Alt, ui.KeyUp)
	expectSpans(t, d, [2]int{1, 1}, [2]int{5, 5})
	hs.key(multiKey|ui.Alt, ui.KeyUp)
	expectSpans(t, d, [2]int{1, 1})
	// Above the first line there is none to add.
	hs.key(multiKey|ui.Alt, ui.KeyUp)
	expectSpans(t, d, [2]int{1, 1})
}

func TestAddCursorSkipsFoldedLines(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "a1\nb2\nc3\nd4\ne5\n")
	snap := d.buf.Snapshot()
	d.addFold(snap, fold.Range{Start: 0, End: 2})
	hs.place(d, 1, 1)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	expectSpans(t, d, [2]int{1, 1}, [2]int{10, 10}) // line 3, past the fold
}

func TestEscapeAndClickGoBackToOneCaret(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "abc\nabc\nabc\n")
	hs.place(d, 1, 1)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	hs.key(0, ui.KeyEscape)
	expectSpans(t, d, [2]int{9, 9})

	hs.key(multiKey|ui.Alt, ui.KeyUp)
	if len(d.cursors) != 1 {
		t.Fatalf("%d other selections", len(d.cursors))
	}
	// A selection made otherwise, as Find's, is one again.
	hs.place(d, 0, 3)
	expectSpans(t, d, [2]int{0, 3})
}

func TestAddNextOccurrence(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "foo bar foo foofoo foo")
	hs.place(d, 1, 1)
	hs.key(ui.Ctrl|multiKey, ui.KeyG)
	expectSpans(t, d, [2]int{0, 3})
	hs.key(ui.Ctrl|multiKey, ui.KeyG)
	expectSpans(t, d, [2]int{0, 3}, [2]int{8, 11})
	// From a caret, whole words: foofoo is not one.
	hs.key(ui.Ctrl|multiKey, ui.KeyG)
	expectSpans(t, d, [2]int{0, 3}, [2]int{8, 11}, [2]int{19, 22})
	hs.key(ui.Ctrl|multiKey, ui.KeyG) // round to the start: none left
	expectSpans(t, d, [2]int{0, 3}, [2]int{8, 11}, [2]int{19, 22})
	hs.tt.Type("qux")
	hs.settle()
	expectText(t, d, "qux bar qux foofoo qux")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "foo bar foo foofoo foo")

	// From a selection, any match, never one that overlaps another.
	hs, d = editHarness(t, "b.txt", "aaaaa")
	hs.place(d, 0, 2)
	hs.key(ui.Ctrl|multiKey, ui.KeyG)
	expectSpans(t, d, [2]int{0, 2}, [2]int{2, 4})
	hs.key(ui.Ctrl|multiKey, ui.KeyG)
	expectSpans(t, d, [2]int{0, 2}, [2]int{2, 4})
}

func TestSelectAllOccurrences(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "let 名 = 1; 名 + 名; 名名\n")
	hs.place(d, 5, 5)
	hs.key(multiKey|ui.Shift, ui.KeyL)
	expectSpans(t, d, [2]int{4, 5}, [2]int{11, 12}, [2]int{15, 16})
	expectSel(t, d, 4, 5)
	if got := hs.statusPos(); !strings.Contains(got, "3 selections") {
		t.Errorf("status bar %q", got)
	}
	hs.tt.Type("ü")
	hs.settle()
	expectText(t, d, "let ü = 1; ü + ü; 名名\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "let 名 = 1; 名 + 名; 名名\n")
}

// statusPos returns the caret's place as the status bar says it.
func (hs *harness) statusPos() string {
	for _, s := range hs.tt.Texts() {
		if strings.HasPrefix(s, "Ln ") {
			return s
		}
	}
	return ""
}

func TestColumnSelectByKeys(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "abcd\nab\n\nabcdef\n")
	hs.place(d, 1, 1)
	for range 3 {
		hs.key(multiKey|ui.Alt|ui.Shift, ui.KeyDown)
	}
	// A column of carets stands on every line, a short one at its end.
	expectSpans(t, d, [2]int{1, 1}, [2]int{6, 6}, [2]int{8, 8}, [2]int{10, 10})
	hs.key(multiKey|ui.Alt|ui.Shift, ui.KeyRight)
	hs.key(multiKey|ui.Alt|ui.Shift, ui.KeyRight)
	// Of some width, it leaves out the line that ends before it.
	expectSpans(t, d, [2]int{1, 3}, [2]int{6, 7}, [2]int{10, 12})
	hs.tt.Type("Z")
	hs.settle()
	expectText(t, d, "aZd\naZ\n\naZdef\n")
}

func TestColumnsCountTabsAndWideRunes(t *testing.T) {
	for _, tc := range []struct {
		line      string
		i, col    int
		at        int
		reachable bool
	}{
		{"\tab", 1, 4, 1, true},
		{"a\tb", 2, 4, 2, true},
		{"日本x", 2, 4, 2, true},
		{"日本x", 1, 2, 1, true},
		{"ab", 2, 2, 2, true},
	} {
		if got := columnOf(tc.line, tc.i); got != tc.col {
			t.Errorf("columnOf(%q, %d) = %d, want %d", tc.line, tc.i, got, tc.col)
		}
		if got, ok := runeAtColumn(tc.line, tc.col); got != tc.at || ok != tc.reachable {
			t.Errorf("runeAtColumn(%q, %d) = %d, %v", tc.line, tc.col, got, ok)
		}
	}
	// Inside a tab or a wide rune, the nearer edge.
	if got, _ := runeAtColumn("\tx", 3); got != 1 {
		t.Errorf("in a tab: %d", got)
	}
	if got, _ := runeAtColumn("日x", 1); got != 0 {
		t.Errorf("in a wide rune: %d", got)
	}
	if got, ok := runeAtColumn("ab", 5); got != 2 || ok {
		t.Errorf("past the end: %d, %v", got, ok)
	}
}

func TestMultiDeleteByCharactersAndWords(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "日本語 x👍🏽\n日本語 x👍🏽\n")
	n := len([]rune("日本語 x👍🏽\n"))
	hs.setSpans(d, [2]int{7, 7}, [2]int{n + 7, n + 7})
	// The emoji and its skin tone go as one character.
	hs.key(0, ui.KeyBackspace)
	expectText(t, d, "日本語 x\n日本語 x\n")
	hs.key(ui.Alt, ui.KeyBackspace)
	expectText(t, d, "日本語 \n日本語 \n")
	hs.key(ui.Alt, ui.KeyBackspace)
	expectText(t, d, "\n\n")
	// Typing and deleting in a row undo as one step.
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "日本語 x👍🏽\n日本語 x👍🏽\n")

	hs, d = editHarness(t, "b.txt", "ab\nab\n")
	hs.setSpans(d, [2]int{0, 0}, [2]int{3, 3})
	hs.key(0, ui.KeyDelete)
	expectText(t, d, "b\nb\n")
	expectSpans(t, d, [2]int{0, 0}, [2]int{2, 2})
}

func TestMultiMovesAndExtends(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "one two\nthree\nfour five\n")
	hs.setSpans(d, [2]int{0, 0}, [2]int{8, 8})
	hs.key(ui.Shift, ui.KeyRight)
	expectSpans(t, d, [2]int{0, 1}, [2]int{8, 9})
	// Off a selection, a move collapses it to its edge that way.
	hs.key(0, ui.KeyLeft)
	expectSpans(t, d, [2]int{0, 0}, [2]int{8, 8})
	hs.key(ui.Alt|ui.Shift, ui.KeyRight)
	expectSpans(t, d, [2]int{0, 3}, [2]int{8, 13})
	hs.key(0, ui.KeyEnd)
	expectSpans(t, d, [2]int{7, 7}, [2]int{13, 13})
	hs.key(0, ui.KeyHome)
	expectSpans(t, d, [2]int{0, 0}, [2]int{8, 8})
	// Up and down keep each caret's column; the carets that meet join.
	hs.key(0, ui.KeyDown)
	expectSpans(t, d, [2]int{8, 8}, [2]int{14, 14})
	hs.key(0, ui.KeyUp)
	hs.key(0, ui.KeyUp)
	expectSpans(t, d, [2]int{0, 0})
}

func TestMultiCopyCutAndPaste(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "one two\nthree four\n")
	hs.setSpans(d, [2]int{0, 3}, [2]int{8, 13})
	hs.key(ui.Cmd, ui.KeyC)
	if got := hs.tt.Clipboard(); got != "one\nthree" {
		t.Fatalf("copied %q", got)
	}
	// As many lines as selections: a line at each.
	hs.setSpans(d, [2]int{7, 7}, [2]int{18, 18})
	hs.command("paste")
	expectText(t, d, "one twoone\nthree fourthree\n")
	// Otherwise all of it at each.
	hs.tt.SetClipboard("!")
	hs.key(ui.Cmd, ui.KeyV)
	expectText(t, d, "one twoone!\nthree fourthree!\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "one twoone\nthree fourthree\n")

	// Cut with carets only takes their lines, in one step.
	hs, d = editHarness(t, "b.txt", "a\nb\nc\nd\n")
	hs.setSpans(d, [2]int{0, 0}, [2]int{4, 4})
	hs.key(ui.Cmd, ui.KeyX)
	if got := hs.tt.Clipboard(); got != "a\nc\n" {
		t.Fatalf("cut %q", got)
	}
	expectText(t, d, "b\nd\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "a\nb\nc\nd\n")

	// Copy of the Edit menu takes them all too, its key ⌘C still.
	hs.setSpans(d, [2]int{0, 1}, [2]int{4, 5})
	a := hs.a
	m := a.menu(false, func(id string, _ *mygo.Window) { a.run(id) })
	copied := false
	for _, menu := range m.Items() {
		for _, it := range menu.Submenu {
			if it.Label == locales.CmdEditCopy() && it.Role == "" && it.Click != nil {
				if it.Accelerator != "CmdOrCtrl+C" {
					t.Errorf("Copy takes %q", it.Accelerator)
				}
				it.Click(it, nil)
				copied = true
			}
		}
	}
	if !copied {
		t.Fatal("no Copy of several selections in the Edit menu")
	}
	hs.settle()
	if got := hs.tt.Clipboard(); got != "a\nc" {
		t.Fatalf("Copy from the menu %q", got)
	}
}

func TestMultiTypingClosesBracketsAndJoinsUndo(t *testing.T) {
	hs, d := editHarness(t, "a.go", "f\nf\n")
	hs.setSpans(d, [2]int{1, 1}, [2]int{3, 3})
	for _, s := range []string{"(", "x", ")", ";"} {
		hs.tt.Type(s)
		hs.settle()
	}
	expectText(t, d, "f(x);\nf(x);\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "f\nf\n")
	// A bracket typed over selections goes around each.
	hs.setSpans(d, [2]int{0, 1}, [2]int{2, 3})
	hs.tt.Type("[")
	hs.settle()
	expectText(t, d, "[f]\n[f]\n")
	expectSpans(t, d, [2]int{1, 2}, [2]int{5, 6})
	// Enter and Tab at each.
	hs, d = editHarness(t, "b.go", "{}\n{}\n")
	hs.setSpans(d, [2]int{1, 1}, [2]int{4, 4})
	hs.key(0, ui.KeyEnter)
	expectText(t, d, "{\n\t\n}\n{\n\t\n}\n")
	hs.key(0, ui.KeyTab)
	expectText(t, d, "{\n\t\t\n}\n{\n\t\t\n}\n")
	hs.key(ui.Shift, ui.KeyTab)
	expectText(t, d, "{\n\t\n}\n{\n\t\n}\n")
}

func TestMultiTypingWaitsForTheInputMethod(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "ab\nab\n")
	hs.setSpans(d, [2]int{1, 1}, [2]int{4, 4})
	hs.tt.Compose("ni", 2)
	hs.settle()
	if !d.composing {
		t.Fatal("not composing")
	}
	expectText(t, d, "ab\nab\n")
	hs.tt.Type("你")
	hs.settle()
	expectText(t, d, "a你b\na你b\n")
	expectSpans(t, d, [2]int{2, 2}, [2]int{6, 6})

	// Composing over selections takes them out at each, once committed.
	hs.setSpans(d, [2]int{0, 2}, [2]int{4, 6})
	hs.tt.Compose("ha", 2)
	hs.settle()
	hs.tt.Type("哈")
	hs.settle()
	expectText(t, d, "哈b\n哈b\n")
	hs.key(ui.Cmd, ui.KeyZ)
	expectText(t, d, "a你b\na你b\n")
}

// The pointer's place over rune at of the editor's text, in the window,
// for a file of ASCII.
func (hs *harness) textPoint(t *testing.T, d *editor, line, col int) (float32, float32) {
	t.Helper()
	box, ok := hs.tt.Find(locales.EditorLabel(d.name()))
	if !ok {
		t.Fatal("no editor")
	}
	f := hs.a.font
	return box.X + d.textX + (float32(col)+0.3)*f.charW, box.Y + editorPadY + float32(line)*f.lineH + f.lineH/2 - d.vscroll.Y
}

func TestAltClickAddsAndTakesCarets(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "abcd\nabcd\nabcd\n")
	hs.place(d, 0, 0)
	x, y := hs.textPoint(t, d, 1, 2)
	hs.tt.ClickAtWith(ui.Alt, x, y)
	hs.settle()
	expectSpans(t, d, [2]int{0, 0}, [2]int{7, 7})
	if d.defClick {
		t.Error("an Alt-click went to a definition")
	}
	x2, y2 := hs.textPoint(t, d, 2, 3)
	hs.tt.ClickAtWith(ui.Alt, x2, y2)
	hs.settle()
	expectSpans(t, d, [2]int{0, 0}, [2]int{7, 7}, [2]int{13, 13})
	// Again on one takes it away.
	hs.tt.ClickAtWith(ui.Alt, x, y)
	hs.settle()
	expectSpans(t, d, [2]int{0, 0}, [2]int{13, 13})
	// A plain click is one caret again.
	hs.tt.ClickAt(x, y)
	hs.settle()
	if len(d.cursors) != 0 {
		t.Errorf("%d other selections after a click", len(d.cursors))
	}
}

// Cmd-click still goes to a definition, and opens a note's link, rather
// than adding a caret.
func TestCmdClickKeepsGoingToDefinitions(t *testing.T) {
	hs, d := editHarness(t, "a.go", "package a\n\nfunc f() { f() }\n")
	x, y := hs.textPoint(t, d, 2, 12)
	hs.tt.Press(x, y)
	hs.settle()
	ev := ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, Clicks: 1, Mods: ui.Cmd, X: d.textX + 12*hs.a.font.charW, Y: editorPadY + 2*hs.a.font.lineH + 2}
	hs.a.editorKey(d, ev)
	if !d.defClick || len(d.cursors) != 0 {
		t.Errorf("Cmd-click: definition %v, %d other selections", d.defClick, len(d.cursors))
	}
	hs.tt.Release(x, y)
	hs.settle()
}

// A drag with Shift and Alt, or with the middle button, selects a column.
// The harness presses no button but the first with modifiers held, so the
// events go to the editor's handler as the window would send them.
func TestColumnSelectByDrag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button int
		mods   ui.Modifiers
	}{{"shift-alt", 0, ui.Shift | ui.Alt}, {"middle", 2, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			hs, d := editHarness(t, "a.txt", "abcdef\nab\nabcdef\n")
			a, f := hs.a, hs.a.font
			at := func(line, col int) (float32, float32) {
				return d.textX + float32(col)*f.charW, editorPadY + float32(line)*f.lineH + f.lineH/2
			}
			x, y := at(0, 1)
			if !a.editorKey(d, ui.InputEvent{Kind: ui.InputPointerDown, Button: tc.button, Mods: tc.mods, Clicks: 1, X: x, Y: y}) {
				t.Fatal("the press was not taken")
			}
			hs.settle()
			x, y = at(2, 4)
			a.editorKey(d, ui.InputEvent{Kind: ui.InputPointerMove, Button: tc.button, Mods: tc.mods, X: x, Y: y})
			a.editorKey(d, ui.InputEvent{Kind: ui.InputPointerUp, Button: tc.button, Mods: tc.mods, X: x, Y: y})
			hs.settle()
			expectSpans(t, d, [2]int{1, 4}, [2]int{8, 9}, [2]int{11, 14})
			expectSel(t, d, 11, 14)
		})
	}
}

// The selections paint on every line, with the text area's own one.
func TestMultiSelectionShape(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "ab ab\n\tab\n")
	hs.place(d, 0, 2)
	hs.key(multiKey|ui.Shift, ui.KeyL)
	snap := d.buf.Snapshot()
	sh := hs.a.selection(d, snap, 0, 1)
	f := hs.a.font
	want := []runeRect{{line: 0, x0: 0, x1: 2 * f.charW}, {line: 0, x0: 3 * f.charW, x1: 5 * f.charW}, {line: 1, x0: f.tabW, x1: f.tabW + 2*f.charW}}
	if !slices.Equal(sh.rects, want) {
		t.Errorf("boxes %v, want %v", sh.rects, want)
	}
}

func TestMultiEditsFollowInSplitViews(t *testing.T) {
	hs, left, right := splitHarness(t, func(hs *harness) string { return hs.path("main.go") })
	text := left.buf.String()
	at := strings.Index(text, "func main")
	hs.place(left, at, at)
	hs.setSpans(left, [2]int{1, 1}, [2]int{at, at})
	right.vscroll.Focus()
	hs.settle()
	hs.place(right, 0, 0)
	hs.tt.Type("// x\n")
	hs.settle()
	// The left column's carets move with the text typed in the right.
	expectSpans(t, left, [2]int{6, 6}, [2]int{at + 5, at + 5})
}

func TestMultiCursorStaysOneInVimAndLivePreview(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "abc\nabc\n")
	hs.a.run("sel.addCursorBelow")
	hs.settle()
	if len(d.cursors) != 0 {
		t.Errorf("vim mode: %d other selections", len(d.cursors))
	}

	hs, d = editHarness(t, "a.md", "abc\nabc\n")
	hs.place(d, 0, 0)
	hs.key(multiKey|ui.Alt, ui.KeyDown)
	if len(d.cursors) != 1 {
		t.Fatalf("source: %d other selections", len(d.cursors))
	}
	hs.a.run("view.toggleLivePreview")
	hs.settle()
	if len(d.cursors) != 0 {
		t.Errorf("Live Preview: %d other selections", len(d.cursors))
	}
}

func TestMultiCursorReadOnlyCopiesOnly(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "ab\nab\n")
	hs.setSpans(d, [2]int{0, 1}, [2]int{3, 4})
	// A file that could not be read takes no edits; the frame shows its
	// failure, so the edits are made as its keys would make them.
	d.sync = syncLoadFailed
	hs.a.deleteSpans(d, moveChar, -1)
	all, p := d.spans()
	d.typeSpans(all, p, 0, 0, "x", stepTyping)
	hs.a.copySpans(d)
	d.sync = syncIdle
	expectText(t, d, "ab\nab\n")
	if got := hs.tt.Clipboard(); got != "a\na" {
		t.Errorf("copied %q", got)
	}
}

// What the text area typed at the primary is found as a replacement of its
// selection, wherever the runes around it repeat it.
func TestTypedAt(t *testing.T) {
	buf := func(s string) ui.TextSnapshot { b := ui.NewTextBuffer(s); return b.Snapshot() }
	for _, tc := range []struct {
		old, now   string
		sel        [2]int
		start, end int
		ins        string
	}{
		{"a", "aa", [2]int{0, 0}, 0, 0, "a"},
		{"abc", "abc", [2]int{2, 3}, 2, 3, "c"},
		{"abc", "ac", [2]int{1, 2}, 1, 2, ""},
		{"ab", "aéb", [2]int{1, 1}, 1, 1, "é"},
		// An input method's replacement of the rune before the caret.
		{"abc", "aXc", [2]int{2, 2}, 1, 2, "X"},
	} {
		start, end, ins, ok := typedAt(buf(tc.old), buf(tc.now), tc.sel)
		if !ok || start != tc.start || end != tc.end || ins != tc.ins {
			t.Errorf("%q → %q at %v: %d %d %q %v", tc.old, tc.now, tc.sel, start, end, ins, ok)
		}
	}
}

func TestMergeSpans(t *testing.T) {
	all, p := mergeSpans([]cursor{{5, 5, -1}, {0, 3, -1}, {3, 3, -1}, {4, 8, -1}, {9, 9, -1}}, 0)
	want := []cursor{{0, 3, -1}, {4, 8, -1}, {9, 9, -1}}
	if !slices.Equal(all, want) || p != 1 {
		t.Errorf("merged %v, primary %d", all, p)
	}
}

func TestCharStepKeepsClustersWhole(t *testing.T) {
	b := ui.NewTextBuffer("é👍🏽🇯🇵x")
	snap := b.Snapshot()
	var stops []int
	for at := 0; at < snap.Len(); at = charStep(snap, at, 1) {
		stops = append(stops, at)
	}
	if want := []int{0, 2, 4, 6}; !slices.Equal(stops, want) {
		t.Errorf("forward %v, want %v", stops, want)
	}
	stops = stops[:0]
	for at := snap.Len(); at > 0; at = charStep(snap, at, -1) {
		stops = append(stops, at)
	}
	if want := []int{7, 6, 4, 2}; !slices.Equal(stops, want) {
		t.Errorf("back %v, want %v", stops, want)
	}
}

// The keys of the commands of several selections on each system, which
// TestShortcutsDoNotCollide checks against the others.
func TestMultiCursorKeys(t *testing.T) {
	for goos, want := range map[string]map[string]string{
		"darwin": {"sel.addCursorAbove": "⌥⌘↑", "sel.addNextOccurrence": "⌃⌘G", "sel.selectAllOccurrences": "⇧⌘L", "sel.columnDown": "⌥⇧⌘↓"},
		"windows": {"sel.addCursorAbove": "Ctrl+Alt+Up", "sel.addCursorBelow": "Ctrl+Alt+Down", "sel.addNextOccurrence": "Ctrl+D",
			"sel.selectAllOccurrences": "Ctrl+Shift+L", "sel.columnDown": "Ctrl+Alt+Shift+Down"},
		"linux": {"sel.addCursorAbove": "", "sel.addNextOccurrence": "Ctrl+D", "sel.selectAllOccurrences": "Ctrl+Shift+L", "sel.columnUp": ""},
	} {
		t.Run(goos, func(t *testing.T) {
			onOS(t, goos)
			a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
			t.Cleanup(a.shutdown)
			for id, keys := range want {
				if got := a.keyHint(id); got != keys {
					t.Errorf("%s: %q, want %q", id, got, keys)
				}
			}
			// A terminal keeps them.
			for _, mc := range multiCommands {
				st, ok := parseAccel(platformAccel(mc.id, mc.accel))
				if !ok {
					continue
				}
				if id, skip := a.shellSkip(st); skip && id == mc.id {
					t.Errorf("a terminal leaves %s to %s", st, mc.id)
				}
			}
		})
	}
}
