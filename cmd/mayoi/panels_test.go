package main

import (
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// Search moved to the bottom panel shows as a tab beside the terminal's,
// its results under them, and the panel's tabs show one view at a time.
func TestSearchInPanel(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	hs.searchFor("main")
	s := &a.inFiles
	if at, _ := a.viewSide(settings.ViewSearch); at != settings.SideLeft ||
		hs.shown(settings.ViewSearch) != settings.ViewSearch || !tt.HasText("3 results in 1 file") {
		t.Fatalf("search on %q, showing %q: %q", at, hs.shown(settings.ViewSearch), tt.Texts())
	}

	// Moved to the bottom panel: its tab beside the terminal's, its
	// results under them, and the left panel on the explorer.
	a.moveView(settings.ViewSearch, settings.SideBottom)
	hs.settle()
	if at, _ := a.viewSide(settings.ViewSearch); at != settings.SideBottom ||
		!hs.panelOpen(settings.ViewSearch) || hs.shown(settings.ViewSearch) != settings.ViewSearch {
		t.Fatalf("search on %q, panel open %v, showing %q",
			at, hs.panelOpen(settings.ViewSearch), hs.shown(settings.ViewSearch))
	}
	if hs.shown(settings.ViewExplorer) != settings.ViewExplorer {
		t.Errorf("the left panel shows %q, not the explorer", hs.shown(settings.ViewExplorer))
	}
	terminalTab, ok := tt.Find("TERMINAL")
	if !ok {
		t.Fatalf("the panel has no terminal's tab: %q", tt.Texts())
	}
	searchTab, ok := tt.Find("SEARCH")
	if !ok || searchTab.X <= terminalTab.X {
		t.Fatalf("the search tab at %v, the terminal's at %v", searchTab, terminalTab)
	}
	if first, last := s.list.Visible(); last < first {
		t.Errorf("the results list has no room in the panel")
	}
	summary, ok := tt.Find("3 results in 1 file")
	if !ok || summary.Y <= terminalTab.Y {
		t.Errorf("the summary at %v, the tabs at %v", summary, terminalTab)
	}

	// The tabs show one view at a time.
	if err := tt.Click("TERMINAL"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.shown(settings.ViewSearch) != panelTerminal {
		t.Errorf("the terminal's tab shows %q", hs.shown(settings.ViewSearch))
	}
	if err := tt.Click("SEARCH"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.shown(settings.ViewSearch) != settings.ViewSearch || !tt.HasText("3 results in 1 file") {
		t.Errorf("the search tab shows %q", hs.shown(settings.ViewSearch))
	}

	// Find in Files shows it again, opening its panel with the keyboard in
	// its query.
	a.window.Panels.Get(settings.SideBottom).Open = false
	hs.settle()
	a.run("edit.findInFiles")
	hs.settle()
	if !hs.panelOpen(settings.ViewSearch) || hs.shown(settings.ViewSearch) != settings.ViewSearch || !tt.Focused("Search") {
		t.Fatalf("find in files: panel open %v, showing %q, focused %v",
			hs.panelOpen(settings.ViewSearch), hs.shown(settings.ViewSearch), tt.Focused("Search"))
	}
	if !tt.HasText("3 results in 1 file") {
		t.Errorf("the panel lost what search found: %q", tt.Texts())
	}
}

// Every view moves to a panel on any side and shows as a tab there; the
// panel the last view leaves takes no room, and the editors take it.
func TestViewsInPanels(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	hs.gitRepo()
	a.openFile(hs.path("main.go"), true)
	hs.settle()

	// The file views start on the layout's left, the terminal below, and
	// the left panel is narrow enough to name its tabs short.
	if at, _ := a.viewSide(settings.ViewExplorer); at != settings.SideLeft {
		t.Fatalf("the explorer is on %q", at)
	}
	if !tt.HasText("GIT") || tt.HasText("SOURCE CONTROL") {
		t.Errorf("the left panel names its tabs %q", tt.Texts())
	}
	if at, _ := a.viewSide(panelTerminal); at != settings.SideBottom {
		t.Fatalf("the terminal is on %q", at)
	}
	tab, ok := tt.Find("Close main.go")
	if !ok {
		t.Fatalf("no editor tab: %q", tt.Texts())
	}
	width := a.leftPanel().Size

	// The explorer moves to the bottom panel, which shows it, and the left
	// panel shows search.
	a.moveView(settings.ViewExplorer, settings.SideBottom)
	hs.settle()
	if at, _ := a.viewSide(settings.ViewExplorer); at != settings.SideBottom ||
		hs.shown(settings.ViewExplorer) != settings.ViewExplorer {
		t.Fatalf("the explorer is on %q, showing %q", at, hs.shown(settings.ViewExplorer))
	}
	if hs.shown(settings.ViewSearch) != settings.ViewSearch {
		t.Errorf("the left panel shows %q, not search", hs.shown(settings.ViewSearch))
	}
	// ⇧⌘E shows the explorer wherever it is.
	a.run("view.explorer")
	hs.settle()
	if hs.shown(settings.ViewExplorer) != settings.ViewExplorer || a.keyFocus != placeExplorer {
		t.Errorf("show explorer: showing %q, the keyboard at %v", hs.shown(settings.ViewExplorer), a.keyFocus)
	}

	// Source control moves too, with its own tools.
	a.moveView(settings.ViewSCM, settings.SideBottom)
	hs.settle()
	if at, _ := a.viewSide(settings.ViewSCM); at != settings.SideBottom || hs.shown(settings.ViewSCM) != settings.ViewSCM {
		t.Fatalf("source control is on %q, showing %q", at, hs.shown(settings.ViewSCM))
	}
	if _, ok := tt.Find("Branch main"); !ok {
		t.Errorf("the panel has no branch row: %q", tt.Texts())
	}

	// A view moved to the right panel shows there, and the layout mirrored
	// puts it on the left of the window.
	a.moveView(settings.ViewSearch, settings.SideRight)
	hs.settle()
	if at, _ := a.viewSide(settings.ViewSearch); at != settings.SideRight {
		t.Fatalf("search is on %q", at)
	}
	if tab, ok := tt.Find("SEARCH"); !ok || tab.X < 1200-a.rightPanel().Size {
		t.Errorf("the search tab at %v, the right panel %v wide", tab, a.rightPanel().Size)
	}
	a.run("view.sidebarRight")
	hs.settle()
	if tab, ok := tt.Find("SEARCH"); !ok || tab.X > 1200-a.rightPanel().Size {
		t.Errorf("mirrored, the search tab at %v", tab)
	}
	a.run("view.sidebarRight")
	hs.settle()

	// The left panel has no view left, so it takes no room: the editors
	// take it, and their tabs move over by its width.
	a.moveView(settings.ViewExplorer, settings.SideRight)
	a.moveView(settings.ViewSCM, settings.SideRight)
	hs.settle()
	if p := a.leftPanel(); p.Visible() {
		t.Errorf("the left panel %+v shows, with no view", p)
	}
	if moved, ok := tt.Find("Close main.go"); !ok || moved.X > tab.X-width/2 {
		t.Errorf("the editors start at %v, was %v with a %v panel", moved.X, tab.X, width)
	}

	// The panel the views left keeps its size for the view that comes
	// back, and shows it.
	size := a.rightPanel().Size
	a.moveView(settings.ViewSCM, settings.SideLeft)
	hs.settle()
	if got := a.rightPanel().Size; got != size {
		t.Errorf("the right panel is %v wide, was %v", got, size)
	}
	if hs.shown(settings.ViewSCM) != settings.ViewSCM {
		t.Errorf("the left panel shows %q", hs.shown(settings.ViewSCM))
	}
	a.leftPanel().Size = 400
	a.moveView(settings.ViewSCM, settings.SideBottom)
	a.moveView(settings.ViewSCM, settings.SideLeft)
	hs.settle()
	if got := a.leftPanel().Size; got != 400 {
		t.Errorf("the left panel is %v wide, was 400 when its last view left", got)
	}
}

// A view moves alone: the views of the panel it goes to stay where they
// were, as tabs, and the one that moves shows.
func TestViewsMoveAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("view.terminal")
	hs.settle()
	hs.searchFor("main")

	// The terminal and search in the bottom panel: the terminal's tab
	// first, search's after it.
	a.moveView(settings.ViewSearch, settings.SideBottom)
	hs.settle()
	if got := a.bottomPanel().Views; !slices.Equal(got, []string{panelTerminal, settings.ViewSearch}) {
		t.Fatalf("the bottom panel holds %q", got)
	}
	// Moving the terminal to the left panel takes the terminal alone.
	a.moveView(panelTerminal, settings.SideLeft)
	hs.settle()
	if got := a.bottomPanel().Views; !slices.Equal(got, []string{settings.ViewSearch}) {
		t.Errorf("the bottom panel holds %q, not search alone", got)
	}
	if hs.shown(settings.ViewSearch) != settings.ViewSearch {
		t.Errorf("the bottom panel shows %q", hs.shown(settings.ViewSearch))
	}
	// The terminal sits in the left panel beside the file views, showing.
	if got := a.leftPanel().Views; !slices.Equal(got, []string{settings.ViewExplorer, settings.ViewSCM, panelTerminal}) {
		t.Errorf("the left panel holds %q", got)
	}
	if hs.shown(panelTerminal) != panelTerminal {
		t.Errorf("the left panel shows %q, not the terminal", hs.shown(panelTerminal))
	}
	if _, ok := tt.Find("Move Terminal to Editor Area"); !ok {
		t.Errorf("the terminal's view is not drawn: %q", tt.Texts())
	}
}

// The menu of a panel's tab moves that view, and offers the panel's own
// actions; the panel's buttons show and hide the panels of the layout.
func TestPanelTabMenus(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.run("view.terminal")
	hs.settle()
	a.moveView(settings.ViewSearch, settings.SideBottom)
	hs.settle()

	// Search's tab moves search alone to the right.
	if err := tt.RightClick("SEARCH"); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	if !slices.Contains(menu, "Move to") || !slices.Contains(menu, "Maximize Panel") || !slices.Contains(menu, "Hide Panel") {
		t.Fatalf("the tab's menu: %q", menu)
	}
	if err := tt.ChooseMenuItem("Move to", "Right"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if at, _ := a.viewSide(settings.ViewSearch); at != settings.SideRight {
		t.Errorf("search is on %q", at)
	}
	if got := a.bottomPanel().Views; !slices.Equal(got, []string{panelTerminal}) {
		t.Errorf("the bottom panel holds %q", got)
	}

	// The panel's Hide Panel closes it, and its button shows it again.
	if err := tt.RightClick("TERMINAL"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Hide Panel"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.bottomPanel().Open {
		t.Error("the bottom panel stays open after hiding it")
	}
	if err := tt.Click(a.withKey("Toggle Bottom Panel", "view.panelBottom")); err != nil {
		t.Fatalf("the title bar has no bottom panel button: %v", err)
	}
	hs.settle()
	if !a.bottomPanel().Open || hs.shown(panelTerminal) != panelTerminal {
		t.Errorf("the button: open %v, showing %q", a.bottomPanel().Open, hs.shown(panelTerminal))
	}
}

// The three buttons at the end of the title bar show and hide the panels
// of the layout's left, of the bottom and of the right, and a panel with
// no view has nothing to show.
func TestLayoutControls(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.moveView(settings.ViewSearch, settings.SideRight)
	hs.settle()
	if err := tt.Click(a.withKey("Toggle Secondary Side Bar", "view.panelRight")); err != nil {
		t.Fatalf("no right panel button: %v", err)
	}
	hs.settle()
	if a.rightPanel().Open {
		t.Error("the right panel stays open")
	}
	if _, ok := tt.Find("SEARCH"); ok {
		t.Error("the right panel is drawn while closed")
	}
	if err := tt.Click(a.withKey("Toggle Secondary Side Bar", "view.panelRight")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.rightPanel().Open {
		t.Error("the right panel stays closed")
	}
	// A panel with no view of its own is not made one by its button: the
	// bottom panel, emptied, keeps its size and shows nothing.
	a.moveView(panelTerminal, settings.SideLeft)
	a.moveView(settings.ViewSearch, settings.SideLeft)
	hs.settle()
	if a.bottomPanel().Visible() {
		t.Fatalf("the bottom panel %+v shows, with no view", a.bottomPanel())
	}
	before := len(a.window.Panels)
	if err := tt.Click(a.withKey("Toggle Bottom Panel", "view.panelBottom")); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.bottomPanel().Open || len(a.window.Panels) != before {
		t.Errorf("the button made a panel of the bottom one: open %v, %d panels, was %d", a.bottomPanel().Open, len(a.window.Panels), before)
	}
	if tt.Focused("Toggle Bottom Panel") {
		t.Error("the button for a panel with no views takes the keyboard")
	}
}

// A panel on a side is as wide as its handle is dragged, keeps its size as
// a narrower window makes it give way, and takes it back as the window
// widens.
func TestSidePanelKeepsItsWidth(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.moveView(settings.ViewSearch, settings.SideRight)
	var editorWidth float32
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		editorWidth = a.colsView.Bounds(c).W
	}, 1200, 800)
	hs.tt = tt
	hs.settle()
	size := a.rightPanel().Size
	edge := 1200 - size + 2
	tt.Press(edge, 300)
	tt.Move(edge-40, 300)
	tt.Move(edge-60, 300)
	tt.Release(edge-60, 300)
	hs.settle()
	if got := a.rightPanel().Size; got < size+50 || got > size+70 {
		t.Fatalf("the panel %v after dragging 60 from %v", got, size)
	}
	wide := a.rightPanel().Size
	hs.tt.SetSize(760, 800)
	hs.settle()
	if editorWidth < 320 {
		t.Errorf("the panels leave the editors %v in a narrow window", editorWidth)
	}
	hs.tt.SetSize(1200, 800)
	hs.settle()
	if got := a.rightPanel().Size; got != wide {
		t.Errorf("the panel is %v wide, was %v", got, wide)
	}
}

// Two side panels share the room outside the editors: each gives way for
// the other, and its header uses the width it actually gets, not the width
// it would have in a wider window.
func TestSidePanelsShareTheWindow(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	a.moveView(settings.ViewSCM, settings.SideRight)
	a.leftPanel().Size = 500
	a.rightPanel().Size = 500
	var editorWidth float32
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		editorWidth = a.colsView.Bounds(c).W
	}, 900, 760)
	hs.tt = tt
	hs.settle()
	if editorWidth < 320 {
		t.Errorf("the editors are %v wide, less than the 320 the panels leave them", editorWidth)
	}
	if !tt.HasText("GIT") || tt.HasText("SOURCE CONTROL") {
		t.Errorf("the clamped panel names its tabs %q", tt.Texts())
	}
}

// Maximized, a panel takes the window, and its tab strip comes back with
// it as it is restored; hidden, it takes no room at all.
func TestSidePanelMaximizesAndHides(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.moveView(settings.ViewSearch, settings.SideRight)
	hs.settle()
	if err := tt.RightClick("SEARCH"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Maximize Panel"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.panelMax != settings.SideRight {
		t.Fatalf("the panel maximized is %q", a.panelMax)
	}
	if tab, ok := tt.Find("SEARCH"); !ok || tab.X > 300 {
		t.Errorf("the maximized panel's tab is at %v", tab)
	}
	if err := tt.RightClick("SEARCH"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Restore Panel"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.panelMax != "" {
		t.Errorf("the panel is still maximized")
	}
	if tab, ok := tt.Find("SEARCH"); !ok || tab.X < 800 {
		t.Errorf("restored, the panel's tab is at %v", tab)
	}
	// Hidden, it takes no room either.
	a.hidePanel(settings.SideRight)
	hs.settle()
	if _, ok := tt.Find("SEARCH"); ok {
		t.Error("the hidden panel is drawn")
	}
}

// sideBarVisible, and the names VS Code gives it, follow the left panel:
// a closed one holds nothing.
func TestSideBarConditions(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.settle()
	when := a.whenContext(placeExplorer)
	for _, name := range []string{"sideBarVisible", "explorerViewletVisible"} {
		if !when(name) {
			t.Errorf("%s is false with the left panel open", name)
		}
	}
	a.leftPanel().Open = false
	for _, name := range []string{"sideBarVisible", "explorerViewletVisible"} {
		if when(name) {
			t.Errorf("%s holds with the left panel closed", name)
		}
	}
}

// An enabled plugin's view has a tab and a menu item before it is first
// revealed. Its tab starts it, and revealing it again leaves it where the
// user moved it.
func TestPluginViewMovesBetweenPanels(t *testing.T) {
	hs := newHarness(t, false)
	h := withBundled(hs)
	a, tt := hs.a, hs.tt
	const view = "plugin:todos.list"
	if !a.viewInPanel(view) || !tt.HasText("TODOS") {
		t.Fatalf("the unrevealed plugin's view has no tab: %q", tt.Texts())
	}
	var viewMenu, moveMenu, todosMenu *mygo.MenuItem
	for _, it := range a.menu(false, func(string, *mygo.Window) {}).Items() {
		if it.Label == "View" {
			viewMenu = it
		}
	}
	if viewMenu != nil {
		for _, it := range viewMenu.Submenu {
			if it.Label == "Move View to Panel" {
				moveMenu = it
			}
		}
	}
	if moveMenu != nil {
		for _, it := range moveMenu.Submenu {
			if it.Label == "TODOs" {
				todosMenu = it
			}
		}
	}
	if todosMenu == nil {
		t.Fatal("Move View to Panel does not list the enabled plugin's view")
	}

	if err := tt.Click("TODOS"); err != nil {
		t.Fatal(err)
	}
	hs.until("the TODOs", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		data, _ := json.Marshal(h.views["todos.list"])
		return strings.Contains(string(data), "say more")
	})
	a.moveView(view, settings.SideRight)
	hs.settle()
	if at, _ := a.viewSide(view); at != settings.SideRight {
		t.Fatalf("the plugin's view is on %q", at)
	}
	if !tt.HasText("TODOS") {
		t.Errorf("the right panel has no tab for the plugin's view: %q", tt.Texts())
	}
	if tt.HasText("This plugin's view is gone.") {
		t.Fatalf("the plugin's view is gone: %q", tt.Texts())
	}
	hs.until("the moved TODOs", func() bool { return tt.HasText("say more") && tt.HasText("main.go") })
	a.showPluginView("todos.list")
	hs.settle()
	if at, _ := a.viewSide(view); at != settings.SideRight || hs.shown(view) != view {
		t.Errorf("revealing moved the plugin's view to %q, showing %q", at, hs.shown(view))
	}
}

// Removing a plugin takes its view's tab away, rather than leaving a tab
// that can only say the view is gone.
func TestUninstalledPluginViewLosesItsTab(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": lintPlugin, "main.ts": lintMain}})
	a, tt := hs.a, hs.tt
	a.showPluginView("lint.view")
	a.moveView("plugin:lint.view", settings.SideRight)
	hs.settle()
	if !tt.HasText("LINT") {
		t.Fatalf("no plugin tab before its removal: %q", tt.Texts())
	}
	if err := os.RemoveAll(h.reg.Plugin("lint").Dir); err != nil {
		t.Fatal(err)
	}
	h.reg.Load()
	hs.settle()
	if tt.HasText("LINT.VIEW") || tt.HasText("This plugin's view is gone.") {
		t.Errorf("the removed plugin leaves a dead tab: %q", tt.Texts())
	}
}

// A terminal shown from a command opens the panel that holds it, with
// the keyboard in the terminal.
func TestShowingATerminalOpensItsPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	a.run("view.terminal")
	hs.settle()
	s := a.panelTerm
	if s == nil {
		t.Fatalf("no terminal: %q", tt.Texts())
	}
	a.hidePanel(settings.SideBottom)
	hs.settle()
	if a.bottomPanel().Open {
		t.Fatal("the panel stays open after hiding it")
	}
	a.showTerminal(s)
	hs.settle()
	if !a.bottomPanel().Open || !a.terminalShown() {
		t.Fatalf("the panel: open %v, terminal shown %v", a.bottomPanel().Open, a.terminalShown())
	}
	if a.focus != paneTerminal {
		t.Errorf("the keyboard is in %v, not in the terminal", a.focus)
	}
}

// A window that quits with the terminal in an open panel starts a
// terminal again, as a local terminal is not attached to.
func TestTerminalComesBackWithThePanel(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	root := workspaceFixture(t)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Panels.Get(settings.SideBottom).Open = true
	a := newApp(&fakeHost{folder: root}, s, w)
	t.Cleanup(a.shutdown)
	t.Cleanup(a.closeTerminals)
	tt := ui.NewTester(a.view, 1200, 760)
	for range 3 {
		step(tt)
	}
	if len(a.terms) != 1 {
		t.Fatalf("%d terminals, want one in the panel the window kept open", len(a.terms))
	}
	if !a.terminalShown() {
		t.Error("the panel does not show the terminal")
	}
}

// The View menu moves any view to any panel, the one it is in checked, and
// the menu names the views as their tabs do.
func TestViewMenuMovesViews(t *testing.T) {
	onOS(t, "linux")
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	if err := tt.Click("Menu"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("View", "Move View to Panel", "Search", "Bottom"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if at, _ := a.viewSide(settings.ViewSearch); at != settings.SideBottom {
		t.Fatalf("search is on %q", at)
	}
	if !a.bottomPanel().Open || hs.shown(settings.ViewSearch) != settings.ViewSearch {
		t.Errorf("the bottom panel: open %v, showing %q", a.bottomPanel().Open, hs.shown(settings.ViewSearch))
	}
}

// Hiding a panel the keyboard is not in leaves the keyboard where it is:
// in search's query, typing goes on.
func TestHidingAPanelKeepsTheKeyboard(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.moveView(settings.ViewSearch, settings.SideRight)
	hs.settle()
	a.run("edit.findInFiles")
	hs.settle()
	if !tt.Focused("Search") {
		t.Fatalf("the keyboard is not in search: %q", tt.Texts())
	}
	a.hidePanel(settings.SideLeft)
	hs.settle()
	if !tt.Focused("Search") {
		t.Errorf("hiding the left panel took the keyboard out of search")
	}
	// The panel the keyboard is in takes it back to the editors.
	a.hidePanel(settings.SideRight)
	hs.settle()
	if tt.Focused("Search") || a.focus != paneEditor {
		t.Errorf("hiding search's panel left the keyboard in %v", a.focus)
	}
}

// A panel whose last terminal goes stops being maximized: the panel that
// shows the terminal again takes its own room.
func TestPanelStopsMaximizedWhenItsLastTerminalGoes(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(a.closeTerminals)
	a.run("view.terminal")
	hs.settle()
	a.togglePanelMax(settings.SideBottom)
	hs.settle()
	if a.panelMax != settings.SideBottom {
		t.Fatalf("the panel is %q, not maximized", a.panelMax)
	}
	for _, s := range slices.Clone(a.terms) {
		a.removeTerminal(s)
	}
	hs.settle()
	if a.bottomPanel().Open || a.panelMax != "" {
		t.Fatalf("after the last terminal: open %v, maximized %q", a.bottomPanel().Open, a.panelMax)
	}
	a.run("view.terminal")
	hs.settle()
	if a.panelMax != "" || !a.bottomPanel().Open {
		t.Errorf("the panel came back: maximized %q, open %v", a.panelMax, a.bottomPanel().Open)
	}
}
