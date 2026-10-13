package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"mayoi/internal/remotefs"
)

// Remote tabs exist immediately; only byte IO runs off the frame thread.
// A read may finish after a close, rename, edit, save or workspace switch.
func (a *app) loadRemoteDocument(d *document, initial bool) {
	d.remoteRead++
	request, generation, path, version := d.remoteRead, a.remoteGeneration, d.path, d.buf.Version()
	if initial {
		d.remoteLoading, d.remoteLoadError = true, ""
	}
	go func() {
		var data []byte
		var err error
		if initial && !remotefs.Connected(path) {
			err = remotefs.Connect(path)
		}
		if err == nil {
			data, err = remotefs.ReadFileLimit(path, maxOpenBytes)
		}
		if err == nil && isBinary(data) {
			err = errBinary
		}
		a.post(func() {
			if generation != a.remoteGeneration || request != d.remoteRead || d.path != path || !slices.Contains(a.docs, d) {
				return
			}
			if d.buf.Version() != version || d.remoteSaving || d.dirty() {
				return
			}
			if err != nil {
				if initial {
					d.remoteLoading = false
					d.remoteLoadError = fmt.Sprintf("Could not open %s: %v", d.name(), err)
				} else {
					a.fail("Could not reload %s: %v", d.name(), err)
				}
				return
			}
			crlf := bytes.Contains(data, []byte("\r\n"))
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
			if ed := d.main(); ed != nil && string(data) != d.buf.String() {
				ed.edit(ed.sel, textEdit{0, d.buf.Len(), string(data)})
			}
			d.crlf, d.diskCRLF = crlf, crlf
			d.markSaved()
			d.measureWidest()
			d.indent = detectIndent(d.buf.Snapshot(), path)
			d.remoteLoading, d.remoteLoadError, d.diskGone = false, "", false
			restores := d.remoteRestores
			d.remoteRestores = nil
			for _, restore := range restores {
				restore()
			}
			a.rememberTabs()
		})
	}()
}

func localStatForRestore(path string) (os.FileInfo, error) {
	if remotefs.IsRemote(path) {
		return nil, nil
	}
	return os.Stat(path)
}

// The watch set belongs to visible directories and documents with live tabs.
func (a *app) syncRemoteWatch() {
	if a.tree == nil || !remotefs.IsRemote(a.root) {
		return
	}
	dirs := []string{a.root}
	for dir := range a.files.Open.All() {
		visible := true
		for parent := filepath.Dir(dir); parent != a.root && len(parent) >= len(a.root); parent = filepath.Dir(parent) {
			if !a.files.Open.Has(parent) {
				visible = false
				break
			}
		}
		if visible {
			dirs = append(dirs, dir)
		}
	}
	var files []string
	for _, d := range a.docs {
		if !d.remoteLoading && d.remoteLoadError == "" {
			files = append(files, d.path)
		}
	}
	for _, v := range a.imageViewers() {
		files = append(files, v.path)
	}
	a.tree.SetPollTargets(dirs, files)
}

// Collapse bursts of directory and file changes into one index invalidation.
type remoteEvents struct {
	changed, removed []string
	structure        bool
}

func (a *app) queueRemoteChange(changed, removed []string) {
	if a.remoteEvents == nil {
		events := &remoteEvents{}
		a.remoteEvents = events
		generation := a.remoteGeneration
		time.AfterFunc(250*time.Millisecond, func() {
			a.post(func() {
				if a.remoteEvents != events {
					return
				}
				a.remoteEvents = nil
				if generation != a.remoteGeneration {
					return
				}
				if events.structure {
					a.indexStale, a.ignoredStale = true, true
				}
				if len(events.changed)+len(events.removed) > 0 {
					a.diskChanged(events.changed, events.removed)
				}
			})
		})
	}
	e := a.remoteEvents
	e.structure = e.structure || len(changed)+len(removed) == 0
	e.changed = append(e.changed, changed...)
	e.removed = append(e.removed, removed...)
}

// Prompt input and results belong to the UI. Network operations only capture values.
func (a *app) remotePrompt(pr *prompt, work func() error, done func()) {
	pr.busy = true
	a.remoteMutations++
	generation := a.remoteGeneration
	go func() {
		err := work()
		a.post(func() {
			if generation != a.remoteGeneration {
				return
			}
			pr.busy = false
			a.remoteMutations--
			defer a.autoSaveAll()
			if err != nil {
				if a.prompt == pr {
					pr.err = err.Error()
				} else {
					a.fail("Remote operation failed: %v", err)
				}
				return
			}
			if a.prompt == pr {
				a.prompt = nil
			}
			done()
		})
	}()
}

func (a *app) askRemoteEntry(dir string, folder bool) {
	title := "New File"
	if folder {
		title = "New Folder"
	}
	pr := &prompt{title: title, hint: a.relOrPath(dir)}
	pr.done = func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsRune(name, 0) {
			return "Enter a name."
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		a.remotePrompt(pr, func() error {
			if _, err := remotefs.Lstat(path); err == nil {
				return fmt.Errorf("a file or folder with that name exists")
			} else if !os.IsNotExist(err) {
				return err
			}
			if folder {
				return remotefs.MkdirAll(path, 0o755)
			}
			if err := remotefs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return remotefs.CreateFile(path)
		}, func() {
			a.files.Open.Add(dir)
			a.tree.Refresh()
			a.indexStale = true
			if !folder {
				a.openFile(path, true)
			}
		})
		return ""
	}
	a.prompt = pr
}
func (a *app) askRemoteRename(path string) {
	pr := &prompt{title: "Rename", hint: a.relOrPath(path), value: filepath.Base(path)}
	pr.done = func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
			return "Enter a name without slashes."
		}
		to := filepath.Join(filepath.Dir(path), name)
		if to == path {
			return ""
		}
		// A concurrent save must finish before the path it writes is renamed.
		for _, d := range a.docs {
			if (d.path == path || strings.HasPrefix(d.path, path+"/")) && d.remoteSaving {
				return "Wait for the remote save to finish."
			}
		}
		a.remotePrompt(pr, func() error {
			if _, err := remotefs.Lstat(to); err == nil {
				return fmt.Errorf("a file or folder with that name exists")
			} else if !os.IsNotExist(err) {
				return err
			}
			return remotefs.Rename(path, to)
		}, func() {
			for _, d := range a.docs {
				if d.path == path || strings.HasPrefix(d.path, path+"/") {
					d.path = to + d.path[len(path):]
					d.diskGone = false
					if d.remoteLoading {
						a.loadRemoteDocument(d, true)
					}
				}
			}
			for _, v := range a.imageViewers() {
				if v.path == path || strings.HasPrefix(v.path, path+"/") {
					v.path = to + v.path[len(path):]
					a.loadImage(v)
				}
			}
			a.tree.Refresh()
			a.indexStale = true
			a.rememberTabs()
		})
		return ""
	}
	a.prompt = pr
}
