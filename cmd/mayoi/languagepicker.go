package main

import (
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
	"mayoi/locales"
)

// languageChoice is a row of the box of languages: Auto Detect, or a
// language by its id, of a plugin or of Mayoi's own.
type languageChoice struct {
	auto      bool
	id, label string
	owner     *plugin.Plugin
}

// documentLanguages are the languages a document can be set to: Plain
// Text first, then Mayoi's and the plugins' by name, a plugin's taking
// the place of Mayoi's of the same id.
func documentLanguages() []languageChoice {
	choices := map[string]languageChoice{
		"jsonc": {id: "jsonc", label: "JSONC"},
	}
	for _, l := range highlight.Languages() {
		id := highlight.ID(l)
		choices[id] = languageChoice{id: id, label: highlight.DisplayName(l)}
	}
	if plugins != nil {
		for _, p := range plugins.enabled() {
			for _, lang := range p.Contributes.Languages {
				id, label := strings.ToLower(lang.ID), lang.ID
				if builtin, ok := choices[id]; ok {
					label = builtin.label
				} else if lexer := p.Lexer(id); lexer != nil {
					label = highlight.DisplayName(lexer)
				}
				choices[id] = languageChoice{id: id, label: label, owner: p}
			}
		}
	}
	list := make([]languageChoice, 0, len(choices)+1)
	for _, choice := range choices {
		list = append(list, choice)
	}
	slices.SortFunc(list, func(a, b languageChoice) int {
		return strings.Compare(strings.ToLower(a.label), strings.ToLower(b.label))
	})
	return append([]languageChoice{{id: "plaintext", label: highlight.DisplayName(nil)}}, list...)
}

// openLanguagePicker shows the box of languages for the active editor's
// document, on the one it has now.
func (a *app) openLanguagePicker() {
	ed := a.activeEditor()
	if ed == nil {
		return
	}
	d := ed.document
	list := append([]languageChoice{{auto: true, label: locales.LanguageAutoDetect()}}, documentLanguages()...)
	items := make([]pickItem, len(list))
	current := 0
	for i, choice := range list {
		detail := choice.id
		if choice.auto {
			detail = autoLanguageName(d)
		} else if choice.owner != nil {
			detail += " · " + choice.owner.Title()
		}
		items[i] = pickItem{Label: choice.label, Description: detail}
		// A plugin's language takes the row of Mayoi's of its id, so the
		// id alone finds the row.
		if d.language != "" && !choice.auto && choice.id == d.language {
			current = i
		}
	}
	items[current].current = true
	a.openPluginPicker(items, locales.LanguagePickPlaceholder(), func(i int) {
		if i >= 0 && containsDoc(a.docs, d) {
			a.chooseLanguage(d, list[i])
		}
	})
	a.pluginPick.icon, a.pluginPick.initial = iconFileCode, current
}

// chooseLanguage sets a document's language, or for Auto Detect has its
// file name decide it again. Its text, selection and undo stay as they
// are, and the plugins' tick moves it to the new language's server.
func (a *app) chooseLanguage(d *document, ch languageChoice) {
	if ch.auto {
		a.clearDocumentLanguage(d)
	} else {
		a.setDocumentLanguage(d, ch.owner, ch.id)
	}
	a.invalidate()
}

// autoLexer is the lexer a document's file name gives it, as Auto Detect
// finds it: a plugin's, or else Mayoi's.
func autoLexer(d *document) chroma.Lexer {
	if p, l := pluginLanguageOf(d.path); l != nil {
		if lexer := p.Lexer(l.ID); lexer != nil {
			return lexer
		}
	}
	if d.path == "" && d.markdown {
		return highlight.Lexer("untitled.md")
	}
	return highlight.Lexer(d.path)
}

// autoLanguageName is the name of the language Auto Detect gives a
// document.
func autoLanguageName(d *document) string {
	if p, l := pluginLanguageOf(d.path); l != nil && p.Lexer(l.ID) == nil {
		return l.ID
	}
	return highlight.DisplayName(autoLexer(d))
}

// documentLanguageName is the name of a document's language as the
// status bar shows it.
func documentLanguageName(d *document) string {
	if d.language == "jsonc" {
		return "JSONC"
	}
	if d.language != "" && d.language != "plaintext" && d.hl.Lexer() == nil {
		return d.language
	}
	return highlight.DisplayName(d.hl.Lexer())
}
