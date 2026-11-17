package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
			fmt.Fprintf(&b, "\n%d level %d center %v %q", bl.kind, bl.level, bl.center, markdownRunsText(bl.runs))
		}
		return b.String()
	}
	if len(blocks) != 8 {
		t.Fatalf("blocks: %s", describe())
	}
	if logo := blocks[0]; logo.kind != markdownImageBlock || !logo.center || len(logo.runs) != 1 || logo.runs[0].bitmap == nil || logo.runs[0].w != 340 || logo.runs[0].h != 100 {
		t.Fatalf("logo: %+v", logo)
	}
	// A picture of the web not fetched is its alt text, a link still.
	if badge := blocks[1]; badge.kind != markdownParagraph || !badge.center || len(badge.runs) != 1 || badge.runs[0].text != "website" || badge.runs[0].link != "https://example.com" || !badge.runs[0].raw {
		t.Fatalf("badge: %+v", badge)
	}
	// Markdown between the tags of a <div> is in it.
	if h := blocks[2]; h.kind != markdownHeading || h.level != 2 || !h.center || markdownRunsText(h.runs) != "Centered & code" || !h.runs[len(h.runs)-1].code {
		t.Fatalf("heading in div: %+v", h)
	}
	p := blocks[3]
	if p.center || markdownRunsText(p.runs) != "Left link bold K plain\nnext" {
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
	if len(after) != 3 || !after[0].center || after[1].center || after[2].center {
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
	if b := second[0]; b.kind != markdownImageBlock || !b.center || b.runs[0].bitmap == nil || b.runs[0].w != 40 || b.runs[0].link != "https://example.com" {
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
