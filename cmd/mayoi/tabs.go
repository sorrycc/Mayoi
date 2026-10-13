package main

import (
	"mayoi/internal/remotefs"
	"slices"

	"github.com/egoist/mygo/ui"
)

// tabStrip shows a tab for each tab of a column: a click shows it, the
// cross closes it, and a dot marks unsaved changes. A tab is dragged
// along the strip, or to another column, and dropped where it goes.
func (a *app) tabStrip(c *ui.Context, col *column) {
	p := a.pal
	var closing *colTab
	// The column that has the keyboard shows it on its tab.
	focused := col == a.column()
	drop := func(e ui.Element, before *colTab) {
		if it, ok := ui.Drop[dragItem](e); ok {
			a.post(func() {
				at := len(col.tabs)
				if _, i := a.locate(before); i >= 0 {
					at = i
				}
				a.dropInColumn(it, col, at)
			})
		}
		// Files from another app, there too.
		if paths := e.DroppedFiles(); paths != nil {
			paths = slices.Clone(paths)
			a.post(func() {
				at := len(col.tabs)
				if _, i := a.locate(before); i >= 0 {
					at = i
				}
				a.dropFiles(paths, col, at)
			})
		}
	}
	ui.Row(c).Height(tabStripH).Background(p.chrome).AlignItems(ui.Center).Children(func() {
		// The strip's edge along the bottom, which the tab shown breaks to
		// join the text below it.
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(p.border)
		strip := ui.ScrollHorizontal(c.Key("tabs")).Grow(1).Height(tabStripH).ScrollbarInsets(0, 0, 0, 0)
		// Past the last tab: there.
		drop(strip, nil)
		_, atEnd := ui.DragOver[dragItem](strip)
		atEnd = atEnd || strip.FileDragOver()
		strip.Children(func() {
			ui.Row(c).Height(tabStripH).AlignItems(ui.Stretch).Children(func() {
				for i, t := range col.tabs {
					active := i == col.active
					tip := "Terminal"
					if t.term == nil {
						tip = a.relOrPath(t.path())
					}
					tab := ui.ButtonBase(c.Key(t.id)).Padding(0, 6, 0, 12).Gap(7).AlignItems(ui.Center).
						BorderWidth(0, 1, 0, 0).BorderColor(p.border).Label(t.name()).Tooltip(tip).
						Drag(dragItem{tab: t})
					if tab.Dragging() {
						a.dragNow = true
					}
					drop(tab, t)
					_, over := ui.DragOver[dragItem](tab)
					over = over || tab.FileDragOver()
					hovered := tab.Hovered()
					switch {
					case active:
						tab.Background(p.editor)
					case hovered:
						tab.Background(mix(p.chrome, p.editor, 0.5))
					}
					if tab.Pressed() && (!active || !focused) {
						a.activate(col, i, true)
					}
					if tab.DoubleClicked() {
						a.keepTab(t)
					}
					if active && col.revealed != t.id {
						// Bring the tab shown into view, once as it is chosen.
						tab.ScrollIntoView()
						col.revealed = t.id
					}
					tab.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, col, t, &closing) })
					tab.Children(func() {
						if !active {
							ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(p.border)
						}
						switch {
						case over:
							// Where the tab dragged goes: before this one.
							ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(2).Background(p.accent)
						case active && focused:
							// The accent line along the top of the tab shown.
							ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(1.5).Background(p.accent)
						case active:
							ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(1.5).Background(p.faint)
						}
						icon, tint := iconTerminal, p.muted
						switch {
						case t.diff != nil && t.diff.commit != nil:
							icon, tint = iconCommit, p.accent
						case t.diff != nil:
							icon, tint = iconDiff, p.changed
						case t.term == nil:
							icon, tint = fileIcon(p, t.name())
						}
						ui.Icon(c, icon).FontSize(14).TextColor(tint)
						name := ui.Text(c, t.name()).FontSize(12.5).SingleLine().MaxWidth(220)
						if t.transient {
							// A preview, which the next file chosen replaces.
							name.Italic()
						}
						switch {
						case t.ed != nil && t.ed.diskGone, t.view != nil && t.view.diskGone:
							name.TextColor(p.danger).Strikethrough()
						case active:
							name.TextColor(p.text)
						default:
							name.TextColor(p.muted)
						}
						// The dot of unsaved changes turns into the cross under the
						// pointer.
						x := ui.ButtonBase(c).Size(18, 18).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label("Close " + t.name())
						if x.Hovered() {
							x.Background(p.hover)
						}
						x.Children(func() {
							switch {
							case t.ed != nil && t.ed.dirty() && !x.Hovered():
								ui.Box(c).Size(8, 8).Radius(4).Background(p.text.Alpha(0.7))
							case active || hovered || x.Hovered():
								ui.Icon(c, iconX).FontSize(13).TextColor(p.muted)
							}
						})
						if x.Clicked() {
							closing = t
						}
					})
				}
				if atEnd {
					ui.Box(c).Width(2).Background(p.accent)
				}
			})
		})
		ui.Row(c).Padding(0, 6, 0, 4).Children(func() {
			if a.iconButton(c, iconColumns, a.withKey("Split Right", "view.splitRight"), false).Clicked() {
				a.post(func() {
					if at := slices.Index(a.cols, col); at >= 0 {
						a.col = at
						a.splitRight()
					}
				})
			}
		})
	})
	if closing != nil {
		// After the frame, which is still drawing the columns: the column
		// may go with its last tab.
		a.post(func() { a.closeTab(closing, nil) })
	}
}

// tabMenu is the menu of a tab.
func (a *app) tabMenu(m *ui.Menu, col *column, t *colTab, closing **colTab) {
	// show makes the tab the one commands are for.
	show := func(fn func()) {
		a.post(func() {
			if col, i := a.locate(t); col != nil {
				a.activate(col, i, true)
				fn()
			}
		})
	}
	if m.Item("Close").Chosen() {
		*closing = t
	}
	if m.Item("Close Others").Disabled(len(col.tabs) < 2).Chosen() {
		a.post(func() { a.closeOthers(col, t) })
	}
	if m.Item("Close All").Chosen() {
		a.post(func() { a.closeTabs(slices.Clone(col.tabs), nil) })
	}
	m.Separator()
	if m.Item("Split Right").Chosen() {
		show(a.splitRight)
	}
	at := slices.Index(a.cols, col)
	alone := len(col.tabs) < 2
	if m.Item("Move to Next Column").Disabled(alone && at == len(a.cols)-1).Chosen() {
		show(func() { a.moveTabBy(1) })
	}
	if m.Item("Move to Previous Column").Disabled(alone && at == 0).Chosen() {
		show(func() { a.moveTabBy(-1) })
	}
	if t.term != nil {
		if m.Item("Move to Panel").Chosen() {
			a.post(func() { a.terminalToPanel(t.term) })
		}
		return
	}
	m.Separator()
	if d := t.ed; d != nil && canPreview(d.document) {
		label := "Show Preview"
		if d.preview != nil {
			label = "Show Source"
		}
		if m.Item(label).Chosen() {
			show(func() { a.setPreview(d, d.preview == nil); a.focusColumn() })
		}
	}
	path := t.path()
	if t.diff != nil {
		if m.Item("Open File").Chosen() {
			show(a.openChanges)
		}
	} else if t.ed != nil && path != "" && a.git.repo != nil && m.Item("Open Changes").Chosen() {
		show(a.openChanges)
	}
	if m.Item("Reveal in Explorer").Disabled(path == "" || a.root == "").Chosen() {
		a.post(func() { a.showExplorer(); a.revealInTree(path) })
	}
	if !remotefs.IsRemote(path) && m.Item(revealLabel()).Disabled(path == "").Chosen() {
		a.host.reveal(path)
	}
	if m.Item("Copy Path").Disabled(path == "").Chosen() {
		a.services.WriteClipboard(remotefs.Display(path))
	}
}
