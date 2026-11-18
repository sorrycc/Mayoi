package main

import (
	"os"
	"strings"
	"testing"
)

func TestTrimEdits(t *testing.T) {
	apply := func(text string, md bool) string {
		r := []rune(text)
		edits := trimEdits(text, md, nil)
		sortEdits(edits)
		for _, e := range edits {
			r = append(r[:e.start:e.start], append([]rune(e.text), r[e.end:]...)...)
		}
		return string(r)
	}
	for _, c := range []struct {
		in, want string
		md       bool
	}{
		{"", "", false},
		{"clean\ntext", "clean\ntext", false},
		{"a \t\nb\t\n  \nc   ", "a\nb\n\nc", false},
		{"héllo 世界  \nnext ", "héllo 世界\nnext", false},
		{"lone\r \n", "lone\r\n", false},
		// Markdown keeps two or more spaces ending a line of text.
		{"break  \nthree   \none \ntab\t\n  \nlast  ", "break  \nthree   \none\ntab\n\nlast  ", true},
		{"break  \n", "break\n", false},
	} {
		if got := apply(c.in, c.md); got != c.want {
			t.Errorf("trim(%q, md %v) = %q, want %q", c.in, c.md, got, c.want)
		}
	}
	if e := trimEdits("a \nb \nc ", false, func(l int) bool { return l == 1 }); len(e) != 2 || e[1].start != 7 {
		t.Errorf("kept line 1: %v", e)
	}
}

// trimHarness opens a file of text, whose saves trim when on.
func trimHarness(t *testing.T, name, text string, on bool) (*harness, *editor) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.AutoSave = false
	a.settings.TrimTrailingWhitespace = on
	if err := os.WriteFile(hs.path(name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(hs.path(name), true)
	hs.settle()
	return hs, a.activeEditor()
}

func placeCaret(d *editor, at int) {
	d.selectRange(at, at, false)
	d.sel = [2]int{at, at}
}

func saveAndRead(t *testing.T, hs *harness, d *editor) string {
	t.Helper()
	saved := false
	hs.a.saveDoc(d.document, func() { saved = true })
	hs.until("the save", func() bool { return saved })
	data, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTrimTrailingWhitespaceOnSave(t *testing.T) {
	const text = "one  \ntwo\t\nthree \n"
	hs, d := trimHarness(t, "off.txt", text, false)
	d.edit(d.sel, textEdit{0, 0, "x"})
	if got := saveAndRead(t, hs, d); got != "x"+text {
		t.Fatalf("off: saved %q", got)
	}

	hs, d = trimHarness(t, "on.txt", text, true)
	d.edit(d.sel, textEdit{0, 0, "x"})
	// The caret after "thr" of line three stays after it.
	at := strings.Index("x"+text, "ree")
	placeCaret(d, at)
	steps := len(d.hist.undo)
	if got := saveAndRead(t, hs, d); got != "xone\ntwo\nthree\n" {
		t.Fatalf("on: saved %q", got)
	}
	if d.dirty() {
		t.Error("dirty after the trimmed save")
	}
	if want := at - 3; d.sel != [2]int{want, want} {
		t.Errorf("caret at %v, want %d", d.sel, want)
	}
	if len(d.hist.undo) != steps+1 {
		t.Errorf("%d steps of undo, want %d", len(d.hist.undo), steps+1)
	}
	// One undo brings the whitespace back, and the caret to where it was.
	d.undo()
	if got := d.buf.String(); got != "x"+text || d.sel[1] != at {
		t.Errorf("undone: %q, caret %v", got, d.sel)
	}
	// A clean text saves again without an edit.
	d.redo()
	v := d.buf.Version()
	saveAndRead(t, hs, d)
	if d.buf.Version() != v {
		t.Error("saving a trimmed text edited it")
	}
}

func TestTrimTrailingWhitespaceKeepsCRLF(t *testing.T) {
	hs, d := trimHarness(t, "crlf.txt", "one \r\ntwo\t\r\nthree\r\n", true)
	d.edit(d.sel, textEdit{0, 0, "x"})
	if got := saveAndRead(t, hs, d); got != "xone\r\ntwo\r\nthree\r\n" {
		t.Fatalf("saved %q", got)
	}
}

func TestTrimTrailingWhitespaceMarkdown(t *testing.T) {
	hs, d := trimHarness(t, "notes.md", "a line  \nanother \n   \ntab\t\n", true)
	d.edit(d.sel, textEdit{0, 0, "x"})
	if got := saveAndRead(t, hs, d); got != "xa line  \nanother\n\ntab\n" {
		t.Fatalf("saved %q", got)
	}
}

// Auto Save leaves the line of the caret, where typing may go on.
func TestTrimTrailingWhitespaceAutoSaveKeepsCaretLine(t *testing.T) {
	hs, d := trimHarness(t, "auto.txt", "one \ntwo\n", true)
	d.edit([2]int{8, 8}, textEdit{8, 8, " "})
	hs.a.requestSave(d.document, saveWaiter{auto: true})
	hs.settle()
	if data, _ := os.ReadFile(d.path); string(data) != "one\ntwo \n" {
		t.Fatalf("saved %q", data)
	}
}

func TestTrimTrailingWhitespaceCommand(t *testing.T) {
	hs, d := trimHarness(t, "cmd.txt", "one \ntwo  \n", false)
	a := hs.a
	placeCaret(d, 9)
	a.run("sel.trimWhitespace")
	hs.settle()
	if got := d.buf.String(); got != "one\ntwo\n" || d.sel != [2]int{7, 7} {
		t.Fatalf("trimmed %q, caret %v", got, d.sel)
	}
	// What it did is told; with nothing to trim, so is that.
	d.edit(d.sel, textEdit{0, 0, "x \n"})
	a.trimActive(d)
	if n := len(a.notes); n != 1 || a.notes[0].message != "Trimmed trailing whitespace from 1 line" {
		t.Fatalf("notes %v", a.notes)
	}
	a.trimActive(d)
	if n := len(a.notes); n != 2 || a.notes[1].message != "No trailing whitespace to trim." {
		t.Fatalf("notes %v", a.notes)
	}
}
