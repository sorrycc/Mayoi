package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"

	bundled "mayoi/plugins"
)

const lintPlugin = `{
  "name": "lint", "main": "main.ts", "activation": ["onLanguage:markdown"],
  "contributes": {
    "formatters": [{"id": "lint.shout", "languages": ["markdown"]}],
    "views": [{"id": "lint.view", "title": "Lint", "icon": "check"}],
    "previews": [{"id": "lint.preview", "pattern": "*.box", "output": "svg"}]
  }
}`

const lintMain = `import { events, languages, views, previews } from "mayoi";
events.on("editor.open", (e) => {
  if (e.language !== "markdown") return;
  languages.setDiagnostics(e.path, [
    { range: { start: { line: 2, character: 2 }, end: { line: 2, character: 7 } }, severity: "warning", message: "small is vague" },
  ]);
});
events.on("editor.didSave", (e) => views.render("lint.view", { type: "text", text: "saved " + e.path.split(/[\\/]/).pop() }));
languages.registerFormatter("lint.shout", async (doc) => (await doc.text()).replace("editor", "EDITOR"));
views.onShow("lint.view", () => views.render("lint.view", {
  type: "stack", children: [{ type: "text", text: "Hello view" }, { type: "list", items: [{ label: "one" }] }, { type: "markdown", text: "# Title\n\nSome *text*." }],
}));
previews.register("lint.preview", (text) =>
  '<svg xmlns="http://www.w3.org/2000/svg" width="' + (text.trim().length * 10) + '" height="20"><rect width="10" height="20"/></svg>');
`

func TestPluginDiagnosticsFormatViewsAndPreviews(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": lintPlugin, "main.ts": lintMain}})
	a := hs.a
	readme := hs.path("README.md")
	a.openFile(readme, true)
	hs.until("the diagnostics", func() bool { return len(h.diagnosticsOf(readme)) == 1 })
	d := a.activeEditor()
	runs := diagRuns(d.document, d.buf.Snapshot(), 0, 10)
	if len(runs) != 1 || runs[0].line != 2 || runs[0].start != 2 || runs[0].end != 7 || runs[0].severity != "warning" {
		t.Fatalf("diagnostic runs = %+v", runs)
	}
	a.showProblems()
	if !a.pluginPick.open || len(a.pluginPick.items) != 1 || a.pluginPick.items[0].Label != "small is vague" {
		t.Fatalf("problems box = %+v", a.pluginPick)
	}
	a.closePluginPicker(0)
	hs.settle()

	// Format on save: the file on disk has the formatter's text.
	a.settings.FormatOnSave = map[string]bool{"markdown": true}
	a.saveDoc(d.document, nil)
	hs.until("the formatted save", func() bool {
		data, _ := os.ReadFile(readme)
		return strings.Contains(string(data), "A small EDITOR.")
	})
	if d.dirty() {
		t.Fatal("dirty after a formatted save")
	}
	// The save is an event.
	hs.until("the didSave render", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		v := h.views["lint.view"]
		return v != nil && v.Text == "saved README.md"
	})

	// The sidebar shows the view, which renders as it shows.
	a.showPluginView("lint.view")
	hs.until("the view", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		v := h.views["lint.view"]
		return v != nil && v.Type == "stack"
	})
	if a.window.SidebarView != "plugin:lint.view" {
		t.Fatalf("sidebar shows %q", a.window.SidebarView)
	}
	hs.settle() // draws it

	// A file of the preview's pattern previews as the plugin's SVG.
	box := hs.path("shape.box")
	os.WriteFile(box, []byte("abcd\n"), 0o644)
	a.openFile(box, true)
	hs.settle()
	bd := a.activeEditor()
	if !canPreview(bd.document) {
		t.Fatal("a .box file has no preview")
	}
	a.togglePreview()
	hs.until("the preview", func() bool { return bd.preview != nil && bd.preview.src != nil })
	if bd.preview.w != 40 {
		t.Fatalf("preview width = %v", bd.preview.w)
	}
}

// TestFormatOnSaveJoins: a save asked for while the formatter runs waits
// for it, so that the file is written once, formatted.
func TestFormatOnSaveJoins(t *testing.T) {
	hs := newHarness(t, false)
	withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": lintPlugin, "main.ts": lintMain}})
	a := hs.a
	readme := hs.path("README.md")
	a.openFile(readme, true)
	hs.settle()
	d := a.activeEditor()
	a.settings.FormatOnSave = map[string]bool{"markdown": true}
	d.edit(d.sel, textEdit{0, 0, "editor note\n"})
	saved := 0
	a.saveDoc(d.document, func() { saved++ })
	a.saveDoc(d.document, func() { saved++ })
	if data, _ := os.ReadFile(readme); strings.Contains(string(data), "editor note") {
		t.Fatal("written before the formatter")
	}
	hs.until("the formatted save", func() bool { return saved == 2 })
	if data, _ := os.ReadFile(readme); !strings.HasPrefix(string(data), "EDITOR note\n") {
		t.Fatalf("on disk: %q", data)
	}
}

func TestBundledPlugins(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, nil)
	h.reg.Dirs.Bundled = bundled.FS
	h.reg.Load()
	// They start off, until the settings turn them on.
	for _, p := range h.reg.Plugins() {
		if p.Bundled && !p.Stats().Disabled {
			t.Errorf("%s is on by default", p.Name)
		}
	}
	turnOnBundled(hs, h)
	names := []string{}
	for _, p := range h.reg.Plugins() {
		if !p.Bundled {
			continue
		}
		names = append(names, p.Name)
		if st := p.Stats(); st.Err != "" {
			t.Errorf("%s: %s", p.Name, st.Err)
		}
	}
	entries, _ := fs.ReadDir(bundled.FS, ".")
	if len(names) != len(entries) || len(names) < 2 {
		t.Fatalf("bundled = %v", names)
	}
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	defer func() {
		if t.Failed() {
			for _, p := range h.reg.Plugins() {
				t.Logf("%s %+v %+v", p.Name, p.Stats(), p.Logs())
			}
			data, _ := json.Marshal(h.views["todos.list"])
			t.Logf("view: %s index: %v", data, a.index)
		}
	}()
	hs.until("word count", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.status["word-count.status"].Text == "4 words"
	})
	a.showPluginView("todos.list")
	hs.until("the TODOs", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		v := h.views["todos.list"]
		return v != nil && v.Type == "empty"
	})
	// A TODO, saved, is found.
	os.WriteFile(hs.path("main.go"), []byte("package main\n\n// TODO: say more\nfunc main() {}\n"), 0o644)
	a.indexRead.Invalidate()
	hs.settle()
	go h.reg.Run("todos.refresh", nil)
	hs.until("the TODO", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		data, _ := json.Marshal(h.views["todos.list"])
		return strings.Contains(string(data), "say more")
	})
}
