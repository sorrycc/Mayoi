package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/alecthomas/chroma/v2"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
)

// resolvePluginLanguage finds a language by its name: one the plugin
// declares, with its lexer if it has one, or else one of Mayoi's own,
// "plaintext" for one neither knows.
func resolvePluginLanguage(owner *plugin.Plugin, name string) (string, chroma.Lexer, *plugin.Plugin) {
	name = strings.ToLower(name)
	if owner != nil {
		for _, lang := range owner.Contributes.Languages {
			if strings.EqualFold(lang.ID, name) {
				return name, owner.Lexer(name), owner
			}
		}
	}
	if lexer := highlight.ByName(name); lexer != nil {
		return name, lexer, nil
	}
	return "plaintext", nil, nil
}

func (a *app) setDocumentLanguage(d *document, owner *plugin.Plugin, name string) {
	d.pastedLanguage = false
	language, lexer, owner := resolvePluginLanguage(owner, name)
	d.language, d.languageOwner, d.lexerOwner = language, "", owner
	if owner != nil {
		d.languageOwner = owner.Name
	}
	d.hl.Close()
	d.hl = highlight.New(lexer, a.invalidate)
}

// clearDocumentLanguage has a document's language found by its file
// again, as it was before one was set.
func (a *app) clearDocumentLanguage(d *document) {
	d.pastedLanguage = false
	if d.language == "" {
		return
	}
	d.language, d.languageOwner, d.lexerOwner = "", "", nil
	var lexer chroma.Lexer
	if p, language := pluginLanguageOf(d.path); language != nil {
		d.lexerOwner, lexer = p, p.Lexer(language.ID)
	}
	if lexer == nil {
		lexer = autoLexer(d)
	}
	d.hl.Close()
	d.hl = highlight.New(lexer, a.invalidate)
}

// restoreDocumentLanguage sets the language a session kept for a
// document. One of a plugin not loaded yet is kept by name, for the
// plugin's lexer once it loads; one Mayoi no longer knows is left out.
func (a *app) restoreDocumentLanguage(d *document, language, owner string) {
	if owner == "" {
		if language == "plaintext" || highlight.ByName(language) != nil {
			a.setDocumentLanguage(d, nil, language)
		}
		return
	}
	if plugins != nil {
		for _, p := range plugins.enabled() {
			if p.Name == owner {
				a.setDocumentLanguage(d, p, language)
				return
			}
		}
	}
	d.language, d.languageOwner, d.lexerOwner = strings.ToLower(language), owner, nil
	d.hl.Close()
	d.hl = highlight.New(nil, a.invalidate)
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
			lexer = highlight.ByName(d.language)
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
