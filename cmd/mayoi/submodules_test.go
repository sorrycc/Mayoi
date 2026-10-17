package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/git"
	"mayoi/locales"
)

// A real gitlink, with its own index and detached HEAD, reproduces the
// workspace from the report without modifying the user's reference repo.
func submoduleHarness(t *testing.T) (*harness, *gitContext, func(...string) string) {
	t.Helper()
	// Keep this fixture independent of the developer's global Git identity.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	hs := newHarness(t, true)
	parent := hs.gitRepo()
	source := t.TempDir()
	gitIn(t, source, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, "module.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, source, "add", ".")
	gitIn(t, source, "commit", "-q", "-m", "module first")
	parent("-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "packages/module")
	parent("commit", "-q", "-m", "add module")
	subroot := hs.path("packages/module")
	gitIn(t, subroot, "checkout", "-q", "--detach")
	gitIn(t, subroot, "config", "user.useConfigOnly", "true")
	// A clone does not inherit its parent's local user configuration. These
	// commits run through the application, without gitIn's per-command -c flags.
	gitIn(t, subroot, "config", "user.name", "t")
	gitIn(t, subroot, "config", "user.email", "t@t")
	hs.a.repositories.dirty = true
	hs.a.showSourceControl()
	settleRepositories(t, hs, 2)
	return hs, hs.a.repositoryForRoot(subroot), parent
}

func settleRepositories(t *testing.T, hs *harness, count int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		hs.settle()
		contexts := hs.a.repositories.contexts
		ready := len(contexts) == count && !hs.a.repositories.scanning
		for _, ctx := range contexts {
			g := ctx.git
			ready = ready && !g.running && !g.stale && g.pending == 0 && g.busy == "" && g.status != nil && !g.loadingHistory && g.historyOf == ctx.historyKey()
		}
		if ready {
			return
		}
	}
	t.Fatalf("repositories did not settle: want %d, got %+v", count, hs.a.repositories.contexts)
}

func TestSubmoduleIndependentChangesAndCommits(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	root := sub.git.repo.Root
	file := filepath.Join(root, "module.txt")
	os.WriteFile(file, []byte("staged\n"), 0o644)
	gitIn(t, root, "add", "module.txt")
	os.WriteFile(file, []byte("unstaged\n"), 0o644)
	a.gitChanged()
	settleRepositories(t, hs, 2)
	groups := sub.groupEntries()
	if len(groups[groupStaged]) != 1 || len(groups[groupChanges]) != 1 {
		t.Fatalf("module groups: %+v", groups)
	}
	if sub.git.status.Branch != "" || sub.branch() == "" {
		t.Fatalf("detached HEAD not shown: %+v", sub.git.status)
	}
	if len(a.git.status.Entries) != 1 || !a.git.status.Entries[0].Submodule() {
		t.Fatalf("parent entries: %+v", a.git.status.Entries)
	}
	if a.repositoryForPath(file) != sub {
		t.Fatal("file belongs to parent")
	}
	if !hs.tt.HasText("module") || !hs.tt.HasText("module.txt") {
		t.Fatalf("missing module UI: %q", hs.tt.Texts())
	}
	hs.shot("submodules")
	before := strings.TrimSpace(parent("rev-parse", "HEAD"))
	a.git.message = "parent message"
	sub.git.message = "module commit"
	// History selection must not redirect the module's commit button.
	a.selectRepository(a.git.repo.Root)
	sub.commit(false)
	settleRepositories(t, hs, 2)
	if got := strings.TrimSpace(gitIn(t, root, "log", "-1", "--format=%s")); got != "module commit" {
		t.Fatalf("module log: %q; UI: %q", got, hs.tt.Texts())
	}
	if strings.TrimSpace(parent("rev-parse", "HEAD")) != before || a.git.message != "parent message" {
		t.Fatal("module commit changed parent")
	}
	if sub.git.message != "" || len(sub.groupEntries()[groupChanges]) != 1 {
		t.Fatal("module message/unstaged work lost")
	}
	// The parent records only the new gitlink; its commit leaves inner work.
	a.rootRepository().stage(root)
	settleRepositories(t, hs, 2)
	a.rootRepository().commit(false)
	settleRepositories(t, hs, 2)
	if strings.TrimSpace(parent("log", "-1", "--format=%s")) != "parent message" {
		t.Fatal("parent did not commit reference")
	}
	if text, _ := os.ReadFile(file); string(text) != "unstaged\n" {
		t.Fatalf("module work changed: %q", text)
	}
}

func TestRepositoryHistorySelectionAndBoundDiff(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	root := sub.git.repo.Root
	path := filepath.Join(root, "module.txt")
	os.WriteFile(path, []byte("module edit\n"), 0o644)
	a.gitChanged()
	settleRepositories(t, hs, 2)
	a.openFile(path, true)
	a.selectRepository("")
	if a.repository() != sub {
		t.Fatal("auto did not follow editor")
	}
	a.selectRepository(a.git.repo.Root)
	if a.repository() == sub {
		t.Fatal("manual history selection followed editor")
	}
	sub.showChanges(diffTarget{path: path, base: diffIndex}, true)
	v := a.activeTab().diff
	a.listDiffFiles(v)
	if v.repoRoot != root || len(v.files) != 1 || v.files[0].path != path {
		t.Fatalf("module diff: %+v", v)
	}
	a.selectRepository(a.git.repo.Root)
	if a.diffContext(v) != sub {
		t.Fatal("history switch redirected diff")
	}
	a.selectRepository(root)
	if a.repository() != sub || a.repository().git.history[0].Subject != "module first" {
		t.Fatal("wrong selected history")
	}
	sub.scm = &scmState{section: scmHistory, row: 3}
	sub.scm.list.ScrollIntoView(3)
	sub.git.historyN = 100
	a.selectRepository(a.git.repo.Root)
	a.selectRepository(root)
	if sub.scm.row != 3 || sub.git.historyN != 100 {
		t.Fatal("history state lost")
	}
	// The same hash can occur in two different repositories.
	hash := sub.git.history[0].Hash
	sub.showCommit(git.Commit{Hash: hash}, true)
	moduleTab := a.activeTab()
	a.rootRepository().showCommit(git.Commit{Hash: hash}, true)
	if a.activeTab() == moduleTab || a.activeTab().diff.repoRoot == root {
		t.Fatal("commit tabs collided across repositories")
	}
	a.repositories.picker.query = "packages/module"
	if choices := a.repositoryChoices(); len(choices) != 1 || choices[0] != root {
		t.Fatalf("filtered choices: %v", choices)
	}
}

func TestSubmodulePendingCommandKeepsItsRepository(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	release := make(chan struct{})
	started := make(chan struct{})
	called := false
	sub.gitDo("module command", func(r *git.Repo) error {
		close(started)
		<-release
		if r.Root != sub.git.repo.Root {
			return os.ErrInvalid
		}
		return nil
	}, func() { called = true })
	<-started
	a.selectRepository(a.git.repo.Root)
	close(release)
	settleRepositories(t, hs, 2)
	if !called || sub.git.pending != 0 || a.git.pending != 0 || a.git.busy != "" {
		t.Fatal("command state leaked across repositories")
	}
}

func TestNestedSubmoduleDiscoveryAndRemoval(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	root := sub.git.repo.Root
	source := t.TempDir()
	gitIn(t, source, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(source, "nested.txt"), []byte("nested\n"), 0o644)
	gitIn(t, source, "add", ".")
	gitIn(t, source, "commit", "-q", "-m", "nested first")
	gitIn(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "nested")
	gitIn(t, root, "commit", "-q", "-am", "add nested module")
	a.repositories.dirty = true
	a.gitChanged()
	settleRepositories(t, hs, 3)
	nested := a.repositoryForPath(filepath.Join(root, "nested", "nested.txt"))
	if nested == sub || nested.git.repo.Root != filepath.Join(root, "nested") {
		t.Fatal("nested file did not use innermost repo")
	}
	sub.git.message = "keep this message"
	a.repositories.dirty = true
	settleRepositories(t, hs, 3)
	if a.repositoryForRoot(root) != sub || sub.git.message != "keep this message" {
		t.Fatal("discovery replaced module state")
	}
	a.selectRepository(root)
	parent("submodule", "deinit", "-f", "--", "packages/module")
	a.repositories.dirty = true
	a.gitChanged()
	settleRepositories(t, hs, 1)
	if a.repositories.selected != "" || sub.git.repo != nil {
		t.Fatal("removed module remained selectable")
	}
}

func TestWorktreeWithSubmoduleKeepsWorktreeRoot(t *testing.T) {
	hs, _, parent := submoduleHarness(t)
	worktree := filepath.Join(t.TempDir(), "linked")
	parent("worktree", "add", "-q", "-b", "linked", "--", worktree)
	gitIn(t, worktree, "-c", "protocol.file.allow=always", "submodule", "update", "--init")
	hs.a.openFolder(worktree)
	hs.a.showSourceControl()
	settleRepositories(t, hs, 2)
	a := hs.a
	sub := a.repositoryForPath(filepath.Join(worktree, "packages/module/module.txt"))
	if a.git.repo.Root != worktree || a.git.repo.Dir == a.git.repo.Common || sub.git.repo.Root != filepath.Join(worktree, "packages/module") {
		t.Fatalf("wrong worktree or module: %+v %+v", a.git.repo, sub.git.repo)
	}
	if a.git.status.Branch != "linked" {
		t.Fatalf("branch %q", a.git.status.Branch)
	}
}

func TestRepositoryDialogCannotCommitInNewWorkspace(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	os.WriteFile(hs.path("notes.txt"), []byte("pending\n"), 0o644)
	hs.settleGit()
	a.git.message = "old dialog"
	a.commit(false)
	if a.confirm == nil {
		t.Fatal("expected stage-all confirmation")
	}
	run := a.confirm.run
	old := a.rootRepository()
	// A new root can reuse the window's gitState address; old callbacks
	// must be rejected before staging or saving any of its documents.
	a.git = gitState{repo: &git.Repo{Root: t.TempDir()}}
	invoked := false
	old.gitDo("stale", func(*git.Repo) error { invoked = true; return nil }, nil)
	run()
	if invoked || a.git.pending != 0 || a.git.busy != "" {
		t.Fatal("old dialog started a command in the new workspace")
	}
}

func TestSubmoduleUIAndStatusRepositoryPicker(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	hs.tt.SetSize(1200, 1000)
	a.window.SidebarWidth = 340
	path := filepath.Join(sub.git.repo.Root, "module.txt")
	os.WriteFile(path, []byte("staged via UI\n"), 0o644)
	gitIn(t, sub.git.repo.Root, "add", "module.txt")
	a.gitChanged()
	settleRepositories(t, hs, 2)
	// Both repository sections must lay out real, clickable rows, including
	// a virtual change list nested inside the changes pane's scroll view.
	if b, ok := hs.tt.Find("module.txt"); !ok || b.W <= 0 || b.H <= 0 {
		t.Fatalf("module row has no bounds: %v %v", b, ok)
	}
	if err := hs.tt.Click("module.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if tab := a.activeTab(); tab.diff == nil || tab.diff.repoRoot != sub.git.repo.Root {
		t.Fatal("click did not open module diff")
	}
	// Exercise the bottom status bar, then search and submit with the keyboard.
	button, ok := hs.tt.Find(locales.RepositorySelect() + " " + sub.label())
	if !ok || button.Y < 970 {
		t.Fatalf("missing bottom repository picker: %v %v", button, ok)
	}
	hs.tt.ClickAt(button.X+button.W/2, button.Y+button.H/2)
	hs.settle()
	if !a.repositories.picker.open {
		t.Fatal("bottom repository button did not open picker")
	}
	hs.tt.Type("packages/module")
	hs.settle()
	hs.shot("submodule-picker")
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if a.repositories.selected != sub.git.repo.Root || a.repositories.picker.open {
		t.Fatal("picker did not select module history")
	}
	a.selectRepository(a.git.repo.Root)
	sub.git.message = "commit from module button"
	sub.scm.panels.key = "cached before module commit"
	sub.git.checked = time.Now().Add(time.Hour)
	hs.settle()
	before := strings.TrimSpace(parent("rev-parse", "HEAD"))
	if err := hs.tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	settleRepositories(t, hs, 2)
	if strings.TrimSpace(gitIn(t, sub.git.repo.Root, "log", "-1", "--format=%s")) != "commit from module button" || strings.TrimSpace(parent("rev-parse", "HEAD")) != before {
		t.Fatalf("module commit button did not produce the expected commit; UI: %q", hs.tt.Texts())
	}
	if sub.scm.panels.key != "" {
		t.Fatal("module commit did not invalidate its history panel")
	}
	// A file command also follows the file, even with parent history pinned.
	os.WriteFile(path, []byte("second edit\n"), 0o644)
	a.openFile(path, true)
	a.run("git.stageFile")
	settleRepositories(t, hs, 2)
	if len(sub.groupEntries()[groupStaged]) != 1 || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
		t.Fatal("stage file command used history selection")
	}
	a.selectRepository(sub.git.repo.Root)
	hs.settle()
	hs.tt.Move(1100, 800)
	hs.settle()
	hs.shot("submodule-history")
}

func TestSubmoduleChangesPaneScroll(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	file := filepath.Join(sub.git.repo.Root, "module.txt")
	os.WriteFile(file, []byte("changed\n"), 0o644)
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	// At normal window height the lower module section is below the fold.
	// Its rows must become visible and remain actionable after scrolling.
	hs.tt.Scroll(150, 300, 0, 400)
	hs.settle()
	if b, ok := hs.tt.Find("module.txt"); !ok || b.W <= 0 || b.H <= 0 {
		t.Fatalf("scrolled module row has no bounds: %v %v", b, ok)
	}
	if err := hs.tt.Click("module.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v := hs.a.activeTab().diff; v == nil || v.repoRoot != sub.git.repo.Root {
		t.Fatal("scrolled row did not open module diff")
	}
}

func TestSubmoduleLayoutUsesSpaceForChanges(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	hs.a.window.SidebarWidth = 300
	os.WriteFile(filepath.Join(sub.git.repo.Root, "module.txt"), []byte("changed\n"), 0o644)
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	hs.shot("submodules-layout")
	file, ok := hs.tt.Find("module.txt")
	history, hok := hs.tt.Find("COMMITS")
	if !ok || !hok || file.Y+file.H > history.Y || file.H < 18 {
		t.Fatalf("module file clipped while history has room: file=%v (%v), history=%v (%v)", file, ok, history, hok)
	}
	if hs.a.rootRepository().relativeRoot() != "" {
		t.Fatal("root must have no relative path label")
	}
}

func TestCleanSubmodulesOnlyShowHeaders(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	for i := 2; i <= 5; i++ {
		parent("-c", "protocol.file.allow=always", "submodule", "add", "-q", sub.git.repo.Root, fmt.Sprintf("packages/module-%d", i))
	}
	parent("commit", "-qam", "add clean modules")
	hs.a.rootRepository().tookGitMeta(gitMeta(hs.a.git.repo))
	settleRepositories(t, hs, 6)
	sub.git.message = "keep this draft while clean"
	hs.settle()
	hs.shot("submodules-clean")
	if hs.tt.HasText("Commit Message") || hs.tt.HasText("No Changes") {
		t.Fatalf("clean repositories still show commit controls: %q", hs.tt.Texts())
	}
}

func TestRepositoryDiscoveryOnlyOnMetadataChanges(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	token := a.repositories.token
	for _, ctx := range a.repositories.contexts {
		ctx.git.checked = time.Now().Add(time.Hour)
		ctx.git.read = time.Now().Add(-2 * statusEvery)
	}
	// A status interval is not a reason to enumerate any repository's index.
	a.refreshGit()
	settleRepositories(t, hs, 2)
	if a.repositories.token != token {
		t.Fatal("status timer rescanned module indexes")
	}
	// An unstaged .gitmodules edit and index updates both invalidate discovery.
	modules := hs.path(".gitmodules")
	data, _ := os.ReadFile(modules)
	os.WriteFile(modules, append(data, []byte("\n# changed\n")...), 0o644)
	a.rootRepository().tookGitMeta(gitMeta(a.git.repo))
	a.discoverRepositories()
	settleRepositories(t, hs, 2)
	if a.repositories.token == token {
		t.Fatal(".gitmodules edit did not rescan")
	}
	token = a.repositories.token
	parent("add", ".gitmodules")
	a.rootRepository().tookGitMeta(gitMeta(a.git.repo))
	a.discoverRepositories()
	settleRepositories(t, hs, 2)
	if a.repositories.token == token {
		t.Fatal("index update did not rescan")
	}
	// Deinit updates Git config even when the gitlink and index are unchanged.
	parent("submodule", "deinit", "-f", "--", "packages/module")
	a.rootRepository().tookGitMeta(gitMeta(a.git.repo))
	settleRepositories(t, hs, 1)
	if sub.git.repo != nil {
		t.Fatal("deinitialized module retained")
	}
}

func TestRepositoryDestructiveDialogsNameTarget(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	gitIn(t, sub.git.repo.Root, "commit", "--allow-empty", "-qm", "module second")
	path := filepath.Join(sub.git.repo.Root, "module.txt")
	os.WriteFile(path, []byte("changed\n"), 0o644)
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	hs.a.openFile(path, true)
	hs.a.selectRepository("") // Auto follows the module; the dialog captures it.
	before := strings.TrimSpace(parent("rev-parse", "HEAD"))
	hs.a.run("git.undoCommit")
	cf := hs.a.confirm
	if cf == nil || !strings.Contains(cf.title, "packages/module") {
		t.Fatalf("unnamed Undo target: %+v", cf)
	}
	hs.a.selectRepository(hs.a.git.repo.Root)
	cf.run()
	settleRepositories(t, hs, 2)
	if strings.TrimSpace(parent("rev-parse", "HEAD")) != before {
		t.Fatal("Undo dialog followed picker into parent")
	}
	if got := strings.TrimSpace(gitIn(t, sub.git.repo.Root, "log", "-1", "--format=%s")); got != "module first" {
		t.Fatalf("Undo failed in module: %q", got)
	}
	sub.discard(git.Entry{Path: path, X: '.', Y: 'M'})
	if !strings.Contains(hs.a.confirm.title, "packages/module") {
		t.Fatal("unnamed Discard target")
	}
	sub.resetTo(git.Commit{Hash: sub.git.status.Head})
	if !strings.Contains(hs.a.confirm.title, "packages/module") {
		t.Fatal("unnamed Reset target")
	}
}

func TestRepositoryDiffRootsMoveWithWorkspace(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	sub.showChanges(diffTarget{base: diffIndex}, true)
	hs.a.rememberTabs()
	saved := hs.a.window.Columns
	if len(saved) == 0 || len(saved[0].Tabs) == 0 {
		t.Fatal("no saved diff tab")
	}
	root := saved[0].Tabs[0].RepoRoot
	if root != "packages/module" {
		t.Fatalf("saved module root: %q", root)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	os.MkdirAll(moved, 0o755)
	hs.a.openFolder(moved)
	hs.a.window.Columns = saved
	hs.a.restoreTabs()
	if v := hs.a.activeTab().diff; v == nil || v.repoRoot != filepath.Join(moved, "packages/module") {
		t.Fatalf("restored module root did not follow workspace: %+v", v)
	}
}
