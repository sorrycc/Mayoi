package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing/fstest"
	"time"
	"unicode"

	"github.com/MoeForge/moejs"
)

// Host is the app, as plugins see it.
type Host interface {
	// Call runs a call of a plugin to the app, on a goroutine of its own,
	// which may wait for the app's main thread. The result is marshalled
	// to JSON for the plugin.
	Call(p *Plugin, method string, args json.RawMessage) (any, error)
	// Settings are the plugin's object of plugins.<name> in settings.json,
	// nil for none.
	Settings(name string) json.RawMessage
	// MayoiSetting is a setting of Mayoi's own, by its key in settings.json.
	MayoiSetting(key string) any
	// Folder is the folder of the window in front, "" for none.
	Folder() string
	// Changed tells that the plugins, what they contribute, or their state
	// changed. It is called on any goroutine.
	Changed()
	// Logged tells of a line a plugin logged, on any goroutine.
	Logged(p *Plugin, line LogLine)
}

// Dirs are where plugins come from.
type Dirs struct {
	Plugins string // installed plugins, a folder each, and plugins.json
	Scripts string // user scripts, a file each
	Storage string // what plugins store
	// Bundled are the plugins in the app, a folder each.
	Bundled fs.FS
}

// LogLine is a line a plugin logged, or Mayoi logged of it.
type LogLine struct {
	Time    time.Time
	Plugin  string
	Level   string // "debug", "info", "warn" or "error"
	Message string
}

// maxLog is how many lines a plugin's log keeps.
const maxLog = 500

// maxCrashes is how many times in a row a plugin may stop before it is
// disabled until reloaded.
const maxCrashes = 3

// Plugin is a plugin Mayoi found: installed, bundled, or a user script.
type Plugin struct {
	Manifest
	// Dir is the plugin's folder, "" for a bundled one.
	Dir     string
	Bundled bool
	// Script is set for a user script, whose commands are the functions
	// it exports.
	Script  bool
	exports []string
	fsys    fs.FS
	reg     *Registry
	sig     string // the files as they were read, to tell a change

	mu          sync.Mutex
	in          *instance
	starting    chan struct{}
	loadErr     string // what reading it failed with: it cannot start
	lastErr     string
	crashes     int
	activatedIn time.Duration
	startedAt   time.Time
	dynamic     []CommandContrib
	subs        map[string]bool
	hovers      []string
	logs        []LogLine
	// queueing is set while launch starts the plugin, and queued are the
	// events sent meanwhile.
	queueing bool
	queued   []queuedEvent
}

type queuedEvent struct {
	name string
	data any
}

// Stats is how a plugin is doing.
type Stats struct {
	Active      bool
	Disabled    bool // by its settings
	Crashed     bool // stopped too often, until reloaded
	ActivatedIn time.Duration
	StartedAt   time.Time // when it last started, zero while it does not run
	CPU         time.Duration
	Memory      int64
	Crashes     int
	Err         string
}

// Title is the plugin's name to show.
func (p *Plugin) Title() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// Stats tells how the plugin is doing.
func (p *Plugin) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Stats{Active: p.in != nil, ActivatedIn: p.activatedIn, Crashes: p.crashes, Crashed: p.crashes >= maxCrashes,
		Err: p.lastErr, Disabled: !p.reg.enabled(p)}
	if p.loadErr != "" {
		st.Err = p.loadErr
	}
	if p.in != nil {
		st.CPU, st.Memory = p.in.stats()
		st.StartedAt = p.startedAt
	}
	return st
}

// Logs are the lines the plugin logged last.
func (p *Plugin) Logs() []LogLine {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.logs)
}

// Subscribed reports whether the active plugin listens to an event.
func (p *Plugin) Subscribed(event string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in != nil && p.subs[event]
}

// HoverLanguages are the languages the plugin answers hovers of.
func (p *Plugin) HoverLanguages() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.hovers)
}

// FS is the plugin's folder, for the files its contributions name.
func (p *Plugin) FS() fs.FS { return p.fsys }

// Log logs a line of Mayoi's about the plugin, as of a language server it
// declares.
func (p *Plugin) Log(level, msg string) { p.logf(level, "%s", msg) }

func (p *Plugin) logf(level, format string, args ...any) {
	line := LogLine{Time: time.Now(), Plugin: p.Name, Level: level, Message: fmt.Sprintf(format, args...)}
	p.mu.Lock()
	p.logs = append(p.logs, line)
	if len(p.logs) > maxLog {
		p.logs = slices.Delete(p.logs, 0, len(p.logs)-maxLog)
	}
	p.mu.Unlock()
	if p.reg.host != nil {
		p.reg.host.Logged(p, line)
	}
}

// Registry holds the plugins and runs them.
type Registry struct {
	Dirs    Dirs
	Version string // Mayoi's
	// Locale is the language plugins see, as "zh-Hans".
	Locale func() string
	// HTTP is the client of fetch.
	HTTP *http.Client
	// safe runs no plugin but the bundled ones: mayoi --disable-plugins.
	safe atomic.Bool

	host Host
	mu   sync.Mutex
	list []*Plugin
	// origins are where the plugins of the plugins folder came from, by
	// name, as Load last read them.
	origins map[string]Origin
	// storage are the stores of the plugins loaded so far, by file.
	storeMu sync.Mutex
	stores  map[string]map[string]json.RawMessage
}

// New makes a registry of the plugins of dirs. Load finds them.
func New(dirs Dirs, host Host) *Registry {
	return &Registry{Dirs: dirs, host: host, stores: map[string]map[string]json.RawMessage{},
		HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (r *Registry) locale() string {
	if r.Locale == nil {
		return ""
	}
	return r.Locale()
}

// SetSafe runs no plugin but the bundled ones, or all of them again.
func (r *Registry) SetSafe(on bool) { r.safe.Store(on) }

// Safe reports whether only the bundled plugins run.
func (r *Registry) Safe() bool { return r.safe.Load() }

// enabled reports whether the settings, or else its manifest, leave the
// plugin on, and safe mode lets it run.
func (r *Registry) enabled(p *Plugin) bool {
	if r.safe.Load() && !p.Bundled {
		return false
	}
	if r.host == nil {
		return p.DefaultOn()
	}
	var s struct {
		Enabled *bool `json:"enabled"`
	}
	json.Unmarshal(r.host.Settings(p.Name), &s)
	if s.Enabled == nil {
		return p.DefaultOn()
	}
	return *s.Enabled
}

// Plugins are the plugins found, bundled first, then by name.
func (r *Registry) Plugins() []*Plugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.list)
}

// Plugin is the plugin of a name, nil for none.
func (r *Registry) Plugin(name string) *Plugin {
	for _, p := range r.Plugins() {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Enabled are the plugins the settings leave on, which are not crashed.
func (r *Registry) Enabled() []*Plugin {
	var out []*Plugin
	for _, p := range r.Plugins() {
		if r.enabled(p) && p.loadErr == "" {
			out = append(out, p)
		}
	}
	return out
}

// Origin is where an installed plugin came from.
type Origin struct {
	// Source is where one installed from Git came from, which FromGit
	// says it was.
	Source
	FromGit bool
	// Linked is set for a folder that is a link, as mayoi plugin dev
	// makes.
	Linked bool
}

// Origin is where the plugin of a name came from, as Load last read it:
// the zero Origin for a bundled one, a script, or one copied in.
func (r *Registry) Origin(name string) Origin {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.origins[name]
}

// Load finds the plugins again: those whose files did not change keep
// running, the others stop and start anew as something asks for them.
func (r *Registry) Load() {
	var found []*Plugin
	origins := map[string]Origin{}
	if r.Dirs.Bundled != nil {
		entries, _ := fs.ReadDir(r.Dirs.Bundled, ".")
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub, _ := fs.Sub(r.Dirs.Bundled, e.Name())
			found = append(found, r.read(sub, "", true))
		}
	}
	if r.Dirs.Plugins != "" {
		entries, _ := os.ReadDir(r.Dirs.Plugins)
		for _, e := range entries {
			dir := filepath.Join(r.Dirs.Plugins, e.Name())
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			// A plugin linked by mayoi plugin dev is a symlink to a folder.
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				continue
			}
			p := r.read(os.DirFS(dir), dir, false)
			if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				origins[p.Name] = Origin{Linked: true}
			}
			found = append(found, p)
		}
		for name, src := range ReadRecords(r.Dirs.Plugins) {
			o := origins[name]
			o.Source, o.FromGit = src, true
			origins[name] = o
		}
	}
	if r.Dirs.Scripts != "" {
		entries, _ := os.ReadDir(r.Dirs.Scripts)
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			if e.IsDir() || (ext != ".ts" && ext != ".js" && ext != ".mjs") || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			found = append(found, r.readScript(e.Name()))
		}
	}
	// One plugin to a name: a bundled one, then the first found.
	seen := map[string]bool{}
	found = slices.DeleteFunc(found, func(p *Plugin) bool {
		if seen[p.Name] {
			return true
		}
		seen[p.Name] = true
		return false
	})
	r.mu.Lock()
	old := r.list
	for i, p := range found {
		for _, o := range old {
			if o.Name == p.Name && o.sig == p.sig && o.Dir == p.Dir {
				// Unchanged: keep it, running or not, with its log.
				found[i] = o
			}
		}
	}
	r.list = found
	r.origins = origins
	r.mu.Unlock()
	for _, o := range old {
		if !slices.Contains(found, o) {
			o.shutdown()
			if p := r.Plugin(o.Name); p != nil {
				p.mu.Lock()
				p.logs = o.Logs()
				p.mu.Unlock()
				p.logf("info", "reloaded")
			}
		}
	}
	if r.host != nil {
		r.host.Changed()
	}
}

// read reads the plugin of a folder: one whose manifest does not read is
// kept, with the error, for the log and the Plugins page.
func (r *Registry) read(fsys fs.FS, dir string, bundled bool) *Plugin {
	p := &Plugin{Dir: dir, Bundled: bundled, fsys: fsys, reg: r, sig: signature(fsys)}
	data, err := fs.ReadFile(fsys, ManifestFile)
	if err == nil {
		var m *Manifest
		m, err = ParseManifest(data)
		if m != nil {
			p.Manifest = *m
		}
	}
	if p.Name == "" {
		p.Name = filepath.Base(dir)
	}
	if err != nil {
		p.loadErr = err.Error()
	} else if !Satisfies(r.Version, p.Mayoi) {
		p.loadErr = fmt.Sprintf("it needs Mayoi %s, and this is %s", p.Mayoi, r.Version)
	}
	return p
}

// readScript reads a user script: its commands are the functions it
// exports, script.<name>.<function>.
func (r *Registry) readScript(file string) *Plugin {
	fsys := os.DirFS(r.Dirs.Scripts)
	name := strings.TrimSuffix(file, filepath.Ext(file))
	p := &Plugin{Script: true, Dir: r.Dirs.Scripts, fsys: fsys, reg: r, sig: fileSig(fsys, file)}
	p.Name = "script." + name
	p.Main = file
	p.Activation = []string{"onCommand"}
	p.exports, p.loadErr = scriptExports(fsys, file)
	for _, e := range p.exports {
		p.Contributes.Commands = append(p.Contributes.Commands, CommandContrib{ID: p.Name + "." + e, Title: humanize(e), Arg: "<text>"})
	}
	return p
}

// scriptExports are the names a script exports, read without running it.
func scriptExports(fsys fs.FS, file string) ([]string, string) {
	l := &loader{fsys: fsys, modules: map[string]*moejs.Module{}}
	m, err := l.compile(file)
	if err != nil {
		return nil, err.Error()
	}
	var names []string
	for _, e := range m.Exports() {
		if e != "default" && e != "activate" {
			names = append(names, e)
		}
	}
	sort.Strings(names)
	return names, ""
}

// humanize turns a function's name into a title: sortLines is Sort Lines.
func humanize(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || r == '-':
			b.WriteByte(' ')
			continue
		case i == 0:
			r = unicode.ToUpper(r)
		case unicode.IsUpper(r):
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// signature tells the files of a folder apart from what they were: their
// names, sizes and times.
func signature(fsys fs.FS) string {
	var b strings.Builder
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return fs.SkipDir
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			fmt.Fprintf(&b, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

func fileSig(fsys fs.FS, file string) string {
	info, err := fs.Stat(fsys, file)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// Reload stops a plugin, clearing its crashes, so that it starts anew.
func (r *Registry) Reload(name string) {
	if p := r.Plugin(name); p != nil {
		p.shutdown()
		p.mu.Lock()
		p.crashes, p.lastErr = 0, ""
		p.mu.Unlock()
	}
	r.Load()
}

// Restart stops a plugin, clearing its crashes, and starts it again in
// the background, unless it is off or has no code of its own to run.
func (r *Registry) Restart(name string) {
	p := r.Plugin(name)
	if p == nil {
		return
	}
	p.shutdown()
	p.mu.Lock()
	p.crashes, p.lastErr = 0, ""
	p.mu.Unlock()
	if !r.enabled(p) || !p.HasCode() || p.Script || p.loadErr != "" {
		return
	}
	p.logf("info", "restarting")
	p.launch()
	if r.host != nil {
		r.host.Changed()
	}
}

// ReloadAll stops every plugin and finds them again.
func (r *Registry) ReloadAll() {
	for _, p := range r.Plugins() {
		p.shutdown()
		p.mu.Lock()
		p.crashes, p.lastErr = 0, ""
		p.mu.Unlock()
	}
	r.Load()
}

// Close stops every plugin.
func (r *Registry) Close() {
	for _, p := range r.Plugins() {
		p.shutdown()
	}
}

// shutdown stops the plugin's runtime, if it runs.
func (p *Plugin) shutdown() {
	p.mu.Lock()
	in := p.in
	p.in = nil
	p.subs, p.hovers, p.dynamic = nil, nil, nil
	p.mu.Unlock()
	if in != nil {
		in.stop()
		if p.reg.host != nil {
			p.reg.host.Changed()
		}
	}
}

// Command is a command of a plugin.
type Command struct {
	ID, Title, Arg string
	Plugin         *Plugin
}

// Commands are the commands of the plugins that are on, titled in the
// language of tag.
func (r *Registry) Commands(tag string) []Command {
	var out []Command
	for _, p := range r.Enabled() {
		p.mu.Lock()
		dynamic := slices.Clone(p.dynamic)
		p.mu.Unlock()
		for _, c := range append(slices.Clone(p.Contributes.Commands), dynamic...) {
			out = append(out, Command{ID: c.ID, Title: c.TitleIn(tag), Arg: c.Arg, Plugin: p})
		}
	}
	return out
}

// CommandPlugin is the plugin of a command id, nil for none.
func (r *Registry) CommandPlugin(id string) *Plugin {
	for _, c := range r.Commands("") {
		if c.ID == id {
			return c.Plugin
		}
	}
	return nil
}

// ErrNoCommand is Run's error for an id no plugin has.
var ErrNoCommand = errors.New("no such command")

// Run runs a command of a plugin, starting it first, and waits for what
// it returns.
func (r *Registry) Run(id string, arg any) (json.RawMessage, error) {
	p := r.CommandPlugin(id)
	if p == nil {
		return nil, ErrNoCommand
	}
	// A command may ask the user and wait as long as they take.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if p.Script {
		return p.Request(ctx, "call", map[string]any{"name": strings.TrimPrefix(id, p.Name+"."), "arg": arg})
	}
	return p.Request(ctx, "command", map[string]any{"id": id, "arg": arg})
}

// Request calls the plugin, starting it first.
func (p *Plugin) Request(ctx context.Context, kind string, payload any) (json.RawMessage, error) {
	in, err := p.ensure()
	if err != nil {
		return nil, err
	}
	return in.request(ctx, kind, payload)
}

// Started reports whether the plugin runs, without starting it.
func (p *Plugin) Started() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in != nil
}

// ensure starts the plugin unless it runs, and returns its instance.
func (p *Plugin) ensure() (*instance, error) {
	p.mu.Lock()
	switch {
	case p.loadErr != "":
		p.mu.Unlock()
		return nil, errors.New(p.loadErr)
	case !p.reg.enabled(p):
		p.mu.Unlock()
		return nil, fmt.Errorf("%s is disabled", p.Name)
	case p.crashes >= maxCrashes:
		p.mu.Unlock()
		return nil, fmt.Errorf("%s stopped %d times and is off until reloaded: %s", p.Name, maxCrashes, p.lastErr)
	case !p.HasCode():
		p.mu.Unlock()
		return nil, fmt.Errorf("%s has no code to run", p.Name)
	case p.in != nil:
		in := p.in
		p.mu.Unlock()
		return in, nil
	case p.starting != nil:
		ch := p.starting
		p.mu.Unlock()
		<-ch
		p.mu.Lock()
		in, err := p.in, p.lastErr
		p.mu.Unlock()
		if in == nil {
			return nil, errors.New(err)
		}
		return in, nil
	}
	ch := make(chan struct{})
	p.starting = ch
	p.mu.Unlock()
	began := time.Now()
	in, err := start(p, p.reg.host, p.reg.HTTP)
	p.mu.Lock()
	p.starting = nil
	if err != nil {
		p.crashes++
		p.lastErr = err.Error()
	} else {
		p.in, p.activatedIn, p.startedAt = in, time.Since(began), time.Now()
	}
	p.mu.Unlock()
	close(ch)
	if err != nil {
		p.logf("error", "could not start: %v", err)
		if p.reg.host != nil {
			p.reg.host.Changed()
		}
		return nil, err
	}
	p.logf("info", "started in %v", time.Since(began).Round(time.Millisecond))
	go p.follow(in)
	if p.reg.host != nil {
		p.reg.host.Changed()
	}
	return in, nil
}

// follow notes how the plugin's runtime ends: stopped by Mayoi, or by
// what it did, which counts toward disabling it.
func (p *Plugin) follow(in *instance) {
	<-in.done
	p.mu.Lock()
	ours := p.in == in
	if ours {
		p.in = nil
		p.subs, p.hovers, p.dynamic = nil, nil, nil
		p.crashes++
		p.lastErr = in.describe(in.err)
	}
	crashes := p.crashes
	p.mu.Unlock()
	if !ours {
		return
	}
	in.stop()
	if crashes >= maxCrashes {
		p.logf("error", "stopped: %s; off until reloaded, after %d stops", in.describe(in.err), crashes)
	} else {
		p.logf("error", "stopped: %s", in.describe(in.err))
	}
	if p.reg.host != nil {
		p.reg.host.Changed()
	}
}

// RunOnce runs source, of the extension ext, as a plugin of its own, once:
// its top level, then its default export when it exports a function, whose
// result it returns. What it logs is in the log of "selection".
func (r *Registry) RunOnce(source, ext string) (json.RawMessage, error) {
	file := "selection" + ext
	fsys := fstest.MapFS{file: &fstest.MapFile{Data: []byte(source)}}
	p := &Plugin{fsys: fsys, reg: r, Script: true}
	p.Name, p.Main = "selection", file
	defer p.shutdown()
	in, err := start(p, r.host, r.HTTP)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.in = in
	p.mu.Unlock()
	if !slices.Contains(in.exportNames(), "default") {
		return json.RawMessage("null"), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return in.request(ctx, "call", map[string]any{"name": "default"})
}

// Activate starts the plugins whose activation names event, as
// "onLanguage:go", and those that start on any; with a "*" event, as
// "onStartup", only those that name it.
func (r *Registry) Activate(event string) {
	for _, p := range r.Enabled() {
		if !p.HasCode() || p.Script || p.Started() {
			continue
		}
		for _, a := range p.Activation {
			if a == event || (a == "*" && event != "") {
				p.launch()
				break
			}
		}
	}
}

// Toggled follows the settings of the plugin of name as they turn it on or
// off: it stops it when they turn it off, and starts it, as Mayoi starting
// does, when they turn it on.
func (r *Registry) Toggled(name string) {
	p := r.Plugin(name)
	if p == nil {
		return
	}
	if !r.enabled(p) {
		p.shutdown()
		return
	}
	if !p.HasCode() || p.Script || p.loadErr != "" || p.Started() {
		return
	}
	for _, a := range p.Activation {
		if a == "onStartup" || a == "*" {
			p.launch()
			return
		}
	}
}

// launch starts the plugin in the background, holding the events sent
// meanwhile, which it gets once it listens to them.
func (p *Plugin) launch() {
	p.mu.Lock()
	if p.in != nil || p.queueing {
		p.mu.Unlock()
		return
	}
	p.queueing = true
	p.mu.Unlock()
	go func() {
		_, err := p.ensure()
		p.mu.Lock()
		queued := p.queued
		p.queued, p.queueing = nil, false
		p.mu.Unlock()
		if err != nil {
			return
		}
		for _, ev := range queued {
			if p.Subscribed(ev.name) {
				ctx, cancel := context.WithTimeout(context.Background(), BusyLimit)
				p.Request(ctx, "event", map[string]any{"name": ev.name, "data": ev.data})
				cancel()
			}
		}
	}()
}

// ActivateWorkspace starts the plugins whose workspaceContains globs
// match a file of the folder, which exists tells.
func (r *Registry) ActivateWorkspace(exists func(glob string) bool) {
	for _, p := range r.Enabled() {
		if !p.HasCode() || p.Started() {
			continue
		}
		for _, a := range p.Activation {
			if glob, ok := strings.CutPrefix(a, "workspaceContains:"); ok && exists(glob) {
				p.launch()
				break
			}
		}
	}
}

// Emit sends an event to the plugins that run and listen to it, without
// waiting for them.
func (r *Registry) Emit(event string, data any) {
	for _, p := range r.Plugins() {
		p.mu.Lock()
		if p.queueing {
			p.queued = append(p.queued, queuedEvent{event, data})
			p.mu.Unlock()
			continue
		}
		p.mu.Unlock()
		if p.Subscribed(event) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), BusyLimit)
				defer cancel()
				p.Request(ctx, "event", map[string]any{"name": event, "data": data})
			}()
		}
	}
}

// call routes a call of a plugin to the app: the registry answers some
// itself, the host the others.
func (r *Registry) call(p *Plugin, method string, args json.RawMessage) ([]byte, error) {
	if r.host == nil {
		return nil, errors.New("no host")
	}
	res, err := r.host.Call(p, method, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

// callSync answers the calls a plugin waits for: those of the registry,
// which never wait for the app's main thread.
func (r *Registry) callSync(p *Plugin, method string, args json.RawMessage) ([]byte, error) {
	var a struct {
		ID, Title, Name, Key, Language string
		Value                          json.RawMessage
		Folder                         bool
	}
	json.Unmarshal(args, &a)
	var res any
	switch method {
	case "app.version":
		res = r.Version
	case "app.platform":
		res = runtime.GOOS
	case "commands.register":
		p.mu.Lock()
		known := slices.ContainsFunc(p.Contributes.Commands, func(c CommandContrib) bool { return c.ID == a.ID })
		if !known && !slices.ContainsFunc(p.dynamic, func(c CommandContrib) bool { return c.ID == a.ID }) {
			title := a.Title
			if title == "" {
				title = a.ID
			}
			p.dynamic = append(p.dynamic, CommandContrib{ID: a.ID, Title: title})
		}
		p.mu.Unlock()
		if !known && r.host != nil {
			r.host.Changed()
		}
	case "commands.unregister":
		p.mu.Lock()
		p.dynamic = slices.DeleteFunc(p.dynamic, func(c CommandContrib) bool { return c.ID == a.ID })
		p.mu.Unlock()
		if r.host != nil {
			r.host.Changed()
		}
	case "events.subscribe":
		p.mu.Lock()
		if p.subs == nil {
			p.subs = map[string]bool{}
		}
		p.subs[a.Name] = true
		p.mu.Unlock()
	case "languages.hover":
		p.mu.Lock()
		if !slices.Contains(p.hovers, a.Language) {
			p.hovers = append(p.hovers, a.Language)
		}
		p.mu.Unlock()
	case "settings.get":
		res = r.Settings(p)[a.Key]
	case "settings.all":
		res = r.Settings(p)
	case "settings.mayoi":
		if r.host != nil {
			res = r.host.MayoiSetting(a.Key)
		}
	case "storage.get":
		res = r.store(p, a.Folder)[a.Key]
	case "storage.set":
		return []byte("null"), r.storeSet(p, a.Folder, a.Key, a.Value)
	case "storage.keys":
		keys := []string{}
		for k := range r.store(p, a.Folder) {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		res = keys
	default:
		return nil, fmt.Errorf("unknown call %s", method)
	}
	return json.Marshal(res)
}

// Settings are a plugin's settings: what settings.json sets, over the
// defaults its manifest declares.
func (r *Registry) Settings(p *Plugin) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, s := range p.Contributes.Settings {
		if len(s.Default) > 0 {
			out[k] = s.Default
		}
	}
	if r.host != nil {
		var set map[string]json.RawMessage
		json.Unmarshal(r.host.Settings(p.Name), &set)
		for k, v := range set {
			if k != "enabled" {
				out[k] = v
			}
		}
	}
	return out
}

// storeFile is the file of a plugin's store, global or of the folder in
// front.
func (r *Registry) storeFile(p *Plugin, folder bool) string {
	if r.Dirs.Storage == "" {
		return ""
	}
	dir := filepath.Join(r.Dirs.Storage, p.Name)
	if !folder {
		return filepath.Join(dir, "global.json")
	}
	f := ""
	if r.host != nil {
		f = r.host.Folder()
	}
	return filepath.Join(dir, "folders", folderKey(f)+".json")
}

// folderKey names a folder's store: its path, made a file name.
func folderKey(folder string) string {
	var b strings.Builder
	for _, r := range strings.TrimPrefix(path.Clean(filepath.ToSlash(folder)), "/") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func (r *Registry) store(p *Plugin, folder bool) map[string]json.RawMessage {
	file := r.storeFile(p, folder)
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	if s, ok := r.stores[file]; ok {
		return s
	}
	s := map[string]json.RawMessage{}
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &s)
	}
	r.stores[file] = s
	return s
}

func (r *Registry) storeSet(p *Plugin, folder bool, key string, value json.RawMessage) error {
	s := r.store(p, folder)
	file := r.storeFile(p, folder)
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	if len(value) == 0 || string(value) == "null" {
		delete(s, key)
	} else {
		s[key] = value
	}
	if file == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}
