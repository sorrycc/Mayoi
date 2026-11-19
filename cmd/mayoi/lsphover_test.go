package main

import (
	"context"
	"os"
	"testing"
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
