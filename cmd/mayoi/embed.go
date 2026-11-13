package main

import (
	"bytes"
	"path/filepath"
	"strings"

	"mayoi/internal/vfs"

	"github.com/yuin/goldmark/ast"
)

// A note embedded, ![[note]] or ![[note#heading]] alone in its paragraph,
// shows in place, as Obsidian shows it: the note, or the part of it under
// the heading, after a bar of the accent, under a line naming it, which a
// click opens. A note embedded in one embedded is a link, and so is one
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
	if strings.HasPrefix(heading, "^") {
		return nil, false // a block's id, which needs Obsidian's index
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
