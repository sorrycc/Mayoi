package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// worktreePicker is the box that switches worktrees: the checkouts of the
// repository, the one open first, found by folder and branch, and
// the name typed, to create a worktree on a new branch of.
type worktreePicker struct {
	repoRoot string
	open     bool
	query    string
	sel      int
	moved    bool
	loading  bool
	// head is the commit the work tree is at when the box opened, which a
	// worktree created starts from; "" before the first commit.
	head string
	// from names head for the row that creates a worktree: the branch checked
	// out when the box opened and its short hash, or the hash alone detached.
	from string
	// trees are those read when the box opened, the current one first;
	// names are what the query matches, the folder and branch of each.
	trees   []git.Worktree
	names   []string
	results []workspace.Match
	shown   string // the query the results are of
	stale   bool
}

// worktreeName is the name a worktree shows by: the folder it is in.
func worktreeName(t git.Worktree) string { return filepath.Base(t.Path) }

// openWorktreePicker shows the box of the worktrees, reading them and the
// commit HEAD is at in the background.
func (a *gitContext) openWorktreePicker() {
	if !a.live() || a.git.busy != "" {
		return
	}
	r := a.git.repo
	if r == nil {
		return
	}
	w := &a.app.worktrees
	*w = worktreePicker{open: true, loading: true, repoRoot: r.Root}
	branch := ""
	if st := a.git.status; st != nil {
		branch = st.Branch
	}
	bg.Go(func() {
		trees, err := r.Worktrees()
		head := r.Head()
		a.app.post(func() {
			if !w.open || w.repoRoot != r.Root || a.git.repo != r {
				return
			}
			if err != nil {
				a.closeWorktreePicker()
				a.gitFailed(err)
				return
			}
			w.loading = false
			w.head = head
			w.from = git.ShortHash(head)
			if branch != "" {
				w.from = branch + " (" + w.from + ")"
			}
			// A bare repository is not a checkout to switch to.
			w.trees = slices.DeleteFunc(trees, func(t git.Worktree) bool { return t.Bare })
			slices.SortStableFunc(w.trees, func(x, y git.Worktree) int { return btoi(!x.Current) - btoi(!y.Current) })
			w.names = make([]string, len(w.trees))
			for i, t := range w.trees {
				w.names[i] = strings.TrimSpace(worktreeName(t) + " " + t.Branch)
			}
			w.stale = true
		})
	})
}

// closeWorktreePicker shuts the box and lets go of the worktrees it read.
func (a *gitContext) closeWorktreePicker() {
	a.app.worktrees = worktreePicker{}
	if a.app.focus != paneTerminal {
		a.app.focusColumn()
	}
}

// worktreeChoice is a row of the box: a worktree, or the name typed, to
// create one of.
type worktreeChoice struct {
	create string // the name of the branch to create a worktree on, "" for a worktree
	at     int    // the result shown, for a worktree
}

// worktreeChoices returns the rows of the box: the worktrees the query
// matches, preceded by creating one when the query names a branch and
// folder that are not taken yet.
func (a *gitContext) worktreeChoices() []worktreeChoice {
	w := &a.app.worktrees
	var out []worktreeChoice
	if name := strings.TrimSpace(w.query); !w.loading && name != "" && w.head != "" && validBranch(name) && !a.worktreeTaken(name) {
		out = append(out, worktreeChoice{create: name})
	}
	for i := range w.results {
		out = append(out, worktreeChoice{at: i})
	}
	return out
}

// worktreeTaken reports whether a branch or a folder of worktrees has the
// name, as a worktree made of it would need both new.
func (a *gitContext) worktreeTaken(name string) bool {
	w := &a.app.worktrees
	folder := strings.ReplaceAll(name, "/", "-")
	if slices.Contains(a.git.branches, name) {
		return true
	}
	for _, t := range w.trees {
		if t.Branch == name || worktreeName(t) == folder {
			return true
		}
	}
	return false
}

// chooseWorktree does what a row of the box does: switches to its
// worktree, or creates the worktree typed from the commit HEAD is at.
func (a *gitContext) chooseWorktree(ch worktreeChoice) {
	w := &a.app.worktrees
	if !a.live() || a.git.busy != "" || !w.open || w.repoRoot != a.git.repo.Root {
		return
	}
	if ch.create != "" {
		base := w.head
		a.closeWorktreePicker()
		a.createWorktree(ch.create, base)
		return
	}
	if ch.at < 0 || ch.at >= len(w.results) {
		return
	}
	t := w.trees[w.results[ch.at].Index]
	a.closeWorktreePicker()
	if !t.Current {
		a.openWorktree(t.Path)
	}
}

// openWorktree switches the window to a worktree's folder, once it is
// found to be a folder. A folder that is gone is reported, not pruned:
// the branch stays checked out where it is.
func (a *gitContext) openWorktree(path string) {
	if !a.live() {
		return
	}
	if !a.app.online() {
		a.app.fail("%s", locales.NotConnectedTo(a.app.conn.Host))
		return
	}
	r := a.git.repo
	bg.Go(func() {
		var found error
		info, err := vfs.Stat(path)
		switch {
		case err != nil:
			found = err
		case !info.IsDir():
			found = errors.New(locales.ErrWorktreeNotFolder(path))
		}
		a.app.post(func() {
			// The window moved to another repository while the folder was read.
			if a.git.repo != r {
				return
			}
			if errors.Is(found, fs.ErrNotExist) {
				a.app.fail("%s", locales.WorktreeMissing(shortenHome(path)))
				return
			}
			if found != nil {
				a.gitFailed(found)
				return
			}
			a.app.switchFolder(path)
		})
	})
}

// createWorktree makes a worktree on a new branch from base, beside the
// main one, and switches to it. Once made, it stays made whatever happens
// to opening it.
func (a *gitContext) createWorktree(name, base string) {
	var path string
	var created error
	a.gitDo(locales.BusyCreatingWorktree(name), func(r *git.Repo) error {
		path, created = r.AddWorktree(name, base)
		return nil
	}, func() {
		var exists *git.WorktreeExists
		switch {
		case errors.As(created, &exists):
			a.app.fail("%s", locales.WorktreeExists(shortenHome(exists.Path)))
			return
		case created != nil:
			a.gitFailed(created)
			return
		}
		a.app.tell(note{message: locales.WorktreeCreated(shortenHome(path))})
		a.app.switchFolder(path)
	})
}

// worktreePickerView shows the box of the worktrees while it is open.
func (a *gitContext) worktreePickerView(c *ui.Context) {
	w := &a.app.worktrees
	if !w.open {
		return
	}
	p := a.app.pal
	changed := w.stale || w.shown != w.query
	if changed {
		w.results = workspace.Fuzzy(w.names, strings.TrimSpace(w.query), len(w.names))
		w.shown, w.stale, w.sel = w.query, false, 0
	}
	choices := a.worktreeChoices()
	if changed && len(w.results) > 0 && len(choices) > len(w.results) &&
		strings.Contains(strings.ToLower(w.names[w.results[0].Index]), strings.ToLower(strings.TrimSpace(w.query))) {
		// Enter switches to the first match when its folder or branch has the
		// query as typed; a new name that only matches scattered letters, as
		// api does feature-login-page, creates.
		w.sel = 1
	}
	ui.Overlay(c, func() {
		back := a.app.dialogBack(c, "worktrees")
		back.Children(func() {
			box := a.app.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.WorktreeSwitchLabel())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconFolderGit).FontSize(15).TextColor(p.muted)
					in := a.app.textKeys(ui.TextInputBase(c.Key("worktree-input"), &w.query)).Grow(1).FontSize(14).AutoFocus().Disabled(a.git.busy != "").
						Placeholder(locales.WorktreePlaceholder()).Label(locales.WorktreeSwitchLabel())
					if in.Shortcut(0, ui.KeyDown) {
						w.sel, w.moved = max(min(w.sel+1, len(choices)-1), 0), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						w.sel, w.moved = max(w.sel-1, 0), true
					}
					if in.Submitted() && w.sel >= 0 && w.sel < len(choices) {
						a.app.post(func() { a.chooseWorktree(choices[w.sel]) })
					}
				})
				switch {
				case len(choices) > 0:
					ui.Scroll(c.Key("worktree-results")).MaxHeight(380).Padding(6).Children(func() {
						for i, ch := range choices {
							a.worktreeRow(c, ch, i)
							if i == 0 && ch.create != "" && len(choices) > 1 {
								ui.Box(c).Height(1).Background(p.border).Margin(4, 6)
							}
						}
					})
				case w.loading:
					ui.Text(c, locales.WorktreeReading()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				default:
					ui.Text(c, locales.WorktreeNoMatches()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				}
				// A local branch that no worktree has checked out cannot be made a
				// worktree now, so the box points to checking it out.
				if name := strings.TrimSpace(w.query); !w.loading && validBranch(name) && slices.Contains(a.git.branches, name) &&
					!slices.ContainsFunc(w.trees, func(t git.Worktree) bool { return t.Branch == name }) {
					ui.Text(c, locales.WorktreeBranchExists(name)).FontSize(12).TextColor(p.faint).Padding(6, 16)
				}
				if !w.loading && w.head == "" {
					ui.Text(c, locales.WorktreeNoCommit()).FontSize(12).TextColor(p.faint).Padding(6, 16)
				}
				w.moved = false
				action := locales.QuickSwitchAction()
				if w.sel < len(choices) && choices[w.sel].create != "" {
					action = locales.QuickCreateAction()
				}
				a.app.quickFooter(c, false, action, locales.WorktreeTypeToCreate(), false, false)
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeWorktreePicker()
			}
		})
	})
}

// worktreeRow is a row of the box of the worktrees: a worktree, with the
// characters the query matched in the accent color, a check by the one
// open, its branch, the short hash of its commit and its path; or the
// worktree to create.
func (a *gitContext) worktreeRow(c *ui.Context, ch worktreeChoice, i int) {
	w := &a.app.worktrees
	p := a.app.pal
	if ch.create != "" {
		row := ui.ButtonBase(c.Key("create")).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).
			Label(locales.WorktreeCreateLabel(ch.create)).Disabled(a.git.busy != "")
		if i != w.sel && row.Hovered() {
			row.Background(p.hover)
		}
		if i == w.sel && w.moved {
			row.ScrollIntoView()
		}
		if row.Clicked() {
			a.app.post(func() { a.chooseWorktree(ch) })
		}
		row.Transition(hoverFade)
		row.Children(func() {
			a.app.pickMark(c, i == w.sel, "worktrees")
			ui.Icon(c, iconPlus).FontSize(14).TextColor(p.accent)
			ui.RichText(c, ui.Span{Text: locales.WorktreeCreatePrefix()}, ui.Span{Text: ch.create, Weight: 650}).FontSize(13).
				TextColor(p.text).SingleLine().Shrink(1)
			ui.Spacer(c)
			ui.Text(c, locales.WorktreeFrom(w.from)).FontSize(12).TextColor(p.faint).SingleLine().Shrink(0)
		})
		return
	}
	t := w.trees[w.results[ch.at].Index]
	name := worktreeName(t)
	row := ui.ButtonBase(c.Key("worktree:"+t.Path)).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).
		Label(name).Tooltip(shortenHome(t.Path)).Disabled(a.git.busy != "")
	if i != w.sel && row.Hovered() {
		row.Background(p.hover)
	}
	if i == w.sel && w.moved {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		a.app.post(func() { a.chooseWorktree(ch) })
	}
	row.Transition(hoverFade)
	row.Children(func() {
		a.app.pickMark(c, i == w.sel, "worktrees")
		icon, col := iconFolderGit, p.muted
		if t.Current {
			icon, col = iconCheck, p.accent
		}
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.RichText(c, matchSpans(name, w.results[ch.at].Positions, p)...).FontSize(13).SingleLine()
			// The branch, or the hash of a detached commit, which is not
			// shown twice.
			parts := []string{t.Branch, git.ShortHash(t.Head)}
			if t.Detached || t.Branch == "" {
				parts = []string{git.ShortHash(t.Head)}
			}
			parts = append(parts, shortenHome(t.Path))
			if t.Prunable {
				parts = append(parts, locales.WorktreeMissingHint())
			}
			if t.Locked {
				parts = append(parts, locales.WorktreeLockedHint())
			}
			ui.Text(c, strings.Join(parts, " • ")).FontSize(12).TextColor(p.faint).SingleLine()
		})
	})
}
