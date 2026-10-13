package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/workspace"
)

// recentPicker is the box of the recent folders, found by path: but the
// window's, and those not on the disk.
type recentPicker struct {
	open  bool
	query string
	sel   int
	moved bool
	// dirs are the folders as the box opened, names their paths shown,
	// and results those the query matches.
	dirs    []string
	names   []string
	results []workspace.Match
	shown   string // the query the results are of
	stale   bool
}

// openRecentPicker shows the box of the recent folders.
func (a *app) openRecentPicker() {
	r := &a.recents
	*r = recentPicker{open: true, stale: true}
	r.dirs = a.settings.RecentExcept(a.root)
	r.names = make([]string, len(r.dirs))
	for i, dir := range r.dirs {
		r.names[i] = shortenHome(dir)
	}
}

func (a *app) closeRecentPicker() {
	a.recents.open = false
	if a.focus != paneTerminal {
		a.focusColumn()
	}
}

// chooseRecent opens the folder of a row in the window, or with newWin in
// a window of its own.
func (a *app) chooseRecent(i int, newWin bool) {
	r := &a.recents
	if i < 0 || i >= len(r.results) {
		return
	}
	dir := r.dirs[r.results[i].Index]
	a.closeRecentPicker()
	if newWin {
		a.host.newWindow(dir)
	} else {
		a.switchFolder(dir)
	}
}

// recentPickerView shows the box of the recent folders while it is open.
func (a *app) recentPickerView(c *ui.Context) {
	r := &a.recents
	if !r.open {
		return
	}
	p := a.pal
	if r.stale || r.shown != r.query {
		r.results = workspace.Fuzzy(r.names, strings.TrimSpace(r.query), 60)
		if strings.TrimSpace(r.query) == "" {
			// Every folder, the one opened last first.
			r.results = r.results[:0]
			for i := range r.names {
				r.results = append(r.results, workspace.Match{Index: i})
			}
		}
		r.shown, r.stale, r.sel = r.query, false, 0
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "recent")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label("Open Recent")
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconFolderOpen).FontSize(15).TextColor(p.muted)
					in := a.textKeys(ui.TextInputBase(c.Key("recent-input"), &r.query)).Grow(1).FontSize(14).AutoFocus().
						Placeholder("Open a recent folder").Label("Open Recent")
					if in.Shortcut(0, ui.KeyDown) {
						r.sel, r.moved = min(r.sel+1, len(r.results)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						r.sel, r.moved = max(r.sel-1, 0), true
					}
					sel := r.sel
					switch {
					case in.Shortcut(ui.Cmd, ui.KeyEnter):
						a.post(func() { a.chooseRecent(sel, true) })
					case in.Submitted():
						a.post(func() { a.chooseRecent(sel, false) })
					}
				})
				if len(r.results) > 0 {
					ui.Scroll(c.Key("recent-results")).MaxHeight(380).Padding(6).Children(func() {
						for i := range r.results {
							a.recentRow(c, i)
						}
					})
				} else {
					msg := "No matching folders"
					if len(r.dirs) == 0 {
						msg = "No recent folders"
					}
					ui.Text(c, msg).FontSize(13).TextColor(p.muted).Padding(12, 16)
				}
				r.moved = false
				a.quickFooter(c, false, "open", keyText("CmdOrCtrl+Enter")+" in a new window", false)
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeRecentPicker()
			}
		})
	})
}

// recentRow is a row of the box of the recent folders: its name and the
// folder it is in, with the characters the query matched in the accent
// color.
func (a *app) recentRow(c *ui.Context, i int) {
	r := &a.recents
	p := a.pal
	m := r.results[i]
	name := r.names[m.Index]
	row := ui.ButtonBase(c.Key("recent:"+name)).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).
		Label("Open " + r.dirs[m.Index])
	switch {
	case i == r.sel:
		row.Background(p.active)
	case row.Hovered():
		row.Background(p.hover)
	}
	if i == r.sel && r.moved {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		newWin := row.ClickModifiers()&ui.Cmd != 0
		a.post(func() { a.chooseRecent(i, newWin) })
	}
	row.Children(func() {
		ui.Icon(c, iconFolder).FontSize(15).TextColor(p.accent)
		base := strings.LastIndexByte(name, '/') + 1
		ui.RichText(c, a.matchSpans(name[base:], base, m.Positions, p.text)...).FontSize(13).SingleLine().Shrink(0)
		if base > 1 {
			ui.RichText(c, a.matchSpans(name[:base-1], 0, m.Positions, p.faint)...).FontSize(12).SingleLine().Shrink(1)
		}
	})
}
