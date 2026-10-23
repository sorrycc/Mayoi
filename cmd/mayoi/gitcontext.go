package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
)

// gitContext binds source control state, UI and asynchronous commands to a
// work tree. Shared editor services are reached explicitly through app;
// no window Git commands can be promoted onto a repository controller.
type gitContext struct {
	app           *app
	git           *gitState
	identity      *git.Repo
	scm           *scmState
	closed        bool
	changesUI     *scmState
	changesHeight float32
}

type repositoryState struct {
	contexts []*gitContext
	selected string // empty follows the active editor
	scanning bool
	dirty    bool
	token    *int
	picker   repositoryPicker
}

func (a *app) rootRepository() *gitContext {
	if len(a.repositories.contexts) == 0 {
		a.repositories.dirty = true
		a.repositories.contexts = []*gitContext{{app: a, git: &a.git, scm: &a.scm, identity: a.git.repo}}
	}
	if ctx := a.repositories.contexts[0]; ctx.identity != a.git.repo {
		a.repositories.dirty = true
		a.repositories.contexts[0] = &gitContext{app: a, git: &a.git, scm: &a.scm, identity: a.git.repo}
	}
	return a.repositories.contexts[0]
}

func (a *app) repository() *gitContext {
	ctx := a.chosenRepository()
	if ctx.git.repo == nil {
		// A folder that is not a work tree goes on with the first under it.
		for _, sub := range a.repositories.contexts {
			if sub.git.repo != nil {
				return sub
			}
		}
	}
	return ctx
}

// chosenRepository is the one the picker chose, else the active tab's.
func (a *app) chosenRepository() *gitContext {
	root := a.rootRepository()
	if selected := a.repositories.selected; selected != "" {
		for _, ctx := range a.repositories.contexts {
			if ctx.git.repo != nil && ctx.git.repo.Root == selected {
				return ctx
			}
		}
	}
	if len(a.cols) == 0 {
		return root
	}
	if t := a.activeTab(); t != nil {
		if t.diff != nil && t.diff.repoRoot != "" {
			if ctx := a.repositoryForRoot(t.diff.repoRoot); ctx.git.repo != nil {
				return ctx
			}
			return root
		}
		if t.path() != "" {
			return a.repositoryForPath(t.path())
		}
	}
	return root
}

func (a *app) repositoryForRoot(root string) *gitContext {
	for _, ctx := range a.repositories.contexts {
		if ctx.git.repo != nil && ctx.git.repo.Root == root {
			return ctx
		}
	}
	if root == "" {
		return a.rootRepository()
	}
	return &gitContext{app: a, git: &gitState{looked: true}, scm: &scmState{}}
}

// Longest root wins for files. The parent keeps its gitlink in its own
// status and action context. State is never keyed by the common Git dir.
func (a *app) repositoryForPath(path string) *gitContext {
	best := a.rootRepository()
	for _, ctx := range a.repositories.contexts {
		r := ctx.git.repo
		if r != nil && r.Contains(path) && (best.git.repo == nil || len(r.Root) > len(best.git.repo.Root)) {
			best = ctx
		}
	}
	return best
}

func (a *app) resetRepositories() {
	for _, ctx := range a.repositories.contexts {
		if ctx.git.writing != nil {
			ctx.git.writing.cancel()
		}
		if ctx.git != &a.git {
			ctx.git.repo = nil
		}
	}
	a.repositories = repositoryState{}
}

func (a *app) refreshGit() {
	a.lookUpRepo()
	a.rootRepository()
	// A folder that is not a work tree may hold some (#17).
	if !a.git.looked || !a.online() {
		return
	}
	a.discoverRepositories()
	for _, ctx := range a.repositories.contexts {
		ctx.refreshGit()
	}
}

// The index and module roots are read in the background after Git metadata
// or .gitmodules changes, never on a status timer. Registered gitlinks avoid
// walking the workspace; initialized modules remain visible even when clean.
func (a *app) discoverRepositories() {
	state := &a.repositories
	if state.scanning || !state.dirty {
		return
	}
	root, folder := a.git.repo, a.root
	token := new(int)
	state.token, state.scanning, state.dirty = token, true, false
	bg.Go(func() {
		defer recoverCrash()
		// An incomplete scan must not drop live contexts and their messages.
		failed := func() {
			a.post(func() {
				if state.token == token {
					state.scanning = false
				}
			})
		}
		// repos[0] is the folder's own work tree, nil when it is none: then
		// the work trees in the folders under it come after.
		repos := []*git.Repo{root}
		seen := map[string]bool{}
		if root != nil {
			seen[root.Root] = true
		} else {
			nested, err := git.Nested(folder, git.NestedDepth)
			if err != nil {
				failed()
				return
			}
			for _, r := range nested {
				seen[r.Root] = true
				repos = append(repos, r)
			}
		}
		for i := 0; i < len(repos); i++ {
			if repos[i] == nil {
				continue
			}
			subs, err := repos[i].Submodules()
			if err != nil {
				failed()
				return
			}
			for _, sub := range subs {
				if !seen[sub.Root] {
					seen[sub.Root] = true
					repos = append(repos, sub)
				}
			}
		}
		a.post(func() {
			if a.git.repo != root || a.root != folder || state.token != token {
				return
			}
			old := state.contexts
			next := []*gitContext{a.rootRepository()}
			for _, repo := range repos[1:] {
				var ctx *gitContext
				for _, candidate := range old {
					if candidate.git.repo != nil && candidate.git.repo.Root == repo.Root {
						ctx = candidate
						break
					}
				}
				if ctx == nil {
					ctx = &gitContext{app: a, identity: repo, git: &gitState{repo: repo, looked: true, stale: true, remotesStale: true}, scm: &scmState{}}
				}
				next = append(next, ctx)
			}
			for _, ctx := range old {
				if !slices.Contains(next, ctx) {
					if ctx.git.writing != nil {
						ctx.git.writing.cancel()
					}
					ctx.git.repo = nil
				}
			}
			state.contexts = next
			if state.selected != "" && !seen[state.selected] {
				state.selected = ""
			}
			state.scanning = false
		})
	})
}

func (a *app) changeCount() int {
	n := 0
	for _, ctx := range a.rootRepositoryList() {
		if ctx.git.status != nil {
			n += len(ctx.git.status.Entries)
		}
	}
	return n
}

func (a *app) rootRepositoryList() []*gitContext {
	a.rootRepository()
	return a.repositories.contexts
}

func (a *gitContext) owns(path string) bool {
	return a.git.repo != nil && a.app.repositoryForPath(path).git == a.git && a.git.repo.Contains(path)
}

func (a *gitContext) label() string {
	if a.git.repo == nil {
		return ""
	}
	return filepath.Base(a.git.repo.Root)
}

func (a *gitContext) relativeRoot() string {
	if a.git.repo == a.app.git.repo {
		return ""
	}
	if a.git.repo == nil {
		return ""
	}
	rel, err := filepath.Rel(a.app.root, a.git.repo.Root)
	if err != nil || strings.HasPrefix(rel, "..") {
		return a.git.repo.Root
	}
	if rel == "." {
		return ""
	}
	return rel
}

func (a *app) branchPickerView(c *ui.Context) {
	ctx := a.repositoryForRoot(a.branches.repoRoot)
	ctx.branchPickerView(c)
}

// Diff tabs and their actions retain their repository when the history
// picker changes. Legacy tabs without a root belong to the main work tree.
func (a *app) diffContext(v *diffView) *gitContext {
	if v.repoRoot == "" {
		return a.rootRepository()
	}
	return a.repositoryForRoot(v.repoRoot)
}
func (a *app) diffRoot(roots []string) string {
	if len(roots) > 0 {
		return roots[0]
	}
	if r := a.repository().git.repo; r != nil {
		return r.Root
	}
	return ""
}
func (a *app) diffBelongs(v *diffView, root string) bool {
	actual := v.repoRoot
	if actual == "" && a.git.repo != nil {
		actual = a.git.repo.Root
	}
	if root == "" && a.git.repo != nil {
		root = a.git.repo.Root
	}
	return actual == root
}
func (a *gitContext) showChanges(target diffTarget, focus bool) {
	if a.git.repo != nil {
		a.app.showChanges(target, focus, a.git.repo.Root)
	}
}
func (a *gitContext) openChangesTab(focus bool) *diffView {
	if a.git.repo == nil {
		return nil
	}
	return a.app.openChangesTab(focus, a.git.repo.Root)
}
func (a *gitContext) showCommit(cm git.Commit, focus bool) *diffView {
	if a.git.repo != nil {
		return a.app.showCommit(cm, focus, a.git.repo.Root)
	}
	return nil
}
func (a *gitContext) previewCommit(cm git.Commit) *diffView {
	if a.git.repo != nil {
		return a.app.previewCommit(cm, a.git.repo.Root)
	}
	return nil
}

// A controller captured by a dialog must not start work on a folder opened
// later, even though the root gitState lives at the same window address.
func (a *gitContext) live() bool { return a.git.repo != nil && a.git.repo == a.identity }

// repositoryQuestion identifies the target before a destructive action in a
// window that contains more than one repository, including Auto mode.
func (a *gitContext) repositoryQuestion(title string) string {
	if len(a.app.repositories.contexts) < 2 {
		return title
	}
	name := filepath.ToSlash(a.relativeRoot())
	if name == "" {
		name = a.label()
	}
	if branch := a.branch(); branch != "" {
		name += " (" + branch + ")"
	}
	return title + " · " + name
}
