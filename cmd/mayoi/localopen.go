package main

import (
	"io/fs"
	"slices"
	"time"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// Local reads cannot be interrupted inside the filesystem. Keep at most
// two running per project; waiting requests live on their documents, so
// closing a tab removes its waiting work without a goroutine or queue.
const localReadLimit = 2

// localReadStuck is how long a read runs before it no longer holds its
// place, as one of a share that is gone, so that other files still open.
// It keeps running until the share answers.
const localReadStuck = 2 * time.Second

type localReader func(string, int64) (fs.FileInfo, []byte, error)

func (a *app) loadFile(d *document) {
	if remote.Is(d.path) {
		a.loadRemote(d)
		return
	}
	if d.sync == syncLoading || a.loadsClosed || d.dirty() {
		return
	}
	// Keep the previous error available while a retry is running.
	d.sync, d.localQueued, d.readingAt = syncLoading, true, time.Time{}
	read := a.localRead
	if read == nil {
		read = vfs.ReadFileInfo
	}
	a.startLocalReads(read)
}

// readDone answers what waits for the document's read, as it ends or the
// document closes.
func (d *document) readDone() {
	waits := d.readWaits
	d.readWaits = nil
	for _, w := range waits {
		w()
	}
}

func (a *app) startLocalReads(read localReader) {
	if a.loadsClosed {
		return
	}
	for _, d := range a.docs {
		if a.localReads >= localReadLimit {
			break
		}
		if !d.localQueued {
			continue
		}
		d.localQueued = false
		a.localReads++
		// held is whether the read still counts; both change it on the UI
		// thread, as the read ends or as it is stuck.
		held := true
		stuck := a.localStuck
		if stuck == 0 {
			stuck = localReadStuck
		}
		timer := time.AfterFunc(stuck, func() {
			a.post(func() {
				if held {
					held = false
					a.localReads--
					a.startLocalReads(read)
				}
			})
		})
		path, root := d.path, a.root
		bg.Go(func() {
			info, data, err := read(path, maxOpenBytes)
			timer.Stop()
			a.post(func() {
				if held {
					held = false
					a.localReads--
				}
				if !a.loadsClosed && a.root == root && slices.Contains(a.docs, d) && d.path == path && d.sync == syncLoading && !d.dirty() {
					a.remoteLoaded(d, path, info, data, err)
				} else {
					d.readDone()
				}
				a.startLocalReads(read)
			})
		})
	}
}
