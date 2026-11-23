package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

// liveParse parses s for Live Preview, without pictures.
func liveParse(s string) *liveDoc {
	var b ui.TextBuffer
	b.Set(s)
	doc := parseLive(s, "/note.md", "", true, &markdownCache{math: true, mermaid: &mermaidStyle{}})
	doc.snap = b.Snapshot()
	return doc
}

// liveSpans lists a parse's spans as text: the kind, then the runes.
func liveSpans(doc *liveDoc, s string) []string {
	r := []rune(s)
	var out []string
	doc.visit(0, len(r), func(sp *liveSpan) {
		kind := map[liveKind]string{liveStyle: "style", liveMark: "mark", liveObject: "object"}[sp.kind]
		out = append(out, kind+":"+string(r[sp.start:sp.end]))
	})
	return out
}

func hasSpan(spans []string, want string) bool {
	for _, s := range spans {
		if s == want {
			return true
		}
	}
	return false
}

// Live Preview finds the marks of Markdown, which it hides, and the text
// they style.
func TestParseLive(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"# Title #\n", []string{"style:# Title #", "mark:# ", "mark: #"}},
		{"a **bold** and *it* é\n", []string{"style:bold", "mark:**", "style:it", "mark:*"}},
		{"中文**粗体**结束\n", []string{"style:粗体", "mark:**"}},
		{"use `code` here\n", []string{"style:code", "mark:`"}},
		{"see [label](https://x.dev/a_(b)) ok\n", []string{"style:label", "mark:[", "mark:](https://x.dev/a_(b)", "object:)"}},
		{"<https://x.dev> and https://y.dev\n", []string{"style:https://x.dev", "mark:<", "style:https://y.dev"}},
		{"to [[note#h|Label]] now\n", []string{"style:Label", "mark:[[note#h|", "mark:]]"}},
		{"~~gone~~ and ==lit==\n", []string{"style:gone", "mark:~~", "style:lit", "mark:=="}},
		{"- one\n  - two\n1. three\n", []string{"object:-", "style:1."}},
		{"- [ ] todo\n- [x] done\n", []string{"object:[ ]", "object:[x]", "mark:- "}},
		{"- item\n  more of it\n", []string{"object:-", "style:  "}},
		{"> quoted\n> more\n", []string{"object:> ", "style:quoted"}},
		{"> [!NOTE]\n> text\n", []string{"object:[!NOTE]"}},
		{"```go\nx := 1\n```\n", []string{"mark:```", "style:go"}},
		{"a $x^2$ b\n", []string{"object:$x^2$"}},
		{"---\nkey: v\n---\n# T\n", []string{"style:---\nkey: v\n---", "mark:# "}},
		{"para\n\n***\n", []string{"mark:***"}},
		{"a \\*not\\* it\n", []string{"mark:\\"}},
		{"see #tag/sub, (#two) end\n", []string{"style:#tag/sub", "style:#two"}},
		{"said[^1] so\n\n[^1]: x\n", []string{"object:[^1]", "object:[^1]: ", "style:[^1]: x"}},
		{"> [!tip] Own title\n> body\n", []string{"object:[!tip]", "style:[!tip] Own title"}},
		{"> [!info]-\n> body\n", []string{"object:[!info]-"}},
	} {
		got := liveSpans(liveParse(tc.src), tc.src)
		for _, w := range tc.want {
			if !hasSpan(got, w) {
				t.Errorf("%q: no %q in %q", tc.src, w, got)
			}
		}
	}
}

// Tables, diagrams and formulas in display style are blocks rendered over
// their lines; a block of code is a card behind its lines.
func TestParseLiveBlocks(t *testing.T) {
	src := "a\n\n| x | y |\n|---|---|\n| 1 | 2 |\n\n$$\nx^2\n$$\n\n```js\nf()\n```\n"
	doc := liveParse(src)
	r := []rune(src)
	var blocks []string
	for _, b := range doc.blocks {
		blocks = append(blocks, string(r[b.start:b.end]))
		if len(b.blocks) == 0 {
			t.Errorf("block %q rendered nothing", string(r[b.start:b.end]))
		}
	}
	if fmt.Sprint(blocks) != fmt.Sprint([]string{"| x | y |\n|---|---|\n| 1 | 2 |", "$$\nx^2\n$$"}) {
		t.Errorf("blocks %q", blocks)
	}
	var code []string
	for _, d := range doc.decos {
		if d.kind == decoCode {
			code = append(code, string(r[d.start:d.end]))
		}
	}
	if len(code) != 1 || code[0] != "```js\nf()\n```" {
		t.Errorf("code cards %q", code)
	}
}

// The spans of a parse follow the edits made since: those before stay,
// those after move, a style grows with what is typed at its end, and a
// mark does not, so that typing after ** is not hidden.
func TestLiveMapTo(t *testing.T) {
	src := "# Head\nsome **bold** text\n"
	var b ui.TextBuffer
	b.Set(src)
	doc := parseLive(src, "/note.md", "", false, &markdownCache{})
	doc.snap = b.Snapshot()
	for _, tc := range []struct {
		at   int
		with string
		want []string
	}{
		{6, "ing", []string{"style:# Heading", "mark:# "}}, // typed at the heading's end
		{18, "er", []string{"style:bolder", "mark:**"}},    // typed at bold's end
		{0, "x\n", []string{"style:# Head", "style:bold"}}, // a line above
	} {
		b.Set(src)
		doc.snap = b.Snapshot()
		b.Replace(tc.at, tc.at, tc.with)
		edited := b.Snapshot()
		got := liveSpans(doc.mapTo(edited), edited.String())
		for _, w := range tc.want {
			if !hasSpan(got, w) {
				t.Errorf("%q at %d: no %q in %q", tc.with, tc.at, w, got)
			}
		}
	}
	// Typed after the closing mark, the text is not in it.
	b.Set(src)
	doc.snap = b.Snapshot()
	b.Replace(20, 20, "!")
	after := b.Snapshot()
	r := []rune(after.String())
	doc.mapTo(after).visit(0, len(r), func(sp *liveSpan) {
		if sp.kind == liveMark && strings.Contains(string(r[sp.start:sp.end]), "!") {
			t.Errorf("the mark %q took what was typed after it", string(r[sp.start:sp.end]))
		}
	})
}

func TestSnapDiff(t *testing.T) {
	for _, tc := range []struct {
		a, b    string
		p, q, r int
	}{
		{"hello", "hello", 5, 5, 5},
		{"hello", "help", 3, 5, 4},
		{"abc", "xabc", 0, 0, 1},
		{"é中文x", "é中x", 2, 3, 2},
		{strings.Repeat("a", 9000) + "b" + strings.Repeat("c", 9000), strings.Repeat("a", 9000) + "dd" + strings.Repeat("c", 9000), 9000, 9001, 9002},
	} {
		var x, y ui.TextBuffer
		x.Set(tc.a)
		y.Set(tc.b)
		if p, q, r := snapDiff(x.Snapshot(), y.Snapshot()); p != tc.p || q != tc.q || r != tc.r {
			t.Errorf("%.12q → %.12q: %d %d %d, want %d %d %d", tc.a, tc.b, p, q, r, tc.p, tc.q, tc.r)
		}
	}
}

func TestLiveTask(t *testing.T) {
	for _, tc := range []struct {
		line string
		at   int
		done bool
	}{
		{"- [ ] a", 3, false},
		{"  * [x] b", 5, true},
		{"> 1. [X] c", 6, true},
		{"- a [ ]", -1, false},
		{"text", -1, false},
	} {
		if at, done := liveTask(tc.line); at != tc.at || done != tc.done {
			t.Errorf("%q: %d %v", tc.line, at, done)
		}
	}
}

// Live Preview opens as the setting says: its text styled, a task's box
// that a click checks, a table rendered over its lines until the caret
// goes in, and its marks shown where the caret is.
func TestLivePreview(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "# Title\n\nSome **bold** text.\n\n- [ ] task\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nend\n" + strings.Repeat("more\n", 80)
	path := hs.path("live.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("Live Preview parsed", func() bool {
		l := d.document.live
		return a.livePreview(d) && l != nil && l.doc != nil && !l.quick && len(d.liveHits) == 1
	})
	// Laid out and measured, not only made: a view made in the last frame
	// has no place yet, and the wheel would go elsewhere.
	var table *liveView
	hs.until("the table rendered", func() bool {
		for _, lv := range d.livePane.views {
			table = lv
		}
		return len(d.livePane.views) == 1 && table.measured && table.at.H > 0
	})
	// Faded in.
	hs.settle()
	if table.frame != a.frame {
		t.Fatal("the table is not rendered")
	}

	// The wheel over the table scrolls the text, as over its text.
	before := d.vscroll.Y
	hs.tt.Scroll(table.at.X+table.at.W/2, table.at.Y+table.at.H/2, 0, 40)
	hs.settle()
	if d.vscroll.Y <= before {
		t.Errorf("the wheel over the table left the text at %v, from %v", d.vscroll.Y, before)
	}
	d.vscroll.Y = before
	hs.settle()

	// A click on the task's box checks it, as one step of undo.
	h := d.liveHits[0]
	hs.tt.ClickAt(d.liveAt.X+h.r.X+h.r.W/2, d.liveAt.Y+h.r.Y+h.r.H/2)
	hs.settle()
	if got := d.buf.String(); !strings.Contains(got, "- [x] task") {
		t.Fatalf("the click left the task as %q", got)
	}
	if active, _ := hs.tt.TextInputActive(); !active {
		t.Error("the click left the keyboard elsewhere")
	}
	hs.key(ui.Cmd, ui.KeyZ)
	if got := d.buf.String(); !strings.Contains(got, "- [ ] task") {
		t.Fatalf("undo left the task as %q", got)
	}

	// The caret in the table shows its source.
	at := strings.Index(src, "| 1")
	hs.place(d, at, at)
	hs.settle()
	if table.frame == a.frame {
		t.Error("the table stayed rendered with the caret in it")
	}

	// Switched to its source, the text shows as code again.
	a.run("view.toggleLivePreview")
	hs.settle()
	if a.livePreview(d) || d.gutterW == 0 {
		t.Error("the source has no line numbers")
	}
}

// liveBench is a note of some 24,000 lines, of every kind of Markdown
// Live Preview styles.
func liveBench() string {
	part := "## Section\n\nSome **bold**, *italic* and `code` text with a [link](https://example.com) and ==marks==.\n\n" +
		"- [ ] a task\n- [x] done\n- an item\n  - nested\n\n> A quote of some length.\n\n```go\nfunc f() int { return 1 }\n```\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n$$\nx^2\n$$\n\n"
	return strings.Repeat(part, 1000)
}

// The parse in the background, a moment after each edit.
func BenchmarkParseLive(b *testing.B) {
	src := liveBench()
	cache := &markdownCache{math: true}
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		parseLive(src, "/note.md", "", true, cache)
	}
}

// The quick parse of the first frame.
func BenchmarkParseLiveQuick(b *testing.B) {
	src := liveBench()
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		parseLive(src, "/note.md", "", false, &markdownCache{math: true})
	}
}

// The spans following a key typed, until the next parse.
func BenchmarkLiveMapTo(b *testing.B) {
	src := liveBench()
	var buf ui.TextBuffer
	buf.Set(src)
	doc := parseLive(src, "/note.md", "", true, &markdownCache{math: true})
	doc.snap = buf.Snapshot()
	at := len(src) / 2
	buf.Replace(at, at, "a")
	edited := buf.Snapshot()
	for b.Loop() {
		doc.mapTo(edited)
	}
}

// A task's box checked shows checked at once, before the text is parsed
// again, rather than as its [x].
func TestLiveMapToTask(t *testing.T) {
	src := "- [ ] task\n"
	var b ui.TextBuffer
	b.Set(src)
	doc := parseLive(src, "/note.md", "", false, &markdownCache{})
	doc.snap = b.Snapshot()
	b.Replace(3, 4, "x")
	var got []liveObj
	doc.mapTo(b.Snapshot()).visit(0, b.Snapshot().Len(), func(s *liveSpan) {
		if s.kind == liveObject {
			got = append(got, s.obj)
		}
	})
	if len(got) != 1 || got[0] != objTaskDone {
		t.Fatalf("the box checked is %v, not checked", got)
	}
}

// A parse draws again only the blocks edited since the last: the others
// it takes as they were.
func TestParseLiveKeepsBlocks(t *testing.T) {
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n\n$$\nx^2\n$$\n\ntext\n"
	cache := &markdownCache{math: true}
	first := parseLive(src, "/note.md", "", true, cache)
	again := parseLive(src+"more\n", "/note.md", "", true, cache)
	if len(first.blocks) != 2 || len(again.blocks) != 2 {
		t.Fatalf("blocks: %d, then %d", len(first.blocks), len(again.blocks))
	}
	for i := range first.blocks {
		if &first.blocks[i].blocks[0] != &again.blocks[i].blocks[0] {
			t.Errorf("block %d was drawn again", i)
		}
	}
	if len(cache.formulas) != 1 {
		t.Errorf("the cache keeps %d formulas, not the one", len(cache.formulas))
	}
}

// A bullet and a task's box keep their room as the caret comes to them
// and their source shows, so the text after them holds still; the line
// that continues an item stands under its text.
func TestLiveMarkersHoldStill(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "intro\n\n- bullet item\n  continued line\n- [ ] task item\n\n1. [ ] ordered task\n\nend\n"
	path := hs.path("marks.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("Live Preview parsed", func() bool {
		l := d.document.live
		return a.livePreview(d) && l != nil && l.doc != nil && !l.quick
	})
	runeOf := func(s string) int { return len([]rune(src[:strings.Index(src, s)])) }
	x := func(at int) float32 {
		x, _, _, _ := d.lines.Caret(at)
		return x
	}
	hs.place(d, 0, 0)
	hs.settle()
	if b, c := x(runeOf("bullet item")), x(runeOf("continued")); b-c > 0.5 || c-b > 0.5 {
		t.Errorf("the continued line starts at %v, its item's text at %v", c, b)
	}
	for _, tc := range []struct{ marker, text string }{{"- bullet", "bullet item"}, {"- [ ]", "task item"}, {"[ ] ordered", "ordered task"}} {
		hs.place(d, 0, 0)
		hs.settle()
		before := x(runeOf(tc.text))
		if before <= 0 {
			t.Fatalf("%q laid out at %v", tc.text, before)
		}
		at := runeOf(tc.marker)
		hs.place(d, at, at)
		hs.settle()
		if after := x(runeOf(tc.text)); after-before > 0.5 || before-after > 0.5 {
			t.Errorf("%q moved from %v to %v as the caret came to its marker", tc.text, before, after)
		}
	}
}

// A task's text is struck through as its box shows checked, before the
// text is parsed again, and fades back as it is unchecked.
func TestLiveStrike(t *testing.T) {
	a := &app{pal: darkPalette()}
	src := "- [x] done it\n"
	var b ui.TextBuffer
	b.Set(src)
	snap := b.Snapshot()
	box := &liveSpan{start: 2, end: 5, kind: liveObject, obj: objTaskDone}
	d := &editor{}
	now := time.Now()
	r, ok := a.liveStrike(d, snap, box, now)
	if !ok || !r.Strikethrough || snap.Slice(r.Start, r.End) != "done it" {
		t.Fatalf("the task done struck %v %q", ok, snap.Slice(r.Start, r.End))
	}
	box.obj = objTask
	if _, ok := a.liveStrike(d, snap, box, now); ok {
		t.Error("a task to do is struck through")
	}
	d.liveTicked, d.liveTickAt = 2, now
	if r, ok := a.liveStrike(d, snap, box, now.Add(motionShow/2)); !ok || r.DecorationColor.A == 0 {
		t.Error("a task just unchecked is not fading its strike out")
	}
}

// A note split into two columns draws its table in each, each its own
// view measured, rather than one view bound in both, which crashed.
func TestLiveBlockInSplitColumns(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	left := openLive(t, hs, "split.md", "intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nend\n", 1)
	a.splitRight()
	right := a.activeEditor()
	hs.settle()
	if right == left || right.document != left.document || !a.livePreview(right) {
		t.Fatal("the split column does not show the note in Live Preview")
	}
	hs.until("both tables measured", func() bool {
		for _, d := range []*editor{left, right} {
			if len(d.livePane.views) != 1 {
				return false
			}
			for _, lv := range d.livePane.views {
				if !lv.measured || lv.at.H <= 0 || lv.frame != a.frame {
					return false
				}
			}
		}
		return true
	})
	for k, lv := range left.livePane.views {
		if right.livePane.views[k] == lv {
			t.Fatal("the columns share the table's view")
		}
	}
}

// The text below a table glides to its place as the caret goes into the
// table and its source shows, and back as the caret leaves, rather than
// jumps.
func TestLiveBlockGlides(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n| 5 | 6 |\n\nend\n"
	path := hs.path("glide.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	l := func() *liveState { return d.document.live }
	hs.until("the table rendered", func() bool { return l() != nil && l().doc != nil && !l().quick && len(d.livePane.views) == 1 })
	hs.settle()
	end := d.buf.Snapshot().LineCount() - 2
	drawn := d.lines.Top(end)

	at := len([]rune(src[:strings.Index(src, "| 3")]))
	d.selectRange(at, at, false)
	step(hs.tt)
	step(hs.tt)
	first := d.lines.Top(end)
	hs.settle()
	source := d.lines.Top(end)
	if source >= drawn-1 {
		t.Fatalf("the table's source is as high as its drawing: %v, %v", source, drawn)
	}
	if first < drawn-(drawn-source)/2 {
		t.Errorf("the text below jumped from %v to %v, not gliding to %v", drawn, first, source)
	}

	d.selectRange(0, 0, false)
	step(hs.tt)
	step(hs.tt)
	if back := d.lines.Top(end); back > source+(drawn-source)/2 {
		t.Errorf("the text below jumped back from %v to %v, not gliding to %v", source, back, drawn)
	}
	hs.settle()
	if back := d.lines.Top(end); back < drawn-1 || back > drawn+1 {
		t.Errorf("the text below settled at %v, not %v", back, drawn)
	}
}

// A # is a tag at a word's start, of more than digits; not in code, a
// heading's marks or a link's text.
func TestParseLiveTags(t *testing.T) {
	src := "# C#sharp #123 `#code` [#link](x) x#y #ok #a1\n"
	r := []rune(src)
	var tags []string
	for _, sp := range liveParse(src).spans {
		if sp.look.tag {
			tags = append(tags, string(r[sp.start:sp.end]))
		}
	}
	if fmt.Sprint(tags) != "[#ok #a1]" {
		t.Errorf("tags %q", tags)
	}
}

// A wiki link to a note there is not reads fainter than one to a note
// there is, as in Obsidian.
func TestParseLiveUnresolved(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/there.md", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "[[there]] and [[nowhere]]\n"
	doc := parseLive(src, dir+"/note.md", dir, true, &markdownCache{})
	r := []rune(src)
	looks := map[string]liveColor{}
	for _, sp := range doc.spans {
		if sp.kind == liveStyle {
			looks[string(r[sp.start:sp.end])] = sp.look.color
		}
	}
	if looks["there"] != liveLink || looks["nowhere"] != liveUnresolved {
		t.Errorf("looks %v", looks)
	}
	// The Reading view shows them alike.
	missing := map[string]bool{}
	for _, run := range parseMarkdown([]byte(src), dir+"/note.md", dir, nil)[0].runs {
		missing[run.text] = run.missing
	}
	if missing["there"] || !missing["nowhere"] {
		t.Errorf("Reading view: missing %v", missing)
	}
}

// A callout of - shows its title alone in Live Preview, its lines below
// taking no room; its chevron opens it, gliding the text below down, and
// the caret in it shows its source.
func TestLiveCalloutFolds(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "intro\n\n> [!faq]- Why?\n> One.\n> Two.\n> Three.\n\nend\n"
	path := hs.path("fold.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	hs.settle()
	end := d.buf.Snapshot().LineCount() - 2
	title := 2
	folded := d.lines.Top(end) - d.lines.Top(title+1)
	if lineH := d.lines.Top(1) - d.lines.Top(0); folded > lineH*1.5 {
		t.Fatalf("folded, the callout's lines take %v, more than its blank line", folded)
	}
	var chevron *liveHit
	for i := range d.liveHits {
		if d.liveHits[i].fold != 0 {
			chevron = &d.liveHits[i]
		}
	}
	if chevron == nil || chevron.key != "[!faq]- Why?" {
		t.Fatalf("no chevron: %+v", d.liveHits)
	}
	r := chevron.r
	if !a.liveClick(d, r.X+r.W/2, r.Y+r.H/2) {
		t.Fatal("the chevron took no click")
	}
	step(hs.tt)
	step(hs.tt)
	gliding := d.lines.Top(end) - d.lines.Top(title+1)
	hs.settle()
	open := d.lines.Top(end) - d.lines.Top(title+1)
	if open < folded+3*(d.lines.Top(1)-d.lines.Top(0))-1 {
		t.Fatalf("opened, its lines take %v", open)
	}
	if gliding <= folded || gliding >= open {
		t.Errorf("the text below jumped from %v to %v, not gliding to %v", folded, gliding, open)
	}
	// Folded again, then the caret in it: its source shows.
	a.toggleCallout(d.document, chevron.key, chevron.fold)
	hs.settle()
	at := len([]rune(src[:strings.Index(src, "Two")]))
	d.selectRange(at, at, false)
	hs.settle()
	if got := d.lines.Top(end) - d.lines.Top(title+1); got < open-1 {
		t.Errorf("with the caret in it, its lines take %v, not %v", got, open)
	}
}

// A note embedded shows rendered in Live Preview, its source a wiki link,
// not code, and drawn again once the note changes.
func TestParseLiveEmbeds(t *testing.T) {
	dir := t.TempDir()
	other := dir + "/other.md"
	os.WriteFile(other, []byte("# Other\n\nHello.\n"), 0o644)
	src := "Intro.\n\n![[other]]\n\n![[nowhere]]\n"
	cache := &markdownCache{}
	doc := parseLive(src, dir+"/note.md", dir, true, cache)
	if len(doc.blocks) != 1 {
		t.Fatalf("blocks %+v", doc.blocks)
	}
	b := doc.blocks[0]
	if !b.embed || b.blocks[0].kind != markdownEmbedTitle || markdownRunsText(b.blocks[2].runs) != "Hello." {
		t.Fatalf("embed %+v", b)
	}
	for _, sp := range doc.spans {
		if sp.look.code && sp.start < b.end && sp.end > b.start {
			t.Errorf("its source is code: %+v", sp)
		}
	}
	os.WriteFile(other, []byte("# Other\n\nChanged, longer.\n"), 0o644)
	os.Chtimes(other, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	again := parseLive(src, dir+"/note.md", dir, true, cache)
	if again.blocks[0].key == b.key || markdownRunsText(again.blocks[0].blocks[2].runs) != "Changed, longer." {
		t.Errorf("not drawn again: %q", markdownRunsText(again.blocks[0].blocks[2].runs))
	}
}

// Tags, footnotes and the ids of blocks read alike in Live Preview and the
// Reading view: the same #tags are pills, a footnote's reference is its
// number, and an id, ^id, reads faint in one and hides in the other.
func TestLiveAndReadingAgree(t *testing.T) {
	src := "A #ok and C#x `#c` [#l](x) \\#no #a1 b[^note].\n\nEnd of it ^blk-1\n\n[^note]: The note.\n"
	r := []rune(src)
	doc := liveParse(src)
	var tags []string
	note, id := "", ""
	for _, sp := range doc.spans {
		switch {
		case sp.look.tag:
			tags = append(tags, string(r[sp.start:sp.end]))
		case sp.obj == objNote && sp.drawn != nil:
			note = sp.drawn.note
		case sp.kind == liveStyle && sp.look.color == liveFaint:
			id = string(r[sp.start:sp.end])
		}
	}
	if fmt.Sprint(tags) != "[#ok #a1]" || note != "1" || id != "^blk-1" {
		t.Errorf("Live Preview: tags %q, note %q, id %q", tags, note, id)
	}
	blocks := parseMarkdown([]byte(src), "", "", nil)
	tags = nil
	for _, run := range blocks[0].runs {
		if run.tag {
			tags = append(tags, run.text)
		}
	}
	if fmt.Sprint(tags) != "[#ok #a1]" || !strings.Contains(markdownRunsText(blocks[0].runs), "#no #a1 b¹.") {
		t.Errorf("Reading view: tags %q in %q", tags, markdownRunsText(blocks[0].runs))
	}
	if got := markdownRunsText(blocks[1].runs); got != "End of it" {
		t.Errorf("Reading view: the block's id shows: %q", got)
	}
}

// The id of a block is ^id at the end of its line, after white space or
// alone.
func TestMarkdownBlockID(t *testing.T) {
	for line, want := range map[string]int{
		"Text ^abc":  5,
		"^abc":       0,
		"Text^abc":   -1,
		"Text ^":     -1,
		"Text ^a_b":  -1,
		"Text ^A-9z": 5,
		"Text":       -1,
	} {
		if got := markdownBlockID([]byte(line)); got != want {
			t.Errorf("%q: %d, want %d", line, got, want)
		}
	}
}

// ![[note#^id]] shows the block of the id: a paragraph, an item with the
// items under it, or the block above a line of ^id alone.
func TestMarkdownBlockText(t *testing.T) {
	note := "# Title\nFirst line\nsecond line ^para\n\n- one\n- two ^item\n  - under\n- three\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n^table\n"
	for id, want := range map[string]string{
		"para":    "First line\nsecond line\n",
		"item":    "- two\n  - under\n",
		"table":   "| a | b |\n| - | - |\n| 1 | 2 |\n",
		"missing": "",
	} {
		got, ok := markdownBlockText([]byte(note), id)
		if string(got) != want || ok != (want != "") {
			t.Errorf("^%s: %q %v, want %q", id, got, ok, want)
		}
	}
	dir := t.TempDir()
	os.WriteFile(dir+"/other.md", []byte(note), 0o644)
	blocks := parseMarkdown([]byte("![[other#^item]]\n"), dir+"/note.md", dir, nil)
	var got []string
	for _, b := range blocks {
		if b.kind == markdownEmbedTitle {
			got = append(got, "["+b.title+"]")
		} else {
			got = append(got, markdownRunsText(b.runs))
		}
	}
	if strings.Join(got, "|") != "[other › ^item]|two|under" {
		t.Errorf("embedded %q", got)
	}
}

// A callout folded keeps folded as its title is edited, in Live Preview,
// unless its - or + changes, which says anew how it shows.
func TestLiveCalloutKeepsFoldAsRetitled(t *testing.T) {
	doc := &document{folds: map[string]bool{"[!faq]- Why?": false}, foldKey: "[!faq]- Why?"}
	l := &liveState{}
	liveRetitle(doc, l, 10, "[!faq]- Why?")
	liveRetitle(doc, l, 10, "[!faq]- Why not?")
	if folded, ok := doc.folds["[!faq]- Why not?"]; !ok || folded || doc.foldKey != "[!faq]- Why not?" {
		t.Errorf("retitled, it lost whether it is folded: %v", doc.folds)
	}
	if _, ok := doc.folds["[!faq]- Why?"]; ok {
		t.Errorf("the old title is kept: %v", doc.folds)
	}
	liveRetitle(doc, l, 10, "[!faq]+ Why not?")
	if _, ok := doc.folds["[!faq]+ Why not?"]; ok {
		t.Errorf("its + took how its - showed: %v", doc.folds)
	}
}

// The underline of a link of Live Preview fills in under the pointer,
// over motionHover, and fades as it leaves.
func TestLiveLinkHover(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "Some text before [a link here](https://example.com) and after.\n"
	path := hs.path("hover.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	hs.settle()
	_, y0, h, ok := d.lines.Caret(0)
	if !ok {
		t.Fatal("not laid out")
	}
	y := d.liveAt.Y + editorPadY + y0 + h/2 - d.vscroll.Y
	at := len([]rune("Some text before [")) + 1
	for x := d.liveAt.X; x < d.liveAt.X+1200 && d.liveHover != at; x += 6 {
		hs.tt.Move(x, y)
		step(hs.tt)
	}
	if d.liveHover != at {
		t.Fatalf("over the link, hover is %d, not %d", d.liveHover, at)
	}
	hs.settle()
	// Its source hidden, a click opens it: the pointer is a hand.
	if hs.tt.Cursor() != ui.CursorPointer {
		t.Errorf("over the link, the pointer is %v", hs.tt.Cursor())
	}
	// Its source showing, a click puts the caret in it, and ⌘ makes the
	// pointer a hand, as a ⌘-click follows it.
	d.selectRange(at+1, at+1, false)
	hs.settle()
	// Its [ shows, the text further along.
	for x := d.liveAt.X; x < d.liveAt.X+1200 && d.liveHover != at; x += 6 {
		hs.tt.Move(x, y)
		step(hs.tt)
	}
	hs.settle()
	if d.liveHover != at {
		t.Fatalf("over the link showing its source, hover is %d, not %d", d.liveHover, at)
	}
	if hs.tt.Cursor() == ui.CursorPointer {
		t.Error("over the link showing its source without Cmd, the pointer is a hand")
	}
	hs.tt.HoldModifiers(ui.Cmd)
	hs.settle()
	if hs.tt.Cursor() != ui.CursorPointer {
		t.Errorf("over the link with Cmd held, the pointer is %v", hs.tt.Cursor())
	}
	hs.tt.HoldModifiers(0)
	hs.settle()
	hs.tt.Move(d.liveAt.X+2, y+h*4)
	step(hs.tt)
	if d.liveHover != 0 || d.liveHoverWas != at {
		t.Errorf("off the link, hover is %d, was %d", d.liveHover, d.liveHoverWas)
	}
}

// A click on a wiki link of Live Preview whose source is hidden opens its
// note, as in Obsidian, at its block; one to a note not there makes it.
func TestLiveLinkClick(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	other := hs.path("other.md")
	os.WriteFile(other, []byte("# Other\n\nFirst.\n\nThe block. ^b\n"), 0o644)
	src := "See [[other#^b|the other]] and [[Missing]] too.\n"
	path := hs.path("click.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	click := func(label string) {
		t.Helper()
		a.openFile(path, true)
		d := a.activeEditor()
		hs.until("parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
		d.selectRange(0, 0, false)
		hs.settle()
		at := strings.Index(src, label) + 1
		_, y0, h, ok := d.lines.Caret(at - 1)
		if !ok {
			t.Fatal("not laid out")
		}
		y := d.liveAt.Y + editorPadY + y0 + h/2 - d.vscroll.Y
		x := d.liveAt.X
		for ; x < d.liveAt.X+1200 && d.liveHover != at; x += 6 {
			hs.tt.Move(x, y)
			step(hs.tt)
		}
		if d.liveHover != at {
			t.Fatalf("over %q, hover is %d, not %d", label, d.liveHover, at)
		}
		hs.tt.ClickAt(x, y)
		hs.settle()
		if d.sel != [2]int{0, 0} {
			t.Errorf("clicking %q moved the caret to %v", label, d.sel)
		}
	}

	click("the other")
	hs.until("the other note open at its block", func() bool {
		d := a.activeEditor()
		if d == nil || d.path != other || d.unread() {
			return false
		}
		s, _ := d.buf.Snapshot().LineRange(4)
		return d.sel == [2]int{s, s}
	})

	click("Missing")
	missing := hs.path("Missing.md")
	hs.until("the missing note made and open", func() bool {
		d := a.activeEditor()
		return d != nil && d.path == missing
	})
	if !isFile(missing) {
		t.Error("the missing note was not made")
	}
}

// openLive opens src as a note in Live Preview, once parsed with its
// blocks drawn, and returns its editor.
func openLive(t *testing.T, hs *harness, name, src string, blocks int) *editor {
	t.Helper()
	path := hs.path(name)
	os.WriteFile(path, []byte(src), 0o644)
	hs.a.settings.MarkdownMode = "live"
	hs.a.openFile(path, true)
	d := hs.a.activeEditor()
	hs.until("Live Preview parsed", func() bool {
		l := d.document.live
		return l != nil && l.doc != nil && !l.quick && len(d.livePane.views) == blocks
	})
	hs.settle()
	return d
}

// The caret leaving a block before the text below it glided down turns
// it back from where it is, and going in again while it glides back up
// does too: it never jumps.
func TestLiveGlideTurnsBack(t *testing.T) {
	hs := newHarness(t, true)
	src := "intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n| 5 | 6 |\n\nend\n"
	d := openLive(t, hs, "turn.md", src, 1)
	end := d.buf.Snapshot().LineCount() - 2
	drawn := d.lines.Top(end)
	at := len([]rune(src[:strings.Index(src, "| 3")]))
	for range 2 {
		d.selectRange(at, at, false)
		step(hs.tt)
		step(hs.tt)
		step(hs.tt)
		was := d.lines.Top(end)
		d.selectRange(0, 0, false)
		step(hs.tt)
		if now := d.lines.Top(end); now < was-8 || now > was+8 {
			t.Fatalf("leaving mid-glide, the text below jumped from %v to %v", was, now)
		}
		step(hs.tt)
		step(hs.tt)
		was = d.lines.Top(end)
		d.selectRange(at, at, false)
		step(hs.tt)
		if now := d.lines.Top(end); now < was-8 || now > was+8 {
			t.Fatalf("going in mid-glide, the text below jumped from %v to %v", was, now)
		}
		d.selectRange(0, 0, false)
		hs.settle()
	}
	if back := d.lines.Top(end); back < drawn-1 || back > drawn+1 {
		t.Errorf("the text below settled at %v, not %v", back, drawn)
	}
}

// A diagram with text right below it, no blank line between, glides as
// the caret goes in: its last line holds the room its drawing gives up.
func TestLiveGlideWithoutBlank(t *testing.T) {
	hs := newHarness(t, true)
	src := "intro\n\n```mermaid\ngraph TD\n  A --> B\n  B --> C\n  C --> D\n```\nright below\n\nend\n"
	d := openLive(t, hs, "diagram.md", src, 1)
	below := d.buf.Snapshot().LineCount() - 4
	drawn := d.lines.Top(below)
	at := len([]rune(src[:strings.Index(src, "A -->")]))
	d.selectRange(at, at, false)
	step(hs.tt)
	step(hs.tt)
	first := d.lines.Top(below)
	hs.settle()
	source := d.lines.Top(below)
	if source >= drawn-1 {
		t.Fatalf("the diagram's source is as high as its drawing: %v, %v", source, drawn)
	}
	if first < drawn-(drawn-source)/2 {
		t.Errorf("the text below jumped from %v to %v, not gliding to %v", drawn, first, source)
	}
}

// The wheel over a diagram's drawing scrolls the text, as over its text,
// and a click on it edits its source.
func TestLiveDiagramInput(t *testing.T) {
	hs := newHarness(t, true)
	src := "intro\n\n```mermaid\ngraph TD\n  A --> B\n  B --> C\n```\n\nend\n" + strings.Repeat("more\n", 80)
	d := openLive(t, hs, "diagram-input.md", src, 1)
	var lv *liveView
	for _, v := range d.livePane.views {
		lv = v
	}
	x, y := lv.at.X+lv.at.W/2, lv.at.Y+lv.at.H/2
	before := d.vscroll.Y
	hs.tt.Scroll(x, y, 0, 40)
	hs.settle()
	if d.vscroll.Y <= before {
		t.Errorf("the wheel over the diagram left the text at %v, from %v", d.vscroll.Y, before)
	}
	d.vscroll.Y = before
	hs.settle()

	hs.tt.ClickAt(lv.at.X+lv.at.W/2, lv.at.Y+lv.at.H/2)
	hs.settle()
	if at := len([]rune(src[:strings.Index(src, "```")])); d.sel != [2]int{at, at} {
		t.Errorf("the click on the diagram left the selection at %v, not %v", d.sel, at)
	}
}

// The buttons over a diagram in Live Preview do what they say, each once,
// from a press and a release where they show, the diagram drawn still and
// the caret where it was: not a press on the block, which edits its source.
// Show Source shows its source in the card, whose Show Diagram brings the
// drawing back; the wheel over the buttons scrolls the text.
func TestLiveDiagramToolbar(t *testing.T) {
	src := "intro\n\n```mermaid\ngraph TD\n  A --> B\n  B --> C\n```\n\nend\n" + strings.Repeat("more\n", 80)
	code := "graph TD\n  A --> B\n  B --> C"
	open := func(t *testing.T) (*harness, *editor, *liveView) {
		hs := newHarness(t, true)
		d := openLive(t, hs, "toolbar.md", src, 1)
		lv := tableView(d)
		hs.tt.Move(lv.at.X+lv.at.W/2, lv.at.Y+lv.at.H/2)
		hs.settle()
		return hs, d, lv
	}
	click := func(t *testing.T, hs *harness, d *editor, lv *liveView, label string) {
		t.Helper()
		r, ok := hs.tt.Find(label)
		if !ok || r.W <= 0 {
			t.Fatalf("no %q over the diagram", label)
		}
		sel := d.sel
		hs.tt.Press(r.X+r.W/2, r.Y+r.H/2)
		if _, ok := hs.tt.Find(label); !ok || d.sel != sel {
			t.Fatalf("pressed, %q went, the caret at %v", label, d.sel)
		}
		hs.tt.Release(r.X+r.W/2, r.Y+r.H/2)
		hs.settle()
		if d.sel != sel || lv.frame != hs.a.frame {
			t.Fatalf("%q moved the caret to %v or showed the block's source", label, d.sel)
		}
	}
	t.Run("open", func(t *testing.T) {
		hs, d, lv := open(t)
		click(t, hs, d, lv, locales.OpenDiagram())
		if hs.a.diagram == nil || !hs.tt.HasText(locales.CloseDiagram()) {
			t.Fatal("Open Diagram did not open it over the window")
		}
	})
	t.Run("copy", func(t *testing.T) {
		hs, d, lv := open(t)
		click(t, hs, d, lv, locales.CopyCode())
		if got := hs.tt.Clipboard(); got != code {
			t.Fatalf("Copy Code copied %q", got)
		}
		click(t, hs, d, lv, locales.CopyAsImage())
		if !bytes.HasPrefix(hs.h.image, []byte("\x89PNG")) {
			t.Fatalf("Copy as Image copied %d bytes, not a PNG", len(hs.h.image))
		}
		click(t, hs, d, lv, locales.ExportDiagram())
		if menu := hs.tt.Menu(); len(menu) != 2 {
			t.Fatalf("Export shows %q", menu)
		}
		hs.tt.CloseMenu()
	})
	t.Run("source", func(t *testing.T) {
		hs, d, lv := open(t)
		click(t, hs, d, lv, locales.ShowSource())
		if !hs.tt.HasText("graph TD") || hs.tt.HasText(locales.OpenDiagram()) {
			t.Fatal("Show Source did not show the source in the card")
		}
		hs.h.image = nil
		click(t, hs, d, lv, locales.CopyCode())
		if got := hs.tt.Clipboard(); got != code {
			t.Fatalf("the source's Copy Code copied %q", got)
		}
		click(t, hs, d, lv, locales.ShowDiagram())
		if _, ok := hs.tt.Find(locales.OpenDiagram()); !ok {
			t.Fatal("Show Diagram did not draw it again")
		}
		b, _ := hs.tt.Find(locales.CopyCode())
		was := d.vscroll.Y
		hs.tt.Scroll(b.X+b.W/2, b.Y+b.H/2, 0, 40)
		hs.settle()
		if d.vscroll.Y != was+40 {
			t.Fatalf("the wheel over the buttons scrolled the text from %v to %v", was, d.vscroll.Y)
		}
	})
}

// A formula in display style whose source is higher than its drawing keeps
// its source's room: the caret going in moves nothing below it.
func TestLiveMathHoldsStill(t *testing.T) {
	hs := newHarness(t, true)
	src := "intro\n\n$$\nx\n$$\n\nend\n"
	d := openLive(t, hs, "math.md", src, 1)
	end := d.buf.Snapshot().LineCount() - 2
	drawn := d.lines.Top(end)
	at := len([]rune(src[:strings.Index(src, "x")]))
	d.selectRange(at, at, false)
	hs.settle()
	if source := d.lines.Top(end); source < drawn-0.5 || source > drawn+0.5 {
		t.Errorf("the text below moved from %v to %v as the caret went into the formula", drawn, source)
	}
}

// A picture of the web on its way shows as a card in its place, which the
// picture fades in over once it arrived.
func TestLivePictureArrives(t *testing.T) {
	hs := newHarness(t, true)
	release := make(chan struct{})
	get := markdownRemote.get
	markdownRemote.get = func(string) ([]byte, error) {
		<-release
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		var b bytes.Buffer
		png.Encode(&b, img)
		return b.Bytes(), nil
	}
	t.Cleanup(func() { markdownRemote.get = get })
	url := fmt.Sprintf("https://example.com/%d.png", time.Now().UnixNano())
	src := "intro\n\n![a picture](" + url + ")\n\nend\n"
	d := openLive(t, hs, "picture.md", src, 0)
	picture := func() *liveDrawn {
		for _, o := range d.liveObjects {
			if o.s.obj == objImage {
				return &o
			}
		}
		return nil
	}
	if o := picture(); o == nil || !o.s.drawn.loading || o.fade != 0 {
		t.Fatalf("the picture on its way shows as %+v, not its card", o)
	}
	close(release)
	l := d.document.live
	hs.until("the picture arrived", func() bool { o := picture(); return o != nil && !o.s.drawn.loading })
	if o := picture(); o.fade >= 1 && len(l.pics) > 0 {
		t.Error("the picture that arrived did not fade in")
	}
	hs.settle()
	if o := picture(); o.fade < 1 || len(l.pics) != 0 {
		t.Errorf("the picture settled at %v faded in, %d cards kept", o.fade, len(l.pics))
	}
}

// A heading has room above it, as in the Reading view, but on the text's
// first line, and its line keeps its place as the caret comes to it.
func TestLiveHeadingGaps(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	src := "# Top\n\ntext\n\n## Section\n\nmore\n"
	path := hs.path("gaps.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("Live Preview parsed", func() bool {
		l := d.document.live
		return a.livePreview(d) && l != nil && l.doc != nil && !l.quick
	})
	hs.place(d, len("# Top\n\nt"), len("# Top\n\nt"))
	hs.settle()
	g := a.geometry(d, d.buf.LineCount())
	if top := g.top(0); top > 0.5 {
		t.Errorf("the first heading stands %v below the text's top", top)
	}
	before := []float32{g.top(4), g.top(5), g.top(6)}
	at := strings.Index(src, "## Section") + 3
	hs.place(d, at, at)
	hs.settle()
	for i, was := range before {
		if now := g.top(4 + i); now-was > 0.5 || was-now > 0.5 {
			t.Errorf("line %d moved from %v to %v as the caret came to the heading", 4+i, was, now)
		}
	}
	if _, y, _, _ := d.lines.Caret(at); y-g.top(4) < a.settings.FontSize*liveGap(2)-0.5 {
		t.Errorf("the caret on the heading stands %v below its line's top, in the room above it", y-g.top(4))
	}
}

// A footnote's definition is muted, its label drawn as its number, and
// the text between two definitions apart keeps its look.
func TestParseLiveFootnoteDefs(t *testing.T) {
	src := "a[^n] b[^m]\n\n[^n]: first\n\nbetween\n\n[^m]: second\n"
	doc := liveParse(src)
	r := []rune(src)
	var labels []string
	doc.visit(0, len(r), func(sp *liveSpan) {
		if sp.obj == objNoteDef && sp.kind == liveObject {
			labels = append(labels, string(r[sp.start:sp.end])+"="+sp.drawn.note)
		}
		if sp.kind == liveStyle && sp.look.color == liveMuted && strings.Contains(string(r[sp.start:sp.end]), "between") {
			t.Errorf("the text between footnotes is muted: %q", string(r[sp.start:sp.end]))
		}
	})
	if fmt.Sprint(labels) != "[[^n]: =1. [^m]: =2.]" {
		t.Errorf("labels %q", labels)
	}
}

// A note embedded in an item of a list or in a quote shows in place too,
// standing in where the item's text or the quote's does.
func TestParseLiveNestedEmbeds(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/other.md", []byte("Hello.\n"), 0o644)
	src := "- ![[other]]\n\n> ![[other]]\n\n1. item\n\n   ![[other]]\n\n| ![[other]] |\n|---|\n"
	doc := parseLive(src, dir+"/note.md", dir, true, &markdownCache{})
	var got []string
	for _, b := range doc.blocks {
		if b.embed {
			got = append(got, fmt.Sprintf("%s q%d l%d %v", strings.TrimSpace(string([]rune(src)[b.start:b.end])), b.quote, b.list, b.bullet))
		}
	}
	want := "[- ![[other]] q0 l1 true > ![[other]] q1 l0 false ![[other]] q0 l1 false]"
	if fmt.Sprint(got) != want {
		t.Errorf("embeds %q, want %q", got, want)
	}
	for _, b := range doc.blocks {
		if b.embed && b.source != "![[other]]" {
			t.Errorf("an embed's source is %q", b.source)
		}
	}
}

// Bare paths end before the marks of a sentence after them, and start
// only at a word's start.
func TestLivePathsIn(t *testing.T) {
	for src, want := range map[string][]string{
		"research: /Users/me/a.md":      {"/Users/me/a.md"},
		"see /x/y.md. Then ~/z.md, too": {"/x/y.md", "~/z.md"},
		"(at ./a.md) and ../b.md:":      {"./a.md", "../b.md"},
		"/x/y.go:12:3; and/or 1/2":      {"/x/y.go:12:3"},
		"https://example.com/x / //y":   nil,
		"**/x/bold.md**":                {"/x/bold.md"},
		`at C:\x\a.md:3, D:/y/b.md.`:    {`C:\x\a.md:3`, "D:/y/b.md"},
		`~\a.md .\b.md ..\c.md xC:\d`:   {`~\a.md`, `.\b.md`, `..\c.md`},
	} {
		var got []string
		for _, p := range livePathsIn([]byte(src), 0, len(src)) {
			got = append(got, src[p[0]:p[1]])
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: paths %q, want %q", src, got, want)
		}
	}
}

// A bare path to a file there is, whole, in the home folder or from the
// note's folder, reads as a link; one to no file stays text.
func TestParseLiveBarePaths(t *testing.T) {
	dir := t.TempDir()
	setHome(t, dir)
	os.MkdirAll(dir+"/notes/sub", 0o755)
	for _, f := range []string{"/abs.md", "/home.md", "/notes/sub/here.md", "/notes/up.md"} {
		os.WriteFile(dir+f, []byte("x\n"), 0o644)
	}
	src := "- [ ] research: " + dir + "/abs.md\n- ~/home.md, ./here.md and ../up.md.\n- " + dir + "/abs.md:12 and /no/such.md and ./gone.md\n"
	doc := parseLive(src, dir+"/notes/sub/note.md", dir, true, &markdownCache{})
	r := []rune(src)
	var links []string
	for _, sp := range doc.spans {
		if sp.kind == liveStyle && sp.look.color == liveLink {
			links = append(links, string(r[sp.start:sp.end]))
		}
	}
	want := []string{dir + "/abs.md", "~/home.md", "./here.md", "../up.md", dir + "/abs.md:12"}
	if !slices.Equal(links, want) {
		t.Errorf("links %q, want %q", links, want)
	}
	if runtime.GOOS == "windows" {
		// A drive's whole path with either slash, and \ from the note.
		fwd := filepath.ToSlash(dir)
		// Each on its own line, as the ~ of a short name, RUNNER~1, may
		// pair with another as a strikethrough.
		src := "- " + fwd + "/abs.md:2\n- " + fwd + `\notes/up.md,` + "\n- " + `~\home.md and .\here.md` + "\n"
		r := []rune(src)
		links = nil
		for _, sp := range parseLive(src, dir+`\notes\sub\note.md`, dir, true, &markdownCache{}).spans {
			if sp.kind == liveStyle && sp.look.color == liveLink {
				links = append(links, string(r[sp.start:sp.end]))
			}
		}
		want := []string{fwd + "/abs.md:2", fwd + `\notes/up.md`, `~\home.md`, `.\here.md`}
		if !slices.Equal(links, want) {
			t.Errorf("Windows links %q, want %q", links, want)
		}
	}
	// The first frame's quick parse asks the disk nothing.
	for _, sp := range parseLive(src, dir+"/notes/sub/note.md", dir, false, &markdownCache{}).spans {
		if sp.kind == liveStyle && sp.look.color == liveLink {
			t.Errorf("quick parse links %q", string(r[sp.start:sp.end]))
		}
	}
}

// A click on a bare path of Live Preview opens its file in a tab, at the
// line after it; ⌘-clicking, or following, a path or a link of Markdown
// to ~/ or a whole path does too.
func TestLiveBarePathOpens(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	hs := newHarness(t, true)
	a := hs.a
	other := filepath.Join(home, "other.md")
	os.WriteFile(other, []byte("one\ntwo\nthree\n"), 0o644)
	abs := hs.path("abs.md")
	os.WriteFile(abs, []byte("abs\n"), 0o644)
	src := "See ~/other.md:3 here.\n\n[home](~/other.md) and [whole](" + abs + ") and ../" + filepath.Base(filepath.Dir(abs)) + "/abs.md\n"
	path := hs.path("bare.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	d.selectRange(len([]rune(src))-1, len([]rune(src))-1, false)
	hs.settle()
	label := "~/other.md:3"
	at := len([]rune(src[:strings.Index(src, label)])) + 1
	_, y0, h, ok := d.lines.Caret(at - 1)
	if !ok {
		t.Fatal("not laid out")
	}
	y := d.liveAt.Y + editorPadY + y0 + h/2 - d.vscroll.Y
	x := d.liveAt.X
	for ; x < d.liveAt.X+1200 && d.liveHover != at; x += 6 {
		hs.tt.Move(x, y)
		step(hs.tt)
	}
	if d.liveHover != at {
		t.Fatalf("over %q, hover is %d, not %d", label, d.liveHover, at)
	}
	hs.settle()
	if hs.tt.Cursor() != ui.CursorPointer {
		t.Errorf("over the path, the pointer is %v", hs.tt.Cursor())
	}
	hs.tt.ClickAt(x, y)
	hs.until("the file open at line 3", func() bool {
		e := a.activeEditor()
		if e == nil || e.path != other || e.unread() {
			return false
		}
		s, _ := e.buf.Snapshot().LineRange(2)
		return e.sel == [2]int{s, s}
	})

	follow := func(label, want string) {
		t.Helper()
		a.openFile(path, true)
		d := a.activeEditor()
		at := len([]rune(src[:strings.Index(src, label)])) + 2
		d.selectRange(at, at, false)
		hs.until("the caret in "+label, func() bool { return d.sel == [2]int{at, at} })
		a.followLiveLink(d)
		hs.until("following "+label+" opens "+want, func() bool {
			e := a.activeEditor()
			return e != nil && e.path == want
		})
	}
	follow("[home]", other)
	follow("[whole]", abs)
	follow("../", abs)
}

// setHome makes dir the home folder ~/ names, on Windows too.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// TestLiveClickHoldsCaret: a click on rendered text puts one caret where
// it went, though marks hidden and a word in bold before it lay the line
// out otherwise than it reads, as the text area finds the pointer held
// again each frame; a drag selects from there, and two and three clicks a
// word and the line.
func TestLiveClickHoldsCaret(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	line := "**Mayoi** rewrites what's expected from the start."
	src := "Intro.\n\n" + line + "\n"
	path := hs.path("click.md")
	os.WriteFile(path, []byte(src), 0o644)
	a.settings.MarkdownMode = "live"
	a.openFile(path, true)
	d := a.activeEditor()
	hs.until("parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	start := len([]rune("Intro.\n\n"))
	p := start + len([]rune("**Mayoi** rewrites what's ex")) // the p of expected
	end := start + len([]rune(line))
	// at is where in the window rune i of the text starts, as laid out now.
	at := func(i int) (float32, float32) {
		t.Helper()
		x, y, h, ok := d.lines.Caret(i)
		if !ok {
			t.Fatal("not laid out")
		}
		return d.liveAt.X + d.textX + x - d.hscroll.X, d.liveAt.Y + editorPadY + y + h/2 - d.vscroll.Y
	}
	away := func() {
		d.selectRange(0, 0, false)
		hs.settle()
	}

	away()
	x, y := at(p)
	hs.tt.Press(x+1, y)
	for range 5 {
		step(hs.tt)
	}
	if d.sel != [2]int{p, p} {
		t.Errorf("held, the click is at %v, not at %d", d.sel, p)
	}
	hs.tt.Release(x+1, y)
	hs.settle()
	if d.sel != [2]int{p, p} {
		t.Errorf("clicked, the caret is at %v, not at %d", d.sel, p)
	}

	away()
	x, y = at(p)
	x1, _ := at(end)
	hs.tt.Press(x+1, y)
	step(hs.tt)
	hs.tt.Move(x1+4, y)
	step(hs.tt)
	hs.tt.Release(x1+4, y)
	hs.settle()
	if d.sel != [2]int{p, end} {
		t.Errorf("dragged, the selection is %v, not %v", d.sel, [2]int{p, end})
	}

	// On the line, its marks showing, two clicks take the word.
	d.selectRange(p, p, false)
	hs.settle()
	x, y = at(p)
	hs.tt.ClickAt(x+1, y)
	hs.tt.ClickAt(x+1, y)
	hs.settle()
	word := strings.Index(line, "expected")
	if want := [2]int{start + word, start + word + len("expected")}; d.sel != want && d.sel != [2]int{want[1], want[0]} {
		t.Errorf("double-clicked, the selection is %v, not %v", d.sel, want)
	}
	d.selectRange(p, p, false)
	hs.settle()
	x, y = at(p)
	hs.tt.ClickAt(x+1, y)
	hs.tt.ClickAt(x+1, y)
	hs.tt.ClickAt(x+1, y)
	hs.settle()
	if lo, hi := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1]); lo != start || hi < end {
		t.Errorf("triple-clicked, the selection is %v, not the line from %d", d.sel, start)
	}
}
