package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/agents"
	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// withPlugins starts the plugins of files, a folder each by name, for the
// harness's window, as the app starts them for its own.
func withPlugins(hs *harness, files map[string]map[string]string) *pluginHost {
	t := hs.t
	dir := filepath.Join(t.(*testing.T).TempDir(), "plugins")
	for name, fs := range files {
		for f, body := range fs {
			p := filepath.Join(dir, name, f)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	a := hs.a
	h := newPluginHost(func() []*app { return []*app{a} }, a.post, func() *settings.Settings { return a.settings }, func() {})
	h.reg = plugin.New(plugin.Dirs{Plugins: dir, Storage: filepath.Join(filepath.Dir(dir), "storage")}, h)
	h.reg.Version = "dev"
	old, oldLexer := plugins, highlight.LexerName
	plugins, highlight.LexerName = h, pluginLexer
	h.syncPrefs()
	h.reg.Load()
	t.Cleanup(func() {
		h.reg.Close()
		plugins, highlight.LexerName = old, oldLexer
		agents.Use(nil)
	})
	hs.settle()
	return h
}

const upperPlugin = `{
  "name": "upper", "main": "main.ts",
  "contributes": {
    "commands": [{"id": "upper.selection", "title": "Upper Case Selection"}, {"id": "upper.taken", "title": "Taken"}],
    "keybindings": [{"key": "ctrl+alt+u", "command": "upper.selection"}, {"key": "ctrl+p", "mac": "cmd+p", "command": "upper.taken"}],
    "statusItems": [{"id": "upper.status"}]
  }
}`

const upperMain = `import { commands, editor, statusItem } from "mayoi";
commands.register("upper.selection", async () => {
  const ed = await editor.active();
  if (!ed) return;
  const s = ed.selection;
  const range = { start: s.anchor, end: s.active };
  const text = await ed.document.text(range);
  await ed.edit([{ range, text: text.toUpperCase() }]);
  await statusItem("upper.status").set({ text: "upped " + text.length });
});
commands.register("upper.taken", () => {});
`

func TestPluginCommandEditsTheSelection(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"upper": {"plugin.json": upperPlugin, "main.ts": upperMain}})
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	d := a.activeEditor()
	// "small" of "A small editor."
	start := strings.Index(d.buf.String(), "small")
	hs.place(d, start, start+5)

	var cmd *command
	for _, c := range a.commands() {
		if c.id == "upper.selection" {
			cmd = &c
		}
		if c.id == "upper.taken" && c.accel != "" {
			t.Errorf("a plugin took ⌘P: %q", c.accel)
		}
	}
	if cmd == nil || cmd.label != "Upper Case Selection" || cmd.accel == "" {
		t.Fatalf("the plugin's command: %+v", cmd)
	}
	if g := commandGroup(locales.In("en"), "upper.selection"); g != "upper" {
		t.Errorf("the palette groups it under %q", g)
	}
	a.run("upper.selection")
	hs.until("the edit", func() bool { return strings.Contains(d.buf.String(), "A SMALL editor.") })
	// One step of undo takes it back.
	d.undo()
	hs.settle()
	if !strings.Contains(d.buf.String(), "A small editor.") {
		t.Fatalf("after undo: %q", d.buf.String())
	}
	hs.until("the status item", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.status["upper.status"].Text == "upped 5"
	})
	// Skipped for ⌘P, offered in the settings window.
	skipped := a.skippedKeys()
	if len(skipped) != 1 || skipped[0].Command != "upper.taken" {
		t.Fatalf("skipped keys: %+v", skipped)
	}
}

// The switch of the Plugins page turns a plugin off, which stops it and
// takes its status item away, and on, which starts it again.
func TestPluginSwitchStopsAndStarts(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"starter": {
		"plugin.json": `{"name": "starter", "main": "main.ts", "activation": ["onStartup"], "contributes": {"statusItems": [{"id": "starter.status"}]}}`,
		"main.ts":     `import { statusItem } from "mayoi"; statusItem("starter.status").set({ text: "on" });`,
	}})
	h.reg.Activate("onStartup")
	p := h.reg.Plugin("starter")
	status := func() (pluginStatus, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		st, ok := h.status["starter.status"]
		return st, ok
	}
	running := func() bool { st, _ := status(); return p.Started() && st.Text == "on" }
	hs.until("the plugin starting", running)
	w := &prefsWindow{m: &mayoi{s: hs.a.settings}}
	enabled := func() string { return string(hs.a.settings.Prefs.Plugins["starter"]) }

	w.setPluginSetting("starter", "enabled", false, true)
	if got := enabled(); got != `{"enabled":false}` {
		t.Fatalf("turned off, the settings are %q", got)
	}
	if p.Started() || !p.Stats().Disabled {
		t.Fatalf("turned off, started %v, stats %+v", p.Started(), p.Stats())
	}
	if _, ok := status(); ok {
		t.Errorf("turned off, its status item stays")
	}

	w.setPluginSetting("starter", "enabled", true, true)
	if got := enabled(); got != "" {
		t.Fatalf("turned on, the settings are %q", got)
	}
	hs.until("the plugin starting again", running)
}
