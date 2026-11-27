package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"mayoi/internal/lsp/lsptest"
)

// TestMain runs the test binary as a fake language server when asked.
func TestMain(m *testing.M) {
	if os.Getenv("MAYOI_FAKE_LSP") == "1" {
		lsptest.Serve(os.Stdin, os.Stdout)
		return
	}
	os.Exit(m.Run())
}

func TestClient(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
	diags := make(chan []Diagnostic, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, Options{Cmd: cmd, RootURI: URI(t.TempDir()), Handlers: Handlers{
		Diagnostics: func(uri string, items []Diagnostic) { diags <- items },
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.Hover() || !c.Definition() || !c.Completion() || !c.Formatting() {
		t.Fatal("capabilities")
	}
	uri := URI("/work/a.fake")
	c.Open(uri, "fake", "good\nso bad\n")
	select {
	case d := <-diags:
		if len(d) != 1 || d[0].Range.Start.Line != 1 || d[0].Range.Start.Character != 3 || d[0].Message != "bad word" {
			t.Fatalf("diagnostics = %+v", d)
		}
	case <-ctx.Done():
		t.Fatal("no diagnostics")
	}
	c.Open(uri, "fake", "all good\n")
	if d := <-diags; len(d) != 0 {
		t.Fatalf("after the change: %+v", d)
	}
	if h, err := c.HoverAt(ctx, uri, Position{0, 1}); err != nil || h.Markdown != "**hello**" {
		t.Fatalf("hover = %q, %v", h.Markdown, err)
	}
	if locs, err := c.DefinitionAt(ctx, uri, Position{0, 1}); err != nil || len(locs) != 1 || Path(locs[0].URI) != filepath.FromSlash("/tmp/def.fake") || locs[0].Range.Start.Line != 3 {
		t.Fatalf("definition = %+v, %v", locs, err)
	}
	if items, err := c.CompletionAt(ctx, uri, Position{0, 1}); err != nil || len(items) != 1 || items[0].InsertText != "println()" {
		t.Fatalf("completion = %+v, %v", items, err)
	}
	if edits, err := c.Format(ctx, uri, 4, true); err != nil || len(edits) != 1 || edits[0].NewText != "// formatted\n" {
		t.Fatalf("format = %+v, %v", edits, err)
	}
	c.Closed(uri)
	if c.IsOpen(uri) {
		t.Fatal("still open")
	}
}

func TestURIs(t *testing.T) {
	if u := URI("/a b/c.go"); u != "file:///a%20b/c.go" {
		t.Fatalf("URI = %s", u)
	}
	if p := Path("file:///a%20b/c.go"); p != filepath.FromSlash("/a b/c.go") {
		t.Fatalf("Path = %s", p)
	}
	if markupText(json.RawMessage(`[{"language":"go","value":"func f()"},"doc"]`)) != "```go\nfunc f()\n```\n\ndoc" {
		t.Fatal("marked strings")
	}
}

// Hover contents read as Markdown: plaintext escaped, line by line, a
// MarkedString in a language fenced, Markdown as it is.
func TestHoverMarkup(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{`{"kind":"markdown","value":"**b**"}`, "**b**"},
		{`{"kind":"plaintext","value":"*a* 1. b\n  c\n\nd"}`, "\\*a\\* 1\\. b\\\n\u00a0\u00a0c\n\nd"},
		{`"_md_"`, "_md_"},
		{`{"language":"go","value":"func f()"}`, "```go\nfunc f()\n```"},
	} {
		if got := markupText(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("%s: %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestLanguageID: Mayoi's names of languages go to servers as the
// protocol's identifiers, JSX and TSX by the file.
func TestLanguageID(t *testing.T) {
	for _, c := range []struct{ language, uri, want string }{
		{"go", "file:///a/main.go", "go"},
		{"c++", "file:///a/main.cpp", "cpp"},
		{"c", "file:///a/main.h", "c"},
		{"react", "file:///a/App.jsx", "javascriptreact"},
		{"typescript", "file:///a/App.tsx", "typescriptreact"},
		{"typescript", "file:///a/app.ts", "typescript"},
		{"javascript", "file:///a/app.mjs", "javascript"},
	} {
		if got := LanguageID(c.language, c.uri); got != c.want {
			t.Errorf("LanguageID(%q, %q) = %q, want %q", c.language, c.uri, got, c.want)
		}
	}
}

// A server that answers initialize with why it cannot start, and quits at
// once, says why, not that it stopped.
func TestStartRefused(t *testing.T) {
	for range 20 {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := Start(ctx, Options{Cmd: cmd, RootURI: URI(t.TempDir()),
			InitOptions: json.RawMessage(`{"failInitialize": "Could not find a valid TypeScript installation"}`)})
		cancel()
		var refused *ResponseError
		if !errors.As(err, &refused) || refused.Message != "Could not find a valid TypeScript installation" {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestDocumentSyncSkipsUnchangedText(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Start(ctx, Options{Cmd: cmd, RootURI: URI(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uri := URI("/work/sync.fake")
	for _, step := range []struct {
		text    string
		close   bool
		version int
	}{
		{text: "first", version: 1},
		{text: "first", version: 1},
		{text: "second", version: 2},
		{text: "second", version: 2},
		{text: "second", close: true, version: 1},
	} {
		if step.close {
			c.Closed(uri)
		}
		c.Open(uri, "fake", step.text)
		data, err := c.Call(ctx, "test/snapshot", nil)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			Document struct{ TextDocument struct{ Version int } }
		}
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatal(err)
		}
		if got := snapshot.Document.TextDocument.Version; got != step.version {
			t.Fatalf("text %q, close %v: server version %d, want %d", step.text, step.close, got, step.version)
		}
	}
}
