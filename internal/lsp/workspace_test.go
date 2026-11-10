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
	data, _ := json.Marshal(map[string]any{"changes": map[string]any{text: []any{map[string]any{"newText": text}}}, "text": text})
	var got map[string]any
	json.Unmarshal(mapJSON(data, sortedMappings(m, true), true), &got)
	if got["text"] != text {
		t.Fatal("rewrote source text")
	}
	changes := got["changes"].(map[string]any)
	if _, ok := changes[URI("/other/repo/literal")]; !ok {
		t.Fatalf("URI map key: %s", mapJSON(data, sortedMappings(m, true), true))
	}
	if changes[URI("/other/repo/literal")].([]any)[0].(map[string]any)["newText"] != text {
		t.Fatal("rewrote edit text")
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
