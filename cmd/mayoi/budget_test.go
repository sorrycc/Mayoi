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
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// budgetFrames is how many frames each budget takes the median of.
const budgetFrames = 60

// medianFrame returns the median of n runs of frame, after 3n to warm
// up: the first after a file opens wait on its highlighting. With
// builds, which counts the builds of the view, a run costs its time over
// the builds it took.
func medianFrame(n int, frame func(), builds func() int) time.Duration {
	for range 3 * n {
		frame()
	}
	ds := make([]time.Duration, n)
	for i := range ds {
		start, was := time.Now(), 0
		if builds != nil {
			was = builds()
		}
		frame()
		ds[i] = time.Since(start)
		if builds != nil {
			ds[i] /= time.Duration(max(builds()-was, 1))
		}
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
		// perBuild counts the frames an input draws one by one: the tester
		// draws, at once, all those a fade asks for, which a window spreads
		// over its own.
		perBuild bool
		// measure sets the window up and returns the frame to time.
		measure func(t *testing.T, hs *harness) func()
	}{
		{"idle frame", 2 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigDoc(hs)
			return func() { step(hs.tt) }
		}},
		{"scroll frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
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
		{"type frame", 4 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			hs.place(d, 5000, 5000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		{"type frame with find open", 6 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			hs.a.find.query = "func"
			hs.a.openFind(false)
			hs.settle()
			d.vscroll.Focus()
			hs.place(d, 5000, 5000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		// Live Preview styles the lines about the view each frame, from a
		// parse that follows each edit until the next arrives.
		{"Live Preview type frame", 4 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigNote(hs)
			hs.place(d, 40000, 40000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		// The caret moving from line to line shows the marks of the line it
		// goes to and hides those of the line it leaves.
		{"Live Preview caret frame", 4 * time.Millisecond, true, func(t *testing.T, hs *harness) func() {
			d := bigNote(hs)
			hs.place(d, 40000, 40000)
			i := 0
			return func() {
				key := ui.KeyDown
				if i/20%2 == 1 {
					key = ui.KeyUp
				}
				i++
				hs.tt.Key(0, key)
				step(hs.tt)
			}
		}},
		// Typing a wiki link in a vault of 10,000 notes: the box follows
		// each key, its suggestions found away from the frame.
		{"Live Preview type frame with link suggestions", 4 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			os.MkdirAll(hs.path(".obsidian"), 0o755)
			for i := range 10000 {
				p := hs.path(fmt.Sprintf("vault/%d/note %d.md", i/100, i))
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, nil, 0o644)
			}
			t.Cleanup(func() { forgetWikiFiles(hs.root) })
			d := bigNote(hs)
			hs.place(d, 40000, 40000)
			hs.tt.Type("[")
			hs.tt.Type("[")
			hs.until("the suggestions", func() bool { s := &hs.a.wikiSug; return s.open && len(s.items) > 0 })
			i := 0
			return func() {
				if i++; i%8 == 0 {
					hs.tt.Key(0, ui.KeyBackspace)
				} else {
					hs.tt.Type("n")
				}
				step(hs.tt)
			}
		}},
		{"Live Preview scroll frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigNote(hs)
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
		// The palette shows in the frame after its key, and goes in the
		// frame after Escape: both, well within the tenth of a second
		// that still reads as at once.
		{"palette open and close", 60 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
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
		{"quick open typing", 3 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
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
		// The Sessions view of some 2,000 sessions, scrolled with the
		// pointer over its rows, whose hovers fade as they pass under it,
		// and filtered as a query is typed and cleared.
		{"sessions scroll, hover and filter", 2 * time.Millisecond, true, func(t *testing.T, hs *harness) func() {
			manySessions(t, hs, 2000)
			row, ok := hs.tt.Find("fix the session 0 of the folder 0, then run its tests")
			if !ok {
				t.Fatal("no session's row")
			}
			x, y := row.X+row.W/2, row.Y+120
			hs.tt.Move(x, y)
			i := 0
			return func() {
				switch {
				case i%40 == 39:
					hs.a.sessions.query = ""
				case i%10 == 9:
					hs.tt.Type("1")
				}
				dy := float32(60)
				if i/20%2 == 1 {
					dy = -60
				}
				i++
				hs.tt.Scroll(x, y, 0, dy)
				step(hs.tt)
			}
		}},
		// Each key typed in the query of those sessions filters them again.
		{"sessions filter typing", 2 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			manySessions(t, hs, 2000)
			i := 0
			return func() {
				if i%6 == 0 {
					hs.a.sessions.query = ""
				}
				i++
				hs.tt.Type("s")
				step(hs.tt)
			}
		}},
		// A terminal printing as fast as it can, as a build's log does,
		// costs the window little more than an idle frame (#115).
		{"terminal streaming", 4 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
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
			// The terminal asks for a frame after each, so step's Frame
			// draws up to 20 of them: a refresh draws the one a window
			// would.
			if !hs.tt.FramePending() {
				t.Fatal("the terminal does not ask for frames")
			}
			return func() { hs.tt.Advance(time.Second / 60) }
		}},
	}
	var report strings.Builder
	for _, b := range budgets {
		t.Run(b.name, func(t *testing.T) {
			hs := newHarness(t, true)
			frame := b.measure(t, hs)
			var builds func() int
			if b.perBuild {
				builds = func() int { return hs.a.builds }
			}
			got := medianFrame(budgetFrames, frame, builds)
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

// bigNote opens a Markdown note of some 6,000 lines as Live Preview,
// scrolled into its middle, once parsed, and returns its editor.
func bigNote(hs *harness) *editor {
	part := "## Section\n\nSome **bold**, *italic* and `code` text with a [link](https://example.com) and ==marks==.\n\n" +
		"- [ ] a task\n- [x] done\n- an item\n  - nested\n\n> A quote of some length.\n\n```go\nfunc f() int { return 1 }\n```\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n"
	os.WriteFile(hs.path("big.md"), []byte(strings.Repeat(part, 250)), 0o644)
	hs.a.settings.MarkdownMode = "live"
	hs.a.openFile(hs.path("big.md"), true)
	d := hs.a.activeEditor()
	hs.until("the note parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	d.vscroll.Y = 40000
	hs.settle()
	return d
}

// manySessions shows the sessions of every folder in the Sessions view,
// n of them, of Claude Code, used last over the past two months.
func manySessions(t *testing.T, hs *harness, n int) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	now := time.Now()
	for i := range n {
		id := fmt.Sprintf("s%04d", i)
		p := filepath.Join(home, ".claude", "projects", fmt.Sprintf("p%02d", i%40), id+".jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		line := fmt.Sprintf(`{"type":"user","message":{"content":"fix the session %d of the folder %d, then run its tests"},"cwd":"/work/project-%02d"}`+"\n", i, i%40, i%40)
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Duration(i) * 45 * time.Minute)
		os.Chtimes(p, at, at)
	}
	hs.a.sessions.all = 1
	hs.a.showSessions()
	hs.settle()
	if got := len(hs.a.sessions.sessions); got != n {
		t.Fatalf("%d sessions read, want %d", got, n)
	}
}
