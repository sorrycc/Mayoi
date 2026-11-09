package main

import "testing"

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
