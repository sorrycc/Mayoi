package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"mayoi/internal/plugin"
	"mayoi/locales"
)

// fakeServers is a plugin whose servers, fake.<language> for each
// language, run commands.
func fakeServers(hs *harness, commands map[string]string) *pluginHost {
	var langs, servers []any
	for id, command := range commands {
		langs = append(langs, map[string]any{"id": id, "extensions": []string{"." + id}})
		servers = append(servers, map[string]any{"id": "fake." + id, "languages": []string{id}, "command": command, "args": []string{"-test.run=^$"}})
	}
	manifest, _ := json.Marshal(map[string]any{"name": "fake", "contributes": map[string]any{"languages": langs, "languageServers": servers}})
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	// Its processes end before the harness's folders are removed, as
	// Windows asks.
	hs.t.Cleanup(stopServers)
	return h
}

func serverOf(key string) *lspServer {
	lspServers.Lock()
	defer lspServers.Unlock()
	return lspServers.m[key]
}

// A server that quit as it started, past its automatic tries, starts again
// as asked from Show Language Servers: the file already open is told of,
// and a server of another folder is left as it was. Two restarts at once
// start it once; the one it replaces leaves the diagnostics to it.
func TestLanguageServerRestart(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "crash")
	old := lspRetryPause
	lspRetryPause = 0
	t.Cleanup(func() { lspRetryPause = old })
	hs := newHarness(t, false)
	h := fakeServers(hs, map[string]string{"fake": os.Args[0]})
	a := hs.a
	key := serverKey(a.root, "fake.fake")
	open := func(name string) string {
		path := hs.path(name)
		os.WriteFile(path, []byte("so bad\n"), 0o644)
		a.openFile(path, true)
		hs.settle()
		return path
	}
	for i, name := range []string{"a.fake", "b.fake", "c.fake", "d.fake"} {
		open(name)
		want := min(i+1, maxServerStarts)
		hs.until("the failed start", func() bool {
			s := serverOf(key)
			return s != nil && s.starts == want && s.state() == lspFailed
		})
	}
	hs.settle()
	s := serverOf(key)
	if s.failure != lspFailQuit || a.fails != maxServerStarts {
		t.Fatalf("failure %d, told %d times", s.failure, a.fails)
	}
	if got, want := s.failMessage(), locales.LspCouldNotStart("fake.fake", locales.LspQuit("exit status 3")); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}

	// A server of another folder, which the restart leaves alone.
	t.Setenv("MAYOI_FAKE_LSP", "1")
	p, sc := serverByID("fake.fake")
	other := newServer(p, *sc, t.TempDir(), 1)
	lspServers.Lock()
	lspServers.m[other.key] = other
	lspServers.Unlock()
	go other.start(a, nil)
	hs.until("the other folder's server", func() bool { return other.live() != nil })

	a.showLanguageServers()
	if !a.pluginPick.open || len(a.pluginPick.items) != 1 {
		t.Fatalf("listed %+v", a.pluginPick.items)
	}
	if it := a.pluginPick.items[0]; it.Label != "fake.fake" || it.Description != s.stateText() || it.Detail != a.root {
		t.Fatalf("row = %+v", it)
	}
	a.closePluginPicker(0)
	// Asked again at once, it is the same restart.
	first := serverOf(key)
	a.restartServer(key)
	if serverOf(key) != first || !first.asked || first.starts != 1 {
		t.Fatal("a second restart replaced the first")
	}
	path := hs.path("a.fake")
	hs.until("the restarted server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	if serverOf(other.key) != other || other.live() == nil {
		t.Fatal("the other folder's server was touched")
	}

	// Restarted while it runs, the old one is gone before the new starts,
	// and its end clears nothing of the new one's.
	c := first.client
	a.restartServer(key)
	next := serverOf(key)
	hs.until("the next server", func() bool { return next.live() != nil })
	select {
	case <-c.Done():
	default:
		t.Fatal("the old server still runs")
	}
	hs.until("the next server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	time.Sleep(50 * time.Millisecond)
	hs.settle()
	if len(h.diagnosticsOf(path)) != 1 {
		t.Fatal("the old server's end cleared the new one's diagnostics")
	}

	// One that stops by itself clears its diagnostics, and is not running.
	next.client.Close()
	hs.until("the stopped server's diagnostics cleared", func() bool { return len(h.diagnosticsOf(path)) == 0 })
	if next.state() != lspStopped {
		t.Fatalf("state = %d", next.state())
	}
	a.restartServer(key)
	hs.until("the server after the stop", func() bool { return len(h.diagnosticsOf(path)) == 1 })
}

// A program that is missing is told apart from one that does not answer;
// how to install it is said only when the index says so of the plugin.
// One still starting as the app quits is stopped, and not told of.
func TestLanguageServerFailures(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "hang")
	oldTimeout := lspStartTimeout
	lspStartTimeout = time.Second
	serverIndex.Lock()
	oldEntries := serverIndex.entries
	serverIndex.entries = []plugin.IndexEntry{{Name: "fake", Detect: "mayoi-no-such-lsp", Install: "brew install no-such-lsp"}}
	serverIndex.Unlock()
	t.Cleanup(func() {
		lspStartTimeout = oldTimeout
		serverIndex.Lock()
		serverIndex.entries = oldEntries
		serverIndex.Unlock()
	})
	hs := newHarness(t, false)
	fakeServers(hs, map[string]string{"miss": "mayoi-no-such-lsp", "hang": os.Args[0]})
	a := hs.a
	for _, name := range []string{"a.miss", "a.hang"} {
		path := hs.path(name)
		os.WriteFile(path, []byte("x\n"), 0o644)
		a.openFile(path, true)
	}
	miss, hang := serverKey(a.root, "fake.miss"), serverKey(a.root, "fake.hang")
	hs.until("both failed", func() bool {
		m, h := serverOf(miss), serverOf(hang)
		return m != nil && h != nil && m.state() == lspFailed && h.state() == lspFailed
	})
	if got, want := serverOf(miss).failMessage(), locales.LspCouldNotStartInstall("fake.miss",
		locales.LspNotInstalled("mayoi-no-such-lsp"), "brew install no-such-lsp"); got != want {
		t.Fatalf("missing: %q, want %q", got, want)
	}
	if s := serverOf(hang); s.failure != lspFailSilent || s.failMessage() != locales.LspCouldNotStart("fake.hang", locales.LspNoAnswer()) {
		t.Fatalf("silent: %d %q", s.failure, s.failMessage())
	}
	serverIndex.Lock()
	serverIndex.entries = nil
	serverIndex.Unlock()
	if got := serverOf(miss).failMessage(); got != locales.LspCouldNotStart("fake.miss", locales.LspNotInstalled("mayoi-no-such-lsp")) {
		t.Fatalf("without the index: %q", got)
	}

	// Asked to start again, and the app quits as it does.
	lspStartTimeout = time.Minute
	hs.settle()
	fails := a.fails
	a.restartServer(hang)
	s := serverOf(hang)
	start := time.Now()
	stopServers()
	if d := time.Since(start); d > 10*time.Second || s.state() != lspFailed {
		t.Fatalf("stopped in %v, state %d", d, s.state())
	}
	hs.settle()
	if a.fails != fails {
		t.Fatal("a server stopped as the app quit was told of")
	}
}

// A server that exits with 0 as it starts, as a wrapper that found nothing
// to run may, says it quit without an error.
func TestLanguageServerQuitsCleanly(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "quit")
	hs := newHarness(t, false)
	fakeServers(hs, map[string]string{"fake": os.Args[0]})
	a := hs.a
	path := hs.path("a.fake")
	os.WriteFile(path, []byte("x\n"), 0o644)
	a.openFile(path, true)
	key := serverKey(a.root, "fake.fake")
	hs.until("the failed start", func() bool {
		s := serverOf(key)
		return s != nil && s.state() == lspFailed
	})
	hs.settle()
	s := serverOf(key)
	if got, want := s.failMessage(), locales.LspCouldNotStart("fake.fake", locales.LspQuitClean()); s.failure != lspFailQuit || got != want {
		t.Fatalf("failure %d, message %q, want %q", s.failure, got, want)
	}
}
