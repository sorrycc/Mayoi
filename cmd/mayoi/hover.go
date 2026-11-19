package main

import (
	"context"
	"strings"
	"time"

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
	// focused has the arrows and the page keys scroll the card, as Show
	// Hover asked again while it shows does, until another key.
	focused bool
	scroll  ui.ScrollState
	// auto is a card the pointer asked for, resting on a name: it goes
	// as the pointer leaves the name and the card.
	auto bool
}

// newHoverCard is the card of Markdown text for rune at of d, the caret
// or the pointer's, of the server's range span or none.
func newHoverCard(d *editor, text string, span *lsp.Range, at int) *hoverCard {
	h := &hoverCard{tree: &viewNode{Type: "markdown", Text: text}, ed: d, at: at, version: d.buf.Version(), sel: d.sel}
	snap := d.buf.Snapshot()
	if span != nil {
		start := posRune(snap, fromLSP(span.Start))
		if at := min(at, snap.Len()); start <= at && snap.LineAt(start) == snap.LineAt(at) {
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

// staleHover reports whether the card no longer goes: its caret or text
// changed, or its editor is not the active one; the pointer's, that the
// pointer rests in another.
func (a *app) staleHover(h *hoverCard) bool {
	if h.ed.buf.Version() != h.version || h.ed.sel != h.sel {
		return true
	}
	if h.auto {
		return a.rest.ed != h.ed
	}
	return a.activeEditor() != h.ed
}

// pointerHover is the name the pointer rests on in an editor, whose card
// is asked for once it rested there hoverDwell, by the frames' clock.
type pointerHover struct {
	ed *editor
	// word is the runes of the name, at the rune the pointer came to it
	// at, and version and scroll the text and the editor's offsets it
	// was found in.
	word    [2]int
	at      int
	version uint64
	scroll  [2]float32
	since   time.Time
	asked   bool
	// ctx is the question asked, and cancel stops it.
	ctx    context.Context
	cancel context.CancelFunc
	// seen is the build that last found the pointer on the name or its
	// card: one that did not drops them, as an editor no longer shown.
	seen int
	restKept
}

// restKept is what dropping the name the pointer rested on keeps.
type restKept struct {
	// hushed is that a key, a press, Escape, a scroll or an edit went on
	// from the name the pointer is on: no name is asked of until the
	// pointer moves.
	hushed bool
	// probe is the pointer the name was last found for, kept as the
	// pointer stays still, and found what was found there.
	probe restProbe
	found restFound
}

// restProbe is what the name under the pointer depends on.
type restProbe struct {
	ed                 *editor
	x, y, sx, sy, w, h float32
	version            uint64
}

type restFound struct {
	word [2]int
	at   int
	ok   bool
}

// dropRest forgets the name the pointer rested on: its question, asked
// or to come, and its card.
func (a *app) dropRest() {
	r := &a.rest
	if r.cancel != nil {
		r.cancel()
	}
	if h := a.hover; h != nil && h.auto {
		a.hover = nil
	}
	*r = pointerHover{restKept: r.restKept}
}

// hushRest drops the name the pointer rested on, and asks of none until
// the pointer moves.
func (a *app) hushRest() {
	a.dropRest()
	a.rest.hushed = true
}

// restHover follows the pointer over the text of d as its frame is built:
// the name it rests on hoverDwell asks for its card, quietly, which stays
// while the pointer is over the name or the card, and goes as it leaves
// them. A press, a drag, a key held, the editor scrolling or its text
// changing goes on from the name, until the pointer moves again.
func (a *app) restHover(c *ui.Context, d *editor, area ui.Element, g geometry, snap ui.TextSnapshot, live bool) {
	r := &a.rest
	px, py, _ := area.PointerPosition()
	view := d.lastView
	if h := a.hover; h != nil && h.auto && h.ed == d && r.ed == d {
		// Into the card, to read or scroll it, over the gap to it.
		if at, ok := a.hoverAnchor(h); ok && h.holds(view.X+px-d.hscroll.X, view.Y+py, at) {
			r.seen = a.builds
			return
		}
	}
	if !area.Hovered() || !a.prefs().EditorHover || live || a.hover != nil && !a.hover.auto {
		if r.ed == d {
			a.dropRest()
		}
		return
	}
	if d.pressing || d.selecting || c.Modifiers() != 0 {
		a.hushRest()
		return
	}
	scroll := [2]float32{d.hscroll.X, d.vscroll.Y}
	if r.ed == d && (r.version != snap.Version() || r.scroll != scroll) {
		a.hushRest()
		return
	}
	var found restFound
	if !r.hushed && px-d.hscroll.X >= d.textX && py >= float32(len(d.sticky))*a.font.lineH {
		probe := restProbe{d, px, py, d.hscroll.X, d.vscroll.Y, view.W, g.height(), snap.Version()}
		if r.probe != probe {
			r.probe = probe
			r.found.word, r.found.at, r.found.ok = a.pointedWord(g, snap, px-d.textX, py+d.vscroll.Y-editorPadY)
		}
		found = r.found
	}
	if !found.ok {
		if r.ed == d {
			a.dropRest()
		}
		return
	}
	if r.ed == d && r.word == found.word {
		r.seen = a.builds
		if !r.asked {
			if wait := hoverDwell - c.Now().Sub(r.since); wait > 0 {
				c.After(wait)
			} else {
				r.asked = true
				a.askHover(d, r.at, r)
			}
		}
		return
	}
	a.dropRest()
	r.ed, r.word, r.at, r.version, r.scroll, r.since, r.seen = d, found.word, found.at, snap.Version(), scroll, c.Now(), a.builds
	c.After(hoverDwell)
}

// holds reports whether the window's point (x, y) is on the card or in
// the gap between it and its anchor at, which the pointer crosses to it.
func (h *hoverCard) holds(x, y float32, at ui.Rect) bool {
	r := h.rect
	if r.W <= 0 || x < r.X || x > r.X+r.W {
		return false
	}
	return y >= min(r.Y, at.Y+at.H) && y <= max(r.Y+r.H, at.Y)
}

// pointedWord returns the name of word runes at (x, y) in an editor's
// text, x from the text's left and y from its first line's top as g
// places the lines, and the rune there: none over the space past a line's
// end, at a fold's mark, or on a rune not of a name.
func (a *app) pointedWord(g geometry, snap ui.TextSnapshot, x, y float32) (word [2]int, at int, ok bool) {
	if x < 0 || y < 0 || g.n == 0 {
		return
	}
	line := g.at(y)
	if g.hidden(line) || y < g.top(line) || y >= g.top(line+1) {
		return
	}
	s, e := snap.LineRange(line)
	text := strings.TrimRight(snap.Slice(s, e), "\r\n")
	runes := []rune(text)
	i := -1
	if l := g.laid(); l != nil {
		i = laidRuneAt(l, line, s, len(runes), x, y)
	} else {
		i = a.font.runeAt(text, len(runes), x)
	}
	if i < 0 || !isWordRune(runes[i]) {
		return
	}
	from, to := i, i+1
	for from > 0 && isWordRune(runes[from-1]) {
		from--
	}
	for to < len(runes) && isWordRune(runes[to]) {
		to++
	}
	return [2]int{s + from, s + to}, s + i, true
}

// laidRuneAt is the rune of line i, from rune s of the text and n runes
// long, whose glyph is at (x, y) where l laid the line out, -1 for none.
func laidRuneAt(l *ui.TextLines, i, s, n int, x, y float32) int {
	if n == 0 {
		return -1
	}
	rows := l.Rows(i)
	k := 0
	for k+1 < len(rows) {
		if _, ry, _, ok := l.Caret(s + rows[k+1]); !ok || y < ry {
			break
		}
		k++
	}
	from, to := rows[k], n
	if k+1 < len(rows) {
		to = rows[k+1]
	}
	if from >= to {
		return -1
	}
	if x0, _, _, ok := l.Caret(s + from); !ok || x < x0 {
		return -1
	}
	// The last rune of the row that starts at or before x.
	lo, hi := from, to-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if mx, _, _, _ := l.Caret(s + mid); mx <= x {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	// Past the line's end; a row the line wraps at ends where it goes on.
	if lo+1 < to || k+1 == len(rows) {
		if rx, _, _, _ := l.Caret(s + lo + 1); x >= rx {
			return -1
		}
	}
	return lo
}

// hoverKey scrolls a focused card of d by a key: a line with the arrows,
// a page with Page Up and Page Down, to its ends with Home and End. Any
// other key leaves it to the editor, which types or moves the caret and
// so closes the card.
func (a *app) hoverKey(d *editor, ev ui.InputEvent) bool {
	h := a.hover
	if h == nil || h.ed != d || !h.focused || ev.Mods != 0 {
		return false
	}
	line := a.font.lineH
	page := max(h.rect.H-line, line)
	var dy float32
	switch ev.Key {
	case ui.KeyUp:
		dy = -line
	case ui.KeyDown:
		dy = line
	case ui.KeyPageUp:
		dy = -page
	case ui.KeyPageDown:
		dy = page
	case ui.KeyHome:
		dy = -h.scroll.MaxY
	case ui.KeyEnd:
		dy = h.scroll.MaxY
	default:
		h.focused = false
		return false
	}
	// With the keys, at once, as a list's choice moves.
	h.scroll.Y = max(0, min(h.scroll.Y+dy, h.scroll.MaxY))
	a.invalidate()
	return true
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
	if a.rest.ed != nil && a.rest.seen != a.builds {
		// Its editor no longer shows, or looked at the pointer no more.
		a.dropRest()
	}
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
		if h.focused {
			// Its keys scroll it: the accent says so, its border as wide.
			box.BorderColor(p.accent)
		}
		if above {
			box.Bottom(winH - edge)
		} else {
			box.Top(edge)
		}
		box.Children(func() {
			ui.Scroll(c.Key("hover-scroll")).TrackScroll(&h.scroll).Children(func() {
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
			if h.auto {
				a.hushRest()
			}
			a.hover = nil
		}
	})
}
