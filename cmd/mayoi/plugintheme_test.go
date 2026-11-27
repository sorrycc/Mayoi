package main

import (
	"os"
	"slices"
	"strings"
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

// One theme with a palette for light windows and one for dark, as the
// issue's Solarized: tiny, so that each color tells its file.
const solPlugin = `{
  "name": "sol",
  "contributes": {
    "themes": [{"id": "sol.theme", "label": "Sol", "path": "light.json", "darkPath": "dark.json"},
      {"id": "sol.broken", "label": "Broken", "path": "light.json", "darkPath": "missing.json"}]
  }
}`

// The light file says it is dark, and the dark one light: the file read
// tells the darkness.
const (
	solLight = `{"type": "dark", "colors": {"editor.background": "#fdf6e3", "terminal.ansiRed": "#dc322f"}}`
	solDark  = `{"base": "light", "colors": {"editor": "#002b36"}, "ansi": ["#073642", "#cb4b16"]}`
)

func TestPluginThemeLightAndDark(t *testing.T) {
	hs := newHarness(t, false)
	withPlugins(hs, map[string]map[string]string{
		"sol":   {"plugin.json": solPlugin, "light.json": solLight, "dark.json": solDark},
		"night": {"plugin.json": nightPlugin, "night.json": nightTheme, "paper.json": paperTheme},
	})
	light, dark := paletteFor("sol.theme", false), paletteFor("sol.theme", true)
	if light.dark || light.editor != ui.Hex("#fdf6e3") || light.ansi[1] != ui.Hex("#dc322f") {
		t.Fatalf("light palette: dark=%v editor=%v", light.dark, light.editor)
	}
	if !dark.dark || dark.editor != ui.Hex("#002b36") || dark.ansi[1] != ui.Hex("#cb4b16") || dark.chrome != darkPal.chrome {
		t.Fatalf("dark palette: dark=%v editor=%v", dark.dark, dark.editor)
	}
	if alwaysDark("sol.theme") || themeSource("sol.theme", "light") != themeSource("", "light") ||
		themeSource("sol.theme", "") != themeSource("", "") {
		t.Fatal("the theme does not follow the appearance")
	}
	n := 0
	for _, c := range allThemes() {
		if strings.HasPrefix(c.id, "sol.theme") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d entries of the theme", n)
	}
	// A file that does not read is Mayoi's own palette there.
	if paletteFor("sol.broken", true) != darkPal || paletteFor("sol.broken", false).editor != light.editor {
		t.Fatal("a missing dark file")
	}
	// A theme of one file stays as it was: dark always, or light.
	if !alwaysDark("night.dark") || !paletteFor("night.dark", false).dark || alwaysDark("night.paper") || paletteFor("night.paper", true).dark {
		t.Fatal("themes of one file")
	}

	// Chosen once, the appearance picks the palette of the editor and the
	// terminals, the theme kept.
	a := hs.a
	a.run("view.theme.sol.theme")
	hs.settle()
	for _, tc := range []struct {
		appearance string
		want       *palette
	}{{"dark", dark}, {"light", light}, {"", light}} {
		a.setAppearance(tc.appearance)
		hs.settle()
		if a.settings.Theme != "sol.theme" || a.pal.editor != tc.want.editor || a.pal.dark != tc.want.dark {
			t.Errorf("appearance %q: theme %q, editor %v", tc.appearance, a.settings.Theme, a.pal.editor)
		}
	}
	lt, dt := a.termThemes()
	if lt.Palette != light.ansi || dt.Palette != dark.ansi || lt.Background != light.panel || dt.Background != dark.panel {
		t.Error("the terminals' colors are not the theme's")
	}
	if a.termTheme != "sol.theme" {
		t.Errorf("the terminals have %q's colors", a.termTheme)
	}

	// Follow System, in a window the system makes dark: the system's
	// appearance is the harness's, not the native app's.
	hd := newHarness(t, true)
	withPlugins(hd, map[string]map[string]string{"sol": {"plugin.json": solPlugin, "light.json": solLight, "dark.json": solDark}})
	hd.a.run("view.theme.sol.theme")
	hd.settle()
	if hd.a.settings.Appearance != "" || hd.a.pal.editor != dark.editor || !hd.a.pal.dark {
		t.Errorf("following a dark system: appearance %q, editor %v", hd.a.settings.Appearance, hd.a.pal.editor)
	}
}
