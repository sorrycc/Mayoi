package main

import (
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// A history is the source control view's, scoped to the commits that
// changed some paths: one file's, which follows the file through its
// renames, a folder's, or several files' and folders'. The rows keep
// their graph, their badges and their time, and a scope row under the
// group's header names the paths and clears the scope.

// openFileHistory shows the history of the file of the tab shown, from
// the command palette's command.
func (a *app) openFileHistory() {
	if t := a.activeTab(); t != nil && t.path() != "" {
		a.showFileHistory(t.path())
	}
}

// openFolderHistory shows the history of the folder of the file of the
// tab shown, or of the window's folder when no file is.
func (a *app) openFolderHistory() {
	dir := a.root
	if t := a.activeTab(); t != nil && t.path() != "" {
		dir = remote.Dir(t.path())
	}
	if dir != "" {
		a.showFolderHistory(dir)
	}
}

// showFileHistory scopes the source control view's history to the
// commits that changed a file, which is followed through its renames.
func (a *app) showFileHistory(path string) {
	a.showPathHistory([]string{path}, true)
}

// showFolderHistory scopes the source control view's history to the
// commits that changed anything in a folder.
func (a *app) showFolderHistory(dir string) {
	a.showPathHistory([]string{dir}, false)
}

// showPathsHistory scopes the source control view's history to the
// commits that changed any of paths: files, folders, or both. Their
// renames are not followed, which git does for one file alone.
func (a *app) showPathsHistory(paths []string) {
	a.showPathHistory(paths, false)
}

// showPathHistory scopes the source control view's history to paths and
// shows it, following one file's renames when follow is set.
func (a *app) showPathHistory(paths []string, follow bool) {
	if len(paths) == 0 {
		return
	}
	ctx := a.repositoryForPath(paths[0])
	if ctx.git.repo == nil {
		return
	}
	// The paths of another repository, or another work tree, are not of
	// this history.
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if ctx.git.repo.Contains(p) && !slices.Contains(kept, p) {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return
	}
	// The history pane shows the repository chosen, which must be this
	// history's when several are open.
	if len(a.rootRepositoryList()) > 1 {
		a.selectRepository(ctx.git.repo.Root)
	}
	a.showSourceControl()
	ctx.scm.section = scmFull
	ctx.setHistory(kept, follow && len(kept) == 1)
}

// setHistory reads the history of paths, or the branch's own again when
// there are none, following the file's renames when follow is set.
func (a *gitContext) setHistory(paths []string, follow bool) {
	g, s := a.git, a.scm
	if len(paths) > 0 && g.repo == nil {
		return
	}
	for _, p := range paths {
		if !g.repo.Contains(p) {
			return
		}
	}
	if slices.Equal(g.historyPaths, paths) && g.historyFollow == follow {
		return
	}
	g.historyPaths, g.historyFollow = paths, follow
	g.history, g.historyAt, g.unpushed, g.incoming, g.historyMore = nil, nil, nil, nil, false
	g.historyN = 0
	g.historyGen++
	g.historyOf = ""
	s.collapsed[groupCommits] = false
	s.row = -1
	s.selection.Clear()
	s.anchor = ""
	s.revealScope = len(paths) > 0
	a.app.invalidate()
}

// clearHistory shows the branch's history again.
func (a *gitContext) clearHistory() { a.setHistory(nil, false) }

// fileOf returns the path the file whose history is read had in a
// commit: before a rename, the name it went by then. It is the file's
// path now when the history does not say, as for a merge.
func (a *gitContext) fileOf(hash string) string {
	if name := a.git.historyAt[hash]; name != "" && a.git.repo != nil {
		return a.git.repo.Abs(name)
	}
	if len(a.git.historyPaths) == 1 {
		return a.git.historyPaths[0]
	}
	return ""
}

// showHistoryCommit shows a commit of the history shown, in the preview
// tab or in one of its own: the files of the history's paths alone when
// diffHistoryOnly says so, at the file it follows when not, and every
// file of it for the branch's own history.
func (a *gitContext) showHistoryCommit(cm git.Commit, keep bool) {
	paths := slices.Clone(a.git.historyPaths)
	if len(paths) == 0 {
		if keep {
			a.showCommit(cm, true)
		} else {
			a.previewCommit(cm)
		}
		return
	}
	at := ""
	if a.git.historyFollow {
		// The file went by another name before a rename, and that name
		// is the one the commit changed.
		at = a.fileOf(cm.Hash)
		paths = []string{at}
	}
	folder := len(paths) == 1 && !a.git.historyFollow
	a.showCommitPaths(cm, paths, at, keep, a.app.settings.DiffHistoryOnly, folder)
}

// historyLabel names the paths whose history is read: the file, the
// folder with its slash, or how many paths there are.
func (a *gitContext) historyLabel() string {
	g := a.git
	if g.repo == nil || len(g.historyPaths) == 0 {
		return ""
	}
	if len(g.historyPaths) > 1 {
		return locales.ScmPathsCount(len(g.historyPaths))
	}
	rel := g.repo.Rel(g.historyPaths[0])
	if rel == "" || rel == "." {
		rel = filepath.Base(g.repo.Root)
		return rel + "/"
	}
	if !g.historyFollow {
		return rel + "/"
	}
	return rel
}

// historyTip names the paths whose history is read, and lists them when
// there is more than one.
func (a *gitContext) historyTip() string {
	tip := locales.ScmPathHistory(a.historyLabel())
	paths := a.git.historyPaths
	if len(paths) < 2 || a.git.repo == nil {
		return tip
	}
	// A long selection says how many more there are than it lists.
	const listed = 20
	for i, p := range paths {
		if i == listed {
			tip += "\n" + locales.ScmPathHistoryMore(len(paths)-listed)
			break
		}
		tip += "\n" + a.git.repo.Rel(p)
	}
	return tip
}

// historyIcon is the icon of the row of the paths whose history is read.
func (a *gitContext) historyIcon() *ui.SVG {
	switch {
	case a.git.historyFollow:
		return iconFile
	case len(a.git.historyPaths) > 1:
		return iconFiles
	}
	return iconFolder
}
