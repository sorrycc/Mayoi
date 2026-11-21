package main

import (
	"bytes"
	"encoding/json"
	"image"
	pngenc "image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
)

// barItem finds the item of a menu bar's menu by its labels.
func barItem(t *testing.T, m *mygo.Menu, path ...string) *mygo.MenuItem {
	t.Helper()
	items := m.Items()
	var it *mygo.MenuItem
	for _, label := range path {
		it = nil
		for _, x := range items {
			if x.Label == label {
				it = x
				break
			}
		}
		if it == nil {
			t.Fatalf("no menu item %q of %q", label, path)
		}
		items = it.Submenu
	}
	return it
}

// shape writes the layout as columns by their place, rows in [] and
// stacks in {}.
func shape(a *app) string {
	var b strings.Builder
	var write func(p *tile)
	write = func(p *tile) {
		if p.col != nil {
			for i, c := range a.cols {
				if c == p.col {
					b.WriteByte(byte('A' + i))
				}
			}
			return
		}
		open, end := "[", "]"
		if p.down {
			open, end = "{", "}"
		}
		b.WriteString(open)
		for _, k := range p.kids {
			write(k)
		}
		b.WriteString(end)
	}
	write(a.layout)
	return b.String()
}

func editorBox(t *testing.T, tt *ui.Tester, name string) ui.Rect {
	t.Helper()
	r, ok := tt.Find("Editor " + name)
	if !ok {
		t.Fatalf("no editor of %s: %q", name, tt.Texts())
	}
	return r
}

// On macOS ⌘D and ⇧⌘D are the menu's keys of New Column Right and New
// Column Below, which the menu bar runs before the focused editor or
// terminal sees them. Split Right keeps ⌘\\ and Split Down has no key there,
// and both still show the tab again. Add Next Occurrence takes ⌃⌘G.
func TestSplitKeysOnMacOS(t *testing.T) {
	onOS(t, "darwin")
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	ran := ""
	m := a.menu(false, func(id string, _ *mygo.Window) { ran = id; a.run(id) })
	for label, want := range map[string]string{"Split Right": "CmdOrCtrl+\\", "Split Down": "",
		"New Column Right": "CmdOrCtrl+D", "New Column Below": "Shift+CmdOrCtrl+D"} {
		if got := barItem(t, m, "View", label).Accelerator; got != want {
			t.Errorf("%s: %q, want %q", label, got, want)
		}
	}
	if got := barItem(t, m, "Selection", "Add Next Occurrence").Accelerator; got != "Ctrl+CmdOrCtrl+G" {
		t.Errorf("Add Next Occurrence: %q", got)
	}
	for id, want := range map[string]string{"view.splitRight": "⌘\\", "view.splitDown": "", "view.newColumnRight": "⌘D",
		"view.newColumnDown": "⇧⌘D", "sel.addNextOccurrence": "⌃⌘G"} {
		if got := a.keyHint(id); got != want {
			t.Errorf("%s reads %q, want %q", id, got, want)
		}
	}
	// ⌘D in the text is neither Add Next Occurrence nor a split: the menu
	// alone takes it.
	d := a.activeEditor()
	hs.place(d, 1, 1)
	hs.key(ui.Super, ui.KeyD)
	hs.key(ui.Super|ui.Shift, ui.KeyD)
	expectSpans(t, d, [2]int{1, 1})
	if shape(a) != "[A]" {
		t.Fatalf("⌘D in the text: %s", shape(a))
	}
	click := func(label string) {
		it := barItem(t, m, "View", label)
		it.Click(it, nil)
	}
	click("Split Right")
	hs.settle()
	if ran != "view.splitRight" || shape(a) != "[AB]" || a.col != 1 || a.column().tab() == nil {
		t.Fatalf("Split Right ran %q: %s, keyboard in %d", ran, shape(a), a.col)
	}
	click("Split Down")
	hs.settle()
	if shape(a) != "[A{BC}]" || a.col != 2 || a.column().tab().ed.document != d.document {
		t.Fatalf("Split Down: %s, keyboard in %d", shape(a), a.col)
	}
	// A binding of keybindings.json for ⌘D takes it from the menu, and
	// one of ⌘D for Split Right gives it that key.
	km, err := keymap.Parse([]byte(`[{"key": "cmd+d", "command": "workbench.action.splitEditor"}]`))
	if err != nil {
		t.Fatal(err)
	}
	a.keys, a.hints = km, nil
	m = a.menu(false, func(string, *mygo.Window) {})
	if got := barItem(t, m, "View", "New Column Right").Accelerator; got != "" {
		t.Errorf("bound elsewhere, New Column Right keeps %q", got)
	}
	if got := a.keyHint("view.splitRight"); got != "⌘D" {
		t.Errorf("Split Right bound to ⌘D reads %q", got)
	}
}

// A binding of keybindings.json takes a menu's key when it is that key on
// the system Mayoi's shortcuts are of, whatever MyGo's ui.Cmd is on the
// machine the test runs on: ⌘D and ⌘\\ on macOS, which a binding of ⌃D
// leaves, and Ctrl+\\ elsewhere.
func TestBindingTakesSplitRightKey(t *testing.T) {
	was := ui.Cmd
	t.Cleanup(func() { ui.Cmd = was })
	accel := func(goos, binding, id string) string {
		t.Helper()
		onOS(t, goos)
		a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
		t.Cleanup(a.shutdown)
		key, _ := json.Marshal(binding)
		km, err := keymap.Parse([]byte(`[{"key": ` + string(key) + `, "command": "editor.action.addSelectionToNextFindMatch"}]`))
		if err != nil {
			t.Fatal(err)
		}
		a.keys = km
		for _, cmd := range a.commands() {
			if cmd.id == id {
				return a.menuAccel(cmd)
			}
		}
		t.Fatalf("no %s", id)
		return ""
	}
	for _, machine := range []ui.Modifiers{ui.Super, ui.Ctrl} {
		ui.Cmd = machine
		for _, tc := range []struct{ goos, binding, id, want string }{
			{"darwin", "cmd+d", "view.newColumnRight", ""},
			{"darwin", "ctrl+d", "view.newColumnRight", "CmdOrCtrl+D"},
			{"darwin", "cmd+\\", "view.splitRight", ""},
			{"darwin", "ctrl+\\", "view.splitRight", "CmdOrCtrl+\\"},
			{"linux", "ctrl+\\", "view.splitRight", ""},
			{"linux", "ctrl+d", "view.splitRight", "Ctrl+\\"},
			{"windows", "ctrl+\\", "view.splitRight", ""},
			{"windows", "ctrl+d", "view.splitRight", "Ctrl+\\"},
		} {
			if got := accel(tc.goos, tc.binding, tc.id); got != tc.want {
				t.Errorf("ui.Cmd %v on %s: %s bound, %s has %q, want %q", machine, tc.goos, tc.binding, tc.id, got, tc.want)
			}
		}
	}
}

// newColumnBy runs New Column Right or Below as the menu bar does for ⌘D
// or ⇧⌘D on macOS.
func newColumnBy(t *testing.T, hs *harness, label string) {
	t.Helper()
	a := hs.a
	m := a.menu(false, func(id string, _ *mygo.Window) { a.run(id) })
	it := barItem(t, m, "View", label)
	it.Click(it, nil)
	hs.settle()
}

// expectEmptyColumn checks that the column with the keyboard has no tab
// and has the keyboard itself, the frames after too.
func expectEmptyColumn(t *testing.T, hs *harness, want string, at int) {
	t.Helper()
	a := hs.a
	hs.settle()
	if shape(a) != want || a.col != at || len(a.cols[at].tabs) != 0 || a.focus != paneEditor || !hs.tt.Focused("Empty Column") {
		t.Fatalf("%s, keyboard in %d, %d tabs there, focus %v, empty column focused %v",
			shape(a), a.col, len(a.cols[at].tabs), a.focus, hs.tt.Focused("Empty Column"))
	}
}

// ⌘D opens an empty column beside a document and ⇧⌘D one below an empty
// column or a picture, each with the keyboard and no tab of the one split,
// and what opens next goes there.
func TestNewEmptyColumn(t *testing.T) {
	onOS(t, "darwin")
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	src := a.activeEditor()
	hs.place(src, 3, 7)
	newColumnBy(t, hs, "New Column Right")
	expectEmptyColumn(t, hs, "[AB]", 1)
	if len(a.cols[0].tabs) != 1 || a.cols[0].tab().ed != src || src.sel != [2]int{3, 7} || len(a.docs) != 1 {
		t.Fatalf("the column split: %d tabs, selection %v, %d documents", len(a.cols[0].tabs), src.sel, len(a.docs))
	}
	// Tab goes on from it to the welcome page, and it is no stop of Tab
	// once the keyboard left it.
	hs.key(0, ui.KeyTab)
	if hs.tt.Focused("Empty Column") || a.cols[1].holds != holdNone {
		t.Fatalf("after Tab: empty column focused %v, holds %v", hs.tt.Focused("Empty Column"), a.cols[1].holds)
	}
	a.col = 1
	a.cols[1].holds = holdAsked
	a.focusColumn()
	expectEmptyColumn(t, hs, "[AB]", 1)
	// From the empty one: one more, below it.
	newColumnBy(t, hs, "New Column Below")
	expectEmptyColumn(t, hs, "[A{BC}]", 2)
	// What opens next goes to it, and a new file after.
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	if a.col != 2 || a.cols[2].tab() == nil || a.cols[2].tab().path() != hs.path("README.md") || len(a.cols[1].tabs) != 0 {
		t.Fatalf("opened in column %d: %s", a.col, shape(a))
	}
	// From a picture.
	img := hs.path("dot.png")
	var png bytes.Buffer
	if err := pngenc.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(img, png.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(img, true)
	hs.settle()
	newColumnBy(t, hs, "New Column Below")
	expectEmptyColumn(t, hs, "[A{BCD}]", 3)
	if v := a.cols[2].tab().view; v == nil || v.path != img || len(a.cols[2].tabs) != 2 {
		t.Fatalf("the picture's column: %d tabs", len(a.cols[2].tabs))
	}
	a.newFile()
	hs.settle()
	if a.col != 3 || len(a.cols[3].tabs) != 1 || a.cols[3].tab().ed == nil {
		t.Fatalf("new file in column %d: %s", a.col, shape(a))
	}
	// ⌘W closes an empty column, the keyboard going to its neighbour.
	newColumnBy(t, hs, "New Column Right")
	expectEmptyColumn(t, hs, "[A{BC[DE]}]", 4)
	a.run("file.close")
	hs.settle()
	if shape(a) != "[A{BCD}]" || a.col != 3 {
		t.Fatalf("closed the empty column: %s, keyboard in %d", shape(a), a.col)
	}
}

// From a terminal tab ⌘D opens an empty column and no terminal; the
// terminal goes on as it was, and a new terminal in the editor area goes
// to the new column. With the panel's terminal focused, the column that
// had the keyboard splits.
func TestNewEmptyColumnFromTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	onOS(t, "darwin")
	hs := newHarness(t, true)
	a := hs.a
	a.run("view.terminalTab")
	hs.settle()
	s := a.column().tab().term
	if s == nil || len(a.terms) != 1 {
		t.Fatal("no terminal tab")
	}
	hs.key(ui.Super, ui.KeyD)
	if shape(a) != "[A]" || len(a.terms) != 1 {
		t.Fatalf("⌘D in the terminal: %s, %d terminals", shape(a), len(a.terms))
	}
	newColumnBy(t, hs, "New Column Right")
	expectEmptyColumn(t, hs, "[AB]", 1)
	if len(a.terms) != 1 || a.cols[0].tab().term != s || s.tab == nil || len(a.cols[0].tabs) != 1 {
		t.Fatalf("%d terminals, the first in a tab %v", len(a.terms), s.tab != nil)
	}
	a.run("view.terminalTab")
	hs.settle()
	if len(a.terms) != 2 || a.col != 1 || a.cols[1].tab().term == nil {
		t.Fatalf("new terminal in column %d, %d terminals", a.col, len(a.terms))
	}
	// The panel's terminal has the keyboard: the column that had it last
	// splits, and the panel's terminal stays.
	a.run("view.terminal")
	hs.settle()
	if a.focus != paneTerminal || a.panelTerm == nil {
		t.Fatalf("the panel: focus %v", a.focus)
	}
	panel, n := a.panelTerm, len(a.terms)
	newColumnBy(t, hs, "New Column Below")
	expectEmptyColumn(t, hs, "[A{BC}]", 2)
	if a.panelTerm != panel || len(a.terms) != n {
		t.Fatalf("the panel's terminal changed: %d terminals", len(a.terms))
	}
	a.closeTerminals()
}

// Off macOS Split Right keeps Ctrl+\, Split Down takes Ctrl+Shift+D, from
// a terminal too, and Ctrl+D is still Add Next Occurrence, which a
// terminal keeps for its shell's end of input.
func TestSplitKeysOffMacOS(t *testing.T) {
	onOS(t, "linux")
	hs, d := editHarness(t, "a.txt", "foo bar foo")
	a := hs.a
	hs.place(d, 1, 1)
	if runtime.GOOS == "darwin" {
		// The text area of macOS takes ⌃D first, as Emacs's: the editor's
		// own route of the key.
		if !a.multiStroke(ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Ctrl, Key: ui.KeyD}) {
			t.Fatal("Ctrl+D is not Add Next Occurrence")
		}
		hs.settle()
	} else {
		hs.key(ui.Ctrl, ui.KeyD)
	}
	expectSpans(t, d, [2]int{0, 3})
	if shape(a) != "[A]" {
		t.Fatalf("Ctrl+D split: %s", shape(a))
	}
	// From a picture, which takes no keys as text does.
	img := hs.path("dot.png")
	var png bytes.Buffer
	if err := pngenc.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(img, png.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(img, true)
	hs.settle()
	hs.key(ui.Ctrl, ui.KeyBackslash)
	if shape(a) != "[AB]" || a.col != 1 {
		t.Fatalf("Ctrl+\\: %s", shape(a))
	}
	if runtime.GOOS == "darwin" {
		// The tester of macOS does not give Control and Shift to the
		// menus' keys of Linux: menuKeys' lookup of the key.
		st := keymap.Stroke{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyD}
		for _, cmd := range a.commands() {
			if got, ok := keymap.ParseAccel(a.menuAccel(cmd)); ok && got == st {
				a.run(cmd.id)
			}
		}
		hs.settle()
	} else {
		hs.key(ui.Ctrl|ui.Shift, ui.KeyD)
	}
	if shape(a) != "[A{BC}]" || a.col != 2 {
		t.Fatalf("Ctrl+Shift+D: %s", shape(a))
	}
	if v, w := a.cols[1].tab().view, a.cols[2].tab().view; v == nil || w == nil || v == w || w.path != img {
		t.Fatalf("the picture split down: %v and %v", v, w)
	}
	if id, ok := a.shellSkip(keymap.Stroke{Mods: ui.Ctrl, Key: ui.KeyD}); ok {
		t.Errorf("a terminal leaves Ctrl+D to %q", id)
	}
	if id, ok := a.shellSkip(keymap.Stroke{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyD}); !ok || id != "view.splitDown" {
		t.Errorf("a terminal keeps Ctrl+Shift+D: %q %v", id, ok)
	}
	for id, want := range map[string]string{"view.splitRight": "Ctrl+\\", "view.splitDown": "Ctrl+Shift+D", "sel.addNextOccurrence": "Ctrl+D",
		"view.newColumnRight": "", "view.newColumnDown": ""} {
		if got := a.keyHint(id); got != want {
			t.Errorf("%s reads %q, want %q", id, got, want)
		}
	}
}

// Split Right, then Split Down of the right column, stacks two columns
// beside the first, each another view of the document; the handle
// between the stacked two moves the height, and closing one gives its
// room back.
func TestSplitRightThenDown(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	long := hs.longFile("long.go", 300)
	a.openFile(long, true)
	hs.settle()
	src := a.activeEditor()
	hs.place(src, 40, 44)
	hs.scrollEditor(src, 600)
	a.run("view.splitRight")
	hs.settle()
	a.run("view.splitDown")
	hs.settle()
	if shape(a) != "[A{BC}]" || a.col != 2 || len(a.docs) != 1 {
		t.Fatalf("%s, keyboard in %d, %d documents", shape(a), a.col, len(a.docs))
	}
	right, below := a.cols[1].tab().ed, a.cols[2].tab().ed
	if right == below || right.document != below.document || below.sel != src.sel || below.vscroll.Y != src.vscroll.Y {
		t.Fatalf("the split below: shared %v, selection %v, scrolled to %v", right.document == below.document, below.sel, below.vscroll.Y)
	}
	// Its own caret and scroll, and the document's undo.
	hs.place(below, 0, 0)
	hs.scrollEditor(below, 0)
	if right.sel != [2]int{40, 44} || right.vscroll.Y != 600 {
		t.Fatalf("the split above moved with the one below: %v, %v", right.sel, right.vscroll.Y)
	}
	tt.Type("// hi\n")
	hs.settle()
	right.vscroll.Focus()
	hs.settle()
	if a.col != 1 {
		t.Fatalf("the keyboard is in column %d", a.col)
	}
	hs.key(ui.Cmd, ui.KeyZ)
	if strings.HasPrefix(src.buf.String(), "// hi") {
		t.Fatal("undone from another split, the typing stays")
	}

	// Each its own file, to find on the screen.
	a.openFileIn(a.cols[0], 0, hs.path("main.go"), false)
	a.openFileIn(a.cols[1], 0, hs.path("README.md"), false)
	a.openFileIn(a.cols[2], 0, hs.path("go.mod"), true)
	hs.settle()
	l, u, b := editorBox(t, tt, "main.go"), editorBox(t, tt, "README.md"), editorBox(t, tt, "go.mod")
	if near := func(x, y float32) bool { return x-y < 2 && y-x < 2 }; !near(u.X, b.X) || !near(u.W, b.W) || u.Y+u.H > b.Y || l.X+l.W > u.X || l.H < u.H*1.6 {
		t.Fatalf("left %v, above %v, below %v", l, u, b)
	}
	hs.shot("split-right-then-down")
	// The handle along the top of the one below moves the height.
	y := u.Y + u.H + 2
	hs.drag(u.X+u.W/2, y, u.X+u.W/2, y+60)
	u2, b2 := editorBox(t, tt, "README.md"), editorBox(t, tt, "go.mod")
	if d := u2.H - u.H; d < 50 || d > 70 || b2.H > b.H-50 {
		t.Fatalf("dragged 60: above %v, below %v, from %v and %v", u2, b2, u, b)
	}
	if l2 := editorBox(t, tt, "main.go"); l2 != l {
		t.Fatalf("the column beside moved: %v from %v", l2, l)
	}
	if w := a.window; w.Panes == nil || len(w.Panes.Kids) != 2 || !w.Panes.Kids[1].Down {
		t.Fatalf("remembered %+v", w.Panes)
	}

	// Next and previous column go through them as they read.
	a.run("view.prevColumn")
	hs.settle()
	if a.col != 1 {
		t.Fatalf("previous column: %d", a.col)
	}
	// Closing the tabs of the one below gives its room to the one above,
	// whose editor stays where it is.
	above := a.cols[1]
	a.activate(above, slices.IndexFunc(above.tabs, func(t *colTab) bool { return t.ed == right }), false)
	hs.settle()
	sel, y := right.sel, right.vscroll.Y
	a.col = 2
	for len(a.cols) == 3 {
		a.closeActive()
		hs.settle()
	}
	if shape(a) != "[AB]" || a.window.Panes != nil {
		t.Fatalf("closed below: %s, remembered %+v", shape(a), a.window.Panes)
	}
	if right.sel != sel || right.vscroll.Y != y {
		t.Fatalf("the editor above moved: %v at %v, from %v at %v", right.sel, right.vscroll.Y, sel, y)
	}
	l3, u3 := editorBox(t, tt, "main.go"), editorBox(t, tt, "long.go")
	if !(u3.H > l3.H-40) || u3.X != u.X {
		t.Fatalf("after closing: left %v, right %v", l3, u3)
	}
}

// Split Down in a column side by side with others splits it alone, and
// Split Right in a stacked column splits only it.
func TestSplitIsLocal(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.splitRight()
	a.splitRight()
	a.col = 1
	a.splitDown()
	if shape(a) != "[A{BC}D]" || a.col != 2 {
		t.Fatalf("%s, keyboard in %d", shape(a), a.col)
	}
	a.splitRight()
	if shape(a) != "[A{B[CD]}E]" || a.col != 3 {
		t.Fatalf("%s, keyboard in %d", shape(a), a.col)
	}
	a.splitDown()
	if shape(a) != "[A{B[C{DE}]}F]" {
		t.Fatalf("%s", shape(a))
	}
	// Each goes as its last tab closes, the layout tidied after it.
	for _, want := range []string{"[A{B[CD]}E]", "[A{BC}D]", "[ABC]"} {
		a.col = 3
		if len(a.cols) == 4 {
			a.col = 2
		}
		a.closeActive()
		hs.settle()
		if got := shape(a); got != want {
			t.Fatalf("closed to %s, want %s", got, want)
		}
	}
	var total float32
	for _, c := range a.cols {
		total += c.weight
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("the columns' weights add to %v", total)
	}
}

// Quick Open to the side, a locked column's file workspace and a terminal
// moved to the locked right column find their columns in a layout of
// both.
func TestSplitTargets(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.splitRight()
	a.splitDown()
	// From the one below, the side is the column above it.
	a.openFileToSide(hs.path("go.mod"))
	hs.settle()
	if shape(a) != "[A{BC}]" || a.col != 1 || a.column().tab().path() != hs.path("go.mod") {
		t.Fatalf("to the side: %s, keyboard in %d", shape(a), a.col)
	}
	// All locked, files get a workspace left of them all.
	for _, c := range slices.Clone(a.cols) {
		a.toggleColumnLock(c)
	}
	if shape(a) != "[AB{CD}]" || a.cols[0].locked || len(a.cols[0].tabs) != 0 {
		t.Fatalf("locked: %s", shape(a))
	}
	// A terminal moved to the locked right goes right of the stack, the
	// height of the editor area.
	s := &session{}
	a.terms = append(a.terms, s)
	a.terminalTab(a.cols[0], 0, s, false)
	a.terminalToLockedRight(s)
	if shape(a) != "[AB{CD}E]" || !a.cols[4].locked || a.cols[4].tab().term != s {
		t.Fatalf("terminal to the right: %s", shape(a))
	}
}

// Columns stacked come back so after a restart, from the settings' JSON,
// a layout an older version kept as a row as it was, and one that does not
// name each column once as a row.
func TestRestoresStackedColumns(t *testing.T) {
	root := workspaceFixture(t)
	file := func(name string) string { return filepath.Join(root, name) }
	cols := []settings.Column{
		{Tabs: []settings.Tab{{File: file("main.go")}}, Active: 0, Weight: 1},
		{Tabs: []settings.Tab{{File: file("README.md")}}, Active: 0, Weight: 2},
		{Tabs: []settings.Tab{{File: file("go.mod")}}, Active: 0, Weight: 1},
	}
	stack := &settings.Pane{Column: -1, Kids: []settings.Pane{
		{Column: 0, Weight: 1},
		{Column: -1, Down: true, Weight: 1, Kids: []settings.Pane{{Column: 1, Weight: 2}, {Column: 2, Weight: 1}}},
	}}
	restore := func(t *testing.T, panes *settings.Pane) (*app, *ui.Tester) {
		s := settings.Default()
		w := s.NewWindow()
		w.Folder = root
		w.Columns, w.Panes, w.Column = cols, panes, 2
		// Through the file the settings are kept in.
		data, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		var back settings.Window
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		a := newApp(&fakeHost{folder: root}, s, &back)
		t.Cleanup(a.shutdown)
		tt := ui.NewTester(a.view, 1200, 760)
		step(tt)
		step(tt)
		return a, tt
	}
	t.Run("stacked", func(t *testing.T) {
		a, tt := restore(t, stack)
		if shape(a) != "[A{BC}]" || a.col != 2 {
			t.Fatalf("restored %s, keyboard in %d", shape(a), a.col)
		}
		// The files are read in the background, a slow runner's after the
		// frames restore took.
		for deadline := time.Now().Add(settleTimeout); time.Now().Before(deadline); {
			_, up := tt.Find("Editor README.md")
			_, down := tt.Find("Editor go.mod")
			if up && down {
				break
			}
			time.Sleep(5 * time.Millisecond)
			step(tt)
		}
		u, b := editorBox(t, tt, "README.md"), editorBox(t, tt, "go.mod")
		if u.H < b.H*1.6 || u.Y > b.Y {
			t.Fatalf("above %v, below %v: the weights went", u, b)
		}
		again, _ := json.Marshal(a.window.Panes)
		root := *stack
		root.Weight = 1
		want, _ := json.Marshal(root)
		if string(again) != string(want) {
			t.Fatalf("remembered %s, want %s", again, want)
		}
	})
	t.Run("row", func(t *testing.T) {
		a, _ := restore(t, nil)
		if shape(a) != "[ABC]" || a.window.Panes != nil {
			t.Fatalf("restored %s", shape(a))
		}
	})
	t.Run("broken", func(t *testing.T) {
		a, _ := restore(t, &settings.Pane{Column: -1, Kids: []settings.Pane{{Column: 0}, {Column: 0}, {Column: 2}}})
		if shape(a) != "[ABC]" {
			t.Fatalf("restored %s", shape(a))
		}
	})
	t.Run("gone", func(t *testing.T) {
		// A column whose file is gone goes, and its stack with it.
		gone := slices.Clone(cols)
		gone[2].Tabs = []settings.Tab{{File: file("gone.go")}}
		s := settings.Default()
		w := s.NewWindow()
		w.Folder, w.Columns, w.Panes = root, gone, stack
		a := newApp(&fakeHost{folder: root}, s, w)
		t.Cleanup(a.shutdown)
		tt := ui.NewTester(a.view, 1200, 760)
		step(tt)
		step(tt)
		if shape(a) != "[AB]" || a.window.Panes != nil {
			t.Fatalf("restored %s", shape(a))
		}
	})
}

// A terminal splits down into a new one in its folder, and it goes on as
// it was; with the panel's terminal focused, the column that had the
// keyboard splits.
func TestSplitDownTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("view.terminalTab")
	hs.settle()
	s := a.column().tab().term
	if s == nil {
		t.Fatal("no terminal tab")
	}
	a.run("view.splitDown")
	hs.settle()
	ns := a.column().tab().term
	if shape(a) != "[{AB}]" || ns == nil || ns == s || ns.dir != s.dir || len(a.terms) != 2 || a.cols[0].tab().term != s {
		t.Fatalf("%s, %d terminals", shape(a), len(a.terms))
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(ns.term.Text(), "$") && !strings.Contains(ns.term.Text(), "%") && time.Now().Before(deadline) {
		hs.settle()
	}
	// The panel's terminal has the keyboard: the column splits.
	a.run("view.terminal")
	hs.settle()
	if a.focus != paneTerminal || a.panelTerm == nil {
		t.Fatalf("the panel: focus %v", a.focus)
	}
	panel := a.panelTerm
	a.run("view.splitRight")
	hs.settle()
	if shape(a) != "[{A[BC]}]" || a.panelTerm != panel || a.column().tab().term == nil || a.focus != paneEditor {
		t.Fatalf("from the panel: %s, focus %v", shape(a), a.focus)
	}
	a.closeTerminals()
}
