package main

import (
	"cmp"
	"fmt"
	"io"
	"mayoi/internal/keymap"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	mtransfer "github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// session is a terminal: one of the panel, or one a tab of a column
// shows.
type session struct {
	id    int
	term  *terminal.Terminal
	title string
	dir   string
	view  ui.Handle // its view, which takes the keyboard
	tab   *colTab   // the tab that shows it, nil for one of the panel
	// lost is set while a terminal of a host waits for the connection
	// that ended it, to start again.
	lost bool
	// hc is the address of the terminal of herdr-core the shell runs in,
	// "" for one that runs here.
	hc string
	// attached is when the view last attached to it, and hcNew is set
	// until the first attach of a shell just started, which has no
	// history to show.
	attached time.Time
	hcNew    bool
	// hcStarting is set while its shell starts in herdr-core, in the
	// background, and the terminal shows nothing.
	hcStarting bool
	hcWait     *hcWait
	// hcReady is when its shell in herdr-core last came to be attached,
	// having started or its machine come back, which the terminal shows
	// for a moment.
	hcReady time.Time
	// waitSince is when it last began to wait, to start in herdr-core or
	// for its connection or machine.
	waitSince time.Time
	// find is its find bar's: open, what it looks for, how the search
	// goes, as OnSearch tells, and back set when the last step went
	// toward newer output, which rolls the count down.
	findOpen  bool
	findQuery string
	found     terminal.SearchStatus
	findInput ui.Handle
	findShook time.Time
	// status is what its programs say they do (OSC 7501), statusSince
	// when the record most in need of the user last changed, and
	// statusFrom the state that record was in before.
	status      terminal.ProgramStatus
	statusSince time.Time
	statusFrom  terminal.StatusState
	// name is what the user renamed it to, "" for none, shown over the
	// title its programs give it.
	name string
	// started is when its program started, to tell a shell that exits
	// as it starts from one the user ended.
	started time.Time
}

// label is what the terminal is called: the name the user gave it, or
// the title its programs give it.
func (s *session) label() string {
	return cmp.Or(s.name, s.title)
}

// termTheme is the terminal's colors for a palette: its background the
// panel's, so that the panel and the editor are of a piece.
func termTheme(p *palette) *terminal.Theme {
	return &terminal.Theme{
		Foreground: p.text, Background: p.panel,
		Cursor: p.accent, Selection: p.selection,
		// The matches of find as the editor's, made opaque.
		Search: p.match.Over(p.panel), SearchText: p.text,
		SearchSelected: p.matchNow.Over(p.panel), SearchSelectedText: p.text,
		Palette: p.ansi,
	}
}

// termThemes are the terminal's colors in a light window and in a dark
// one, for the theme of the settings.
func (a *app) termThemes() (light, dark *terminal.Theme) {
	th := a.settings.Theme
	return termTheme(paletteFor(th, false)), termTheme(paletteFor(th, true))
}

// themeTerminals gives the terminals the colors of the theme, once it
// changes, as they follow the window's light and dark themselves.
func (a *app) themeTerminals() {
	if a.settings.Theme == a.termTheme {
		return
	}
	a.termTheme = a.settings.Theme
	light, dark := a.termThemes()
	for _, s := range a.terms {
		s.term.SetTheme(light, dark)
	}
}

// workDir is where a terminal starts: the workspace, or the home
// directory.
func (a *app) workDir() string {
	if a.root != "" {
		return a.root
	}
	dir, _ := os.UserHomeDir()
	return dir
}

// startTerminal starts a terminal in dir, nil when it cannot.
func (a *app) startTerminal(dir string) *session {
	s := &session{dir: dir, title: shellName()}
	a.offerHC()
	if c := sessionConn(s); c != nil {
		s.title = c.Host
	}
	// A shell in herdr-core, of the host in a remote folder as one of hc's
	// machines, starts in the background: the terminal shows meanwhile.
	s.hcStarting = a.hcBackend()
	if a.addTerminal(s) == nil {
		return nil
	}
	if s.hcStarting {
		a.beginWait(s)
		a.hcBegin(s)
	}
	return s
}

// addTerminal starts the program of a terminal, nil when it cannot.
func (a *app) addTerminal(s *session) *session {
	a.nextTerm++
	s.id = a.nextTerm
	term, err := a.spawnTerminal(s, s.dir)
	if err != nil {
		a.fail("%s", locales.FailStartTerminal(err.Error()))
		return nil
	}
	s.term, s.started = term, time.Now()
	a.terms = append(a.terms, s)
	return s
}

// spawnTerminal starts the program of a terminal in dir: the shell, of
// the host in a remote folder.
func (a *app) spawnTerminal(s *session, dir string) (*terminal.Terminal, error) {
	light, dark := a.termThemes()
	// In a remote folder, a shell of its host, over the connection.
	var command, env []string
	if c, rdir, ok := remote.Lookup(dir); ok {
		command = c.TerminalCommand(rdir)
		dir, _ = os.UserHomeDir()
	}
	// In herdr-core, its terminal there.
	if s.hc != "" {
		command = hcAttach(s.hc, !s.hcNew)
		env = hcEnv()
		s.attached, s.hcNew = time.Now(), false
	}
	// Its shell still starting in herdr-core, nothing yet.
	var conn io.ReadWriteCloser
	if s.hcStarting {
		s.hcWait = newHCWait()
		conn = s.hcWait
	}
	// term is the terminal made, which OnExit tells from one that took
	// its place, and in what its programs last said that waits for the
	// app.
	var term *terminal.Terminal
	var in statusIn
	term, err := terminal.New(terminal.Options{
		Command:   command,
		Env:       env,
		Conn:      conn,
		Dir:       dir,
		Font:      termFont(a.settings),
		Theme:     light,
		DarkTheme: dark,
		Cursor:    terminal.CursorBar,
		// Selecting copies, as in Ghostty and iTerm2.
		CopyOnSelect: copyOnSelect(a.prefs().TerminalCopyOnSelect),
		OnTitle: func(title string) {
			a.post(func() { s.title = title })
		},
		// A terminal of a host gets files and images pasted as copies
		// uploaded to it.
		OnPaste: func(d mtransfer.Data) bool {
			text, _ := d.Read(mtransfer.Text)
			return a.terminalPaste(s, string(text))
		},
		// Local file paths open in the editor; links to a port of a
		// remote host open the port forwarded to it here.
		OpenLink: func(link string) bool { return a.terminalLink(s, link) },
		// The shortcuts of keybindings.json, before the shell, and on
		// Windows and Linux those of Mayoi's it leaves to Mayoi.
		OnKey: func(mods ui.Modifiers, key ui.Key) bool {
			a.keyTerm = s
			st := keymap.Stroke{Mods: mods, Key: key}
			if a.keyStroke(st, placeTerminal) {
				return true
			}
			if hcDetachKey(s, mods, key) {
				return true
			}
			id, ok := a.shellSkip(st)
			if !ok {
				a.seeProgramStatus(s)
			}
			if ok {
				a.post(func() {
					if id == "" {
						a.showTabKey(key)
					} else {
						a.run(id)
					}
				})
			}
			return ok
		},
		// CmdOrCtrl+= and CmdOrCtrl+- scale the whole window: Command on
		// macOS, Control on Windows/Linux. Ignore terminal-local zoom so
		// scrolling with the modifier held cannot change its font size.
		OnZoom: func(terminal.Zoom) bool { return true },
		OnSearch: func(st terminal.SearchStatus) {
			a.post(func() { s.found = st })
		},
		OnExit: func(code int) {
			a.post(func() { a.terminalExited(s, term, code) })
		},
		// A program done in a project out of sight, as a build or an
		// agent, has the rail mark the project's square.
		OnBell: func() {
			a.post(func() { a.alert, a.alertState = locales.BellFrom(s.label()), terminal.StatusIdle })
		},
		OnNotify: func(title, body string) {
			a.post(func() {
				a.alert, a.alertState = strings.TrimSpace(title+" "+body), terminal.StatusIdle
				if a.alert == "" {
					a.alert = locales.NotificationFrom(s.label())
				}
			})
		},
		OnProgramStatus: func(st terminal.ProgramStatus) {
			// Once a frame however often the program says it.
			if in.put(st) {
				a.post(func() {
					if st, ok := in.take(); ok {
						a.programStatus(s, term, st)
					}
				})
			}
		},
	})
	return term, err
}

// newTerminal starts a terminal in the panel and shows it.
func (a *app) newTerminal() { a.newTerminalIn(a.workDir()) }

// newTerminalIn starts a terminal of the panel in dir and shows it.
func (a *app) newTerminalIn(dir string) {
	if s := a.startPanelTerminal(dir); s != nil {
		a.showTerminalView()
		a.rememberTabs()
		a.focusTerminal()
	}
}

// startPanelTerminal starts a terminal of the panel in dir, without
// showing it, which the state leaves as it was.
func (a *app) startPanelTerminal(dir string) *session {
	s := a.startTerminal(dir)
	if s == nil {
		return nil
	}
	a.panelTerm = s
	return s
}

// openTerminal starts a terminal in dir where terminalLocation says: in
// the panel, or in a tab of the file column.
func (a *app) openTerminal(dir string) {
	if a.settings.TerminalLocation == "editor" {
		a.newTerminalTab(a.fileColumn(), dir, true)
		return
	}
	a.newTerminalIn(dir)
}

// newTerminalTab starts a terminal in dir in a new tab of a column, after
// the one shown, and reports whether it did.
func (a *app) newTerminalTab(col *column, dir string, focus bool) bool {
	return a.newTerminalTabAt(col, col.active+1, dir, focus)
}

// newTerminalTabAt starts a terminal in dir in a tab at place at of a
// column.
func (a *app) newTerminalTabAt(col *column, at int, dir string, focus bool) bool {
	return a.terminalTab(col, at, a.startTerminal(dir), focus)
}

// terminalTab shows a terminal started in a tab at place at of a column,
// and reports whether there was one.
func (a *app) terminalTab(col *column, at int, s *session, focus bool) bool {
	if s == nil {
		return false
	}
	s.tab = &colTab{term: s}
	at = min(max(at, 0), len(col.tabs))
	a.addTab(col, at, s.tab)
	a.activate(col, at, focus)
	return true
}

// lookPath finds a program on the path, as exec.LookPath, which tests
// replace.
var lookPath = exec.LookPath

// shellName is the name of the shell a terminal runs: $SHELL's, and on
// Windows, which has none, the first of those the terminal looks for that
// is there: PowerShell's, then the command prompt.
func shellName() string {
	if hostOS == "windows" {
		for _, name := range []string{"pwsh.exe", "powershell.exe", os.Getenv("COMSPEC"), "cmd.exe"} {
			if _, err := lookPath(name); name != "" && err == nil {
				return strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ".exe")
			}
		}
		return "shell"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return filepath.Base(sh)
	}
	return "shell"
}

// panelTerms returns the terminals of the panel.
func (a *app) panelTerms() []*session {
	var out []*session
	for _, s := range a.terms {
		if s.tab == nil {
			out = append(out, s)
		}
	}
	return out
}

// leavePanel has the panel show another of its terminals in place of s,
// which goes: the one after it, or the one before the last. The panel
// hides after its last.
func (a *app) leavePanel(s *session) {
	rest := a.panelTerms()
	i := slices.Index(rest, s)
	if i < 0 {
		return
	}
	rest = slices.Delete(rest, i, i+1)
	if a.panelTerm == s {
		a.panelTerm = nil
		if len(rest) > 0 {
			a.panelTerm = rest[min(i, len(rest)-1)]
		}
	}
	if len(rest) == 0 {
		// The panel keeps the view it shows, if it has one, and hides
		// after its last terminal when it has none.
		if side, ok := a.viewSide(panelTerminal); ok {
			if p := a.window.Panels.Get(side); len(p.Views) == 1 {
				p.Open = false
				if a.panelMax == side {
					a.panelMax = ""
				}
			}
		}
		a.settings.Changed()
	}
}

// removeTerminal drops a terminal whose program ended, or that the user
// killed: its tab closes, or the panel hides after its last.
func (a *app) removeTerminal(s *session) {
	i := slices.Index(a.terms, s)
	if i < 0 {
		return
	}
	s.term.Close()
	if t := s.tab; t != nil {
		a.terms = slices.Delete(a.terms, i, i+1)
		a.dropTab(t)
		a.focusColumn()
		a.rememberTabs()
		return
	}
	a.leavePanel(s)
	a.terms = slices.Delete(a.terms, i, i+1)
	a.rememberTabs()
	if a.panelTerm == nil {
		a.focusColumn()
	} else {
		a.focusTerminal()
	}
}

// terminalToColumn moves a terminal to a tab at place at of a column,
// from the panel or from another tab.
func (a *app) terminalToColumn(s *session, col *column, at int) {
	if s.tab != nil {
		a.moveTab(s.tab, col, at)
		return
	}
	a.leavePanel(s)
	s.tab = &colTab{term: s}
	a.addTab(col, at, s.tab)
	if _, i := a.locate(s.tab); i >= 0 {
		a.activate(col, i, true)
	}
}

// terminalToPanel moves a terminal from its tab to the panel, which
// shows it.
func (a *app) terminalToPanel(s *session) {
	t := s.tab
	if t == nil {
		return
	}
	s.tab = nil
	a.dropTab(t)
	a.panelTerm = s
	a.showTerminalView()
	a.rememberTabs()
	a.focusTerminal()
}

// focusTerminal gives the keyboard to the terminal the panel shows.
func (a *app) focusTerminal() {
	if s := a.panelTerm; s != nil {
		s.view.Focus()
	}
	a.focus = paneTerminal
}

// closeTerminals hangs up every terminal, as the window closes.
func (a *app) closeTerminals() {
	for _, s := range a.terms {
		s.term.Close()
	}
	a.terms, a.panelTerm = nil, nil
}

// hidePanel hides the panel on a side, and gives the keyboard back to the
// column when it was in that panel.
func (a *app) hidePanel(s settings.Side) {
	holds := a.panelHoldsKeys(s)
	if p := a.window.Panels.Get(s); p != nil {
		p.Open = false
		a.settings.Changed()
	}
	if a.panelMax == s {
		a.panelMax = ""
	}
	if holds {
		a.focusColumn()
	}
}

// panelHoldsKeys reports whether the keyboard is in the panel on a side:
// in its terminal, its tree or one of its fields.
func (a *app) panelHoldsKeys(s settings.Side) bool {
	p := a.window.Panels.Get(s)
	if !p.Visible() {
		return false
	}
	switch p.Shown() {
	case panelTerminal:
		return a.focus == paneTerminal || a.keyFocus == placeTerminal
	case settings.ViewExplorer:
		return a.keyFocus == placeExplorer
	case settings.ViewSearch, settings.ViewSCM:
		return a.keyFocus == placeInput
	}
	return false
}

// panel is the panel on a side: its views' tabs, the tools of the view
// shown, and the view itself. A terminal dragged from a tab is dropped on
// it, and a file dragged from a view types its path.
func (a *app) panel(c *ui.Context, side settings.Side) {
	p := a.pal
	panel := a.window.Panels.Get(side)
	box := ui.Column(c).Fill().Background(p.panel)
	if it, ok := ui.Drop[dragItem](box); ok {
		switch s := a.panelTerm; {
		case it.tab != nil && it.tab.term != nil && a.owns(it):
			a.post(func() { a.terminalToPanel(it.tab.term) })
		case it.file != nil && s != nil && panel.Shown() == settings.ViewTerminal:
			// A file of a view types its path, as one from the Finder.
			a.post(func() { a.pastePaths(s, []string{it.file.path}) })
		}
	}
	over, _ := a.dragOver(box)
	hovered := box.Hovered()
	box.Children(func() {
		a.panelHeader(c, side)
		switch view := panel.Shown(); view {
		case settings.ViewTerminal:
			a.panelTerminals(c, side, over)
		default:
			ui.Column(c.Key("view:" + view)).Grow(1).MinHeight(0).Children(func() { a.viewBody(c, view, hovered) })
		}
	})
}

// panelHeader is the row at the top of the panel: its views as tabs, and
// the tools of the view shown. A panel on a side may be too narrow for the
// tabs, the tools and the buttons in one row: it names its tabs short, and
// gives the tools and the buttons a row of their own.
func (a *app) panelHeader(c *ui.Context, side settings.Side) {
	panel := a.window.Panels.Get(side)
	view := panel.Shown()
	// The bottom panel is as wide as the editors. A side panel's header
	// follows the width left after both side panels are clamped, not the
	// width the panel would have in a wider window.
	wide := side == settings.SideBottom || a.panelMax == side
	if !wide {
		width, _ := c.Size()
		size, _ := a.sidePanelSize(side, width)
		wide = size >= panelWide
	}
	ui.Row(c).Height(panelHeadH).Padding(0, 8, 0, 14).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("panel-tabs:" + string(side))).Grow(1).MinWidth(0).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, v := range panel.Views {
				label := a.viewTitle(v)
				if !wide && v == settings.ViewSCM {
					label = viewShort(v)
				}
				// A theme may set titles in sentence case.
				label = a.pal.title(label)
				// Source control counts the files it has to show.
				count := 0
				if v == settings.ViewSCM {
					count = a.changeCount()
				}
				tab := a.panelTab(c, side, v, label, count, view == v, func() { a.showPanelView(v) })
				tab.ContextMenu(func(m *ui.Menu) { a.panelTabMenu(v, side, m) })
			}
		})
		if !wide {
			return
		}
		a.viewTitleActions(c, view, true)
		a.panelButtons(c, side)
	})
	if wide {
		return
	}
	ui.Row(c).Height(panelHeadH-6).Padding(0, 8, 0, 14).Gap(2).AlignItems(ui.Center).Children(func() {
		a.viewTitleActions(c, view, true)
		a.panelButtons(c, side)
	})
}

// panelButtons are the buttons that maximize the panel, or hide it.
func (a *app) panelButtons(c *ui.Context, side settings.Side) {
	icon, label := iconMaximize, locales.PanelMaximize()
	if a.panelMax == side {
		icon, label = iconMinimize, locales.PanelRestore()
	}
	if a.iconButton(c, icon, label, false).Clicked() {
		a.togglePanelMax(side)
	}
	if a.iconButton(c, iconX, locales.PanelHide(), false).Clicked() {
		a.hidePanel(side)
	}
}

// togglePanelMax has a panel take the editors' room, or give it back.
func (a *app) togglePanelMax(s settings.Side) {
	if a.panelMax == s {
		a.panelMax = ""
	} else {
		a.panelMax = s
	}
}

// panelWidth is how wide a panel on a side is: its own, or the room it
// takes when it is maximized.
func (a *app) panelWidth(s settings.Side) float32 {
	if a.panelMax == s {
		return panelWide
	}
	return a.window.Panels.Get(s).Size
}

// panelTab is a view's tab in the panel's header: its name, with a line
// under it while it shows, and a count after it when it has one.
func (a *app) panelTab(c *ui.Context, side settings.Side, v, label string, count int, on bool, show func()) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key("panel:"+string(side)+":"+v)).Height(panelHeadH).Padding(0, 6).
		Shrink(1).MinWidth(0).Justify(ui.Center).AlignItems(ui.Center).Label(label)
	col := p.muted
	switch {
	case on:
		col = p.text
	case b.Hovered():
		b.Background(p.hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).LetterSpacing(0.7).TextColor(col).SingleLine()
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(10).TextColor(p.muted).Margin(4, 0, 0, 0)
		}
		if on {
			ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(1.5).Background(p.accent)
		}
	})
	if b.Clicked() {
		show()
	}
	return b
}

// panelTabMenu is the menu of a tab of a panel's views: where that view
// shows, and the panel's own actions.
func (a *app) panelTabMenu(v string, side settings.Side, m *ui.Menu) {
	a.viewSideItems(v, m)
	m.Separator()
	label := locales.PanelMaximize()
	if a.panelMax == side {
		label = locales.PanelRestore()
	}
	if m.Item(label).Chosen() {
		a.post(func() { a.togglePanelMax(side) })
	}
	if m.Item(locales.PanelHide()).Chosen() {
		a.post(func() { a.hidePanel(side) })
	}
}

// panelTerminals is the terminal's view: its terminals' tabs, and the
// terminal shown, or the way to start one.
func (a *app) panelTerminals(c *ui.Context, side settings.Side, over dragItem) {
	p := a.pal
	terms := a.panelTerms()
	// MyGo's scroll bar takes the thumb's width plus six DIPs for its
	// track. Keep that whole hit area below overflowing chips; a strip
	// that fits needs no extra space.
	scrollbarH := float32(0)
	if a.panelScroll.MaxX > 0.5 {
		scrollbarH = c.Theme().ScrollbarWidth + 6
	}
	if len(terms) > 0 {
		ui.Row(c).Height(termStripH+scrollbarH).Padding(0, 8, 0, 8).Gap(2).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			// The terminals, as chips: a click shows one, and dragging one
			// to a column makes a tab of it.
			ui.ScrollHorizontal(c.Key("terms")).TrackScroll(&a.panelScroll).Grow(1).Height(termStripH+scrollbarH).Padding(0, 0, scrollbarH, 0).Children(func() {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					for i, s := range terms {
						chip := ui.ButtonBase(c.Key(s.id)).Padding(3, 8).Gap(6).Radius(5).AlignItems(ui.Center).Label(s.label()).
							Drag(dragItem{term: s}).Transition(tabMotion)
						if chip.Dragging() {
							a.dragNow = true
						}
						on := s == a.panelTerm
						under := p.panel
						if on {
							chip.Background(p.active)
							under = p.active.Over(p.panel)
						} else if chip.Hovered() {
							chip.Background(p.hover)
							under = p.hover.Over(p.panel)
						}
						chip.Children(func() {
							a.termIcon(c, s, 12, p.muted, under)
							col := p.muted
							if on {
								col = p.text
							}
							ui.Text(c, fmt.Sprintf("%d: %s", i+1, s.label())).FontSize(12).TextColor(col).SingleLine().MaxWidth(180)
						})
						if chip.Clicked() {
							a.panelTerm = s
							a.focusTerminal()
						}
						chip.ContextMenu(func(m *ui.Menu) {
							if m.Item(locales.TermMoveLockedRight()).Chosen() {
								a.post(func() { a.terminalToLockedRight(s) })
							}
							if m.Item(locales.TermMoveEditorArea()).Chosen() {
								a.post(func() { a.terminalToColumn(s, a.column(), a.column().active+1) })
							}
							if m.Item(locales.TermRename()).Chosen() {
								a.post(func() { a.askRenameTerminal(s) })
							}
							a.hcMenu(m, s)
							if m.Item(locales.TermKill()).Chosen() {
								a.post(func() { a.killTerminal(s) })
							}
						})
					}
				})
			})
			ui.Row(c).Gap(4).AlignItems(ui.Center).Margin(0, 0, scrollbarH, 0).Children(func() {
				if a.iconButton(c, iconPlus, a.withKey(locales.CmdViewNewTerminal(), "view.newTerminal"), false).Clicked() {
					a.newTerminal()
				}
				if s := a.panelTerm; s != nil {
					if a.iconButton(c, iconToEditor, locales.TermMoveToEditor(), false).Clicked() {
						a.post(func() { a.terminalToColumn(s, a.column(), a.column().active+1) })
					}
					if a.iconButton(c, iconTrash, locales.TermKill(), false).Clicked() {
						a.post(func() { a.killTerminal(s) })
					}
				}
			})
		})
	}
	term := ui.Box(c).Grow(1)
	// Files from another app type their paths into the terminal shown.
	filesOver := false
	if s := a.panelTerm; s != nil {
		if paths := term.DroppedFiles(); paths != nil {
			paths = slices.Clone(paths)
			a.post(func() { a.pastePaths(s, paths) })
		}
		filesOver = term.FileDragOver()
	}
	term.Children(func() {
		s := a.panelTerm
		if s == nil {
			ui.Column(c).Fill().Center().Children(func() {
				if ui.Button(c, locales.CmdViewNewTerminal()).Clicked() {
					a.newTerminal()
				}
			})
		} else {
			a.terminalView(c, s)
		}
		if over.tab != nil && over.tab.term != nil || filesOver {
			a.dropMark(c)
		}
	})
}

// terminalView shows a terminal, in the panel or in a tab: faded while it
// waits for its connection or its machine, as what it shows may be old,
// or while its shell starts, which it shows once attached.
func (a *app) terminalView(c *ui.Context, s *session) {
	ui.Box(c).Fill().Padding(2, 4, 4, 14).Background(a.pal.panel).Children(func() {
		v := terminal.View(c.Key(s.id), s.term).Fill().Bind(&s.view)
		shown := float32(1)
		if s.lost || s.hcStarting {
			shown = 0.55
		}
		if o := v.Animate("shown", shown, 220*time.Millisecond); o < 1 {
			v.Opacity(o)
		}
		a.waitPill(c, s)
		a.termFindView(c, s)
	})
}

// termFont is the terminals' font: the settings' own for the terminal,
// else the code's, a little smaller.
func termFont(s *settings.Settings) terminal.Font {
	f := terminal.Font{Family: s.TerminalFontFamily, Size: s.TerminalFontSize, Weight: s.FontWeight, Features: s.FontFeatures}
	if strings.TrimSpace(f.Family) == "" {
		f.Family = s.FontFamily
	}
	f.Family = monoFallback(f.Family)
	if f.Size == 0 {
		f.Size = max(s.FontSize-0.5, 9)
	}
	return f
}

// fontTerminals gives the terminals the font of the settings, once it
// changes.
func (a *app) fontTerminals() {
	f := termFont(a.settings)
	if sameTermFont(f, a.termFont) {
		return
	}
	a.termFont = f
	for _, s := range a.terms {
		s.term.SetFont(f)
	}
}

// copyTerminals has the terminals copy what is selected, or not, as the
// settings say, once they change.
func (a *app) copyTerminals() {
	on := a.prefs().TerminalCopyOnSelect
	if on == a.termCopy {
		return
	}
	a.termCopy = on
	for _, s := range a.terms {
		s.term.SetCopyOnSelect(copyOnSelect(on))
	}
}

// copyOnSelect is where the terminal copies a selection when the setting
// is on or off.
func copyOnSelect(on bool) terminal.CopyOnSelect {
	if on {
		return terminal.CopyOnSelectClipboard
	}
	return terminal.CopyOnSelectNone
}

func sameTermFont(a, b terminal.Font) bool {
	return a.Family == b.Family && a.Size == b.Size && a.Weight == b.Weight && slices.Equal(a.Features, b.Features)
}
