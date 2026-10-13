package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
)

// onOS has the test run as Mayoi does on goos, its keys and its names.
func onOS(t *testing.T, goos string) {
	old := hostOS
	hostOS = goos
	t.Cleanup(func() { hostOS = old })
}

// No two commands share a key on any system, and none takes Control and
// Alt off macOS, which are AltGr there and type text.
func TestShortcutsDoNotCollide(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		t.Run(goos, func(t *testing.T) {
			onOS(t, goos)
			a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
			taken := map[keymap.Stroke]string{}
			if goos != "darwin" {
				for k := ui.Key1; k <= ui.Key9; k++ {
					taken[keymap.Stroke{Mods: ui.Alt, Key: k}] = "a tab"
				}
			}
			taken[keymap.Stroke{Mods: ui.Ctrl, Key: ui.KeyR}] = "file.openRecent"
			for _, cmd := range a.commands() {
				accel := cmd.accel
				if goos == "darwin" {
					// Command, also on the system the test runs on.
					accel = strings.ReplaceAll(accel, "CmdOrCtrl", "Cmd")
				}
				st, ok := keymap.ParseAccel(accel)
				if !ok {
					if accel != "" {
						t.Errorf("%s: %q does not parse", cmd.id, accel)
					}
					continue
				}
				if other, ok := taken[st]; ok {
					t.Errorf("%s and %s both take %s", cmd.id, other, accel)
				}
				taken[st] = cmd.id
				if goos != "darwin" && st.Mods&(ui.Ctrl|ui.Alt) == ui.Ctrl|ui.Alt {
					t.Errorf("%s takes %s, which is AltGr", cmd.id, accel)
				}
			}
		})
	}
}

// Off macOS the commands take VS Code's keys of the system, which their
// tooltips show as it writes them.
func TestWindowsKeyHints(t *testing.T) {
	onOS(t, "windows")
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	for id, want := range map[string]string{
		"go.commands": "Ctrl+Shift+P", "edit.findNext": "F3", "edit.findPrev": "Shift+F3",
		"edit.replace": "Ctrl+H", "go.line": "Ctrl+G", "view.scm": "Ctrl+Shift+G", "file.saveAll": "",
		"view.terminal": "Ctrl+`", "edit.redo": "Ctrl+Shift+Z",
	} {
		if got := a.keyHint(id); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	if got := a.withKey("Toggle Sidebar", "view.sidebar"); got != "Toggle Sidebar (Ctrl+B)" {
		t.Errorf("tooltip %q", got)
	}
	if got := keyText("CmdOrCtrl+Enter"); got != "Ctrl+Enter" {
		t.Errorf("keyText %q", got)
	}
	if got := capsText(accelCaps("Cmd+K", false)); got != "Win+K" {
		t.Errorf("the Windows key reads %q", got)
	}
	onOS(t, "darwin")
	a.hints = nil
	if got := a.withKey("Toggle Sidebar", "view.sidebar"); got != "Toggle Sidebar (⌘B)" {
		t.Errorf("macOS tooltip %q", got)
	}
}

// Off macOS Mayoi runs the menus' shortcuts itself, which a window of
// native UI's menu bar does not.
func TestWindowsMenuKeys(t *testing.T) {
	onOS(t, "windows")
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	open := a.settings.SidebarOpen
	tt.Key(ui.Ctrl, ui.KeyB)
	hs.settle()
	if a.settings.SidebarOpen == open {
		t.Error("Ctrl+B did not toggle the sidebar")
	}
	tt.Key(ui.Ctrl, ui.KeyP)
	hs.settle()
	if !a.quick.open {
		t.Error("Ctrl+P did not open quick open")
	}
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	// Before a file opens: the editor of the macOS the test may run on
	// deletes with ⌃H, as Emacs does.
	tt.Key(ui.Ctrl, ui.KeyH)
	hs.settle()
	if !a.find.replacing {
		t.Error("Ctrl+H did not open replace")
	}
	a.find.open, a.find.replacing = false, false
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	tt.Key(0, ui.KeyF3)
	hs.settle()
	if !a.find.open {
		t.Error("F3 in the editor did not find the next match")
	}
}

// A terminal off macOS leaves Mayoi the keys of the workbench, and the
// shell the others with Control, which edit its line.
func TestShellSkip(t *testing.T) {
	onOS(t, "linux")
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	for _, c := range []struct {
		mods ui.Modifiers
		key  ui.Key
		id   string
		ok   bool
	}{
		{ui.Ctrl, ui.KeyP, "go.quickOpen", true},
		{ui.Ctrl | ui.Shift, ui.KeyP, "go.commands", true},
		{ui.Ctrl | ui.Shift, ui.KeyF, "edit.findInFiles", true},
		{0, ui.KeyF3, "edit.findNext", true},
		{ui.Alt, ui.Key2, "", true},
		{ui.Ctrl, ui.KeyW, "", false},
		{ui.Ctrl, ui.KeyF, "", false},
		{ui.Ctrl, ui.KeyR, "", false},
		{ui.Ctrl | ui.Shift, ui.KeyV, "", false},
	} {
		id, ok := a.shellSkip(keymap.Stroke{Mods: c.mods, Key: c.key})
		if id != c.id || ok != c.ok {
			t.Errorf("%v+%v: %q %v, want %q %v", c.mods, c.key, id, ok, c.id, c.ok)
		}
	}
	onOS(t, "darwin")
	if _, ok := a.shellSkip(keymap.Stroke{Mods: ui.Ctrl, Key: ui.KeyP}); ok {
		t.Error("a terminal on macOS left ⌃P to Mayoi")
	}
}

// Vim's normal mode leaves ⌃F, ⌃B and ⌃R to Find, the sidebar and the
// recent folders off macOS, unless vimCtrlKeys gives them to it.
func TestVimCtrlKeys(t *testing.T) {
	for _, k := range []ui.Key{ui.KeyF, ui.KeyB, ui.KeyR} {
		ev := ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Ctrl, Key: k}
		if _, ok := vimRune(ev, false); ok {
			t.Errorf("vim took ⌃%c without its Ctrl keys", 'A'+rune(k-ui.KeyA))
		}
		if r, ok := vimRune(ev, true); !ok || r != rune(k-ui.KeyA)+1 {
			t.Errorf("vim did not take ⌃%c", 'A'+rune(k-ui.KeyA))
		}
	}
	if _, ok := vimRune(ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Ctrl, Key: ui.KeyD}, false); !ok {
		t.Error("vim left ⌃D")
	}
	onOS(t, "windows")
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	if a.vimCtrl() {
		t.Error("vim takes Control's keys on Windows by default")
	}
	a.settings.VimCtrlKeys = true
	if !a.vimCtrl() {
		t.Error("vimCtrlKeys did not give vim Control's keys")
	}
}

func TestFileManagerLabels(t *testing.T) {
	for goos, want := range map[string][3]string{
		"darwin":  {"Reveal in Finder", "Move to Trash", "the Trash"},
		"windows": {"Reveal in File Explorer", "Delete", "the Recycle Bin"},
		"linux":   {"Open Containing Folder", "Move to Trash", "the Trash"},
	} {
		onOS(t, goos)
		if got := [3]string{revealLabel(), trashLabel(), trashName()}; got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}
