package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repo makes a work tree with a commit of a.txt and b.txt.
func repo(t *testing.T) (*Repo, func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	write(t, dir, "a.txt", "one\ntwo\nthree\n")
	write(t, dir, "b.txt", "bee\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Root != dir {
		t.Fatalf("root %q, want %q as spelled", r.Root, dir)
	}
	return r, git
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatus(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	write(t, r.Root, "new dir/c d.txt", "sea\n")
	git("mv", "b.txt", "bb.txt")
	st, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Head == "" {
		t.Errorf("branch %q head %q", st.Branch, st.Head)
	}
	got := map[string]string{}
	for _, e := range st.Entries {
		got[r.Rel(e.Path)] = string([]byte{e.X, e.Y})
		if e.X == 'R' && r.Rel(e.Orig) != "b.txt" {
			t.Errorf("renamed from %q", e.Orig)
		}
	}
	want := map[string]string{"a.txt": ".M", "bb.txt": "R.", "new dir/c d.txt": "??"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q (all %v)", k, got[k], v, got)
		}
	}
}

func TestSubmodule(t *testing.T) {
	r, git := repo(t)
	inner, _ := repo(t)
	git("-c", "protocol.file.allow=always", "submodule", "add", "-q", inner.Root, "sub")
	git("commit", "-q", "-m", "sub")
	sub := filepath.Join(r.Root, "sub")
	write(t, sub, "a.txt", "changed\n")
	st, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	var e Entry
	for _, x := range st.Entries {
		if x.Path == sub {
			e = x
		} else if x.Submodule() {
			t.Errorf("%s is no submodule: %q", x.Path, x.Sub)
		}
	}
	if e.Sub != "S.M." || !e.Submodule() || !e.Dirty() {
		t.Fatalf("submodule entry %+v", e)
	}
	head := inner.Head()
	for _, rev := range []string{"", "HEAD"} {
		if text, ok, err := r.Show(rev, sub); !ok || err != nil || text != "Subproject commit "+head+"\n" {
			t.Errorf("show %q: %q %v %v", rev, text, ok, err)
		}
	}
	if text, ok, err := r.WorkSubproject(sub, e.Dirty()); !ok || err != nil || text != "Subproject commit "+head+"-dirty\n" {
		t.Errorf("work tree: %q %v %v", text, ok, err)
	}
	os.RemoveAll(filepath.Join(sub, ".git"))
	if _, ok, err := r.WorkSubproject(sub, false); ok || err != nil {
		t.Errorf("not checked out: ok %v err %v", ok, err)
	}
}

func TestShowNotInIndex(t *testing.T) {
	r, _ := repo(t)
	write(t, r.Root, ".changeset/new.md", "hi\n")
	for _, rel := range []string{".changeset/new.md", "gone.txt"} {
		for _, rev := range []string{"", "HEAD"} {
			if text, ok, err := r.Show(rev, r.Abs(rel)); ok || err != nil {
				t.Errorf("show %q %s: %q %v %v", rev, rel, text, ok, err)
			}
		}
	}
	if err := r.Stage(r.Abs(".changeset/new.md")); err != nil {
		t.Fatal(err)
	}
	if text, ok, err := r.Show("", r.Abs(".changeset/new.md")); !ok || err != nil || text != "hi\n" {
		t.Errorf("staged: %q %v %v", text, ok, err)
	}
}

func TestStageUnstageDiscardCommit(t *testing.T) {
	r, _ := repo(t)
	a := filepath.Join(r.Root, "a.txt")
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	if err := r.Stage(a); err != nil {
		t.Fatal(err)
	}
	if text, ok, _ := r.Show("", a); !ok || text != "one\n2\nthree\n" {
		t.Fatalf("index has %q", text)
	}
	if err := r.Unstage(a); err != nil {
		t.Fatal(err)
	}
	if text, _, _ := r.Show("", a); text != "one\ntwo\nthree\n" {
		t.Fatalf("index has %q after unstaging", text)
	}
	if err := r.StageText(a, "one\ntwo\nthree\nfour\n"); err != nil {
		t.Fatal(err)
	}
	if err := r.Discard(a); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(a); string(data) != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("discarding gave %q", data)
	}
	if err := r.Commit("second\n\nbody", false); err != nil {
		t.Fatal(err)
	}
	if m := r.LastMessage(); m != "second\n\nbody" {
		t.Fatalf("message %q", m)
	}
	if err := r.Commit("second, amended", true); err != nil {
		t.Fatal(err)
	}
	if m := r.LastMessage(); m != "second, amended" {
		t.Fatalf("message %q", m)
	}
	if _, ok, err := r.Show("HEAD", filepath.Join(r.Root, "none.txt")); ok || err != nil {
		t.Fatalf("a file HEAD lacks: ok %v err %v", ok, err)
	}
}

func TestBranchesAndBlame(t *testing.T) {
	r, _ := repo(t)
	if err := r.Switch("topic", true); err != nil {
		t.Fatal(err)
	}
	bs, _ := r.Branches()
	if !slices.Equal(bs, []string{"main", "topic"}) {
		t.Fatalf("branches %v", bs)
	}
	a := filepath.Join(r.Root, "a.txt")
	b, err := r.BlameLine(a, 1, "one\ntwo\nthree\n")
	if err != nil || b.Commit == "" || b.Author != "t" || b.Summary != "first" {
		t.Fatalf("blame %+v %v", b, err)
	}
	b, err = r.BlameLine(a, 1, "one\nTWO\nthree\n")
	if err != nil || b.Commit != "" {
		t.Fatalf("blame of a line changed %+v %v", b, err)
	}
}

func TestPushPullFetch(t *testing.T) {
	r, git := repo(t)
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	git("remote", "add", "origin", remote)
	if err := r.Push("main", false); err != nil {
		t.Fatal(err)
	}
	st, _ := r.Status()
	if st.Upstream != "origin/main" || st.Ahead != 0 {
		t.Fatalf("upstream %q ahead %d", st.Upstream, st.Ahead)
	}
	write(t, r.Root, "a.txt", "changed\n")
	r.Stage(filepath.Join(r.Root, "a.txt"))
	r.Commit("third", false)
	if st, _ = r.Status(); st.Ahead != 1 {
		t.Fatalf("ahead %d", st.Ahead)
	}
	if err := r.Push("main", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if err := r.Pull(); err != nil {
		t.Fatal(err)
	}
	if st, _ = r.Status(); st.Ahead != 0 || st.Behind != 0 {
		t.Fatalf("ahead %d behind %d", st.Ahead, st.Behind)
	}
	git("remote", "set-url", "origin", "https://127.0.0.1:1/none.git")
	if err := r.Fetch(); err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("fetch from nowhere: %v", err)
	}
}

func TestStatusEqual(t *testing.T) {
	r, _ := repo(t)
	write(t, r.Root, "a.txt", "changed\n")
	st1, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := r.Status()
	if st1 == st2 || !st1.Equal(st2) {
		t.Errorf("two reads of one state differ: %+v %+v", st1, st2)
	}
	write(t, r.Root, "b.txt", "changed\n")
	st3, _ := r.Status()
	if st1.Equal(st3) {
		t.Errorf("a change is not seen")
	}
	var none *Status
	if none.Equal(st1) || !none.Equal(nil) {
		t.Errorf("nil statuses")
	}
}

func TestUndoCommit(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "a.txt", "second\n")
	git("commit", "-qam", "second")
	if err := r.UndoCommit(); err != nil {
		t.Fatal(err)
	}
	if msg := r.LastMessage(); msg != "first" {
		t.Errorf("last message %q", msg)
	}
	st, _ := r.Status()
	if len(st.Entries) != 1 || st.Entries[0].X != 'M' || st.Entries[0].Y != '.' {
		t.Errorf("the change is not staged: %+v", st.Entries)
	}
	// The first commit has no parent: HEAD goes before it, and every file
	// stays staged.
	if err := r.UndoCommit(); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status()
	if st.Head != "" || len(st.Entries) != 2 {
		t.Errorf("after undoing the first commit: %+v", st)
	}
}

func TestLogAndCommitFiles(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	write(t, r.Root, "c.txt", "sea\n")
	git("mv", "b.txt", "bb.txt")
	git("add", ".")
	git("commit", "-q", "-m", "second\n\nWith a body\nof two lines.")
	cs, err := r.Log(10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Subject != "second" || cs[0].Body != "With a body\nof two lines." || cs[1].Subject != "first" {
		t.Fatalf("log %+v", cs)
	}
	if cs[0].Author != "t" || len(cs[0].Parents) != 1 || cs[0].Parents[0] != cs[1].Hash || len(cs[1].Parents) != 0 {
		t.Errorf("commit %+v", cs[0])
	}
	if cs[0].Time.IsZero() || len(cs[0].Short()) != 7 {
		t.Errorf("time %v short %q", cs[0].Time, cs[0].Short())
	}
	fs, err := r.CommitFiles(cs[0])
	if err != nil {
		t.Fatal(err)
	}
	want := []CommitFile{
		{Path: r.Abs("a.txt"), Status: 'M'},
		{Path: r.Abs("bb.txt"), Orig: r.Abs("b.txt"), Status: 'R'},
		{Path: r.Abs("c.txt"), Status: 'A'},
	}
	if !slices.Equal(fs, want) {
		t.Errorf("files %+v, want %+v", fs, want)
	}
	// The first commit adds its files.
	fs, _ = r.CommitFiles(cs[1])
	if len(fs) != 2 || fs[0].Status != 'A' {
		t.Errorf("first commit %+v", fs)
	}
	info, err := r.CommitInfo(cs[0].Hash)
	if err != nil || info.Hash != cs[0].Hash || info.Body != cs[0].Body {
		t.Errorf("info %+v %v", info, err)
	}
	// Without an upstream, no remote has them.
	if u := r.Unpushed(10); len(u) != 2 {
		t.Errorf("unpushed without an upstream %v", u)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	git("init", "-q", "--bare", remote)
	git("remote", "add", "origin", remote)
	git("push", "-q", "origin", cs[1].Hash+":refs/heads/main")
	if u := r.Unpushed(10); len(u) != 1 || !u[cs[0].Hash] {
		t.Errorf("unpushed of a branch not published %v", u)
	}
}

func TestLogBeforeTheFirstCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cs, err := r.Log(10, false); err != nil || len(cs) != 0 {
		t.Errorf("log %v %v", cs, err)
	}
}

func TestBranchList(t *testing.T) {
	r, git := repo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git("init", "-q", "--bare", remote)
	git("remote", "add", "origin", remote)
	git("push", "-q", "-u", "origin", "main")
	git("switch", "-q", "-c", "topic")
	write(t, r.Root, "a.txt", "topic\n")
	git("commit", "-qam", "on topic")
	bs, err := r.BranchList()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
		if b.Time.IsZero() || b.Subject == "" {
			t.Errorf("branch %+v", b)
		}
	}
	slices.Sort(names)
	if want := []string{"main", "origin/main", "topic"}; !slices.Equal(names, want) {
		t.Errorf("branches %v, want %v", names, want)
	}
	for i, b := range bs {
		if i > 0 && b.Time.After(bs[i-1].Time) {
			t.Errorf("not by the last commit: %+v after %+v", b, bs[i-1])
		}
		if b.Name == "topic" && b.Subject != "on topic" {
			t.Errorf("topic %+v", b)
		}
	}
	for _, b := range bs {
		if b.Remote != strings.HasPrefix(b.Name, "origin/") {
			t.Errorf("remote %+v", b)
		}
	}
}

func TestUndoCommitInAShallowClone(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "a.txt", "second\n")
	git("commit", "-qam", "second")
	dir := filepath.Join(t.TempDir(), "shallow")
	if out, err := exec.Command("git", "clone", "-q", "--depth", "1", "file://"+r.Root, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	head := s.Head()
	if err := s.UndoCommit(); err == nil {
		t.Errorf("undid a commit whose parent the clone lacks")
	}
	if s.Head() != head || head == "" {
		t.Errorf("HEAD %q, was %q", s.Head(), head)
	}
}

func TestLogRefs(t *testing.T) {
	r, git := repo(t)
	git("tag", "v1")
	git("branch", "side")
	git("switch", "-q", "side")
	write(t, r.Root, "c.txt", "sea\n")
	git("add", ".")
	git("commit", "-q", "-m", "on side")
	git("switch", "-q", "main")
	if err := r.CreateTag("v2", "HEAD", "Version two"); err != nil {
		t.Fatal(err)
	}
	cs, err := r.Log(10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("log of HEAD %+v", cs)
	}
	want := []Ref{{Name: "main", Kind: RefBranch, Head: true}, {Name: "v1", Kind: RefTag}, {Name: "v2", Kind: RefTag}}
	got := slices.Clone(cs[0].Refs)
	slices.SortFunc(got, func(x, y Ref) int { return strings.Compare(x.Name, y.Name) })
	if !slices.Equal(got, want) {
		t.Errorf("refs %+v, want %+v", got, want)
	}
	// Every branch: side's commit first, after it the one it was made on.
	cs, _ = r.Log(10, true)
	if len(cs) != 2 || cs[0].Subject != "on side" || !slices.Equal(cs[0].Refs, []Ref{{Name: "side", Kind: RefBranch}}) {
		t.Fatalf("log of all %+v", cs)
	}
	if err := r.Checkout(cs[0].Hash); err != nil {
		t.Fatal(err)
	}
	info, _ := r.CommitInfo("HEAD")
	if len(info.Refs) != 2 || info.Refs[0] != (Ref{Name: "HEAD", Kind: RefHead}) {
		t.Errorf("detached refs %+v", info.Refs)
	}
	if err := r.CreateBranch("third", "main"); err != nil {
		t.Fatal(err)
	}
	if tags, _ := r.Tags(); !slices.Equal(tags, []string{"v1", "v2"}) {
		t.Errorf("tags %v", tags)
	}
}

func TestParseRefs(t *testing.T) {
	got := parseRefs("HEAD -> refs/heads/main, tag: refs/tags/v0.2.1, refs/remotes/origin/main, refs/remotes/origin/HEAD")
	want := []Ref{{Name: "main", Kind: RefBranch, Head: true}, {Name: "v0.2.1", Kind: RefTag}, {Name: "origin/main", Kind: RefRemote}}
	if !slices.Equal(got, want) {
		t.Errorf("refs %+v, want %+v", got, want)
	}
	if got := parseRefs(""); got != nil {
		t.Errorf("no refs %+v", got)
	}
}

// branchOff makes branch side with a commit of c.txt, and one of a.txt
// on main, which side lacks.
func branchOff(t *testing.T, r *Repo, git func(args ...string), a string) (side string) {
	t.Helper()
	git("switch", "-q", "-c", "side")
	write(t, r.Root, "c.txt", "sea\n")
	git("add", ".")
	git("commit", "-q", "-m", "side")
	side = r.Head()
	git("switch", "-q", "main")
	write(t, r.Root, "a.txt", a)
	git("commit", "-q", "-am", "main")
	return side
}

func TestMergeRebaseCherryPick(t *testing.T) {
	r, git := repo(t)
	side := branchOff(t, r, git, "one\nTWO\nthree\n")
	if err := r.CherryPick(side, false); err != nil {
		t.Fatal(err)
	}
	if r.LastMessage() != "side" {
		t.Errorf("picked %q", r.LastMessage())
	}
	git("reset", "-q", "--hard", "HEAD^")
	if err := r.Merge("side"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.LastMessage(), "Merge branch 'side'") {
		t.Errorf("merged %q", r.LastMessage())
	}
	git("reset", "-q", "--hard", "HEAD^")
	main := r.Head()
	git("switch", "-q", "side")
	if err := r.Rebase("main"); err != nil {
		t.Fatal(err)
	}
	if r.Count("main", "HEAD") != 1 || r.Count("HEAD", "main") != 0 || r.Head() == side {
		t.Errorf("side not on top of main %s", main)
	}
}

func TestConflictContinueAbort(t *testing.T) {
	r, git := repo(t)
	git("switch", "-q", "-c", "side")
	write(t, r.Root, "a.txt", "one\nSIDE\nthree\n")
	git("commit", "-q", "-am", "side change")
	side := r.Head()
	git("switch", "-q", "main")
	write(t, r.Root, "a.txt", "one\nMAIN\nthree\n")
	git("commit", "-q", "-am", "main change")
	head := r.Head()

	if r.InProgress() != "" {
		t.Fatal("in progress before")
	}
	if err := r.CherryPick(side, false); err == nil {
		t.Fatal("no conflict")
	}
	st, _ := r.Status()
	if st.Op != OpCherryPick {
		t.Fatalf("op %q", st.Op)
	}
	if msg := r.MergeMessage(); !strings.HasPrefix(msg, "side change") || strings.Contains(msg, "#") {
		t.Errorf("message %q", msg)
	}
	if err := r.Abort(OpCherryPick); err != nil {
		t.Fatal(err)
	}
	if r.InProgress() != "" || r.Head() != head {
		t.Fatalf("not aborted: %q", r.InProgress())
	}

	if err := r.Merge("side"); err == nil {
		t.Fatal("no conflict")
	}
	if r.InProgress() != OpMerge {
		t.Fatalf("op %q", r.InProgress())
	}
	write(t, r.Root, "a.txt", "one\nBOTH\nthree\n")
	git("add", "a.txt")
	if err := r.Continue(OpMerge); err != nil {
		t.Fatal(err)
	}
	if r.InProgress() != "" || r.Count(head, "HEAD") != 2 {
		t.Fatalf("not merged: %q", r.InProgress())
	}
}

func TestRebaseConflict(t *testing.T) {
	r, git := repo(t)
	git("switch", "-q", "-c", "side")
	write(t, r.Root, "a.txt", "one\nSIDE\nthree\n")
	git("commit", "-q", "-am", "side change")
	side := r.Head()
	git("switch", "-q", "main")
	write(t, r.Root, "a.txt", "one\nMAIN\nthree\n")
	git("commit", "-q", "-am", "main change")
	git("switch", "-q", "side")
	if err := r.Rebase("main"); err == nil {
		t.Fatal("no conflict")
	}
	if r.InProgress() != OpRebase {
		t.Fatalf("op %q", r.InProgress())
	}
	if err := r.Abort(OpRebase); err != nil {
		t.Fatal(err)
	}
	if r.InProgress() != "" || r.Head() != side {
		t.Fatal("not aborted")
	}
}

func TestRevertReset(t *testing.T) {
	r, git := repo(t)
	first := r.Head()
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	git("commit", "-q", "-am", "second")
	second := r.Head()
	if err := r.Revert(second, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Root, "a.txt")); string(b) != "one\ntwo\nthree\n" {
		t.Errorf("reverted to %q", b)
	}
	if err := r.Reset(second, ResetHard); err != nil {
		t.Fatal(err)
	}
	if err := r.Reset(first, ResetMixed); err != nil {
		t.Fatal(err)
	}
	st, _ := r.Status()
	if r.Head() != first || len(st.Entries) != 1 || st.Entries[0].Y != 'M' || st.Entries[0].X != '.' {
		t.Errorf("mixed: %v", st.Entries)
	}
	git("add", ".")
	if err := r.Reset(second, ResetSoft); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status()
	if r.Head() != second || len(st.Entries) != 0 {
		t.Errorf("soft: %v", st.Entries)
	}
}
