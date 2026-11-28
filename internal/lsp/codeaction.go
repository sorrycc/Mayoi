package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	OrganizeImports  = "source.organizeImports"
	FixAll           = "source.fixAll"
	diagnosticSettle = 100 * time.Millisecond
)

type syncedDocument struct {
	version           int
	language, text    string
	synced, diagnosed bool
	diagnostics       []json.RawMessage
	changed           chan struct{}
	diagnosedAt       time.Time
}

// OpenContext synchronizes changed text within ctx and returns its protocol version.
func (c *Client) OpenContext(ctx context.Context, uri, language, text string) (int, error) {
	select {
	case c.opening <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	defer func() { <-c.opening }()
	c.mu.Lock()
	doc, open := c.documents[uri]
	if open && doc.synced && doc.text == text && doc.language == language {
		version := doc.version
		c.mu.Unlock()
		return version, nil
	}
	if !open {
		doc = &syncedDocument{changed: make(chan struct{})}
		c.documents[uri] = doc
	}
	doc.version++
	doc.text, doc.language, doc.synced, doc.diagnosed = text, language, false, false
	doc.diagnostics = nil
	close(doc.changed)
	doc.changed = make(chan struct{})
	version := doc.version
	c.mu.Unlock()
	method := "textDocument/didChange"
	params := map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
		"contentChanges": []any{map[string]any{"text": text}}}
	if !open {
		method = "textDocument/didOpen"
		params = map[string]any{"textDocument": map[string]any{
			"uri": uri, "version": version, "languageId": LanguageID(language, uri), "text": text}}
	}
	err := c.send(ctx, method, params, nil)
	c.mu.Lock()
	doc.synced = err == nil
	c.mu.Unlock()
	return version, err
}

func (c *Client) publishedDiagnostics(params json.RawMessage) {
	var p struct {
		URI         string          `json:"uri"`
		Version     *int            `json:"version"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	var raw []json.RawMessage
	if json.Unmarshal(params, &p) != nil || json.Unmarshal(p.Diagnostics, &raw) != nil {
		return
	}
	c.mu.Lock()
	if doc := c.documents[p.URI]; doc != nil {
		if p.Version != nil && *p.Version != doc.version {
			c.mu.Unlock()
			return
		}
		doc.diagnostics, doc.diagnosed = raw, true
		doc.diagnosedAt = time.Now()
		close(doc.changed)
		doc.changed = make(chan struct{})
	}
	c.mu.Unlock()
	if c.h.Diagnostics != nil {
		var items []Diagnostic
		if json.Unmarshal(p.Diagnostics, &items) == nil {
			c.h.Diagnostics(p.URI, items)
		}
	}
}

func (c *Client) CodeActionsSupported(kind string) bool {
	if !c.caps.CodeActions {
		return false
	}
	if len(c.caps.CodeActionKinds) == 0 {
		return true
	}
	for _, offered := range c.caps.CodeActionKinds {
		if offered == "" || kind == offered || strings.HasPrefix(kind, offered+".") || strings.HasPrefix(offered, kind+".") {
			return true
		}
	}
	return false
}

// CodeActions requests one source-action kind against the synchronized document.
func (c *Client) CodeActions(ctx context.Context, uri, kind string) ([]CodeAction, error) {
	c.mu.Lock()
	doc := c.documents[uri]
	if doc == nil {
		c.mu.Unlock()
		return nil, errors.New("the document is not open")
	}
	version, text := doc.version, doc.text
	c.mu.Unlock()
	diagnostics := []json.RawMessage{}
	if kind == FixAll {
		var err error
		diagnostics, err = c.diagnosticsFor(ctx, uri, version)
		if err != nil {
			return nil, err
		}
	}
	last := text[strings.LastIndex(text, "\n")+1:]
	res, err := c.Call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        Range{End: Position{strings.Count(text, "\n"), len(utf16.Encode([]rune(last)))}},
		"context":      map[string]any{"only": []string{kind}, "diagnostics": diagnostics},
	})
	if err != nil {
		return nil, err
	}
	var actions []CodeAction
	err = json.Unmarshal(res, &actions)
	return actions, err
}

func (c *Client) diagnosticsFor(ctx context.Context, uri string, version int) ([]json.RawMessage, error) {
	deadline, bounded := ctx.Deadline()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		doc := c.documents[uri]
		if doc == nil || doc.version != version {
			c.mu.Unlock()
			return nil, errors.New("the document changed")
		}
		var quiet <-chan time.Time
		// Servers may clear old errors before publishing analysis fixes of the same version.
		delay := time.Until(doc.diagnosedAt.Add(diagnosticSettle))
		if !doc.diagnosed || len(doc.diagnostics) == 0 && delay > 0 {
			if bounded {
				// Allow delayed analysis, reserving time for the request and formatter.
				delay = time.Until(deadline.Add(-diagnosticSettle))
			}
		}
		if (doc.diagnosed || bounded) && delay <= 0 {
			items := append([]json.RawMessage{}, doc.diagnostics...)
			c.mu.Unlock()
			return items, nil
		} else if doc.diagnosed || bounded {
			quiet = time.After(delay)
		}
		changed := doc.changed
		c.mu.Unlock()
		select {
		case <-quiet:
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return nil, errors.New("the language server stopped")
		}
	}
}

type CodeAction struct {
	Title    string          `json:"title"`
	Kind     string          `json:"kind"`
	Edit     *WorkspaceEdit  `json:"edit"`
	Command  json.RawMessage `json:"command"`
	Disabled *struct {
		Reason string `json:"reason"`
	} `json:"disabled"`
}

type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes"`
	DocumentChanges []struct {
		Kind         string `json:"kind"`
		TextDocument *struct {
			URI     string `json:"uri"`
			Version *int   `json:"version"`
		} `json:"textDocument"`
		Edits []TextEdit `json:"edits"`
	} `json:"documentChanges"`
	ChangeAnnotations map[string]json.RawMessage `json:"changeAnnotations"`
}

// SourceActionEdits accepts only complete, unannotated text edits of uri at version.
func SourceActionEdits(actions []CodeAction, kind, uri string, version int) ([]TextEdit, error) {
	var out []TextEdit
	seen := map[TextEdit]bool{}
	for _, action := range actions {
		if action.Disabled != nil {
			continue
		}
		if action.Kind == "" {
			return nil, errors.New("this action does not identify its source kind")
		}
		if action.Kind != kind && !strings.HasPrefix(action.Kind, kind+".") {
			continue
		}
		if len(action.Command) > 0 && string(action.Command) != "null" || action.Edit == nil {
			return nil, errors.New("this action requires command execution or resolution")
		}
		edits, err := action.Edit.editsFor(uri, version)
		if err != nil {
			return nil, err
		}
		for _, edit := range edits {
			if edit.AnnotationID != "" {
				return nil, errors.New("this action requires annotated edits")
			}
			if !seen[edit] {
				out = append(out, edit)
				seen[edit] = true
			}
		}
	}
	return out, nil
}

func (w *WorkspaceEdit) editsFor(uri string, version int) ([]TextEdit, error) {
	if len(w.ChangeAnnotations) > 0 || len(w.Changes) > 0 && len(w.DocumentChanges) > 0 {
		return nil, errors.New("this action requires unsupported workspace edits")
	}
	var out []TextEdit
	for target, edits := range w.Changes {
		if target != uri {
			return nil, errors.New("this action changes another file")
		}
		out = append(out, edits...)
	}
	for _, change := range w.DocumentChanges {
		if change.Kind != "" || change.TextDocument == nil || change.TextDocument.URI != uri {
			return nil, errors.New("this action changes another file or performs a file operation")
		}
		if change.TextDocument.Version != nil && *change.TextDocument.Version != version {
			return nil, errors.New("this action targets an outdated document version")
		}
		out = append(out, change.Edits...)
	}
	return out, nil
}
