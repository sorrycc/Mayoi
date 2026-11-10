package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"

	bundled "mayoi/plugins"
)

// withBundled starts the plugins Mayoi comes with over a folder that has
// TODOs in it, a Markdown file open.
func withBundled(hs *harness) *pluginHost {
	os.WriteFile(hs.path("main.go"), []byte("package main\n\n// TODO: say more\n// FIXME: the edge case\nfunc main() {}\n"), 0o644)
	os.WriteFile(hs.path("notes.go"), []byte("package main\n\n// HACK: until the API settles\n"), 0o644)
	h := withPlugins(hs, nil)
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

	h.renderView(h.reg.Plugin("todos"), "todos.list", json.RawMessage(`{"type": "empty", "icon": "check", "text": "No TODOs in this folder."}`))
	hs.settle()
	if !hs.tt.HasText("No TODOs in this folder.") {
		t.Errorf("no empty state: %q", hs.tt.Texts())
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
		delete(pluginViews, "todos.list")
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
		h.renderView(h.reg.Plugin("todos"), "todos.list", json.RawMessage(`{"type": "empty", "icon": "check", "text": "No TODOs in this folder."}`))
		hs.settle()
		hs.shot("plugin-view-empty" + suffix)
		viewsMu.Lock()
		delete(pluginViews, "todos.list")
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
