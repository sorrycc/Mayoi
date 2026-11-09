package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
	bundled "mayoi/plugins"
)

const prettierManifest = `{
  "name": "prettier", "displayName": "Prettier", "version": "3.1.0", "author": "Mayoi Editor",
  "description": "Formats JavaScript, TypeScript, CSS and Markdown with Prettier",
  "main": "main.ts",
  "contributes": {
    "commands": [{"id": "prettier.format", "title": "Format with Prettier"}, {"id": "prettier.check", "title": "Check Formatting"}],
    "keybindings": [{"key": "ctrl+alt+shift+p", "command": "prettier.format"}],
    "menus": {"editor/context": [{"command": "prettier.format"}]},
    "statusItems": [{"id": "prettier.status", "align": "left"}],
    "formatters": [{"id": "prettier.code", "title": "Prettier", "languages": ["javascript", "typescript", "css", "markdown"]}],
    "settings": {
      "printWidth": {"type": "number", "default": 100, "description": "The line length to wrap at"},
      "semi": {"type": "boolean", "default": true, "description": "Semicolons at the ends of statements"}
    }
  }
}`

// withPluginsPage opens the Plugins page of the settings window over
// plugins of each kind: from Git with an update waiting, copied in,
// broken, and those Mayoi comes with; and an index of three.
func withPluginsPage(t *testing.T) (*prefsHarness, *pluginHost) {
	ph := newPrefsHarness(t)
	h := withPlugins(ph.harness, map[string]map[string]string{
		"prettier": {"plugin.json": prettierManifest, "main.ts": ``},
		"broken":   {"plugin.json": `{"name": "broken", "version": "0.1.0", "description": "Its manifest names no file", "main": 3}`},
		"rainbow":  {"plugin.json": `{"name": "rainbow", "displayName": "Rainbow Brackets", "version": "1.2.0", "description": "Colors brackets by how deep they are", "main": "main.ts"}`, "main.ts": ``},
	})
	plugin.WriteRecords(h.reg.Dirs.Plugins, plugin.Records{"prettier": {URL: "https://github.com/mayoi-editor/plugin-prettier", Commit: "abc"}})
	h.reg.Dirs.Bundled = bundled.FS
	h.reg.Load()
	h.mu.Lock()
	h.updates = map[string]string{"prettier": "def"}
	h.mu.Unlock()
	ph.w.setPluginSetting("rainbow", "enabled", false, true)
	pg := &ph.w.plugins
	pg.index = indexItems([]plugin.IndexEntry{
		{Name: "prettier", DisplayName: "Prettier", Description: "Formats code with Prettier", Repository: "https://github.com/mayoi-editor/plugin-prettier", Tags: []string{"format"}},
		{Name: "eslint", DisplayName: "ESLint", Description: "Finds problems in JavaScript and TypeScript", Author: "Mayoi Editor", Repository: "https://github.com/mayoi-editor/plugin-eslint", Tags: []string{"lint"}},
		{Name: "git-blame", Description: "Who changed the line at the caret, in the status bar", Repository: "https://github.com/ann/git-blame"},
	})
	pg.indexOK = true
	ph.w.section = settings.CatPlugins
	ph.frames()
	return ph, h
}

// The page lists where each plugin came from as the plugins change, asks
// before it removes one, and filters the index.
func TestPluginsPage(t *testing.T) {
	ph, h := withPluginsPage(t)
	pg := &ph.w.plugins
	sources := map[string]string{}
	for _, it := range pg.items {
		sources[it.Name] = it.source
	}
	if sources["prettier"] != "mayoi-editor/plugin-prettier" || sources["rainbow"] != locales.PluginsLocal() ||
		sources["word-count"] != locales.PluginsBundled() {
		t.Errorf("sources %v", sources)
	}
	if !ph.pt.HasText(locales.PluginsBy("Mayoi Editor")) || !ph.pt.HasText(locales.PluginsUpdateWaits()) {
		t.Errorf("no author or update shows: %q", ph.pt.Texts())
	}
	if !pg.installed["prettier"] || pg.installed["eslint"] {
		t.Errorf("installed %v", pg.installed)
	}

	// The list is kept while the plugins don't change.
	items := pg.items
	ph.frames()
	if &pg.items[0] != &items[0] {
		t.Error("the list was made anew without a change")
	}

	if err := ph.pt.Click(locales.PluginsRemove()); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if pg.confirm != "broken" || !ph.pt.HasText(locales.PluginsRemoveAsk("broken")) {
		t.Fatalf("no confirm of the removal: %q, %q", pg.confirm, ph.pt.Texts())
	}
	if err := ph.pt.Click(locales.ButtonCancel()); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if pg.confirm != "" {
		t.Errorf("Cancel left the confirm of %s", pg.confirm)
	}
	if _, err := os.Stat(h.reg.Plugin("broken").Dir); err != nil {
		t.Errorf("the plugin is gone: %v", err)
	}

	// The settings show in the details, which the row's link opens.
	if ph.pt.HasText("printWidth") {
		t.Error("the row shows the plugin's settings")
	}
	if err := ph.pt.Click(locales.PluginsSettingsCount(1)); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if pg.detail != "word-count" {
		t.Fatalf("the settings link opened %q", pg.detail)
	}
	ph.w.closeDetail()
	ph.frames()

	pg.filter = "LINT"
	ph.frames()
	if got := ph.w.indexShown(); len(got) != 1 || pg.index[got[0]].Name != "eslint" {
		t.Errorf("the filter finds %v", got)
	}
	pg.filter = "nothing like it"
	ph.frames()
	if !ph.pt.HasText(locales.PluginsIndexNoMatch("nothing like it")) {
		t.Errorf("no word of nothing found: %q", ph.pt.Texts())
	}
}

// A row opens the details of its plugin, which list what it adds once,
// read how it does only while they show, and go back to the list where
// it was with Esc.
func TestPluginDetail(t *testing.T) {
	ph, h := withPluginsPage(t)
	pg := &ph.w.plugins
	redrawn := 0
	h.prefsWin = func() { redrawn++ }

	if err := ph.pt.Click(locales.PluginsRemove()); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if pg.detail != "" {
		t.Fatalf("Remove opened the details of %s", pg.detail)
	}
	pg.confirm = ""
	ph.pt.SetSize(940, 520)
	ph.frames()
	pg.listScroll.Y = 160
	ph.frames()
	scrolled := pg.listScroll.Y
	if scrolled == 0 {
		t.Fatal("the list does not scroll")
	}

	if err := ph.pt.Click("Rainbow Brackets"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if pg.detail != "rainbow" || !ph.pt.HasText(locales.PluginsBack()) {
		t.Fatalf("the row opened no details: %q", pg.detail)
	}
	ph.pt.Key(0, ui.KeyEscape)
	ph.frames()
	if pg.detail != "" {
		t.Fatal("Esc left the details")
	}
	if pg.listScroll.Y != scrolled {
		t.Errorf("the list came back scrolled to %v, not %v", pg.listScroll.Y, scrolled)
	}

	ph.w.openDetail("prettier")
	ph.frames()
	for _, want := range []string{strings.ToUpper(locales.PluginsHealth()), locales.PluginsMemory(), locales.PluginsCommands(), "Format with Prettier",
		"prettier.check", locales.PluginsMenus(), "editor/context", locales.PluginsStatusItems(), locales.PluginsFormatters(),
		"javascript, typescript, css, markdown", "printWidth", locales.PluginsLogOpen()} {
		if !ph.pt.HasText(want) {
			t.Errorf("the details show no %q", want)
		}
	}
	for _, none := range []string{locales.PluginsThemes(), locales.PluginsLanguageServers(), locales.PluginsSnippets()} {
		if ph.pt.HasText(none) {
			t.Errorf("the details show the empty group %q", none)
		}
	}
	d := &pg.info
	groups, at := &d.groups[0], d.at
	ph.frames()
	if &d.groups[0] != groups {
		t.Error("what the plugin adds was worked out anew without a change")
	}
	if d.at != at {
		t.Error("the health was read again before its time")
	}

	pl := h.reg.Plugin("prettier")
	pl.Log("error", "could not find prettier")
	ph.settle()
	ph.pt.Advance(pluginHealthEvery)
	ph.frames()
	if !ph.pt.HasText("could not find prettier") {
		t.Errorf("the log shows no new line: %q", ph.pt.Texts())
	}
	if redrawn == 0 {
		t.Error("an error logged did not draw the settings window")
	}

	ph.w.closeDetail()
	ph.frames()
	if pg.info.name != "" {
		t.Error("the details kept what they read once closed")
	}
}

func TestShotPluginsPage(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		ph, h := withPluginsPage(t)
		ph.pt.SetDark(dark)
		ph.pt.SetSize(940, 1500)
		ph.frames()
		ph.shot("plugins" + suffix)
		if r, ok := ph.pt.Find(locales.PluginsRemove()); ok {
			ph.pt.Move(r.X+r.W/2, r.Y+r.H/2)
		}
		ph.frames()
		ph.shot("plugins-hover" + suffix)
		ph.pt.Click(locales.PluginsRemove())
		ph.frames()
		ph.shot("plugins-confirm" + suffix)
		pg := &ph.w.plugins
		pg.confirm = ""
		pg.err, pg.errFor = "git clone failed: repository not found", ""
		pg.reading = true
		ph.pt.SetSize(940, 1100)
		ph.frames()
		ph.shot("plugins-reading" + suffix)
		pg.reading, pg.indexErr = false, locales.PluginsIndexFailed("dial tcp: no such host")
		ph.frames()
		ph.shot("plugins-index-failed" + suffix)
		pg.indexErr, pg.index, pg.shown = "", nil, nil
		ph.frames()
		ph.shot("plugins-index-empty" + suffix)
		pg.err = ""
		pl := h.reg.Plugin("prettier")
		pl.Log("info", "activated")
		pl.Log("warn", "no .prettierrc found, using the defaults")
		pl.Log("error", "could not parse src/app.ts: Unexpected token (12:4)")
		ph.w.openDetail("prettier")
		ph.pt.SetSize(940, 1500)
		ph.frames()
		ph.shot("plugin-detail" + suffix)
		ph.w.openDetail("word-count")
		ph.frames()
		ph.shot("plugin-detail-bundled" + suffix)
		ph.pt.Key(0, ui.KeyEscape)
		ph.frames()
		ph.shot("plugins-back" + suffix)
	}
}
