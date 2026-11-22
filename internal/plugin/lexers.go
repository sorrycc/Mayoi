package plugin

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func (p *Plugin) loadLexers() error {
	for _, language := range p.Contributes.Languages {
		if language.LexerFile == "" {
			continue
		}
		lexer, err := chroma.NewXMLLexer(p.fsys, language.LexerFile)
		if err == nil {
			_, err = lexer.Rules()
		}
		if err != nil {
			return fmt.Errorf("language %q: %w", language.ID, err)
		}
		if p.lexers == nil {
			p.lexers = map[string]chroma.Lexer{}
		}
		// Keep plugin grammars private; Using can still reuse the built-in lexers.
		p.lexers[strings.ToLower(language.ID)] = chroma.Coalesce(lexer.SetRegistry(lexers.GlobalLexerRegistry))
	}
	return nil
}

// Lexer resolves a language declared by this plugin without registering global aliases.
func (p *Plugin) Lexer(id string) chroma.Lexer {
	if p == nil {
		return nil
	}
	for _, language := range p.Contributes.Languages {
		if !strings.EqualFold(language.ID, id) {
			continue
		}
		if language.LexerFile != "" {
			return p.lexers[strings.ToLower(id)]
		}
		return lexers.Get(language.Lexer)
	}
	return nil
}
