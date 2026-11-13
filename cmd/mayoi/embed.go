package main

import (
	"bytes"
	"path/filepath"
	"strings"

	"mayoi/internal/vfs"

	"github.com/yuin/goldmark/ast"
)

// A note embedded, ![[note]], ![[note#heading]] or ![[note#^id]] alone in
// its paragraph, shows in place, as Obsidian shows it: the note, the part
// of it under the heading, or the block of the id, after a bar of the
// accent, under a line naming it, which a click opens. A note embedded in one embedded is a link, and so is one
// not found.

// markdownEmbedMax is the most of a note, in bytes, that shows embedded.
const markdownEmbedMax = 256 << 10

// markdownEmbedDepth is how deep notes embed in notes embedded.
const markdownEmbedDepth = 1

// markdownEmbedOf is the wiki link that embeds a note in paragraph n,
// alone in it, but for white space.
func markdownEmbedOf(n ast.Node, source []byte) (*wikiLinkNode, bool) {
	var link *wikiLinkNode
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch x := ch.(type) {
		case *wikiLinkNode:
			if link != nil || !x.embed || x.target == "" || isImage(x.target) {
				return nil, false
			}
			link = x
		case *ast.Text:
			if len(bytes.TrimSpace(x.Segment.Value(source))) > 0 {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return link, link != nil
}

// markdownEmbedFile is the note a wiki link embeds, "" for none, as a
// file of Markdown found from the note at path.
func markdownEmbedFile(root, path string, l *wikiLinkNode) string {
	file := wikiLinkFile(root, path, l.target)
	if file == "" || !isMarkdownPath(file) || filepath.Clean(file) == filepath.Clean(path) {
		return ""
	}
	return file
}

// markdownEmbedText is the text of the note file that shows embedded: all
// of it, or the part under heading, from it to the next heading as large
// or larger; false when the note cannot be read, or has no such heading.
func markdownEmbedText(file, heading string) ([]byte, bool) {
	info, err := vfs.Stat(file)
	if err != nil || info.Size() > markdownEmbedMax {
		return nil, false
	}
	data, err := vfs.ReadFile(file)
	if err != nil {
		return nil, false
	}
	if heading == "" {
		return data, true
	}
	if id, ok := strings.CutPrefix(heading, "^"); ok {
		return markdownBlockText(data, id)
	}
	line := markdownHeadingLine(data, heading)
	if line < 0 {
		return nil, false
	}
	lines := strings.SplitAfter(string(data), "\n")
	if line >= len(lines) {
		return nil, false
	}
	level := markdownHashes(lines[line])
	end := len(lines)
	var fence byte
	fenceLen := 0
	for i := line + 1; i < len(lines); i++ {
		rest := strings.TrimLeft(lines[i], " ")
		switch {
		case fence != 0:
			if fenceCloses(strings.TrimRight(rest, "\r\n"), fence, fenceLen) {
				fence = 0
			}
			continue
		case fenceOpens(rest):
			fence, fenceLen = rest[0], fenceRun(rest)
			continue
		}
		if h := markdownHashes(lines[i]); h > 0 && (level == 0 || h <= level) {
			end = i
			break
		}
	}
	return []byte(strings.Join(lines[line:end], "")), true
}

// markdownHashes is the level of the heading of # line opens, 0 for none.
func markdownHashes(line string) int {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return 0
	}
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n < len(t) && t[n] != ' ' && t[n] != '\t' && t[n] != '\n' && t[n] != '\r' {
		return 0
	}
	return n
}

// markdownEmbedName is how the line over a note embedded names it: its
// name, and the heading it shows the part under.
func markdownEmbedName(file, heading string) string {
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if heading != "" {
		name += " › " + heading
	}
	return name
}

// markdownBlockID is where the id of a block, ^id, starts at the end of
// line, after white space or alone on it, as Obsidian marks a block to
// link to; -1 for none.
func markdownBlockID(line []byte) int {
	i := len(line)
	for i > 0 && (line[i-1] == '-' || line[i-1] >= '0' && line[i-1] <= '9' || line[i-1]|0x20 >= 'a' && line[i-1]|0x20 <= 'z') {
		i--
	}
	if i == len(line) || i == 0 || line[i-1] != '^' {
		return -1
	}
	at := i - 1
	if at > 0 && line[at-1] != ' ' && line[at-1] != '\t' {
		return -1
	}
	return at
}

// markdownBlockIDIn is where the id of a block starts in the run of text
// src[s:e], which ends its line, -1 for none.
func markdownBlockIDIn(src []byte, s, e int) int {
	if e < len(src) && src[e] != '\n' && src[e] != '\r' {
		return -1
	}
	at := markdownBlockID(src[s:e])
	switch {
	case at < 0:
		return -1
	case at == 0 && s > 0 && src[s-1] != ' ' && src[s-1] != '\t' && src[s-1] != '\n':
		return -1
	}
	return s + at
}

// markdownBlockText is the block of data whose id is id, without it: the
// paragraph or the item of a list whose last line ends in ^id, with the
// items in it, or the block above a line of ^id alone; false for none.
func markdownBlockText(data []byte, id string) ([]byte, bool) {
	if id == "" {
		return nil, false
	}
	lines := strings.SplitAfter(string(data), "\n")
	blank := func(i int) bool { return strings.TrimSpace(lines[i]) == "" }
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		at := markdownBlockID([]byte(body))
		if at < 0 || body[at+1:] != id {
			continue
		}
		if strings.TrimSpace(body[:at]) == "" {
			// ^id alone: the block above it.
			end := i
			for end > 0 && blank(end-1) {
				end--
			}
			start := end
			for start > 0 && !blank(start-1) {
				start--
			}
			if start == end {
				return nil, false
			}
			return []byte(strings.Join(lines[start:end], "")), true
		}
		lines[i] = strings.TrimRight(body[:at], " \t") + line[len(body):]
		if m := liveListMarker.FindStringSubmatchIndex(lines[i]); m != nil {
			// An item, with those under it, from its marker.
			indent := len(lines[i][:m[4]]) - len(strings.TrimLeft(lines[i][:m[4]], " \t"))
			end := i + 1
			for end < len(lines) && !blank(end) {
				rest := strings.TrimLeft(lines[end], " \t")
				if len(lines[end])-len(rest) <= indent {
					break
				}
				end++
			}
			out := make([]string, 0, end-i)
			for _, l := range lines[i:end] {
				out = append(out, l[min(indent, len(l)-len(strings.TrimLeft(l, " \t"))):])
			}
			return []byte(strings.Join(out, "")), true
		}
		// A paragraph, from the line after the blank line, heading or item
		// above it.
		opens := func(l string) bool { return markdownHashes(l) > 0 || liveListMarker.MatchString(l) }
		start := i
		for start > 0 && !blank(start-1) && !opens(lines[start-1]) {
			start--
		}
		return []byte(strings.Join(lines[start:i+1], "")), true
	}
	return nil, false
}
