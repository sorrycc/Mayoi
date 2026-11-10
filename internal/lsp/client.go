// Package lsp is a client of the Language Server Protocol: it starts a
// server, speaks JSON-RPC with it over its standard input and output, and
// keeps it told of the documents open. The app asks it for diagnostics,
// hovers, definitions, completions and formatting.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Position is a place in a document: a line and a character in UTF-16
// code units, both from 0.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a run of a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range of a document of a URI.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic is a problem a server found.
type Diagnostic struct {
	Range    Range           `json:"range"`
	Severity int             `json:"severity,omitempty"` // 1 error, 2 warning, 3 info, 4 hint
	Code     json.RawMessage `json:"code,omitempty"`
	Source   string          `json:"source,omitempty"`
	Message  string          `json:"message"`
}

// TextEdit replaces a range with text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// CompletionItem is a completion a server offers.
type CompletionItem struct {
	Label      string    `json:"label"`
	Kind       int       `json:"kind,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	InsertText string    `json:"insertText,omitempty"`
	TextEdit   *TextEdit `json:"textEdit,omitempty"`
	FilterText string    `json:"filterText,omitempty"`
	SortText   string    `json:"sortText,omitempty"`
	// InsertTextFormat is 2 for a snippet.
	InsertTextFormat int `json:"insertTextFormat,omitempty"`
}

// Handlers are what the client does with what the server sends unasked.
type Handlers struct {
	// Diagnostics replaces the diagnostics of a document.
	Diagnostics func(uri string, items []Diagnostic)
	// Log is a message of the server's, and of the client's about it.
	Log func(level, msg string)
	// Notification receives notifications not otherwise handled by the client.
	Notification func(method string, params json.RawMessage)
}

// Options start a client.
type Options struct {
	// Cmd runs the server; its standard input and output are the client's.
	Cmd *exec.Cmd
	// Stdin, when set, is what writes to Cmd's standard input, which Cmd
	// reads from a pipe of the caller's, as a command of an SSH host does.
	Stdin io.WriteCloser
	// RootURI is the folder of the workspace.
	RootURI string
	// InitOptions are the server's initializationOptions.
	InitOptions json.RawMessage
	// Configuration answers workspace/configuration by section.
	Configuration    map[string]any
	WorkspaceFolders []WorkspaceFolder
	PathMappings     []PathMapping
	Handlers         Handlers
}

// Client is a running server.
type Client struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	h    Handlers
	caps struct {
		Hover, Definition, Completion, Formatting bool
		// Sync is the text document sync kind: 1 full, 2 incremental.
		Sync int
	}

	wonce   sync.Once
	writing chan struct{} // one frame writer; acquiring it can be cancelled
	mu      sync.Mutex
	seq     int
	waiting map[int]chan response
	done    chan struct{}
	err     error
	// versions are the documents open, by URI, and their versions.
	versions           map[string]int
	configuration      map[string]any
	folders            []WorkspaceFolder
	toServer, toClient []PathMapping
}

type response struct {
	result json.RawMessage
	err    error
}

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Start starts the server and initializes it.
func Start(ctx context.Context, opts Options) (*Client, error) {
	in := opts.Stdin
	if in == nil {
		var err error
		if in, err = opts.Cmd.StdinPipe(); err != nil {
			return nil, err
		}
	}
	out, err := opts.Cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, _ := opts.Cmd.StderrPipe()
	if err := opts.Cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: opts.Cmd, in: in, h: opts.Handlers, waiting: map[int]chan response{}, done: make(chan struct{}),
		versions: map[string]int{}, configuration: opts.Configuration, folders: opts.WorkspaceFolders,
		toServer: sortedMappings(opts.PathMappings, true), toClient: sortedMappings(opts.PathMappings, false)}
	if c.folders == nil {
		c.folders = []WorkspaceFolder{{URI: opts.RootURI, Name: filepath.Base(Path(opts.RootURI))}}
	}
	if stderr != nil {
		go c.logLines(stderr)
	}
	go c.read(bufio.NewReader(out))
	go func() {
		err := opts.Cmd.Wait()
		c.mu.Lock()
		if c.err == nil {
			c.err = fmt.Errorf("the server stopped: %v", err)
		}
		for id, ch := range c.waiting {
			ch <- response{nil, c.err}
			delete(c.waiting, id)
		}
		c.mu.Unlock()
		close(c.done)
	}()
	var initOpts any
	if len(opts.InitOptions) > 0 {
		initOpts = opts.InitOptions
	}
	params := map[string]any{
		"processId":             nil,
		"rootUri":               opts.RootURI,
		"workspaceFolders":      c.folders,
		"initializationOptions": initOpts,
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": false},
				"completion":         map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
				"formatting":         map[string]any{},
				"publishDiagnostics": map[string]any{"relatedInformation": false},
			},
			"workspace": map[string]any{"workspaceFolders": true, "configuration": true},
		},
		"clientInfo": map[string]any{"name": "Mayoi"},
	}
	res, err := c.Call(ctx, "initialize", params)
	if err != nil {
		c.Close()
		return nil, err
	}
	var init struct {
		Capabilities struct {
			HoverProvider              json.RawMessage `json:"hoverProvider"`
			DefinitionProvider         json.RawMessage `json:"definitionProvider"`
			CompletionProvider         json.RawMessage `json:"completionProvider"`
			DocumentFormattingProvider json.RawMessage `json:"documentFormattingProvider"`
			TextDocumentSync           json.RawMessage `json:"textDocumentSync"`
		} `json:"capabilities"`
	}
	json.Unmarshal(res, &init)
	on := func(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "false" && string(raw) != "null" }
	cp := init.Capabilities
	c.caps.Hover, c.caps.Definition, c.caps.Completion, c.caps.Formatting =
		on(cp.HoverProvider), on(cp.DefinitionProvider), on(cp.CompletionProvider), on(cp.DocumentFormattingProvider)
	c.caps.Sync = 1
	var kind int
	if json.Unmarshal(cp.TextDocumentSync, &kind) != nil {
		var o struct {
			Change int `json:"change"`
		}
		json.Unmarshal(cp.TextDocumentSync, &o)
		kind = o.Change
	}
	c.caps.Sync = kind
	c.Notify("initialized", map[string]any{})
	return c, nil
}

// Hover, Definition, Completion and Formatting report what the server
// answers.
func (c *Client) Hover() bool      { return c.caps.Hover }
func (c *Client) Definition() bool { return c.caps.Definition }
func (c *Client) Completion() bool { return c.caps.Completion }
func (c *Client) Formatting() bool { return c.caps.Formatting }

// Done is closed once the server stopped.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close asks the server to shut down, and stops it after a moment.
func (c *Client) Close() {
	select {
	case <-c.done:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Call(ctx, "shutdown", nil); err == nil {
		c.send(ctx, "exit", nil, nil)
	}
	c.in.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
	}
}

func (c *Client) logLines(r io.Reader) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64<<10), 1<<20)
	for s.Scan() {
		if c.h.Log != nil {
			c.h.Log("debug", s.Text())
		}
	}
}

// write waits as long as the server takes to read: a busy server may not read
// a large document for a while, and a deadline would close its input.
func (c *Client) write(m any) error {
	return c.writeContext(context.Background(), m)
}

// A cancelled queued writer must leave the shared stream intact. Once a frame
// starts writing, a timeout closes the stream because a partial frame cannot be
// safely followed by another message.
func (c *Client) writeContext(ctx context.Context, m any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wonce.Do(func() { c.writing = make(chan struct{}, 1) })
	select {
	case c.writing <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.writing }()
	if err := ctx.Err(); err != nil {
		return err
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.in.Close(); close(closed) })
	if _, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n", len(data)); err == nil {
		_, err = c.in.Write(data)
	}
	if !stop() {
		// The callback may already be running. Finish it before releasing
		// ownership so it cannot close a later writer's stream.
		<-closed
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Call sends a request and waits for its result.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.seq++
	id := c.seq
	ch := make(chan response, 1)
	c.waiting[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.waiting, id); c.mu.Unlock() }()
	if err := c.send(ctx, method, params, &id); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return mapResult(method, r.result, c.toClient, false), r.err
	case <-ctx.Done():
		c.Notify("$/cancelRequest", map[string]any{"id": id})
		return nil, ctx.Err()
	}
}

// Notify sends a notification.
func (c *Client) Notify(method string, params any) error {
	return c.send(context.Background(), method, params, nil)
}

func (c *Client) send(ctx context.Context, method string, params any, id *int) error {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = *id
	}
	if params != nil || id != nil {
		m["params"] = mapParameterValues(method, params, c.toServer)
	}
	return c.writeContext(ctx, m)
}

// read reads the server's messages until it stops.
func (c *Client) read(r *bufio.Reader) {
	// A child can exit while os/exec is still copying stdin from a caller's
	// pipe. Closing the writer unblocks that copy so Wait and Done complete.
	defer c.in.Close()
	for {
		n := -1
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(strings.ToLower(line), "content-length:"); ok {
				n, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		if n < 0 || n > 64<<20 {
			return
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var m message
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		m.Params = mapParams(m.Method, m.Params, c.toClient, false)
		switch {
		case m.ID != nil && m.Method == "":
			var id int
			json.Unmarshal(*m.ID, &id)
			c.mu.Lock()
			ch := c.waiting[id]
			delete(c.waiting, id)
			c.mu.Unlock()
			if ch != nil {
				if m.Error != nil {
					ch <- response{nil, errors.New(m.Error.Message)}
				} else {
					ch <- response{m.Result, nil}
				}
			}
		case m.ID != nil:
			c.answer(*m.ID, m.Method, m.Params)
		default:
			c.notified(m.Method, m.Params)
		}
	}
}

// answer answers a request of the server's.
func (c *Client) answer(id json.RawMessage, method string, params json.RawMessage) {
	var result any
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		json.Unmarshal(params, &p)
		values := make([]any, len(p.Items))
		for i, item := range p.Items {
			var value any = c.configuration
			if item.Section != "" {
				for _, part := range strings.Split(item.Section, ".") {
					if object, ok := value.(map[string]any); ok {
						value = object[part]
					} else {
						value = nil
						break
					}
				}
			}
			values[i] = value
		}
		result = values
	case "workspace/workspaceFolders":
		result = c.folders
	}
	raw, err := json.Marshal(result)
	if err == nil {
		c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": mapResult(method, raw, c.toServer, true)})
	}
}

func (c *Client) notified(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(params, &p) == nil && c.h.Diagnostics != nil {
			c.h.Diagnostics(p.URI, p.Diagnostics)
		}
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &p) == nil && c.h.Log != nil {
			level := "info"
			switch p.Type {
			case 1:
				level = "error"
			case 2:
				level = "warn"
			}
			c.h.Log(level, p.Message)
		}
	default:
		if c.h.Notification != nil {
			c.h.Notification(method, params)
		}
	}
}

// Open tells the server of a document open, or of its new text: the whole
// text each time, which every server takes.
func (c *Client) Open(uri, language, text string) {
	c.mu.Lock()
	v, open := c.versions[uri]
	v++
	c.versions[uri] = v
	c.mu.Unlock()
	if !open {
		c.Notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": language, "version": v, "text": text,
		}})
		return
	}
	c.Notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": v},
		"contentChanges": []any{map[string]any{"text": text}},
	})
}

// IsOpen reports whether the server was told of a document.
func (c *Client) IsOpen(uri string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.versions[uri]
	return ok
}

// Saved tells the server a document was saved.
func (c *Client) Saved(uri string) {
	if c.IsOpen(uri) {
		c.Notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}})
	}
}

// Closed tells the server a document closed.
func (c *Client) Closed(uri string) {
	c.mu.Lock()
	_, open := c.versions[uri]
	delete(c.versions, uri)
	c.mu.Unlock()
	if open {
		c.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	}
}

func docPos(uri string, pos Position) map[string]any {
	return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}
}

// HoverAt is the hover of a place, as Markdown, "" for none.
func (c *Client) HoverAt(ctx context.Context, uri string, pos Position) (string, error) {
	res, err := c.Call(ctx, "textDocument/hover", docPos(uri, pos))
	if err != nil || len(res) == 0 || string(res) == "null" {
		return "", err
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	json.Unmarshal(res, &h)
	return markupText(h.Contents), nil
}

// markupText reads hover contents: MarkupContent, a MarkedString, or a
// list of them.
func markupText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var mc struct {
		Kind     string `json:"kind"`
		Value    string `json:"value"`
		Language string `json:"language"`
	}
	if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
		if mc.Language != "" {
			return "```" + mc.Language + "\n" + mc.Value + "\n```"
		}
		return mc.Value
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		var parts []string
		for _, item := range list {
			if t := markupText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// DefinitionAt is where what is at a place is defined.
func (c *Client) DefinitionAt(ctx context.Context, uri string, pos Position) ([]Location, error) {
	res, err := c.Call(ctx, "textDocument/definition", docPos(uri, pos))
	if err != nil || len(res) == 0 || string(res) == "null" {
		return nil, err
	}
	var one Location
	if json.Unmarshal(res, &one) == nil && one.URI != "" {
		return []Location{one}, nil
	}
	var many []struct {
		Location
		TargetURI   string `json:"targetUri"`
		TargetRange Range  `json:"targetSelectionRange"`
	}
	json.Unmarshal(res, &many)
	var out []Location
	for _, l := range many {
		if l.TargetURI != "" {
			out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
		} else if l.URI != "" {
			out = append(out, l.Location)
		}
	}
	return out, nil
}

// CompletionAt are the completions at a place.
func (c *Client) CompletionAt(ctx context.Context, uri string, pos Position) ([]CompletionItem, error) {
	res, err := c.Call(ctx, "textDocument/completion", docPos(uri, pos))
	if err != nil || len(res) == 0 || string(res) == "null" {
		return nil, err
	}
	var list struct {
		Items []CompletionItem `json:"items"`
	}
	if json.Unmarshal(res, &list) == nil && list.Items != nil {
		return list.Items, nil
	}
	var items []CompletionItem
	json.Unmarshal(res, &items)
	return items, nil
}

// Format are the edits that format a document.
func (c *Client) Format(ctx context.Context, uri string, tabSize int, spaces bool) ([]TextEdit, error) {
	res, err := c.Call(ctx, "textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": tabSize, "insertSpaces": spaces},
	})
	if err != nil {
		return nil, err
	}
	var edits []TextEdit
	json.Unmarshal(res, &edits)
	return edits, nil
}

// URI is the file: URI of a path the server sees.
func URI(path string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// Path is the path of a file: URI, "" for another scheme.
func Path(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	// "/C:/a" is C:\a on Windows.
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}
