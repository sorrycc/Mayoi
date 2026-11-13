package main

import (
	"fmt"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/vfs"
)

func blockLocalReads(t *testing.T, a *app) (func(), *atomic.Int32) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	started := &atomic.Int32{}
	a.localRead = func(path string, limit int64) (fs.FileInfo, []byte, error) {
		started.Add(1)
		<-gate
		return vfs.ReadFileInfo(path, limit)
	}
	return release, started
}

func TestLocalOpenBoundedAndClosed(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	release, started := blockLocalReads(t, a)
	for i := range 8 {
		path := hs.path(fmt.Sprintf("queued-%d.txt", i))
		if err := os.WriteFile(path, []byte("first\nsecond target\n"), 0600); err != nil {
			t.Fatal(err)
		}
		a.openFile(path, true)
	}
	if a.localReads != localReadLimit {
		t.Fatalf("running = %d", a.localReads)
	}
	// Drop running and queued tabs alike. Their completions cannot revive
	// them; two fresh requests wait for the occupied slots to drain.
	for len(a.editorTabs()) > 0 {
		a.dropTab(a.editorTabs()[0])
	}
	a.openFile(hs.path("queued-0.txt"), true)
	a.openFile(hs.path("README.md"), true)
	active := a.activeTab()
	step(hs.tt)
	if started.Load() > localReadLimit {
		t.Fatalf("started %d reads", started.Load())
	}
	release()
	hs.settle()
	if a.localReads != 0 || started.Load() != 4 {
		t.Fatalf("running=%d started=%d", a.localReads, started.Load())
	}
	if len(a.docs) != 2 || a.activeTab() != active {
		t.Fatal("stale completion reopened or focused a tab")
	}
}

func TestLocalOpenTargetsAndSharedBuffer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AutoSave = false
	release, _ := blockLocalReads(t, a)
	path := hs.path("target.txt")
	os.WriteFile(path, []byte("one\ntwo target\nthree\n"), 0600)
	a.previewFile(path)
	first := a.activeEditor()
	if !a.activeTab().transient || !first.locked() {
		t.Fatal("missing locked preview")
	}
	a.openFile(path, true) // pin the existing preview
	a.goToLine(1, 1)
	a.goToLine(2, 5)
	a.openFileToSide(path)
	second := a.activeEditor()
	a.goToLine(3, 2)
	if a.activeTab().transient || first.document != second.document || a.localReads != 1 {
		t.Fatal("deduplication or pinning changed")
	}
	a.openFile(hs.path("main.go"), true)
	active := a.activeTab()
	release()
	hs.settle()
	if a.activeTab() != active {
		t.Fatal("delayed target stole focus")
	}
	selection := func(ed *editor) [2]int {
		if ed.pending != nil {
			return *ed.pending
		}
		return ed.sel
	}
	if selection(first) != [2]int{8, 8} || selection(second) != [2]int{16, 16} {
		t.Fatalf("targets: %v %v", first.pending, second.pending)
	}
	first.edit([2]int{}, textEdit{0, 0, "changed "})
	text := first.buf.String()
	a.openFile(path, true)
	hs.settle()
	if first.buf.String() != text || !first.dirty() {
		t.Fatal("reopen replaced modified buffer")
	}
}

func TestLocalOpenFailureRetry(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	path := hs.path("missing.txt")
	a.openFile(path, true)
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.sync != syncLoadFailed || d.loadErr == nil || !hs.tt.HasText(path) {
		t.Fatal("failure lost context")
	}
	err := d.loadErr
	release, _ := blockLocalReads(t, a)
	os.WriteFile(path, []byte("recovered\n"), 0600)
	if err := hs.tt.Click("Try Again"); err != nil {
		t.Fatal(err)
	}
	step(hs.tt)
	if d.loadErr != err || !d.locked() {
		t.Fatal("retry discarded error or unlocked buffer")
	}
	a.loadFile(d.document)
	if a.localReads != 1 {
		t.Fatal("duplicate retry")
	}
	release()
	hs.settle()
	if d.unread() || d.loadErr != nil || d.buf.String() != "recovered\n" {
		t.Fatal("retry did not recover")
	}
}

func TestLocalOpenRejectsStaleProject(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			hs := newHarness(t, true)
			a := hs.a
			release, _ := blockLocalReads(t, a)
			a.openFile(hs.path("main.go"), true)
			d := a.activeEditor()
			if shutdown {
				a.shutdown()
			} else {
				a.root = hs.path("other-project")
			}
			release()
			hs.settle()
			if d.buf.Len() != 0 || a.localReads != 0 {
				t.Fatal("stale project result applied or slot leaked")
			}
		})
	}
}

func TestLocalOpenBlockedFrameBudget(t *testing.T) {
	if os.Getenv("MAYOI_BUDGETS") == "" {
		t.Skip("MAYOI_BUDGETS is not set")
	}
	hs := newHarness(t, true)
	a := hs.a
	release, _ := blockLocalReads(t, a)
	a.openFile(hs.path("main.go"), true)
	median := medianFrame(60, func() { step(hs.tt) }, nil)
	t.Logf("blocked-read frame median: %s", median)
	if median > 3*time.Millisecond {
		t.Fatalf("blocked-read frame exceeds 3ms: %s", median)
	}
	release()
	hs.settle()
}

func TestLocalOpenReadyFeedback(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			hs := newHarness(t, true)
			hs.a.settings.AutoSave = false
			hs.tt.SetPreferences(ui.Preferences{ReduceMotion: reduced})
			release, _ := blockLocalReads(t, hs.a)
			hs.a.openFile(hs.path("main.go"), true)
			d := hs.a.activeEditor()
			step(hs.tt)
			if !d.locked() {
				t.Fatal("loading editor accepted input")
			}
			release()
			hs.until("file ready", func() bool { return !d.unread() })
			step(hs.tt)
			if d.landAt.IsZero() || d.landDuration != motionShow {
				t.Fatal("ready state did not use the tokenized landing feedback")
			}
			hs.tt.Type("x")
			hs.settle()
			if !d.dirty() {
				t.Fatal("ready feedback delayed typing")
			}
		})
	}
}
