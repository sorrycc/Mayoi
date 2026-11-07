package main

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

// The editor closes the brackets and quotes typed: an opening one puts
// its closing one after the caret, which typing it again then steps over,
// and deleting the opening one deletes too while nothing is between them.
// Typed over a selection, they go around it, as in Markdown do the marks
// of emphasis, highlight, strikethrough and math, which typed again make
// the double ones, as ** and ==. The text area takes the
// typing itself, so the editor finds what was typed in the edit it made
// and adds to that edit, in the same step of undo.

var autoPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '"': '"', '\'': '\'', '`': '`'}

// markdownWraps are the marks that go around a selection in Markdown only.
const markdownWraps = "=*_~$"

// autoClose follows an edit of typing, from text old with selection sel
// to text now, and reports whether it edited the text further.
func (d *editor) autoClose(old, now ui.TextSnapshot, sel [2]int) bool {
	if d.hl == nil || d.hl.Lexer() == nil {
		d.closers = d.closers[:0]
		return false
	}
	a, b := min(sel[0], sel[1]), max(sel[0], sel[1])
	n, m := old.Len(), now.Len()
	if b > n {
		return false
	}
	switch {
	case a == b && m == n+1:
		// One rune typed at the caret.
		r := runeAt(now, a)
		d.shiftClosers(a, 1)
		return d.typed(now, a, r)
	case a == b && m == n-1 && a > 0:
		// One rune deleted before the caret.
		r := runeAt(old, a-1)
		next := runeAt(old, a)
		paired := d.isCloser(a)
		d.shiftClosers(a-1, -1)
		if c, ok := autoPairs[r]; ok && next == c && paired && now.Slice(a-1, a) == string(c) {
			d.dropCloser(a - 1)
			d.buf.Replace(a-1, a, "")
			d.selectRange(a-1, a-1, false)
			return true
		}
	case a < b && m == n-(b-a)+1:
		// One rune typed over a selection: an opening one goes around it.
		r := runeAt(now, a)
		c, ok := autoPairs[r]
		if !ok && strings.ContainsRune(markdownWraps, r) && strings.EqualFold(highlight.Name(d.hl.Lexer()), "markdown") {
			c, ok = r, true
		}
		if !ok || old.LineAt(a) != old.LineAt(b) && r != '(' && r != '[' && r != '{' {
			break
		}
		inner := old.Slice(a, b)
		d.buf.Replace(a+1, a+1, inner+string(c))
		k := utf8.RuneCountInString(inner)
		d.selectRange(a+1, a+1+k, false)
		d.sel = [2]int{a + 1, a + 1 + k}
		return true
	default:
		d.closers = d.closers[:0]
	}
	return false
}

// typed follows rune r typed at offset at of text now.
func (d *editor) typed(now ui.TextSnapshot, at int, r rune) bool {
	next := runeAt(now, at+1)
	// Typing a closing one that the editor put there steps over it.
	if d.isCloser(at+1) && next == r {
		d.dropCloser(at + 1)
		d.buf.Replace(at+1, at+2, "")
		d.selectRange(at+1, at+1, false)
		d.sel = [2]int{at + 1, at + 1}
		return true
	}
	c, ok := autoPairs[r]
	if !ok {
		return false
	}
	// Only before the end of a line, a space or a closing bracket, so that
	// a bracket typed before a word does not close.
	if next != 0 && next != '\n' && !strings.ContainsRune(" \t)]};,:", next) {
		return false
	}
	if r == c {
		// A quote: not after a word, as an apostrophe is, or in a language
		// that does not quote with it.
		prev := runeAt(now, at-1)
		if at > 0 && (isWordRune(prev) || prev == r) {
			return false
		}
		switch highlight.Name(d.hl.Lexer()) {
		case "markdown", "Markdown", "reStructuredText", "plaintext", "TeX":
			return false
		}
		if r == '\'' && strings.Contains("Go Rust OCaml Haskell", highlight.Name(d.hl.Lexer())) {
			return false
		}
		if r == '`' && !strings.Contains("Go JavaScript TypeScript TSX JSX Markdown Bash Zsh Kotlin Scala", highlight.Name(d.hl.Lexer())) {
			return false
		}
	}
	d.buf.Replace(at+1, at+1, string(c))
	d.closers = append(d.closers, at+1)
	d.selectRange(at+1, at+1, false)
	d.sel = [2]int{at + 1, at + 1}
	return true
}

// shiftClosers moves the closing brackets the editor put after offset at
// by delta, as text is put in or taken out before them.
func (d *editor) shiftClosers(at, delta int) {
	for i, c := range d.closers {
		if c >= at {
			d.closers[i] = c + delta
		}
	}
}

// keepClosers forgets the closing brackets the editor put that an edit
// took out, as deleting forward does, which typing must not step over.
func (d *editor) keepClosers(snap ui.TextSnapshot) {
	d.closers = slices.DeleteFunc(d.closers, func(at int) bool {
		return !strings.ContainsRune(")]}\"'`", runeAt(snap, at))
	})
}

func (d *editor) isCloser(at int) bool {
	for _, c := range d.closers {
		if c == at {
			return true
		}
	}
	return false
}

func (d *editor) dropCloser(at int) {
	for i, c := range d.closers {
		if c == at {
			d.closers = append(d.closers[:i], d.closers[i+1:]...)
			return
		}
	}
}

// runeAt returns the rune at offset i of a text, 0 outside it.
func runeAt(snap ui.TextSnapshot, i int) rune {
	if i < 0 || i >= snap.Len() {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(snap.Slice(i, i+1))
	return r
}
