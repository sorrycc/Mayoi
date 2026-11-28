package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func actionClient(t *testing.T, mode string) *Client {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
	options, _ := json.Marshal(map[string]any{"sourceActions": true, "actionMode": mode})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Start(ctx, Options{Cmd: cmd, RootURI: URI(t.TempDir()), InitOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestSourceActions(t *testing.T) {
	c := actionClient(t, "")
	if !c.CodeActionsSupported(OrganizeImports) || !c.CodeActionsSupported(FixAll) || c.CodeActionsSupported("quickfix") {
		t.Fatal("source action capabilities")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uri := URI("/work/actions.fake")
	version, err := c.OpenContext(ctx, uri, "fake", "needimports\nbad\n")
	if err != nil {
		t.Fatal(err)
	}
	if same, err := c.OpenContext(ctx, uri, "fake", "needimports\nbad\n"); err != nil || same != version {
		t.Fatalf("unchanged synchronization: version %d -> %d, %v", version, same, err)
	}
	actions, err := c.CodeActions(ctx, uri, OrganizeImports)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := SourceActionEdits(actions, OrganizeImports, uri, version)
	if err != nil || len(edits) != 1 || edits[0].NewText != "// organized\n" {
		t.Fatalf("imports: %+v, %v", edits, err)
	}
	version, err = c.OpenContext(ctx, uri, "fake", "// organized\nneedimports\nbad\n")
	if err != nil {
		t.Fatal(err)
	}
	actions, err = c.CodeActions(ctx, uri, FixAll)
	if err != nil {
		t.Fatal(err)
	}
	edits, err = SourceActionEdits(actions, FixAll, uri, version)
	if err != nil || len(edits) != 1 || edits[0].Range.Start.Line != 2 || edits[0].NewText != "good" {
		t.Fatalf("fix all after imports: %+v, %v", edits, err)
	}
	data, err := c.Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		CodeActions []struct {
			Context struct{ Diagnostics []json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if got := snapshot.CodeActions[1].Context.Diagnostics; len(got) != 1 || !strings.Contains(string(got[0]), `"opaque":"kept"`) {
		t.Fatalf("original diagnostic data was lost: %s", data)
	}
}

func TestSourceActionDiagnosticsDeadline(t *testing.T) {
	c := actionClient(t, "noReply")
	uri := URI("/work/actions.fake")
	c.Open(uri, "fake", "bad\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.CodeActions(ctx, uri, FixAll); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Call(ctx, "test/snapshot", nil); err != nil {
		t.Fatalf("deadline disconnected transport: %v", err)
	}
}

func TestSourceActionWithoutDiagnostics(t *testing.T) {
	c := actionClient(t, "noDiagnostics")
	uri := URI("/work/no-diagnostics.fake")
	c.Open(uri, "fake", "bad\n")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := c.CodeActions(ctx, uri, FixAll); err != nil {
		t.Fatal(err)
	}
	data, err := c.Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		CodeActions []struct {
			Context struct{ Diagnostics []json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CodeActions) != 1 || len(snapshot.CodeActions[0].Context.Diagnostics) != 0 {
		t.Fatalf("no empty-context request: %s", data)
	}
}

func TestSourceActionBooleanCapability(t *testing.T) {
	c := actionClient(t, "booleanCapability")
	if !c.CodeActionsSupported(FixAll) || !c.CodeActionsSupported(OrganizeImports) {
		t.Fatal("boolean capability rejected")
	}
}

func TestSourceActionDiagnosticVersions(t *testing.T) {
	uri := URI("/work/actions.fake")
	c := &Client{documents: map[string]*syncedDocument{uri: {version: 2, changed: make(chan struct{})}}}
	for _, version := range []int{1, 3} {
		p, _ := json.Marshal(map[string]any{"uri": uri, "version": version, "diagnostics": []any{map[string]any{"message": "stale"}}})
		c.publishedDiagnostics(p)
		if c.documents[uri].diagnosed {
			t.Fatalf("accepted diagnostics of version %d", version)
		}
	}
	p, _ := json.Marshal(map[string]any{"uri": uri, "version": 2, "diagnostics": []any{map[string]any{"message": "current", "extra": 42}}})
	c.publishedDiagnostics(p)
	if !c.documents[uri].diagnosed || !strings.Contains(string(c.documents[uri].diagnostics[0]), `"extra":42`) {
		t.Fatal("lost current diagnostic or unknown properties")
	}
	c.documents[uri].version++
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.diagnosticsFor(ctx, uri, 2); err == nil {
		t.Fatal("wait survived a document change")
	}
}

func TestSourceActionStagedDiagnostics(t *testing.T) {
	uri := URI("/work/staged.fake")
	c := &Client{documents: map[string]*syncedDocument{uri: {version: 1, changed: make(chan struct{})}}}
	publish := func(items string) {
		t.Helper()
		params := `{"uri":"` + uri + `","version":1,"diagnostics":` + items + `}`
		c.publishedDiagnostics(json.RawMessage(params))
	}
	publish(`[]`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	answer := make(chan []json.RawMessage, 1)
	go func() { items, _ := c.diagnosticsFor(ctx, uri, 1); answer <- items }()
	select {
	case <-answer:
		t.Fatal("used the preliminary empty diagnostic set")
	case <-time.After(20 * time.Millisecond):
	}
	publish(`[{"message":"analysis fix","data":{"opaque":true}}]`)
	select {
	case items := <-answer:
		if len(items) != 1 || !strings.Contains(string(items[0]), "analysis fix") {
			t.Fatalf("diagnostics: %s", items)
		}
	case <-ctx.Done():
		t.Fatal("did not use the subsequent analysis diagnostics")
	}
}

func TestDocumentSyncWaitsAndHonorsDeadline(t *testing.T) {
	w := &heldWriter{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	t.Cleanup(func() { w.Close() })
	c := &Client{in: w, documents: map[string]*syncedDocument{}, opening: make(chan struct{}, 1)}
	uri := URI("/work/ordered.fake")
	opened := make(chan struct{})
	go func() { c.Open(uri, "fake", "first"); close(opened) }()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("document synchronization did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.OpenContext(ctx, uri, "fake", "second"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued synchronization: %v", err)
	}
	select {
	case <-w.closed:
		t.Fatal("queued synchronization closed the transport")
	default:
	}
	closed := make(chan struct{})
	go func() { c.Closed(uri); close(closed) }()
	select {
	case <-closed:
		t.Fatal("document closed before its synchronization finished")
	case <-time.After(20 * time.Millisecond):
	}
	if !c.IsOpen(uri) {
		t.Fatal("pending close discarded the document during synchronization")
	}
	close(w.release)
	for _, done := range []<-chan struct{}{opened, closed} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("document notification did not finish")
		}
	}
	if c.IsOpen(uri) {
		t.Fatal("document remained open after closing")
	}
}

func TestSourceActionEdits(t *testing.T) {
	const uri = "file:///work/actions.fake"
	const edit = `{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}`
	for _, tc := range []struct {
		name, body string
		count      int
		bad        bool
	}{
		{"changes", `"edit":{"changes":{"` + uri + `":[` + edit + `]}}`, 1, false},
		{"documentChanges", `"edit":{"documentChanges":[{"textDocument":{"uri":"` + uri + `","version":2},"edits":[` + edit + `]}]}`, 1, false},
		{"duplicates", `"edit":{"changes":{"` + uri + `":[` + edit + `,` + edit + `]}}`, 1, false},
		{"commandAndEdit", `"command":{"command":"fix"},"edit":{"changes":{"` + uri + `":[` + edit + `]}}`, 0, true},
		{"resolution", `"data":{"opaque":1}`, 0, true},
		{"foreign", `"edit":{"changes":{"file:///another.fake":[` + edit + `],"` + uri + `":[` + edit + `]}}`, 0, true},
		{"outdated", `"edit":{"documentChanges":[{"textDocument":{"uri":"` + uri + `","version":1},"edits":[` + edit + `]}]}`, 0, true},
		{"resource", `"edit":{"documentChanges":[{"kind":"create","uri":"` + uri + `"}]}`, 0, true},
		{"annotations", `"edit":{"changeAnnotations":{"a":{}},"changes":{"` + uri + `":[` + edit + `]}}`, 0, true},
		{"disabled", `"disabled":{"reason":"not available"}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actions []CodeAction
			if err := json.Unmarshal([]byte(`[{"title":"Fix","kind":"source.fixAll",`+tc.body+`}]`), &actions); err != nil {
				t.Fatal(err)
			}
			edits, err := SourceActionEdits(actions, FixAll, uri, 2)
			if (err != nil) != tc.bad || len(edits) != tc.count {
				t.Fatalf("edits %+v, error %v", edits, err)
			}
		})
	}
}

func TestSourceActionURIMapping(t *testing.T) {
	mappings := []PathMapping{{ClientURI: "file:///alias", ServerURI: "file:///server"}}
	params := json.RawMessage(`{"textDocument":{"uri":"file:///alias/a.go"},"context":{"diagnostics":[{"data":{"uri":"file:///alias/opaque"},"relatedInformation":[{"location":{"uri":"file:///alias/b.go"}}]}]}}`)
	mapped := mapParams("textDocument/codeAction", params, mappings, true)
	if !strings.Contains(string(mapped), `"uri":"file:///server/a.go"`) || !strings.Contains(string(mapped), `"uri":"file:///server/b.go"`) || !strings.Contains(string(mapped), `"uri":"file:///alias/opaque"`) {
		t.Fatal(string(mapped))
	}
	result := json.RawMessage(`[{"edit":{"changes":{"file:///server/a.go":[{"newText":"file:///server/literal"}]},"documentChanges":[{"textDocument":{"uri":"file:///server/a.go","version":1},"edits":[]}]},"data":{"uri":"file:///server/opaque"}}]`)
	mapped = mapResult("textDocument/codeAction", result, mappings, false)
	if !strings.Contains(string(mapped), `"file:///alias/a.go"`) || !strings.Contains(string(mapped), `"uri":"file:///alias/a.go"`) || !strings.Contains(string(mapped), `"file:///server/literal"`) || !strings.Contains(string(mapped), `"uri":"file:///server/opaque"`) {
		t.Fatal(string(mapped))
	}
}
