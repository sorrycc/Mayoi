package main

// sidebarShare is what of the sidebar every project of every window
// shares: how wide it is, whether it is closed, and whether the Sessions
// view lists the sessions of every folder. What it shows is each
// project's own.
type sidebarShare struct {
	width  float32
	closed bool
	all    int
}

// sidebarShare is what the project's sidebar has of the shared state.
func (a *app) sidebarShare() sidebarShare {
	return sidebarShare{width: a.window.SidebarWidth, closed: a.window.SidebarClosed, all: a.sessions.all}
}

// setSidebarShare gives the project's sidebar the shared state.
func (a *app) setSidebarShare(s sidebarShare) {
	if s.width != 0 {
		a.window.SidebarWidth = s.width
	}
	a.window.SidebarClosed, a.sessions.all = s.closed, s.all
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
