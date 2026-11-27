package main

// A file's history is the source control view's, scoped to the commits
// that changed one file: the rows keep their graph, their badges and
// their time, and a scope row under the group's header names the file
// and clears the scope.

// openFileHistory shows the history of the file of the tab shown, from
// the command palette's command.
func (a *app) openFileHistory() {
	if t := a.activeTab(); t != nil && t.path() != "" {
		a.showFileHistory(t.path())
	}
}

// showFileHistory scopes the source control view's history to the
// commits that changed path, and shows them.
func (a *app) showFileHistory(path string) {
	ctx := a.repositoryForPath(path)
	if ctx.git.repo == nil || !ctx.git.repo.Contains(path) {
		return
	}
	// The history pane shows the repository chosen, which must be this
	// file's when several are open.
	if len(a.rootRepositoryList()) > 1 {
		a.selectRepository(ctx.git.repo.Root)
	}
	a.showSourceControl()
	ctx.scm.section = scmFull
	ctx.setFileHistory(path)
}

// setFileHistory reads the history of path, or the branch's own again
// when path is "".
func (a *gitContext) setFileHistory(path string) {
	g, s := a.git, a.scm
	if path != "" && (g.repo == nil || !g.repo.Contains(path)) {
		return
	}
	if g.historyPath == path {
		return
	}
	g.historyPath = path
	g.history, g.historyAt, g.unpushed = nil, nil, nil
	g.historyN = 0
	g.historyGen++
	g.historyOf = ""
	s.collapsed[groupCommits] = false
	s.row = -1
	s.selection.Clear()
	s.anchor = ""
	s.revealScope = path != ""
	a.app.invalidate()
}

// fileOf returns the path the file whose history is read had in a
// commit: before a rename, the name it went by then. It is the file's
// path now when the history does not say, as for a merge.
func (a *gitContext) fileOf(hash string) string {
	if name := a.git.historyAt[hash]; name != "" && a.git.repo != nil {
		return a.git.repo.Abs(name)
	}
	return a.git.historyPath
}

// clearFileHistory shows the branch's history again.
func (a *gitContext) clearFileHistory() { a.setFileHistory("") }
