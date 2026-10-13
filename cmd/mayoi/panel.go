package main

import (
	"fmt"
	"mayoi/internal/keymap"
	"mayoi/internal/remotefs"
	"mayoi/internal/settings"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
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
	light, dark := a.termThemes()
	localDir := dir
	var command []string
	if program, args := remotefs.Terminal(dir); program != "" {
		localDir = ""
		command = append([]string{program}, args...)
		target, _, _ := remotefs.Split(dir)
		s.title = "SSH: " + target.Host
	}
	term, err := terminal.New(terminal.Options{
		Command:   command,
		Dir:       localDir,
		Font:      termFont(a.settings),
		Theme:     light,
		DarkTheme: dark,
		Cursor:    terminal.CursorBar,
		OnTitle: func(title string) {
			a.post(func() { s.title = title })
		},
		// The shortcuts of keybindings.json, before the shell.
		OnKey: func(mods ui.Modifiers, key ui.Key) bool {
			a.keyTerm = s
			return a.keyStroke(keymap.Stroke{Mods: mods, Key: key}, placeTerminal)
		},
		OnExit: func(int) {
			a.post(func() { a.removeTerminal(s) })
		},
	})
	if err != nil {
		a.fail("Could not start a terminal: %v", err)
		return nil
	}
	s.term = term
	a.terms = append(a.terms, s)
	return s
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

func shellName() string {
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

// panel is the terminal panel: a header of its terminals and its tools,
// and the terminal shown. A terminal dragged from a tab is dropped on it,
// and a file dragged from the sidebar types its path.
func (a *app) panel(c *ui.Context) {
	p := a.pal
	terms := a.panelTerms()
	box := ui.Column(c).Fill().Background(p.panel)
	if it, ok := ui.Drop[dragItem](box); ok {
		switch s := a.panelTerm; {
		case it.tab != nil && it.tab.term != nil:
			a.post(func() { a.terminalToPanel(it.tab.term) })
		case it.file != nil && s != nil:
			// A file of the sidebar types its path, as one from the Finder.
			a.post(func() { a.pastePaths(s, []string{it.file.path}) })
		}
	}
	over, _ := ui.DragOver[dragItem](box)
	box.Children(func() {
		ui.Row(c).Height(panelHeadH).Padding(0, 8, 0, 14).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Height(panelHeadH).Justify(ui.Center).Children(func() {
				ui.Text(c, "TERMINAL").FontSize(11).FontWeight(600).LetterSpacing(0.7).TextColor(p.text)
				ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(1.5).Background(p.accent)
			})
			ui.Box(c).Width(10)
			// The terminals, as chips: a click shows one, and dragging one
			// to a column makes a tab of it.
			ui.ScrollHorizontal(c.Key("terms")).Grow(1).Children(func() {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					for i, s := range terms {
						chip := ui.ButtonBase(c.Key(s.id)).Padding(3, 8).Gap(6).Radius(5).AlignItems(ui.Center).Label(s.title).
							Drag(dragItem{term: s})
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
							if m.Item("Move to Editor Area").Chosen() {
								a.post(func() { a.terminalToColumn(s, a.column(), a.column().active+1) })
							}
							if m.Item("Kill Terminal").Chosen() {
								a.post(func() { a.removeTerminal(s) })
							}
						})
					}
				})
			})
			if a.iconButton(c, iconPlus, "New Terminal (⌃⇧`)", false).Clicked() {
				a.newTerminal()
			}
			if s := a.panelTerm; s != nil {
				if a.iconButton(c, iconToEditor, "Move Terminal to Editor Area", false).Clicked() {
					a.post(func() { a.terminalToColumn(s, a.column(), a.column().active+1) })
				}
				if a.iconButton(c, iconTrash, "Kill Terminal", false).Clicked() {
					a.post(func() { a.removeTerminal(s) })
				}
			}
			icon, label := iconMaximize, "Maximize Panel"
			if a.panelMax {
				icon, label = iconMinimize, "Restore Panel"
			}
			if a.iconButton(c, icon, label, false).Clicked() {
				a.panelMax = !a.panelMax
			}
			if a.iconButton(c, iconX, "Hide Panel", false).Clicked() {
				a.hidePanel()
			}
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
					if ui.Button(c, "New Terminal").Clicked() {
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

func sameTermFont(a, b terminal.Font) bool {
	return a.Family == b.Family && a.Size == b.Size && a.Weight == b.Weight && slices.Equal(a.Features, b.Features)
}
