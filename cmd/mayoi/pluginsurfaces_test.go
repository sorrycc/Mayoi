package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
	bundled "mayoi/plugins"
)

// withBundled starts the plugins Mayoi comes with over a folder that has
// TODOs in it, a Markdown file open.
func withBundled(hs *harness) *pluginHost {
	os.WriteFile(hs.path("main.go"), []byte("package main\n\n// TODO: say more\n// FIXME: the edge case\nfunc main() {}\n"), 0o644)
	os.WriteFile(hs.path("notes.go"), []byte("package main\n\n// HACK: until the API settles\n"), 0o644)
	h := withPlugins(hs, nil)
	h.reg.Locale = i18n.Locale
	h.reg.Dirs.Bundled = bundled.FS
	turnOnBundled(hs, h)
	h.reg.Load()
	hs.a.indexRead.Invalidate()
	hs.a.openFile(hs.path("README.md"), true)
	hs.until("word count", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return strings.HasSuffix(h.status["word-count.status"].Text, " words")
	})
	return h
}

// turnOnBundled turns on the plugins Mayoi comes with, which start off.
func turnOnBundled(hs *harness, h *pluginHost) {
	entries, _ := fs.ReadDir(bundled.FS, ".")
	all := map[string]json.RawMessage{}
	for _, e := range entries {
		all[e.Name()] = json.RawMessage(`{"enabled": true}`)
	}
	hs.a.settings.Prefs.Plugins = all
	h.syncPrefs()
}

func TestTODOsFollowSearchSymlinks(t *testing.T) {
	hs := newHarness(t, false)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "linked.go"), []byte("package main\n// TODO: linked marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, hs.path("linked")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create symlink: %v", err)
		}
		t.Fatal(err)
	}
	h := withBundled(hs)
	a := hs.a
	wantTODOs := func(linked bool) {
		t.Helper()
		hs.until("TODOs with the effective setting", func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			data, _ := json.Marshal(h.views["todos.list"])
			view := string(data)
			return strings.Contains(view, "say more") && strings.Contains(view, "linked marker") == linked
		})
	}
	a.showPluginView("todos.list")
	wantTODOs(true)

	a.settings.Prefs.SearchFollowSymlinks = false
	a.settings.Changed()
	go h.reg.Run("todos.refresh", nil)
	wantTODOs(false)

	if err := os.MkdirAll(hs.path(".mayoi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hs.path(".mayoi/settings.json"), []byte(`{"searchFollowSymlinks": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.loadFolderPrefs(hs.root)
	go h.reg.Run("todos.refresh", nil)
	wantTODOs(true)
}

// A status item set again as it was draws nothing anew; what the frames
// read of the plugins is worked out once, not as each draws.
func TestPluginSurfacesKeepWhatTheyWorkedOut(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": lintPlugin, "main.ts": lintMain}})
	a := hs.a
	pl := h.reg.Plugin("lint")
	h.setStatus(pl, "lint.status", json.RawMessage(`{"text": "3 words"}`))
	hs.settle()
	h.mu.Lock()
	list := h.sortedStatus()
	h.mu.Unlock()
	if len(list) != 1 || list[0].Text != "3 words" {
		t.Fatalf("status %+v", list)
	}
	h.setStatus(pl, "lint.status", json.RawMessage(`{"text": "3 words"}`))
	h.mu.Lock()
	same := h.statusList != nil
	h.mu.Unlock()
	if !same {
		t.Error("the same status again sorted the items anew")
	}
	h.setStatus(pl, "lint.status", json.RawMessage(`{"text": "4 words"}`))
	h.mu.Lock()
	text := h.sortedStatus()[0].Text
	h.mu.Unlock()
	if text != "4 words" {
		t.Errorf("status %q", text)
	}

	readme := hs.path("README.md")
	a.openFile(readme, true)
	hs.until("the diagnostics", func() bool { return len(h.diagnosticsOf(readme)) == 1 })
	if d1, d2 := h.diagnosticsOf(readme), h.diagnosticsOf(readme); &d1[0] != &d2[0] {
		t.Error("the diagnostics were sorted anew without a change")
	}
	if got := h.problemCount(); got != "0  1" {
		t.Errorf("problems %q", got)
	}
	h.setDiagnosticList("lint", readme, "", false, []diagnostic{{Severity: "error", Message: "bad"}})
	if got := h.problemCount(); got != "1  0" {
		t.Errorf("problems after a change %q", got)
	}

	s1, s2 := pluginSlots(), pluginSlots()
	if len(s1) == 0 || &s1[0] != &s2[0] {
		t.Errorf("the views were listed anew without a change: %d", len(s1))
	}
	h.Changed()
	hs.settle()
	if s3 := pluginSlots(); &s3[0] == &s1[0] {
		t.Error("the views were not listed anew as the plugins changed")
	}
}

// The TODOs view draws its tree, chosen and folded by hand, and says so
// when there are none.
func TestPluginViewRows(t *testing.T) {
	hs := newHarness(t, false)
	h := withBundled(hs)
	a := hs.a
	a.showPluginView("todos.list")
	hs.until("the TODOs", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		data, _ := json.Marshal(h.views["todos.list"])
		return strings.Contains(string(data), "say more")
	})
	hs.settle()
	if !hs.tt.HasText("say more") || !hs.tt.HasText("main.go") {
		t.Fatalf("no TODO rows: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	pv := a.viewStates["todos.list"]
	if pv.sel != "k:main.go" || pv.open["k:main.go"] {
		t.Fatalf("the click chose %q, open %v", pv.sel, pv.open)
	}
	if hs.tt.HasText("say more") {
		t.Error("the folded node still shows its rows")
	}

	empty, _ := json.Marshal(viewNode{Type: "empty", Icon: "check", Text: "No TODOs in this folder.", Workspace: a.root})
	h.renderView(h.reg.Plugin("todos"), "todos.list", empty)
	hs.settle()
	if !hs.tt.HasText("No TODOs in this folder.") {
		t.Errorf("no empty state: %q", hs.tt.Texts())
	}
}

func TestTODOFilter(t *testing.T) {
	hs := newHarness(t, false)
	withBundled(hs)
	hs.a.showPluginView("todos.list")
	tt := hs.tt
	hs.until("unfiltered TODOs", func() bool {
		return tt.HasText("say more") && tt.HasText("until the API settles")
	})
	if err := os.WriteFile(hs.path("notes.go"), []byte("package main\n// HACK: marker after refresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Filter TODOs…"); err != nil {
		t.Fatal(err)
	}
	tt.Type("NOTES hack")
	hs.until("a TODO matching the path and tag from the existing scan", func() bool {
		return tt.HasText("until the API settles") && !tt.HasText("say more")
	})
	tt.Type(" absent")
	hs.until("no matches", func() bool { return tt.HasText("No matching TODOs.") })

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("EDGE CASE")
	hs.until("a TODO matching its text", func() bool {
		return tt.HasText("the edge case") && !tt.HasText("say more") && !tt.HasText("until the API settles")
	})
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	hs.until("clearing the filter restores the scanned TODOs", func() bool {
		return tt.HasText("say more") && tt.HasText("until the API settles")
	})
}

func TestTODOScanScope(t *testing.T) {
	hs := newHarness(t, false)
	withBundled(hs)
	a, tt := hs.a, hs.tt
	include, exclude := locales.SearchFilesInclude(), locales.SearchFilesExclude()
	src := hs.path("src")
	if err := os.MkdirAll(filepath.Join(src, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"src/z.go":            "// TODO: keep this source\n",
		"src/generated/a.go":  "// TODO: generated marker\n",
		"src/ignored.test.go": "// TODO: test marker\n",
	} {
		if err := os.WriteFile(hs.path(name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.window.SidebarView = "plugin:todos.list"
	hs.until("the initial TODOs", func() bool { return tt.HasText("keep this source") })
	if tt.HasText(include) || tt.HasText(exclude) {
		t.Fatal("the scan scope should start collapsed")
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("⋯")
	hs.until("the scope fields", func() bool { return tt.HasText(exclude) })
	tt.Key(0, ui.KeyTab)
	tt.Type("src/**")
	tt.Key(0, ui.KeyTab)
	tt.Type("src/generated/**, **/*.test.go")
	for i := range 4000 {
		if err := os.WriteFile(filepath.Join(src, "generated", fmt.Sprintf("%04d.go", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.indexRead.Invalidate()
	tt.Key(0, ui.KeyEnter)
	hs.until("the source past the limit of excluded files", func() bool {
		return tt.HasText("keep this source") && !tt.HasText("say more") &&
			!tt.HasText("generated marker") && !tt.HasText("test marker")
	})

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "other.go"), []byte("// TODO: other workspace marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFolder(other)
	hs.until("the unfiltered other workspace", func() bool { return tt.HasText("other workspace marker") })
	if tt.HasText("keep this source") || tt.HasText(include) {
		t.Fatal("the first workspace's view leaked into the other workspace")
	}
	a.openFolder(hs.root)
	hs.until("the saved scope after returning to the first workspace", func() bool {
		return tt.HasText("keep this source") && !tt.HasText("generated marker") && !tt.HasText("other workspace marker")
	})
}

func TestTODOScanScopeLabelsMatchSearch(t *testing.T) {
	original := i18n.Locale()
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return original }) })
	for _, language := range []string{"en", "ja", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLocaleFunc(func() string { return language })
			hs := newHarness(t, false)
			withBundled(hs)
			hs.a.window.SidebarView = "plugin:todos.list"
			hs.until("the TODOs", func() bool { return hs.tt.HasText("say more") })
			if err := hs.tt.Click("⋯"); err != nil {
				t.Fatal(err)
			}
			hs.until("the same file filters as Search", func() bool {
				return hs.tt.HasText(locales.SearchFilesInclude()) && hs.tt.HasText(locales.SearchFilesExclude())
			})
		})
	}
}

func TestShotPluginSurfaces(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		hs.tt.SetSize(1100, 640)
		h := withBundled(hs)
		a := hs.a
		viewsMu.Lock()
		delete(pluginViews, pluginViewKey{"todos.list", a.root})
		viewsMu.Unlock()
		a.showPluginView("todos.list")
		hs.until("the TODOs", func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			data, _ := json.Marshal(h.views["todos.list"])
			return strings.Contains(string(data), "say more")
		})
		hs.settle()
		if r, ok := hs.tt.Find("the edge case"); ok {
			hs.tt.Move(r.X+r.W/2, r.Y+r.H/2)
		}
		hs.settle()
		hs.shot("plugin-view" + suffix)
		empty, _ := json.Marshal(viewNode{Type: "empty", Icon: "check", Text: "No TODOs in this folder.", Workspace: a.root})
		h.renderView(h.reg.Plugin("todos"), "todos.list", empty)
		hs.settle()
		hs.shot("plugin-view-empty" + suffix)
		viewsMu.Lock()
		delete(pluginViews, pluginViewKey{"todos.list", a.root})
		viewsMu.Unlock()
		hs.settle()
		hs.shot("plugin-view-waits" + suffix)

		if r, ok := hs.tt.Find(h.status["word-count.status"].Text); ok {
			hs.tt.Move(r.X+r.W/2, r.Y+r.H/2)
		}
		hs.settle()
		hs.shot("plugin-status" + suffix)

		a.openPluginPicker([]pickItem{
			{Label: "Format with Prettier", Description: "prettier", Icon: "braces"},
			{Label: "Check Formatting", Description: "prettier", Detail: "Reports the files Prettier would change", Icon: "check"},
			{Label: "Open Settings", Icon: "settings"},
		}, "Run a Prettier command", func(int) {})
		hs.settle()
		hs.shot("plugin-pick" + suffix)
		a.closePluginPicker(-1)
		a.openQuick(">")
		hs.settle()
		hs.shot("palette" + suffix)
		a.closeQuick()
		hs.settle()
	}
}
