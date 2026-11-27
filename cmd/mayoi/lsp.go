package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"mayoi/internal/bg"
	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// The language servers plugins declare: started for a folder as a
// document of one of their languages opens there, on the folder's machine,
// and told of the documents of their languages as they open, change, save
// and close.

// lspServer is a language server of a folder.
type lspServer struct {
	plugin  *plugin.Plugin
	contrib plugin.ServerContrib
	root    string
	key     string // of lspServers
	client  *lsp.Client
	ready   chan struct{} // closed once started, or failed to
	err     error
	failure lspFailure
	// nativeTSC is whether, for lspFailNoTypeScript, the folder's tsc is
	// TypeScript 7 or later.
	nativeTSC bool
	command   string       // the program it ran, once resolved
	launch    serverLaunch // how it ran, once resolved
	failedAt  time.Time    // when it could not start
	starts    int          // since its plugin was last loaded or turned on
	// asked is whether a person asked for it to start, as a restart does.
	asked bool
	// ctx is cancelled to stop it starting; stopping is set as it is
	// stopped on purpose, so its end is not told of.
	ctx      context.Context
	cancel   context.CancelFunc
	stopping atomic.Bool
	// detached are the files a change of language took from it, whose
	// diagnostics it may still send late, until it has them open again.
	detachedMu sync.Mutex
	detached   map[string]bool
}

// lspFailure is why a language server could not start.
type lspFailure int

const (
	lspFailOther   lspFailure = iota // as err says
	lspFailMissing                   // its program is not on the folder's machine
	lspFailQuit                      // its program ended before it answered
	lspFailSilent                    // it did not answer in time
	// its program ended with 127 on the folder's host, and whether it is
	// there could not be told
	lspFailUnchecked
	// typescript-language-server found no TypeScript with a tsserver in the
	// folder: nativeTSC is whether the tsc of the folder's machine is
	// TypeScript 7's, which brings its own server
	lspFailNoTypeScript
)

// lspStartTimeout is how long a server has to start and answer, and
// lspCheckTimeout how long its host has to say whether its program is
// there, when that is not clear from how it ended.
var (
	lspStartTimeout = 30 * time.Second
	lspCheckTimeout = 10 * time.Second
)

// lspState is where a language server is in its life.
type lspState int

const (
	lspStarting lspState = iota // its command starts, and it is greeted
	lspRunning                  // client talks to it
	lspFailed                   // it could not start, as err says
	lspStopped                  // it ran, and its command ended
)

// state works out the server's state from ready and its client, which
// start writes before it closes ready.
func (s *lspServer) state() lspState {
	select {
	case <-s.ready:
	default:
		return lspStarting
	}
	if s.client == nil {
		return lspFailed
	}
	select {
	case <-s.client.Done():
		return lspStopped
	default:
		return lspRunning
	}
}

var lspServers = struct {
	sync.Mutex
	m map[string]*lspServer // by folder and server id
}{m: map[string]*lspServer{}}

// serverKey is the key of the server of id in the folder root.
func serverKey(root, id string) string { return root + "\x00" + id }

// current reports whether s is still the server of its folder: one
// restarted, forgotten or stopped as the app quits is not, and what it
// would do then is left to the one after it.
func (s *lspServer) current() bool {
	lspServers.Lock()
	defer lspServers.Unlock()
	return lspServers.m[s.key] == s
}

// maxServerStarts is how many times a server is started, after it stops
// or fails to start, until its plugin is reloaded or turned on again.
const maxServerStarts = 3

// lspRetryPause is how long after a server fails to start a document of
// its language opening starts it again.
var lspRetryPause = 30 * time.Second

// serverContrib is the server a plugin declares for a language, nil for
// none.
func serverContrib(lang string) (*plugin.Plugin, *plugin.ServerContrib) {
	if plugins == nil || lang == "" {
		return nil, nil
	}
	for _, p := range plugins.enabled() {
		for i, s := range p.Contributes.LanguageServers {
			if slices.Contains(s.Languages, lang) {
				return p, &p.Contributes.LanguageServers[i]
			}
		}
	}
	return nil, nil
}

// serverURI is the URI a server of root knows a path by: on an SSH host,
// the path there.
func serverURI(path string) string {
	if _, rp, ok := remote.Split(path); ok {
		return lsp.URI(rp)
	}
	return lsp.URI(path)
}

// serverPath is the path of a URI of a server of root.
func serverPath(root, uri string) string {
	p := lsp.Path(uri)
	if p == "" {
		return ""
	}
	if remote.Is(root) {
		return remote.On(root, filepath.ToSlash(p))
	}
	return p
}

// lspFor is the server of a document's language in the folder, started
// in the background the first time; nil when no plugin declares one.
// opening is whether the document is opening, which starts again one that
// failed to start a while ago.
func (a *app) lspFor(d *document, opening bool) *lspServer {
	if a.root == "" || d.path == "" {
		return nil
	}
	p, sc := serverContrib(languageOf(d))
	if p == nil {
		return nil
	}
	key := serverKey(a.root, sc.ID)
	lspServers.Lock()
	defer lspServers.Unlock()
	s := lspServers.m[key]
	// One that stopped is started again, a few times, and one that could
	// not start as well, as a document opens a while after.
	if s != nil {
		switch st := s.state(); {
		case s.starts >= maxServerStarts, st == lspStarting, st == lspRunning:
			return s
		case st == lspFailed && (!opening || time.Since(s.failedAt) < lspRetryPause):
			return s
		}
	}
	starts := 0
	if s != nil {
		starts = s.starts
	}
	s = newServer(p, *sc, a.root, starts+1)
	lspServers.m[key] = s
	go s.start(a, nil)
	return s
}

// lspRunningFor returns the language server of document d that runs, nil
// for none: unlike lspFor, it starts none, nor again one that stopped.
func (a *app) lspRunningFor(d *document) *lspServer {
	if a.root == "" || d.path == "" {
		return nil
	}
	_, sc := serverContrib(languageOf(d))
	if sc == nil {
		return nil
	}
	lspServers.Lock()
	s := lspServers.m[serverKey(a.root, sc.ID)]
	lspServers.Unlock()
	if s == nil || s.state() != lspRunning {
		return nil
	}
	return s
}

// newServer is a server of root yet to start, for lspServers.
func newServer(p *plugin.Plugin, sc plugin.ServerContrib, root string, starts int) *lspServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &lspServer{plugin: p, contrib: sc, root: root, key: serverKey(root, sc.ID), ready: make(chan struct{}), starts: starts,
		ctx: ctx, cancel: cancel}
}

// start starts the server on the folder's machine, once prev, the one
// before it there, is gone.
func (s *lspServer) start(a *app, prev *lspServer) {
	defer func() {
		// One that could not start is told of once, in the window that
		// started it, unless another took its place.
		if s.client == nil {
			s.failure = s.classify()
			switch s.failure {
			case lspFailUnchecked:
				s.failure = s.check()
			case lspFailNoTypeScript:
				s.nativeTSC = s.checkNativeTSC()
			}
			s.failedAt = time.Now()
			if s.ctx.Err() == nil && s.current() {
				msg := s.failMessage()
				a.post(func() {
					a.fails++
					a.tell(note{message: msg, label: locales.RunningRestart(), action: func() { a.restartServer(s.key) }})
				})
			}
		}
		close(s.ready)
	}()
	if prev != nil {
		prev.stop()
		if plugins != nil {
			plugins.setDiagnosticList(s.owner(), "", "", true, nil)
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, lspStartTimeout)
	defer cancel()
	if ctx.Err() != nil {
		s.err = ctx.Err()
		return
	}
	launch, err := s.resolve(ctx)
	if err != nil {
		s.err = err
		s.plugin.Log("error", "could not resolve "+s.contrib.ID+": "+err.Error())
		return
	}
	pr, pw := io.Pipe()
	cmd, err := vfs.CommandEnvironment(s.root, launch.environment(), launch.UnsetEnv, pr, launch.Command, launch.Args...)
	if err != nil {
		pr.Close()
		pw.Close()
		s.err = err
		return
	}
	s.command, s.launch = launch.Command, launch
	owner := s.owner()
	root := s.root
	rootURI := serverURI(root)
	if len(launch.WorkspaceFolders) > 0 {
		rootURI = launch.WorkspaceFolders[0].URI
	}
	c, err := lsp.Start(ctx, lsp.Options{Cmd: cmd, Stdin: pw, RootURI: rootURI, InitOptions: launch.Options,
		Configuration: launch.Configuration, WorkspaceFolders: launch.WorkspaceFolders, PathMappings: launch.PathMappings,
		WatchFiles: !vfs.IsRemote(root),
		Handlers: lsp.Handlers{
			Diagnostics: func(uri string, items []lsp.Diagnostic) {
				path := serverPath(root, uri)
				if path == "" || plugins == nil || !s.current() || s.isDetached(path, uri) {
					return
				}
				list := make([]diagnostic, len(items))
				for i, it := range items {
					sev := []string{"error", "error", "warning", "info", "hint"}[min(max(it.Severity, 0), 4)]
					code := strings.Trim(string(it.Code), `"`)
					list[i] = diagnostic{Range: textRange{fromLSP(it.Range.Start), fromLSP(it.Range.End)}, Severity: sev, Message: it.Message, Code: code}
				}
				source := s.contrib.ID
				if len(items) > 0 && items[0].Source != "" {
					source = items[0].Source
				}
				plugins.setDiagnosticList(owner, path, source, false, list)
			},
			Log: func(level, msg string) { s.plugin.Log(level, s.contrib.ID+": "+msg) },
		}})
	if err != nil {
		pr.Close()
		pw.Close()
		s.err = fmt.Errorf("%s: %w", launch.Command, err)
		s.plugin.Log("error", "could not start "+s.contrib.ID+": "+err.Error())
		return
	}
	s.client = c
	s.plugin.Log("info", "started "+s.contrib.ID)
	// The documents open of its languages, in each window of the folder.
	a.post(func() {
		if !s.current() {
			return
		}
		for _, w := range s.windows(a) {
			for _, d := range w.docs {
				if slices.Contains(s.contrib.Languages, languageOf(d)) {
					c.Open(serverURI(d.path), languageOf(d), d.buf.String())
				}
			}
		}
		if s.asked {
			a.tell(note{message: locales.LspRestarted(s.contrib.ID)})
		}
	})
	go func() {
		<-c.Done()
		s.plugin.Log("warn", s.contrib.ID+" stopped")
		// One stopped on purpose, or replaced, leaves the diagnostics to
		// what comes after it.
		if s.stopping.Load() || !s.current() {
			return
		}
		if plugins != nil {
			plugins.setDiagnosticList(owner, "", "", true, nil)
		}
		a.post(func() {
			a.tell(note{message: locales.LspStoppedOf(s.contrib.ID), label: locales.RunningRestart(),
				action: func() { a.restartServer(s.key) }})
		})
	}()
}

// owner is whose the server's diagnostics are, of its folder's.
func (s *lspServer) owner() string { return "lsp:" + s.contrib.ID + "\x00" + s.root }

// windows are the windows of the server's folder, a among them when it
// still shows it.
func (s *lspServer) windows(a *app) []*app {
	var ws []*app
	if a.root == s.root {
		ws = append(ws, a)
	}
	if plugins != nil {
		for _, w := range plugins.apps() {
			if w != a && w.root == s.root {
				ws = append(ws, w)
			}
		}
	}
	return ws
}

// stop stops the server, starting or running, and returns once its
// process is gone.
func (s *lspServer) stop() {
	s.stopping.Store(true)
	s.cancel()
	<-s.ready
	if s.client != nil {
		s.client.Close()
	}
}

// forgetFailedServers forgets the servers of the plugins of names, or of
// every plugin for none, that could not start or stopped, so that a
// document of their languages opening starts them afresh.
func forgetFailedServers(names ...string) {
	lspServers.Lock()
	defer lspServers.Unlock()
	for key, s := range lspServers.m {
		if st := s.state(); (st == lspFailed || st == lspStopped) && (len(names) == 0 || slices.Contains(names, s.plugin.Name)) {
			delete(lspServers.m, key)
		}
	}
}

// startServers starts the servers of the documents open, those that
// failed forgotten.
func (h *pluginHost) startServers() {
	for _, a := range h.apps() {
		for _, d := range a.docs {
			a.lspFor(d, true)
		}
	}
}

// fromLSP is a position of LSP's as plugins have it, the same.
func fromLSP(p lsp.Position) position { return position(p) }

// client is the server's client once it runs, nil before or after.
func (s *lspServer) live() *lsp.Client {
	if s == nil || s.state() != lspRunning {
		return nil
	}
	return s.client
}

// wait waits for the server to start, for so long.
func (s *lspServer) wait(limit time.Duration) *lsp.Client {
	if s == nil {
		return nil
	}
	select {
	case <-s.ready:
	case <-time.After(limit):
	}
	return s.live()
}

// moveServer takes a document whose language changed from the servers
// that have it open, but for the one of its new language, which opens it
// again as that language next. Each other server's diagnostics of it go,
// and so do those it sends late.
func (a *app) moveServer(d *document) {
	if d.path == "" {
		return
	}
	uri := serverURI(d.path)
	next := a.lspFor(d, false)
	lspServers.Lock()
	servers := make([]*lspServer, 0, len(lspServers.m))
	for _, s := range lspServers.m {
		servers = append(servers, s)
	}
	lspServers.Unlock()
	for _, s := range servers {
		c := s.live()
		if c == nil || !c.IsOpen(uri) {
			continue
		}
		c.Closed(uri)
		if s != next {
			s.detach(d.path)
		}
	}
}

// detach drops the server's diagnostics of a file it no longer has, and
// those it sends of it until it opens it again.
func (s *lspServer) detach(path string) {
	s.detachedMu.Lock()
	if s.detached == nil {
		s.detached = map[string]bool{}
	}
	s.detached[path] = true
	s.detachedMu.Unlock()
	if plugins != nil {
		plugins.setDiagnosticList(s.owner(), path, "", false, nil)
	}
}

// isDetached is whether diagnostics the server sends of a file are late
// ones, of a file a change of language took from it.
func (s *lspServer) isDetached(path, uri string) bool {
	s.detachedMu.Lock()
	defer s.detachedMu.Unlock()
	if !s.detached[path] {
		return false
	}
	if c := s.live(); c != nil && c.IsOpen(uri) {
		delete(s.detached, path)
		return false
	}
	return true
}

// lspDoc tells a document's server what happened to it: "open", "change",
// "save" or "close".
func (a *app) lspDoc(event string, d *document) {
	if event == "close" {
		lspServers.Lock()
		servers := make([]*lspServer, 0, len(lspServers.m))
		for _, s := range lspServers.m {
			servers = append(servers, s)
		}
		lspServers.Unlock()
		for _, s := range servers {
			if c := s.live(); c != nil {
				c.Closed(serverURI(d.path))
			}
		}
		return
	}
	s := a.lspFor(d, event == "open")
	if s == nil && event == "open" {
		a.offerServer(d)
	}
	c := s.live()
	if c == nil {
		return // the server opens it as it starts
	}
	uri := serverURI(d.path)
	switch event {
	case "save":
		c.Saved(uri)
	default:
		c.Open(uri, languageOf(d), d.buf.String())
	}
}

// stopServers stops every language server, as the app quits.
func stopServers() {
	lspServers.Lock()
	servers := make([]*lspServer, 0, len(lspServers.m))
	for _, s := range lspServers.m {
		servers = append(servers, s)
	}
	lspServers.m = map[string]*lspServer{}
	lspServers.Unlock()
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.stop()
		}()
	}
	wg.Wait()
}

// caretPosition is where the caret of an editor is, as LSP has it.
func caretPosition(d *editor) position {
	snap := d.buf.Snapshot()
	return positionOf(snap, min(d.sel[1], snap.Len()))
}

// showHover asks the server and the plugins of the document's language
// what is at the caret, and shows it. Asked again while its card shows,
// it gives the card the keys that scroll it.
func (a *app) showHover() {
	d := a.textEditor()
	if d == nil {
		return
	}
	if h := a.hover; h != nil && h.ed == d && !a.staleHover(h) {
		// The pointer's card, given the keys, stays as the pointer leaves.
		h.auto, h.focused = false, true
		return
	}
	// From the menu too, the pointer still on a name asks nothing more.
	a.hushRest()
	snap := d.buf.Snapshot()
	a.askHover(d, min(d.sel[1], snap.Len()), nil)
}

// askHover asks the server and the plugins of d's language what is at
// rune at, and shows it. rest is the name the pointer rests on, asked of
// quietly: of a server that runs, saying nothing of none or a failure;
// nil is the caret, as Show Hover asks.
func (a *app) askHover(d *editor, at int, rest *pointerHover) {
	pos := positionOf(d.buf.Snapshot(), at)
	var s *lspServer
	if rest == nil {
		s = a.lspFor(d.document, false)
	} else {
		s = a.lspRunningFor(d.document)
	}
	doc := a.docJSON(d.document)
	lang := languageOf(d.document)
	uri := serverURI(d.path)
	var providers []*plugin.Plugin
	if plugins != nil {
		for _, p := range plugins.enabled() {
			if slices.Contains(p.HoverLanguages(), lang) {
				providers = append(providers, p)
			}
		}
	}
	if rest != nil && s == nil && len(providers) == 0 {
		return
	}
	text := d.buf.String()
	var current func() bool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if rest == nil {
		current = a.lspAsked(s, d, &a.hoverAsked)
	} else {
		// Of the editor the pointer is over, which need not have the
		// keyboard: the name it still rests on, its text as it was.
		a.restAsked++
		n, version, word, picks := a.restAsked, d.buf.Version(), rest.word, a.picksOpened
		rest.ctx, rest.cancel = ctx, cancel
		current = func() bool {
			r := &a.rest
			return a.restAsked == n && r.ed == d && r.word == word && d.buf.Version() == version && languageOf(d.document) == lang &&
				a.picksOpened == picks && (s == nil || s.current())
		}
	}
	auto := rest != nil
	bg.Go(func() {
		defer cancel()
		var parts []string
		var span *lsp.Range
		if c := s.wait(3 * time.Second); c != nil && c.Hover() {
			// The server knows the text as it is now.
			c.Open(uri, lang, text)
			if hover, err := c.HoverAt(ctx, uri, lsp.Position(pos)); err == nil && strings.TrimSpace(hover.Markdown) != "" {
				parts, span = append(parts, hover.Markdown), hover.Range
			}
		}
		for _, p := range providers {
			if ctx.Err() != nil {
				break
			}
			res, err := p.Request(ctx, "hover", map[string]any{"language": lang, "document": doc, "position": pos})
			var text string
			if err == nil && json.Unmarshal(res, &text) == nil && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		a.post(func() {
			if !current() {
				return
			}
			if len(parts) == 0 {
				if auto {
					return // the pointer asked: nothing shows, nothing is said
				}
				// A server not running did not say there is nothing.
				if s != nil && s.state() != lspRunning {
					a.lspFailed(s)
					return
				}
				a.tell(note{message: locales.HoverNone()})
				return
			}
			if h := a.hover; auto && h != nil && !h.auto && !a.staleHover(h) {
				return // the card asked for at the caret stays
			}
			h := newHoverCard(d, strings.Join(parts, "\n\n---\n\n"), span, at)
			h.auto = auto
			a.hover = h
		})
	})
}

// lspAsked counts a question asked at the caret of d, of s or none, in
// asked, and is whether its answer still goes: the last of its kind asked,
// of the server still the folder's, at the caret, view and text it was
// asked at. One gone on from, a server restarted, or a picker opened
// since, as Quick Open, leaves it too late.
func (a *app) lspAsked(s *lspServer, d *editor, asked *int) func() bool {
	*asked++
	n, root, version, sel, picks := *asked, a.root, d.buf.Version(), d.sel, a.picksOpened
	language := languageOf(d.document)
	return func() bool {
		return *asked == n && a.root == root && a.activeEditor() == d && d.buf.Version() == version && d.sel == sel &&
			a.picksOpened == picks && languageOf(d.document) == language && (s == nil || s.current())
	}
}

const sourceDefinition lsp.Navigation = "sourceDefinition"

func (a *app) jumpCommand(kind lsp.Navigation) func() {
	return func() {
		if d := a.textEditor(); d != nil {
			a.locationsAt(d, kind, false)
		}
	}
}

// goToDefinition goes to where what is at the caret is defined, as the
// document's language server says.
func (a *app) goToDefinition() {
	if d := a.textEditor(); d != nil {
		a.definitionAt(d, false)
	}
}

// definitionAt goes to where what is at the caret of d is defined, or
// offers the places when there are several. A click, which a Cmd-click is,
// says nothing of a language with no server.
func (a *app) definitionAt(d *editor, click bool) {
	a.locationsAt(d, lsp.Definition, click)
}

// locationsAt uses the same document, stale-answer checks and targets for
// keyboard commands and Cmd-click. A click says nothing about missing servers.
func (a *app) locationsAt(d *editor, kind lsp.Navigation, click bool) {
	s := a.lspFor(d.document, false)
	current := a.lspAsked(s, d, &a.defAsked)
	if s == nil {
		if !click {
			a.tell(note{message: locales.LspNone(languageOf(d.document))})
		}
		return
	}
	pos, uri, root := caretPosition(d), serverURI(d.path), a.root
	lang, text := languageOf(d.document), d.buf.String()
	command := s.contrib.SourceDefinitionCommand
	bg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := s.wait(5 * time.Second)
		if c == nil {
			a.post(func() {
				if current() {
					a.lspFailed(s)
				}
			})
			return
		}
		supported := c.CanNavigate(kind)
		var sourceMethod string
		if kind == sourceDefinition {
			if source := s.launch.SourceDefinition; source != nil && c.SupportsExperimental(source.Capability) {
				sourceMethod = source.Method
			}
			supported = sourceMethod != "" || c.SupportsCommand(command)
		}
		if !supported {
			a.post(func() {
				if current() && !click {
					a.tell(note{message: locales.LspNavigationUnsupported()})
				}
			})
			return
		}
		// The server knows the text as it is now: not as the last change
		// it was told of a moment ago, nor before the documents open are
		// told of, as a server that just started is.
		c.Open(uri, lang, text)
		var locs []lsp.Location
		var err error
		if sourceMethod != "" {
			locs, err = c.SourceDefinitionRequestAt(ctx, sourceMethod, uri, lsp.Position(pos))
		} else if kind == sourceDefinition {
			locs, err = c.SourceDefinitionAt(ctx, command, uri, lsp.Position(pos))
		} else {
			locs, err = c.NavigateAt(ctx, kind, uri, lsp.Position(pos))
		}
		a.post(func() {
			if !current() {
				return
			}
			// A server that stopped or restarted did not say there is none.
			if err != nil && s.state() != lspRunning {
				a.lspFailed(s)
				return
			}
			if err != nil {
				s.plugin.Log("warn", fmt.Sprintf("%s: %v", kind, err))
				a.tell(note{message: locales.LspNavigationFailed()})
				return
			}
			if len(locs) == 0 {
				message := locales.LspNavigationNone()
				if kind == lsp.Definition {
					message = locales.DefinitionNone()
				}
				a.tell(note{message: message})
				return
			}
			type place struct {
				path string
				loc  lsp.Location
			}
			type spot struct {
				path string
				pos  lsp.Position
			}
			places := make([]place, 0, len(locs))
			seen := make(map[spot]bool, len(locs))
			lines := make(map[spot]int)
			for _, l := range locs {
				path := serverPath(root, l.URI)
				target := spot{path: path, pos: l.Range.Start}
				if path == "" || seen[target] {
					continue
				}
				seen[target] = true
				lines[spot{path: path, pos: lsp.Position{Line: target.pos.Line}}]++
				places = append(places, place{path, l})
			}
			if len(places) == 0 {
				message := locales.LspLocationElsewhere(locs[0].URI)
				if kind == lsp.Definition {
					message = locales.DefinitionElsewhere(locs[0].URI)
				}
				a.tell(note{message: message})
				return
			}
			if len(places) == 1 && kind != lsp.References {
				a.goToLocation(places[0].path, places[0].loc)
				return
			}
			items := make([]pickItem, len(places))
			for i, p := range places {
				start := p.loc.Range.Start
				label := filepath.Base(p.path) + ":" + strconv.Itoa(start.Line+1)
				// Places on one line are told apart by their column.
				if kind == lsp.References || lines[spot{path: p.path, pos: lsp.Position{Line: start.Line}}] > 1 {
					label += ":" + strconv.Itoa(start.Character+1)
				}
				items[i] = pickItem{Label: label, Description: a.relPath(filepath.Dir(p.path)), Icon: "file"}
			}
			title := locales.LspChooseLocation()
			switch kind {
			case lsp.Definition:
				title = locales.DefinitionPick()
			case lsp.References:
				title = locales.CmdGoReferences()
			}
			a.openPluginPicker(items, title, func(i int) {
				// The list opened is a picker, which current() counts:
				// the choice goes while the folder is the same.
				if i >= 0 && a.root == root {
					a.goToLocation(places[i].path, places[i].loc)
				}
			})
			a.pluginPick.icon = iconFileSearch
		})
	})
}

// goToLocation shows a place a language server named, the caret at its
// start.
func (a *app) goToLocation(path string, loc lsp.Location) {
	a.openFileAt(path, loc.Range.Start.Line, true)
	if ed := a.activeEditor(); ed != nil && ed.path == path {
		// At the name, once the file is read, not the line's start.
		ed.whenRead(func(ed *editor) {
			at := posRune(ed.buf.Snapshot(), fromLSP(loc.Range.Start))
			ed.selectRange(at, at, true)
			ed.lightLanding()
		})
	}
}

// lspFailed tells why a language server did not answer: one that could
// not start was told of as it failed.
func (a *app) lspFailed(s *lspServer) {
	switch {
	case s != nil && s.state() == lspFailed:
		a.tell(note{message: s.failMessage(), label: locales.RunningRestart(), action: func() { a.restartServer(s.key) }})
		return
	case s != nil && s.state() == lspStopped:
		a.tell(note{message: locales.LspDown(), label: locales.RunningRestart(), action: func() { a.restartServer(s.key) }})
		return
	}
	a.tell(note{message: locales.LspNotReady()})
}

// complete offers the completions of the document's language server at
// the caret, in a box, and puts the one chosen in place of the word before
// the caret.
func (a *app) complete() {
	d := a.textEditor()
	if d == nil {
		return
	}
	// In a wiki link of a note, the notes it may link to.
	if a.wikiSuggestNow(d) {
		return
	}
	s := a.lspFor(d.document, false)
	if s == nil {
		a.tell(note{message: locales.LspNone(languageOf(d.document))})
		return
	}
	pos, uri := caretPosition(d), serverURI(d.path)
	lang, text := languageOf(d.document), d.buf.String()
	current := a.lspAsked(s, d, &a.completeAsked)
	bg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := s.wait(5 * time.Second)
		if c == nil || !c.Completion() {
			a.post(func() {
				if current() {
					a.lspFailed(s)
				}
			})
			return
		}
		// The server knows the text as it is now.
		c.Open(uri, lang, text)
		items, err := c.CompletionAt(ctx, uri, lsp.Position(pos))
		a.post(func() {
			if !current() {
				return
			}
			// A server that stopped did not say there is none.
			if err != nil && s.state() != lspRunning {
				a.lspFailed(s)
				return
			}
			if err != nil || len(items) == 0 {
				a.tell(note{message: locales.CompletionNone()})
				return
			}
			slices.SortStableFunc(items, func(x, y lsp.CompletionItem) int {
				return strings.Compare(sortKey(x), sortKey(y))
			})
			if len(items) > 500 {
				items = items[:500]
			}
			picks := make([]pickItem, len(items))
			for i, it := range items {
				picks[i] = pickItem{Label: it.Label, Description: it.Detail, Icon: completionIcon(it.Kind)}
			}
			a.openPluginPicker(picks, locales.CompletionPlaceholder(), func(i int) {
				if i >= 0 {
					a.queueEdit(d, func(d *editor) { applyCompletion(d, items[i]) })
				}
			})
		})
	})
}

func sortKey(it lsp.CompletionItem) string {
	if it.SortText != "" {
		return it.SortText
	}
	return it.Label
}

// applyCompletion puts a completion in place: its edit, or its text in
// place of the word before the caret.
func applyCompletion(d *editor, it lsp.CompletionItem) {
	snap := d.buf.Snapshot()
	text := it.InsertText
	if text == "" {
		text = it.Label
	}
	var s, e int
	if it.TextEdit != nil {
		s, e = posRune(snap, fromLSP(it.TextEdit.Range.Start)), posRune(snap, fromLSP(it.TextEdit.Range.End))
		text = it.TextEdit.NewText
	} else {
		e = min(d.sel[1], snap.Len())
		s = e
		for s > 0 {
			r := []rune(snap.Slice(s-1, s))
			if len(r) == 0 || !(unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) || r[0] == '_') {
				break
			}
			s--
		}
	}
	if it.InsertTextFormat == 2 {
		d.selectRange(s, e, false)
		d.sel = [2]int{s, e}
		expandSnippet(d, text, map[string]string{})
		return
	}
	at := s + len([]rune(text))
	d.edit([2]int{at, at}, textEdit{s, e, text})
}

// completionIcon is the icon of a kind of completion.
func completionIcon(kind int) string {
	switch kind {
	case 2, 3, 4: // method, function, constructor
		return "function"
	case 5, 6, 10: // field, variable, property
		return "hash"
	case 7, 8, 22: // class, interface, struct
		return "box"
	case 9: // module
		return "package"
	case 14: // keyword
		return "keyboard"
	case 15: // snippet
		return "braces"
	case 17: // file
		return "file"
	case 19: // folder
		return "folder"
	}
	return "code"
}

// lspFormat formats a document with its language server, when it has one
// that formats, within limit; it reports false when there is none.
func (a *app) lspFormat(d *editor, limit time.Duration, then func()) bool {
	s := a.lspFor(d.document, false)
	c := s.live()
	if c == nil || !c.Formatting() {
		return false
	}
	c.Open(serverURI(d.path), languageOf(d.document), d.buf.String())
	snap := d.buf.Snapshot()
	version := snap.Version()
	size, spaces := max(d.indent.size, 1), !d.indent.tabs
	if d.indent.tabs {
		size = tabWidth()
	}
	uri := serverURI(d.path)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		edits, err := c.Format(ctx, uri, size, spaces)
		a.post(func() {
			defer func() {
				if then != nil {
					then()
				}
			}()
			if err != nil {
				if !errors.Is(err, context.DeadlineExceeded) {
					a.fail("%s", locales.LspFailed(firstLine(err.Error())))
				}
				return
			}
			if d.buf.Version() != version || len(edits) == 0 {
				return
			}
			data, _ := json.Marshal(lspEdits(edits))
			a.applyFormat(d, snap, data)
		})
	}()
	return true
}

func lspEdits(edits []lsp.TextEdit) []map[string]any {
	out := make([]map[string]any, len(edits))
	for i, e := range edits {
		out[i] = map[string]any{"range": e.Range, "text": e.NewText}
	}
	return out
}
