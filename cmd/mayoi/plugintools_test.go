package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"mayoi/internal/plugin"
)

// The log keeps its last maxPluginLog lines in a ring, numbered on as
// they come, and clearing it keeps the numbers going.
func TestPluginLogRing(t *testing.T) {
	var r logRing
	for i := range maxPluginLog + 5 {
		r.add(plugin.LogLine{Message: fmt.Sprint(i)})
	}
	if r.first() != 5 || r.next != maxPluginLog+5 {
		t.Fatalf("first %d next %d", r.first(), r.next)
	}
	if got := r.at(5).Message; got != "5" {
		t.Errorf("oldest %q", got)
	}
	if got := r.at(r.next - 1).Message; got != fmt.Sprint(maxPluginLog+4) {
		t.Errorf("newest %q", got)
	}
	r.clear()
	if r.first() != r.next {
		t.Errorf("cleared, first %d next %d", r.first(), r.next)
	}
	r.add(plugin.LogLine{Message: "after"})
	if r.first() != maxPluginLog+5 || r.at(r.first()).Message != "after" {
		t.Errorf("after clearing, first %d %q", r.first(), r.at(r.first()).Message)
	}
	if !containsFold("Started in 3ms", "STARTED") || containsFold("abc", "abcd") || !containsFold("Écrit", "écrit") {
		t.Error("containsFold")
	}
}

// The Plugin Log shows what the plugins logged as it comes, filtered by
// plugin, level and text, and follows its end.
func TestPluginLogTab(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{
		"upper": {"plugin.json": upperPlugin, "main.ts": upperMain},
		"lint":  {"plugin.json": lintPlugin, "main.ts": lintMain},
	})
	a := hs.a
	up, lint := h.reg.Plugin("upper"), h.reg.Plugin("lint")
	up.Log("info", "warming the caches")
	lint.Log("warn", "small is vague")
	lint.Log("error", "the rule broke")
	a.showPluginLog()
	hs.settle()
	tab := a.activeTab()
	if tab == nil || tab.tool == nil || tab.tool.kind != toolLog {
		t.Fatalf("no log tab: %+v", tab)
	}
	for _, s := range []string{"warming the caches", "small is vague", "the rule broke"} {
		if !hs.tt.HasText(s) {
			t.Errorf("no %q in %q", s, hs.tt.Texts())
		}
	}
	l := &tab.tool.log
	l.plugin = "lint"
	hs.settle()
	if hs.tt.HasText("warming the caches") || !hs.tt.HasText("the rule broke") {
		t.Errorf("filtered by plugin: %q", hs.tt.Texts())
	}
	l.levels &^= logWarn
	hs.settle()
	if hs.tt.HasText("small is vague") {
		t.Error("the warning shows with warnings off")
	}
	l.plugin, l.levels, l.query = "", logAllLevels, "CACHES"
	hs.settle()
	if len(l.shown) != 1 || !hs.tt.HasText("warming the caches") {
		t.Errorf("searched: %d lines, %q", len(l.shown), hs.tt.Texts())
	}
	l.query = "nothing like it"
	hs.settle()
	if !hs.tt.HasText("Clear Filters") {
		t.Errorf("no empty state of the filters: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click("Clear Filters"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if l.query != "" || len(l.shown) < 3 {
		t.Errorf("the filters stayed: %q, %d lines", l.query, len(l.shown))
	}

	// A line that comes shows, and stands out for a moment.
	up.Log("info", "a fresh line")
	hs.settle()
	if !hs.tt.HasText("a fresh line") {
		t.Errorf("the new line does not show: %q", hs.tt.Texts())
	}

	// Show Log of a plugin opens the same tab, for it.
	a.showPluginLogOf(lint)
	hs.settle()
	if a.activeTab() != tab || l.plugin != "lint" {
		t.Errorf("another tab, or of %q", l.plugin)
	}

	if err := hs.tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.tt.HasText("No plugin has logged anything yet.") {
		t.Errorf("cleared: %q", hs.tt.Texts())
	}
}

// A log of many lines draws the lines in view, not all of them, and its
// lines cost nothing as they come but their place in the ring.
func TestPluginLogDrawsTheLinesInView(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"upper": {"plugin.json": upperPlugin, "main.ts": upperMain}})
	a := hs.a
	up := h.reg.Plugin("upper")
	for i := range 3000 {
		up.Log("info", fmt.Sprintf("line %04d", i))
	}
	a.showPluginLog()
	hs.settle()
	if !hs.tt.HasText("line 2999") {
		t.Fatalf("the end does not show: %q", hs.tt.Texts())
	}
	if hs.tt.HasText("line 0000") || hs.tt.HasText("line 1500") {
		t.Error("lines out of view are drawn")
	}
	l := &a.activeTab().tool.log
	allocs := testing.AllocsPerRun(200, func() {
		h.Logged(up, plugin.LogLine{Plugin: "upper", Level: "info", Message: "x"})
	})
	if allocs > 1 {
		t.Errorf("a line costs %v allocations", allocs)
	}
	h.mu.Lock()
	h.log.add(plugin.LogLine{Plugin: "upper", Level: "info", Message: "y"})
	h.mu.Unlock()
	l.sync(h, time.Now())
	if n := len(l.shown); n < 3000 {
		t.Errorf("%d lines shown", n)
	}
}

// Running Plugins lists the plugins, how each is doing, sorted by the
// column clicked, and reads them again every second while it shows.
func TestRunningPluginsTab(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{
		"upper":   {"plugin.json": upperPlugin, "main.ts": upperMain},
		"palette": {"plugin.json": `{"name": "palette", "version": "0.2.0"}`},
	})
	a := hs.a
	if _, err := h.reg.Run("upper.taken", nil); err != nil {
		t.Fatal(err)
	}
	a.showRunningPlugins()
	hs.settle()
	tab := a.activeTab()
	if tab == nil || tab.tool == nil || tab.tool.kind != toolRunning {
		t.Fatalf("no table: %+v", tab)
	}
	r := &tab.tool.run
	if len(r.rows) != 2 || r.active != 1 {
		t.Fatalf("rows %d, %d running", len(r.rows), r.active)
	}
	if r.rows[0].pl.Name != "palette" || !hs.tt.HasText("Running") || !hs.tt.HasText("No code") {
		t.Errorf("first %q, texts %q", r.rows[0].pl.Name, hs.tt.Texts())
	}
	if err := hs.tt.Click("State"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if r.sort != runState || r.rows[0].pl.Name != "upper" {
		t.Errorf("sorted by %v, first %q", r.sort, r.rows[0].pl.Name)
	}
	if err := hs.tt.Click("Plugin"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click("Plugin"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !r.desc || r.rows[0].pl.Name != "upper" {
		t.Errorf("by name the other way: desc %v, first %q", r.desc, r.rows[0].pl.Name)
	}
	at := r.at
	hs.settle()
	if !r.at.After(at) {
		t.Error("not read again a second later")
	}

	// Restart stops the plugin and starts it again.
	h.reg.Restart("upper")
	hs.until("upper again", func() bool { return h.reg.Plugin("upper").Started() })
	if st := h.reg.Plugin("upper").Stats(); st.StartedAt.IsZero() || st.Crashes != 0 {
		t.Errorf("restarted: %+v", st)
	}

	// Details open the settings window's.
	a.post(func() { a.host.showSettings(prefsPlugins, pluginDetailKey+"upper") })
	hs.settle()
	if got := hs.h.settingsShown; len(got) == 0 || got[len(got)-1] != "plugins:plugin:upper" {
		t.Errorf("settings shown %q", got)
	}
}

// The plugin views' folders and choice are each window's own, and two
// lists of a view without keys keep their rows apart.
func TestPluginViewStateIsTheWindows(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": lintPlugin, "main.ts": lintMain}})
	a := hs.a
	a.showPluginView("lint.view")
	hs.until("the view", func() bool { return hs.tt.HasText("Hello view") })
	h.renderView(h.reg.Plugin("lint"), "lint.view", json.RawMessage(`{"type": "stack", "children": [
		{"type": "list", "items": [{"label": "first one"}, {"label": "first two"}]},
		{"type": "list", "items": [{"label": "second one"}, {"label": "second two"}]}]}`))
	hs.settle()
	if err := hs.tt.Click("first one"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	pv := a.viewStates["lint.view"]
	viewsMu.Lock()
	rv := pluginViews["lint.view"]
	viewsMu.Unlock()
	one, other := rv.tree.Children[0].Items[0], rv.tree.Children[1].Items[0]
	if pv.sel != one.path || one.path == other.path {
		t.Errorf("chose %q; the rows are at %q and %q", pv.sel, one.path, other.path)
	}
	b := &app{}
	if bv := b.viewState("lint.view", rv); bv == pv || bv.sel != "" {
		t.Errorf("another window shares the choice: %q", bv.sel)
	}
}

func TestShotPluginTools(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		hs.tt.SetSize(1180, 680)
		h := withBundled(hs)
		a := hs.a
		for _, pl := range h.reg.Plugins() {
			h.reg.Restart(pl.Name)
		}
		hs.until("the plugins", func() bool { return h.reg.Plugin("todos").Started() && h.reg.Plugin("word-count").Started() })
		word := h.reg.Plugin("word-count")
		for i, m := range []string{"counted 214 words in README.md", "settings changed: wordsPerMinute = 240", "watching *.md", "could not read notes.go: permission denied"} {
			lvl := []string{"info", "debug", "info", "warn"}[i]
			word.Log(lvl, m)
		}
		if todos := h.reg.Plugin("todos"); todos != nil {
			todos.Log("info", "found 3 TODOs in 2 files")
			todos.Log("error", "TypeError: cannot read properties of undefined (reading 'line')\n    at scan (main.ts:42:17)")
		}
		a.showRunningPlugins()
		hs.settle()
		hs.tt.Advance(time.Second)
		hs.settle()
		if r, ok := hs.tt.Find("Word Count"); ok {
			hs.tt.Move(r.X+r.W/2, r.Y+r.H/2)
		}
		hs.settle()
		hs.shot("plugin-running" + suffix)
		a.showPluginLog()
		hs.settle()
		hs.shot("plugin-log" + suffix)
		l := &a.activeTab().tool.log
		for i := range 60 {
			word.Log("debug", fmt.Sprintf("tick %d", i))
		}
		l.list.ScrollTo(0, 0)
		hs.settle()
		word.Log("info", "a fresh line")
		hs.settle()
		hs.shot("plugin-log-scrolled" + suffix)
		l.query = "nothing like it"
		hs.settle()
		hs.shot("plugin-log-nomatch" + suffix)
		l.query = ""
		a.closeTab(a.activeTab(), nil)
		hs.settle()
	}
}
