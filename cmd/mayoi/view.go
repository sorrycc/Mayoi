package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"mayoi/internal/remotefs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/highlight"
)

const (
	titleBarH  = 38
	tabStripH  = 35
	crumbsH    = 24
	statusBarH = 24
	panelHeadH = 34
)

// view builds the window: a title bar, the explorer beside the editor
// and the terminal panel below it, and a status bar.
func (a *app) view(c *ui.Context) {
	a.mu.Lock()
	if !a.hasServices {
		a.services, a.hasServices = c.Services(), true
	}
	a.mu.Unlock()
	if !a.started {
		// Before what was posted meanwhile, as opening the file the app was
		// started with: it opens in the columns of the last session.
		a.started = true
		a.start()
	}
	a.drain()
	a.pumpChain()
	// The key the keymap took is let go of where its release does not
	// come: in a place with no text, or another window.
	if a.held != nil && (a.blurred || a.keyFocus != placeEditor && a.keyFocus != placeInput) {
		a.held = nil
	}
	// The places that take the keys say so as they are built.
	a.keyFocus = placeNone
	a.pal = a.applyTheme(c)
	a.themeTerminals()
	a.fontTerminals()
	p := a.pal
	if p := a.settings.Prefs; !a.font.is(p) {
		a.font = newCodeFont(p)
	}
	if now := c.Now(); now != a.frameAt {
		a.frameAt = now
		a.frame++
		a.dragging, a.dragNow = a.dragNow, false
	}
	a.shortcuts(c)
	// Which pane has the keyboard focus, as the frame before left it, and
	// which column: its editor is the one typing is of.
	for i, col := range a.cols {
		switch t := col.tab(); {
		case t == nil:
		case t.ed != nil && t.ed.vscroll.FocusWithin(c):
			a.focus, a.col = paneEditor, i
			t.ed.typist = t.ed
		case t.term != nil && t.term.view.FocusWithin(c):
			a.focus, a.col = paneEditor, i
		case t.view != nil && t.view.pane.FocusWithin(c):
			a.focus, a.col = paneEditor, i
		case t.diff != nil && t.diff.pane.FocusWithin(c):
			a.focus, a.col = paneEditor, i
		case t.ed != nil && t.ed.preview != nil && t.ed.preview.pane.FocusWithin(c):
			a.focus, a.col = paneEditor, i
		}
	}
	a.autoSaveTick()
	switch {
	case a.panelTerm != nil && a.panelTerm.view.FocusWithin(c):
		a.focus = paneTerminal
	case a.files.List.FocusWithin(c):
		a.focus = paneFiles
	}

	ui.Column(c).Fill().Background(p.chrome).Children(func() {
		a.titleBar(c)
		// The panes keep their places in the tree, by keys, as the sidebar
		// and the panel come and go, so that the editors keep their state.
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			s := a.settings
			sidebar := func() {
				if !s.SidebarOpen {
					return
				}
				// Its splitter is along the edge facing the editors.
				at, dir := edgeRight, float32(1)
				box := ui.Column(c.Key("sidebar")).Width(s.SidebarWidth).Shrink(0).BorderColor(p.border)
				if s.SidebarRight {
					at, dir = edgeLeft, -1
					box.BorderWidth(0, 0, 0, 1)
				} else {
					box.BorderWidth(0, 1, 0, 0)
				}
				box.Children(func() {
					a.sidebar(c)
					a.splitter(c, at, func(d float32) {
						w, _ := c.Size()
						s.SidebarWidth = clamp(s.SidebarWidth+dir*d, 160, max(w-320, 160))
					})
				})
			}
			if !s.SidebarRight {
				sidebar()
			}
			ui.Column(c.Key("main")).Grow(1).MinWidth(0).Children(func() { a.main(c) })
			if s.SidebarRight {
				sidebar()
			}
		})
		a.statusBar(c)
	})
	a.quickOpenView(c)
	a.branchPickerView(c)
	a.recentPickerView(c)
	a.remotePickerView(c)
	a.promptView(c)
	a.confirmView(c)
	if !a.dialogBuilt {
		a.dialog = ""
	}
	a.dialogBuilt = false
	a.windowKeys(c)
	for _, msg := range a.errors {
		c.Toast(msg)
	}
	a.errors = a.errors[:0]
	for _, n := range a.notes {
		if n.action != nil {
			c.ToastAction(n.message, n.label, n.action)
		} else {
			c.Toast(n.message)
		}
	}
	a.notes = a.notes[:0]
	a.tick()
}

// main is the columns of editors and, below them, the panel.
func (a *app) main(c *ui.Context) {
	p, s := a.pal, a.settings
	panel := a.window.PanelOpen
	editor := ui.Column(c.Key("editor")).Grow(1).MinHeight(0).Background(p.editor).Clip()
	if panel && a.panelMax {
		// Out of sight, but built, so that the editors keep their state.
		editor.Grow(0).Height(0).Invisible()
	}
	editor.Children(func() { a.columns(c) })
	if !panel {
		return
	}
	_, h := c.Size()
	room := h - titleBarH - statusBarH
	height := clamp(s.PanelHeight, 80, max(room-80, 80))
	box := ui.Column(c.Key("panel")).Shrink(0).BorderWidth(1, 0, 0, 0).BorderColor(p.border)
	if a.panelMax {
		box.Grow(1)
	} else {
		box.Height(height)
	}
	box.Children(func() {
		a.panel(c)
		if !a.panelMax {
			a.splitter(c, edgeTop, func(d float32) {
				s.PanelHeight = clamp(height-d, 80, max(room-80, 80))
			})
		}
	})
}

// edge is an edge of a pane, which its splitter is along.
type edge int

const (
	edgeRight edge = iota // the sidebar's, on the left
	edgeTop               // the panel's
	edgeLeft              // a column's, after the first, or the sidebar's on the right
)

// splitter is the handle along an edge of a pane that resizes it. drag
// gets how far the pointer moved, across the edge.
func (a *app) splitter(c *ui.Context, at edge, drag func(d float32)) {
	h := ui.Box(c).Absolute()
	switch at {
	case edgeTop:
		h.Top(0).Left(0).Right(0).Height(5).Cursor(ui.CursorResizeNS)
	case edgeLeft:
		h.Top(0).Bottom(0).Left(0).Width(5).Cursor(ui.CursorResizeEW)
	default:
		h.Top(0).Bottom(0).Right(0).Width(5).Cursor(ui.CursorResizeEW)
	}
	lit := h.Hovered() || h.Dragging()
	h.Children(func() {
		if !lit {
			return
		}
		line := ui.Box(c).Absolute().Background(a.pal.accentDim)
		switch at {
		case edgeTop:
			line.Top(0).Left(0).Right(0).Height(2)
		case edgeLeft:
			line.Top(0).Bottom(0).Left(0).Width(2)
		default:
			line.Top(0).Bottom(0).Right(0).Width(2)
		}
	})
	dx, dy, held := h.Dragged()
	if held && (dx != 0 || dy != 0) {
		if at == edgeTop {
			drag(dy)
		} else {
			drag(dx)
		}
		a.settings.Changed()
	}
}

func (a *app) titleBar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, titleBarH)).Padding(0, bar.Right+8, 0, bar.Left+10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).DragWindow().Children(func() {
		// The sidebar's button is on its side.
		right := a.settings.SidebarRight
		sidebarButton := func() {
			icon := iconSidebar
			if right {
				icon = iconSidebarRight
			}
			if a.iconButton(c, icon, "Toggle Sidebar (⌘B)", a.settings.SidebarOpen).Clicked() {
				a.toggleSidebar()
			}
		}
		ui.Row(c).Width(120).Gap(2).Children(func() {
			if !right {
				sidebarButton()
			}
		})
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			a.commandCenter(c)
		})
		ui.Row(c).Width(120).Justify(ui.End).Gap(2).Children(func() {
			if a.iconButton(c, iconPanel, "Toggle Terminal (⌃`)", a.window.PanelOpen).Clicked() {
				a.togglePanel()
			}
			if right {
				sidebarButton()
			}
		})
	})
}

// commandCenter is the box in the middle of the title bar: it names the
// document and the folder, and opens quick open.
func (a *app) commandCenter(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("command-center")).Height(24).Width(420).MaxWidthPercent(100).Shrink(1).Padding(0, 10).Gap(8).
		Radius(6).Justify(ui.Center).AlignItems(ui.Center).Border(1, p.border).Label("Quick Open").Tooltip("Quick Open (⌘P)")
	b.Background(p.hover)
	hovered := b.Hovered()
	if hovered {
		b.Background(p.hover.Alpha(2)).Border(1, mix(p.border, p.muted, 0.3))
	}
	b.Children(func() {
		col := p.faint
		if hovered {
			col = p.muted
		}
		ui.Icon(c, iconSearch).FontSize(12).TextColor(col)
		name, folder := "", ""
		if t := a.activeTab(); t != nil {
			name = t.name()
			if t.ed != nil && t.ed.dirty() {
				name = "● " + name
			}
		}
		if a.root != "" {
			folder = a.workspaceName()
		}
		switch {
		case name != "" && folder != "":
			ui.Text(c, name).FontSize(12).FontWeight(500).TextColor(p.text).SingleLine().Shrink(1)
			ui.Text(c, folder).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
		case name != "" || folder != "":
			ui.Text(c, name+folder).FontSize(12).FontWeight(500).TextColor(p.muted).SingleLine().Shrink(1)
		default:
			ui.Text(c, "Mayoi").FontSize(12).FontWeight(500).TextColor(p.muted).SingleLine()
		}
	})
	if b.Clicked() {
		a.openQuick("")
	}
}

// iconButton is a small square button showing an icon, its tooltip the
// label.
func (a *app) iconButton(c *ui.Context, icon *ui.SVG, label string, on bool) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c).Size(26, 24).Radius(5).Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	switch {
	case b.Pressed():
		b.Background(p.hover)
		col = p.text
	case b.Hovered():
		b.Background(p.hover)
		col = p.text
	}
	if on {
		col = p.text
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(15).TextColor(col) })
	return b
}

// minColumnW is the least width a column is dragged to.
const minColumnW = 160

// columns is the editor area: its columns side by side, which share its
// width by their weights, with a handle between two that moves the width
// from one to the other.
func (a *app) columns(c *ui.Context) {
	p := a.pal
	width := a.colsView.Bounds(c).W
	var total float32
	for _, col := range a.cols {
		total += col.weight
	}
	// The columns as the frame began: one an action of the frame takes
	// away, from its tab strip as it draws, is let be till the next.
	cols := slices.Clone(a.cols)
	ui.Row(c.Key("columns")).Bind(&a.colsView).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		for i, col := range cols {
			if !slices.Contains(a.cols, col) {
				continue
			}
			box := ui.Column(c.Key(col.id)).Basis(0).Grow(col.weight).MinWidth(0)
			if i > 0 {
				box.BorderWidth(0, 0, 0, 1).BorderColor(p.border)
			}
			box.Children(func() {
				a.columnView(c, col)
				if i == 0 || width <= 0 {
					return
				}
				prev := cols[i-1]
				a.splitter(c, edgeLeft, func(d float32) {
					least := minColumnW / width * total
					by := d / width * total
					if lo, hi := least-prev.weight, col.weight-least; lo <= hi {
						by = clamp(by, lo, hi)
						prev.weight, col.weight = prev.weight+by, col.weight-by
						a.rememberTabs()
					}
				})
			})
		}
	})
}

// columnView is a column: its tab strip, the breadcrumbs, and the editor
// or the terminal of the tab shown, or the welcome page.
func (a *app) columnView(c *ui.Context, col *column) {
	if len(col.tabs) == 0 {
		body := ui.Column(c).Bind(&col.body).Grow(1).MinHeight(0)
		over := a.takeFiles(body, col)
		body.Children(func() {
			a.welcome(c)
			a.dropZones(c, col, over)
		})
		return
	}
	a.tabStrip(c, col)
	shown := col.tab()
	if shown != nil && shown.term == nil {
		a.breadcrumbs(c, shown)
	}
	// Every tab keeps its editor, so that its selection and its place stay
	// as tabs change: those not shown are built invisible, out of the way.
	// The key keeps them as the breadcrumbs above come and go.
	body := ui.Box(c.Key("body")).Bind(&col.body).Grow(1)
	over := a.takeFiles(body, col)
	body.Children(func() {
		for _, t := range col.tabs {
			box := ui.Column(c.Key(t.id)).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			if t != shown {
				box.Invisible()
			}
			box.Children(func() {
				switch {
				case t.term != nil:
					// A terminal keeps its state itself.
					if t == shown {
						a.terminalView(c, t.term)
					}
				case t.view != nil:
					if t == shown {
						a.viewerView(c, t.view, t.name())
					}
				case t.diff != nil:
					if t == shown {
						a.diffViewUI(c, t.diff)
					}
				case t.ed != nil && (t.ed.remoteLoading || t.ed.remoteLoadError != ""):
					if t == shown {
						ui.Column(c).Padding(24).Gap(12).Children(func() {
							if t.ed.remoteLoading {
								ui.Text(c, "Loading remote file…").TextColor(a.pal.muted)
							} else {
								ui.Text(c, t.ed.remoteLoadError).TextColor(a.pal.danger)
								if ui.Button(c, "Retry").Clicked() {
									a.loadRemoteDocument(t.ed.document, true)
								}
							}
						})
					}
				case t == shown && t.ed.preview != nil:
					// The text keeps its place, out of sight, under the picture.
					ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Invisible().Children(func() {
						a.hiddenEditor(c, t.ed)
					})
					a.viewerView(c, t.ed.preview, t.name())
				case t == shown:
					a.editorView(c, t.ed)
					a.hunkPeekView(c, t.ed)
					if col == a.column() {
						a.findView(c, t.ed)
					}
				default:
					a.hiddenEditor(c, t.ed)
				}
			})
		}
		a.dropZones(c, col, over)
	})
}

// hiddenEditor keeps the state of an editor's text area while another
// tab is shown. The area scrolls to its caret when its text changes, as
// an edit made in another column changes it: it stays where it was.
func (a *app) hiddenEditor(c *ui.Context, d *editor) {
	if !d.hidden {
		d.hidden, d.hiddenY = true, d.vscroll.Y
	}
	d.vscroll.Y = d.hiddenY
	ui.ScrollHorizontal(c).TrackScroll(&d.hscroll).Fill().Children(func() {
		// Laid out as the editor shows it, so that its place stays.
		area := ui.TextAreaBufferBase(c, d.buf).Font(a.font.family).FontSize(a.font.size).FontWeight(a.font.weight).
			FontFeatures(a.font.featureList()...).FixedLineHeight(a.font.lineH).
			Fill().TrackScroll(&d.vscroll)
		if a.wraps(d) {
			// The rows the editor measured stay measured only at its width
			// and in its style.
			gutterW := a.font.gutterWidth(d.buf.Snapshot().LineCount())
			area.Padding(editorPadY, textPadR, editorPadY, gutterW+gutterGap).TextColor(a.pal.text).TrackLines(&d.lines)
			if w := d.hscroll.Bounds(c).W; w > 0 {
				area.Width(w)
			}
		} else {
			area.NoWrap()
		}
	})
}

// dragItem is what is dragged to a column: one of its tabs, or of
// another's, a terminal of the panel, or a file of the sidebar.
type dragItem struct {
	tab  *colTab
	term *session
	file *fileDrag
}

// dropInColumn puts what was dragged at place at of a column.
func (a *app) dropInColumn(it dragItem, col *column, at int) {
	if slices.Index(a.cols, col) < 0 {
		return
	}
	switch {
	case it.file != nil:
		a.dropFile(it.file, col, at)
	case it.term != nil:
		a.terminalToColumn(it.term, col, at)
	case it.tab != nil:
		a.moveTab(it.tab, col, at)
	}
}

// dropBeside puts what was dragged in a column of its own, before a
// column or after it.
func (a *app) dropBeside(it dragItem, col *column, after bool) {
	if slices.Index(a.cols, col) < 0 {
		return
	}
	if it.file != nil {
		a.dropFileBeside(it.file, col, after)
		return
	}
	if from, _ := a.locate(it.tab); from == col && len(col.tabs) == 1 {
		return // it is the column
	}
	a.dropInColumn(it, a.splitColumn(col, after), 0)
}

// takeFiles makes a column's body take files dragged from another app,
// which open in it, and reports whether they are over it.
func (a *app) takeFiles(body ui.Element, col *column) bool {
	if paths := body.DroppedFiles(); paths != nil {
		paths = slices.Clone(paths)
		a.post(func() { a.dropOnColumn(paths, col) })
	}
	return body.FileDragOver()
}

// dropZones are where a tab or files dragged over a column are dropped:
// on a side, for a column of their own there, or between them, in this
// one. They are there only while something is dragged over the editor;
// over says files are over the column's body.
func (a *app) dropZones(c *ui.Context, col *column, over bool) {
	files := over || col.filesOver
	if !a.dragging && !files {
		return
	}
	w := col.body.Bounds(c).W
	side := min(w/4, 140)
	zone := func(key string, left, width, markLeft, markW float32, drop func(it dragItem), dropFiles func([]string)) {
		z := ui.Box(c.Key(key)).Absolute().Top(0).Bottom(0).Left(left).Width(width)
		_, ok := ui.DragOver[dragItem](z)
		if files && z.FileDragOver() {
			ok, over = true, true
		}
		if ok {
			ui.Box(c.Key(key+"-mark")).Absolute().Top(0).Bottom(0).Left(markLeft).Width(markW).PassThrough().
				Background(a.pal.accent.Alpha(0.14)).Border(1, a.pal.accentDim)
		}
		if it, ok := ui.Drop[dragItem](z); ok {
			a.post(func() { drop(it) })
		}
		if !files {
			return
		}
		if paths := z.DroppedFiles(); paths != nil {
			paths = slices.Clone(paths)
			a.post(func() { dropFiles(paths) })
		}
	}
	zone("drop-before", 0, side, 0, w/2, func(it dragItem) { a.dropBeside(it, col, false) },
		func(paths []string) { a.dropFilesBeside(paths, col, false) })
	zone("drop-in", side, w-2*side, 0, w, func(it dragItem) {
		if it.file != nil {
			// As files from another app: after the tab shown.
			a.dropFileOnColumn(it.file, col)
			return
		}
		a.dropInColumn(it, col, len(col.tabs))
	},
		func(paths []string) { a.dropOnColumn(paths, col) })
	zone("drop-after", w-side, side, w/2, w/2, func(it dragItem) { a.dropBeside(it, col, true) },
		func(paths []string) { a.dropFilesBeside(paths, col, true) })
	if col.filesOver != over {
		col.filesOver = over
		if !over {
			// One more frame, without the zones.
			a.invalidate()
		}
	}
}

// dropMark tints a pane that takes what is dragged over it.
func (a *app) dropMark(c *ui.Context) {
	ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Right(0).PassThrough().
		Background(a.pal.accent.Alpha(0.14)).Border(1, a.pal.accentDim)
}

// breadcrumbs name the file a tab shows and, in an editor, the blocks
// its caret is in. An SVG has a button there to show it as a picture or
// as text.
func (a *app) breadcrumbs(c *ui.Context, t *colTab) {
	p := a.pal
	d := t.ed
	var parts []string
	if path := t.path(); path != "" {
		rel := path
		if a.root != "" && strings.HasPrefix(path, a.root+string(filepath.Separator)) {
			rel = path[len(a.root)+1:]
		}
		parts = strings.Split(filepath.ToSlash(rel), "/")
	} else if t.diff != nil && t.diff.commit != nil {
		parts = []string{"Commit " + git.ShortHash(t.diff.commit.hash), t.name()}
	} else {
		parts = []string{t.name()}
	}
	// The blocks the caret is in, after the file: the innermost two.
	var snap ui.TextSnapshot
	var scope []int
	if d != nil && d.preview == nil && isCode(d.document) {
		snap = d.buf.Snapshot()
		scope = d.scopesOf(snap, snap.LineAt(min(d.sel[1], snap.Len())))
		scope = scope[max(0, len(scope)-2):]
	}
	ui.Row(c).Height(crumbsH).Padding(0, 14).Gap(4).AlignItems(ui.Center).Background(p.editor).Clip().Children(func() {
		for i, part := range parts {
			last := i == len(parts)-1
			if last {
				icon, col := fileIcon(p, part)
				switch {
				case t.diff != nil && t.diff.commit != nil:
					icon, col = iconCommit, p.accent
				case t.diff != nil:
					icon, col = iconDiff, p.changed
				}
				ui.Icon(c, icon).FontSize(13).TextColor(col)
			}
			t := ui.Text(c, part).FontSize(12).SingleLine().TextColor(p.muted).Shrink(0)
			if last {
				// A long name, as a commit's subject, ends in an ellipsis.
				t.TextColor(p.text).Shrink(1).MinWidth(24)
			}
			if !last || len(scope) > 0 {
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint)
			}
		}
		for i, line := range scope {
			label := scopeLabel(snap, line)
			b := ui.ButtonBase(c.Key(fmt.Sprintf("scope:%d", i))).Height(crumbsH-6).Padding(0, 5).Radius(4).
				AlignItems(ui.Center).Shrink(1).MinWidth(0).Label("Go to " + label)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(12).Font(a.font.family).SingleLine().TextColor(p.muted)
			})
			if b.Clicked() {
				a.queueEdit(d, func(d *editor) { a.jumpToScope(d, line) })
			}
			if i < len(scope)-1 {
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint)
			}
		}
		if d != nil && isSVG(d.document) {
			ui.Spacer(c)
			a.previewButton(c, t, d)
		}
		if t.diff != nil {
			ui.Spacer(c)
			a.diffButtons(c, t)
		}
	})
}

// previewButton shows an SVG as a picture, or as text again.
func (a *app) previewButton(c *ui.Context, t *colTab, d *editor) {
	p := a.pal
	label, icon := "Show Preview (⇧⌘V)", iconImage
	if d.preview != nil {
		label, icon = "Show Source (⇧⌘V)", iconFileCode
	}
	b := ui.ButtonBase(c.Key("preview")).Height(crumbsH-4).Padding(0, 6).Radius(4).Shrink(0).
		AlignItems(ui.Center).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(p.muted) })
	if b.Clicked() {
		a.post(func() {
			a.setPreview(d, d.preview == nil)
			if col, i := a.locate(t); col != nil {
				a.activate(col, i, true)
			}
		})
	}
}

func (a *app) statusBar(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(statusBarH).Padding(0, 6).Gap(2).AlignItems(ui.Center).Background(p.status).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		text := func(s string) ui.Element {
			return ui.Text(c, s).FontSize(11.5).TextColor(p.statusFg).SingleLine()
		}
		// item is a part of the bar, a button when it does something.
		item := func(key, label, tip string, icon *ui.SVG, fn func()) {
			if fn == nil {
				ui.Row(c.Key("status:"+key)).Height(statusBarH).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
					if icon != nil {
						ui.Icon(c, icon).FontSize(12).TextColor(p.statusFg)
					}
					text(label)
				})
				return
			}
			b := ui.ButtonBase(c.Key("status:"+key)).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
				AlignItems(ui.Center).Label(tip).Tooltip(tip)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Children(func() {
				if icon != nil {
					ui.Icon(c, icon).FontSize(12).TextColor(p.statusFg)
				}
				text(label)
			})
			if b.Clicked() {
				fn()
			}
		}
		// menuItem is a part of the bar that opens a menu.
		menuItem := func(key, label, tip string, build func(m *ui.Menu)) {
			b := ui.ButtonBase(c.Key("status:"+key)).Height(statusBarH-6).Padding(0, 6).Radius(4).
				AlignItems(ui.Center).Label(tip).Tooltip(tip)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Children(func() { text(label) })
			b.Menu(build)
		}
		if a.settings.Theme == themeClaude {
			a.statusClawd(c)
		}
		if b := a.branch(); b != "" {
			bb := ui.ButtonBase(c.Key("status:branch")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
				AlignItems(ui.Center).Label("Branch " + b).Tooltip("Branch " + b + ": switch, fetch, pull or push")
			if bb.Hovered() {
				bb.Background(p.hover)
			}
			bb.Children(func() {
				ui.Icon(c, iconBranch).FontSize(12).TextColor(p.statusFg)
				text(a.branchLabel())
			})
			bb.Menu(a.branchMenu)
		}
		if busy := a.git.busy; busy != "" {
			ui.Row(c.Key("status:busy")).Height(statusBarH).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
				// In Claude, Clawd hops for it instead.
				if a.settings.Theme != themeClaude {
					ui.Spinner(c).Size(11, 11)
				}
				text(busy)
			})
		}
		if len(a.chord) > 0 {
			item("chord", "("+strings.Join(chordCaps(a.chord), " ")+") was pressed. Waiting for the next key…", "", iconKeyboard, nil)
		}
		if a.root != "" {
			if t, _, ok := remotefs.Split(a.root); ok {
				label := "SSH: " + t.Host
				if !remotefs.Connected(a.root) {
					label += " · Disconnected"
				}
				item("folder", label, remotefs.Display(a.root), iconTerminal, a.reconnectRemote)
			} else {
				item("folder", a.workspaceName(), "Reveal "+shortenHome(a.root)+" in Finder", iconFolder, func() { a.host.reveal(a.root) })
			}
		}
		ui.Spacer(c)
		if v := a.activeViewer(); v != nil {
			a.viewerStatus(c, v, item, text)
			return
		}
		if t := a.activeTab(); t != nil && t.diff != nil {
			if s := t.diff.diffStatus(); s != "" {
				item("diff", s, "", iconDiff, nil)
			}
			return
		}
		if t := a.activeTab(); t != nil && t.term != nil {
			// A terminal: where its shell is, and its size in cells.
			term := t.term.term
			if dir := term.Dir(); dir != "" {
				item("cwd", shortenHome(dir), "", iconTerminal, nil)
			}
			if cols, rows := term.Size(); cols > 0 {
				item("size", fmt.Sprintf("%d × %d", cols, rows), "", nil, nil)
			}
			return
		}
		d := a.activeEditor()
		if d == nil {
			return
		}
		if d.diskGone {
			ui.Row(c).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconTrash).FontSize(12).TextColor(p.danger)
				text("Deleted on disk").TextColor(p.danger)
			})
		}
		snap := d.buf.Snapshot()
		caret := min(d.caret, snap.Len())
		if a.settings.Vim && d.vim.mode.visual() {
			caret = vimCursor(d, snap)
		}
		line := snap.LineAt(caret)
		s, _ := snap.LineRange(line)
		pos := fmt.Sprintf("Ln %d, Col %d", line+1, columns(snap.Slice(s, caret))+1)
		if n := abs(d.sel[1] - d.sel[0]); n > 0 {
			pos += fmt.Sprintf(" (%d selected)", n)
		}
		if a.settings.Vim {
			item("vim", d.vim.label(), "", nil, nil)
		}
		item("pos", pos, "Go to Line (⌃G)", nil, func() { a.openQuick(":") })
		item("lines", plural(snap.LineCount(), "line"), "", nil, nil)
		menuItem("indent", d.indent.String(), "Indentation", func(m *ui.Menu) {
			if m.Item("Indent Using Tabs").Checked(d.indent.tabs).Chosen() {
				d.indent.tabs = true
			}
			for _, n := range []int{2, 4, 8} {
				if m.Item(fmt.Sprintf("Indent Using %d Spaces", n)).Checked(!d.indent.tabs && d.indent.size == n).Chosen() {
					d.indent = indentStyle{size: n}
				}
			}
			m.Separator()
			if m.Item("Convert Indentation to Tabs").Chosen() {
				a.editActive(func(d *editor) { reindent(d, indentStyle{tabs: true, size: tabWidth}) })()
			}
			if m.Item("Convert Indentation to Spaces").Chosen() {
				size := d.indent.size
				if d.indent.tabs {
					size = tabWidth
				}
				a.editActive(func(d *editor) { reindent(d, indentStyle{size: size}) })()
			}
		})
		item("enc", "UTF-8", "", nil, nil)
		eol := "LF"
		if d.crlf {
			eol = "CRLF"
		}
		menuItem("eol", eol, "Line Endings", func(m *ui.Menu) {
			for _, crlf := range []bool{false, true} {
				label := "LF"
				if crlf {
					label = "CRLF"
				}
				if m.Item(label).Checked(d.crlf == crlf).Chosen() {
					d.crlf = crlf
				}
			}
		})
		item("lang", highlight.DisplayName(d.hl.Lexer()), "", nil, nil)
	})
}

// plural counts n things, as "1 line" or "2 lines".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// iconArt is the app icon's art, which tools/icongen places on the
// macOS icon grid.
//
//go:embed icon-art.png
var iconArt []byte

// iconFrame is the width of the art's own pixel tile border, which the
// icon crops, in pixels of the 1254 px art; as in tools/icongen.
const iconFrame = 52

// logo is Mayoi's mark: the app icon itself, its border cropped as on
// the Dock, decoded the first time the welcome page shows.
var logo = sync.OnceValue(func() *ui.Bitmap {
	art, err := png.Decode(bytes.NewReader(iconArt))
	if err != nil {
		panic(err)
	}
	return ui.NewBitmap(art.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(art.Bounds().Inset(iconFrame)))
})

// welcome shows while no file is open: what to start with, and the
// folders opened last.
func (a *app) welcome(c *ui.Context) {
	p := a.pal
	// Centered in the view, and scrolled when the window is too short.
	view := a.welcomeView.Bounds(c)
	page := ui.Scroll(c.Key("welcome")).Bind(&a.welcomeView).Fill().Background(p.editor)
	if a.settings.Theme == themeMatrix {
		still := a.blurred || c.Preferences().ReduceMotion
		page.Draw(func(pt *ui.Painter, r ui.Rect) { a.drawRain(pt, r, still) })
	}
	page.Children(func() {
		ui.Column(c).FillWidth().MinHeight(view.H).Center().Padding(40, 24, 64).Children(func() {
			content := ui.Column(c).Width(380).MaxWidthPercent(100).Gap(32)
			if a.settings.Theme == themeMatrix {
				// Over the rain, which would run through the words.
				content.Width(380+2*24).Padding(24).Radius(12).Background(p.editor.Alpha(0.88)).Border(1, p.border)
			}
			content.Children(func() {
				ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
					if a.settings.Theme == themeClaude {
						// Clawd in the logo's place.
						const cell = 4
						w, h := clawdSize(cell)
						still := a.clawdStill(c)
						bg := p.editor
						ui.Row(c.Key("welcome:clawd")).Size(w, h).Tooltip(a.clawdTip()).Draw(func(pt *ui.Painter, r ui.Rect) {
							a.drawClawd(pt, r, cell, bg, still)
						})
					} else {
						// Clipped to the icon's squircle: 185 of its 824 DIPs.
						ui.Column(c).Size(52, 52).Radius(52.0 * 185 / 824).Clip().Children(func() {
							ui.Image(c, logo()).Size(52, 52)
						})
					}
					ui.Column(c).Gap(3).Children(func() {
						ui.Text(c, "Mayoi").FontSize(26).FontWeight(600).LetterSpacing(-0.3).TextColor(p.text)
						ui.Text(c, "迷い · a small, quick code editor").FontSize(13).TextColor(p.muted)
					})
				})
				a.welcomeSection(c, "Start", func() {
					if a.root == "" {
						a.welcomeAction(c, iconFolderOpen, "Open Folder…", "⌘O", a.chooseFolder)
						a.welcomeAction(c, iconTerminal, "Connect via SSH…", "", a.chooseRemote)
					} else {
						if remotefs.IsRemote(a.root) {
							a.welcomeAction(c, iconFolderOpen, "Open Remote Folder…", "⌘O", a.chooseRemoteFolder)
						}
						a.welcomeAction(c, iconSearch, "Quick Open…", "⌘P", func() { a.openQuick("") })
						a.welcomeAction(c, iconSearch, "Find in Files", "⇧⌘F", func() { a.showSearch(false) })
					}
					a.welcomeAction(c, iconChevronRight, "Command Palette…", "⇧⌘P", func() { a.openQuick(">") })
					a.welcomeAction(c, iconFilePlus, "New File", "⌘N", a.newFile)
					a.welcomeAction(c, iconTerminal, "Toggle Terminal", "⌃`", a.togglePanel)
					a.welcomeAction(c, iconSidebar, "Toggle Sidebar", "⌘B", a.toggleSidebar)
				})
				var recent []string
				if a.root == "" {
					recent = a.settings.RecentExcept("")
				}
				if len(recent) > 0 {
					a.welcomeSection(c, "Recent", func() {
						for _, dir := range recent[:min(6, len(recent))] {
							r := ui.ButtonBase(c.Key("recent:"+dir)).FillWidth().Padding(6, 10).Radius(6).Gap(10).
								Justify(ui.Start).AlignItems(ui.Center).Label("Open " + dir).Tooltip(shortenHome(dir))
							if r.Hovered() {
								r.Background(p.hover)
							}
							r.Children(func() {
								ui.Icon(c, iconFolder).FontSize(15).TextColor(p.accent)
								ui.Text(c, filepath.Base(dir)).FontSize(13).TextColor(p.text).SingleLine().Shrink(0)
								ui.Text(c, shortenHome(filepath.Dir(dir))).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
							})
							// With Cmd, in a window of its own.
							switch {
							case r.Focused() && c.Shortcut(ui.Cmd, ui.KeyEnter):
								a.host.newWindow(dir)
							case r.Clicked():
								if r.ClickModifiers()&ui.Cmd != 0 {
									a.host.newWindow(dir)
								} else {
									a.switchFolder(dir)
								}
							}
						}
					})
				}
			})
		})
	})
}

// welcomeSection is a titled group of the welcome page.
func (a *app) welcomeSection(c *ui.Context, title string, rows func()) {
	p := a.pal
	ui.Column(c).Gap(2).Children(func() {
		// As the sidebar titles its views.
		ui.Text(c, strings.ToUpper(title)).FontSize(11).FontWeight(600).LetterSpacing(0.7).TextColor(p.muted).Padding(0, 10, 6)
		rows()
	})
}

// welcomeAction is a row of the welcome page that runs a command: its
// icon, its name and its keys.
func (a *app) welcomeAction(c *ui.Context, icon *ui.SVG, label, keys string, fn func()) {
	p := a.pal
	r := ui.ButtonBase(c.Key("action:"+label)).FillWidth().Padding(6, 10).Radius(6).Gap(10).
		Justify(ui.Start).AlignItems(ui.Center).Label(label)
	hovered := r.Hovered()
	if hovered {
		r.Background(p.hover)
	}
	r.Children(func() {
		col := p.muted
		if hovered {
			col = p.accent
		}
		ui.Icon(c, icon).FontSize(15).TextColor(col)
		ui.Text(c, label).FontSize(13).TextColor(p.text).Grow(1).SingleLine()
		a.keycaps(c, keys)
	})
	if r.Clicked() {
		fn()
	}
}

// keycaps shows keys as the caps of a keyboard, one a symbol, except for
// a named key as "esc".
func (a *app) keycaps(c *ui.Context, keys string) {
	var caps []string
	if isASCII(keys) && len(keys) > 1 && strings.ToLower(keys) == keys && !strings.ContainsAny(keys, "`") {
		caps = []string{keys}
	} else {
		for _, k := range keys {
			caps = append(caps, string(k))
		}
	}
	a.caps(c, caps)
}

// caps shows keys as the caps of a keyboard.
func (a *app) caps(c *ui.Context, caps []string) {
	p := a.pal
	ui.Row(c).Gap(3).Shrink(0).Children(func() {
		for _, k := range caps {
			ui.Text(c, k).FontSize(11).TextColor(p.muted).TextAlign(ui.Center).
				MinWidth(18).Height(18).Padding(1, 4, 0).Radius(4).Background(p.hover).Border(1, p.border)
		}
	})
}

func (a *app) toggleSidebar() {
	a.settings.SidebarOpen = !a.settings.SidebarOpen
	a.settings.Changed()
}

// toggleSidebarRight moves the sidebar to the other side of the editors.
func (a *app) toggleSidebarRight() {
	a.settings.SidebarRight = !a.settings.SidebarRight
	a.settings.Changed()
}

// togglePanel shows or hides the terminal panel, starting a terminal the
// first time it shows.
func (a *app) togglePanel() {
	w := a.window
	if w.PanelOpen && a.focus != paneTerminal && a.panelTerm != nil {
		// Shown but not focused: focus it, as VS Code does.
		a.focusTerminal()
		return
	}
	w.PanelOpen = !w.PanelOpen
	if w.PanelOpen {
		if a.panelTerm == nil {
			a.newTerminal()
		}
		a.focusTerminal()
	} else {
		a.focusColumn()
	}
	a.settings.Changed()
}

func shortenHome(p string) string {
	if remotefs.IsRemote(p) {
		return remotefs.Display(p)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(v, hi)) }
