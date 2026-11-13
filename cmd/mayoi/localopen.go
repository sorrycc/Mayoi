package main

import (
	"io/fs"
	"slices"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// Local reads cannot be interrupted inside the filesystem. Keep at most
// two running per project; waiting requests live on their documents, so
// closing a tab removes its waiting work without a goroutine or queue.
const localReadLimit = 2

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
	d.sync, d.localQueued = syncLoading, true
	read := a.localRead
	if read == nil {
		read = vfs.ReadFileInfo
	}
	a.startLocalReads(read)
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
		path, root := d.path, a.root
		bg.Go(func() {
			info, data, err := read(path, maxOpenBytes)
			a.post(func() {
				a.localReads--
				if !a.loadsClosed && a.root == root && slices.Contains(a.docs, d) && d.path == path && d.sync == syncLoading && !d.dirty() {
					a.remoteLoaded(d, path, info, data, err)
				}
				a.startLocalReads(read)
			})
		})
	}
}
