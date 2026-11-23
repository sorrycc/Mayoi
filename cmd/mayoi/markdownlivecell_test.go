package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func TestTableRowCells(t *testing.T) {
	for _, tc := range []struct {
		line     string
		want     []string
		trailing bool
	}{
		{"| a | b |", []string{"a", "b"}, true},
		{"a | b", []string{"a", "b"}, false},
		{`| a \| b | c |`, []string{`a \| b`, "c"}, true},
		{"|  | x |", []string{"  ", "x"}, true},
		{"||x|", []string{"", "x"}, true},
		{"> | 梨 | 🍐 |", []string{"梨", "🍐"}, true},
		{"  | `a` | **b** |  ", []string{"`a`", "**b**"}, true},
	} {
		cells, trailing := tableRowCells(tc.line)
		var got []string
		for _, c := range cells {
			got = append(got, tc.line[c[0]:c[1]])
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") || trailing != tc.trailing {
			t.Errorf("tableRowCells(%q) = %q, %v; want %q, %v", tc.line, got, trailing, tc.want, tc.trailing)
		}
	}
}

func TestTableCellEdit(t *testing.T) {
	for _, tc := range []struct {
		line string
		k    int
		text string
		want string
	}{
		{"| a | b |", 1, "bee", "| a | bee |"},
		{"| a | b |", 0, "", "|  | b |"},
		{"|  | b |", 0, "x", "| x | b |"},
		{"|  | b |", 0, "", "|  | b |"},
		{"||b|", 0, "x", "| x |b|"},
		{"| a |", 2, "c", "| a |  | c |"},
		{"a | b", 3, "d", "a | b |  | d"},
		{"| 梨 | b |", 0, "梨🍐", "| 梨🍐 | b |"},
	} {
		s, e, with := tableCellEdit(tc.line, tc.k, tc.text)
		if got := tc.line[:s] + with + tc.line[e:]; got != tc.want {
			t.Errorf("tableCellEdit(%q, %d, %q) = %q, want %q", tc.line, tc.k, tc.text, got, tc.want)
		}
	}
	for in, want := range map[string]string{"a|b": `a\|b`, `a\|b`: `a\|b`, "a\nb": "a b", `x\`: `x\`, `\\|`: `\\\|`} {
		if got := tableCellSource(in); got != want {
			t.Errorf("tableCellSource(%q) = %q, want %q", in, got, want)
		}
	}
}

// liveTableNote is a note with a table whose cells hold an escaped pipe,
// markup, CJK, alignments and a link.
const liveTableNote = "intro\n\n| Name | Note |\n| :--- | ---: |\n| apple | a \\| b |\n| 梨 | **ripe** |\n| site | [link](https://example.com/x) |\n\nend\n"

// tableView returns the table Live Preview drew last in an editor.
func tableView(d *editor) *liveView {
	var last *liveView
	for _, lv := range d.livePane.views {
		if last == nil || lv.frame > last.frame {
			last = lv
		}
	}
	return last
}

// writes waits for what was typed into a cell to be written to the row
// of the note that starts with prefix, as want.
func writes(t *testing.T, hs *harness, d *editor, prefix, want string) {
	t.Helper()
	hs.until("the row written as "+want, func() bool {
		for _, l := range strings.Split(d.buf.String(), "\n") {
			if strings.HasPrefix(l, prefix) {
				return l == want
			}
		}
		return false
	})
}

// tableLine is the line of the note in an editor that starts with prefix.
func tableLine(t *testing.T, d *editor, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(d.buf.String(), "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	t.Fatalf("no line starts with %q in %q", prefix, d.buf.String())
	return ""
}

// A click on a cell of a table in Live Preview edits it in place, the
// table drawn and still; Tab and Shift-Tab move across cells, each edit
// one of the text, undone as typing is; Escape leaves, the caret after
// the table.
func TestLiveTableCellEdit(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := liveTableNote + strings.Repeat("more\n", 60)
	d := openLive(t, hs, "cells.md", src, 1)
	lv := tableView(d)
	hs.until("the table measured", func() bool { return lv.measured && lv.frame == a.frame })
	at := lv.at
	sel := d.sel

	if err := hs.tt.Click("apple"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	cell := &d.liveCell
	if !cell.on || cell.row != 1 || cell.col != 0 || cell.text != "apple" {
		t.Fatalf("the click edits %+v, not the cell apple", *cell)
	}
	if active, _ := hs.tt.TextInputActive(); !active || !cellFocused(hs, d) {
		t.Fatal("the cell's input does not have the keyboard")
	}
	if lv.frame != a.frame || lv.at != at {
		t.Errorf("the table moved or showed its source as the cell opened: %v, from %v", lv.at, at)
	}
	if d.sel == sel && !d.selOutside(d.buf.Snapshot(), 2, 6) {
		t.Errorf("the editor's caret %v stands in the table", d.sel)
	}

	hs.tt.Type("s")
	hs.settle()
	writes(t, hs, d, "| apple", "| apples | a \\| b |")
	// Parsed again, the cell, its caret and its table stay.
	hs.until("the note parsed again", func() bool {
		l := d.document.live
		return l.doc != nil && !l.running && !l.pending && strings.Contains(l.doc.blocks[0].source, "apples")
	})
	hs.settle()
	if tableView(d) != lv || len(d.livePane.views) != 1 || !cell.on || cell.row != 1 || cell.col != 0 || !cellFocused(hs, d) {
		t.Fatalf("parsed again, the cell is %+v, its view kept %v", *cell, tableView(d) == lv)
	}
	if lv.at.Y != at.Y {
		t.Errorf("the table moved from %v to %v as it was parsed again", at.Y, lv.at.Y)
	}
	hs.tt.Type("!")
	hs.settle()
	writes(t, hs, d, "| apple", "| apples! | a \\| b |")

	// Tab: the next cell, its source as it is, a pipe typed escaped.
	hs.key(0, ui.KeyTab)
	if cell.row != 1 || cell.col != 1 || cell.text != `a \| b` {
		t.Fatalf("Tab went to %+v", *cell)
	}
	hs.tt.Type("|c")
	hs.settle()
	writes(t, hs, d, "| apple", `| apples! | a \| b\|c |`)
	if cell.text != `a \| b\|c` {
		t.Errorf("the input shows %q", cell.text)
	}
	// Tab at the row's end: the next row's first cell.
	hs.key(0, ui.KeyTab)
	if cell.row != 2 || cell.col != 0 || cell.text != "梨" {
		t.Fatalf("Tab at the row's end went to %+v", *cell)
	}
	hs.tt.Type("🍐")
	hs.settle()
	writes(t, hs, d, "| 梨", "| 梨🍐 | **ripe** |")
	hs.key(ui.Shift, ui.KeyTab)
	if cell.row != 1 || cell.col != 1 {
		t.Fatalf("Shift-Tab went to %+v", *cell)
	}

	// Undo takes back the cells' edits, one cell at a time, the input
	// showing the source as it is; redo makes them again.
	hs.key(ui.Cmd, ui.KeyZ)
	if got := tableLine(t, d, "| 梨"); got != "| 梨 | **ripe** |" {
		t.Fatalf("undo left the row %q", got)
	}
	hs.key(ui.Cmd, ui.KeyZ)
	if got := tableLine(t, d, "| apple"); got != "| apples! | a \\| b |" || cell.text != `a \| b` {
		t.Fatalf("undo left the row %q, the input %q", got, cell.text)
	}
	if !cell.on || !cellFocused(hs, d) {
		t.Fatal("undo stopped editing the cell")
	}
	hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
	if got := tableLine(t, d, "| apple"); got != `| apples! | a \| b\|c |` || cell.text != `a \| b\|c` {
		t.Fatalf("redo left the row %q, the input %q", got, cell.text)
	}
	if got := tableLine(t, d, "| :"); got != "| :--- | ---: |" {
		t.Errorf("the alignments became %q", got)
	}

	// Escape: the editor has the keyboard, its caret after the table,
	// which stays drawn.
	hs.key(0, ui.KeyEscape)
	if cell.on {
		t.Fatal("Escape left the cell edited")
	}
	snap := d.buf.Snapshot()
	if want := liveAfter(snap, 6); d.sel != [2]int{want, want} {
		t.Errorf("Escape left the caret at %v, not %v", d.sel, want)
	}
	if !hs.tt.Focused("Editor cells.md") {
		t.Error("Escape left the keyboard elsewhere")
	}
	hs.settle()
	if lv := tableView(d); lv == nil || lv.frame != a.frame {
		t.Error("the table shows its source after Escape")
	}
}

// Down past the last row, and Shift-Tab before the first cell, leave the
// table with the editor's caret after it and before it.
func TestLiveTableCellLeaves(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "leave.md", liveTableNote, 1)
	if err := hs.tt.Click("site"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.key(0, ui.KeyDown)
	snap := d.buf.Snapshot()
	if want := liveAfter(snap, 6); d.liveCell.on || d.sel != [2]int{want, want} {
		t.Fatalf("Down past the last row left the cell %v, the caret at %v", d.liveCell.on, d.sel)
	}
	if err := hs.tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !d.liveCell.on || d.liveCell.row != 0 {
		t.Fatalf("the click on the header edits %+v", d.liveCell)
	}
	hs.key(ui.Shift, ui.KeyTab)
	if _, want := snap.LineRange(1); d.liveCell.on || d.sel != [2]int{want, want} {
		t.Fatalf("Shift-Tab before the first cell left the cell %v, the caret at %v", d.liveCell.on, d.sel)
	}

	// What is typed waits for typing to pause, but a shortcut, as Save,
	// finds it written.
	if err := hs.tt.Click("site"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Type("s")
	hs.tt.Key(ui.Cmd, ui.KeyS)
	if !strings.Contains(d.buf.String(), "| sites |") {
		t.Fatalf("⌘S found the text as %q", d.buf.String())
	}
	hs.until("the note saved", func() bool {
		got, _ := os.ReadFile(d.path)
		return strings.Contains(string(got), "| sites |")
	})
}

// A ⌘-click on a link in a cell opens it rather than editing the cell.
func TestLiveTableCellLink(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "link.md", liveTableNote, 1)
	if err := hs.tt.ClickWith(ui.Cmd, locales.OpenLink("https://example.com/x")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.tt.OpenedURLs(); len(got) != 1 || got[0] != "https://example.com/x" {
		t.Errorf("the ⌘-click on the link opened %q", got)
	}
	if d.liveCell.on {
		t.Error("the ⌘-click on the link edits its cell")
	}
}

// Two columns split on one note edit cells each on their own: one edited
// in one column shows as text in the other.
func TestLiveTableCellSplit(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	left := openLive(t, hs, "split-cells.md", liveTableNote, 1)
	a.splitRight()
	right := a.activeEditor()
	hs.until("both tables measured", func() bool {
		lv := tableView(right)
		return lv != nil && lv.measured && lv.frame == a.frame
	})
	lv := tableView(right)
	hs.tt.ClickAt(lv.at.X+8, lv.at.Y+lv.at.H/2)
	hs.settle()
	if !right.liveCell.on || left.liveCell.on {
		t.Fatalf("a click in the right column edits %+v there and %+v in the left", right.liveCell, left.liveCell)
	}
	hs.tt.Type("Z")
	hs.settle()
	hs.until("the cell written", func() bool { return strings.Contains(right.buf.String(), "Z |") })
	if left.liveCell.on {
		t.Error("the left column edits a cell it was not asked to")
	}
	if lv := tableView(left); lv == nil || lv.frame != a.frame {
		t.Error("the left column's table is not drawn")
	}
}

// Switched to Source mode, the cell is no longer edited and the table shows
// as its source; Reading view does not edit its cells.
func TestLiveTableCellSourceMode(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	d := openLive(t, hs, "source-cells.md", liveTableNote, 1)
	if err := hs.tt.Click("apple"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !d.liveCell.on {
		t.Fatal("the click does not edit the cell")
	}
	a.run("view.toggleLivePreview")
	hs.settle()
	if d.liveCell.on || a.livePreview(d) {
		t.Fatal("Source mode edits a cell")
	}
	if _, ok := hs.tt.Find(locales.LiveCellLabel("2", "1")); ok {
		t.Error("Source mode shows the cell's input")
	}
	a.setPreview(d, true)
	hs.settle()
	if d.preview == nil || d.preview.markdown == nil || d.preview.markdown.cellPress != nil {
		t.Error("the Reading view edits cells")
	}
}

// cellFocused reports whether the input of the cell an editor edits has
// the keyboard.
func cellFocused(hs *harness, d *editor) bool {
	c := d.liveCell
	return c.on && hs.tt.Focused(locales.LiveCellLabel(strconv.Itoa(c.row+1), strconv.Itoa(c.col+1)))
}

func TestTableStructure(t *testing.T) {
	const table = "| A | B \\| b | C |\n| :--- | :---: | ---: |\n| **x** | `y` | 梨 |\n| 1 |"
	for _, tc := range []struct {
		name string
		src  string
		op   tableOp
		r, k int
		want string
	}{
		{"row above", table, tableRowAbove, 1, 0, "| A | B \\| b | C |\n| :--- | :---: | ---: |\n|  |  |  |\n| **x** | `y` | 梨 |\n| 1 |"},
		{"row below the header", table, tableRowBelow, 0, 2, "| A | B \\| b | C |\n| :--- | :---: | ---: |\n|  |  |  |\n| **x** | `y` | 梨 |\n| 1 |"},
		{"row below the last", table, tableRowBelow, 2, 0, table + "\n|  |  |  |"},
		{"row deleted", table, tableRowDelete, 1, 1, "| A | B \\| b | C |\n| :--- | :---: | ---: |\n| 1 |"},
		{"last row deleted", table, tableRowDelete, 2, 0, "| A | B \\| b | C |\n| :--- | :---: | ---: |\n| **x** | `y` | 梨 |"},
		{"column left", table, tableColLeft, 1, 1, "| A |  | B \\| b | C |\n| :--- | --- | :---: | ---: |\n| **x** |  | `y` | 梨 |\n| 1 |"},
		{"column right of the last", table, tableColRight, 0, 2, "| A | B \\| b | C |  |\n| :--- | :---: | ---: | --- |\n| **x** | `y` | 梨 |  |\n| 1 |"},
		{"column left of the first", table, tableColLeft, 0, 0, "|  | A | B \\| b | C |\n| --- | :--- | :---: | ---: |\n|  | **x** | `y` | 梨 |\n|  | 1 |"},
		{"column deleted", table, tableColDelete, 2, 1, "| A | C |\n| :--- | ---: |\n| **x** | 梨 |\n| 1 |"},
		{"last column deleted", table, tableColDelete, 0, 2, "| A | B \\| b |\n| :--- | :---: |\n| **x** | `y` |\n| 1 |"},
		{"first column deleted", table, tableColDelete, 1, 0, "| B \\| b | C |\n| :---: | ---: |\n| `y` | 梨 |\n|  |"},
		{"without pipes, column left", "a | b\n--- | ---\nc | d", tableColLeft, 0, 0, "|  | a | b\n| --- | --- | ---\n|  | c | d"},
		{"without pipes, column right", "a | b\n--- | ---\nc | d", tableColRight, 0, 1, "a | b |  |\n--- | --- | --- |\nc | d |  |"},
		{"without pipes, column deleted", "a | b\n--- | ---\nc | d", tableColDelete, 0, 1, "| a |\n| --- |\n| c |"},
		{"without pipes, first deleted", "a | b | c\n--- | --- | ---\nd | e | f", tableColDelete, 0, 0, "b | c\n--- | ---\ne | f"},
		{"without pipes, row below", "a | b\n--- | ---\nc | d", tableRowBelow, 1, 0, "a | b\n--- | ---\nc | d\n|  |  |"},
		{"quoted, row below", "> | a | b |\n> | - | - |", tableRowBelow, 0, 0, "> | a | b |\n> | - | - |\n> |  |  |"},
		{"header only, row below", "| a |\n| --- |", tableRowBelow, 0, 0, "| a |\n| --- |\n|  |"},
	} {
		edits, ok := tableStructure(tc.src, tc.op, tc.r, tc.k)
		if got := applyTableEdits(tc.src, edits); !ok || got != tc.want {
			t.Errorf("%s: got %v\n%s\nwant\n%s", tc.name, ok, got, tc.want)
			continue
		}
		// The table stays one, as wide as its header.
		rows, cols := tableShape(strings.Split(tc.want, "\n"))
		if rows == 0 || cols == 0 {
			t.Errorf("%s: no table after it", tc.name)
		}
	}
	for _, tc := range []struct {
		name string
		src  string
		op   tableOp
		r, k int
	}{
		{"the header deleted", table, tableRowDelete, 0, 0},
		{"a row above the header", table, tableRowAbove, 0, 1},
		{"the only column deleted", "| a |\n| - |\n| b |", tableColDelete, 1, 0},
		{"a row past the table", table, tableRowBelow, 3, 0},
		{"a column past the header", table, tableColLeft, 1, 3},
		{"not a table", "| a | b |\n| c | d |", tableRowBelow, 0, 0},
		{"alignments a column short", "| a | b |\n| --- |", tableColLeft, 0, 0},
	} {
		if _, ok := tableStructure(tc.src, tc.op, tc.r, tc.k); ok {
			t.Errorf("%s: made", tc.name)
		}
	}
}

// Tab after the last cell adds a row and edits its first cell; Return in
// the last row adds one and keeps its column; Return in an empty last row
// takes it away and leaves the table; Escape leaves too. Each is one step
// of undo.
func TestLiveTableAppendRows(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "append.md", liveTableNote, 1)
	if err := hs.tt.Click("site"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	cell := &d.liveCell
	hs.key(0, ui.KeyTab)
	if !cell.on || cell.row != 3 || cell.col != 1 {
		t.Fatalf("Tab went to %+v", *cell)
	}
	hs.tt.Key(0, ui.KeyTab)
	// Typed a frame later: the new row's cell shows in that frame, before
	// the note is parsed again, and takes it.
	hs.tt.Frame()
	hs.tt.Type("k")
	writes(t, hs, d, "| k", "| k |  |")
	hs.key(ui.Cmd, ui.KeyZ)
	if !cell.on || cell.row != 4 || cell.col != 0 || cell.last != 7 {
		t.Fatalf("Tab after the last cell went to %+v", *cell)
	}
	if got := strings.Split(d.buf.String(), "\n")[7]; got != "|  |  |" {
		t.Fatalf("the row added is %q", got)
	}
	if !cellFocused(hs, d) {
		t.Fatal("the new row's cell does not have the keyboard at once")
	}
	hs.tt.Type("kiwi")
	hs.key(0, ui.KeyTab)
	hs.tt.Type("green")
	hs.key(0, ui.KeyEnter)
	if got := tableLine(t, d, "| kiwi"); got != "| kiwi | green |" {
		t.Fatalf("the row typed is %q", got)
	}
	if !cell.on || cell.row != 5 || cell.col != 1 {
		t.Fatalf("Return in the last row went to %+v", *cell)
	}
	if !cellFocused(hs, d) {
		t.Fatal("the next row's cell does not have the keyboard at once")
	}
	// Return again, in the empty row: taken away, the caret after the table.
	hs.key(0, ui.KeyEnter)
	if cell.on {
		t.Fatalf("Return in an empty last row still edits %+v", *cell)
	}
	snap := d.buf.Snapshot()
	if want := liveAfter(snap, 7); d.sel != [2]int{want, want} {
		t.Errorf("the caret is at %v, not after the table at %v", d.sel, want)
	}
	if strings.Contains(d.buf.String(), "|  |  |") {
		t.Errorf("the empty row stayed: %q", d.buf.String())
	}
	// Undo brings back the empty row, then takes back the kiwi row.
	d.undo()
	if !strings.Contains(d.buf.String(), "| kiwi | green |\n|  |  |\n") {
		t.Fatalf("undo left %q", d.buf.String())
	}
	// The empty row, what was typed in each cell, and the row Tab added.
	for range 4 {
		d.undo()
	}
	if got := d.buf.String(); got != liveTableNote {
		t.Errorf("undo left %q", got)
	}
}

// A table of a header alone: Tab and Return in it add its first row,
// and Escape leaves it.
func TestLiveTableHeaderOnly(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "header.md", "intro\n\n| Name | Note |\n| --- | --- |\n\nend\n", 1)
	if err := hs.tt.Click("Note"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	cell := &d.liveCell
	if !cell.on || cell.row != 0 || cell.rows() != 1 {
		t.Fatalf("the click edits %+v", *cell)
	}
	hs.key(0, ui.KeyEnter)
	if !cell.on || cell.row != 1 || cell.col != 1 {
		t.Fatalf("Return in the header went to %+v", *cell)
	}
	if got := d.buf.String(); got != "intro\n\n| Name | Note |\n| --- | --- |\n|  |  |\n\nend\n" {
		t.Fatalf("Return left %q", got)
	}
	hs.key(0, ui.KeyEscape)
	if cell.on || !hs.tt.Focused("Editor header.md") {
		t.Fatal("Escape did not leave the table")
	}
}

// The menu of a cell adds and takes away rows and columns, its header
// and its last column kept; each change is one step of undo, and the
// cell edited is the one that stands for it.
func TestLiveTableMenu(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "menu.md", liveTableNote, 1)
	labels := []string{locales.LiveTableRowAbove(), locales.LiveTableRowBelow(), locales.LiveTableRowDelete(), "-",
		locales.LiveTableColLeft(), locales.LiveTableColRight(), locales.LiveTableColDelete()}
	if err := hs.tt.RightClick("Name"); err != nil {
		t.Fatal(err)
	}
	if got := hs.tt.Menu(); strings.Join(got, ",") != strings.Join(labels, ",") {
		t.Fatalf("the header's menu is %q", got)
	}
	// The header stays the first row.
	for _, item := range []string{locales.LiveTableRowAbove(), locales.LiveTableRowDelete()} {
		if err := hs.tt.ChooseMenuItem(item); err == nil {
			t.Errorf("the header's %s is on", item)
		}
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()

	choose := func(on, item string) {
		t.Helper()
		if err := hs.tt.RightClick(on); err != nil {
			t.Fatal(err)
		}
		if err := hs.tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		hs.settle()
	}
	choose("ripe", locales.LiveTableColRight())
	want := "intro\n\n| Name | Note |  |\n| :--- | ---: | --- |\n| apple | a \\| b |  |\n| 梨 | **ripe** |  |\n| site | [link](https://example.com/x) |  |\n\nend\n"
	if got := d.buf.String(); got != want {
		t.Fatalf("Insert Column Right left\n%s", got)
	}
	cell := &d.liveCell
	if !cell.on || cell.row != 2 || cell.col != 2 {
		t.Fatalf("the new column's cell is not edited: %+v", *cell)
	}
	if !cellFocused(hs, d) {
		t.Fatal("the new column's cell does not have the keyboard at once")
	}
	hs.tt.Type("x")
	choose("apple", locales.LiveTableRowDelete())
	if got := d.buf.String(); strings.Contains(got, "apple") || !strings.Contains(got, "| 梨 | **ripe** | x |") {
		t.Fatalf("Delete Row left\n%s", got)
	}
	if !cell.on || cell.row != 1 || cell.col != 0 {
		t.Fatalf("after Delete Row the cell is %+v", *cell)
	}
	choose("Note", locales.LiveTableColDelete())
	if got := d.buf.String(); got != "intro\n\n| Name |  |\n| :--- | --- |\n| 梨 | x |\n| site |  |\n\nend\n" {
		t.Fatalf("Delete Column left\n%s", got)
	}
	if !cell.on || cell.row != 0 || cell.col != 1 {
		t.Fatalf("after Delete Column the cell is %+v", *cell)
	}
	// Undo takes back one change at a time.
	for i, want := range []string{"| Name | Note |  |", "| apple | a \\| b |  |\n| 梨 | **ripe** | x |", "| 梨 | **ripe** |  |", liveTableNote} {
		hs.key(ui.Cmd, ui.KeyZ)
		hs.settle()
		if !strings.Contains(d.buf.String(), want) {
			t.Fatalf("undo %d did not bring back %q: %q", i+1, want, d.buf.String())
		}
	}

	// A table of one column keeps it.
	openLive(t, hs, "one.md", "intro\n\n| Only |\n| --- |\n| x |\n", 1)
	if err := hs.tt.RightClick("Only"); err != nil {
		t.Fatal(err)
	}
	if err := hs.tt.ChooseMenuItem(locales.LiveTableColDelete()); err == nil {
		t.Error("the only column's Delete Column is on")
	}
}

// A change from the menu of a table changed since, a row added from the
// other view, is not made; the cell edited in place follows its table as
// lines are added above it, and stays in it as undo takes its row away.
func TestLiveTableStale(t *testing.T) {
	hs := newHarness(t, true)
	d := openLive(t, hs, "stale.md", liveTableNote, 1)
	if err := hs.tt.Click("apple"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	cell := &d.liveCell
	// The menu opened, then a row added from the other view before a
	// change is chosen: the change is not made.
	if err := hs.tt.RightClick("ripe"); err != nil {
		t.Fatal(err)
	}
	_, e := d.buf.Snapshot().LineRange(6)
	d.edit(d.sel, textEdit{e, e, "\n| added | row |"})
	if err := hs.tt.ChooseMenuItem(locales.LiveTableRowDelete()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := d.buf.String(); !strings.Contains(got, "ripe") {
		t.Fatalf("a change of a table changed since was made: %q", got)
	}
	d.undo()
	hs.settle()

	// Lines added above the table: the cell follows it.
	s, _ := d.buf.Snapshot().LineRange(0)
	d.edit(d.sel, textEdit{s, s, "new\nlines\n"})
	hs.settle()
	if !cell.on || cell.first != 4 || cell.row != 1 || cell.text != "apple" {
		t.Fatalf("lines above the table left the cell %+v", *cell)
	}
	// A row added below, then undone while in it: the nearest row edited.
	hs.key(ui.Cmd, ui.KeyZ) // the lines above
	hs.settle()
	if !cell.on || cell.first != 2 || cell.text != "apple" {
		t.Fatalf("undo left the cell %+v", *cell)
	}
	if err := hs.tt.Click("site"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.key(0, ui.KeyEnter)
	if cell.row != 4 {
		t.Fatalf("Return in the last row went to %+v", *cell)
	}
	hs.key(ui.Cmd, ui.KeyZ)
	hs.settle()
	if !cell.on || cell.row != 3 || cell.text != "site" {
		t.Fatalf("undo of the row left the cell %+v", *cell)
	}
}
