package main

import (
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/locales"
)

const (
	// stashListN is how many stashes are read with the status, which
	// Stashes… lists; stashMenuN how many the menu of the view lists.
	stashListN = 100
	stashMenuN = 10
)

// stashPicker is the box of the stashes, which applies, pops or drops one.
type stashPicker struct {
	repoRoot string
	open     bool
	query    string
	sel      int
	moved    bool
}

// stashLabel names a stash in a menu: its message, and its branch.
func stashLabel(s git.Stash) string {
	if s.Branch == "" {
		return s.Message
	}
	return s.Message + " · " + s.Branch
}

// canStash reports whether there is something to stash: a tracked file
// changed, or any file when untracked.
func (a *gitContext) canStash(untracked bool) bool {
	groups := a.groupEntries()
	n := len(groups[groupStaged]) + len(groups[groupChanges]) + len(groups[groupMerge])
	if untracked {
		n += len(groups[groupUntracked])
	}
	return a.git.busy == "" && n > 0
}

// askStash asks for the message of a stash of the changes, and of the
// untracked files too when untracked, then makes it. Changed files are
// saved before.
func (a *gitContext) askStash(untracked bool) {
	if !a.live() || !a.canStash(untracked) {
		return
	}
	title := locales.CmdGitStash()
	if untracked {
		title = locales.CmdGitStashUntracked()
	}
	// The repository goes in the hint: the button is named by the title.
	hint := locales.StashMessageHint()
	if repo := strings.TrimPrefix(a.repositoryQuestion(""), " · "); repo != "" {
		hint = repo + " · " + hint
	}
	defer a.bindPanelDialogs(a.git.repo)
	a.app.prompt = &prompt{title: title, hint: hint, optional: true, done: func(msg string) string {
		if a.git.busy != "" {
			return a.git.busy
		}
		a.stash(msg, untracked)
		return ""
	}}
}

func (a *gitContext) stash(msg string, untracked bool) {
	for _, d := range a.app.docs {
		if d.path != "" && d.dirty() && a.owns(d.path) {
			a.app.saveDoc(d, nil)
		}
	}
	var hash string
	a.gitDo(locales.BusyStashing(), func(r *git.Repo) error {
		var err error
		hash, err = r.Stash(msg, untracked)
		if errors.Is(err, git.ErrNothingToStash) {
			return errors.New(locales.StashNothing())
		}
		return err
	}, func() {
		a.app.tell(note{message: locales.StashMade(), label: locales.StashPop(), action: func() { a.popStash(hash) }})
	})
}

// applyStash applies the stash hash, keeping it.
func (a *gitContext) applyStash(hash string) {
	a.gitDo(locales.BusyApplyingStash(), func(r *git.Repo) error { return r.StashApply(hash) }, nil)
}

// popStash applies the stash hash and drops it, unless its changes
// conflict, when Git keeps it.
func (a *gitContext) popStash(hash string) {
	a.gitDo(locales.BusyApplyingStash(), func(r *git.Repo) error { return r.StashPop(hash) }, nil)
}

// dropStash deletes the stash s, once asked.
func (a *gitContext) dropStash(s git.Stash) {
	if !a.live() {
		return
	}
	defer a.bindPanelDialogs(a.git.repo)
	a.app.confirm = &confirm{title: a.repositoryQuestion(locales.StashDropQuestion(s.Message)), message: locales.StashDropDetail(),
		ok: locales.StashDrop(), danger: true, run: func() {
			a.gitDo(locales.BusyDroppingStash(), func(r *git.Repo) error { return r.StashDrop(s.Hash) }, nil)
		}}
}

// stashMenu lists the newest stashes, each with what can be done with it,
// and Stashes… for the rest.
func (a *gitContext) stashMenu(m *ui.Menu) {
	stashes := a.git.stashes
	for _, s := range stashes[:min(len(stashes), stashMenuN)] {
		m.Submenu(stashLabel(s), func(m *ui.Menu) {
			busy := a.git.busy != ""
			if m.Item(locales.StashApply()).Disabled(busy).Chosen() {
				a.app.post(func() { a.applyStash(s.Hash) })
			}
			if m.Item(locales.StashPop()).Disabled(busy).Chosen() {
				a.app.post(func() { a.popStash(s.Hash) })
			}
			m.Separator()
			if m.Item(locales.StashDropEllipsis()).Disabled(busy).Chosen() {
				a.app.post(func() { a.dropStash(s) })
			}
		})
	}
	m.Separator()
	if m.Item(locales.CmdGitStashes()).Chosen() {
		a.app.post(a.openStashPicker)
	}
}

// openStashPicker shows the box of the stashes.
func (a *gitContext) openStashPicker() {
	if !a.live() {
		return
	}
	if len(a.git.stashes) == 0 {
		a.app.fail("%s", locales.StashNone())
		return
	}
	a.app.stashes = stashPicker{open: true, repoRoot: a.git.repo.Root}
}

// stashChoices are the stashes whose message, branch or name has the
// words of the query.
func (a *gitContext) stashChoices() []git.Stash {
	words := strings.Fields(strings.ToLower(a.app.stashes.query))
	var out []git.Stash
	for _, s := range a.git.stashes {
		text := strings.ToLower(s.Message + " " + s.Branch + " " + s.Ref())
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(text, w)
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

// stashPickerView is the box of the stashes: ↵ applies the one chosen,
// ⌘↵ pops it and ⌘⌫ drops it, as the buttons of its row do.
func (a *app) stashPickerView(c *ui.Context) {
	pick := &a.stashes
	if !pick.open {
		return
	}
	g := a.repositoryForRoot(pick.repoRoot)
	if g.git.repo == nil || g.git.repo.Root != pick.repoRoot {
		pick.open = false
		return
	}
	stashes := g.stashChoices()
	pick.sel = min(max(pick.sel, 0), max(len(stashes)-1, 0))
	p := a.pal
	run := func(s git.Stash, do func(git.Stash)) {
		pick.open = false
		a.post(func() { do(s) })
	}
	apply := func(s git.Stash) { g.applyStash(s.Hash) }
	pop := func(s git.Stash) { g.popStash(s.Hash) }
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "stashes")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().Role(ui.RoleDialog).Label(locales.CmdGitStashes())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					in := a.textKeys(ui.TextInputBase(c.Key("stash-input"), &pick.query)).Grow(1).FontSize(14).AutoFocus().Placeholder(locales.StashFind()).Label(locales.CmdGitStashes())
					if in.Changed() {
						pick.sel = 0
					}
					if in.Shortcut(0, ui.KeyDown) {
						pick.sel = min(pick.sel+1, len(stashes)-1)
						pick.moved = true
					}
					if in.Shortcut(0, ui.KeyUp) {
						pick.sel = max(pick.sel-1, 0)
						pick.moved = true
					}
					if pick.sel < len(stashes) && g.git.busy == "" {
						s := stashes[pick.sel]
						switch {
						case in.Shortcut(ui.Cmd, ui.KeyEnter):
							run(s, pop)
						case in.Shortcut(ui.Cmd, ui.KeyBackspace):
							run(s, g.dropStash)
						case in.Submitted():
							run(s, apply)
						}
					}
				})
				ui.Scroll(c.Key("stash-results")).MaxHeight(380).Padding(6).Children(func() {
					if len(stashes) == 0 {
						ui.Text(c, locales.StashNoMatch()).FontSize(13).TextColor(p.muted).Padding(7, 10)
					}
					for i, s := range stashes {
						row := ui.ButtonBase(c.Key("stash:"+s.Hash)).FillWidth().Padding(5, 10).Gap(10).Radius(6).AlignItems(ui.Center).Label(s.Message)
						if i == pick.sel && pick.moved {
							row.ScrollIntoView()
						}
						if i != pick.sel && row.Hovered() {
							row.Background(p.hover)
						}
						row.Transition(hoverFade)
						row.Children(func() {
							a.pickMark(c, i == pick.sel, "stashes")
							ui.Icon(c, iconBox).FontSize(14).TextColor(p.muted)
							ui.Text(c, s.Message).FontSize(13).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
							ui.Text(c, s.Branch).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
							if !s.When.IsZero() {
								ui.Text(c, shortAgo(s.When, time.Now())).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0)
							}
							if g.git.busy != "" || i != pick.sel && !row.Hovered() {
								return
							}
							if a.smallButton(c, iconCheck, locales.StashApply()) {
								run(s, apply)
							}
							if a.smallButton(c, iconArrowUp, locales.StashPop()) {
								run(s, pop)
							}
							if a.smallButton(c, iconTrash, locales.StashDropEllipsis()) {
								run(s, g.dropStash)
							}
						})
						if row.Clicked() && g.git.busy == "" {
							pick.sel = i
						}
					}
				})
				a.stashFooter(c)
			})
			pick.moved = false
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				pick.open = false
			}
		})
	})
}

// stashFooter shows the keys of the box of the stashes.
func (a *app) stashFooter(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 14).Gap(14).AlignItems(ui.Center).Background(p.hover).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		hint := func(keys, label string) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				a.keycaps(c, keys)
				ui.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
			})
		}
		hint("⏎", locales.StashApply())
		hint(keyText("CmdOrCtrl+Enter"), locales.StashPop())
		hint(keyText("CmdOrCtrl+Backspace"), locales.StashDrop())
		ui.Spacer(c)
		hint("esc", locales.QuickClose())
	})
}
