package main

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/keymap"
	"mayoi/internal/plugin"
	"mayoi/locales"
)

// The themes plugins contribute: a palette in JSON, over one of Mayoi's,
// or a VS Code theme, whose colors and token colors map to Mayoi's.

// themeFile is a theme of a plugin as its file says it, Mayoi's format and
// VS Code's at once: a VS Code theme has "type" and colors with dots, as
// "editor.background", and "tokenColors".
type themeFile struct {
	// Base is the palette the theme changes: "dark" or "light", Mayoi's
	// own, or a theme's id, as "claude".
	Base   string            `json:"base"`
	Dark   *bool             `json:"dark"`
	Mono   bool              `json:"mono"`
	Colors map[string]string `json:"colors"`
	Syntax map[string]string `json:"syntax"`
	Ansi   []string          `json:"ansi"`
	// VS Code's.
	Type        string `json:"type"`
	TokenColors []struct {
		Scope    json.RawMessage `json:"scope"`
		Settings struct {
			Foreground string `json:"foreground"`
		} `json:"settings"`
	} `json:"tokenColors"`
}

// pluginTheme is a theme a plugin contributes.
type pluginTheme struct {
	plugin  *plugin.Plugin
	contrib plugin.ThemeContrib
}

// pluginThemes are the themes of the plugins that are on.
func pluginThemes() []pluginTheme {
	var out []pluginTheme
	for _, p := range plugins.enabled() {
		for _, t := range p.Contributes.Themes {
			out = append(out, pluginTheme{p, t})
		}
	}
	return out
}

// allThemes are Mayoi's themes, then the plugins'.
func allThemes() []themeChoice {
	out := builtinThemes
	for _, t := range pluginThemes() {
		label, by := t.contrib.Label, t.plugin.Title()
		out = append(out, themeChoice{t.contrib.ID, func() string { return label }, func() string { return locales.ThemeAboutPlugin(by) }})
	}
	return out
}

// themeCache keeps the palettes made of the plugins' themes, by id and
// darkness, until the plugins change.
var themeCache = struct {
	sync.Mutex
	gen  uint64
	pals map[string]*palette
}{pals: map[string]*palette{}}

// pluginPalette is the palette of a plugin's theme, in a window that is
// dark or not, nil when no plugin has the theme.
func pluginPalette(id string, dark bool) *palette {
	if plugins == nil || !strings.Contains(id, ".") {
		return nil
	}
	plugins.mu.Lock()
	gen := plugins.gen
	plugins.mu.Unlock()
	key := id
	if dark {
		key += "\x00dark"
	}
	themeCache.Lock()
	if themeCache.gen != gen {
		themeCache.gen, themeCache.pals = gen, map[string]*palette{}
	}
	p, ok := themeCache.pals[key]
	themeCache.Unlock()
	if ok {
		return p
	}
	for _, t := range pluginThemes() {
		if t.contrib.ID == id {
			p = loadPluginTheme(t, dark)
			break
		}
	}
	themeCache.Lock()
	themeCache.pals[key] = p
	themeCache.Unlock()
	return p
}

// pluginThemeDark reports whether a plugin's theme is always dark.
func pluginThemeDark(id string) bool {
	if plugins == nil || !strings.Contains(id, ".") {
		return false
	}
	for _, t := range pluginThemes() {
		if t.contrib.ID == id {
			if t.contrib.Dark {
				return true
			}
			f, err := readThemeFile(t)
			return err == nil && f.isDark(false)
		}
	}
	return false
}

func readThemeFile(t pluginTheme) (*themeFile, error) {
	data, err := fs.ReadFile(t.plugin.FS(), path.Clean(t.contrib.Path))
	if err != nil {
		return nil, err
	}
	var f themeFile
	if err := json.Unmarshal(keymap.StripJSONC(data), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// isDark tells the theme's darkness, as its file says it, or as the
// window is when it does not.
func (f *themeFile) isDark(window bool) bool {
	switch {
	case f.Dark != nil:
		return *f.Dark
	case f.Type != "":
		return f.Type != "light" && f.Type != "hcLight"
	case f.Base == "dark":
		return true
	case f.Base == "light":
		return false
	}
	return window
}

// loadPluginTheme makes the palette of a plugin's theme; a file that does
// not read is Mayoi's own palette, as the log says.
func loadPluginTheme(t pluginTheme, window bool) *palette {
	f, err := readThemeFile(t)
	if err != nil {
		return nil
	}
	dark := f.isDark(window) || t.contrib.Dark
	var base *palette
	switch f.Base {
	case "", "dark", "light":
		base = darkPal
		if !dark {
			base = lightPal
		}
	default:
		if f.Base == t.contrib.ID {
			return nil
		}
		base = paletteFor(f.Base, dark)
	}
	p := *base
	p.dark = dark
	if f.Mono {
		p.mono = true
	}
	for k, v := range f.Colors {
		c, ok := parseThemeColor(v)
		if !ok {
			continue
		}
		if field := themeField(&p, k); field != nil {
			*field = c
		} else if fields := vscodeColors[k]; fields != nil {
			for _, name := range fields {
				if field := themeField(&p, name); field != nil {
					*field = c
				}
			}
		}
	}
	for k, v := range f.Syntax {
		if kind, ok := syntaxKinds[strings.ToLower(k)]; ok {
			if c, ok := parseThemeColor(v); ok {
				p.syntax[kind] = c
			}
		}
	}
	for _, tc := range f.TokenColors {
		c, ok := parseThemeColor(tc.Settings.Foreground)
		if !ok {
			continue
		}
		for _, scope := range scopesOf(tc.Scope) {
			if kind, ok := scopeKind(scope); ok {
				p.syntax[kind] = c
			}
		}
	}
	vscodeAnsi(&p, f.Colors)
	for i, v := range f.Ansi {
		if c, ok := parseThemeColor(v); ok && i < len(p.ansi) {
			p.ansi[i] = c
		}
	}
	if fg, ok := f.Colors["editor.foreground"]; ok {
		if c, ok := parseThemeColor(fg); ok {
			p.syntax[highlight.Plain] = c
		}
	}
	p.fillHover, p.fillPressed = p.fill.Mix(white, 0.1), p.fill.Mix(black, 0.15)
	return p.derive()
}

// parseThemeColor reads #rgb, #rrggbb or #rrggbbaa.
func parseThemeColor(s string) (ui.Color, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return ui.Color{}, false
	}
	h := s[1:]
	switch len(h) {
	case 3, 6:
		return ui.Hex(s), true
	case 8:
		c := ui.Hex("#" + h[:6])
		var a int
		for _, r := range h[6:] {
			a <<= 4
			switch {
			case r >= '0' && r <= '9':
				a |= int(r - '0')
			case r >= 'a' && r <= 'f':
				a |= int(r-'a') + 10
			case r >= 'A' && r <= 'F':
				a |= int(r-'A') + 10
			}
		}
		return c.Alpha(float32(a) / 255), true
	}
	return ui.Color{}, false
}

// themeField is the color of a palette a theme file names, as "editor",
// nil for a name it does not have.
func themeField(p *palette, name string) *ui.Color {
	switch name {
	case "editor":
		return &p.editor
	case "chrome":
		return &p.chrome
	case "panel":
		return &p.panel
	case "border":
		return &p.border
	case "hover":
		return &p.hover
	case "active":
		return &p.active
	case "text":
		return &p.text
	case "muted":
		return &p.muted
	case "faint":
		return &p.faint
	case "accent":
		return &p.accent
	case "accentDim":
		return &p.accentDim
	case "fill":
		return &p.fill
	case "fillText":
		return &p.fillText
	case "dangerFill":
		return &p.dangerFill
	case "dangerText":
		return &p.dangerText
	case "surface":
		return &p.surface
	case "surfaceHover":
		return &p.surfaceHover
	case "surfacePressed":
		return &p.surfacePressed
	case "scrollbar":
		return &p.scrollbar
	case "inverse":
		return &p.inverse
	case "inverseText":
		return &p.inverseText
	case "line":
		return &p.line
	case "selection":
		return &p.selection
	case "gutter":
		return &p.gutter
	case "gutterNow":
		return &p.gutterNow
	case "status":
		return &p.status
	case "statusFg":
		return &p.statusFg
	case "modified":
		return &p.modified
	case "danger":
		return &p.danger
	case "match":
		return &p.match
	case "matchNow":
		return &p.matchNow
	case "matchRing":
		return &p.matchRing
	case "guide":
		return &p.guide
	case "guideNow":
		return &p.guideNow
	case "bracket":
		return &p.bracket
	case "word":
		return &p.word
	case "shadow":
		return &p.shadow
	case "added":
		return &p.added
	case "changed":
		return &p.changed
	case "removed":
		return &p.removed
	case "strip":
		return &p.strip
	}
	return nil
}

// vscodeColors are the colors of a VS Code theme Mayoi takes, and the
// colors of its palette each sets.
var vscodeColors = map[string][]string{
	"editor.background":                       {"editor"},
	"sideBar.background":                      {"chrome", "strip"},
	"editorGroupHeader.tabsBackground":        {"strip"},
	"panel.background":                        {"panel"},
	"editorGroup.border":                      {"border"},
	"panel.border":                            {"border"},
	"sideBar.border":                          {"border"},
	"list.hoverBackground":                    {"hover"},
	"list.activeSelectionBackground":          {"active"},
	"list.inactiveSelectionBackground":        {"active"},
	"foreground":                              {"text"},
	"editor.foreground":                       {"text"},
	"descriptionForeground":                   {"muted"},
	"disabledForeground":                      {"faint"},
	"focusBorder":                             {"accent"},
	"textLink.foreground":                     {"accent"},
	"button.background":                       {"fill"},
	"button.foreground":                       {"fillText"},
	"input.background":                        {"surface"},
	"dropdown.background":                     {"surface"},
	"scrollbarSlider.background":              {"scrollbar"},
	"editorWidget.background":                 {"inverse"},
	"editorWidget.foreground":                 {"inverseText"},
	"editor.lineHighlightBackground":          {"line"},
	"editor.selectionBackground":              {"selection"},
	"editorLineNumber.foreground":             {"gutter"},
	"editorLineNumber.activeForeground":       {"gutterNow"},
	"statusBar.background":                    {"status"},
	"statusBar.foreground":                    {"statusFg"},
	"editorWarning.foreground":                {"modified"},
	"errorForeground":                         {"danger"},
	"editorError.foreground":                  {"danger"},
	"editor.findMatchHighlightBackground":     {"match"},
	"editor.findMatchBackground":              {"matchNow"},
	"editor.findMatchBorder":                  {"matchRing"},
	"editorIndentGuide.background":            {"guide"},
	"editorIndentGuide.background1":           {"guide"},
	"editorIndentGuide.activeBackground":      {"guideNow"},
	"editorIndentGuide.activeBackground1":     {"guideNow"},
	"editorBracketMatch.border":               {"bracket"},
	"editor.wordHighlightBackground":          {"word"},
	"widget.shadow":                           {"shadow"},
	"editorGutter.addedBackground":            {"added"},
	"editorGutter.modifiedBackground":         {"changed"},
	"editorGutter.deletedBackground":          {"removed"},
	"gitDecoration.addedResourceForeground":   {"added"},
	"gitDecoration.deletedResourceForeground": {"removed"},
}

// syntaxKinds are the kinds of tokens a theme file's syntax names.
var syntaxKinds = map[string]highlight.Kind{
	"plain": highlight.Plain, "keyword": highlight.Keyword, "type": highlight.Type, "function": highlight.Function,
	"string": highlight.String, "number": highlight.Number, "comment": highlight.Comment, "operator": highlight.Operator,
	"punctuation": highlight.Punctuation, "constant": highlight.Constant, "builtin": highlight.Builtin, "tag": highlight.Tag,
	"attribute": highlight.Attribute, "variable": highlight.Variable, "heading": highlight.Heading,
	"deleted": highlight.Deleted, "inserted": highlight.Inserted,
}

// scopesOf reads a token color's scope: a string, with scopes separated by
// commas, or a list of them.
func scopesOf(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.Split(s, ",")
	}
	var list []string
	json.Unmarshal(raw, &list)
	return list
}

// scopeKind is the kind of token of a TextMate scope, by its start, the
// most particular first.
func scopeKind(scope string) (highlight.Kind, bool) {
	scope = strings.TrimSpace(scope)
	if strings.Contains(scope, " ") {
		return 0, false // a scope within another: too particular
	}
	for _, m := range []struct {
		prefix string
		kind   highlight.Kind
	}{
		{"comment", highlight.Comment},
		{"string", highlight.String},
		{"constant.numeric", highlight.Number},
		{"constant.language", highlight.Constant},
		{"constant.character", highlight.String},
		{"constant", highlight.Constant},
		{"keyword.operator", highlight.Operator},
		{"keyword", highlight.Keyword},
		{"storage.type", highlight.Type},
		{"storage", highlight.Keyword},
		{"entity.name.function", highlight.Function},
		{"support.function", highlight.Builtin},
		{"meta.function-call", highlight.Function},
		{"entity.name.type", highlight.Type},
		{"entity.name.class", highlight.Type},
		{"support.type", highlight.Type},
		{"support.class", highlight.Type},
		{"entity.name.tag", highlight.Tag},
		{"entity.other.attribute-name", highlight.Attribute},
		{"variable.language", highlight.Builtin},
		{"variable", highlight.Variable},
		{"support.constant", highlight.Builtin},
		{"punctuation", highlight.Punctuation},
		{"markup.heading", highlight.Heading},
		{"markup.deleted", highlight.Deleted},
		{"markup.inserted", highlight.Inserted},
	} {
		if scope == m.prefix || strings.HasPrefix(scope, m.prefix+".") {
			return m.kind, true
		}
	}
	return 0, false
}

// ansiNames are the terminal colors of a VS Code theme, in Mayoi's order.
var ansiNames = []string{
	"terminal.ansiBlack", "terminal.ansiRed", "terminal.ansiGreen", "terminal.ansiYellow",
	"terminal.ansiBlue", "terminal.ansiMagenta", "terminal.ansiCyan", "terminal.ansiWhite",
	"terminal.ansiBrightBlack", "terminal.ansiBrightRed", "terminal.ansiBrightGreen", "terminal.ansiBrightYellow",
	"terminal.ansiBrightBlue", "terminal.ansiBrightMagenta", "terminal.ansiBrightCyan", "terminal.ansiBrightWhite",
}

// vscodeAnsi fills a palette's terminal colors from a VS Code theme's.
func vscodeAnsi(p *palette, colors map[string]string) {
	for i, name := range ansiNames {
		if c, ok := parseThemeColor(colors[name]); ok {
			p.ansi[i] = c
		}
	}
}
