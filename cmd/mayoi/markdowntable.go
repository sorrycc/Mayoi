package main

import (
	"strings"
)

// tableRowCells finds the cells of a row of a table of Markdown, a line,
// after the > of its quotes and its indent: the bytes of each cell's text,
// its white space trimmed, split at the pipes that are not escaped, \|.
// An empty cell is the room between its pipes. trailing reports whether
// the row ends with a pipe.
func tableRowCells(line string) (cells [][2]int, trailing bool) {
	i := len(liveQuoteMarks.FindString(line))
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	end := len(strings.TrimRight(line, " \t\r"))
	if i < end && line[i] == '|' {
		i++
	}
	start := i
	trim := func(s, e int) [2]int {
		ts, te := s, e
		for ts < te && (line[ts] == ' ' || line[ts] == '\t') {
			ts++
		}
		for te > ts && (line[te-1] == ' ' || line[te-1] == '\t') {
			te--
		}
		if ts == te {
			return [2]int{s, e} // empty: all of its room
		}
		return [2]int{ts, te}
	}
	for ; i < end; i++ {
		switch line[i] {
		case '\\':
			i++
		case '|':
			cells = append(cells, trim(start, i))
			start = i + 1
		}
	}
	if start < end || len(cells) == 0 {
		cells = append(cells, trim(start, end))
	} else {
		trailing = true
	}
	return cells, trailing
}

// tableCellSource is the text typed into a cell as its source: its pipes
// escaped, as they would end the cell otherwise, and on one line.
func tableCellSource(text string) string {
	if !strings.ContainsAny(text, "|\r\n") {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		switch ch := text[i]; ch {
		case '\\':
			b.WriteByte(ch)
			if i+1 < len(text) && text[i+1] != '\r' && text[i+1] != '\n' {
				i++
				b.WriteByte(text[i])
			}
		case '|':
			b.WriteString(`\|`)
		case '\r', '\n':
			b.WriteByte(' ')
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// tableCellEdit is how a row of a table, line, takes text in its cell k:
// the bytes of the line replaced, and their text. A cell the row is short
// of is added after its last, with any between them empty.
func tableCellEdit(line string, k int, text string) (start, end int, with string) {
	cells, trailing := tableRowCells(line)
	if k < len(cells) {
		c := cells[k]
		if strings.TrimSpace(line[c[0]:c[1]]) == "" {
			// An empty cell: its text stands a space from its pipes.
			if text == "" {
				return c[0], c[0], ""
			}
			return c[0], c[1], " " + text + " "
		}
		return c[0], c[1], text
	}
	end = len(strings.TrimRight(line, " \t\r"))
	var b strings.Builder
	for i := len(cells); i <= k; i++ {
		t := ""
		if i == k {
			t = text
		}
		if trailing {
			b.WriteString(" " + t + " |")
		} else {
			b.WriteString(" | " + t)
		}
	}
	return end, end, b.String()
}

// tableCellText is the source of cell k of a row of a table, line, its
// white space trimmed: empty for a cell the row is short of.
func tableCellText(line string, k int) string {
	cells, _ := tableRowCells(line)
	if k >= len(cells) {
		return ""
	}
	return strings.TrimSpace(line[cells[k][0]:cells[k][1]])
}

// tableRowRooms finds the rooms of the cells of a row of a table, a line:
// the bytes between its pipes that are not escaped, white space and all.
// start is where the row starts after the > of its quotes and its indent,
// end where it ends before its trailing white space, and lead and trail
// report whether it starts and ends with a pipe.
func tableRowRooms(line string) (rooms [][2]int, lead, trail bool, start, end int) {
	i := len(liveQuoteMarks.FindString(line))
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	start, end = i, len(strings.TrimRight(line, " \t\r"))
	if i < end && line[i] == '|' {
		lead = true
		i++
	}
	s := i
	for ; i < end; i++ {
		switch line[i] {
		case '\\':
			i++
		case '|':
			rooms = append(rooms, [2]int{s, i})
			s = i + 1
		}
	}
	if s < end || len(rooms) == 0 {
		rooms = append(rooms, [2]int{s, max(s, end)})
	} else {
		trail = true
	}
	return rooms, lead, trail, start, end
}

// tableDelimiter reports how many columns a table's line of alignments,
// line, has, as | :--- | ---: |, or -1 for a line that is not one.
func tableDelimiter(line string) int {
	cells, _ := tableRowCells(line)
	for _, c := range cells {
		t := strings.TrimSpace(line[c[0]:c[1]])
		t = strings.TrimPrefix(strings.TrimSuffix(t, ":"), ":")
		if t == "" || strings.Trim(t, "-") != "" {
			return -1
		}
	}
	return len(cells)
}

// tableOp is a change of a table's rows or columns.
type tableOp int

const (
	tableRowAbove tableOp = iota
	tableRowBelow
	tableRowDelete
	tableColLeft
	tableColRight
	tableColDelete
)

// tableEdit replaces the bytes start up to end of a table's source with
// text.
type tableEdit struct {
	start, end int
	text       string
}

// tableShape is how many rows a table's source has, its header one, and
// how many columns its header has: 0, 0 for a source that is not a table,
// a header and a line of alignments as wide.
func tableShape(lines []string) (rows, cols int) {
	if len(lines) < 2 {
		return 0, 0
	}
	cells, _ := tableRowCells(lines[0])
	if tableDelimiter(lines[1]) != len(cells) {
		return 0, 0
	}
	return len(lines) - 1, len(cells)
}

// tableCanDo reports whether op can change a table of rows and cols at its
// cell of row r, 0 its header, and column k: the header stays the first
// row, and the table keeps a column.
func tableCanDo(op tableOp, rows, cols, r, k int) bool {
	if r < 0 || r >= rows || k < 0 || k >= cols {
		return false
	}
	switch op {
	case tableRowAbove, tableRowDelete:
		return r > 0
	case tableColDelete:
		return cols > 1
	}
	return true
}

// tableStructure is how op changes a table, its source src, at its cell
// of row r and column k: the edits of src, which do not overlap, as rows
// added or removed whole, and cells added to or removed from each row, the
// rest of each row as it was. A new row is empty, as wide as the header,
// quoted and indented as the row it goes by; a new column is empty, ---
// in the line of alignments. A row short of the column is left as it is.
func tableStructure(src string, op tableOp, r, k int) ([]tableEdit, bool) {
	lines := strings.Split(src, "\n")
	rows, cols := tableShape(lines)
	if !tableCanDo(op, rows, cols, r, k) {
		return nil, false
	}
	// at is where line i starts in src, and end where it ends.
	at := make([]int, len(lines)+1)
	for i, l := range lines {
		at[i+1] = at[i] + len(l) + 1
	}
	end := func(i int) int { return at[i] + len(lines[i]) }
	line := r + 1
	if r == 0 {
		line = 0
	}
	switch op {
	case tableRowAbove:
		return []tableEdit{{end(line - 1), end(line - 1), "\n" + tableEmptyRow(lines[line], cols)}}, true
	case tableRowBelow:
		after := max(line, 1)
		return []tableEdit{{end(after), end(after), "\n" + tableEmptyRow(lines[line], cols)}}, true
	case tableRowDelete:
		return []tableEdit{{end(line - 1), end(line), ""}}, true
	}
	var edits []tableEdit
	for i, l := range lines {
		var s, e int
		var with string
		var ok bool
		switch op {
		case tableColLeft, tableColRight:
			text := ""
			if i == 1 {
				text = "---"
			}
			s, e, with, ok = tableColInsert(l, k, op == tableColRight, text)
		case tableColDelete:
			s, e, with, ok = tableColRemove(l, k)
		}
		if ok {
			edits = append(edits, tableEdit{at[i] + s, at[i] + e, with})
		}
	}
	return edits, true
}

// tableEmptyRow is an empty row of cols cells, quoted and indented as the
// row of a table like, between pipes, which an empty row needs to be one.
func tableEmptyRow(like string, cols int) string {
	_, _, _, start, _ := tableRowRooms(like)
	return like[:start] + "|" + strings.Repeat("  |", cols)
}

// tableColInsert is how a row of a table, line, takes a new cell of text
// before its cell k, or after it if right: the bytes of the line replaced,
// and their text. A row short of cell k takes none.
func tableColInsert(line string, k int, right bool, text string) (start, end int, with string, ok bool) {
	rooms, lead, trail, s, e := tableRowRooms(line)
	if k >= len(rooms) {
		return 0, 0, "", false
	}
	j := k
	if right {
		j++
	}
	room := "  "
	if text != "" {
		room = " " + text + " "
	}
	switch {
	case j == 0 && !lead:
		// The row's first cell, which a pipe now starts.
		return s, s, "|" + room + "| ", true
	case j < len(rooms):
		return rooms[j][0], rooms[j][0], room + "|", true
	case trail:
		return e, e, room + "|", true
	}
	return e, e, " |" + room + "|", true
}

// tableColRemove is how a row of a table, line, loses its cell k, with a
// pipe beside it: the bytes of the line replaced, and their text. A row
// short of the cell stays as it is; a row left with one cell keeps pipes
// around it, as a row needs one.
func tableColRemove(line string, k int) (start, end int, with string, ok bool) {
	rooms, lead, trail, s, e := tableRowRooms(line)
	switch {
	case k >= len(rooms):
		return 0, 0, "", false
	case len(rooms) == 1:
		return s, e, "|  |", true
	case k < len(rooms)-1:
		start, end = rooms[k][0], rooms[k][1]+1
		if k == 0 && !lead {
			// Its first cell: the next starts the row.
			for end < rooms[1][1] && (line[end] == ' ' || line[end] == '\t') {
				end++
			}
		}
	default:
		start, end = rooms[k][0]-1, rooms[k][1]
	}
	if len(rooms) == 2 && !lead && !trail {
		// One cell left without a pipe: it would not be a row.
		rest := line[s:start] + line[end:e]
		return s, len(line), "| " + strings.TrimSpace(rest) + " |", true
	}
	return start, end, "", true
}

// applyTableEdits makes edits of a table's source, src.
func applyTableEdits(src string, edits []tableEdit) string {
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		src = src[:e.start] + e.text + src[e.end:]
	}
	return src
}
