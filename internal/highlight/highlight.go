// Package highlight colors source code with Chroma's lexers, off the main
// thread, a window of lines at a time.
//
// A Highlighter keeps the tokens of the lines it lexed last, each with the
// text it was lexed from: a line whose text changed since shows no colors
// until the next pass lexes it again, so that stale tokens never color the
// wrong runes.
package highlight

import (
	"embed"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Kind is the class of a token, which a palette colors.
type Kind uint8

const (
	Plain Kind = iota
	Keyword
	Type
	Function
	String
	Number
	Comment
	Operator
	Punctuation
	Constant
	Builtin
	Tag
	Attribute
	Variable
	Heading
	Deleted
	Inserted
	kinds
)

// Kinds is the number of kinds, for tables indexed by Kind.
const Kinds = int(kinds)

// Token is a run of one kind in a line, in runes from the line's start.
type Token struct {
	Start, End int
	Kind       Kind
}

// Line is a lexed line: the text it was lexed from and its tokens, Plain
// ones left out.
type Line struct {
	Text   string
	Tokens []Token
	// open is whether the line ends inside a token that goes on to the
	// next, as a string or a comment over lines does.
	open bool
}

// MaxLineRunes is the longest line lexed: longer ones, as in minified
// files, show plain.
const MaxLineRunes = 4000

// vueXML is Chroma's Vue lexer with its v- directive values ending at
// their closing quote: Chroma's own run on, over lines, to a later quote,
// as in a value prettier wraps.
//
//go:embed vue.xml
var vueXML embed.FS

// vue is the lexer of .vue files, made once.
var vue = sync.OnceValue(func() chroma.Lexer {
	return chroma.Coalesce(chroma.MustNewXMLLexer(vueXML, "vue.xml"))
})

// Lexer returns the lexer of a file by its name, nil for plain text.
func Lexer(path string) chroma.Lexer {
	name := filepath.Base(path)
	// Names Chroma gets wrong, or does not know: go.mod is not AMPL, .sql
	// is any SQL, not MySQL's, and .vue needs the fixed lexer above.
	switch strings.ToLower(name) {
	case "go.mod", "go.work", "go.sum":
		return nil
	case "brewfile":
		return chroma.Coalesce(lexers.Get("ruby"))
	case ".npmrc":
		return chroma.Coalesce(lexers.Get("ini"))
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sql":
		return chroma.Coalesce(lexers.Get("sql"))
	case ".vue":
		return vue()
	}
	l := lexers.Match(name)
	if l == nil {
		switch strings.ToLower(name) {
		case "makefile", "gnumakefile":
			l = lexers.Get("make")
		case "dockerfile":
			l = lexers.Get("docker")
		}
	}
	if l == nil {
		return nil
	}
	return chroma.Coalesce(l)
}

// Name returns the lexer's name to show, "Plain Text" for nil.
func Name(l chroma.Lexer) string {
	if l == nil {
		return "Plain Text"
	}
	return l.Config().Name
}

// displayNames are the names to show of lexers whose own are lower case.
var displayNames = map[string]string{
	"markdown":      "Markdown",
	"plaintext":     "Plain Text",
	"properties":    "Properties",
	"react":         "React",
	"vue":           "Vue",
	"dns":           "DNS",
	"verilog":       "Verilog",
	"systemverilog": "SystemVerilog",
	"reg":           "Registry",
}

// DisplayName returns the name of the lexer's language as a status bar
// shows it, which Name keeps as Chroma has it, for comparisons.
func DisplayName(l chroma.Lexer) string {
	name := Name(l)
	if d, ok := displayNames[name]; ok {
		return d
	}
	return name
}

// Lex lexes text, whole lines, into a Line for each line of it.
func Lex(l chroma.Lexer, text string) []Line {
	lines, _ := lex(l, text, nil)
	return lines
}

// lex lexes text as Lex does, calling done with the index of each line as
// its tokens are complete, and stops as soon as done returns true. It
// returns the lines, and the line it stopped at or -1.
//
// Chroma lexes as it is asked for tokens, so stopping early saves the rest
// of the work, while the lines up to there come out as from lexing all of
// it: a token that goes on past them is lexed as such.
func lex(l chroma.Lexer, text string, done func(row int, lines []Line) bool) ([]Line, int) {
	lines := make([]Line, 0, strings.Count(text, "\n")+1)
	for s := range strings.SplitSeq(text, "\n") {
		lines = append(lines, Line{Text: s})
	}
	if l == nil {
		return lines, -1
	}
	it, err := l.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, text)
	if err != nil {
		return lines, -1
	}
	// complete ends a line: longer ones, as in minified files, show plain.
	complete := func(row int) bool {
		if row >= len(lines) {
			return false
		}
		if ln := &lines[row]; len(ln.Tokens) > 0 && utf8.RuneCountInString(ln.Text) > MaxLineRunes {
			ln.Tokens = nil
		}
		return done != nil && done(row, lines)
	}
	row, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		kind := classify(tok.Type)
		v := tok.Value
		for v != "" {
			nl := strings.IndexByte(v, '\n')
			part := v
			if nl >= 0 {
				part = v[:nl]
			}
			n := utf8.RuneCountInString(part)
			if kind != Plain && n > 0 && row < len(lines) {
				ln := &lines[row]
				if k := len(ln.Tokens); k > 0 && ln.Tokens[k-1].Kind == kind && ln.Tokens[k-1].End == col {
					ln.Tokens[k-1].End += n
				} else {
					ln.Tokens = append(ln.Tokens, Token{Start: col, End: col + n, Kind: kind})
				}
			}
			col += n
			if nl < 0 {
				break
			}
			if row < len(lines) && nl < len(v)-1 && tok.Type != chroma.Text && tok.Type != chroma.TextWhitespace {
				lines[row].open = true
			}
			if complete(row) {
				return lines, row
			}
			row, col = row+1, 0
			v = v[nl+1:]
		}
	}
	for ; row < len(lines); row++ {
		if complete(row) {
			return lines, row
		}
	}
	return lines, -1
}

func classify(t chroma.TokenType) Kind {
	switch {
	case t == chroma.KeywordType:
		return Type
	case t == chroma.KeywordConstant:
		return Constant
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return Function
	case t == chroma.NameClass || t == chroma.NameNamespace:
		return Type
	case t == chroma.NameBuiltin || t == chroma.NameBuiltinPseudo:
		return Builtin
	case t == chroma.NameTag:
		return Tag
	case t == chroma.NameAttribute || t == chroma.NameDecorator:
		return Attribute
	case t == chroma.NameConstant || t == chroma.NameEntity:
		return Constant
	case t == chroma.NameVariable || t == chroma.NameVariableInstance || t == chroma.NameVariableGlobal || t == chroma.NameVariableClass:
		return Variable
	case t == chroma.NameOther || t == chroma.NameProperty:
		return Plain
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	case t == chroma.Literal || t == chroma.LiteralDate:
		return Constant
	case t.InCategory(chroma.Comment):
		return Comment
	case t.InCategory(chroma.Operator):
		return Operator
	case t == chroma.Punctuation:
		return Punctuation
	case t == chroma.GenericHeading || t == chroma.GenericSubheading:
		return Heading
	case t == chroma.GenericDeleted:
		return Deleted
	case t == chroma.GenericInserted:
		return Inserted
	case t == chroma.GenericStrong || t == chroma.GenericEmph:
		return Keyword
	}
	return Plain
}

// Source is what a Highlighter lexes: the text of lines first up to last
// of a document at a version. A ui.TextSnapshot satisfies it.
type Source interface {
	Version() uint64
	LineCount() int
	LineRange(line int) (int, int)
	Slice(start, end int) string
}

// WholeFile is the most lines lexed in one pass from the start: longer
// documents are lexed around the lines in view, with Context lines above,
// which multi-line comments and strings mostly start within.
//
// Ahead is how many lines past those in view must be lexed already: a
// view scrolling nearer the end of the lines lexed has them lexed again
// around it before it gets there, so that no line shows plain on the way.
const (
	WholeFile = 6000
	Context   = 400
	Below     = 200
	Ahead     = 60
)

// Quiet is how long after the last edit the lines lexed again only around
// the edits are all lexed again, for the rare lexer whose state at a line
// the tokens around it do not tell.
var Quiet = 300 * time.Millisecond

// Highlighter lexes a document in the background and keeps the tokens of
// the lines it lexed last. Its methods are safe from any goroutine.
type Highlighter struct {
	lexer  chroma.Lexer
	notify func()
	kick   chan struct{} // a request came while the worker waits

	mu      sync.Mutex
	version uint64 // the version lexed
	gen     uint64 // counts the passes: the tokens are the same while it is
	first   int    // the line Lines[0] is
	lines   []Line
	// shift is how many lines the text asked for last has more than the
	// one lexed, which the lines below an edit have moved by.
	shift   int
	total   int // the lines of the text lexed
	running bool
	want    request
	closed  bool
}

type request struct {
	src         Source
	first, last int
	// view is the lines in view, first and last, which a first pass over
	// many lines lexes before the rest.
	view [2]int
}

// New makes a highlighter of lexer, which calls notify, from another
// goroutine, when it has new tokens.
func New(lexer chroma.Lexer, notify func()) *Highlighter {
	return &Highlighter{lexer: lexer, notify: notify, kick: make(chan struct{}, 1)}
}

// Lexer returns the highlighter's lexer.
func (h *Highlighter) Lexer() chroma.Lexer { return h.lexer }

// Close stops the highlighter's work.
func (h *Highlighter) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

// Request asks for the tokens of lines first up to last of src, lexed in
// the background unless the last pass has them. It returns at once.
func (h *Highlighter) Request(src Source, first, last int) {
	if h == nil || h.lexer == nil {
		return
	}
	n := src.LineCount()
	from, to := 0, n-1
	if n > WholeFile {
		from, to = max(0, first-Context), min(n-1, last+Below)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if h.version == src.Version() && h.lines != nil && h.first <= max(0, first-Ahead) && h.first+len(h.lines)-1 >= min(n-1, last+Ahead) {
		return
	}
	h.want = request{src, from, to, [2]int{first, last}}
	h.shift = n - h.total
	select {
	case h.kick <- struct{}{}:
	default:
	}
	if !h.running {
		h.running = true
		go h.run()
	}
}

func (h *Highlighter) run() {
	// check is the last pass that lexed lines only around the edits: once
	// the edits stop for a while, all its lines are lexed again, and those
	// that came out otherwise are put right.
	var check request
	for {
		h.mu.Lock()
		r := h.want
		h.want = request{}
		if h.closed || r.src == nil && check.src == nil {
			h.running = false
			h.mu.Unlock()
			return
		}
		prev, prevFirst := h.lines, h.first
		h.mu.Unlock()

		full := r.src == nil
		if full {
			select {
			case <-h.kick:
				continue // asked for more meanwhile
			case <-time.After(Quiet):
			}
			r, check = check, request{}
			h.mu.Lock()
			stale := h.version != r.src.Version() || h.first != r.first
			h.mu.Unlock()
			if stale {
				continue
			}
		}
		var lines []Line
		var over patch   // lines written over prev's, rather than new ones
		var rest request // the lines left for after the first in view
		if !full && prev == nil && r.last-r.first > 2*(Context+Below) {
			// Opened: the lines around the view first, the rest after.
			rest = r
			r.first, r.last = max(r.first, r.view[0]-Context), min(r.last, r.view[1]+Below)
			lines = lexRange(h.lexer, r.src, r.first, r.last+1)
		} else if !full && prev != nil && prevFirst == r.first {
			p := relexPatch(h.lexer, r, prev)
			switch {
			case !p.exact:
				check = r
			case p.same:
				check = request{}
			}
			switch {
			case p.same:
				lines = prev
			case p.inPlace():
				// A keystroke within a line: its lines go over the old
				// ones, under the lock, rather than into a copy of them
				// all. Only this goroutine writes h.lines.
				lines, over = prev, p
			default:
				lines = p.apply(prev)
			}
		} else {
			lines = lexRange(h.lexer, r.src, r.first, r.last+1)
			check = request{}
			if full && len(lines) == len(prev) && alike(lines, prev) {
				continue // as lexed around the edits
			}
		}

		h.mu.Lock()
		copy(lines[over.from:], over.lines)
		h.version, h.first, h.lines = r.src.Version(), r.first, lines
		h.total, h.shift = r.src.LineCount(), 0
		if w := h.want; w.src != nil {
			h.shift = w.src.LineCount() - h.total
		}
		h.gen++
		if w := h.want; w.src != nil && w.src.Version() == h.version && w.first >= h.first && w.last < h.first+len(lines) {
			h.want = request{} // asked again for what this pass lexed
		}
		if h.want.src == nil && rest.src != nil {
			h.want = rest
		}
		h.mu.Unlock()
		if h.notify != nil {
			h.notify()
		}
	}
}

// Gen returns a number that changes whenever the highlighter has new
// tokens: what was built from Line's results holds while it is the same.
func (h *Highlighter) Gen() uint64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gen
}

// Line returns the tokens of line i, if the last pass lexed it from text.
//
// While a pass is to come, a line moved by an edit above it keeps its
// tokens, and so does a line edited by one insertion or deletion: those
// before the edit stay, those after it move with the text, and one the
// edit falls inside grows or shrinks. One the edit touches shows plain.
func (h *Highlighter) Line(i int, text string) []Token {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	j := i - h.first
	if j >= 0 && j < len(h.lines) && h.lines[j].Text == text {
		return h.lines[j].Tokens
	}
	if k := j - h.shift; h.shift != 0 && k >= 0 && k < len(h.lines) && h.lines[k].Text == text {
		return h.lines[k].Tokens
	}
	if j < 0 || j >= len(h.lines) {
		return nil
	}
	return adjust(h.lines[j], text)
}

// adjust moves the tokens of a line lexed from l.Text onto text, when
// text is l.Text with one run of runes inserted or deleted.
func adjust(l Line, text string) []Token {
	if len(l.Tokens) == 0 || len(text) > MaxLineRunes || len(l.Text) > MaxLineRunes {
		return nil
	}
	from, to := []rune(l.Text), []rune(text)
	p := 0
	for p < len(from) && p < len(to) && from[p] == to[p] {
		p++
	}
	s := 0
	for s < len(from)-p && s < len(to)-p && from[len(from)-1-s] == to[len(to)-1-s] {
		s++
	}
	// From p, the run [p, p+del) of the old text became [p, p+ins).
	del, ins := len(from)-p-s, len(to)-p-s
	if del > 0 && ins > 0 {
		return nil // a replacement, which no token is known to cover
	}
	out := make([]Token, 0, len(l.Tokens))
	for _, t := range l.Tokens {
		switch {
		case t.End <= p && (t.End < p || del > 0):
			out = append(out, t)
		case t.Start >= p+del && (t.Start > p || del > 0):
			out = append(out, Token{t.Start + ins - del, t.End + ins - del, t.Kind})
		case t.Start < p && p+del < t.End:
			out = append(out, Token{t.Start, t.End + ins - del, t.Kind})
		}
	}
	return out
}

// requestText returns the text of the lines of request r, without the
// newline after the last.
func requestText(r request) string {
	start, _ := r.src.LineRange(r.first)
	_, stop := r.src.LineRange(r.last)
	return r.src.Slice(start, stop)
}

// lexRange lexes lines first up to end of src.
func lexRange(l chroma.Lexer, src Source, first, end int) []Line {
	start, _ := src.LineRange(first)
	_, stop := src.LineRange(end - 1)
	return Lex(l, src.Slice(start, stop))
}

// relexAhead is how many lines below a change relex first gives the
// lexer, and relexMargin how far above where the text was cut its lexing
// must settle to be kept.
const relexAhead, relexMargin = 160, 32

// relex lexes the lines of request r again, given those of the same lines
// of an earlier text: only the lines that changed, and around them.
//
// Chroma's lexers cannot start in the state a line starts in, so lexing
// starts some way above the change, as at the start of the text, at a
// line outside any token over lines, and is kept only if the lines with
// tokens between come out as they did: else it starts further up. Below
// the change, it goes on until a few lines with tokens come out as they
// did, ending outside any token over lines, after which the earlier
// tokens hold.
//
// It reports whether the lines are surely as lexing all of them makes
// them: when they are those of the earlier text, or lexed from the start.
func relex(l chroma.Lexer, r request, prev []Line) ([]Line, bool) {
	p := relexPatch(l, r, prev)
	if p.same {
		return prev, true
	}
	return p.apply(prev), p.exact
}

// patch is what relex changes in the earlier lines: lines takes the place
// of prev[from:to], or none when same.
type patch struct {
	from, to int
	lines    []Line
	same     bool
	// exact is relex's report: the lines are as lexing all of them makes
	// them.
	exact bool
}

// apply returns the lines of prev patched, new ones, leaving prev as it is.
func (p patch) apply(prev []Line) []Line {
	out := append(slices.Clip(prev[:p.from]), p.lines...)
	return append(out, prev[p.to:]...)
}

// inPlace reports whether the patch keeps the number of lines, so that it
// can be written over them.
func (p patch) inPlace() bool { return !p.same && len(p.lines) == p.to-p.from }

// relexPatch does relex's work, returning the lines it changes.
func relexPatch(l chroma.Lexer, r request, prev []Line) patch {
	n := r.last - r.first + 1
	// The lines are read from the text at once: reading each from it
	// again finds its place over again.
	all := requestText(r)
	at := make([]int, n+1) // where each line starts in all
	for i, rest := 1, all; i <= n; i++ {
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			for ; i <= n; i++ {
				at[i] = len(all) + 1
			}
			break
		}
		at[i] = at[i-1] + nl + 1
		rest = rest[nl+1:]
	}
	text := func(i int) string { return all[min(at[i], len(all)) : at[i+1]-1] }
	a := 0
	for a < len(prev) && a < n && prev[a].Text == text(a) {
		a++
	}
	if a == len(prev) && a == n {
		return patch{same: true, exact: true}
	}
	b := 0 // the lines alike at the end
	for b < len(prev)-a && b < n-a && prev[len(prev)-1-b].Text == text(n-1-b) {
		b++
	}
	shift := n - len(prev)
	const settle = 3 // the lines with tokens alike that end the lexing
	for back := 16; ; back *= 4 {
		s := max(0, a-back)
		for s > 0 && prev[s-1].open {
			s--
		}
		lo := min(at[s], len(all))
		var lines []Line
		var wrong bool
		stop := -1
		// The text goes to the lexer as far as some lines below the change,
		// rather than to the end of the request: Chroma reads all the text
		// it is given before it lexes, and the lexing mostly settles a few
		// lines below. Lexing that settles too near where the text was cut
		// may have been misled by the cut, and goes on with more.
		for ahead := relexAhead; ; ahead *= 4 {
			end := min(n-1, n-b+ahead)
			hi := at[end+1] - 1
			seen, run := 0, 0
			wrong = false
			lines, stop = lex(l, all[lo:hi], func(k int, lines []Line) bool {
				ln := lines[k]
				switch i := s + k; {
				case i < a:
					// From the start of the text the state is known; from further
					// down, these lines tell whether it was guessed right.
					if s == 0 {
						return false
					}
					if len(ln.Tokens) > 0 {
						seen++
					}
					wrong = !slices.Equal(ln.Tokens, prev[i].Tokens) || ln.open != prev[i].open || i == a-1 && seen < 2
					return wrong
				case i >= n-b:
					switch old := prev[i-shift]; {
					case !slices.Equal(ln.Tokens, old.Tokens) || ln.open || old.open:
						run = 0
					case len(old.Tokens) > 0:
						run++
					}
					return run == settle
				}
				return false
			})
			if end == n-1 || !wrong && stop >= 0 && s+stop <= end-relexMargin {
				break
			}
			if wrong {
				// The cut may have misled the lines above as well, if a
				// token runs past it: they are checked with all the text,
				// before lexing starts further up.
				ahead = n
			}
		}
		if wrong {
			continue
		}
		if stop < 0 {
			return patch{from: s, to: len(prev), lines: lines, exact: s == 0}
		}
		return patch{from: s, to: s + stop - shift + 1, lines: lines[:stop+1]}
	}
}

// alike reports whether two runs of lines have the same tokens.
func alike(x, y []Line) bool {
	return slices.EqualFunc(x, y, func(a, b Line) bool { return slices.Equal(a.Tokens, b.Tokens) })
}
