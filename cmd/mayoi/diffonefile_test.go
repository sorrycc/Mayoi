package main

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// settleShown waits for the file a diff showing one file at a time shows
// to be read, and returns it.
func (hs *harness) settleShown(v *diffView) *diffFile {
	hs.t.Helper()
	for range 200 {
		hs.settle()
		if f := v.shownFile(); f != nil && f.loaded && !f.running && !f.waiting && !f.gitStale {
			return f
		}
	}
	hs.t.Fatal("the file shown never settled")
	return nil
}

// oneFileDiffOf opens the tab of the changes of a work tree whose files
// a.txt, b.txt and c.txt changed, a.txt in two places, one file at a time.
func oneFileDiffOf(t *testing.T) (*harness, *diffView) {
	t.Helper()
	hs := newHarness(t, true)
	git := hs.gitRepo()
	long := func(changed ...int) string {
		var b strings.Builder
		for i := range 20 {
			if i == changed[0] || len(changed) > 1 && i == changed[1] {
				b.WriteString("changed\n")
				continue
			}
			b.WriteString("line\n")
		}
		return b.String()
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(hs.path(name), []byte(long(-1)), 0o644)
	}
	git("add", ".")
	git("commit", "-qm", "three files")
	os.WriteFile(hs.path("a.txt"), []byte(long(1, 18)), 0o644)
	os.WriteFile(hs.path("b.txt"), []byte(long(5)), 0o644)
	os.WriteFile(hs.path("c.txt"), []byte(long(9)), 0o644)
	hs.a.settings.DiffFiles = "one"
	hs.settleGit()
	hs.a.run("git.openAllChanges")
	hs.settle()
	v := hs.a.activeTab().diff
	if v == nil {
		t.Fatal("no tab of the changes")
	}
	hs.settleShown(v)
	return hs, v
}

// rowFiles returns the files a diff's rows are of, each once.
func rowFiles(v *diffView) []string {
	var out []string
	for _, r := range v.rows {
		if r.kind == rowCommit {
			continue
		}
		p := v.files[r.file].path
		if len(out) == 0 || out[len(out)-1] != p {
			out = append(out, p)
		}
	}
	return out
}

// TestDiffOneFileShowsFileAlone: with one file at a time, the tab lays out
// and reads the file shown alone, is named after it, and the next file
// and a file revealed from Source Control show instead of it.
func TestDiffOneFileShowsFileAlone(t *testing.T) {
	hs, v := oneFileDiffOf(t)
	if len(v.files) != 3 {
		t.Fatalf("files %d", len(v.files))
	}
	if got := rowFiles(v); len(got) != 1 || got[0] != hs.path("a.txt") {
		t.Fatalf("rows of %v", got)
	}
	if v.name() != "a.txt" {
		t.Fatalf("name %q", v.name())
	}
	for _, f := range v.files[1:] {
		if f.loaded || f.running {
			t.Fatalf("%s read while not shown", f.path)
		}
	}
	hs.shot("diff-one-file")
	v.stepFile(1)
	hs.settleShown(v)
	if got := rowFiles(v); len(got) != 1 || got[0] != hs.path("b.txt") {
		t.Fatalf("next file: rows of %v", got)
	}
	if v.cursor < 0 || v.rows[v.cursor].kind != rowFile {
		t.Fatalf("cursor %d not at the header", v.cursor)
	}
	hs.a.showChanges(diffTarget{path: hs.path("c.txt"), base: diffIndex}, true)
	hs.settleShown(v)
	if got := rowFiles(v); len(got) != 1 || got[0] != hs.path("c.txt") {
		t.Fatalf("revealed: rows of %v", got)
	}
	// At the last file it says so, and stays.
	v.stepFile(1)
	hs.settle()
	if v.boundary == nil || v.boundary.msg != "This is the last file of the diff." || v.shownFile().path != hs.path("c.txt") {
		t.Fatalf("last file: boundary %+v, shown %s", v.boundary, v.shownFile().path)
	}
}

// TestDiffOneFileChangeNavigation: with one file at a time, the change
// navigation stays in the file shown, and at its edge steps into the next
// file, or the previous, as diffCrossFiles says, landing on its first
// change going forward and its last going back.
func TestDiffOneFileChangeNavigation(t *testing.T) {
	hs, v := oneFileDiffOf(t)
	if len(v.starts) != 2 {
		t.Fatalf("changes of a.txt %d", len(v.starts))
	}
	v.stepHunk(1, crossAsk)
	v.stepHunk(1, crossAsk)
	if v.current != 1 {
		t.Fatalf("current %d", v.current)
	}
	// The last change of a.txt: it says so, and stays.
	v.stepHunk(1, crossAsk)
	hs.settle()
	if v.boundary == nil || !strings.Contains(v.boundary.msg, "last change of a.txt") || v.shownFile().path != hs.path("a.txt") {
		t.Fatalf("edge: boundary %+v, shown %s", v.boundary, v.shownFile().path)
	}
	// One more step shows b.txt at its change.
	v.stepHunk(1, crossAsk)
	hs.settleShown(v)
	if got := rowFiles(v); len(got) != 1 || got[0] != hs.path("b.txt") {
		t.Fatalf("crossed: rows of %v", got)
	}
	if v.current != 0 || v.boundary != nil {
		t.Fatalf("crossed: current %d, boundary %+v", v.current, v.boundary)
	}
	// Going back lands on the last change of a.txt.
	v.stepHunk(-1, crossAlways)
	hs.settleShown(v)
	if v.shownFile().path != hs.path("a.txt") || v.current != 1 {
		t.Fatalf("back: shown %s, current %d", v.shownFile().path, v.current)
	}
	// Never crossing stays in the file.
	v.stepHunk(1, crossNever)
	hs.settle()
	if v.shownFile().path != hs.path("a.txt") || v.boundary == nil {
		t.Fatalf("never: shown %s, boundary %+v", v.shownFile().path, v.boundary)
	}
	// At the first change of the diff it says so, and stays.
	v.stepHunk(-1, crossAlways)
	v.stepHunk(-1, crossAlways)
	hs.settle()
	if v.shownFile().path != hs.path("a.txt") || v.current != 0 || v.boundary == nil ||
		v.boundary.msg != "This is the first change of the diff." {
		t.Fatalf("start: shown %s, current %d, boundary %+v", v.shownFile().path, v.current, v.boundary)
	}
}

// TestDiffOneFileStagedShowsNext: with one file at a time, staging the
// file shown shows the next file of the changes not staged.
func TestDiffOneFileStagedShowsNext(t *testing.T) {
	hs, v := oneFileDiffOf(t)
	v.stepFile(1)
	hs.settleShown(v)
	if out, err := exec.Command("git", "-C", hs.root, "add", "b.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	hs.settleGit()
	f := hs.settleShown(v)
	if f.path != hs.path("c.txt") || f.base != diffIndex {
		t.Fatalf("after staging: shown %s, base %d", f.path, f.base)
	}
}

// TestDiffOneFileToggle: turning one file at a time off shows every file
// again, and on, the file at the top of the view alone.
func TestDiffOneFileToggle(t *testing.T) {
	hs, v := oneFileDiffOf(t)
	v.stepFile(1)
	hs.settleShown(v)
	hs.a.setDiffFiles(v, false)
	hs.settleDiff(v)
	if got := rowFiles(v); len(got) != 3 {
		t.Fatalf("every file: rows of %v", got)
	}
	hs.shot("diff-all-files")
	if v.shownFile() != nil || !strings.HasPrefix(v.name(), "Changes") {
		t.Fatalf("every file: name %q", v.name())
	}
	hs.a.setDiffFiles(v, true)
	hs.settleShown(v)
	if got := rowFiles(v); len(got) != 1 || got[0] != hs.path("b.txt") {
		t.Fatalf("one again: rows of %v", got)
	}
}

// TestDiffOneFileCommit: one file at a time leaves a commit's changes be:
// its tab, kept or previewed, lists every file of it, folded and named
// after the commit, a file revealed scrolls to it, and turning one file at
// a time on from the tab of the changes leaves an open commit's tab as it
// was.
func TestDiffOneFileCommit(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	var paths []string
	for _, name := range []string{"x.txt", "y.txt", "z.txt"} {
		var b strings.Builder
		for i := range 60 {
			b.WriteString(name + " line " + strconv.Itoa(i) + "\n")
		}
		os.WriteFile(hs.path(name), []byte(b.String()), 0o644)
		paths = append(paths, hs.path(name))
	}
	git("add", ".")
	git("commit", "-qm", "three long files")
	os.WriteFile(hs.path("x.txt"), []byte("changed\n"), 0o644)
	hs.settleGit()
	c := a.git.history[0]
	check := func(v *diffView, what string) {
		t.Helper()
		if got := rowFiles(v); !slices.Equal(got, paths) {
			t.Fatalf("%s: rows of %v", what, got)
		}
		if v.full || v.built.one || v.shownFile() != nil || v.name() != c.Subject {
			t.Fatalf("%s: full %v, one %v, name %q", what, v.full, v.built.one, v.name())
		}
		if oneFileFits(v) {
			t.Fatalf("%s: the Layout menu offers one file at a time", what)
		}
	}
	a.settings.DiffFiles = "one"
	pv := a.previewCommit(c)
	hs.settleDiff(pv)
	check(pv, "preview")
	a.settings.DiffFiles = ""

	v := a.showCommit(c, true)
	hs.settleDiff(v)
	check(v, "every file")
	changes := a.openChangesTab(true)
	hs.settleDiff(changes)
	a.setDiffFiles(changes, true)
	hs.settleShown(changes)
	if !changes.full || !changes.built.one {
		t.Fatalf("changes: full %v, one %v", changes.full, changes.built.one)
	}
	a.showCommit(c, true)
	hs.settleDiff(v)
	check(v, "turned on from the changes")

	last := v.files[len(v.files)-1]
	v.revealFile(last)
	hs.settle()
	hs.settle()
	check(v, "revealed")
	if r := v.rows[v.focusRowAt(v.topRow(a.font.lineH))]; v.files[r.file] != last {
		t.Fatalf("revealed %s, top shows %s", last.path, v.files[r.file].path)
	}
}

// TestDiffOneFileShowsWholeFile: with one file at a time, the file shown
// shows every line of it, the unchanged ones too, until folded; turning
// one file at a time on shows the whole file, and every file folds.
func TestDiffOneFileShowsWholeFile(t *testing.T) {
	hs, v := oneFileDiffOf(t)
	whole := func() bool {
		lines := 0
		for _, r := range v.rows {
			if r.kind == rowGap || r.kind == rowHunk {
				return false
			}
			if r.kind == rowLine && r.new >= 0 {
				lines++
			}
		}
		return lines == 20
	}
	if !v.full || !whole() {
		t.Fatalf("one file: full %v, rows %d", v.full, len(v.rows))
	}
	hs.a.setDiffFull(v, false)
	hs.settleShown(v)
	if whole() {
		t.Fatal("folded: every line shows")
	}
	hs.a.setDiffFiles(v, false)
	hs.settleDiff(v)
	if v.full {
		t.Fatal("every file: full")
	}
	hs.a.setDiffFiles(v, true)
	hs.settleShown(v)
	if !v.full || !whole() {
		t.Fatalf("one file again: full %v, rows %d", v.full, len(v.rows))
	}
}
