package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

func TestPasteDetectsLanguage(t *testing.T) {
	for _, sample := range []struct{ language, text string }{
		{"json", `{"name":"Mayoi","ok":true}`},
		{"json", " [1, 2, 3] \n"},
		{"jsonc", "{\n// settings\n\"ok\": true,\n}"},
		{"go", "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"hello\") }\n"},
		{"bash", "#!/bin/bash\necho hello\n"},
	} {
		for _, name := range []string{"", "no-extension", "file.unknown"} {
			t.Run(sample.language+"/"+name, func(t *testing.T) {
				var hs *harness
				var d *editor
				if name == "" {
					hs = newHarness(t, false)
					hs.a.newFile()
					hs.settle()
					d = hs.a.activeEditor()
				} else {
					hs, d = editHarness(t, name, "")
				}
				version, saved := d.buf.Version(), d.saved
				hs.tt.SetClipboard(sample.text)
				if name == "" {
					hs.key(ui.Cmd, ui.KeyV)
				} else {
					hs.command("paste")
				}
				if got := languageOf(d.document); got != sample.language {
					t.Fatalf("detected %q, want %q", got, sample.language)
				}
				if d.buf.String() != sample.text || d.buf.Version() != version+1 || d.saved != saved || !d.dirty() {
					t.Fatal("detection changed the paste or saved state")
				}
				if d.hl.Lexer() == nil || !hs.tt.HasText(documentLanguageName(d.document)) {
					t.Fatal("detected language is not highlighted or displayed")
				}
				hs.key(ui.Cmd, ui.KeyZ)
				if d.buf.String() != "" {
					t.Fatal("paste is not one step of undo")
				}
				if languageOf(d.document) != "plaintext" || d.language != "" {
					t.Fatal("undo kept an inferred language as a manual choice")
				}
				hs.tt.SetClipboard("package main\n")
				hs.key(ui.Cmd, ui.KeyV)
				if languageOf(d.document) != "go" {
					t.Fatal("paste after undo did not detect a new language")
				}
			})
		}
	}
}

func TestPastePreservesLanguage(t *testing.T) {
	for _, tc := range []struct{ name, initial, manual, want string }{
		{"file.go", "", "", "go"},
		{"file.txt", "", "", "plaintext"},
		{"Makefile", "", "", "makefile"},
		{"file.unknown", "existing content", "", "plaintext"},
		{"whitespace.unknown", " \n", "", "plaintext"},
		{"manual.unknown", "", "python", "python"},
		{"plain.unknown", "", "plaintext", "plaintext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs, d := editHarness(t, tc.name, tc.initial)
			if tc.manual != "" {
				hs.a.setDocumentLanguage(d.document, nil, tc.manual)
			}
			hs.key(ui.Cmd, ui.KeyA)
			hs.tt.SetClipboard(`{"ok":true}`)
			hs.command("pasteAndMatchStyle")
			if got := languageOf(d.document); got != tc.want {
				t.Fatalf("language changed to %q, want %q", got, tc.want)
			}
			if d.buf.String() != `{"ok":true}` {
				t.Fatal("paste did not replace the selection")
			}
		})
	}
}

func TestPastePreservesPluginLanguage(t *testing.T) {
	hs, d := editHarness(t, "source.custom", "")
	withPlugins(hs, map[string]map[string]string{"custom": {
		"plugin.json": `{"name":"custom","contributes":{"languages":[{"id":"custom","extensions":[".custom"]}]}}`,
	}})
	hs.tt.SetClipboard(`{"ok":true}`)
	hs.key(ui.Cmd, ui.KeyV)
	if languageOf(d.document) != "custom" || d.hl.Lexer() != nil {
		t.Fatal("paste replaced a plugin language that has no grammar")
	}
}

func TestPasteDetectionIgnoresTypingAndUnknownText(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	hs.settle()
	d := a.activeEditor()
	hs.tt.Type(`{"ok":true}`)
	hs.settle()
	if languageOf(d.document) != "plaintext" {
		t.Fatal("typing triggered paste detection")
	}
	hs.key(ui.Cmd, ui.KeyA)
	hs.command("delete")
	for _, text := range []string{"", " \n", "hello world", `{"unfinished":`, "42", "true", "false", "null", `"hello"`, "// comment\n42"} {
		hs.tt.SetClipboard(text)
		hs.key(ui.Cmd, ui.KeyV)
		if languageOf(d.document) != "plaintext" || d.language != "" {
			t.Fatalf("ambiguous text %q selected a language", text)
		}
		hs.key(ui.Cmd, ui.KeyA)
		hs.command("delete")
	}
}

func TestPasteKeepsFirstDetectedLanguage(t *testing.T) {
	hs := newHarness(t, false)
	hs.a.newFile()
	hs.settle()
	d := hs.a.activeEditor()
	hs.tt.SetClipboard(`{"ok":true}`)
	hs.key(ui.Cmd, ui.KeyV)
	hs.tt.SetClipboard("package main\n")
	hs.key(ui.Cmd, ui.KeyV)
	if languageOf(d.document) != "json" {
		t.Fatal("a later paste redetected a nonempty document")
	}
	hs.key(ui.Cmd, ui.KeyA)
	hs.command("delete")
	hs.key(ui.Cmd, ui.KeyV)
	if languageOf(d.document) != "go" {
		t.Fatal("paste into the cleared document did not detect its new language")
	}
}

func TestVimPasteDetectsLanguage(t *testing.T) {
	hs, d := vimHarness(t, "new.unknown", "")
	hs.tt.SetClipboard(`{"ok":true}`)
	hs.vim("p")
	if languageOf(d.document) != "json" || highlight.Name(d.hl.Lexer()) != "JSON" {
		t.Fatal("Vim paste did not detect JSON")
	}
	expectText(t, d, `{"ok":true}`)
	hs.vim("u")
	expectText(t, d, "")
	if languageOf(d.document) != "plaintext" {
		t.Fatal("Vim undo kept the inferred language")
	}
}

func TestPasteManualLanguageSurvivesUndo(t *testing.T) {
	hs, d := editHarness(t, "new.unknown", "")
	hs.tt.SetClipboard(`{"ok":true}`)
	hs.key(ui.Cmd, ui.KeyV)
	hs.a.chooseLanguage(d.document, languageChoice{id: "json"})
	hs.key(ui.Cmd, ui.KeyZ)
	if d.buf.String() != "" || languageOf(d.document) != "json" || d.pastedLanguage {
		t.Fatal("undo discarded the language explicitly chosen after detection")
	}
}

func TestPasteDetectionUpdatesLSP(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	manifest, _ := json.Marshal(map[string]any{"name": "fake", "contributes": map[string]any{
		"languageServers": []any{map[string]any{
			"id": "fake.json", "languages": []string{"json"}, "command": os.Args[0], "args": []string{"-test.run=^$"},
		}},
	}})
	hs, d := editHarness(t, "data.unknown", "")
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	if hs.a.lspRunningFor(d.document) != nil {
		t.Fatal("unrecognized file started a language server")
	}
	hs.tt.SetClipboard(`{"value":"bad"}`)
	hs.key(ui.Cmd, ui.KeyV)
	hs.until("detected language's diagnostics", func() bool { return len(h.diagnosticsOf(d.path)) == 1 })
	s := hs.a.lspRunningFor(d.document)
	if s == nil || serverDocumentLanguages(t, s)[serverURI(d.path)] != "json" {
		t.Fatal("detected language was not sent to the server")
	}
}
