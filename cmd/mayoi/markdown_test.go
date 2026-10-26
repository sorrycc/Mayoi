package main

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	east "github.com/yuin/goldmark/extension/ast"
	"mayoi/locales"
)

const markdownFixture = `---
title: hidden front matter
---
# Markdown preview

[![CI](https://example.com/ci.svg)](https://example.com) [![npm](https://example.com/npm.svg)](https://example.org)
<!-- hidden block
comment -->
Inline <!-- hidden inline -->comment<br>hard break.<br/>Again<br />Done.
![SVG logo](logo.svg)

A paragraph with **bold**, *italic*, ***both***, ~~deleted~~, ` + "`inline code`" + ` and [a link](https://example.com). Visit https://example.com.
Soft
break and a hard  
break. <em>HTML is styled</em>

中文段落含 **中文粗体**、*中文斜体*、~~中文删除线~~、` + "`行内代码`" + `，以及 ~~[删除链接](https://example.com)~~。

## Lists and quotes

- Compact bullet
  - Nested compact bullet
    1. Nested compact number
- Next compact item

  Continuation paragraph.

  ~~~go
  println("continuation code")
  ~~~

1. One
2. Two
3. Three
4. Four
5. Five
6. Six
7. Seven
8. Eight
9. Nine
10. Ten
11. Eleven

1. Loose first item

2. Loose second item

- [x] Finished
- [ ] Pending
  - Nested bullet
    1. Nested number

> A quoted paragraph.
> > A nested quote.

` + "```go\npackage main\n\nfunc main() { println(\"Hello\") }\n```" + `

` + "```\nplain code\n```" + `

| Left | Center | Right |
| :--- | :---: | ---: |
| one | **two** | three |

---

![Local art](art.png)
![Remote alt](https://example.com/image.png)
![Missing alt](missing.png)

### Heading three
#### Heading four
##### Heading five
###### Heading six

<div>HTML block is shown as its text.</div>
`

func TestMarkdownBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs", "readme.md")
	blocks := parseMarkdown([]byte(markdownFixture), path, "", nil)
	kinds := map[markdownKind]int{}
	levels := map[int]bool{}
	var bold, italic, both, strike, code, link, nested, task, quote, language, table, image, raw bool
	for _, b := range blocks {
		kinds[b.kind]++
		if b.kind == markdownHeading {
			levels[b.level] = true
		}
		nested = nested || b.indent >= 3
		quote = quote || b.quote == 2
		language = language || b.language == "go"
		if b.kind == markdownTableHeader {
			table = len(b.aligns) == 3 && b.aligns[0] == east.AlignLeft && b.aligns[1] == east.AlignCenter && b.aligns[2] == east.AlignRight && b.cells[0][0].bold
		}
		for _, r := range b.runs {
			bold = bold || r.bold
			italic = italic || r.italic
			both = both || (r.bold && r.italic)
			strike = strike || r.strike
			code = code || r.code
			link = link || r.link == "https://example.com"
			task = task || b.marker == "☑" || strings.Contains(r.text, "☑")
			raw = raw || r.raw
			image = image || (r.image == "art.png" && markdownPath("", path, r.image) == filepath.Join(filepath.Dir(path), "art.png"))
		}
	}
	if len(levels) != 6 || !bold || !italic || !both || !strike || !code || !link || !nested || !task || !quote || !language || !table || !image || !raw || kinds[markdownRule] != 1 {
		t.Fatalf("conversion: levels %v, styles %v %v %v %v %v, link %v nested %v task %v quote %v language %v table %v image %v raw %v kinds %v", levels, bold, italic, both, strike, code, link, nested, task, quote, language, table, image, raw, kinds)
	}
	for _, ending := range []string{"---", "..."} {
		bs := parseMarkdown([]byte("---\ntitle: hidden\n"+ending+"\nVisible <!-- hidden\ncomment -->text<br>next<br/>last<br />end"), path, "", nil)
		if len(bs) != 1 || markdownRunsText(bs[0].runs) != "Visible text\nnext\nlast\nend" {
			t.Fatalf("front matter/comments/br: %+v", bs)
		}
	}
	if bs := parseMarkdown([]byte("---\nIntro text.\n\n---\nAfter"), path, "", nil); len(bs) != 4 {
		t.Fatalf("rules taken for front matter: %+v", bs)
	}
	badges := parseMarkdown([]byte("[![CI](https://example.com/ci)](https://example.com) [![npm](missing)](https://example.org)"), path, "", nil)
	if len(badges) != 1 || badges[0].kind != markdownParagraph || badges[0].runs[0].link == "" || !badges[0].runs[0].raw {
		t.Fatal("badges not inline muted links")
	}
	bs := parseMarkdown([]byte("99. first\n\n    continuation\n\n    ~~~\n    code\n    ~~~\n\n    > quote\n\n    - nested\n\n100. second\n\n- literal ☑ text\n- [x] task"), path, "", nil)
	if bs[0].markerWidth < 2.5 || bs[0].marker != "99." {
		t.Fatalf("ordered width: %+v", bs)
	}
	for _, b := range bs[1:5] {
		if b.offset != bs[0].markerWidth+0.5 {
			t.Fatalf("continuation indent: %+v", bs)
		}
	}
	if bs[len(bs)-2].marker != "•" || bs[len(bs)-1].marker != "☑" {
		t.Fatal("task checkbox detection")
	}
	if markdownPath("", path, "https://example.com/a.png") != "" || markdownPath("", path, "//example.com/a.png") != "" || markdownPath("", path, "/tmp/a%20b.png") != filepath.FromSlash("/tmp/a b.png") {
		t.Fatal("destination resolution")
	}
	// A leading slash is of the root on Windows too, and a whole path of
	// a disk is that path.
	if got := markdownPath(filepath.FromSlash("/r"), path, "/docs/a.png"); got != filepath.FromSlash("/r/docs/a.png") {
		t.Errorf("root-relative destination: %q", got)
	}
	if runtime.GOOS == "windows" {
		for _, dest := range []string{`C:\x\a.png`, "C:/x/a.png"} {
			if got := markdownPath("", path, dest); got != `C:\x\a.png` {
				t.Errorf("markdownPath(%q) = %q", dest, got)
			}
		}
	}
}

func TestMarkdownLinkifyAfterPunctuation(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{"- 反思：https://x.com/a/status/1", "反思：[https://x.com/a/status/1]"},
		{"a:https://x.com/b", "a:[https://x.com/b]"},
		{"见https://x.com/c 。", "见[https://x.com/c] 。"},
		{"1https://x.com/d", "1[https://x.com/d]"},
		{"a：https://x.com/a_b_c，后", "a：[https://x.com/a_b_c]，后"},
		{"a（https://x.com/e）and (https://x.com/(f))", "a（[https://x.com/e]）and ([https://x.com/(f)])"},
		{"a：https://x.com/g?x=1&amp; done.", "a：[https://x.com/g?x=1]& done."},
		{"a：https://x.com/h. b：https://x.com/i", "a：[https://x.com/h]. b：[https://x.com/i]"},
		{"foohttps://x.com/j", "foohttps://x.com/j"},
		{"a：www.example.com", "a：www.example.com"},
		{"`a：https://x.com/k`", "a：https://x.com/k"},
		{"[a：https://x.com/l](https://y.com)", "[a：https://x.com/l](https://y.com)"},
	} {
		var got strings.Builder
		for _, r := range parseMarkdown([]byte(c.source), "", "", nil)[0].runs {
			if r.link != "" && r.link == r.text {
				got.WriteString("[" + r.text + "]")
			} else if r.link != "" {
				got.WriteString("[" + r.text + "](" + r.link + ")")
			} else {
				got.WriteString(r.text)
			}
		}
		if got.String() != c.want {
			t.Errorf("%q: got %q, want %q", c.source, got.String(), c.want)
		}
	}
}

func TestMarkdownFootnotes(t *testing.T) {
	source := "## 数据[^11]\n\nA[^1] b[^1] c[^note].\n\n## 参考链接\n\n" +
		"[^1]: 第 4 次尝试 https://x.com/a/status/1\n[^note]: 锁定：https://x.com/a/status/2\n[^11]: Eleven.\n[^unused]: Never referenced.\n\n" +
		"After.\n"
	blocks := parseMarkdown([]byte(source), "", "", nil)
	text := func(b markdownBlock) string {
		var s strings.Builder
		for _, r := range b.runs {
			if r.link != "" {
				s.WriteString("[" + r.text + "](" + r.link + ")")
			} else {
				s.WriteString(r.text)
			}
		}
		return s.String()
	}
	var got []string
	for _, b := range blocks {
		got = append(got, fmt.Sprintf("%d %s %v %v %s", b.kind, b.marker, b.footnote, b.anchors, text(b)))
	}
	want := []string{
		"1  false [user-content-fnref-1] 数据[¹](#user-content-fn-1)",
		"0  false [user-content-fnref-2 user-content-fnref-2-2 user-content-fnref-3] A[²](#user-content-fn-2) b[²](#user-content-fn-2) c[³](#user-content-fn-3).",
		"1  false [] 参考链接",
		"0  false [] After.",
		"2  false [] ",
		"0 1. true [user-content-fn-1] Eleven.[ ↩](#user-content-fnref-1)",
		"0 2. true [user-content-fn-2] 第 4 次尝试 [https://x.com/a/status/1](https://x.com/a/status/1)[ ↩](#user-content-fnref-2)[ ↩²](#user-content-fnref-2-2)",
		"0 3. true [user-content-fn-3] 锁定：[https://x.com/a/status/2](https://x.com/a/status/2)[ ↩](#user-content-fnref-3)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMarkdownFootnoteLinks(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var body strings.Builder
	body.WriteString("The start[^1].\n\n")
	for i := range 80 {
		body.WriteString("Paragraph " + strconv.Itoa(i) + ".\n\n")
	}
	body.WriteString("[^1]: The note.\n")
	os.WriteFile(hs.path("notes.md"), []byte(body.String()), 0o644)
	a.openFile(hs.path("notes.md"), true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	if hs.tt.HasText("The note.") {
		t.Fatal("the note shows already")
	}
	if err := hs.tt.Click("Open link #user-content-fn-1"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.tt.HasText("The note.") || hs.tt.HasText("Paragraph 0.") {
		t.Fatal("the reference did not scroll to its note")
	}
	if err := hs.tt.Click("Open link #user-content-fnref-1"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.tt.HasText("Paragraph 0.") {
		t.Fatal("the back link did not scroll to the reference")
	}
}

func TestMarkdownPreview(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	d := a.activeEditor()
	if d.preview != nil || !hs.tt.HasText("Show Preview (⇧⌘V)") {
		t.Fatal("Markdown did not open as source")
	}
	if err := hs.tt.Click("Show Preview (⇧⌘V)"); err != nil {
		t.Fatal(err)
	}
	hs.until("Markdown preview", func() bool { return d.preview != nil && d.preview.loaded })
	hs.settle()
	if !hs.tt.HasText("Preview") {
		t.Fatal("missing preview status")
	}
	if !hs.tt.HasText("Mayoi") {
		t.Fatal("heading not shown")
	}
	a.run("view.togglePreview")
	hs.settle()
	if d.preview != nil {
		t.Fatal("source toggle failed")
	}
	for _, name := range []string{"main.go", "web/index.html"} {
		a.openFile(hs.path(name), true)
		a.run("view.togglePreview")
		if a.activeEditor().preview != nil {
			t.Fatalf("previewed %s", name)
		}
	}
	path := hs.path("notes.MARKDOWN")
	os.WriteFile(path, []byte("# Notes\n"), 0o644)
	a.openFile(path, true)
	a.run("view.togglePreview")
	hs.until("long extension", func() bool { return a.activeEditor().preview.loaded })
}

func TestMarkdownPreviewColumn(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	writePNG(t, hs.path("wide.png"), 2000, 100)
	source := "[Paragraph](https://example.com/paragraph)\n\n| Left | Right |\n| --- | --- |\n| [First](https://example.com/first) | [Second](https://example.com/second) |\n\n[![Wide image](wide.png)](https://example.com/image)\n"
	if err := os.WriteFile(hs.path("README.md"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(hs.path("README.md"), true)
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return a.activeEditor().preview.loaded })
	for _, layout := range []struct {
		name   string
		width  int
		split  bool
		capped bool
	}{
		{"wide", 1600, false, true},
		{"narrow", 900, false, false},
		{"split", 1600, true, false},
	} {
		t.Run(layout.name, func(t *testing.T) {
			hs.tt.SetSize(layout.width, 760)
			if layout.split {
				a.run("view.splitRight")
				if a.activeEditor().preview == nil {
					a.run("view.togglePreview")
				}
				hs.until("split preview", func() bool { return a.activeEditor().preview.loaded })
			}
			hs.settle()
			bounds := func(label string) ui.Rect {
				r, ok := hs.tt.Find(label)
				if !ok {
					t.Fatalf("missing %s", label)
				}
				return r
			}
			// The column is 120 digits wide, or the pane's width less its
			// padding when that is narrower, and sits in the pane's middle.
			pane := bounds("Preview README.md")
			measure := a.activeEditor().preview.markdown.measure
			if capped := measure < pane.W-2*textPadR; capped != layout.capped {
				t.Fatalf("120 digits are %v wide in pane %v, capped %v, want %v", measure, pane, capped, layout.capped)
			}
			column := min(measure, pane.W-2*textPadR)
			image := bounds("Open link https://example.com/image")
			if math.Abs(float64(image.W-column)) > 1 {
				t.Errorf("image is %v wide, want the column's %v in pane %v", image.W, column, pane)
			}
			left, right := image.X-pane.X, pane.X+pane.W-image.X-image.W
			if left < textPadR-1 || math.Abs(float64(left-right)) > 2 {
				t.Errorf("column is not centred: %v left, %v right in pane %v", left, right, pane)
			}
			if paragraph := bounds("Open link https://example.com/paragraph"); math.Abs(float64(paragraph.X-image.X)) > 1 {
				t.Errorf("paragraph starts at %v, not the column's %v", paragraph.X, image.X)
			}
			first := bounds("Open link https://example.com/first")
			second := bounds("Open link https://example.com/second")
			// A table is as wide as its text, which is narrower than the
			// column here: its columns are as their words.
			if gap := second.X - first.X; gap <= first.W || gap >= column/2 {
				t.Errorf("table columns are %v apart, want as wide as %q, in the column's %v", gap, "First", column)
			}
			if first.X-image.X < 0 || first.X-image.X > 20 {
				t.Errorf("table starts at %v, not the column's %v", first.X, image.X)
			}
		})
	}
}

func TestMarkdownPreviewSelect(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("README.md"), []byte("# Title\n\nSome prose.\n\n1. First\n2. Second\n"), 0o644)
	a.openFile(hs.path("README.md"), true)
	d := a.activeEditor()
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	r, ok := hs.tt.Find("Some prose.")
	if !ok {
		t.Fatal("paragraph not shown")
	}
	hs.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	hs.tt.Key(ui.Cmd, ui.KeyA)
	hs.tt.Key(ui.Cmd, ui.KeyC)
	hs.settle()
	// Every block shares one selection; a list's numbers are left out.
	if got, want := hs.tt.Clipboard(), "Title\nSome prose.\nFirst\nSecond"; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

func TestMarkdownLiveSplitAndScroll(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("README.md"), []byte(strings.Repeat("A paragraph.\n\n", 3000)), 0o644)
	a.openFile(hs.path("README.md"), true)
	a.run("view.splitRight")
	source := a.cols[0].tab().ed
	d := a.activeEditor()
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	v := d.preview
	if source.document != d.document || source.preview != nil {
		t.Fatal("split did not keep separate views of the same document")
	}
	// The preview follows the text, which the edit above leaves where it
	// is, with its caret.
	at := 100 * len("A paragraph.\n\n")
	hs.place(source, at, at)
	source.vscroll.Y = editorPadY + 200*a.font.lineH
	hs.settle()
	hs.settle()
	before, _ := v.markdown.list.Visible()
	textY := source.vscroll.Y
	if before < 90 || v.markdown.scroll.Y <= 0 {
		t.Fatalf("scroll position %d, %v", before, v.markdown.scroll.Y)
	}
	source.edit(source.sel, textEdit{0, len("A paragraph."), "Updated paragraph."})
	hs.until("updated preview", func() bool {
		return v.version == d.buf.Version() && markdownRunsText(v.markdown.blocks[0].runs) == "Updated paragraph."
	})
	hs.settle()
	// Where the text is, the preview shows, the same as before the edit.
	source.vscroll.Y = textY
	hs.settle()
	hs.settle()
	after, _ := v.markdown.list.Visible()
	if after != before {
		t.Fatalf("scroll moved from %d to %d, the text at %v", before, after, source.vscroll.Y)
	}
	// A newer edit wins even if an older parse has already started.
	source.edit(source.sel, textEdit{0, len("Updated paragraph."), "Newest paragraph."})
	hs.until("newest preview", func() bool {
		return v.version == d.buf.Version() && markdownRunsText(v.markdown.blocks[0].runs) == "Newest paragraph."
	})
	if source.preview != nil {
		t.Fatal("editing source changed its preview state")
	}
}

func TestMarkdownPreviewRestoreAndReopen(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	a.run("view.splitRight")
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return a.activeEditor().preview.loaded })
	a.rememberTabs()
	if a.window.Columns[0].Tabs[0].Preview || !a.window.Columns[1].Tabs[0].Preview {
		t.Fatal("per-column preview state was not saved")
	}
	b := newApp(&fakeHost{folder: hs.root}, a.settings, a.window)
	t.Cleanup(b.shutdown)
	bt := ui.NewTester(b.view, 1200, 760)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		step(bt)
		if b.activeEditor() != nil && b.activeEditor().preview != nil && b.activeEditor().preview.loaded {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(b.cols) != 2 || b.cols[0].tab().ed.preview != nil || b.activeEditor().preview == nil || !b.activeEditor().preview.loaded {
		t.Fatal("preview was not restored independently")
	}
	b.run("file.close")
	b.run("file.reopenClosed")
	step(bt)
	if b.activeEditor() == nil || b.activeEditor().preview == nil {
		t.Fatal("closed preview was not reopened")
	}
}

func TestShotMarkdown(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("set SHOTS to an output directory")
	}
	for _, theme := range []struct {
		name, theme string
		dark        bool
	}{
		{"light", "", false}, {"dark", "", true}, {"matrix", themeMatrix, true}, {"claude", themeClaude, true}, {"qoder", themeQoder, false},
		{"grokbot", themeGrokBot, true},
	} {
		hs := newHarness(t, theme.dark)
		hs.a.settings.Theme = theme.theme
		// A taller headless viewport includes every supported element in one image.
		hs.tt = ui.NewTester(hs.a.view, 1200, 2200)
		hs.tt.SetDark(theme.dark)
		writePNG(t, hs.path("art.png"), 160, 60)
		os.WriteFile(hs.path("logo.svg"), []byte(testSVG), 0o644)
		os.WriteFile(hs.path("sample.md"), []byte(markdownFixture), 0o644)
		hs.a.openFile(hs.path("sample.md"), true)
		hs.a.run("view.togglePreview")
		hs.until("preview", func() bool { return hs.a.activeEditor().preview.loaded })
		hs.settle()
		hs.shot("mayoi-md-" + theme.name)
	}
	// Render the actual Markdown inline builder at two device pixels per
	// point, with enough space for italic overhangs and no editor chrome.
	hs := newHarness(t, false)
	blocks := parseMarkdown([]byte("中文斜体 *中文斜体* italic *italic*"), "", "", nil)
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Background(hs.a.pal.editor).Padding(16).Children(func() {
			hs.a.markdownText(c, nil, "", blocks[0].runs, 24, false, false)
		})
	}, 640, 72, 2)
	f, err := os.Create(filepath.Join(os.Getenv("SHOTS"), "mayoi-md-italic.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownLocalImagesAndLinks(t *testing.T) {
	hs := newHarness(t, false)
	dir := hs.path("docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "readme.md")
	art := filepath.Join(dir, "art.png")
	writePNG(t, art, 80, 40)
	os.WriteFile(filepath.Join(dir, "logo.svg"), []byte(testSVG), 0o644)
	c := &markdownCache{}
	imageSource := []byte("![art](art.png) ![logo](logo.svg)")
	first := parseMarkdown(imageSource, path, "", nil, c)
	second := parseMarkdown(imageSource, path, "", nil, c)
	// Pictures of a paragraph next to each other share a row.
	if len(first) != 1 || len(first[0].runs) != 2 || first[0].runs[0].bitmap != second[0].runs[0].bitmap || first[0].runs[1].w != 64 || first[0].runs[1].h != 32 {
		t.Fatal("image cache or SVG")
	}
	// A reused pointer proves a second bitmap was not decoded.
	future := time.Now().Add(time.Second)
	os.Chtimes(art, future, future)
	third := parseMarkdown(imageSource, path, "", nil, c)
	if third[0].runs[0].bitmap == first[0].runs[0].bitmap || len(c.images) != 2 {
		t.Fatal("mtime did not invalidate/prune cache")
	}
	parseMarkdown([]byte("no images"), path, "", nil, c)
	if len(c.images) != 0 {
		t.Fatal("unused images retained")
	}
	// A superseded parse skips images but leaves them cached for the next.
	parseMarkdown(imageSource, path, "", nil, c)
	parseMarkdown(imageSource, path, "", func() bool { return false }, c)
	if len(c.images) != 2 {
		t.Fatal("superseded parse pruned the cache")
	}
	// Code highlighted once is reused as long as the document has it.
	code := []byte("```go\nfunc main() {}\n```\n")
	first = parseMarkdown(code, path, "", nil, c)
	second = parseMarkdown(append([]byte("Edited.\n\n"), code...), path, "", nil, c)
	if &first[0].runs[0] != &second[1].runs[0] || len(c.code) != 1 {
		t.Fatal("code highlighted again")
	}
	parseMarkdown([]byte("no code"), path, "", nil, c)
	if len(c.code) != 0 {
		t.Fatal("unused code retained")
	}
	// A leading / resolves against the repository's root.
	os.Mkdir(filepath.Join(hs.root, ".git"), 0o755)
	root := markdownRoot(path, "")
	if root != hs.root || markdownPath(root, path, "/docs/art.png") != art {
		t.Fatalf("root-relative: %q", root)
	}
	rooted := parseMarkdown([]byte("![art](/docs/art.png)"), path, root, nil)
	if len(rooted) != 1 || rooted[0].runs[0].bitmap == nil {
		t.Fatal("root-relative image not decoded")
	}
	src := "[![Relative](art.png)](../main.go)\n\n![Absolute](" + art + ")\n\n[Open local](../main.go#text)\n\n[![Remote](https://example.com/art.png)](https://example.com)\n\n![Missing](missing.png)\n\n| Picture |\n| --- |\n| [![Cell](art.png)](../main.go) |\n| [![Cell remote](https://example.com/art.png)](https://example.org) |\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	blocks := parseMarkdown([]byte(src), path, "", nil)
	images := 0
	for _, b := range blocks {
		if b.kind == markdownTable && b.cells[0][0].text == "Cell" && b.cells[0][0].bitmap == nil {
			t.Fatal("table image not decoded")
		}
		if b.kind != markdownImageBlock {
			continue
		}
		r := b.runs[0]
		images++
		if r.text == "Relative" || r.text == "Absolute" {
			if r.bitmap == nil || r.w != 80 || r.h != 40 {
				t.Fatalf("local image: %+v", r)
			}
		} else if r.bitmap != nil {
			t.Fatal("remote or missing image loaded")
		}
	}
	for _, b := range parseMarkdown([]byte(src), path, "", func() bool { return false }) {
		for _, r := range b.runs {
			if r.bitmap != nil {
				t.Fatal("stale parse decoded an image")
			}
		}
	}
	if images != 2 {
		t.Fatalf("%d images", images)
	}
	hs.a.openFile(path, true)
	hs.a.run("view.togglePreview")
	hs.until("preview", func() bool { return hs.a.activeEditor().preview.loaded })
	hs.settle()
	r, ok := hs.tt.Find("Open link ../main.go")
	if !ok {
		t.Fatal("missing linked image")
	}
	hs.tt.Press(r.X+r.W/2, r.Y+r.H/2)
	hs.tt.Move(0, 0)
	hs.tt.Release(0, 0)
	if hs.a.activeEditor().path != path {
		t.Fatal("dragging out opened local link")
	}
	if err := hs.tt.Click("Open link https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(hs.tt.OpenedURLs()) != 1 {
		t.Fatal("linked remote image did not open browser")
	}
	if !hs.tt.HasText("Cell remote") {
		t.Fatal("missing table image placeholder")
	}
	r, ok = hs.tt.Find("Open link ../main.go#text")
	if !ok {
		t.Fatal("missing text link")
	}
	hs.tt.Press(r.X+r.W/2, r.Y+r.H/2)
	hs.tt.Move(0, 0)
	hs.tt.Release(0, 0)
	if hs.a.activeEditor().path != path {
		t.Fatal("dragging out opened text link")
	}
	if err := hs.tt.Click("Open link ../main.go"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.a.activeEditor().path != hs.path("main.go") {
		t.Fatal("relative link did not open in Mayoi")
	}

}

func TestMarkdownDebounceAndClosedResult(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	special := specialFiles(t, hs.root)
	for _, path := range special {
		done := make(chan error, 1)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		go func() { _, _, _, _, err := decodeImage(path); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("special file accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("special file blocked")
		}
		runtime.ReadMemStats(&after)
		if after.TotalAlloc-before.TotalAlloc > 1<<20 {
			t.Fatal("special file allocated too much")
		}
	}
	md := "Special files:"
	for _, path := range special {
		md += " ![x](" + filepath.ToSlash(path) + ")"
	}
	os.WriteFile(hs.path("README.md"), []byte(md), 0o644)
	a.openFile(hs.path("README.md"), true)
	a.run("view.togglePreview")
	hs.until("special-file preview", func() bool { return a.activeEditor().preview.loaded })
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, ed.buf.Len(), "Updated after special files"})
	hs.until("special-file edit", func() bool { return ed.preview.version == ed.buf.Version() })
	a.setPreview(ed, false)
	a.openFile(hs.path("README.md"), true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	v := d.preview
	a.followMarkdown(v)
	generation := v.markdown.generation.Load()
	d.edit(d.sel, textEdit{0, d.buf.Len(), "# During first parse\n"})
	a.followMarkdown(v)
	if !v.markdown.running || v.wait != nil || v.markdown.generation.Load() != generation+1 {
		t.Fatal("edit during first parse started another worker")
	}
	for deadline := time.Now().Add(5 * time.Second); v.markdown.running && time.Now().Before(deadline); {
		a.drain()
		time.Sleep(time.Millisecond)
	}
	if v.loaded || v.wait == nil {
		t.Fatal("first stale result did not debounce")
	}
	hs.until("preview", func() bool { return d.preview.loaded })
	old := v.version
	d.edit(d.sel, textEdit{0, d.buf.Len(), "# First edit\n"})
	a.followMarkdown(v)
	if v.wait == nil || v.version != old {
		t.Fatal("edit did not debounce")
	}
	d.edit(d.sel, textEdit{0, d.buf.Len(), "# Latest edit\n"})
	a.followMarkdown(v)
	hs.until("latest edit", func() bool { return v.version == d.buf.Version() })
	if markdownRunsText(v.markdown.blocks[0].runs) != "Latest edit" {
		t.Fatal("stale text won")
	}
	// A result started before switching back to source cannot change the old viewer.
	d.edit(d.sel, textEdit{0, d.buf.Len(), "# Discarded\n"})
	v.loaded = false
	a.followMarkdown(v)
	a.setPreview(d, false)
	hs.settle()
	if markdownRunsText(v.markdown.blocks[0].runs) != "Latest edit" || d.preview != nil {
		t.Fatal("closed preview received a result")
	}
}

func TestMarkdownInlineStyles(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.FontWeight = 500
	a.settings.FontFeatures = []string{"calt=0", "ss01"}
	for _, theme := range []string{"", themeMatrix, themeClaude, themeQoder, themeGrokBot} {
		for _, dark := range []bool{false, true} {
			a.pal = paletteFor(theme, dark)
			p := a.pal
			for _, run := range []markdownInline{{text: "deleted", strike: true}, {text: "deleted link", strike: true, link: "https://example.com"}} {
				span := a.markdownSpan(run, p.text, 13, false)
				if span.DecorationColor.A == 0 || span.DecorationColor != span.Color {
					t.Fatalf("decoration: %+v", span)
				}
			}
			code := a.markdownSpan(markdownInline{text: "code", code: true}, p.text, 13, false)
			if code.Background.A == 0 || code.Background == p.editor || code.Color.A == 0 {
				t.Fatalf("inline code: %+v", code)
			}
			if code.Font != a.settings.FontFamily || code.Weight != 500 || code.Features != "calt=0,ss01" || code.Text != "\u2009code\u2009" || code.Radius == 0 || code.Size != 13*0.9 {
				t.Fatalf("code font/padding: %+v", code)
			}
			latin := a.markdownSpan(markdownInline{text: "italic", italic: true}, p.text, 13, false)
			cjk := a.markdownSpan(markdownInline{text: "中文斜体", italic: true}, p.text, 13, false)
			if !latin.Italic || !cjk.Italic || latin.Color != p.text || cjk.Color != p.text {
				t.Fatalf("italic styles: %+v / %+v", latin, cjk)
			}
			block := a.markdownSpan(markdownInline{text: "code", code: true}, p.text, 13, true)
			if block.Background.A != 0 || block.Text != "code" {
				t.Fatalf("block code: %+v", block)
			}
		}
	}
}

func TestMarkdownFontAndZoom(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return a.activeEditor().preview.loaded })
	v := a.activeEditor().preview
	for _, size := range []float32{13, 18} {
		a.settings.FontSize = size
		tt := ui.NewTester(func(c *ui.Context) {
			a.markdownText(c, nil, "", []markdownInline{{text: "link", bold: true, italic: true, strike: true, code: true, link: "https://example.com"}}, size, false, false)
		}, 300, 80)
		link, ok := tt.Find("Open link https://example.com")
		if !ok || link.H < size {
			t.Fatal("link did not inherit font size")
		}
		before := bytes.Clone(tt.Image().Pix)
		tt.Move(link.X+link.W/2, link.Y+link.H/2)
		if bytes.Equal(before, tt.Image().Pix) {
			t.Fatal("hover underline did not change link rendering")
		}
		if a.markdownSize(markdownBlock{kind: markdownParagraph}) != size || a.markdownSize(markdownBlock{kind: markdownHeading, level: 1}) != size*2 {
			t.Fatal("preview font does not follow settings")
		}
	}
	a.zoom(1)
	if a.settings.FontSize != 19 || v.zoom != 0 {
		t.Fatalf("font %v, viewer zoom %v", a.settings.FontSize, v.zoom)
	}
	a.zoom(-1)
	if a.settings.FontSize != 18 || v.zoom != 0 {
		t.Fatal("zoom out")
	}
	a.zoom(0)
	if a.settings.FontSize != 13 || v.zoom != 0 {
		t.Fatal("zoom reset")
	}
}

func TestMarkdownListSpacing(t *testing.T) {
	hs := newHarness(t, false)
	blocks := parseMarkdown([]byte("- tight one\n  - nested\n- tight two\n\nParagraph.\n\n1. loose one\n\n2. loose two\n"), "test.md", "", nil)
	var tight, loose bool
	for _, b := range blocks {
		if b.indent == 0 {
			continue
		}
		_, bottom := hs.a.markdownSpacing(b)
		if b.tight {
			tight = true
			if bottom != 0 {
				t.Fatal("tight list has paragraph spacing")
			}
		} else {
			loose = true
			if bottom != hs.a.settings.FontSize*hs.a.settings.LineHeight*0.75 {
				t.Fatal("loose list spacing")
			}
		}
	}
	if !tight || !loose {
		t.Fatalf("tight %v loose %v", tight, loose)
	}
}

func TestMarkdownImageSpacing(t *testing.T) {
	blocks := parseMarkdown([]byte("![remote](https://example.com/a.png)\n![missing](missing.png)\n![missing too](missing2.png)\n"), "test.md", "", nil)
	if len(blocks) != 1 {
		t.Fatalf("images gained empty paragraphs: %+v", blocks)
	}
	for _, b := range blocks {
		if b.kind != markdownParagraph {
			t.Fatalf("unexpected image separator: %+v", b)
		}
	}
	for _, source := range []string{"- ![one](a.png) ![two](b.png) text", "1. text ![one](a.png) ![two](b.png) tail"} {
		blocks = parseMarkdown([]byte(source), "test.md", "", nil)
		for i, b := range blocks {
			if (b.marker != "") != (i == 0) {
				t.Fatalf("repeated or missing marker: %+v", blocks)
			}
		}
	}
}

// A shot of a long document, to look at the preview's parts.
func TestShotMarkdownParts(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("SHOTS is not set")
	}
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		a := hs.a
		os.WriteFile(hs.path("parts.md"), []byte(markdownPartsFixture), 0o644)
		a.openFile(hs.path("parts.md"), true)
		a.run("view.togglePreview")
		hs.until("preview", func() bool { return a.activeEditor().preview.loaded })
		hs.settle()
		name := "markdown-parts-light"
		if dark {
			name = "markdown-parts-dark"
		}
		hs.shot(name)
	}
}

const markdownPartsFixture = "## Alerts\n\n" +
	"> [!NOTE]\n> Useful information that users should know.\n\n" +
	"> [!IMPORTANT]\n> Key information users need to know.\n\n" +
	"> [!WARNING]\n> Urgent info that needs immediate attention.\n\n" +
	"> [!CAUTION]\n> Advises about risks of certain actions.\n\n" +
	"### Contents\n\n1. [Alerts](#alerts)\n2. [Tables](#tables)\n\n" +
	"## Tables\n\n| Key | Command | Notes |\n|:--|:--|--:|\n| `⌘P` | Quick open | 1 |\n| `⇧⌘F` | Find in files, with include and exclude globs | 2 |\n| `⌘\\\\` | Split right | 3 |\n| `⇧⌘V` | Toggle preview | 4 |\n\n" +
	"---\n\n```js\nconst a = 1 // a comment\n```\n\n```\nplain\n```\n"

func TestMarkdownAlerts(t *testing.T) {
	src := "> [!NOTE]\n> Read this.\n\n> [!warning]\n> Careful **now**.\n> > Nested quote.\n\n> [!FOO]\n> Not an alert.\n\n> [!TIP]\n\n> Plain.\n"
	blocks := parseMarkdown([]byte(src), "test.md", "", nil)
	var got []string
	for _, b := range blocks {
		switch b.kind {
		case markdownAlertTitle:
			got = append(got, "title:"+strconv.Itoa(int(b.alert)))
		default:
			got = append(got, strconv.Itoa(int(b.alert))+"@"+strconv.Itoa(b.alertAt)+"/"+strconv.Itoa(b.quote)+":"+markdownRunsText(b.runs))
		}
	}
	want := []string{
		"title:1", "1@1/1:Read this.",
		"title:4", "4@1/1:Careful now.", "4@1/2:Nested quote.",
		"0@0/1:[!FOO] Not an alert.",
		"title:2",
		"0@0/1:Plain.",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("alerts:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownSlugsAndWidths(t *testing.T) {
	blocks := parseMarkdown([]byte("# Hello, World!\n## Hello, World!\n### `Code` and 中文 _x_\n\n| a | a much longer column |\n|---|---|\n| 1 | 2 |\n"), "test.md", "", nil)
	var slugs []string
	var rows []markdownBlock
	for i, b := range blocks {
		if b.slug != "" {
			slugs = append(slugs, b.slug)
		}
		if b.kind == markdownTable || b.kind == markdownTableHeader {
			rows = append(rows, b)
			if b.table != 3 || b.kind == markdownTable && (b.row != 1 || !b.last) {
				t.Fatalf("row %d: table %d, row %d, last %v", i, b.table, b.row, b.last)
			}
		}
	}
	// A character 10 wide, a cell's padding 1.5 times the size and 2.
	widths := markdownColumns(rows, 10, func(r markdownInline, head bool) float32 { return float32(len(r.text)) * 10 })
	if strings.Join(slugs, " ") != "hello-world hello-world-1 code-and-中文-x" {
		t.Fatalf("slugs %q", slugs)
	}
	if len(widths) != 2 || widths[0] != 20+17 || widths[1] != 200+17 {
		t.Fatalf("widths %v", widths)
	}
}

func TestMarkdownAnchorAndCopy(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var body strings.Builder
	body.WriteString("[Go to the end](#the-end)\n\n")
	for i := range 80 {
		body.WriteString("Paragraph " + strconv.Itoa(i) + ".\n\n")
	}
	body.WriteString("## The End\n\n```go\nfunc main() {}\n```\n")
	os.WriteFile(hs.path("long.md"), []byte(body.String()), 0o644)
	a.openFile(hs.path("long.md"), true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	if hs.tt.HasText("The End") {
		t.Fatal("the end shows already")
	}
	if err := hs.tt.Click("Open link #the-end"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.tt.HasText("The End") {
		t.Fatal("the anchor did not scroll to its heading")
	}
	code, ok := hs.tt.Find("func main() {}")
	if !ok {
		t.Fatal("no code")
	}
	if hs.tt.HasText("Copy Code") {
		t.Fatal("copy shows without the pointer")
	}
	hs.tt.Move(code.X+code.W/2, code.Y+code.H/2)
	hs.settle()
	if err := hs.tt.Click("Copy Code"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := a.services.ReadClipboard(); got != "func main() {}" {
		t.Fatalf("clipboard %q", got)
	}
}

func TestMarkdownPreviewToSide(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	src := a.activeEditor()
	a.run("view.previewToSide")
	hs.settle()
	if len(a.cols) != 2 || a.activeEditor() != src || src.preview != nil {
		t.Fatalf("columns %d, the text has the keyboard %v", len(a.cols), a.activeEditor() == src)
	}
	side := a.cols[1].tab().ed
	if side == nil || side.document != src.document || side.preview == nil {
		t.Fatal("no preview to the side")
	}
	hs.until("preview", func() bool { return side.preview.loaded })
}

func TestMarkdownPreviewToSideColumn(t *testing.T) {
	for _, tc := range []struct {
		name string
		// arrange leaves README.md with the keyboard among the columns,
		// and returns the column its preview should go to.
		arrange func(hs *harness) *column
		cols    int
	}{
		{"right", func(hs *harness) *column {
			a := hs.a
			a.openFile(hs.path("main.go"), true)
			right := a.splitColumn(a.column(), true)
			a.activate(a.cols[0], 0, true)
			a.openFile(hs.path("README.md"), true)
			return right
		}, 2},
		{"left", func(hs *harness) *column {
			a := hs.a
			a.openFile(hs.path("main.go"), true)
			left := a.column()
			a.splitRight()
			a.openFile(hs.path("README.md"), true)
			return left
		}, 2},
		{"shown already", func(hs *harness) *column {
			a := hs.a
			a.openFile(hs.path("README.md"), true)
			a.splitRight()
			a.openFile(hs.path("main.go"), true)
			a.activate(a.cols[0], 0, true)
			return a.cols[1]
		}, 2},
		{"past a locked column", func(hs *harness) *column {
			a := hs.a
			a.openFile(hs.path("main.go"), true)
			left := a.column()
			right := a.splitColumn(left, true)
			a.toggleColumnLock(right)
			a.splitRight()
			a.openFile(hs.path("README.md"), true)
			return left
		}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHarness(t, true)
			a := hs.a
			want := tc.arrange(hs)
			hs.settle()
			src := a.activeEditor()
			tabs := len(want.tabs)
			if want.showing(src.document) < 0 {
				tabs++
			}
			a.run("view.previewToSide")
			hs.settle()
			if len(a.cols) != tc.cols || a.activeEditor() != src || src.preview != nil {
				t.Fatalf("columns %d, the text has the keyboard %v", len(a.cols), a.activeEditor() == src)
			}
			side := want.tab().ed
			if side == nil || side.document != src.document || side.preview == nil {
				t.Fatal("no preview in the column beside")
			}
			if len(want.tabs) != tabs {
				t.Fatalf("%d tabs beside, want %d", len(want.tabs), tabs)
			}
		})
	}
}

func TestMarkdownEmpty(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("empty.md"), nil, 0o644)
	a.openFile(hs.path("empty.md"), true)
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return a.activeEditor().preview.loaded })
	hs.settle()
	if !hs.tt.HasText("Nothing to preview") {
		t.Fatal("no word of an empty document")
	}
}

func TestMarkdownBlockLines(t *testing.T) {
	src := "---\ntitle: x\n---\n# Title\n\npara one\nstill one\n\n- a\n- b\n\n> [!NOTE]\n> noted\n\n```go\nx := 1\n```\n\n| h | i |\n|---|---|\n| 1 | 2 |\n\n---\n\ntail\n"
	blocks := parseMarkdown([]byte(src), "/x.md", "", nil)
	var got []string
	for _, b := range blocks {
		got = append(got, strconv.Itoa(b.line))
	}
	// Title, para, a, b, alert title, noted, code, header, row, rule (where
	// the row is, as goldmark keeps no place for it), tail.
	if want := "3 5 8 9 11 11 14 18 20 20 24"; strings.Join(got, " ") != want {
		t.Fatalf("lines %s, want %s", strings.Join(got, " "), want)
	}
}

// markdownSyncDoc is a document of many sections, long enough to scroll
// both its text and its preview.
func markdownSyncDoc() string {
	var b strings.Builder
	for i := range 120 {
		fmt.Fprintf(&b, "## Section %d\n\nSome text of section %d, long enough to take a line or two of the preview as it wraps.\n\n", i, i)
		if i%10 == 3 {
			b.WriteString("```go\nfunc f() {\n\treturn\n}\n```\n\n")
		}
	}
	return b.String()
}

func TestMarkdownScrollSync(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("sync.md"), []byte(markdownSyncDoc()), 0o644)
	a.openFile(hs.path("sync.md"), true)
	hs.settle()
	src := a.activeEditor()
	a.run("view.previewToSide")
	hs.settle()
	side := a.cols[1].tab().ed
	hs.until("preview", func() bool { return side.preview.loaded })
	hs.settle()
	m := side.preview.markdown
	// The heading at the top of the preview, as the last frame showed it.
	pane, ok := hs.tt.Find(locales.PreviewLabel("sync.md"))
	if !ok {
		t.Fatal("no preview")
	}
	topHeading := func() string {
		first, last := m.list.Visible()
		for i := max(first, 0); i <= last; i++ {
			if m.blocks[i].kind != markdownHeading {
				continue
			}
			h := markdownRunsText(m.blocks[i].runs)
			if r, ok := hs.tt.Find(h); ok && r.Y+r.H > pane.Y+1 {
				return h
			}
		}
		return ""
	}
	// Scrolling the text, with the keyboard in it, scrolls the preview.
	line := strings.Count(markdownSyncDoc()[:strings.Index(markdownSyncDoc(), "## Section 60\n")], "\n")
	src.vscroll.Y = editorPadY + float32(line)*a.font.lineH
	for range 4 {
		hs.settle()
	}
	if h := topHeading(); h != "Section 60" && h != "Section 59" {
		t.Fatalf("the preview shows %q at its top, not section 60", h)
	}
	// The end of the text is the end of the preview.
	src.vscroll.Y = src.vscroll.MaxY
	for range 4 {
		hs.settle()
	}
	if !m.list.AtEnd() {
		t.Fatal("the preview is not at its end with the text")
	}
	// Scrolling the preview scrolls the text.
	src.vscroll.Y = 0
	for range 4 {
		hs.settle()
	}
	if m.scroll.Y != 0 {
		t.Fatalf("the preview is at %v with the text at its top", m.scroll.Y)
	}
	for range 30 {
		hs.tt.Scroll(pane.X+pane.W/2, pane.Y+pane.H/2, 0, 120)
		hs.settle()
	}
	if src.vscroll.Y <= 0 {
		t.Fatal("the text did not follow the preview")
	}
	top := a.geometry(src, src.buf.Snapshot().LineCount()).at(src.vscroll.Y - editorPadY)
	heading := topHeading()
	n, _ := strconv.Atoi(strings.TrimPrefix(heading, "Section "))
	want := strings.Count(markdownSyncDoc()[:strings.Index(markdownSyncDoc(), "## "+heading+"\n")], "\n")
	if top < want-6 || top > want+6 {
		t.Fatalf("the text is at line %d, the preview at %q (line %d, %d)", top, heading, want, n)
	}
}

func TestMarkdownToggleKeepsPlace(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	doc := markdownSyncDoc()
	os.WriteFile(hs.path("sync.md"), []byte(doc), 0o644)
	a.openFile(hs.path("sync.md"), true)
	hs.settle()
	d := a.activeEditor()
	lineOf := func(heading string) int {
		return strings.Count(doc[:strings.Index(doc, "## "+heading+"\n")], "\n")
	}
	d.vscroll.Y = editorPadY + float32(lineOf("Section 60"))*a.font.lineH
	hs.settle()
	// The preview opens where the text was.
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return d.preview != nil && d.preview.loaded })
	for range 6 {
		hs.settle()
	}
	m := d.preview.markdown
	pane, ok := hs.tt.Find(locales.PreviewLabel("sync.md"))
	if !ok {
		t.Fatal("no preview")
	}
	topHeading := func() string {
		first, last := m.list.Visible()
		for i := max(first, 0); i <= last; i++ {
			if m.blocks[i].kind != markdownHeading {
				continue
			}
			h := markdownRunsText(m.blocks[i].runs)
			if r, ok := hs.tt.Find(h); ok && r.Y+r.H > pane.Y+1 {
				return h
			}
		}
		return ""
	}
	if h := topHeading(); h != "Section 60" {
		t.Fatalf("the preview opened at %q, not section 60", h)
	}
	// The text opens where the preview was.
	for range 20 {
		hs.tt.Scroll(pane.X+pane.W/2, pane.Y+pane.H/2, 0, 120)
		hs.settle()
	}
	heading := topHeading()
	a.run("view.togglePreview")
	hs.settle()
	hs.settle()
	top := a.geometry(d, d.buf.Snapshot().LineCount()).at(d.vscroll.Y - editorPadY)
	if want := lineOf(heading); top < want-4 || top > want+4 {
		t.Fatalf("the text opened at line %d, the preview was at %q, line %d", top, heading, want)
	}
}
