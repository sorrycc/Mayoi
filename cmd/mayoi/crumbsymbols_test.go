package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/symbols"
	"mayoi/locales"
)

// symbolsHarness opens a file of symbols with the Explorer hidden, so that
// the breadcrumbs' name is the only one on screen.
func symbolsHarness(t *testing.T, name, text string) (*harness, *editor) {
	t.Helper()
	hs := newHarness(t, false)
	os.WriteFile(hs.path(name), []byte(text), 0o644)
	hs.a.openFile(hs.path(name), true)
	hs.settle()
	hs.a.run("view.sidebar")
	hs.settle()
	return hs, hs.a.activeEditor()
}

// symbolsText declares a function, a method and a type, whose names sort
// unlike their lines: first, third, then second.
const symbolsText = "package a\n\nfunc first() {}\n\ntype second struct{}\n\nfunc (s second) third() {}\n"

// openSymbols clicks the file's name in the breadcrumbs, which opens the
// box of its symbols.
func openSymbols(t *testing.T, hs *harness, name string) {
	t.Helper()
	if err := hs.tt.Click(locales.CrumbsCurrentFileTip(name)); err != nil {
		t.Fatalf("%v: %v", err, hs.tt.Texts())
	}
	hs.settle()
}

// rowsOf are the symbols the box shows, in the order of its rows.
func rowsOf(k *crumbsPicker) []string {
	var out []string
	for r := range k.outline.Rows() {
		out = append(out, k.syms[k.outline.Item(r)].Name)
	}
	return out
}

// The name of the file in the breadcrumbs opens its symbols, in the order
// of the document, as go to symbol lists them.
func TestCrumbsSymbolsList(t *testing.T) {
	hs, d := symbolsHarness(t, "a.go", symbolsText)
	a, tt := hs.a, hs.tt
	openSymbols(t, hs, "a.go")
	k := &a.crumbs
	if !k.open || !k.self {
		t.Fatalf("no box of symbols: open %v self %v", k.open, k.self)
	}
	_, names := a.documentSymbols()
	if !slices.Equal(rowsOf(k), names) {
		t.Errorf("rows %q, want the document's order %q", rowsOf(k), names)
	}
	for _, name := range []string{"first", "second", "third"} {
		if _, ok := tt.Find(name); !ok {
			t.Errorf("no row of %q: %v", name, tt.Texts())
		}
	}
	// A second click on the name closes the box, as a folder's does.
	openSymbols(t, hs, "a.go")
	if k.open {
		t.Error("a second click leaves the box open")
	}
	if d == nil {
		t.Fatal("no editor")
	}
}

// A symbol clicked is gone to, as go to symbol goes to it: its line to the
// top, its name selected.
func TestCrumbsSymbolsJump(t *testing.T) {
	hs, d := symbolsHarness(t, "a.go", symbolsText)
	a, tt := hs.a, hs.tt
	openSymbols(t, hs, "a.go")
	if err := tt.Click("third"); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	hs.settle()
	if a.crumbs.open {
		t.Error("the box stays open after a symbol is chosen")
	}
	if d.sel != [2]int{66, 71} { // "third" on the line at 50
		t.Fatalf("selection %v", d.sel)
	}
}

// The keys move in the box, from the symbol the reader is in: Enter goes to
// the one chosen, and Escape closes the box.
func TestCrumbsSymbolsKeys(t *testing.T) {
	hs, d := symbolsHarness(t, "a.go", symbolsText)
	a, tt := hs.a, hs.tt
	// The caret in third, whose symbol the box chooses: it is the last row.
	a.jumpToScope(d, 6)
	hs.settle()
	openSymbols(t, hs, "a.go")
	k := &a.crumbs
	if got := k.syms[k.outline.Item(k.sel)].Name; got != "third" {
		t.Fatalf("row %d is %q, not the symbol of the caret", k.sel, got)
	}
	tt.Key(0, ui.KeyUp)
	hs.settle()
	if got := k.syms[k.outline.Item(k.sel)].Name; got != "second" {
		t.Errorf("Up chose %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	for range 4 {
		hs.settle()
	}
	if k.open {
		t.Error("Enter leaves the box open")
	}
	if want := strings.Index(symbolsText, "type second"); d.sel[0] < want || d.sel[0] > want+len("type second") {
		t.Errorf("the selection %v is not of second", d.sel)
	}
	// Escape closes it.
	openSymbols(t, hs, "a.go")
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if k.open {
		t.Error("Escape leaves the box open")
	}
}

// The symbols of a note are its headings, as go to symbol lists them, in
// the order of the note, and one chosen is gone to as a heading of the box
// of headings is.
func TestCrumbsSymbolsMarkdown(t *testing.T) {
	doc := "# Title\n\ntext\n\n## Section\n\ntext\n\n```\n# not a heading\n```\n"
	hs, d := symbolsHarness(t, "notes.md", doc)
	a, tt := hs.a, hs.tt
	openSymbols(t, hs, "notes.md")
	k := &a.crumbs
	_, names := a.documentSymbols()
	if !slices.Equal(rowsOf(k), names) {
		t.Errorf("rows %q, want the document's order %q", rowsOf(k), names)
	}
	for _, name := range rowsOf(k) {
		if strings.Contains(name, "not a heading") {
			t.Errorf("the box lists a heading of a code block: %q", rowsOf(k))
		}
	}
	if err := tt.Click("Section"); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	for range 4 {
		hs.settle()
	}
	if got, want := readingLine(d), headingLineOf(doc, "## Section"); got != want {
		t.Errorf("at line %d after going to Section, want %d", got, want)
	}
}

// The box goes as the file does, with another file shown, and a file
// without declarations says so rather than show nothing.
func TestCrumbsSymbolsFollowsTheFile(t *testing.T) {
	hs, _ := symbolsHarness(t, "a.go", symbolsText)
	a, tt := hs.a, hs.tt
	openSymbols(t, hs, "a.go")
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	if a.crumbs.open {
		t.Error("the box stays open over another file")
	}
	if _, ok := tt.Find(locales.CrumbsCurrentFileTip("main.go")); !ok {
		t.Errorf("no name of the file shown: %v", tt.Texts())
	}
	// A file without declarations says so rather than show nothing.
	os.WriteFile(hs.path("plain.go"), []byte("package a\n"), 0o644)
	a.openFile(hs.path("plain.go"), true)
	hs.settle()
	openSymbols(t, hs, "plain.go")
	if _, ok := tt.Find(locales.SymbolsNone()); !ok {
		t.Errorf("a file without symbols does not say so: %v", tt.Texts())
	}
}

// The row of a symbol the box opens on is that of the symbol a line is in:
// the last one declared at or before it, as a heading holds the text under
// it, and none before the first.
func TestSymbolsRowOfALine(t *testing.T) {
	syms := []symbols.Symbol{{Line: 3, Name: "a"}, {Line: 7, Name: "b"}, {Line: 7, Name: "c"}, {Line: 12, Name: "d"}}
	for _, c := range []struct {
		line int
		row  int
		name string
	}{
		{0, -1, ""},   // before the first
		{3, 0, "a"},   // on its line
		{6, 0, "a"},   // between two
		{7, 2, "c"},   // the last of those at the line
		{9, 2, "c"},   // past it
		{12, 3, "d"},  // the last
		{400, 3, "d"}, // past the last
	} {
		at := symbolRowOf(syms, c.line)
		if at != c.row || at >= 0 && syms[at].Name != c.name {
			t.Errorf("line %d is row %d, want %d %q", c.line, at, c.row, c.name)
		}
	}
}
