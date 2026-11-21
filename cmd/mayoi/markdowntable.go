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
