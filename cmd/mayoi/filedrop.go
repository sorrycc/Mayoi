package main

import (
	"os"
	"slices"
	"strings"

	"mayoi/internal/remote"

	mtransfer "github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// Files dragged from the Finder open where they are dropped: in a column,
// at a place of its tab strip, or in a column of their own beside one.
// Folders open as `mayoi <dir>` opens them. On a terminal, the paths are
// typed instead. Files dragged from the explorer, source control and the
// search results go where those from the Finder go.

// fileDrag is a file dragged from the sidebar, and the match of it a
// search result shows, which opens selected.
type fileDrag struct {
	path  string
	match *searchMatch
}

// dropFile opens a file dragged from the sidebar at place at of a column.
func (a *app) dropFile(f *fileDrag, col *column, at int) {
	a.dropFiles([]string{f.path}, col, at)
	a.showDropped(f)
}

// dropFileBeside opens a file dragged from the sidebar in a column of its
// own beside col.
func (a *app) dropFileBeside(f *fileDrag, col *column, after bool) {
	a.dropFilesBeside([]string{f.path}, col, after)
	a.showDropped(f)
}

// dropFileOnColumn opens a file dragged from the sidebar after the tab a
// column shows, or types its path into the terminal it shows.
func (a *app) dropFileOnColumn(f *fileDrag, col *column) {
	a.dropOnColumn([]string{f.path}, col)
	a.showDropped(f)
}

// showDropped selects the match of a search result dropped.
func (a *app) showDropped(f *fileDrag) {
	if f.match != nil {
		a.selectMatch(f.path, *f.match)
	}
}

// dragFile makes a row of the sidebar drag its file to the columns. The
// drag also carries the file out to other apps, as a copy.
func (a *app) dragFile(row ui.Element, f *fileDrag) {
	row.Drag(dragItem{file: f})
	if a.host.nativeDrags() {
		row.DragDataFrom(func() mtransfer.Data { return fileDragData(f.path) })
	}
	if row.Dragging() {
		a.dragNow = true
	}
}

// fileDragData is what a drag out to another app offers: the file as a
// file list when it is of this machine and still there, so the drop gets
// a real file, and its path as text otherwise, as a drop on the app's own
// terminal types it.
func fileDragData(path string) mtransfer.Data {
	if remote.HostOf(path) == "" {
		if _, err := os.Lstat(path); err == nil {
			if data, ferr := mtransfer.FileData(path); ferr == nil {
				return data
			}
		}
	}
	return mtransfer.TextData(remote.Display(path))
}

// dropFiles opens the files dropped on a column at place at, the first
// there and each other after it, and the folders in windows of their own.
func (a *app) dropFiles(paths []string, col *column, at int) {
	folders, files := splitPaths(paths)
	if len(folders) > 0 {
		a.host.openFolders(folders)
	}
	if slices.Index(a.cols, col) < 0 {
		col = a.column()
		at = col.active + 1
	}
	for i, p := range files {
		a.openFileIn(col, at, p, i == len(files)-1)
		at = col.active + 1
	}
}

// dropFilesBeside opens the files dropped on a side of a column in a
// column of their own there.
func (a *app) dropFilesBeside(paths []string, col *column, after bool) {
	if slices.Index(a.cols, col) < 0 {
		return
	}
	if _, files := splitPaths(paths); len(files) > 0 {
		nc := a.splitColumn(col, after)
		a.dropFiles(paths, nc, 0)
		if len(nc.tabs) == 0 {
			// None of them opened.
			a.removeColumn(nc)
		}
		return
	}
	a.dropFiles(paths, col, col.active+1)
}

// dropOnColumn opens what is dropped on the middle of a column after the
// tab shown, or types it into the terminal the column shows.
func (a *app) dropOnColumn(paths []string, col *column) {
	if t := col.tab(); t != nil && t.term != nil {
		a.pastePaths(t.term, paths)
		return
	}
	a.dropFiles(paths, col, col.active+1)
}

// pastePaths types the paths into a terminal, quoted for the shell, as
// the Terminal does. A terminal of a host gets the paths on the host:
// those of its own files, and, for the others, of copies uploaded to it.
// One of this machine gets a remote path as scp spells it, host:/path.
func (a *app) pastePaths(s *session, paths []string) {
	c := sessionConn(s)
	if c == nil {
		shown := make([]string, len(paths))
		for i, p := range paths {
			shown[i] = remote.Display(p)
		}
		s.term.Paste(quotePaths(shown))
		return
	}
	if slices.ContainsFunc(paths, func(p string) bool { return remote.HostOf(p) != c.Host }) {
		a.uploadPaths(s, paths, nil)
		return
	}
	rps := make([]string, len(paths))
	for i, p := range paths {
		rps[i] = hostPath(p)
	}
	s.term.Paste(quotePaths(rps))
}

// shellQuote quotes s for a POSIX shell, unless it needs no quotes.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
