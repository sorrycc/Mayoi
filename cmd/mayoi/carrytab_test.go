package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTakeTabFromAnotherProject(t *testing.T) {
	src, dst := newHarness(t, false), newHarness(t, false)
	from, a := src.a, dst.a
	path := src.path("README.md")

	// A clean file moves: it opens here and its tab leaves the other.
	from.openFile(path, true)
	tab := from.column().tab()
	col := a.fileColumn()
	a.takeTab(from, tab, col, len(col.tabs), false)
	if col.tab() == nil || col.tab().path() != path {
		t.Fatal("the tab did not open in the project it was dropped in")
	}
	if c, _ := from.locate(tab); c != nil {
		t.Fatal("a moved tab stayed in its project")
	}
	a.dropTab(col.tab())

	// Changes not yet saved come along, still unsaved.
	from.openFile(path, true)
	tab = from.column().tab()
	tab.ed.buf.Replace(0, 0, "edited ")
	want := tab.ed.buf.String()
	a.takeTab(from, tab, col, len(col.tabs), true)
	got := col.tab().ed
	if got.buf.String() != want || !got.dirty() {
		t.Fatalf("the changes did not come along: dirty %v, %q", got.dirty(), got.buf.String())
	}
	if c, _ := from.locate(tab); c == nil {
		t.Fatal("a copied tab left its project")
	}

	// Open here already, with its own changes: the other's stay with it.
	got.buf.Replace(0, 0, "mine ")
	notes := len(a.notes)
	a.takeTab(from, tab, col, len(col.tabs), false)
	if c, _ := from.locate(tab); c == nil {
		t.Fatal("a tab whose changes could not come along left its project")
	}
	if len(a.notes) != notes+1 {
		t.Fatal("no note said the changes stayed")
	}

	// A terminal stays where it is.
	if carriable(&colTab{term: &session{}}) {
		t.Fatal("a terminal tab can go to another project")
	}
}

// railHost is a project's host that knows the other projects of its
// rail, which a fake host does not.
type railHost struct {
	*fakeHost
	d *desktop
}

func (h railHost) projectOf(t *colTab) *app { return h.d.projectOf(t) }

// A tab dragged onto the square of another project rests there until the
// project shows, then goes on with the pointer to drop in a column there.
func TestCarryTabOverRail(t *testing.T) {
	src, dst := workspaceFixture(t), filepath.Join(t.TempDir(), "carrydst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	f, hs, tt := railFixture(t, src, dst)
	from, a := hs[0].a, hs[1].a
	a.host = railHost{a.host.(*fakeHost), hs[1]}
	path := filepath.Join(src, "README.md")
	from.openFile(path, true)
	tab := from.column().tab()
	step(tt)
	ed, ok := tt.Find("Editor README.md")
	if !ok {
		t.Fatal("no editor of the file")
	}
	square, ok := tt.Find("carrydst")
	if !ok {
		t.Fatal("no square of the other project")
	}
	// The tab, in the strip at the top of the column, above the editor.
	x0, y0 := ed.X+30, ed.Y-tabStripH
	x1, y1 := square.X+square.W/2, square.Y+square.H/2
	tt.Press(x0, y0)
	step(tt)
	for i := 1; i <= 4; i++ {
		k := float32(i) / 4
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
		step(tt)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.active != hs[1] {
		if time.Now().After(deadline) {
			t.Fatal("the other project did not show as the tab rested on its square")
		}
		time.Sleep(20 * time.Millisecond)
		step(tt)
	}
	// On to its column, which shows where the tab would drop.
	x2, y2 := float32(700), float32(400)
	for i := 1; i <= 4; i++ {
		k := float32(i) / 4
		tt.Move(x1+(x2-x1)*k, y1+(y2-y1)*k)
		step(tt)
	}
	if a.carried != tab {
		t.Fatal("the project shown does not see the tab carried in")
	}
	tt.Release(x2, y2)
	for range 3 {
		step(tt)
	}
	if a.fileColumn().showingFile(path) < 0 {
		t.Fatal("the tab did not open in the column it was dropped on")
	}
	if c, _ := from.locate(tab); c != nil {
		t.Fatal("the tab stayed in its project")
	}
}
