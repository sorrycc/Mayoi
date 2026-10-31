package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
)

// TestMain runs the tests with macOS's keys and names, which they are
// written for, on any system: those of Windows and Linux ask for theirs
// with onOS. They read English, whatever languages the system prefers,
// unless MYGO_LOCALE asks for another, as for shots of a translation.
// The language is worked out once: with no app running, MyGo would match
// it again for every message, which the frame benchmarks would measure.
func TestMain(m *testing.M) {
	hostOS = "darwin"
	keepLineEndings()
	if os.Getenv("MYGO_LOCALE") == "" {
		os.Setenv("MYGO_LOCALE", "en")
	}
	locale := i18n.Locale()
	i18n.SetLocaleFunc(func() string { return locale })
	// The previews' pictures of the web are not fetched, but by the tests
	// of them, from servers of their own.
	markdownRemote.get = func(string) ([]byte, error) { return nil, errors.New("offline") }
	os.Exit(m.Run())
}

// keepLineEndings has every git the tests run, theirs and the app's, keep
// the line endings of the files: Git for Windows turns them into CRLF by
// its own settings, and the diffs and contents compared with them.
func keepLineEndings() {
	os.Setenv("GIT_CONFIG_COUNT", "1")
	os.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	os.Setenv("GIT_CONFIG_VALUE_0", "false")
}

// skipOnWindows skips a test that does not yet pass on Windows, for why.
func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why)
	}
}

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
			t.Cleanup(a.shutdown)
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
	t.Cleanup(a.shutdown)
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
	closed := a.window.SidebarClosed
	tt.Key(ui.Ctrl, ui.KeyB)
	hs.settle()
	if a.window.SidebarClosed == closed {
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

// A terminal off macOS leaves Mayoi the keys of the workbench and find,
// and the shell the others with Control, which edit its line.
func TestShellSkip(t *testing.T) {
	onOS(t, "linux")
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	t.Cleanup(a.shutdown)
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
		{ui.Ctrl, ui.KeyF, "edit.find", true},
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
// recent folders off macOS, and ⌃A and ⌃X to Select All and Cut, unless vimCtrlKeys gives them to it.
func TestVimCtrlKeys(t *testing.T) {
	for _, k := range []ui.Key{ui.KeyF, ui.KeyB, ui.KeyR, ui.KeyA, ui.KeyX} {
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
	t.Cleanup(a.shutdown)
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

// The caps of keys are those of the keyboard, as keyText writes them on
// each system.
func TestSplitCaps(t *testing.T) {
	for keys, want := range map[string]string{
		"⇧⌘P":             "⇧|⌘|P",
		"⌘K ⌘S":           "⌘|K|⌘|S",
		"⌘F3":             "⌘|F3",
		"↑↓":              "↑|↓",
		"esc":             "esc",
		"⏎":               "⏎",
		"Ctrl+O":          "Ctrl|O",
		"Ctrl+Shift+P":    "Ctrl|Shift|P",
		"Ctrl+`":          "Ctrl|`",
		"Ctrl++":          "Ctrl|+",
		"Shift+F3":        "Shift|F3",
		"F3":              "F3",
		"Ctrl+K Ctrl+S":   "Ctrl|K|Ctrl|S",
		"Ctrl+Alt+Delete": "Ctrl|Alt|Delete",
	} {
		if got := strings.Join(splitCaps(keys), "|"); got != want {
			t.Errorf("splitCaps(%q) = %s, want %s", keys, got, want)
		}
	}
}

// A path typed after :e is of the folder, or of the home folder after ~/,
// spelled as the system spells it: with backslashes on Windows, as the
// Explorer has the file.
func TestResolvePathIsTheSystems(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	a := &app{root: root}
	if got, want := a.resolvePath("src/a.go"), filepath.Join(root, "src", "a.go"); got != want {
		t.Errorf("resolvePath(src/a.go) = %q, want %q", got, want)
	}
	if got, want := a.resolvePath("~/notes/b.md"), filepath.Join(homeDir(), "notes", "b.md"); got != want {
		t.Errorf("resolvePath(~/notes/b.md) = %q, want %q", got, want)
	}
	abs := filepath.Join(root, "c.go")
	if got := a.resolvePath(abs); got != abs {
		t.Errorf("resolvePath(%q) = %q", abs, got)
	}
}

// A name to rename to has no folders in it: no slash on any system, and
// on Windows no backslash either, which is a letter of a name elsewhere.
func TestHasSlash(t *testing.T) {
	if !hasSlash("a/b") || hasSlash("a.go") {
		t.Error("a slash names a folder")
	}
	if got, want := hasSlash(`a\b`), filepath.Separator == '\\'; got != want {
		t.Errorf(`hasSlash("a\b") = %v`, got)
	}
}

// A terminal is named for its shell: $SHELL's, and on Windows the first
// the terminal finds of PowerShell's and the command prompt.
func TestShellName(t *testing.T) {
	t.Setenv("SHELL", "/bin/fish")
	if got := shellName(); got != "fish" {
		t.Errorf("shellName = %q, want fish", got)
	}
	onOS(t, "windows")
	t.Setenv("COMSPEC", "cmd.exe")
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	for have, want := range map[string]string{"pwsh.exe": "pwsh", "powershell.exe": "powershell", "cmd.exe": "cmd", "": "shell"} {
		lookPath = func(name string) (string, error) {
			if name == have {
				return name, nil
			}
			return "", os.ErrNotExist
		}
		if got := shellName(); got != want {
			t.Errorf("with %q, shellName = %q, want %q", have, got, want)
		}
	}
}

// The home folder reads as ~, and the folders in it from there: not one
// whose name only begins as the home folder's, and on Windows whatever
// the case of the letters.
func TestShortenIn(t *testing.T) {
	sep := string(filepath.Separator)
	home := sep + "Users" + sep + "me"
	for p, want := range map[string]string{
		home:                      "~",
		home + sep + "proj":       "~" + sep + "proj",
		home + "2" + sep + "proj": home + "2" + sep + "proj",
		sep + "tmp":               sep + "tmp",
	} {
		if got := shortenIn(p, home); got != want {
			t.Errorf("shortenIn(%q) = %q, want %q", p, got, want)
		}
	}
	if got := shortenIn(sep+"users"+sep+"ME"+sep+"proj", home); got != sep+"users"+sep+"ME"+sep+"proj" {
		t.Errorf("off Windows the case of a folder counts: %q", got)
	}
	onOS(t, "windows")
	if got := shortenIn(sep+"users"+sep+"ME"+sep+"proj", home); got != "~"+sep+"proj" {
		t.Errorf("on Windows the case of a folder does not count: %q", got)
	}
}

// Off macOS the welcome page's keys read as Control's, a cap a key.
func TestSplitCapsOfKeyText(t *testing.T) {
	onOS(t, "linux")
	if got := strings.Join(splitCaps(keyText("CmdOrCtrl+Shift+P")), "|"); got != "Ctrl|Shift|P" {
		t.Errorf("caps of Command Palette on Linux = %s, want Ctrl|Shift|P", got)
	}
}
