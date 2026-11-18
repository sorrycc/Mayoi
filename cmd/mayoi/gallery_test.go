package main

// The state gallery: each surface in each of its states, in every theme,
// light and dark, and every language, side by side on one page, to review
// a change of look across all of them at once and to catch what clips in
// one language or theme alone. Run
// GALLERY=<dir> go test -run TestGallery ./cmd/mayoi
// and open <dir>/index.html.

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
)

// galleryThemes are the themes of the gallery, "" for Mayoi's own.
var galleryThemes = []string{"", themeMatrix, themeClaude, themeQoder, themeGrokBot, themeCursor,
	themePurpleGold, themePumpkin, themeWitchsBrew, themeBloodMoon, themeGTAVI}

// galleryLocales are the languages Mayoi has a catalog of.
var galleryLocales = []string{"en", "ja", "zh-Hans"}

// galleryStates are the states shown, each brought about on a window just
// opened on the test folder.
var galleryStates = []struct {
	name string
	set  func(hs *harness)
}{
	{"empty", func(hs *harness) {}},
	{"editor", func(hs *harness) { hs.a.openFile(hs.path("main.go"), true) }},
	{"file-loading", func(hs *harness) {
		hs.a.openFile(hs.path("main.go"), true)
		hs.settle()
		ed := hs.a.activeEditor()
		ed.sync, ed.readingAt = syncLoading, hs.tt.Now().Add(-readingShows)
	}},
	{"file-error", func(hs *harness) { hs.a.openFile(hs.path("missing.go"), true) }},
	{"heading-crumbs", func(hs *harness) {
		os.WriteFile(hs.path("notes.md"), []byte(headingsDoc), 0o644)
		hs.a.openFile(hs.path("notes.md"), true)
		hs.settle()
		d := hs.a.activeEditor()
		hs.a.jumpToScope(d, headingLineOf(headingsDoc, "### Deep"))
		hs.settle()
		hs.a.openHeadingCrumbs(hs.a.column(), hs.a.activeTab(), 1)
	}},
	{"heading-crumbs-filtered", func(hs *harness) {
		os.WriteFile(hs.path("notes.md"), []byte(headingsDoc), 0o644)
		hs.a.openFile(hs.path("notes.md"), true)
		hs.settle()
		d := hs.a.activeEditor()
		hs.a.jumpToScope(d, headingLineOf(headingsDoc, "### Deep"))
		hs.settle()
		hs.a.openHeadingCrumbs(hs.a.column(), hs.a.activeTab(), 2)
		hs.a.crumbs.query = "othr"
	}},
	{"jsonl-table", func(hs *harness) {
		os.WriteFile(hs.path("rows.jsonl"), []byte(testJSONL), 0o644)
		hs.a.openFile(hs.path("rows.jsonl"), true)
	}},
	{"jsonl-empty", func(hs *harness) {
		os.WriteFile(hs.path("empty.jsonl"), []byte("\n"), 0o644)
		hs.a.openFile(hs.path("empty.jsonl"), true)
	}},
	{"jsonl-no-match", func(hs *harness) {
		os.WriteFile(hs.path("rows.jsonl"), []byte(testJSONL), 0o644)
		hs.a.openFile(hs.path("rows.jsonl"), true)
		hs.settle()
		hs.a.activeEditor().preview.jsonl.query = "zzzz"
	}},
	{"jsonl-reading", func(hs *harness) {
		os.WriteFile(hs.path("big.jsonl"), []byte(jsonlText(20_000)), 0o644)
		hs.a.openFile(hs.path("big.jsonl"), true)
		hs.settle()
		j := hs.a.activeEditor().preview.jsonl
		j.ix.partial = true
	}},
	{"palette", func(hs *harness) { hs.a.run("go.commands") }},
	{"no-match", func(hs *harness) { hs.a.openQuick("zzzz-nothing") }},
	{"path-pending", func(hs *harness) {
		hs.a.openQuick(hs.path("pending.go"))
		p := &hs.a.quick.probe
		p.key = hs.a.quickPathKey()
		p.name, p.pending = hs.a.quick.query, true
		p.read.Invalidate()
		p.read.Start(p.key)
	}},
	{"path-missing", func(hs *harness) { hs.a.openQuick(hs.path("missing.go")) }},
	{"path-error", func(hs *harness) {
		hs.a.quick.probe.stat = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
		hs.a.openQuick(hs.path("denied.go"))
	}},
	{"github", func(hs *harness) { galleryGitHub(hs, ghOverviewFixture(), nil) }},
	{"github-loading", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		g := hs.a.repository().git
		g.gh.overview, g.gh.loading = nil, true
	}},
	{"github-avatars-loading", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		// The pictures on their way: the rows show their authors' initials.
		ghAvatars.Lock()
		for _, url := range ghOverviewFixture().Avatars {
			ghAvatars.m[url] = &avatarRead{loading: true}
		}
		ghAvatars.Unlock()
	}},
	{"github-card", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		hs.a.showGitHubCard("review:7")
	}},
	{"github-no-match", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.a.github.query = "zzzz"
	}},
	{"github-empty", func(hs *harness) {
		galleryGitHub(hs, &git.GHOverview{Viewer: "me", Repo: "o/r", URL: "https://github.com/o/r", IssuesOn: true}, nil)
	}},
	{"github-signed-out", func(hs *harness) {
		galleryGitHub(hs, nil, fmt.Errorf("%w: To get started with GitHub CLI, please run:  gh auth login", git.ErrNoGH))
	}},
	{"github-missing", func(hs *harness) {
		galleryGitHub(hs, nil, fmt.Errorf("%w: exec: \"gh\": executable file not found in $PATH", git.ErrNoGH))
	}},
	{"github-no-default", func(hs *harness) { galleryGitHub(hs, nil, git.ErrNoDefaultRepo) }},
	{"github-error", func(hs *harness) { galleryGitHub(hs, nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH)) }},
	{"github-stale", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		g := hs.a.repository().git
		g.gh.problem, g.gh.err = git.GHOther, "HTTP 502: Bad Gateway"
	}},
	{"issue", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		hs.a.openIssue(hs.a.repository(), 3, true)
	}},
	{"issue-failed", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		fetchIssue = func(*git.Repo, int) (*git.GHIssue, error) {
			return nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH)
		}
		hs.a.openIssue(hs.a.repository(), 3, true)
	}},
	{"pr", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		fakePR(hs.t)
		hs.settle()
		prTabOf(hs, 5, prConversation)
	}},
	{"pr-checks", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		fakePR(hs.t)
		hs.settle()
		prTabOf(hs, 5, prChecks)
	}},
	{"pr-changes", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		fakePR(hs.t)
		os.WriteFile(hs.path("a.txt"), []byte("one\n"), 0o644)
		run := hs.gitRepo()
		base := strings.TrimSpace(run("rev-parse", "HEAD"))
		os.WriteFile(hs.path("a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
		run("commit", "-qam", "more")
		head := strings.TrimSpace(run("rev-parse", "HEAD"))
		hs.settle()
		info := &git.PRInfo{Number: 5, ID: "PR_x", Title: "Fix things", State: "OPEN", BaseRef: "main", HeadRef: "feature", BaseOid: base, HeadOid: head}
		hs.a.showPullRequest(info, base, nil, hs.a.git.repo.Root)
		v := hs.a.column().tab().diff
		hs.until("the threads", func() bool { return v.commit.pr.threadsOK })
		hs.settleDiff(v)
	}},
	{"pr-reading", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		// As gh is asked, before it answers.
		pr := prTabOf(hs, 5, prConversation).commit.pr
		pr.viewing, pr.viewAsked = true, time.Now()
	}},
	{"pr-failed", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		fakePR(hs.t)
		fetchPRView = func(*git.Repo, int) (*git.PRView, error) {
			return nil, fmt.Errorf("%w: HTTP 502: Bad Gateway", git.ErrNoGH)
		}
		hs.settle()
		prTabOf(hs, 5, prConversation)
	}},
	{"issue-new", func(hs *harness) {
		galleryGitHub(hs, ghOverviewFixture(), nil)
		hs.settle()
		hs.a.newIssue(hs.a.repository())
	}},
	{"sessions", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
	}},
	{"sessions-hover", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
		hs.settle()
		// The pointer over the second row, which shows its buttons.
		hs.tt.Move(120, 222)
	}},
	{"sessions-find", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
		hs.settle()
		// The query marked in the rows, and the keyboard in the list, which
		// shows the chosen row's buttons.
		hs.a.sessions.query = "fix else"
		hs.settle()
		hs.key(0, ui.KeyTab)
		hs.key(0, ui.KeyTab)
	}},
	{"sessions-why", func(hs *harness) {
		home := agentHome(hs.t, hs.root)
		// A titled session, which the query finds in its first prompt.
		p := filepath.Join(home, ".claude", "projects", "p-titled", "titled.jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(`{"type":"user","message":{"content":"the sign in page of the account settings shows a blank form"},"cwd":"`+
			filepath.ToSlash(hs.root)+`"}`+"\n"+`{"type":"custom-title","customTitle":"Login bug"}`+"\n"), 0o644)
		hs.a.showSessions()
		hs.settle()
		hs.a.sessions.query = "login blank"
	}},
	{"sessions-agents", func(hs *harness) {
		// A session of each agent, each row marked with its agent's icon.
		home := agentHome(hs.t, hs.root)
		cwd := filepath.ToSlash(hs.root)
		files := map[string]string{
			".qoder/projects/p/q1.jsonl": `{"type":"user","message":{"content":"add dark mode"},"cwd":"` + cwd + `"}` + "\n",
			".codex/sessions/2026/rollout-x1.jsonl": `{"type":"session_meta","payload":{"id":"x1","cwd":"` + cwd + `"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"user","content":"speed up the build"}}` + "\n",
			".grok/sessions/p/g1/summary.json": `{"info":{"id":"g1","cwd":"` + cwd + `"},"generated_title":"Write the release notes"}`,
		}
		for name, text := range files {
			p := filepath.Join(home, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(text), 0o644)
		}
		hs.a.showSessions()
	}},
	{"live-preview", func(hs *harness) {
		// A note as Live Preview: its marks hidden but where the caret is,
		// its tasks, quote, code and table drawn.
		src := "# Notes\n\nSome **bold** and `code`, a [link](https://example.com).\n\n- [ ] open\n- [x] done\n\n" +
			"> [!TIP]\n> A tip.\n\n```go\nfunc main() {}\n```\n\n| Key | Value |\n|-----|-------|\n| a   | 1     |\n"
		os.WriteFile(hs.path("notes.md"), []byte(src), 0o644)
		hs.a.settings.MarkdownMode = "live"
		hs.a.openFile(hs.path("notes.md"), true)
		d := hs.a.activeEditor()
		hs.until("the note parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
		at := strings.Index(src, "bold")
		hs.place(d, at, at)
	}},
	{"wiki-suggest", func(hs *harness) {
		// [[ typed in a note of a vault: the notes it may link to, under it.
		os.MkdirAll(hs.path(".obsidian"), 0o755)
		for _, name := range []string{"ideas.md", "projects/plan.md", "projects/roadmap.md", "daily/2026-11-14.md"} {
			os.MkdirAll(filepath.Dir(hs.path(name)), 0o755)
			os.WriteFile(hs.path(name), []byte("# "+name+"\n"), 0o644)
		}
		os.WriteFile(hs.path("notes.md"), []byte("# Notes\n\nSee \n"), 0o644)
		hs.t.Cleanup(func() { forgetWikiFiles(hs.root) })
		hs.a.settings.MarkdownMode = "live"
		hs.a.openFile(hs.path("notes.md"), true)
		d := hs.a.activeEditor()
		hs.until("the note parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
		hs.place(d, 13, 13)
		hs.insert("[")
		hs.insert("[")
		hs.insert("p")
		hs.until("the suggestions", func() bool { s := &hs.a.wikiSug; return s.open && !s.loading && len(s.items) > 0 })
	}},
	{"commit-checks", func(hs *harness) {
		pick := commitChecksBox(hs)
		pick.checks = []git.Check{
			{Name: "Unit", Workflow: "Test", Bucket: "fail", JobID: 1, Link: "https://github.com/o/r/actions/runs/1/job/1"},
			{Name: "Build", Workflow: "Test", Bucket: "pending"},
			{Name: "Lint", Workflow: "Test", Bucket: "pass"},
			{Name: "ci/other", Bucket: "pass", Description: "Fine"},
		}
		pick.poll.Start(pick.asked(), 0)
		pick.poll.Done(pick.asked())
		pick.poll.Keep(pick.asked(), time.Hour, func() {})
	}},
	{"commit-checks-loading", func(hs *harness) {
		pick := commitChecksBox(hs)
		pick.poll.Start(pick.asked(), 0)
	}},
	{"commit-checks-none", func(hs *harness) {
		pick := commitChecksBox(hs)
		pick.poll.Start(pick.asked(), 0)
		pick.poll.Done(pick.asked())
		pick.poll.Keep(pick.asked(), time.Hour, func() {})
	}},
	{"unsaved", func(hs *harness) {
		hs.a.openFile(hs.path("main.go"), true)
		hs.settle()
		hs.insert("// more\n")
	}},
}

// commitChecksBox makes the test folder a work tree, and shows the box of
// the checks of a commit of it, read of none yet.
func commitChecksBox(hs *harness) *checksPicker {
	if out, err := exec.Command("git", "init", "-q", hs.root).CombinedOutput(); err != nil {
		hs.t.Fatalf("git init: %v %s", err, out)
	}
	hs.a.git.looked = false
	hs.until("the work tree", func() bool { return hs.a.git.repo != nil })
	hs.a.checks = checksPicker{open: true, repoRoot: hs.a.git.repo.Root, hash: "0123456789abcdef0123456789abcdef01234567"}
	return &hs.a.checks
}

func TestGallery(t *testing.T) {
	dir := os.Getenv("GALLERY")
	if dir == "" {
		t.Skip("GALLERY is not set")
	}
	var page strings.Builder
	page.WriteString(`<!doctype html><meta charset="utf-8"><title>Mayoi state gallery</title>
<style>body{font:13px system-ui;margin:16px;background:#888}h2{margin:24px 0 8px}
.row{display:flex;gap:8px;overflow-x:auto}figure{margin:0}img{width:480px;display:block}figcaption{padding:2px 0}</style>`)
	for _, st := range galleryStates {
		fmt.Fprintf(&page, "<h2>%s</h2>\n", html.EscapeString(st.name))
		for _, loc := range galleryLocales {
			page.WriteString(`<div class="row">`)
			for _, theme := range galleryThemes {
				for _, dark := range []bool{false, true} {
					if !dark && alwaysDark(theme) {
						continue
					}
					name := galleryName(st.name, loc, theme, dark)
					t.Run(name, func(t *testing.T) {
						locale := i18n.Locale()
						i18n.SetLocaleFunc(func() string { return loc })
						t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return locale }) })
						hs := newHarness(t, dark)
						hs.tt.SetLocale(loc)
						if theme != "" {
							hs.a.setTheme(theme)
						}
						st.set(hs)
						hs.settle()
						os.Setenv("SHOTS", dir)
						defer os.Unsetenv("SHOTS")
						hs.shot(name)
					})
					fmt.Fprintf(&page, `<figure><img loading="lazy" src="%s.png"><figcaption>%s</figcaption></figure>`,
						name, html.EscapeString(name))
				}
			}
			page.WriteString("</div>\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func galleryName(state, loc, theme string, dark bool) string {
	if theme == "" {
		theme = "mayoi"
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	return strings.Join([]string{state, loc, theme, mode}, "-")
}
