package main

import (
	"unicode"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// isCJK reports whether r is a Chinese, Japanese or Korean character, or
// their punctuation, which sits next to other text with no space between.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef
}

// scanCJKDelimiter is goldmark's ScanDelimiter with the CJK-friendly
// flanking rule of markdown-it-cjk-friendly: a run after punctuation can
// still close, and one before punctuation can still open, when CJK text
// is on either side, so **"准新机"**标着 and **调用。**qodercli are bold
// though CJK has no spaces.
func scanCJKDelimiter(line []byte, before rune, processor parser.DelimiterProcessor) *parser.Delimiter {
	c := line[0]
	if !processor.IsDelimiter(c) {
		return nil
	}
	j := 0
	for ; j < len(line) && line[j] == c; j++ {
	}
	after := rune(' ')
	if j != len(line) {
		after = util.ToRune(line, j)
	}
	beforePunct, beforeSpace := util.IsPunctRune(before), util.IsSpaceRune(before)
	afterPunct, afterSpace := util.IsPunctRune(after), util.IsSpaceRune(after)
	left := !afterSpace && (!afterPunct || beforeSpace || beforePunct || isCJK(before) || isCJK(after))
	right := !beforeSpace && (!beforePunct || afterSpace || afterPunct || isCJK(after) || isCJK(before))
	canOpen, canClose := left, right
	if c == '_' {
		canOpen = left && (!right || beforePunct)
		canClose = right && (!left || afterPunct)
	}
	return parser.NewDelimiter(canOpen, canClose, j, c, processor)
}

// cjkDelimiters is a delimiter processor for one kind of run.
type cjkDelimiters struct {
	chars string
	node  func(consumes int) ast.Node
}

func (d cjkDelimiters) IsDelimiter(b byte) bool {
	for i := range len(d.chars) {
		if d.chars[i] == b {
			return true
		}
	}
	return false
}

func (d cjkDelimiters) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (d cjkDelimiters) OnMatch(consumes int) ast.Node { return d.node(consumes) }

var (
	emphasisDelimiters      = cjkDelimiters{"*_", func(consumes int) ast.Node { return ast.NewEmphasis(consumes) }}
	strikethroughDelimiters = cjkDelimiters{"~", func(int) ast.Node { return east.NewStrikethrough() }}
)

// cjkEmphasisParser parses *, _ and ~~ runs with scanCJKDelimiter, in
// place of goldmark's emphasis and strikethrough parsers.
type cjkEmphasisParser struct{ delimiters cjkDelimiters }

func (p cjkEmphasisParser) Trigger() []byte { return []byte(p.delimiters.chars) }

func (p cjkEmphasisParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := scanCJKDelimiter(line, before, p.delimiters)
	if node == nil {
		return nil
	}
	// Like goldmark's strikethrough, only ~ and ~~ strike, not ~~~.
	if p.delimiters.chars == "~" && (node.OriginalLength > 2 || before == '~') {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}
