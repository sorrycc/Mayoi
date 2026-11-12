package main

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/settings"
)

// The editor places what it paints where the text area lays the text out,
// with its tab stops every tabSize cells as the settings have them.
func TestWidthFollowsTabStops(t *testing.T) {
	for _, n := range []int{2, 4, 8} {
		p := settings.Default().Prefs
		p.TabSize = n
		f := newCodeFont(p)
		if f.tabW != float32(n)*f.charW || tabWidth() != n {
			t.Fatalf("tab size %d: tabs %v wide, %d columns; a cell %v", n, f.tabW, tabWidth(), f.charW)
		}
		shaped := func(s string) float32 { w, _ := ui.ShapeText(s, f.font(), 0).Size(); return w }
		stop, cell := f.tabW, f.charW
		for s, want := range map[string]float32{
			"\t": stop, "\t\tx": 2*stop + cell, "a\tb": stop + cell, "    x": 5 * cell,
			"\tnaïve": stop + shaped("naïve"),
			"\tnaïve\t迷い": f.afterTab(stop+shaped("naïve")) + shaped("迷い"),
		} {
			if got := f.width(s); got < want-0.05 || got > want+0.05 {
				t.Errorf("tab size %d: width(%q) = %.2f, want %.2f", n, s, got, want)
			}
		}
	}
	tabCols.Store(4)
}

// The selection is a box for each line: from where it starts, to the end
// of the lines it goes on past, and none where nothing is selected.
func TestSelectionShape(t *testing.T) {
	hs, d := editHarness(t, "s.go", "package p\n\nfunc f() {\n\treturn\n}\n")
	f := hs.a.font
	snap := d.buf.Snapshot()
	at, _ := snap.LineRange(3)
	hs.place(d, 3, at+3) // from "kage p" into "\tre"
	sh := hs.a.selection(d, d.buf.Snapshot(), 0, 4)
	if sh.native || len(sh.rects) != 4 {
		t.Fatalf("shape %+v, want a box for each of 4 lines", sh.rects)
	}
	near := func(a, b float32) bool { return a > b-0.05 && a < b+0.05 }
	if r := sh.rects[0]; r.line != 0 || !near(r.x0, 3*f.charW) || !near(r.x1, 9.5*f.charW) {
		t.Errorf("first line %+v", r)
	}
	if r := sh.rects[1]; r.line != 1 || r.x0 != 0 || !near(r.x1, f.charW/2) {
		t.Errorf("empty line %+v, want the stub of its end", r)
	}
	if r := sh.rects[3]; r.line != 3 || r.x0 != 0 || !near(r.x1, f.tabW+2*f.charW) {
		t.Errorf("last line %+v", r)
	}
	hs.place(d, at, at)
	if sh := hs.a.selection(d, d.buf.Snapshot(), 0, 4); len(sh.rects) != 0 {
		t.Errorf("a caret has a shape: %+v", sh.rects)
	}
	// Text from right to left is the text area's to place.
	hs, d = editHarness(t, "r.txt", "abc שלום def\n")
	hs.place(d, 0, 8)
	if sh := hs.a.selection(d, d.buf.Snapshot(), 0, 0); !sh.native {
		t.Error("a line from right to left is not left to the text area")
	}
}

// The colors given to the text area are as few as color the same runes.
func TestColorRangesMerge(t *testing.T) {
	p := darkPal
	src := "x := 1 + 2 // a b\n"
	hl := highlight.New(highlight.Lexer("a.go"), nil)
	buf := ui.NewTextBuffer(src)
	snap := buf.Snapshot()
	hl.Request(snap, 0, 0)
	for range 400 {
		if hl.Gen() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var cc colorCache
	got := cc.ranges(hl, p, snap, 0, 0, 700)
	if len(got) == 0 {
		t.Fatal("no colors")
	}
	text := []rune(src)
	for i, r := range got {
		if r.Start >= r.End || i > 0 && r.Start < got[i-1].End {
			t.Fatalf("ranges overlap or are empty: %+v", got)
		}
		if i > 0 && got[i-1].Color == r.Color && blank(string(text[got[i-1].End:r.Start])) {
			t.Errorf("ranges %d and %d of one color are apart only by blanks: %+v", i-1, i, got)
		}
	}
	// Each token keeps its color.
	color := func(at int) ui.Color {
		for _, r := range got {
			if at >= r.Start && at < r.End {
				return r.Color
			}
		}
		return p.text
	}
	if c := color(5); c != p.syntax[highlight.Number] {
		t.Errorf("1 is %v, want a number's color", c)
	}
	if c := color(13); c != p.syntax[highlight.Comment] {
		t.Errorf("the comment is %v", c)
	}
	if again := cc.ranges(hl, p, snap, 0, 0, 700); &again[0] != &got[0] {
		t.Error("the same view built its colors again")
	}
}

// Markdown's bold is in weight bold, and its ==highlighted== and
// ~~struck~~ runs are kept for the editor to paint.
func TestColorRangesMarkdown(t *testing.T) {
	p := darkPal
	src := "会**整体**线 ==一个== ~~x~~\n"
	hl := highlight.New(highlight.Lexer("a.md"), nil)
	snap := ui.NewTextBuffer(src).Snapshot()
	hl.Request(snap, 0, 0)
	for range 400 {
		if hl.Gen() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var cc colorCache
	got := cc.ranges(hl, p, snap, 0, 0, 700)
	if len(got) == 0 || got[0].Start != 1 || got[0].End != 7 || got[0].Weight != 700 {
		t.Errorf("bold range %+v, want 1 to 7 in weight 700", got)
	}
	if want := []lineRun{{0, 9, 15}}; !slices.Equal(cc.marks, want) {
		t.Errorf("marks %v, want %v", cc.marks, want)
	}
	if want := []lineRun{{0, 16, 21}}; !slices.Equal(cc.strikes, want) {
		t.Errorf("strikes %v, want %v", cc.strikes, want)
	}
}
