package main

import (
	"runtime"
	"testing"

	"mayoi/internal/settings"
)

const iconsPlugin = `{
  "name": "icons",
  "contributes": {
    "iconThemes": [{"id": "icons.set", "label": "Icon Set", "path": "dist/set.json"}]
  }
}`

// An icon theme in VS Code's format, its icons beside its file's folder,
// as Material Icon Theme has them.
const iconSet = `{
  // comments, as VS Code's files may have
  "iconDefinitions": {
    "file": {"iconPath": "./../icons/file.svg"},
    "go": {"iconPath": "./../icons/go.svg"},
    "go-light": {"iconPath": "./../icons/go_light.svg"},
    "ts": {"iconPath": "./../icons/ts.svg"},
    "test-ts": {"iconPath": "./../icons/test-ts.svg"},
    "python": {"iconPath": "./../icons/python.svg"},
    "docker": {"iconPath": "./../icons/docker.svg"},
    "docs": {"iconPath": "./../icons/docs.svg"},
    "broken": {"iconPath": "./../icons/broken.svg"},
    "font": {"fontCharacter": "\\E001"},
    "png": {"iconPath": "./../icons/png.png"},
    "folder": {"iconPath": "./../icons/folder.svg"},
    "folder-open": {"iconPath": "./../icons/folder-open.svg"},
    "folder-src": {"iconPath": "./../icons/folder-src.svg"},
    "folder-src-open": {"iconPath": "./../icons/folder-src-open.svg"},
    "folder-lib": {"iconPath": "./../icons/folder-lib.svg"}
  },
  "file": "file",
  "folder": "folder",
  "folderExpanded": "folder-open",
  "fileExtensions": {"go": "go", "ts": "ts", "spec.ts": "test-ts", "bad": "broken", "png": "png", "fnt": "font"},
  "fileNames": {"Dockerfile": "docker", "docs/readme.md": "docs"},
  "languageIds": {"python": "python"},
  "folderNames": {"src": "folder-src", "lib": "folder-lib"},
  "folderNamesExpanded": {"src": "folder-src-open"},
  "light": {"fileExtensions": {"go": "go-light"}}
}`

const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M0 0h16v16H0z" fill="#e44"/></svg>`

func withIconTheme(t *testing.T) *harness {
	hs := newHarness(t, false)
	files := map[string]string{"plugin.json": iconsPlugin, "dist/set.json": iconSet, "icons/broken.svg": "<svg"}
	for _, name := range []string{"file", "go", "go_light", "ts", "test-ts", "python", "docker", "docs", "folder", "folder-open", "folder-src", "folder-src-open", "folder-lib"} {
		files["icons/"+name+".svg"] = iconSVG
	}
	withPlugins(hs, map[string]map[string]string{"icons": files})
	return hs
}

func TestIconThemeFiles(t *testing.T) {
	hs := withIconTheme(t)
	p := paletteFor("", true)
	if s, _ := fileIcon(p, "main.go"); s != iconFileCode {
		t.Fatal("no icon theme chosen: Mayoi's icons")
	}
	hs.a.settings.IconTheme = "icons.set"
	th := activeIconTheme()
	if th == nil {
		t.Fatal("the icon theme did not read")
	}
	for name, want := range map[string]string{
		"main.go":                         "go",
		"x.ts":                            "ts",
		"a.spec.ts":                       "test-ts", // the longest extension first
		"DOCKERFILE":                      "docker",  // names in any case
		"/home/me/project/docs/README.md": "docs",    // a name with its folder
		"README.md":                       "file",
		"tool.py":                         "python", // by its language
		"notes.zzz":                       "file",
		"a.bad":                           "broken",
	} {
		if got := th.fileID(iconKey{name: iconName(name)}); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if runtime.GOOS == "windows" {
		if got := th.fileID(iconKey{name: iconName(`C:\Users\me\project\docs\readme.md`)}); got != "docs" {
			t.Errorf("a Windows path: %q", got)
		}
	}
	if got := th.fileID(iconKey{name: "main.go", light: true}); got != "go-light" {
		t.Errorf("light window: %q", got)
	}
	if got := th.fileID(iconKey{name: "x.ts", light: true}); got != "ts" {
		t.Errorf("light window keeps what light leaves out: %q", got)
	}
	// An icon that is no SVG, or no file, is Mayoi's.
	if got := th.fileID(iconKey{name: "a.png"}); got != "file" {
		t.Errorf("a PNG icon: %q", got)
	}
	if got := th.fileID(iconKey{name: "a.fnt"}); got != "file" {
		t.Errorf("a font's icon: %q", got)
	}
	goIcon, _ := fileIcon(p, "main.go")
	if goIcon == nil || goIcon == iconFileCode {
		t.Fatal("main.go has the theme's icon")
	}
	if again, _ := fileIcon(p, "other.go"); again != goIcon {
		t.Error("an icon is read once")
	}
	if !isThemeIcon(goIcon) || isThemeIcon(iconFileCode) {
		t.Error("the theme's icons show in their own colors, Mayoi's in the text's")
	}
	if s, _ := fileIcon(p, "a.bad"); s != iconFile {
		t.Error("an icon that does not parse is Mayoi's")
	}
	if light, _ := fileIcon(paletteFor("", false), "main.go"); light == goIcon {
		t.Error("a light window has the light icon")
	}
	hs.a.settings.IconTheme = "icons.gone"
	if s, _ := fileIcon(p, "main.go"); s != iconFileCode {
		t.Error("an icon theme no plugin has: Mayoi's icons")
	}
}

func TestIconThemeFolders(t *testing.T) {
	hs := withIconTheme(t)
	hs.a.settings.IconTheme = "icons.set"
	th := activeIconTheme()
	for _, c := range []struct {
		name string
		open bool
		want string
	}{
		{"src", false, "folder-src"},
		{"src", true, "folder-src-open"},
		{"/home/me/project/SRC", true, "folder-src-open"},
		{"lib", true, "folder-lib"}, // no open icon: its closed one
		{"other", false, "folder"},
		{"other", true, "folder-open"},
	} {
		if got := th.folderID(iconKey{name: iconName(c.name), folder: true, open: c.open}); got != c.want {
			t.Errorf("%s open=%v: %q, want %q", c.name, c.open, got, c.want)
		}
	}
	p := paletteFor("", true)
	if s, _ := folderIcon(p, "src", false); s == iconFolder {
		t.Error("src has the theme's icon")
	}
}

func TestIconThemeAssociations(t *testing.T) {
	hs := withIconTheme(t)
	hs.a.settings.IconTheme = "icons.set"
	hs.a.settings.IconThemeAssociations = &settings.IconAssociations{
		Files: map[string]string{
			"*.foo": "ts", "Jenkinsfile": "go", "notes/TODO.txt": "docker", "*.go": "python", "*.ts": "nothing",
		},
		Folders: map[string]string{"scripts": "folder-src", "tools": "folder-lib", "src": "nothing"},
	}
	th := activeIconTheme()
	for name, want := range map[string]string{
		"a.foo":             "ts",
		"jenkinsfile":       "go",
		"/x/notes/todo.txt": "docker",
		"todo.txt":          "file",
		"main.go":           "python", // the settings' before the theme's
		"x.ts":              "ts",     // an icon the theme has not: the theme's
		"Dockerfile":        "docker",
		"a.spec.ts":         "test-ts",
	} {
		if got := th.fileID(iconKey{name: iconName(name)}); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	for _, c := range []struct {
		name string
		open bool
		want string
	}{
		{"scripts", false, "folder-src"},
		{"scripts", true, "folder-src-open"}, // the open icon the theme pairs
		{"tools", true, "folder-lib"},
		{"src", true, "folder-src-open"},
	} {
		if got := th.folderID(iconKey{name: iconName(c.name), folder: true, open: c.open}); got != c.want {
			t.Errorf("folder %s open=%v: %q, want %q", c.name, c.open, got, c.want)
		}
	}
	// Changed associations are matched anew.
	hs.a.settings.IconThemeAssociations = nil
	if got := activeIconTheme().fileID(iconKey{name: "main.go"}); got != "go" {
		t.Errorf("without associations: %q", got)
	}
}

func TestIconName(t *testing.T) {
	for in, want := range map[string]string{
		"Main.go":          "main.go",
		"/a/b/c/d/e/f.txt": "c/d/e/f.txt",
		"src/":             "src",
	} {
		if got := iconName(in); got != want {
			t.Errorf("iconName(%q) = %q, want %q", in, got, want)
		}
	}
}
