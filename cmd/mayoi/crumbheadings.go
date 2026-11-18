package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/symbols"
	"mayoi/internal/workspace"
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

// headingLevel names a heading's level, as H2, by its marks.
func headingLevel(h symbols.Symbol) string { return fmt.Sprintf("H%d", len(h.Detail)) }

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

// headingFilter is what a query of the box of headings found: the
// headings it matched, with the places of the characters it matched, and
// the headings that show, those matched and those they are under, as the
// tree's roots and kids. Nil hits shows them all.
type headingFilter struct {
	query   string
	version uint64
	hits    map[int][]int
	roots   []int
	kids    map[int][]int
}

// filterHeadings finds the headings the box's query matches, once for
// each query and version of the outline.
func (k *crumbsPicker) filterHeadings(o *headingOutline) *headingFilter {
	q := strings.TrimSpace(k.query)
	f := &k.filter
	if f.query == q && f.version == o.version && (q == "") == (f.hits == nil) {
		return f
	}
	*f = headingFilter{query: q, version: o.version}
	if q == "" {
		return f
	}
	names := make([]string, len(o.heads))
	for i, h := range o.heads {
		names[i] = h.Name
	}
	f.hits = map[int][]int{}
	f.kids = map[int][]int{}
	shown := map[int]bool{}
	for _, m := range workspace.Fuzzy(names, q, len(names)) {
		f.hits[m.Index] = m.Positions
		for i := m.Index; i >= 0 && !shown[i]; i = o.parent[i] {
			shown[i] = true
		}
	}
	// In the outline's order, each under the heading it is under.
	for i := range o.heads {
		switch {
		case !shown[i]:
		case o.parent[i] < 0:
			f.roots = append(f.roots, i)
		default:
			f.kids[o.parent[i]] = append(f.kids[o.parent[i]], i)
		}
	}
	return f
}

// headingsFilterField is the field atop the box of headings that filters
// them as it is typed in. It has the keyboard: Up and Down choose among
// the rows, and Enter goes to the one chosen.
func (a *app) headingsFilterField(c *ui.Context, o *headingOutline) (submitted bool) {
	p := a.pal
	k := &a.crumbs
	ui.Row(c).Gap(6).AlignItems(ui.Center).Height(explorerRowH+4).Padding(0, 8).Margin(0, 0, 4, 0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Icon(c, iconSearch).FontSize(12).TextColor(p.faint).Shrink(0)
		in := a.textKeys(ui.TextInputBase(c.Key("crumbs-filter"), &k.query)).Grow(1).FontSize(12.5).
			Placeholder(locales.CrumbsFilterHeadings()).Label(locales.CrumbsFilterHeadings())
		if k.focus {
			k.focus = false
			in.Focus()
		}
		if in.Changed() {
			// The first heading matched chosen, once the rows show, and
			// those folded opened, so that none matched hides.
			k.reveal = "first"
			for i := range o.kids {
				k.outline.Open.Add(i)
			}
		}
		rows := k.outline.Rows()
		move := func(to int) {
			if rows > 0 {
				k.sel = min(max(to, 0), rows-1)
				k.outline.List.ScrollIntoView(k.sel)
			}
		}
		// Each key asked for each frame, as one not asked for is not had.
		down, up := in.Shortcut(0, ui.KeyDown), in.Shortcut(0, ui.KeyUp)
		pgDown, pgUp := in.Shortcut(0, ui.KeyPageDown), in.Shortcut(0, ui.KeyPageUp)
		switch {
		case down:
			move(k.sel + 1)
		case up:
			move(k.sel - 1)
		case pgDown:
			move(k.sel + crumbsPage)
		case pgUp:
			move(k.sel - crumbsPage)
		}
		submitted = in.Submitted()
	})
	return submitted
}

// crumbsPage is how many rows Page Up and Page Down move in the box.
const crumbsPage = 10

// headingsTree is the box's tree of the headings, under a field that
// filters them: a click on one, or Enter, goes to it, and its arrow folds
// the headings under it.
func (a *app) headingsTree(c *ui.Context, d *editor) {
	p := a.pal
	k := &a.crumbs
	o, chain := a.headingCrumbs(d)
	if o == nil {
		ui.Text(c, locales.CrumbsNoHeadings()).FontSize(12.5).TextColor(p.muted).Padding(6, 10).MaxWidthPercent(100)
		return
	}
	submitted := a.headingsFilterField(c, o)
	f := k.filterHeadings(o)
	roots, kids := o.roots, o.kids
	if f.hits != nil {
		roots, kids = f.roots, f.kids
	}
	if len(roots) == 0 {
		ui.Text(c, locales.CrumbsNoHeadingMatch()).FontSize(12.5).TextColor(p.muted).Padding(6, 10).MaxWidthPercent(100)
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
				count(kids[i])
			}
		}
	}
	count(roots)
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	clicked := -1
	out := ui.Outline(c.Key("crumbs-headings"), &k.outline, roots, func(i int) []int {
		if i < 0 || i >= len(o.heads) {
			return nil
		}
		return kids[i]
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
			// Its level, quiet, as the tree's nesting shows the rest.
			ui.Text(c, headingLevel(h)).FontSize(10.5).FontFeatures("tnum").TextColor(p.faint).
				SingleLine().Width(18).Shrink(0)
			var label ui.Element
			switch pos, hit := f.hits[i]; {
			case f.hits == nil:
				label = ui.Text(c, h.Name).TextColor(p.text)
			case hit:
				// What the query matched, in the accent.
				label = ui.RichText(c, a.matchSpans(h.Name, 0, pos, p.text)...)
			default:
				// A heading shown only for those under it that matched.
				label = ui.Text(c, h.Name).TextColor(p.muted)
			}
			label.FontSize(13).SingleLine().Shrink(1).MinWidth(0)
			if i != here {
				return
			}
			// The heading the reader is in: bolder, with a dot of the
			// accent at the row's end.
			label.FontWeight(600)
			ui.Spacer(c)
			ui.Box(c.Key("here")).Size(6, 6).Radius(3).Shrink(0).Background(p.accent).Margin(0, 6, 0, 0)
		})
	}).Height(crumbsTreeH(c, rows)).Gap(0)
	out.Bind(&k.view)
	c.SetTheme(base)
	switch k.reveal {
	case "":
	case "first":
		// The first heading the query matched chosen, or with none
		// typed, the one the reader is in.
		k.reveal = ""
		for r := range k.outline.Rows() {
			if i := k.outline.Item(r); f.hits == nil && i == here || f.hits != nil && f.hits[i] != nil {
				k.sel = r
				k.outline.List.ScrollIntoView(r)
				break
			}
		}
	default:
		// The heading the reader is in chosen, once the rows show.
		k.reveal = ""
		for r := range k.outline.Rows() {
			if k.outline.Item(r) == here {
				k.sel = r
				k.outline.List.ScrollIntoView(r)
			}
		}
	}
	if clicked < 0 && (out.Submitted() || submitted) && k.sel >= 0 && k.sel < k.outline.Rows() {
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

// headingCrumbsShown are the places in a chain of headings the
// breadcrumbs show, -1 for an ellipsis in the place of those left out: all
// of a short chain, and of a long one the outermost and the innermost two,
// so that where the reader is keeps room to read.
func headingCrumbsShown(n int) []int {
	if n <= 3 {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	return []int{0, -1, n - 2, n - 1}
}

// headingCrumbsView shows the headings the reader is in, after the file's
// name, each opening the box of the document's headings, the innermost
// brighter. A heading the reader comes into opens its room as it fades in,
// one left closes it, and those after slide along.
func (a *app) headingCrumbsView(c *ui.Context, col *column, t *colTab, o *headingOutline, chain []int) {
	p := a.pal
	k := &a.crumbs
	for _, n := range headingCrumbsShown(len(chain)) {
		// The ellipsis stands for the headings left out, and opens the
		// box below the innermost of them.
		key, name, label := "heading-more", "…", locales.CrumbsHeadings()
		at := len(chain) - 3
		if n >= 0 {
			h := o.heads[chain[n]]
			key, name, label, at = fmt.Sprintf("heading:%d", chain[n]), h.Name, locales.CrumbsHeadingTip(h.Name), n
		}
		inner := n == len(chain)-1
		ui.Row(c.Key(key)).Gap(4).AlignItems(ui.Center).Shrink(1).MinWidth(0).Transition(crumbIn).Children(func() {
			ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint).Shrink(0)
			on := k.open && k.tab == t && k.heads && k.at == at
			b := ui.ButtonBase(c.Key("crumb")).Height(crumbsH-6).Padding(0, 5).Radius(4).
				AlignItems(ui.Center).Shrink(1).MinWidth(0).Label(label)
			if n < 0 {
				b.Shrink(0)
			}
			if on || b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() {
				fg := p.muted
				if inner || on || b.Hovered() {
					fg = p.text
				}
				ui.Text(c, name).FontSize(12).SingleLine().Shrink(1).MinWidth(0).TextColor(fg)
			})
			if b.Clicked() {
				a.openHeadingCrumbs(col, t, at)
			}
			if on {
				a.crumbsBox(c, b)
			}
		})
	}
}
