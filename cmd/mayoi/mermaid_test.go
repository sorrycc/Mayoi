package main

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func testMermaidStyle() mermaidStyle {
	p := darkPalette()
	return mermaidStyle{
		bg: p.editor, fg: p.text, muted: p.muted, line: p.muted, surface: p.surface, surfaceAlt: p.surfaceHover,
		border: p.border, accent: p.accent, danger: p.danger, mono: "monospace", monoWeight: 400,
	}
}

func TestRenderMermaid(t *testing.T) {
	st := testMermaidStyle()
	for name, src := range map[string]string{
		"flowchart": "flowchart LR\nA[Start] --> B{Ok?}\nB -- yes --> C[Done]\nB -- no --> A",
		"sequence":  "sequenceDiagram\nAlice->>Bob: Hello\nBob-->>Alice: Hi",
		"class":     "classDiagram\nclass Animal{\n+String name\n}\nAnimal <|-- Duck",
		"gantt":     "gantt\ntitle Plan\ndateFormat YYYY-MM-DD\nsection A\nTask :a1, 2024-01-01, 30d",
		"pie":       "pie title Pets\n\"Dogs\" : 386\n\"Cats\" : 85",
		"git":       "gitGraph\ncommit\nbranch dev\ncommit\ncheckout main\nmerge dev",
	} {
		d := renderMermaid(src, st)
		if d.err != "" {
			t.Errorf("%s: %s", name, d.err)
			continue
		}
		if d.svg == nil || d.w <= 0 || d.h <= 0 || len(d.labels) == 0 {
			t.Errorf("%s: svg %v %vx%v, %d labels", name, d.svg != nil, d.w, d.h, len(d.labels))
		}
		for _, l := range d.labels {
			if l.x < -d.w || l.x > 2*d.w || l.y < -d.h || l.y > 2*d.h {
				t.Errorf("%s: a label at %v,%v outside %vx%v", name, l.x, l.y, d.w, d.h)
			}
		}
	}
}

func TestRenderMermaidError(t *testing.T) {
	d := renderMermaid("sequenceDiagram\nA->>B hi", testMermaidStyle())
	if d.err == "" || strings.Contains(d.err, "\n") || !strings.Contains(d.err, "line 2") {
		t.Errorf("err = %q", d.err)
	}
	if d := renderMermaid("not a diagram", testMermaidStyle()); d.err == "" {
		t.Error("no error for text that is no diagram")
	}
}

func TestMermaidSVG(t *testing.T) {
	st := testMermaidStyle()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50" width="100" height="50" style="--_bg:var(--pele-bg,#fff);--_s:var(--pele-surface,#f3f3f3);--_a:var(--pele-nope,#808080);--_r:var(--pele-radius,4px);font:16px sans-serif" fill="var(--_fg,#123456)">` +
		`<g transform="translate(10,20)"><rect x="0" y="0" width="10" height="10" fill="var(--_s)" style="rx:var(--_r)"/>` +
		`<rect fill="color-mix(in srgb,var(--_a) 50%,#000000)"/>` +
		`<text x="5" y="6" text-anchor="middle">A &amp; B</text>` +
		`<text text-anchor="middle" xml:space="preserve"><tspan x="0" y="-4" font-weight="var(--_w)">one</tspan><tspan font-family="var(--_fm,monospace)"> two</tspan><tspan x="0" y="16" font-size="12">three</tspan></text></g>` +
		`<g transform="rotate(90 0 0)"><text x="1" y="2">turned</text></g></svg>`
	out, texts, err := mermaidSVG(svg, st)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`fill="` + hexColor(st.surface) + `"`, ` rx="4"`, `fill="#404040"`, `fill="#123456"`} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in %s", want, s)
		}
	}
	for _, gone := range []string{"<text", "tspan", "var(", "--_"} {
		if strings.Contains(s, gone) {
			t.Errorf("%s left in %s", gone, s)
		}
	}
	if _, err := ui.ParseSVG(out); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 4 {
		t.Fatalf("%d chunks: %+v", len(texts), texts)
	}
	if r := texts[0].runs; len(r) != 1 || r[0].text != "A & B" || r[0].size != 16 || texts[0].anchor != "middle" {
		t.Errorf("first chunk %+v", texts[0])
	}
	if x, y := texts[0].m.apply(texts[0].x, texts[0].y); x != 15 || y != 26 {
		t.Errorf("first chunk at %v,%v", x, y)
	}
	if r := texts[1].runs; len(r) != 2 || r[0].style != pelebold || r[1].style != peleMono || r[1].text != " two" || texts[1].y != -4 {
		t.Errorf("second chunk %+v", texts[1])
	}
	if r := texts[2].runs; len(r) != 1 || r[0].size != 12 || texts[2].y != 16 {
		t.Errorf("third chunk %+v", texts[2])
	}
	if l := texts[3].shape(st); len(l) != 1 || l[0].angle != 90 {
		t.Errorf("turned %+v", l)
	}
}

func TestCSSResolve(t *testing.T) {
	vars := map[string]string{"--a": "var(--b)", "--b": "#112233"}
	for in, want := range map[string]string{
		"var(--a)":                               "#112233",
		"var(--x,#fff)":                          "#fff",
		"var(--x, var(--a))":                     "#112233",
		"1px solid var(--b)":                     "1px solid #112233",
		"color-mix(in srgb,#ffffff 25%,#000000)": "#404040",
		"none":                                   "none",
	} {
		if got := cssResolve(in, vars); got != want {
			t.Errorf("cssResolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTransform(t *testing.T) {
	m := parseTransform("translate(10, 5) scale(2)")
	if x, y := m.apply(1, 1); x != 12 || y != 7 {
		t.Errorf("translate scale: %v,%v", x, y)
	}
	m = parseTransform("rotate(90 10 10)")
	if x, y := m.apply(20, 10); math.Abs(float64(x-10)) > 1e-4 || math.Abs(float64(y-20)) > 1e-4 {
		t.Errorf("rotate: %v,%v", x, y)
	}
}

const mermaidMarkdown = "# Diagrams\n\n```mermaid\nflowchart LR\n  A[Write **Markdown**] --> B{Has a diagram?}\n  B -- yes --> C[Draw it]\n  B -- no --> D[Show text]\n  C --> E((Done))\n```\n\n```mermaid\nsequenceDiagram\n  participant E as Editor\n  participant P as Pele\n  E->>P: render(source)\n  P-->>E: SVG\n  Note over E,P: labels drawn by Mayoi\n```\n\n```mermaid\nsequenceDiagram\n  A->>B hi\n```\n"

// TestMermaidPreview: a Markdown preview draws its blocks of Mermaid as
// diagrams, says why one cannot be drawn, shows one's source on asking,
// and shows them all as source with previewMermaid off.
func TestMermaidPreview(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	path := hs.path("diagrams.md")
	os.WriteFile(path, []byte(mermaidMarkdown), 0o644)
	a.openFile(path, true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	hs.until("diagrams", func() bool { return d.preview != nil && d.preview.loaded })
	hs.settle()
	m := d.preview.markdown
	var diagrams []markdownBlock
	for _, b := range m.blocks {
		if b.diagram != nil {
			diagrams = append(diagrams, b)
		}
	}
	if len(diagrams) != 3 || diagrams[0].diagram.err != "" || diagrams[1].diagram.err != "" || diagrams[2].diagram.err == "" {
		t.Fatalf("diagrams %+v", diagrams)
	}
	hs.shot("mermaid-markdown")
	if !hs.tt.HasText(locales.MermaidCannotDraw(diagrams[2].diagram.err)) {
		t.Error("no error under the diagram that cannot be drawn")
	}
	if hs.tt.HasText("flowchart LR") {
		t.Error("the source of a diagram shows")
	}
	m.sources = map[int]bool{diagrams[0].line: true}
	hs.settle()
	if !hs.tt.HasText("flowchart LR") {
		t.Error("the source asked for does not show")
	}

	a.settings.PreviewMermaid = false
	hs.until("source", func() bool {
		for _, b := range d.preview.markdown.blocks {
			if b.diagram != nil {
				return false
			}
		}
		return true
	})
}

// TestMermaidFilePreview: a .mmd file previews as its diagram, drawn
// again as it is edited.
func TestMermaidFilePreview(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	path := hs.path("flow.mmd")
	os.WriteFile(path, []byte("stateDiagram-v2\n  [*] --> Idle\n  Idle --> Busy: start\n  Busy --> Idle: done\n  Busy --> [*]\n"), 0o644)
	a.openFile(path, true)
	a.run("view.togglePreview")
	d := a.activeEditor()
	hs.until("diagram", func() bool { return d.preview != nil && d.preview.diagram != nil })
	hs.settle()
	hs.shot("mermaid-file")
	first := d.preview.diagram
	d.buf.Replace(d.buf.Len(), d.buf.Len(), "  Idle --> Off: stop\n")
	hs.until("drawn again", func() bool { return d.preview.diagram != first })
	if d.preview.err != "" {
		t.Fatal(d.preview.err)
	}
}
