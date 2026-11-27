package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/settings"
	"mayoi/locales"
)

func TestLanguagePickerEntrances(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, entrance := range []string{"status", "palette"} {
			t.Run(entrance+"/"+map[bool]string{false: "untitled", true: "saved"}[saved], func(t *testing.T) {
				hs := newHarness(t, false)
				a, tt := hs.a, hs.tt
				if saved {
					a.openFile(hs.path("main.go"), true)
				} else {
					a.newFile()
				}
				hs.settle()
				d := a.activeEditor()
				text, version, dirty := d.buf.String(), d.buf.Version(), d.dirty()
				if entrance == "status" {
					if err := tt.Click(locales.CmdEditorChangeLanguage()); err != nil {
						t.Fatal(err)
					}
				} else {
					a.run("go.commands")
					hs.settle()
					tt.Type("Change Language")
					hs.settle()
					tt.Key(0, ui.KeyEnter)
				}
				hs.settle()
				if !a.pluginPick.open || a.pluginPick.placeholder != locales.LanguagePickPlaceholder() {
					t.Fatal("language picker did not open")
				}
				tt.Type("JSONC")
				hs.settle()
				tt.Key(0, ui.KeyEnter)
				hs.settle()
				if a.pluginPick.open || languageOf(d.document) != "jsonc" || !tt.HasText("JSONC") || highlight.Name(d.hl.Lexer()) != "JSON" {
					t.Fatal("JSONC was not applied or displayed")
				}
				if d.buf.String() != text || d.buf.Version() != version || d.dirty() != dirty {
					t.Fatal("choosing a language edited the document")
				}
			})
		}
	}
}

func TestLanguagePickerCancelAndUndo(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.newFile()
	hs.settle()
	d := a.activeEditor()
	tt.Type(`{"ok": true}`)
	hs.settle()
	a.openLanguagePicker()
	hs.settle()
	tt.Type("JSON")
	hs.settle()
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if languageOf(d.document) != "plaintext" || a.pluginPick.open {
		t.Fatal("cancel changed the language")
	}
	a.openLanguagePicker()
	hs.settle()
	tt.Type("JSON")
	hs.settle()
	if err := tt.Click("JSON"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if languageOf(d.document) != "json" || !d.dirty() {
		t.Fatal("JSON was not applied to the unsaved document")
	}
	d.undo()
	hs.settle()
	if d.buf.String() != "" || languageOf(d.document) != "json" {
		t.Fatal("language selection changed the undo history")
	}
	a.openLanguagePicker()
	hs.settle()
	tt.Type("Plain Text")
	hs.settle()
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if languageOf(d.document) != "plaintext" || !tt.HasText("Plain Text") {
		t.Fatal("cannot return to plain text")
	}
}

func TestLanguagePickerKeepsSourceDocument(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	d := a.activeEditor().document
	a.openLanguagePicker()
	a.newFile()
	other := a.activeEditor().document
	hs.settle()
	hs.tt.Type("JSON")
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if languageOf(d) != "json" || languageOf(other) != "plaintext" {
		t.Fatal("picker changed the active tab instead of its source")
	}
	a.openLanguagePicker()
	a.closeTab(a.activeTab(), nil, nil)
	hs.settle()
	hs.tt.Type("JSONC")
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
	if languageOf(other) != "plaintext" || languageOf(d) != "json" {
		t.Fatal("picker changed a closed document or another tab")
	}
}

func TestLanguagePickerPluginLanguages(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	withPlugins(hs, map[string]map[string]string{"syntax": {
		"plugin.json": `{"name":"syntax","contributes":{"languages":[{"id":"example","lexerFile":"grammar.xml"},{"id":"jsonc","lexer":"JSON"},{"id":"server-only"}]}}`,
		"grammar.xml": exampleGrammar,
	}})
	a.newFile()
	d := a.activeEditor().document
	for _, language := range []string{"example", "JSONC", "server-only"} {
		a.openLanguagePicker()
		hs.settle()
		hs.tt.Type(language)
		hs.settle()
		hs.tt.Key(0, ui.KeyEnter)
		hs.settle()
		if languageOf(d) != strings.ToLower(language) || d.languageOwner != "syntax" {
			t.Fatalf("plugin language %q was not selected", language)
		}
		if language == "JSONC" && !hs.tt.HasText("JSONC") {
			t.Fatal("plugin's JSONC is displayed as JSON")
		}
	}
	if !hs.tt.HasText("server-only") {
		t.Fatal("language without a grammar is shown as plain text")
	}
}

func serverDocumentLanguages(t *testing.T, s *lspServer) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := s.live().Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct{ Languages map[string]string }
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot.Languages
}

func TestLanguagePickerUpdatesLSP(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	manifest, _ := json.Marshal(map[string]any{"name": "fake", "contributes": map[string]any{
		"languageServers": []any{
			map[string]any{"id": "fake.go", "languages": []string{"go"}, "command": os.Args[0], "args": []string{"-test.run=^$"}},
			map[string]any{"id": "fake.json", "languages": []string{"json", "jsonc"}, "command": os.Args[0], "args": []string{"-test.run=^$"}},
		},
	}})
	hs := newHarness(t, false)
	a := hs.a
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	path := hs.path("bad.go")
	if err := os.WriteFile(path, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path, true)
	hs.until("Go diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	d := a.activeEditor().document
	uri := serverURI(path)
	goServer := serverOf(serverKey(a.root, "fake.go"))
	for _, language := range []string{"JSON", "JSONC", "Plain Text"} {
		a.openLanguagePicker()
		hs.settle()
		hs.tt.Type(language)
		hs.settle()
		hs.tt.Key(0, ui.KeyEnter)
		hs.settle()
		if language == "Plain Text" {
			break
		}
		hs.until("JSON server", func() bool { return a.lspRunningFor(d) != nil })
		jsonServer := a.lspRunningFor(d)
		if got := serverDocumentLanguages(t, jsonServer)[uri]; got != strings.ToLower(language) {
			t.Fatalf("server language = %q, want %s", got, language)
		}
		if _, open := serverDocumentLanguages(t, goServer)[uri]; open {
			t.Fatal("old server still has the document open")
		}
	}
	jsonServer := serverOf(serverKey(a.root, "fake.json"))
	if _, open := serverDocumentLanguages(t, jsonServer)[uri]; open {
		t.Fatal("plain text left the document open on the JSON server")
	}
	hs.settle()
	if len(h.diagnosticsOf(path)) != 0 || a.lspRunningFor(d) != nil {
		t.Fatal("plain text retained the old server or diagnostics")
	}
}

func TestLanguageChangeDiscardsPendingLSPAnswer(t *testing.T) {
	for _, kind := range []string{"caret", "pointer"} {
		t.Run(kind, func(t *testing.T) {
			hs := withFakeServer(t)
			a := hs.a
			path := hs.path("late.fake")
			if err := os.WriteFile(path, []byte("late"), 0o644); err != nil {
				t.Fatal(err)
			}
			a.openFile(path, true)
			serverUp(hs)
			d := a.activeEditor()
			hs.place(d, 1, 1)
			if kind == "caret" {
				a.showHover()
			} else {
				hs.tt.Move(restAt(t, hs, d, 1))
				step(hs.tt)
				hs.tt.Advance(hoverDwell)
			}
			holdLate(hs)
			a.setDocumentLanguage(d.document, nil, "json")
			hs.settle()
			releaseLate(hs)
			if a.hover != nil {
				t.Fatal("old language's hover answer was displayed")
			}
		})
	}
}

// pickLanguage chooses a row of the box of languages by what is typed.
func pickLanguage(hs *harness, query string) {
	hs.tt.Type(query)
	hs.settle()
	hs.tt.Key(0, ui.KeyEnter)
	hs.settle()
}

func TestLanguagePickerExtensionlessAutoDetect(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false // the unsaved change has to stay unsaved
	path := hs.path("notes")
	if err := os.WriteFile(path, []byte("{\"a\": 1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path, true)
	hs.settle()
	ed := a.activeEditor()
	d := ed.document
	tt.Type("x")
	hs.key(ui.Shift, ui.KeyRight)
	hs.key(ui.Shift, ui.KeyRight)
	hs.settle()
	if ed.sel[0] == ed.sel[1] {
		t.Fatal("no selection")
	}
	text, version, dirty, sel := d.buf.String(), d.buf.Version(), d.dirty(), ed.sel
	kept := func(when string) {
		t.Helper()
		if d.buf.String() != text || d.buf.Version() != version || d.dirty() != dirty || ed.sel != sel || d.path != path {
			t.Fatalf("%s changed the document", when)
		}
	}
	if err := tt.Click("Plain Text"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	pk := &a.pluginPick
	if !pk.open || pk.items[0].Label != locales.LanguageAutoDetect() || !pk.items[0].current || pk.sel != 0 {
		t.Fatal("the box did not open on Auto Detect")
	}
	if pk.items[0].Description != "Plain Text" {
		t.Fatalf("Auto Detect says %q, want what the file name gives", pk.items[0].Description)
	}
	pickLanguage(hs, "JSON")
	if languageOf(d) != "json" || highlight.Name(d.hl.Lexer()) != "JSON" || !tt.HasText("JSON") {
		t.Fatal("JSON was not applied or shown")
	}
	kept("choosing JSON")
	// A second editor of the document shows its language too.
	a.splitRight()
	hs.settle()
	if other := a.activeEditor(); other == ed || other.document != d || documentLanguageName(other.document) != "JSON" {
		t.Fatal("the split does not show the document's language")
	}
	a.openLanguagePicker()
	hs.settle()
	if it := pk.items[pk.sel]; it.Label != "JSON" || !it.current {
		t.Fatalf("the box opened on %q, not the current language", it.Label)
	}
	pickLanguage(hs, "Auto Detect")
	if d.language != "" || languageOf(d) != "plaintext" || d.hl.Lexer() != nil || !tt.HasText("Plain Text") {
		t.Fatal("Auto Detect did not go back to the file name's language")
	}
	kept("Auto Detect")
	ed.undo()
	hs.settle()
	if d.buf.String() != "{\"a\": 1}\n" {
		t.Fatal("choosing languages changed undo")
	}
}

func TestLanguageAutoDetectFollowsFileName(t *testing.T) {
	hs, ed := editHarness(t, "main.go", "package main\n")
	a := hs.a
	a.openLanguagePicker()
	hs.settle()
	if got := a.pluginPick.items[0].Description; got != "Go" {
		t.Fatalf("Auto Detect says %q, want Go", got)
	}
	pickLanguage(hs, "Plain Text")
	if languageOf(ed.document) != "plaintext" || ed.hl.Lexer() != nil {
		t.Fatal("Plain Text did not override the file name")
	}
	a.openLanguagePicker()
	hs.settle()
	pickLanguage(hs, "Auto Detect")
	if languageOf(ed.document) != "go" || highlight.Name(ed.hl.Lexer()) != "Go" {
		t.Fatal("Auto Detect did not give Go back")
	}
}

func TestLanguagePickerUsesMayoiLexers(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	hs.settle()
	d := a.activeEditor().document
	for _, c := range []struct{ query, file string }{{"Markdown", "a.md"}, {"Vue", "a.vue"}} {
		a.openLanguagePicker()
		hs.settle()
		pickLanguage(hs, c.query)
		if d.hl.Lexer() != highlight.Lexer(c.file) {
			t.Fatalf("%s got another lexer than %s files", c.query, c.file)
		}
	}
}

func TestLanguageRestoredWithTab(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	path := hs.path("notes")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path, true)
	hs.settle()
	ed := a.activeEditor()
	a.openLanguagePicker()
	hs.settle()
	pickLanguage(hs, "JSON")
	var ts settings.Tab
	for _, col := range a.session().Columns {
		for _, tab := range col.Tabs {
			if tab.File == path {
				ts = tab
			}
		}
	}
	if ts.Language != "json" || ts.LanguagePlugin != "" {
		t.Fatalf("session kept language %q of %q", ts.Language, ts.LanguagePlugin)
	}
	a.clearDocumentLanguage(ed.document)
	a.restoreEditor(ed, ts)
	if languageOf(ed.document) != "json" || highlight.Name(ed.hl.Lexer()) != "JSON" {
		t.Fatal("restoring the tab did not restore its language")
	}
	// A language Mayoi no longer knows is left out; one of a plugin not
	// loaded yet waits for it.
	a.clearDocumentLanguage(ed.document)
	a.restoreEditor(ed, settings.Tab{File: path, Language: "nosuchlanguage"})
	if ed.document.language != "" {
		t.Fatal("an unknown language was restored")
	}
	a.restoreEditor(ed, settings.Tab{File: path, Language: "example", LanguagePlugin: "syntax"})
	if languageOf(ed.document) != "example" || ed.document.languageOwner != "syntax" {
		t.Fatal("a plugin's language was not kept for the plugin")
	}
}

func TestLanguageChangeDropsLateDiagnostics(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	server := func(name, id, language string) string {
		m, _ := json.Marshal(map[string]any{"name": name, "contributes": map[string]any{"languageServers": []any{
			map[string]any{"id": id, "languages": []string{language}, "command": os.Args[0], "args": []string{"-test.run=^$"}},
		}}})
		return string(m)
	}
	hs := newHarness(t, false)
	a := hs.a
	h := withPlugins(hs, map[string]map[string]string{
		"fakego":   {"plugin.json": server("fakego", "fakego.server", "go")},
		"fakejson": {"plugin.json": server("fakejson", "fakejson.server", "json")},
	})
	t.Cleanup(stopServers)
	path := hs.path("bad.go")
	if err := os.WriteFile(path, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path, true)
	hs.until("Go diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	goServer := serverOf(serverKey(a.root, "fakego.server"))
	goOwner, jsonOwner := goServer.owner(), "lsp:fakejson.server\x00"+a.root
	a.openLanguagePicker()
	hs.settle()
	pickLanguage(hs, "JSON")
	hs.until("JSON diagnostics", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.diags[path][jsonOwner] != nil && h.diags[path][goOwner] == nil
	})
	// The Go server answers a change it got before the switch.
	uri := serverURI(path)
	goServer.live().Notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 9}, "contentChanges": []any{map[string]any{"text": "bad"}}})
	serverDocumentLanguages(t, goServer) // its publish comes before this answer
	hs.settle()
	h.mu.Lock()
	late := h.diags[path][goOwner]
	h.mu.Unlock()
	if late != nil {
		t.Fatal("the old server's late diagnostics came back")
	}
	// Back to Go, the Go server's diagnostics are its again.
	a.openLanguagePicker()
	hs.settle()
	pickLanguage(hs, "Auto Detect")
	hs.until("Go diagnostics again", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.diags[path][goOwner] != nil && h.diags[path][jsonOwner] == nil
	})
}

func TestClosingTabKeepsDiagnostics(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	manifest, _ := json.Marshal(map[string]any{"name": "fake", "contributes": map[string]any{"languageServers": []any{
		map[string]any{"id": "fake.go", "languages": []string{"go"}, "command": os.Args[0], "args": []string{"-test.run=^$"}},
	}}})
	hs := newHarness(t, false)
	a := hs.a
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	path := hs.path("bad.go")
	if err := os.WriteFile(path, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path, true)
	hs.until("Go diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	a.closeActive()
	hs.settle()
	hs.settle()
	if len(h.diagnosticsOf(path)) != 1 {
		t.Fatal("closing the tab dropped its file's problems")
	}
}
