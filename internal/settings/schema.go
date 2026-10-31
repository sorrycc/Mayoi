package settings

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Kind is what a preference's value is, as the settings window edits it.
type Kind int

const (
	KindBool   Kind = iota
	KindEnum        // one of Options
	KindNumber      // from Min to Max, by Step
	KindText        // a line of text
	KindList        // a list of strings
	KindStyles      // the looks of the projects on the rail, by folder
)

// The categories of the preferences, in the order the settings window
// shows them.
const (
	CatAppearance = "appearance"
	CatFonts      = "fonts"
	CatEditor     = "editor"
	CatMarkdown   = "markdown"
	CatTerminal   = "terminal"
	CatDiff       = "diff"
	CatGit        = "git"
	CatVim        = "vim"
	CatGeneral    = "general"
)

// Categories are the categories of the preferences, in order.
var Categories = []string{CatAppearance, CatFonts, CatEditor, CatMarkdown, CatTerminal, CatDiff, CatGit, CatVim, CatGeneral}

// Field is a preference as the settings window shows it: its key in the
// settings' file, its category, and what it may be.
type Field struct {
	Key      string
	Category string
	Kind     Kind
	// Options are the values of a KindEnum, "" first for its default.
	Options []string
	// Min, Max and Step bound a KindNumber.
	Min, Max, Step float64
	// Folder lets a folder's own .mayoi/settings.json choose the
	// preference for the windows of the folder.
	Folder bool
	// Memory is what the windows remember, as which of Source Control's
	// panels is open, rather than a choice: the window leaves it out.
	Memory bool
}

// Schema is every preference, in the order the settings window shows
// them.
var Schema = []Field{
	{Key: "theme", Category: CatAppearance, Kind: KindEnum, Options: []string{"", "matrix", "claude", "qoder", "grokbot", "cursor", "purplegold", "pumpkin", "witchsbrew", "bloodmoon"}},
	{Key: "appearance", Category: CatAppearance, Kind: KindEnum, Options: []string{"", "light", "dark"}},
	{Key: "zoomLevel", Category: CatAppearance, Kind: KindNumber, Min: MinZoomLevel, Max: MaxZoomLevel, Step: 0.5},
	{Key: "sidebarRight", Category: CatAppearance, Kind: KindBool},
	{Key: "panelPosition", Category: CatAppearance, Kind: KindEnum, Options: []string{"", "left", "right"}},
	{Key: "projectTabs", Category: CatAppearance, Kind: KindBool},
	{Key: "projectTabStyle", Category: CatAppearance, Kind: KindStyles},

	{Key: "fontFamily", Category: CatFonts, Kind: KindText},
	{Key: "fontSize", Category: CatFonts, Kind: KindNumber, Min: 9, Max: 28, Step: 1},
	{Key: "fontWeight", Category: CatFonts, Kind: KindNumber, Min: 100, Max: 900, Step: 100},
	{Key: "lineHeight", Category: CatFonts, Kind: KindNumber, Min: 1, Max: 3, Step: 0.05},
	{Key: "fontFeatures", Category: CatFonts, Kind: KindList},

	{Key: "autoSave", Category: CatEditor, Kind: KindBool, Folder: true},
	{Key: "wordWrap", Category: CatEditor, Kind: KindBool, Folder: true},
	{Key: "editorAgentContext", Category: CatEditor, Kind: KindBool},

	{Key: "markdownPreview", Category: CatMarkdown, Kind: KindBool, Folder: true},
	{Key: "previewMermaid", Category: CatMarkdown, Kind: KindBool, Folder: true},
	{Key: "previewMath", Category: CatMarkdown, Kind: KindBool, Folder: true},

	{Key: "terminalFontFamily", Category: CatTerminal, Kind: KindText},
	{Key: "terminalFontSize", Category: CatTerminal, Kind: KindNumber, Min: 0, Max: 28, Step: 1},
	{Key: "terminalCopyOnSelect", Category: CatTerminal, Kind: KindBool, Folder: true},
	{Key: "terminalLocation", Category: CatTerminal, Kind: KindEnum, Options: []string{"", "editor"}},
	{Key: "terminalBackend", Category: CatTerminal, Kind: KindEnum, Options: []string{"", "hc"}},

	{Key: "diffLayout", Category: CatDiff, Kind: KindEnum, Options: []string{"", "split", "inline"}},
	{Key: "diffHighlight", Category: CatDiff, Kind: KindEnum, Options: []string{"", "line", "char"}, Folder: true},
	{Key: "diffWhitespace", Category: CatDiff, Kind: KindEnum, Options: []string{"", "eol", "all", "blank"}, Folder: true},
	{Key: "diffContext", Category: CatDiff, Kind: KindNumber, Min: 0, Max: MaxDiffContext, Step: 1, Folder: true},
	{Key: "diffCrossFiles", Category: CatDiff, Kind: KindEnum, Options: []string{"", "always", "never"}, Folder: true},
	{Key: "diffSyncScroll", Category: CatDiff, Kind: KindBool},
	{Key: "diffAlignedRows", Category: CatDiff, Kind: KindBool},
	{Key: "mergeAutoApply", Category: CatDiff, Kind: KindBool, Folder: true},

	{Key: "blame", Category: CatGit, Kind: KindBool, Folder: true},
	{Key: "scmTree", Category: CatGit, Kind: KindBool},
	{Key: "scmAllBranches", Category: CatGit, Kind: KindBool},
	{Key: "aiModel", Category: CatGit, Kind: KindText, Folder: true},
	{Key: "scmRepositoryOpen", Category: CatGit, Kind: KindBool, Memory: true},
	{Key: "scmRemotes", Category: CatGit, Kind: KindBool, Memory: true},

	{Key: "vim", Category: CatVim, Kind: KindBool, Folder: true},
	{Key: "vimCtrlKeys", Category: CatVim, Kind: KindBool, Folder: true},

	{Key: "locale", Category: CatGeneral, Kind: KindEnum, Options: []string{"", "en", "zh-Hans", "ja"}},
	{Key: "crashReports", Category: CatGeneral, Kind: KindEnum, Options: []string{"", "always", "never"}},
	{Key: "syncIgnored", Category: CatGeneral, Kind: KindList},
}

// FieldOf returns the field of the preference of key.
func FieldOf(key string) (Field, bool) {
	i := slices.IndexFunc(Schema, func(f Field) bool { return f.Key == key })
	if i < 0 {
		return Field{}, false
	}
	return Schema[i], true
}

// FolderKeys are the keys of the preferences a folder may choose.
func FolderKeys() []string {
	var keys []string
	for _, f := range Schema {
		if f.Folder {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// prefFields are the fields of Prefs by their keys in the settings' file.
var prefFields = func() map[string]int {
	m := map[string]int{}
	t := reflect.TypeFor[Prefs]()
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			m[name] = i
		}
	}
	return m
}()

// Raw returns the value of the preference of key as JSON, nil for a key
// the preferences don't have. A value the file leaves out is its zero.
func (p *Prefs) Raw(key string) json.RawMessage {
	i, ok := prefFields[key]
	if !ok {
		return nil
	}
	data, _ := json.Marshal(reflect.ValueOf(p).Elem().Field(i).Interface())
	return data
}

// Set makes raw, as JSON, the value of the preference of key, kept in its
// bounds as the file's are.
func (p *Prefs) Set(key string, raw json.RawMessage) error {
	i, ok := prefFields[key]
	if !ok {
		return fmt.Errorf("no preference %q", key)
	}
	f := reflect.ValueOf(p).Elem().Field(i)
	v := reflect.New(f.Type())
	if err := json.Unmarshal(raw, v.Interface()); err != nil {
		return err
	}
	f.Set(v.Elem())
	p.normalize()
	return nil
}

// IsDefaultKey reports whether the preference of key is as a first launch
// has it.
func (p *Prefs) IsDefaultKey(key string) bool {
	d := Default().Prefs
	return jsonEqual(p.Raw(key), d.Raw(key))
}

// Reset gives the preference of key its value of a first launch.
func (p *Prefs) Reset(key string) {
	d := Default().Prefs
	p.Set(key, d.Raw(key))
}

// jsonEqual reports whether two values of JSON are the same, empty lists
// and maps as null.
func jsonEqual(x, y json.RawMessage) bool {
	norm := func(r json.RawMessage) string {
		s := string(r)
		if s == "[]" || s == "{}" || s == "" {
			return "null"
		}
		return s
	}
	return norm(x) == norm(y)
}
