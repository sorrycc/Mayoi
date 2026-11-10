package main

// A state that changes moves nothing else: a tab's title stays where it
// was, and so do the tabs beside it and what is clicked on them, as the
// pointer comes over a tab or its file gets unsaved changes. A title that
// shifts as the state does makes a person lose their place, and a target
// that moves under the pointer is missed.

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// layoutOf returns where each of names shows, failing for one missing.
func (hs *harness) layoutOf(names ...string) map[string]ui.Rect {
	hs.t.Helper()
	out := map[string]ui.Rect{}
	for _, n := range names {
		r, ok := hs.tt.Find(n)
		if !ok {
			hs.t.Fatalf("nothing shows %q: %q", n, hs.tt.Texts())
		}
		out[n] = r
	}
	return out
}

// sameLayout fails for each of want that moved or changed size in got.
func sameLayout(t *testing.T, state string, want, got map[string]ui.Rect) {
	t.Helper()
	for n, w := range want {
		if g := got[n]; g != w {
			t.Errorf("%s: %q moved from %+v to %+v", state, n, w, g)
		}
	}
}

func TestTabTitlesHoldStill(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.AutoSave = false
	os.WriteFile(hs.path("first-tab.txt"), []byte("one\n"), 0o644)
	os.WriteFile(hs.path("second-tab.txt"), []byte("two\n"), 0o644)
	a.toggleSidebar()
	a.openFile(hs.path("first-tab.txt"), true)
	hs.settle()
	a.openFile(hs.path("second-tab.txt"), true)
	hs.settle()
	hs.tt.Move(0, float32(featH))
	hs.settle()

	// The crosses mark each tab's end: the title bar shows the name too.
	names := []string{"Close first-tab.txt", "Close second-tab.txt"}
	rest := hs.layoutOf(names...)

	// The pointer comes over the first tab: its cross shows, nothing moves.
	x, y := hs.tabCenter("first-tab.txt")
	hs.tt.Move(x, y)
	hs.settle()
	sameLayout(t, "hovered", rest, hs.layoutOf(names...))

	// The file of the second gets unsaved changes: its dot shows where the
	// cross was, and nothing moves.
	hs.tt.Move(0, float32(featH))
	hs.settle()
	hs.insert("x")
	if !a.activeEditor().dirty() {
		t.Fatal("typing left the file clean")
	}
	sameLayout(t, "unsaved", rest, hs.layoutOf(names...))

	// Choosing the other tab moves the keyboard, not the tabs.
	a.openFile(hs.path("first-tab.txt"), true)
	hs.settle()
	sameLayout(t, "chosen", rest, hs.layoutOf(names...))
}

// The caret's line and column in the status bar keep their width while
// their digits change but not their count, so that what is beside them
// stays put as the caret moves.
func TestStatusPositionHoldsItsWidth(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	os.WriteFile(hs.path("digits.txt"), []byte("1111111111111111\n"), 0o644)
	a.openFile(hs.path("digits.txt"), true)
	hs.settle()
	d := a.activeEditor()
	var widths []float32
	for _, col := range []int{1, 7, 8} { // Col 2, Col 8, Col 9
		hs.place(d, col, col)
		pos := locales.StatusPos(1, col+1)
		r, ok := hs.tt.Find(pos)
		if !ok {
			t.Fatalf("no %q in the status bar: %q", pos, hs.tt.Texts())
		}
		widths = append(widths, r.W)
	}
	if widths[0] != widths[1] || widths[1] != widths[2] {
		t.Errorf("the caret's position changes width with its digits: %v", widths)
	}
}
