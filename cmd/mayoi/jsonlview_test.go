package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
	"mayoi/locales"
)

const testJSONL = `{"id":1,"name":"ann","age":31,"user":{"plan":"pro"}}
{"id":2,"name":"bob","age":27,"user":{"plan":"free"}}

{"id":3,"name":"cat","age":45,"user":{"plan":"team"}}
{oops
`

// openJSONL opens a file of JSON Lines of text as a table, read, and
// returns its editor.
func openJSONL(t *testing.T, hs *harness, name, text string) *editor {
	t.Helper()
	os.WriteFile(hs.path(name), []byte(text), 0o644)
	hs.a.openFile(hs.path(name), true)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.preview == nil || d.preview.jsonl == nil {
		t.Fatalf("%s did not open as a table", name)
	}
	hs.until("the rows", func() bool { j := d.preview.jsonl; return j.ix != nil && !j.ix.partial && !j.reading })
	return d
}

func TestJSONLOpensAsTable(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	d := openJSONL(t, hs, "people.jsonl", testJSONL)
	j := d.preview.jsonl
	for _, want := range []string{"4 rows", "name", "ann", "cat", "{1}", "1 line isn't JSON", "Row Details"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if j.count() != 4 || j.ix.lines[2] != 3 {
		t.Fatalf("%d rows, the third on line %d", j.count(), j.ix.lines[2])
	}
	// The first row is chosen, and its details show it whole; the keys
	// move the choice.
	if j.row != 0 || !tt.HasText("Line 1") {
		t.Errorf("row %d chosen, texts %q", j.row, tt.Texts())
	}
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if j.row != 1 || !tt.HasText("Line 2") {
		t.Errorf("Down chose row %d, texts %q", j.row, tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	hs.settle()
	if j.details || tt.HasText("Line 2") {
		t.Error("Space left the details")
	}
	tt.Key(0, ui.KeySpace)
	hs.settle()

	// The filter keeps the rows that hold its query.
	a.run("edit.find")
	hs.settle()
	tt.Type("BOB")
	hs.until("the filter", func() bool { return j.shown.query == "BOB" })
	if j.count() != 1 || j.ix.line(int(j.rowAt(0)))[:7] != `{"id":2` || !tt.HasText("1 of 4 rows") {
		t.Fatalf("filtered to %d rows, texts %q", j.count(), tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	hs.until("no filter", func() bool { return j.shown.query == "" })

	// Sorting by a column of numbers sorts them as numbers, the line that
	// is not JSON last.
	j.sort = ui.SortOrder{Column: pathID([]jsonlStep{{key: "age"}}), Descending: true}
	hs.until("the sort", func() bool { return j.shown.sort != "" && !j.reading })
	var ids []string
	for i := range j.count() {
		line := j.ix.line(int(j.rowAt(i)))
		ids = append(ids, line[:min(len(line), 7)])
	}
	if strings.Join(ids, " ") != `{"id":3 {"id":1 {"id":2 {oops` {
		t.Errorf("sorted %v", ids)
	}

	// The chosen row's line is where the source opens.
	j.row = 0
	a.run("view.togglePreview")
	hs.settle()
	if d.preview != nil {
		t.Fatal("the table stays")
	}
	if line := d.buf.Snapshot().LineAt(d.sel[1]); line != 3 {
		t.Errorf("the source opens at line %d, want 3", line)
	}
	// And the table comes back at the caret's row.
	a.run("view.togglePreview")
	hs.until("the table again", func() bool { j := d.preview.jsonl; return j != nil && j.ix != nil && !j.reading })
	if j := d.preview.jsonl; j.rowAt(j.row) != 2 {
		t.Errorf("the table opens at row %d, want 2", j.rowAt(j.row))
	}
}

func TestJSONLInvalidLines(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "bad.ndjson", testJSONL)
	if err := tt.Click("1 line isn't JSON"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	j := d.preview.jsonl
	if j.rowAt(j.row) != 3 || !tt.HasText("Line 5") {
		t.Fatalf("row %d chosen, texts %q", j.row, tt.Texts())
	}
	found := false
	for _, s := range tt.Texts() {
		found = found || strings.HasPrefix(s, "Not JSON:")
	}
	if !found {
		t.Errorf("the details do not say why: %q", tt.Texts())
	}
}

func TestJSONLExpandsColumns(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "nested.jsonl", testJSONL)
	j := d.preview.jsonl
	a := hs.a
	a.expandJSONL(d.preview, j.column(pathID([]jsonlStep{{key: "user"}})))
	hs.until("the members", func() bool { return j.column(pathID([]jsonlStep{{key: "user"}, {key: "plan"}})) != nil })
	hs.settle()
	if !tt.HasText("user.plan") || !tt.HasText("team") || tt.HasText("{1}") {
		t.Errorf("expanded: %q", tt.Texts())
	}
	j.collapse(j.column(pathID([]jsonlStep{{key: "user"}})))
	hs.settle()
	if tt.HasText("user.plan") || !tt.HasText("{1}") {
		t.Errorf("collapsed: %q", tt.Texts())
	}
}

func TestJSONLCopies(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "copy.jsonl", testJSONL)
	j := d.preview.jsonl
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "name"}})
	hs.a.jsonlInput(d.preview, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	if got := tt.Clipboard(); got != "ann" {
		t.Errorf("copied %q, want the cell's ann", got)
	}
	j.list.Cells.Cursor = "#"
	hs.a.jsonlInput(d.preview, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	if got := tt.Clipboard(); !strings.HasPrefix(got, `{"id":1`) {
		t.Errorf("copied %q, want the line", got)
	}
}

// The details name the field of the cursor's column, and the Copy button
// says it copied, then goes back.
func TestJSONLDetailsField(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "field.jsonl", testJSONL)
	j := d.preview.jsonl
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "age"}})
	hs.settle()
	if !tt.HasText("age") || !strings.Contains(j.prettyOf, ":"+j.list.Cells.Cursor) {
		t.Errorf("the details don't name age: %q, of %q", tt.Texts(), j.prettyOf)
	}
	if err := tt.Click("Copy Line"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := tt.Clipboard(); !strings.HasPrefix(got, `{"id":1`) {
		t.Errorf("copied %q, want the line", got)
	}
	if err := tt.Click("Copied the line"); err != nil {
		t.Errorf("the Copy button doesn't say it copied: %v", err)
	}
	tt.Advance(jsonlCopiedFor)
	hs.settle()
	if err := tt.Click("Copy Line"); err != nil {
		t.Errorf("the Copy button didn't go back: %v", err)
	}
}

func TestJSONLOpensAsSource(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.JSONLMode = "source"
	os.WriteFile(hs.path("src.jsonl"), []byte(testJSONL), 0o644)
	hs.a.openFile(hs.path("src.jsonl"), true)
	hs.settle()
	d := hs.a.activeEditor()
	if d.preview != nil || !hs.tt.HasText("Table (⇧⌘V)") {
		t.Fatalf("opened as a table, or without its mode: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click("Table (⇧⌘V)"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if d.preview == nil || d.preview.jsonl == nil {
		t.Fatal("the mode did not show the table")
	}
}

// A file larger than the first frame reads shows its head at once and
// its rows from the background.
func TestJSONLReadsLargeFiles(t *testing.T) {
	hs := newHarness(t, true)
	text := jsonlText(20_000)
	d := openJSONL(t, hs, "big.jsonl", text)
	if n := d.preview.jsonl.ix.rows(); n != 20_000 {
		t.Fatalf("%d rows", n)
	}
	if !hs.tt.HasText("20,000 rows") {
		t.Errorf("texts %q", hs.tt.Texts())
	}
}

// tallJSONL is lines of JSON with many keys each, whose details run past
// their pane.
func tallJSONL(rows, keys int) string {
	var b strings.Builder
	for r := range rows {
		b.WriteString("{")
		for k := range keys {
			if k > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\"k%02d\":%d", k, r*keys+k)
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// The details scroll to the chosen field, at once as the keys move along
// the rows, gliding to another field of the row, and show another row from
// its top when no field is chosen.
func TestJSONLDetailsFollowField(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "tall.jsonl", tallJSONL(3, 80))
	j := d.preview.jsonl
	hs.settle()
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "k70"}})
	hs.settle()
	far := j.prettyScroll.Y
	if far <= 0 || j.markAt < 0 {
		t.Fatalf("the details didn't scroll to k70: at %v, mark %d", far, j.markAt)
	}
	// Another field of the row glides there.
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "k05"}})
	hs.settle()
	if !j.prettyScroll.Scrolling() && j.prettyScroll.Y == far {
		t.Error("the details stayed at k70")
	}
	tt.Advance(motionMove)
	hs.settle()
	near := j.prettyScroll.Y
	if near >= far {
		t.Errorf("the details are at %v for k05, %v for k70", near, far)
	}
	// The keys moving to the next row show its field at once.
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "k70"}})
	hs.settle()
	tt.Advance(motionMove)
	hs.settle()
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if j.row != 1 || j.prettyScroll.Scrolling() || j.prettyScroll.Y <= near {
		t.Errorf("row %d, details at %v gliding %v", j.row, j.prettyScroll.Y, j.prettyScroll.Scrolling())
	}
	// With no field, another row shows from its top.
	j.list.Cells.Cursor = "#"
	hs.settle()
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if j.row != 2 || j.prettyScroll.Y != 0 {
		t.Errorf("row %d, details at %v", j.row, j.prettyScroll.Y)
	}
}

// ⌘C washes the cell it copied over for a moment, and copying the line
// turns the details' Copy button to a check.
func TestJSONLCopyWashes(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "wash.jsonl", testJSONL)
	j := d.preview.jsonl
	j.list.Cells.Cursor = pathID([]jsonlStep{{key: "name"}})
	hs.a.jsonlInput(d.preview, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	hs.settle()
	if j.wash.at.IsZero() || j.wash.id != j.list.Cells.Cursor || j.wash.row != 0 {
		t.Errorf("no wash of the cell: %+v", j.wash)
	}
	if tt.HasText("Copied the line") {
		t.Error("copying a value says it copied the line")
	}
	j.list.Cells.Cursor = "#"
	hs.a.jsonlInput(d.preview, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	hs.settle()
	if j.wash.id != "" || j.wash.at.IsZero() {
		t.Errorf("no wash of the row: %+v", j.wash)
	}
	if err := tt.Click("Copied the line"); err != nil {
		t.Errorf("⌘C of the line doesn't show on the Copy button: %v", err)
	}
	tt.Advance(jsonlCopiedFor)
	hs.settle()
	if err := tt.Click("Copy Line"); err != nil {
		t.Errorf("the Copy button didn't go back: %v", err)
	}
}

// A cell's menu filters the rows to its value, or hides them; the chips
// in the bar say so and remove a filter, and with no row left, Clear
// Filters shows them all again.
func TestJSONLFiltersByValue(t *testing.T) {
	hs := newHarness(t, true)
	tt := hs.tt
	d := openJSONL(t, hs, "where.jsonl", testJSONL)
	j := d.preview.jsonl
	r, ok := tt.Find("bob")
	if !ok {
		t.Fatalf("no bob in %q", tt.Texts())
	}
	tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	hs.settle()
	if err := tt.ChooseMenuItem("Hide Rows with This Value"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	hs.until("the rows without bob", func() bool { return j.shown.where != "" && !j.reading })
	if j.count() != 3 || j.rowAt(1) != 2 || !tt.HasText("≠") || !tt.HasText("3 of 4 rows") {
		t.Fatalf("%d rows, texts %q", j.count(), tt.Texts())
	}
	// Keeping a value of the same column takes the hiding's place.
	j.addWhere(j.column(pathID([]jsonlStep{{key: "name"}})), `"cat"`, false)
	j.addWhere(j.column(pathID([]jsonlStep{{key: "age"}})), `31`, false)
	hs.until("the rows of cat", func() bool { return j.shown.where == whereKey(j.where) && !j.reading })
	if len(j.where) != 2 || j.count() != 0 || !tt.HasText("No rows match the filters") {
		t.Fatalf("%d filters, %d rows, texts %q", len(j.where), j.count(), tt.Texts())
	}
	if err := tt.Click("Remove Filter"); err != nil {
		t.Fatal(err)
	}
	hs.until("one filter", func() bool { return len(j.where) == 1 && j.shown.where == whereKey(j.where) && !j.reading })
	if j.count() != 1 || j.where[0].text != "31" || j.ix.line(int(j.rowAt(0)))[:7] != `{"id":1` {
		t.Errorf("%d rows, filters %v", j.count(), j.where)
	}
	j.addWhere(j.column(pathID([]jsonlStep{{key: "age"}})), `99`, false)
	hs.until("no rows", func() bool { return j.shown.where == whereKey(j.where) && !j.reading })
	if err := tt.Click("Clear Filters"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	hs.until("every row", func() bool { return j.shown.where == "" && !j.reading })
	if j.count() != 4 {
		t.Errorf("%d rows after clearing", j.count())
	}
}

// jsonlLongRecord is the size of the long record of jsonlLongText, as long
// as one a transcript was found to hold.
const jsonlLongRecord = 1_351_246

// jsonlLongText is n rows of JSON Lines, the row at line at, from 1, of
// at least jsonlLongRecord bytes and the others short.
func jsonlLongText(n, at int) string {
	var b strings.Builder
	for i := range n {
		if i+1 == at {
			fmt.Fprintf(&b, `{"id":%d,"kind":"long","blob":"%s"}`+"\n", i, strings.Repeat("ab", jsonlLongRecord/2))
			continue
		}
		fmt.Fprintf(&b, `{"id":%d,"kind":"short","text":"row %d"}`+"\n", i, i)
	}
	return b.String()
}

// A file of JSON Lines with a record longer than the editor shows opens as
// its table, the record a row of it; its source says which line is too
// long, and Show Table goes back. Other files still refuse such a line.
func TestJSONLLongRecordOpens(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	text := jsonlLongText(2000, 1122)
	d := openJSONL(t, hs, "long.jsonl", text)
	j := d.preview.jsonl
	if j.count() != 2000 || d.longLine != 1122 || d.buf.Len() != len(text) {
		t.Fatalf("%d rows, long line %d, %d of %d bytes", j.count(), d.longLine, d.buf.Len(), len(text))
	}
	j.row = 1121
	j.chosen.Clear()
	j.chosen.Add(1121)
	j.list.ScrollTo(1121, ui.Start)
	hs.settle()
	if !tt.HasText("Line 1,122") || len(j.prettyText) > jsonlPrettyMax+64 {
		t.Fatalf("the long row's details: %d bytes, texts %q", len(j.prettyText), tt.Texts())
	}
	// Moving through the rows past it stays quick.
	start := time.Now()
	for i := range 20 {
		key := ui.KeyDown
		if i%2 == 1 {
			key = ui.KeyUp
		}
		tt.Key(0, key)
		tt.Frame()
	}
	if per := time.Since(start) / 20; per > 100*time.Millisecond {
		t.Errorf("%v a frame about the long row", per)
	}

	a.run("view.togglePreview")
	hs.settle()
	msg := locales.JsonlLongSource(1122, "long.jsonl")
	if d.preview != nil || !tt.HasText(msg) || d.areaCol != nil {
		t.Fatalf("the source: preview %v, laid out %v, texts %q", d.preview != nil, d.areaCol != nil, tt.Texts())
	}
	if err := tt.Click("Show Table"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if d.preview == nil || d.preview.jsonl == nil {
		t.Fatal("Show Table left the source")
	}

	// Opened as source by the setting, it says so too.
	a.settings.JSONLMode = "source"
	os.WriteFile(hs.path("src.ndjson"), []byte(text), 0o644)
	a.openFile(hs.path("src.ndjson"), true)
	hs.settle()
	if e := a.activeEditor(); e.preview != nil || e.longLine != 1122 || !tt.HasText(locales.JsonlLongSource(1122, "src.ndjson")) {
		t.Fatalf("as source: %q", tt.Texts())
	}

	// A file of plain JSON keeps the editor's guard.
	os.WriteFile(hs.path("one.json"), []byte(text), 0o644)
	a.openFile(hs.path("one.json"), true)
	hs.settle()
	if e := a.activeEditor(); e.sync != syncLoadFailed || e.loadErr != errLongLine || e.buf.Len() != 0 {
		t.Fatalf("one.json: %v, %d bytes", e.loadErr, e.buf.Len())
	}
}

// A restored tab of JSON Lines with a long record shows its table, or, as
// source, which line is too long, without laying its text out.
func TestRestoreJSONLLongRecord(t *testing.T) {
	root := workspaceFixture(t)
	long := filepath.Join(root, "long.jsonl")
	os.WriteFile(long, []byte(jsonlLongText(50, 7)), 0o644)
	for _, preview := range []bool{true, false} {
		s := settings.Default()
		w := s.NewWindow()
		w.Folder = root
		w.Columns = []settings.Column{{Tabs: []settings.Tab{{File: long, Preview: preview}}, Active: 0, Weight: 1}}
		a := newApp(&fakeHost{folder: root}, s, w)
		tt := ui.NewTester(a.view, 1200, 760)
		hs := &harness{t: t, a: a, tt: tt}
		hs.until("the read", func() bool { return a.cols[0].tabs[0].ed.sync != syncLoading })
		hs.settle()
		d := a.cols[0].tabs[0].ed
		switch {
		case d.longLine != 7:
			t.Errorf("preview %v: long line %d, %v", preview, d.longLine, d.loadErr)
		case preview && (d.preview == nil || !tt.HasText("50 rows")):
			t.Errorf("restored table: %q", tt.Texts())
		case !preview && (d.preview != nil || !tt.HasText(locales.JsonlLongSource(7, "long.jsonl")) || d.areaCol != nil):
			t.Errorf("restored source: %q", tt.Texts())
		}
		a.shutdown()
	}
}
