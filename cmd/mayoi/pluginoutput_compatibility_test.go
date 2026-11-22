package main

import (
	"encoding/json"
	"strings"
	"testing"

	"mayoi/internal/highlight"
)

func TestPluginOutputLegacyCompatibility(t *testing.T) {
	for _, tc := range []struct{ language, wantLanguage, wantLexer string }{
		{"", "plaintext", "Plain Text"},
		{"json", "json", "JSON"},
		{"legacy-custom-language", "plaintext", "Plain Text"},
	} {
		label := tc.language
		if label == "" {
			label = "default"
		}
		t.Run(label, func(t *testing.T) {
			hs := newHarness(t, false)
			a := hs.a
			a.newFile()
			h := withPlugins(hs, map[string]map[string]string{"legacy-output": {
				"plugin.json": `{"name":"legacy-output","main":"main.ts","contributes":{"commands":[{"id":"legacy-output.write","title":"Write"}]}}`,
				"main.ts": `import {commands,window} from "mayoi";
commands.register("legacy-output.write", async arg => {
 await window.openText("legacy output", {title:"Legacy output",language:arg || undefined});
 return "done";
});`,
			}})
			got := pluginRun(t, hs, h, "legacy-output.write", tc.language, nil)
			if string(got) != `"done"` || len(a.docs) != 2 || len(a.cols) != 1 {
				t.Fatalf("legacy output changed: result=%s docs=%d columns=%d", got, len(a.docs), len(a.cols))
			}
			d := a.activeEditor().document
			if d.buf.String() != "legacy output" || d.title != "Legacy output" || d.path != "" || d.dirty() {
				t.Fatal("legacy output content, title or saved state changed")
			}
			if languageOf(d) != tc.wantLanguage || highlight.Name(d.hl.Lexer()) != tc.wantLexer {
				t.Fatalf("output language=%q lexer=%q", languageOf(d), highlight.Name(d.hl.Lexer()))
			}
		})
	}
}

func TestPluginOutputUnknownLanguageClearsReusedHighlighting(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	source := a.activeEditor().document
	files := outputProbe("output")
	files["main.ts"] = strings.Replace(files["main.ts"], `language:"plaintext"`, `language:a.language`, 1)
	h := withPlugins(hs, map[string]map[string]string{"output": files})
	var output *document
	for _, tc := range []struct{ language, wantLanguage, wantLexer string }{
		{"json", "json", "JSON"},
		{"legacy-custom-language", "plaintext", "Plain Text"},
	} {
		raw, _ := json.Marshal(map[string]any{"ref": a.documentRef(source), "text": `{"ok":true}`, "language": tc.language})
		if got := pluginRun(t, hs, h, "output.write", string(raw), nil); string(got) != "true" {
			t.Fatalf("output failed: %s", got)
		}
		d := a.activeEditor().document
		if len(a.docs) != 2 || len(a.cols) != 2 || output != nil && output != d {
			t.Fatal("language fallback did not reuse the existing output")
		}
		if languageOf(d) != tc.wantLanguage || highlight.Name(d.hl.Lexer()) != tc.wantLexer {
			t.Fatalf("reused language=%q lexer=%q", languageOf(d), highlight.Name(d.hl.Lexer()))
		}
		if d.buf.String() != `{"ok":true}` || d.dirty() || a.cols[0].tab().ed.document != source {
			t.Fatal("language fallback changed source, output text or saved state")
		}
		output = d
	}
}
