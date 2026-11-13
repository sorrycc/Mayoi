package main

import (
	"strings"
	"testing"
)

// Comments are found within a line and over lines, not in code, and go
// from the Reading view, which keeps its lines.
func TestMarkdownComments(t *testing.T) {
	src := "a %%one%% b `%%code%%` c\n%%\nblock\n%%\n```\n%%fenced%%\n```\nopen %% never\n"
	var got []string
	for _, c := range markdownComments([]byte(src)) {
		got = append(got, src[c[0]:c[1]])
	}
	if strings.Join(got, "|") != "%%one%%|%%\nblock\n%%" {
		t.Fatalf("comments %q", got)
	}
	stripped := string(markdownStripComments([]byte(src)))
	if want := "a b `%%code%%` c\n\n\n\n```\n%%fenced%%\n```\nopen %% never\n"; stripped != want {
		t.Fatalf("stripped %q, want %q", stripped, want)
	}
	blocks := parseMarkdown([]byte("Keep %%drop **this**%% this.\n\n%%\n# Hidden\n%%\n\nAfter.\n"), "t.md", "", nil)
	var texts []string
	for _, b := range blocks {
		texts = append(texts, markdownRunsText(b.runs))
	}
	if strings.Join(texts, "|") != "Keep this.|After." {
		t.Fatalf("reading %q", texts)
	}
}

// In Live Preview a comment reads faint, as its source: what is in it is
// not styled.
func TestParseLiveComments(t *testing.T) {
	src := "Text %%a **b** #tag%% end.\n"
	doc := liveParse(src)
	r := []rune(src)
	for _, sp := range doc.spans {
		text := string(r[sp.start:sp.end])
		switch {
		case text == "%%a **b** #tag%%":
			if sp.look.color != liveFaint {
				t.Errorf("the comment's look %+v", sp.look)
			}
		case sp.start >= 5 && sp.end <= 21:
			t.Errorf("styled in a comment: %q %+v", text, sp)
		}
	}
}
