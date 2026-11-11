package main

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/internal/bg"
	"mayoi/locales"
)

// The Sessions view lists the sessions of the coding agents on this
// machine, Claude Code of each of its config folders, Qoder, Codex and
// Grok, the one used last first: of the window's folder, or of every
// folder. A query narrows them by title, first prompt, folder or id, and
// Enter resumes one in a terminal of its folder. With the terminal
// backend "hc", a session that runs in a terminal of herdr-core says
// what it does, and resuming it shows that terminal.

// agentScanner reads the sessions for every window, and keeps what it
// read of each file.
var agentScanner = agents.NewScanner()

// sessionsEvery is how often the sessions are read again while the view
// shows, and sessionsLiveEvery what hc says runs.
const (
	sessionsEvery     = 5 * time.Second
	sessionsLiveEvery = 3 * time.Second
)

// sessionsState is the Sessions view: its query, which folders, the
// sessions read and those that run in herdr-core.
type sessionsState struct {
	query string
	// all lists the sessions of every folder rather than the window's:
	// 1 for every folder, 0 for the window's, as a segmented control has
	// it.
	all    int
	input  ui.Handle
	list   ui.ListState
	row    int
	toList bool

	read     bg.Poller[string]
	sessions []agents.Session
	loaded   bool
	// version counts the sessions read, and rows are the sessions shown
	// as of rowsKey.
	version int
	rows    []agents.Session
	rowsKey sessionsKey

	live     bg.Poller[string]
	running  map[string]hcAgent
	liveSeen bool
}

// sessionsKey is what the sessions shown depend on.
type sessionsKey struct {
	query, root string
	all         bool
	version     int
}

// agentEnv is where the agents keep their sessions.
func (a *app) agentEnv() *agents.Env {
	home, _ := os.UserHomeDir()
	return &agents.Env{Home: home, ConfigDir: os.Getenv("CLAUDE_CONFIG_DIR"), ClaudeDirs: a.prefs().AgentSessionsClaudeDirs}
}

// readSessions reads the sessions again, in the background, once they
// are due, and what runs in herdr-core while the terminal backend is hc.
func (a *app) readSessions() {
	st := &a.sessions
	if st.read.Start("", sessionsEvery) {
		e := a.agentEnv()
		bg.Go(func() {
			ss := agentScanner.Scan(e)
			a.post(func() {
				if !st.read.Done("") {
					return
				}
				st.sessions, st.loaded = ss, true
				st.version++
				st.read.Keep("", sessionsEvery, a.invalidate)
			})
		})
	}
	if !a.hcBackend() {
		st.running, st.liveSeen = nil, false
		return
	}
	if st.live.Start("", sessionsLiveEvery) {
		roots := a.agentEnv().ClaudeRoots()
		bg.Go(func() {
			running := hcAgents(roots)
			a.post(func() {
				if !st.live.Done("") {
					return
				}
				st.running, st.liveSeen = running, true
				st.live.Keep("", sessionsLiveEvery, a.invalidate)
			})
		})
	}
}

// sessionRows are the sessions the view shows: of the window's folder or
// of every folder, that match the query.
func (a *app) sessionRows() []agents.Session {
	st := &a.sessions
	all := st.all == 1 || a.root == ""
	k := sessionsKey{query: st.query, root: a.root, all: all, version: st.version}
	if k == st.rowsKey && st.rows != nil {
		return st.rows
	}
	rows := []agents.Session{}
	for _, s := range st.sessions {
		if (all || s.In(a.root)) && s.Matches(st.query) {
			rows = append(rows, s)
		}
	}
	st.rows, st.rowsKey = rows, k
	return rows
}

// sessionsView is the Sessions view: the query, which folders, and the
// sessions.
func (a *app) sessionsView(c *ui.Context) {
	p := a.pal
	st := &a.sessions
	a.readSessions()
	if !st.loaded {
		ui.Progress(c.Key("sessions-activity"), -1).Height(2).Radius(0).Label(locales.SessionsReading())
	} else {
		ui.Box(c.Key("sessions-activity")).Height(2)
	}
	rows := a.sessionRows()
	ui.Column(c).Padding(0, 12, 6, 12).Gap(6).Children(func() {
		in := a.searchField(c, "sessions-query", &st.query, locales.SessionsFind(), &st.input, nil)
		if in.Changed() {
			st.row = 0
		}
		if in.Submitted() && len(rows) > 0 {
			a.resumeSession(rows[min(max(st.row, 0), len(rows)-1)])
		}
		if in.Shortcut(0, ui.KeyDown) && len(rows) > 0 {
			st.row = min(max(st.row, 0), len(rows)-1)
			st.toList = true
		}
		if a.root != "" {
			ui.Segmented(c.Key("sessions-scope"), &st.all, locales.SessionsThisProject(), locales.SessionsAllProjects())
		}
	})
	if len(rows) == 0 {
		a.sessionsEmpty(c)
		return
	}
	st.list.Selected = &st.row
	st.list.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		return rows[i].Key()
	}
	st.list.Label = func(i int) string {
		if i >= len(rows) {
			return ""
		}
		return rows[i].Label()
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var action func()
	now := c.Now()
	out := ui.List(c.Key("sessions"), &st.list, len(rows), func(i int) {
		a.sessionRow(c, rows[i], now, &action)
	}).Grow(1).Padding(0, 6, 8, 6)
	c.SetTheme(base)
	if st.toList && st.list.Focus(c) {
		st.toList = false
	}
	if st.list.Shortcut(c, 0, ui.KeyEscape) {
		st.input.Focus()
	}
	if action != nil {
		action()
	}
	if out.Submitted() && st.row >= 0 && st.row < len(rows) {
		// Enter, or a double click.
		a.resumeSession(rows[st.row])
	}
}

// sessionsEmpty is what the view shows with no session to show: that
// they are being read, that none matches, or that there are none, and
// what the view does.
func (a *app) sessionsEmpty(c *ui.Context) {
	p := a.pal
	st := &a.sessions
	msg := locales.SessionsNone()
	switch {
	case !st.loaded:
		msg = locales.SessionsReading()
	case st.query != "":
		msg = locales.SessionsNoMatch()
	case st.all == 1 || a.root == "":
		msg = locales.SessionsNoneAll()
	}
	ui.Column(c).Padding(10, 14, 0, 14).Gap(6).Children(func() {
		ui.Text(c, msg).FontSize(12).TextColor(p.muted)
		ui.Text(c, locales.SessionsHint()).FontSize(11.5).TextColor(p.faint)
	})
}

// sessionRow is the row of a session: what it does while it runs in
// herdr-core, its title, and its agent, folder and when it was used
// last. Its menu resumes it or copies how to.
func (a *app) sessionRow(c *ui.Context, s agents.Session, now time.Time, action *func()) {
	p := a.pal
	st := &a.sessions
	live, running := st.running[liveKey(s.Harness, s.ID)]
	rec := a.liveRecord(live)
	row := ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Height(38).Padding(0, 4, 0, 4).Tooltip(s.Dir + "\n" + s.ID)
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.SessionsResume()).Chosen() {
			*action = func() { a.resumeSession(s) }
		}
		m.Separator()
		if m.Item(locales.SessionsCopyCommand()).Chosen() {
			cmd := a.agentEnv().ResumeCommand(s)
			*action = func() { a.copyText(cmd) }
		}
		if m.Item(locales.SessionsCopyID()).Chosen() {
			*action = func() { a.copyText(s.ID) }
		}
	})
	row.Children(func() {
		// The dot's place is kept for a session that runs in none, so
		// that rows don't move as one starts or stops.
		dot := ui.Box(c.Key("dot")).Size(7, 7).Shrink(0)
		if running {
			dot.Children(func() {
				ui.Box(c.Key("on")).Size(7, 7).Radius(3.5).Background(a.progColor(rec.State)).Transition(dotPop).Tooltip(progLine(rec))
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, s.Label()).FontSize(12.5).TextColor(p.text).SingleLine().ClipX()
			ui.Row(c).Gap(5).AlignItems(ui.Center).MinWidth(0).Children(func() {
				if running {
					_, what := progParts(rec)
					ui.Text(c.Key("state"), what).FontSize(11).TextColor(a.progColor(rec.State)).SingleLine().Shrink(0).Transition(dotPop)
				}
				ui.Text(c, a.sessionMeta(s, now)).FontSize(11).TextColor(p.faint).SingleLine().ClipX().Shrink(1)
			})
		})
	})
}

// sessionMeta is what a session's row says under its title, what tells
// sessions apart first, as the narrow panel cuts its end: when it was
// used last, its agent, the config folder of Claude Code other than
// ~/.claude, by the name after .claude-, and its folder when the view
// lists every folder's.
func (a *app) sessionMeta(s agents.Session, now time.Time) string {
	parts := []string{ago(s.Updated, now), s.Harness.Name()}
	if base := filepath.Base(s.Root); s.Root != "" && base != ".claude" {
		parts = append(parts, cmp.Or(strings.TrimPrefix(base, ".claude-"), base))
	}
	if (a.sessions.all == 1 || a.root == "") && s.Dir != "" {
		parts = append(parts, filepath.Base(s.Dir))
	}
	return strings.Join(parts, " · ")
}

// liveRecord is what a session running in herdr-core does, as its
// terminal here says it, or else as hc does.
func (a *app) liveRecord(live hcAgent) terminal.StatusRecord {
	for _, t := range a.terms {
		if t.hc == live.addr && live.addr != "" {
			if r, ok := t.status.Top(); ok {
				return r
			}
		}
	}
	return terminal.StatusRecord{State: live.state, App: live.harness.App(), Progress: -1}
}

// copyText puts text on the clipboard, and says so.
func (a *app) copyText(text string) {
	a.services.WriteClipboard(text)
	a.tell(note{message: locales.CopiedText(text)})
}

// resumeSession resumes a session: the terminal of herdr-core it runs in
// shows, or a terminal of its folder starts, which runs the agent's
// command that resumes it.
func (a *app) resumeSession(s agents.Session) {
	if live, ok := a.sessions.running[liveKey(s.Harness, s.ID)]; ok {
		a.showHCTerminal(live.addr, live.cwd)
		return
	}
	if fi, err := os.Stat(s.Dir); s.Dir == "" || err != nil || !fi.IsDir() {
		a.tell(note{message: locales.SessionsDirGone(s.Dir)})
		return
	}
	cmd := a.agentEnv().ResumeCommand(s)
	if t := a.openSessionTerminal(s.Dir); t != nil {
		// The shell runs it once it starts, as typed ahead.
		t.term.Send([]byte(cmd + "\r"))
	}
}

// openSessionTerminal starts a terminal in dir where terminalLocation
// says, shows it with the keyboard in it, and returns it, nil when it
// can't start.
func (a *app) openSessionTerminal(dir string) *session {
	if a.settings.TerminalLocation == "editor" {
		s := a.startTerminal(dir)
		col := a.fileColumn()
		if !a.terminalTab(col, col.active+1, s, true) {
			return nil
		}
		return s
	}
	s := a.startPanelTerminal(dir)
	if s != nil {
		a.showTerminalView()
		a.rememberTabs()
		a.focusTerminal()
	}
	return s
}

// showHCTerminal shows the terminal of herdr-core at addr: one here that
// shows it, or a new one attached to it, in dir, where terminalLocation
// says.
func (a *app) showHCTerminal(addr, dir string) {
	for _, s := range a.terms {
		if s.hc == addr {
			a.showTerminal(s)
			return
		}
	}
	s := a.attachTerminal(addr, dir)
	if s == nil {
		return
	}
	if a.settings.TerminalLocation == "editor" {
		col := a.fileColumn()
		a.terminalTab(col, col.active+1, s, true)
		return
	}
	a.panelTerm = s
	a.showTerminalView()
	a.rememberTabs()
	a.focusTerminal()
}
