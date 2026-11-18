package main

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/highlight"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// The Table view of a file of JSON Lines: a row a line, a column a key,
// with the chosen row whole below. It reads the document's text, so that
// what is edited as source, a remote file and one read again all show.

// jsonlQuickMax is the most of a text, in runes, indexed on the main
// thread for the first frame to show rows; the rest comes from the
// background.
const jsonlQuickMax = 256 << 10

// jsonlCopiedFor is how long the details' Copy button shows a check
// after it copied.
const jsonlCopiedFor = 1500 * time.Millisecond

// jsonlHitsMax is the most matches of the filter a cell or the details
// mark.
const jsonlHitsMax = 200

// jsonlPrettyMax is the most of a row's pretty JSON its details show, in
// bytes; Show in Source has the rest.
const jsonlPrettyMax = 256 << 10

// jsonlWant is what the rows shown are of: the text's version, the
// filter's query, the values it keeps or hides, as whereKey has them, and
// the sort.
type jsonlWant struct {
	version uint64
	query   string
	where   string
	sort    string
	desc    bool
}

// jsonlWash is a cell just copied: of row, and of the column of id, or
// every cell of the row for "", copied at at; fresh until the next frame
// gives it its time.
type jsonlWash struct {
	row   int32
	id    string
	at    time.Time
	fresh bool
}

// jsonlTable is the state of a Table view.
type jsonlTable struct {
	ix *jsonlIndex
	// shown is what view is of, asked what the read on its way is of,
	// reading whether one is, since when.
	shown, asked jsonlWant
	reading      bool
	readingAt    time.Time
	gen          atomic.Int64
	// view is the rows shown, in their order, nil for all in the file's.
	view []int32
	// keys are what the rows sort by, of the index and the column keysOf.
	keys   []jsonlSortKey
	keysOf string
	keysIx *jsonlIndex

	cols []*jsonlColumn
	// widths are the columns' widths, by ID, as the table first gave them.
	widths    map[string]float32
	ends      map[string]bool // columns of numbers, aligned to their end
	expanding map[string]bool

	list   ui.ListState
	chosen ui.Selection[int]
	row    int // the row chosen, in view, -1 for none
	sort   ui.SortOrder
	query  string
	input  ui.Handle
	// where are the values the rows are filtered by, besides query.
	where []jsonlWhere

	// cells keep the cells of the rows shown lately, by row, for the
	// columns of cellsOf.
	cells   map[int32][]jsonlCell
	cellsOf []*jsonlColumn

	details bool
	// split is the share of the room the rows take, in percent.
	split float32
	// pretty is the chosen row's text indented, of prettyOf.
	pretty   []ui.Span
	prettyOf string
	// prettyText is the text of pretty, and markAt where the member of
	// the chosen field starts in it, -1 for none.
	prettyText string
	markAt     int
	// prettyScroll is the details' scrolling, and revealed the row and the
	// field they last scrolled to.
	prettyScroll ui.ScrollState
	revealed     string
	// copied is when the line was last copied, which the details' Copy
	// button says.
	copied time.Time
	// wash is the cell copied last, which shows it for a moment.
	wash jsonlWash

	// line is the line of the file to show once indexed, -1 for none.
	line int
}

func newJSONLTable() *jsonlTable {
	return &jsonlTable{row: -1, line: -1, details: true, split: 64}
}

// rowAt returns the row of the index row i of the view is.
func (t *jsonlTable) rowAt(i int) int32 {
	if t.view != nil {
		return t.view[i]
	}
	return int32(i)
}

// count is how many rows the view shows.
func (t *jsonlTable) count() int {
	if t.ix == nil {
		return 0
	}
	if t.view != nil {
		return len(t.view)
	}
	return t.ix.rows()
}

// column returns the column of id, nil for none.
func (t *jsonlTable) column(id string) *jsonlColumn {
	for _, col := range t.cols {
		if col.id == id {
			return col
		}
	}
	return nil
}

// shownColumns are the columns the table shows, in the order they come.
func (t *jsonlTable) shownColumns() []*jsonlColumn {
	var out []*jsonlColumn
	for _, col := range t.cols {
		if !col.hidden && !col.expanded {
			out = append(out, col)
		}
	}
	return out
}

// hiddenColumns are the columns the person can show again.
func (t *jsonlTable) hiddenColumns() []*jsonlColumn {
	var out []*jsonlColumn
	for _, col := range t.cols {
		if col.hidden && !col.expanded {
			out = append(out, col)
		}
	}
	return out
}

// want is what the rows are to show now.
func (t *jsonlTable) want(version uint64) jsonlWant {
	return jsonlWant{version: version, query: t.query, where: whereKey(t.where), sort: t.sort.Column, desc: t.sort.Descending}
}

// followJSONL keeps a table's rows those of its document's text, its
// filter and its sort: at once for a small text, and from the background
// for the rest, the rows shown until then staying.
func (a *app) followJSONL(v *viewer) {
	t, snap := v.jsonl, v.doc.buf.Snapshot()
	want := t.want(snap.Version())
	if want == t.shown || t.reading && want == t.asked {
		return
	}
	if t.ix == nil {
		if snap.Len() <= jsonlQuickMax {
			a.applyJSONL(t, readJSONL(snap.String(), nil, &jsonlRead{}, want, t.where, func() bool { return false }))
			return
		}
		// The head of the text, for the first frame.
		head := snap.Slice(0, jsonlQuickMax)
		if nl := strings.LastIndexByte(head, '\n'); nl > 0 {
			head = head[:nl+1]
		}
		ix := indexJSONL(head)
		ix.partial = true
		a.applyJSONL(t, &jsonlRead{ix: ix, want: jsonlWant{version: ^uint64(0)}})
	}
	gen := t.gen.Add(1)
	t.asked, t.reading, t.readingAt = want, true, time.Now()
	ix := t.ix
	if ix != nil && (ix.partial || t.shown.version != want.version) {
		ix = nil
	}
	had := jsonlRead{ix: t.keysIx, keys: t.keys, keysOf: t.keysOf}
	where := slices.Clone(t.where)
	bg.Go(func() {
		defer recoverCrash()
		stop := func() bool { return t.gen.Load() != gen }
		text := ""
		if ix == nil {
			text = snap.String()
		}
		r := readJSONL(text, ix, &had, want, where, stop)
		a.post(func() {
			if stop() || r == nil {
				return
			}
			t.reading = false
			a.applyJSONL(t, r)
			a.invalidate()
		})
	})
}

// jsonlRead is what a read of a table found.
type jsonlRead struct {
	ix     *jsonlIndex
	want   jsonlWant
	view   []int32
	keys   []jsonlSortKey
	keysOf string
}

// readJSONL indexes text, unless ix has it, and finds the rows want
// shows, where of want.where, nil once stop says to; had has the sort keys
// found last.
func readJSONL(text string, ix *jsonlIndex, had *jsonlRead, want jsonlWant, where []jsonlWhere, stop func() bool) *jsonlRead {
	if ix == nil {
		ix = indexJSONL(text)
	}
	r := &jsonlRead{ix: ix, want: want}
	var filter []int32
	if want.query != "" {
		if filter = filterRows(ix, want.query, stop); filter == nil {
			return nil
		}
	}
	if len(where) > 0 {
		if filter = filterWhere(ix, filter, want.query == "", where, stop); filter == nil {
			return nil
		}
	}
	if want.sort != "" {
		if had.ix == ix && had.keysOf == want.sort {
			r.keys = had.keys
		} else if col, ok := pathOfID(want.sort); ok {
			if r.keys = sortKeys(ix, col, stop); r.keys == nil {
				return nil
			}
		}
		r.keysOf = want.sort
	}
	r.view = jsonlView(ix, filter, want.query != "" || len(where) > 0, r.keys, want.desc)
	return r
}

// pathOfID returns the column of an ID pathID made.
func pathOfID(id string) (*jsonlColumn, bool) {
	if id == pathID(nil) {
		return newColumn(nil, ""), true
	}
	var path []jsonlStep
	for len(id) > 0 {
		mark := id[0]
		id = id[1:]
		end := strings.IndexAny(id, "\x01\x02")
		if end < 0 {
			end = len(id)
		}
		part := id[:end]
		id = id[end:]
		switch mark {
		case '\x01':
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, false
			}
			path = append(path, jsonlStep{index: n, item: true})
		case '\x02':
			path = append(path, jsonlStep{key: part})
		default:
			return nil, false
		}
	}
	return newColumn(path, ""), len(path) > 0
}

// applyJSONL shows what a read found, keeping the row chosen.
func (a *app) applyJSONL(t *jsonlTable, r *jsonlRead) {
	chosen := int32(-1)
	if t.ix != nil && t.row >= 0 && t.row < t.count() {
		chosen = t.rowAt(t.row)
	}
	sameText := t.ix != nil && r.ix == t.ix
	t.ix, t.shown, t.view = r.ix, r.want, r.view
	t.keys, t.keysOf, t.keysIx = r.keys, r.keysOf, r.ix
	if !sameText {
		t.cols = jsonlColumns(r.ix, t.cols)
		t.cells, t.cellsOf = nil, nil
		if chosen >= int32(r.ix.rows()) {
			chosen = -1
		}
	}
	t.row = -1
	if chosen >= 0 {
		t.row = slices.Index(t.viewRows(), chosen)
	}
	if t.line >= 0 && !r.ix.partial {
		row := int32(min(r.ix.rowOfLine(t.line), r.ix.rows()-1))
		t.line = -1
		if i := slices.Index(t.viewRows(), row); i >= 0 {
			t.row = i
			t.list.ScrollTo(i, ui.Start)
		}
	}
	if t.row < 0 && t.count() > 0 {
		t.row = 0
	}
	t.chosen.Clear()
	if t.row >= 0 {
		t.chosen.Add(int(t.rowAt(t.row)))
	}
}

// viewRows returns the rows of the view, in its order.
func (t *jsonlTable) viewRows() []int32 {
	if t.view != nil {
		return t.view
	}
	rows := make([]int32, t.ix.rows())
	for i := range rows {
		rows[i] = int32(i)
	}
	return rows
}

// rowCells returns the cells of a row for cols.
func (t *jsonlTable) rowCells(row int32, cols []*jsonlColumn) []jsonlCell {
	if !slices.Equal(cols, t.cellsOf) || len(t.cells) > 4096 {
		t.cells, t.cellsOf = map[int32][]jsonlCell{}, cols
	}
	if got, ok := t.cells[row]; ok {
		return got
	}
	line := t.ix.line(int(row))
	out := make([]jsonlCell, len(cols))
	if t.ix.kinds[row] == jsonlInvalid {
		if len(out) > 0 {
			out[0] = jsonlCell{cellLine(line, jsonlCellMax), cellInvalid}
		}
	} else {
		for k, col := range cols {
			if raw, ok := lookup(line, col.path); ok && raw != "" {
				out[k] = cellOf(raw)
			}
		}
	}
	t.cells[row] = out
	return out
}

// measureColumn gives a column the width of its title and the cells of
// the first rows, and says whether it holds numbers.
func (t *jsonlTable) measureColumn(col *jsonlColumn, size float32) {
	if t.widths == nil {
		t.widths, t.ends = map[string]float32{}, map[string]bool{}
	}
	if _, ok := t.widths[col.id]; ok {
		return
	}
	chars, numbers, values := len([]rune(col.title()))+2, 0, 0
	for i := range min(t.ix.rows(), 100) {
		if t.ix.kinds[i] == jsonlInvalid {
			continue
		}
		raw, ok := lookup(t.ix.line(i), col.path)
		if !ok || raw == "" {
			continue
		}
		cell := cellOf(raw)
		chars = max(chars, len([]rune(cell.text)))
		values++
		if cell.kind == cellNumber {
			numbers++
		}
	}
	t.widths[col.id] = clamp(float32(chars)*size*0.6+20, 56, 320)
	t.ends[col.id] = values > 0 && numbers*2 > values
}

// jsonlView shows a table of the rows of a file of JSON Lines.
func (a *app) jsonlView(c *ui.Context, v *viewer, name string) {
	t := v.jsonl
	a.followJSONL(v)
	if t.wash.fresh {
		// A copy just made shows from this frame: its cells, and the line
		// the details' Copy button.
		t.wash.at, t.wash.fresh = c.Now(), false
		if t.wash.id == "" {
			t.copied = t.wash.at
		}
	}
	p := a.pal
	ui.Column(c.Key("jsonl")).Fill().Background(p.editor).Children(func() {
		a.jsonlBar(c, v)
		ui.Divider(c)
		switch {
		case t.ix == nil:
		case t.ix.rows() == 0:
			a.viewEmpty(c, "file-text", locales.JsonlEmpty())
		case t.count() == 0 && t.filtered():
			a.jsonlNoMatch(c, t)
		case !t.details:
			a.jsonlGrid(c, v, name)
		default:
			ui.SplitVerticalPercent(c.Key("jsonl-split"), &t.split, func() {
				a.jsonlGrid(c, v, name)
			}, func() {
				a.jsonlDetails(c, v)
			}).Grow(1)
		}
	})
}

// filtered reports whether the rows shown are filtered, by a query or by
// values.
func (t *jsonlTable) filtered() bool { return t.shown.query != "" || t.shown.where != "" }

// clearFilters shows every row again.
func (t *jsonlTable) clearFilters() {
	t.query, t.where = "", nil
}

// addWhere filters the rows by a column's value raw: to those that hold
// it, or, when not, to those that don't. It takes the place of a filter it
// makes needless: any other of the column, for one that keeps a value.
func (t *jsonlTable) addWhere(col *jsonlColumn, raw string, not bool) {
	cell := cellOf(raw)
	w := jsonlWhere{id: col.id, value: valueKey(raw), not: not, title: col.title(), text: cellLine(cell.text, 40), kind: cell.kind}
	t.where = slices.DeleteFunc(t.where, func(o jsonlWhere) bool {
		return o.id == w.id && (!o.not || !not || o.value == w.value)
	})
	t.where = append(t.where, w)
}

// jsonlNoMatch says no row is left by the filters, and offers to clear
// them.
func (a *app) jsonlNoMatch(c *ui.Context, t *jsonlTable) {
	text := locales.JsonlNoMatch(t.shown.query)
	if t.shown.where != "" {
		text = locales.JsonlNoMatchFilters()
	}
	ui.Column(c.Key("jsonl-none")).FillWidth().AlignItems(ui.Center).Children(func() {
		a.viewEmpty(c, "search", text)
		if a.barButton(c, "jsonl-clear", locales.JsonlClearFilters()).Clicked() {
			t.clearFilters()
			a.post(func() { a.focusColumn() })
		}
	})
}

// whereChip is a filter by a value, in the bar: the column, = or ≠, and
// the value as its cell colors it, with a button to remove it, which it
// reports clicked. It opens its width as it comes, and closes it as it goes.
func (a *app) whereChip(c *ui.Context, w jsonlWhere) (remove bool) {
	p := a.pal
	op := "="
	if w.not {
		op = "≠"
	}
	ui.Row(c.Key("where:"+whereKey([]jsonlWhere{w}))).Height(22).Padding(0, 2, 0, 8).Gap(4).Radius(11).
		Background(p.text.Alpha(0.07)).AlignItems(ui.Center).Shrink(1).Transition(wordIn).
		Tooltip(w.title + " " + op + " " + w.text).Children(func() {
		ui.Text(c, w.title).FontSize(12).TextColor(p.muted).SingleLine().Shrink(0)
		ui.Text(c, op).FontSize(12).TextColor(p.faint).Shrink(0)
		ui.Text(c, w.text).FontSize(12).Font(a.font.family).TextColor(a.cellColor(w.kind)).SingleLine().Shrink(1)
		label := locales.JsonlRemoveFilter()
		b := ui.ButtonBase(c.Key("remove")).Size(18, 18).Radius(9).AlignItems(ui.Center).Justify(ui.Center).
			Label(label).Tooltip(label).Shrink(0)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() { ui.Icon(c, iconX).FontSize(10).TextColor(p.muted) })
		remove = b.Clicked()
	})
	return remove
}

// jsonlBar is the bar over a table: its filter, and what it shows.
func (a *app) jsonlBar(c *ui.Context, v *viewer) {
	t, p := v.jsonl, a.pal
	ui.Row(c.Key("jsonl-bar")).Padding(6, 10).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c.Key("jsonl-filter")).Width(260).Shrink(1).Children(func() {
			in := a.searchField(c, "jsonl-query", &t.query, locales.JsonlFilter(), &t.input, nil)
			in.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
					if t.query != "" || len(t.where) > 0 {
						t.clearFilters()
					} else {
						v.pane.Focus()
					}
					return true
				}
				return false
			})
			if in.Submitted() {
				v.pane.Focus()
			}
		})
		gone := -1
		for k, w := range t.where {
			if a.whereChip(c, w) {
				gone = k
			}
		}
		if gone >= 0 {
			t.where = slices.Delete(t.where, gone, gone+1)
		}
		var msg string
		switch {
		case t.ix == nil:
		case t.filtered():
			msg = locales.JsonlRowsOf(t.count(), t.ix.rows())
		default:
			msg = locales.JsonlRows(t.ix.rows())
		}
		if t.ix != nil && t.ix.partial || t.reading && time.Since(t.readingAt) >= readingShows {
			msg = locales.JsonlReading()
		} else if t.reading {
			c.After(readingShows - time.Since(t.readingAt))
		}
		ui.Text(c.Key("jsonl-count"), msg).FontSize(12).TextColor(p.muted).FontFeatures("tnum").SingleLine().Shrink(0).
			Transition(slideAlong)
		ui.Spacer(c)
		if n := len(t.hiddenColumns()); n > 0 {
			b := ui.ButtonBase(c.Key("jsonl-hidden")).Height(22).Padding(0, 6).Radius(4).AlignItems(ui.Center).
				Label(locales.JsonlHiddenColumns(n)).Tooltip(locales.JsonlShowColumn())
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() { ui.Text(c, locales.JsonlHiddenColumns(n)).FontSize(12).TextColor(p.muted).FontFeatures("tnum") })
			b.Menu(func(m *ui.Menu) { a.jsonlShowMenu(m, t) })
		}
		label := locales.JsonlDetails()
		b := ui.ButtonBase(c.Key("jsonl-details")).Size(26, 22).Radius(4).AlignItems(ui.Center).Justify(ui.Center).
			Label(label).Tooltip(label).Selected(t.details)
		switch {
		case t.details:
			b.Background(p.hover)
		case b.Hovered():
			b.Background(p.hover.Alpha(0.6))
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() {
			col := p.muted
			if t.details {
				col = p.text
			}
			ui.Icon(c, iconBraces).FontSize(13).TextColor(col)
		})
		if b.Clicked() {
			t.details = !t.details
		}
	})
}

// jsonlShowMenu lists the hidden columns of a table, to show again.
func (a *app) jsonlShowMenu(m *ui.Menu, t *jsonlTable) {
	hidden := t.hiddenColumns()
	if len(hidden) > 1 && m.Item(locales.JsonlShowAll()).Chosen() {
		for _, col := range hidden {
			col.hidden = false
		}
	}
	if len(hidden) > 1 {
		m.Separator()
	}
	for _, col := range hidden[:min(len(hidden), 100)] {
		if m.Item(col.title()).Chosen() {
			col.hidden = false
		}
	}
}

// jsonlGrid is the table of the rows.
func (a *app) jsonlGrid(c *ui.Context, v *viewer, name string) {
	t, p := v.jsonl, a.pal
	const size = 12.5
	shown := t.shownColumns()
	lastLine := 1
	if n := t.ix.rows(); n > 0 {
		lastLine = int(t.ix.lines[n-1]) + 1
	}
	cols := make([]ui.TableColumn, 0, len(shown)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: float32(len(strconv.Itoa(lastLine)))*size*0.6 + 22,
		Align: ui.End, Fixed: true, RowHeader: true})
	for _, col := range shown {
		t.measureColumn(col, size)
		align := ui.Start
		if t.ends[col.id] {
			align = ui.End
		}
		cols = append(cols, ui.TableColumn{Title: col.title(), ID: col.id, Width: t.widths[col.id], MinWidth: 40,
			Align: align, Sortable: true})
	}
	s := &t.list
	s.Selected, s.Selection, s.CellSelect, s.MenuKeepsSelection = &t.row, &t.chosen, true, true
	s.Sort = &t.sort
	s.Key = func(i int) any { return int(t.rowAt(i)) }
	s.HeaderMenu = func(m *ui.Menu, j int) {
		if j > 0 && j-1 < len(shown) {
			a.jsonlColumnMenu(m, v, shown[j-1])
			return
		}
		a.jsonlShowMenu(m, t)
	}
	s.CellMenu = func(m *ui.Menu, i, j int) {
		row := t.rowAt(i)
		if j > 0 && j-1 < len(shown) && m.Item(locales.JsonlCopyValue()).Chosen() {
			a.copyJSONLValue(t, row, shown[j-1])
		}
		if m.Item(locales.JsonlCopyLine()).Chosen() {
			a.copyJSONLLine(t, row)
		}
		if j > 0 && j-1 < len(shown) && t.ix.kinds[row] != jsonlInvalid {
			col := shown[j-1]
			if raw, ok := lookup(t.ix.line(int(row)), col.path); ok && raw != "" {
				m.Separator()
				if m.Item(locales.JsonlFilterValue()).Chosen() {
					t.addWhere(col, raw, false)
				}
				if m.Item(locales.JsonlHideValue()).Chosen() {
					t.addWhere(col, raw, true)
				}
			}
		}
		m.Separator()
		if m.Item(a.withKey(locales.JsonlShowInSource(), "view.togglePreview")).Chosen() {
			a.post(func() { a.jsonlToSource(v, row) })
		}
	}
	mono := a.font.family
	query := foldASCII(t.shown.query)
	table := ui.Table(c.Key("jsonl-table"), s, cols, t.count(), func(i, j int) {
		row := t.rowAt(i)
		id := "#"
		if j > 0 && j-1 < len(shown) {
			id = shown[j-1].id
		}
		a.copyWash(c, t, row, id)
		if j == 0 {
			// The chosen row's number stands out, as the caret's line's
			// does in the editor.
			col := p.gutter
			switch {
			case t.ix.kinds[row] == jsonlInvalid:
				col = p.dangerFg
			case i == t.row:
				col = p.gutterNow
			}
			number := ui.Text(c, strconv.Itoa(int(t.ix.lines[row])+1)).FontSize(size).Font(mono).FontFeatures("tnum").SingleLine()
			if !s.CellSelected(i, j) {
				number.TextColor(col)
			}
			return
		}
		cells := t.rowCells(row, shown)
		if j-1 >= len(cells) {
			return
		}
		cell := cells[j-1]
		if cell.kind == cellMissing {
			return
		}
		if s.CellSelected(i, j) {
			// A chosen cell's text takes the accent's.
			ui.Text(c, cell.text).FontSize(size).Font(mono).SingleLine()
			return
		}
		color := a.cellColor(cell.kind)
		if hits := foldHits(cell.text, query, jsonlHitsMax); len(hits) > 0 {
			// The filter's matches are marked as find marks them.
			spans := []ui.Span{{Text: cell.text, Color: color}}
			for _, h := range hits {
				spans = markSpans(spans, h[0], h[1], p.match)
			}
			ui.RichText(c, spans...).FontSize(size).Font(mono).SingleLine()
			return
		}
		ui.Text(c, cell.text).FontSize(size).Font(mono).SingleLine().TextColor(color)
	}).Bind(&v.pane).Grow(1).Label(locales.PreviewLabel(name))
	a.takeKeys(table, placeViewer, func(ev ui.InputEvent) bool { return a.jsonlInput(v, ev) })
	if table.Submitted() && t.row >= 0 && t.row < t.count() {
		row := t.rowAt(t.row)
		a.post(func() { a.jsonlToSource(v, row) })
	}
}

// cellColor is the color of a cell of kind, as the editor colors JSON.
func (a *app) cellColor(kind byte) ui.Color {
	p := a.pal
	switch kind {
	case cellNumber:
		return p.syntax[highlight.Number]
	case cellBool, cellNull:
		return p.syntax[highlight.Constant]
	case cellObject, cellArray:
		return p.muted
	case cellInvalid:
		return p.dangerFg
	}
	return p.text
}

// jsonlColumnMenu is the menu of a column's header.
func (a *app) jsonlColumnMenu(m *ui.Menu, v *viewer, col *jsonlColumn) {
	t := v.jsonl
	if m.Item(locales.JsonlSortAscending()).Chosen() {
		t.sort = ui.SortOrder{Column: col.id}
	}
	if m.Item(locales.JsonlSortDescending()).Chosen() {
		t.sort = ui.SortOrder{Column: col.id, Descending: true}
	}
	if t.sort.Column != "" && m.Item(locales.JsonlSortNone()).Chosen() {
		t.sort = ui.SortOrder{}
	}
	m.Separator()
	if t.canExpand(col) && m.Item(locales.JsonlExpand()).Chosen() {
		a.expandJSONL(v, col)
	}
	if col.parent != "" {
		if parent := t.column(col.parent); parent != nil && m.Item(locales.JsonlCollapse(parent.title())).Chosen() {
			t.collapse(parent)
		}
	}
	if len(t.shownColumns()) > 1 && m.Item(locales.JsonlHide()).Chosen() {
		col.hidden = true
		if t.sort.Column == col.id {
			t.sort = ui.SortOrder{}
		}
	}
	if len(t.hiddenColumns()) > 0 {
		m.Submenu(locales.JsonlShowColumn(), func(m *ui.Menu) { a.jsonlShowMenu(m, t) })
	}
}

// canExpand reports whether a column holds objects or arrays in the first
// rows, whose members can be columns.
func (t *jsonlTable) canExpand(col *jsonlColumn) bool {
	if t.expanding[col.id] {
		return false
	}
	for i := range min(t.ix.rows(), 200) {
		if t.ix.kinds[i] == jsonlInvalid {
			continue
		}
		if raw, ok := lookup(t.ix.line(i), col.path); ok && raw != "" && (raw[0] == '{' || raw[0] == '[') {
			return true
		}
	}
	return false
}

// expandJSONL shows a column of objects or arrays as the columns of their
// members, found in the background, in its place.
func (a *app) expandJSONL(v *viewer, col *jsonlColumn) {
	t, ix := v.jsonl, v.jsonl.ix
	if t.expanding == nil {
		t.expanding = map[string]bool{}
	}
	t.expanding[col.id] = true
	gen := t.gen.Load()
	bg.Go(func() {
		defer recoverCrash()
		found := members(ix, col, func() bool { return false })
		a.post(func() {
			delete(t.expanding, col.id)
			if t.ix != ix || gen != t.gen.Load() && t.reading || len(found) == 0 {
				return
			}
			at := slices.Index(t.cols, col)
			if at < 0 {
				return
			}
			var add []*jsonlColumn
			for _, m := range found {
				if t.column(m.id) == nil {
					add = append(add, m)
				} else {
					t.column(m.id).hidden = false
				}
			}
			t.cols = slices.Insert(t.cols, at+1, add...)
			col.expanded = true
			if t.sort.Column == col.id {
				t.sort = ui.SortOrder{}
			}
			a.invalidate()
		})
	})
}

// collapse shows an expanded column again in place of its members.
func (t *jsonlTable) collapse(col *jsonlColumn) {
	col.expanded = false
	gone := map[string]bool{col.id: false}
	t.cols = slices.DeleteFunc(t.cols, func(m *jsonlColumn) bool {
		if _, ok := gone[m.parent]; ok && m.parent != "" {
			gone[m.id] = true
			return true
		}
		return false
	})
	if gone[t.sort.Column] {
		t.sort = ui.SortOrder{}
	}
}

// jsonlInput takes the keys a table answers beyond those of its rows.
func (a *app) jsonlInput(v *viewer, ev ui.InputEvent) bool {
	t := v.jsonl
	switch {
	case ev.Kind == ui.InputCommand && ev.Text == "copy":
		if t.row < 0 || t.row >= t.count() {
			return false
		}
		row := t.rowAt(t.row)
		if col := t.column(t.list.Cells.Cursor); col != nil && !t.list.Cells.All {
			a.copyJSONLValue(t, row, col)
		} else {
			a.copyJSONLLine(t, row)
		}
		return true
	case ev.Kind == ui.InputKeyDown && ev.Key == ui.KeySpace && ev.Mods == 0:
		t.details = !t.details
		return true
	case ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape && (t.query != "" || len(t.where) > 0):
		t.clearFilters()
		return true
	}
	return false
}

// copyJSONLValue copies the value of a row's cell: a string's text, or the
// value as JSON.
func (a *app) copyJSONLValue(t *jsonlTable, row int32, col *jsonlColumn) {
	raw, ok := lookup(t.ix.line(int(row)), col.path)
	if !ok || t.ix.kinds[row] == jsonlInvalid {
		return
	}
	if raw != "" && raw[0] == '"' {
		raw = unquote(raw)
	}
	a.services.WriteClipboard(raw)
	t.wash = jsonlWash{row: row, id: col.id, fresh: true}
	a.invalidate()
}

// copyJSONLLine copies a row's line whole, which its cells and the
// details' Copy button show.
func (a *app) copyJSONLLine(t *jsonlTable, row int32) {
	a.services.WriteClipboard(t.ix.line(int(row)))
	t.wash = jsonlWash{row: row, fresh: true}
	a.invalidate()
}

// copyWash washes a cell of row in the column of id over for a moment once
// it was copied, the wash fading over motionEnter: what ⌘C took shows
// without a word.
func (a *app) copyWash(c *ui.Context, t *jsonlTable, row int32, id string) {
	w := t.wash
	if w.at.IsZero() || w.row != row || w.id != "" && w.id != id {
		return
	}
	since := c.Now().Sub(w.at)
	if since < 0 || since >= motionEnter {
		return
	}
	c.AnimationFrame()
	k := 1 - ui.EaseOut(float32(since)/float32(motionEnter))
	ui.Box(c.Key("copied")).Absolute().Left(0).Right(0).Top(0).Bottom(0).PassThrough().Background(a.pal.text.Alpha(0.16 * k))
}

// jsonlDetails shows the chosen row whole, indented.
func (a *app) jsonlDetails(c *ui.Context, v *viewer) {
	t, p := v.jsonl, a.pal
	ui.Column(c.Key("jsonl-details")).Fill().Background(p.editor).Children(func() {
		if t.row < 0 || t.row >= t.count() {
			ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, locales.JsonlNoRow()).FontSize(12).TextColor(p.muted)
			})
			return
		}
		row := t.rowAt(t.row)
		line := t.ix.line(int(row))
		number := int(t.ix.lines[row]) + 1
		invalid := t.ix.kinds[row] == jsonlInvalid
		// The field of the cursor's column, which the details mark.
		var field *jsonlColumn
		if col := t.column(t.list.Cells.Cursor); col != nil && !invalid && len(col.path) > 0 && !t.list.Cells.All {
			field = col
		}
		ui.Row(c.Key("jsonl-details-bar")).Padding(4, 10).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, locales.JsonlLine(number)).FontSize(12).FontWeight(600).TextColor(p.muted).FontFeatures("tnum").Shrink(0)
			if field != nil {
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint).Shrink(0)
				ui.Text(c.Key("jsonl-field"), field.title()).FontSize(12).Font(a.font.family).TextColor(p.text).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			copied := !t.copied.IsZero() && c.Now().Sub(t.copied) < jsonlCopiedFor
			icon, iconCol, copyLabel := iconCopy, p.muted, locales.JsonlCopyLine()
			if copied {
				icon, iconCol, copyLabel = iconCheck, p.addedFg, locales.JsonlCopied()
				c.After(jsonlCopiedFor - c.Now().Sub(t.copied))
			}
			b := ui.ButtonBase(c.Key("jsonl-copy")).Size(24, 22).Radius(4).AlignItems(ui.Center).Justify(ui.Center).
				Label(copyLabel).Tooltip(copyLabel)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			pressPose(b)
			b.Children(func() {
				ui.Icon(c.Key("icon"), icon).FontSize(13).TextColor(iconCol).Transition(hoverFade).ContentTransition(ui.ContentTransition{})
			})
			if b.Clicked() {
				a.copyJSONLLine(t, row)
			}
			source := a.withKey(locales.JsonlShowInSource(), "view.togglePreview")
			s := ui.ButtonBase(c.Key("jsonl-source")).Size(24, 22).Radius(4).AlignItems(ui.Center).Justify(ui.Center).
				Label(source).Tooltip(source)
			if s.Hovered() {
				s.Background(p.hover)
			}
			s.Transition(hoverFade)
			pressPose(s)
			s.Children(func() { ui.Icon(c, iconFileCode).FontSize(13).TextColor(p.muted) })
			if s.Clicked() {
				a.post(func() { a.jsonlToSource(v, row) })
			}
		})
		var path []jsonlStep
		key := strconv.Itoa(int(row)) + ":" + strconv.FormatUint(t.shown.version, 10) + ":" + t.shown.query
		if field != nil {
			path, key = field.path, key+":"+field.id
		}
		if t.prettyOf != key {
			t.pretty, t.prettyText, t.markAt = a.prettySpans(line, invalid, path, t.shown.query)
			t.prettyOf = key
		}
		fieldID := ""
		if field != nil {
			fieldID = field.id
		}
		// The scroll is placed before it takes its state, which it then
		// shows in this frame.
		sc := ui.Scroll(c.Key("jsonl-pretty"))
		t.revealField(sc, row, fieldID)
		sc.TrackScroll(&t.prettyScroll).Grow(1).Padding(prettyPad, 10, 12, 10).Children(func() {
			ui.RichText(c.Key("jsonl-pretty-text"), t.pretty...).FontSize(prettySize).Font(a.font.family).LineHeight(prettyLineH / prettySize).Selectable()
		})
	})
}

// prettySpans are a row's line indented and colored as the editor colors
// JSON, or, for a line that is not JSON, why and the line; the member at
// path and the matches of query marked. It returns their text too, and
// where the member at path starts in it, -1 for none.
func (a *app) prettySpans(line string, invalid bool, path []jsonlStep, query string) ([]ui.Span, string, int) {
	p := a.pal
	if invalid {
		why := locales.JsonlNotJSON(jsonlError(line)) + "\n\n"
		return []ui.Span{
			{Text: why, Color: p.dangerFg},
			{Text: line, Color: p.text},
		}, why + line, -1
	}
	text := prettyJSON(line)
	more := false
	if len(text) > jsonlPrettyMax {
		cut := strings.LastIndexByte(text[:jsonlPrettyMax], '\n')
		text, more = text[:max(cut, 0)], true
	}
	var spans []ui.Span
	add := func(s string, col ui.Color) {
		if n := len(spans); n > 0 && spans[n-1].Color == col {
			spans[n-1].Text += s
			return
		}
		spans = append(spans, ui.Span{Text: s, Color: col})
	}
	for i := 0; i < len(text); {
		ch := text[i]
		switch {
		case ch == '"':
			e := skipString(text, i)
			col := p.syntax[highlight.String]
			if k := skipSpace(text, e); k < len(text) && text[k] == ':' {
				col = p.syntax[highlight.Attribute]
			}
			add(text[i:e], col)
			i = e
		case ch == '-' || isDigit(ch):
			e := skipValue(text, i)
			add(text[i:e], p.syntax[highlight.Number])
			i = e
		case ch == 't' || ch == 'f' || ch == 'n':
			e := skipValue(text, i)
			add(text[i:e], p.syntax[highlight.Constant])
			i = e
		default:
			e := i + 1
			for e < len(text) && !strings.ContainsRune("\"-0123456789tfn", rune(text[e])) {
				e++
			}
			add(text[i:e], p.muted)
			i = e
		}
	}
	if more {
		add("\n…", p.muted)
	}
	// Past the text, "…" is never marked.
	at := -1
	if from, to, ok := lookupAt(text, path); ok {
		spans, at = markSpans(spans, from, to, p.word), from
	}
	for _, h := range foldHits(text, foldASCII(query), jsonlHitsMax) {
		spans = markSpans(spans, h[0], h[1], p.match)
	}
	return spans, text, at
}

// prettyLineH is the height of a line of the details' text, and
// prettyPad the room above it.
const (
	prettySize  = 12.5
	prettyLineH = prettySize * 1.45
	prettyPad   = 4
)

// prettyTop is how far down the details' text, laid out width wide, the
// line holding byte at starts: its lines counted, each as many as its
// characters wrap to, as the editor's font is monospaced.
func prettyTop(text string, at int, width float32) float32 {
	perLine := max(int(width/(prettySize*0.6)), 1)
	lines := 0
	for rest := text[:at]; ; {
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			break
		}
		lines += 1 + (utf8.RuneCountInString(rest[:nl])-1)/perLine
		rest = rest[nl+1:]
	}
	return prettyPad + float32(lines)*prettyLineH
}

// revealField scrolls the details of row to the member of the chosen
// field, as little as shows it, and to their top for another row: at once
// as the keys move along the rows, gliding over motionMove to another
// field of the row when it is near.
func (t *jsonlTable) revealField(sc ui.Element, row int32, field string) {
	reveal := strconv.Itoa(int(row)) + ":" + field
	if t.revealed == reveal {
		return
	}
	sameRow := strings.HasPrefix(t.revealed, strconv.Itoa(int(row))+":")
	t.revealed = reveal
	s := &t.prettyScroll
	view := sc.Bounds()
	top := s.Y
	switch {
	case t.markAt >= 0 && view.H > 0:
		y := prettyTop(t.prettyText, t.markAt, view.W-20)
		if y < s.Y || y+prettyLineH > s.Y+view.H {
			top = max(0, y-view.H/3)
		}
	case !sameRow:
		top = 0
	}
	if top == s.Y && !s.Scrolling() {
		return
	}
	glide := ui.Animation{}
	if d := top - s.Y; sameRow && max(d, -d) < 3*view.H {
		glide = ui.Animation{Duration: motionMove, Ease: ui.EaseOut}
	}
	if glide.Duration == 0 && !s.Scrolling() {
		s.Y = top
		return
	}
	s.ScrollTo(s.X, top, glide)
}

// jsonlToSource shows a table's text in its place, at the line of row.
func (a *app) jsonlToSource(v *viewer, row int32) {
	d := a.activeEditor()
	if d == nil || d.preview != v {
		return
	}
	t := v.jsonl
	if i := slices.Index(t.viewRows(), row); i >= 0 {
		t.row = i
	}
	a.setPreview(d, false)
	a.focusColumn()
}

// jsonlModeButtons choose how a file of JSON Lines shows: its source or
// its table, the one it shows marked.
func (a *app) jsonlModeButtons(c *ui.Context, t *colTab, d *editor) {
	p := a.pal
	table := d.preview != nil
	modes := []struct {
		on    bool
		label string
		icon  *ui.SVG
	}{
		{!table, locales.MarkdownModeSource(), iconFileCode},
		{table, locales.JsonlModeTable(), iconRows},
	}
	ui.Row(c.Key("jsonl-modes")).Height(crumbsH - 4).Padding(1).Gap(1).Radius(5).Background(p.hover.Alpha(0.5)).
		AlignItems(ui.Center).Shrink(0).Label(locales.JsonlModes()).Children(func() {
		for k, m := range modes {
			label := a.withKey(m.label, "view.togglePreview")
			b := ui.ButtonBase(c.Key("jsonl-mode:"+strconv.Itoa(k))).Height(crumbsH-6).Padding(0, 6).Radius(4).
				AlignItems(ui.Center).Label(label).Tooltip(label).Selected(m.on)
			col := p.muted
			switch {
			case m.on:
				// The mode shown is marked by color, not by weight, so the
				// buttons hold still.
				b.Background(p.editor)
				col = p.text
			case b.Hovered():
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			pressPose(b)
			b.Children(func() { ui.Icon(c, m.icon).FontSize(13).TextColor(col) })
			if b.Clicked() && !m.on {
				a.post(func() {
					if col, i := a.locate(t); col != nil {
						a.activate(col, i, true)
					}
					a.setPreview(d, !table)
					a.focusColumn()
				})
			}
		}
	})
}

// setJSONLOpen has files of JSON Lines open as their table or source.
func (a *app) setJSONLOpen(mode string) {
	a.setPref("jsonlMode", func(p *settings.Prefs) { p.JSONLMode = mode })
}

// jsonlStatus is the status bar's part for a table: the lines that are
// not JSON, which a click goes through, and its columns.
func (a *app) jsonlStatus(v *viewer, item func(key, label, tip string, icon *ui.SVG, fn func())) {
	t := v.jsonl
	if t.ix == nil || t.ix.rows() == 0 {
		return
	}
	if n := t.ix.invalid; n > 0 {
		item("jsonl-invalid", locales.JsonlInvalid(n), locales.JsonlNextInvalid(), iconTriangleAlert, func() { a.nextInvalidJSONL(v) })
	}
	item("jsonl-columns", locales.JsonlColumns(len(t.shownColumns())), "", nil, nil)
}

// nextInvalidJSONL chooses the next row shown after the chosen one that
// is not JSON, from the top past the last.
func (a *app) nextInvalidJSONL(v *viewer) {
	t := v.jsonl
	n := t.count()
	for k := 1; k <= n; k++ {
		i := (max(t.row, -1) + k + n) % n
		if t.ix.kinds[t.rowAt(i)] == jsonlInvalid {
			t.row = i
			t.chosen.Clear()
			t.chosen.Add(int(t.rowAt(i)))
			t.list.ScrollIntoView(i)
			v.pane.Focus()
			return
		}
	}
}

// markSpans gives the text of spans from byte from to byte to, counted
// across them all, the background bg.
func markSpans(spans []ui.Span, from, to int, bg ui.Color) []ui.Span {
	if from >= to {
		return spans
	}
	out := make([]ui.Span, 0, len(spans)+2)
	at := 0
	for _, sp := range spans {
		start, end := at, at+len(sp.Text)
		at = end
		lo, hi := max(from, start)-start, min(to, end)-start
		if lo >= hi {
			out = append(out, sp)
			continue
		}
		if lo > 0 {
			head := sp
			head.Text = sp.Text[:lo]
			out = append(out, head)
		}
		mid := sp
		mid.Text, mid.Background = sp.Text[lo:hi], bg
		out = append(out, mid)
		if hi < len(sp.Text) {
			tail := sp
			tail.Text = sp.Text[hi:]
			out = append(out, tail)
		}
	}
	return out
}
