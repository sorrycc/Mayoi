package main

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// mermaidExportPad is the room around a diagram copied or exported, in
// DIPs, in the color of its background.
const mermaidExportPad = 16

// mermaidExportScale is the pixels a DIP of a diagram's PNG, and
// mermaidExportSide the longest side the PNG may have, past which it is
// drawn at a smaller scale.
const (
	mermaidExportScale = 2
	mermaidExportSide  = 8192
)

// png draws the diagram as a PNG, as the window shows it, on the
// background of the theme it was drawn in.
func (d *mermaidDiagram) png() ([]byte, error) {
	if d.svg == nil || d.w <= 0 || d.h <= 0 {
		return nil, fmt.Errorf("%s", locales.ErrSVGNoSize())
	}
	// A copy, so that its labels are shaped for the scale here.
	dg := *d
	dg.shaped, dg.scale = nil, 0
	w, h := int(math.Ceil(float64(d.w+2*mermaidExportPad))), int(math.Ceil(float64(d.h+2*mermaidExportPad)))
	scale := min(float32(mermaidExportScale), mermaidExportSide/float32(max(w, h)))
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Background(d.style.bg).Draw(func(pt *ui.Painter, r ui.Rect) {
			dg.draw(pt, ui.Rect{X: r.X + mermaidExportPad, Y: r.Y + mermaidExportPad, W: d.w, H: d.h})
		})
	}, w, h, scale)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// exportSVG is the diagram as an SVG that draws it whole, its labels as
// text where the window draws them, on the background of its theme.
func (d *mermaidDiagram) exportSVG() []byte {
	var b bytes.Buffer
	w, h := d.w+2*mermaidExportPad, d.h+2*mermaidExportPad
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g">`, w, h, w, h)
	fmt.Fprintf(&b, `<rect width="%g" height="%g" fill="%s"/>`, w, h, hexColor(d.style.bg))
	// The diagram's own SVG, placed in the room around it.
	shapes := d.shapes
	open := bytes.Index(shapes, []byte("<svg"))
	last := bytes.LastIndex(shapes, []byte("</svg>"))
	inner := open >= 0 && bytes.IndexByte(shapes[open:], '>') >= 0 && last > open
	if inner {
		end := open + bytes.IndexByte(shapes[open:], '>')
		root := svgSizeAttr.ReplaceAllString(string(shapes[open:end]), "")
		root = svgStyleAttr.ReplaceAllString(root, "")
		fmt.Fprintf(&b, `%s x="%d" y="%d" width="%g" height="%g" viewBox="0 0 %g %g" overflow="visible">`,
			root, mermaidExportPad, mermaidExportPad, d.w, d.h, d.w, d.h)
		b.Write(shapes[end+1 : last])
	} else {
		fmt.Fprintf(&b, `<g transform="translate(%d %d)">`, mermaidExportPad, mermaidExportPad)
	}
	for _, l := range d.labels {
		lay := ui.ShapeText(l.text, l.font, 0)
		_, lh := lay.Size()
		m := l.font.Metrics()
		base := (lh-m.Ascent-m.Descent)/2 + m.Ascent
		fmt.Fprintf(&b, `<text x="%g" y="%g" xml:space="preserve" font-family="%s" font-size="%g" fill="%s"`,
			l.x, l.y+base, xmlEscape(svgFontFamily(l.font.Family)), l.font.Size, hexColor(l.color))
		if l.font.Weight >= 600 {
			b.WriteString(` font-weight="bold"`)
		} else if l.font.Weight != 0 && l.font.Weight != 400 {
			fmt.Fprintf(&b, ` font-weight="%d"`, l.font.Weight)
		}
		if l.font.Italic {
			b.WriteString(` font-style="italic"`)
		}
		if l.angle != 0 {
			fmt.Fprintf(&b, ` transform="rotate(%g %g %g)"`, l.angle, l.x, l.y)
		}
		b.WriteByte('>')
		xmlEscapeTo(&b, l.text)
		b.WriteString("</text>")
	}
	if inner {
		b.WriteString("</svg>")
	} else {
		b.WriteString("</g>")
	}
	b.WriteString("</svg>\n")
	return b.Bytes()
}

var svgStyleAttr = regexp.MustCompile(`\sstyle="[^"]*"`)

// svgFontFamily is a label's font as SVG's font-family, the window's own
// for "", with the generic family other apps fall back to.
func svgFontFamily(family string) string {
	if family == "" {
		return `system-ui, -apple-system, "Segoe UI", sans-serif`
	}
	generic := "sans-serif"
	if strings.Contains(strings.ToLower(family), "mono") {
		generic = "monospace"
	}
	return `"` + strings.ReplaceAll(family, `"`, "") + `", ` + generic
}

// copyDiagramImage puts the diagram on the clipboard as a PNG.
func (a *app) copyDiagramImage(d *mermaidDiagram) {
	img, err := d.png()
	if err == nil {
		err = a.host.copyImage(img)
	}
	if err != nil {
		a.fail("%s", locales.ErrCopyDiagram(err.Error()))
		return
	}
	a.tell(note{message: locales.CopiedDiagramImage()})
}

// exportDiagram saves the diagram as a PNG or an SVG, as ext says, where
// the save dialog says, next to doc when it is a local file.
func (a *app) exportDiagram(d *mermaidDiagram, doc *document, n int, ext string) {
	name := "diagram"
	dir := a.root
	if doc != nil && doc.path != "" {
		base := filepath.Base(doc.path)
		name = strings.TrimSuffix(base, filepath.Ext(base))
		if n > 0 {
			name = fmt.Sprintf("%s-diagram-%d", name, n)
		}
		dir = filepath.Dir(doc.path)
	}
	if remote.Is(dir) {
		dir = ""
	}
	a.host.exportAs(filepath.Join(dir, name+"."+ext), ext, func(path string) {
		var data []byte
		var err error
		if ext == "svg" {
			data = d.exportSVG()
		} else {
			data, err = d.png()
		}
		if err == nil {
			err = os.WriteFile(path, data, 0o644)
		}
		if err != nil {
			a.fail("%s", locales.ErrExportDiagram(err.Error()))
			return
		}
		a.tell(note{message: locales.ExportedDiagram(filepath.Base(path))})
	})
}

// diagramExportMenu is the menu of a diagram's export button.
func (a *app) diagramExportMenu(d *mermaidDiagram, doc *document, n int) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item(locales.ExportPNG()).Chosen() {
			a.post(func() { a.exportDiagram(d, doc, n, "png") })
		}
		if m.Item(locales.ExportSVG()).Chosen() {
			a.post(func() { a.exportDiagram(d, doc, n, "svg") })
		}
	}
}

// diagramButtons are the buttons that copy a diagram as an image and
// export it, in a row of small buttons.
func (a *app) diagramButtons(c *ui.Context, d *mermaidDiagram, doc *document, n int) {
	if a.smallButton(c, iconImage, locales.CopyAsImage()) {
		a.copyDiagramImage(d)
	}
	p := a.pal
	label := locales.ExportDiagram()
	b := ui.ButtonBase(c.Key(label)).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(revealFade)
	pressPose(b)
	b.Children(func() { ui.Icon(c, iconDownload).FontSize(13).TextColor(col) })
	b.Menu(a.diagramExportMenu(d, doc, n))
}

// activeDiagram is the diagram the palette's commands of diagrams act
// on: the one opened over the window, or the active Mermaid preview's,
// with its document and its number among its document's diagrams.
func (a *app) activeDiagram() (*mermaidDiagram, *document, int) {
	if o := a.diagram; o != nil {
		if b := a.diagramBlock(o); b != nil {
			return b.diagram, o.from.doc, a.diagramNumber(o.from, b.line)
		}
	}
	if v := a.activeViewer(); v != nil && v.markdown == nil && v.diagram != nil && v.err == "" {
		return v.diagram, v.doc, 0
	}
	return nil, nil, 0
}

// diagramNumber is which of a Markdown preview's diagrams the block at
// line is, from 1.
func (a *app) diagramNumber(v *viewer, line int) int {
	n := 0
	if m := v.markdown; m != nil {
		for _, b := range m.blocks {
			if b.diagram != nil {
				n++
			}
			if b.line == line {
				return max(n, 1)
			}
		}
	}
	return max(n, 1)
}

// copyActiveDiagram and exportActiveDiagram are the palette's commands
// of the diagram shown.
func (a *app) copyActiveDiagram() {
	if d, _, _ := a.activeDiagram(); d != nil {
		a.copyDiagramImage(d)
		return
	}
	a.tell(note{message: locales.NoDiagramShown()})
}

func (a *app) exportActiveDiagram(ext string) {
	if d, doc, n := a.activeDiagram(); d != nil {
		a.exportDiagram(d, doc, n, ext)
		return
	}
	a.tell(note{message: locales.NoDiagramShown()})
}
