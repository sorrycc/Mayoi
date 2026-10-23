package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/keymap"
	"mayoi/internal/mon"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// remoteIndexEvery is the least time between two listings of the files of
// a remote folder.
const remoteIndexEvery = 30 * time.Second

// host is what the app asks of the desktop: native dialogs and the trash.
// Its functions return at once and call back on the main thread.
type host interface {
	openFolder(done func(path string))
	saveAs(name string, done func(path string))
	// confirmClose asks whether to save a changed document before closing
	// it: 0 saves, 1 discards the changes, 2 keeps it open.
	confirmClose(name string, done func(choice int))
	trash(path string) error
	reveal(path string)
	// copyFiles puts files on the clipboard, as the Finder copies them,
	// and clipboardFiles are the local files the clipboard has.
	copyFiles(paths []string) error
	clipboardFiles() []string
	// clipboardImage is the image the clipboard has, as PNG, or nil.
	clipboardImage() []byte
	// choosePaths asks in the native dialog for files, or for a folder
	// when folders is set, starting in dir.
	choosePaths(title, button, dir string, folders bool, done func(paths []string))
	setTitle(title string)
	closeWindow()
	// setTheme gives the windows a theme and an appearance of the
	// settings.
	setTheme(theme, appearance string)
	// setLocale shows the windows in the language of tag, or of the
	// system for "".
	setLocale(tag string)
	// showFolder brings the other window that has dir open to the front,
	// and reports whether there is one.
	showFolder(dir string) bool
	// newWindow opens dir in a window of its own, or brings the one that
	// has it open to the front.
	newWindow(dir string)
	// newBlankWindow opens a window that shows nothing.
	newBlankWindow()
	// openFolders opens folders as `mayoi <dir>` does: a window that
	// shows nothing takes one, as may this one.
	openFolders(dirs []string)
	// recentChanged shows the recent folders anew, in the menus, once
	// they change.
	recentChanged()
	menuChanged()
	// fontChanged draws every window anew, in the font the settings
	// have now.
	fontChanged()
	// installCLI and uninstallCLI put the mayoi command on the shell's
	// path, and take it away, saying how it went.
	installCLI()
	uninstallCLI()
	// account is the name of the person settings sync syncs for, "" when
	// signed out. signIn signs in, showing the code to approve in the
	// window, signOut signs out, and syncNow syncs at once.
	account() string
	signIn()
	signOut()
	syncNow()
	// syncInfo is how settings sync is, and syncPicture the person's
	// picture, nil until it is fetched. syncConflictResolved says the
	// account's keybindings.json kept beside this machine's may be gone.
	syncInfo() syncInfo
	syncPicture() *ui.Bitmap
	syncConflictResolved()
	// ai is a client of Mon's AI gateway, as the account signed in, nil
	// while signed out. aiSignedOut says Mon refused its token: the
	// account was signed out elsewhere.
	ai() *mon.Client
	aiSignedOut()
	// update is the new version of Mayoi a check found, "" when there is
	// none, and installUpdate opens the update window to install it.
	update() string
	installUpdate()
	// With project tabs, stepProject shows the project by steps along the
	// rail, showProject the one at i, the last for -1, and closeProject
	// closes the window's, as the window closes when it is its only one.
	// projectTabsChanged gathers the windows into one, or parts them, as
	// the settings now have it.
	stepProject(by int)
	showProject(i int)
	closeProject()
	projectTabsChanged()
}

// app is the state of a window of Mayoi.
type app struct {
	host     host
	settings *settings.Settings
	// window is what the settings keep of this window.
	window *settings.Window

	pal *palette
	// fade is the window's way from one palette to another, as the
	// theme or the appearance changes.
	fade     paletteFade
	font     codeFont
	services ui.Services
	// vimReg is what vim mode put on the clipboard last, which tells
	// whether a paste of the same text pastes lines.
	vimReg vimRegister
	// vimLine is vim mode's command line, vimSearch the pattern searched
	// last, vimHist the searches and the commands typed, and vimMsg what
	// a command said.
	vimLine   vimLine
	vimSearch vimSearch
	vimHist   [2][]string
	vimMsg    vimMessage
	vimCount  vimCounted
	// vimRegs are vim mode's registers but the unnamed one, which is the
	// clipboard. vimRec is the register recorded into, 0 while none is,
	// and vimRecKeys the keys typed since; vimPlayed is the register played
	// last, which @@ plays again. vimPlaying is set while keys play, as
	// many deep as registers play registers, and vimBudget is how many more
	// they may type. vimFailed is set as a command fails, which ends the
	// keys playing.
	vimRegs    map[rune]vimRegister
	vimRec     rune
	vimRecKeys []rune
	vimPlayed  rune
	vimPlaying int
	vimBudget  int
	vimFailed  bool
	// vimLastEx is the command run last after :, which @: runs again, and
	// vimLastSub what followed :s, which & does again. vimGlobalOn is set
	// while :g runs its command.
	vimLastEx   string
	vimLastSub  string
	vimGlobalOn bool

	// queue holds the functions other goroutines post for the main thread,
	// which the next frame runs.
	mu    sync.Mutex
	queue []func()

	root string
	tree *workspace.Tree
	// conn is the connection to the host of a remote folder, nil for one
	// of this machine, connState and connErr how it was last seen, and
	// connUnsub stops following it.
	conn      *remote.Conn
	connState remote.State
	connErr   error
	connUnsub func()
	// retry is the reconnection due while a connection lost waits for the
	// host to come back: retries counts those that failed in a row, and
	// retryAt is when the next starts.
	retry   *time.Timer
	retries int
	retryAt time.Time
	// indexed is when the files of a remote folder were last listed,
	// which is done again at most every remoteIndexEvery.
	indexed time.Time
	files   ui.OutlineState[string]
	// crumbs is the box a folder of the breadcrumbs opens.
	crumbs  crumbsPicker
	fileRow int
	// filesView is the explorer's list, which focusFiles has take the
	// keyboard as it next shows.
	filesView  ui.Handle
	focusFiles bool
	// treeSynced is the document the tree last followed, selectInTree the
	// file it should choose once its row shows.
	treeSynced, selectInTree string
	git                      gitState
	repositories             repositoryState
	commitTools              commitTools
	scm                      scmState

	// ignored is what Git ignores in the workspace, which the tree shows
	// faint.
	ignored                map[string]bool
	ignoring, ignoredStale bool

	index      []string
	recent     []string // the files shown last, latest first
	indexing   bool
	indexStale bool
	// opening is set while a file opens as a preview tab.
	opening bool

	// docs are the documents open, each in a tab or several; cols the
	// columns of the editor area, and col the one that has the keyboard,
	// or had it last, which commands are for.
	docs []*document
	cols []*column
	col  int
	// findCol is the column the find widget shows in, the one with the
	// keyboard as the frame's columns began: a click on another's tab as
	// they draw moves col, and the widget's input binds one control.
	findCol *column
	// review is the comments on lines of the changes, to send to an agent.
	review review
	// fileCol caches the last unlocked column used for files. Removal clears
	// it; column and activate refresh it. Use fileColumn to resolve a live,
	// unlocked target before opening files, including during restoration.
	fileCol  *column
	nextID   int
	nextTab  int
	nextCol  int
	untitled int
	// closed are the tabs closed, the last last, which ⇧⌘T opens again.
	closed []closedTab
	// shownDoc is the document the column with the keyboard showed last
	// frame, which saves itself once it shows another.
	shownDoc *document

	// terms are the terminals, those of the panel and those in tabs, and
	// panelTerm the one the panel shows.
	terms     []*session
	panelTerm *session
	nextTerm  int
	// panelScroll measures whether the terminal chips need a scroll bar;
	// panelLayout requests a follow-up frame when their available room or
	// titles change, so the header can use the freshly measured overflow.
	panelScroll ui.ScrollState
	panelLayout panelHeaderLayout
	// termTheme is the theme of the settings the terminals have the colors
	// of.
	termTheme string
	// termFont is the font the terminals have.
	termFont terminal.Font
	// termCopy is whether the terminals copy what is selected.
	termCopy bool
	// transfers are the copies to and from hosts running.
	transfers []*transfer

	panelMax    bool // the panel takes the editor's room
	welcomeView ui.Handle
	rain        rainState   // The Matrix's, behind the welcome page
	mascot      mascotState // the theme's character, Claude's or Qoder's
	// blurred is set while the window has not the keyboard.
	blurred  bool
	colsView ui.Handle // the columns, whose width their weights share
	// frame counts the frames, and frameAt is when the last started: a
	// frame may build the view several times.
	frame   int
	frameAt time.Time
	// dragging is set while a tab is dragged, as the frame before found.
	dragging, dragNow bool
	// middle is the tab a middle press is on, until it is let go of: the
	// press closes it rather than shows it.
	middle *colTab

	quick      quickOpen
	palette    []paletteEntry    // the commands of the command palette, made as it first opens
	hints      map[string]string // the keys of the commands, as tooltips show them
	branches   branchPicker      // the box that switches branches
	recents    recentPicker      // the box of the recent folders
	themes     themePicker       // the box of the color themes
	remotePick remotePicker      // the box that connects to a host
	conflicts  conflictsList     // the box of the files in conflict
	find       findState
	inFiles    searchState // search in the files, of the sidebar
	prompt     *prompt
	confirm    *confirm
	signin     *signin
	// syncOpen shows the account's panel above its part of the status
	// bar, and tickAt is when a countdown shown repaints next.
	syncOpen bool
	tickAt   time.Time
	// dialog is the dialog shown, which came in as it showed, and
	// dialogBuilt says a build of the view built one.
	dialog      string
	dialogBuilt bool

	errors []string // to show as toasts
	// fails counts the errors shown, which stop the commands of
	// runCommands.
	fails   int
	notes   []note // what went well, to show as toasts
	title   string
	focus   pane
	started bool
	// keys are the shortcuts of keybindings.json, nil for none; chord the
	// strokes of a chord of them typed so far. keyFocus is the place the
	// keyboard is in as the view was built, placeNone where the window
	// takes their keys, and keyTerm the terminal that took a key last.
	keys        *keymap.Map
	chord       []keymap.Stroke
	keyFocus    keyPlace
	cutInEditor bool
	keyTerm     *session
	// held is the key the keymap took last, until it is let go of.
	held *ui.Key
	// chain are the commands of runCommands still to run, which stop at
	// an error once fails passes chainFails.
	chain      []chainStep
	chainFails int
	// kept is the session the window had before its documents closed as it
	// closes, which it keeps for its next start, nil until then.
	kept *settings.Session
	// rail builds the rail of project tabs left of the sidebar, nil for
	// none.
	rail func(c *ui.Context)
	// alert is what a terminal of the project last asked attention for,
	// by its bell or a notification, until the project shows: the rail
	// marks its square.
	alert string
}

type pane int

const (
	paneNone pane = iota
	paneFiles
	paneEditor   // a column: its editor, or its terminal
	paneTerminal // the panel
)

func newApp(h host, s *settings.Settings, w *settings.Window) *app {
	a := &app{host: h, settings: s, window: w, fileRow: -1}
	w.Normalize(settings.DefaultLayout)
	a.scm.row, a.inFiles.row = -1, -1
	a.files.List.Selected = &a.fileRow
	a.cols = []*column{a.newColumn()}
	return a
}

// post runs fn on the main thread before the next frame. Safe from any
// goroutine.
func (a *app) post(fn func()) {
	a.mu.Lock()
	a.queue = append(a.queue, fn)
	sv := a.services
	a.mu.Unlock()
	sv.Invalidate()
}

// setServices has the app's posts and invalidations draw the frames of
// the window sv is of, which shows it now.
func (a *app) setServices(sv ui.Services) {
	a.mu.Lock()
	a.services = sv
	a.mu.Unlock()
}

// park keeps the place of the editors shown as the project goes out of
// sight behind another of the window, which it gets back as it shows
// again, as a tab does: their text areas are built anew then.
func (a *app) park() {
	if !a.started {
		return
	}
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.ed != nil {
				a.hideEditor(t.ed)
			}
		}
	}
	a.rememberTabs()
	a.blurred = true
}

// invalidate draws a new frame, as when the highlighter has new tokens.
// Safe from any goroutine.
func (a *app) invalidate() {
	a.mu.Lock()
	sv := a.services
	a.mu.Unlock()
	sv.Invalidate()
}

// drain runs the functions posted since the last frame.
func (a *app) drain() {
	for {
		a.mu.Lock()
		q := a.queue
		a.queue = nil
		a.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

func (a *app) fail(format string, args ...any) {
	a.errors = append(a.errors, fmt.Sprintf(format, args...))
	a.fails++
}

// note is a toast of what a command did, with a button of label that runs
// action, as Undo, when action is set.
type note struct {
	message, label string
	action         func()
}

// tell shows a toast of what a command did.
func (a *app) tell(n note) { a.notes = append(a.notes, n) }

// start restores the window's last session: its folder and the files it
// had open.
func (a *app) start() {
	w := a.window
	if w.Folder != "" {
		// A remote folder opens at once, as its host connects.
		if remote.Is(w.Folder) {
			a.openFolder(w.Folder)
		} else if info, err := os.Stat(w.Folder); err == nil && info.IsDir() {
			a.openFolder(w.Folder)
		}
	}
	a.restoreTabs()
	if w.TerminalOpen && w.PanelOpen && a.root != "" {
		a.newTerminal()
	}
}

// openFolder makes dir the workspace.
func (a *app) openFolder(dir string) {
	dir = filepath.Clean(dir)
	if a.tree != nil {
		a.tree.Close()
	}
	if a.root != dir {
		a.closed = nil
	}
	a.root = dir
	a.attachRemote(dir)
	a.tree = workspace.NewTree(dir, a.post)
	a.tree.OnChange = a.diskChanged
	a.tree.IsOpen = a.files.Open.Has
	a.tree.Load(dir)
	a.files = ui.OutlineState[string]{Indent: treeStep}
	a.fileRow = -1
	a.files.List.Selected = &a.fileRow
	a.index, a.indexStale = nil, true
	a.ignored, a.ignoredStale = nil, true
	a.resetRepositories()
	a.git = gitState{}
	a.resetSearch()
	a.window.Folder = dir
	a.settings.AddRecent(dir)
	a.host.recentChanged()
	for _, d := range a.docs {
		a.tree.Watch(d.path)
	}
	for _, v := range a.imageViewers() {
		a.tree.Watch(v.path)
	}
	a.settings.Changed()
}

// folder is the window's folder, also before the window starts and
// opens it.
func (a *app) folder() string {
	if a.root != "" || a.window.Folder == "" {
		return a.root
	}
	return filepath.Clean(a.window.Folder)
}

// blank reports whether the window shows nothing: no folder, no tabs and
// no terminal, also before it starts.
func (a *app) blank() bool {
	if a.folder() != "" || len(a.docs) > 0 || len(a.terms) > 0 {
		return false
	}
	for _, cs := range a.window.Columns {
		if len(cs.Tabs) > 0 {
			return false
		}
	}
	return true
}

// closeFolder closes the workspace, once its documents are closed,
// keeping its session for when it opens again.
func (a *app) closeFolder() {
	ses := a.session()
	a.closeAll(func() {
		a.stash(ses)
		a.window.Expanded = nil
		if a.tree != nil {
			a.tree.Close()
		}
		a.detachRemote()
		a.resetRepositories()
		a.root, a.tree, a.index, a.git = "", nil, nil, gitState{}
		a.resetSearch()
		a.closed = nil
		a.window.Folder = ""
		a.settings.Changed()
	})
}

// saveDoc saves a document, asking where an untitled one goes.
func (a *app) saveDoc(d *document, then func()) {
	if d.path == "" {
		a.askSavePath(d.name(), func(path string) {
			if path == "" {
				return
			}
			a.dropKept(d)
			d.path = path
			d.hl.Close()
			d.hl = highlight.New(highlight.Lexer(path), a.invalidate)
			a.saveDoc(d, then)
			if a.tree != nil {
				a.tree.Watch(path)
			}
		})
		return
	}
	saved := func() {
		d.diskGone, d.autoSave.failed = false, false
		a.repositoryForPath(d.path).git.stale = true
		a.rememberTabs()
		if then != nil {
			then()
		}
	}
	if remote.Is(d.path) {
		// Saved by hand, a file that changed on its host asks again.
		d.conflict = false
		a.saveRemote(d, saved, func(err error) { a.fail("%s", locales.FailSave(d.name(), err.Error())) })
		return
	}
	if err := d.save(); err != nil {
		a.fail("%s", locales.FailSave(d.name(), err.Error()))
		return
	}
	saved()
}

func (a *app) saveActive() {
	if t := a.activeTab(); t != nil && t.merge != nil {
		a.saveMerge(t.merge)
		return
	}
	if d := a.activeEditor(); d != nil {
		a.saveDoc(d.document, nil)
	}
}

func (a *app) saveActiveAs() {
	ed := a.activeEditor()
	if ed == nil {
		return
	}
	d := ed.document
	a.askSavePath(d.name(), func(path string) {
		if path == "" {
			return
		}
		d.path = path
		d.hl.Close()
		d.hl = highlight.New(highlight.Lexer(path), a.invalidate)
		a.saveDoc(d, nil)
	})
}

func (a *app) saveAll() {
	for _, d := range a.docs {
		if d.dirty() {
			a.saveDoc(d, nil)
		}
	}
}

// diskChanged follows files changed on disk: an open document without
// changes of its own shows the file's new text.
func (a *app) diskChanged(changed, removed []string) {
	for _, path := range append(slices.Clone(changed), removed...) {
		if filepath.Base(path) == ".gitmodules" || a.git.repo == nil && a.mayHoldWorkTree(path) {
			a.repositories.dirty = true
		}
	}
	a.indexStale, a.ignoredStale = true, true
	a.diffsOnDisk(append(changed, removed...))
	a.searchAgain(append(changed, removed...))
	// An image shows the file's new picture, or that it is gone.
	for _, v := range a.imageViewers() {
		switch {
		case slices.Contains(changed, v.path):
			v.diskGone = false
			a.loadImage(v)
		case slices.Contains(removed, v.path):
			// A remote tree reports only what it found gone.
			if _, err := os.Stat(v.path); err != nil || remote.Is(v.path) {
				v.diskGone = true
			}
		}
	}
	for _, p := range changed {
		i := a.docIndex(p)
		if i < 0 {
			continue
		}
		d := a.docs[i]
		d.diskGone = false
		if d.dirty() {
			continue
		}
		a.reloadDoc(d)
	}
	for _, p := range removed {
		if i := a.docIndex(p); i >= 0 {
			if _, err := os.Stat(p); err != nil || remote.Is(p) {
				a.docs[i].diskGone = true
			}
		}
	}
	a.gitChanged()
}

// mayHoldWorkTree reports whether a path made or gone under a folder that
// is not a work tree may add or drop one under it: a .git, or a folder
// where one may be, cloned or removed, outside the work trees found.
func (a *app) mayHoldWorkTree(path string) bool {
	if filepath.Base(path) == ".git" {
		return true
	}
	rel, err := filepath.Rel(a.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Count(filepath.ToSlash(rel), "/") >= git.NestedDepth {
		return false
	}
	ctx := a.repositoryForPath(path)
	return ctx.git.repo == nil || ctx.git.repo.Root == path
}

// reloadDoc makes a document's text the file's on disk, as a step of
// undo, as in other editors.
func (a *app) reloadDoc(d *document) {
	if remote.Is(d.path) {
		a.reloadRemote(d)
		return
	}
	data, err := os.ReadFile(d.path)
	if err != nil || isBinary(data) {
		return
	}
	d.takeText(data)
}

// takeText makes a document's text data, read from its file, as a step of
// undo, and clean.
func (d *document) takeText(data []byte) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if string(data) != d.buf.String() {
		if ed := d.main(); ed != nil {
			ed.edit(ed.sel, textEdit{0, d.buf.Len(), string(data)})
		}
		d.measureWidest()
	}
	d.markSaved()
}

// imageViewers returns the viewers of the image files open in tabs.
func (a *app) imageViewers() []*viewer {
	var out []*viewer
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.view != nil {
				out = append(out, t.view)
			}
		}
	}
	return out
}

// refreshIndex lists the workspace's files again in the background, for
// quick open, when they may have changed.
func (a *app) refreshIndex() {
	if a.root == "" || a.indexing || !a.indexStale || !a.online() {
		return
	}
	// A remote folder's files are listed on its host, which a busy folder
	// would keep doing.
	if a.conn != nil && a.index != nil && time.Since(a.indexed) < remoteIndexEvery {
		return
	}
	a.indexed = time.Now()
	a.indexing, a.indexStale = true, false
	root := a.root
	bg.Go(func() {
		defer recoverCrash()
		files := workspace.Index(root)
		a.post(func() {
			a.indexing = false
			if a.root == root {
				a.index = files
				a.quick.stale = true
			}
		})
	})
}

// refreshIgnored asks Git again in the background what it ignores, when
// the files may have changed.
func (a *app) refreshIgnored() {
	if a.root == "" || a.ignoring || !a.ignoredStale || !a.online() {
		return
	}
	if a.conn != nil && a.ignored != nil && time.Since(a.indexed) < remoteIndexEvery {
		return
	}
	a.ignoring, a.ignoredStale = true, false
	root := a.root
	bg.Go(func() {
		defer recoverCrash()
		ignored := workspace.Ignored(root)
		a.post(func() {
			a.ignoring = false
			if a.root == root {
				a.ignored = ignored
			}
		})
	})
}

// isIgnored reports whether Git ignores path, or a folder above it.
func (a *app) isIgnored(path string) bool {
	if len(a.ignored) == 0 {
		return false
	}
	for p := path; len(p) > len(a.root); p = filepath.Dir(p) {
		if a.ignored[p] {
			return true
		}
	}
	return false
}

// windowTitle is the title the window shows.
func (a *app) windowTitle() string {
	parts := []string{}
	if t := a.activeTab(); t != nil {
		name := t.name()
		if t.ed != nil && t.ed.dirty() {
			name = "● " + name
		}
		parts = append(parts, name)
	}
	if a.root != "" {
		parts = append(parts, filepath.Base(a.root))
		if host := remote.HostOf(a.root); host != "" {
			parts[len(parts)-1] = filepath.Base(hostPath(a.root)) + " [SSH: " + host + "]"
		}
	}
	if len(parts) == 0 {
		return "Mayoi"
	}
	return strings.Join(parts, " — ")
}

// tick runs once a frame, after the posted functions.
func (a *app) tick() {
	if a.tree != nil {
		a.tree.SetIdle(a.blurred)
	}
	a.markOffline()
	a.refreshIgnored()
	a.refreshGit()
	a.searchTick()
	a.keepEdited()
	if t := a.windowTitle(); t != a.title {
		a.title = t
		a.host.setTitle(t)
	}
}
