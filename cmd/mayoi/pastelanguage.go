package main

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"

	"mayoi/internal/keymap"
)

func (a *app) detectPastedLanguage(d *document, text string) {
	text = strings.TrimSpace(text)
	if text == "" || d.buf.Snapshot().Len() != 0 || d.language != "" || d.markdown {
		return
	}
	if d.hl.Lexer() != nil || pluginLanguage(d.path) != "" {
		return
	}
	var language string
	data := []byte(text)
	jsonc := bytes.TrimSpace(keymap.StripJSONC(data))
	switch {
	case json.Valid(data):
		if data[0] != '{' && data[0] != '[' {
			return
		}
		language = "json"
	case json.Valid(jsonc):
		if jsonc[0] != '{' && jsonc[0] != '[' {
			return
		}
		language = "jsonc"
	default:
		if lexer := lexers.Analyse(text); lexer != nil {
			language = lexer.Config().Name
		}
	}
	if language != "" {
		a.setDocumentLanguage(d, nil, language)
		d.pastedLanguage = true
	}
}
