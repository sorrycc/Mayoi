package main

import (
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// How often the checks of a commit are asked of GitHub again: while they
// run, soon; a commit of the last day with none yet may be starting them;
// the others seldom, as only a job run again changes them. One read asks
// about commitChecksMax commits at most, the newest first.
const (
	commitChecksRunningEvery = 30 * time.Second
	commitChecksNewEvery     = 2 * time.Minute
	commitChecksDoneEvery    = 10 * time.Minute
	commitChecksNewFor       = 24 * time.Hour
	commitChecksMax          = 200
)

// refreshCommitChecks asks GitHub in the background how the checks of the
// pushed commits of the history went, and of those a commit's tab shows:
// those never asked, still running, or asked long ago, as the history is
// read, or as the answer gets old; only while the window shows the
// project. What finished is kept, so most reads ask about few commits,
// and many none.
func (a *gitContext) refreshCommitChecks() {
	g := a.git
	if !a.live() || len(g.history) == 0 && len(g.checksWanted) == 0 || !a.app.online() || !a.app.host.shown() {
		return
	}
	r := g.repo
	key := commitChecksAsked{r, g.historyGen, g.refsGen}
	if !g.commitChecksPoll.Start(key, a.commitChecksEvery()) {
		return
	}
	if g.commitChecksRepo != r {
		g.commitChecks, g.commitChecksRead, g.commitChecksRepo, g.headChecksRunning = nil, nil, r, ""
		g.commitChecksRunning = false
	}
	hashes := a.commitChecksDue(time.Now())
	if len(hashes) == 0 {
		g.commitChecksPoll.Done(key)
		g.commitChecksPoll.Keep(key, a.commitChecksEvery(), a.app.invalidate)
		return
	}
	bg.Go(func() {
		defer recoverCrash()
		got, err := r.CommitChecks(hashes)
		a.app.post(func() {
			if !g.commitChecksPoll.Done(key) || g.repo != r || g.commitChecksRepo != r {
				return
			}
			switch {
			case err == nil:
				if g.commitChecks == nil {
					g.commitChecks, g.commitChecksRead = map[string]git.CommitStatus{}, map[string]time.Time{}
				}
				now := time.Now()
				for _, h := range hashes {
					if st, ok := got[h]; ok {
						g.commitChecks[h] = st
					} else {
						delete(g.commitChecks, h)
					}
					g.commitChecksRead[h] = now
					delete(g.checksWanted, h)
				}
				g.commitChecksOff, g.ghOK, g.commitChecksRunning = false, true, false
				for _, st := range g.commitChecks {
					g.commitChecksRunning = g.commitChecksRunning || !st.Done()
				}
				a.headChecksChanged()
			case errors.Is(err, remote.ErrOffline):
				// The host is away: what was read stays until it is back.
			default:
				g.commitChecksOff = true
				clear(g.checksWanted)
			}
			g.commitChecksPoll.Keep(key, a.commitChecksEvery(), a.app.invalidate)
		})
	})
}

// commitChecksEvery is how soon the checks are asked for again: soon
// while a commit's run, seldom once gh could not tell.
func (a *gitContext) commitChecksEvery() time.Duration {
	g := a.git
	if g.commitChecksOff {
		return prOffEvery
	}
	if g.commitChecksRunning {
		return commitChecksRunningEvery
	}
	return commitChecksNewEvery
}

// commitChecksDue are the commits whose checks are to be asked for at
// now: those a tab wants, then the pushed commits of the history, newest
// first, never asked, or whose answer is older than how soon it may
// change.
func (a *gitContext) commitChecksDue(now time.Time) []string {
	g := a.git
	// A wake comes as the soonest is due, give or take a little.
	const slack = 2 * time.Second
	due := func(h string, made time.Time) bool {
		at, ok := g.commitChecksRead[h]
		if !ok {
			return true
		}
		age := now.Sub(at) + slack
		st, has := g.commitChecks[h]
		switch {
		case has && !st.Done():
			return age >= commitChecksRunningEvery
		case !has && !made.IsZero() && now.Sub(made) < commitChecksNewFor:
			return age >= commitChecksNewEvery
		}
		return age >= commitChecksDoneEvery
	}
	var out []string
	for h := range g.checksWanted {
		if len(out) < commitChecksMax && due(h, time.Time{}) {
			out = append(out, h)
		}
	}
	for _, cm := range g.history {
		if len(out) >= commitChecksMax {
			break
		}
		if !g.unpushed[cm.Hash] && !g.checksWanted[cm.Hash] && due(cm.Hash, cm.Time) {
			out = append(out, cm.Hash)
		}
	}
	return out
}

// checksOf is how the checks of commit hash went, as last read; false
// while not known, or when it has none.
func (a *gitContext) checksOf(hash string) (git.CommitStatus, bool) {
	g := a.git
	if g.commitChecksRepo != g.repo || g.repo == nil {
		return git.CommitStatus{}, false
	}
	st, ok := g.commitChecks[hash]
	return st, ok
}

// hasCommitChecks is whether a commit of the repository has checks, for
// the rows of the history to keep room for their icon.
func (a *gitContext) hasCommitChecks() bool {
	g := a.git
	return g.commitChecksRepo == g.repo && len(g.commitChecks) > 0
}

// wantChecks has the checks of commit hash read, as of a commit a tab
// shows that the history may not have.
func (a *gitContext) wantChecks(hash string) {
	g := a.git
	if g.commitChecksOff || g.repo == nil || g.commitChecksRepo == g.repo && !g.commitChecksRead[hash].IsZero() || g.checksWanted[hash] {
		return
	}
	if g.checksWanted == nil {
		g.checksWanted = map[string]bool{}
	}
	g.checksWanted[hash] = true
	g.commitChecksPoll.Expire()
	a.app.post(a.refreshCommitChecks)
}

// recheckCommit has the checks of commit hash read again at once, as
// after a job of it was run again.
func (a *gitContext) recheckCommit(hash string) {
	g := a.git
	delete(g.commitChecksRead, hash)
	g.commitChecksPoll.Expire()
	a.refreshCommitChecks()
}

// headChecksChanged tells once the checks of the commit at HEAD, seen
// running, end: with a toast while the window has the keys, else with a
// notification of the system.
func (a *gitContext) headChecksChanged() {
	g := a.git
	if g.status == nil {
		return
	}
	head := g.status.Head
	st, ok := g.commitChecks[head]
	if ok && !st.Done() {
		g.headChecksRunning = head
		return
	}
	if g.headChecksRunning != head {
		return
	}
	g.headChecksRunning = ""
	if !ok {
		return
	}
	short := git.ShortHash(head)
	msg := locales.ChecksPassedOn(short)
	if st.Failed > 0 {
		msg = locales.ChecksFailedOn(st.Failed, short)
	}
	show := func() { a.openCommitChecks(head) }
	if a.app.host.focused() {
		n := note{message: msg}
		if st.Failed > 0 {
			n.label, n.action = locales.PrShowChecks(), show
		}
		a.app.tell(n)
		return
	}
	subject := ""
	for _, cm := range g.history {
		if cm.Hash == head {
			subject = cm.Subject
			break
		}
	}
	a.app.host.notify(msg, subject, show)
}

// checksSummary says how many of a commit's checks passed, failed and
// run, as "21 checks passed · 1 check failed".
func checksSummary(st git.CommitStatus) string {
	var parts []string
	if st.Passed > 0 {
		parts = append(parts, locales.PrChecksPassed(st.Passed))
	}
	if st.Failed > 0 {
		parts = append(parts, locales.PrChecksFailed(st.Failed))
	}
	if st.Pending > 0 {
		parts = append(parts, locales.PrChecksPending(st.Pending))
	}
	return strings.Join(parts, " · ")
}

// commitChecksIcon is the icon of how a commit's checks went, in the
// history's row of the commit, which a click lists them from. It keeps
// its room in a row without checks, so that the rows line up.
func (a *gitContext) commitChecksIcon(c *ui.Context, hash string, action *func()) {
	p := a.app.pal
	st, ok := a.checksOf(hash)
	if !ok {
		ui.Box(c).Size(14, 16).Shrink(0)
		return
	}
	icon, col := prChecksIcon(p, st.Checks)
	tip := checksSummary(st)
	b := ui.ButtonBase(c.Key("checks:"+hash)).Size(14, 16).Shrink(0).Radius(3).Justify(ui.Center).AlignItems(ui.Center).
		Label(locales.PrShowChecks()).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() { ui.Icon(c, icon).FontSize(11).TextColor(col) })
	if b.Clicked() {
		*action = func() { a.openCommitChecks(hash) }
	}
}

// headChecksBadge shows how the checks of HEAD went in the status bar,
// while the branch has no pull request, whose badge says so; a click
// lists them.
func (a *gitContext) headChecksBadge(c *ui.Context) {
	st, ok := a.headChecks()
	if !ok {
		return
	}
	p := a.app.pal
	tip := locales.ChecksOfCommit(git.ShortHash(a.git.status.Head)) + " · " + checksSummary(st)
	icon, _ := prChecksIcon(p, st.Checks)
	b := ui.ButtonBase(c.Key("status:checks")).Height(statusBarH-6).Padding(0, 6).Radius(4).AlignItems(ui.Center).
		Shrink(0).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() { ui.Icon(c, icon).FontSize(11).TextColor(p.statusFg) })
	if b.Clicked() {
		a.app.post(a.openChecks)
	}
}

// headChecks is how the checks of HEAD went, while the branch, which the
// status bar shows them beside, has no pull request to show them.
func (a *gitContext) headChecks() (git.CommitStatus, bool) {
	g := a.git
	if g.status == nil || g.status.Head == "" || g.status.Branch == "" || a.currentPR() != nil || g.unpushed[g.status.Head] {
		return git.CommitStatus{}, false
	}
	return a.checksOf(g.status.Head)
}
