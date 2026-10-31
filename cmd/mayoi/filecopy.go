package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// ⌘C in the explorer puts the file chosen on the clipboard as the Finder
// copies files, and ⌘V copies the files the clipboard has, of another
// window or of the Finder, into the folder chosen. The Finder knows the
// files of this machine only: a remote file copied goes on the clipboard
// as its path, which the app remembers, so that a paste into a folder of
// this machine downloads it, and one into a folder of another host
// copies it there. Files of this machine pasted into a remote folder are
// uploaded.

// copiedRemote are the remote files copied last, of every window, and
// the text of the clipboard they left, which tells whether they are
// still what it has.
var copiedRemote struct {
	sync.Mutex
	text  string
	paths []string
}

// explorerInput takes the Copy and Paste of the Edit menu while the
// explorer has the keyboard.
func (a *app) explorerInput(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputCommand {
		return false
	}
	switch ev.Text {
	case "copy":
		if a.fileRow < 0 || a.fileRow >= a.files.Rows() {
			return false
		}
		a.copyFiles(a.files.Item(a.fileRow))
		return true
	case "paste":
		a.pasteFiles(a.targetDir())
		return true
	}
	return false
}

func (a *app) copyFiles(paths ...string) {
	if slices.ContainsFunc(paths, remote.Is) {
		lines := make([]string, len(paths))
		for i, p := range paths {
			lines[i] = hostPath(p)
		}
		text := strings.Join(lines, "\n")
		copiedRemote.Lock()
		copiedRemote.text, copiedRemote.paths = text, slices.Clone(paths)
		copiedRemote.Unlock()
		a.services.WriteClipboard(text)
		return
	}
	if err := a.host.copyFiles(paths); err != nil {
		a.fail("%s", locales.FailCopy(err.Error()))
	}
}

// clipboardSources are the files the clipboard has: those of the Finder,
// or the remote ones copied last while it still has them.
func (a *app) clipboardSources() []string {
	if files := a.host.clipboardFiles(); len(files) > 0 {
		return files
	}
	copiedRemote.Lock()
	text, paths := copiedRemote.text, copiedRemote.paths
	copiedRemote.Unlock()
	if text == "" || a.services.ReadClipboard() != text {
		return nil
	}
	return slices.Clone(paths)
}

// pasteFiles copies the files of the clipboard into dir, and chooses the
// first of them in the tree. Those that go to or come from a host are
// copied in the background.
func (a *app) pasteFiles(dir string) { a.copyPathsInto(a.clipboardSources(), dir) }

// copyPathsInto copies files and folders into dir, as a paste or a drop
// on the explorer does.
func (a *app) copyPathsInto(srcs []string, dir string) {
	if len(srcs) == 0 {
		return
	}
	if remote.Is(dir) || slices.ContainsFunc(srcs, remote.Is) {
		a.pasteAcross(srcs, dir)
		return
	}
	var first string
	for _, p := range srcs {
		to, err := copyInto(p, dir)
		if err != nil {
			a.fail("%s", locales.FailPasteItem(filepath.Base(p), err.Error()))
			continue
		}
		if first == "" {
			first = to
		}
	}
	a.pasted(dir, first)
}

// pasted shows the files pasted into dir in the tree, and chooses first.
func (a *app) pasted(dir, first string) {
	if first == "" || a.tree == nil {
		return
	}
	if dir != a.root {
		a.files.Open.Add(dir)
	}
	a.tree.Refresh()
	a.indexStale, a.indexed = true, time.Time{}
	a.selectInTree = first
}

// pasteAcross copies files into dir in the background, where one or the
// other is on a host, and says how it goes in the status bar.
func (a *app) pasteAcross(srcs []string, dir string) {
	if c, _, ok := remote.Lookup(dir); ok && !c.Connected() {
		a.fail("%s", locales.FailPasteNotConnected(c.Host))
		return
	}
	x := a.startTransfer(transferLabel(srcs, dir))
	bg.Go(func() {
		got, err := transferInto(srcs, dir, x.progress)
		a.post(func() {
			if x.progress.Canceled() {
				// Canceled while the status bar still showed it, though
				// the copy got to its end, or placed some before it
				// stopped: what arrived goes, but never a file it
				// copied, and then the status.
				go func() {
					for _, p := range got {
						if !slices.Contains(srcs, p) {
							vfs.RemoveAll(p)
						}
					}
					a.post(func() {
						a.endTransfer(x)
						a.transferFailed(locales.FailPaste(), remote.ErrCanceled)
					})
				}()
				return
			}
			a.endTransfer(x)
			if err != nil {
				a.transferFailed(locales.FailPaste(), err)
			}
			if len(got) > 0 {
				a.pasted(dir, got[0])
			}
		})
	})
}

// transferLabel says what a paste does: "Uploading 3 items to box…".
func transferLabel(srcs []string, dir string) string {
	what := filepath.Base(srcs[0])
	if len(srcs) > 1 {
		what = locales.ItemsCount(len(srcs))
	}
	to := remote.HostOf(dir)
	switch {
	case to == "":
		return locales.TransferDownloading(what)
	case !slices.ContainsFunc(srcs, func(p string) bool { return remote.HostOf(p) != to }):
		return locales.TransferCopyingOn(what, to)
	}
	return locales.TransferUploadingTo(what, to)
}

// transferInto copies files of this machine or of hosts into dir, of
// this machine or of a host, and returns where they went: the remote ones
// download, those of this machine upload, and those of the host of dir
// copy there.
func transferInto(srcs []string, dir string, p *remote.Progress) ([]string, error) {
	c, rdir, remoteDir := remote.Lookup(dir)
	if !remoteDir {
		var out []string
		for _, src := range srcs {
			to, err := fetchInto(src, dir, p)
			if err != nil {
				return out, err
			}
			out = append(out, to)
		}
		return out, nil
	}
	got, err := toHost(c, srcs, rdir, p)
	return hostPaths(c, got), err
}

// toHost copies files of this machine or of hosts into the folder rdir of
// c's host, and returns where they went there. With rdir "", those of
// other machines go to a temporary folder of the host, as for a program
// there to read them, and those of the host stay where they are.
func toHost(c *remote.Conn, srcs []string, rdir string, p *remote.Progress) ([]string, error) {
	// The paths come back in the order of srcs: same and local hold the
	// places of theirs.
	got := make([]string, len(srcs))
	var local, same []string
	var localAt, sameAt []int
	var stage string
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	done := func() []string {
		return slices.DeleteFunc(got, func(p string) bool { return p == "" })
	}
	for i, src := range srcs {
		sc, rp, ok := remote.Lookup(src)
		switch {
		case !ok:
			local, localAt = append(local, src), append(localAt, i)
		case sc == c && rdir == "":
			got[i] = rp
		case sc == c:
			same, sameAt = append(same, rp), append(sameAt, i)
		default:
			// From another host, by way of this machine.
			if stage == "" {
				var err error
				if stage, err = os.MkdirTemp("", "mayoi-paste-"); err != nil {
					return nil, err
				}
			}
			to, err := fetchInto(src, stage, p)
			if err != nil {
				return nil, err
			}
			local, localAt = append(local, to), append(localAt, i)
		}
	}
	if len(same) > 0 {
		out, err := c.CopyOnHost(same, rdir, p)
		for k, rp := range out {
			got[sameAt[k]] = rp
		}
		if err != nil {
			return done(), err
		}
	}
	if len(local) > 0 {
		out, err := c.Upload(local, rdir, p)
		for k, rp := range out {
			got[localAt[k]] = rp
		}
		if err != nil {
			return done(), err
		}
	}
	return done(), nil
}

func hostPaths(c *remote.Conn, rps []string) []string {
	out := make([]string, len(rps))
	for i, rp := range rps {
		out[i] = remote.Path(c.Host, rp)
	}
	return out
}

// fetchInto copies a file or folder, of this machine or of a host, into
// the folder dir of this machine, under a name of its own there. One of a
// host lands beside first, and takes its name once it all arrived.
func fetchInto(src, dir string, p *remote.Progress) (string, error) {
	c, rp, ok := remote.Lookup(src)
	if !ok {
		return copyInto(src, dir)
	}
	stage, err := os.MkdirTemp(dir, ".mayoi-paste-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	name := path.Base(rp)
	at := filepath.Join(stage, name)
	if err := c.Download(rp, at, p); err != nil {
		return "", err
	}
	info, err := os.Lstat(at)
	if err != nil {
		return "", err
	}
	to := freeName(dir, name, info.IsDir())
	return to, os.Rename(at, to)
}

// copyInto copies the file or folder at src into dir, under a name of
// its own there, and returns where it went.
func copyInto(src, dir string) (string, error) {
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}
	if info.IsDir() && (dir == src || strings.HasPrefix(dir, src+string(filepath.Separator))) {
		return "", errors.New(locales.ErrPasteIntoItself())
	}
	to := freeName(dir, filepath.Base(src), info.IsDir())
	return to, copyEntry(src, to)
}

// freeName is name in dir, or, when that is taken, "name copy", then
// "name copy 2" and on, before the extension of a file, as the Finder
// names them.
func freeName(dir, name string, isDir bool) string {
	path := filepath.Join(dir, name)
	if _, err := os.Lstat(path); err != nil {
		return path
	}
	stem, ext := name, ""
	if e := filepath.Ext(name); !isDir && e != name {
		stem, ext = strings.TrimSuffix(name, e), e
	}
	for n := 1; ; n++ {
		suffix := " copy"
		if n > 1 {
			suffix = fmt.Sprintf(" copy %d", n)
		}
		path = filepath.Join(dir, stem+suffix+ext)
		if _, err := os.Lstat(path); err != nil {
			return path
		}
	}
}

// copyEntry copies a file, a link or a folder with all it has, keeping
// their modes.
func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyEntry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
