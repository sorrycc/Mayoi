package main

import (
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/vfs"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// commitTools keeps the selected comparison tied to its repository.
type commitTools struct {
	repo     *git.Repo
	selected string
}

func (a *gitContext) commitToolsMenu(m *ui.Menu, cm git.Commit, head bool, action *func()) {
	m.Separator()
	if m.Item(locales.CommitCreatePatch()).Chosen() {
		*action = func() { a.commitPatch(cm, true) }
	}
	if m.Item(locales.CommitCopyPatch()).Chosen() {
		*action = func() { a.commitPatch(cm, false) }
	}
	m.Separator()
	// HEAD compared with itself, or a commit with itself, has nothing to show.
	if m.Item(locales.CommitCompareHeadToHere()).Disabled(head).Chosen() {
		*action = func() { a.compareRevisions("HEAD", cm.Hash) }
	}
	if m.Item(locales.CommitCompareHereToHead()).Disabled(head).Chosen() {
		*action = func() { a.compareRevisions(cm.Hash, "HEAD") }
	}
	if m.Item(locales.CommitSelectForCompare()).Chosen() {
		a.app.commitTools = commitTools{repo: a.git.repo, selected: cm.Hash}
	}
	selected := a.app.commitTools
	if selected.repo == a.git.repo && selected.selected != "" {
		if m.Item(locales.CommitCompareWithSelected(git.ShortHash(selected.selected))).Disabled(selected.selected == cm.Hash).Chosen() {
			*action = func() { a.compareRevisions(selected.selected, cm.Hash) }
		}
	}
	m.Separator()
	if m.Item(locales.CommitOpenOnRemote()).Chosen() {
		*action = func() { a.commitRemote(cm) }
	}
}

// compareRevisions opens the changes from one commit to another in a
// diff of their own, side by side or inline as the changes show, which
// stays on those two commits however HEAD moves after.
func (a *gitContext) compareRevisions(from, to string) {
	r := a.git.repo
	if r == nil {
		return
	}
	var hashes [2]string
	a.gitRead(locales.BusyComparing(), func(repo *git.Repo) (err error) {
		for i, rev := range []string{from, to} {
			if hashes[i], err = repo.ResolveCommit(rev); err != nil {
				return err
			}
		}
		return nil
	}, func() {
		if a.git.repo != r {
			return
		}
		a.showCommit(git.Commit{Hash: hashes[0] + ".." + hashes[1]}, true)
	})
}

func (a *gitContext) commitPatch(cm git.Commit, save bool) {
	r := a.git.repo
	if r == nil {
		return
	}
	var patch string
	a.gitRead(locales.BusyReadingPatch(), func(repo *git.Repo) (err error) { patch, err = repo.CommitPatch(cm.Hash); return }, func() {
		if a.git.repo != r {
			return
		}
		if !save {
			a.app.services.WriteClipboard(patch)
			a.app.tell(note{message: locales.CopiedPatchOf(cm.Short())})
			return
		}
		a.app.askSavePath(cm.Short()+".patch", func(path string) {
			if path == "" || a.git.repo != r {
				return
			}
			a.gitRead(locales.BusySavingPatch(), func(_ *git.Repo) error { _, err := vfs.WriteFile(path, []byte(patch), 0o644); return err }, func() { a.app.tell(note{message: locales.PatchSavedTo(hostPath(path))}) })
		})
	})
}

func (a *gitContext) commitRemote(cm git.Commit) {
	var link string
	a.gitRead(locales.BusyReadingRemote(), func(repo *git.Repo) (err error) { link, err = repo.CommitRemoteURL(cm); return }, func() { a.app.services.OpenURL(link) })
}

// gitRead runs without invalidating status or loaded history. Results and
// pending counters belong to the repository captured when the read starts.
func (a *gitContext) gitRead(busy string, fn func(*git.Repo) error, then func()) {
	if !a.live() {
		return
	}
	g := a.git
	r := g.repo
	if r == nil {
		return
	}
	if !a.app.online() {
		a.app.fail("%s", locales.NotConnectedTo(a.app.conn.Host))
		return
	}
	if g.busy != "" {
		return
	}
	g.busy = busy
	g.pending++
	bg.Go(func() {
		err := fn(r)
		a.app.post(func() {
			if g.repo != r {
				return
			}
			g.pending--
			g.busy = ""
			if err != nil {
				a.gitFailed(err)
				return
			}
			if then != nil {
				then()
			}
		})
	})
}

// fileOnRemote opens the file at path on its repository's hosting
// service, or copies a permalink to it, at the commit checked out. When
// d is its editor, the link marks the caret's line or the lines chosen.
func (a *app) fileOnRemote(path string, d *editor, permalink bool) {
	ctx := a.repositoryForPath(path)
	if path == "" || ctx.git.repo == nil {
		return
	}
	from, to := 0, 0
	if d != nil && d.path == path {
		if n := editorExcerpt(d); n != nil {
			from, to = n.from+1, n.to
		}
	}
	var link string
	ctx.gitRead(locales.BusyReadingRemote(), func(repo *git.Repo) (err error) {
		link, err = repo.FileRemoteURL(path, from, to, permalink)
		return
	}, func() {
		if permalink {
			a.services.WriteClipboard(link)
			a.tell(note{message: locales.CopiedPermalink()})
			return
		}
		a.services.OpenURL(link)
	})
}

// activeFileOnRemote is fileOnRemote for the tab shown.
func (a *app) activeFileOnRemote(permalink bool) {
	if t := a.activeTab(); t != nil {
		a.fileOnRemote(t.path(), t.ed, permalink)
	}
}
