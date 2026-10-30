package main

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
	"mayoi/internal/settings"
	"mayoi/locales"
)

const (
	tabWidth   = 4
	editorPadY = 8  // above the first line
	gutterGap  = 10 // between the line numbers and the text
	textPadR   = 32 // right of the widest line
)

// codeFont is the editor's font and the metrics of its monospaced cells.
type codeFont struct {
	family string
	size   float32
	weight int
	// features are the font's OpenType features, comma separated.
	features string
	// lineHeight is the height of a line as the settings have it, a
	// multiple of size, and lineH the height of a line.
	lineHeight float32
	lineH      float32
	charW      float32
	// tabW is how far apart the platform's text layout puts its tab stops,
	// which is not a number of cells: a tab ends at the next stop.
	tabW float32
}

func newCodeFont(p settings.Prefs) codeFont {
	f := codeFont{
		family: monoFallback(p.FontFamily), size: p.FontSize, weight: p.FontWeight, features: strings.Join(p.FontFeatures, ","),
		lineHeight: p.LineHeight, lineH: float32(int(p.FontSize*p.LineHeight + 0.5)),
	}
	w, _ := ui.ShapeText(strings.Repeat("0", 64), f.font(), 0).Size()
	f.charW = w / 64
	if f.tabW, _ = ui.ShapeText("\t", f.font(), 0).Size(); f.tabW <= 0 {
		f.tabW = tabWidth * f.charW
	}
	return f
}

// is reports whether the font is the one the preferences choose.
func (f codeFont) is(p settings.Prefs) bool {
	return f.charW != 0 && f.family == monoFallback(p.FontFamily) && f.size == p.FontSize && f.weight == p.FontWeight &&
		f.features == strings.Join(p.FontFeatures, ",") && f.lineHeight == p.LineHeight
}

// monoFallback is a font family list that ends in the system's monospaced
// font, so that a family the system lacks, as a font named in the settings
// but not installed, falls back to a monospaced font rather than to the
// system's proportional one. A list that names a generic family already
// is kept.
func monoFallback(family string) string {
	for _, f := range strings.Split(family, ",") {
		switch strings.ToLower(strings.Trim(strings.TrimSpace(f), `"'`)) {
		case "monospace", "ui-monospace", "system-ui", "sans-serif", "serif", "ui-serif", "ui-sans-serif":
			return family
		}
	}
	if strings.TrimSpace(family) == "" {
		return "monospace"
	}
	return family + ", monospace"
}

func (f codeFont) font() ui.Font {
	return ui.Font{Family: f.family, Size: f.size, Weight: f.weight, Features: f.features}
}

// span is a run of code in the font.
func (f codeFont) span(text string, col ui.Color) ui.Span {
	return ui.Span{Text: text, Font: f.family, Size: f.size, Weight: f.weight, Features: f.features, Color: col}
}

// featureList is the font's features as Element.FontFeatures takes them.
func (f codeFont) featureList() []string {
	if f.features == "" {
		return nil
	}
	return strings.Split(f.features, ",")
}

// width measures a run from the start of a line as the text area lays it
// out: ASCII by its cells and the tab stops, anything else by shaping it.
func (f codeFont) width(s string) float32 {
	var x float32
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\t':
			x = f.afterTab(x)
		case c < 0x80:
			x += f.charW
		default:
			w, _ := ui.ShapeText(s, f.font(), 0).Size()
			return w
		}
	}
	return x
}

// afterTab returns where a tab at x ends: the next tab stop.
func (f codeFont) afterTab(x float32) float32 {
	return (float32(int(x/f.tabW+0.001)) + 1) * f.tabW
}

// indentWidth is the width of one level of indent of a style.
func (f codeFont) indentWidth(s indentStyle) float32 {
	if s.tabs {
		return f.tabW
	}
	return float32(s.size) * f.charW
}

// gutterWidth is the width of the line numbers of a document of n lines.
func (f codeFont) gutterWidth(n int) float32 {
	digits := max(len(strconv.Itoa(n)), 3)
	return float32(digits)*f.charW + 28
}

// editorView shows a document: its text in a text area inside a container
// that scrolls it sideways, with the line numbers painted over the area's
// left padding, where they stay as it scrolls.
//
// Unwrapped, the lines are all lineH high, so that line i is at
// editorPadY+i*lineH in the area's content, which places the line numbers
// and the caret's line, and finds the lines in view to highlight. Wrapped,
// the area is as wide as the view, and says where it laid out its lines
// (geometry): what is painted on a line is placed along it as if it did
// not wrap, and then on its rows (wrapRows).
func (a *app) editorView(c *ui.Context, d *editor) {
	p, f := a.pal, a.font
	switch {
	case d.hidden:
		// Back in sight, where it was.
		d.hidden, d.pinFrame, d.resync, d.shownFrame = false, 0, true, a.frame
		d.vscroll.Y = d.hiddenY
	case d.pinFrame != 0 && a.frame > d.pinFrame+1:
		d.pinFrame = 0
	case d.pinFrame != 0 && a.frame > d.pinFrame:
		// Every build of the frame after: a frame that animates builds the
		// view again, after the area laid out where it was put back.
		d.vscroll.Y = d.pinY
	}

	if !a.settings.Vim {
		vimReset(d)
		if a.vimLine.d == d {
			a.vimCloseLine(false)
		}
	}
	// Edits asked for since the last frame, made before the text is read,
	// so that this frame shows them with the selection they make, rather
	// than the caret's line and colors of the text before them over the
	// text after. They start from the selection the last frame read, so
	// they wait for the area's own when it took keys or a click since.
	keepLine, keepY := -1, float32(0)
	early := len(d.cmds) > 0 && !d.areaInput && d.buf.Snapshot().Version() == d.version
	if early {
		keepLine, keepY = a.runCmds(d)
	}
	// What was typed since the last frame, for undo.
	d.observe()
	snap := d.buf.Snapshot()
	edited := snap.Version() != d.version
	d.version = snap.Version()
	lines := snap.LineCount()
	gutterW := f.gutterWidth(lines)
	d.gutterW = gutterW
	view := d.hscroll.Bounds(c)
	if view.H > 0 {
		d.lastView = view
	} else {
		// A tab shown again builds a new container, which has no bounds
		// until it is laid out: it takes the place of the one before.
		view = d.lastView
	}
	d.vim.viewH = view.H
	textX := gutterW + gutterGap
	// Vim's normal and visual modes take the keys as commands: the area
	// types nothing, and shows a block for its caret.
	vimKeys := a.settings.Vim && d.vim.mode != vimInsert
	focused := d.vscroll.Focused(c)
	g := a.geometry(d, lines)
	wrap := g.lines != nil
	if wrap {
		d.hscroll.X = 0
	}
	if r := d.reloaded; r != nil {
		// The line at the top, found where the lines were before, stays at
		// the top, below the lines the reload added or took above it.
		d.reloaded = nil
		top := g.at(d.vscroll.Y - editorPadY)
		keepY = editorPadY + g.top(top) - d.vscroll.Y
		if top > r.to {
			top += r.delta
		}
		keepLine = min(max(top, 0), lines-1)
	}

	ui.ScrollHorizontal(c).TrackScroll(&d.hscroll).Fill().Background(p.editor).Children(func() {
		area := ui.TextAreaBufferBase(c, d.buf).
			Font(f.family).FontSize(f.size).FontWeight(f.weight).FontFeatures(f.featureList()...).FixedLineHeight(f.lineH).TextColor(p.text).
			FillHeight().
			Padding(editorPadY, textPadR, editorPadY, textX).
			Label(locales.EditorLabel(d.name())).
			ReadOnly(vimKeys || a.chording() || d.locked())
		a.takeKeys(area, placeEditor, func(ev ui.InputEvent) bool {
			if a.editorKey(d, ev) {
				return true
			}
			if ev.Button == 0 {
				switch ev.Kind {
				case ui.InputPointerDown:
					d.selecting = true
				case ui.InputPointerUp:
					d.selecting = false
				}
			}
			switch ev.Kind {
			case ui.InputKeyUp, ui.InputPointerUp, ui.InputScroll, ui.InputZoom:
			default:
				d.areaInput = true // it may move the area's selection
			}
			return false
		})
		if wrap {
			area.TrackLines(&d.lines)
			if keepLine >= 0 {
				g := a.geometry(d, lines)
				d.vscroll.Y = max(0, editorPadY+g.top(min(keepLine, lines-1))-keepY)
			}
		} else {
			area.NoWrap()
			if keepLine >= 0 {
				d.vscroll.Y = max(0, editorPadY+g.top(keepLine)-keepY)
			}
		}

		// A new area puts the caret at the end: at the editor's selection,
		// which is the start for a file just opened.
		d.resync = d.resync || !d.caretSeen
		quiet := d.pending != nil && d.quiet
		jump := d.pending != nil && !quiet
		// A jump asked for goes a third of the way down from an edge of
		// the view too; an edit's selection only from out of view.
		center := jump && (d.center || !a.inView(d, g, snap, d.pending[1], view.H))
		if r := d.pending; quiet {
			// The view stays where it is, as it does for an edit made in
			// another column.
			d.sel = *r
			d.pending, d.quiet, d.resync, d.parked = nil, false, true, -1
		} else if r != nil {
			// A jump asked for goes where it goes: no pin puts it back.
			area.SetTextSelection(r[0], r[1])
			d.pending, d.resync, d.parked, d.pinFrame = nil, false, -1, 0
			if d.landing {
				d.landing, d.landLine, d.landAt = false, snap.LineAt(min(r[0], snap.Len())), c.Now()
			}
		}
		// The frame a hidden tab comes back in builds it a new text area,
		// which scrolls to a caret it is given out of view: it is covered
		// for that frame, until the pin puts it back.
		shown := d.shownFrame == a.frame
		if d.resync {
			// The area scrolls to its caret when it is given one, and when
			// its text changes under it: out of view, and the keyboard
			// elsewhere, the caret waits in view instead, so that an edit
			// made in another column leaves this one where it is.
			d.resync, d.parked = false, -1
			if !focused && view.H > 0 && !a.inView(d, g, snap, d.sel[1], view.H) {
				d.parked = a.offsetInView(d, g, snap, view.H)
				area.SetTextSelection(d.parked, d.parked)
			} else {
				a.setSelection(d, area)
				if shown && (wrap || view.H <= 0 || !a.inView(d, g, snap, d.sel[1], view.H)) {
					d.coverFrame = a.frame
				}
			}
		}
		anchor, caret := area.TextAnchorCaret()
		d.areaInput = false
		if d.parked >= 0 {
			switch {
			case anchor != d.parked || caret != d.parked:
				d.parked = -1 // the pointer put the caret
			case focused:
				// The keyboard is back: so is the caret.
				d.parked = -1
				a.setSelection(d, area)
				if shown {
					d.coverFrame = a.frame
				}
				anchor, caret = d.sel[0], d.sel[1]
			default:
				anchor, caret = d.sel[0], d.sel[1]
			}
		}
		if sel := [2]int{anchor, caret}; sel != d.sel {
			if !edited {
				d.hist.typing = false // the caret moved: typing after starts a step
				if n := snap.Len(); snap.LineAt(min(caret, n)) != snap.LineAt(min(d.sel[1], n)) {
					d.closers = d.closers[:0]
				}
			}
			d.sel = sel
		}
		if !early && len(d.cmds) > 0 {
			// The next frame shows them.
			a.runCmds(d)
		}
		caretLine := snap.LineAt(min(caret, snap.Len()))
		if vimKeys && d.vim.mode.visual() {
			// The line of vim's cursor, not of the end of the selection.
			caretLine = snap.LineAt(vimCursor(d, snap))
		}
		// The view follows the caret of the editor typed in, not one that
		// an edit made in another column moved.
		follows := focused || jump
		if quiet {
			d.caret = caret
		}
		caretMoved := caret != d.caret && d.caretSeen && follows

		if caret != d.caret || !d.caretSeen {
			if d.caretSeen && follows && !wrap {
				a.revealColumn(d, snap, caret, view.W, textX)
			}
			d.caret, d.caretSeen = caret, true
		}
		// Show the caret's line before the area takes the offset, so that
		// it finds the caret in view and leaves the offset be.
		d.sticky = append(d.sticky[:0], a.scrollToCaret(d, g, snap, caret, view.H, center, jump || caretMoved || edited && follows && !quiet)...)
		var sticky []stickyRow
		if len(d.sticky) > 0 {
			sticky = d.stuck.rows(a, d, snap, d.sticky)
		}
		d.reviewHover = -1
		if a.settings.EditorAgentContext {
			_, py, over := area.PointerPosition()
			if over && py >= float32(len(d.sticky))*a.font.lineH {
				d.reviewHover = g.at(py + d.vscroll.Y - editorPadY)
			}
		}

		// The lines in view.
		first, last := a.linesInView(g, d.vscroll.Y, view.H)
		contentW := view.W
		if m := &d.measured; !wrap && (m.version != snap.Version() || m.first != first || m.last != last) {
			m.version, m.first, m.last = snap.Version(), first, last
			for i := first; i <= last; i++ {
				s, e := snap.LineRange(i)
				if e-s > d.widest {
					d.widest = max(d.widest, columns(snap.Slice(s, e)))
				}
			}
		}
		if !wrap {
			contentW = max(view.W, textX+float32(d.widest)*f.charW+textPadR)
		}
		if contentW > 0 {
			area.Width(contentW)
		} else {
			area.FillWidth()
		}
		area.TrackScroll(&d.vscroll).
			// The area is as wide as its widest line: keep its scroll bar
			// at the right of the view, wherever it scrolls sideways.
			ScrollbarInsets(2, max(0, d.hscroll.MaxX-d.hscroll.X)+2, 2, 0)
		if d.focusNext {
			d.focusNext = false
			d.vscroll.Focus(c)
		}

		if d.hl != nil && d.hl.Lexer() != nil {
			d.hl.Request(snap, first, last)
			area.TextRanges(d.colors.ranges(d.hl, p, snap, first, last)...)
		}

		lineH, charW, size, family := f.lineH, f.charW, f.size, f.family
		// Who changed the caret's line last, after it, unless it wraps.
		var blame string
		var blameX float32
		// The comment of each review note after its last line, in its
		// place, unless the lines wrap.
		noted := a.notedLines(d)
		var hints []editorHint
		if !wrap {
			hints = a.editorHints(d, snap, noted, first, last)
		}
		if !wrap && !slices.ContainsFunc(hints, func(h editorHint) bool { return h.line == caretLine }) {
			if blame = a.blameLine(d, caretLine, snap.Version(), focused); blame != "" {
				s, e := snap.LineRange(caretLine)
				blameX = textX + f.width(snap.Slice(s, min(e, s+maxBlameAfter))) + 4*charW
			}
		}
		matches := a.visibleMatches(d, first, last)
		if matches == nil {
			matches = a.vimMatches(d, snap, first, last)
		}
		deco := a.decorate(d, snap, first, last)
		d.git.Refresh(d.path, d.buf.Snapshot(), a.post)
		marks := d.git.Lines
		gitRuns := d.gitRuns()
		guideW := f.indentWidth(d.indent)
		rows := a.overviewRows(d, snap, view.H)
		spaces := a.selectedWhitespace(d, snap, first, last)
		// The editor paints the selection, rounded, and keeps it in sight,
		// fainter, while the keyboard is elsewhere, as in find's field.
		shape := a.selection(d, snap, first, last)
		selecting := anchor != caret
		selCol := p.selected
		if !focused {
			selCol = p.selectedDim
		}
		if !shape.native {
			area.SelectionColor(ui.Color{})
		}
		// The block of vim's cursor: from the cursor to the next rune, or a
		// cell wide at the end of a line.
		var block *runeRect
		if vimKeys {
			at := vimCursor(d, snap)
			l := snap.LineAt(at)
			s, e := snap.LineRange(l)
			var x0, x1 float32
			if e-s > highlightRunes {
				x0 = float32(at-s) * f.charW
				x1 = x0 + f.charW
			} else {
				x0 = f.width(snap.Slice(s, at))
				x1 = x0 + f.charW
				if at < e {
					x1 = f.width(snap.Slice(s, at+1))
				}
			}
			block = &runeRect{line: l, x0: x0, x1: x1}
		}
		// While an operator waits for its motion, the block is half as
		// high, as vim's cursor is.
		halfBlock := vimKeys && d.vim.operatorPending()
		// The text yanked lights up and fades.
		var flash []runeRect
		var flashK float32
		if since := time.Since(d.vim.flashAt); vimKeys && since < vimFlashFor && !c.Preferences().ReduceMotion {
			c.AnimationFrame()
			flashK = 1 - float32(since)/float32(vimFlashFor)
			from, to := d.vim.flash[0], min(d.vim.flash[1], snap.Len())
			for l := max(first, snap.LineAt(from)); l <= min(last, snap.LineAt(to)); l++ {
				s, e := snap.LineRange(l)
				a0, a1 := max(from, s), min(to, e)
				if a1 < a0 || l > snap.LineAt(from) && a1 == s && to <= s {
					continue
				}
				x0 := f.width(snap.Slice(s, a0))
				x1 := f.width(snap.Slice(s, a1))
				if to > e {
					x1 += f.charW / 2 // and its newline
				}
				flash = append(flash, runeRect{line: l, x0: x0, x1: max(x1, x0+f.charW/2)})
			}
		}
		// rowsAt finds where the lines in view wrap, as this frame laid
		// them out, and lineY places line i's first row.
		var wr *wrapRows
		rowsAt := func(r ui.Rect) {
			if wrap {
				// The lines in view now, with those the decorations were
				// found for, unless the view went far.
				from, to := a.linesInView(g, d.vscroll.Y, r.H)
				if first >= from-rowSlack && last <= to+rowSlack {
					from, to = min(from, first), max(to, last)
				}
				wr = a.rowsOf(d, snap, from, to, r.W-textX-textPadR)
			}
		}
		lineY := func(r ui.Rect, i int) float32 { return r.Y + editorPadY + g.top(i) - d.vscroll.Y }
		// lineHeight is the height of line i, all its rows.
		lineHeight := func(i int) float32 { return float32(wr.rowCount(i)) * lineH }
		// place calls fn with the part on each row of a run along line i:
		// its top, and its x's in the area.
		place := func(r ui.Rect, i int, x0, x1 float32, fn func(y, x0, x1 float32)) {
			y := lineY(r, i)
			wr.pieces(i, x0, x1, func(row int, x0, x1 float32) {
				fn(y+float32(row)*lineH, r.X+textX+x0, r.X+textX+x1)
			})
		}
		area.Draw(func(pt *ui.Painter, r ui.Rect) {
			rowsAt(r)
			if y := lineY(r, caretLine); !selecting && y+lineH > r.Y && y < r.Y+r.H {
				pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: lineHeight(caretLine)}, p.line, 0)
			}
			// The line a jump landed on, lit, fading as the eye finds it.
			if since := pt.Now().Sub(d.landAt); !d.landAt.IsZero() && since >= 0 && since < landFor && d.landLine < lines {
				t := float32(since) / float32(landFor)
				pt.Fill(ui.Rect{X: r.X, Y: lineY(r, d.landLine), W: r.W, H: lineHeight(d.landLine)}, p.accent.Alpha(0.26*(1-t*t)), 0)
				pt.AnimationFrame()
			}
			shape.paint(pt, func(i int, x0, x1 float32, fn func(y, x0, x1 float32)) { place(r, i, x0, x1, fn) }, lineH, selCol)
			for _, fl := range flash {
				place(r, fl.line, fl.x0, fl.x1, func(y, x0, x1 float32) {
					pt.Fill(ui.Rect{X: x0 - 1, Y: y + 1, W: x1 - x0 + 2, H: lineH - 2}, p.accent.Alpha(0.32*flashK), 3)
				})
			}
			if block != nil {
				place(r, block.line, block.x0, block.x1, func(y, x0, x1 float32) {
					box := ui.Rect{X: x0, Y: y, W: x1 - x0, H: lineH}
					if halfBlock {
						box.Y, box.H = y+lineH/2, lineH/2
					}
					if focused {
						pt.Fill(box, p.cursor, 1)
					} else {
						pt.Stroke(box, p.cursor, 1, 1)
					}
				})
			}
			for _, w := range deco.words {
				if selecting && len(shape.rects) == 1 && shape.rects[0].line == w.line && shape.rects[0].x0 == w.x0 {
					continue // the selection itself
				}
				place(r, w.line, w.x0, w.x1, func(y, x0, x1 float32) {
					pt.Fill(ui.Rect{X: x0 - 1, Y: y + 1, W: x1 - x0 + 2, H: lineH - 2}, p.word, 3)
				})
			}
			// The indent guides, at the start of each level of a line's
			// indent, and brighter along the caret's block, down all the
			// rows of a line that wraps.
			for k, level := range deco.levels {
				i := deco.first + k
				y, h := lineY(r, i), lineHeight(i)
				for l := range level {
					col := p.guide
					if l == deco.active-1 && i >= deco.activeFrom && i <= deco.activeTo {
						col = p.guideNow
					}
					pt.Fill(ui.Rect{X: r.X + textX + float32(l)*guideW, Y: y, W: 1, H: h}, col, 0)
				}
			}
			for _, b := range deco.brackets {
				place(r, b.line, b.x0, b.x1, func(y, x0, x1 float32) {
					box := ui.Rect{X: x0 - 0.5, Y: y + 1.5, W: x1 - x0 + 1, H: lineH - 3}
					pt.Fill(box, p.word, 2)
					pt.Stroke(box, p.bracket, 2, 1)
				})
			}
			// Spaces and tabs selected show, faint: a dot, and an arrow as
			// long as the tab.
			var arrows ui.Path
			for _, w := range spaces {
				place(r, w.line, w.x0, w.x1, func(y, x0, x1 float32) {
					cy := y + lineH/2
					if w.tab {
						x0, x1 := x0+3, x1-4
						arrows.MoveTo(x0, cy).LineTo(x1, cy)
						arrows.MoveTo(x1-3, cy-3).LineTo(x1, cy).LineTo(x1-3, cy+3)
					} else {
						cx := (x0 + x1) / 2
						pt.Fill(ui.Rect{X: cx - 1, Y: cy - 1, W: 2, H: 2}, p.faint, 1)
					}
				})
			}
			if len(spaces) > 0 {
				pt.StrokePath(&arrows, 1, p.faint)
			}
			for _, m := range matches {
				place(r, m.line, m.x0, m.x1, func(y, x0, x1 float32) {
					box := ui.Rect{X: x0 - 1, Y: y + 1, W: x1 - x0 + 2, H: lineH - 2}
					if m.cur {
						pt.Fill(box, p.matchNow, 3)
						pt.Stroke(box, p.matchRing, 3, 1)
					} else {
						pt.Fill(box, p.match, 3)
					}
				})
			}
			for _, h := range hints {
				y := lineY(r, h.line)
				pt.Fill(ui.Rect{X: r.X + h.x - 4, Y: y + 2, W: h.w + 8, H: lineH - 4}, p.accent.Alpha(0.09), 4)
				icon := size * 0.85
				pt.Icon(iconComment, ui.Rect{X: r.X + h.x, Y: y + (lineH-icon)/2, W: icon, H: icon}, p.accent)
				span := ui.Span{Text: h.text, Font: family, Size: size * 0.92, Color: p.muted}
				_, th := pt.MeasureText(0, span)
				pt.RichText(r.X+h.x+icon+hintGap*charW, y+(lineH-th)/2, 0, span)
			}
			if blame != "" {
				span := ui.Span{Text: blame, Font: family, Size: size * 0.92, Color: p.faint}
				_, h := pt.MeasureText(0, span)
				pt.RichText(r.X+blameX, lineY(r, caretLine)+(lineH-h)/2, 0, span)
			}
		})
		area.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			if wr == nil {
				rowsAt(r)
			}
			x := r.X + d.hscroll.X
			top := d.vscroll.Y
			from, to := a.linesInView(g, top, r.H)
			stuck := float32(len(sticky)) * lineH
			_, numH := pt.MeasureText(0, ui.Span{Text: "0", Font: family, Size: size})
			number := func(i int, y float32, col ui.Color) {
				label := strconv.Itoa(i + 1)
				pt.RichText(x+gutterW-14-float32(len(label))*charW, y+(lineH-numH)/2, 0,
					ui.Span{Text: label, Font: family, Size: size, Color: col})
			}

			// The lines that open the blocks in view stick at the top, over
			// the text, scrolling sideways with it.
			for k, row := range sticky {
				y := r.Y + float32(k)*lineH
				pt.Fill(ui.Rect{X: x, Y: y, W: view.W, H: lineH}, p.editor, 0)
				_, h := pt.MeasureText(0, row.spans...)
				pt.RichText(r.X+textX+row.indent, y+(lineH-h)/2, 0, row.spans...)
			}

			pt.Fill(ui.Rect{X: x, Y: r.Y, W: textX - 2, H: r.H}, p.editor, 0)
			if d.hscroll.X > 0 {
				// A shadow at the edge the text scrolls under.
				pt.FillGradient(ui.Rect{X: x + textX - 2, Y: r.Y, W: 6, H: r.H}, ui.LinearGradient{
					From: ui.RGBA(0, 0, 0, 0.18), To: ui.RGBA(0, 0, 0, 0), Angle: 90,
				}, 0)
			}
			// Along the right edge, where the scroll bar goes, the whole
			// document: the lines changed since the last commit, find's
			// matches and the caret's line.
			if lines > 1 && len(gitRuns)+len(rows)+len(noted) > 0 {
				right := x + view.W - 10
				for _, g := range gitRuns {
					y, h := r.Y+g.from*r.H, max((g.to-g.from)*r.H, 2)
					switch g.kind {
					case linediff.Added:
						pt.Fill(ui.Rect{X: right, Y: y, W: 3, H: h}, p.added, 0)
					case linediff.Modified:
						pt.Fill(ui.Rect{X: right, Y: y, W: 3, H: h}, p.changed, 0)
					default:
						pt.Fill(ui.Rect{X: right, Y: y - 1, W: 3, H: 3}, p.removed, 0)
					}
				}
				for _, f := range rows {
					pt.Fill(ui.Rect{X: right + 4, Y: r.Y + f*r.H, W: 5, H: 2}, p.matchRing, 1)
				}
				for _, n := range noted {
					pt.Fill(ui.Rect{X: right + 4, Y: r.Y + float32(n.from)/float32(lines)*r.H - 1, W: 5, H: 3}, p.accent, 1)
				}
				caretAt := float32(caretLine) / float32(lines)
				pt.Fill(ui.Rect{X: right, Y: r.Y + caretAt*r.H - 1, W: 9, H: 2}, p.text.Alpha(0.6), 1)
			}
			if stuck > 0 {
				// A hairline closes the lines that stick off from the text.
				pt.Fill(ui.Rect{X: x, Y: r.Y + stuck - 1, W: view.W, H: 1}, p.border, 0)
			}
			if top > 0 {
				// A shadow under the breadcrumbs, or the lines that stick, as
				// the text scrolls up under them.
				pt.FillGradient(ui.Rect{X: x, Y: r.Y + stuck, W: view.W, H: 6}, ui.LinearGradient{
					From: p.shadow, To: p.shadow.Alpha(0), Angle: 180,
				}, 0)
			}
			pt.Clip(ui.Rect{X: x, Y: r.Y + stuck, W: gutterW, H: r.H - stuck}, 0, func() {
				// The lines changed since the last commit: a bar by their
				// numbers, down all the rows of a line that wraps, and a
				// notch where lines were removed.
				barX := x + gutterW - 7
				for i := from; i <= min(to, len(marks)-1); i++ {
					y, h := lineY(r, i), lineHeight(i)
					switch marks[i] {
					case linediff.Added:
						pt.Fill(ui.Rect{X: barX, Y: y, W: 3, H: h}, p.added, 0)
					case linediff.Modified:
						pt.Fill(ui.Rect{X: barX, Y: y, W: 3, H: h}, p.changed, 0)
					case linediff.Deleted:
						pt.Fill(ui.Rect{X: barX, Y: y - 2.5, W: 7, H: 5}, p.removed, 2.5)
					case linediff.DeletedBelow:
						pt.Fill(ui.Rect{X: barX, Y: y + h - 2.5, W: 7, H: 5}, p.removed, 2.5)
					}
				}
				// A bar along the lines of each review comment.
				for _, n := range noted {
					for i := max(n.from, from); i < n.to && i <= to; i++ {
						pt.Fill(ui.Rect{X: x + 2, Y: lineY(r, i), W: 2, H: lineHeight(i)}, p.accent, 0)
					}
				}
				// A number by the first row of each line.
				for i := from; i <= to; i++ {
					col := p.gutter
					if i == caretLine && focused {
						col = p.gutterNow
					} else if i == caretLine {
						col = p.muted
					}
					number(i, lineY(r, i), col)
				}
			})
			for k, row := range sticky {
				number(row.line, r.Y+float32(k)*lineH, p.muted)
			}
		})
	})
	if d.coverFrame == a.frame {
		// Built again, the text area scrolls to the selection it is given:
		// it is covered for that frame, until the pin puts it back.
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(p.editor)
	}
}

// runCmds makes the edits asked for since the last frame. While the lines
// wrap, it returns the line at the top of the view and where it shows, to
// keep it there: the area forgets the heights of the rows it measured
// when the app edits its text, and guesses them again. The line is -1
// when there was no edit, or one scrolled where it goes.
func (a *app) runCmds(d *editor) (keepLine int, keepY float32) {
	keepLine = -1
	if a.wraps(d) {
		g := a.geometry(d, d.buf.Snapshot().LineCount())
		keepLine = g.at(d.vscroll.Y - editorPadY)
		keepY = editorPadY + g.top(keepLine) - d.vscroll.Y
	}
	y, version := d.vscroll.Y, d.buf.Snapshot().Version()
	cmds := d.cmds
	d.cmds = nil
	for _, cmd := range cmds {
		cmd(d)
	}
	if d.vscroll.Y != y || d.buf.Snapshot().Version() == version {
		keepLine = -1
	}
	a.invalidate()
	return keepLine, keepY
}

// linesInView returns the first and last lines that show, a little of
// them or all, in a view viewH high, the area scrolled to y.
func (a *app) linesInView(g geometry, y, viewH float32) (first, last int) {
	if g.lines == nil {
		first = max(0, int((y-editorPadY)/g.lineH))
		return first, min(g.n-1, first+int(viewH/g.lineH)+1)
	}
	return g.at(y - editorPadY), g.at(y - editorPadY + viewH)
}

// colorCache keeps the colors of the tokens of the lines in view, which
// the text area is given every frame, and which are the same until the
// text, the view, the tokens or the palette change.
type colorCache struct {
	version, gen uint64
	first, last  int
	pal          *palette
	out          []ui.TextRange
}

// ranges returns the colors of the tokens of lines first up to last. The
// text area looks through all of them for each of its lines, and makes a
// span of each and of each gap between two, so they are as few as say the
// same: a token in the color of the text is left out, and the blanks
// after a token, which show no color, are part of it, so that tokens of
// one color with only blanks between them are one range, and those of two
// colors leave no gap.
func (cc *colorCache) ranges(hl *highlight.Highlighter, p *palette, snap ui.TextSnapshot, first, last int) []ui.TextRange {
	gen := hl.Gen()
	if cc.out != nil && cc.version == snap.Version() && cc.gen == gen && cc.first == first && cc.last == last && cc.pal == p {
		return cc.out
	}
	cc.version, cc.gen, cc.first, cc.last, cc.pal = snap.Version(), gen, first, last, p
	out := cc.out[:0]
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		if e-s > highlight.MaxLineRunes {
			continue
		}
		text := snap.Slice(s, e)
		ascii := isASCII(text) // a rune is a byte: the blanks are easy to find
		from := len(out)
		for _, tok := range hl.Line(i, text) {
			col := p.syntax[tok.Kind]
			if col == p.text {
				continue
			}
			if k := len(out) - 1; k >= from && ascii && out[k].End-s <= tok.Start && blank(text[out[k].End-s:tok.Start]) {
				if out[k].Color == col {
					out[k].End = s + tok.End
					continue
				}
				out[k].End = s + tok.Start
			}
			out = append(out, ui.TextRange{Start: s + tok.Start, End: s + tok.End, Color: col})
		}
	}
	if out == nil {
		out = []ui.TextRange{}
	}
	cc.out = out
	return out
}

// blank reports whether s is only spaces and tabs.
func blank(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// scrollToCaret scrolls the editor to show the caret's row, below the
// lines that stick at the top, and returns those lines. jump brings a row
// out of view a third of the way down, as going to a line does; reveal
// scrolls as little as it takes, as the caret moves and as an edit, an
// undo among them, selects.
//
// It runs before the text area takes the offset, which would otherwise
// scroll to the caret itself, under the lines that stick.
func (a *app) scrollToCaret(d *editor, g geometry, snap ui.TextSnapshot, caret int, viewH float32, jump, reveal bool) []int {
	lineH := a.font.lineH
	sticky := a.stickyAt(d, g, snap, viewH)
	if viewH <= 0 || !jump && !reveal {
		return sticky
	}
	caret = min(caret, snap.Len())
	line := snap.LineAt(caret)
	s, _ := snap.LineRange(line)
	end := max(0, 2*editorPadY+g.height()-viewH)
	top := editorPadY + g.top(line) + float32(g.row(line, caret-s))*lineH // in the content
	if jump {
		stuck := float32(len(sticky)) * lineH
		if top < d.vscroll.Y+stuck || top+lineH > d.vscroll.Y+viewH-lineH {
			d.vscroll.Y = clamp(top-viewH/3, 0, end)
			sticky = a.stickyAt(d, g, snap, viewH)
		}
	}
	// Scrolling changes which lines stick: until the two agree.
	for range 3 {
		y, was := top-d.vscroll.Y, d.vscroll.Y
		switch from, to := max(editorPadY, float32(len(sticky))*lineH), viewH-editorPadY; {
		case y < from:
			d.vscroll.Y -= from - y
		case y+lineH > to:
			d.vscroll.Y += y + lineH - to
		}
		d.vscroll.Y = clamp(d.vscroll.Y, 0, end)
		if d.vscroll.Y == was {
			break
		}
		sticky = a.stickyAt(d, g, snap, viewH)
	}
	return sticky
}

// stickyAt returns the lines that stick at the top of the editor where it
// is scrolled: above the first line wholly in view.
func (a *app) stickyAt(d *editor, g geometry, snap ui.TextSnapshot, viewH float32) []int {
	lineH := a.font.lineH
	if d.vscroll.Y <= editorPadY || !isCode(d.document) {
		return nil
	}
	y := d.vscroll.Y - editorPadY
	topLine := g.at(y)
	if g.top(topLine) <= y-1 {
		topLine++ // a pixel of it may be under the top
	}
	return stickyLines(snap, topLine, min(maxSticky, int(viewH/lineH/3)), d.scopesOf)
}

// setSelection gives an editor's text area its selection without the
// view following it: the area scrolls to its caret, for a frame, and the
// next puts the editor back where it is.
func (a *app) setSelection(d *editor, area ui.Element) {
	area.SetTextSelection(d.sel[0], d.sel[1])
	d.pinFrame, d.pinY = a.frame, d.vscroll.Y
}

// inView reports whether the first row of the line of offset at shows
// whole in a view viewH high, where the editor is scrolled.
func (a *app) inView(d *editor, g geometry, snap ui.TextSnapshot, at int, viewH float32) bool {
	top := editorPadY + g.top(snap.LineAt(min(at, snap.Len())))
	return top >= d.vscroll.Y && top+a.font.lineH <= d.vscroll.Y+viewH
}

// offsetInView returns the start of a line in the middle of the view.
func (a *app) offsetInView(d *editor, g geometry, snap ui.TextSnapshot, viewH float32) int {
	s, _ := snap.LineRange(g.at(d.vscroll.Y + viewH/2 - editorPadY))
	return s
}

// revealColumn scrolls the editor sideways, as little as it takes, to show
// the caret.
func (a *app) revealColumn(d *editor, snap ui.TextSnapshot, caret int, viewW, textX float32) {
	if viewW <= 0 {
		return
	}
	line := snap.LineAt(min(caret, snap.Len()))
	s, _ := snap.LineRange(line)
	// Measure the line as the area lays it out, tabs at the platform's
	// stops.
	x := textX + a.font.width(snap.Slice(s, min(caret, snap.Len())))
	left, right := d.hscroll.X+textX, d.hscroll.X+viewW-textPadR
	switch {
	case x < left:
		d.hscroll.X = max(0, x-textX-4*a.font.charW)
	case x > right:
		d.hscroll.X = x - viewW + textPadR + 4*a.font.charW
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
