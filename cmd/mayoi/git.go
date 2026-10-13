package main

import (
	"errors"
	"fmt"
	"mayoi/internal/remotefs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
)

// gitState is what the window knows of the Git work tree its folder is
// in: the files that differ from HEAD, the branch, and the commands
// running.
type gitState struct {
	repo *git.Repo // nil outside a work tree
	// looked is set once the folder was looked up, looking while it is.
	looked, looking bool

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
	// stale has the next frame read the status again, unless running;
	// read is when it was read last.
	stale, running bool
	read           time.Time

	// meta is when the files Git changes as HEAD moves last changed, and
	// checked when the app last looked.
	meta, checked time.Time
	// refsMeta is when the refs last changed, and refsGen counts the
	// changes, for the history to be read again.
	refsMeta time.Time
	refsGen  int

	// busy names the command running in the background, as "Pushing…",
	// which the status bar shows; "" while none. pending counts the
	// commands running, named or not.
	busy    string
	pending int
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
	if a.root == "" || g.looked || g.looking {
		return
	}
	g.looking = true
	root := a.root
	go func() {
		r, err := git.Open(root)
		a.post(func() {
			if a.root != root {
				return
			}
			g.looking, g.looked = false, true
			if err == nil {
				g.repo, g.stale, g.remotesStale = r, true, true
			}
		})
	}()
}

// refreshGit runs once a frame: it looks up the work tree, notices HEAD
// moving, and reads the status again when it may have changed.
func (a *app) refreshGit() {
	a.lookUpRepo()
	g := &a.git
	if g.repo == nil {
		return
	}
	if !remotefs.IsRemote(a.root) && time.Since(g.checked) >= 1500*time.Millisecond {
		g.checked = time.Now()
		t, refs := gitMeta(g.repo)
		if !t.Equal(g.meta) {
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
	go func() {
		st, err := r.Status()
		branches, _ := r.Branches()
		var remotes []string
		var remotesErr error
		if readRemotes {
			remotes, remotesErr = r.Remotes()
		}
		a.post(func() {
			g.running, g.read = false, time.Now()
			if g.repo != r {
				return
			}
			if err != nil {
				return
			}
			a.setStatus(st)
			g.branches = branches
			if readRemotes && remotesErr == nil {
				g.remotes = remotes
			}
		})
	}()
}

// historyKey names the state the history is read for: where HEAD and its
// upstream are, and how many commits are asked for.
func (a *app) historyKey() string {
	g := &a.git
	st := g.status
	if st == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%t|%d", st.Head, st.Upstream, st.Ahead, st.Behind, max(g.historyN, historyPage),
		len(g.remotes), a.settings.ScmAllBranches, g.refsGen)
}

// refreshHistory reads the last commits again in the background when HEAD
// or its upstream moved, or more are asked for.
func (a *app) refreshHistory() {
	g := &a.git
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
	r, remotes, all := g.repo, len(g.remotes) > 0, a.settings.ScmAllBranches
	go func() {
		cs, err := r.Log(n, all)
		var unpushed map[string]bool
		if remotes {
			unpushed = r.Unpushed(n)
		}
		tags, _ := r.Tags()
		a.post(func() {
			g.loadingHistory = false
			if g.repo != r {
				return
			}
			g.historyOf = key
			if err != nil {
				return
			}
			g.history, g.unpushed, g.tags = cs, unpushed, tags
			g.historyGen++
		})
	}()
}

// gitMeta returns when the files that HEAD moving changes last changed:
// the index, HEAD and its log, and what a fetch writes; and when the refs
// did, as a branch or tag is made, moved or deleted.
func gitMeta(r *git.Repo) (head, refs time.Time) {
	for _, p := range []string{
		filepath.Join(r.Dir, "index"), filepath.Join(r.Dir, "HEAD"), filepath.Join(r.Dir, "logs", "HEAD"),
		filepath.Join(r.Common, "FETCH_HEAD"),
	} {
		if info, err := remotefs.Stat(p); err == nil && info.ModTime().After(head) {
			head = info.ModTime()
		}
	}
	later := func(p string) {
		if info, err := remotefs.Stat(p); err == nil && info.ModTime().After(refs) {
			refs = info.ModTime()
		}
	}
	later(filepath.Join(r.Common, "packed-refs"))
	// A ref written goes in by a rename, which changes its folder's time.
	if remotefs.IsRemote(r.Root) {
		var visit func(string)
		visit = func(dir string) {
			later(dir)
			entries, err := remotefs.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				if entry.IsDir() {
					visit(filepath.Join(dir, entry.Name()))
				}
			}
		}
		visit(filepath.Join(r.Common, "refs"))
	} else {
		filepath.WalkDir(filepath.Join(r.Common, "refs"), func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				later(p)
			}
			return nil
		})
	}
	return head, refs
}

// setStatus takes a status read, and the folders above its files. A
// status that says what the last did is dropped, so that what is worked
// out from it, from the rows of source control to those of the diff, is
// kept.
func (a *app) setStatus(st *git.Status) {
	g := &a.git
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
func (a *app) branch() string {
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
func (a *app) gitChanged() {
	for _, d := range a.docs {
		d.git.HeadMoved()
	}
	a.diffsChanged()
	a.git.stale, a.git.remotesStale = true, true
}

// gitDo runs a command on the work tree in the background, naming it
// busy while it runs when busy is set, then reads the status again and
// calls then, unless it failed: then it says why.
func (a *app) gitDo(busy string, fn func(r *git.Repo) error, then func()) {
	g := &a.git
	r := g.repo
	if r == nil {
		return
	}
	if busy != "" {
		if g.busy != "" {
			return
		}
		g.busy = busy
	}
	g.pending++
	go func() {
		err := fn(r)
		a.post(func() {
			if a.git.repo != r {
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
	}()
}

// gitFailed says why a git command failed.
func (a *app) gitFailed(err error) {
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
		msg += "\nGit needs to sign in: run the command in the terminal once."
	}
	a.fail("%s", msg)
}

// fetch fetches from the remotes, which tells how far behind the branch
// is.
func (a *app) fetch() {
	a.gitDo("Fetching…", func(r *git.Repo) error { return r.Fetch() }, nil)
}

// pull brings in the upstream's commits, when the branch has none of its
// own that it lacks.
func (a *app) pull() {
	pulled := 0
	a.gitDo("Pulling…", func(r *git.Repo) error {
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
			a.tell(note{message: "Pulled " + plural(pulled, "commit")})
		} else {
			a.tell(note{message: "Already up to date"})
		}
	})
}

// push pushes the branch to its upstream, or to a remote, which becomes
// its upstream.
func (a *app) push() {
	st := a.git.status
	if st == nil || st.Branch == "" {
		a.fail("Check out a branch to push.")
		return
	}
	branch, upstream := st.Branch, st.Upstream != ""
	a.gitDo("Pushing…", func(r *git.Repo) error { return r.Push(branch, upstream) }, func() {
		if upstream {
			a.tell(note{message: "Pushed " + branch + " to " + st.Upstream})
		} else {
			a.tell(note{message: "Published " + branch})
		}
	})
}

// switchBranch checks out a branch, or makes it first when create is set,
// once the documents are saved: Git keeps the changes of the work tree
// when they do not clash with the branch's.
func (a *app) switchBranch(branch string, create bool) {
	if create {
		a.checkoutBranch(branch, true)
		return
	}
	var worktree string
	a.gitDo("Checking worktrees…", func(r *git.Repo) error {
		var err error
		worktree, err = r.WorktreeForBranch(branch)
		return err
	}, func() {
		if worktree != "" && branch != a.branch() {
			a.askOpenWorktree(branch, worktree)
			return
		}
		a.checkoutBranch(branch, false)
	})
}

func (a *app) checkoutBranch(branch string, create bool) {
	if !a.docsSaved("switching branches") {
		return
	}
	a.gitDo("Switching to "+branch+"…", func(r *git.Repo) error { return r.Switch(branch, create) }, nil)
}

// docsSaved reports whether the documents of the work tree are saved,
// saying which is not when one is not, before what doing names.
func (a *app) docsSaved(doing string) bool {
	for _, d := range a.docs {
		if d.dirty() && d.path != "" && a.git.repo != nil && a.git.repo.Contains(d.path) {
			a.fail("Save the changes of %s before %s.", d.name(), doing)
			return false
		}
	}
	return true
}

// checkOutCommit checks out a commit of the history: its branch when it
// has one local branch, or else the commit itself, once asked, as that
// detaches HEAD.
func (a *app) checkOutCommit(cm git.Commit) {
	var branches []string
	for _, r := range cm.Refs {
		if r.Kind == git.RefBranch {
			branches = append(branches, r.Name)
		}
	}
	if len(branches) == 1 {
		a.switchBranch(branches[0], false)
		return
	}
	a.confirm = &confirm{title: "Check out " + cm.Short() + "?", ok: "Check Out",
		message: "HEAD will be detached at the commit: commits made there belong to no branch until you create one.",
		run: func() {
			if !a.docsSaved("checking out a commit") {
				return
			}
			hash := cm.Hash
			a.gitDo("Checking out "+cm.Short()+"…", func(r *git.Repo) error { return r.Checkout(hash) }, nil)
		}}
}

// askRefAt asks for the name of a branch, or a tag, to make at a commit
// of the history, without checking it out.
func (a *app) askRefAt(cm git.Commit, tag bool) {
	g := &a.git
	if g.repo == nil {
		return
	}
	what, title, names := "branch", "Create Branch…", g.branches
	if tag {
		what, title, names = "tag", "Create Tag…", g.tags
	}
	hash := cm.Hash
	a.prompt = &prompt{title: title, hint: "at " + cm.Short() + " " + ellipsize(cm.Subject, 32), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " ~^:?*[\\") {
			return "Enter a " + what + " name without spaces."
		}
		if slices.Contains(names, name) {
			return "A " + what + " with that name exists."
		}
		a.gitDo("Creating "+what+" "+name+"…", func(r *git.Repo) error {
			if tag {
				return r.CreateTag(name, hash, "")
			}
			return r.CreateBranch(name, hash)
		}, func() {
			g.refsGen++
			a.tell(note{message: fmt.Sprintf("Created %s %s at %s", what, name, git.ShortHash(hash))})
		})
		return ""
	}}
}

// askNewBranch asks for the name of a branch to make and check out.
func (a *app) askNewBranch() {
	if a.git.repo == nil {
		return
	}
	a.prompt = &prompt{title: "Create Branch…", hint: "from " + a.branch(), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " ~^:?*[\\") {
			return "Enter a branch name without spaces."
		}
		if slices.Contains(a.git.branches, name) {
			return "A branch with that name exists."
		}
		a.switchBranch(name, true)
		return ""
	}}
}

// branchMenu is the menu of the branch in the status bar: the branches to
// switch to, and the commands of the remotes.
func (a *app) branchMenu(m *ui.Menu) {
	g := &a.git
	cur := a.branch()
	if m.Item("Switch Branch…").Disabled(g.busy != "").Chosen() {
		a.post(a.openBranchPicker)
	}
	m.Separator()
	for _, b := range g.branches {
		if m.Item(b).Checked(b == cur).Disabled(g.busy != "").Chosen() && b != cur {
			a.post(func() { a.switchBranch(b, false) })
		}
	}
	if len(g.branches) > 0 {
		m.Separator()
	}
	if m.Item("Create Branch…").Disabled(g.busy != "").Chosen() {
		a.post(a.askNewBranch)
	}
	m.Separator()
	if m.Item("Fetch").Disabled(g.busy != "").Chosen() {
		a.post(a.fetch)
	}
	if m.Item("Pull").Disabled(g.busy != "").Chosen() {
		a.post(a.pull)
	}
	if m.Item("Push").Disabled(g.busy != "").Chosen() {
		a.post(a.push)
	}
}

// branchLabel is what the status bar says of the branch: its name, and
// how far it is ahead of its upstream and behind it.
func (a *app) branchLabel() string {
	label := a.branch()
	if st := a.git.status; st != nil && st.Upstream != "" {
		if st.Behind > 0 {
			label += fmt.Sprintf(" ↓%d", st.Behind)
		}
		if st.Ahead > 0 {
			label += fmt.Sprintf(" ↑%d", st.Ahead)
		}
	}
	return label
}
