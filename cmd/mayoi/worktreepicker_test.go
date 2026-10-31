package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// worktreeFixture is the harness's repository with a linked worktree
// named side, on a branch of the same name, and the box of the worktrees
// open on it.
func worktreeFixture(t *testing.T) (*harness, func(args ...string) string, string) {
	t.Helper()
	hs := newHarness(t, true)
	run := hs.gitRepo()
	side := filepath.Join(t.TempDir(), "side")
	run("worktree", "add", "-b", "side", side)
	return hs, run, side
}

// openWorktrees clicks the worktree button of the window's folder in source
// control and waits until the worktrees are read.
func openWorktrees(t *testing.T, hs *harness) {
	t.Helper()
	openSCM(hs)
	if err := hs.tt.Click(locales.WorktreeStatusTip(filepath.Base(hs.a.root))); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	awaitWorktrees(t, hs)
}

// awaitWorktrees draws frames until the worktrees the box opened on are read.
func awaitWorktrees(t *testing.T, hs *harness) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for hs.a.worktrees.loading && time.Now().Before(deadline) {
		hs.settle()
	}
	if hs.a.worktrees.loading {
		t.Fatal("the worktree box never finished reading the worktrees")
	}
}

// The worktree box opens from source control, shuts on Escape, and lets go of
// the worktrees it read.
func TestSourceControlWorktreeBox(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	a := hs.a
	openWorktrees(t, hs)
	if !a.worktrees.open || len(a.worktrees.trees) != 2 {
		t.Fatalf("box open %v with %d worktrees", a.worktrees.open, len(a.worktrees.trees))
	}
	// The one open comes first, and has its check; the other is by its folder.
	if first := a.worktrees.trees[0]; !first.Current || first.Branch != "main" {
		t.Fatalf("first worktree %+v is not the one open", first)
	}
	if !hs.tt.HasText("side") {
		t.Fatal("box does not name the linked worktree")
	}
	// Escape shuts the box and lets go of what it read.
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.worktrees.open || a.worktrees.trees != nil || a.worktrees.names != nil {
		t.Fatal("closed box kept its worktrees")
	}
}

func TestWorktreeBoxSwitchesToLinked(t *testing.T) {
	hs, run, side := worktreeFixture(t)
	openWorktrees(t, hs)
	if err := hs.tt.Click("side"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	want, err := filepath.EvalSymlinks(side)
	if err != nil {
		t.Fatal(err)
	}
	if hs.a.root != want {
		t.Fatalf("window shows %q, want %q", hs.a.root, want)
	}
	if strings.TrimSpace(run("symbolic-ref", "--short", "HEAD")) != "main" || hs.a.fails != 0 {
		t.Fatalf("switching changed the branch or failed %d times", hs.a.fails)
	}
	// The status bar names the linked folder it is in.
	if !hs.tt.HasText(filepath.Base(want)) {
		t.Fatal("status bar does not name the linked worktree")
	}
}

func TestWorktreeBoxCurrentOnlyCloses(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	a := hs.a
	root := a.root
	openWorktrees(t, hs)
	// Enter on the first row, the one open, shuts the box and does nothing else.
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if a.worktrees.open || a.root != root || a.fails != 0 {
		t.Fatalf("choosing the open worktree: open %v root %q failures %d", a.worktrees.open, a.root, a.fails)
	}
}

func TestWorktreeBoxMissingFolderIsReported(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	gone := filepath.Join(t.TempDir(), "gone")
	run("worktree", "add", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	root, fails := a.root, a.fails
	openWorktrees(t, hs)
	if err := hs.tt.Click("gone"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// The failure is shown as a message, which the frames clear: it counts.
	if a.root != root || a.fails != fails+1 {
		t.Fatalf("missing folder: root %q, failures %d before %d", a.root, a.fails, fails)
	}
	// Nothing was pruned: Git still lists the worktree, and the branch
	// stays where it was.
	if list := run("worktree", "list"); !strings.Contains(list, "gone") {
		t.Fatalf("worktree list lost the missing worktree:\n%s", list)
	}
	if strings.TrimSpace(run("symbolic-ref", "--short", "HEAD")) != "main" {
		t.Fatal("missing folder changed the branch")
	}
}

func TestWorktreeBoxFuzzyAndCreate(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	openWorktrees(t, hs)
	// A query naming an existing worktree offers no creation.
	hs.tt.Type("side")
	hs.settle()
	for _, ch := range a.repository().worktreeChoices() {
		if ch.create != "" {
			t.Fatal("offered to create a worktree named like an existing one")
		}
	}
	if len(a.repository().worktreeChoices()) == 0 {
		t.Fatal("query side matched no worktree")
	}
	hs.tt.Key(ui.Cmd, ui.KeyA)
	hs.tt.Type("feature/new")
	hs.settle()
	if err := hs.tt.Click(locales.WorktreeCreateLabel("feature/new")); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if a.worktrees.open {
		t.Fatal("box still open after creating")
	}
	if !slices.Contains(strings.Fields(run("branch", "--format=%(refname:short)")), "feature/new") {
		t.Fatal("branch feature/new was not made")
	}
	if !strings.HasSuffix(a.root, filepath.Join(".worktrees", "feature-new")) {
		t.Fatalf("window shows %q, not the new worktree folder", a.root)
	}
	if strings.TrimSpace(run("rev-parse", "HEAD")) != strings.TrimSpace(run("rev-parse", "feature/new")) {
		t.Fatal("new worktree does not start from the commit HEAD was at")
	}
}

func TestWorktreeBoxBusyDisabled(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	openSCM(hs)
	hs.a.git.busy = "held operation"
	hs.settle()
	if err := hs.tt.Click(locales.WorktreeStatusTip(filepath.Base(hs.root))); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.a.worktrees.open {
		t.Fatal("busy repository opened the worktree box")
	}
	hs.a.git.busy = ""
}

func TestWorktreeBoxStaysWithItsRepository(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	parentRoot := a.repository().git.repo.Root
	openSCM(hs)
	if err := hs.tt.Click(locales.WorktreeStatusTip(filepath.Base(parentRoot))); err != nil {
		t.Fatal(err)
	}
	// Selecting the module before the worktrees arrive must not move the
	// box to it: its results belong to the repository it opened on.
	a.selectRepository(sub.git.repo.Root)
	hs.settle()
	awaitWorktrees(t, hs)
	if a.worktrees.repoRoot != parentRoot || len(a.worktrees.trees) != 1 {
		t.Fatalf("box of %q has %d worktrees", a.worktrees.repoRoot, len(a.worktrees.trees))
	}
}

// Opening and shutting the box, and switching between the worktrees, many
// times leaves no goroutine and no heap behind: what the box read is let go
// when it shuts.
func TestWorktreeBoxDoesNotLeak(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	a := hs.a
	cycle := func(n int) {
		for range n {
			openWorktrees(t, hs)
			hs.tt.Key(0, ui.KeyEscape)
			hs.settle()
		}
	}
	// Warm up what the first use of the box allocates once.
	cycle(2)
	base, baseHeap := settledGoroutines(hs), heapInUse()
	cycle(50)
	main := mustEval(t, hs.root)
	for range 5 {
		openWorktrees(t, hs)
		if err := hs.tt.Click("side"); err != nil {
			t.Fatal(err)
		}
		hs.settle()
		openWorktrees(t, hs)
		if err := hs.tt.Click(filepath.Base(hs.root)); err != nil {
			t.Fatal(err)
		}
		hs.settle()
	}
	if a.root != main {
		t.Fatalf("switching back did not return to the main worktree: %q", a.root)
	}
	got := settledGoroutines(hs)
	grown := int64(heapInUse()) - int64(baseHeap)
	t.Logf("goroutines %d before, %d after 55 box cycles; heap in use %+d KB", base, got, grown>>10)
	if got > base {
		t.Fatalf("goroutines %d after the cycles, %d before", got, base)
	}
	if grown > 8<<20 {
		t.Fatalf("heap grew by %d KB over the cycles", grown>>10)
	}
	if a.worktrees.trees != nil || a.worktrees.names != nil {
		t.Fatal("closed box kept what it read")
	}
}

// settledGoroutines draws frames until background work ends, and reports
// how many goroutines run then.
func settledGoroutines(hs *harness) int {
	for range 20 {
		hs.settle()
	}
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

// heapInUse collects garbage, and reports the heap in use after it.
func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Reading a worktree's folder holds no git command busy, and a switch made
// before the reading ends wins: the reading does not switch the window back.
func TestWorktreeOpenHoldsNoGit(t *testing.T) {
	hs, run, side := worktreeFixture(t)
	a := hs.a
	other := filepath.Join(t.TempDir(), "other")
	run("worktree", "add", "-b", "other", other)
	pending := a.git.pending
	a.repository().openWorktree(side)
	if a.git.busy != "" || a.git.pending != pending {
		t.Fatalf("opening a worktree holds git: busy %q, pending %d, was %d", a.git.busy, a.git.pending, pending)
	}
	a.switchFolder(other)
	settledGoroutines(hs)
	if a.root != other {
		t.Fatalf("window shows %q, want %q: the reading of the folder switched it back", a.root, other)
	}
}

// A query that only a worktree's folder path contains is not a name of it:
// Enter creates the branch it names, not switch to the worktree whose path
// it happens to be in.
func TestWorktreeBoxPathOnlyCreates(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	main := a.root
	// A linked worktree whose path has the query, and whose folder and branch do not.
	dir := filepath.Join(t.TempDir(), "pathonly")
	run("worktree", "add", "-b", "side2", filepath.Join(dir, "side2"))
	openWorktrees(t, hs)
	// The corpora are built here, not read from the box, which clears them when
	// Enter closes it: folder and branch alone match nothing, the path does.
	var names, legacy []string
	for _, tr := range a.worktrees.trees {
		names = append(names, worktreeName(tr)+" "+tr.Branch)
		legacy = append(legacy, worktreeName(tr)+" "+tr.Branch+" "+tr.Path)
	}
	if len(workspace.Fuzzy(names, "pathonly", len(names))) != 0 {
		t.Fatal("setup: a worktree's folder or branch matches the query")
	}
	if len(workspace.Fuzzy(legacy, "pathonly", len(legacy))) == 0 {
		t.Fatal("setup: the query matches no worktree path")
	}
	// The worktree is made beside the main one, from the commit HEAD is at.
	want := filepath.Join(filepath.Dir(main), filepath.Base(main)+".worktrees", "pathonly")
	base := strings.TrimSpace(run("rev-parse", "HEAD"))
	hs.tt.Type("pathonly")
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if !slices.Contains(strings.Fields(run("branch", "--format=%(refname:short)")), "pathonly") {
		t.Fatal("Enter did not create branch pathonly")
	}
	wantEval, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustEval(t, a.root); got != wantEval {
		t.Fatalf("window shows %q, want the new worktree folder %q", got, wantEval)
	}
	if got := strings.TrimSpace(run("rev-parse", "pathonly")); got != base {
		t.Fatalf("branch pathonly starts at %s, want the commit HEAD was at, %s", got, base)
	}
}

// Enter switches to a worktree whose folder has the query as typed, and
// creates for a new name that matches a worktree only by scattered letters.
func TestWorktreeBoxScatteredMatchCreates(t *testing.T) {
	hs, run, side := worktreeFixture(t)
	a := hs.a
	openWorktrees(t, hs)
	hs.tt.Type("sd")
	hs.settle()
	if len(a.worktrees.results) == 0 {
		t.Fatal("setup: sd matches no worktree")
	}
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	if !slices.Contains(strings.Fields(run("branch", "--format=%(refname:short)")), "sd") {
		t.Fatal("Enter on sd did not create branch sd")
	}
	if filepath.Base(a.root) != "sd" {
		t.Fatalf("window shows %q, want the new worktree sd", a.root)
	}
	openWorktrees(t, hs)
	hs.tt.Type("sid")
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if got := mustEval(t, a.root); got != mustEval(t, side) {
		t.Fatalf("Enter on sid shows %q, want the worktree side", got)
	}
}

// A local branch that no worktree has checked out is named as one to check
// out with Switch Branch…, and offers no worktree; a branch a worktree has
// checked out is not.
func TestWorktreeBoxExistingBranchHint(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	run("branch", "orphan")
	hs.a.gitChanged()
	hs.settleGit()
	openWorktrees(t, hs)
	hs.tt.Type("orphan")
	hs.settle()
	if !hs.tt.HasText(locales.WorktreeBranchExists("orphan")) {
		t.Fatal("no hint for a branch no worktree has checked out")
	}
	for _, ch := range hs.a.repository().worktreeChoices() {
		if ch.create != "" {
			t.Fatal("offered to create a worktree on an existing branch")
		}
	}
	for _, name := range []string{"side", "main"} {
		hs.tt.Key(ui.Cmd, ui.KeyA)
		hs.tt.Type(name)
		hs.settle()
		if hs.tt.HasText(locales.WorktreeBranchExists(name)) {
			t.Fatalf("hint for %s, which a worktree has checked out", name)
		}
	}
}

// With HEAD detached, the row to create says which commit it starts from,
// by its short hash.
func TestWorktreeBoxCreateRowDetached(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	run("checkout", "-q", "--detach")
	hs.a.gitChanged()
	hs.settleGit()
	short := git.ShortHash(strings.TrimSpace(run("rev-parse", "HEAD")))
	openWorktrees(t, hs)
	hs.tt.Type("newone")
	hs.settle()
	if !hs.tt.HasText(locales.WorktreeFrom(short)) {
		t.Fatalf("create row does not say it is based on %s", short)
	}
}

// openSCM shows source control, where the worktree and the branch buttons
// are, and waits for its frames.
func openSCM(hs *harness) {
	hs.a.showSourceControl()
	hs.settle()
}

// The branch stays in the status bar when source control is closed;
// source control also has its branch and worktree buttons.
func TestWorktreeInSourceControlAndBranchInStatusBar(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	hs.tt.SetSize(1200, 760)
	hs.settle()
	tip := locales.WorktreeStatusTip(filepath.Base(hs.root))
	branch := locales.BranchLabel("main")
	if _, ok := hs.tt.Find(tip); ok {
		t.Fatal("explorer shows the worktree button outside source control")
	}
	if r, ok := hs.tt.Find(branch); !ok || r.Y < 760-statusBarH {
		t.Fatalf("status bar has no branch button: %v (shown %v)", r, ok)
	}
	if err := hs.tt.Click(branch); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.a.branches.open {
		t.Fatal("status bar branch button did not open the picker")
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	openSCM(hs)
	if r, ok := hs.tt.Find(tip); !ok || r.Y < titleBarH || r.Y > 760-statusBarH {
		t.Fatalf("source control has no worktree button: %v (shown %v)", r, ok)
	}
	n := 0
	for _, label := range hs.tt.Texts() {
		if label == branch {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want branch buttons in source control and status bar, got %d", n)
	}
}

// The worktree button names a linked worktree's folder, and the main one
// shows its icon alone, in source control.
func TestSourceControlWorktreeButtonNamesLinked(t *testing.T) {
	hs, _, side := worktreeFixture(t)
	hs.tt.SetSize(1200, 760)
	openSCM(hs)
	if _, ok := hs.tt.Find(locales.WorktreeStatusTip(filepath.Base(hs.root))); !ok {
		t.Fatal("source control has no worktree button in the main worktree")
	}
	if hs.tt.HasText(filepath.Base(side)) {
		t.Fatal("the main worktree's button names a linked folder")
	}
	hs.a.switchFolder(side)
	hs.settleGit()
	openSCM(hs)
	if _, ok := hs.tt.Find(locales.WorktreeStatusTip(filepath.Base(side))); !ok || !hs.tt.HasText(filepath.Base(side)) {
		t.Fatal("the linked worktree's button does not name its folder")
	}
}

// control is a button that must be shown whole at a sidebar's width: its
// visible rectangle is at least least wide, which a clipped one is not.
type control struct {
	label string
	least float32
}

// Least widths of the controls: an icon with its padding, and a branch with
// its icon, name and chevron.
const (
	iconLeast   = 20
	branchLeast = 40
)

// fitsSidebar checks that the controls are shown whole at width w, inside the
// sidebar and clear of each other.
func fitsSidebar(t *testing.T, tt *ui.Tester, w float32, controls []control) {
	t.Helper()
	var rs []ui.Rect
	for _, c := range controls {
		r, ok := tt.Find(c.label)
		if !ok {
			t.Fatalf("sidebar %v: %q is not shown", w, c.label)
		}
		if r.W < c.least || r.X < 0 || r.X+r.W > w {
			t.Fatalf("sidebar %v: %q shown at %v, least width %v", w, c.label, r, c.least)
		}
		for _, o := range rs {
			if overlaps(o, r) {
				t.Fatalf("sidebar %v: %q at %v overlaps %v", w, c.label, r, o)
			}
		}
		rs = append(rs, r)
	}
}

// A linked worktree with a long folder name and branch, in a source control
// row narrowed to the sidebar's least width. With an upstream, its remote
// buttons leave the row below scmRemoteRowW, and the menu keeps them; a
// branch without one keeps its publish button at every width.
func TestSourceControlRowFitsNarrowSidebar(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	long := strings.Repeat("F", 48)
	branch := strings.Repeat("b", 48)
	dir := filepath.Join(t.TempDir(), long)
	run("worktree", "add", "-b", branch, dir)
	gitIn(t, dir, "push", "-q", "-u", "origin", branch)
	a.switchFolder(dir)
	hs.settleGit()
	openSCM(hs)
	hs.settleGit()
	// Draw the sidebar alone so polling cannot replace the test's status,
	// and labels cannot resolve to the status bar's matching buttons.
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(a.window.SidebarWidth).Height(760).Children(func() { a.sidebar(c) })
	}, 600, 760)
	draw := func() {
		for range 3 {
			step(tt)
		}
	}
	name := locales.WorktreeStatusTip(filepath.Base(dir))
	more := control{locales.ScmMoreActions(), iconLeast}
	remoteButtons := []control{
		{locales.CmdGitFetch(), iconLeast}, {locales.CmdGitPull(), iconLeast}, {locales.CmdGitPush(), iconLeast},
	}
	for _, w := range []float32{160, 240, 400} {
		a.window.SidebarWidth = w
		draw()
		controls := []control{{name, iconLeast}, {locales.BranchLabel(branch), branchLeast}, more}
		if w >= scmRemoteRowW {
			controls = append(controls, remoteButtons...)
		} else if _, ok := tt.Find(locales.CmdGitFetch()); ok {
			t.Fatalf("sidebar %v: fetch is shown in a row too narrow for it", w)
		}
		fitsSidebar(t, tt, w, controls)
	}
	// Counts take room of their own: a row with them shows its remote buttons
	// only where they fit, and wide, always.
	a.window.SidebarWidth = 160
	a.git.status.Ahead, a.git.status.Behind = 10, 10
	draw()
	for _, w := range []float32{160, 180, 200, 240, 400} {
		a.window.SidebarWidth = w
		draw()
		controls := []control{{name, iconLeast}, {locales.BranchLabel(branch), branchLeast}, more}
		if _, ok := tt.Find(locales.CmdGitFetch()); ok || w >= 400 {
			controls = append(controls, control{locales.CmdGitFetch(), iconLeast},
				control{locales.ScmPullCount(10), iconLeast}, control{locales.ScmPushCount(10), iconLeast})
		}
		fitsSidebar(t, tt, w, controls)
	}
	a.git.status.Ahead, a.git.status.Behind = 0, 0

	// A pull request of the branch shows its badge in the row, which takes its
	// room before the remote buttons: a plain one, and one with checks and a
	// review, which is the wider. Each is checked with and without counts.
	plain := &git.PR{Number: 93, State: "OPEN", URL: "https://example.com/pull/93"}
	reviewed := &git.PR{Number: 93, State: "OPEN", URL: "https://example.com/pull/93", Review: "APPROVED", Checks: git.ChecksPass, Passed: 3}
	for _, pr := range []*git.PR{plain, reviewed} {
		a.git.pr, a.git.prRepo, a.git.prBranch = pr, a.git.repo, branch
		for _, n := range []int{0, 10} {
			a.git.status.Ahead, a.git.status.Behind = n, n
			pull, push := locales.CmdGitPull(), locales.CmdGitPush()
			if n > 0 {
				pull, push = locales.ScmPullCount(n), locales.ScmPushCount(n)
			}
			draw()
			for _, w := range []float32{160, 200, 240, 400} {
				a.window.SidebarWidth = w
				draw()
				// The badge is shown whole or by its number beside the worktree and
				// branch buttons at every width; the remote buttons show where they fit.
				controls := []control{{name, iconLeast}, {locales.BranchLabel(branch), branchLeast}, {prTip(pr), iconLeast}, more}
				if _, ok := tt.Find(locales.CmdGitFetch()); ok || w >= 400 {
					controls = append(controls, control{locales.CmdGitFetch(), iconLeast}, control{pull, iconLeast}, control{push, iconLeast})
				}
				fitsSidebar(t, tt, w, controls)
			}
		}
	}
	a.git.status.Ahead, a.git.status.Behind = 0, 0
	a.git.pr, a.git.prRepo, a.git.prBranch = nil, nil, ""

	// A branch without an upstream: fetch and publish stay at every width.
	dir2 := filepath.Join(t.TempDir(), long+"2")
	run("worktree", "add", "-b", branch+"2", dir2)
	a.switchFolder(dir2)
	hs.settleGit()
	openSCM(hs)
	for _, w := range []float32{160, 240, 400} {
		a.window.SidebarWidth = w
		draw()
		fitsSidebar(t, tt, w, []control{
			{locales.WorktreeStatusTip(filepath.Base(dir2)), iconLeast}, {locales.BranchLabel(branch + "2"), branchLeast},
			{locales.CmdGitFetch(), iconLeast}, {locales.ScmPublishBranch(), iconLeast}, more,
		})
	}
}

// overlaps reports whether two rectangles share more than a sub-pixel edge.
func overlaps(a, b ui.Rect) bool {
	const eps = 0.5
	return a.X+eps < b.X+b.W && b.X+eps < a.X+a.W && a.Y+eps < b.Y+b.H && b.Y+eps < a.Y+a.H
}

// With no row to move among, the arrows and Enter do nothing, rather than
// choose a row that is not there.
func TestWorktreeBoxNoRowsArrowsAndEnter(t *testing.T) {
	hs, _, _ := worktreeFixture(t)
	openWorktrees(t, hs)
	hs.tt.Type("bad..no-match-xyz")
	hs.settle()
	for _, k := range []ui.Key{ui.KeyDown, ui.KeyUp, ui.KeyDown} {
		hs.tt.Key(0, k)
		hs.settle()
	}
	if !hs.a.worktrees.open || hs.a.fails != 0 {
		t.Fatalf("no rows: failures %d, open %v", hs.a.fails, hs.a.worktrees.open)
	}
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if hs.a.fails != 0 {
		t.Fatalf("Enter with no rows: failures %d", hs.a.fails)
	}
}

// The row that creates a worktree names the base it starts from as the box
// opened, even when the branch changes while the box is open.
func TestWorktreeBoxCreateRowFrozenAtOpening(t *testing.T) {
	hs, run, _ := worktreeFixture(t)
	before := git.ShortHash(strings.TrimSpace(run("rev-parse", "HEAD")))
	openWorktrees(t, hs)
	run("checkout", "-q", "-b", "elsewhere")
	run("commit", "-q", "--allow-empty", "-m", "moved")
	hs.a.gitChanged()
	hs.settleGit()
	hs.tt.Type("newone")
	hs.settle()
	if !hs.tt.HasText(locales.WorktreeFrom("main (" + before + ")")) {
		t.Fatalf("create row does not keep the base of the opening: %v", hs.tt.Texts())
	}
}

// The worktree button of a repository's header in a multi-repository source
// control opens the box of that repository, even when another one is selected,
// and changing the selection later does not move it.
func TestWorktreeButtonBindsItsRepository(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	parentRoot := a.repository().git.repo.Root
	subRoot := sub.git.repo.Root
	a.selectRepository(subRoot)
	hs.settle()
	openSCM(hs)
	if err := hs.tt.Click(locales.WorktreeStatusTip(filepath.Base(parentRoot))); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	awaitWorktrees(t, hs)
	if a.worktrees.repoRoot != parentRoot {
		t.Fatalf("box of %q, want the header's repository %q", a.worktrees.repoRoot, parentRoot)
	}
	a.selectRepository(parentRoot)
	hs.settle()
	if a.worktrees.repoRoot != parentRoot {
		t.Fatalf("selecting another repository moved the box to %q", a.worktrees.repoRoot)
	}
}

// The header of each repository in a multi-repository source control is
// rendered alone, so each of its controls is the one its label finds. At each
// sidebar width its controls are shown whole, and the submodule's wide header
// shows its remote buttons, counts included, and its more menu lists them.
func TestRepositoryHeaderFitsNarrowSidebar(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	parentRoot := a.repository().git.repo.Root
	parent := a.repositoryForRoot(parentRoot)
	// The submodule gets a branch of its own and a remote with an upstream, so
	// its header has the remote actions to show.
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	gitIn(t, sub.git.repo.Root, "checkout", "-q", "-b", "sub-work")
	gitIn(t, sub.git.repo.Root, "remote", "set-url", "origin", remote)
	gitIn(t, sub.git.repo.Root, "push", "-q", "-u", "origin", "sub-work")
	sub.gitChanged()
	hs.settleGit()
	checkHeader(t, parent, false)
	checkHeader(t, sub, true)
}

// checkHeader renders ctx's header alone at each sidebar width, and checks its
// controls there; at 360 and wider, with remote, it checks the actions too, and
// the more menu lists them.
func checkHeader(t *testing.T, ctx *gitContext, remote bool) {
	t.Helper()
	width := float32(160)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(width).Children(func() { ctx.repositoryHeader(c) })
	}, 600, 100)
	name := ctx.label()
	base := filepath.Base(ctx.git.repo.Root)
	controls := []control{
		{locales.ScmToggleRepository(name), iconLeast},
		{locales.ScmSelectRepository(name), iconLeast},
		{locales.WorktreeStatusTip(base), iconLeast},
		{locales.BranchLabel(ctx.branch()), branchLeast},
		{locales.ScmMoreActions(), iconLeast},
	}
	for _, w := range []float32{160, 240, 360, 400} {
		width = w
		for range 3 {
			step(tt)
		}
		shown := controls
		if w >= 360 && remote {
			shown = append(append([]control{}, controls...),
				control{locales.CmdGitFetch(), iconLeast}, control{locales.ScmPullCount(10), iconLeast}, control{locales.ScmPushCount(10), iconLeast})
		}
		if ctx.git.status != nil {
			ctx.git.status.Ahead, ctx.git.status.Behind = 10, 10
		}
		for range 3 {
			step(tt)
		}
		fitsSidebar(t, tt, w, shown)
	}
	if !remote {
		return
	}
	if err := tt.Click(locales.ScmMoreActions()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	for _, label := range []string{locales.CmdGitFetch(), locales.CmdGitPull(), locales.CmdGitPush()} {
		if !slices.Contains(tt.Menu(), label) {
			t.Fatalf("the more menu of the header lacks %q: %q", label, tt.Menu())
		}
	}
}

// The source control branch row alone, rendered in its own tester, so that
// the badge it shows is the one it draws and not the status bar's. A long
// linked name does not make the badge show its number alone: the name shrinks
// to make room for the whole badge, at a wide sidebar, and after a narrow one.
func TestSourceControlBadgeWholeWhenRoomAllows(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	branch := strings.Repeat("b", 48)
	dir := filepath.Join(t.TempDir(), strings.Repeat("F", 48))
	run("worktree", "add", "-b", branch, dir)
	gitIn(t, dir, "push", "-q", "-u", "origin", branch)
	a.switchFolder(dir)
	hs.settleGit()
	ctx := a.repository()
	width := float32(400)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(width).Children(func() { ctx.scmBranchRow(c) })
	}, 600, 60)
	for range 3 {
		step(tt)
	}
	reviewed := &git.PR{Number: 93, State: "OPEN", URL: "https://example.com/pull/93", Review: "APPROVED", Checks: git.ChecksPass, Passed: 3}
	ctx.git.pr, ctx.git.prRepo, ctx.git.prBranch = reviewed, ctx.git.repo, branch
	// The badge's label is its tooltip too, so the width tells the two apart:
	// the number alone is about 34 DIP, the whole badge about 124.
	whole := func(where string, w float32) {
		t.Helper()
		for range 3 {
			step(tt)
		}
		r, ok := tt.Find(prTip(reviewed))
		if !ok || r.X < 0 || r.X+r.W > w {
			t.Fatalf("%s: badge at %v (shown %v) leaves the row of %v", where, r, ok, w)
		}
		if r.W < 60 {
			t.Fatalf("%s: the badge shows its number alone at %v: %v", where, w, r)
		}
	}
	whole("wide, long name", 400)
	width = 160
	for range 3 {
		step(tt)
	}
	width = 400
	whole("wide again after a narrow row", 400)
	width = 200
	for range 3 {
		step(tt)
	}
	r, ok := tt.Find(prTip(reviewed))
	if !ok || r.X < 0 || r.X+r.W > 200 {
		t.Fatalf("at 200 the badge is at %v (shown %v), off the row", r, ok)
	}
	ctx.git.pr, ctx.git.prRepo, ctx.git.prBranch = nil, nil, ""
}

// A pull request that arrives after the row is built shows its badge, and the
// row lays out around it. The test makes two explicit redraw requests with
// Frame, not settle, which runs the view's frames up to twenty; it does not
// count frames, since the headless tester has no single-frame step.
func TestPullRequestBadgeAppearsInAFrame(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs, run, _ := worktreeFixture(t)
	a := hs.a
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	branch := strings.Repeat("b", 48)
	dir := filepath.Join(t.TempDir(), strings.Repeat("F", 48))
	run("worktree", "add", "-b", branch, dir)
	gitIn(t, dir, "push", "-q", "-u", "origin", branch)
	a.switchFolder(dir)
	hs.settleGit()
	openSCM(hs)
	hs.settleGit()
	// Draw the sidebar alone so polling cannot replace the test's status,
	// and labels cannot resolve to the status bar's matching buttons.
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(a.window.SidebarWidth).Height(760).Children(func() { a.sidebar(c) })
	}, 600, 760)
	draw := func() {
		for range 3 {
			step(tt)
		}
	}
	name := filepath.Base(dir)
	reviewed := &git.PR{Number: 93, State: "OPEN", URL: "https://example.com/pull/93", Review: "APPROVED", Checks: git.ChecksPass, Passed: 3}
	for _, w := range []float32{200, 400} {
		a.window.SidebarWidth = w
		draw()
		a.git.pr, a.git.prRepo, a.git.prBranch = reviewed, a.git.repo, branch
		// Two redraw requests: the first draws the badge, the second lays out the
		// row by the badge's width as it was drawn.
		tt.Frame()
		tt.Frame()
		controls := []control{{locales.WorktreeStatusTip(name), iconLeast}, {locales.BranchLabel(branch), branchLeast}, {prTip(reviewed), iconLeast}, {locales.ScmMoreActions(), iconLeast}}
		if _, ok := tt.Find(locales.CmdGitFetch()); ok || w >= 400 {
			controls = append(controls, control{locales.CmdGitFetch(), iconLeast}, control{locales.CmdGitPull(), iconLeast}, control{locales.CmdGitPush(), iconLeast})
		}
		fitsSidebar(t, tt, w, controls)
		a.git.pr, a.git.prRepo, a.git.prBranch = nil, nil, ""
		tt.Frame()
		tt.Frame()
	}
}
