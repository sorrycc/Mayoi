package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/internal/symbols"
	"mayoi/locales"
)

// openFileSymbols opens the box of the symbols of the file of tab t of
// column col, below its name in the breadcrumbs, or closes it if it is the
// one open. Its symbols are those go to symbol lists.
func (a *app) openFileSymbols(col *column, t *colTab) {
	k := &a.crumbs
	if k.open && k.tab == t && k.self {
		k.open = false
		return
	}
	*k = crumbsPicker{
		open: true, col: col, tab: t, file: t.path(), self: true, gen: k.gen + 1,
		outline: ui.OutlineState[int]{Indent: treeStep}, sel: -1, focus: true, reveal: "sel",
	}
	k.outline.List.Selected = &k.sel
	a.crumbsSymbols(t.ed)
	// The symbol the reader is in, chosen, as the folder on the way to the
	// file is.
	if d := t.ed; d != nil {
		k.sel = symbolRowOf(k.syms, readingLine(d))
	}
}

// crumbsSymbols takes the symbols of a document for the box, read again as
// the text or the document changed since.
func (a *app) crumbsSymbols(d *editor) {
	k := &a.crumbs
	if d == nil || k.syms != nil && k.symDoc == d.id && k.symVer == d.buf.Version() {
		return
	}
	k.symDoc, k.symVer = d.id, d.buf.Version()
	k.syms, _ = a.symbolsOf(d)
}

// symbolRowOf is the row of the symbol a line is in, -1 for none: the last
// one declared at or before the line, as a heading holds the text under it.
// The rows are in the order of the document, so the search stops.
func symbolRowOf(syms []symbols.Symbol, line int) int {
	at := -1
	for i, s := range syms {
		if s.Line > line {
			break
		}
		at = i
	}
	return at
}

// fileSymbols is the box of a file's symbols, as a folder's box lists its
// files: a row of an icon and a name, in the order of the document, as go
// to symbol lists them. A click shows the symbol, the keys move among the
// rows, and Enter does what a click does.
func (a *app) fileSymbols(c *ui.Context, d *editor) {
	p := a.pal
	k := &a.crumbs
	note := func(s string) {
		ui.Text(c, s).FontSize(12.5).TextColor(p.muted).Padding(6, 10).MaxWidthPercent(100)
	}
	if d == nil {
		note(locales.SymbolNoFile())
		return
	}
	a.crumbsSymbols(d)
	if len(k.syms) == 0 {
		note(locales.SymbolsNone())
		return
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	roots := make([]int, len(k.syms))
	for i := range roots {
		roots[i] = i
	}
	var clicked int
	clicked = -1
	out := ui.Outline(c.Key("crumbs-symbols"), &k.outline, roots, func(int) []int {
		return nil
	}, func(i int) {
		sym := k.syms[i]
		row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(explorerRowH).Label(sym.Name)
		if row.Clicked() {
			clicked = i
		}
		row.Children(func() {
			icon, col := a.symbolIcon(sym.Kind)
			ui.Icon(c, icon).FontSize(14).TextColor(col)
			ui.Text(c, sym.Name).FontSize(13).SingleLine().Shrink(1).TextColor(p.text)
		})
	}).Height(crumbsTreeH(c, len(k.syms))).Gap(0)
	out.Bind(&k.view)
	if k.focus {
		k.focus = false
		k.view.Focus()
	}
	c.SetTheme(base)
	if k.reveal != "" {
		k.reveal = ""
		if k.sel >= 0 && k.sel < len(k.syms) {
			k.outline.List.ScrollIntoView(k.sel)
		}
	}
	if clicked < 0 && out.Submitted() && k.sel >= 0 && k.sel < len(k.syms) {
		clicked = k.sel
	}
	if clicked < 0 || clicked >= len(k.syms) {
		return
	}
	// The box goes before the symbol is read: closing it empties it.
	sym := k.syms[clicked]
	a.closeCrumbs()
	a.openSymbolAt(d, sym)
}

// openSymbolAt goes to a symbol of a document: in its text, as go to symbol
// goes, and in the Reading view of a note, to the heading there.
func (a *app) openSymbolAt(d *editor, sym symbols.Symbol) {
	if d.preview != nil && d.preview.markdown != nil {
		a.goToHeading(d, sym.Line)
		return
	}
	a.editActive(func(d *editor) { a.goToSymbol(d, sym) })()
}
