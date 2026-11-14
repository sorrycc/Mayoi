package main

import (
	"bytes"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"mayoi/internal/vfs"

	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Live Preview shows the text of a Markdown file as it reads, as
// Obsidian's does: the text is the source, edited as it always is, but
// its marks hide away from the caret, its headings are large, its
// emphasis and code styled, its pictures and formulas drawn in place, and
// its tables and diagrams rendered over the lines they take. The marks of
// what the caret or the selection touches show, faint, to be edited.
//
// The text is parsed in the background, a moment after each edit, into
// spans of runes. Until the next parse arrives, the spans of the last
// follow the edits made since (liveDoc.mapTo), so that typing never lands
// in a mark that is hidden.

// liveKind is what a span of Live Preview does to its runes.
type liveKind uint8

const (
	// liveStyle styles its runes, wherever the caret is.
	liveStyle liveKind = iota
	// liveMark is a mark of Markdown, as ** or the # of a heading: hidden,
	// unless revealed, then faint.
	liveMark
	// liveObject draws its first rune as an object, a bullet, a task's box,
	// a picture or a formula, and hides the rest, unless revealed.
	liveObject
)

// liveColor is the color of a span's text, by its role, which the theme
// gives as the frame paints.
type liveColor uint8

const (
	liveText liveColor = iota
	liveMuted
	liveFaint
	liveHeading
	liveLink
	liveAlert
	// liveUnresolved is a link to a note there is not, as Obsidian shows
	// one: the link's color, fainter.
	liveUnresolved
	// liveNote is the reference to a footnote, in the link's color, but
	// not underlined, as it draws raised.
	liveNote
)

// liveLook is how a span styles its runes.
type liveLook struct {
	// size is a scale of the text's size, 0 for its own.
	size   float32
	weight int
	italic bool
	strike bool
	code   bool
	// block is code of a block, on its card rather than a chip.
	block bool
	// mark is ==highlighted== text, on a marker's yellow.
	mark bool
	// tag is a #tag, on a pill of the link's color.
	tag   bool
	color liveColor
	// alert is the kind of the alert whose color an alert's title takes.
	alert markdownAlert
	// indent widens each rune, the white space before an item of a list
	// in another, to as many times the text's size, for the item to stand
	// under its parent's text, as in the Reading view.
	indent float32
	// hang is the depth of the list whose item a line of white space
	// continues, which widens to stand the line under the item's text.
	hang uint8
}

// liveObj is what a liveObject draws.
type liveObj uint8

const (
	objBullet liveObj = iota
	objRing
	objTask
	objTaskDone
	objImage
	objMath
	// objQuote is the room a quote's > takes, where its bar is painted.
	objQuote
	// objAlert is the marker of an alert with no title, as [!NOTE],
	// drawn as its icon and its kind.
	objAlert
	// objNote is a reference to a footnote, [^1], drawn as its label
	// raised, as a superscript.
	objNote
	// objGlyph is a rune of a block's source drawn as wide as the columns
	// it takes, so that a table's pipes line up.
	objGlyph
	// objExternal is the ) of a link to the web, drawn as an icon.
	objExternal
	// objNoteDef is the label of a footnote's definition, [^1]: , drawn
	// as its number, as the Reading view numbers its footnotes, in the
	// room of its source.
	objNoteDef
)

// liveSpan is a run of runes that Live Preview styles, hides or draws.
// A parse of a long note has some hundreds of thousands, so it keeps
// what only some draw, a picture or a formula, apart.
type liveSpan struct {
	start, end int
	// from and to are the element the span is of, whose marks show while
	// the caret or the selection touches it, or, with byLine, while the
	// caret is on its lines.
	from, to int
	look     liveLook
	// drawn is the picture or the formula of objImage and objMath.
	drawn  *livePicture
	kind   liveKind
	byLine bool
	obj    liveObj
	// depth is how many quotes the > of objQuote are of, and alert the
	// kind of objAlert, whose depth is 1 for an alert with a title of its
	// own, drawn as its icon alone.
	depth uint8
	alert markdownAlert
}

// livePicture is what objImage, objMath and objNote draw: a picture's
// bitmap and size, where it is from, and the width its wiki link asked
// for, 0 for its own; a formula; or the number of a footnote, as the
// Reading view numbers it. A picture of the web still on its way is
// loading, with no bitmap.
type livePicture struct {
	image   markdownInline
	dest    string
	width   float32
	formula *mathFormula
	note    string
	loading bool
}

// liveDecoKind is what Live Preview paints behind a run of lines.
type liveDecoKind uint8

const (
	decoQuote liveDecoKind = iota
	decoCode
	decoRule
	// decoHeading is a heading's line, of the level depth, with room
	// above it, and the hairline under it, ruled, of the first two levels,
	// as the Reading view draws it.
	decoHeading
)

// liveDeco is a run of lines with something painted behind them: a quote's
// bar, a block of code's card, a rule.
type liveDeco struct {
	start, end int
	kind       liveDecoKind
	depth      int
	alert      markdownAlert
	// fenced is set for a block of code between fences, whose first line
	// is its fence and its language.
	fenced bool
	// ruled is set for a heading with a hairline under it.
	ruled bool
}

// liveBlock is a run of lines that Live Preview shows rendered in their
// place while the caret is elsewhere: a table, a diagram, a formula in
// display style.
type liveBlock struct {
	start, end int
	source     string
	// key is its source and how many blocks of the same source come
	// before it, which tells it apart from them.
	key string
	// blocks are its source as the Reading view shows it.
	blocks []markdownBlock
	// embed is set for a note embedded, ![[note]], whose source is a wiki
	// link rather than code, and version is the note's, which its key
	// takes, to draw it again once the note changed.
	embed   bool
	version string
	// quote is how many quotes a note embedded is in, and list how deep in
	// lists, whose item's marker is mark up to text, the item's text: it
	// stands where the item's text does, after the marker drawn, bullet,
	// when the marker is on its line.
	quote, list uint8
	mark, text  int
	bullet      bool
}

// liveCallout is a callout that folds, [!faq]- or [!faq]+, from the start
// of its first line, its title's, to the end of its last.
type liveCallout struct {
	start, end int
	fold       byte
}

// liveDoc is a parse of a Markdown text for Live Preview, in runes of
// the text snap.
type liveDoc struct {
	snap  ui.TextSnapshot
	spans []liveSpan
	// reach is how far the spans up to each reach, the most of their
	// ends, by which the first span to reach a rune is found.
	reach  []int
	decos  []liveDeco
	blocks []liveBlock
	// callouts are the callouts that fold.
	callouts []liveCallout
	// edit is the edit made to the parse's text since, which its spans,
	// decos and blocks follow as they are found; nil for a parse.
	edit *liveEdit
}

// index finds how far the spans reach.
func (doc *liveDoc) index() {
	doc.reach = slices.Grow(doc.reach[:0], len(doc.spans))
	most := 0
	for _, s := range doc.spans {
		most = max(most, s.end)
		doc.reach = append(doc.reach, most)
	}
}

// liveListMarker is the marker of a list's item at the start of a line,
// after its quotes' >, and the box of a task after it.
var liveListMarker = regexp.MustCompile(`^((?:[ \t]*>)*[ \t]*)([-*+]|\d{1,9}[.)])([ \t]+)(?:(\[[ xX]\])(?:[ \t]|$))?`)

// liveRule is a thematic break's line.
var liveRule = regexp.MustCompile(`(?m)^ {0,3}([-*_])(?:[ \t]*[-*_]){2,}[ \t]*\r?$`)

// liveNoteDef is the label of a footnote's definition at the start of
// its line, with the white space after it.
var liveNoteDef = regexp.MustCompile(`^ {0,3}\[\^[^\]\s]+\]:[ \t]*`)

// liveQuoteMarks are the > of the quotes a line opens with, with a space
// after each.
var liveQuoteMarks = regexp.MustCompile(`^(?: {0,3}> ?)+`)

// liveSizes are the scales of the headings' text, as the Reading view
// sizes them.
var liveSizes = [7]float32{1, 2, 1.5, 1.25, 1, 0.875, 0.85}

// parseLive parses source, the text of the Markdown document at path,
// for Live Preview, from and into the cache. Unless full is set, it is
// quick, for the first frame to show: it draws no picture, formula or
// block, which show as their source until a full parse.
func parseLive(source string, path, root string, full bool, cache *markdownCache) *liveDoc {
	images := full
	src := []byte(source)
	doc := &liveDoc{}
	if cache.images == nil {
		cache.images = map[markdownImageKey]markdownInline{}
	}
	used := map[markdownImageKey]bool{}
	usedFormulas := map[mathKey]bool{}

	// runeAt converts a byte offset of src to a rune offset.
	ascii := len(src) == utf8.RuneCount(src)
	var runeOf []int32
	if !ascii {
		// A byte inside a rune takes the rune's offset.
		runeOf = make([]int32, len(src)+1)
		n := int32(-1)
		for i := range src {
			if utf8.RuneStart(src[i]) {
				n++
			}
			runeOf[i] = n
		}
		runeOf[len(src)] = n + 1
	}
	runeAt := func(b int) int {
		b = max(0, min(b, len(src)))
		if ascii {
			return b
		}
		return int(runeOf[b])
	}
	// lineStart and lineEnd are the start of the line of byte b and its end,
	// before its newline.
	lineStart := func(b int) int {
		return bytes.LastIndexByte(src[:max(0, min(b, len(src)))], '\n') + 1
	}
	lineEnd := func(b int) int {
		b = max(0, min(b, len(src)))
		if i := bytes.IndexByte(src[b:], '\n'); i >= 0 {
			e := b + i
			if e > 0 && src[e-1] == '\r' {
				e--
			}
			return e
		}
		return len(src)
	}
	// add appends a span of bytes start up to end.
	add := func(s liveSpan) {
		if s.end <= s.start {
			return
		}
		s.start, s.end, s.from, s.to = runeAt(s.start), runeAt(s.end), runeAt(s.from), runeAt(s.to)
		doc.spans = append(doc.spans, s)
	}
	style := func(start, end int, look liveLook) {
		add(liveSpan{start: start, end: end, kind: liveStyle, from: start, to: end, look: look})
	}
	mark := func(start, end, from, to int) {
		add(liveSpan{start: start, end: end, kind: liveMark, from: from, to: to})
	}
	lineMark := func(start, end int) {
		add(liveSpan{start: start, end: end, kind: liveMark, from: start, to: end, byLine: true})
	}
	deco := func(start, end int, d liveDeco) {
		d.start, d.end = runeAt(start), runeAt(end)
		doc.decos = append(doc.decos, d)
	}
	formula := func(tex string, display bool) *mathFormula {
		key := mathKey{tex, display}
		usedFormulas[key] = true
		f, ok := cache.formulas[key]
		if !ok {
			f = renderMath(tex, display)
			if cache.formulas == nil {
				cache.formulas = map[mathKey]*mathFormula{}
			}
			cache.formulas[key] = f
		}
		return f
	}

	// Front matter is code, faint.
	body, skip := markdownSkipFrontMatter(src)
	off := 0
	if skip > 0 {
		off = len(src) - len(body)
		style(0, lineEnd(max(0, off-1)), liveLook{code: true, block: true, color: liveFaint})
	}
	tree := markdownParser(cache.math).Parse(text.NewReader(body))

	// extent is where an inline node is in src, with its marks, or -1.
	var extent func(n ast.Node) (int, int)
	inner := func(n ast.Node) (int, int) {
		s, e := -1, -1
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			cs, ce := extent(ch)
			if cs < 0 {
				continue
			}
			if s < 0 {
				s = cs
			}
			e = ce
		}
		return s, e
	}
	// delims counts the runs of ch before s and after e, up to most.
	delims := func(s, e int, ch byte, most int) (int, int) {
		before := 0
		for before < most && s-before-1 >= 0 && src[s-before-1] == ch {
			before++
		}
		after := 0
		for after < most && e+after < len(src) && src[e+after] == ch {
			after++
		}
		return before, after
	}
	// linkEnd is where the destination of a link whose text ends at e
	// ends: past (dest), [ref] or ].
	linkEnd := func(e int) int {
		if e >= len(src) || src[e] != ']' {
			return e
		}
		e++
		if e < len(src) && src[e] == '(' {
			depth := 0
			for i := e; i < len(src); i++ {
				switch src[i] {
				case '\\':
					i++
				case '(':
					depth++
				case ')':
					if depth--; depth == 0 {
						return i + 1
					}
				case '\n':
					if i+1 < len(src) && src[i+1] == '\n' {
						return e
					}
				}
			}
			return e
		}
		if e < len(src) && src[e] == '[' {
			if i := bytes.IndexByte(src[e:], ']'); i >= 0 && !bytes.Contains(src[e:e+i], []byte{'\n'}) {
				return e + i + 1
			}
		}
		return e
	}
	extent = func(n ast.Node) (int, int) {
		switch x := n.(type) {
		case *ast.Text:
			return off + x.Segment.Start, off + x.Segment.Stop
		case *ast.RawHTML:
			if x.Segments.Len() == 0 {
				return -1, -1
			}
			return off + x.Segments.At(0).Start, off + x.Segments.At(x.Segments.Len()-1).Stop
		case *ast.AutoLink:
			// Its label is a slice of the source, where it starts.
			label := x.Label(body)
			if len(label) == 0 || cap(label) > cap(body) {
				return -1, -1
			}
			s := off + cap(body) - cap(label)
			e := s + len(label)
			if s > 0 && src[s-1] == '<' && e < len(src) && src[e] == '>' {
				return s - 1, e + 1
			}
			return s, e
		case *mathNode:
			return off + x.start, off + x.stop
		case *wikiLinkNode:
			return off + x.start, off + x.stop
		case *ast.Emphasis:
			s, e := inner(n)
			if s < 0 {
				return -1, -1
			}
			return s - x.Level, e + x.Level
		case *east.Strikethrough, *markNode:
			s, e := inner(n)
			if s < 0 {
				return -1, -1
			}
			ch := byte('~')
			if _, ok := n.(*markNode); ok {
				ch = '='
			}
			b, a := delims(s, e, ch, 2)
			return s - b, e + a
		case *ast.CodeSpan:
			s, e := inner(n)
			if s < 0 {
				return -1, -1
			}
			b, a := codeTicks(src, s, e)
			return s - b, e + a
		case *ast.Link:
			s, e := inner(n)
			if s < 1 {
				return -1, -1
			}
			return s - 1, linkEnd(e)
		case *ast.Image:
			s, e := inner(n)
			if s < 2 {
				return -1, -1
			}
			return s - 2, linkEnd(e)
		}
		return inner(n)
	}

	// scale is the size of the text walkInline walks, a heading's or 1,
	// for its code to stay a size smaller than the text around it.
	scale := float32(1)
	var walkInline func(n ast.Node)
	walkInline = func(n ast.Node) {
		// last is where the child before ended, -1 before the first.
		last := -1
		if n.Type() == ast.TypeBlock {
			if l := n.Lines(); l != nil && l.Len() > 0 {
				last = off + l.At(0).Start
			}
		} else {
			last, _ = extent(n)
		}
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			s, e := extent(ch)
			if e > last {
				last = e
			}
			switch x := ch.(type) {
			case *ast.Emphasis:
				if s >= 0 {
					look := liveLook{italic: x.Level == 1}
					if x.Level == 2 {
						look = liveLook{weight: 700}
					}
					style(s+x.Level, e-x.Level, look)
					mark(s, s+x.Level, s, e)
					mark(e-x.Level, e, s, e)
				}
			case *east.Strikethrough, *markNode:
				if s >= 0 {
					is, ie := inner(ch)
					look := liveLook{strike: true}
					if _, ok := ch.(*markNode); ok {
						look = liveLook{mark: true}
					}
					style(is, ie, look)
					mark(s, is, s, e)
					mark(ie, e, s, e)
				}
			case *ast.CodeSpan:
				if s >= 0 {
					is, ie := inner(ch)
					look := liveLook{code: true}
					if scale != 1 {
						look.size = scale * 0.9
					}
					style(is, ie, look)
					mark(s, is, s, e)
					mark(ie, e, s, e)
				}
				continue
			case *ast.Link:
				if s >= 0 {
					is, ie := inner(ch)
					style(is, ie, liveLook{color: liveLink})
					mark(s, is, s, e)
					if liveExternal(string(x.Destination)) && e-ie >= 3 && src[e-1] == ')' {
						// A link to the web ends in its icon, drawn in the
						// room of its ).
						mark(ie, e-1, s, e)
						add(liveSpan{start: e - 1, end: e, kind: liveObject, from: s, to: e, obj: objExternal})
					} else {
						mark(ie, e, s, e)
					}
				}
			case *ast.Text:
				if s >= 0 {
					liveRunText(src, s, e, x.Parent(), style, mark)
				}
				continue
			case *east.FootnoteLink:
				// [^1], which the parse does not place: after what came before.
				// It draws as its footnote's number, as the Reading view does.
				if at := max(0, last); at < len(src) {
					if i := bytes.Index(src[at:min(len(src), at+256)], []byte("[^")); i >= 0 {
						ls := at + i + 2
						if j := bytes.IndexByte(src[ls:min(len(src), ls+128)], ']'); j > 0 {
							le := ls + j
							style(ls-2, le+1, liveLook{color: liveNote})
							add(liveSpan{start: ls - 2, end: le + 1, kind: liveObject, from: ls - 2, to: le + 1, obj: objNote,
								drawn: &livePicture{note: strconv.Itoa(x.Index)}})
							last = le + 1
						}
					}
				}
				continue
			case *ast.AutoLink:
				if s >= 0 {
					is, ie := s, e
					if src[s] == '<' {
						is, ie = s+1, e-1
					}
					style(is, ie, liveLook{color: liveLink})
					mark(s, is, s, e)
					mark(ie, e, s, e)
				}
				continue
			case *ast.Image:
				if s >= 0 {
					dest := string(x.Destination)
					var image markdownInline
					if images {
						image = markdownPicture(cache, used, path, root, dest)
					}
					switch {
					case image.bitmap != nil:
						add(liveSpan{start: s, end: e, kind: liveObject, from: s, to: e, obj: objImage, drawn: &livePicture{image: image, dest: dest}})
					case images && markdownFetching(dest):
						// A picture of the web on its way: a card in its place.
						add(liveSpan{start: s, end: e, kind: liveObject, from: s, to: e, obj: objImage, drawn: &livePicture{dest: dest, loading: true}})
					default:
						is, ie := inner(ch)
						style(is, ie, liveLook{color: liveLink})
						mark(s, is, s, e)
						mark(ie, e, s, e)
					}
				}
				continue
			case *wikiLinkNode:
				if s >= 0 {
					liveWikiLink(src, s, e, x, path, root, images, cache, used, add, style, mark)
				}
				continue
			case *mathNode:
				if s >= 0 {
					var f *mathFormula
					if full {
						f = formula(x.tex, x.display)
					}
					if f != nil && f.err == "" && f.w > 0 {
						add(liveSpan{start: s, end: e, kind: liveObject, from: s, to: e, obj: objMath, drawn: &livePicture{formula: f}})
					} else {
						style(s, e, liveLook{code: true, color: liveMuted})
					}
				}
				continue
			case *ast.RawHTML:
				if s >= 0 {
					style(s, e, liveLook{color: liveMuted})
				}
				continue
			case *east.TaskCheckBox:
				continue // with its list's marker
			}
			walkInline(ch)
		}
	}
	// linesOf is where a block's lines are in src: from the start of the
	// line of its first to the end of its last.
	var linesOf func(n ast.Node) (int, int)
	linesOf = func(n ast.Node) (int, int) {
		s, e := -1, -1
		if n.Type() == ast.TypeBlock {
			if l := n.Lines(); l != nil && l.Len() > 0 {
				s, e = off+l.At(0).Start, off+l.At(l.Len()-1).Stop
			}
		}
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			var cs, ce int
			if ch.Type() == ast.TypeBlock {
				cs, ce = linesOf(ch)
			} else {
				cs, ce = extent(ch)
			}
			if cs < 0 {
				continue
			}
			if s < 0 || cs < s {
				s = cs
			}
			e = max(e, ce)
		}
		if s < 0 {
			return -1, -1
		}
		return lineStart(s), lineEnd(max(s, e-1))
	}

	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			switch x := ch.(type) {
			case *ast.Heading:
				s, e := linesOf(x)
				if s < 0 {
					if l := x.Lines(); l != nil && l.Len() > 0 {
						s = lineStart(off + l.At(0).Start)
					} else {
						continue
					}
					e = lineEnd(s)
				}
				look := liveLook{size: liveSizes[min(x.Level, 6)], weight: 700, color: liveHeading}
				if x.Level == 6 {
					look.color = liveMuted
				}
				first := lineEnd(s)
				if t := bytes.TrimLeft(src[s:first], " "); len(t) > 0 && t[0] == '#' {
					// # Title: its #s and the spaces after them are a mark,
					// as are the #s that may close it.
					hashes := s + (first - s - len(t))
					at := hashes
					for at < first && src[at] == '#' {
						at++
					}
					for at < first && (src[at] == ' ' || src[at] == '\t') {
						at++
					}
					style(s, first, look)
					lineMark(s, at)
					deco(s, first, liveDeco{kind: decoHeading, depth: x.Level, ruled: x.Level <= 2})
					if end := bytes.TrimRight(src[at:first], " \t"); bytes.HasSuffix(end, []byte{'#'}) {
						trimmed := bytes.TrimRight(end, "#")
						if len(trimmed) == 0 || trimmed[len(trimmed)-1] == ' ' {
							mark(at+len(bytes.TrimRight(trimmed, " \t")), first, s, first)
						}
					}
				} else {
					// Title over a line of = or -, which shows faint.
					style(s, e, look)
					deco(s, lineEnd(s), liveDeco{kind: decoHeading, depth: x.Level})
					next := lineEnd(e + 1)
					if e+1 < len(src) {
						style(e+1, next, liveLook{color: liveFaint})
					}
				}
				scale = look.size
				walkInline(x)
				scale = 1
			case *ast.Paragraph, *ast.TextBlock:
				if v, at, ok := liveEmbed(x, src, off, path, root, full, cache); ok {
					if s, e := linesOf(x); s >= 0 && at >= s && at < e {
						b := liveBlock{start: s, end: e, source: string(src[at:e]), embed: true, version: v}
						b.quote = uint8(min(255, bytes.Count(liveQuoteMarks.Find(src[s:lineEnd(s)]), []byte{'>'})))
						if item := liveItemOf(x); item != nil {
							is, _ := linesOf(item)
							if m := liveListMarker.FindSubmatchIndex(src[is:lineEnd(is)]); is >= 0 && m != nil {
								text := is + m[1]
								for text < len(src) && (src[text] == ' ' || src[text] == '\t') {
									text++
								}
								b.list, b.mark, b.text, b.bullet = uint8(min(255, liveDepth(item))), is+m[4], text, is == s && m[8] < 0
							}
						}
						doc.blocks = append(doc.blocks, b)
					}
				}
				if b, ok := liveMathBlock(x); ok && full {
					s, e := linesOf(x)
					f := formula(b.tex, true)
					if s >= 0 && f.err == "" && f.w > 0 {
						doc.blocks = append(doc.blocks, liveBlock{start: s, end: e, source: string(src[s:e])})
						continue
					}
				}
				walkInline(x)
			case *ast.ThematicBreak:
				// It keeps no place: it is the first rule after the block
				// before it.
				from := off
				if prev := x.PreviousSibling(); prev != nil {
					if _, e := linesOf(prev); e >= 0 {
						from = e
					}
				} else if ps, _ := linesOf(n); ps >= 0 {
					from = ps
				}
				m := liveRule.FindIndex(src[from:])
				if m == nil {
					continue
				}
				s, e := from+m[0], lineEnd(from+m[0])
				lineMark(s, e)
				deco(s, e, liveDeco{kind: decoRule})
			case *ast.FencedCodeBlock:
				s, e := liveFence(src, off, x, lineStart, lineEnd)
				if s < 0 {
					continue
				}
				if lang := string(x.Language(body)); full && strings.EqualFold(lang, "mermaid") && cache.mermaid != nil {
					doc.blocks = append(doc.blocks, liveBlock{start: s, end: e, source: string(src[s:e])})
					continue
				}
				style(s, e, liveLook{code: true, block: true})
				deco(s, e, liveDeco{kind: decoCode, fenced: true})
				// The fences are marks of the block, its language faint.
				open := lineEnd(s)
				ticks := s
				for ticks < open && (src[ticks] == ' ' || src[ticks] == '`' || src[ticks] == '~') {
					ticks++
				}
				add(liveSpan{start: s, end: ticks, kind: liveMark, from: s, to: e})
				style(ticks, open, liveLook{code: true, block: true, color: liveFaint, size: 0.85})
				if close := lineStart(e); close > open {
					if t := bytes.TrimSpace(src[close:e]); len(t) >= 3 && (t[0] == '`' || t[0] == '~') && len(bytes.Trim(t, string(t[0]))) == 0 {
						add(liveSpan{start: close, end: e, kind: liveMark, from: s, to: e})
					}
				}
			case *ast.CodeBlock:
				s, e := linesOf(x)
				if s >= 0 {
					style(s, e, liveLook{code: true, block: true})
					deco(s, e, liveDeco{kind: decoCode})
				}
			case *ast.HTMLBlock:
				s, e := linesOf(x)
				if s >= 0 {
					style(s, e, liveLook{code: true, block: true, color: liveMuted})
				}
			case *ast.Blockquote:
				s, e := linesOf(x)
				if s >= 0 {
					alert, _, _ := markdownAlertOf(x, body)
					liveQuote(src, s, e, alert, lineEnd, add, deco, style)
					if fold := markdownAlertFold(markdownAlertLine(x, body)); alert != alertNone && fold != 0 {
						doc.callouts = append(doc.callouts, liveCallout{start: runeAt(s), end: runeAt(e), fold: fold})
					}
				}
				walk(x)
			case *ast.List:
				depth := liveDepth(x)
				for item := x.FirstChild(); item != nil; item = item.NextSibling() {
					s, _ := linesOf(item)
					if s < 0 {
						continue
					}
					ms, text := liveItem(src, s, lineEnd(s), x.IsOrdered(), depth, add, style)
					if ms < 0 {
						continue
					}
					// The lines of its paragraphs after its first stand under
					// its text, their white space as wide as its marker.
					for ch := item.FirstChild(); ch != nil; ch = ch.NextSibling() {
						switch ch.(type) {
						case *ast.Paragraph, *ast.TextBlock:
						default:
							continue
						}
						ps, pe := linesOf(ch)
						for at := ps; at >= 0 && at <= pe && at < len(src); at = lineEnd(at) + 1 {
							if at == s {
								continue
							}
							ws := at + len(liveQuoteMarks.Find(src[at:lineEnd(at)]))
							we := ws
							for we < len(src) && (src[we] == ' ' || src[we] == '\t') {
								we++
							}
							if we > ws {
								add(liveSpan{start: ws, end: we, kind: liveStyle, from: ms, to: text, look: liveLook{hang: uint8(min(depth, 255))}})
							}
						}
					}
				}
				walk(x)
			case *east.Table:
				s, e := linesOf(x)
				if s >= 0 && full {
					doc.blocks = append(doc.blocks, liveBlock{start: s, end: e, source: string(src[s:e])})
				}
			case *east.FootnoteList:
				// Each footnote on its own lines, which need not be together:
				// muted, its label drawn as its number.
				for ch := x.FirstChild(); ch != nil; ch = ch.NextSibling() {
					fn, ok := ch.(*east.Footnote)
					if !ok {
						continue
					}
					s, e := linesOf(fn)
					if s < 0 {
						continue
					}
					style(s, e, liveLook{color: liveMuted})
					if m := liveNoteDef.Find(src[s:lineEnd(s)]); m != nil {
						add(liveSpan{start: s, end: s + len(m), kind: liveObject, from: s, to: s + len(m), byLine: true,
							obj: objNoteDef, drawn: &livePicture{note: strconv.Itoa(fn.Index) + "."}})
					}
				}
				walk(x)
			default:
				walk(ch)
			}
		}
	}
	walk(tree)
	// Comments, %%like this%%, read faint, their marks too, as their
	// source: what is in them is not styled.
	if comments := markdownComments(body); len(comments) > 0 {
		in := func(s, e int) bool {
			k := sort.Search(len(comments), func(i int) bool { return runeAt(off+comments[i][1]) > s })
			return k < len(comments) && runeAt(off+comments[k][0]) <= s && e <= runeAt(off+comments[k][1])
		}
		doc.spans = slices.DeleteFunc(doc.spans, func(sp liveSpan) bool { return in(sp.start, sp.end) })
		doc.decos = slices.DeleteFunc(doc.decos, func(d liveDeco) bool { return in(d.start, d.end) })
		doc.blocks = slices.DeleteFunc(doc.blocks, func(b liveBlock) bool { return in(runeAt(b.start), runeAt(b.end)) })
		for _, cm := range comments {
			style(off+cm[0], off+cm[1], liveLook{color: liveFaint})
		}
	}

	seen := map[string]int{}
	// A block of the same source as one the last parse drew is drawn as
	// it was: only those edited are parsed, and drawn, again.
	blocks := map[string][]markdownBlock{}
	cache.keep = true
	for i := range doc.blocks {
		b := &doc.blocks[i]
		k := b.source
		if b.embed {
			k += "\x00" + b.version
		}
		b.key = k + "\x00" + strconv.Itoa(seen[k])
		seen[k]++
		if !b.embed {
			// Its source, where the caret is, is code, so a table's pipes
			// line up.
			style(b.start, b.end, liveLook{code: true, block: true})
		}
		parsed, ok := cache.blocks[k]
		if !ok {
			parsed, ok = blocks[k]
		}
		if !ok {
			parsed = parseMarkdown([]byte(b.source), path, root, nil, cache)
		}
		b.blocks, blocks[k] = parsed, parsed
		b.start, b.end = runeAt(b.start), runeAt(b.end)
		if b.list > 0 {
			b.mark, b.text = runeAt(b.mark), runeAt(b.text)
		}
	}
	cache.keep = false
	// The spans by start, those that start together the larger first, so
	// that the marks inside an element style over it.
	slices.SortStableFunc(doc.spans, func(x, y liveSpan) int {
		if x.start != y.start {
			return x.start - y.start
		}
		return y.end - x.end
	})
	doc.index()
	if full {
		// The blocks hold what they drew: the cache keeps what the text
		// itself draws, and the diagrams of the blocks it has.
		cache.blocks = blocks
		for k := range cache.images {
			if !used[k] {
				delete(cache.images, k)
			}
		}
		for k := range cache.formulas {
			if !usedFormulas[k] {
				delete(cache.formulas, k)
			}
		}
		for k := range cache.diagrams {
			if !slices.ContainsFunc(doc.blocks, func(b liveBlock) bool { return strings.Contains(b.source, k) }) {
				delete(cache.diagrams, k)
			}
		}
		clear(cache.code)
	}
	return doc
}

// codeTicks counts the backticks around a code span whose text is src[s:e],
// with the space that pads it on each side.
func codeTicks(src []byte, s, e int) (before, after int) {
	b := s
	if b > 0 && src[b-1] == ' ' && b > 1 && src[b-2] == '`' {
		b--
	}
	for b > 0 && src[b-1] == '`' {
		b--
	}
	a := e
	if a < len(src) && src[a] == ' ' && a+1 < len(src) && src[a+1] == '`' {
		a++
	}
	for a < len(src) && src[a] == '`' {
		a++
	}
	return s - b, a - e
}

// liveEmbed reports whether paragraph x of src, from off, embeds a note
// alone, ![[note]], that shows in a full parse, at the top of the text,
// in a list or in a quote; the version of the note, its time and size;
// and where in src its link starts.
func liveEmbed(x ast.Node, src []byte, off int, path, root string, full bool, cache *markdownCache) (string, int, bool) {
	if !full || root == "" || !liveEmbedHost(x) {
		return "", 0, false
	}
	l, ok := markdownEmbedOf(x, src[off:])
	if !ok {
		return "", 0, false
	}
	file := markdownEmbedFile(cache, root, path, l)
	if file == "" {
		return "", 0, false
	}
	info, err := vfs.Stat(file)
	if err != nil || info.Size() > markdownEmbedMax {
		return "", 0, false
	}
	return file + "\x00" + info.ModTime().String() + "\x00" + strconv.FormatInt(info.Size(), 10), off + l.start, true
}

// liveEmbedHost reports whether a paragraph may embed a note: one of the
// text, of an item of a list, or of a quote, not of a table or a
// footnote.
func liveEmbedHost(x ast.Node) bool {
	for n := x.Parent(); n != nil; n = n.Parent() {
		switch n.(type) {
		case *ast.Document:
			return true
		case *ast.ListItem, *ast.List, *ast.Blockquote:
		default:
			return false
		}
	}
	return false
}

// liveItemOf is the item of a list a block is in, nil for none.
func liveItemOf(x ast.Node) ast.Node {
	for n := x.Parent(); n != nil; n = n.Parent() {
		if _, ok := n.(*ast.ListItem); ok {
			return n
		}
	}
	return nil
}

// liveExternal reports whether a link's destination is on the web.
func liveExternal(dest string) bool {
	return strings.HasPrefix(dest, "https://") || strings.HasPrefix(dest, "http://")
}

// liveMathBlock reports whether a paragraph is a formula in display style
// alone.
func liveMathBlock(n ast.Node) (*mathNode, bool) {
	m, ok := n.FirstChild().(*mathNode)
	return m, ok && m.display && m.NextSibling() == nil
}

// liveFence is where a fenced block of code is in src, from its opening
// fence to the end of its closing one, or of its last line without one.
func liveFence(src []byte, off int, x *ast.FencedCodeBlock, lineStart, lineEnd func(int) int) (int, int) {
	lines := x.Lines()
	var s int
	switch {
	case x.Info != nil:
		s = lineStart(off + x.Info.Segment.Start)
	case lines.Len() > 0:
		s = lineStart(max(0, lineStart(off+lines.At(0).Start)-1))
	default:
		return -1, -1
	}
	t := bytes.TrimLeft(src[s:lineEnd(s)], " ")
	if len(t) == 0 || t[0] != '`' && t[0] != '~' {
		return -1, -1
	}
	fence, n := t[0], 0
	for n < len(t) && t[n] == fence {
		n++
	}
	// The line after the code closes it with as long a run of the mark.
	last := lineEnd(s)
	if lines.Len() > 0 {
		last = lineEnd(off + lines.At(lines.Len()-1).Start)
	}
	if last+1 < len(src) {
		end := lineEnd(last + 1)
		line := bytes.TrimSpace(src[last+1 : end])
		if len(line) >= n && line[0] == fence && len(bytes.Trim(line, string(fence))) == 0 {
			return s, end
		}
	}
	return s, last
}

// liveDepth is how deep a list is in other lists, 1 for one at the top.
func liveDepth(n ast.Node) int {
	d := 0
	for ; n != nil; n = n.Parent() {
		if _, ok := n.(*ast.List); ok {
			d++
		}
	}
	return d
}

// liveIndent is how far, in the text's size, an item of a list stands in
// from its parent's: past a bullet and its space.
const liveIndent = 1.45

// liveItem styles the marker of a list's item on the line src[s:e]: a dot
// for a bullet, a ring in a list inside another, a box for a task, the
// number of an ordered list muted. It returns where its marker starts and
// its text, -1 for no marker.
func liveItem(src []byte, s, e int, ordered bool, depth int, add func(liveSpan), style func(int, int, liveLook)) (int, int) {
	m := liveListMarker.FindSubmatchIndex(src[s:e])
	if m == nil {
		return -1, -1
	}
	text := s + m[1]
	for text < e && (src[text] == ' ' || src[text] == '\t') {
		text++
	}
	ms, me := s+m[4], s+m[5]
	task := m[8] >= 0
	// Its white space, after its quotes' >, as wide as its parents' markers.
	if depth > 1 {
		ws := ms
		for ws > s && (src[ws-1] == ' ' || src[ws-1] == '\t') {
			ws--
		}
		if ws < ms {
			style(ws, ms, liveLook{indent: liveIndent * float32(depth-1) / float32(ms-ws)})
		}
	}
	if ordered {
		style(ms, me, liveLook{color: liveMuted})
	} else if !task {
		obj := objBullet
		if depth > 1 {
			obj = objRing
		}
		add(liveSpan{start: ms, end: me, kind: liveObject, from: ms, to: me, obj: obj})
	}
	if task {
		// A task done reads faint, struck through, as the frame styles it
		// from its box: at once as the box is checked.
		bs, be := s+m[8], s+m[9]
		obj := objTask
		if src[bs+1] != ' ' {
			obj = objTaskDone
		}
		from := bs
		if !ordered {
			// The task's box stands for the bullet, and shows as its source
			// with it: as wide as both, so the line holds still.
			from = ms
			add(liveSpan{start: ms, end: s + m[7], kind: liveMark, from: ms, to: be})
		}
		add(liveSpan{start: bs, end: be, kind: liveObject, from: from, to: be, obj: obj})
	}
	return ms, text
}

// liveQuote styles the lines src[s:e] of a quote: each line's > take the
// room of the bars painted there, its text muted, an alert's title in its
// color, its marker hidden.
func liveQuote(src []byte, s, e int, alert markdownAlert, lineEnd func(int) int,
	add func(liveSpan), deco func(int, int, liveDeco), style func(int, int, liveLook)) {
	depth := 1
	for at := s; at <= e && at < len(src); {
		end := lineEnd(at)
		marks := liveQuoteMarks.Find(src[at:end])
		if n := bytes.Count(marks, []byte{'>'}); n > 0 {
			depth = n
			add(liveSpan{start: at, end: at + len(marks), kind: liveObject, from: at, to: at + len(marks), byLine: true, obj: objQuote, depth: uint8(min(n, 255))})
		}
		deco(at, end, liveDeco{kind: decoQuote, depth: depth, alert: alert})
		if alert == alertNone {
			style(at+len(marks), end, liveLook{color: liveMuted})
		} else if at == s {
			// [!NOTE] draws as the alert's icon and kind; one with a title of
			// its own, as Obsidian's [!tip] Title, as its icon before it.
			ms := at + len(marks)
			rest := string(src[ms:end])
			if _, n := markdownAlertMarker(rest); n > 0 {
				style(ms, end, liveLook{weight: 600, color: liveAlert, alert: alert})
				sp := liveSpan{start: ms, end: ms + n, kind: liveObject, from: ms, to: end, byLine: true, obj: objAlert, alert: alert}
				if strings.TrimSpace(rest[n:]) != "" {
					sp.depth = 1
				}
				add(sp)
			}
		}
		at = end + 1
	}
}

// liveWikiLink styles a wiki link src[s:e]: its label as a link, its
// brackets and target marks, or a picture it embeds drawn.
func liveWikiLink(src []byte, s, e int, x *wikiLinkNode, path, root string, images bool, cache *markdownCache,
	used map[markdownImageKey]bool, add func(liveSpan), style func(int, int, liveLook), mark func(int, int, int, int)) {
	open := s + 2
	if x.embed {
		open++
	}
	close := e - 2
	if open > close {
		return
	}
	if x.embed && images && x.target != "" {
		if file := cache.wikiLinkFile(root, path, x.target); file != "" && isImage(file) {
			image := markdownPicture(cache, used, path, root, wikiLinkDest(path, file, ""))
			if image.bitmap != nil {
				sp := liveSpan{start: s, end: e, kind: liveObject, from: s, to: e, obj: objImage, drawn: &livePicture{image: image}}
				w, _, _ := strings.Cut(x.label, "x")
				if n, err := strconv.Atoi(w); err == nil && n > 0 {
					sp.drawn.width = float32(n)
				}
				add(sp)
				return
			}
		}
	}
	label := open
	if i := bytes.LastIndexByte(src[open:close], '|'); i >= 0 {
		label = open + i + 1
	}
	look := liveLook{color: liveLink}
	if images && root != "" && x.target != "" && cache.wikiLinkFile(root, path, x.target) == "" {
		look.color = liveUnresolved
	}
	style(label, close, look)
	mark(s, label, s, e)
	mark(close, e, s, e)
}

// liveEdit is where a text changed since a parse: it agreed up to rune p,
// and from rune q of the parse's text and r of the text now to their
// ends.
type liveEdit struct{ p, q, r int }

// start is where rune x of the parse's text is now, as the start of a
// span: what was before the edit stays, what was after moves with it, and
// what it replaced goes to its end.
func (e liveEdit) start(x int) int {
	switch {
	case x < e.p:
		return x
	case x >= e.q:
		return x + e.r - e.q
	}
	return e.r
}

// end is where rune x is now as the end of a span, which grows with what
// was typed at its end if it grows.
func (e liveEdit) end(x int, grows bool) int {
	switch {
	case x < e.p, x == e.p && (e.p < e.q || !grows):
		return x
	case x >= e.q:
		return x + e.r - e.q
	}
	if grows {
		return e.r
	}
	return e.p
}

// before is the last rune of the parse's text that is at or before rune
// x of the text now.
func (e liveEdit) before(x int) int {
	switch {
	case x < e.p:
		return x
	case x >= e.r:
		return x - (e.r - e.q)
	}
	return e.p
}

// mapTo returns the parse moved to snap, a text that the edits made since
// the parse changed: what was before the edits stays, what was after
// moves with them, and what they replaced goes. A span that styles text
// grows with what is typed at its end; a mark does not. Its spans, decos
// and blocks move only as visit, eachDeco and eachBlock find them, as a
// frame needs those about the view alone.
func (doc *liveDoc) mapTo(snap ui.TextSnapshot) *liveDoc {
	if doc.snap.Version() == snap.Version() && doc.snap.Len() == snap.Len() {
		return doc
	}
	p, q, r := snapDiff(doc.snap, snap)
	e := liveEdit{p, q, r}
	out := &liveDoc{snap: snap, spans: doc.spans, reach: doc.reach, decos: doc.decos, blocks: doc.blocks, edit: &e}
	// The callouts that fold, few, move at once.
	for _, c := range doc.callouts {
		c.start, c.end = e.start(c.start), e.end(c.end, true)
		if c.end >= c.start {
			out.callouts = append(out.callouts, c)
		}
	}
	return out
}

// eachDeco calls fn with each deco that reaches runes from up to to,
// moved to the text as it is.
func (doc *liveDoc) eachDeco(from, to int, fn func(d liveDeco)) {
	e := doc.edit
	for _, d := range doc.decos {
		if e != nil {
			d.start, d.end = e.start(d.start), e.end(d.end, true)
		}
		if d.end >= d.start && d.end >= from && d.start <= to {
			fn(d)
		}
	}
}

// eachBlock calls fn with each block that reaches runes from up to to,
// moved to the text as it is, but for those edited since the parse,
// which show as their text until the next.
func (doc *liveDoc) eachBlock(from, to int, fn func(b *liveBlock)) {
	e := doc.edit
	oldFrom, oldTo := from, to
	if e != nil {
		oldFrom, oldTo = e.before(from), e.before(to)
	}
	// The blocks follow one another, by their starts and their ends.
	k := sort.Search(len(doc.blocks), func(i int) bool { return doc.blocks[i].end >= oldFrom })
	for i := k; i < len(doc.blocks) && doc.blocks[i].start <= oldTo; i++ {
		b := doc.blocks[i]
		if e != nil {
			if b.start < e.q && b.end >= e.p {
				continue
			}
			b.start, b.end = e.start(b.start), e.end(b.end, true)
			b.mark, b.text = e.start(b.mark), e.start(b.text)
		}
		if b.end >= from && b.start <= to {
			fn(&b)
		}
	}
}

// visit calls fn with each span that reaches runes from up to to, by
// their starts, moved to the text as it is.
func (doc *liveDoc) visit(from, to int, fn func(s *liveSpan)) {
	e := doc.edit
	oldFrom, oldTo := from, to
	if e != nil {
		oldFrom, oldTo = e.before(from), e.before(to)
	}
	k := sort.Search(len(doc.reach), func(i int) bool { return doc.reach[i] > oldFrom })
	for i := k; i < len(doc.spans); i++ {
		s := doc.spans[i]
		if s.start > oldTo {
			break
		}
		if e != nil {
			edited := s.kind == liveObject && s.start < e.q && s.end > e.p
			if edited {
				// A task's box checked or unchecked shows so at once; a
				// picture or a formula edited is text until parsed.
				if s.obj != objTask && s.obj != objTaskDone {
					continue
				}
				start, end := e.start(s.start), e.end(s.end, true)
				switch doc.snap.Slice(start, min(end, doc.snap.Len())) {
				case "[ ]":
					s.obj = objTask
				case "[x]", "[X]":
					s.obj = objTaskDone
				default:
					continue
				}
			}
			s.start, s.end = e.start(s.start), e.end(s.end, s.kind == liveStyle || edited)
			s.from, s.to = e.start(s.from), e.end(s.to, true)
		}
		if s.end <= s.start || s.end <= from || s.start > to {
			continue
		}
		fn(&s)
	}
}

// snapDiff returns where two texts differ: they agree up to rune p, and
// from rune q of a and r of b to their ends.
func snapDiff(a, b ui.TextSnapshot) (p, q, r int) {
	const chunk = 4096
	na, nb := a.Len(), b.Len()
	for p < na && p < nb {
		k := min(chunk, na-p, nb-p)
		sa, sb := a.Slice(p, p+k), b.Slice(p, p+k)
		if sa == sb {
			p += k
			continue
		}
		i := 0
		for i < len(sa) && i < len(sb) && sa[i] == sb[i] {
			i++
		}
		for i > 0 && i < len(sa) && !utf8.RuneStart(sa[i]) {
			i--
		}
		p += utf8.RuneCountInString(sa[:i])
		break
	}
	s := 0
	for s < na-p && s < nb-p {
		k := min(chunk, na-p-s, nb-p-s)
		sa, sb := a.Slice(na-s-k, na-s), b.Slice(nb-s-k, nb-s)
		if sa == sb {
			s += k
			continue
		}
		i := 0
		for i < len(sa) && i < len(sb) && sa[len(sa)-1-i] == sb[len(sb)-1-i] {
			i++
		}
		for i > 0 && !utf8.RuneStart(sa[len(sa)-i]) {
			i--
		}
		s += utf8.RuneCountInString(sa[len(sa)-i:])
		break
	}
	return p, na - s, nb - s
}

// liveTask is the rune of the line text whose [ ] or [x] is a task's
// box, after a list's marker, and whether it is done; -1 for none.
func liveTask(line string) (int, bool) {
	m := liveListMarker.FindStringSubmatchIndex(line)
	if m == nil || m[8] < 0 {
		return -1, false
	}
	return utf8.RuneCountInString(line[:m[8]+1]), line[m[8]+1] != ' '
}

// liveRunText styles what a run of text src[s:e], a child of parent, holds:
// the \ that escapes a mark, hidden as a mark of its own, #tags, as pills,
// but in a link's text, and the id of a block, ^id, faint.
func liveRunText(src []byte, s, e int, parent ast.Node, style func(int, int, liveLook), mark func(int, int, int, int)) {
	if id := markdownBlockIDIn(src, s, e); id >= 0 {
		style(id, e, liveLook{color: liveFaint})
		e = id
	}
	for i := s; i < e; i++ {
		if src[i] == '\\' && i+1 < e && liveEscapable(src[i+1]) {
			mark(i, i+1, i, i+2)
			i++
		}
	}
	switch parent.(type) {
	case *ast.Link, *ast.Image:
		return
	}
	for _, t := range markdownTagsIn(src, s, e) {
		style(t[0], t[1], liveLook{tag: true, color: liveLink})
	}
}

// markdownTagsIn are the #tags of the run of text src[s:e], as Live
// Preview finds them: not escaped, at a word's start.
func markdownTagsIn(src []byte, s, e int) [][2]int {
	var tags [][2]int
	for i := s; i < e; i++ {
		switch c := src[i]; {
		case c == '\\' && i+1 < e && liveEscapable(src[i+1]):
			i++
		case c == '#' && (i == 0 || liveTagAfter(src[i-1])):
			if j := liveTagEnd(src, i+1, e); j > i+1 {
				tags = append(tags, [2]int{i, j})
				i = j - 1
			}
		}
	}
	return tags
}

// liveEscapable reports whether a \ before c escapes it: an ASCII mark.
func liveEscapable(c byte) bool {
	return c < utf8.RuneSelf && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// liveTagAfter reports whether a # after c may open a tag: at a word's
// start.
func liveTagAfter(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '(' || c == '\r'
}

// liveTagEnd is where the tag whose name starts at src[s] ends, before
// e, as Obsidian reads one: letters, digits, _, - and /, not all digits;
// s for none.
func liveTagEnd(src []byte, s, e int) int {
	i, word := s, false
	for i < e {
		r, n := utf8.DecodeRune(src[i:e])
		switch {
		case r == '_' || r == '-' || r == '/' || unicode.IsLetter(r) || unicode.Is(unicode.Mn, r):
			word = true
		case unicode.IsDigit(r):
		default:
			if !word {
				return s
			}
			return i
		}
		i += n
	}
	if !word {
		return s
	}
	return i
}
