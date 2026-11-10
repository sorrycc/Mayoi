package main

// Latency budgets: what a person feels as the app's speed, measured as
// the median cost of a frame, failing once a change makes it slower than
// its budget. The budgets leave room for a slower machine than the
// M-series Mac docs/ARCHITECTURE.md's figures come from, as CI's; they
// catch a regression, not noise. Run
// MAYOI_BUDGETS=1 go test -run TestBudgets ./cmd/mayoi
// The terminal's budget starts a shell, which -short skips.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// budgetFrames is how many frames each budget takes the median of.
const budgetFrames = 60

// medianFrame returns the median of n runs of frame, after 3n to warm
// up: the first after a file opens wait on its highlighting.
func medianFrame(n int, frame func()) time.Duration {
	for range 3 * n {
		frame()
	}
	ds := make([]time.Duration, n)
	for i := range ds {
		start := time.Now()
		frame()
		ds[i] = time.Since(start)
	}
	slices.Sort(ds)
	return ds[n/2]
}

func TestBudgets(t *testing.T) {
	if os.Getenv("MAYOI_BUDGETS") == "" {
		t.Skip("MAYOI_BUDGETS is not set")
	}
	budgets := []struct {
		name   string
		budget time.Duration
		// measure sets the window up and returns the frame to time.
		measure func(t *testing.T, hs *harness) func()
	}{
		{"idle frame", 2 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			bigDoc(hs)
			return func() { step(hs.tt) }
		}},
		{"scroll frame", 4 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			bigDoc(hs)
			i := 0
			return func() {
				dy := float32(60)
				if i/30%2 == 1 {
					dy = -60
				}
				i++
				hs.tt.Scroll(700, 400, 0, dy)
				step(hs.tt)
			}
		}},
		{"type frame", 4 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			hs.place(d, 5000, 5000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		{"type frame with find open", 6 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			hs.a.find.query = "func"
			hs.a.openFind(false)
			hs.settle()
			d.vscroll.Focus()
			hs.place(d, 5000, 5000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		// The palette shows in the frame after its key, and goes in the
		// frame after Escape: both, well within the tenth of a second
		// that still reads as at once.
		{"palette open and close", 60 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			bigDoc(hs)
			return func() {
				hs.a.run("go.commands")
				step(hs.tt)
				if !hs.a.quick.open {
					t.Fatal("the palette did not open")
				}
				hs.tt.Key(0, ui.KeyEscape)
				step(hs.tt)
			}
		}},
		{"quick open typing", 3 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			hs.a.openQuick("")
			hs.settle()
			i := 0
			return func() {
				if i%8 == 0 {
					hs.a.quick.query = ""
				}
				i++
				hs.tt.Type("m")
				step(hs.tt)
			}
		}},
		// A terminal printing as fast as it can, as a build's log does,
		// costs the window little more than an idle frame (#115).
		{"terminal streaming", 4 * time.Millisecond, func(t *testing.T, hs *harness) func() {
			if testing.Short() {
				t.Skip("starts a shell")
			}
			hs.a.run("view.terminalTab")
			hs.settle()
			s := hs.a.terms[0]
			hs.termRun(s, "echo ready", "ready")
			hs.tt.Type("yes 'streaming the terminal as fast as it goes, line after line after line'")
			hs.tt.Key(0, ui.KeyEnter)
			for deadline := time.Now().Add(10 * time.Second); !strings.Contains(s.term.Text(), "line after line\n") && time.Now().Before(deadline); {
				step(hs.tt)
			}
			t.Cleanup(func() { hs.tt.Key(ui.Ctrl, ui.KeyC) })
			return func() { step(hs.tt) }
		}},
	}
	var report strings.Builder
	for _, b := range budgets {
		t.Run(b.name, func(t *testing.T) {
			hs := newHarness(t, true)
			frame := b.measure(t, hs)
			got := medianFrame(budgetFrames, frame)
			fmt.Fprintf(&report, "%-28s %6.2f ms  (budget %v)\n", b.name, float64(got.Microseconds())/1000, b.budget)
			if got > b.budget {
				t.Errorf("%s: %v a frame, over its budget of %v", b.name, got, b.budget)
			}
		})
	}
	t.Logf("frame costs, medians of %d:\n%s", budgetFrames, report.String())
}

// bigDoc opens a Go file of some 25,000 lines, scrolled into its middle,
// and returns its editor.
func bigDoc(hs *harness) *editor {
	src, _ := os.ReadFile("editor.go")
	var big []byte
	for range 60 {
		big = append(big, src...)
	}
	os.WriteFile(hs.path("big.go"), big, 0o644)
	hs.a.openFile(hs.path("big.go"), true)
	hs.settle()
	d := hs.a.activeEditor()
	d.vscroll.Y = 4000
	hs.settle()
	return d
}
