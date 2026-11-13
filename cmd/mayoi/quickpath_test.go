package main

import (
	"os"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

func TestQuickPathBlockedTypingAndLatestRequest(t *testing.T) {
	hs := newHarness(t, true)
	hs.settle()
	a := hs.a
	blocked := hs.path("blocked.go")
	latest := hs.path("main.go")
	release := make(chan struct{})
	var calls atomic.Int32
	a.quick.probe.stat = func(name string) (os.FileInfo, error) {
		calls.Add(1)
		if name == blocked {
			<-release
			return os.Stat(latest)
		}
		return os.Stat(name)
	}
	defer func() { close(release); hs.settle() }()
	a.openQuick(blocked)
	hs.tt.Frame()
	if !hs.tt.HasText(locales.QuickPathChecking()) {
		t.Fatal("missing pending feedback")
	}
	// Frames must return while the probe is still blocked. Frame timing
	// budgets are measured separately, without race instrumentation.
	for range 30 {
		hs.tt.Frame()
	}
	// Return while pending must not open an old file or queue an open.
	hs.tt.Key(0, ui.KeyEnter)
	hs.tt.Frame()
	if !a.quick.open || a.quick.path != "" {
		t.Fatal("pending Return opened a file")
	}
	hs.tt.Type("x")
	hs.tt.Frame()
	if a.quick.query != blocked+"x" {
		t.Fatal("typing did not update the pending query")
	}
	for i := range 100 {
		a.quick.query = latest + string(rune('a'+i))
		hs.tt.Frame()
	}
	a.quick.query = latest
	hs.tt.Frame()
	if calls.Load() > 1 {
		t.Fatalf("unbounded workers: %d", calls.Load())
	}
	// Escape remains available even with a blocked system call.
	hs.tt.Key(0, ui.KeyEscape)
	hs.tt.Frame()
	if a.quick.open {
		t.Fatal("Escape did not close pending Quick Open")
	}
}

func TestQuickPathLatestOpensAfterBlockedProbe(t *testing.T) {
	hs := newHarness(t, true)
	hs.settle()
	a := hs.a
	first, latest := hs.path("slow.go"), hs.path("main.go")
	release := make(chan struct{})
	var calls atomic.Int32
	a.quick.probe.stat = func(name string) (os.FileInfo, error) {
		calls.Add(1)
		if name == first {
			<-release
		}
		return os.Stat(latest)
	}
	a.openQuick(first)
	hs.tt.Frame()
	a.quick.query = latest
	hs.tt.Frame()
	close(release)
	hs.settle()
	if a.quick.path != latest || calls.Load() != 2 {
		t.Fatalf("latest path = %q, calls = %d", a.quick.path, calls.Load())
	}
	hs.key(0, ui.KeyEnter)
	if d := a.activeEditor(); a.quick.open || d == nil || d.path != latest {
		t.Fatal("Return did not open the latest path")
	}
}

func TestQuickPathDropsObsoleteResults(t *testing.T) {
	for _, change := range []string{"query", "root", "mode", "close", "reopen", "query-back"} {
		t.Run(change, func(t *testing.T) {
			hs := newHarness(t, true)
			hs.settle()
			a := hs.a
			name := hs.path("main.go")
			release := make(chan struct{})
			var calls atomic.Int32
			a.quick.probe.stat = func(path string) (os.FileInfo, error) {
				if calls.Add(1) == 1 {
					<-release
					return os.Stat(name)
				}
				return nil, os.ErrNotExist
			}
			a.openQuick(name)
			hs.tt.Frame()
			switch change {
			case "query":
				a.quick.query = hs.path("missing.go")
			case "root":
				a.root = t.TempDir()
			case "mode":
				a.quick.query = ">"
			case "close":
				a.closeQuick()
			case "reopen":
				a.closeQuick()
				a.openQuick(name)
			case "query-back":
				a.quick.query = ":2"
				hs.tt.Frame()
				a.quick.query = name
			}
			hs.tt.Frame()
			close(release)
			hs.settle()
			if a.quick.probe.result.name != "" || a.quick.path != "" {
				t.Fatal("obsolete result survived a source change")
			}
			if calls.Load() > 2 {
				t.Fatalf("duplicate probes: %d", calls.Load())
			}
		})
	}
}

func TestQuickPathFailureStates(t *testing.T) {
	for _, err := range []error{os.ErrPermission, os.ErrNotExist} {
		hs := newHarness(t, true)
		hs.a.quick.probe.stat = func(string) (os.FileInfo, error) { return nil, err }
		hs.a.openQuick(hs.path("denied.go"))
		hs.settle()
		msg := locales.QuickPathMissing()
		if err == os.ErrPermission {
			msg = locales.QuickPathError()
		}
		if !hs.tt.HasText(msg) {
			t.Fatalf("missing feedback: %s", msg)
		}
	}
}
