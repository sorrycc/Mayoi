package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

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
	plugin   *plugin.Plugin
	contrib  plugin.ServerContrib
	root     string
	client   *lsp.Client
	ready    chan struct{} // closed once started, or failed to
	err      error
	failedAt time.Time // when it could not start
	starts   int       // since its plugin was last loaded or turned on
}

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
	if host, _, ok := remote.Split(root); ok {
		return remote.Path(host, p)
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
	key := a.root + "\x00" + sc.ID
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
	s = &lspServer{plugin: p, contrib: *sc, root: a.root, ready: make(chan struct{}), starts: starts + 1}
	lspServers.m[key] = s
	go s.start(a)
	return s
}

// start starts the server on the folder's machine.
func (s *lspServer) start(a *app) {
	defer func() {
		// One that could not start is told of once, in the window that
		// started it.
		if s.client == nil {
			s.failedAt = time.Now()
			msg := locales.LspFailed(firstLine(s.err.Error()))
			a.post(func() { a.fail("%s", msg) })
		}
		close(s.ready)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	owner := "lsp:" + s.contrib.ID
	root := s.root
	rootURI := serverURI(root)
	if len(launch.WorkspaceFolders) > 0 {
		rootURI = launch.WorkspaceFolders[0].URI
	}
	c, err := lsp.Start(ctx, lsp.Options{Cmd: cmd, Stdin: pw, RootURI: rootURI, InitOptions: launch.Options,
		Configuration: launch.Configuration, WorkspaceFolders: launch.WorkspaceFolders, PathMappings: launch.PathMappings,
		Handlers: lsp.Handlers{
			Diagnostics: func(uri string, items []lsp.Diagnostic) {
				path := serverPath(root, uri)
				if path == "" || plugins == nil {
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
		s.err = fmt.Errorf("%s: %v", launch.Command, err)
		s.plugin.Log("error", "could not start "+s.contrib.ID+": "+err.Error())
		return
	}
	s.client = c
	s.plugin.Log("info", "started "+s.contrib.ID)
	// The documents open of its languages.
	a.post(func() {
		for _, d := range a.docs {
			if slices.Contains(s.contrib.Languages, languageOf(d)) {
				c.Open(serverURI(d.path), languageOf(d), d.buf.String())
			}
		}
	})
	go func() {
		<-c.Done()
		s.plugin.Log("warn", s.contrib.ID+" stopped")
		if plugins != nil {
			plugins.setDiagnosticList(owner, "", "", true, nil)
		}
	}()
}

// forgetFailedServers forgets the servers of the plugins of names, or of
// every plugin for none, that could not start, so that a document of their
// languages opening starts them afresh.
func forgetFailedServers(names ...string) {
	lspServers.Lock()
	defer lspServers.Unlock()
	for key, s := range lspServers.m {
		if s.state() == lspFailed && (len(names) == 0 || slices.Contains(names, s.plugin.Name)) {
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
		if c := s.live(); c != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.Close()
			}()
		}
	}
	wg.Wait()
}

// caretPosition is where the caret of an editor is, as LSP has it.
func caretPosition(d *editor) position {
	snap := d.buf.Snapshot()
	return positionOf(snap, min(d.sel[1], snap.Len()))
}

// hoverCard is what Show Hover shows: Markdown of the language server and
// the plugins, over the editor.
type hoverCard struct {
	tree *viewNode
}

// showHover asks the server and the plugins of the document's language
// what is at the caret, and shows it.
func (a *app) showHover() {
	d := a.textEditor()
	if d == nil {
		return
	}
	pos := caretPosition(d)
	s := a.lspFor(d.document, false)
	doc := docJSON(d.document)
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var parts []string
		if c := s.wait(3 * time.Second); c != nil && c.Hover() {
			if text, err := c.HoverAt(ctx, uri, lsp.Position(pos)); err == nil && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		for _, p := range providers {
			res, err := p.Request(ctx, "hover", map[string]any{"language": lang, "document": doc, "position": pos})
			var text string
			if err == nil && json.Unmarshal(res, &text) == nil && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		a.post(func() {
			if len(parts) == 0 {
				a.tell(note{message: locales.HoverNone()})
				return
			}
			a.hover = &hoverCard{tree: &viewNode{Type: "markdown", Text: strings.Join(parts, "\n\n---\n\n")}}
		})
	}()
}

// hoverView draws the hover card while it shows.
func (a *app) hoverView(c *ui.Context) {
	h := a.hover
	if h == nil {
		return
	}
	p := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(620).MaxWidthPercent(92).MaxHeightPercent(70).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).
				Clip().Role(ui.RoleDialog).Label(locales.CmdGoHover())
			box.Children(func() {
				ui.Scroll(c.Key("hover-card")).Padding(12, 16).Background(p.editor).Children(func() {
					a.drawViewNode(c, "hover", h.tree, nil)
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.hover = nil
			}
		})
	})
}

// goToDefinition goes to where what is at the caret is defined, as the
// document's language server says.
func (a *app) goToDefinition() {
	d := a.textEditor()
	if d == nil {
		return
	}
	s := a.lspFor(d.document, false)
	if s == nil {
		a.tell(note{message: locales.LspNone(languageOf(d.document))})
		return
	}
	pos, uri, root := caretPosition(d), serverURI(d.path), a.root
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := s.wait(5 * time.Second)
		if c == nil || !c.Definition() {
			a.post(func() { a.lspFailed(s) })
			return
		}
		locs, err := c.DefinitionAt(ctx, uri, lsp.Position(pos))
		a.post(func() {
			if err != nil || len(locs) == 0 {
				a.tell(note{message: locales.DefinitionNone()})
				return
			}
			path := serverPath(root, locs[0].URI)
			if path == "" {
				return
			}
			a.openFileAt(path, locs[0].Range.Start.Line, true)
			if ed := a.activeEditor(); ed != nil && ed.path == path {
				// At the name, once the file is read, not the line's start.
				ed.whenRead(func(ed *editor) {
					at := posRune(ed.buf.Snapshot(), fromLSP(locs[0].Range.Start))
					ed.selectRange(at, at, true)
					ed.lightLanding()
				})
			}
		})
	}()
}

// lspFailed tells why a language server did not answer: one that could
// not start was told of as it failed.
func (a *app) lspFailed(s *lspServer) {
	if s != nil && (s.state() == lspFailed || s.state() == lspStopped) {
		a.tell(note{message: locales.LspDown()})
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
	s := a.lspFor(d.document, false)
	if s == nil {
		a.tell(note{message: locales.LspNone(languageOf(d.document))})
		return
	}
	// The server knows the text as it is now.
	if c := s.live(); c != nil {
		c.Open(serverURI(d.path), languageOf(d.document), d.buf.String())
	}
	pos, uri, version := caretPosition(d), serverURI(d.path), d.buf.Version()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := s.wait(5 * time.Second)
		if c == nil || !c.Completion() {
			a.post(func() { a.lspFailed(s) })
			return
		}
		items, err := c.CompletionAt(ctx, uri, lsp.Position(pos))
		a.post(func() {
			if err != nil || len(items) == 0 {
				a.tell(note{message: locales.CompletionNone()})
				return
			}
			if d.buf.Version() != version {
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
	}()
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
