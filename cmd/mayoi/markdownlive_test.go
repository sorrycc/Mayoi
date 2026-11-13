package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

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
	for _, sp := range doc.spans {
		kind := map[liveKind]string{liveStyle: "style", liveMark: "mark", liveObject: "object"}[sp.kind]
		out = append(out, kind+":"+string(r[sp.start:sp.end]))
	}
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
		{"see [label](https://x.dev/a_(b)) ok\n", []string{"style:label", "mark:[", "mark:](https://x.dev/a_(b))"}},
		{"<https://x.dev> and https://y.dev\n", []string{"style:https://x.dev", "mark:<", "style:https://y.dev"}},
		{"to [[note#h|Label]] now\n", []string{"style:Label", "mark:[[note#h|", "mark:]]"}},
		{"~~gone~~ and ==lit==\n", []string{"style:gone", "mark:~~", "style:lit", "mark:=="}},
		{"- one\n  - two\n1. three\n", []string{"object:-", "style:1."}},
		{"- [ ] todo\n- [x] done\n", []string{"object:[ ]", "object:[x]", "mark:- ", "style:done"}},
		{"> quoted\n> more\n", []string{"object:> ", "style:quoted"}},
		{"> [!NOTE]\n> text\n", []string{"object:[!NOTE]"}},
		{"```go\nx := 1\n```\n", []string{"mark:```", "style:go"}},
		{"a $x^2$ b\n", []string{"object:$x^2$"}},
		{"---\nkey: v\n---\n# T\n", []string{"style:---\nkey: v\n---", "mark:# "}},
		{"para\n\n***\n", []string{"mark:***"}},
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
	for _, sp := range doc.mapTo(after).spans {
		if sp.kind == liveMark && strings.Contains(string(r[sp.start:sp.end]), "!") {
			t.Errorf("the mark %q took what was typed after it", string(r[sp.start:sp.end]))
		}
	}
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
