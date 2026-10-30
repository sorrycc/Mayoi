package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/workspace"
	"mayoi/locales"
)

// paletteEntry is a command as the command palette lists it: its title,
// as "View: Toggle Sidebar", and the keys of its shortcut. en is its title
// in English, which the palette matches too, when the language shown is
// another.
type paletteEntry struct {
	id, title, en string
	group         int // the length of the title's "View: "
	caps          []string
}

// commandGroup names the menu of a command, by the prefix of its id, in
// the language of t.
func commandGroup(t locales.Messages, id string) string {
	switch {
	case strings.HasPrefix(id, "view.appearance."), strings.HasPrefix(id, "view.theme."):
		return t.MenuTheme()
	case strings.HasPrefix(id, localeCommand):
		return t.MenuLanguage()
	}
	switch id[:strings.IndexByte(id, '.')] {
	case "file":
		return t.MenuFile()
	case "edit":
		return t.MenuEdit()
	case "sel":
		return t.MenuSelection()
	case "go":
		return t.MenuGo()
	case "view", "editor", "image":
		return t.MenuView()
	case "git":
		return t.MenuGit()
	case "review":
		return t.MenuReview()
	case "account":
		return t.MenuAccount()
	}
	return ""
}

// viewKeys are the keys of the commands that the editor or the view
// takes itself, which their menu items leave out, for the palette to show.
var viewKeys = map[string]string{
	"edit.undo": "CmdOrCtrl+Z", "edit.redo": "Shift+CmdOrCtrl+Z",
	"sel.moveUp": "Alt+Up", "sel.moveDown": "Alt+Down",
	"sel.copyUp": "Shift+Alt+Up", "sel.copyDown": "Shift+Alt+Down",
	"sel.lineBelow": "CmdOrCtrl+Enter", "sel.lineAbove": "Shift+CmdOrCtrl+Enter",
	"file.openRecent": "Ctrl+R",
}

// paletteEntries are the commands the palette runs, by title, made once
// for the window, and again when the language changes: the commands do
// not change.
func (a *app) paletteEntries() []paletteEntry {
	if a.palette != nil {
		return a.palette
	}
	shown, en := locales.In(i18n.Locale()), locales.In("en")
	english := map[string]string{}
	for _, cmd := range a.commandsIn(en) {
		english[cmd.id] = commandGroup(en, cmd.id) + ": " + cmd.label
	}
	for _, cmd := range a.commandsIn(shown) {
		if cmd.id == "go.commands" {
			continue
		}
		group := commandGroup(shown, cmd.id)
		title := group + ": " + cmd.label
		e := paletteEntry{id: cmd.id, title: title, group: len(group) + 2, caps: a.commandCaps(cmd)}
		if english[cmd.id] != title {
			e.en = english[cmd.id]
		}
		a.palette = append(a.palette, e)
	}
	slices.SortStableFunc(a.palette, func(x, y paletteEntry) int { return strings.Compare(x.title, y.title) })
	return a.palette
}

// commandOn reports whether a command that turns something on or off,
// or picks a theme, has it on.
func (a *app) commandOn(id string) bool {
	s := a.prefs()
	switch id {
	case "file.autoSave":
		return s.AutoSave
	case "view.wordWrap":
		return s.WordWrap
	case "view.vim":
		return s.Vim
	case "view.sidebarRight":
		return s.SidebarRight
	case "view.terminalLocation":
		return s.TerminalLocation == "editor"
	case "git.blame":
		return s.Blame
	case "view.appearance.system":
		return s.Appearance == ""
	case "view.theme.mayoi":
		return s.Theme == ""
	}
	if pos, ok := strings.CutPrefix(id, "view.panelPosition."); ok {
		return a.panelSide() == pos
	}
	if tag, ok := strings.CutPrefix(id, localeCommand); ok {
		return a.localeOn(tag)
	}
	if ap, ok := strings.CutPrefix(id, "view.appearance."); ok {
		return s.Appearance == ap
	}
	if theme, ok := strings.CutPrefix(id, "view.theme."); ok {
		return s.Theme == theme
	}
	return false
}

// keyNames are the caps of the keys named in accelerators that have
// signs of their own: macOS's sign, and the name elsewhere.
var keyNames = map[string][2]string{
	"Left": {"←", "Left"}, "Right": {"→", "Right"}, "Up": {"↑", "Up"}, "Down": {"↓", "Down"},
	"Tab": {"⇥", "Tab"}, "Enter": {"⏎", "Enter"}, "Return": {"⏎", "Enter"},
	"Escape": {"esc", "Esc"}, "Backspace": {"⌫", "Backspace"}, "Delete": {"⌦", "Delete"},
}

// accelCaps returns the keys of an accelerator as the caps of a keyboard:
// as macOS shows them with mac, as ⇧⌘P, and otherwise by name.
func accelCaps(accel string, mac bool) []string {
	if accel == "" {
		return nil
	}
	var ctrl, alt, shift, cmd bool
	key := ""
	// The key may be "+" itself, after the modifiers.
	parts := strings.Split(accel, "+")
	if strings.HasSuffix(accel, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	for _, p := range parts {
		switch p {
		case "CmdOrCtrl", "CommandOrControl":
			if mac {
				cmd = true
			} else {
				ctrl = true
			}
		case "Cmd", "Command", "Super", "Meta":
			cmd = true
		case "Ctrl", "Control":
			ctrl = true
		case "Alt", "Option":
			alt = true
		case "Shift":
			shift = true
		default:
			key = p
		}
	}
	var caps []string
	if mac {
		for _, m := range []struct {
			on  bool
			sym string
		}{{ctrl, "⌃"}, {alt, "⌥"}, {shift, "⇧"}, {cmd, "⌘"}} {
			if m.on {
				caps = append(caps, m.sym)
			}
		}
	} else {
		for _, m := range []struct {
			on   bool
			name string
		}{{ctrl, "Ctrl"}, {alt, "Alt"}, {shift, "Shift"}, {cmd, superName()}} {
			if m.on {
				caps = append(caps, m.name)
			}
		}
	}
	if n, ok := keyNames[key]; ok {
		if mac {
			key = n[0]
		} else {
			key = n[1]
		}
	}
	return append(caps, key)
}

// rememberCommand puts a command first among those run last, which the
// palette lists first.
func (a *app) rememberCommand(id string) {
	q := &a.quick
	q.used = slices.DeleteFunc(q.used, func(u string) bool { return u == id })
	q.used = slices.Insert(q.used, 0, id)
	q.used = q.used[:min(len(q.used), 8)]
}

// quickCommands is quick open running a command by name: those run last
// first until a name is typed, then the others by title.
func (a *app) quickCommands(c *ui.Context, query string) {
	q := &a.quick
	p := a.pal
	entries := a.paletteEntries()
	key := ">" + query
	if q.stale || q.shown != key {
		q.results = paletteMatches(entries, strings.TrimSpace(query))
		if strings.TrimSpace(query) == "" {
			var first []workspace.Match
			for _, id := range q.used {
				if i := slices.IndexFunc(entries, func(e paletteEntry) bool { return e.id == id }); i >= 0 {
					first = append(first, workspace.Match{Index: i})
				}
			}
			rest := slices.DeleteFunc(q.results, func(m workspace.Match) bool {
				return slices.ContainsFunc(first, func(f workspace.Match) bool { return f.Index == m.Index })
			})
			q.results = append(first, rest...)
		}
		q.shown, q.stale, q.sel = key, false, 0
	}
	choose := func(i int) {
		if i < 0 || i >= len(q.results) {
			q.shook = c.Now()
			return
		}
		id := entries[q.results[i].Index].id
		a.rememberCommand(id)
		a.closeQuick()
		a.post(func() { a.run(id) })
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "quick")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(600).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip()
			shake(c, box, q.shook)
			box.
				Role(ui.RoleDialog).Label(locales.PaletteLabel())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconCommand).FontSize(15).TextColor(p.muted)
					in := a.textKeys(ui.TextInputBase(c.Key("quick-input"), &q.query)).Grow(1).FontSize(14).AutoFocus().
						Placeholder(locales.PalettePlaceholder()).Label(locales.PaletteLabel())
					if in.Shortcut(0, ui.KeyDown) {
						q.sel, q.moved = min(q.sel+1, len(q.results)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						q.sel, q.moved = max(q.sel-1, 0), true
					}
					if in.Submitted() {
						choose(q.sel)
					}
				})
				if len(q.results) == 0 {
					ui.Text(c, locales.PaletteNoMatches()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				} else {
					ui.Scroll(c.Key("quick-commands")).MaxHeight(380).Padding(6).Children(func() {
						for i, m := range q.results {
							e := entries[m.Index]
							// As tall with keys as without.
							row := ui.ButtonBase(c.Key("cmd:"+e.id)).MinHeight(30).Padding(5, 10).Gap(10).Radius(6).
								Justify(ui.Start).AlignItems(ui.Center).Label(e.title)
							if i != q.sel && row.Hovered() {
								row.Background(p.hover)
							}
							if i == q.sel && q.moved {
								row.ScrollIntoView()
							}
							if row.Clicked() {
								choose(i)
							}
							row.Transition(hoverFade)
							row.Children(func() { a.pickMark(c, i == q.sel, "commands"); a.commandRow(c, e, m.Positions) })
						}
					})
				}
				q.moved = false
				a.quickFooter(c, false, locales.QuickRunAction(), "", false, false)
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeQuick()
			}
		})
	})
}

// paletteMatches are the commands that query matches, best first: by their
// titles, or by their English ones, whose matched characters do not show.
func paletteMatches(entries []paletteEntry, query string) []workspace.Match {
	titles := make([]string, len(entries))
	english := make([]string, len(entries))
	other := false
	for i, e := range entries {
		titles[i], english[i] = e.title, e.en
		other = other || e.en != ""
	}
	matches := workspace.Fuzzy(titles, query, len(entries))
	if !other || query == "" {
		return matches
	}
	found := map[int]bool{}
	for _, m := range matches {
		found[m.Index] = true
	}
	for _, m := range workspace.Fuzzy(english, query, len(entries)) {
		if !found[m.Index] && english[m.Index] != "" {
			m.Positions = nil
			matches = append(matches, m)
		}
	}
	slices.SortStableFunc(matches, func(x, y workspace.Match) int { return y.Score - x.Score })
	return matches
}

// commandRow shows a command of the results: its menu, its name with the
// characters the query matched in the accent color, a check when it is
// on, and its keys.
func (a *app) commandRow(c *ui.Context, e paletteEntry, positions []int) {
	p := a.pal
	spans := append(a.matchSpans(e.title[:e.group], 0, positions, p.muted), a.matchSpans(e.title[e.group:], e.group, positions, p.text)...)
	ui.RichText(c, spans...).FontSize(13).SingleLine().Shrink(1).Grow(1).MinWidth(0)
	if a.commandOn(e.id) {
		ui.Icon(c, iconCheck).FontSize(14).TextColor(p.accent)
	}
	if len(e.caps) > 0 {
		a.caps(c, e.caps)
	}
}
