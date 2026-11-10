package main

import (
	"errors"
	"slices"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// syncState is where a document is in reading and writing its file. Clean
// or changed is the buffer's to say, as dirty does.
type syncState int

const (
	syncIdle syncState = iota
	// syncLoading: a remote file is read in the background.
	syncLoading
	// syncLoadFailed: it could not be read, loadErr says why.
	syncLoadFailed
	// syncFormatting: a save waits for a plugin's formatter.
	syncFormatting
	// syncSaving: a remote file is written in the background.
	syncSaving
	// syncConflict: a remote file changed on its host since it was read,
	// and the save waits to be told whether to write over it.
	syncConflict
)

// errNotSaved answers a save that the person chose not to make, which
// says nothing.
var errNotSaved = errors.New("not saved")

// saveWaiter is a save asked for: done runs once the text as it was then
// is written, or failed with why it could not be. format runs the
// formatter first, as formatOnSave asks.
type saveWaiter struct {
	done   func()
	failed func(error)
	format bool
}

// requestSave saves a document, then answers w: at once when nothing is
// under way, or with the save after the formatter or the write under way,
// or the answer of a conflict's question.
func (a *app) requestSave(d *document, w saveWaiter) {
	switch d.sync {
	case syncLoading, syncLoadFailed:
		// Not read: there is no text to write, which says why.
		err := errNotSaved
		switch {
		case errors.Is(d.loadErr, remote.ErrOffline):
			err = errors.New(locales.ErrNotConnectedTo(remote.HostOf(d.path)))
		case d.sync == syncLoadFailed:
			err = d.loadErr
		}
		if w.failed != nil {
			w.failed(err)
		}
	case syncConflict:
		d.waiters = append(d.waiters, w)
	default:
		d.queued = append(d.queued, w)
		a.nextSave(d)
	}
}

// nextSave makes the saves queued once nothing is under way: the
// formatter first if one of them asks for it, then the write.
func (a *app) nextSave(d *document) {
	if d.sync != syncIdle || len(d.queued) == 0 {
		return
	}
	if slices.ContainsFunc(d.queued, func(w saveWaiter) bool { return w.format }) {
		if ed := a.docEditor(d); ed != nil {
			for i := range d.queued {
				d.queued[i].format = false
			}
			d.sync = syncFormatting
			a.format(ed, formatOnSaveLimit, true, func() {
				if d.sync == syncFormatting {
					d.sync = syncIdle
				}
				a.nextSave(d)
			})
			return
		}
	}
	d.waiters, d.queued = d.queued, nil
	if remote.Is(d.path) {
		a.writeRemote(d)
		a.nextSave(d)
		return
	}
	a.answerSaves(d, d.save())
	a.nextSave(d)
}

// answerSaves tells the saves waiting that they were made, or not, why.
func (a *app) answerSaves(d *document, err error) {
	ws := d.waiters
	d.waiters = nil
	for _, w := range ws {
		switch {
		case err == nil && w.done != nil:
			w.done()
		case err != nil && w.failed != nil:
			w.failed(err)
		}
	}
}
