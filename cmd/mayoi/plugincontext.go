package main

import (
	"errors"
	"sync"
	"sync/atomic"

	"mayoi/internal/plugin"
)

var pluginWindowSeq atomic.Uint64

func (a *app) pluginWindow() uint64 {
	if a.pluginWindowID == 0 {
		a.pluginWindowID = pluginWindowSeq.Add(1)
	}
	return a.pluginWindowID
}

func (a *app) documentRef(d *document) plugin.DocumentRef {
	return plugin.DocumentRef{WindowID: a.pluginWindow(), DocumentID: d.id}
}

func (h *pluginHost) window(id uint64) (*app, error) {
	if id == 0 {
		if a := h.front(); a != nil {
			return a, nil
		}
		return nil, errNoWindow
	}
	for _, a := range h.apps() {
		if a.pluginWindowID == id && !a.loadsClosed {
			return a, nil
		}
	}
	return nil, errors.New("the window is closed")
}

func (h *pluginHost) inWindow(id uint64, fn func(*app) (any, error)) (any, error) {
	return h.main(func(_ *app) (any, error) {
		a, err := h.window(id)
		if err != nil {
			return nil, err
		}
		v, err := fn(a)
		a.invalidate()
		return v, err
	})
}

func (h *pluginHost) waitInWindow(id uint64, start func(*app, func(any, error))) (any, error) {
	return h.wait(func(_ *app, done func(any, error)) {
		a, err := h.window(id)
		if err != nil {
			done(nil, err)
			return
		}
		if a.pluginCalls == nil {
			a.pluginCalls = map[uint64]func(){}
		}
		a.nextPluginCall++
		key := a.nextPluginCall
		var once sync.Once
		finish := func(v any, err error) {
			once.Do(func() {
				h.onMain(func() {
					delete(a.pluginCalls, key)
					done(v, err)
				})
			})
		}
		a.pluginCalls[key] = func() { finish(nil, errors.New("the window is closed")) }
		start(a, finish)
		a.invalidate()
	})
}

func (a *app) pluginContext(source string, d *document) *plugin.CommandContext {
	c := &plugin.CommandContext{Source: source, WindowID: a.pluginWindow()}
	if d != nil {
		ref, version := a.documentRef(d), d.buf.Version()
		c.Document, c.Version = &ref, &version
	}
	return c
}

func (a *app) closePluginWindow() {
	for _, cancel := range a.pluginCalls {
		cancel()
	}
	if plugins != nil {
		for _, d := range a.docs {
			plugins.reg.Emit("editor.close", map[string]any{"path": d.path, "ref": a.documentRef(d)})
		}
	}
}
