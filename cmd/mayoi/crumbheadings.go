package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/symbols"
	"mayoi/locales"
)

// headingOutline is the outline of a Markdown document's headings, as the
// breadcrumbs show it, kept for the version of the text it was read from.
type headingOutline struct {
	version uint64
	// read is set once the headings were read, and reading while they are
	// read again in the background, for a later version.
	read, reading bool
	heads         []symbols.Symbol
	// parent is the heading each is under, -1 for one at the top, and
	// kids the headings right under each, roots those at the top.
	parent []int
	kids   map[int][]int
	roots  []int
}

// headingsOf returns the outline of the headings of a Markdown document.
// The first is read at once; after an edit, the last one read stands
// while the text is read again in the background, as reading a long note
// at each key would slow typing.
func (a *app) headingsOf(d *document) *headingOutline {
	o := &d.headings
	version := d.buf.Version()
	switch {
	case !o.read:
		*o = outlineOf(symbols.Find(d.buf.Snapshot(), true))
		o.version, o.read = version, true
	case o.version != version && !o.reading:
		o.reading = true
		snap := d.buf.Snapshot()
		bg.Go(func() {
			defer recoverCrash()
			n := outlineOf(symbols.Find(snap, true))
			n.version, n.read = version, true
			a.post(func() {
				*o = n
				a.invalidate()
			})
		})
	}
	return o
}

// outlineOf nests headings by their levels: each under the last before it
// of a lower level.
func outlineOf(heads []symbols.Symbol) headingOutline {
	o := headingOutline{heads: heads, parent: make([]int, len(heads)), kids: map[int][]int{}}
	var stack []int
	for i, h := range heads {
		for len(stack) > 0 && len(heads[stack[len(stack)-1]].Detail) >= len(h.Detail) {
			stack = stack[:len(stack)-1]
		}
		o.parent[i] = -1
		if len(stack) > 0 {
			o.parent[i] = stack[len(stack)-1]
			o.kids[o.parent[i]] = append(o.kids[o.parent[i]], i)
		} else {
			o.roots = append(o.roots, i)
		}
		stack = append(stack, i)
	}
	return o
}

// chain is the headings that hold a line, from the outermost: the last
// heading at or before it, and those it is under. Empty before the first.
func (o *headingOutline) chain(line int) []int {
	var out []int
	for i := o.headingAt(line); i >= 0; i = o.parent[i] {
		out = append(out, i)
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// headingAt is the last heading at or before a line, -1 for none.
func (o *headingOutline) headingAt(line int) int {
	return sort.Search(len(o.heads), func(i int) bool { return o.heads[i].Line > line }) - 1
}

// placeLine is the line of a place at the top of a preview, the last
// line for its end. A line more than half in view counts, as a heading
// scrolled to sits a hair below the top.
func placeLine(place float32) int {
	if place == markdownEnd {
		return math.MaxInt
	}
	return int(place + 0.5)
}

// headingLabel is how the breadcrumbs name a heading: its marks, then its
// text.
func headingLabel(h symbols.Symbol) string { return h.Detail + " " + h.Name }

// readingLine is the line of a document's text the reader is at: the
// caret's, or in the Reading view, which has none, the line at the top of
// what shows.
func readingLine(d *editor) int {
	if d.preview != nil {
		if m := d.preview.markdown; m != nil {
			return placeLine(m.sync.top)
		}
		return -1
	}
	snap := d.buf.Snapshot()
	return snap.LineAt(min(d.sel[1], snap.Len()))
}

// headingCrumbs are the headings of a Markdown document the reader is
// in, after the file's name, and nil for another document, one without
// headings, or a place before the first.
func (a *app) headingCrumbs(d *editor) (*headingOutline, []int) {
	if d == nil || !isMarkdown(d.document) || d.preview != nil && d.preview.markdown == nil {
		return nil, nil
	}
	o := a.headingsOf(d.document)
	if len(o.heads) == 0 {
		return nil, nil
	}
	return o, o.chain(readingLine(d))
}

// openHeadingCrumbs opens the box of the headings of the document tab t
// of column col shows, below heading crumb at, or closes it if it is the
// one open.
func (a *app) openHeadingCrumbs(col *column, t *colTab, at int) {
	k := &a.crumbs
	if k.open && k.tab == t && k.heads && k.at == at {
		k.open = false
		return
	}
	*k = crumbsPicker{
		open: true, col: col, tab: t, file: t.path(), heads: true, at: at, gen: k.gen + 1,
		outline: ui.OutlineState[int]{Indent: treeStep}, sel: -1, focus: true, reveal: "-",
	}
	k.outline.List.Selected = &k.sel
	// Every heading open, as VS Code's outline opens.
	o := a.headingsOf(t.ed.document)
	for i := range o.kids {
		k.outline.Open.Add(i)
	}
}

// headingsTree is the box's tree of the headings: a click on one, or Enter,
// goes to it, and its arrow folds the headings under it.
func (a *app) headingsTree(c *ui.Context, d *editor) {
	p := a.pal
	k := &a.crumbs
	o, chain := a.headingCrumbs(d)
	if o == nil {
		ui.Text(c, locales.CrumbsNoHeadings()).FontSize(12.5).TextColor(p.muted).Padding(6, 10).MaxWidthPercent(100)
		return
	}
	here := -1
	if len(chain) > 0 {
		here = chain[len(chain)-1]
	}
	// The rows the tree shows: the headings, less those under one folded.
	rows := 0
	var count func(items []int)
	count = func(items []int) {
		for _, i := range items {
			rows++
			if k.outline.Open.Has(i) {
				count(o.kids[i])
			}
		}
	}
	count(o.roots)
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	clicked := -1
	icon, iconCol := a.symbolIcon("heading")
	out := ui.Outline(c.Key("crumbs-headings"), &k.outline, o.roots, func(i int) []int {
		if i < 0 || i >= len(o.heads) {
			return nil
		}
		return o.kids[i]
	}, func(i int) {
		if i < 0 || i >= len(o.heads) {
			return
		}
		h := o.heads[i]
		row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(explorerRowH).Label(h.Name)
		if row.Clicked() {
			clicked = i
		}
		row.Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(iconCol)
			label := ui.Text(c, h.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0).TextColor(p.text)
			if i == here {
				label.FontWeight(600)
			}
		})
	}).Height(crumbsTreeH(c, rows)).Gap(0)
	out.Bind(&k.view)
	if k.focus {
		k.focus = false
		k.view.Focus()
	}
	c.SetTheme(base)
	if k.reveal != "" {
		// The heading the reader is in chosen, once the rows show.
		k.reveal = ""
		for r := range k.outline.Rows() {
			if k.outline.Item(r) == here {
				k.sel = r
				k.outline.List.ScrollIntoView(r)
			}
		}
	}
	if clicked < 0 && out.Submitted() && k.sel >= 0 && k.sel < k.outline.Rows() {
		clicked = k.outline.Item(k.sel)
	}
	if clicked < 0 || clicked >= len(o.heads) {
		return
	}
	a.closeCrumbs()
	a.goToHeading(d, o.heads[clicked].Line)
}

// goToHeading takes an editor to the heading on a line: in its text, the
// caret to the heading at the top, under the lines that stick; in the
// Reading view, the heading scrolled to the top.
func (a *app) goToHeading(d *editor, line int) {
	if d.preview != nil {
		if m := d.preview.markdown; m != nil {
			m.sync.want, m.sync.placing, m.sync.tries = float32(line), true, 0
			a.invalidate()
		}
		return
	}
	a.queueEdit(d, func(d *editor) { a.jumpToScope(d, line) })
}

// headingCrumbsView shows the headings the reader is in, after the file's
// name, each opening the box of the document's headings.
func (a *app) headingCrumbsView(c *ui.Context, col *column, t *colTab, o *headingOutline, chain []int) {
	p := a.pal
	k := &a.crumbs
	for n, i := range chain {
		h := o.heads[i]
		on := k.open && k.tab == t && k.heads && k.at == n
		b := ui.ButtonBase(c.Key(fmt.Sprintf("heading-crumb:%d", n))).Height(crumbsH-6).Padding(0, 5).Radius(4).
			AlignItems(ui.Center).Shrink(1).MinWidth(0).Label(locales.CrumbsHeadingTip(h.Name))
		if on || b.Hovered() {
			b.Background(p.hover)
		}
		b.Transition(hoverFade)
		b.Children(func() {
			fg := p.muted
			if on || b.Hovered() {
				fg = p.text
			}
			ui.Text(c, headingLabel(h)).FontSize(12).SingleLine().Shrink(1).MinWidth(0).TextColor(fg)
		})
		if b.Clicked() {
			a.openHeadingCrumbs(col, t, n)
		}
		if on {
			a.crumbsBox(c, b)
		}
		if n < len(chain)-1 {
			ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint).Shrink(0)
		}
	}
}
