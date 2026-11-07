package main

import (
	"html"
	"strings"
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

// markdownHTMLBlocks are the elements of HTML that start a block of the
// preview of their own, as <p> or <h1>.
var markdownHTMLBlocks = map[string]bool{
	"p": true, "div": true, "center": true, "section": true, "article": true, "header": true, "footer": true, "nav": true, "main": true, "aside": true,
	"figure": true, "figcaption": true, "details": true, "summary": true, "blockquote": true, "pre": true, "address": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "caption": true, "hr": true,
}

// markdownAligned is whether a block of HTML is centered, by its align,
// or else as the block it is in is.
func markdownAligned(t markdownTag, outer bool) bool {
	if t.name == "center" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t.attrs["align"])) {
	case "center", "middle":
		return true
	case "left", "right", "justify":
		return false
	}
	return outer
}

// markdownBox is a block of HTML open, and whether it is centered.
type markdownBox struct {
	name   string
	center bool
}

func markdownCentered(boxes []markdownBox) bool {
	return len(boxes) > 0 && boxes[len(boxes)-1].center
}

// markdownHTMLBlock emits the blocks of a block of HTML, from b: its text,
// its spaces collapsed, styled as its inline tags say, its pictures by
// image, a block for each of its paragraphs and headings, those in boxes
// centered. Its other tags go, their text staying.
func markdownHTMLBlock(raw string, b markdownBlock, boxes *[]markdownBox, emit func(markdownBlock), anchor func(*markdownBlock), image func(markdownTag, markdownInline) markdownInline) {
	b.kind, b.center = markdownParagraph, markdownCentered(*boxes)
	var style markdownInline
	var open []markdownOpen
	// space is whether the text so far ends in a space or a break, or is
	// none, for a space after it to go.
	space := true
	flush := func() {
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
			anchor(&b)
		}
		emit(b)
		b.runs, b.slug, space = nil, "", true
	}
	for _, t := range markdownTags(raw) {
		switch {
		case t.name == "":
			var text strings.Builder
			for _, r := range t.text {
				if strings.ContainsRune(" \t\r\n\f", r) {
					if !space {
						text.WriteByte(' ')
					}
					space = true
					continue
				}
				text.WriteRune(r)
				space = false
			}
			if text.Len() > 0 {
				r := style
				r.text = text.String()
				b.runs = append(b.runs, r)
			}
		case t.name == "br" && !t.end:
			r := style
			r.text = "\n"
			b.runs = append(b.runs, r)
			space = true
		case t.name == "img" && !t.end:
			b.runs = append(b.runs, image(t, style))
			space = false
		case (t.name == "td" || t.name == "th") && !t.end:
			if !space {
				r := style
				r.text = " "
				b.runs = append(b.runs, r)
				space = true
			}
		case t.name == "hr" && !t.end:
			flush()
			rule := b
			rule.kind, rule.runs = markdownRule, nil
			emit(rule)
		case markdownHTMLBlocks[t.name]:
			flush()
			if t.end {
				for i := len(*boxes) - 1; i >= 0; i-- {
					if (*boxes)[i].name == t.name {
						*boxes = (*boxes)[:i]
						break
					}
				}
				b.kind, b.level = markdownParagraph, 0
			} else if !t.self {
				// A paragraph ends at the start of another, its end tag
				// left out.
				if n := len(*boxes); t.name == "p" && n > 0 && (*boxes)[n-1].name == "p" {
					*boxes = (*boxes)[:n-1]
				}
				*boxes = append(*boxes, markdownBox{t.name, markdownAligned(t, markdownCentered(*boxes))})
				if len(t.name) == 2 && t.name[0] == 'h' && t.name[1] >= '1' && t.name[1] <= '6' {
					b.kind, b.level = markdownHeading, int(t.name[1]-'0')
				}
			}
			// A summary is bold, as its marker shows on GitHub.
			if t.name == "summary" {
				style.bold = !t.end
			}
			b.center = markdownCentered(*boxes)
		default:
			markdownStyleTag(t, &style, &open)
		}
	}
	flush()
}
