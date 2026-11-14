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
	// rooms are how high the lines of callouts that fold showed below
	// their titles, by their keys, the room that glides as one folds.
	rooms map[string]float32
	// titles are the keys of the callouts that fold, by the runes their
	// lines start at, as the last frame saw them, by which one keeps
	// whether it is folded as its title is edited.
	titles map[int]string
	// pics are the pictures of the web drawn as cards on their way, by
	// where they are from.
	pics map[string]*livePic
	// colors are the colors of the code in view, of colorKey.
	colors   []ui.TextRange
	colorKey liveColorKey
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
// showed as, from, to what it shows as now, so that the text below it
// moves over rather than jumps: source is the height of its lines of
// source, drawn that of its drawing, with its margin, and blank that of
// the blank line after it, which takes the room the drawing gives up as
// it glides; with none, its last line grows, by grown in the last frame.
type liveIn struct {
	first, last int
	in          bool
	at          time.Time
	from        float32
	source      float32
	drawn       float32
	blank       float32
	grown       float32
}

// extra is how much higher than its source the block the caret went into
// shows at now: the room its drawing gave up, gliding away.
func (in *liveIn) extra(now time.Time, glide bool) float32 {
	since, gap := now.Sub(in.at), in.from-in.source
	if !in.in || !glide || gap <= 0 || since < 0 || since >= motionMove {
		return 0
	}
	return gap * (1 - ui.EaseInOut(float32(since)/float32(motionMove)))
}

// height is how high the block the caret left shows at now, gliding from
// what it showed as to want.
func (in *liveIn) height(now time.Time, want float32, glide bool) float32 {
	since := now.Sub(in.at)
	if in.in || !glide || in.from <= 0 || since < 0 || since >= motionMove {
		return want
	}
	return in.from + (want-in.from)*ui.EaseInOut(float32(since)/float32(motionMove))
}

// liveView is a block Live Preview shows rendered: its preview, where it
// showed, at, and how high, and the frame it showed in last. It shows
// once measured, fading in from shownAt.
type liveView struct {
	v      *viewer
	box    ui.Handle
	at     ui.Rect
	height float32
	frame  int
	// source is how high its lines of source showed last, 0 before they
	// did.
	source   float32
	measured bool
	shownAt  time.Time
}

// liveHit is a task's box the last frame drew, where it showed, and the
// rune of its [; or, with fold set, the chevron of a callout that folds,
// of key.
type liveHit struct {
	r    ui.Rect
	at   int
	fold byte
	key  string
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
	glide := !c.Preferences().ReduceMotion

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
	// blink: only their color, as their room shows at once. Those of the
	// lines they showed on already stay, as a selection drawn over more.
	now := c.Now()
	if lines := [2]int{lineFrom, lineTo}; lines != d.liveLines {
		if d.liveLines != [2]int{} || !d.liveRevealAt.IsZero() {
			d.liveRevealAt, d.liveLinesWas = now, d.liveLines
		}
		d.liveLines = lines
	}
	faint, fresh, fading := p.faint, p.faint, false
	if since := now.Sub(d.liveRevealAt); since >= 0 && since < motionHover {
		fresh, fading = p.faint.Alpha(ui.EaseOut(float32(since)/float32(motionHover))), true
		c.AnimationFrame()
	}
	markColor := func(s *liveSpan) ui.Color {
		if !fading {
			return faint
		}
		if i := snap.LineAt(min(s.start, n)); i >= d.liveLinesWas[0] && i <= d.liveLinesWas[1] {
			return faint
		}
		return fresh
	}
	if d.tickNow {
		d.tickNow, d.liveTickAt = false, now
	}
	codeFont, family := a.settings.FontFamily, c.Theme().Font
	textFont := ui.Font{Family: family, Size: size}
	// The ranges and objects of the last frame, cleared for this one's:
	// the area keeps a copy of the ranges, and the objects were drawn.
	ranges := d.liveRanges[:0]
	if d.liveObjects == nil {
		d.liveObjects = map[int]liveDrawn{}
	}
	objects := d.liveObjects
	clear(objects)
	var codes []liveDeco
	// The bullet whose mark shows first, as at+1, to fade between them.
	marker := 0
	// The link under the pointer, whose underline fills in.
	hover, link := a.liveHovered(d, area, doc, f)
	if hover != d.liveHover {
		d.liveHoverWas, d.liveHover, d.liveHoverAt = d.liveHover, hover, now
	}
	d.liveLink = link
	hoverIn := float32(1)
	if since := now.Sub(d.liveHoverAt); since >= 0 && since < motionHover {
		hoverIn = ui.EaseOut(float32(since) / float32(motionHover))
		c.AnimationFrame()
	}
	// A hand over a task's box, which a click checks, and over a link,
	// which a click opens, or a ⌘-click once its source shows.
	if !area.Hovered() {
		d.liveBox = 0
	}
	// ⌘ as the frame sees it, which draws again as it is pressed or let
	// go, the pointer still.
	if d.liveHover != 0 && (c.Modifiers() == ui.Cmd || !liveLinkShown(d.liveLink, selFrom, selTo)) || d.liveBox != 0 {
		area.Cursor(ui.CursorPointer)
	}
	if doc != nil {
		// The spans that reach the lines about the view.
		doc.visit(from, to, func(s *liveSpan) {
			switch s.kind {
			case liveStyle:
				r := a.liveRange(s, size, codeFont)
				if liveLinkLook(s.look) {
					switch s.start + 1 {
					case d.liveHover:
						r.DecorationColor = r.DecorationColor.Mix(r.Color, hoverIn)
					case d.liveHoverWas:
						r.DecorationColor = r.DecorationColor.Mix(r.Color, 1-hoverIn)
					}
				}
				if s.look.hang > 0 {
					if w := a.liveHang(l, snap, s, size, textFont); w > 0 {
						r.ObjectWidth, r.ObjectHeight = w/float32(s.end-s.start), 1
					}
				}
				ranges = append(ranges, r)
			case liveMark:
				if revealed(s) {
					ranges = append(ranges, ui.TextRange{Start: s.start, End: s.end, Color: markColor(s)})
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
					case objQuote, objExternal, objNoteDef:
						ranges = append(ranges, ui.TextRange{Start: s.start, End: s.end, Color: markColor(s)})
						return
					case objTask, objTaskDone:
						// The box of an ordered task, as its source, keeps the
						// room it drew in: the space after it widens by what
						// the box was wider.
						if s.from == s.start && s.end < n && snap.Slice(s.end, s.end+1) == " " {
							w := l.width(snap.Slice(s.start, s.end), textFont)
							if extra := size*1.1 - w; extra > 0.5 {
								ranges = append(ranges, ui.TextRange{Start: s.end, End: s.end + 1, ObjectWidth: l.width(" ", textFont) + extra, ObjectHeight: 1})
							}
						}
						return
					default:
						return
					}
				}
				w, h, dsc := a.liveObjectSize(l, snap, s, size, f.width, family)
				drawn := liveDrawn{s: s, shown: shown, fade: 1}
				switch {
				case s.obj == objNoteDef:
					// As wide as its source, which shows as the caret comes.
					w = l.width(snap.Slice(s.start, s.end), textFont)
				case task:
					// As wide as its source, which shows as the caret comes;
					// one of an ordered list, its box alone, no narrower than
					// a bullet.
					w = l.width(snap.Slice(s.from, s.end), textFont)
					if s.from == s.start {
						w = max(w, size*1.1)
					}
				case s.obj == objImage:
					// A picture of the web that arrived fades in over its
					// card, its room gliding from the card's.
					w, h, drawn.fade = l.pictureRoom(s.drawn, w, h, now, glide)
					if drawn.fade < 1 {
						c.AnimationFrame()
					}
				}
				ranges = append(ranges, ui.TextRange{Start: s.start, End: s.start + 1, ObjectWidth: w, ObjectHeight: h, ObjectDescent: dsc})
				if s.end > s.start+1 {
					ranges = append(ranges, ui.TextRange{Start: s.start + 1, End: s.end, Hidden: true})
				}
				objects[s.start] = drawn
			}
		})
		doc.eachDeco(from, to, func(dc liveDeco) {
			if dc.kind == decoCode {
				codes = append(codes, dc)
			}
		})
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
		ranges = append(ranges, l.codeColors(d.hl, p, snap, doc, codes, f.first, f.last)...)
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
	d.liveRanges = ranges

	// The blocks shown rendered, where the caret is not.
	type shown struct {
		b     *liveBlock
		lv    *liveView
		first int
		// clip is how high it shows while its room grows, 0 for all of it,
		// fade how far it faded in, and pad how much higher its source is
		// than its drawing, whose room keeps it.
		clip, fade, pad float32
		// inset is how far it stands in, in a list or a quote.
		inset float32
	}
	var blocks []shown
	// The callouts folded, but one the caret is in, which shows its
	// source: their lines below their titles take no room, which glides
	// away as one folds and back as it opens.
	var folds []liveFold
	if doc != nil {
		folds = a.liveFolds(c, d, l, doc, area, f, from, to, onLines)
	}
	hidden := func(line int) bool {
		for _, fd := range folds {
			if fd.hides && line > fd.title && line <= fd.last {
				return true
			}
		}
		return false
	}
	if doc != nil {
		area.TextGaps(a.liveGaps(doc, snap, from, to, hidden)...)
	}
	if doc != nil {
		if l.views == nil {
			l.views = map[string]*liveView{}
		}
		lineH := size * a.settings.LineHeight
		in := &l.in
		caretIn := false
		// sourceH is how high a block's lines show as source, as they did
		// last, and room how high it shows drawn: its drawing with its
		// margin, or its source if that is higher, so that the caret
		// going in moves nothing below it.
		sourceH := func(lv *liveView, first, last int) float32 {
			if lv != nil && lv.source > 0 {
				return lv.source
			}
			return float32(last-first+1) * f.g.lineH
		}
		room := func(lv *liveView, first, last int) float32 {
			return max(lv.height+size, sourceH(lv, first, last))
		}
		doc.eachBlock(from, to, func(b *liveBlock) {
			if len(b.blocks) == 0 {
				return
			}
			first, last := snap.LineAt(b.start), snap.LineAt(b.end)
			if hidden(first) {
				return
			}
			lv := l.views[b.key]
			if onLines(b.start, b.end) {
				if caretIn {
					return
				}
				caretIn = true
				if !in.in || in.first != first {
					// Gone in: the room its drawing gives up glides away
					// below its source, from where it was, as it may still
					// have been gliding out.
					drawn := in.drawn
					if lv != nil {
						drawn = room(lv, first, last)
					} else if in.first != first {
						drawn = 0
					}
					from := drawn
					if in.first == first && !in.in {
						from = in.height(now, drawn, glide)
					}
					*in = liveIn{first: first, in: true, at: now, from: from, source: sourceH(lv, first, last), drawn: drawn,
						blank: f.g.top(last+2) - f.g.top(last+1)}
				} else {
					// Its lines showed as source in the last frame, the last
					// as much higher as it was grown.
					in.source = f.g.top(last+1) - f.g.top(first) - in.grown
				}
				in.last, in.grown = last, 0
				if lv != nil {
					lv.source = in.source
				}
				area.TextRanges(a.liveGlyphs(l, snap, max(first, f.first-liveSlack), min(last, f.last+liveSlack), size, codeFont, objects)...)
				extra := in.extra(now, glide)
				if extra <= 0.5 {
					return
				}
				c.AnimationFrame()
				if last+1 < snap.LineCount() && liveBlank(snap, last+1) {
					area.TextBlocks(ui.TextBlock{First: last + 1, Last: last + 1, Height: in.blank + extra})
					return
				}
				// Text right below it: its last line grows as much, by its
				// last rune drawn standing on the baseline it had.
				ls, le := snap.LineRange(last)
				if le <= ls {
					return
				}
				glyph := snap.Slice(le-1, le)
				font := ui.Font{Family: codeFont, Size: size * 0.9}
				m := font.Metrics()
				lh := f.g.lineH
				base := (lh-m.Ascent-m.Descent)/2 + m.Ascent
				area.TextRanges(ui.TextRange{Start: le - 1, End: le, ObjectWidth: l.width(glyph, font), ObjectHeight: base, ObjectDescent: lh + extra - base})
				objects[le-1] = liveDrawn{s: &liveSpan{start: le - 1, end: le, kind: liveObject, obj: objGlyph}, glyph: glyph, room: lh}
				in.grown = extra
				return
			}
			if lv == nil {
				m := &markdownPreview{blocks: b.blocks, lines: strings.Count(b.source, "\n") + 1}
				lv = &liveView{v: &viewer{doc: d.document, markdown: m, loaded: true}}
				lv.height = float32(last-first+1) * lineH * 1.4
				if in.first == first && in.drawn > size {
					// Its source edited: as high as it drew before, until
					// it shows how high it draws now.
					lv.height, lv.measured = in.drawn-size, true
				}
				l.views[b.key] = lv
			} else if &lv.v.markdown.blocks[0] != &b.blocks[0] {
				// Drawn again, as in another theme's colors: the view shows
				// the new drawing, at the height it had.
				m := &markdownPreview{blocks: b.blocks, lines: lv.v.markdown.lines}
				lv.v = &viewer{doc: d.document, markdown: m, loaded: true}
			}
			lv.frame = a.frame
			inset := a.liveInset(l, snap, b, size, textFont)
			lv.v.markdown.measure = max(1, f.width-inset)
			height := room(lv, first, last)
			sh := shown{b: b, lv: lv, first: first, fade: 1, pad: height - lv.height - size, inset: inset}
			// One shown first fades in once measured, until then not there.
			if !lv.measured {
				sh.fade = 0
			} else if since := now.Sub(lv.shownAt); since >= 0 && since < motionShow {
				sh.fade = ui.EaseOut(float32(since) / float32(motionShow))
				c.AnimationFrame()
			}
			if in.first == first {
				if in.in {
					// Left: its room glides from what it showed as to its
					// drawing's, and the drawing fades in over it.
					in.in, in.from, in.at = false, in.source+in.extra(now, glide), now
				}
				since := now.Sub(in.at)
				if since >= 0 && since < motionShow {
					sh.fade = min(sh.fade, ui.EaseOut(float32(since)/float32(motionShow)))
					c.AnimationFrame()
				}
				if h := in.height(now, height, glide); h != height {
					height = h
					sh.clip = max(1, height-size-sh.pad)
					c.AnimationFrame()
				}
			}
			area.TextBlocks(ui.TextBlock{First: first, Last: last, Height: height})
			blocks = append(blocks, sh)
		})
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
		// The chevrons of the callouts that fold, after their titles.
		for _, fd := range folds {
			if hidden(fd.title) {
				continue
			}
			cs := size * 1.1
			cx := r.X + f.left + f.width - cs
			cy := lineY(r, fd.title) + (lineY(r, fd.title+1)-lineY(r, fd.title)-cs)/2
			if x0, _, _, ok := d.lines.Caret(fd.end); ok {
				cx = min(cx, r.X+f.left+x0+size*0.35)
			}
			box := ui.Rect{X: cx, Y: cy, W: cs, H: cs}
			turn, turning := calloutTurn(d.document, fd.key, fd.folded, pt.Now())
			if turning {
				pt.AnimationFrame()
			}
			drawChevron(pt, box, turn, a.markdownColor(fd.alert))
			d.liveHits = append(d.liveHits, liveHit{r: ui.Rect{X: box.X - d.liveAt.X - 3, Y: box.Y - d.liveAt.Y - 3, W: cs + 6, H: cs + 6}, fold: fd.fold, key: fd.key})
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
		doc.eachDeco(from, to, func(dc liveDeco) {
			if dc.kind != decoQuote {
				return
			}
			l0, l1 := snap.LineAt(min(dc.start, n)), snap.LineAt(min(dc.end, n))
			if tintTo > tintFrom && (tintTo != l0 || tint.alert != dc.alert) {
				drawTint()
			}
			if dc.alert == alertNone {
				return
			}
			if tintTo <= tintFrom {
				tint, tintFrom = dc, l0
			}
			tintTo = l1 + 1
		})
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
		doc.eachDeco(from, to, func(dc liveDeco) {
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
				if !dc.ruled {
					return
				}
				// Into the blank line after it, as far below its text as the
				// Reading view's, or along its foot.
				y := y1 - 1
				if liveBlank(snap, l1+1) {
					y = y1 + size*0.25
				}
				pt.Fill(ui.Rect{X: x, Y: y, W: f.width, H: 1}, p.border, 0)
			case decoQuote:
				// The bars of a line whose > shows are the >.
				if onLines(dc.start, dc.end) && l0 == l1 && liveQuoteMarks.MatchString(snap.Line(l0)) {
					drawBars()
					return
				}
				if barTo > barFrom && (barTo != l0 || bars.depth != dc.depth || bars.alert != dc.alert) {
					drawBars()
				}
				if barTo <= barFrom {
					bars, barFrom = dc, l0
				}
				barTo = l1 + 1
			}
		})
		drawBars()
		// The markers of the items whose text is a note embedded, by its
		// title, as a bullet stands by its item's text.
		for _, sb := range blocks {
			if sb.b.bullet {
				a.liveEmbedMarker(pt, snap, sb.b, ui.Rect{X: x, Y: lineY(r, sb.first) + size/2}, size, family, sb.fade)
			}
		}
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
			// size, or only fading with Reduce Motion; one unchecked
			// empties. The pointer over it darkens its edge.
			k := float32(1)
			if since := pt.Now().Sub(d.liveTickAt); at == d.liveTicked && !d.liveTickAt.IsZero() && since >= 0 && since < motionShow {
				k = ui.EaseOut(float32(since) / float32(motionShow))
				pt.AnimationFrame()
			}
			edge := p.muted
			if d.liveBox == at+1 {
				edge = p.text
			}
			if s.obj == objTaskDone {
				pt.Stroke(box, edge.Alpha(1-k), 3, 1.25)
				pt.Fill(box, p.fill.Alpha(k), 3)
				cw := b * 0.8
				if glide {
					cw *= 0.5 + 0.5*k
				}
				pt.Icon(iconCheck, ui.Rect{X: box.X + (b-cw)/2, Y: box.Y + (b-cw)/2, W: cw, H: cw}, p.fillText.Alpha(k))
			} else {
				pt.Fill(box, p.fill.Alpha(1-k), 3)
				pt.Stroke(box, edge, 3, 1.25)
			}
			d.liveHits = append(d.liveHits, liveHit{r: ui.Rect{X: box.X - d.liveAt.X, Y: box.Y - d.liveAt.Y, W: b, H: b}, at: at})
		case objImage:
			// A picture on its way is a card at its place, which fades
			// away over the picture once it arrived.
			if !s.drawn.loading {
				pt.Image(s.drawn.image.bitmap, r, ui.Contain)
			}
			if o.fade < 1 {
				k := 1 - o.fade
				pt.Fill(r, p.chrome.Alpha(k), 6)
				pt.Stroke(r, p.border.Alpha(k), 6, 1)
				is := min(size*1.5, r.H/2)
				pt.Icon(iconImage, ui.Rect{X: r.X + (r.W-is)/2, Y: r.Y + (r.H-is)/2, W: is, H: is}, p.faint.Alpha(k))
			}
		case objMath:
			s.drawn.formula.draw(pt, r, p.text)
		case objNote:
			// Raised to the top of its line's text.
			span := ui.Span{Text: liveNoteLabel(snap, s), Font: family, Size: size * liveNoteSize, Color: p.accent}
			_, th := pt.MeasureText(0, span)
			pt.RichText(r.X+size*0.05, r.Y+r.H-size*0.4-th*0.8, 0, span)
		case objNoteDef:
			// Its number, as the Reading view's list of footnotes, which
			// stands in from the text: by the text, in the room its source
			// takes.
			span := ui.Span{Text: s.drawn.note, Font: family, Size: size, Color: p.muted}
			tw, th := pt.MeasureText(0, span)
			pt.RichText(r.X+max(0, r.W-tw-size*0.4), r.Y+r.H-th*0.8, 0, span)
		case objExternal:
			// Small, after the link's text, as Obsidian draws it.
			is := size * 0.68
			pt.Icon(iconExternal, ui.Rect{X: r.X + size*0.12, Y: r.Y + r.H - size*0.78, W: is, H: is}, p.accent.Alpha(0.7))
		case objGlyph:
			// In the middle of the columns it takes, and of its line's room
			// when it holds the line higher.
			h := r.H
			if o.room > 0 {
				h = o.room
			}
			span := ui.Span{Text: o.glyph, Font: codeFont, Size: size * 0.9, Color: p.text}
			tw, th := pt.MeasureText(0, span)
			pt.RichText(r.X+(r.W-tw)/2, r.Y+(h-th)/2, 0, span)
		case objAlert:
			// An alert with no title of its own is titled by its kind.
			icon, title := markdownAlertHead(s.alert, liveAlertWord(snap, s))
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
			y := editorPadY + f.g.top(s.first) - d.vscroll.Y + size/2 + s.pad/2
			// Clipped to the room it has while that grows, its drawing
			// measured whole.
			room := ui.Box(c.Key("live-room:" + s.b.key)).Absolute().Top(y).Left(f.left + s.inset).Width(max(1, f.width-s.inset))
			if s.clip > 0 {
				room.Height(s.clip).Clip()
			}
			if s.fade < 1 {
				room.Opacity(s.fade)
			}
			room.Children(func() {
				box := ui.Column(c.Key("live-block:" + s.b.key)).Width(max(1, f.width-s.inset)).Bind(&lv.box)
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
						a.markdownRow(c, lv.v, i, max(1, f.width-s.inset), font, nil)
					}
				})
			})
			lv.at = lv.box.Bounds(c)
			// Not the room the Reading view keeps below its last row: the
			// blank line after it is that room here.
			bottom, outside := a.markdownBelow(lv.v.markdown, len(lv.v.markdown.blocks)-1)
			if h := lv.at.H - bottom - outside; lv.at.H > 0 && (h != lv.height || !lv.measured) {
				if !lv.measured {
					lv.measured, lv.shownAt = true, c.Now()
				}
				lv.height = h
				a.invalidate()
			}
		}
	})
}

// liveGaps is the room above the headings of the lines from up to to, as
// the Reading view leaves above them, but for one on the text's first
// line: the room is there wherever the caret is, so a heading's line
// keeps its place as the caret comes to it.
func (a *app) liveGaps(doc *liveDoc, snap ui.TextSnapshot, from, to int, hidden func(int) bool) []ui.TextGap {
	n := snap.Len()
	var gaps []ui.TextGap
	doc.eachDeco(from, to, func(dc liveDeco) {
		if dc.kind != decoHeading {
			return
		}
		if line := snap.LineAt(min(dc.start, n)); line > 0 && !hidden(line) {
			gaps = append(gaps, ui.TextGap{Para: line, Height: a.settings.FontSize * liveGap(dc.depth)})
		}
	})
	return gaps
}

// liveGap is the room above a heading of level, in the text's size: more
// above the larger.
func liveGap(level int) float32 {
	switch {
	case level <= 2:
		return 0.9
	case level == 3:
		return 0.65
	}
	return 0.45
}

// liveInset is how far a note embedded, b, stands in from the text's
// left, in text of size in font: past the quotes and the markers of the
// lists it is in, where the text of its item stands.
func (a *app) liveInset(l *liveState, snap ui.TextSnapshot, b *liveBlock, size float32, font ui.Font) float32 {
	if !b.embed {
		return 0
	}
	w := float32(b.quote) * size * 0.9
	if b.list > 0 {
		w += a.liveHang(l, snap, &liveSpan{from: b.mark, to: b.text, look: liveLook{hang: b.list}}, size, font)
	}
	return w
}

// liveEmbedMarker paints the marker of the item a note embedded, b, is
// the text of, by the note's title, which stands at at: a bullet's dot,
// a ring in a list in another, or an ordered list's number, faded in by
// fade.
func (a *app) liveEmbedMarker(pt *ui.Painter, snap ui.TextSnapshot, b *liveBlock, at ui.Rect, size float32, family string, fade float32) {
	p := a.pal
	if b.text > snap.Len() || b.mark >= b.text {
		return
	}
	marker := strings.TrimSpace(snap.Slice(b.mark, b.text))
	x := at.X + float32(b.quote)*size*0.9 + liveIndent*float32(b.list-1)*size
	// The middle of the title's row, as the Reading view draws it.
	cy := at.Y + size*a.settings.LineHeight*0.45
	if marker != "" && marker[0] >= '0' && marker[0] <= '9' {
		span := ui.Span{Text: marker, Font: family, Size: size, Color: p.muted.Alpha(fade)}
		_, th := pt.MeasureText(0, span)
		pt.RichText(x, cy-th/2, 0, span)
		return
	}
	dot := size * 0.32
	box := ui.Rect{X: x + (size*1.1-dot)/2, Y: cy - dot/2, W: dot, H: dot}
	if b.list > 1 {
		pt.Stroke(box, p.muted.Alpha(fade), dot/2, 1.25)
	} else {
		pt.Fill(box, p.muted.Alpha(fade), dot/2)
	}
}

// liveLinkLook reports whether a span of look is the text of a link,
// underlined.
func liveLinkLook(look liveLook) bool {
	return !look.tag && (look.color == liveLink || look.color == liveUnresolved)
}

// liveHovered is the link of Live Preview the pointer is over in an
// editor's text area, as the rune its text starts at plus one, 0 for none
// or while the pointer selects, and the runes of the whole link, its
// marks with its text, which a click opens while they are hidden.
func (a *app) liveHovered(d *editor, area ui.Element, doc *liveDoc, f liveFrame) (int, [2]int) {
	px, py, over := area.PointerPosition()
	if !over || doc == nil || f.g.lines == nil || d.selecting {
		return 0, [2]int{}
	}
	x, y := px-f.left, py+d.vscroll.Y-editorPadY
	line := f.g.at(y)
	ls, le := f.snap.LineRange(line)
	hover, link := 0, [2]int{}
	doc.visit(ls, le, func(s *liveSpan) {
		if hover != 0 || s.kind != liveStyle || !liveLinkLook(s.look) || s.start < ls || s.end > le {
			return
		}
		x0, y0, h0, ok := d.lines.Caret(s.start)
		x1, y1, h1, ok1 := d.lines.Caret(s.end)
		if !ok || !ok1 {
			return
		}
		var in bool
		if y0 == y1 {
			in = y >= y0 && y < y0+h0 && x >= x0 && x <= x1
		} else {
			// Wrapped: from its start to the end of its first row, the rows
			// between, and its last row up to its end.
			in = y >= y0 && y < y0+h0 && x >= x0 || y >= y0+h0 && y < y1 || y >= y1 && y < y1+h1 && x <= x1
		}
		if in {
			hover, link = s.start+1, [2]int{s.start, s.end}
		}
	})
	if hover != 0 {
		// The marks about its text are of the whole link; a bare URL has
		// none, its text the whole of it.
		text := link
		doc.visit(ls, le, func(s *liveSpan) {
			if s.kind == liveMark && !s.byLine && (s.end == text[0] || s.start == text[1]) && s.from <= text[0] && s.to >= text[1] {
				link = [2]int{s.from, s.to}
			}
		})
	}
	return hover, link
}

// liveDrawn is an object a frame of Live Preview draws: its span, whether
// it shows as its source, as a bullet's mark does, how far a picture
// faded in, and the rune it draws for objGlyph, in the middle of the top
// room of its box when that is set.
type liveDrawn struct {
	s     *liveSpan
	shown bool
	fade  float32
	glyph string
	room  float32
}

// livePic is a picture of the web that Live Preview drew a card for, w
// by h, while it was on its way, and when it arrived, zero until then.
type livePic struct {
	at   time.Time
	w, h float32
}

// pictureRoom is the room of a picture w by h at now, and how far it faded
// in: a card's for one on its way, then one gliding from the card's to its
// own, the picture fading in over it.
func (l *liveState) pictureRoom(p *livePicture, w, h float32, now time.Time, glide bool) (float32, float32, float32) {
	pic := l.pics[p.dest]
	if p.loading {
		if pic == nil {
			if l.pics == nil || len(l.pics) >= 64 {
				l.pics = map[string]*livePic{}
			}
			pic = &livePic{}
			l.pics[p.dest] = pic
		}
		pic.w, pic.h = w, h
		return w, h, 0
	}
	if pic == nil {
		return w, h, 1
	}
	if pic.at.IsZero() {
		pic.at = now
	}
	since := now.Sub(pic.at)
	if since < 0 || since >= motionMove {
		delete(l.pics, p.dest)
		return w, h, 1
	}
	fade := float32(1)
	if since < motionShow {
		fade = ui.EaseOut(float32(since) / float32(motionShow))
	}
	if glide {
		k := ui.EaseInOut(float32(since) / float32(motionMove))
		w, h = pic.w+(w-pic.w)*k, pic.h+(h-pic.h)*k
	}
	return w, h, fade
}

// liveColorKey is what the colors of the code in view are of.
type liveColorKey struct {
	version     uint64
	gen         uint64
	first, last int
	pal         *palette
	doc         *liveDoc
	hl          *highlight.Highlighter
}

// codeColors are the colors of the code of the blocks of code codes, in
// their language, on lines first to last: not their fences, whose
// language reads faint and whose marks hide.
func (l *liveState) codeColors(hl *highlight.Highlighter, p *palette, snap ui.TextSnapshot, doc *liveDoc, codes []liveDeco, first, last int) []ui.TextRange {
	k := liveColorKey{snap.Version(), hl.Gen(), first, last, p, doc, hl}
	if l.colors != nil && l.colorKey == k {
		return l.colors
	}
	out := l.colors[:0]
	n := snap.Len()
	for _, dc := range codes {
		l0, l1 := snap.LineAt(min(dc.start, n)), snap.LineAt(min(dc.end, n))
		if dc.fenced {
			l0++
			s, e := snap.LineRange(l1)
			if t := strings.TrimSpace(snap.Slice(s, min(e, s+64))); l1 >= l0 && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")) {
				l1--
			}
		}
		for i := max(l0, first); i <= min(l1, last); i++ {
			s, e := snap.LineRange(i)
			if e-s > highlight.MaxLineRunes {
				continue
			}
			for _, tok := range hl.Line(i, snap.Slice(s, e)) {
				if col := p.syntax[tok.Kind]; col != p.text {
					out = append(out, ui.TextRange{Start: s + tok.Start, End: s + tok.End, Color: col})
				}
			}
		}
	}
	if out == nil {
		out = []ui.TextRange{}
	}
	l.colors, l.colorKey = out, k
	return out
}

// liveBlank reports whether line i of snap is there and blank.
func liveBlank(snap ui.TextSnapshot, i int) bool {
	if i < 0 || i >= snap.LineCount() {
		return false
	}
	s, e := snap.LineRange(i)
	return e-s <= 64 && strings.TrimSpace(snap.Slice(s, e)) == ""
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
	start := n
	for start < end {
		if r := snap.Slice(start, start+1); r != " " && r != "\t" {
			break
		}
		start++
	}
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
	case liveNote:
		r.Color = p.accent
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
		if s.drawn.loading {
			// A card for a picture on its way, as wide as one asked for,
			// else a modest one, of a photo's proportions.
			w = size * 18
			if s.drawn.width > 0 {
				w = s.drawn.width
			}
			w = min(w, max(1, width))
			return w, w * 9 / 16, 0
		}
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
	case objNoteDef:
		return size, size * 0.75, 0
	case objExternal:
		return size * 0.9, size * 0.75, 0
	case objNote:
		return l.width(liveNoteLabel(snap, s), ui.Font{Family: family, Size: size * liveNoteSize}) + size*0.15, size * 0.75, 0
	case objAlert:
		if s.depth == 1 {
			return size * 1.35, size * 0.95, size * 0.25 // its icon
		}
		_, title := markdownAlertHead(s.alert, liveAlertWord(snap, s))
		tw, _ := ui.ShapeText(title, ui.Font{Family: family, Size: size, Weight: 600}, 0).Size()
		return size*1.55 + tw, size * 0.95, size * 0.25
	}
	return size, size, 0
}

// liveNoteSize is the scale of a footnote's reference to its text.
const liveNoteSize = 0.72

// liveNoteLabel is how the reference to a footnote s, [^label], reads:
// the footnote's number, as the Reading view numbers it, or its label.
func liveNoteLabel(snap ui.TextSnapshot, s *liveSpan) string {
	if s.drawn != nil && s.drawn.note != "" {
		return s.drawn.note
	}
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
			if h.fold != 0 {
				// A callout's chevron folds it, or opens it, the caret
				// where it was.
				a.toggleCallout(d.document, h.key, h.fold)
				return true
			}
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

// livePointer notes the task's box the pointer moved over in an editor of
// Live Preview, for the next frame to show; a drag selecting shows none.
func (a *app) livePointer(d *editor, ev ui.InputEvent) {
	box := 0
	for _, h := range d.liveHits {
		if r := h.r; ev.Button < 0 && h.key == "" && ev.X >= r.X-2 && ev.X <= r.X+r.W+2 && ev.Y >= r.Y-2 && ev.Y <= r.Y+r.H+2 {
			box = h.at + 1
		}
	}
	if box != d.liveBox {
		d.liveBox = box
		a.invalidate()
	}
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
	if dest, ok := markdownLinkIn(snap.Slice(s, e), caret-s); ok {
		a.openLiveDest(d, dest)
	}
}

// liveLinkShown reports whether a link of Live Preview, the runes
// link, shows its source, as the caret or the selection from to touches it.
func liveLinkShown(link [2]int, from, to int) bool {
	return from <= link[1] && to >= link[0]
}

// liveLinkPressed is the link of Live Preview under the pointer that a
// click opens, as in Obsidian: one whose source is hidden, with or without
// ⌘. A click on one whose source shows puts the caret in it.
func (a *app) liveLinkPressed(d *editor) ([2]int, bool) {
	link := d.liveLink
	if d.liveHover == 0 || link[0] >= link[1] {
		return link, false
	}
	n := d.buf.Snapshot().Len()
	if link[1] > n || liveLinkShown(link, min(d.sel[0], d.sel[1], n), min(max(d.sel[0], d.sel[1]), n)) {
		return link, false
	}
	return link, true
}

// openLiveLink opens the link of Live Preview at the runes link: a wiki
// link, a link of Markdown or a bare URL.
func (a *app) openLiveLink(d *editor, link [2]int) {
	snap := d.buf.Snapshot()
	if link[1] > snap.Len() {
		return
	}
	text := snap.Slice(link[0], link[1])
	if l, size := wikiLinkAt(text); size > 0 {
		a.openWikiLink(d.path, l)
	} else if dest, ok := markdownLinkIn(text, 0); ok {
		a.openLiveDest(d, dest)
	}
}

// openLiveDest opens the destination of a link of Markdown in an
// editor's note: a page in the browser, a file in Mayoi, or a heading of
// a note.
func (a *app) openLiveDest(d *editor, dest string) {
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

// liveFold is a callout that folds, shown in a frame of Live Preview: its
// title's line, the rune its title ends at, its last line, its key and
// alert, whether it shows folded and whether its lines below its title
// take no room, or less than their own, as it folds.
type liveFold struct {
	title, end, last int
	key              string
	fold             byte
	alert            markdownAlert
	folded, hides    bool
}

// liveFolds are the callouts that fold about the view, but those the caret
// is in, which show their source; it gives the area the room the lines of
// each below its title take, none folded, and less as it glides.
func (a *app) liveFolds(c *ui.Context, d *editor, l *liveState, doc *liveDoc, area ui.Element, f liveFrame, from, to int, onLines func(int, int) bool) []liveFold {
	snap := f.snap
	n := snap.Len()
	now := c.Now()
	var folds []liveFold
	for _, co := range doc.callouts {
		if co.end < from || co.start > to || co.start >= n {
			continue
		}
		title, last := snap.LineAt(co.start), snap.LineAt(min(co.end, n))
		key := liveCalloutKey(snap, title)
		alert, _ := markdownAlertMarker(key)
		if alert == alertNone || markdownAlertFold(key) == 0 {
			continue // edited since the parse
		}
		liveRetitle(d.document, l, co.start, key)
		if onLines(co.start, co.end) {
			continue
		}
		_, end := snap.LineRange(title)
		fd := liveFold{title: title, end: end, last: last, key: key, fold: co.fold, alert: alert}
		fd.folded = calloutFolded(d.document, key, co.fold)
		if last <= title {
			folds = append(folds, fd)
			continue
		}
		since := calloutSince(d.document, now)
		glide := key == d.document.foldKey && since >= 0 && since < motionMove && !c.Preferences().ReduceMotion
		room, ok := l.rooms[key]
		if !ok {
			room = float32(last-title) * f.g.lineH
		}
		height := float32(0)
		switch {
		case glide:
			k := ui.EaseInOut(float32(since) / float32(motionMove))
			if fd.folded {
				k = 1 - k
			}
			height = room * k
			c.AnimationFrame()
		case !fd.folded:
			// Open: its lines show, as high as they lay out.
			if l.rooms == nil || len(l.rooms) > 256 {
				l.rooms = map[string]float32{}
			}
			if h := f.g.top(last+1) - f.g.top(title+1); h > 0 {
				l.rooms[key] = h
			}
			folds = append(folds, fd)
			continue
		}
		fd.hides = true
		area.TextBlocks(ui.TextBlock{First: title + 1, Last: last, Height: height})
		folds = append(folds, fd)
	}
	return folds
}

// liveRetitle notes that the callout whose line starts at rune start has
// the key key, and if its title was edited since the last frame, moves
// whether it is folded from its old key to it: unless its - or + changed,
// which says anew how it shows.
func liveRetitle(doc *document, l *liveState, start int, key string) {
	was, ok := l.titles[start]
	if ok && was != key && markdownAlertFold(was) == markdownAlertFold(key) {
		if folded, set := doc.folds[was]; set {
			if _, taken := doc.folds[key]; !taken {
				doc.folds[key] = folded
				delete(doc.folds, was)
				if doc.foldKey == was {
					doc.foldKey = key
				}
			}
		}
	}
	if !ok || was != key {
		if l.titles == nil || len(l.titles) > 256 {
			l.titles = map[int]string{}
		}
		l.titles[start] = key
	}
}

// liveCalloutKey is the marker line of the callout whose title is line,
// as its document keeps whether it is folded: the line after its quotes'
// >, trimmed.
func liveCalloutKey(snap ui.TextSnapshot, line int) string {
	text := snap.Line(line)
	return strings.TrimSpace(text[len(liveQuoteMarks.FindString(text)):])
}

// liveAlertWord is the type of the callout whose marker is the object s,
// as typed.
func liveAlertWord(snap ui.TextSnapshot, s *liveSpan) string {
	if s.end > snap.Len() {
		return ""
	}
	word, _ := markdownAlertType(snap.Slice(s.start, s.end))
	return word
}
