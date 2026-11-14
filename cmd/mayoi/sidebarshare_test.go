package main

import (
	"testing"
	"time"

	"mayoi/internal/agents"
)

// The sidebar's width, whether it is closed and the scope of the Sessions
// view follow from one project to the next; what it shows is each one's,
// but for the sessions of every folder.
func TestSidebarSharedAcrossProjects(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	a, b := hs[0].a, hs[1].a
	a.showSessions()
	a.window.SidebarWidth = 300
	step(tt)
	f.activate(hs[1])
	step(tt)
	if b.window.SidebarWidth != 300 || b.sessions.all != 0 || b.window.SidebarClosed {
		t.Fatalf("other project: width %v, all %d, closed %v; want 300, 0, open", b.window.SidebarWidth, b.sessions.all, b.window.SidebarClosed)
	}
	if b.window.SidebarView == "sessions" {
		t.Errorf("other project shows %q, want its own view", b.window.SidebarView)
	}
	f.activate(hs[0])
	step(tt)
	a.sessions.all = 1
	step(tt)
	f.activate(hs[1])
	step(tt)
	if b.sessions.all != 1 || b.window.SidebarView != "sessions" {
		t.Fatalf("other project: all %d, view %q; want 1, sessions", b.sessions.all, b.window.SidebarView)
	}
	b.window.SidebarView = ""
	step(tt)
	b.window.SidebarClosed = true
	step(tt)
	f.activate(hs[0])
	step(tt)
	if !a.window.SidebarClosed || a.window.SidebarView != "sessions" {
		t.Errorf("back: closed %v, view %q; want closed, sessions", a.window.SidebarClosed, a.window.SidebarView)
	}
}

// A session open in a terminal of another project is marked so, and
// resuming it shows that project and terminal rather than starting one.
func TestResumeShowsTerminalOfAnotherProject(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	a, b := hs[0].a, hs[1].a
	ps := []*app{a, b}
	for _, p := range ps {
		p.host.(*fakeHost).projects = ps
	}
	ses := agents.Session{Harness: agents.Claude, Root: "/h/.claude", ID: "c1", Dir: other, Updated: time.Now()}
	term := hcSession(t, b, "t1")
	term.resumed = ses.Key()
	a.sessions.sessions = []agents.Session{ses}
	a.placeLive(time.Now())
	k := liveKey(ses.Harness, ses.ID)
	if o := a.sessions.open[k]; o.owner != b || o.t != term {
		t.Fatalf("open %+v, want the terminal of the other project", o)
	}
	n := len(a.terms)
	a.resumeSession(ses)
	if got := a.host.(*fakeHost).showed; got != b || len(a.terms) != n {
		t.Fatalf("showed %v with %d terminals here; want the other project, none started", got, len(a.terms))
	}
	f.activate(hs[1])
	step(tt)
	if term.tab == nil || b.column().tab() != term.tab {
		t.Errorf("the other project shows %v, want the session's terminal", b.column().tab())
	}
}
