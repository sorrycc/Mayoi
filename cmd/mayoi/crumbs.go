package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// crumb is a part of a file's path as the breadcrumbs show it: its name,
// and the folder it names, "" for the file itself or a name that is no
// folder, as a commit's.
type crumb struct {
	name, dir string
}

// crumbsOf returns the crumbs of a file's path: from the folder open in
// the window when the file is in it, else from the home folder, as ~, the
// host of a remote file, or the root of the file system.
func crumbsOf(path, root, home string) []crumb {
	sep := string(filepath.Separator)
	var out []crumb
	base, rel := "", path
	switch host, rp, ok := remote.Split(path); {
	case root != "" && strings.HasPrefix(path, root+sep):
		base, rel = root, path[len(root)+1:]
	case ok:
		base, rel = remote.Path(host, "/"), strings.TrimPrefix(rp, "/")
		out = append(out, crumb{host, base})
	case home != "" && strings.HasPrefix(path, home+sep):
		base, rel = home, path[len(home)+1:]
		out = append(out, crumb{"~", base})
	default:
		vol := filepath.VolumeName(path)
		base = vol + sep
		rel = strings.TrimPrefix(path[len(vol):], sep)
		if vol != "" {
			out = append(out, crumb{vol, base})
		}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	dir := base
	for i, part := range parts {
		if part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		if i == len(parts)-1 {
			out = append(out, crumb{part, ""})
		} else {
			out = append(out, crumb{part, dir})
		}
	}
	return out
}

// crumbsPicker is the box a folder of the breadcrumbs opens, with the
// files in the folder as a tree of its own: what it opens and chooses
// leaves the Explorer as it is, and it reads its folders itself, so that
// it works for files outside the folder open, and on a host.
type crumbsPicker struct {
	open bool
	// col and tab are where the breadcrumbs were clicked, file the path
	// of the tab's file then, and dir the folder clicked.
	col       *column
	tab       *colTab
	file, dir string
	// gen counts the boxes opened: what a read for an earlier one posts
	// back is dropped.
	gen  int
	tree ui.OutlineState[string]
	sel  int
	view ui.Handle
	// focus gives the tree the keyboard as it shows, and reveal chooses
	// the row of the entry on the way to the file, once it shows.
	focus  bool
	reveal string

	kids    map[string][]string
	dirs    map[string]bool
	loading map[string]bool
	errs    map[string]string
}

// openCrumbs opens the box of the folder dir of the breadcrumbs of tab t
// of column col, or closes it if it is the one open.
func (a *app) openCrumbs(col *column, t *colTab, dir string) {
	k := &a.crumbs
	if k.open && k.tab == t && k.dir == dir {
		k.open = false
		return
	}
	file := t.path()
	reveal := file
	for reveal != "" && filepath.Dir(reveal) != dir && filepath.Dir(reveal) != reveal {
		reveal = filepath.Dir(reveal)
	}
	*k = crumbsPicker{
		open: true, col: col, tab: t, file: file, dir: dir, gen: k.gen + 1,
		tree: ui.OutlineState[string]{Indent: treeStep}, sel: -1, focus: true, reveal: reveal,
		kids: map[string][]string{}, dirs: map[string]bool{dir: true}, loading: map[string]bool{}, errs: map[string]string{},
	}
	k.tree.List.Selected = &k.sel
	a.crumbsLoad(dir)
}

// crumbsLoad reads a folder of the box in the background, unless it was.
func (a *app) crumbsLoad(dir string) {
	k := &a.crumbs
	if _, ok := k.kids[dir]; ok || k.loading[dir] {
		return
	}
	k.loading[dir] = true
	delete(k.errs, dir)
	gen := k.gen
	bg.Go(func() {
		defer recoverCrash()
		entries, err := vfs.ReadDir(dir)
		a.post(func() {
			if a.crumbs.gen != gen {
				return // another box, or none, since
			}
			delete(k.loading, dir)
			if err != nil {
				k.errs[dir] = err.Error()
				return
			}
			kids, dirs := workspace.SortEntries(dir, entries)
			for d := range dirs {
				k.dirs[d] = true
			}
			k.kids[dir] = kids
		})
	})
}

// crumbsValid reports whether the box still belongs to what shows: its
// tab, shown in its column, with the same file.
func (a *app) crumbsValid() bool {
	k := &a.crumbs
	return slices.Contains(a.cols, k.col) && k.col.tab() == k.tab && k.tab.path() == k.file
}

// closeCrumbs closes the box, dropping what it read.
func (a *app) closeCrumbs() {
	a.crumbs = crumbsPicker{gen: a.crumbs.gen + 1}
}

// crumbsOpen shows a file chosen in the box in the column whose
// breadcrumbs opened it, as a preview, with the keyboard, and closes the
// box.
func (a *app) crumbsOpen(path string) {
	col := a.crumbs.col
	a.closeCrumbs()
	if !slices.Contains(a.cols, col) {
		col = a.fileColumn()
	}
	if i := col.showingFile(path); i >= 0 {
		a.activate(col, i, true)
		return
	}
	a.previewIn(col, func(at int) {
		a.opening = true
		a.openFileIn(col, at, path, true)
		a.opening = false
	})
}

// crumbsBox shows the box below the crumb anchor, while it is open.
func (a *app) crumbsBox(c *ui.Context, anchor ui.Element) {
	k := &a.crumbs
	if !k.open {
		return
	}
	ui.PopoverBase(c, anchor, &k.open, func(panel ui.Element) {
		p := a.pal
		a.floating(c, panel.Width(300).Radius(8).Padding(4).Gap(0), false).
			Role(ui.RoleDialog).Label(locales.CrumbsFolderTip(filepath.Base(k.dir)))
		panel.Children(func() {
			kids, read := k.kids[k.dir]
			note := func(s string, col ui.Color) {
				ui.Text(c, s).FontSize(12.5).TextColor(col).Padding(6, 10).MaxWidthPercent(100)
			}
			switch {
			case k.errs[k.dir] != "":
				note(locales.RemoteCouldNotRead(filepath.Base(k.dir), k.errs[k.dir]), p.dangerFg)
			case !read:
				note(locales.Loading(), p.muted)
			case len(kids) == 0:
				note(locales.CrumbsEmpty(), p.muted)
			default:
				a.crumbsTree(c, kids)
			}
		})
	})
	if !k.open {
		a.closeCrumbs()
	}
}

// crumbsMaxH is the most the box's tree is tall, before it scrolls.
const crumbsMaxH = 360

// crumbsTree is the tree of the box: a click on a folder opens or closes
// it, on a file shows it; the keys move among the rows, and Enter does
// what a click does.
func (a *app) crumbsTree(c *ui.Context, kids []string) {
	p := a.pal
	k := &a.crumbs
	rows := k.tree.Rows()
	if rows == 0 {
		rows = len(kids)
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var clicked string
	out := ui.Outline(c.Key("crumbs-tree"), &k.tree, kids, func(path string) []string {
		if !k.dirs[path] {
			return nil
		}
		if k.tree.Open.Has(path) {
			a.crumbsLoad(path)
		}
		if kk, ok := k.kids[path]; ok {
			return kk
		}
		return []string{}
	}, func(path string) {
		isDir := k.dirs[path]
		name := filepath.Base(path)
		row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(explorerRowH).Label(name)
		if row.Clicked() {
			clicked = path
		}
		row.Children(func() {
			icon, col := iconFolder, p.accent
			switch {
			case isDir && k.tree.Open.Has(path):
				icon = iconFolderOpen
			case !isDir:
				icon, col = fileIcon(p, name)
			}
			ui.Icon(c, icon).FontSize(14).TextColor(col)
			label := ui.Text(c, name).FontSize(13).SingleLine().Shrink(1).TextColor(p.text)
			if path == k.file {
				label.FontWeight(600)
			}
			if !isDir || !k.tree.Open.Has(path) {
				return
			}
			_, read := k.kids[path]
			switch {
			case k.errs[path] != "":
				ui.Spacer(c)
				ui.Icon(c, iconX).FontSize(11).TextColor(p.dangerFg).Tooltip(k.errs[path]).Margin(0, 4, 0, 0)
			case !read:
				ui.Spacer(c)
				ui.Text(c, locales.Loading()).FontSize(11).TextColor(p.faint).SingleLine().Shrink(0).Margin(0, 4, 0, 0)
			case len(k.kids[path]) == 0:
				ui.Spacer(c)
				ui.Text(c, locales.CrumbsEmpty()).FontSize(11).TextColor(p.faint).SingleLine().Shrink(0).Margin(0, 4, 0, 0)
			}
		})
	}).Height(min(float32(rows)*explorerRowH, crumbsMaxH)).Gap(0)
	out.Bind(&k.view)
	if k.focus {
		k.focus = false
		k.view.Focus()
	}
	c.SetTheme(base)
	if k.reveal != "" {
		for i := range k.tree.Rows() {
			if k.tree.Item(i) == k.reveal {
				k.sel = i
				k.tree.List.ScrollIntoView(i)
				k.reveal = ""
			}
		}
	}
	if clicked == "" && out.Submitted() && k.sel >= 0 && k.sel < k.tree.Rows() {
		clicked = k.tree.Item(k.sel)
	}
	if clicked == "" {
		return
	}
	if !k.dirs[clicked] {
		a.crumbsOpen(clicked)
		return
	}
	for i := range k.tree.Rows() {
		if k.tree.Item(i) == clicked {
			k.sel = i
		}
	}
	if k.tree.Open.Has(clicked) {
		k.tree.Open.Remove(clicked)
	} else {
		k.tree.Open.Add(clicked)
	}
}

// homeDir is the user's home folder, "" when it is not known.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
