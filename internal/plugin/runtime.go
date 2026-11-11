package plugin

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/engine"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
)

// This is the only file that knows moejs: a plugin's runtime, its event
// loop, and the modules it imports.

//go:embed prelude.js
var preludeSource string

var (
	preludeOnce   sync.Once
	preludeScript *moejs.Script
	preludeErr    error
	shimModule    *moejs.Module
)

// mayoiShim is the module "mayoi", the prelude's object by its names.
const mayoiShim = `const m = globalThis.__mayoi;
export const version = m.version, platform = m.platform, log = m.log, commands = m.commands,
  editor = m.editor, workspace = m.workspace, window = m.window, settings = m.settings,
  storage = m.storage, events = m.events, process = m.process, languages = m.languages,
  statusItem = m.statusItem, views = m.views, previews = m.previews, webview = m.webview, agents = m.agents,
  ai = m.ai;
export default m;
`

func compilePrelude() error {
	preludeOnce.Do(func() {
		preludeScript, preludeErr = moejs.CompileScript("mayoi:prelude", preludeSource)
		if preludeErr == nil {
			shimModule, preludeErr = moejs.Compile("mayoi", mayoiShim)
		}
	})
	return preludeErr
}

// Budgets of a plugin's runtime.
var (
	// MaxFile is the largest file a plugin may load: compiling cannot be
	// interrupted.
	MaxFile = 1 << 20
	// MemoryLimit is what a plugin's JavaScript may keep.
	MemoryLimit int64 = 256 << 20
	// BusyLimit is how long a plugin's JavaScript may run without a break
	// before it is stopped: a loop that never ends.
	BusyLimit = 10 * time.Second
	// ActivateLimit is how long activating may take.
	ActivateLimit = 2 * time.Second
)

// instance is a plugin's runtime while it is active.
type instance struct {
	p        *Plugin
	rt       *moejs.Runtime
	loop     *eventloop.Loop
	api      *webapi.API
	dispatch moejs.Value
	release  func(func(*moejs.Runtime)) // the hold that keeps the loop alive

	mu      sync.Mutex
	seq     int
	waiting map[int]chan reply
	busy    time.Time // when the JavaScript started running, zero while idle
	cpu     time.Duration
	done    chan struct{} // closed as the loop ends
	err     error         // what ended it
}

type reply struct {
	ok   bool
	data string
}

// loader compiles the modules of a plugin's folder.
type loader struct {
	fsys    fs.FS
	modules map[string]*moejs.Module
}

func (l *loader) compile(name string) (*moejs.Module, error) {
	if m, ok := l.modules[name]; ok {
		return m, nil
	}
	data, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, MaxFile)
	}
	var m *moejs.Module
	switch path.Ext(name) {
	case ".ts", ".mts", ".tsx":
		m, err = moejs.CompileTS(name, string(data))
	default:
		m, err = moejs.Compile(name, string(data))
	}
	if err != nil {
		return nil, err
	}
	l.modules[name] = m
	return m, nil
}

// resolve finds the module a specifier names: "mayoi", or a relative path
// inside the plugin's folder, with or without its extension.
func (l *loader) resolve(ref moejs.Referrer, spec string) (*moejs.Module, error) {
	if spec == "mayoi" {
		return shimModule, nil
	}
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return nil, fmt.Errorf("cannot import %q: plugins import \"mayoi\" and their own files; bundle packages with mayoi plugin pack", spec)
	}
	from := "."
	if ref != nil && ref.Name() != "mayoi" {
		from = path.Dir(ref.Name())
	}
	target := path.Join(from, spec)
	if target == ".." || strings.HasPrefix(target, "../") {
		return nil, fmt.Errorf("cannot import %q: it is outside the plugin's folder", spec)
	}
	for _, cand := range []string{target, target + ".ts", target + ".js", target + ".mjs", target + "/index.ts", target + "/index.js"} {
		if st, err := fs.Stat(l.fsys, cand); err == nil && !st.IsDir() {
			return l.compile(cand)
		}
	}
	return nil, fmt.Errorf("cannot import %q: no such file", spec)
}

// start compiles the plugin and runs its top level and activate on a
// loop of their own.
func start(p *Plugin, host Host, httpClient *http.Client) (*instance, error) {
	if err := compilePrelude(); err != nil {
		return nil, err
	}
	l := &loader{fsys: p.fsys, modules: map[string]*moejs.Module{}}
	entry, err := l.compile(path.Clean(p.Main))
	if err != nil {
		return nil, err
	}
	linked, err := moejs.Link(entry, l.resolve)
	if err != nil {
		return nil, err
	}
	in := &instance{p: p, waiting: map[int]chan reply{}, done: make(chan struct{})}
	in.rt = moejs.NewRuntime(moejs.Options{
		DisableDynamicCode: true,
		MemoryLimit:        MemoryLimit,
		Importer:           &moejs.Importer{Resolve: l.resolve},
		Locale:             p.reg.locale(),
	})
	in.loop, err = eventloop.New(in.rt, eventloop.Options{
		OnError: func(err error) { p.logf("error", "%s", in.describe(err)) },
		OnIdle: func() {
			in.mu.Lock()
			if !in.busy.IsZero() {
				in.cpu += time.Since(in.busy)
			}
			in.busy = time.Time{}
			in.mu.Unlock()
		},
		OnWake: func() error {
			in.mu.Lock()
			in.busy = time.Now()
			in.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	in.api, err = webapi.Install(in.loop, webapi.Options{Client: httpClient})
	if err != nil {
		return nil, err
	}
	if err := in.rt.SetGlobal("__mayoiHost", in.natives(host)); err != nil {
		return nil, err
	}
	if _, err := in.rt.RunScript(preludeScript); err != nil {
		return nil, fmt.Errorf("prelude: %s", in.describe(err))
	}
	ready := make(chan error, 1)
	in.busy = time.Now()
	go in.watch()
	go func() {
		err := in.loop.Run(func(rt *moejs.Runtime) error {
			in.release = in.loop.Hold()
			if err := rt.Load(linked); err != nil && !errors.Is(err, moejs.ErrModulePending) {
				ready <- fmt.Errorf("%s", in.describe(err))
				in.release(nil)
				return nil
			}
			exports := map[string]any{}
			for _, name := range linked.Exports() {
				if v, ok := rt.Export(name); ok {
					exports[name] = v
				}
			}
			if err := rt.SetGlobal("__mayoiExports", exports); err != nil {
				ready <- err
				in.release(nil)
				return nil
			}
			ready <- nil
			return nil
		})
		in.mu.Lock()
		in.err = err
		for id, ch := range in.waiting {
			ch <- reply{false, "the plugin stopped"}
			delete(in.waiting, id)
		}
		in.mu.Unlock()
		close(in.done)
	}()
	select {
	case err := <-ready:
		if err != nil {
			in.stop()
			return nil, err
		}
	case <-in.done:
		return nil, fmt.Errorf("stopped as it loaded: %s", in.describe(in.err))
	case <-time.After(ActivateLimit):
		in.stop()
		return nil, fmt.Errorf("loading took longer than %v", ActivateLimit)
	}
	if hasExport(linked, "activate") {
		ctx, cancel := context.WithTimeout(context.Background(), ActivateLimit)
		defer cancel()
		if _, err := in.request(ctx, "call", map[string]any{"name": "activate"}); err != nil {
			in.stop()
			return nil, fmt.Errorf("activate: %v", err)
		}
	}
	return in, nil
}

func hasExport(m *moejs.Module, name string) bool {
	for _, e := range m.Exports() {
		if e == name {
			return true
		}
	}
	return false
}

// exportNames are the functions a user script exports.
func (in *instance) exportNames() []string {
	var names []string
	done := make(chan struct{})
	if !in.loop.RunOnLoop(func(rt *moejs.Runtime) {
		defer close(done)
		m := rt.Module()
		if m == nil {
			return
		}
		for _, name := range m.Exports() {
			if v, ok := rt.Export(name); ok {
				if o := v.AsObject(); v.IsObject() && o != nil && o.IsCallable() {
					names = append(names, name)
				}
			}
		}
	}) {
		return nil
	}
	select {
	case <-done:
	case <-in.done:
	case <-time.After(time.Second):
	}
	return names
}

// watch stops the plugin when its JavaScript runs too long without a
// break, or as the loop ends.
func (in *instance) watch() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-in.done:
			return
		case <-t.C:
			in.mu.Lock()
			busy := in.busy
			in.mu.Unlock()
			if !busy.IsZero() && time.Since(busy) > BusyLimit {
				in.p.logf("error", "stopped: its JavaScript ran for %v without a break", BusyLimit)
				in.loop.Interrupt(errBusy)
				return
			}
		}
	}
}

var errBusy = errors.New("ran too long")

// stop ends the loop for good.
func (in *instance) stop() {
	select {
	case <-in.done:
	default:
		in.loop.Terminate()
	}
	<-in.done
	in.api.Close()
}

// stats are what the plugin used: its JavaScript's time running, and the
// memory it keeps as last measured.
func (in *instance) stats() (cpu time.Duration, memory int64) {
	in.mu.Lock()
	cpu = in.cpu
	if !in.busy.IsZero() {
		cpu += time.Since(in.busy)
	}
	in.mu.Unlock()
	return cpu, in.rt.Stats().LiveBytes
}

// request calls the plugin's dispatch with a kind and a payload, and
// waits for its reply, the context, or the end of the plugin.
func (in *instance) request(ctx context.Context, kind string, payload any) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ch := make(chan reply, 1)
	in.mu.Lock()
	in.seq++
	id := in.seq
	in.waiting[id] = ch
	in.mu.Unlock()
	forget := func() {
		in.mu.Lock()
		delete(in.waiting, id)
		in.mu.Unlock()
	}
	posted := eventloop.Post(in.loop, func(rt *moejs.Runtime) error {
		_, err := rt.CallFunction(in.dispatch, moejs.Undefined(), engine.IntValue(id), moejs.String(kind), moejs.String(string(data)))
		return err
	})
	if !posted {
		forget()
		return nil, errors.New("the plugin stopped")
	}
	select {
	case r := <-ch:
		if !r.ok {
			return nil, errors.New(r.data)
		}
		return json.RawMessage(r.data), nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	case <-in.done:
		forget()
		return nil, fmt.Errorf("the plugin stopped: %s", in.describe(in.err))
	}
}

// natives are the functions of __mayoiHost.
func (in *instance) natives(host Host) map[string]any {
	str := func(v moejs.Value) string {
		out, err := in.rt.ToGo(v)
		if err != nil {
			return ""
		}
		s, _ := out.(string)
		return s
	}
	p := in.p
	return map[string]any{
		"call": moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			method, data := str(moejs.Arg(args, 0)), str(moejs.Arg(args, 1))
			promise, settle := in.loop.NewPromise()
			go func() {
				res, err := p.reg.call(p, method, json.RawMessage(data))
				settle(func(rt *moejs.Runtime) (moejs.Value, error) {
					if err != nil {
						return moejs.Undefined(), err
					}
					return rt.ParseJSON(res)
				})
			}()
			return promise, nil
		}),
		"callSync": moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			res, err := p.reg.callSync(p, str(moejs.Arg(args, 0)), json.RawMessage(str(moejs.Arg(args, 1))))
			if err != nil {
				return moejs.Undefined(), err
			}
			return in.rt.ParseJSON(res)
		}),
		"reply": moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			id := int(moejs.Arg(args, 0).AsNumber())
			ok := moejs.Arg(args, 1).IsTrue()
			data := str(moejs.Arg(args, 2))
			in.mu.Lock()
			ch := in.waiting[id]
			delete(in.waiting, id)
			in.mu.Unlock()
			if ch != nil {
				ch <- reply{ok, data}
			}
			return moejs.Undefined(), nil
		}),
		"bind": moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			in.dispatch = moejs.Arg(args, 0)
			return moejs.Undefined(), nil
		}),
		"log": moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			p.logf(str(moejs.Arg(args, 0)), "%s", str(moejs.Arg(args, 1)))
			return moejs.Undefined(), nil
		}),
	}
}

// describe tells an error of the runtime: an exception with its stack,
// an interrupt by its cause.
func (in *instance) describe(err error) string {
	if err == nil {
		return "it ended"
	}
	var ex *moejs.Exception
	if errors.As(err, &ex) {
		if s := in.rt.StackTrace(ex); s != "" {
			return s
		}
	}
	var ie *moejs.InterruptedError
	if errors.As(err, &ie) {
		switch {
		case errors.Is(err, moejs.ErrMemoryLimit):
			return fmt.Sprintf("it kept more than %d MiB", MemoryLimit>>20)
		case errors.Is(err, eventloop.ErrTerminated):
			return "it was stopped"
		case errors.Is(err, errBusy):
			return fmt.Sprintf("its JavaScript ran for %v without a break", BusyLimit)
		}
	}
	return err.Error()
}
