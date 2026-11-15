package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// How often the GitHub view asks GitHub again: while it shows, and sooner
// while checks of a pull request listed run; seldom while it is hidden,
// for the count on its switch; and seldom once gh could not tell.
const (
	ghShownEvery   = 60 * time.Second
	ghPendingEvery = 30 * time.Second
	ghHiddenEvery  = 5 * time.Minute
	ghOffEvery     = 10 * time.Minute
)

// ghGetCLI is where the GitHub CLI is to be had.
const ghGetCLI = "https://cli.github.com"

// githubState is what a work tree has of GitHub: its remotes there, and
// the issues and pull requests open, as GitHub last told.
type githubState struct {
	// remotes are its remotes on GitHub, read as remotesOf names the
	// remotes read with the status; readingRemotes while they are read.
	remotes        []git.GitHubRemote
	remotesOf      string
	readingRemotes bool
	// overview is what GitHub told of overviewOf, nil until it told;
	// problem and err why it could not tell last, poll asks again, and gen
	// counts the refreshes asked for.
	overview   *git.GHOverview
	overviewOf *git.Repo
	problem    git.GHProblem
	err        string
	poll       bg.Poller[ghAsked]
	gen        int
	loading    bool
}

// ghAsked is what GitHub was asked of: a work tree, as of its refs'
// changes and the refreshes asked for.
type ghAsked struct {
	repo      *git.Repo
	refs, gen int
}

// githubPanel is the GitHub view of the sidebar: its query, its list and
// the row chosen, and the sections folded.
type githubPanel struct {
	query     string
	input     ui.Handle
	list      ui.ListState
	row       int
	collapsed map[string]bool
	// skeleton is set while the shapes of the rows show, for the rows to
	// fade in over them from cameAt.
	skeleton bool
	cameAt   time.Time
	// rows are the rows last built, of rowsOf: githubRows builds them
	// again only as the overview, the branch, the query or the sections
	// folded change. words are each item's, lowercased, for the query,
	// as of wordsOf.
	rows    []ghRow
	rowsOf  ghRowsKey
	built   bool
	words   map[*git.GHItem]string
	wordsOf *git.GHOverview
	// appear fades in the rows that come, as a section opens or a refresh
	// brings an item.
	appear appearing
	// scrolledAt is when the list last scrolled: its rows show no card
	// until it has rested for scmScrollQuiet.
	scrolledAt time.Time
}

// ghRowsKey is what the rows of the GitHub view are of.
type ghRowsKey struct {
	o      *git.GHOverview
	branch string
	query  string
	folded uint8
}

// ghSectionIDs are the ids of the sections of the GitHub view, in order.
var ghSectionIDs = [...]string{"branch", "review", "mine", "assigned", "issues", "prs"}

// fetchGitHubOverview asks GitHub for the overview of a work tree, which
// the tests replace.
var fetchGitHubOverview = func(r *git.Repo) (*git.GHOverview, error) { return r.GitHubOverview() }

// readGitHubRemotes lists the remotes of a work tree on GitHub, which the
// tests replace.
var readGitHubRemotes = func(r *git.Repo) ([]git.GitHubRemote, error) { return r.GitHubRemotes() }

// refreshGitHubRemotes reads in the background which remotes of the work
// tree are on GitHub, as the remotes change.
func (a *gitContext) refreshGitHubRemotes() {
	g := a.git
	if !a.live() {
		return
	}
	key := fmt.Sprintf("%p\x00%s", g.repo, strings.Join(g.remotes, "\x00"))
	if g.gh.remotesOf == key || g.gh.readingRemotes || !a.app.online() {
		return
	}
	g.gh.readingRemotes = true
	r := g.repo
	bg.Go(func() {
		defer recoverCrash()
		rs, err := readGitHubRemotes(r)
		a.app.post(func() {
			g.gh.readingRemotes = false
			if g.repo != r || errors.Is(err, remote.ErrOffline) {
				return
			}
			g.gh.remotesOf, g.gh.remotes = key, rs
			a.app.invalidate()
		})
	})
}

// githubKnown reports whether it is known yet whether the work tree is on
// GitHub.
func (a *gitContext) githubKnown() bool {
	return a.git.looked && (a.git.repo == nil || a.git.gh.remotesOf != "")
}

// onGitHub reports whether the work tree has a remote on GitHub.
func (a *gitContext) onGitHub() bool { return a.git.repo != nil && len(a.git.gh.remotes) > 0 }

// hasGitHub reports whether the sidebar has the GitHub view: the folder's
// repository is on GitHub.
func (a *app) hasGitHub() bool { return a.root != "" && a.repository().onGitHub() }

// githubShown reports whether the sidebar shows the GitHub view.
func (a *app) githubShown() bool {
	return !a.window.SidebarClosed && a.window.SidebarView == "github"
}

// showGitHub shows the GitHub view in the sidebar, with the keyboard in
// its query.
func (a *app) showGitHub() {
	if a.root == "" {
		return
	}
	a.window.SidebarClosed, a.window.SidebarView = false, "github"
	a.settings.Changed()
	a.github.input.Focus()
	a.repository().git.gh.poll.Expire()
}

// refreshGitHub asks GitHub in the background for the issues and pull
// requests open, once it is known the work tree is on GitHub: as its
// refs change, as a refresh is asked for, or as the last answer gets old;
// only while the window shows the project.
func (a *gitContext) refreshGitHub() {
	g := a.git
	a.refreshGitHubRemotes()
	if !a.live() || !a.onGitHub() || !a.app.online() || !a.app.host.shown() {
		return
	}
	r := g.repo
	key := ghAsked{r, g.refsGen, g.gh.gen}
	if !g.gh.poll.Start(key, a.ghEvery()) {
		return
	}
	if g.gh.overviewOf != r {
		g.gh.overview, g.gh.problem, g.gh.err = nil, git.GHFine, ""
	}
	g.gh.loading = true
	bg.Go(func() {
		defer recoverCrash()
		o, err := fetchGitHubOverview(r)
		a.app.post(func() {
			if !g.gh.poll.Done(key) || g.repo != r {
				return
			}
			g.gh.loading = false
			switch {
			case err == nil:
				g.gh.overview, g.gh.overviewOf, g.gh.problem, g.gh.err = o, r, git.GHFine, ""
				g.ghOK = true
				// The history marks the heads of the pull requests listed.
				g.prHeads, g.prHeadsRepo, g.prHeadsOff = o.PRHeads(), r, false
			case errors.Is(err, remote.ErrOffline):
				// The host is away: what was read stays until it is back.
			default:
				g.gh.overviewOf, g.gh.problem, g.gh.err = r, git.GitHubProblem(err), ghMessage(err)
			}
			g.gh.poll.Keep(key, a.ghEvery(), a.app.invalidate)
		})
	})
}

// ghMessage is why gh failed, without what all its failures say.
func ghMessage(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), git.ErrNoGH.Error()+":"))
}

// ghEvery is how long the answer of GitHub is good for.
func (a *gitContext) ghEvery() time.Duration {
	g := a.git
	switch {
	case g.gh.problem != git.GHFine:
		return ghOffEvery
	case !a.app.githubShown():
		return ghHiddenEvery
	}
	if o := a.githubOverview(); o != nil {
		for _, it := range o.PRs {
			if it.Checks == git.ChecksPending && (it.Author == o.Viewer || it.HeadRef == a.branch()) {
				return ghPendingEvery
			}
		}
	}
	return ghShownEvery
}

// githubOverview is what GitHub last told of the work tree, nil for none.
func (a *gitContext) githubOverview() *git.GHOverview {
	g := a.git
	if g.gh.overviewOf != g.repo {
		return nil
	}
	return g.gh.overview
}

// refreshGitHubNow asks GitHub again, at once.
func (a *gitContext) refreshGitHubNow() {
	a.git.gh.gen++
	a.app.invalidate()
}

// githubCount is the count on the GitHub view's switch: the pull requests
// whose review is asked of the signed in user.
func (a *app) githubCount() int {
	o := a.repository().githubOverview()
	if o == nil {
		return 0
	}
	n := 0
	for _, it := range o.PRs {
		if it.ReviewAsked {
			n++
		}
	}
	return n
}

// ghRow is a row of the GitHub view: the header of a section, an issue or
// a pull request in it, or a note.
type ghRow struct {
	key     string
	section string
	header  bool
	first   bool // the first header, with no room above it
	title   string
	count   int
	item    *git.GHItem
	number  string // an item's, as #12
	by      string // an item's author, but the viewer's own
	note    string
	link    string // a note's, which a click opens
}

// ghSection is a section of the GitHub view, and the items in it.
type ghSection struct {
	id, title string
	items     []*git.GHItem
	// always is set for a section that shows with nothing in it, which
	// none says then.
	always bool
	none   string
	more   int    // how many more GitHub has than those listed
	link   string // where they are
}

// githubSections are the sections of the GitHub view: the branch's pull
// request, those whose review is asked, those of the signed in user, the
// issues assigned to them, then every issue and pull request open.
func (a *gitContext) githubSections(o *git.GHOverview) []ghSection {
	branch := a.branch()
	var this, review, mine, assigned, issues, prs []*git.GHItem
	for i := range o.PRs {
		it := &o.PRs[i]
		switch {
		case branch != "" && it.HeadRef == branch:
			this = append(this, it)
		case it.ReviewAsked:
			review = append(review, it)
		case o.Viewer != "" && it.Author == o.Viewer:
			mine = append(mine, it)
		default:
			prs = append(prs, it)
		}
	}
	for i := range o.Issues {
		it := &o.Issues[i]
		if it.AssignedTo(o.Viewer) {
			assigned = append(assigned, it)
		} else {
			issues = append(issues, it)
		}
	}
	issuesNone := locales.GhNoIssues()
	if !o.IssuesOn {
		issuesNone = locales.GhIssuesOff()
	}
	return []ghSection{
		{id: "branch", title: locales.GhThisBranch(), items: this},
		{id: "review", title: locales.GhNeedsReview(), items: review},
		{id: "mine", title: locales.GhYourPRs(), items: mine},
		{id: "assigned", title: locales.GhAssigned(), items: assigned},
		{id: "issues", title: locales.GhIssues(), items: issues, always: true, none: issuesNone,
			more: o.OpenIssues - len(o.Issues), link: o.URL + "/issues"},
		{id: "prs", title: locales.GhPRs(), items: prs, always: true, none: locales.GhNoPRs(),
			more: o.OpenPRs - len(o.PRs), link: o.URL + "/pulls"},
	}
}

// ghWords are the words of an item the query looks in, lowercased: its
// number, title, author, branch, labels and assignees.
func ghWords(it *git.GHItem) string {
	var b strings.Builder
	b.WriteString("#" + strconv.Itoa(it.Number) + " " + it.Title + " " + it.Author + " " + it.HeadRef)
	for _, l := range it.Labels {
		b.WriteString(" " + l.Name)
	}
	for _, as := range it.Assignees {
		b.WriteString(" " + as)
	}
	return strings.ToLower(b.String())
}

// ghMatch reports whether an item's words, s, have every word of the query.
func ghMatch(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, strings.TrimPrefix(w, "#")) {
			return false
		}
	}
	return true
}

// githubRows are the rows of the GitHub view, of the items that match
// the query, the sections folded showing only their headers. They are
// built again only as what they are of changes, not each frame.
func (a *app) githubRows(ctx *gitContext, o *git.GHOverview, now time.Time) []ghRow {
	gp := &a.github
	key := ghRowsKey{o: o, branch: ctx.branch(), query: gp.query}
	for i, id := range ghSectionIDs {
		if gp.collapsed[id] {
			key.folded |= 1 << i
		}
	}
	if gp.built && key == gp.rowsOf {
		return gp.rows
	}
	if gp.wordsOf != o {
		gp.words, gp.wordsOf = nil, o
	}
	words := strings.Fields(strings.ToLower(strings.TrimSpace(gp.query)))
	if len(words) > 0 && gp.words == nil {
		gp.words = make(map[*git.GHItem]string, len(o.PRs)+len(o.Issues))
		for i := range o.PRs {
			gp.words[&o.PRs[i]] = ghWords(&o.PRs[i])
		}
		for i := range o.Issues {
			gp.words[&o.Issues[i]] = ghWords(&o.Issues[i])
		}
	}
	var rows []ghRow
	for _, s := range ctx.githubSections(o) {
		var shown []*git.GHItem
		for _, it := range s.items {
			if len(words) == 0 || ghMatch(gp.words[it], words) {
				shown = append(shown, it)
			}
		}
		if len(shown) == 0 && (!s.always || len(words) > 0) {
			continue
		}
		rows = append(rows, ghRow{key: "head:" + s.id, section: s.id, header: true, first: len(rows) == 0, title: s.title, count: len(shown)})
		if gp.collapsed[s.id] {
			continue
		}
		for _, it := range shown {
			n := strconv.Itoa(it.Number)
			r := ghRow{key: s.id + ":" + n, section: s.id, item: it, number: "#" + n}
			if it.Author != "" && it.Author != o.Viewer {
				r.by = " · " + it.Author
			}
			rows = append(rows, r)
		}
		switch {
		case len(shown) == 0:
			rows = append(rows, ghRow{key: s.id + ":none", section: s.id, note: s.none})
		case s.more > 0 && len(words) == 0:
			rows = append(rows, ghRow{key: s.id + ":more", section: s.id, note: locales.GhMoreOnGitHub(s.more), link: s.link})
		}
	}
	// The rows that come fade in: those of a section opened, or an item
	// a refresh brought; those of another query or repository show at once.
	fresh := !gp.built || key.query != gp.rowsOf.query || gp.rowsOf.o == nil || o.Repo != gp.rowsOf.o.Repo
	gp.appear.note(len(rows), func(i int) string { return rows[i].key }, now, fresh)
	gp.rows, gp.rowsOf, gp.built = rows, key, true
	return rows
}

// ghFade has a row of the GitHub view fade in as it comes.
func (a *app) ghFade(c *ui.Context, row ui.Element, key string, now time.Time) {
	if k := a.github.appear.shown(key, now); k < 1 {
		row.Opacity(k)
		c.AnimationFrame()
	}
}

// githubView is the GitHub view: the issues and pull requests open of the
// repository, in sections, with a query that finds them.
func (a *app) githubView(c *ui.Context) {
	gp := &a.github
	if gp.collapsed == nil {
		gp.collapsed = map[string]bool{}
	}
	ctx := a.repository()
	g := ctx.git
	o := ctx.githubOverview()
	if g.gh.loading {
		ui.Progress(c.Key("gh-activity"), -1).Height(2).Radius(0).Label(locales.GhReading())
	} else {
		ui.Box(c.Key("gh-activity")).Height(2)
	}
	if o == nil {
		if !a.online() {
			a.githubNote(c, iconWifiOff, locales.NotConnectedTo(a.conn.Host), "", nil)
		}
		switch {
		case !ctx.githubKnown() || g.gh.problem == git.GHFine:
			a.githubSkeleton(c)
		default:
			a.githubProblem(c, ctx)
		}
		return
	}
	now := c.Now()
	rows := a.githubRows(ctx, o, now)
	ui.Column(c).Padding(0, 12, 6, 12).Children(func() {
		in := a.searchField(c, "gh-query", &gp.query, locales.GhFind(), &gp.input, nil)
		if in.Changed() {
			gp.row = ghFirstItem(rows)
		}
		if in.Shortcut(0, ui.KeyDown) {
			gp.row = ghStep(rows, gp.row, 1)
			gp.list.ScrollIntoView(gp.row)
		}
		if in.Shortcut(0, ui.KeyUp) {
			gp.row = ghStep(rows, gp.row, -1)
			gp.list.ScrollIntoView(gp.row)
		}
		if in.Submitted() && gp.row >= 0 && gp.row < len(rows) {
			r := rows[gp.row]
			a.post(func() { a.openGitHubRow(ctx, r, true) })
		}
	})
	// What was read stays, with a line at the foot of the view that says
	// why it is not read again: its host is away, or GitHub failed. It
	// floats over the list, which keeps its place as it comes and goes.
	var bar func()
	switch {
	case !a.online():
		bar = func() { a.githubBar(c, iconWifiOff, locales.NotConnectedTo(a.conn.Host), "", nil) }
	case g.gh.problem != git.GHFine:
		bar = func() {
			a.githubBar(c, iconTriangleAlert, locales.GhStale(g.gh.err), locales.GhRetry(), ctx.refreshGitHubNow)
		}
	}
	ui.Column(c.Key("gh-body")).Grow(1).MinHeight(0).Children(func() {
		a.githubList(c, ctx, o, rows, now, bar != nil)
		if bar != nil {
			bar()
		}
	})
}

// githubList is the list of the GitHub view, under its query: its
// sections, items and notes, or that the repository has nothing open, or
// that nothing matches. With room kept at its foot for the line over it.
func (a *app) githubList(c *ui.Context, ctx *gitContext, o *git.GHOverview, rows []ghRow, now time.Time, foot bool) {
	p := a.pal
	gp := &a.github
	if len(o.PRs) == 0 && len(o.Issues) == 0 && o.OpenPRs == 0 && o.OpenIssues == 0 {
		a.githubEmpty(c, ctx, o)
		return
	}
	if len(rows) == 0 {
		ui.Text(c, locales.GhNoMatch()).FontSize(12).TextColor(p.muted).Padding(10, 18, 0, 18)
		return
	}
	if gp.skeleton {
		gp.skeleton, gp.cameAt = false, now
	}
	gp.row = min(max(gp.row, 0), len(rows)-1)
	if rows[gp.row].header {
		gp.row = ghStep(rows, gp.row, 1)
	}
	gp.list.Selected = &gp.row
	gp.list.Header = func(i int) bool { return i < len(rows) && rows[i].header }
	gp.list.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		return rows[i].key
	}
	gp.list.Label = func(i int) string {
		switch r := rows[i]; {
		case r.item != nil:
			return "#" + strconv.Itoa(r.item.Number) + " " + r.item.Title
		case r.header:
			return r.title
		}
		return rows[i].note
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var action func()
	out := ui.List(c.Key("gh-list"), &gp.list, len(rows), func(i int) {
		switch r := rows[i]; {
		case r.header:
			a.githubHeader(c, r, now, &action)
		case r.item != nil:
			a.githubItem(c, ctx, o, r, i == gp.row, now, &action)
		default:
			a.githubNoteRow(c, r, now, &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6)
	if foot {
		out.Padding(0, 6, 8+ghBarRoom, 6)
	}
	c.SetTheme(base)
	// The rows fade in over where the shapes of them were.
	if since := now.Sub(gp.cameAt); since < motionShow {
		out.Opacity(ui.EaseOut(float32(since) / float32(motionShow)))
		c.AnimationFrame()
	}
	out.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputScroll {
			gp.scrolledAt = ev.Time
		}
		if ev.Kind == ui.InputCommand && ev.Text == "copy" && gp.row >= 0 && gp.row < len(rows) && rows[gp.row].item != nil {
			a.copyGitHubLink(rows[gp.row].item)
			return true
		}
		return false
	})
	if out.Submitted() && gp.row >= 0 && gp.row < len(rows) {
		r := rows[gp.row]
		action = func() { a.openGitHubRow(ctx, r, true) }
	}
	if action != nil {
		action()
	}
}

// ghFirstItem is the first row of an item, 0 for none.
func ghFirstItem(rows []ghRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}
	return 0
}

// ghStep is the row by rows after row i, past the headers, or i when
// there is none.
func ghStep(rows []ghRow, i, by int) int {
	for k := i + by; k >= 0 && k < len(rows); k += by {
		if !rows[k].header {
			return k
		}
	}
	return min(max(i, 0), max(len(rows)-1, 0))
}

// ghHeaderGap is the room above the header of a section but the first,
// which sets the sections apart.
const ghHeaderGap = 8

// githubHeader is the header of a section, whose click folds it. Under
// the pointer its words darken, and nothing moves.
func (a *app) githubHeader(c *ui.Context, r ghRow, now time.Time, action *func()) {
	p := a.pal
	gp := &a.github
	h, top := scmRowHeight, float32(0)
	if !r.first {
		h, top = scmRowHeight+ghHeaderGap, ghHeaderGap
	}
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(h).Padding(top, 6, 0, 2).Background(p.chrome)
	a.ghFade(c, row, r.key, now)
	clicked := row.Clicked()
	col := p.muted
	if row.Hovered() {
		col = p.text
	}
	row.Children(func() {
		chevron(c, !gp.collapsed[r.section]).FontSize(12).TextColor(col).Transition(hoverFade)
		size, spacing := p.titleLook(11, 0.4)
		ui.Text(c, p.title(r.title)).FontSize(size).FontWeight(600).LetterSpacing(spacing).
			TextColor(col).Transition(hoverFade).SingleLine().Shrink(1)
		ui.Spacer(c)
		ui.Text(c, strconv.Itoa(r.count)).FontSize(10.5).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.hover).
			FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
	})
	if clicked && *action == nil {
		*action = func() { gp.collapsed[r.section] = !gp.collapsed[r.section] }
	}
}

// githubNoteRow is a note of a section: that it has none, or how many
// more GitHub has, which a click shows there.
func (a *app) githubNoteRow(c *ui.Context, r ghRow, now time.Time, action *func()) {
	p := a.pal
	if r.link == "" {
		a.ghFade(c, ui.Text(c, r.note).FontSize(12).TextColor(p.faint).Padding(4, 10, 6, ghTitleIndent), r.key, now)
		return
	}
	// Under the titles, as a row of the list would be; its arrow says it
	// opens GitHub.
	b := ui.ButtonBase(c.Key("note")).FillWidth().Justify(ui.Start).Padding(4, 10, 6, ghTitleIndent).Radius(5).Label(r.note)
	a.ghFade(c, b, r.key, now)
	col := p.muted
	if b.Hovered() {
		col = p.accent
	}
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, r.note).FontSize(12).TextColor(col).Transition(hoverFade).SingleLine()
			ui.Icon(c, iconExternal).FontSize(10).TextColor(col).Transition(hoverFade)
		})
	})
	if b.Clicked() {
		*action = func() { a.services.OpenURL(r.link) }
	}
}

// ghItemH is how tall the row of an issue or a pull request is: its title
// over a line of its number, author, age and labels. ghTitleIndent is
// where its title starts, past its icon, and ghChipH how tall a label is.
const (
	ghItemH       = 40
	ghTitleIndent = 25
	ghChipH       = 15
)

// githubItem is the row of an issue or a pull request: its state's icon,
// its title, and under it its number, author and age, its labels, and
// its comments; a pull request's checks and review at the end. A click
// opens it in a tab, a pull request's changes; its menu has the rest.
// Under the pointer its actions show at its end, and its card beside it.
func (a *app) githubItem(c *ui.Context, ctx *gitContext, o *git.GHOverview, r ghRow, chosen bool, now time.Time, action *func()) {
	p := a.pal
	it := r.item
	row := ui.Column(c).Grow(1).Height(ghItemH).Padding(3, 8, 3, 6).Gap(2).Justify(ui.Center).Radius(6)
	a.ghFade(c, row, r.key, now)
	hovered := row.Hovered()
	if hovered {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	if wait := scmScrollQuiet - now.Sub(a.github.scrolledAt); wait > 0 {
		c.After(wait)
	} else {
		a.githubItemCard(c, row, r, now)
	}
	icon, col := ghItemIcon(p, it)
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0)
			ui.Text(c, it.Title).FontSize(13).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if ic, cc := prChecksIcon(p, it.Checks); it.PR && ic != nil {
				ui.Icon(c, ic).FontSize(11).TextColor(cc).Shrink(0)
			}
		})
		ui.Row(c).AlignItems(ui.Center).Padding(0, 0, 0, ghTitleIndent-6).Children(func() {
			// The labels give their room up first, then the author, who
			// shortens; the number and the age stay. The viewer's own
			// items name no author.
			ui.Text(c, r.number).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0).FontFeatures("tnum")
			if r.by != "" {
				ui.Text(c, r.by).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0.01).MinWidth(0)
			}
			if !it.Updated.IsZero() {
				ui.Text(c, " · "+shortAgo(it.Updated, now)).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0).
					FontFeatures("tnum")
			}
			if review, col := a.ghReview(it); review != "" {
				ui.Text(c, review).FontSize(11).FontWeight(500).TextColor(col).SingleLine().Shrink(0).Margin(0, 0, 0, 6)
			}
			// A label that does not fit goes whole, to a line that does
			// not show, rather than being cut: the first too, after the
			// empty box that holds the line. The card shows them all.
			if len(it.Labels) > 0 {
				ui.Row(c).Wrap().Gap(4).Height(ghChipH).Shrink(1).MinWidth(0).Margin(0, 0, 0, 2).Clip().Children(func() {
					ui.Box(c).Size(0, ghChipH)
					for k, l := range it.Labels {
						if k == 3 {
							break
						}
						a.ghLabel(c, l, 10.5)
					}
				})
			}
			ui.Spacer(c)
			if it.Comments > 0 {
				ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 6).Children(func() {
					ui.Icon(c, iconComment).FontSize(10).TextColor(p.faint)
					ui.Text(c, strconv.Itoa(it.Comments)).FontSize(11).TextColor(p.faint).FontFeatures("tnum")
				})
			}
		})
		if hovered {
			a.githubItemActions(c, ctx, o, it, chosen, action)
		}
	})
	row.ContextMenu(func(m *ui.Menu) { a.githubItemMenu(m, ctx, o, it, action) })
	if row.Clicked() && *action == nil {
		*action = func() { a.openGitHubItem(ctx, it, false) }
	}
}

// ghItemIcon is the icon of an issue or a pull request, in the color of
// its state.
func ghItemIcon(p *palette, it *git.GHItem) (*ui.SVG, ui.Color) {
	switch {
	case !it.PR:
		return iconIssue, p.addedFg
	case it.Draft:
		return iconPullRequest, p.muted
	}
	return iconPullRequest, p.addedFg
}

// ghActionW is how wide a button of a row's actions is, and ghActionFade
// how far the row's text fades out before them.
const (
	ghActionW    = 22
	ghActionFade = 18
)

// githubItemActions are the buttons at the end of an item's row under the
// pointer: a pull request's Check Out, an issue's Work on Issue, then Open
// on GitHub and Copy Link. They float over the row's end, which fades out
// under them, so that nothing of the row moves as they come.
func (a *app) githubItemActions(c *ui.Context, ctx *gitContext, o *git.GHOverview, it *git.GHItem, chosen bool, action *func()) {
	p := a.pal
	type act struct {
		icon  *ui.SVG
		label string
		off   bool
		do    func()
	}
	n := it.Number
	var acts []act
	switch {
	case it.PR && it.HeadRef != ctx.branch():
		acts = append(acts, act{iconCheckout, locales.GhCheckOut(), !ctx.git.canMutate(), func() { ctx.checkoutPR(n) }})
	case !it.PR:
		acts = append(acts, act{iconAgent, locales.GhWorkOnIssue(), !ctx.git.canMutate() || o.DefaultBranch == "", func() { a.workOnIssue(ctx, n) }})
	}
	if it.URL != "" {
		acts = append(acts,
			act{iconExternal, locales.PrOpenOnGitHub(), false, func() { a.services.OpenURL(it.URL) }},
			act{iconCopy, locales.GhCopyLink(), false, func() { a.copyGitHubLink(it) }})
	}
	if len(acts) == 0 {
		return
	}
	// What the row is drawn over, solid, for the text under the buttons to
	// fade into: the list's mark of the row chosen, or of the row under the
	// pointer, then the row's own.
	bg := p.chrome
	if chosen {
		bg = p.active.Over(bg)
	} else {
		bg = p.hover.Over(bg)
	}
	bg = p.hover.Over(bg)
	w := float32(len(acts)*ghActionW + ghActionFade + 4)
	strip := ui.Row(c.Key("gh-actions")).Absolute().Top(0).Bottom(0).Right(0).Width(w).Padding(0, 4, 0, ghActionFade).
		AlignItems(ui.Center).Justify(ui.End).Radius(6).
		LinearGradient(ui.LinearGradient{From: bg.Alpha(0), To: bg, Angle: 90, End: ghActionFade / w}).Transition(revealFade)
	strip.Children(func() {
		for _, ac := range acts {
			b := ui.ButtonBase(c.Key(ac.label)).Size(ghActionW, ghActionW).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
				Label(ac.label).Tooltip(ac.label).Disabled(ac.off)
			col := p.muted
			switch {
			case ac.off:
				col = p.faint
			case b.Hovered():
				b.Background(p.hover)
				col = p.text
			}
			b.Transition(hoverFade)
			pressPose(b)
			b.Children(func() { ui.Icon(c, ac.icon).FontSize(13).TextColor(col).Transition(hoverFade) })
			if b.Clicked() && !ac.off {
				*action = ac.do
			}
		}
	})
}

// ghCardW is how wide the card of an issue or a pull request is, as wide
// as a commit's.
const ghCardW = commitCardW

// githubItemCard is the card that shows as the pointer rests on the row of
// an issue or a pull request, beside the sidebar: its whole title and all
// its labels, then who made it and who it is assigned to, a pull
// request's branch, size, checks and review, and when it was updated. It
// shows only what the view read already.
func (a *app) githubItemCard(c *ui.Context, row ui.Element, r ghRow, now time.Time) {
	p := a.pal
	it := r.item
	ui.TooltipBase(c, row, func(tip ui.Element) {
		if a.settings.SidebarRight {
			tip.AttachTo(row, ui.AnchorLeft, ui.AnchorRight).Margin(0, 8, 0, 0)
		} else {
			tip.AttachTo(row, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8)
		}
		fadeIn(a.floating(c, tip, false)).Radius(10).Width(ghCardW)
		tip.Children(func() {
			ui.Column(c).Gap(6).Padding(12, 14, 10, 14).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					icon, col := ghItemIcon(p, it)
					ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0)
					ui.Text(c, r.number).FontSize(11.5).TextColor(p.muted).FontFeatures("tnum").SingleLine()
					if it.Draft {
						ui.Text(c, locales.PrDraft()).FontSize(11).TextColor(p.muted).SingleLine()
					}
				})
				ui.Text(c, it.Title).FontSize(13).FontWeight(600).TextColor(p.text).LineHeight(1.35).MaxLines(4)
				if len(it.Labels) > 0 {
					ui.Row(c).Wrap().Gap(4).Margin(2, 0, 0, 0).Children(func() {
						for _, l := range it.Labels {
							a.ghLabel(c, l, 10.5)
						}
					})
				}
			})
			ui.Box(c).Height(1).Background(p.border).Shrink(0)
			ui.Column(c).Gap(6).Padding(10, 14, 12, 14).Children(func() {
				field := func(name string, value func()) {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Height(18).Children(func() {
						ui.Text(c, name).FontSize(11.5).TextColor(p.muted).Shrink(0)
						ui.Spacer(c)
						ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(value)
					})
				}
				text := func(s string, col ui.Color) {
					ui.Text(c, s).FontSize(11.5).TextColor(col).FontFeatures("tnum").SingleLine().Shrink(1).MinWidth(0)
				}
				if it.Author != "" {
					field(locales.GhCardAuthor(), func() { text(it.Author, p.text) })
				}
				if len(it.Assignees) > 0 {
					field(locales.GhCardAssignees(), func() { text(strings.Join(it.Assignees, ", "), p.text) })
				}
				if it.PR {
					if it.HeadRef != "" {
						field(locales.ScmCardBranch(), func() {
							ui.Text(c, it.HeadRef).Font(a.font.family).FontSize(11.5).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
							if it.BaseRef != "" {
								ui.Icon(c, iconArrowRight).FontSize(10).TextColor(p.faint).Shrink(0)
								ui.Text(c, it.BaseRef).Font(a.font.family).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(0)
							}
						})
					}
					if it.Files > 0 {
						field(locales.ScmCardChanged(), func() {
							text(locales.ScmCardFiles(it.Files), p.text)
							text("+"+strconv.Itoa(it.Added), p.addedFg)
							text("−"+strconv.Itoa(it.Deleted), p.removedFg)
						})
					}
					if it.Checks != git.ChecksNone {
						field(locales.GhCardChecks(), func() { a.ghCheckCounts(c, it) })
					}
					if review := prReviewLabel(it.Review); review != "" {
						_, col := a.ghReview(it)
						if it.Draft || it.Review == "REVIEW_REQUIRED" {
							col = p.text
						}
						field(locales.GhCardReview(), func() { text(review, col) })
					}
				}
				if it.Comments > 0 {
					field(locales.GhCardComments(), func() { text(strconv.Itoa(it.Comments), p.text) })
				}
				if !it.Updated.IsZero() {
					field(locales.GhCardUpdated(), func() { text(ago(it.Updated, now), p.text) })
				}
			})
		})
	})
}

// ghCheckCounts is how the checks of a pull request went, as the card
// shows them: how many passed, failed and run, each by its icon; or its
// icon alone, for checks GitHub did not count.
func (a *app) ghCheckCounts(c *ui.Context, it *git.GHItem) {
	p := a.pal
	st := it.CheckCounts
	if st.Passed+st.Failed+st.Pending == 0 {
		if ic, col := prChecksIcon(p, it.Checks); ic != nil {
			ui.Icon(c, ic).FontSize(11).TextColor(col)
		}
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Description(checksSummary(st)).Children(func() {
		for _, n := range []struct {
			count int
			state git.Checks
		}{{st.Passed, git.ChecksPass}, {st.Failed, git.ChecksFail}, {st.Pending, git.ChecksPending}} {
			if n.count == 0 {
				continue
			}
			ic, col := prChecksIcon(p, n.state)
			ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, ic).FontSize(11).TextColor(col)
				ui.Text(c, strconv.Itoa(n.count)).FontSize(11.5).TextColor(p.text).FontFeatures("tnum")
			})
		}
	})
}

// ghLabel is a label, on a chip of its color.
func (a *app) ghLabel(c *ui.Context, l git.GHLabel, size float32) {
	p := a.pal
	col := p.muted
	if len(l.Color) == 6 {
		col = ui.Hex("#" + l.Color)
	}
	// The label's own color as its edge and its tint, its name in text's
	// color, which reads on every theme.
	ui.Text(c, l.Name).FontSize(size).TextColor(p.text).SingleLine().Shrink(0).Height(ghChipH).Padding(0, 5).Radius(ghChipH/2).
		Background(col.Alpha(0.18)).Border(1, col.Alpha(0.5))
}

// ghReview is the word of a pull request's state under its title, and its
// color: a draft, or a review that approved or asked for changes. A review
// that is only required says nothing, as most do.
func (a *app) ghReview(it *git.GHItem) (string, ui.Color) {
	p := a.pal
	switch {
	case !it.PR:
		return "", p.muted
	case it.Draft:
		return locales.PrDraft(), p.muted
	case it.Review == "APPROVED":
		return locales.PrApproved(), p.addedFg
	case it.Review == "CHANGES_REQUESTED":
		return locales.PrChangesRequested(), p.dangerFg
	}
	return "", p.muted
}

// githubItemMenu is the menu of an issue or a pull request of the view.
func (a *app) githubItemMenu(m *ui.Menu, ctx *gitContext, o *git.GHOverview, it *git.GHItem, action *func()) {
	n := it.Number
	do := func(fn func()) { *action = fn }
	if it.PR {
		if m.Item(locales.PrOpenChanges()).Chosen() {
			do(func() { ctx.openPullRequest(n) })
		}
		if m.Item(locales.GhOpenConversation()).Chosen() {
			do(func() { a.openIssue(ctx, n, false) })
		}
		if m.Item(locales.PrShowChecks()).Disabled(it.Checks == git.ChecksNone).Chosen() {
			do(func() { ctx.openChecksOf(n) })
		}
		m.Separator()
		if m.Item(locales.GhCheckOut()).Disabled(!ctx.git.canMutate()).Chosen() {
			do(func() { ctx.checkoutPR(n) })
		}
		if m.Item(locales.GhAddressReview()).Chosen() {
			do(func() { a.openPRWithThreads(ctx, n) })
		}
	} else {
		if m.Item(locales.GhOpenIssue()).Chosen() {
			do(func() { a.openIssue(ctx, n, false) })
		}
		m.Separator()
		if m.Item(locales.GhWorkOnIssue()).Disabled(!ctx.git.canMutate() || o.DefaultBranch == "").Chosen() {
			do(func() { a.workOnIssue(ctx, n) })
		}
		if m.Item(locales.GhSendIssue()).Chosen() {
			do(func() { a.sendIssue(ctx, n) })
		}
	}
	m.Separator()
	if m.Item(locales.PrOpenOnGitHub()).Disabled(it.URL == "").Chosen() {
		do(func() { a.services.OpenURL(it.URL) })
	}
	if m.Item(locales.GhCopyLink()).Disabled(it.URL == "").Chosen() {
		do(func() { a.copyGitHubLink(it) })
	}
}

// openGitHubRow opens what a row of the view is: its item, or a note's
// link.
func (a *app) openGitHubRow(ctx *gitContext, r ghRow, keep bool) {
	switch {
	case r.item != nil:
		a.openGitHubItem(ctx, r.item, keep)
	case r.link != "":
		a.services.OpenURL(r.link)
	case r.header:
		a.github.collapsed[r.section] = !a.github.collapsed[r.section]
	}
}

// openGitHubItem opens an issue in a tab, or a pull request's changes.
func (a *app) openGitHubItem(ctx *gitContext, it *git.GHItem, keep bool) {
	if it.PR {
		ctx.openPullRequest(it.Number)
		return
	}
	a.openIssue(ctx, it.Number, keep)
}

// copyGitHubLink puts the link of an issue or a pull request on the
// clipboard.
func (a *app) copyGitHubLink(it *git.GHItem) {
	if it.URL == "" {
		return
	}
	a.services.WriteClipboard(it.URL)
	a.tell(note{message: locales.GhLinkCopied("#" + strconv.Itoa(it.Number))})
}

// openChecksOf shows the box of the checks of pull request n.
func (a *gitContext) openChecksOf(n int) {
	if !a.live() {
		return
	}
	if !a.app.online() {
		a.app.fail("%s", locales.NotConnectedTo(a.app.conn.Host))
		return
	}
	a.app.checks = checksPicker{open: true, repoRoot: a.git.repo.Root, number: n}
	a.readChecks()
}

// checkoutPR checks the branch of pull request n out, as gh pr checkout
// does.
func (a *gitContext) checkoutPR(n int) {
	a.gitDo(locales.BusyCheckingOutPR("#"+strconv.Itoa(n)), func(r *git.Repo) error { return r.CheckoutPR(n) }, func() {
		a.app.tell(note{message: locales.GhCheckedOut("#" + strconv.Itoa(n))})
	})
}

// githubTitleActions are the buttons of the GitHub view's title, while the
// pointer is over the sidebar: a new issue, the repository on GitHub, and
// a refresh.
func (a *app) githubTitleActions(c *ui.Context, hovered bool) {
	if !hovered {
		return
	}
	ctx := a.repository()
	o := ctx.githubOverview()
	if a.iconButton(c, iconPlus, locales.GhNewIssue(), false).Disabled(o == nil || !o.IssuesOn).Clicked() {
		a.post(func() { a.newIssue(ctx) })
	}
	if a.iconButton(c, iconExternal, locales.GhOpenRepo(), false).Disabled(o == nil).Clicked() {
		a.services.OpenURL(o.URL)
	}
	if a.iconButton(c, iconRefresh, locales.GhRefresh(), false).Clicked() {
		ctx.refreshGitHubNow()
	}
}

// githubSkeleton is the shapes of the view, while it reads its rows the
// first time: its query's field, a section's header and its rows, each
// where the real one goes, so that nothing moves as they come.
func (a *app) githubSkeleton(c *ui.Context) {
	p := a.pal
	a.github.skeleton = true
	bar := p.text.Alpha(0.07)
	ui.Column(c.Key("gh-skeleton")).Grow(1).Label(locales.GhReading()).Children(func() {
		ui.Box(c).Height(26).Margin(0, 12, 6, 12).Radius(5).Background(p.editor).Border(1, p.border)
		ui.Column(c).Padding(0, 6, 8, 6).Children(func() {
			ui.Row(c).Height(scmRowHeight).Padding(0, 0, 0, 16).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(84, 7).Radius(3.5).Background(bar)
			})
			for i, w := range []float32{0.8, 0.62, 0.88, 0.55, 0.72} {
				ui.Column(c.Key(i)).Height(ghItemH).Padding(3, 8, 3, 6).Gap(7).Justify(ui.Center).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(13, 13).Radius(6.5).Background(bar)
						ui.Box(c).WidthPercent(w * 90).Height(8).Radius(4).Background(bar)
					})
					ui.Box(c).Size(90, 6).Radius(3).Background(bar).Margin(0, 0, 0, ghTitleIndent-6)
				})
			}
		})
	})
}

// githubNote is a line over the view: that its host is away, or that
// GitHub could not be read again, with a button that tries again.
func (a *app) githubNote(c *ui.Context, icon *ui.SVG, text, button string, fn func()) {
	p := a.pal
	ui.Column(c.Key("gh-note")).Margin(4, 12, 6, 12).Padding(6, 8).Gap(6).Radius(6).Background(p.hover).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(p.modifiedFg).Shrink(0).Margin(1, 0, 0, 0)
			ui.Text(c, text).FontSize(12).TextColor(p.muted).LineHeight(1.4).Grow(1).Shrink(1).MinWidth(0).Selectable()
		})
		if button != "" && ui.Button(c.Key("gh-note-button"), button).AlignSelf(ui.End).Clicked() {
			fn()
		}
	})
}

// ghBarRoom is the room the list keeps at its foot for githubBar, so
// that its last rows scroll out from under it.
const ghBarRoom = 60

// githubBar is the line at the foot of the view, over its list: that its
// host is away, or that GitHub could not be read again, with a button that
// tries again, by its icon so that the words of why keep the room. It rises in as it comes, and the list stays where it is.
func (a *app) githubBar(c *ui.Context, icon *ui.SVG, text, button string, fn func()) {
	p := a.pal
	bar := ui.Row(c.Key("gh-bar")).Absolute().Left(8).Right(8).Bottom(8).Padding(6, 6, 6, 10).Gap(8).AlignItems(ui.Center).
		Radius(8).Transition(barIn)
	a.floating(c, bar, false)
	bar.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(p.modifiedFg).Shrink(0)
		ui.Text(c, text).FontSize(12).TextColor(p.muted).LineHeight(1.35).MaxLines(3).Tooltip(text).
			Grow(1).Shrink(1).MinWidth(0)
		if button != "" && a.smallButton(c, iconRefresh, button) {
			fn()
		}
	})
}

// githubCard is a state of the view in its middle: an icon, what it is,
// why, and what to do.
func (a *app) githubCard(c *ui.Context, key string, icon *ui.SVG, title, detail string, buttons func()) {
	p := a.pal
	ui.Column(c.Key(key)).Padding(28, 20, 8, 20).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, icon).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.text).TextAlign(ui.Center).Margin(4, 0, 0, 0)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45).Selectable()
		}
		ui.Box(c).Height(4)
		buttons()
	})
}

// githubProblem is why the view has nothing to show: gh is not there, not
// signed in, does not know which repository, or failed; each with what
// to do next.
func (a *app) githubProblem(c *ui.Context, ctx *gitContext) {
	g := ctx.git
	host := ""
	if a.conn != nil {
		host = a.conn.Host
	}
	switch g.gh.problem {
	case git.GHMissing:
		detail := locales.GhMissingDetail()
		if host != "" {
			detail = locales.GhMissingOn(host)
		}
		a.githubCard(c, "gh-missing", iconTerminal, locales.GhMissing(), detail, func() {
			if ui.PrimaryButton(c, locales.GhGetCLI()).FillWidth().Clicked() {
				a.services.OpenURL(ghGetCLI)
			}
			if ui.Button(c.Key("gh-retry"), locales.GhRetry()).FillWidth().Clicked() {
				ctx.refreshGitHubNow()
			}
		})
	case git.GHSignedOut:
		a.githubCard(c, "gh-signed-out", iconLock, locales.GhSignedOut(), locales.GhSignedOutDetail(), func() {
			if ui.PrimaryButton(c, locales.GhSignIn()).FillWidth().Clicked() {
				a.post(func() { a.runInTerminal(ctx.git.repo.Root, "gh auth login") })
			}
			if ui.Button(c.Key("gh-retry"), locales.GhRetry()).FillWidth().Clicked() {
				ctx.refreshGitHubNow()
			}
		})
	case git.GHNoDefault:
		a.githubCard(c, "gh-no-default", iconGitHub, locales.GhNoDefault(), locales.GhNoDefaultDetail(), func() {
			seen := map[string]bool{}
			for _, rm := range g.gh.remotes {
				if seen[rm.Path] {
					continue
				}
				seen[rm.Path] = true
				path := rm.Path
				if ui.Button(c.Key("gh-default:"+path), path).FillWidth().Clicked() {
					a.post(func() { ctx.setDefaultRepo(path) })
				}
			}
		})
	default:
		a.githubCard(c, "gh-error", iconTriangleAlert, locales.GhFailed(), g.gh.err, func() {
			if ui.PrimaryButton(c, locales.GhRetry()).FillWidth().Clicked() {
				ctx.refreshGitHubNow()
			}
			if ui.Button(c.Key("gh-report"), locales.ReportProblem()).FillWidth().Clicked() {
				a.reportProblem(locales.GhFailed(), g.gh.err)
			}
		})
	}
}

// githubEmpty is the view of a repository with no issue or pull request
// open.
func (a *app) githubEmpty(c *ui.Context, ctx *gitContext, o *git.GHOverview) {
	a.githubCard(c, "gh-empty", iconCircleCheck, locales.GhNothingOpen(), o.Repo, func() {
		if o.IssuesOn && ui.PrimaryButton(c, locales.GhNewIssue()).FillWidth().Clicked() {
			a.post(func() { a.newIssue(ctx) })
		}
	})
}

// setDefaultRepo tells gh which repository, owner/name, the work tree is
// of, and reads it.
func (a *gitContext) setDefaultRepo(path string) {
	a.gitRead(locales.GhSettingDefault(path), func(r *git.Repo) error { return r.SetDefaultRepo(path) }, func() {
		a.git.gh.problem = git.GHFine
		a.refreshGitHubNow()
	})
}

// runInTerminal runs a command in a new terminal in dir, where terminals
// open.
func (a *app) runInTerminal(dir, command string) {
	s := a.startTerminal(dir)
	if s == nil {
		return
	}
	a.placeTerminal(s)
	s.term.Send([]byte(command + "\r"))
}

// placeTerminal shows a terminal started where terminalLocation says: in
// the panel, or in a tab of the file column.
func (a *app) placeTerminal(s *session) {
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

// ghSortedLabels is the names of labels, as they read in a prompt.
func ghSortedLabels(ls []git.GHLabel) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	slices.Sort(out)
	return out
}
