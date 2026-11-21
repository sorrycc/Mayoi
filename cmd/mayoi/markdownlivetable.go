package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// liveRow is a row of a table Live Preview draws: how high it was as last
// built, 0 before it was, and the box it was built in.
type liveRow struct {
	h   float32
	box ui.Handle
}

// liveRowsShown is how far beyond the view, in views, the rows of a table
// are built: they are built for where the frame scrolls, so that this is
// only for what a scroll moves on in it, as it glides.
const liveRowsShown = 0.5

// liveTableRows builds the rows of the table lv draws at y, from the top
// of the view viewH high, that are in view or near it, and the row of the
// cell edited, cell, -1 for none: the rows between take their room, as
// high as they were built, so that the table is as high as all of its
// rows, its columns as wide, wherever it is scrolled to. It returns how
// far the view scrolls for the cell edited, if reveal, to show, and
// whether its row shows whole.
func (a *app) liveTableRows(c *ui.Context, lv *liveView, y, viewH, width float32, font ui.Font, cell int, reveal bool) (shift float32, shown bool) {
	m := lv.v.markdown
	n := len(m.blocks)
	size, lh := a.settings.FontSize, a.settings.LineHeight
	// The heights of the rows the last frame built. The text area keeps
	// what shows where it is as they change, by the room of each row
	// (liveTableRooms).
	for _, i := range lv.built {
		if i < len(lv.rows) {
			// Not for less than a pixel, as a row laid out again at
			// another place comes out a little more or less: its room in
			// the text area would change, which lays its line out again.
			if got := lv.rows[i].box.Bounds(c).H; got > 0 && max(got-lv.rows[i].h, lv.rows[i].h-got) >= 0.5 {
				lv.rows[i].h = got
			}
		}
	}
	if len(lv.rows) != n {
		// Rows came or went, as from a cell: those before keep their
		// heights.
		rows := make([]liveRow, n)
		for i := range min(n, len(lv.rows)) {
			rows[i].h = lv.rows[i].h
		}
		lv.rows = rows
	}
	// All of them, once shown and for each width, to know how high each
	// is; then those in view.
	full := !lv.measured || lv.rowsW != width || lv.rowsFont != font
	lv.rowsW, lv.rowsFont = width, font
	// Those not built yet as high as those built are, on the average.
	sum, count := float32(0), 0
	for i := 1; i < n; i++ {
		if h := lv.rows[i].h; h > 0 {
			sum, count = sum+h, count+1
		}
	}
	lv.rowEst = size*lh + size*0.8 + 1
	if count > 0 {
		lv.rowEst = sum / float32(count)
	}
	height := func(i int) float32 {
		if lv.rows[i].h > 0 {
			return lv.rows[i].h
		}
		return lv.rowEst
	}

	lv.built = lv.built[:0]
	room := float32(0)
	at := y
	for i := range n {
		h := height(i)
		if i == cell {
			shown = at >= 0 && at+h <= viewH
		}
		if i == cell && reveal {
			// As little as shows it, a line's room around it.
			margin := size * lh
			if top, bottom := at, at+h; top < margin {
				shift += top - margin
			} else if over := bottom - (viewH - margin); over > 0 {
				shift += min(over, top-margin)
			}
		}
		near := at+h >= -viewH*liveRowsShown && at <= viewH*(1+liveRowsShown)
		at += h
		if !full && !near && i != cell {
			room += h
			continue
		}
		if room > 0 {
			ui.Box(c).WidthPercent(100).Height(room)
			room = 0
		}
		lv.built = append(lv.built, i)
		ui.Column(c.Key("live-row:" + strconv.Itoa(i))).WidthPercent(100).Bind(&lv.rows[i].box).Children(func() {
			a.markdownRow(c, lv.v, i, width, font, nil)
		})
	}
	if room > 0 {
		ui.Box(c).WidthPercent(100).Height(room)
	}
	return shift, shown
}

// liveTableRooms are the rooms the text area keeps for the lines of a
// table drawn, first up to last, height high in all, its drawing top
// below the room's top: one for each row, as high as it was built or as
// those built are, the header's with its line of alignments and the room
// above the drawing, the last row's with what is left, all of them
// smaller alike while the room is smaller, as it glides. Each line of the
// table is then where its row shows, for the lines the view finds in it
// and for a caret waiting in view; and the text area, which takes the
// height of a line's room as it lays the line out, never keeps one of a
// table's lines as of no height. Nil where its rows are not its lines.
func (a *app) liveTableRooms(lv *liveView, first, last int, height, top float32) []ui.TextBlock {
	n := len(lv.v.markdown.blocks)
	if n < 2 || last-first != n {
		return nil
	}
	est := lv.rowEst
	if est <= 0 {
		size := a.settings.FontSize
		est = size*a.settings.LineHeight + size*0.8 + 1
	}
	h := func(i int) float32 {
		if i < len(lv.rows) && lv.rows[i].h > 0 {
			return lv.rows[i].h
		}
		return est
	}
	total := top
	for i := range n {
		total += h(i)
	}
	k := float32(1)
	if total > height {
		k = height / total
	}
	rooms := lv.rooms[:0]
	at := (top + h(0)) * k
	rooms = append(rooms, ui.TextBlock{First: first, Last: first + 1, Height: at})
	for i := 1; i < n-1; i++ {
		rooms = append(rooms, ui.TextBlock{First: first + 1 + i, Last: first + 1 + i, Height: h(i) * k})
		at += h(i) * k
	}
	rooms = append(rooms, ui.TextBlock{First: last, Last: last, Height: max(0, height-at)})
	lv.rooms = rooms
	return rooms
}

// liveCellPark is where the text area's caret waits while a cell of a
// table is edited in place: at the start of the cell's row, as long as
// the last frame showed it or it is still to be scrolled into view, as a
// row just added. An edit, as one written from the cell, has
// the area take the heights of its lines for guesses until it measures
// them again, so that the line in the middle of the view could be taken
// for one far below, and the view scroll to it.
func liveCellPark(d *editor, snap ui.TextSnapshot) (int, bool) {
	cell := &d.liveCell
	line := cell.rowLine(cell.row)
	if !cell.on || !cell.shown && cell.reveal == 0 || line >= snap.LineCount() {
		return 0, false
	}
	at, _ := snap.LineRange(line)
	return at, true
}
