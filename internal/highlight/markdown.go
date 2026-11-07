package highlight

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// markToken is the token of ==highlighted== text, a type Chroma's own
// Markdown lexer does not make.
const markToken = chroma.GenericUnderline

// markdown is the lexer of Markdown files, made once: Chroma's, with its
// emphasis, strikethrough and ==highlight== found by the flanking rule the
// preview uses, so that marks between CJK characters, with no space
// before them, count.
var markdown = sync.OnceValue(func() chroma.Lexer {
	// The registry finds the lexers of fenced code blocks.
	l := chroma.MustNewLexer(lexers.Get("markdown").Config(), markdownRules).SetRegistry(lexers.GlobalLexerRegistry)
	return chroma.Coalesce(&markdownLexer{l})
})

// markdownLexer lexes a YAML front matter at the top of a file as YAML,
// as Chroma's Markdown lexer does, and the rest as Markdown.
type markdownLexer struct{ chroma.Lexer }

func (m *markdownLexer) Tokenise(options *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	front, rest, ok := splitFrontMatter(text)
	yaml := lexers.Get("yaml")
	if !ok || yaml == nil {
		return m.Lexer.Tokenise(options, text)
	}
	head, err := yaml.Tokenise(options, front)
	if err != nil {
		return nil, err
	}
	body, err := m.Lexer.Tokenise(options, rest)
	if err != nil {
		return nil, err
	}
	return chroma.Concaterator(head, body), nil
}

// splitFrontMatter splits a front matter between --- lines off the top of
// a file.
func splitFrontMatter(text string) (front, rest string, ok bool) {
	first, after, found := strings.Cut(text, "\n")
	if !found || strings.TrimSuffix(first, "\r") != "---" {
		return "", text, false
	}
	pos := len(first) + 1
	for {
		line, next, found := strings.Cut(after, "\n")
		if !found {
			return "", text, false
		}
		pos += len(line) + 1
		if strings.TrimSuffix(line, "\r") == "---" {
			return text[:pos], text[pos:], true
		}
		after = next
	}
}

// markdownRules are Chroma's Markdown rules but for the inline marks: a
// run opens where it is not followed by a space and closes where it does
// not follow one, whatever is around it, and _ does not mark within a
// word. Only ~~ strikes and only == highlights, not ~~~ or ===, so that
// a == b stays text.
func markdownRules() chroma.Rules {
	return chroma.Rules{
		"root": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `^(#[^#].+\n)`, Type: chroma.ByGroups(chroma.GenericHeading)},
			{Pattern: `^(#{2,6}.+\n)`, Type: chroma.ByGroups(chroma.GenericSubheading)},
			{Pattern: `^(\s*)([*-] )(\[[ xX]\])( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([*-])(\s)(.+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Text, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([0-9]+\.)( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*>\s)(.+\n)`, Type: chroma.ByGroups(chroma.Keyword, chroma.GenericEmph)},
			{Pattern: "^(```\\n)([\\w\\W]*?)(^```$)", Type: chroma.ByGroups(chroma.String, chroma.Text, chroma.String)},
			{Pattern: "^(```)(\\w+)(\\n)([\\w\\W]*?)(^```$)", Type: chroma.UsingByGroup(2, 4, chroma.String, chroma.String, chroma.String, chroma.Text, chroma.String)},
			chroma.Include("inline"),
		},
		"inline": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `\\.`, Type: chroma.Text},
			{Pattern: `\*\*(?=[^\s*])(?:[^*\n]|\*(?!\*))*?(?<=[^\s*])\*\*`, Type: chroma.GenericStrong},
			{Pattern: `(?<!\w)__(?=[^\s_])(?:[^_\n]|_(?!_))*?(?<=[^\s_])__(?!\w)`, Type: chroma.GenericStrong},
			{Pattern: `\*(?=[^\s*])[^*\n]*?(?<=[^\s*])\*`, Type: chroma.GenericEmph},
			{Pattern: `(?<!\w)_(?=[^\s_])[^_\n]*?(?<=[^\s_])_(?!\w)`, Type: chroma.GenericEmph},
			{Pattern: `(?<!~)~~(?=[^\s~])[^~\n]*?(?<=[^\s~])~~(?!~)`, Type: chroma.GenericDeleted},
			{Pattern: `(?<!=)==(?=[^\s=])(?:[^=\n]|=(?!=))*?(?<=[^\s=])==(?!=)`, Type: markToken},
			{Pattern: "`[^`]+`", Type: chroma.LiteralStringBacktick},
			{Pattern: `[@#][\w/:]+`, Type: chroma.NameEntity},
			{Pattern: `(!?\[)([^]]+)(\])(\()([^)]+)(\))`, Type: chroma.ByGroups(chroma.Text, chroma.NameTag, chroma.Text, chroma.Text, chroma.NameAttribute, chroma.Text)},
			{Pattern: `.|\n`, Type: chroma.Text},
		},
	}
}
