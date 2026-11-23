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
// search results go where those from the Finder go. A file or a folder
// of the explorer dropped on a row of it moves there (treeDrag).

// fileDrag is a file dragged from the sidebar, and the match of it a
// search result shows, which opens selected; more, when set, gives the
// files dragged with it, of the rows chosen with its row, as the drag
// starts and as it drops rather than each frame.
type fileDrag struct {
	path  string
	match *searchMatch
	more  func() []string
}

// paths are the files dragged, the first first.
func (f *fileDrag) paths() []string {
	paths := []string{f.path}
	if f.more != nil {
		paths = append(paths, f.more()...)
	}
	return paths
}

// dropFile opens a file dragged from the sidebar at place at of a column.
func (a *app) dropFile(f *fileDrag, col *column, at int) {
	a.dropFiles(f.paths(), col, at)
	a.showDropped(f)
}

// dropFileBeside opens a file dragged from the sidebar in a column of its
// own beside col.
func (a *app) dropFileBeside(f *fileDrag, col *column, after bool) {
	a.dropFilesBeside(f.paths(), col, after)
	a.showDropped(f)
}

// dropFileOnColumn opens a file dragged from the sidebar after the tab a
// column shows, or types its path into the terminal it shows.
func (a *app) dropFileOnColumn(f *fileDrag, col *column) {
	a.dropOnColumn(f.paths(), col)
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
	a.dragValue(row, dragItem{file: f}, f.paths)
}

// dragValue makes row drag value within Mayoi, and the files of paths out
// to other apps, as a copy.
func (a *app) dragValue(row ui.Element, value any, paths func() []string) {
	row.Drag(value)
	if a.host.nativeDrags() {
		row.DragDataFrom(func() mtransfer.Data { return fileDragData(paths()...) })
	}
	if row.Dragging() {
		a.dragNow = true
	}
}

// treeDrag is a file or a folder dragged from the explorer of from, which
// moves to a folder of that explorer it is dropped on. A file drags as a
// dragItem, which opens in the columns as well; a folder as itself, which
// nothing else takes.
type treeDrag struct {
	path string
	from *app
}

// treeTake is what a row of the explorer takes: a dragItem, of which
// only one with tree set moves, or the treeDrag of a folder.
type treeTake interface{ treeTake() }

func (dragItem) treeTake()  {}
func (*treeDrag) treeTake() {}

// treeSource is the file or folder of the explorer that v drags, nil for
// what was dragged from elsewhere, as from search or source control.
func treeSource(v treeTake) *treeDrag {
	switch v := v.(type) {
	case dragItem:
		return v.tree
	case *treeDrag:
		return v
	}
	return nil
}

// fileDragData is what a drag out to another app offers: the files as a
// file list when they are of this machine and still there, so the drop
// gets real files, and their paths as text otherwise, a line each, as a
// drop on the app's own terminal types them.
func fileDragData(paths ...string) mtransfer.Data {
	local := true
	shown := make([]string, len(paths))
	for i, path := range paths {
		if _, err := os.Lstat(path); remote.HostOf(path) != "" || err != nil {
			local = false
		}
		shown[i] = remote.Display(path)
	}
	if local {
		if data, ferr := mtransfer.FileData(paths...); ferr == nil {
			return data
		}
	}
	return mtransfer.TextData(strings.Join(shown, "\n"))
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
