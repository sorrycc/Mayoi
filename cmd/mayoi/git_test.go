package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	gitpkg "mayoi/internal/git"
	"mayoi/internal/linediff"
)

// gitRepo makes the harness's folder a work tree with all its files
// committed, and returns a function that runs git there.
func (hs *harness) gitRepo() func(args ...string) string {
	t := hs.t
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", hs.root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	os.WriteFile(hs.path(".gitignore"), []byte("node_modules/\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "first")
	hs.a.openFolder(hs.root)
	hs.settleGit()
	return git
}

// settleGit draws frames until the status read in the background is in.
func (hs *harness) settleGit() {
	hs.a.gitChanged()
	hs.a.git.checked = time.Time{} // look at the refs now
	for range 200 {
		hs.settle()
		if g := &hs.a.git; g.looked && !g.running && !g.stale && g.busy == "" && g.pending == 0 &&
			(g.repo == nil || g.historyOf == hs.a.historyKey()) {
			return
		}
	}
	g := &hs.a.git
	hs.t.Fatalf("git status never settled: looked %v running %v stale %v busy %q pending %d history %q want %q",
		g.looked, g.running, g.stale, g.busy, g.pending, g.historyOf, hs.a.historyKey())
}

func TestExplorerShowsGitState(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	os.WriteFile(hs.path("main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(hs.path("internal/a/new.go"), []byte("package a\n"), 0o644)
	hs.a.files.Open.Add(hs.path("internal"))
	hs.settleGit()
	a := hs.a
	if b := a.branch(); b != "main" {
		t.Errorf("branch %q", b)
	}
	if e := a.git.entries[hs.path("main.go")]; e.Letter() != 'M' {
		t.Errorf("main.go: %q", e.Letter())
	}
	if e := a.git.entries[hs.path("internal/a/new.go")]; e.Letter() != 'U' {
		t.Errorf("new.go: %q", e.Letter())
	}
	if l := a.git.dirs[hs.path("internal")]; l != 'U' {
		t.Errorf("internal: %q", l)
	}
	if !hs.tt.HasText("M") {
		t.Errorf("no M badge: %q", hs.tt.Texts())
	}
	hs.shot("git-explorer")
	git("add", ".")
	git("commit", "-q", "-m", "second")
	hs.settleGit()
	if len(a.git.entries) != 0 {
		t.Errorf("after the commit: %v", a.git.entries)
	}
	if strings.Contains(strings.Join(hs.tt.Texts(), " "), " U ") {
		t.Errorf("a badge stays")
	}
}

func TestSourceControlStagesAndCommits(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(hs.path("notes.txt"), []byte("hi\n"), 0o644)
	os.Remove(hs.path("go.mod"))
	a.run("view.scm")
	hs.settleGit()
	for _, s := range []string{"SOURCE CONTROL", "CHANGES", "UNTRACKED", "main.go", "notes.txt", "go.mod"} {
		if !hs.tt.HasText(s) {
			t.Errorf("no %q: %q", s, hs.tt.Texts())
		}
	}
	hs.shot("git-scm")
	a.stage(hs.path("main.go"))
	hs.settleGit()
	if g := a.groupEntries(); len(g[groupStaged]) != 1 || len(g[groupChanges]) != 1 {
		t.Fatalf("groups %v", g)
	}
	// Committing what is staged.
	a.git.message = "change main"
	a.commit(false)
	hs.settleGit()
	if log := git("log", "--format=%s", "-1"); strings.TrimSpace(log) != "change main" {
		t.Fatalf("log %q", log)
	}
	if a.git.message != "" {
		t.Errorf("message %q stays", a.git.message)
	}
	// Nothing staged: committing asks to stage everything.
	a.git.message = "the rest"
	a.commit(false)
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if n := a.changeCount(); n != 0 {
		t.Fatalf("%d changes after committing all: %v", n, a.git.status.Entries)
	}
	// Amending takes the last message.
	a.startAmend()
	hs.settleGit()
	if a.git.message != "the rest" || !a.git.amend {
		t.Fatalf("amending: %q %v", a.git.message, a.git.amend)
	}
	a.git.message = "the rest, amended"
	a.commit(false)
	hs.settleGit()
	if log := git("log", "--format=%s"); log != "the rest, amended\nchange main\nfirst\n" {
		t.Fatalf("log %q", log)
	}
}

func TestDiscardChanges(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("broken\n"), 0o644)
	os.WriteFile(hs.path("junk.txt"), []byte("junk\n"), 0o644)
	hs.settleGit()
	g := a.groupEntries()
	a.discard(append(g[groupChanges], g[groupUntracked]...)...)
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	hs.settleGit()
	if data, _ := os.ReadFile(hs.path("main.go")); !strings.HasPrefix(string(data), "package main") {
		t.Errorf("main.go: %q", data)
	}
	if _, err := os.Stat(hs.path("junk.txt")); err == nil {
		t.Error("junk.txt stays")
	}
	if n := a.changeCount(); n != 0 {
		t.Errorf("%d changes", n)
	}
}

func hunk(a, alen, b, blen int) linediff.Hunk {
	return linediff.Hunk{A: a, ALen: alen, B: b, BLen: blen}
}

func TestGutterHunkActions(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	before, _ := os.ReadFile(hs.path("main.go"))
	os.WriteFile(hs.path("main.go"), []byte(strings.Replace(string(before), "hello, mayoi", "hi", 1)+"// more\n"), 0o644)
	a.openFile(hs.path("main.go"), true)
	hs.settleGit()
	d := a.activeEditor()
	if hs := editorHunks(d); len(hs) != 2 {
		t.Fatalf("hunks %v", hs)
	}
	// A click on the mark of line 7 shows what it was.
	x := a.settings.SidebarWidth + d.gutterW - 5
	y := titleBarH + tabStripH + crumbsH + editorPadY + 6.5*a.font.lineH
	hs.tt.ClickAt(x, y)
	hs.settle()
	if h, k := d.peekedHunk(); k != 0 || h.B != 6 {
		t.Fatalf("peeked %v %d (peek %d)", h, k, d.peek)
	}
	if !hs.tt.HasText("\tfmt.Println(\"hello, mayoi\")") && !hs.tt.HasText("    fmt.Println(\"hello, mayoi\")") {
		t.Errorf("the old line does not show: %q", hs.tt.Texts())
	}
	hs.shot("git-hunk-peek")
	// Staging it leaves the other in the work tree only.
	h, _ := d.peekedHunk()
	a.stageHunk(d, h)
	hs.settleGit()
	if staged := git("diff", "--cached", "--numstat"); strings.TrimSpace(staged) != "1\t1\tmain.go" {
		t.Errorf("staged %q", staged)
	}
	if unstaged := git("diff", "--numstat"); strings.TrimSpace(unstaged) != "1\t0\tmain.go" {
		t.Errorf("not staged %q", unstaged)
	}
	// Reverting the other takes the line added out, as a step of undo.
	a.queueEdit(d, func(d *editor) { a.onCaretHunk(d, a.revertHunk) })
	d.selectRange(d.buf.Len()-2, d.buf.Len()-2, false)
	hs.settle()
	a.queueEdit(d, func(d *editor) { a.onCaretHunk(d, a.revertHunk) })
	hs.settle()
	if strings.Contains(d.buf.String(), "// more") {
		t.Errorf("not reverted: %q", d.buf.String())
	}
	d.undo()
	if !strings.Contains(d.buf.String(), "// more") {
		t.Errorf("undo: %q", d.buf.String())
	}
	// Going from change to change.
	d.selectRange(0, 0, false)
	hs.settle()
	a.stepChange(1)
	hs.settle()
	if l := caretLine(d); l != 6 {
		t.Errorf("next change at line %d", l)
	}
	a.stepChange(1)
	hs.settle()
	if l := caretLine(d); l != 8 {
		t.Errorf("next change at line %d", l)
	}
}

func TestBranchesAndBlame(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.askNewBranch()
	if a.prompt == nil {
		t.Fatal("no prompt")
	}
	if msg := a.prompt.done("main"); msg == "" {
		t.Error("made a branch that exists")
	}
	if msg := a.prompt.done("topic"); msg != "" {
		t.Fatal(msg)
	}
	a.prompt = nil
	hs.settleGit()
	if b := a.branch(); b != "topic" || !slices.Equal(a.git.branches, []string{"main", "topic"}) {
		t.Fatalf("branch %q of %v", b, a.git.branches)
	}
	if !hs.tt.HasText("topic") {
		t.Errorf("the status bar lacks the branch: %q", hs.tt.Texts())
	}
	// A commit on the branch, then back to main, which has the old text.
	os.WriteFile(hs.path("README.md"), []byte("# Topic\n"), 0o644)
	git("commit", "-qam", "topic work")
	a.openFile(hs.path("README.md"), true)
	hs.settleGit()
	d := a.activeEditor()
	if d.buf.String() != "# Topic\n" {
		t.Fatalf("text %q", d.buf.String())
	}
	a.switchBranch("main", false)
	hs.settleGit()
	hs.until("the file reloads", func() bool { return d.buf.String() == "# Mayoi\n\nA small editor.\n" })
	if b := a.branch(); b != "main" {
		t.Fatalf("branch %q", b)
	}
	// The caret's line, at rest, says who changed it last.
	d.selectRange(2, 2, false)
	hs.settle()
	hs.until("the line is blamed", func() bool {
		hs.tt.Frame()
		return d.blame.done
	})
	if !strings.HasPrefix(d.blame.text, "t, just now · first") {
		t.Errorf("blame %q", d.blame.text)
	}
	hs.shot("git-blame")
	// A line typed is not committed.
	a.queueEdit(d, func(d *editor) { d.edit(d.sel, textEdit{0, 0, "new\n"}) })
	hs.settle()
	hs.until("the new line is blamed", func() bool {
		hs.tt.Frame()
		return d.blame.done
	})
	if d.blame.text != "" {
		t.Errorf("blame of a new line %q", d.blame.text)
	}
}

func TestPushPullFetch(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	git("remote", "add", "origin", remote)
	// The first push makes the upstream.
	a.push()
	hs.settleGit()
	if st := a.git.status; st.Upstream != "origin/main" {
		t.Fatalf("upstream %q", st.Upstream)
	}
	// Commit & Push.
	os.WriteFile(hs.path("README.md"), []byte("# Changed\n"), 0o644)
	hs.settleGit()
	a.stage(hs.path("README.md"))
	hs.settleGit()
	a.git.message = "change the readme"
	a.commit(true)
	hs.settleGit()
	hs.settleGit()
	if st := a.git.status; st.Ahead != 0 {
		t.Fatalf("ahead %d after commit & push", st.Ahead)
	}
	if out, _ := exec.Command("git", "-C", remote, "log", "--format=%s", "-1", "main").Output(); strings.TrimSpace(string(out)) != "change the readme" {
		t.Fatalf("remote has %q", out)
	}
	// A commit elsewhere: fetching tells, pulling brings it.
	other := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", "-b", "main", remote, other).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	os.WriteFile(other+"/go.mod", []byte("module other\n"), 0o644)
	for _, args := range [][]string{{"commit", "-qam", "other"}, {"push", "-q"}} {
		cmd := exec.Command("git", append([]string{"-C", other, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	a.fetch()
	hs.settleGit()
	if st := a.git.status; st.Behind != 1 || a.branchLabel() != "main ↓1" {
		t.Fatalf("behind %d, label %q", st.Behind, a.branchLabel())
	}
	a.pull()
	hs.settleGit()
	if st := a.git.status; st.Behind != 0 {
		t.Fatalf("behind %d after pulling", st.Behind)
	}
	if data, _ := os.ReadFile(hs.path("go.mod")); string(data) != "module other\n" {
		t.Fatalf("go.mod %q", data)
	}
	// A remote that cannot be reached says so.
	git("remote", "set-url", "origin", "/nonexistent/repo.git")
	a.errors = nil
	a.fetch()
	for range 100 {
		hs.settle()
		if a.git.busy == "" {
			break
		}
	}
	hs.tt.Frame()
	if !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, "nonexistent") }) {
		t.Errorf("no toast: %q", hs.tt.Texts())
	}
}

func TestRenamesAndDiscardingOpenFiles(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	git("mv", "go.mod", "go2.mod")
	os.WriteFile(hs.path("go2.mod"), []byte("module example\n\ngo 1.27\n\n// renamed\n"), 0o644)
	hs.settleGit()
	g := a.groupEntries()
	if len(g[groupStaged]) != 1 || len(g[groupChanges]) != 1 {
		t.Fatalf("groups %+v", g)
	}
	a.stageAll()
	hs.settleGit()
	if g = a.groupEntries(); len(g[groupChanges]) != 0 {
		t.Fatalf("after staging all: %+v", g)
	}
	// Unstaging a file renamed puts back where it was.
	a.toggleStaged(scmRow{group: groupStaged, e: g[groupStaged][0]})
	hs.settleGit()
	if st := strings.TrimSpace(git("status", "--short")); st != "D go.mod\n?? go2.mod" {
		t.Errorf("status %q", st)
	}
	// Discarding the changes of a file open with unsaved edits shows the
	// file as it is.
	git("checkout", "-q", "--", "go.mod")
	a.openFile(hs.path("main.go"), true)
	os.WriteFile(hs.path("main.go"), []byte("package main // on disk\n"), 0o644)
	hs.settleGit()
	d := a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, "// typed\n"})
	if !d.dirty() {
		t.Fatal("not dirty")
	}
	for _, e := range a.groupEntries()[groupChanges] {
		if e.Path == hs.path("main.go") {
			a.discard(e)
		}
	}
	a.confirm.run()
	hs.settleGit()
	if d.dirty() || !strings.HasPrefix(d.buf.String(), "package main\n") {
		t.Errorf("dirty %v, text %q", d.dirty(), d.buf.String())
	}
	d.undo()
	if !strings.HasPrefix(d.buf.String(), "// typed") {
		t.Errorf("undo: %q", d.buf.String())
	}
}

// diffFile returns the file of the tab of the changes compared against
// base, nil when it has none.
func diffFileOf(v *diffView, path string, base diffBase) *diffFile {
	for _, f := range v.files {
		if f.path == path && f.base == base {
			return f
		}
	}
	return nil
}

// kinds spells the rows of a diff, a letter each.
func kinds(v *diffView) string {
	var s strings.Builder
	for _, r := range v.rows {
		s.WriteByte("LGHSFNsC"[r.kind])
	}
	return s.String()
}

func TestDiffTab(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nimport \"fmt\"\n\n// main says hi.\nfunc main() {\n\tfmt.Println(\"hi, mayoi\")\n\tfmt.Println(\"bye\")\n}\n"), 0o644)
	hs.settleGit()
	a.run("view.scm")
	hs.settle()
	if err := hs.tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	tab := a.activeTab()
	if tab == nil || tab.diff == nil {
		t.Fatalf("tab %+v", tab)
	}
	v := tab.diff
	file := diffFileOf(v, hs.path("main.go"), diffIndex)
	if file == nil {
		t.Fatalf("files %v", v.files)
	}
	want := []linediff.Hunk{hunk(4, 1, 4, 1), hunk(6, 1, 6, 2)}
	if !slices.Equal(file.hunks, want) {
		t.Fatalf("hunks %v, want %v", file.hunks, want)
	}
	if s := v.diffStatus(); s != "1 file · 2 changes · +3 −2" {
		t.Errorf("status %q", s)
	}
	if got := kinds(v); got != "SFHLLLLLLLLLLL" {
		t.Errorf("rows %s", got)
	}
	// The click chose the file's header.
	if v.cursor != 1 {
		t.Errorf("cursor %d", v.cursor)
	}
	hs.shot("git-diff-unified")
	a.setDiffLayout(v, layoutSplit)
	hs.settle()
	if !v.split || len(file.pairs) != 9 || len(file.unified) != 11 {
		t.Errorf("rows %d %d", len(file.pairs), len(file.unified))
	}
	hs.shot("git-diff-split")
	// Typing in the file changes the diff.
	a.openFile(hs.path("main.go"), true)
	d := a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, "// top\n"})
	col, i := a.changesTab()
	a.activate(col, i, true)
	time.Sleep(200 * time.Millisecond)
	hs.settle()
	if len(file.hunks) != 3 || file.hunks[0] != hunk(0, 0, 0, 1) {
		t.Errorf("after typing: %v", file.hunks)
	}
	// Open Changes in the editor comes back to the same tab.
	a.openFile(hs.path("main.go"), true)
	a.openChanges()
	hs.settle()
	if tab := a.activeTab(); tab == nil || tab.diff != v {
		t.Errorf("open changes: %+v", tab)
	}
	v.current = -1
	v.stepHunk(1)
	v.stepHunk(1)
	v.stepHunk(1)
	if v.current != 2 || v.jump != v.starts[2] {
		t.Errorf("current %d jump %d", v.current, v.jump)
	}
	// It is kept, and comes back.
	a.rememberTabs()
	found := false
	for _, ts := range a.window.Columns[0].Tabs {
		found = found || ts.Diff != "" && ts.Layout == "split"
	}
	if !found {
		t.Errorf("tabs kept %+v", a.window.Columns)
	}
	a.closeActive()
	hs.settle()
	a.reopenClosed()
	hs.settle()
	if tab := a.activeTab(); tab == nil || tab.diff == nil || tab.diff.layout != layoutSplit {
		t.Errorf("reopened %+v", tab)
	}
}

func TestDiffScrollsToTheFileChosen(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	// Files enough that most are out of view.
	for i := range 30 {
		os.WriteFile(hs.path(fmt.Sprintf("f%02d.txt", i)), []byte("one\ntwo\nthree\n"), 0o644)
	}
	git("add", ".")
	git("commit", "-qm", "files")
	for i := range 30 {
		os.WriteFile(hs.path(fmt.Sprintf("f%02d.txt", i)), []byte(fmt.Sprintf("one\n%d\nthree\n", i)), 0o644)
	}
	git("add", "f05.txt")
	os.WriteFile(hs.path("f05.txt"), []byte("one\nfive\nthree\nfour\n"), 0o644)
	hs.settleGit()
	a.run("view.scm")
	hs.settle()
	// Staged first, then the rest.
	a.showChanges(diffTarget{path: hs.path("f25.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	if len(v.files) != 31 || v.files[0].base != diffStaged || v.files[1].base != diffIndex {
		t.Fatalf("%d files", len(v.files))
	}
	at := v.heads[26]
	if v.cursor != at || v.files[v.rows[at].file].path != hs.path("f25.txt") {
		t.Fatalf("cursor %d, want %d", v.cursor, at)
	}
	// In view: at the top, unless the end of the files was in view.
	if top := editorPadY + float32(v.tops[at])*a.font.lineH; top < v.scroll.Y || top > v.scroll.Y+v.viewH {
		t.Errorf("scrolled to %v, header at %v", v.scroll.Y, top)
	}
	// Only the files near the view were read.
	if f := diffFileOf(v, hs.path("f02.txt"), diffIndex); f.loaded {
		t.Errorf("a file far from the view was read")
	}
	if f := diffFileOf(v, hs.path("f25.txt"), diffIndex); !f.loaded {
		t.Errorf("the file chosen was not read: near %v, scroll %v, rows %d", v.near, v.scroll.Y, len(v.rows))
	}
	// The source control view chose it too.
	if r := a.scm.rows[a.scm.row]; r.e.Path != hs.path("f25.txt") {
		t.Errorf("scm chose %q", r.e.Path)
	}
	// A file both staged and changed since: its staged changes, or those
	// not, as the group clicked.
	a.showChanges(diffTarget{path: hs.path("f05.txt"), base: diffStaged}, true)
	hs.settleGit()
	if f := v.files[v.rows[v.cursor].file]; f.path != hs.path("f05.txt") || f.base != diffStaged {
		t.Errorf("staged: %+v", v.rows[v.cursor])
	}
	a.showChanges(diffTarget{path: hs.path("f05.txt"), any: true}, true)
	hs.settleGit()
	if f := v.files[v.rows[v.cursor].file]; f.path != hs.path("f05.txt") || f.base != diffIndex {
		t.Errorf("any: %+v", v.rows[v.cursor])
	}
	// A section.
	a.showChanges(diffTarget{base: diffIndex}, true)
	hs.settleGit()
	if r := v.rows[v.cursor]; r.kind != rowSection || v.files[r.file].base != diffIndex {
		t.Errorf("section: %+v", r)
	}
	// A file folded above the view keeps the rows in view where they were.
	a.showChanges(diffTarget{path: hs.path("f10.txt"), base: diffIndex}, true)
	hs.settleGit()
	place := func() float32 { return editorPadY + float32(v.tops[v.cursor])*a.font.lineH - v.scroll.Y }
	was := place()
	if was != 0 {
		t.Errorf("f10 at %v", was)
	}
	diffFileOf(v, hs.path("f08.txt"), diffIndex).folded = true
	v.changed()
	hs.settle()
	if f := v.files[v.rows[v.cursor].file]; f.path != hs.path("f10.txt") || place() != was {
		t.Errorf("moved: %v → %v", was, place())
	}
	// A file with no changes says so.
	a.showChanges(diffTarget{path: hs.path("go.mod"), any: true}, true)
	hs.settle()
	if len(a.errors) == 0 && !hs.tt.HasText("go.mod has no changes.") {
		t.Errorf("no word of no changes")
	}
}

func TestDiffFolds(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	long := hs.path("long.go")
	var b strings.Builder
	b.WriteString("package main\n\nfunc long() {\n")
	for i := range 100 {
		fmt.Fprintf(&b, "\tprintln(%d)\n", i)
	}
	b.WriteString("}\n")
	os.WriteFile(long, []byte(b.String()), 0o644)
	git("add", ".")
	git("commit", "-qm", "long")
	data, _ := os.ReadFile(long)
	os.WriteFile(long, []byte(strings.Replace(string(data), "println(50)", "println(\"fifty\")", 1)), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: long, any: true}, true)
	hs.settleGit()
	v := a.activeTab().diff
	if got := kinds(v); got != "SFGHLLLLLLLLG" {
		t.Fatalf("rows %s", got)
	}
	if g := v.rows[2]; g.n != 50 || g.gap != 0 {
		t.Errorf("gap %+v", g)
	}
	if h := v.rows[3].label; h != "@@ -51,7 +51,7 @@ func long() {" {
		t.Errorf("header %q", h)
	}
	// Opening a gap shows its lines; z on one of them folds it again.
	a.activateRow(v, 2, false)
	hs.settle()
	if got := kinds(v); got != "SFH"+strings.Repeat("L", 58)+"G" {
		t.Fatalf("opened: %s", got)
	}
	a.toggleGap(v, 10)
	hs.settle()
	if got := kinds(v); got != "SFGHLLLLLLLLG" {
		t.Fatalf("folded again: %s", got)
	}
	// Every line, and no headers, with the full files.
	a.setDiffFull(v, true)
	hs.settle()
	if got := kinds(v); got != "SF"+strings.Repeat("L", 105) {
		t.Errorf("full: %s", got)
	}
	a.setDiffFull(v, false)
	hs.settle()

	// A click on a gap opens it.
	if pane, ok := hs.tt.Find("Diff of the Changes"); ok {
		v.scroll.Y = 0
		hs.settle()
		hs.tt.ClickAt(pane.X+60, pane.Y+editorPadY+(float32(v.tops[2])+0.5)*a.font.lineH)
		hs.settle()
	}
	if got := kinds(v); v.cursor < 0 || v.rows[v.cursor].gap != 0 || got != "SFH"+strings.Repeat("L", 58)+"G" {
		t.Errorf("click on the gap: cursor %d, %s", v.cursor, got)
	}
	a.toggleGap(v, v.cursor)
	hs.settle()

	// The keys of a pager.
	v.pane.Focus()
	v.cursor = -1
	v.scroll.Y = 0
	hs.settle()
	hs.key(0, ui.KeyJ)
	hs.key(0, ui.KeyJ)
	if v.cursor != 2 {
		t.Errorf("cursor %d", v.cursor)
	}
	hs.key(0, ui.KeyBracketRight)
	if v.current != 0 || v.cursor != v.starts[0] {
		t.Errorf("current %d, cursor %d", v.current, v.cursor)
	}
	for range 4 {
		hs.key(0, ui.KeyK)
	}
	hs.key(0, ui.KeyZ) // on the header: nothing to fold
	if got := kinds(v); v.cursor != 3 || got != "SFGHLLLLLLLLG" {
		t.Errorf("z on a header: %d %s", v.cursor, got)
	}
	hs.key(0, ui.KeyK)
	hs.key(0, ui.KeyZ)
	if got := kinds(v); got != "SFH"+strings.Repeat("L", 58)+"G" {
		t.Errorf("z on a gap: %s", got)
	}
	hs.key(0, ui.KeyZ)
	if got := kinds(v); got != "SFGHLLLLLLLLG" {
		t.Errorf("z in an open gap: %s", got)
	}
	// Enter on a file's header folds it.
	v.setCursor(1)
	hs.key(0, ui.KeyEnter)
	if got := kinds(v); got != "SF" {
		t.Errorf("folded file: %s", got)
	}
	hs.key(0, ui.KeyEnter)

	// Lines wrap at the edge of the view.
	if l := a.diffGeometry(v, false, 400); l.cols[0] >= 40 {
		t.Errorf("cols %v", l.cols)
	}
	if wrapCount(strings.Repeat("word ", 40), 30) != 7 || wrapCount("short", 30) != 1 {
		t.Errorf("wrapCount %d", wrapCount(strings.Repeat("word ", 40), 30))
	}
	os.WriteFile(long, []byte(strings.Replace(string(data), "println(50)", "println(\""+strings.Repeat("fifty ", 60)+"\")", 1)), 0o644)
	hs.settleGit()
	a.toggleDiffWrap(v)
	hs.settle()
	tall := 0
	for _, r := range v.rows {
		tall = max(tall, r.h)
	}
	if tall < 3 || v.tops[len(v.rows)] <= len(v.rows) {
		t.Errorf("wrapped: tallest row %d", tall)
	}
	hs.shot("git-diff-wrap")
	a.toggleDiffWrap(v)

	// Side by side when wide enough, unless chosen.
	if a.splits(v, 900) || !a.splits(v, 2400) {
		t.Errorf("auto layout")
	}
	v.layout = layoutInline
	if a.splits(v, 2400) {
		t.Errorf("inline")
	}
	v.layout = layoutAuto
}

func TestDiffUntrackedMarkdown(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	path := hs.path(".changeset/khaki-cars-brake.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "---\n\"@myflicker/amux\": patch\n---\n\n- 新增中文说明\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	hs.a.run("git.openAllChanges")
	hs.settleGit()
	check := func() {
		t.Helper()
		hs.until("Markdown additions", func() bool {
			v := hs.a.activeTab().diff
			for file, f := range v.files {
				if f.path == path && f.err == "" && f.new.text == body {
					for _, row := range v.rows {
						if row.file == file && row.kind == rowLine && row.new >= 0 && f.new.lines[row.new] == "- 新增中文说明" {
							return true
						}
					}
				}
			}
			return false
		})
	}
	check()
	hs.shot("git-diff-untracked-markdown")
	git("add", "--", ".changeset/khaki-cars-brake.md")
	hs.settleGit()
	check()
}

func TestDiffMultilineErrorDoesNotOverlapHeader(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	const first = "fatal: cannot read the file"
	render := func(note string) *ui.Tester {
		v := newDiffView(layoutInline)
		v.files = []*diffFile{{path: hs.path("notes.md"), loaded: true, err: note}}
		tt := ui.NewTester(func(c *ui.Context) { a.diffViewUI(c, v) }, 900, 400)
		tt.Frame()
		return tt
	}
	hs.tt = render(first)
	hs.shot("diff-error-single")
	baseline := hs.tt.Image()
	hs.tt = render(first + "\nCheck the repository state.\nTry again after fixing it.")
	hs.shot("diff-error-multiline")
	got := hs.tt.Image()
	// A three-line error must leave the section and file header as they
	// appeared with a one-line error, rather than painting over them.
	bottom := int(editorPadY + 2*a.font.lineH)
	for y := range bottom {
		for x := range baseline.Bounds().Dx() {
			if got.RGBAAt(x, y) != baseline.RGBAAt(x, y) {
				t.Fatalf("multiline error paints over the header at (%d, %d)", x, y)
			}
		}
	}
	// Each line must still be drawn below the header.
	background := got.RGBAAt(850, 399)
	for line := range 3 {
		top := int(editorPadY + float32(2+line)*a.font.lineH)
		ink := false
		for y := top; y < top+int(a.font.lineH); y++ {
			for x := 90; x < 850; x++ {
				ink = ink || got.RGBAAt(x, y) != background
			}
		}
		if !ink {
			t.Fatalf("error line %d is not visible below the header", line+1)
		}
	}
}

func TestDiffOfEveryFile(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nimport \"fmt\"\n\n// main says hi.\nfunc main() {\n\tfmt.Println(\"hello, mayoi\")\n}\n"), 0o644)
	os.WriteFile(hs.path("README.md"), []byte("# Mayoi\n\nA small editor, with diffs.\n"), 0o644)
	git("add", "README.md")
	os.WriteFile(hs.path("new.txt"), []byte("a new file\n"), 0o644)
	hs.settleGit()
	a.run("git.openAllChanges")
	hs.settleGit()
	v := a.activeTab().diff
	var names []string
	for _, f := range v.files {
		names = append(names, filepath.Base(f.path)+string(f.letter))
	}
	if strings.Join(names, " ") != "README.mdM main.goM new.txtU" {
		t.Fatalf("files %v", names)
	}
	if got := kinds(v); !strings.HasPrefix(got, "SFHLLL") || strings.Count(got, "S") != 2 || strings.Count(got, "F") != 3 {
		t.Errorf("rows %s", got)
	}
	if s := v.diffStatus(); s != "3 files · 3 changes · +3 −2" {
		t.Errorf("status %q", s)
	}
	if !hs.tt.HasText("Changes") {
		t.Errorf("no name: %q", hs.tt.Texts())
	}
	hs.shot("git-diff-all")
	v.cursor = -1
	v.scroll.Y = 0
	v.stepFile(1)
	if v.cursor != v.heads[1] {
		t.Errorf("next file: cursor %d", v.cursor)
	}
	// A click on a header folds its file.
	pane, ok := hs.tt.Find("Diff of the Changes")
	if !ok {
		t.Fatal("no diff pane")
	}
	click := func(i int) {
		hs.tt.ClickAt(pane.X+60, pane.Y+editorPadY+(float32(v.tops[i])+0.5)*a.font.lineH-v.scroll.Y)
		hs.settle()
	}
	v.scroll.Y = 0
	hs.settle()
	head := v.heads[1]
	click(head)
	if f := v.files[v.rows[head].file]; !f.folded {
		t.Errorf("click on a header did not fold it")
	}
	// The header chosen has buttons: staging takes the file out of its
	// section, and into the staged one.
	v.setCursor(head)
	hs.settle()
	if err := hs.tt.Click("Stage Changes"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if diffFileOf(v, hs.path("main.go"), diffStaged) == nil || diffFileOf(v, hs.path("main.go"), diffIndex) != nil {
		t.Errorf("after staging: %v", names)
	}
}

func TestCommitButtonAndUndo(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	if !hs.tt.HasText("No Changes") {
		t.Errorf("a clean work tree does not say so: %q", hs.tt.Texts())
	}
	// Nothing staged: the button commits all, once asked.
	os.WriteFile(hs.path("main.go"), []byte("package main\n"), 0o644)
	hs.settleGit()
	if !hs.tt.HasText("Commit All") {
		t.Fatalf("no Commit All: %q", hs.tt.Texts())
	}
	a.git.message = "trim main"
	hs.settle()
	if err := hs.tt.Click("Commit All"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if log := git("log", "--format=%s", "-1"); strings.TrimSpace(log) != "trim main" {
		t.Fatalf("log %q", log)
	}
	if !hs.tt.HasText("Committed “trim main” to main") {
		t.Errorf("no word of the commit: %q", hs.tt.Texts())
	}
	// Undoing it, from the toast, keeps its changes staged, and its
	// message to commit again.
	if err := hs.tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if log := git("log", "--format=%s", "-1"); strings.TrimSpace(log) != "first" {
		t.Errorf("after undoing, log %q", log)
	}
	if a.git.message != "trim main" {
		t.Errorf("message %q", a.git.message)
	}
	if g := a.groupEntries(); len(g[groupStaged]) != 1 {
		t.Errorf("groups %v", g)
	}
	if !hs.tt.HasText("Commit") || hs.tt.HasText("Commit All") {
		t.Errorf("with changes staged, the button commits them: %q", hs.tt.Texts())
	}
	// An Undo offered for a commit does not undo one made since.
	a.git.message = "again"
	a.commit(false)
	hs.settleGit()
	first := strings.TrimSpace(git("rev-parse", "HEAD"))
	os.WriteFile(hs.path("go.mod"), []byte("module y\n"), 0o644)
	git("commit", "-qam", "later")
	hs.settleGit()
	later := strings.TrimSpace(git("rev-parse", "HEAD"))
	a.undoCommitOf(first)
	hs.settleGit()
	if head := strings.TrimSpace(git("rev-parse", "HEAD")); head != later {
		t.Errorf("undid the later commit")
	}
	if len(a.errors) == 0 && !hs.tt.HasText("a commit has been made since, so this one is no longer the last to undo") {
		t.Errorf("no word of why not")
	}
}

func TestSubjectLength(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	a.git.message = strings.Repeat("a", 50)
	hs.settle()
	if hs.tt.HasText("50/72") {
		t.Errorf("a short summary is counted")
	}
	a.git.message = strings.Repeat("a", 80) + "\n\nbody"
	hs.settle()
	if !hs.tt.HasText("80/72") || !hs.tt.HasText("Summary is long") {
		t.Errorf("a long summary is not counted: %q", hs.tt.Texts())
	}
}

func TestHistoryAndCommitTabs(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	git("mv", "README.md", "README.markdown")
	git("commit", "-qam", "Trim main\n\nAnd rename the README.")
	a.run("view.scm")
	hs.settleGit()
	if len(a.git.history) != 2 {
		t.Fatalf("history %+v", a.git.history)
	}
	for _, s := range []string{"COMMITS", "Trim main", "first"} {
		if !hs.tt.HasText(s) {
			t.Errorf("no %q: %q", s, hs.tt.Texts())
		}
	}
	// A click on a commit shows its changes in a tab of its own.
	if err := hs.tt.Click("Trim main"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	hs.settle()
	tab := a.activeTab()
	if tab == nil || tab.diff == nil || tab.diff.commit == nil {
		t.Fatalf("tab %+v", tab)
	}
	v := tab.diff
	if v.name() != "Trim main" || len(v.files) != 2 {
		t.Fatalf("commit tab %q with %d files", v.name(), len(v.files))
	}
	main := diffFileOf(v, hs.path("main.go"), diffCommit)
	if main == nil || !main.loaded || main.letter != 'M' || len(main.hunks) == 0 {
		t.Fatalf("main.go %+v", main)
	}
	if f := diffFileOf(v, hs.path("README.markdown"), diffCommit); f == nil || f.letter != 'R' || f.entry.Orig != hs.path("README.md") {
		t.Errorf("rename %+v", f)
	}
	if got := kinds(v); !strings.HasPrefix(got, "CF") {
		t.Errorf("rows %s", got)
	}
	if !hs.tt.HasText("And rename the README.") || v.rows[0].h < 4 {
		t.Errorf("the message's body, its card %d lines: %q", v.rows[0].h, hs.tt.Texts())
	}
	// It does not take the place of the changes of the work tree.
	if col, _ := a.changesTab(); col != nil {
		t.Errorf("a commit's tab is taken for the changes")
	}
	os.WriteFile(hs.path("go.mod"), []byte("module x\n"), 0o644)
	hs.settleGit()
	a.openChangesTab(false)
	hs.settleGit()
	if col, i := a.commitTab(a.git.history[0].Hash); col == nil || col.tabs[i] != tab {
		t.Errorf("the commit's tab went")
	}
	// It is kept, and comes back.
	a.rememberTabs()
	kept := false
	for _, ts := range a.window.Columns[0].Tabs {
		kept = kept || ts.Diff == "commit:"+a.git.history[0].Hash
	}
	if !kept {
		t.Errorf("tabs kept %+v", a.window.Columns)
	}
	col, i := a.commitTab(a.git.history[0].Hash)
	a.activate(col, i, true)
	a.closeActive()
	hs.settle()
	if col, _ := a.commitTab(a.git.history[0].Hash); col != nil {
		t.Fatalf("not closed")
	}
	a.reopenClosed()
	hs.settleGit()
	hs.settle()
	if tab := a.activeTab(); tab == nil || tab.diff == nil || tab.diff.commit == nil || len(tab.diff.files) != 2 || tab.diff.name() != "Trim main" {
		t.Errorf("reopened %+v", tab)
	}
}

func TestDiffStickyHeader(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	os.WriteFile(hs.path("long.txt"), []byte(b.String()), 0o644)
	git("add", ".")
	git("commit", "-qm", "long")
	a.run("view.wordWrap") // off: the rows are a line each
	os.WriteFile(hs.path("long.txt"), []byte(strings.ReplaceAll(b.String(), "0\n", "zero\n")), 0o644)
	os.WriteFile(hs.path("main.go"), []byte("package main\n"), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("long.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	file := diffFileOf(v, hs.path("long.txt"), diffIndex)
	if file == nil || !file.loaded {
		t.Fatalf("long.txt not read")
	}
	head := -1
	for _, h := range v.heads {
		if v.files[v.rows[h].file] == file {
			head = h
		}
	}
	lineH := a.font.lineH
	// Scrolled into its lines, its header stays at the top.
	v.scroll.Y = editorPadY + float32(v.tops[head]+10)*lineH
	hs.settle()
	if h, y := v.stickyHeader(lineH); h != head || y != v.scroll.Y {
		t.Fatalf("sticky %d at %v, want %d at %v", h, y, head, v.scroll.Y)
	}
	hs.shot("diff-sticky")
	// At its own header, none shows.
	v.scroll.Y = editorPadY + float32(v.tops[head])*lineH
	hs.settle()
	if h, _ := v.stickyHeader(lineH); h != -1 {
		t.Errorf("sticky %d over the header itself", h)
	}
	// A click on it folds the file, and shows its header.
	v.scroll.Y = editorPadY + float32(v.tops[head]+10)*lineH
	hs.settle()
	x := a.settings.SidebarWidth + 200
	y := titleBarH + tabStripH + crumbsH + lineH/2
	hs.tt.ClickAt(x, y)
	hs.settle()
	if !file.folded {
		t.Fatalf("not folded by its sticky header")
	}
	if top := editorPadY + float32(v.tops[head])*lineH; v.scroll.Y > top {
		t.Errorf("the header is above the view: %v > %v", v.scroll.Y, top)
	}
}

func TestDiffHunkActions(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig := b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed := strings.Replace(strings.Replace(orig, "line 2\n", "line two\n", 1), "line 25\n", "line twenty-five\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	heads := func(base diffBase) []int {
		var out []int
		for i, r := range v.rows {
			if r.kind == rowHunk && v.files[r.file].path == hs.path("t.txt") && v.files[r.file].base == base {
				out = append(out, i)
			}
		}
		return out
	}
	hh := heads(diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	// Staging the second run stages its change alone.
	f, hks := v.hunksAt(hh[1])
	if len(hks) != 1 || hks[0].B != 25 {
		t.Fatalf("hunks %v", hks)
	}
	a.applyHunks(f, hks, hunkStage)
	hs.settleGit()
	if staged := git("diff", "--cached"); !strings.Contains(staged, "+line twenty-five") || strings.Contains(staged, "line two") {
		t.Fatalf("staged %q", staged)
	}
	// Unstaging it from the staged changes.
	hs.settle()
	sh := heads(diffStaged)
	if len(sh) != 1 {
		t.Fatalf("staged runs %v: %s", sh, kinds(v))
	}
	f, hks = v.hunksAt(sh[0])
	a.applyHunks(f, hks, hunkUnstage)
	hs.settleGit()
	if staged := git("diff", "--cached"); staged != "" {
		t.Fatalf("still staged %q", staged)
	}
	// The s key stages the change of the chosen line.
	hs.settle()
	for i, r := range v.rows {
		if r.changed() && r.new == 2 && v.files[r.file].base == diffIndex {
			v.setCursor(i)
		}
	}
	v.pane.Focus()
	hs.settle()
	hs.tt.Key(0, ui.KeyS)
	hs.settleGit()
	if staged := git("diff", "--cached"); !strings.Contains(staged, "+line two") || strings.Contains(staged, "twenty") {
		t.Fatalf("s staged %q", staged)
	}
	git("reset", "-q")
	hs.settleGit()
	// Discarding the first run on disk asks, then puts its line back.
	hs.settle()
	hh = heads(diffIndex)
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(f, hks, hunkDiscard)
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	data, _ := os.ReadFile(hs.path("t.txt"))
	if want := strings.Replace(orig, "line 25\n", "line twenty-five\n", 1); string(data) != want {
		t.Fatalf("after discarding:\n%s", data)
	}
	// In an open file, discarding is a step of undo.
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	col, i := a.changesTab()
	a.activate(col, i, true)
	time.Sleep(200 * time.Millisecond)
	hs.settleGit()
	hh = heads(diffIndex)
	if len(hh) != 1 {
		t.Fatalf("runs %v", hh)
	}
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(f, hks, hunkDiscard)
	hs.settle()
	d, _ := a.editorOf(a.diffDoc(f))
	if d == nil || d.buf.String() != orig {
		t.Fatalf("editor after discarding: %v", d != nil)
	}
	d.undo()
	if !strings.Contains(d.buf.String(), "twenty-five") {
		t.Errorf("undo: %q", d.buf.String())
	}
}

func TestBranchPicker(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	git("branch", "topic")
	a.run("view.scm")
	hs.settleGit()
	if err := hs.tt.Click("main"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	b := &a.branches
	if !b.open || b.loading || len(b.branches) != 2 {
		t.Fatalf("picker %+v", b)
	}
	// Typing finds a branch, and Enter switches to it.
	hs.tt.Type("top")
	hs.settle()
	if cs := a.branchChoices(); len(cs) != 2 || b.branches[b.results[cs[0].at].Index].Name != "topic" || cs[1].create != "top" {
		t.Fatalf("choices %+v", cs)
	}
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if b.open || a.branch() != "topic" {
		t.Fatalf("open %v on %q", b.open, a.branch())
	}
	// A name of none creates it.
	a.openBranchPicker()
	hs.settle()
	hs.tt.Type("new-work")
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if a.branch() != "new-work" {
		t.Fatalf("on %q", a.branch())
	}
	if out := git("branch", "--list", "new-work"); !strings.Contains(out, "new-work") {
		t.Errorf("branches %q", out)
	}
	// The name of a remote's branch switches to it, rather than making one.
	remote := filepath.Join(t.TempDir(), "remote.git")
	git("init", "-q", "--bare", remote)
	git("remote", "add", "origin", remote)
	git("push", "-q", "origin", "main:feature")
	git("fetch", "-q", "origin")
	a.openBranchPicker()
	hs.settle()
	hs.tt.Type("feature")
	hs.settle()
	for _, ch := range a.branchChoices() {
		if ch.create != "" {
			t.Errorf("offers to create %q", ch.create)
		}
	}
	a.closeBranchPicker()
	// Escape closes it.
	a.openBranchPicker()
	hs.settle()
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if b.open {
		t.Errorf("still open")
	}
}

// The commits show their branches and tags, every branch's once asked,
// and a tag made at one shows on it.
func TestHistoryBranchesAndTags(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	git("tag", "v1")
	git("switch", "-q", "-c", "side")
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	git("commit", "-qam", "On the side")
	git("switch", "-q", "main")
	a.run("view.scm")
	hs.settleGit()
	// Past the branch, the tag folds into a count at the sidebar's width.
	if len(a.git.history) != 1 || len(a.git.history[0].Refs) != 2 || !hs.tt.HasText("+1") || hs.tt.HasText("On the side") {
		t.Fatalf("history of main %+v: %q", a.git.history, hs.tt.Texts())
	}
	a.toggleAllBranches()
	hs.settleGit()
	if len(a.git.history) != 2 || !hs.tt.HasText("On the side") || !hs.tt.HasText("side") {
		t.Fatalf("history of all %+v: %q", a.git.history, hs.tt.Texts())
	}
	if g := a.historyGraph(); len(g) != 2 || g[0].lane != 0 || g[1].lane != 0 || len(g[0].below) != 1 {
		t.Errorf("graph %+v", g)
	}
	side := a.git.history[0]
	a.askRefAt(side, true)
	if msg := a.prompt.done("v1"); msg == "" {
		t.Error("a tag of a name taken was made")
	}
	a.prompt.done("v2")
	a.prompt = nil
	hs.settleGit()
	if refs := a.git.history[0].Refs; !slices.Contains(refs, gitpkg.Ref{Name: "v2", Kind: gitpkg.RefTag}) {
		t.Errorf("refs %+v", refs)
	}
	a.askRefAt(side, false)
	a.prompt.done("third")
	a.prompt = nil
	hs.settleGit()
	if refs := a.git.history[0].Refs; !slices.ContainsFunc(refs, func(r gitpkg.Ref) bool { return r.Name == "third" }) {
		t.Errorf("refs %+v", refs)
	}
	// A commit of two branches is checked out detached, once asked.
	a.checkOutCommit(a.git.history[0])
	if a.confirm == nil {
		t.Fatal("not asked")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if st := a.git.status; st.Branch != "" || st.Head != side.Hash {
		t.Errorf("status %+v", st)
	}
	// One of a single branch switches to it.
	a.checkOutCommit(a.git.history[1])
	hs.settleGit()
	if a.git.status.Branch != "main" {
		t.Errorf("branch %q", a.git.status.Branch)
	}
}

// The view's ⋯ menu takes an item chosen with the pointer off the sidebar,
// where it leaves the menu: Refresh, shown only while the pointer is on
// it, going does not lose the choice. The toggle of every branch shows
// while it is on.
func TestScmMenuChosenAway(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	tt := hs.tt
	a.run("view.scm")
	hs.settleGit()
	choose := func(item string) {
		t.Helper()
		r, ok := tt.Find("More Actions")
		if !ok {
			t.Fatalf("no menu button: %q", tt.Texts())
		}
		x, y := r.X+r.W/2, r.Y+r.H/2
		tt.Move(x, y)
		hs.settle()
		tt.Press(x, y)
		hs.settle()
		tt.Release(x, y)
		hs.settle()
		tt.Move(600, 300)
		hs.settle()
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("%v: %q", err, tt.Menu())
		}
		hs.settle()
	}
	for _, want := range []bool{true, false} {
		choose("View as Tree")
		if a.settings.ScmTree != want {
			t.Errorf("tree %v, want %v", a.settings.ScmTree, want)
		}
		choose("Show All Branches")
		hs.settleGit()
		if a.settings.ScmAllBranches != want {
			t.Errorf("all branches %v, want %v", a.settings.ScmAllBranches, want)
		}
		// Off the header, its toggle shows only while on.
		if got := hs.tt.HasText("Show Current Branch Only"); got != want {
			t.Errorf("toggle shown %v with all branches %v: %q", got, want, tt.Texts())
		}
	}
}

// A long message wraps in the card of its commit, which grows as its view
// narrows, folds a long body until asked, and copies its hash on a click.
func TestCommitCardWraps(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a, tt := hs.a, hs.tt
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	subject := strings.Repeat("Close a tab after the frame so its column goes ", 2)
	body := strings.Repeat("- a line\n", 30)
	git("commit", "-qam", subject+"\n\n"+body)
	a.run("view.scm")
	hs.settleGit()
	a.showCommit(a.git.history[0], true)
	hs.settleGit()
	hs.settle()
	v := a.activeTab().diff
	if v == nil || v.rows[0].kind != rowCommit {
		t.Fatalf("no card")
	}
	wide := v.rows[0].h
	if !tt.HasText("Show More") {
		t.Errorf("a long body does not fold: %q", tt.Texts())
	}
	// Narrower, the subject and the body wrap to more lines.
	tt.SetSize(800, 760)
	hs.settle()
	if v.rows[0].h <= wide {
		t.Errorf("the card is %d lines narrow, %d wide", v.rows[0].h, wide)
	}
	narrow := v.rows[0].h
	if err := tt.Click("Show More"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !v.commit.unfolded || v.rows[0].h <= narrow || !tt.HasText("Show Less") {
		t.Errorf("Show More: unfolded %v, %d lines after %d", v.commit.unfolded, v.rows[0].h, narrow)
	}
	if err := tt.Click("Show Less"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.commit.unfolded || v.rows[0].h != narrow {
		t.Errorf("Show Less: unfolded %v, %d lines", v.commit.unfolded, v.rows[0].h)
	}
	hash := a.git.history[0].Hash
	if err := tt.Click(hash[:7]); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := tt.Clipboard(); got != hash {
		t.Errorf("clipboard %q, not the hash %q", got, hash)
	}
}
