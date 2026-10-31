package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWikiLinkAt(t *testing.T) {
	for _, c := range []struct {
		s    string
		want wikiLink
		n    int
	}{
		{"[[note]] after", wikiLink{target: "note"}, 8},
		{"[[王健康/评估|王健康]]、", wikiLink{target: "王健康/评估", label: "王健康"}, len("[[王健康/评估|王健康]]")},
		{"[[note#Some heading|label]]", wikiLink{target: "note", heading: "Some heading", label: "label"}, 27},
		{"[[#Heading]]", wikiLink{heading: "Heading"}, 12},
		{`[[note\|label]]`, wikiLink{target: "note", label: "label"}, 15},
		{"![[pic.png|300]]", wikiLink{target: "pic.png", label: "300", embed: true}, 16},
		{"[[]]", wikiLink{}, 0},
		{"[[a]b]]", wikiLink{}, 0},
		{"[[open", wikiLink{}, 0},
		{"[note]", wikiLink{}, 0},
		{"![pic](a.png)", wikiLink{embed: true}, 0},
	} {
		l, n := wikiLinkAt(c.s)
		if n != c.n || n > 0 && l != c.want {
			t.Errorf("%q: got %+v %d, want %+v %d", c.s, l, n, c.want, c.n)
		}
	}
}

func TestWikiLinkIn(t *testing.T) {
	line := "见 [[王健康/评估|王健康]] 和 [[b]]"
	for at, want := range map[int]string{0: "", 2: "王健康/评估", 10: "王健康/评估", 16: "王健康/评估", 17: "", 19: "b", 24: "b"} {
		l, ok := wikiLinkIn(line, at)
		if ok != (want != "") || l.target != want {
			t.Errorf("at %d: got %q %v, want %q", at, l.target, ok, want)
		}
	}
}

func TestMarkdownWikiLinks(t *testing.T) {
	root := t.TempDir()
	write := func(rel, s string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	path := write("daily/today.md", "")
	write("daily/near.md", "")
	write("top.md", "")
	write("people/王健康/评估.md", "")
	write("deep/a/b/王健康/评估.md", "")
	write("Notes/Mixed Case.md", "")
	write(".hidden/secret.md", "")
	write("assets/.keep", "")
	writePNG(t, filepath.Join(root, "assets", "art.png"), 80, 40)

	source := "[[near]] [[top]] [[王健康/评估|王健康]] [[mixed case#Some Heading]] [[missing|gone]] [[secret]] [[#Local Heading]] `[[code]]`"
	var got strings.Builder
	for _, r := range parseMarkdown([]byte(source), path, root, nil)[0].runs {
		if r.link != "" {
			got.WriteString("[" + r.text + "](" + r.link + ")")
		} else {
			got.WriteString(r.text)
		}
	}
	want := "[near](near.md) [top](../top.md) [王健康](../people/%E7%8E%8B%E5%81%A5%E5%BA%B7/%E8%AF%84%E4%BC%B0.md) " +
		"[mixed case > Some Heading](../Notes/Mixed%20Case.md#Some%20Heading) gone secret [Local Heading](#local-heading) [[code]]"
	if got.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", got.String(), want)
	}
	// Each destination opens the file found.
	if p := markdownPath(root, path, "../people/%E7%8E%8B%E5%81%A5%E5%BA%B7/%E8%AF%84%E4%BC%B0.md"); p != filepath.Join(root, "people", "王健康", "评估.md") {
		t.Fatalf("destination resolves to %q", p)
	}

	// An embedded picture shows, as wide as its size says.
	blocks := parseMarkdown([]byte("![[art.png|40]] ![[art.png]] ![[top]]"), path, root, nil)
	var runs []markdownInline
	for _, b := range blocks {
		runs = append(runs, b.runs...)
	}
	if len(runs) < 3 || runs[0].bitmap == nil || runs[0].w != 40 || runs[0].h != 20 || runs[1].bitmap == nil || runs[1].w != 80 {
		t.Fatalf("embedded pictures: %+v", runs)
	}
	if last := runs[len(runs)-1]; last.text != "top" || last.link != "../top.md" {
		t.Fatalf("embedded note: %+v", last)
	}
}

func TestMarkdownHeadingLine(t *testing.T) {
	source := []byte("---\ntitle: x\n---\n# Top\n\n```\n# not a heading\n```\n\n## Some **Heading**\n\nText\n\nSetext\n---\n\n## Top\n")
	for heading, want := range map[string]int{"Some Heading": 9, "some-heading": 9, "Setext": 13, "Top": 3, "top-1": 16, "nope": -1} {
		if got := markdownHeadingLine(source, heading); got != want {
			t.Errorf("%q: got line %d, want %d", heading, got, want)
		}
	}
}
