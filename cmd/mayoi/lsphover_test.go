package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The fake server's document: "late" at 4, "one" at 9, "latenothing" at
// 13, "latebroken" at 25, "nothing" at 36 and "broken" at 44.
const lateText = "use late one\nlatenothing\nlatebroken\nnothing broken\n"

// openLate opens lateText as a.fake, its server up, and returns its editor.
func openLate(hs *harness) *editor {
	path := hs.path("a.fake")
	os.WriteFile(path, []byte(lateText), 0o644)
	hs.a.openFile(path, true)
	d := hs.a.activeEditor()
	serverUp(hs)
	return d
}

// told reports whether a toast says msg.
func told(hs *harness, msg string) bool { return hs.tt.HasText(msg) }

// Only the hover last asked for, at the caret, text and view it was asked
// at, shows: an older answer, or one after the caret moved, the text
// changed or another file opened, shows no card and says nothing, empty or
// failed as it may be.
func TestHoverAnswerTooLate(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	quiet := func(what string) {
		t.Helper()
		if a.hover != nil || told(hs, "Nothing to show here") {
			t.Fatalf("%s: card %v, %q", what, a.hover != nil, hs.tt.Texts())
		}
	}

	// Asked of "late", then of "one", whose card shows first.
	hs.place(d, 5, 5)
	a.showHover()
	holdLate(hs)
	hs.place(d, 10, 10)
	a.showHover()
	hs.until("the newer hover", func() bool { return a.hover != nil })
	card := a.hover
	releaseLate(hs)
	if a.hover != card {
		t.Fatal("an older hover replaced the newer one")
	}
	a.hover = nil

	// The caret moved on.
	hs.place(d, 5, 5)
	a.showHover()
	holdLate(hs)
	hs.place(d, 1, 1)
	releaseLate(hs)
	quiet("after the caret moved")

	// The text changed, the caret where it was.
	hs.place(d, 5, 5)
	a.showHover()
	holdLate(hs)
	a.queueEdit(d, func(d *editor) { n := d.buf.Snapshot().Len(); d.edit(d.sel, textEdit{n, n, "x"}) })
	hs.settle()
	releaseLate(hs)
	quiet("after an edit")

	// Nothing to show, after the caret moved.
	hs.place(d, 15, 15)
	a.showHover()
	holdLate(hs)
	hs.place(d, 1, 1)
	releaseLate(hs)
	quiet("an empty hover after the caret moved")

	// A failure, in a file left.
	hs.place(d, 27, 27)
	a.showHover()
	holdLate(hs)
	other := hs.path("b.fake")
	os.WriteFile(other, []byte("plain\n"), 0o644)
	a.openFile(other, true)
	hs.settle()
	releaseLate(hs)
	quiet("a failed hover in a file left")
}

// Only the completions last asked for, at the caret, text and view they
// were asked at, are offered: an older answer neither replaces nor closes
// the newer box, and one too late opens none and says nothing, empty or
// failed as it may be. A hover asked meanwhile does not drop them.
func TestCompletionAnswerTooLate(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	quiet := func(what string) {
		t.Helper()
		if a.pluginPick.open || told(hs, "No completions here") || told(hs, "The language server isn't running") {
			t.Fatalf("%s: box %v, %q", what, a.pluginPick.open, hs.tt.Texts())
		}
	}

	// Asked at "late", then at "one", whose box opens first.
	hs.place(d, 5, 5)
	a.complete()
	holdLate(hs)
	hs.place(d, 10, 10)
	a.complete()
	hs.until("the newer completions", func() bool { return a.pluginPick.open })
	items := a.pluginPick.items
	releaseLate(hs)
	if !a.pluginPick.open || &a.pluginPick.items[0] != &items[0] {
		t.Fatal("an older answer replaced or closed the newer box")
	}
	a.closePluginPicker(-1)
	hs.settle()

	// The caret moved on.
	hs.place(d, 5, 5)
	a.complete()
	holdLate(hs)
	hs.place(d, 1, 1)
	releaseLate(hs)
	quiet("after the caret moved")

	// None, after the caret moved.
	hs.place(d, 15, 15)
	a.complete()
	holdLate(hs)
	hs.place(d, 1, 1)
	releaseLate(hs)
	quiet("empty completions after the caret moved")

	// A failure, after an edit.
	hs.place(d, 27, 27)
	a.complete()
	holdLate(hs)
	a.queueEdit(d, func(d *editor) { n := d.buf.Snapshot().Len(); d.edit(d.sel, textEdit{n, n, "x"}) })
	hs.settle()
	releaseLate(hs)
	quiet("failed completions after an edit")

	// A hover asked at the same place shows, and leaves the completions
	// to come.
	hs.place(d, 5, 5)
	a.complete()
	holdLate(hs)
	a.showHover()
	hs.until("the hover", func() bool { return a.hover != nil })
	releaseLate(hs)
	if !a.pluginPick.open || a.pluginPick.items[0].Label != "println" {
		t.Fatalf("after a hover, completions %v", a.pluginPick.open)
	}
	a.closePluginPicker(-1)
	a.hover = nil
	hs.settle()

	// The workspace switched.
	key := a.root + "\x00fake.server"
	other := workspaceFixture(t)
	// Its server ends before the folder is removed, as Windows asks.
	t.Cleanup(stopServers)
	hs.place(d, 5, 5)
	a.complete()
	holdLate(hs)
	a.switchFolder(other)
	hs.settle()
	if _, err := serverOf(key).live().Call(context.Background(), "test/release", nil); err != nil {
		t.Fatal(err)
	}
	hs.held--
	hs.settle()
	quiet("after the workspace switched")
}

// A server restarted while it is asked for a hover or completions shows
// none of what the old one answers, nor says it had none.
func TestHoverAndCompletionAcrossRestart(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)
	key := a.root + "\x00fake.server"
	for _, ask := range []func(){a.showHover, a.complete} {
		hs.place(d, 5, 5)
		ask()
		holdLate(hs)
		a.restartServer(key)
		hs.held--
		serverUp(hs)
		hs.settle()
		if a.hover != nil || a.pluginPick.open || told(hs, "Nothing to show here") || told(hs, "No completions here") ||
			told(hs, "The language server isn't running") {
			t.Fatalf("after a restart: card %v, box %v, %q", a.hover != nil, a.pluginPick.open, hs.tt.Texts())
		}
	}
}

// Answers asked for and still wanted say what they are: none, a failure,
// or a server that stopped meanwhile.
func TestHoverAndCompletionCurrentAnswers(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openLate(hs)

	hs.place(d, 38, 38)
	a.showHover()
	hs.until("no hover told", func() bool { return told(hs, "Nothing to show here") })
	hs.place(d, 46, 46)
	a.showHover()
	hs.settle()
	if a.hover != nil {
		t.Fatal("a failed hover showed a card")
	}

	hs.place(d, 38, 38)
	a.complete()
	hs.until("no completions told", func() bool { return told(hs, "No completions here") })
	if a.pluginPick.open {
		t.Fatal("empty completions opened a box")
	}

	// The server stops while it is asked.
	hs.place(d, 5, 5)
	a.complete()
	holdLate(hs)
	c := serverOf(a.root + "\x00fake.server").live()
	c.Close()
	hs.held--
	hs.until("the stopped server told", func() bool { return told(hs, "The language server isn't running") })
	if a.pluginPick.open {
		t.Fatal("a stopped server opened a box")
	}
}

// The fake server's words that answer as their names say: "plain" at 0,
// "marked" at 6, "long" at 13; "one" at 18 answers **hello**.
const hoverText = "plain marked long one\n"

// openHover opens text as a.fake, its server up, and returns its editor.
func openHover(hs *harness, name, text string) *editor {
	path := hs.path(name)
	os.WriteFile(path, []byte(text), 0o644)
	hs.a.openFile(path, true)
	d := hs.a.activeEditor()
	serverUp(hs)
	return d
}

// hoverAt asks for the hover at the caret at, and waits for it to show.
func hoverAt(hs *harness, d *editor, at int) *hoverCard {
	hs.t.Helper()
	hs.place(d, at, at)
	hs.a.hover = nil
	hs.a.showHover()
	hs.until("the hover", func() bool { return hs.a.hover != nil && hs.a.hover.rect.W > 0 })
	hs.settle()
	return hs.a.hover
}

// A hover shows by its word: a signature in a language first, in the
// code's colors, its documentation under it; plaintext as it is, its
// marks and lines kept; long documentation scrolls in a card no higher
// than hoverMaxH.
func TestHoverCardContents(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openHover(hs, "a.fake", hoverText)

	h := hoverAt(hs, d, 9)
	if h.at != 6 {
		t.Fatalf("anchored at %d, not the start of its word, 6", h.at)
	}
	if len(h.sig) != 1 || h.sig[0].language != "go" || len(h.docs) != 1 {
		t.Fatalf("signature %d blocks, docs %d", len(h.sig), len(h.docs))
	}
	colored := false
	for _, r := range h.sig[0].runs {
		colored = colored || a.pal.syntax[r.kind].A > 0 && r.text == "func"
	}
	if !colored || !hs.tt.HasText("Returns") {
		t.Fatalf("signature colored %v, texts %q", colored, hs.tt.Texts())
	}
	at, _ := a.hoverAnchor(h)
	if h.rect.Y < at.Y+at.H || h.rect.X-at.X > 1 || at.X-h.rect.X > 1 {
		t.Fatalf("card %+v by its word at %+v: on the first line, it goes under it, at its start", h.rect, at)
	}

	h = hoverAt(hs, d, 2)
	if len(h.sig) != 0 || !hs.tt.HasText("*not* bold") || !hs.tt.HasText("next line") {
		t.Fatalf("plaintext: %q", hs.tt.Texts())
	}

	h = hoverAt(hs, d, 14)
	if h.rect.H > hoverMaxH+0.5 || h.rect.H < hoverMaxH/2 {
		t.Fatalf("long card %v high", h.rect.H)
	}
	y := d.vscroll.Y
	hs.tt.Wheel(h.rect.X+20, h.rect.Y+h.rect.H/2, 0, 200)
	hs.settle()
	if a.hover != h || d.vscroll.Y != y {
		t.Fatalf("scrolling the card closed it (%v) or scrolled the editor (%v)", a.hover != h, d.vscroll.Y != y)
	}
}

// A plugin's hover shows as the server's does, under it when both answer.
func TestHoverOfPlugin(t *testing.T) {
	hs := newHarness(t, false)
	withPlugins(hs, map[string]map[string]string{"hov": {
		"plugin.json": `{"name": "hov", "main": "main.ts", "activation": ["onLanguage:hov"],
  "contributes": {"languages": [{"id": "hov", "extensions": [".hov"]}]}}`,
		"main.ts": `import { languages } from "mayoi";
languages.registerHover("hov", () => "From **plugin**");`,
	}})
	a := hs.a
	path := hs.path("a.hov")
	os.WriteFile(path, []byte("word\n"), 0o644)
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("the plugin's hover", func() bool {
		if a.hover == nil {
			hs.place(d, 1, 1)
			a.showHover()
			hs.settle()
		}
		return a.hover != nil
	})
	if a.hover.at != 1 || len(a.hover.sig) != 0 || !hs.tt.HasText("From ") {
		t.Fatalf("plugin hover at %d: %q", a.hover.at, hs.tt.Texts())
	}
}

// A hover goes with its caret: Escape, in Vim's insert mode too, which it
// leaves; a click outside it, which goes on to the editor; a key that
// moves the caret; typing; and another editor.
func TestHoverCloses(t *testing.T) {
	hs := withFakeServer(t)
	a := hs.a
	d := openHover(hs, "a.fake", hoverText+"second line\n")

	hoverAt(hs, d, 19)
	hs.key(0, ui.KeyEscape)
	if a.hover != nil {
		t.Fatal("Escape left it")
	}

	a.settings.Vim = true
	d.vim.mode = vimInsert
	hoverAt(hs, d, 19)
	hs.key(0, ui.KeyEscape)
	if a.hover != nil || d.vim.mode != vimInsert {
		t.Fatalf("Escape in insert mode: card %v, mode %v", a.hover != nil, d.vim.mode)
	}
	hs.key(0, ui.KeyEscape)
	a.settings.Vim = false
	hs.settle()

	h := hoverAt(hs, d, 19)
	v := d.lastView
	hs.tt.ClickAt(v.X+d.textX+3*a.font.charW, v.Y+editorPadY+1.5*a.font.lineH)
	hs.settle()
	if a.hover != nil || d.sel[1] <= len(hoverText) {
		t.Fatalf("a click outside: card %v, caret %v", a.hover == h, d.sel)
	}

	hoverAt(hs, d, 19)
	hs.key(0, ui.KeyRight)
	if a.hover != nil {
		t.Fatal("moving the caret left it")
	}

	hoverAt(hs, d, 19)
	hs.insert("x")
	hs.settle()
	if a.hover != nil {
		t.Fatal("typing left it")
	}

	hoverAt(hs, d, 19)
	other := hs.path("b.fake")
	os.WriteFile(other, []byte("plain\n"), 0o644)
	a.openFile(other, true)
	hs.settle()
	if a.hover != nil {
		t.Fatal("another editor left it")
	}
}

// A card stays in its editor: under a line at the top, above one lower
// down, short of the right edge, and in its column of a split. On a
// wrapped line it is by where the word shows, not where the line starts.
func TestHoverCardPlace(t *testing.T) {
	view := ui.Rect{X: 100, Y: 50, W: 600, H: 400}
	line := func(x, y float32) ui.Rect { return ui.Rect{X: x, Y: y, W: 1, H: 18} }
	for _, c := range []struct {
		name  string
		at    ui.Rect
		cardW float32
		x     float32
		above bool
	}{
		{"top", line(200, 60), 300, 200, false},
		{"bottom", line(200, 400), 300, 200, true},
		{"middle, room above", line(200, 260), 300, 200, true},
		{"right edge", line(680, 300), 300, 700 - hoverMargin - 300, true},
		{"left of the view", line(90, 300), 300, 100 + hoverMargin, true},
	} {
		x, w, edge, maxH, above := hoverPlace(c.at, view, 1200, c.cardW)
		if x != c.x || above != c.above || w != hoverMaxW || maxH <= 0 || maxH > hoverMaxH {
			t.Errorf("%s: x %v above %v w %v maxH %v, want x %v above %v", c.name, x, above, w, maxH, c.x, c.above)
		}
		if above && (edge != c.at.Y-hoverGap || edge-maxH < view.Y+hoverMargin-0.5) ||
			!above && (edge != c.at.Y+c.at.H+hoverGap || edge+maxH > view.Y+view.H-hoverMargin+0.5) {
			t.Errorf("%s: edge %v, maxH %v leaves the view", c.name, edge, maxH)
		}
	}
	// An editor narrower than a card: the card keeps to the window.
	if x, w, _, _, _ := hoverPlace(line(830, 300), ui.Rect{X: 800, Y: 50, W: 120, H: 400}, 1000, 0); w != 240 || x+w > 1000-hoverMargin {
		t.Errorf("narrow editor: x %v w %v", x, w)
	}

	hs := withFakeServer(t)
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	a.splitRight()
	long := strings.Repeat("word ", 40) + "marked\n"
	d := openHover(hs, "a.fake", long)
	h := hoverAt(hs, d, len(long)-2)
	v := d.lastView
	if v.X < 50 || h.rect.X < v.X || h.rect.X+h.rect.W > v.X+v.W+0.5 {
		t.Fatalf("card %+v out of its column %+v", h.rect, v)
	}

	a.toggleWordWrap()
	hs.settle()
	start := len(long) - 7
	h = hoverAt(hs, d, start)
	at, ok := a.hoverAnchor(h)
	caret, caretOK := hs.tt.TextCaret()
	if !ok || !caretOK || at.Y < v.Y+a.font.lineH || abs32(at.Y-caret.Y) > 1 || abs32(at.X-caret.X) > 1 {
		t.Fatalf("wrapped: anchor %+v %v, caret %+v %v", at, ok, caret, caretOK)
	}
	if h.rect.X < v.X || h.rect.X+h.rect.W > v.X+v.W+0.5 || h.rect.Y < at.Y+at.H && h.rect.Y+h.rect.H > at.Y {
		t.Fatalf("wrapped: card %+v over its line %+v", h.rect, at)
	}
}
