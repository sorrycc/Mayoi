package main

import (
	"fmt"
	"path/filepath"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// commitTree is the panel of the files of a commit's tab, beside its
// diff: a tree of their folders, or a list by path, as source control
// shows the changes. A file chosen scrolls the diff to it, and the file
// at the top of the diff is the one chosen as it scrolls.
type commitTree struct {
	list ui.ListState
	row  int
	// closed are the folders closed, by path; gen counts the changes of
	// the files and of the folders closed, and rowsOf and rowsTree are
	// what the rows were made for.
	closed   map[string]bool
	gen      int
	rows     []commitTreeRow
	rowsOf   int
	rowsTree bool
	built    bool
	// top is the file at the top of the diff, nil for none, and shown
	// the one the list last chose for it.
	top, shown *diffFile
}

// commitTreeRow is a row of the panel: a folder's, or a file of the
// diff's.
type commitTreeRow struct {
	treeRow
	file *diffFile
}

// commitFilesLeast is how narrow the panel of a commit's files may be,
// and diffLeast how narrow the diff beside it: a tab narrower than both
// shows no panel.
const (
	commitFilesLeast = 140
	diffLeast        = 240
)

// showsCommitFiles reports whether a diff shows the panel of its files:
// a commit's, once it has files, when the panel is open.
func (a *app) showsCommitFiles(v *diffView) bool {
	return v.commit != nil && len(v.files) > 0 && a.window.CommitFilesOpen
}

// toggleCommitFiles shows the panel of the files of commits' tabs, or
// hides it.
func (a *app) toggleCommitFiles() {
	a.window.CommitFilesOpen = !a.window.CommitFilesOpen
	a.settings.Changed()
}

// diffViewUI shows a diff, with the panel of its files beside it when it
// is a commit's.
func (a *app) diffViewUI(c *ui.Context, v *diffView) {
	if !a.showsCommitFiles(v) {
		a.diffPaneUI(c, v)
		return
	}
	row := ui.Row(c.Key("diff-files")).Fill()
	width := row.Bounds().W
	if width > 0 && width < commitFilesLeast+diffLeast {
		row.Children(func() { a.diffPaneUI(c, v) })
		return
	}
	row.Children(func() {
		ui.Box(c.Key("diff-main")).Grow(1).Shrink(1).MinWidth(0).FillHeight().Children(func() { a.diffPaneUI(c, v) })
		a.commitFilesUI(c, v, width)
	})
}

// commitTreeRows returns the rows of the panel of a diff's files, made
// again only when the files, the folders closed or the layout change.
func (a *app) commitTreeRows(v *diffView) []commitTreeRow {
	t := &v.tree
	tree := a.settings.ScmTree
	if t.built && t.rowsOf == t.gen && t.rowsTree == tree {
		return t.rows
	}
	t.built, t.rowsOf, t.rowsTree = true, t.gen, tree
	t.shown = nil // chosen again by the new rows
	t.rows = t.rows[:0]
	if !tree {
		for _, f := range v.files {
			t.rows = append(t.rows, commitTreeRow{treeRow{e: f.entry}, f})
		}
		return t.rows
	}
	root := v.repoRoot
	if r := a.diffContext(v).git.repo; root == "" && r != nil {
		root = r.Root
	}
	byPath := make(map[string]*diffFile, len(v.files))
	es := make([]git.Entry, len(v.files))
	for i, f := range v.files {
		byPath[f.path] = f
		es[i] = f.entry
	}
	for _, r := range fileTree(es, root, func(dir string) bool { return t.closed[dir] }) {
		t.rows = append(t.rows, commitTreeRow{r, byPath[r.e.Path]})
	}
	return t.rows
}

// toggleFolder opens a folder of the panel, or closes it.
func (t *commitTree) toggleFolder(dir string) {
	if t.closed == nil {
		t.closed = map[string]bool{}
	}
	if t.closed[dir] {
		delete(t.closed, dir)
	} else {
		t.closed[dir] = true
	}
	t.gen++
}

// revealFile scrolls a diff to a file of it, once laid out.
func (v *diffView) revealFile(f *diffFile) {
	v.reveal = &diffTarget{path: f.path, any: true}
	v.changed()
}

// commitFilesUI is the panel of the files of a commit's diff, at its
// right, as wide as the layout keeps it within a tab width wide.
func (a *app) commitFilesUI(c *ui.Context, v *diffView, width float32) {
	p := a.pal
	t := &v.tree
	rows := a.commitTreeRows(v)
	w := a.window.CommitFilesWidth
	if width > 0 {
		w = clamp(w, commitFilesLeast, max(width-diffLeast, commitFilesLeast))
	}
	// The file at the top of the diff is the one chosen, while the list
	// does not have the keyboard.
	if t.top != t.shown {
		t.shown = t.top
		if k := t.rowOf(t.top); k >= 0 && !t.list.FocusWithin(c) {
			t.row = k
			t.list.ScrollIntoView(k)
		}
	}
	t.row = min(t.row, len(rows)-1)
	ui.Column(c.Key("commit-files")).Width(w).Shrink(0).FillHeight().Background(p.chrome).
		BorderWidth(0, 0, 0, 1).BorderColor(p.border).Children(func() {
		ui.Row(c).Height(26).Shrink(0).AlignItems(ui.Center).Gap(6).Padding(0, 6, 0, 12).Children(func() {
			ui.Text(c, locales.DiffCommitFiles()).FontSize(11).FontWeight(600).TextColor(p.muted).SingleLine().Shrink(1)
			ui.Text(c, fmt.Sprint(len(v.files))).FontSize(10.5).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.hover)
			ui.Spacer(c)
			if a.smallButton(c, iconX, locales.DiffHideCommitFiles()) {
				a.post(a.toggleCommitFiles)
			}
		})
		base := c.Theme()
		quiet := *base
		quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
		c.SetTheme(&quiet)
		var action func()
		t.list.Selected = &t.row
		t.list.Key = func(i int) any {
			if i >= len(rows) {
				return i
			}
			if r := rows[i]; r.dir != "" {
				return "dir:" + r.dir
			}
			return rows[i].e.Path
		}
		t.list.Label = func(i int) string {
			if r := rows[i]; r.dir != "" {
				return r.name
			}
			return filepath.Base(rows[i].e.Path)
		}
		out := ui.List(c.Key("commit-files-list"), &t.list, len(rows), func(i int) {
			a.commitTreeRowUI(c, v, i, rows[i], &action)
		}).Grow(1).Padding(0, 6, 8, 6).Label(locales.DiffCommitFiles())
		c.SetTheme(base)
		if action != nil {
			action()
		}
		chosen := func() (commitTreeRow, bool) {
			if t.row >= 0 && t.row < len(rows) {
				return rows[t.row], true
			}
			return commitTreeRow{}, false
		}
		if r, ok := chosen(); ok && out.Changed() && r.file != nil {
			v.revealFile(r.file)
		}
		if r, ok := chosen(); ok && out.Submitted() {
			switch {
			case r.dir != "":
				t.toggleFolder(r.dir)
			case r.file != nil:
				v.revealFile(r.file)
				v.pane.Focus()
			}
		}
		if r, ok := chosen(); ok && r.dir != "" {
			if out.Shortcut(0, ui.KeyRight) && t.closed[r.dir] || out.Shortcut(0, ui.KeyLeft) && !t.closed[r.dir] {
				t.toggleFolder(r.dir)
			}
		}
		a.splitter(c, edgeLeft, func(d float32) {
			a.window.CommitFilesWidth = clamp(w-d, commitFilesLeast, max(width-diffLeast, commitFilesLeast))
		})
	})
}

// rowOf returns the row of a file in the panel, -1 when it shows none,
// as in a folder closed.
func (t *commitTree) rowOf(f *diffFile) int {
	if f == nil {
		return -1
	}
	for i, r := range t.rows {
		if r.file == f {
			return i
		}
	}
	return -1
}

// commitTreeRowUI is a row of the panel of a commit's files: a folder,
// which a click opens or closes, or a file, which a click scrolls the
// diff to.
func (a *app) commitTreeRowUI(c *ui.Context, v *diffView, i int, r commitTreeRow, action *func()) {
	p := a.pal
	t := &v.tree
	tree := a.settings.ScmTree
	indent := float32(8)
	if tree {
		indent = treeIndent(r.depth)
		if r.dir == "" {
			indent += 16
		}
	}
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 4, 0, indent)
	if tree {
		a.scmGuides(row, r.depth)
	}
	clicked := row.Clicked()
	if r.dir != "" {
		closed := t.closed[r.dir]
		row.Gap(4).Children(func() {
			folder := iconFolderOpen
			if closed {
				folder = iconFolder
			}
			chevron(c, !closed).FontSize(12).TextColor(p.muted)
			ui.Icon(c, folder).FontSize(14).TextColor(p.accent).Margin(0, 2, 0, 0)
			ui.Text(c, r.name).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
		})
		if clicked && *action == nil {
			*action = func() {
				t.row = i
				t.toggleFolder(r.dir)
			}
		}
		return
	}
	e, f := r.e, r.file
	row.Tooltip(a.diffRelPath(v, e.Path))
	if f != nil {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.DiffGoToFile()).Chosen() {
				*action = func() { v.revealFile(f) }
			}
			if m.Item(openLabel(e)).Disabled(e.X == 'D').Chosen() {
				*action = func() { a.openPath(e.Path, true) }
			}
		})
	}
	row.Children(func() {
		a.commitFileLabel(c, e, tree, a.diffRelPath(v, e.Path))
	})
	if clicked && *action == nil && f != nil {
		*action = func() {
			t.row = i
			v.revealFile(f)
		}
	}
}

// diffRelPath is a file of a diff's path in its work tree.
func (a *app) diffRelPath(v *diffView, path string) string {
	if r := a.diffContext(v).git.repo; r != nil {
		return r.Rel(path)
	}
	return path
}

// commitFileLabel shows a file a commit changed: its icon, its name in
// the color of its change, and in a list by path its folder; then the
// letter of its change.
func (a *app) commitFileLabel(c *ui.Context, e git.Entry, tree bool, rel string) {
	p := a.pal
	name := filepath.Base(e.Path)
	icon, col := entryIcon(p, e, name)
	ui.Icon(c, icon).FontSize(14).TextColor(col).Shrink(0)
	label := ui.Text(c, name).FontSize(13).SingleLine().Shrink(0).TextColor(gitColor(p, e.X))
	if e.X == 'D' {
		label.Strikethrough()
	}
	if dir := filepath.Dir(rel); dir != "." && !tree {
		ui.Text(c, dir).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
	}
	ui.Spacer(c)
	ui.Text(c, string(e.X)).FontSize(11).FontWeight(600).Font(a.font.family).Padding(0, 4, 0, 2).
		TextColor(gitColor(p, e.X)).Shrink(0)
}

// commitFilesRead is what was read of the files a commit of the history
// changed, for its rows in source control.
type commitFilesRead struct {
	files   []git.CommitFile
	loading bool
	err     string
}

// toggleCommitOpen shows the files a commit of the history changed under
// its row, read once, or hides them.
func (a *gitContext) toggleCommitOpen(cm git.Commit) {
	s := a.scm
	if s.openCommits == nil {
		s.openCommits = map[string]bool{}
	}
	if s.openCommits[cm.Hash] {
		delete(s.openCommits, cm.Hash)
	} else {
		s.openCommits[cm.Hash] = true
		a.readCommitFiles(cm)
	}
	s.commitsGen++
}

// readCommitFiles reads the files a commit changed, in the background,
// once.
func (a *gitContext) readCommitFiles(cm git.Commit) {
	s := a.scm
	r := a.git.repo
	if r == nil || s.commitFiles[cm.Hash] != nil {
		return
	}
	if s.commitFiles == nil {
		s.commitFiles = map[string]*commitFilesRead{}
	}
	read := &commitFilesRead{loading: true}
	s.commitFiles[cm.Hash] = read
	bg.Go(func() {
		defer recoverCrash()
		files, err := r.CommitFiles(cm)
		a.app.post(func() {
			read.loading, read.files = false, files
			if err != nil {
				read.err = errorLine(err)
			}
			s.commitsGen++
		})
	})
}

// commitFileRows adds the rows of the files a commit of the history
// changed, at at in it, under its row: a tree of their folders or a list
// by path, as the changes show; or while they are read, or could not be,
// a row that says so.
func (a *gitContext) commitFileRows(rows []scmRow, cm *git.Commit, at int) []scmRow {
	s := a.scm
	prefix := "c:" + cm.Hash + ":"
	read := s.commitFiles[cm.Hash]
	note := func(text string) []scmRow {
		return append(rows, scmRow{group: groupCommits, under: cm, at: at, note: text, key: prefix + "note"})
	}
	switch {
	case read == nil || read.loading:
		return note(locales.Loading())
	case read.err != "":
		return note(read.err)
	case len(read.files) == 0:
		return note(locales.DiffCommitNoFiles())
	}
	es := commitEntries(read.files)
	if !a.app.settings.ScmTree || a.git.repo == nil {
		for _, e := range es {
			rows = append(rows, scmRow{group: groupCommits, under: cm, at: at, e: e, key: prefix + e.Path})
		}
		return rows
	}
	dirKey := func(dir string) string { return prefix + "dir:" + dir }
	for _, t := range fileTree(es, a.git.repo.Root, func(dir string) bool { return s.closedDirs[dirKey(dir)] }) {
		r := scmRow{group: groupCommits, under: cm, at: at, depth: t.depth}
		if t.dir != "" {
			r.dir, r.name, r.key = t.dir, t.name, dirKey(t.dir)
		} else {
			r.e, r.key = t.e, prefix+t.e.Path
		}
		rows = append(rows, r)
	}
	return rows
}

// showCommitFile shows a commit's changes scrolled to a file of it: in
// the preview tab, the keyboard staying where it is, or kept in a tab of
// its own, focused.
func (a *gitContext) showCommitFile(cm git.Commit, path string, keep bool) {
	var v *diffView
	if keep {
		v = a.showCommit(cm, true)
	} else {
		v = a.previewCommit(cm)
	}
	if v != nil {
		v.reveal = &diffTarget{path: path, any: true}
		v.changed()
	}
}

// scmCommitFile is a row under a commit of the history whose files show:
// the lanes of the graph running past it, then a folder of the files, a
// file, which a click shows in the commit's changes, or a note.
func (a *gitContext) scmCommitFile(c *ui.Context, i int, r scmRow, action *func()) {
	p := a.app.pal
	cm := r.under
	graph := a.historyGraph()
	var gr *graphRow
	if r.at >= 0 && r.at < len(graph) {
		gr = &graph[r.at]
	}
	lanes := min(graphLanes(graph), graphMaxLanes)
	tree := a.app.settings.ScmTree && r.note == ""
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 4, 0, 6)
	clicked := row.Clicked()
	if r.dir == "" && r.note == "" {
		row.Tooltip(a.git.repo.Rel(r.e.Path))
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.CmdGitOpenChanges()).Chosen() {
				*action = func() { a.showCommitFile(*cm, r.e.Path, true) }
			}
			if m.Item(openLabel(r.e)).Disabled(r.e.X == 'D').Chosen() {
				*action = func() { a.app.openPath(r.e.Path, true) }
			}
		})
	}
	row.Children(func() {
		ui.Box(c).Size(12, 22).Shrink(0) // under the commit's arrow
		ui.Box(c).Size(graphLaneW*float32(lanes-1)+12, 22).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
			if gr != nil {
				a.app.paintGraphThrough(pt, b, gr)
			}
		})
		indent := float32(0)
		if tree {
			indent = treeIndent(r.depth) - 4
			if r.dir == "" {
				indent += 16
			}
		}
		inner := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 0, 0, indent)
		if tree && r.depth > 0 {
			inner.Draw(func(pt *ui.Painter, b ui.Rect) {
				for k := range r.depth {
					pt.Fill(ui.Rect{X: b.X + treeIndent(k) + 2, Y: b.Y, W: 1, H: b.H}, p.guide, 0)
				}
			})
		}
		inner.Children(func() {
			switch {
			case r.note != "":
				ui.Text(c, r.note).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
			case r.dir != "":
				closed := a.scm.closedDirs[r.key]
				folder := iconFolderOpen
				if closed {
					folder = iconFolder
				}
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
					chevron(c, !closed).FontSize(12).TextColor(p.muted)
					ui.Icon(c, folder).FontSize(14).TextColor(p.accent).Margin(0, 2, 0, 0)
					ui.Text(c, r.name).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
				})
			default:
				a.app.commitFileLabel(c, r.e, tree, a.git.repo.Rel(r.e.Path))
			}
		})
	})
	if clicked && *action == nil && r.note == "" {
		*action = func() {
			a.scm.selection.Clear()
			a.scm.anchor = ""
			a.scm.row = i
			if r.dir != "" {
				a.toggleDir(r)
				return
			}
			a.showCommitFile(*cm, r.e.Path, false)
		}
	}
}

// paintGraphThrough paints the lanes of the graph that run on below a
// commit's row straight down past a row under it.
func (a *app) paintGraphThrough(pt *ui.Painter, b ui.Rect, gr *graphRow) {
	for i, l := range gr.below {
		if l.hash == "" {
			continue
		}
		x := b.X + 6 + graphLaneW*float32(min(i, graphMaxLanes-1))
		pt.Fill(ui.Rect{X: x - 0.75, Y: b.Y, W: 1.5, H: b.H}, graphColor(a.pal, l.color), 0)
	}
}
