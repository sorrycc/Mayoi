package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// sidebar is the explorer, the workspace's files in a tree, or the
// changes to commit, as the switch at its top chooses.
func (a *app) sidebar(c *ui.Context) {
	p := a.pal
	sb := ui.Column(c).Fill().Background(p.chrome)
	hovered := sb.Hovered()
	view := a.settings.SidebarView
	if a.root == "" {
		view = ""
	}
	scm, search := view == "git", view == "search"
	sb.Children(func() {
		ui.Row(c).Height(34).Padding(0, 6, 0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
			if a.root != "" {
				a.sidebarSwitch(c, iconFiles, a.withKey("Explorer", "view.explorer"), !scm && !search, 0, a.showExplorer)
				a.sidebarSwitch(c, iconSearch, a.withKey("Search", "edit.findInFiles"), search, 0, func() { a.showSearch(false) })
				a.sidebarSwitch(c, iconBranch, a.withKey("Source Control", "view.scm"), scm, a.changeCount(), a.showSourceControl)
			}
			title, short, actions := "EXPLORER", "", 0
			switch {
			case scm:
				title, short = "SOURCE CONTROL", "GIT"
				if a.git.repo != nil {
					actions = 1
				}
			case search:
				title = "SEARCH"
				if hovered {
					actions = 3
				}
			}
			// The switches, the gaps, the paddings and the actions' buttons
			// take the rest of the row.
			room := a.settings.SidebarWidth - 1 - 14 - 6 - float32(actions)*28
			if a.root != "" {
				room -= 3*26 + 2*3
			}
			title = fitTitle(c, title, short, room)
			ui.Text(c, title).FontSize(11).FontWeight(600).LetterSpacing(titleSpacing).TextColor(p.muted).Grow(1).
				Padding(0, 0, 0, 6).SingleLine()
			if scm {
				a.scmTitleActions(c)
				return
			}
			if search {
				a.searchTitleActions(c, hovered)
				return
			}
		})
		if a.root == "" {
			a.noFolder(c)
			return
		}
		if scm {
			a.scmView(c)
			return
		}
		if search {
			a.searchView(c)
			return
		}
		// The folder's row, with the explorer's actions while the pointer
		// is over the sidebar or the tree has the keyboard: on the title's
		// row, they would leave the title no room.
		ui.Row(c).Height(24).Padding(0, 6, 0, 16).Gap(2).AlignItems(ui.Center).Children(func() {
			ui.Text(c, strings.ToUpper(filepath.Base(a.root))).FontSize(11).FontWeight(700).LetterSpacing(0.4).
				TextColor(p.text).SingleLine().Grow(1)
			if !hovered && !a.files.List.FocusWithin(c) {
				return
			}
			if a.iconButton(c, iconFilePlus, "New File…", false).Clicked() {
				a.askNewEntry(a.targetDir(), false)
			}
			if a.iconButton(c, iconFolderPlus, "New Folder…", false).Clicked() {
				a.askNewEntry(a.targetDir(), true)
			}
			if a.iconButton(c, iconRefresh, "Refresh Explorer", false).Clicked() {
				a.tree.Refresh()
				a.indexStale = true
			}
			if a.iconButton(c, iconCollapse, "Collapse Folders", false).Clicked() {
				a.files.Open.Clear()
			}
		})
		a.fileTree(c)
	})
}

// titleSpacing is the letter spacing of the sidebar's title.
const (
	titleSpacing = 0.6
	explorerRowH = 21
)

// fitTitle returns the sidebar's title, or its short form when the title
// would not fit in room, as the sidebar is narrow or the buttons beside it
// are many: cut short, it would read as neither.
func fitTitle(c *ui.Context, title, short string, room float32) string {
	if short == "" {
		return title
	}
	w, _ := c.MeasureText(0, ui.Span{Text: title, Size: 11, Weight: 600})
	if w+titleSpacing*float32(len(title)) <= room {
		return title
	}
	return short
}

// sidebarSwitch is a button at the top of the sidebar that shows a view
// of it, with a count of what it has when count is more than 0.
func (a *app) sidebarSwitch(c *ui.Context, icon *ui.SVG, label string, on bool, count int, show func()) {
	p := a.pal
	// No tooltip: it would show over the title bar, and the title beside
	// the buttons names the view.
	b := ui.ButtonBase(c.Key(label)).Size(26, 24).Radius(5).Justify(ui.Center).AlignItems(ui.Center).Label(label)
	col := p.muted
	if on {
		b.Background(p.active)
		col = p.text
	} else if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		if count > 0 {
			label := fmt.Sprint(count)
			if count > 99 {
				label = "99+"
			}
			ui.Text(c, label).Absolute().Top(-2).Right(-3).FontSize(9).FontWeight(600).Padding(0, 3).Radius(6).
				MinWidth(13).TextAlign(ui.Center).Background(p.fill).TextColor(p.fillText)
		}
	})
	if b.Clicked() {
		show()
	}
}

func (a *app) noFolder(c *ui.Context) {
	p := a.pal
	// As source control shows a folder without a repository.
	ui.Column(c).Padding(28, 20, 16, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFolderOpen).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, "No Folder Open").FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, "Open a folder to browse its files, search them and track their changes.").
			FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		ui.Box(c).Height(4)
		if ui.PrimaryButton(c, "Open Folder").FillWidth().Clicked() {
			a.chooseFolder()
		}
	})
}

// fileTree shows the files: a click on a folder opens or closes it, on a
// file shows it in the editor.
func (a *app) fileTree(c *ui.Context) {
	p := a.pal
	t := a.tree
	t.Load(a.root)
	children := func(path string) []string {
		kids := t.Children(path)
		if kids != nil && a.files.Open.Has(path) {
			t.Load(path)
		}
		return kids
	}
	activePath := ""
	if t := a.activeTab(); t != nil {
		activePath = t.path()
	}
	// Follow the document shown: open the folders above it and choose it.
	if activePath != a.treeSynced {
		a.treeSynced = activePath
		a.revealInTree(activePath)
	}
	if a.selectInTree != "" {
		for i := range a.files.Rows() {
			if a.files.Item(i) == a.selectInTree {
				a.fileRow = i
				a.selectInTree = ""
				a.files.List.ScrollIntoView(i)
			}
		}
	}
	// Rows show chosen in a tint of the accent, not in the accent itself,
	// which would hide the icons: the list takes the colors from the theme
	// as it builds.
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var clicked, kept string
	var action func()
	// end is where the last row ends, while it shows: a click below it is
	// on no file.
	end := float32(-1)
	out := ui.Outline(c.Key("files"), &a.files, t.Children(a.root), children, func(path string) {
		isDir := t.IsDir(path)
		name := filepath.Base(path)
		row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(explorerRowH)
		if n := a.files.Rows(); n > 0 && a.files.Item(n-1) == path {
			if r := row.Bounds(); r.H > 0 {
				end = r.Y + r.H
			}
		}
		if row.Clicked() {
			clicked = path
		}
		// The row, not the list's, takes the clicks, and so the double
		// click that keeps a file open.
		if row.DoubleClicked() && !isDir {
			kept = path
		}
		if !isDir {
			a.dragFile(row, &fileDrag{path: path})
		}
		row.ContextMenu(func(m *ui.Menu) {
			dir := path
			if !isDir {
				dir = filepath.Dir(path)
			}
			if m.Item("New File…").Chosen() {
				action = func() { a.askNewEntry(dir, false) }
			}
			if m.Item("New Folder…").Chosen() {
				action = func() { a.askNewEntry(dir, true) }
			}
			m.Separator()
			if m.Item(revealLabel()).Chosen() {
				action = func() { a.host.reveal(path) }
			}
			if !isDir && m.Item("Open in Terminal").Chosen() {
				action = func() { a.newTerminalIn(dir) }
			}
			if isDir && m.Item("Open in Terminal").Chosen() {
				action = func() { a.newTerminalIn(path) }
			}
			m.Separator()
			if _, changed := a.git.entries[path]; changed && !isDir {
				if m.Item("Open Changes").Chosen() {
					action = func() { a.showChanges(diffTarget{path: path, any: true}, true) }
				}
				m.Separator()
			}
			if m.Item("Copy").Chosen() {
				action = func() { a.copyFiles(path) }
			}
			if m.Item("Paste").Disabled(len(a.host.clipboardFiles()) == 0).Chosen() {
				action = func() { a.pasteFiles(dir) }
			}
			m.Separator()
			if m.Item("Copy Path").Chosen() {
				action = func() { a.services.WriteClipboard(path) }
			}
			if m.Item("Copy Relative Path").Chosen() {
				action = func() { a.services.WriteClipboard(t.Rel(path)) }
			}
			m.Separator()
			if m.Item("Rename…").Chosen() {
				action = func() { a.askRename(path) }
			}
			if m.Item(trashLabel()).Chosen() {
				action = func() { a.trash(path) }
			}
		})
		row.Children(func() {
			ignored := a.isIgnored(path)
			icon, col := iconFolder, p.accent
			switch {
			case isDir && a.files.Open.Has(path):
				icon = iconFolderOpen
			case !isDir:
				icon, col = fileIcon(p, name)
			}
			if ignored {
				col = col.Alpha(0.5)
			}
			ui.Icon(c, icon).FontSize(14).TextColor(col)
			label := ui.Text(c, name).FontSize(13).SingleLine().Shrink(1).TextColor(p.text)
			// What Git has of the file: its letter, or a dot by a folder
			// that has changes.
			var letter byte
			if isDir {
				letter = a.git.dirs[path]
			} else if e, ok := a.git.entries[path]; ok {
				letter = e.Letter()
			}
			switch i := a.docIndex(path); {
			case i >= 0 && a.docs[i].dirty():
				label.TextColor(p.modified)
			case ignored:
				label.TextColor(p.faint)
			case letter != 0:
				label.TextColor(gitColor(p, letter))
			case path != activePath && !isDir:
				label.TextColor(mix(p.text, p.muted, 0.35))
			}
			if letter == 0 {
				return
			}
			ui.Spacer(c)
			if isDir {
				ui.Box(c).Size(6, 6).Radius(3).Margin(0, 6, 0, 0).Background(gitColor(p, letter).Alpha(0.8))
				return
			}
			ui.Text(c, string(letter)).FontSize(11).FontWeight(600).Font(a.font.family).Padding(0, 4, 0, 0).
				TextColor(gitColor(p, letter)).Shrink(0)
		})
	}).Grow(1).Padding(0, 6, 8, 6).Gap(0)
	a.takeKeys(out, placeExplorer, a.explorerInput)
	// The space below the files, while the last shows: a click there
	// chooses none, so that new files go in the workspace, and its menu is
	// the workspace's. The pointer is followed only then.
	below := false
	if end >= 0 {
		_, y, over := out.PointerPosition()
		below = over && out.Bounds().Y+y > end
	}
	if out.Clicked() && clicked == "" && below {
		a.fileRow = -1
	}
	out.ContextMenu(func(m *ui.Menu) {
		// Beside a row, as by its arrow, no menu: an empty one does not
		// open.
		if !below {
			return
		}
		a.fileRow = -1
		if m.Item("New File…").Chosen() {
			action = func() { a.askNewEntry(a.root, false) }
		}
		if m.Item("New Folder…").Chosen() {
			action = func() { a.askNewEntry(a.root, true) }
		}
		m.Separator()
		if m.Item(revealLabel()).Chosen() {
			action = func() { a.host.reveal(a.root) }
		}
		if m.Item("Open in Terminal").Chosen() {
			action = func() { a.newTerminalIn(a.root) }
		}
		m.Separator()
		if m.Item("Paste").Disabled(len(a.host.clipboardFiles()) == 0).Chosen() {
			action = func() { a.pasteFiles(a.root) }
		}
	})
	if a.files.List.Shortcut(c, 0, ui.KeyEscape) {
		a.fileRow = -1
	}
	c.SetTheme(base)
	if action != nil {
		action()
	}
	if out.Submitted() && a.fileRow >= 0 && a.fileRow < a.files.Rows() {
		// Enter: keep the file open with the focus.
		if path := a.files.Item(a.fileRow); !t.IsDir(path) {
			a.openFile(path, true)
		}
	}
	if clicked != "" {
		for i := range a.files.Rows() {
			if a.files.Item(i) == clicked {
				a.fileRow = i
			}
		}
		if t.IsDir(clicked) {
			if a.files.Open.Has(clicked) {
				a.files.Open.Remove(clicked)
			} else {
				a.files.Open.Add(clicked)
			}
		} else if kept != clicked {
			a.previewFile(clicked)
		}
	}
	if kept != "" {
		// A double click: keep the file open with the focus.
		a.openFile(kept, true)
	}
}

// targetDir is where new files go: the folder chosen in the tree, or the
// chosen file's, or the workspace's.
func (a *app) targetDir() string {
	if a.fileRow >= 0 && a.fileRow < a.files.Rows() {
		p := a.files.Item(a.fileRow)
		if a.tree.IsDir(p) {
			return p
		}
		return filepath.Dir(p)
	}
	return a.root
}

// revealInTree opens the folders above a file, and chooses it and scrolls
// to it once the tree lists it.
func (a *app) revealInTree(path string) {
	if a.tree == nil || !strings.HasPrefix(path, a.root+string(filepath.Separator)) {
		return
	}
	a.selectInTree = path
	for d := filepath.Dir(path); d != a.root && len(d) > len(a.root); d = filepath.Dir(d) {
		a.files.Open.Add(d)
		a.tree.Load(d)
	}
}

func (a *app) askNewEntry(dir string, folder bool) {
	title := "New File"
	if folder {
		title = "New Folder"
	}
	a.prompt = &prompt{title: title, hint: "in " + a.relOrPath(dir), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return "Enter a name."
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if _, err := os.Lstat(path); err == nil {
			return "A file or folder with that name exists."
		}
		if folder {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err.Error()
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err.Error()
			}
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				return err.Error()
			}
		}
		a.files.Open.Add(dir)
		a.tree.Refresh()
		a.indexStale = true
		if !folder {
			a.openFile(path, true)
		}
		return ""
	}}
}

func (a *app) askRename(path string) {
	a.prompt = &prompt{title: "Rename", hint: a.relOrPath(path), value: filepath.Base(path), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsRune(name, filepath.Separator) {
			return "Enter a name without slashes."
		}
		to := filepath.Join(filepath.Dir(path), name)
		if to == path {
			return ""
		}
		if _, err := os.Lstat(to); err == nil {
			return "A file or folder with that name exists."
		}
		if err := os.Rename(path, to); err != nil {
			return err.Error()
		}
		// Open documents follow the files renamed.
		for _, d := range a.docs {
			if d.path == path || strings.HasPrefix(d.path, path+string(filepath.Separator)) {
				d.path = to + d.path[len(path):]
				d.diskGone = false
			}
		}
		a.tree.Refresh()
		a.indexStale = true
		a.rememberTabs()
		return ""
	}}
}

func (a *app) trash(path string) {
	if err := a.host.trash(path); err != nil {
		a.fail("Could not move %s to %s: %v", filepath.Base(path), trashName(), err)
		return
	}
	a.tree.Refresh()
	a.indexStale = true
}

func (a *app) relOrPath(path string) string {
	if a.root != "" {
		if path == a.root {
			return filepath.Base(a.root)
		}
		if strings.HasPrefix(path, a.root+string(filepath.Separator)) {
			return filepath.ToSlash(path[len(a.root)+1:])
		}
	}
	return shortenHome(path)
}

// gitColor is the color of a file of a letter of Git's: changed, added,
// removed or in conflict.
func gitColor(p *palette, letter byte) ui.Color {
	switch letter {
	case 'A', 'U':
		return p.added
	case 'D':
		return p.removed
	case 'C':
		return p.danger
	}
	return p.modified
}

// mix blends from toward to by t.
func mix(from, to ui.Color, t float32) ui.Color { return from.Mix(to, t) }
