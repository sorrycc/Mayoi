package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// benchMarkdown is the changelog and the docs, some thousands of lines
// of Markdown, for the cost of a parse and of a frame of the preview:
// go test -short -run XXX -bench Markdown
func benchMarkdown(b *testing.B) []byte {
	var src []byte
	for _, name := range []string{"../../CHANGELOG.md", "../../docs/FEATURES.md", "../../docs/ARCHITECTURE.md", "../../docs/SETTINGS.md", "../../README.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			b.Fatal(err)
		}
		src = append(append(src, data...), '\n')
	}
	return src
}

func BenchmarkMarkdownParse(b *testing.B) {
	src := benchMarkdown(b)
	path := filepath.Join(b.TempDir(), "doc.md")
	cache := &markdownCache{}
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for range b.N {
		parseMarkdown(src, path, "", nil, cache)
	}
}

func benchMarkdownPreview(b *testing.B) (*harness, *editor) {
	hs := newHarness(b, true)
	os.WriteFile(hs.path("big.md"), benchMarkdown(b), 0o644)
	hs.a.openFile(hs.path("big.md"), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.a.setPreview(d, true)
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	return hs, d
}

func BenchmarkMarkdownIdleFrame(b *testing.B) {
	hs, _ := benchMarkdownPreview(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step(hs.tt)
	}
}

func BenchmarkMarkdownScrollFrame(b *testing.B) {
	hs, _ := benchMarkdownPreview(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(80)
		if i/200%2 == 1 {
			dy = -80
		}
		hs.tt.Scroll(600, 400, 0, dy)
		step(hs.tt)
	}
}

// The Reading view's parse a moment after a key typed, as it parses the
// whole note with the cache of the last parse, which keeps what each of
// its blocks of code, formulas and diagrams drew: against one with none.
// go test -short -run XXX -bench MarkdownReparse
func BenchmarkMarkdownReparse(b *testing.B) {
	// Each section's code and formula its own, as a note's are.
	var src []byte
	for i, part := range strings.SplitAfter(liveBench(), "## Section\n") {
		part = strings.ReplaceAll(part, "x^2", "x^{"+strconv.Itoa(i)+"}")
		src = append(src, strings.ReplaceAll(part, "return 1", "return "+strconv.Itoa(i))...)
	}
	for _, warm := range []bool{true, false} {
		name := "cold"
		if warm {
			name = "warm"
		}
		b.Run(name, func(b *testing.B) {
			cache := &markdownCache{math: true}
			parseMarkdown(src, "/note.md", "", nil, cache)
			edited := append(slices.Clone(src), "a\n"...)
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			for i := 0; b.Loop(); i++ {
				c := cache
				if !warm {
					c = &markdownCache{math: true}
				}
				// One parse of the text before the key, the next of after.
				text := src
				if i%2 == 1 {
					text = edited
				}
				parseMarkdown(text, "/note.md", "", nil, c)
			}
		})
	}
}
