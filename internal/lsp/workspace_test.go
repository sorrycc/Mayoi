package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPathMappings(t *testing.T) {
	m := []PathMapping{{URI("/工作 区"), URI("/real")}, {URI("/工作 区/repo"), URI("/other/repo")}}
	for _, tc := range []struct {
		value, want string
		server      bool
	}{
		{URI("/工作 区/repo/A#%.java"), URI("/other/repo/A#%.java"), true},
		{URI("/other/repo/A#%.java"), URI("/工作 区/repo/A#%.java"), false},
		{URI("/realish/A.java"), URI("/realish/A.java"), false},
		{"jdt://contents/a", "jdt://contents/a", false},
	} {
		if got := mappedURI(tc.value, sortedMappings(m, tc.server), tc.server); got != tc.want {
			t.Fatalf("map %s = %s; want %s", tc.value, got, tc.want)
		}
	}
	text := URI("/工作 区/repo/literal")
	data, _ := json.Marshal(map[string]any{"edit": map[string]any{"changes": map[string]any{text: []any{map[string]any{"newText": text}}}}, "text": text})
	var got map[string]any
	json.Unmarshal(mapParams("workspace/applyEdit", data, sortedMappings(m, true), true), &got)
	if got["text"] != text {
		t.Fatal("rewrote source text")
	}
	changes := got["edit"].(map[string]any)["changes"].(map[string]any)
	if _, ok := changes[URI("/other/repo/literal")]; !ok {
		t.Fatalf("URI map key: %s", got)
	}
	if changes[URI("/other/repo/literal")].([]any)[0].(map[string]any)["newText"] != text {
		t.Fatal("rewrote edit text")
	}
}

func TestProtocolMappingPreservesOpaqueJSON(t *testing.T) {
	mappings := sortedMappings([]PathMapping{{"file:///workspace", "file:///canonical"}}, false)
	for _, method := range []string{"textDocument/hover", "textDocument/completion", "textDocument/formatting", "extension/result"} {
		for _, body := range []string{
			`{"contents":{"kind":"plaintext","value":"file:///canonical/literal"}}`,
			`{"contents":["file:///canonical/literal",{"language":"java","value":"file:///canonical/source"}]}`,
			`{"items":[{"label":"file:///canonical/label","data":{"uri":"file:///canonical/opaque","token":9007199254740993}}]}`,
			`[{"newText":"file:///canonical/source"}]`,
		} {
			if got := mapResult(method, json.RawMessage(body), mappings, false); string(got) != body {
				t.Fatalf("%s changed opaque result: %s", method, got)
			}
		}
	}
	for _, method := range []string{"extension/request", "workspace/didChangeConfiguration"} {
		body := json.RawMessage(`{"uri":"file:///canonical/opaque","settings":{"token":9007199254740993}}`)
		if got := mapParams(method, body, mappings, false); !bytes.Equal(got, body) {
			t.Fatalf("%s changed: %s", method, got)
		}
	}
	// A mapped definition changes only location fields, not extension data or
	// numeric tokens, including fields whose names also happen to be "uri".
	for _, body := range []string{
		`{"uri":"file:///canonical/A","data":{"uri":"file:///canonical/opaque","token":9007199254740993}}`,
		`[{"uri":"file:///canonical/A","data":{"uri":"file:///canonical/opaque","token":9007199254740993}}]`,
		`[{"targetUri":"file:///canonical/A","data":{"uri":"file:///canonical/opaque","token":9007199254740993}}]`,
	} {
		want := strings.Replace(body, "file:///canonical/A", "file:///workspace/A", 1)
		got := mapResult("textDocument/definition", json.RawMessage(body), mappings, false)
		if normalizedJSON(t, got) != normalizedJSON(t, []byte(want)) {
			t.Fatalf("definition = %s; want %s", got, want)
		}
	}
}

func TestProtocolURIFields(t *testing.T) {
	mappings := []PathMapping{{"file:///workspace", "file:///canonical"}}
	for _, tc := range []struct{ method, body, want string }{
		{"initialize", `{"rootUri":"file:///workspace","workspaceFolders":[{"uri":"file:///workspace/repo"}],"initializationOptions":{"uri":"file:///workspace/literal","token":9007199254740993}}`,
			`{"rootUri":"file:///canonical","workspaceFolders":[{"uri":"file:///canonical/repo"}],"initializationOptions":{"uri":"file:///workspace/literal","token":9007199254740993}}`},
		{"textDocument/didOpen", `{"textDocument":{"uri":"file:///workspace/A","text":"file:///workspace/literal","version":9007199254740993}}`,
			`{"textDocument":{"uri":"file:///canonical/A","text":"file:///workspace/literal","version":9007199254740993}}`},
		{"textDocument/publishDiagnostics", `{"uri":"file:///workspace/A","diagnostics":[{"message":"file:///workspace/literal","data":{"uri":"file:///workspace/opaque","token":9007199254740993},"relatedInformation":[{"location":{"uri":"file:///workspace/B"}}]}]}`,
			`{"uri":"file:///canonical/A","diagnostics":[{"message":"file:///workspace/literal","data":{"uri":"file:///workspace/opaque","token":9007199254740993},"relatedInformation":[{"location":{"uri":"file:///canonical/B"}}]}]}`},
		{"workspace/applyEdit", `{"edit":{"changes":{"file:///workspace/A":[{"newText":"file:///workspace/literal"}]},"documentChanges":[{"textDocument":{"uri":"file:///workspace/B"},"edits":[{"newText":"file:///workspace/literal"}]},{"kind":"rename","oldUri":"file:///workspace/old","newUri":"file:///workspace/new"}]}}`,
			`{"edit":{"changes":{"file:///canonical/A":[{"newText":"file:///workspace/literal"}]},"documentChanges":[{"textDocument":{"uri":"file:///canonical/B"},"edits":[{"newText":"file:///workspace/literal"}]},{"kind":"rename","oldUri":"file:///canonical/old","newUri":"file:///canonical/new"}]}}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			got := mapParams(tc.method, json.RawMessage(tc.body), mappings, true)
			if normalizedJSON(t, got) != normalizedJSON(t, []byte(tc.want)) {
				t.Fatalf("got %s; want %s", got, tc.want)
			}
			var params map[string]any
			decoder := json.NewDecoder(strings.NewReader(tc.body))
			decoder.UseNumber()
			decoder.Decode(&params)
			mapped := mapParameterValues(tc.method, params, mappings)
			goResult, _ := json.Marshal(mapped)
			if normalizedJSON(t, goResult) != normalizedJSON(t, []byte(tc.want)) {
				t.Fatalf("Go parameters: %s; want %s", goResult, tc.want)
			}
			original, _ := json.Marshal(params)
			if normalizedJSON(t, original) != normalizedJSON(t, []byte(tc.body)) {
				t.Fatal("modified caller's parameters")
			}
		})
	}
}

// Compare JSON values without rounding numbers or depending on object order.
func normalizedJSON(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func BenchmarkDocumentURIMapping(b *testing.B) {
	text := strings.Repeat("class A { String value = \"你好\"; }\n", 32768)
	params := map[string]any{"textDocument": map[string]any{"uri": "file:///workspace/A.java", "text": text}}
	mappings := []PathMapping{{"file:///workspace", "file:///canonical"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mapParameterValues("textDocument/didOpen", params, mappings)
	}
}

type heldWriter struct {
	bytes.Buffer
	entered, release, closed chan struct{}
	first, closeOnce         sync.Once
}

func (w *heldWriter) Write(p []byte) (int, error) {
	w.first.Do(func() {
		close(w.entered)
		select {
		case <-w.release:
		case <-w.closed:
		}
	})
	select {
	case <-w.closed:
		return 0, io.ErrClosedPipe
	default:
		return w.Buffer.Write(p)
	}
}
func (w *heldWriter) Close() error { w.closeOnce.Do(func() { close(w.closed) }); return nil }

func TestQueuedCancellationKeepsTransport(t *testing.T) {
	w := &heldWriter{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	t.Cleanup(func() { w.Close() })
	c := &Client{in: w, waiting: map[int]chan response{}}
	owner := make(chan error, 1)
	go func() { owner <- c.writeContext(context.Background(), map[string]any{"method": "owner"}) }()
	select {
	case <-w.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first writer did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	queued := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "queued", nil); queued <- err }()
	select {
	case err := <-queued:
		if err != context.DeadlineExceeded {
			t.Fatalf("queued call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued call did not observe its deadline")
	}
	select {
	case <-w.closed:
		t.Fatal("queued cancellation closed the shared transport")
	default:
	}
	close(w.release)
	if err := <-owner; err != nil {
		t.Fatalf("owner failed: %v", err)
	}
	if err := c.Notify("next", nil); err != nil {
		t.Fatalf("reusing connection: %v", err)
	}
	if len(c.waiting) != 0 {
		t.Fatal("cancelled request leaked")
	}
}

type captureWriter struct{ bytes.Buffer }

func (w *captureWriter) Close() error { return nil }

func TestWorkspaceRequests(t *testing.T) {
	var out captureWriter
	c := &Client{in: &out, configuration: map[string]any{"java": map[string]any{"autobuild": map[string]any{"enabled": false}}}, folders: []WorkspaceFolder{{URI("/repo"), "repo"}}}
	c.answer(json.RawMessage(`"server-id"`), "workspace/configuration", json.RawMessage(`{"items":[{"section":"java.autobuild"},{"section":"missing"},{"section":"java.autobuild.enabled"}]}`))
	readResult := func() string {
		r := bufio.NewReader(bytes.NewReader(out.Bytes()))
		var n int
		line, _ := r.ReadString('\n')
		fmt.Sscanf(line, "Content-Length: %d", &n)
		r.ReadString('\n')
		data := make([]byte, n)
		io.ReadFull(r, data)
		var m message
		json.Unmarshal(data, &m)
		return string(m.Result)
	}
	if got := readResult(); got != `[{"enabled":false},null,false]` {
		t.Fatal(got)
	}
	out.Reset()
	c.answer(json.RawMessage(`1`), "workspace/workspaceFolders", nil)
	if got := readResult(); !strings.Contains(got, `"uri":"file:///repo"`) {
		t.Fatal(got)
	}
}

func TestBlockedWriteDeadline(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	c := &Client{in: writer, waiting: map[int]chan response{}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := c.Call(ctx, "test", nil); err != context.DeadlineExceeded {
		t.Fatalf("error: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("write exceeded deadline")
	}
	if len(c.waiting) != 0 {
		t.Fatal("leaked pending request")
	}
}

// A server busy for longer than any deadline still gets the whole document.
func TestNotifyWaitsForSlowReader(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	c := &Client{in: writer}
	read := make(chan int64)
	go func() {
		time.Sleep(2500 * time.Millisecond)
		n, _ := io.Copy(io.Discard, reader)
		read <- n
	}()
	text := strings.Repeat("x", 1<<16)
	if err := c.Notify("textDocument/didChange", map[string]any{"text": text}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	writer.Close()
	if n := <-read; n < int64(len(text)) {
		t.Fatalf("server read %d bytes", n)
	}
}

func TestChildExitWithCopiedStdin(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_LSP=1")
	r, w := io.Pipe()
	defer r.Close()
	cmd.Stdin = r
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Start(ctx, Options{Cmd: cmd, Stdin: w, RootURI: URI(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	c.Notify("exit", nil)
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("child exit left os/exec waiting on stdin")
	}
	started := time.Now()
	c.Close()
	if time.Since(started) > time.Second {
		t.Fatal("closing stopped child blocked")
	}
}
