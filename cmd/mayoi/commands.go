package main

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
)

// command is something the menus, their shortcuts and the command
// palette do.
type command struct {
	id, label, accel string
	run              func()
}

func (a *app) commands() []command {
	doc := func(fn func(d *editor)) func() {
		return func() {
			if d := a.textEditor(); d != nil {
				fn(d)
			}
		}
	}
	cmds := []command{
		{"file.new", "New File", "CmdOrCtrl+N", a.newFile},
		{"file.openFolder", "Open Folder…", "CmdOrCtrl+O", a.chooseFolder},
		{"file.openFolderWindow", "Open Folder in New Window…", "Alt+CmdOrCtrl+O", a.chooseFolderWindow},
		// ⌃R, which the view takes itself, so that a terminal keeps it.
		{"file.openRecent", "Open Recent…", "", a.openRecentPicker},
		{"file.clearRecent", "Clear Recently Opened", "", a.clearRecent},
		{"remote.connect", "Connect to Host…", "", a.openRemotePicker},
		{"remote.openFolder", "Open Remote Folder…", "", a.openRemoteFolder},
		{"remote.reconnect", "Reconnect to Host", "", a.reconnect},
		{"remote.disconnect", "Disconnect from Host", "", a.disconnect},
		{"remote.forwardPort", "Forward a Port…", "", a.askForwardPort},
		{"file.save", "Save", "CmdOrCtrl+S", a.saveActive},
		{"file.saveAs", "Save As…", "Shift+CmdOrCtrl+S", a.saveActiveAs},
		{"file.saveAll", "Save All", "Alt+CmdOrCtrl+S", a.saveAll},
		{"file.autoSave", "Auto Save", "", a.toggleAutoSave},
		{"file.settings", "Open Settings", "CmdOrCtrl+,", a.openSettings},
		{"file.keybindings", "Open Keyboard Shortcuts", "", a.openKeybindings},
		{"file.copyPath", "Copy Path of Active File", "", func() { a.copyActivePath(false) }},
		{"file.copyRelativePath", "Copy Relative Path of Active File", "", func() { a.copyActivePath(true) }},
		{"file.reveal", revealActiveLabel(), "", a.revealActive},
		{"file.close", "Close Tab", "CmdOrCtrl+W", a.closeActive},
		{"file.reopenClosed", "Reopen Closed Tab", "Shift+CmdOrCtrl+T", a.reopenClosed},
		{"file.closeFolder", "Close Folder", "", a.closeFolder},
		{"file.closeProject", "Close Project", "", func() { a.host.closeProject() }},
		{"file.installCLI", "Install 'mayoi' Command", "", func() { a.host.installCLI() }},
		{"file.uninstallCLI", "Uninstall 'mayoi' Command", "", func() { a.host.uninstallCLI() }},
		// Undo and Redo take no keys in the menu: ⌘Z goes to the focused
		// control, the editor's history or a text field's.
		{"edit.undo", "Undo", "", a.editActive(func(d *editor) { d.undo() })},
		{"edit.redo", "Redo", "", a.editActive(func(d *editor) { d.redo() })},
		{"edit.find", "Find", "CmdOrCtrl+F", func() { a.openFind(false) }},
		{"edit.replace", "Replace", "Alt+CmdOrCtrl+F", func() { a.openFind(true) }},
		{"edit.findNext", "Find Next", "CmdOrCtrl+G", doc(func(d *editor) { a.find.open = true; a.step(d, 1) })},
		{"edit.findPrev", "Find Previous", "Shift+CmdOrCtrl+G", doc(func(d *editor) { a.find.open = true; a.step(d, -1) })},
		{"edit.findInFiles", "Find in Files", "Shift+CmdOrCtrl+F", func() { a.showSearch(false) }},
		{"edit.replaceInFiles", "Replace in Files", "Shift+CmdOrCtrl+H", func() { a.showSearch(true) }},
		{"sel.comment", "Toggle Line Comment", "CmdOrCtrl+/", a.editActive(toggleComment)},
		{"sel.indent", "Indent Lines", "CmdOrCtrl+]", a.editActive(func(d *editor) { indentLines(d, false) })},
		{"sel.outdent", "Outdent Lines", "CmdOrCtrl+[", a.editActive(func(d *editor) { indentLines(d, true) })},
		// The arrows with Alt are the editor's own keys, which the terminal
		// takes too.
		{"sel.moveUp", "Move Line Up", "", a.editActive(func(d *editor) { moveLines(d, false) })},
		{"sel.moveDown", "Move Line Down", "", a.editActive(func(d *editor) { moveLines(d, true) })},
		{"sel.copyUp", "Copy Line Up", "", a.editActive(func(d *editor) { copyLines(d, false) })},
		{"sel.copyDown", "Copy Line Down", "", a.editActive(func(d *editor) { copyLines(d, true) })},
		{"sel.deleteLine", "Delete Line", "Shift+CmdOrCtrl+K", a.editActive(deleteLines)},
		{"sel.lineBelow", "Insert Line Below", "", a.editActive(func(d *editor) { insertLine(d, false) })},
		{"sel.lineAbove", "Insert Line Above", "", a.editActive(func(d *editor) { insertLine(d, true) })},
		{"sel.line", "Select Line", "CmdOrCtrl+L", a.editActive(selectLines)},
		{"sel.indentTabs", "Indent Using Tabs", "", a.editActive(func(d *editor) { d.indent.tabs = true })},
		{"sel.indent2", "Indent Using 2 Spaces", "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 2} })},
		{"sel.indent4", "Indent Using 4 Spaces", "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 4} })},
		{"sel.indent8", "Indent Using 8 Spaces", "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 8} })},
		{"sel.camelCase", "Transform to Camel Case", "", a.editActive(camelCase)},
		{"sel.sortAsc", "Sort Lines Ascending", "", a.editActive(func(d *editor) { sortLines(d, false) })},
		{"sel.sortDesc", "Sort Lines Descending", "", a.editActive(func(d *editor) { sortLines(d, true) })},
		{"go.bracket", "Go to Bracket", "Shift+CmdOrCtrl+\\", a.editActive(goToBracket)},
		{"go.quickOpen", "Quick Open…", "CmdOrCtrl+P", func() { a.openQuick("") }},
		{"go.commands", "Command Palette…", "Shift+CmdOrCtrl+P", func() { a.openQuick(">") }},
		{"go.line", "Go to Line…", "Ctrl+G", func() { a.openQuick(":") }},
		{"go.symbol", "Go to Symbol…", "Shift+CmdOrCtrl+O", func() { a.openQuick("@") }},
		{"go.nextTab", "Next Tab", "Ctrl+Tab", func() { a.cycleTab(1) }},
		{"go.prevTab", "Previous Tab", "Ctrl+Shift+Tab", func() { a.cycleTab(-1) }},
		{"go.nextProject", "Next Project", "Shift+CmdOrCtrl+]", func() { a.host.stepProject(1) }},
		{"go.prevProject", "Previous Project", "Shift+CmdOrCtrl+[", func() { a.host.stepProject(-1) }},
		{"view.sidebar", "Toggle Sidebar", "CmdOrCtrl+B", a.toggleSidebar},
		{"view.sidebarRight", "Sidebar on Right", "", a.toggleSidebarRight},
		{"view.explorer", "Explorer", "Shift+CmdOrCtrl+E", a.showExplorer},
		{"view.search", "Search", "", func() { a.showSearch(false) }},
		{"view.scm", "Source Control", "Ctrl+Shift+G", a.showSourceControl},
		{"git.commit", "Commit", "", func() { a.commit(false) }},
		{"git.commitPush", "Commit & Push", "", func() { a.commit(true) }},
		{"git.amend", "Amend Last Commit", "", func() { a.showSourceControl(); a.startAmend() }},
		{"git.openChanges", "Open Changes", "", a.openChanges},
		{"git.openAllChanges", "Open All Changes", "", func() { a.openChangesTab(true) }},
		{"git.nextChange", "Next Change", "Alt+F5", func() { a.stepChange(1) }},
		{"git.prevChange", "Previous Change", "Shift+Alt+F5", func() { a.stepChange(-1) }},
		{"git.showChange", "Show Change", "", a.editActive(func(d *editor) { d.peekHunk(caretLine(d)) })},
		{"git.revertChange", "Revert Change", "", a.editActive(func(d *editor) { a.onCaretHunk(d, a.revertHunk) })},
		{"git.stageChange", "Stage Change", "", a.editActive(func(d *editor) { a.onCaretHunk(d, a.stageHunk) })},
		{"git.stageFile", "Stage File", "", func() { a.stage(a.activePaths()...) }},
		{"git.unstageFile", "Unstage File", "", func() { a.unstage(a.activePaths()...) }},
		{"git.stageAll", "Stage All Changes", "", a.stageAll},
		{"git.undoCommit", "Undo Last Commit", "", a.undoCommit},
		{"git.continue", "Continue Merge, Rebase or Cherry-Pick", "", a.continueOp},
		{"git.abort", "Abort Merge, Rebase or Cherry-Pick…", "", a.abortOp},
		{"git.switchBranch", "Switch Branch…", "", a.openBranchPicker},
		{"git.createBranch", "Create Branch…", "", a.askNewBranch},
		{"git.blame", "Inline Blame", "", a.toggleBlame},
		{"git.fetch", "Fetch", "", a.fetch},
		{"git.pull", "Pull", "", a.pull},
		{"git.push", "Push", "", a.push},
		{"git.refresh", "Refresh Source Control", "", a.gitChanged},
		{"view.terminal", "Toggle Terminal", "Ctrl+`", a.togglePanel},
		{"view.newTerminal", "New Terminal", "Ctrl+Shift+`", a.newTerminal},
		{"view.terminalTab", "New Terminal in Editor Area", "", func() { a.newTerminalTab(a.column(), a.workDir(), true) }},
		{"view.terminalMove", "Move Terminal to Editor Area or Panel", "", a.moveTerminal},
		{"view.terminalLockRight", "Move Terminal to Locked Right Column", "", a.moveTerminalToLockedRight},
		{"view.lockColumn", "Toggle Column Lock", "", func() { a.toggleColumnLock(a.column()) }},
		{"view.splitRight", "Split Right", "CmdOrCtrl+\\", a.splitRight},
		{"view.nextColumn", "Focus Next Column", "Alt+CmdOrCtrl+Right", func() { a.focusColumnBy(1) }},
		{"view.prevColumn", "Focus Previous Column", "Alt+CmdOrCtrl+Left", func() { a.focusColumnBy(-1) }},
		{"view.moveTabNext", "Move Tab to Next Column", "", func() { a.moveTabBy(1) }},
		{"view.moveTabPrev", "Move Tab to Previous Column", "", func() { a.moveTabBy(-1) }},
		{"view.wordWrap", "Word Wrap", "", a.toggleWordWrap},
		{"view.vim", "Vim Mode", "", a.toggleVim},
		{"view.projectTabs", "Project Tabs", "", a.toggleProjectTabs},
		{"view.projectIcons", "Project Icons", "", a.toggleProjectIcons},
		{"view.wordWrapTab", "Toggle Word Wrap in Tab", "Alt+Z", a.toggleTabWrap},
		{"view.togglePreview", "Toggle Preview", "Shift+CmdOrCtrl+V", a.togglePreview},
		// The text, or the picture shown.
		{"view.zoomIn", "Zoom In", "CmdOrCtrl+=", func() { a.zoom(1) }},
		{"view.zoomOut", "Zoom Out", "CmdOrCtrl+-", func() { a.zoom(-1) }},
		{"view.zoomReset", "Reset Zoom", "CmdOrCtrl+0", func() { a.zoom(0) }},
		{"view.appearance.system", "System", "", func() { a.setAppearance("") }},
		{"view.appearance.light", "Light", "", func() { a.setAppearance("light") }},
		{"view.appearance.dark", "Dark", "", func() { a.setAppearance("dark") }},
		{"view.theme.mayoi", "Mayoi", "", func() { a.setTheme("") }},
		{"view.theme.matrix", "The Matrix", "", func() { a.setTheme(themeMatrix) }},
		{"view.theme.claude", "Claude", "", func() { a.setTheme(themeClaude) }},
		{"view.theme.qoder", "Qoder", "", func() { a.setTheme(themeQoder) }},
	}
	for i := range cmds {
		cmds[i].accel = platformAccel(cmds[i].id, cmds[i].accel)
	}
	if !remoteSupported {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return strings.HasPrefix(c.id, "remote.") })
	}
	return cmds
}

// recentCommand is the prefix of the commands that open a recent
// folder, before the folder.
const recentCommand = "file.recent:"

func (a *app) run(id string) {
	if dir, ok := strings.CutPrefix(id, recentCommand); ok {
		a.switchFolder(dir)
		return
	}
	for _, cmd := range a.commands() {
		if cmd.id == id {
			cmd.run()
			return
		}
	}
}

// chooseFolder asks for a folder to open in the window, or shows the
// window that has it open.
func (a *app) chooseFolder() {
	a.host.openFolder(a.switchFolder)
}

// chooseFolderWindow asks for a folder to open in a window of its own.
func (a *app) chooseFolderWindow() {
	a.host.openFolder(func(dir string) { a.host.newWindow(dir) })
}

// clearRecent forgets the recent folders.
func (a *app) clearRecent() {
	a.settings.ClearRecent()
	a.host.recentChanged()
}

// switchFolder opens dir in the window, with the tabs it had when it
// closed, in place of the window's folder and its tabs, unless another
// window has it open: then that one comes to the front. With project
// tabs, a window with a folder keeps it, and dir opens in a tab of its
// own.
func (a *app) switchFolder(dir string) {
	switch {
	case a.host.showFolder(filepath.Clean(dir)):
	case a.settings.ProjectTabs && a.folder() != "":
		a.host.newWindow(dir)
	default:
		a.switchTo(dir)
	}
}

// toggleProjectTabs gathers the windows into one, as project tabs, or
// parts them into a window each.
func (a *app) toggleProjectTabs() {
	a.settings.ProjectTabs = !a.settings.ProjectTabs
	a.settings.Changed()
	a.host.projectTabsChanged()
}

// toggleProjectIcons shows each project's favicon or logo on the rail,
// or its initials again. The other windows follow through the settings
// watcher, as with the other view toggles.
func (a *app) toggleProjectIcons() {
	a.settings.ProjectIcons = !a.settings.ProjectIcons
	a.settings.Changed()
	a.invalidate()
}

// closeActive closes the tab shown, or its empty column when removable.
// Locked columns and their last file workspace stay; a lone empty ordinary
// column closes the window.
func (a *app) closeActive() {
	t := a.activeTab()
	if t == nil {
		if len(a.cols) > 1 {
			a.removeColumn(a.column())
			a.focusColumn()
			a.rememberTabs()
			return
		}
		a.host.closeWindow()
		return
	}
	a.closeTab(t, nil)
}

// moveTerminal moves the terminal that has the keyboard, or had it last,
// from the panel to a tab of the column, or from its tab to the panel.
func (a *app) moveTerminal() {
	switch t := a.activeTab(); {
	case a.focus == paneTerminal && a.panelTerm != nil:
		a.terminalToColumn(a.panelTerm, a.column(), a.column().active+1)
	case t != nil && t.term != nil:
		a.terminalToPanel(t.term)
	case a.panelTerm != nil:
		a.terminalToColumn(a.panelTerm, a.column(), a.column().active+1)
	}
}

// setAppearance makes the windows light, dark, or follow the system for
// "", in their theme: The Matrix stays dark.
func (a *app) setAppearance(appearance string) {
	a.settings.Appearance = appearance
	a.themeChanged()
}

// setTheme gives the windows Mayoi's own theme for "", The Matrix for
// "matrix" or Claude for "claude", in their appearance.
func (a *app) setTheme(theme string) {
	a.settings.Theme = theme
	a.themeChanged()
}

func (a *app) themeChanged() {
	a.settings.Changed()
	a.host.setTheme(a.settings.Theme, a.settings.Appearance)
}

// openSettings opens the settings file in a tab, as they are now. The
// preferences in it apply as it is saved.
func (a *app) openSettings() {
	if path := a.settings.Path(); path != "" {
		a.settings.Flush()
		a.openFile(path, true)
	}
}

// zoom makes the picture shown bigger or smaller, or fits it for 0, and
// otherwise the text.
func (a *app) zoom(by float32) {
	if v := a.activeViewer(); v != nil && v.markdown == nil {
		v.zoomBy(int(by))
		return
	}
	s := a.settings
	if by == 0 {
		s.FontSize = 13
	} else {
		s.FontSize = clamp(s.FontSize+by, 9, 28)
	}
	s.Changed()
	a.host.fontChanged()
}

// menu is the menu bar, which the windows share: its items run the
// command of the window they are chosen in, by run. cli adds the items
// that install the mayoi command; builds that can update themselves get
// "Check for Updates…", in the app menu on macOS and the Help menu
// elsewhere.
func (a *app) menu(cli bool, run func(id string, win *mygo.Window)) *mygo.Menu {
	byID := map[string]command{}
	for _, cmd := range a.commands() {
		byID[cmd.id] = cmd
	}
	item := func(id string) *mygo.MenuItem {
		cmd := byID[id]
		return &mygo.MenuItem{Label: cmd.label, Accelerator: a.menuAccel(cmd), Click: func(_ *mygo.MenuItem, win *mygo.Window) {
			run(id, win)
		}}
	}
	checkbox := func(id string, checked bool) *mygo.MenuItem {
		it := item(id)
		it.Type, it.Checked = mygo.MenuItemCheckbox, checked
		return it
	}
	radio := func(id string, checked bool) *mygo.MenuItem {
		it := item(id)
		it.Type, it.Checked = mygo.MenuItemRadio, checked
		return it
	}
	// The appearances, which The Matrix, always dark, has no choice of,
	// and the themes.
	appearance := func(id, ap string) *mygo.MenuItem {
		it := radio(id, a.settings.Appearance == ap)
		it.Disabled = a.settings.Theme == themeMatrix
		return it
	}
	theme := func(id, th string) *mygo.MenuItem { return radio(id, a.settings.Theme == th) }
	// The recent folders but the window's, and the box of them all.
	var recent []*mygo.MenuItem
	for _, dir := range a.settings.RecentExcept(a.root) {
		id := recentCommand + dir
		recent = append(recent, &mygo.MenuItem{Label: shortenHome(dir), Click: func(_ *mygo.MenuItem, win *mygo.Window) {
			run(id, win)
		}})
	}
	if len(recent) > 0 {
		recent = append(recent, mygo.Separator())
	}
	more := item("file.openRecent")
	more.Label = "More…"
	forget := item("file.clearRecent")
	forget.Disabled = len(a.settings.Recent) == 0
	recent = append(recent, more, mygo.Separator(), forget)
	file := []*mygo.MenuItem{
		item("file.new"), item("file.openFolder"), item("file.openFolderWindow"),
		{Label: "Open Recent", Submenu: recent},
	}
	if remoteSupported {
		// Reconnecting and disconnecting are of a remote folder's window.
		reconnect, disconnect, forward := item("remote.reconnect"), item("remote.disconnect"), item("remote.forwardPort")
		reconnect.Disabled, disconnect.Disabled, forward.Disabled = a.conn == nil, a.conn == nil, a.conn == nil
		file = append(file, &mygo.MenuItem{Label: "Remote SSH", Submenu: []*mygo.MenuItem{
			item("remote.connect"), item("remote.openFolder"), mygo.Separator(), reconnect, disconnect, mygo.Separator(), forward,
		}})
	}
	file = append(file, mygo.Separator(),
		item("file.save"), item("file.saveAs"), item("file.saveAll"), checkbox("file.autoSave", a.settings.AutoSave), item("file.settings"), item("file.keybindings"), mygo.Separator(),
		item("file.close"), item("file.reopenClosed"), item("file.closeFolder"), item("file.closeProject"),
	)
	if cli {
		file = append(file, mygo.Separator(), item("file.installCLI"), item("file.uninstallCLI"))
	}
	// The app menu of macOS, as RoleAppMenu makes it, with room for
	// "Check for Updates…" after About.
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	var help []*mygo.MenuItem
	if mygo.Updater.Enabled() {
		if runtime.GOOS == "darwin" {
			appMenu = &mygo.MenuItem{Label: mygo.App.Name(), Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout}, updater.MenuItem(), mygo.Separator(),
				{Role: mygo.RoleServices}, mygo.Separator(),
				{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(),
				{Role: mygo.RoleQuit},
			}}
		} else {
			help = append(help, updater.MenuItem())
		}
	}
	menus := []*mygo.MenuItem{
		appMenu,
		{Label: "File", Submenu: file},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			item("edit.undo"), item("edit.redo"), mygo.Separator(),
			{Role: mygo.RoleCut}, {Role: mygo.RoleCopy}, {Role: mygo.RolePaste}, {Role: mygo.RoleSelectAll}, mygo.Separator(),
			item("edit.find"), item("edit.replace"), item("edit.findNext"), item("edit.findPrev"), mygo.Separator(),
			item("edit.findInFiles"), item("edit.replaceInFiles"),
		}},
		{Label: "Selection", Submenu: []*mygo.MenuItem{
			item("sel.line"), mygo.Separator(),
			item("sel.comment"), item("sel.indent"), item("sel.outdent"), mygo.Separator(),
			item("sel.moveUp"), item("sel.moveDown"), item("sel.copyUp"), item("sel.copyDown"), mygo.Separator(),
			item("sel.lineBelow"), item("sel.lineAbove"), item("sel.deleteLine"), mygo.Separator(),
			item("sel.sortAsc"), item("sel.sortDesc"), item("sel.camelCase"),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			item("view.sidebar"), checkbox("view.sidebarRight", a.settings.SidebarRight), item("view.explorer"), item("view.search"), item("view.scm"), mygo.Separator(),
			checkbox("view.wordWrap", a.settings.WordWrap), item("view.wordWrapTab"), item("view.togglePreview"),
			checkbox("view.vim", a.settings.Vim), checkbox("view.projectTabs", a.settings.ProjectTabs), checkbox("view.projectIcons", a.settings.ProjectIcons), mygo.Separator(),
			item("view.splitRight"), item("view.nextColumn"), item("view.prevColumn"),
			item("view.moveTabNext"), item("view.moveTabPrev"), mygo.Separator(),
			item("view.terminal"), item("view.newTerminal"), item("view.terminalTab"), item("view.terminalMove"), mygo.Separator(),
			item("view.zoomIn"), item("view.zoomOut"), item("view.zoomReset"), mygo.Separator(),
			{Label: "Theme", Submenu: []*mygo.MenuItem{
				appearance("view.appearance.system", ""), appearance("view.appearance.light", "light"), appearance("view.appearance.dark", "dark"),
				mygo.Separator(), theme("view.theme.mayoi", ""), theme("view.theme.matrix", themeMatrix), theme("view.theme.claude", themeClaude),
				theme("view.theme.qoder", themeQoder),
			}}, mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen}, {Role: mygo.RoleToggleDevTools},
		}},
		{Label: "Go", Submenu: []*mygo.MenuItem{
			item("go.quickOpen"), item("go.commands"), item("go.symbol"), item("go.line"), item("go.bracket"), mygo.Separator(),
			item("go.nextTab"), item("go.prevTab"), mygo.Separator(),
			item("go.nextProject"), item("go.prevProject"),
		}},
		{Label: "Git", Submenu: []*mygo.MenuItem{
			item("git.commit"), item("git.commitPush"), item("git.amend"), item("git.undoCommit"), mygo.Separator(),
			item("git.openChanges"), item("git.openAllChanges"), item("git.nextChange"), item("git.prevChange"), mygo.Separator(),
			item("git.showChange"), item("git.revertChange"), item("git.stageChange"), mygo.Separator(),
			item("git.stageFile"), item("git.unstageFile"), item("git.stageAll"), mygo.Separator(),
			item("git.switchBranch"), item("git.createBranch"), item("git.fetch"), item("git.pull"), item("git.push"), mygo.Separator(),
			checkbox("git.blame", a.settings.Blame), item("git.refresh"),
		}},
		{Role: mygo.RoleWindowMenu},
	}
	if len(help) > 0 {
		menus = append(menus, &mygo.MenuItem{Label: "Help", Submenu: help})
	}
	return mygo.NewMenu(menus)
}

// menuAccel is the accelerator of a command's menu item: none when
// keybindings.json binds its keys, which the menu would take first, or
// takes them away from it.
func (a *app) menuAccel(cmd command) string {
	st, ok := keymap.ParseAccel(cmd.accel)
	if ok && (a.keys.Binds(st) || a.keys.Removes(commandNames(cmd.id)...)) {
		return ""
	}
	return cmd.accel
}

// terminalFocused reports whether a terminal has the keyboard, of the
// panel or of a tab.
func (a *app) terminalFocused(c *ui.Context) bool {
	for _, t := range a.terms {
		if t.view.FocusWithin(c) {
			return true
		}
	}
	return false
}

// shortcuts are the keys the view takes itself.
func (a *app) shortcuts(c *ui.Context) {
	// ⌃R, unless a terminal has the keyboard: shells search their history
	// with it. Vim's normal mode redoes with it.
	if !a.terminalFocused(c) && !a.vimTakesCtrl(c) && !a.keyBound(ui.Ctrl, ui.KeyR) && c.Shortcut(ui.Ctrl, ui.KeyR) {
		a.openRecentPicker()
	}
	// ⌘1…9, Alt+1…9 on Windows and Linux, shows a tab of the column, as in
	// browsers.
	for k := ui.Key1; k <= ui.Key9; k++ {
		if !a.keyBound(tabMod(), k) && c.Shortcut(tabMod(), k) {
			a.showTabKey(k)
		}
		// ⌃⌘1…9, Ctrl+Alt+1…9 on Windows and Linux, shows a project of
		// the rail.
		if mods := tabMod() | ui.Ctrl; a.settings.ProjectTabs && !a.keyBound(mods, k) && c.Shortcut(mods, k) {
			if k == ui.Key9 {
				a.host.showProject(-1)
			} else {
				a.host.showProject(int(k - ui.Key1))
			}
		}
	}
	a.menuKeys(c)
}

// showTabKey shows the tab of the column of digit k, 9 the last.
func (a *app) showTabKey(k ui.Key) {
	col := a.column()
	i := int(k - ui.Key1)
	if k == ui.Key9 {
		i = len(col.tabs) - 1
	}
	a.activate(col, i, true)
}
