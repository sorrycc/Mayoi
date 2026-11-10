package lsp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise mapping at the actual wire boundary, including values that must
// stay opaque, rather than only testing the mapper in isolation.
func TestMappedClientPreservesContent(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	init := json.RawMessage(`{"uri":"file:///work/literal","token":9007199254740993,"hoverContents":{"kind":"plaintext","value":"file:///tmp/literal"}}`)
	config := map[string]any{"uri": "file:///work/literal", "token": json.Number("9007199254740993")}
	c, err := Start(ctx, Options{Cmd: cmd, RootURI: "file:///work", InitOptions: init, Configuration: config,
		PathMappings: []PathMapping{{"file:///work", "file:///tmp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	text := "file:///work/source literal\n你好\n"
	c.Open("file:///work/A.fake", "fake", text)
	raw, err := c.Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Initialize struct {
			RootURI string            `json:"rootUri"`
			Options json.RawMessage   `json:"initializationOptions"`
			Folders []WorkspaceFolder `json:"workspaceFolders"`
		} `json:"initialize"`
		Document struct {
			TextDocument struct{ URI, Text string } `json:"textDocument"`
		} `json:"document"`
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Initialize.RootURI != "file:///tmp" || snapshot.Initialize.Folders[0].URI != "file:///tmp" {
		t.Fatalf("initialize: %s", raw)
	}
	if normalizedJSON(t, snapshot.Initialize.Options) != normalizedJSON(t, init) {
		t.Fatalf("initializationOptions changed: %s", raw)
	}
	if snapshot.Document.TextDocument.URI != "file:///tmp/A.fake" || snapshot.Document.TextDocument.Text != text {
		t.Fatalf("document: %s", raw)
	}
	hover, err := c.HoverAt(ctx, "file:///work/A.fake", Position{})
	if err != nil || hover != "file:///tmp/literal" {
		t.Fatalf("hover %q: %v", hover, err)
	}
	locations, err := c.DefinitionAt(ctx, "file:///work/A.fake", Position{})
	if err != nil || len(locations) != 1 || locations[0].URI != "file:///work/def.fake" {
		t.Fatalf("definition %+v: %v", locations, err)
	}
	// A timed-out response cancels just that request, and later requests work.
	slow, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	_, err = c.Call(slow, "test/noReply", nil)
	stop()
	if err != context.DeadlineExceeded {
		t.Fatalf("timeout: %v", err)
	}
	if _, err = c.HoverAt(ctx, "file:///work/A.fake", Position{}); err != nil {
		t.Fatalf("connection after timeout: %v", err)
	}
}

func TestConfigurationAndFoldersOnWire(t *testing.T) {
	var out captureWriter
	c := &Client{in: &out, configuration: map[string]any{"uri": "file:///work/literal", "token": json.Number("9007199254740993")},
		folders: []WorkspaceFolder{{"file:///work", "work"}}, toServer: []PathMapping{{"file:///work", "file:///tmp"}}}
	c.answer(json.RawMessage(`1`), "workspace/configuration", json.RawMessage(`{"items":[{}]}`))
	if !strings.Contains(out.String(), `"uri":"file:///work/literal"`) || !strings.Contains(out.String(), `9007199254740993`) {
		t.Fatalf("configuration changed: %s", out.String())
	}
	out.Reset()
	c.answer(json.RawMessage(`2`), "workspace/workspaceFolders", nil)
	if !strings.Contains(out.String(), `"uri":"file:///tmp"`) {
		t.Fatalf("folders: %s", out.String())
	}
}
