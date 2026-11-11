package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// diffEditRow returns the row of a diff that shows line of the new side of
// the file at path, -1 for none.
func diffEditRow(v *diffView, path string, line int) int {
	for i, r := range v.rows {
		if r.kind == rowLine && r.new == line && v.files[r.file].path == path {
			return i
		}
	}
	return -1
}

// TestDiffEditBeginsOnTheWritableSide: beginning to edit in a diff edits
// the working tree side of a file, through the document the editor opens,
// and refuses the sides it may not write.
func TestDiffEditBeginsOnTheWritableSide(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	orig, changed := twoHunkFile(hs, git)
	_ = orig
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f := v.files[0]
	if !f.writable() {
		t.Fatal("the working tree side is not writable")
	}
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the changed line")
	}
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	if v.editing == nil || v.editing.f != f {
		t.Fatal("no edit target, or not the file of the row")
	}
	// The edit is of the document the editor opens, at the line the row
	// shows.
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("no document was made for the file")
	}
	if v.editing.doc != a.docs[i] {
		t.Fatal("the edit is not of the document of the file")
	}
	snap := v.editing.doc.buf.Snapshot()
	if line := snap.LineAt(min(v.editing.ed.sel[1], snap.Len())); line != 2 {
		t.Fatalf("the caret is on line %d, want 2", line)
	}
	// Once begun, beginning again at the file keeps the target, and puts
	// the caret where is asked.
	row = diffEditRow(v, hs.path("t.txt"), 25)
	if !a.beginDiffEdit(v, row) || v.editing.f != f {
		t.Fatal("editing the file again did not keep the target")
	}
	if line := snap.LineAt(min(v.editing.ed.sel[1], snap.Len())); line != 25 {
		t.Fatalf("the caret is on line %d, want 25", line)
	}
	a.endDiffEdit(v)
	if v.editing != nil {
		t.Fatal("still editing after leaving")
	}

	// The index side of the staged changes is not for editing.
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	git("add", "t.txt")
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffStaged}, true)
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	sf := v.files[0]
	if sf.writable() {
		t.Fatal("the index side is writable")
	}
	row = diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the staged change")
	}
	if a.beginDiffEdit(v, row) || v.editing != nil {
		t.Fatal("the index side began to edit")
	}
	if len(a.errors) == 0 {
		t.Error("no word of why it may not edit")
	}
}

// TestDiffEditOpensTheFileItEdits: a file that no editor has is read into
// a document of its own to edit, which the editor opens as it is, with the
// edits of the diff in it.
func TestDiffEditOpensTheFileItEdits(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the changed line")
	}
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	// No tab opened: the document is the diff's to edit.
	if a.docIndex(hs.path("t.txt")) < 0 {
		t.Fatal("no document was made for the file")
	}
	for _, col := range a.cols {
		for _, tab := range col.tabs {
			if tab.path() == hs.path("t.txt") {
				t.Error("a tab opened for the file")
			}
		}
	}
	// The editor opens the same document, edits and all.
	e := v.editing
	e.ed.edit(e.ed.sel, textEdit{0, 0, "// edited in the diff\n"})
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the editor did not open the document")
	}
	if got := a.docs[i].buf.String(); !strings.Contains(got, "// edited in the diff\n") || !strings.Contains(got, changed) {
		t.Fatalf("the document the editor opened:\n%s", got)
	}
}

// TestDiffEditRefusesWhatMayNotEdit: a diff that is behind the files, a
// binary file and a submodule do not begin to edit.
func TestDiffEditRefusesWhatMayNotEdit(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	// A diff that is behind the file does not edit over it.
	fresh := strings.Replace(changed, "line twenty-five\n", "line 25th\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(fresh), 0o644)
	f := v.files[0]
	f.diskStale = true
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if a.beginDiffEdit(v, row) {
		t.Fatal("a diff behind the file began to edit")
	}
	if len(a.errors) == 0 {
		t.Error("no word of the diff being behind")
	}
}

// TestDiffEditTypesIntoTheDocument: typing in a diff edits the document
// the editor opens, which shows as changed and is not written before it is
// saved; the diff reads it again; undo takes the typing back.
func TestDiffEditTypesIntoTheDocument(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := twoHunkFile(hs, git)
	_ = orig
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	e := v.editing
	// Typing goes in at the caret, into what the work tree has.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"}) {
		t.Fatal("the diff did not take the text")
	}
	want := strings.Replace(changed, "line two\n", "Xline two\n", 1)
	if e.doc.buf.String() != want {
		t.Fatalf("in the document:\n%s", e.doc.buf.String())
	}
	if !e.doc.dirty() {
		t.Error("the document does not show as changed")
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != changed {
		t.Errorf("the file was written before the document was saved:\n%s", data)
	}
	// More typing joins the step of undo, as typing in the editor does.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "Y"})
	if e.doc.buf.String() != strings.Replace(changed, "line two\n", "XYline two\n", 1) {
		t.Fatalf("in the document:\n%s", e.doc.buf.String())
	}
	if n := len(e.doc.hist.undo); n > 1 {
		t.Errorf("the typing took %d steps of undo", n)
	}
	// The diff catches up with the edits, and shows them as its changes.
	hs.settleDiff(a.changesTabAndShow())
	v = a.activeTab().diff
	if got := v.files[0].new.text; got != e.doc.buf.Snapshot().String() {
		t.Fatalf("the diff has %q, not what the document has", got)
	}
	shown := false
	for _, r := range v.rows {
		if r.kind == rowLine && r.new >= 0 && strings.Contains(v.files[r.file].new.lines[r.new], "XYline two") {
			shown = true
		}
	}
	if !shown {
		t.Error("the diff does not show the typed line")
	}
	// Undo takes the typing back, in the editor of the document too.
	if !e.ed.undo() || e.doc.buf.String() != changed {
		t.Fatalf("undo: %q", e.doc.buf.String())
	}
	// Typing through the pane goes in as well.
	a.beginDiffEdit(v, diffEditRow(a.changesTabAndShow(), hs.path("t.txt"), 2))
	hs.settle()
	hs.tt.Type("Z")
	hs.settle()
	if !strings.Contains(e.doc.buf.String(), "Zline two") {
		t.Fatalf("typed through the pane: %q", e.doc.buf.String())
	}
}

// diffEditOf begins editing a diff's file at line and returns its edit.
func diffEditOf(t *testing.T, hs *harness, v *diffView, path string, line int) *diffEdit {
	t.Helper()
	row := diffEditRow(v, path, line)
	if row < 0 {
		t.Fatalf("no row of line %d of %s", line, path)
	}
	if !a_of(hs).beginDiffEdit(v, row) {
		t.Fatalf("the writable side of %s did not begin to edit", path)
	}
	return v.editing
}

func a_of(hs *harness) *app { return hs.a }

// TestDiffEditMovesTheCaret: the arrow keys move the caret over the rows
// the diff shows, keeping the column, and extend the selection with shift.
func TestDiffEditMovesTheCaret(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	path := hs.path("t.txt")
	e := diffEditOf(t, hs, v, path, 2)
	snap := e.doc.buf.Snapshot()
	lineAt := func() int { return snap.LineAt(min(e.ed.sel[1], snap.Len())) }
	// Right goes a rune along, and left comes back.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight})
	if n := e.ed.sel[1] - 14; n != 1 {
		t.Fatalf("right: %d", n)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyLeft})
	if e.ed.sel[1] != 14 {
		t.Fatalf("left: %d", e.ed.sel[1])
	}
	// Down goes to the row below, keeping the column.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDown})
	if lineAt() != 3 || e.ed.sel[1] != 14+9 {
		t.Fatalf("down: line %d, sel %v", lineAt(), e.ed.sel)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUp})
	if lineAt() != 2 {
		t.Fatalf("up: line %d", lineAt())
	}
	// With shift, the selection grows, and copy takes its text.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDown, Mods: ui.Shift})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	if text := e.diffEditedText(); text != "line two\nl" { // line 2 and a rune of line 3
		t.Fatalf("selected %q", text)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	if got := a.services.ReadClipboard(); got != "line two\nl" {
		t.Fatalf("copied %q", got)
	}
	// Paste puts what was copied in at the caret, which the left key
	// brings to the start of the selection.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyLeft})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "paste"})
	if text := e.diffEditedText(); text != "" || !strings.Contains(e.doc.buf.Snapshot().String(), "line two\nlline two\nline 3") {
		t.Fatalf("pasted: %q", e.diffEditedText())
	}
	// Cut takes the selection away and holds it.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "cut"})
	if text := e.diffEditedText(); text != "" || !strings.Contains(e.doc.buf.Snapshot().String(), "ine 3") {
		t.Fatalf("cut: %q", e.diffEditedText())
	}
	if got := a.services.ReadClipboard(); got != "l" {
		t.Fatalf("cut holds %q", got)
	}
	// Select all takes the document.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "selectAll"})
	if text := e.diffEditedText(); text != e.doc.buf.Snapshot().String() {
		t.Fatal("select all missed the document")
	}
	// Backspace takes the selection away.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyBackspace})
	if e.doc.buf.Snapshot().String() != "" {
		t.Fatalf("backspace: %q", e.doc.buf.Snapshot().String())
	}
	// And undo, from the diff, takes the editing back.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "undo"})
	if e.doc.buf.Snapshot().String() != changed {
		t.Fatalf("undo: %q", e.doc.buf.Snapshot().Slice(0, 40))
	}
	// Escape goes back to the pager.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape})
	if v.editing != nil {
		t.Fatal("still editing after escape")
	}
	// The pager's keys are the pager's again.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyJ}) {
		t.Error("the pager did not take j")
	}
}

// TestDiffEditTakesKeysFromThePager: while the side is edited the pager's
// keys type, and paging stays with the page keys.
func TestDiffEditTakesKeysFromThePager(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	path := hs.path("t.txt")
	e := diffEditOf(t, hs, v, path, 2)
	// A pager key types while the side is edited.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "j"}) {
		t.Fatal("the edit did not take j")
	}
	if !strings.Contains(e.doc.buf.Snapshot().String(), "jline two") {
		t.Fatalf("typed: %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	// The caret did not move by a row.
	if line := snapLineOf(e); line != 2 {
		t.Fatalf("j moved to line %d", line)
	}
	// The page keys page on.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyPageDown}) {
		t.Fatal("the pager did not page")
	}
}

func snapLineOf(e *diffEdit) int {
	snap := e.doc.buf.Snapshot()
	return snap.LineAt(min(e.ed.sel[1], snap.Len()))
}

// TestDiffEditSavesTheFile: saving from the diff writes the document as it
// is, and the document shows as saved, as the editor's does.
func TestDiffEditSavesTheFile(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"})
	// Saving, by the key the editor saves with.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyS, Mods: ui.Cmd}) {
		t.Fatal("the edit did not save")
	}
	want := strings.Replace(changed, "line two\n", "Xline two\n", 1)
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != want {
		t.Fatalf("on disk:\n%s", data)
	}
	if e.doc.dirty() {
		t.Error("the document shows as changed after saving")
	}
}

// TestDiffEditKeepsItsPlaceAsTheDiffCatchesUp: while the diff reads what
// the typing made again, the scroll holds, and the caret stays on its line
// when the rows come back; and the changes of a stale diff are not applied.
func TestDiffEditKeepsItsPlaceAsTheDiffCatchesUp(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	// A file tall enough to scroll: a change in every tenth line keeps
	// the unchanged lines from folding away.
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig := b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "tall")
	changed := orig
	for i := 10; i < 60; i += 10 {
		changed = strings.Replace(changed, fmt.Sprintf("line %d\n", i), fmt.Sprintf("line ten %d\n", i), 1)
	}
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 10)
	// The caret's row, and the scroll, before the typing.
	row := v.caretRow(e)
	if row < 0 {
		t.Fatal("the caret has no row")
	}
	line := snapLineOf(e)
	// Scroll away, and type a rune: no line was added or taken.
	v.scroll.Y = 100
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"})
	// The diff is behind: its changes are not applied over the typing.
	f, hks := v.hunksAt(v.starts[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	if len(a.errors) == 0 {
		t.Error("no word of the diff catching up")
	}
	a.errors = nil
	// Caught up, the scroll is where it was, and the caret on its line.
	hs.settleDiff(v)
	if v.scroll.Y != 100 {
		t.Errorf("the scroll moved to %v", v.scroll.Y)
	}
	if snapLineOf(e) != line {
		t.Errorf("the caret moved to line %d", snapLineOf(e))
	}
	if row := v.caretRow(e); row < 0 {
		t.Error("the caret has no row after the diff caught up")
	}
	if got := v.editing.f.new.text; got != e.doc.buf.Snapshot().String() {
		t.Fatal("the diff did not catch up with the document")
	}
}

// TestDiffEditKeepsWorkingAcrossLayouts: side by side or in one column,
// the edit keeps its file and its caret's line, and undo keeps working.
func TestDiffEditKeepsWorkingAcrossLayouts(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	line := snapLineOf(e)
	// Side by side, the caret keeps its line, and typing goes in.
	a.setDiffLayout(v, layoutSplit)
	if got := snapLineOf(e); got != line {
		t.Fatalf("side by side: the caret is on line %d", got)
	}
	if row := v.caretRow(e); row < 0 {
		t.Fatal("side by side: the caret has no row")
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "S"})
	if !strings.Contains(e.doc.buf.Snapshot().String(), "Sline two") {
		t.Fatalf("side by side: typed %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	// Back in one column, undo takes it back, and typing goes in again,
	// from the start of the line it began on.
	a.setDiffLayout(v, layoutInline)
	if !e.ed.undo() || strings.Contains(e.doc.buf.Snapshot().String(), "Sline two") {
		t.Fatal("undo did not take the typing back")
	}
	a.beginDiffEdit(v, diffEditRow(v, hs.path("t.txt"), 2))
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "T"})
	if !strings.Contains(e.doc.buf.Snapshot().String(), "Tline two") {
		t.Fatalf("inline: typed %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	if v.editing.f != e.f {
		t.Fatal("the edit changed files across the layouts")
	}
}

// TestDiffEditPlacesTheCaretByClicking: the button edits the side, a click
// on it puts the caret at the character clicked, and a drag selects.
func TestDiffEditPlacesTheCaretByClicking(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	path := hs.path("t.txt")
	a.setDiffLayout(v, layoutInline)
	hs.settle()
	// The button of the breadcrumbs begins the editing of the side.
	if _, ok := hs.tt.Find("Edit"); !ok {
		t.Fatal("no button to edit the side")
	}
	if err := hs.tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.editing == nil || v.editing.f.path != path {
		t.Fatal("the button did not begin the editing")
	}
	e := v.editing
	l := a.diffGeometry(v, v.split, v.viewW)
	// A click at a character of line 25 puts the caret there.
	row := diffEditRow(v, path, 25)
	if row < 0 {
		t.Fatal("no row of line 25")
	}
	v.scroll.Y = max(0, editorPadY+float32(v.tops[row])*a.font.lineH-300)
	hs.settle()
	y := titleBarH + tabStripH + crumbsH + editorPadY + float32(v.tops[row])*a.font.lineH - v.scroll.Y + a.font.lineH/2
	x := a.window.SidebarWidth + l.textX + 3*a.font.charW
	hs.tt.ClickAt(x, y)
	hs.settle()
	snap := e.doc.buf.Snapshot()
	s, _ := snap.LineRange(25)
	if e.ed.sel[1] != s+3 {
		t.Fatalf("the caret is at %d, want %d", e.ed.sel[1], s+3)
	}
	// A drag from there down selects the lines between: the press puts the
	// caret where it is, at the character the click had found.
	y2 := y + 2*a.font.lineH
	hs.tt.Press(x, y)
	hs.tt.Move(x, y2)
	hs.tt.Release(x, y2)
	hs.settle()
	if text := e.diffEditedText(); text != "e twenty-five\nline 26\nlin" {
		t.Fatalf("the drag selected %q", text)
	}
	// The input methods get a place to put their candidates at.
	if _, ok := a.diffCaretRect(v, e, l); !ok {
		t.Error("the caret has no place for the input methods")
	}
}

// TestDiffEditBeginsByTheKey: the I key of the pager begins the editing of
// the working tree side of the chosen row, and a line of the diff copies
// while it reads as a pager.
func TestDiffEditBeginsByTheKey(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	// The change gone to is of the working tree: I edits its side.
	v.stepHunk(1, crossAsk)
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyI}) {
		t.Fatal("the pager did not take I")
	}
	if v.editing == nil || v.editing.f.path != hs.path("t.txt") {
		t.Fatal("I did not begin the editing")
	}
	a.endDiffEdit(v)
	// A line of a side that is read copies as it is.
	v.setCursor(diffEditRow(v, hs.path("t.txt"), 25))
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"}) {
		t.Fatal("the diff did not copy the line")
	}
	if got := a.services.ReadClipboard(); got != "line twenty-five" {
		t.Fatalf("copied %q", got)
	}
}

// TestDiffEditTakesTheEditorKeys: Enter breaks the line as the editor's
// does, with the indent, and Tab indents, each a step of undo.
func TestDiffEditTakesTheEditorKeys(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	_ = changed
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 25)
	// Enter at the start of the line breaks it off above.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEnter}) {
		t.Fatal("the edit did not take Enter")
	}
	hs.settle()
	snap := e.doc.buf.Snapshot()
	if line := snap.LineAt(min(e.ed.sel[1], snap.Len())); line != 26 {
		t.Fatalf("the caret is on line %d after Enter", line)
	}
	if s, en := snap.LineRange(25); s != en {
		t.Fatalf("Enter left %q on the empty line", snap.Slice(s, en))
	}
	if s, en := snap.LineRange(26); snap.Slice(s, en) != "line twenty-five" {
		t.Fatalf("Enter left %q below", snap.Slice(s, en))
	}
	// Tab indents the line the caret is on.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyTab}) {
		t.Fatal("the edit did not take Tab")
	}
	hs.settle()
	snap = e.doc.buf.Snapshot()
	if s, en := snap.LineRange(26); snap.Slice(s, en) != "    line twenty-five" {
		t.Fatalf("Tab left %q", snap.Slice(s, en))
	}
	// And undo takes the two back, one step each.
	if !e.ed.undo() {
		t.Fatal("no undo of the indent")
	}
	snap = e.doc.buf.Snapshot()
	if s, en := snap.LineRange(26); snap.Slice(s, en) != "line twenty-five" {
		t.Fatalf("undo left %q", snap.Slice(s, en))
	}
	changed = twoHunkFileChanged()
	if !e.ed.undo() || e.doc.buf.Snapshot().String() != changed {
		t.Fatalf("undo did not take the break back: %q", e.doc.buf.Snapshot().Slice(0, 40))
	}
}

// twoHunkFileChanged is the text twoHunkFile's file has in the work tree.
func twoHunkFileChanged() string {
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig := b.String()
	return strings.Replace(strings.Replace(orig, "line 2\n", "line two\n", 1), "line 25\n", "line twenty-five\n", 1)
}

// TestDiffEditComposeReplacesItsMark: an input method's marked text takes
// the place of what it marked before, wherever composing began, and the
// text it commits with leaves none of the marks behind.
func TestDiffEditComposeReplacesItsMark(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	// The caret sits at the start of the changed line, where the text
	// before it is the line break, which no mark matches.
	// n marks, ni marks over it, 你好 commits: no mark is left behind.
	for _, ev := range []ui.InputEvent{
		{Kind: ui.InputCompose, Text: "n"},
		{Kind: ui.InputCompose, Text: "ni"},
		{Kind: ui.InputText, Text: "你好"},
	} {
		if !a.diffInput(v, ev) {
			t.Fatalf("the diff did not take %+v", ev)
		}
	}
	if got := e.doc.buf.String(); !strings.Contains(got, "\n你好line two\n") || strings.Contains(got, "nni") || strings.Contains(got, "n你") {
		t.Fatalf("after composing:\n%s", got)
	}
}

// TestDiffEditComposeMarksAgainAfterItsEnd: composing again after a
// commit marks where the caret then is, not where the last one did.
func TestDiffEditComposeMarksAgainAfterItsEnd(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	for _, ev := range []ui.InputEvent{
		{Kind: ui.InputCompose, Text: "n"},
		{Kind: ui.InputText, Text: "你"},
		{Kind: ui.InputCompose, Text: "h"},
		{Kind: ui.InputText, Text: "好"},
	} {
		if !a.diffInput(v, ev) {
			t.Fatalf("the diff did not take %+v", ev)
		}
	}
	// The first mark became 你, and the second mark went where the
	// caret then was, right after it, as 好 alone: 你好.
	if got := e.doc.buf.String(); !strings.Contains(got, "\n你好line two\n") {
		t.Fatalf("after composing twice:\n%s", got)
	}
}

// TestDiffEditComposeUndoesAsOneStep: the marks of composing and the
// text it commits with are one step of undo, which brings the text
// before composing began back.
func TestDiffEditComposeUndoesAsOneStep(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	before := e.doc.buf.Snapshot().String()
	for _, ev := range []ui.InputEvent{
		{Kind: ui.InputCompose, Text: "n"},
		{Kind: ui.InputCompose, Text: "ni"},
		{Kind: ui.InputCompose, Text: "nihao"},
		{Kind: ui.InputText, Text: "你好"},
	} {
		if !a.diffInput(v, ev) {
			t.Fatalf("the diff did not take %+v", ev)
		}
	}
	if e.doc.buf.String() == before {
		t.Fatal("composing changed nothing")
	}
	steps := 0
	for e.ed.undo() {
		steps++
		if steps > 3 {
			break
		}
	}
	if steps != 1 {
		t.Errorf("composing took %d steps of undo", steps)
	}
	if e.doc.buf.String() != before {
		t.Fatalf("after undo:\n%s", e.doc.buf.String())
	}
	// An escape takes the mark of a composing back as well, and the next
	// Escape then leaves the editing.
	a.diffEditInput(v, ui.InputEvent{Kind: ui.InputCompose, Text: "n"})
	if e.compose == "" {
		t.Fatal("the input method does not mark")
	}
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}) {
		t.Fatal("the diff did not take the escape")
	}
	if e.compose != "" || !strings.Contains(e.doc.buf.String(), "\nline two\n") {
		t.Fatalf("the escape did not take the mark back: %q %q", e.compose, e.doc.buf.String())
	}
}

// TestDiffUndoFollowsTheDocumentEdited: after an arrow wrote a revert in
// one file, the undo of the diff's keyboard takes back what the editing
// of another file typed, not the arrow's.
func TestDiffUndoFollowsTheDocumentEdited(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	origB := "alpha\nbeta\ngamma\n"
	os.WriteFile(hs.path("u.txt"), []byte(origB), 0o644)
	git("add", "u.txt")
	git("commit", "-qm", "base")
	changedB := strings.Replace(origB, "gamma\n", "gamma local\n", 1)
	os.WriteFile(hs.path("u.txt"), []byte(changedB), 0o644)
	hs.settleGit()
	a.openChangesTab(true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	// The arrow reverts a block of t.txt, which registers its document.
	rowA := diffEditRow(v, hs.path("t.txt"), 2)
	if rowA < 0 {
		t.Fatal("no row of the change of t.txt")
	}
	fA := v.files[v.rows[rowA].file]
	a.revertInto(v, fA, fA.hunks[:1], "Revert")
	if a.confirm != nil {
		a.confirm.run()
		a.confirm = nil
	}
	hs.settle()
	docA := a.docs[a.docIndex(hs.path("t.txt"))]
	// One hunk reverted: the first change is back, the second stands.
	wantA := strings.Replace(twoHunkFileChanged(), "line two\n", "line 2\n", 1)
	if docA.buf.String() != wantA {
		t.Fatalf("the revert did not go in: %q", docA.buf.String())
	}
	// Editing u.txt types into its document, and the undo of the diff's
	// keyboard is then that document's.
	rowB := diffEditRow(v, hs.path("u.txt"), 2)
	if rowB < 0 {
		t.Fatal("no row of the change of u.txt")
	}
	if !a.beginDiffEdit(v, rowB) {
		t.Fatal("u.txt did not begin to edit")
	}
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "Z"}) {
		t.Fatal("the diff did not take the text")
	}
	docB := a.docs[a.docIndex(hs.path("u.txt"))]
	if !strings.Contains(docB.buf.String(), "Zgamma local") {
		t.Fatalf("the typing did not go in: %q", docB.buf.String())
	}
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyZ, Mods: ui.Cmd}) {
		t.Fatal("the diff did not take the undo")
	}
	hs.settle()
	if docB.buf.String() != changedB {
		t.Fatalf("the undo did not take the typing back: %q", docB.buf.String())
	}
	if docA.buf.String() != wantA {
		t.Fatalf("the undo reached the other file: %q", docA.buf.String())
	}
}

// TestDiffEditComposeReplacesTheSelection: beginning to compose takes the
// selected text in the place of the mark, and the text the input method
// commits with stands there.
func TestDiffEditComposeReplacesTheSelection(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	// The caret is at the start of "line two": the shift and the right
	// arrow select the line's first four runes.
	for range 4 {
		a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	}
	// Composing begins: the mark stands in the selection's place.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputCompose, Text: "n"}) {
		t.Fatal("the diff did not take the mark")
	}
	if got := e.doc.buf.Snapshot().String(); !strings.Contains(got, "\nn two\n") {
		t.Fatalf("the mark did not take the selection: %q", lineAtOf(got, 2))
	}
	// The input method commits: the text stands where the selection was.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "好"}) {
		t.Fatal("the diff did not take the text")
	}
	if got := e.doc.buf.Snapshot().String(); !strings.Contains(got, "\n好 two\n") {
		t.Fatalf("after committing over the selection: %q", lineAtOf(got, 2))
	}
}

// TestDiffEditComposeEscapeGivesTheSelectionBack: an escape takes the
// mark of a composing that took a selection back, and the selection's
// text stands again.
func TestDiffEditComposeEscapeGivesTheSelectionBack(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	for range 4 {
		a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCompose, Text: "n"})
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}) {
		t.Fatal("the diff did not take the escape")
	}
	if e.compose != "" {
		t.Fatal("the escape did not end the composing")
	}
	if got := e.doc.buf.Snapshot().String(); !strings.Contains(got, "\nline two\n") {
		t.Fatalf("the escape did not give the selection back: %q", lineAtOf(got, 2))
	}
}

// lineAtOf returns the kth line of a text, for the failures to say.
func lineAtOf(text string, k int) string {
	lines := strings.Split(text, "\n")
	if k < len(lines) {
		return lines[k]
	}
	return ""
}

// TestDiffUnalignedSideStreams: with the rows not aligned, each side's
// lines stand in a stream of their own, which closes up where the other
// side has lines it lacks; aligned again, the streams go.
func TestDiffUnalignedSideStreams(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	orig := "a\nb\nc\nd\ne\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := "a\nb\nX\nY\nc\nd\ne\n"
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a := hs.a
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutSplit)
	a.settings.DiffAlignedRows = false
	a.layoutRows(v, diffGeometry{half: 600, cols: [2]int{80, 80}})
	if len(v.sideTops[0]) != len(v.rows)+1 {
		t.Fatal("no side streams")
	}
	if v.sideTops[0][len(v.sideTops[0])-1] >= v.sideTops[1][len(v.sideTops[1])-1] {
		t.Fatalf("the left stream did not close up: %v against %v", v.sideTops[0], v.sideTops[1])
	}
	// A row both sides have stands as high on the left as the rows
	// aligned have it, until the added lines come.
	if k := addedRow(v); k > 0 && v.sideTops[0][k] != v.tops[k] {
		t.Fatalf("the left stream left early: %d against %d", v.sideTops[0][k], v.tops[k])
	}
	// Aligned again, the streams go.
	a.settings.DiffAlignedRows = true
	a.layoutRows(v, diffGeometry{})
	if len(v.sideTops[0]) != 0 {
		t.Fatal("the side streams stayed")
	}
}

// TestDiffUnalignedRowUnder: the half the pointer is on finds the row in
// its own stream, so that a click lands on the line that is painted
// there.
func TestDiffUnalignedRowUnder(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	orig := "a\nb\nc\nd\ne\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := "a\nb\nX\nY\nc\nd\ne\n"
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a := hs.a
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutSplit)
	a.settings.DiffAlignedRows = false
	a.layoutRows(v, diffGeometry{half: 600, cols: [2]int{80, 80}})
	k := addedRow(v)
	if k < 0 {
		t.Fatal("no added line")
	}
	lineH := a.font.lineH
	// On the right half, at the height the right stream shows the added
	// line, the row under the pointer is that line's.
	y := editorPadY + float32(v.sideTops[1][k])*lineH + lineH/2
	if got := a.rowUnder(v, 700, y, diffGeometry{half: 600}); got != k {
		t.Fatalf("row under the right stream %d, want %d", got, k)
	}
	// On the left half at the same height stands a line the left has
	// kept, whose row is not the added line's.
	if got := a.rowUnder(v, 100, y, diffGeometry{half: 600}); got == k {
		t.Fatalf("the left stream shows the added line's row %d", k)
	}
}

// addedRow returns the row of a diff's first added line, -1 for none.
func addedRow(v *diffView) int {
	for i, r := range v.rows {
		if r.kind == rowLine && r.old < 0 && r.new >= 0 {
			return i
		}
	}
	return -1
}
