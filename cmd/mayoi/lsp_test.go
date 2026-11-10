package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// A server whose command is missing starts once it is installed, as a
// document of its language opens a while after; it tries only so often,
// until its plugin is reloaded, and tells of each failure once.
func TestLanguageServerRetry(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAYOI_FAKE_LSP", "1")
	old := lspRetryPause
	lspRetryPause = 0
	t.Cleanup(func() { lspRetryPause = old })
	manifest, _ := json.Marshal(map[string]any{
		"name": "fake",
		"contributes": map[string]any{
			"languages":       []any{map[string]any{"id": "fake", "extensions": []string{".fake"}}},
			"languageServers": []any{map[string]any{"id": "fake.server", "languages": []string{"fake"}, "command": "mayoi-fake-lsp", "args": []string{"-test.run=^$"}}},
		},
	})
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	a := hs.a
	server := func() *lspServer {
		lspServers.Lock()
		defer lspServers.Unlock()
		return lspServers.m[a.root+"\x00fake.server"]
	}
	failed := func(starts int) {
		t.Helper()
		hs.untilLong("the failed start", func() bool {
			s := server()
			return s != nil && s.starts == starts && s.state() == lspFailed
		})
		hs.settle()
		if a.fails != starts {
			t.Fatalf("after %d failed starts, %d failures told", starts, a.fails)
		}
	}
	open := func(name string) string {
		path := hs.path(name)
		os.WriteFile(path, []byte("so bad\n"), 0o644)
		a.openFile(path, true)
		hs.settle()
		return path
	}
	open("a.fake")
	failed(1)
	// Editing does not start it again, nor does asking it for completions.
	d := a.activeEditor()
	a.queueEdit(d, func(d *editor) { d.edit([2]int{0, 0}, textEdit{0, 0, "x"}) })
	a.complete()
	hs.settle()
	if s := server(); s.starts != 1 {
		t.Fatalf("starts after an edit = %d", s.starts)
	}
	open("b.fake")
	failed(2)
	open("c.fake")
	failed(3)
	// No more, however many open.
	open("d.fake")
	hs.settle()
	if s := server(); s.starts != maxServerStarts || a.fails != maxServerStarts {
		t.Fatalf("past the cap: starts %d, failures told %d", s.starts, a.fails)
	}

	// Installed, then the plugin reloaded: the open documents' server starts.
	if err := os.Symlink(os.Args[0], filepath.Join(bin, "mayoi-fake-lsp")); err != nil {
		t.Fatal(err)
	}
	h.reloadAll()
	path := hs.path("a.fake")
	hs.untilLong("the server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	if s := server(); s.starts != 1 || s.state() != lspRunning {
		t.Fatalf("after the reload: starts %d, state %d", s.starts, s.state())
	}
}

// A failed server starts again as a document of its language opens, the
// command installed meanwhile, without a reload.
func TestLanguageServerRetryOnOpen(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAYOI_FAKE_LSP", "1")
	old := lspRetryPause
	lspRetryPause = 0
	t.Cleanup(func() { lspRetryPause = old })
	manifest, _ := json.Marshal(map[string]any{
		"name": "fake",
		"contributes": map[string]any{
			"languages":       []any{map[string]any{"id": "fake", "extensions": []string{".fake"}}},
			"languageServers": []any{map[string]any{"id": "fake.server", "languages": []string{"fake"}, "command": "mayoi-fake-lsp", "args": []string{"-test.run=^$"}}},
		},
	})
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	a := hs.a
	os.WriteFile(hs.path("a.fake"), []byte("so bad\n"), 0o644)
	a.openFile(hs.path("a.fake"), true)
	hs.untilLong("the failed start", func() bool { return a.fails == 1 })
	if err := os.Symlink(os.Args[0], filepath.Join(bin, "mayoi-fake-lsp")); err != nil {
		t.Fatal(err)
	}
	path := hs.path("b.fake")
	os.WriteFile(path, []byte("so bad\n"), 0o644)
	a.openFile(path, true)
	hs.untilLong("the server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
}
