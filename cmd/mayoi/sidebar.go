package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// sidebar is the explorer, the workspace's files in a tree, or the
// changes to commit, as the switch at its top chooses.
func (a *app) sidebar(c *ui.Context) {
	p := a.pal
	sb := ui.Column(c).Fill().Background(p.chrome)
	hovered := sb.Hovered()
	view := a.window.SidebarView
	if a.root == "" {
		view = ""
	}
	scm, search := view == "git", view == "search"
	sb.Children(func() {
		ui.Row(c).Height(34).Padding(0, 6, 0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
			if a.root != "" {
				a.sidebarSwitch(c, iconFiles, a.withKey(locales.SidebarExplorer(), "view.explorer"), !scm && !search, 0, a.showExplorer)
				a.sidebarSwitch(c, iconSearch, a.withKey(locales.SidebarSearch(), "edit.findInFiles"), search, 0, func() { a.showSearch(false) })
				a.sidebarSwitch(c, iconBranch, a.withKey(locales.SidebarScm(), "view.scm"), scm, a.changeCount(), a.showSourceControl)
			}
			title, short, actions := locales.SidebarTitleExplorer(), "", 0
			switch {
			case scm:
				title, short = locales.SidebarTitleScm(), locales.SidebarTitleScmShort()
				if a.repository().git.repo != nil {
					actions = 1
				}
			case search:
				title = locales.SidebarTitleSearch()
				if hovered {
					actions = 3
				}
			}
			// The switches, the gaps, the paddings and the actions' buttons
			// take the rest of the row.
			room := a.window.SidebarWidth - 1 - 14 - 6 - float32(actions)*28
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
		// The view chosen fades in as the switches change it.
		pane := ui.Column(c.Key("view:" + view)).Grow(1).MinHeight(0)
		pane.Opacity(pane.Keyframes("in", []ui.Keyframe{{Value: 0}, {At: 150 * time.Millisecond, Value: 1, Ease: ui.EaseOut}}))
		pane.Children(func() {
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
				if a.iconButton(c, iconFilePlus, locales.NewFileEllipsis(), false).Clicked() {
					a.askNewEntry(a.targetDir(), false)
				}
				if a.iconButton(c, iconFolderPlus, locales.NewFolderEllipsis(), false).Clicked() {
					a.askNewEntry(a.targetDir(), true)
				}
				if a.iconButton(c, iconRefresh, locales.RefreshExplorer(), false).Clicked() {
					a.tree.Refresh()
					a.indexStale = true
				}
				if a.iconButton(c, iconCollapse, locales.CollapseFolders(), false).Clicked() {
					a.files.Open.Clear()
				}
			})
			if t := a.tree; t.Loaded(a.root) && len(t.Children(a.root)) == 0 {
				a.emptyFolder(c)
			}
			if a.conn != nil && !a.tree.Loaded(a.root) {
				a.remoteTreeWaits(c)
				return
			}
			a.fileTree(c)
		})
	})
}

// emptyFolder is what the explorer shows of a folder with no files yet,
// with buttons that make the first, as noFolder is for no folder.
func (a *app) emptyFolder(c *ui.Context) {
	p := a.pal
	ui.Column(c.Key("empty-folder")).Padding(28, 20, 8, 20).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconFolderOpen).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, locales.FolderEmpty()).FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, locales.FolderEmptyDetail()).
			FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		ui.Box(c).Height(4)
		if ui.PrimaryButton(c, locales.NewFileEllipsis()).FillWidth().Clicked() {
			a.askNewEntry(a.root, false)
		}
		if ui.Button(c, locales.NewFolderEllipsis()).FillWidth().Clicked() {
			a.askNewEntry(a.root, true)
		}
	})
}

// titleSpacing is the letter spacing of the sidebar's title; treeStep is
// how far each level of a tree indents, the Explorer's and Source
// Control's, as tight as VS Code's.
const (
	titleSpacing = 0.6
	explorerRowH = 21
	treeStep     = 8
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

// switchMark is the identity of the mark under the sidebar's switch of
// the view shown, which goes from one switch to another.
type switchMark struct{}

// sidebarSwitch is a button at the top of the sidebar that shows a view
// of it, with a count of what it has when count is more than 0.
func (a *app) sidebarSwitch(c *ui.Context, icon *ui.SVG, label string, on bool, count int, show func()) {
	p := a.pal
	// No tooltip: it would show over the title bar, and the title beside
	// the buttons names the view.
	b := ui.ButtonBase(c.Key(label)).Size(26, 24).Radius(5).Justify(ui.Center).AlignItems(ui.Center).Label(label)
	col := p.muted
	if on {
		col = p.text
	} else if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		if on {
			// The mark of the view shown slides from the switch before.
			ui.Box(c.Key("on")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(5).Background(p.active).
				Transition(ui.ElementTransition{Animation: &ui.Snappy, Match: switchMark{}})
		}
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		if count > 0 {
			label := fmt.Sprint(count)
			if count > 99 {
				label = "99+"
			}
			// The count rolls to a new one, as a counter's digits do.
			ui.Text(c.Key("count"), label).Absolute().Top(-2).Right(-3).FontSize(9).FontWeight(600).Padding(0, 3).Radius(6).
				MinWidth(13).TextAlign(ui.Center).Background(p.fill).TextColor(p.fillText).
				ContentTransition(ui.ContentTransition{Numeric: true}).Transition(dotPop)
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
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconFolderOpen).FontSize(20).TextColor(p.muted)
		})
		ui.Text(c, locales.NoFolderOpen()).FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, locales.NoFolderDetail()).
			FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		ui.Box(c).Height(4)
		if ui.PrimaryButton(c, locales.OpenFolder()).FillWidth().Clicked() {
			a.chooseFolder()
		}
		if remoteSupported && ui.Button(c.Key("no-folder-remote"), locales.CmdRemoteConnect()).FillWidth().Clicked() {
			a.openRemotePicker()
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
	// on no file. top is where the first row starts, while it shows.
	end, top := float32(-1), float32(-1)
	// activeDir is the folder of the document shown, whose guide is
	// brighter, as the block of the caret's is in the editor.
	activeDir := ""
	if activePath != "" {
		activeDir = filepath.Dir(activePath)
	}
	out := ui.Outline(c.Key("files"), &a.files, t.Children(a.root), children, func(path string) {
		isDir := t.IsDir(path)
		name := filepath.Base(path)
		row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(explorerRowH)
		if n := a.files.Rows(); n > 0 && a.files.Item(n-1) == path {
			if r := row.Bounds(); r.H > 0 {
				end = r.Y + r.H
			}
		}
		if n := a.files.Rows(); n > 0 && a.files.Item(0) == path {
			if r := row.Bounds(); r.H > 0 {
				top = r.Y
			}
		}
		if depth := treeDepth(a.root, path); depth > 0 {
			row.Draw(func(pt *ui.Painter, r ui.Rect) { a.treeGuides(pt, r, path, depth, activeDir) })
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
		// Files of the Finder dropped on a row copy into its folder, up to
		// a host for a remote folder.
		if paths := row.DroppedFiles(); paths != nil {
			srcs, into := slices.Clone(paths), path
			if !isDir {
				into = filepath.Dir(path)
			}
			action = func() { a.copyPathsInto(srcs, into) }
		}
		if row.FileDragOver() {
			row.Background(p.accent.Alpha(0.16)).Radius(4)
		}
		row.ContextMenu(func(m *ui.Menu) {
			dir := path
			if !isDir {
				dir = filepath.Dir(path)
			}
			if m.Item(locales.NewFileEllipsis()).Chosen() {
				action = func() { a.askNewEntry(dir, false) }
			}
			if m.Item(locales.NewFolderEllipsis()).Chosen() {
				action = func() { a.askNewEntry(dir, true) }
			}
			m.Separator()
			if a.conn == nil && m.Item(revealLabel()).Chosen() {
				action = func() { a.host.reveal(path) }
			}
			if !isDir && m.Item(locales.OpenInTerminal()).Chosen() {
				action = func() { a.openTerminal(dir) }
			}
			if isDir && m.Item(locales.OpenInTerminal()).Chosen() {
				action = func() { a.openTerminal(path) }
			}
			m.Separator()
			if _, changed := a.repositoryForPath(path).git.entries[path]; changed && !isDir {
				if m.Item(locales.CmdGitOpenChanges()).Chosen() {
					action = func() { a.repositoryForPath(path).showChanges(diffTarget{path: path, any: true}, true) }
				}
				m.Separator()
			}
			if m.Item(locales.CopyLabel()).Chosen() {
				action = func() { a.copyFiles(path) }
			}
			if m.Item(locales.PasteLabel()).Disabled(len(a.clipboardSources()) == 0).Chosen() {
				action = func() { a.pasteFiles(dir) }
			}
			m.Separator()
			if a.conn != nil {
				if m.Item(locales.UploadEllipsis()).Chosen() {
					action = func() { a.askUpload(dir) }
				}
				if m.Item(locales.DownloadEllipsis()).Chosen() {
					action = func() { a.askDownload(path) }
				}
				m.Separator()
			}
			if m.Item(locales.CopyPath()).Chosen() {
				action = func() { a.services.WriteClipboard(hostPath(path)) }
			}
			if m.Item(locales.CopyRelativePath()).Chosen() {
				action = func() { a.services.WriteClipboard(t.Rel(path)) }
			}
			m.Separator()
			if m.Item(locales.RenameEllipsis()).Chosen() {
				action = func() { a.askRename(path) }
			}
			if m.Item(a.trashLabel()).Chosen() {
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
				for _, ctx := range a.rootRepositoryList() {
					if next := ctx.git.dirs[path]; letterRank(next) > letterRank(letter) {
						letter = next
					}
				}
			} else if e, ok := a.repositoryForPath(path).git.entries[path]; ok {
				letter = e.Letter()
			}
			switch i := a.docIndex(path); {
			case i >= 0 && a.docs[i].dirty():
				label.TextColor(p.modifiedFg)
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
	if first, _ := a.files.List.Visible(); a.files.Rows() > 0 && (first > 0 || top >= 0 && top < out.Bounds().Y-0.5) {
		// A shadow under the folder's row, as the files scroll up under it.
		out.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			pt.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 6}, ui.LinearGradient{
				From: p.shadow, To: p.shadow.Alpha(0), Angle: 180,
			}, 0)
		})
	}
	a.takeKeys(out, placeExplorer, a.explorerInput)
	out.Bind(&a.filesView)
	if a.focusFiles {
		a.focusFiles = false
		// Not from a box open over the window, which keeps the keyboard.
		if !a.quick.open && a.prompt == nil && a.confirm == nil {
			a.filesView.Focus()
		}
	}
	// Below the rows, into the workspace.
	if paths := out.DroppedFiles(); paths != nil {
		srcs := slices.Clone(paths)
		action = func() { a.copyPathsInto(srcs, a.root) }
	}
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
		if m.Item(locales.NewFileEllipsis()).Chosen() {
			action = func() { a.askNewEntry(a.root, false) }
		}
		if m.Item(locales.NewFolderEllipsis()).Chosen() {
			action = func() { a.askNewEntry(a.root, true) }
		}
		m.Separator()
		if a.conn == nil && m.Item(revealLabel()).Chosen() {
			action = func() { a.host.reveal(a.root) }
		}
		if m.Item(locales.OpenInTerminal()).Chosen() {
			action = func() { a.openTerminal(a.root) }
		}
		m.Separator()
		if m.Item(locales.PasteLabel()).Disabled(len(a.clipboardSources()) == 0).Chosen() {
			action = func() { a.pasteFiles(a.root) }
		}
		if a.conn != nil && m.Item(locales.UploadEllipsis()).Chosen() {
			action = func() { a.askUpload(a.root) }
		}
	})
	if a.files.List.Shortcut(c, 0, ui.KeyEscape) {
		a.fileRow = -1
	}
	c.SetTheme(base)
	if action != nil {
		action()
	}
	// A file chosen otherwise than by a click on its name, as by a click
	// beside it, the arrows or typing its first letters, shows as well.
	if out.Changed() && clicked == "" && a.fileRow >= 0 && a.fileRow < a.files.Rows() {
		if path := a.files.Item(a.fileRow); !t.IsDir(path) {
			clicked = path
		}
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

// treeDepth returns how deep path is in the tree of root: 0 for the
// files and folders at its top.
func treeDepth(root, path string) int {
	if len(path) <= len(root)+1 || !strings.HasPrefix(path, root) {
		return 0
	}
	return strings.Count(path[len(root)+1:], string(filepath.Separator))
}

// treeGuides paints the indent guides of a row of the tree, depth deep,
// whose content is in r: a line down from the arrow of each folder above
// it, as VS Code draws them, brighter for the folder of the document shown.
func (a *app) treeGuides(pt *ui.Painter, r ui.Rect, path string, depth int, activeDir string) {
	p, th := a.pal, pt.Theme()
	x0 := treeGuideX(r.X, depth, th.Space(4), th.Space(1))
	gap := th.Space(1)
	y, h := r.Y-gap, r.H+2*gap
	dir := filepath.Dir(path)
	for k := depth - 1; k >= 0; k-- {
		x := x0[k]
		col := p.guide
		if dir == activeDir {
			col = p.guideNow
		}
		pt.Fill(ui.Rect{X: x, Y: y, W: 1, H: h}, col, 0)
		dir = filepath.Dir(dir)
	}
}

// treeGuideX returns where the guides of a row depth deep, whose content
// starts at x, go: the middle of the arrow of the folder k levels down, for
// each k under depth. Before the content are the arrow and a gap, and on
// deeper rows their indentation and a gap more.
func treeGuideX(x float32, depth int, arrow, gap float32) []float32 {
	start := x - arrow - gap
	if depth > 0 {
		start -= float32(depth)*treeStep + gap
	}
	xs := make([]float32, depth)
	for k := range xs {
		xs[k] = start + arrow/2
		if k > 0 {
			xs[k] += float32(k)*treeStep + gap
		}
	}
	return xs
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
	title := locales.NewFileTitle()
	if folder {
		title = locales.NewFolderTitle()
	}
	a.prompt = &prompt{title: title, hint: locales.InDir(a.relOrPath(dir)), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return locales.EnterName()
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if remote.Is(path) {
			a.newRemoteEntry(dir, path, folder)
			return ""
		}
		if _, err := os.Lstat(path); err == nil {
			return locales.NameExists()
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
			// A file made anew opens as its source, to edit.
			a.openText(func() { a.openFile(path, true) })
		}
		return ""
	}}
}

// hasSlash reports whether a name has a separator of folders in it: a
// slash, which is one on every system, or Windows' backslash there.
func hasSlash(name string) bool {
	return strings.ContainsAny(name, "/"+string(filepath.Separator))
}

func (a *app) askRename(path string) {
	a.prompt = &prompt{title: locales.RenameTitle(), hint: a.relOrPath(path), value: filepath.Base(path), done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" || hasSlash(name) {
			return locales.EnterNameNoSlashes()
		}
		to := filepath.Join(filepath.Dir(path), name)
		if to == path {
			return ""
		}
		if remote.Is(path) {
			go func() {
				err := vfs.Rename(path, to)
				a.post(func() {
					if err != nil {
						a.fail("%s", locales.FailRename(filepath.Base(path), err.Error()))
						return
					}
					a.renamed(path, to)
				})
			}()
			return ""
		}
		if _, err := os.Lstat(to); err == nil {
			return locales.NameExists()
		}
		if err := os.Rename(path, to); err != nil {
			return err.Error()
		}
		a.renamed(path, to)
		return ""
	}}
}

// renamed has the open documents follow the files renamed.
func (a *app) renamed(path, to string) {
	for _, d := range a.docs {
		if d.path == path || strings.HasPrefix(d.path, path+string(filepath.Separator)) {
			if a.tree != nil {
				a.tree.Unwatch(d.path)
			}
			d.path = to + d.path[len(path):]
			d.diskGone = false
			if a.tree != nil {
				a.tree.Watch(d.path)
			}
		}
	}
	a.tree.Refresh()
	a.indexStale, a.indexed = true, time.Time{}
	a.rememberTabs()
}

// newRemoteEntry makes a file or a folder on a remote folder's host, in
// the background, and opens the file.
func (a *app) newRemoteEntry(dir, path string, folder bool) {
	go func() {
		var err error
		if folder {
			if _, serr := vfs.Lstat(path); serr == nil {
				err = errors.New(locales.ErrNameExists())
			} else {
				err = vfs.MkdirAll(path)
			}
		} else {
			err = vfs.Create(path)
		}
		a.post(func() {
			if err != nil {
				a.fail("%s", locales.FailMake(filepath.Base(path), err.Error()))
				return
			}
			a.files.Open.Add(dir)
			a.tree.Refresh()
			a.indexStale, a.indexed = true, time.Time{}
			if !folder {
				// A file made anew opens as its source, to edit.
				a.openText(func() { a.openFile(path, true) })
			}
		})
	}()
}

// trashLabel is what moving a file to the trash is called, or deleting a
// remote one, which has no trash.
func (a *app) trashLabel() string {
	if a.conn != nil {
		return locales.DeletePermanently()
	}
	return trashLabel()
}

func (a *app) trash(path string) {
	if remote.Is(path) {
		a.confirm = &confirm{title: locales.DeleteQuestion(filepath.Base(path)),
			message: locales.DeleteRemoteDetail(remote.HostOf(path)),
			ok:      locales.ButtonDelete(), danger: true, run: func() {
				go func() {
					err := vfs.RemoveAll(path)
					a.post(func() {
						if err != nil {
							a.fail("%s", locales.FailDelete(filepath.Base(path), err.Error()))
							return
						}
						a.tree.Refresh()
						a.indexStale, a.indexed = true, time.Time{}
					})
				}()
			}}
		return
	}
	if err := a.host.trash(path); err != nil {
		a.fail("%s", locales.FailMoveTo(filepath.Base(path), trashName(), err.Error()))
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
// removed or in conflict, as text.
func gitColor(p *palette, letter byte) ui.Color {
	switch letter {
	case 'A', 'U':
		return p.addedFg
	case 'D':
		return p.removedFg
	case 'C':
		return p.dangerFg
	}
	return p.modifiedFg
}

// mix blends from toward to by t.
func mix(from, to ui.Color, t float32) ui.Color { return from.Mix(to, t) }
