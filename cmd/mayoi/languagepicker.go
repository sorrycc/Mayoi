package main

import (
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
	"mayoi/locales"
)

type languageChoice struct {
	id, label string
	owner     *plugin.Plugin
}

func documentLanguages() []languageChoice {
	choices := map[string]languageChoice{
		"plaintext": {id: "plaintext", label: "Plain Text"},
		"jsonc":     {id: "jsonc", label: "JSONC"},
	}
	for _, name := range lexers.Names(false) {
		lexer := lexers.Get(name)
		id := strings.ToLower(strings.ReplaceAll(name, " ", ""))
		if lexers.Get(id) == nil {
			id = strings.ToLower(name)
		}
		choices[id] = languageChoice{id: id, label: highlight.DisplayName(lexer)}
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
	list := make([]languageChoice, 0, len(choices))
	for _, choice := range choices {
		list = append(list, choice)
	}
	slices.SortFunc(list, func(a, b languageChoice) int { return strings.Compare(a.label, b.label) })
	return list
}

func (a *app) openLanguagePicker() {
	ed := a.activeEditor()
	if ed == nil {
		return
	}
	d := ed.document
	list := documentLanguages()
	items := make([]pickItem, len(list))
	for i, choice := range list {
		detail := choice.id
		if choice.owner != nil {
			detail += " · " + choice.owner.Title()
		}
		items[i] = pickItem{Label: choice.label, Description: detail, Icon: "code"}
	}
	a.openPluginPicker(items, locales.CmdEditorChangeLanguage(), func(i int) {
		if i >= 0 && containsDoc(a.docs, d) {
			choice := list[i]
			a.setDocumentLanguage(d, choice.owner, choice.id)
		}
	})
	a.pluginPick.icon = iconFileCode
}

func documentLanguageName(d *document) string {
	if d.language == "jsonc" {
		return "JSONC"
	}
	if d.language != "" && d.language != "plaintext" && d.hl.Lexer() == nil {
		return d.language
	}
	return highlight.DisplayName(d.hl.Lexer())
}
