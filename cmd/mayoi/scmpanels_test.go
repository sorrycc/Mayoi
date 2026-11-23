package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
	"mayoi/locales"
)

func settleScmPanels(t *testing.T, hs *harness) {
	settleRepositoryPanels(t, hs, hs.a.rootRepository())
}

func settleRepositoryPanels(t *testing.T, hs *harness, ctx *gitContext) {
	t.Helper()
	for range 40 {
		hs.settle()
		p := &ctx.scm.panels
		ready := p.loaded && !p.loading
		for _, l := range p.logs {
			if l.loading {
				ready = false
			}
		}
		if ready {
			return
		}
	}
	t.Fatal("Git panels did not settle")
}
func findPanelRow(t *testing.T, hs *harness, key string) scmRow {
	t.Helper()
	for _, r := range hs.a.scmRows() {
		if r.key == key {
			return r
		}
	}
	t.Fatalf("missing panel row %q", key)
	return scmRow{}
}

func TestScmBranchRemotePanels(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	run("branch", "topic")
	run("remote", "add", "origin", hs.root)
	run("fetch", "-q", "origin")
	run("branch", "--set-upstream-to=origin/main", "main")
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	if !hs.tt.HasText("REPOSITORY") || !hs.tt.HasText("BRANCHES") {
		t.Fatalf("panel headings missing: %q", hs.tt.Texts())
	}
	lastCommit, repository := -1, -1
	for i, row := range a.scmRows() {
		if row.key == "group:commits" || row.commit != nil && row.panel == nil && row.at >= 0 {
			lastCommit = i
		}
		if row.key == "panel:tabs" {
			repository = i
		}
	}
	if lastCommit < 0 || repository <= lastCommit {
		t.Fatalf("Repository must follow commit history: last commit=%d repository=%d", lastCommit, repository)
	}
	main := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	if !main.panel.branch.Current || main.panel.branch.Upstream != "origin/main" {
		t.Fatalf("branch metadata: %+v", main.panel.branch)
	}
	if main.panel.detail != "origin/main · now" {
		t.Fatalf("an even branch counts nothing: %q", main.panel.detail)
	}
	if err := hs.tt.RightClick("topic"); err != nil {
		t.Fatal(err)
	}
	if menu := hs.tt.Menu(); !slices.Contains(menu, "Rename Branch…") || !slices.Contains(menu, "Merge into Current Branch…") {
		t.Fatalf("branch menu: %q", menu)
	}
	hs.tt.Key(0, ui.KeyEscape)
	a.activateScmPanelRow(main)
	settleScmPanels(t, hs)
	key := main.key + ":history"
	log := a.scm.panels.logs[key]
	if log == nil || len(log.commits) != 1 {
		t.Fatalf("branch history: %+v", log)
	}
	a.activateScmPanelRow(main)
	hs.settle()
	a.activateScmPanelRow(main)
	hs.settle()
	if a.scm.panels.logs[key] != log {
		t.Fatal("collapse discarded cached history")
	}
	if err := hs.tt.Click("Remotes"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	remote := findPanelRow(t, hs, "panel:remote:origin")
	a.activateScmPanelRow(remote)
	settleScmPanels(t, hs)
	var urls []string
	for _, r := range a.scmRows() {
		if r.panel != nil && r.panel.kind == "url" {
			urls = append(urls, r.panel.detail+" "+r.panel.label)
		}
	}
	if len(urls) != 1 || urls[0] != "fetch, push "+hs.root {
		t.Fatalf("a remote that pushes where it fetches has one URL row: %q", urls)
	}
	remoteBranch := findPanelRow(t, hs, "panel:branch:refs/remotes/origin/main")
	a.activateScmPanelRow(remoteBranch)
	settleScmPanels(t, hs)
	if l := a.scm.panels.logs[remoteBranch.key+":history"]; l == nil || len(l.commits) != 1 {
		t.Fatalf("remote history: %+v", l)
	}
	for _, r := range a.scmRows() {
		if r.commit != nil && strings.HasPrefix(r.key, "panel:") && r.at != -1 {
			t.Fatal("panel commit uses HEAD graph index")
		}
	}
	if len(a.git.history) > 0 {
		a.scm.collapsed[groupCommits] = true
	}
	for _, tab := range scmPanelTabs {
		a.switchScmPanelTab(tab.id)
		for _, dark := range []bool{true, false} {
			hs.tt.SetDark(dark)
			a.window.SidebarWidth = 320
			hs.settle()
			name := "git-panels-expanded-light"
			if dark {
				name = "git-panels-expanded-dark"
			}
			hs.shot(name + "-" + tab.key)
		}
	}
	if a.scm.panels.logs[key] != log {
		t.Fatal("switching tabs discarded branch history")
	}
}

func TestScmPanelWorktreeBranches(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("branch", "topic")
	run("branch", "plain")
	path := filepath.Join(t.TempDir(), "linked tree")
	run("worktree", "add", path, "topic")
	run("update-ref", "refs/remotes/origin/topic", "HEAD")

	hs.a.settings.ScmRepositoryOpen = true
	hs.a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	topic := "panel:branch:refs/heads/topic"
	if got := findPanelRow(t, hs, topic).panel.branch.WorktreePath; filepath.Base(got) != filepath.Base(path) {
		t.Fatalf("linked worktree path: %q, want %q", got, path)
	}
	for _, ref := range []string{"refs/heads/main", "refs/heads/plain"} {
		if got := findPanelRow(t, hs, "panel:branch:"+ref).panel.branch.WorktreePath; got != "" {
			t.Fatalf("%s unexpectedly marked with worktree %q", ref, got)
		}
	}
	worktreePath := findPanelRow(t, hs, topic).panel.branch.WorktreePath
	if rect, ok := hs.tt.Find("topic"); ok {
		hs.tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
		hs.settle()
	} else {
		t.Fatal("worktree branch is not visible")
	}
	if err := hs.tt.Click(locales.PanelOpenWorktreeNew() + " topic"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if !slices.Equal(hs.h.windows, []string{worktreePath}) || hs.a.root != hs.root {
		t.Fatalf("opened windows %q, current folder %q", hs.h.windows, hs.a.root)
	}

	hs.a.switchScmPanelTab(scmRemotesTab)
	settleScmPanels(t, hs)
	for _, branch := range hs.a.scm.panels.branches {
		if branch.Remote && branch.WorktreePath != "" {
			t.Fatalf("remote branch marked with worktree: %+v", branch)
		}
	}

	run("worktree", "remove", path)
	hs.a.switchScmPanelTab(scmBranchesTab)
	hs.a.invalidateScmPanels()
	settleScmPanels(t, hs)
	if got := findPanelRow(t, hs, topic).panel.branch.WorktreePath; got != "" {
		t.Fatalf("removed worktree still shown: %q", got)
	}
}

func TestScmPanelTabsPreserveViewState(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	main := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	a.activateScmPanelRow(main)
	settleScmPanels(t, hs)
	log := a.scm.panels.logs[main.key+":history"]
	log.n = 2 * historyPage
	check := func(section string) {
		t.Helper()
		headers := 0
		for _, row := range a.scmRows() {
			if row.key == "panel:tabs" {
				headers++
			}
			if row.panel != nil && row.panel.kind == "section" && row.key != "panel:"+section {
				t.Fatalf("inactive section remains visible: %s", row.key)
			}
		}
		if headers != 1 {
			t.Fatalf("repository header count %d", headers)
		}
		findPanelRow(t, hs, "panel:"+section)
	}
	check("branches")
	if err := hs.tt.Click("Remotes"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	check("remotes")
	if err := hs.tt.Click("Branches"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	check("branches")
	if a.scm.panels.closed[main.key] || a.scm.panels.logs[main.key+":history"] != log || log.n != 2*historyPage {
		t.Fatal("switching tabs lost expanded branch or loaded pages")
	}
	if err := hs.tt.Click("REPOSITORY"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.ScmRepositoryOpen {
		t.Fatal("repository panel did not collapse")
	}
	if err := hs.tt.Click("Remotes"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	check("remotes")
	a.gitChanged()
	hs.settleGit()
	settleScmPanels(t, hs)
	if a.scm.panels.tab != scmRemotesTab {
		t.Fatal("refresh reset the active tab")
	}
	check("remotes")
}

func findRepositoryPanelRow(t *testing.T, ctx *gitContext, key string) scmRow {
	t.Helper()
	for _, row := range ctx.scmRows() {
		if row.key == key {
			return row
		}
	}
	t.Fatalf("missing panel row %q", key)
	return scmRow{}
}

func TestScmPanelHistoryRetryAndPagination(t *testing.T) {
	for _, module := range []bool{false, true} {
		name := "single"
		if module {
			name = "submodule"
		}
		t.Run(name, func(t *testing.T) {
			if !module {
				skipOnWindows(t, "the history of a deleted branch shows no error on Windows")
			}
			var hs *harness
			var ctx *gitContext
			var run func(...string) string
			if module {
				hs, ctx, _ = submoduleHarness(t)
				run = func(args ...string) string { return gitIn(t, ctx.git.repo.Root, args...) }
				hs.a.selectRepository(ctx.git.repo.Root)
			} else {
				hs = newHarness(t, true)
				run = hs.gitRepo()
				ctx = hs.a.rootRepository()
			}
			a := hs.a

			run("commit", "--allow-empty", "-qm", "second")
			run("commit", "--allow-empty", "-qm", "third")
			run("branch", "topic")
			a.settings.ScmRepositoryOpen = true
			a.run("view.scm")
			hs.a.gitChanged()
			settleRepositories(t, hs, len(hs.a.repositories.contexts))
			ctx.refreshScmPanels()
			settleRepositoryPanels(t, hs, ctx)
			branch := findRepositoryPanelRow(t, ctx, "panel:branch:refs/heads/topic")
			ctx.activateScmPanelRow(branch)
			settleRepositoryPanels(t, hs, ctx)
			key := branch.key + ":history"
			l := ctx.scm.panels.logs[key]
			l.n = 1
			l.loaded = false
			ctx.scm.panels.gen++
			settleRepositoryPanels(t, hs, ctx)
			more := findRepositoryPanelRow(t, ctx, key+":more")
			ctx.activateScmPanelRow(more)
			ctx.panelLog(key, "", "refs/heads/topic")
			ctx.activateScmPanelRow(more)
			settleRepositoryPanels(t, hs, ctx)
			if len(l.commits) != 3 {
				t.Fatalf("pagination returned %d commits", len(l.commits))
			}
			// Remove the real ref after metadata was read: the read must expose an
			// actionable error, then recover when that same ref is restored.
			// Keep the ref snapshot stale deliberately while exercising a failed read.
			// Otherwise a metadata poll can remove the deleted branch
			// before its error row is observed.
			ctx.git.checked = ctx.app.gitNow().Add(time.Hour)
			defer func() { ctx.git.checked = time.Time{} }()
			run("branch", "-D", "topic")
			l.loaded = false
			ctx.scm.panels.gen++
			settleRepositoryPanels(t, hs, ctx)
			retry := findRepositoryPanelRow(t, ctx, key+":error")
			if retry.panel.detail == "" {
				t.Fatal("missing Git error context")
			}
			run("branch", "topic")
			ctx.activateScmPanelRow(retry)
			settleRepositoryPanels(t, hs, ctx)
			if l.err != "" || len(l.commits) != 3 {
				t.Fatalf("retry did not recover: %+v", l)
			}
		})
	}
}

func TestScmEmptyRemotesAndRemotePrompt(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	a.switchScmPanelTab(scmRemotesTab)
	hs.settle()
	empty := findPanelRow(t, hs, "remotes:status")
	if empty.panel.kind != "addRemote" {
		t.Fatalf("empty action: %+v", empty.panel)
	}
	a.activateScmPanelRow(empty)
	if a.prompt == nil || a.prompt.title != "Add Remote…" {
		t.Fatal("missing remote name prompt")
	}
	if msg := a.prompt.done("origin"); msg != "" {
		t.Fatal(msg)
	}
	a.prompt = nil
	hs.settle()
	if a.prompt == nil || a.prompt.title != "Add Remote URL…" {
		t.Fatal("missing remote URL prompt")
	}
	if msg := a.prompt.done(hs.root); msg != "" {
		t.Fatal(msg)
	}
	a.prompt = nil
	hs.settleGit()
	settleScmPanels(t, hs)
	if len(a.scm.panels.remotes) != 1 || a.scm.panels.remotes[0].Name != "origin" {
		t.Fatalf("add remote: %+v", a.scm.panels.remotes)
	}
	hs.shot("git-panels")
}

func TestScmIncomingOutgoingSources(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	base := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("remote", "add", "origin", hs.root)
	incoming := strings.TrimSpace(run("commit-tree", "HEAD^{tree}", "-p", base, "-m", "Remote incoming"))
	run("update-ref", "refs/remotes/origin/main", incoming)
	run("branch", "--set-upstream-to=origin/main", "main")
	run("commit", "--allow-empty", "-qm", "Local outgoing")
	outgoing := strings.TrimSpace(run("rev-parse", "HEAD"))
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	branch := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	if branch.panel.branch.Ahead != 1 || branch.panel.branch.Behind != 1 {
		t.Fatalf("divergence: %+v", branch.panel.branch)
	}
	a.activateScmPanelRow(branch)
	settleScmPanels(t, hs)
	for kind, want := range map[string]string{"incoming": incoming, "outgoing": outgoing} {
		l := a.scm.panels.logs[branch.key+":"+kind]
		if l == nil || len(l.commits) != 1 || l.commits[0].Hash != want {
			t.Fatalf("%s history: %+v", kind, l)
		}
	}
}

func TestScmPanelDialogsStayWithRepository(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	run("branch", "topic")
	run("remote", "add", "origin", hs.root)
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	other := newHarness(t, true)
	other.gitRepo()
	a.switchScmPanelTab(scmRemotesTab)
	settleScmPanels(t, hs)
	remote := findPanelRow(t, hs, "panel:remote:origin")
	a.activateScmPanelRow(remote)
	settleScmPanels(t, hs)
	a.switchScmPanelTab(scmBranchesTab)
	settleScmPanels(t, hs)
	original := a.git.repo
	for _, open := range []func(){
		func() { a.askPanelRemote(nil, false) },
		func() { a.askPanelRemote(remote.panel.remote, false) },
		func() { a.askRenamePanelBranch(*findPanelRow(t, hs, "panel:branch:refs/heads/topic").panel.branch) },
		a.askPanelNewBranch,
	} {
		open()
		pr := a.prompt
		a.prompt = nil
		a.git.repo = other.a.git.repo
		if msg := pr.done("changed"); msg == "" {
			t.Fatal("stale prompt accepted repository switch")
		}
		if a.git.pending != 0 {
			t.Fatal("stale prompt started a Git operation")
		}
		a.git.repo = original
	}
	for _, tc := range []struct{ row, item string }{
		{"topic", "Delete Branch…"},
		{"origin", "Remove Remote…"},
		{"origin", "Prune Remote…"},
		{"origin", "Fetch and Prune…"},
	} {
		tab := scmRemotesTab
		if tc.row == "topic" {
			tab = scmBranchesTab
		}
		a.switchScmPanelTab(tab)
		hs.settle()
		if err := hs.tt.RightClick(tc.row); err != nil {
			t.Fatal(err)
		}
		if err := hs.tt.ChooseMenuItem(tc.item); err != nil {
			t.Fatalf("%s: %v", tc.item, err)
		}
		hs.settle()
		cf := a.confirm
		if cf == nil {
			t.Fatalf("%s did not request confirmation", tc.item)
		}
		if a.git.pending != 0 {
			t.Fatalf("%s started before confirmation", tc.item)
		}
		a.confirm = nil
		a.git.repo = other.a.git.repo
		cf.run()
		if a.git.pending != 0 {
			t.Fatalf("%s acted on another repository", tc.item)
		}
		a.git.repo = original
		hs.settle()
	}
}

// Disk notifications and Git metadata checks can invalidate the panel during
// resizing, terminal activity or idle polling. Its published rows must remain
// visible until their replacements are ready.
func TestScmPanelRefreshKeepsPublishedRows(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("remote", "add", "origin", hs.root)
	run("fetch", "origin")
	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	main := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	a.activateScmPanelRow(main)
	settleScmPanels(t, hs)
	key := main.key + ":history"
	a.scm.panels.logs[key].n = 2 * historyPage
	before := a.scmRows()
	keys := func(rows []scmRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.key
		}
		return out
	}
	for range 3 {
		a.diskChanged(nil, nil)
		a.refreshScmPanels()
		during := a.scmRows()
		if !slices.Equal(keys(before), keys(during)) {
			t.Fatalf("refresh replaced visible rows with loading placeholders:\nbefore %q\nduring %q", keys(before), keys(during))
		}
		if a.scm.panels.logs[key].n != 2*historyPage {
			t.Fatal("refresh discarded loaded page limit")
		}
		for frame := range 60 {
			a.window.SidebarWidth = 260 + float32(frame%20)*4
			step(hs.tt)
			if !slices.Equal(keys(before), keys(a.scmRows())) {
				t.Fatalf("background refresh changed visible rows in frame %d", frame)
			}
			time.Sleep(5 * time.Millisecond)
		}
		settleScmPanels(t, hs)
		if !slices.Equal(keys(before), keys(a.scmRows())) {
			t.Fatal("unchanged refresh altered published rows")
		}
	}
}

func TestScmPanelRefreshErrorKeepsSnapshotAndRepositoryChangeDropsIt(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	main := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	a.activateScmPanelRow(main)
	settleScmPanels(t, hs)
	key := main.key + ":history"
	hash := a.scm.panels.logs[key].commits[0].Hash
	dir, moved := filepath.Join(hs.root, ".git"), filepath.Join(t.TempDir(), "saved-git")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := os.Stat(moved); err == nil {
			_ = os.Rename(moved, dir)
		}
	}()
	a.invalidateScmPanels()
	a.refreshScmPanels()
	settleScmPanels(t, hs)
	if a.scm.panels.err == "" {
		t.Fatal("metadata failure was hidden")
	}
	findPanelRow(t, hs, main.key)
	rows := a.scmRows()
	if !slices.ContainsFunc(rows, func(r scmRow) bool { return r.key == key+":"+hash }) {
		t.Fatal("read failure discarded published commits")
	}
	if err := os.Rename(moved, dir); err != nil {
		t.Fatal(err)
	}
	a.invalidateScmPanels()
	a.refreshScmPanels()
	settleScmPanels(t, hs)
	if a.scm.panels.err != "" {
		t.Fatal("metadata retry failed")
	}
	// A new repository identity must not inherit the previous snapshot, even
	// when an old read still has a completion queued for the same folder.
	a.invalidateScmPanels()
	a.refreshScmPanels()
	fresh, err := git.Open(hs.root)
	if err != nil {
		t.Fatal(err)
	}
	a.git.repo = fresh
	a.refreshScmPanels()
	if p := &a.scm.panels; p.loaded || len(p.branches) > 0 || len(p.logs) > 0 {
		t.Fatal("new repository inherited published data")
	}
	settleScmPanels(t, hs)
	if a.scm.panels.repo != fresh {
		t.Fatal("old completion replaced the new repository")
	}
}

func TestScmPanelsLoadOnlyExpandedContent(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("remote", "add", "origin", hs.root)
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	p := &a.scm.panels
	if a.settings.ScmRepositoryOpen || p.repo != nil || p.loading || len(p.logs) != 0 {
		t.Fatal("collapsed repository started panel reads")
	}
	a.activateScmPanelRow(findPanelRow(t, hs, "panel:tabs"))
	settleScmPanels(t, hs)
	if !a.settings.ScmRepositoryOpen || !p.loaded || len(p.logs) != 0 || len(p.remotes) != 0 {
		t.Fatal("opening Branches read hidden remote details or history")
	}
	a.switchScmPanelTab(scmRemotesTab)
	settleScmPanels(t, hs)
	if len(p.remotes) != 1 || len(p.remotes[0].FetchURLs) != 0 || len(p.logs) != 0 {
		t.Fatalf("collapsed remote read URLs or history: %+v", p.remotes)
	}
	remote := findPanelRow(t, hs, "panel:remote:origin")
	a.activateScmPanelRow(remote)
	settleScmPanels(t, hs)
	if len(p.remotes[0].FetchURLs) != 1 || p.remotes[0].FetchURLs[0] != hs.root {
		t.Fatalf("expanded remote URLs: %+v", p.remotes)
	}
	a.editPanelRemote("origin", false)
	hs.settle()
	if a.prompt == nil || a.prompt.title != "Edit Fetch URL…" || a.prompt.value != hs.root {
		t.Fatalf("edit remote prompt: %+v", a.prompt)
	}
	a.prompt = nil
	a.activateScmPanelRow(findPanelRow(t, hs, "panel:tabs"))
	token := p.request
	a.invalidateScmPanels()
	a.refreshScmPanels()
	hs.settle()
	if a.settings.ScmRepositoryOpen || p.request != token {
		t.Fatal("collapsed repository refreshed metadata")
	}
}

func TestShortRemoteURL(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:me/app.git":           "github.com/me/app",
		"https://github.com/me/app":           "github.com/me/app",
		"https://user@gitlab.com/me/app.git/": "gitlab.com/me/app",
		"ssh://git@host:2222/srv/app.git":     "host:2222/srv/app",
		"/Users/me/src/app":                   "/Users/me/src/app",
		"../app.git":                          "../app",
	} {
		if got := shortRemoteURL(url); got != want {
			t.Errorf("shortRemoteURL(%q) = %q, want %q", url, got, want)
		}
	}
}

// Remotes read once stay listed while Branches reads again, so coming back
// to Remotes shows them rather than none until they are read again; before
// their first read, Remotes says it is loading.
func TestScmRemotesStayAcrossBranchReads(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("remote", "add", "origin", hs.root)
	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	p := &a.scm.panels
	a.switchScmPanelTab(scmRemotesTab)
	rows := a.scmRows()
	if r := rows[len(rows)-1]; r.panel == nil || r.panel.label != "Loading…" {
		t.Fatalf("remotes not yet read: %+v", r.panel)
	}
	settleScmPanels(t, hs)
	a.switchScmPanelTab(scmBranchesTab)
	settleScmPanels(t, hs)
	if len(p.remotes) != 1 {
		t.Fatalf("reading branches dropped the remotes: %+v", p.remotes)
	}
	a.switchScmPanelTab(scmRemotesTab)
	findPanelRow(t, hs, "panel:remote:origin")
}

func TestScmPullInactiveBranch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	upstreamDir := t.TempDir()
	// Create a bare remote by cloning the harness repo.
	hs := newHarness(t, true)
	run := hs.gitRepo()
	gitIn(t, hs.root, "clone", "--bare", hs.root, upstreamDir)
	run("remote", "add", "origin", upstreamDir)
	run("fetch", "-q", "origin")
	run("branch", "--set-upstream-to=origin/main", "main")
	run("switch", "-c", "dev")
	devHead := strings.TrimSpace(run("rev-parse", "HEAD"))

	// Advance origin/main in a temporary worktree.
	workDir := t.TempDir()
	gitIn(t, hs.root, "clone", upstreamDir, workDir)
	gitIn(t, workDir, "commit", "--allow-empty", "-qm", "remote 1")
	gitIn(t, workDir, "push", "-q", "origin", "main")

	// Fetch in local repo so origin/main is ahead of local main.
	run("fetch", "-q", "origin")

	a := hs.a
	a.settings.ScmRepositoryOpen = true
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)

	mainRow := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	if mainRow.panel.branch.Current || mainRow.panel.branch.Behind != 1 || !hs.tt.HasText("↓1") {
		t.Fatalf("expected inactive main behind by 1, got %+v detail=%q", mainRow.panel.branch, mainRow.panel.detail)
	}
	behindCount, _ := hs.tt.Find("↓1")
	if aheadCount, ok := hs.tt.Find("↑0"); !ok || behindCount.X >= aheadCount.X {
		t.Fatal("a branch behind its upstream should show ↓1 to the left of ↑0")
	}

	// Advance origin/main once more before pulling so PullBranch must also update origin/main.
	gitIn(t, workDir, "commit", "--allow-empty", "-qm", "remote 2")
	want := strings.TrimSpace(gitIn(t, workDir, "rev-parse", "HEAD"))
	gitIn(t, workDir, "push", "-q", "origin", "main")

	if err := hs.tt.RightClick("main"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(hs.tt.Menu(), locales.PanelPullBranch()) {
		t.Fatalf("missing pull in branch menu: %q", hs.tt.Menu())
	}
	hs.tt.Key(0, ui.KeyEscape)
	if rect, ok := hs.tt.Find("main"); ok {
		hs.tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
		hs.settle()
	} else {
		t.Fatal("main branch is not visible")
	}
	if err := hs.tt.Click(locales.PanelPullBranch() + " main"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	settleScmPanels(t, hs)

	if got := strings.TrimSpace(run("rev-parse", "--abbrev-ref", "HEAD")); got != "dev" {
		t.Fatalf("HEAD switched to %q, want dev", got)
	}
	if got := strings.TrimSpace(run("rev-parse", "HEAD")); got != devHead {
		t.Fatalf("dev HEAD moved: %s != %s", got, devHead)
	}
	mainRow = findPanelRow(t, hs, "panel:branch:refs/heads/main")
	if mainRow.panel.branch.Hash != want || mainRow.panel.branch.Ahead != 0 || mainRow.panel.branch.Behind != 0 {
		t.Fatalf("main after pull: %+v (want hash %s, ahead=0, behind=0)", mainRow.panel.branch, want)
	}
	if hs.tt.HasText("↑0") || hs.tt.HasText("↓0") {
		t.Fatal("an up-to-date branch should hide both counts")
	}
}

func TestScmPushInactiveBranch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, false)
	run := hs.gitRepo()
	upstreamDir := t.TempDir()
	initialHead := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("branch", "release")
	gitIn(t, hs.root, "clone", "--bare", hs.root, upstreamDir)
	run("remote", "add", "origin", upstreamDir)
	run("fetch", "-q", "origin")
	run("switch", "-c", "topic", "--track", "origin/release")
	run("commit", "--allow-empty", "-qm", "topic change")
	want := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("tag", "-a", "topic-tag", "-m", "topic tag")
	run("switch", "main")
	run("config", "push.default", "nothing")
	run("config", "push.followTags", "true")

	hs.a.settings.ScmRepositoryOpen = true
	hs.a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	key := "panel:branch:refs/heads/topic"
	if b := findPanelRow(t, hs, key).panel.branch; b.Ahead != 1 || b.Current || !hs.tt.HasText("↑1") {
		t.Fatalf("want inactive topic ahead by one: %+v", b)
	}
	if _, ok := hs.tt.Find("↓0"); !ok {
		t.Fatal("a branch ahead of its upstream should also show zero behind")
	}
	rect, ok := hs.tt.Find("topic")
	if !ok {
		t.Fatal("topic branch is not visible")
	}
	hs.tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	hs.settle()
	if err := hs.tt.Click(locales.ScmPushCommits(1) + " topic"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	settleScmPanels(t, hs)

	if got := strings.TrimSpace(gitIn(t, upstreamDir, "rev-parse", "refs/heads/release")); got != want {
		t.Fatalf("origin/release = %q, want %q", got, want)
	}
	if got := strings.TrimSpace(gitIn(t, upstreamDir, "rev-parse", "refs/heads/main")); got != initialHead {
		t.Fatalf("origin/main changed to %q", got)
	}
	if got := strings.TrimSpace(gitIn(t, upstreamDir, "for-each-ref", "refs/heads/topic", "refs/tags/topic-tag")); got != "" {
		t.Fatalf("push sent extra refs: %s", got)
	}
	if got := strings.TrimSpace(run("symbolic-ref", "--short", "HEAD")); got != "main" {
		t.Fatalf("push switched to %q", got)
	}
	if b := findPanelRow(t, hs, key).panel.branch; b.Ahead != 0 || b.Behind != 0 {
		t.Fatalf("tracking counts after push: %+v", b)
	}
}
