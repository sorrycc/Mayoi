package main

import (
	"bytes"
	"strings"
)

// Obsidian's comments, %%like this%%, within a line or over lines, are
// notes to the writer: faint in Live Preview, and gone from the Reading
// view. In code they are code.

// markdownComments are where the comments of src are, in bytes, each with
// its %% marks. One not closed is no comment.
func markdownComments(src []byte) [][2]int {
	if !bytes.Contains(src, []byte("%%")) {
		return nil
	}
	var out [][2]int
	var fence byte
	fenceLen := 0
	open := -1
	for start := 0; start <= len(src); {
		end := len(src)
		if i := bytes.IndexByte(src[start:], '\n'); i >= 0 {
			end = start + i
		}
		line := src[start:end]
		next := end + 1
		if open < 0 {
			rest := strings.TrimLeft(string(line), " ")
			if fence != 0 {
				if fenceCloses(strings.TrimRight(rest, "\r"), fence, fenceLen) {
					fence = 0
				}
				start = next
				continue
			}
			if fenceOpens(rest) {
				fence, fenceLen = rest[0], fenceRun(rest)
				start = next
				continue
			}
		}
		for i := 0; i < len(line); {
			switch {
			case open < 0 && line[i] == '`':
				// A span of code, to the run of as many backticks.
				n := 1
				for i+n < len(line) && line[i+n] == '`' {
					n++
				}
				if j := bytes.Index(line[i+n:], line[i:i+n]); j >= 0 {
					i += n + j + n
				} else {
					i += n
				}
			case line[i] == '%' && i+1 < len(line) && line[i+1] == '%':
				if open < 0 {
					open = start + i
				} else {
					out = append(out, [2]int{open, start + i + 2})
					open = -1
				}
				i += 2
			default:
				i++
			}
		}
		start = next
	}
	return out
}

// markdownStripComments drops the comments of src, but for their line
// breaks, which keep the lines where they are, and a space that would
// double where one was within a line.
func markdownStripComments(src []byte) []byte {
	comments := markdownComments(src)
	if len(comments) == 0 {
		return src
	}
	out := make([]byte, 0, len(src))
	at := 0
	for _, c := range comments {
		out = append(out, src[at:c[0]]...)
		for _, ch := range src[c[0]:c[1]] {
			if ch == '\n' {
				out = append(out, ch)
			}
		}
		at = c[1]
		if len(out) > 0 && out[len(out)-1] == ' ' && at < len(src) && src[at] == ' ' {
			at++
		}
	}
	return append(out, src[at:]...)
}
