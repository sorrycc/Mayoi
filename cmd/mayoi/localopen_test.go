package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/internal/vfs"
	"mayoi/locales"
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
	// Let the new active file finish before the older target, so a
	// delayed focus request cannot hide behind the active file's load.
	a.localRead = vfs.ReadFileInfo
	a.openFile(hs.path("main.go"), true)
	active := a.activeTab()
	hs.held++ // the target's read is intentionally blocked
	hs.until("active file ready", func() bool { return !active.ed.unread() })
	release()
	hs.held--
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

// A read that ends soon shows no spinner; a slow one says it reads, and
// the text it brings comes in without a flash.
func TestLocalOpenReadingShowsLate(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			hs := newHarness(t, true)
			hs.a.settings.AutoSave = false
			hs.tt.SetPreferences(ui.Preferences{ReduceMotion: reduced})
			release, _ := blockLocalReads(t, hs.a)
			hs.a.openFile(hs.path("main.go"), true)
			d := hs.a.activeEditor()
			reading := locales.RemoteReading(d.name())
			step(hs.tt)
			if !d.locked() {
				t.Fatal("loading editor accepted input")
			}
			if hs.tt.HasText(reading) {
				t.Fatal("a read just begun shows that it reads")
			}
			hs.tt.Advance(readingShows)
			step(hs.tt)
			if !hs.tt.HasText(reading) {
				t.Fatal("a slow read does not say it reads")
			}
			release()
			hs.until("file ready", func() bool { return !d.unread() })
			step(hs.tt)
			if !d.landAt.IsZero() {
				t.Fatal("the text read lit a line")
			}
			hs.tt.Type("x")
			hs.settle()
			if !d.dirty() {
				t.Fatal("typing did not reach the file read")
			}
		})
	}
}

// Reads stuck on a share that is gone stop holding their places, so a
// file after them still opens.
func TestLocalOpenStuckReadsLetOthersOpen(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.localStuck = time.Millisecond
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	stuck := map[string]bool{hs.path("a.txt"): true, hs.path("b.txt"): true}
	for p := range stuck {
		os.WriteFile(p, []byte("x\n"), 0o600)
	}
	a.localRead = func(path string, limit int64) (fs.FileInfo, []byte, error) {
		if stuck[path] {
			<-gate
		}
		return vfs.ReadFileInfo(path, limit)
	}
	a.openFile(hs.path("a.txt"), true)
	a.openFile(hs.path("b.txt"), true)
	a.openFile(hs.path("main.go"), true)
	d := a.activeEditor()
	// settle would wait for the stuck reads too. Each stuck read lets go
	// of its place as its own timer fires, which on Windows's coarse
	// timers can be after main.go is read in the place the first left.
	for deadline := time.Now().Add(settleTimeout); d.unread() || a.localReads != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			if d.unread() {
				t.Fatal("main.go waits behind the stuck reads")
			}
			t.Fatalf("stuck reads still hold %d places", a.localReads)
		}
		step(hs.tt)
	}
}

// A plugin's workspace.open answers once the file is read, so that the
// plugin finds its text.
func TestPluginOpenAnswersOnceRead(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	release, _ := blockLocalReads(t, a)
	h := newPluginHost(func() []*app { return []*app{a} }, a.post, func() *settings.Settings { return a.settings }, func() {})
	answered := make(chan error, 1)
	go func() {
		_, err := h.Call(nil, 0, "workspace.open", json.RawMessage(`{"path":"main.go"}`))
		answered <- err
	}()
	for range 20 {
		step(hs.tt)
		time.Sleep(time.Millisecond)
	}
	select {
	case <-answered:
		t.Fatal("workspace.open answered before the file was read")
	default:
	}
	release()
	for deadline := time.Now().Add(settleTimeout); ; time.Sleep(time.Millisecond) {
		step(hs.tt)
		select {
		case err := <-answered:
			if d := a.activeEditor(); err != nil || d == nil || d.unread() || d.buf.Len() == 0 {
				t.Fatalf("answered %v with %+v", err, d)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace.open never answered")
		}
	}
}

// failedBeside opens other.txt, then a file that cannot be read in a
// column beside it, and gives the keyboard back to other.txt with an edit
// of its own.
func failedBeside(t *testing.T, name string, data []byte) (*harness, *column, *editor, *editor) {
	t.Helper()
	hs := newHarness(t, true)
	a := hs.a
	other := hs.path("other.txt")
	if err := os.WriteFile(other, []byte("other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.openFile(other, true)
	hs.settle()
	kept := a.activeEditor()
	a.splitRight()
	hs.settle()
	if data != nil {
		if err := os.WriteFile(hs.path(name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.openFile(hs.path(name), true)
	hs.settle()
	failed := a.activeEditor()
	failCol := a.column()
	if failed == nil || failed.sync != syncLoadFailed {
		t.Fatal("file did not fail")
	}
	a.focusColumnBy(-1)
	hs.settle()
	kept.buf.Replace(0, 0, "edited ")
	hs.settle()
	if a.activeEditor() != kept {
		t.Fatal("other column not active")
	}
	return hs, failCol, failed, kept
}

// pressFailure presses on a failure page at a point beside the path it shows.
func pressFailure(t *testing.T, hs *harness, path string, dy float32) {
	t.Helper()
	r, ok := hs.tt.Find(path)
	if !ok {
		t.Fatal("failure page lost its path")
	}
	hs.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2+dy)
	hs.settle()
}

func TestLocalOpenFailurePressActivates(t *testing.T) {
	for _, tc := range []struct {
		name string
		dy   float32
	}{{"text", 0}, {"blank", 80}} {
		t.Run(tc.name, func(t *testing.T) {
			hs, failCol, failed, kept := failedBeside(t, "report.md", nil)
			a := hs.a
			pressFailure(t, hs, failed.path, tc.dy)
			if a.column() != failCol || a.activeEditor() != failed || a.focus != paneEditor {
				t.Fatal("press on failure page did not activate its tab")
			}
			if !hs.tt.HasText(failed.path) || failed.loadErr == nil || !failed.locked() {
				t.Fatal("failure page lost its error or unlocked")
			}
			a.closeActive()
			hs.settle()
			if slices.Contains(a.docs, failed.document) || !slices.Contains(a.docs, kept.document) || kept.buf.String() != "edited other\n" {
				t.Fatal("Close Tab did not close the failed tab alone")
			}
		})
	}
}

func TestLocalOpenFailureRetryActivates(t *testing.T) {
	hs, failCol, failed, _ := failedBeside(t, "report.md", nil)
	a := hs.a
	release, _ := blockLocalReads(t, a)
	os.WriteFile(failed.path, []byte("recovered\n"), 0600)
	if err := hs.tt.Click("Try Again"); err != nil {
		t.Fatal(err)
	}
	step(hs.tt)
	step(hs.tt)
	if a.column() != failCol || a.activeEditor() != failed {
		t.Fatal("Try Again did not activate its tab")
	}
	if a.localReads != 1 {
		t.Fatalf("Try Again read %d times", a.localReads)
	}
	release()
	hs.settle()
	if failed.unread() || failed.buf.String() != "recovered\n" {
		t.Fatal("retry did not recover")
	}
	if !hs.tt.Focused("Editor report.md") || hs.tt.Focused("Editor other.txt") {
		t.Fatal("recovered editor did not take the keyboard")
	}
}

func TestLocalOpenFailureRetryLeftAlone(t *testing.T) {
	hs, failCol, failed, kept := failedBeside(t, "report.md", nil)
	a := hs.a
	release, _ := blockLocalReads(t, a)
	os.WriteFile(failed.path, []byte("recovered\n"), 0600)
	if err := hs.tt.Click("Try Again"); err != nil {
		t.Fatal(err)
	}
	step(hs.tt)
	step(hs.tt)
	if a.column() != failCol {
		t.Fatal("Try Again did not activate its tab")
	}
	// Back to the other column's text, by the pointer, while it reads.
	r, ok := hs.tt.Find("Editor other.txt")
	if !ok {
		t.Fatal("other text not shown")
	}
	hs.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	step(hs.tt)
	step(hs.tt)
	if a.activeEditor() != kept {
		t.Fatal("press on other text did not activate it")
	}
	release()
	hs.settle()
	if failed.unread() {
		t.Fatal("retry did not recover")
	}
	if a.activeEditor() != kept || !hs.tt.Focused("Editor other.txt") || hs.tt.Focused("Editor report.md") {
		t.Fatal("retry that ended took the keyboard from the other column")
	}
}

func TestFailurePageWithoutRetryActivates(t *testing.T) {
	t.Run("binary", func(t *testing.T) {
		hs, failCol, failed, _ := failedBeside(t, "blob.bin", []byte{0, 1, 2, 0, 3})
		if failed.loadErr != errBinary || hs.tt.HasText("Try Again") {
			t.Fatal("binary failure shows Try Again")
		}
		pressFailure(t, hs, failed.path, 40)
		if hs.a.column() != failCol || hs.a.activeEditor() != failed {
			t.Fatal("press on binary failure did not activate its tab")
		}
	})
	t.Run("remote", func(t *testing.T) {
		hs, failCol, failed, _ := failedBeside(t, "report.md", nil)
		failed.path, failed.loadErr = "/ssh:box/srv/report.md", remote.ErrOffline
		hs.settle()
		if hs.tt.HasText("Try Again") || !hs.tt.HasText(locales.RemoteShowsOnce(failed.name(), "box")) {
			t.Fatal("remote failure page not shown")
		}
		pressFailure(t, hs, failed.path, 40)
		if hs.a.column() != failCol || hs.a.activeEditor() != failed {
			t.Fatal("press on remote failure did not activate its tab")
		}
	})
}
