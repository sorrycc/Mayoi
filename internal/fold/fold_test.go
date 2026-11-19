package fold

import (
	"slices"
	"strings"
	"testing"
)

func find(lang string, lines ...string) []Range {
	return Find(For(lang), strings.Join(lines, "\n"))
}

func check(t *testing.T, what string, got []Range, want ...Range) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

// A JSON object and array fold to the line before their closing bracket,
// nested; one closed on the next line has nothing to fold.
func TestJSONNested(t *testing.T) {
	check(t, "json", find("JSON",
		`{`,
		`  "a": {`,
		`    "b": [`,
		`      1,`,
		`      2`,
		`    ],`,
		`    "c": {}`,
		`  },`,
		`  "d": [`,
		`  ]`,
		`}`,
	), Range{0, 9}, Range{1, 6}, Range{2, 4})
}

// Brackets in strings and comments are not brackets.
func TestFalseDelimiters(t *testing.T) {
	check(t, "json strings", find("JSON",
		`{`,
		`  "a": "{[",`,
		`  "b": "\"}",`,
		`  "c": 1`,
		`}`,
	), Range{0, 3})
	check(t, "go", find("Go",
		`func f() {`,
		`	s := "}"`,
		"	r := `{",
		"	}`",
		`	c := '}'`,
		`	// }`,
		`	/* { */`,
		`	x := 1`,
		`}`,
	), Range{0, 7})
	check(t, "js", find("JavaScript",
		`function f() {`,
		"  const t = `${a + {b: 1}.b} }`",
		`  const r = /[}]/g`,
		`  const s = '}'`,
		`  return a / b`,
		`}`,
	), Range{0, 4})
	check(t, "python", find("Python",
		`def f():`,
		`    s = "{"`,
		`    # }`,
		`    t = """`,
		`  {`,
		`"""`,
		`    return s`,
	), Range{0, 6}, Range{3, 5})
}

// Go, TypeScript and Python fold their blocks; a block's closing line
// stays.
func TestLanguages(t *testing.T) {
	check(t, "go", find("Go",
		`package p`,
		``,
		`import (`,
		`	"fmt"`,
		`	"os"`,
		`)`,
		``,
		`func f(x int) {`,
		`	if x > 0 {`,
		`		fmt.Println(x)`,
		`		os.Exit(1)`,
		`	} else {`,
		`		return`,
		`	}`,
		`}`,
	), Range{2, 4}, Range{7, 13}, Range{8, 10}, Range{11, 12})
	check(t, "ts", find("TypeScript",
		`interface A {`,
		`  a: string;`,
		`  b: number;`,
		`}`,
		`const f = (a: A) => {`,
		`  return [`,
		`    a.a,`,
		`    a.b,`,
		`  ];`,
		`};`,
	), Range{0, 2}, Range{4, 8}, Range{5, 7})
	check(t, "python", find("Python",
		`class A:`,
		`    def f(self):`,
		`        x = {`,
		`            "a": 1,`,
		`            "b": 2,`,
		`        }`,
		``,
		`        return x`,
		``,
		`def g():`,
		`    pass`,
		``,
	), Range{0, 7}, Range{1, 7}, Range{2, 4}, Range{9, 10})
	check(t, "rust lifetimes", find("Rust",
		`impl<'a> Foo<'a> {`,
		`    fn f(&'a self) -> char {`,
		`        '}'`,
		`    }`,
		`}`,
	), Range{0, 3}, Range{1, 2})
}

// Markdown folds a fenced block to its closing fence and a section to its
// last line, a fence in it hiding a heading.
func TestMarkdown(t *testing.T) {
	check(t, "markdown", find("markdown",
		`# A`,
		`text`,
		`## B`,
		"```go",
		`# not a heading`,
		"```",
		``,
		`# C`,
		`end`,
	), Range{0, 5}, Range{2, 5}, Range{3, 5}, Range{7, 8})
}

// Text half typed folds what closes, and a stray closer breaks nothing.
func TestMalformed(t *testing.T) {
	check(t, "unclosed", find("JSON",
		`{`,
		`  "a": [`,
		`    1,`,
		`    2`,
		`  ],`,
		`  "b": {`,
		`    "c": 1`,
	), Range{1, 3})
	check(t, "stray", find("Go",
		`func f() {`,
		`	)`,
		`	x := 1`,
		`}`,
	), Range{0, 2})
	check(t, "unclosed string", find("JSON",
		`{`,
		`  "a": "open,`,
		`  "b": 1`,
		`}`,
	), Range{0, 2})
	if Find(For("JSON"), strings.Repeat("{", 10)) != nil && len(Find(For("JSON"), strings.Repeat("{", 10))) != 0 {
		t.Error("a line of openers folds")
	}
}

// Text in no known language folds by indent.
func TestPlainIndent(t *testing.T) {
	check(t, "plain", find("",
		`a`,
		`  b`,
		`  c`,
		``,
		`d`,
	), Range{0, 2})
}

func BenchmarkJSON(b *testing.B) {
	var s strings.Builder
	s.WriteString("[\n")
	for range 20000 {
		s.WriteString("  {\n    \"name\": \"x{}\",\n    \"list\": [1, 2, 3],\n    \"o\": {\n      \"k\": true\n    }\n  },\n")
	}
	s.WriteString("]\n")
	text := s.String()
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		Find(For("JSON"), text)
	}
}
