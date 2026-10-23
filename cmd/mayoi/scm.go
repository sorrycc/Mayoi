package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// scmGroup is a group of the changes the source control view lists.
type scmGroup int

const (
	groupMerge scmGroup = iota // in conflict
	groupStaged
	groupChanges
	groupUntracked
	groupCommits // the last commits of HEAD
	groupCount
)

// groupName names a group of the view, in the language shown.
func groupName(group scmGroup) string {
	switch group {
	case groupMerge:
		return locales.GroupMergeChanges()
	case groupStaged:
		return locales.GroupStagedChanges()
	case groupChanges:
		return locales.GroupChanges()
	case groupUntracked:
		return locales.GroupUntracked()
	}
	return locales.GroupCommits()
}

// scmRow is a row of the source control view: a group's header, or a
// file of it.
type scmRow struct {
	group  scmGroup
	header bool
	count  int // the files of a header's group
	e      git.Entry
	key    string // the list's key of the row
	// commit is the commit of a row of the history, its place in it at;
	// more is set for the row that reads more of it.
	commit *git.Commit
	at     int
	more   bool
	// dir is the folder of a folder's row in the tree, and name what it
	// shows of it; depth is how deep a row of the tree is.
	dir   string
	name  string
	depth int
	panel *scmPanelRow
	// under is the commit of the history a row of its files is under, at
	// at in it: a folder's, a file's, or one with a note, while its files
	// are read or when they could not be.
	under *git.Commit
	note  string
}

// file reports whether a row is a file's.
func (r *scmRow) file() bool {
	return r.panel == nil && !r.header && r.commit == nil && !r.more && r.dir == "" && r.under == nil
}

// scmState is the source control view's: its list, the row chosen, and
// the groups closed.
type scmState struct {
	section     scmSection
	rowsSection scmSection
	list        ui.ListState
	row         int
	selection   ui.Selection[string]
	anchor      string // the file Shift extends the choice from
	collapsed   [groupCount]bool
	rows        []scmRow // as the last frame built them
	// closedDirs are the folders of the tree closed, by their rows' keys;
	// dirsGen counts their changes.
	closedDirs map[string]bool
	dirsGen    int
	// rowsOf is the status, the groups and folders closed, the read of
	// the history and the layout the rows were made for.
	rowsOf        *git.Status
	rowsCollapsed [groupCount]bool
	rowsHistory   int
	rowsDirs      int
	rowsTree      bool
	panels        scmPanels
	rowsPanels    int
	// openCommits are the commits of the history whose files show under
	// them, by hash, and commitFiles what was read of each; commitsGen
	// counts their changes, and rowsCommits is what the rows were made
	// for.
	openCommits map[string]bool
	commitFiles map[string]*commitFilesRead
	commitsGen  int
	rowsCommits int
}

// groupEntries returns the files of each group, by path. They are worked
// out once for each status read, as the view asks for them every frame.
func (a *gitContext) groupEntries() [groupCount][]git.Entry {
	g := a.git
	if g.groupsOf != g.status || g.status == nil {
		g.groups, g.groupsOf = groupStatus(g.status), g.status
	}
	return g.groups
}

// groupStatus returns the files of a status in each group, by path, each
// group's slice clipped, so that appending to it copies.
func groupStatus(st *git.Status) [groupCount][]git.Entry {
	var out [groupCount][]git.Entry
	if st == nil {
		return out
	}
	for _, e := range st.Entries {
		switch {
		case e.Conflicted():
			out[groupMerge] = append(out[groupMerge], e)
		case e.Untracked():
			out[groupUntracked] = append(out[groupUntracked], e)
		default:
			if e.Staged() {
				out[groupStaged] = append(out[groupStaged], e)
			}
			if e.Unstaged() {
				out[groupChanges] = append(out[groupChanges], e)
			}
		}
	}
	for i, es := range out {
		slices.SortFunc(es, func(x, y git.Entry) int { return strings.Compare(x.Path, y.Path) })
		out[i] = slices.Clip(es)
	}
	return out
}

// scmRows returns the rows of the view: the groups that have files, and
// the files of those open, then the last commits and repository panel. They are made again
// only when the status, the groups closed or the history change.
func (a *gitContext) scmRows() []scmRow {
	s := a.scm
	g := a.git
	tree := a.app.settings.ScmTree
	if s.rows != nil && s.rowsOf == g.status && s.rowsCollapsed == s.collapsed && s.rowsHistory == g.historyGen &&
		s.rowsDirs == s.dirsGen && s.rowsTree == tree && s.rowsPanels == s.panels.gen && s.rowsSection == s.section &&
		s.rowsCommits == s.commitsGen {
		return s.rows
	}
	var rows []scmRow
	groups := a.groupEntries()
	for g, es := range groups {
		if s.section == scmHistory {
			continue
		}
		if len(es) == 0 {
			continue
		}
		rows = append(rows, scmRow{group: scmGroup(g), header: true, count: len(es), key: fmt.Sprintf("group:%d", g)})
		if s.collapsed[g] {
			continue
		}
		if tree && a.git.repo != nil {
			rows = treeRows(rows, scmGroup(g), es, a.git.repo.Root, s.closedDirs)
			continue
		}
		for _, e := range es {
			rows = append(rows, scmRow{group: scmGroup(g), e: e, key: fmt.Sprintf("%d:%s", g, e.Path)})
		}
	}
	if h := g.history; (len(h) > 0 || s.section == scmHistory) && s.section != scmChanges {
		rows = append(rows, scmRow{group: groupCommits, header: true, count: len(h), key: "group:commits"})
		if !s.collapsed[groupCommits] {
			for i := range h {
				rows = append(rows, scmRow{group: groupCommits, commit: &h[i], at: i, key: "commit:" + h[i].Hash})
				if s.openCommits[h[i].Hash] {
					rows = a.commitFileRows(rows, &h[i], i)
				}
			}
			if len(h) >= max(g.historyN, historyPage) {
				rows = append(rows, scmRow{group: groupCommits, more: true, key: "commits:more"})
			}
		}
	}
	if s.section != scmChanges {
		rows = a.scmPanelRows(rows)
	}
	s.rowsSection = s.section
	s.rowsPanels = s.panels.gen
	s.rows, s.rowsOf, s.rowsCollapsed, s.rowsHistory = rows, g.status, s.collapsed, g.historyGen
	s.rowsDirs, s.rowsTree, s.rowsCommits = s.dirsGen, tree, s.commitsGen
	// A refresh or a collapsed folder must not leave invisible files chosen.
	if s.selection.Len() > 0 {
		visible := make(map[string]bool, len(rows))
		for _, r := range rows {
			visible[r.key] = true
		}
		for key := range s.selection.All() {
			if !visible[key] {
				s.selection.Remove(key)
			}
		}
	}
	if s.rows == nil {
		s.rows = []scmRow{}
	}
	return s.rows
}

// scmDir is a folder of the tree of a group's changes: its folders and
// its files.
type scmDir struct {
	name, path string
	dirs       []*scmDir
	files      []git.Entry
}

// treeRow is a row of a tree of files: a folder's, dir its path and name
// what it shows of it, or a file's, e; depth is how deep it is.
type treeRow struct {
	dir, name string
	e         git.Entry
	depth     int
}

// fileTree lays out files, es by path, in a tree of their folders under
// root: a folder's folders first, then its files, and a folder with a
// single folder in it and no files shown as one, as "internal/git". The
// folders closed show without what is in them.
func fileTree(es []git.Entry, root string, closed func(dir string) bool) []treeRow {
	top := &scmDir{path: root}
	at := map[string]*scmDir{root: top}
	var dirOf func(path string) *scmDir
	dirOf = func(path string) *scmDir {
		if d, ok := at[path]; ok {
			return d
		}
		up := filepath.Dir(path)
		if up == path {
			// The top of the disk, reached without meeting root, as a
			// root spelled otherwise than Git spells its files, which
			// Windows' long and 8.3 names of a folder may be: the top
			// of the tree, rather than climbing forever.
			return top
		}
		parent := dirOf(up)
		d := &scmDir{name: filepath.Base(path), path: path}
		parent.dirs = append(parent.dirs, d)
		at[path] = d
		return d
	}
	for _, e := range es {
		dir := filepath.Dir(e.Path)
		if dir != root && !inFolder(root, dir) {
			dir = root // outside the work tree: shown at its top
		}
		d := dirOf(dir)
		d.files = append(d.files, e)
	}
	var rows []treeRow
	var add func(d *scmDir, depth int)
	add = func(d *scmDir, depth int) {
		slices.SortFunc(d.dirs, func(x, y *scmDir) int { return strings.Compare(x.name, y.name) })
		for _, sub := range d.dirs {
			name := sub.name
			for len(sub.dirs) == 1 && len(sub.files) == 0 {
				sub = sub.dirs[0]
				name += "/" + sub.name
			}
			rows = append(rows, treeRow{dir: sub.path, name: name, depth: depth})
			if closed == nil || !closed(sub.path) {
				add(sub, depth+1)
			}
		}
		for _, e := range d.files {
			rows = append(rows, treeRow{e: e, depth: depth})
		}
	}
	add(top, 0)
	return rows
}

// treeRows adds the rows of a group's files, es by path, to rows, in a
// tree of their folders under root, as fileTree lays them out. The
// folders closed, by their rows' keys, show without what is in them.
func treeRows(rows []scmRow, g scmGroup, es []git.Entry, root string, closed map[string]bool) []scmRow {
	dirKey := func(dir string) string { return fmt.Sprintf("%d:dir:%s", g, dir) }
	for _, t := range fileTree(es, root, func(dir string) bool { return closed[dirKey(dir)] }) {
		if t.dir != "" {
			rows = append(rows, scmRow{group: g, dir: t.dir, name: t.name, depth: t.depth, key: dirKey(t.dir)})
			continue
		}
		rows = append(rows, scmRow{group: g, e: t.e, depth: t.depth, key: fmt.Sprintf("%d:%s", g, t.e.Path)})
	}
	return rows
}

// toggleDir opens a folder of the tree, or closes it.
func (a *gitContext) toggleDir(r scmRow) {
	s := a.scm
	if s.closedDirs == nil {
		s.closedDirs = map[string]bool{}
	}
	if s.closedDirs[r.key] {
		delete(s.closedDirs, r.key)
	} else {
		s.closedDirs[r.key] = true
	}
	s.dirsGen++
}

// dirEntries returns the files of a group in a folder, or in the folders
// in it.
func (a *gitContext) dirEntries(g scmGroup, dir string) []git.Entry {
	var out []git.Entry
	for _, e := range a.groupEntries()[g] {
		if inFolder(dir, e.Path) {
			out = append(out, e)
		}
	}
	return out
}

// inFolder reports whether path is in the folder dir, or in a folder in
// it: also of a dir that ends in a separator, as the top of a disk, C:\ or
// /, does. On Windows the case of the letters does not count, as C:\Proj
// and c:\proj are one folder.
func inFolder(dir, path string) bool {
	sep := string(filepath.Separator)
	if !strings.HasSuffix(dir, sep) {
		dir += sep
	}
	if len(path) <= len(dir) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(path[:len(dir)], dir)
	}
	return strings.HasPrefix(path, dir)
}

// changeCount is how many files differ from HEAD, or Git does not track.
func (a *gitContext) changeCount() int {
	if a.git.status == nil {
		return 0
	}
	return len(a.git.status.Entries)
}

// scmView is the sidebar's source control view: the branch, the commit
// message, and the files changed, staged and not.
func (a *gitContext) scmView(c *ui.Context) {
	g := a.git
	a.scmActivity(c)
	if g.repo == nil {
		if g.looked {
			a.app.scmNoRepo(c)
		}
		return
	}
	a.scmBranchRow(c)
	a.scmCommitControls(c)
	a.scmList(c)
}

// scmCommitControls is shared by the single-repository view and the rows of
// the multi-repository list. It always acts on this repository.
func (a *gitContext) scmCommitControls(c *ui.Context) {
	p, g := a.app.pal, a.git
	ui.Column(c).FillWidth().Padding(2, 12, 10, 12).Gap(8).Children(func() {
		if g.amend {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 4, 4, 8).Radius(5).Background(p.accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconPencil).FontSize(12).TextColor(p.accent)
				ui.Text(c, locales.ScmAmending()).FontSize(11.5).TextColor(p.text).Grow(1).SingleLine().Shrink(1)
				b := ui.ButtonBase(c.Key("stop-amending")).Padding(1, 6).Radius(4).Label(locales.ScmStopAmending())
				if b.Hovered() {
					b.Background(p.hover)
				}
				b.Transition(hoverFade)
				b.Children(func() { ui.Text(c, locales.ButtonCancel()).FontSize(11.5).FontWeight(500).TextColor(p.accent) })
				if b.Clicked() {
					g.amend, g.message = false, ""
				}
			})
		}
		if st := g.status; st != nil && st.Op != "" {
			a.scmOpBanner(c, st.Op)
		}
		branch := a.branch()
		enter := keyText("CmdOrCtrl+Enter")
		hint := locales.ScmMessageHint(enter)
		if branch != "" {
			hint = locales.ScmMessageHintOn(enter, branch)
		}
		if g.writing != nil {
			hint = locales.ScmWritingHint()
		}
		ui.Box(c.Key("commit-box")).Children(func() {
			box := a.app.textKeys(ui.TextArea(c.Key("commit-message"), &g.message)).Placeholder(hint).FontSize(12.5).
				Lines(1, 8).MinHeight(28).MaxHeight(160).Padding(5, 8).Label(locales.ScmCommitMessage())
			button := a.app.host.ai() != nil || g.writing != nil
			if button {
				box.PaddingEnd(28) // clear of the button at its corner
			}
			if g.message == "" {
				// Lines measures the text, not the placeholder: the hint, as it
				// wraps, sets the height too, so a long branch name shows whole.
				width := box.Bounds().W
				if width <= 0 {
					width = a.app.window.SidebarWidth - 24
				}
				inset := float32(8 + 8 + 2) // the padding and the border
				if button {
					inset += 28 - 8
				}
				_, height := c.MeasureText(max(width-inset, 1), ui.Span{Text: hint, Size: 12.5})
				box.MinHeight(max(28, height+5+5+2))
			}
			if g.writing != nil {
				box.ReadOnly(true)
			}
			if box.Shortcut(ui.Cmd, ui.KeyEnter) {
				a.commit(false)
			}
			a.writeButton(c)
		})
		a.subjectLength(c)
		a.commitButton(c)
	})
}

func (a *gitContext) scmList(c *ui.Context) {
	p, g := a.app.pal, a.git
	if a.scm.section != scmChanges {
		a.refreshScmPanels()
	}
	rows := a.scmRows()
	s := a.scm
	if a.scm.section != scmHistory && g.status != nil && len(g.status.Entries) == 0 {
		if a.scm.section == scmChanges {
			ui.Text(c, locales.DiffNoChanges()).FontSize(12).TextColor(p.muted).Padding(6, 14, 12, 14)
		} else {
			a.scmClean(c)
		}
	}
	if len(rows) == 0 {
		return
	}
	s.list.Selected = &s.row
	s.list.Header = func(i int) bool { return i < len(rows) && rows[i].header }
	s.list.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		return rows[i].key
	}
	s.list.Label = func(i int) string {
		switch r := rows[i]; {
		case r.panel != nil:
			return r.panel.label
		case r.header:
			return ""
		case r.note != "":
			return r.note
		case r.dir != "":
			return r.name
		case r.commit != nil:
			return r.commit.Subject
		case r.more:
			return locales.ScmShowMoreCommits()
		}
		return filepath.Base(rows[i].e.Path)
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	if s.row >= 0 && s.row < len(rows) && rows[s.row].file() && !s.selection.Has(rows[s.row].key) {
		quiet.Accent = p.chrome
	}
	c.SetTheme(&quiet)
	var action func()
	var out ui.Element
	buildList := func() {
		out = ui.List(c.Key("scm"), &s.list, len(rows), func(i int) {
			switch r := rows[i]; {
			case r.panel != nil:
				a.scmPanelRowView(c, r, &action)
			case r.header:
				a.scmHeader(c, r, &action)
			case r.commit != nil:
				a.scmCommit(c, i, r, &action)
			case r.under != nil:
				a.scmCommitFile(c, i, r, &action)
			case r.more:
				a.scmMore(c, &action)
			case r.dir != "":
				a.scmFolder(c, r, &action)
			default:
				a.scmFile(c, r, &action)
			}
		}).Grow(1).Padding(0, 6, 8, 6)
		if s.section == scmChanges {
			out.Fill()
		}
	}
	if s.section == scmChanges {
		ui.Box(c.Key("change-list")).FillWidth().Height(min(float32(len(rows))*22+8, 360)).Shrink(0).Children(buildList)
	} else {
		buildList()
	}
	c.SetTheme(base)
	if action != nil {
		action()
	}
	a.scmListInput(out, rows, s.list.ScrollIntoView)
}

// scmListInput keeps keyboard selection and actions scoped to one repository,
// whether its rows have a list of their own or share the multi-repository list.
func (a *gitContext) scmListInput(out ui.Element, rows []scmRow, reveal func(int)) {
	s, g := a.scm, a.git
	chosen := func() (scmRow, bool) {
		if s.row >= 0 && s.row < len(rows) && rows[s.row].file() {
			return rows[s.row], true
		}
		return scmRow{}, false
	}
	if out.Changed() {
		if _, ok := chosen(); ok {
			a.selectScmRow(s.row, 0)
		} else {
			s.selection.Clear()
			s.anchor = ""
		}
	}
	for _, key := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd} {
		if !out.Shortcut(ui.Shift, key) {
			continue
		}
		step, start := 1, s.row+1
		switch key {
		case ui.KeyUp:
			step, start = -1, s.row-1
		case ui.KeyHome:
			start = 0
		case ui.KeyEnd:
			step, start = -1, len(rows)-1
		}
		for i := start; i >= 0 && i < len(rows); i += step {
			if rows[i].file() {
				a.selectScmRow(i, ui.Shift)
				reveal(i)
				break
			}
		}
	}
	if out.Shortcut(ui.Cmd, ui.KeyA) {
		if _, ok := chosen(); !ok {
			for i, r := range rows {
				if r.file() {
					s.row, s.anchor = i, r.key
					break
				}
			}
		}
		for _, r := range rows {
			if r.file() {
				s.selection.Add(r.key)
			}
		}
	}
	if s.row >= 0 && s.row < len(rows) && out.Submitted() {
		switch r := rows[s.row]; {
		case r.panel != nil:
			a.activateScmPanelRow(r)
		case r.under != nil && r.dir == "" && r.note == "":
			a.showCommitFile(*r.under, r.e.Path, true)
		case r.dir != "":
			a.toggleDir(r)
		case r.commit != nil:
			a.showCommit(*r.commit, true)
		case r.more:
			g.historyN = max(g.historyN, historyPage) + historyPage
		}
	}
	if r, ok := chosen(); ok && out.Submitted() {
		a.openChange(r, true)
	}
	// Right shows the files of the commit chosen, or opens the folder of
	// them; Left hides them, or closes the folder.
	if s.row >= 0 && s.row < len(rows) {
		r := rows[s.row]
		right, left := out.Shortcut(0, ui.KeyRight), out.Shortcut(0, ui.KeyLeft)
		switch {
		case r.commit != nil && r.at >= 0:
			if right && !s.openCommits[r.commit.Hash] || left && s.openCommits[r.commit.Hash] {
				a.toggleCommitOpen(*r.commit)
			}
		case r.under != nil && r.dir != "" && (right && s.closedDirs[r.key] || left && !s.closedDirs[r.key]):
			a.toggleDir(r)
		case r.under != nil && left:
			for k := s.row - 1; k >= 0; k-- {
				if rows[k].commit == r.under {
					s.row = k
					s.list.ScrollIntoView(k)
					break
				}
			}
		}
	}
	if r, ok := chosen(); ok && out.Shortcut(0, ui.KeySpace) {
		a.toggleStaged(r)
	}
}

// scmOpBanner says which command stopped for conflicts, and how many
// are left, with the buttons that go on with it and undo it.
func (a *gitContext) scmOpBanner(c *ui.Context, op git.Op) {
	p := a.app.pal
	g := a.git
	conflicts := len(a.groupEntries()[groupMerge])
	label := locales.ScmOpInProgress(opTitle(op))
	if conflicts > 0 {
		label += " · " + locales.ScmConflicts(conflicts)
	}
	tint := p.accent
	if conflicts > 0 {
		tint = p.danger
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 4, 4, 8).Radius(5).Background(tint.Alpha(0.12)).Children(func() {
		ui.Icon(c, iconBranch).FontSize(12).TextColor(tint)
		ui.Text(c, label).FontSize(11.5).TextColor(p.text).Grow(1).SingleLine().Shrink(1)
		button := func(key, text, tip string) bool {
			b := ui.ButtonBase(c.Key(key)).Padding(1, 6).Radius(4).Label(tip).Disabled(g.busy != "")
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() { ui.Text(c, text).FontSize(11.5).FontWeight(500).TextColor(p.accent) })
			return b.Clicked()
		}
		if button("op-abort", locales.ButtonAbort(), locales.ScmAbortOp(opTitle(op))) {
			a.app.post(a.abortOp)
		}
		if button("op-continue", locales.ScmContinue(), locales.ScmContinueOp(opTitle(op))) {
			a.app.post(a.continueOp)
		}
	})
}

// scmActivity is a thin bar under the view's title, which moves while a
// Git command runs in the background, and is empty room while none does.
func (a *gitContext) scmActivity(c *ui.Context) {
	g := a.git
	if g.busy == "" && g.pending == 0 {
		ui.Box(c.Key("scm-activity")).Height(2)
		return
	}
	ui.Progress(c.Key("scm-activity"), -1).Height(2).Radius(0).Label(locales.ScmRunningGit())
}

// subjectLimit is how long the first line of a commit message should be,
// as Git's tools show it: past it, the view says how long it is.
const (
	subjectHint  = 50
	subjectLimit = 72
)

// subjectLength says how long the first line of the message is once it
// grows past what a summary should be, in a warning's color past what
// Git's tools show.
func (a *gitContext) subjectLength(c *ui.Context) {
	p := a.app.pal
	subject, _, _ := strings.Cut(a.git.message, "\n")
	n := utf8.RuneCountInString(strings.TrimRight(subject, " \t\r"))
	if n <= subjectHint {
		return
	}
	col, note := p.faint, locales.ScmSummary()
	if n > subjectLimit {
		col, note = p.modifiedFg, locales.ScmSummaryLong()
	}
	ui.Row(c).Margin(-4, 2, 0, 2).Children(func() {
		ui.Text(c, note).FontSize(11).TextColor(col).Grow(1).SingleLine()
		ui.Text(c, fmt.Sprintf("%d/%d", n, subjectLimit)).FontSize(11).Font(a.app.font.family).TextColor(col)
	})
}

// commitButton is the button that commits, which says what it commits,
// joined to one with a menu of the other ways to commit.
func (a *gitContext) commitButton(c *ui.Context) {
	g := a.git
	t := c.Theme()
	groups := a.groupEntries()
	staged := len(groups[groupStaged])
	others := len(groups[groupChanges]) + len(groups[groupUntracked])
	label := locales.CmdGitCommit()
	switch {
	case g.amend:
		label = locales.ScmAmendCommit()
	case staged == 0 && others > 0:
		label = locales.ScmCommitAll()
	}
	tip := locales.ScmCommitTip()
	switch {
	case g.amend:
		tip = locales.ScmAmendTip()
	case staged == 0 && others > 0:
		tip = locales.ScmCommitAllTip()
	}
	tip += " (" + keyText("CmdOrCtrl+Enter") + ")"
	committing := g.busy == locales.BusyCommitting()
	idle := g.busy == "" && (g.amend || staged+others > 0)
	// In the accent color while it commits, or can; gray while there is
	// nothing to commit or another command runs, its menu still there.
	bg, hover, pressed, fg := t.Accent, t.AccentHover, t.AccentPressed, t.AccentText
	if !idle && !committing {
		p := a.app.pal
		bg, fg = p.text.Alpha(0.06).Over(p.chrome), p.faint
		hover, pressed = p.text.Alpha(0.1).Over(p.chrome), p.text.Alpha(0.13).Over(p.chrome)
	}
	ui.Row(c.Key("commit")).Height(28).Children(func() {
		main := ui.ButtonBase(c.Key("commit-main")).Grow(1).Height(28).Radius(6, 0, 0, 6).Justify(ui.Center).AlignItems(ui.Center).Gap(6).
			Background(bg).Label(label).Tooltip(tip).Disabled(!idle)
		switch {
		case !idle:
		case main.Pressed():
			main.Background(pressed)
		case main.Hovered():
			main.Background(hover)
		}
		main.Transition(hoverFade)
		main.Children(func() {
			if committing {
				ui.Spinner(c).Size(12, 12)
				ui.Text(c, locales.BusyCommitting()).FontSize(12.5).FontWeight(600).TextColor(fg).SingleLine()
				return
			}
			ui.Icon(c, iconCheck).FontSize(14).TextColor(fg)
			ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(fg).SingleLine()
		})
		if main.Clicked() && idle {
			a.commit(false)
		}
		ui.Box(c).Width(1).Height(28).Background(fg.Alpha(0.28).Over(bg))
		more := ui.ButtonBase(c.Key("commit-more")).Width(28).Height(28).Radius(0, 6, 6, 0).Justify(ui.Center).AlignItems(ui.Center).
			Background(bg).Label(locales.ScmMoreCommitActions()).Tooltip(locales.ScmMoreCommitActions())
		switch {
		case more.Pressed():
			more.Background(pressed)
		case more.Hovered():
			more.Background(hover)
		}
		more.Transition(hoverFade)
		more.Children(func() { ui.Icon(c, iconChevronDown).FontSize(14).TextColor(fg) })
		more.Menu(a.commitMenu)
	})
}

// commitMenu is the menu of the ways to commit.
func (a *gitContext) commitMenu(m *ui.Menu) {
	g := a.git
	st := g.status
	busy := g.busy != ""
	none := st == nil || len(st.Entries) == 0 && !g.amend
	if m.Item(locales.CmdGitCommit()).Shortcut(ui.Cmd, ui.KeyEnter).Disabled(busy || none).Chosen() {
		a.app.post(func() { a.commit(false) })
	}
	if m.Item(locales.CmdGitCommitPush()).Disabled(busy || none).Chosen() {
		a.app.post(func() { a.commit(true) })
	}
	m.Separator()
	if m.Item(locales.CmdGitAmend()).Checked(g.amend).Disabled(busy || st == nil || st.Head == "").Chosen() {
		a.app.post(func() {
			if g.amend {
				g.amend, g.message = false, ""
				return
			}
			a.startAmend()
		})
	}
	if m.Item(locales.CmdGitUndoCommit()).Disabled(busy || st == nil || st.Head == "").Chosen() {
		a.app.post(a.undoCommit)
	}
}

// scmNoRepo is the view of a folder outside a work tree: what that means,
// and a button to make it one.
func (a *app) scmNoRepo(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(28, 20, 16, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconBranch).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, locales.ScmNoRepo()).FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, locales.ScmNoRepoDetail()).
			FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		ui.Box(c).Height(4)
		if ui.PrimaryButton(c, locales.ScmInitRepo()).FillWidth().Clicked() {
			a.initRepo()
		}
	})
}

// scmClean is the view of a work tree with no changes: that it is clean,
// and how its branch stands with its upstream, with buttons to bring them
// level.
func (a *gitContext) scmClean(c *ui.Context) {
	p := a.app.pal
	g := a.git
	st := g.status
	ui.Column(c).Padding(24, 20, 16, 20).Gap(6).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.added.Alpha(0.13)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconCheck).FontSize(20).TextColor(p.added)
		})
		ui.Text(c, locales.DiffNoChanges()).FontSize(13).FontWeight(600).TextColor(p.text).Margin(6, 0, 0, 0)
		note := locales.ScmTreeClean()
		switch {
		case st.Branch == "":
		case st.Upstream == "" && len(g.remotes) == 0:
			note = locales.ScmBranchClean(st.Branch)
		case st.Upstream == "":
			note = locales.ScmBranchUnpublished(st.Branch)
		case st.Ahead > 0 && st.Behind > 0:
			note = locales.ScmBranchAheadBehind(st.Branch, st.Ahead, st.Upstream, st.Behind)
		case st.Ahead > 0:
			note = locales.ScmBranchAhead(st.Branch, st.Ahead, st.Upstream)
		case st.Behind > 0:
			note = locales.ScmBranchBehind(st.Branch, st.Behind, st.Upstream)
		default:
			note = locales.ScmBranchUpToDate(st.Branch, st.Upstream)
		}
		ui.Text(c, note).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		busy := g.busy != ""
		button := func(icon *ui.SVG, label string, primary bool, fn func()) {
			ui.Box(c).Height(4)
			var b ui.Element
			if primary {
				b = ui.PrimaryButton(c.Key(label), "")
			} else {
				b = ui.Button(c.Key(label), "")
			}
			b.FillWidth().Justify(ui.Center).Disabled(busy).Label(label).Children(func() {
				ui.Icon(c, icon).FontSize(14)
				ui.Text(c, label).SingleLine()
			})
			if b.Clicked() {
				fn()
			}
		}
		switch {
		case st.Branch == "":
		case st.Upstream == "" && len(g.remotes) > 0:
			button(iconPush, locales.ScmPublishBranch(), true, a.push)
		case st.Upstream == "":
		case st.Behind > 0:
			button(iconPull, locales.ScmPullCommits(st.Behind), true, a.pull)
			if st.Ahead > 0 {
				button(iconPush, locales.ScmPushCommits(st.Ahead), false, a.push)
			}
		case st.Ahead > 0:
			button(iconPush, locales.ScmPushCommits(st.Ahead), true, a.push)
		}
	})
}

// scmHeader is the row heading a group: a click opens or closes it.
func (a *gitContext) scmHeader(c *ui.Context, r scmRow, action *func()) {
	p := a.app.pal
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(scmRowHeight).Background(p.chrome)
	hovered := row.Hovered()
	clicked := row.Clicked()
	row.Children(func() {
		chevron(c, !a.scm.collapsed[r.group]).FontSize(12).TextColor(p.muted)
		ui.Text(c, strings.ToUpper(groupName(r.group))).FontSize(11).FontWeight(600).LetterSpacing(0.4).
			TextColor(p.muted).SingleLine().Shrink(1)
		ui.Spacer(c)
		if r.group == groupCommits && len(a.app.repositories.contexts) > 1 {
			if a.app.repositoryButton(c, false) {
				*action = func() {}
			}
		}
		if hovered {
			paths := a.groupPaths(r.group)
			switch r.group {
			case groupStaged:
				if a.app.smallButton(c, iconFiles, locales.ScmOpenStaged()) {
					*action = func() { a.showChanges(diffTarget{base: diffStaged}, true) }
				}
				if a.app.smallButton(c, iconMinus, locales.ScmUnstageAll()) {
					*action = func() { a.unstage(paths...) }
				}
			case groupChanges, groupUntracked:
				if a.app.smallButton(c, iconFiles, locales.CmdGitOpenChanges()) {
					*action = func() { a.showChanges(diffTarget{base: diffIndex}, true) }
				}
				if a.app.smallButton(c, iconUndo, locales.ScmDiscardAll()) {
					*action = func() { a.discard(a.groupEntries()[r.group]...) }
				}
				if a.app.smallButton(c, iconPlus, locales.CmdGitStageAll()) {
					*action = func() { a.stage(paths...) }
				}
			case groupMerge:
				if a.app.smallButton(c, iconPlus, locales.ScmStageAllMerge()) {
					*action = func() { a.stage(paths...) }
				}
			}
		}
		// The toggle of every branch stays, in the accent, while it is on.
		if all := a.app.settings.ScmAllBranches; r.group == groupCommits && (hovered || all) {
			tip := locales.ScmShowAllBranches()
			if all {
				tip = locales.ScmShowCurrentBranch()
			}
			b := ui.ButtonBase(c.Key("commits:all")).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
				Label(tip).Tooltip(tip)
			col := p.muted
			if b.Hovered() {
				b.Background(p.hover)
				col = p.text
			}
			if all {
				col = p.accent
			}
			b.Transition(hoverFade)
			pressPose(b)
			b.Children(func() { ui.Icon(c, iconBranch).FontSize(13).TextColor(col) })
			if b.Clicked() {
				*action = a.toggleAllBranches
			}
		}
		ui.Text(c, fmt.Sprint(r.count)).FontSize(10.5).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.hover).
			ContentTransition(ui.ContentTransition{Numeric: true})
	})
	if clicked && *action == nil {
		*action = func() { a.scm.collapsed[r.group] = !a.scm.collapsed[r.group] }
	}
}

// scmFile is the row of a changed file: a click shows its changes.
func (a *gitContext) scmFile(c *ui.Context, r scmRow, action *func()) {
	p := a.app.pal
	e := r.e
	indent := float32(14)
	tree := a.app.settings.ScmTree
	if tree {
		indent = treeIndent(r.depth) + 16
	}
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(scmRowHeight).Padding(0, 0, 0, indent)
	if tree {
		a.app.scmGuides(row, r.depth)
	}
	if a.scm.selection.Has(r.key) {
		row.Background(p.active)
	}
	hovered := row.Hovered()
	clicked := row.Clicked()
	row.ContextMenu(func(m *ui.Menu) { a.scmFileMenu(m, r, action) })
	letter := e.Letter()
	if e.Submodule() {
		row.Tooltip(submoduleDescription(e, r.group == groupStaged))
	}
	if r.group == groupStaged {
		letter = e.X
	}
	if letter != 'D' && !e.Submodule() {
		a.app.dragFile(row, &fileDrag{path: e.Path})
	}
	row.Children(func() {
		name := filepath.Base(e.Path)
		icon, col := entryIcon(p, e, name)
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		label := ui.Text(c, name).FontSize(13).SingleLine().Shrink(0).TextColor(gitColor(p, letter))
		if letter == 'D' {
			label.Strikethrough()
		}
		if dir := filepath.Dir(a.git.repo.Rel(e.Path)); dir != "." && !tree {
			ui.Text(c, dir).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
		}
		if e.Submodule() {
			state := locales.ScmSubmoduleContentLabel()
			if r.group == groupStaged || len(e.Sub) == 4 && e.Sub[1] == 'C' {
				state = locales.ScmSubmoduleCommitLabel()
				if r.group != groupStaged && e.Dirty() {
					state += " · " + locales.ScmSubmoduleContentLabel()
				}
			}
			ui.Text(c, state).FontSize(10.5).TextColor(p.muted).SingleLine().Shrink(2)
		}
		ui.Spacer(c)
		if hovered {
			if letter != 'D' && a.app.smallButton(c, iconOpenFile, openLabel(e)) {
				*action = func() { a.app.openPath(e.Path, true) }
			}
			switch r.group {
			case groupStaged:
				if a.app.smallButton(c, iconMinus, locales.UnstageChanges()) {
					*action = func() { a.toggleStaged(r) }
				}
			case groupMerge:
				if a.app.smallButton(c, iconMerge, locales.MergeSolveConflicts()) {
					*action = func() { a.app.openMerge(e.Path, true) }
				}
			default:
				if r.group != groupMerge && !e.Submodule() && a.app.smallButton(c, iconUndo, locales.DiscardChangesEllipsis()) {
					*action = func() { a.discardChosen(r) }
				}
				if a.app.smallButton(c, iconPlus, locales.StageChanges()) {
					*action = func() { a.toggleStaged(r) }
				}
			}
		}
		ui.Text(c, string(letter)).FontSize(11).FontWeight(600).Font(a.app.font.family).Padding(0, 4, 0, 2).
			TextColor(gitColor(p, letter)).Shrink(0)
	})
	if clicked && *action == nil {
		mods := row.ClickModifiers()
		*action = func() {
			for i, row := range a.scmRows() {
				if row.key == r.key {
					a.selectScmRow(i, mods)
					break
				}
			}
			if mods == 0 {
				a.openChange(r, true)
			}
		}
	}
}

// selectScmRow keeps a file's range anchor independent of the diff preview
// and the list's keyboard cursor. File rows also handle dragging, so their
// pointer clicks do not reach the list's built-in selection handler.
func (a *gitContext) selectScmRow(i int, mods ui.Modifiers) {
	s := a.scm
	rows := a.scmRows()
	if i < 0 || i >= len(rows) || !rows[i].file() {
		return
	}
	r := rows[i]
	anchor := -1
	for j, row := range rows {
		if row.key == s.anchor && row.file() {
			anchor = j
			break
		}
	}
	switch {
	case mods&ui.Shift != 0 && anchor >= 0:
		s.selection.Clear()
		for j := min(anchor, i); j <= max(anchor, i); j++ {
			if rows[j].file() {
				s.selection.Add(rows[j].key)
			}
		}
	case mods&ui.Cmd != 0:
		if s.selection.Has(r.key) {
			s.selection.Remove(r.key)
		} else {
			s.selection.Add(r.key)
		}
		s.anchor = r.key
	default:
		s.selection.Clear()
		s.selection.Add(r.key)
		s.anchor = r.key
	}
	s.row = i
}

// scmCommit is the row of a commit of the history: its lanes of the
// graph, who made it, its branches and tags, its subject, and how long
// ago it was made. A click chooses row i and shows the commit's changes
// in the preview tab, which Enter or a double click keeps.
func (a *gitContext) scmCommit(c *ui.Context, i int, r scmRow, action *func()) {
	p := a.app.pal
	cm := r.commit
	g := a.git
	unpushed := g.unpushed[cm.Hash]
	head := g.status != nil && g.status.Head == cm.Hash
	badges := refBadges(cm.Refs)
	tip := fmt.Sprintf("%s\n%s · %s · %s", cm.Subject, cm.Author, ago(cm.Time, time.Now()), cm.Short())
	if unpushed {
		tip += locales.ScmNotPushed()
	}
	if len(cm.Parents) > 1 {
		tip += locales.ScmMerge()
	}
	if len(badges) > 0 {
		names := make([]string, len(badges))
		for i, b := range badges {
			names[i] = b.label()
		}
		tip += "\n" + strings.Join(names, ", ")
	}
	indent := float32(6)
	if r.at < 0 {
		indent = scmPanelContentIndent(r.depth)
	}
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(scmRowHeight).Padding(0, 4, 0, indent).Tooltip(tip)
	clicked := row.Clicked()
	row.ContextMenu(func(m *ui.Menu) { a.scmCommitMenu(m, cm, head, action) })
	open := a.scm.openCommits[cm.Hash]
	graph := a.historyGraph()
	var gr *graphRow
	if r.at >= 0 && r.at < len(graph) {
		gr = &graph[r.at]
	}
	lanes := min(graphLanes(graph), graphMaxLanes)
	row.Children(func() {
		if r.at >= 0 {
			// The arrow that shows the files of the commit under it.
			label := locales.ScmShowCommitFiles()
			if open {
				label = locales.ScmHideCommitFiles()
			}
			b := ui.ButtonBase(c.Key("files:"+cm.Hash)).Size(12, 22).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).
				Label(label).Tooltip(label)
			b.Children(func() { chevron(c, open).FontSize(12).TextColor(p.muted) })
			if b.Clicked() {
				*action = func() { a.toggleCommitOpen(*cm) }
			}
			ui.Box(c).Size(graphLaneW*float32(lanes-1)+12, 22).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
				if gr != nil {
					a.app.paintGraphRow(pt, b, gr, cm, head)
				}
			})
		}
		author := avatarColor(p, cm.Email+cm.Author)
		ui.Box(c).Size(15, 15).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
			pt.Fill(b, author.Alpha(0.22), b.W/2)
			paintInitials(pt, b, cm.Author, 7, author)
		})
		lane := p.accent
		if gr != nil {
			lane = graphColor(p, gr.color)
		}
		// The row's lead, before its badges: its indent, the graph and
		// the author.
		lead := indent + 15 + 6
		if r.at >= 0 {
			lead += 12 + 6 + graphLaneW*float32(lanes-1) + 12 + 6
		}
		shown, badgeW := a.app.badgeRoom(len(badges), indent-6, lead)
		for i, bd := range badges {
			if i == shown && len(badges) > shown {
				ui.Text(c, fmt.Sprintf("+%d", len(badges)-i)).FontSize(10.5).TextColor(p.muted).Padding(0, 4).
					Height(16).Radius(4).Background(p.hover).Shrink(0)
				break
			}
			a.app.refBadge(c, bd, lane, badgeW)
		}
		col := p.text
		switch {
		case len(cm.Parents) > 1:
			col = p.muted
		case !unpushed && !head:
			col = mix(p.text, p.muted, 0.35)
		}
		ui.Text(c, cm.Subject).FontSize(12.5).TextColor(col).SingleLine().Shrink(1).MinWidth(24)
		ui.Spacer(c)
		if unpushed {
			ui.Icon(c, iconArrowUp).FontSize(11).TextColor(p.accent).Shrink(0)
		}
		ui.Text(c, shortAgo(cm.Time, time.Now())).FontSize(11).TextColor(p.faint).Shrink(0).Padding(0, 2, 0, 0)
	})
	if clicked && *action == nil {
		*action = func() {
			a.scm.selection.Clear()
			a.scm.anchor = ""
			a.scm.row = i
			a.previewCommit(*cm)
		}
	}
}

// graphLaneW is how far apart the lanes of the history's graph are, and
// graphMaxLanes how many it shows: those past it run along its edge.
const (
	graphLaneW    = 10
	graphMaxLanes = 6
)

// badgeRoom is how many of n badges a commit's row shows, and how wide
// each is at most: those that fit beside the start of its subject and its
// time, past its lead, sharing the room, none narrower than a few letters.
func (a *app) badgeRoom(n int, indented, lead float32) (shown int, w float32) {
	const gap, more, least, widest = 6, 30, 48, 110
	shown = min(a.badgesShown(indented), n)
	room := a.window.SidebarWidth - lead - 110
	for ; shown > 0; shown-- {
		r := room
		if shown < n {
			r -= more
		}
		if w = r/float32(shown) - gap; w >= least || shown == 1 {
			return shown, min(max(w, 40), widest)
		}
	}
	return 0, widest
}

// badgesShown is how many branches and tags a commit's row shows before
// it counts the rest: more as the sidebar widens, fewer as the row is
// indented past the history's, so its subject and time keep their room.
func (a *app) badgesShown(indented float32) int {
	return min(max(1+int((a.window.SidebarWidth-indented-240)/90), 1), 3)
}

// historyGraph returns the lanes of the history, laid out once for each
// read of it.
func (a *gitContext) historyGraph() []graphRow {
	g := a.git
	if g.graph == nil || g.graphOf != g.historyGen {
		g.graph, g.graphOf = layoutGraph(g.history), g.historyGen
	}
	return g.graph
}

// graphColor is the color of a lane of the graph: the accent, then
// the terminal's yellow, green, red, magenta and cyan.
func graphColor(p *palette, i int) ui.Color {
	cols := [...]ui.Color{p.accent, p.ansi[3], p.ansi[2], p.ansi[1], p.ansi[5], p.ansi[6]}
	return cols[i%len(cols)]
}

// paintGraphRow paints a row's lanes in b: those that lead to the commit
// down to its dot, those to its parents from it, and the rest straight
// through; then the dot, a ring for HEAD and a hollow one for a merge.
func (a *app) paintGraphRow(pt *ui.Painter, b ui.Rect, gr *graphRow, cm *git.Commit, head bool) {
	p := a.pal
	x := func(i int) float32 { return b.X + 6 + graphLaneW*float32(min(i, graphMaxLanes-1)) }
	top, mid, bottom := b.Y, b.Y+b.H/2, b.Y+b.H
	line := func(x1, y1, x2, y2 float32, col ui.Color) {
		var path ui.Path
		path.MoveTo(x1, y1)
		if x1 == x2 {
			path.LineTo(x2, y2)
		} else {
			ym := (y1 + y2) / 2
			path.CubeTo(x1, ym, x2, ym, x2, y2)
		}
		pt.StrokePath(&path, 1.5, col)
	}
	for i, l := range gr.above {
		if l.hash == "" {
			continue
		}
		if l.hash == cm.Hash {
			line(x(i), top, x(gr.lane), mid, graphColor(p, l.color))
		} else {
			line(x(i), top, x(i), mid, graphColor(p, l.color))
		}
	}
	for i, l := range gr.below {
		if l.hash == "" {
			continue
		}
		through := i != gr.lane && i < len(gr.above) && gr.above[i].hash == l.hash
		if through || !gr.from[i] {
			line(x(i), mid, x(i), bottom, graphColor(p, l.color))
		}
		if gr.from[i] {
			line(x(gr.lane), mid, x(i), bottom, graphColor(p, l.color))
		}
	}
	col := graphColor(p, gr.color)
	cx := x(gr.lane)
	switch {
	case head:
		dot := ui.Rect{X: cx - 5, Y: mid - 5, W: 10, H: 10}
		pt.Fill(dot, p.chrome, 5)
		pt.Stroke(dot, col, 5, 2)
	case gr.merge:
		dot := ui.Rect{X: cx - 3.5, Y: mid - 3.5, W: 7, H: 7}
		pt.Fill(dot, p.chrome, 3.5)
		pt.Stroke(dot, col, 3.5, 1.5)
	default:
		pt.Fill(ui.Rect{X: cx - 4, Y: mid - 4, W: 8, H: 8}, col, 4)
	}
}

// refBadge is a branch or tag a commit's row shows: a branch of a remote
// at the same commit as the local one of its name goes in the local's,
// its remotes listed.
type refBadge struct {
	git.Ref
	remotes []string
}

// label is what a badge says, as "main ⇄ origin".
func (b refBadge) label() string {
	if len(b.remotes) == 0 {
		return b.Name
	}
	return b.Name + " ⇄ " + strings.Join(b.remotes, ", ")
}

// refBadges returns the badges of a commit's refs: HEAD when detached,
// the branch checked out, the other branches, those of remotes, then
// the tags.
func refBadges(refs []git.Ref) []refBadge {
	local := map[string]int{}
	var out []refBadge
	rank := func(r git.Ref) int {
		switch {
		case r.Kind == git.RefHead:
			return 0
		case r.Head:
			return 1
		case r.Kind == git.RefBranch:
			return 2
		case r.Kind == git.RefRemote:
			return 3
		}
		return 4
	}
	sorted := slices.Clone(refs)
	slices.SortStableFunc(sorted, func(x, y git.Ref) int { return rank(x) - rank(y) })
	for _, r := range sorted {
		if r.Kind == git.RefRemote {
			if remote, name, ok := strings.Cut(r.Name, "/"); ok {
				if i, ok := local[name]; ok {
					out[i].remotes = append(out[i].remotes, remote)
					continue
				}
			}
		}
		if r.Kind == git.RefBranch {
			local[r.Name] = len(out)
		}
		out = append(out, refBadge{Ref: r})
	}
	return out
}

// refBadge shows a branch or tag of a commit's row, in its lane's color:
// a branch of a remote fainter, and a tag in the plain colors.
func (a *app) refBadge(c *ui.Context, b refBadge, lane ui.Color, maxW float32) {
	p := a.pal
	fg, bg, icon := lane, lane.Alpha(0.16), iconBranch
	switch b.Kind {
	case git.RefRemote:
		fg, bg = mix(lane, p.muted, 0.4), lane.Alpha(0.08)
	case git.RefTag:
		fg, bg, icon = p.text, p.hover, iconTag
	case git.RefHead:
		icon = nil
	}
	weight := 500
	if b.Head || b.Kind == git.RefHead {
		weight = 700
	}
	ui.Row(c).Gap(3).AlignItems(ui.Center).Height(16).Padding(0, 5, 0, 4).Radius(4).Background(bg).
		MaxWidth(maxW).Shrink(0).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(10).TextColor(fg).Shrink(0)
		}
		ui.Text(c, b.label()).FontSize(11).FontWeight(weight).TextColor(fg).SingleLine().Shrink(1)
	})
}

// scmCommitMenu is the menu of a commit of the history, in the order of
// Sourcetree's: checking it out and bringing it into the branch, refs at
// it, moving the branch by it, going on with a command stopped for
// conflicts, then copying.
func (a *gitContext) scmCommitMenu(m *ui.Menu, cm *git.Commit, head bool, action *func()) {
	g := a.git
	busy := g.busy != ""
	var op git.Op
	if g.status != nil {
		op = g.status.Op
	}
	// The commands that move HEAD by commits wait for one stopped.
	moving := busy || op != ""
	branch := a.branch()
	in := a.inHead(cm.Hash)
	if m.Item(locales.CmdGitOpenChanges()).Chosen() {
		*action = func() { a.showCommit(*cm, true) }
	}
	m.Separator()
	if m.Item(locales.ScmCheckOutEllipsis()).Disabled(moving || head).Chosen() {
		*action = func() { a.checkOutCommit(*cm) }
	}
	if m.Item(locales.ScmMergeInto(branch)).Disabled(moving || in).Chosen() {
		*action = func() { a.mergeCommit(*cm) }
	}
	if m.Item(locales.ScmRebaseOnto(branch)).Disabled(moving || in).Chosen() {
		*action = func() { a.rebaseOnto(*cm) }
	}
	m.Separator()
	if m.Item(locales.ScmCreateTagHere()).Disabled(busy).Chosen() {
		*action = func() { a.askRefAt(*cm, true) }
	}
	if m.Item(locales.ScmCreateBranchHere()).Disabled(busy).Chosen() {
		*action = func() { a.askRefAt(*cm, false) }
	}
	m.Separator()
	if m.Item(locales.ScmResetTo(branch)).Disabled(moving || head).Chosen() {
		*action = func() { a.resetTo(*cm) }
	}
	if m.Item(locales.ScmRevertCommit()).Disabled(moving).Chosen() {
		*action = func() { a.revertCommit(*cm) }
	}
	if m.Item(locales.ScmCherryPickCommit()).Disabled(moving || in).Chosen() {
		*action = func() { a.cherryPick(*cm) }
	}
	if op != "" {
		m.Separator()
		if m.Item(locales.ScmContinueOp(opTitle(op))).Disabled(busy).Chosen() {
			*action = a.continueOp
		}
		if m.Item(locales.ScmAbortOp(opTitle(op))).Disabled(busy).Chosen() {
			*action = a.abortOp
		}
	}
	m.Separator()
	if m.Item(locales.CopyCommitHash()).Chosen() {
		a.app.services.WriteClipboard(cm.Hash)
	}
	if m.Item(locales.ScmCopyCommitMessage()).Chosen() {
		msg := cm.Subject
		if cm.Body != "" {
			msg += "\n\n" + cm.Body
		}
		a.app.services.WriteClipboard(msg)
	}
	for _, r := range cm.Refs {
		if r.Kind == git.RefHead {
			continue
		}
		name := r.Name
		if m.Item(locales.ScmCopyQuoted(name)).Chosen() {
			a.app.services.WriteClipboard(name)
		}
	}
	if head {
		m.Separator()
		if m.Item(locales.CmdGitUndoCommit()).Disabled(moving).Chosen() {
			*action = a.undoCommit
		}
	}
	a.commitToolsMenu(m, *cm, head, action)
}

// toggleAllBranches has the commits show every branch and tag, or only
// the branch checked out.
func (a *gitContext) toggleAllBranches() {
	a.app.settings.ScmAllBranches = !a.app.settings.ScmAllBranches
	a.app.settings.Changed()
}

// scmMore is the row after the commits of the history, which reads more.
func (a *gitContext) scmMore(c *ui.Context, action *func()) {
	p := a.app.pal
	row := ui.Row(c).Grow(1).Height(scmRowHeight).Padding(0, 0, 0, 24).AlignItems(ui.Center)
	hovered, clicked := row.Hovered(), row.Clicked()
	row.Children(func() {
		col := p.muted
		if hovered {
			col = p.accent
		}
		ui.Text(c, locales.ScmShowMoreCommits()).FontSize(12).TextColor(col)
	})
	if clicked {
		*action = func() { a.git.historyN = max(a.git.historyN, historyPage) + historyPage }
	}
}

// shortAgo says how long before now t was, in a few letters.
func shortAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d/(7*24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
}

// treeIndent is how far in a row of the tree depth folders deep starts.
func treeIndent(depth int) float32 { return 4 + float32(depth)*treeStep }

// scmGuides paints the indent guides of a row of the tree, depth folders
// deep: a line down from the arrow of each folder above it, as the
// Explorer draws them.
func (a *app) scmGuides(row ui.Element, depth int) {
	if depth == 0 {
		return
	}
	col := a.pal.guide
	row.Draw(func(pt *ui.Painter, r ui.Rect) {
		for k := range depth {
			pt.Fill(ui.Rect{X: r.X + treeIndent(k) + 6, Y: r.Y, W: 1, H: r.H}, col, 0)
		}
	})
}

// scmFolder is the row of a folder of the tree: a click opens or closes
// it, and its buttons stage, unstage or discard every file in it.
func (a *gitContext) scmFolder(c *ui.Context, r scmRow, action *func()) {
	p := a.app.pal
	closed := a.scm.closedDirs[r.key]
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(scmRowHeight).Padding(0, 0, 0, treeIndent(r.depth))
	a.app.scmGuides(row, r.depth)
	hovered, clicked := row.Hovered(), row.Clicked()
	row.Children(func() {
		folder := iconFolderOpen
		if closed {
			folder = iconFolder
		}
		chevron(c, !closed).FontSize(12).TextColor(p.muted)
		ui.Icon(c, folder).FontSize(14).TextColor(p.accent).Margin(0, 2, 0, 0)
		ui.Text(c, r.name).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
		ui.Spacer(c)
		if !hovered {
			return
		}
		es := a.dirEntries(r.group, r.dir)
		var paths []string
		for _, e := range es {
			paths = append(paths, entryPaths(e, r.group == groupStaged)...)
		}
		switch r.group {
		case groupStaged:
			if a.app.smallButton(c, iconMinus, locales.ScmUnstageFolder()) {
				*action = func() { a.unstage(paths...) }
			}
		case groupChanges, groupUntracked:
			if a.app.smallButton(c, iconUndo, locales.ScmDiscardFolder()) {
				*action = func() { a.discard(es...) }
			}
			if a.app.smallButton(c, iconPlus, locales.ScmStageFolder()) {
				*action = func() { a.stage(paths...) }
			}
		case groupMerge:
			if a.app.smallButton(c, iconPlus, locales.ScmStageFolder()) {
				*action = func() { a.stage(paths...) }
			}
		}
	})
	if clicked && *action == nil {
		*action = func() { a.toggleDir(r) }
	}
}

// scmFileMenu is the menu of a changed file's row.
func (a *gitContext) scmFileMenu(m *ui.Menu, r scmRow, action *func()) {
	e := r.e
	if m.Item(locales.CmdGitOpenChanges()).Disabled(r.group == groupMerge).Chosen() {
		*action = func() { a.openChange(r, true) }
	}
	if r.group == groupMerge && m.Item(locales.MergeSolveConflicts()).Chosen() {
		*action = func() { a.app.openMerge(e.Path, true) }
	}
	if m.Item(openLabel(e)).Disabled(e.Letter() == 'D' && r.group != groupStaged).Chosen() {
		*action = func() { a.app.openPath(e.Path, true) }
	}
	m.Separator()
	if r.group == groupStaged {
		if m.Item(locales.UnstageChanges()).Chosen() {
			*action = func() { a.toggleStaged(r) }
		}
	} else {
		if m.Item(locales.StageChanges()).Chosen() {
			*action = func() { a.toggleStaged(r) }
		}
		if r.group != groupMerge && !e.Submodule() && m.Item(locales.DiscardChangesEllipsis()).Chosen() {
			*action = func() { a.discardChosen(r) }
		}
	}
	m.Separator()
	if m.Item(locales.ScmRevealInExplorer()).Chosen() {
		*action = func() { a.app.showExplorer(); a.app.revealInTree(e.Path) }
	}
	if m.Item(locales.CopyPath()).Chosen() {
		a.app.services.WriteClipboard(e.Path)
	}
}

// scmTitleActions is the button beside the view's title: the menu of the
// view's actions, Refresh among them. A button of its own for Refresh
// would leave the title no room.
func (a *gitContext) scmTitleActions(c *ui.Context) {
	if a.git.repo == nil {
		return
	}
	a.app.iconButton(c.Key("scm:more"), iconMore, locales.ScmMoreActions(), false).Menu(a.scmMenu)
}

// scmMenu is the menu of the view's actions.
func (a *gitContext) scmMenu(m *ui.Menu) {
	g := a.git
	st := g.status
	if m.Item(locales.CmdGitCommit()).Disabled(g.busy != "").Chosen() {
		a.app.post(func() { a.commit(false) })
	}
	if m.Item(locales.CmdGitCommitPush()).Disabled(g.busy != "").Chosen() {
		a.app.post(func() { a.commit(true) })
	}
	if m.Item(locales.CmdGitAmend()).Disabled(st == nil || st.Head == "").Chosen() {
		a.app.post(a.startAmend)
	}
	if m.Item(locales.CmdGitUndoCommit()).Disabled(g.busy != "" || st == nil || st.Head == "").Chosen() {
		a.app.post(a.undoCommit)
	}
	m.Separator()
	groups := a.groupEntries()
	if m.Item(locales.CmdGitOpenAllChanges()).Disabled(len(groups[groupStaged])+len(groups[groupChanges])+len(groups[groupUntracked]) == 0).Chosen() {
		a.app.post(func() { a.openChangesTab(true) })
	}
	m.Separator()
	if m.Item(locales.CmdGitStageAll()).Disabled(len(groups[groupChanges])+len(groups[groupUntracked])+len(groups[groupMerge]) == 0).Chosen() {
		a.app.post(a.stageAll)
	}
	if m.Item(locales.ScmUnstageAll()).Disabled(len(groups[groupStaged]) == 0).Chosen() {
		a.app.post(func() { a.unstage(a.groupPaths(groupStaged)...) })
	}
	if m.Item(locales.ScmDiscardAll()).Disabled(len(groups[groupChanges])+len(groups[groupUntracked]) == 0).Chosen() {
		a.app.post(func() { a.discard(append(groups[groupChanges], groups[groupUntracked]...)...) })
	}
	m.Separator()
	if m.Item(locales.CmdGitFetch()).Disabled(g.busy != "").Chosen() {
		a.app.post(a.fetch)
	}
	if m.Item(locales.CmdGitPull()).Disabled(g.busy != "").Chosen() {
		a.app.post(a.pull)
	}
	if m.Item(locales.CmdGitPush()).Disabled(g.busy != "").Chosen() {
		a.app.post(a.push)
	}
	m.Separator()
	if m.Item(locales.ScmViewAsTree()).Checked(a.app.settings.ScmTree).Chosen() {
		a.app.post(func() {
			a.app.settings.ScmTree = !a.app.settings.ScmTree
			a.app.settings.Changed()
		})
	}
	if m.Item(locales.ScmShowAllBranches()).Checked(a.app.settings.ScmAllBranches).Chosen() {
		a.app.post(a.toggleAllBranches)
	}
	if m.Item(locales.Refresh()).Chosen() {
		a.app.post(func() {
			a.git.refsGen++
			a.gitChanged()
		})
	}
}

// smallButton is a small button of a row, showing an icon, which reports
// whether it was clicked.
func (a *app) smallButton(c *ui.Context, icon *ui.SVG, label string) bool {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(revealFade)
	pressPose(b)
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(col) })
	return b.Clicked()
}

// groupPaths returns the paths of the files of a group: of a file
// renamed and staged, where it was too, which unstaging puts back.
func (a *gitContext) groupPaths(g scmGroup) []string {
	var out []string
	for _, e := range a.groupEntries()[g] {
		out = append(out, entryPaths(e, g == groupStaged)...)
	}
	return out
}

// entryPaths returns the path of a file, and where it was renamed from
// when staged.
func entryPaths(e git.Entry, staged bool) []string {
	if staged && e.Orig != "" {
		return []string{e.Path, e.Orig}
	}
	return []string{e.Path}
}

// chooseScmFile chooses the row of a file of a diff in the view, as the
// diff scrolls to it.
func (a *gitContext) chooseScmFile(f *diffFile) {
	for i, r := range a.scm.rows {
		if !r.header && r.e.Path == f.path && (r.group == groupStaged) == (f.base == diffStaged) {
			a.scm.row = i
			if a.scm.section == scmChanges {
				a.app.repositories.cursor = a.git.repo.Root + ":" + r.key
			}
			return
		}
	}
}

// openChange shows the changes of a file of the view: those staged, or
// those not, or the three-way merge of one in conflict.
func (a *gitContext) openChange(r scmRow, focus bool) {
	switch r.group {
	case groupMerge:
		a.app.openMerge(r.e.Path, focus)
	case groupStaged:
		a.showChanges(diffTarget{path: r.e.Path, base: diffStaged}, focus)
	default:
		a.showChanges(diffTarget{path: r.e.Path, base: diffIndex}, focus)
	}
}

// openLabel is what opening a changed file is called: a submodule opens
// as a folder.
func openLabel(e git.Entry) string {
	if e.Submodule() {
		return locales.OpenFolder()
	}
	return locales.OpenFile()
}

// entryIcon is the icon of a changed file, or of a submodule.
func entryIcon(p *palette, e git.Entry, name string) (*ui.SVG, ui.Color) {
	if e.Submodule() {
		return iconFolder, p.accent
	}
	return fileIcon(p, name)
}

// scmActionEntries returns the chosen files on the same side of the index
// as r. An action on an unselected row still acts on that file alone.
func (a *gitContext) scmActionEntries(r scmRow) []git.Entry {
	if !a.scm.selection.Has(r.key) {
		return []git.Entry{r.e}
	}
	var es []git.Entry
	for _, row := range a.scmRows() {
		if row.file() && a.scm.selection.Has(row.key) && (row.group == groupStaged) == (r.group == groupStaged) {
			es = append(es, row.e)
		}
	}
	return es
}

// discardChosen discards the changes of the chosen files not staged, or
// of r alone when it is not chosen. Conflicts are left to be resolved.
func (a *gitContext) discardChosen(r scmRow) {
	if !a.scm.selection.Has(r.key) {
		a.discard(r.e)
		return
	}
	var es []git.Entry
	for _, row := range a.scmRows() {
		if row.file() && a.scm.selection.Has(row.key) && row.group != groupStaged && row.group != groupMerge {
			es = append(es, row.e)
		}
	}
	a.discard(es...)
}

// toggleStaged stages the chosen files, or unstages those already staged.
func (a *gitContext) toggleStaged(r scmRow) {
	var paths []string
	seen := map[string]bool{}
	for _, e := range a.scmActionEntries(r) {
		for _, path := range entryPaths(e, r.group == groupStaged) {
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
	}
	if r.group == groupStaged {
		a.unstage(paths...)
	} else {
		a.stage(paths...)
	}
}

func (a *gitContext) stage(paths ...string) {
	if len(paths) == 0 {
		return
	}
	a.gitDo("", func(r *git.Repo) error { return r.Stage(paths...) }, nil)
}

func (a *gitContext) unstage(paths ...string) {
	if len(paths) == 0 {
		return
	}
	a.gitDo("", func(r *git.Repo) error { return r.Unstage(paths...) }, nil)
}

// stageAll stages every change, and the files Git does not track.
func (a *gitContext) stageAll() {
	groups := a.groupEntries()
	var paths []string
	for _, g := range []scmGroup{groupMerge, groupChanges, groupUntracked} {
		for _, e := range groups[g] {
			paths = append(paths, e.Path)
		}
	}
	a.stage(paths...)
}

// discard asks, then throws away the changes of files in the work tree
// that the index does not have: a tracked file is as the index has it
// again, and one Git does not track goes to the Trash. Submodules are
// left as they are, as git restore leaves them.
func (a *gitContext) discard(es ...git.Entry) {
	if !a.live() {
		return
	}
	es = slices.DeleteFunc(slices.Clone(es), git.Entry.Submodule)
	if len(es) == 0 {
		return
	}
	var tracked, untracked []string
	for _, e := range es {
		if e.Untracked() {
			untracked = append(untracked, e.Path)
		} else {
			tracked = append(tracked, e.Path)
		}
	}
	title := locales.DiscardFilesQuestion(len(es))
	if len(es) == 1 {
		title = locales.DiscardFileQuestion(filepath.Base(es[0].Path))
		if es[0].Untracked() {
			title = locales.MoveToTrashQuestion(filepath.Base(es[0].Path), trashName())
		}
	}
	msg := locales.CannotBeUndone()
	if len(untracked) > 0 {
		msg = locales.UntrackedGoTo(trashName())
		if a.app.conn != nil {
			msg = locales.UntrackedDeleted()
		}
	}
	a.app.confirm = &confirm{title: a.repositoryQuestion(title), message: msg, ok: locales.ButtonDiscard(), danger: true, run: func() {
		if !a.live() {
			return
		}
		if a.app.conn != nil && len(untracked) > 0 {
			// On the host, in the background.
			gone := slices.Clone(untracked)
			a.gitDo("", func(*git.Repo) error {
				for _, p := range gone {
					if err := vfs.RemoveAll(p); err != nil {
						return err
					}
				}
				return nil
			}, func() {
				a.app.tree.Refresh()
				a.app.indexStale = true
			})
			untracked = nil
		}
		for _, p := range untracked {
			if err := a.app.host.trash(p); err != nil {
				a.app.fail("%s", locales.FailMoveTo(filepath.Base(p), trashName(), err.Error()))
			}
		}
		if a.app.tree != nil && len(untracked) > 0 {
			a.app.tree.Refresh()
			a.app.indexStale = true
		}
		if len(tracked) > 0 {
			a.gitDo("", func(r *git.Repo) error { return r.Discard(tracked...) }, func() {
				// Open documents show the files as they are again, unsaved
				// changes too, as a step of undo.
				for _, p := range tracked {
					if i := a.app.docIndex(p); i >= 0 {
						a.app.reloadDoc(a.app.docs[i])
					}
				}
			})
		} else {
			a.gitChanged()
		}
	}}
}

// startAmend has the next commit amend the last, its message the last
// one's unless one is written.
func (a *gitContext) startAmend() {
	if !a.live() {
		return
	}
	g := a.git
	g.amend = true
	if strings.TrimSpace(g.message) != "" || g.repo == nil {
		return
	}
	r := g.repo
	bg.Go(func() {
		msg := r.LastMessage()
		a.app.post(func() {
			if g.repo == r && g.amend && strings.TrimSpace(g.message) == "" {
				g.message = msg
			}
		})
	})
}

// undoCommit takes back the last commit, keeping its changes staged and
// its message in the box, to commit again.
func (a *gitContext) undoCommit() {
	if !a.live() {
		return
	}
	if len(a.app.repositories.contexts) < 2 {
		a.undoCommitOf("")
		return
	}
	a.app.confirm = &confirm{title: a.repositoryQuestion(locales.CmdGitUndoCommit()), ok: locales.ButtonUndo(),
		run: func() { a.undoCommitOf("") }}
}

// undoCommitOf takes back the last commit when it is hash, or whatever it
// is for "": an Undo offered for a commit undoes it, not one made since.
func (a *gitContext) undoCommitOf(hash string) {
	if !a.live() {
		return
	}
	g := a.git
	if g.repo == nil || g.status == nil || g.status.Head == "" {
		return
	}
	var msg string
	a.gitDo(locales.BusyUndoingCommit(), func(r *git.Repo) error {
		if hash != "" && r.Head() != hash {
			return errors.New(locales.ErrCommitSince())
		}
		msg = r.LastMessage()
		return r.UndoCommit()
	}, func() {
		g.amend = false
		if strings.TrimSpace(g.message) == "" {
			g.message = msg
		}
	})
}

// commit commits what is staged with the message written, then pushes
// when push is set. With nothing staged it offers to stage every change
// first. Changed files are saved before.
func (a *gitContext) commit(push bool) {
	if !a.live() {
		return
	}
	g := a.git
	if g.repo == nil || g.busy != "" || g.writing != nil {
		return
	}
	msg := strings.TrimSpace(g.message)
	if msg == "" {
		a.app.fail("%s", locales.WriteMessageFirst())
		return
	}
	for _, d := range a.app.docs {
		if d.path != "" && d.dirty() && a.owns(d.path) {
			a.app.saveDoc(d, nil)
		}
	}
	amend := g.amend
	run := func(stageAll bool) {
		var made string // the commit made
		a.gitDo(locales.BusyCommitting(), func(r *git.Repo) error {
			if stageAll {
				if err := r.Stage(r.Root); err != nil {
					return err
				}
			}
			if err := r.Commit(msg, amend); err != nil {
				return err
			}
			made = r.Head()
			return nil
		}, func() {
			g.message, g.amend = "", false
			switch {
			case push:
				a.push()
			case amend:
				a.app.tell(note{message: locales.AmendedLastCommit()})
			default:
				subject, _, _ := strings.Cut(msg, "\n")
				said := locales.Committed(ellipsize(subject, 48))
				if b := a.branch(); b != "" {
					said = locales.CommittedTo(ellipsize(subject, 48), b)
				}
				a.app.tell(note{message: said, label: locales.ButtonUndo(),
					action: func() { a.undoCommitOf(made) }})
			}
		})
	}
	groups := a.groupEntries()
	switch {
	case amend || len(groups[groupStaged]) > 0:
		run(false)
	case len(groups[groupMerge]) > 0:
		a.app.fail("%s", locales.ResolveMergeFirst())
	case g.status != nil && (g.status.Op == git.OpMerge || g.status.Op == git.OpRevert || g.status.Op == git.OpCherryPick) &&
		len(groups[groupChanges])+len(groups[groupUntracked]) == 0:
		run(false) // ends the merge, though its changes are all HEAD's
	case len(groups[groupChanges])+len(groups[groupUntracked]) > 0:
		a.app.confirm = &confirm{title: locales.StageAllQuestion(), ok: locales.StageAllCommit(),
			message: locales.NoStagedChanges(), run: func() { run(true) }}
	default:
		a.app.fail("%s", locales.NoChangesToCommit())
	}
}

// ellipsize cuts s to n runes, with an ellipsis where it was cut.
func ellipsize(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// initRepo makes the folder a Git repository.
func (a *app) initRepo() {
	if a.root == "" {
		return
	}
	root := a.root
	bg.Go(func() {
		err := git.Init(root)
		a.post(func() {
			if err != nil {
				a.gitFailed(err)
				return
			}
			if a.root == root {
				a.git = gitState{}
				a.ignoredStale = true
			}
		})
	})
}

// showExplorer shows the files in the sidebar.
func (a *app) showExplorer() {
	a.window.SidebarClosed, a.window.SidebarView = false, ""
	a.settings.Changed()
	// The files take the keyboard, as in VS Code: arrows choose, and ⌘V
	// pastes into the folder chosen.
	a.focusFiles = true
}

// showSourceControl shows the changes in the sidebar.
func (a *app) showSourceControl() {
	a.window.SidebarClosed, a.window.SidebarView = false, "git"
	a.settings.Changed()
}

// activePaths returns the file of the tab shown, to stage or unstage.
func (a *app) activePaths() []string {
	if t := a.activeTab(); t != nil && t.path() != "" {
		return []string{t.path()}
	}
	return nil
}

// scmBranchRow is the row at the top of the source control view: the
// branch, which a click switches, and the buttons of its remote, which
// say how far it is ahead and behind.
func (a *gitContext) scmBranchRow(c *ui.Context) {
	ui.Row(c).Height(30).Padding(0, 8, 0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		a.scmBranchButton(c, false)
		ui.Spacer(c)
		a.scmActions(c)
	})
}

// scmBranchButton also fits in a multi-repository header.
func (a *gitContext) scmBranchButton(c *ui.Context, compact bool) {
	p := a.app.pal
	branch := a.branch()
	if branch == "" {
		branch = locales.NoBranch()
	}
	b := ui.ButtonBase(c.Key("scm-branch")).Height(24).Padding(0, 6).Gap(5).Radius(5).AlignItems(ui.Center).
		Shrink(1).Label(locales.BranchLabel(branch)).Tooltip(locales.CmdGitSwitchBranch()).Disabled(a.git.busy != "")
	size := float32(12.5)
	if compact {
		b.MaxWidth(100).Shrink(2)
		size = 11
	}
	col := p.text
	if b.Hovered() || b.Pressed() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconBranch).FontSize(13).TextColor(p.muted)
		ui.Text(c, branch).FontSize(size).FontWeight(500).TextColor(col).SingleLine().Shrink(1)
		ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.faint)
	})
	if b.Clicked() {
		a.openBranchPicker()
	}
}

// scmActions are the buttons of the branch's remote: fetching, and
// pulling and pushing, which say how many commits they would bring, or
// publishing a branch that has no upstream yet.
func (a *gitContext) scmActions(c *ui.Context) {
	g := a.git
	st := g.status
	busy := g.busy != ""
	if len(g.remotes) == 0 {
		return
	}
	if a.app.syncButton(c, "fetch", iconFetch, locales.CmdGitFetch(), 0).Disabled(busy).Clicked() {
		a.fetch()
	}
	if st != nil && st.Branch != "" && st.Upstream == "" {
		if a.app.syncButton(c, "publish", iconPush, locales.ScmPublishBranch(), 0).Disabled(busy).Clicked() {
			a.push()
		}
		return
	}
	behind, ahead := 0, 0
	if st != nil {
		behind, ahead = st.Behind, st.Ahead
	}
	pull, push := locales.CmdGitPull(), locales.CmdGitPush()
	if behind > 0 {
		pull = locales.ScmPullCount(behind)
	}
	if ahead > 0 {
		push = locales.ScmPushCount(ahead)
	}
	if a.app.syncButton(c, "pull", iconPull, pull, behind).Disabled(busy).Clicked() {
		a.pull()
	}
	if a.app.syncButton(c, "push", iconPush, push, ahead).Disabled(busy).Clicked() {
		a.push()
	}
}

// syncButton is a button of the branch's remote, with the count of the
// commits it would move beside its icon when there are any.
func (a *app) syncButton(c *ui.Context, key string, icon *ui.SVG, label string, count int) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key("sync:"+key)).Height(24).MinWidth(26).Padding(0, 5).Gap(3).Radius(5).
		Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	if count > 0 {
		col = p.accent
		b.Background(p.accent.Alpha(0.12))
	}
	if b.Hovered() || b.Pressed() {
		b.Background(p.hover)
		if count > 0 {
			b.Background(p.accent.Alpha(0.2))
		}
		if count == 0 {
			col = p.text
		}
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(11.5).FontWeight(600).TextColor(col)
		}
	})
	return b
}
