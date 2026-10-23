package main

import (
	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
)

// Window commands target the selected repository. Repository controls and
// their callbacks call the bound gitContext directly.
func (a *app) historyKey() string       { return a.repository().historyKey() }
func (a *app) refreshHistory()          { a.repository().refreshHistory() }
func (a *app) setStatus(st *git.Status) { a.repository().setStatus(st) }
func (a *app) branch() string           { return a.repository().branch() }
func (a *app) gitChanged() {
	// Editor marks discover Git independently, including repositories created
	// after the window first looked. A global refresh must invalidate them
	// even before source control has discovered a repository.
	for _, d := range a.docs {
		d.git.HeadMoved()
	}
	a.diffsChanged()
	for _, ctx := range a.rootRepositoryList() {
		ctx.gitChanged()
	}
}
func (a *app) gitDo(busy string, fn func(r *git.Repo) error, then func()) {
	a.repository().gitDo(busy, fn, then)
}
func (a *app) fetch()                                  { a.repository().fetch() }
func (a *app) pull()                                   { a.repository().pull() }
func (a *app) push()                                   { a.repository().push() }
func (a *app) switchBranch(branch string, create bool) { a.repository().switchBranch(branch, create) }
func (a *app) checkOutCommit(cm git.Commit)            { a.repository().checkOutCommit(cm) }
func (a *app) inHead(hash string) bool                 { return a.repository().inHead(hash) }
func (a *app) continueOp()                             { a.repository().continueOp() }
func (a *app) abortOp()                                { a.repository().abortOp() }
func (a *app) askRefAt(cm git.Commit, tag bool)        { a.repository().askRefAt(cm, tag) }
func (a *app) askNewBranch()                           { a.repository().askNewBranch() }
func (a *app) groupEntries() [groupCount][]git.Entry   { return a.repository().groupEntries() }
func (a *app) scmRows() []scmRow                       { return a.repository().scmRows() }
func (a *app) toggleDir(r scmRow)                      { a.repository().toggleDir(r) }
func (a *app) historyGraph() []graphRow                { return a.repository().historyGraph() }
func (a *app) toggleAllBranches()                      { a.repository().toggleAllBranches() }
func (a *app) scmTitleActions(c *ui.Context)           { a.repository().scmTitleActions(c) }
func (a *app) chooseScmFile(f *diffFile) {
	ctx := a.repositoryForPath(f.path)
	if f.repoRoot != "" {
		ctx = a.repositoryForRoot(f.repoRoot)
	}
	ctx.changesContext().chooseScmFile(f)
}
func (a *app) scmActionEntries(r scmRow) []git.Entry  { return a.repository().scmActionEntries(r) }
func (a *app) toggleStaged(r scmRow)                  { a.repository().toggleStaged(r) }
func (a *app) stage(paths ...string)                  { a.repository().stage(paths...) }
func (a *app) stageAll()                              { a.repository().stageAll() }
func (a *app) discard(es ...git.Entry)                { a.repository().discard(es...) }
func (a *app) startAmend()                            { a.repository().startAmend() }
func (a *app) undoCommit()                            { a.repository().undoCommit() }
func (a *app) undoCommitOf(hash string)               { a.repository().undoCommitOf(hash) }
func (a *app) commit(push bool)                       { a.repository().commit(push) }
func (a *app) invalidateScmPanels()                   { a.repository().invalidateScmPanels() }
func (a *app) refreshScmPanels()                      { a.repository().refreshScmPanels() }
func (a *app) activateScmPanelRow(r scmRow)           { a.repository().activateScmPanelRow(r) }
func (a *app) switchScmPanelTab(tab scmPanelTab)      { a.repository().switchScmPanelTab(tab) }
func (a *app) askRenamePanelBranch(b git.Branch)      { a.repository().askRenamePanelBranch(b) }
func (a *app) editPanelRemote(name string, push bool) { a.repository().editPanelRemote(name, push) }
func (a *app) askPanelRemote(remote *git.Remote, push bool) {
	a.repository().askPanelRemote(remote, push)
}
func (a *app) askPanelNewBranch()                   { a.repository().askPanelNewBranch() }
func (a *app) writeMessage()                        { a.repository().writeMessage() }
func (a *app) openBranchPicker()                    { a.repository().openBranchPicker() }
func (a *app) branchChoices() []branchChoice        { return a.repository().branchChoices() }
func (a *app) commitPatch(cm git.Commit, save bool) { a.repository().commitPatch(cm, save) }
func (a *app) commitRemote(cm git.Commit)           { a.repository().commitRemote(cm) }
func (a *app) gitRead(busy string, fn func(*git.Repo) error, then func()) {
	a.repository().gitRead(busy, fn, then)
}

func (a *app) gitFailed(err error) { a.repository().gitFailed(err) }
