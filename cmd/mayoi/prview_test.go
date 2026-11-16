package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"mayoi/internal/git"
)

// prViewFixture is a pull request as gh tells of it in the tests: open,
// with a review, a comment and a commit, waiting on a check.
func prViewFixture(n int) *git.PRView {
	now := time.Now()
	return &git.PRView{ID: "PR_x", Number: n, Title: "Fix things", URL: "https://github.com/o/r/pull/5", State: "OPEN",
		Body: "It **fixes** the crash.", Author: "bob", Created: now.Add(-50 * time.Hour), Updated: now.Add(-2 * time.Hour),
		BaseRef: "main", HeadRef: "feature", Additions: 12, Deletions: 3, Files: 2, CanUpdate: true,
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Review: "REVIEW_REQUIRED", Methods: []string{"MERGE", "SQUASH"},
		Labels:    []git.GHLabel{{Name: "bug", Color: "d73a4a"}},
		Reviewers: []git.PRReviewer{{Login: "ann", State: "APPROVED"}, {Login: "cy", State: "REQUESTED"}},
		Linked:    []git.GHLinked{{Number: 3, Title: "Crash on open"}},
		Events: []git.PREvent{{Author: "ann", Body: "Looks good.", Created: now.Add(-3 * time.Hour), Review: "APPROVED"},
			{Author: "cy", Body: "Does it cover the empty folder?", Created: now.Add(-time.Hour)}},
		AllComments: 1,
		Commits:     []git.PRCommit{{Hash: "0123456789abcdef0123456789abcdef01234567", Subject: "Fix the crash", Author: "Bob", Login: "bob", Date: now.Add(-50 * time.Hour)}},
		AllCommits:  1,
		Avatars:     map[string]string{"bob": "https://avatars.test/bob", "ann": "https://avatars.test/ann"}}
}

// fakePR has gh tell of pull requests as prViewFixture does, with a check
// running, a failed one and a review thread on line 2 of a.txt, and
// restores gh once the test ends.
func fakePR(t testing.TB) {
	t.Helper()
	view, checks, threads := fetchPRView, fetchPRChecks, fetchPRThreads
	t.Cleanup(func() { fetchPRView, fetchPRChecks, fetchPRThreads = view, checks, threads })
	fetchPRView = func(_ *git.Repo, n int) (*git.PRView, error) { return prViewFixture(n), nil }
	fetchPRChecks = func(*git.Repo, int) ([]git.Check, error) {
		return []git.Check{{Name: "build", Bucket: "pass"}, {Name: "test", Bucket: "fail", Description: "2 failed", JobID: 7, RunID: 3},
			{Name: "lint", Bucket: "pending"}}, nil
	}
	fetchPRThreads = func(*git.Repo, int) ([]git.GHThread, error) {
		return []git.GHThread{{ID: "T1", Path: "a.txt", Line: 2, Comments: []git.GHComment{{Author: "ann", Body: "Why two?"}}},
			{ID: "T2", Path: "a.txt", Line: 1, Resolved: true, Comments: []git.GHComment{{Author: "cy", Body: "Fine."}}}}, nil
	}
}

// prTabOf is the tab of pull request n of the harness's work tree, at
// page, that does not read its changes, for the gallery.
func prTabOf(hs *harness, n int, page prPage) *diffView {
	a := hs.a
	v := newPendingPR(parseDiffLayout(a.settings.DiffLayout), n, "Fix things")
	v.repoRoot = a.git.repo.Root
	v.commit.pr.page, v.commit.pr.fetched = page, true
	col := a.fileColumn()
	a.addTab(col, len(col.tabs), &colTab{diff: v})
	a.activate(col, len(col.tabs)-1, true)
	return v
}

func TestPRPageKeys(t *testing.T) {
	for p := range prPages {
		if got := parsePRPage(p.key()); got != p {
			t.Errorf("%v: parsed %v", p, got)
		}
	}
	if parsePRPage("") != prConversation || parsePRPage("nope") != prConversation {
		t.Error("a page not known is not the conversation")
	}
}

// A pull request's tab reads its pages as it shows: the pull request, its
// checks and its threads; the page it shows stays as the tab is kept.
func TestPRViewReadsPages(t *testing.T) {
	fakePR(t)
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("a.txt"), []byte("one\n"), 0o644)
	run := hs.gitRepo()
	base := strings.TrimSpace(run("rev-parse", "HEAD"))
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\n"), 0o644)
	run("commit", "-qam", "more")
	head := strings.TrimSpace(run("rev-parse", "HEAD"))

	v := prTabOf(hs, 5, prConversation)
	pr := v.commit.pr
	hs.until("the pull request read", func() bool { return pr.view != nil && pr.checksOK && pr.threadsOK })
	if pr.view.Title != "Fix things" || len(pr.checks) != 3 || len(pr.ghThreads) != 2 {
		t.Fatalf("view %+v checks %d threads %d", pr.view, len(pr.checks), len(pr.ghThreads))
	}
	if got := a.sortedChecks(pr.checks); got[0].Name != "test" || got[1].Name != "lint" {
		t.Fatalf("checks in order %v", got)
	}

	// Its changes once read keep the page, and what was read.
	info := &git.PRInfo{Number: 5, ID: "PR_x", Title: "Fix things", State: "OPEN", BaseRef: "main", HeadRef: "feature", BaseOid: base, HeadOid: head}
	a.showPullRequest(info, base, nil, a.git.repo.Root)
	tab := a.column().tab()
	if tab.diff == v || tab.diff.commit.pr.page != prConversation || tab.diff.commit.pr.view == nil {
		t.Fatalf("pages not kept: %+v", tab.diff.commit.pr.prPageState)
	}
	a.setPRPage(tab.diff, prChecks)
	hs.settle()
	kept := false
	for _, c := range a.window.Columns {
		for _, ts := range c.Tabs {
			if strings.HasPrefix(ts.Diff, "commit:pr:5:") {
				kept = ts.PRPage == "checks"
			}
		}
	}
	if !kept {
		t.Fatal("the page shown is not kept with the tab")
	}
}

// A review thread shows on its line in a pull request's diff, a resolved
// one only once asked for.
func TestPRThreadsInDiff(t *testing.T) {
	fakePR(t)
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("a.txt"), []byte("one\n"), 0o644)
	run := hs.gitRepo()
	base := strings.TrimSpace(run("rev-parse", "HEAD"))
	os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\n"), 0o644)
	run("commit", "-qam", "more")
	head := strings.TrimSpace(run("rev-parse", "HEAD"))
	info := &git.PRInfo{Number: 5, ID: "PR_x", Title: "Fix things", State: "OPEN", BaseRef: "main", HeadRef: "feature", BaseOid: base, HeadOid: head}
	a.showPullRequest(info, base, nil, a.git.repo.Root)
	v := a.column().tab().diff
	pr := v.commit.pr
	if pr.page != prChanges {
		t.Fatalf("page %v, want the changes", pr.page)
	}
	hs.until("the threads read", func() bool { return pr.threadsOK })
	hs.settleDiff(v)
	cards := func() []string {
		var ids []string
		for _, r := range v.rows {
			if th := v.threadAt(r.n); r.kind == rowComment && th != nil {
				ids = append(ids, th.ID)
			}
		}
		return ids
	}
	if got := cards(); !slices.Equal(got, []string{"T1"}) {
		t.Fatalf("cards %v, want T1", got)
	}
	pr.showResolved = true
	pr.threadGen++
	v.changed()
	hs.settleDiff(v)
	if got := cards(); len(got) != 2 {
		t.Fatalf("cards %v, want both", got)
	}
}

// The tab of an issue that turns out to be a pull request, as one kept of
// its conversation, becomes the pull request's.
func TestIssueTabBecomesPR(t *testing.T) {
	fakePR(t)
	hs := githubHarness(t, ghOverviewFixture(), nil)
	a := hs.a
	fetchIssue = func(_ *git.Repo, n int) (*git.GHIssue, error) {
		return &git.GHIssue{Number: n, PR: true, Title: "Fix things", State: "OPEN"}, nil
	}
	a.openIssue(a.repository(), 5, true)
	hs.until("the tab a pull request's", func() bool {
		t := a.column().tab()
		return t != nil && t.isPR()
	})
	if pr := a.column().tab().diff.commit.pr; pr.number != 5 || pr.page != prConversation {
		t.Fatalf("pr %d page %v", pr.number, pr.page)
	}
}
