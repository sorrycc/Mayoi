package main

import (
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"golang.org/x/text/width"

	"mayoi/internal/bg"
	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
)

// liveState is a document's Live Preview: its parses, and the blocks it
// shows rendered.
type liveState struct {
	// doc is the last parse, quick when it was the quick one of the
	// first frame, and mapped that parse moved to the text as it is.
	doc, mapped *liveDoc
	mappedFrom  *liveDoc
	quick       bool
	cache       markdownCache
	// running is set while a parse runs, pending while one waits for the
	// edits to pause, generation numbers the parses asked for, and wait
	// is the timer of the next.
	running, pending bool
	generation       int
	wait             *time.Timer
	// mermaid and math are the settings the last parse drew in, and stale
	// is set once a picture of the web arrived since, to parse again.
	mermaid *mermaidStyle
	math    bool
	stale   bool
	// views are the blocks rendered, by their keys.
	views map[string]*liveView
	// measure is how wide the text is at most, in font: the Reading
	// view's column, so that switching keeps its lines.
	measure  float32
	measured ui.Font
	// widths are how wide runs of text are in their fonts, as measured.
	widths map[liveWidthKey]float32
	// in is the block the caret is in, or left last.
	in liveIn
}

// liveWidthKey is a run of text in a font.
type liveWidthKey struct {
	text string
	font ui.Font
}

// width is how wide text is in font.
func (l *liveState) width(text string, font ui.Font) float32 {
	k := liveWidthKey{text, font}
	if w, ok := l.widths[k]; ok {
		return w
	}
	if l.widths == nil || len(l.widths) >= 512 {
		l.widths = map[liveWidthKey]float32{}
	}
	w, _ := ui.ShapeText(text, font, 0).Size()
	l.widths[k] = w
	return w
}

// liveIn is the block the caret went into, by its first line and its last,
// and when; or, once it left, when it did. Its height glides from what it
// showed as to what it shows as now, so that the text below it moves over
// rather than jumps: source is the height of its lines of source, drawn
// that of its drawing, with its margin, and blank that of the blank line
// after it, which takes the room the drawing gives up as it glides.
type liveIn struct {
	first, last int
	in          bool
	at          time.Time
	source      float32
	drawn       float32
	blank       float32
}

// liveView is a block Live Preview shows rendered: its preview, where it
// showed, at, and how high, and the frame it showed in last.
type liveView struct {
	v      *viewer
	box    ui.Handle
	at     ui.Rect
	height float32
	frame  int
	// source is how high its lines of source showed last, 0 before they
	// did.
	source float32
}

// liveHit is a task's box the last frame drew, where it showed, and the
// rune of its [.
type liveHit struct {
	r  ui.Rect
	at int
}

// liveDelay is how long after the last edit the text is parsed again.
// Until then, the last parse follows the edits.
const liveDelay = 120 * time.Millisecond

// liveQuickMax is the longest text, in bytes, parsed on the main thread
// for the first frame of Live Preview to show it styled.
const liveQuickMax = 256 << 10

// liveSlack is how many lines beyond those in view Live Preview styles,
// for the lines the view scrolls to in the next frame.
const liveSlack = 24

// livePreview reports whether an editor shows its text as Live Preview.
func (a *app) livePreview(d *editor) bool {
	return d.live && d.preview == nil && isMarkdown(d.document)
}

// setLive shows an editor's text as Live Preview, or as its source,
// and its text again in place of its Reading view.
func (a *app) setLive(d *editor, on bool) {
	if d.preview != nil {
		a.setPreview(d, false)
	}
	if d.live == on {
		return
	}
	d.live = on
	a.invalidate()
	a.rememberTabs()
}

// toggleLivePreview switches the editor shown between Live Preview and
// its source, from its Reading view to Live Preview.
func (a *app) toggleLivePreview() {
	d := a.activeEditor()
	if d == nil || !isMarkdown(d.document) {
		return
	}
	a.setLive(d, d.preview != nil || !d.live)
	a.focusColumn()
}

// setMarkdownMode shows the editor shown as source, Live Preview or
// Reading view.
func (a *app) setMarkdownMode(mode string) {
	d := a.activeEditor()
	if d == nil || !isMarkdown(d.document) {
		return
	}
	switch mode {
	case "reading":
		d.live = true
		a.setPreview(d, true)
	case "live":
		a.setLive(d, true)
	default:
		a.setLive(d, false)
	}
	a.focusColumn()
}

// markdownMode is how an editor shows a Markdown document.
func markdownMode(d *editor) string {
	switch {
	case d.preview != nil:
		return "reading"
	case d.live:
		return "live"
	}
	return ""
}

// followLive parses a document's text for Live Preview, a moment after
// each edit, and returns the last parse moved to the text as it is, nil
// before the first.
func (a *app) followLive(doc *document) *liveDoc {
	l := doc.live
	if l == nil {
		l = &liveState{}
		doc.live = l
	}
	snap := doc.buf.Snapshot()
	var want *mermaidStyle
	if a.prefs().PreviewMermaid {
		st := a.mermaidStyle()
		want = &st
	}
	math := a.prefs().PreviewMath
	if l.doc == nil && snap.ByteLen() <= liveQuickMax {
		// The first frame shows it styled, but for its pictures, formulas
		// and blocks, which the full parse brings.
		quick := parseLive(snap.String(), doc.path, "", false, &markdownCache{math: math})
		quick.snap = snap
		l.doc, l.quick = quick, true
	}
	same := (want == nil) == (l.mermaid == nil) && (want == nil || *want == *l.mermaid) && l.math == math
	fresh := l.doc != nil && !l.quick && !l.stale && same && l.doc.snap.Version() == snap.Version()
	if !fresh && !l.pending && !l.running {
		a.parseLive(doc, l, want, math)
	}
	if l.doc == nil {
		return nil
	}
	if l.mapped == nil || l.mappedFrom != l.doc || l.mapped.snap.Version() != snap.Version() {
		l.mapped, l.mappedFrom = l.doc.mapTo(snap), l.doc
	}
	return l.mapped
}

// parseLive parses a document for Live Preview in the background, at
// once the first time and after liveDelay after that.
func (a *app) parseLive(doc *document, l *liveState, want *mermaidStyle, math bool) {
	l.pending = true
	l.generation++
	generation := l.generation
	if l.cache.arrived == nil {
		l.cache.arrived = func() {
			a.post(func() {
				l.stale = true
				a.invalidate()
			})
		}
	}
	start := func() {
		l.wait, l.pending = nil, false
		if generation != l.generation || l.running || !slices.Contains(a.docs, doc) {
			return
		}
		arrived := l.stale
		l.running, l.stale = true, false
		snap := doc.buf.Snapshot()
		source, path, workspace := snap.String(), doc.path, a.folder()
		// The worker has its own copy of the cache, which it hands back.
		cache := &markdownCache{images: maps.Clone(l.cache.images), code: maps.Clone(l.cache.code), arrived: l.cache.arrived,
			mermaid: want, math: math, formulas: maps.Clone(l.cache.formulas)}
		sameStyle := (want == nil) == (l.cache.mermaid == nil) && (want == nil || *want == *l.cache.mermaid)
		if want != nil && sameStyle {
			cache.diagrams = maps.Clone(l.cache.diagrams)
		}
		// The blocks drawn as they were, unless drawn otherwise now, or
		// a picture they may show arrived since.
		if sameStyle && math == l.cache.math && !arrived {
			cache.blocks = l.cache.blocks
		}
		bg.Go(func() {
			defer recoverCrash()
			root := markdownRoot(path, workspace)
			parsed := parseLive(source, path, root, true, cache)
			a.post(func() {
				l.running = false
				parsed.snap = snap
				l.doc, l.quick, l.cache = parsed, false, *cache
				l.mermaid, l.math = want, math
				a.invalidate()
			})
		})
	}
	if l.doc == nil || l.quick {
		start()
		return
	}
	l.wait = time.AfterFunc(liveDelay, func() { a.post(start) })
}

// liveMargins are the left and right of the text of Live Preview in a
// view w wide: the Reading view's column, in the middle.
func (a *app) liveMargins(c *ui.Context, doc *document, w float32) (left, right float32) {
	l := doc.live
	if l == nil {
		return textPadR, textPadR
	}
	font := ui.Font{Family: c.Theme().Font, Size: a.settings.FontSize}
	if l.measured != font || l.measure == 0 {
		l.measured = font
		l.measure, _ = ui.ShapeText(strings.Repeat("0", 120), font, 0).Size()
	}
	side := max(textPadR, (w-l.measure)/2)
	return side, side
}

// liveFrame is what a frame of Live Preview paints by.
type liveFrame struct {
	snap        ui.TextSnapshot
	g           geometry
	first, last int
	// left is where the text starts in the area, width how wide it is.
	left, width   float32
	anchor, caret int
	vimKeys       bool
	focused       bool
}

// liveArea styles the text area of an editor as Live Preview: it gives
// the area the ranges of the lines about the view, the blocks that show
// rendered, and paints the objects, the cards, bars and rules behind the
// text, and the marks of Git.
func (a *app) liveArea(c *ui.Context, d *editor, area ui.Element, f liveFrame) {
	p := a.pal
	size := a.settings.FontSize
	doc := a.followLive(d.document)
	l := d.document.live
	snap := f.snap
	n := snap.Len()
	from, _ := snap.LineRange(max(0, f.first-liveSlack))
	_, to := snap.LineRange(min(snap.LineCount()-1, f.last+liveSlack))

	// What the caret or the selection touches shows its marks.
	selFrom, selTo := min(f.anchor, f.caret), max(f.anchor, f.caret)
	selFrom, selTo = min(selFrom, n), min(selTo, n)
	lineFrom, lineTo := snap.LineAt(selFrom), snap.LineAt(selTo)
	onLines := func(start, end int) bool {
		return snap.LineAt(min(start, n)) <= lineTo && snap.LineAt(min(max(start, end-1), n)) >= lineFrom
	}
	revealed := func(s *liveSpan) bool {
		if s.byLine {
			return onLines(s.from, s.to)
		}
		return selFrom <= s.to && selTo >= s.from
	}
	// The marks of the lines the caret comes to fade in, rather than
	// blink: only their color, as their room shows at once.
	now := c.Now()
	if lines := [2]int{lineFrom, lineTo}; lines != d.liveLines {
		if d.liveLines != [2]int{} || !d.liveRevealAt.IsZero() {
			d.liveRevealAt = now
		}
		d.liveLines = lines
	}
	faint := p.faint
	if since := now.Sub(d.liveRevealAt); since >= 0 && since < motionHover {
		faint = p.faint.Alpha(ui.EaseOut(float32(since) / float32(motionHover)))
		c.AnimationFrame()
	}
	if d.tickNow {
		d.tickNow, d.liveTickAt = false, now
	}

	codeFont, family := a.settings.FontFamily, c.Theme().Font
	textFont := ui.Font{Family: family, Size: size}
	var ranges []ui.TextRange
	objects := map[int]liveDrawn{}
	var codes []liveDeco
	// The bullet whose mark shows first, as at+1, to fade between them.
	marker := 0
	if doc != nil {
		// The spans that reach the lines about the view.
		doc.visit(from, to, func(s *liveSpan) {
			switch s.kind {
			case liveStyle:
				r := a.liveRange(s, size, codeFont)
				if s.look.hang > 0 {
					if w := a.liveHang(l, snap, s, size, textFont); w > 0 {
						r.ObjectWidth, r.ObjectHeight = w/float32(s.end-s.start), 1
					}
				}
				ranges = append(ranges, r)
			case liveMark:
				if revealed(s) {
					ranges = append(ranges, ui.TextRange{Start: s.start, End: s.end, Color: faint})
				} else {
					ranges = append(ranges, ui.TextRange{Start: s.start, End: s.end, Hidden: true})
				}
			case liveObject:
				shown := revealed(s)
				task := s.obj == objTask || s.obj == objTaskDone
				if task {
					if r, ok := a.liveStrike(d, snap, s, now); ok {
						ranges = append(ranges, r)
					}
				}
				if shown {
					switch s.obj {
					case objBullet, objRing:
						// A bullet keeps its room, its mark drawn in its place.
						if marker == 0 {
							marker = s.start + 1
						}
					case objQuote:
						ranges = append(ranges, ui.TextRange{Start: s.start, End: s.end, Color: faint})
						return
					default:
						return
					}
				}
				w, h, dsc := a.liveObjectSize(l, snap, s, size, f.width, family)
				if task {
					// As wide as its source, which shows as the caret comes;
					// one of an ordered list, its box alone, no narrower than
					// a bullet.
					w = l.width(snap.Slice(s.from, s.end), textFont)
					if s.from == s.start {
						w = max(w, size*1.1)
					}
				}
				ranges = append(ranges, ui.TextRange{Start: s.start, End: s.start + 1, ObjectWidth: w, ObjectHeight: h, ObjectDescent: dsc})
				if s.end > s.start+1 {
					ranges = append(ranges, ui.TextRange{Start: s.start + 1, End: s.end, Hidden: true})
				}
				objects[s.start] = liveDrawn{s: s, shown: shown}
			}
		})
		for _, dc := range doc.decos {
			if dc.kind == decoCode && dc.end >= from && dc.start <= to {
				codes = append(codes, dc)
			}
		}
	}
	if marker != d.liveMarker {
		d.liveMarkerWas, d.liveMarker, d.liveMarkerAt = d.liveMarker, marker, now
	}
	// markerIn is how far the bullet at has turned into its mark, or back.
	markerIn := func(at int, shown bool) float32 {
		since := now.Sub(d.liveMarkerAt)
		if since < 0 || since >= motionHover || at+1 != d.liveMarker && at+1 != d.liveMarkerWas {
			if shown {
				return 1
			}
			return 0
		}
		k := ui.EaseOut(float32(since) / float32(motionHover))
		if !shown {
			return 1 - k
		}
		return k
	}
	// The code of blocks of code in its language's colors.
	if d.hl != nil && d.hl.Lexer() != nil && len(codes) > 0 {
		d.hl.Request(snap, f.first, f.last)
		// Not the fence's line, whose language reads faint.
		for i, dc := range codes {
			if dc.fenced {
				_, codes[i].start = snap.LineRange(snap.LineAt(min(dc.start, n)))
			}
		}
		for _, r := range d.colors.ranges(d.hl, p, snap, f.first, f.last, a.font.bold()) {
			if slices.ContainsFunc(codes, func(dc liveDeco) bool { return r.Start >= dc.start && r.End <= dc.end }) {
				ranges = append(ranges, ui.TextRange{Start: r.Start, End: r.End, Color: r.Color})
			}
		}
	}
	// Find's matches, over the rest.
	if fd := &a.find; fd.open && fd.doc == d.id {
		i := sort.Search(len(fd.matches), func(i int) bool { return fd.matches[i][1] > from })
		for ; i < len(fd.matches) && fd.matches[i][0] <= to; i++ {
			m := fd.matches[i]
			if m[1] > n {
				break
			}
			col := p.match
			if i == fd.cur {
				col = p.matchNow
			}
			ranges = append(ranges, ui.TextRange{Start: m[0], End: m[1], Background: col, Radius: 3})
		}
	}
	area.TextRanges(ranges...)

	// The blocks shown rendered, where the caret is not.
	type shown struct {
		b     *liveBlock
		lv    *liveView
		first int
		// clip is how high it shows while its room grows, 0 for all of it,
		// and fade how far it faded in.
		clip, fade float32
	}
	var blocks []shown
	if doc != nil {
		if l.views == nil {
			l.views = map[string]*liveView{}
		}
		lineH := size * a.settings.LineHeight
		in, glide := &l.in, !c.Preferences().ReduceMotion
		caretIn := false
		for i := range doc.blocks {
			b := &doc.blocks[i]
			if b.end < from || b.start > to || len(b.blocks) == 0 {
				continue
			}
			first, last := snap.LineAt(b.start), snap.LineAt(b.end)
			lv := l.views[b.key]
			if onLines(b.start, b.end) {
				if caretIn {
					continue
				}
				caretIn = true
				if !in.in || in.first != first {
					// Gone in: the drawing's room it gives up glides away
					// below its source.
					drawn := in.drawn
					if lv != nil {
						drawn = lv.height + size
					} else if in.first != first {
						drawn = 0
					}
					source := float32(last-first+1) * f.g.lineH
					if lv != nil && lv.source > 0 {
						source = lv.source
					}
					*in = liveIn{first: first, in: true, at: now, source: source, drawn: drawn,
						blank: f.g.top(last+2) - f.g.top(last+1)}
				} else {
					// Its lines showed as source in the last frame.
					in.source = f.g.top(last+1) - f.g.top(first)
				}
				in.last = last
				if lv != nil {
					lv.source = in.source
				}
				gap := in.drawn - in.source
				since := now.Sub(in.at)
				if glide && gap > 0.5 && since >= 0 && since < motionMove && last+1 < snap.LineCount() &&
					strings.TrimSpace(snap.Line(last+1)) == "" {
					k := ui.EaseInOut(float32(since) / float32(motionMove))
					area.TextBlocks(ui.TextBlock{First: last + 1, Last: last + 1, Height: in.blank + gap*(1-k)})
					c.AnimationFrame()
				}
				area.TextRanges(a.liveGlyphs(l, snap, max(first, f.first-liveSlack), min(last, f.last+liveSlack), size, codeFont, objects)...)
				continue
			}
			if lv == nil {
				m := &markdownPreview{blocks: b.blocks, lines: strings.Count(b.source, "\n") + 1}
				lv = &liveView{v: &viewer{doc: d.document, markdown: m, loaded: true}}
				lv.height = float32(last-first+1) * lineH * 1.4
				if in.first == first && in.drawn > size {
					// Its source edited: as high as it drew before, until
					// it shows how high it draws now.
					lv.height = in.drawn - size
				}
				l.views[b.key] = lv
			} else if &lv.v.markdown.blocks[0] != &b.blocks[0] {
				// Drawn again, as in another theme's colors: the view shows
				// the new drawing, at the height it had.
				m := &markdownPreview{blocks: b.blocks, lines: lv.v.markdown.lines}
				lv.v = &viewer{doc: d.document, markdown: m, loaded: true}
			}
			lv.frame = a.frame
			lv.v.markdown.measure = f.width
			sh := shown{b: b, lv: lv, first: first, fade: 1}
			height := lv.height + size
			if in.first == first {
				if in.in {
					// Left: its room glides from its source's to its
					// drawing's, and the drawing fades in over it.
					in.in, in.at = false, now
				}
				since := now.Sub(in.at)
				if since >= 0 && since < motionShow {
					sh.fade = ui.EaseOut(float32(since) / float32(motionShow))
					c.AnimationFrame()
				}
				if glide && in.source > 0 && since >= 0 && since < motionMove {
					k := ui.EaseInOut(float32(since) / float32(motionMove))
					height = in.source + (height-in.source)*k
					sh.clip = max(1, height-size)
					c.AnimationFrame()
				}
			}
			area.TextBlocks(ui.TextBlock{First: first, Last: last, Height: height})
			blocks = append(blocks, sh)
		}
		if in.in && !caretIn && (lineFrom < in.first || lineFrom > in.last) {
			// Gone from it with the block out of view: nothing glides.
			*in = liveIn{first: -1}
		}
		// The blocks out of view for long are let go.
		for k, lv := range l.views {
			if a.frame-lv.frame > 600 {
				delete(l.views, k)
			}
		}
	}

	quote := size * 0.9
	lineY := func(r ui.Rect, i int) float32 { return r.Y + editorPadY + f.g.top(i) - d.vscroll.Y }
	area.Draw(func(pt *ui.Painter, r ui.Rect) {
		d.liveAt, d.liveHits = ui.Point{X: r.X, Y: r.Y}, d.liveHits[:0]
		x := r.X + f.left
		// The line a jump landed on, lit, fading as the eye finds it.
		if since := pt.Now().Sub(d.landAt); !d.landAt.IsZero() && since >= 0 && since < landFor && d.landLine < snap.LineCount() {
			t := float32(since) / float32(landFor)
			y := lineY(r, d.landLine)
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: lineY(r, d.landLine+1) - y}, p.accent.Alpha(0.26*(1-t*t)), 0)
			pt.AnimationFrame()
		}
		if doc == nil {
			return
		}
		// An alert's lines are tinted its color, as Obsidian's callouts are,
		// one box from its first line to its last, where the caret is too.
		var tint liveDeco
		var tintFrom, tintTo int
		drawTint := func() {
			if tintTo > tintFrom {
				inset := float32(tint.depth-1) * quote
				y0, y1 := lineY(r, tintFrom), lineY(r, tintTo)
				pt.Fill(ui.Rect{X: x + inset, Y: y0, W: f.width - inset, H: y1 - y0}, a.markdownColor(tint.alert).Alpha(alertTint), 4)
			}
			tintFrom, tintTo = 0, 0
		}
		for _, dc := range doc.decos {
			if dc.kind != decoQuote || dc.end < from || dc.start > to {
				continue
			}
			l0, l1 := snap.LineAt(min(dc.start, n)), snap.LineAt(min(dc.end, n))
			if tintTo > tintFrom && (tintTo != l0 || tint.alert != dc.alert) {
				drawTint()
			}
			if dc.alert == alertNone {
				continue
			}
			if tintTo <= tintFrom {
				tint, tintFrom = dc, l0
			}
			tintTo = l1 + 1
		}
		drawTint()
		// The bars of quotes, one from the first line of a run of a quote's
		// lines to its last, rather than one a line.
		var bars liveDeco
		var barFrom, barTo int
		drawBars := func() {
			if barTo <= barFrom {
				return
			}
			y0, y1 := lineY(r, barFrom), lineY(r, barTo)
			for k := range bars.depth {
				bar := p.guideNow
				if bars.alert != alertNone && k == bars.depth-1 {
					bar = a.markdownColor(bars.alert)
				}
				pt.Fill(ui.Rect{X: x + float32(k)*quote + 1, Y: y0, W: 3, H: y1 - y0}, bar, 1.5)
			}
			barFrom, barTo = 0, 0
		}
		for _, dc := range doc.decos {
			if dc.end < from || dc.start > to {
				continue
			}
			l0, l1 := snap.LineAt(min(dc.start, n)), snap.LineAt(min(dc.end, n))
			y0, y1 := lineY(r, l0), lineY(r, l1+1)
			switch dc.kind {
			case decoCode:
				pad := size * 0.6
				pt.Fill(ui.Rect{X: x - pad, Y: y0, W: f.width + 2*pad, H: y1 - y0}, p.chrome, 8)
				pt.Stroke(ui.Rect{X: x - pad, Y: y0, W: f.width + 2*pad, H: y1 - y0}, p.border, 8, 1)
			case decoRule:
				if !onLines(dc.start, dc.end) {
					pt.Fill(ui.Rect{X: x, Y: (y0 + y1) / 2, W: f.width, H: 2}, p.border, 1)
				}
			case decoHeading:
				// Into the blank line after it, as far below its text as the
				// Reading view's, or along its foot.
				y := y1 - 1
				if l1+1 < snap.LineCount() && strings.TrimSpace(snap.Line(l1+1)) == "" {
					y = y1 + size*0.25
				}
				pt.Fill(ui.Rect{X: x, Y: y, W: f.width, H: 1}, p.border, 0)
			case decoQuote:
				// The bars of a line whose > shows are the >.
				if onLines(dc.start, dc.end) && l0 == l1 && liveQuoteMarks.MatchString(snap.Line(l0)) {
					drawBars()
					continue
				}
				if barTo > barFrom && (barTo != l0 || bars.depth != dc.depth || bars.alert != dc.alert) {
					drawBars()
				}
				if barTo <= barFrom {
					bars, barFrom = dc, l0
				}
				barTo = l1 + 1
			}
		}
		drawBars()
	})
	area.DrawObjects(func(pt *ui.Painter, at int, r ui.Rect) {
		o, ok := objects[at]
		if !ok {
			return
		}
		s := o.s
		switch s.obj {
		case objBullet, objRing:
			// The dot, or the mark it stands for while the caret is at it,
			// one fading into the other.
			k := markerIn(at, o.shown)
			if k > 0 && k < 1 {
				pt.AnimationFrame()
			}
			dot := size * 0.32
			box := ui.Rect{X: r.X + (r.W-dot)/2, Y: r.Y + r.H - size*0.36 - dot/2, W: dot, H: dot}
			if k < 1 {
				if s.obj == objRing {
					pt.Stroke(box, p.muted.Alpha(1-k), dot/2, 1.25)
				} else {
					pt.Fill(box, p.muted.Alpha(1-k), dot/2)
				}
			}
			if k > 0 {
				span := ui.Span{Text: snap.Slice(at, at+1), Font: family, Size: size, Color: faint.Alpha(k)}
				tw, th := pt.MeasureText(0, span)
				pt.RichText(r.X+(r.W-tw)/2, r.Y+r.H-th*0.78, 0, span)
			}
		case objTask, objTaskDone:
			// Where a bullet's dot is, in the room its source takes.
			b := size * 0.9
			box := ui.Rect{X: r.X + min((r.W-b)/2, size*0.1), Y: r.Y + (r.H-b)/2, W: b, H: b}
			// A box just checked fills in, its check growing from half its
			// size; one unchecked empties.
			k := float32(1)
			if since := pt.Now().Sub(d.liveTickAt); at == d.liveTicked && !d.liveTickAt.IsZero() && since >= 0 && since < motionShow {
				k = ui.EaseOut(float32(since) / float32(motionShow))
				pt.AnimationFrame()
			}
			if s.obj == objTaskDone {
				pt.Stroke(box, p.muted.Alpha(1-k), 3, 1.25)
				pt.Fill(box, p.fill.Alpha(k), 3)
				cw := b * 0.8 * (0.5 + 0.5*k)
				pt.Icon(iconCheck, ui.Rect{X: box.X + (b-cw)/2, Y: box.Y + (b-cw)/2, W: cw, H: cw}, p.fillText.Alpha(k))
			} else {
				pt.Fill(box, p.fill.Alpha(1-k), 3)
				pt.Stroke(box, p.muted, 3, 1.25)
			}
			d.liveHits = append(d.liveHits, liveHit{ui.Rect{X: box.X - d.liveAt.X, Y: box.Y - d.liveAt.Y, W: b, H: b}, at})
		case objImage:
			pt.Image(s.drawn.image.bitmap, r, ui.Contain)
		case objMath:
			s.drawn.formula.draw(pt, r, p.text)
		case objNote:
			// Raised to the top of its line's text.
			span := ui.Span{Text: liveNoteLabel(snap, s), Font: family, Size: size * liveNoteSize, Color: p.accent}
			_, th := pt.MeasureText(0, span)
			pt.RichText(r.X+size*0.05, r.Y+r.H-size*0.4-th*0.8, 0, span)
		case objGlyph:
			// In the middle of the columns it takes.
			span := ui.Span{Text: o.glyph, Font: codeFont, Size: size * 0.9, Color: p.text}
			tw, th := pt.MeasureText(0, span)
			pt.RichText(r.X+(r.W-tw)/2, r.Y+(r.H-th)/2, 0, span)
		case objAlert:
			// An alert with no title of its own is titled by its kind.
			icon, title := markdownAlertHead(s.alert)
			col := a.markdownColor(s.alert)
			is := size * 1.1
			pt.Icon(icon, ui.Rect{X: r.X, Y: r.Y + r.H - size*0.95, W: is, H: is}, col)
			if s.depth == 1 {
				break // its title follows
			}
			span := ui.Span{Text: title, Font: family, Size: size, Weight: 600, Color: col}
			_, th := pt.MeasureText(0, span)
			pt.RichText(r.X+is+size*0.45, r.Y+r.H-th*0.8, 0, span)
		}
	})
	area.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		// The lines changed since the last commit: a bar left of the text.
		marks := d.git.Lines
		barX := r.X + f.left - size*1.2
		for i := f.first; i <= min(f.last, len(marks)-1); i++ {
			y, h := lineY(r, i), lineY(r, i+1)-lineY(r, i)
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
		// vim's cursor, a block over the rune after it.
		if f.vimKeys {
			at := vimCursor(d, snap)
			x0, y0, h, ok := d.lines.Caret(at)
			if !ok {
				return
			}
			x1 := x0 + size*0.6
			if at < n && snap.Slice(at, at+1) != "\n" {
				if x, y, _, ok := d.lines.Caret(at + 1); ok && y == y0 && x > x0 {
					x1 = x
				}
			}
			box := ui.Rect{X: r.X + f.left + x0, Y: r.Y + editorPadY + y0 - d.vscroll.Y, W: x1 - x0, H: h}
			if d.vim.operatorPending() {
				box.Y, box.H = box.Y+h/2, h/2
			}
			if f.focused {
				pt.Fill(box, p.cursor.Alpha(0.6), 1)
			} else {
				pt.Stroke(box, p.cursor, 1, 1)
			}
		}
	})
	d.git.Refresh(d.path, snap, a.post)

	// The blocks rendered, over the room their lines keep, within the view.
	if len(blocks) == 0 {
		return
	}
	ui.Box(c.Key("live-blocks")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Clip().PassThrough().Children(func() {
		for _, s := range blocks {
			s := s
			lv := s.lv
			y := editorPadY + f.g.top(s.first) - d.vscroll.Y + size/2
			// Clipped to the room it has while that grows, its drawing
			// measured whole.
			room := ui.Box(c.Key("live-room:" + s.b.key)).Absolute().Top(y).Left(f.left).Width(f.width)
			if s.clip > 0 {
				room.Height(s.clip).Clip()
			}
			if s.fade < 1 {
				room.Opacity(s.fade)
			}
			room.Children(func() {
				box := ui.Column(c.Key("live-block:" + s.b.key)).Width(f.width).Bind(&lv.box)
				box.HandleInput(func(ev ui.InputEvent) bool {
					switch ev.Kind {
					case ui.InputScroll:
						// The text scrolls under the block as over its text.
						d.vscroll.Y = clamp(d.vscroll.Y+ev.DY, 0, d.vscroll.MaxY)
						a.invalidate()
						return true
					case ui.InputPointerDown:
						if ev.Button == 0 && ev.Clicks == 1 {
							// A click edits its source.
							at := s.b.start
							d.cmds = append(d.cmds, func(d *editor) { d.selectRange(at, at, false) })
							d.focusNext = true
							a.invalidate()
							return true
						}
					}
					return false
				})
				box.Children(func() {
					font := ui.Font{Family: c.Theme().Font, Size: size}
					for i := range lv.v.markdown.blocks {
						a.markdownRow(c, lv.v, i, f.width, font, nil)
					}
				})
			})
			lv.at = lv.box.Bounds(c)
			// Not the room the Reading view keeps below its last row: the
			// blank line after it is that room here.
			bottom, outside := a.markdownBelow(lv.v.markdown, len(lv.v.markdown.blocks)-1)
			if h := lv.at.H - bottom - outside; lv.at.H > 0 && h != lv.height {
				lv.height = h
				a.invalidate()
			}
		}
	})
}

// liveDrawn is an object a frame of Live Preview draws: its span, whether
// it shows as its source, as a bullet's mark does, and the rune it draws
// for objGlyph.
type liveDrawn struct {
	s     *liveSpan
	shown bool
	glyph string
}

// liveHang is how wide the white space s, which continues an item of a
// list on a line of its own, is to stand the line under the item's text,
// in text of font: as wide as the item's indent, its marker as it draws,
// and the space after it.
func (a *app) liveHang(l *liveState, snap ui.TextSnapshot, s *liveSpan, size float32, font ui.Font) float32 {
	if s.to <= s.from || s.to > snap.Len() {
		return 0
	}
	prefix := snap.Slice(s.from, s.to)
	m := liveListMarker.FindStringSubmatch(prefix)
	if m == nil {
		return 0
	}
	w := liveIndent*float32(s.look.hang-1)*size + l.width(prefix, font)
	ordered := m[2][0] >= '0' && m[2][0] <= '9'
	switch {
	case m[4] == "" && !ordered:
		w += size*1.1 - l.width(m[2], font) // a bullet's dot
	case m[4] != "" && ordered:
		w += max(0, size*1.1-l.width(m[4], font)) // its box
	}
	return w
}

// liveStrike is how the text of the task whose box is s shows: struck
// through and faint once done, from the frame its box is checked, which
// fades it there, or back.
func (a *app) liveStrike(d *editor, snap ui.TextSnapshot, s *liveSpan, now time.Time) (ui.TextRange, bool) {
	p := a.pal
	done := s.obj == objTaskDone
	k := float32(1)
	if since := now.Sub(d.liveTickAt); s.start == d.liveTicked && !d.liveTickAt.IsZero() && since >= 0 && since < motionShow {
		k = ui.EaseOut(float32(since) / float32(motionShow))
	}
	if !done {
		if k >= 1 {
			return ui.TextRange{}, false
		}
		k = 1 - k
	}
	n := min(s.end, snap.Len())
	_, end := snap.LineRange(snap.LineAt(n))
	line := snap.Slice(n, end)
	start := n + utf8.RuneCountInString(line) - utf8.RuneCountInString(strings.TrimLeft(line, " \t"))
	if start >= end {
		return ui.TextRange{}, false
	}
	return ui.TextRange{Start: start, End: end, Strikethrough: true, Color: p.text.Mix(p.faint, k), DecorationColor: p.faint.Alpha(k)}, true
}

// liveGlyphs are the ranges that draw the runes of lines first to last of
// a block's source as wide as the columns they take, those whose font
// draws them otherwise, as ⌘ or 中, so that a table's pipes line up. It
// notes them in objects to draw.
func (a *app) liveGlyphs(l *liveState, snap ui.TextSnapshot, first, last int, size float32, codeFont string, objects map[int]liveDrawn) []ui.TextRange {
	var ranges []ui.TextRange
	font := ui.Font{Family: codeFont, Size: size * 0.9}
	var cell float32
	for i := first; i <= last; i++ {
		at, _ := snap.LineRange(i)
		for _, r := range snap.Line(i) {
			if r >= utf8.RuneSelf && r != '\t' {
				if cell == 0 {
					cell = l.width("0", font)
				}
				cols := float32(1)
				if k := width.LookupRune(r).Kind(); k == width.EastAsianWide || k == width.EastAsianFullwidth {
					cols = 2
				}
				glyph := string(r)
				if w := cols * cell; max(l.width(glyph, font)-w, w-l.width(glyph, font)) > 0.5 {
					ranges = append(ranges, ui.TextRange{Start: at, End: at + 1, ObjectWidth: w, ObjectHeight: font.Size * 0.8, ObjectDescent: font.Size * 0.2})
					objects[at] = liveDrawn{s: &liveSpan{start: at, end: at + 1, kind: liveObject, obj: objGlyph}, glyph: glyph}
				}
			}
			at++
		}
	}
	return ranges
}

// liveRange is the range a span of style gives its runes, in text of
// size.
func (a *app) liveRange(s *liveSpan, size float32, codeFont string) ui.TextRange {
	p := a.pal
	look := s.look
	r := ui.TextRange{Start: s.start, End: s.end, Weight: look.weight, Italic: look.italic, Strikethrough: look.strike}
	if look.size > 0 && look.size != 1 {
		r.Size = size * look.size
	}
	if look.code {
		r.Family = codeFont
		if r.Size == 0 {
			r.Size = size * 0.9
		}
		if !look.block {
			r.Background, r.Radius = p.surfaceHover, 4
		}
	}
	if look.mark {
		r.Background, r.Radius = p.modified.Alpha(0.3), 3
	}
	if look.tag {
		r.Background, r.Radius = p.accent.Alpha(0.12), size*0.6
	}
	if look.indent > 0 {
		// An object that draws nothing, low enough to leave its line as
		// high as its text.
		r.ObjectWidth, r.ObjectHeight = size*look.indent, 1
	}
	switch look.color {
	case liveMuted:
		r.Color = p.muted
	case liveFaint:
		r.Color = p.faint
	case liveHeading:
		r.Color = p.syntax[highlight.Heading]
	case liveLink:
		r.Color = p.accent
		if !look.tag {
			// Underlined, faint, as Obsidian's links are.
			r.Underline, r.DecorationColor = true, p.accent.Alpha(0.4)
		}
	case liveUnresolved:
		r.Color = p.accent.Alpha(0.65)
		r.Underline, r.DecorationColor = true, p.accent.Alpha(0.2)
	case liveAlert:
		r.Color = a.markdownColor(look.alert)
	}
	if look.strike {
		r.DecorationColor = r.Color
	}
	return r
}

// liveObjectSize is the box of an object in text of size, in a column
// width wide: its width, its height above the baseline, and below it.
func (a *app) liveObjectSize(l *liveState, snap ui.TextSnapshot, s *liveSpan, size, width float32, family string) (w, h, below float32) {
	switch s.obj {
	case objBullet, objRing:
		return size * 1.1, size * 0.75, 0
	case objTask, objTaskDone:
		return size * 1.4, size * 0.95, size * 0.2
	case objQuote:
		return size * 0.9 * float32(max(1, int(s.depth))), size * 0.7, 0
	case objImage:
		iw, ih := s.drawn.image.w, s.drawn.image.h
		if iw <= 0 || ih <= 0 {
			return 1, 1, 0
		}
		w = iw
		if s.drawn.width > 0 {
			w = s.drawn.width
		}
		w = min(w, max(1, width))
		return w, w * ih / iw, 0
	case objMath:
		f := s.drawn.formula
		return f.w * size, f.ascent * size, f.depth * size
	case objNote:
		return l.width(liveNoteLabel(snap, s), ui.Font{Family: family, Size: size * liveNoteSize}) + size*0.15, size * 0.75, 0
	case objAlert:
		if s.depth == 1 {
			return size * 1.35, size * 0.95, size * 0.25 // its icon
		}
		_, title := markdownAlertHead(s.alert)
		tw, _ := ui.ShapeText(title, ui.Font{Family: family, Size: size, Weight: 600}, 0).Size()
		return size*1.55 + tw, size * 0.95, size * 0.25
	}
	return size, size, 0
}

// liveNoteSize is the scale of a footnote's reference to its text.
const liveNoteSize = 0.72

// liveNoteLabel is the label of the reference to a footnote s, [^label].
func liveNoteLabel(snap ui.TextSnapshot, s *liveSpan) string {
	if s.end > snap.Len() || s.end-s.start < 4 {
		return ""
	}
	return snap.Slice(s.start+2, s.end-1)
}

// toggleTask checks the task whose box is at rune at of an editor's text,
// or unchecks it, as one step of undo.
func (a *app) toggleTask(d *editor, at int) {
	snap := d.buf.Snapshot()
	if at < 0 || at+3 > snap.Len() {
		return
	}
	line := snap.LineAt(at)
	s, _ := snap.LineRange(line)
	box, done := liveTask(snap.Line(line))
	if box < 0 || s+box-1 != at {
		return
	}
	mark := "x"
	if done {
		mark = " "
	}
	d.edit(d.sel, textEdit{s + box, s + box + 1, mark})
	d.liveTicked, d.tickNow = at, true
}

// toggleReadingTask checks the task of block i of a Reading view, or
// unchecks it, in its text, and shows it so at once, before the text is
// parsed again.
func (a *app) toggleReadingTask(v *viewer, i int) {
	m := v.markdown
	d := a.docEditor(v.doc)
	if d == nil || i >= len(m.blocks) {
		return
	}
	b := &m.blocks[i]
	snap := d.buf.Snapshot()
	if b.line >= snap.LineCount() {
		return
	}
	s, _ := snap.LineRange(b.line)
	box, _ := liveTask(snap.Line(b.line))
	if box < 0 {
		return
	}
	a.toggleTask(d, s+box-1)
	if b.marker == "☐" {
		b.marker = "☑"
	} else {
		b.marker = "☐"
	}
}

// liveClick toggles the task whose box the pointer pressed in an editor
// of Live Preview at (x, y) in its text area, and reports whether it did.
func (a *app) liveClick(d *editor, x, y float32) bool {
	for _, h := range d.liveHits {
		r := h.r
		if x >= r.X-2 && x <= r.X+r.W+2 && y >= r.Y-2 && y <= r.Y+r.H+2 {
			at := h.at
			d.cmds = append(d.cmds, func(d *editor) { a.toggleTask(d, at) })
			// The editor takes the keyboard, as a click on its text does.
			d.focusNext = true
			a.invalidate()
			return true
		}
	}
	return false
}

// liveLinks are the links of a line of Markdown: [text](dest), <url> and
// bare URLs, the destination their first group.
var liveLinks = regexp.MustCompile(`!?\[[^\]\n]*\]\(<?([^)\s>]+)>?(?:\s+"[^"]*")?\)|<(https?://[^>\s]+)>|(https?://[^\s<>()]+)`)

// markdownLinkIn returns the destination of the link of a line of
// Markdown at rune at, as a Cmd-click there follows it.
func markdownLinkIn(line string, at int) (string, bool) {
	for _, m := range liveLinks.FindAllStringSubmatchIndex(line, -1) {
		s, e := utf8.RuneCountInString(line[:m[0]]), utf8.RuneCountInString(line[:m[1]])
		if at < s || at > e {
			continue
		}
		for g := 1; g < 4; g++ {
			if m[2*g] >= 0 {
				return line[m[2*g]:m[2*g+1]], true
			}
		}
	}
	return "", false
}

// followLiveLink opens the link of Markdown at the caret: a page in the
// browser, a file in Mayoi, or a heading of a note.
func (a *app) followLiveLink(d *editor) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	s, e := snap.LineRange(snap.LineAt(caret))
	dest, ok := markdownLinkIn(snap.Slice(s, e), caret-s)
	if !ok {
		return
	}
	u, err := url.Parse(dest)
	switch {
	case err == nil && (u.Scheme == "http" || u.Scheme == "https"):
		a.services.OpenURL(dest)
	case strings.HasPrefix(dest, "#"):
		a.openMarkdownAt(d.path, strings.TrimPrefix(dest, "#"))
	default:
		local := markdownPath(markdownRoot(d.path, a.folder()), d.path, dest)
		switch {
		case local == "":
		case err == nil && u.Fragment != "" && isMarkdownPath(local):
			a.openMarkdownAt(local, u.Fragment)
		default:
			a.openFile(local, true)
		}
	}
}

// modeFade fades an editor's page in as it switches between its source,
// Live Preview and the Reading view, which lay its text out anew: from
// faint, as the page it was gives way.
func (a *app) modeFade(c *ui.Context, page ui.Element, d *editor) {
	mode, now := markdownMode(d), c.Now()
	if mode != d.shownMode {
		if d.modeSeen {
			d.modeAt = now
		}
		d.shownMode, d.modeSeen = mode, true
	}
	if since := now.Sub(d.modeAt); !d.modeAt.IsZero() && since >= 0 && since < motionShow {
		page.Opacity(0.4 + 0.6*ui.EaseOut(float32(since)/float32(motionShow)))
		c.AnimationFrame()
	}
}
