package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// gitState is what the window knows of the Git work tree its folder is
// in: the files that differ from HEAD, the branch, and the commands
// running.
type gitState struct {
	repo *git.Repo // nil outside a work tree
	// looked is set once the folder was looked up, looking while it is.
	looked, looking bool
	lookup          *int // the lookup running, which a reset drops

	status   *git.Status
	branches []string // the local branches, read with the status
	remotes  []string // the remotes, read with the status
	// remotesStale has the next read of the status read the remotes too.
	remotesStale bool
	// entries are the files that differ, by path; dirs the folders above
	// them, with the letter of the one that matters most.
	entries map[string]git.Entry
	dirs    map[string]byte
	// groups are the files of status in each group of the source control
	// view, worked out for the status groupsOf.
	groups   [groupCount][]git.Entry
	groupsOf *git.Status
	// mergeDone are the files of the command in progress that the merge
	// view staged as resolved, by path, and mergeDoneOp the command they
	// belong to: a list of the files in conflict has them resolved, until
	// the command ends or another starts.
	mergeDone   map[string]bool
	mergeDoneOp git.Op
	// stale has the next frame read the status again, unless running;
	// read is when it was read last.
	stale, running bool
	read           time.Time

	// meta is when the files Git changes as HEAD moves last changed, and
	// checked when the app last looked; metaRunning is set while a remote
	// work tree is looked at.
	meta, checked time.Time
	metaRunning   bool
	// refsMeta is when the refs last changed, and refsGen counts the
	// changes, for the history to be read again.
	refsMeta time.Time
	refsGen  int

	// busy names the command running in the background, as "Pushing…",
	// which the status bar shows; "" while none. pending counts the
	// commands running, named or not.
	busy    string
	pending int
	// writing is set while the AI writes the commit message.
	writing *aiWriting
	// message is the commit message being written, and amend set while
	// it is to amend the last commit.
	message string
	amend   bool

	// history is the last commits of HEAD, newest first, and unpushed
	// those its upstream lacks, read for the state historyOf names;
	// historyN is how many to read, historyGen counts the reads.
	history        []git.Commit
	unpushed       map[string]bool
	historyOf      string
	historyN       int
	historyGen     int
	loadingHistory bool
	// graph is the lanes of history, laid out for the read graphOf
	// counts.
	graph   []graphRow
	graphOf int
	// tags are the names of the tags, read with the history.
	tags []string
	// reach is the commits of history that HEAD has, worked out for the
	// HEAD and the read reachOf names.
	reach   map[string]bool
	reachOf string
}

// historyPage is how many commits the history reads at first, and more
// each time more are asked for.
const historyPage = 50

// statusEvery is how often the status is read again on its own, for the
// files changed in folders the tree does not watch.
const statusEvery = 5 * time.Second

// lookUpRepo finds the work tree of the folder in the background.
func (a *app) lookUpRepo() {
	g := &a.git
	if a.root == "" || g.looked || g.looking || !a.online() {
		return
	}
	g.looking = true
	root := a.root
	// The lookup, which a folder opened anew, the same one or not, drops:
	// it found what was there before.
	tok := new(int)
	g.lookup = tok
	bg.Go(func() {
		defer recoverCrash()
		r, err := git.Open(root)
		a.post(func() {
			if a.root != root || g.lookup != tok {
				return
			}
			g.looking, g.looked = false, true
			if err == nil {
				g.repo, g.stale, g.remotesStale = r, true, true
			}
		})
	})
}

// refreshGit runs once a frame: it looks up the work tree, notices HEAD
// moving, and reads the status again when it may have changed.
func (a *gitContext) refreshGit() {
	if !a.live() {
		return
	}
	g := a.git
	if g.repo == nil || !a.app.online() {
		return
	}
	switch {
	case a.app.conn != nil:
		// On the host, at one round trip, in the background.
		if !g.metaRunning && time.Since(g.checked) >= remoteMetaEvery {
			g.checked, g.metaRunning = time.Now(), true
			r := g.repo
			bg.Go(func() {
				defer recoverCrash()
				t, refs, err := remoteGitMeta(r)
				a.app.post(func() {
					if g.repo != r {
						return
					}
					g.metaRunning = false
					if err == nil {
						a.tookGitMeta(t, refs)
					}
				})
			})
		}
	case time.Since(g.checked) >= 1500*time.Millisecond:
		g.checked = time.Now()
		a.tookGitMeta(gitMeta(g.repo))
	}
	a.refreshHistory()
	if g.running || !g.stale && time.Since(g.read) < statusEvery {
		return
	}
	g.running, g.stale = true, false
	r := g.repo
	// The remotes change seldom: they are read again with the status after
	// a command, or as HEAD moves, not every few seconds.
	readRemotes := g.remotesStale
	g.remotesStale = false
	bg.Go(func() {
		defer recoverCrash()
		st, err := r.Status()
		branches, _ := r.Branches()
		var remotes []string
		var remotesErr error
		if readRemotes {
			remotes, remotesErr = r.Remotes()
		}
		a.app.post(func() {
			if g.repo != r {
				return
			}
			g.running, g.read = false, time.Now()
			if err != nil {
				return
			}
			a.setStatus(st)
			g.branches = branches
			if readRemotes && remotesErr == nil {
				g.remotes = remotes
			}
		})
	})
}

// historyKey names the state the history is read for: where HEAD and its
// upstream are, and how many commits are asked for.
func (a *gitContext) historyKey() string {
	g := a.git
	st := g.status
	if st == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%t|%d", st.Head, st.Upstream, st.Ahead, st.Behind, max(g.historyN, historyPage),
		len(g.remotes), a.app.settings.ScmAllBranches, g.refsGen)
}

// refreshHistory reads the last commits again in the background when HEAD
// or its upstream moved, or more are asked for.
func (a *gitContext) refreshHistory() {
	if !a.live() {
		return
	}
	g := a.git
	st := g.status
	if g.repo == nil || st == nil || g.loadingHistory {
		return
	}
	n := max(g.historyN, historyPage)
	key := a.historyKey()
	if key == g.historyOf {
		return
	}
	g.loadingHistory = true
	// Without a remote nothing is pushed or not: no commit is marked.
	r, remotes, all := g.repo, len(g.remotes) > 0, a.app.settings.ScmAllBranches
	bg.Go(func() {
		defer recoverCrash()
		cs, err := r.Log(n, all)
		var unpushed map[string]bool
		if remotes {
			unpushed = r.Unpushed(n)
		}
		tags, _ := r.Tags()
		a.app.post(func() {
			if g.repo != r {
				return
			}
			g.loadingHistory = false
			g.historyOf = key
			if err != nil {
				return
			}
			g.history, g.unpushed, g.tags = cs, unpushed, tags
			g.historyGen++
		})
	})
}

// tookGitMeta takes when the files HEAD moving changes last changed, and
// the refs: as they change, the documents compare their lines again, and
// the history is read again.
func (a *gitContext) tookGitMeta(t, refs time.Time) {
	g := a.git
	if !t.Equal(g.meta) {
		a.app.repositories.dirty = true
		if !g.meta.IsZero() {
			a.gitChanged()
		}
		g.meta = t
	}
	if !refs.Equal(g.refsMeta) {
		if !g.refsMeta.IsZero() {
			g.refsGen++
		}
		g.refsMeta = refs
	}
}

// remoteMetaEvery is how often a remote work tree is looked at for HEAD
// moving.
const remoteMetaEvery = 3 * time.Second

// remoteGitMeta is gitMeta of a remote work tree, which stats the files
// and the folders of the refs on its host, at one round trip.
func remoteGitMeta(r *git.Repo) (head, refs time.Time, err error) {
	c, dir, ok := remote.Lookup(r.Dir)
	_, common, _ := remote.Split(r.Common)
	if !ok {
		return head, refs, errors.New("not remote")
	}
	heads := []string{dir + "/index", dir + "/HEAD", dir + "/logs/HEAD", common + "/FETCH_HEAD", common + "/config", hostPath(r.Root) + "/.gitmodules"}
	infos, err := c.StatUnder(append(slices.Clone(heads), common+"/packed-refs"), common+"/refs")
	if err != nil {
		return head, refs, err
	}
	for p, info := range infos {
		t := info.ModTime()
		if slices.Contains(heads, p) {
			if t.After(head) {
				head = t
			}
		} else if t.After(refs) {
			refs = t
		}
	}
	return head, refs, nil
}

// gitMeta returns when the files that HEAD moving changes last changed:
// the index, HEAD and its log, and what a fetch writes; and when the refs
// did, as a branch or tag is made, moved or deleted.
func gitMeta(r *git.Repo) (head, refs time.Time) {
	for _, p := range []string{
		filepath.Join(r.Dir, "index"), filepath.Join(r.Dir, "HEAD"), filepath.Join(r.Dir, "logs", "HEAD"),
		filepath.Join(r.Common, "FETCH_HEAD"), filepath.Join(r.Common, "config"), r.Abs(".gitmodules"),
	} {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(head) {
			head = info.ModTime()
		}
	}
	later := func(p string) {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(refs) {
			refs = info.ModTime()
		}
	}
	later(filepath.Join(r.Common, "packed-refs"))
	// A ref written goes in by a rename, which changes its folder's time.
	filepath.WalkDir(filepath.Join(r.Common, "refs"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			later(p)
		}
		return nil
	})
	return head, refs
}

// setStatus takes a status read, and the folders above its files. A
// status that says what the last did is dropped, so that what is worked
// out from it, from the rows of source control to those of the diff, is
// kept.
func (a *gitContext) setStatus(st *git.Status) {
	g := a.git
	if g.status.Equal(st) {
		return
	}
	g.status = st
	g.entries = make(map[string]git.Entry, len(st.Entries))
	g.dirs = map[string]byte{}
	for _, e := range st.Entries {
		g.entries[e.Path] = e
		l := e.Letter()
		for d := filepath.Dir(e.Path); len(d) >= len(g.repo.Root); d = filepath.Dir(d) {
			if old, ok := g.dirs[d]; ok && letterRank(old) >= letterRank(l) {
				break
			}
			g.dirs[d] = l
			if d == filepath.Dir(d) {
				break
			}
		}
	}
}

// letterRank orders the letters of files by how much a folder above them
// shows it: a conflict before a change before a file added.
func letterRank(l byte) int {
	switch l {
	case 'C':
		return 4
	case 'M', 'R', 'T', 'D':
		return 3
	case 'A':
		return 2
	case 'U':
		return 1
	}
	return 0
}

// branch is the branch checked out, or the commit when HEAD is detached,
// "" outside a work tree.
func (a *gitContext) branch() string {
	st := a.git.status
	switch {
	case st == nil:
		return ""
	case st.Branch != "":
		return st.Branch
	case st.Head != "":
		return git.ShortHash(st.Head)
	}
	return ""
}

// gitChanged has the documents read their bases again, and the status be
// read again, as when a commit may have been made.
func (a *gitContext) gitChanged() {
	if !a.live() {
		return
	}
	a.invalidateScmPanels()
	for _, d := range a.app.docs {
		if a.owns(d.path) {
			d.git.HeadMoved()
		}
	}
	a.app.diffsChanged(a.git.repo.Root)
	a.git.stale, a.git.remotesStale = true, true
	for _, ctx := range a.app.repositories.contexts {
		if ctx.git.repo != nil && a.git.repo != nil && ctx.git.repo.Root != a.git.repo.Root && ctx.git.repo.Contains(a.git.repo.Root) {
			ctx.git.stale = true
		}
	}
}

// gitDo runs a command on the work tree in the background, naming it
// busy while it runs when busy is set, then reads the status again and
// calls then, unless it failed: then it says why.
func (a *gitContext) gitDo(busy string, fn func(r *git.Repo) error, then func()) {
	if !a.live() {
		return
	}
	g := a.git
	r := g.repo
	if r == nil {
		return
	}
	if !a.app.online() {
		a.app.fail("%s", locales.NotConnectedTo(a.app.conn.Host))
		return
	}
	if busy != "" {
		if g.busy != "" {
			return
		}
		g.busy = busy
	}
	g.pending++
	bg.Go(func() {
		err := fn(r)
		a.app.post(func() {
			if g.repo != r {
				return
			}
			g.pending--
			if busy != "" {
				g.busy = ""
			}
			a.gitChanged()
			if err != nil {
				a.gitFailed(err)
				return
			}
			if then != nil {
				then()
			}
		})
	})
}

// gitFailed says why a git command failed.
func (a *gitContext) gitFailed(err error) {
	var occupied *git.WorktreeInUse
	if errors.As(err, &occupied) {
		a.askOpenWorktree(occupied.Branch, occupied.Path)
		return
	}
	msg := err.Error()
	if len(msg) > 400 {
		msg = msg[:400] + "…"
	}
	if git.NeedsAuth(err) {
		msg += "\n" + locales.GitNeedsAuth()
	}
	a.app.fail("%s", msg)
}

// fetch fetches from the remotes, which tells how far behind the branch
// is.
func (a *gitContext) fetch() {
	a.gitDo(locales.BusyFetching(), func(r *git.Repo) error { return r.Fetch() }, nil)
}

// pull brings in the upstream's commits, when the branch has none of its
// own that it lacks.
func (a *gitContext) pull() {
	pulled := 0
	a.gitDo(locales.BusyPulling(), func(r *git.Repo) error {
		before := r.Head()
		if err := r.Pull(); err != nil {
			return err
		}
		if before != "" {
			pulled = r.Count(before, "HEAD")
		}
		return nil
	}, func() {
		if pulled > 0 {
			a.app.tell(note{message: locales.PulledCommits(pulled)})
		} else {
			a.app.tell(note{message: locales.AlreadyUpToDate()})
		}
	})
}

// push pushes the branch to its upstream, or to a remote, which becomes
// its upstream.
func (a *gitContext) push() {
	st := a.git.status
	if st == nil || st.Branch == "" {
		a.app.fail("%s", locales.CheckOutToPush())
		return
	}
	branch, upstream := st.Branch, st.Upstream != ""
	a.gitDo(locales.BusyPushing(), func(r *git.Repo) error { return r.Push(branch, upstream) }, func() {
		if upstream {
			a.app.tell(note{message: locales.PushedTo(branch, st.Upstream)})
		} else {
			a.app.tell(note{message: locales.Published(branch)})
		}
	})
}

// gitSync pulls and pushes in one background operation, keeping the
// repository busy until both finish. A branch without an upstream is
// published by the existing push command.
func (a *gitContext) gitSync() {
	if !a.gitSyncVisible() {
		return
	}
	if a.git.status.Upstream == "" {
		a.push()
		return
	}
	upstream, ahead, pulled := a.git.status.Upstream, a.git.status.Ahead, 0
	a.gitDo(locales.BusySyncing(), func(r *git.Repo) error {
		before := r.Head()
		if err := r.Sync(); err != nil {
			return err
		}
		if before != "" {
			pulled = r.Count(before, "HEAD")
		}
		return nil
	}, func() {
		if pulled == 0 && ahead == 0 {
			a.app.tell(note{message: locales.AlreadyUpToDate()})
		} else {
			a.app.tell(note{message: locales.SyncedWith(upstream)})
		}
	})
}

// switchBranch checks out a branch, or makes it first when create is set,
// once the documents are saved: Git keeps the changes of the work tree
// when they do not clash with the branch's.
func (a *gitContext) switchBranch(branch string, create bool) {
	if !a.docsSaved(locales.BeforeSwitching()) {
		return
	}
	a.gitDo(locales.BusySwitchingTo(branch), func(r *git.Repo) error { return r.Switch(branch, create) }, nil)
}

// docsSaved reports whether the documents of the work tree are saved,
// saying which is not when one is not, before what doing names.
func (a *gitContext) docsSaved(doing string) bool {
	if !a.live() {
		return false
	}
	for _, d := range a.app.docs {
		if d.dirty() && d.path != "" && a.git.repo != nil && a.git.repo.Contains(d.path) {
			a.app.fail("%s", locales.SaveBefore(d.name(), doing))
			return false
		}
	}
	return true
}

// checkOutCommit checks out a commit of the history: its branch when it
// has one local branch, or else the commit itself, once asked, as that
// detaches HEAD.
func (a *gitContext) checkOutCommit(cm git.Commit) {
	if b := localBranch(cm); b != "" {
		a.switchBranch(b, false)
		return
	}
	a.app.confirm = &confirm{title: locales.CheckOutQuestion(cm.Short()), ok: locales.CheckOutButton(),
		message: locales.CheckOutDetached(),
		run: func() {
			if !a.docsSaved(locales.BeforeCheckout()) {
				return
			}
			hash := cm.Hash
			a.gitDo(locales.BusyCheckingOut(cm.Short()), func(r *git.Repo) error { return r.Checkout(hash) }, nil)
		}}
}

// localBranch is the one local branch at a commit, "" when it has none
// or more.
func localBranch(cm git.Commit) string {
	name := ""
	for _, r := range cm.Refs {
		if r.Kind == git.RefBranch {
			if name != "" {
				return ""
			}
			name = r.Name
		}
	}
	return name
}

// opArg is op as the messages of the catalogs select it.
func opArg(op git.Op) string {
	if op == git.OpCherryPick {
		return "cherrypick"
	}
	return string(op)
}

// opTitle names op as a title, "Cherry-Pick", in the language shown.
func opTitle(op git.Op) string { return locales.OpTitle(opArg(op)) }

// opNoun names op inside a sentence, "cherry-pick", in the language shown.
func opNoun(op git.Op) string { return locales.OpNoun(opArg(op)) }

// revOf names a commit for a command: by its one local branch, so that
// a merge's message names the branch, or else by its hash.
func revOf(cm git.Commit) string { return cmp.Or(localBranch(cm), cm.Hash) }

// inHead reports whether HEAD has commit hash, by the commits of the
// history read: one read before them counts as not.
func (a *gitContext) inHead(hash string) bool {
	g := a.git
	if g.status == nil || g.status.Head == "" {
		return false
	}
	key := fmt.Sprintf("%s|%d", g.status.Head, g.historyGen)
	if g.reachOf != key {
		parents := make(map[string][]string, len(g.history))
		for _, c := range g.history {
			parents[c.Hash] = c.Parents
		}
		g.reach, g.reachOf = map[string]bool{}, key
		todo := []string{g.status.Head}
		for len(todo) > 0 {
			h := todo[len(todo)-1]
			todo = todo[:len(todo)-1]
			if g.reach[h] {
				continue
			}
			g.reach[h] = true
			todo = append(todo, parents[h]...)
		}
	}
	return g.reach[hash]
}

// runOp runs a command that moves HEAD by commits, once the documents
// are saved, then says done. When it stops for conflicts it says how to
// go on, and the message it proposes goes in the box.
func (a *gitContext) runOp(op git.Op, busy, done string, fn func(r *git.Repo) error) {
	if !a.docsSaved(locales.BeforeOp(opNoun(op))) {
		return
	}
	stopped, msg := false, ""
	a.gitDo(busy, func(r *git.Repo) error {
		err := fn(r)
		if err != nil && r.InProgress() == op {
			stopped, msg = true, r.MergeMessage()
			return nil
		}
		return err
	}, func() {
		g := a.git
		g.refsGen++
		if !stopped {
			a.app.tell(note{message: done})
			return
		}
		if op != git.OpRebase && strings.TrimSpace(g.message) == "" {
			g.message, g.amend = msg, false
		}
		a.app.showSourceControl()
		a.app.openConflicts()
		a.app.tell(note{message: locales.OpStopped(opTitle(op)),
			label: locales.ButtonAbort(), action: func() { a.abortOpOf(op) }})
	})
}

// mergeCommit merges a commit of the history into the branch checked
// out, once asked.
func (a *gitContext) mergeCommit(cm git.Commit) {
	rev, into := revOf(cm), a.branch()
	a.app.confirm = &confirm{title: locales.MergeQuestion(cmp.Or(localBranch(cm), cm.Short()), into), ok: locales.MergeButton(),
		message: ellipsize(cm.Subject, 80),
		run: func() {
			a.runOp(git.OpMerge, locales.BusyMerging(), locales.MergedInto(cmp.Or(localBranch(cm), cm.Short()), into),
				func(r *git.Repo) error { return r.Merge(rev) })
		}}
}

// rebaseOnto moves the commits of the branch checked out that a commit
// of the history lacks on top of it, once asked.
func (a *gitContext) rebaseOnto(cm git.Commit) {
	rev, branch, onto := revOf(cm), a.branch(), cmp.Or(localBranch(cm), cm.Short())
	a.app.confirm = &confirm{title: locales.RebaseQuestion(branch, onto), ok: locales.RebaseButton(),
		message: locales.RebaseDetail(branch, onto),
		run: func() {
			a.runOp(git.OpRebase, locales.BusyRebasing(), locales.RebasedOnto(branch, onto),
				func(r *git.Repo) error { return r.Rebase(rev) })
		}}
}

// cherryPick makes a commit of a commit's changes on the branch checked
// out.
func (a *gitContext) cherryPick(cm git.Commit) {
	hash, merge := cm.Hash, len(cm.Parents) > 1
	a.runOp(git.OpCherryPick, locales.BusyCherryPicking(), locales.CherryPicked(ellipsize(cm.Subject, 48)),
		func(r *git.Repo) error { return r.CherryPick(hash, merge) })
}

// revertCommit makes a commit that takes back a commit's changes, once
// asked.
func (a *gitContext) revertCommit(cm git.Commit) {
	hash, merge := cm.Hash, len(cm.Parents) > 1
	msg := locales.RevertDetail(ellipsize(cm.Subject, 60))
	if merge {
		msg += " " + locales.RevertMergeDetail()
	}
	a.app.confirm = &confirm{title: locales.RevertQuestion(cm.Short()), ok: locales.RevertButton(), message: msg, run: func() {
		a.runOp(git.OpRevert, locales.BusyReverting(), locales.Reverted(ellipsize(cm.Subject, 48)),
			func(r *git.Repo) error { return r.Revert(hash, merge) })
	}}
}

// resetModes are the ways a reset may go, in the order resetChoices offers
// them.
var resetModes = []git.ResetMode{git.ResetSoft, git.ResetMixed, git.ResetHard}

// resetChoices are the ways a reset may go, as the dialog offers them.
func resetChoices() []confirmChoice {
	return []confirmChoice{
		{label: locales.ResetSoft(), message: locales.ResetSoftDetail()},
		{label: locales.ResetMixed(), message: locales.ResetMixedDetail()},
		{label: locales.ResetHard(), message: locales.ResetHardDetail(), danger: true},
	}
}

// resetTo moves the branch checked out to a commit of the history, in
// the mode chosen in the dialog.
func (a *gitContext) resetTo(cm git.Commit) {
	branch, hash := a.branch(), cm.Hash
	choices := resetChoices()
	cf := &confirm{title: a.repositoryQuestion(locales.ResetQuestion(branch, cm.Short())), ok: locales.ResetButton(),
		choices: choices, choice: 1}
	cf.run = func() {
		mode := resetModes[cf.choice]
		if mode == git.ResetHard && !a.docsSaved(locales.BeforeHardReset()) {
			return
		}
		a.gitDo(locales.BusyResetting(), func(r *git.Repo) error { return r.Reset(hash, mode) }, func() {
			a.git.refsGen++
			a.app.tell(note{message: locales.ResetDone(branch, git.ShortHash(hash), strings.ToLower(choices[cf.choice].label))})
		})
	}
	a.app.confirm = cf
}

// continueOp goes on with the command stopped for conflicts, once they
// are resolved and staged: a merge, cherry-pick or revert with the
// message written, when one is.
func (a *gitContext) continueOp() {
	g := a.git
	if g.status == nil || g.status.Op == "" || g.busy != "" {
		return
	}
	op := g.status.Op
	if len(a.groupEntries()[groupMerge]) > 0 {
		a.app.fail("%s", locales.ResolveConflictsFirst())
		return
	}
	if op != git.OpRebase && strings.TrimSpace(g.message) != "" {
		a.commit(false)
		return
	}
	if !a.docsSaved(locales.BeforeContinuing()) {
		return
	}
	stopped := false
	a.gitDo(locales.BusyContinuing(opNoun(op)), func(r *git.Repo) error {
		err := r.Continue(op)
		if err != nil && r.InProgress() == op {
			// A rebase stops again at the next commit that conflicts.
			if st, serr := r.Status(); serr == nil && slices.ContainsFunc(st.Entries, git.Entry.Conflicted) {
				stopped = true
				return nil
			}
		}
		return err
	}, func() {
		g.refsGen++
		if stopped {
			a.app.tell(note{message: locales.OpStoppedAgain(opTitle(op)),
				label: locales.ButtonAbort(), action: func() { a.abortOpOf(op) }})
			return
		}
		a.app.tell(note{message: locales.OpDone(opTitle(op))})
	})
}

// abortOp undoes the command stopped for conflicts, once asked.
func (a *gitContext) abortOp() {
	if st := a.git.status; st != nil && st.Op != "" {
		a.abortOpOf(st.Op)
	}
}

// abortOpOf undoes op, stopped for conflicts, once asked.
func (a *gitContext) abortOpOf(op git.Op) {
	g := a.git
	a.app.confirm = &confirm{title: locales.AbortQuestion(opNoun(op)), ok: locales.ButtonAbort(), danger: true,
		message: locales.AbortDetail(),
		run: func() {
			a.gitDo(locales.BusyAborting(), func(r *git.Repo) error { return r.Abort(op) }, func() {
				g.refsGen++
				if op != git.OpRebase {
					g.message = ""
				}
			})
		}}
}

// askRefAt asks for the name of a branch, or a tag, to make at a commit
// of the history, without checking it out.
func (a *gitContext) askRefAt(cm git.Commit, tag bool) {
	g := a.git
	if g.repo == nil {
		return
	}
	title, names := locales.CreateBranchEllipsis(), g.branches
	if tag {
		title, names = locales.CreateTagEllipsis(), g.tags
	}
	isTag := strconv.FormatBool(tag)
	hash := cm.Hash
	a.app.prompt = &prompt{title: title, hint: locales.RefAt(cm.Short(), ellipsize(cm.Subject, 32)), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " ~^:?*[\\") {
			return locales.RefNameNoSpaces(isTag)
		}
		if slices.Contains(names, name) {
			return locales.RefExists(isTag)
		}
		a.gitDo(locales.BusyCreatingRef(isTag, name), func(r *git.Repo) error {
			if tag {
				return r.CreateTag(name, hash, "")
			}
			return r.CreateBranch(name, hash)
		}, func() {
			g.refsGen++
			a.app.tell(note{message: locales.CreatedRef(isTag, name, git.ShortHash(hash))})
		})
		return ""
	}}
}

// askNewBranch asks for the name of a branch to make and check out.
func (a *gitContext) askNewBranch() {
	if !a.live() || a.git.busy != "" {
		return
	}
	defer a.bindPanelDialogs(a.git.repo)
	a.app.prompt = &prompt{title: locales.CreateBranchEllipsis(), hint: locales.BranchFrom(a.branch()), done: func(name string) string {
		if a.git.busy != "" {
			return a.git.busy
		}
		name = strings.TrimSpace(name)
		if !validBranch(name) {
			return locales.BranchInvalidName()
		}
		if slices.Contains(a.git.branches, name) {
			return locales.RefExists("false")
		}
		a.switchBranch(name, true)
		return ""
	}}
}

// gitSyncVisible keeps publishing and syncing off detached and unborn
// branches, and off repositories with no upstream or remote.
func (a *gitContext) gitSyncVisible() bool {
	st := a.git.status
	return a.git.repo != nil && st != nil && st.Branch != "" && st.Head != "" &&
		(st.Upstream != "" || len(a.git.remotes) > 0)
}

// gitSyncLabel keeps both counts when either is nonzero; an up-to-date
// branch has only the sync icon, which can still fetch new commits.
func (a *gitContext) gitSyncLabel() string {
	if st := a.git.status; st != nil && st.Upstream != "" && (st.Behind > 0 || st.Ahead > 0) {
		return fmt.Sprintf("%d↓ %d↑", st.Behind, st.Ahead)
	}
	return ""
}
