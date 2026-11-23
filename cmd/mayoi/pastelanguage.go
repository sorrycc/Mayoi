package main

import (
	"encoding/json"

	"github.com/alecthomas/chroma/v2/lexers"

	"mayoi/internal/keymap"
)

func (a *app) detectPastedLanguage(d *document, text string) {
	if text == "" || d.buf.Snapshot().Len() != 0 || d.language != "" || d.markdown {
		return
	}
	if d.hl.Lexer() != nil || pluginLanguage(d.path) != "" {
		return
	}
	var language string
	data := []byte(text)
	switch {
	case json.Valid(data):
		language = "json"
	case json.Valid(keymap.StripJSONC(data)):
		language = "jsonc"
	default:
		if lexer := lexers.Analyse(text); lexer != nil {
			language = lexer.Config().Name
		}
	}
	if language != "" {
		a.setDocumentLanguage(d, nil, language)
	}
}
