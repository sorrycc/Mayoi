package main

import (
	"cmp"
	"os"
	"path/filepath"
	"strconv"
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
	// lines are the rows under the heads of their days, as of linesKey
	// and the day linesDay.
	lines    []sessionLine
	linesKey sessionsKey
	linesDay time.Time
	// skeleton is set while the view shows the shapes of rows, as the
	// sessions are read, and shownAt when the rows took their place.
	skeleton bool
	shownAt  time.Time

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

// sessionDay is how long ago a session was used last, by the days of
// the calendar, as the view heads its rows.
type sessionDay int

const (
	sessionsToday sessionDay = iota
	sessionsYesterday
	sessionsWeek
	sessionsOlder
)

// name is the head of the rows of the day.
func (d sessionDay) name() string {
	switch d {
	case sessionsToday:
		return locales.SessionsToday()
	case sessionsYesterday:
		return locales.SessionsYesterday()
	case sessionsWeek:
		return locales.SessionsPreviousWeek()
	}
	return locales.SessionsOlder()
}

// dayOf is the day of a session used last at t, as of today, the
// midnight that started it.
func dayOf(t, today time.Time) sessionDay {
	switch {
	case !t.Before(today):
		return sessionsToday
	case !t.Before(today.AddDate(0, 0, -1)):
		return sessionsYesterday
	case !t.Before(today.AddDate(0, 0, -6)):
		return sessionsWeek
	}
	return sessionsOlder
}

// sessionLine is a row of the list: the head of a day, with how many
// sessions it has, or a session.
type sessionLine struct {
	head  bool
	day   sessionDay
	count int
	s     agents.Session
}

// key is the line's in the list.
func (l sessionLine) key() string {
	if l.head {
		return "\x01" + strconv.Itoa(int(l.day))
	}
	return l.s.Key()
}

// sessionLines are rows under the heads of the days they were used last,
// as of now.
func (a *app) sessionLines(rows []agents.Session, now time.Time) []sessionLine {
	st := &a.sessions
	y, m, d := now.Local().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Local().Location())
	if st.lines != nil && st.linesKey == st.rowsKey && st.linesDay.Equal(today) {
		return st.lines
	}
	lines := make([]sessionLine, 0, len(rows)+4)
	head := -1
	for _, s := range rows {
		day := dayOf(s.Updated, today)
		if head < 0 || lines[head].day != day {
			head = len(lines)
			lines = append(lines, sessionLine{head: true, day: day})
		}
		lines[head].count++
		lines = append(lines, sessionLine{day: day, s: s})
	}
	st.lines, st.linesKey, st.linesDay = lines, st.rowsKey, today
	return lines
}

// sessionAt is the session row of lines nearest i, at or after it, else
// before it, and -1 for none.
func sessionAt(lines []sessionLine, i int) int {
	i = min(max(i, 0), len(lines)-1)
	for j := i; j < len(lines); j++ {
		if !lines[j].head {
			return j
		}
	}
	for j := i - 1; j >= 0; j-- {
		if !lines[j].head {
			return j
		}
	}
	return -1
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
	now := c.Now()
	rows := a.sessionLines(a.sessionRows(), now)
	ui.Column(c).Padding(0, 12, 6, 12).Gap(6).Children(func() {
		in := a.searchField(c, "sessions-query", &st.query, locales.SessionsFind(), &st.input, nil)
		if in.Changed() {
			st.row = 0
		}
		if in.Submitted() && len(rows) > 0 {
			if i := sessionAt(rows, st.row); i >= 0 {
				a.resumeSession(rows[i].s)
			}
		}
		if in.Shortcut(0, ui.KeyDown) && len(rows) > 0 {
			st.row = sessionAt(rows, st.row)
			st.toList = true
		}
		if a.root != "" {
			ui.Segmented(c.Key("sessions-scope"), &st.all, locales.SessionsThisProject(), locales.SessionsAllProjects())
		}
	})
	if !st.loaded {
		a.sessionsSkeleton(c)
		return
	}
	if len(rows) == 0 {
		a.sessionsEmpty(c)
		return
	}
	if st.skeleton {
		// The rows take the place of their shapes, and fade in over them.
		st.skeleton, st.shownAt = false, now
	}
	// The choice is a session's, what Enter in the query resumes.
	if st.row >= 0 && st.row < len(rows) && rows[st.row].head {
		st.row = sessionAt(rows, st.row)
	}
	st.list.Selected = &st.row
	st.list.Header = func(i int) bool { return i < len(rows) && rows[i].head }
	st.list.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		return rows[i].key()
	}
	st.list.Label = func(i int) string {
		if i >= len(rows) || rows[i].head {
			return ""
		}
		return rows[i].s.Label()
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var action func()
	dots := a.hcBackend()
	out := ui.List(c.Key("sessions"), &st.list, len(rows), func(i int) {
		if r := rows[i]; r.head {
			a.sessionsHead(c, r)
		} else {
			a.sessionRow(c, r.s, now, dots, &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6)
	if since := now.Sub(st.shownAt); since < motionShow {
		out.Opacity(ui.EaseOut(float32(since) / float32(motionShow)))
		c.AnimationFrame()
	}
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
	if out.Submitted() && st.row >= 0 && st.row < len(rows) && !rows[st.row].head {
		// Enter, or a double click.
		a.resumeSession(rows[st.row].s)
	}
}

// sessionsHeadH is the height of the head of a day, and sessionRowH of a
// session's row.
const (
	sessionsHeadH = 26
	sessionRowH   = 40
)

// sessionsHead heads the sessions of a day, with how many there are. It
// stays at the top as they scroll under it.
func (a *app) sessionsHead(c *ui.Context, r sessionLine) {
	p := a.pal
	ui.Row(c).Grow(1).Height(sessionsHeadH).Padding(4, 6, 0, 6).AlignItems(ui.Center).Gap(6).Background(p.chrome).Children(func() {
		size, spacing := p.titleLook(10.5, 0.4)
		ui.Text(c, p.title(r.day.name())).FontSize(size).FontWeight(600).LetterSpacing(spacing).
			TextColor(p.muted).SingleLine().Shrink(1)
		ui.Spacer(c)
		ui.Text(c, strconv.Itoa(r.count)).FontSize(10.5).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0)
	})
}

// sessionsSkeleton is what the view shows while it reads the sessions
// the first time: the shapes of rows, where the rows will be, so that
// nothing moves as they come.
func (a *app) sessionsSkeleton(c *ui.Context) {
	p := a.pal
	a.sessions.skeleton = true
	bar := p.text.Alpha(0.07)
	ui.Column(c.Key("sessions-skeleton")).Padding(0, 6, 8, 6).Grow(1).Children(func() {
		ui.Row(c).Height(sessionsHeadH).Padding(4, 6, 0, 6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(44, 7).Radius(3.5).Background(bar)
		})
		for i, w := range []float32{0.78, 0.6, 0.86, 0.52, 0.7} {
			ui.Row(c.Key(i)).Height(sessionRowH).Padding(0, 6, 0, 6).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(7).Children(func() {
					ui.Box(c).WidthPercent(w * 100).Height(8).Radius(4).Background(bar)
					ui.Row(c).Children(func() {
						ui.Box(c).Size(70, 6).Radius(3).Background(bar)
						ui.Spacer(c)
						ui.Box(c).Size(36, 6).Radius(3).Background(bar)
					})
				})
			})
		}
	})
}

// sessionsEmpty is what the view shows with no session to show: that
// none matches the query, or that there are none, and which agents' it
// lists.
func (a *app) sessionsEmpty(c *ui.Context) {
	p := a.pal
	st := &a.sessions
	if st.query != "" {
		ui.Text(c, locales.SessionsNoMatch()).FontSize(12).TextColor(p.muted).Padding(10, 18, 0, 18)
		return
	}
	msg := locales.SessionsNone()
	if st.all == 1 || a.root == "" {
		msg = locales.SessionsNoneAll()
	}
	ui.Column(c.Key("sessions-empty")).Padding(28, 20, 8, 20).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconClock).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, msg).FontSize(13).FontWeight(600).TextColor(p.text).TextAlign(ui.Center).Margin(4, 0, 0, 0)
		ui.Text(c, locales.SessionsHint()).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
	})
}

// sessionRow is the row of a session: its title, and under it what it
// does while it runs in herdr-core, its agent and its folder, and at the
// end when it was used last. Its menu resumes it or copies how to. With dots,
// as hc says which sessions run, a dot before it says it runs.
func (a *app) sessionRow(c *ui.Context, s agents.Session, now time.Time, dots bool, action *func()) {
	p := a.pal
	st := &a.sessions
	live, running := st.running[liveKey(s.Harness, s.ID)]
	rec := a.liveRecord(live)
	row := ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Height(sessionRowH).Padding(0, 6, 0, 6).
		Tooltip(s.Dir + "\n" + s.ID + "\n" + dateTime(s.Updated.Local()))
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
		if dots {
			dot := ui.Box(c.Key("dot")).Size(7, 7).Shrink(0)
			if running {
				dot.Children(func() {
					ui.Box(c.Key("on")).Size(7, 7).Radius(3.5).Background(a.progColor(rec.State)).Transition(dotPop).Tooltip(progLine(rec))
				})
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, s.Label()).FontSize(12.5).TextColor(p.text).SingleLine().ClipX()
			ui.Row(c).Gap(5).AlignItems(ui.Center).MinWidth(0).Children(func() {
				if running {
					_, what := progParts(rec)
					ui.Text(c.Key("state"), what).FontSize(11).TextColor(a.progColor(rec.State)).SingleLine().Shrink(0).Transition(dotPop)
				}
				ui.Text(c, a.sessionMeta(s)).FontSize(11).TextColor(p.faint).SingleLine().ClipX().Shrink(1)
				ui.Spacer(c)
				ui.Text(c, ago(s.Updated, now)).FontSize(11).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0).Padding(0, 0, 0, 6)
			})
		})
	})
}

// sessionMeta is what a session's row says under its title, what tells
// sessions apart first, as the narrow panel cuts its end: its agent, the
// config folder of Claude Code other than ~/.claude, by the name after
// .claude-, and its folder when the view lists every folder's.
func (a *app) sessionMeta(s agents.Session) string {
	parts := []string{s.Harness.Name()}
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
	s := a.startTerminal(dir)
	if s != nil {
		a.panelTerm = s
		a.window.PanelOpen, a.window.TerminalOpen = true, true
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
	a.window.PanelOpen, a.window.TerminalOpen = true, true
	a.rememberTabs()
	a.focusTerminal()
}

// showSessions shows the sessions in the sidebar, with the keyboard in
// their query.
func (a *app) showSessions() {
	a.window.SidebarClosed, a.window.SidebarView = false, "sessions"
	a.settings.Changed()
	a.sessions.input.Focus()
}
