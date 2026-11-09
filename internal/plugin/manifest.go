// Package plugin runs Mayoi's plugins: folders with a plugin.json and an
// entry module of JavaScript or TypeScript, each in a moejs runtime of its
// own, on a goroutine of its own. The app sees them through a Registry,
// and they see the app through the Host it implements.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mayoi/internal/keymap"
)

// ManifestFile is the name of a plugin's manifest in its folder.
const ManifestFile = "plugin.json"

// Manifest is what plugin.json says of a plugin.
type Manifest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	// Author is who wrote the plugin, as the Plugins page shows it.
	Author string `json:"author,omitempty"`
	// Mayoi is the versions of Mayoi the plugin works with, as ">=0.20.0".
	Mayoi string `json:"mayoi,omitempty"`
	// Main is the entry module, relative to the folder: .ts, .js or .mjs.
	Main string `json:"main,omitempty"`
	// Activation are the events that start the plugin: onCommand:<id>,
	// onLanguage:<lang>, onView:<id>, onPreview:<id>, workspaceContains:<glob>,
	// onStartup, or * for any.
	Activation  []string    `json:"activation,omitempty"`
	Contributes Contributes `json:"contributes"`
}

// Contributes are what a plugin adds to Mayoi, known without running it.
type Contributes struct {
	Commands        []CommandContrib          `json:"commands,omitempty"`
	Keybindings     []KeyContrib              `json:"keybindings,omitempty"`
	Menus           map[string][]MenuContrib  `json:"menus,omitempty"`
	Settings        map[string]SettingContrib `json:"settings,omitempty"`
	StatusItems     []StatusContrib           `json:"statusItems,omitempty"`
	Views           []ViewContrib             `json:"views,omitempty"`
	Previews        []PreviewContrib          `json:"previews,omitempty"`
	Themes          []ThemeContrib            `json:"themes,omitempty"`
	Languages       []LanguageContrib         `json:"languages,omitempty"`
	Snippets        []SnippetContrib          `json:"snippets,omitempty"`
	Formatters      []FormatterContrib        `json:"formatters,omitempty"`
	TerminalLinks   []LinkContrib             `json:"terminalLinks,omitempty"`
	Webviews        []WebviewContrib          `json:"webviews,omitempty"`
	LanguageServers []ServerContrib           `json:"languageServers,omitempty"`
}

// CommandContrib is a command: its id, which starts with the plugin's
// name, and its title, with titles in other languages as "title:zh-Hans".
type CommandContrib struct {
	ID     string            `json:"id"`
	Title  string            `json:"title"`
	Titles map[string]string `json:"-"`
	// Arg names what the command takes from `mayoi run id=arg`, as
	// "<text>"; "" takes none.
	Arg string `json:"arg,omitempty"`
}

func (c *CommandContrib) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	type plain CommandContrib
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*c = CommandContrib(p)
	for k, v := range raw {
		if tag, ok := strings.CutPrefix(k, "title:"); ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				if c.Titles == nil {
					c.Titles = map[string]string{}
				}
				c.Titles[tag] = s
			}
		}
	}
	return nil
}

// TitleIn is the command's title in the language of tag, or its own.
func (c CommandContrib) TitleIn(tag string) string {
	if t, ok := c.Titles[tag]; ok && t != "" {
		return t
	}
	if base, _, ok := strings.Cut(tag, "-"); ok {
		if t, ok := c.Titles[base]; ok && t != "" {
			return t
		}
	}
	return c.Title
}

// KeyContrib binds keys, in keybindings.json's format, to a command; Mac
// is the keys on macOS, when they differ.
type KeyContrib struct {
	Key     string `json:"key"`
	Mac     string `json:"mac,omitempty"`
	Command string `json:"command"`
	When    string `json:"when,omitempty"`
}

// MenuContrib puts a command in a menu: "plugins" (the menu bar's),
// "editor/context", "explorer/context" or "tab/context".
type MenuContrib struct {
	Command string `json:"command"`
	When    string `json:"when,omitempty"`
}

// SettingContrib is a setting of the plugin, kept under
// plugins.<name>.<key> in settings.json.
type SettingContrib struct {
	Type        string          `json:"type"`
	Default     json.RawMessage `json:"default,omitempty"`
	Description string          `json:"description,omitempty"`
	Enum        []string        `json:"enum,omitempty"`
}

// StatusContrib is an item of the status bar, which the plugin fills.
type StatusContrib struct {
	ID    string `json:"id"`
	Align string `json:"align,omitempty"` // "left" or "right", the default
	// Priority orders the items of a side, the highest first.
	Priority int `json:"priority,omitempty"`
}

// ViewContrib is a view the plugin renders: in the sidebar beside the
// Explorer, or in a tab.
type ViewContrib struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Icon     string `json:"icon,omitempty"`
	Location string `json:"location,omitempty"` // "sidebar", the default, or "tab"
}

// PreviewContrib previews the files a glob matches beside their source,
// as the plugin renders them: "svg", "markdown" or "view".
type PreviewContrib struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Pattern string `json:"pattern"`
	Output  string `json:"output"`
}

// ThemeContrib is a theme: a palette in JSON, or a VS Code theme, in the
// plugin's folder.
type ThemeContrib struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
	// Dark tells a theme for dark windows; a theme file may say too.
	Dark bool `json:"dark,omitempty"`
}

// LanguageContrib tells Mayoi of a language: its files, how its comments
// start, and which Chroma lexer highlights it.
type LanguageContrib struct {
	ID         string   `json:"id"`
	Extensions []string `json:"extensions,omitempty"`
	Filenames  []string `json:"filenames,omitempty"`
	Lexer      string   `json:"lexer,omitempty"`
	Comment    string   `json:"lineComment,omitempty"`
	Indent     int      `json:"indent,omitempty"`
}

// SnippetContrib is a file of snippets in VS Code's format for a language,
// or every language for "*".
type SnippetContrib struct {
	Language string `json:"language"`
	Path     string `json:"path"`
}

// FormatterContrib formats the documents of languages.
type FormatterContrib struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	Languages []string `json:"languages"`
}

// LinkContrib turns what a pattern matches in a terminal into a link that
// runs a command with the match.
type LinkContrib struct {
	Pattern string `json:"pattern"`
	Command string `json:"command"`
}

// WebviewContrib is a panel of HTML from the plugin's folder.
type WebviewContrib struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// ServerContrib is a language server, started for its languages.
type ServerContrib struct {
	ID        string          `json:"id"`
	Languages []string        `json:"languages"`
	Command   string          `json:"command"`
	Args      []string        `json:"args,omitempty"`
	Options   json.RawMessage `json:"initializationOptions,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Reserved are the prefixes of Mayoi's own command ids, which no plugin
// may take as its name.
var Reserved = []string{
	"file", "edit", "sel", "go", "view", "git", "review", "remote", "account",
	"editor", "image", "mermaid", "plugins", "script", "mayoi",
}

// ValidName reports whether name may name a plugin.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("plugin name %q must be lowercase letters, digits and hyphens, starting with a letter", name)
	}
	for _, r := range Reserved {
		if name == r {
			return fmt.Errorf("plugin name %q is reserved", name)
		}
	}
	return nil
}

// ParseManifest reads a manifest and checks it.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(keymap.StripJSONC(data), &m); err != nil {
		return nil, fmt.Errorf("%s: %v", ManifestFile, err)
	}
	if err := m.check(); err != nil {
		return nil, fmt.Errorf("%s: %v", ManifestFile, err)
	}
	return &m, nil
}

// ReadManifest reads the manifest of the plugin in dir.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

func (m *Manifest) check() error {
	if err := ValidName(m.Name); err != nil {
		return err
	}
	if m.Main != "" && (filepath.IsAbs(m.Main) || strings.HasPrefix(filepath.Clean(m.Main), "..")) {
		return fmt.Errorf("main %q must be a path inside the plugin's folder", m.Main)
	}
	if m.Mayoi != "" && !versionRangeOK(m.Mayoi) {
		return fmt.Errorf("mayoi %q is not a version range such as \">=0.20.0\"", m.Mayoi)
	}
	own := func(kind, id string) error {
		if !strings.HasPrefix(id, m.Name+".") || len(id) == len(m.Name)+1 {
			return fmt.Errorf("%s %q must start with %q", kind, id, m.Name+".")
		}
		return nil
	}
	c := &m.Contributes
	seen := map[string]bool{}
	for _, cmd := range c.Commands {
		if err := own("command", cmd.ID); err != nil {
			return err
		}
		if cmd.Title == "" {
			return fmt.Errorf("command %q has no title", cmd.ID)
		}
		if seen[cmd.ID] {
			return fmt.Errorf("command %q is declared twice", cmd.ID)
		}
		seen[cmd.ID] = true
	}
	for _, k := range c.Keybindings {
		if k.Key == "" && k.Mac == "" {
			return fmt.Errorf("a keybinding of %q has no key", k.Command)
		}
	}
	for _, s := range c.StatusItems {
		if err := own("status item", s.ID); err != nil {
			return err
		}
	}
	for _, v := range c.Views {
		if err := own("view", v.ID); err != nil {
			return err
		}
	}
	for _, p := range c.Previews {
		if err := own("preview", p.ID); err != nil {
			return err
		}
		switch p.Output {
		case "svg", "markdown", "view":
		default:
			return fmt.Errorf("preview %q: output must be svg, markdown or view", p.ID)
		}
		if p.Pattern == "" {
			return fmt.Errorf("preview %q has no pattern", p.ID)
		}
	}
	for _, f := range c.Formatters {
		if err := own("formatter", f.ID); err != nil {
			return err
		}
	}
	for _, w := range c.Webviews {
		if err := own("webview", w.ID); err != nil {
			return err
		}
	}
	for _, t := range c.Themes {
		if err := own("theme", t.ID); err != nil {
			return err
		}
	}
	for _, s := range c.LanguageServers {
		if err := own("language server", s.ID); err != nil {
			return err
		}
		if s.Command == "" {
			return fmt.Errorf("language server %q has no command", s.ID)
		}
	}
	for _, l := range c.TerminalLinks {
		if _, err := regexp.Compile(l.Pattern); err != nil {
			return fmt.Errorf("terminal link %q: %v", l.Pattern, err)
		}
	}
	for key := range c.Settings {
		if key == "enabled" {
			return errors.New(`setting "enabled" is Mayoi's own`)
		}
	}
	return nil
}

// HasCode reports whether the plugin runs code, rather than only
// declaring themes, languages or snippets.
func (m *Manifest) HasCode() bool { return m.Main != "" }

// versionRangeOK reports whether r is a range of versions this package
// understands: comparisons of versions, separated by spaces.
func versionRangeOK(r string) bool {
	for _, part := range strings.Fields(r) {
		op := strings.TrimLeft(part, "<>=^~")
		if _, ok := parseVersion(op); !ok {
			return false
		}
	}
	return strings.TrimSpace(r) != ""
}

// parseVersion reads major.minor.patch, with the minor and patch optional.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n := 0
		if p == "" {
			return out, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Satisfies reports whether version is in range r; an empty range, or a
// version that does not parse (a development build), satisfies any.
func Satisfies(version, r string) bool {
	v, ok := parseVersion(version)
	if !ok || strings.TrimSpace(r) == "" {
		return true
	}
	for _, part := range strings.Fields(r) {
		op := part[:len(part)-len(strings.TrimLeft(part, "<>=^~"))]
		w, ok := parseVersion(part[len(op):])
		if !ok {
			return false
		}
		c := compareVersions(v, w)
		switch op {
		case ">=":
			ok = c >= 0
		case ">":
			ok = c > 0
		case "<=":
			ok = c <= 0
		case "<":
			ok = c < 0
		case "^":
			ok = c >= 0 && v[0] == w[0] && (w[0] != 0 || v[1] == w[1])
		case "~":
			ok = c >= 0 && v[0] == w[0] && v[1] == w[1]
		default:
			ok = c == 0
		}
		if !ok {
			return false
		}
	}
	return true
}
