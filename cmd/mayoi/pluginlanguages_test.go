package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/highlight"
)

const exampleGrammar = `<lexer><config><name>Example</name><alias>json</alias></config><rules><state name="root"><rule pattern="word"><token type="Keyword"/></rule></state></rules></lexer>`

func languagePluginFiles() map[string]map[string]string {
	return map[string]map[string]string{"syntax": {
		"plugin.json": `{"name":"syntax","main":"main.ts","contributes":{"commands":[{"id":"syntax.capture","title":"Capture"},{"id":"syntax.set","title":"Set"},{"id":"syntax.output","title":"Output"},{"id":"syntax.metadata","title":"Metadata"}],"languages":[{"id":"example","extensions":[".example"],"lexerFile":"grammar.xml"}]}}`,
		"grammar.xml": exampleGrammar,
		"main.ts": `import {commands,editor,events,window} from "mayoi";
let saved, metadata;
commands.register("syntax.capture",async()=>{saved=(await editor.active()).document;});
commands.register("syntax.set",arg=>saved.setLanguage(arg));
commands.register("syntax.output",()=>window.openText("word",{language:"example",title:"Example"}));
commands.register("syntax.metadata",()=>metadata);
events.on("editor.metadata",({document})=>{metadata=document.language;});`,
	}}
}

func TestPluginSetLanguagePreservesDocument(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	d := a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, "word"})
	h := withPlugins(hs, languagePluginFiles())
	pluginRun(t, hs, h, "syntax.capture", nil, nil)
	version, saved, dirty := d.buf.Version(), d.saved, d.dirty()
	if got := string(pluginRun(t, hs, h, "syntax.set", "example", nil)); got != "true" {
		t.Fatal(got)
	}
	hs.settle()
	if languageOf(d.document) != "example" || d.buf.String() != "word" || d.buf.Version() != version || d.saved != saved || d.dirty() != dirty {
		t.Fatal("language changed document state")
	}
	if got := string(pluginRun(t, hs, h, "syntax.metadata", nil, nil)); got != `"example"` {
		t.Fatal("missing metadata: " + got)
	}
	if got := highlight.Lex(d.hl.Lexer(), "word")[0].Tokens; len(got) != 1 || got[0].Kind != highlight.Keyword {
		t.Fatal(got)
	}
	d.undo()
	hs.settle()
	if d.buf.String() != "" {
		t.Fatal("language changed undo history")
	}
	if got := string(pluginRun(t, hs, h, "syntax.set", "json", nil)); got != "false" {
		t.Fatal("accepted stale document version: " + got)
	}
	a.setDocumentLanguage(d.document, h.reg.Plugin("syntax"), "example")
	path := hs.path("saved.json")
	a.saveDocAs(d.document, path)
	hs.settle()
	if languageOf(d.document) != "json" || d.dirty() {
		t.Fatal("Save As kept explicit language or became dirty")
	}
}

func TestPluginSetLanguageKeepsSourceWindow(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, true)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	h := withPlugins(hs, languagePluginFiles())
	apps := []*app{a, b}
	h.apps = func() []*app { return apps }
	pluginRun(t, hs, h, "syntax.capture", nil, nil)
	apps = []*app{b, a}
	if got := string(pluginRun(t, hs, h, "syntax.set", "json", nil)); got != "true" {
		t.Fatal(got)
	}
	if languageOf(a.activeEditor().document) != "json" || languageOf(b.activeEditor().document) != "plaintext" {
		t.Fatal("crossed windows")
	}
	ref := a.documentRef(a.activeEditor().document)
	a.closeTab(a.column().tab(), nil, nil)
	hs.settle()
	result := pluginCall(t, hs, h, "document.setLanguage", map[string]any{"ref": ref, "language": "json"})
	if result.err != nil || result.value != false {
		t.Fatalf("closed source: %+v", result)
	}
	a.shutdown()
	result = pluginCall(t, hs, h, "document.setLanguage", map[string]any{"ref": ref, "language": "json"})
	if result.err == nil {
		t.Fatal("closed window silently fell back")
	}
}

func TestPluginLexerReloadAndOutput(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	h := withPlugins(hs, languagePluginFiles())
	path := hs.path("a.example")
	os.WriteFile(path, []byte("word"), 0o644)
	a.openFile(path, true)
	hs.settle()
	source := a.activeEditor().document
	pluginRun(t, hs, h, "syntax.output", nil, nil)
	output := a.activeEditor().document
	if output == source || languageOf(output) != "example" || output.dirty() {
		t.Fatal("wrong output")
	}
	p := h.reg.Plugin("syntax")
	file := filepath.Join(p.Dir, "grammar.xml")
	if err := os.WriteFile(file, []byte(strings.Replace(exampleGrammar, `type="Keyword"`, `type="NameFunction"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	h.reg.Load()
	hs.settle()
	for _, d := range []*document{source, output} {
		got := highlight.Lex(d.hl.Lexer(), "word")[0].Tokens
		if len(got) != 1 || got[0].Kind != highlight.Function {
			t.Fatalf("not reloaded: %v", got)
		}
	}
	old := a.settings.Prefs.Plugins
	a.settings.Prefs.Plugins = map[string]json.RawMessage{"syntax": json.RawMessage(`{"enabled":false}`)}
	h.syncPrefs()
	h.toggled(old, a.settings.Prefs.Plugins)
	hs.settle()
	for _, d := range []*document{source, output} {
		if d.hl.Lexer() != nil {
			t.Fatal("disabled grammar remains")
		}
	}
	old = a.settings.Prefs.Plugins
	a.settings.Prefs.Plugins = map[string]json.RawMessage{"syntax": json.RawMessage(`{"enabled":true}`)}
	h.syncPrefs()
	h.toggled(old, a.settings.Prefs.Plugins)
	hs.settle()
	if len(highlight.Lex(output.hl.Lexer(), "word")[0].Tokens) != 1 {
		t.Fatal("reenabling grammar failed")
	}
}

func TestPluginSetLanguageUnknownAndOldGeneration(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	h := withPlugins(hs, languagePluginFiles())
	pluginRun(t, hs, h, "syntax.capture", nil, nil)
	if got := string(pluginRun(t, hs, h, "syntax.set", "unknown-language", nil)); got != "true" {
		t.Fatal(got)
	}
	if d := a.activeEditor(); languageOf(d.document) != "plaintext" || d.hl.Lexer() != nil {
		t.Fatal("unknown language did not fall back")
	}
	p := h.reg.Plugin("syntax")
	generation := p.Generation()
	h.reg.Reload("syntax")
	hs.settle()
	raw, _ := json.Marshal(map[string]any{"ref": a.documentRef(a.activeEditor().document), "language": "json"})
	done := make(chan pluginReply, 1)
	go func() { v, e := h.Call(p, generation, "document.setLanguage", raw); done <- pluginReply{v, e} }()
	var result pluginReply
	hs.until("old generation", func() bool {
		select {
		case result = <-done:
			return true
		default:
			return false
		}
	})
	if result.err != nil || result.value != false {
		t.Fatalf("old runtime changed language: %+v", result)
	}
}
