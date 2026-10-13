package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	east "github.com/yuin/goldmark/extension/ast"
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
break. <em>HTML stays text</em>

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

<div>HTML block is displayed literally.</div>
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
	if markdownPath("", path, "https://example.com/a.png") != "" || markdownPath("", path, "//example.com/a.png") != "" || markdownPath("", path, "/tmp/a%20b.png") != "/tmp/a b.png" {
		t.Fatal("destination resolution")
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
	v.markdown.list.ScrollTo(100, ui.Start)
	hs.settle()
	before, _ := v.markdown.list.Visible()
	if before < 90 || v.markdown.scroll.Y <= 0 {
		t.Fatalf("scroll position %d, %v", before, v.markdown.scroll.Y)
	}
	source.edit(source.sel, textEdit{0, len("A paragraph."), "Updated paragraph."})
	hs.until("updated preview", func() bool {
		return v.version == d.buf.Version() && markdownRunsText(v.markdown.blocks[0].runs) == "Updated paragraph."
	})
	hs.settle()
	after, _ := v.markdown.list.Visible()
	if after != before {
		t.Fatalf("scroll moved from %d to %d", before, after)
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
	bt := ui.NewTester(b.view, 1200, 760)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bt.Frame()
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
	bt.Frame()
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
		{"light", "", false}, {"dark", "", true}, {"matrix", themeMatrix, true}, {"claude", themeClaude, true},
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
			hs.a.markdownText(c, blocks[0].runs, 24, false, "", "", false)
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

func markdownRunsText(runs []markdownInline) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.text)
	}
	return b.String()
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
	cache := map[markdownImageKey]markdownInline{}
	imageSource := []byte("![art](art.png) ![logo](logo.svg)")
	first := parseMarkdown(imageSource, path, "", nil, cache)
	second := parseMarkdown(imageSource, path, "", nil, cache)
	if len(first) != 2 || first[0].runs[0].bitmap != second[0].runs[0].bitmap || first[1].runs[0].w != 64 || first[1].runs[0].h != 32 {
		t.Fatal("image cache or SVG")
	}
	// A reused pointer proves a second bitmap was not decoded.
	future := time.Now().Add(time.Second)
	os.Chtimes(art, future, future)
	third := parseMarkdown(imageSource, path, "", nil, cache)
	if third[0].runs[0].bitmap == first[0].runs[0].bitmap || len(cache) != 2 {
		t.Fatal("mtime did not invalidate/prune cache")
	}
	parseMarkdown([]byte("no images"), path, "", nil, cache)
	if len(cache) != 0 {
		t.Fatal("unused images retained")
	}
	// A superseded parse skips images but leaves them cached for the next.
	parseMarkdown(imageSource, path, "", nil, cache)
	parseMarkdown(imageSource, path, "", func() bool { return false }, cache)
	if len(cache) != 2 {
		t.Fatal("superseded parse pruned the cache")
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
	fifo := hs.path("fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, "/dev/zero"} {
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
	os.WriteFile(hs.path("README.md"), []byte("![fifo]("+fifo+") ![zero](/dev/zero)"), 0o644)
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
	for _, theme := range []string{"", themeMatrix, themeClaude} {
		for _, dark := range []bool{false, true} {
			a.pal = paletteFor(theme, dark)
			p := a.pal
			for _, run := range []markdownInline{{text: "deleted", strike: true}, {text: "deleted link", strike: true, link: "https://example.com"}} {
				span := a.markdownSpan(run, p.text, false)
				if span.DecorationColor.A == 0 || span.DecorationColor != span.Color {
					t.Fatalf("decoration: %+v", span)
				}
			}
			code := a.markdownSpan(markdownInline{text: "code", code: true}, p.text, false)
			if code.Background.A == 0 || code.Background == p.editor || code.Color.A == 0 {
				t.Fatalf("inline code: %+v", code)
			}
			if code.Font != a.settings.FontFamily || code.Weight != 500 || code.Features != "calt=0,ss01" || code.Text != "\u2009code\u2009" {
				t.Fatalf("code font/padding: %+v", code)
			}
			latin := a.markdownSpan(markdownInline{text: "italic", italic: true}, p.text, false)
			cjk := a.markdownSpan(markdownInline{text: "中文斜体", italic: true}, p.text, false)
			if !latin.Italic || !cjk.Italic || latin.Color != p.text || cjk.Color != p.text {
				t.Fatalf("italic styles: %+v / %+v", latin, cjk)
			}
			block := a.markdownSpan(markdownInline{text: "code", code: true}, p.text, true)
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
			a.markdownText(c, []markdownInline{{text: "link", bold: true, italic: true, strike: true, code: true, link: "https://example.com"}}, size, false, "", "", false)
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
		if a.markdownSize(markdownBlock{kind: markdownParagraph}) != size || a.markdownSize(markdownBlock{kind: markdownHeading, level: 1}) != size*1.6 {
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
