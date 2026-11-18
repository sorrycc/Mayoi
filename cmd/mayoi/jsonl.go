package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// JSON Lines: a file of one JSON value a line, as datasets and logs keep
// them, which the Table view shows a row a line. The index here is what
// that view reads: where each line is and what it holds, made off the main
// thread, and the cells of the rows in view read from the lines as they
// show, so that a file of a million lines costs what the rows in view do.

// isJSONLPath reports whether a file is JSON Lines, by its extension.
func isJSONLPath(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, ".jsonl") || strings.EqualFold(ext, ".ndjson")
}

// isJSONL reports whether a document is JSON Lines.
func isJSONL(d *document) bool { return d.path != "" && isJSONLPath(d.path) }

// What a row's line holds.
const (
	jsonlObject  byte = iota // an object, whose keys are columns
	jsonlValue               // a value that is not an object
	jsonlInvalid             // text that is not JSON
)

// jsonlChunk is how much text one worker indexes at a time.
const jsonlChunk = 1 << 20

// jsonlCellMax is the most of a value a cell shows, in runes.
const jsonlCellMax = 256

// jsonlMaxColumns is how many columns the table shows of its own; the
// keys past them are hidden until shown.
const jsonlMaxColumns = 200

// jsonlIndex is where each row of a text of JSON Lines is and what it
// holds. A row is a line that is not blank.
type jsonlIndex struct {
	text string
	// starts and ends are the bytes of each row's line, without the space
	// around it, and lines the line of the text it is, from 0.
	starts, ends, lines []uint32
	kinds               []byte
	// keys are the keys of the objects, in the order they first come, and
	// counts how many objects have each.
	keys   []string
	counts []int
	// values counts the rows of values that are not objects, invalid
	// those that are not JSON.
	values, invalid int
	// partial is set for an index of the text's head only, as the first
	// frame of a large file shows.
	partial bool

	foldOnce sync.Once
	folded   string
}

// rows is how many rows the index has.
func (ix *jsonlIndex) rows() int { return len(ix.starts) }

// line returns the text of row i.
func (ix *jsonlIndex) line(i int) string { return ix.text[ix.starts[i]:ix.ends[i]] }

// rowOfLine returns the row of a line of the text, or of the first line
// after it that is a row; rows() when none is.
func (ix *jsonlIndex) rowOfLine(line int) int {
	i, _ := slices.BinarySearch(ix.lines, uint32(max(line, 0)))
	return i
}

// fold returns the text with its ASCII letters in lower case, of the same
// length, which filters search.
func (ix *jsonlIndex) fold() string {
	ix.foldOnce.Do(func() { ix.folded = foldASCII(ix.text) })
	return ix.folded
}

func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// jsonlPart is what a worker found in a chunk of the text.
type jsonlPart struct {
	starts, ends, lines []uint32
	kinds               []byte
	keys                []string
	counts              map[string]int
	newlines            int
	values, invalid     int
}

// indexJSONL indexes text, using the cores there are.
func indexJSONL(text string) *jsonlIndex {
	// The chunks start at a line's start.
	var bounds []int
	for at := 0; at < len(text); {
		bounds = append(bounds, at)
		next := at + jsonlChunk
		if next >= len(text) {
			break
		}
		if nl := strings.IndexByte(text[next:], '\n'); nl >= 0 {
			next += nl + 1
		} else {
			break
		}
		at = next
	}
	bounds = append(bounds, len(text))
	parts := make([]jsonlPart, max(len(bounds)-1, 0))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(parts)) {
		wg.Go(func() {
			for k := range work {
				parts[k] = scanJSONL(text, bounds[k], bounds[k+1])
			}
		})
	}
	for k := range parts {
		work <- k
	}
	close(work)
	wg.Wait()

	ix := &jsonlIndex{text: text}
	n := 0
	for k := range parts {
		n += len(parts[k].starts)
	}
	ix.starts, ix.ends, ix.lines, ix.kinds = make([]uint32, 0, n), make([]uint32, 0, n), make([]uint32, 0, n), make([]byte, 0, n)
	seen := map[string]int{}
	line := uint32(0)
	for k := range parts {
		p := &parts[k]
		ix.starts = append(ix.starts, p.starts...)
		ix.ends = append(ix.ends, p.ends...)
		for _, l := range p.lines {
			ix.lines = append(ix.lines, l+line)
		}
		ix.kinds = append(ix.kinds, p.kinds...)
		line += uint32(p.newlines)
		ix.values += p.values
		ix.invalid += p.invalid
		for _, key := range p.keys {
			at, ok := seen[key]
			if !ok {
				at = len(ix.keys)
				seen[key] = at
				ix.keys = append(ix.keys, key)
				ix.counts = append(ix.counts, 0)
			}
			ix.counts[at] += p.counts[key]
		}
	}
	return ix
}

// scanJSONL indexes the lines of text from start to end.
func scanJSONL(text string, start, end int) jsonlPart {
	p := jsonlPart{counts: map[string]int{}}
	line := 0
	for at := start; at < end; line++ {
		stop := end
		if nl := strings.IndexByte(text[at:end], '\n'); nl >= 0 {
			stop = at + nl
		}
		s, e := at, stop
		for s < e && isJSONSpace(text[s]) {
			s++
		}
		for e > s && isJSONSpace(text[e-1]) {
			e--
		}
		if s < e {
			kind := jsonlKind(text[s:e])
			p.starts = append(p.starts, uint32(s))
			p.ends = append(p.ends, uint32(e))
			p.lines = append(p.lines, uint32(line))
			p.kinds = append(p.kinds, kind)
			switch kind {
			case jsonlObject:
				eachKey(text[s:e], func(key, _ string) bool {
					if _, ok := p.counts[key]; !ok {
						p.keys = append(p.keys, key)
					}
					p.counts[key]++
					return true
				})
			case jsonlValue:
				p.values++
			default:
				p.invalid++
			}
		}
		if stop == end {
			if stop < len(text) && text[stop] == '\n' {
				line++
			}
			break
		}
		at = stop + 1
	}
	p.newlines = line
	return p
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// jsonlKind says what a line holds.
func jsonlKind(line string) byte {
	if !json.Valid([]byte(line)) {
		return jsonlInvalid
	}
	if line[0] == '{' {
		return jsonlObject
	}
	return jsonlValue
}

// The scanner below reads JSON already known to be valid.

func skipSpace(s string, i int) int {
	for i < len(s) && isJSONSpace(s[i]) {
		i++
	}
	return i
}

// skipString returns the end of the string that starts at i.
func skipString(s string, i int) int {
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(s)
}

// skipValue returns the end of the value that starts at i.
func skipValue(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '"':
		return skipString(s, i)
	case '{', '[':
		depth := 0
		for ; i < len(s); i++ {
			switch s[i] {
			case '"':
				i = skipString(s, i) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
		return len(s)
	}
	for i < len(s) && !strings.ContainsRune(",}] \t\r\n", rune(s[i])) {
		i++
	}
	return i
}

// unquote returns the text of a JSON string.
func unquote(raw string) string {
	if len(raw) >= 2 && !strings.ContainsRune(raw, '\\') {
		return raw[1 : len(raw)-1]
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) != nil {
		return raw
	}
	return s
}

// eachKey calls fn with each key of the object obj and its value, until
// fn returns false.
func eachKey(obj string, fn func(key, value string) bool) {
	i := skipSpace(obj, 0)
	if i >= len(obj) || obj[i] != '{' {
		return
	}
	i++
	for {
		i = skipSpace(obj, i)
		if i >= len(obj) || obj[i] != '"' {
			return
		}
		ke := skipString(obj, i)
		key := unquote(obj[i:ke])
		i = skipSpace(obj, ke)
		if i >= len(obj) || obj[i] != ':' {
			return
		}
		i = skipSpace(obj, i+1)
		ve := skipValue(obj, i)
		if !fn(key, obj[i:ve]) {
			return
		}
		i = skipSpace(obj, ve)
		if i >= len(obj) || obj[i] != ',' {
			return
		}
		i++
	}
}

// eachItem calls fn with each item of the array arr, until fn returns
// false.
func eachItem(arr string, fn func(i int, value string) bool) {
	i := skipSpace(arr, 0)
	if i >= len(arr) || arr[i] != '[' {
		return
	}
	i++
	for n := 0; ; n++ {
		i = skipSpace(arr, i)
		if i >= len(arr) || arr[i] == ']' {
			return
		}
		ve := skipValue(arr, i)
		if !fn(n, arr[i:ve]) {
			return
		}
		i = skipSpace(arr, ve)
		if i >= len(arr) || arr[i] != ',' {
			return
		}
		i++
	}
}

// jsonlStep is a step of a column's path: a key of an object, or an item
// of an array when item.
type jsonlStep struct {
	key   string
	index int
	item  bool
}

// lookup returns the value at path in the JSON value v, and whether it
// has one. An empty path is v itself.
func lookup(v string, path []jsonlStep) (string, bool) {
	for _, st := range path {
		found := false
		if st.item {
			eachItem(v, func(i int, value string) bool {
				if i == st.index {
					v, found = value, true
				}
				return i < st.index
			})
		} else {
			eachKey(v, func(key, value string) bool {
				if key == st.key {
					v, found = value, true
				}
				return !found
			})
		}
		if !found {
			return "", false
		}
	}
	return v, true
}

// What a cell holds, as it colors it.
const (
	cellMissing byte = iota
	cellString
	cellNumber
	cellBool
	cellNull
	cellObject
	cellArray
	cellInvalid
)

// jsonlCell is what a cell shows: its text, on one line, and what it is.
type jsonlCell struct {
	text string
	kind byte
}

// cellOf returns what a cell of the value raw shows.
func cellOf(raw string) jsonlCell {
	switch raw[0] {
	case '"':
		return jsonlCell{cellLine(unquote(raw), jsonlCellMax), cellString}
	case '{':
		n := 0
		eachKey(raw, func(string, string) bool { n++; return true })
		return jsonlCell{"{" + strconv.Itoa(n) + "}", cellObject}
	case '[':
		n := 0
		eachItem(raw, func(int, string) bool { n++; return true })
		return jsonlCell{"[" + strconv.Itoa(n) + "]", cellArray}
	case 't', 'f':
		return jsonlCell{raw, cellBool}
	case 'n':
		return jsonlCell{raw, cellNull}
	}
	return jsonlCell{raw, cellNumber}
}

// cellLine is s on one line, its line breaks and tabs as spaces, cut to n
// runes with an ellipsis.
func cellLine(s string, n int) string {
	cut := len(s)
	if utf8.RuneCountInString(s) > n {
		cut = 0
		for k := 0; k < n; k++ {
			_, size := utf8.DecodeRuneInString(s[cut:])
			cut += size
		}
	}
	plain := cut == len(s)
	for i := 0; i < cut; i++ {
		if s[i] < ' ' {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	out := strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, s[:cut])
	if cut < len(s) {
		out += "…"
	}
	return out
}

// jsonlColumn is a column of the table: the value at path of each row's
// line, the whole line's for an empty path.
type jsonlColumn struct {
	path []jsonlStep
	id   string
	// hidden is set for a column the person hid, or one of the keys past
	// those the table shows of its own.
	hidden bool
	// expanded is set for an object's or an array's column shown as its
	// members' columns, which have it as their parent.
	expanded bool
	parent   string
}

// title is how the column's header reads: its path, the keys joined by
// dots and the items in brackets, or "value" for the whole line.
func (col *jsonlColumn) title() string {
	if len(col.path) == 0 {
		return "value"
	}
	var b strings.Builder
	for k, st := range col.path {
		switch {
		case st.item:
			b.WriteString("[" + strconv.Itoa(st.index) + "]")
		case k > 0:
			b.WriteString("." + st.key)
		default:
			b.WriteString(st.key)
		}
	}
	return b.String()
}

// pathID is the ID of a column of path, which no key spells otherwise.
func pathID(path []jsonlStep) string {
	if len(path) == 0 {
		return "\x00value"
	}
	var b strings.Builder
	for _, st := range path {
		if st.item {
			b.WriteString("\x01" + strconv.Itoa(st.index))
		} else {
			b.WriteString("\x02" + st.key)
		}
	}
	return b.String()
}

func newColumn(path []jsonlStep, parent string) *jsonlColumn {
	return &jsonlColumn{path: path, id: pathID(path), parent: parent}
}

// jsonlColumns are the columns of an index: the whole line's when some
// rows are not objects, then the keys of at least a tenth of the rows, as
// they first come, and the others hidden. Columns the table had stay as
// they were, hidden, moved or expanded.
func jsonlColumns(ix *jsonlIndex, had []*jsonlColumn) []*jsonlColumn {
	cols := slices.Clone(had)
	have := map[string]bool{}
	for _, col := range cols {
		have[col.id] = true
	}
	least := max(1, ix.rows()/10)
	add := func(path []jsonlStep, shown bool) {
		col := newColumn(path, "")
		if have[col.id] {
			return
		}
		have[col.id] = true
		col.hidden = !shown
		cols = append(cols, col)
	}
	if ix.values > 0 {
		add(nil, ix.values >= least || len(ix.keys) == 0)
	}
	shown := 0
	for _, col := range cols {
		if !col.hidden && !col.expanded {
			shown++
		}
	}
	for k, key := range ix.keys {
		on := ix.counts[k] >= least && shown < jsonlMaxColumns
		if on && !have[pathID([]jsonlStep{{key: key}})] {
			shown++
		}
		add([]jsonlStep{{key: key}}, on)
	}
	return cols
}

// members returns the columns of the members of the objects or arrays in
// column col of rows, in the order they first come: an array's first
// items up to jsonlMaxItems.
func members(ix *jsonlIndex, col *jsonlColumn, stop func() bool) []*jsonlColumn {
	var keys []string
	seen := map[string]bool{}
	items := 0
	for i := range ix.rows() {
		if i%4096 == 0 && stop() {
			return nil
		}
		if ix.kinds[i] == jsonlInvalid {
			continue
		}
		v, ok := lookup(ix.line(i), col.path)
		if !ok || v == "" {
			continue
		}
		switch v[0] {
		case '{':
			eachKey(v, func(key, _ string) bool {
				if !seen[key] && len(keys) < jsonlMaxColumns {
					seen[key] = true
					keys = append(keys, key)
				}
				return true
			})
		case '[':
			eachItem(v, func(n int, _ string) bool {
				items = max(items, n+1)
				return n+1 < jsonlMaxItems
			})
		}
	}
	var out []*jsonlColumn
	for _, key := range keys {
		out = append(out, newColumn(append(slices.Clone(col.path), jsonlStep{key: key}), col.id))
	}
	for n := range items {
		out = append(out, newColumn(append(slices.Clone(col.path), jsonlStep{index: n, item: true}), col.id))
	}
	return out
}

// jsonlMaxItems is how many items of an array expand into columns.
const jsonlMaxItems = 20

// jsonlSortKey is what a row sorts by in a column.
type jsonlSortKey struct {
	rank byte // numbers, strings, booleans, objects and arrays, null, none
	num  float64
	str  string
}

// Ranks past jsonlRankLast stay last whichever way the rows sort.
const (
	rankNumber byte = iota
	rankString
	rankBool
	rankJSON
	rankNull
	rankNone
)

func sortKeyOf(raw string, ok bool) jsonlSortKey {
	if !ok || raw == "" {
		return jsonlSortKey{rank: rankNone}
	}
	switch raw[0] {
	case '"':
		s := unquote(raw)
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && s != "" {
			return jsonlSortKey{rank: rankNumber, num: f, str: s}
		}
		return jsonlSortKey{rank: rankString, str: s}
	case 't':
		return jsonlSortKey{rank: rankBool, num: 1}
	case 'f':
		return jsonlSortKey{rank: rankBool}
	case 'n':
		return jsonlSortKey{rank: rankNull}
	case '{', '[':
		return jsonlSortKey{rank: rankJSON, str: raw}
	}
	f, _ := strconv.ParseFloat(raw, 64)
	return jsonlSortKey{rank: rankNumber, num: f, str: raw}
}

// sortKeys returns what each row sorts by in column col, nil once stop
// says to.
func sortKeys(ix *jsonlIndex, col *jsonlColumn, stop func() bool) []jsonlSortKey {
	keys := make([]jsonlSortKey, ix.rows())
	for i := range keys {
		if i%4096 == 0 && stop() {
			return nil
		}
		if ix.kinds[i] == jsonlInvalid {
			keys[i] = jsonlSortKey{rank: rankNone + 1}
			continue
		}
		keys[i] = sortKeyOf(lookup(ix.line(i), col.path))
	}
	return keys
}

// compareSortKeys orders a and b, descending when desc; what has no value
// comes last either way.
func compareSortKeys(a, b *jsonlSortKey, desc bool) int {
	if a.rank != b.rank {
		if a.rank >= rankNull || b.rank >= rankNull || !desc {
			return int(a.rank) - int(b.rank)
		}
		return int(b.rank) - int(a.rank)
	}
	c := 0
	switch a.rank {
	case rankNumber, rankBool:
		switch {
		case a.num < b.num:
			c = -1
		case a.num > b.num:
			c = 1
		}
	case rankString, rankJSON:
		c = naturalCompare(a.str, b.str)
	}
	if desc {
		return -c
	}
	return c
}

// naturalCompare orders strings as people read them: runs of digits by
// their numbers, as versions and addresses want, and letters without
// their case first.
func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if la, lb := lowerASCII(ca), lowerASCII(cb); la != lb {
			return int(la) - int(lb)
		}
		i++
		j++
	}
	if c := (len(a) - i) - (len(b) - j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// filterRows returns the rows whose lines hold query, without its case,
// nil once stop says to.
func filterRows(ix *jsonlIndex, query string, stop func() bool) []int32 {
	text, q := ix.fold(), foldASCII(query)
	out := []int32{}
	for at, n := 0, 0; at < len(text); n++ {
		if n%256 == 0 && stop() {
			return nil
		}
		k := strings.Index(text[at:], q)
		if k < 0 {
			break
		}
		hit := at + k
		row, _ := slices.BinarySearch(ix.ends, uint32(hit+len(q)))
		if row >= ix.rows() {
			break
		}
		if uint32(hit) >= ix.starts[row] {
			out = append(out, int32(row))
		}
		at = int(ix.ends[row]) + 1
	}
	return out
}

// jsonlView is the rows of an index the table shows, in its order: those
// filter keeps, sorted by keys, all of them in the file's order for nil.
func jsonlView(ix *jsonlIndex, filter []int32, filtered bool, keys []jsonlSortKey, desc bool) []int32 {
	if !filtered && keys == nil {
		return nil
	}
	var rows []int32
	if filtered {
		rows = slices.Clone(filter)
	} else {
		rows = make([]int32, ix.rows())
		for i := range rows {
			rows[i] = int32(i)
		}
	}
	if keys != nil {
		slices.SortStableFunc(rows, func(a, b int32) int { return compareSortKeys(&keys[a], &keys[b], desc) })
	}
	return rows
}

// prettyJSON returns line indented, its numbers as it spells them, or ""
// when it is not JSON.
func prettyJSON(line string) string {
	var b bytes.Buffer
	b.Grow(len(line) * 2)
	if json.Indent(&b, []byte(line), "", "  ") != nil {
		return ""
	}
	return b.String()
}

// jsonlError says why a line is not JSON.
func jsonlError(line string) string {
	var v any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		return err.Error()
	}
	return ""
}

// lookupAt returns where the value at path is in the JSON value s, from
// its key for a member of an object, and whether it has one. An empty
// path is none.
func lookupAt(s string, path []jsonlStep) (from, to int, ok bool) {
	at := skipSpace(s, 0)
	for _, st := range path {
		found := false
		i := at
		switch {
		case i < len(s) && s[i] == '{' && !st.item:
			for i++; ; i++ {
				if i = skipSpace(s, i); i >= len(s) || s[i] != '"' {
					break
				}
				k, ke := i, skipString(s, i)
				if i = skipSpace(s, ke); i >= len(s) || s[i] != ':' {
					break
				}
				i = skipSpace(s, i+1)
				ve := skipValue(s, i)
				if unquote(s[k:ke]) == st.key {
					from, to, at, found = k, ve, i, true
					break
				}
				if i = skipSpace(s, ve); i >= len(s) || s[i] != ',' {
					break
				}
			}
		case i < len(s) && s[i] == '[' && st.item:
			for n := 0; ; n++ {
				if i = skipSpace(s, i+1); i >= len(s) || s[i] == ']' {
					break
				}
				ve := skipValue(s, i)
				if n == st.index {
					from, to, at, found = i, ve, i, true
					break
				}
				if i = skipSpace(s, ve); i >= len(s) || s[i] != ',' {
					break
				}
			}
		}
		if !found {
			return 0, 0, false
		}
	}
	return from, to, len(path) > 0
}

// foldHits returns where the folded query q is in s, ignoring the case of
// English letters, at most max of them.
func foldHits(s, q string, most int) [][2]int {
	var out [][2]int
	for i := 0; len(q) > 0 && i+len(q) <= len(s) && len(out) < most; {
		k := 0
		for k < len(q) {
			c := s[i+k]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != q[k] {
				break
			}
			k++
		}
		if k == len(q) {
			out = append(out, [2]int{i, i + len(q)})
			i += len(q)
		} else {
			i++
		}
	}
	return out
}
