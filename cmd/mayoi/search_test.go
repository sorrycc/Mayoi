package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// settleSearch draws frames until the search under way, or waiting to
// start, is done.
func (hs *harness) settleSearch() {
	s := &hs.a.inFiles
	deadline := time.Now().Add(5 * time.Second)
	for {
		hs.settle()
		if !s.running && !s.waiting || time.Now().After(deadline) {
			break
		}
	}
	hs.settle()
}

// searchFor shows search and types a query into it.
func (hs *harness) searchFor(query string) {
	hs.a.run("edit.findInFiles")
	hs.settle()
	hs.a.inFiles.query = ""
	hs.tt.Type(query)
	hs.settleSearch()
}

func resultFiles(s *searchState) []string {
	var out []string
	for _, f := range s.files {
		out = append(out, f.rel)
	}
	return out
}

func TestSearchInFiles(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	hs.searchFor("main")
	s := &a.inFiles
	if a.window.SidebarView != "search" || !tt.HasText("SEARCH") {
		t.Fatalf("sidebar shows %q", a.window.SidebarView)
	}
	if got := strings.Join(resultFiles(s), " "); got != "main.go" || s.count != 3 {
		t.Fatalf("results %q, %d matches; want main.go, 3", got, s.count)
	}
	if !tt.HasText("3 results in 1 file") {
		t.Errorf("summary missing: %q", tt.Texts())
	}
	hs.shot("search")

	// Choosing a match shows it, selected.
	m := s.files[0].matches[2]
	if m.line != 5 || m.col != 5 || m.hit != "main" || m.pre != "func " {
		t.Errorf("third match %+v", m)
	}
	a.openMatch(s.files[0], m, true)
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.path != hs.path("main.go") {
		t.Fatalf("opened %v", d)
	}
	if got := d.buf.Slice(d.sel[0], d.sel[1]); got != "main" || d.buf.Snapshot().LineAt(d.sel[0]) != 5 {
		t.Errorf("selected %q on line %d", got, d.buf.Snapshot().LineAt(d.sel[0]))
	}

	// Options.
	s.matchCase, s.query = true, "Mayoi"
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "README.md web/index.html" {
		t.Errorf("Mayoi with case: %q", got)
	}
	s.query = "mayoi"
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "main.go" {
		t.Errorf("mayoi with case: %q", got)
	}
	s.matchCase, s.word, s.query = false, true, "Answe"
	hs.settleSearch()
	if s.count != 0 || !tt.HasText("No results found") {
		t.Errorf("whole word Answe: %d", s.count)
	}
	s.word, s.regex, s.query = false, true, `const \w+ =`
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "internal/a/a.go internal/b/b.ts" {
		t.Errorf("regex: %q", got)
	}
	s.query = `(`
	hs.settleSearch()
	if s.err == "" || !tt.HasText("Invalid expression") {
		t.Error("no error for an invalid expression")
	}
}

func TestSearchFilesAndEdits(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	s := &a.inFiles
	hs.searchFor("Mayoi")
	if got := strings.Join(resultFiles(s), " "); got != "README.md main.go web/index.html" {
		t.Fatalf("results %q", got)
	}
	s.include = "*.md, web/**"
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "README.md web/index.html" {
		t.Errorf("include: %q", got)
	}
	s.exclude = "web"
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "README.md" {
		t.Errorf("exclude: %q", got)
	}
	s.include, s.exclude = "", ""
	hs.settleSearch()

	// An open file is searched as it is, unsaved edits and all.
	a.openFile(hs.path("internal/b/b.ts"), true)
	hs.settle()
	d := a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, "// Mayoi\n"})
	hs.settleSearch()
	if got := strings.Join(resultFiles(s), " "); got != "README.md internal/b/b.ts main.go web/index.html" {
		t.Errorf("after an edit: %q", got)
	}
	// So is a file changed on disk.
	os.WriteFile(hs.path("go.mod"), []byte("module mayoi\n"), 0o644)
	os.WriteFile(hs.path("web/index.html"), []byte("<!doctype html>\n"), 0o644)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hs.settle()
		if strings.Join(resultFiles(s), " ") == "README.md go.mod internal/b/b.ts main.go" {
			break
		}
	}
	if got := strings.Join(resultFiles(s), " "); got != "README.md go.mod internal/b/b.ts main.go" {
		t.Errorf("after changes on disk: %q", got)
	}

	// Binary files are passed by.
	os.WriteFile(hs.path("blob.bin"), []byte("Mayoi\x00\x01"), 0o644)
	a.indexStale = true
	a.refreshIndex()
	hs.settle()
	a.startSearch()
	hs.settleSearch()
	for _, f := range s.files {
		if f.rel == "blob.bin" {
			t.Error("searched a binary file")
		}
	}
}

func TestReplaceInFiles(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	// The open file's replacement stays to save, which a slow machine's
	// settling would give the time to save itself.
	a.settings.AutoSave = false
	s := &a.inFiles
	// One file open, the other not; one with CRLF line endings.
	os.WriteFile(hs.path("web/style.css"), []byte("body {\r\n  color: #333;\r\n}\r\n"), 0o600)
	os.Chmod(hs.path("web/style.css"), 0o600)
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	hs.searchFor("color")
	if s.count != 1 {
		t.Fatalf("%d matches of color", s.count)
	}
	s.query = "o"
	s.include = "main.go, web/style.css"
	hs.settleSearch()
	if len(s.files) != 2 {
		t.Fatalf("results %q", resultFiles(s))
	}
	s.replacing, s.with = true, "0"
	hs.settle()
	a.replaceAllInFiles()
	if a.confirm == nil || !strings.Contains(a.confirm.title, "in 2 files") {
		t.Fatalf("question %+v", a.confirm)
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleSearch()
	if s.count != 0 {
		t.Errorf("%d matches left in %q", s.count, resultFiles(s))
	}
	css, _ := os.ReadFile(hs.path("web/style.css"))
	if string(css) != "b0dy {\r\n  c0l0r: #333;\r\n}\r\n" {
		t.Errorf("style.css: %q", css)
	}
	// Windows has no permission bits to keep.
	if info, _ := os.Stat(hs.path("web/style.css")); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("style.css mode %v", info.Mode())
	}
	d := a.activeEditor()
	if got := d.buf.String(); strings.Contains(got, "o") || !d.dirty() {
		t.Errorf("main.go: %q", got)
	}
	if !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.HasPrefix(s, "Replaced ") }) {
		t.Errorf("no note of the replacement: %q", hs.tt.Texts())
	}
	// The open file's replacement is one step of undo.
	d.undo()
	hs.settle()
	if got := d.buf.String(); !strings.HasPrefix(got, "package main") || d.dirty() {
		t.Errorf("after undo: %q", got)
	}
}

func TestReplaceOneMatch(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	s := &a.inFiles
	hs.searchFor(`fmt\.(\w+)`)
	if s.count != 0 {
		t.Fatalf("matched %q as text", s.query)
	}
	s.regex = true
	hs.settleSearch()
	if s.count != 1 {
		t.Fatalf("%d matches", s.count)
	}
	s.replacing, s.with, s.details = true, "log.$1", true
	hs.settle()
	hs.shot("search-replace")
	if spans := a.matchPreview(s.files[0].matches[0]); len(spans) != 4 || spans[1].Text != "fmt.Println" || !spans[1].Strikethrough || spans[2].Text != "log.Println" {
		t.Errorf("preview %+v", spans)
	}
	f, m := s.files[0], s.files[0].matches[0]
	// A match that moved since the search is left alone.
	os.WriteFile(hs.path("main.go"), []byte("package main\n\n"+mustRead(t, hs.path("main.go"))), 0o644)
	stale := m
	a.replaceMatches([]*searchFile{f}, &stale)
	hs.settleSearch()
	if got := mustRead(t, hs.path("main.go")); !strings.Contains(got, "fmt.Println") {
		t.Errorf("replaced a stale match: %q", got)
	}
	if !hs.tt.HasText("The match changed since the search; search again") {
		t.Error("no word of the stale match")
	}
	f, m = s.files[0], s.files[0].matches[0]
	a.replaceMatches([]*searchFile{f}, &m)
	hs.settleSearch()
	if got := mustRead(t, hs.path("main.go")); !strings.Contains(got, `log.Println("hello, mayoi")`) {
		t.Errorf("main.go: %q", got)
	}
}

func TestSearchViewKeys(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("internal/a/a.go"), true)
	hs.settle()
	// "Answer", on the third line, selected.
	d := a.activeEditor()
	s0, _ := d.buf.Snapshot().LineRange(2)
	d.selectRange(s0+6, s0+12, false)
	hs.settle()
	a.run("edit.replaceInFiles")
	hs.settleSearch()
	s := &a.inFiles
	if s.query != "Answer" || !s.replacing {
		t.Fatalf("query %q, replacing %v", s.query, s.replacing)
	}
	if !tt.Focused("Search") {
		t.Error("the query lacks the keyboard")
	}
	// Down goes to the first match, and Enter shows it.
	tt.Key(0, ui.KeyDown)
	hs.settle()
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	d = a.activeEditor()
	if got := d.buf.Slice(d.sel[0], d.sel[1]); d.path != hs.path("internal/a/a.go") || got != "Answer" {
		t.Errorf("Enter selected %q in %s", got, d.path)
	}
	// Folding a file hides its matches.
	tt.Click("internal/a")
	hs.settle()
	if len(a.searchRows()) != 1 {
		t.Errorf("%d rows with the file folded", len(a.searchRows()))
	}
}

func mustRead(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Down and Up walk the results past their headers, as far as they go and
// with the list scrolled, and Up from the first match goes to the query.
func TestSearchViewWalkResults(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	for i := range 6 {
		var b strings.Builder
		for j := range 8 {
			fmt.Fprintf(&b, "line %d needle %d\n", j, i)
		}
		os.WriteFile(hs.path(fmt.Sprintf("n%d.txt", i)), []byte(b.String()), 0o644)
	}
	a.indexStale = true
	hs.searchFor("needle")
	s := &a.inFiles
	rows := a.searchRows()
	if s.count != 48 || len(rows) != 54 {
		t.Fatalf("%d matches in %d rows", s.count, len(rows))
	}
	var want []int // the rows of the matches
	for i, r := range rows {
		if r.match >= 0 {
			want = append(want, i)
		}
	}
	tt.Key(0, ui.KeyDown)
	hs.settle()
	for i, row := range want {
		if s.row != row {
			t.Fatalf("after %d Downs, row %d; want %d", i+1, s.row, row)
		}
		tt.Key(0, ui.KeyDown)
		hs.settle()
	}
	if s.row != want[len(want)-1] {
		t.Errorf("Down past the end moved to %d", s.row)
	}
	for i := len(want) - 2; i >= 0; i-- {
		tt.Key(0, ui.KeyUp)
		hs.settle()
		if s.row != want[i] {
			t.Fatalf("Up to row %d; want %d", s.row, want[i])
		}
	}
	tt.Key(0, ui.KeyUp)
	hs.settle()
	if !tt.Focused("Search") {
		t.Error("Up from the first match kept the keyboard in the results")
	}
	// Down from the query again starts at the first match.
	tt.Key(0, ui.KeyDown)
	hs.settle()
	tt.Key(0, ui.KeyDown)
	hs.settle()
	if s.row != want[1] {
		t.Errorf("row %d after going back to the results; want %d", s.row, want[1])
	}
	// Escape goes back to the query too.
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if !tt.Focused("Search") {
		t.Error("Escape kept the keyboard in the results")
	}
}

// A query ignoring case finds what its expression finds, without it.
func TestFoldQueryMatchesAsRegexp(t *testing.T) {
	texts := []string{
		"", "aaa", "AaAa", "Error errorf ERRORF", "the Kelvin kelvin", "miſs mis",
		"héllo HELLO héllo", "a\nB\na b", "erroR_err err",
	}
	queries := []string{"a", "aa", "errorf", "err", "kelvin", "s", "hello", "a b", "b"}
	for _, word := range []bool{false, true} {
		for _, q := range queries {
			re, _ := compileQuery(q, false, word, false)
			fq := foldQueryOf(q, false, word, false)
			for _, text := range texts {
				want := re.FindAllStringIndex(text, -1)
				got, ok := fq.matches(text, -1)
				if !ok {
					continue
				}
				if fmt.Sprint(got) != fmt.Sprint(want) && !(len(got) == 0 && len(want) == 0) {
					t.Errorf("%q in %q, word %v: %v; want %v", q, text, word, got, want)
				}
			}
		}
	}
	if q := foldQueryOf("é", false, false, false); q.needle != "" {
		t.Error("folds a query beyond ASCII")
	}
}

// The files searched at once find no more than maxSearchMatches in all.
func TestSearchStopsAtMost(t *testing.T) {
	hs := newHarness(t, true)
	line := strings.Repeat("needle ", 10) + "\n"
	for i := range 30 {
		os.WriteFile(hs.path(fmt.Sprintf("n%02d.txt", i)), []byte(strings.Repeat(line, 100)), 0o644)
	}
	hs.a.indexStale = true
	hs.searchFor("needle")
	s := &hs.a.inFiles
	n := 0
	for _, f := range s.files {
		n += len(f.matches)
	}
	if !s.stopped || s.count != maxSearchMatches || n != maxSearchMatches {
		t.Errorf("stopped %v with %d matches, %d in the files", s.stopped, s.count, n)
	}
}
