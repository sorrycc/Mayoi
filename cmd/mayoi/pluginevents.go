package main

import (
	"strings"
	"time"

	"mayoi/internal/workspace"
)

// pluginSeen is what the plugins were last told of a window: each
// document's version and saved version, the document in front, and the
// folder.
type pluginSeen struct {
	docs    map[*document]docSeen
	focused *document
	root    string
	indexed bool
	// changed are the documents edited since the plugins were last told,
	// which they are told of once the edits rest.
	changed   map[*document]bool
	changedAt time.Time
	waking    bool
}

type docSeen struct{ version, saved uint64 }

// pluginChangeDelay is how long edits rest before the plugins hear of
// them: editor.change is not of every keystroke.
const pluginChangeDelay = 300 * time.Millisecond

// docEvent is what an event of a document tells a plugin.
func docEvent(d *document) map[string]any {
	return map[string]any{"path": d.path, "language": languageOf(d), "version": d.buf.Version(), "document": docJSON(d)}
}

// pluginTick tells the plugins what changed in the window since the last
// frame: documents opened, edited, saved and closed, the one in front, and
// the folder; and starts those whose activation names them.
func (a *app) pluginTick() {
	h := plugins
	if h == nil {
		return
	}
	if a == h.front() {
		if f := h.folderSnap.Load(); f == nil || *f != a.root {
			root := a.root
			h.folderSnap.Store(&root)
		}
	}
	s := &a.pluginSeen
	if s.docs == nil {
		s.docs, s.changed = map[*document]docSeen{}, map[*document]bool{}
	}
	if a.root != s.root {
		s.root, s.indexed = a.root, false
		h.reg.Emit("workspace.changed", map[string]any{"root": a.root})
	}
	if !s.indexed && a.root != "" && a.index == nil && wantsWorkspace(h) {
		a.refreshIndex()
	}
	if !s.indexed && a.root != "" && len(a.index) > 0 {
		s.indexed = true
		index := a.index
		h.reg.ActivateWorkspace(func(glob string) bool {
			g := workspace.ParseGlobs(glob)
			for _, f := range index {
				if g.Match(f) {
					return true
				}
			}
			return false
		})
	}
	now := time.Now()
	for _, d := range a.docs {
		seen, ok := s.docs[d]
		ver, saved := d.buf.Version(), d.saved
		switch {
		case !ok:
			h.reg.Activate("onLanguage:" + languageOf(d))
			h.reg.Emit("editor.open", docEvent(d))
			a.lspDoc("open", d)
		case seen.version != ver:
			s.changed[d], s.changedAt = true, now
		}
		if ok && seen.saved != saved && !d.dirty() {
			h.reg.Emit("editor.didSave", docEvent(d))
			a.lspDoc("save", d)
		}
		s.docs[d] = docSeen{ver, saved}
	}
	for d := range s.docs {
		if !containsDoc(a.docs, d) {
			delete(s.docs, d)
			delete(s.changed, d)
			h.reg.Emit("editor.close", map[string]any{"path": d.path})
			a.lspDoc("close", d)
		}
	}
	if len(s.changed) > 0 {
		if now.Sub(s.changedAt) >= pluginChangeDelay {
			for d := range s.changed {
				h.reg.Emit("editor.change", docEvent(d))
				a.lspDoc("change", d)
			}
			clear(s.changed)
		} else if !s.waking {
			s.waking = true
			time.AfterFunc(pluginChangeDelay, func() {
				a.post(func() { s.waking = false })
			})
		}
	}
	var front *document
	if d := a.activeEditor(); d != nil {
		front = d.document
	}
	if front != s.focused {
		s.focused = front
		if front != nil {
			h.reg.Activate("onLanguage:" + languageOf(front))
			h.reg.Emit("editor.focus", docEvent(front))
		}
	}
}

// wantsWorkspace reports whether a plugin starts on the files of the
// folder, which the index lists.
func wantsWorkspace(h *pluginHost) bool {
	for _, p := range h.enabled() {
		for _, a := range p.Activation {
			if strings.HasPrefix(a, "workspaceContains:") && !p.Started() {
				return true
			}
		}
	}
	return false
}

func containsDoc(docs []*document, d *document) bool {
	for _, x := range docs {
		if x == d {
			return true
		}
	}
	return false
}
