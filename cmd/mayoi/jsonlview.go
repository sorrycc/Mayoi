package main

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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

// jsonlPrettyMax is the most of a row's pretty JSON its details show, in
// bytes; Show in Source has the rest.
const jsonlPrettyMax = 256 << 10

// jsonlWant is what the rows shown are of: the text's version, the
// filter's query and the sort.
type jsonlWant struct {
	version uint64
	query   string
	sort    string
	desc    bool
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
	return jsonlWant{version: version, query: t.query, sort: t.sort.Column, desc: t.sort.Descending}
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
			a.applyJSONL(t, readJSONL(snap.String(), nil, &jsonlRead{}, want, func() bool { return false }))
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
	bg.Go(func() {
		defer recoverCrash()
		stop := func() bool { return t.gen.Load() != gen }
		text := ""
		if ix == nil {
			text = snap.String()
		}
		r := readJSONL(text, ix, &had, want, stop)
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
// shows, nil once stop says to; had has the sort keys found last.
func readJSONL(text string, ix *jsonlIndex, had *jsonlRead, want jsonlWant, stop func() bool) *jsonlRead {
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
	r.view = jsonlView(ix, filter, want.query != "", r.keys, want.desc)
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
	p := a.pal
	ui.Column(c.Key("jsonl")).Fill().Background(p.editor).Children(func() {
		a.jsonlBar(c, v)
		ui.Divider(c)
		switch {
		case t.ix == nil:
		case t.ix.rows() == 0:
			a.viewEmpty(c, "file-text", locales.JsonlEmpty())
		case t.count() == 0 && t.shown.query != "":
			a.viewEmpty(c, "search", locales.JsonlNoMatch(t.shown.query))
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

// jsonlBar is the bar over a table: its filter, and what it shows.
func (a *app) jsonlBar(c *ui.Context, v *viewer) {
	t, p := v.jsonl, a.pal
	ui.Row(c.Key("jsonl-bar")).Padding(6, 10).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c.Key("jsonl-filter")).Width(260).Shrink(1).Children(func() {
			in := a.searchField(c, "jsonl-query", &t.query, locales.JsonlFilter(), &t.input, nil)
			in.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
					if t.query != "" {
						t.query = ""
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
		var msg string
		switch {
		case t.ix == nil:
		case t.shown.query != "":
			msg = locales.JsonlRowsOf(t.count(), t.ix.rows())
		default:
			msg = locales.JsonlRows(t.ix.rows())
		}
		if t.ix != nil && t.ix.partial || t.reading && time.Since(t.readingAt) >= readingShows {
			msg = locales.JsonlReading()
		} else if t.reading {
			c.After(readingShows - time.Since(t.readingAt))
		}
		ui.Text(c, msg).FontSize(12).TextColor(p.muted).FontFeatures("tnum").SingleLine()
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
			a.services.WriteClipboard(t.ix.line(int(row)))
		}
		m.Separator()
		if m.Item(a.withKey(locales.JsonlShowInSource(), "view.togglePreview")).Chosen() {
			a.post(func() { a.jsonlToSource(v, row) })
		}
	}
	mono := a.font.family
	table := ui.Table(c.Key("jsonl-table"), s, cols, t.count(), func(i, j int) {
		row := t.rowAt(i)
		if j == 0 {
			col := p.gutter
			if t.ix.kinds[row] == jsonlInvalid {
				col = p.dangerFg
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
		text := ui.Text(c, cell.text).FontSize(size).Font(mono).SingleLine()
		if !s.CellSelected(i, j) {
			// A chosen cell's text takes the accent's.
			text.TextColor(a.cellColor(cell.kind))
		}
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
			a.services.WriteClipboard(t.ix.line(int(row)))
		}
		return true
	case ev.Kind == ui.InputKeyDown && ev.Key == ui.KeySpace && ev.Mods == 0:
		t.details = !t.details
		return true
	case ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape && t.query != "":
		t.query = ""
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
		ui.Row(c.Key("jsonl-details-bar")).Padding(4, 10).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, locales.JsonlLine(number)).FontSize(12).FontWeight(600).TextColor(p.muted).FontFeatures("tnum")
			ui.Spacer(c)
			copyLabel := locales.JsonlCopyLine()
			b := ui.ButtonBase(c.Key("jsonl-copy")).Size(24, 22).Radius(4).AlignItems(ui.Center).Justify(ui.Center).
				Label(copyLabel).Tooltip(copyLabel)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			pressPose(b)
			b.Children(func() { ui.Icon(c, iconCopy).FontSize(13).TextColor(p.muted) })
			if b.Clicked() {
				a.services.WriteClipboard(line)
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
		key := strconv.Itoa(int(row)) + ":" + strconv.FormatUint(t.shown.version, 10)
		if t.prettyOf != key {
			t.pretty, t.prettyOf = a.prettySpans(line, t.ix.kinds[row] == jsonlInvalid), key
		}
		ui.Scroll(c.Key("jsonl-pretty")).Grow(1).Padding(4, 10, 12, 10).Children(func() {
			ui.RichText(c.Key("jsonl-pretty-text"), t.pretty...).FontSize(12.5).Font(a.font.family).LineHeight(1.45).Selectable()
		})
	})
}

// prettySpans are a row's line indented and colored as the editor colors
// JSON, or, for a line that is not JSON, why and the line.
func (a *app) prettySpans(line string, invalid bool) []ui.Span {
	p := a.pal
	if invalid {
		return []ui.Span{
			{Text: locales.JsonlNotJSON(jsonlError(line)) + "\n\n", Color: p.dangerFg},
			{Text: line, Color: p.text},
		}
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
	return spans
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
