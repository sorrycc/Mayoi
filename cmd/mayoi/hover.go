package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/internal/lsp"
	"mayoi/locales"
)

// How a hover card sits by what it is of: no wider or higher than these,
// gap from its line and margin from its editor's edges.
const (
	hoverMaxW   = 520
	hoverMaxH   = 320
	hoverGap    = 4
	hoverMargin = 8
	// hoverAbove is the room above its line a card goes above in, even
	// with more below: it reads above the line, which it leaves in sight.
	hoverAbove = 160
)

// hoverCard is what Show Hover shows: Markdown of the language server and
// the plugins, by what it is of in an editor, while its caret and text stay.
type hoverCard struct {
	tree *viewNode
	// sig is the code it starts with, as a signature, and docs the rest.
	sig, docs []markdownBlock
	ed        *editor
	// at is the rune it is of: where the server's range starts on the
	// caret's line, else the caret.
	at      int
	version uint64
	sel     [2]int
	// x is where at stands across its line, measured once, for an editor
	// that does not wrap.
	x      float32
	xKnown bool
	rect   ui.Rect // where the card laid out last, empty before
}

// newHoverCard is the card of Markdown text for the caret of d, of the
// server's range span or none.
func newHoverCard(d *editor, text string, span *lsp.Range) *hoverCard {
	h := &hoverCard{tree: &viewNode{Type: "markdown", Text: text}, ed: d, at: d.sel[1], version: d.buf.Version(), sel: d.sel}
	snap := d.buf.Snapshot()
	if span != nil {
		start := posRune(snap, fromLSP(span.Start))
		if caret := min(d.sel[1], snap.Len()); start <= caret && snap.LineAt(start) == snap.LineAt(caret) {
			h.at = start
		}
	}
	blocks := parseMarkdown([]byte(text), "", "", nil)
	n := 0
	for n < len(blocks) && blocks[n].kind == markdownCodeBlock {
		n++
	}
	h.sig, h.docs = blocks[:n], blocks[n:]
	if n > 0 && len(h.docs) > 0 && h.docs[0].kind == markdownRule {
		h.docs = h.docs[1:] // the signature's own line parts them
	}
	return h
}

// staleHover reports whether the card no longer goes: its editor is not the
// active one, or its caret or text changed.
func (a *app) staleHover(h *hoverCard) bool {
	return a.activeEditor() != h.ed || h.ed.buf.Version() != h.version || h.ed.sel != h.sel
}

// hoverAnchor is where the rune h is of shows in the window, one pixel
// wide and its line high, and whether it shows: not folded, nor scrolled
// out of the editor's view.
func (a *app) hoverAnchor(h *hoverCard) (ui.Rect, bool) {
	d := h.ed
	view := d.lastView
	if view.H <= 0 || view.W <= 0 {
		return ui.Rect{}, false
	}
	snap := d.buf.Snapshot()
	at := min(h.at, snap.Len())
	line := snap.LineAt(at)
	g := a.geometry(d, snap.LineCount())
	if g.hidden(line) {
		return ui.Rect{}, false
	}
	x, y, lineH := float32(0), g.top(line), a.font.lineH
	if g.laid() != nil {
		cx, cy, ch, ok := d.lines.Caret(at)
		if !ok {
			return ui.Rect{}, false
		}
		x, y, lineH = cx, cy, ch
	} else {
		if !h.xKnown {
			s, _ := snap.LineRange(line)
			h.x, h.xKnown = a.font.width(snap.Slice(s, at)), true
		}
		x = h.x
	}
	r := ui.Rect{X: view.X + d.textX + x - d.hscroll.X, Y: view.Y + editorPadY + y - d.vscroll.Y, W: 1, H: lineH}
	if r.Y+r.H <= view.Y || r.Y >= view.Y+view.H || r.X < view.X || r.X > view.X+view.W {
		return r, false
	}
	return r, true
}

// hoverPlace is where a card goes for an anchor at, in an editor whose
// view is view in a window winW wide: its left, its width at most, its
// edge by the line (its bottom when above, its top when below), and the
// most it may be high. It goes above while the room there takes a card
// of hoverAbove or is more than below, and stays in the editor, in the
// window when the editor is narrower than a card can be.
func hoverPlace(at, view ui.Rect, winW, cardW float32) (x, w, edge, maxH float32, above bool) {
	w = min(hoverMaxW, max(view.W-2*hoverMargin, min(240, winW-2*hoverMargin)))
	if cardW <= 0 || cardW > w {
		cardW = w
	}
	left, right := view.X+hoverMargin, view.X+view.W-hoverMargin
	if right-left < cardW {
		left, right = hoverMargin, winW-hoverMargin
	}
	x = max(left, min(at.X, right-cardW))
	up := at.Y - hoverGap - (view.Y + hoverMargin)
	down := view.Y + view.H - hoverMargin - (at.Y + at.H + hoverGap)
	if above = up >= hoverAbove || up >= down; above {
		return x, w, at.Y - hoverGap, max(0, min(hoverMaxH, up)), true
	}
	return x, w, at.Y + at.H + hoverGap, max(0, min(hoverMaxH, down)), false
}

// hoverView draws the hover card while it goes, by what it is of.
func (a *app) hoverView(c *ui.Context) {
	h := a.hover
	if h == nil {
		return
	}
	if a.staleHover(h) {
		a.hover = nil
		return
	}
	at, shows := a.hoverAnchor(h)
	if !shows {
		return // back as the line scrolls into view
	}
	p, f := a.pal, a.font
	winW, winH := c.Size()
	x, w, edge, maxH, above := hoverPlace(at, h.ed.lastView, winW, h.rect.W)
	ui.Overlay(c, func() {
		box := a.floating(c, ui.Column(c.Key("hover-card")).Absolute().Left(x).MaxWidth(w).MaxHeight(maxH).Radius(8).Clip(), false).
			Transition(popIn).Role(ui.RoleTooltip).Label(locales.CmdGoHover())
		if above {
			box.Bottom(winH - edge)
		} else {
			box.Top(edge)
		}
		box.Children(func() {
			ui.Scroll(c.Key("hover-scroll")).Children(func() {
				if len(h.sig) > 0 {
					col := ui.Column(c.Key("hover-sig")).Gap(4).Padding(8, 12).Background(p.editor)
					if len(h.docs) > 0 {
						col.BorderWidth(0, 0, 1, 0).BorderColor(p.border)
					}
					col.Children(func() {
						for k, b := range h.sig {
							var spans []ui.Span
							for _, r := range b.runs {
								spans = append(spans, a.markdownSpan(r, p.text, f.size, true))
							}
							ui.RichText(c.Key(k), spans...).FontSize(f.size).Selectable()
						}
					})
				}
				if len(h.docs) > 0 {
					ui.Column(c.Key("hover-docs")).Padding(8, 12).Children(func() {
						a.markdownBlocksView(c, "hover-docs", h.docs, a.settings.FontSize)
					})
				}
			})
		})
		if h.rect.W == 0 && x < at.X {
			// Kept from the edge by a width it may not have: shown the
			// frame after, where its own puts it.
			box.Opacity(0)
			a.invalidate()
		}
		if b := box.Bounds(); b.W > 0 {
			h.rect = b
		}
		if box.PressedOutside() || box.OverlayShortcut(0, ui.KeyEscape) || box.Shortcut(0, ui.KeyEscape) {
			a.hover = nil
		}
	})
}
