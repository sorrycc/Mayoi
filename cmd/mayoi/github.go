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
// while checks of a pull request listed run, or once GitHub did not answer
// in time; seldom while it is hidden, for the count on its switch; and
// seldom once gh could not tell.
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
	// problem and err why it could not tell last, poll asks again, gen
	// counts the refreshes asked for and answered those GitHub answered.
	// loading is set while it is asked; loadingAt is when the view first
	// showed it so, zero until it did.
	overview   *git.GHOverview
	overviewOf *git.Repo
	problem    git.GHProblem
	err        string
	poll       bg.Poller[ghAsked]
	gen        int
	answered   int
	loading    bool
	loadingAt  time.Time
	// failedAgain is when a refresh asked for, as the view showed GitHub
	// had failed, failed again, for its icon to shake its head: the words
	// may say what they said, and the click still answers.
	failedAgain time.Time
}

// refreshing reports whether a refresh the person asked for, by Refresh or
// Retry, is yet to be answered: the view says so, where it keeps quiet as
// it asks again on its own.
func (g *githubState) refreshing() bool { return g.answered < g.gen }

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
	// made are the rows last built, of rowsOf: githubRows builds them
	// again only as the overview, the branch, the query or the sections
	// folded change. rows are those shown, with those that go among them
	// as they close their room, the first of them gone at goneEnd. words
	// are each item's, lowercased, for the query, as of wordsOf.
	made    []ghRow
	rows    []ghRow
	goneEnd time.Time
	rowsOf  ghRowsKey
	built   bool
	words   map[*git.GHItem]string
	wordsOf *git.GHOverview
	// appear opens the rows that come, as a section opens or a refresh
	// brings an item.
	appear appearing
	// rowKey is the key of the row chosen, for the choice to stay on it
	// as a refresh brings rows above it or takes them.
	rowKey string
	// scrolledAt is when the list last scrolled: its rows show no card
	// until it has rested for scmScrollQuiet.
	scrolledAt time.Time
	// glide is the choice's background on its way from the row it was on,
	// as in Sessions; hopAt is when it last went over a section's header,
	// which it does not glide over, and hopBy which way, 1 down, -1 up.
	glide choiceGlide
	hopAt time.Time
	hopBy float32
	// card is the key of the row whose card Space showed, until the choice
	// moves, or Space or Escape hides it.
	card string
	// focusList is set for the list to take the keyboard as it builds.
	focusList bool
	// checks are the pull requests whose checks a refresh changed, by
	// number, for their icons to fade from how they were to how they are.
	checks map[int]ghChecksWere
	// firstKey is the key of the first header, which has no room above it.
	// As another header becomes first, the room above gapKey glides from
	// gapAt: shrinks, as the section above it goes, or grows, as one comes.
	firstKey string
	gapKey   string
	gapAt    time.Time
	gapGone  bool
}

// ghChecksWere is how the checks of a pull request were before a refresh
// changed them, at at.
type ghChecksWere struct {
	from git.Checks
	at   time.Time
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
	g.gh.loading, g.gh.loadingAt = true, time.Time{}
	bg.Go(func() {
		defer recoverCrash()
		o, err := fetchGitHubOverview(r)
		a.app.post(func() {
			if !g.gh.poll.Done(key) || g.repo != r {
				return
			}
			asked := key.gen > g.gh.answered
			g.gh.loading, g.gh.answered = false, max(g.gh.answered, key.gen)
			switch {
			case err == nil:
				// An answer that tells what the last did keeps it, so that
				// nothing of the view is built again.
				same := g.gh.overviewOf == r && o.Same(g.gh.overview)
				if same {
					o = g.gh.overview
				}
				// The history marks the heads of the pull requests listed.
				if !same || g.prHeadsRepo != r || g.prHeadsOff {
					g.prHeads, g.prHeadsRepo, g.prHeadsOff = o.PRHeads(), r, false
				}
				g.gh.overview, g.gh.overviewOf, g.gh.problem, g.gh.err = o, r, git.GHFine, ""
				g.ghOK = true
			case errors.Is(err, remote.ErrOffline):
				// The host is away: what was read stays until it is back.
			default:
				if asked && g.gh.overviewOf == r && g.gh.problem != git.GHFine {
					g.gh.failedAgain = a.app.gitNow()
				}
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

// ghShownError is why gh failed as the view says it: in plain words when
// GitHub did not answer in time, else as gh said it.
func (g *githubState) ghShownError() string {
	if g.problem == git.GHBusy {
		return locales.GhBusy()
	}
	return g.err
}

// ghEvery is how long the answer of GitHub is good for.
func (a *gitContext) ghEvery() time.Duration {
	g := a.git
	switch {
	case g.gh.problem == git.GHBusy:
		return ghPendingEvery
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
	avatar  string // the link of by's picture
	// again is set for a pull request whose review of the viewer is asked
	// for again; reviewers are the pictures of its reviewers but the
	// viewer, the first ghRowReviewers, moreReviewers how many more, and
	// reviewedBy their names.
	again         bool
	reviewers     []ghFace
	moreReviewers int
	reviewedBy    string
	// checks is set for a pull request's row to keep the room of the icon
	// of its checks, when any pull request listed has checks: one whose
	// checks come then moves nothing.
	checks bool
	note   string
	link   string // a note's, which a click opens
	// gone is when the row went, as a refresh took its item or its
	// section folded, while it closes its room; zero for a row that stays.
	gone time.Time
}

// ghFace is a person of a row: their login and the link of their picture.
type ghFace struct{ login, avatar string }

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
		if !gp.goneEnd.IsZero() && !now.Before(gp.goneEnd) {
			gp.rows, gp.goneEnd = ghLeave(gp.rows, gp.made, now)
			gp.keepChoice()
		}
		return gp.rows
	}
	if old := gp.rowsOf.o; gp.built && old != nil && old != o && old.Repo == o.Repo {
		gp.noteChecks(old, o, now)
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
	anyChecks := slices.ContainsFunc(o.PRs, func(it git.GHItem) bool { return it.Checks != git.ChecksNone })
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
			r := ghRow{key: s.id + ":" + n, section: s.id, item: it, number: "#" + n, checks: it.PR && anyChecks}
			if it.Author != "" && it.Author != o.Viewer {
				r.by, r.avatar = it.Author, o.Avatars[it.Author]
			}
			if it.PR {
				r.again = it.AskedAgain(o.Viewer)
				var names []string
				for _, rv := range it.Reviewers {
					if rv.Login == o.Viewer {
						continue
					}
					names = append(names, rv.Login)
					if len(r.reviewers) == ghRowReviewers {
						r.moreReviewers++
						continue
					}
					r.reviewers = append(r.reviewers, ghFace{rv.Login, o.Avatars[rv.Login]})
				}
				r.reviewedBy = strings.Join(names, ", ")
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
	// The rows that come open their room, and those that go close it:
	// those of a section opened or folded, or an item a refresh brought or
	// took; those of another query or repository show at once.
	fresh := !gp.built || key.query != gp.rowsOf.query || gp.rowsOf.o == nil || o.Repo != gp.rowsOf.o.Repo
	gp.appear.note(len(rows), func(i int) string { return rows[i].key }, now, fresh)
	was := gp.rows
	if fresh {
		was = nil
	}
	// A row that comes back as it goes comes on from where it was.
	for _, r := range was {
		if !r.gone.IsZero() {
			gp.appear.resume(r.key, gp.ghShown(r, now), now)
		}
	}
	gp.noteFirst(gp.made, rows, now, fresh)
	gp.made, gp.rowsOf, gp.built = rows, key, true
	gp.rows, gp.goneEnd = ghLeave(was, rows, now)
	gp.keepChoice()
	return gp.rows
}

// noteFirst notes the first header of rows, which were made before: one
// that was under another becomes first as the section above it goes, and
// gives up its room above as that closes; one that was first, as a section
// comes above it, takes its room as that opens. Fresh rows take theirs at
// once.
func (gp *githubPanel) noteFirst(made, rows []ghRow, now time.Time, fresh bool) {
	first := ""
	if len(rows) > 0 {
		first = rows[0].key
	}
	was := gp.firstKey
	gp.firstKey = first
	if fresh || was == "" || first == "" || first == was {
		return
	}
	had := func(rs []ghRow, key string) bool {
		return slices.ContainsFunc(rs, func(r ghRow) bool { return r.key == key })
	}
	switch {
	case had(made, first):
		gp.gapKey, gp.gapAt, gp.gapGone = first, now, true
	case had(rows, was):
		gp.gapKey, gp.gapAt, gp.gapGone = was, now, false
	}
}

// headerGap is the room above header r at now: none for the first, else
// ghHeaderGap; gliding from one to the other as noteFirst noted, over
// motionLeave as a section goes and motionShow as one comes, while moving.
// With less motion, it is there at once.
func (gp *githubPanel) headerGap(r ghRow, now time.Time, reduce bool) (gap float32, moving bool) {
	gap = ghHeaderGap
	if r.first {
		gap = 0
	}
	if r.key != gp.gapKey || !r.gone.IsZero() || reduce {
		return gap, false
	}
	span := motionShow
	if gp.gapGone {
		span = motionLeave
	}
	since := now.Sub(gp.gapAt)
	if since < 0 || since >= span {
		return gap, false
	}
	k := ui.EaseOut(float32(since) / float32(span))
	if gp.gapGone {
		return ghHeaderGap * (1 - k), true
	}
	return ghHeaderGap * k, true
}

// keepChoice keeps the choice on its row, wherever the rows shown have it
// now; one gone leaves it where it was, for the list to settle it on the
// nearest.
func (gp *githubPanel) keepChoice() {
	if gp.rowKey == "" {
		return
	}
	for i, r := range gp.rows {
		if r.key == gp.rowKey && r.gone.IsZero() {
			gp.row = i
			return
		}
	}
}

// ghLeave is rows with those of was they don't have among them as they
// go: each after the row before it that stays, gone from now unless it
// went before, and left out once gone for motionLeave. end is when the
// first of those among them is, zero for none.
func ghLeave(was, rows []ghRow, now time.Time) (out []ghRow, end time.Time) {
	if len(was) == 0 {
		return rows, time.Time{}
	}
	has := make(map[string]bool, len(rows))
	for _, r := range rows {
		has[r.key] = true
	}
	var after map[string][]ghRow
	n, prev := 0, ""
	for _, r := range was {
		if has[r.key] {
			prev = r.key
			continue
		}
		if r.gone.IsZero() {
			r.gone = now
		}
		until := r.gone.Add(motionLeave)
		if !now.Before(until) {
			continue
		}
		if end.IsZero() || until.Before(end) {
			end = until
		}
		if after == nil {
			after = map[string][]ghRow{}
		}
		after[prev] = append(after[prev], r)
		n++
	}
	if n == 0 {
		return rows, time.Time{}
	}
	out = make([]ghRow, 0, len(rows)+n)
	out = append(out, after[""]...)
	for _, r := range rows {
		out = append(out, r)
		out = append(out, after[r.key]...)
	}
	return out, end
}

// noteChecks notes the pull requests of o whose checks are other than they
// were in old, the overview before it: only a change a refresh brought,
// not a row that comes into view.
func (gp *githubPanel) noteChecks(old, o *git.GHOverview, now time.Time) {
	for n, w := range gp.checks {
		if now.Sub(w.at) >= motionShow {
			delete(gp.checks, n)
		}
	}
	was := make(map[int]git.Checks, len(old.PRs))
	for _, it := range old.PRs {
		was[it.Number] = it.Checks
	}
	for _, it := range o.PRs {
		if from, ok := was[it.Number]; ok && from != it.Checks {
			if gp.checks == nil {
				gp.checks = map[int]ghChecksWere{}
			}
			gp.checks[it.Number] = ghChecksWere{from, now}
		}
	}
}

// ghShown is how far in row r of the GitHub view is at now: from 0 to 1
// as it comes, and back to 0 as it goes.
func (gp *githubPanel) ghShown(r ghRow, now time.Time) float32 {
	if !r.gone.IsZero() {
		return 1 - ui.EaseOut(min(float32(now.Sub(r.gone))/float32(motionLeave), 1))
	}
	return gp.appear.shown(r.key, now)
}

// ghOpenShut has row r of the GitHub view, h tall, open as far as it
// shows at now: it fades, and grows or shrinks to its height, which moves
// the rows under it along; with less motion, it only fades.
func (a *app) ghOpenShut(c *ui.Context, row ui.Element, r ghRow, h float32, now time.Time) {
	k := a.github.ghShown(r, now)
	if k >= 1 {
		return
	}
	row.Opacity(k)
	if !c.Preferences().ReduceMotion {
		row.Height(h * k).ClipY()
	}
	c.AnimationFrame()
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
	now := c.Now()
	a.githubActivity(c, &g.gh, now)
	if o == nil {
		if !a.online() {
			a.githubNote(c, iconWifiOff, locales.NotConnectedTo(a.conn.Host), "", nil)
		}
		switch {
		case !ctx.githubKnown() || g.gh.problem == git.GHFine:
			// The query keeps its place and what is typed in it, and the
			// keyboard, as the rows are read the first time.
			a.githubQuery(c, ctx, nil, nil)
			a.githubSkeleton(c)
		default:
			a.githubProblem(c, ctx)
		}
		return
	}
	rows := a.githubRows(ctx, o, now)
	queryKeys := a.githubQuery(c, ctx, o, rows)
	// What was read stays, with a line at the foot of the view that says
	// why it is not read again: its host is away, or GitHub failed. It
	// floats over the list, which keeps its place as it comes and goes.
	var bar func()
	switch {
	case !a.online():
		bar = func() { a.githubBar(c, iconWifiOff, locales.NotConnectedTo(a.conn.Host), "", false, time.Time{}, nil) }
	case g.gh.problem != git.GHFine:
		bar = func() {
			a.githubBar(c, iconTriangleAlert, locales.GhStale(g.gh.ghShownError()), locales.GhRetry(), g.gh.refreshing(), g.gh.failedAgain, ctx.refreshGitHubNow)
		}
	}
	ui.Column(c.Key("gh-body")).Grow(1).MinHeight(0).Children(func() {
		a.githubList(c, ctx, o, rows, now, bar != nil, queryKeys)
		if bar != nil {
			bar()
		}
	})
}

// githubActivity is the line at the head of the view that runs while a
// refresh asked for is read, once it took readingShows: one answered
// sooner, as most are, shows nothing that flickers. A refresh the view
// asks for on its own says nothing, as what it read stays meanwhile.
func (a *app) githubActivity(c *ui.Context, gh *githubState, now time.Time) {
	show := false
	if gh.loading {
		if gh.loadingAt.IsZero() {
			gh.loadingAt = now
		}
		if wait := readingShows - now.Sub(gh.loadingAt); gh.refreshing() && wait > 0 {
			c.After(wait)
		} else {
			show = gh.refreshing()
		}
	}
	if show {
		ui.Progress(c.Key("gh-activity"), -1).Height(2).Radius(0).Label(locales.GhReading())
	} else {
		ui.Box(c.Key("gh-activity")).Height(2)
	}
}

// githubQuery is the query of the view, over its list, and its keys: those
// that move the choice and run the chosen row's actions, while it has the
// keyboard, which it reports. Without rows yet, it takes only what is typed.
func (a *app) githubQuery(c *ui.Context, ctx *gitContext, o *git.GHOverview, rows []ghRow) (queryKeys bool) {
	gp := &a.github
	ui.Column(c.Key("gh-query-row")).Padding(0, 12, 6, 12).Children(func() {
		in := a.searchField(c, "gh-query", &gp.query, locales.GhFind(), &gp.input, nil)
		queryKeys = in.Focused()
		if in.Changed() {
			gp.row = ghFirstItem(rows)
		}
		// Escape clears what is typed, as Clear Search does; with nothing
		// typed, it is left to what else takes it.
		if gp.query != "" && in.Shortcut(0, ui.KeyEscape) {
			gp.query = ""
			a.invalidate()
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
		// The keys of the chosen row's actions, as from the list.
		if gp.row >= 0 && gp.row < len(rows) && rows[gp.row].item != nil {
			if do := a.githubKeys(ctx, o, rows[gp.row].item, in.Shortcut); do != nil {
				a.post(do)
			}
		}
	})
	return queryKeys
}

// githubList is the list of the GitHub view, under its query: its
// sections, items and notes, or that the repository has nothing open, or
// that nothing matches. With room kept at its foot for the line over it.
func (a *app) githubList(c *ui.Context, ctx *gitContext, o *git.GHOverview, rows []ghRow, now time.Time, foot, queryKeys bool) {
	p := a.pal
	gp := &a.github
	if len(o.PRs) == 0 && len(o.Issues) == 0 && o.OpenPRs == 0 && o.OpenIssues == 0 {
		a.githubEmpty(c, ctx, o)
		return
	}
	if len(rows) == 0 {
		a.githubNoMatch(c)
		return
	}
	// The rows that go are left out once gone, though none shows to ask
	// for a frame.
	if !gp.goneEnd.IsZero() {
		c.After(max(gp.goneEnd.Sub(now), 0))
	}
	if gp.skeleton {
		gp.skeleton, gp.cameAt = false, now
	}
	gp.row = ghSettle(rows, min(max(gp.row, 0), len(rows)-1), 0)
	was := gp.row
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
	// The rows paint their hover themselves, as the only one under them,
	// and the choice, to glide it between them.
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = ui.Color{}, p.text, ui.Color{}
	c.SetTheme(&quiet)
	var action func()
	reduce := c.Preferences().ReduceMotion
	out := ui.List(c.Key("gh-list"), &gp.list, len(rows), func(i int) {
		var choice *sessionChoice
		if i == gp.row {
			listKeys := gp.list.FocusWithin(c)
			choice = &sessionChoice{keyed: listKeys, held: listKeys || queryKeys}
			choice.y, choice.there = a.ghGlide(rows, i, now, reduce)
			if k, y := gp.ghHop(now); k > 0 {
				choice.y, choice.there, choice.fade = y, 1-k, k
			}
		}
		switch r := rows[i]; {
		case r.header:
			a.githubHeader(c, r, now, &action)
		case r.item != nil:
			a.githubItem(c, ctx, o, r, choice, now, &action)
		default:
			a.githubNoteRow(c, r, choice, now, &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6).FocusRing(false) // the choice rings itself
	if foot {
		out.Padding(0, 6, 8+ghBarRoom, 6)
	}
	c.SetTheme(base)
	// The list's keys stop at a note that only says: the choice goes on
	// past it.
	gp.row = ghSettle(rows, gp.row, was)
	gp.rowKey = ""
	if gp.row >= 0 && gp.row < len(rows) {
		gp.rowKey = rows[gp.row].key
	}
	listKeys := gp.list.FocusWithin(c)
	if gp.focusList && gp.list.Focus(c) {
		gp.focusList, listKeys = false, true
	}
	if gp.row >= 0 && gp.row < len(rows) {
		// The list's keys and clicks move the choice after its rows are
		// built: it glides from the next frame on.
		a.ghGlide(rows, gp.row, now, reduce)
	}
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
	// Space shows the chosen row's card, or hides it, as Quick Look does,
	// and Escape hides it; it goes as the choice moves.
	if gp.row >= 0 && gp.row < len(rows) && rows[gp.row].item != nil {
		r := rows[gp.row]
		if out.Shortcut(0, ui.KeySpace) {
			if gp.card == r.key {
				gp.card = ""
			} else {
				gp.card = r.key
			}
		}
		if do := a.githubKeys(ctx, o, r.item, out.Shortcut); do != nil && action == nil {
			action = do
		}
	}
	if gp.card != "" && (out.Shortcut(0, ui.KeyEscape) || !listKeys || gp.row < 0 || gp.row >= len(rows) || rows[gp.row].key != gp.card) {
		gp.card = ""
	}
	if action != nil {
		action()
	}
}

// githubNoMatch is the view of a query that finds nothing: what to do
// next is to clear it, which gives the keyboard back to the query.
func (a *app) githubNoMatch(c *ui.Context) {
	gp := &a.github
	a.githubCard(c, "gh-no-match", iconSearch, locales.GhNoMatch(), "", time.Time{}, func() {
		if ghPressed(ui.Button(c.Key("gh-clear"), locales.GhClearSearch())) {
			gp.query, gp.row = "", 0
			gp.input.Focus()
		}
	})
}

// githubKeys is what the keys of the chosen row's actions, as shortcut
// reports them, ask to do with it, nil for none: ⌘Enter checks a pull
// request out, or works on an issue, as the pickers' second action, and
// ⌥Enter opens it on GitHub.
func (a *app) githubKeys(ctx *gitContext, o *git.GHOverview, it *git.GHItem, shortcut func(ui.Modifiers, ui.Key) bool) func() {
	switch {
	case shortcut(ui.Cmd, ui.KeyEnter):
		if do, ok := a.githubFirstAction(ctx, o, it); ok {
			return do
		}
	case shortcut(ui.Alt, ui.KeyEnter) && it.URL != "":
		return func() { a.services.OpenURL(it.URL) }
	}
	return nil
}

// githubFirstAction is the first action of an item, which ⌘Enter runs: a
// pull request's Check Out, but on its own branch, or an issue's Work on
// Issue; false while it cannot run.
func (a *app) githubFirstAction(ctx *gitContext, o *git.GHOverview, it *git.GHItem) (func(), bool) {
	n := it.Number
	switch {
	case it.PR && it.HeadRef != ctx.branch() && ctx.git.canMutate():
		return func() { ctx.checkoutPR(n) }, true
	case !it.PR && ctx.git.canMutate() && o.DefaultBranch != "":
		return func() { a.workOnIssue(ctx, n) }, true
	}
	return nil, false
}

// ghStepH is how far the top of row r of the view, not a header, is from
// the next one's, for the choice to glide by.
func ghStepH(r ghRow) float32 {
	if r.item != nil {
		return ghItemH
	}
	return ghNoteH
}

// ghGlide is how far from row i the choice's background shows at now, and
// how much of the way there it went, as in Sessions: within a section; to
// another, it shows there at once.
//
// Over a header, which it would cover on its way, it comes in where it
// goes instead, fading in from a little short of it, the way it went.
// Over a row that opens or closes its room, it glides by the room it has.
func (a *app) ghGlide(rows []ghRow, i int, now time.Time, reduce bool) (y, there float32) {
	gp := &a.github
	g := &gp.glide
	from, fromKey, fromAt := g.row, g.key, g.at
	y, there = g.to(len(rows), i, func(j int) string { return rows[j].key }, func(j int) (float32, bool) {
		return ghStepH(rows[j]) * gp.ghShown(rows[j], now), !rows[j].header
	}, now, reduce)
	if fromKey != "" && fromKey != rows[i].key && y == 0 && !reduce && from >= 0 && from < len(rows) &&
		rows[from].key == fromKey && abs(from-i) <= choiceGlideSpan && now.Sub(fromAt) >= motionHover {
		lo, hi := min(from, i), max(from, i)
		if slices.ContainsFunc(rows[lo:hi], func(r ghRow) bool { return r.header }) {
			gp.hopAt, gp.hopBy = now, 1
			if i < from {
				gp.hopBy = -1
			}
		}
	}
	return y, there
}

// ghHopY is how far short of its row the choice comes in from, as it goes
// over a section's header.
const ghHopY = 6

// ghHop is how far the choice still has to come in at now after it went
// over a header, from 1 to 0, and where from.
func (gp *githubPanel) ghHop(now time.Time) (k, y float32) {
	since := now.Sub(gp.hopAt)
	if gp.hopAt.IsZero() || since < 0 || since >= motionHover {
		return 0, 0
	}
	k = 1 - ui.EaseOut(float32(since)/float32(motionHover))
	return k, -gp.hopBy * ghHopY * k
}

// ghChoosable reports whether the choice may stop at row r: an item, or a
// note that opens GitHub; not a header, a note that only says, nor a row
// that goes.
func ghChoosable(r ghRow) bool { return (r.item != nil || r.link != "") && r.gone.IsZero() }

// ghFirstItem is the first row the choice may stop at, 0 for none.
func ghFirstItem(rows []ghRow) int {
	for i, r := range rows {
		if ghChoosable(r) {
			return i
		}
	}
	return 0
}

// ghSettle is where the choice stops that came to row i from row from:
// i if it may stop there, else the next row it may the way it went, or
// the other way, or from.
func ghSettle(rows []ghRow, i, from int) int {
	if i < 0 || i >= len(rows) || ghChoosable(rows[i]) {
		return i
	}
	by := 1
	if i < from {
		by = -1
	}
	for _, d := range [...]int{by, -by} {
		if k := ghStep(rows, i, d); k != i {
			return k
		}
	}
	return from
}

// ghStep is the row by rows after row i the choice may stop at, past the
// headers and the notes that only say, or i when there is none.
func ghStep(rows []ghRow, i, by int) int {
	for k := i + by; k >= 0 && k < len(rows); k += by {
		if ghChoosable(rows[k]) {
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
	top, moving := gp.headerGap(r, now, c.Preferences().ReduceMotion)
	if moving {
		c.AnimationFrame()
	}
	h := scmRowHeight + top
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(h).Padding(top, 6, 0, 2).Background(p.chrome)
	a.ghOpenShut(c, row, r, h, now)
	going := !r.gone.IsZero()
	clicked := row.Clicked() && !going
	col := p.muted
	if row.Hovered() && !going {
		col = p.text
	}
	row.Children(func() {
		chevron(c, !gp.collapsed[r.section]).FontSize(12).TextColor(col).Transition(hoverFade)
		size, spacing := p.titleLook(10.5, 0.4)
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
// more GitHub has, which a click shows there. Either is ghNoteH tall, for
// the choice to glide over it.
func (a *app) githubNoteRow(c *ui.Context, r ghRow, choice *sessionChoice, now time.Time, action *func()) {
	p := a.pal
	if r.link == "" {
		row := ui.Row(c).Height(ghNoteH).AlignItems(ui.Center).Padding(0, 10, 2, ghTitleIndent)
		a.ghOpenShut(c, row, r, ghNoteH, now)
		row.Children(func() {
			if choice != nil {
				a.choiceMark(c, choice, ghNoteH)
			}
			ui.Text(c, r.note).FontSize(12).TextColor(p.faint).SingleLine()
		})
		return
	}
	// Under the titles, as a row of the list would be; its arrow says it
	// opens GitHub.
	b := ui.ButtonBase(c.Key("note")).FillWidth().Height(ghNoteH).Justify(ui.Start).Padding(0, 10, 2, ghTitleIndent).Radius(5).Label(r.note)
	a.ghOpenShut(c, b, r, ghNoteH, now)
	going := !r.gone.IsZero()
	col := p.muted
	if b.Hovered() && !going {
		col = p.accent
	}
	b.Children(func() {
		if choice != nil {
			a.choiceMark(c, choice, ghNoteH)
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, r.note).FontSize(12).TextColor(col).Transition(hoverFade).SingleLine()
			ui.Icon(c, iconExternal).FontSize(10).TextColor(col).Transition(hoverFade)
		})
	})
	if b.Clicked() && !going {
		*action = func() { a.services.OpenURL(r.link) }
	}
}

// ghItemH is how tall the row of an issue or a pull request is: its title
// over a line of its number, author, age and labels, and ghNoteH a note's.
// ghTitleIndent is where its title starts, past its icon, and ghChipH how
// tall a label is.
// ghRowAvatar and ghCardAvatar are how wide a person's picture is, in a
// row and in its card.
const (
	ghItemH       = 40
	ghNoteH       = 26
	ghTitleIndent = 25
	ghChipH       = 15
	ghRowAvatar   = 14
	ghCardAvatar  = 16
)

// ghRowReviewers is how many pictures of reviewers a row shows, at most,
// and ghFaceOverlap how far each goes over the edge of the one before.
const (
	ghRowReviewers = 3
	ghFaceOverlap  = 5
)

// githubItem is the row of an issue or a pull request: its state's icon,
// its title, and under it its number, author by their picture and age,
// its labels, and its comments; a pull request's checks and review at the
// end, its checks' room kept. A click
// opens it in a tab, a pull request's changes; its menu has the rest.
// Under the pointer its actions show at its end, and its card beside it.
//
// The chosen row shows the choice, which glides to it from the row it was
// on, and its actions while the list has the keyboard, as the keys that
// run them do; Space shows its card.
func (a *app) githubItem(c *ui.Context, ctx *gitContext, o *git.GHOverview, r ghRow, choice *sessionChoice, now time.Time, action *func()) {
	p := a.pal
	it := r.item
	chosen := choice != nil
	row := ui.Column(c).Grow(1).Height(ghItemH).Padding(3, 8, 3, 6).Gap(2).Justify(ui.Center).Radius(6)
	a.ghOpenShut(c, row, r, ghItemH, now)
	// A row that goes shows nothing of the pointer, and does nothing.
	going := !r.gone.IsZero()
	hovered := row.Hovered() && !going
	// The row's hover is the only one under it, the list painting none,
	// and the choice shows none over its own.
	if hovered && !chosen {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	switch wait := scmScrollQuiet - now.Sub(a.github.scrolledAt); {
	case going:
	case chosen && a.github.card == r.key:
		a.githubItemCard(c, o, row, r, true, now)
	case wait > 0:
		c.After(wait)
	default:
		a.githubItemCard(c, o, row, r, false, now)
	}
	icon, col := ghItemIcon(p, it)
	row.Children(func() {
		bg := p.hover.Over(p.chrome)
		if chosen {
			bg = a.choiceMark(c, choice, ghItemH).Over(p.chrome)
		}
		// What the row shows over, as its hover fades: for the rings of
		// its reviewers' pictures.
		under := p.chrome
		if chosen || hovered {
			under = bg
		}
		// A draft's title is quieter, as its icon is: it is not yet for
		// review.
		title := p.text
		if it.Draft {
			title = p.muted
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0)
			ui.Text(c, it.Title).FontSize(13).TextColor(title).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if it.PR {
				a.ghChecks(c, it, r.checks, now)
			}
		})
		age := ""
		if !it.Updated.IsZero() {
			age = " · " + shortAgo(it.Updated, now)
		}
		review, reviewCol := a.ghReview(it)
		if r.again {
			review = locales.GhReReview()
		}
		fit := a.ghMetaFit(c, r, age, review)
		// As tall as the rings of the reviewers' pictures, with them or not.
		ui.Row(c).Height(ghRowAvatar+2).AlignItems(ui.Center).Padding(0, 0, 0, ghTitleIndent-6).Children(func() {
			// What does not fit goes in ghMetaFit's order: the labels give
			// their room up first, then the author's name shortens. The
			// viewer's own items name no author.
			ui.Text(c, r.number).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0).FontFeatures("tnum")
			if fit.avatar {
				a.ghAvatar(c, r.by, r.avatar, ghRowAvatar, now).Margin(0, 4, 0, 6)
			}
			if fit.name {
				ui.Text(c, r.by).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0.01).MinWidth(fit.nameLeast)
			}
			if fit.age {
				ui.Text(c, age).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0).FontFeatures("tnum")
			}
			switch {
			case r.again:
				// In the accent, with its arrow: it is the viewer's turn.
				a.ghStateChip(c, iconRefresh, review, p.accent).Margin(0, 0, 0, 6).Tooltip(locales.GhReReviewTip())
			case review != "":
				a.ghStateChip(c, nil, review, reviewCol).Margin(0, 0, 0, 6)
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
			if fit.reviewers {
				a.ghReviewerStack(c, r, under, now)
			}
			if fit.comments {
				ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 6).Children(func() {
					ui.Icon(c, iconComment).FontSize(10).TextColor(p.faint)
					ui.Text(c, strconv.Itoa(it.Comments)).FontSize(11).TextColor(p.faint).FontFeatures("tnum")
				})
			}
		})
		if hovered || chosen && choice.keyed {
			a.githubItemActions(c, ctx, o, it, bg, action)
		}
	})
	if going {
		return
	}
	row.ContextMenu(func(m *ui.Menu) {
		a.githubItemMenu(m, ctx, o, it, action)
		m.Separator()
		// The card of the row, chosen, as Space shows it.
		if m.Item(locales.GhShowDetails()).Shortcut(0, ui.KeySpace).Chosen() {
			*action = func() { a.showGitHubCard(r.key) }
		}
	})
	if row.Clicked() && *action == nil {
		*action = func() { a.openGitHubItem(ctx, it, false) }
	}
}

// ghNameLeast is how narrow an author's name shortens to before it goes,
// leaving their picture.
const ghNameLeast = 36

// ghMeta is what of the line under an item's title shows, and how narrow
// the author's name may shorten to, nameLeast.
type ghMeta struct {
	avatar, name, age, reviewers, comments bool
	nameLeast                              float32
}

// ghMetaFit is what of the line under row r's title fits the sidebar's
// width, age and review the words of its age and of its state's chip. The
// number and the chip always show, as the chip says whose turn it is;
// then, as room allows and in this order, its age, the author's picture,
// their name at least ghNameLeast wide, its comments and its reviewers.
// The labels take what is left, then the name the rest of its width.
func (a *app) ghMetaFit(c *ui.Context, r ghRow, age, review string) ghMeta {
	text := func(s string, size float32, weight int) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: s, Size: size, Weight: weight, Features: "tnum"})
		return w
	}
	// The sidebar's edge, the list's padding and the row's, then the indent
	// of the line.
	room := a.window.SidebarWidth - 1 - 12 - 14 - (ghTitleIndent - 6) - text(r.number, 11.5, 400)
	if review != "" {
		room -= 6 + 10 + text(review, 10.5, 500)
		if r.again {
			room -= 9 + 2
		}
	}
	take := func(w float32) bool {
		if w > room {
			return false
		}
		room -= w
		return true
	}
	var m ghMeta
	m.age = age != "" && take(text(age, 11.5, 400))
	m.avatar = r.by != "" && take(4+ghRowAvatar+6)
	if m.avatar {
		m.nameLeast = min(text(r.by, 11.5, 400), ghNameLeast)
		m.name = take(m.nameLeast)
	}
	m.comments = r.item.Comments > 0 && take(6+10+3+text(strconv.Itoa(r.item.Comments), 11, 400))
	if n := len(r.reviewers); n > 0 {
		const ring = ghRowAvatar + 2
		w := 6 + ring + float32(n-1)*(ring-ghFaceOverlap)
		if r.moreReviewers > 0 {
			w += 2 + text("+"+strconv.Itoa(r.moreReviewers), 10.5, 400)
		}
		m.reviewers = take(w)
	}
	return m
}

// ghReviewerStack is the pictures of the reviewers of a pull request's
// row, at its end, each over the edge of the one before, ringed in under,
// what the row shows over, so that they read apart; then how many more.
func (a *app) ghReviewerStack(c *ui.Context, r ghRow, under ui.Color, now time.Time) {
	p := a.pal
	const ring = ghRowAvatar + 2
	ui.Row(c.Key("reviewers")).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 6).Label(locales.GhReviewersLabel(r.reviewedBy)).
		Children(func() {
			for k, f := range r.reviewers {
				b := ui.Box(c.Key(f.login)).Size(ring, ring).Shrink(0).Radius(ring / 2).Background(under).Transition(hoverFade).
					Justify(ui.Center).AlignItems(ui.Center)
				if k > 0 {
					b.Margin(0, 0, 0, -ghFaceOverlap)
				}
				b.Children(func() { a.ghAvatar(c, f.login, f.avatar, ghRowAvatar, now) })
			}
			if r.moreReviewers > 0 {
				ui.Text(c, "+"+strconv.Itoa(r.moreReviewers)).FontSize(10.5).TextColor(p.faint).FontFeatures("tnum").
					SingleLine().Margin(0, 0, 0, 2)
			}
		})
}

// ghChecks is the icon of a pull request's checks, at the end of its
// title: while they run, a ring that turns. As a refresh changes them, the
// icon of how they are fades in over the one of how they were, which fades
// out sooner, and grows from a little smaller; with less motion, it only
// fades.
func (a *app) ghChecks(c *ui.Context, it *git.GHItem, room bool, now time.Time) {
	reduce := c.Preferences().ReduceMotion
	w, changed := a.github.checks[it.Number]
	since := now.Sub(w.at)
	if !changed || since >= motionShow || since < 0 {
		switch {
		case it.Checks != git.ChecksNone:
			a.ghChecksMark(c, "is", it.Checks, reduce)
		case room:
			ui.Box(c).Size(11, 11).Shrink(0)
		}
		return
	}
	c.AnimationFrame()
	k := ui.EaseOut(float32(since) / float32(motionShow))
	gone := ui.EaseOut(min(float32(since)/float32(motionLeave), 1))
	ui.Box(c.Key("checks")).Size(11, 11).Shrink(0).Children(func() {
		if w.from != git.ChecksNone {
			a.ghChecksMark(c, "was", w.from, reduce).Absolute().Left(0).Top(0).Opacity(1 - gone)
		}
		if it.Checks != git.ChecksNone {
			e := a.ghChecksMark(c, "is", it.Checks, reduce).Absolute().Left(0).Top(0).Opacity(k)
			if !reduce {
				e.Scale(0.6+0.4*k, 0.6+0.4*k)
			}
		}
	})
}

// ghChecksMark is the icon of checks that went as state, 11 across: for
// checks that run, a faint ring with an arc of it that turns, once each
// motionSpin, as spinRing paints it: a list with none running asks for no
// frame. With less motion, it is the clock, still.
func (a *app) ghChecksMark(c *ui.Context, key string, state git.Checks, reduce bool) ui.Element {
	ic, col := prChecksIcon(a.pal, state)
	if state != git.ChecksPending || reduce {
		return ui.Icon(c.Key(key), ic).FontSize(11).TextColor(col).Shrink(0).Width(11)
	}
	return spinRing(c, key, 11, col)
}

// ghItemIcon is the icon of an issue or a pull request, in the color of
// its state.
func ghItemIcon(p *palette, it *git.GHItem) (*ui.SVG, ui.Color) {
	switch {
	case !it.PR:
		return iconIssue, p.addedFg
	case it.Draft:
		return iconPullRequestDraft, p.muted
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
// pointer, or chosen while the list has the keyboard: a pull request's
// Check Out, an issue's Work on Issue, then Open on GitHub and Copy Link,
// their tips naming their keys. They float over the row's end, which fades
// out under them into bg, what the row is drawn over, so that nothing of
// the row moves as they come.
func (a *app) githubItemActions(c *ui.Context, ctx *gitContext, o *git.GHOverview, it *git.GHItem, bg ui.Color, action *func()) {
	p := a.pal
	type act struct {
		icon  *ui.SVG
		label string
		keys  string
		off   bool
		do    func()
	}
	n := it.Number
	var acts []act
	first := keyText("CmdOrCtrl+Enter")
	switch {
	case it.PR && it.HeadRef != ctx.branch():
		acts = append(acts, act{iconCheckout, locales.GhCheckOut(), first, !ctx.git.canMutate(), func() { ctx.checkoutPR(n) }})
	case !it.PR:
		acts = append(acts, act{iconAgent, locales.GhWorkOnIssue(), first, !ctx.git.canMutate() || o.DefaultBranch == "", func() { a.workOnIssue(ctx, n) }})
	}
	if it.URL != "" {
		acts = append(acts,
			act{iconExternal, locales.PrOpenOnGitHub(), keyText("Alt+Enter"), false, func() { a.services.OpenURL(it.URL) }},
			act{iconCopy, locales.GhCopyLink(), keyText("CmdOrCtrl+C"), false, func() { a.copyGitHubLink(it) }})
	}
	if len(acts) == 0 {
		return
	}
	w := float32(len(acts)*ghActionW + ghActionFade + 4)
	strip := ui.Row(c.Key("gh-actions")).Absolute().Top(0).Bottom(0).Right(0).Width(w).Padding(0, 4, 0, ghActionFade).
		AlignItems(ui.Center).Justify(ui.End).Radius(6).
		LinearGradient(ui.LinearGradient{From: bg.Alpha(0), To: bg, Angle: 90, End: ghActionFade / w}).Transition(revealFade)
	strip.Children(func() {
		for _, ac := range acts {
			b := ui.ButtonBase(c.Key(ac.label)).Size(ghActionW, ghActionW).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
				Label(ac.label).Tooltip(ac.label + " (" + ac.keys + ")").Disabled(ac.off)
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
// as a commit's, and ghCardNameW the room of the name of its reviewers'
// lines, before their pictures.
const (
	ghCardW     = commitCardW
	ghCardNameW = 64
)

// githubItemCard is the card that shows as the pointer rests on the row of
// an issue or a pull request, beside the sidebar: its whole title and all
// its labels, then who made it and who it is assigned to, by their
// pictures, a pull request's branch, size, checks, review and each of its
// reviewers, and when it was updated. It
// shows only what the view read already.
//
// Space shows it for the chosen row, keyed, at once, as a key opens what
// it opens, and it stays until the choice moves.
func (a *app) githubItemCard(c *ui.Context, o *git.GHOverview, row ui.Element, r ghRow, keyed bool, now time.Time) {
	if keyed {
		ui.Overlay(c, func() {
			a.githubCardBox(c, o, ui.Box(c.Key("gh-card")).Absolute().PassThrough().Role(ui.RoleTooltip), row, r, false, now)
		})
		return
	}
	ui.TooltipBase(c, row, func(tip ui.Element) { a.githubCardBox(c, o, tip, row, r, true, now) })
}

// githubCardBox builds the card of row r in tip, by row, the side away
// from the sidebar's edge; fading in, or at once.
func (a *app) githubCardBox(c *ui.Context, o *git.GHOverview, tip, row ui.Element, r ghRow, fade bool, now time.Time) {
	p := a.pal
	it := r.item
	a.besideSidebar(tip, row)
	a.floating(c, tip, false).Radius(10).Width(ghCardW)
	if fade {
		fadeIn(tip)
	}
	tip.Children(func() {
		ui.Column(c).Gap(6).Padding(12, 14, 10, 14).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				icon, col := ghItemIcon(p, it)
				ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0)
				ui.Text(c, r.number).FontSize(11.5).TextColor(p.muted).FontFeatures("tnum").SingleLine()
				if it.Draft {
					a.ghStateChip(c, nil, locales.PrDraft(), p.muted)
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
			person := func(login string) {
				a.ghAvatar(c, login, o.Avatars[login], ghCardAvatar, now)
				text(login, p.text)
			}
			if it.Author != "" {
				field(locales.GhCardAuthor(), func() { person(it.Author) })
			}
			switch len(it.Assignees) {
			case 0:
			case 1:
				field(locales.GhCardAssignees(), func() { person(it.Assignees[0]) })
			default:
				// Their pictures side by side, their names after.
				field(locales.GhCardAssignees(), func() {
					ui.Row(c).Gap(2).Shrink(0).Children(func() {
						for _, as := range it.Assignees {
							a.ghAvatar(c.Key(as), as, o.Avatars[as], ghCardAvatar, now)
						}
					})
					text(strings.Join(it.Assignees, ", "), p.text)
				})
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
				// Each reviewer as a line of a table by the field's name: their
				// picture and name, and how their latest review went at the
				// end, so that the pictures line up.
				for k, rv := range it.Reviewers {
					name := ""
					if k == 0 {
						name = locales.GhCardReviewers()
					}
					ui.Row(c.Key(rv.Login)).Gap(6).AlignItems(ui.Center).Height(18).Children(func() {
						ui.Text(c, name).FontSize(11.5).TextColor(p.muted).Shrink(0).Width(ghCardNameW).SingleLine()
						a.ghAvatar(c, rv.Login, o.Avatars[rv.Login], ghCardAvatar, now)
						text(rv.Login, p.text)
						ui.Spacer(c)
						ic, word, col := a.ghReviewerState(rv)
						ui.Icon(c, ic).FontSize(11).TextColor(col).Shrink(0)
						ui.Text(c, word).FontSize(11.5).TextColor(col).SingleLine().Shrink(0).Margin(0, 0, 0, -3)
					})
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
}

// ghReviewerState is the icon, word and color of how a reviewer's latest
// review went, as GHReviewer's State says, or that it is asked again.
func (a *app) ghReviewerState(rv git.GHReviewer) (*ui.SVG, string, ui.Color) {
	p := a.pal
	if rv.Again {
		return iconRefresh, locales.GhReReview(), p.accent
	}
	switch rv.State {
	case "APPROVED":
		return iconCheck, locales.PrApproved(), p.addedFg
	case "CHANGES_REQUESTED":
		return iconDiff, locales.PrChangesRequested(), p.dangerFg
	case "COMMENTED":
		return iconComment, locales.GhReviewCommented(), p.muted
	}
	return iconClock, locales.GhReviewPending(), p.modifiedFg
}

// ghCheckCounts is how the checks of a pull request went, as the card
// shows them: how many passed, failed and run, each by its icon; or its
// icon alone, for checks GitHub did not count.
func (a *app) ghCheckCounts(c *ui.Context, it *git.GHItem) {
	p := a.pal
	st := it.CheckCounts
	reduce := c.Preferences().ReduceMotion
	if st.Passed+st.Failed+st.Pending == 0 {
		if it.Checks != git.ChecksNone {
			a.ghChecksMark(c, "checks", it.Checks, reduce)
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
			ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
				a.ghChecksMark(c, "mark", n.state, reduce)
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

// ghStateChip is the state of a pull request on a chip as tall as a
// label's, tinted in col, with icon before its word if not nil: filled,
// where a label is ringed, so that the two read apart.
func (a *app) ghStateChip(c *ui.Context, icon *ui.SVG, word string, col ui.Color) ui.Element {
	tint := col.Alpha(0.14)
	if col == a.pal.muted {
		// A draft's is grey, as its icon is.
		tint = a.pal.text.Alpha(0.07)
	}
	return ui.Row(c).Gap(2).AlignItems(ui.Center).Height(ghChipH).Padding(0, 5).Radius(ghChipH / 2).Background(tint).Shrink(0).
		Children(func() {
			if icon != nil {
				ui.Icon(c, icon).FontSize(9).TextColor(col)
			}
			ui.Text(c, word).FontSize(10.5).FontWeight(500).TextColor(col).SingleLine()
		})
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

// showGitHubCard chooses the row of key and shows its card, as Space does,
// with the keyboard in the list.
func (a *app) showGitHubCard(key string) {
	gp := &a.github
	for i, r := range gp.rows {
		if r.key == key {
			gp.row, gp.card, gp.focusList = i, key, true
			return
		}
	}
}

// githubItemMenu is the menu of an issue or a pull request of the view,
// with the keys of the chosen row's.
func (a *app) githubItemMenu(m *ui.Menu, ctx *gitContext, o *git.GHOverview, it *git.GHItem, action *func()) {
	n := it.Number
	do := func(fn func()) { *action = fn }
	if it.PR {
		if m.Item(locales.PrOpenChanges()).Shortcut(0, ui.KeyEnter).Chosen() {
			do(func() { ctx.openPullRequestWith(n, false, false, it.Title) })
		}
		if m.Item(locales.GhOpenConversation()).Chosen() {
			do(func() { a.openIssue(ctx, n, false) })
		}
		if m.Item(locales.PrShowChecks()).Disabled(it.Checks == git.ChecksNone).Chosen() {
			do(func() { ctx.openChecksOf(n) })
		}
		m.Separator()
		if m.Item(locales.GhCheckOut()).Shortcut(ui.Cmd, ui.KeyEnter).Disabled(!ctx.git.canMutate() || it.HeadRef == ctx.branch()).Chosen() {
			do(func() { ctx.checkoutPR(n) })
		}
		if m.Item(locales.GhAddressReview()).Chosen() {
			do(func() { a.openPRWithThreads(ctx, n) })
		}
	} else {
		if m.Item(locales.GhOpenIssue()).Shortcut(0, ui.KeyEnter).Chosen() {
			do(func() { a.openIssue(ctx, n, false) })
		}
		m.Separator()
		if m.Item(locales.GhWorkOnIssue()).Shortcut(ui.Cmd, ui.KeyEnter).Disabled(!ctx.git.canMutate() || o.DefaultBranch == "").Chosen() {
			do(func() { a.workOnIssue(ctx, n) })
		}
		if m.Item(locales.GhSendIssue()).Chosen() {
			do(func() { a.sendIssue(ctx, n) })
		}
	}
	m.Separator()
	if m.Item(locales.PrOpenOnGitHub()).Shortcut(ui.Alt, ui.KeyEnter).Disabled(it.URL == "").Chosen() {
		do(func() { a.services.OpenURL(it.URL) })
	}
	if m.Item(locales.GhCopyLink()).Shortcut(ui.Cmd, ui.KeyC).Disabled(it.URL == "").Chosen() {
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

// openGitHubItem opens an issue in a tab, or a pull request's changes: in
// the preview tab of the file column, unless keep.
func (a *app) openGitHubItem(ctx *gitContext, it *git.GHItem, keep bool) {
	if it.PR {
		ctx.openPullRequestWith(it.Number, false, keep, it.Title)
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
	// Its arrow turns to a ring that turns until GitHub answers, so that
	// the click shows at once however long that takes; with less motion,
	// the arrow only dims.
	refreshing := ctx.git.gh.refreshing() && a.online()
	reduce := c.Preferences().ReduceMotion
	b := a.iconButtonOf(c, locales.GhRefresh(), false, func(col ui.Color) {
		switch {
		case !refreshing:
			ui.Icon(c.Key("refresh"), iconRefresh).FontSize(15).TextColor(col)
		case reduce:
			ui.Icon(c.Key("refresh"), iconRefresh).FontSize(15).TextColor(a.pal.faint)
		default:
			spinRing(c, "refreshing", 13, col).Transition(revealFade)
		}
	})
	if b.Clicked() && !refreshing {
		ctx.refreshGitHubNow()
	}
}

// githubSkeleton is the shapes of the view's list, under its query, while
// it reads its rows the first time: a section's header and its rows, each
// where the real one goes, so that nothing moves as they come. A wave runs
// down them while they wait.
func (a *app) githubSkeleton(c *ui.Context) {
	p := a.pal
	a.github.skeleton = true
	bar := p.text.Alpha(0.07)
	reduce := c.Preferences().ReduceMotion
	ui.Column(c.Key("gh-skeleton")).Grow(1).Padding(0, 6, 8, 6).Label(locales.GhReading()).Children(func() {
		ui.Row(c).Height(scmRowHeight).Padding(0, 0, 0, 16).AlignItems(ui.Center).Children(func() {
			pulseShape(ui.Box(c).Size(84, 7), bar, 3.5, 0, reduce)
		})
		for i, w := range []float32{0.8, 0.62, 0.88, 0.55, 0.72} {
			ui.Column(c.Key(i)).Height(ghItemH).Padding(3, 8, 3, 6).Gap(7).Justify(ui.Center).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					pulseShape(ui.Box(c).Size(13, 13), bar, 6.5, i+1, reduce)
					pulseShape(ui.Box(c).WidthPercent(w*90).Height(8), bar, 4, i+1, reduce)
				})
				pulseShape(ui.Box(c).Size(90, 6).Margin(0, 0, 0, ghTitleIndent-6), bar, 3, i+1, reduce)
			})
		}
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
		if button != "" && ghPressed(ui.Button(c.Key("gh-note-button"), button).AlignSelf(ui.End)) {
			fn()
		}
	})
}

// ghBarRoom is the room the list keeps at its foot for githubBar, so
// that its last rows scroll out from under it.
const ghBarRoom = 60

// githubBar is the line at the foot of the view, over its list: that its
// host is away, or that GitHub could not be read again, with a button that
// tries again, by its icon so that the words of why keep the room; while
// busy, as it tries, its arrow is a ring that turns, from the click on.
// It rises in as it comes, and the list stays where it is.
//
// Its icon shakes its head from shakeAt, as a retry failed again.
func (a *app) githubBar(c *ui.Context, icon *ui.SVG, text, button string, busy bool, shakeAt time.Time, fn func()) {
	p := a.pal
	bar := ui.Row(c.Key("gh-bar")).Absolute().Left(8).Right(8).Bottom(8).Padding(6, 6, 6, 10).Gap(8).AlignItems(ui.Center).
		Radius(8).Transition(barIn)
	a.floating(c, bar, false)
	bar.Children(func() {
		shake(c, ui.Icon(c.Key("icon"), icon).FontSize(13).TextColor(p.modifiedFg).Shrink(0), shakeAt)
		ui.Text(c, text).FontSize(12).TextColor(p.muted).LineHeight(1.35).MaxLines(3).Tooltip(text).
			Grow(1).Shrink(1).MinWidth(0)
		switch {
		case button == "":
		case busy:
			ui.Box(c.Key(button)).Size(20, 20).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Label(button).Children(func() {
				a.ghBusyMark(c, 13, p.muted)
			})
		case a.smallButton(c, iconRefresh, button):
			fn()
		}
	})
}

// ghPressed sinks button b of the view as it is held, as pressPose does,
// and reports whether it was clicked.
func ghPressed(b ui.Element) bool {
	pressPose(b)
	return b.Clicked()
}

// githubCard is a state of the view in its middle: an icon, what it is,
// why, and what to do. Its icon shakes its head from shakeAt, as what was
// tried again failed again; zero for never.
func (a *app) githubCard(c *ui.Context, key string, icon *ui.SVG, title, detail string, shakeAt time.Time, buttons func()) {
	p := a.pal
	ui.Column(c.Key(key)).Padding(28, 20, 8, 20).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		badge := popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center))
		shake(c, badge, shakeAt).Children(func() {
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
	again := g.gh.failedAgain
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
		a.githubCard(c, "gh-missing", iconTerminal, locales.GhMissing(), detail, again, func() {
			if ghPressed(ui.PrimaryButton(c, locales.GhGetCLI()).FillWidth()) {
				a.services.OpenURL(ghGetCLI)
			}
			a.ghRetryButton(c, ctx, false)
		})
	case git.GHSignedOut:
		a.githubCard(c, "gh-signed-out", iconLock, locales.GhSignedOut(), locales.GhSignedOutDetail(), again, func() {
			if ghPressed(ui.PrimaryButton(c, locales.GhSignIn()).FillWidth()) {
				a.post(func() { a.runInTerminal(ctx.git.repo.Root, "gh auth login") })
			}
			a.ghRetryButton(c, ctx, false)
		})
	case git.GHNoDefault:
		a.githubCard(c, "gh-no-default", iconGitHub, locales.GhNoDefault(), locales.GhNoDefaultDetail(), again, func() {
			seen := map[string]bool{}
			for _, rm := range g.gh.remotes {
				if seen[rm.Path] {
					continue
				}
				seen[rm.Path] = true
				path := rm.Path
				if ghPressed(ui.Button(c.Key("gh-default:"+path), path).FillWidth()) {
					a.post(func() { ctx.setDefaultRepo(path) })
				}
			}
		})
	default:
		a.githubCard(c, "gh-error", iconTriangleAlert, locales.GhFailed(), g.gh.ghShownError(), again, func() {
			a.ghRetryButton(c, ctx, true)
			if ghPressed(ui.Button(c.Key("gh-report"), locales.ReportProblem()).FillWidth()) {
				a.reportProblem(locales.GhFailed(), g.gh.err)
			}
		})
	}
}

// ghRetryButton is Try Again on a card of the view, primary or not, which
// asks GitHub again. From the click until GitHub answers it is dimmed,
// with a ring that turns at its start, so that the click shows at once
// however long GitHub takes; its words keep their place.
func (a *app) ghRetryButton(c *ui.Context, ctx *gitContext, primary bool) {
	busy := ctx.git.gh.refreshing() && a.online()
	ui.Box(c.Key("gh-retry")).FillWidth().Children(func() {
		var b ui.Element
		if primary {
			b = ui.PrimaryButton(c, locales.GhRetry())
		} else {
			b = ui.Button(c.Key("button"), locales.GhRetry())
		}
		b.FillWidth().Disabled(busy)
		pressPose(b)
		if b.Clicked() && !busy {
			ctx.refreshGitHubNow()
		}
		if busy {
			col := a.pal.muted
			if primary {
				col = c.Theme().AccentText
			}
			ui.Row(c.Key("busy")).Absolute().Left(10).Top(0).Bottom(0).AlignItems(ui.Center).PassThrough().Transition(revealFade).
				Children(func() { a.ghBusyMark(c, 12, col) })
		}
	})
}

// ghBusyMark is the mark of a Try Again that runs, size across: a ring
// that turns, or with less motion, the arrow, still.
func (a *app) ghBusyMark(c *ui.Context, size float32, col ui.Color) {
	if c.Preferences().ReduceMotion {
		ui.Icon(c.Key("busy-mark"), iconRefresh).FontSize(size).TextColor(col)
		return
	}
	spinRing(c, "busy-mark", size, col)
}

// githubEmpty is the view of a repository with no issue or pull request
// open.
func (a *app) githubEmpty(c *ui.Context, ctx *gitContext, o *git.GHOverview) {
	a.githubCard(c, "gh-empty", iconCircleCheck, locales.GhNothingOpen(), o.Repo, time.Time{}, func() {
		if o.IssuesOn && ghPressed(ui.PrimaryButton(c, locales.GhNewIssue()).FillWidth()) {
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
