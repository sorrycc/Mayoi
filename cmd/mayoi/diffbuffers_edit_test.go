package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

func bufferEditHarness(t *testing.T, left, right string) (*harness, *diffView, [2]*colTab) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.AutoSave = false
	a.newFile()
	l := a.activeTab()
	l.ed.buf.Set(left)
	a.newFile()
	r := a.activeTab()
	r.ed.buf.Set(right)
	h := withPlugins(hs, nil)
	result := pluginCall(t, hs, h, "window.showDiff", map[string]any{"left": a.documentRef(l.ed.document), "right": a.documentRef(r.ed.document), "layout": "split"})
	if result.err != nil {
		t.Fatal(result.err)
	}
	v := a.activeTab().diff
	hs.settleDiff(v)
	return hs, v, [2]*colTab{l, r}
}

func bufferSideRow(t *testing.T, v *diffView, side, line int) int {
	t.Helper()
	for i, r := range v.rows {
		if r.kind == rowLine && r.sideLine(side) == line {
			return i
		}
	}
	t.Fatalf("no row for side %d line %d", side, line)
	return -1
}

func TestBufferDiffEditsBothSources(t *testing.T) {
	hs, v, tabs := bufferEditHarness(t, "one\nleft\nlast\n", "one\nright\nlast\n")
	a := hs.a
	for side := range 2 {
		row := bufferSideRow(t, v, side, 1)
		if !a.beginDiffEditSide(v, row, side) {
			t.Fatalf("cannot edit side %d: %v", side, a.errors)
		}
		hs.settle()
		e := v.editing
		if e.doc != tabs[side].ed.document || e.side != side {
			t.Fatal("wrong edit source")
		}
		before := e.doc.buf.String()
		other := tabs[1-side].ed.buf.String()
		e.diffReplace("changed-")
		hs.settleDiff(v)
		if !strings.Contains(tabs[side].ed.buf.String(), "changed-") || tabs[1-side].ed.buf.String() != other {
			t.Fatal("edit affected wrong side")
		}
		a.undoDiff(v, false)
		hs.settleDiff(v)
		if tabs[side].ed.buf.String() != before {
			t.Fatal("undo did not restore source")
		}
		a.undoDiff(v, true)
		hs.settleDiff(v)
		if !strings.Contains(tabs[side].ed.buf.String(), "changed-") {
			t.Fatal("redo did not update source")
		}
	}
	hs.shot("feedback-buffer-edit")
	if tabs[0].ed.sourceViews() != 1 || tabs[1].ed.sourceViews() != 1 || len(v.edits) != 2 {
		t.Fatal("synthetic editor changed source ownership")
	}
}

func TestBufferDiffCloseEditedSourceFreezes(t *testing.T) {
	hs, v, tabs := bufferEditHarness(t, "left\n", "right\n")
	a := hs.a
	if !a.beginDiffEditSide(v, bufferSideRow(t, v, 0, 0), 0) {
		t.Fatal(a.errors)
	}
	hs.settle()
	v.editing.diffReplace("final-")
	hs.settleDiff(v)
	final := tabs[0].ed.buf.String()
	ref := a.documentRef(tabs[0].ed.document)
	hs.h.holdClose = true
	a.closeTab(tabs[0], nil, nil)
	if len(hs.h.held) != 1 {
		t.Fatal("synthetic diff editor suppressed unsaved-close prompt")
	}
	hs.h.held[0](1)
	hs.settleDiff(v)
	if _, err := docByID(a, ref.DocumentID); err == nil {
		t.Fatal("closed source kept alive by diff")
	}
	if v.editing != nil || len(v.edits) != 0 || !v.files[0].buffers.closed[0] || v.files[0].old.text != final {
		t.Fatal("source not frozen at final text")
	}
	if a.beginDiffEditSide(v, bufferSideRow(t, v, 0, 0), 0) {
		t.Fatal("closed source remained editable")
	}
	if !a.beginDiffEditSide(v, bufferSideRow(t, v, 1, 0), 1) {
		t.Fatal("live peer stopped editing")
	}
	hs.settle()
	peer := tabs[1].ed.buf.String()
	row := bufferSideRow(t, v, 0, 0)
	l := a.diffGeometry(v, true, v.viewW)
	origin, ok := hs.tt.Find(locales.DiffPaneLabel())
	if !ok {
		t.Fatal("no diff pane")
	}
	y, _ := a.sideRowY(v, 0, row)
	hs.tt.ClickAt(origin.X+l.textX+2, origin.Y+y+a.font.lineH/2)
	hs.tt.Type("must not edit peer")
	hs.settleDiff(v)
	if v.editing != nil || tabs[1].ed.buf.String() != peer {
		t.Fatal("clicking the closed side edited the live peer")
	}

}

func TestBufferDiffClickEditsEmptyAndPopulatedSources(t *testing.T) {
	for _, texts := range [][2]string{{"", ""}, {"same\nleft\n", "same\nright\n"}} {
		hs, v, tabs := bufferEditHarness(t, texts[0], texts[1])
		a := hs.a
		for side := range 2 {
			row := bufferSideRow(t, v, side, 0)
			l := a.diffGeometry(v, true, v.viewW)
			x := l.textX + 2
			if side == 1 {
				x += l.right()
			}
			y, _ := a.sideRowY(v, side, row)
			origin, found := hs.tt.Find(locales.DiffPaneLabel())
			if !found {
				t.Fatal("no diff pane")
			}
			hs.tt.ClickAt(origin.X+x, origin.Y+y+a.font.lineH/2)
			hs.tt.Type("typed")
			hs.settleDiff(v)
			if v.editing == nil || v.editing.side != side || !strings.Contains(tabs[side].ed.buf.String(), "typed") {
				t.Fatalf("click did not edit side %d: %q %v", side, tabs[side].ed.buf.String(), a.errors)
			}
		}
	}
}

func TestBufferDiffEditSaveAsAndComposition(t *testing.T) {
	hs, v, tabs := bufferEditHarness(t, "left\n", "right\n")
	a := hs.a
	if !a.beginDiffEditSide(v, bufferSideRow(t, v, 0, 0), 0) {
		t.Fatal(a.errors)
	}
	hs.settle()
	e := v.editing
	e.beginCompose("中")
	e.commitCompose("中文")
	hs.settleDiff(v)
	if tabs[0].ed.buf.String() != "中文left\n" {
		t.Fatal("composition edited wrong buffer")
	}
	path := hs.path("edited-left.txt")
	hs.h.saveTo = path
	a.diffEditKey(v, e, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyS, Mods: ui.Cmd})
	hs.settleDiff(v)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "中文left\n" {
		t.Fatalf("save did not persist source: %q %v", data, err)
	}
	if tabs[0].ed.path != path || v.files[0].buffers.ids[0] != tabs[0].ed.id {
		t.Fatal("Save As changed source identity")
	}
}

// Opening a row goes to the source that has its line, and past a closed
// source to the one still open.
func TestBufferDiffOpensTheSourceOfTheRow(t *testing.T) {
	hs, v, tabs := bufferEditHarness(t, "same\nleft only\n", "same\n")
	a := hs.a
	f := v.files[0]
	row := -1
	for i, r := range v.rows {
		if r.kind == rowLine && r.old == 1 && r.new < 0 {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no row only the left source has")
	}
	a.openBufferRow(f, v.rows[row])
	hs.settle()
	if a.activeTab() != tabs[0] {
		t.Fatal("a left-only row opened the right source")
	}
	a.dropTab(tabs[1])
	col := a.fileColumn()
	for i, tab := range col.tabs {
		if tab.diff == v {
			a.activate(col, i, true)
		}
	}
	hs.settleDiff(v)
	a.openBufferRow(f, v.rows[bufferSideRow(t, v, 1, 0)])
	hs.settle()
	if a.activeTab() != tabs[0] {
		t.Fatal("a closed right source left nothing opened")
	}
}
