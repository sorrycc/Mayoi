// Package fold finds the runs of lines of a text that an editor can fold:
// the lines between an opening bracket and its closing one, those indented
// more than the line above them, and a Markdown document's fenced code and
// sections. It reads the text as a whole and knows the strings and comments
// of a language well enough that brackets in them do not count. Text half
// typed or not well formed folds as far as it reads.
package fold

import (
	"slices"
	"strings"
)

// Range is a run of lines that folds: Start shows, and the lines after it
// up to End hide. End is after Start.
type Range struct {
	Start, End int
}

// MaxBytes is the longest text Find reads: a longer one has no folds.
const MaxBytes = 16 << 20

// Lang says how a language's text folds.
type Lang struct {
	// Line starts a comment to the end of the line, Block a comment up
	// to its second string.
	Line  []string
	Block [][2]string
	// Quotes are the quotes of strings; such a string ends at the end of
	// its line unless MultiQuotes. Raw are those of raw strings, with no
	// escapes, and Long those of strings over lines with escapes, as
	// Python's triple quotes.
	Quotes      string
	MultiQuotes bool
	Raw         []string
	Long        []string
	// Template is set when a backtick quotes a string over lines with
	// ${...} in it, as in JavaScript.
	Template bool
	// Chars is set when a ' quotes a character, as 'x' or '\n', and is
	// otherwise not a quote, as a lifetime is in Rust.
	Chars bool
	// Regex is set when a / where a value goes starts a regular
	// expression, as in JavaScript.
	Regex bool
	// Brackets fold from an opening bracket to its closing one, Indent by
	// how far lines are indented, and Markdown by fences and headings.
	Brackets, Indent, Markdown bool
}

var (
	cLike  = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"`, Chars: true, Brackets: true}
	jsLike = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"'`, Template: true, Regex: true, Brackets: true}
	json   = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"`, Brackets: true}
	goLang = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"`, Raw: []string{"`"}, Chars: true, Brackets: true}
	rust   = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"`, MultiQuotes: true, Chars: true, Brackets: true}
	css    = Lang{Block: [][2]string{{"/*", "*/"}}, Quotes: `"'`, Brackets: true}
	scss   = Lang{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `"'`, Brackets: true}
	python = Lang{Line: []string{"#"}, Quotes: `"'`, Long: []string{`"""`, `'''`}, Brackets: true, Indent: true}
	hashes = Lang{Line: []string{"#"}, Quotes: `"'`, Indent: true}
	toml   = Lang{Line: []string{"#"}, Quotes: `"'`, Long: []string{`"""`, `'''`}, Brackets: true}
	sql    = Lang{Line: []string{"--"}, Block: [][2]string{{"/*", "*/"}}, Quotes: `'"`, Brackets: true, Indent: true}
	lua    = Lang{Line: []string{"--"}, Quotes: `"'`, Indent: true}
	plain  = Lang{Indent: true}
	md     = Lang{Markdown: true}
)

// langs are the languages by the names of their lexers in Chroma.
var langs = map[string]Lang{
	"JSON": json, "JSON5": json, "JSONC": json,
	"Go":         goLang,
	"JavaScript": jsLike, "TypeScript": jsLike, "TSX": jsLike, "react": jsLike, "Svelte": jsLike, "QML": jsLike,
	"C": cLike, "C++": cLike, "C#": cLike, "Java": cLike, "Kotlin": cLike, "Swift": cLike, "Scala": cLike,
	"Objective-C": cLike, "Dart": cLike, "Groovy": cLike, "PHP": cLike, "Zig": cLike, "Protocol Buffer": cLike,
	"GLSL": cLike, "HLSL": cLike, "Solidity": cLike, "Thrift": cLike, "Terraform": cLike, "HCL": cLike,
	"Rust": rust,
	"CSS":  css, "SCSS": scss, "LessCss": scss, "Sass": hashes,
	"Python": python, "Python 2": python, "Starlark": python,
	"YAML": hashes, "Ruby": hashes, "Bash": hashes, "Zsh": hashes, "Fish": hashes, "Perl": hashes, "Elixir": hashes,
	"Nim": hashes, "R": hashes, "PowerShell": hashes, "Makefile": hashes, "Docker": hashes, "CMake": hashes,
	"TOML":     toml,
	"SQL":      sql,
	"Lua":      lua,
	"markdown": md, "Markdown": md,
}

// For returns how the language of a lexer of Chroma named name folds:
// by indent for one it does not know.
func For(name string) Lang {
	if l, ok := langs[name]; ok {
		return l
	}
	return plain
}

// Find returns the runs of lines of text that fold in lang, ordered by
// their first lines, at most one from each line: the longest. The runs
// nest, or are apart.
func Find(lang Lang, text string) []Range {
	if len(text) > MaxBytes {
		return nil
	}
	ends := map[int]int{}
	add := func(start, end int) {
		if end > start && end > ends[start] {
			ends[start] = end
		}
	}
	var cont []bool
	if lang.Brackets || lang.Line != nil || lang.Block != nil || lang.Long != nil || lang.Raw != nil {
		cont = scan(lang, text, add)
	}
	if lang.Indent {
		// A run the brackets found from a line is the line's: its
		// closing bracket shows.
		indents(text, cont, func(start, end int) {
			if _, ok := ends[start]; !ok {
				add(start, end)
			}
		})
	}
	if lang.Markdown {
		markdown(text, add)
	}
	out := make([]Range, 0, len(ends))
	for s, e := range ends {
		out = append(out, Range{s, e})
	}
	slices.SortFunc(out, func(a, b Range) int { return a.Start - b.Start })
	return nest(out)
}

// nest drops the runs that cross one before them, keeping the outer, so
// that the runs left nest.
func nest(rs []Range) []Range {
	var open []Range
	out := rs[:0]
	for _, r := range rs {
		for len(open) > 0 && open[len(open)-1].End < r.Start {
			open = open[:len(open)-1]
		}
		if len(open) > 0 && r.End > open[len(open)-1].End {
			continue
		}
		open = append(open, r)
		out = append(out, r)
	}
	return out
}

// opener is a bracket open, on line, or the ${ of a template.
type opener struct {
	ch       byte
	line     int
	template bool
}

// scan reads text in lang, calling add for each run between brackets over
// lines and each comment of more than one line. It returns, of each line,
// whether it starts within a string, a comment or brackets, as a line
// continuing the one before.
func scan(lang Lang, text string, add func(start, end int)) []bool {
	cont := []bool{false}
	var stack []opener
	line := 0
	n := len(text)
	// last is the last byte of code before i that is not a space, for a
	// / to tell a regular expression from a division.
	var last byte
	lastWord := ""
	inTemplate := false
	newline := func() {
		line++
		cont = append(cont, len(stack) > 0 || inTemplate)
	}
	// skipTo moves past the first end at or after i, counting lines, and
	// returns where it stopped: n when there is none.
	skipTo := func(i int, end string, escapes bool) int {
		for i < n {
			c := text[i]
			switch {
			case c == '\n':
				newline()
				cont[line] = true
			case escapes && c == '\\':
				i++
				if i < n && text[i] == '\n' {
					newline()
					cont[line] = true
				}
			case strings.HasPrefix(text[i:], end):
				return i + len(end)
			}
			i++
		}
		return n
	}
	i := 0
	for i < n {
		c := text[i]
		if inTemplate {
			// Within a template's text, up to its end or a ${.
			switch {
			case c == '\n':
				newline()
				cont[line] = true
			case c == '\\':
				i++
				if i < n && text[i] == '\n' {
					newline()
					cont[line] = true
				}
			case c == '`':
				inTemplate = false
				last = '`'
			case c == '$' && i+1 < n && text[i+1] == '{':
				inTemplate = false
				stack = append(stack, opener{'{', line, true})
				last = '{'
				i++
			}
			i++
			continue
		}
		switch c {
		case '\n':
			newline()
			i++
			continue
		case ' ', '\t', '\r':
			i++
			continue
		}
		if comment := startsAny(text[i:], lang.Line); comment != "" {
			for i < n && text[i] != '\n' {
				i++
			}
			continue
		}
		if b := startsBlock(text[i:], lang.Block); b[0] != "" {
			from := line
			i = skipTo(i+len(b[0]), b[1], false)
			// A comment over lines folds to its last line.
			add(from, line)
			continue
		}
		if q := startsAny(text[i:], lang.Long); q != "" {
			i = skipTo(i+len(q), q, true)
			last = '"'
			continue
		}
		if q := startsAny(text[i:], lang.Raw); q != "" {
			i = skipTo(i+len(q), q, false)
			last = '"'
			continue
		}
		if lang.Template && c == '`' {
			inTemplate = true
			i++
			continue
		}
		if c == '\'' && lang.Chars {
			// A character, 'x' or '\n', or not a quote at all.
			if end := charEnd(text, i); end > 0 {
				i, last = end, '\''
				continue
			}
			i++
			continue
		}
		if strings.IndexByte(lang.Quotes, c) >= 0 {
			i = stringEnd(text, i, lang.MultiQuotes, func() { newline(); cont[line] = true })
			last = '"'
			continue
		}
		if c == '/' && lang.Regex && regexAfter(last, lastWord) {
			if end := regexEnd(text, i); end > 0 {
				i, last = end, '/'
				continue
			}
		}
		if lang.Brackets {
			switch c {
			case '{', '[', '(':
				stack = append(stack, opener{c, line, false})
			case '}', ']', ')':
				open := byte('(')
				switch c {
				case '}':
					open = '{'
				case ']':
					open = '['
				}
				// The opener it closes: one or two left open within it, as
				// in a line half typed, are given up.
				k := len(stack) - 1
				for k >= 0 && k >= len(stack)-3 && stack[k].ch != open {
					k--
				}
				if k >= 0 && stack[k].ch == open {
					o := stack[k]
					stack = stack[:k]
					add(o.line, line-1)
					if o.template {
						inTemplate = true
						last = 0
						i++
						continue
					}
				}
			}
		}
		if isWord(c) {
			j := i
			for j < n && isWord(text[j]) {
				j++
			}
			lastWord, last = text[i:j], 'a'
			i = j
			continue
		}
		last, lastWord = c, ""
		i++
	}
	return cont
}

func startsAny(s string, prefixes []string) string {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return p
		}
	}
	return ""
}

func startsBlock(s string, blocks [][2]string) [2]string {
	for _, b := range blocks {
		if strings.HasPrefix(s, b[0]) {
			return b
		}
	}
	return [2]string{}
}

// stringEnd returns where the string quoted at i ends: after its closing
// quote, or at the end of its line unless multi, calling nl for each line
// it goes over.
func stringEnd(text string, i int, multi bool, nl func()) int {
	q := text[i]
	n := len(text)
	for i++; i < n; i++ {
		switch text[i] {
		case '\\':
			if i+1 < n && text[i+1] == '\n' {
				if !multi {
					return i + 1
				}
				nl()
			}
			i++
		case '\n':
			if !multi {
				return i
			}
			nl()
		case q:
			return i + 1
		}
	}
	return n
}

// charEnd returns where the character quoted at i ends, as 'x', '\n' or
// '\u{1F600}', or 0 where the ' quotes none.
func charEnd(text string, i int) int {
	n := len(text)
	j := i + 1
	if j < n && text[j] == '\\' {
		for k := j + 2; k < n && k < j+12 && text[k] != '\n'; k++ {
			if text[k] == '\'' {
				return k + 1
			}
		}
		return 0
	}
	// One rune, then the closing quote.
	if j < n && text[j] != '\'' && text[j] != '\n' {
		k := j + 1
		for k < n && text[k]&0xC0 == 0x80 {
			k++
		}
		if k < n && text[k] == '\'' {
			return k + 1
		}
	}
	return 0
}

// regexAfter reports whether a / after the code byte last, or the word
// lastWord, starts a regular expression rather than divides.
func regexAfter(last byte, lastWord string) bool {
	if lastWord != "" {
		switch lastWord {
		case "return", "typeof", "case", "in", "of", "delete", "void", "throw", "new", "do", "else", "yield", "await":
			return true
		}
		return false
	}
	return last == 0 || strings.IndexByte("(,=:[!&|?{};+-*%<>~^", last) >= 0
}

// regexEnd returns where the regular expression at i ends, after its flags,
// or 0 where it does not end on its line.
func regexEnd(text string, i int) int {
	n := len(text)
	if i+1 < n && (text[i+1] == '/' || text[i+1] == '*') {
		return 0
	}
	class := false
	for j := i + 1; j < n; j++ {
		switch text[j] {
		case '\\':
			j++
		case '\n':
			return 0
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				j++
				for j < n && isWord(text[j]) {
					j++
				}
				return j
			}
		}
	}
	return 0
}

func isWord(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// indentTab is how wide a tab counts in an indent.
const indentTab = 4

// indents calls add for each line followed by lines indented more than it,
// up to the last of them that is not blank. Lines that continue the line
// before (cont), within its brackets or a string, count with it.
func indents(text string, cont []bool, add func(start, end int)) {
	type level struct{ line, width int }
	var stack []level
	lastText := -1
	line := 0
	for start := 0; start <= len(text); line++ {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}
		s := text[start:end]
		start = end + 1
		width, blank := indentOf(s)
		switch {
		case blank:
		case line < len(cont) && cont[line]:
			lastText = line
		default:
			for len(stack) > 0 && stack[len(stack)-1].width >= width {
				add(stack[len(stack)-1].line, lastText)
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, level{line, width})
			lastText = line
		}
		if end == len(text) {
			break
		}
	}
	for _, l := range stack {
		add(l.line, lastText)
	}
}

// indentOf returns how wide the indent of line s is, and whether s is
// blank.
func indentOf(s string) (int, bool) {
	w := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			w++
		case '\t':
			w += indentTab - w%indentTab
		case '\r':
		default:
			return w, false
		}
	}
	return w, true
}

// markdown calls add for each fenced block, from its opening fence to its
// closing one, and each section, from its heading to the last line before
// the next heading as high or higher that is not blank.
func markdown(text string, add func(start, end int)) {
	type heading struct{ line, level int }
	var open []heading
	fence, fenceLine := "", 0
	lastText := -1
	line := 0
	for start := 0; start <= len(text); line++ {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}
		s := strings.TrimRight(text[start:end], "\r")
		start = end + 1
		t := strings.TrimLeft(s, " ")
		indent := len(s) - len(t)
		switch {
		case fence != "":
			if indent < 4 && strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				add(fenceLine, line)
				fence = ""
			}
			lastText = line
		case indent < 4 && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")):
			k := 0
			for k < len(t) && t[k] == t[0] {
				k++
			}
			fence, fenceLine, lastText = t[:k], line, line
		case indent < 4 && strings.HasPrefix(t, "#"):
			k := 0
			for k < len(t) && t[k] == '#' {
				k++
			}
			if k <= 6 && (k == len(t) || t[k] == ' ' || t[k] == '\t') {
				for len(open) > 0 && open[len(open)-1].level >= k {
					add(open[len(open)-1].line, lastText)
					open = open[:len(open)-1]
				}
				open = append(open, heading{line, k})
			}
			lastText = line
		case strings.TrimSpace(t) != "":
			lastText = line
		}
		if end == len(text) {
			break
		}
	}
	for _, h := range open {
		add(h.line, lastText)
	}
}
