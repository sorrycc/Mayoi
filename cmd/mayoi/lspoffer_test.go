package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mayoi/internal/plugin"
)

// zigEntry is the plugin of the index the offers' tests offer.
var zigEntry = plugin.IndexEntry{Name: "zig-lang", DisplayName: "Zig", Repository: "https://example.com/zig.git",
	Languages: []string{"zigx"}, Detect: "mayoi-zigx-ls.exe", Install: "brew install zigx"}

// offerHarness is a harness with a plugin of the language zigx, which has
// no server, and an index of plugins that fetch reads; the program of its
// server is looked for in bin. open opens a file of zigx there.
type offerHarness struct {
	*harness
	bin   string
	reads atomic.Int32
	open  func(name string) string
}

func newOfferHarness(t *testing.T, fetch func() ([]plugin.IndexEntry, error)) *offerHarness {
	oh := &offerHarness{bin: t.TempDir()}
	t.Setenv("PATH", oh.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	reset := func() {
		serverIndex.Lock()
		serverIndex.entries, serverIndex.at, serverIndex.failedAt = nil, time.Time{}, time.Time{}
		serverIndex.Unlock()
		serverChecks.Lock()
		clear(serverChecks.m)
		serverChecks.Unlock()
	}
	reset()
	old, oldInstall, oldRecheck := fetchServerIndex, installServer, serverRecheck
	t.Cleanup(func() {
		fetchServerIndex, installServer, serverRecheck = old, oldInstall, oldRecheck
		reset()
	})
	fetchServerIndex = func(context.Context, *http.Client) ([]plugin.IndexEntry, error) {
		oh.reads.Add(1)
		return fetch()
	}
	manifest := `{"name": "zigx", "contributes": {"languages": [{"id": "zigx", "extensions": [".zigx"]}]}}`
	oh.harness = newHarness(t, false)
	withPlugins(oh.harness, map[string]map[string]string{"zigx": {"plugin.json": manifest}})
	oh.open = func(name string) string {
		path := oh.path(name)
		os.WriteFile(path, []byte("so bad\n"), 0o644)
		oh.a.openFile(path, true)
		oh.settle()
		return path
	}
	return oh
}

// install puts the program of zigEntry's server on the path.
func (oh *offerHarness) install() {
	os.WriteFile(filepath.Join(oh.bin, zigEntry.Detect), []byte("#!/bin/sh\n"), 0o755)
}

// checked waits for the check of zigx's server to end, and reports
// whether a file of it opening now would look again.
func (oh *offerHarness) checked() bool {
	var again bool
	oh.until("the check", func() bool {
		serverChecks.Lock()
		defer serverChecks.Unlock()
		until, seen := serverChecks.m[oh.a.root+"\x00zigx"]
		again = !seen
		return !seen || !until.IsZero() || slices.Contains(oh.a.settings.ServerOffers, "zigx")
	})
	oh.settle()
	return again
}

// offers counts the offers of zigEntry's plugin shown.
func (oh *offerHarness) offers() int {
	n := 0
	for _, s := range oh.tt.Texts() {
		n += strings.Count(s, "Install the Zig plugin")
	}
	return n
}

// A file of a language with no server offers the plugin of the index
// that brings one, once, when its program is installed; when it is not,
// it says how to install it.
func TestOfferLanguageServer(t *testing.T) {
	index := func() ([]plugin.IndexEntry, error) { return []plugin.IndexEntry{zigEntry}, nil }
	oh := newOfferHarness(t, index)
	oh.open("a.zigx")
	oh.until("the missing server told", func() bool { return oh.tt.HasText("brew install zigx") })
	if oh.offers() != 0 {
		t.Fatal("offered the plugin of a server not installed")
	}

	// Installed: offered, once.
	oh = newOfferHarness(t, index)
	oh.install()
	oh.open("b.zigx")
	oh.until("the plugin offered", func() bool { return oh.offers() == 1 })
	if !oh.tt.HasText("Install") || !slices.Contains(oh.a.settings.ServerOffers, "zigx") {
		t.Fatalf("the offer: %q, kept %v", oh.tt.Texts(), oh.a.settings.ServerOffers)
	}
	serverChecks.Lock()
	clear(serverChecks.m)
	serverChecks.Unlock()
	oh.open("c.zigx")
	oh.checked()
	if n := oh.offers(); n != 1 {
		t.Fatalf("offered %d times", n)
	}
}

// An index that could not be read is not asked for again for a while,
// however many files open, and the offer comes as one opens after it is.
func TestOfferLanguageServerIndexFails(t *testing.T) {
	var offline atomic.Bool
	offline.Store(true)
	oh := newOfferHarness(t, func() ([]plugin.IndexEntry, error) {
		if offline.Load() {
			return nil, errors.New("offline")
		}
		return []plugin.IndexEntry{zigEntry}, nil
	})
	oh.install()
	oh.open("a.zigx")
	if !oh.checked() {
		t.Fatal("an index not read keeps the language from being looked at again")
	}
	oh.open("b.zigx")
	oh.checked()
	if n := oh.reads.Load(); n != 1 || oh.offers() != 0 || len(oh.a.settings.ServerOffers) != 0 {
		t.Fatalf("offline: %d reads, %d offers, kept %v", n, oh.offers(), oh.a.settings.ServerOffers)
	}

	// Online, a while after.
	offline.Store(false)
	serverIndex.Lock()
	serverIndex.failedAt = time.Now().Add(-serverIndexRetry)
	serverIndex.Unlock()
	oh.open("c.zigx")
	oh.until("the plugin offered", func() bool { return oh.offers() == 1 })
	if n := oh.reads.Load(); n != 2 {
		t.Fatalf("%d reads", n)
	}
}

// A server's program installed after Mayoi said it was missing is
// offered for, as a file of its language opens a while after, without the
// index read again.
func TestOfferLanguageServerInstalledSince(t *testing.T) {
	oh := newOfferHarness(t, func() ([]plugin.IndexEntry, error) { return []plugin.IndexEntry{zigEntry}, nil })
	serverRecheck = time.Hour
	oh.open("a.zigx")
	oh.until("the missing server told", func() bool { return oh.tt.HasText("brew install zigx") })
	oh.install()
	// Not looked for again so soon.
	oh.open("b.zigx")
	oh.checked()
	if oh.offers() != 0 {
		t.Fatal("looked for the program again at once")
	}
	serverChecks.Lock()
	serverChecks.m[oh.a.root+"\x00zigx"] = time.Now().Add(-time.Second)
	serverChecks.Unlock()
	oh.open("c.zigx")
	oh.until("the plugin offered", func() bool { return oh.offers() == 1 })
	if n := oh.reads.Load(); n != 1 {
		t.Fatalf("%d reads", n)
	}
}

// A plugin that could not install can be tried again, and is not kept
// as offered; installed, its server starts for the file open.
func TestOfferLanguageServerInstallFails(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	oh := newOfferHarness(t, func() ([]plugin.IndexEntry, error) { return []plugin.IndexEntry{zigEntry}, nil })
	// Stopped before the folder it runs in is removed, as Windows asks.
	t.Cleanup(stopServers)
	oh.install()
	var fail atomic.Bool
	fail.Store(true)
	installServer = func(context.Context, string, string, string) (*plugin.Manifest, error) {
		if fail.Load() {
			return nil, errors.New("could not reach example.com\nmore")
		}
		manifest, _ := json.Marshal(map[string]any{"name": "zig-lang", "contributes": map[string]any{
			"languageServers": []any{map[string]any{"id": "zig-lang.server", "languages": []string{"zigx"}, "command": os.Args[0], "args": []string{"-test.run=^$"}}},
		}})
		dir := filepath.Join(plugins.reg.Dirs.Plugins, "zig-lang")
		os.MkdirAll(dir, 0o755)
		return nil, os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o644)
	}
	path := oh.open("a.zigx")
	// The file read, as it is long before an install ends.
	oh.until("the file's change told", func() bool { return len(oh.a.pluginSeen.changed) == 0 })
	oh.until("the plugin offered", func() bool { return slices.Contains(oh.a.settings.ServerOffers, "zigx") })
	// As its button does.
	oh.a.installServerPlugin(zigEntry, "zigx")
	oh.until("the failure", func() bool { return oh.tt.HasText("Could not install zig-lang: could not reach example.com") })
	if !oh.tt.HasText("Try Again") || slices.Contains(oh.a.settings.ServerOffers, "zigx") {
		t.Fatalf("failed: %q, kept %v", oh.tt.Texts(), oh.a.settings.ServerOffers)
	}

	fail.Store(false)
	oh.a.installServerPlugin(zigEntry, "zigx")
	oh.until("the server's diagnostics", func() bool { return len(plugins.diagnosticsOf(path)) == 1 })
}
