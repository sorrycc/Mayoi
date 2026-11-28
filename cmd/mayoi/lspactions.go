package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/lsp"
	"mayoi/locales"
)

func (a *app) saveActions(d *document) []string {
	if d.path == "" {
		return nil
	}
	prefs, language := a.prefs(), languageOf(d)
	var kinds []string
	if prefs.OrganizeImportsOnSave[language] || prefs.OrganizeImportsOnSave["*"] {
		kinds = append(kinds, lsp.OrganizeImports)
	}
	if prefs.FixAllOnSave[language] || prefs.FixAllOnSave["*"] {
		kinds = append(kinds, lsp.FixAll)
	}
	return kinds
}

// prepareSave shares one deadline across code actions and the existing formatter.
func (a *app) prepareSave(d *editor, actions, format bool, then func()) {
	deadline := time.Now().Add(formatOnSaveLimit)
	var kinds []string
	if actions {
		kinds = a.saveActions(d.document)
	}
	a.runSaveActions(d, kinds, deadline, func(current bool) {
		remaining := time.Until(deadline)
		if current && format && remaining > 0 {
			a.format(d, remaining, true, then)
			return
		}
		then()
	})
}

func (a *app) runSaveActions(d *editor, kinds []string, deadline time.Time, then func(bool)) {
	if len(kinds) == 0 {
		then(true)
		return
	}
	kind := kinds[0]
	name := locales.SaveActionOrganizeImports()
	if kind == lsp.FixAll {
		name = locales.SaveActionFixAll()
	}
	failed := func(err error) {
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New(locales.SaveActionTimeout())
		}
		a.fail("%s", locales.SaveActionFailed(name, firstLine(err.Error())))
	}
	if time.Until(deadline) <= 0 {
		failed(context.DeadlineExceeded)
		then(true)
		return
	}
	s := a.lspFor(d.document, false)
	c := s.live()
	if c == nil || !c.CodeActionsSupported(kind) {
		failed(errors.New(locales.SaveActionUnavailable()))
		a.runSaveActions(d, kinds[1:], deadline, then)
		return
	}
	snap := d.buf.Snapshot()
	path, language, root := d.path, languageOf(d.document), a.root
	uri := serverURI(path)
	text := snap.String()
	bg.Go(func() {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		version, err := c.OpenContext(ctx, uri, language, text)
		var edits []lsp.TextEdit
		if err == nil {
			var actions []lsp.CodeAction
			actions, err = c.CodeActions(ctx, uri, kind)
			if err == nil {
				edits, err = lsp.SourceActionEdits(actions, kind, uri, version)
			}
		}
		a.post(func() {
			if d.buf.Version() != snap.Version() || d.path != path || languageOf(d.document) != language || a.root != root || !s.current() || d.locked() {
				then(false)
				return
			}
			if err == nil {
				err = validateSourceEdits(snap, edits)
			}
			if err != nil {
				failed(err)
			} else if len(edits) > 0 {
				data, _ := json.Marshal(lspEdits(edits))
				a.applyFormat(d, snap, data)
			}
			a.runSaveActions(d, kinds[1:], deadline, then)
		})
	})
}

func validateSourceEdits(snap ui.TextSnapshot, edits []lsp.TextEdit) error {
	var changes []textEdit
	for _, edit := range edits {
		start, end := posRune(snap, fromLSP(edit.Range.Start)), posRune(snap, fromLSP(edit.Range.End))
		if start > end || positionOf(snap, start) != fromLSP(edit.Range.Start) || positionOf(snap, end) != fromLSP(edit.Range.End) {
			return errors.New(locales.SaveActionInvalidEdits())
		}
		changes = append(changes, textEdit{start, end, edit.NewText})
	}
	if editsOverlap(changes) {
		return errors.New(locales.SaveActionInvalidEdits())
	}
	return nil
}
