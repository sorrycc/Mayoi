package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/keymap"
	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
	bundled "mayoi/plugins"
)

// pluginHost is the app's side of the plugins: the registry, and what the
// plugins show, which every window draws.
type pluginHost struct {
	reg *plugin.Registry
	// apps are the windows' apps, the one in front first; onMain runs a
	// function on the main thread; prefs are the settings; menu builds the
	// menus anew. The tests give their own.
	apps   func() []*app
	onMain func(func())
	prefs  func() *settings.Settings
	menu   func()
	// prefsWin draws the settings window anew, whose Plugins page shows
	// how the plugins do; nil in the tests that have none.
	prefsWin func()

	// prefsSnap and folderSnap are the settings and the folder of the
	// window in front as the main thread last saw them, for the plugins'
	// goroutines to read.
	prefsSnap  atomic.Pointer[settings.Prefs]
	folderSnap atomic.Pointer[string]

	mu   sync.Mutex
	logs []plugin.LogLine // of every plugin, the last maxPluginLog
	// status are the items of the status bar the plugins filled, by id.
	status map[string]pluginStatus
	// views are the trees the plugins rendered last, by view id.
	views map[string]*viewNode
	// progress are the titles of the work plugins said they do.
	progress []string
	// diags are the diagnostics of each file, by path, then plugin.
	diags    map[string]map[string][]diagnostic
	diagsGen uint64
	// gen counts the changes of the plugins and their contributions,
	// which the windows build their menus and palettes anew for.
	gen uint64

	// syncing has the plugins follow settings sync, as the app does but
	// the tests don't; syncBusy and syncAgain run one sync at a time, and
	// records are plugins.json as the last agreed it.
	syncing, syncBusy, syncAgain bool
	records                      plugin.Records
	// updates are the newer commits found of the plugins from Git.
	updates map[string]string

	// edIDs number the editors plugins were told of; main thread only.
	edIDs  map[*editor]int
	edNext int
}

// maxPluginLog is how many lines the log of every plugin keeps.
const maxPluginLog = 2000

// plugins is the app's plugins, nil until the app starts them (and in
// most tests).
var plugins *pluginHost

// startPlugins finds the plugins and watches their folders.
func (m *mayoi) startPlugins(safe bool) {
	h := newPluginHost(
		func() []*app {
			apps := make([]*app, len(m.wins))
			for i, w := range m.wins {
				apps[i] = w.a
			}
			return apps
		},
		mygo.RunOnMain,
		func() *settings.Settings { return m.s },
		func() {
			m.menuKey = ""
			m.refreshMenu()
		},
	)
	h.prefsWin = m.prefsChanged
	dirs := plugin.UserDirs()
	dirs.Bundled = bundled.FS
	h.reg = plugin.New(dirs, h)
	h.reg.Version = appVersion()
	h.reg.Locale = i18n.Locale
	h.reg.SetSafe(safe)
	plugins = h
	highlight.LexerName = pluginLexer
	h.syncPrefs()
	h.reg.Load()
	if stop, err := h.reg.Watch(); err == nil {
		mygo.App.OnQuit(stop)
	}
	mygo.App.OnQuit(h.reg.Close)
	mygo.App.OnQuit(stopServers)
	h.reg.Activate("onStartup")
	h.syncing = true
	h.syncPlugins()
	h.checkPluginUpdates()
}

// newPluginHost makes the app's side of the plugins, without a registry.
func newPluginHost(apps func() []*app, onMain func(func()), prefs func() *settings.Settings, menu func()) *pluginHost {
	return &pluginHost{apps: apps, onMain: onMain, prefs: prefs, menu: menu, status: map[string]pluginStatus{},
		views: map[string]*viewNode{}, diags: map[string]map[string][]diagnostic{}, edIDs: map[*editor]int{}}
}

// syncPrefs keeps a copy of the settings for the plugins' goroutines; on
// the main thread, as the settings change.
func (h *pluginHost) syncPrefs() {
	if s := h.prefs(); s != nil {
		p := s.Prefs
		h.prefsSnap.Store(&p)
	}
}

// toggled stops the plugins the settings turned off and starts those
// they turned on, from old to now, as they would start had Mayoi started
// with them on.
func (h *pluginHost) toggled(old, now map[string]json.RawMessage) {
	on := func(all map[string]json.RawMessage, name string) bool {
		var s struct {
			Enabled *bool `json:"enabled"`
		}
		json.Unmarshal(all[name], &s)
		return s.Enabled == nil || *s.Enabled
	}
	started := false
	for _, p := range h.reg.Plugins() {
		was, is := on(old, p.Name), on(now, p.Name)
		if was == is {
			continue
		}
		if !is {
			h.mu.Lock()
			for id, st := range h.status {
				if st.plugin == p {
					delete(h.status, id)
				}
			}
			h.mu.Unlock()
		}
		h.reg.Toggled(p.Name)
		started = started || is
	}
	if !started {
		return
	}
	// The plugins turned on start for the folders and files open, as
	// those opening would start them.
	for _, a := range h.apps() {
		a.pluginSeen.indexed = false
		for _, d := range a.docs {
			h.reg.Activate("onLanguage:" + languageOf(d))
		}
		a.invalidate()
	}
}

// front is the app of the window in front, nil for none.
func (h *pluginHost) front() *app {
	if apps := h.apps(); len(apps) > 0 {
		return apps[0]
	}
	return nil
}

// enabled are the plugins on, none before the app starts them.
func (h *pluginHost) enabled() []*plugin.Plugin {
	if h == nil {
		return nil
	}
	return h.reg.Enabled()
}

// Settings implements plugin.Host.
func (h *pluginHost) Settings(name string) json.RawMessage {
	p := h.prefsSnap.Load()
	if p == nil {
		return nil
	}
	return p.Plugins[name]
}

// MayoiSetting implements plugin.Host: a preference by its key in
// settings.json.
func (h *pluginHost) MayoiSetting(key string) any {
	p := h.prefsSnap.Load()
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var all map[string]any
	json.Unmarshal(data, &all)
	return all[key]
}

// Folder implements plugin.Host.
func (h *pluginHost) Folder() string {
	if f := h.folderSnap.Load(); f != nil {
		return *f
	}
	return ""
}

// Changed implements plugin.Host: the menus and palettes are made anew.
func (h *pluginHost) Changed() {
	h.mu.Lock()
	h.gen++
	h.mu.Unlock()
	h.onMain(func() {
		for _, a := range h.apps() {
			a.palette, a.hints = nil, nil
			a.invalidate()
		}
		h.menu()
		h.redrawPrefs()
	})
	h.recordsChanged()
}

// Logged implements plugin.Host.
func (h *pluginHost) Logged(p *plugin.Plugin, line plugin.LogLine) {
	h.mu.Lock()
	h.logs = append(h.logs, line)
	if len(h.logs) > maxPluginLog {
		h.logs = slices.Delete(h.logs, 0, len(h.logs)-maxPluginLog)
	}
	h.mu.Unlock()
	if line.Level == "error" {
		h.onMain(func() {
			h.redraw()
			h.redrawPrefs()
		})
	}
}

// redrawPrefs draws the settings window anew, if it is open; on the main
// thread.
func (h *pluginHost) redrawPrefs() {
	if h.prefsWin != nil {
		h.prefsWin()
	}
}

// errNoWindow is a call's error while no window is open.
var errNoWindow = errors.New("no window is open")

// main runs fn on the main thread with the app of the window in front,
// and waits for what it returns.
func (h *pluginHost) main(fn func(a *app) (any, error)) (any, error) {
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	h.onMain(func() {
		a := h.front()
		if a == nil {
			ch <- result{nil, errNoWindow}
			return
		}
		v, err := fn(a)
		ch <- result{v, err}
		a.invalidate()
	})
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(30 * time.Second):
		return nil, errors.New("the app did not answer")
	}
}

// wait runs start on the main thread with the app in front, which calls
// done once there is an answer, as after the user chose: then returns it.
func (h *pluginHost) wait(start func(a *app, done func(any, error))) (any, error) {
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	var once sync.Once
	done := func(v any, err error) { once.Do(func() { ch <- result{v, err} }) }
	h.onMain(func() {
		a := h.front()
		if a == nil {
			done(nil, errNoWindow)
			return
		}
		start(a, done)
		a.invalidate()
	})
	r := <-ch
	return r.v, r.err
}

// pluginCommands are the commands of the plugins, labelled in the
// language of t, after Mayoi's own, with the keys of plugin.json those
// leave free.
func (a *app) pluginCommands(t locales.Messages, own []command) []command {
	if plugins == nil {
		return nil
	}
	tag := i18n.Locale()
	var cmds []command
	for _, pc := range plugins.reg.Commands(tag) {
		id := pc.ID
		cmds = append(cmds, command{id, pc.Title, "", func() { plugins.runCommand(a, id, "") }})
	}
	for _, th := range pluginThemes() {
		id := th.contrib.ID
		cmds = append(cmds, command{"view.theme." + id, th.contrib.Label, "", func() { a.setTheme(id) }})
	}
	cmds = append(cmds,
		command{"plugins.reload", t.CmdPluginsReload(), "", func() { plugins.reg.ReloadAll() }},
		command{"plugins.log", t.CmdPluginsLog(), "", a.showPluginLog},
		command{"plugins.running", t.CmdPluginsRunning(), "", a.showRunningPlugins},
		command{"plugins.openFolder", t.CmdPluginsOpenFolder(), "", a.openPluginsFolder},
		command{"plugins.openScripts", t.CmdPluginsOpenScripts(), "", a.openScriptsFolder},
		command{"plugins.runSelection", t.CmdPluginsRunSelection(), "", a.runSelectionScript},
		command{"plugins.manage", t.CmdPluginsManage(), "", func() { a.host.showSettings(prefsPlugins, "") }},
	)
	// The keys plugins bind, where Mayoi and keybindings.json leave them.
	taken := map[keymap.Stroke]bool{}
	for _, c := range own {
		if st, ok := keymap.ParseAccel(platformAccel(c.id, c.accel)); ok {
			taken[st] = true
		}
	}
	for _, p := range plugins.enabled() {
		for _, k := range p.Contributes.Keybindings {
			st, ok := pluginStroke(k)
			if !ok || taken[st] || a.keys.Binds(st) {
				continue
			}
			for i := range cmds {
				if cmds[i].id == k.Command && cmds[i].accel == "" {
					cmds[i].accel = st.Accel()
					taken[st] = true
				}
			}
		}
	}
	return cmds
}

// pluginStroke is the keystroke of a plugin's keybinding on this system,
// its mac key on macOS; a chord has none.
func pluginStroke(k plugin.KeyContrib) (keymap.Stroke, bool) {
	key := k.Key
	if runtime.GOOS == "darwin" && k.Mac != "" {
		key = k.Mac
	}
	strokes, err := keymap.ParseKeys(key)
	if err != nil || len(strokes) != 1 {
		return keymap.Stroke{}, false
	}
	return strokes[0], true
}

// skippedKeys are the keybindings of plugins that keys Mayoi or the user
// bind took, for the shortcuts editor to offer.
func (a *app) skippedKeys() []plugin.KeyContrib {
	if plugins == nil {
		return nil
	}
	got := map[string]bool{}
	for _, c := range a.commands() {
		if c.accel != "" {
			got[c.id+"\x00"+c.accel] = true
		}
	}
	var out []plugin.KeyContrib
	for _, p := range plugins.enabled() {
		for _, k := range p.Contributes.Keybindings {
			st, ok := pluginStroke(k)
			if !ok || got[k.Command+"\x00"+st.Accel()] || slices.Equal(a.keys.Last(k.Command), []keymap.Stroke{st}) {
				continue
			}
			k.Key = st.String()
			out = append(out, k)
		}
	}
	return out
}

// runCommand runs a command of a plugin, telling of what goes wrong.
func (h *pluginHost) runCommand(a *app, id, arg string) {
	go func() {
		var argv any
		if arg != "" {
			argv = arg
		}
		if _, err := h.reg.Run(id, argv); err != nil && !errors.Is(err, plugin.ErrNoCommand) {
			a.post(func() { a.fail("%s", locales.PluginCommandFailed(id, firstLine(err.Error()))) })
		}
	}()
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// pluginMenu is the Plugins menu: each plugin's commands its manifest
// puts there, or all of a plugin's commands when it names none, then the
// commands of the plugins themselves.
func (a *app) pluginMenu(item func(id string) *mygo.MenuItem) *mygo.MenuItem {
	var items []*mygo.MenuItem
	if plugins != nil {
		for _, p := range plugins.enabled() {
			ids := []string{}
			if menu, ok := p.Contributes.Menus["plugins"]; ok {
				for _, mc := range menu {
					ids = append(ids, mc.Command)
				}
			} else {
				for _, c := range p.Contributes.Commands {
					ids = append(ids, c.ID)
				}
			}
			if len(ids) == 0 {
				continue
			}
			var sub []*mygo.MenuItem
			for _, id := range ids {
				if it := item(id); it.Label != "" {
					sub = append(sub, it)
				}
			}
			if len(sub) > 0 {
				items = append(items, &mygo.MenuItem{Label: p.Title(), Submenu: sub})
			}
		}
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items, item("plugins.manage"), item("plugins.reload"), item("plugins.log"), item("plugins.running"), item("plugins.problems"), mygo.Separator(),
		item("plugins.openFolder"), item("plugins.openScripts"), item("plugins.runSelection"))
	return &mygo.MenuItem{Label: locales.MenuPlugins(), Submenu: items}
}

// pluginMenuItems puts the commands plugins add to a context menu, as
// "explorer/context", in m, then a separator: each runs with arg, the
// path the menu is of.
func (a *app) pluginMenuItems(m *ui.Menu, where, arg string) {
	if plugins == nil {
		return
	}
	labels := map[string]string{}
	for _, c := range plugins.reg.Commands(i18n.Locale()) {
		labels[c.ID] = c.Title
	}
	added := false
	for _, p := range plugins.enabled() {
		for _, mc := range p.Contributes.Menus[where] {
			if label, ok := labels[mc.Command]; ok {
				if m.Item(label).Chosen() {
					id := mc.Command
					a.post(func() { plugins.runCommand(a, id, arg) })
				}
				added = true
			}
		}
	}
	if added {
		m.Separator()
	}
}

// pluginLinkRes are the patterns of the plugins' terminal links, compiled.
var pluginLinkRes sync.Map // string -> *regexp.Regexp

// pluginTerminalLink runs the command of a plugin's terminal link whose
// pattern a link clicked in a terminal matches, with the match.
func (a *app) pluginTerminalLink(link string) bool {
	if plugins == nil {
		return false
	}
	for _, p := range plugins.enabled() {
		for _, l := range p.Contributes.TerminalLinks {
			v, ok := pluginLinkRes.Load(l.Pattern)
			if !ok {
				re, err := regexp.Compile(l.Pattern)
				if err != nil {
					continue
				}
				v, _ = pluginLinkRes.LoadOrStore(l.Pattern, re)
			}
			if match := v.(*regexp.Regexp).FindString(link); match != "" {
				plugins.runCommand(a, l.Command, match)
				return true
			}
		}
	}
	return false
}

// openPluginsFolder shows the folder of the plugins in the system's file
// manager, made first.
func (a *app) openPluginsFolder() {
	dir := plugin.UserDirs().Plugins
	os.MkdirAll(dir, 0o755)
	mygo.Shell.OpenPath(dir)
}

// openScriptsFolder opens the folder of user scripts, with an example the
// first time.
func (a *app) openScriptsFolder() {
	dir := plugin.UserDirs().Scripts
	os.MkdirAll(dir, 0o755)
	example := filepath.Join(dir, "example.ts")
	if entries, _ := os.ReadDir(dir); len(entries) == 0 {
		os.WriteFile(example, []byte(scriptExample), 0o644)
		os.WriteFile(filepath.Join(dir, "mayoi.d.ts"), []byte(plugin.TypesSource), 0o644)
	}
	if _, err := os.Stat(example); err == nil {
		a.openFile(example, true)
		return
	}
	mygo.Shell.OpenPath(dir)
}

// scriptExample is the user script Open Scripts Folder writes first.
const scriptExample = `// Each function this file exports is a command: Script: Upper Case.
// It gets the selection, or what mayoi run script.example.upperCase=<text> gives,
// and returns what replaces the selection. See docs/PLUGINS.md.
import { editor } from "mayoi";

export async function upperCase(arg?: string) {
  if (arg !== undefined) return arg.toUpperCase();
  const ed = await editor.active();
  if (!ed) return;
  const text = await ed.document.text(selectionRange(ed.selection));
  await ed.insert(text.toUpperCase());
}

function selectionRange(s: { anchor: { line: number; character: number }; active: { line: number; character: number } }) {
  const before = s.anchor.line < s.active.line || (s.anchor.line === s.active.line && s.anchor.character <= s.active.character);
  return before ? { start: s.anchor, end: s.active } : { start: s.active, end: s.anchor };
}
`

// runSelectionScript runs the selection, or the whole file, as a plugin
// of its own, once: its output goes to the log.
func (a *app) runSelectionScript() {
	d := a.activeEditor()
	if d == nil || plugins == nil {
		return
	}
	snap := d.buf.Snapshot()
	text := snap.String()
	if s, e := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1]); s != e {
		text = snap.Slice(s, e)
	}
	ext := ".js"
	if l := languageOf(d.document); l == "typescript" || l == "tsx" {
		ext = ".ts"
	}
	go func() {
		res, err := plugins.reg.RunOnce(text, ext)
		a.post(func() {
			if err != nil {
				a.fail("%s", locales.PluginScriptFailed(firstLine(err.Error())))
				return
			}
			if len(res) > 0 && string(res) != "null" {
				a.tell(note{message: locales.PluginScriptResult(truncate(string(res), 200))})
			}
		})
	}()
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// showPluginLog opens the log of every plugin in a tab.
func (a *app) showPluginLog() { a.showPluginLogOf(nil) }

// showPluginLogOf opens the log of the plugin p in a tab, of every plugin
// for nil.
func (a *app) showPluginLogOf(p *plugin.Plugin) {
	if plugins == nil {
		return
	}
	plugins.mu.Lock()
	lines := slices.Clone(plugins.logs)
	plugins.mu.Unlock()
	title := locales.PluginLogTitle()
	if p != nil {
		title = locales.PluginLogOf(p.Title())
	}
	var b strings.Builder
	for _, l := range lines {
		if p == nil {
			fmt.Fprintf(&b, "%s [%s] %s: %s\n", l.Time.Format("15:04:05.000"), l.Level, l.Plugin, l.Message)
		} else if l.Plugin == p.Name {
			fmt.Fprintf(&b, "%s [%s] %s\n", l.Time.Format("15:04:05.000"), l.Level, l.Message)
		}
	}
	switch {
	case b.Len() > 0:
	case p != nil:
		b.WriteString(locales.PluginsLogNone() + "\n")
	default:
		b.WriteString(locales.PluginLogEmpty() + "\n")
	}
	a.openGenerated(title, b.String())
}

// showRunningPlugins opens a report of the plugins: whether each runs,
// how long it took to start, and what it used.
func (a *app) showRunningPlugins() {
	if plugins == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-24s %-9s %10s %10s %10s  %s\n", "PLUGIN", "STATE", "STARTED IN", "CPU", "MEMORY", "LAST ERROR")
	for _, p := range plugins.reg.Plugins() {
		st := p.Stats()
		state := "idle"
		switch {
		case st.Disabled:
			state = "disabled"
		case st.Crashed:
			state = "stopped"
		case st.Active:
			state = "running"
		case !p.HasCode():
			state = "declares"
		}
		mem := "-"
		if st.Memory > 0 {
			mem = fmt.Sprintf("%.1f MiB", float64(st.Memory)/(1<<20))
		}
		started := "-"
		if st.ActivatedIn > 0 {
			started = st.ActivatedIn.Round(time.Millisecond).String()
		}
		fmt.Fprintf(&b, "%-24s %-9s %10s %10s %10s  %s\n", p.Name, state, started, st.CPU.Round(time.Millisecond), mem, firstLine(st.Err))
	}
	a.openGenerated(locales.PluginRunningTitle(), b.String())
}

// openGenerated opens text in an untitled tab, as a report.
func (a *app) openGenerated(title, text string) {
	a.untitled++
	col := a.fileColumn()
	a.addUntitled(col, a.untitled, text)
	t := col.tabs[len(col.tabs)-1]
	if t.ed != nil {
		t.ed.title = title
		t.ed.saved = t.ed.buf.Version()
	}
	a.activate(col, len(col.tabs)-1, true)
}

// languageOf is the language plugins know a document by: the lexer's
// name in lower case, as "go" or "typescript", or a language a plugin
// declares for its file.
func languageOf(d *document) string {
	if id := pluginLanguage(d.path); id != "" {
		return id
	}
	var name string
	if d.hl != nil {
		name = highlight.Name(d.hl.Lexer())
	} else {
		name = highlight.Name(highlight.Lexer(d.path))
	}
	return strings.ToLower(strings.ReplaceAll(name, " ", ""))
}

// prefsPlugins is the settings window's section of the plugins.
const prefsPlugins = "plugins"

// pluginCLI runs mayoi plugin, when the command is that: it works on the
// plugins folder, without the app.
func pluginCLI(args []string) bool {
	if len(args) == 0 || args[0] != "plugin" {
		return false
	}
	os.Exit(plugin.RunCLI(args[1:], plugin.UserDirs(), os.Stdout, os.Stderr))
	return true
}
