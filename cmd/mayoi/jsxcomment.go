package main

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

// Comments in JSX children are JSX expressions, {/* like this */}: a
// line comment there is text, and shows on the page. A line inside a tag
// or in JavaScript is commented as JavaScript is.

// jsxPlace is where a line of a JSX or TSX file is: in its children, in a
// tag, or in JavaScript.
type jsxPlace uint8

const (
	jsxCodePlace jsxPlace = iota
	jsxTagPlace
	jsxChildrenPlace
)

// jsxFrame is a tag, a run of children, or a JavaScript expression in a
// JSX file, open at a token.
type jsxFrame struct {
	kind      jsxFrameKind
	closing   bool // a tag's </name>
	selfClose bool // a tag's <name />
	generic   bool // a TypeScript generic's <T,>, not a tag's <>
	braces    int  // an expression's {..}, 1 for the one JSX makes
}

type jsxFrameKind uint8

const (
	jsxTagFrame jsxFrameKind = iota
	jsxChildrenFrame
	jsxExprFrame
)

// jsxDocument reports whether a document is JSX or TSX. Chroma calls a
// .tsx file's lexer TypeScript, as it calls a .ts file's.
func jsxDocument(d *document) bool {
	switch strings.ToLower(filepath.Ext(d.path)) {
	case ".tsx", ".jsx":
		return true
	}
	if d.hl == nil {
		return false
	}
	switch highlight.Name(d.hl.Lexer()) {
	case "TSX", "react", "JSX":
		return true
	}
	return false
}

// maxJSXPrefix is the most text lexed to tell where a line is in a JSX
// file: a large file's earlier lines should not make ⌘/ wait a second.
const maxJSXPrefix = 256 << 10

// jsxChildrenAt reports whether a line of a JSX or TSX document is in its
// children, where a comment is {/* ... */}. A line after maxJSXPrefix is
// left to the line comment, which is quick.
func jsxChildrenAt(d *document, snap ui.TextSnapshot, line int) bool {
	if !jsxDocument(d) || d.hl == nil {
		return false
	}
	s, _ := snap.LineRange(line)
	prefix := snap.Slice(0, s)
	if len(prefix) > maxJSXPrefix {
		return false
	}
	return jsxContext(d.hl.Lexer(), prefix) == jsxChildrenPlace
}

// jsxContext reads where the text before a line leaves a JSX file.
func jsxContext(l chroma.Lexer, prefix string) jsxPlace {
	if l == nil {
		return jsxCodePlace
	}
	it, err := l.Tokenise(nil, prefix)
	if err != nil {
		return jsxCodePlace
	}
	var toks []chroma.Token
	for t := it(); t != chroma.EOF; t = it() {
		toks = append(toks, t)
	}
	var stack []jsxFrame
	for i, tok := range toks {
		if tok.Type == chroma.Punctuation {
			jsxPunctuation(&stack, tok.Value, toks, i)
			continue
		}
		top, ok := jsxTop(&stack, jsxTagFrame)
		if !ok || top.closing {
			continue
		}
		switch {
		case tok.Value == "extends" && jsxTypeAfter(toks, i):
			top.generic = true
		case tok.Type == chroma.Operator && tok.Value == "=" && jsxNameTagBefore(toks, i):
			top.generic = true
		}
	}
	if len(stack) == 0 {
		return jsxCodePlace
	}
	switch stack[len(stack)-1].kind {
	case jsxChildrenFrame:
		return jsxChildrenPlace
	case jsxTagFrame:
		return jsxTagPlace
	}
	return jsxCodePlace
}

// jsxPunctuation follows the punctuation of a token, which coalescing may
// have run together, as ">}" after a tag and an expression.
func jsxPunctuation(stack *[]jsxFrame, s string, toks []chroma.Token, at int) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			if i+1 < len(s) && s[i+1] == '/' {
				jsxPushTag(stack, true)
				i++
			} else if jsxTagNameAhead(s, i, toks, at, jsxInChildren(stack)) {
				jsxPushTag(stack, false)
			}
		case '/':
			if top, ok := jsxTop(stack, jsxTagFrame); ok && !top.closing {
				top.selfClose = true
			}
		case ',':
			if top, ok := jsxTop(stack, jsxTagFrame); ok && !top.closing {
				top.generic = true
			}
		case '>':
			jsxEndTag(stack)
		case '{':
			jsxOpenExpr(stack)
		case '}':
			jsxCloseExpr(stack)
		}
	}
}

// jsxInChildren reports whether the line so far is in JSX children.
func jsxInChildren(stack *[]jsxFrame) bool {
	return len(*stack) > 0 && (*stack)[len(*stack)-1].kind == jsxChildrenFrame
}

// jsxTagNameAhead reports whether a < at i starts a tag: a name or the
// empty tag <> follows it, not the < of a comparison.
func jsxTagNameAhead(s string, i int, toks []chroma.Token, at int, inChildren bool) bool {
	if !inChildren && jsxValueBefore(s, i, toks, at) {
		return false // Array<T>, React.FC<T>, foo<T>, not <foo>
	}
	if i+1 < len(s) {
		next := s[i+1]
		return next == '>' || isJSXNameStart(next)
	}
	if at+1 >= len(toks) {
		return false
	}
	next := toks[at+1]
	if next.Type == chroma.NameTag {
		return true
	}
	return next.Type == chroma.Punctuation && strings.HasPrefix(next.Value, ">")
}

// jsxTypeAfter reports whether a type follows the extends at at, as a
// TypeScript type parameter's does, not a JSX attribute's = or tag end.
func jsxTypeAfter(toks []chroma.Token, at int) bool {
	for i := at + 1; i < len(toks); i++ {
		if toks[i].Type == chroma.Text {
			continue
		}
		next := toks[i]
		if next.Type == chroma.Operator && next.Value == "=" {
			return false
		}
		if next.Type == chroma.Punctuation && (strings.HasPrefix(next.Value, ">") || strings.HasPrefix(next.Value, "/>")) {
			return false
		}
		return true
	}
	return false
}

// jsxValueBefore reports whether a value ends right before the < at i of
// a punctuation token, so the < opens type arguments or compares, as in
// Array<T>, a?.<T>() and f(x)<y, and does not start a tag, as the < of
// render(<App> does.
func jsxValueBefore(s string, i int, toks []chroma.Token, at int) bool {
	if i > 0 {
		switch s[i-1] {
		case ')', ']', '.':
			return true
		}
		return false
	}
	return jsxNameBefore(toks, at)
}

// jsxNameBefore reports whether a name or type token is immediately
// before the < at at, as a TypeScript type argument's is.
func jsxNameBefore(toks []chroma.Token, at int) bool {
	for i := at - 1; i >= 0; i-- {
		if toks[i].Type == chroma.Text {
			continue
		}
		t := toks[i].Type
		return t.InCategory(chroma.Name) || t == chroma.KeywordType
	}
	return false
}

// jsxNameTagBefore reports whether the token before at is a tag's name,
// as a TypeScript type parameter's is, not a JSX attribute's.
func jsxNameTagBefore(toks []chroma.Token, at int) bool {
	for i := at - 1; i >= 0; i-- {
		if toks[i].Type == chroma.Text {
			continue
		}
		return toks[i].Type == chroma.NameTag
	}
	return false
}

func isJSXNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$'
}

func jsxPushTag(stack *[]jsxFrame, closing bool) {
	*stack = append(*stack, jsxFrame{kind: jsxTagFrame, closing: closing})
}

func jsxEndTag(stack *[]jsxFrame) {
	if len(*stack) == 0 || (*stack)[len(*stack)-1].kind != jsxTagFrame {
		return
	}
	tag := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	switch {
	case tag.selfClose, tag.generic:
	case tag.closing:
		jsxPopChildren(stack)
	default:
		*stack = append(*stack, jsxFrame{kind: jsxChildrenFrame})
	}
}

func jsxPopChildren(stack *[]jsxFrame) {
	for i := len(*stack) - 1; i >= 0; i-- {
		if (*stack)[i].kind == jsxChildrenFrame {
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return
		}
	}
}

func jsxOpenExpr(stack *[]jsxFrame) {
	if len(*stack) == 0 {
		return
	}
	switch top := &(*stack)[len(*stack)-1]; top.kind {
	case jsxTagFrame, jsxChildrenFrame:
		*stack = append(*stack, jsxFrame{kind: jsxExprFrame, braces: 1})
	case jsxExprFrame:
		top.braces++
	}
}

func jsxCloseExpr(stack *[]jsxFrame) {
	if len(*stack) == 0 || (*stack)[len(*stack)-1].kind != jsxExprFrame {
		return
	}
	top := &(*stack)[len(*stack)-1]
	if top.braces--; top.braces <= 0 {
		*stack = (*stack)[:len(*stack)-1]
	}
}

func jsxTop(stack *[]jsxFrame, kind jsxFrameKind) (*jsxFrame, bool) {
	if len(*stack) == 0 || (*stack)[len(*stack)-1].kind != kind {
		return nil, false
	}
	return &(*stack)[len(*stack)-1], true
}

const (
	jsxOpen  = "{/*"
	jsxClose = "*/}"
)

// jsxCommentEndsAt reports whether a line ends a JSX comment, as the
// last line of a block comment does.
func jsxCommentEndsAt(snap ui.TextSnapshot, line int) bool {
	s, e := snap.LineRange(line)
	return strings.HasSuffix(strings.TrimSpace(snap.Slice(s, e)), jsxClose)
}

// jsxCommentLine is a line of a selection: where its text starts and ends,
// without the spaces around it.
type jsxCommentLine struct {
	s, e    int // runes, of the text without the spaces around it
	trimmed string
}

// toggleJSXComment wraps the lines of the selection in one JSX comment,
// or takes it away when they are wrapped already.
func toggleJSXComment(d *editor, snap ui.TextSnapshot, first, last int) {
	var lines []jsxCommentLine
	for i := first; i <= last; i++ {
		s, e := snap.LineRange(i)
		text := snap.Slice(s, e)
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		s += utf8.RuneCountInString(leading(text))
		e -= utf8.RuneCountInString(text) - utf8.RuneCountInString(strings.TrimRight(text, " \t"))
		lines = append(lines, jsxCommentLine{s, e, trimmed})
	}
	if len(lines) == 0 {
		return
	}
	firstLine, lastLine := lines[0], lines[len(lines)-1]
	sel := d.sel
	var edits []textEdit

	// A comment on each line.
	all := true
	for _, l := range lines {
		if !strings.HasPrefix(l.trimmed, jsxOpen) || !strings.HasSuffix(l.trimmed, jsxClose) {
			all = false
			break
		}
	}
	if all {
		for i := len(lines) - 1; i >= 0; i-- {
			l := lines[i]
			suffixAt, suffixN := jsxCommentSuffix(l)
			edits = append(edits, textEdit{suffixAt, suffixAt + suffixN, ""})
			sel = shiftSel(sel, suffixAt, -suffixN)
			prefixAt, prefixN := jsxCommentPrefix(l)
			edits = append(edits, textEdit{prefixAt, prefixAt + prefixN, ""})
			sel = shiftSel(sel, prefixAt, -prefixN)
		}
		d.edit(sel, edits...)
		return
	}

	// One comment around the lines.
	if strings.HasPrefix(firstLine.trimmed, jsxOpen) && strings.HasSuffix(lastLine.trimmed, jsxClose) &&
		!strings.HasSuffix(firstLine.trimmed, jsxClose) && !strings.HasPrefix(lastLine.trimmed, jsxOpen) {
		suffixAt, suffixN := jsxCommentSuffix(lastLine)
		edits = append(edits, textEdit{suffixAt, suffixAt + suffixN, ""})
		sel = shiftSel(sel, suffixAt, -suffixN)
		prefixAt, prefixN := jsxCommentPrefix(firstLine)
		edits = append(edits, textEdit{prefixAt, prefixAt + prefixN, ""})
		sel = shiftSel(sel, prefixAt, -prefixN)
		d.edit(sel, edits...)
		return
	}

	// A block that already holds a JSX comment cannot take another:
	// its */} would end the new one early.
	start, _ := snap.LineRange(first)
	_, end := snap.LineRange(last)
	if strings.Contains(snap.Slice(start, end), jsxClose) {
		return
	}

	open, close := jsxOpen+" ", " "+jsxClose
	if len(lines) == 1 {
		l := lines[0]
		edits = append(edits, textEdit{l.e, l.e, close})
		sel = insertSel(sel, l.e, utf8.RuneCountInString(close))
		edits = append(edits, textEdit{l.s, l.s, open})
		sel = insertSel(sel, l.s, utf8.RuneCountInString(open))
	} else {
		edits = append(edits, textEdit{lastLine.e, lastLine.e, close})
		sel = insertSel(sel, lastLine.e, utf8.RuneCountInString(close))
		edits = append(edits, textEdit{firstLine.s, firstLine.s, open})
		sel = insertSel(sel, firstLine.s, utf8.RuneCountInString(open))
	}
	d.edit(sel, edits...)
}

func jsxCommentPrefix(l jsxCommentLine) (int, int) {
	n := utf8.RuneCountInString(jsxOpen)
	if strings.HasPrefix(l.trimmed, jsxOpen+" ") {
		n++
	}
	return l.s, n
}

func jsxCommentSuffix(l jsxCommentLine) (int, int) {
	n := utf8.RuneCountInString(jsxClose)
	if strings.HasSuffix(l.trimmed, " "+jsxClose) {
		n++
	}
	return l.e - n, n
}
