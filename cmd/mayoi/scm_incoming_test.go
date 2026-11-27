package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

// incomingRepo is a work tree with a remote, a peer that pushed two
// commits, and optionally a local commit ahead: the state a fetch leaves
// a branch in.
type incomingRepo struct {
	hs     *harness
	remote string
	peer   string
	git    func(args ...string) string
}

func newIncomingRepo(t *testing.T, ahead bool) *incomingRepo {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	r := &incomingRepo{hs: hs, remote: t.TempDir(), peer: t.TempDir()}
	r.git = hs.gitRepo()
	gitIn(t, r.remote, "init", "-q", "--bare", "-b", "main")
	r.git("remote", "add", "origin", r.remote)
	r.git("push", "-q", "-u", "origin", "main")
	gitIn(t, r.peer, "clone", "-q", r.remote, ".")
	gitIn(t, r.peer, "config", "user.email", "t@t")
	gitIn(t, r.peer, "config", "user.name", "t")
	gitIn(t, r.peer, "commit", "-q", "--allow-empty", "-m", "remote one")
	if err := os.WriteFile(filepath.Join(r.peer, "remote.txt"), []byte("from remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r.peer, "add", "remote.txt")
	gitIn(t, r.peer, "commit", "-q", "-m", "remote two")
	gitIn(t, r.peer, "push", "-q", "origin", "main")
	if ahead {
		r.git("commit", "-q", "--allow-empty", "-m", "local one")
	}
	return r
}

// fetch has the view fetch the remote, as its button does.
func (r *incomingRepo) fetch(t *testing.T) {
	t.Helper()
	r.hs.a.run("view.scm")
	r.hs.settleGit()
	r.hs.a.fetch()
	r.hs.settleGit()
}

// rows lists the commit rows of the history, in order, marking those not
// pulled.
func (r *incomingRepo) rows() []string {
	var out []string
	in := r.hs.a.git.incoming
	for _, row := range r.hs.a.scmRows() {
		if row.commit == nil || row.at < 0 {
			continue
		}
		if in[row.commit.Hash] {
			out = append(out, "not-pulled:"+row.commit.Subject)
		} else {
			out = append(out, "commits:"+row.commit.Subject)
		}
	}
	return out
}

// commitRow is the history row of the commit whose subject is subject.
func commitRow(t *testing.T, r *incomingRepo, subject string) scmRow {
	t.Helper()
	for _, row := range r.hs.a.scmRows() {
		if row.commit != nil && row.at >= 0 && row.commit.Subject == subject {
			return row
		}
	}
	t.Fatalf("no row of the commit %q: %q", subject, r.hs.tt.Texts())
	return scmRow{}
}

// TestScmIncoming covers the full fetched-but-unpulled experience on one
// shared diverged repository: dirty-tree fetch, row ordering and graph
// lanes, hover card, preview click, file-tree expansion, all-branches
// deduplication, and isolation from Repository panel commits.
func TestScmIncoming(t *testing.T) {
	r := newIncomingRepo(t, true)
	// A dirty work tree must not stop Fetch, nor lose its local edits.
	dirtyPath := r.hs.path("main.go")
	dirtyText := "package main // local edit\n"
	if err := os.WriteFile(dirtyPath, []byte(dirtyText), 0o644); err != nil {
		t.Fatal(err)
	}
	r.fetch(t)
	if len(r.hs.a.errors) != 0 {
		t.Fatalf("fetch on dirty tree failed: %q", r.hs.a.errors)
	}
	if got, err := os.ReadFile(dirtyPath); err != nil || string(got) != dirtyText {
		t.Fatalf("dirty file after fetch = %q, %v", got, err)
	}

	t.Run("shown and graph", func(t *testing.T) {
		got := strings.Join(r.rows(), " | ")
		want := "not-pulled:remote two | not-pulled:remote one | commits:local one"
		if !strings.HasPrefix(got, want) {
			t.Fatalf("rows\n%s\nwant prefix\n%s", got, want)
		}
		if len(r.hs.a.git.incoming) != 2 {
			t.Fatalf("read %d commits not pulled", len(r.hs.a.git.incoming))
		}
		for _, s := range []string{"remote two", "remote one"} {
			if !r.hs.tt.HasText(s) {
				t.Errorf("no %q in %q", s, r.hs.tt.Texts())
			}
		}
		g := r.hs.a.historyGraph()
		if len(g) != len(r.hs.a.git.history) {
			t.Fatalf("graph of %d rows for %d commits", len(g), len(r.hs.a.git.history))
		}
		seen := 0
		for _, row := range r.hs.a.scmRows() {
			if row.commit == nil || row.at < 0 {
				continue
			}
			if row.at != seen {
				t.Fatalf("row %q has at=%d, want %d", row.key, row.at, seen)
			}
			seen++
		}
		if seen != len(g) {
			t.Fatalf("saw %d commit rows, graph has %d", seen, len(g))
		}
	})

	t.Run("hover card", func(t *testing.T) {
		r.hs.tt.Advance(2 * time.Second)
		r.hs.settle()
		hover := func(subject string) {
			t.Helper()
			box, ok := r.hs.tt.Find(subject)
			if !ok {
				t.Fatalf("no %q in %q", subject, r.hs.tt.Texts())
			}
			r.hs.tt.Move(box.X+box.W/2, box.Y+box.H/2)
			r.hs.tt.Advance(time.Second)
			r.hs.settle()
		}
		hover("remote two")
		if !r.hs.tt.HasText(locales.ScmNotPulled()) {
			t.Errorf("hover card for incoming commit missing %q: %q", locales.ScmNotPulled(), r.hs.tt.Texts())
		}
		hover("local one")
		if r.hs.tt.HasText(locales.ScmNotPulled()) {
			t.Errorf("hover card for local commit should not show %q: %q", locales.ScmNotPulled(), r.hs.tt.Texts())
		}
		r.hs.tt.Move(1, 1)
		r.hs.settle()
	})

	t.Run("revert waits for pull", func(t *testing.T) {
		revert := func(subject string) error {
			t.Helper()
			if err := r.hs.tt.RightClick(subject); err != nil {
				t.Fatal(err)
			}
			r.hs.settle()
			defer r.hs.tt.CloseMenu()
			if !slices.Contains(r.hs.tt.Menu(), locales.ScmRevertCommit()) {
				t.Fatalf("menu of %q = %q", subject, r.hs.tt.Menu())
			}
			return r.hs.tt.ChooseMenuItem(locales.ScmRevertCommit())
		}
		if err := revert("remote two"); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("Revert Commit on an incoming commit: %v", err)
		}
		if err := revert("local one"); err != nil || r.hs.a.confirm == nil {
			t.Fatalf("Revert Commit on a local commit: %v, asked %v", err, r.hs.a.confirm != nil)
		}
		r.hs.a.dismiss()
		r.hs.settle()
	})

	t.Run("click previews commit", func(t *testing.T) {
		want := commitRow(t, r, "remote two").commit.Hash
		if err := r.hs.tt.Click("remote two"); err != nil {
			t.Fatal(err)
		}
		r.hs.settle()
		tab := r.hs.a.activeTab()
		if tab == nil || tab.diff == nil || tab.diff.commit == nil || tab.diff.commit.hash != want {
			t.Fatalf("clicking incoming commit previewed %+v", tab)
		}
	})

	t.Run("expand real files", func(t *testing.T) {
		row := commitRow(t, r, "remote two")
		r.hs.a.repository().toggleCommitOpen(*row.commit)
		r.hs.until("incoming commit files loaded", func() bool {
			rd := r.hs.a.scm.commitFiles[row.commit.Hash]
			return rd != nil && !rd.loading
		})
		r.hs.settle()
		var fileRows []scmRow
		for _, sr := range r.hs.a.scmRows() {
			if sr.under != nil && sr.under.Hash == row.commit.Hash && sr.note == "" && sr.dir == "" {
				fileRows = append(fileRows, sr)
			}
		}
		if len(fileRows) != 1 || filepath.Base(fileRows[0].e.Path) != "remote.txt" {
			t.Fatalf("expanded file rows = %+v", fileRows)
		}
		if fileRows[0].at != row.at {
			t.Errorf("file row lane = %d, commit row lane = %d", fileRows[0].at, row.at)
		}
		if !r.hs.tt.HasText("remote.txt") {
			t.Errorf("expanded file remote.txt missing from UI: %q", r.hs.tt.Texts())
		}
		r.hs.a.repository().toggleCommitOpen(*row.commit)
		r.hs.settle()
	})

	t.Run("all branches no duplicate", func(t *testing.T) {
		r.hs.a.toggleAllBranches()
		r.hs.settleGit()
		defer func() {
			r.hs.a.toggleAllBranches()
			r.hs.settleGit()
		}()
		counts := map[string]int{}
		for _, row := range r.hs.a.scmRows() {
			if row.commit != nil && row.at >= 0 {
				counts[row.commit.Hash]++
			}
		}
		for hash, n := range counts {
			if n != 1 {
				t.Errorf("commit %s shown %d times in all-branches mode", hash, n)
			}
		}
		if len(r.hs.a.git.incoming) != 2 {
			t.Fatalf("read %d commits not pulled in all-branches mode", len(r.hs.a.git.incoming))
		}
		for hash := range r.hs.a.git.incoming {
			if counts[hash] != 1 {
				t.Errorf("incoming commit %s shown %d times in all-branches mode", hash, counts[hash])
			}
		}
	})

	t.Run("panel commit unaffected", func(t *testing.T) {
		a := r.hs.a
		a.settings.ScmRepositoryOpen = true
		r.hs.settle()
		settleScmPanels(t, r.hs)
		main := findPanelRow(t, r.hs, "panel:branch:refs/heads/main")
		a.activateScmPanelRow(main)
		settleScmPanels(t, r.hs)
		inRow := findPanelRow(t, r.hs, main.key+":incoming")
		ctx := a.repository()
		panelCommitIdx := -1
		var panelCommit scmRow
		for i, row := range a.scmRows() {
			if row.commit != nil && strings.HasPrefix(row.key, inRow.key+":") {
				panelCommitIdx, panelCommit = i, row
				break
			}
		}
		if panelCommitIdx < 0 {
			t.Fatalf("no incoming panel commit row: %q", r.hs.tt.Texts())
		}
		if panelCommit.at != -1 {
			t.Errorf("panel commit has at=%d, want -1", panelCommit.at)
		}
		tip := scmCommitTip(panelCommit.commit, refBadges(panelCommit.commit.Refs), ctx.commitExtras(panelCommit))
		if strings.Contains(tip, locales.ScmNotPulled()) {
			t.Errorf("panel commit tip %q should not carry history not-pulled note", tip)
		}
		// Focus the SCM list by clicking a history commit and confirm
		// KeyRight works there before verifying a panel commit ignores it.
		local := commitRow(t, r, "local one")
		if err := r.hs.tt.Click("local one"); err != nil {
			t.Fatal(err)
		}
		r.hs.settle()
		r.hs.tt.Key(0, ui.KeyRight)
		r.hs.settle()
		if !a.scm.openCommits[local.commit.Hash] {
			t.Fatal("KeyRight did not open focused history commit; list is not receiving keys")
		}
		r.hs.tt.Key(0, ui.KeyLeft)
		r.hs.settle()
		a.scm.row = panelCommitIdx
		r.hs.settle()
		r.hs.tt.Key(0, ui.KeyRight)
		r.hs.settle()
		if a.scm.openCommits[panelCommit.commit.Hash] {
			t.Errorf("KeyRight opened commit files on panel commit %s", panelCommit.commit.Hash)
		}
	})
}

// A pull brings incoming commits into HEAD's history, where they show
// once as local commits.
func TestScmIncomingClearsAfterPull(t *testing.T) {
	r := newIncomingRepo(t, false)
	r.fetch(t)
	if len(r.hs.a.git.incoming) != 2 {
		t.Fatalf("read %d commits not pulled", len(r.hs.a.git.incoming))
	}
	r.hs.a.pull()
	r.hs.settleGit()
	if n := len(r.hs.a.git.incoming); n != 0 {
		t.Fatalf("%d commits not pulled after the pull", n)
	}
	got := strings.Join(r.rows(), " | ")
	want := "commits:remote two | commits:remote one | commits:first"
	if got != want {
		t.Fatalf("rows %s, want %s", got, want)
	}
}

// Nothing to compare with shows nothing and raises no error: no upstream,
// a gone upstream, or a detached HEAD.
func TestScmIncomingNothingToCompare(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	hs.a.run("view.scm")
	hs.settleGit()
	check := func(label string) {
		t.Helper()
		if n := len(hs.a.git.incoming); n != 0 {
			t.Fatalf("%s: %d commits not pulled", label, n)
		}
		if len(hs.a.errors) != 0 {
			t.Errorf("%s: errors %q", label, hs.a.errors)
		}
	}
	run("commit", "-q", "--allow-empty", "-m", "local")
	hs.settleGit()
	check("no upstream")

	// Configured upstream whose remote-tracking ref is gone.
	run("config", "branch.main.remote", "origin")
	run("config", "branch.main.merge", "refs/heads/main")
	run("config", "remote.origin.url", "unused")
	run("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	hs.a.git.refsGen++
	hs.settleGit()
	check("gone upstream")

	run("checkout", "-q", "--detach")
	hs.settleGit()
	check("detached")
}

// A file's history shows only the commits of HEAD that changed it: none
// of the upstream's leak in, nor a page more, and going back to the
// branch's history shows them again, once each, past another page.
func TestScmIncomingScopedHistory(t *testing.T) {
	r := newIncomingRepo(t, true)
	r.fetch(t)
	if len(r.hs.a.git.incoming) != 2 {
		t.Fatalf("read %d commits not pulled", len(r.hs.a.git.incoming))
	}
	for _, follow := range []bool{true, false} {
		r.hs.a.rootRepository().setHistory([]string{r.hs.path("main.go")}, follow)
		if len(r.hs.a.git.incoming) != 0 || r.hs.a.git.historyMore {
			t.Fatalf("follow=%v: switching scope kept %d incoming, more %v", follow, len(r.hs.a.git.incoming), r.hs.a.git.historyMore)
		}
		r.hs.settleGit()
		if got := strings.Join(r.rows(), " | "); got != "commits:first" {
			t.Fatalf("follow=%v: file history rows %s", follow, got)
		}
		if len(r.hs.a.git.incoming) != 0 || r.hs.a.git.historyMore {
			t.Fatalf("follow=%v: file history read %d incoming, more %v", follow, len(r.hs.a.git.incoming), r.hs.a.git.historyMore)
		}
		r.hs.a.rootRepository().clearHistory()
		r.hs.settleGit()
	}
	r.hs.a.git.historyN = 2 * historyPage
	r.hs.settleGit()
	got := strings.Join(r.rows(), " | ")
	want := "not-pulled:remote two | not-pulled:remote one | commits:local one | commits:first"
	if got != want {
		t.Fatalf("branch history rows\n%s\nwant\n%s", got, want)
	}
	if r.hs.a.git.historyMore {
		t.Fatal("a short history offers more")
	}
}
