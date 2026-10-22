package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
	"mayoi/locales"
)

type repositoryRowKind int

const (
	repositoryHeader repositoryRowKind = iota
	repositoryCommit
	repositoryClean
	repositoryEntry
)

// A shared list owns scrolling; each row retains its repository's selection,
// draft and command context. Keys include the root, including for equal paths.
type repositoryChangeRow struct {
	repo *gitContext
	kind repositoryRowKind
	at   int
	key  string
}

func (a *gitContext) changesExpanded() bool {
	if a.expanded != nil {
		return *a.expanded
	}
	return a.hasCommitControls()
}

func (a *app) repositoryChangeRows(contexts []*gitContext) []repositoryChangeRow {
	var rows []repositoryChangeRow
	for _, repo := range contexts {
		root := repo.git.repo.Root
		rows = append(rows, repositoryChangeRow{repo: repo, kind: repositoryHeader, key: root + ":header"})
		if !repo.changesExpanded() {
			continue
		}
		view := repo.changesContext()
		rows = append(rows, repositoryChangeRow{repo: view, kind: repositoryCommit, key: root + ":input"})
		changes := view.scmRows()
		if len(changes) == 0 && repo.git.status != nil {
			rows = append(rows, repositoryChangeRow{repo: view, kind: repositoryClean, key: root + ":clean"})
		}
		for i, r := range changes {
			rows = append(rows, repositoryChangeRow{repo: view, kind: repositoryEntry, at: i, key: root + ":" + r.key})
		}
	}
	return rows
}

func (a *app) scmView(c *ui.Context) {
	contexts := a.rootRepositoryList()
	if contexts[0].git.repo == nil && len(contexts) > 1 {
		contexts = contexts[1:]
	}
	if len(contexts) == 1 {
		contexts[0].scm.section = scmFull
		contexts[0].scmView(c)
		return
	}
	ctx := a.repository()
	ctx.scm.section = scmHistory
	body := ui.Column(c.Key("repositories")).Grow(1).MinHeight(0)
	body.Children(func() {
		height := body.Bounds().H
		if height <= 0 {
			_, height = c.Size()
			height = max(height-100, 1)
		}
		ratio := a.window.ScmChangesRatio
		if ratio <= 0 {
			ratio = 0.5
		}
		// Keep both panes reachable when a saved layout opens in a smaller window.
		minimum := min(float32(100), height/3)
		changesHeight := clamp(height*ratio, minimum, height-minimum)
		ui.Column(c.Key("repository-changes-pane")).Height(changesHeight).Shrink(0).MinHeight(0).Children(func() {
			ui.Text(c, strings.ToUpper(locales.GroupChanges())).FontSize(11).FontWeight(600).
				LetterSpacing(0.4).TextColor(a.pal.muted).Height(24).Padding(4, 10).Shrink(0)
			a.repositoryChanges(c, contexts)
		})
		ui.Column(c.Key("history:"+ctx.git.repo.Root)).Height(0).Grow(1).MinHeight(0).
			BorderWidth(1, 0, 0, 0).BorderColor(a.pal.border).Children(func() {
			ctx.scmList(c)
			// This sits on the history's top edge and does not move with either list.
			a.splitter(c, edgeTop, func(delta float32) {
				a.window.ScmChangesRatio = clamp((changesHeight+delta)/height, minimum/height, 1-minimum/height)
			}).Label(locales.ScmResizeChanges())
		})
	})
}

func (a *app) repositoryChanges(c *ui.Context, contexts []*gitContext) {
	rows := a.repositoryChangeRows(contexts)
	state := &a.repositories
	if state.cursor != "" {
		for i, row := range rows {
			if row.key == state.cursor {
				state.row = i
				break
			}
		}
		state.cursor = ""
	}
	state.changes.Selected = &state.row
	state.changes.Key = func(i int) any {
		if i < 0 || i >= len(rows) {
			return i
		}
		return rows[i].key
	}
	state.changes.Header = func(i int) bool { return i >= 0 && i < len(rows) && rows[i].kind == repositoryHeader }
	state.changes.Label = func(i int) string {
		if i < 0 || i >= len(rows) {
			return ""
		}
		r := rows[i]
		if r.kind == repositoryEntry {
			row := r.repo.scmRows()[r.at]
			if row.header {
				return groupName(row.group)
			}
			if row.dir != "" {
				return row.name
			}
			return filepath.Base(row.e.Path)
		}
		return r.repo.label()
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = a.pal.chrome, a.pal.text, a.pal.hover
	c.SetTheme(&quiet)
	headers := make(map[int]ui.Rect)
	entries := make(map[int]ui.Rect)
	out := ui.List(c.Key("repository-changes"), &state.changes, len(rows), func(i int) {
		theme := c.Theme()
		c.SetTheme(base)
		defer c.SetTheme(theme)
		r := rows[i]
		switch r.kind {
		case repositoryHeader:
			head := ui.Box(c).FillWidth().Height(28).Children(func() { r.repo.repositoryHeader(c) })
			headers[i] = head.Bounds()
		case repositoryCommit:
			r.repo.scmActivity(c)
			r.repo.scmCommitControls(c)
		case repositoryClean:
			ui.Text(c, locales.DiffNoChanges()).FontSize(12).TextColor(a.pal.muted).Padding(4, 14, 12, 14)
		case repositoryEntry:
			var rowAction func()
			row := r.repo.scmRows()[r.at]
			box := ui.Box(c).FillWidth().Height(22).Padding(0, 6).Children(func() {
				switch {
				case row.header:
					r.repo.scmHeader(c, row, &rowAction)
				case row.dir != "":
					r.repo.scmFolder(c, row, &rowAction)
				default:
					r.repo.scmFile(c, row, &rowAction)
				}
			})
			entries[i] = box.Bounds()
			if row.header {
				headers[i] = box.Bounds()
			}
			if rowAction != nil {
				a.post(func() {
					rowAction()
					state.row = i
				})
			}
		}
	}).Grow(1).MinHeight(0)
	c.SetTheme(base)
	a.repositoryGroupHeader(c, out, rows, headers)
	if state.row < 0 || state.row >= len(rows) || rows[state.row].kind != repositoryEntry {
		return
	}
	r := rows[state.row]
	// The shared cursor follows its key when a refresh inserts rows above it.
	// Keep the repository cursor in sync even when its choice did not change.
	r.repo.scm.row = r.at
	reveal := func(i int) {
		// The list already accounts for the repository header. Leave one
		// additional row above the choice when the floating group covers it.
		if box := entries[i]; box.H > 0 && box.Y < out.Bounds().Y+28+22 {
			i = max(i-1, 0)
		}
		state.changes.ScrollIntoView(i)
	}
	if out.Changed() && r.repo.scmRows()[r.at].file() {
		if box := entries[state.row]; box.H > 0 && box.Y < out.Bounds().Y+28+22 {
			reveal(state.row)
		}
	}
	r.repo.scmListInput(out, r.repo.scmRows(), func(at int) {
		for i, row := range rows {
			if row.repo.git == r.repo.git && row.kind == repositoryEntry && row.at == at {
				state.row = i
				reveal(i)
				break
			}
		}
	})
}

// The list pins repositories. A second, clipped layer pins the current change
// group beneath its repository, until the next group or repository pushes it
// away. It reuses the group's controls and repository-bound command context.
func (a *app) repositoryGroupHeader(c *ui.Context, list ui.Element, rows []repositoryChangeRow, headers map[int]ui.Rect) {
	first, _ := a.repositories.changes.Visible()
	viewport := list.Bounds()
	if first < 0 || first >= len(rows) || viewport.H <= 0 {
		return
	}
	repo := rows[first].repo.git
	group := -1
	for i := first; i >= 0 && rows[i].repo.git == repo; i-- {
		if r := rows[i]; r.kind == repositoryEntry && r.repo.scmRows()[r.at].header {
			group = i
			break
		}
	}
	top := viewport.Y + 28
	// A group can reach the pinned repository while the preceding row is
	// still partially visible behind it. Include headers beneath that row.
	for i, bounds := range headers {
		if i <= first || bounds.H <= 0 || bounds.Y > top || rows[i].repo.git != repo {
			continue
		}
		if r := rows[i]; r.kind == repositoryEntry && r.repo.scmRows()[r.at].header {
			group = max(group, i)
		}
	}
	if group < 0 {
		return
	}
	offset := float32(0)
	for i, bounds := range headers {
		if i > group && bounds.H > 0 {
			offset = min(offset, bounds.Y-top-22)
		}
	}
	if offset <= -22 {
		return
	}
	r := rows[group]
	row := r.repo.scmRows()[r.at]
	list.Children(func() {
		ui.Box(c.Key("pinned-group:" + r.key)).Absolute().Top(28).Left(0).Right(0).Height(22).Clip().
			Role(ui.RoleGroup).Label(r.repo.label() + " · " + groupName(row.group)).Children(func() {
			var action func()
			ui.Box(c).Absolute().Top(offset).Left(0).Right(0).Height(22).Padding(0, 6).Children(func() {
				r.repo.scmHeader(c, row, &action)
			})
			if action != nil {
				a.post(action)
			}
		})
	})
}

// The arrow changes expansion; the name selects history. Branch and remote
// actions bind to this header's repository regardless of the history selection.
func (a *gitContext) repositoryHeader(c *ui.Context) {
	p := a.app.pal
	root := a.git.repo.Root
	name := a.label()
	selected := a.app.repositories.selected == root
	head := ui.Row(c.Key("repository-header")).FillWidth().Height(28).Padding(0, 6).Gap(2).
		AlignItems(ui.Center).Background(p.chrome)
	if selected {
		head.Background(p.accent.Alpha(0.05).Over(p.chrome))
	} else if head.Hovered() {
		head.Background(p.hover)
	}
	head.Children(func() {
		if selected {
			ui.Box(c.Key("selected-repository")).Absolute().Left(0).Top(5).Bottom(5).
				Width(2).Radius(1).Background(p.accent)
		}
		arrow := ui.ButtonBase(c.Key("expand")).Size(20, 24).Label(locales.ScmToggleRepository(name))
		arrow.Children(func() { chevron(c, a.changesExpanded()).FontSize(12).TextColor(p.muted) })
		if arrow.Clicked() {
			open := !a.changesExpanded()
			a.expanded = &open
		}
		selectRepo := ui.ButtonBase(c.Key("select")).Height(24).Grow(1).Shrink(1).MinWidth(60).Gap(4).
			AlignItems(ui.Center).Justify(ui.Start).Label(locales.ScmSelectRepository(name)).Tooltip(root)
		selectRepo.Children(func() {
			ui.Text(c, name).FontSize(12).FontWeight(600).TextColor(p.text).SingleLine().Shrink(1)
			if dir := filepath.Dir(a.relativeRoot()); dir != "." && a.app.window.SidebarWidth >= 300 {
				ui.Text(c, filepath.ToSlash(dir)).FontSize(10.5).TextColor(p.faint).SingleLine().Shrink(100)
			}
		})
		if selectRepo.Clicked() {
			a.app.post(func() { a.app.selectRepository(root) })
		}
		a.scmBranchButton(c, true)
		// The menu keeps every remote action available in a narrow sidebar.
		if a.app.window.SidebarWidth >= 360 {
			a.scmActions(c)
		}
		ui.ButtonBase(c.Key("more")).Size(20, 24).Label(locales.ScmMoreActions()).
			Children(func() { ui.Icon(c, iconMore).FontSize(13).TextColor(p.muted) }).Menu(a.scmMenu)
	})
}

func submoduleDescription(e git.Entry, staged bool) string {
	var state []string
	if staged || len(e.Sub) == 4 && e.Sub[1] == 'C' {
		state = append(state, locales.ScmSubmoduleCommit())
	}
	if !staged && e.Dirty() {
		state = append(state, locales.ScmSubmoduleDirty())
	}
	return strings.Join(state, " · ")
}
