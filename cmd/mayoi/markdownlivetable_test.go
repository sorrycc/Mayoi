package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

// liveLongTable is a note of prose around a table of rows rows, under a
// header of three columns, the second centred and the third right.
func liveLongTable(rows int) string {
	var b strings.Builder
	for i := range 20 {
		fmt.Fprintf(&b, "Intro paragraph %d with **bold** and a [link](https://example.com).\n\n", i)
	}
	b.WriteString("| Name | Kind | Note |\n|---|:---:|---:|\n")
	for i := range rows {
		fmt.Fprintf(&b, "| item %d | **kind** %d | some `code` and text %d |\n", i, i%7, i*13)
	}
	b.WriteString("\n")
	for i := range 20 {
		fmt.Fprintf(&b, "Outro paragraph %d with *italic* text.\n\n", i)
	}
	return b.String()
}

// cellShown reports whether the input of the cell edited shows within
// the editor named name.
func cellShown(hs *harness, d *editor, name string) bool {
	c := d.liveCell
	in, ok := hs.tt.Find(locales.LiveCellLabel(strconv.Itoa(c.row+1), strconv.Itoa(c.col+1)))
	view, ok2 := hs.tt.Find("Editor " + name)
	return ok && ok2 && in.Y >= view.Y && in.Y+in.H <= view.Y+view.H
}

// wheelTo scrolls the editor's text by the wheel over x, y, as far as it
// goes down, or up, each step checked by each.
func wheelTo(hs *harness, d *editor, x, y float32, down bool, each func()) {
	dy := float32(240)
	if !down {
		dy = -dy
	}
	for range 400 {
		was := d.vscroll.Y
		hs.tt.Scroll(x, y, 0, dy)
		step(hs.tt)
		step(hs.tt)
		if each != nil {
			each()
		}
		if d.vscroll.Y == was {
			return
		}
	}
}

// A long table builds the rows in view only, and stays as high, its
// columns as wide, as it scrolls; its first and last rows edit in place,
// the cell moved to by a key or a row added shows, and each column of a
// split scrolls and edits its own.
func TestLiveTableLong(t *testing.T) {
	const rows = 400
	hs := newHarness(t, true)
	a := hs.a
	src := liveLongTable(rows)
	d := openLive(t, hs, "long.md", src, 1)
	lv := tableView(d)
	hs.until("the table measured", func() bool { return lv.measured && lv.frame == a.frame && len(lv.built) < rows })
	if len(lv.built) > rows/4 || hs.tt.HasText("item 399") {
		t.Fatalf("the table built %d of its %d rows", len(lv.built), rows+1)
	}
	height, top := lv.at.H, lv.at.Y+d.vscroll.Y
	sum := float32(0)
	for _, r := range lv.rows {
		sum += r.h
	}
	if math.Abs(float64(sum-height)) > 2 {
		t.Fatalf("the table is %v high, its rows %v", height, sum)
	}
	cols := slices.Clone(lv.v.markdown.tables[0])

	// To its end and past, as high, where it was, as wide.
	view, _ := hs.tt.Find("Editor long.md")
	x, y := view.X+view.W/2, view.Y+view.H/2
	wheelTo(hs, d, x, y, true, func() {
		if math.Abs(float64(lv.at.H-height)) > 0.5 || math.Abs(float64(lv.at.Y+d.vscroll.Y-top)) > 0.5 {
			t.Fatalf("scrolled to %v, the table is %v high at %v, not %v at %v", d.vscroll.Y, lv.at.H, lv.at.Y+d.vscroll.Y, height, top)
		}
		if got := lv.v.markdown.tables[0]; !slices.Equal(got, cols) {
			t.Fatalf("scrolled to %v, the columns are %v, not %v", d.vscroll.Y, got, cols)
		}
	})
	if lv.at.Y+lv.at.H > view.Y+view.H {
		t.Fatalf("at the end, the table reaches %v, below the view's %v", lv.at.Y+lv.at.H, view.Y+view.H)
	}
	// Back to the last row: it edits, and Return there adds a row that
	// shows, which undo takes back.
	hs.tt.Scroll(x, y, 0, -400)
	hs.settle()
	if err := hs.tt.Click("item 399"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	cell := &d.liveCell
	if !cell.on || cell.row != rows || cell.col != 0 || !cellFocused(hs, d) {
		t.Fatalf("the click on the last row edits %+v", *cell)
	}
	hs.key(0, ui.KeyEnter)
	if !cell.on || cell.row != rows+1 || !cellShown(hs, d, "long.md") || !cellFocused(hs, d) {
		t.Fatalf("Return in the last row went to %+v, shown %v", *cell, cellShown(hs, d, "long.md"))
	}
	hs.key(ui.Cmd, ui.KeyZ)
	if got := d.buf.String(); got != src || !cell.on || cell.row != rows || !cellShown(hs, d, "long.md") {
		t.Fatalf("undo left the cell %+v, the text as it was: %v", *cell, got == src)
	}
	hs.key(0, ui.KeyEscape)

	// Back to the top: the first row edits, and the cell the keys move
	// to, past the view, shows.
	wheelTo(hs, d, x, y, false, nil)
	hs.tt.Scroll(x, y, 0, lv.at.Y-view.Y-view.H/2)
	hs.settle()
	if err := hs.tt.Click("item 0"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.key(0, ui.KeyUp)
	if !cell.on || cell.row != 0 || !cellShown(hs, d, "long.md") {
		t.Fatalf("Up from the first row went to %+v", *cell)
	}
	was := d.vscroll.Y
	for i := range 40 {
		hs.key(0, ui.KeyEnter)
		if cell.row != i+1 || !cellShown(hs, d, "long.md") || !cellFocused(hs, d) {
			t.Fatalf("Return went to %+v, shown %v", *cell, cellShown(hs, d, "long.md"))
		}
	}
	if d.vscroll.Y <= was {
		t.Fatalf("the view stayed at %v as the cell went down", d.vscroll.Y)
	}
	for range 3 {
		hs.key(0, ui.KeyTab)
	}
	if cell.row != 41 || cell.col != 0 || !cellShown(hs, d, "long.md") {
		t.Fatalf("Tab went to %+v", *cell)
	}
	// Narrower, the rows wrap anew: the table is as high as they are,
	// and the cell still edited.
	hs.tt.SetSize(760, 700)
	hs.settle()
	hs.settle()
	sum = 0
	for _, r := range lv.rows {
		sum += r.h
	}
	if math.Abs(float64(sum-lv.at.H)) > 2 || !cell.on || !cellFocused(hs, d) {
		t.Fatalf("narrower, the table is %v high, its rows %v, the cell %+v", lv.at.H, sum, *cell)
	}
	hs.key(0, ui.KeyEscape)

	// Split: the right column at the end of the table, the left at its
	// top, each builds its own rows and edits its own cell.
	hs.tt.SetSize(1400, 800)
	hs.settle()
	a.splitRight()
	right := a.activeEditor()
	hs.until("the right column's table measured", func() bool {
		lv := tableView(right)
		return lv != nil && lv.measured && lv.frame == a.frame && len(lv.built) < rows
	})
	// Escape left the left column's caret after the table, in view.
	d.vscroll.Y = 0
	hs.settle()
	// The right column scrolled to the table's last rows.
	rlv := tableView(right)
	right.vscroll.Y += rlv.at.Y + rlv.at.H - (view.Y + view.H) + 100
	hs.settle()
	if err := hs.tt.Click("item 399"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !right.liveCell.on || right.liveCell.row != rows || d.liveCell.on {
		t.Fatalf("the click in the right column edits %+v there, %+v in the left", right.liveCell, d.liveCell)
	}
	if left := tableView(d); left.frame != a.frame || slices.Contains(left.built, rows) || d.vscroll.Y != 0 {
		t.Fatalf("the left column at %v built %d rows, the last among them", d.vscroll.Y, len(left.built))
	}
}
