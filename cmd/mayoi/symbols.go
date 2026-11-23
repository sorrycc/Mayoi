package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/symbols"
)

// symbolCache keeps the symbols of a version of a document, which go to
// symbol and the box of a file's name both list.
type symbolCache struct {
	doc     int
	version uint64
	// markdown is whether the symbols are the headings of a Markdown text,
	// which the language of the document may change without its text.
	markdown bool
	symbols  []symbols.Symbol
	names    []string
}

// markdownOf reports whether a document reads as Markdown, whose symbols
// are its headings rather than its declarations.
func markdownOf(d *editor) bool {
	return d != nil && d.hl != nil && strings.EqualFold(highlight.Name(d.hl.Lexer()), "markdown")
}

// symbolsOf returns the symbols of a document, and their names as the
// queries of the lists that show them match: the one place go to symbol
// and the box of a file's name read them from.
func (a *app) symbolsOf(d *editor) ([]symbols.Symbol, []string) {
	if d == nil {
		return nil, nil
	}
	sc := &a.quick.syms
	version, markdown := d.buf.Version(), markdownOf(d)
	if sc.doc != d.id || sc.version != version || sc.markdown != markdown || sc.symbols == nil {
		found := symbols.Find(d.buf.Snapshot(), markdown)
		if found == nil {
			found = []symbols.Symbol{}
		}
		names := make([]string, len(found))
		for i, s := range found {
			names[i] = s.Name
		}
		sc.doc, sc.version, sc.markdown = d.id, version, markdown
		sc.symbols, sc.names = found, names
	}
	return sc.symbols, sc.names
}

// documentSymbols returns the symbols of the document shown.
func (a *app) documentSymbols() ([]symbols.Symbol, []string) {
	return a.symbolsOf(a.activeEditor())
}

// goToSymbol queues an edit of an editor that goes to a symbol: the line it
// is on to the top of the view, with its name selected. The carets besides
// the first go, as a jump of the app's takes them away.
func (a *app) goToSymbol(d *editor, sym symbols.Symbol) {
	if d == nil {
		return
	}
	a.queueEdit(d, func(d *editor) {
		d.collapse()
		a.jumpToScope(d, sym.Line)
		s, _ := d.buf.Snapshot().LineRange(sym.Line)
		d.selectRange(s+sym.Col, s+sym.Col+utf8.RuneCountInString(sym.Name), false)
	})
}

// symbolIcon is the mark of a kind of symbol, and its color.
func (a *app) symbolIcon(kind string) (*ui.SVG, ui.Color) {
	p := a.pal
	switch kind {
	case "function", "method":
		return iconFunction, p.syntax[highlight.Function]
	case "class":
		return iconBox, p.syntax[highlight.Type]
	case "interface":
		return iconBraces, p.syntax[highlight.Type]
	case "type", "enum":
		return iconType, p.syntax[highlight.Type]
	case "heading":
		return iconHash, p.syntax[highlight.Heading]
	}
	return iconHash, p.muted
}
