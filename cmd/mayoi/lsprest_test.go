package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// restAt is where, in the window, the pointer rests on rune at of d: a
// little into its glyph, halfway down its row.
func restAt(t testing.TB, hs *harness, d *editor, at int) (float32, float32) {
	t.Helper()
	r, ok := hs.a.hoverAnchor(&hoverCard{ed: d, at: at})
	if !ok {
		t.Fatalf("rune %d is not in view", at)
	}
	return r.X + 2, r.Y + r.H/2
}

// pastEnd is where, in the window, the pointer is past the end of the
// line of d that rune at ends.
func pastEnd(t testing.TB, hs *harness, d *editor, at int) (float32, float32) {
	t.Helper()
	x, y := restAt(t, hs, d, at)
	return x + 40, y
}

// rest moves the pointer to (x, y) and leaves it there hoverDwell, for the
// card to be asked for, and the answer to come.
func rest(hs *harness, x, y float32) {
	hs.t.Helper()
	hs.tt.Move(x, y)
	step(hs.tt)
	hs.tt.Advance(hoverDwell)
	hs.settle()
}

// restOn rests the pointer on rune at of d, as rest does.
func restOn(t testing.TB, hs *harness, d *editor, at int) {
	t.Helper()
	x, y := restAt(t, hs, d, at)
	rest(hs, x, y)
}

// The pointer resting on a name hoverDwell asks for its card once, where
// it rests, the caret staying: moving within the name asks no more, and
// moving to another before the pause asks of that one alone.
func TestRestHoverDwell(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	hs.place(d, 1, 1)
	asked := a.restAsked

	x, y := restAt(t, hs, d, 10) // "one"
	hs.tt.Move(x, y)
	step(hs.tt)
	hs.tt.Advance(hoverDwell - 60*time.Millisecond)
	if a.restAsked != asked || a.hover != nil {
		t.Fatal("asked before the pointer rested")
	}
	x2, _ := restAt(t, hs, d, 11)
	hs.tt.Move(x2, y)
	step(hs.tt)
	hs.tt.Advance(60 * time.Millisecond)
	hs.settle()
	if a.restAsked != asked+1 {
		t.Fatalf("asked %d times, want once", a.restAsked-asked)
	}
	h := a.hover
	if h == nil || !h.auto || h.at != 10 {
		t.Fatalf("card %v, want one of the pointer at 10", h != nil)
	}
	if d.sel != [2]int{1, 1} {
		t.Fatalf("the caret moved to %v", d.sel)
	}
	hs.tt.Advance(3 * hoverDwell)
	hs.settle()
	if a.restAsked != asked+1 || a.hover != h {
		t.Fatal("resting on asked again")
	}

	// To "use", which drops the card, then back to "one" before the pause.
	ux, uy := restAt(t, hs, d, 1)
	hs.tt.Move(ux, uy)
	step(hs.tt)
	if a.hover != nil {
		t.Fatal("the card stayed as the pointer went to another name")
	}
	hs.tt.Advance(hoverDwell / 2)
	hs.tt.Move(x, y)
	step(hs.tt)
	hs.tt.Advance(hoverDwell / 2)
	if a.restAsked != asked+1 {
		t.Fatal("asked of a name the pointer passed")
	}
	hs.tt.Advance(hoverDwell / 2)
	hs.settle()
	if a.restAsked != asked+2 || a.hover == nil || a.hover.at != 10 {
		t.Fatalf("asked %d times, card %v", a.restAsked-asked, a.hover != nil)
	}
}

// What the pointer asked for and has no card, or failed, says nothing.
func TestRestHoverQuiet(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	for _, at := range []int{37, 45} { // "nothing", "broken"
		asked := a.restAsked
		restOn(t, hs, d, at)
		if a.restAsked != asked+1 {
			t.Fatalf("rune %d: not asked", at)
		}
		if a.hover != nil || told(hs, "Nothing to show here") || told(hs, "The language server isn't running") {
			t.Fatalf("rune %d: card %v, %q", at, a.hover != nil, hs.tt.Texts())
		}
	}
}

// An answer to the pointer that comes after it went on, as it moved off
// the name, the text changed, the editor scrolled or was left, or Show
// Hover asked at the caret, shows no card, and its question is stopped.
func TestRestHoverAnswerTooLate(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	path := hs.path("late.fake")
	os.WriteFile(path, []byte(lateText+strings.Repeat("filler line\n", 200)), 0o644)
	a.openFile(path, true)
	d := a.activeEditor()
	serverUp(hs)
	hs.settle()
	hs.place(d, 1, 1)

	ask := func() context.Context {
		t.Helper()
		// Moved, as what went on from a name asks again only then.
		x, y := restAt(t, hs, d, 5) // "late"
		hs.tt.Move(x+1, y)
		step(hs.tt)
		hs.tt.Move(x, y)
		step(hs.tt)
		hs.tt.Advance(hoverDwell)
		holdLate(hs)
		ctx := a.rest.ctx
		if ctx == nil {
			t.Fatal("not asked")
		}
		return ctx
	}
	gone := func(what string, ctx context.Context) {
		t.Helper()
		if ctx.Err() == nil {
			t.Fatalf("%s: the question goes on", what)
		}
		releaseLate(hs)
		if h := a.hover; h != nil && h.auto {
			t.Fatalf("%s: the late answer showed", what)
		}
		if told(hs, "Nothing to show here") {
			t.Fatalf("%s: told %q", what, hs.tt.Texts())
		}
	}

	ctx := ask()
	hs.tt.Move(pastEnd(t, hs, d, 12))
	step(hs.tt)
	gone("moved off the name", ctx)

	ctx = ask()
	a.queueEdit(d, func(d *editor) { n := d.buf.Snapshot().Len(); d.edit(d.sel, textEdit{n, n, "x"}) })
	hs.settle()
	gone("an edit", ctx)

	ctx = ask()
	x, y := restAt(t, hs, d, 5)
	hs.tt.Scroll(x, y, 0, 60)
	step(hs.tt)
	gone("a scroll", ctx)
	hs.scrollEditor(d, 0)
	hs.settle()

	ctx = ask()
	hs.place(d, 10, 10)
	a.showHover()
	hs.until("the card at the caret", func() bool { return a.hover != nil })
	if a.hover.auto || ctx.Err() == nil {
		t.Fatalf("Show Hover: card auto %v, question stopped %v", a.hover.auto, ctx.Err() != nil)
	}
	card := a.hover
	releaseLate(hs)
	if a.hover != card {
		t.Fatal("Show Hover: the pointer's late answer replaced its card")
	}
	hs.key(0, ui.KeyEscape)
	hs.settle()

	ctx = ask()
	other := hs.path("b.fake")
	os.WriteFile(other, []byte("plain\n"), 0o644)
	a.openFile(other, true)
	hs.settle()
	gone("another file", ctx)
}

// The card the pointer asked for stays as the pointer goes into it, to
// read and scroll it, and goes as it leaves it and the name, or on
// Escape, which asks again only as the pointer moves.
func TestRestHoverCard(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	path := hs.path("long.fake")
	os.WriteFile(path, []byte("long\n"+strings.Repeat("x\n", 200)), 0o644)
	a.openFile(path, true)
	d := a.activeEditor()
	serverUp(hs)
	hs.settle()
	hs.place(d, 4, 4)

	x, y := restAt(t, hs, d, 1)
	rest(hs, x, y)
	h := a.hover
	if h == nil || !h.auto {
		t.Fatal("no card")
	}
	// Across the gap into the card, and down it.
	r := h.rect
	hs.tt.Move(x, r.Y+2)
	step(hs.tt)
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	hs.tt.Move(cx, cy)
	hs.settle()
	if a.hover != h {
		t.Fatal("the card went as the pointer went into it")
	}
	hs.tt.Scroll(cx, cy, 0, 120)
	hs.settle()
	if a.hover != h || h.scroll.Y <= 0 || d.vscroll.Y != 0 {
		t.Fatalf("scrolled the card %v, the editor %v, card kept %v", h.scroll.Y, d.vscroll.Y, a.hover == h)
	}
	// Out of the card, onto lines past their end.
	hs.tt.Move(r.X+r.W+60, cy)
	step(hs.tt)
	if a.hover != nil {
		t.Fatal("the card stayed as the pointer left it")
	}

	// Escape closes it, and it is asked for again only as the pointer
	// moves.
	rest(hs, x, y)
	if a.hover == nil {
		t.Fatal("no card again")
	}
	asked := a.restAsked
	hs.key(0, ui.KeyEscape)
	hs.tt.Advance(3 * hoverDwell)
	hs.settle()
	if a.hover != nil || a.restAsked != asked {
		t.Fatalf("after Escape: card %v, asked %d", a.hover != nil, a.restAsked-asked)
	}
	rest(hs, x+1, y)
	if a.hover == nil {
		t.Fatal("no card after the pointer moved")
	}
}

// Pressing, dragging, a key held, the gutter, past a line's end, a file
// with no hover and the setting off ask for no card.
func TestRestHoverGestures(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	hs.place(d, 1, 1)
	none := func(what string) {
		t.Helper()
		if a.hover != nil {
			t.Fatalf("%s: a card shows", what)
		}
	}
	asked := a.restAsked
	still := func(what string) {
		t.Helper()
		none(what)
		if a.restAsked != asked {
			t.Fatalf("%s: asked %d times", what, a.restAsked-asked)
		}
	}
	x, y := restAt(t, hs, d, 10)

	// A pointer event carries the modifiers held, as the Tester's do not.
	hs.tt.Move(x, y)
	hs.tt.HoldModifiers(ui.Cmd)
	step(hs.tt)
	hs.tt.Advance(hoverDwell)
	hs.settle()
	hs.tt.HoldModifiers(0)
	hs.tt.Advance(hoverDwell)
	hs.settle()
	still("with ⌘ held")

	ux, _ := restAt(t, hs, d, 1)
	hs.drag(ux, y, x, y)
	hs.tt.Advance(hoverDwell)
	hs.settle()
	if d.sel[0] == d.sel[1] {
		t.Fatal("the drag selected nothing")
	}
	still("after a drag")

	// Moved first, past what the drag hushed, then a click that stays.
	hs.tt.Move(ux, y)
	step(hs.tt)
	hs.tt.ClickAt(x+1, y)
	hs.tt.Advance(hoverDwell)
	hs.settle()
	still("after a click")

	rest(hs, d.lastView.X+4, y)
	still("over the gutter")
	ex, ey := pastEnd(t, hs, d, 12)
	rest(hs, ex, ey)
	still("past the line's end")

	a.settings.EditorHover = false
	rest(hs, x, y+0.5)
	still("with editorHover off")
	a.settings.EditorHover = true

	plain := hs.path("plain.txt")
	os.WriteFile(plain, []byte("one two three\n"), 0o644)
	a.openFile(plain, true)
	hs.settle()
	p := a.activeEditor()
	restOn(t, hs, p, 5)
	still("in a file of no hover")
}

// The name under the pointer is the one that shows there, as the lines
// wrap, fold, scroll either way, the text zooms and the editors split.
func TestRestHoverGeometry(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	var b strings.Builder
	b.WriteString("package p\n\nfunc folded() {\n\ta := 1\n\tb := 2\n}\n\nvar after = 1\n// ")
	for i := range 120 {
		fmt.Fprintf(&b, "w%d ", i)
	}
	b.WriteString("\n")
	for i := range 200 {
		fmt.Fprintf(&b, "var v%d = %d\n", i, i)
	}
	text := b.String()
	path := hs.path("geo.go")
	os.WriteFile(path, []byte(text), 0o644)
	a.openFile(path, true)
	hs.settle()
	d := a.activeEditor()
	runes := []rune(text)
	at := func(s string, off int) int { return len([]rune(text[:strings.Index(text, s)])) + off }
	check := func(what string, d *editor, i int) {
		t.Helper()
		from, to := i, i+1
		for from > 0 && isWordRune(runes[from-1]) {
			from--
		}
		for to < len(runes) && isWordRune(runes[to]) {
			to++
		}
		hs.tt.Move(restAt(t, hs, d, i))
		step(hs.tt)
		step(hs.tt)
		if r := a.rest; r.ed != d || r.word != [2]int{from, to} {
			t.Fatalf("%s: rune %d (%q) found %v of %v, want %v", what, i, string(runes[from:to]), r.word, r.ed == d, [2]int{from, to})
		}
	}

	check("plain", d, at("after", 2))
	check("plain, the long line", d, at("w7 ", 1))

	a.toggleFoldAt(d, 2)
	hs.settle()
	check("folded above", d, at("after", 1))
	a.unfoldAllCmd(d)
	hs.settle()

	hs.place(d, at("w100 ", 0), at("w100 ", 0))
	hs.settle()
	if d.hscroll.X <= 0 {
		t.Fatal("the editor did not scroll sideways")
	}
	check("scrolled sideways", d, at("w97 ", 1))
	hs.place(d, 0, 0)
	hs.settle()

	hs.scrollEditor(d, 40*a.font.lineH)
	hs.settle()
	check("scrolled down", d, at("v50 ", 1))
	hs.scrollEditor(d, 0)
	hs.settle()

	a.settings.WordWrap = true
	a.settings.WrapGen++
	hs.settle()
	w := at("w110 ", 2)
	if g := a.geometry(d, d.buf.Snapshot().LineCount()); g.row(8, w-at("// w0", 0)) == 0 {
		t.Fatal("the long line does not wrap there")
	}
	check("wrapped", d, w)
	a.toggleFoldAt(d, 2)
	hs.settle()
	check("wrapped and folded", d, w)
	a.unfoldAllCmd(d)
	a.settings.WordWrap = false
	a.settings.WrapGen++
	hs.settle()

	a.settings.FontSize = 20
	hs.settle()
	check("zoomed", d, at("after", 3))
	a.settings.FontSize = 13
	hs.settle()

	a.splitRight()
	hs.settle()
	right := a.activeEditor()
	if right == d || right.path != d.path {
		t.Fatalf("the split shows %v", right)
	}
	check("the right editor", right, at("folded", 1))
	check("the left editor, without the keyboard", d, at("after", 0))

	// Past a line's end, and in the gutter, none.
	hs.tt.Move(pastEnd(t, hs, d, at("after = 1", 9)))
	step(hs.tt)
	if a.rest.ed != nil {
		t.Fatal("found a name past the line's end")
	}
}
