package main

import (
	"strings"
	"unicode/utf8"

	"mayoi/locales"
)

// trimEdits are the edits that remove the spaces and tabs at the ends of
// the lines of text, none when it has none. In Markdown, two or more
// spaces ending a line of text stay, as they break the line there. A line
// keep reports stays as it is.
func trimEdits(text string, markdown bool, keep func(line int) bool) []textEdit {
	var edits []textEdit
	// runes is how many runes come before the byte at from.
	runes, from := 0, 0
	for line, start := 0, 0; start <= len(text); line++ {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}
		ws := end
		for ws > start && (text[ws-1] == ' ' || text[ws-1] == '\t') {
			ws--
		}
		hardBreak := markdown && ws > start && strings.HasSuffix(text[ws:end], "  ")
		if ws < end && !hardBreak && (keep == nil || !keep(line)) {
			runes += utf8.RuneCountInString(text[from:ws])
			edits = append(edits, textEdit{runes, runes + end - ws, ""})
			runes += end - ws
			from = end
		}
		start = end + 1
	}
	return edits
}

// trimTrailing trims the spaces and tabs at the ends of the lines of an
// editor's document as one step of undo, the caret keeping its place in
// its text, and returns how many lines it trimmed. keepCaret leaves the
// line of the caret, as one typed there may go on.
func trimTrailing(d *editor, keepCaret bool) int {
	if d.locked() || d.log != nil {
		// A log document trims as its window is edited, not as a whole.
		return 0
	}
	doc := d.document
	md := isMarkdown(doc)
	snap := d.buf.Snapshot()
	// A text found without trailing whitespace is skipped until it changes.
	if doc.trimmed == (trimSeen{snap.Version(), md, true}) {
		return 0
	}
	var keep func(int) bool
	if keepCaret {
		caret := snap.LineAt(d.sel[1])
		keep = func(line int) bool { return line == caret }
	}
	edits := trimEdits(snap.String(), md, keep)
	if len(edits) > 0 {
		// Where an offset goes: back by what was trimmed before it, to the
		// end of its line's text from within what was trimmed.
		moved := func(x int) int {
			y := x
			for _, e := range edits {
				if e.start >= x {
					break
				}
				y -= min(x, e.end) - e.start
			}
			return y
		}
		d.edit([2]int{moved(d.sel[0]), moved(d.sel[1])}, edits...)
	}
	if !keepCaret {
		doc.trimmed = trimSeen{d.buf.Version(), md, true}
	}
	return len(edits)
}

// trimSeen is the version of a document's text last found without
// trailing whitespace, and whether it was trimmed as Markdown.
type trimSeen struct {
	version  uint64
	markdown bool
	ok       bool
}

// trimsOnSave reports whether saving a document trims trailing whitespace
// first.
func (a *app) trimsOnSave(d *document) bool {
	return a.prefs().TrimTrailingWhitespace && d.path != ""
}

// trimOnSave trims a document about to be written for the saves ws, as
// trimTrailingWhitespace asks: after the formatter, in the same write.
// Saves that all save themselves leave the caret's line.
func (a *app) trimOnSave(d *document, ws []saveWaiter) {
	if !a.trimsOnSave(d) {
		return
	}
	ed := a.docEditor(d)
	if ed == nil {
		return
	}
	auto := len(ws) > 0
	for _, w := range ws {
		auto = auto && w.auto
	}
	trimTrailing(ed, auto)
}

// trimActive trims the trailing whitespace of the document shown, saying
// what it did.
func (a *app) trimActive(d *editor) {
	if d.locked() {
		return
	}
	if n := trimTrailing(d, false); n > 0 {
		a.tell(note{message: locales.TrimmedWhitespace(n)})
	} else {
		a.tell(note{message: locales.TrimWhitespaceNone()})
	}
}
