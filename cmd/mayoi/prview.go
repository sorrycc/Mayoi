package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// prPage is a part of a pull request's tab: its conversation, its
// changes, its commits or its checks.
type prPage int

const (
	prConversation prPage = iota
	prChanges
	prCommits
	prChecks
	prPages
)

// key is how the settings keep a page.
func (p prPage) key() string {
	switch p {
	case prChanges:
		return "changes"
	case prCommits:
		return "commits"
	case prChecks:
		return "checks"
	}
	return "conversation"
}

// parsePRPage reads a page the settings kept, the conversation for none.
func parsePRPage(s string) prPage {
	for p := range prPages {
		if p.key() == s {
			return p
		}
	}
	return prConversation
}

// prPageMod is the keys held with 1 to 4 that show a page of a pull
// request's tab: ⌥⌘ on macOS, Ctrl+Shift elsewhere, as ⌘ alone shows the
// column's tabs.
func prPageMod() ui.Modifiers {
	if macKeys() {
		return ui.Super | ui.Alt
	}
	return ui.Ctrl | ui.Shift
}

// prReadEvery is how often a pull request's tab reads it again while it
// shows: sooner while one of its checks runs.
const prReadEvery = 60 * time.Second

// readPRView reads, in the background, what a pull request's tab shows
// besides its changes: the pull request, its checks and its review
// threads, each shown as it comes. quiet keeps a failure from replacing
// what was read.
func (a *app) readPRView(v *diffView) {
	pr := v.commit.pr
	r := a.diffContext(v).git.repo
	if pr.viewing || r == nil || !a.online() {
		return
	}
	pr.viewing, pr.viewAsked = true, time.Now()
	n := pr.number
	type result struct {
		view    *git.PRView
		checks  []git.Check
		threads []git.GHThread
		err     error
		cerr    error
		terr    error
	}
	var res result
	bg.Go(func() {
		defer recoverCrash()
		done := make(chan struct{}, 2)
		bg.Go(func() {
			defer func() { done <- struct{}{} }()
			res.checks, res.cerr = fetchPRChecks(r, n)
		})
		bg.Go(func() {
			defer func() { done <- struct{}{} }()
			res.threads, res.terr = fetchPRThreads(r, n)
		})
		res.view, res.err = fetchPRView(r, n)
		<-done
		<-done
		a.post(func() {
			pr.viewing = false
			pr.viewAt = time.Now()
			switch {
			case res.err == nil:
				pr.view, pr.viewErr = res.view, ""
				if pr.title == "" {
					pr.title = res.view.Title
				}
			case errors.Is(res.err, remote.ErrOffline):
			case errors.Is(res.err, git.ErrNoPR):
				pr.viewErr = locales.PrNotFound("#" + strconv.Itoa(n))
			default:
				pr.viewErr = ghMessage(res.err)
			}
			if res.cerr == nil {
				pr.checks, pr.checksOK = res.checks, true
			}
			if res.terr == nil {
				pr.ghThreads, pr.threadsOK = res.threads, true
				pr.threadGen++
				v.changed()
			}
			if pr.info != nil && pr.view != nil && pr.view.HeadOid != "" && pr.view.HeadOid != v.commit.hash && !v.commit.pending() {
				pr.newer = true
			}
			a.invalidate()
		})
	})
}

// fetchPRView, fetchPRChecks and fetchPRThreads read pull request n of a
// work tree, its checks and its review threads, which the tests replace.
var (
	fetchPRView    = func(r *git.Repo, n int) (*git.PRView, error) { return r.PullRequestView(n) }
	fetchPRChecks  = func(r *git.Repo, n int) ([]git.Check, error) { return r.PRChecks(n) }
	fetchPRThreads = func(r *git.Repo, n int) ([]git.GHThread, error) { return r.ReviewThreads(n) }
)

// pollPRView reads the pull request of a tab that shows again: once at
// first, then every prReadEvery, or checksEvery while a check runs.
func (a *app) pollPRView(v *diffView, now time.Time) {
	pr := v.commit.pr
	if pr.viewing {
		return
	}
	every := prReadEvery
	if prChecksRunning(pr.checks) {
		every = checksEvery
	}
	if pr.viewAsked.IsZero() || now.Sub(pr.viewAt) >= every && now.Sub(pr.viewAsked) >= every {
		a.readPRView(v)
	}
}

// prChecksRunning is whether one of checks still runs.
func prChecksRunning(checks []git.Check) bool {
	for _, c := range checks {
		if c.Bucket == "pending" {
			return true
		}
	}
	return false
}

// prViewUI is the tab of a pull request: its header and the tabs of its
// pages over the page shown.
func (a *app) prViewUI(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	now := c.Now()
	if v.commit.pending() && !pr.opening && pr.openErr == "" && !pr.fetched {
		// A tab that did not open it, as one of an issue that turned out
		// to be a pull request, reads its changes once it shows.
		pr.fetched = true
		a.post(func() { a.diffContext(v).fetchPR(pr, false) })
	}
	a.pollPRView(v, now)
	a.parsePRView(v)
	if a.showsTabOf(v) {
		for k := ui.Key1; k <= ui.Key4; k++ {
			if c.Shortcut(prPageMod(), k) {
				page := prPage(k - ui.Key1)
				a.post(func() { a.setPRPage(v, page) })
			}
		}
	}
	ui.Column(c.Key("pr-view")).Fill().Background(p.editor).Label(v.name()).Children(func() {
		a.prActivity(c, v)
		a.prHeader(c, v, now)
		a.prTabs(c, v)
		page := ui.Column(c.Key("pr-page:" + pr.page.key())).Grow(1).Shrink(1).MinHeight(0).FillWidth()
		fadePage(page)
		page.Children(func() {
			switch pr.page {
			case prChanges:
				a.prChangesUI(c, v)
			case prCommits:
				a.prScrollPage(c, "pr-commits", func() { a.prCommitsUI(c, v, now) })
			case prChecks:
				a.prScrollPage(c, "pr-checks", func() { a.prChecksPageUI(c, v) })
			default:
				a.prScrollPage(c, "pr-conversation", func() { a.prConversationUI(c, v, now) })
			}
		})
	})
}

// isPR reports whether a tab is a pull request's.
func (t *colTab) isPR() bool { return t.diff != nil && t.diff.commit != nil && t.diff.commit.pr != nil }

// showsTabOf reports whether diff v is the tab shown of the column with
// the keyboard.
func (a *app) showsTabOf(v *diffView) bool {
	t := a.column().tab()
	return t != nil && t.diff == v
}

// setPRPage shows a page of a pull request's tab, reading its changes
// once they show.
func (a *app) setPRPage(v *diffView, page prPage) {
	pr := v.commit.pr
	if pr.page == page {
		return
	}
	pr.page = page
	if page == prChanges {
		v.pane.Focus()
	}
	a.rememberTabs()
	a.invalidate()
}

// prScrollPage is a page of a pull request's tab that scrolls, its text
// no wider than an issue's.
func (a *app) prScrollPage(c *ui.Context, key string, fn func()) {
	ui.Scroll(c.Key(key)).Fill().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(18, 24, 48, 24).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(issueWidth).Gap(14).Children(fn)
		})
	})
}

// prState is how a pull request's state shows: its word and color.
func (a *app) prState(state string, draft bool) (string, ui.Color) {
	p := a.pal
	switch {
	case state == "MERGED":
		return locales.PrMerged(), p.accent
	case state == "CLOSED":
		return locales.PrClosed(), p.dangerFg
	case draft:
		return locales.PrDraft(), p.muted
	}
	return locales.PrOpen(), p.addedFg
}

// prHeader is what a pull request is, over its pages: its state, number
// and branches with what can be done to it, its title, and who opened it
// and how much it changes.
func (a *app) prHeader(c *ui.Context, v *diffView, now time.Time) {
	p := a.pal
	pr := v.commit.pr
	pv, in := pr.view, pr.info
	title := pr.title
	switch {
	case pv != nil:
		title = pv.Title
	case in != nil:
		title = in.Title
	}
	ui.Column(c.Key("pr-header")).FillWidth().Padding(14, 20, 10, 20).Gap(8).Children(func() {
		ui.Row(c).Height(26).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case pv != nil:
				state, col := a.prState(pv.State, pv.Draft)
				ui.Text(c.Key("pr-state"), state).FontSize(12).FontWeight(600).TextColor(col).Padding(2, 9).Radius(10).Background(col.Alpha(0.14)).
					SingleLine().Shrink(0).Transition(hoverFade)
			case in != nil:
				state, col := a.prState(in.State, in.Draft)
				ui.Text(c.Key("pr-state"), state).FontSize(12).FontWeight(600).TextColor(col).Padding(2, 9).Radius(10).Background(col.Alpha(0.14)).
					SingleLine().Shrink(0).Transition(hoverFade)
			case pr.viewErr == "":
				ui.Box(c.Key("pr-state")).Size(52, 20).Radius(10).Background(p.text.Alpha(0.07)).Shrink(0)
			}
			ui.Text(c, "#"+strconv.Itoa(pr.number)).FontSize(13).TextColor(p.muted).FontFeatures("tnum").SingleLine().Shrink(0)
			base, head := "", ""
			switch {
			case pv != nil:
				base, head = pv.BaseRef, pv.HeadRef
			case in != nil:
				base, head = in.BaseRef, in.HeadRef
			}
			if head != "" {
				a.branchPill(c, base+" ← "+head)
			}
			ui.Spacer(c)
			if pr.viewing && pr.view != nil || pr.acting != "" {
				spinRing(c, "pr-refreshing", 12, p.muted).Tooltip(locales.GhReading())
			}
			a.prActions(c, v)
		})
		if title == "" {
			ui.Box(c.Key("pr-title")).WidthPercent(55).Height(18).Radius(5).Background(p.text.Alpha(0.07)).Margin(2, 0)
		} else {
			ui.Text(c.Key("pr-title"), title).FontSize(18).FontWeight(600).TextColor(p.text).LineHeight(1.3).Selectable()
		}
		if pv == nil {
			return
		}
		ui.Row(c.Key("pr-meta")).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
			if pv.Author != "" {
				a.ghAvatar(c, pv.Author, pv.Avatars[pv.Author], issueMetaAvatar, now)
				ui.Text(c, locales.GhOpenedBy(pv.Author, ago(pv.Created, now))).FontSize(12).TextColor(p.muted).SingleLine().
					Tooltip(dateTime(pv.Created.Local()))
			}
			dot := func() { ui.Text(c, "·").FontSize(12).TextColor(p.faint) }
			dot()
			ui.Text(c, "+"+strconv.Itoa(pv.Additions)).FontSize(12).TextColor(p.addedFg).FontFeatures("tnum").SingleLine()
			ui.Text(c, "−"+strconv.Itoa(pv.Deletions)).FontSize(12).TextColor(p.dangerFg).FontFeatures("tnum").SingleLine()
			ui.Text(c, locales.CardFilesChanged(pv.Files)).FontSize(12).TextColor(p.muted).SingleLine()
			if !pv.Updated.IsZero() {
				dot()
				ui.Text(c, locales.PrUpdatedAgo(ago(pv.Updated, now))).FontSize(12).TextColor(p.muted).SingleLine().
					Tooltip(dateTime(pv.Updated.Local()))
			}
		})
	})
}

// branchPill shows a pull request's branches, base ← head, in the font
// of code; a click copies its head's.
func (a *app) branchPill(c *ui.Context, text string) {
	p := a.pal
	head := text
	if _, h, ok := strings.Cut(text, " ← "); ok {
		head = h
	}
	b := ui.ButtonBase(c.Key("pr-branches")).Height(20).Padding(0, 7).Gap(4).Radius(10).AlignItems(ui.Center).
		Background(p.text.Alpha(0.06)).Shrink(1).MinWidth(0).Label(text).Tooltip(locales.PrCopyBranchTip(head))
	if b.Hovered() {
		b.Background(p.text.Alpha(0.1))
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconBranch).FontSize(11).TextColor(p.muted).Shrink(0)
		ui.Text(c, text).Font(a.font.family).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
	})
	if b.Clicked() {
		a.services.WriteClipboard(head)
		a.tell(note{message: locales.PrBranchCopied(head)})
	}
}

// prActions are the buttons of a pull request's header: check it out,
// review it, merge it, and the rest in a menu.
func (a *app) prActions(c *ui.Context, v *diffView) {
	pr := v.commit.pr
	pv := pr.view
	ctx := a.diffContext(v)
	busy := pr.acting != ""
	open := pv != nil && pv.State == "OPEN"
	if ctx.branch() != "" && pv != nil && pv.HeadRef == ctx.branch() {
		ui.Text(c, locales.PrCheckedOut()).FontSize(12).TextColor(a.pal.muted).SingleLine().Shrink(0).Tooltip(locales.PrCheckedOutTip(pv.HeadRef))
	} else if ui.Button(c.Key("pr-checkout"), locales.GhCheckOut()).Disabled(pv == nil || !ctx.git.canMutate()).Shrink(0).Clicked() {
		n := pr.number
		a.post(func() { ctx.checkoutPR(n) })
	}
	if open && !pv.ViewerIsIt {
		rb := menuButton(c, ui.Button(c.Key("pr-review"), ""), locales.PrReview()).Disabled(busy)
		rb.Menu(func(m *ui.Menu) {
			if m.Item(locales.PrApprove()).Chosen() {
				a.post(func() { a.reviewPR(v, "approve", "") })
			}
			if m.Item(locales.PrRequestChangesMenu()).Chosen() {
				a.post(func() { a.startReview(v, "request-changes") })
			}
			if m.Item(locales.PrCommentReview()).Chosen() {
				a.post(func() { a.startReview(v, "comment") })
			}
		})
	}
	if open && !pv.Draft && len(pv.Methods) > 0 {
		mb := menuButton(c, ui.PrimaryButton(c.Key("pr-merge"), ""), locales.PrMerge()).Disabled(busy || pv.Mergeable == "CONFLICTING")
		if pv.Mergeable == "CONFLICTING" {
			mb.Tooltip(locales.PrConflicts(pv.BaseRef))
		}
		mb.Menu(func(m *ui.Menu) {
			for _, method := range pv.Methods {
				if m.Item(prMethodLabel(method)).Chosen() {
					a.post(func() { a.mergePR(v, method) })
				}
			}
			m.Separator()
			if m.Item(locales.PrDeleteBranch()).Checked(a.settings.PRDeleteBranch).Disabled(pv.CrossRepo).Chosen() {
				a.post(func() {
					a.settings.PRDeleteBranch = !a.settings.PRDeleteBranch
					a.settings.Changed()
				})
			}
		})
	}
	more := a.iconButton(c.Key("pr-more"), iconMore, locales.PrMore(), false).Disabled(pv == nil || busy)
	more.Menu(func(m *ui.Menu) {
		if pv == nil {
			return
		}
		if pv.State == "OPEN" && pv.CanUpdate {
			if pv.Draft {
				if m.Item(locales.PrMarkReady()).Chosen() {
					a.post(func() { a.readyPR(v, false) })
				}
			} else if m.Item(locales.PrMarkDraft()).Chosen() {
				a.post(func() { a.readyPR(v, true) })
			}
			if m.Item(locales.PrClosePR()).Chosen() {
				a.post(func() { a.closePR(v, false) })
			}
			m.Separator()
		} else if pv.State == "CLOSED" && pv.CanUpdate {
			if m.Item(locales.PrReopenPR()).Chosen() {
				a.post(func() { a.closePR(v, true) })
			}
			m.Separator()
		}
		n := pr.number
		if m.Item(locales.GhAddressReview()).Chosen() {
			a.post(func() { a.openPRWithThreads(ctx, n) })
		}
		if m.Item(locales.PrReload()).Chosen() {
			a.post(func() { a.reloadPR(v) })
		}
		m.Separator()
		if m.Item(locales.GhCopyLink()).Disabled(pv.URL == "").Chosen() {
			a.services.WriteClipboard(pv.URL)
			a.tell(note{message: locales.GhLinkCopied("#" + strconv.Itoa(n))})
		}
		if m.Item(locales.PrCopyBranch()).Disabled(pv.HeadRef == "").Chosen() {
			a.services.WriteClipboard(pv.HeadRef)
			a.tell(note{message: locales.PrBranchCopied(pv.HeadRef)})
		}
		if m.Item(locales.PrOpenOnGitHub()).Disabled(pv.URL == "").Chosen() {
			a.services.OpenURL(pv.URL)
		}
	})
}

// menuButton has button b say label with a chevron after it, as one that
// opens a menu.
func menuButton(c *ui.Context, b ui.Element, label string) ui.Element {
	return b.Label(label).Gap(5).Shrink(0).Children(func() {
		ui.Text(c, label).SingleLine()
		ui.Icon(c, iconChevronDown).FontSize(11)
	})
}

// prMethodLabel is how a way to merge reads in the menu of Merge.
func prMethodLabel(method string) string {
	switch method {
	case "SQUASH":
		return locales.PrSquashMerge()
	case "REBASE":
		return locales.PrRebaseMerge()
	}
	return locales.PrMergeCommit()
}

// prTabs choose the page of a pull request's tab, each with how many it
// holds once known.
func (a *app) prTabs(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	pv := pr.view
	names := [prPages]string{locales.PrConversation(), locales.PrChanges(), locales.PrCommits(), locales.PrChecks()}
	var counts [prPages]string
	if pv != nil {
		counts[prConversation] = strconv.Itoa(len(pv.Events))
		counts[prCommits] = strconv.Itoa(pv.AllCommits)
		counts[prChanges] = strconv.Itoa(pv.Files)
	}
	if !v.commit.pending() && len(v.files) > 0 {
		counts[prChanges] = strconv.Itoa(len(v.files))
	}
	if pr.checksOK {
		counts[prChecks] = strconv.Itoa(len(pr.checks))
	}
	sel := int(pr.page)
	ui.Row(c.Key("pr-tabs")).FillWidth().Height(34).Padding(0, 14).AlignItems(ui.End).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Shrink(0).
		Children(func() {
			tabs := ui.TabsBase(c.Key("pr-tab-list"), &sel, int(prPages))
			tabs.List.Gap(2).FillHeight().AlignItems(ui.End).Label(locales.PullRequest())
			tabs.List.Children(func() {
				for i, name := range names {
					on := i == int(pr.page)
					tab := tabs.Tab(i).Height(34).Padding(0, 10).Gap(6).AlignItems(ui.Center).Label(name).
						Tooltip(keyText(prPageAccel(i)))
					fg := p.muted
					if on {
						fg = p.text
					} else if tab.Hovered() {
						fg = p.text
					}
					tab.Transition(hoverFade)
					tab.Children(func() {
						if i == int(prChecks) {
							if icon, col := prChecksSummary(p, pr.checks); icon != nil {
								ui.Icon(c, icon).FontSize(12).TextColor(col)
							}
						}
						ui.Text(c, name).FontSize(12.5).FontWeight(500).TextColor(fg).SingleLine()
						if counts[i] != "" {
							ui.Text(c, counts[i]).FontSize(11).TextColor(p.muted).FontFeatures("tnum").Padding(0, 6).Radius(8).
								Background(p.text.Alpha(0.07)).SingleLine()
						}
						// A bar under the chosen one, of the accent.
						bar := ui.Box(c.Key("bar")).Absolute().Left(6).Right(6).Bottom(0).Height(2).Radius(1)
						if on {
							bar.Background(p.accent)
						} else {
							bar.Background(p.accent.Alpha(0))
						}
						bar.Transition(hoverFade)
					})
				}
			})
			if tabs.List.Changed() {
				page := prPage(sel)
				a.post(func() { a.setPRPage(v, page) })
			}
		})
}

// prPageAccel is the accelerator that shows page i of a pull request's
// tab.
func prPageAccel(i int) string {
	n := strconv.Itoa(i + 1)
	if macKeys() {
		return "Alt+CmdOrCtrl+" + n
	}
	return "Ctrl+Shift+" + n
}

// prChecksSummary is the icon and color of how the checks went: one
// failed is a failure, else one running is pending.
func prChecksSummary(p *palette, checks []git.Check) (*ui.SVG, ui.Color) {
	pending := false
	for _, ch := range checks {
		switch ch.Bucket {
		case "fail":
			return iconX, p.dangerFg
		case "pending":
			pending = true
		}
	}
	switch {
	case pending:
		return iconClock, p.modifiedFg
	case len(checks) > 0:
		return iconCheck, p.addedFg
	}
	return nil, p.muted
}

// parsePRView parses the description and the conversation of a pull
// request's tab, as they were read or as their pictures arrive.
func (a *app) parsePRView(v *diffView) {
	pr := v.commit.pr
	pv := pr.view
	if pv == nil || pr.parsedOf == pv && !pr.stale {
		return
	}
	pr.parsedOf, pr.stale = pv, false
	pr.cache.arrived = func() {
		a.post(func() {
			pr.stale = true
			a.invalidate()
		})
	}
	pr.cache.allow = func(u string) bool { return ghPictureHost(u, pv.URL) }
	root := v.repoRoot
	if r := a.diffContext(v).git.repo; root == "" && r != nil {
		root = r.Root
	}
	path := filepath.Join(root, "PULL_REQUEST.md")
	pr.texts = pr.texts[:0]
	pr.texts = append(pr.texts, parseMarkdown([]byte(ghPictures(pv.Body)), path, root, nil, &pr.cache))
	for _, e := range pv.Events {
		pr.texts = append(pr.texts, parseMarkdown([]byte(ghPictures(e.Body)), path, root, nil, &pr.cache))
	}
}

// prConversationUI is the page of what a pull request is and what was
// said of it: whether it can merge, its reviewers and the rest of its
// fields, its description, its comments and reviews, and the box to
// comment.
func (a *app) prConversationUI(c *ui.Context, v *diffView, now time.Time) {
	pr := v.commit.pr
	pv := pr.view
	switch {
	case pv == nil && pr.viewErr != "":
		a.githubCard(c, "pr-failed", iconTriangleAlert, locales.GhIssueFailed(v.name()), pr.viewErr, time.Time{}, func() {
			if ui.PrimaryButton(c, locales.GhRetry()).Clicked() {
				a.post(func() { a.readPRView(v) })
			}
		})
		return
	case pv == nil:
		a.issueSkeleton(c)
		return
	}
	if pv.State == "OPEN" {
		a.prMergeBox(c, v)
	}
	a.prFields(c, v, now)
	if len(pr.texts) > 0 {
		a.issueCard(c, "pr-body", pv.Author, pv.Avatars[pv.Author], pv.Created, "", pr.texts[0], now)
	}
	for i, e := range pv.Events {
		if i+1 >= len(pr.texts) {
			break
		}
		key := "pr-event:" + strconv.Itoa(i)
		if e.Review != "" {
			a.prReviewEvent(c, key, pv, e, pr.texts[i+1], now)
			continue
		}
		a.issueCard(c, key, e.Author, pv.Avatars[e.Author], e.Created, e.URL, pr.texts[i+1], now)
	}
	if more := pv.AllComments - countComments(pv.Events); more > 0 {
		b := ui.ButtonBase(c.Key("pr-more-comments")).Padding(4, 2).Label(locales.GhMoreComments(more))
		b.Children(func() { ui.Text(c, locales.GhMoreComments(more)).FontSize(12).TextColor(a.pal.accent) })
		if b.Clicked() {
			a.services.OpenURL(pv.URL)
		}
	}
	a.prComposer(c, v)
}

// countComments is how many of events are comments, not reviews.
func countComments(events []git.PREvent) int {
	n := 0
	for _, e := range events {
		if e.Review == "" {
			n++
		}
	}
	return n
}

// prReviewEvent is a review in a pull request's conversation: who
// reviewed it and how, and what they said, if anything.
func (a *app) prReviewEvent(c *ui.Context, key string, pv *git.PRView, e git.PREvent, blocks []markdownBlock, now time.Time) {
	p := a.pal
	icon, col, word := prReviewLook(p, e.Review)
	ui.Row(c.Key(key+":line")).Gap(8).AlignItems(ui.Center).Padding(0, 4).Children(func() {
		ui.Row(c).Size(24, 24).Radius(12).Justify(ui.Center).AlignItems(ui.Center).Background(col.Alpha(0.14)).Shrink(0).
			Children(func() { ui.Icon(c, icon).FontSize(12).TextColor(col) })
		name := e.Author
		if name == "" {
			name = locales.GhGhost()
		}
		a.ghAvatar(c, name, pv.Avatars[name], issueMetaAvatar, now)
		ui.Text(c, locales.PrReviewedBy(name, word)).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
		ui.Text(c, ago(e.Created, now)).FontSize(12).TextColor(p.muted).SingleLine().Shrink(0).Tooltip(dateTime(e.Created.Local()))
	})
	if strings.TrimSpace(e.Body) != "" {
		a.issueCard(c, key, e.Author, pv.Avatars[e.Author], e.Created, e.URL, blocks, now)
	}
}

// prReviewLook is the icon, color and word of a review's state.
func prReviewLook(p *palette, state string) (*ui.SVG, ui.Color, string) {
	switch state {
	case "APPROVED":
		return iconCheck, p.addedFg, locales.PrReviewApproved()
	case "CHANGES_REQUESTED":
		return iconTriangleAlert, p.dangerFg, locales.PrReviewChanges()
	case "DISMISSED":
		return iconMinus, p.muted, locales.PrReviewDismissed()
	case "REQUESTED":
		return iconClock, p.modifiedFg, locales.PrReviewRequested()
	}
	return iconComment, p.muted, locales.PrReviewCommented()
}

// prMergeBox says, atop an open pull request's conversation, whether it
// can merge and, when not, what it waits for, tinted as that is: green
// once it can, red while it conflicts or a check failed, amber while it
// waits. A failed check's jobs run again from here.
func (a *app) prMergeBox(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	pv := pr.view
	var failed, running []string
	for _, ch := range pr.checks {
		switch ch.Bucket {
		case "fail":
			failed = append(failed, ch.Title())
		case "pending":
			running = append(running, ch.Title())
		}
	}
	key, col, icon := "waiting", p.modifiedFg, iconClock
	title, hint := locales.PrNotReady(), ""
	switch {
	case pv.Draft:
		key, col, icon, title, hint = "draft", p.muted, iconPullRequestDraft, locales.PrIsDraft(), locales.PrIsDraftHint()
	case pv.Mergeable == "CONFLICTING":
		key, col, icon, title, hint = "conflicts", p.dangerFg, iconTriangleAlert, locales.PrConflicts(pv.BaseRef), locales.PrConflictsHint(pv.HeadRef)
	case len(failed) > 0:
		key, col, icon, title, hint = "failed", p.dangerFg, iconX, locales.PrChecksFailed(len(failed)), strings.Join(failed, ", ")
	case pv.Review == "CHANGES_REQUESTED":
		key, col, icon, title, hint = "changes", p.dangerFg, iconTriangleAlert, locales.PrChangesRequested(), ""
	case len(running) > 0:
		key, col, icon, title, hint = "running", p.modifiedFg, iconClock, locales.PrChecksPending(len(running)), strings.Join(running, ", ")
	case pv.Review == "REVIEW_REQUIRED":
		key, col, icon, title, hint = "review", p.modifiedFg, iconClock, locales.PrReviewRequired(), ""
	case pv.MergeState == "BEHIND":
		key, col, icon, title, hint = "behind", p.modifiedFg, iconArrowDown, locales.PrBehind(pv.BaseRef), ""
	case pv.MergeState == "BLOCKED":
		key, col, icon, title, hint = "blocked", p.modifiedFg, iconLock, locales.PrBlocked(), ""
	case pv.Mergeable == "MERGEABLE":
		key, col, icon, title, hint = "ready", p.addedFg, iconCheck, locales.PrReady(), locales.PrReadyHint(pv.BaseRef)
	case pv.Mergeable == "UNKNOWN":
		key, col, icon, title, hint = "checking", p.muted, iconClock, locales.PrCheckingMerge(), ""
	}
	ui.Row(c.Key("pr-merge-box")).Gap(10).AlignItems(ui.Center).Padding(10, 12).Radius(8).
		Background(col.Alpha(0.08)).Border(1, col.Alpha(0.3)).Transition(hoverFade).Children(func() {
		ui.Row(c.Key(key)).Size(28, 28).Radius(14).Justify(ui.Center).AlignItems(ui.Center).Background(col.Alpha(0.16)).Shrink(0).
			Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(col) })
		ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
			if hint != "" {
				ui.Text(c, hint).FontSize(12).TextColor(p.muted).MaxLines(2).Tooltip(hint)
			}
		})
		if key == "failed" {
			if ui.Button(c.Key("pr-rerun"), locales.PrRerunFailed()).Disabled(pr.acting != "").Shrink(0).Clicked() {
				a.post(func() { a.rerunFailed(v) })
			}
			if ui.Button(c.Key("pr-see-checks"), locales.PrSeeChecks()).Shrink(0).Clicked() {
				a.post(func() { a.setPRPage(v, prChecks) })
			}
		}
	})
}

// prFields are the people and the rest of a pull request: its reviewers
// by their reviews, who it is assigned to, its labels and milestone, and
// the issues it closes.
func (a *app) prFields(c *ui.Context, v *diffView, now time.Time) {
	p := a.pal
	pv := v.commit.pr.view
	row := func(key, label string, fn func()) {
		ui.Row(c.Key(key)).Gap(12).AlignItems(ui.Center).MinHeight(26).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(p.muted).Width(84).Shrink(0).SingleLine()
			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Grow(1).Shrink(1).MinWidth(0).Children(fn)
		})
	}
	none := func() { ui.Text(c, "—").FontSize(12.5).TextColor(p.faint) }
	ui.Column(c.Key("pr-fields")).Radius(8).Border(1, p.border).Padding(8, 14).Gap(2).Children(func() {
		row("reviewers", locales.PrReviewers(), func() {
			if len(pv.Reviewers) == 0 {
				none()
			}
			for _, r := range pv.Reviewers {
				icon, col, word := prReviewLook(p, r.State)
				ui.Row(c.Key(r.Login)).Height(22).Gap(5).Padding(0, 8, 0, 3).Radius(11).AlignItems(ui.Center).Background(p.text.Alpha(0.05)).
					Tooltip(locales.PrReviewerTip(r.Login, word)).Children(func() {
					if r.Team {
						ui.Icon(c, iconHash).FontSize(12).TextColor(p.muted)
					} else {
						a.ghAvatar(c, r.Login, pv.Avatars[r.Login], 16, now)
					}
					ui.Text(c, r.Login).FontSize(12).TextColor(p.text).SingleLine()
					ui.Icon(c, icon).FontSize(11).TextColor(col)
				})
			}
		})
		row("assignees", locales.PrAssignees(), func() {
			if len(pv.Assignees) == 0 {
				none()
			}
			for _, as := range pv.Assignees {
				ui.Row(c.Key(as)).Gap(5).AlignItems(ui.Center).Children(func() {
					a.ghAvatar(c, as, pv.Avatars[as], 16, now)
					ui.Text(c, as).FontSize(12).TextColor(p.text).SingleLine()
				})
			}
		})
		if len(pv.Labels) > 0 {
			row("labels", locales.PrLabels(), func() {
				for _, l := range pv.Labels {
					a.ghLabel(c, l, 10.5)
				}
			})
		}
		if pv.Milestone != "" {
			row("milestone", locales.PrMilestone(), func() { ui.Text(c, pv.Milestone).FontSize(12.5).TextColor(p.text).SingleLine() })
		}
		if len(pv.Linked) > 0 {
			ctx := a.diffContext(v)
			row("linked", locales.PrCloses(), func() {
				for _, l := range pv.Linked {
					n := l.Number
					b := ui.ButtonBase(c.Key("linked:"+strconv.Itoa(n))).Height(22).Padding(0, 6).Gap(5).Radius(5).AlignItems(ui.Center).
						Label(l.Title).Tooltip(l.Title)
					if b.Hovered() {
						b.Background(p.hover)
					}
					b.Transition(hoverFade)
					b.Children(func() {
						ui.Icon(c, iconIssue).FontSize(12).TextColor(p.addedFg)
						ui.Text(c, "#"+strconv.Itoa(n)+" "+l.Title).FontSize(12).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
					})
					if b.Clicked() {
						a.post(func() { a.openIssue(ctx, n, true) })
					}
				}
			})
		}
	})
}

// prComposer is the box to comment on a pull request, or, while a review
// is being written, to say what the review says: ⌘↵ sends it.
func (a *app) prComposer(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	ui.Column(c.Key("pr-composer")).Gap(8).Margin(6, 0, 0, 0).Children(func() {
		placeholder := locales.GhCommentPlaceholder()
		switch pr.reviewing {
		case "request-changes":
			placeholder = locales.PrRequestChangesPlaceholder()
		case "comment":
			placeholder = locales.PrReviewPlaceholder()
		}
		in := a.textKeys(ui.TextArea(c.Key("pr-composer-text"), &pr.comment)).FontSize(13).Lines(3, 14).Padding(8, 10).
			Placeholder(placeholder).Label(locales.GhCommentLabel())
		if pr.focusComposer {
			in.AutoFocus()
			pr.focusComposer = false
		}
		if pr.sending {
			in.ReadOnly(true)
		}
		empty := strings.TrimSpace(pr.comment) == ""
		send := in.Shortcut(ui.Cmd, ui.KeyEnter)
		if in.Shortcut(0, ui.KeyEscape) && pr.reviewing != "" {
			a.post(func() { pr.reviewing = "" })
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.GhMarkdownHint()).FontSize(11.5).TextColor(p.faint).Grow(1).Shrink(1).MinWidth(0)
			if pr.reviewing != "" {
				if ui.Button(c.Key("pr-review-cancel"), locales.ButtonCancel()).Clicked() {
					a.post(func() { pr.reviewing = "" })
				}
				label := locales.PrRequestChanges()
				if pr.reviewing == "comment" {
					label = locales.PrSubmitReview()
				}
				if ui.PrimaryButton(c.Key("pr-review-send"), label).Disabled(empty || pr.sending).Clicked() {
					send = true
				}
				return
			}
			if ui.PrimaryButton(c.Key("pr-comment"), locales.GhComment()).Disabled(empty || pr.sending).Clicked() {
				send = true
			}
		})
		if send && !empty && !pr.sending {
			if pr.reviewing != "" {
				event, body := pr.reviewing, strings.TrimSpace(pr.comment)
				a.post(func() { a.reviewPR(v, event, body) })
			} else {
				a.post(func() { a.commentPR(v) })
			}
		}
	})
}

// startReview has the box of the conversation write a review that needs
// words: one that requests changes, or comments.
func (a *app) startReview(v *diffView, event string) {
	pr := v.commit.pr
	pr.reviewing, pr.focusComposer = event, true
	a.setPRPage(v, prConversation)
}

// prAct runs an action on the pull request of a tab in the background:
// it shows busy meanwhile, then says it is done, or why it failed, and
// reads the pull request again. then runs once it ends, told how it
// went. It reports whether the action started.
func (a *app) prAct(v *diffView, name, done string, fn func(r *git.Repo) error, then func(err error)) bool {
	pr := v.commit.pr
	ctx := a.diffContext(v)
	r := ctx.git.repo
	if r == nil || pr.acting != "" {
		return false
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return false
	}
	pr.acting = name
	bg.Go(func() {
		defer recoverCrash()
		err := fn(r)
		a.post(func() {
			pr.acting = ""
			if then != nil {
				then(err)
			}
			if err != nil {
				a.fail("%s", locales.PrActionFailed(name, ghMessage(err)))
				return
			}
			if done != "" {
				a.tell(note{message: done})
			}
			a.readPRView(v)
			ctx.git.prPoll.Expire()
			ctx.refreshGitHubNow()
		})
	})
	return true
}

// commentPR sends the comment written in a pull request's conversation.
func (a *app) commentPR(v *diffView) {
	pr := v.commit.pr
	body, n := strings.TrimSpace(pr.comment), pr.number
	if body == "" {
		return
	}
	// What was written stays, to send again, unless it went.
	pr.sending = a.prAct(v, locales.GhComment(), locales.GhCommented("#"+strconv.Itoa(n)), func(r *git.Repo) error {
		return r.CommentPR(n, body)
	}, func(err error) {
		pr.sending = false
		if err == nil {
			pr.comment = ""
		}
	})
}

// reviewPR reviews a pull request: approves it, requests changes or
// comments, with body.
func (a *app) reviewPR(v *diffView, event, body string) {
	pr := v.commit.pr
	n := pr.number
	done := locales.PrApproved()
	switch event {
	case "request-changes":
		done = locales.PrChangesRequested()
	case "comment":
		done = locales.PrReviewSent()
	}
	pr.sending = a.prAct(v, locales.PrReview(), done+" · #"+strconv.Itoa(n), func(r *git.Repo) error {
		return r.ReviewPR(n, event, body)
	}, func(err error) {
		pr.sending = false
		if err != nil {
			return
		}
		if body != "" {
			pr.comment = ""
		}
		pr.reviewing = ""
	}) && body != ""
}

// mergePR merges a pull request by method, deleting its branch on GitHub
// as the settings say.
func (a *app) mergePR(v *diffView, method string) {
	pr := v.commit.pr
	pv := pr.view
	if pv == nil {
		return
	}
	del := a.settings.PRDeleteBranch
	a.prAct(v, locales.PrMerge(), locales.PrMerged()+" · #"+strconv.Itoa(pr.number), func(r *git.Repo) error {
		return r.MergePR(pv, method, del)
	}, nil)
}

// closePR closes a pull request, or opens it again.
func (a *app) closePR(v *diffView, reopen bool) {
	n := v.commit.pr.number
	name, done := locales.PrClosePR(), locales.PrClosed()
	if reopen {
		name, done = locales.PrReopenPR(), locales.PrOpen()
	}
	a.prAct(v, name, done+" · #"+strconv.Itoa(n), func(r *git.Repo) error { return r.ClosePR(n, reopen) }, nil)
}

// readyPR marks a pull request ready for review, or a draft again.
func (a *app) readyPR(v *diffView, draft bool) {
	n := v.commit.pr.number
	name, done := locales.PrMarkReady(), locales.PrMarkedReady()
	if draft {
		name, done = locales.PrMarkDraft(), locales.PrMarkedDraft()
	}
	a.prAct(v, name, done, func(r *git.Repo) error { return r.ReadyPR(n, draft) }, nil)
}

// rerunFailed runs the failed jobs of a pull request's checks again.
func (a *app) rerunFailed(v *diffView) {
	pr := v.commit.pr
	var jobs []git.Check
	for _, ch := range pr.checks {
		if checkFailed(ch) {
			jobs = append(jobs, ch)
		}
	}
	if len(jobs) == 0 {
		return
	}
	a.prAct(v, locales.PrRerunFailed(), locales.PrRerunning(len(jobs)), func(r *git.Repo) error {
		var errs []error
		for _, ch := range jobs {
			errs = append(errs, r.RerunJob(ch))
		}
		return errors.Join(errs...)
	}, nil)
}

// prCommitsUI is the page of a pull request's commits, the oldest first,
// as GitHub lists them; each opens its changes.
func (a *app) prCommitsUI(c *ui.Context, v *diffView, now time.Time) {
	p := a.pal
	pr := v.commit.pr
	pv := pr.view
	switch {
	case pv == nil && pr.viewErr != "":
		ui.Text(c, pr.viewErr).FontSize(13).TextColor(p.muted)
		return
	case pv == nil:
		a.issueSkeleton(c)
		return
	case len(pv.Commits) == 0:
		ui.Text(c, locales.PrNoCommits()).FontSize(13).TextColor(p.muted)
		return
	}
	if more := pv.AllCommits - len(pv.Commits); more > 0 {
		ui.Text(c, locales.PrEarlierCommits(more)).FontSize(12).TextColor(p.muted)
	}
	ui.Column(c.Key("pr-commit-list")).Radius(8).Border(1, p.border).Clip().Children(func() {
		for i, cm := range pv.Commits {
			b := ui.ButtonBase(c.Key("commit:"+cm.Hash)).FillWidth().Height(44).Padding(0, 12).Gap(10).AlignItems(ui.Center).Label(cm.Subject)
			if i > 0 {
				b.BorderWidth(1, 0, 0, 0).BorderColor(p.border)
			}
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() {
				login := cm.Login
				if login == "" {
					login = cm.Author
				}
				a.ghAvatar(c, login, pv.Avatars[cm.Login], 20, now)
				ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, cm.Subject).FontSize(13).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
					ui.Text(c, locales.PrCommitBy(cm.Author, ago(cm.Date, now))).FontSize(11.5).TextColor(p.muted).SingleLine()
				})
				ui.Text(c, git.ShortHash(cm.Hash)).Font(a.font.family).FontSize(11.5).TextColor(p.accent).Padding(1, 6).Radius(5).
					Background(p.accent.Alpha(0.1)).SingleLine().Shrink(0)
			})
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item(locales.PrOpenCommit()).Chosen() {
					a.post(func() { a.showCommit(git.Commit{Hash: cm.Hash}, true, v.repoRoot) })
				}
				if m.Item(locales.CopyCommitHash()).Chosen() {
					a.post(func() { a.copyCommitHash(cm.Hash) })
				}
			})
			if b.Clicked() {
				hash := cm.Hash
				a.post(func() { a.showCommit(git.Commit{Hash: hash}, true, v.repoRoot) })
			}
		}
	})
}

// prChecksPageUI is the page of a pull request's checks: those failed
// first, each with what can be done to it.
func (a *app) prChecksPageUI(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	ctx := a.diffContext(v)
	switch {
	case !pr.checksOK && pr.viewing:
		a.issueSkeleton(c)
		return
	case !pr.checksOK:
		ui.Text(c, locales.PrChecksUnread()).FontSize(13).TextColor(p.muted)
		return
	case len(pr.checks) == 0:
		ui.Text(c, locales.ChecksNone()).FontSize(13).TextColor(p.muted)
		return
	}
	var passed, failed, running int
	for _, ch := range pr.checks {
		switch ch.Bucket {
		case "pass":
			passed++
		case "fail":
			failed++
		case "pending":
			running++
		}
	}
	ui.Row(c.Key("pr-checks-sum")).Gap(12).AlignItems(ui.Center).Children(func() {
		count := func(n int, s string, col ui.Color) {
			if n > 0 {
				ui.Text(c, s).FontSize(12.5).TextColor(col).FontFeatures("tnum").SingleLine()
			}
		}
		count(failed, locales.PrChecksFailed(failed), p.dangerFg)
		count(running, locales.PrChecksPending(running), p.modifiedFg)
		count(passed, locales.PrChecksPassed(passed), p.addedFg)
		ui.Spacer(c)
		if failed > 0 && ui.Button(c.Key("pr-rerun-all"), locales.PrRerunFailed()).Disabled(pr.acting != "").Clicked() {
			a.post(func() { a.rerunFailed(v) })
		}
	})
	where := "pull request #" + strconv.Itoa(pr.number)
	ui.Column(c.Key("pr-check-list")).Radius(8).Border(1, p.border).Clip().Children(func() {
		for i, ch := range a.sortedChecks(pr.checks) {
			row := ui.Row(c.Key("check:"+ch.Link+":"+ch.Name)).FillWidth().Height(38).Padding(0, 12).Gap(10).AlignItems(ui.Center).Label(ch.Title())
			if i > 0 {
				row.BorderWidth(1, 0, 0, 0).BorderColor(p.border)
			}
			if row.Hovered() {
				row.Background(p.hover)
			}
			row.Transition(hoverFade)
			row.Children(func() {
				icon, col := checkIcon(p, ch)
				ui.Icon(c, icon).FontSize(14).TextColor(col)
				ui.Text(c, ch.Title()).FontSize(13).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, ch.Description).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
				if checkFailed(ch) {
					if a.smallButton(c, iconSend, locales.ChecksSend()) {
						a.post(func() { ctx.sendCheckLog(ch, where, false) })
					}
					if a.smallButton(c, iconCopy, locales.ChecksCopyLog()) {
						a.post(func() { ctx.sendCheckLog(ch, where, true) })
					}
					if a.smallButton(c, iconRefresh, locales.ChecksRerun()) {
						a.post(func() { ctx.rerunCheck(ch, ""); a.readPRViewSoon(v) })
					}
				}
				if ch.Link != "" && a.smallButton(c, iconExternal, locales.PrOpenOnGitHub()) {
					a.services.OpenURL(ch.Link)
				}
			})
		}
	})
}

// sortedChecks are checks those failed first, then those running, then
// the rest, each kind in the order gh gave.
func (a *app) sortedChecks(checks []git.Check) []git.Check {
	rank := func(ch git.Check) int {
		switch ch.Bucket {
		case "fail":
			return 0
		case "pending":
			return 1
		case "cancel":
			return 2
		}
		return 3
	}
	out := make([]git.Check, 0, len(checks))
	for r := range 4 {
		for _, ch := range checks {
			if rank(ch) == r {
				out = append(out, ch)
			}
		}
	}
	return out
}

// readPRViewSoon reads the pull request of a tab again on its next poll,
// as a job runs again.
func (a *app) readPRViewSoon(v *diffView) {
	v.commit.pr.viewAt = time.Time{}
	v.commit.pr.viewAsked = time.Time{}
}

// prChangesUI is the page of a pull request's changes: its toolbar over
// the diff, or the shapes of it while its commits are read, or why they
// could not be.
func (a *app) prChangesUI(c *ui.Context, v *diffView) {
	pr := v.commit.pr
	if v.commit.pending() {
		ui.Scroll(c.Key("pr-scroll")).Grow(1).Children(func() {
			if pr.openErr == "" {
				a.prSkeleton(c)
				return
			}
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24, 24, 48, 24).Children(func() {
				a.githubCard(c, "pr-open-failed", iconTriangleAlert, locales.GhIssueFailed(v.name()), pr.openErr, time.Time{}, func() {
					if ui.PrimaryButton(c, locales.GhRetry()).Clicked() {
						n, threads := pr.number, pr.threads
						a.post(func() { a.diffContext(v).openPullRequestWith(n, prSamePage, threads, false, "") })
					}
				})
			})
		})
		return
	}
	a.prChangesBar(c, v)
	ui.Box(c.Key("pr-diff-body")).Grow(1).Shrink(1).MinHeight(0).FillWidth().Children(func() { a.diffBodyUI(c, v) })
}

// prChangesBar is the line over a pull request's diff: the commits it
// shows, how much changed, how many files are viewed, its threads, and
// whether its head moved since.
func (a *app) prChangesBar(c *ui.Context, v *diffView) {
	p := a.pal
	pr := v.commit.pr
	cm := v.commit
	added, removed := 0, 0
	if pv := pr.view; pv != nil && pv.HeadOid == cm.hash {
		added, removed = pv.Additions, pv.Deletions
	}
	ui.Row(c.Key("pr-changes-bar")).FillWidth().Height(32).Padding(0, 16).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).
		BorderColor(p.border).Shrink(0).Children(func() {
		hash := func(h string) {
			b := ui.ButtonBase(c.Key("bar-hash:"+h)).Height(18).Padding(0, 6).Radius(5).AlignItems(ui.Center).
				Background(p.accent.Alpha(0.1)).Label(locales.CopyCommitHash()).Tooltip(locales.CopyCommitHash()).Shrink(0)
			if b.Hovered() {
				b.Background(p.accent.Alpha(0.2))
			}
			b.Transition(hoverFade)
			b.Children(func() {
				ui.Text(c, git.ShortHash(h)).Font(a.font.family).FontSize(11.5).TextColor(p.accent).SingleLine()
			})
			if b.Clicked() {
				a.post(func() { a.copyCommitHash(h) })
			}
		}
		hash(cm.from)
		ui.Text(c, "→").FontSize(12).TextColor(p.muted).Shrink(0)
		hash(cm.hash)
		ui.Text(c, locales.CardFilesChanged(len(v.files))).FontSize(12).TextColor(p.muted).SingleLine().Shrink(0)
		if added+removed > 0 {
			ui.Text(c, "+"+strconv.Itoa(added)).FontSize(12).TextColor(p.addedFg).FontFeatures("tnum").SingleLine().Shrink(0)
			ui.Text(c, "−"+strconv.Itoa(removed)).FontSize(12).TextColor(p.dangerFg).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		if pr.err != "" {
			ui.Text(c, pr.err).FontSize(12).TextColor(p.dangerFg).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if pr.newer {
			ui.Text(c, locales.PrUpdated()).FontSize(12).FontWeight(500).TextColor(p.modifiedFg).SingleLine().Shrink(0)
			if ui.Button(c.Key("pr-reload"), locales.PrReload()).Shrink(0).Clicked() {
				a.post(func() { a.reloadPR(v) })
			}
		}
		if open, resolved := prThreadCounts(pr.ghThreads); open+resolved > 0 {
			label := locales.PrThreadsOpen(open)
			b := ui.ButtonBase(c.Key("pr-threads")).Height(22).Padding(0, 8).Gap(5).Radius(5).AlignItems(ui.Center).Shrink(0).
				Label(label).Tooltip(locales.PrThreadsTip(strconv.Itoa(open), strconv.Itoa(resolved)))
			if b.Hovered() || pr.showResolved {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() {
				ui.Icon(c, iconComment).FontSize(12).TextColor(p.muted)
				ui.Text(c, label).FontSize(12).TextColor(p.text).SingleLine()
			})
			b.Menu(func(m *ui.Menu) {
				if m.Item(locales.PrShowResolved()).Checked(pr.showResolved).Disabled(resolved == 0).Chosen() {
					a.post(func() { pr.showResolved = !pr.showResolved; pr.threadGen++; v.changed() })
				}
				ctx, n := a.diffContext(v), pr.number
				if m.Item(locales.GhAddressReview()).Disabled(open == 0).Chosen() {
					a.post(func() { a.openPRWithThreads(ctx, n) })
				}
			})
		}
		if pr.viewedOK && len(v.files) > 0 {
			ui.Text(c, locales.PrViewedCount(strconv.Itoa(v.viewedCount()), strconv.Itoa(len(v.files)))).FontSize(12).TextColor(p.muted).
				FontFeatures("tnum").SingleLine().Shrink(0)
		}
	})
}

// prThreadCounts counts the review threads not resolved, and those
// resolved.
func prThreadCounts(ths []git.GHThread) (open, resolved int) {
	for _, th := range ths {
		if th.Resolved {
			resolved++
		} else {
			open++
		}
	}
	return open, resolved
}
