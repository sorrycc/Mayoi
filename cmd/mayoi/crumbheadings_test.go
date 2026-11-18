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

// headingsDoc has headings nested three deep, and enough text between
// them for the Reading view to scroll to each.
var headingsDoc = strings.NewReplacer("text\n", "text\n"+strings.Repeat("\nfiller\n", 40)).Replace(`intro

# Title

text

## 1. Section

text

### Deep

text

## 2. Other

text
`) + "```\n# not a heading\n```\n"

// headingLineOf is the line, from 0, of the first line of s that is text.
func headingLineOf(s, text string) int {
	return strings.Count(s[:strings.Index(s, text)], "\n")
}

// The headings that hold a line are the last at or before it and those
// it is under; none before the first.
func TestHeadingChain(t *testing.T) {
	o := outlineOf(symbols.Find(ui.NewTextBuffer(headingsDoc).Snapshot(), true))
	name := func(chain []int) []string {
		var out []string
		for _, i := range chain {
			out = append(out, o.heads[i].Name)
		}
		return out
	}
	cases := []struct {
		at   string
		want []string
	}{
		{"intro", nil},
		{"# Title", []string{"Title"}},
		{"## 1. Section", []string{"Title", "1. Section"}},
		{"### Deep\n\ntext", []string{"Title", "1. Section", "Deep"}},
		{"## 2. Other", []string{"Title", "2. Other"}},
		{"# not a heading", []string{"Title", "2. Other"}},
	}
	for _, c := range cases {
		line := headingLineOf(headingsDoc, c.at)
		if c.at == "### Deep\n\ntext" {
			line += 2
		}
		if got := name(o.chain(line)); !slices.Equal(got, c.want) {
			t.Errorf("at %q: %v, want %v", c.at, got, c.want)
		}
	}
	if got, want := o.roots, []int{0}; !slices.Equal(got, want) {
		t.Errorf("roots %v, want %v", got, want)
	}
	if got, want := o.kids[0], []int{1, 3}; !slices.Equal(got, want) {
		t.Errorf("under the title %v, want %v", got, want)
	}
}

// headingsHarness opens notes.md, of headingsDoc, with the Explorer hidden
// and the caret at text.
func headingsHarness(t *testing.T, at string) (*harness, *editor) {
	hs := newHarness(t, false)
	os.WriteFile(hs.path("notes.md"), []byte(headingsDoc), 0o644)
	hs.a.openFile(hs.path("notes.md"), true)
	hs.settle()
	hs.a.run("view.sidebar")
	hs.settle()
	d := hs.a.activeEditor()
	hs.a.jumpToScope(d, headingLineOf(headingsDoc, at))
	hs.settle()
	return hs, d
}

// The breadcrumbs name the headings the caret is in, after the file, and
// none in a document without headings.
func TestHeadingCrumbs(t *testing.T) {
	hs, d := headingsHarness(t, "### Deep")
	a, tt := hs.a, hs.tt
	for _, name := range []string{"Title", "1. Section", "Deep"} {
		if _, ok := tt.Find(locales.CrumbsHeadingTip(name)); !ok {
			t.Errorf("no crumb for %s: %v", name, tt.Texts())
		}
	}
	if _, ok := tt.Find(locales.CrumbsHeadingTip("2. Other")); ok {
		t.Error("a crumb for a heading the caret is not in")
	}
	// The caret moved, the crumbs follow, and the bar keeps its height.
	bar, _ := tt.Find(locales.CrumbsHeadingTip("Title"))
	s, _ := d.buf.Snapshot().LineRange(headingLineOf(headingsDoc, "## 2. Other"))
	d.selectRange(s, s, false)
	hs.settle()
	if _, ok := tt.Find(locales.CrumbsHeadingTip("Deep")); ok {
		t.Error("the crumb of a heading left stays")
	}
	if r, ok := tt.Find(locales.CrumbsHeadingTip("2. Other")); !ok || r.Y != bar.Y || r.H != bar.H {
		t.Errorf("the crumb of the heading the caret came to is at %v, ok %v, was %v", r, ok, bar)
	}
	// Before the first heading, none.
	d.selectRange(0, 0, false)
	hs.settle()
	if _, ok := tt.Find(locales.CrumbsHeadingTip("Title")); ok {
		t.Error("a heading crumb before the first heading")
	}
	// A document without headings shows none, nor a chevron after its name.
	os.WriteFile(hs.path("plain.md"), []byte("just text\n\nmore\n"), 0o644)
	a.openFile(hs.path("plain.md"), true)
	hs.settle()
	if _, ok := tt.Find(locales.CrumbsHeadingTip("Title")); ok {
		t.Error("a heading crumb in a document without headings")
	}
	if o, chain := a.headingCrumbs(a.activeEditor()); o != nil || chain != nil {
		t.Errorf("headings of a document without: %v", chain)
	}
}

// The headings are read once for each version of the text: at once the
// first time, and after an edit in the background, the last read standing
// until then.
func TestHeadingCrumbsCache(t *testing.T) {
	hs, d := headingsHarness(t, "# Title")
	a := hs.a
	o := a.headingsOf(d.document)
	heads := o.heads
	hs.settle()
	if o = a.headingsOf(d.document); &o.heads[0] != &heads[0] {
		t.Error("the headings were read again for the same text")
	}
	d.buf.Replace(0, 0, "# New\n")
	if o = a.headingsOf(d.document); o.heads[0].Name != "Title" {
		t.Errorf("the headings were read again at the edit: %v", o.heads[0])
	}
	hs.settle()
	if o = a.headingsOf(d.document); o.heads[0].Name != "New" || o.version != d.buf.Version() {
		t.Errorf("the headings of the text changed: %v", o.heads[0])
	}
	// The caret, kept where it was, is under the heading put above it.
	if _, ok := hs.tt.Find(locales.CrumbsHeadingTip("New")); !ok {
		t.Errorf("no crumb after the headings were read again: %v", hs.tt.Texts())
	}
}

// A heading crumb opens a box of the document's headings, nested, the one
// the caret is in chosen; its arrows fold, and a heading clicked is gone
// to, in source, Live Preview and the Reading view.
func TestHeadingCrumbsBox(t *testing.T) {
	for _, mode := range []string{"", "live", "reading"} {
		t.Run("mode="+mode, func(t *testing.T) {
			hs, d := headingsHarness(t, "### Deep")
			a, tt := hs.a, hs.tt
			if mode != "" {
				a.setMarkdownMode(mode)
				hs.settle()
				if mode == "reading" {
					hs.until("preview", func() bool { return d.preview != nil && d.preview.loaded })
					hs.settle()
				}
			}
			if err := tt.Click(locales.CrumbsHeadingTip("Title")); err != nil {
				t.Fatalf("%v: %v", err, tt.Texts())
			}
			hs.settle()
			k := &a.crumbs
			if !k.open || !k.heads {
				t.Fatal("no box of headings")
			}
			var rows []string
			for r := range k.outline.Rows() {
				rows = append(rows, strings.Repeat(" ", k.outline.Depth(r))+d.headings.heads[k.outline.Item(r)].Name)
			}
			if want := []string{"Title", " 1. Section", "  Deep", " 2. Other"}; !slices.Equal(rows, want) {
				t.Errorf("rows %q, want %q", rows, want)
			}
			if k.sel < 0 || d.headings.heads[k.outline.Item(k.sel)].Name != "Deep" {
				t.Errorf("row %d is chosen, not Deep", k.sel)
			}
			// Left on the chosen row folds the one it is under; Right opens it.
			k.sel = 1
			tt.Key(0, ui.KeyLeft)
			hs.settle()
			if k.outline.Rows() != 3 {
				t.Errorf("Left left %d rows", k.outline.Rows())
			}
			tt.Key(0, ui.KeyRight)
			hs.settle()
			// Down to 2. Other, and Enter goes there.
			k.sel = 3
			tt.Key(0, ui.KeyEnter)
			for range 4 {
				hs.settle()
			}
			if k.open {
				t.Error("the box stays open after a heading is chosen")
			}
			want := headingLineOf(headingsDoc, "## 2. Other")
			if got := readingLine(d); got != want {
				t.Errorf("at line %d after going to 2. Other, want %d", got, want)
			}
			if _, ok := tt.Find(locales.CrumbsHeadingTip("2. Other")); !ok {
				t.Errorf("the crumbs do not follow: %v", tt.Texts())
			}
		})
	}
}

// A long chain of headings shows the outermost and the innermost two, an
// ellipsis for those between, which opens the box; the innermost is the
// one in the text's color, and each shows its name without its marks.
func TestHeadingCrumbsCollapse(t *testing.T) {
	doc := "# One\n\n## Two\n\n### Three\n\n#### Four\n\n##### Five\n\ntext\n"
	hs := newHarness(t, false)
	os.WriteFile(hs.path("deep.md"), []byte(doc), 0o644)
	hs.a.openFile(hs.path("deep.md"), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.a.jumpToScope(d, headingLineOf(doc, "text"))
	hs.settle()
	tt := hs.tt
	for _, name := range []string{"One", "Four", "Five"} {
		if _, ok := tt.Find(locales.CrumbsHeadingTip(name)); !ok {
			t.Errorf("no crumb for %s: %v", name, tt.Texts())
		}
	}
	for _, name := range []string{"Two", "Three"} {
		if _, ok := tt.Find(locales.CrumbsHeadingTip(name)); ok {
			t.Errorf("a crumb for %s, which the ellipsis stands for", name)
		}
	}
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "#") {
			t.Errorf("a crumb shows a heading's marks: %q", s)
		}
	}
	if err := tt.Click(locales.CrumbsHeadings()); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	hs.settle()
	if k := &hs.a.crumbs; !k.open || !k.heads || k.at != 2 {
		t.Fatalf("the ellipsis opened %+v", k.at)
	}
	if _, ok := tt.Find("H5"); !ok {
		t.Errorf("the box does not show the levels: %v", tt.Texts())
	}
}
