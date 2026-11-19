package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// startServer starts the server of id in root, as a document opening
// does, and waits for it to start or fail.
func startServer(t *testing.T, a *app, root, id string) *lspServer {
	t.Helper()
	p, sc := serverByID(id)
	s := newServer(p, *sc, root, 1)
	lspServers.Lock()
	lspServers.m[s.key] = s
	lspServers.Unlock()
	go s.start(a, nil)
	select {
	case <-s.ready:
	case <-time.After(time.Minute):
		t.Fatalf("%s did not start or fail", id)
	}
	return s
}

// A server of an SSH folder that ends with 127 is looked for on the host,
// in the folder and with its environment: one missing says so, and how to
// install it as the index says; one there, which said 127 itself, quit;
// and one whose host cannot tell in time says that. A check under way
// ends as the server is restarted, and tells nothing.
func TestLanguageServerExit127Remote(t *testing.T) {
	hs, c, dir := remoteHarness(t)
	a := hs.a
	host := remote.HostOf(a.root)
	if _, err := c.Run(nil, `mkdir -p "$1/bin" && printf '#!/bin/sh\nexit 127\n' > "$1/bin/quit127" && chmod +x "$1/bin/quit127"`, dir); err != nil {
		t.Fatal(err)
	}
	serverIndex.Lock()
	oldEntries := serverIndex.entries
	serverIndex.entries = []plugin.IndexEntry{{Name: "fake", Detect: "mayoi-no-such-lsp", Install: "brew install no-such-lsp"},
		{Name: "fake", Detect: "quit127", Install: "brew install quit127"}}
	serverIndex.Unlock()
	oldCheck, oldTimeout := checkProgram, lspCheckTimeout
	t.Cleanup(func() {
		checkProgram, lspCheckTimeout = oldCheck, oldTimeout
		serverIndex.Lock()
		serverIndex.entries = oldEntries
		serverIndex.Unlock()
	})
	fakeServers(hs, map[string]string{"miss": "mayoi-no-such-lsp", "quit": "./bin/quit127"})

	miss := startServer(t, a, a.root, "fake.miss")
	if got, want := miss.failMessage(), locales.LspCouldNotStartInstall("fake.miss",
		locales.LspNotInstalledOn("mayoi-no-such-lsp", host), "brew install no-such-lsp"); got != want {
		t.Fatalf("missing: %q, want %q", got, want)
	}
	quit := startServer(t, a, a.root, "fake.quit")
	if got, want := quit.failMessage(), locales.LspCouldNotStart("fake.quit", locales.LspQuit("exit status 127")); quit.failure != lspFailQuit || got != want {
		t.Fatalf("there: %d %q, want %q", quit.failure, got, want)
	}

	// The program is looked for in the folder, on the PATH it is given.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, tc := range []struct {
		root  string
		env   []string
		name  string
		found bool
	}{
		{a.root, nil, "./bin/quit127", true},
		{remote.Path(host, "/"), nil, "./bin/quit127", false},
		{a.root, nil, "quit127", false},
		{a.root, []string{"PATH=" + dir + "/bin"}, "quit127", true},
		{a.root, []string{"PATH=/nowhere"}, "sh", false},
		{a.root, nil, "sh", true},
	} {
		if found, known := programOnHost(ctx, tc.root, tc.env, nil, tc.name); found != tc.found || !known {
			t.Errorf("%s in %s with %v: found %v, known %v", tc.name, tc.root, tc.env, found, known)
		}
	}

	// A host that does not say in time.
	var calls atomic.Int32
	var gotRoot, gotName string
	checkProgram = func(ctx context.Context, root string, _, _ []string, name string) (bool, bool) {
		calls.Add(1)
		gotRoot, gotName = root, name
		<-ctx.Done()
		return false, false
	}
	lspCheckTimeout = 200 * time.Millisecond
	start := time.Now()
	s := startServer(t, a, a.root, "fake.miss")
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the check took %v", d)
	}
	if gotRoot != a.root || gotName != "mayoi-no-such-lsp" || calls.Load() != 1 {
		t.Fatalf("checked %q in %q, %d times", gotName, gotRoot, calls.Load())
	}
	if got, want := s.failMessage(), locales.LspCouldNotStart("fake.miss", locales.LspExitUnchecked("mayoi-no-such-lsp", host)); s.failure != lspFailUnchecked || got != want {
		t.Fatalf("unchecked: %d %q, want %q", s.failure, got, want)
	}

	// Restarted while its check is under way: the old one is not told
	// of, and the new one's check is its own.
	calls.Store(0)
	entered := make(chan struct{})
	checkProgram = func(ctx context.Context, root string, _, _ []string, name string) (bool, bool) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return false, false
		}
		return false, true
	}
	lspCheckTimeout = time.Minute
	hs.settle()
	fails := a.fails
	p, sc := serverByID("fake.miss")
	old := newServer(p, *sc, a.root, 1)
	lspServers.Lock()
	lspServers.m[old.key] = old
	lspServers.Unlock()
	go old.start(a, nil)
	<-entered
	a.restartServer(old.key)
	next := serverOf(old.key)
	hs.until("the restarted server's failure", func() bool { return next.state() == lspFailed })
	hs.settle()
	if old.state() != lspFailed || next == old || next.failure != lspFailMissing || calls.Load() != 2 {
		t.Fatalf("old %d, next %d, %d checks", old.state(), next.failure, calls.Load())
	}
	if a.fails != fails+1 {
		t.Fatalf("told %d failures, want 1", a.fails-fails)
	}

	// A host that is gone cannot tell.
	c.Disconnect()
	start = time.Now()
	if found, known := programOnHost(ctx, a.root, nil, nil, "sh"); found || known {
		t.Fatalf("offline: found %v, known %v", found, known)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the offline check took %v", d)
	}
}
