package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
	"mayoi/locales"
)

// panelTerminal is the terminal's view, which a panel shows as it shows
// the file views moved there.
const panelTerminal = settings.ViewTerminal

// panelWide is how wide a panel is before its header shows the tools of
// the view in it, and the buttons that maximize and hide it, in the row of
// its tabs.
const panelWide = 260

// viewLabel is a view's name, in the menus and the commands.
func viewLabel(v string) string {
	switch v {
	case settings.ViewExplorer:
		return locales.CmdViewExplorer()
	case settings.ViewSearch:
		return locales.CmdViewSearch()
	case settings.ViewSCM:
		return locales.CmdViewScm()
	case settings.ViewTerminal:
		return locales.TabTerminal()
	}
	return ""
}

// viewName is a view's name as its tab shows it.
func viewName(v string) string {
	switch v {
	case settings.ViewExplorer:
		return locales.SidebarTitleExplorer()
	case settings.ViewSearch:
		return locales.SidebarTitleSearch()
	case settings.ViewSCM:
		return locales.SidebarTitleScm()
	case settings.ViewTerminal:
		return locales.TabTerminal()
	}
	return viewLabel(v)
}

// viewTitle is a view's name as its tab shows it: a plugin's view takes
// the plugin's title.
func (a *app) viewTitle(v string) string {
	if id, ok := strings.CutPrefix(v, "plugin:"); ok {
		for _, s := range pluginSlots() {
			if s.id == id {
				return s.title
			}
		}
		return id
	}
	return viewName(v)
}

// viewShort is the name a narrow panel gives a view's tab.
func viewShort(v string) string {
	if v == settings.ViewSCM {
		return locales.SidebarTitleScmShort()
	}
	return viewName(v)
}

// sideLabel is a side's name, in the menus.
func sideLabel(s settings.Side) string {
	switch s {
	case settings.SideLeft:
		return locales.PanelLeft()
	case settings.SideRight:
		return locales.PanelRight()
	}
	return locales.PanelBottom()
}

// panelAt reports whether the terminal's view shows on a side.
func (a *app) panelAt(s settings.Side) bool {
	at, ok := a.viewSide(panelTerminal)
	return ok && at == s
}

// sidebarShown reports whether the panel on the layout's left shows.
func (a *app) sidebarShown() bool { return a.window.Panels.Get(settings.SideLeft).Visible() }

// panelView is the view the panel on a side shows, "" with no panel
// there.
func (a *app) panelView(s settings.Side) string { return a.window.Panels.Get(s).Shown() }

// viewSide is the side the panel that holds a view shows on, false when
// no panel holds it.
func (a *app) viewSide(v string) (settings.Side, bool) { return a.window.Panels.SideOf(v) }

// viewInPanel reports whether a panel holds a view.
func (a *app) viewInPanel(v string) bool { return a.window.Panels.Has(v) }

// showView shows a view where it is, or on the side it starts on when no
// panel has it.
func (a *app) showView(v string) {
	if a.viewInPanel(v) {
		a.showPanelView(v)
		return
	}
	a.moveView(v, settings.DefaultSide(v))
}

// showPanelView has the panel that holds a view show it, open, with the
// keyboard in it.
func (a *app) showPanelView(v string) {
	s, ok := a.viewSide(v)
	if !ok {
		return
	}
	a.window.Panels.Get(s).Activate(v)
	a.settings.Changed()
	if v == panelTerminal {
		if a.panelTerm != nil {
			a.focusTerminal()
		}
		return
	}
	// The keyboard is in the view, not in a terminal behind its tab.
	a.focus = paneNone
	a.focusView(v)
}

// viewBody is what a view shows, in the panel that holds it.
func (a *app) viewBody(c *ui.Context, v string, hovered bool) {
	if _, ok := strings.CutPrefix(v, "plugin:"); ok {
		a.pluginSidebar(c, v)
		return
	}
	if a.root == "" && v != settings.ViewTerminal {
		a.noFolder(c)
		return
	}
	switch v {
	case settings.ViewSCM:
		a.scmView(c)
	case settings.ViewSearch:
		a.searchView(c)
	default:
		a.explorerView(c, hovered)
	}
}

// viewTitleActions are a view's tools, at the end of its title's row.
func (a *app) viewTitleActions(c *ui.Context, v string, hovered bool) {
	switch v {
	case settings.ViewSCM:
		a.scmTitleActions(c)
	case settings.ViewSearch:
		a.searchTitleActions(c, hovered)
	}
}

// focusView gives the keyboard to what a view shows: its query, or its
// files.
func (a *app) focusView(v string) {
	switch v {
	case settings.ViewSearch:
		a.inFiles.input.Focus()
	case settings.ViewExplorer:
		// The tree takes the keyboard as it next builds, as VS Code's does.
		a.focusFiles = true
	}
}

// moveView moves a view to the panel on a side, which shows it.
func (a *app) moveView(v string, s settings.Side) {
	if !slices.Contains(settings.SidebarViews, v) && v != panelTerminal && !strings.HasPrefix(v, "plugin:") {
		return
	}
	w := a.window
	if at, ok := w.Panels.SideOf(v); ok && at == s {
		a.showPanelView(v)
		return
	}
	from, had := w.Panels.SideOf(v)
	w.Panels.Place(v, s)
	if had && from != s {
		// A panel the move leaves with no view shows nothing, and stops
		// taking the editors' room if it did.
		if p := w.Panels.Get(from); len(p.Views) == 0 && a.panelMax == from {
			a.panelMax = ""
		}
	}
	a.showPanelView(v)
	a.host.menuChanged()
}

// viewSideItems adds where a view shows to a menu, with a check on where
// it is.
func (a *app) viewSideItems(v string, m *ui.Menu) {
	at, _ := a.viewSide(v)
	m.Submenu(locales.PanelMoveTo(), func(m *ui.Menu) {
		for _, s := range settings.Sides {
			if m.Item(sideLabel(s)).Checked(at == s).Chosen() {
				a.post(func() { a.moveView(v, s) })
			}
		}
	})
}

// showTerminalView has the panel that holds the terminal show it, the
// bottom panel when no panel holds it.
func (a *app) showTerminalView() {
	if side, ok := a.viewSide(panelTerminal); ok {
		a.window.Panels.Get(side).Activate(panelTerminal)
		return
	}
	a.window.Panels.Place(panelTerminal, settings.SideBottom)
}

// terminalShown reports whether the terminal's view is the one a panel
// shows.
func (a *app) terminalShown() bool {
	s, ok := a.viewSide(panelTerminal)
	if !ok {
		return false
	}
	p := a.window.Panels.Get(s)
	return p.Visible() && p.Shown() == panelTerminal
}
