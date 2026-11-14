package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWikiQueryIn(t *testing.T) {
	for _, c := range []struct {
		line  string
		col   int
		ok    bool
		text  string
		to    int
		close int
		embed bool
	}{
		{"see [[no", 8, true, "no", 8, -1, false},
		{"see [[]]", 6, true, "", 6, 6, false},
		{"see [[note]] after", 8, true, "no", 10, 10, false},
		{"see [[note|label]]", 8, true, "no", 10, 16, false},
		{"![[pic", 6, true, "pic", 6, -1, true},
		{"see [[note]] after", 14, false, "", 0, 0, false},
		{"see [[note|lab", 14, false, "", 0, 0, false},
		{"see [note", 9, false, "", 0, 0, false},
		{"`[[code", 7, false, "", 0, 0, false},
		{"`a` [[x", 7, true, "x", 7, -1, false},
		{"[[王健康/评", 7, true, "王健康/评", 7, -1, false},
		{"[[a#He", 6, true, "a#He", 6, -1, false},
	} {
		q, ok := wikiQueryIn([]rune(c.line), c.col)
		if ok != c.ok {
			t.Errorf("%q at %d: ok %v, want %v", c.line, c.col, ok, c.ok)
			continue
		}
		if ok && (q.text != c.text || q.to != c.to || q.close != c.close || q.embed != c.embed) {
			t.Errorf("%q at %d: got %+v, want text %q to %d close %d embed %v", c.line, c.col, q, c.text, c.to, c.close, c.embed)
		}
	}
}

func TestWikiFenced(t *testing.T) {
	for text, want := range map[string]bool{
		"":                         false,
		"text\n":                   false,
		"```go\n":                  true,
		"```go\ncode\n```\n":       false,
		"~~~~\n```\n":              true,
		"````\n```\n````\n":        false,
		"  ```\n":                  true,
		"```js\n``` not closing\n": true,
	} {
		if got := wikiFenced(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

func TestWikiAsk(t *testing.T) {
	for _, c := range []struct {
		text, target string
		mode         wikiMode
		query        string
	}{
		{"note", "note", wikiFilesMode, "note"},
		{"note#He", "note", wikiHeadingsMode, "He"},
		{"#He", "", wikiHeadingsMode, "He"},
		{"note#A#B", "note", wikiHeadingsMode, "B"},
		{"note#^ab", "note", wikiBlocksMode, "ab"},
		{"note^ab", "note", wikiBlocksMode, "ab"},
		{"^", "", wikiBlocksMode, ""},
	} {
		target, mode, query := wikiAsk(c.text)
		if target != c.target || mode != c.mode || query != c.query {
			t.Errorf("%q: got %q %d %q", c.text, target, mode, query)
		}
	}
}

func TestWikiHeadingsAndBlocks(t *testing.T) {
	src := "---\ntitle: x\n---\n# One\n\nA paragraph\nthat goes on.\n\n## Two ##\n\n- an item ^item1\n- another\n\n```\n# not a heading\ncode\n```\n\nLast one.\n^last\n"
	var heads []string
	for _, h := range wikiHeadingsOf(src) {
		heads = append(heads, h.text)
	}
	if !slices.Equal(heads, []string{"One", "Two"}) {
		t.Errorf("headings %q", heads)
	}
	blocks := wikiBlocksOf(src)
	var got []string
	for _, b := range blocks {
		got = append(got, b.text+"|"+b.id)
	}
	want := []string{"A paragraph that goes on.|", "an item|item1", "another|", "Last one.|last"}
	if !slices.Equal(got, want) {
		t.Fatalf("blocks %q, want %q", got, want)
	}
	// The paragraph's last line, counted with the front matter.
	if lines := strings.Split(src, "\n"); blocks[0].line != 6 || lines[6] != blocks[0].lineText {
		t.Errorf("paragraph on line %d, %q", blocks[0].line, blocks[0].lineText)
	}
}

func wikiVault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755)
	for rel, s := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { forgetWikiFiles(root) })
	return root
}

func TestWikiSuggestionsFiles(t *testing.T) {
	root := wikiVault(t, map[string]string{
		"daily/today.md": "", "projects/plan.md": "", "a/same.md": "", "b/same.md": "", "pic.png": "",
	})
	path := filepath.Join(root, "daily", "today.md")
	links := func(query string) []string {
		var out []string
		for _, it := range wikiSuggestions(wikiInput{path: path, mode: wikiFilesMode, query: query}) {
			out = append(out, it.link)
		}
		return out
	}
	if got := links("plan"); len(got) == 0 || got[0] != "plan" {
		t.Errorf("plan: %q", got)
	}
	// Two notes of one name: by their paths.
	if got := links("same"); !slices.Contains(got, "a/same") || !slices.Contains(got, "b/same") {
		t.Errorf("same: %q", got)
	}
	// An attachment keeps its extension.
	if got := links("pic"); len(got) == 0 || got[0] != "pic.png" {
		t.Errorf("pic: %q", got)
	}
	// The vault's own link format.
	os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), []byte(`{"newLinkFormat":"relative"}`), 0o644)
	wikiFormats.Lock()
	delete(wikiFormats.m, root)
	wikiFormats.Unlock()
	if got := links("plan"); len(got) == 0 || got[0] != "../projects/plan" {
		t.Errorf("relative plan: %q", got)
	}
	os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), []byte(`{"newLinkFormat":"absolute"}`), 0o644)
	wikiFormats.Lock()
	delete(wikiFormats.m, root)
	wikiFormats.Unlock()
	if got := links("plan"); len(got) == 0 || got[0] != "projects/plan" {
		t.Errorf("absolute plan: %q", got)
	}
}

func TestWikiSuggestionsHeadingsAndBlocks(t *testing.T) {
	root := wikiVault(t, map[string]string{
		"note.md":  "# Intro\n\n## Setup steps\n\nInstall it first.\n\n- with an id ^abc\n",
		"other.md": "",
	})
	path := filepath.Join(root, "other.md")
	heads := wikiSuggestions(wikiInput{path: path, target: "note", mode: wikiHeadingsMode, query: "setup"})
	if len(heads) != 1 || heads[0].link != "note#Setup steps" {
		t.Errorf("headings: %+v", heads)
	}
	blocks := wikiSuggestions(wikiInput{path: path, target: "note", mode: wikiBlocksMode})
	if len(blocks) != 2 || blocks[1].link != "note#^abc" || blocks[1].newID != "" {
		t.Fatalf("blocks: %+v", blocks)
	}
	if b := blocks[0]; b.newID == "" || b.link != "note#^"+b.newID || b.file == "" || b.lineText != "Install it first." {
		t.Errorf("block without an id: %+v", b)
	}
	// The note's own, from its text.
	self := wikiSuggestions(wikiInput{path: path, mode: wikiHeadingsMode, self: "# Mine\n"})
	if len(self) != 1 || self[0].link != "#Mine" {
		t.Errorf("own headings: %+v", self)
	}
}

// TestWikiSuggestTyping: [[ typed in a note opens the box of its vault's
// notes, which Enter puts in place, the caret after the link.
func TestWikiSuggestTyping(t *testing.T) {
	hs := newHarness(t, true)
	os.MkdirAll(hs.path(".obsidian"), 0o755)
	os.WriteFile(hs.path("ideas.md"), []byte("# Ideas\n\nA thought.\n"), 0o644)
	os.WriteFile(hs.path("today.md"), []byte("see \n"), 0o644)
	t.Cleanup(func() { forgetWikiFiles(hs.root) })
	hs.a.openFile(hs.path("today.md"), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.place(d, 4, 4)
	hs.insert("[")
	hs.insert("[")
	if got := d.buf.String(); got != "see [[]]\n" {
		t.Fatalf("typed %q", got)
	}
	hs.insert("ide")
	hs.until("the suggestions", func() bool { s := &hs.a.wikiSug; return s.open && !s.loading && len(s.items) > 0 })
	if s := &hs.a.wikiSug; s.items[0].link != "ideas" {
		t.Fatalf("first suggestion %+v", s.items[0])
	}
	hs.key(0, ui.KeyEnter)
	if got := d.buf.String(); got != "see [[ideas]]\n" {
		t.Fatalf("chosen %q", got)
	}
	if d.sel != [2]int{13, 13} {
		t.Errorf("caret %v, want after the link", d.sel)
	}
	if hs.a.wikiSug.open {
		t.Error("the box stays open")
	}
	// A heading of it, after #.
	hs.place(d, 13, 13)
	hs.insert(" [[ideas#]]")
	hs.place(d, 22, 22)
	hs.insert("I")
	hs.until("the headings", func() bool {
		s := &hs.a.wikiSug
		return s.open && !s.loading && len(s.items) > 0 && s.items[0].mode == wikiHeadingsMode
	})
	hs.key(0, ui.KeyEnter)
	if got := d.buf.String(); got != "see [[ideas]] [[ideas#Ideas]]\n" {
		t.Fatalf("heading chosen %q", got)
	}
	// Escape closes it; typing on opens it again.
	hs.insert(" [[x")
	hs.until("the box", func() bool { return hs.a.wikiSug.open })
	hs.key(0, ui.KeyEscape)
	if hs.a.wikiSug.open {
		t.Fatal("Escape left the box open")
	}
	// Shift-Enter keeps what is typed.
	hs.insert("y")
	hs.until("the box again", func() bool { return hs.a.wikiSug.open })
	hs.key(ui.Shift, ui.KeyEnter)
	if got := d.buf.String(); !strings.HasSuffix(got, " [[xy]]\n") {
		t.Fatalf("as typed %q", got)
	}
}

// TestWikiSuggestLivePreview: in Live Preview the box shows under the
// link, and a block of the note itself chosen gets its id.
func TestWikiSuggestLivePreview(t *testing.T) {
	hs := newHarness(t, true)
	os.MkdirAll(hs.path(".obsidian"), 0o755)
	os.WriteFile(hs.path("ideas.md"), []byte("# Ideas\n"), 0o644)
	os.WriteFile(hs.path("today.md"), []byte("A thought.\n\nsee \n"), 0o644)
	t.Cleanup(func() { forgetWikiFiles(hs.root) })
	hs.a.settings.MarkdownMode = "live"
	hs.a.openFile(hs.path("today.md"), true)
	d := hs.a.activeEditor()
	hs.until("the note parsed", func() bool { l := d.document.live; return l != nil && l.doc != nil && !l.quick })
	hs.place(d, 16, 16)
	hs.insert("[")
	hs.insert("[")
	hs.until("the suggestions", func() bool { s := &hs.a.wikiSug; return s.open && !s.loading && len(s.items) > 0 })
	if !slices.Contains(hs.tt.Texts(), "ideas") {
		t.Errorf("the box does not show ideas: %q", hs.tt.Texts())
	}
	hs.insert("^")
	hs.until("the blocks", func() bool {
		s := &hs.a.wikiSug
		return s.open && !s.loading && len(s.items) > 0 && s.items[0].mode == wikiBlocksMode
	})
	id := hs.a.wikiSug.items[0].newID
	hs.key(0, ui.KeyEnter)
	if got, want := d.buf.String(), "A thought. ^"+id+"\n\nsee [[^"+id+"]]\n"; got != want && got != "A thought. ^"+id+"\n\nsee [[#^"+id+"]]\n" {
		t.Fatalf("got %q, want %q", got, want)
	}
}
