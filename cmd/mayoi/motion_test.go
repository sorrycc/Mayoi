package main

import (
	"fmt"
	"os"
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

func TestAppearingFades(t *testing.T) {
	var ap appearing
	keys := []string{"a", "b"}
	key := func(i int) string { return keys[i] }
	now := time.Unix(1000, 0)
	ap.note(len(keys), key, now, false)
	if k := ap.shown("a", now); k != 1 {
		t.Errorf("a row there as the list first shows fades in: %v", k)
	}
	keys = []string{"a", "c", "b"}
	now = now.Add(16 * time.Millisecond)
	ap.note(len(keys), key, now, false)
	if k := ap.shown("c", now); k != 0 {
		t.Errorf("a row just come shows at %v, not 0", k)
	}
	if k := ap.shown("b", now); k != 1 {
		t.Errorf("a row there before fades again: %v", k)
	}
	if k := ap.shown("c", now.Add(showFor/2)); k <= 0 || k >= 1 {
		t.Errorf("halfway, the row shows at %v", k)
	}
	if k := ap.shown("c", now.Add(showFor)); k != 1 {
		t.Errorf("once in, the row shows at %v", k)
	}
	// Rows of another list, noted fresh, show at once.
	keys = []string{"x", "y"}
	ap.note(len(keys), key, now.Add(time.Second), true)
	if k := ap.shown("x", now.Add(time.Second)); k != 1 {
		t.Errorf("a row of a list noted fresh shows at %v", k)
	}
}

func TestScmGroupOpensQuietly(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	os.WriteFile(hs.path("new.txt"), []byte("new\n"), 0o644)
	hs.a.run("view.scm")
	hs.settleGit()
	g := hs.a.repository()
	key := ""
	for _, r := range g.scmRows() {
		if r.file() {
			key = r.key
		}
	}
	if key == "" {
		t.Fatal("no file's row in source control")
	}
	group := 0
	for _, r := range g.scmRows() {
		if r.header {
			group = int(r.group)
			break
		}
	}
	g.scm.collapsed[group] = true
	step(hs.tt)
	g.scm.collapsed[group] = false
	step(hs.tt)
	if k := g.scm.appear.shown(key, hs.tt.Now()); k >= 1 {
		t.Fatalf("the rows of the group opened show at %v", k)
	}
	for range 60 {
		hs.tt.Advance(50 * time.Millisecond)
		hs.tt.Frame()
	}
	if k := g.scm.appear.shown(key, hs.tt.Now()); k != 1 {
		t.Errorf("once in, the rows show at %v", k)
	}
	if !quiet(hs) {
		t.Error("frames are drawn after the group's rows came in")
	}
}

func TestSearchFileUnfoldsQuietly(t *testing.T) {
	hs := newHarness(t, true)
	hs.searchFor("main")
	s := &hs.a.inFiles
	if len(s.files) != 1 {
		t.Fatalf("results in %d files", len(s.files))
	}
	rel := s.files[0].rel
	for _, folded := range []bool{true, false} {
		if err := hs.tt.Click("main.go"); err != nil {
			t.Fatal(err)
		}
		step(hs.tt)
		if s.collapsed[rel] != folded {
			t.Fatalf("folded %v, want %v", s.collapsed[rel], folded)
		}
	}
	if _, ok := s.unfolded[rel]; !ok || !hs.tt.FramePending() {
		t.Fatal("the matches of the file unfolded do not fade in")
	}
	// The chevron turns and the row eases back from the press as well.
	for range 20 {
		hs.tt.Advance(50 * time.Millisecond)
		hs.tt.Frame()
	}
	if _, ok := s.unfolded[rel]; ok {
		t.Error("the file stays unfolding once its matches came in")
	}
	if !quiet(hs) {
		t.Error("frames are drawn after the matches came in")
	}
}
