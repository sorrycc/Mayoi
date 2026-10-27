package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// quiet reports whether the window asks for no frame a moment after the
// last, once what moves came to rest.
func quiet(hs *harness) bool {
	for range 3 {
		step(hs.tt)
	}
	hs.tt.Advance(16 * time.Millisecond)
	return !hs.tt.FramePending()
}

func TestTreeOpeningFades(t *testing.T) {
	var o treeOpening
	var open ui.Selection[string]
	now := time.Unix(1000, 0)
	open.Add("/w/restored")
	o.note(&open, now)
	if k := o.shown("/w/restored", now); k != 1 {
		t.Errorf("a folder open as the tree first shows fades in: %v", k)
	}
	open.Add("/w/a")
	now = now.Add(16 * time.Millisecond)
	o.note(&open, now)
	if k := o.shown("/w/a", now); k != 0 {
		t.Errorf("the rows of a folder just opened show at %v, not 0", k)
	}
	if k := o.shown("/w/restored", now); k != 1 {
		t.Errorf("the rows of a folder open before fade again: %v", k)
	}
	mid := now.Add(showFor / 2)
	o.note(&open, mid)
	if k := o.shown("/w/a", mid); k <= 0 || k >= 1 {
		t.Errorf("halfway, the rows show at %v", k)
	}
	end := now.Add(showFor)
	o.note(&open, end)
	if k := o.shown("/w/a", end); k != 1 {
		t.Errorf("once in, the rows show at %v", k)
	}
	// Closed and opened again, it fades in again.
	open.Remove("/w/a")
	o.note(&open, end.Add(time.Second))
	open.Add("/w/a")
	again := end.Add(2 * time.Second)
	o.note(&open, again)
	if k := o.shown("/w/a", again); k != 0 {
		t.Errorf("opened again, the rows show at %v", k)
	}
}

func TestTreeFolderOpensQuietly(t *testing.T) {
	hs := newHarness(t, true)
	dir := hs.path("internal")
	hs.a.files.Open.Add(dir)
	step(hs.tt)
	if k := hs.a.treeOpening.shown(dir, hs.tt.Now()); k >= 1 {
		t.Fatalf("the rows of the folder opened show at %v", k)
	}
	hs.settle()
	if !quiet(hs) {
		t.Error("frames are drawn after the folder's rows came in")
	}
}

func TestJumpLightsLine(t *testing.T) {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	hs, d := editHarness(t, "jump.txt", b.String())
	hs.a.goToLine(300, 0)
	step(hs.tt)
	if d.landAt.IsZero() || d.landLine != 299 {
		t.Fatalf("the jump lit line %d at %v, not line 299", d.landLine, d.landAt)
	}
	if !hs.tt.FramePending() {
		t.Error("the lit line does not fade")
	}
	hs.tt.Advance(landFor)
	step(hs.tt)
	if !quiet(hs) {
		t.Error("frames are drawn after the lit line faded")
	}
	// Moving the caret lights nothing.
	at := d.landAt
	hs.place(d, 0, 0)
	if d.landAt != at {
		t.Error("a caret placed lit its line")
	}
}

func TestPanesComeInQuietly(t *testing.T) {
	hs := newHarness(t, true)
	t.Cleanup(hs.a.closeTerminals)
	for range 2 {
		hs.a.run("view.sidebar")
		hs.settle()
	}
	hs.a.run("view.terminal")
	hs.settle()
	if !quiet(hs) {
		t.Error("frames are drawn after the sidebar and the panel came in")
	}
}

// TestScopesNearAndFar checks that the scopes found from the lines near
// a line, as typing reads them, are those found from all the lines the
// scan goes over, for blocks short and longer than those lines.
func TestScopesNearAndFar(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nfunc long() {\n\tif x {\n")
	for i := range scopeProbe + 300 {
		fmt.Fprintf(&b, "\t\tcall(%d)\n", i)
	}
	b.WriteString("\t}\n}\n\nfunc short() {\n\tfor {\n\n\t\tstep()\n\t}\n}\n")
	text := b.String()
	snap := ui.NewTextBuffer(text).Snapshot()
	d := &editor{}
	n := snap.LineCount()
	for _, line := range []int{4, 100, scopeProbe + 200, n - 4, n - 5, n - 3} {
		d.scopes = scopeCache{}
		d.win = nil
		if got, want := d.scopesOf(snap, line), scopes(snap, line); !slices.Equal(got, want) {
			t.Errorf("scopes of line %d: %v, want %v", line, got, want)
		}
	}
}
