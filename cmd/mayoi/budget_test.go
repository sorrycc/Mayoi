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
	"mayoi/internal/git"
)

// budgetFrames is how many frames each budget takes the median of.
const budgetFrames = 60

// budgetAttempts is how many medians a budget takes, at most, before it
// fails: one, and more, after the other budgets, only while each misses.
const budgetAttempts = 3

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
		// The pointer moving over the text finds the name under it, for
		// the card it rests on, a frame at a time.
		{"pointer over text frame", 2 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigDoc(hs)
			i := 0
			return func() {
				i++
				hs.tt.Move(300+float32(i%60)*5, 150+float32(i%23)*11)
				step(hs.tt)
			}
		}},
		// A hover open follows its word as the editor scrolls, and hides
		// while the word is out of view.
		{"scroll frame with a hover", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			hs.place(d, 5000, 5000)
			hs.a.hover = newHoverCard(d, "```go\nfunc f(a, b int) string\n```\n\n"+strings.Repeat("Some *documentation* of `f`.\n\n", 20), nil, d.sel[1])
			hs.settle()
			i := 0
			return func() {
				dy := float32(60)
				if i/30%2 == 1 {
					dy = -60
				}
				i++
				hs.tt.Scroll(1100, 400, 0, dy)
				step(hs.tt)
				if hs.a.hover == nil {
					t.Fatal("scrolling closed the hover")
				}
			}
		}},
		{"wrapped CJK line scroll frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			var b strings.Builder
			for i := range 200 {
				if i%20 == 10 {
					b.WriteString(strings.Repeat("测试长行", 600) + "\n")
				} else {
					fmt.Fprintf(&b, "line %d\n", i)
				}
			}
			os.WriteFile(hs.path("long.txt"), []byte(b.String()), 0o644)
			hs.a.openFile(hs.path("long.txt"), true)
			hs.a.run("view.wordWrap")
			hs.settle()
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
		// Typing at a caret on every "func" of the file: thousands of them,
		// edited at once in one step of undo.
		{"type frame with thousands of carets", 45 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			at := strings.Index(d.buf.String(), "func")
			hs.place(d, at, at)
			hs.key(ui.Super|ui.Shift, ui.KeyL)
			if len(d.cursors) < 1000 {
				t.Fatalf("%d selections", len(d.cursors)+1)
			}
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		{"select all occurrences", 30 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigDoc(hs)
			at := strings.Index(d.buf.String(), "func")
			return func() {
				hs.place(d, at, at)
				hs.key(ui.Super|ui.Shift, ui.KeyL)
			}
		}},
		// The Table view of JSON Lines builds the rows in view only, its
		// cells read from their lines as they show.
		{"JSON Lines table scroll frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigJSONL(t, hs)
			i := 0
			return func() {
				dy := float32(60)
				if i/30%2 == 1 {
					dy = -60
				}
				i++
				hs.tt.Scroll(700, 300, 0, dy)
				step(hs.tt)
			}
		}},
		{"JSON Lines table cursor frame", 4 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigJSONL(t, hs)
			i := 0
			return func() {
				key := ui.KeyDown
				if i/30%2 == 1 {
					key = ui.KeyUp
				}
				i++
				hs.tt.Key(0, key)
				step(hs.tt)
			}
		}},
		{"JSON Lines filter typing frame", 6 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigJSONL(t, hs)
			hs.a.run("edit.find")
			hs.settle()
			i := 0
			return func() {
				if i%2 == 0 {
					hs.tt.Type("9")
				} else {
					hs.tt.Key(0, ui.KeyBackspace)
				}
				i++
				step(hs.tt)
			}
		}},
		// Live Preview styles the lines about the view each frame, from a
		// parse that follows each edit until the next arrives.
		{"Live Preview type frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigNote(hs)
			hs.place(d, 40000, 40000)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		// Typing in a cell of a table edited in place: the input alone
		// changes, what was typed written to the text as typing pauses.
		{"Live Preview table cell type frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigNote(hs)
			editTableCell(t, hs, d)
			return func() { hs.tt.Type("a"); step(hs.tt) }
		}},
		// A row added to a table from a cell, as Return in its last row
		// does, in a note of some 400 lines: one edit of the text, the
		// table drawn as it was until the parse it asks for at once
		// arrives. Most of it is the text measuring the lines after a new
		// one again, as for any line added, which Return in this note's
		// text costs far more than (some 120 ms): hence above 5 ms.
		{"Live Preview table row add frame", 15 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			part := "Some **bold** text with a [link](https://example.com).\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n"
			os.WriteFile(hs.path("rows.md"), []byte(strings.Repeat(part, 60)), 0o644)
			hs.a.settings.MarkdownMode = "live"
			hs.a.openFile(hs.path("rows.md"), true)
			d := hs.a.activeEditor()
			hs.until("the note parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
			editTableCell(t, hs, d)
			return func() {
				c := &d.liveCell
				hs.a.liveTableEdit(d, c.first, c.last, tableRowBelow, c.row, c.col)
				step(hs.tt)
			}
		}},
		// A table of 1,000 rows in Live Preview, of three columns, among
		// prose: it builds the rows in view and near it only, the others
		// taking their room, so that a frame costs as much as for a short
		// one, idle, scrolled through and typed in at its last row. A row
		// added to it costs far more, in the text measuring its lines
		// again (TestLiveTableLong adds one).
		{"Live Preview 1,000-row table idle frame", 2 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigTable(t, hs)
			return func() { step(hs.tt) }
		}},
		{"Live Preview 1,000-row table scroll frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			bigTable(t, hs)
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
		{"Live Preview 1,000-row table last cell type frame", 5 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
			d := bigTable(t, hs)
			hs.a.openLiveCell(d, bigTableFirst, bigTableFirst+1+bigTableRows, bigTableRows, 0)
			hs.settle()
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
		{"Live Preview type frame with link suggestions", 6 * time.Millisecond, false, func(t *testing.T, hs *harness) func() {
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
		// The GitHub view of 200 pull requests and 300 issues, scrolled
		// with the pointer over its rows, and filtered as a query is typed
		// and cleared.
		{"github scroll, hover and filter", 2 * time.Millisecond, true, func(t *testing.T, hs *harness) func() {
			manyGitHub(t, hs, 200, 300)
			row, ok := hs.tt.Find("Pull request 0, which makes the tree faster")
			if !ok {
				t.Fatal("no pull request's row")
			}
			x, y := row.X+row.W/2, row.Y+120
			hs.tt.Move(x, y)
			i := 0
			return func() {
				switch {
				case i%40 == 39:
					hs.a.github.query = ""
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
			// would. The last frame may have taken the ask; the next
			// output asks again.
			for deadline := time.Now().Add(2 * time.Second); !hs.tt.FramePending() && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
			if !hs.tt.FramePending() {
				t.Fatal("the terminal does not ask for frames")
			}
			return func() { hs.tt.Advance(time.Second / 60) }
		}},
	}
	// A shared runner's neighbours can slow every median for a while: a
	// budget missed is measured again, on a window of its own, once the
	// others were, failing only when every attempt misses it. A regression
	// misses them all.
	tries := make([][]time.Duration, len(budgets))
	missed := func(i int) bool { n := len(tries[i]); return n > 0 && tries[i][n-1] > budgets[i].budget }
	for attempt := range budgetAttempts {
		for i, b := range budgets {
			if attempt > 0 && !missed(i) {
				continue
			}
			name := b.name
			if attempt > 0 {
				name = fmt.Sprintf("%s, attempt %d", b.name, attempt+1)
			}
			t.Run(name, func(t *testing.T) {
				hs := newHarness(t, true)
				frame := b.measure(t, hs)
				var builds func() int
				if b.perBuild {
					builds = func() int { return hs.a.builds }
				}
				tries[i] = append(tries[i], medianFrame(budgetFrames, frame, builds))
			})
		}
	}
	var report strings.Builder
	for i, b := range budgets {
		if len(tries[i]) == 0 {
			continue // left out by -run
		}
		var ms []string
		for _, d := range tries[i] {
			ms = append(ms, fmt.Sprintf("%.2f ms", float64(d.Microseconds())/1000))
		}
		fmt.Fprintf(&report, "%-28s %s  (budget %v)\n", b.name, strings.Join(ms, ", "), b.budget)
		if missed(i) {
			t.Errorf("%s: %s a frame, over its budget of %v in each of %d attempts", b.name, strings.Join(ms, ", "), b.budget, len(ms))
		}
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

// editTableCell clicks the first cell of the body of a table Live Preview
// shows in an editor, to edit it in place.
func editTableCell(t *testing.T, hs *harness, d *editor) {
	t.Helper()
	var table *liveView
	hs.until("a table shown", func() bool {
		// The highest in view.
		table = nil
		for _, lv := range d.livePane.views {
			if m := lv.v.markdown; lv.frame == hs.a.frame && lv.measured && lv.at.Y > 0 && len(m.blocks) > 0 &&
				m.blocks[0].kind == markdownTableHeader && (table == nil || lv.at.Y < table.at.Y) {
				table = lv
			}
		}
		return table != nil
	})
	hs.tt.ClickAt(table.at.X+10, table.at.Y+table.at.H*0.75)
	hs.settle()
	if !d.liveCell.on {
		t.Fatal("the click does not edit a cell")
	}
}

// bigTableRows is how many rows bigTable's table has below its header,
// whose line is bigTableFirst.
const (
	bigTableRows  = 1000
	bigTableFirst = 40
)

// bigTable opens a note of prose around a table of bigTableRows rows as
// Live Preview, once the table is measured, scrolled into the table, and
// returns its editor.
func bigTable(t *testing.T, hs *harness) *editor {
	t.Helper()
	d := openLive(t, hs, "table.md", liveLongTable(bigTableRows), 1)
	lv := tableView(d)
	hs.until("the table measured", func() bool { return lv.measured && lv.frame == hs.a.frame })
	d.vscroll.Y = 2000
	hs.settle()
	return d
}

// bigJSONL opens a file of 100,000 lines of JSON Lines as its table,
// once read, and returns its editor.
func bigJSONL(t *testing.T, hs *harness) *editor {
	return openJSONL(t, hs, "big.jsonl", jsonlText(100_000))
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

// manyGitHub shows the GitHub view of a repository with prs pull
// requests and issues issues open, each labeled, by people with pictures,
// each pull request with three reviewers.
func manyGitHub(t *testing.T, hs *harness, prs, issues int) {
	o := &git.GHOverview{Viewer: "me", Repo: "o/r", URL: "https://github.com/o/r", IssuesOn: true, DefaultBranch: "main",
		OpenPRs: prs, OpenIssues: issues,
		Avatars: map[string]string{"ann": "https://avatars.test/ann", "bob": "https://avatars.test/bob", "cat": "https://avatars.test/cat"}}
	now := time.Now()
	for i := range prs {
		o.PRs = append(o.PRs, git.GHItem{Number: 1000 + i, Title: fmt.Sprintf("Pull request %d, which makes the tree faster", i),
			Author: []string{"ann", "me", "bob"}[i%3], PR: true, ReviewAsked: i%7 == 0, Checks: git.Checks(i % 4),
			Updated: now.Add(-time.Duration(i) * time.Hour), Comments: i % 5, Labels: []git.GHLabel{{Name: "perf", Color: "fbca04"}},
			Reviewers: []git.GHReviewer{{Login: "bob", State: "APPROVED"}, {Login: "cat", State: "COMMENTED"},
				{Login: "ann", State: "REQUESTED"}, {Login: "me", State: "REQUESTED", Again: i%7 == 0}}})
	}
	for i := range issues {
		o.Issues = append(o.Issues, git.GHItem{Number: i + 1, Title: fmt.Sprintf("Issue %d, of the explorer", i), Author: "cat",
			Updated: now.Add(-time.Duration(i) * time.Hour), Comments: i % 9, Labels: []git.GHLabel{{Name: "bug", Color: "d73a4a"}, {Name: "ui", Color: "1d76db"}}})
	}
	fakeGitHub(t, o, nil)
	hs.h.visible = true
	hs.gitRepo()
	hs.a.showGitHub()
	hs.settle()
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
