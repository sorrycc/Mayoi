package main

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/locales"
)

// hostOS is the system Mayoi runs on, as runtime.GOOS names it: its keys,
// and the names of its file manager, follow it.
var hostOS = runtime.GOOS

// macKeys reports whether the shortcuts are macOS's, with Command, rather
// than those of Windows and Linux, with Control, as VS Code has them.
func macKeys() bool { return hostOS == "darwin" }

// otherAccels are the shortcuts of commands on Windows and Linux where
// they are not macOS's with Control for Command: VS Code's, "" for none.
// Control and Alt together are AltGr there, which types text.
var otherAccels = map[string]string{
	"edit.findNext":         "F3",
	"edit.findPrev":         "Shift+F3",
	"edit.replace":          "Ctrl+H",
	"file.saveAll":          "",
	"file.openFolderWindow": "",
	"view.nextColumn":       "",
	"view.prevColumn":       "",
}

// platformAccel is a command's accelerator on the system: on Windows and
// Linux its own there, or Control for Command.
func platformAccel(id, accel string) string {
	if macKeys() {
		return accel
	}
	if o, ok := otherAccels[id]; ok {
		return o
	}
	accel = strings.ReplaceAll(accel, "CommandOrControl", "Ctrl")
	return strings.ReplaceAll(accel, "CmdOrCtrl", "Ctrl")
}

// tabMod is the modifier that, with a digit, shows a tab of the column:
// Command on macOS, Alt elsewhere, as in VS Code.
func tabMod() ui.Modifiers {
	if macKeys() {
		return ui.Super
	}
	return ui.Alt
}

// keyText is an accelerator as its keys read: "⇧⌘P" on macOS, "Ctrl+Shift+P"
// elsewhere.
func keyText(accel string) string {
	return capsText(accelCaps(platformAccel("", accel), macKeys()))
}

// capsText joins the caps of a stroke as the system writes them.
func capsText(caps []string) string {
	if macKeys() {
		return strings.Join(caps, "")
	}
	return strings.Join(caps, "+")
}

// withKey is label with the keys of command id after it, in parentheses,
// as a tooltip shows them: the label alone when it has none.
func (a *app) withKey(label, id string) string {
	if keys := a.keyHint(id); keys != "" {
		return label + " (" + keys + ")"
	}
	return label
}

// keyHint is the keys of command id as they read, "" for none, kept for
// the window until the shortcuts change.
func (a *app) keyHint(id string) string {
	if a.hints == nil {
		a.hints = map[string]string{}
		for _, cmd := range a.commands() {
			caps := a.commandCaps(cmd)
			if len(a.keys.Last(commandNames(cmd.id)...)) > 1 {
				a.hints[cmd.id] = strings.Join(caps, " ")
			} else {
				a.hints[cmd.id] = capsText(caps)
			}
		}
	}
	return a.hints[id]
}

// commandCaps are the keys of a command as caps, each stroke's apart for
// a chord: those keybindings.json gives it, or its menu's, or those the
// view takes itself.
func (a *app) commandCaps(cmd command) []string {
	if keys := a.keys.Last(commandNames(cmd.id)...); len(keys) == 1 {
		return accelCaps(keys[0].Accel(), macKeys())
	} else if keys != nil {
		return chordCaps(keys)
	}
	accel := a.menuAccel(cmd)
	if accel == "" && cmd.accel == "" {
		accel = platformAccel(cmd.id, viewKeys[cmd.id])
	}
	return accelCaps(accel, macKeys())
}

// menuKeys runs the commands of the menus' shortcuts on Windows and Linux,
// where the menu bar of a window of native UI does not: those the focused
// element did not take.
func (a *app) menuKeys(c *ui.Context) {
	if macKeys() {
		return
	}
	for _, cmd := range a.commands() {
		st, ok := keymap.ParseAccel(a.menuAccel(cmd))
		if ok && c.Shortcut(st.Mods, st.Key) {
			a.run(cmd.id)
		}
	}
}

// shellSkips are the commands whose shortcuts a terminal leaves to Mayoi
// on Windows and Linux, as VS Code's commandsToSkipShell: the shell has
// the other keys with Control, which edit its line.
var shellSkips = map[string]bool{
	"go.quickOpen": true, "go.commands": true, "go.nextTab": true, "go.prevTab": true,
	"view.terminal": true, "view.newTerminal": true, "view.sidebar": true,
	"view.zoomIn": true, "view.zoomOut": true, "view.zoomReset": true,
}

// shellSkip returns the command a terminal leaves stroke s to, on Windows
// and Linux: one of shellSkips, or any with Control and Shift but Copy and
// Paste's, or with a function key. ok is false for none.
func (a *app) shellSkip(s keymap.Stroke) (id string, ok bool) {
	if macKeys() {
		return "", false
	}
	if s.Mods == tabMod() && s.Key >= ui.Key1 && s.Key <= ui.Key9 {
		return "", true
	}
	for _, cmd := range a.commands() {
		st, ok := keymap.ParseAccel(a.menuAccel(cmd))
		if !ok || st != s {
			continue
		}
		ctrlShift := s.Mods&(ui.Ctrl|ui.Shift) == ui.Ctrl|ui.Shift && s.Key != ui.KeyC && s.Key != ui.KeyV
		fkey := s.Key >= ui.KeyF1 && s.Key <= ui.KeyF12
		if shellSkips[cmd.id] || ctrlShift || fkey {
			return cmd.id, true
		}
	}
	return "", false
}

// fileManager is the app that shows files of the system: the Finder, File
// Explorer, or a file manager.
func fileManager(t locales.Messages) string {
	switch hostOS {
	case "darwin":
		return t.FileManagerFinder()
	case "windows":
		return t.FileManagerExplorer()
	}
	return t.FileManagerOther()
}

// revealLabel is the menu item that shows a file in the file manager.
func revealLabel() string {
	t := locales.In(i18n.Locale())
	if hostOS == "linux" {
		return t.RevealOpenContaining()
	}
	return t.RevealIn(fileManager(t))
}

// trashName is where files go that are deleted: the Recycle Bin on
// Windows, the Trash elsewhere.
func trashName() string {
	if hostOS == "windows" {
		return locales.TrashNameRecycleBin()
	}
	return locales.TrashNameTrash()
}

// trashLabel is the menu item that moves a file to trashName.
func trashLabel() string {
	if hostOS == "windows" {
		return locales.TrashLabelDelete()
	}
	return locales.TrashLabelMove()
}

// revealActiveLabel is the command that shows the active file in the file
// manager, in the language of t.
func revealActiveLabel(t locales.Messages) string {
	if hostOS == "linux" {
		return t.RevealActiveOpenContaining()
	}
	return t.RevealActiveIn(fileManager(t))
}

// vimCtrl reports whether vim's normal mode takes ⌃F, ⌃B and ⌃R: on
// macOS, where shortcuts are Command's, and elsewhere with vimCtrlKeys,
// rather than Find, Toggle Sidebar and Open Recent.
func (a *app) vimCtrl() bool { return macKeys() || a.prefs().VimCtrlKeys }

// vimTakesCtrl reports whether the editor with the keyboard takes vim's
// keys with Control, as ⌃R.
func (a *app) vimTakesCtrl(c *ui.Context) bool { return a.vimTakesKeys(c) && a.vimCtrl() }

// superName is the key of the Super modifier off macOS, as its keyboards
// name it.
func superName() string {
	if hostOS == "windows" {
		return "Win"
	}
	return "Super"
}

// revealFolderLabel is the tooltip of the folder that shows it in the file
// manager.
func revealFolderLabel(folder string) string {
	t := locales.In(i18n.Locale())
	if hostOS == "linux" {
		return t.RevealFolderOpen(folder)
	}
	return t.RevealFolderIn(folder, fileManager(t))
}
