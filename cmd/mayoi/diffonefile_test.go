package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/git"
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

// TestDiffOneFileCommit: a commit's tab with one file at a time shows its
// first file alone, named with the commit, and a file revealed from the
// panel of its files instead.
func TestDiffOneFileCommit(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.settings.DiffFiles = "one"
	c := a.git.history[0]
	a.showCommit(c, true)
	hs.settleGit()
	v := a.activeTab().diff
	first := hs.settleShown(v)
	if len(v.files) < 2 || first != v.files[0] {
		t.Fatalf("files %d, shown %v", len(v.files), first)
	}
	if got := rowFiles(v); len(got) != 1 || got[0] != first.path {
		t.Fatalf("rows of %v", got)
	}
	if want := filepath.Base(first.path) + " · " + git.ShortHash(c.Hash); v.name() != want {
		t.Fatalf("name %q, want %q", v.name(), want)
	}
	last := v.files[len(v.files)-1]
	v.revealFile(last)
	if f := hs.settleShown(v); f != last {
		t.Fatalf("revealed %s, shows %s", last.path, f.path)
	}
	if got := rowFiles(v); len(got) != 1 || got[0] != last.path {
		t.Fatalf("revealed: rows of %v", got)
	}
}
