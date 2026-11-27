package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// startTreeRename chooses name in the explorer and presses the key that
// renames it in place: Enter on macOS, F2 elsewhere. A row chosen with
// the keyboard is not clicked again, which would show its tab.
func startTreeRename(t *testing.T, hs *harness, name string) {
	t.Helper()
	if hs.a.explorerChoice() != hs.path(name) || hs.a.keyFocus != placeExplorer {
		mustClick(t, hs, 0, name)
	}
	key := ui.KeyF2
	if enterRenames {
		key = ui.KeyEnter
	}
	hs.key(0, key)
	if r := hs.a.treeRename; r == nil || r.path != hs.path(name) {
		t.Fatalf("%s: renaming %+v", name, r)
	}
	if hs.a.prompt != nil {
		t.Fatal("a dialog asked for the name")
	}
}

// Enter on macOS, and F2, edit a file's name in its row, its name before
// the extension selected; Enter keeps the name typed, the tab and the
// row's choice following the file, and the rows keep their places.
func TestExplorerRenameInPlace(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	before, ok := tt.Find("README.md")
	if !ok {
		t.Fatal("no README.md row")
	}
	startTreeRename(t, hs, "go.mod")
	if after, _ := tt.Find("README.md"); after != before {
		t.Fatalf("the next row moved from %v to %v", before, after)
	}
	hs.shot("explorer-rename")
	// Typing replaces the name before ".mod", which was selected.
	tt.Type("build")
	hs.settle()
	if got := a.treeRename.draft; got != "build.mod" {
		t.Fatalf("typed %q, want build.mod", got)
	}
	hs.key(0, ui.KeyEnter)
	if !onDisk(hs.path("build.mod")) || onDisk(hs.path("go.mod")) {
		t.Fatal("go.mod was not renamed to build.mod")
	}
	if a.treeRename != nil {
		t.Fatal("still renaming")
	}
	if got := activePath(a); got != hs.path("build.mod") {
		t.Fatalf("the tab shows %q", got)
	}
	if got := a.explorerChoice(); got != hs.path("build.mod") {
		t.Fatalf("chose %q", got)
	}
	if a.keyFocus != placeExplorer {
		t.Fatal("the explorer lost the keyboard")
	}
	// F2 renames on every system, a folder too.
	mustClick(t, hs, 0, "web")
	hs.key(0, ui.KeyF2)
	if r := a.treeRename; r == nil || r.path != hs.path("web") {
		t.Fatalf("F2 renaming %+v", r)
	}
	tt.Type("site")
	hs.key(0, ui.KeyEnter)
	if !onDisk(hs.path("site/index.html")) || onDisk(hs.path("web")) {
		t.Fatal("web was not renamed to site")
	}
	if a.fails != 0 {
		t.Fatalf("%d failures", a.fails)
	}
}

// Escape keeps the old name and the row chosen; a name taken says so
// under the field, which stays open, and the old name stays.
func TestExplorerRenameCancelsAndRefuses(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	startTreeRename(t, hs, "main.go")
	tt.Type("zzz")
	hs.key(0, ui.KeyEscape)
	if a.treeRename != nil || !onDisk(hs.path("main.go")) || onDisk(hs.path("zzz.go")) {
		t.Fatalf("Escape: renaming %+v", a.treeRename)
	}
	if got := a.explorerChoice(); got != hs.path("main.go") {
		t.Fatalf("Escape chose %q", got)
	}
	startTreeRename(t, hs, "main.go")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("README.md")
	hs.key(0, ui.KeyEnter)
	if a.treeRename == nil || a.treeRename.err != locales.NameExists() || !tt.HasText(locales.NameExists()) {
		t.Fatalf("a name taken: renaming %+v", a.treeRename)
	}
	if !onDisk(hs.path("main.go")) {
		t.Fatal("main.go is gone")
	}
	hs.shot("explorer-rename-taken")
	hs.key(0, ui.KeyEscape)
	if a.treeRename != nil || tt.HasText(locales.NameExists()) {
		t.Fatalf("Escape left renaming %+v", a.treeRename)
	}
	// A name with a slash is not one.
	startTreeRename(t, hs, "main.go")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("a/b.go")
	hs.key(0, ui.KeyEnter)
	if a.treeRename == nil || a.treeRename.err != locales.EnterNameNoSlashes() {
		t.Fatalf("a slash: renaming %+v", a.treeRename)
	}
	// Typing again takes the message away.
	hs.key(0, ui.KeyBackspace)
	if a.treeRename == nil || a.treeRename.err != "" || tt.HasText(locales.EnterNameNoSlashes()) {
		t.Fatalf("typing kept the message: %+v", a.treeRename)
	}
}

// A click elsewhere keeps the name typed, as VS Code does.
func TestExplorerRenameKeptOnBlur(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	// A double click keeps the file open and renames nothing.
	r, _ := tt.Find("README.md")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	hs.settle()
	if tab := a.activeTab(); a.treeRename != nil || tab == nil || tab.transient {
		t.Fatalf("a double click: renaming %+v", a.treeRename)
	}
	startTreeRename(t, hs, "main.go")
	tt.Type("app")
	hs.settle()
	mustClick(t, hs, 0, "README.md")
	if a.treeRename != nil || !onDisk(hs.path("app.go")) || onDisk(hs.path("main.go")) {
		t.Fatalf("a click elsewhere: renaming %+v", a.treeRename)
	}
	if got := activePath(a); got != hs.path("README.md") {
		t.Fatalf("the click showed %q", got)
	}
}

// On macOS, where Enter renames, ⌘↓ keeps the file open; elsewhere Enter
// does.
func TestExplorerKeepOpenKey(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	mustClick(t, hs, 0, "main.go")
	if tab := a.activeTab(); tab == nil || !tab.transient {
		t.Fatal("a click did not preview main.go")
	}
	if enterRenames {
		hs.key(ui.Cmd, ui.KeyDown)
	} else {
		hs.key(0, ui.KeyEnter)
	}
	if tab := a.activeTab(); tab == nil || tab.transient || a.treeRename != nil {
		t.Fatalf("not kept open: renaming %+v", a.treeRename)
	}
}

// A compact row renames its last folder, the folders before it shown; the
// menu's Rename… and the command edit in place, the command in a dialog
// for a file the explorer does not show.
func TestExplorerRenameCompactAndCommand(t *testing.T) {
	hs := compactExplorerHarness(t)
	a, tt := hs.a, hs.tt
	if err := tt.RightClick(packageChain); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename…"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if r := a.treeRename; r == nil || r.draft != "example" || a.prompt != nil {
		t.Fatalf("rename of the compact row: %+v", r)
	}
	lead := strings.TrimSuffix(packageChain, "example")
	if !tt.HasText(lead) {
		t.Fatalf("no %q before the field: %q", lead, tt.Texts())
	}
	tt.Type("demo")
	hs.key(0, ui.KeyEnter)
	if !onDisk(hs.path("src/main/java/com/demo/App.java")) {
		t.Fatal("the last folder was not renamed")
	}
	a.openFile(hs.path("mixed/README"), true)
	hs.settle()
	a.run("file.rename")
	hs.settle()
	if r := a.treeRename; r == nil || r.path != hs.path("mixed/README") || a.prompt != nil {
		t.Fatalf("the command: renaming %+v, prompt %v", r, a.prompt != nil)
	}
	hs.key(0, ui.KeyEscape)
	// With the explorer hidden, the command asks in a dialog.
	a.window.SidebarClosed = true
	a.run("file.rename")
	hs.settle()
	if a.treeRename != nil || a.prompt == nil {
		t.Fatal("the command did not ask in a dialog with the explorer hidden")
	}
}

// The dialog's title keeps its line beside a long path, in every
// language: Chinese and Japanese break between any two characters.
func TestPromptTitleHoldsItsLine(t *testing.T) {
	locale := i18n.Locale()
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return locale }) })
	for _, loc := range []string{"en", "ja", "zh-Hans"} {
		i18n.SetLocaleFunc(func() string { return loc })
		hs := newHarness(t, true)
		a, tt := hs.a, hs.tt
		a.askRename(hs.path("main.go"))
		hs.settle()
		title := locales.In(loc).RenameTitle()
		short, ok := tt.Find(title)
		if !ok {
			t.Fatalf("%s: no title %q", loc, title)
		}
		a.prompt.hint = strings.Repeat("very/long/path/", 20) + "main.go"
		hs.settle()
		if long, _ := tt.Find(title); long.H != short.H || long.W != short.W {
			t.Fatalf("%s: the title went from %v to %v", loc, short, long)
		}
	}
}
