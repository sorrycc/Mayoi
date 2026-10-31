package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// diagramOverlay is a block of Mermaid of a Markdown preview shown over
// the whole window, where it zooms and moves as a .mmd file's preview.
type diagramOverlay struct {
	// from is the preview the block is in, and line the block's line,
	// which find it again as the preview parses its text again.
	from *viewer
	line int
	// shown is the diagram of the block view shows, a copy of it, so
	// that its labels are shaped for the scale here, not the preview's.
	shown *mermaidDiagram
	view  *viewer
	// focused is set once the overlay took the keyboard.
	focused bool
}

// openDiagram shows a block of Mermaid of a preview over the window.
func (a *app) openDiagram(from *viewer, b markdownBlock) {
	o := &diagramOverlay{from: from, line: b.line, view: &viewer{loaded: true}}
	o.show(b.diagram, a.mermaidStyle())
	a.diagram = o
}

// show has the overlay show d, drawn in st, fitting it again when it is
// another size.
func (o *diagramOverlay) show(d *mermaidDiagram, st mermaidStyle) {
	dg := *d
	dg.shaped, dg.scale = nil, 0
	v := o.view
	if v.src != nil && (v.w != dg.w || v.h != dg.h) {
		v.zoom, v.panX, v.panY = 0, 0, 0
	}
	o.shown = d
	v.diagram, v.src, v.w, v.h, v.style = &dg, dg.svg, dg.w, dg.h, st
}

// diagramBlock is the block the overlay shows, as the preview has it now, nil
// once the preview or the block is gone or the block draws no diagram.
func (a *app) diagramBlock(o *diagramOverlay) *markdownBlock {
	m := o.from.markdown
	if m == nil || !a.viewerShown(o.from) {
		return nil
	}
	for i := range m.blocks {
		if b := &m.blocks[i]; b.line == o.line && b.kind == markdownCodeBlock {
			if b.diagram == nil || b.diagram.err != "" {
				return nil
			}
			return b
		}
	}
	return nil
}

// viewerShown reports whether a tab of the editor area has the viewer.
func (a *app) viewerShown(v *viewer) bool {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.view == v || t.ed != nil && t.ed.preview == v {
				return true
			}
		}
	}
	return false
}

func (a *app) diagramOverlayView(c *ui.Context) {
	o := a.diagram
	if o == nil {
		return
	}
	b := a.diagramBlock(o)
	if b == nil {
		a.diagram = nil
		return
	}
	if b.diagram != o.shown {
		// Drawn again, as the theme or the text changed.
		o.show(b.diagram, a.mermaidStyle())
	}
	p, v := a.pal, o.view
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "diagram")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Grow(1).AlignSelf(ui.Stretch).Margin(titleBarH+8, 24, 24, 24).Radius(10).Clip(), true).
				Role(ui.RoleDialog).Label(b.language)
			box.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6, 8, 6, 12).Border(1, p.border).BorderWidth(0, 0, 1, 0).Children(func() {
					ui.Text(c, b.language).FontSize(12).TextColor(p.muted).SingleLine().Grow(1)
					if a.smallButton(c, iconMinus, locales.CmdViewZoomOut()) {
						v.zoomBy(-1)
					}
					zoom := ui.ButtonBase(c.Key("diagram-zoom")).Height(20).MinWidth(44).Padding(0, 6).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
						Label(locales.FitToView()).Tooltip(locales.FitToView())
					col := p.muted
					if zoom.Hovered() {
						zoom.Background(p.hover)
						col = p.text
					}
					zoom.Children(func() { ui.Text(c, v.zoomPercent()).FontSize(12).TextColor(col).SingleLine() })
					if zoom.Clicked() {
						v.zoomBy(0)
					}
					if a.smallButton(c, iconPlus, locales.CmdViewZoomIn()) {
						v.zoomBy(1)
					}
					if a.smallButton(c, iconCopy, locales.CopyCode()) {
						a.services.WriteClipboard(markdownRunsText(b.runs))
						a.tell(note{message: locales.CopiedCode()})
					}
					if a.smallButton(c, iconX, locales.CloseDiagram()) {
						a.diagram = nil
					}
				})
				ui.Column(c).Grow(1).MinHeight(0).Children(func() {
					a.viewerView(c, v, b.language)
				})
			})
			if !o.focused {
				o.focused = true
				v.pane.Focus()
			}
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.diagram = nil
			}
		})
	})
}
