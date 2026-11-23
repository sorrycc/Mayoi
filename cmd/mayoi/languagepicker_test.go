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
				if !a.pluginPick.open || a.pluginPick.placeholder != locales.CmdEditorChangeLanguage() {
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
