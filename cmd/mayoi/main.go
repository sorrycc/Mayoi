// Mayoi is a small, quick code editor: a file tree, tabs of editors and a
// terminal panel, drawn by MyGo's native UI.
package main

import (
	"errors"
	"fmt"
	"log"
	"mayoi/internal/cli"
	"mayoi/internal/keymap"
	"mayoi/internal/remotefs"
	"mayoi/internal/settings"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

// desktop is the host of a window of the app: native dialogs, the trash.
type desktop struct {
	m   *mayoi
	a   *app
	win *mygo.Window
}

// mayoi is the app's windows, the focused one last first.
type mayoi struct {
	s    *settings.Settings
	keys *keymap.Map // the shortcuts of keybindings.json
	wins []*desktop
	// pending are the folders and files the app was asked to open before
	// it was ready.
	ready   bool
	pending []string
	// quitting is set once the app quits, so that the windows it closes
	// stay in the settings, to open again at the next start.
	quitting bool
}

func (h *desktop) openFolder(done func(string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: h.win, Title: "Open Folder", ButtonLabel: "Open", Directory: true, CreateDirectories: true,
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
			Parent: h.win, Title: "Save As", DefaultPath: filepath.Join(dir, name), CreateDirectories: true,
		})
		if err == nil && path != "" {
			h.a.post(func() { done(path) })
		}
	}()
}

func (h *desktop) confirmClose(name string, done func(int)) {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: h.win, Type: mygo.MessageWarning,
			Message: "Do you want to save the changes you made to " + name + "?",
			Detail:  "Your changes will be lost if you don't save them.",
			Buttons: []string{"Save", "Don't Save", "Cancel"}, DefaultButton: 0, CancelButton: 2,
		})
		choice := 2
		if err == nil {
			choice = res.Button
		}
		h.a.post(func() { done(choice) })
	}()
}

func (h *desktop) trash(path string) error {
	if remotefs.IsRemote(path) {
		return errors.New("Remote Trash is unavailable. Use the SSH terminal to remove remote files")
	}
	return mygo.Shell.TrashItem(path)
}
func (h *desktop) reveal(path string) {
	if remotefs.IsRemote(path) {
		h.a.tell(note{message: remotefs.Display(path)})
		return
	}
	mygo.Shell.ShowItemInFolder(path)
}
func (h *desktop) copyFiles(paths []string) error {
	return mygo.Clipboard.WriteFiles(paths...)
}
func (h *desktop) clipboardFiles() []string {
	paths, _ := mygo.Clipboard.ReadFiles()
	return paths
}
func (h *desktop) setTitle(title string) { h.win.SetTitle(title) }
func (h *desktop) closeWindow()          { h.win.Close() }
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

func (h *desktop) openFolders(dirs []string) {
	for _, dir := range dirs {
		h.m.openFolder(dir, true)
	}
}

func (h *desktop) recentChanged() { h.m.refreshMenu() }

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
			h.tell(mygo.MessageWarning, "The 'mayoi' command could not be installed.", err.Error())
		default:
			h.tell(mygo.MessageInfo, "The 'mayoi' command is installed in "+shortenHome(filepath.Dir(p))+".",
				"In a terminal, mayoi . opens the folder you are in, and mayoi <file> a file.")
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
			h.tell(mygo.MessageWarning, "The 'mayoi' command could not be uninstalled.", err.Error())
		case len(removed) == 0:
			h.tell(mygo.MessageInfo, "The 'mayoi' command is not installed.", "")
		default:
			h.tell(mygo.MessageInfo, "The 'mayoi' command is uninstalled.", "It was in "+shortenHome(filepath.Dir(removed[0]))+".")
		}
	}()
}

// tell shows a message on the window.
func (h *desktop) tell(kind mygo.MessageType, message, detail string) {
	mygo.Dialog.Message(mygo.MessageOptions{Parent: h.win, Type: kind, Message: message, Detail: detail})
}

// show brings the window to the front.
func (h *desktop) show() {
	if h.win.IsMinimized() {
		h.win.Restore()
	}
	h.win.Focus()
}

// do runs fn on the window's app before its next frame.
func (h *desktop) do(fn func()) {
	h.a.post(fn)
	h.win.Invalidate()
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
	// One Mayoi runs: the folders and files another is started with open
	// in this one.
	if os.Getenv("MAYOI_SNAPSHOT") == "" && !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// Release builds check for updates once a day and offer them in
	// MyGo's update window; development builds never check.
	mygo.Use(native.Plugin)
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
	mygo.App.OnOpenURL(func(raw string) {
		if !strings.HasPrefix(raw, "mayoi-ssh://") {
			return
		}
		paths := argPaths([]string{strings.TrimPrefix(raw, "mayoi-")}, "")
		if !m.ready {
			m.pending = append(m.pending, paths...)
			return
		}
		m.open(paths)
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
	mygo.App.OnQuit(func() { remotefs.CloseAll(); m.s.Flush() })
	mygo.App.WhenReady(func() {
		m.s = settings.Load(settingsPath())
		mygo.Theme.SetSource(themeSource(m.s.Theme, m.s.Appearance))
		if stop, err := m.s.Watch(func(p settings.Prefs) { mygo.RunOnMain(func() { m.reload(p) }) }); err == nil {
			mygo.App.OnQuit(stop)
		}
		keysPath := keymap.Path(m.s.Path())
		keys, keysErr := keymap.Load(keysPath)
		m.keys = keys
		if stop, err := keymap.Watch(keysPath, func(km *keymap.Map, err error) { mygo.RunOnMain(func() { m.reloadKeys(km, err) }) }); err == nil {
			mygo.App.OnQuit(stop)
		}
		wd, _ := os.Getwd()
		m.ready = true
		m.launch(append(m.pending, argPaths(os.Args[1:], wd)...))
		m.pending = nil
		if len(m.wins) == 0 {
			return
		}
		first := m.wins[0]
		first.a.keysFailed(keys, keysErr)
		if os.Getenv("MAYOI_DATA_DIR") == "" {
			go func() {
				home, _ := os.UserHomeDir()
				if err := cli.RefreshInstalled(cli.Dirs(home, cli.ShellPath()), appBundle()); err != nil {
					first.a.post(func() { first.a.tell(note{message: "Reinstall the mayoi command from File to enable SSH URLs"}) })
				}
			}()
		}
		m.setMenu(first.a)
		if out := os.Getenv("MAYOI_SNAPSHOT"); out != "" {
			// For checking the real window: capture it once it settles, and
			// quit.
			a, win := first.a, first.win
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
		if strings.HasPrefix(arg, "ssh://") {
			if p, err := remotefs.Parse(arg); err == nil {
				paths = append(paths, p)
			} else {
				log.Print(err)
			}
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
	if len(folders) == 0 {
		// The one focused last ends up in front.
		for _, w := range slices.Backward(slices.Clone(m.s.Windows)) {
			m.openWindow(w)
		}
	} else {
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
		m.openWindow(m.s.NewWindow())
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
		if remotefs.IsRemote(p) {
			folders = append(folders, p)
			continue
		}
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
		if root := w.a.folder(); root != "" && strings.HasPrefix(path, root+string(filepath.Separator)) && len(root) > len(deepest) {
			win, deepest = w, root
		}
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
			w.a.keys, w.a.chord, w.a.palette = km, nil, nil
			w.a.invalidate()
		}
		m.refreshMenu()
	}
	front := m.wins[0]
	front.do(func() { front.a.keysFailed(km, err) })
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
// the recent folders.
func (m *mayoi) setMenu(a *app) {
	mygo.App.SetMenu(a.menu(runtime.GOOS == "darwin" && mygo.App.IsPackaged(), m.run))
	if runtime.GOOS != "darwin" {
		return
	}
	var items []*mygo.MenuItem
	for _, dir := range m.s.RecentExcept("") {
		id := recentCommand + dir
		// In the window in front, which comes forward.
		items = append(items, &mygo.MenuItem{Label: shortenHome(dir), Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
			if len(m.wins) > 0 {
				m.wins[0].show()
			}
			m.run(id, nil)
		}})
	}
	var dock *mygo.Menu
	if len(items) > 0 {
		dock = mygo.NewMenu(items)
	}
	mygo.App.Dock.SetMenu(dock)
}

// run runs a command of the menu in the window it was chosen in, or the
// one focused last.
func (m *mayoi) run(id string, win *mygo.Window) {
	if len(m.wins) == 0 {
		return
	}
	h := m.wins[0]
	for _, w := range m.wins {
		if w.win == win {
			h = w
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
	w.do(func() {
		if a.autoSaveAll(); !a.anyDirty() {
			m.settle(wins[1:], done)
			return
		}
		w.show()
		a.closeKeeping(func() { m.settle(wins[1:], done) })
	})
}

// openWindow opens a window for st, which restores what st has of it.
func (m *mayoi) openWindow(st *settings.Window) *desktop {
	h := &desktop{m: m}
	a := newApp(h, m.s, st)
	a.keys = m.keys
	h.a = a
	key := "main"
	if st.ID > 0 {
		key = fmt.Sprintf("main-%d", st.ID)
	}
	opts := mygo.WindowOptions{
		Title: "Mayoi", Width: 1280, Height: 820, MinWidth: 640, MinHeight: 420,
		StateKey:             key,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 14, Y: 13},
		BackgroundColor:      "#131317",
		Content:              ui.View(a.view),
	}
	if len(m.wins) > 0 {
		// Below and right of the window in front, unless the frame of
		// st's window is remembered.
		opts.X, opts.Y = m.wins[0].win.Position()
		opts.X, opts.Y = opts.X+28, opts.Y+28
	}
	win := mygo.NewWindow(opts)
	h.win = win
	m.wins = slices.Insert(m.wins, 0, h)
	m.s.Focused(st)

	// Closing the window saves the files that save themselves, and with
	// other unsaved changes asks about each first.
	closing := false
	win.OnClose(func(e *mygo.CloseEvent) {
		if closing || m.quitting || !a.anyDirty() {
			return
		}
		e.PreventDefault()
		a.post(func() {
			done := func() {
				closing = true
				win.Close()
			}
			if a.autoSaveAll(); !a.anyDirty() {
				done()
				return
			}
			a.closeKeeping(done)
		})
		win.Invalidate()
	})
	// Back from another app, which may have committed.
	win.OnFocus(func() {
		if i := slices.Index(m.wins, h); i > 0 {
			m.wins = slices.Insert(slices.Delete(m.wins, i, i+1), 0, h)
		}
		m.s.Focused(st)
		a.post(func() { a.blurred = false })
		a.post(a.gitChanged)
		// The File menu's recent folders leave out this one's.
		m.refreshMenu()
	})
	win.OnBlur(func() {
		a.post(func() { a.blurred = true })
		a.post(a.autoSaveAll)
	})
	// Files dropped where nothing takes them, as on the explorer, open in
	// the column that has the keyboard.
	win.OnFileDrop(func(e *mygo.FileDropEvent) {
		paths := e.Paths
		a.post(func() {
			col := a.column()
			a.dropFiles(paths, col, col.active+1)
		})
	})
	// The last window to close quits the app, and stays in the settings
	// to open again, as do those the quitting closes. The others keep
	// their folders' sessions.
	win.OnClosed(func() {
		a.keepSession()
		a.remoteGeneration++
		a.remotePicker = nil
		if a.tree != nil {
			a.tree.Close()
		}
		a.closeTerminals()
		m.wins = slices.DeleteFunc(m.wins, func(w *desktop) bool { return w == h })
		if !m.quitting && len(m.wins) > 0 {
			m.s.Forget(st)
		}
		if len(m.wins) == 0 {
			m.s.Flush()
		}
	})
	return h
}

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
