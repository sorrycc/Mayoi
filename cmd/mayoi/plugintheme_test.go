package main

import (
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

const nightPlugin = `{
  "name": "night",
  "contributes": {
    "themes": [{"id": "night.dark", "label": "Night", "path": "night.json"}, {"id": "night.paper", "label": "Paper", "path": "paper.json"}],
    "languages": [{"id": "d2", "extensions": [".d2"], "lexer": "yaml", "lineComment": "#"}],
    "snippets": [{"language": "d2", "path": "d2.json"}]
  }
}`

// A VS Code theme, as its marketplace has them.
const nightTheme = `{
  // comments, as VS Code's themes have
  "name": "Night", "type": "dark",
  "colors": {"editor.background": "#282a36", "sideBar.background": "#21222c", "editor.selectionBackground": "#44475a80",
    "terminal.ansiRed": "#ff5555", "statusBar.background": "#191a21"},
  "tokenColors": [
    {"scope": "comment", "settings": {"foreground": "#6272a4"}},
    {"scope": ["keyword", "storage.type"], "settings": {"foreground": "#ff79c6"}},
    {"scope": "entity.name.function", "settings": {"foreground": "#50fa7b"}},
  ]
}`

// Mayoi's own format, over Claude's light palette.
const paperTheme = `{"base": "claude", "dark": false, "colors": {"accent": "#c15f3c"}, "syntax": {"string": "#0a7d32"}}`

const d2Snippets = `{
  "Box": {"prefix": ["box", "shape"], "body": ["${1:name}: {", "\tshape: ${2|rectangle,circle|}", "}$0"], "description": "A shape"}
}`

func TestPluginThemesLanguagesSnippets(t *testing.T) {
	hs := newHarness(t, false)
	withPlugins(hs, map[string]map[string]string{"night": {
		"plugin.json": nightPlugin, "night.json": nightTheme, "paper.json": paperTheme, "d2.json": d2Snippets,
	}})
	same := func(a, b ui.Color) bool { return a == b }

	p := paletteFor("night.dark", false)
	if !p.dark || !same(p.editor, ui.Hex("#282a36")) || !same(p.chrome, ui.Hex("#21222c")) || !same(p.status, ui.Hex("#191a21")) {
		t.Fatalf("night palette: dark=%v editor=%v chrome=%v", p.dark, p.editor, p.chrome)
	}
	if !same(p.syntax[highlight.Comment], ui.Hex("#6272a4")) || !same(p.syntax[highlight.Type], ui.Hex("#ff79c6")) ||
		!same(p.syntax[highlight.Function], ui.Hex("#50fa7b")) || !same(p.ansi[1], ui.Hex("#ff5555")) {
		t.Fatal("night's token and terminal colors")
	}
	if !alwaysDark("night.dark") || alwaysDark("night.paper") {
		t.Fatal("always dark")
	}
	paper := paletteFor("night.paper", true)
	if paper.dark || !same(paper.accent, ui.Hex("#c15f3c")) || !same(paper.editor, claudeLightPal.editor) ||
		!same(paper.syntax[highlight.String], ui.Hex("#0a7d32")) {
		t.Fatal("paper palette")
	}
	ids := []string{}
	for _, c := range allThemes() {
		ids = append(ids, c.id)
	}
	if !slices.Contains(ids, "night.dark") || !slices.Contains(ids, "night.paper") {
		t.Fatalf("themes = %v", ids)
	}
	hs.a.run("view.theme.night.dark")
	if hs.a.settings.Theme != "night.dark" {
		t.Fatalf("theme = %q", hs.a.settings.Theme)
	}
	hs.settle() // draws in it

	// A file of the plugin's language.
	path := hs.path("diagram.d2")
	os.WriteFile(path, []byte("a: {\n}\n"), 0o644)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if languageOf(d.document) != "d2" || commentFor(d.document) != "#" || highlight.Name(highlight.Lexer(path)) != "YAML" {
		t.Fatalf("language %q, comment %q, lexer %q", languageOf(d.document), commentFor(d.document), highlight.Name(highlight.Lexer(path)))
	}
	list := snippetsFor("d2")
	if len(list) != 2 || list[0].prefix != "box" || list[1].prefix != "shape" {
		t.Fatalf("snippets = %+v", list)
	}
	text, stop := snippetText(list[0].body, map[string]string{})
	if text != "name: {\n\tshape: rectangle\n}" || text[stop[0]:stop[1]] != "name" {
		t.Fatalf("snippet text %q, stop %v", text, stop)
	}
	// Inserted on an indented line, indented as it is, the placeholder
	// selected.
	os.WriteFile(path, []byte("a: {\n  \n}\n"), 0o644)
	d.buf.Set("a: {\n  \n}\n")
	hs.settle()
	hs.place(d, 7, 7)
	hs.a.queueEdit(d, func(d *editor) { expandSnippet(d, list[0].body, hs.a.snippetVars(d)) })
	hs.settle()
	want := "a: {\n  name: {\n  " + d.indent.unit() + "shape: rectangle\n  }\n}\n"
	if got := d.buf.String(); got != want {
		t.Fatalf("after the snippet: %q, want %q", got, want)
	}
	if s := d.buf.Snapshot(); s.Slice(min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])) != "name" {
		t.Fatalf("selected %v", d.sel)
	}
	if v := snippetVarsText(hs.a, d); v != "diagram" {
		t.Fatalf("TM_FILENAME_BASE = %q", v)
	}
}

func snippetVarsText(a *app, d *editor) string {
	out, _ := snippetText("${TM_FILENAME_BASE}", a.snippetVars(d))
	return out
}
