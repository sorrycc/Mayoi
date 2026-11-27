package main

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
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
	// A labelled equation is drawn with its label beside it.
	for _, tex := range []string{`E=mc^2 \tag{1}`, `x \tag*{A}`, `\begin{align}a&=b\tag{1}\\c&=d\tag{2}\end{align}`} {
		f, plain := renderMath(tex, true), renderMath(`E=mc^2`, true)
		if f.err != "" || f.svg == nil || f.w <= 0 || f.ascent <= 0 || f.depth < 0 {
			t.Errorf("%s: %+v", tex, f)
		}
		if tex == `E=mc^2 \tag{1}` && f.w < plain.w+1 {
			t.Errorf("%s is no wider than without its label: %v, %v", tex, f.w, plain.w)
		}
	}
	if f := renderMath(`\frac{a}{b}`, false); f.depth < 0.3 {
		t.Errorf("a fraction does not reach below the baseline: %+v", f)
	}
	if f := renderMath(`\nosuchmacro`, false); !strings.Contains(f.err, `\nosuchmacro`) {
		t.Errorf("err = %q", f.err)
	}
	// Text MathJax sets is drawn in the window's fonts, not refused.
	if f := renderMath(`\text{中文}`, false); f.err != "" || f.svg == nil || f.w <= 0 {
		t.Errorf("\\text{中文}: %+v", f)
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
	if got := mathPlaces(spans, font, 300, 0, ui.Start); len(got) != 1 || got[0] != objects[0].Rect {
		t.Errorf("places %+v, want %+v", got, objects[0].Rect)
	}
	// Taller lines move it down by what the lines above grew by, and half
	// what its own did.
	natural := tl.Caret(0).H
	line := tl.Caret(len(strings.Repeat("word ", 30)) + 1)
	lines := line.Y / natural
	got := mathPlaces(spans, font, 300, 40, ui.Start)
	want := objects[0].Rect.Y + lines*(40-natural) + (max(40, line.H)-line.H)/2
	if len(got) != 1 || got[0].X != objects[0].Rect.X || abs32(got[0].Y-want) > 0.01 {
		t.Errorf("places %+v, want y %v", got, want)
	}
}

// TestMathPlacesAligned: in centered text, as of <p align="center">, a
// box stands as far into its line as the line is in from the start, and
// in text to the right twice that.
func TestMathPlacesAligned(t *testing.T) {
	font := ui.Font{Size: 20}
	spans := []ui.Span{{Text: "short"}, markdownExternalSpan(font.Size)}
	start := mathPlaces(spans, font, 300, 0, ui.Start)
	w, _ := ui.ShapeRichText(spans, font, 300).Size()
	center := mathPlaces(spans, font, 300, 0, ui.Center)
	right := mathPlaces(spans, font, 300, 0, ui.End)
	if len(start) != 1 || len(center) != 1 || len(right) != 1 {
		t.Fatalf("places %v %v %v", start, center, right)
	}
	in := center[0].X - start[0].X
	if abs32(in-(300-w)/2) > 1 || abs32(right[0].X-start[0].X-2*in) > 0.01 {
		t.Errorf("start %v center %v right %v, the line %v wide", start[0].X, center[0].X, right[0].X, w)
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
			hs.a.markdownText(c, nil, "", blocks[0].runs, 18, hs.a.pal.text, false, ui.Start, nil)
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

// TestMarkdownMathFence: a display formula that opens a line of its own
// keeps every line up to its end, a line of = alone included, which
// Goldmark reads as a heading underline otherwise, as the = of a matrix
// product's row does.
func TestMarkdownMathFence(t *testing.T) {
	src := "$$\n" +
		"\\mathbf{A}\\mathbf{x} =\n" +
		"\\begin{bmatrix}\na_{11} & a_{12} \\\\\na_{21} & a_{22}\n\\end{bmatrix}\n" +
		"=\n" +
		"\\begin{bmatrix}\nx_1 \\\\\nx_2\n\\end{bmatrix}\n" +
		"$$\n"

	blocks := parseMarkdown([]byte(src), "", "", nil, &markdownCache{math: true})
	n := 0
	for _, b := range blocks {
		if b.kind != markdownMathBlock {
			continue
		}
		n++
		if b.formula == nil || b.formula.err != "" {
			t.Errorf("the matrix product does not render: %+v", b.formula)
		}
		if !strings.Contains(b.runs[0].text, "a_{21}") {
			t.Errorf("the formula lost a row: %q", b.runs[0].text)
		}
	}
	if n != 1 {
		t.Fatalf("%d display formulas, want 1", n)
	}

	// A heading still follows a paragraph that is not a formula.
	head := parseMarkdown([]byte("text\n=\n"), "", "", nil, &markdownCache{math: true})
	if len(head) != 1 || head[0].kind != markdownHeading {
		t.Errorf("a setext heading is not a heading: %+v", head)
	}
}

// TestMarkdownMathHoldsTogether: a display formula stays whole over a
// line of = or - alone, which Goldmark would end it on as a heading's
// underline, a rule, or a table's delimiter row, and the parsers that
// would end it work again below the formula's own closing $$.
func TestMarkdownMathHoldsTogether(t *testing.T) {
	parse := func(src string) []markdownBlock {
		return parseMarkdown([]byte(src), "", "", nil, &markdownCache{math: true})
	}
	kinds := func(blocks []markdownBlock) []markdownKind {
		out := make([]markdownKind, len(blocks))
		for i, b := range blocks {
			out[i] = b.kind
		}
		return out
	}
	for _, c := range []struct {
		name, src string
		want      []markdownKind
	}{
		{
			name: "a line of = alone",
			src:  "$$\na\n=\nb\n$$\n",
			want: []markdownKind{markdownMathBlock},
		},
		{
			name: "a line of - alone",
			src:  "$$\na\n-\nb\n$$\n",
			want: []markdownKind{markdownMathBlock},
		},
		{
			name: "a line of three -",
			src:  "$$\na\n---\nb\n$$\n",
			want: []markdownKind{markdownMathBlock},
		},
		{
			name: "a list below the formula",
			src:  "$$\nx\n$$\n- one\n- two\n",
			want: []markdownKind{markdownMathBlock, markdownParagraph, markdownParagraph},
		},
		{
			name: "a heading below the formula",
			src:  "$$\nx\n$$\nTitle\n===\n",
			want: []markdownKind{markdownHeading},
		},
		{
			name: "a rule below the formula",
			src:  "$$\nx\n$$\n\n---\n",
			want: []markdownKind{markdownMathBlock, markdownRule},
		},
		{
			name: "a table below the formula",
			src:  "$$\nx\n$$\n\n| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownMathBlock, markdownTableHeader, markdownTable},
		},
		{
			name: "a table immediately below the formula",
			src:  "$$\nx\n$$\n| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownMathBlock, markdownTableHeader, markdownTable},
		},
		{
			name: "a formula with - immediately before a table",
			src:  "$$\na\n-\nb\n$$\n| c | d |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownMathBlock, markdownTableHeader, markdownTable},
		},
		{
			name: "text and a table immediately below the formula",
			src:  "$$\nx\n$$\nintro\n| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownParagraph, markdownTableHeader, markdownTable},
		},
		{
			name: "plain text immediately below the formula",
			src:  "$$\nx\n$$\nhello\n",
			want: []markdownKind{markdownParagraph},
		},
		{
			name: "a setext heading of =",
			src:  "Title\n===\n",
			want: []markdownKind{markdownHeading},
		},
		{
			name: "a setext heading of -",
			src:  "Title\n---\n",
			want: []markdownKind{markdownHeading},
		},
		{
			name: "a rule of its own",
			src:  "a\n\n---\n\n",
			want: []markdownKind{markdownParagraph, markdownRule},
		},
		{
			name: "a list of its own",
			src:  "- one\n- two\n",
			want: []markdownKind{markdownParagraph, markdownParagraph},
		},
		{
			name: "a table of its own",
			src:  "| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownTableHeader, markdownTable},
		},
		{
			name: "an unclosed formula over a list marker",
			src:  "$$\nx\n- y\n",
			want: []markdownKind{markdownParagraph, markdownParagraph},
		},
		{
			name: "an unclosed formula over a table",
			src:  "$$\n| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownParagraph, markdownTableHeader, markdownTable},
		},
		{
			name: "an unclosed formula, a line, then a table",
			src:  "$$\nx\n| a | b |\n| - | - |\n| 1 | 2 |\n",
			want: []markdownKind{markdownParagraph, markdownTableHeader, markdownTable},
		},
		{
			name: "an unclosed formula over a line of - alone",
			src:  "$$\na\n-\nb\n",
			want: []markdownKind{markdownParagraph},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := kinds(parse(c.src)); !slices.Equal(got, c.want) {
				t.Errorf("%q: %v, want %v", c.src, got, c.want)
			}
		})
	}

	// A table parses with math on and off: the transformer that reads a
	// display formula's lines is the table's own, and must be there
	// either way.
	for _, math := range []bool{false, true} {
		src := "| a | b |" + "\n" + "| - | - |" + "\n" + "| 1 | 2 |" + "\n"
		blocks := parseMarkdown([]byte(src), "", "", nil, &markdownCache{math: math})
		if len(blocks) != 2 || blocks[0].kind != markdownTableHeader || len(blocks[0].aligns) != 2 {
			var got []markdownKind
			for _, b := range blocks {
				got = append(got, b.kind)
			}
			t.Errorf("math %v: a table does not parse: %v", math, got)
		}
	}

	// With math off, a $$ is text and the parsers end a paragraph below it
	// as they always did.
	for _, c := range []struct {
		name, src string
		want      []markdownKind
	}{
		{"a formula that is not one", "$$\na\n-\nb\n$$\n", []markdownKind{markdownHeading, markdownParagraph}},
		{"a setext heading", "text\n-\n", []markdownKind{markdownHeading}},
		{"a list below the dollars", "$$\nx\n$$\n- a\n- b\n", []markdownKind{markdownParagraph, markdownParagraph, markdownParagraph}},
	} {
		t.Run("math off: "+c.name, func(t *testing.T) {
			blocks := parseMarkdown([]byte(c.src), "", "", nil, &markdownCache{})
			got := make([]markdownKind, len(blocks))
			for i, b := range blocks {
				got[i] = b.kind
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("%q: %v, want %v", c.src, got, c.want)
			}
		})
	}
}

// TestMarkdownMathDrawsItsText: a formula whose SVG holds the text MathJax
// sets for characters its math font lacks draws that text, in the window's
// fonts. An icon is drawn from a mask, which rasterizes without fonts, so
// such a formula draws as a picture instead: as an icon, its text is a gap.
func TestMarkdownMathDrawsItsText(t *testing.T) {
	dark := func(f *mathFormula, icon bool) int {
		const w, h = 240, 80
		img := ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Children(func() {
				ui.Box(c).Width(w).Height(h).Draw(func(pt *ui.Painter, r ui.Rect) {
					if icon {
						pt.Icon(f.svg, r, ui.RGB(0, 0, 0))
						return
					}
					f.draw(pt, r, ui.RGB(0, 0, 0))
				})
			})
		}, w, h, 1)
		n := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if px := img.RGBAAt(x, y); px.R < 100 && px.G < 100 && px.B < 100 {
					n++
				}
			}
		}
		return n
	}

	// A character in a font every system the tests run on has, which
	// MathJax sets as text: 或 needs a CJK font, and the CI's Linux image
	// installs none.
	// A formula of nothing but that text: every pixel it draws is the
	// text's, so an icon, which has no text in its mask, draws none.
	text := renderMath(`\text{é}`, true)
	if text.err != "" || !text.hasText {
		t.Fatalf(`\text{é}: %+v`, text)
	}
	if n := dark(text, false); n == 0 {
		t.Error("the formula draws no pixels, so its text is a gap")
	}
	// The mask an icon draws from has no text in it: that is why such a
	// formula does not draw as one.
	if n := dark(text, true); n != 0 {
		t.Errorf("an icon draws %d pixels of the text, which it cannot", n)
	}

	plain := renderMath(`x^2`, true)
	if plain.err != "" || plain.hasText {
		t.Fatalf(`x^2: %+v`, plain)
	}
	if n := dark(plain, false); n == 0 {
		t.Error("the formula of no text draws no pixels")
	}
}

// TestMarkdownMathDocument: a whole document of formulas parses as it
// reads, its display formulas whole over the lines of = and - that are
// rows of them, and the headings, list and table below them as they are.
func TestMarkdownMathDocument(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "math-document.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := parseMarkdown(src, "", "", nil, &markdownCache{math: true})

	var formulas, headings, rules, tables int
	var rows int
	for _, b := range blocks {
		switch b.kind {
		case markdownMathBlock:
			formulas++
			if b.formula == nil || b.formula.err != "" {
				t.Errorf("display formula %d does not render: %+v", formulas, b.formula)
				continue
			}
			f := markdownRunsText(b.runs)
			for _, row := range []string{"a\n-\nb", "a\n---\nb"} {
				if strings.Contains(f, row) {
					rows++
				}
			}
		case markdownHeading:
			headings++
		case markdownRule:
			rules++
		case markdownTable, markdownTableHeader:
			tables++
		}
	}
	if formulas != 4 {
		t.Errorf("%d display formulas, want the document's 4", formulas)
	}
	// A row of its own is a line of - or ---: the formula keeps it.
	if rows != 2 {
		t.Errorf("%d formulas keep a row of - alone, want 2", rows)
	}
	// The document's 11 headings, the two below the formulas among them.
	if headings != 11 {
		t.Errorf("%d headings, want the document's 11", headings)
	}
	if rules != 0 {
		t.Errorf("%d rules, want none", rules)
	}
	if tables != 3 {
		t.Errorf("%d rows of the one table, want 3", tables)
	}
}

// TestLivePreviewMathFence: Live Preview keeps display formulas whole over
// lines of = and - alone, and renders a formula whose SVG holds text.
func TestLivePreviewMathFence(t *testing.T) {
	src := "$$\na\n=\nb\n$$\n\n$$\na\n-\nb\n$$\n\n$$\n\\text{é}\n$$\n"
	doc := liveParse(src)
	if len(doc.blocks) != 3 {
		t.Fatalf("%d live blocks, want 3: %+v", len(doc.blocks), doc.blocks)
	}
	for i, b := range doc.blocks {
		if !strings.HasPrefix(b.source, "$$\n") || !strings.HasSuffix(b.source, "\n$$") {
			t.Errorf("block %d source = %q", i, b.source)
		}
	}

	// A $$ no line closes is text, and a table below it is a table.
	table := "| a | b |\n| - | - |\n| 1 | 2 |"
	if live := liveParse("$$\n" + table + "\n"); len(live.blocks) != 1 || live.blocks[0].source != table {
		t.Errorf("an unclosed formula over a table: %+v", live.blocks)
	}
}
