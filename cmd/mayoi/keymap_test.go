package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
)

const testKeys = `// as Cursor's, with ctrl+. for the prefix
[
	{ "key": "ctrl+. e s", "command": "editor.action.sortLinesAscending" },
	{ "key": "ctrl+. e c", "command": "editor.action.transformToCamelcase" },
	{ "key": "ctrl+. 1", "command": "workbench.view.explorer" },
	{ "key": "ctrl+. g c", "command": "runCommands",
	  "args": { "commands": ["editor.action.sortLinesAscending", "cursor.generateGitCommitMessage", "workbench.action.files.save"] } },
	{ "key": "ctrl+. o r", "command": "vscode.open", "args": "README.md" },
	{ "key": "d", "command": "deleteFile", "when": "filesExplorerFocus && !inputFocus" },
	{ "key": "cmd+1", "command": "workbench.action.toggleSidebarVisibility" },
	{ "key": "cmd+1", "command": "-workbench.action.focusFirstEditorGroup" },
	{ "key": "cmd+b", "command": "workbench.action.files.saveAll" },
]
`

func withKeys(t *testing.T, a *app, src string) {
	t.Helper()
	km, err := keymap.Parse([]byte(src))
	if err != nil || len(km.Errors) > 0 {
		t.Fatalf("%v %q", err, km.Errors)
	}
	a.keys, a.palette = km, nil
}

func TestKeymapChordInEditor(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false
	withKeys(t, a, testKeys)
	os.WriteFile(hs.path("list.txt"), []byte("pear\napple\nfig"), 0o644)
	a.openFile(hs.path("list.txt"), true)
	hs.settle()
	d := a.activeEditor()

	tt.Key(ui.Ctrl, ui.KeyPeriod)
	hs.settle()
	if !tt.HasText("(⌃.) was pressed. Waiting for the next key…") {
		t.Errorf("the status bar does not wait for the chord: %q", tt.Texts())
	}
	// The keys of the chord come with their text, which types nothing.
	tt.TypeKey(0, ui.KeyE, "e")
	tt.TypeKey(0, ui.KeyS, "s")
	hs.settle()
	if got := d.buf.String(); got != "apple\nfig\npear" {
		t.Fatalf("sorted %q", got)
	}
	if len(a.chord) > 0 || tt.HasText("(⌃.) was pressed. Waiting for the next key…") {
		t.Error("the chord is still waiting")
	}

	// A key the chord has no binding of is dropped with it. The keys taken
	// type nothing: Tester.Key sends no text, as the system sends none.
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.Key(0, ui.KeyX)
	hs.settle()
	if got := d.buf.String(); got != "apple\nfig\npear" || len(a.chord) > 0 {
		t.Fatalf("ctrl+. x typed: %q, chord %v", got, a.chord)
	}
	// Typing goes on as it did.
	tt.Type("z")
	hs.settle()
	if !strings.Contains(d.buf.String(), "z") {
		t.Errorf("typing after the chord: %q", d.buf.String())
	}
}

func TestKeymapInTextField(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	withKeys(t, a, testKeys)
	a.openQuick("")
	hs.settle()
	tt.Type("ma")
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.TypeKey(0, ui.KeyX, "x")
	hs.settle()
	if a.quick.query != "ma" {
		t.Errorf("the chord typed in quick open: %q", a.quick.query)
	}
	// A chord there runs too.
	a.window.SidebarView = "git"
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.TypeKey(0, ui.Key1, "1")
	hs.settle()
	if a.quick.query != "ma" || a.window.SidebarView != "" {
		t.Errorf("ctrl+. 1 in quick open: query %q, sidebar %q", a.quick.query, a.window.SidebarView)
	}
}

func TestKeymapOverridesShortcuts(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	withKeys(t, a, testKeys)
	a.openFile(hs.path("go.mod"), true)
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	closed := a.window.SidebarClosed
	tt.Key(ui.Super, ui.Key1)
	hs.settle()
	if a.window.SidebarClosed == closed {
		t.Error("cmd+1 did not toggle the sidebar")
	}
	if a.activeTab().path() != hs.path("README.md") {
		t.Errorf("cmd+1 showed the first tab too: %s", a.activeTab().path())
	}
	// The menu gives up the keys the file binds, and those it takes away.
	for _, cmd := range a.commands() {
		// cmd+b is CmdOrCtrl+B only where CmdOrCtrl is Command, on a Mac.
		if cmd.id == "view.sidebar" && a.menuAccel(cmd) != "" && ui.Cmd == ui.Super {
			t.Errorf("Toggle Sidebar keeps %s, which the file binds", a.menuAccel(cmd))
		}
		if cmd.id == "file.save" && a.menuAccel(cmd) != "CmdOrCtrl+S" {
			t.Errorf("Save lost its keys")
		}
	}
	// The palette shows the file's keys.
	for _, e := range a.paletteEntries() {
		if e.id == "sel.sortAsc" && !slices.Equal(e.caps, []string{"⌃.", "E", "S"}) {
			t.Errorf("Sort Lines Ascending shows %q", e.caps)
		}
		if e.id == "file.saveAll" && !slices.Equal(e.caps, []string{"⌘", "B"}) {
			t.Errorf("Save All shows %q", e.caps)
		}
	}
}

func TestKeymapExplorerKeys(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	withKeys(t, a, testKeys)
	if err := tt.Click("go.mod"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// d in the editor types.
	a.focusColumn()
	hs.settle()
	tt.TypeKey(0, ui.KeyD, "d")
	hs.settle()
	if _, err := os.Stat(hs.path("go.mod")); err != nil {
		t.Fatal("d in the editor trashed the file")
	}
	// d in the explorer trashes the file chosen.
	if err := tt.Click("go.mod"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	tt.TypeKey(0, ui.KeyD, "d")
	hs.settle()
	if !slices.Contains(hs.h.trashed, hs.path("go.mod")) {
		t.Errorf("d in the explorer: trashed %q", hs.h.trashed)
	}
}

func TestKeymapRunCommands(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false
	withKeys(t, a, testKeys)
	os.WriteFile(hs.path("list.txt"), []byte("b\na\n"), 0o644)
	a.openFile(hs.path("list.txt"), true)
	hs.settle()
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.Key(0, ui.KeyG)
	tt.Key(0, ui.KeyC)
	hs.settle()
	// Sorted, a note of the command Mayoi has not, then saved.
	if data, _ := os.ReadFile(hs.path("list.txt")); string(data) != "\na\nb" {
		t.Errorf("saved %q", data)
	}
	if !tt.HasText("Mayoi has no command cursor.generateGitCommitMessage") {
		t.Errorf("no note of the command Mayoi has not: %q", tt.Texts())
	}

	// vscode.open opens a file of the folder.
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.Key(0, ui.KeyO)
	tt.Key(0, ui.KeyR)
	hs.settle()
	if a.activeTab().path() != hs.path("README.md") {
		t.Errorf("vscode.open showed %q", a.activeTab().path())
	}
}

func TestCamelCaseAndSort(t *testing.T) {
	for in, want := range map[string]string{
		"foo_bar baz": "fooBarBaz",
		"foo-bar":     "fooBar",
		"Foo_bar":     "FooBar",
		"already":     "already",
	} {
		if got := toCamelCase(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	hs := newHarness(t, false)
	a := hs.a
	os.WriteFile(hs.path("w.txt"), []byte("x my_long-name y\nc\nb\na"), 0o644)
	a.openFile(hs.path("w.txt"), true)
	hs.settle()
	d := a.activeEditor()
	// The word of the caret.
	a.queueEdit(d, func(d *editor) { d.sel = [2]int{6, 6} })
	a.run("sel.camelCase")
	hs.settle()
	if got := d.buf.String(); got != "x myLongName y\nc\nb\na" {
		t.Fatalf("camel case %q", got)
	}
	// The lines selected.
	a.queueEdit(d, func(d *editor) { d.sel = [2]int{15, 20} })
	a.run("sel.sortDesc")
	a.queueEdit(d, func(d *editor) { d.sel = [2]int{15, 20} })
	a.run("sel.sortAsc")
	hs.settle()
	if got := d.buf.String(); got != "x myLongName y\na\nb\nc" {
		t.Fatalf("sorted %q", got)
	}
}

func TestKeymapChordTypesNothingAndLetsGo(t *testing.T) {
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	a.settings.AutoSave = false
	withKeys(t, a, testKeys)
	a.openFile(hs.path("go.mod"), true)
	hs.settle()
	d := a.activeEditor()
	before := d.buf.String()
	// ctrl+. 1 from the editor: the Explorer, and no 1 typed.
	a.window.SidebarView = "git"
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	tt.TypeKey(0, ui.Key1, "1")
	hs.settle()
	if d.buf.String() != before || a.window.SidebarView != "" {
		t.Fatalf("ctrl+. 1: text %q, sidebar %q", d.buf.String(), a.window.SidebarView)
	}
	// The key let go of, the editor types again.
	tt.Key(0, ui.Key1) // its release
	a.focusColumn()
	hs.settle()
	tt.Type("q")
	hs.settle()
	if !strings.HasPrefix(d.buf.String(), "q") || a.chording() {
		t.Errorf("typing after the chord: %q, chording %v", d.buf.String(), a.chording())
	}
}
