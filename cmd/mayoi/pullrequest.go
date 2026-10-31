package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// How often the pull request of the branch is asked for again: sooner
// while its checks run, and seldom once gh could not tell.
const (
	prEvery        = 60 * time.Second
	prPendingEvery = 30 * time.Second
	prOffEvery     = 10 * time.Minute
	prHeadsEvery   = 2 * time.Minute
)

// refreshPR asks gh in the background for the pull request of the branch
// checked out, as the branch or its commit changes, or as the last answer
// gets old; only while the window shows the project.
func (a *gitContext) refreshPR() {
	g := a.git
	st := g.status
	if a.live() && st != nil && st.Branch == "" && !g.ghProbed && a.app.online() {
		// Detached, there is no pull request to ask for: only whether gh
		// answers, for the pull requests' commands.
		g.ghProbed = true
		r := g.repo
		bg.Go(func() {
			defer recoverCrash()
			err := r.GitHubReady()
			a.app.post(func() {
				if g.repo == r && !errors.Is(err, remote.ErrOffline) {
					g.ghOK = err == nil
				}
			})
		})
	}
	if !a.live() || st == nil || st.Branch == "" || g.prRunning || !a.app.online() || !a.app.host.shown() {
		return
	}
	key := st.Branch + "\x00" + st.Head + "\x00" + st.Upstream
	if g.prRepo == g.repo && key == g.prOf && time.Since(g.prRead) < a.prEvery() {
		return
	}
	g.prRunning = true
	r, branch := g.repo, st.Branch
	bg.Go(func() {
		defer recoverCrash()
		pr, err := r.PullRequest()
		a.app.post(func() {
			if g.repo != r {
				return
			}
			g.prRunning = false
			if g.status == nil || g.status.Branch != branch {
				// Switched meanwhile: the next frame asks for the new one.
				return
			}
			g.prRepo, g.prBranch, g.prOf, g.prRead = r, branch, key, time.Now()
			switch {
			case err == nil:
				g.pr, g.prOff, g.ghOK = pr, false, true
			case errors.Is(err, git.ErrNoPR):
				g.pr, g.prOff, g.ghOK = nil, false, true
			case errors.Is(err, remote.ErrOffline):
				// The host is away: what was read stays until it is back.
			default:
				g.pr, g.prOff, g.ghOK = nil, true, false
			}
			if g.prWake != nil {
				g.prWake.Stop()
			}
			g.prWake = time.AfterFunc(a.prEvery(), a.app.invalidate)
		})
	})
}

// refreshPRHeads asks gh in the background for the pull requests open, by
// the commit at their head, for the history to mark: once it is read, as
// the refs change, or as the last answer gets old; only while the window
// shows the project. The history never waits for it.
func (a *gitContext) refreshPRHeads() {
	g := a.git
	if !a.live() || len(g.history) == 0 || g.prHeadsRunning || !a.app.online() || !a.app.host.shown() {
		return
	}
	every := prHeadsEvery
	if g.prHeadsOff {
		every = prOffEvery
	}
	if g.prHeadsRepo == g.repo && g.prHeadsOf == g.refsGen && time.Since(g.prHeadsRead) < every {
		return
	}
	if g.prHeadsRepo != g.repo {
		g.prHeads = nil
	}
	g.prHeadsRunning = true
	r, refs := g.repo, g.refsGen
	bg.Go(func() {
		defer recoverCrash()
		heads, err := r.PullRequestHeads()
		a.app.post(func() {
			g.prHeadsRunning = false
			if g.repo != r {
				return
			}
			g.prHeadsRepo, g.prHeadsOf, g.prHeadsRead = r, refs, time.Now()
			switch {
			case err == nil:
				g.prHeads, g.prHeadsOff = heads, false
			case errors.Is(err, remote.ErrOffline):
				// The host is away: what was read stays until it is back.
			default:
				g.prHeads, g.prHeadsOff = nil, true
			}
			every := prHeadsEvery
			if g.prHeadsOff {
				every = prOffEvery
			}
			if g.prHeadsWake != nil {
				g.prHeadsWake.Stop()
			}
			g.prHeadsWake = time.AfterFunc(every, a.app.invalidate)
		})
	})
}

// prHeadsAt is the pull requests open whose head is commit hash.
func (a *gitContext) prHeadsAt(hash string) []git.PRHead {
	g := a.git
	if g.prHeadsRepo != g.repo {
		return nil
	}
	return g.prHeads[hash]
}

func (a *gitContext) prEvery() time.Duration {
	g := a.git
	switch {
	case g.prOff:
		return prOffEvery
	case g.pr != nil && g.pr.Checks == git.ChecksPending:
		return prPendingEvery
	}
	return prEvery
}

// currentPR is the pull request of the branch checked out, nil while none
// is known.
func (a *gitContext) currentPR() *git.PR {
	g := a.git
	if g.pr == nil || g.prRepo != g.repo || g.status == nil || g.status.Branch != g.prBranch {
		return nil
	}
	return g.pr
}

// prBadge shows the pull request of the branch, its number, how its
// checks went and its review, whose changes a click opens in a tab; a
// click on the icon of the checks lists them. In the status bar, it is in
// the bar's color, and short says only the number. It returns the badge,
// nil without a pull request, for a row to measure.
func (a *gitContext) prBadge(c *ui.Context, inStatus, short bool) *ui.Element {
	pr := a.currentPR()
	if pr == nil {
		return nil
	}
	p := a.app.pal
	fg, size, key, h := p.muted, float32(11.5), "scm-pr", float32(22)
	if inStatus {
		fg, key, h = p.statusFg, "status:pr", statusBarH-6
	}
	icon, col := prChecksIcon(p, pr.Checks)
	if inStatus {
		col = fg
	}
	b := ui.ButtonBase(c.Key(key)).Height(h).Padding(0, 6).Gap(4).Radius(4).AlignItems(ui.Center).
		Shrink(0).Label(prTip(pr)).Tooltip(prTip(pr))
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		text := func(s string) { ui.Text(c, s).FontSize(size).TextColor(fg).SingleLine() }
		text("#" + strconv.Itoa(pr.Number))
		if short {
			return
		}
		if icon != nil {
			text("·")
			// The icon of the checks lists them.
			ib := ui.ButtonBase(c.Key(key+":checks")).Height(h-4).Padding(0, 2).Radius(3).AlignItems(ui.Center).
				Label(locales.PrShowChecks()).Tooltip(locales.PrShowChecks())
			if ib.Hovered() {
				ib.Background(p.hover)
			}
			ib.Children(func() { ui.Icon(c, icon).FontSize(11).TextColor(col) })
			if ib.Clicked() {
				a.app.post(a.openChecks)
			}
		}
		if review := prReviewLabel(pr.Review); review != "" {
			text("·")
			text(review)
		}
	})
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.PrOpenChanges()).Chosen() {
			a.app.post(func() { a.openPullRequest(pr.Number) })
		}
		if m.Item(locales.PrShowChecks()).Disabled(pr.Checks == git.ChecksNone).Chosen() {
			a.app.post(a.openChecks)
		}
		if m.Item(locales.PrOpenOnGitHub()).Disabled(pr.URL == "").Chosen() {
			a.app.post(func() { a.app.services.OpenURL(pr.URL) })
		}
	})
	if b.Clicked() {
		a.app.post(func() { a.openPullRequest(pr.Number) })
	}
	return &b
}

// prBadgeLabel is what the badge says, for the status bar to measure.
func prBadgeLabel(pr *git.PR) string {
	s := "#" + strconv.Itoa(pr.Number)
	if pr.Checks != git.ChecksNone {
		s += " · ✓"
	}
	if review := prReviewLabel(pr.Review); review != "" {
		s += " · " + review
	}
	return s
}

func prChecksIcon(p *palette, checks git.Checks) (*ui.SVG, ui.Color) {
	switch checks {
	case git.ChecksPass:
		return iconCheck, p.addedFg
	case git.ChecksFail:
		return iconX, p.dangerFg
	case git.ChecksPending:
		return iconClock, p.modifiedFg
	}
	return nil, p.muted
}

func prReviewLabel(review string) string {
	switch review {
	case "APPROVED":
		return locales.PrApproved()
	case "CHANGES_REQUESTED":
		return locales.PrChangesRequested()
	case "REVIEW_REQUIRED":
		return locales.PrReviewRequired()
	}
	return ""
}

// prTip says the state of the pull request, then its checks and review.
func prTip(pr *git.PR) string {
	state := locales.PrOpen()
	switch pr.State {
	case "MERGED":
		state = locales.PrMerged()
	case "CLOSED":
		state = locales.PrClosed()
	}
	parts := []string{locales.PrTip("#"+strconv.Itoa(pr.Number), state)}
	if pr.Passed > 0 {
		parts = append(parts, locales.PrChecksPassed(pr.Passed))
	}
	if pr.Failed > 0 {
		parts = append(parts, locales.PrChecksFailed(pr.Failed))
	}
	if pr.Pending > 0 {
		parts = append(parts, locales.PrChecksPending(pr.Pending))
	}
	if review := prReviewLabel(pr.Review); review != "" {
		parts = append(parts, review)
	}
	return strings.Join(parts, " · ")
}

// prHeadChip shows a pull request open whose head is a commit of the
// history, in its lane's color, a draft's in the plain colors; a click
// opens its changes in a tab.
func (a *gitContext) prHeadChip(c *ui.Context, hash string, pr git.PRHead, lane ui.Color, maxW float32, action *func()) {
	p := a.app.pal
	fg, bg := lane, lane.Alpha(0.16)
	if pr.Draft {
		fg, bg = p.muted, p.hover
	}
	tip := prHeadTip(pr)
	b := ui.ButtonBase(c.Key("prhead:"+hash+":"+strconv.Itoa(pr.Number))).Height(16).Padding(0, 5, 0, 4).Gap(3).
		Radius(4).AlignItems(ui.Center).MaxWidth(maxW).Shrink(0).Label(tip).Tooltip(tip)
	if b.Hovered() {
		bg = mix(bg, p.hover, 0.5)
	}
	b.Background(bg)
	b.Children(func() {
		ui.Icon(c, iconPullRequest).FontSize(10).TextColor(fg).Shrink(0)
		ui.Text(c, "#"+strconv.Itoa(pr.Number)).FontSize(11).FontWeight(600).TextColor(fg).SingleLine().Shrink(1)
	})
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.PrOpenChanges()).Chosen() {
			*action = func() { a.openPullRequest(pr.Number) }
		}
		if m.Item(locales.PrOpenOnGitHub()).Disabled(pr.URL == "").Chosen() {
			*action = func() { a.app.services.OpenURL(pr.URL) }
		}
	})
	if b.Clicked() {
		*action = func() { a.openPullRequest(pr.Number) }
	}
}

// prHeadTip says which pull request a commit is the head of, and its title.
func prHeadTip(pr git.PRHead) string {
	s := locales.PrHeadTip("#"+strconv.Itoa(pr.Number), pr.Title)
	if pr.Draft {
		s += " · " + locales.PrDraft()
	}
	return s
}
