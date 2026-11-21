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
