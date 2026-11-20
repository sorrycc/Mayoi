package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// ⌘K ⌘I shows the hover; pressed again it gives the card the arrows and
// the page keys, which scroll it and leave the caret. Before that they
// move the caret, which closes the card; Escape closes a focused card with
// the caret where it was, and a key that types closes it and types.
func TestHoverKeys(t *testing.T) {
	hs := withFakeServer(t)
	a, tt := hs.a, hs.tt
	d := openHover(hs, "a.fake", hoverText+"second line\n")
	chord := func() {
		tt.Key(ui.Super, ui.KeyK)
		tt.Key(ui.Super, ui.KeyI)
		hs.settle()
	}
	shown := func() *hoverCard {
		t.Helper()
		hs.until("the hover", func() bool { return a.hover != nil && a.hover.rect.W > 0 })
		hs.settle()
		return a.hover
	}

	// Not focused, Down moves the caret, which closes the card.
	hs.place(d, 14, 14)
	chord()
	h := shown()
	if h.focused {
		t.Fatal("the card took the keys at once")
	}
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if a.hover != nil || caretLine(d) != 1 {
		t.Fatalf("Down before focus: card %v, line %d", a.hover != nil, caretLine(d))
	}

	// Focused, the keys scroll the card and the caret stays.
	hs.place(d, 14, 14)
	chord()
	h = shown()
	chord()
	if a.hover != h || !h.focused {
		t.Fatalf("asked again: same card %v, focused %v", a.hover == h, h.focused)
	}
	if h.scroll.MaxY <= 0 {
		t.Fatal("the long card does not scroll")
	}
	tt.Key(0, ui.KeyDown)
	hs.settle()
	line := h.scroll.Y
	if line <= 0 || d.sel != [2]int{14, 14} || a.hover != h {
		t.Fatalf("Down: scrolled %v, caret %v, card %v", line, d.sel, a.hover == h)
	}
	tt.Key(0, ui.KeyPageDown)
	hs.settle()
	if h.scroll.Y <= line {
		t.Fatalf("Page Down scrolled to %v from %v", h.scroll.Y, line)
	}
	tt.Key(0, ui.KeyEnd)
	hs.settle()
	if h.scroll.Y != h.scroll.MaxY {
		t.Fatalf("End scrolled to %v of %v", h.scroll.Y, h.scroll.MaxY)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyUp)
	hs.settle()
	if h.scroll.Y != 0 || d.sel != [2]int{14, 14} {
		t.Fatalf("Home and Up: scrolled %v, caret %v", h.scroll.Y, d.sel)
	}
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.hover != nil || d.sel != [2]int{14, 14} {
		t.Fatalf("Escape: card %v, caret %v", a.hover != nil, d.sel)
	}

	// A key that types, while focused, types and closes the card.
	chord()
	shown()
	chord()
	tt.Type("x")
	hs.settle()
	if a.hover != nil || d.buf.String()[14] != 'x' {
		t.Fatalf("typing: card %v, text %q", a.hover != nil, d.buf.String())
	}
}

// On Windows and Linux the chord is Ctrl+K Ctrl+I.
func TestHoverChordOffMac(t *testing.T) {
	onOS(t, "linux")
	hs := withFakeServer(t)
	a, tt := hs.a, hs.tt
	d := openHover(hs, "a.fake", hoverText)
	hs.place(d, 19, 19)
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Key(ui.Ctrl, ui.KeyI)
	hs.until("the hover", func() bool { return a.hover != nil })
	if d.buf.String() != hoverText {
		t.Fatalf("the chord typed: %q", d.buf.String())
	}
}

// From the keyboard alone, on Linux, whose window runs the menus' keys
// itself: F12 offers two definitions, Down and Enter go to the second,
// Escape leaves the caret, and Go Back and Go Forward go back and forth
// between them and the file it came from. Their keys are run by command:
// MyGo's editor moves by words with Alt and the arrows on the Mac the
// tests may run on.
func TestDefinitionKeys(t *testing.T) {
	onOS(t, "linux")
	hs := withFakeServer(t)
	a, tt := hs.a, hs.tt
	from := hs.path("b.fake")
	os.WriteFile(from, []byte("start\n"), 0o644)
	a.openFile(from, true)
	hs.settle()
	path := hs.path("a.fake")
	os.WriteFile(path, []byte("use two\ndef one two\ndef two\n"), 0o644)
	a.openFileAt(path, 0, true)
	hs.settle()
	d := a.activeEditor()
	serverUp(hs)
	at := func(what, name string, line, col int) {
		t.Helper()
		hs.until(what, func() bool { n, l, c := caretAt(a); return n == name && l == line && c == col })
	}

	hs.place(d, 5, 5)
	tt.Key(0, ui.KeyF12)
	hs.until("the definitions offered", func() bool { return a.pluginPick.open })
	hs.settle()
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.pluginPick.open {
		t.Fatal("Escape left the definitions offered")
	}
	at("Escape leaves the caret", "a.fake", 0, 5)

	tt.Key(0, ui.KeyF12)
	hs.until("the definitions offered", func() bool { return a.pluginPick.open })
	hs.settle()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	at("the second definition", "a.fake", 2, 4)

	a.run("go.back")
	hs.settle()
	at("back where it asked", "a.fake", 0, 5)
	a.run("go.back")
	hs.settle()
	at("back in the file it came from", "b.fake", 0, 0)
	a.run("go.forward")
	hs.settle()
	at("forward into a.fake", "a.fake", 0, 5)
	a.run("go.forward")
	hs.settle()
	at("forward to the definition", "a.fake", 2, 4)
}

// Find References lists even a single place, and choosing it goes there,
// though the list it opened is a picker; Go Back returns.
func TestFindReferencesPicker(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	path := hs.path("a.fake")
	os.WriteFile(path, []byte("use one\ndef one\n"), 0o644)
	a.openFile(path, true)
	d := a.activeEditor()
	serverUp(hs)

	hs.place(d, 5, 5)
	a.run("go.references")
	hs.until("the references offered", func() bool { return a.pluginPick.open })
	if items := a.pluginPick.items; len(items) != 1 || items[0].Label != "a.fake:2:5" {
		t.Fatalf("references = %+v", items)
	}
	a.closePluginPicker(0)
	hs.until("the reference chosen", func() bool { n, l, c := caretAt(a); return n == "a.fake" && l == 1 && c == 4 })
	a.run("go.back")
	hs.until("back where it asked", func() bool { n, l, c := caretAt(a); return n == "a.fake" && l == 0 && c == 5 })
}

// A definition or a hover answered after a picker opened, as Quick Open,
// leaves it be: no definitions take its place, and no caret moves.
func TestLspAnswerAfterPicker(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	path := hs.path("a.fake")
	os.WriteFile(path, []byte("use late one\ndef late\ndef late\n"), 0o644)
	a.openFile(path, true)
	d := a.activeEditor()
	serverUp(hs)

	hs.place(d, 5, 5)
	a.goToDefinition()
	holdLate(hs)
	a.openQuick("")
	hs.settle()
	releaseLate(hs)
	if !a.quick.open || a.pluginPick.open || d.sel != [2]int{5, 5} {
		t.Fatalf("after Quick Open: quick %v, picker %v, caret %v", a.quick.open, a.pluginPick.open, d.sel)
	}
	a.closeQuick()
	hs.settle()

	// A plugin's picker opened meanwhile keeps its place too.
	chose := 0
	a.goToDefinition()
	holdLate(hs)
	a.openPluginPicker([]pickItem{{Label: "mine"}}, "", func(i int) { chose = i + 1 })
	hs.settle()
	releaseLate(hs)
	if !a.pluginPick.open || a.pluginPick.items[0].Label != "mine" || chose != 0 {
		t.Fatalf("the plugin's picker: open %v, items %+v, closed with %d", a.pluginPick.open, a.pluginPick.items, chose-1)
	}
	a.closePluginPicker(-1)
	hs.settle()

	// Asked after it closed, the answer goes.
	a.goToDefinition()
	holdLate(hs)
	releaseLate(hs)
	hs.until("the definitions offered", func() bool { return a.pluginPick.open && len(a.pluginPick.items) == 2 })
	a.closePluginPicker(-1)
}

// In vim's normal mode gd goes to the definition, ⌃O back to where it
// was, and K shows the hover, which K again gives its keys.
func TestVimDefinitionAndHover(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openHover(hs, "a.fake", "use one long\ndef one\n")
	a.settings.Vim = true
	hs.frames()

	hs.place(d, 5, 5)
	hs.vim("gd")
	hs.until("the definition", func() bool { _, l, c := caretAt(a); return l == 1 && c == 4 })
	hs.vim("<C-o>")
	hs.settle()
	if _, l, c := caretAt(a); l != 0 || c != 5 {
		t.Fatalf("⌃O went to %d:%d", l, c)
	}

	hs.place(d, 9, 9)
	hs.vim("K")
	hs.until("the hover", func() bool { return a.hover != nil && a.hover.rect.W > 0 })
	h := a.hover
	hs.vim("K")
	hs.settle()
	if a.hover != h || !h.focused || d.vim.mode != vimNormal {
		t.Fatalf("K again: same card %v, focused %v, mode %v", a.hover == h, h.focused, d.vim.mode)
	}
}
