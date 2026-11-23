package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
)

type codeLens struct {
	ID      string `json:"id"`
	Line    int    `json:"line"`
	Title   string `json:"title"`
	Command string `json:"command"`
	Arg     string `json:"arg,omitempty"`
}

type codeLensBatch struct {
	owner               *plugin.Plugin
	generation, version uint64
	items               []codeLens
}

func (d *document) currentLenses() []codeLensBatch {
	d.lenses = slices.DeleteFunc(d.lenses, func(b codeLensBatch) bool {
		return b.version != d.buf.Version() || b.generation != b.owner.Generation()
	})
	return d.lenses
}

func (h *pluginHost) setCodeLenses(p *plugin.Plugin, generation uint64, method string, raw json.RawMessage) (any, error) {
	var args struct {
		Ref     plugin.DocumentRef `json:"ref"`
		Version *uint64            `json:"version"`
		Items   []codeLens         `json:"items"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if p == nil || generation == 0 || args.Ref.WindowID == 0 {
		return nil, errors.New("code lenses need an open document and plugin runtime")
	}
	return h.inWindow(args.Ref.WindowID, func(a *app) (any, error) {
		if p.Generation() != generation {
			return false, nil
		}
		d, err := docByID(a, args.Ref.DocumentID)
		if err != nil {
			return false, nil
		}
		if method == "languages.clearCodeLenses" {
			d.lenses = slices.DeleteFunc(d.lenses, func(b codeLensBatch) bool { return b.owner == p })
			return true, nil
		}
		if args.Version == nil {
			return nil, errors.New("code lenses need the snapshot version")
		}
		if *args.Version != d.buf.Version() {
			return false, nil
		}
		if err := h.validateCodeLenses(p, d, args.Items); err != nil {
			return nil, err
		}
		d.lenses = slices.DeleteFunc(d.lenses, func(b codeLensBatch) bool { return b.owner == p })
		if len(args.Items) > 0 {
			d.lenses = append(d.lenses, codeLensBatch{p, generation, *args.Version, args.Items})
		}
		return true, nil
	})
}

func (h *pluginHost) validateCodeLenses(p *plugin.Plugin, d *document, items []codeLens) error {
	ids := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || ids[item.ID] || item.Line < 0 || item.Line >= d.buf.Snapshot().LineCount() || item.Title == "" {
			return errors.New("code lenses need unique IDs, titles and valid lines")
		}
		if h.reg.CommandPlugin(item.Command) != p {
			return fmt.Errorf("command %q does not belong to this plugin", item.Command)
		}
		ids[item.ID] = true
	}
	return nil
}

func (a *app) runCodeLens(d *document, batch codeLensBatch, item codeLens) {
	if plugins == nil || d.buf.Version() != batch.version || batch.owner.Generation() != batch.generation {
		return
	}
	if current, err := docByID(a, d.id); err != nil || current != d {
		return
	}
	if !slices.ContainsFunc(d.currentLenses(), func(current codeLensBatch) bool {
		return current.owner == batch.owner && current.generation == batch.generation && slices.Contains(current.items, item)
	}) {
		return
	}
	origin := a.pluginContext("codeLens", d)
	origin.Position = &struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	}{Line: item.Line}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, err := batch.owner.RequestGeneration(ctx, batch.generation, "command", map[string]any{"id": item.Command, "arg": item.Arg, "context": origin})
		if err != nil {
			a.post(func() { a.fail("%s", errorLine(err)) })
		}
	}()
}

func (a *app) codeLensGaps(d *editor, g geometry) {
	for _, batch := range d.currentLenses() {
		for _, item := range batch.items {
			if !g.hidden(item.Line) && g.gaps[item.Line] == 0 {
				g.gaps[item.Line] = a.font.lineH
			}
		}
	}
}

func (a *app) drawCodeLenses(c *ui.Context, d *editor, g geometry, textX, height float32) {
	if len(g.gaps) == 0 {
		return
	}
	ui.Box(c.Key("code-lenses")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Clip().PassThrough().Children(func() {
		for line := range g.gaps {
			y := editorPadY + g.top(line) - g.gaps[line] - d.vscroll.Y
			if y < float32(len(d.sticky))*a.font.lineH || y >= height {
				continue
			}
			ui.Row(c.Key(fmt.Sprint("lens-line:", line))).Absolute().Left(textX).Top(y).Height(a.font.lineH).Gap(8).Children(func() {
				a.codeLensButtons(c, d, line)
			})
		}
	})
}

func (a *app) codeLensButtons(c *ui.Context, d *editor, line int) {
	for _, batch := range d.currentLenses() {
		for _, item := range batch.items {
			if item.Line != line {
				continue
			}
			// A press must expire when its document, runtime, or command changes.
			key := fmt.Sprintf("%p:%d:%d:%s:%s:%s", batch.owner, batch.generation, batch.version, item.ID, item.Command, item.Arg)
			button := ui.ButtonBase(c.Key(key)).Height(a.font.lineH).Label(item.Title).Cursor(ui.CursorPointer)
			button.Children(func() { ui.Text(c, item.Title).FontSize(a.font.size * 0.85).TextColor(a.pal.muted) })
			if button.Clicked() {
				a.runCodeLens(d.document, batch, item)
			}
		}
	}
}
