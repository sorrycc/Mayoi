package main

import (
	"html"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	east "github.com/yuin/goldmark/extension/ast"
)

// markdownTag is a tag of the HTML in Markdown, or the text between tags
// when name is "". Names are in lower case.
type markdownTag struct {
	name, text string
	attrs      map[string]string
	end, self  bool
}

// markdownVoid are the elements without content or an end tag.
var markdownVoid = map[string]bool{"img": true, "br": true, "hr": true, "source": true, "wbr": true, "input": true, "meta": true, "link": true, "area": true, "col": true}

// markdownTags splits HTML into its tags and the text between them, its
// entities decoded, as much as the preview makes use of: comments and
// declarations go, as do the scripts and styles, and a < that opens no
// tag is text.
func markdownTags(s string) []markdownTag {
	var out []markdownTag
	text := func(t string) {
		if t != "" {
			out = append(out, markdownTag{text: html.UnescapeString(t)})
		}
	}
	for s != "" {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			text(s)
			break
		}
		text(s[:i])
		s = s[i:]
		switch {
		case strings.HasPrefix(s, "<!--"):
			end := strings.Index(s[4:], "-->")
			if end < 0 {
				return out
			}
			s = s[4+end+3:]
			continue
		case strings.HasPrefix(s, "<!") || strings.HasPrefix(s, "<?"):
			end := strings.IndexByte(s, '>')
			if end < 0 {
				return out
			}
			s = s[end+1:]
			continue
		}
		t, n := markdownTagAt(s)
		if n == 0 {
			text("<")
			s = s[1:]
			continue
		}
		s = s[n:]
		// A script's or a style's text is not shown, nor taken for tags.
		if (t.name == "script" || t.name == "style") && !t.end && !t.self {
			end := strings.Index(strings.ToLower(s), "</"+t.name)
			if end < 0 {
				return out
			}
			s = s[end:]
			continue
		}
		out = append(out, t)
	}
	return out
}

// markdownTagAt reads the tag s opens with, and the bytes it takes, 0 for
// none.
func markdownTagAt(s string) (markdownTag, int) {
	var t markdownTag
	i := 1
	if i < len(s) && s[i] == '/' {
		t.end = true
		i++
	}
	start := i
	for i < len(s) && (isASCIILetter(s[i]) || i > start && (s[i] >= '0' && s[i] <= '9' || s[i] == '-')) {
		i++
	}
	if i == start {
		return t, 0
	}
	t.name = strings.ToLower(s[start:i])
	for {
		for i < len(s) && strings.IndexByte(" \t\r\n\f", s[i]) >= 0 {
			i++
		}
		switch {
		case i >= len(s):
			return t, 0
		case s[i] == '>':
			t.self = t.self || markdownVoid[t.name]
			return t, i + 1
		case s[i] == '/':
			t.self = true
			i++
			continue
		}
		start := i
		for i < len(s) && strings.IndexByte(" \t\r\n\f=>/", s[i]) < 0 {
			i++
		}
		if i == start {
			// A stray = or quote: past it.
			i++
			continue
		}
		name, value := strings.ToLower(s[start:i]), ""
		for i < len(s) && strings.IndexByte(" \t\r\n\f", s[i]) >= 0 {
			i++
		}
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && strings.IndexByte(" \t\r\n\f", s[i]) >= 0 {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				end := strings.IndexByte(s[i+1:], s[i])
				if end < 0 {
					return t, 0
				}
				value = s[i+1 : i+1+end]
				i += end + 2
			} else {
				start := i
				for i < len(s) && strings.IndexByte(" \t\r\n\f>", s[i]) < 0 {
					i++
				}
				value = s[start:i]
			}
		}
		if t.attrs == nil {
			t.attrs = map[string]string{}
		}
		if _, ok := t.attrs[name]; !ok {
			t.attrs[name] = html.UnescapeString(value)
		}
	}
}

func isASCIILetter(b byte) bool { return b|0x20 >= 'a' && b|0x20 <= 'z' }

// markdownOpen is an inline tag of HTML open, and the style before it.
type markdownOpen struct {
	name  string
	style markdownInline
}

// markdownStyleTag applies an inline tag of HTML to style, as <b> or
// <a href>, keeping the style before it in open until its end tag, and
// reports whether it was one of them.
func markdownStyleTag(t markdownTag, style *markdownInline, open *[]markdownOpen) bool {
	s := *style
	switch t.name {
	case "b", "strong":
		s.bold = true
	case "i", "em", "cite", "var", "dfn":
		s.italic = true
	case "s", "del", "strike":
		s.strike = true
	case "u", "ins", "abbr":
		s.underline = true
	case "small":
		s.small = true
	case "sup":
		s.script = 1
	case "sub":
		s.script = -1
	case "mark":
		s.mark = true
	case "code", "kbd", "samp", "tt":
		s.code = true
	case "a":
		if href := t.attrs["href"]; href != "" {
			s.link = href
		}
	default:
		return false
	}
	if t.end {
		for i := len(*open) - 1; i >= 0; i-- {
			if (*open)[i].name == t.name {
				*style = (*open)[i].style
				*open = (*open)[:i]
				break
			}
		}
		return true
	}
	if !t.self {
		*open = append(*open, markdownOpen{t.name, *style})
		*style = s
	}
	return true
}

// markdownScripts are the characters raised and lowered that <sup> and
// <sub> take theirs to, as fonts have them.
var markdownScripts = [2]map[rune]rune{
	{'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ', ' ': ' '},
	{'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉', '+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎', ' ': ' '},
}

// markdownScript raises or lowers the text of a run of <sup> or <sub>
// into the characters for it, when each has one, as 2 of mc² does; a run
// of others stays as it is, which the preview shows smaller.
func markdownScript(r *markdownInline) {
	if r.script == 0 || r.bitmap != nil || r.math != nil {
		return
	}
	m := markdownScripts[0]
	if r.script < 0 {
		m = markdownScripts[1]
	}
	out := []rune(r.text)
	for i, c := range out {
		s, ok := m[c]
		if !ok {
			return
		}
		out[i] = s
	}
	r.text, r.script = string(out), 0
}

// markdownHTMLBlocks are the elements of HTML that start a block of the
// preview of their own, as <p> or <h1>.
var markdownHTMLBlocks = map[string]bool{
	"p": true, "div": true, "center": true, "section": true, "article": true, "header": true, "footer": true, "nav": true, "main": true, "aside": true,
	"figure": true, "figcaption": true, "details": true, "summary": true, "blockquote": true, "pre": true, "address": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true, "caption": true, "hr": true,
}

// markdownAligned is how a block of HTML aligns its text, by its align,
// or else as the block it is in does.
func markdownAligned(t markdownTag, outer ui.Align) ui.Align {
	if t.name == "center" {
		return ui.Center
	}
	switch strings.ToLower(strings.TrimSpace(t.attrs["align"])) {
	case "center", "middle":
		return ui.Center
	case "right":
		return ui.End
	case "left", "justify":
		return ui.Start
	}
	return outer
}

// markdownBox is a block of HTML open, and how it aligns its text.
type markdownBox struct {
	name  string
	align ui.Align
}

// markdownHTMLList is a list of HTML open, <ul> or <ol>: the number of
// its next item, 0 for one of dots, and how wide its markers are, in
// sizes of the text.
type markdownHTMLList struct {
	next  int
	width float32
}

// markdownDetails is a <details> open: whether it shows open at first,
// and the key it folds by, once its summary ended, "" until then.
type markdownDetails struct {
	open bool
	key  string
}

// markdownHTMLCell is a cell of a table of HTML: its text, whether it is
// of the header, <th>, how it aligns its text, and how many columns it
// spans.
type markdownHTMLCell struct {
	runs  []markdownInline
	head  bool
	align east.Alignment
	span  int
}

// markdownHTMLTable is a <table> open: its rows of cells, its caption,
// the block it starts as, and how many tables in it are open, whose
// cells are text of its own.
type markdownHTMLTable struct {
	rows    [][]markdownHTMLCell
	caption []markdownInline
	in      bool // in its caption
	base    markdownBlock
	inner   int
	// align is the align of its row open.
	align string
}

// markdownHTML is the HTML of a text as the preview walks its blocks:
// the tags still open, which may end in a later block of HTML, the
// Markdown between them in them.
type markdownHTML struct {
	boxes []markdownBox
	lists []markdownHTMLList
	// quote is how many <blockquote>s are open.
	quote   int
	details []markdownDetails
	// folds are the keys of the callouts and the <details> folding the
	// blocks walked are in, those of the parse, and seen how many
	// <details> of each summary came before, for each its own key.
	folds *[]string
	seen  map[string]int
	table *markdownHTMLTable
	// pre is the text of a <pre> open, nil for none.
	pre *strings.Builder
	// dark and light are the pictures a <picture> open has for a theme.
	dark, light string
	// marker is the marker of the item of a list just opened, which the
	// first block emitted in it shows.
	marker string
	// The parse's own: emit adds a block, anchor gives a heading its
	// slug, image is the picture of an <img>, code the runs of a block
	// of code, and rows adds the rows of a table.
	emit   func(markdownBlock)
	anchor func(*markdownBlock)
	image  func(markdownTag, markdownInline) markdownInline
	code   func(string) []markdownInline
	rows   func([]markdownBlock)
}

func (h *markdownHTML) align() ui.Align {
	if len(h.boxes) == 0 {
		return ui.Start
	}
	return h.boxes[len(h.boxes)-1].align
}

// adjust places a block b, as the walk of the Markdown has it, within the
// HTML open: aligned as its box, folded as its <details>, in its quotes,
// and under the items of its lists.
func (h *markdownHTML) adjust(b markdownBlock) markdownBlock {
	b.align = h.align()
	b.folds = slices.Clip(*h.folds)
	b.quote += h.quote
	for _, l := range h.lists {
		b.indent++
		b.offset += l.width + 0.5
		b.tight = true
	}
	return b
}

// take is what emit does first with a block: one in a cell of a table
// is its text, and the first of an item of a list shows its marker. It
// reports whether it took the block.
func (h *markdownHTML) take(b *markdownBlock) bool {
	text := markdownHasText(b.runs) || slices.ContainsFunc(b.runs, func(r markdownInline) bool { return r.bitmap != nil })
	if (b.kind == markdownParagraph || b.kind == markdownHeading) && !text {
		return false
	}
	if t := h.table; t != nil && b.fold == 0 {
		if cell := h.cell(); cell != nil {
			if len(cell.runs) > 0 {
				cell.runs = append(cell.runs, markdownInline{text: "\n"})
			}
			cell.runs = append(cell.runs, b.runs...)
		}
		return true
	}
	h.summarize(b)
	if h.marker != "" && len(h.lists) > 0 && b.marker == "" {
		w := h.lists[len(h.lists)-1].width
		b.marker, b.markerWidth, b.offset = h.marker, w, b.offset-w-0.5
		h.marker = ""
	}
	return false
}

// summarize gives a <details> without a summary of its own GitHub's,
// Details, before the first block in it, b.
func (h *markdownHTML) summarize(b *markdownBlock) {
	if n := len(h.details); n == 0 || h.details[n-1].key != "" || b.fold != 0 {
		return
	}
	s := *b
	s.kind, s.level, s.marker, s.markerWidth = markdownParagraph, 0, "", 0
	s.runs, s.anchors, s.slug = []markdownInline{{text: "Details", bold: true}}, nil, ""
	h.fold(&s)
	h.emit(s)
	b.folds = slices.Clip(*h.folds)
}

// fold makes s the summary of the <details> open last, which folds the
// blocks after it until its end.
func (h *markdownHTML) fold(s *markdownBlock) {
	d := &h.details[len(h.details)-1]
	key := "<details>"
	s.fold = '-'
	if d.open {
		key, s.fold = "<details open>", '+'
	}
	key += markdownRunsText(s.runs)
	if h.seen == nil {
		h.seen = map[string]int{}
	}
	if n := h.seen[key]; n > 0 {
		key += "\x00" + strconv.Itoa(n)
	}
	h.seen[strings.SplitN(key, "\x00", 2)[0]]++
	d.key, s.foldKey = key, key
	*h.folds = append(slices.Clip(*h.folds), key)
}

// cell is the cell of the table open that text goes in, nil for none.
func (h *markdownHTML) cell() *markdownHTMLCell {
	t := h.table
	if t == nil || len(t.rows) == 0 {
		return nil
	}
	row := t.rows[len(t.rows)-1]
	if len(row) == 0 {
		return nil
	}
	return &row[len(row)-1]
}

// markdownFoldOf is how the callout or the <details> of a fold key folds
// at first: '-' folded, '+' open, 0 not at all.
func markdownFoldOf(key string) byte {
	switch {
	case strings.HasPrefix(key, "<details open>"):
		return '+'
	case strings.HasPrefix(key, "<details>"):
		return '-'
	}
	return markdownAlertFold(key)
}

// block emits the blocks of a block of HTML, raw, b its place as the walk
// of the Markdown has it: its text, its spaces collapsed, styled as its
// inline tags say, its pictures by image, a block for each of its
// paragraphs, headings, items of lists and summaries, its tables and its
// <pre>s. Its other tags go, their text staying.
func (h *markdownHTML) block(raw string, base markdownBlock) {
	b := h.adjust(base)
	b.kind = markdownParagraph
	var style markdownInline
	var open []markdownOpen
	// space is whether the text so far ends in a space or a break, or is
	// none, for a space after it to go; in a cell, cellSpace is.
	space, cellSpace := true, true
	// runs is where text goes: the block's, a cell's or a caption's.
	runs := func() (*[]markdownInline, *bool) {
		if t := h.table; t != nil {
			if t.in {
				return &t.caption, &cellSpace
			}
			if cell := h.cell(); cell != nil {
				return &cell.runs, &cellSpace
			}
			return nil, &cellSpace
		}
		return &b.runs, &space
	}
	add := func(r markdownInline) {
		if out, _ := runs(); out != nil {
			*out = append(*out, r)
		}
	}
	// brk breaks the line of a cell, unless it is empty or just broken.
	brk := func() {
		out, sp := runs()
		if out == nil || len(*out) == 0 || strings.HasSuffix((*out)[len(*out)-1].text, "\n") {
			return
		}
		r := style
		r.text = "\n"
		*out = append(*out, r)
		*sp = true
	}
	flush := func() {
		if h.table != nil {
			return
		}
		for n := len(b.runs); n > 0 && b.runs[n-1].bitmap == nil; n = len(b.runs) {
			b.runs[n-1].text = strings.TrimRight(b.runs[n-1].text, " ")
			if b.runs[n-1].text != "" {
				break
			}
			b.runs = b.runs[:n-1]
		}
		if b.kind == markdownHeading {
			for i := range b.runs {
				b.runs[i].bold = true
			}
			h.anchor(&b)
		}
		h.emit(b)
		kind, level := b.kind, b.level
		b = h.adjust(base)
		b.kind, b.level = kind, level
		space = true
	}
	tags := markdownTags(raw)
	for i, t := range tags {
		if h.pre != nil {
			switch {
			case t.name == "":
				h.pre.WriteString(t.text)
			case t.name == "pre" && t.end:
				text := strings.TrimPrefix(strings.TrimPrefix(h.pre.String(), "\r"), "\n")
				h.pre = nil
				code := h.adjust(base)
				code.kind, code.runs = markdownCodeBlock, h.code(strings.TrimRight(text, " \t\r\n"))
				h.emit(code)
			}
			continue
		}
		switch {
		case t.name == "":
			out, sp := runs()
			if out == nil {
				continue
			}
			var text strings.Builder
			for _, r := range t.text {
				if strings.ContainsRune(" \t\r\n\f", r) {
					if !*sp {
						text.WriteByte(' ')
					}
					*sp = true
					continue
				}
				text.WriteRune(r)
				*sp = false
			}
			if text.Len() > 0 {
				r := style
				r.text = text.String()
				*out = append(*out, r)
			}
		case t.name == "br" && !t.end:
			r := style
			r.text = "\n"
			add(r)
			_, sp := runs()
			*sp = true
		case t.name == "source" && !t.end:
			// A <picture>'s for a theme, as GitHub's READMEs have their
			// logos for the dark one.
			media := strings.ToLower(t.attrs["media"])
			src := strings.Fields(strings.Split(t.attrs["srcset"], ",")[0])
			switch {
			case len(src) == 0:
			case strings.Contains(media, "prefers-color-scheme") && strings.Contains(media, "dark"):
				h.dark = src[0]
			case strings.Contains(media, "prefers-color-scheme") && strings.Contains(media, "light"):
				h.light = src[0]
			}
		case t.name == "picture":
			h.dark, h.light = "", ""
		case t.name == "img" && !t.end:
			r := h.image(t, style)
			for _, alt := range []struct {
				src string
				to  **markdownInline
			}{{h.dark, &r.dark}, {h.light, &r.light}} {
				if alt.src == "" {
					continue
				}
				at := markdownTag{name: "img", attrs: maps.Clone(t.attrs)}
				at.attrs["src"] = alt.src
				p := h.image(at, style)
				*alt.to = &p
			}
			add(r)
			_, sp := runs()
			*sp = false
		case t.name == "table":
			if tb := h.table; tb != nil {
				// A table in a cell is its text, its rows its lines.
				switch {
				case t.end && tb.inner == 0:
					h.endTable()
				case t.end:
					tb.inner--
					brk()
				case !t.self:
					tb.inner++
					brk()
				}
				continue
			}
			if !t.end && !t.self {
				flush()
				h.table = &markdownHTMLTable{base: h.adjust(base)}
				h.table.base.line = b.line
				cellSpace = true
			}
		case h.table != nil && h.table.inner == 0 && (t.name == "tr" || t.name == "td" || t.name == "th" || t.name == "caption"):
			tb := h.table
			switch {
			case t.name == "caption":
				tb.in = !t.end
				cellSpace = true
			case t.end:
			case t.name == "tr":
				// Its align is its cells' unless theirs.
				tb.rows, tb.align = append(tb.rows, nil), strings.ToLower(strings.TrimSpace(t.attrs["align"]))
			default:
				if len(tb.rows) == 0 {
					tb.rows = append(tb.rows, nil)
				}
				align := strings.ToLower(strings.TrimSpace(t.attrs["align"]))
				if align == "" {
					align = tb.align
				}
				span, _ := strconv.Atoi(t.attrs["colspan"])
				cell := markdownHTMLCell{head: t.name == "th", span: min(max(span, 1), 20)}
				switch align {
				case "center", "middle":
					cell.align = east.AlignCenter
				case "right":
					cell.align = east.AlignRight
				}
				n := len(tb.rows) - 1
				tb.rows[n] = append(tb.rows[n], cell)
				cellSpace = true
			}
		case h.table != nil && h.table.inner > 0 && (t.name == "td" || t.name == "th"):
			// The cells of a table in a cell apart on their row's line.
			if out, sp := runs(); !t.end && out != nil && !*sp {
				*out = append(*out, markdownInline{text: " "})
				*sp = true
			}
		case h.table != nil && t.name == "li" && !t.end:
			brk()
			add(markdownInline{text: "• "})
			cellSpace = true
		case h.table != nil && (markdownHTMLBlocks[t.name] || t.name == "hr"):
			brk()
		case t.name == "hr" && !t.end:
			flush()
			rule := h.adjust(base)
			rule.kind = markdownRule
			h.emit(rule)
		case t.name == "pre" && !t.end && !t.self:
			flush()
			h.pre = &strings.Builder{}
		case markdownHTMLBlocks[t.name]:
			if n := len(h.details); t.name == "summary" && t.end && n > 0 && h.details[n-1].key == "" {
				// The summary of a <details>, which folds the blocks after
				// it.
				if !markdownHasText(b.runs) {
					b.runs = []markdownInline{{text: "Details", bold: true}}
				}
				h.fold(&b)
			}
			flush()
			if t.end {
				for i := len(h.boxes) - 1; i >= 0; i-- {
					if h.boxes[i].name == t.name {
						h.boxes = h.boxes[:i]
						break
					}
				}
				switch t.name {
				case "ul", "ol":
					if n := len(h.lists); n > 0 {
						h.lists, h.marker = h.lists[:n-1], ""
					}
				case "li":
					h.marker = ""
				case "blockquote":
					h.quote = max(0, h.quote-1)
				case "details":
					if n := len(h.details); n > 0 {
						if key := h.details[n-1].key; key != "" {
							if k := slices.Index(*h.folds, key); k >= 0 {
								// Anew, as the blocks in it share the keys'.
								*h.folds = append(slices.Clone((*h.folds)[:k]), (*h.folds)[k+1:]...)
							}
						}
						h.details = h.details[:n-1]
					}
				}
				b = h.adjust(base)
				b.kind, b.level = markdownParagraph, 0
			} else if !t.self {
				// A paragraph ends at the start of another, its end tag
				// left out.
				if n := len(h.boxes); t.name == "p" && n > 0 && h.boxes[n-1].name == "p" {
					h.boxes = h.boxes[:n-1]
				}
				h.boxes = append(h.boxes, markdownBox{t.name, markdownAligned(t, h.align())})
				switch t.name {
				case "ul", "ol":
					l := markdownHTMLList{width: 1.5}
					if t.name == "ol" {
						l.next = 1
						if n, err := strconv.Atoi(strings.TrimSpace(t.attrs["start"])); err == nil {
							l.next = n
						}
						// As wide as the number of its last item, its
						// items at most as many as the <li>s after it.
						items := 0
						for _, u := range tags[i+1:] {
							if u.name == "li" && !u.end {
								items++
							}
						}
						l.width = max(l.width, float32(len(strconv.Itoa(l.next+max(items, 1)-1))+1)*0.65)
					}
					h.lists, h.marker = append(h.lists, l), ""
				case "li":
					if n := len(h.lists); n > 0 {
						l := &h.lists[n-1]
						h.marker = "•"
						if l.next > 0 {
							h.marker = strconv.Itoa(l.next) + "."
							l.next++
						}
					}
				case "blockquote":
					h.quote++
				case "details":
					_, shown := t.attrs["open"]
					h.details = append(h.details, markdownDetails{open: shown})
				}
				b = h.adjust(base)
				if len(t.name) == 2 && t.name[0] == 'h' && t.name[1] >= '1' && t.name[1] <= '6' {
					b.kind, b.level = markdownHeading, int(t.name[1]-'0')
				}
			}
			// A summary is bold, as its marker shows on GitHub.
			if t.name == "summary" {
				style.bold = !t.end
			}
		default:
			markdownStyleTag(t, &style, &open)
		}
	}
	flush()
}

// endTable emits the table open, its caption above it, its rows those of
// a table of Markdown, the first its header when all of its cells are.
func (h *markdownHTML) endTable() {
	t := h.table
	h.table = nil
	if len(t.caption) > 0 {
		c := t.base
		c.kind, c.align, c.runs = markdownParagraph, ui.Center, t.caption
		h.emit(c)
	}
	cols := 0
	for _, row := range t.rows {
		n := 0
		for _, cell := range row {
			n += cell.span
		}
		cols = max(cols, n)
	}
	if cols == 0 {
		return
	}
	var rows []markdownBlock
	body := 0
	for k, row := range t.rows {
		if len(row) == 0 {
			continue
		}
		rb := t.base
		rb.kind, rb.runs = markdownTable, nil
		head := k == 0 && !slices.ContainsFunc(row, func(c markdownHTMLCell) bool { return !c.head })
		if head {
			rb.kind = markdownTableHeader
		} else {
			body++
			rb.row = body
		}
		for _, cell := range row {
			runs := markdownMerge(cell.runs)
			// Its lines without the spaces around them.
			for i := range runs {
				if runs[i].bitmap == nil {
					runs[i].text = strings.ReplaceAll(strings.ReplaceAll(runs[i].text, " \n", "\n"), "\n ", "\n")
				}
			}
			// A picture stands on lines of its own: the breaks by it go.
			for i := range runs {
				if runs[i].bitmap != nil {
					continue
				}
				if i > 0 && runs[i-1].bitmap != nil {
					runs[i].text = strings.TrimLeft(runs[i].text, " \n")
				}
				if i+1 < len(runs) && runs[i+1].bitmap != nil {
					runs[i].text = strings.TrimRight(runs[i].text, " \n")
				}
			}
			runs = slices.DeleteFunc(runs, func(r markdownInline) bool { return r.bitmap == nil && r.text == "" })
			runs = markdownTrimRuns(runs, 0)
			for len(runs) > 0 && runs[len(runs)-1].bitmap == nil && strings.TrimRight(runs[len(runs)-1].text, " \n") == "" {
				runs = runs[:len(runs)-1]
			}
			if n := len(runs); n > 0 && runs[n-1].bitmap == nil {
				runs[n-1].text = strings.TrimRight(runs[n-1].text, " \n")
			}
			if cell.head {
				for i := range runs {
					runs[i].bold = true
				}
			}
			for range cell.span {
				rb.cells = append(rb.cells, runs)
				rb.aligns = append(rb.aligns, cell.align)
				runs = nil
			}
		}
		for len(rb.cells) < cols {
			rb.cells = append(rb.cells, nil)
			rb.aligns = append(rb.aligns, east.AlignNone)
		}
		rows = append(rows, rb)
	}
	if len(rows) > 0 {
		h.rows(rows)
	}
}

// finish emits what of the HTML is still open at the end of the text: a
// table or a <pre> not ended.
func (h *markdownHTML) finish(base markdownBlock) {
	if h.pre != nil {
		h.block("</pre>", base)
	}
	if h.table != nil {
		h.table.inner = 0
		h.endTable()
	}
}
