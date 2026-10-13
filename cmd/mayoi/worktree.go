package main

import (
	"fmt"
	"mayoi/internal/git"
	"mayoi/internal/remotefs"
)

// Opening an existing checkout never changes the branch in either worktree.
func (a *app) askOpenWorktree(branch, path string) {
	r := a.git.repo
	open := func(newWindow bool) {
		if a.git.repo != r {
			return
		}
		a.gitDo("Opening worktree…", func(_ *git.Repo) error {
			if remotefs.IsRemote(path) && !remotefs.Connected(path) {
				if err := remotefs.Connect(path); err != nil {
					return err
				}
			}
			info, err := remotefs.Stat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("Worktree is not a folder: %s", remotefs.Display(path))
			}
			return nil
		}, func() { a.openRemoteProject(path, newWindow) })
	}
	a.confirm = &confirm{
		title:        "Open Existing Worktree?",
		message:      fmt.Sprintf("The branch %q is already checked out in:\n\n%s\n\nChoose where to open this worktree.", branch, remotefs.Display(path)),
		ok:           "Open in Current Window",
		run:          func() { open(false) },
		alternate:    "Open in New Window",
		runAlternate: func() { open(true) },
	}
}
