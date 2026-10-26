package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// diffNavOf opens the tab of all the changes of the harness's work tree,
// whose files a.txt and b.txt each changed one line, and waits for the
// diffs to read.
func diffNavOf(t *testing.T, hs *harness) *diffView {
	t.Helper()
	hs.settleGit()
	hs.a.run("git.openAllChanges")
	hs.settle()
	hs.settle()
	v := hs.a.activeTab().diff
	if v == nil {
		t.Fatal("no tab of the changes")
	}
	hs.settleDiff(v)
	return v
}

// changeAt returns the row of the kth change of a diff, -1 for none.
func changeAt(v *diffView, k int) int {
	if k < 0 || k >= len(v.starts) {
		return -1
	}
	return v.starts[k]
}

// TestDiffChangeNavigationSaysAtTheEnds: the change navigation of a diff
// of two files moves from change to change, and where a file's changes
// end it says so, and waits for one more step that way before it goes to
// the next file; at the ends of the diff it says so, and stays. It never
// goes round the ends without a word.
func TestDiffChangeNavigationSaysAtTheEnds(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\nfive\nsix\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "two files")
	os.WriteFile(hs.path("a.txt"), []byte("one\n2\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\n6\nsix\n"), 0o644)
	v := diffNavOf(t, hs)
	if len(v.files) != 2 || len(v.starts) != 2 {
		t.Fatalf("files %d, changes %d", len(v.files), len(v.starts))
	}
	if v.current != -1 {
		t.Fatalf("current %d", v.current)
	}
	// From nothing, a step goes to the first change.
	v.stepHunk(1, crossAsk)
	if v.current != 0 || v.boundary != nil {
		t.Fatalf("first step: current %d, boundary %v", v.current, v.boundary)
	}
	// The next step reaches the last change of a.txt: it says so, and
	// waits for another.
	v.stepHunk(1, crossAsk)
	if v.current != 0 {
		t.Fatalf("stayed at %d", v.current)
	}
	if v.boundary == nil || v.boundary.step != 1 ||
		!strings.Contains(v.boundary.msg, "last change of a.txt") || !strings.Contains(v.boundary.msg, "Press again") {
		t.Fatalf("boundary %+v", v.boundary)
	}
	// One more step crosses into b.txt.
	v.stepHunk(1, crossAsk)
	if v.current != 1 || v.boundary != nil {
		t.Fatalf("crossed to %d, boundary %v", v.current, v.boundary)
	}
	if file := v.rows[v.starts[v.current]].file; v.files[file].path != hs.path("b.txt") {
		t.Fatalf("crossed to %s", v.files[file].path)
	}
	// At the last change of the diff it says so, and stays.
	v.stepHunk(1, crossAsk)
	if v.current != 1 || v.boundary == nil || v.boundary.msg != "This is the last change of the diff." {
		t.Fatalf("end: current %d, boundary %+v", v.current, v.boundary)
	}
	v.stepHunk(1, crossAsk)
	if v.current != 1 || v.boundary == nil || v.boundary.msg != "This is the last change of the diff." {
		t.Fatalf("end again: current %d, boundary %+v", v.current, v.boundary)
	}
	// Back into a.txt: it says so first, and crosses on one more step.
	v.stepHunk(-1, crossAsk)
	if v.current != 1 || v.boundary == nil || v.boundary.step != -1 ||
		!strings.Contains(v.boundary.msg, "first change of b.txt") {
		t.Fatalf("back: current %d, boundary %+v", v.current, v.boundary)
	}
	v.stepHunk(-1, crossAsk)
	if v.current != 0 || v.boundary != nil {
		t.Fatalf("crossed back to %d, boundary %v", v.current, v.boundary)
	}
	// At the first change of the diff it says so, and stays.
	v.stepHunk(-1, crossAsk)
	if v.current != 0 || v.boundary == nil || v.boundary.msg != "This is the first change of the diff." {
		t.Fatalf("start: current %d, boundary %+v", v.current, v.boundary)
	}
	// Any other way of moving clears what it said.
	v.stepHunk(1, crossAsk)
	v.setCursor(changeAt(v, 1))
	if v.boundary != nil {
		t.Fatalf("boundary %+v after a move", v.boundary)
	}
}

// TestDiffChangeNavigationCrossesAsChosen: the settings have the change
// navigation cross into another file at once, or never.
func TestDiffChangeNavigationCrossesAsChosen(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\nfive\nsix\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "two files")
	os.WriteFile(hs.path("a.txt"), []byte("one\n2\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\n6\nsix\n"), 0o644)
	v := diffNavOf(t, hs)

	// Cross at once.
	v.stepHunk(1, crossAlways)
	if v.current != 0 {
		t.Fatalf("first step: current %d", v.current)
	}
	v.stepHunk(1, crossAlways)
	if v.current != 1 || v.boundary != nil {
		t.Fatalf("crossed to %d, boundary %v", v.current, v.boundary)
	}
	v.stepHunk(-1, crossAlways)
	if v.current != 0 || v.boundary != nil {
		t.Fatalf("crossed back to %d, boundary %v", v.current, v.boundary)
	}

	// Never leave the file.
	v.stepHunk(1, crossNever)
	if v.current != 0 || v.boundary == nil || v.boundary.msg != "This is the last change of a.txt." {
		t.Fatalf("stayed: current %d, boundary %+v", v.current, v.boundary)
	}
	v.stepHunk(1, crossNever)
	if v.current != 0 || v.boundary == nil || v.boundary.msg != "This is the last change of a.txt." {
		t.Fatalf("stayed again: current %d, boundary %+v", v.current, v.boundary)
	}
}

// TestDiffStatusSaysWhereTheChangeIs: the status bar says which change
// of how many the diff is at, and what the navigation said at an end.
func TestDiffStatusSaysWhereTheChangeIs(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\nfive\nsix\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "two files")
	os.WriteFile(hs.path("a.txt"), []byte("one\n2\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\n6\nsix\n"), 0o644)
	v := diffNavOf(t, hs)
	if s := v.diffStatus(); s != "2 files · 2 changes · +2 −2" {
		t.Fatalf("status %q", s)
	}
	// With a change gone to, it says which of how many.
	v.stepHunk(1, crossAsk)
	if s := v.diffStatus(); !strings.HasPrefix(s, "Change 1 of 2 · ") || !strings.HasSuffix(s, "2 files · 2 changes · +2 −2") {
		t.Fatalf("status %q", s)
	}
	// At an end of a file, what the navigation said takes its place.
	v.stepHunk(1, crossAsk)
	if s := v.diffStatus(); s != v.boundary.msg {
		t.Fatalf("status %q, boundary %q", s, v.boundary.msg)
	}
	// Crossing to the next file counts on.
	v.stepHunk(1, crossAsk)
	if s := v.diffStatus(); !strings.HasPrefix(s, "Change 2 of 2 · ") {
		t.Fatalf("status %q", s)
	}
}

// TestDiffFileNavigationSaysAtTheEnds: the file navigation of a diff says
// where it is at the first and last file, and jumps no further.
func TestDiffFileNavigationSaysAtTheEnds(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\nfive\nsix\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "two files")
	os.WriteFile(hs.path("a.txt"), []byte("one\n2\nthree\n"), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte("four\n6\nsix\n"), 0o644)
	v := diffNavOf(t, hs)
	v.cursor = -1
	v.scroll.Y = 0
	// To the next file, and at the last file it says so, and jumps no
	// further.
	v.stepFile(1)
	if v.cursor != v.heads[1] || v.boundary != nil {
		t.Fatalf("next file: cursor %d, boundary %v", v.cursor, v.boundary)
	}
	v.stepFile(1)
	if v.cursor != v.heads[1] || v.boundary == nil || v.boundary.msg != "This is the last file of the diff." {
		t.Fatalf("end: cursor %d, boundary %+v", v.cursor, v.boundary)
	}
	v.stepFile(1)
	if v.cursor != v.heads[1] || v.boundary == nil || v.boundary.msg != "This is the last file of the diff." {
		t.Fatalf("end again: cursor %d, boundary %+v", v.cursor, v.boundary)
	}
	// Back to the first file, and at the first it says so.
	v.stepFile(-1)
	if v.cursor != v.heads[0] || v.boundary != nil {
		t.Fatalf("previous file: cursor %d, boundary %v", v.cursor, v.boundary)
	}
	v.stepFile(-1)
	if v.cursor != v.heads[0] || v.boundary == nil || v.boundary.msg != "This is the first file of the diff." {
		t.Fatalf("start: cursor %d, boundary %+v", v.cursor, v.boundary)
	}
}

// TestDiffJumpToSourceKeepsPosition: opening the file a diff shows, from
// the change gone to, from the header of the change, after the layout
// changed, after the file folded and opened again, and after the sides
// read again, lands the caret on the line of the change each time.
func TestDiffJumpToSourceKeepsPosition(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	var body strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&body, "line %d\n", i)
	}
	os.WriteFile(hs.path("a.txt"), []byte(body.String()), 0o644)
	git("add", ".")
	git("commit", "-qm", "lines")
	lines := strings.Split(strings.TrimSuffix(body.String(), "\n"), "\n")
	lines[9] = "line ten"
	os.WriteFile(hs.path("a.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	v := diffNavOf(t, hs)
	path := hs.path("a.txt")
	if len(v.files) != 1 || len(v.starts) != 1 {
		t.Fatalf("files %d, changes %d", len(v.files), len(v.starts))
	}
	// The diff comes back to the front, painted, without the keyboard.
	back := func() {
		t.Helper()
		if col, i := hs.a.changesTab(); col != nil {
			hs.a.activate(col, i, false)
		}
		hs.settle()
	}
	// The caret of the file opened sits on the line of the change.
	at := func(where string) {
		t.Helper()
		hs.a.openDiffFile(v)
		hs.settle()
		d := hs.a.activeEditor()
		if d == nil || d.path != path {
			t.Fatalf("%s: editor %+v", where, d)
		}
		if line := d.buf.Snapshot().LineAt(min(d.caret, d.buf.Snapshot().Len())); line != 9 {
			t.Fatalf("%s: caret on line %d, want 9", where, line)
		}
		back()
	}
	v.stepHunk(1, crossAsk)
	at("from the change")
	// The header of the run of changes sits above its context lines.
	hdr := -1
	for k := v.starts[0]; k >= 0 && v.rows[k].file == v.rows[v.starts[0]].file; k-- {
		if v.rows[k].kind == rowHunk {
			hdr = k
			break
		}
	}
	if hdr < 0 {
		t.Fatal("no header of the change")
	}
	v.setCursor(hdr)
	at("from the header of the change")
	hs.a.setDiffLayout(v, layoutSplit)
	at("side by side")
	hs.a.setDiffLayout(v, layoutInline)
	at("inline")
	f := v.files[0]
	f.folded = true
	v.changed()
	hs.settle()
	if v.cursor != v.heads[0] {
		t.Fatalf("folded: cursor %d, want %d", v.cursor, v.heads[0])
	}
	at("folded")
	f.folded = false
	v.changed()
	hs.settle()
	at("opened again")
	f.gitStale = true
	hs.a.refreshDiff(v)
	hs.settle()
	hs.settle()
	at("read again")
}
