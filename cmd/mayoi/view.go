package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/remote"
	"mayoi/locales"
)

const (
	titleBarH  = 38
	tabStripH  = 35
	crumbsH    = 24
	statusBarH = 24
	panelHeadH = 34
	// panelTitleW is the least width of a panel at a side that still
	// shows its title, past the room its terminals and tools need.
	panelTitleW = 300
)

// view builds the window: a title bar, the explorer beside the editor
// and the terminal panel below it, and a status bar.
func (a *app) view(c *ui.Context) {
	// The window's, which may be another than the frame before's as the
	// project moves to another window.
	a.setServices(c.Services())
	a.review.ctx = c
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
	updateLinuxControlsTheme(a.fade.to)
	a.themeTerminals()
	a.fontTerminals()
	a.copyTerminals()
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
			if a.rail != nil {
				a.rail(c)
			}
			s, w := a.settings, a.window
			// The panel is at the window's edge, past the sidebar, so
			// that with both on a side the panel is the outer one.
			side := ""
			if w.PanelOpen {
				side = a.panelSide()
			}
			sidebar := func() {
				if w.SidebarClosed {
					return
				}
				// Its splitter is along the edge facing the editors.
				at, dir := edgeRight, float32(1)
				box := ui.Column(c.Key("sidebar")).Width(w.SidebarWidth).Shrink(0).BorderColor(p.border)
				if s.SidebarRight {
					at, dir = edgeLeft, -1
					box.BorderWidth(0, 0, 0, 1)
				} else {
					box.BorderWidth(0, 1, 0, 0)
				}
				box.Children(func() {
					a.sidebar(c)
					a.splitter(c, at, func(d float32) {
						width, _ := c.Size()
						w.SidebarWidth = clamp(w.SidebarWidth+dir*d, 160, max(width-320, 160))
					})
				})
			}
			if side == "left" {
				a.sidePanel(c, false)
			}
			if !s.SidebarRight {
				sidebar()
			}
			main := ui.Column(c.Key("main")).MinWidth(0)
			if (side == "left" || side == "right") && a.panelMax {
				// A panel maximized at a side takes the row, so the
				// editors give it theirs rather than sharing it. One at
				// the bottom is in the editors' column, and takes theirs.
				main.Grow(0).Width(0)
			} else {
				main.Grow(1)
			}
			main.Children(func() { a.main(c) })
			if s.SidebarRight {
				sidebar()
			}
			if side == "right" {
				a.sidePanel(c, true)
			}
		})
		a.statusBar(c)
	})
	a.quickOpenView(c)
	a.branchPickerView(c)
	a.repositoryPickerView(c)
	a.recentPickerView(c)
	a.themePickerView(c)
	a.remotePickerView(c)
	a.conflictsView(c)
	a.promptView(c)
	a.confirmView(c)
	a.signinView(c)
	if !a.dialogBuilt {
		a.dialog = ""
	}
	a.dialogBuilt = false
	a.windowKeys(c)
	if inEditor := a.keyFocus == placeEditor; inEditor != a.cutInEditor {
		a.cutInEditor = inEditor
		a.host.menuChanged()
	}
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

// main is the columns of editors and, below them, the panel when it is
// at the bottom.
func (a *app) main(c *ui.Context) {
	p, w := a.pal, a.window
	panel := w.PanelOpen
	a.remoteBanner(c)
	editor := ui.Column(c.Key("editor")).Grow(1).MinHeight(0).Background(p.editor).Clip()
	if panel && a.panelMax {
		// Out of sight, but built, so that the editors keep their state.
		editor.Grow(0).Height(0).Invisible()
	}
	editor.Children(func() { a.columns(c) })
	if !panel || a.panelSide() != "bottom" {
		return
	}
	_, h := c.Size()
	room := h - titleBarH - statusBarH
	height := clamp(w.PanelHeight, 80, max(room-80, 80))
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
				w.PanelHeight = clamp(height-d, 80, max(room-80, 80))
			})
		}
	})
}

// panelSide is the side the terminal panel is on: "bottom", "left" or
// "right".
func (a *app) panelSide() string {
	switch a.settings.PanelPosition {
	case "left", "right":
		return a.settings.PanelPosition
	}
	return "bottom"
}

// sidePanel is the terminal panel at a side of the window, past the
// sidebar, its splitter along the edge facing the editors.
func (a *app) sidePanel(c *ui.Context, right bool) {
	p, w := a.pal, a.window
	width, _ := c.Size()
	at, dir := edgeRight, float32(1)
	box := ui.Column(c.Key("panel")).Shrink(0).BorderColor(p.border)
	if right {
		at, dir = edgeLeft, -1
		box.BorderWidth(0, 0, 0, 1)
	} else {
		box.BorderWidth(0, 1, 0, 0)
	}
	// It leaves the editors 320 points beside the sidebar and the rail,
	// as the sidebar leaves them, however narrow the window.
	room := width - 320
	if !w.SidebarClosed {
		room -= w.SidebarWidth
	}
	if a.rail != nil {
		room -= railW
	}
	room = max(room, minPanelW)
	size := clamp(w.PanelWidth, minPanelW, room)
	if a.panelMax {
		box.Grow(1)
	} else {
		box.Width(size)
	}
	box.Children(func() {
		a.panel(c)
		if !a.panelMax {
			a.splitter(c, at, func(d float32) {
				w.PanelWidth = clamp(size+dir*d, minPanelW, room)
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
	// The line shows once the pointer rests on the edge, as one crossing
	// it on its way elsewhere does not light it, and fades in and out.
	// Held, it shows at once.
	var lit float32
	if h.Pressed() || h.Dragging() {
		lit = h.Jump("lit", 1)
	} else {
		show := ui.Animation{Duration: 150 * time.Millisecond}
		if h.Hovered() {
			show.Delay = 250 * time.Millisecond
		}
		lit = h.AnimateTo("lit", unit(h.Hovered()), show)
	}
	h.Children(func() {
		if lit < 0.01 {
			return
		}
		line := ui.Box(c).Absolute().Background(a.pal.accentDim.Alpha(lit))
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
	rightPad := bar.Right + 8
	if runtime.GOOS == "linux" && bar.Right > 0 {
		rightPad = bar.Right - 2
	}
	ui.Row(c).Height(max(bar.Height, titleBarH)).Padding(0, rightPad, 0, bar.Left+10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).DragWindow().Children(func() {
		// The sidebar's button is on its side. The terminal's is on the
		// panel's: at the left of the bar when the panel is at the
		// window's left, past the sidebar's button when the sidebar is
		// there too, as the panel lies past the sidebar; at the right
		// otherwise.
		right := a.settings.SidebarRight
		panelLeft := a.panelSide() == "left"
		sidebarButton := func() {
			icon := iconSidebar
			if right {
				icon = iconSidebarRight
			}
			if a.iconButton(c, icon, a.withKey(locales.CmdViewSidebar(), "view.sidebar"), !a.window.SidebarClosed).Clicked() {
				a.toggleSidebar()
			}
		}
		terminalButton := func() {
			btn := a.iconButton(c, iconTerminal, a.withKey(locales.CmdViewTerminal(), "view.terminal"), a.window.PanelOpen)
			if runtime.GOOS == "linux" {
				btn.Padding(1, 0, 0, 0)
			}
			if btn.Clicked() {
				a.togglePanel()
			}
		}
		ui.Row(c).Basis(0).Grow(1).MinWidth(56).Gap(2).Children(func() {
			if hostOS != "darwin" {
				a.iconButton(c, iconMenu, locales.Menu(), false).Menu(a.appUIMenu)
			}
			if panelLeft {
				terminalButton()
			}
			if !right {
				sidebarButton()
			}
		})
		ui.Row(c).Width(420).MaxWidthPercent(68).Shrink(1).MinWidth(140).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			a.commandCenter(c)
		})
		ui.Row(c).Basis(0).Grow(1).MinWidth(56).Justify(ui.End).Gap(2).Children(func() {
			if !panelLeft {
				terminalButton()
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
	b := ui.ButtonBase(c.Key("command-center")).Height(24).FillWidth().Padding(0, 10).Gap(8).
		Radius(6).Justify(ui.Center).AlignItems(ui.Center).Border(1, p.border).Label(locales.QuickOpenLabel()).Tooltip(a.withKey(locales.QuickOpenLabel(), "go.quickOpen"))
	b.Background(p.surface)
	hovered := b.Hovered()
	switch {
	case b.Pressed():
		b.Background(p.surfacePressed)
	case hovered:
		b.Background(p.surfaceHover).Border(1, mix(p.border, p.muted, 0.3))
	}
	b.Transition(hoverFade)
	room := b.Bounds().W - 2*10 - 12 - 2*8 // its padding, the icon and the gaps
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
			folder = filepath.Base(a.root)
			if host := remote.HostOf(a.root); host != "" {
				folder = filepath.Base(hostPath(a.root)) + " [SSH: " + host + "]"
			}
		}
		var nameW float32
		if name != "" {
			nameW, _ = c.MeasureText(0, ui.Span{Text: name, Size: 12, Weight: 550})
		}
		if name != "" && folder != "" && room > 0 {
			// The folder goes before the name is cut short.
			folderW, _ := c.MeasureText(0, ui.Span{Text: folder, Size: 12})
			if nameW+8+folderW > room {
				folder = ""
			}
		}
		switch {
		case name != "" && a.root != "":
			// A short name, as a terminal's "zsh", keeps its own width
			// rather than a gap after it.
			ui.Text(c, name).FontSize(12).FontWeight(550).TextColor(p.text).SingleLine().Shrink(1).MinWidth(min(32, nameW)).
				ContentTransition(ui.ContentTransition{})
			if folder == "" {
				break
			}
			ui.Text(c, folder).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(24)
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
	b.Transition(revealFade)
	pressPose(b)
	b.Children(func() { ui.Icon(c, icon).FontSize(15).TextColor(col) })
	return b
}

// minColumnW is the least width a column is dragged to.
const minColumnW = 160

// minPanelW is the least width the panel is dragged to at a side.
const minPanelW = 160

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
	a.findCol = a.column()
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
		if col.locked {
			a.tabStrip(c, col)
		}
		body := ui.Column(c).Bind(&col.body).Grow(1).MinHeight(0)
		over := a.takeFiles(body, col)
		body.Children(func() {
			if col.locked {
				ui.Column(c).Fill().Center().Gap(12).Background(a.pal.editor).Children(func() {
					ui.Text(c, locales.LockedColumn()).FontSize(14).TextColor(a.pal.muted)
					ui.Column(c).Width(180).MaxWidthPercent(100).Children(func() {
						a.welcomeAction(c, iconPlus, locales.CmdViewNewTerminal(), "", func() {
							a.newTerminalTab(col, a.workDir(), true)
						})
					})
				})
			} else {
				a.welcome(c)
			}
			a.dropZones(c, col, over)
		})
		return
	}
	a.tabStrip(c, col)
	shown := col.tab()
	if shown != nil && shown.term == nil {
		a.breadcrumbs(c, col, shown)
	}
	for _, t := range col.tabs {
		if t != shown && t.ed != nil {
			a.hideEditor(t.ed)
		}
	}
	body := ui.Box(c.Key("body")).Bind(&col.body).Grow(1)
	over := a.takeFiles(body, col)
	body.Children(func() {
		if shown != nil {
			ui.Column(c.Key(shown.id)).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				switch {
				case shown.term != nil:
					a.terminalView(c, shown.term)
				case shown.view != nil:
					a.viewerView(c, shown.view, shown.name())
				case shown.diff != nil:
					a.diffViewUI(c, shown.diff)
				case shown.merge != nil:
					a.mergeViewUI(c, shown.merge)
				case shown.ed.loading || shown.ed.loadErr != nil:
					a.hideEditor(shown.ed)
					a.loadingView(c, shown.ed)
				case shown.ed.preview != nil:
					a.hideEditor(shown.ed)
					a.viewerView(c, shown.ed.preview, shown.name())
				default:
					a.editorView(c, shown.ed)
					a.hunkPeekView(c, shown.ed)
					a.reviewBoxView(c, shown.ed)
					if col == a.findCol {
						a.findView(c, shown.ed)
					}
					a.vimLineView(c, shown.ed)
				}
			})
		}
		a.dropZones(c, col, over)
	})
}

func (a *app) hideEditor(d *editor) {
	if !d.hidden {
		d.hidden, d.hiddenY = true, d.vscroll.Y
	}
}

// dragItem is what is dragged to a column: one of its tabs, or of
// another's, a terminal of the panel, or a file of the sidebar.
type dragItem struct {
	tab  *colTab
	term *session
	file *fileDrag
}

// owns reports whether what was dragged is this window's to take: a tab or
// a terminal dragged from another window stays there, as its view binds
// controls of that window, or a file, which opens in any.
func (a *app) owns(it dragItem) bool {
	switch {
	case it.file != nil:
		return true
	case it.term != nil:
		return slices.Contains(a.terms, it.term)
	case it.tab != nil:
		col, _ := a.locate(it.tab)
		return col != nil
	}
	return false
}

// dropInColumn puts what was dragged at place at of a column.
func (a *app) dropInColumn(it dragItem, col *column, at int) {
	if slices.Index(a.cols, col) < 0 || !a.owns(it) {
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
	if slices.Index(a.cols, col) < 0 || !a.owns(it) {
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

// breadcrumbs name the file a tab of column col shows and, in an editor,
// the blocks its caret is in. A folder of the file's path shows its files
// in a box, as clicked. An SVG has a button there to show it as a picture
// or as text.
func (a *app) breadcrumbs(c *ui.Context, col *column, t *colTab) {
	p := a.pal
	d := t.ed
	var parts []crumb
	if path := t.path(); path != "" {
		parts = crumbsOf(path, a.root, homeDir())
	} else if t.diff != nil && t.diff.commit != nil && t.diff.commit.from != "" {
		parts = []crumb{{locales.Comparison(), ""}, {t.name(), ""}}
	} else if t.diff != nil && t.diff.commit != nil {
		parts = []crumb{{locales.DiffCommitName(git.ShortHash(t.diff.commit.hash)), ""}, {t.name(), ""}}
	} else {
		parts = []crumb{{t.name(), ""}}
	}
	if k := &a.crumbs; k.open && k.col == col && !a.crumbsValid() {
		a.closeCrumbs()
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
				icon, col := fileIcon(p, part.name)
				switch {
				case t.diff != nil && t.diff.commit != nil:
					icon, col = iconCommit, p.accent
				case t.diff != nil:
					icon, col = iconDiff, p.changed
				}
				ui.Icon(c, icon).FontSize(13).TextColor(col)
			}
			if part.dir != "" && t.diff == nil {
				// A folder: its files in a box below it.
				k := &a.crumbs
				on := k.open && k.tab == t && k.dir == part.dir
				b := ui.ButtonBase(c.Key("crumb:"+part.dir)).Height(crumbsH-6).Padding(0, 4).Margin(0, -4).Radius(4).
					AlignItems(ui.Center).Shrink(0).Label(locales.CrumbsFolderTip(part.name))
				if on || b.Hovered() {
					b.Background(p.hover)
				}
				b.Transition(hoverFade)
				b.Children(func() {
					col := p.muted
					if on || b.Hovered() {
						col = p.text
					}
					ui.Text(c, part.name).FontSize(12).SingleLine().TextColor(col)
				})
				if b.Clicked() {
					a.openCrumbs(col, t, part.dir)
				}
				if on {
					a.crumbsBox(c, b)
				}
			} else {
				t := ui.Text(c, part.name).FontSize(12).SingleLine().TextColor(p.muted).Shrink(0)
				if last {
					// A long name, as a commit's subject, ends in an ellipsis.
					t.TextColor(p.text).Shrink(1).MinWidth(24)
				}
			}
			if !last || len(scope) > 0 {
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(p.faint)
			}
		}
		for i, line := range scope {
			label := scopeLabel(snap, line)
			b := ui.ButtonBase(c.Key(fmt.Sprintf("scope:%d", i))).Height(crumbsH-6).Padding(0, 5).Radius(4).
				AlignItems(ui.Center).Shrink(1).MinWidth(0).Label(locales.GoToScope(label))
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
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
		if d != nil && canPreview(d.document) {
			ui.Spacer(c)
			a.previewButton(c, t, d)
		}
		if t.diff != nil {
			ui.Spacer(c)
			a.diffButtons(c, t)
		}
	})
}

// previewButton switches between a document's preview and source.
func (a *app) previewButton(c *ui.Context, t *colTab, d *editor) {
	p := a.pal
	label, icon := a.withKey(locales.ShowPreview(), "view.togglePreview"), iconImage
	if d.preview != nil {
		label, icon = a.withKey(locales.ShowSource(), "view.togglePreview"), iconFileCode
	}
	b := ui.ButtonBase(c.Key("preview")).Height(crumbsH-4).Padding(0, 6).Radius(4).Shrink(0).
		AlignItems(ui.Center).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	pressPose(b)
	// The icon turns into the other's, the preview's or the source's.
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(p.muted).ContentTransition(ui.ContentTransition{}) })
	if b.Clicked() {
		a.post(func() {
			a.setPreview(d, d.preview == nil)
			if col, i := a.locate(t); col != nil {
				a.activate(col, i, true)
			}
		})
	}
}

// statusVisibility is which of the parts of the status bar that can go
// show.
type statusVisibility struct {
	branchLabel   bool
	gitSyncCounts bool
	folder        bool
	terminal      bool
	lines         bool
	indent        bool
	encoding      bool
	eol           bool
	language      bool
}

// statusFit returns which parts of the status bar show in width: as the
// window narrows, the least needed go first, each only once the bar
// would not hold it. The position is measured at its widest, so that the
// parts stay as the caret moves.
func (a *app) statusFit(c *ui.Context, width float32) statusVisibility {
	measure := func(s string, icon bool) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: s, Size: 11.5})
		w += 2*6 + 2 // the item's padding, and the gap after it
		if icon {
			w += 12 + 5
		}
		return w
	}
	var v statusVisibility
	used := float32(2 * 6)
	type part struct {
		on *bool
		w  float32
	}
	// In the order they go.
	var parts []part
	add := func(on *bool, w float32) { parts = append(parts, part{on, w}) }
	if m := a.themeMascot(); m != nil {
		w, _ := m.size(1)
		used += w + 12 + 2
	}
	if busy := a.repository().git.busy; busy != "" {
		used += measure(busy, true)
	}
	if a.conn != nil {
		used += measure("SSH: "+a.conn.Host, true) + 2
		if l := portsLabel(a.conn.Forwards()); l != "" {
			used += measure(l, true)
		}
	}
	if x := a.transferText(); x != "" {
		used += measure(x, true)
	}
	if k := len(a.review.notes); k > 0 {
		used += measure(fmt.Sprint(k), true)
	}
	if len(a.chord) > 0 {
		used += measure(locales.ChordWaiting(strings.Join(chordCaps(a.chord), " ")), true)
	}
	folder := func() {
		if a.root != "" {
			label := filepath.Base(a.root)
			if len(a.repositories.contexts) > 1 {
				label = a.repositoryLabel()
			}
			add(&v.folder, measure(label, true))
		}
	}
	t := a.activeTab()
	switch d := a.activeEditor(); {
	case a.activeViewer() != nil, t != nil && t.diff != nil, d == nil:
		folder()
	case t != nil && t.term != nil:
		folder()
		dir, size := t.term.term.Dir(), ""
		if cols, rows := t.term.term.Size(); cols > 0 {
			size = fmt.Sprintf("%d × %d", cols, rows)
		}
		if dir != "" || size != "" {
			add(&v.terminal, measure(shortenHome(dir), true)+measure(size, false))
		}
	default:
		snap := d.buf.Snapshot()
		n := snap.LineCount()
		used += measure(locales.StatusPos(n, 999), false)
		if a.settings.Vim {
			// The mode's badge, and the keys typed after it.
			used += measure(d.vim.mode.String(), false) + 2*6
			if k := d.vim.pending(); k != "" {
				used += measure(k, false)
			}
			if a.vimRec != 0 {
				used += measure(locales.VimRecording(string(a.vimRec)), false) + 6 + 5
			}
			if m := a.vimMsg; m.text != "" && time.Since(m.at) < vimMessageFor {
				used += min(measure(m.text, false), 420) + 6
			}
		}
		if d.diskGone {
			used += measure(locales.DeletedOnDisk(), true)
		}
		eol := "LF"
		if d.crlf {
			eol = "CRLF"
		}
		add(&v.encoding, measure("UTF-8", false))
		folder()
		add(&v.lines, measure(locales.LinesCount(n), false))
		add(&v.eol, measure(eol, false))
		add(&v.indent, measure(d.indent.String(), false))
		add(&v.language, measure(highlight.DisplayName(d.hl.Lexer()), false))
	}
	if a.host.account() != "" {
		used += syncStatusW
	}
	if version := a.host.update(); version != "" {
		used += measure(version, true)
	}
	if repo := a.repository(); repo.gitSyncVisible() {
		used += 2*6 + 12 + 2 // the sync icon stays beside the branch
		if label := repo.gitSyncLabel(); label != "" {
			add(&v.gitSyncCounts, measure(label, false)-2*6-2+5)
		}
	}
	if a.branch() != "" {
		used += 2*6 + 12 + 2 // its icon, which stays
		add(&v.branchLabel, measure(a.branch(), false)-2*6-2+5)
	}
	for _, p := range parts {
		*p.on = true
		used += p.w
	}
	for _, p := range parts {
		if used <= width {
			break
		}
		*p.on = false
		used -= p.w
	}
	return v
}

// gitSyncStatus is separate from the branch menu so syncing needs one
// click. The icon remains when the bar is too narrow for the counts.
func (a *gitContext) gitSyncStatus(c *ui.Context, showCounts bool) {
	if !a.gitSyncVisible() {
		return
	}
	p, g := a.app.pal, a.git
	label, tip, icon := locales.GitSync(), locales.GitSyncTip(g.status.Upstream), iconRefresh
	if g.status.Upstream == "" {
		label, tip, icon = locales.ScmPublishBranch(), locales.ScmPublishBranch(), iconPush
	}
	if g.busy != "" {
		tip = g.busy
	}
	b := ui.ButtonBase(c.Key("status:git-sync")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
		AlignItems(ui.Center).Label(label).Tooltip(tip).Disabled(g.busy != "")
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(p.statusFg)
		if counts := a.gitSyncLabel(); showCounts && counts != "" {
			ui.Text(c, counts).FontSize(11.5).TextColor(p.statusFg).SingleLine()
		}
	})
	if b.Clicked() {
		a.gitSync()
	}
}

// vimBadge shows vim mode's mode in the status bar, as a badge colored
// by it: plain in normal mode, the accent in insert mode, and the color
// of a change in visual mode; then the keys of a command typed so far.
func (a *app) vimBadge(c *ui.Context, v *vimState, text func(string) ui.Element) {
	p := a.pal
	bg, fg := p.text.Alpha(0.09), p.statusFg
	switch {
	case v.mode == vimInsert:
		bg, fg = p.fill, p.fillText
	case v.mode.visual():
		bg, fg = p.modified.Alpha(0.22), p.modifiedFg
	}
	row := ui.Row(c.Key("status:vim")).Height(statusBarH).Padding(0, 6).Gap(6).AlignItems(ui.Center)
	// From mode to mode, the badge's colors cross over. Its name changes at
	// once: the old fading out over the new broke up the strokes of both.
	fade := ui.Animation{Duration: 140 * time.Millisecond}
	bg, fg = row.AnimateColor("bg", bg, fade), row.AnimateColor("fg", fg, fade)
	row.Children(func() {
		ui.Text(c, v.mode.String()).FontSize(10.5).FontWeight(650).LetterSpacing(0.6).TextColor(fg).SingleLine().
			Padding(1, 6).Radius(4).Background(bg)
		if a.vimRec != 0 {
			// A register recording, as vim says: a red dot, and its name.
			ui.Row(c.Key("status:vim-rec")).Gap(5).AlignItems(ui.Center).
				Transition(ui.ElementTransition{Duration: 160 * time.Millisecond, Enter: &ui.Motion{Scale: 0.8}, Exit: &ui.Motion{Scale: 0.8}}).
				Children(func() {
					ui.Box(c).Width(6).Height(6).Radius(3).Background(p.dangerFg)
					ui.Text(c, locales.VimRecording(string(a.vimRec))).FontSize(11.5).TextColor(p.dangerFg).SingleLine()
				})
		}
		if k := v.pending(); k != "" {
			text(k)
		}
		// What the last command said, as a search that went round, for a
		// moment; an error in the color of one.
		if m := a.vimMsg; m.text != "" && time.Since(m.at) < vimMessageFor {
			col := p.statusFg
			if m.err {
				col = p.dangerFg
			}
			ui.Text(c.Key("status:vim-msg"), m.text).FontSize(11.5).TextColor(col).SingleLine().MaxWidth(420).
				Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Colors: true, Enter: &ui.Motion{}, Exit: &ui.Motion{}}).
				ContentTransition(ui.ContentTransition{})
		}
	})
}

func (a *app) statusBar(c *ui.Context) {
	p := a.pal
	repo := a.repository()
	width, _ := c.Size()
	visible := a.statusFit(c, width)
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
			b.Transition(hoverFade)
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
		menuItem := func(show bool, key, label, tip string, build func(m *ui.Menu)) {
			if !show {
				return
			}
			b := ui.ButtonBase(c.Key("status:"+key)).Height(statusBarH-6).Padding(0, 6).Radius(4).
				AlignItems(ui.Center).Label(tip).Tooltip(tip)
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() { text(label) })
			b.Menu(build)
		}
		if m := a.themeMascot(); m != nil {
			a.statusMascot(c, m)
		} else {
			// None shows: a repaint of the frame before, which had one,
			// may have set it moving as the theme changed.
			a.mascot = mascotState{}
		}
		a.remoteStatus(c, text)
		a.portsStatus(c, text)
		if b := repo.branch(); b != "" {
			bb := ui.ButtonBase(c.Key("status:branch")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
				AlignItems(ui.Center).Label(locales.BranchLabel(b)).Tooltip(locales.BranchStatusTip(b)).Disabled(repo.git.busy != "")
			if bb.Hovered() {
				bb.Background(p.hover)
			}
			bb.Transition(hoverFade)
			bb.Children(func() {
				ui.Icon(c, iconBranch).FontSize(12).TextColor(p.statusFg)
				if visible.branchLabel {
					// Switched to another, the name rolls to it.
					text(b).ContentTransition(ui.ContentTransition{})
				}
			})
			if bb.Clicked() {
				repo.openBranchPicker()
			}
		}
		repo.gitSyncStatus(c, visible.gitSyncCounts)
		a.transferStatus(c, text)
		if busy := repo.git.busy; busy != "" {
			ui.Row(c.Key("status:busy")).Height(statusBarH).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
				// In a theme with a character, it hops for it instead.
				if a.themeMascot() == nil {
					ui.Spinner(c).Size(11, 11)
				}
				text(busy)
			})
		}
		if len(a.chord) > 0 {
			item("chord", locales.ChordWaiting(strings.Join(chordCaps(a.chord), " ")), "", iconKeyboard, nil)
		}
		if a.root != "" && visible.folder {
			if len(a.repositories.contexts) > 1 {
				a.repositoryButton(c, true)
			} else if a.conn != nil {
				item("folder", filepath.Base(hostPath(a.root)), "", iconFolder, nil)
			} else {
				item("folder", filepath.Base(a.root), revealFolderLabel(shortenHome(a.root)), iconFolder, func() { a.host.reveal(a.root) })
			}
		}
		a.reviewStatus(c, text)
		ui.Spacer(c)
		// Settings sync is last, at the right, whatever shows before it,
		// and a new version of Mayoi before it.
		defer a.syncStatus(c)
		if v := a.host.update(); v != "" {
			defer item("update", v, locales.UpdateAvailable(v), iconUpdate, a.host.installUpdate)
		}
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
			if dir := term.Dir(); dir != "" && visible.terminal {
				item("cwd", shortenHome(dir), "", iconTerminal, nil)
			}
			if cols, rows := term.Size(); cols > 0 && visible.terminal {
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
				text(locales.DeletedOnDisk()).TextColor(p.dangerFg)
			})
		}
		snap := d.buf.Snapshot()
		caret := min(d.caret, snap.Len())
		if a.settings.Vim && d.vim.mode.visual() {
			caret = vimCursor(d, snap)
		}
		line := snap.LineAt(caret)
		s, _ := snap.LineRange(line)
		pos := locales.StatusPos(line+1, columns(snap.Slice(s, caret))+1)
		if n := abs(d.sel[1] - d.sel[0]); n > 0 {
			pos += locales.StatusSelected(n)
		}
		if a.settings.Vim {
			a.vimBadge(c, &d.vim, text)
		}
		item("pos", pos, a.withKey(locales.GoToLineLabel(), "go.line"), nil, func() { a.openQuick(":") })
		if visible.lines {
			item("lines", locales.LinesCount(snap.LineCount()), "", nil, nil)
		}
		menuItem(visible.indent, "indent", d.indent.String(), locales.Indentation(), func(m *ui.Menu) {
			if m.Item(locales.CmdSelIndentTabs()).Checked(d.indent.tabs).Chosen() {
				d.indent.tabs = true
			}
			for _, n := range []int{2, 4, 8} {
				if m.Item(locales.CmdSelIndentSpaces(n)).Checked(!d.indent.tabs && d.indent.size == n).Chosen() {
					d.indent = indentStyle{size: n}
				}
			}
			m.Separator()
			if m.Item(locales.ConvertToTabs()).Chosen() {
				a.editActive(func(d *editor) { reindent(d, indentStyle{tabs: true, size: tabWidth}) })()
			}
			if m.Item(locales.ConvertToSpaces()).Chosen() {
				size := d.indent.size
				if d.indent.tabs {
					size = tabWidth
				}
				a.editActive(func(d *editor) { reindent(d, indentStyle{size: size}) })()
			}
		})
		if visible.encoding {
			item("enc", "UTF-8", "", nil, nil)
		}
		eol := "LF"
		if d.crlf {
			eol = "CRLF"
		}
		menuItem(visible.eol, "eol", eol, locales.LineEndings(), func(m *ui.Menu) {
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
		if visible.language {
			item("lang", highlight.DisplayName(d.hl.Lexer()), "", nil, nil)
		}
	})
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
				// Inset as the rows below, so the logo lines up with their icons.
				riseIn(ui.Row(c.Key("welcome:head")).Padding(0, 10).Gap(16).AlignItems(ui.Center), 0).Children(func() {
					if m := a.themeMascot(); m != nil {
						a.welcomeMascot(c, m)
					} else {
						// Clipped to the icon's squircle: 185 of its 824 DIPs.
						ui.Column(c).Size(52, 52).Radius(52.0 * 185 / 824).Clip().Children(func() {
							ui.Image(c, logo()).Size(52, 52)
						})
					}
					ui.Column(c).Gap(3).Children(func() {
						ui.Text(c, "Mayoi").FontSize(26).FontWeight(600).LetterSpacing(-0.3).TextColor(p.text)
						ui.Text(c, locales.WelcomeTagline()).FontSize(13).TextColor(p.muted)
					})
				})
				if v := a.host.update(); v != "" {
					a.welcomeSection(c, locales.WelcomeUpdate(), 30*time.Millisecond, func() {
						a.welcomeAction(c, iconUpdate, locales.UpdateAvailable(v), "", a.host.installUpdate)
					})
				}
				a.welcomeSection(c, locales.WelcomeStart(), 60*time.Millisecond, func() {
					if a.root == "" {
						a.welcomeAction(c, iconFolderOpen, locales.CmdFileOpenFolder(), a.keyHint("file.openFolder"), a.chooseFolder)
						if remoteSupported {
							a.welcomeAction(c, iconServer, locales.CmdRemoteConnect(), a.keyHint("remote.connect"), a.openRemotePicker)
						}
					} else {
						a.welcomeAction(c, iconFileSearch, locales.CmdGoQuickOpen(), a.keyHint("go.quickOpen"), func() { a.openQuick("") })
						a.welcomeAction(c, iconSearch, locales.CmdEditFindInFiles(), a.keyHint("edit.findInFiles"), func() { a.showSearch(false) })
					}
					a.welcomeAction(c, iconCommand, locales.CmdGoCommands(), a.keyHint("go.commands"), func() { a.openQuick(">") })
					a.welcomeAction(c, iconFilePlus, locales.CmdFileNew(), a.keyHint("file.new"), a.newFile)
					a.welcomeAction(c, iconTerminal, locales.CmdViewTerminal(), a.keyHint("view.terminal"), a.togglePanel)
					a.welcomeAction(c, iconSidebar, locales.CmdViewSidebar(), a.keyHint("view.sidebar"), a.toggleSidebar)
					a.welcomeAction(c, iconSparkles, locales.CmdViewSelectTheme(), a.keyHint("view.selectTheme"), a.openThemePicker)
					if a.host.account() == "" && a.settings.Path() != "" {
						a.welcomeAction(c, iconCloud, locales.CmdAccountSignIn(), "", func() { a.host.signIn() })
					}
				})
				var recent []string
				if a.root == "" {
					recent = a.settings.RecentExcept("")
				}
				if len(recent) > 0 {
					a.welcomeSection(c, locales.WelcomeRecent(), 120*time.Millisecond, func() {
						for _, dir := range recent[:min(6, len(recent))] {
							r := ui.ButtonBase(c.Key("recent:"+dir)).FillWidth().Height(welcomeRowH).Padding(0, 10).Radius(6).Gap(10).
								Justify(ui.Start).AlignItems(ui.Center).Label(locales.RecentOpenLabel(dir)).Tooltip(shortenHome(dir))
							if r.Hovered() {
								r.Background(p.hover)
							}
							r.Transition(hoverFade)
							r.Children(func() {
								icon := iconFolder
								if remote.Is(dir) {
									icon = iconServer
								}
								ui.Icon(c, icon).FontSize(15).TextColor(p.accent)
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
						// The rest, in Open Recent's picker.
						if len(recent) > 6 {
							a.welcomeAction(c, iconMore, locales.MenuOpenRecentMore(), a.keyHint("file.openRecent"), a.openRecentPicker)
						}
					})
				}
				a.welcomeStar(c)
			})
		})
	})
}

// repoURL is Mayoi's repository on GitHub.
const repoURL = "https://github.com/sorrycc/Mayoi"

// welcomeStar is the link at the foot of the welcome page that opens
// Mayoi's repository, to star it: quieter than the rows above it.
func (a *app) welcomeStar(c *ui.Context) {
	p := a.pal
	r := ui.ButtonBase(c.Key("welcome:star")).AlignSelf(ui.Start).Height(24).Padding(0, 10).Radius(6).Gap(6).
		AlignItems(ui.Center).Label(locales.WelcomeStar()).Tooltip(repoURL)
	col := p.faint
	if r.Hovered() {
		col = p.accent
	}
	col = r.AnimateColor("text", col, ui.Animation{Duration: hoverFade.Duration})
	riseIn(r, 150*time.Millisecond).Children(func() {
		ui.Icon(c, iconStar).FontSize(13).TextColor(col)
		ui.Text(c, locales.WelcomeStar()).FontSize(12).TextColor(col).SingleLine()
	})
	if r.Clicked() {
		a.services.OpenURL(repoURL)
	}
}

// welcomeRowH is the height of every row of the welcome page, with keys
// or without, so that they keep one rhythm.
const welcomeRowH = 30

// welcomeSection is a titled group of the welcome page.
// It rises in after delay as the page shows.
func (a *app) welcomeSection(c *ui.Context, title string, delay time.Duration, rows func()) {
	p := a.pal
	riseIn(ui.Column(c.Key("welcome:"+title)).Gap(2), delay).Children(func() {
		// As the sidebar titles its views.
		ui.Text(c, strings.ToUpper(title)).FontSize(11).FontWeight(600).LetterSpacing(0.7).TextColor(p.muted).Padding(0, 10, 6)
		rows()
	})
}

// welcomeAction is a row of the welcome page that runs a command: its
// icon, its name and its keys.
func (a *app) welcomeAction(c *ui.Context, icon *ui.SVG, label, keys string, fn func()) {
	p := a.pal
	r := ui.ButtonBase(c.Key("action:"+label)).FillWidth().Height(welcomeRowH).Padding(0, 10).Radius(6).Gap(10).
		Justify(ui.Start).AlignItems(ui.Center).Label(label)
	hovered := r.Hovered()
	if hovered {
		r.Background(p.hover)
	}
	r.Transition(hoverFade)
	col := p.muted
	if hovered {
		col = p.accent
	}
	col = r.AnimateColor("icon", col, ui.Animation{Duration: hoverFade.Duration})
	r.Children(func() {
		ui.Icon(c, icon).FontSize(15).TextColor(col)
		ui.Text(c, label).FontSize(13).TextColor(p.text).Grow(1).SingleLine()
		a.keycaps(c, keys)
	})
	if r.Clicked() {
		fn()
	}
}

// keycaps shows keys as the caps of a keyboard.
func (a *app) keycaps(c *ui.Context, keys string) { a.caps(c, splitCaps(keys)) }

// splitCaps splits keys as keyText writes them into their caps: those of
// "Ctrl+Shift+P" at the pluses, those of "⇧⌘P" a symbol each, with a
// named key, as "esc" or "F3", one cap, and the strokes of a chord apart
// by spaces one after the other.
func splitCaps(keys string) []string {
	var caps []string
	for _, stroke := range strings.Fields(keys) {
		if len(stroke) > 1 && strings.Contains(stroke, "+") {
			// The key may be "+" itself, after the modifiers.
			plus := strings.HasSuffix(stroke, "++")
			for _, k := range strings.Split(strings.TrimSuffix(stroke, "++"), "+") {
				if k != "" {
					caps = append(caps, k)
				}
			}
			if plus {
				caps = append(caps, "+")
			}
			continue
		}
		word := ""
		for _, r := range stroke {
			if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				word += string(r)
				continue
			}
			if word != "" {
				caps, word = append(caps, word), ""
			}
			caps = append(caps, string(r))
		}
		if word != "" {
			caps = append(caps, word)
		}
	}
	return caps
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
	a.window.SidebarClosed = !a.window.SidebarClosed
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

// toggleTerminalLocation has new terminals open in the editor area, or in
// the panel again.
func (a *app) toggleTerminalLocation() {
	if a.settings.TerminalLocation == "" {
		a.settings.TerminalLocation = "editor"
	} else {
		a.settings.TerminalLocation = ""
	}
	a.settings.Changed()
}

// setPanelPosition moves the terminal panel to a side of the window, or
// back below the editors for "", and shows it when it was hidden.
func (a *app) setPanelPosition(pos string) {
	a.settings.PanelPosition = pos
	a.settings.Changed()
	if !a.window.PanelOpen {
		a.togglePanel()
	}
}

func shortenHome(p string) string {
	if remote.Is(p) {
		return remote.Display(p)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return shortenIn(p, home)
	}
	return p
}

// shortenIn is p with ~ for the home folder home when p is in it, or is
// it: of a folder, not of a name home only begins, and on Windows
// whatever the case of its letters, as C: and c: are one disk.
func shortenIn(p, home string) string {
	same := func(a, b string) bool { return a == b || hostOS == "windows" && strings.EqualFold(a, b) }
	home = strings.TrimSuffix(home, string(filepath.Separator))
	if home == "" || len(p) < len(home) || !same(p[:len(home)], home) {
		return p
	}
	if rest := p[len(home):]; rest == "" || strings.HasPrefix(rest, string(filepath.Separator)) {
		return "~" + rest
	}
	return p
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(v, hi)) }
