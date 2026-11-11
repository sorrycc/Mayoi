package main

// The state gallery: each surface in each of its states, in every theme,
// light and dark, and every language, side by side on one page, to review
// a change of look across all of them at once and to catch what clips in
// one language or theme alone. Run
// GALLERY=<dir> go test -run TestGallery ./cmd/mayoi
// and open <dir>/index.html.

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
)

// galleryThemes are the themes of the gallery, "" for Mayoi's own.
var galleryThemes = []string{"", themeMatrix, themeClaude, themeQoder, themeGrokBot, themeCursor,
	themePurpleGold, themePumpkin, themeWitchsBrew, themeBloodMoon}

// galleryLocales are the languages Mayoi has a catalog of.
var galleryLocales = []string{"en", "ja", "zh-Hans"}

// galleryStates are the states shown, each brought about on a window just
// opened on the test folder.
var galleryStates = []struct {
	name string
	set  func(hs *harness)
}{
	{"empty", func(hs *harness) {}},
	{"editor", func(hs *harness) { hs.a.openFile(hs.path("main.go"), true) }},
	{"palette", func(hs *harness) { hs.a.run("go.commands") }},
	{"no-match", func(hs *harness) { hs.a.openQuick("zzzz-nothing") }},
	{"sessions", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
	}},
	{"sessions-hover", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
		hs.settle()
		// The pointer over the second row, which shows its buttons.
		hs.tt.Move(120, 222)
	}},
	{"unsaved", func(hs *harness) {
		hs.a.openFile(hs.path("main.go"), true)
		hs.settle()
		hs.insert("// more\n")
	}},
}

func TestGallery(t *testing.T) {
	dir := os.Getenv("GALLERY")
	if dir == "" {
		t.Skip("GALLERY is not set")
	}
	var page strings.Builder
	page.WriteString(`<!doctype html><meta charset="utf-8"><title>Mayoi state gallery</title>
<style>body{font:13px system-ui;margin:16px;background:#888}h2{margin:24px 0 8px}
.row{display:flex;gap:8px;overflow-x:auto}figure{margin:0}img{width:480px;display:block}figcaption{padding:2px 0}</style>`)
	for _, st := range galleryStates {
		fmt.Fprintf(&page, "<h2>%s</h2>\n", html.EscapeString(st.name))
		for _, loc := range galleryLocales {
			page.WriteString(`<div class="row">`)
			for _, theme := range galleryThemes {
				for _, dark := range []bool{false, true} {
					if !dark && alwaysDark(theme) {
						continue
					}
					name := galleryName(st.name, loc, theme, dark)
					t.Run(name, func(t *testing.T) {
						locale := i18n.Locale()
						i18n.SetLocaleFunc(func() string { return loc })
						t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return locale }) })
						hs := newHarness(t, dark)
						hs.tt.SetLocale(loc)
						if theme != "" {
							hs.a.setTheme(theme)
						}
						st.set(hs)
						hs.settle()
						os.Setenv("SHOTS", dir)
						defer os.Unsetenv("SHOTS")
						hs.shot(name)
					})
					fmt.Fprintf(&page, `<figure><img loading="lazy" src="%s.png"><figcaption>%s</figcaption></figure>`,
						name, html.EscapeString(name))
				}
			}
			page.WriteString("</div>\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func galleryName(state, loc, theme string, dark bool) string {
	if theme == "" {
		theme = "mayoi"
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	return strings.Join([]string{state, loc, theme, mode}, "-")
}
