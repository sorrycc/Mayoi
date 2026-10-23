package main

import (
	"fmt"
	"mayoi/internal/keymap"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

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
}

type panelHeaderLayout struct {
	windowWidth, panelWidth float32
	side, titles, font      string
}

// termTheme is the terminal's colors for a palette: its background the
// panel's, so that the panel and the editor are of a piece.
func termTheme(p *palette) *terminal.Theme {
	return &terminal.Theme{
		Foreground: p.text, Background: p.panel,
		Cursor: p.accent, Selection: p.selection,
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
	a.nextTerm++
	s := &session{id: a.nextTerm, dir: dir, title: shellName()}
	if c := sessionConn(s); c != nil {
		s.title = c.Host
	}
	term, err := a.spawnTerminal(s, dir)
	if err != nil {
		a.fail("%s", locales.FailStartTerminal(err.Error()))
		return nil
	}
	s.term = term
	a.terms = append(a.terms, s)
	return s
}

// spawnTerminal starts the program of a terminal in dir: the shell, of
// the host in a remote folder.
func (a *app) spawnTerminal(s *session, dir string) (*terminal.Terminal, error) {
	light, dark := a.termThemes()
	// In a remote folder, a shell of its host, over the connection.
	var command []string
	if c, rdir, ok := remote.Lookup(dir); ok {
		command = c.TerminalCommand(rdir)
		dir, _ = os.UserHomeDir()
	}
	// term is the terminal made, which OnExit tells from one that took
	// its place.
	var term *terminal.Terminal
	term, err := terminal.New(terminal.Options{
		Command:   command,
		Dir:       dir,
		Font:      termFont(a.settings),
		Theme:     light,
		DarkTheme: dark,
		Cursor:    terminal.CursorBar,
		// Selecting copies, as in Ghostty and iTerm2.
		CopyOnSelect: copyOnSelect(a.settings.TerminalCopyOnSelect),
		OnTitle: func(title string) {
			a.post(func() { s.title = title })
		},
		// A terminal of a host gets files and images pasted as copies
		// uploaded to it.
		OnPaste: func(d mtransfer.Data) bool {
			text, _ := d.Read(mtransfer.Text)
			return a.terminalPaste(s, string(text))
		},
		// A link to a port of the host opens the port forwarded to it.
		OpenLink: func(link string) bool { return a.terminalLink(s, link) },
		// The shortcuts of keybindings.json, before the shell, and on
		// Windows and Linux those of Mayoi's it leaves to Mayoi.
		OnKey: func(mods ui.Modifiers, key ui.Key) bool {
			a.keyTerm = s
			st := keymap.Stroke{Mods: mods, Key: key}
			if a.keyStroke(st, placeTerminal) {
				return true
			}
			id, ok := a.shellSkip(st)
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
		// The wheel with Command held zooms no terminal on its own: Cmd+=
		// and Cmd+- size all the text, and Command held to switch apps
		// scrolls no terminal's font away.
		OnZoom: func(terminal.Zoom) bool { return true },
		OnExit: func(code int) {
			a.post(func() { a.terminalExited(s, term, code) })
		},
		// A program done in a project out of sight, as a build or an
		// agent, has the rail mark the project's square.
		OnBell: func() {
			a.post(func() { a.alert = locales.BellFrom(s.title) })
		},
		OnNotify: func(title, body string) {
			a.post(func() {
				a.alert = strings.TrimSpace(title + " " + body)
				if a.alert == "" {
					a.alert = locales.NotificationFrom(s.title)
				}
			})
		},
	})
	return term, err
}

// newTerminal starts a terminal in the panel and shows it.
func (a *app) newTerminal() { a.newTerminalIn(a.workDir()) }

// newTerminalIn starts a terminal of the panel in dir and shows it.
func (a *app) newTerminalIn(dir string) {
	s := a.startTerminal(dir)
	if s == nil {
		return
	}
	a.panelTerm = s
	a.window.PanelOpen, a.window.TerminalOpen = true, true
	a.settings.Changed()
	a.focusTerminal()
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
	s := a.startTerminal(dir)
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
		a.window.PanelOpen, a.window.TerminalOpen = false, false
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
	a.window.PanelOpen, a.window.TerminalOpen = true, true
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

// hidePanel hides the panel, and gives the keyboard back to the column.
func (a *app) hidePanel() {
	a.window.PanelOpen = false
	a.settings.Changed()
	a.focusColumn()
}

// panelMenu is the menu of the panel's header: where the panel sits,
// and its size.
func (a *app) panelMenu(m *ui.Menu) {
	m.Submenu(locales.MenuPanelPosition(), func(m *ui.Menu) {
		for _, pos := range []struct{ label, id, side string }{
			{locales.PanelBottom(), "", "bottom"},
			{locales.PanelLeft(), "left", "left"},
			{locales.PanelRight(), "right", "right"},
		} {
			if m.Item(pos.label).Checked(a.panelSide() == pos.side).Chosen() {
				a.post(func() { a.setPanelPosition(pos.id) })
			}
		}
	})
	m.Separator()
	label := locales.PanelMaximize()
	if a.panelMax {
		label = locales.PanelRestore()
	}
	if m.Item(label).Chosen() {
		a.post(func() { a.panelMax = !a.panelMax })
	}
	if m.Item(locales.PanelHide()).Chosen() {
		a.post(a.hidePanel)
	}
}

// panel is the terminal panel: a header of its terminals and its tools,
// and the terminal shown. A terminal dragged from a tab is dropped on it,
// and a file dragged from the sidebar types its path.
func (a *app) panel(c *ui.Context) {
	p := a.pal
	terms := a.panelTerms()
	// MyGo's scroll bar takes the thumb's width plus six DIPs for its
	// track. Keep that whole hit area below overflowing chips; a strip
	// that fits needs no extra space.
	scrollbarH := float32(0)
	if a.panelScroll.MaxX > 0.5 {
		scrollbarH = c.Theme().ScrollbarWidth + 6
	}
	box := ui.Column(c).Fill().Background(p.panel)
	if it, ok := ui.Drop[dragItem](box); ok {
		switch s := a.panelTerm; {
		case it.tab != nil && it.tab.term != nil && a.owns(it):
			a.post(func() { a.terminalToPanel(it.tab.term) })
		case it.file != nil && s != nil:
			// A file of the sidebar types its path, as one from the Finder.
			a.post(func() { a.pastePaths(s, []string{it.file.path}) })
		}
	}
	over, _ := ui.DragOver[dragItem](box)
	// A panel at a side, dragged narrow, leaves out its title so that
	// its terminals and its tools keep their room.
	width := box.Bounds().W
	var titles strings.Builder
	for _, s := range terms {
		titles.WriteString(s.title)
		titles.WriteByte(0)
	}
	windowWidth, _ := c.Size()
	layout := panelHeaderLayout{windowWidth, width, a.panelSide(), titles.String(), c.Theme().Font}
	if layout != a.panelLayout {
		a.panelLayout = layout
		c.AnimationFrame()
	}
	titled := a.panelSide() == "bottom" || width == 0 || width >= panelTitleW
	box.Children(func() {
		head := ui.Row(c).Height(panelHeadH+scrollbarH).Padding(0, 8, 0, 14).Gap(4).AlignItems(ui.Center)
		head.ContextMenu(a.panelMenu)
		head.Children(func() {
			if titled {
				ui.Column(c).Height(panelHeadH).Margin(0, 0, scrollbarH, 0).Justify(ui.Center).Children(func() {
					ui.Text(c, strings.ToUpper(locales.TabTerminal())).FontSize(11).FontWeight(600).LetterSpacing(0.7).TextColor(p.text)
					ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(1.5).Background(p.accent)
				})
				ui.Box(c).Width(10)
			}
			// The terminals, as chips: a click shows one, and dragging one
			// to a column makes a tab of it.
			ui.ScrollHorizontal(c.Key("terms")).TrackScroll(&a.panelScroll).Grow(1).Height(panelHeadH+scrollbarH).Padding(0, 0, scrollbarH, 0).Children(func() {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					for i, s := range terms {
						chip := ui.ButtonBase(c.Key(s.id)).Padding(3, 8).Gap(6).Radius(5).AlignItems(ui.Center).Label(s.title).
							Drag(dragItem{term: s}).Transition(tabMotion)
						if chip.Dragging() {
							a.dragNow = true
						}
						on := s == a.panelTerm
						if on {
							chip.Background(p.active)
						} else if chip.Hovered() {
							chip.Background(p.hover)
						}
						chip.Children(func() {
							ui.Icon(c, iconTerminal).FontSize(12).TextColor(p.muted)
							col := p.muted
							if on {
								col = p.text
							}
							ui.Text(c, fmt.Sprintf("%d: %s", i+1, s.title)).FontSize(12).TextColor(col).SingleLine().MaxWidth(180)
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
							if m.Item(locales.TermKill()).Chosen() {
								a.post(func() { a.removeTerminal(s) })
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
						a.post(func() { a.removeTerminal(s) })
					}
				}
				icon, label := iconMaximize, locales.PanelMaximize()
				if a.panelMax {
					icon, label = iconMinimize, locales.PanelRestore()
				}
				if a.iconButton(c, icon, label, false).Clicked() {
					a.panelMax = !a.panelMax
				}
				if a.iconButton(c, iconX, locales.PanelHide(), false).Clicked() {
					a.hidePanel()
				}
			})
		})
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
	})
}

// terminalView shows a terminal, in the panel or in a tab.
func (a *app) terminalView(c *ui.Context, s *session) {
	ui.Box(c).Fill().Padding(2, 4, 4, 14).Background(a.pal.panel).Children(func() {
		terminal.View(c.Key(s.id), s.term).Fill().Bind(&s.view)
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
	on := a.settings.TerminalCopyOnSelect
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
