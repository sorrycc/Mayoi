package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// wrapFile is a Go file with a long comment line among short ones.
func wrapFile(n int) string {
	var b strings.Builder
	b.WriteString("package p\n\n// " + strings.Repeat("lorem ipsum dolor ", 40) + "end\n")
	for i := range n {
		fmt.Fprintf(&b, "func f%d() int {\n\treturn %d\n}\n", i, i)
	}
	return b.String()
}

func TestWordWrapToggles(t *testing.T) {
	hs, d := editHarness(t, "w.go", wrapFile(10))
	a := hs.a
	if a.wraps(d) {
		t.Fatal("an editor wraps before Word Wrap is on")
	}
	a.run("view.wordWrap")
	hs.settle()
	if !a.settings.WordWrap || !a.wraps(d) {
		t.Fatalf("Word Wrap on: settings %v, the editor wraps %v", a.settings.WordWrap, a.wraps(d))
	}
	if d.hscroll.MaxX != 0 {
		t.Errorf("a wrapping editor scrolls sideways, as far as %v", d.hscroll.MaxX)
	}
	if rows := d.lines.Rows(2); len(rows) < 2 {
		t.Fatalf("the long line takes rows %v", rows)
	}

	// The tab's own choice, which the next change of the setting undoes.
	a.run("view.wordWrapTab")
	hs.settle()
	if !a.settings.WordWrap || a.wraps(d) {
		t.Fatalf("the tab still wraps (%v), or the setting changed (%v)", a.wraps(d), a.settings.WordWrap)
	}
	if d.hscroll.MaxX <= 0 {
		t.Errorf("the tab that stopped wrapping does not scroll sideways")
	}
	// A column split from it makes the same choice.
	a.run("view.splitRight")
	hs.settle()
	if right := a.cols[1].tab().ed; a.wraps(right) {
		t.Error("the split of a tab that does not wrap wraps")
	}
	a.run("view.wordWrap")
	a.run("view.wordWrap")
	hs.settle()
	if !a.wraps(d) || !a.wraps(a.cols[1].tab().ed) {
		t.Error("the tabs do not follow the setting after it changed")
	}
	if prefs, _ := a.settings.Encode(); !strings.Contains(string(prefs), `"wordWrap": true`) {
		t.Error("the setting is not saved")
	}
}

func TestWrappedLinesPlaced(t *testing.T) {
	hs, d := editHarness(t, "w.go", wrapFile(10))
	a := hs.a
	a.run("view.wordWrap")
	hs.settle()
	f := a.font
	snap := d.buf.Snapshot()
	g := a.geometry(d, snap.LineCount())
	rows := d.lines.Rows(2)
	if len(rows) < 2 {
		t.Fatalf("the long line takes rows %v", rows)
	}
	// The line after it is below all its rows.
	if got, want := g.top(3)-g.top(2), float32(len(rows))*f.lineH; got != want {
		t.Errorf("the long line is %v high, want %v for %d rows", got, want, len(rows))
	}
	if l := g.at(g.top(2) + f.lineH*1.5); l != 2 {
		t.Errorf("the second row is of line %d", l)
	}

	// A run along the line lands on the row of its runes.
	wr := a.rowsOf(d, snap, 0, 5, 0)
	xs := wr.rowStarts(2)
	if len(xs) != len(rows) || xs[0] != 0 || xs[1] != float32(rows[1])*f.charW {
		t.Fatalf("rows start at %v, want at the cells of runes %v", xs, rows)
	}
	var got [][3]float32
	x0 := xs[1] + 2*f.charW
	wr.pieces(2, x0, x0+3*f.charW, func(row int, x0, x1 float32) { got = append(got, [3]float32{float32(row), x0, x1}) })
	if len(got) != 1 || got[0] != [3]float32{1, 2 * f.charW, 5 * f.charW} {
		t.Errorf("a run on the second row is placed at %v", got)
	}
	// One across the break, in two.
	got = got[:0]
	wr.pieces(2, xs[1]-f.charW, xs[1]+f.charW, func(row int, x0, x1 float32) { got = append(got, [3]float32{float32(row), x0, x1}) })
	if len(got) != 2 || got[0][0] != 0 || got[1] != [3]float32{1, 0, f.charW} {
		t.Errorf("a run across the break is placed at %v", got)
	}

	// The selection across the long line has a box on each row.
	s, e := snap.LineRange(2)
	hs.place(d, s+1, e)
	sh := a.selection(d, d.buf.Snapshot(), 0, 5)
	boxes := sh.rowBoxes(func(i int, x0, x1 float32, fn func(y, x0, x1 float32)) {
		wr.pieces(i, x0, x1, func(row int, x0, x1 float32) { fn(float32(row)*f.lineH, x0, x1) })
	})
	n := len(boxes)
	if n != len(rows) {
		t.Errorf("the selection of the long line has %d boxes, want %d", n, len(rows))
	}
	hs.shot("13-word-wrap")
	// The other occurrences of the word at the caret, on every row.
	hs.place(d, s+30, s+30)
	if dc := a.decorate(d, d.buf.Snapshot(), 0, 5); len(dc.words) < 20 {
		t.Errorf("%d occurrences of the word at the caret", len(dc.words))
	}
	hs.shot("14-word-wrap-words")
}

func TestWrappedCaretStaysInView(t *testing.T) {
	// Long lines all through, so that the rows of the last ones are
	// estimated until the view reaches them.
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "// %d %s\n", i, strings.Repeat("wrap me please ", 30))
	}
	b.WriteString("last")
	hs, d := editHarness(t, "w.txt", b.String())
	a := hs.a
	a.run("view.wordWrap")
	hs.settle()
	// To the end: ⌘↓ on macOS, Ctrl+End elsewhere.
	if ui.Cmd == ui.Super {
		hs.key(ui.Super, ui.KeyDown)
	} else {
		hs.key(ui.Ctrl, ui.KeyEnd)
	}
	if d.sel[1] != d.buf.Snapshot().Len() {
		t.Fatalf("the caret is at %d, not the end", d.sel[1])
	}
	g := a.geometry(d, d.buf.Snapshot().LineCount())
	if y := editorPadY + g.top(300) - d.vscroll.Y; y < 0 || y > 700 {
		t.Fatalf("the last line is at %v in the view", y)
	}
	// A line far down, gone to, shows.
	hs.key(ui.Cmd, ui.KeyUp)
	a.goToLine(200, 0)
	hs.settle()
	g = a.geometry(d, d.buf.Snapshot().LineCount())
	if y := editorPadY + g.top(199) - d.vscroll.Y; y < 0 || y > 760 {
		t.Errorf("line 200 is at %v in the view", y)
	}
}

func TestWrapKeepsPlace(t *testing.T) {
	hs, d := editHarness(t, "w.go", wrapFile(400))
	a := hs.a
	d.vscroll.Y = 3000
	hs.settle()
	g := a.geometry(d, d.buf.Snapshot().LineCount())
	line := g.at(d.vscroll.Y - editorPadY)
	a.run("view.wordWrap")
	hs.settle()
	g = a.geometry(d, d.buf.Snapshot().LineCount())
	if got := g.at(d.vscroll.Y - editorPadY); got < line-1 || got > line+1 {
		t.Errorf("the view showed line %d, and after wrapping %d", line, got)
	}
}

func TestWrappedHiddenTabKeepsPlace(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.WordWrap = true
	p := hs.path("w.go")
	if err := os.WriteFile(p, []byte(wrapFile(400)), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(p, true)
	hs.settle()
	d := a.activeEditor()
	// The area measures the rows above as it shows them: it ends where
	// the line it reached is.
	hs.scrollEditor(d, 6000)
	at := d.vscroll.Y
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	a.activate(a.column(), 0, true)
	hs.settle()
	if d.vscroll.Y != at {
		t.Errorf("left at %v, back at %v", at, d.vscroll.Y)
	}
}
