package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func benchMarkdown(b *testing.B, side bool) *harness {
	hs := newHarness(b, true)
	unit := "## Section\n\nA paragraph with **bold**, *italic*, `code` and [a link](https://example.com), long enough to wrap across the preview's width once or twice.\n\n- one\n- two\n  - nested\n\n> a quote\n\n```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n"
	os.WriteFile(hs.path("big.md"), []byte(strings.Repeat(unit, 300)), 0o644)
	hs.a.openFile(hs.path("big.md"), true)
	if side {
		hs.a.run("view.previewToSide")
	} else {
		hs.a.run("view.togglePreview")
	}
	hs.until("preview", func() bool { return len(hs.a.cols[len(hs.a.cols)-1].tab().ed.preview.markdown.blocks) > 0 })
	hs.settle()
	return hs
}

func benchMarkdownScroll(b *testing.B, side bool) {
	hs := benchMarkdown(b, side)
	m := hs.a.cols[len(hs.a.cols)-1].tab().ed.preview.markdown
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		m.scroll.Y = max(0, m.scroll.Y+dy)
		hs.tt.Frame()
	}
}

func BenchmarkMarkdownScrollFrame(b *testing.B)     { benchMarkdownScroll(b, false) }
func BenchmarkMarkdownSideScrollFrame(b *testing.B) { benchMarkdownScroll(b, true) }

func TestMarkdownSideScrollSteady(t *testing.T) {
	hs := newHarness(t, true)
	var doc strings.Builder
	for i := range 300 {
		fmt.Fprintf(&doc, "## Section %d\n\nA paragraph with **bold**, *italic*, `code` and [a link](https://example.com), long enough to wrap across the preview's width once or twice.\n\n- one\n- two\n  - nested\n\n> a quote\n\n```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n", i)
	}
	os.WriteFile(hs.path("big.md"), []byte(doc.String()), 0o644)
	hs.a.openFile(hs.path("big.md"), true)
	hs.a.run("view.previewToSide")
	src := hs.a.cols[0].tab().ed
	m := hs.a.cols[1].tab().ed.preview.markdown
	hs.until("preview", func() bool { return len(m.blocks) > 0 })
	hs.settle()
	// The headings the preview shows, by where they are on the screen: the
	// preview moves the way its text does, never back.
	shown := func() map[int]float32 {
		at := map[int]float32{}
		for i := range 300 {
			if r, ok := hs.tt.Find(fmt.Sprintf("Section %d", i)); ok {
				at[i] = r.Y
			}
		}
		return at
	}
	for _, dy := range []float32{40, -40} {
		was, back := shown(), 0
		for range 80 {
			hs.tt.Move(420, 300)
			hs.tt.Scroll(420, 300, 0, dy)
			hs.tt.Frame()
			now := shown()
			for i, y := range now {
				if y0, ok := was[i]; ok && (y-y0)*dy > 0.5 {
					back++
					break
				}
			}
			was = now
		}
		if back > 0 {
			t.Errorf("scrolling by %v, the preview went back %d times", dy, back)
		}
		if dy > 0 && src.vscroll.Y == 0 {
			t.Fatal("the text did not scroll")
		}
	}
}
