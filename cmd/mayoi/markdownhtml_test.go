package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestMarkdownHTML shows the HTML of a README as GitHub does, rather than
// its tags: the header of #69, centered, its logo as wide as it says.
func TestMarkdownHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	os.Mkdir(filepath.Join(dir, "assets"), 0o755)
	writePNG(t, filepath.Join(dir, "assets", "logo.png"), 680, 200)
	src := `<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png" />
    <img src="assets/logo.png" alt="logo" width="340" />
  </picture>
</p>

<p align="center">
  <a href="https://example.com"><img src="https://img.shields.io/badge/website-24292f?style=flat" alt="website" /></a>
</p>

<div align="center">

## Centered &amp; <code>code</code>

</div>

Left <a href="https://example.org">link <b>bold</b></a> <kbd>K</kbd> <span>plain</span><!-- gone --><br>next

<details>
<summary>More</summary>

<h3 id="x">Title</h3>
<script>alert("<p>")</script>
<p>One<br/>
  two &lt;tag&gt;
<hr>
</details>
`
	blocks := parseMarkdown([]byte(src), path, "", nil)
	describe := func() string {
		var b strings.Builder
		for _, bl := range blocks {
			fmt.Fprintf(&b, "\n%d level %d center %v %q", bl.kind, bl.level, bl.align, markdownRunsText(bl.runs))
		}
		return b.String()
	}
	if len(blocks) != 8 {
		t.Fatalf("blocks: %s", describe())
	}
	if logo := blocks[0]; logo.kind != markdownImageBlock || logo.align != ui.Center || len(logo.runs) != 1 || logo.runs[0].bitmap == nil || logo.runs[0].w != 340 || logo.runs[0].h != 100 {
		t.Fatalf("logo: %+v", logo)
	}
	// A picture of the web not fetched is its alt text, a link still.
	if badge := blocks[1]; badge.kind != markdownParagraph || badge.align != ui.Center || len(badge.runs) != 1 || badge.runs[0].text != "website" || badge.runs[0].link != "https://example.com" || !badge.runs[0].raw {
		t.Fatalf("badge: %+v", badge)
	}
	// Markdown between the tags of a <div> is in it.
	if h := blocks[2]; h.kind != markdownHeading || h.level != 2 || h.align != ui.Center || markdownRunsText(h.runs) != "Centered & code" || !h.runs[len(h.runs)-1].code {
		t.Fatalf("heading in div: %+v", h)
	}
	p := blocks[3]
	if p.align != ui.Start || markdownRunsText(p.runs) != "Left link bold K plain\nnext" {
		t.Fatalf("inline HTML: %q", markdownRunsText(p.runs))
	}
	var link, bold, kbd bool
	for _, r := range p.runs {
		link = link || r.link == "https://example.org" && strings.HasPrefix(r.text, "link")
		bold = bold || r.bold && r.text == "bold" && r.link == "https://example.org"
		kbd = kbd || r.code && r.text == "K"
		if r.raw {
			t.Fatalf("tag left as text: %+v", r)
		}
	}
	if !link || !bold || !kbd {
		t.Fatalf("inline styles: %+v", p.runs)
	}
	if s := blocks[4]; markdownRunsText(s.runs) != "More" || !s.runs[0].bold {
		t.Fatalf("summary: %+v", s)
	}
	if h := blocks[5]; h.kind != markdownHeading || h.level != 3 || h.slug != "title" || markdownRunsText(h.runs) != "Title" {
		t.Fatalf("HTML heading: %+v", h)
	}
	if one := blocks[6]; one.kind != markdownParagraph || markdownRunsText(one.runs) != "One\ntwo <tag>" {
		t.Fatalf("paragraph: %q", markdownRunsText(one.runs))
	}
	if blocks[7].kind != markdownRule {
		t.Fatalf("rule: %s", describe())
	}
	// A paragraph's end tag left out, the Markdown after it is not in it.
	after := parseMarkdown([]byte("<p align=\"center\">Centered\n\n<p>Plain\n\nText"), path, "", nil)
	if len(after) != 3 || after[0].align != ui.Center || after[1].align != ui.Start || after[2].align != ui.Start {
		t.Fatalf("unclosed paragraphs: %+v", after)
	}
}

// TestMarkdownRemoteImages fetches the pictures of the web a preview shows
// in the background, once, and has it parse again once they arrived; an
// SVG with text, which would show without its text, stays its alt text.
func TestMarkdownRemoteImages(t *testing.T) {
	art := filepath.Join(t.TempDir(), "art.png")
	writePNG(t, art, 40, 20)
	data, err := os.ReadFile(art)
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	// The server answers once the first parse is looked at: a fetch it
	// starts could otherwise arrive before it reaches the same picture
	// further down.
	answer := make(chan struct{})
	var answering sync.Once
	letAnswer := func() { answering.Do(func() { close(answer) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-answer
		switch r.URL.Path {
		case "/art.png":
			w.Write(data)
		case "/badge":
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="80" height="20"><rect width="80" height="20"/><text x="4" y="14">build</text></svg>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer letAnswer()
	markdownRemote.Lock()
	get := markdownRemote.get
	markdownRemote.get = markdownHTTPGet
	markdownRemote.Unlock()
	defer func() {
		markdownRemote.Lock()
		markdownRemote.get = get
		markdownRemote.Unlock()
	}()

	path := filepath.Join(t.TempDir(), "readme.md")
	src := []byte(fmt.Sprintf(`<p align="center"><a href="https://example.com"><img src="%[1]s/art.png" alt="Art"></a> <img src="%[1]s/badge" alt="build"> <img src="%[1]s/missing.png" alt="gone"></p>

![Art again](%[1]s/art.png)`, srv.URL))
	// Without somewhere to tell, the parse fetches nothing.
	parseMarkdown(src, path, "", nil)
	if hits.Load() != 0 {
		t.Fatal("fetched without a preview to show it")
	}
	arrived := make(chan struct{}, 8)
	c := &markdownCache{arrived: func() { arrived <- struct{}{} }}
	first := parseMarkdown(src, path, "", nil, c)
	for _, b := range first {
		if b.kind == markdownImageBlock {
			t.Fatal("shown before fetched")
		}
	}
	letAnswer()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("no picture arrived")
	}
	// Every fetch done: the one that arrived may be the badge's.
	deadline := time.Now().Add(10 * time.Second)
	for _, u := range []string{"/art.png", "/badge", "/missing.png"} {
		for {
			if _, done := markdownFetch(srv.URL+u, nil); done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s not fetched", u)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	second := parseMarkdown(src, path, "", nil, c)
	if len(second) != 3 {
		t.Fatalf("blocks: %+v", second)
	}
	if b := second[0]; b.kind != markdownImageBlock || b.align != ui.Center || b.runs[0].bitmap == nil || b.runs[0].w != 40 || b.runs[0].link != "https://example.com" {
		t.Fatalf("remote picture: %+v", b)
	}
	if b := second[1]; b.kind != markdownParagraph || markdownRunsText(b.runs) != "build gone" || b.runs[0].bitmap != nil {
		t.Fatalf("badge and missing: %q", markdownRunsText(b.runs))
	}
	if b := second[2]; b.kind != markdownImageBlock || b.runs[0].bitmap != second[0].runs[0].bitmap {
		t.Fatal("Markdown picture of the web not shown")
	}
	if n := hits.Load(); n != 3 {
		t.Fatalf("%d fetches, not one each", n)
	}
}

// TestMarkdownHTMLTables shows a <table> as a table, as GitHub does: its
// first row its header when all of it is <th>, each cell aligned as it
// says, a grid of pictures and names without a header, a cell spanning
// columns, a table in a cell its text, and Markdown in a cell its own.
func TestMarkdownHTMLTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	writePNG(t, filepath.Join(dir, "a.png"), 100, 100)
	src := `<table>
<caption>Features</caption>
<tr><th>Feature</th><th align="center">Status</th></tr>
<tr><td><b>Tables</b></td><td align="center">yes <a href="https://example.com">link</a></td></tr>
<tr><td colspan="2">Wide</td></tr>
<tr><td><table><tr><td>in</td><td>ner</td></tr></table></td><td>after</td></tr>
</table>

<table>
  <tr>
    <td align="center"><img src="a.png" width="60"><br><sub><b>alice</b></sub></td>
    <td align="center"><img src="a.png" width="60"><br><sub><b>bob</b></sub></td>
  </tr>
</table>

<table><tr><td>

**Markdown** here

</td></tr></table>

After.
`
	blocks := parseMarkdown([]byte(src), path, "", nil)
	var got []string
	for _, b := range blocks {
		line := strconv.Itoa(int(b.kind)) + ":"
		if b.kind == markdownTable || b.kind == markdownTableHeader {
			for k, cell := range b.cells {
				line += markdownRunsText(cell) + "@" + strconv.Itoa(int(b.aligns[k])) + ","
			}
			line += strconv.Itoa(b.table)
		} else {
			line += markdownRunsText(b.runs)
		}
		got = append(got, line)
	}
	want := []string{"0:Features",
		"6:Feature@0,Status@3,1", "5:Tables@0,yes link@3,1", "5:Wide@0,@0,1", "5:in ner@0,after@0,1",
		"5:a.pngalice@3,a.pngbob@3,5",
		"5:Markdown here@0,6", "0:After."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("tables:\n got %q\nwant %q", got, want)
	}
	if blocks[0].align != ui.Center {
		t.Error("the caption is not centered")
	}
	if head := blocks[1]; !head.cells[0][0].bold {
		t.Error("the header is not bold")
	}
	if !blocks[4].last || blocks[3].last {
		t.Error("the last row is not marked")
	}
	// A picture stands on its own line, the name under it smaller.
	cell := blocks[5].cells[0]
	if len(cell) != 2 || cell[0].bitmap == nil || cell[0].w != 60 || cell[1].text != "alice" || cell[1].script != -1 || !cell[1].bold {
		t.Fatalf("contributor cell %+v", cell)
	}
	if blocks[5].kind != markdownTable || blocks[5].row != 1 {
		t.Error("a table without <th> has a header")
	}
}

// TestMarkdownHTMLLists shows <ul> and <ol> as lists, with their markers,
// nested, numbered from start, the text after an item's list in it.
func TestMarkdownHTMLLists(t *testing.T) {
	src := `<ul>
  <li>First</li>
  <li>Second
    <ol start="9">
      <li>Nine</li>
      <li>Ten</li>
    </ol>
  </li>
  <li>Third
</ul>

After.
`
	blocks := parseMarkdown([]byte(src), "test.md", "", nil)
	var got []string
	for _, b := range blocks {
		got = append(got, fmt.Sprintf("%s %d %q", markdownRunsText(b.runs), b.indent, b.marker))
	}
	want := []string{`First 1 "•"`, `Second 1 "•"`, `Nine 2 "9."`, `Ten 2 "10."`, `Third 1 "•"`, `After. 0 ""`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lists:\n got %q\nwant %q", got, want)
	}
	outer, inner := blocks[0], blocks[2]
	if outer.offset != 0 || outer.markerWidth != 1.5 || !outer.tight {
		t.Errorf("item %+v", outer)
	}
	// The numbers stand after the outer list's text, as wide as "10.".
	if inner.offset != outer.markerWidth+0.5 || inner.markerWidth < 1.9 {
		t.Errorf("nested item offset %v width %v", inner.offset, inner.markerWidth)
	}
}

// TestMarkdownHTMLDetails folds a <details> under its summary, as GitHub
// does, open at first when it says, the Markdown between its tags in it,
// and one without a summary under GitHub's, Details.
func TestMarkdownHTMLDetails(t *testing.T) {
	src := `<details>
<summary>More</summary>

Hidden **text**.

</details>

<details open><summary>Open</summary><p>Shown.</p></details>

<details><p>Bare.</p></details>

After.
`
	blocks := parseMarkdown([]byte(src), "test.md", "", nil)
	doc := &document{}
	shown := func() string {
		var got []string
		for _, b := range blocks {
			if !calloutHidden(doc, b.folds) {
				got = append(got, markdownRunsText(b.runs))
			}
		}
		return strings.Join(got, "|")
	}
	if b := blocks[0]; b.fold != '-' || b.foldKey == "" || !b.runs[0].bold {
		t.Fatalf("summary %+v", b)
	}
	if got := shown(); got != "More|Open|Shown.|Details|After." {
		t.Fatalf("shows %q", got)
	}
	a := &app{}
	a.toggleCallout(doc, blocks[0].foldKey, blocks[0].fold)
	a.toggleCallout(doc, blocks[2].foldKey, blocks[2].fold)
	if got := shown(); got != "More|Hidden text.|Open|Details|After." {
		t.Fatalf("toggled shows %q", got)
	}
}

// TestMarkdownHTMLDetailsClick opens a <details> in the Reading view by a
// click on its summary.
func TestMarkdownHTMLDetailsClick(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("README.md"), []byte("<details>\n<summary>More</summary>\n\nHidden text.\n\n</details>\n"), 0o644)
	a.openFile(hs.path("README.md"), true)
	d := a.activeEditor()
	a.run("view.togglePreview")
	hs.until("preview", func() bool { return d.preview != nil && d.preview.loaded })
	hs.settle()
	if hs.tt.HasText("Hidden text.") {
		t.Fatal("folded text shown")
	}
	if err := hs.tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !hs.tt.HasText("Hidden text.") {
		t.Fatal("a click on the summary did not open it")
	}
}

// TestMarkdownHTMLText styles the text tags of HTML: <sup> and <sub>
// raised and lowered in the characters for them, or smaller, <u> and
// <ins> underlined, <small> smaller; and the blocks: align="right", a
// <blockquote> a quote, a <pre> code keeping its spaces.
func TestMarkdownHTMLText(t *testing.T) {
	src := `<p>E = mc<sup>2</sup>, H<sub>2</sub>O, x<sup>th</sup> <u>u</u> <ins>ins</ins> <small>small</small></p>

<p align="right">Right</p>

<blockquote>Quoted</blockquote>

<pre>
one
  two</pre>

Inline mc<sup>2</sup>.
`
	blocks := parseMarkdown([]byte(src), "test.md", "", nil)
	if len(blocks) != 5 {
		t.Fatalf("blocks %+v", blocks)
	}
	p := blocks[0]
	if got := markdownRunsText(p.runs); got != "E = mc², H₂O, xth u ins small" {
		t.Fatalf("text %q", got)
	}
	var th, u, ins, small bool
	for _, r := range p.runs {
		th = th || r.text == "th" && r.script == 1
		u = u || r.text == "u" && r.underline
		ins = ins || r.text == "ins" && r.underline
		small = small || r.text == "small" && r.small
	}
	if !th || !u || !ins || !small {
		t.Fatalf("styles %+v", p.runs)
	}
	if blocks[1].align != ui.End {
		t.Error("align=right is not")
	}
	if q := blocks[2]; q.quote != 1 || markdownRunsText(q.runs) != "Quoted" {
		t.Errorf("blockquote %+v", q)
	}
	if pre := blocks[3]; pre.kind != markdownCodeBlock || markdownRunsText(pre.runs) != "one\n  two" {
		t.Errorf("pre %q", markdownRunsText(pre.runs))
	}
	if got := markdownRunsText(blocks[4].runs); got != "Inline mc²." {
		t.Errorf("inline sup %q", got)
	}
}

// TestMarkdownHTMLPicture keeps a <picture>'s sources for the dark theme
// and the light one beside its <img>, as wide as it says.
func TestMarkdownHTMLPicture(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "light.png"), 200, 100)
	writePNG(t, filepath.Join(dir, "dark.png"), 200, 100)
	src := `<picture>
  <source media="(prefers-color-scheme: dark)" srcset="dark.png 2x">
  <img src="light.png" width="100">
</picture>
`
	blocks := parseMarkdown([]byte(src), filepath.Join(dir, "README.md"), "", nil)
	if len(blocks) != 1 || len(blocks[0].runs) != 1 {
		t.Fatalf("blocks %+v", blocks)
	}
	r := blocks[0].runs[0]
	if r.bitmap == nil || r.dark == nil || r.dark.bitmap == nil || r.dark.w != 100 || r.dark.image != "dark.png" || r.light != nil {
		t.Fatalf("picture %+v", r)
	}
}
