package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
	"mayoi/locales"
)

type pluginTextOptions struct {
	Text, Title, Language string
	WindowID              uint64              `json:"windowId"`
	Beside                *plugin.DocumentRef `json:"beside"`
	Key                   string              `json:"key"`
}

func (h *pluginHost) openPluginText(p *plugin.Plugin, generation uint64, raw json.RawMessage) (any, error) {
	var opts pluginTextOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil, err
	}
	if opts.Beside != nil {
		if opts.Beside.WindowID == 0 || opts.Beside.DocumentID <= 0 {
			return nil, errors.New("invalid source document")
		}
		if opts.WindowID != 0 && opts.WindowID != opts.Beside.WindowID {
			return nil, errors.New("output and source must share a window")
		}
		opts.WindowID = opts.Beside.WindowID
	}
	if opts.Key != "" && p == nil {
		return nil, errors.New("reusable output needs a plugin owner")
	}
	return h.inWindow(opts.WindowID, func(a *app) (any, error) {
		if p != nil && p.Generation() != generation {
			return nil, errors.New("plugin output expired")
		}
		return nil, a.openPluginText(p, opts)
	})
}

func (a *app) pluginOutputColumn(ref *plugin.DocumentRef) (*column, error) {
	if ref == nil {
		return a.fileColumn(), nil
	}
	doc, err := docByID(a, ref.DocumentID)
	if err != nil {
		return nil, err
	}
	source := a.column()
	if tab := source.tab(); tab == nil || tab.ed == nil || tab.ed.document != doc {
		source = nil
		for _, col := range a.cols {
			if col.showing(doc) >= 0 {
				source = col
				break
			}
		}
	}
	if source == nil {
		return nil, errors.New("source document has no open tab")
	}
	// The flat column order also includes panes stacked below the source.
	parent := source.node.parent
	at := parent.at(source.node)
	if !parent.down && at+1 < len(parent.kids) {
		if right := parent.kids[at+1].col; right != nil && !right.locked {
			return right, nil
		}
	}
	return a.splitColumn(source, true), nil
}

func (a *app) openPluginText(owner *plugin.Plugin, opts pluginTextOptions) error {
	language := strings.ToLower(opts.Language)
	lexer := lexers.Get(language)
	if language != "" && language != "plaintext" && lexer == nil {
		return fmt.Errorf("unknown language %q", opts.Language)
	}
	var doc *document
	if opts.Key != "" {
		for _, d := range a.docs {
			if d.outputOwner == owner.Name && d.outputKey == opts.Key && d.path == "" {
				doc = d
				break
			}
		}
	}
	if doc != nil && doc.dirty() {
		return errors.New("output has unsaved edits; close or undo them before replacing it")
	}
	col, err := a.pluginOutputColumn(opts.Beside)
	if err != nil {
		return err
	}
	if doc == nil {
		a.untitled++
		a.addUntitled(col, a.untitled, opts.Text)
		doc = col.tabs[len(col.tabs)-1].ed.document
		if opts.Key != "" {
			doc.outputOwner, doc.outputKey = owner.Name, opts.Key
		}
	} else {
		doc.buf.Set(opts.Text)
	}
	doc.title = opts.Title
	if doc.title == "" {
		doc.title = locales.UntitledName(doc.number)
	}
	doc.saved, doc.savedSnap = doc.buf.Version(), doc.buf.Snapshot()
	if language != "" {
		doc.language = language
		doc.hl.Close()
		doc.hl = highlight.New(lexer, a.invalidate)
	}
	for _, c := range a.cols {
		if i := c.showing(doc); i >= 0 {
			a.moveTab(c.tabs[i], col, len(col.tabs))
			return nil
		}
	}
	return nil
}
