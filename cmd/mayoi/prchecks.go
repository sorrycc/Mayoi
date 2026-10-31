package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"
)

// How many lines of a failed job's log go to the terminal, and how often
// the box reads the checks again while one runs.
const (
	checkLogLines = 3000
	checksEvery   = 30 * time.Second
)

// checksPicker is the box of the checks of the branch's pull request: ↵
// sends the log of a failed job to the terminal, ⌘↵ runs it again.
type checksPicker struct {
	repoRoot string
	open     bool
	number   int
	query    string
	sel      int
	moved    bool
	// checks are those gh listed, read when, loading set while it runs,
	// and err why it failed.
	checks  []git.Check
	read    time.Time
	loading bool
	err     string
	wake    *time.Timer
}

// openChecks shows the box of the checks of the branch's pull request.
func (a *gitContext) openChecks() {
	if !a.live() {
		return
	}
	if !a.app.online() {
		a.app.fail("%s", locales.NotConnectedTo(a.app.conn.Host))
		return
	}
	pr := a.currentPR()
	if pr == nil {
		a.app.fail("%s", locales.PrNoneForBranch())
		return
	}
	a.app.checks = checksPicker{open: true, repoRoot: a.git.repo.Root, number: pr.Number}
	a.readChecks()
}

// readChecks asks gh for the checks of the box's pull request, and asks
// again a while later when one still runs.
func (a *gitContext) readChecks() {
	pick := &a.app.checks
	if pick.loading {
		return
	}
	pick.loading = true
	r, g, number := a.git.repo, a.git, pick.number
	bg.Go(func() {
		defer recoverCrash()
		checks, err := r.PRChecks(number)
		a.app.post(func() {
			if pick.repoRoot != r.Root || pick.number != number {
				return
			}
			pick.loading, pick.read = false, time.Now()
			switch {
			case err == nil:
				pick.checks, pick.err, g.ghOK = checks, "", true
			case errors.Is(err, git.ErrNoGH):
				g.ghOK = false
				pick.err = locales.PrNoGitHub()
			default:
				pick.err = err.Error()
			}
			if pick.wake != nil {
				pick.wake.Stop()
			}
			pick.wake = time.AfterFunc(checksEvery, a.app.invalidate)
		})
	})
}

// checksRunning is whether a check of the box still runs.
func (p *checksPicker) checksRunning() bool {
	for _, c := range p.checks {
		if c.Bucket == "pending" {
			return true
		}
	}
	return false
}

// checkChoices are the checks whose workflow, name or description has the
// words of the query.
func (a *app) checkChoices() []git.Check {
	words := strings.Fields(strings.ToLower(a.checks.query))
	var out []git.Check
	for _, ch := range a.checks.checks {
		text := strings.ToLower(ch.Title() + " " + ch.Description)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(text, w)
		}
		if ok {
			out = append(out, ch)
		}
	}
	return out
}

// checkFailed is whether the job of a check failed or was cancelled, for
// its log to be read and the job run again.
func checkFailed(ch git.Check) bool {
	return ch.IsJob() && (ch.Bucket == "fail" || ch.Bucket == "cancel")
}

// sendCheckLog reads the end of the log of a failed job and pastes it into
// the terminal used last, as a message to the agent in it, without
// pressing Enter; with no terminal, or with copyOnly, it copies it.
func (a *gitContext) sendCheckLog(ch git.Check, number int, copyOnly bool) {
	r := a.git.repo
	a.app.tell(note{message: locales.ChecksReadingLog(ch.Title())})
	bg.Go(func() {
		defer recoverCrash()
		log, err := r.JobLog(ch, checkLogLines)
		a.app.post(func() {
			if err != nil {
				a.app.fail("%s", locales.ChecksLogFailed(ch.Title(), err.Error()))
				return
			}
			text := formatCheckLog(ch, number, log)
			switch {
			case copyOnly:
				a.app.services.WriteClipboard(text)
				a.app.tell(note{message: locales.ChecksLogCopied(ch.Title())})
			case a.app.pasteToTerminal(text):
				a.app.tell(note{message: locales.ChecksLogSent(ch.Title())})
			default:
				a.app.services.WriteClipboard(text)
				a.app.tell(note{message: locales.ChecksNoTerminal(ch.Title())})
			}
		})
	})
}

// formatCheckLog is the log of a failed job as a message to an agent: what
// failed, then the lines of the log.
func formatCheckLog(ch git.Check, number int, log string) string {
	lines := strings.Split(log, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "The CI check %q failed on pull request #%d", ch.Title(), number)
	if ch.Link != "" {
		b.WriteString(": " + ch.Link)
	}
	fmt.Fprintf(&b, "\nThe last %d lines of its log:\n", len(lines))
	f := fence(lines)
	b.WriteString(f + "\n" + log + "\n" + f + "\n")
	return b.String()
}

// rerunCheck runs the job of a check again, then reads the checks and the
// pull request again to show it running.
func (a *gitContext) rerunCheck(ch git.Check) {
	r, g := a.git.repo, a.git
	bg.Go(func() {
		defer recoverCrash()
		err := r.RerunJob(ch)
		a.app.post(func() {
			if err != nil {
				a.app.fail("%s", locales.ChecksRerunFailed(ch.Title(), err.Error()))
				return
			}
			a.app.tell(note{message: locales.ChecksRerunning(ch.Title())})
			if g.repo == r {
				g.prRead = time.Time{}
			}
			if pick := &a.app.checks; pick.open && pick.repoRoot == r.Root {
				a.readChecks()
			}
		})
	})
}

// checkIcon is the icon and the color of how a check went.
func checkIcon(p *palette, ch git.Check) (*ui.SVG, ui.Color) {
	switch ch.Bucket {
	case "pass":
		return iconCheck, p.addedFg
	case "fail":
		return iconX, p.dangerFg
	case "pending":
		return iconClock, p.modifiedFg
	case "cancel":
		return iconStopSquare, p.muted
	}
	return iconMinus, p.faint
}

// prChecksView is the box of the checks of the pull request.
func (a *app) prChecksView(c *ui.Context) {
	pick := &a.checks
	if !pick.open {
		return
	}
	g := a.repositoryForRoot(pick.repoRoot)
	if g.git.repo == nil || g.git.repo.Root != pick.repoRoot {
		pick.open = false
		return
	}
	if !pick.loading && pick.checksRunning() && time.Since(pick.read) >= checksEvery {
		g.readChecks()
	}
	choices := a.checkChoices()
	pick.sel = min(max(pick.sel, 0), max(len(choices)-1, 0))
	p := a.pal
	number := pick.number
	run := func(do func()) {
		pick.open = false
		a.post(do)
	}
	send := func(ch git.Check) { run(func() { g.sendCheckLog(ch, number, false) }) }
	copyLog := func(ch git.Check) { run(func() { g.sendCheckLog(ch, number, true) }) }
	rerun := func(ch git.Check) { run(func() { g.rerunCheck(ch) }) }
	openLink := func(ch git.Check) { run(func() { a.services.OpenURL(ch.Link) }) }
	title := locales.ChecksTitle("#" + strconv.Itoa(number))
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "checks")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(600).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().Role(ui.RoleDialog).Label(title)
			box.Children(func() {
				ui.Row(c).Padding(10, 14).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					in := a.textKeys(ui.TextInputBase(c.Key("checks-input"), &pick.query)).Grow(1).FontSize(14).AutoFocus().Placeholder(locales.ChecksFind("#" + strconv.Itoa(number))).Label(title)
					if in.Changed() {
						pick.sel = 0
					}
					if in.Shortcut(0, ui.KeyDown) {
						pick.sel = min(pick.sel+1, len(choices)-1)
						pick.moved = true
					}
					if in.Shortcut(0, ui.KeyUp) {
						pick.sel = max(pick.sel-1, 0)
						pick.moved = true
					}
					if pick.sel < len(choices) {
						ch := choices[pick.sel]
						switch {
						case in.Shortcut(ui.Cmd, ui.KeyEnter):
							if checkFailed(ch) {
								rerun(ch)
							}
						case in.Submitted():
							if checkFailed(ch) {
								send(ch)
							} else if ch.Link != "" {
								openLink(ch)
							}
						}
					}
				})
				ui.Scroll(c.Key("checks-results")).MaxHeight(380).Padding(6).Children(func() {
					switch {
					case pick.loading && len(pick.checks) == 0:
						ui.Text(c, locales.ChecksLoading()).FontSize(13).TextColor(p.muted).Padding(7, 10)
					case pick.err != "" && len(pick.checks) == 0:
						ui.Text(c, pick.err).FontSize(13).TextColor(p.muted).Padding(7, 10)
					case len(pick.checks) == 0:
						ui.Text(c, locales.ChecksNone()).FontSize(13).TextColor(p.muted).Padding(7, 10)
					case len(choices) == 0:
						ui.Text(c, locales.ChecksNoMatch()).FontSize(13).TextColor(p.muted).Padding(7, 10)
					}
					for i, ch := range choices {
						row := ui.ButtonBase(c.Key("check:"+strconv.Itoa(i)+":"+ch.Link+":"+ch.Name)).FillWidth().Padding(5, 10).Gap(10).Radius(6).AlignItems(ui.Center).Label(ch.Title())
						if i == pick.sel && pick.moved {
							row.ScrollIntoView()
						}
						if i != pick.sel && row.Hovered() {
							row.Background(p.hover)
						}
						row.Transition(hoverFade)
						row.Children(func() {
							a.pickMark(c, i == pick.sel, "checks")
							icon, col := checkIcon(p, ch)
							ui.Icon(c, icon).FontSize(14).TextColor(col)
							ui.Text(c, ch.Title()).FontSize(13).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
							ui.Text(c, ch.Description).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
							if i != pick.sel && !row.Hovered() {
								return
							}
							if checkFailed(ch) {
								if a.smallButton(c, iconSend, locales.ChecksSend()) {
									send(ch)
								}
								if a.smallButton(c, iconCopy, locales.ChecksCopyLog()) {
									copyLog(ch)
								}
								if a.smallButton(c, iconRefresh, locales.ChecksRerun()) {
									rerun(ch)
								}
							}
							if ch.Link != "" && a.smallButton(c, iconExternal, locales.PrOpenOnGitHub()) {
								openLink(ch)
							}
						})
						row.ContextMenu(func(m *ui.Menu) {
							failed := checkFailed(ch)
							if m.Item(locales.ChecksSend()).Disabled(!failed).Chosen() {
								send(ch)
							}
							if m.Item(locales.ChecksCopyLog()).Disabled(!failed).Chosen() {
								copyLog(ch)
							}
							if m.Item(locales.ChecksRerun()).Disabled(!failed).Chosen() {
								rerun(ch)
							}
							m.Separator()
							if m.Item(locales.PrOpenOnGitHub()).Disabled(ch.Link == "").Chosen() {
								openLink(ch)
							}
						})
						if row.Clicked() {
							pick.sel = i
						}
					}
				})
				a.checksFooter(c)
			})
			pick.moved = false
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				pick.open = false
			}
		})
	})
}

// checksFooter shows the keys of the box of the checks.
func (a *app) checksFooter(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 14).Gap(14).AlignItems(ui.Center).Background(p.hover).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		hint := func(keys, label string) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				a.keycaps(c, keys)
				ui.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
			})
		}
		hint("⏎", locales.ChecksSend())
		hint(keyText("CmdOrCtrl+Enter"), locales.ChecksRerun())
		ui.Spacer(c)
		hint("esc", locales.QuickClose())
	})
}
