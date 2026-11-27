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
// line 3 of /tmp/def.fake. With initializationOptions failInitialize, it
// answers initialize with that error and quits, as a server that cannot
// start says why. The answer of a word "late" waits for
// test/release, and test/held says whether one waits; of hovers and
// completions, a word "latenothing" or "latebroken" waits as well. The
// hover and the completions of a word ending "nothing" are none, and of
// one ending "broken" an error.
func Serve(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	send := func(m any) {
		data, _ := json.Marshal(m)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
	var initialize, document, navigation json.RawMessage
	texts := map[string]string{} // by URI
	languages := map[string]string{}
	var late func() // the answer held back
	var options struct {
		HoverContents  json.RawMessage `json:"hoverContents"`
		FailInitialize string          `json:"failInitialize"`
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
		// answer replies by the word at the place asked of, held back when
		// it starts "late", none or an error by its end.
		answer := func(result any) {
			name := wordAt(texts, m.Params)
			id := *m.ID
			respond := func() {
				switch {
				case strings.HasSuffix(name, "broken"):
					send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "broken"}})
				case strings.HasSuffix(name, "nothing"):
					send(map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
				default:
					send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
				}
			}
			if strings.HasPrefix(name, "late") && late == nil {
				late = respond
				return
			}
			respond()
		}
		switch m.Method {
		case "initialize":
			initialize = append(json.RawMessage(nil), m.Params...)
			var p struct {
				InitializationOptions json.RawMessage `json:"initializationOptions"`
			}
			json.Unmarshal(m.Params, &p)
			json.Unmarshal(p.InitializationOptions, &options)
			if options.FailInitialize != "" {
				send(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "error": map[string]any{"code": -32603, "message": options.FailInitialize}})
				return
			}
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
					URI      string `json:"uri"`
					Text     string `json:"text"`
					Language string `json:"languageId"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			json.Unmarshal(m.Params, &p)
			if m.Method == "textDocument/didOpen" {
				languages[p.TextDocument.URI] = p.TextDocument.Language
			}
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
		case "textDocument/didClose":
			var p struct{ TextDocument struct{ URI string } }
			json.Unmarshal(m.Params, &p)
			delete(texts, p.TextDocument.URI)
			delete(languages, p.TextDocument.URI)
		case "textDocument/hover":
			// "plain", "marked" and "long" answer as their names say, with
			// the range of the word.
			line, s, e, word := wordSpan(texts, m.Params)
			at := map[string]any{"start": map[string]any{"line": line, "character": s}, "end": map[string]any{"line": line, "character": e}}
			switch {
			case len(options.HoverContents) > 0:
				answer(map[string]any{"contents": options.HoverContents})
			case word == "plain":
				answer(map[string]any{"contents": map[string]any{"kind": "plaintext", "value": "*not* bold\nnext line"}, "range": at})
			case word == "marked":
				answer(map[string]any{"contents": []any{map[string]any{"language": "go", "value": "func marked(x int) string"}, "Returns **x**."}, "range": at})
			case word == "long":
				answer(map[string]any{"contents": map[string]any{"kind": "markdown",
					"value": "```go\nfunc long()\n```\n\n" + strings.Repeat("A line of documentation that goes on.\n\n", 60)}, "range": at})
			default:
				answer(map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**hello**"}})
			}
		case "test/snapshot":
			reply(map[string]any{"initialize": initialize, "document": document, "navigation": navigation, "languages": languages})
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
			// References of a word of a document are where it is defined.
			if defs, _ := definitions(texts, m.Params); defs != nil && m.Method == "textDocument/references" {
				reply(defs)
				break
			}
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
			answer(map[string]any{"isIncomplete": false, "items": []any{map[string]any{"label": "println", "insertText": "println()"}}})
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
	name := wordAt(texts, params)
	if name == "nothing" {
		return []any{}, name
	}
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(params, &p)
	lines := strings.Split(texts[p.TextDocument.URI], "\n")
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

// wordAt is the word at the place of a request's params, of the
// document's text.
func wordAt(texts map[string]string, params json.RawMessage) string {
	_, _, _, word := wordSpan(texts, params)
	return word
}

// wordSpan is the word at the place of a request's params, its line and
// where on it it starts and ends.
func wordSpan(texts map[string]string, params json.RawMessage) (line, start, end int, word string) {
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
		return p.Position.Line, 0, 0, ""
	}
	text := lines[p.Position.Line]
	letter := func(r byte) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }
	s, e := min(p.Position.Character, len(text)), min(p.Position.Character, len(text))
	for s > 0 && letter(text[s-1]) {
		s--
	}
	for e < len(text) && letter(text[e]) {
		e++
	}
	return p.Position.Line, s, e, text[s:e]
}
