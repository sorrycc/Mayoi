package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"mayoi/internal/git"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// ghOverviewFixture is a repository's issues and pull requests as GitHub
// would tell: a pull request whose review is asked, one of the viewer's,
// another's, and two issues, one assigned to the viewer.
func ghOverviewFixture() *git.GHOverview {
	now := time.Now()
	return &git.GHOverview{
		Viewer: "me", Repo: "o/r", URL: "https://github.com/o/r", Permission: "WRITE", IssuesOn: true, DefaultBranch: "main",
		PRs: []git.GHItem{
			{Number: 7, Title: "Speed up the tree", Author: "ann", PR: true, ReviewAsked: true, Checks: git.ChecksPass, Updated: now, URL: "https://github.com/o/r/pull/7"},
			{Number: 8, Title: "Mine to finish", Author: "me", PR: true, Draft: true, Checks: git.ChecksPending, Updated: now},
			{Number: 9, Title: "Someone else's", Author: "bob", PR: true, Updated: now, Labels: []git.GHLabel{{Name: "docs", Color: "0075ca"}}},
		},
		Issues: []git.GHItem{
			{Number: 3, Title: "Crash on open", Author: "bob", Assignees: []string{"me"}, Comments: 4, Updated: now, URL: "https://github.com/o/r/issues/3"},
			{Number: 4, Title: "Wider sidebar", Author: "cat", Updated: now, Labels: []git.GHLabel{{Name: "enhancement", Color: "a2eeef"}}},
		},
		OpenPRs: 3, OpenIssues: 30,
	}
}

// fakeGitHub has the harness's repository be on GitHub, as overview and
// err say gh tells of it, and restores gh once the test ends.
func fakeGitHub(t testing.TB, overview *git.GHOverview, err error) {
	t.Helper()
	remotes, fetch, issue := readGitHubRemotes, fetchGitHubOverview, fetchIssue
	t.Cleanup(func() { readGitHubRemotes, fetchGitHubOverview, fetchIssue = remotes, fetch, issue })
	readGitHubRemotes = func(*git.Repo) ([]git.GitHubRemote, error) {
		return []git.GitHubRemote{{Name: "origin", Host: "github.com", Path: "o/r"}}, nil
	}
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return overview, err }
	fetchIssue = func(_ *git.Repo, n int) (*git.GHIssue, error) {
		return &git.GHIssue{Number: n, Title: "Crash on open", URL: "https://github.com/o/r/issues/3", State: "OPEN", Author: "bob",
			Body: "It **crashes** as the folder opens.", CanUpdate: true, Created: time.Now().Add(-26 * time.Hour),
			Comments: []git.GHComment{{Author: "ann", Body: "Seen it too."}}, AllComments: 1}, nil
	}
}

// galleryGitHub shows the GitHub view of the harness's work tree, as
// overview and err say gh tells of it, for the gallery.
func galleryGitHub(hs *harness, overview *git.GHOverview, err error) {
	fakeGitHub(hs.t, overview, err)
	hs.h.visible = true
	hs.gitRepo()
	hs.a.showGitHub()
}

// githubHarness is a harness on a work tree that is on GitHub.
func githubHarness(t *testing.T, overview *git.GHOverview, err error) *harness {
	fakeGitHub(t, overview, err)
	hs := newHarness(t, false)
	hs.h.visible = true
	hs.gitRepo()
	hs.settle()
	return hs
}

func TestGitHubViewListsIssuesAndPRs(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	if !hs.a.hasGitHub() {
		t.Fatal("a repository on GitHub has no GitHub view")
	}
	hs.a.showGitHub()
	hs.settle()
	for _, title := range []string{"Speed up the tree", "Mine to finish", "Someone else's", "Crash on open", "Wider sidebar"} {
		if _, ok := hs.tt.Find(title); !ok {
			t.Errorf("%q is not listed", title)
		}
	}
	if _, ok := hs.tt.Find(locales.GhMoreOnGitHub(28)); !ok {
		t.Error("the issues GitHub has beyond those listed are not counted")
	}
	if n := hs.a.githubCount(); n != 1 {
		t.Errorf("the switch counts %d, want the 1 pull request whose review is asked", n)
	}
	// The history's marks are read from the view's pull requests.
	if heads := hs.a.git.prHeadsRepo; heads != hs.a.git.repo {
		t.Error("the marks of the history were not read from the GitHub view")
	}
	hs.a.github.query = "crash"
	hs.settle()
	if _, ok := hs.tt.Find("Crash on open"); !ok {
		t.Error("the issue that matches the query is not listed")
	}
	if _, ok := hs.tt.Find("Wider sidebar"); ok {
		t.Error("an issue that does not match the query is listed")
	}
	hs.a.github.query = "zzzz"
	hs.settle()
	if _, ok := hs.tt.Find(locales.GhNoMatch()); !ok {
		t.Error("no word that nothing matches")
	}
}

func TestGitHubSectionsFold(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	ctx := hs.a.repository()
	rows := hs.a.githubRows(ctx, ctx.githubOverview(), hs.tt.Now())
	var sections []string
	for _, r := range rows {
		if r.header {
			sections = append(sections, r.section)
		}
	}
	if got := strings.Join(sections, " "); got != "review mine assigned issues prs" {
		t.Fatalf("sections = %q", got)
	}
	hs.a.github.collapsed["issues"] = true
	hs.settle()
	if _, ok := hs.tt.Find("Wider sidebar"); ok {
		t.Error("a section folded still shows its rows")
	}
	if _, ok := hs.tt.Find("Crash on open"); !ok {
		t.Error("the issue assigned, of another section, is not listed")
	}
}

// TestGitHubRowsComeIn: the rows of the GitHub view are built again only
// as what they show changes, and those of a section opened fade in, while
// its header holds still under the pointer.
func TestGitHubRowsComeIn(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	ctx := hs.a.repository()
	o := ctx.githubOverview()
	rows := hs.a.githubRows(ctx, o, hs.tt.Now())
	if again := hs.a.githubRows(ctx, o, hs.tt.Now()); &again[0] != &rows[0] {
		t.Error("the rows were built again with nothing changed")
	}
	gp.query = "crash"
	if again := hs.a.githubRows(ctx, o, hs.tt.Now()); len(again) == len(rows) {
		t.Error("the rows were not built again for a query")
	}
	gp.query = ""
	hs.settle()

	head := hs.a.pal.title(locales.GhIssues())
	at, ok := hs.tt.Find(head)
	if !ok {
		t.Fatalf("no header %q", head)
	}
	hs.tt.Move(at.X+at.W/2, at.Y+at.H/2)
	hs.settle()
	if now, _ := hs.tt.Find(head); now != at {
		t.Errorf("the header moved under the pointer: %v, was %v", now, at)
	}

	gp.collapsed["issues"] = true
	hs.settle()
	gp.collapsed["issues"] = false
	step(hs.tt)
	if k := gp.appear.shown("issues:4", hs.tt.Now()); k >= 1 {
		t.Errorf("a row of a section opened shows at once (%v)", k)
	}
	hs.settle()
	if k := gp.appear.shown("issues:4", hs.tt.Now()); k != 1 {
		t.Errorf("a row of a section opened stays faded (%v)", k)
	}
}

func TestGitHubViewOnlyOnGitHub(t *testing.T) {
	fakeGitHub(t, nil, nil)
	readGitHubRemotes = func(*git.Repo) ([]git.GitHubRemote, error) { return nil, nil }
	asked := false
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { asked = true; return nil, errors.New("asked") }
	hs := newHarness(t, false)
	hs.h.visible = true
	hs.gitRepo()
	hs.a.window.SidebarView = "github"
	hs.settle()
	if hs.a.hasGitHub() {
		t.Fatal("a repository with no remote on GitHub has the GitHub view")
	}
	if asked {
		t.Error("GitHub was asked of a repository that is not on it")
	}
	if _, ok := hs.tt.Find(locales.GhReading()); ok {
		t.Error("the sidebar shows the GitHub view of a repository not on GitHub")
	}
}

func TestGitHubProblems(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: To get started with GitHub CLI, please run:  gh auth login", git.ErrNoGH), locales.GhSignedOut()},
		{fmt.Errorf("%w: exec: \"gh\": executable file not found in $PATH", git.ErrNoGH), locales.GhMissing()},
		{git.ErrNoDefaultRepo, locales.GhNoDefault()},
		{fmt.Errorf("%w: HTTP 502", git.ErrNoGH), locales.GhFailed()},
	} {
		t.Run(c.want, func(t *testing.T) {
			hs := githubHarness(t, nil, c.err)
			hs.a.showGitHub()
			hs.settle()
			if _, ok := hs.tt.Find(c.want); !ok {
				t.Errorf("%v does not say %q", c.err, c.want)
			}
		})
	}
}

func TestGitHubKeepsWhatWasRead(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return nil, fmt.Errorf("%w: HTTP 502", git.ErrNoGH) }
	hs.a.repository().refreshGitHubNow()
	hs.settle()
	if _, ok := hs.tt.Find("Crash on open"); !ok {
		t.Error("what was read vanished as reading it again failed")
	}
	if _, ok := hs.tt.Find(locales.GhStale("HTTP 502")); !ok {
		t.Error("no word that reading it again failed")
	}
}

func TestIssueTab(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.openIssue(hs.a.repository(), 3, true)
	hs.settle()
	tab := hs.a.activeTab()
	if tab == nil || tab.issue == nil || tab.name() != "#3 Crash on open" {
		t.Fatalf("the tab shown is %v", tab)
	}
	for _, s := range []string{"Crash on open", "Seen it too.", locales.GhWorkOnIssue(), locales.GhCloseIssue()} {
		if _, ok := hs.tt.Find(s); !ok {
			t.Errorf("the issue's tab does not show %q", s)
		}
	}
	// The tab comes back with the session.
	hs.a.rememberTabs()
	var kept *settings.Tab
	for _, col := range hs.a.window.Columns {
		for i := range col.Tabs {
			if col.Tabs[i].Issue == 3 {
				kept = &col.Tabs[i]
			}
		}
	}
	if kept == nil || kept.RepoRoot != "." {
		t.Fatalf("the issue's tab was kept as %+v", kept)
	}
	// Opening it again shows the same tab.
	hs.a.openIssue(hs.a.repository(), 3, false)
	n := 0
	for _, col := range hs.a.cols {
		for _, t := range col.tabs {
			if t.issue != nil {
				n++
			}
		}
	}
	if n != 1 {
		t.Errorf("%d tabs of the issue, want 1", n)
	}
}

func TestIssueBranch(t *testing.T) {
	for _, c := range []struct {
		n     int
		title string
		want  string
	}{
		{12, "Fix the crash on open!", "12-fix-the-crash-on-open"},
		{161, "zoom调大之后顶栏外观问题", "161-zoom"},
		{160, "建议支持在 Vim 模式下配置传统编辑器快捷键", "160-vim"},
		{5, "设置", "issue-5"},
		{7, strings.Repeat("word ", 30), "7-" + strings.TrimSuffix(strings.Repeat("word-", 8), "-")},
	} {
		if got := issueBranch(c.n, c.title); got != c.want {
			t.Errorf("issueBranch(%d, %q) = %q, want %q", c.n, c.title, got, c.want)
		}
	}
}

func TestIssuePrompt(t *testing.T) {
	is := &git.GHIssue{Number: 3, Title: "Crash", URL: "u", Body: "Steps\n", Labels: []git.GHLabel{{Name: "bug"}},
		Comments: []git.GHComment{{Author: "ann", Body: "Me too"}, {Body: "gone"}}}
	got := issuePrompt("o/r", is, "3-crash")
	for _, want := range []string{"GitHub issue #3 of o/r: Crash\nu\n", "Labels: bug", "Steps", "@ann:\nMe too", "@ghost:\ngone",
		"on branch 3-crash", `"Closes #3"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt has no %q:\n%s", want, got)
		}
	}
	if strings.Contains(issuePrompt("o/r", is, ""), "on branch") {
		t.Error("the prompt of an issue sent with no branch names one")
	}
}

func TestThreadNotes(t *testing.T) {
	hs := newHarness(t, false)
	gitc := hs.gitRepo()
	if err := os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {\n\tprintln(1)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitc("add", ".")
	gitc("commit", "-q", "-m", "main")
	head := strings.TrimSpace(gitc("rev-parse", "HEAD"))
	r := hs.a.git.repo
	key := prKey(7, head, head)
	notes := threadNotes(r, key, head, head, []git.GHThread{
		{ID: "T1", Path: "main.go", Line: 4, StartLine: 3, Comments: []git.GHComment{{Author: "ann", Body: "Name it"}, {Author: "me", Body: "OK"}}},
		{ID: "T2", Path: "main.go", Line: 1, Resolved: true, Comments: []git.GHComment{{Body: "done"}}},
		{ID: "T3", Path: "main.go", Line: 99, Comments: []git.GHComment{{Body: "past the end"}}},
	})
	if len(notes) != 1 {
		t.Fatalf("%d notes, want the one thread not resolved and on lines of the file", len(notes))
	}
	n := notes[0]
	if n.path != hs.path("main.go") || n.commit != key || n.from != 2 || n.to != 4 || n.thread != "T1" ||
		strings.Join(n.snippet, "\n") != "func main() {\n\tprintln(1)" || n.text != "@ann: Name it\n\n@me: OK" {
		t.Fatalf("note = %+v", n)
	}
	hs.a.addThreadNotes(7, notes)
	hs.a.addThreadNotes(7, notes)
	if len(hs.a.review.notes) != 1 {
		t.Errorf("%d notes after adding the same thread twice", len(hs.a.review.notes))
	}
	// The thread comes back with the session.
	kept := hs.a.keptReview()
	hs.a.restoreReview(kept)
	if len(hs.a.review.notes) != 1 || hs.a.review.notes[0].thread != "T1" {
		t.Errorf("the thread of the note kept is lost: %+v", kept)
	}
}

func TestIssueHandoff(t *testing.T) {
	hs := newHarness(t, false)
	handOffIssue(hs.root, issueHandoff{prompt: "Work on GitHub issue #3", of: "#3"})
	hs.settle()
	var s *session
	for _, t := range hs.a.terms {
		if t.prompt != "" {
			s = t
		}
	}
	if s == nil {
		t.Fatal("no terminal holds the issue's prompt")
	}
	if s.promptOf != "#3" || s.dir != hs.root {
		t.Errorf("terminal = %+v", s)
	}
	issueHandoffs.Lock()
	left := len(issueHandoffs.of)
	issueHandoffs.Unlock()
	if left != 0 {
		t.Errorf("%d handoffs left once taken", left)
	}
}

func TestGHPictureHost(t *testing.T) {
	for u, want := range map[string]bool{
		"https://github.com/user-attachments/assets/1":        true,
		"https://private-user-images.githubusercontent.com/1": true,
		"https://ghe.corp/o/r/assets/1":                       true,
		"https://evil.example/x.png":                          false,
	} {
		if got := ghPictureHost(u, "https://ghe.corp/o/r"); got != want {
			t.Errorf("ghPictureHost(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestGHPictures(t *testing.T) {
	in := `See <img width="1280" alt="图片" src="https://github.com/user-attachments/assets/a1" /> and <IMG SRC='x.png'> and <img alt="none">`
	want := `See ![图片](<https://github.com/user-attachments/assets/a1>) and ![](<x.png>) and <img alt="none">`
	if got := ghPictures(in); got != want {
		t.Errorf("ghPictures = %q, want %q", got, want)
	}
}

func TestSendIssueWithNoTerminal(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.sendIssue(hs.a.repository(), 3)
	hs.settle()
	got := hs.tt.Clipboard()
	if !strings.Contains(got, "Work on GitHub issue #3 of o/r: Crash on open") || strings.Contains(got, "on branch") {
		t.Errorf("clipboard = %q", got)
	}
}

// TestGitHubRowActions: under the pointer, the row of an item shows its
// actions and, as the pointer rests, its card with every label, while its
// title holds still.
func TestGitHubRowActions(t *testing.T) {
	o := ghOverviewFixture()
	o.PRs[2].Labels = append(o.PRs[2].Labels, git.GHLabel{Name: "one"}, git.GHLabel{Name: "two"}, git.GHLabel{Name: "fourth label"})
	o.PRs[2].URL, o.PRs[2].HeadRef, o.PRs[2].BaseRef = "https://github.com/o/r/pull/9", "bob/fix", "main"
	hs := githubHarness(t, o, nil)
	hs.a.showGitHub()
	hs.settle()
	at, ok := hs.tt.Find("Someone else's")
	if !ok {
		t.Fatal("no row")
	}
	if _, ok := hs.tt.Find(locales.GhCheckOut()); ok {
		t.Fatal("the actions show with no pointer over the row")
	}
	hs.tt.Move(at.X+4, at.Y+at.H/2)
	hs.settle()
	if now, _ := hs.tt.Find("Someone else's"); now != at {
		t.Errorf("the title moved under the pointer: %v, was %v", now, at)
	}
	for _, label := range []string{locales.GhCheckOut(), locales.PrOpenOnGitHub(), locales.GhCopyLink()} {
		if _, ok := hs.tt.Find(label); !ok {
			t.Errorf("no %q under the pointer", label)
		}
	}
	for i := 0; i < 80; i++ {
		step(hs.tt)
	}
	if _, ok := hs.tt.Find("fourth label"); !ok {
		t.Error("the card does not show the labels the row has no room for")
	}
	if _, ok := hs.tt.Find(locales.ScmCardBranch()); !ok {
		t.Error("the card does not show the pull request's branch")
	}
	tabs := len(hs.a.fileColumn().tabs)
	if err := hs.tt.Click(locales.GhCopyLink()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.a.services.ReadClipboard(); got != "https://github.com/o/r/pull/9" {
		t.Errorf("Copy Link copied %q", got)
	}
	if n := len(hs.a.fileColumn().tabs); n != tabs {
		t.Errorf("a click on an action opened the row too: %d tabs, were %d", n, tabs)
	}
}

// TestGitHubStaleHoldsTheList: the line that GitHub could not be read
// again comes over the list, which stays where it is.
func TestGitHubStaleHoldsTheList(t *testing.T) {
	o := ghOverviewFixture()
	var err error
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return o, err }
	hs.a.showGitHub()
	hs.settle()
	at, ok := hs.tt.Find("Speed up the tree")
	if !ok {
		t.Fatal("no row")
	}
	err = fmt.Errorf("%w: HTTP 502", git.ErrNoGH)
	hs.a.repository().refreshGitHubNow()
	hs.settle()
	if _, ok := hs.tt.Find(locales.GhStale("HTTP 502")); !ok {
		t.Fatal("no word that GitHub could not be read again")
	}
	if now, _ := hs.tt.Find("Speed up the tree"); now != at {
		t.Errorf("the list moved as the line came: %v, was %v", now, at)
	}
}
