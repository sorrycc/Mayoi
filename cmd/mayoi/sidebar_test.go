package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// The tab of source control shows its whole name in a panel wide enough
// for it, and its short form in a narrow one.
func TestViewTabNamesFit(t *testing.T) {
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
			t.Fatalf("no tab %q: %q", title, tt.Texts())
		}
		if r.W-6 < w {
			t.Errorf("the tab %q has %.0f points of the %.0f it takes", title, r.W-6, w)
		}
	}
	hs.gitRepo()
	// The panel on the layout's left is narrow: source control is GIT
	// there, with the views beside it.
	if !tt.HasText("GIT") || tt.HasText("SOURCE CONTROL") {
		t.Fatalf("the left panel names its tabs %q", tt.Texts())
	}
	// A panel as wide as the editors says the whole name.
	a.moveView(settings.ViewSCM, settings.SideBottom)
	hs.settleGit()
	whole("SOURCE CONTROL")
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
