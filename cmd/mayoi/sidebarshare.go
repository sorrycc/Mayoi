package main

// sidebarShare is what of the sidebar every project of every window
// shares: how wide it is, whether it is closed, whether the Sessions
// view lists the sessions of every folder, and whether it shows them so.
// What it shows otherwise is each project's own.
type sidebarShare struct {
	width  float32
	closed bool
	all    int
	// allSessions is whether the sidebar shows the sessions of every
	// folder: the next project shows them too.
	allSessions bool
}

// sidebarShare is what the project's sidebar has of the shared state.
func (a *app) sidebarShare() sidebarShare {
	s := sidebarShare{
		width:       a.window.SidebarWidth,
		closed:      a.window.SidebarClosed,
		all:         a.sessions.all,
		allSessions: a.window.SidebarView == "sessions" && a.sessions.all == 1,
	}
	// A project that lists no sessions passes on what it was given: it
	// can't show them, but the next one can.
	if !a.hasSessions() {
		s.allSessions = a.shared.allSessions && a.sessions.all == 1
	}
	return s
}

// setSidebarShare gives the project's sidebar the shared state. It shows
// the sessions of every folder as another did, but for a remote folder,
// and keeps its own view otherwise.
func (a *app) setSidebarShare(s sidebarShare) {
	if s.width != 0 {
		a.window.SidebarWidth = s.width
	}
	a.window.SidebarClosed, a.sessions.all = s.closed, s.all
	if s.allSessions && a.hasSessions() {
		a.window.SidebarView = "sessions"
	}
}

// shareSidebar shares what a project changed of its sidebar since it
// last shared, as it builds, with every other project, or gives it what
// another changed. The first project to share sets the state.
func (m *mayoi) shareSidebar(a *app) {
	cur := a.sidebarShare()
	switch {
	case !m.sidebarSeen:
		m.sidebar, m.sidebarSeen = cur, true
	case a.sharedSeen && cur != a.shared:
		was := m.sidebar
		m.sidebar = cur
		for _, h := range m.wins {
			if h.a != a {
				h.a.invalidate()
			}
		}
		// The width changes with every step of a drag: the settings keep
		// it with the next change.
		if was.closed != cur.closed && m.s != nil {
			m.s.Changed()
		}
	case cur != m.sidebar:
		a.setSidebarShare(m.sidebar)
	}
	a.shared, a.sharedSeen = m.sidebar, true
}
