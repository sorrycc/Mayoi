package main

// A state that changes moves nothing else: a tab's title stays where it
// was, and so do the tabs beside it and what is clicked on them, as the
// pointer comes over a tab or its file gets unsaved changes. A title that
// shifts as the state does makes a person lose their place, and a target
// that moves under the pointer is missed.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
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

// The rows of the box of the checks stay as tall, and where they were, as
// the pointer or the keys come over them and the actions of one show: a
// row that grew under the pointer pushed those below it down.
func TestCheckRowsHoldStill(t *testing.T) {
	var asked []string
	gh := git.GH
	git.GH = func(args ...string) ([]byte, error) {
		asked = append(asked, strings.Join(args, " "))
		if args[0] == "api" {
			return []byte("boom\n"), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { git.GH = gh })
	hs := newHarness(t, false)
	pick := commitChecksBox(hs)
	pick.checks = []git.Check{
		{Name: "Unit", Workflow: "Test", Bucket: "fail", JobID: 1, Link: "https://example.invalid/unit"},
		{Name: "Build", Workflow: "Test", Bucket: "pending"},
		{Name: "Lint", Workflow: "Test", Bucket: "pass", Link: "https://example.invalid/lint"},
		{Name: "Budgets", Workflow: "Test", Bucket: "fail", JobID: 2},
		{Name: "ci/other", Bucket: "pass", Description: "A description long enough to be cut short in a narrow box"},
	}
	for i := range 16 {
		pick.checks = append(pick.checks, git.Check{Name: fmt.Sprint("Extra ", i), Bucket: "pass", Link: "https://example.invalid/x"})
	}
	pick.poll.Start(pick.asked(), 0)
	pick.poll.Done(pick.asked())
	pick.poll.Keep(pick.asked(), time.Hour, func() {})
	hs.settle()
	rows := []string{"Test / Unit", "Test / Build", "Test / Lint", "Test / Budgets", "ci/other", pick.checks[4].Description}
	hover := func(name string) {
		r, _ := hs.tt.Find(name)
		hs.tt.Move(r.X+20, r.Y+r.H/2)
		hs.settle()
	}
	still := func(state string) {
		t.Helper()
		rest := hs.layoutOf(rows...)
		for _, n := range rows[:5] {
			if h := rest[n].H; h != 30 {
				t.Errorf("%s: %q is %v tall", state, n, h)
			}
		}
		for _, n := range rows {
			hover(n)
			sameLayout(t, state+", over "+n, rest, hs.layoutOf(rows...))
		}
		for range 4 {
			hs.tt.Key(0, ui.KeyDown)
			hs.settle()
			sameLayout(t, state+", down", rest, hs.layoutOf(rows...))
		}
		for range 4 {
			hs.tt.Key(0, ui.KeyUp)
			hs.settle()
			sameLayout(t, state+", up", rest, hs.layoutOf(rows...))
		}
	}
	still("wide")

	// An action shows within the row it is of, in the middle of its height.
	hs.tt.Key(0, ui.KeyDown) // to the pending one, of no actions
	hs.settle()
	hover("Test / Lint")
	row, _ := hs.tt.Find("Test / Lint")
	b, ok := hs.tt.Find(locales.PrOpenOnGitHub())
	if !ok || b.Y-row.Y != row.Y+row.H-(b.Y+b.H) || b.X+b.W > row.X+row.W {
		t.Errorf("Open on GitHub at %+v, of a row at %+v", b, row)
	}

	// Scrolled, the rows below hold still as well, and so does the scroll.
	hover("ci/other")
	hs.tt.Wheel(row.X+20, row.Y, 0, 200)
	hs.settle()
	deep := []string{"Extra 10", "Extra 11", "Extra 12"}
	rest := hs.layoutOf(deep...)
	if rest["Extra 10"] == (ui.Rect{}) || hs.layoutOf("Test / Unit")["Test / Unit"].Y >= row.Y-60 {
		t.Fatalf("the rows did not scroll: %+v", rest)
	}
	for _, n := range deep {
		hover(n)
		sameLayout(t, "scrolled, over "+n, rest, hs.layoutOf(deep...))
	}
	hs.tt.Wheel(row.X+20, row.Y, 0, -400)
	hs.settle()

	// In a narrow window, as in a wide one.
	hs.tt.SetSize(420, 760)
	hs.tt.Move(0, 0)
	hs.settle()
	still("narrow")
	hs.tt.SetSize(1200, 760)
	hs.settle()

	// The actions that show take their clicks, and close the box.
	click := func(over, action string) {
		t.Helper()
		pick.open, pick.sel = true, 1 // the pending one, of no actions
		hs.settle()
		hover(over)
		if err := hs.tt.Click(action); err != nil {
			t.Fatalf("%s of %s: %v", action, over, err)
		}
		hs.settle()
		if pick.open {
			t.Errorf("%s of %s left the box open", action, over)
		}
	}
	click("Test / Lint", locales.PrOpenOnGitHub())
	if got := hs.tt.OpenedURLs(); len(got) != 1 || got[0] != "https://example.invalid/lint" {
		t.Errorf("opened %v", got)
	}
	click("Test / Budgets", locales.ChecksRerun())
	hs.until("the job run again", func() bool {
		return slices.ContainsFunc(asked, func(s string) bool { return strings.HasPrefix(s, "run rerun --job 2 ") })
	})
	hs.tt.SetClipboard("")
	click("Test / Budgets", locales.ChecksCopyLog())
	hs.until("the log copied", func() bool { return strings.Contains(hs.tt.Clipboard(), "boom") })
	hs.tt.SetClipboard("")
	click("Test / Budgets", locales.ChecksSend())
	hs.until("the log sent", func() bool { return strings.Contains(hs.tt.Clipboard(), "boom") })
}
