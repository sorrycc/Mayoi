package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func TestRenderMath(t *testing.T) {
	for _, tex := range []string{`E=ma^2`, `\frac{a}{b}+\sqrt{x^2+1}`, `\begin{pmatrix}1&2\\3&4\end{pmatrix}`, `\text{if } x>0`, `\boldsymbol{x}`} {
		for _, display := range []bool{false, true} {
			f := renderMath(tex, display)
			if f.err != "" || f.svg == nil || f.w <= 0 || f.ascent <= 0 || f.depth < 0 {
				t.Errorf("%s: %+v", tex, f)
			}
		}
	}
	if f := renderMath(`\frac{a}{b}`, false); f.depth < 0.3 {
		t.Errorf("a fraction does not reach below the baseline: %+v", f)
	}
	if f := renderMath(`\nosuchmacro`, false); !strings.Contains(f.err, `\nosuchmacro`) {
		t.Errorf("err = %q", f.err)
	}
	if f := renderMath(`\text{中文}`, false); f.err != locales.ErrMathText() {
		t.Errorf("err = %q", f.err)
	}
}

// TestMarkdownMath: GitHub's math, within text and in blocks of its own,
// parses as formulas, and the dollars that are not math stay text.
func TestMarkdownMath(t *testing.T) {
	parse := func(src string) []markdownBlock {
		return parseMarkdown([]byte(src), "", "", nil, &markdownCache{math: true})
	}
	for _, c := range []struct{ src, tex string }{
		{"$$E=ma^2$$\n", "E=ma^2"},
		{"$$\na_1 + b_1 = c_1\n$$\n", "a_1 + b_1 = c_1"},
		{"```math\n\\frac{1}{2}\n```\n", `\frac{1}{2}`},
	} {
		blocks := parse(c.src)
		if len(blocks) != 1 || blocks[0].kind != markdownMathBlock || blocks[0].formula == nil || blocks[0].formula.err != "" ||
			!blocks[0].formula.display || markdownRunsText(blocks[0].runs) != c.tex {
			t.Errorf("%q: %+v", c.src, blocks)
		}
	}
	for _, c := range []struct {
		src  string
		math []string
	}{
		{"Mass $E=mc^2$ here, and $a_1 + b_1$ *too*.\n", []string{"E=mc^2", "a_1 + b_1"}},
		{"Across $a +\nb$ lines\n", []string{"a +\nb"}},
		{"Inline $$\\sum_i x_i$$ display\n", []string{`\sum_i x_i`}},
		{"It costs $5 and $6, or $ y$.\n", nil},
		{"Escaped \\$x$ here\n", nil},
		{"`$x$` and $ and $$\n", nil},
		{"$x$5\n", nil},
	} {
		blocks := parse(c.src)
		var math []string
		for _, b := range blocks {
			if b.kind != markdownParagraph {
				t.Errorf("%q: block %+v", c.src, b)
			}
			for _, r := range b.runs {
				if r.math != nil {
					if r.math.err != "" {
						t.Errorf("%q: %s", c.src, r.math.err)
					}
					math = append(math, strings.Trim(r.text, "$"))
				}
				if r.italic && r.text != "too" {
					t.Errorf("%q: %q is italic", c.src, r.text)
				}
			}
		}
		if strings.Join(math, "|") != strings.Join(c.math, "|") {
			t.Errorf("%q: math %q, want %q", c.src, math, c.math)
		}
	}
	if blocks := parse("It costs $5 and $6.\n"); markdownRunsText(blocks[0].runs) != "It costs $5 and $6." {
		t.Errorf("dollars: %+v", blocks)
	}
	if words, _ := markdownWords(parse("One $x + y$ two\n\n$$z$$\n")); words != 2 {
		t.Errorf("words = %d, counting math", words)
	}

	// Without math, the text is as it was.
	blocks := parseMarkdown([]byte("$$E=ma^2$$ and $a_1 b_1$\n"), "", "", nil, &markdownCache{})
	if len(blocks) != 1 || markdownRunsText(blocks[0].runs) != "$$E=ma^2$$ and $a_1 b_1$" {
		t.Errorf("math off: %+v", blocks)
	}
}

// TestMathPlaces: a formula stands where the text puts its box, on the
// baseline of its line, in lines spaced as the text spaces them.
func TestMathPlaces(t *testing.T) {
	f := renderMath(`\frac{a}{b}`, false)
	font := ui.Font{Size: 20}
	object := mathSpan(f, font.Size)
	spans := []ui.Span{{Text: strings.Repeat("word ", 30)}, object, {Text: " end"}}
	tl := ui.ShapeRichText(spans, font, 300)
	objects := tl.Objects()
	if len(objects) != 1 || objects[0].Rect.Y <= 0 {
		t.Fatalf("objects %+v", objects)
	}
	// In the font's own spacing, the boxes are the layout's.
	if got := mathPlaces(spans, font, 300, 0); len(got) != 1 || got[0] != objects[0].Rect {
		t.Errorf("places %+v, want %+v", got, objects[0].Rect)
	}
	// Taller lines move it down by what the lines above grew by, and half
	// what its own did.
	natural := tl.Caret(0).H
	line := tl.Caret(len(strings.Repeat("word ", 30)) + 1)
	lines := line.Y / natural
	got := mathPlaces(spans, font, 300, 40)
	want := objects[0].Rect.Y + lines*(40-natural) + (max(40, line.H)-line.H)/2
	if len(got) != 1 || got[0].X != objects[0].Rect.X || abs32(got[0].Y-want) > 0.01 {
		t.Errorf("places %+v, want y %v", got, want)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

const mathMarkdown = "# Math\n\nThe energy $E=mc^2$ of a mass, and a fraction $\\frac{a}{b}$ within text that wraps " +
	"over more than one line of the preview, $\\sqrt{x^2+y^2}$ at its end.\n\n" +
	"$$\n\\int_0^\\infty e^{-x^2}\\,dx = \\frac{\\sqrt{\\pi}}{2}\n$$\n\n" +
	"```math\n\\begin{aligned}a &= b + c \\\\ d &= e\\end{aligned}\n```\n\n" +
	"$$\\nosuchmacro$$\n\nIt costs $5 and $6.\n"

// TestMathPreview: the preview draws the math of a document, and its
// source once math is turned off.
func TestMathPreview(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	path := hs.path("math.md")
	os.WriteFile(path, []byte(mathMarkdown), 0o644)
	a.openFile(path, true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	hs.until("math", func() bool { return d.preview != nil && d.preview.loaded })
	hs.settle()
	m := d.preview.markdown
	var blocks []markdownBlock
	inline := 0
	for _, b := range m.blocks {
		if b.kind == markdownMathBlock {
			blocks = append(blocks, b)
		}
		for _, r := range b.runs {
			if r.math != nil && r.math.err == "" {
				inline++
			}
		}
	}
	if len(blocks) != 3 || blocks[0].formula.err != "" || blocks[1].formula.err != "" || blocks[2].formula.err == "" || inline != 3 {
		t.Fatalf("%d inline, blocks %+v", inline, blocks)
	}
	hs.shot("math-markdown")
	if !hs.tt.HasText(locales.MathCannotDraw(blocks[2].formula.err)) {
		t.Error("no error under the formula that cannot be drawn")
	}
	if hs.tt.HasText("E=mc^2") || !hs.tt.HasText("It costs $5 and $6.") {
		t.Error("the source of a formula shows, or the dollars do not")
	}

	a.settings.PreviewMath = false
	hs.until("source", func() bool {
		for _, b := range d.preview.markdown.blocks {
			if b.kind == markdownMathBlock || b.formula != nil {
				return false
			}
		}
		return true
	})
	hs.settle()
	if !hs.tt.HasText("$E=mc^2$") {
		t.Error("the source of the math does not show once it is off")
	}
}

func TestShotMath(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("set SHOTS to an output directory")
	}
	hs := newHarness(t, false)
	blocks := parseMarkdown([]byte("A long line with $E=mc^2$ and a fraction $\\frac{a}{b}$ in it, then $\\sum_{i=1}^n x_i$ wraps "+
		"onto the next line with $\\sqrt{x}$ there."), "", "", nil, &markdownCache{math: true})
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Background(hs.a.pal.editor).Padding(16).Children(func() {
			hs.a.markdownText(c, nil, "", blocks[0].runs, 18, false, false)
		})
	}, 520, 140, 2)
	f, err := os.Create(filepath.Join(os.Getenv("SHOTS"), "mayoi-math-inline.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
