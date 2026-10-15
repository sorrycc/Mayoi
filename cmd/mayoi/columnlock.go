package main

import "slices"

// fileColumn chooses where an implicit file open goes. Explicit drops and
// session restoration use openFileIn and can still target a locked column.
func (a *app) fileColumn() *column {
	col := a.column()
	if !col.locked {
		return col
	}
	if a.fileCol != nil && !a.fileCol.locked && slices.Contains(a.cols, a.fileCol) {
		return a.fileCol
	}
	for i := a.col - 1; i >= 0; i-- {
		if !a.cols[i].locked {
			a.fileCol = a.cols[i]
			return a.fileCol
		}
	}
	for _, c := range a.cols {
		if !c.locked {
			a.fileCol = c
			return c
		}
	}
	// Even when every column is locked, files have a workspace to the left.
	a.fileCol = a.splitColumn(a.cols[0], false)
	return a.fileCol
}

func (a *app) toggleColumnLock(col *column) {
	if !slices.Contains(a.cols, col) {
		return
	}
	col.locked = !col.locked
	if col.locked {
		// Ensure there is a file workspace without changing keyboard focus.
		if a.fileCol == col {
			a.fileCol = nil
		}
		a.fileColumn()
	} else if len(col.tabs) == 0 {
		a.removeColumn(col)
		a.focusColumn()
	}
	a.rememberTabs()
}

// terminalToLockedRight reuses a terminal-only rightmost column, or makes
// one there, and moves the live session without restarting its process.
func (a *app) terminalToLockedRight(s *session) {
	if !slices.Contains(a.terms, s) {
		return
	}
	to := a.cols[len(a.cols)-1]
	from, _ := a.locate(s.tab)
	terminalOnly := true
	for _, t := range to.tabs {
		terminalOnly = terminalOnly && t.term != nil
	}
	reusable := terminalOnly && (to.locked || from == to)
	if !reusable {
		to = a.splitColumn(to, true)
	}
	if !to.locked {
		a.toggleColumnLock(to)
	}
	a.terminalToColumn(s, to, len(to.tabs))
	a.rememberTabs()
}

func (a *app) moveTerminalToLockedRight() {
	switch t := a.activeTab(); {
	case a.focus == paneTerminal && a.panelTerm != nil:
		a.terminalToLockedRight(a.panelTerm)
	case t != nil && t.term != nil:
		a.terminalToLockedRight(t.term)
	case a.panelTerm != nil:
		a.terminalToLockedRight(a.panelTerm)
	}
}
