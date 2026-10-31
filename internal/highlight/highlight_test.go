package highlight

import (
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLexSplitsTokensByLine(t *testing.T) {
	src := "package main\n\n/* a\nb */\nfunc f() string { return \"hé\" }\n"
	lines := Lex(Lexer("x.go"), src)
	if len(lines) != 6 {
		t.Fatalf("%d lines, want 6", len(lines))
	}
	if got := lines[0].Tokens; len(got) == 0 || got[0] != (Token{0, 7, Keyword}) {
		t.Errorf("line 0 tokens %v", got)
	}
	// The block comment continues on its second line.
	if got := lines[3].Tokens; len(got) == 0 || got[0].Kind != Comment || got[0].Start != 0 {
		t.Errorf("line 3 tokens %v", got)
	}
	var str *Token
	for i, tok := range lines[4].Tokens {
		if tok.Kind == String {
			str = &lines[4].Tokens[i]
		}
	}
	// Columns are runes: "hé" with its quotes is 4 runes.
	if str == nil || str.End-str.Start != 4 {
		t.Errorf("string token %v in %v", str, lines[4].Tokens)
	}
}

func TestVueDirectiveOverLines(t *testing.T) {
	src := "<template>\n  <div\n    v-if=\"\n      a &&\n      !b\n    \"\n    class=\"hint\"\n  >\n    text\n  </div>\n</template>\n"
	lines := Lex(Lexer("x.vue"), src)
	// class after the wrapped value is an attribute, not more of its string.
	if got := lines[6].Tokens; len(got) == 0 || got[0] != (Token{4, 9, Attribute}) {
		t.Errorf("class line tokens %v", got)
	}
	if !slices.Contains(lines[9].Tokens, Token{4, 7, Tag}) {
		t.Errorf("closing div tokens %v", lines[9].Tokens)
	}
	if Name(Lexer("x.vue")) != "vue" {
		t.Errorf("lexer %q", Name(Lexer("x.vue")))
	}
}

func TestPlainText(t *testing.T) {
	if Lexer("notes.unknownext") != nil {
		t.Error("lexer for an unknown extension")
	}
	if Lexer("Makefile") == nil || Lexer("main.go") == nil {
		t.Error("no lexer for Makefile or main.go")
	}
}

type fakeSource struct {
	lines   []string
	version uint64
	text    string
	starts  []int
}

func newSource(lines []string, version uint64) fakeSource {
	s := fakeSource{lines: lines, version: version, text: strings.Join(lines, "\n")}
	at := 0
	for _, l := range lines {
		s.starts = append(s.starts, at)
		at += len(l) + 1
	}
	return s
}

func (s fakeSource) Version() uint64 { return s.version }
func (s fakeSource) LineCount() int  { return len(s.lines) }
func (s fakeSource) LineRange(line int) (int, int) {
	return s.starts[line], s.starts[line] + len(s.lines[line])
}
func (s fakeSource) Slice(a, z int) string { return s.text[a:z] }

func TestHighlighterRejectsChangedLines(t *testing.T) {
	done := make(chan struct{}, 8)
	h := New(Lexer("x.go"), func() { done <- struct{}{} })
	src := newSource([]string{"package main", "var x = 1"}, 1)
	h.Request(src, 0, 1)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no notice")
	}
	if len(h.Line(0, "package main")) == 0 {
		t.Error("no tokens for an unchanged line")
	}
	if h.Line(0, "pzckage main") != nil {
		t.Error("tokens for a line changed by a replacement")
	}
}

func TestLineKeepsTokensAcrossAnEdit(t *testing.T) {
	l := Line{Text: `x := "ab" // c`, Tokens: []Token{{2, 4, Operator}, {5, 9, String}, {10, 14, Comment}}}
	cases := []struct {
		text string
		want []Token
	}{
		// Typed inside the string: it grows, and the comment moves.
		{`x := "aXYb" // c`, []Token{{2, 4, Operator}, {5, 11, String}, {12, 16, Comment}}},
		// Deleted inside the string.
		{`x := "b" // c`, []Token{{2, 4, Operator}, {5, 8, String}, {9, 13, Comment}}},
		// Typed at the end of the operator, which may now be another: plain.
		{`x :== "ab" // c`, []Token{{6, 10, String}, {11, 15, Comment}}},
		// Deleted across two tokens: both plain.
		{`x :ab" // c`, []Token{{7, 11, Comment}}},
		// Replaced: nothing is known.
		{`x := "zz" // c`, nil},
	}
	for _, c := range cases {
		if got := adjust(l, c.text); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}

// Lines moved down by a line inserted above them keep their tokens while
// the next pass is to come.
func TestHighlighterKeepsMovedLines(t *testing.T) {
	done := make(chan struct{}, 8)
	h := New(Lexer("x.go"), func() { done <- struct{}{} })
	src := newSource([]string{"package main", "", "var x = \"s\""}, 1)
	h.Request(src, 0, 2)
	<-done
	want := h.Line(2, `var x = "s"`)
	// As a request for the text with a line more does, before its pass.
	h.mu.Lock()
	h.shift = 1
	h.mu.Unlock()
	if got := h.Line(3, `var x = "s"`); len(want) == 0 || !slices.Equal(got, want) {
		t.Errorf("moved line: %v, want %v", got, want)
	}
}

// relexSamples are texts with tokens over lines, of a few languages.
var relexSamples = map[string]string{
	"x.go":   "package main\n\nimport \"fmt\"\n\n/* A block comment\n   over lines. */\nfunc main() {\n\ts := `raw\nstring`\n\t// a line comment\n\tfor i := 0; i < 10; i++ {\n\t\tfmt.Println(\"hi\", i, s)\n\t}\n}\n",
	"x.js":   "// js\nconst a = `tmpl ${x}\nmore`;\n/* block\n */\nfunction f(y) {\n  return y + 'q' + \"r\";\n}\n",
	"x.py":   "def f(x):\n    \"\"\"Doc\n    string.\"\"\"\n    s = '''a\n    b'''\n    # comment\n    return x * 2\n",
	"x.html": "<!doctype html>\n<!-- a\n comment -->\n<script>\nlet a = 1;\n</script>\n<p class=\"x\">hi</p>\n",
	"x.css":  "/* c\n */\nbody { color: #333; }\n.a::after { content: \"x\"; }\n",
	"x.md":   "# Title\n\n```go\nfunc f() {}\n```\n\n*em* and **strong**\n",
}

// Lexing again after an edit gives the tokens of lexing the whole text, in
// languages whose lexers' state the tokens tell.
func TestRelexMatchesLexingAll(t *testing.T) {
	inserts := []string{"/*", "*/", "`", "\"", "'''", `"""`, "x", "// y", "# z", "}", "{", "<!--", "-->", "<script>", "```", "func f() {", ""}
	for name, sample := range relexSamples {
		if name == "x.css" || name == "x.md" {
			continue // checked once quiet, below
		}
		t.Run(name, func(t *testing.T) {
			base := strings.Split(sample, "\n")
			var lines []string
			for range 30 {
				lines = append(lines, base...)
			}
			l := Lexer(name)
			for seed := range uint64(4) {
				rng := rand.New(rand.NewPCG(seed, 2))
				src := newSource(slices.Clone(lines), 1)
				prev := lexRange(l, src, 0, len(src.lines))
				for step := range 150 {
					next := slices.Clone(src.lines)
					i := rng.IntN(len(next))
					switch rng.IntN(4) {
					case 0: // a line inserted
						next = slices.Insert(next, i, inserts[rng.IntN(len(inserts))])
					case 1: // a line deleted
						if len(next) > 1 {
							next = slices.Delete(next, i, i+1)
						}
					default: // typed into a line
						ln := next[i]
						at := rng.IntN(len(ln) + 1)
						next[i] = ln[:at] + inserts[rng.IntN(len(inserts))] + ln[at:]
					}
					src = newSource(next, uint64(step+2))
					got, _ := relex(l, request{src: src, last: len(next) - 1}, prev)
					want := lexRange(l, src, 0, len(next))
					if len(got) != len(want) {
						t.Fatalf("seed %d, step %d: %d lines, want %d", seed, step, len(got), len(want))
					}
					for k := range want {
						if got[k].Text != want[k].Text || !slices.Equal(got[k].Tokens, want[k].Tokens) {
							t.Fatalf("seed %d, step %d, line %d %q: %v, want %v", seed, step, k, want[k].Text, got[k].Tokens, want[k].Tokens)
						}
					}
					prev = got
				}
			}
		})
	}
}

// After a burst of edits, once quiet, a highlighter's tokens are those of
// lexing the whole text, in every language.
func TestHighlighterSettlesOnLexingAll(t *testing.T) {
	defer func(q time.Duration) { Quiet = q }(Quiet)
	Quiet = 20 * time.Millisecond
	inserts := []string{"/*", "*/", "`", "\"", "{", "}", "```", "<!--", "-->", "x"}
	for name, sample := range relexSamples {
		t.Run(name, func(t *testing.T) {
			var lines []string
			for range 30 {
				lines = append(lines, strings.Split(sample, "\n")...)
			}
			l := Lexer(name)
			h := New(l, nil)
			defer h.Close()
			rng := rand.New(rand.NewPCG(7, 7))
			src := newSource(lines, 1)
			for step := range 200 {
				next := slices.Clone(src.lines)
				i := rng.IntN(len(next))
				at := rng.IntN(len(next[i]) + 1)
				next[i] = next[i][:at] + inserts[rng.IntN(len(inserts))] + next[i][at:]
				src = newSource(next, uint64(step+2))
				h.Request(src, 0, len(next)-1)
				if step%20 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
			want := lexRange(l, src, 0, len(src.lines))
			deadline := time.Now().Add(10 * time.Second)
			for {
				h.mu.Lock()
				got, version, running := h.lines, h.version, h.running
				h.mu.Unlock()
				if version == src.version && !running {
					for k := range want {
						if got[k].Text != want[k].Text || !slices.Equal(got[k].Tokens, want[k].Tokens) {
							t.Fatalf("line %d %q: %v, want %v", k, want[k].Text, got[k].Tokens, want[k].Tokens)
						}
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("not settled: version %d of %d, running %v", version, src.version, running)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}

// BenchmarkRelexTyping lexes a 6,000-line Go file again after a letter is
// typed in its middle, as each keystroke does.
func BenchmarkRelexTyping(b *testing.B) {
	src, _ := os.ReadFile("../../cmd/mayoi/editor.go")
	lines := strings.Split(strings.Repeat(string(src), 10), "\n")
	l := Lexer("x.go")
	s := newSource(lines, 1)
	prev := lexRange(l, s, 0, len(lines))
	mid := len(lines) / 2
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		next := slices.Clone(s.lines)
		next[mid] += "x"
		s = newSource(next, uint64(i+2))
		prev, _ = relex(l, request{src: s, last: len(next) - 1}, prev)
	}
}

// A large file opened shows the colors of the lines in view first, then
// those of the rest.
func TestHighlighterLexesTheViewFirst(t *testing.T) {
	var lines []string
	for range 200 {
		lines = append(lines, strings.Split(relexSamples["x.go"], "\n")...)
	}
	passes := make(chan []Line, 8)
	var h *Highlighter
	h = New(Lexer("x.go"), func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		passes <- h.lines
	})
	defer h.Close()
	src := newSource(lines, 1)
	h.Request(src, 1000, 1040)
	first := <-passes
	if len(first) >= len(lines) || len(first) < 41 {
		t.Fatalf("first pass of %d lines, of %d", len(first), len(lines))
	}
	if all := <-passes; len(all) != len(lines) {
		t.Fatalf("second pass of %d lines, of %d", len(all), len(lines))
	}
	if !slices.Equal(h.Line(1010, lines[1010]), lexRange(Lexer("x.go"), src, 0, len(lines))[1010].Tokens) {
		t.Error("tokens differ from lexing all")
	}
}

func TestLanguageNames(t *testing.T) {
	for name, want := range map[string]string{
		"go.mod":   "Plain Text",
		"main.go":  "Go",
		"q.sql":    "SQL",
		"Brewfile": "Ruby",
		".npmrc":   "INI",
		"Makefile": "Makefile",
	} {
		if got := DisplayName(Lexer(name)); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
}

// A large file scrolled has the lines below the view lexed again before
// they show.
func TestHighlighterLexesAhead(t *testing.T) {
	var lines []string
	for len(lines) <= WholeFile {
		lines = append(lines, strings.Split(relexSamples["x.go"], "\n")...)
	}
	passes := make(chan struct{}, 8)
	h := New(Lexer("x.go"), func() { passes <- struct{}{} })
	defer h.Close()
	pass := func() {
		select {
		case <-passes:
		case <-time.After(5 * time.Second):
			t.Fatal("no pass")
		}
	}
	src := newSource(lines, 1)
	h.Request(src, 1000, 1040)
	pass()
	h.mu.Lock()
	end := h.first + len(h.lines) - 1
	h.mu.Unlock()
	// The view's last line nearer the end of those lexed than Ahead.
	last := end - Ahead + 1
	h.Request(src, last-40, last)
	pass()
	h.mu.Lock()
	defer h.mu.Unlock()
	if got := h.first + len(h.lines) - 1; got < last+Ahead {
		t.Errorf("lexed up to line %d, not %d past the view's %d", got, Ahead, last)
	}
}
