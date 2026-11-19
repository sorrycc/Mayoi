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
// "bad" is an error, on in and out, until it is told to exit. A word is
// defined by the lines of its document that are "def" and it, and else at
// line 3 of /tmp/def.fake. The answer of a word "late" waits for
// test/release, and test/held says whether one waits.
func Serve(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	send := func(m any) {
		data, _ := json.Marshal(m)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
	var initialize, document, navigation json.RawMessage
	texts := map[string]string{} // by URI
	var late func()              // the answer held back
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
				"typeDefinitionProvider": map[string]any{}, "implementationProvider": true, "referencesProvider": true,
				"executeCommandProvider": map[string]any{"commands": []string{"fake.sourceDefinition"}},
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
			texts[p.TextDocument.URI] = text
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
			reply(map[string]any{"initialize": initialize, "document": document, "navigation": navigation})
		case "test/noReply":
			// A response timeout must not disconnect an otherwise healthy client.
		case "textDocument/definition":
			defs, name := definitions(texts, m.Params)
			if defs == nil {
				defs = []any{map[string]any{"uri": "file:///tmp/def.fake", "range": map[string]any{
					"start": map[string]any{"line": 3, "character": 1}, "end": map[string]any{"line": 3, "character": 4}}}}
			}
			if name == "late" && late == nil {
				late = func() { reply(defs) }
				break
			}
			reply(defs)
		case "textDocument/typeDefinition":
			navigation = m.Params
			reply(json.RawMessage(`[{"targetUri":"file:///tmp/def.fake","targetRange":{"start":{"line":1,"character":0}},
				"targetSelectionRange":{"start":{"line":3,"character":1},"end":{"line":3,"character":4}}}]`))
		case "textDocument/implementation", "textDocument/references", "workspace/executeCommand":
			navigation = m.Params
			reply(json.RawMessage(`[{"uri":"file:///tmp/def.fake","range":{"start":{"line":3,"character":1},"end":{"line":3,"character":4}}}]`))
		case "test/held":
			reply(late != nil)
		case "test/release":
			// The answer held goes before this one's.
			if late != nil {
				late()
				late = nil
			}
			reply(nil)
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

// definitions are where the word at a place follows "def" on a line of
// the document's text, as many times as it does there, and the word: an
// empty list when the word is "nothing", nil when it is defined in none.
func definitions(texts map[string]string, params json.RawMessage) ([]any, string) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}
	json.Unmarshal(params, &p)
	lines := strings.Split(texts[p.TextDocument.URI], "\n")
	if p.Position.Line >= len(lines) {
		return nil, ""
	}
	line := lines[p.Position.Line]
	word := func(r byte) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }
	s, e := min(p.Position.Character, len(line)), min(p.Position.Character, len(line))
	for s > 0 && word(line[s-1]) {
		s--
	}
	for e < len(line) && word(line[e]) {
		e++
	}
	name := line[s:e]
	if name == "nothing" {
		return []any{}, name
	}
	var out []any
	for i, l := range lines {
		rest, ok := strings.CutPrefix(l, "def ")
		if !ok || name == "" {
			continue
		}
		at := 4
		for _, w := range strings.Split(rest, " ") {
			if w == name {
				out = append(out, map[string]any{"uri": p.TextDocument.URI, "range": map[string]any{
					"start": map[string]any{"line": i, "character": at}, "end": map[string]any{"line": i, "character": at + len(name)}}})
			}
			at += len(w) + 1
		}
	}
	return out, name
}
