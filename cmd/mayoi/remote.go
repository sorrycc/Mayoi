package main

import (
	"bytes"
	"fmt"
	"mayoi/internal/remotefs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (a *app) chooseRemote() {
	a.prompt = &prompt{title: "Connect via SSH…", hint: "Host or SSH URL", done: func(value string) string {
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "ssh://") {
			value = "ssh://" + value
		}
		dir, err := remotefs.Parse(value)
		if err != nil {
			return err.Error()
		}
		u, _ := url.Parse(value)
		home := u.Path == ""
		a.post(func() { a.browseRemote(dir, home, !a.blank()) })
		return ""
	}}
}

// connectRemote keeps authentication and directory checks off the UI thread.
// A later open/close invalidates the result, so an old connection cannot switch
// the window back to a workspace the user has left.
func (a *app) connectRemote(dir string, restore bool) {
	if a.root != dir || restore {
		a.remoteGeneration++
	}
	gen := a.remoteGeneration
	a.remoteBusy = true
	go func() {
		err := remotefs.Connect(dir)
		if err == nil {
			info, e := remotefs.Stat(dir)
			err = e
			if e == nil && !info.IsDir() {
				err = fmt.Errorf("remote path is not a directory")
			}
		}
		a.post(func() {
			if gen != a.remoteGeneration {
				return
			}
			a.remoteBusy = false
			if err != nil {
				a.fail("Could not connect to %s: %v", remotefs.Display(dir), err)
				return
			}
			if restore {
				a.openFolder(dir)
				a.restoreTabs()
				if a.window.TerminalOpen && a.window.PanelOpen {
					a.newTerminal()
				}
			}
			if !restore {
				if a.root == dir {
					// Reconnecting the same workspace keeps its live tabs and pending reads.
					if a.tree != nil {
						a.tree.Refresh()
					}
					for _, d := range a.docs {
						if d.remoteLoading || d.remoteLoadError != "" {
							a.loadRemoteDocument(d, true)
						}
					}
					for _, v := range a.imageViewers() {
						a.loadImage(v)
					}
					a.git.stale, a.indexStale, a.ignoredStale = true, true, true
				} else {
					a.openSession(dir)
				}
			}
			a.tell(note{message: "Connected to " + remotefs.Display(dir)})
		})
	}()
}
func (a *app) reconnectRemote() {
	if dir := a.folder(); remotefs.IsRemote(dir) {
		a.connectRemote(dir, a.root == "")
	} else {
		a.chooseRemote()
	}
}

// Native Save dialogs operate on the local machine. Remote documents instead
// ask for a path on their own host, including Save As and untitled buffers.
func (a *app) chooseSavePath(name string, done func(string)) {
	base := a.root
	if ed := a.activeEditor(); ed != nil && remotefs.IsRemote(ed.path) {
		base = filepath.Dir(ed.path)
	}
	if !remotefs.IsRemote(base) {
		a.host.saveAs(name, done)
		return
	}
	target, remote, _ := remotefs.Split(base)
	a.prompt = &prompt{title: "Save Remote File", hint: target.Host, value: filepath.Join(remote, name), done: func(value string) string {
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) {
			return "Enter an absolute path on the remote host."
		}
		path := target.Virtual(value)
		generation := a.remoteGeneration
		go func() {
			_, err := remotefs.Stat(path)
			a.post(func() {
				if generation != a.remoteGeneration {
					return
				}
				if err == nil {
					a.confirm = &confirm{title: "Replace Remote File?", message: remotefs.Display(path), ok: "Replace", run: func() {
						if generation == a.remoteGeneration {
							done(path)
						}
					}}
				} else if os.IsNotExist(err) {
					done(path)
				} else {
					a.fail("Could not check remote path: %v", err)
				}
			})
		}()

		return ""
	}}
}

// Remote saves run off the frame thread. Mark only the snapshot actually sent
// as saved; edits made during the upload remain dirty and a requested close
// waits for those edits to be saved too.
func (a *app) saveRemoteDoc(d *document, then func()) {
	if a.remoteMutations > 0 {
		if then != nil {
			a.fail("Wait for the remote file operation to finish before closing")
		}
		return
	}
	if d.remoteLoading || d.remoteLoadError != "" {
		return
	}
	if then != nil {
		d.remoteAfterSave = append(d.remoteAfterSave, then)
	}
	if d.remoteSaving {
		d.remoteSaveAgain = true
		return
	}
	d.remoteSaving = true
	snap, crlf, path := d.buf.Snapshot(), d.crlf, d.path
	generation := a.remoteGeneration
	d.remoteRead++
	go func() {
		var b bytes.Buffer
		_, err := snap.WriteTo(&b)
		data := b.Bytes()
		if crlf {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		if err == nil {
			mode := os.FileMode(0o644)
			if info, e := remotefs.Stat(path); e == nil {
				mode = info.Mode().Perm()
			} else if !os.IsNotExist(e) {
				err = e
			}
			if err == nil {
				err = remotefs.WriteFile(path, data, mode)
			}
		}
		a.post(func() {
			if generation != a.remoteGeneration {
				return
			}
			d.remoteSaving = false
			again := d.remoteSaveAgain
			d.remoteSaveAgain = false
			after := d.remoteAfterSave
			d.remoteAfterSave = nil
			if err != nil {
				d.autoSave.failed = true
				a.fail("Could not save %s: %v", d.name(), err)
				return
			}
			if d.path != path {
				return
			}
			d.savedSnap, d.saved, d.diskCRLF = snap, snap.Version(), crlf
			d.diskGone, d.autoSave.failed = false, false
			a.git.stale = true
			a.rememberTabs()
			if d.dirty() && (again || len(after) > 0) {
				d.remoteAfterSave = after
				a.saveRemoteDoc(d, nil)
				return
			}
			for _, done := range after {
				done()
			}
		})
	}()
}
