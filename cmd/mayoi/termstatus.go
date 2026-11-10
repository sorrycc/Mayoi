package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/locales"
)

// A terminal whose shell runs in herdr-core, or waits for one, says so:
// a dot at the corner of its icon, in its tab and its chip of the panel,
// and a pill over the terminal while it waits.

// termState is what a terminal's dot says.
type termState int

const (
	termHere     termState = iota // runs in Mayoi: no dot
	termKept                      // its shell runs in herdr-core
	termStarting                  // its shell starts in herdr-core
	termWaiting                   // waits for the connection or the machine that went
)

// termStatus is what a terminal's dot says about it.
func termStatus(s *session) termState {
	switch {
	case s == nil:
		return termHere
	case s.hcStarting:
		return termStarting
	case s.lost:
		return termWaiting
	case s.hc != "":
		return termKept
	}
	return termHere
}

// termWait is what the pill over a terminal says while it waits, "" when
// it doesn't.
func termWait(s *session) string {
	switch termStatus(s) {
	case termStarting:
		return locales.HcStarting()
	case termWaiting:
		if c := sessionConn(s); c != nil {
			return locales.TermWaiting(c.Host)
		}
		if machine, _, ok := strings.Cut(s.hc, "/"); ok {
			return locales.TermWaiting(machine)
		}
		return locales.TermWaiting("herdr-core")
	}
	return ""
}

// A program in a terminal may say what it does (OSC 7501): working,
// waiting for the user, done or failed. The badge of its icon takes the
// color of the record most in need of the user, the pill over it says
// what it does while it works or waits, and a project out of sight has
// its square on the rail marked as it waits, finishes or fails. Typing
// in the terminal tells that the user saw what finished.

// progStatus returns the record of a terminal's programs most in need of
// the user, and whether there is one that is not idle.
func progStatus(s *session) (terminal.StatusRecord, bool) {
	if s == nil {
		return terminal.StatusRecord{}, false
	}
	r, ok := s.status.Top()
	return r, ok && r.State != terminal.StatusIdle
}

// progColor is the color of a program's state.
func (a *app) progColor(st terminal.StatusState) ui.Color {
	p := a.pal
	switch st {
	case terminal.StatusBlocked:
		return p.modifiedFg
	case terminal.StatusDone:
		return p.addedFg
	case terminal.StatusError:
		return p.dangerFg
	}
	return p.accent
}

// progParts is what a record says, for people, in two parts: who, the
// program's label or name, "" when it gives neither, and what, its
// message on one line, or its state when it gives none.
func progParts(r terminal.StatusRecord) (who, what string) {
	what = strings.Join(strings.Fields(r.Message), " ")
	if what == "" {
		switch r.State {
		case terminal.StatusWorking:
			what = locales.ProgWorking()
		case terminal.StatusBlocked:
			switch r.Kind {
			case terminal.KindPermission:
				what = locales.ProgApproval()
			case terminal.KindQuestion:
				what = locales.ProgAnswer()
			case terminal.KindAuth:
				what = locales.ProgSignIn()
			default:
				what = locales.ProgBlocked()
			}
		case terminal.StatusDone:
			what = locales.ProgDone()
		case terminal.StatusError:
			what = locales.ProgFailed()
		default:
			what = locales.ProgIdle()
		}
	}
	who = strings.Join(strings.Fields(r.Title), " ")
	if who == "" {
		who = r.App
	}
	return who, what
}

// progLine is what a record says, for people, on one line: who, then
// what.
func progLine(r terminal.StatusRecord) string {
	who, what := progParts(r)
	if who == "" {
		return what
	}
	return who + ": " + what
}

// progOthers is how many records of a terminal's programs besides the
// one shown are not idle.
func progOthers(st terminal.ProgramStatus) int {
	n := 0
	for _, r := range st.Records {
		if r.State != terminal.StatusIdle {
			n++
		}
	}
	return max(n-1, 0)
}

// progTipMax is how many records the tooltip of a terminal's icon lists.
const progTipMax = 6

// progTip is the tooltip of a terminal's icon: what each of its programs
// not idle says, the one most in need of the user first, and how many
// more there are past progTipMax.
func progTip(st terminal.ProgramStatus) string {
	top, ok := st.Top()
	if !ok {
		return ""
	}
	lines := []string{progLine(top)}
	more := 0
	for _, r := range st.Records {
		if r.State == terminal.StatusIdle || r.ID == top.ID {
			continue
		}
		if len(lines) == progTipMax {
			more++
			continue
		}
		lines = append(lines, progLine(r))
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf("+%d", more))
	}
	return strings.Join(lines, "\n")
}

// progFlashFor is how long the pill over a terminal says that the work
// it showed finished or failed, before it rises away.
const progFlashFor = 1800 * time.Millisecond

// progPilled returns the record the pill over a terminal shows: one
// working or waiting for the user, or for a moment one that finished or
// failed as it did.
func progPilled(s *session) (terminal.StatusRecord, bool) {
	r, busy := progStatus(s)
	switch {
	case !busy:
		return r, false
	case r.State == terminal.StatusWorking, r.State == terminal.StatusBlocked:
		return r, true
	}
	return r, progShown(s.statusFrom) && time.Since(s.statusSince) < progFlashFor
}

// progShown reports whether the pill shows a record in state st.
func progShown(st terminal.StatusState) bool {
	return st == terminal.StatusWorking || st == terminal.StatusBlocked
}

// statusIn is the newest status of a terminal's programs that waits for
// the app to take it, so that a program that says what it does many
// times a frame has the view built once for it.
type statusIn struct {
	mu  sync.Mutex
	st  terminal.ProgramStatus
	set bool
}

// put keeps st in place of any that waits, and reports whether none did,
// when taking it has to be posted.
func (q *statusIn) put(st terminal.ProgramStatus) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	first := !q.set
	q.st, q.set = st, true
	return first
}

// take returns the status that waits, and whether one did.
func (q *statusIn) take() (terminal.ProgramStatus, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	st, ok := q.st, q.set
	q.st, q.set = terminal.ProgramStatus{}, false
	return st, ok
}

// programStatus takes what a terminal's programs say they do, unless the
// terminal was replaced since. As the record most in need of the user
// comes to finish or fail, the project's square on the rail is marked,
// with what it says; one that waits marks it for as long as it waits, as
// projectWork tells.
func (a *app) programStatus(s *session, term *terminal.Terminal, st terminal.ProgramStatus) {
	if s.term != term {
		return
	}
	old, had := s.status.Top()
	s.status = st
	r, ok := st.Top()
	if !ok || had && old.ID == r.ID && old.State == r.State {
		return
	}
	s.statusSince, s.statusFrom = time.Now(), terminal.StatusIdle
	if had {
		s.statusFrom = old.State
	}
	switch r.State {
	case terminal.StatusBlocked:
		// The view again once the badge stops calling.
		bg.AfterFunc(pulseFor, func() { a.post(func() {}) })
	case terminal.StatusDone, terminal.StatusError:
		if progShown(s.statusFrom) {
			// The view again once the pill stops saying so.
			bg.AfterFunc(progFlashFor, func() { a.post(func() {}) })
		}
		a.alert, a.alertState, a.alertAt = progLine(r), r.State, time.Now()
	}
}

// railWork is what the programs of a project's terminals do now, for its
// square on the rail: the records that work, those that wait for the
// user, when the last terminal of those whose record most in need of
// the user waits began to, and how far along the work is, from 0 to 1,
// while each record that works says, -1 otherwise.
type railWork struct {
	working, blocked []terminal.StatusRecord
	blockedSince     time.Time
	progress         float32
}

// projectWork is what the programs of a's terminals do now, in the panel,
// at a side or in tabs, but for those of terminals whose shell starts in
// herdr-core or waits for its machine, which say what they did before.
func projectWork(a *app) railWork {
	var w railWork
	for _, s := range a.terms {
		if st := termStatus(s); st == termStarting || st == termWaiting {
			continue
		}
		for _, r := range s.status.Records {
			switch r.State {
			case terminal.StatusWorking:
				w.working = append(w.working, r)
			case terminal.StatusBlocked:
				w.blocked = append(w.blocked, r)
			}
		}
		if r, ok := s.status.Top(); ok && r.State == terminal.StatusBlocked && s.statusSince.After(w.blockedSince) {
			w.blockedSince = s.statusSince
		}
	}
	w.progress = workProgress(w.working)
	return w
}

// workProgress is how far along the records that work are together, the
// mean of what they say from 0 to 1, or -1 when there are none or one
// does not say.
func workProgress(rs []terminal.StatusRecord) float32 {
	if len(rs) == 0 {
		return -1
	}
	sum := 0
	for _, r := range rs {
		if r.Progress < 0 {
			return -1
		}
		sum += min(r.Progress, 100)
	}
	return float32(sum) / float32(100*len(rs))
}

// workTurn is how long the ring of a project's square on the rail takes
// to turn once while its programs work, workBreath how long its arc takes
// to grow and shrink again, and workFPS how often it is painted again,
// without building the view, which is enough for a ring this small.
const (
	workTurn   = 1100 * time.Millisecond
	workBreath = 1800 * time.Millisecond
	workFPS    = 40
)

// workRing paints a ring of col of radius r around cx, cy: a faint track
// and, from the top, done of it, while done says how far the work is, or
// an arc that turns, growing and shrinking as it does. It stays still for
// those who ask for less motion.
func workRing(pt *ui.Painter, cx, cy, r float32, col ui.Color, done float32, calm bool) {
	var track ui.Path
	track.Circle(cx, cy, r)
	pt.StrokePath(&track, 1.75, col.Alpha(0.25))
	// The circle starts at its right, a quarter past its top.
	if done >= 0 {
		workArc(pt, cx, cy, r, col, 0.75, done)
		return
	}
	from, length := float32(0.75), float32(0.25)
	if !calm {
		now := pt.Now().UnixNano()
		turn := float32(now%int64(workTurn)) / float32(workTurn)
		breath := float64(now%int64(workBreath)) / float64(workBreath)
		length = 0.18 + 0.14*float32(1-math.Cos(2*math.Pi*breath))
		from = turn
		pt.AnimationFrameRate(workFPS)
	}
	workArc(pt, cx, cy, r, col, from, length)
}

// workArc strokes length of the circle of radius r around cx, cy from
// from, as fractions of it, in two where it goes past where the circle
// starts.
func workArc(pt *ui.Painter, cx, cy, r float32, col ui.Color, from, length float32) {
	if length <= 0.005 {
		return
	}
	from -= float32(math.Floor(float64(from)))
	length = min(length, 1)
	for _, span := range [][2]float32{{from, min(from+length, 1)}, {0, max(from+length-1, 0)}} {
		if span[1] <= span[0] {
			continue
		}
		var arc ui.Path
		arc.Circle(cx, cy, r).Trim(span[0], span[1])
		pt.StrokePath(&arc, 1.75, col)
	}
}

// seeProgramStatus tells a terminal that the user, typing in it, saw the
// work of its programs that finished or failed.
func (a *app) seeProgramStatus(s *session) {
	for _, r := range s.status.Records {
		if r.State == terminal.StatusDone || r.State == terminal.StatusError {
			s.term.SeeProgramStatus()
			return
		}
	}
}

// pulseFor is how long a terminal's badge sends out rings as it begins to
// wait, whole beats of them: after, it stays still, so that a machine
// away for hours doesn't have the window paint all the while.
const pulseFor = 6 * pulseBeat

// pulseBeat is how often the badge of a terminal that waits sends out a
// ring.
const pulseBeat = 1400 * time.Millisecond

// beginWait notes that a terminal begins to wait, and builds the view
// again once its badge and pill stop moving.
func (a *app) beginWait(s *session) {
	s.waitSince = time.Now()
	bg.AfterFunc(pulseFor, func() { a.post(func() {}) })
}

// waitedLong reports whether a terminal has waited longer than its badge
// moves for.
func waitedLong(s *session) bool { return time.Since(s.waitSince) >= pulseFor }

// hcReadyFor is how long a terminal says its shell in herdr-core is
// ready, once it attached having started or waited.
const hcReadyFor = 1600 * time.Millisecond

// hcJustReady reports whether a terminal's shell in herdr-core attached
// a moment ago, having started or waited.
func hcJustReady(s *session) bool {
	return s != nil && termStatus(s) == termKept && time.Since(s.hcReady) < hcReadyFor
}

// termIcon is a terminal's icon of size on bg, the color under it, with
// the badge of its state at its lower right corner: a dot cut out of the
// icon by a ring of bg, steady while its shell runs in herdr-core,
// sending out rings while it starts or waits, and once as it gets there.
// The badge grows in and shrinks away as the state comes and goes, and
// its color follows the state.
func (a *app) termIcon(c *ui.Context, s *session, size float32, tint, bg ui.Color) {
	p := a.pal
	st := termStatus(s)
	prog, busy := progStatus(s)
	// Starting or waiting for its shell comes first: the program's
	// status is old while it does.
	busy = busy && st != termStarting && st != termWaiting
	box := ui.Box(c).Size(size, size).Shrink(0)
	switch {
	case busy:
		box.Tooltip(progTip(s.status)).Label(progLine(prog))
	case st == termKept:
		box.Tooltip(locales.HcRunsIn(s.hc)).Label(locales.HcRunsIn(s.hc))
	case st == termStarting, st == termWaiting:
		box.Tooltip(termWait(s)).Label(termWait(s))
	}
	box.Children(func() {
		ui.Icon(c, iconTerminal).FontSize(size).TextColor(tint)
		if st == termHere && !busy {
			return
		}
		col := p.addedFg
		switch {
		case busy:
			col = a.progColor(prog.State)
		case st == termStarting:
			col = p.accent
		case st == termWaiting:
			col = p.modifiedFg
		}
		const d, cut = float32(6), float32(1.5)
		badge := ui.Box(c.Key("dot")).Absolute().Right(-2-cut).Bottom(-1-cut).Size(d+2*cut, d+2*cut).Transition(dotPop)
		col = badge.AnimateColor("col", col, ui.Smooth)
		bg = badge.AnimateColor("bg", bg, ui.Animation{Duration: hoverFade.Duration})
		// The rings stay still for those who ask for less motion.
		calm := c.Preferences().ReduceMotion
		var pulse time.Time
		switch {
		case calm:
		case busy:
			// A program that waits for the user calls for a while.
			if prog.State == terminal.StatusBlocked && time.Since(s.statusSince) < pulseFor {
				pulse = s.statusSince
			}
		case (st == termStarting || st == termWaiting) && !waitedLong(s):
			pulse = s.waitSince
		}
		var ready time.Time
		if !calm && hcJustReady(s) {
			ready = s.hcReady
		}
		badge.Draw(func(pt *ui.Painter, r ui.Rect) { statusDot(pt, r, cut, col, bg, pulse, ready) })
	})
}

// statusDot paints a dot of col in r, inside a ring of bg cut wide. For
// pulseFor from pulse, unless it is zero, rings swell from the dot and
// fade, as a beacon, at 30 frames a second; a moment after ready, one
// ring does, quicker and at 60, as the shell gets there. The window
// paints them without building the view.
func statusDot(pt *ui.Painter, r ui.Rect, cut float32, col, bg ui.Color, pulse, ready time.Time) {
	dot := ui.Rect{X: r.X + cut, Y: r.Y + cut, W: r.W - 2*cut, H: r.H - 2*cut}
	switch since := pt.Now().Sub(ready); {
	case beacon(pt, dot, col, pulse, 0.9, 0.45):
	case !ready.IsZero() && since < hcBurst:
		ring(pt, dot, col, float32(since)/float32(hcBurst), 1.3, 0.6)
		pt.AnimationFrameRate(60)
	}
	pt.Fill(r, bg, r.W/2)
	pt.Fill(dot, col, dot.W/2)
}

// beacon paints, for pulseFor from pulse unless it is zero, a ring of col
// that swells from dot and fades every pulseBeat, at 30 frames a second,
// and reports whether it did. The badge of a terminal and the icon of its
// pill beat together, as they go by the same time.
func beacon(pt *ui.Painter, dot ui.Rect, col ui.Color, pulse time.Time, reach, alpha float32) bool {
	waited := pt.Now().Sub(pulse)
	if pulse.IsZero() || waited >= pulseFor {
		return false
	}
	ring(pt, dot, col, float32(waited%pulseBeat)/float32(pulseBeat), reach, alpha)
	pt.AnimationFrameRate(30)
	return true
}

// ring paints a ring of col swollen from dot by reach of its width, t of
// the way, fading as it goes.
func ring(pt *ui.Painter, dot ui.Rect, col ui.Color, t, reach, alpha float32) {
	grow := dot.W * reach * easeOutCubic(t)
	o := ui.Rect{X: dot.X - grow, Y: dot.Y - grow, W: dot.W + 2*grow, H: dot.H + 2*grow}
	pt.Fill(o, col.Alpha(alpha*(1-t)), o.W/2)
}

// hcBurst is how long the ring takes that a terminal's badge sends out as
// its shell gets to herdr-core.
const hcBurst = 700 * time.Millisecond

func easeOutCubic(t float32) float32 {
	return 1 - float32(math.Pow(float64(1-t), 3))
}

// hcMenu adds to the menu of a terminal whose shell runs in herdr-core
// an item that copies its address, for hc attach elsewhere.
func (a *app) hcMenu(m *ui.Menu, s *session) {
	if s.hc == "" {
		return
	}
	if m.Item(locales.TermCopyHC()).Chosen() {
		addr := s.hc
		a.post(func() {
			a.services.WriteClipboard(addr)
			a.tell(note{message: locales.CopiedText(addr)})
		})
	}
}

// pillMotion is how the pill over a terminal comes, changes and goes: it
// drops in, its width follows what it says, and it rises away.
var pillMotion = ui.ElementTransition{Duration: motionShow, Colors: true, Size: true, Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{Y: -4}}

// waitPill is the pill over a terminal that waits, at its upper right:
// a spinner in the color of its badge and what it waits for, which turns
// into a still icon once it waited a while. Once its
// shell in herdr-core gets there, the spinner turns into a check and the
// pill says so for a moment, then rises away. While the terminal waits
// for nothing, the pill says what its programs do.
func (a *app) waitPill(c *ui.Context, s *session) {
	msg, ready := termWait(s), hcJustReady(s)
	if msg == "" && !ready {
		a.progPill(c, s)
		return
	}
	p := a.pal
	col := p.accent
	switch {
	case ready:
		msg, col = locales.HcReady(), p.addedFg
	case termStatus(s) == termWaiting:
		col = p.modifiedFg
	}
	box := ui.Row(c.Key("term-wait")).Absolute().Top(8).Right(14).Padding(4, 10, 4, 8).Gap(7).Radius(11).AlignItems(ui.Center).Clip().Label(msg)
	a.floating(c, box, false).Transition(pillMotion).Children(func() {
		ui.Box(c).Size(11, 11).Shrink(0).Children(func() {
			switch {
			case ready:
				ui.Icon(c.Key("ok"), iconCheck).FontSize(11).TextColor(col).Transition(dotPop)
			case waitedLong(s) && termStatus(s) == termWaiting:
				// Still after a while, as the badge, for a machine away
				// for long.
				ui.Icon(c.Key("away"), iconWifiOff).FontSize(11).TextColor(col).Transition(dotPop)
			case waitedLong(s):
				ui.Icon(c.Key("slow"), iconClock).FontSize(11).TextColor(col).Transition(dotPop)
			default:
				ui.Spinner(c.Key("spin")).Size(11, 11).TextColor(col).Transition(dotPop)
			}
		})
		ui.Text(c, msg).FontSize(11.5).TextColor(p.muted).SingleLine().MaxWidth(320).ContentTransition(pillText)
	})
}

// pillText is how what the pill over a terminal says changes: it fades
// across.
var pillText = ui.ContentTransition{Animation: &ui.Animation{Duration: motionShow}}

// progBar is how the bar of the pill over a terminal follows the
// progress of the work it shows.
const progBar = 180 * time.Millisecond

// progPill is the pill over a terminal whose program works or waits for
// the user, in the place of waitPill's: the icon of its state, who says
// so and what, cut to fit, its progress in figures and as a bar along
// the pill's foot, and how many more of its records are not idle. One
// that waits for the user is ringed in its color, and its icon calls as
// the badge does. As the work finishes or fails, the pill says so for a
// moment, then rises away.
func (a *app) progPill(c *ui.Context, s *session) {
	r, ok := progPilled(s)
	if !ok {
		return
	}
	p := a.pal
	col := a.progColor(r.State)
	who, what := progParts(r)
	box := ui.Row(c.Key("term-wait")).Absolute().Top(8).Right(14).Padding(4, 10, 4, 8).Gap(6).Radius(11).AlignItems(ui.Center).Clip().Label(progLine(r))
	a.floating(c, box, false)
	var pulse time.Time
	if r.State == terminal.StatusBlocked {
		box.Border(1, col.Alpha(0.55))
		if !c.Preferences().ReduceMotion {
			pulse = s.statusSince
		}
	}
	box.Transition(pillMotion).Children(func() {
		mark := ui.Box(c.Key("mark")).Size(11, 11).Shrink(0)
		mark.Draw(func(pt *ui.Painter, b ui.Rect) {
			const d = 5
			beacon(pt, ui.Rect{X: b.X + (b.W-d)/2, Y: b.Y + (b.H-d)/2, W: d, H: d}, col, pulse, 1.4, 0.35)
		})
		mark.Children(func() {
			switch {
			case r.State == terminal.StatusBlocked && r.Kind == terminal.KindAuth:
				ui.Icon(c.Key("auth"), iconLock).FontSize(11).TextColor(col).Transition(dotPop)
			case r.State == terminal.StatusBlocked:
				ui.Icon(c.Key("ask"), iconMessageAlert).FontSize(11).TextColor(col).Transition(dotPop)
			case r.State == terminal.StatusDone:
				ui.Icon(c.Key("ok"), iconCheck).FontSize(11).TextColor(col).Transition(dotPop)
			case r.State == terminal.StatusError:
				ui.Icon(c.Key("fail"), iconX).FontSize(11).TextColor(col).Transition(dotPop)
			default:
				ui.Spinner(c.Key("spin")).Size(11, 11).TextColor(col).Transition(dotPop)
			}
		})
		if who != "" {
			ui.Text(c.Key("who"), who).FontSize(11.5).FontWeight(600).TextColor(p.text).SingleLine().MaxWidth(120)
		}
		ui.Text(c.Key("what"), what).FontSize(11.5).TextColor(p.muted).SingleLine().MaxWidth(220).ContentTransition(pillText)
		if r.Progress >= 0 {
			ui.Text(c.Key("pct"), fmt.Sprintf("%d%%", r.Progress)).FontSize(11).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0)
		}
		if n := progOthers(s.status); n > 0 {
			ui.Text(c.Key("more"), fmt.Sprintf("+%d", n)).FontSize(10.5).FontWeight(600).FontFeatures("tnum").TextColor(p.muted).
				Padding(0, 5, 0, 5).Radius(7).Background(p.faint.Alpha(0.18)).SingleLine().Shrink(0).Transition(dotPop)
		}
		if r.Progress >= 0 {
			bar := ui.Box(c.Key("bar")).Absolute().Left(0).Right(0).Bottom(0).Height(2).Transition(ui.ElementTransition{Duration: progBar})
			done := bar.Animate("done", float32(r.Progress)/100, progBar)
			bar.Draw(func(pt *ui.Painter, b ui.Rect) {
				pt.Fill(b, col.Alpha(0.16), 0)
				pt.Fill(ui.Rect{X: b.X, Y: b.Y, W: b.W * done, H: b.H}, col, 0)
			})
		}
	})
}

// hcBusy is what the status bar says while hc installs or a host is
// added to it, which take a while: "" while neither does.
func hcBusy() string {
	switch {
	case hcInstalling:
		return locales.HcInstalling()
	case hcAdding != "":
		return locales.HcAdding(hcAdding)
	}
	return ""
}
