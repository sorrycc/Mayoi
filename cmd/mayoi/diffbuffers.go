package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alecthomas/chroma/v2"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
	"mayoi/locales"
)

type bufferComparison struct {
	ids       [2]int
	names     [2]string
	languages [2]string
	closed    [2]bool
	snapshots [2]ui.TextSnapshot
	lexers    [2]chroma.Lexer
}

func (b *bufferComparison) label(side int) string {
	name := b.names[side]
	if b.closed[side] {
		name += " · " + locales.PrClosed()
	}
	return name
}

func newBufferCompareView(layout diffLayout, sources bufferComparison) *diffView {
	v := newDiffView(layout)
	v.local = true
	v.files = []*diffFile{{base: diffBuffers, buffers: &sources, gitStale: true}}
	v.near = [2]int{0, 1}
	return v
}

func (a *app) captureBuffer(f *diffFile, side int, d *document) bool {
	b := f.buffers
	snap, name, lang := d.buf.Snapshot(), d.name(), languageOf(d)
	changed := b.snapshots[side].Version() != snap.Version() || b.names[side] != name
	if b.languages[side] != lang {
		b.languages[side] = lang
		s := &f.old
		if side == 1 {
			s = &f.new
		}
		if s.hl != nil {
			s.hl.Close()
			s.hl = nil
		}
		changed = true
	}
	b.snapshots[side], b.names[side] = snap, name
	if d.hl != nil {
		b.lexers[side] = d.hl.Lexer()
	}
	if side == 0 {
		f.path = d.path
	} else {
		f.peerPath = d.path
	}
	return changed
}

func (a *app) captureBuffers(f *diffFile) {
	for side, id := range f.buffers.ids {
		d, err := docByID(a, id)
		if err != nil {
			if !f.buffers.closed[side] {
				f.buffers.closed[side], f.gitStale = true, true
			}
			continue
		}
		if a.captureBuffer(f, side, d) {
			f.gitStale = true
		}
	}
}

func (a *app) freezeBufferComparisons(d *document) {
	for _, col := range a.cols {
		for _, tab := range col.tabs {
			if tab.diff == nil {
				continue
			}
			if len(tab.diff.files) == 0 || tab.diff.files[0].buffers == nil {
				continue
			}
			f := tab.diff.files[0]
			for side, id := range f.buffers.ids {
				if id != d.id {
					continue
				}
				a.captureBuffer(f, side, d)
				f.buffers.closed[side], f.gitStale = true, true
			}
		}
	}
}

func (a *app) bufferVersionsCurrent(b *bufferComparison, versions [2]uint64) bool {
	for side, id := range b.ids {
		version := b.snapshots[side].Version()
		if d, err := docByID(a, id); err == nil {
			version = d.buf.Version()
		}
		if version != versions[side] {
			return false
		}
	}
	return true
}

func (a *app) openBufferSource(id, line int) {
	for _, col := range a.cols {
		for i, tab := range col.tabs {
			if tab.ed == nil || tab.ed.id != id {
				continue
			}
			a.activate(col, i, true)
			tab.ed.goToLine(max(0, line)+1, 1)
			return
		}
	}
}

func (h *pluginHost) showBufferDiff(raw json.RawMessage) (any, error) {
	var args struct {
		Left, Right plugin.DocumentRef
		Layout      string
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Left.WindowID == 0 || args.Left.WindowID != args.Right.WindowID {
		return nil, errors.New("compare documents in the same open window")
	}
	if args.Left.DocumentID == args.Right.DocumentID {
		return nil, errors.New("choose two different documents")
	}
	if args.Layout != "" && args.Layout != "auto" && args.Layout != "split" && args.Layout != "inline" {
		return nil, fmt.Errorf("unknown diff layout %q", args.Layout)
	}
	return h.inWindow(args.Left.WindowID, func(a *app) (any, error) {
		left, err := docByID(a, args.Left.DocumentID)
		if err != nil {
			return nil, err
		}
		right, err := docByID(a, args.Right.DocumentID)
		if err != nil {
			return nil, err
		}
		layout := parseDiffLayout(a.settings.DiffLayout)
		if args.Layout != "" {
			layout = parseDiffLayout(args.Layout)
		}
		v := newBufferCompareView(layout, bufferComparison{ids: [2]int{left.id, right.id}})
		a.captureBuffers(v.files[0])
		col := a.fileColumn()
		at := a.addTab(col, min(col.active+1, len(col.tabs)), &colTab{diff: v})
		a.activate(col, at, true)
		return nil, nil
	})
}

func bufferLexer(f *diffFile, side *diffSide) chroma.Lexer {
	if f.buffers == nil {
		return highlight.Lexer(f.path)
	}
	if side == &f.new {
		return f.buffers.lexers[1]
	}
	return f.buffers.lexers[0]
}
