package main

import (
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// findTerminal is the terminal find is for: the one with the keyboard, of
// the panel or of a tab, nil for none.
func (a *app) findTerminal() *session {
	switch t := a.activeTab(); {
	case a.focus == paneTerminal:
		return a.panelTerm
	case a.focus == paneEditor && t != nil && t.term != nil:
		return t.term
	}
	return nil
}

// openTermFind opens the find bar of a terminal, its query selected so
// typing replaces it.
func (a *app) openTermFind(s *session) {
	s.findOpen = true
	s.findInput.SelectAll()
}

// termFindStep goes to the match of a terminal's search by after, opening
// its find bar: on toward older output, up the screen, as Return does in
// Ghostty's find, and back toward newer output.
func (a *app) termFindStep(s *session, by int) {
	if !s.findOpen {
		a.openTermFind(s)
	}
	if by > 0 {
		s.term.SearchNext()
	} else {
		s.term.SearchPrevious()
	}
	s.findBack = by < 0
}

// closeTermFind closes the find bar of a terminal, ends its search, and
// gives the keyboard back to the terminal.
func (a *app) closeTermFind(s *session) {
	s.findOpen = false
	s.term.EndSearch()
	s.view.Focus()
}

// termFindView is the find bar of a terminal, over its top right. The
// terminal's search matches exactly but for the case of ASCII letters, so
// it has none of the editor's options.
func (a *app) termFindView(c *ui.Context, s *session) {
	if !s.findOpen {
		return
	}
	p, st := a.pal, s.found
	// A terminal that took the place of one that ended searches again.
	if s.term.SearchStatus().Text != s.findQuery {
		s.term.Search(s.findQuery)
	}
	countW := a.findCountWidth(c)
	box := a.floating(c, ui.Row(c.Key("term-find")).Absolute().Top(4).Right(22).Width(330).Padding(5, 6).Gap(4).
		AlignItems(ui.Center).Radius(8), false).Transition(popIn)
	shake(c, box, s.findShook)
	box.Role(ui.RoleToolbar).Label(locales.FindLabel())
	count, none := locales.FindNoResults(), true
	switch {
	case s.findQuery == "", st.Text != s.findQuery:
		count, none = "", false
	case st.Total > 0:
		count, none = locales.FindCount(st.Selected, st.Total), false
	case st.Searching:
		count, none = "", false
	}
	box.Children(func() {
		field := ui.Row(c).Grow(1).Height(26).Padding(0, 8).AlignItems(ui.Center).Radius(5).
			Background(p.editor).Border(1, p.border).Transition(fieldFocus)
		field.Children(func() {
			in := a.textKeys(ui.TextInputBase(c.Key("term-find-query"), &s.findQuery)).Bind(&s.findInput).Grow(1).FontSize(12.5).
				Placeholder(locales.FindLabel()).Label(locales.FindLabel())
			if in.Focused() {
				field.Border(1, p.accentDim)
			}
			if in.Changed() {
				s.term.Search(s.findQuery)
			}
			if in.Submitted() {
				if none {
					s.findShook = c.Now()
				}
				a.termFindStep(s, enterStep(in))
			}
		})
		col := p.muted
		if none {
			col = p.dangerFg
		}
		ui.Text(c, count).FontSize(findCountSize).TextColor(col).Width(countW).SingleLine().TextAlign(ui.Center).
			ContentTransition(ui.ContentTransition{Numeric: true, Down: s.findBack})
		// Older output is up the screen.
		if a.iconButton(c, iconArrowUp, locales.FindNextMatch(), false).Disabled(st.Total == 0).Clicked() {
			a.termFindStep(s, 1)
		}
		if a.iconButton(c, iconArrowDown, locales.FindPrevMatch(keyText("Shift+Enter")), false).Disabled(st.Total == 0).Clicked() {
			a.termFindStep(s, -1)
		}
		if a.iconButton(c, iconX, locales.CloseEsc(), false).Clicked() {
			a.closeTermFind(s)
		}
	})
	// Only from the bar: Escape in the terminal is the program's.
	if box.Shortcut(0, ui.KeyEscape) {
		a.closeTermFind(s)
	}
}
