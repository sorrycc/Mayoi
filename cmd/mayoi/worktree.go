package main

import (
	"errors"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
	"os"
)

// Opening an existing checkout never changes the branch in either worktree.
func (a *gitContext) askOpenWorktree(branch, path string) {
	r := a.git.repo
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && !remote.Is(path) {
		a.askPruneWorktree(branch, path)
		return
	}
	open := func(newWindow bool) {
		if a.git.repo != r {
			return
		}
		a.gitDo(locales.BusyOpeningWorktree(), func(_ *git.Repo) error {
			info, err := vfs.Stat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return errors.New(locales.ErrWorktreeNotFolder(path))
			}
			return nil
		}, func() {
			if newWindow {
				a.app.host.newWindow(path)
			} else {
				a.app.switchFolder(path)
			}
		})
	}
	a.app.confirm = &confirm{
		title:          locales.WorktreeOpenTitle(),
		message:        locales.WorktreeOpenDetail(branch, shortenHome(path)),
		ok:             locales.WorktreeOpenCurrent(),
		okShort:        locales.WorktreeCurrentWindow(),
		run:            func() { open(false) },
		alternate:      locales.WorktreeOpenNew(),
		alternateShort: locales.WorktreeNewWindow(),
		runAlternate:   func() { open(true) },
	}
}

// askPruneWorktree offers to forget a worktree whose folder was deleted
// without git knowing, which still holds the branch, and to switch to it.
func (a *gitContext) askPruneWorktree(branch, path string) {
	r := a.git.repo
	a.app.confirm = &confirm{
		title:   locales.WorktreeMissingTitle(),
		message: locales.WorktreeMissingDetail(branch, shortenHome(path)),
		ok:      locales.WorktreePruneSwitch(),
		run: func() {
			if a.git.repo != r {
				return
			}
			a.gitDo(locales.BusySwitchingTo(branch), func(r *git.Repo) error {
				if err := r.PruneWorktrees(); err != nil {
					return err
				}
				err := r.Switch(branch, false)
				if occupied, ok := errors.AsType[*git.WorktreeInUse](err); ok {
					// Pruning keeps a locked worktree.
					return errors.New(locales.ErrWorktreeLocked(occupied.Path))
				}
				return err
			}, nil)
		},
	}
}
