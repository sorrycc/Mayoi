package lsp

import (
	"context"
	"encoding/json"
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
	if h, err := c.HoverAt(ctx, uri, Position{0, 1}); err != nil || h != "**hello**" {
		t.Fatalf("hover = %q, %v", h, err)
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
