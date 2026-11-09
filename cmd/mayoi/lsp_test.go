package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"mayoi/internal/lsp/lsptest"
)

// The test binary is a fake language server when asked, for the plugins'
// language servers.
func init() {
	if os.Getenv("MAYOI_FAKE_LSP") == "1" {
		lsptest.Serve(os.Stdin, os.Stdout)
		os.Exit(0)
	}
}

func TestLanguageServer(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	manifest, _ := json.Marshal(map[string]any{
		"name": "fake",
		"contributes": map[string]any{
			"languages":       []any{map[string]any{"id": "fake", "extensions": []string{".fake"}}},
			"languageServers": []any{map[string]any{"id": "fake.server", "languages": []string{"fake"}, "command": os.Args[0], "args": []string{"-test.run=^$"}}},
		},
	})
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	a := hs.a
	path := hs.path("a.fake")
	os.WriteFile(path, []byte("fine\nso bad\n"), 0o644)
	a.openFile(path, true)
	// Started as a process, which a busy machine takes its time with.
	hs.untilLong("the server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	dg := h.diagnosticsOf(path)[0]
	if dg.Severity != "error" || dg.Message != "bad word" || dg.Range.Start.Line != 1 || dg.Range.Start.Character != 3 {
		t.Fatalf("diagnostic = %+v", dg)
	}
	d := a.activeEditor()
	// An edit goes to the server, which finds nothing bad now.
	a.queueEdit(d, func(d *editor) { d.edit([2]int{0, 0}, textEdit{8, 11, "good"}) })
	hs.untilLong("the edit's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 0 })

	a.showHover()
	hs.until("the hover", func() bool { return a.hover != nil })
	if !strings.Contains(a.hover.tree.Text, "**hello**") {
		t.Fatalf("hover = %q", a.hover.tree.Text)
	}
	a.hover = nil

	hs.place(d, 2, 2)
	a.complete()
	hs.until("the completions", func() bool { return a.pluginPick.open })
	if a.pluginPick.items[0].Label != "println" {
		t.Fatalf("completions = %+v", a.pluginPick.items)
	}
	a.closePluginPicker(0)
	hs.settle()
	if got := d.buf.String(); !strings.HasPrefix(got, "println()ne\n") {
		t.Fatalf("after the completion: %q", got)
	}

	// Format Document, with no plugin formatter, asks the server.
	a.formatActive()
	hs.until("the formatting", func() bool { return strings.HasPrefix(d.buf.String(), "// formatted\n") })
}
