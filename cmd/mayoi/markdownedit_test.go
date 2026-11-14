package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseMdLine(t *testing.T) {
	for _, c := range []struct {
		text, quote, indent, marker, box, body string
		heading                                int
	}{
		{"plain", "", "", "", "", "plain", 0},
		{"- item", "", "", "-", "", "item", 0},
		{"  * [x] done", "", "  ", "*", "[x]", "done", 0},
		{"12) twelve", "", "", "12)", "", "twelve", 0},
		{"> > - [ ] task", "> > ", "", "-", "[ ]", "task", 0},
		{"## Title", "", "", "", "", "Title", 2},
		{"#tag", "", "", "", "", "#tag", 0},
		{"-dash", "", "", "", "", "-dash", 0},
		{"1.5 apples", "", "", "", "", "1.5 apples", 0},
		{"- ", "", "", "-", "", "", 0},
	} {
		l := parseMdLine(c.text)
		if l.quote != c.quote || l.indent != c.indent || l.marker != c.marker || l.box != c.box || l.body != c.body || l.heading != c.heading {
			t.Errorf("%q: %+v", c.text, l)
		}
	}
	if got := nextMarker("9.", 1); got != "10." {
		t.Errorf("nextMarker 9. = %q", got)
	}
	if got := nextMarker("1)", -1); got != "1)" {
		t.Errorf("nextMarker 1) back = %q", got)
	}
}

func TestMarkdownEnterContinuesLists(t *testing.T) {
	for _, c := range []struct {
		name, text string
		at         int
		want       string
		caret      int
	}{
		{"bullet", "- one", 5, "- one\n- ", 8},
		{"number", "  9. nine", 9, "  9. nine\n  10. ", 16},
		{"task", "- [x] done", 10, "- [x] done\n- [ ] ", 17},
		{"quote", "> said", 6, "> said\n> ", 9},
		{"split", "- onetwo", 5, "- one\n- two", 8},
		{"ends list", "- one\n- ", 8, "- one\n", 6},
		{"outdents", "- one\n  - ", 10, "- one\n- ", 8},
		{"ends quote", "> > ", 4, "> ", 2},
		{"list in quote ends", "> - ", 4, "> ", 2},
		{"before marker", "- one", 0, "\n- one", 1},
		{"in fence", "```\n- code", 10, "```\n- code\n", 11},
		{"rule", "* * *", 5, "* * *\n", 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			hs, d := editHarness(t, "notes.md", c.text)
			hs.place(d, c.at, c.at)
			hs.key(0, ui.KeyEnter)
			expectText(t, d, c.want)
			expectSel(t, d, c.caret, c.caret)
		})
	}
}

func TestEnterOutsideMarkdownKeepsIndentOnly(t *testing.T) {
	hs, d := editHarness(t, "notes.txt", "- one")
	hs.place(d, 5, 5)
	hs.key(0, ui.KeyEnter)
	expectText(t, d, "- one\n")
}

func TestMarkdownInsertLineAboveCarriesMarker(t *testing.T) {
	hs, d := editHarness(t, "notes.md", "3. three")
	hs.place(d, 3, 3)
	hs.key(ui.Cmd|ui.Shift, ui.KeyEnter)
	expectText(t, d, "2. \n3. three")
	expectSel(t, d, 3, 3)
}

func TestMarkdownCmdEnterTogglesTask(t *testing.T) {
	hs, d := editHarness(t, "notes.md", "- one")
	hs.place(d, 5, 5)
	for _, want := range []string{"- [ ] one", "- [x] one", "- one"} {
		hs.key(ui.Cmd, ui.KeyEnter)
		expectText(t, d, want)
		expectSel(t, d, len(want), len(want))
	}

	hs, d = editHarness(t, "notes.md", "plain")
	hs.place(d, 0, 0)
	hs.key(ui.Cmd, ui.KeyEnter)
	expectText(t, d, "- [ ] plain")

	hs, d = editHarness(t, "notes.md", "```\n- code")
	hs.place(d, 10, 10)
	hs.key(ui.Cmd, ui.KeyEnter)
	expectText(t, d, "```\n- code\n")
}

func TestMarkdownTabIndentsItem(t *testing.T) {
	hs, d := editHarness(t, "notes.md", "- one\n- two")
	d.indent = indentStyle{size: 2}
	hs.place(d, 9, 9)
	hs.key(0, ui.KeyTab)
	expectText(t, d, "- one\n  - two")
	expectSel(t, d, 11, 11)
	hs.key(ui.Shift, ui.KeyTab)
	expectText(t, d, "- one\n- two")

	hs, d = editHarness(t, "quote.md", "> - one")
	d.indent = indentStyle{size: 2}
	hs.place(d, 7, 7)
	hs.key(0, ui.KeyTab)
	expectText(t, d, ">   - one")

	// Text that is no item takes a tab at the caret.
	hs, d = editHarness(t, "text.md", "ab")
	d.indent = indentStyle{size: 2}
	hs.place(d, 1, 1)
	hs.key(0, ui.KeyTab)
	expectText(t, d, "a b")
}

func TestMarkdownToggleCommands(t *testing.T) {
	run := func(t *testing.T, text string, anchor, caret int, ids ...string) (*harness, *editor) {
		t.Helper()
		hs, d := editHarness(t, "notes.md", text)
		hs.place(d, anchor, caret)
		for _, id := range ids {
			hs.a.run(id)
			hs.settle()
		}
		return hs, d
	}
	t.Run("task cycles", func(t *testing.T) {
		hs, d := run(t, "buy", 3, 3, "markdown.toggleTask")
		expectText(t, d, "- [ ] buy")
		expectSel(t, d, 9, 9)
		hs.a.run("markdown.toggleTask")
		hs.settle()
		expectText(t, d, "- [x] buy")
		if d.liveTicked != 2 {
			t.Fatalf("ticked %d", d.liveTicked)
		}
		hs.a.run("markdown.toggleTask")
		hs.settle()
		expectText(t, d, "- buy")
	})
	t.Run("list cycles and numbers", func(t *testing.T) {
		hs, d := run(t, "a\n\nb", 0, 4, "markdown.toggleList")
		expectText(t, d, "- a\n\n- b")
		hs.a.run("markdown.toggleList")
		hs.settle()
		expectText(t, d, "1. a\n\n2. b")
		hs.a.run("markdown.toggleList")
		hs.settle()
		expectText(t, d, "a\n\nb")
	})
	t.Run("quote", func(t *testing.T) {
		hs, d := run(t, "a\n\nb", 0, 4, "markdown.toggleQuote")
		expectText(t, d, "> a\n>\n> b")
		hs.a.run("markdown.toggleQuote")
		hs.settle()
		expectText(t, d, "a\n\nb")
	})
	t.Run("heading", func(t *testing.T) {
		hs, d := run(t, "- Title", 7, 7, "markdown.heading2")
		expectText(t, d, "## Title")
		expectSel(t, d, 8, 8)
		hs.a.run("markdown.heading3")
		hs.settle()
		expectText(t, d, "### Title")
		hs.a.run("markdown.heading3")
		hs.settle()
		expectText(t, d, "Title")
	})
	t.Run("clear", func(t *testing.T) {
		_, d := run(t, "> - [x] done\n## H\nplain", 0, 20, "markdown.clearFormatting")
		expectText(t, d, "done\nH\nplain")
	})
	t.Run("embed", func(t *testing.T) {
		hs, d := run(t, "see [[a]] and [[b.png]]", 0, 0, "markdown.toggleEmbed")
		expectText(t, d, "see ![[a]] and ![[b.png]]")
		hs.a.run("markdown.toggleEmbed")
		hs.settle()
		expectText(t, d, "see [[a]] and [[b.png]]")
	})
	t.Run("not markdown", func(t *testing.T) {
		hs, d := editHarness(t, "notes.txt", "buy")
		hs.a.run("markdown.toggleTask")
		hs.settle()
		expectText(t, d, "buy")
	})
}

func TestJoinLines(t *testing.T) {
	hs, d := editHarness(t, "a.txt", "  one  \n   two   three\n\nfour\nfive")
	hs.place(d, 0, 0)
	hs.a.run("sel.joinLines")
	hs.settle()
	expectText(t, d, "  one two three\n\nfour\nfive")
	expectSel(t, d, 5, 5)
	hs.place(d, 0, 22)
	hs.a.run("sel.joinLines")
	hs.settle()
	expectText(t, d, "  one two three four\nfive")
	expectSel(t, d, 0, 20)
}

func TestMarkdownMoveListItem(t *testing.T) {
	for _, c := range []struct {
		name, text string
		from, to   int
		down       bool
		want       string
		selFrom    int
		selTo      int
	}{
		{"down with children", "- a\n  - a1\n- b\n- c", 2, 2, true, "- b\n- a\n  - a1\n- c", 6, 6},
		{"up with children", "- a\n- b\n  - b1\n- c", 6, 6, false, "- b\n  - b1\n- a\n- c", 2, 2},
		{"past children", "- a\n  - a1\n- b", 13, 13, false, "- b\n- a\n  - a1", 2, 2},
		{"nested stays in parent", "- a\n  - a1\n- b", 9, 9, true, "- a\n  - a1\n- b", 9, 9},
		{"first stays", "- a\n- b", 2, 2, false, "- a\n- b", 2, 2},
		{"renumbers", "1. a\n2. b\n3. c", 3, 3, true, "1. b\n2. a\n3. c", 8, 8},
		{"renumbers wider", "9. a\n10. b", 3, 3, true, "9. b\n10. a", 9, 9},
		{"selection of two", "- a\n- b\n- c", 2, 6, true, "- c\n- a\n- b", 6, 10},
		{"keeps blank between", "- a\n\n- b", 2, 2, true, "- b\n\n- a", 7, 7},
		{"in quote", "> - a\n> - b", 4, 4, true, "> - b\n> - a", 10, 10},
		{"line below selected", "- a\n- b\n- c", 0, 4, true, "- b\n- a\n- c", 4, 8},
	} {
		t.Run(c.name, func(t *testing.T) {
			hs, d := editHarness(t, "notes.md", c.text)
			hs.place(d, c.from, c.to)
			key := ui.KeyUp
			if c.down {
				key = ui.KeyDown
			}
			hs.key(ui.Cmd|ui.Shift, key)
			expectText(t, d, c.want)
			expectSel(t, d, c.selFrom, c.selTo)
		})
	}
}

func TestMarkdownMoveKeySelectsOutsideLists(t *testing.T) {
	hs, d := editHarness(t, "notes.md", "text\n- a\n- b")
	hs.place(d, 4, 4)
	hs.key(ui.Cmd|ui.Shift, ui.KeyDown)
	expectText(t, d, "text\n- a\n- b")
	if d.sel == [2]int{4, 4} {
		t.Errorf("selection stayed at %v", d.sel)
	}
}
