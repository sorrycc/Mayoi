package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

type scmSection int

const (
	scmFull scmSection = iota
	scmChanges
	scmHistory
)

type repositoryPicker struct {
	open  bool
	query string
	sel   int
	moved bool
}

func (a *app) selectRepository(root string) {
	if root != "" && a.repositoryForRoot(root).git.repo == nil {
		return
	}
	a.repositories.selected = root
	a.repositories.picker.open = false
	a.invalidate()
}

func (a *app) openRepositoryPicker() {
	a.repositories.picker = repositoryPicker{open: true}
}

func (a *app) repositoryLabel() string {
	label := a.repository().label()
	if a.repositories.selected == "" {
		label += " · " + locales.RepositoryAutomatic()
	}
	return label
}

// The picker controls history and window Git commands. Commit boxes in the
// changes sections always retain the repository named by their own header.
func (a *app) repositoryButton(c *ui.Context, status bool) bool {
	ctx := a.repository()
	if ctx.git.repo == nil || len(a.repositories.contexts) < 2 {
		return false
	}
	label := a.repositoryLabel()
	tip := locales.RepositorySelect()
	b := ui.ButtonBase(c.Key("repository-picker")).Height(20).Padding(0, 5).Gap(4).Radius(4).AlignItems(ui.Center).Label(tip).Tooltip(tip)
	fg := a.pal.muted
	if status {
		b.Label(tip + " " + ctx.label())
		fg = a.pal.statusFg
	}
	if b.Hovered() {
		b.Background(a.pal.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconFolder).FontSize(12).TextColor(fg)
		ui.Text(c, label).FontSize(11).TextColor(fg).SingleLine().Shrink(1)
	})
	if b.Clicked() {
		a.post(a.openRepositoryPicker)
	}
	return b.Clicked()
}

func (a *app) repositoryChoices() []string {
	// Either slash finds a repository, whichever its root is spelled with.
	query := filepath.ToSlash(strings.ToLower(strings.TrimSpace(a.repositories.picker.query)))
	var roots []string
	if query == "" || strings.Contains(strings.ToLower(locales.RepositoryAutomatic()), query) {
		roots = append(roots, "")
	}
	for _, ctx := range a.rootRepositoryList() {
		if ctx.git.repo != nil && (query == "" || strings.Contains(filepath.ToSlash(strings.ToLower(ctx.git.repo.Root)), query)) {
			roots = append(roots, ctx.git.repo.Root)
		}
	}
	return roots
}

func (a *app) repositoryPickerView(c *ui.Context) {
	pick := &a.repositories.picker
	if !pick.open {
		return
	}
	roots := a.repositoryChoices()
	pick.sel = min(max(pick.sel, 0), max(len(roots)-1, 0))
	p := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "repositories")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().Role(ui.RoleDialog).Label(locales.RepositorySelect())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					in := a.textKeys(ui.TextInputBase(c.Key("repository-input"), &pick.query)).Grow(1).FontSize(14).AutoFocus().Placeholder(locales.RepositorySelect()).Label(locales.RepositorySelect())
					if in.Changed() {
						pick.sel = 0
					}
					if in.Shortcut(0, ui.KeyDown) {
						pick.sel = min(pick.sel+1, len(roots)-1)
						pick.moved = true
					}
					if in.Shortcut(0, ui.KeyUp) {
						pick.sel = max(pick.sel-1, 0)
						pick.moved = true
					}
					if in.Submitted() && pick.sel >= 0 && pick.sel < len(roots) {
						root := roots[pick.sel]
						a.post(func() { a.selectRepository(root) })
					}
				})
				ui.Scroll(c.Key("repository-results")).MaxHeight(380).Padding(6).Children(func() {
					for i, root := range roots {
						label, detail := locales.RepositoryAutomatic(), locales.RepositoryFollowEditor()
						if root != "" {
							label = filepath.Base(root)
							detail = root
						}
						b := ui.ButtonBase(c.Key("repository:"+root)).FillWidth().Padding(7, 10).Radius(5).Label(label + " " + detail)
						if i == pick.sel && pick.moved {
							b.ScrollIntoView()
						}
						if i == pick.sel || b.Hovered() {
							b.Background(p.hover)
						}
						b.Transition(hoverFade)
						b.Children(func() {
							ui.Column(c).FillWidth().Gap(3).Children(func() {
								ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
									ui.Icon(c, iconFolder).FontSize(13).TextColor(p.muted)
									ui.Text(c, label).FontSize(13).TextColor(p.text).SingleLine()
									if a.repositories.selected == root {
										ui.Icon(c, iconCheck).FontSize(12).TextColor(p.accent)
									}
								})
								ui.Text(c, detail).FontSize(11).TextColor(p.muted).SingleLine().Shrink(1)
							})
						})
						if b.Clicked() {
							a.post(func() { a.selectRepository(root) })
						}
					}
				})
			})
			pick.moved = false
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				pick.open = false
			}
		})
	})
}

// changesContext keeps file selection separate from the history/panel state.
// The canonical scmState is always the one used by panel commands and reads.
func (a *gitContext) changesContext() *gitContext {
	if len(a.app.repositories.contexts) < 2 {
		return a
	}
	if a.changesUI == nil {
		a.changesUI = &scmState{section: scmChanges}
	}
	view := *a
	view.scm = a.changesUI
	return &view
}

func (a *gitContext) hasCommitControls() bool {
	return a.changeCount() > 0 || a.git.amend || a.git.writing != nil || a.git.status != nil && a.git.status.Op != ""
}
