package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRelocate(t *testing.T) {
	text := []string{"a", "b", "c", "d", "b", "c"}
	for _, c := range []struct {
		lines []string
		from  int
		at    int
		ok    bool
	}{
		{[]string{"b", "c"}, 1, 1, true},
		{[]string{"b", "c"}, 3, 4, true}, // the nearest
		{[]string{"b", "c"}, 0, 1, true},
		{[]string{"c", "d"}, 5, 2, true},
		{[]string{"x"}, 2, 2, false},
		{[]string{"c", "d", "b", "c", "e"}, 2, 2, false},
	} {
		at, ok := relocate(c.lines, text, c.from)
		if at != c.at || ok != c.ok {
			t.Errorf("relocate(%v, %d) = %d, %v; want %d, %v", c.lines, c.from, at, ok, c.at, c.ok)
		}
	}
}

func TestFormatReview(t *testing.T) {
	rel := func(p string) string { return strings.TrimPrefix(p, "/repo/") }
	one := &reviewNote{path: "/repo/a.go", from: 11, to: 13, diff: true, snippet: []string{"-x := 1", "+x := 2"}, text: " Not here, the previous section. \n"}
	if got, want := formatReview([]*reviewNote{one}, rel), "a.go:12-13\n```diff\n-x := 1\n+x := 2\n```\nNot here, the previous section."; got != want {
		t.Errorf("one:\n%s\nwant:\n%s", got, want)
	}
	two := &reviewNote{path: "/repo/README.md", from: 0, to: 1, snippet: []string{"```go"}, text: "Fix the fence", outdated: true}
	removed := &reviewNote{path: "/elsewhere/b.go", commit: "0123456789abcdef", oldSide: true, from: 4, to: 5, diff: true, snippet: []string{"-y"}}
	got := formatReview([]*reviewNote{one, two, removed}, rel)
	for _, want := range []string{
		"1. a.go:12-13\n",
		"\n\n2. README.md:1 (outdated: the lines have changed since)\n````md\n```go\n````\nFix the fence\n",
		"\n3. /elsewhere/b.go:5 (removed lines, numbered as before the change; in commit 0123456)\n```diff\n-y\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	staged := &reviewNote{path: "/repo/a.go", staged: true, from: 2, to: 3, snippet: []string{"z"}}
	if got := staged.ref(rel); got != "a.go:3 (staged)" {
		t.Errorf("staged %q", got)
	}
}

func TestCommentRows(t *testing.T) {
	if got := commentRows("short\n\nthe quick brown fox jumps", 10); !slices.Equal(got, []string{"short", "", "the quick ", "brown fox ", "jumps"}) {
		t.Errorf("rows %q", got)
	}
}

// A card grows with its comment, and the box a comment is typed in from
// 3 lines to 10.
func TestNoteHeight(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	one, two := &reviewNote{text: "one"}, &reviewNote{text: "one\ntwo"}
	if h1, h2 := a.noteHeight(one, false, "", 300), a.noteHeight(two, false, "", 300); h2 <= h1 {
		t.Errorf("heights %v, %v", h1, h2)
	}
	long := strings.Repeat("word ", 200)
	if h1, h2 := a.noteHeight(one, false, "", 300), a.noteHeight(&reviewNote{text: long}, false, "", 300); h2 < 4*h1 {
		t.Errorf("a long comment does not wrap: %v, %v", h1, h2)
	}
	empty, tall := a.noteHeight(one, true, "", 300), a.noteHeight(one, true, strings.Repeat("x\n", 40), 300)
	if a.noteHeight(one, true, "x\ny", 300) != empty || tall <= empty || tall != a.noteHeight(one, true, strings.Repeat("x\n", 80), 300) {
		t.Errorf("box heights %v, %v", empty, tall)
	}
	if k := a.noteLines(one, false, "", 300); float32(k)*a.font.lineH < a.noteHeight(one, false, "", 300)+2*noteInset {
		t.Errorf("%d lines do not hold the card", k)
	}
}

// reviewRepo makes a repository with t.txt of 30 lines committed, then
// changes line 3 and line 26 of it.
func reviewRepo(hs *harness) (orig, changed string) {
	git := hs.gitRepo()
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig = b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "t")
	changed = strings.Replace(strings.Replace(orig, "line 2\n", "line two\n", 1), "line 25\n", "line twenty-five\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	return orig, changed
}

func TestReviewDiff(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	_, changed := reviewRepo(hs)
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutInline)
	hs.settle()
	removed := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.old == 2 && r.new < 0 })
	if removed < 0 || v.rows[removed+1].new != 2 {
		t.Fatalf("rows %s", kinds(v))
	}
	// Shift+J chooses the removed line and the line added.
	v.pane.Focus()
	v.setCursor(removed)
	hs.settle()
	hs.key(ui.Shift, ui.KeyJ)
	if lo, hi := v.selection(); lo != removed || hi != removed+1 || !v.selected(removed) {
		t.Fatalf("selection %d-%d, cursor %d anchor %d", lo, hi, v.cursor, v.anchor)
	}
	a.copyExcerpt(v)
	hs.settle()
	if got, want := tt.Clipboard(), "t.txt:3\n```diff\n-line 2\n+line two\n```"; got != want {
		t.Errorf("copied %q, want %q", got, want)
	}
	// c writes a comment under the lines.
	hs.key(0, ui.KeyC)
	if a.review.draft == nil || v.anchor >= 0 {
		t.Fatalf("no draft, or still chosen: %v", v.anchor)
	}
	at := strings.Index(kinds(v), "c")
	if at != removed+2 {
		t.Fatalf("card at %d: %s", at, kinds(v))
	}
	tt.Type("Not here, the previous section.")
	hs.settle()
	// The keys of the diff are the box's while it is typed in.
	if a.review.text != "Not here, the previous section." {
		t.Fatalf("typed %q", a.review.text)
	}
	hs.shot("review-draft")
	hs.key(ui.Cmd, ui.KeyEnter)
	if len(a.review.notes) != 1 || a.review.draft != nil {
		t.Fatalf("notes %d, draft %v", len(a.review.notes), a.review.draft)
	}
	n := a.review.notes[0]
	if n.from != 2 || n.to != 3 || n.oldSide || n.text != "Not here, the previous section." {
		t.Errorf("note %+v", n)
	}
	if !strings.Contains(kinds(v), "c") {
		t.Errorf("no card: %s", kinds(v))
	}
	hs.shot("review-saved")
	// The hunks are as they were, the card among the lines of the first.
	if len(v.starts) != 2 {
		t.Errorf("starts %v: %s", v.starts, kinds(v))
	}
	// Lines added above move the note with its lines.
	os.WriteFile(hs.path("t.txt"), []byte("new 1\nnew 2\n"+changed), 0o644)
	hs.settleGit()
	hs.settle()
	text := a.reviewText()
	if n.from != 4 || n.outdated || !strings.HasPrefix(text, "t.txt:5\n```diff\n-line 2\n+line two\n```\nNot here") {
		t.Errorf("moved: %d %v\n%s", n.from, n.outdated, text)
	}
	// Changing the lines marks it outdated.
	os.WriteFile(hs.path("t.txt"), []byte(strings.Replace(changed, "line two", "line 2!", 1)), 0o644)
	hs.settleGit()
	if text := a.reviewText(); !n.outdated || !strings.Contains(text, "(outdated") {
		t.Errorf("outdated %v:\n%s", n.outdated, text)
	}
	// With no terminal, sending copies them, and keeps them.
	a.sendReview()
	hs.settle()
	if got := tt.Clipboard(); !strings.Contains(got, "Not here") || len(a.review.notes) != 1 {
		t.Errorf("sent to the clipboard %q, %d notes", got, len(a.review.notes))
	}
	a.clearReview()
	hs.settle()
	if strings.Contains(kinds(v), "c") {
		t.Errorf("cleared: %s", kinds(v))
	}
	// A drag chooses the rows from where it started.
	pane, ok := tt.Find("Diff of the Changes")
	if !ok {
		t.Fatal("no diff")
	}
	v.scroll.Y = 0
	hs.settle()
	rowY := func(i int) float32 { return pane.Y + editorPadY + (float32(v.tops[i])+0.5)*a.font.lineH }
	first := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine })
	hs.drag(pane.X+120, rowY(first), pane.X+120, rowY(first+3))
	if lo, hi := v.selection(); lo != first || hi != first+3 {
		t.Errorf("dragged %d-%d, want %d-%d", lo, hi, first, first+3)
	}
	// A click chooses one row again, as Escape does.
	tt.ClickAt(pane.X+120, rowY(first+1))
	hs.settle()
	if lo, hi := v.selection(); lo != first+1 || hi != first+1 {
		t.Errorf("clicked %d-%d", lo, hi)
	}
	hs.key(ui.Shift, ui.KeyDown)
	hs.key(0, ui.KeyEscape)
	if v.anchor >= 0 {
		t.Errorf("Escape keeps %d", v.anchor)
	}
}

func TestReviewEditorToTerminal(t *testing.T) {
	featShell(t)
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	reviewRepo(hs)
	a.newTerminal()
	hs.settle()
	s := a.panelTerm
	hs.until("the prompt", func() bool { return strings.Contains(s.term.Text(), "❯") })
	a.openFile(hs.path("t.txt"), true)
	hs.settle()
	d := a.activeEditor()
	snap := d.buf.Snapshot()
	from, _ := snap.LineRange(2)
	to, _ := snap.LineRange(4) // the start of a line ends before it
	hs.place(d, from, to)
	a.run("review.comment")
	hs.settle()
	if a.review.draft == nil || a.review.ed != d {
		t.Fatal("no box")
	}
	if _, ok := tt.Find("Review Comment"); !ok {
		t.Error("the box does not show")
	}
	tt.Type("Use a loop")
	hs.settle()
	hs.shot("review-editor")
	hs.key(ui.Cmd, ui.KeyEnter)
	if len(a.review.notes) != 1 {
		t.Fatalf("notes %d", len(a.review.notes))
	}
	n := a.review.notes[0]
	if n.diff || n.from != 2 || n.to != 4 || !slices.Equal(n.snippet, []string{"line two", "line 3"}) {
		t.Errorf("note %+v", n)
	}
	// The command on lines of a note edits it.
	hs.place(d, from, from)
	a.run("review.comment")
	hs.settle()
	if a.review.draft != n || a.review.text != "Use a loop" {
		t.Fatalf("editing %v %q", a.review.draft == n, a.review.text)
	}
	hs.key(0, ui.KeyEscape)
	if a.review.draft != nil || n.text != "Use a loop" {
		t.Fatalf("cancel: %v %q", a.review.draft, n.text)
	}
	a.sendReview()
	hs.settle()
	hs.until("the comment pasted", func() bool {
		return strings.Contains(s.term.Text(), "t.txt:3-4") && strings.Contains(s.term.Text(), "Use a loop")
	})
	if len(a.review.notes) != 0 || a.focus != paneTerminal {
		t.Errorf("after sending: %d notes, focus %v", len(a.review.notes), a.focus)
	}
}

// Review comments in a diff, inline and side by side, and in a file.
func TestShotReview(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		name := "review-light"
		if dark {
			name = "review-dark"
		}
		hs := newHarness(t, dark)
		a, tt := hs.a, hs.tt
		reviewRepo(hs)
		a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
		hs.settleGit()
		v := a.activeTab().diff
		a.setDiffLayout(v, layoutInline)
		hs.settle()
		pane, _ := tt.Find("Diff of the Changes")
		rowY := func(i int) float32 {
			return pane.Y + editorPadY + (float32(v.tops[i])+0.5)*a.font.lineH - v.scroll.Y
		}
		added := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.new == 2 && r.old < 0 })
		tt.Move(pane.X+200, rowY(added))
		hs.settle()
		hs.shot(name + "-hover")
		v.pane.Focus()
		v.setCursor(added - 1)
		hs.key(ui.Shift, ui.KeyJ)
		hs.key(0, ui.KeyC)
		tt.Type("Keep the number: the tests read it as a digit, and the rest of the file spells none out.")
		hs.settle()
		hs.shot(name + "-draft")
		hs.key(ui.Cmd, ui.KeyEnter)
		last := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.new == 25 && r.old < 0 })
		v.setCursor(last)
		hs.key(0, ui.KeyC)
		hs.settle()
		tt.Type("Same here.")
		hs.key(ui.Cmd, ui.KeyEnter)
		tt.Move(pane.X+200, pane.Y+pane.H-10)
		hs.settle()
		hs.shot(name + "-saved")
		a.setDiffLayout(v, layoutSplit)
		hs.settle()
		hs.shot(name + "-split")
		a.openFile(hs.path("t.txt"), true)
		hs.settle()
		d := a.activeEditor()
		snap := d.buf.Snapshot()
		from, _ := snap.LineRange(6)
		to, _ := snap.LineRange(8)
		hs.place(d, from, to)
		a.run("review.comment")
		hs.settle()
		tt.Type("Fold these into a loop.")
		hs.settle()
		hs.shot(name + "-editor-draft")
		hs.key(ui.Cmd, ui.KeyEnter)
		// An empty comment cannot be made.
		hs.place(d, from, from)
		a.run("review.comment")
		hs.settle()
		a.review.text = ""
		hs.settle()
		hs.shot(name + "-editor-empty")
		hs.key(0, ui.KeyEscape)
		hs.place(d, 0, 0)
		hs.settle()
		hs.shot(name + "-editor")
		if b, ok := tt.Find("Edit Comment"); ok {
			tt.Move(b.X+b.W/2, b.Y+b.H/2)
			hs.settle()
			hs.shot(name + "-editor-hover")
		}
		a.setDiffLayout(v, layoutInline)
	}
}

func TestNoteHint(t *testing.T) {
	for text, want := range map[string]string{
		"  Use a loop  ":        "Use a loop",
		"First\nsecond":         "First…",
		strings.Repeat("a", 90): strings.Repeat("a", 79) + "…",
	} {
		if got := noteHint(&reviewNote{text: text}); got != want {
			t.Errorf("noteHint(%q) = %q, want %q", text, got, want)
		}
	}
}

// The button by the line under the pointer comments on it, an empty
// comment is not kept, and the breadcrumbs send the comments.
func TestReviewLineButton(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	reviewRepo(hs)
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutInline)
	hs.settle()
	pane, _ := tt.Find("Diff of the Changes")
	added := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.new == 2 && r.old < 0 })
	tt.Move(pane.X+200, pane.Y+editorPadY+(float32(v.tops[added])+0.5)*a.font.lineH)
	hs.settle()
	b, ok := tt.Find("Add Comment (C)")
	if !ok {
		t.Fatal("no button by the line")
	}
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	hs.settle()
	n := a.review.draft
	if n == nil || n.from != 2 || n.to != 3 || n.oldSide {
		t.Fatalf("draft %+v", n)
	}
	// Nothing typed, nothing kept.
	hs.key(ui.Cmd, ui.KeyEnter)
	if len(a.review.notes) != 0 || a.review.draft == nil {
		t.Fatalf("kept an empty comment: %d notes, draft %v", len(a.review.notes), a.review.draft)
	}
	tt.Type("Why?")
	hs.key(ui.Cmd, ui.KeyEnter)
	if len(a.review.notes) != 1 || a.review.draft != nil {
		t.Fatalf("notes %d, draft %v", len(a.review.notes), a.review.draft)
	}
	// The lines of a comment are marked as such.
	if !notedLine(v, a.diffNotes(v), v.rows[added], false) || notedLine(v, a.diffNotes(v), v.rows[added-1], true) {
		t.Error("the lines noted")
	}
	if len(v.cards) != 1 || v.rows[v.cards[0]].kind != rowComment {
		t.Errorf("cards %v", v.cards)
	}
	if _, ok := tt.Find("Send 1 Review Comment to Terminal"); !ok {
		t.Error("no button to send them")
	}
	// Each card's buttons are its own: delete the second.
	first := a.review.notes[0]
	last := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.new == 25 && r.old < 0 })
	v.setCursor(last)
	hs.key(0, ui.KeyC)
	hs.settle()
	tt.Type("And here?")
	hs.key(ui.Cmd, ui.KeyEnter)
	hs.settle()
	if len(v.cards) != 2 {
		t.Fatalf("cards %v", v.cards)
	}
	noteX, noteW := noteGeometry(a.diffGeometry(v, v.split, v.viewW), v.split, false)
	card := v.cards[1]
	// The header of the card, its inside in the middle of it.
	cardH := float32(v.rows[card].h)*a.font.lineH - 2*noteInset
	inside := a.noteHeight(a.review.notes[1], false, "", noteTextW(noteW)) - 2*notePadY
	y := pane.Y + editorPadY + float32(v.tops[card])*a.font.lineH - v.scroll.Y + noteInset + (cardH-inside)/2 + noteHeadH/2
	tt.ClickAt(pane.X+noteX+noteW-notePadX-10, y)
	hs.settle()
	if len(a.review.notes) != 1 || a.review.notes[0] != first {
		t.Errorf("deleted the wrong one: %d left", len(a.review.notes))
	}
	// Delete on a card deletes its note.
	v.pane.Focus()
	hs.settle()
	v.setCursor(v.cards[0])
	hs.key(0, ui.KeyDelete)
	if len(a.review.notes) != 0 {
		t.Errorf("Delete kept %d", len(a.review.notes))
	}
}

// A comment shows after its lines in a file, and clicking it edits it.
func TestReviewEditorHint(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	reviewRepo(hs)
	a.openFile(hs.path("t.txt"), true)
	hs.settle()
	d := a.activeEditor()
	snap := d.buf.Snapshot()
	at, _ := snap.LineRange(4)
	hs.place(d, at, at)
	a.run("review.comment")
	hs.settle()
	tt.Type("Say why\nand how")
	hs.key(ui.Cmd, ui.KeyEnter)
	if len(a.review.notes) != 1 {
		t.Fatalf("notes %d", len(a.review.notes))
	}
	n := a.review.notes[0]
	hs.settle()
	hints := a.editorHints(d, d.buf.Snapshot(), a.notedLines(d), 0, 29)
	if len(hints) != 1 || hints[0].line != 4 || hints[0].text != "Say why…" {
		t.Fatalf("hints %+v", hints)
	}
	b, ok := tt.Find("Edit Comment")
	if !ok {
		t.Fatal("no button over the hint")
	}
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	hs.settle()
	if a.review.draft != n || a.review.ed != d || a.review.text != "Say why\nand how" {
		t.Fatalf("editing %v %q", a.review.draft == n, a.review.text)
	}
	// While it is written, it shows in the box only.
	if hints := a.editorHints(d, d.buf.Snapshot(), a.notedLines(d), 0, 29); len(hints) != 0 {
		t.Errorf("hints while editing %+v", hints)
	}
	hs.key(0, ui.KeyEscape)
	// Lines typed above move it with its lines.
	d.buf.Replace(0, 0, "new\n")
	hs.settle()
	if n.from != 5 || n.outdated {
		t.Errorf("moved to %d, outdated %v", n.from, n.outdated)
	}
}

// The comments not sent are kept with the window's session, and the next
// start shows them again, found where their lines moved to.
func TestReviewKept(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	reviewRepo(hs)
	a.openFile(hs.path("t.txt"), true)
	hs.settle()
	d := a.activeEditor()
	at, _ := d.buf.Snapshot().LineRange(4)
	hs.place(d, at, at)
	a.run("review.comment")
	hs.settle()
	hs.tt.Type("Keep me")
	hs.key(ui.Cmd, ui.KeyEnter)
	if got := a.window.Review; len(got) != 1 || got[0].Text != "Keep me" || got[0].From != 4 || got[0].Path != hs.path("t.txt") {
		t.Fatalf("kept %+v", got)
	}
	// Lines are added above it before the next start.
	text, _ := os.ReadFile(hs.path("t.txt"))
	os.WriteFile(hs.path("t.txt"), append([]byte("new\nnew\n"), text...), 0o644)
	b := newApp(&fakeHost{folder: hs.root}, a.settings, a.window)
	t.Cleanup(b.shutdown)
	tt := ui.NewTester(b.view, 1200, 760)
	step(tt)
	step(tt)
	if len(b.review.notes) != 1 {
		t.Fatalf("notes %d", len(b.review.notes))
	}
	n := b.review.notes[0]
	if b.followNote(n); n.from != 6 || n.outdated || n.text != "Keep me" {
		t.Errorf("restored %+v", n)
	}
	b.clearReview()
	if len(b.window.Review) != 0 {
		t.Errorf("cleared, kept %d", len(b.window.Review))
	}
	// Its toast puts them back.
	undo := b.notes[len(b.notes)-1]
	if undo.label != "Undo" {
		t.Fatalf("toast %+v", undo)
	}
	undo.action()
	if len(b.review.notes) != 1 || b.review.notes[0] != n || len(b.window.Review) != 1 {
		t.Errorf("undone: %d notes, %d kept", len(b.review.notes), len(b.window.Review))
	}
	b.deleteNote(n)
	b.notes[len(b.notes)-1].action()
	if len(b.review.notes) != 1 {
		t.Errorf("delete undone: %d notes", len(b.review.notes))
	}
}

// N goes from card to card in a diff, and the command from comment to
// comment in files, round past the last.
func TestReviewStep(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	reviewRepo(hs)
	a.openFile(hs.path("t.txt"), true)
	hs.settle()
	d := a.activeEditor()
	for _, l := range []int{20, 4} {
		at, _ := d.buf.Snapshot().LineRange(l)
		hs.place(d, at, at)
		a.run("review.comment")
		hs.settle()
		hs.tt.Type(fmt.Sprint("on ", l))
		hs.key(ui.Cmd, ui.KeyEnter)
	}
	hs.place(d, 0, 0)
	line := func() int { return d.buf.Snapshot().LineAt(d.sel[1]) }
	for _, want := range []int{4, 20, 4} {
		a.run("review.next")
		hs.settle()
		if line() != want {
			t.Errorf("next: line %d, want %d", line(), want)
		}
	}
	a.run("review.previous")
	hs.settle()
	if line() != 20 {
		t.Errorf("previous: line %d", line())
	}
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutInline)
	a.setDiffFull(v, true)
	hs.settle()
	if len(v.cards) != 2 {
		t.Fatalf("cards %v", v.cards)
	}
	v.pane.Focus()
	v.setCursor(0)
	hs.settle()
	for _, want := range []int{0, 1, 0} {
		hs.key(0, ui.KeyN)
		if v.cursor != v.cards[want] {
			t.Errorf("N: cursor %d, want card %d at %d", v.cursor, want, v.cards[want])
		}
	}
	hs.key(ui.Shift, ui.KeyN)
	if v.cursor != v.cards[1] {
		t.Errorf("Shift+N: cursor %d", v.cursor)
	}
}

// Review comments of each kind: on lines removed, long, and outdated.
func TestShotReviewStates(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		name := "review-states-light"
		if dark {
			name = "review-states-dark"
		}
		hs := newHarness(t, dark)
		a := hs.a
		_, changed := reviewRepo(hs)
		a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
		hs.settleGit()
		v := a.activeTab().diff
		a.setDiffLayout(v, layoutInline)
		hs.settle()
		v.pane.Focus()
		removed := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.old == 25 && r.new < 0 })
		v.setCursor(removed)
		hs.key(0, ui.KeyC)
		hs.tt.Type("Why was this removed?")
		hs.key(ui.Cmd, ui.KeyEnter)
		added := slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.new == 2 && r.old < 0 })
		v.setCursor(added)
		hs.key(0, ui.KeyC)
		hs.tt.Type("Spell the numbers out everywhere, or nowhere.\n\nThe file mixes both, and the tests read only digits, so a word breaks them. Keep digits, and say so in a comment at the top of the file so the next change does not do this again.")
		hs.key(ui.Cmd, ui.KeyEnter)
		hs.settle()
		hs.shot(name)
		// The lines of the second comment change under it.
		os.WriteFile(hs.path("t.txt"), []byte(strings.Replace(changed, "line two\n", "line 2\n", 1)), 0o644)
		a.gitChanged()
		hs.settleGit()
		hs.settle()
		hs.shot(name + "-outdated")
	}
}

// Going to a comment on lines removed shows its card in the changes.
func TestReviewGoToRemoved(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	reviewRepo(hs)
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.setDiffLayout(v, layoutInline)
	hs.settle()
	v.pane.Focus()
	v.setCursor(slices.IndexFunc(v.rows, func(r diffRow) bool { return r.kind == rowLine && r.old == 25 && r.new < 0 }))
	hs.key(0, ui.KeyC)
	hs.tt.Type("Why?")
	hs.key(ui.Cmd, ui.KeyEnter)
	n := a.review.notes[0]
	if !n.oldSide {
		t.Fatalf("note %+v", n)
	}
	a.openFile(hs.path("t.txt"), true)
	hs.settle()
	v.setCursor(0)
	a.goToNote(n)
	hs.settle()
	if t2 := a.activeTab(); t2 == nil || t2.diff != v {
		t.Fatal("the changes are not shown")
	}
	if c := v.cursor; c < 0 || v.rows[c].kind != rowComment || a.review.noteAt(v.rows[c].n) != n || v.revealNote != nil {
		t.Errorf("cursor %d on %v", c, v.rows[max(c, 0)].kind)
	}
}
