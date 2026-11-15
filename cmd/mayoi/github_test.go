package main

import (
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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
			{Number: 7, Title: "Speed up the tree", Author: "ann", PR: true, ReviewAsked: true, Checks: git.ChecksPass, Updated: now, URL: "https://github.com/o/r/pull/7",
				Reviewers: []git.GHReviewer{{Login: "bob", State: "APPROVED"}, {Login: "cat", State: "COMMENTED"}, {Login: "me", State: "REQUESTED", Again: true}}},
			{Number: 8, Title: "Mine to finish", Author: "me", PR: true, Draft: true, Checks: git.ChecksPending, Updated: now},
			{Number: 9, Title: "Someone else's", Author: "bob", PR: true, Updated: now, Labels: []git.GHLabel{{Name: "docs", Color: "0075ca"}},
				Reviewers: []git.GHReviewer{{Login: "ann", State: "APPROVED"}, {Login: "cat", State: "CHANGES_REQUESTED"},
					{Login: "dan", State: "REQUESTED"}, {Login: "eve", State: "COMMENTED"}}},
		},
		Issues: []git.GHItem{
			{Number: 3, Title: "Crash on open", Author: "bob", Assignees: []string{"me"}, Comments: 4, Updated: now, URL: "https://github.com/o/r/issues/3"},
			{Number: 4, Title: "Wider sidebar", Author: "cat", Updated: now, Labels: []git.GHLabel{{Name: "enhancement", Color: "a2eeef"}}},
		},
		OpenPRs: 3, OpenIssues: 30,
		Avatars: map[string]string{"ann": "https://avatars.test/ann", "bob": "https://avatars.test/bob",
			"cat": "https://avatars.test/cat", "me": "https://avatars.test/me"},
	}
}

// fakeAvatar is the picture of url as GitHub's identicons are: a grid of
// five by five, mirrored, in a color of its own.
func fakeAvatar(url string) *ui.Bitmap {
	h := fnv.New32a()
	h.Write([]byte(url))
	n := h.Sum32()
	img := image.NewRGBA(image.Rect(0, 0, ghAvatarPx, ghAvatarPx))
	fg := color.RGBA{uint8(60 + n%160), uint8(60 + n>>8%160), uint8(60 + n>>16%160), 255}
	const cell, margin = 10, 7
	for y := range ghAvatarPx {
		for x := range ghAvatarPx {
			img.Set(x, y, color.RGBA{240, 240, 240, 255})
			cx, cy := (x-margin)/cell, (y-margin)/cell
			if x < margin || y < margin || cx > 4 || cy > 4 {
				continue
			}
			if cx > 2 {
				cx = 4 - cx
			}
			if n>>(cx*5+cy)&1 == 1 {
				img.Set(x, y, fg)
			}
		}
	}
	return ui.NewBitmap(img)
}

// resetGitHubAvatars forgets the pictures fetched, for a test of its own.
func resetGitHubAvatars() {
	ghAvatars.Lock()
	ghAvatars.m, ghAvatars.queue = map[string]*avatarRead{}, nil
	ghAvatars.Unlock()
}

// fakeGitHub has the harness's repository be on GitHub, as overview and
// err say gh tells of it, and restores gh once the test ends.
func fakeGitHub(t testing.TB, overview *git.GHOverview, err error) {
	t.Helper()
	remotes, fetch, issue, avatar := readGitHubRemotes, fetchGitHubOverview, fetchIssue, fetchGitHubAvatar
	resetGitHubAvatars()
	t.Cleanup(func() {
		readGitHubRemotes, fetchGitHubOverview, fetchIssue, fetchGitHubAvatar = remotes, fetch, issue, avatar
		resetGitHubAvatars()
	})
	fetchGitHubAvatar = fakeAvatar
	readGitHubRemotes = func(*git.Repo) ([]git.GitHubRemote, error) {
		return []git.GitHubRemote{{Name: "origin", Host: "github.com", Path: "o/r"}}, nil
	}
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return overview, err }
	fetchIssue = func(_ *git.Repo, n int) (*git.GHIssue, error) {
		return &git.GHIssue{Number: n, Title: "Crash on open", URL: "https://github.com/o/r/issues/3", State: "OPEN", Author: "bob",
			Body: "It **crashes** as the folder opens.", CanUpdate: true, Created: time.Now().Add(-26 * time.Hour),
			Labels: []git.GHLabel{{Name: "bug", Color: "d73a4a"}}, Assignees: []string{"me"},
			Comments: []git.GHComment{{Author: "ann", Body: "Seen it too."}}, AllComments: 1,
			Avatars: map[string]string{"bob": "https://avatars.test/bob", "ann": "https://avatars.test/ann", "me": "https://avatars.test/me"}}, nil
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
		{fmt.Errorf("%w: HTTP 404: Not Found", git.ErrNoGH), locales.GhFailed()},
		{fmt.Errorf("%w: unexpected end of JSON input", git.ErrNoGH), locales.GhBusy()},
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
	if _, ok := hs.tt.Find(locales.GhStale(locales.GhBusy())); !ok {
		t.Error("no word that reading it again failed")
	}
	if got := hs.a.repository().ghEvery(); got != ghPendingEvery {
		t.Errorf("GitHub not answering in time is asked again in %v, want %v", got, ghPendingEvery)
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
	// The shell holds its folder open on Windows until it is closed.
	t.Cleanup(hs.a.closeTerminals)
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
	if _, ok := hs.tt.Find(locales.GhStale(locales.GhBusy())); !ok {
		t.Fatal("no word that GitHub could not be read again")
	}
	if now, _ := hs.tt.Find("Speed up the tree"); now != at {
		t.Errorf("the list moved as the line came: %v, was %v", now, at)
	}
}

// TestGitHubHoverIsOneLayer: a row under the pointer, not chosen, shows
// the hover once, not the list's and its own over each other.
func TestGitHubHoverIsOneLayer(t *testing.T) {
	for _, dark := range []bool{true, false} {
		fakeGitHub(t, ghOverviewFixture(), nil)
		hs := newHarness(t, dark)
		hs.h.visible = true
		hs.gitRepo()
		hs.a.showGitHub()
		hs.settle()
		at, ok := hs.tt.Find("Someone else's")
		if !ok {
			t.Fatal("no row")
		}
		hs.tt.Move(at.X+4, at.Y+at.H/2)
		hs.settle()
		img := hs.tt.Image()
		scale := float32(img.Bounds().Dx()) / 1200
		r, g, b, _ := img.At(int((at.X-22)*scale), int((at.Y+at.H/2)*scale)).RGBA()
		p := hs.a.pal
		want := p.hover.Over(p.chrome)
		near := func(got uint32, want uint8) bool { d := int(got>>8) - int(want); return d >= -2 && d <= 2 }
		if !near(r, want.R) || !near(g, want.G) || !near(b, want.B) {
			t.Errorf("dark %v: the hover is %02x%02x%02x, want %02x%02x%02x", dark, r>>8, g>>8, b>>8, want.R, want.G, want.B)
		}
	}
}

// TestGitHubChoiceGlides: the choice's background glides to the row the
// keys move it to, from where it was, and settles there.
func TestGitHubChoiceGlides(t *testing.T) {
	o := ghOverviewFixture()
	o.PRs = append(o.PRs, git.GHItem{Number: 10, Title: "Also asked", Author: "dan", PR: true, ReviewAsked: true, Updated: time.Now()})
	hs := githubHarness(t, o, nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	from := gp.row
	hs.tt.Key(0, ui.KeyDown)
	step(hs.tt)
	if gp.row == from || gp.rows[gp.row].key != "review:10" {
		t.Fatalf("Down put the choice on %q", gp.rows[gp.row].key)
	}
	if gp.glide.y == 0 {
		t.Error("the choice shows at its new row at once")
	}
	hs.settle()
	if gp.glide.y != 0 {
		t.Errorf("the choice stays %v off its row", gp.glide.y)
	}
	// To another section, over its header, it does not glide but comes in
	// where it goes, from a little above, and settles there.
	hs.tt.Key(0, ui.KeyDown)
	step(hs.tt)
	if gp.glide.y != 0 {
		t.Errorf("the choice glides over a header, %v off its row", gp.glide.y)
	}
	step(hs.tt)
	if k, y := gp.ghHop(hs.tt.Now()); k <= 0 || y >= 0 {
		t.Errorf("the choice does not come in over a header: %v, %v", k, y)
	}
	hs.settle()
	if k, _ := gp.ghHop(hs.tt.Now()); k != 0 {
		t.Errorf("the choice is still %v from coming in", k)
	}
}

// TestGitHubEscapeClears: Escape in the query clears what is typed, and
// with nothing typed leaves it be.
func TestGitHubEscapeClears(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	hs.tt.Type("crash")
	hs.settle()
	if gp.query != "crash" {
		t.Fatalf("the query is %q", gp.query)
	}
	if _, ok := hs.tt.Find("Wider sidebar"); ok {
		t.Fatal("the query finds what it should not")
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if gp.query != "" {
		t.Errorf("the query is %q after Escape", gp.query)
	}
	if _, ok := hs.tt.Find("Wider sidebar"); !ok {
		t.Error("the rows are not back after Escape")
	}
	hs.tt.Type("w")
	hs.settle()
	if gp.query != "w" {
		t.Errorf("Escape took the keyboard from the query: %q", gp.query)
	}
}

// TestGitHubStateChips: a draft has its own icon and a quiet title; a
// review asked again shows its chip.
func TestGitHubStateChips(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	o := hs.a.repository().githubOverview()
	if ic, _ := ghItemIcon(hs.a.pal, &o.PRs[1]); ic != iconPullRequestDraft {
		t.Error("a draft has the icon of a pull request ready for review")
	}
	if ic, _ := ghItemIcon(hs.a.pal, &o.PRs[0]); ic != iconPullRequest {
		t.Error("a pull request ready for review has the icon of a draft")
	}
	for _, w := range []string{locales.PrDraft(), locales.GhReReview()} {
		if _, ok := hs.tt.Find(w); !ok {
			t.Errorf("no %q chip", w)
		}
	}
}

// TestGitHubChecksCrossfade: as a refresh changes the checks of a pull
// request, their icon fades from how they were; a refresh that changes
// nothing, or another repository's, fades none.
func TestGitHubChecksCrossfade(t *testing.T) {
	o := ghOverviewFixture()
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return o, nil }
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	next := ghOverviewFixture()
	next.PRs[1].Checks = git.ChecksPass
	o = next
	hs.a.repository().refreshGitHubNow()
	for range 30 {
		step(hs.tt)
		if _, ok := gp.checks[8]; ok {
			break
		}
	}
	w, ok := gp.checks[8]
	if !ok || w.from != git.ChecksPending {
		t.Fatalf("checks of #8 that passed did not fade from pending: %+v", gp.checks)
	}
	if _, ok := gp.checks[7]; ok {
		t.Error("checks that did not change fade")
	}
	same := ghOverviewFixture()
	same.PRs[1].Checks = git.ChecksPass
	o = same
	hs.settle()
	hs.a.repository().refreshGitHubNow()
	hs.settle()
	if w2 := gp.checks[8]; w2.at != w.at && hs.tt.Now().Sub(w2.at) < motionShow {
		t.Error("a refresh that changed nothing fades the checks again")
	}
}

// TestGitHubKeys: Space shows the chosen row's card and hides it, and
// Escape does too; ⌥Enter opens it on GitHub; the menu names the keys.
func TestGitHubKeys(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	hs.tt.Key(0, ui.KeyDown) // into the list from the query
	hs.settle()
	gp.focusList = true
	hs.settle()
	r := gp.rows[gp.row]
	if r.item == nil {
		t.Fatalf("the choice is on %q", r.key)
	}
	hs.tt.Key(0, ui.KeySpace)
	hs.settle()
	if gp.card != r.key {
		t.Fatalf("Space showed no card (%q)", gp.card)
	}
	if _, ok := hs.tt.Find(locales.GhCardUpdated()); !ok {
		t.Error("the card of Space does not show")
	}
	first := locales.GhWorkOnIssue()
	if r.item.PR {
		first = locales.GhCheckOut()
	}
	if _, ok := hs.tt.Find(first); !ok {
		t.Error("the chosen row shows no actions while the list has the keyboard")
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if gp.card != "" {
		t.Error("Escape left the card")
	}
	hs.tt.Key(0, ui.KeySpace)
	hs.settle()
	hs.tt.Key(0, ui.KeySpace)
	hs.settle()
	if gp.card != "" {
		t.Error("Space again left the card")
	}
	hs.tt.Key(0, ui.KeySpace)
	hs.settle()
	hs.tt.Key(0, ui.KeyDown)
	hs.settle()
	if gp.card != "" {
		t.Error("the card stays as the choice moves")
	}
	ctx := hs.a.repository()
	o := ctx.githubOverview()
	keys := func(m ui.Modifiers, k ui.Key) func(ui.Modifiers, ui.Key) bool {
		return func(mm ui.Modifiers, kk ui.Key) bool { return mm == m && kk == k }
	}
	if hs.a.githubKeys(ctx, o, &o.PRs[0], keys(ui.Alt, ui.KeyEnter)) == nil {
		t.Error("⌥Enter does not open a pull request on GitHub")
	}
	if hs.a.githubKeys(ctx, o, &o.PRs[0], keys(ui.Cmd, ui.KeyEnter)) == nil {
		t.Error("⌘Enter does not check a pull request out")
	}
	if hs.a.githubKeys(ctx, o, &o.Issues[0], keys(ui.Cmd, ui.KeyEnter)) == nil {
		t.Error("⌘Enter does not work on an issue")
	}
	if hs.a.githubKeys(ctx, o, &o.Issues[1], keys(ui.Alt, ui.KeyEnter)) != nil {
		t.Error("⌥Enter opens an issue with no link")
	}
}

// TestShotGitHub draws the GitHub view, light and dark, at twice the
// size: a row under the pointer, the choice gliding, the card Space shows,
// with its reviewers, and checks fading from pending to passed. SHOTS=<dir> go test -run TestShotGitHub.
func TestShotGitHub(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		name := "github-light"
		if dark {
			name = "github-dark"
		}
		o := ghOverviewFixture()
		o.PRs[0].HeadRef, o.PRs[0].BaseRef, o.PRs[0].Files, o.PRs[0].Added, o.PRs[0].Deleted = "ann/tree", "main", 4, 120, 30
		o.PRs[0].Assignees = []string{"ann", "bob"}
		o.PRs = append(o.PRs, git.GHItem{Number: 10, Title: "Also asked", Author: "dan", PR: true, ReviewAsked: true, Updated: time.Now(),
			Checks: git.ChecksFail, URL: "https://github.com/o/r/pull/10"})
		fakeGitHub(t, o, nil)
		fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return o, nil }
		hs := newHarness(t, dark)
		hs.tt.SetScale(2) // as a Retina display draws the pictures
		hs.h.visible = true
		hs.gitRepo()
		hs.a.showGitHub()
		hs.settle()
		at, _ := hs.tt.Find("Someone else's")
		hs.tt.Move(at.X+4, at.Y+at.H/2)
		hs.settle()
		hs.shot(name + "-hover")
		hs.tt.Move(900, 700)
		hs.tt.Key(0, ui.KeyDown)
		step(hs.tt)
		step(hs.tt)
		step(hs.tt)
		hs.shot(name + "-glide")
		hs.settle()
		hs.a.github.focusList = true
		hs.settle()
		hs.tt.Key(0, ui.KeySpace)
		hs.settle()
		hs.shot(name + "-card")
		hs.tt.Key(0, ui.KeyEscape)
		next := ghOverviewFixture()
		next.PRs[1].Checks = git.ChecksPass
		next.PRs = append(next.PRs, o.PRs[3])
		o = next
		hs.a.repository().refreshGitHubNow()
		for range 30 {
			step(hs.tt)
			if _, ok := hs.a.github.checks[8]; ok {
				break
			}
		}
		step(hs.tt)
		step(hs.tt)
		step(hs.tt)
		hs.shot(name + "-checks")
		hs.a.window.SidebarWidth = 200
		hs.settle()
		hs.shot(name + "-narrow")
	}
}

// TestGitHubAvatars: the rows show their authors' initials while their
// pictures come, which take their place, each fetched once, and nothing
// of the row moves as they do.
func TestGitHubAvatars(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	var mu sync.Mutex
	calls := map[string]int{}
	release := make(chan struct{})
	fetchGitHubAvatar = func(url string) *ui.Bitmap {
		mu.Lock()
		calls[url]++
		mu.Unlock()
		<-release
		return fakeAvatar(url)
	}
	// ann, bob and cat author the rows; the viewer's own name none.
	hs.held = 3
	hs.a.showGitHub()
	hs.settle()
	before, ok := hs.tt.Find("Someone else's")
	if !ok {
		t.Fatal("no row")
	}
	read := func(login string) *avatarRead {
		ghAvatars.Lock()
		defer ghAvatars.Unlock()
		return ghAvatars.m["https://avatars.test/"+login]
	}
	for _, who := range []string{"ann", "bob", "cat"} {
		if rd := read(who); rd == nil || !rd.loading || rd.bm != nil {
			t.Errorf("%s's picture is not on its way: %+v", who, rd)
		}
	}
	if rd := read("me"); rd != nil {
		t.Error("the picture of the viewer, whose rows name no author, is fetched")
	}
	close(release)
	hs.held = 0
	hs.settle()
	hs.settle()
	for _, who := range []string{"ann", "bob", "cat"} {
		if rd := read(who); rd == nil || rd.bm == nil {
			t.Errorf("%s's picture did not come", who)
		}
	}
	after, _ := hs.tt.Find("Someone else's")
	if after != before {
		t.Errorf("the row moved as its picture came: %+v, then %+v", before, after)
	}
	hs.a.repository().refreshGitHubNow()
	hs.settle()
	mu.Lock()
	defer mu.Unlock()
	for url, n := range calls {
		if n != 1 {
			t.Errorf("%s fetched %d times", url, n)
		}
	}
}

// TestGitHubCardReviewers: a pull request's card names each reviewer, by
// how their latest review went.
func TestGitHubCardReviewers(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	hs.a.showGitHubCard("review:7")
	hs.settle()
	for _, s := range []string{locales.GhCardReviewers(), "bob", locales.PrApproved(), "cat", locales.GhReviewCommented(), locales.GhReReview()} {
		if _, ok := hs.tt.Find(s); !ok {
			t.Errorf("the card does not say %q", s)
		}
	}
}

// TestGitHubChoiceSkipsNotes: the keys move the choice past a note that
// only says a section has nothing.
func TestGitHubChoiceSkipsNotes(t *testing.T) {
	o := ghOverviewFixture()
	o.Issues, o.OpenIssues = nil, 0
	hs := githubHarness(t, o, nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	at := func(key string) int {
		for i, r := range gp.rows {
			if r.key == key {
				return i
			}
		}
		t.Fatalf("no row %q", key)
		return -1
	}
	at("issues:none")
	gp.row, gp.focusList = at("mine:8"), true
	hs.settle()
	hs.tt.Key(0, ui.KeyDown)
	hs.settle()
	if k := gp.rows[gp.row].key; k != "prs:9" {
		t.Errorf("Down went to %q, not past the note to prs:9", k)
	}
	hs.tt.Key(0, ui.KeyUp)
	hs.settle()
	if k := gp.rows[gp.row].key; k != "mine:8" {
		t.Errorf("Up went to %q, not back past the note to mine:8", k)
	}
}

// TestGitHubChecksKeepRoom: a pull request whose checks come moves not
// its title, when others listed have checks.
func TestGitHubChecksKeepRoom(t *testing.T) {
	o := ghOverviewFixture()
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return o, nil }
	hs.a.showGitHub()
	hs.settle()
	before, ok := hs.tt.Find("Someone else's")
	if !ok {
		t.Fatal("no row")
	}
	next := ghOverviewFixture()
	next.PRs[2].Checks = git.ChecksPending
	o = next
	hs.a.repository().refreshGitHubNow()
	hs.settle()
	if hs.a.repository().githubOverview() != next {
		t.Fatal("the refresh did not come")
	}
	if after, _ := hs.tt.Find("Someone else's"); after != before {
		t.Errorf("the title moved as its checks came: %+v, then %+v", before, after)
	}
}

// TestGitHubRowReviewers: a pull request's row shows the pictures of its
// reviewers but the viewer, three at most and how many more, and says when
// the viewer's review is asked again; neither moves its title.
func TestGitHubRowReviewers(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	var seven, nine ghRow
	for _, r := range gp.rows {
		switch r.key {
		case "review:7":
			seven = r
		case "prs:9":
			nine = r
		}
	}
	if !seven.again || len(seven.reviewers) != 2 || seven.reviewedBy != "bob, cat" {
		t.Errorf("#7's reviewers = %+v, again %v; want bob and cat, asked again", seven.reviewers, seven.again)
	}
	if nine.again || len(nine.reviewers) != ghRowReviewers || nine.moreReviewers != 1 {
		t.Errorf("#9's reviewers = %+v and %d more; want 3 and 1", nine.reviewers, nine.moreReviewers)
	}
	for _, s := range []string{locales.GhReReview(), locales.GhReviewersLabel("ann, cat, dan, eve"), "+1"} {
		if _, ok := hs.tt.Find(s); !ok {
			t.Errorf("the list does not show %q", s)
		}
	}
	title, _ := hs.tt.Find("Speed up the tree")
	plain := ghOverviewFixture()
	plain.PRs[0].Reviewers = nil
	hs2 := githubHarness(t, plain, nil)
	hs2.a.showGitHub()
	hs2.settle()
	if was, _ := hs2.tt.Find("Speed up the tree"); was != title {
		t.Errorf("the title moved with the reviewers: %+v, without %+v", title, was)
	}
}

// TestGitHubPendingChecksTurn: the ring of checks that run asks for
// frames while it shows; with none running, or with less motion, the
// view asks for none.
func TestGitHubPendingChecksTurn(t *testing.T) {
	for _, c := range []struct {
		name    string
		pending bool
		reduce  bool
		turns   bool
	}{{"running", true, false, true}, {"less motion", true, true, false}, {"none running", false, false, false}} {
		t.Run(c.name, func(t *testing.T) {
			o := ghOverviewFixture()
			if !c.pending {
				o.PRs[1].Checks = git.ChecksPass
			}
			hs := githubHarness(t, o, nil)
			hs.tt.SetPreferences(ui.Preferences{ReduceMotion: c.reduce})
			hs.a.showGitHub()
			hs.settle()
			hs.tt.Move(1, 1) // no row's hover or card
			hs.settle()
			if turns := !quiet(hs); turns != c.turns {
				t.Errorf("asks for frames: %v, want %v", turns, c.turns)
			}
		})
	}
}

// TestGitHubAvatarsKept: past ghAvatarsKept pictures, the one asked for
// least lately goes, not one on its way.
func TestGitHubAvatarsKept(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	release := make(chan struct{})
	fetchGitHubAvatar = func(url string) *ui.Bitmap { <-release; return fakeAvatar(url) }
	ghAvatars.Lock()
	for i := range ghAvatarsKept {
		ghAvatars.m[fmt.Sprintf("https://avatars.test/%d", i)] = &avatarRead{bm: fakeAvatar("x"), used: uint64(i + 1)}
	}
	ghAvatars.m["https://avatars.test/0"].loading = true
	ghAvatars.tick = ghAvatarsKept
	ghAvatars.Unlock()
	hs.a.ghAvatarBitmap("https://avatars.test/new", hs.tt.Now())
	ghAvatars.Lock()
	n := len(ghAvatars.m)
	_, first := ghAvatars.m["https://avatars.test/0"]
	_, second := ghAvatars.m["https://avatars.test/1"]
	_, fresh := ghAvatars.m["https://avatars.test/new"]
	ghAvatars.Unlock()
	close(release)
	if n != ghAvatarsKept || !first || second || !fresh {
		t.Errorf("kept %d (on its way kept %v, least lately kept %v, new kept %v)", n, first, second, fresh)
	}
}

// TestGitHubRefreshSaysSo: a refresh the view asks for on its own shows
// nothing; Refresh is answering at once, and shows the line once it took
// readingShows; GitHub's answer ends both.
func TestGitHubRefreshSaysSo(t *testing.T) {
	o := ghOverviewFixture()
	hs := githubHarness(t, o, nil)
	hs.a.showGitHub()
	hs.settle()
	release := make(chan struct{})
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { <-release; return o, nil }
	ctx := hs.a.repository()
	g := &ctx.git.gh
	g.poll.Expire()
	for range 30 {
		step(hs.tt)
	}
	if !g.loading {
		t.Fatal("GitHub was not asked again")
	}
	if _, ok := hs.tt.Find(locales.GhReading()); ok || g.refreshing() {
		t.Error("a refresh the view asked for on its own says so")
	}
	release <- struct{}{}
	hs.settle()

	ctx.refreshGitHubNow()
	step(hs.tt)
	if !g.refreshing() {
		t.Error("Refresh is not answering within a frame")
	}
	if _, ok := hs.tt.Find(locales.GhReading()); ok {
		t.Error("the line shows before readingShows, where it would flicker")
	}
	for range 30 {
		step(hs.tt)
	}
	if _, ok := hs.tt.Find(locales.GhReading()); !ok {
		t.Error("no line while a refresh asked for runs long")
	}
	close(release)
	hs.settle()
	if _, ok := hs.tt.Find(locales.GhReading()); ok || g.refreshing() {
		t.Error("the refresh still shows once GitHub answered")
	}
}

// TestGitHubSameAnswerBuildsNothing: an answer that tells what the last
// did keeps it, and the rows built of it; one that tells otherwise does not.
func TestGitHubSameAnswerBuildsNothing(t *testing.T) {
	o := ghOverviewFixture()
	answer := o
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { c := *answer; return &c, nil }
	hs.a.showGitHub()
	hs.settle()
	ctx := hs.a.repository()
	gp := &hs.a.github
	was, rows := ctx.githubOverview(), &gp.rows[0]
	ctx.refreshGitHubNow()
	hs.settle()
	if ctx.githubOverview() != was {
		t.Error("an answer that tells the same took the place of the last")
	}
	if &gp.rows[0] != rows {
		t.Error("the rows were built again of an answer that tells the same")
	}
	next := *o
	next.PRs = slices.Clone(o.PRs)
	next.PRs[0].Title = "Speed up the tree more"
	answer = &next
	ctx.refreshGitHubNow()
	hs.settle()
	if _, ok := hs.tt.Find("Speed up the tree more"); !ok {
		t.Error("an answer that tells otherwise is not shown")
	}
}

// TestGitHubQueryWhileReading: as the view reads its rows the first time,
// its query shows, and what is typed in it finds the rows once they come.
func TestGitHubQueryWhileReading(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	release := make(chan struct{})
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { <-release; return ghOverviewFixture(), nil }
	g := &hs.a.repository().git.gh
	g.overview, g.overviewOf = nil, nil
	hs.a.showGitHub()
	for range 10 {
		step(hs.tt)
	}
	if _, ok := hs.tt.Find(locales.GhFind()); !ok {
		t.Fatal("no query while the rows are read")
	}
	hs.tt.Type("crash")
	step(hs.tt)
	close(release)
	hs.settle()
	if _, ok := hs.tt.Find("Crash on open"); !ok {
		t.Error("the row the query typed early finds is not shown")
	}
	if _, ok := hs.tt.Find("Wider sidebar"); ok {
		t.Error("a row the query typed early does not find is shown")
	}
}

// TestGitHubRowsGo: a row a refresh takes, as a pull request merged,
// closes its room as it fades out, and is left out once gone; the choice
// stays on its own row, though a row above it went.
func TestGitHubRowsGo(t *testing.T) {
	o := ghOverviewFixture()
	answer := o
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return answer, nil }
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	for i, r := range gp.rows {
		if r.key == "issues:4" {
			gp.row = i
		}
	}
	hs.settle()
	next := *o
	next.PRs = slices.Clone(o.PRs[1:])
	answer = &next
	hs.a.repository().refreshGitHubNow()
	for range 40 {
		step(hs.tt)
		if gp.goneEnd.IsZero() {
			continue
		}
		break
	}
	going := false
	for _, r := range gp.rows {
		if r.key == "review:7" {
			going = !r.gone.IsZero()
		}
	}
	if !going {
		t.Fatal("the row taken is gone at once")
	}
	if k := gp.rows[gp.row].key; k != "issues:4" {
		t.Errorf("the choice is on %q as a row above it goes, want issues:4", k)
	}
	hs.settle()
	for _, r := range gp.rows {
		if r.key == "review:7" || r.key == "head:review" {
			t.Errorf("%q is still listed once gone", r.key)
		}
	}
	if _, ok := hs.tt.Find("Speed up the tree"); ok {
		t.Error("the row taken still shows")
	}
	if k := gp.rows[gp.row].key; k != "issues:4" {
		t.Errorf("the choice is on %q once the row above it is gone, want issues:4", k)
	}
}

// TestGitHubSectionFoldCloses: the rows of a section folded close their
// room, not vanish, and are left out once gone; the choice stops at none.
func TestGitHubSectionFoldCloses(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	gp.collapsed["issues"] = true
	step(hs.tt)
	var going *ghRow
	for i, r := range gp.rows {
		if r.key == "issues:4" {
			going = &gp.rows[i]
		}
	}
	if going == nil || going.gone.IsZero() {
		t.Fatal("the row of a section folded is gone at once")
	}
	if ghChoosable(*going) {
		t.Error("the choice may stop at a row that goes")
	}
	hs.settle()
	if _, ok := hs.tt.Find("Wider sidebar"); ok {
		t.Error("the row of a section folded still shows")
	}
}

// TestGitHubNoMatchClears: a query that finds nothing says so, and Clear
// Search brings every row back with the keyboard in the query.
func TestGitHubNoMatchClears(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.showGitHub()
	hs.settle()
	hs.a.github.query = "zzzz"
	hs.settle()
	if err := hs.tt.Click(locales.GhClearSearch()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if q := hs.a.github.query; q != "" {
		t.Errorf("the query is %q after Clear Search", q)
	}
	if _, ok := hs.tt.Find("Wider sidebar"); !ok {
		t.Error("the rows are not back after Clear Search")
	}
}

// TestGitHubRetryAnswersAtOnce: Try Again on a card turns busy within a
// frame of the click, and is itself again once GitHub answered.
func TestGitHubRetryAnswersAtOnce(t *testing.T) {
	hs := githubHarness(t, nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH))
	hs.a.showGitHub()
	hs.settle()
	release := make(chan struct{})
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) {
		<-release
		return nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH)
	}
	at, ok := hs.tt.Find(locales.GhRetry())
	if !ok {
		t.Fatal("no Try Again")
	}
	shot := func() []byte {
		img := hs.tt.Image()
		scale := float32(img.Bounds().Dx()) / 1200
		var px []byte
		for x := at.X - 60; x < at.X+at.W+60; x += 2 {
			for y := at.Y; y < at.Y+at.H; y += 2 {
				r, g, b, _ := img.At(int(x*scale), int(y*scale)).RGBA()
				px = append(px, byte(r>>8), byte(g>>8), byte(b>>8))
			}
		}
		return px
	}
	before := shot()
	if err := hs.tt.Click(locales.GhRetry()); err != nil {
		t.Fatal(err)
	}
	step(hs.tt)
	g := &hs.a.repository().git.gh
	if !g.refreshing() {
		t.Fatal("Try Again asked nothing")
	}
	if slices.Equal(shot(), before) {
		t.Error("Try Again shows nothing within a frame of the click")
	}
	// Busy, it asks nothing more.
	gen := g.gen
	hs.tt.Click(locales.GhRetry())
	step(hs.tt)
	if g.gen != gen {
		t.Error("Try Again asked again while busy")
	}
	close(release)
	hs.settle()
	if g.refreshing() {
		t.Error("Try Again is still busy once GitHub answered")
	}
}

// TestGitHubHeaderGapGlides: as the first section goes, the header under
// it gives up its room above as that closes, not at once; with less
// motion, at once.
func TestGitHubHeaderGapGlides(t *testing.T) {
	o := ghOverviewFixture()
	o.PRs[1].Checks = git.ChecksPass // no ring that turns
	answer := o
	hs := githubHarness(t, o, nil)
	fetchGitHubOverview = func(*git.Repo) (*git.GHOverview, error) { return answer, nil }
	hs.a.showGitHub()
	hs.settle()
	gp := &hs.a.github
	if gp.firstKey != "head:review" {
		t.Fatalf("the first header is %q, want head:review", gp.firstKey)
	}
	next := *o
	next.PRs = slices.Clone(o.PRs[1:])
	answer = &next
	hs.a.repository().refreshGitHubNow()
	for range 40 {
		step(hs.tt)
		if gp.firstKey != "head:review" {
			break
		}
	}
	if gp.firstKey != "head:mine" || gp.gapKey != "head:mine" || !gp.gapGone {
		t.Fatalf("first %q, gliding %q (gone %v), want head:mine giving up its room", gp.firstKey, gp.gapKey, gp.gapGone)
	}
	var mine ghRow
	for _, r := range gp.rows {
		if r.key == "head:mine" {
			mine = r
		}
	}
	at := gp.gapAt
	if gap, moving := gp.headerGap(mine, at, false); gap != ghHeaderGap || !moving {
		t.Errorf("as the section above goes, the room above is %v (moving %v), want %v", gap, moving, ghHeaderGap)
	}
	if gap, _ := gp.headerGap(mine, at.Add(motionLeave/2), false); gap <= 0 || gap >= ghHeaderGap {
		t.Errorf("halfway, the room above is %v", gap)
	}
	if gap, moving := gp.headerGap(mine, at.Add(motionLeave), false); gap != 0 || moving {
		t.Errorf("once the section above is gone, the room above is %v (moving %v)", gap, moving)
	}
	if gap, moving := gp.headerGap(mine, at, true); gap != 0 || moving {
		t.Errorf("with less motion, the room above is %v (moving %v), want none at once", gap, moving)
	}
	hs.tt.Move(1, 1) // no row's hover or card
	hs.settle()
	hs.settle() // the history marks the heads of the pull requests again
	if !quiet(hs) {
		t.Error("the view asks for frames once the room settled")
	}
}

// TestGitHubFailedAgainShakes: Try Again that fails again shakes the
// card's icon, as the words may say what they said; the first failure,
// or one the view asked for on its own, does not.
func TestGitHubFailedAgainShakes(t *testing.T) {
	hs := githubHarness(t, nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH))
	hs.a.showGitHub()
	hs.settle()
	g := &hs.a.repository().git.gh
	if !g.failedAgain.IsZero() {
		t.Fatal("the first failure shakes")
	}
	g.poll.Expire()
	hs.settle()
	if !g.failedAgain.IsZero() {
		t.Fatal("a failure the view asked again on its own shakes")
	}
	if err := hs.tt.Click(locales.GhRetry()); err != nil {
		t.Fatal(err)
	}
	for range 40 {
		step(hs.tt)
		if !g.refreshing() {
			break
		}
	}
	if g.failedAgain.IsZero() {
		t.Fatal("Try Again that failed again shows nothing")
	}
	if quiet(hs) {
		t.Error("the icon does not move as Try Again failed again")
	}
	hs.settle()
	if !quiet(hs) {
		t.Error("the icon still moves once it shook")
	}
}

// TestGitHubNarrowKeepsChip: in a narrow sidebar, the line under a pull
// request's title keeps its number and its Re-review chip in the row, and
// gives up its reviewers' pictures and the author's name first; wide, it
// shows them all.
func TestGitHubNarrowKeepsChip(t *testing.T) {
	hs := githubHarness(t, ghOverviewFixture(), nil)
	hs.a.window.SidebarWidth = 320
	hs.a.showGitHub()
	hs.settle()
	reviewers := locales.GhReviewersLabel("bob, cat")
	if _, ok := hs.tt.Find(reviewers); !ok {
		t.Fatal("a wide sidebar shows no reviewers")
	}
	hs.a.window.SidebarWidth = 190
	hs.settle()
	chip, ok := hs.tt.Find(locales.GhReReview())
	if !ok {
		t.Fatal("the Re-review chip is gone in a narrow sidebar")
	}
	if right := chip.X + chip.W; right > hs.a.window.SidebarWidth {
		t.Errorf("the chip ends at %v, past the sidebar's %v", right, hs.a.window.SidebarWidth)
	}
	if _, ok := hs.tt.Find(reviewers); ok {
		t.Error("the reviewers' pictures squeeze the chip rather than go")
	}
}
