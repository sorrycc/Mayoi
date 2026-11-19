package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

const foldJSON = `{
  "name": "mayoi",
  "deps": {
    "a": [
      1,
      2
    ],
    "b": "{not a brace["
  },
  "tail": [
    "x"
  ]
}
`

// foldHarness opens a file, and waits for its folds to be found.
func foldHarness(t *testing.T, name, text string, wrap bool) (*harness, *editor) {
	t.Helper()
	hs := newHarness(t, false)
	hs.a.settings.WordWrap = wrap
	os.WriteFile(hs.path(name), []byte(text), 0o644)
	hs.a.openFile(hs.path(name), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.until("the folds are found", func() bool {
		return d.foldScan.done && d.foldScan.version == d.buf.Snapshot().Version()
	})
	hs.settle()
	return hs, d
}

// caretOn puts the caret at the end of line i.
func caretOn(hs *harness, d *editor, i int) {
	_, e := d.buf.Snapshot().LineRange(i)
	hs.place(d, e, e)
}

// shownRows returns how many rows of lineH the lines from i up to j take.
func shownRows(hs *harness, d *editor, i, j int) float32 {
	g := hs.a.geometry(d, d.buf.Snapshot().LineCount())
	return (g.top(j) - g.top(i)) / hs.a.font.lineH
}

// A JSON object and the arrays within it fold, one in another, and open:
// the text stays whole, its lines take no room, and the lines after keep
// their numbers.
func TestFoldNestedJSON(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		hs, d := foldHarness(t, "data.json", foldJSON, wrap)
		a := hs.a
		// The array within "deps" first, then "deps" with the caret on it.
		caretOn(hs, d, 3)
		a.run("edit.fold")
		hs.settle()
		if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 1 || runs[0] != (hiddenRun{4, 5}) {
			t.Fatalf("wrap %v: folded %v", wrap, runs)
		}
		if got := shownRows(hs, d, 3, 6); got != 1 {
			t.Errorf("wrap %v: the folded array takes %v rows", wrap, got)
		}
		caretOn(hs, d, 2)
		a.run("edit.fold")
		hs.settle()
		if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 1 || runs[0] != (hiddenRun{3, 7}) {
			t.Fatalf("wrap %v: folded %v", wrap, runs)
		}
		if !wrap {
			hs.shot("fold-json")
		}
		if got := shownRows(hs, d, 0, 9); got != 4 {
			t.Errorf("wrap %v: lines 0 to 9 take %v rows", wrap, got)
		}
		if d.buf.String() != foldJSON {
			t.Fatalf("wrap %v: folding changed the text", wrap)
		}
		// Opening the outer leaves the inner folded.
		a.run("edit.unfold")
		hs.settle()
		if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 1 || runs[0] != (hiddenRun{4, 5}) {
			t.Errorf("wrap %v: after unfold %v", wrap, runs)
		}
		// Fold All, then Unfold All.
		a.run("edit.foldAll")
		hs.settle()
		if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 1 || runs[0] != (hiddenRun{1, 11}) {
			t.Errorf("wrap %v: fold all %v", wrap, runs)
		}
		if l := caretLine(d); l != 0 {
			t.Errorf("wrap %v: the caret is on hidden line %d", wrap, l)
		}
		a.run("edit.unfoldAll")
		hs.settle()
		if len(d.folding.marks) != 0 || shownRows(hs, d, 0, 12) != 12 {
			t.Errorf("wrap %v: unfold all left %v, %v rows", wrap, d.folding.marks, shownRows(hs, d, 0, 12))
		}
		hs.close()
	}
}

// The keys move the caret past the lines folded, never into them.
func TestFoldCaretSkipsFolded(t *testing.T) {
	hs, d := foldHarness(t, "data.json", foldJSON, false)
	a := hs.a
	caretOn(hs, d, 2)
	a.run("edit.fold")
	hs.settle()
	snap := d.buf.Snapshot()
	after, _ := snap.LineRange(8)
	_, head := snap.LineRange(2)
	hs.key(0, ui.KeyRight)
	if d.sel != [2]int{after, after} {
		t.Fatalf("right went to %v, want %d", d.sel, after)
	}
	hs.key(0, ui.KeyLeft)
	if d.sel != [2]int{head, head} {
		t.Fatalf("left went to %v, want %d", d.sel, head)
	}
	hs.key(0, ui.KeyDown)
	if l := caretLine(d); l != 8 {
		t.Fatalf("down went to line %d", l)
	}
	hs.key(0, ui.KeyUp)
	if l := caretLine(d); l != 2 {
		t.Fatalf("up went to line %d", l)
	}
	if len(d.folding.marks) != 1 {
		t.Fatalf("moving opened the fold")
	}
	// Selecting over it takes its lines with it.
	hs.key(ui.Shift, ui.KeyDown)
	if got := snap.Slice(min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])); !strings.Contains(got, `"b": "{not a brace["`) {
		t.Errorf("the selection over the fold is %q", got)
	}
}

// A fold moves with the edits above it, and opens for one within it.
func TestFoldFollowsEdits(t *testing.T) {
	hs, d := foldHarness(t, "data.json", foldJSON, false)
	a := hs.a
	caretOn(hs, d, 9)
	a.run("edit.fold") // "tail"
	hs.settle()
	caretOn(hs, d, 2)
	a.run("edit.fold") // "deps"
	hs.settle()
	// Lines typed above move both.
	hs.place(d, 1, 1)
	hs.tt.Type("\n\n")
	hs.settle()
	if lines := d.foldLines(); len(lines) != 2 || lines[0] != 4 || lines[1] != 11 {
		t.Fatalf("after typing above: %v", lines)
	}
	// Once the text is read again, they hide what they did.
	hs.until("the folds are found", func() bool { return d.foldScan.version == d.buf.Snapshot().Version() })
	hs.settle()
	if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 2 || runs[0] != (hiddenRun{5, 9}) || runs[1] != (hiddenRun{12, 12}) {
		t.Fatalf("runs %v", runs)
	}
	// An edit within the lines hidden opens their fold, the other stays.
	snap := d.buf.Snapshot()
	s, _ := snap.LineRange(7)
	d.cmds = append(d.cmds, func(d *editor) { d.edit([2]int{s, s}, textEdit{s, s, "  "}) })
	a.invalidate()
	hs.settle()
	if lines := d.foldLines(); len(lines) != 1 || lines[0] != 11 {
		t.Fatalf("after an edit within: %v", lines)
	}
	// Undo brings the text back; the fold stays open.
	d.undo()
	hs.settle()
	if lines := d.foldLines(); len(lines) != 1 || lines[0] != 11 {
		t.Errorf("after undo: %v", lines)
	}
	// A fold whose line no longer opens a block opens.
	snap = d.buf.Snapshot()
	_, e := snap.LineRange(11)
	d.cmds = append(d.cmds, func(d *editor) { d.edit([2]int{e - 1, e - 1}, textEdit{e - 1, e, ""}) })
	a.invalidate()
	hs.settle()
	hs.until("the folds are found", func() bool { return d.foldScan.version == d.buf.Snapshot().Version() })
	hs.settle()
	if len(d.folding.marks) != 0 {
		t.Errorf("a fold of a line with no bracket stays: %v", d.foldLines())
	}
}

// Go to Line and Find open the folds that hide what they go to.
func TestFoldRevealsJumps(t *testing.T) {
	hs, d := foldHarness(t, "data.json", foldJSON, false)
	a := hs.a
	a.run("edit.foldAll")
	hs.settle()
	a.goToLine(5, 1)
	hs.settle()
	if l := caretLine(d); l != 4 {
		t.Fatalf("went to line %d", l)
	}
	if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) > 0 && runs[0].from <= 4 && 4 <= runs[0].to {
		t.Fatalf("line 5 stays hidden: %v", runs)
	}
	// Folded again, a match within shows.
	a.run("edit.foldAll")
	hs.settle()
	hs.place(d, 0, 0)
	a.run("edit.find")
	hs.settle()
	hs.tt.Type("not a brace")
	hs.settle()
	a.run("edit.findNext")
	hs.settle()
	if l := caretLine(d); l != 7 {
		t.Fatalf("find went to line %d", l)
	}
	if _, hidden := runAt(a.hiddenRuns(d, d.buf.Snapshot()), 7); hidden {
		t.Errorf("the match stays hidden")
	}
}

// A click on a line's chevron in the gutter folds it, and a click on the
// mark after the folded line opens it.
func TestFoldGutterClicks(t *testing.T) {
	hs, d := foldHarness(t, "main.go", "package main\n\nfunc main() {\n\tprintln(1)\n\tprintln(2)\n}\n", false)
	a := hs.a
	x := a.window.SidebarWidth + d.gutterW - 14 - foldColW/2
	y := titleBarH + tabStripH + crumbsH + editorPadY + 2.5*a.font.lineH
	hs.tt.Move(x, y)
	hs.settle()
	if !d.folding.hover {
		t.Errorf("the pointer over the gutter is not seen")
	}
	hs.shot("fold-gutter-hover")
	hs.tt.ClickAt(x, y)
	hs.settle()
	if lines := d.foldLines(); len(lines) != 1 || lines[0] != 2 {
		t.Fatalf("the click folded %v", lines)
	}
	if len(d.folding.pills) != 1 {
		t.Fatalf("%d marks after folded lines", len(d.folding.pills))
	}
	hs.shot("fold-gutter-folded")
	if d.buf.String() != "package main\n\nfunc main() {\n\tprintln(1)\n\tprintln(2)\n}\n" {
		t.Fatalf("the click changed the text")
	}
	pill := d.folding.pills[0].r
	hs.tt.ClickAt(a.window.SidebarWidth+pill.X+pill.W/2, titleBarH+tabStripH+crumbsH+pill.Y+pill.H/2)
	hs.settle()
	if len(d.folding.marks) != 0 {
		t.Fatalf("the mark's click left %v", d.foldLines())
	}
	// The chevron folds it again, and opens it.
	hs.tt.ClickAt(x, y)
	hs.settle()
	hs.tt.ClickAt(x, y)
	hs.settle()
	if len(d.folding.marks) != 0 {
		t.Errorf("two clicks left %v", d.foldLines())
	}
}

// Python folds by indent, its folds kept with the tab and back in a
// session restored; another editor of the file folds on its own.
func TestFoldPythonSessionAndSplit(t *testing.T) {
	text := "class A:\n    def f(self):\n        return 1\n\n    def g(self):\n        return 2\n"
	hs, d := foldHarness(t, "a.py", text, false)
	a := hs.a
	caretOn(hs, d, 1)
	a.run("edit.fold")
	hs.settle()
	if lines := d.foldLines(); len(lines) != 1 || lines[0] != 1 {
		t.Fatalf("folded %v", lines)
	}
	other := cloneEditor(d)
	a.unfoldAllCmd(d)
	if lines := other.foldLines(); len(lines) != 1 || lines[0] != 1 {
		t.Errorf("the other editor's folds %v", lines)
	}
	// A session puts back the lines folded once the text is read.
	hs.place(d, 0, 0)
	a.restoreEditor(d, settings.Tab{Folds: []int{0, 4}})
	a.invalidate()
	hs.settle()
	if lines := d.foldLines(); len(lines) != 2 || lines[0] != 0 || lines[1] != 4 {
		t.Errorf("restored %v", lines)
	}
	if runs := a.hiddenRuns(d, d.buf.Snapshot()); len(runs) != 1 || runs[0] != (hiddenRun{1, 5}) {
		t.Errorf("runs %v", runs)
	}
}
