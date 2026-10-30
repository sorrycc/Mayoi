package main

import (
	"slices"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// autoSaveDelay is how long after the last edit a changed file saves.
const autoSaveDelay = time.Second

// autoSave is a document's state for saving itself: the text and line
// endings its timer is for, and whether saving failed, which the app
// says once.
type autoSave struct {
	version uint64
	crlf    bool
	timer   *time.Timer
	failed  bool
}

// autoSaves reports whether a document saves itself: a file still on
// disk, while the setting is on.
func (a *app) autoSaves(d *document) bool {
	return a.prefs().AutoSave && d.path != "" && !d.diskGone && !d.locked()
}

// autoSaveTick follows the documents each frame: a changed one saves a
// moment after its last edit, or when the column that has the keyboard
// stops showing it.
func (a *app) autoSaveTick() {
	var shown *document
	if ed := a.activeEditor(); ed != nil {
		shown = ed.document
	}
	if prev := a.shownDoc; prev != nil && prev != shown {
		a.autoSaveDoc(prev)
	}
	a.shownDoc = shown
	if !a.prefs().AutoSave {
		return
	}
	for _, d := range a.docs {
		as := &d.autoSave
		v := d.buf.Version()
		if v == as.version && d.crlf == as.crlf {
			continue
		}
		as.version, as.crlf = v, d.crlf
		if as.timer != nil {
			as.timer.Stop()
		}
		if !d.dirty() || !a.autoSaves(d) {
			continue
		}
		crlf := d.crlf
		as.timer = time.AfterFunc(autoSaveDelay, func() {
			a.post(func() {
				if d.buf.Version() == v && d.crlf == crlf {
					a.autoSaveDoc(d)
				}
			})
		})
	}
}

// autoSaveDoc saves a changed document that saves itself, saying so once
// if it cannot.
func (a *app) autoSaveDoc(d *document) {
	if !a.autoSaves(d) || !d.dirty() || !slices.Contains(a.docs, d) {
		return
	}
	failed := func(err error) {
		if !d.autoSave.failed {
			d.autoSave.failed = true
			a.fail("%s", locales.FailSave(d.name(), err.Error()))
		}
	}
	if remote.Is(d.path) {
		a.saveRemote(d, func() { d.autoSave.failed = false }, failed)
		return
	}
	if err := d.save(); err != nil {
		failed(err)
		return
	}
	d.autoSave.failed = false
}

// autoSaveAll saves every changed document that saves itself, as the
// window loses the keyboard or closes.
func (a *app) autoSaveAll() {
	for _, d := range slices.Clone(a.docs) {
		a.autoSaveDoc(d)
	}
}

func (a *app) toggleAutoSave() {
	a.setPref("autoSave", func(p *settings.Prefs) { p.AutoSave = !p.AutoSave })
	if a.prefs().AutoSave {
		a.autoSaveAll()
	}
}
