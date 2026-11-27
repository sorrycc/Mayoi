package main

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/keymap"
	"mayoi/internal/plugin"
	"mayoi/internal/settings"
)

// The file icon themes plugins contribute, in VS Code's format: icons,
// each an SVG file of the plugin, and which of them files and folders
// have, by their names, extensions and languages, with other choices for
// light windows. The iconTheme setting chooses one; without one, or when
// its plugin is off, files and folders have Mayoi's own icons.

// iconMaps are which icons an icon theme gives files and folders: the
// keys of VS Code's icon themes, at their top or in their "light" part.
type iconMaps struct {
	File                string            `json:"file"`
	Folder              string            `json:"folder"`
	FolderExpanded      string            `json:"folderExpanded"`
	FileExtensions      map[string]string `json:"fileExtensions"`
	FileNames           map[string]string `json:"fileNames"`
	LanguageIDs         map[string]string `json:"languageIds"`
	FolderNames         map[string]string `json:"folderNames"`
	FolderNamesExpanded map[string]string `json:"folderNamesExpanded"`
}

// iconThemeFile is an icon theme as its file says it.
type iconThemeFile struct {
	IconDefinitions map[string]struct {
		IconPath string `json:"iconPath"`
	} `json:"iconDefinitions"`
	iconMaps
	Light *iconMaps `json:"light"`
}

// pluginIconTheme is an icon theme a plugin contributes.
type pluginIconTheme struct {
	plugin  *plugin.Plugin
	contrib plugin.IconThemeContrib
}

// pluginIconThemes are the icon themes of the plugins that are on.
func pluginIconThemes() []pluginIconTheme {
	var out []pluginIconTheme
	for _, p := range plugins.enabled() {
		for _, t := range p.Contributes.IconThemes {
			out = append(out, pluginIconTheme{p, t})
		}
	}
	return out
}

// iconTheme is an icon theme read: the files of its icons, and its maps
// for dark windows and for light ones, their keys in lower case as names
// are matched.
type iconTheme struct {
	plugin *plugin.Plugin
	paths  map[string]string // the file of each icon, by its id
	maps   [2]iconMaps       // for dark windows, then light ones
	// opened is the icon of an open folder of each icon of a closed one,
	// as the theme pairs them, for the folders the settings associate.
	opened [2]map[string]string

	mu   sync.Mutex
	svgs map[string]*ui.SVG // nil for an icon that does not draw
	// assoc is what the settings associate, made of from, and ids the
	// icons found, by darkness, kind and name, as they last associated.
	from  *settings.IconAssociations
	assoc iconAssoc
	ids   map[iconKey]string
}

// iconKey is what an icon is found for: a file's name, or a folder's,
// open or not, in a dark window or a light one.
type iconKey struct {
	name        string
	folder      bool
	open, light bool
}

// iconAssoc is what the settings associate, keys in lower case: icons of
// files by name or path's end and by extension, and of folders.
type iconAssoc struct {
	names, exts, folders map[string]string
}

// maxIconIDs bounds the icons a theme remembers it found.
const maxIconIDs = 20000

// readIconTheme reads the icon theme of a plugin, nil for one that does
// not read, as the plugin's log says.
func readIconTheme(t pluginIconTheme) *iconTheme {
	name := path.Clean(t.contrib.Path)
	data, err := fs.ReadFile(t.plugin.FS(), name)
	if err == nil {
		var f iconThemeFile
		if err = json.Unmarshal(keymap.StripJSONC(data), &f); err == nil {
			return newIconTheme(t.plugin, path.Dir(name), &f)
		}
	}
	t.plugin.Log("error", "icon theme "+t.contrib.ID+": "+err.Error())
	return nil
}

// newIconTheme makes the theme of a file read, whose icons' paths are
// from dir, in the plugin's folder.
func newIconTheme(p *plugin.Plugin, dir string, f *iconThemeFile) *iconTheme {
	t := &iconTheme{plugin: p, paths: map[string]string{}, svgs: map[string]*ui.SVG{}, ids: map[iconKey]string{}}
	for id, d := range f.IconDefinitions {
		// SVGs alone: Mayoi draws no fonts' glyphs or PNGs as icons.
		if strings.EqualFold(path.Ext(d.IconPath), ".svg") {
			t.paths[id] = path.Join(dir, d.IconPath)
		}
	}
	dark := lowerMaps(f.iconMaps)
	light := dark
	if l := f.Light; l != nil {
		light = overMaps(dark, lowerMaps(*l))
	}
	t.maps = [2]iconMaps{dark, light}
	for i, m := range t.maps {
		t.opened[i] = map[string]string{}
		for name, id := range m.FolderNames {
			if open, ok := m.FolderNamesExpanded[name]; ok {
				if _, seen := t.opened[i][id]; !seen {
					t.opened[i][id] = open
				}
			}
		}
	}
	return t
}

// lowerMaps is m with its keys in lower case, as VS Code matches them.
func lowerMaps(m iconMaps) iconMaps {
	lower := func(in map[string]string) map[string]string {
		out := make(map[string]string, len(in))
		for k, v := range in {
			out[strings.ToLower(k)] = v
		}
		return out
	}
	m.FileExtensions, m.FileNames, m.LanguageIDs = lower(m.FileExtensions), lower(m.FileNames), lower(m.LanguageIDs)
	m.FolderNames, m.FolderNamesExpanded = lower(m.FolderNames), lower(m.FolderNamesExpanded)
	return m
}

// overMaps is base with what light says in place of its own.
func overMaps(base, light iconMaps) iconMaps {
	over := func(a, b map[string]string) map[string]string {
		if len(b) == 0 {
			return a
		}
		out := make(map[string]string, len(a)+len(b))
		for k, v := range a {
			out[k] = v
		}
		for k, v := range b {
			out[k] = v
		}
		return out
	}
	m := base
	m.File = cmp.Or(light.File, base.File)
	m.Folder = cmp.Or(light.Folder, base.Folder)
	m.FolderExpanded = cmp.Or(light.FolderExpanded, base.FolderExpanded)
	m.FileExtensions, m.FileNames = over(base.FileExtensions, light.FileExtensions), over(base.FileNames, light.FileNames)
	m.LanguageIDs = over(base.LanguageIDs, light.LanguageIDs)
	m.FolderNames = over(base.FolderNames, light.FolderNames)
	m.FolderNamesExpanded = over(base.FolderNamesExpanded, light.FolderNamesExpanded)
	return m
}

// makeIconAssoc makes what the settings associate ready to match.
func makeIconAssoc(a *settings.IconAssociations) iconAssoc {
	as := iconAssoc{names: map[string]string{}, exts: map[string]string{}, folders: map[string]string{}}
	if a == nil {
		return as
	}
	for k, id := range a.Files {
		k = strings.ToLower(strings.Trim(filepath.ToSlash(strings.TrimSpace(k)), "/"))
		if ext, ok := strings.CutPrefix(k, "*."); ok && ext != "" {
			as.exts[ext] = id
		} else if k != "" {
			as.names[k] = id
		}
	}
	for k, id := range a.Folders {
		if k = strings.ToLower(strings.Trim(filepath.ToSlash(strings.TrimSpace(k)), "/")); k != "" {
			as.folders[k] = id
		}
	}
	return as
}

// iconThemes keeps the icon themes read, by id, nil for one that does not
// read, until the plugins change.
var iconThemes = struct {
	sync.Mutex
	host   *pluginHost
	gen    uint64
	themes map[string]*iconTheme
}{themes: map[string]*iconTheme{}}

// themeIcons are the icons of the icon themes read, which show in their
// own colors.
var themeIcons sync.Map // *ui.SVG to struct{}

// isThemeIcon reports whether s is an icon theme's icon.
func isThemeIcon(s *ui.SVG) bool {
	_, ok := themeIcons.Load(s)
	return ok
}

// iconElement shows an icon of fileIcon or folderIcon, size high: an icon
// theme's in its own colors, Mayoi's in col.
func iconElement(c *ui.Context, s *ui.SVG, size float32, col ui.Color) ui.Element {
	if isThemeIcon(s) {
		w, h := s.Size()
		return ui.Image(c, s).Height(size).Width(size * w / max(h, 1)).Shrink(0)
	}
	return ui.Icon(c, s).FontSize(size).TextColor(col)
}

// paintIcon paints an icon of fileIcon or folderIcon in r, as iconElement
// shows it.
func paintIcon(pt *ui.Painter, s *ui.SVG, r ui.Rect, col ui.Color) {
	if isThemeIcon(s) {
		pt.Image(s, r, ui.Contain)
		return
	}
	pt.Icon(s, r, col)
}

// activeIconTheme is the icon theme the settings choose, with what they
// associate, nil for Mayoi's own icons.
func activeIconTheme() *iconTheme {
	if plugins == nil {
		return nil
	}
	s := plugins.prefs()
	if s == nil || s.IconTheme == "" {
		return nil
	}
	t := iconThemeByID(s.IconTheme)
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.from != s.IconThemeAssociations || t.assoc.names == nil {
		t.from, t.assoc = s.IconThemeAssociations, makeIconAssoc(s.IconThemeAssociations)
		clear(t.ids)
	}
	t.mu.Unlock()
	return t
}

// iconThemeByID is the icon theme of id, nil when no plugin that is on
// has it or it does not read.
func iconThemeByID(id string) *iconTheme {
	plugins.mu.Lock()
	gen := plugins.gen
	plugins.mu.Unlock()
	iconThemes.Lock()
	defer iconThemes.Unlock()
	if iconThemes.host != plugins || iconThemes.gen != gen {
		iconThemes.host, iconThemes.gen, iconThemes.themes = plugins, gen, map[string]*iconTheme{}
		themeIcons.Clear()
	}
	if t, ok := iconThemes.themes[id]; ok {
		return t
	}
	var t *iconTheme
	for _, pt := range pluginIconThemes() {
		if pt.contrib.ID == id {
			t = readIconTheme(pt)
			break
		}
	}
	iconThemes.themes[id] = t
	return t
}

// themeFileIcon is the icon the icon theme chosen gives a file, by its
// name or path, nil for Mayoi's own.
func themeFileIcon(p *palette, name string) *ui.SVG {
	if t := activeIconTheme(); t != nil {
		return t.icon(iconKey{name: iconName(name), light: !p.dark})
	}
	return nil
}

// themeFolderIcon is the icon the icon theme chosen gives a folder, open
// or not, by its name or path, nil for Mayoi's own.
func themeFolderIcon(p *palette, name string, open bool) *ui.SVG {
	if t := activeIconTheme(); t != nil {
		return t.icon(iconKey{name: iconName(name), folder: true, open: open, light: !p.dark})
	}
	return nil
}

// iconName is a name or path as icons are found for it: in lower case,
// with slashes, its last few parts at most.
func iconName(name string) string {
	name = strings.Trim(strings.ToLower(filepath.ToSlash(name)), "/")
	i := len(name)
	for range maxIconParts {
		j := strings.LastIndexByte(name[:i], '/')
		if j < 0 {
			return name
		}
		i = j
	}
	return name[i+1:]
}

// maxIconParts is how many parts of a path, from its end, an icon is
// found for.
const maxIconParts = 4

// icon is the icon of a file or a folder, nil when the theme has none
// that draws.
func (t *iconTheme) icon(k iconKey) *ui.SVG {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, ok := t.ids[k]
	if !ok {
		if k.folder {
			id = t.folderID(k)
		} else {
			id = t.fileID(k)
		}
		if len(t.ids) >= maxIconIDs {
			clear(t.ids)
		}
		t.ids[k] = id
	}
	return t.svg(id)
}

// ends are the ends of a path, the whole first: "a/b.go", then "b.go".
func ends(name string) []string {
	out := []string{name}
	for i := strings.IndexByte(name, '/'); i >= 0; i = strings.IndexByte(name, '/') {
		name = name[i+1:]
		out = append(out, name)
	}
	return out
}

// fileID is the id of the icon of a file: what the settings associate
// with its name, then with its extension, then what the theme has of its
// name, its extensions, the longest first, and its language.
func (t *iconTheme) fileID(k iconKey) string {
	m := &t.maps[b2i(k.light)]
	names := ends(k.name)
	base := names[len(names)-1]
	for _, n := range names {
		if id := t.has(t.assoc.names[n]); id != "" {
			return id
		}
	}
	for i := strings.IndexByte(base, '.'); i >= 0; i = strings.IndexByte(base, '.') {
		base = base[i+1:]
		if id := t.has(t.assoc.exts[base]); id != "" {
			return id
		}
	}
	for _, n := range names {
		if id := t.has(m.FileNames[n]); id != "" {
			return id
		}
	}
	base = names[len(names)-1]
	for i := strings.IndexByte(base, '.'); i >= 0; i = strings.IndexByte(base, '.') {
		base = base[i+1:]
		if id := t.has(m.FileExtensions[base]); id != "" {
			return id
		}
	}
	if len(m.LanguageIDs) > 0 {
		lang := highlight.ID(highlight.Lexer(names[len(names)-1]))
		if v, ok := vscodeLanguageIDs[lang]; ok {
			lang = v
		}
		if id := t.has(m.LanguageIDs[lang]); id != "" {
			return id
		}
	}
	return t.has(m.File)
}

// folderID is the id of the icon of a folder, open or not: what the
// settings associate with its name, the open icon the theme pairs with it
// for an open one, then what the theme has of its name.
func (t *iconTheme) folderID(k iconKey) string {
	i := b2i(k.light)
	m := &t.maps[i]
	names := ends(k.name)
	for _, n := range names {
		if id := t.has(t.assoc.folders[n]); id != "" {
			if open := t.has(t.opened[i][id]); k.open && open != "" {
				return open
			}
			return id
		}
	}
	for _, n := range names {
		if k.open {
			if id := t.has(m.FolderNamesExpanded[n]); id != "" {
				return id
			}
		}
		// An open folder the theme names but has no open icon of keeps its
		// closed one, as in VS Code.
		if id := t.has(m.FolderNames[n]); id != "" {
			return id
		}
	}
	if k.open {
		if id := t.has(m.FolderExpanded); id != "" {
			return id
		}
	}
	return t.has(m.Folder)
}

// has is id when the theme has an icon of it, else "".
func (t *iconTheme) has(id string) string {
	if _, ok := t.paths[id]; ok {
		return id
	}
	return ""
}

// svg is the icon of id, read the first time it shows, nil for "" or one
// that does not draw, as the plugin's log says once.
func (t *iconTheme) svg(id string) *ui.SVG {
	if id == "" {
		return nil
	}
	if s, ok := t.svgs[id]; ok {
		return s
	}
	data, err := fs.ReadFile(t.plugin.FS(), t.paths[id])
	var s *ui.SVG
	if err == nil {
		s, err = ui.ParseSVG(data)
	}
	if err != nil {
		t.plugin.Log("warn", "icon "+id+": "+err.Error())
	} else {
		themeIcons.Store(s, struct{}{})
	}
	t.svgs[id] = s
	return s
}

// b2i is 1 for true.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// vscodeLanguageIDs are the ids VS Code gives the languages Mayoi names
// otherwise, by Mayoi's.
var vscodeLanguageIDs = map[string]string{
	"bash": "shellscript", "c++": "cpp", "c#": "csharp", "docker": "dockerfile", "basemakefile": "makefile",
	"batchfile": "bat", "tsx": "typescriptreact", "react": "javascriptreact", "fsharp": "fsharp",
}
