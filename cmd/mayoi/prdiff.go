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

// prDiff is the pull request a diff shows the changes of, as GitHub has
// it: read when the tab opens, or once a tab kept from before shows.
type prDiff struct {
	number int
	// info is what gh told of it, nil until read; reading is set while gh
	// runs, and read once it answered, err why it failed.
	info    *git.PRInfo
	reading bool
	read    bool
	err     string
	// viewed are the files the user marked viewed on GitHub, by their
	// paths in the repository, viewedOK set once GitHub told them; marking
	// those whose mark is being sent.
	viewed   map[string]bool
	viewedOK bool
	marking  map[string]bool
	// newer is set when the pull request's head moved past the commit the
	// tab shows.
	newer bool
	// title names the tab until GitHub tells its own. opening is set while
	// the pull request is read for the tab, threads when its review threads
	// are read with it, and openErr why it could not be.
	title   string
	opening bool
	threads bool
	openErr string
	// fetched is set once the tab, pending, asked for its changes itself.
	fetched bool
	// The pages of the tab, which stay as its changes are read anew.
	*prPageState
}

// prPageState is what the pages of a pull request's tab show of it, and
// what is under way there.
type prPageState struct {
	// page is the page of the tab shown.
	page prPage
	// view is the pull request as its pages show it, nil until read;
	// viewing is set while it is read, viewAsked when it last was asked
	// for and viewAt answered, viewErr why it could not be. checks and
	// ghThreads, its review threads, are read with it, checksOK and
	// threadsOK set once they were; threadGen counts the changes to
	// ghThreads, which the diff lays out their cards again for.
	view      *git.PRView
	viewing   bool
	viewAsked time.Time
	viewAt    time.Time
	viewErr   string
	checks    []git.Check
	checksOK  bool
	ghThreads []git.GHThread
	threadsOK bool
	threadGen int
	// showResolved shows the threads resolved in the diff too.
	showResolved bool
	// texts are the description and the conversation parsed, as of
	// parsedOf; stale is set once a picture of them arrived.
	texts    [][]markdownBlock
	parsedOf *git.PRView
	stale    bool
	cache    markdownCache
	// comment is what the box of the conversation holds, sending set
	// while it is sent, and reviewing the review it writes, as gh pr
	// review names it: request-changes or comment, "" for a comment.
	// focusComposer gives the box the keyboard once it shows.
	comment       string
	sending       bool
	reviewing     string
	focusComposer bool
	// acting names the action under way on the pull request.
	acting string
	// replyTo is the review thread being answered in the diff, reply
	// what is typed, replying set while it is sent; resolving are the
	// threads whose resolution is being sent.
	replyTo   string
	reply     string
	replying  bool
	resolving map[string]bool
}

// name is how the tab of a pull request is named: its number and title.
func (pr *prDiff) name() string {
	s := "#" + strconv.Itoa(pr.number)
	if pr.info != nil && pr.info.Title != "" {
		s += " " + pr.info.Title
	} else if pr.title != "" {
		s += " " + pr.title
	}
	return s
}

// prKey is the key of the diff of pull request n: its changes from commit
// from to commit to.
func prKey(n int, from, to string) string {
	return "pr:" + strconv.Itoa(n) + ":" + from + ".." + to
}

// parsePRKey reads a key of prKey.
func parsePRKey(key string) (n int, from, to string, ok bool) {
	rest, ok := strings.CutPrefix(key, "pr:")
	if !ok {
		return 0, "", "", false
	}
	num, span, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, "", "", false
	}
	n, err := strconv.Atoi(num)
	from, to, ok = strings.Cut(span, "..")
	if err != nil || n <= 0 || !ok || from == "" || to == "" {
		return 0, "", "", false
	}
	return n, from, to, true
}

// openPullRequest opens pull request n in a tab of its own, at page.
func (a *gitContext) openPullRequest(n int, page prPage) {
	a.openPullRequestWith(n, page, false, true, "")
}

// prSamePage is the page of openPullRequestWith that leaves a tab on the
// page it shows.
const prSamePage prPage = -1

// openPullRequestWith shows the tab of pull request n at once, at page,
// in the preview tab of the file column unless keep, named title until
// GitHub tells it, and reads the pull request in the background: what
// GitHub has of it, its commits fetched where they are missing, and with
// threads, its review threads not resolved, as notes on its lines for an
// agent to address. Its changes are those GitHub shows, from where its
// head left its base to its head.
func (a *gitContext) openPullRequestWith(n int, page prPage, threads, keep bool, title string) {
	if !a.live() {
		return
	}
	r := a.git.repo
	if r == nil {
		return
	}
	app := a.app
	var pr *prDiff
	if col, i := app.prTab(n, r.Root); col != nil {
		t := col.tabs[i]
		if keep {
			app.keepTab(t)
		}
		app.activate(col, i, true)
		pr = t.diff.commit.pr
		if page != prSamePage {
			pr.page = page
		}
		app.readPRViewSoon(t.diff)
	} else {
		v := newPendingPR(parseDiffLayout(app.settings.DiffLayout), n, title)
		v.repoRoot = r.Root
		pr = v.commit.pr
		pr.page = max(page, prConversation)
		col := app.fileColumn()
		if keep {
			at := min(col.active+1, len(col.tabs))
			app.addTab(col, at, &colTab{diff: v})
			app.activate(col, at, true)
		} else {
			app.previewIn(col, func(at int) {
				app.addTab(col, at, &colTab{diff: v, transient: true})
				app.activate(col, at, false)
			})
		}
		app.rememberTabs()
	}
	a.fetchPR(pr, threads)
}

// fetchPR reads, in the background, the pull request of a tab, its
// commits fetched where they are missing, and with threads its review
// threads as notes, then shows its changes in its tabs.
func (a *gitContext) fetchPR(pr *prDiff, threads bool) {
	app, r, n := a.app, a.git.repo, pr.number
	if pr.opening || r == nil {
		return
	}
	pr.openErr = ""
	if !app.online() {
		pr.openErr = locales.NotConnectedTo(app.conn.Host)
		return
	}
	pr.opening, pr.threads = true, threads
	var (
		info   *git.PRInfo
		from   string
		viewed map[string]bool
		notes  []*reviewNote
	)
	g := a.git
	bg.Go(func() {
		defer recoverCrash()
		err := func() (err error) {
			if info, err = r.PullRequestInfo(n); err != nil {
				if errors.Is(err, git.ErrNoPR) {
					return errors.New(locales.PrNotFound("#" + strconv.Itoa(n)))
				}
				return err
			}
			if from, err = r.FetchPullRequest(info); err != nil {
				return err
			}
			// Without the marks, the files show unmarked; the tab still opens.
			viewed, _ = r.PRViewed(info.ID)
			if threads {
				ths, err := r.ReviewThreads(info.Number)
				if err != nil {
					return err
				}
				notes = threadNotes(r, prKey(info.Number, from, info.HeadOid), from, info.HeadOid, ths)
			}
			return nil
		}()
		app.post(func() {
			pr.opening = false
			if g.repo != r {
				return
			}
			if err != nil {
				if errors.Is(err, git.ErrNoGH) {
					g.ghOK = false
				}
				app.prOpenFailed(n, r.Root, err)
				return
			}
			g.ghOK = true
			app.showPullRequest(info, from, viewed, r.Root)
			if threads {
				app.addThreadNotes(info.Number, notes)
			}
		})
	})
}

// newPendingPR makes the tab of pull request n while it is read, named
// title until GitHub tells it.
func newPendingPR(layout diffLayout, n int, title string) *diffView {
	v := newDiffView(layout)
	v.commit = &commitDiff{pr: newPRDiff(n)}
	v.commit.pr.title = title
	return v
}

// newPRDiff is pull request n of a tab, its pages not yet read.
func newPRDiff(n int) *prDiff { return &prDiff{number: n, prPageState: &prPageState{}} }

// pending reports whether a commit's diff is the tab of a pull request
// whose commits are not known yet.
func (cm *commitDiff) pending() bool { return cm != nil && cm.pr != nil && cm.hash == "" }

// prTab is the column and place of a tab of pull request n of the
// repository at root, in a column not locked, nil for none.
func (a *app) prTab(n int, root string) (*column, int) {
	for _, col := range a.cols {
		if col.locked {
			continue
		}
		for i, t := range col.tabs {
			if a.isPRTab(t, n, root) {
				return col, i
			}
		}
	}
	return nil, -1
}

// isPRTab reports whether a tab is one of pull request n of the
// repository at root.
func (a *app) isPRTab(t *colTab, n int, root string) bool {
	return t.diff != nil && t.diff.commit != nil && t.diff.commit.pr != nil && t.diff.commit.pr.number == n && a.diffBelongs(t.diff, root)
}

// prOpenFailed tells why pull request n could not be read: in its tabs
// still pending, else as a failure.
func (a *app) prOpenFailed(n int, root string, err error) {
	msg := ghMessage(err)
	shown := false
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if a.isPRTab(t, n, root) && t.diff.commit.pending() {
				t.diff.commit.pr.opening, t.diff.commit.pr.openErr = false, msg
				shown = true
			}
		}
	}
	if !shown {
		a.repositoryForRoot(root).gitFailed(err)
	}
	a.invalidate()
}

// showPullRequest shows the changes of a pull request, from commit from to
// its head, in its tabs: each pending or showing older commits made anew,
// each showing the same commits told what GitHub tells now. With none, it
// opens one after the tab shown of the column with the keyboard.
func (a *app) showPullRequest(info *git.PRInfo, from string, viewed map[string]bool, root string) {
	key := prKey(info.Number, from, info.HeadOid)
	view := func(layout diffLayout) *diffView {
		v := newCommitView(layout, key, git.Commit{})
		v.repoRoot = root
		pr := v.commit.pr
		pr.info, pr.read = info, true
		pr.viewed, pr.viewedOK = viewed, viewed != nil
		return v
	}
	found := false
	for _, col := range a.cols {
		if col.locked {
			continue
		}
		for _, t := range col.tabs {
			if !a.isPRTab(t, info.Number, root) {
				continue
			}
			found = true
			if t.diff.commit.key() == key {
				// The same commits: what GitHub tells now.
				old := t.diff.commit.pr
				old.info, old.read, old.err, old.newer = info, true, "", false
				if viewed != nil {
					old.viewed, old.viewedOK = viewed, true
					foldViewed(t.diff)
				}
				t.diff.changed()
				continue
			}
			v := view(t.diff.layout)
			// Its pages stay as they were, the page shown too.
			v.commit.pr.prPageState = t.diff.commit.pr.prPageState
			v.commit.pr.title = t.diff.commit.pr.title
			t.diff.stop()
			t.diff = v
		}
	}
	if !found {
		col := a.fileColumn()
		at := min(col.active+1, len(col.tabs))
		v := view(parseDiffLayout(a.settings.DiffLayout))
		v.commit.pr.page = prChanges
		a.addTab(col, at, &colTab{diff: v})
		a.activate(col, at, true)
	}
	a.rememberTabs()
	a.invalidate()
}

// prActivity is the line over a pull request's tab, moving while it is
// read.
func (a *app) prActivity(c *ui.Context, v *diffView) {
	if pr := v.commit.pr; pr.opening || pr.viewing && pr.view == nil {
		ui.Progress(c.Key("pr-activity"), -1).Height(2).Radius(0).Label(locales.GhReadingIssue(v.name()))
		return
	}
	ui.Box(c.Key("pr-activity")).Height(2)
}

// prSkeleton is the shapes of a pull request's changes while it is read:
// its card, then a file's lines.
func (a *app) prSkeleton(c *ui.Context) {
	bar := a.pal.text.Alpha(0.07)
	ui.Column(c.Key("pr-skeleton")).FillWidth().Padding(16, cardPadX+diffRailW, 16, cardPadX+diffRailW).Gap(12).Children(func() {
		ui.Box(c).WidthPercent(55).Height(16).Radius(5).Background(bar)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(48, 14).Radius(5).Background(bar)
			ui.Box(c).WidthPercent(30).Height(8).Radius(4).Background(bar)
		})
		ui.Box(c).FillWidth().Height(22).Radius(6).Background(bar).Margin(12, 0, 0, 0)
		for _, w := range []float32{62, 45, 80, 38, 70, 52} {
			ui.Box(c).WidthPercent(w).Height(8).Radius(4).Background(bar)
		}
	})
}

// readPR reads, once, what GitHub has of the pull request of a tab kept
// from before: its title, whether its head moved, and the files viewed.
func (a *app) readPR(v *diffView) {
	pr := v.commit.pr
	ctx := a.diffContext(v)
	r := ctx.git.repo
	if pr.read || pr.reading || r == nil || !a.online() {
		return
	}
	pr.reading = true
	n, head := pr.number, v.commit.hash
	bg.Go(func() {
		defer recoverCrash()
		info, err := r.PullRequestInfo(n)
		var viewed map[string]bool
		if err == nil {
			viewed, _ = r.PRViewed(info.ID)
		}
		a.post(func() {
			pr.reading = false
			if errors.Is(err, remote.ErrOffline) {
				return
			}
			pr.read = true
			if err != nil {
				pr.err = err.Error()
				v.changed()
				return
			}
			pr.info, pr.newer = info, info.HeadOid != head
			if viewed != nil {
				pr.viewed, pr.viewedOK = viewed, true
				foldViewed(v)
			}
			v.tree.gen++
			v.changed()
		})
	})
}

// foldViewed folds the files of a pull request's diff marked viewed, as
// GitHub does.
func foldViewed(v *diffView) {
	pr := v.commit.pr
	for _, f := range v.files {
		if f.prPath != "" && pr.viewed[f.prPath] {
			f.folded = true
		}
	}
}

// fileViewed reports whether a file of a pull request's diff is marked
// viewed.
func (v *diffView) fileViewed(f *diffFile) bool {
	return v.commit != nil && v.commit.pr != nil && f.prPath != "" && v.commit.pr.viewed[f.prPath]
}

// viewedCount is how many of the files of a pull request's diff are marked
// viewed.
func (v *diffView) viewedCount() int {
	n := 0
	for _, f := range v.files {
		if v.fileViewed(f) {
			n++
		}
	}
	return n
}

// toggleViewed marks a file of a pull request viewed on GitHub, which
// folds it, or not, which unfolds it. The mark shows at once, and goes
// back when GitHub refuses it.
func (a *app) toggleViewed(v *diffView, f *diffFile) {
	pr := v.commit.pr
	r := a.diffContext(v).git.repo
	if pr == nil || pr.info == nil || !pr.viewedOK || f.prPath == "" || pr.marking[f.prPath] || r == nil {
		return
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return
	}
	path, id := f.prPath, pr.info.ID
	viewed := !pr.viewed[path]
	if pr.viewed == nil {
		pr.viewed = map[string]bool{}
	}
	if pr.marking == nil {
		pr.marking = map[string]bool{}
	}
	set := func(on bool) {
		if on {
			pr.viewed[path] = true
		} else {
			delete(pr.viewed, path)
		}
		f.folded = on
		v.tree.gen++
		v.changed()
	}
	set(viewed)
	pr.marking[path] = true
	bg.Go(func() {
		defer recoverCrash()
		err := r.SetPRViewed(id, path, viewed)
		a.post(func() {
			delete(pr.marking, path)
			if err != nil {
				set(!viewed)
				a.fail("%s", locales.PrViewedFailed(err.Error()))
			}
		})
	})
}

// reloadPR opens the pull request of a diff again, at its head on GitHub.
func (a *app) reloadPR(v *diffView) {
	if pr := v.commit.pr; pr != nil {
		a.diffContext(v).openPullRequestWith(pr.number, prSamePage, false, false, "")
	}
}

// openPRInBrowser opens the pull request of a diff on GitHub.
func (a *app) openPRInBrowser(v *diffView) {
	if pr := v.commit.pr; pr != nil && pr.info != nil && pr.info.URL != "" {
		a.services.OpenURL(pr.info.URL)
	}
}
