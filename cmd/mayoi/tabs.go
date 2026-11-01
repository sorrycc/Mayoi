package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// crossFade is how a tab's cross comes and goes with the pointer.
var crossFade = ui.ElementTransition{Duration: 100 * time.Millisecond, Enter: &ui.Motion{}, Exit: &ui.Motion{}}

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
	ui.Row(c).Height(tabStripH).Background(p.strip).AlignItems(ui.Center).
		ContextMenu(func(m *ui.Menu) { a.columnLockMenu(m, col) }).Children(func() {
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
					tip := locales.TabTerminal()
					if t.term == nil {
						tip = a.relOrPath(t.path())
					}
					tab := ui.ButtonBase(c.Key(t.id)).Padding(0, 6, 0, 12).Gap(7).AlignItems(ui.Center).
						BorderWidth(0, 1, 0, 0).BorderColor(p.border).Label(t.name()).Tooltip(tip).
						Drag(dragItem{tab: t}).Transition(tabMotion)
					// The middle button closes the tab, as in a browser.
					box := tab.LayoutBounds()
					tab = tab.HandleInput(func(ev ui.InputEvent) bool { return a.tabMiddle(t, ev, box) })
					if tab.Dragging() {
						a.dragNow = true
					}
					drop(tab, t)
					_, over := ui.DragOver[dragItem](tab)
					over = over || tab.FileDragOver()
					hovered := tab.Hovered()
					under := p.strip
					switch {
					case active:
						under = p.editor
						tab.Background(under)
					case hovered && p.card > 0:
						// In a card, the strip is the editor's color already.
						under = p.hover.Over(p.strip)
						tab.Background(under)
					case hovered:
						under = mix(p.strip, p.editor, 0.5)
						tab.Background(under)
					}
					if tab.Pressed() && a.middle != t && (!active || !focused) {
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
						case active:
							// The line along the top of the tab shown, in the accent
							// while its column has the keyboard.
							line := p.faint
							if focused {
								line = p.accent
							}
							l := ui.Box(c.Key("line")).Absolute().Top(0).Left(0).Right(0).Height(2).Background(line)
							if focused && p.gilt.A != 0 {
								l.Draw(func(pt *ui.Painter, r ui.Rect) { drawGilt(pt, r, p) })
							}
						}
						icon, tint := iconTerminal, p.muted
						switch {
						case t.diff != nil && t.diff.commit != nil && t.diff.commit.pr != nil:
							icon, tint = iconPullRequest, p.accent
						case t.diff != nil && t.diff.commit != nil:
							icon, tint = iconCommit, p.accent
						case t.diff != nil:
							icon, tint = iconDiff, p.changed
						case t.term == nil:
							icon, tint = fileIcon(p, t.name())
						}
						if t.term != nil {
							a.termIcon(c, t.term, 14, tint, under)
						} else {
							ui.Icon(c, icon).FontSize(14).TextColor(tint)
						}
						name := ui.Text(c, t.name()).FontSize(12.5).SingleLine().MaxWidth(220)
						if t.transient {
							// A preview, which the next file chosen replaces.
							name.Italic()
						}
						switch {
						case t.ed != nil && t.ed.diskGone, t.view != nil && t.view.diskGone:
							name.TextColor(p.dangerFg).Strikethrough()
						case active:
							name.FontWeight(550).TextColor(p.text)
						default:
							name.TextColor(p.muted)
						}
						// The dot of unsaved changes turns into the cross under the
						// pointer.
						x := ui.ButtonBase(c).Size(18, 18).Radius(4).Justify(ui.Center).AlignItems(ui.Center).Label(t.closeLabel())
						if x.Hovered() {
							x.Background(p.hover)
						}
						x.Transition(hoverFade)
						x.Children(func() {
							switch {
							case t.ed != nil && t.ed.dirty() && !x.Hovered():
								ui.Box(c.Key("dot")).Size(8, 8).Radius(4).Background(p.text.Alpha(0.7)).Transition(dotPop)
							case active || hovered || x.Hovered():
								ui.Icon(c.Key("x"), iconX).FontSize(13).TextColor(p.muted).Transition(crossFade)
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
			if col.locked {
				if a.iconButton(c, iconLock, locales.UnlockColumn(), true).Clicked() {
					a.post(func() { a.toggleColumnLock(col) })
				}
			}
			if a.iconButton(c, iconColumns, a.withKey(locales.CmdViewSplitRight(), "view.splitRight"), false).Clicked() {
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

// middleButton is the middle mouse button, as InputEvent.Button names it:
// 0 the primary button and 1 the secondary.
const middleButton = 2

// tabMiddle takes the input of the middle button on a tab, which closes
// the tab rather than shows it, as a browser's tabs do. The press is
// taken, so that the release comes back here wherever in the tab it is,
// and the tab it is on is remembered while it is held, so that the frames
// between do not show it. Let go of off the tab, box as the last frame
// laid it out, it closes nothing, as in a browser.
func (a *app) tabMiddle(t *colTab, ev ui.InputEvent, box ui.Rect) bool {
	if ev.Button != middleButton {
		// A press of another button ends one of the middle whose release
		// never came, which would keep the tab from showing.
		if ev.Kind == ui.InputPointerDown {
			a.middle = nil
		}
		return false
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		a.middle = t
		return true
	case ui.InputPointerUp:
		on := box.W == 0 || ev.X >= 0 && ev.Y >= 0 && ev.X < box.W && ev.Y < box.H
		if a.middle == t && on {
			a.post(func() { a.closeTab(t, nil) })
		}
		a.middle = nil
		return true
	}
	return false
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
	if m.Item(locales.TabClose()).Chosen() {
		*closing = t
	}
	if m.Item(locales.TabCloseOthers()).Disabled(len(col.tabs) < 2).Chosen() {
		a.post(func() { a.closeOthers(col, t) })
	}
	if m.Item(locales.TabCloseAll()).Chosen() {
		a.post(func() { a.closeTabs(slices.Clone(col.tabs), nil) })
	}
	m.Separator()
	a.columnLockMenu(m, col)
	if m.Item(locales.CmdViewSplitRight()).Chosen() {
		show(a.splitRight)
	}
	at := slices.Index(a.cols, col)
	alone := len(col.tabs) < 2
	if m.Item(locales.MoveToNextColumn()).Disabled(alone && at == len(a.cols)-1).Chosen() {
		show(func() { a.moveTabBy(1) })
	}
	if m.Item(locales.MoveToPrevColumn()).Disabled(alone && at == 0).Chosen() {
		show(func() { a.moveTabBy(-1) })
	}
	if t.term != nil {
		if m.Item(locales.TermMoveLockedRight()).Chosen() {
			a.post(func() { a.terminalToLockedRight(t.term) })
		}
		if m.Item(locales.MoveToPanel()).Chosen() {
			a.post(func() { a.terminalToPanel(t.term) })
		}
		a.hcMenu(m, t.term)
		return
	}
	m.Separator()
	if d := t.ed; d != nil && canPreview(d.document) {
		label := locales.ShowPreview()
		if d.preview != nil {
			label = locales.ShowSource()
		}
		if m.Item(label).Chosen() {
			show(func() { a.setPreview(d, d.preview == nil); a.focusColumn() })
		}
		if m.Item(locales.CmdViewPreviewToSide()).Chosen() {
			show(a.previewToSide)
		}
	}
	path := t.path()
	if t.diff != nil {
		if m.Item(locales.OpenFile()).Chosen() {
			show(a.openChanges)
		}
	} else if t.ed != nil && path != "" && a.repositoryForPath(path).git.repo != nil && m.Item(locales.CmdGitOpenChanges()).Chosen() {
		show(a.openChanges)
	}
	if m.Item(locales.ScmRevealInExplorer()).Disabled(path == "" || a.root == "").Chosen() {
		a.post(func() { a.showExplorer(); a.revealInTree(path) })
	}
	if m.Item(revealLabel()).Disabled(path == "" || remote.Is(path)).Chosen() {
		a.host.reveal(path)
	}
	if m.Item(locales.CopyPath()).Disabled(path == "").Chosen() {
		a.copyPath(path, false)
	}
	if m.Item(locales.CopyRelativePath()).Disabled(path == "").Chosen() {
		a.copyPath(path, true)
	}
	if path != "" && a.repositoryForPath(path).git.repo != nil {
		m.Separator()
		if m.Item(locales.CmdGitOpenFileOnRemote()).Chosen() {
			a.post(func() { a.fileOnRemote(path, t.ed, false) })
		}
		if m.Item(locales.CmdGitCopyPermalink()).Chosen() {
			a.post(func() { a.fileOnRemote(path, t.ed, true) })
		}
	}
}

func (a *app) columnLockMenu(m *ui.Menu, col *column) {
	label := locales.LockColumn()
	if col.locked {
		label = locales.UnlockColumn()
	}
	if m.Item(label).Chosen() {
		a.post(func() { a.toggleColumnLock(col) })
	}
}
