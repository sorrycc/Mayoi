package main

import (
	"cmp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/internal/bg"
	"mayoi/locales"
)

// The Sessions view lists the sessions of the coding agents on this
// machine, Claude Code of each of its config folders, Qoder, Codex and
// Grok, the one used last first: of the window's folder, or of every
// folder. A query narrows them by title, first prompt, folder, agent or
// id, its words marked where a row shows them, and Enter resumes one in a
// terminal of its folder. With the terminal
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
	all   int
	input ui.Handle
	list  ui.ListState
	row   int
	// rowKey is the key of the session chosen, which the choice stays on
	// as the sessions are read again and move.
	rowKey string

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
	// appear has the rows that come with a reading fade in, as of the
	// rows of appearKey.
	appear    appearing
	appearKey sessionsKey
	// looks are what the rows show of their sessions, by key, and words
	// the query's, as of looksKey.
	looks    map[string]*sessionLook
	words    []string
	looksKey lookKey

	live     bg.Poller[string]
	running  map[string]hcAgent
	liveSeen bool
	// liveSince is when each session that runs, by liveKey, took the
	// state hc says it is in, which its dot calls out for a while.
	liveSince map[string]time.Time
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
		st.running, st.liveSeen, st.liveSince = nil, false, nil
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
				st.noteLive(running, time.Now())
				st.running, st.liveSeen = running, true
				st.live.Keep("", sessionsLiveEvery, a.invalidate)
			})
		})
	}
}

// noteLive notes, at now, when each session of running took its state:
// one that starts or changes its state takes it now, one that stopped is
// forgotten.
func (st *sessionsState) noteLive(running map[string]hcAgent, now time.Time) {
	since := make(map[string]time.Time, len(running))
	for k, ag := range running {
		if was, ok := st.running[k]; ok && was.state == ag.state && !st.liveSince[k].IsZero() {
			since[k] = st.liveSince[k]
		} else {
			since[k] = now
		}
	}
	st.liveSince = since
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
	// id is the line's key, made once as the lines are.
	id string
}

// key is the line's in the list.
func (l sessionLine) key() string {
	switch {
	case l.id != "":
		return l.id
	case l.head:
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
			lines[head].id = lines[head].key()
		}
		lines[head].count++
		lines = append(lines, sessionLine{day: day, s: s, id: s.Key()})
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

// sessionStep is the session row of lines after i, or before it for a
// negative d, past the heads; i itself at the end.
func sessionStep(lines []sessionLine, i, d int) int {
	for j := i + d; j >= 0 && j < len(lines); j += d {
		if !lines[j].head {
			return j
		}
	}
	return i
}

// keepChoice keeps the choice on the session it was on as the lines
// change, or on the session nearest its row once that one is gone, and
// notes which rows came with a reading, to fade them in.
func (a *app) keepChoice(lines []sessionLine, now time.Time) {
	st := &a.sessions
	if st.appearKey != st.rowsKey {
		k, was := st.rowsKey, st.appearKey
		fresh := k.query != was.query || k.root != was.root || k.all != was.all
		st.appear.note(len(lines), func(i int) string { return lines[i].key() }, now, fresh)
		st.appearKey = k
	}
	if len(lines) == 0 {
		return
	}
	if st.rowKey != "" && (st.row < 0 || st.row >= len(lines) || lines[st.row].key() != st.rowKey) {
		for i, l := range lines {
			if l.key() == st.rowKey {
				st.row = i
				break
			}
		}
	}
	st.row = sessionAt(lines, max(st.row, 0))
	st.noteChoice(lines)
}

// noteChoice notes the key of the session chosen in lines.
func (st *sessionsState) noteChoice(lines []sessionLine) {
	st.rowKey = ""
	if st.row >= 0 && st.row < len(lines) {
		st.rowKey = lines[st.row].key()
	}
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
	a.keepChoice(rows, now)
	ui.Column(c).Padding(0, 12, 6, 12).Gap(6).Children(func() {
		in := a.searchField(c, "sessions-query", &st.query, locales.SessionsFind(), &st.input, nil)
		if in.Changed() {
			st.row, st.rowKey = 0, ""
		}
		if in.Submitted() && len(rows) > 0 {
			if i := sessionAt(rows, st.row); i >= 0 {
				a.resumeSession(rows[i].s)
			}
		}
		// Up and Down move the choice, as in Quick Open, with the keyboard
		// left in the query.
		for _, step := range [...]struct {
			key ui.Key
			d   int
		}{{ui.KeyDown, 1}, {ui.KeyUp, -1}} {
			if in.Shortcut(0, step.key) && len(rows) > 0 {
				st.row = sessionStep(rows, st.row, step.d)
				st.noteChoice(rows)
				st.list.ScrollIntoView(st.row)
			}
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
	// The rows paint their hover themselves, to fade it with their
	// buttons.
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, ui.Color{}
	c.SetTheme(&quiet)
	var action func()
	dots := a.hcBackend()
	a.sessionLooks()
	out := ui.List(c.Key("sessions"), &st.list, len(rows), func(i int) {
		if r := rows[i]; r.head {
			a.sessionsHead(c, r)
		} else {
			// The chosen row shows its buttons while the list has the
			// keyboard, as the keys that run them do.
			keyed := i == st.row && st.list.FocusWithin(c)
			a.sessionRow(c, r, now, dots, i == st.row, keyed, &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6).Gap(sessionRowGap)
	if since := now.Sub(st.shownAt); since < motionShow {
		out.Opacity(ui.EaseOut(float32(since) / float32(motionShow)))
		c.AnimationFrame()
	}
	c.SetTheme(base)
	if out.Changed() {
		st.noteChoice(rows)
	}
	if st.list.Shortcut(c, 0, ui.KeyEscape) {
		st.input.Focus()
	}
	// Copy of the Edit menu, which takes ⌘C, copies the command that
	// resumes the session chosen while the list has the keyboard.
	out.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputCommand || ev.Text != "copy" || st.row < 0 || st.row >= len(rows) || rows[st.row].head {
			return false
		}
		a.copyText(a.agentEnv().ResumeCommand(rows[st.row].s))
		return true
	})
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
	// sessionWhyH is how much taller a row is that says why the query
	// found it.
	sessionWhyH = 15
	// sessionRowGap is the room between rows, so that the chosen row and
	// the one under the pointer read as two.
	sessionRowGap = 2
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
		ui.Text(c, strconv.Itoa(r.count)).FontSize(10.5).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0).
			ContentTransition(ui.ContentTransition{Numeric: true})
	})
}

// sessionsSkeleton is what the view shows while it reads the sessions
// the first time: the shapes of rows, where the rows will be, so that
// nothing moves as they come.
func (a *app) sessionsSkeleton(c *ui.Context) {
	p := a.pal
	a.sessions.skeleton = true
	bar := p.text.Alpha(0.07)
	ui.Column(c.Key("sessions-skeleton")).Padding(0, 6, 8, 6).Gap(sessionRowGap).Grow(1).Children(func() {
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
// end when it was used last, where the pointer over it shows buttons that
// resume it and copy how to, as its menu does; keyed, as the row chosen
// while the list has the keyboard, it shows them too. The words of the
// query are marked in the title, in the accent, and in the agent and
// folder, in the color of text. With dots, as hc says which sessions run,
// a dot before it says it runs. The chosen row has the background of the
// choice; the one under the pointer fades its own in with its buttons. A
// row that came with a reading of the sessions fades in as it opens its
// place, and the rows under it slide down.
func (a *app) sessionRow(c *ui.Context, l sessionLine, now time.Time, dots, chosen, keyed bool, action *func()) ui.Element {
	p := a.pal
	st := &a.sessions
	s := l.s
	lk := liveKey(s.Harness, s.ID)
	live, running := st.running[lk]
	rec := a.liveRecord(live)
	look := a.sessionLook(l)
	h := float32(sessionRowH)
	if look.why != nil {
		h += sessionWhyH
	}
	row := ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Height(h).Padding(0, 6, 0, 6).Tooltip(look.tip)
	if k := st.appear.shown(l.key(), now); k < 1 {
		row.Opacity(k)
		if !c.Preferences().ReduceMotion {
			row.Height(h * k).ClipY()
		}
		c.AnimationFrame()
	}
	// The buttons take the place of the time as the pointer comes, and
	// give it back as it goes; neither moves the title or the folder. They
	// come at once with the choice the keys move.
	reveal := row.Animate("reveal", unit(row.Hovered()), motionHover)
	if keyed {
		reveal = 1
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.SessionsResume()).Shortcut(0, ui.KeyEnter).Chosen() {
			*action = func() { a.resumeSession(s) }
		}
		m.Separator()
		if m.Item(locales.SessionsCopyCommand()).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
			cmd := a.agentEnv().ResumeCommand(s)
			*action = func() { a.copyText(cmd) }
		}
		if m.Item(locales.SessionsCopyID()).Chosen() {
			*action = func() { a.copyText(s.ID) }
		}
	})
	row.Children(func() {
		if !chosen && reveal > 0 {
			// The hover, under the rest, in step with the buttons: it never
			// leaves them over a row gone plain, nor the other way round.
			ui.Box(c.Key("hover")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(6).Background(p.hover).Opacity(reveal)
		}
		// The dot's place is kept for a session that runs in none, so
		// that rows don't move as one starts or stops.
		if dots {
			dot := ui.Box(c.Key("dot")).Size(7, 7).Shrink(0)
			if running {
				dot.Children(func() {
					on := ui.Box(c.Key("on")).Size(7, 7).Transition(dotPop).Tooltip(progLine(rec))
					col := on.AnimateColor("col", a.progColor(rec.State), ui.Smooth)
					// It breathes while the agent works, and calls for a
					// while as it begins to wait for the user; it stays
					// still for those who ask for less motion.
					var pulse time.Time
					breathe := false
					if !c.Preferences().ReduceMotion {
						switch rec.State {
						case terminal.StatusWorking:
							breathe = true
						case terminal.StatusBlocked:
							if since := st.liveSince[lk]; now.Sub(since) < pulseFor {
								pulse = since
							}
						}
					}
					on.Draw(func(pt *ui.Painter, r ui.Rect) { liveDot(pt, r, col, pulse, breathe) })
				})
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.RichText(c, look.title...).FontSize(12.5).SingleLine().ClipX()
			ui.Row(c).Gap(5).AlignItems(ui.Center).MinWidth(0).Children(func() {
				// What a session does opens its room as it starts to run,
				// and the agent and folder move over for it.
				if running {
					_, what := progParts(rec)
					ui.Text(c.Key("state"), what).FontSize(11).TextColor(a.progColor(rec.State)).SingleLine().Shrink(0).Transition(wordIn)
				}
				ui.RichText(c.Key("meta"), look.meta...).FontSize(11).SingleLine().ClipX().Shrink(1).Transition(slideAlong)
				ui.Spacer(c)
				ui.Text(c, ago(s.Updated, now)).FontSize(11).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0).Padding(0, 0, 0, 6).
					Opacity(1 - reveal)
			})
			if look.why != nil {
				// What the query found that the lines above don't show.
				ui.RichText(c.Key("why"), look.why...).FontSize(11).SingleLine().ClipX()
			}
		})
		if reveal > 0 {
			// The buttons sit on the row's background, which the ends of
			// the title and the folder fade into before them.
			bg := p.hover.Over(p.chrome)
			if chosen {
				bg = p.active.Over(p.chrome)
			}
			ui.Row(c.Key("actions")).Absolute().Right(0).Top(0).Bottom(0).Opacity(reveal).Children(func() {
				ui.Box(c).Width(16).Gradient(bg.Alpha(0), bg, 90)
				ui.Row(c).Gap(2).Padding(0, 6, 0, 2).AlignItems(ui.Center).Background(bg).Radius(0, 6, 6, 0).Children(func() {
					a.sessionActions(c, s, action)
				})
			})
		}
	})
	return row
}

// lookKey is what the looks of the rows depend on: the rows shown, and
// the colors, which change as the theme fades to another.
type lookKey struct {
	rows                       sessionsKey
	text, accent, faint, muted ui.Color
}

// sessionLook is what a session's row shows of it, in spans with the
// words of the query marked, and its tip: made once for the rows shown
// and their colors, rather than each frame the row is built, as while a
// hover fades or the rows a reading brought come in.
type sessionLook struct {
	title, meta, why []ui.Span
	tip              string
}

// sessionLooks readies the looks for the rows of this frame: those made
// for other rows or colors go, and the query is split into its words
// again.
func (a *app) sessionLooks() {
	st, p := &a.sessions, a.pal
	k := lookKey{st.rowsKey, p.text, p.accent, p.faint, p.muted}
	if st.looks != nil && k == st.looksKey {
		return
	}
	st.looks, st.looksKey = map[string]*sessionLook{}, k
	st.words = strings.Fields(strings.ToLower(st.query))
}

// sessionLook is the look of the row of l, made the first time it shows.
func (a *app) sessionLook(l sessionLine) *sessionLook {
	st, p, s := &a.sessions, a.pal, l.s
	if lk, ok := st.looks[l.key()]; ok {
		return lk
	}
	words := st.words
	lk := &sessionLook{
		title: wordSpans(s.Label(), words, p.text, p.accent),
		meta:  wordSpans(a.sessionMeta(s), words, p.faint, p.text),
		tip:   s.Dir + "\n" + s.ID + "\n" + dateTime(s.Updated.Local()),
	}
	if why := a.sessionWhy(s, words); why != "" {
		lk.why = wordSpans(why, words, p.muted, p.text)
	}
	if st.looks != nil {
		st.looks[l.key()] = lk
	}
	return lk
}

// liveDot paints the dot of a session that runs, of col, in r. While
// breathe, a ring swells from it and fades every two pulseBeats, slower
// and fainter than a call, at 20 frames a second; for pulseFor from
// pulse, unless it is zero, it calls as a terminal's badge does as its
// program begins to wait. The window paints them without building the
// view.
func liveDot(pt *ui.Painter, r ui.Rect, col ui.Color, pulse time.Time, breathe bool) {
	if !beacon(pt, r, col, pulse, 0.9, 0.45) && breathe {
		beat := 2 * pulseBeat
		ring(pt, r, col, float32(pt.Now().UnixNano()%int64(beat))/float32(beat), 0.7, 0.3)
		pt.AnimationFrameRate(20)
	}
	pt.Fill(r, col, r.W/2)
}

// sessionWhy is what of a session the query finds that its row does not
// show, for the row to say why it is there: around the first word of
// words in neither its title nor what goes under it, its first prompt,
// its folder, from ~, or its id, the first that has it. It is "" when the
// row shows every word.
func (a *app) sessionWhy(s agents.Session, words []string) string {
	if len(words) == 0 {
		return ""
	}
	shown := strings.ToLower(s.Label() + "\n" + a.sessionMeta(s))
	var hidden []string
	if s.Title != "" && s.Prompt != "" {
		hidden = append(hidden, s.Prompt)
	}
	if s.Dir != "" {
		hidden = append(hidden, shortenHome(s.Dir))
	}
	if s.Label() != s.ID {
		hidden = append(hidden, s.ID)
	}
	for _, w := range words {
		if strings.Contains(shown, w) {
			continue
		}
		for _, h := range hidden {
			if i := strings.Index(strings.ToLower(h), w); i >= 0 {
				return snippetAt(h, i, len(w))
			}
		}
	}
	return ""
}

// snippetLead is how many bytes of text before a match its snippet keeps.
const snippetLead = 24

// snippetAt is text cut to show the run at i of n bytes of its lower
// case near its start: from a word that starts at most snippetLead before
// it, after an ellipsis. Text whose lower case is not as long as it is
// stays whole, as i would not be its own.
func snippetAt(text string, i, n int) string {
	if i <= snippetLead || len(strings.ToLower(text)) != len(text) || i+n > len(text) {
		return text
	}
	from := i - snippetLead
	if sp := strings.IndexByte(text[from:i], ' '); sp >= 0 {
		from += sp + 1
	} else {
		from = i
	}
	for from < i && !utf8.RuneStart(text[from]) {
		from++
	}
	return "…" + text[from:]
}

// sessionActions are the buttons of a session's row: one resumes it, the
// other copies the command that does.
func (a *app) sessionActions(c *ui.Context, s agents.Session, action *func()) {
	if a.smallButton(c, iconTerminal, locales.SessionsResume()) {
		*action = func() { a.resumeSession(s) }
	}
	if a.smallButton(c, iconCopy, locales.SessionsCopyCommand()) {
		cmd := a.agentEnv().ResumeCommand(s)
		*action = func() { a.copyText(cmd) }
	}
}

// wordSpans is text in spans of normal, with each run of it that one of
// words, in lower case, finds, ignoring case, in hit and a heavier weight.
// With no words, it is the one span of text.
func wordSpans(text string, words []string, normal, hit ui.Color) []ui.Span {
	if len(words) == 0 {
		return []ui.Span{{Text: text, Color: normal}}
	}
	// low is text in lower case, and at where each of its bytes came
	// from in text, as a rune's lower case can be longer or shorter.
	var b strings.Builder
	at := make([]int, 0, len(text)+1)
	for i, r := range text {
		n := b.Len()
		b.WriteRune(unicode.ToLower(r))
		for range b.Len() - n {
			at = append(at, i)
		}
	}
	at = append(at, len(text))
	low := b.String()
	on := make([]bool, len(text))
	for _, w := range words {
		for from := 0; from < len(low); {
			k := strings.Index(low[from:], w)
			if k < 0 || w == "" {
				break
			}
			lo, hi := at[from+k], at[from+k+len(w)]
			for j := lo; j < hi; j++ {
				on[j] = true
			}
			from += k + len(w)
		}
	}
	var spans []ui.Span
	start := 0
	for i := 1; i <= len(text); i++ {
		if i < len(text) && on[i] == on[start] {
			continue
		}
		sp := ui.Span{Text: text[start:i], Color: normal}
		if on[start] {
			sp.Color, sp.Weight = hit, 650
		}
		spans = append(spans, sp)
		start = i
	}
	return spans
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
