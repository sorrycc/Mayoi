package main

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mayoi/internal/remote"
)

func TestHCMachineName(t *testing.T) {
	for host, want := range map[string]string{
		"vps":                "vps",
		"me@Box.example.com": "box-example-com",
		"me@box:2222":        "box-2222",
		"[::1]:22":           "1-22",
		"dev_box":            "dev-box",
	} {
		if got := hcMachineName(host); got != want {
			t.Errorf("%q: %q, want %q", host, got, want)
		}
	}
}

// A host is hc's machine by the name Mayoi gives it, or by its ssh
// target for one added by hand; a host with a port goes through ssh -p.
func TestHCMachine(t *testing.T) {
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		return []byte(`{"machines":[{"name":"rohan","target":"me@rohan.lan"},{"name":"box-2222","target":"ssh -p 2222 me@box"}]}`), nil
	})
	for host, want := range map[string]string{"me@rohan.lan": "rohan", "me@box:2222": "box-2222", "me@box": "", "other": ""} {
		if got, ok := hcMachine(host); got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", host, got, ok, want)
		}
	}
	f.answer(func([]string) ([]byte, error) { return nil, nil })
	hcAddMachine("me@box:2222")
	hcAddMachine("vps")
	add := f.ran("machine")
	want := [][]string{
		{"machine", "add", "--json", "--install", "--name", "box-2222", "--via", "ssh -p 2222 me@box"},
		{"machine", "add", "--json", "--install", "--name", "vps", "vps"},
	}
	if got := add[len(add)-2:]; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("ran %q", got)
	}
}

// Only a failure of hc that asks for a password suggests logging in;
// one that could not copy hc there suggests installing it.
func TestHCAddFailure(t *testing.T) {
	login := hcAddFailure("vps", &hcErr{"needs_login", "on vps: it asks for a password"})
	install := hcAddFailure("mini", &hcErr{"install_failed", "on mini: this hc does not run there"})
	other := hcAddFailure("vps", errors.New("ssh: connect to host vps: Connection refused"))
	if !strings.Contains(login, "hc machine login vps") {
		t.Errorf("login: %q", login)
	}
	if strings.Contains(install, "hc machine login") || !strings.Contains(install, "Install hc on mini") {
		t.Errorf("install: %q", install)
	}
	if strings.Contains(other, "hc machine login") || !strings.Contains(other, "Connection refused") {
		t.Errorf("other: %q", other)
	}
}

// A terminal of a host hc doesn't know uses ssh, and the first asks once
// whether to add the host; adding it says so. Once added, a terminal of
// it starts on it, its address the machine's.
func TestHCRemote(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(func() { clear(hcAsked) })
	known := false
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		switch {
		case args[0] == "machine" && args[1] == "ls" && known:
			return []byte(`{"machines":[{"name":"vps","target":"vps"}]}`), nil
		case args[0] == "machine" && args[1] == "ls":
			return []byte(`{"machines":[]}`), nil
		case args[0] == "run":
			return []byte(`{"address":"t2"}`), nil
		}
		return []byte(`{}`), nil
	})
	if _, ok := hcMachine("vps"); ok {
		t.Fatal("a machine hc doesn't know")
	}
	a.hcOffer("vps")
	if a.confirm == nil {
		t.Fatal("not asked")
	}
	add := a.confirm.run
	a.confirm = nil
	if a.hcOffer("vps"); a.confirm != nil {
		t.Error("asked again")
	}
	add()
	hs.settle()
	if got := f.ran("machine"); !slices.ContainsFunc(got, func(c []string) bool { return c[1] == "add" }) {
		t.Errorf("ran %q", got)
	}
	if !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, "vps was added") }) {
		t.Errorf("no toast: %q", hs.tt.Texts())
	}
	known = true
	if m, ok := hcMachine("vps"); !ok || m != "vps" {
		t.Fatalf("machine %q", m)
	}
	if testing.Short() {
		return
	}
	attached := fakeAttach(t)
	t.Cleanup(a.closeTerminals)
	s := hcSession(t, a, "")
	s.dir, s.hcStarting = remote.Path("vps", "/srv/app"), true
	a.hcBegin(s)
	hs.settle()
	run := f.ran("run")
	if s.hc != "vps/t2" || s.hcStarting || !slices.Contains(run[0], "--on") || !slices.Contains(run[0], "/srv/app") {
		t.Errorf("address %q, ran %q", s.hc, run)
	}
	if !slices.Equal(*attached, []string{"vps/t2"}) {
		t.Errorf("attached %q", *attached)
	}
}

// Adding a host that fails says how to log in to it.
func TestHCAddFails(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, func([]string) ([]byte, error) { return nil, errors.New("needs a password") })
	fails := a.fails
	a.hcAdd("me@box:2222")
	hs.settle()
	if a.fails != fails+1 {
		t.Errorf("%d errors", a.fails-fails)
	}
}

// A terminal of another machine that hc can't reach says the connection
// went, and waits: once hc says its shell is gone, it closes.
func TestHCLost(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	old := hcRetry
	hcRetry = 10 * time.Millisecond
	t.Cleanup(func() { hcRetry = old })
	var gone atomic.Bool
	useFakeHC(t, func(args []string) ([]byte, error) {
		if gone.Load() {
			return nil, &hcErr{"not_found", "no terminal"}
		}
		return nil, &hcErr{"unreachable", "vps is not connected"}
	})
	s := hcSession(t, a, "vps/t2")
	s.dir = remote.Path("vps", "/srv/app")
	s.attached = time.Now().Add(-time.Minute)
	a.terminalExited(s, s.term, 0)
	// It looks again and again, which settle would wait out: frames
	// rather.
	until := func(done func() bool) {
		for deadline := time.Now().Add(2 * time.Second); !done() && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			step(hs.tt)
		}
	}
	until(func() bool { return s.lost })
	if !slices.Contains(a.terms, s) || !s.lost || !strings.Contains(s.term.Text(), "vps") {
		t.Fatalf("open %v, lost %v, text %q", slices.Contains(a.terms, s), s.lost, s.term.Text())
	}
	gone.Store(true)
	until(func() bool { return !slices.Contains(a.terms, s) })
	if slices.Contains(a.terms, s) {
		t.Error("a terminal whose shell is gone stays")
	}
}

// Starting again in a remote folder, a terminal of the host's machine
// attaches again in the folder its shell is in, as a path of the host.
func TestHCRemoteTerms(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	old := hcLookFor
	hcLookFor = 200 * time.Millisecond
	t.Cleanup(func() { hcLookFor = old })
	release := make(chan struct{})
	useFakeHC(t, func(args []string) ([]byte, error) {
		if args[0] == "ls" {
			return []byte(`{"terminals":[{"address":"t1","cwd":"/tmp","state":"live"},{"address":"vps/t2","cwd":"/srv/app","state":"live"}]}`), nil
		}
		switch args[2] {
		case "vps/t2":
			return []byte(`{"state":"live","cwd":"/srv/app"}`), nil
		case "vps/t4":
			return nil, &hcErr{"unreachable", "vps is not connected"}
		case "vps/t5":
			<-release
		}
		return nil, &hcErr{"not_found", "no terminal"}
	})
	root := a.root
	a.root = remote.Path("vps", "/srv")
	t.Cleanup(func() { a.root = root })
	began := time.Now()
	live := a.hcTerms([]string{"t1", "vps/t2", "vps/t3", "vps/t4", "vps/t5"})
	if took := time.Since(began); took > time.Second {
		t.Errorf("waited %v for a machine that doesn't answer", took)
	}
	// t3 ended; t4's machine is away and t5's doesn't answer, so they may
	// run on there, and attach to wait for it.
	want := map[string]string{"t1": "/tmp", "vps/t2": remote.Path("vps", "/srv/app"), "vps/t4": a.root, "vps/t5": a.root}
	if !maps.Equal(live, want) {
		t.Errorf("live %q, want %q", live, want)
	}
	// The look that didn't answer ends before hc is itself again.
	close(release)
	hs.settle()
}

// A terminal that waited for its machine says for a moment, once it
// attaches again, that its shell runs in herdr-core; one attaching
// again without having waited doesn't.
func TestHCReady(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	fakeAttach(t)
	s := hcSession(t, a, "vps/t2")
	a.hcReattach(s)
	if hcJustReady(s) {
		t.Error("a terminal that didn't wait says it is ready")
	}
	s.lost = true
	if termWait(s) == "" || hcJustReady(s) {
		t.Errorf("waiting: pill %q, ready %v", termWait(s), hcJustReady(s))
	}
	a.restartTerminals()
	if s.lost || !hcJustReady(s) {
		t.Errorf("back: lost %v, ready %v", s.lost, hcJustReady(s))
	}
	s.hcReady = time.Now().Add(-hcReadyFor)
	if hcJustReady(s) {
		t.Error("it says it is ready for good")
	}
	hs.settle()
}

// A terminal's badge and pill move only as it begins to wait: one that
// waited long stays still, so that the window doesn't paint all the
// while.
func TestWaitedLong(t *testing.T) {
	hs := newHarness(t, true)
	s := &session{lost: true}
	hs.a.beginWait(s)
	if waitedLong(s) {
		t.Error("a terminal that just began to wait waited long")
	}
	s.waitSince = time.Now().Add(-pulseFor)
	if !waitedLong(s) {
		t.Error("a terminal that waited pulseFor didn't wait long")
	}
}
