package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
	"mayoi/internal/settings"
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
		family: p.FontFamily, size: p.FontSize, weight: p.FontWeight, features: strings.Join(p.FontFeatures, ","),
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
	return f.charW != 0 && f.family == p.FontFamily && f.size == p.FontSize && f.weight == p.FontWeight &&
		f.features == strings.Join(p.FontFeatures, ",") && f.lineHeight == p.LineHeight
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
	case d.pinFrame != 0 && a.frame > d.pinFrame:
		d.pinFrame = 0
		d.vscroll.Y = d.pinY
	}
	if !a.settings.Vim {
		vimReset(d)
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

	ui.ScrollHorizontal(c).TrackScroll(&d.hscroll).Fill().Background(p.editor).Children(func() {
		area := ui.TextAreaBufferBase(c, d.buf).
			Font(f.family).FontSize(f.size).FontWeight(f.weight).FontFeatures(f.featureList()...).FixedLineHeight(f.lineH).TextColor(p.text).
			FillHeight().
			Padding(editorPadY, textPadR, editorPadY, textX).
			Label("Editor " + d.name()).
			ReadOnly(vimKeys || a.chording())
		a.takeKeys(area, placeEditor, func(ev ui.InputEvent) bool { return a.editorKey(d, ev) })
		if wrap {
			area.TrackLines(&d.lines)
		} else {
			area.NoWrap()
		}

		// A new area puts the caret at the end: at the editor's selection,
		// which is the start for a file just opened.
		d.resync = d.resync || !d.caretSeen
		jump := d.pending != nil
		if r := d.pending; r != nil {
			area.SetTextSelection(r[0], r[1])
			d.pending, d.resync, d.parked = nil, false, -1
		}
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
			}
		}
		selStart, caret := area.TextSelection()
		if d.parked >= 0 {
			switch {
			case selStart != d.parked || caret != d.parked:
				d.parked = -1 // the pointer put the caret
			case focused:
				// The keyboard is back: so is the caret.
				d.parked = -1
				a.setSelection(d, area)
				selStart, caret = d.sel[0], d.sel[1]
			default:
				selStart, caret = d.sel[0], d.sel[1]
			}
		}
		if sel := [2]int{selStart, caret}; sel != d.sel {
			if !edited {
				d.hist.typing = false // the caret moved: typing after starts a step
				if n := snap.Len(); snap.LineAt(min(caret, n)) != snap.LineAt(min(d.sel[1], n)) {
					d.closers = d.closers[:0]
				}
			}
			d.sel = sel
		}
		if len(d.cmds) > 0 {
			// Edits asked for since the last frame, now that the selection
			// is known; the next frame shows them.
			cmds := d.cmds
			d.cmds = nil
			for _, cmd := range cmds {
				cmd(d)
			}
			a.invalidate()
		}
		caretLine := snap.LineAt(min(caret, snap.Len()))
		if vimKeys && d.vim.mode.visual() {
			// The line of vim's cursor, not of the end of the selection.
			caretLine = snap.LineAt(vimCursor(d, snap))
		}
		// The view follows the caret of the editor typed in, not one that
		// an edit made in another column moved.
		follows := focused || jump
		caretMoved := caret != d.caret && d.caretSeen && follows

		if caret != d.caret || !d.caretSeen {
			if d.caretSeen && follows && !wrap {
				a.revealColumn(d, snap, caret, view.W, textX)
			}
			d.caret, d.caretSeen = caret, true
		}
		// Show the caret's line before the area takes the offset, so that
		// it finds the caret in view and leaves the offset be.
		d.sticky = append(d.sticky[:0], a.scrollToCaret(d, g, snap, caret, view.H, jump, caretMoved || edited && follows)...)
		var sticky []stickyRow
		if len(d.sticky) > 0 {
			sticky = d.stuck.rows(a, d, snap, d.sticky)
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
		if !wrap {
			if blame = a.blameLine(d, caretLine, snap.Version(), focused); blame != "" {
				s, e := snap.LineRange(caretLine)
				blameX = textX + f.width(snap.Slice(s, min(e, s+maxBlameAfter))) + 4*charW
			}
		}
		matches := a.visibleMatches(d, first, last)
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
		selecting := selStart != caret
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
			shape.paint(pt, func(i int, x0, x1 float32, fn func(y, x0, x1 float32)) { place(r, i, x0, x1, fn) }, lineH, selCol)
			if block != nil {
				place(r, block.line, block.x0, block.x1, func(y, x0, x1 float32) {
					box := ui.Rect{X: x0, Y: y, W: x1 - x0, H: lineH}
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
			if lines > 1 && len(gitRuns)+len(rows) > 0 {
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
	if d.shownFrame == a.frame {
		// Built again, the text area scrolls to the selection it is given:
		// it is covered for that frame, until the pin puts it back.
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(p.editor)
	}
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
// scrolls as little as it takes, as the caret moves.
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
