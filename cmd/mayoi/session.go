package main

import (
	"path/filepath"

	"mayoi/internal/settings"
)

// session is what the window shows of its folder now: its columns and
// their tabs, where each is scrolled, the panel and the explorer's
// folders open.
func (a *app) session() settings.Session {
	a.rememberTabs()
	return a.window.Session
}

// stash keeps ses as the session of the window's folder, for the next
// window to open the folder.
func (a *app) stash(ses settings.Session) {
	w := *a.window
	w.Session = ses
	a.settings.Stash(&w)
}

// openSession opens dir in the window, with the tabs it had when a
// window last closed it.
func (a *app) openSession(dir string) {
	a.openFolder(dir)
	ses := a.settings.TakeSession(dir)
	if ses == nil {
		return
	}
	// The panel and its terminals are the window's still, and the
	// folder's layout its own.
	a.window.Columns, a.window.Column, a.window.FileColumn, a.window.Expanded = ses.Columns, ses.Column, ses.FileColumn, ses.Expanded
	if ses.SidebarWidth != 0 {
		a.window.Layout = ses.Layout
	}
	a.restoreTabs()
}

// switchTo closes the window's folder, keeping its session, and opens dir
// with its own, once the documents of the folder are closed. A window
// with no folder keeps the files it has open.
func (a *app) switchTo(dir string) {
	dir = filepath.Clean(dir)
	if a.root == "" || a.root == dir {
		a.openSession(dir)
		return
	}
	ses := a.session()
	a.closeAll(func() {
		a.stash(ses)
		a.openSession(dir)
	})
}

// closeKeeping closes every document as the window closes, keeping the
// session it had before for the window's next start, then calls then
// unless one stayed open.
func (a *app) closeKeeping(then func()) {
	ses := a.session()
	a.closeAll(func() {
		a.kept = &ses
		then()
	})
}

// keepSession keeps what the window shows as it closes: what it showed
// before its documents closed, if they did.
func (a *app) keepSession() {
	if !a.started {
		// A project of the rail not shown since the app started: its
		// session is as the settings have it.
		return
	}
	if a.kept != nil {
		a.window.Session = *a.kept
		a.settings.Changed()
		return
	}
	a.rememberTabs()
}
