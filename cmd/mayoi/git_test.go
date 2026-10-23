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
	// The title is GIT where the font leaves no room for the whole.
	if !hs.tt.HasText("SOURCE CONTROL") && !hs.tt.HasText("GIT") {
		t.Errorf("no title: %q", hs.tt.Texts())
	}
	for _, s := range []string{"CHANGES", "UNTRACKED", "main.go", "notes.txt", "go.mod"} {
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
	x := a.window.SidebarWidth + d.gutterW - 5
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
		step(hs.tt)
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
		step(hs.tt)
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
	if st := a.git.status; st.Behind != 1 || a.repository().gitSyncLabel() != "1↓ 0↑" {
		t.Fatalf("behind %d, label %q", st.Behind, a.repository().gitSyncLabel())
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
	step(hs.tt)
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

// changesTabAndShow shows the tab of the changes again and returns its
// diff: discarding in a file that is not open shows that file's tab.
func (a *app) changesTabAndShow() *diffView {
	col, i := a.changesTab()
	if col == nil {
		return nil
	}
	a.activate(col, i, true)
	return col.tabs[i].diff
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
		s.WriteByte("LGHSFNsCc"[r.kind])
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
	v.stepHunk(1, crossAsk)
	v.stepHunk(1, crossAsk)
	v.stepHunk(1, crossAsk)
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
	if _, ok := hs.tt.Find("Layout"); !ok {
		t.Errorf("no diff layout menu")
	}
	hs.shot("git-diff-all")
	v.cursor = -1
	v.scroll.Y = 0
	v.stepFile(1)
	if v.cursor != v.heads[1] {
		t.Errorf("next file: cursor %d", v.cursor)
	}
	// The toolbar and Git menu use the same navigation command.
	v.cursor = -1
	v.scroll.Y = 0
	a.run("git.nextFile")
	if v.cursor != v.heads[1] {
		t.Errorf("git.nextFile: cursor %d", v.cursor)
	}
	a.run("git.prevFile")
	if v.cursor != v.heads[0] {
		t.Errorf("git.prevFile: cursor %d", v.cursor)
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
	x := a.window.SidebarWidth + 200
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
	a.applyHunks(v, f, hks, hunkStage)
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
	a.applyHunks(v, f, hks, hunkUnstage)
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
	// Discarding the first run asks, opens the file, and puts its line
	// back in the document, where undo takes it back.
	a.settings.AutoSave = false
	hs.settle()
	hh = heads(diffIndex)
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settle()
	dd := a.docs[a.docIndex(hs.path("t.txt"))]
	if dd == nil {
		t.Fatal("the file was not opened to discard in")
	}
	if want := strings.Replace(orig, "line 25\n", "line twenty-five\n", 1); dd.buf.String() != want {
		t.Fatalf("in the document:\n%s", dd.buf.String())
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); !strings.Contains(string(data), "line two\n") {
		t.Fatalf("the file was written before the document was saved:\n%s", data)
	}
	a.saveDoc(dd, nil)
	if data, _ := os.ReadFile(hs.path("t.txt")); strings.Contains(string(data), "line two\n") {
		t.Fatalf("after saving:\n%s", data)
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
	a.applyHunks(v, f, hks, hunkDiscard)
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

// twoHunkFile makes t.txt of thirty lines, commits it, and changes its
// lines 2 and 25 in the work tree, returning the text as it was and as
// it is.
func twoHunkFile(hs *harness, git func(args ...string) string) (orig, changed string) {
	hs.t.Helper()
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig = b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed = strings.Replace(strings.Replace(orig, "line 2\n", "line two\n", 1), "line 25\n", "line twenty-five\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	return orig, changed
}

// hunkRuns returns the rows that head the runs of changes of a file of
// the tab of the changes, against base.
func hunkRuns(v *diffView, path string, base diffBase) []int {
	var out []int
	for i, r := range v.rows {
		if r.kind == rowHunk && v.files[r.file].path == path && v.files[r.file].base == base {
			out = append(out, i)
		}
	}
	return out
}

// settleDiff pumps frames until every file of a diff has read what it
// shows again.
func (hs *harness) settleDiff(v *diffView) {
	hs.t.Helper()
	for range 200 {
		hs.settle()
		ok := len(v.files) > 0
		for _, f := range v.files {
			if !f.loaded || f.running || f.waiting || f.gitStale {
				ok = false
			}
		}
		if ok {
			return
		}
	}
	hs.t.Fatal("the diff never settled")
}

// TestDiscardOfUnopenedFileIsUndoable: the changes of a file that is not
// open go back as the index has them in the document, as one step of
// undo, and the file is written as the document is saved.
func TestDiscardOfUnopenedFileIsUndoable(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks := v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settle()
	want := strings.Replace(orig, "line 25\n", "line twenty-five\n", 1)
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the file was not opened to discard in")
	}
	d := a.docs[i]
	if d.buf.String() != want || strings.Contains(d.buf.String(), "line two\n") {
		t.Fatalf("in the document: %q", d.buf.String())
	}
	if !d.dirty() {
		t.Error("the document does not show as changed")
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != changed {
		t.Errorf("the file was written before the document was saved: %q", data)
	}
	a.saveDoc(d, nil)
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != want {
		t.Errorf("after saving: %q", data)
	}
	ed := d.main()
	ed.undo()
	if d.buf.String() != changed {
		t.Errorf("undo: %q", d.buf.String())
	}
	ed.redo()
	if d.buf.String() != want {
		t.Errorf("redo: %q", d.buf.String())
	}
}

// TestDiscardOfEveryHunkIsOneUndoStep: the changes of a whole run go
// back as one step of undo, and what is staged stays staged, through the
// discard, its undo and its redo.
func TestDiscardOfEveryHunkIsOneUndoStep(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks := v.hunksAt(hh[0])
	_, more := v.hunksAt(hh[1])
	hks = append(hks, more...)
	if len(hks) != 2 {
		t.Fatalf("hunks %v", hks)
	}
	a.applyHunks(v, f, hks, hunkDiscard)
	a.confirm.run()
	a.confirm = nil
	hs.settle()
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the file was not opened to discard in")
	}
	d := a.docs[i]
	if d.buf.String() != orig {
		t.Fatalf("in the document: %q", d.buf.String())
	}
	ed := d.main()
	if !ed.undo() || d.buf.String() != changed {
		t.Fatalf("one undo took: %q", d.buf.String())
	}
	// The diff catches up with the text as undo made it, and one of the
	// changes is staged, the other is not.
	hs.settleDiff(a.changesTabAndShow())
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks = v.hunksAt(hh[1])
	a.applyHunks(v, f, hks, hunkStage)
	hs.settleGit()
	staged := func() string { return strings.TrimSpace(git("diff", "--cached")) }
	if want := staged(); !strings.Contains(want, "line twenty-five") || strings.Contains(want, "line two\n") {
		t.Fatalf("staged %q", want)
	}
	// Discarding what is left, through the document, leaves what is
	// staged alone, as do its undo and redo.
	hs.settleDiff(v)
	hh = hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 1 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	hs.settle()
	if want := strings.Replace(changed, "line two\n", "line 2\n", 1); d.buf.String() != want {
		t.Fatalf("in the document: %q", d.buf.String())
	}
	if st := staged(); !strings.Contains(st, "line twenty-five") {
		t.Errorf("staged after discarding: %q", st)
	}
	if !ed.undo() || d.buf.String() != changed {
		t.Fatalf("undo: %q", d.buf.String())
	}
	if st := staged(); !strings.Contains(st, "line twenty-five") {
		t.Errorf("staged after undo: %q", st)
	}
	if !ed.redo() || d.buf.String() != strings.Replace(changed, "line two\n", "line 2\n", 1) {
		t.Fatalf("redo: %q", d.buf.String())
	}
	if st := staged(); !strings.Contains(st, "line twenty-five") {
		t.Errorf("staged after redo: %q", st)
	}
}

// TestDiscardRefusesStaleContent: the changes of a diff that is behind
// the file, on disk or in the document, are not put over what is there
// now: the diff is read again, and the discard asked for once more.
func TestDiscardRefusesStaleContent(t *testing.T) {
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
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	// The file changes on disk after the diff was drawn.
	fresh := strings.Replace(changed, "line twenty-five\n", "line 25th\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(fresh), 0o644)
	f, hks := v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	a.confirm.run()
	a.confirm = nil
	if len(a.errors) == 0 {
		t.Fatal("no word of the file having changed")
	}
	a.errors = nil
	hs.settle()
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the file was not opened")
	}
	d := a.docs[i]
	if d.buf.String() != fresh {
		t.Errorf("the document has %q, not what the file has", d.buf.String())
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != fresh {
		t.Errorf("the file was written over: %q", data)
	}
	// Read again, the change goes back, keeping what the file has now.
	a.changesTabAndShow()
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	if a.confirm != nil {
		t.Fatal("asked about a file that is open now")
	}
	hs.settle()
	if want := strings.Replace(fresh, "line two\n", "line 2\n", 1); d.buf.String() != want {
		t.Errorf("in the document: %q", d.buf.String())
	}

	// A document with changes of its own is told of first, that the diff
	// may catch up with them.
	long := "module example\n\ngo 1.27\n\n// one\n// two\n// three\n// four\n// five\n// six\n// seven\n// eight\n// nine\n// ten\n// eleven\n// twelve\n// thirteen\n// fourteen\n// fifteen\n"
	os.WriteFile(hs.path("notes.txt"), []byte(long), 0o644)
	git("add", "notes.txt")
	git("commit", "-qm", "notes")
	os.WriteFile(hs.path("notes.txt"), []byte(long+"// noted\n"), 0o644)
	a.openFile(hs.path("notes.txt"), true)
	hs.settleGit()
	a.changesTabAndShow()
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("notes.txt"), diffIndex)
	if len(hh) != 1 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	d = a.docs[a.docIndex(hs.path("notes.txt"))]
	ed := d.main()
	ed.edit(ed.sel, textEdit{0, 0, "// typed\n"})
	f, hks = v.hunksAt(hh[0])
	a.applyHunks(v, f, hks, hunkDiscard)
	if len(a.errors) == 0 {
		t.Fatal("no word of the diff catching up")
	}
	a.errors = nil
	if want := "// typed\n" + long + "// noted\n"; d.buf.String() != want {
		t.Errorf("the document has %q", d.buf.String())
	}
	if len(d.hist.undo) != 1 {
		t.Errorf("the refused discard left %d steps of undo", len(d.hist.undo))
	}
	// Caught up, the change goes back, and what was typed stays.
	a.changesTabAndShow()
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("notes.txt"), diffIndex)
	if len(hh) != 2 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	// The run that ends at the noted line, which the diff found again
	// after the text was typed in.
	for _, i := range hh {
		if ff, hks2 := v.hunksAt(i); len(hks2) > 0 && hks2[0].B+hks2[0].BLen == 21 {
			f, hks = ff, hks2
		}
	}
	a.applyHunks(v, f, hks, hunkDiscard)
	hs.settle()
	if want := "// typed\n" + long; d.buf.String() != want {
		t.Errorf("in the document: %q", d.buf.String())
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
	if cs := a.branchChoices(); len(cs) != 2 || cs[0].create != "top" || b.branches[b.results[cs[1].at].Index].Name != "topic" || b.sel != 1 {
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
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if a.branch() != "feature" || a.git.status.Upstream != "origin/feature" {
		t.Fatalf("remote checkout: %+v", a.git.status)
	}
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

// chooseButtonMenu opens the menu of a button and chooses an item of it.
func (hs *harness) chooseButtonMenu(button string, item string) error {
	r, ok := hs.tt.Find(button)
	if !ok {
		return fmt.Errorf("no button %q", button)
	}
	hs.tt.Press(r.X+r.W/2, r.Y+r.H/2)
	hs.tt.Release(r.X+r.W/2, r.Y+r.H/2)
	hs.settle()
	return hs.tt.ChooseMenuItem(item)
}

// diffSpansOf spells the runs of a line of a diff that its highlight
// shows as changed.
func diffSpansOf(s string, spans []linediff.Span) []string {
	var out []string
	for _, sp := range spans {
		out = append(out, s[sp.Start:sp.End])
	}
	return out
}

// The highlight of a diff shows the words that changed, the characters
// within them, or the lines changed and no more.
func TestDiffHighlightSpans(t *testing.T) {
	f := &diffFile{old: diffSide{lines: []string{"abcd"}}, new: diffSide{lines: []string{"abXd"}}}
	// The words changed.
	if got := diffSpansOf("abXd", diffSpans(f, true, 0, 0, highlightWord)); !slices.Equal(got, []string{"abXd"}) {
		t.Errorf("words of the new side %q", got)
	}
	if got := diffSpansOf("abcd", diffSpans(f, false, 0, 0, highlightWord)); !slices.Equal(got, []string{"abcd"}) {
		t.Errorf("words of the old side %q", got)
	}
	// The characters within them did.
	if got := diffSpansOf("abXd", diffSpans(f, true, 0, 0, highlightChar)); !slices.Equal(got, []string{"X"}) {
		t.Errorf("chars of the new side %q", got)
	}
	if got := diffSpansOf("abcd", diffSpans(f, false, 0, 0, highlightChar)); !slices.Equal(got, []string{"c"}) {
		t.Errorf("chars of the old side %q", got)
	}
	// The lines changed, and no more: nothing to paint over them.
	if got := diffSpans(f, true, 0, 0, highlightLine); got != nil {
		t.Errorf("lines %v", got)
	}
	// A line with no other side to compare with shows none.
	if got := diffSpans(f, true, 0, -1, highlightWord); got != nil {
		t.Errorf("no peer %v", got)
	}
	// A line a side does not have shows none.
	if got := diffSpans(f, true, 5, 0, highlightWord); got != nil {
		t.Errorf("past the lines %v", got)
	}
	// A line that moved compares with its peer, not the line at its own
	// number on the other side.
	g := &diffFile{
		old: diffSide{lines: []string{"x := 1", "mu sync.Mutex"}},
		new: diffSide{lines: []string{"x := 1", "// added", "mu sync.RWMutex"}},
	}
	if got := diffSpansOf("mu sync.RWMutex", diffSpans(g, true, 2, 1, highlightWord)); !slices.Equal(got, []string{"RWMutex"}) {
		t.Errorf("words of a moved new line %q", got)
	}
	if got := diffSpansOf("mu sync.Mutex", diffSpans(g, false, 1, 2, highlightWord)); !slices.Equal(got, []string{"Mutex"}) {
		t.Errorf("words of a moved old line %q", got)
	}
}

// The unchanged lines a diff shows around a change are as the settings
// keep: none, a few, or every one the file has.
func TestDiffContext(t *testing.T) {
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
	// One unchanged line on each side of the change.
	a.setDiffContext(1)
	hs.settle()
	if got := kinds(v); got != "SFGHLLLLG" {
		t.Errorf("one line: %s", got)
	}
	// None: the change and its header, between the gaps.
	a.setDiffContext(0)
	hs.settle()
	if got := kinds(v); got != "SFGHLLG" {
		t.Errorf("no lines: %s", got)
	}
	if g := v.rows[2]; g.kind != rowGap || g.n != 53 {
		t.Errorf("the gap above folds %d", g.n)
	}
	// Five around it.
	a.setDiffContext(5)
	hs.settle()
	if got := kinds(v); got != "SFGH"+strings.Repeat("L", 12)+"G" {
		t.Errorf("five lines: %s", got)
	}
	// More than the file has: every line, header and all.
	a.setDiffContext(100)
	hs.settle()
	if got := kinds(v); got != "SFH"+strings.Repeat("L", 105) {
		t.Errorf("all of them: %s", got)
	}
	// Back as it was.
	a.setDiffContext(3)
	hs.settle()
	if got := kinds(v); got != "SFGHLLLLLLLLG" {
		t.Errorf("three again: %s", got)
	}
	// The settings keep it for the next diffs.
	if a.settings.DiffContext != 3 {
		t.Errorf("settings keep %d", a.settings.DiffContext)
	}
}

// The sides of a side-by-side diff scroll together, or each on its own,
// and together again from where the right one was.
func TestDiffSyncScroll(t *testing.T) {
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
	os.WriteFile(long, []byte(strings.Replace(string(data), "println(5)", "println(\"five\")", 1)), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: long, any: true}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutSplit)
	hs.settle()
	if !v.split || !a.settings.DiffSyncScroll {
		t.Fatalf("split %v, synced %v", v.split, a.settings.DiffSyncScroll)
	}
	// Whole files, so there is enough to scroll.
	a.setDiffFull(v, true)
	hs.settle()
	lineH := a.font.lineH
	v.scroll.Y = 5 * lineH
	hs.settle()
	// Each side on its own, from where they showed together.
	a.setDiffSync(v, false)
	hs.settle()
	if v.sideY != 5*lineH {
		t.Fatalf("the right side starts at %v, not where they showed", v.sideY)
	}
	scrollY := v.scroll.Y
	pane, ok := hs.tt.Find("Diff of the Changes")
	if !ok {
		t.Fatal("no diff pane")
	}
	// A wheel over the right half scrolls it alone.
	hs.tt.Scroll(pane.X+pane.W*3/4, pane.Y+pane.H/2, 0, 4*lineH)
	hs.settle()
	if v.sideY == scrollY {
		t.Errorf("the right side does not scroll on its own")
	}
	if v.scroll.Y != scrollY {
		t.Errorf("the left side moved with it: %v", v.scroll.Y)
	}
	// A wheel over the left half scrolls what they share.
	hs.tt.Scroll(pane.X+pane.W/4, pane.Y+pane.H/2, 0, 4*lineH)
	hs.settle()
	if v.scroll.Y == scrollY {
		t.Errorf("the left side does not scroll")
	}
	// Together again, from the line the right side was at.
	rightTop := v.rowAt(v.sideY+lineH/2, lineH)
	a.setDiffSync(v, true)
	hs.settle()
	if top := v.rowAt(v.scroll.Y+lineH/2, lineH); top != rightTop {
		t.Errorf("back together at row %d, the right side was at %d", top, rightTop)
	}
	if !a.settings.DiffSyncScroll {
		t.Errorf("the settings do not keep it")
	}
}

// Whatever a diff highlights within a line, its block actions are for the
// hunk the line is in, never only for what the highlight shows.
func TestDiffHighlightKeepsBlockActions(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	p := hs.path("main.go")
	os.WriteFile(p, []byte("package main\n\nfunc main() {\n\tprintln(1)\n\tprintln(2)\n\tprintln(3)\n}\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "main")
	os.WriteFile(p, []byte("package main\n\nfunc main() {\n\tprintln(11)\n\tprintln(2)\n\tprintln(33)\n}\n"), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: p, any: true}, true)
	hs.settleGit()
	v := a.activeTab().diff
	f := diffFileOf(v, p, diffIndex)
	if f == nil || len(f.hunks) != 2 {
		t.Fatalf("hunks %v", f.hunks)
	}
	i := v.starts[0]
	for _, h := range []diffHighlight{highlightChar, highlightWord, highlightLine} {
		a.setDiffHighlight(h)
		hs.settle()
		// The line stands for its whole hunk, and the actions are of it.
		fl, hs2 := v.hunksAt(i)
		if fl != f || !slices.Equal(hs2, f.hunks[:1]) {
			t.Errorf("highlight %d: the changes of the line are %v", h, hs2)
		}
		if acts := hunkActions(fl); !slices.Equal(acts, []hunkAction{hunkDiscard, hunkStage, hunkAppend}) {
			t.Errorf("highlight %d: actions %v", h, acts)
		}
		// The header of the run stands for all of the hunks it heads.
		hdr := v.hunkHead(i)
		if _, hs2 := v.hunksAt(hdr); !slices.Equal(hs2, f.hunks) {
			t.Errorf("highlight %d: the changes of the header are %v", h, hs2)
		}
	}
}

// Folding a gap or a file, wrapping and a wider window keep the line at
// the top of a diff's view the same line of the file, and a layout chosen
// stays chosen whatever the width.
func TestDiffKeepsItsPlace(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	long := hs.path("long.go")
	var b strings.Builder
	b.WriteString("package main\n\nfunc long() {\n")
	for i := range 100 {
		if i == 50 {
			// A line that wraps, once the diff wraps its lines.
			b.WriteString("\tprintln(\"" + strings.Repeat("fifty ", 60) + "\")\n")
			continue
		}
		fmt.Fprintf(&b, "\tprintln(%d)\n", i)
	}
	b.WriteString("}\n")
	os.WriteFile(long, []byte(b.String()), 0o644)
	git("add", ".")
	git("commit", "-qm", "long")
	data, _ := os.ReadFile(long)
	for i := range 100 {
		if i != 50 && i%10 == 0 {
			data = []byte(strings.Replace(string(data), fmt.Sprintf("println(%d)", i), fmt.Sprintf("println(%d thousand)", i), 1))
		}
	}
	os.WriteFile(long, data, 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: long, any: true}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutSplit)
	hs.settle()
	lineH := a.font.lineH
	at := func() diffAnchor { return v.anchorOf(v.focusRowAt(v.topRow(lineH))) }
	// At the line that wraps.
	a.setDiffFull(v, true)
	hs.settle()
	wrapped := -1
	for i, r := range v.rows {
		if r.kind == rowLine && r.new == 53 {
			wrapped = i
		}
	}
	if wrapped < 0 {
		t.Fatal("no long line")
	}
	v.scroll.Y = float32(v.tops[wrapped]) * lineH
	hs.settle()
	was := at()
	// Wrapped, the line is more rows tall, and still the line at the top.
	a.toggleDiffWrap(v)
	hs.settle()
	if got := at(); got != was {
		t.Errorf("wrapped: %+v → %+v", was, got)
	}
	if r := v.rows[v.focusRowAt(v.topRow(lineH))]; r.kind != rowLine || r.h < 2 {
		t.Errorf("the long line is %d rows tall", r.h)
	}
	// Narrower, the rows lay out again; the line stays at the top, and
	// the layout chosen stays chosen.
	hs.tt.SetSize(800, 760)
	hs.settle()
	if got := at(); got != was {
		t.Errorf("narrower: %+v → %+v", was, got)
	}
	if !v.split {
		t.Errorf("the layout chosen gave way to the width")
	}
	hs.tt.SetSize(1200, 760)
	a.toggleDiffWrap(v)
	hs.settle()
	// A gap above the view folds and opens, and the line stays put.
	a.setDiffFull(v, false)
	hs.settle()
	var gaps []int
	for i, r := range v.rows {
		if r.kind == rowGap {
			gaps = append(gaps, i)
		}
	}
	if len(gaps) < 6 {
		t.Fatalf("%d gaps", len(gaps))
	}
	v.scroll.Y = float32(v.tops[gaps[4]+6]) * lineH
	hs.settle()
	was = at()
	a.activateRow(v, gaps[4], false)
	hs.settle()
	if got := at(); got != was {
		t.Errorf("opened a gap: %+v → %+v", was, got)
	}
	a.toggleGap(v, gaps[4]+1)
	hs.settle()
	if got := at(); got != was {
		t.Errorf("folded a gap: %+v → %+v", was, got)
	}
	// The file folds under its header, and unfolds where it was.
	was = at()
	a.activateRow(v, 1, false)
	hs.settle()
	if got := kinds(v); got != "SF" {
		t.Fatalf("folded: %s", got)
	}
	a.activateRow(v, 1, false)
	hs.settle()
	if got := at(); got != was {
		t.Errorf("unfolded: %+v → %+v", was, got)
	}
}

// A diff's buttons offer what its lines highlight, how much shows around
// a change, and whether the sides scroll together.
func TestDiffButtons(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hi, mayoi\")\n}\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "main")
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello, mayoi\")\n}\n"), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("main.go"), any: true}, true)
	hs.settleGit()
	v := a.activeTab().diff
	if _, ok := hs.tt.Find("Highlight"); !ok {
		t.Fatal("no highlight button")
	}
	if _, ok := hs.tt.Find("Context"); !ok {
		t.Fatal("no context button")
	}
	// The highlight menu chooses the characters.
	if err := hs.chooseButtonMenu("Highlight", "Highlight Characters"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.diffHighlightOf() != highlightChar {
		t.Errorf("highlight %d, want the characters", a.diffHighlightOf())
	}
	if a.settings.DiffHighlight != "char" {
		t.Errorf("settings keep %q", a.settings.DiffHighlight)
	}
	// The context menu chooses no unchanged lines.
	if err := hs.chooseButtonMenu("Context", "No Lines"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.diffContextOf() != 0 {
		t.Errorf("context %d, want none", a.diffContextOf())
	}
	// The last unchanged line, on its own, does not fold.
	if got := kinds(v); got != "SFGHLLL" {
		t.Errorf("rows %s", got)
	}
	// The layout menu has the sides scrolling together, and lets them go.
	if err := hs.chooseButtonMenu("Layout", "Synchronized Scrolling"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.DiffSyncScroll {
		t.Errorf("the sides still scroll together")
	}
	a.setDiffSync(v, true)
}

// TestHunkArrowsFollowTheVersions: the arrows of a diff's blocks write
// only the sides that can be written, named as the versions compared say.
// The changes of the work tree go back as the index has them, which
// reverts them; the index goes on by staging, and the versions Git keeps
// are read only, so the changes staged and a commit's take no arrows —
// nor does a file one side of the diff lacks whole, whose changes go in
// or out with the file itself.
func TestHunkArrowsFollowTheVersions(t *testing.T) {
	file := func(base diffBase, oldOK, newOK bool) *diffFile {
		f := &diffFile{path: "t.txt", base: base, loaded: true}
		f.old.ok, f.new.ok = oldOK, newOK
		return f
	}
	ars := hunkArrows(file(diffIndex, true, true))
	if len(ars) != 1 || ars[0].dir != 1 || ars[0].label != "Revert" {
		t.Fatalf("arrows of the changes not staged: %v", ars)
	}
	if !strings.Contains(ars[0].tip, "Index") || !strings.Contains(ars[0].tip, "Working Tree") {
		t.Errorf("the arrow says %q, not both versions", ars[0].tip)
	}
	for _, base := range []diffBase{diffStaged, diffCommit} {
		if ars := hunkArrows(file(base, true, true)); len(ars) != 0 {
			t.Errorf("arrows of the changes of base %d: %v", base, ars)
		}
	}
	if ars := hunkArrows(file(diffIndex, false, true)); len(ars) != 0 {
		t.Errorf("arrows of a file the diff adds whole: %v", ars)
	}
	if ars := hunkArrows(file(diffIndex, true, false)); len(ars) != 0 {
		t.Errorf("arrows of a file the diff deletes whole: %v", ars)
	}
	if ars := hunkArrows(&diffFile{path: "t.txt", base: diffIndex}); len(ars) != 0 {
		t.Errorf("arrows of a file still being read: %v", ars)
	}
}

// threeBlockFile makes t.txt of thirty lines, commits it, and changes it
// in the work tree three ways at once: a line replaced, lines added, and
// a line removed, returning the text as it was and as it is.
func threeBlockFile(hs *harness, git func(args ...string) string) (orig, changed string) {
	hs.t.Helper()
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig = b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed = strings.Replace(orig, "line 2\n", "line two\n", 1)
	changed = strings.Replace(changed, "line 10\nline 11\n", "line 10\nline ten and a half\nline 11\n", 1)
	changed = strings.Replace(changed, "line 25\n", "", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	return orig, changed
}

// TestRevertArrowAppliesOneBlock: the arrow of a block puts the lines the
// index has in place of the block's own lines of the work tree, in the
// document, as one step of undo, and the blocks beside it stay as they
// are — lines replaced, added and removed alike.
func TestRevertArrowAppliesOneBlock(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := threeBlockFile(hs, git)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 3 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	blocks := func(k int) linediff.Hunk {
		_, hks := v.hunksAt(hh[k])
		if len(hks) != 1 {
			t.Fatalf("the run at %d is %d hunks", hh[k], len(hks))
		}
		return hks[0]
	}
	replaced, added, removed := blocks(0), blocks(1), blocks(2)
	if replaced.ALen != 1 || replaced.BLen != 1 || added.ALen != 0 || added.BLen != 1 || removed.ALen != 1 || removed.BLen != 0 {
		t.Fatalf("hunks replaced %v added %v removed %v", replaced, added, removed)
	}
	f := v.files[v.rows[hh[0]].file]
	ars := hunkArrows(f)
	if len(ars) != 1 {
		t.Fatalf("arrows %v", ars)
	}
	d := a.docs[a.docIndex(hs.path("t.txt"))]
	// applyArrow does what a click on the arrow of one block does: it
	// takes the hunk as the rows show it now, which the clicks of the
	// arrows do too.
	apply := func(pick func(h linediff.Hunk) bool, what string) {
		a.errors = nil
		runs := hunkRuns(v, hs.path("t.txt"), diffIndex)
		var h linediff.Hunk
		n := 0
		for _, r := range runs {
			_, hks := v.hunksAt(r)
			for _, k := range hks {
				if pick(k) {
					h, n = k, n+1
				}
			}
		}
		if n != 1 {
			t.Fatalf("%d blocks of the %s among %v", n, what, runs)
		}
		a.applyArrow(v, f, h, ars[0])
		hs.settle()
		if len(a.errors) > 0 {
			t.Fatalf("the arrow of the %s failed: %v", what, a.errors)
		}
	}
	// The arrow of the added block takes its line out, and the others stay.
	apply(func(h linediff.Hunk) bool { return h.ALen == 0 }, "added block")
	want := strings.Replace(strings.Replace(orig, "line 25\n", "", 1), "line 2\n", "line two\n", 1)
	if d.buf.String() != want {
		t.Fatalf("after the arrow of the added block:\n%s", d.buf.String())
	}
	ed := d.main()
	if !ed.undo() || d.buf.String() != changed {
		t.Fatalf("one undo did not take the block back:\n%s", d.buf.String())
	}
	if !ed.redo() || d.buf.String() != want {
		t.Fatalf("redo:\n%s", d.buf.String())
	}
	hs.settle()
	// The arrow of the replaced block, then of the removed one; the added
	// block stays applied from the redo.
	apply(func(h linediff.Hunk) bool { return h.A == 2 }, "replaced block")
	if want := strings.Replace(orig, "line 25\n", "", 1); d.buf.String() != want {
		t.Fatalf("after the arrow of the replaced block:\n%s", d.buf.String())
	}
	apply(func(h linediff.Hunk) bool { return h.BLen == 0 }, "removed block")
	if d.buf.String() != orig {
		t.Fatalf("after the arrow of the removed block:\n%s", d.buf.String())
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != changed {
		t.Errorf("the file was written before the document was saved")
	}
}

// TestRevertArrowOfUnopenedFileIsUndoable: the arrow of a block of a file
// that no editor has asks once, opens the file, and puts the block back
// in the document, as one step of undo, and the file is written as the
// document is saved.
func TestRevertArrowOfUnopenedFileIsUndoable(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := threeBlockFile(hs, git)
	_ = orig
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 3 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks := v.hunksAt(hh[1])
	if len(hks) != 1 || hks[0].ALen != 0 {
		t.Fatalf("hunks %v", hks)
	}
	a.errors = nil
	a.applyArrow(v, f, hks[0], hunkArrows(f)[0])
	if a.confirm == nil {
		t.Fatal("no question")
	}
	if !strings.Contains(a.confirm.title, "Revert") || a.confirm.ok != "Revert" {
		t.Errorf("the question says %q, ok %q", a.confirm.title, a.confirm.ok)
	}
	a.confirm.run()
	a.confirm = nil
	hs.settle()
	want := strings.Replace(strings.Replace(orig, "line 25\n", "", 1), "line 2\n", "line two\n", 1)
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the file was not opened to revert in")
	}
	d := a.docs[i]
	if d.buf.String() != want {
		t.Fatalf("in the document: %q", d.buf.String())
	}
	if !d.dirty() {
		t.Error("the document does not show as changed")
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != changed {
		t.Errorf("the file was written before the document was saved: %q", data)
	}
	a.saveDoc(d, nil)
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != want {
		t.Errorf("after saving: %q", data)
	}
	ed := d.main()
	if !ed.undo() || d.buf.String() != changed {
		t.Errorf("undo: %q", d.buf.String())
	}
}

// TestRevertArrowRefusesStaleContent: the arrow of a block of a diff that
// is behind the document, or behind the files, does not put its lines
// over what is there now: it says so, and the diff is read again.
func TestRevertArrowRefusesStaleContent(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := threeBlockFile(hs, git)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	f, hks := v.hunksAt(hh[1])
	if len(hks) != 1 {
		t.Fatalf("hunks %v", hks)
	}
	d := a.docs[a.docIndex(hs.path("t.txt"))]
	ed := d.main()
	ed.edit(ed.sel, textEdit{0, 0, "// typed\n"})
	a.applyArrow(v, f, hks[0], hunkArrows(f)[0])
	if len(a.errors) == 0 {
		t.Fatal("no word of the diff catching up")
	}
	a.errors = nil
	if !strings.Contains(d.buf.String(), "ten and a half") || !strings.Contains(d.buf.String(), "// typed\n") {
		t.Errorf("the document was changed by the refused arrow:\n%s", d.buf.String())
	}
	if len(d.hist.undo) != 1 {
		t.Errorf("the refused arrow left %d steps of undo", len(d.hist.undo))
	}
	// A diff whose files Git keeps have changed says so too.
	hs.settleDiff(a.changesTabAndShow())
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("t.txt"), diffIndex)
	f, hks = v.hunksAt(hh[1])
	f.gitStale = true
	a.applyArrow(v, f, hks[0], hunkArrows(f)[0])
	if len(a.errors) == 0 {
		t.Fatal("no word of the diff catching up with the files")
	}
	if !strings.Contains(d.buf.String(), "ten and a half") {
		t.Errorf("the document was changed by the refused arrow:\n%s", d.buf.String())
	}
	_, _ = orig, changed
}

// TestRevertArrowKeepsLineEndingsAndFinalNewline: the arrow of a block of
// a file with CRLF endings keeps them when the document is saved, and a
// file that ends without a newline ends without one after its last block
// goes back.
func TestRevertArrowKeepsLineEndingsAndFinalNewline(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	// CRLF, with the last line changed in the work tree.
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	orig := b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed := strings.Replace(orig, "line 29\r\n", "line twenty-nine\r\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	hh := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 1 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks := v.hunksAt(hh[0])
	if len(hks) != 1 || hks[0].A != 29 || hks[0].BLen != 1 {
		t.Fatalf("hunks %v", hks)
	}
	d := a.docs[a.docIndex(hs.path("t.txt"))]
	if !d.crlf {
		t.Fatal("the document does not keep the file's CRLF")
	}
	a.errors = nil
	a.applyArrow(v, f, hks[0], hunkArrows(f)[0])
	hs.settle()
	if len(a.errors) > 0 {
		t.Fatalf("the arrow failed: %v", a.errors)
	}
	if d.buf.String() != strings.ReplaceAll(orig, "\r\n", "\n") {
		t.Fatalf("in the document:\n%q", d.buf.String())
	}
	a.saveDoc(d, nil)
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != orig {
		t.Errorf("after saving: %q", data)
	}

	// No newline at the end, with the last line changed.
	orig = strings.ReplaceAll(orig, "\r\n", "\n")
	orig = strings.TrimSuffix(orig, "\n")
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "no newline")
	changed = strings.Replace(orig, "line 29", "line twenty-nine", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a.changesTabAndShow()
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	hh = hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(hh) != 1 {
		t.Fatalf("runs %v: %s", hh, kinds(v))
	}
	f, hks = v.hunksAt(hh[0])
	if len(hks) != 1 {
		t.Fatalf("hunks %v", hks)
	}
	a.errors = nil
	a.applyArrow(v, f, hks[0], hunkArrows(f)[0])
	hs.settle()
	if len(a.errors) > 0 {
		t.Fatalf("the arrow failed: %v", a.errors)
	}
	if d.buf.String() != orig {
		t.Fatalf("in the document:\n%q", d.buf.String())
	}
	hs.settleDiff(a.changesTabAndShow())
	if runs := hunkRuns(a.activeTab().diff, hs.path("t.txt"), diffIndex); len(runs) != 0 {
		t.Errorf("the diff still shows %d runs", len(runs))
	}
}

// TestRevertArrowsOfTheDiff: every block of the changes not staged has an
// arrow, which applies its block alone when clicked, in either layout;
// the view keeps its place as the diff is read again; and the undo of the
// diff, from its keyboard, takes the block back.
func TestRevertArrowsOfTheDiff(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := threeBlockFile(hs, git)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	d := a.docs[a.docIndex(hs.path("t.txt"))]
	// arrowAt returns where the arrow of the kth run of the changes not
	// staged shows.
	arrowAt := func(k int) (x, y float32) {
		runs := hunkRuns(v, hs.path("t.txt"), diffIndex)
		if k >= len(runs) {
			t.Fatalf("run %d of %v", k, runs)
		}
		fl, hks := v.hunksAt(runs[k])
		if len(hks) != 1 {
			t.Fatalf("hunks %v", hks)
		}
		ki := slices.IndexFunc(fl.hunks, func(x linediff.Hunk) bool { return x == hks[0] })
		fi := v.rows[runs[k]].file
		from := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.file == fi && r.hunk == ki && r.changed() })
		if from < 0 {
			t.Fatalf("no rows of the hunk %d", ki)
		}
		pane, ok := hs.tt.Find("Diff of the Changes")
		if !ok {
			t.Fatal("no diff pane")
		}
		l := a.diffGeometry(v, v.split, v.viewW)
		k = slices.Index(v.starts, from)
		if k < 0 {
			t.Fatalf("no change starts at row %d", from)
		}
		rc := a.hunkArrowRects(v, k, hunkArrows(fl), l)[0]
		return pane.X + rc.X + rc.W/2 - v.scroll.X, pane.Y + rc.Y + rc.H/2 - v.scroll.Y
	}
	apply := func(k int, what string) {
		a.errors = nil
		a.confirm = nil
		x, y := arrowAt(k)
		hs.tt.ClickAt(x, y)
		hs.settle()
		if len(a.errors) > 0 {
			t.Fatalf("the arrow of the %s failed: %v", what, a.errors)
		}
		if a.confirm != nil {
			t.Fatalf("the arrow of the %s asked", what)
		}
	}
	// The diff opens in the one column at this width, with its arrows.
	if v.split {
		t.Log("the diff is side by side at this width")
	}
	if _, ok := hs.tt.Find("Revert"); !ok {
		t.Fatal("no arrow of a block")
	}
	// The view stays near where it was as the diff is read again: with the
	// removed block in view, the arrow of the added block goes back, and
	// the removed one is still in view after the diff has been read again.
	runs0 := hunkRuns(v, hs.path("t.txt"), diffIndex)
	v.scroll.Y = editorPadY + float32(v.tops[runs0[2]])*a.font.lineH - 5*a.font.lineH
	hs.settle()
	apply(1, "added block")
	want0 := strings.Replace(strings.Replace(orig, "line 25\n", "", 1), "line 2\n", "line two\n", 1)
	if d.buf.String() != want0 {
		t.Fatalf("after the arrow of the added block:\n%s", d.buf.String())
	}
	runs1 := hunkRuns(v, hs.path("t.txt"), diffIndex)
	fl, hks := v.hunksAt(runs1[len(runs1)-1])
	ki := slices.IndexFunc(fl.hunks, func(x linediff.Hunk) bool { return x == hks[0] })
	fi := v.rows[runs1[len(runs1)-1]].file
	from, to := -1, -1
	for i, r := range v.rows {
		if r.file == fi && r.hunk == ki && r.changed() {
			if from < 0 {
				from = i
			}
			to = i + 1
		}
	}
	first, last := v.rowAt(v.scroll.Y, a.font.lineH), v.rowAt(v.scroll.Y+v.viewH, a.font.lineH)
	if from > last || to-1 < first {
		t.Errorf("the removed block went out of the view: rows %d..%d of %d..%d", from, to-1, first, last)
	}
	// Undo from the diff's keyboard takes it back, and redo makes it again.
	v.pane.Focus()
	hs.settle()
	hs.key(ui.Cmd, ui.KeyZ)
	if d.buf.String() != changed {
		t.Fatalf("undo from the diff:\n%s", d.buf.String())
	}
	hs.key(ui.Cmd|ui.Shift, ui.KeyZ)
	if d.buf.String() != want0 {
		t.Fatalf("redo from the diff:\n%s", d.buf.String())
	}
	// Side by side, the arrows do the same: with the added block still
	// applied, the arrow of the replaced block puts its line back.
	a.setDiffLayout(v, layoutSplit)
	hs.settleDiff(v)
	apply(0, "replaced block")
	if want := strings.Replace(orig, "line 25\n", "", 1); d.buf.String() != want {
		t.Fatalf("after the arrow of the replaced block, side by side:\n%s", d.buf.String())
	}
	v.pane.Focus()
	hs.settle()
	hs.key(ui.Cmd, ui.KeyZ)
	if d.buf.String() != want0 {
		t.Fatalf("undo from the side by side diff:\n%s", d.buf.String())
	}
	hs.settleDiff(v)
	// Two arrows in a row, as a quick pair of clicks: each block goes back
	// by itself, or the second waits for the diff to catch up with the
	// first, and nothing goes over what is not there.
	x, y := arrowAt(0)
	x1, y1 := arrowAt(1)
	a.errors = nil
	hs.tt.ClickAt(x, y)
	hs.settle()
	hs.tt.ClickAt(x1, y1)
	hs.settleDiff(a.changesTabAndShow())
	switch d.buf.String() {
	case orig:
	case strings.Replace(orig, "line 25\n", "", 1):
		// The second click waited for the diff to catch up; its block goes
		// back once it has.
		a.errors = nil
		apply(0, "removed block")
		if d.buf.String() != orig {
			t.Fatalf("after the arrow of the removed block:\n%s", d.buf.String())
		}
	default:
		t.Errorf("after two arrows in a row:\n%s", d.buf.String())
	}
}

// TestWholeFileDiffSaysArrowsDoNotApply: a file one side of the diff lacks
// whole takes no arrows, and the diff says so under its lines, with what
// the buttons of its header do instead.
func TestWholeFileDiffSaysArrowsDoNotApply(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("new.txt"), []byte("a new file\nwith lines\n"), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("new.txt"), any: true}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f := diffFileOf(v, hs.path("new.txt"), diffIndex)
	if f == nil {
		t.Fatal("the new file is not in the diff")
	}
	if len(hunkArrows(f)) != 0 {
		t.Error("arrows on a file the diff adds whole")
	}
	if notes(v, "arrow") == 0 {
		t.Errorf("no note of the whole file: rows %s", kinds(v))
	}
	// A file deleted from the work tree says so too.
	os.WriteFile(hs.path("new.txt"), []byte("a new file\nwith lines\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "new")
	os.Remove(hs.path("new.txt"))
	hs.settleGit()
	a.changesTabAndShow()
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	f = diffFileOf(v, hs.path("new.txt"), diffIndex)
	if f == nil {
		t.Fatal("the deleted file is not in the diff")
	}
	if len(hunkArrows(f)) != 0 {
		t.Error("arrows on a file the diff deletes whole")
	}
	if notes(v, "arrow") == 0 {
		t.Errorf("no note of the whole file: rows %s", kinds(v))
	}
}

// notes counts the note rows of a diff whose label has s in it.
func notes(v *diffView, s string) int {
	n := 0
	for _, r := range v.rows {
		if r.kind == rowNote && strings.Contains(r.label, s) {
			n++
		}
	}
	return n
}

// TestRevertArrowOfOneOfTwoNearbyBlocks: two blocks a couple of lines
// apart share the header of their run of changes, and the arrow of one
// still applies its block alone, as the rows take it.
func TestRevertArrowOfOneOfTwoNearbyBlocks(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig := b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed := strings.Replace(strings.Replace(orig, "line 2\n", "line two\n", 1), "line 5\n", "line five\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	runs := hunkRuns(v, hs.path("t.txt"), diffIndex)
	if len(runs) != 1 {
		t.Fatalf("runs %v: %s", runs, kinds(v))
	}
	f, hks := v.hunksAt(runs[0])
	if len(hks) != 2 {
		t.Fatalf("the run has %d hunks: %v", len(hks), hks)
	}
	ars := hunkArrows(f)
	if len(ars) != 1 {
		t.Fatalf("arrows %v", ars)
	}
	// The arrows take the hunk of each block's first row, as the rows of
	// the diff show them.
	hunkOf := func(line int) linediff.Hunk {
		for _, r := range v.rows {
			if r.changed() && r.new == line {
				return f.hunks[r.hunk]
			}
		}
		t.Fatalf("no changed row of line %d", line)
		return linediff.Hunk{}
	}
	d := a.docs[a.docIndex(hs.path("t.txt"))]
	a.errors = nil
	a.applyArrow(v, f, hunkOf(2), ars[0])
	hs.settle()
	if len(a.errors) > 0 {
		t.Fatalf("the arrow failed: %v", a.errors)
	}
	if want := strings.Replace(changed, "line two\n", "line 2\n", 1); d.buf.String() != want {
		t.Fatalf("after the arrow of the first block:\n%s", d.buf.String())
	}
	a.applyArrow(v, f, hunkOf(5), ars[0])
	hs.settle()
	if d.buf.String() != orig {
		t.Fatalf("after the arrow of the second block:\n%s", d.buf.String())
	}
}
