// Package lsptest is a fake language server, for the tests of the LSP
// client and of the app: a test binary runs as one when asked.
package lsptest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Serve answers as a language server of a language whose every word
// "bad" is an error, on in and out, until it is told to exit.
func Serve(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	send := func(m any) {
		data, _ := json.Marshal(m)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
	var initialize, document json.RawMessage
	var options struct {
		HoverContents json.RawMessage `json:"hoverContents"`
	}
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
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				n, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		body := make([]byte, n)
		io.ReadFull(r, body)
		var m struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.Unmarshal(body, &m)
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "result": result}) }
		switch m.Method {
		case "initialize":
			initialize = append(json.RawMessage(nil), m.Params...)
			var p struct {
				InitializationOptions json.RawMessage `json:"initializationOptions"`
			}
			json.Unmarshal(m.Params, &p)
			json.Unmarshal(p.InitializationOptions, &options)
			reply(map[string]any{"capabilities": map[string]any{
				"hoverProvider": true, "definitionProvider": true, "completionProvider": map[string]any{},
				"documentFormattingProvider": true, "textDocumentSync": 1,
			}})
		case "textDocument/didOpen", "textDocument/didChange":
			document = append(json.RawMessage(nil), m.Params...)
			var p struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			json.Unmarshal(m.Params, &p)
			text := p.TextDocument.Text
			if len(p.ContentChanges) > 0 {
				text = p.ContentChanges[0].Text
			}
			var diags []any
			for i, line := range strings.Split(text, "\n") {
				if at := strings.Index(line, "bad"); at >= 0 {
					diags = append(diags, map[string]any{"range": map[string]any{
						"start": map[string]any{"line": i, "character": at}, "end": map[string]any{"line": i, "character": at + 3},
					}, "severity": 1, "message": "bad word", "source": "fake"})
				}
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
				"params": map[string]any{"uri": p.TextDocument.URI, "diagnostics": diags}})
		case "textDocument/hover":
			if len(options.HoverContents) > 0 {
				reply(map[string]any{"contents": options.HoverContents})
			} else {
				reply(map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**hello**"}})
			}
		case "test/snapshot":
			reply(map[string]any{"initialize": initialize, "document": document})
		case "test/noReply":
			// A response timeout must not disconnect an otherwise healthy client.
		case "textDocument/definition":
			reply([]any{map[string]any{"uri": "file:///tmp/def.fake", "range": map[string]any{
				"start": map[string]any{"line": 3, "character": 1}, "end": map[string]any{"line": 3, "character": 4}}}})
		case "textDocument/completion":
			reply(map[string]any{"isIncomplete": false, "items": []any{map[string]any{"label": "println", "insertText": "println()"}}})
		case "textDocument/formatting":
			reply([]any{map[string]any{"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 0}}, "newText": "// formatted\n"}})
		case "shutdown":
			reply(nil)
		case "exit":
			return
		default:
			if m.ID != nil {
				reply(nil)
			}
		}
	}
}
