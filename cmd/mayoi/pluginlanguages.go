package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
)

func resolvePluginLanguage(owner *plugin.Plugin, name string) (string, chroma.Lexer, *plugin.Plugin) {
	name = strings.ToLower(name)
	if lexer := owner.Lexer(name); lexer != nil {
		return name, lexer, owner
	}
	if lexer := lexers.Get(name); lexer != nil {
		return name, lexer, nil
	}
	return "plaintext", nil, nil
}

func (a *app) setDocumentLanguage(d *document, owner *plugin.Plugin, name string) {
	language, lexer, owner := resolvePluginLanguage(owner, name)
	d.language, d.languageOwner, d.lexerOwner = language, "", owner
	if owner != nil {
		d.languageOwner = owner.Name
	}
	d.hl.Close()
	d.hl = highlight.New(lexer, a.invalidate)
}

func (a *app) refreshDocumentLexer(d *document) {
	var owner *plugin.Plugin
	var lexer chroma.Lexer
	if d.language != "" {
		for _, p := range plugins.enabled() {
			if p.Name == d.languageOwner {
				owner = p
				lexer = p.Lexer(d.language)
				break
			}
		}
		if lexer == nil {
			lexer = lexers.Get(d.language)
		}
	} else if p, language := pluginLanguageOf(d.path); language != nil {
		owner, lexer = p, p.Lexer(language.ID)
	}
	if owner == d.lexerOwner && (owner == nil || d.hl.Lexer() == lexer) {
		return
	}
	d.lexerOwner = owner
	if lexer == nil && d.language == "" {
		lexer = highlight.Lexer(d.path)
	}
	d.hl.Close()
	d.hl = highlight.New(lexer, a.invalidate)
}

func (h *pluginHost) setDocumentLanguage(p *plugin.Plugin, generation uint64, raw json.RawMessage) (any, error) {
	var args struct {
		Ref      plugin.DocumentRef `json:"ref"`
		Language string             `json:"language"`
		Version  *uint64            `json:"version"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Ref.WindowID == 0 || args.Ref.DocumentID <= 0 {
		return nil, errors.New("invalid document reference")
	}
	return h.inWindow(args.Ref.WindowID, func(a *app) (any, error) {
		if p != nil && p.Generation() != generation {
			return false, nil
		}
		d, err := docByID(a, args.Ref.DocumentID)
		if err != nil || args.Version != nil && *args.Version != d.buf.Version() {
			return false, nil
		}
		a.setDocumentLanguage(d, p, args.Language)
		return true, nil
	})
}
