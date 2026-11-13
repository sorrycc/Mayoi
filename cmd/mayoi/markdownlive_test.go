package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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
		{"said[^1] so\n\n[^1]: x\n", []string{"object:[^1]"}},
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
	l := d.document.live
	hs.until("the table rendered", func() bool { return len(l.views) == 1 })
	var table *liveView
	for _, lv := range l.views {
		table = lv
	}
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
	src := "intro\n\n- bullet item\n  continued line\n- [ ] task item\n\nend\n"
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
	for _, tc := range []struct{ marker, text string }{{"- bullet", "bullet item"}, {"- [ ]", "task item"}} {
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
	hs.until("the table rendered", func() bool { return l() != nil && l().doc != nil && !l().quick && len(l().views) == 1 })
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
