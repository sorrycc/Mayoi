package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
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

var groupNames = [groupCount]string{"Merge Changes", "Staged Changes", "Changes", "Untracked", "Commits"}

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
}

// file reports whether a row is a file's.
func (r *scmRow) file() bool { return !r.header && r.commit == nil && !r.more && r.dir == "" }

// scmState is the source control view's: its list, the row chosen, and
// the groups closed.
type scmState struct {
	list      ui.ListState
	row       int
	collapsed [groupCount]bool
	rows      []scmRow // as the last frame built them
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
}

// groupEntries returns the files of each group, by path. They are worked
// out once for each status read, as the view asks for them every frame.
func (a *app) groupEntries() [groupCount][]git.Entry {
	g := &a.git
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
// the files of those open, then the last commits. They are made again
// only when the status, the groups closed or the history change.
func (a *app) scmRows() []scmRow {
	s := &a.scm
	g := &a.git
	tree := a.settings.ScmTree
	if s.rows != nil && s.rowsOf == g.status && s.rowsCollapsed == s.collapsed && s.rowsHistory == g.historyGen &&
		s.rowsDirs == s.dirsGen && s.rowsTree == tree {
		return s.rows
	}
	var rows []scmRow
	groups := a.groupEntries()
	for g, es := range groups {
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
	if h := g.history; len(h) > 0 {
		rows = append(rows, scmRow{group: groupCommits, header: true, count: len(h), key: "group:commits"})
		if !s.collapsed[groupCommits] {
			for i := range h {
				rows = append(rows, scmRow{group: groupCommits, commit: &h[i], at: i, key: "commit:" + h[i].Hash})
			}
			if len(h) >= max(g.historyN, historyPage) {
				rows = append(rows, scmRow{group: groupCommits, more: true, key: "commits:more"})
			}
		}
	}
	s.rows, s.rowsOf, s.rowsCollapsed, s.rowsHistory = rows, g.status, s.collapsed, g.historyGen
	s.rowsDirs, s.rowsTree = s.dirsGen, tree
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

// treeRows adds the rows of a group's files, es by path, to rows, in a
// tree of their folders under root: a folder's folders first, then its
// files, and a folder with a single folder in it and no files shown as
// one, as "internal/git". The folders closed, by their rows' keys, show
// without what is in them.
func treeRows(rows []scmRow, g scmGroup, es []git.Entry, root string, closed map[string]bool) []scmRow {
	top := &scmDir{path: root}
	at := map[string]*scmDir{root: top}
	var dirOf func(path string) *scmDir
	dirOf = func(path string) *scmDir {
		if d, ok := at[path]; ok {
			return d
		}
		parent := dirOf(filepath.Dir(path))
		d := &scmDir{name: filepath.Base(path), path: path}
		parent.dirs = append(parent.dirs, d)
		at[path] = d
		return d
	}
	for _, e := range es {
		dir := filepath.Dir(e.Path)
		if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			dir = root // outside the work tree: shown at its top
		}
		d := dirOf(dir)
		d.files = append(d.files, e)
	}
	var add func(d *scmDir, depth int)
	add = func(d *scmDir, depth int) {
		slices.SortFunc(d.dirs, func(x, y *scmDir) int { return strings.Compare(x.name, y.name) })
		for _, sub := range d.dirs {
			name := sub.name
			for len(sub.dirs) == 1 && len(sub.files) == 0 {
				sub = sub.dirs[0]
				name += "/" + sub.name
			}
			key := fmt.Sprintf("%d:dir:%s", g, sub.path)
			rows = append(rows, scmRow{group: g, dir: sub.path, name: name, depth: depth, key: key})
			if !closed[key] {
				add(sub, depth+1)
			}
		}
		for _, e := range d.files {
			rows = append(rows, scmRow{group: g, e: e, depth: depth, key: fmt.Sprintf("%d:%s", g, e.Path)})
		}
	}
	add(top, 0)
	return rows
}

// toggleDir opens a folder of the tree, or closes it.
func (a *app) toggleDir(r scmRow) {
	s := &a.scm
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
func (a *app) dirEntries(g scmGroup, dir string) []git.Entry {
	var out []git.Entry
	for _, e := range a.groupEntries()[g] {
		if strings.HasPrefix(e.Path, dir+string(filepath.Separator)) {
			out = append(out, e)
		}
	}
	return out
}

// changeCount is how many files differ from HEAD, or Git does not track.
func (a *app) changeCount() int {
	if a.git.status == nil {
		return 0
	}
	return len(a.git.status.Entries)
}

// scmView is the sidebar's source control view: the branch, the commit
// message, and the files changed, staged and not.
func (a *app) scmView(c *ui.Context) {
	p := a.pal
	g := &a.git
	a.scmActivity(c)
	if g.repo == nil {
		if g.looked {
			a.scmNoRepo(c)
		}
		return
	}
	a.scmBranchRow(c)
	ui.Column(c).Padding(2, 12, 10, 12).Gap(8).Children(func() {
		if g.amend {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 4, 4, 8).Radius(5).Background(p.accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconPencil).FontSize(12).TextColor(p.accent)
				ui.Text(c, "Amending last commit").FontSize(11.5).TextColor(p.text).Grow(1).SingleLine().Shrink(1)
				b := ui.ButtonBase(c.Key("stop-amending")).Padding(1, 6).Radius(4).Label("Stop Amending")
				if b.Hovered() {
					b.Background(p.hover)
				}
				b.Children(func() { ui.Text(c, "Cancel").FontSize(11.5).FontWeight(500).TextColor(p.accent) })
				if b.Clicked() {
					g.amend, g.message = false, ""
				}
			})
		}
		branch := a.branch()
		enter := keyText("CmdOrCtrl+Enter")
		hint := "Message (" + enter + " to commit)"
		if branch != "" {
			hint = fmt.Sprintf("Message (%s to commit on %s)", enter, branch)
		}
		box := a.textKeys(ui.TextArea(c.Key("commit-message"), &g.message)).Placeholder(hint).FontSize(12.5).
			MinHeight(60).MaxHeight(160).Label("Commit Message")
		if box.Shortcut(ui.Cmd, ui.KeyEnter) {
			a.commit(false)
		}
		a.subjectLength(c)
		a.commitButton(c)
	})
	rows := a.scmRows()
	s := &a.scm
	if g.status != nil && len(g.status.Entries) == 0 {
		a.scmClean(c)
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
		case r.header:
			return ""
		case r.dir != "":
			return r.name
		case r.commit != nil:
			return r.commit.Subject
		case r.more:
			return "Show More Commits"
		}
		return filepath.Base(rows[i].e.Path)
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var action func()
	out := ui.List(c.Key("scm"), &s.list, len(rows), func(i int) {
		switch r := rows[i]; {
		case r.header:
			a.scmHeader(c, r, &action)
		case r.commit != nil:
			a.scmCommit(c, i, r, &action)
		case r.more:
			a.scmMore(c, &action)
		case r.dir != "":
			a.scmFolder(c, r, &action)
		default:
			a.scmFile(c, r, &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6)
	c.SetTheme(base)
	if action != nil {
		action()
	}
	chosen := func() (scmRow, bool) {
		if s.row >= 0 && s.row < len(rows) && rows[s.row].file() {
			return rows[s.row], true
		}
		return scmRow{}, false
	}
	if s.row >= 0 && s.row < len(rows) && out.Submitted() {
		switch r := rows[s.row]; {
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
	if r, ok := chosen(); ok && out.Shortcut(0, ui.KeySpace) {
		a.toggleStaged(r)
	}
}

// scmActivity is a thin bar under the view's title, which moves while a
// Git command runs in the background, and is empty room while none does.
func (a *app) scmActivity(c *ui.Context) {
	g := &a.git
	if g.busy == "" && g.pending == 0 {
		ui.Box(c.Key("scm-activity")).Height(2)
		return
	}
	ui.Progress(c.Key("scm-activity"), -1).Height(2).Radius(0).Label("Running Git")
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
func (a *app) subjectLength(c *ui.Context) {
	p := a.pal
	subject, _, _ := strings.Cut(a.git.message, "\n")
	n := utf8.RuneCountInString(strings.TrimRight(subject, " \t\r"))
	if n <= subjectHint {
		return
	}
	col, note := p.faint, "Summary"
	if n > subjectLimit {
		col, note = p.modified, "Summary is long"
	}
	ui.Row(c).Margin(-4, 2, 0, 2).Children(func() {
		ui.Text(c, note).FontSize(11).TextColor(col).Grow(1).SingleLine()
		ui.Text(c, fmt.Sprintf("%d/%d", n, subjectLimit)).FontSize(11).Font(a.font.family).TextColor(col)
	})
}

// commitButton is the button that commits, which says what it commits,
// joined to one with a menu of the other ways to commit.
func (a *app) commitButton(c *ui.Context) {
	g := &a.git
	t := c.Theme()
	groups := a.groupEntries()
	staged := len(groups[groupStaged])
	others := len(groups[groupChanges]) + len(groups[groupUntracked])
	label := "Commit"
	switch {
	case g.amend:
		label = "Amend Commit"
	case staged == 0 && others > 0:
		label = "Commit All"
	}
	tip := "Commit the staged changes"
	switch {
	case g.amend:
		tip = "Amend the last commit with the staged changes"
	case staged == 0 && others > 0:
		tip = "Stage every change and commit it"
	}
	tip += " (" + keyText("CmdOrCtrl+Enter") + ")"
	committing := g.busy == "Committing…"
	idle := g.busy == "" && (g.amend || staged+others > 0)
	// In the accent color while it commits, or can; gray while there is
	// nothing to commit or another command runs, its menu still there.
	bg, hover, pressed, fg := t.Accent, t.AccentHover, t.AccentPressed, t.AccentText
	if !idle && !committing {
		p := a.pal
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
		main.Children(func() {
			if committing {
				ui.Spinner(c).Size(12, 12)
				ui.Text(c, "Committing…").FontSize(12.5).FontWeight(600).TextColor(fg).SingleLine()
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
			Background(bg).Label("More Commit Actions").Tooltip("More Commit Actions")
		switch {
		case more.Pressed():
			more.Background(pressed)
		case more.Hovered():
			more.Background(hover)
		}
		more.Children(func() { ui.Icon(c, iconChevronDown).FontSize(14).TextColor(fg) })
		more.Menu(a.commitMenu)
	})
}

// commitMenu is the menu of the ways to commit.
func (a *app) commitMenu(m *ui.Menu) {
	g := &a.git
	st := g.status
	busy := g.busy != ""
	none := st == nil || len(st.Entries) == 0 && !g.amend
	if m.Item("Commit").Shortcut(ui.Cmd, ui.KeyEnter).Disabled(busy || none).Chosen() {
		a.post(func() { a.commit(false) })
	}
	if m.Item("Commit & Push").Disabled(busy || none).Chosen() {
		a.post(func() { a.commit(true) })
	}
	m.Separator()
	if m.Item("Amend Last Commit").Checked(g.amend).Disabled(busy || st == nil || st.Head == "").Chosen() {
		a.post(func() {
			if g.amend {
				g.amend, g.message = false, ""
				return
			}
			a.startAmend()
		})
	}
	if m.Item("Undo Last Commit").Disabled(busy || st == nil || st.Head == "").Chosen() {
		a.post(a.undoCommit)
	}
}

// scmNoRepo is the view of a folder outside a work tree: what that means,
// and a button to make it one.
func (a *app) scmNoRepo(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(28, 20, 16, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconBranch).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, "No Git Repository").FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, "The folder is not in a Git repository. Make it one to track and commit its changes.").
			FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		ui.Box(c).Height(4)
		if ui.PrimaryButton(c, "Initialize Repository").FillWidth().Clicked() {
			a.initRepo()
		}
	})
}

// scmClean is the view of a work tree with no changes: that it is clean,
// and how its branch stands with its upstream, with buttons to bring them
// level.
func (a *app) scmClean(c *ui.Context) {
	p := a.pal
	g := &a.git
	st := g.status
	ui.Column(c).Padding(24, 20, 16, 20).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(22).Background(p.added.Alpha(0.13)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconCheck).FontSize(20).TextColor(p.added)
		})
		ui.Text(c, "No Changes").FontSize(13).FontWeight(600).TextColor(p.text).Margin(6, 0, 0, 0)
		note := "The working tree is clean."
		switch {
		case st.Branch == "":
		case st.Upstream == "" && len(g.remotes) == 0:
			note = fmt.Sprintf("%s is clean.", st.Branch)
		case st.Upstream == "":
			note = fmt.Sprintf("%s is clean, and not yet published.", st.Branch)
		case st.Ahead > 0 && st.Behind > 0:
			note = fmt.Sprintf("%s is clean, %s ahead of %s and %s behind.", st.Branch,
				plural(st.Ahead, "commit"), st.Upstream, plural(st.Behind, "commit"))
		case st.Ahead > 0:
			note = fmt.Sprintf("%s is clean, %s ahead of %s.", st.Branch, plural(st.Ahead, "commit"), st.Upstream)
		case st.Behind > 0:
			note = fmt.Sprintf("%s is clean, %s behind %s.", st.Branch, plural(st.Behind, "commit"), st.Upstream)
		default:
			note = fmt.Sprintf("%s is clean and up to date with %s.", st.Branch, st.Upstream)
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
			button(iconPush, "Publish Branch", true, a.push)
		case st.Upstream == "":
		case st.Behind > 0:
			button(iconPull, fmt.Sprintf("Pull %s", plural(st.Behind, "Commit")), true, a.pull)
			if st.Ahead > 0 {
				button(iconPush, fmt.Sprintf("Push %s", plural(st.Ahead, "Commit")), false, a.push)
			}
		case st.Ahead > 0:
			button(iconPush, fmt.Sprintf("Push %s", plural(st.Ahead, "Commit")), true, a.push)
		}
	})
}

// scmHeader is the row heading a group: a click opens or closes it.
func (a *app) scmHeader(c *ui.Context, r scmRow, action *func()) {
	p := a.pal
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(22).Background(p.chrome)
	hovered := row.Hovered()
	clicked := row.Clicked()
	row.Children(func() {
		icon := iconChevronDown
		if a.scm.collapsed[r.group] {
			icon = iconChevronRight
		}
		ui.Icon(c, icon).FontSize(12).TextColor(p.muted)
		ui.Text(c, strings.ToUpper(groupNames[r.group])).FontSize(11).FontWeight(600).LetterSpacing(0.4).
			TextColor(p.muted).SingleLine().Shrink(1)
		ui.Spacer(c)
		if hovered {
			paths := a.groupPaths(r.group)
			switch r.group {
			case groupStaged:
				if a.smallButton(c, iconFiles, "Open Staged Changes") {
					*action = func() { a.showChanges(diffTarget{base: diffStaged}, true) }
				}
				if a.smallButton(c, iconMinus, "Unstage All Changes") {
					*action = func() { a.unstage(paths...) }
				}
			case groupChanges, groupUntracked:
				if a.smallButton(c, iconFiles, "Open Changes") {
					*action = func() { a.showChanges(diffTarget{base: diffIndex}, true) }
				}
				if a.smallButton(c, iconUndo, "Discard All Changes…") {
					*action = func() { a.discard(a.groupEntries()[r.group]...) }
				}
				if a.smallButton(c, iconPlus, "Stage All Changes") {
					*action = func() { a.stage(paths...) }
				}
			case groupMerge:
				if a.smallButton(c, iconPlus, "Stage All Merge Changes") {
					*action = func() { a.stage(paths...) }
				}
			}
		}
		// The toggle of every branch stays, in the accent, while it is on.
		if all := a.settings.ScmAllBranches; r.group == groupCommits && (hovered || all) {
			tip := "Show All Branches"
			if all {
				tip = "Show Current Branch Only"
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
			b.Children(func() { ui.Icon(c, iconBranch).FontSize(13).TextColor(col) })
			if b.Clicked() {
				*action = a.toggleAllBranches
			}
		}
		ui.Text(c, fmt.Sprint(r.count)).FontSize(10.5).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.hover)
	})
	if clicked && *action == nil {
		*action = func() { a.scm.collapsed[r.group] = !a.scm.collapsed[r.group] }
	}
}

// scmFile is the row of a changed file: a click shows its changes.
func (a *app) scmFile(c *ui.Context, r scmRow, action *func()) {
	p := a.pal
	e := r.e
	indent := float32(14)
	tree := a.settings.ScmTree
	if tree {
		indent = treeIndent(r.depth) + 16
	}
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 0, 0, indent)
	hovered := row.Hovered()
	clicked := row.Clicked()
	row.ContextMenu(func(m *ui.Menu) { a.scmFileMenu(m, r, action) })
	letter := e.Letter()
	if r.group == groupStaged {
		letter = e.X
	}
	if letter != 'D' && !e.Submodule() {
		a.dragFile(row, &fileDrag{path: e.Path})
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
		ui.Spacer(c)
		if hovered {
			if letter != 'D' && a.smallButton(c, iconOpenFile, openLabel(e)) {
				*action = func() { a.openPath(e.Path, true) }
			}
			switch r.group {
			case groupStaged:
				if a.smallButton(c, iconMinus, "Unstage Changes") {
					*action = func() { a.unstage(entryPaths(e, true)...) }
				}
			default:
				if r.group != groupMerge && !e.Submodule() && a.smallButton(c, iconUndo, "Discard Changes…") {
					*action = func() { a.discard(e) }
				}
				if a.smallButton(c, iconPlus, "Stage Changes") {
					*action = func() { a.stage(e.Path) }
				}
			}
		}
		ui.Text(c, string(letter)).FontSize(11).FontWeight(600).Font(a.font.family).Padding(0, 4, 0, 2).
			TextColor(gitColor(p, letter)).Shrink(0)
	})
	if clicked && *action == nil {
		*action = func() { a.openChange(r, true) }
	}
}

// scmCommit is the row of a commit of the history: its lanes of the
// graph, who made it, its branches and tags, its subject, and how long
// ago it was made. A click chooses row i and shows the commit's changes
// in the preview tab, which Enter or a double click keeps.
func (a *app) scmCommit(c *ui.Context, i int, r scmRow, action *func()) {
	p := a.pal
	cm := r.commit
	g := &a.git
	unpushed := g.unpushed[cm.Hash]
	head := g.status != nil && g.status.Head == cm.Hash
	badges := refBadges(cm.Refs)
	tip := fmt.Sprintf("%s\n%s · %s · %s", cm.Subject, cm.Author, ago(cm.Time, time.Now()), cm.Short())
	if unpushed {
		tip += " · not pushed"
	}
	if len(cm.Parents) > 1 {
		tip += " · merge"
	}
	if len(badges) > 0 {
		names := make([]string, len(badges))
		for i, b := range badges {
			names[i] = b.label()
		}
		tip += "\n" + strings.Join(names, ", ")
	}
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 4, 0, 6).Tooltip(tip)
	clicked := row.Clicked()
	row.ContextMenu(func(m *ui.Menu) { a.scmCommitMenu(m, cm, head, action) })
	graph := a.historyGraph()
	var gr *graphRow
	if r.at < len(graph) {
		gr = &graph[r.at]
	}
	lanes := min(graphLanes(graph), graphMaxLanes)
	row.Children(func() {
		ui.Box(c).Size(graphLaneW*float32(lanes-1)+12, 22).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
			if gr != nil {
				a.paintGraphRow(pt, b, gr, cm, head)
			}
		})
		author := avatarColor(p, cm.Email+cm.Author)
		ui.Box(c).Size(15, 15).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
			pt.Fill(b, author.Alpha(0.22), b.W/2)
			ini := ui.Span{Text: initials(cm.Author), Size: 7, Weight: 700, Color: author}
			iw, ih := pt.MeasureText(0, ini)
			pt.RichText(b.X+(b.W-iw)/2, b.Y+(b.H-ih)/2, 0, ini)
		})
		lane := p.accent
		if gr != nil {
			lane = graphColor(p, gr.color)
		}
		shown := a.badgesShown()
		for i, bd := range badges {
			if i == shown && len(badges) > shown {
				ui.Text(c, fmt.Sprintf("+%d", len(badges)-i)).FontSize(10.5).TextColor(p.muted).Padding(0, 4).
					Height(16).Radius(4).Background(p.hover).Shrink(0)
				break
			}
			a.refBadge(c, bd, lane)
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

// badgesShown is how many branches and tags a commit's row shows before
// it counts the rest: more as the sidebar widens.
func (a *app) badgesShown() int {
	return min(max(1+int((a.settings.SidebarWidth-240)/90), 1), 3)
}

// historyGraph returns the lanes of the history, laid out once for each
// read of it.
func (a *app) historyGraph() []graphRow {
	g := &a.git
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
func (a *app) refBadge(c *ui.Context, b refBadge, lane ui.Color) {
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
		MaxWidth(110).Shrink(0).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(10).TextColor(fg).Shrink(0)
		}
		ui.Text(c, b.label()).FontSize(11).FontWeight(weight).TextColor(fg).SingleLine().Shrink(1)
	})
}

// scmCommitMenu is the menu of a commit of the history.
func (a *app) scmCommitMenu(m *ui.Menu, cm *git.Commit, head bool, action *func()) {
	g := &a.git
	busy := g.busy != ""
	if m.Item("Open Changes").Chosen() {
		*action = func() { a.showCommit(*cm, true) }
	}
	m.Separator()
	if m.Item("Check Out…").Disabled(busy || head).Chosen() {
		*action = func() { a.checkOutCommit(*cm) }
	}
	if m.Item("Create Branch Here…").Disabled(busy).Chosen() {
		*action = func() { a.askRefAt(*cm, false) }
	}
	if m.Item("Create Tag Here…").Disabled(busy).Chosen() {
		*action = func() { a.askRefAt(*cm, true) }
	}
	m.Separator()
	if m.Item("Copy Commit Hash").Chosen() {
		a.services.WriteClipboard(cm.Hash)
	}
	if m.Item("Copy Commit Message").Chosen() {
		msg := cm.Subject
		if cm.Body != "" {
			msg += "\n\n" + cm.Body
		}
		a.services.WriteClipboard(msg)
	}
	for _, r := range cm.Refs {
		if r.Kind == git.RefHead {
			continue
		}
		name := r.Name
		if m.Item(fmt.Sprintf("Copy “%s”", name)).Chosen() {
			a.services.WriteClipboard(name)
		}
	}
	if head {
		m.Separator()
		if m.Item("Undo Last Commit").Disabled(busy).Chosen() {
			*action = a.undoCommit
		}
	}
}

// toggleAllBranches has the commits show every branch and tag, or only
// the branch checked out.
func (a *app) toggleAllBranches() {
	a.settings.ScmAllBranches = !a.settings.ScmAllBranches
	a.settings.Changed()
}

// scmMore is the row after the commits of the history, which reads more.
func (a *app) scmMore(c *ui.Context, action *func()) {
	p := a.pal
	row := ui.Row(c).Grow(1).Height(22).Padding(0, 0, 0, 24).AlignItems(ui.Center)
	hovered, clicked := row.Hovered(), row.Clicked()
	row.Children(func() {
		col := p.muted
		if hovered {
			col = p.accent
		}
		ui.Text(c, "Show More Commits").FontSize(12).TextColor(col)
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
func treeIndent(depth int) float32 { return 4 + float32(depth)*14 }

// scmFolder is the row of a folder of the tree: a click opens or closes
// it, and its buttons stage, unstage or discard every file in it.
func (a *app) scmFolder(c *ui.Context, r scmRow, action *func()) {
	p := a.pal
	closed := a.scm.closedDirs[r.key]
	row := ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Height(22).Padding(0, 0, 0, treeIndent(r.depth))
	hovered, clicked := row.Hovered(), row.Clicked()
	row.Children(func() {
		chevron, folder := iconChevronDown, iconFolderOpen
		if closed {
			chevron, folder = iconChevronRight, iconFolder
		}
		ui.Icon(c, chevron).FontSize(12).TextColor(p.muted)
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
			if a.smallButton(c, iconMinus, "Unstage Folder") {
				*action = func() { a.unstage(paths...) }
			}
		case groupChanges, groupUntracked:
			if a.smallButton(c, iconUndo, "Discard Folder Changes…") {
				*action = func() { a.discard(es...) }
			}
			if a.smallButton(c, iconPlus, "Stage Folder") {
				*action = func() { a.stage(paths...) }
			}
		case groupMerge:
			if a.smallButton(c, iconPlus, "Stage Folder") {
				*action = func() { a.stage(paths...) }
			}
		}
	})
	if clicked && *action == nil {
		*action = func() { a.toggleDir(r) }
	}
}

// scmFileMenu is the menu of a changed file's row.
func (a *app) scmFileMenu(m *ui.Menu, r scmRow, action *func()) {
	e := r.e
	if m.Item("Open Changes").Chosen() {
		*action = func() { a.openChange(r, true) }
	}
	if m.Item(openLabel(e)).Disabled(e.Letter() == 'D' && r.group != groupStaged).Chosen() {
		*action = func() { a.openPath(e.Path, true) }
	}
	m.Separator()
	if r.group == groupStaged {
		if m.Item("Unstage Changes").Chosen() {
			*action = func() { a.unstage(entryPaths(e, true)...) }
		}
	} else {
		if m.Item("Stage Changes").Chosen() {
			*action = func() { a.stage(e.Path) }
		}
		if r.group != groupMerge && !e.Submodule() && m.Item("Discard Changes…").Chosen() {
			*action = func() { a.discard(e) }
		}
	}
	m.Separator()
	if m.Item("Reveal in Explorer").Chosen() {
		*action = func() { a.showExplorer(); a.revealInTree(e.Path) }
	}
	if m.Item("Copy Path").Chosen() {
		a.services.WriteClipboard(e.Path)
	}
}

// scmTitleActions is the button beside the view's title: the menu of the
// view's actions, Refresh among them. A button of its own for Refresh
// would leave the title no room.
func (a *app) scmTitleActions(c *ui.Context) {
	if a.git.repo == nil {
		return
	}
	a.iconButton(c.Key("scm:more"), iconMore, "More Actions", false).Menu(a.scmMenu)
}

// scmMenu is the menu of the view's actions.
func (a *app) scmMenu(m *ui.Menu) {
	g := &a.git
	st := g.status
	if m.Item("Commit").Disabled(g.busy != "").Chosen() {
		a.post(func() { a.commit(false) })
	}
	if m.Item("Commit & Push").Disabled(g.busy != "").Chosen() {
		a.post(func() { a.commit(true) })
	}
	if m.Item("Amend Last Commit").Disabled(st == nil || st.Head == "").Chosen() {
		a.post(a.startAmend)
	}
	if m.Item("Undo Last Commit").Disabled(g.busy != "" || st == nil || st.Head == "").Chosen() {
		a.post(a.undoCommit)
	}
	m.Separator()
	groups := a.groupEntries()
	if m.Item("Open All Changes").Disabled(len(groups[groupStaged])+len(groups[groupChanges])+len(groups[groupUntracked]) == 0).Chosen() {
		a.post(func() { a.openChangesTab(true) })
	}
	m.Separator()
	if m.Item("Stage All Changes").Disabled(len(groups[groupChanges])+len(groups[groupUntracked])+len(groups[groupMerge]) == 0).Chosen() {
		a.post(a.stageAll)
	}
	if m.Item("Unstage All Changes").Disabled(len(groups[groupStaged]) == 0).Chosen() {
		a.post(func() { a.unstage(a.groupPaths(groupStaged)...) })
	}
	if m.Item("Discard All Changes…").Disabled(len(groups[groupChanges])+len(groups[groupUntracked]) == 0).Chosen() {
		a.post(func() { a.discard(append(groups[groupChanges], groups[groupUntracked]...)...) })
	}
	m.Separator()
	if m.Item("Fetch").Disabled(g.busy != "").Chosen() {
		a.post(a.fetch)
	}
	if m.Item("Pull").Disabled(g.busy != "").Chosen() {
		a.post(a.pull)
	}
	if m.Item("Push").Disabled(g.busy != "").Chosen() {
		a.post(a.push)
	}
	m.Separator()
	if m.Item("View as Tree").Checked(a.settings.ScmTree).Chosen() {
		a.post(func() {
			a.settings.ScmTree = !a.settings.ScmTree
			a.settings.Changed()
		})
	}
	if m.Item("Show All Branches").Checked(a.settings.ScmAllBranches).Chosen() {
		a.post(a.toggleAllBranches)
	}
	if m.Item("Refresh").Chosen() {
		a.post(func() {
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
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(col) })
	return b.Clicked()
}

// groupPaths returns the paths of the files of a group: of a file
// renamed and staged, where it was too, which unstaging puts back.
func (a *app) groupPaths(g scmGroup) []string {
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
func (a *app) chooseScmFile(f *diffFile) {
	for i, r := range a.scm.rows {
		if !r.header && r.e.Path == f.path && (r.group == groupStaged) == (f.base == diffStaged) {
			a.scm.row = i
			return
		}
	}
}

// openChange shows the changes of a file of the view: those staged, or
// those not, or the file itself while it is in conflict.
func (a *app) openChange(r scmRow, focus bool) {
	switch r.group {
	case groupMerge:
		a.openPath(r.e.Path, focus)
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
		return "Open Folder"
	}
	return "Open File"
}

// entryIcon is the icon of a changed file, or of a submodule.
func entryIcon(p *palette, e git.Entry, name string) (*ui.SVG, ui.Color) {
	if e.Submodule() {
		return iconFolder, p.accent
	}
	return fileIcon(p, name)
}

// toggleStaged stages a file of the view, or unstages it when staged.
func (a *app) toggleStaged(r scmRow) {
	if r.group == groupStaged {
		a.unstage(entryPaths(r.e, true)...)
	} else {
		a.stage(r.e.Path)
	}
}

func (a *app) stage(paths ...string) {
	if len(paths) == 0 {
		return
	}
	a.gitDo("", func(r *git.Repo) error { return r.Stage(paths...) }, nil)
}

func (a *app) unstage(paths ...string) {
	if len(paths) == 0 {
		return
	}
	a.gitDo("", func(r *git.Repo) error { return r.Unstage(paths...) }, nil)
}

// stageAll stages every change, and the files Git does not track.
func (a *app) stageAll() {
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
func (a *app) discard(es ...git.Entry) {
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
	title := fmt.Sprintf("Discard the changes of %d files?", len(es))
	if len(es) == 1 {
		title = fmt.Sprintf("Discard the changes of %s?", filepath.Base(es[0].Path))
		if es[0].Untracked() {
			title = fmt.Sprintf("Move %s to %s?", filepath.Base(es[0].Path), trashName())
		}
	}
	msg := "The changes cannot be undone."
	if len(untracked) > 0 {
		msg = "The changes cannot be undone. Files Git does not track go to " + trashName() + "."
	}
	a.confirm = &confirm{title: title, message: msg, ok: "Discard", danger: true, run: func() {
		for _, p := range untracked {
			if err := a.host.trash(p); err != nil {
				a.fail("Could not move %s to %s: %v", filepath.Base(p), trashName(), err)
			}
		}
		if a.tree != nil && len(untracked) > 0 {
			a.tree.Refresh()
			a.indexStale = true
		}
		if len(tracked) > 0 {
			a.gitDo("", func(r *git.Repo) error { return r.Discard(tracked...) }, func() {
				// Open documents show the files as they are again, unsaved
				// changes too, as a step of undo.
				for _, p := range tracked {
					if i := a.docIndex(p); i >= 0 {
						a.reloadDoc(a.docs[i])
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
func (a *app) startAmend() {
	g := &a.git
	g.amend = true
	if strings.TrimSpace(g.message) != "" || g.repo == nil {
		return
	}
	r := g.repo
	go func() {
		msg := r.LastMessage()
		a.post(func() {
			if g.amend && strings.TrimSpace(g.message) == "" {
				g.message = msg
			}
		})
	}()
}

// undoCommit takes back the last commit, keeping its changes staged and
// its message in the box, to commit again.
func (a *app) undoCommit() { a.undoCommitOf("") }

// undoCommitOf takes back the last commit when it is hash, or whatever it
// is for "": an Undo offered for a commit undoes it, not one made since.
func (a *app) undoCommitOf(hash string) {
	g := &a.git
	if g.repo == nil || g.status == nil || g.status.Head == "" {
		return
	}
	var msg string
	a.gitDo("Undoing the last commit…", func(r *git.Repo) error {
		if hash != "" && r.Head() != hash {
			return errors.New("a commit has been made since, so this one is no longer the last to undo")
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
func (a *app) commit(push bool) {
	g := &a.git
	if g.repo == nil || g.busy != "" {
		return
	}
	msg := strings.TrimSpace(g.message)
	if msg == "" {
		a.fail("Write a commit message first.")
		return
	}
	for _, d := range a.docs {
		if d.path != "" && d.dirty() && g.repo.Contains(d.path) {
			a.saveDoc(d, nil)
		}
	}
	amend := g.amend
	run := func(stageAll bool) {
		var made string // the commit made
		a.gitDo("Committing…", func(r *git.Repo) error {
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
				a.tell(note{message: "Amended the last commit"})
			default:
				subject, _, _ := strings.Cut(msg, "\n")
				where := ""
				if b := a.branch(); b != "" {
					where = " to " + b
				}
				a.tell(note{message: fmt.Sprintf("Committed “%s”%s", ellipsize(subject, 48), where), label: "Undo",
					action: func() { a.undoCommitOf(made) }})
			}
		})
	}
	groups := a.groupEntries()
	switch {
	case amend || len(groups[groupStaged]) > 0:
		run(false)
	case len(groups[groupMerge]) > 0:
		a.fail("Resolve the merge conflicts and stage the files first.")
	case len(groups[groupChanges])+len(groups[groupUntracked]) > 0:
		a.confirm = &confirm{title: "Stage all changes and commit them?", ok: "Stage All & Commit",
			message: "There are no staged changes to commit.", run: func() { run(true) }}
	default:
		a.fail("There are no changes to commit.")
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
	go func() {
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
	}()
}

// showExplorer shows the files in the sidebar.
func (a *app) showExplorer() {
	a.settings.SidebarOpen, a.settings.SidebarView = true, ""
	a.settings.Changed()
}

// showSourceControl shows the changes in the sidebar.
func (a *app) showSourceControl() {
	a.settings.SidebarOpen, a.settings.SidebarView = true, "git"
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
func (a *app) scmBranchRow(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 8, 0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		branch := a.branch()
		if branch == "" {
			branch = "No Branch"
		}
		b := ui.ButtonBase(c.Key("scm-branch")).Height(24).Padding(0, 6).Gap(5).Radius(5).AlignItems(ui.Center).
			Shrink(1).Label("Branch " + branch).Tooltip("Switch Branch…").Disabled(a.git.busy != "")
		col := p.text
		if b.Hovered() || b.Pressed() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Icon(c, iconBranch).FontSize(13).TextColor(p.muted)
			ui.Text(c, branch).FontSize(12.5).FontWeight(500).TextColor(col).SingleLine().Shrink(1)
			ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.faint)
		})
		if b.Clicked() {
			a.openBranchPicker()
		}
		ui.Spacer(c)
		a.scmActions(c)
	})
}

// scmActions are the buttons of the branch's remote: fetching, and
// pulling and pushing, which say how many commits they would bring, or
// publishing a branch that has no upstream yet.
func (a *app) scmActions(c *ui.Context) {
	g := &a.git
	st := g.status
	busy := g.busy != ""
	if len(g.remotes) == 0 {
		return
	}
	if a.syncButton(c, "fetch", iconFetch, "Fetch", 0).Disabled(busy).Clicked() {
		a.fetch()
	}
	if st != nil && st.Branch != "" && st.Upstream == "" {
		if a.syncButton(c, "publish", iconPush, "Publish Branch", 0).Disabled(busy).Clicked() {
			a.push()
		}
		return
	}
	behind, ahead := 0, 0
	if st != nil {
		behind, ahead = st.Behind, st.Ahead
	}
	pull, push := "Pull", "Push"
	if behind > 0 {
		pull = fmt.Sprintf("Pull %s", plural(behind, "commit"))
	}
	if ahead > 0 {
		push = fmt.Sprintf("Push %s", plural(ahead, "commit"))
	}
	if a.syncButton(c, "pull", iconPull, pull, behind).Disabled(busy).Clicked() {
		a.pull()
	}
	if a.syncButton(c, "push", iconPush, push, ahead).Disabled(busy).Clicked() {
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
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(11.5).FontWeight(600).TextColor(col)
		}
	})
	return b
}
