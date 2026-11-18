package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestJSONLIndex(t *testing.T) {
	text := "{\"id\":1,\"name\":\"ann\"}\r\n\n  {\"id\":2,\"user.name\":\"x\",\"tags\":[1,2]}  \n{oops\n42\n{\"id\":3}"
	ix := indexJSONL(text)
	if ix.rows() != 5 {
		t.Fatalf("%d rows, want 5", ix.rows())
	}
	if got := ix.line(0); got != `{"id":1,"name":"ann"}` {
		t.Errorf("row 0 is %q, without its CR", got)
	}
	if got := ix.lines; !slices.Equal(got, []uint32{0, 2, 3, 4, 5}) {
		t.Errorf("lines %v, the blank line skipped", got)
	}
	if !slices.Equal(ix.kinds, []byte{jsonlObject, jsonlObject, jsonlInvalid, jsonlValue, jsonlObject}) {
		t.Errorf("kinds %v", ix.kinds)
	}
	if !slices.Equal(ix.keys, []string{"id", "name", "user.name", "tags"}) || !slices.Equal(ix.counts, []int{3, 1, 1, 1}) {
		t.Errorf("keys %v counts %v", ix.keys, ix.counts)
	}
	if ix.invalid != 1 || ix.values != 1 {
		t.Errorf("invalid %d values %d", ix.invalid, ix.values)
	}
	if r := ix.rowOfLine(1); r != 1 {
		t.Errorf("the blank line 1 goes to row %d, want the next, 1", r)
	}
}

// The chunks the workers take join up: rows, lines and keys as one would
// find them.
func TestJSONLIndexChunks(t *testing.T) {
	var b strings.Builder
	for i := range 60000 {
		fmt.Fprintf(&b, "{\"i\":%d,\"k%d\":true}\n", i, i%3)
		if i%1000 == 0 {
			b.WriteString("\n")
		}
	}
	ix := indexJSONL(b.String())
	if ix.rows() != 60000 {
		t.Fatalf("%d rows", ix.rows())
	}
	for _, i := range []int{0, 1, 999, 1000, 1001, 30000, 59999} {
		want := fmt.Sprintf("{\"i\":%d,", i)
		if !strings.HasPrefix(ix.line(i), want) {
			t.Errorf("row %d is %q", i, ix.line(i))
		}
		if got := ix.text[:ix.starts[i]]; strings.Count(got, "\n") != int(ix.lines[i]) {
			t.Errorf("row %d says line %d, is %d", i, ix.lines[i], strings.Count(got, "\n"))
		}
	}
	if !slices.Equal(ix.keys, []string{"i", "k0", "k1", "k2"}) || ix.counts[0] != 60000 || ix.counts[1] != 20000 {
		t.Errorf("keys %v counts %v", ix.keys, ix.counts)
	}
}

func TestJSONLCells(t *testing.T) {
	line := `{"a":{"b":[10,{"c":"deep"}]},"s":"x\ny","n":1.50,"big":12345678901234567890,"t":true,"z":null,"o":{},"arr":[1,2,3]}`
	for _, tc := range []struct {
		path []jsonlStep
		want jsonlCell
	}{
		{[]jsonlStep{{key: "s"}}, jsonlCell{"x y", cellString}},
		{[]jsonlStep{{key: "n"}}, jsonlCell{"1.50", cellNumber}},
		{[]jsonlStep{{key: "big"}}, jsonlCell{"12345678901234567890", cellNumber}},
		{[]jsonlStep{{key: "t"}}, jsonlCell{"true", cellBool}},
		{[]jsonlStep{{key: "z"}}, jsonlCell{"null", cellNull}},
		{[]jsonlStep{{key: "o"}}, jsonlCell{"{0}", cellObject}},
		{[]jsonlStep{{key: "arr"}}, jsonlCell{"[3]", cellArray}},
		{[]jsonlStep{{key: "a"}, {key: "b"}, {index: 1, item: true}, {key: "c"}}, jsonlCell{"deep", cellString}},
	} {
		raw, ok := lookup(line, tc.path)
		if !ok {
			t.Errorf("%s: no value", newColumn(tc.path, "").title())
			continue
		}
		if got := cellOf(raw); got != tc.want {
			t.Errorf("%s: %+v, want %+v", newColumn(tc.path, "").title(), got, tc.want)
		}
	}
	if _, ok := lookup(line, []jsonlStep{{key: "missing"}}); ok {
		t.Error("a missing key has a value")
	}
	long := `"` + strings.Repeat("é", 1000) + `"`
	if got := cellOf(long).text; !strings.HasSuffix(got, "…") || len([]rune(got)) != jsonlCellMax+1 {
		t.Errorf("a long string shows %d runes", len([]rune(got)))
	}
}

func TestJSONLColumns(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "{\"id\":%d,\"user\":{\"name\":\"u%d\",\"age\":%d}", i, i, i)
		if i < 5 {
			b.WriteString(`,"rare":1`)
		}
		if i < 20 {
			b.WriteString(`,"tags":["a","b"]`)
		}
		b.WriteString("}\n")
	}
	b.WriteString("7\n")
	ix := indexJSONL(b.String())
	cols := jsonlColumns(ix, nil)
	var shown, hidden []string
	for _, col := range cols {
		if col.hidden {
			hidden = append(hidden, col.title())
		} else {
			shown = append(shown, col.title())
		}
	}
	if !slices.Equal(shown, []string{"id", "user", "tags"}) || !slices.Equal(hidden, []string{"value", "rare"}) {
		t.Errorf("shown %v hidden %v", shown, hidden)
	}
	// The person's choices stay as the file changes.
	byTitle := func(title string) *jsonlColumn {
		i := slices.IndexFunc(cols, func(col *jsonlColumn) bool { return col.title() == title })
		return cols[i]
	}
	byTitle("id").hidden = true
	again := jsonlColumns(ix, cols)
	if len(again) != len(cols) || !again[slices.Index(again, byTitle("id"))].hidden {
		t.Errorf("columns again: %d, id hidden %v", len(again), byTitle("id").hidden)
	}
	user := byTitle("user")
	var titles []string
	for _, m := range members(ix, user, func() bool { return false }) {
		titles = append(titles, m.title())
		if m.parent != user.id {
			t.Errorf("%s has parent %q", m.title(), m.parent)
		}
	}
	if !slices.Equal(titles, []string{"user.name", "user.age"}) {
		t.Errorf("members of user: %v", titles)
	}
	titles = nil
	for _, m := range members(ix, byTitle("tags"), func() bool { return false }) {
		titles = append(titles, m.title())
	}
	if !slices.Equal(titles, []string{"tags[0]", "tags[1]"}) {
		t.Errorf("members of tags: %v", titles)
	}
}

func TestJSONLSort(t *testing.T) {
	text := strings.Join([]string{
		`{"v":"v1.10"}`, `{"v":"v1.9"}`, `{"v":null}`, `{}`, `{"v":10}`, `{"v":"2"}`, `bad`, `{"v":true}`,
	}, "\n")
	ix := indexJSONL(text)
	col := newColumn([]jsonlStep{{key: "v"}}, "")
	keys := sortKeys(ix, col, func() bool { return false })
	order := func(desc bool) []string {
		var out []string
		for _, r := range jsonlView(ix, nil, false, keys, desc) {
			out = append(out, ix.line(int(r)))
		}
		return out
	}
	want := []string{`{"v":"2"}`, `{"v":10}`, `{"v":"v1.9"}`, `{"v":"v1.10"}`, `{"v":true}`, `{"v":null}`, `{}`, `bad`}
	if got := order(false); !slices.Equal(got, want) {
		t.Errorf("ascending %v", got)
	}
	wantDesc := []string{`{"v":true}`, `{"v":"v1.10"}`, `{"v":"v1.9"}`, `{"v":10}`, `{"v":"2"}`, `{"v":null}`, `{}`, `bad`}
	if got := order(true); !slices.Equal(got, wantDesc) {
		t.Errorf("descending %v", got)
	}
}

func TestJSONLFilter(t *testing.T) {
	ix := indexJSONL("{\"a\":\"Hello\"}\n\n{\"a\":\"world\"}\n{\"b\":\"HELLO again\"}\n")
	got := filterRows(ix, "hello", func() bool { return false })
	if !slices.Equal(got, []int32{0, 2}) {
		t.Errorf("rows %v, want 0 and 2", got)
	}
	if got := filterRows(ix, "nothing", func() bool { return false }); len(got) != 0 {
		t.Errorf("rows %v for no match", got)
	}
	view := jsonlView(ix, []int32{2, 0}, true, nil, false)
	if !slices.Equal(view, []int32{2, 0}) {
		t.Errorf("view %v", view)
	}
}

func TestPrettyJSON(t *testing.T) {
	if got := prettyJSON(`{"n":1.50,"a":[1]}`); got != "{\n  \"n\": 1.50,\n  \"a\": [\n    1\n  ]\n}" {
		t.Errorf("pretty %q", got)
	}
	if prettyJSON("{nope") != "" || jsonlError("{nope") == "" {
		t.Error("invalid JSON is pretty")
	}
}

// jsonlText is a file of n lines of JSON Lines, of some 100 bytes each,
// as logs and datasets have.
func jsonlText(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "{\"id\":%d,\"status\":\"%s\",\"user\":{\"name\":\"user %d\",\"age\":%d},\"tokens\":%d,\"ok\":%v}\n",
			i, []string{"active", "trial", "churned"}[i%3], i, 20+i%50, i*37%100000, i%2 == 0)
	}
	return b.String()
}

func BenchmarkJSONLIndex(b *testing.B) {
	for _, n := range []int{10_000, 100_000, 600_000} {
		text := jsonlText(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.SetBytes(int64(len(text)))
			for b.Loop() {
				indexJSONL(text)
			}
		})
	}
}

func BenchmarkJSONLSort(b *testing.B) {
	ix := indexJSONL(jsonlText(100_000))
	col := newColumn([]jsonlStep{{key: "tokens"}}, "")
	for b.Loop() {
		keys := sortKeys(ix, col, func() bool { return false })
		jsonlView(ix, nil, false, keys, false)
	}
}

func BenchmarkJSONLFilter(b *testing.B) {
	ix := indexJSONL(jsonlText(100_000))
	ix.fold()
	for b.Loop() {
		filterRows(ix, "user 99", func() bool { return false })
	}
}

func TestJSONLLookupAt(t *testing.T) {
	text := prettyJSON(`{"id":1,"user":{"name":"ann","tags":["a","b"]},"x":2}`)
	for _, tc := range []struct {
		path []jsonlStep
		want string
	}{
		{[]jsonlStep{{key: "id"}}, `"id": 1`},
		{[]jsonlStep{{key: "user"}, {key: "name"}}, `"name": "ann"`},
		{[]jsonlStep{{key: "user"}, {key: "tags"}, {index: 1, item: true}}, `"b"`},
		{[]jsonlStep{{key: "x"}}, `"x": 2`},
	} {
		from, to, ok := lookupAt(text, tc.path)
		if !ok || text[from:to] != tc.want {
			t.Errorf("lookupAt %v = %q, %v; want %q", tc.path, text[from:to], ok, tc.want)
		}
	}
	for _, path := range [][]jsonlStep{nil, {{key: "nope"}}, {{key: "id"}, {key: "deeper"}}, {{index: 0, item: true}}} {
		if _, _, ok := lookupAt(text, path); ok {
			t.Errorf("lookupAt %v found a member", path)
		}
	}
}

func TestJSONLFoldHits(t *testing.T) {
	got := foldHits("Anna and ANN", "an", 10)
	want := [][2]int{{0, 2}, {5, 7}, {9, 11}}
	if !slices.Equal(got, want) {
		t.Errorf("foldHits = %v, want %v", got, want)
	}
	if got := foldHits("aaaa", "a", 2); len(got) != 2 {
		t.Errorf("foldHits kept %d hits past its most", len(got))
	}
	if foldHits("abc", "", 10) != nil {
		t.Error("an empty query has hits")
	}
}

func TestJSONLMarkSpans(t *testing.T) {
	bg := ui.Hex("#ff0000")
	spans := []ui.Span{{Text: "abc"}, {Text: "def"}}
	got := markSpans(spans, 2, 4, bg)
	var text []string
	for _, sp := range got {
		mark := ""
		if sp.Background == bg {
			mark = "*"
		}
		text = append(text, mark+sp.Text)
	}
	if want := []string{"ab", "*c", "*d", "ef"}; !slices.Equal(text, want) {
		t.Errorf("markSpans = %v, want %v", text, want)
	}
}
