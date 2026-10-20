// Mayoi is a small, quick code editor: a file tree, tabs of editors and a
// terminal panel, drawn by MyGo's native UI.
package main

import (
	"errors"
	"fmt"
	"log"
	"mayoi/internal/cli"
	"mayoi/internal/keymap"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

// desktop is a project of the app, a folder and what shows of it, in a
// window of its own or, with project tabs, one of a window's: the host of
// its native dialogs, the trash.
type desktop struct {
	m *mayoi
	a *app
	f *frame // the window it is in
	// logoDir is the folder the logo scan is for, logoIcon the icon of
	// its own it tried first, logoSrc its favicon or logo once decoded,
	// logoDone whether it was scanned, logoScannedAt when, and logoGen
	// drops the scans a newer one superseded.
	logoDir       string
	logoIcon      string
	logoSrc       ui.ImageSource
	logoDone      bool
	logoScannedAt time.Time
	logoGen       int
}

// mayoi is the app's projects, the focused one last first, and its
// windows.
type mayoi struct {
	s      *settings.Settings
	keys   *keymap.Map // the shortcuts of keybindings.json
	wins   []*desktop
	frames []*frame
	// pending are the folders and files the app was asked to open before
	// it was ready.
	ready   bool
	pending []string
	// quitting is set once the app quits, so that the windows it closes
	// stay in the settings, to open again at the next start.
	quitting bool
	// menuKey is the key (menuKey) of the menu last set, which is set
	// again only when it changes.
	menuKey      string
	menuRevision uint64 // rejects deferred updates superseded by a newer menu
	// cloud syncs the settings with the person's account, nil without a
	// settings file.
	cloud *cloud
	// icons is the folder of the icons chosen for projects on the rail,
	// beside the settings, "" without a settings file.
	icons string
	// updateFound is the newest version the last check found, "" when
	// Mayoi is up to date, and update the one shown, "" when skipped.
	updateFound string
	update      string
}

func (h *desktop) openFolder(done func(string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: h.f.win, Title: locales.DialogOpenFolder(), ButtonLabel: locales.DialogOpen(), Directory: true, CreateDirectories: true,
		})
		if err == nil && len(paths) > 0 {
			h.a.post(func() { done(paths[0]) })
		}
	}()
}

func (h *desktop) choosePaths(title, button, dir string, folders bool, done func([]string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: h.f.win, Title: title, ButtonLabel: button, DefaultPath: dir,
			Directory: folders, Multiple: !folders, CreateDirectories: folders,
		})
		if err == nil && len(paths) > 0 {
			h.a.post(func() { done(paths) })
		}
	}()
}

// chooseIcon asks for a picture to show for the project of dir on the
// rail, starting in its folder.
func (h *desktop) chooseIcon(dir string, done func(string)) {
	exts := make([]string, len(iconExts))
	for i, e := range iconExts {
		exts[i] = strings.TrimPrefix(e, ".")
	}
	if remote.Is(dir) {
		dir = ""
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: h.f.win, Title: locales.DialogChooseIcon(), ButtonLabel: locales.DialogChoose(), DefaultPath: dir,
			Filters: []mygo.FileFilter{{Name: locales.DialogImages(), Extensions: exts}},
		})
		if err == nil && len(paths) > 0 {
			h.a.post(func() { done(paths[0]) })
		}
	}()
}

func (h *desktop) saveAs(name string, done func(string)) {
	dir := h.a.root
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: h.f.win, Title: locales.DialogSaveAs(), DefaultPath: filepath.Join(dir, name), CreateDirectories: true,
		})
		if err == nil && path != "" {
			h.a.post(func() { done(path) })
		}
	}()
}

func (h *desktop) confirmClose(name string, done func(int)) {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: h.f.win, Type: mygo.MessageWarning,
			Message: locales.SaveChangesQuestion(name),
			Detail:  locales.SaveChangesDetail(),
			Buttons: []string{locales.ButtonSave(), locales.ButtonDontSave(), locales.ButtonCancel()}, DefaultButton: 0, CancelButton: 2,
		})
		choice := 2
		if err == nil {
			choice = res.Button
		}
		h.a.post(func() { done(choice) })
	}()
}

func (h *desktop) trash(path string) error { return mygo.Shell.TrashItem(path) }
func (h *desktop) reveal(path string) {
	if !remote.Is(path) {
		mygo.Shell.ShowItemInFolder(path)
	}
}
func (h *desktop) copyFiles(paths []string) error {
	return mygo.Clipboard.WriteFiles(paths...)
}
func (h *desktop) clipboardFiles() []string {
	paths, _ := mygo.Clipboard.ReadFiles()
	return paths
}
func (h *desktop) clipboardImage() []byte { return mygo.Clipboard.ReadImage() }

// setTitle titles the window, while it shows the project.
func (h *desktop) setTitle(title string) {
	if h.f.active == h {
		h.f.win.SetTitle(title)
	}
}

// closeWindow closes the project, and the window with its last.
func (h *desktop) closeWindow() { h.closeProject() }
func (h *desktop) setTheme(theme, appearance string) {
	mygo.Theme.SetSource(themeSource(theme, appearance))
	// From one theme to another the native appearance may stay, so the
	// other windows would not draw anew.
	for _, w := range h.m.wins {
		w.a.invalidate()
	}
	// The appearances are greyed out under The Matrix.
	h.m.refreshMenu()
}

func (h *desktop) setLocale(tag string) {
	mygo.App.SetLocale(tag)
	// The language may show as it did, as English for the system's
	// English: the menu's choice changes all the same.
	h.m.refreshMenu()
}

func (h *desktop) showFolder(dir string) bool {
	if w := h.m.showing(dir); w != nil && w != h {
		w.show()
		return true
	}
	return false
}

func (h *desktop) newWindow(dir string) {
	h.m.openFolder(filepath.Clean(dir), false)
}

func (h *desktop) newBlankWindow() { h.m.openBlankWindow() }

func (h *desktop) openFolders(dirs []string) {
	for _, dir := range dirs {
		h.m.openFolder(dir, true)
	}
}

func (h *desktop) recentChanged() { h.m.refreshMenu() }
func (h *desktop) menuChanged()   { h.m.refreshMenu() }

func (h *desktop) fontChanged() {
	for _, w := range h.m.wins {
		w.a.invalidate()
	}
}

func (h *desktop) installCLI() {
	go func() {
		home, _ := os.UserHomeDir()
		p, err := cli.Install(cli.Dirs(home, cli.ShellPath()), appBundle())
		switch {
		case errors.Is(err, cli.ErrCanceled):
		case err != nil:
			h.tell(mygo.MessageWarning, locales.CliInstallFailed(), err.Error())
		default:
			h.tell(mygo.MessageInfo, locales.CliInstalled(shortenHome(filepath.Dir(p))), locales.CliInstalledDetail())
		}
	}()
}

func (h *desktop) uninstallCLI() {
	go func() {
		home, _ := os.UserHomeDir()
		removed, err := cli.Uninstall([]string{filepath.Join(home, ".local", "bin"), "/usr/local/bin"})
		switch {
		case errors.Is(err, cli.ErrCanceled):
		case err != nil:
			h.tell(mygo.MessageWarning, locales.CliUninstallFailed(), err.Error())
		case len(removed) == 0:
			h.tell(mygo.MessageInfo, locales.CliNotInstalled(), "")
		default:
			h.tell(mygo.MessageInfo, locales.CliUninstalled(), locales.CliUninstalledDetail(shortenHome(filepath.Dir(removed[0]))))
		}
	}()
}

// tell shows a message on the window.
func (h *desktop) tell(kind mygo.MessageType, message, detail string) {
	mygo.Dialog.Message(mygo.MessageOptions{Parent: h.f.win, Type: kind, Message: message, Detail: detail})
}

// show brings the window to the front, showing the project.
func (h *desktop) show() {
	h.f.activate(h)
	h.f.show()
}

// do runs fn on the window's app before its next frame.
func (h *desktop) do(fn func()) {
	h.a.post(fn)
	h.f.win.Invalidate()
}

// settingsPath is where the settings are kept.
func settingsPath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

func main() {
	mygo.App.SetName("Mayoi")
	// MAYOI_DATA_DIR keeps the settings and window state elsewhere, such as
	// scripts/dev.sh's Mayoi-dev, so a development app leaves Mayoi's alone.
	if dir := os.Getenv("MAYOI_DATA_DIR"); dir != "" {
		mygo.App.SetPath(mygo.PathUserData, dir)
	}
	// From a terminal on Linux, Mayoi runs on apart from it; before the
	// lock, which the Mayoi it starts takes.
	if detach() {
		return
	}
	// One Mayoi runs: the folders and files another is started with open
	// in this one.
	if os.Getenv("MAYOI_SNAPSHOT") == "" && !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// After the lock, so that a second Mayoi leaves the first's crash.log
	// alone.
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		keepCrashes(dir)
	}
	// Release builds check for updates every hour (startUpdates) and show
	// them in the window, whose click opens MyGo's update window; the
	// updater never checks by itself. Development builds never check.
	mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true}))
	m := &mayoi{}
	mygo.App.OnSecondInstance(func(args []string, wd string) {
		if paths := argPaths(args, wd); len(paths) > 0 {
			m.open(paths)
		} else if len(m.wins) > 0 {
			m.wins[0].show()
		}
	})
	// The folders and files of `mayoi` and the Finder, also those the app
	// starts with.
	mygo.App.OnOpenFile(func(path string) {
		if !m.ready {
			m.pending = append(m.pending, path)
			return
		}
		m.open([]string{path})
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if m.quitting || !m.anyDirty() {
			m.quitting = true
			return
		}
		// Ask about the changes of each window first, then quit again.
		e.PreventDefault()
		m.settle(slices.Clone(m.wins), func() {
			m.quitting = true
			mygo.App.Quit()
		})
	})
	mygo.App.OnQuit(func() {
		m.s.Flush()
		// A change made a moment ago, which would sync at the next launch.
		m.cloud.flush()
		// The ssh masters, which would outlive the app.
		remote.Shutdown()
	})
	mygo.App.WhenReady(func() {
		m.s = settings.Load(settingsPath())
		if p := settingsPath(); p != "" {
			m.icons = filepath.Join(filepath.Dir(p), "project-icons")
		}
		mygo.Theme.SetSource(themeSource(m.s.Theme, m.s.Appearance))
		// The language of the settings, unless MYGO_LOCALE (mygo dev -lang)
		// chose one for the run.
		if os.Getenv("MYGO_LOCALE") == "" {
			applyLocale(m.s.Locale)
		}
		mygo.App.OnLocaleChanged(func(string) { m.localeChanged() })
		if stop, err := m.s.Watch(func(p settings.Prefs) { mygo.RunOnMain(func() { m.reload(p); m.cloud.changed() }) }); err == nil {
			mygo.App.OnQuit(stop)
		}
		keysPath := keymap.Path(m.s.Path())
		keys, keysErr := keymap.Load(keysPath)
		m.keys = keys
		if stop, err := keymap.Watch(keysPath, func(km *keymap.Map, err error) { mygo.RunOnMain(func() { m.reloadKeys(km, err); m.cloud.changed() }) }); err == nil {
			mygo.App.OnQuit(stop)
		}
		wd, _ := os.Getwd()
		m.ready = true
		m.launch(append(m.pending, argPaths(os.Args[1:], wd)...))
		m.pending = nil
		m.startCloud()
		m.startUpdates()
		if len(m.wins) == 0 {
			return
		}
		first := m.wins[0]
		first.a.keysFailed(keys, keysErr)
		m.setMenu(first.a)
		if out := os.Getenv("MAYOI_SNAPSHOT"); out != "" {
			// For checking the real window: capture it once it settles, and
			// quit.
			a, win := first.a, first.f.win
			go func() {
				// MAYOI_SNAPSHOT_RUN names commands to run first.
				for _, id := range strings.Fields(os.Getenv("MAYOI_SNAPSHOT_RUN")) {
					time.Sleep(600 * time.Millisecond)
					a.post(func() { a.run(id) })
				}
				time.Sleep(2500 * time.Millisecond)
				if png, err := win.CapturePage(); err == nil {
					os.WriteFile(out, png, 0o644)
				}
				mygo.App.Quit()
			}()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// argPaths are the folders and files of a command line, absolute.
func argPaths(args []string, wd string) []string {
	var paths []string
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if !filepath.IsAbs(arg) {
			arg = filepath.Join(wd, arg)
		}
		paths = append(paths, arg)
	}
	return paths
}

// launch opens the windows the app starts with: one for each folder it
// was given, or else those of the last session. The files it was given
// open in them.
func (m *mayoi) launch(paths []string) {
	folders, files := splitPaths(paths)
	switch {
	case m.s.ProjectTabs:
		// Every project of the last session, in the rail's order, the one
		// focused last shown, and then the folders it was given.
		var front *settings.Window
		if len(m.s.Windows) > 0 {
			front = m.s.Windows[0]
		}
		for _, w := range m.s.InRail() {
			m.openWindow(w)
		}
		if h := m.of(front); h != nil {
			h.show()
		}
		for _, dir := range folders {
			m.openFolder(dir, true)
		}
	case len(folders) == 0:
		// The one focused last ends up in front.
		for _, w := range slices.Backward(slices.Clone(m.s.Windows)) {
			m.openWindow(w)
		}
	default:
		// A folder of the last session opens as it was left. The other
		// windows of the session are let go, keeping their folders'
		// sessions.
		var keep []*settings.Window
		for _, w := range m.s.Windows {
			if slices.Contains(folders, filepath.Clean(w.Folder)) && !slices.ContainsFunc(keep, func(k *settings.Window) bool { return k.Folder == w.Folder }) {
				keep = append(keep, w)
			} else {
				m.s.Stash(w)
			}
		}
		m.s.Windows = keep
		for _, dir := range slices.Backward(folders) {
			m.openFolder(dir, false)
		}
	}
	if len(m.wins) == 0 {
		m.openBlankWindow()
	}
	for _, p := range files {
		m.openFile(p)
	}
}

// open opens folders and files the app is asked to while it runs.
func (m *mayoi) open(paths []string) {
	folders, files := splitPaths(paths)
	for _, dir := range folders {
		m.openFolder(dir, true)
	}
	for _, p := range files {
		m.openFile(p)
	}
}

// splitPaths sorts paths into folders and files, cleaned, without those
// that are neither.
func splitPaths(paths []string) (folders, files []string) {
	for _, p := range paths {
		info, err := os.Stat(p)
		switch p = filepath.Clean(p); {
		case err != nil:
		case info.IsDir():
			if !slices.Contains(folders, p) {
				folders = append(folders, p)
			}
		case info.Mode().IsRegular():
			files = append(files, p)
		}
	}
	return folders, files
}

// openFolder brings the window that has dir open to the front, or opens
// it in a new window. reuse lets a window that shows nothing take it
// instead, as the one an app opens with.
func (m *mayoi) openFolder(dir string, reuse bool) {
	if w := m.showing(dir); w != nil {
		w.show()
		return
	}
	if reuse {
		for _, w := range m.wins {
			if a := w.a; a.blank() {
				// Its folder at once, so that the next folder looks for
				// another window.
				a.window.Folder = dir
				w.do(func() { a.openSession(dir) })
				w.show()
				return
			}
		}
	}
	for _, w := range m.s.Windows {
		if filepath.Clean(w.Folder) == dir && m.of(w) == nil {
			// As the last session left it.
			m.openWindow(w)
			return
		}
	}
	// With the tabs it had when a window last closed it.
	w := m.s.NewWindow()
	w.Folder = dir
	if ses := m.s.TakeSession(dir); ses != nil {
		w.Session = *ses
	}
	m.openWindow(w)
}

// openFile opens a file in the window whose folder holds it, else in the
// window focused last, else in a new window.
func (m *mayoi) openFile(path string) {
	var win *desktop
	deepest := ""
	for _, w := range m.wins {
		if root := w.a.folder(); root != "" && inFolder(root, path) && len(root) > len(deepest) {
			win, deepest = w, root
		}
	}
	if deepest != "" {
		// As the window spells its folder, which on Windows a path given
		// in other letters, c:\proj for C:\Proj, is too: the file opens
		// once, as the Explorer has it.
		path = filepath.Join(deepest, path[len(strings.TrimSuffix(deepest, string(filepath.Separator))):])
	}
	if win == nil && len(m.wins) > 0 {
		win = m.wins[0]
	}
	if win == nil {
		win = m.openWindow(m.s.NewWindow())
	}
	a := win.a
	win.do(func() { a.openFile(path, true) })
	win.show()
}

// showing returns the window that has dir open.
func (m *mayoi) showing(dir string) *desktop {
	for _, w := range m.wins {
		if w.a.folder() == dir {
			return w
		}
	}
	return nil
}

// of returns the window whose state is st.
func (m *mayoi) of(st *settings.Window) *desktop {
	for _, w := range m.wins {
		if w.a.window == st {
			return w
		}
	}
	return nil
}

// reload applies the preferences of the settings file, which a person
// changed as Mayoi runs.
func (m *mayoi) reload(p settings.Prefs) {
	old := m.s.SetPrefs(p)
	if p.Appearance != old.Appearance || p.Theme != old.Theme {
		mygo.Theme.SetSource(themeSource(p.Theme, p.Appearance))
	}
	if p.ProjectTabs != old.ProjectTabs {
		m.projectTabsChanged()
	}
	if p.Locale != old.Locale {
		applyLocale(p.Locale)
	}
	for _, w := range m.wins {
		if p.AutoSave && !old.AutoSave {
			w.a.autoSaveAll()
		}
		w.a.invalidate()
	}
	m.refreshMenu()
}

// reloadKeys applies the shortcuts of keybindings.json, which a person
// saved as Mayoi runs: those of a file that does not parse stay.
func (m *mayoi) reloadKeys(km *keymap.Map, err error) {
	if len(m.wins) == 0 {
		return
	}
	if err == nil {
		m.keys = km
		for _, w := range m.wins {
			w.a.keys, w.a.chord, w.a.palette, w.a.hints = km, nil, nil, nil
			w.a.invalidate()
		}
		m.refreshMenu()
	}
	front := m.wins[0]
	front.do(func() { front.a.keysFailed(km, err) })
}

// applyLocale shows the app in the language of tag, or of the system's
// languages for "" or a tag that names none.
func applyLocale(tag string) {
	if i18n.Normalize(tag) == "" {
		tag = ""
	}
	mygo.App.SetLocale(tag)
}

// localeChanged says anew what the windows made once in the language they
// showed: the menus and the command palette. The windows draw anew
// themselves.
func (m *mayoi) localeChanged() {
	for _, w := range m.wins {
		w.a.palette, w.a.quick.stale = nil, true
		w.a.invalidate()
	}
	m.menuKey = ""
	m.refreshMenu()
}

// refreshMenu builds the menus anew on the window in front, whose folder
// the recent folders of the File menu leave out.
func (m *mayoi) refreshMenu() {
	if !m.ready || len(m.wins) == 0 {
		return
	}
	front := m.wins[0]
	front.do(func() { m.setMenu(front.a) })
}

// setMenu sets the app's menu as a's window has it, and the Dock's menu of
// a new window and the recent folders.
func (m *mayoi) setMenu(a *app) {
	if runtime.GOOS != "darwin" {
		return
	}
	// Setting a menu bar on Linux makes it anew, which closes a menu that
	// is open, as when the window has the focus again from a submenu's
	// grab: one that does not change stays.
	menu := a.menu(runtime.GOOS == "darwin" && mygo.App.IsPackaged(), m.run)
	if apply := m.menuUpdate(menu); apply != nil {
		if runtime.GOOS == "linux" {
			// The view drains app.post during GTK's draw/render callback.
			// SetMenu destroys the old menu bar, which GTK may still have
			// in its list of children to draw. Dispatch from another
			// goroutine so RunOnMain queues the update instead of running
			// it inline inside that traversal. Do not wait here.
			go mygo.RunOnMain(apply)
		} else {
			apply()
		}
	}
	mygo.App.Dock.SetMenu(m.dockMenu())
}

// menuUpdate captures the menu on the UI thread. Its returned function
// must also run there, after drawing on Linux. Dispatching goroutines may
// reach the main loop out of order, so only the latest revision applies.
func (m *mayoi) menuUpdate(menu *mygo.Menu) func() {
	key := menuKey(menu.Items())
	if key == m.menuKey {
		return nil
	}
	m.menuKey = key
	m.menuRevision++
	revision := m.menuRevision
	return func() {
		if revision == m.menuRevision && !m.quitting {
			mygo.App.SetMenu(menu)
		}
	}
}

// dockMenu is the menu of the Dock icon: a window that shows nothing, and
// the recent folders, each in the window in front, which comes forward.
func (m *mayoi) dockMenu() *mygo.Menu {
	items := []*mygo.MenuItem{{Label: locales.CmdFileNewWindow(), Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
		m.openBlankWindow()
	}}}
	recent := m.s.RecentExcept("")
	if len(recent) > 0 {
		items = append(items, mygo.Separator())
	}
	for _, dir := range recent {
		id := recentCommand + dir
		items = append(items, &mygo.MenuItem{Label: shortenHome(dir), Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
			if len(m.wins) > 0 {
				m.wins[0].show()
			}
			m.run(id, nil)
		}})
	}
	return mygo.NewMenu(items)
}

// menuKey writes out what items show of themselves, their submenus'
// too, the same for menus that look and do the same: an item runs the
// command of its label, in the window it is chosen in.
func menuKey(items []*mygo.MenuItem) string {
	var b strings.Builder
	var write func(items []*mygo.MenuItem)
	write = func(items []*mygo.MenuItem) {
		for _, it := range items {
			fmt.Fprintf(&b, "%q %q %v %v %q %t %t %t %q [", it.ID, it.Label, it.Type, it.Role, it.Accelerator,
				it.Disabled, it.Hidden, it.Checked, it.ToolTip)
			write(it.Submenu)
			b.WriteString("]")
		}
	}
	write(items)
	return b.String()
}

// run runs a command of the menu in the window it was chosen in, or the
// one focused last.
func (m *mayoi) run(id string, win *mygo.Window) {
	if len(m.wins) == 0 {
		return
	}
	// The menu toggled the item chosen, which the command may leave as
	// it was: the menu is set anew as the next one is made.
	m.menuKey = ""
	h := m.wins[0]
	for _, f := range m.frames {
		if f.win == win && f.active != nil {
			h = f.active
		}
	}
	a := h.a
	h.do(func() { a.run(id) })
}

func (m *mayoi) anyDirty() bool {
	return slices.ContainsFunc(m.wins, func(w *desktop) bool { return w.a.anyDirty() })
}

// settle saves the files of wins that save themselves, asks about the
// other changes of each, a window at a time, then calls done, unless a
// change was kept.
func (m *mayoi) settle(wins []*desktop, done func()) {
	if len(wins) == 0 {
		done()
		return
	}
	w, a := wins[0], wins[0].a
	if !a.started {
		// Not shown since the app started, it has nothing open.
		m.settle(wins[1:], done)
		return
	}
	w.do(func() {
		if a.autoSaveAll(); !a.anyDirty() {
			m.settle(wins[1:], done)
			return
		}
		w.show()
		a.closeKeeping(func() { m.settle(wins[1:], done) })
	})
}

// openWindow opens a window for st, which restores what st has of it:
// with project tabs, a tab of the window in front, which shows it.
func (m *mayoi) openWindow(st *settings.Window) *desktop {
	h := &desktop{m: m}
	a := newApp(h, m.s, st)
	a.keys = m.keys
	h.a = a
	if m.s.ProjectTabs && len(m.wins) > 0 && m.wins[0].f != nil {
		f := m.wins[0].f
		f.add(h)
		m.wins = append(m.wins, h)
		f.activate(h)
		return h
	}
	m.newFrame(h)
	m.wins = slices.Insert(m.wins, 0, h)
	m.s.Focused(st)
	return h
}

// openBlankWindow opens a window that shows nothing, which the welcome
// page fills with the recent folders and the hosts. It is the window the
// app starts with when it was given nothing to open, and the one the Dock
// icon's and the File menu's New Window open.
func (m *mayoi) openBlankWindow() { m.openWindow(m.s.NewWindow()) }

func (a *app) anyDirty() bool {
	for _, d := range a.docs {
		if d.dirty() {
			return true
		}
	}
	return false
}

// appBundle is the .app the running executable is in, "" when it is in
// none.
func appBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return ""
	}
	if app, _, ok := strings.Cut(exe, ".app/Contents/MacOS/"); ok {
		return app + ".app"
	}
	return ""
}
