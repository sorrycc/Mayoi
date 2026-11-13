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
	"github.com/egoist/mygo/ui"
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
	{"file-loading", func(hs *harness) {
		hs.a.openFile(hs.path("main.go"), true)
		hs.settle()
		hs.a.activeEditor().sync = syncLoading
	}},
	{"file-error", func(hs *harness) { hs.a.openFile(hs.path("missing.go"), true) }},
	{"palette", func(hs *harness) { hs.a.run("go.commands") }},
	{"no-match", func(hs *harness) { hs.a.openQuick("zzzz-nothing") }},
	{"path-pending", func(hs *harness) {
		hs.a.openQuick(hs.path("pending.go"))
		p := &hs.a.quick.probe
		p.key = hs.a.quickPathKey()
		p.name, p.pending = hs.a.quick.query, true
		p.read.Invalidate()
		p.read.Start(p.key)
	}},
	{"path-missing", func(hs *harness) { hs.a.openQuick(hs.path("missing.go")) }},
	{"path-error", func(hs *harness) {
		hs.a.quick.probe.stat = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
		hs.a.openQuick(hs.path("denied.go"))
	}},
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
	{"sessions-find", func(hs *harness) {
		agentHome(hs.t, hs.root)
		hs.a.sessions.all = 1
		hs.a.showSessions()
		hs.settle()
		// The query marked in the rows, and the keyboard in the list, which
		// shows the chosen row's buttons.
		hs.a.sessions.query = "fix else"
		hs.settle()
		hs.key(0, ui.KeyTab)
		hs.key(0, ui.KeyTab)
	}},
	{"sessions-why", func(hs *harness) {
		home := agentHome(hs.t, hs.root)
		// A titled session, which the query finds in its first prompt.
		p := filepath.Join(home, ".claude", "projects", "p-titled", "titled.jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(`{"type":"user","message":{"content":"the sign in page of the account settings shows a blank form"},"cwd":"`+
			filepath.ToSlash(hs.root)+`"}`+"\n"+`{"type":"custom-title","customTitle":"Login bug"}`+"\n"), 0o644)
		hs.a.showSessions()
		hs.settle()
		hs.a.sessions.query = "login blank"
	}},
	{"sessions-agents", func(hs *harness) {
		// A session of each agent, each row marked with its agent's icon.
		home := agentHome(hs.t, hs.root)
		cwd := filepath.ToSlash(hs.root)
		files := map[string]string{
			".qoder/projects/p/q1.jsonl": `{"type":"user","message":{"content":"add dark mode"},"cwd":"` + cwd + `"}` + "\n",
			".codex/sessions/2026/rollout-x1.jsonl": `{"type":"session_meta","payload":{"id":"x1","cwd":"` + cwd + `"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"user","content":"speed up the build"}}` + "\n",
			".grok/sessions/p/g1/summary.json": `{"info":{"id":"g1","cwd":"` + cwd + `"},"generated_title":"Write the release notes"}`,
		}
		for name, text := range files {
			p := filepath.Join(home, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(text), 0o644)
		}
		hs.a.showSessions()
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
