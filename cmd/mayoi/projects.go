package main

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/internal/vfs"
)

// railW is the width of the rail of project tabs.
const railW = 48

// frame is a native window of the app. It shows one project, or with
// project tabs several, one at a time, which the rail on its left
// chooses among.
type frame struct {
	m   *mayoi
	win *mygo.Window
	// tabs are the window's projects in the rail's order, and active the
	// one it shows.
	tabs   []*desktop
	active *desktop
	// services are the window's, which the projects it does not show post
	// to, once it has drawn.
	services    ui.Services
	hasServices bool
	// merged is set as the window closes once its projects moved to
	// another, and closing once the changes of its projects are settled.
	merged, closing bool
	// shown is the project the window built last.
	shown *desktop
}

// newFrame opens a window that shows h.
func (m *mayoi) newFrame(h *desktop) *frame {
	f := &frame{m: m}
	st := h.a.window
	key := "main"
	switch {
	case m.s.ProjectTabs:
		key = "projects"
	case st.ID > 0:
		key = fmt.Sprintf("main-%d", st.ID)
	}
	opts := mygo.WindowOptions{
		Title: "Mayoi", Width: 1280, Height: 820, MinWidth: 640, MinHeight: 420,
		StateKey:             key,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 14, Y: 13},
		BackgroundColor:      "#131317",
		Content:              ui.View(f.view),
	}
	if len(m.wins) > 0 && m.wins[0].f != nil {
		// Below and right of the window in front, unless the frame of
		// st's window is remembered.
		opts.X, opts.Y = m.wins[0].f.win.Position()
		opts.X, opts.Y = opts.X+28, opts.Y+28
	}
	win := mygo.NewWindow(opts)
	f.win = win
	m.frames = append(m.frames, f)
	f.add(h)
	f.active = h

	// Closing the window saves the files that save themselves, and with
	// other unsaved changes asks about each first.
	win.OnClose(func(e *mygo.CloseEvent) {
		if f.merged || f.closing || m.quitting || !slices.ContainsFunc(f.tabs, func(d *desktop) bool { return d.a.anyDirty() }) {
			return
		}
		e.PreventDefault()
		m.settle(slices.Clone(f.tabs), func() {
			f.closing = true
			win.Close()
		})
	})
	// Back from another app, which may have committed.
	win.OnFocus(func() {
		h := f.active
		if h == nil {
			return
		}
		m.front(h)
		a := h.a
		a.post(func() { a.blurred = false })
		a.post(a.gitChanged)
		a.post(a.wake)
		// The File menu's recent folders leave out this one's.
		m.refreshMenu()
	})
	win.OnBlur(func() {
		if h := f.active; h != nil {
			a := h.a
			a.post(func() { a.blurred = true })
			a.post(a.autoSaveAll)
		}
	})
	// Files dropped where nothing takes them, as on the explorer, open in
	// the file column, avoiding a locked terminal column.
	win.OnFileDrop(func(e *mygo.FileDropEvent) {
		h := f.active
		if h == nil {
			return
		}
		paths, a := e.Paths, h.a
		a.post(func() {
			col := a.fileColumn()
			a.dropFiles(paths, col, col.active+1)
		})
	})
	// The last window to close quits the app, and stays in the settings
	// to open again, as do those the quitting closes. The others keep
	// their folders' sessions.
	win.OnClosed(func() {
		m.frames = slices.DeleteFunc(m.frames, func(o *frame) bool { return o == f })
		if f.merged {
			return
		}
		for _, h := range f.tabs {
			h.a.keepSession()
			h.a.closeTerminals()
			h.a.shutdown()
			m.wins = slices.DeleteFunc(m.wins, func(w *desktop) bool { return w == h })
		}
		if !m.quitting && len(m.wins) > 0 {
			for _, h := range f.tabs {
				m.s.Forget(h.a.window)
			}
		}
		if len(m.wins) == 0 {
			m.s.Flush()
		}
	})
	return f
}

// view builds the project the window shows. Those it does not show run
// what was posted to them, as a terminal's new title, and are built
// anew as they show again.
func (f *frame) view(c *ui.Context) {
	f.services, f.hasServices = c.Services(), true
	// The project shown before goes out of sight now, rather than as
	// another was chosen: the rest of its frame built it still.
	if old := f.shown; old != f.active {
		if old != nil && slices.Contains(f.tabs, old) {
			old.a.park()
		}
		f.shown = f.active
	}
	for _, h := range slices.Clone(f.tabs) {
		if h != f.active && h.a.started {
			h.a.setServices(f.services)
			h.a.drain()
		}
	}
	h := f.active
	if h == nil {
		return
	}
	// Each project's elements are its own, as its tabs are numbered as
	// another's are.
	ui.Column(c.Key(fmt.Sprintf("project-%d", h.a.window.ID))).Fill().Children(func() { h.a.view(c) })
}

// add puts h at the end of the rail, without showing it.
func (f *frame) add(h *desktop) {
	h.f = f
	f.tabs = append(f.tabs, h)
	h.a.rail = func(c *ui.Context) { f.railView(c, h.a) }
	if f.hasServices {
		h.a.setServices(f.services)
	}
	f.keepRail()
}

// keepRail keeps the order of the rail in the settings.
func (f *frame) keepRail() {
	if !f.m.s.ProjectTabs {
		return
	}
	ws := make([]*settings.Window, len(f.tabs))
	for i, h := range f.tabs {
		ws[i] = h.a.window
	}
	f.m.s.SetRail(ws)
}

// activate shows h in the window, in place of the project it showed,
// which saves the files that save themselves.
func (f *frame) activate(h *desktop) {
	if f.active == h || h.f != f {
		return
	}
	if old := f.active; old != nil {
		old.a.autoSaveAll()
		old.a.blurred = true
		// Its frames stop: its tree learns it is idle from here.
		if old.a.tree != nil {
			old.a.tree.SetIdle(true)
		}
	}
	f.active = h
	a := h.a
	a.blurred = false
	// Its files may have changed meanwhile, and the keyboard goes back to
	// its column.
	a.post(a.gitChanged)
	a.post(a.focusColumn)
	f.m.front(h)
	title := a.title
	if title == "" {
		title = "Mayoi"
	}
	f.win.SetTitle(title)
	f.win.Invalidate()
	f.m.refreshMenu()
}

// show brings the window to the front.
func (f *frame) show() {
	if f.win.IsMinimized() {
		f.win.Restore()
	}
	f.win.Focus()
}

// front makes h the project focused last.
func (m *mayoi) front(h *desktop) {
	if i := slices.Index(m.wins, h); i > 0 {
		m.wins = slices.Insert(slices.Delete(m.wins, i, i+1), 0, h)
	}
	m.s.Focused(h.a.window)
}

// stepProject shows the project by steps along the rail, around its ends.
func (h *desktop) stepProject(by int) {
	f := h.f
	if n := len(f.tabs); n > 1 {
		i := slices.Index(f.tabs, f.active)
		f.activate(f.tabs[((i+by)%n+n)%n])
	}
}

// showProject shows the project at i of the rail, the last for -1.
func (h *desktop) showProject(i int) {
	f := h.f
	if i < 0 {
		i = len(f.tabs) - 1
	}
	if i >= 0 && i < len(f.tabs) {
		f.activate(f.tabs[i])
	}
}

// closeProject closes the project, once its changes are settled, or the
// window when it is the window's only one.
func (h *desktop) closeProject() { h.closeProjectThen(nil) }

// closeProjectThen closes the project as closeProject does, then runs
// done, unless its changes were kept open.
func (h *desktop) closeProjectThen(done func()) {
	m, f, a := h.m, h.f, h.a
	if len(f.tabs) < 2 {
		f.win.Close()
		return
	}
	drop := func() {
		m.dropProject(h)
		if done != nil {
			done()
		}
	}
	if !a.started {
		drop()
		return
	}
	h.do(func() {
		if a.autoSaveAll(); !a.anyDirty() {
			drop()
			return
		}
		h.show()
		a.closeKeeping(drop)
	})
}

func (h *desktop) projectTabsChanged() { h.m.projectTabsChanged() }

// dropProject takes h off its window's rail, keeping its session for
// the next time its folder opens, and shows its neighbor.
func (m *mayoi) dropProject(h *desktop) {
	f, a := h.f, h.a
	i := slices.Index(f.tabs, h)
	if i < 0 {
		return
	}
	a.keepSession()
	a.closeTerminals()
	a.shutdown()
	a.resetSearch()
	m.wins = slices.DeleteFunc(m.wins, func(w *desktop) bool { return w == h })
	m.s.Forget(a.window)
	f.tabs = slices.Delete(f.tabs, i, i+1)
	if f.active == h {
		f.active = nil
		f.activate(f.tabs[min(i, len(f.tabs)-1)])
	}
	f.keepRail()
	f.win.Invalidate()
}

// projectTabsChanged gathers the windows into the one in front, as project
// tabs, or parts the projects of each window into a window each, as the
// settings now have it.
func (m *mayoi) projectTabsChanged() {
	if len(m.wins) == 0 || m.wins[0].f == nil {
		return
	}
	front := m.wins[0]
	if m.s.ProjectTabs {
		into := front.f
		// The others after the one in front, the one focused last first.
		for _, h := range slices.Clone(m.wins) {
			from := h.f
			if from == into {
				continue
			}
			from.tabs = slices.DeleteFunc(from.tabs, func(d *desktop) bool { return d == h })
			if from.active == h {
				from.active = nil
			}
			h.a.park()
			into.add(h)
			if len(from.tabs) == 0 {
				from.merged = true
				from.win.Close()
			}
		}
		into.keepRail()
		into.win.Invalidate()
	} else {
		for _, f := range slices.Clone(m.frames) {
			for _, h := range slices.Clone(f.tabs) {
				if h == f.active {
					continue
				}
				f.tabs = slices.DeleteFunc(f.tabs, func(d *desktop) bool { return d == h })
				h.a.park()
				m.newFrame(h)
			}
			f.win.Invalidate()
		}
		// Once the new windows have come forward.
		front.do(front.show)
	}
	m.refreshMenu()
}

// railView is the rail of project tabs, left of the sidebar: a square of
// each project's initials, the one shown marked, and after them a button
// that opens another folder. A window of one project has none. a is the
// project it is built in, which the frame goes on building as a click
// shows another. A square is dragged along the rail to reorder it, and a
// folder dropped on the rail opens as a project.
func (f *frame) railView(c *ui.Context, a *app) {
	if !f.m.s.ProjectTabs || len(f.tabs) < 2 {
		return
	}
	p := a.pal
	rail := ui.Column(c.Key("rail")).Width(railW).Shrink(0).Background(railGround(p)).BorderWidth(0, 1, 0, 0).BorderColor(p.border)
	// Past the last square: at the end.
	f.railDrop(rail, a, nil)
	_, atEnd := ui.DragOver[railDrag](rail)
	folders := rail.FileDragOver()
	rail.Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(8, 0, 10, 0).Gap(6).AlignItems(ui.Center).Children(func() {
				for i, h := range slices.Clone(f.tabs) {
					f.railTab(c, a, h, i)
				}
				if atEnd {
					ui.Box(c).Size(24, 2).Radius(1).Background(p.accent)
				}
				ui.Box(c).Size(20, 1).Margin(4, 0, 2, 0).Background(p.border)
				f.railAdd(c, a, folders)
			})
		})
	})
}

// railGround is the rail's background: a shade off the sidebar's, as an
// activity bar is.
func railGround(p *palette) ui.Color {
	if p.dark {
		return mix(p.chrome, ui.RGBA(0, 0, 0, 1), 0.18)
	}
	return mix(p.chrome, p.border, 0.22)
}

// railDrag is a project's square dragged along the rail.
type railDrag struct{ h *desktop }

// railDrop moves the square dropped on e before the project before, at
// the end for nil, and opens the folders dropped there from another app.
func (f *frame) railDrop(e ui.Element, a *app, before *desktop) {
	if it, ok := ui.Drop[railDrag](e); ok {
		a.post(func() { f.move(it.h, before) })
	}
	if paths := e.DroppedFiles(); paths != nil {
		var dirs []string
		for _, path := range paths {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				dirs = append(dirs, path)
			}
		}
		if len(dirs) > 0 {
			a.post(func() { a.host.openFolders(dirs) })
		}
	}
}

// move puts h before the project before on the rail, at its end for nil.
func (f *frame) move(h, before *desktop) {
	if h == before || !slices.Contains(f.tabs, h) {
		return
	}
	f.tabs = slices.DeleteFunc(f.tabs, func(d *desktop) bool { return d == h })
	at := len(f.tabs)
	if i := slices.Index(f.tabs, before); i >= 0 {
		at = i
	}
	f.tabs = slices.Insert(f.tabs, at, h)
	f.keepRail()
	if f.win != nil {
		f.win.Invalidate()
	}
}

// railSquare is the side of a project's square, and railRadius its corners.
const (
	railSquare = 32
	railRadius = 9
)

// railTab is a project's square on the rail, which shows it, with a pill
// along the rail's edge that grows from under the pointer to the project
// shown, a dot of unsaved changes, and one of a terminal's bell or
// notification while the project was out of sight.
func (f *frame) railTab(c *ui.Context, a *app, h *desktop, i int) {
	p := a.pal
	dir := h.a.folder()
	name, where := "Untitled", "No folder"
	if dir != "" {
		name, where = filepath.Base(dir), shortenHome(dir)
	}
	var keys []string
	if i < 9 {
		mods := "Ctrl+Alt+"
		if macKeys() {
			mods = "Ctrl+CmdOrCtrl+"
		}
		keys = accelCaps(platformAccel("", fmt.Sprintf("%s%d", mods, i+1)), macKeys())
	}
	on := h == f.active
	dirty := h.a.anyDirty()
	if on {
		h.a.alert = ""
	}
	alert := h.a.alert
	col := avatarColor(p, dir)
	if f.m.s.ProjectIcons && dir != "" && h.needsLogoScan(dir) {
		f.scanProjectLogo(a, h, dir)
	}
	b := ui.ButtonBase(c.Key(fmt.Sprintf("project-%d", h.a.window.ID))).Width(railW).Height(railSquare + 4).Shrink(0).
		Label(name).Description(where).Drag(railDrag{h})
	f.railTip(c, a, b, name, where, keys, dirty, alert)
	f.railDrop(b, a, h)
	_, over := ui.DragOver[railDrag](b)
	hovered, pressed, dragged := b.Hovered(), b.Pressed(), b.Dragging()
	pill, fill := float32(0), float32(0.14)
	switch {
	case on:
		pill, fill = 20, 0.30
	case hovered:
		pill, fill = 8, 0.22
	}
	pill = b.Animate("pill", pill, 160*time.Millisecond)
	fill = b.Animate("fill", fill, 120*time.Millisecond)
	ring := b.Animate("ring", unit(on), 160*time.Millisecond)
	sink := b.Animate("sink", unit(pressed && !dragged), 80*time.Millisecond)
	ground := railGround(p)
	b.Draw(func(pt *ui.Painter, r ui.Rect) {
		if over {
			// Where the square dragged goes: before this one.
			pt.Fill(ui.Rect{X: r.X + (r.W-24)/2, Y: r.Y - 4, W: 24, H: 2}, p.accent, 1)
		}
		if pill > 0.5 {
			// Along the rail's edge, as the activity bar marks the view
			// shown.
			pc := p.muted
			if on {
				pc = p.accent
			}
			pt.Fill(ui.Rect{X: r.X - 2, Y: r.Y + (r.H-pill)/2, W: 5, H: pill}, pc, 2.5)
		}
		s := float32(railSquare) - 2*sink
		sq := ui.Rect{X: r.X + (r.W-s)/2, Y: r.Y + (r.H-s)/2, W: s, H: s}
		alpha := float32(1)
		if dragged {
			alpha = 0.4
		}
		pt.Fill(sq, col.Alpha(fill*alpha), railRadius)
		if ring > 0.01 {
			pt.Stroke(sq, col.Alpha(0.75*ring*alpha), railRadius, 1.25)
		}
		ink := col
		if !on && !hovered {
			ink = col.Mix(p.muted, 0.25)
		}
		if src := h.projectLogo(); src != nil && !dragged {
			// Shrinking with the square as it is pressed.
			ls := sq.W - 8
			logo := ui.Rect{X: sq.X + (sq.W-ls)/2, Y: sq.Y + (sq.H-ls)/2, W: ls, H: ls}
			pt.Clip(logo, 6, func() { pt.Image(src, logo, ui.Contain) })
		} else {
			paintInitials(pt, sq, projectWords(name), 12, ink.Alpha(alpha))
		}
		if alert != "" {
			// Below the dot of unsaved changes, in the accent.
			d := ui.Rect{X: sq.X + sq.W - 7, Y: sq.Y + sq.H - 7, W: 10, H: 10}
			pt.Fill(ui.Rect{X: d.X - 2, Y: d.Y - 2, W: d.W + 4, H: d.H + 4}, ground, 7)
			pt.Fill(d, p.accent, 5)
		}
		if dirty {
			// Cut out of the square by a ring of the rail's ground.
			d := ui.Rect{X: sq.X + sq.W - 7, Y: sq.Y - 3, W: 10, H: 10}
			pt.Fill(ui.Rect{X: d.X - 2, Y: d.Y - 2, W: d.W + 4, H: d.H + 4}, ground, 7)
			pt.Fill(d, p.text.Alpha(0.85), 5)
		}
	})
	b.ContextMenu(func(mn *ui.Menu) {
		if mn.Item(revealLabel()).Disabled(dir == "" || remote.Is(dir)).Chosen() {
			a.host.reveal(dir)
		}
		if mn.Item("Copy Path").Disabled(dir == "").Chosen() {
			a.services.WriteClipboard(hostPath(dir))
		}
		mn.Separator()
		if mn.Item("Close Project").Chosen() {
			h.closeProject()
		}
		if mn.Item("Close Other Projects").Chosen() {
			a.post(func() { f.closeOthers(h) })
		}
	})
	if b.Clicked() {
		f.activate(h)
	}
}

// railTip is the tip of a project's square, to its right, where the
// window's buttons above it leave it room: the project's name, its folder,
// the keys that show it and whether it has unsaved changes.
func (f *frame) railTip(c *ui.Context, a *app, b ui.Element, name, where string, keys []string, dirty bool, alert string) {
	p := a.pal
	ui.TooltipBase(c, b, func(tip ui.Element) {
		tip.AttachTo(b, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 4)
		a.floating(c, tip, false).Padding(8, 10).Radius(8).Gap(3).MaxWidth(360)
		tip.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine().Grow(1).MinWidth(0)
				if keys != nil {
					a.caps(c, keys)
				}
			})
			ui.Text(c, where).FontSize(11.5).TextColor(p.muted)
			note := func(dot ui.Color, s string) {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(2, 0, 0, 0).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
					ui.Text(c, s).FontSize(11.5).TextColor(p.muted)
				})
			}
			if alert != "" {
				note(p.accent, alert)
			}
			if dirty {
				note(p.text.Alpha(0.85), "Unsaved changes")
			}
		})
	})
}

// railAdd is the button after the squares that opens another folder: a
// square drawn dashed, which fills under the pointer, or as folders are
// dragged over the rail.
func (f *frame) railAdd(c *ui.Context, a *app, folders bool) {
	p := a.pal
	b := ui.ButtonBase(c.Key("rail-add")).Size(railW, railSquare+4).Shrink(0).Label("Open Folder…").
		Tooltip(a.withKey("Open Folder…", "file.openFolder"))
	hot := b.Hovered() || folders
	k := b.Animate("hot", unit(hot), 120*time.Millisecond)
	b.Draw(func(pt *ui.Painter, r ui.Rect) {
		sq := ui.Rect{X: r.X + (r.W-railSquare)/2, Y: r.Y + (r.H-railSquare)/2, W: railSquare, H: railSquare}
		if k > 0.01 {
			pt.Fill(sq, p.accent.Alpha(0.14*k), railRadius)
		}
		pt.StrokeDashed(sq, p.faint.Mix(p.accent, k), railRadius, 1)
	})
	b.Children(func() {
		ui.Box(c).Absolute().Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconPlus).FontSize(16).TextColor(p.muted.Mix(p.accent, k))
		})
	})
	if b.Clicked() {
		a.chooseFolder()
	}
}

// closeOthers closes the projects of the window but h, one after the
// other, each once its changes are settled, and shows h. Canceling one's
// changes stops there.
func (f *frame) closeOthers(h *desktop) {
	others := slices.DeleteFunc(slices.Clone(f.tabs), func(o *desktop) bool { return o == h })
	var next func()
	next = func() {
		others = slices.DeleteFunc(others, func(o *desktop) bool { return !slices.Contains(f.tabs, o) })
		if len(others) == 0 {
			f.activate(h)
			return
		}
		others[0].closeProjectThen(next)
	}
	next()
}

// projectWords spells a folder's name as words, for its initials: "my-app"
// as "my app", "mayoiEditor" as "mayoi Editor".
func projectWords(name string) string {
	var b strings.Builder
	prev := rune(0)
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == '.':
			r = ' '
		case r >= 'A' && r <= 'Z' && prev >= 'a' && prev <= 'z':
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// logoCandidates are a project's favicon or logo files, in order: the
// root and public/ first, then app/, src/ and the asset folders. A gif
// would animate on the rail.
var logoCandidates = []string{
	"favicon.svg", "favicon.ico", "favicon.png", "favicon.webp",
	"logo.svg", "logo.png", "logo.webp",
	"icon.svg", "icon.png", "icon.webp",
	"public/favicon.svg", "public/favicon.ico", "public/favicon.png", "public/favicon.webp",
	"public/logo.svg", "public/logo.png", "public/logo.webp",
	"public/icon.svg", "public/icon.png", "public/icon.webp",
	"public/apple-touch-icon.png",
	"app/favicon.ico", "app/favicon.png", "app/favicon.webp",
	"app/icon.svg", "app/icon.png", "app/icon.webp", "app/icon.ico",
	"app/apple-icon.png",
	"src/favicon.ico", "src/favicon.svg",
	"src/app/favicon.ico", "src/app/icon.svg", "src/app/icon.png", "src/app/icon.ico",
	"src/app/apple-icon.png",
	"src/assets/logo.svg", "src/assets/logo.png",
	"assets/icon.svg", "assets/icon.png",
	"assets/logo.svg", "assets/logo.png",
	"static/logo.svg", "static/logo.png",
}

// iconSourceFiles may declare the icon with a <link rel="icon"> or icon
// metadata, for names no candidate has.
var iconSourceFiles = []string{
	"index.html",
	"public/index.html",
	"src/index.html",
	"site/index.html",
	"docs/index.html",
	"app/layout.tsx",
	"src/app/layout.tsx",
	"app/routes/__root.tsx",
	"src/routes/__root.tsx",
	"app/root.tsx",
	"src/root.tsx",
}

var (
	linkTagRe  = regexp.MustCompile(`(?i)<link\b[^>]*>`)
	relAttrRe  = regexp.MustCompile(`(?i)\srel\s*=\s*["']([^"']+)["']`)
	hrefAttrRe = regexp.MustCompile(`(?i)\shref\s*=\s*["']([^"']+)["']`)
	iconRelRe  = regexp.MustCompile(`(?:[{,\s])rel\s*:\s*["']([^"']+)["']`)
	iconHrefRe = regexp.MustCompile(`(?:[{,\s])href\s*:\s*["']([^"'?#]+)`)
	iconURLRe  = regexp.MustCompile(`(?:[{,\s])url\s*:\s*["']([^"'?#]+)`)
	iconStrRe  = regexp.MustCompile(`(?:[{,\s])icon\s*:\s*["']([^"'?#]+)["']`)
	iconAppRe  = regexp.MustCompile(`(?:[{,\s])apple\s*:\s*["']([^"'?#]+)["']`)
)

// extractIconHref is the icon an HTML or TSX source declares, or "": its
// path, with the leading slash of one from the site's root.
func extractIconHref(source string) string {
	for _, tag := range linkTagRe.FindAllString(source, -1) {
		rel := relAttrRe.FindStringSubmatch(tag)
		if rel == nil || !hasIconRel(rel[1]) {
			continue
		}
		href := hrefAttrRe.FindStringSubmatch(tag)
		if href == nil {
			continue
		}
		if clean := declaredIconHref(href[1]); clean != "" {
			return clean
		}
	}
	// Icon metadata counts when rel and href share a brace-free run.
	// Next.js metadata also names icons with url:, or a bare icon: or apple: path.
	for _, run := range strings.Split(source, "}") {
		if rel := iconRelRe.FindStringSubmatch(run); rel != nil && hasIconRel(rel[1]) {
			if href := iconHrefRe.FindStringSubmatch(run); href != nil {
				if clean := declaredIconHref(href[1]); clean != "" {
					return clean
				}
			}
			if url := iconURLRe.FindStringSubmatch(run); url != nil {
				if clean := declaredIconHref(url[1]); clean != "" {
					return clean
				}
			}
			continue
		}
		for _, re := range []*regexp.Regexp{iconStrRe, iconAppRe} {
			if m := re.FindStringSubmatch(run); m != nil {
				if clean := declaredIconHref(m[1]); clean != "" {
					return clean
				}
			}
		}
	}
	return ""
}

// hasIconRel reports whether a rel names an icon.
func hasIconRel(rel string) bool {
	for _, f := range strings.Fields(strings.ToLower(rel)) {
		if f == "icon" || f == "apple-touch-icon" {
			return true
		}
	}
	return false
}

// cleanIconHref takes the path of an icon href, or "" for a data URI, a
// remote URL, nothing, or a path outside the project.
func cleanIconHref(href string) string {
	href = strings.TrimSpace(href)
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		href = href[:i]
	}
	if href == "" || strings.HasPrefix(href, "data:") || strings.Contains(href, "://") ||
		strings.HasPrefix(href, "//") {
		return ""
	}
	href = strings.TrimPrefix(strings.TrimPrefix(href, "./"), "/")
	if href == "" || strings.HasSuffix(href, "/") {
		return ""
	}
	// Outside the project is not an icon of it.
	for _, seg := range strings.FieldsFunc(href, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return ""
		}
	}
	return href
}

// declaredIconHref is cleanIconHref of href, keeping the leading slash of
// a path from the site's root.
func declaredIconHref(href string) string {
	clean := cleanIconHref(href)
	if clean != "" && strings.HasPrefix(strings.TrimSpace(href), "/") {
		return "/" + clean
	}
	return clean
}

// resolveIconHref maps an icon href, declared by a source in base, a slash
// folder of the project or "" for its root, to the files it may be. A
// relative href is of base first. A leading slash names the static
// folder, public/, then the root, then base as the root of a site of its
// own, such as site/.
func resolveIconHref(href, base string) []string {
	clean := cleanIconHref(href)
	if clean == "" {
		return nil
	}
	inBase := clean
	if base != "" {
		inBase = base + "/" + clean
	}
	paths := []string{inBase, "public/" + clean, clean}
	if strings.HasPrefix(strings.TrimSpace(href), "/") {
		paths = []string{"public/" + clean, clean, inBase}
	}
	var out []string
	for _, p := range paths {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// inDir is the path of rel, a slash path, in dir: a folder of this
// machine or of a host.
func inDir(dir, rel string) string {
	if remote.Is(dir) {
		return path.Join(dir, rel)
	}
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// regularFiles are those of paths that are regular files, in order, at
// one round trip for the files of a host. A host that cannot be asked
// has none.
func regularFiles(paths []string) []string {
	infos, _ := vfs.StatMany(paths)
	var files []string
	for _, p := range paths {
		if info, ok := infos[p]; ok && info.Mode().IsRegular() {
			files = append(files, p)
		}
	}
	return files
}

// logoFiles are the favicon or logo files in dir, in order of preference.
func logoFiles(dir string) []string {
	paths := make([]string, len(logoCandidates))
	for i, rel := range logoCandidates {
		paths[i] = inDir(dir, rel)
	}
	return regularFiles(paths)
}

// findProjectLogo is the first favicon or logo file in dir, or "" when it
// has none.
func findProjectLogo(dir string) string {
	if files := logoFiles(dir); len(files) > 0 {
		return files[0]
	}
	return ""
}

// iconSourceHead is how much of a source file the icon declaration is
// read from: the head holds it, and a minified bundle must not cost
// megabytes of regex.
const iconSourceHead = 64 << 10

// readIconSource reads the head of a source file that may declare the
// icon. A file of this machine opens without blocking, as readImage does:
// a FIFO must neither stall the scan nor leak its goroutine.
func readIconSource(path string) ([]byte, error) {
	if remote.Is(path) {
		return vfs.ReadFileLimit(path, iconSourceHead)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, errors.New("the file is not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, iconSourceHead))
}

// findIconFromSource is the icon the project's sources declare, or "".
func findIconFromSource(dir string) string {
	paths := make([]string, len(iconSourceFiles))
	for i, rel := range iconSourceFiles {
		paths[i] = inDir(dir, rel)
	}
	for _, path := range regularFiles(paths) {
		data, err := readIconSource(path)
		if err != nil {
			continue
		}
		href := extractIconHref(string(data))
		if href == "" {
			continue
		}
		base := pathDir(iconSourceFiles[slices.Index(paths, path)])
		var cands []string
		for _, cand := range resolveIconHref(href, base) {
			cands = append(cands, inDir(dir, cand))
		}
		if files := regularFiles(cands); len(files) > 0 {
			return files[0]
		}
	}
	return ""
}

// pathDir is the folder of rel, a slash path, or "" for the root.
func pathDir(rel string) string {
	if d := path.Dir(rel); d != "." {
		return d
	}
	return ""
}

// mygoConfigFiles are the configurations of a mygo app, which may name
// its icon.
var mygoConfigFiles = []string{"mygo.json", "mygo.config.ts"}

// mygoIconDefault is the icon of a mygo app whose configuration names none.
const mygoIconDefault = "resources/icon.png"

// findMygoIcon is the icon of the mygo app in dir, or "" when it is not
// one or its icon is missing.
func findMygoIcon(dir string) string {
	paths := make([]string, len(mygoConfigFiles))
	for i, rel := range mygoConfigFiles {
		paths[i] = inDir(dir, rel)
	}
	for _, p := range regularFiles(paths) {
		data, err := readIconSource(p)
		if err != nil {
			continue
		}
		icon := mygoConfigIcon(p, data)
		if icon == "" {
			icon = mygoIconDefault
		}
		if files := regularFiles([]string{inDir(dir, icon)}); len(files) > 0 {
			return files[0]
		}
	}
	return ""
}

// mygoConfigIcon is the icon a mygo configuration names, a slash path of
// the project, or "".
func mygoConfigIcon(path string, data []byte) string {
	if strings.HasSuffix(path, ".json") {
		var cfg struct {
			Icon string `json:"icon"`
		}
		if json.Unmarshal(data, &cfg) != nil {
			return ""
		}
		return cleanIconHref(cfg.Icon)
	}
	if m := iconStrRe.FindSubmatch(data); m != nil {
		return cleanIconHref(string(m[1]))
	}
	return ""
}

// nativeIconDirs may hold a macOS app's .icns: an app's Resources, a Tauri
// app's icons, an Electron app's build resources, or the root.
var nativeIconDirs = []string{
	"Resources", "app/Resources", "macos/Resources", "src-tauri/icons", "build", "",
}

// skipIconDirs are top-level folders not looked in for an asset catalog:
// dependencies and builds.
var skipIconDirs = map[string]bool{
	"node_modules": true, "vendor": true, "build": true, "dist": true, "out": true,
	"target": true, "Pods": true, "DerivedData": true,
}

// maxIconDirs is how many top-level folders are looked in for an asset
// catalog.
const maxIconDirs = 32

// findNativeIcons are a macOS app's icon files in dir, in order of
// preference: the .icns of its resources, then the pictures of the app
// icon sets of the asset catalogs in the root or a top-level folder,
// nearest the rail's size first. A folder of a host costs a round trip
// for each step: the folders, the top-level ones, the catalogs, and each
// icon set.
func findNativeIcons(dir string) []string {
	dirs := make([]string, len(nativeIconDirs))
	for i, rel := range nativeIconDirs {
		dirs[i] = dir
		if rel != "" {
			dirs[i] = inDir(dir, rel)
		}
	}
	lists, _, err := vfs.ReadDirs(dirs)
	if err != nil {
		return nil
	}
	var icons []string
	for _, d := range dirs {
		for _, e := range sortedEntries(lists[d]) {
			if !e.Dir && strings.EqualFold(path.Ext(e.Name), ".icns") {
				icons = append(icons, inDir(d, e.Name))
			}
		}
	}
	var catalogs, subs []string
	for _, e := range sortedEntries(lists[dir]) {
		switch {
		case !e.Dir:
		case strings.HasSuffix(e.Name, ".xcassets"):
			catalogs = append(catalogs, inDir(dir, e.Name))
		case !strings.HasPrefix(e.Name, ".") && !skipIconDirs[e.Name] && len(subs) < maxIconDirs:
			subs = append(subs, inDir(dir, e.Name))
		}
	}
	if len(subs) > 0 {
		sublists, _, err := vfs.ReadDirs(subs)
		if err != nil {
			return icons
		}
		for _, d := range subs {
			for _, e := range sortedEntries(sublists[d]) {
				if e.Dir && strings.HasSuffix(e.Name, ".xcassets") {
					catalogs = append(catalogs, inDir(d, e.Name))
				}
			}
		}
	}
	if len(catalogs) == 0 {
		return icons
	}
	catlists, _, err := vfs.ReadDirs(catalogs)
	if err != nil {
		return icons
	}
	for _, c := range catalogs {
		var sets []string
		for _, e := range sortedEntries(catlists[c]) {
			if e.Dir && strings.HasSuffix(e.Name, ".appiconset") {
				sets = append(sets, e.Name)
			}
		}
		// AppIcon is the app's, before another set such as one of a beta.
		if i := slices.Index(sets, "AppIcon.appiconset"); i > 0 {
			sets = slices.Insert(slices.Delete(sets, i, i+1), 0, "AppIcon.appiconset")
		}
		for _, set := range sets {
			setDir := inDir(c, set)
			data, err := readIconSource(inDir(setDir, "Contents.json"))
			if err != nil {
				continue
			}
			for _, name := range appIconFiles(data) {
				icons = append(icons, inDir(setDir, name))
			}
		}
	}
	return regularFiles(icons)
}

// sortedEntries are es by name: a host need not list a folder in order.
func sortedEntries(es []vfs.Entry) []vfs.Entry {
	es = slices.Clone(es)
	slices.SortFunc(es, func(a, b vfs.Entry) int { return cmp.Compare(a.Name, b.Name) })
	return es
}

// appIconFiles are the png pictures an app icon set's Contents.json lists,
// nearest the rail's size first.
func appIconFiles(contents []byte) []string {
	var set struct {
		Images []struct {
			Filename string `json:"filename"`
			Size     string `json:"size"`
			Scale    string `json:"scale"`
		} `json:"images"`
	}
	if json.Unmarshal(contents, &set) != nil {
		return nil
	}
	type pic struct {
		name string
		side int
	}
	var pics []pic
	for _, im := range set.Images {
		// A picture is beside Contents.json, never in a folder of it.
		if !strings.EqualFold(path.Ext(im.Filename), ".png") || strings.ContainsAny(im.Filename, `/\`) ||
			slices.ContainsFunc(pics, func(p pic) bool { return p.name == im.Filename }) {
			continue
		}
		w, _, _ := strings.Cut(im.Size, "x")
		side, _ := strconv.ParseFloat(w, 64)
		scale := 1.0
		if s, err := strconv.ParseFloat(strings.TrimSuffix(im.Scale, "x"), 64); err == nil && s > 0 {
			scale = s
		}
		pics = append(pics, pic{im.Filename, int(side * scale)})
	}
	slices.SortStableFunc(pics, func(a, b pic) int { return nearestSide(a.side, b.side) })
	names := make([]string, len(pics))
	for i, p := range pics {
		names[i] = p.name
	}
	return names
}

// nearestSide orders sides nearest the rail's first: the smallest no
// smaller than logoSide, the larger ones after it, then the smaller ones,
// largest first.
func nearestSide(a, b int) int {
	if big := a >= logoSide; big != (b >= logoSide) {
		if big {
			return -1
		}
		return 1
	}
	if a >= logoSide {
		return cmp.Compare(a, b)
	}
	return cmp.Compare(b, a)
}

// loadProjectLogo decodes the project's favicon or logo, or nil when it
// has none or what it has does not decode: its well-known files, a mygo
// app's icon, the icon its sources declare, then a macOS app's icon.
func loadProjectLogo(dir string) ui.ImageSource {
	for _, path := range logoFiles(dir) {
		if src := decodeProjectLogo(path); src != nil {
			return src
		}
	}
	for _, find := range []func(string) string{findMygoIcon, findIconFromSource} {
		if path := find(dir); path != "" {
			if src := decodeProjectLogo(path); src != nil {
				return src
			}
		}
	}
	for _, path := range findNativeIcons(dir) {
		if src := decodeProjectLogo(path); src != nil {
			return src
		}
	}
	return nil
}

// decodeProjectLogo decodes one icon file, or nil.
func decodeProjectLogo(path string) ui.ImageSource {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		data, err := readImage(path)
		if err != nil {
			return nil
		}
		s, err := ui.ParseSVG(data)
		if err != nil {
			return nil
		}
		if w, h := s.Size(); w <= 0 || h <= 0 {
			return nil
		}
		return s
	case ".ico":
		data, err := readImage(path)
		if err != nil {
			return nil
		}
		img, err := decodeICO(data)
		if err != nil {
			return nil
		}
		return logoBitmap(img)
	case ".icns":
		data, err := readImage(path)
		if err != nil {
			return nil
		}
		img, err := decodeICNS(data)
		if err != nil {
			return nil
		}
		return logoBitmap(img)
	}
	data, err := readImage(path)
	if err != nil {
		return nil
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil ||
		cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > maxIconPixels/int64(cfg.Height) {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return logoBitmap(img)
}

// logoSide is the most pixels on a side a logo keeps once decoded: the
// rail shows it at 24 DIPs, and each project holds its own for as long as
// it is open.
const logoSide = 96

// logoBitmap is img for the rail, scaled down to logoSide when larger.
func logoBitmap(img image.Image) *ui.Bitmap {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if side := max(w, h); side > logoSide {
		s := float64(logoSide) / float64(side)
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*s)), max(1, int(float64(h)*s))))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	return ui.NewBitmap(img)
}

// maxIconPixels is the largest an icon entry decodes at: favicons are
// small, and a lying header must not allocate without bound.
const maxIconPixels = 4096 * 4096

// decodeICO decodes a favicon.ico: its entries largest first, taking the
// first that decodes, PNG or 32-bit BMP.
func decodeICO(data []byte) (image.Image, error) {
	if len(data) < 6 || data[0] != 0 || data[1] != 0 || data[2] != 1 || data[3] != 0 {
		return nil, errors.New("not an icon file")
	}
	n := int(data[4]) | int(data[5])<<8
	type entry struct {
		at, size int
		png      bool
	}
	var entries []entry
	for i := 0; i < n; i++ {
		off := 6 + i*16
		if off+16 > len(data) {
			break
		}
		size := int(binary.LittleEndian.Uint32(data[off+8:]))
		at := int(binary.LittleEndian.Uint32(data[off+12:]))
		if size <= 0 || at < 0 || at+size > len(data) {
			continue
		}
		entries = append(entries, entry{at, size, isPNG(data[at:])})
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		if a.size != b.size {
			return b.size - a.size
		}
		if a.png != b.png {
			if a.png {
				return -1
			}
			return 1
		}
		return 0
	})
	for _, e := range entries {
		entry := data[e.at : e.at+e.size]
		if e.png {
			// The entry's own dimensions, not the directory's: a lying
			// header must not allocate without bound.
			if cfg, err := png.DecodeConfig(bytes.NewReader(entry)); err != nil ||
				int64(cfg.Width) <= 0 || int64(cfg.Height) <= 0 ||
				int64(cfg.Width) > maxIconPixels/int64(cfg.Height) {
				continue
			}
			if img, err := png.Decode(bytes.NewReader(entry)); err == nil {
				return img, nil
			}
			continue
		}
		if img, err := decodeDIB(entry); err == nil {
			return img, nil
		}
	}
	return nil, errors.New("the icon has no picture")
}

// decodeICNS decodes a macOS app's .icns: its PNG entries nearest the
// rail's size first, taking the first that decodes. The older entries,
// run-length coded or JPEG 2000, are skipped: an icns of today has PNG
// ones too.
func decodeICNS(data []byte) (image.Image, error) {
	if len(data) < 8 || string(data[:4]) != "icns" {
		return nil, errors.New("not an icns file")
	}
	type entry struct {
		data []byte
		side int
	}
	var entries []entry
	for at := 8; at+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[at+4:]))
		if n < 8 || n > len(data)-at {
			break
		}
		body := data[at+8 : at+n]
		at += n
		if !isPNG(body) {
			continue
		}
		// The entry's own dimensions: a lying header must not allocate
		// without bound.
		cfg, err := png.DecodeConfig(bytes.NewReader(body))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
			int64(cfg.Width) > maxIconPixels/int64(cfg.Height) {
			continue
		}
		entries = append(entries, entry{body, max(cfg.Width, cfg.Height)})
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return nearestSide(a.side, b.side) })
	for _, e := range entries {
		if img, err := png.Decode(bytes.NewReader(e.data)); err == nil {
			return img, nil
		}
	}
	return nil, errors.New("the icon has no picture")
}

// isPNG reports whether data starts with a PNG's magic.
func isPNG(data []byte) bool {
	return len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n"
}

// decodeDIB decodes the 32-bit BMP of an icon entry.
func decodeDIB(data []byte) (image.Image, error) {
	if len(data) < 40 || binary.LittleEndian.Uint32(data) != 40 {
		return nil, errors.New("not a 32-bit icon picture")
	}
	// int64 throughout: a lying header must neither overflow nor
	// allocate without bound.
	w := int64(int32(binary.LittleEndian.Uint32(data[4:])))
	hh := int64(int32(binary.LittleEndian.Uint32(data[8:])))
	if w <= 0 || w > maxIconPixels {
		return nil, errors.New("not a 32-bit icon picture")
	}
	h, topDown := hh/2, false
	if hh < 0 {
		// Top-down: the height is negative, doubled or not, so what the
		// bytes hold decides.
		h, topDown = -hh, true
		if h%2 == 0 {
			if half := h / 2; half > 0 && int64(len(data)) < 40+w*h*4 && int64(len(data)) >= 40+w*half*4 {
				h = half
			}
		}
	}
	if h <= 0 || h > maxIconPixels/w || binary.LittleEndian.Uint16(data[12:]) != 1 ||
		binary.LittleEndian.Uint16(data[14:]) != 32 || binary.LittleEndian.Uint32(data[16:]) != 0 {
		return nil, errors.New("not a 32-bit icon picture")
	}
	if int64(len(data)) < 40+w*h*4 {
		return nil, errors.New("the icon picture is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	for y := int64(0); y < h; y++ {
		sy := h - 1 - y
		if topDown {
			sy = y
		}
		for x := int64(0); x < w; x++ {
			at := 40 + (sy*w+x)*4
			img.SetNRGBA(int(x), int(y), color.NRGBA{R: data[at+2], G: data[at+1], B: data[at], A: data[at+3]})
		}
	}
	return img, nil
}

// projectLogo is the project's favicon or logo for the rail, or nil for
// its initials: when project icons are off, or none was found.
func (h *desktop) projectLogo() ui.ImageSource {
	if h.m.s.ProjectIcons {
		return h.logoSrc
	}
	return nil
}

// needsLogoScan reports whether the logo of dir is yet to be looked for:
// it was not, it was for another folder, or none was found a minute ago.
// A folder of a host that is not connected waits for it.
func (h *desktop) needsLogoScan(dir string) bool {
	if remote.Is(dir) && !remote.IsConnected(dir) {
		return false
	}
	return !h.logoDone || h.logoDir != dir ||
		h.logoSrc == nil && time.Since(h.logoScannedAt) > time.Minute
}

// scanProjectLogo finds the project's favicon or logo in the background,
// showing it on the rail once decoded. a is the project shown, whose
// frame takes the result: it is the one building, so its services are
// set, which a hidden project's may not be.
func (f *frame) scanProjectLogo(a *app, h *desktop, dir string) {
	h.logoDone = true
	h.logoDir = dir
	h.logoSrc = nil // not the logo of the folder it was for
	h.logoScannedAt = time.Now()
	h.logoGen++
	gen := h.logoGen
	go func() {
		src := loadProjectLogo(dir)
		a.post(func() {
			if h.logoGen != gen {
				return
			}
			h.logoSrc = src
		})
	}()
}

// unit is 1 for true and 0 for false, as an animation's target.
func unit(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
