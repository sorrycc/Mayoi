package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// issueView is the tab of an issue, or of the conversation of a pull
// request: its state, labels, body and comments, with a box to comment;
// or, with no number, a new issue being written.
type issueView struct {
	repoRoot string
	number   int
	// info is what GitHub told of it, nil until read: reading is set while
	// gh runs, and err why it failed.
	info    *git.GHIssue
	reading bool
	err     string
	pane    ui.Handle
	// texts are the body and the comments parsed, as of parsedOf; stale
	// is set once a picture of them arrived, for them to be parsed again.
	texts    [][]markdownBlock
	parsedOf *git.GHIssue
	stale    bool
	cache    markdownCache
	// reply is the comment being written, sending while it is sent, and
	// acting while the issue is being closed or opened again.
	reply   string
	sending bool
	acting  bool
	// title and body are those of a new issue, which creating sends.
	title, body string
	creating    bool
}

func (v *issueView) name() string {
	switch {
	case v.number == 0:
		return locales.GhNewIssueTitle()
	case v.info != nil && v.info.Title != "":
		return "#" + strconv.Itoa(v.number) + " " + v.info.Title
	}
	return "#" + strconv.Itoa(v.number)
}

func (v *issueView) icon() *ui.SVG {
	if v.info != nil && v.info.PR {
		return iconPullRequest
	}
	return iconIssue
}

// clone is the issue again, for another column, without its drafts.
func (v *issueView) clone() *issueView {
	return &issueView{repoRoot: v.repoRoot, number: v.number, info: v.info, err: v.err}
}

// issueTab is the column and place of the tab of issue n of the
// repository at root, nil for none.
func (a *app) issueTab(n int, root string) (*column, int) {
	for _, col := range a.cols {
		for i, t := range col.tabs {
			if t.issue != nil && t.issue.number == n && n > 0 && t.issue.repoRoot == root {
				return col, i
			}
		}
	}
	return nil, -1
}

// openIssue shows issue n, or the conversation of pull request n, in a
// tab, reading it again: in the preview tab of the file column, unless
// keep.
func (a *app) openIssue(ctx *gitContext, n int, keep bool) {
	r := ctx.git.repo
	if r == nil {
		return
	}
	if col, i := a.issueTab(n, r.Root); col != nil {
		if keep {
			a.keepTab(col.tabs[i])
		}
		a.activate(col, i, true)
		a.readIssue(col.tabs[i].issue)
		return
	}
	v := &issueView{repoRoot: r.Root, number: n}
	col := a.fileColumn()
	if keep {
		at := min(col.active+1, len(col.tabs))
		a.addTab(col, at, &colTab{issue: v})
		a.activate(col, at, true)
	} else {
		a.previewIn(col, func(at int) {
			a.addTab(col, at, &colTab{issue: v, transient: true})
			a.activate(col, at, false)
		})
	}
	a.rememberTabs()
	a.readIssue(v)
}

// newIssue opens the tab of a new issue of the repository.
func (a *app) newIssue(ctx *gitContext) {
	r := ctx.git.repo
	if r == nil {
		return
	}
	v := &issueView{repoRoot: r.Root}
	col := a.fileColumn()
	at := min(col.active+1, len(col.tabs))
	a.addTab(col, at, &colTab{issue: v})
	a.activate(col, at, true)
}

// issueContext is the work tree of the repository an issue's tab is of.
func (a *app) issueContext(v *issueView) *gitContext {
	if ctx := a.repositoryForRoot(v.repoRoot); ctx.git.repo != nil {
		return ctx
	}
	// One kept of a folder within its repository: the repository's.
	return a.repositoryForPath(v.repoRoot)
}

// fetchIssue reads issue n of a work tree, which the tests replace.
var fetchIssue = func(r *git.Repo, n int) (*git.GHIssue, error) { return r.Issue(n) }

// readIssue reads what GitHub has of an issue's tab, in the background.
func (a *app) readIssue(v *issueView) {
	r := a.issueContext(v).git.repo
	if v.number == 0 || v.reading || r == nil || !a.online() {
		return
	}
	v.reading = true
	n := v.number
	bg.Go(func() {
		defer recoverCrash()
		info, err := fetchIssue(r, n)
		a.post(func() {
			v.reading = false
			switch {
			case err == nil:
				v.info, v.err = info, ""
			case errors.Is(err, remote.ErrOffline):
			case errors.Is(err, git.ErrNoPR):
				v.err = locales.GhIssueNotFound("#" + strconv.Itoa(n))
			default:
				v.err = ghMessage(err)
			}
			a.rememberTabs()
			a.invalidate()
		})
	})
}

// ghPictureHost reports whether a picture of an issue may be fetched: it
// is of GitHub, which holds what its people attach.
func ghPictureHost(raw, repoURL string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return true
	}
	if r, err := url.Parse(repoURL); err == nil && r.Hostname() != "" {
		return host == strings.ToLower(r.Hostname())
	}
	return false
}

// parseIssue parses the body and the comments of an issue's tab, as they
// were read or as their pictures arrive.
func (a *app) parseIssue(v *issueView) {
	is := v.info
	if is == nil || v.parsedOf == is && !v.stale {
		return
	}
	v.parsedOf, v.stale = is, false
	v.cache.arrived = func() {
		a.post(func() {
			v.stale = true
			a.invalidate()
		})
	}
	v.cache.allow = func(u string) bool { return ghPictureHost(u, is.URL) }
	// Relative links are of the repository's files, as GitHub has them.
	path := filepath.Join(v.repoRoot, "ISSUE.md")
	v.texts = v.texts[:0]
	v.texts = append(v.texts, parseMarkdown([]byte(ghPictures(is.Body)), path, v.repoRoot, nil, &v.cache))
	for _, c := range is.Comments {
		v.texts = append(v.texts, parseMarkdown([]byte(ghPictures(c.Body)), path, v.repoRoot, nil, &v.cache))
	}
}

var (
	ghImgTag  = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	ghImgAttr = regexp.MustCompile(`(?i)\b(src|alt)\s*=\s*("([^"]*)"|'([^']*)')`)
)

// ghPictures writes the pictures of a text of GitHub as Markdown's: a
// picture pasted into an issue is an <img> tag, which Markdown would
// show as its text.
func ghPictures(text string) string {
	return ghImgTag.ReplaceAllStringFunc(text, func(tag string) string {
		var src, alt string
		for _, m := range ghImgAttr.FindAllStringSubmatch(tag, -1) {
			val := m[3] + m[4]
			if strings.EqualFold(m[1], "src") {
				src = val
			} else {
				alt = val
			}
		}
		if src == "" {
			return tag
		}
		alt = strings.NewReplacer("[", "", "]", "").Replace(alt)
		return "![" + alt + "](<" + src + ">)"
	})
}

// issueWidth is the most the text of an issue's tab spans.
const issueWidth = 760

// issueViewUI draws the tab of an issue.
func (a *app) issueViewUI(c *ui.Context, v *issueView) {
	p := a.pal
	ctx := a.issueContext(v)
	if v.info == nil && v.err == "" && !v.reading && v.number > 0 {
		a.readIssue(v)
	}
	a.parseIssue(v)
	pane := ui.Column(c.Key("issue")).Bind(&v.pane).Fill().Background(p.editor).Focusable().FocusRing(false).Label(v.name())
	pane.Children(func() {
		if v.reading && v.info == nil {
			ui.Progress(c.Key("issue-activity"), -1).Height(2).Radius(0).Label(locales.GhReadingIssue(v.name()))
		} else {
			ui.Box(c.Key("issue-activity")).Height(2)
		}
		ui.Scroll(c.Key("issue-scroll")).Grow(1).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24, 24, 48, 24).Children(func() {
				body := ui.Column(c).FillWidth().MaxWidth(issueWidth).Gap(14)
				body.Children(func() {
					switch {
					case v.number == 0:
						a.issueCompose(c, ctx, v)
					case v.info != nil:
						a.issueContent(c, ctx, v)
					case v.err != "":
						a.issueFailed(c, v)
					default:
						a.issueSkeleton(c)
					}
				})
			})
		})
	})
}

// issueSkeleton is the shapes of an issue while it is read.
func (a *app) issueSkeleton(c *ui.Context) {
	bar := a.pal.text.Alpha(0.07)
	ui.Column(c.Key("issue-skeleton")).Gap(12).Children(func() {
		ui.Box(c).Size(70, 18).Radius(9).Background(bar)
		ui.Box(c).WidthPercent(70).Height(16).Radius(5).Background(bar)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(issueMetaAvatar, issueMetaAvatar).Radius(issueMetaAvatar / 2).Background(bar)
			ui.Box(c).WidthPercent(40).Height(8).Radius(4).Background(bar)
		})
		ui.Box(c).FillWidth().Height(120).Radius(8).Background(bar).Margin(8, 0, 0, 0)
	})
}

// issueFailed says why an issue could not be read, with what to do, on
// the card of the GitHub view's states.
func (a *app) issueFailed(c *ui.Context, v *issueView) {
	a.githubCard(c, "issue-failed", iconTriangleAlert, locales.GhIssueFailed(v.name()), v.err, time.Time{}, func() {
		if ui.PrimaryButton(c, locales.GhRetry()).Clicked() {
			v.err = ""
			a.readIssue(v)
		}
	})
}

// issueState is how an issue's state shows: its word and color.
func (a *app) issueState(is *git.GHIssue) (string, ui.Color) {
	p := a.pal
	switch {
	case is.State == "MERGED":
		return locales.PrMerged(), p.accent
	case is.State == "CLOSED" && is.StateReason == "NOT_PLANNED":
		return locales.GhNotPlanned(), p.muted
	case is.State == "CLOSED" && is.PR:
		return locales.PrClosed(), p.dangerFg
	case is.State == "CLOSED":
		return locales.PrClosed(), p.accent
	}
	return locales.PrOpen(), p.addedFg
}

// issueContent is an issue as read: its state and title, who opened it
// and when, its labels, assignees and milestone, what it can do, the pull
// requests that close it, its body and comments, and the box to comment.
func (a *app) issueContent(c *ui.Context, ctx *gitContext, v *issueView) {
	p := a.pal
	is := v.info
	now := c.Now()
	state, col := a.issueState(is)
	ui.Row(c.Key("issue-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, state).FontSize(12).FontWeight(600).TextColor(col).Padding(2, 9).Radius(10).Background(col.Alpha(0.14))
		ui.Text(c, "#"+strconv.Itoa(is.Number)).FontSize(13).TextColor(p.muted).FontFeatures("tnum")
		if is.PR && is.HeadRef != "" {
			ui.Text(c, is.BaseRef+" ← "+is.HeadRef).FontSize(12).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if v.reading {
			ui.Text(c, locales.GhReading()).FontSize(11.5).TextColor(p.faint)
		}
	})
	ui.Text(c.Key("issue-title"), is.Title).FontSize(20).FontWeight(600).TextColor(p.text).LineHeight(1.3).Selectable()
	// Who opened it, by their picture, and when; then its labels, on the
	// chips of the list, who it is assigned to and its milestone.
	ui.Row(c.Key("issue-meta")).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
		var parts []string
		if !is.Created.IsZero() {
			parts = append(parts, locales.GhOpenedBy(is.Author, ago(is.Created, now)))
		}
		if is.AllComments > 0 {
			parts = append(parts, locales.GhComments(is.AllComments))
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if is.Author != "" {
				a.ghAvatar(c, is.Author, is.Avatars[is.Author], issueMetaAvatar, now)
			}
			ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(p.muted)
		})
		for _, l := range is.Labels {
			a.ghLabel(c, l, 10.5)
		}
		if len(is.Assignees) > 0 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(0, 0, 0, 4).Children(func() {
				ui.Row(c).Gap(2).Children(func() {
					for _, as := range is.Assignees {
						a.ghAvatar(c.Key(as), as, is.Avatars[as], issueMetaAvatar, now)
					}
				})
				ui.Text(c, locales.GhAssignees(strings.Join(is.Assignees, ", "))).FontSize(12).TextColor(p.muted)
			})
		}
		if is.Milestone != "" {
			ui.Text(c, locales.GhMilestone(is.Milestone)).FontSize(12).TextColor(p.muted).Margin(0, 0, 0, 4)
		}
	})
	a.issueActions(c, ctx, v)
	for _, l := range is.Linked {
		n := l.Number
		b := ui.ButtonBase(c.Key("linked:"+strconv.Itoa(n))).Padding(6, 10).Gap(8).Radius(6).AlignItems(ui.Center).
			Border(1, p.border).Label(l.Title)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Transition(hoverFade)
		b.Children(func() {
			ui.Icon(c, iconPullRequest).FontSize(13).TextColor(p.accent)
			ui.Text(c, locales.GhLinkedPR("#"+strconv.Itoa(n), l.Title)).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
		})
		if b.Clicked() {
			title := l.Title
			a.post(func() { ctx.openPullRequestWith(n, false, true, title) })
		}
	}
	if len(v.texts) > 0 {
		a.issueCard(c, "issue-body", is.Author, is.Avatars[is.Author], is.Created, "", v.texts[0], now)
	}
	for i, cm := range is.Comments {
		if i+1 < len(v.texts) {
			a.issueCard(c, "comment:"+strconv.Itoa(i), cm.Author, is.Avatars[cm.Author], cm.Created, cm.URL, v.texts[i+1], now)
		}
	}
	if more := is.AllComments - len(is.Comments); more > 0 {
		b := ui.ButtonBase(c.Key("issue-more")).Padding(4, 2).Label(locales.GhMoreComments(more))
		b.Children(func() { ui.Text(c, locales.GhMoreComments(more)).FontSize(12).TextColor(p.accent) })
		if b.Clicked() {
			a.services.OpenURL(is.URL)
		}
	}
	a.issueReply(c, ctx, v)
}

// issueActions are the buttons of an issue's tab.
func (a *app) issueActions(c *ui.Context, ctx *gitContext, v *issueView) {
	is := v.info
	o := ctx.githubOverview()
	ui.Row(c.Key("issue-actions")).Gap(6).Wrap().Children(func() {
		if is.PR {
			if ui.PrimaryButton(c, locales.PrOpenChanges()).Clicked() {
				a.post(func() { ctx.openPullRequestWith(is.Number, false, true, is.Title) })
			}
			if ui.Button(c.Key("issue-threads"), locales.GhAddressReview()).Clicked() {
				a.post(func() { a.openPRWithThreads(ctx, is.Number) })
			}
		} else if is.State == "OPEN" {
			if ui.PrimaryButton(c, locales.GhWorkOnIssue()).Disabled(!ctx.git.canMutate() || o == nil || o.DefaultBranch == "").Clicked() {
				a.post(func() { a.workOnIssue(ctx, is.Number) })
			}
			if ui.Button(c.Key("issue-send"), locales.GhSendIssue()).Clicked() {
				a.post(func() { a.sendIssue(ctx, is.Number) })
			}
		}
		if !is.PR && is.CanUpdate {
			if is.State == "OPEN" {
				if ui.Button(c.Key("issue-close"), locales.GhCloseIssue()).Disabled(v.acting).Clicked() {
					a.post(func() { a.setIssueOpen(ctx, v, false, false) })
				}
				if ui.Button(c.Key("issue-close-np"), locales.GhCloseNotPlanned()).Disabled(v.acting).Clicked() {
					a.post(func() { a.setIssueOpen(ctx, v, false, true) })
				}
			} else if ui.Button(c.Key("issue-reopen"), locales.GhReopenIssue()).Disabled(v.acting).Clicked() {
				a.post(func() { a.setIssueOpen(ctx, v, true, false) })
			}
		}
		if ui.Button(c.Key("issue-web"), locales.PrOpenOnGitHub()).Clicked() {
			a.services.OpenURL(is.URL)
		}
		if ui.Button(c.Key("issue-copy"), locales.GhCopyLink()).Clicked() {
			a.services.WriteClipboard(is.URL)
			a.tell(note{message: locales.GhLinkCopied("#" + strconv.Itoa(is.Number))})
		}
	})
}

// issueMetaAvatar is how wide a person's picture is by the line of who
// opened an issue, and issueCardAvatar by the name over a comment.
const (
	issueMetaAvatar = 18
	issueCardAvatar = 20
)

// issueCard is the body or a comment of an issue: who wrote it, by their
// picture at avatar, and when, over its text.
func (a *app) issueCard(c *ui.Context, key, author, avatar string, at time.Time, link string, blocks []markdownBlock, now time.Time) {
	p := a.pal
	ui.Column(c.Key(key)).Radius(8).Border(1, p.border).Clip().Children(func() {
		ui.Row(c).Height(36).Padding(0, 8, 0, 10).Gap(8).AlignItems(ui.Center).Background(p.chrome).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			name := author
			if name == "" {
				name = locales.GhGhost()
			}
			a.ghAvatar(c, name, avatar, issueCardAvatar, now)
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine()
			if !at.IsZero() {
				ui.Text(c, ago(at, now)).FontSize(12).TextColor(p.muted).SingleLine().Tooltip(dateTime(at.Local()))
			}
			ui.Spacer(c)
			if link != "" && a.smallButton(c, iconExternal, locales.PrOpenOnGitHub()) {
				a.services.OpenURL(link)
			}
		})
		ui.Column(c).Padding(10, 14, 12, 14).Gap(8).Selectable().Children(func() {
			if len(blocks) == 0 {
				ui.Text(c, locales.GhNoDescription()).FontSize(13).TextColor(p.faint).Italic()
				return
			}
			a.issueMarkdown(c, blocks)
		})
	})
}

// issueMarkdown draws the blocks of an issue's text: headings, paragraphs
// whose links open, lists, quotes, code and the pictures GitHub holds.
func (a *app) issueMarkdown(c *ui.Context, blocks []markdownBlock) {
	p := a.pal
	size := a.settings.FontSize
	width := float32(issueWidth - 28)
	for k, b := range blocks {
		indent := float32(b.indent)*16 + float32(b.quote)*14
		// A marker stands by the first line of its item, not the middle.
		row := ui.Row(c.Key(k)).Gap(6).Padding(0, 0, 0, indent).AlignItems(ui.Start)
		if b.quote > 0 {
			row.BorderWidth(0, 0, 0, 3).BorderColor(p.border)
		}
		row.Children(func() {
			if b.marker != "" {
				a.markdownMarker(c, b, size, nil)
			}
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				switch b.kind {
				case markdownHeading:
					sz := size * (1.5 - 0.12*float32(min(b.level, 4)))
					a.markdownText(c, nil, "", b.runs, sz, p.text, false, nil)
				case markdownRule:
					ui.Box(c).FillWidth().Height(1).Background(p.border).Margin(4, 0)
				case markdownCodeBlock:
					var spans []ui.Span
					for _, r := range b.runs {
						spans = append(spans, a.markdownSpan(r, p.text, size*0.9, true))
					}
					ui.RichText(c, spans...).FontSize(size*0.9).Background(p.chrome).Padding(8, 10).Radius(6)
				case markdownImageBlock:
					for _, r := range b.runs {
						if r.bitmap != nil && r.w > 0 {
							a.markdownImage(c, nil, "", r, width)
						} else if r.image != "" {
							a.markdownNoImage(c, nil, "", r, size, ui.Start)
						}
					}
				default:
					if len(b.runs) == 1 && b.runs[0].image != "" {
						if r := b.runs[0]; r.bitmap != nil && r.w > 0 {
							a.markdownImage(c, nil, "", r, width)
						} else {
							a.markdownNoImage(c, nil, "", r, size, ui.Start)
						}
						return
					}
					color := p.text
					if b.quote > 0 {
						color = p.muted
					}
					a.markdownText(c, nil, "", b.runs, size, color, false, nil)
				}
			})
		})
	}
}

// issueReply is the box to comment on an issue: ⌘↵ sends what it has.
func (a *app) issueReply(c *ui.Context, ctx *gitContext, v *issueView) {
	p := a.pal
	ui.Column(c.Key("issue-reply")).Gap(8).Margin(6, 0, 0, 0).Children(func() {
		in := a.textKeys(ui.TextArea(c.Key("issue-reply-text"), &v.reply)).FontSize(13).Lines(3, 14).Padding(8, 10).
			Placeholder(locales.GhCommentPlaceholder()).Label(locales.GhCommentLabel())
		if v.sending {
			in.ReadOnly(true)
		}
		send := in.Shortcut(ui.Cmd, ui.KeyEnter)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.GhMarkdownHint()).FontSize(11.5).TextColor(p.faint).Grow(1)
			if ui.PrimaryButton(c, locales.GhComment()).Disabled(strings.TrimSpace(v.reply) == "" || v.sending).Clicked() {
				send = true
			}
		})
		if send && strings.TrimSpace(v.reply) != "" && !v.sending {
			a.post(func() { a.sendComment(ctx, v) })
		}
	})
}

// sendComment sends the comment written on an issue, and reads it again.
func (a *app) sendComment(ctx *gitContext, v *issueView) {
	r, is, body := ctx.git.repo, v.info, strings.TrimSpace(v.reply)
	if r == nil || is == nil || body == "" {
		return
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return
	}
	v.sending = true
	bg.Go(func() {
		defer recoverCrash()
		err := r.Comment(is, body)
		a.post(func() {
			v.sending = false
			if err != nil {
				// What was written stays, to send again.
				a.fail("%s", locales.GhCommentFailed(ghMessage(err)))
				return
			}
			v.reply = ""
			a.tell(note{message: locales.GhCommented("#" + strconv.Itoa(is.Number))})
			a.readIssue(v)
			ctx.refreshGitHubNow()
		})
	})
}

// setIssueOpen closes an issue, as completed or not planned, or opens it
// again; the toast that says so undoes it.
func (a *app) setIssueOpen(ctx *gitContext, v *issueView, open, notPlanned bool) {
	r, is := ctx.git.repo, v.info
	if r == nil || is == nil || v.acting {
		return
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return
	}
	v.acting = true
	n := is.Number
	bg.Go(func() {
		defer recoverCrash()
		var err error
		if open {
			err = r.ReopenIssue(n)
		} else {
			err = r.CloseIssue(n, notPlanned)
		}
		a.post(func() {
			v.acting = false
			if err != nil {
				a.fail("%s", locales.GhIssueActionFailed(ghMessage(err)))
				return
			}
			msg, undo := locales.GhReopened("#"+strconv.Itoa(n)), func() { a.setIssueOpen(ctx, v, false, false) }
			if !open {
				msg, undo = locales.GhClosed("#"+strconv.Itoa(n)), func() { a.setIssueOpen(ctx, v, true, false) }
			}
			a.tell(note{message: msg, label: locales.ButtonUndo(), action: undo})
			a.readIssue(v)
			ctx.refreshGitHubNow()
		})
	})
}

// issueCompose is the form of a new issue: its title and its body, which
// Create Issue sends.
func (a *app) issueCompose(c *ui.Context, ctx *gitContext, v *issueView) {
	p := a.pal
	o := ctx.githubOverview()
	ui.Text(c.Key("compose-title"), locales.GhNewIssueTitle()).FontSize(20).FontWeight(600).TextColor(p.text)
	if o != nil {
		ui.Text(c.Key("compose-repo"), o.Repo).FontSize(12).TextColor(p.muted)
	}
	title := a.textKeys(ui.TextInput(c.Key("compose-title-input"), &v.title)).FontSize(14).AutoFocus().
		Placeholder(locales.GhIssueTitlePlaceholder()).Label(locales.GhIssueTitleLabel())
	body := a.textKeys(ui.TextArea(c.Key("compose-body"), &v.body)).FontSize(13).Lines(8, 30).Padding(8, 10).
		Placeholder(locales.GhIssueBodyPlaceholder()).Label(locales.GhIssueBodyLabel())
	if v.creating {
		title.ReadOnly(true)
		body.ReadOnly(true)
	}
	create := body.Shortcut(ui.Cmd, ui.KeyEnter) || title.Shortcut(ui.Cmd, ui.KeyEnter)
	ui.Row(c.Key("compose-actions")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.GhMarkdownHint()).FontSize(11.5).TextColor(p.faint).Grow(1)
		if ui.PrimaryButton(c, locales.GhCreateIssue()).Disabled(strings.TrimSpace(v.title) == "" || v.creating).Clicked() {
			create = true
		}
	})
	if create && strings.TrimSpace(v.title) != "" && !v.creating {
		a.post(func() { a.createIssue(ctx, v) })
	}
}

// createIssue sends a new issue, and has its tab show it once made.
func (a *app) createIssue(ctx *gitContext, v *issueView) {
	r := ctx.git.repo
	if r == nil {
		return
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return
	}
	title, body := strings.TrimSpace(v.title), strings.TrimSpace(v.body)
	v.creating = true
	bg.Go(func() {
		defer recoverCrash()
		n, err := r.CreateIssue(title, body)
		a.post(func() {
			v.creating = false
			if err != nil {
				a.fail("%s", locales.GhCreateFailed(ghMessage(err)))
				return
			}
			v.number, v.title, v.body = n, "", ""
			a.tell(note{message: locales.GhCreated("#" + strconv.Itoa(n))})
			a.readIssue(v)
			a.rememberTabs()
			ctx.refreshGitHubNow()
		})
	})
}
