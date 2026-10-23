// Package workspace is the folder Mayoi has open: its file tree, read a
// directory at a time as the user opens them and kept up to date as files
// change on disk, and an index of its files for quick open.
package workspace

import (
	"bufio"
	"cmp"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// Hidden reports whether the file tree leaves an entry out.
func Hidden(name string) bool {
	switch name {
	case ".git", ".DS_Store", ".hg", ".svn", "Thumbs.db":
		return true
	}
	return false
}

// Skipped reports whether the index of files leaves out a directory.
func Skipped(name string) bool {
	return slices.Contains(skippedNames, name)
}

// skippedNames are the directories the index of files leaves out: build
// output and dependencies, and those the tree hides.
var skippedNames = []string{"node_modules", ".next", ".turbo", ".cache", "__pycache__", ".venv", "target", ".gradle",
	".git", ".DS_Store", ".hg", ".svn", "Thumbs.db"}

// Tree is the file tree of a folder. Its methods run on the main thread,
// except where they say otherwise; it reads directories in the background
// and hands their entries to the main thread with post.
type Tree struct {
	Root string

	post func(func()) // runs a function on the main thread

	children map[string][]string // a directory's entries, sorted
	dirs     map[string]bool     // the directories among entries
	loading  map[string]bool
	gen      int // bumped by every change of the tree

	watcher *fsnotify.Watcher
	watched map[string]bool
	// OnChange is called on the main thread with the files that changed
	// on disk, and those removed.
	OnChange func(changed, removed []string)
	// IsOpen reports whether the explorer shows a folder open: a remote
	// tree reads again only the folders that show. nil for all.
	IsOpen func(dir string) bool

	// A remote tree has no watcher: it reads the folders that show, and
	// stats the files Watch names, every PollEvery. files are those
	// files, with what stat said of each last, nil until it did once;
	// polling is set while a poll runs.
	remote  bool
	stamps  map[string]*stamp
	polling bool
	// notify is the inotifywait that watches a remote tree on its host,
	// while it runs, which spares the polls of the folders; noNotify is
	// set once it failed while the host was there.
	notify   *exec.Cmd
	noNotify bool
	// fullAgain has a full poll follow the one running.
	fullAgain bool
	// ahead are the folders of a remote tree read before they open: as a
	// folder is read, so are those in it, at one round trip, which then
	// open at once.
	ahead map[string]aheadList

	mu      sync.Mutex
	pending map[string]bool // directories to read again
	files   map[string]bool // files written
	gone    map[string]bool // paths removed
	timer   *bg.Timer
	closed  bool
	// idle is set while the tree's window is not in front.
	idle bool
}

// NewTree opens the tree of root. post runs a function on the main thread.
func NewTree(root string, post func(func())) *Tree {
	t := &Tree{
		Root: root, post: post,
		children: map[string][]string{}, dirs: map[string]bool{root: true}, loading: map[string]bool{},
		watched: map[string]bool{}, stamps: map[string]*stamp{},
	}
	if vfs.IsRemote(root) {
		t.remote = true
		go t.pollLoop()
		return t
	}
	if w, err := fsnotify.NewWatcher(); err == nil {
		t.watcher = w
		go t.watch()
	}
	return t
}

// Close stops watching the tree.
func (t *Tree) Close() {
	t.mu.Lock()
	t.closed = true
	if t.timer != nil {
		t.timer.Stop()
	}
	notify := t.notify
	t.mu.Unlock()
	if notify != nil && notify.Process != nil {
		notify.Process.Kill()
	}
	if t.watcher != nil {
		t.watcher.Close()
	}
}

// Gen changes whenever the tree does.
func (t *Tree) Gen() int { return t.gen }

// IsDir reports whether path is a directory of the tree.
func (t *Tree) IsDir(path string) bool { return t.dirs[path] }

// Loaded reports whether a directory's entries were read.
func (t *Tree) Loaded(dir string) bool {
	_, ok := t.children[dir]
	return ok
}

// Children returns a directory's entries, nil for a file and an empty
// slice for a directory not read yet: Load reads it.
func (t *Tree) Children(path string) []string {
	if !t.dirs[path] {
		return nil
	}
	if kids, ok := t.children[path]; ok {
		return kids
	}
	return []string{}
}

// aheadList is a folder of a remote tree read ahead, and when.
type aheadList struct {
	entries []vfs.Entry
	at      time.Time
}

// aheadMax is the most folders read ahead at once, and aheadFresh how
// long a folder read ahead is taken as it is: one older shows at once,
// and is read again.
const (
	aheadMax   = 48
	aheadFresh = 5 * time.Second
)

// Load reads a directory's entries in the background, unless they were.
func (t *Tree) Load(dir string) {
	if !t.dirs[dir] || t.loading[dir] {
		return
	}
	if _, ok := t.children[dir]; ok {
		return
	}
	if l, ok := t.ahead[dir]; ok {
		delete(t.ahead, dir)
		kids, dirs := sortEntries(dir, l.entries)
		t.set(dir, kids, dirs)
		if time.Since(l.at) > aheadFresh {
			t.reload(dir)
		} else {
			t.readAhead(dir)
		}
		return
	}
	t.reload(dir)
}

// ReadAhead reports whether a folder of a remote tree was read ahead,
// to open at once.
func (t *Tree) ReadAhead(dir string) bool {
	_, ok := t.ahead[dir]
	return ok
}

// readAhead reads the folders in dir of a remote tree, but those the
// index skips, as node_modules, in the background, for them to open at
// once.
func (t *Tree) readAhead(dir string) {
	if !t.remote {
		return
	}
	var dirs []string
	for _, k := range t.children[dir] {
		if _, read := t.children[k]; read || !t.dirs[k] || t.loading[k] || Skipped(filepath.Base(k)) {
			continue
		}
		if l, ok := t.ahead[k]; ok && time.Since(l.at) < aheadFresh {
			continue
		}
		dirs = append(dirs, k)
		if len(dirs) == aheadMax {
			break
		}
	}
	if len(dirs) == 0 {
		return
	}
	bg.Go(func() {
		lists, _, err := vfs.ReadDirs(dirs)
		if err != nil {
			return
		}
		now := time.Now()
		t.post(func() {
			if t.ahead == nil {
				t.ahead = map[string]aheadList{}
			}
			for d, es := range lists {
				if _, read := t.children[d]; !read && t.dirs[d] {
					t.ahead[d] = aheadList{es, now}
				}
			}
		})
	})
}

func (t *Tree) reload(dir string) {
	t.loading[dir] = true
	bg.Go(func() {
		entries, err := vfs.ReadDir(dir)
		t.post(func() {
			delete(t.loading, dir)
			if !t.dirs[dir] {
				return // removed meanwhile
			}
			if err != nil && t.remote && !errors.Is(err, fs.ErrNotExist) {
				// The host is not there: what was read stays.
				return
			}
			kids, dirs := sortEntries(dir, entries)
			t.set(dir, kids, dirs)
			if err == nil {
				t.readAhead(dir)
			}
		})
	})
}

// set records a directory's entries.
func (t *Tree) set(dir string, kids []string, dirs map[string]bool) {
	old := t.children[dir]
	for _, k := range old {
		if t.dirs[k] && !dirs[k] {
			t.forget(k)
		}
	}
	for k := range dirs {
		t.dirs[k] = true
	}
	t.children[dir] = kids
	t.gen++
	if t.watcher != nil && !t.watched[dir] {
		if t.watcher.Add(dir) == nil {
			t.watched[dir] = true
		}
	}
}

// forget drops a directory, and those inside it, from the tree.
func (t *Tree) forget(dir string) {
	for _, k := range t.children[dir] {
		if t.dirs[k] {
			t.forget(k)
		}
	}
	delete(t.children, dir)
	delete(t.dirs, dir)
	if t.watched[dir] {
		t.watcher.Remove(dir)
		delete(t.watched, dir)
	}
}

// Refresh reads every directory read so far again: of a remote tree,
// those that show, at one round trip.
func (t *Tree) Refresh() {
	if t.remote {
		t.poll(true)
		return
	}
	for dir := range t.children {
		if !t.loading[dir] {
			t.reload(dir)
		}
	}
}

// sortEntries are the paths of a directory's entries, directories first,
// then files, each by name, as the file managers sort them, and the
// directories among them.
func sortEntries(dir string, entries []vfs.Entry) ([]string, map[string]bool) {
	kids := make([]string, 0, len(entries))
	dirs := map[string]bool{}
	for _, e := range entries {
		if Hidden(e.Name) {
			continue
		}
		p := filepath.Join(dir, e.Name)
		if e.Dir {
			dirs[p] = true
		}
		kids = append(kids, p)
	}
	slices.SortFunc(kids, func(a, b string) int {
		if dirs[a] != dirs[b] {
			if dirs[a] {
				return -1
			}
			return 1
		}
		return compareNames(filepath.Base(a), filepath.Base(b))
	})
	return kids, dirs
}

// compareNames orders names case-insensitively, then by case.
func compareNames(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

// watch gathers the watcher's events and reads the directories they
// changed again, a moment after the last, so that a burst of changes, as
// a checkout or a build makes, reads each directory once.
func (t *Tree) watch() {
	for {
		select {
		case ev, ok := <-t.watcher.Events:
			if !ok {
				return
			}
			t.event(ev.Name, ev.Has(fsnotify.Create), ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename), ev.Has(fsnotify.Write))
		case _, ok := <-t.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// event gathers a change of a file, which the next flush takes: made,
// removed or renamed away, or written. Safe from any goroutine.
func (t *Tree) event(name string, created, removed, written bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending, t.files, t.gone = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	if created || removed {
		t.pending[filepath.Dir(name)] = true
	}
	if removed {
		t.gone[name] = true
	}
	if written || created {
		t.files[name] = true
		delete(t.gone, name)
	}
	if t.timer == nil && !t.closed {
		t.timer = bg.AfterFunc(120*time.Millisecond, t.flush)
	}
}

func (t *Tree) flush() {
	t.mu.Lock()
	dirs, files, gone := t.pending, t.files, t.gone
	t.pending, t.files, t.gone, t.timer = nil, nil, nil, nil
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	t.post(func() {
		if t.remote {
			// The folders read, at one round trip.
			var again []string
			for dir := range dirs {
				if _, ok := t.children[dir]; ok && !t.loading[dir] {
					again = append(again, dir)
				}
			}
			t.lookAgain(again)
		} else {
			for dir := range dirs {
				if _, ok := t.children[dir]; ok && !t.loading[dir] {
					t.reload(dir)
				}
			}
		}
		if t.OnChange != nil && (len(files) > 0 || len(gone) > 0) {
			var changed, removed []string
			for f := range files {
				changed = append(changed, f)
			}
			for f := range gone {
				removed = append(removed, f)
			}
			t.OnChange(changed, removed)
		}
	})
}

// Watch watches a file's directory, as for a file open in the editor
// outside the directories the tree read; a remote tree stats the file
// itself.
func (t *Tree) Watch(path string) {
	if t.remote {
		if _, ok := t.stamps[path]; !ok {
			t.stamps[path] = nil
		}
		return
	}
	dir := filepath.Dir(path)
	if t.watcher == nil || t.watched[dir] {
		return
	}
	if t.watcher.Add(dir) == nil {
		t.watched[dir] = true
	}
}

// Unwatch stops a remote tree statting a file, as its document closes.
func (t *Tree) Unwatch(path string) {
	delete(t.stamps, path)
}

// Stamp has a remote tree take info as what it last saw of a file, as of
// one the editor wrote, so that it does not report the change.
func (t *Tree) Stamp(path string, info fs.FileInfo) {
	if _, ok := t.stamps[path]; ok && info != nil {
		t.stamps[path] = stampOf(info)
	}
}

// Rel returns path relative to the root, with slashes.
func (t *Tree) Rel(path string) string {
	rel, err := filepath.Rel(t.Root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// PollEvery is how often a remote tree looks for changes.
const PollEvery = 3 * time.Second

// stamp is what stat says of a file that tells it changed.
type stamp struct {
	size int64
	mod  time.Time
	dir  bool
}

func stampOf(info fs.FileInfo) *stamp {
	return &stamp{size: info.Size(), mod: info.ModTime(), dir: info.IsDir()}
}

// IdlePolls is how many polls of a remote tree are left out while it is
// idle, for each that runs.
const IdlePolls = 4

// SetIdle has a remote tree poll less often while its window is not in
// front, and at once as it comes back.
func (t *Tree) SetIdle(idle bool) {
	if !t.remote {
		return
	}
	t.mu.Lock()
	was := t.idle
	t.idle = idle
	t.mu.Unlock()
	if was && !idle {
		t.poll(false)
	}
}

// pollLoop has the main thread poll a remote tree until it closes.
func (t *Tree) pollLoop() {
	tick := time.NewTicker(PollEvery)
	defer tick.Stop()
	skipped := 0
	for range tick.C {
		t.mu.Lock()
		closed, idle := t.closed, t.idle
		t.mu.Unlock()
		if closed {
			return
		}
		if idle && skipped < IdlePolls {
			skipped++
			continue
		}
		skipped = 0
		t.post(func() { t.poll(false) })
	}
}

// shown returns the directories of the tree that show: the root, and the
// open ones whose directories above are.
func (t *Tree) shown() []string {
	var dirs []string
	var walk func(dir string)
	walk = func(dir string) {
		if _, ok := t.children[dir]; !ok || t.loading[dir] {
			return
		}
		dirs = append(dirs, dir)
		for _, k := range t.children[dir] {
			if t.dirs[k] && (t.IsOpen == nil || t.IsOpen(k)) {
				walk(k)
			}
		}
	}
	walk(t.Root)
	return dirs
}

// poll reads the directories that show, and stats the files watched, of a
// remote tree in the background, and takes what changed: while
// inotifywait watches the tree, only the files outside it, unless full.
// It runs on the main thread.
func (t *Tree) poll(full bool) {
	t.startNotify()
	if t.polling {
		t.fullAgain = t.fullAgain || full
		return
	}
	t.mu.Lock()
	notified := t.notify != nil && !full
	t.mu.Unlock()
	var dirs []string
	if !notified {
		dirs = t.shown()
	}
	files := make([]string, 0, len(t.stamps))
	for p := range t.stamps {
		// inotifywait tells of the files under the root.
		if !notified || !strings.HasPrefix(p, t.Root+"/") {
			files = append(files, p)
		}
	}
	if len(dirs) == 0 && len(files) == 0 {
		return
	}
	t.polling = true
	bg.Go(func() {
		lists, errs, infos, err := vfs.Look(dirs, files)
		t.post(func() {
			t.polling = false
			t.mu.Lock()
			closed := t.closed
			t.mu.Unlock()
			if err != nil || closed {
				// The host is not there: nothing is known to change.
				return
			}
			t.took(lists, errs, files, infos)
			if t.fullAgain {
				t.fullAgain = false
				t.poll(true)
			}
		})
	})
}

// took takes what a poll read: the directories that changed, and the
// files watched that changed or went, which it reports.
func (t *Tree) took(lists map[string][]vfs.Entry, errs map[string]error, files []string, infos map[string]fs.FileInfo) {
	var changed, removed []string
	for dir, entries := range lists {
		old, ok := t.children[dir]
		if !ok || !t.dirs[dir] || t.loading[dir] {
			continue
		}
		kids, dirs := sortEntries(dir, entries)
		same := slices.Equal(old, kids)
		for _, k := range kids {
			if same && t.dirs[k] != dirs[k] {
				same = false
			}
			if !slices.Contains(old, k) && !dirs[k] {
				changed = append(changed, k)
			}
		}
		if same {
			continue
		}
		for _, k := range old {
			if !slices.Contains(kids, k) {
				removed = append(removed, k)
			}
		}
		t.set(dir, kids, dirs)
		t.readAhead(dir)
	}
	for dir, err := range errs {
		if errors.Is(err, fs.ErrNotExist) && dir != t.Root && t.dirs[dir] {
			removed = append(removed, dir)
		}
	}
	for _, p := range files {
		old, watched := t.stamps[p]
		if !watched {
			continue // unwatched meanwhile
		}
		info, ok := infos[p]
		switch {
		case !ok && old != nil:
			t.stamps[p] = nil
			if !slices.Contains(removed, p) {
				removed = append(removed, p)
			}
		case !ok:
		case old == nil:
			// The first look, which nothing is compared with.
			t.stamps[p] = stampOf(info)
		default:
			now := stampOf(info)
			if *now != *old {
				t.stamps[p] = now
				if !slices.Contains(changed, p) {
					changed = append(changed, p)
				}
			}
		}
	}
	if t.OnChange != nil && (len(changed) > 0 || len(removed) > 0) {
		t.OnChange(changed, removed)
	}
}

// SkippedDirs are the names of the directories the index leaves out.
func SkippedDirs() []string { return slices.Clone(skippedNames) }

// lookAgain reads directories of a remote tree again, at one round trip,
// as poll does.
func (t *Tree) lookAgain(dirs []string) {
	if len(dirs) == 0 {
		return
	}
	for _, d := range dirs {
		t.loading[d] = true
	}
	bg.Go(func() {
		lists, errs, _, err := vfs.Look(dirs, nil)
		t.post(func() {
			for _, d := range dirs {
				delete(t.loading, d)
			}
			if err == nil {
				t.took(lists, errs, nil, nil)
			}
		})
	})
}

// notifyExcluded matches the paths inotifywait leaves out: those in the
// directories the index skips, which are large and change as builds run.
func notifyExcluded() string {
	names := make([]string, len(skippedNames))
	for i, n := range skippedNames {
		names[i] = regexp.QuoteMeta(n)
	}
	return "/(" + strings.Join(names, "|") + ")(/|$)"
}

// startNotify watches a remote tree with inotifywait on its host, when it
// has one, is connected, and the tree is not watched yet. It runs on the
// main thread.
func (t *Tree) startNotify() {
	c, root, ok := remote.Lookup(t.Root)
	t.mu.Lock()
	running := t.notify != nil
	t.mu.Unlock()
	if !ok || running || t.noNotify || !c.Connected() || !c.Info().Inotify {
		return
	}
	cmd, err := c.Command(nil, `exec inotifywait -m -r -e create,delete,moved_from,moved_to,close_write --format '%e|%w%f' --exclude "$2" -- "$1"`,
		root, notifyExcluded())
	if err != nil {
		return
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	errs, err := cmd.StderrPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	// What changed before the watches were set up, the last poll found or
	// this one finds.
	errsDone := make(chan struct{})
	go func() {
		defer close(errsDone)
		sc := bufio.NewScanner(errs)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "Watches established") {
				t.post(func() { t.poll(true) })
			}
		}
	}()
	t.mu.Lock()
	t.notify = cmd
	t.mu.Unlock()
	host := c.Host
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			events, name, ok := strings.Cut(sc.Text(), "|")
			if !ok {
				continue
			}
			p := remote.Path(host, name)
			has := func(e string) bool { return slices.Contains(strings.Split(events, ","), e) }
			t.event(p, has("CREATE") || has("MOVED_TO"), has("DELETE") || has("MOVED_FROM"), has("CLOSE_WRITE"))
		}
		<-errsDone
		cmd.Wait()
		t.post(func() {
			t.mu.Lock()
			if t.notify == cmd {
				t.notify = nil
			}
			t.mu.Unlock()
			// Ended with the host there: too many folders to watch, or no
			// inotify. The tree polls from now on.
			if c.Connected() {
				t.noNotify = true
			}
		})
	}()
}

// SortEntries returns the paths of a directory's entries as the tree has
// them, directories first and the hidden ones left out, and the
// directories among them.
func SortEntries(dir string, entries []vfs.Entry) ([]string, map[string]bool) {
	return sortEntries(dir, entries)
}
