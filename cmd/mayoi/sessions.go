package main

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// The Sessions view lists the sessions of the coding agents on this
// machine, Claude Code of each of its config folders, Qoder, Codex, Grok
// and those plugins add (pluginagents.go), the one used last first: of the window's folder, or of every
// folder. A query narrows them by title, first prompt, folder, agent or
// id, its words marked where a row shows them, each row marked with its
// agent's icon, and Enter resumes one in a terminal of its folder, in a tab
// of the column that has the keyboard, titled after the session until the
// agent names it. A row drags the file of its transcript, to a column or
// a terminal, as a file of the explorer. A session that runs, in a
// terminal of herdr-core or here, says what it does, as its agent says
// it (OSC 7501), and resuming it shows that terminal, in a tab.

// agentScanner reads the sessions for every window, and keeps what it
// read of each file.
var agentScanner = agents.NewScanner()

// sessionsEvery is how often the sessions are read again while the view
// shows, and sessionsLiveEvery what runs.
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
	// selection are the sessions Shift and Cmd choose with it, by their
	// keys, none for the session chosen alone; anchor is the one Shift
	// chooses from, and cursor the choice they were chosen with, which,
	// moved otherwise, lets them go.
	selection      ui.Selection[string]
	anchor, cursor string
	// glide is the choice's background on its way from the row it was on,
	// and listH how tall the list was, a page for Page Up and Page Down.
	glide choiceGlide
	listH float32
	// choiceHovered tells that the pointer was over the row chosen, as
	// it is when a click chose it.
	choiceHovered bool
	// endKey is Home or End, which the query took from its caret since the
	// last frame, for the choice; 0 for none.
	endKey ui.Key

	read bg.Poller[string]
	// readAgain has the sessions read again once the reading going ends,
	// as the agents changed meanwhile.
	readAgain bool
	sessions  []agents.Session
	loaded    bool
	// version counts the sessions read, and rows are the sessions shown
	// as of rowsKey.
	version int
	rows    []agents.Session
	rowsKey sessionsKey
	// texts are what the query looks in of each session, as of the
	// reading textsVersion, made once for it rather than for each key.
	texts        []string
	textsVersion int
	// lines are the rows under the heads of their days, as of linesKey
	// and the day linesDay.
	lines    []sessionLine
	linesKey sessionsKey
	linesDay time.Time
	// linesGen counts the lines made. shown are those the list shows, as
	// of shownGen: the lines, with those a reading dropped in their places
	// as they go, the first of them gone at goneEnd.
	linesGen int
	shown    []sessionLine
	shownGen int
	goneEnd  time.Time
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
	// goneLooks are the looks of the reading before, for the rows it
	// dropped as they go.
	goneLooks map[string]*sessionLook

	// running are the sessions that run, by liveKey: those hc says run in
	// herdr-core, hcRunning, those hc only says an agent of runs, hcLoose,
	// and those the terminals here run, placed in the sessions read.
	live      bg.Poller[string]
	running   map[string]hcAgent
	hcRunning map[string]hcAgent
	hcLoose   []hcAgent
	liveSeen  bool
	// agentSince is when each agent that runs, by agentKey, was first
	// seen to, those of the first reading at zero, as they may run from
	// before the window.
	agentSince map[string]time.Time
	// liveSince is when each session that runs, by liveKey, took the
	// state hc says it is in, which its dot calls out for a while.
	liveSince map[string]time.Time
	// open are the terminals of every project that show a session, by
	// liveKey: one it was resumed in, or one its agent runs in.
	open map[string]openTerm
}

// openTerm is a terminal of a project that shows a session.
type openTerm struct {
	owner *app
	t     *session
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
				a.placeLive(time.Now())
				st.read.Keep("", sessionsEvery, a.invalidate)
				if st.readAgain {
					st.readAgain = false
					st.read.Expire()
				}
			})
		})
	}
	if st.live.Start("", sessionsLiveEvery) {
		hc := a.hcBackend()
		roots := a.agentEnv().ClaudeRoots()
		bg.Go(func() {
			var running map[string]hcAgent
			var loose []hcAgent
			if hc {
				running, loose = hcAgents(roots)
			}
			a.post(func() {
				if !st.live.Done("") {
					return
				}
				st.hcRunning, st.hcLoose, st.liveSeen = running, loose, true
				a.placeLive(time.Now())
				st.live.Keep("", sessionsLiveEvery, a.invalidate)
			})
		})
	}
}

// placeLive finds again, at now, the sessions that run: those hc names,
// those terminals here resumed, and, for the agents that run but name
// none, the newest of their agent and folder.
func (a *app) placeLive(now time.Time) {
	st := &a.sessions
	running := maps.Clone(st.hcRunning)
	if running == nil {
		running = map[string]hcAgent{}
	}
	loose := slices.Clone(st.hcLoose)
	locals := a.localAgents()
	since := make(map[string]time.Time, len(loose)+len(locals))
	first := st.agentSince == nil
	for _, ag := range append(slices.Clone(loose), locals...) {
		k := ag.agentKey()
		if was, ok := st.agentSince[k]; ok || first {
			since[k] = was
		} else {
			since[k] = now
		}
	}
	// Until hc is first read, every agent may run from before the window.
	if st.liveSeen {
		st.agentSince = since
	}
	for i := range loose {
		loose[i].since = since[loose[i].agentKey()]
	}
	for _, ag := range locals {
		ag.since = since[ag.agentKey()]
		if ag.term.resumed == "" {
			loose = append(loose, ag)
			continue
		}
		for _, s := range st.sessions {
			if s.Key() == ag.term.resumed {
				running[liveKey(s.Harness, s.ID)] = ag
				break
			}
		}
	}
	running = placeLoose(running, loose, st.sessions)
	st.noteLive(running, now)
	st.running = running
	st.open = a.openTerms(running)
}

// projects are the projects of every window, this one among them.
func (a *app) projects() []*app {
	if ps := a.host.everyProject(); len(ps) > 0 {
		return ps
	}
	return []*app{a}
}

// openTerms are the terminals of every project that show the sessions
// read: those that resumed one, and those an agent of running runs in.
func (a *app) openTerms(running map[string]hcAgent) map[string]openTerm {
	open := map[string]openTerm{}
	var resumed map[string]openTerm
	for _, p := range a.projects() {
		for _, t := range p.terms {
			for k, ag := range running {
				if ag.term == t || ag.term == nil && ag.addr != "" && t.hc == ag.addr {
					open[k] = openTerm{p, t}
				}
			}
			if t.resumed != "" {
				if resumed == nil {
					resumed = map[string]openTerm{}
				}
				resumed[t.resumed] = openTerm{p, t}
			}
		}
	}
	if resumed != nil {
		for _, s := range a.sessions.sessions {
			k := liveKey(s.Harness, s.ID)
			if o, ok := resumed[s.Key()]; ok && open[k].t == nil {
				open[k] = o
			}
		}
	}
	return open
}

// showOpen shows the terminal o of a project, in front, with the
// keyboard in it, and reports whether it is open still.
func (a *app) showOpen(o openTerm) bool {
	if o.owner == nil || !slices.Contains(o.owner.terms, o.t) {
		return false
	}
	if o.owner == a {
		a.terminalInColumn(o.t, false)
		return true
	}
	a.host.showApp(o.owner)
	o.owner.post(func() { o.owner.terminalInColumn(o.t, false) })
	return true
}

// localAgents are the agents that say what they do (OSC 7501) in the
// terminals that run in every project, in their folders.
func (a *app) localAgents() []hcAgent {
	var out []hcAgent
	for _, p := range a.projects() {
		out = append(out, p.ownAgents()...)
	}
	return out
}

// ownAgents are the agents that say what they do in the terminals of
// the project.
func (a *app) ownAgents() []hcAgent {
	var out []hcAgent
	for _, t := range a.terms {
		if t.hc != "" || t.attach != "" {
			continue
		}
		r, ok := t.status.Top()
		if !ok {
			continue
		}
		h, ok := agents.HarnessOfApp(r.App)
		if !ok {
			continue
		}
		dir := t.dir
		if t.term != nil {
			if reported := t.term.Dir(); filepath.IsAbs(reported) {
				dir = reported
			}
		}
		out = append(out, hcAgent{cwd: dir, harness: h, state: r.State, term: t, owner: a})
	}
	return out
}

// readSessionsNow has the sessions read again at once, as the agents
// change: after the reading going, if one is.
func (a *app) readSessionsNow() {
	st := &a.sessions
	if st.read.Running() {
		st.readAgain = true
	} else {
		st.read.Expire()
	}
	a.invalidate()
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
	words := strings.Fields(strings.ToLower(st.query))
	if len(words) > 0 && (st.texts == nil || st.textsVersion != st.version) {
		st.texts = make([]string, len(st.sessions))
		for i, s := range st.sessions {
			st.texts[i] = s.Text()
		}
		st.textsVersion = st.version
	}
	rows := []agents.Session{}
	for i, s := range st.sessions {
		if (all || s.In(a.root)) && (len(words) == 0 || agents.HasWords(st.texts[i], words)) {
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
	// gone is when a reading dropped the line, which then closes its place
	// as it fades out; zero for a line that stays.
	gone time.Time
}

// session reports whether the line is a session's that stays, which the
// choice can rest on.
func (l sessionLine) session() bool { return !l.head && l.gone.IsZero() }

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
	st.linesGen++
	return lines
}

// sessionAt is the session row of lines nearest i, at or after it, else
// before it, and -1 for none.
func sessionAt(lines []sessionLine, i int) int {
	i = min(max(i, 0), len(lines)-1)
	for j := i; j < len(lines); j++ {
		if lines[j].session() {
			return j
		}
	}
	for j := i - 1; j >= 0; j-- {
		if lines[j].session() {
			return j
		}
	}
	return -1
}

// sessionStep is the session row of lines after i, or before it for a
// negative d, past the heads; i itself at the end.
func sessionStep(lines []sessionLine, i, d int) int {
	for j := i + d; j >= 0 && j < len(lines); j += d {
		if lines[j].session() {
			return j
		}
	}
	return i
}

// keepChoice returns the lines the list shows, those a reading dropped
// among them as they go, and keeps the choice on the session it was on
// as they change, or on the session nearest its row once that one is
// gone. It notes which rows came with a reading, to fade them in.
func (a *app) keepChoice(lines []sessionLine, now time.Time) []sessionLine {
	st := &a.sessions
	fresh := false
	if st.appearKey != st.rowsKey {
		k, was := st.rowsKey, st.appearKey
		fresh = k.query != was.query || k.root != was.root || k.all != was.all
		if fresh {
			st.selection.Clear()
		}
		st.appear.note(len(lines), func(i int) string { return lines[i].key() }, now, fresh)
		st.appearKey = k
		if !fresh {
			// A row that comes back as it goes comes on from where it was.
			for _, l := range st.shown {
				if !l.gone.IsZero() {
					st.appear.resume(l.key(), st.lineShown(l, now), now)
				}
			}
		}
	}
	if st.shown == nil || st.shownGen != st.linesGen || (!st.goneEnd.IsZero() && !now.Before(st.goneEnd)) {
		// Those of another query or folder don't go: the list shows
		// other rows.
		was := st.shown
		if fresh {
			was = nil
		}
		st.shown, st.goneEnd = leaveLines(was, lines, now)
		st.shownGen = st.linesGen
	}
	lines = st.shown
	if len(lines) == 0 {
		return lines
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
	st.keepSelection(lines)
	return lines
}

// keepSelection lets go of the sessions chosen that lines no longer
// list, and of them all once the choice moved off the session they were
// chosen with, as a plain click or key moves it.
func (st *sessionsState) keepSelection(lines []sessionLine) {
	if st.selection.Len() == 0 {
		return
	}
	if st.rowKey != st.cursor {
		st.selection.Clear()
		return
	}
	listed := make(map[string]bool, len(lines))
	for _, l := range lines {
		if l.session() {
			listed[l.key()] = true
		}
	}
	for k := range st.selection.All() {
		if !listed[k] {
			st.selection.Remove(k)
		}
	}
}

// selectSessionRow chooses row i of lines as a click or a key with mods
// does: with Shift, the sessions from the anchor to it; with Cmd, it as
// well as those chosen, or no longer; else it alone. Shift and Cmd put
// the choice there at once, rather than gliding over the rows between.
func (a *app) selectSessionRow(lines []sessionLine, i int, mods ui.Modifiers) {
	st := &a.sessions
	if i < 0 || i >= len(lines) || !lines[i].session() {
		return
	}
	k := lines[i].key()
	anchor := -1
	if st.selection.Len() > 0 {
		for j, l := range lines {
			if l.key() == st.anchor && l.session() {
				anchor = j
				break
			}
		}
	}
	if anchor < 0 && st.row >= 0 && st.row < len(lines) && lines[st.row].session() {
		anchor = st.row
	}
	switch {
	case mods&ui.Shift != 0 && anchor >= 0:
		st.selection.Clear()
		for j := min(anchor, i); j <= max(anchor, i); j++ {
			if lines[j].session() {
				st.selection.Add(lines[j].key())
			}
		}
		st.anchor = lines[anchor].key()
	case mods&ui.Cmd != 0:
		if st.selection.Len() == 0 && anchor >= 0 {
			st.selection.Add(lines[anchor].key())
		}
		if st.selection.Has(k) {
			st.selection.Remove(k)
		} else {
			st.selection.Add(k)
		}
		st.anchor = k
	default:
		st.selection.Clear()
		st.anchor = k
	}
	if mods&(ui.Shift|ui.Cmd) != 0 {
		st.glide.key = ""
	}
	st.row = i
	st.noteChoice(lines)
	st.cursor = st.rowKey
}

// selectAllSessions chooses every session of lines, the choice staying
// where it is.
func (a *app) selectAllSessions(lines []sessionLine) {
	st := &a.sessions
	st.selection.Clear()
	for _, l := range lines {
		if l.session() {
			st.selection.Add(l.key())
		}
	}
	st.cursor = st.rowKey
	if st.selection.Len() > 0 && st.anchor == "" {
		st.anchor = st.rowKey
	}
}

// pickedSessions are the sessions of lines chosen with l, in their order,
// when l is one of several chosen, and else l's alone.
func (st *sessionsState) pickedSessions(lines []sessionLine, l sessionLine) []agents.Session {
	if st.selection.Len() < 2 || !st.selection.Has(l.key()) {
		return []agents.Session{l.s}
	}
	var out []agents.Session
	for _, m := range lines {
		if m.session() && st.selection.Has(m.key()) {
			out = append(out, m.s)
		}
	}
	return out
}

// leaveLines are lines with those of was they don't have among them as
// they go: each after the line before it that stays, gone from now unless
// it went before, and left out once gone for motionLeave. end is when the
// first of those among them is, zero for none.
func leaveLines(was, lines []sessionLine, now time.Time) (out []sessionLine, end time.Time) {
	if len(was) == 0 {
		return lines, time.Time{}
	}
	has := make(map[string]bool, len(lines))
	for _, l := range lines {
		has[l.key()] = true
	}
	var after map[string][]sessionLine
	n, prev := 0, ""
	for _, l := range was {
		k := l.key()
		if has[k] {
			prev = k
			continue
		}
		if l.gone.IsZero() {
			l.gone = now
		}
		until := l.gone.Add(motionLeave)
		if !now.Before(until) {
			continue
		}
		if end.IsZero() || until.Before(end) {
			end = until
		}
		if after == nil {
			after = map[string][]sessionLine{}
		}
		after[prev] = append(after[prev], l)
		n++
	}
	if n == 0 {
		return lines, time.Time{}
	}
	out = make([]sessionLine, 0, len(lines)+n)
	out = append(out, after[""]...)
	for _, l := range lines {
		out = append(out, l)
		out = append(out, after[l.key()]...)
	}
	return out, end
}

// lineShown is how far in a line is at now: from 0 to 1 as it comes with
// a reading, and back to 0 as it goes.
func (st *sessionsState) lineShown(l sessionLine, now time.Time) float32 {
	if !l.gone.IsZero() {
		return 1 - ui.EaseOut(min(float32(now.Sub(l.gone))/float32(motionLeave), 1))
	}
	return st.appear.shown(l.key(), now)
}

// openShut has the row of line l open as far as it shows at now, of
// height h: it fades, and grows or shrinks to its height, which moves
// the rows under it along; with less motion, it only fades.
func (a *app) openShut(c *ui.Context, row ui.Element, l sessionLine, h float32, now time.Time) {
	k := a.sessions.lineShown(l, now)
	if k >= 1 {
		return
	}
	row.Opacity(k)
	if !c.Preferences().ReduceMotion {
		row.Height(h * k).ClipY()
	}
	c.AnimationFrame()
}

// sessionKeys moves the choice among rows by the keys shortcut reports:
// Page Up and Page Down by a page, and from the query, Up and Down by a
// row, and Home and End (with Ctrl but on macOS, where they move no
// caret) to the first and the last.
func (a *app) sessionKeys(rows []sessionLine, shortcut func(ui.Modifiers, ui.Key) bool, query bool) {
	st := &a.sessions
	if len(rows) == 0 {
		return
	}
	end := st.endKey
	st.endKey = 0
	to, d := -1, 0
	switch {
	case shortcut(0, ui.KeyPageDown):
		to, d = a.sessionPage(rows, st.row, 1), 1
	case shortcut(0, ui.KeyPageUp):
		to, d = a.sessionPage(rows, st.row, -1), -1
	case !query:
	case shortcut(0, ui.KeyDown):
		to, d = sessionStep(rows, st.row, 1), 1
	case shortcut(0, ui.KeyUp):
		to, d = sessionStep(rows, st.row, -1), -1
	case end == ui.KeyHome:
		to, d = sessionAt(rows, 0), -1
	case end == ui.KeyEnd:
		to, d = sessionStep(rows, len(rows), -1), 1
	}
	if to < 0 || to >= len(rows) {
		return
	}
	st.row = to
	st.noteChoice(rows)
	st.showChoice(rows, d)
}

// endsMods are the modifiers with which Home and End in the query move the
// choice: none on macOS, where they move no caret in a field of its own,
// else Ctrl, as Home and End alone move the caret.
func endsMods() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return 0
	}
	return ui.Ctrl
}

// sessionPage is the session row of lines a page after i, or before it
// for a negative d: the last a list as tall as the last frame's shows
// whole below its pinned head, from i; the next one at least.
func (a *app) sessionPage(lines []sessionLine, i, d int) int {
	view := a.sessions.listH
	if view <= 0 {
		view = 10 * sessionRowH
	}
	view -= sessionsHeadH
	to, y := i, float32(0)
	for j := i + d; j >= 0 && j < len(lines); j += d {
		if y += a.lineH(lines[j]) + sessionRowGap; y > view && to != i {
			break
		}
		if lines[j].session() {
			to = j
		}
	}
	return to
}

// showChoice scrolls the list as little as shows the row chosen, and the
// line past it in d, so that the next the keys go to shows, and the head
// pinned at the top never hides it.
func (st *sessionsState) showChoice(lines []sessionLine, d int) {
	i := st.row
	if j := i + d; d != 0 && j >= 0 && j < len(lines) {
		i = j
	}
	st.list.ScrollIntoView(i)
}

// skipGone moves the choice the list's keys put on a row that goes past
// it, on from pre, where it was, rather than letting it rest there a frame
// and come back.
func (a *app) skipGone(lines []sessionLine, pre int) {
	st := &a.sessions
	if st.row == pre || st.row < 0 || st.row >= len(lines) || lines[st.row].session() {
		return
	}
	j := sessionStep(lines, st.row, cmp.Compare(st.row, pre))
	if j == st.row {
		j = pre
	}
	st.row = j
	st.noteChoice(lines)
}

// lineH is the height of the row of line l, as it stays.
func (a *app) lineH(l sessionLine) float32 {
	switch {
	case l.head:
		return sessionsHeadH
	case a.sessionLook(l).why != nil:
		return sessionRowH + sessionWhyH
	}
	return sessionRowH
}

// choiceGlideSpan is how many lines apart the choice glides between two
// rows at most; further, as Page Down takes it, it shows there at once.
const choiceGlideSpan = 3

// choiceGlide is the background of the choice as it glides to the row of
// key, at row, from y above it (below, for a negative y) at at.
type choiceGlide struct {
	key string
	row int
	y   float32
	at  time.Time
}

// glideTo is how far from row i of lines the choice's background shows at
// now, and how much of the way there it went, from 0 to 1: it glides there
// over motionHover from the row it was on, easing out as it follows a key.
// A key that repeats as it glides, a row far off, or less motion, puts it
// there at once.
func (a *app) glideTo(lines []sessionLine, i int, now time.Time, reduce bool) (y, there float32) {
	g := &a.sessions.glide
	if k := lines[i].key(); g.key != k {
		y = 0
		if g.key != "" && !reduce && g.row >= 0 && g.row < len(lines) && lines[g.row].key() == g.key &&
			abs(g.row-i) <= choiceGlideSpan && now.Sub(g.at) >= motionHover {
			lo, hi := min(g.row, i), max(g.row, i)
			for j := lo; j < hi; j++ {
				y += a.lineH(lines[j]) + sessionRowGap
			}
			if g.row < i {
				y = -y
			}
		}
		g.key, g.y, g.at = k, y, now
	}
	g.row = i
	k := float32(now.Sub(g.at)) / float32(motionHover)
	if g.y == 0 || k >= 1 {
		g.y = 0
		return 0, 1
	}
	there = ui.EaseOut(k)
	return g.y * (1 - there), there
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
	rows := a.keepChoice(a.sessionLines(a.sessionRows(), now), now)
	a.sessionLooks()
	queryKeys := false
	ui.Column(c).Padding(0, 12, 6, 12).Gap(6).Children(func() {
		in := a.searchField(c, "sessions-query", &st.query, locales.SessionsFind(), &st.input, nil)
		queryKeys = in.Focused()
		in.OnTextKey(func(k ui.TextKey) bool {
			if (k.Key != ui.KeyHome && k.Key != ui.KeyEnd) || k.Mods != endsMods() {
				return false
			}
			st.endKey = k.Key
			a.invalidate()
			return true
		})
		if in.Changed() {
			st.row, st.rowKey = 0, ""
		}
		if in.Submitted() && len(rows) > 0 {
			if i := sessionAt(rows, st.row); i >= 0 {
				a.resumeSession(rows[i].s)
			}
		}
		// Up and Down, Page Up and Page Down, and Home and End move the
		// choice, as in Quick Open, with the keyboard left in the query.
		a.sessionKeys(rows, in.Shortcut, true)
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
	// buttons, and the choice, to glide it between them.
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = ui.Color{}, p.text, ui.Color{}
	c.SetTheme(&quiet)
	var action func()
	pre := st.row
	// Page Up and Page Down only scroll a list of macOS, and elsewhere put
	// its choice at the edge of what shows, under the pinned head at the
	// top; here they move the choice by a page, as from the query. They
	// are taken as the first row builds, before the list moves by them.
	var up, down, paged bool
	out := ui.List(c.Key("sessions"), &st.list, len(rows), func(i int) {
		if !paged {
			paged = true
			up, down = st.list.Shortcut(c, 0, ui.KeyPageUp), st.list.Shortcut(c, 0, ui.KeyPageDown)
		}
		a.skipGone(rows, pre)
		if r := rows[i]; r.head {
			a.sessionsHead(c, r, now)
		} else {
			// The chosen row shows its buttons while the list has the
			// keyboard, as the keys that run them do.
			var choice *sessionChoice
			if i == st.row {
				listKeys := st.list.FocusWithin(c)
				loose := st.selection.Len() > 0 && !st.selection.Has(r.key())
				choice = &sessionChoice{keyed: listKeys, held: listKeys || queryKeys, loose: loose}
				choice.y, choice.there = a.glideTo(rows, i, now, c.Preferences().ReduceMotion)
			}
			row := a.sessionRow(c, r, now, choice, &action)
			if choice != nil {
				st.choiceHovered = row.Hovered()
			}
			// Taking the drag of its transcript, the row takes the clicks
			// from the list too: one chooses it, two resume it.
			// One click on a session open in a terminal shows that
			// terminal, in its project.
			o, open := st.open[liveKey(r.s.Harness, r.s.ID)]
			switch double, clicked := row.DoubleClicked(), row.Clicked(); {
			case action != nil:
			case double:
				action = func() { a.resumeSession(r.s) }
			case clicked && open && row.ClickModifiers() == 0:
				a.selectSessionRow(rows, i, 0)
				action = func() {
					if !a.showOpen(o) {
						a.resumeSession(r.s)
					}
				}
			case clicked:
				a.selectSessionRow(rows, i, row.ClickModifiers())
				st.list.Focus(c)
			}
		}
	}).Grow(1).Padding(0, 6, 8, 6).Gap(sessionRowGap).FocusRing(false) // the choice rings itself
	st.listH = out.Bounds().H
	if up || down {
		a.sessionKeys(rows, func(m ui.Modifiers, k ui.Key) bool {
			return m == 0 && (k == ui.KeyPageUp && up || k == ui.KeyPageDown && down)
		}, false)
	}
	a.skipGone(rows, pre)
	if st.row != pre && !st.choiceHovered {
		// The keys of the list moved it: the row past it shows too.
		st.showChoice(rows, cmp.Compare(st.row, pre))
	}
	// Shift with Up, Down, Home and End chooses the sessions from the
	// anchor to the row it moves to.
	for _, k := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd} {
		if !out.Shortcut(ui.Shift, k) {
			continue
		}
		to, d := sessionStep(rows, st.row, 1), 1
		switch k {
		case ui.KeyUp:
			to, d = sessionStep(rows, st.row, -1), -1
		case ui.KeyHome:
			to, d = sessionAt(rows, 0), -1
		case ui.KeyEnd:
			to, d = sessionStep(rows, len(rows), -1), 1
		}
		a.selectSessionRow(rows, to, ui.Shift)
		st.showChoice(rows, d)
	}
	if st.row >= 0 && st.row < len(rows) {
		// The list's keys and clicks move the choice after its rows are
		// built: it glides from the next frame on.
		a.glideTo(rows, st.row, now, c.Preferences().ReduceMotion)
	}
	if since := now.Sub(st.shownAt); since < motionShow {
		out.Opacity(ui.EaseOut(float32(since) / float32(motionShow)))
		c.AnimationFrame()
	}
	c.SetTheme(base)
	if out.Changed() {
		st.noteChoice(rows)
	}
	if n := st.selection.Len(); n > 0 {
		st.keepSelection(rows)
		if st.selection.Len() != n {
			a.invalidate()
		}
	}
	if st.list.Shortcut(c, 0, ui.KeyEscape) {
		// The sessions chosen go first, then the keyboard.
		if st.selection.Len() > 0 {
			st.selection.Clear()
		} else {
			st.input.Focus()
		}
	}
	// Copy of the Edit menu, which takes ⌘C, copies the command that
	// resumes the session chosen while the list has the keyboard, and
	// Select All, which takes ⌘A, chooses every session.
	out.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputCommand {
			return false
		}
		switch ev.Text {
		case "selectAll":
			a.selectAllSessions(rows)
			a.invalidate()
			return true
		case "copy":
			if st.row < 0 || st.row >= len(rows) || !rows[st.row].session() {
				return false
			}
			a.copyText(a.agentEnv().ResumeCommand(rows[st.row].s))
			return true
		}
		return false
	})
	if action != nil {
		action()
	}
	if out.Submitted() && st.row >= 0 && st.row < len(rows) && rows[st.row].session() {
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
	// sessionIconSize is the size of the icon of a row's agent, as tall as
	// the line it leads.
	sessionIconSize = 12
	// sessionRowGap is the room between rows, so that the chosen row and
	// the one under the pointer read as two.
	sessionRowGap = 2
)

// sessionsHead heads the sessions of a day, with how many there are. It
// stays at the top as they scroll under it, and comes and goes with the
// day's rows, as a reading brings the first or drops the last.
func (a *app) sessionsHead(c *ui.Context, r sessionLine, now time.Time) {
	p := a.pal
	head := ui.Row(c).Grow(1).Height(sessionsHeadH).Padding(4, 6, 0, 6).AlignItems(ui.Center).Gap(6).Background(p.chrome)
	a.openShut(c, head, r, sessionsHeadH, now)
	head.Children(func() {
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
// does while it runs in herdr-core, its agent, by its icon and name, and its
// folder, and at the
// end when it was used last, where the pointer over it shows buttons that
// resume it and copy how to, as its menu does; keyed, as the row chosen
// while the list has the keyboard, it shows them too. The words of the
// query are marked in the title, in the accent, and in the agent and
// folder, in the color of text. A dot before it says what it does while
// it runs. The chosen row has the background of the
// choice; the one under the pointer fades its own in with its buttons. A
// row that came with a reading of the sessions fades in as it opens its
// place, and the rows under it slide down; one a reading dropped fades out
// as it closes its place, and they slide up. It drags the file of its
// transcript while there is one.
func (a *app) sessionRow(c *ui.Context, l sessionLine, now time.Time, choice *sessionChoice, action *func()) ui.Element {
	p := a.pal
	st := &a.sessions
	s := l.s
	look := a.sessionLook(l)
	lk := look.live
	live, running := st.running[lk]
	rec := a.liveRecord(live)
	h := float32(sessionRowH)
	if look.why != nil {
		h += sessionWhyH
	}
	row := ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Height(h).Padding(0, 6, 0, 6).Tooltip(look.tip)
	a.openShut(c, row, l, h, now)
	chosen, keyed := choice != nil, choice != nil && choice.keyed
	// Of several chosen, those but the choice show the background of the
	// choice too, without its ring.
	picked := !chosen && st.selection.Len() > 0 && st.selection.Has(l.key())
	// The buttons take the place of the time as the pointer comes, and
	// give it back as it goes; neither moves the title or the folder. They
	// come with the choice the keys move, as it glides in. A row that goes
	// shows none.
	going := !l.gone.IsZero()
	reveal := row.Animate("reveal", unit(row.Hovered() && !going), motionHover)
	if keyed {
		// As the choice comes, with it.
		reveal = max(reveal, choice.there)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if going {
			return
		}
		if m.Item(locales.SessionsResume()).Shortcut(0, ui.KeyEnter).Chosen() {
			*action = func() { a.resumeSession(s) }
		}
		m.Separator()
		if m.Item(locales.SessionsCopyCommand()).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
			cmd := a.agentEnv().ResumeCommand(s)
			*action = func() { a.copyText(cmd) }
		}
		// Of several sessions chosen, the menu of one copies their IDs.
		ids := st.pickedSessions(st.shown, l)
		label := locales.SessionsCopyID()
		if len(ids) > 1 {
			label = locales.SessionsCopyIDs(len(ids))
		}
		if m.Item(label).Chosen() {
			*action = func() { a.copySessionIDs(ids) }
		}
		// What was said in it, of an agent that keeps it in a file.
		m.Separator()
		none := !isFile(s.Transcript)
		if m.Item(locales.SessionsCopyTranscript()).Disabled(none).Chosen() {
			*action = func() { a.copyTranscript(s.Transcript) }
		}
		if m.Item(locales.SessionsOpenTranscript()).Disabled(none).Chosen() {
			*action = func() { a.openFile(s.Transcript, true) }
		}
		if m.Item(revealLabel()).Disabled(none).Chosen() {
			*action = func() { a.host.reveal(s.Transcript) }
		}
	})
	if !going && isFile(s.Transcript) {
		f := &fileDrag{path: s.Transcript}
		if st.selection.Len() > 1 && st.selection.Has(l.key()) {
			// A row of several chosen drags their transcripts too.
			lines := st.shown
			f.more = func() []string {
				var paths []string
				for _, o := range st.pickedSessions(lines, l) {
					if o.Transcript != s.Transcript && isFile(o.Transcript) {
						paths = append(paths, o.Transcript)
					}
				}
				return paths
			}
		}
		a.dragFile(row, f)
	}
	row.Children(func() {
		var mark ui.Color
		if chosen {
			mark = a.choiceMark(c, choice, h)
		}
		if picked {
			ui.Box(c.Key("picked")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(6).Background(p.active)
		}
		if !chosen && reveal > 0 {
			// The hover, under the rest, in step with the buttons: it never
			// leaves them over a row gone plain, nor the other way round.
			ui.Box(c.Key("hover")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(6).Background(p.hover).Opacity(reveal)
		}
		// The dot's place is kept for a session that runs in none, so
		// that rows don't move as one starts or stops.
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
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.RichText(c, look.title...).FontSize(12.5).SingleLine().ClipX()
			ui.Row(c).Gap(5).AlignItems(ui.Center).MinWidth(0).Children(func() {
				// The agent's icon leads, and stays put as the rest moves.
				ui.Box(c.Key("agent")).Size(sessionIconSize, sessionIconSize).Shrink(0).Children(func() {
					ui.Icon(c, look.icon).FontSize(sessionIconSize).TextColor(look.tint)
				})
				// What a session does opens its room as it starts to run,
				// and the agent and folder move over for it.
				if running {
					_, what := progParts(rec)
					ui.Text(c.Key("state"), what).FontSize(11).TextColor(a.progColor(rec.State)).SingleLine().Shrink(0).Transition(wordIn)
				}
				ui.RichText(c.Key("meta"), look.meta...).FontSize(11).SingleLine().ClipX().Shrink(1).Transition(slideAlong)
				ui.Spacer(c)
				if _, open := st.open[lk]; open && !going {
					// A terminal of a project shows it, which a click shows.
					ui.Icon(c.Key("open"), iconTerminal).FontSize(sessionIconSize).TextColor(p.muted).Shrink(0).
						Tooltip(locales.SessionsTerminalOpen()).Transition(wordIn).Opacity(1 - reveal)
				}
				ui.Text(c, look.agoAt(s.Updated, now)).FontSize(11).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0).Padding(0, 0, 0, 6).
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
			switch {
			case chosen:
				bg = mark.Over(p.chrome)
			case picked:
				bg = p.hover.Over(p.active.Over(p.chrome))
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

// sessionChoice is how the row chosen shows the choice: y from its place
// as it glides there, there of the way, and whether the list has the
// keyboard (keyed), or it or the query, whose keys move the choice (held),
// and whether Cmd left it out of the sessions chosen (loose).
type sessionChoice struct {
	y, there           float32
	keyed, held, loose bool
}

// choiceMark is the background of the choice, under the rest of a row h
// tall: quieter while neither the list nor the query has the keyboard,
// none while it is left out of the sessions chosen, but for its ring, and
// ringed in the accent, faintly, while the list does, the ring coming as
// it glides in rather than crossing the text of the rows on its way. It
// fades between them over motionHover, also with less motion.
// It returns its color, for the buttons to sit on.
func (a *app) choiceMark(c *ui.Context, choice *sessionChoice, h float32) ui.Color {
	p := a.pal
	box := ui.Box(c.Key("choice")).Absolute().Left(0).Right(0).Top(0).Height(h).Radius(6)
	held := box.Animate("held", unit(choice.held), motionHover)
	keyed := box.Animate("keyed", unit(choice.keyed), motionHover)
	loose := box.Animate("loose", unit(choice.loose), motionHover)
	col := p.active.Alpha((0.5 + 0.5*held) * (1 - loose))
	box.Background(col)
	if keyed > 0 {
		box.Border(1, p.accent.Alpha(0.45*keyed*choice.there*choice.there))
	}
	if choice.y != 0 {
		box.Translate(0, choice.y)
		c.AnimationFrame()
	}
	return col
}

// lookKey is what the looks of the rows depend on: the rows shown, and
// the colors, which change as the theme fades to another.
type lookKey struct {
	rows                       sessionsKey
	text, accent, faint, muted ui.Color
	locale                     string
}

// sessionLook is what a session's row shows of it, in spans with the
// words of the query marked, and its tip: made once for the rows shown,
// their colors and language, rather than each frame the row is built, as
// while a hover fades or the rows a reading brought come in. live is the
// session's key in what runs in herdr-core.
type sessionLook struct {
	title, meta, why []ui.Span
	tip, live        string
	// icon is the icon of its agent, in tint.
	icon *ui.SVG
	tint ui.Color
	// ago is how long ago the session was used, which holds from agoFrom
	// until agoUntil, the minute it counts.
	ago               string
	agoFrom, agoUntil time.Time
}

// agoAt is how long ago a session used at updated was, as of now: said
// again only once the minute it counts from updated is over, rather than
// each frame.
func (lk *sessionLook) agoAt(updated, now time.Time) string {
	if lk.agoUntil.IsZero() || now.Before(lk.agoFrom) || !now.Before(lk.agoUntil) {
		d := now.Sub(updated)
		m := d / time.Minute
		if d < 0 && d%time.Minute != 0 {
			m--
		}
		lk.ago = ago(updated, now)
		lk.agoFrom = updated.Add(m * time.Minute)
		lk.agoUntil = lk.agoFrom.Add(time.Minute)
	}
	return lk.ago
}

// sessionLooks readies the looks for the rows of this frame: those made
// for other rows or colors go, and the query is split into its words
// again.
func (a *app) sessionLooks() {
	st, p := &a.sessions, a.pal
	k := lookKey{st.rowsKey, p.text, p.accent, p.faint, p.muted, i18n.Locale()}
	if st.looks != nil && k == st.looksKey {
		return
	}
	// A row a reading dropped keeps its look as it goes, though its agent
	// may have gone with it.
	st.goneLooks = nil
	if was := st.looksKey; st.looks != nil && was.rows.query == k.rows.query && k.rows.version != was.rows.version {
		was.rows.version = k.rows.version
		if was == k {
			st.goneLooks = st.looks
		}
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
	if lk, ok := st.goneLooks[l.key()]; ok && !l.gone.IsZero() {
		st.looks[l.key()] = lk
		return lk
	}
	words := st.words
	lk := &sessionLook{
		title: wordSpans(s.Label(), words, p.text, p.accent),
		meta:  wordSpans(a.sessionMeta(s), words, p.faint, p.text),
		tip:   s.Dir + "\n" + s.ID + "\n" + dateTime(s.Updated.Local()),
		live:  liveKey(s.Harness, s.ID),
	}
	lk.icon, lk.tint = agentIcon(p, s.Harness)
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

// sessionMeta is what a session's row says under its title, after its
// agent's icon, what tells sessions apart first, as the narrow panel cuts
// its end: its agent, the config folder of Claude Code other than
// ~/.claude, by the name after .claude-, and its folder when the view
// lists every folder's.
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
	if live.term != nil {
		if r, ok := live.term.status.Top(); ok {
			return r
		}
	}
	for _, p := range a.projects() {
		for _, t := range p.terms {
			if t.hc == live.addr && live.addr != "" {
				if r, ok := t.status.Top(); ok {
					return r
				}
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

// copySessionIDs puts the IDs of sessions on the clipboard, a line each,
// and says so.
func (a *app) copySessionIDs(ss []agents.Session) {
	if len(ss) == 1 {
		a.copyText(ss[0].ID)
		return
	}
	ids := make([]string, len(ss))
	for i, s := range ss {
		ids[i] = s.ID
	}
	a.services.WriteClipboard(strings.Join(ids, "\n"))
	a.tell(note{message: locales.SessionsCopiedIDs(len(ids))})
}

// copyTranscript puts the file of a session's transcript on the
// clipboard, and says so.
func (a *app) copyTranscript(path string) {
	if err := a.host.copyFiles([]string{path}); err != nil {
		a.fail("%s", locales.FailCopy(err.Error()))
		return
	}
	a.tell(note{message: locales.SessionsCopiedTranscript()})
}

// resumeSession resumes a session in a tab of an editor column, with the
// keyboard in it: the terminal of herdr-core it runs in shows, or one here
// that resumed it before, in the column it is in, or else a terminal of its
// folder starts in the column that has the keyboard, which runs the
// agent's command that resumes it.
func (a *app) resumeSession(s agents.Session) {
	k := liveKey(s.Harness, s.ID)
	if o, ok := a.sessions.open[k]; ok && a.showOpen(o) {
		return
	}
	if live, ok := a.sessions.running[k]; ok {
		if live.addr != "" {
			a.showHCTerminal(live.addr, live.cwd, s.Harness)
			return
		}
	}
	key := s.Key()
	for _, p := range a.projects() {
		for _, t := range p.terms {
			if t.resumed == key && a.showOpen(openTerm{p, t}) {
				return
			}
		}
	}
	if fi, err := os.Stat(s.Dir); s.Dir == "" || err != nil || !fi.IsDir() {
		a.tell(note{message: locales.SessionsDirGone(s.Dir)})
		return
	}
	t := a.startTerminal(s.Dir)
	if t == nil {
		return
	}
	cmd := a.agentEnv().ResumeCommand(s)
	t.agent, t.resumed = s.Harness, key
	t.sessionTitle, t.program = s.Label(), programOf(cmd)
	a.terminalInColumn(t, true)
	// The shell runs it once it starts, as typed ahead.
	t.term.Send([]byte(cmd + "\r"))
}

// programOf is the program a shell command runs: its first word but the
// variables it sets, as CLAUDE_CONFIG_DIR=… claude.
func programOf(cmd string) string {
	for _, w := range strings.Fields(cmd) {
		if !strings.Contains(w, "=") {
			return w
		}
	}
	return ""
}

// terminalInColumn shows a terminal in a tab, with the keyboard in it: in
// the tab it has, or, one just started or of the panel, in a new tab after
// the one shown in the column that has the keyboard, which grows in.
func (a *app) terminalInColumn(t *session, fresh bool) {
	col := a.column()
	switch {
	case t.tab != nil:
		a.showTerminal(t)
	case fresh:
		a.terminalTab(col, col.active+1, t, true)
	default:
		a.terminalToColumn(t, col, col.active+1)
	}
}

// showHCTerminal shows the terminal of herdr-core at addr, which runs an
// agent's session, in a tab of a column: one here that shows it, or a new
// one attached to it, in dir.
func (a *app) showHCTerminal(addr, dir string, agent agents.Harness) {
	for _, s := range a.terms {
		if s.hc == addr {
			s.agent = agent
			a.terminalInColumn(s, false)
			return
		}
	}
	if s := a.attachTerminal(addr, dir); s != nil {
		s.agent = agent
		a.terminalInColumn(s, true)
	}
}

// hasSessions is whether the project lists sessions: those read are of
// this machine, which a remote folder's are not.
func (a *app) hasSessions() bool {
	return a.root != "" && !remote.Is(a.root)
}

// showSessions shows the sessions in the sidebar, with the keyboard in
// their query.
func (a *app) showSessions() {
	if !a.hasSessions() {
		return
	}
	a.window.SidebarClosed, a.window.SidebarView = false, "sessions"
	a.settings.Changed()
	a.sessions.input.Focus()
}
