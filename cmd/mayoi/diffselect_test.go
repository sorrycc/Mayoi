package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// diffSelTab shows the changes of twoHunkFile in the layout l, and
// returns the diff, the path, and where the text of side of row i shows
// in the window, at rune col.
func diffSelTab(t *testing.T, l diffLayout) (*harness, *diffView, string, func(i, side int, col float32) (x, y float32)) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	a.setDiffLayout(v, l)
	hs.settle()
	at := func(i, side int, col float32) (x, y float32) {
		g := a.diffGeometry(v, v.split, v.viewW)
		top := editorPadY + float32(v.tops[i])*a.font.lineH
		x = a.window.SidebarWidth + g.textX + col*a.font.charW
		if side == 1 {
			x += g.right()
		}
		return x, titleBarH + tabStripH + crumbsH + top - v.scroll.Y + a.font.lineH/2
	}
	return hs, v, hs.path("t.txt"), at
}

// diffOldRow returns the row of a diff that shows line of the old side of
// the file at path changed, -1 for none.
func diffOldRow(v *diffView, path string, line int) int {
	for i, r := range v.rows {
		if r.kind == rowLine && r.old == line && r.hunk >= 0 && v.files[r.file].path == path {
			return i
		}
	}
	return -1
}

// TestDiffSelectsTextByDragging: a drag over the text of the unified
// layout selects its runes across the lines, removed and added, and copy
// takes them as they are, without numbers or marks.
func TestDiffSelectsTextByDragging(t *testing.T) {
	hs, v, path, at := diffSelTab(t, layoutInline)
	a := hs.a
	if v.split {
		t.Fatal("the layout is not unified")
	}
	from, to := diffOldRow(v, path, 25), diffEditRow(v, path, 26)
	if from < 0 || to < 0 {
		t.Fatal("no rows of the lines")
	}
	x0, y0 := at(from, -1, 5.3)
	x1, y1 := at(to, -1, 4.3)
	hs.tt.Press(x0, y0)
	hs.tt.Move(x1, y1)
	hs.tt.Release(x1, y1)
	hs.settle()
	hs.shot("diff-text-select")
	if got, want := v.selectedText(), "25\nline twenty-five\nline"; got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
	if v.anchor >= 0 {
		t.Error("the drag over the text chose rows")
	}
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"}) {
		t.Fatal("the diff did not copy")
	}
	if got := a.services.ReadClipboard(); got != "25\nline twenty-five\nline" {
		t.Fatalf("copied %q", got)
	}
	// Escape lets go of it, and copy goes back to the chosen line.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape})
	if v.textSel != nil {
		t.Fatal("Escape kept the selection")
	}
}

// TestDiffDoubleClickSelectsAWord: two clicks on the text select the word
// under them rather than open the file, and find takes the word.
func TestDiffDoubleClickSelectsAWord(t *testing.T) {
	hs, v, path, at := diffSelTab(t, layoutInline)
	a := hs.a
	row := diffEditRow(v, path, 25)
	x, y := at(row, -1, 7.3)
	hs.tt.ClickAt(x, y)
	hs.tt.ClickAt(x, y)
	hs.settle()
	if got := v.selectedText(); got != "twenty" {
		t.Fatalf("selected %q, want twenty", got)
	}
	if tab := a.activeTab(); tab == nil || tab.diff != v {
		t.Fatal("the double click left the diff")
	}
	a.openFind(false)
	if a.find.query != "twenty" {
		t.Fatalf("find took %q", a.find.query)
	}
}

// TestDiffSelectionKeepsItsSide: side by side, a drag stays on the side it
// began on, wherever the pointer goes.
func TestDiffSelectionKeepsItsSide(t *testing.T) {
	hs, v, path, at := diffSelTab(t, layoutSplit)
	if !v.split {
		t.Fatal("the sides are not side by side")
	}
	// From the new side's line 24 to the old side's place at line 26: the
	// new side's lines between, and none of the old's line 25.
	from, to := diffEditRow(v, path, 24), diffEditRow(v, path, 26)
	x0, y0 := at(from, 1, 5.3)
	x1, y1 := at(to, 0, 2)
	hs.tt.Press(x0, y0)
	hs.tt.Move(x1, y1)
	hs.tt.Release(x1, y1)
	hs.settle()
	if got, want := v.selectedText(), "24\nline twenty-five\n"; got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
}

func TestWordAround(t *testing.T) {
	for _, c := range []struct {
		text     string
		col      int
		from, to int
	}{
		{"line twenty-five", 7, 5, 11},
		{"line twenty-five", 11, 5, 11},
		{"line twenty-five", 4, 4, 5},
		{"a  b", 2, 1, 3},
		{"f(x)", 1, 0, 1},
		{"", 0, 0, 0},
	} {
		if from, to := wordAround(c.text, c.col); from != c.from || to != c.to {
			t.Errorf("wordAround(%q, %d) = %d, %d, want %d, %d", c.text, c.col, from, to, c.from, c.to)
		}
	}
}

// TestDiffFindsInTheLinesShown: find over a diff finds in the lines it
// shows, not in those folded away, and going to a match unfolds its file
// and chooses its row.
func TestDiffFindsInTheLinesShown(t *testing.T) {
	hs, v, path, _ := diffSelTab(t, layoutInline)
	a := hs.a
	a.openFind(false)
	a.find.query = "line 1"
	hs.settle()
	// Line 1 is near the first change; lines 10 to 19 fold away.
	if n := len(v.find.found); n != 1 {
		t.Fatalf("%d matches of line 1, want 1: %v", n, v.find.found)
	}
	a.find.query = "twenty"
	hs.settle()
	if n := len(v.find.found); n != 1 {
		t.Fatalf("%d matches of twenty, want 1", n)
	}
	m := v.find.found[0]
	if m.side != 1 || m.line != 25 || m.start != 5 || m.end != 11 {
		t.Fatalf("the match is %+v", m)
	}
	v.files[0].folded = true
	v.changed()
	hs.settle()
	a.findStep(1)
	hs.settle()
	hs.shot("diff-find")
	if v.files[0].folded {
		t.Fatal("going to the match left its file folded")
	}
	if v.cursor != diffEditRow(v, path, 25) {
		t.Fatalf("the cursor is on row %d, want the match's", v.cursor)
	}
}
