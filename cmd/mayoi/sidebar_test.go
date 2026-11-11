package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The sidebar's title shows whole: beside the Explorer's buttons, which go
// on the folder's row, and in source control, whose title gives way to
// its short form in a narrow sidebar.
func TestSidebarTitleFits(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	whole := func(title string) {
		t.Helper()
		var w float32
		ui.NewTester(func(c *ui.Context) {
			w, _ = c.MeasureText(0, ui.Span{Text: title, Size: 11, Weight: 600})
		}, 100, 100).Frame()
		r, ok := tt.Find(title)
		if !ok {
			t.Fatalf("no title %q: %q", title, tt.Texts())
		}
		// Less the title's left padding.
		if r.W-6 < w {
			t.Errorf("the title %q has %.0f points of the %.0f it takes", title, r.W-6, w)
		}
	}
	hs.gitRepo()
	tt.Move(100, 150)
	hs.settle()
	whole("EXPLORER")
	if _, ok := tt.Find("New File…"); !ok {
		t.Error("the Explorer's buttons do not show under the pointer")
	}
	a.run("view.scm")
	hs.settleGit()
	// A wider interface font, as DejaVu Sans, has the whole title in a
	// wider sidebar.
	if !tt.HasText("SOURCE CONTROL") {
		a.window.SidebarWidth = 320
		hs.settle()
	}
	whole("SOURCE CONTROL")
	a.window.SidebarWidth = 180
	hs.settle()
	whole("GIT")
}

// The guides of a row go down the middle of the arrows of the folders
// above it, as the outline places them treeStep apart.
func TestTreeGuidesFollowTheArrows(t *testing.T) {
	const arrow, gap, left = 16, 4, 10
	// arrowAt is where the arrow of a row depth deep starts: its
	// indentation and a gap after the row's start, on deeper rows.
	arrowAt := func(depth int) float32 {
		if depth == 0 {
			return left
		}
		return left + float32(depth)*treeStep + gap
	}
	for depth := 1; depth <= 5; depth++ {
		content := arrowAt(depth) + arrow + gap
		for k, x := range treeGuideX(content, depth, arrow, gap) {
			if want := arrowAt(k) + arrow/2; x != want {
				t.Errorf("depth %d, guide %d at %v, want %v", depth, k, x, want)
			}
		}
	}
}
