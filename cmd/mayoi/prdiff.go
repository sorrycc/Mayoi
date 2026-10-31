package main

import (
	"errors"
	"strconv"
	"strings"

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
}

// name is how the tab of a pull request is named: its number and title.
func (pr *prDiff) name() string {
	s := "#" + strconv.Itoa(pr.number)
	if pr.info != nil && pr.info.Title != "" {
		s += " " + pr.info.Title
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

// openPullRequest reads pull request n, or the branch's for 0, fetches its
// commits where they are missing, and shows its changes in a tab: those
// GitHub shows, from where its head left its base to its head.
func (a *gitContext) openPullRequest(n int) {
	r := a.git.repo
	if r == nil {
		return
	}
	var (
		info   *git.PRInfo
		from   string
		viewed map[string]bool
	)
	g := a.git
	a.gitRead(locales.BusyOpeningPR(), func(repo *git.Repo) (err error) {
		if info, err = repo.PullRequestInfo(n); err != nil {
			if errors.Is(err, git.ErrNoGH) {
				a.app.post(func() { g.ghOK = false })
			}
			if errors.Is(err, git.ErrNoPR) {
				if n == 0 {
					return errors.New(locales.PrNoneForBranch())
				}
				return errors.New(locales.PrNotFound("#" + strconv.Itoa(n)))
			}
			return err
		}
		if from, err = repo.FetchPullRequest(info); err != nil {
			return err
		}
		// Without the marks, the files show unmarked; the tab still opens.
		viewed, _ = repo.PRViewed(info.ID)
		return nil
	}, func() {
		if a.git.repo != r {
			return
		}
		g.ghOK = true
		a.app.showPullRequest(info, from, viewed, r.Root)
	})
}

// showPullRequest shows the changes of a pull request, from commit from to
// its head, in a tab: the tab of the pull request, made anew when its head
// moved, else one after the tab shown of the column with the keyboard.
func (a *app) showPullRequest(info *git.PRInfo, from string, viewed map[string]bool, root string) {
	key := prKey(info.Number, from, info.HeadOid)
	v := newCommitView(parseDiffLayout(a.settings.DiffLayout), key, git.Commit{})
	v.repoRoot = root
	pr := v.commit.pr
	pr.info, pr.read = info, true
	pr.viewed, pr.viewedOK = viewed, viewed != nil
	for _, col := range a.cols {
		if col.locked {
			continue
		}
		for i, t := range col.tabs {
			if t.diff == nil || t.diff.commit == nil || t.diff.commit.pr == nil || t.diff.commit.pr.number != info.Number || !a.diffBelongs(t.diff, root) {
				continue
			}
			if t.diff.commit.key() == key {
				// The same commits: what GitHub tells now.
				old := t.diff.commit.pr
				old.info, old.read, old.err, old.newer = info, true, "", false
				if viewed != nil {
					old.viewed, old.viewedOK = viewed, true
					foldViewed(t.diff)
				}
				t.diff.changed()
			} else {
				v.layout = t.diff.layout
				t.diff.stop()
				t.diff = v
			}
			a.keepTab(t)
			a.activate(col, i, true)
			a.rememberTabs()
			return
		}
	}
	col := a.fileColumn()
	at := min(col.active+1, len(col.tabs))
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, true)
	a.rememberTabs()
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
		a.diffContext(v).openPullRequest(pr.number)
	}
}

// openPRInBrowser opens the pull request of a diff on GitHub.
func (a *app) openPRInBrowser(v *diffView) {
	if pr := v.commit.pr; pr != nil && pr.info != nil && pr.info.URL != "" {
		a.services.OpenURL(pr.info.URL)
	}
}
