package main

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/locales"
)

// command is something the menus, their shortcuts and the command
// palette do.
type command struct {
	id, label, accel string
	run              func()
}

func (a *app) commands() []command { return a.commandsIn(locales.In(i18n.Locale())) }

// commandsIn are the commands, labelled in the language of t: the palette
// also matches their English names.
func (a *app) commandsIn(t locales.Messages) []command {
	doc := func(fn func(d *editor)) func() {
		return func() {
			if d := a.textEditor(); d != nil {
				fn(d)
			}
		}
	}
	cmds := []command{
		{"file.new", t.CmdFileNew(), "CmdOrCtrl+N", a.newFile},
		{"file.newWindow", t.CmdFileNewWindow(), "Shift+CmdOrCtrl+N", func() { a.host.newBlankWindow() }},
		{"file.openFolder", t.CmdFileOpenFolder(), "CmdOrCtrl+O", a.chooseFolder},
		{"file.openFolderWindow", t.CmdFileOpenFolderWindow(), "Alt+CmdOrCtrl+O", a.chooseFolderWindow},
		// ⌃R, which the view takes itself, so that a terminal keeps it.
		{"file.openRecent", t.CmdFileOpenRecent(), "", a.openRecentPicker},
		{"file.clearRecent", t.CmdFileClearRecent(), "", a.clearRecent},
		{"remote.connect", t.CmdRemoteConnect(), "", a.openRemotePicker},
		{"remote.openFolder", t.CmdRemoteOpenFolder(), "", a.openRemoteFolder},
		{"remote.reconnect", t.CmdRemoteReconnect(), "", a.reconnect},
		{"remote.disconnect", t.CmdRemoteDisconnect(), "", a.disconnect},
		{"remote.forwardPort", t.CmdRemoteForwardPort(), "", a.askForwardPort},
		{"file.save", t.CmdFileSave(), "CmdOrCtrl+S", a.saveActive},
		{"file.saveAs", t.CmdFileSaveAs(), "Shift+CmdOrCtrl+S", a.saveActiveAs},
		{"file.saveAll", t.CmdFileSaveAll(), "Alt+CmdOrCtrl+S", a.saveAll},
		{"file.autoSave", t.CmdFileAutoSave(), "", a.toggleAutoSave},
		{"file.settings", t.CmdFileSettings(), "CmdOrCtrl+,", a.openSettings},
		{"file.keybindings", t.CmdFileKeybindings(), "", a.openKeybindings},
		{"file.copyPath", t.CmdFileCopyPath(), "", func() { a.copyActivePath(false) }},
		{"file.copyRelativePath", t.CmdFileCopyRelativePath(), "", func() { a.copyActivePath(true) }},
		{"file.reveal", revealActiveLabel(t), "", a.revealActive},
		{"file.close", t.CmdFileClose(), "CmdOrCtrl+W", a.closeActive},
		{"file.reopenClosed", t.CmdFileReopenClosed(), "Shift+CmdOrCtrl+T", a.reopenClosed},
		{"file.closeFolder", t.CmdFileCloseFolder(), "", a.closeFolder},
		{"file.closeProject", t.CmdFileCloseProject(), "", func() { a.host.closeProject() }},
		{"account.signIn", t.CmdAccountSignIn(), "", func() { a.host.signIn() }},
		{"account.show", t.CmdAccountShow(), "", a.showSync},
		{"account.syncNow", t.CmdAccountSyncNow(), "", a.syncNow},
		{"account.compareKeybindings", t.CmdAccountCompareKeybindings(), "", a.compareKeybindings},
		{"account.manage", t.CmdAccountManage(), "", func() { a.services.OpenURL(monURL()) }},
		{"account.signOut", t.CmdAccountSignOut(), "", a.confirmSignOut},
		{"file.installCLI", t.CmdFileInstallCLI(), "", func() { a.host.installCLI() }},
		{"file.uninstallCLI", t.CmdFileUninstallCLI(), "", func() { a.host.uninstallCLI() }},
		// Undo and Redo take no keys in the menu: ⌘Z goes to the focused
		// control, the editor's history or a text field's.
		{"edit.undo", t.CmdEditUndo(), "", a.editActive(func(d *editor) { d.undo() })},
		{"edit.redo", t.CmdEditRedo(), "", a.editActive(func(d *editor) { d.redo() })},
		{"edit.cut", t.CmdEditCut(), "CmdOrCtrl+X", a.editActive(a.cut)},
		{"edit.find", t.CmdEditFind(), "CmdOrCtrl+F", func() { a.openFind(false) }},
		{"edit.replace", t.CmdEditReplace(), "Alt+CmdOrCtrl+F", func() { a.openFind(true) }},
		{"edit.findNext", t.CmdEditFindNext(), "CmdOrCtrl+G", doc(func(d *editor) { a.find.open = true; a.step(d, 1) })},
		{"edit.findPrev", t.CmdEditFindPrev(), "Shift+CmdOrCtrl+G", doc(func(d *editor) { a.find.open = true; a.step(d, -1) })},
		{"edit.findInFiles", t.CmdEditFindInFiles(), "Shift+CmdOrCtrl+F", func() { a.showSearch(false) }},
		{"edit.replaceInFiles", t.CmdEditReplaceInFiles(), "Shift+CmdOrCtrl+H", func() { a.showSearch(true) }},
		{"sel.comment", t.CmdSelComment(), "CmdOrCtrl+/", a.editActive(toggleComment)},
		{"sel.indent", t.CmdSelIndent(), "CmdOrCtrl+]", a.editActive(func(d *editor) { indentLines(d, false) })},
		{"sel.outdent", t.CmdSelOutdent(), "CmdOrCtrl+[", a.editActive(func(d *editor) { indentLines(d, true) })},
		// The arrows with Alt are the editor's own keys, which the terminal
		// takes too.
		{"sel.moveUp", t.CmdSelMoveUp(), "", a.editActive(func(d *editor) { moveLines(d, false) })},
		{"sel.moveDown", t.CmdSelMoveDown(), "", a.editActive(func(d *editor) { moveLines(d, true) })},
		{"sel.copyUp", t.CmdSelCopyUp(), "", a.editActive(func(d *editor) { copyLines(d, false) })},
		{"sel.copyDown", t.CmdSelCopyDown(), "", a.editActive(func(d *editor) { copyLines(d, true) })},
		{"sel.deleteLine", t.CmdSelDeleteLine(), "Shift+CmdOrCtrl+K", a.editActive(deleteLines)},
		{"sel.lineBelow", t.CmdSelLineBelow(), "", a.editActive(func(d *editor) { insertLine(d, false) })},
		{"sel.lineAbove", t.CmdSelLineAbove(), "", a.editActive(func(d *editor) { insertLine(d, true) })},
		{"sel.line", t.CmdSelLine(), "CmdOrCtrl+L", a.editActive(selectLines)},
		{"sel.indentTabs", t.CmdSelIndentTabs(), "", a.editActive(func(d *editor) { d.indent.tabs = true })},
		{"sel.indent2", t.CmdSelIndentSpaces(2), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 2} })},
		{"sel.indent4", t.CmdSelIndentSpaces(4), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 4} })},
		{"sel.indent8", t.CmdSelIndentSpaces(8), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 8} })},
		{"sel.camelCase", t.CmdSelCamelCase(), "", a.editActive(camelCase)},
		{"sel.sortAsc", t.CmdSelSortAsc(), "", a.editActive(func(d *editor) { sortLines(d, false) })},
		{"sel.sortDesc", t.CmdSelSortDesc(), "", a.editActive(func(d *editor) { sortLines(d, true) })},
		{"go.bracket", t.CmdGoBracket(), "Shift+CmdOrCtrl+\\", a.editActive(goToBracket)},
		{"go.quickOpen", t.CmdGoQuickOpen(), "CmdOrCtrl+P", func() { a.openQuick("") }},
		{"go.commands", t.CmdGoCommands(), "Shift+CmdOrCtrl+P", func() { a.openQuick(">") }},
		{"go.line", t.CmdGoLine(), "Ctrl+G", func() { a.openQuick(":") }},
		{"go.symbol", t.CmdGoSymbol(), "Shift+CmdOrCtrl+O", func() { a.openQuick("@") }},
		{"go.nextTab", t.CmdGoNextTab(), "Ctrl+Tab", func() { a.cycleTab(1) }},
		{"go.prevTab", t.CmdGoPrevTab(), "Ctrl+Shift+Tab", func() { a.cycleTab(-1) }},
		{"go.nextProject", t.CmdGoNextProject(), "Shift+CmdOrCtrl+]", func() { a.host.stepProject(1) }},
		{"go.prevProject", t.CmdGoPrevProject(), "Shift+CmdOrCtrl+[", func() { a.host.stepProject(-1) }},
		{"view.sidebar", t.CmdViewSidebar(), "CmdOrCtrl+B", a.toggleSidebar},
		{"view.sidebarRight", t.CmdViewSidebarRight(), "", a.toggleSidebarRight},
		{"view.explorer", t.CmdViewExplorer(), "Shift+CmdOrCtrl+E", a.showExplorer},
		{"view.search", t.CmdViewSearch(), "", func() { a.showSearch(false) }},
		{"view.scm", t.CmdViewScm(), "Ctrl+Shift+G", a.showSourceControl},
		{"git.commit", t.CmdGitCommit(), "", func() { a.commit(false) }},
		{"git.commitPush", t.CmdGitCommitPush(), "", func() { a.commit(true) }},
		{"git.amend", t.CmdGitAmend(), "", func() { a.showSourceControl(); a.startAmend() }},
		{"git.writeMessage", t.CmdGitWriteMessage(), "", func() { a.showSourceControl(); a.writeMessage() }},
		{"git.openChanges", t.CmdGitOpenChanges(), "", a.openChanges},
		{"git.openAllChanges", t.CmdGitOpenAllChanges(), "", func() { a.openChangesTab(true) }},
		{"git.compareFiles", t.CmdGitCompareFiles(), "", a.compareLocalFiles},
		{"git.solveConflicts", t.CmdGitSolveConflicts(), "", a.solveActiveConflicts},
		{"git.conflictFiles", t.CmdGitConflictFiles(), "", a.openConflicts},
		{"git.nextChange", t.CmdGitNextChange(), "Alt+F5", func() { a.stepChange(1) }},
		{"git.prevChange", t.CmdGitPrevChange(), "Shift+Alt+F5", func() { a.stepChange(-1) }},
		{"git.nextFile", t.CmdGitNextFile(), "", func() { a.stepDiffFile(1) }},
		{"git.prevFile", t.CmdGitPrevFile(), "", func() { a.stepDiffFile(-1) }},
		{"git.toggleCommitFiles", t.CmdGitToggleCommitFiles(), "", a.toggleCommitFiles},
		{"git.appendChange", t.CmdGitAppendChange(), "", a.appendAtCursor},
		{"git.showChange", t.CmdGitShowChange(), "", a.editActive(func(d *editor) { d.peekHunk(caretLine(d)) })},
		{"git.revertChange", t.CmdGitRevertChange(), "", a.editActive(func(d *editor) { a.onCaretHunk(d, a.revertHunk) })},
		{"git.stageChange", t.CmdGitStageChange(), "", a.editActive(func(d *editor) { a.onCaretHunk(d, a.stageHunk) })},
		{"git.stageFile", t.CmdGitStageFile(), "", func() {
			if paths := a.activePaths(); len(paths) > 0 {
				a.repositoryForPath(paths[0]).stage(paths...)
			}
		}},
		{"git.unstageFile", t.CmdGitUnstageFile(), "", func() {
			if paths := a.activePaths(); len(paths) > 0 {
				a.repositoryForPath(paths[0]).unstage(paths...)
			}
		}},
		{"git.stageAll", t.CmdGitStageAll(), "", a.stageAll},
		{"git.undoCommit", t.CmdGitUndoCommit(), "", a.undoCommit},
		{"git.continue", t.CmdGitContinue(), "", a.continueOp},
		{"git.abort", t.CmdGitAbort(), "", a.abortOp},
		{"git.switchBranch", t.CmdGitSwitchBranch(), "", a.openBranchPicker},
		{"git.createBranch", t.CmdGitCreateBranch(), "", a.askNewBranch},
		{"git.blame", t.CmdGitBlame(), "", a.toggleBlame},
		{"git.fetch", t.CmdGitFetch(), "", a.fetch},
		{"git.pull", t.CmdGitPull(), "", a.pull},
		{"git.push", t.CmdGitPush(), "", a.push},
		{"git.refresh", t.CmdGitRefresh(), "", a.gitChanged},
		{"review.comment", t.CmdReviewComment(), "Shift+CmdOrCtrl+M", a.addReviewComment},
		{"review.next", t.CmdReviewNext(), "", func() { a.stepNote(1) }},
		{"review.previous", t.CmdReviewPrevious(), "", func() { a.stepNote(-1) }},
		{"review.copy", t.CmdReviewCopy(), "", a.copyReview},
		{"review.send", t.CmdReviewSend(), "", a.sendReview},
		{"review.clear", t.CmdReviewClear(), "", a.clearReview},
		{"view.terminal", t.CmdViewTerminal(), "Ctrl+`", a.togglePanel},
		{"view.newTerminal", t.CmdViewNewTerminal(), "Ctrl+Shift+`", func() { a.openTerminal(a.workDir()) }},
		{"view.terminalTab", t.CmdViewTerminalTab(), "", func() { a.newTerminalTab(a.fileColumn(), a.workDir(), true) }},
		{"view.terminalMove", t.CmdViewTerminalMove(), "", a.moveTerminal},
		{"view.terminalLocation", t.CmdViewTerminalLocation(), "", a.toggleTerminalLocation},
		{"view.terminalLockRight", t.CmdViewTerminalLockRight(), "", a.moveTerminalToLockedRight},
		{"view.panelPosition.bottom", t.CmdViewPanelBottom(), "", func() { a.setPanelPosition("") }},
		{"view.panelPosition.left", t.CmdViewPanelLeft(), "", func() { a.setPanelPosition("left") }},
		{"view.panelPosition.right", t.CmdViewPanelRight(), "", func() { a.setPanelPosition("right") }},
		{"view.lockColumn", t.CmdViewLockColumn(), "", func() { a.toggleColumnLock(a.column()) }},
		{"view.splitRight", t.CmdViewSplitRight(), "CmdOrCtrl+\\", a.splitRight},
		{"view.nextColumn", t.CmdViewNextColumn(), "Alt+CmdOrCtrl+Right", func() { a.focusColumnBy(1) }},
		{"view.prevColumn", t.CmdViewPrevColumn(), "Alt+CmdOrCtrl+Left", func() { a.focusColumnBy(-1) }},
		{"view.moveTabNext", t.CmdViewMoveTabNext(), "", func() { a.moveTabBy(1) }},
		{"view.moveTabPrev", t.CmdViewMoveTabPrev(), "", func() { a.moveTabBy(-1) }},
		{"view.wordWrap", t.CmdViewWordWrap(), "", a.toggleWordWrap},
		{"view.vim", t.CmdViewVim(), "", a.toggleVim},
		{"view.projectTabs", t.CmdViewProjectTabs(), "", a.toggleProjectTabs},
		{"view.wordWrapTab", t.CmdViewWordWrapTab(), "Alt+Z", a.toggleTabWrap},
		{"view.togglePreview", t.CmdViewTogglePreview(), "Shift+CmdOrCtrl+V", a.togglePreview},
		{"view.previewToSide", t.CmdViewPreviewToSide(), "", a.previewToSide},
		// The text, or the picture shown.
		{"view.zoomIn", t.CmdViewZoomIn(), "CmdOrCtrl+=", func() { a.zoom(1) }},
		{"view.zoomOut", t.CmdViewZoomOut(), "CmdOrCtrl+-", func() { a.zoom(-1) }},
		{"view.zoomReset", t.CmdViewZoomReset(), "CmdOrCtrl+0", func() { a.zoom(0) }},
		{"view.appearance.system", t.CmdViewAppearanceSystem(), "", func() { a.setAppearance("") }},
		{"view.appearance.light", t.CmdViewAppearanceLight(), "", func() { a.setAppearance("light") }},
		{"view.appearance.dark", t.CmdViewAppearanceDark(), "", func() { a.setAppearance("dark") }},
		{"view.selectTheme", t.CmdViewSelectTheme(), "", a.openThemePicker},
		{"view.theme.mayoi", "Mayoi", "", func() { a.setTheme("") }},
		{"view.theme.matrix", t.CmdViewThemeMatrix(), "", func() { a.setTheme(themeMatrix) }},
		{"view.theme.claude", "Claude", "", func() { a.setTheme(themeClaude) }},
		{"view.theme.qoder", "Qoder", "", func() { a.setTheme(themeQoder) }},
		{"view.theme.grokbot", "Grok Bot", "", func() { a.setTheme(themeGrokBot) }},
		{localeCommand + "system", t.CmdViewLocaleSystem(), "", func() { a.setLocale("") }},
	}
	// The languages, each named in itself.
	for _, tag := range mygo.App.Languages() {
		cmds = append(cmds, command{localeCommand + tag, i18n.Autonym(tag), "", func() { a.setLocale(tag) }})
	}
	for i := range cmds {
		cmds[i].accel = platformAccel(cmds[i].id, cmds[i].accel)
	}
	if !remoteSupported {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return strings.HasPrefix(c.id, "remote.") })
	}
	return cmds
}

// solveActiveConflicts shows the three-way merge of the file of the tab
// shown when it is in conflict.
func (a *app) solveActiveConflicts() {
	if t := a.activeTab(); t != nil && t.path() != "" {
		a.openMerge(t.path(), true)
	}
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

// localeCommand is the prefix of the commands that choose the language,
// before its tag or "system".
const localeCommand = "view.locale."

// setLocale shows the app in the language of tag, or in the system's for
// "", in every window.
func (a *app) setLocale(tag string) {
	a.settings.Locale = tag
	a.settings.Changed()
	a.host.setLocale(tag)
}

// localeOn reports whether the settings choose the language of tag, of
// the app's languages, or the system's for "system": "zh" in the settings
// chooses "zh-Hans".
func (a *app) localeOn(tag string) bool {
	if tag == "system" || a.settings.Locale == "" {
		return tag == "system" && a.settings.Locale == ""
	}
	return i18n.Match([]string{a.settings.Locale}, mygo.App.Languages()) == tag
}

// syncNow syncs the settings with the account at once, or signs in to
// sync them.
func (a *app) syncNow() {
	if a.host.account() == "" {
		a.host.signIn()
		return
	}
	a.host.syncNow()
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

// updateItem is "Check for Updates…", or "Update to 0.12.0…" once a
// check found that version.
func (a *app) updateItem() *mygo.MenuItem {
	it := updater.MenuItem()
	if v := a.host.update(); v != "" {
		it.Label = locales.MenuUpdateTo(v)
	}
	return it
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
	// The system's language, then each of the app's.
	languages := []*mygo.MenuItem{radio(localeCommand+"system", a.localeOn("system")), mygo.Separator()}
	for _, tag := range mygo.App.Languages() {
		languages = append(languages, radio(localeCommand+tag, a.localeOn(tag)))
	}
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
	more.Label = locales.MenuOpenRecentMore()
	forget := item("file.clearRecent")
	forget.Disabled = len(a.settings.Recent) == 0
	recent = append(recent, more, mygo.Separator(), forget)
	file := []*mygo.MenuItem{
		item("file.new"), item("file.newWindow"), item("file.openFolder"), item("file.openFolderWindow"),
		{Label: locales.MenuOpenRecent(), Submenu: recent},
	}
	if remoteSupported {
		// Reconnecting and disconnecting are of a remote folder's window.
		reconnect, disconnect, forward := item("remote.reconnect"), item("remote.disconnect"), item("remote.forwardPort")
		reconnect.Disabled, disconnect.Disabled, forward.Disabled = a.conn == nil, a.conn == nil, a.conn == nil
		file = append(file, &mygo.MenuItem{Label: locales.MenuRemoteSSH(), Submenu: []*mygo.MenuItem{
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
	// Settings sync: who it syncs for, or signing in.
	var account []*mygo.MenuItem
	if name := a.host.account(); name != "" {
		account = []*mygo.MenuItem{
			{Label: locales.MenuSyncingAs(name), Disabled: true}, item("account.syncNow"), item("account.manage"), item("account.signOut"),
		}
	} else {
		account = []*mygo.MenuItem{item("account.signIn")}
	}
	// The app menu of macOS, as RoleAppMenu makes it, with room for
	// "Check for Updates…" after About and for settings sync, and the Help
	// menu elsewhere with them.
	var help []*mygo.MenuItem
	about := []*mygo.MenuItem{{Role: mygo.RoleAbout}}
	if mygo.Updater.Enabled() {
		about = append(about, a.updateItem())
		help = append(help, a.updateItem(), mygo.Separator())
	}
	help = append(help, account...)
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	if runtime.GOOS == "darwin" {
		help = nil
		appMenu = &mygo.MenuItem{Label: mygo.App.Name(), Submenu: slices.Concat(about, []*mygo.MenuItem{mygo.Separator()}, account, []*mygo.MenuItem{
			mygo.Separator(),
			{Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(),
			{Role: mygo.RoleQuit},
		})}
	}
	// The items of the review comments, off while there are none.
	reviewItem := func(id string) *mygo.MenuItem {
		it := item(id)
		it.Disabled = len(a.review.notes) == 0
		return it
	}
	cut := &mygo.MenuItem{Role: mygo.RoleCut}
	if a.keyFocus == placeEditor {
		cut = item("edit.cut")
	}
	menus := []*mygo.MenuItem{
		appMenu,
		{Label: locales.MenuFile(), Submenu: file},
		{Label: locales.MenuEdit(), Submenu: []*mygo.MenuItem{
			item("edit.undo"), item("edit.redo"), mygo.Separator(),
			cut, {Role: mygo.RoleCopy}, {Role: mygo.RolePaste}, {Role: mygo.RoleSelectAll}, mygo.Separator(),
			item("edit.find"), item("edit.replace"), item("edit.findNext"), item("edit.findPrev"), mygo.Separator(),
			item("edit.findInFiles"), item("edit.replaceInFiles"),
		}},
		{Label: locales.MenuSelection(), Submenu: []*mygo.MenuItem{
			item("sel.line"), mygo.Separator(),
			item("sel.comment"), item("sel.indent"), item("sel.outdent"), mygo.Separator(),
			item("sel.moveUp"), item("sel.moveDown"), item("sel.copyUp"), item("sel.copyDown"), mygo.Separator(),
			item("sel.lineBelow"), item("sel.lineAbove"), item("sel.deleteLine"), mygo.Separator(),
			item("sel.sortAsc"), item("sel.sortDesc"), item("sel.camelCase"),
		}},
		{Label: locales.MenuView(), Submenu: []*mygo.MenuItem{
			item("view.sidebar"), checkbox("view.sidebarRight", a.settings.SidebarRight), item("view.explorer"), item("view.search"), item("view.scm"), mygo.Separator(),
			checkbox("view.wordWrap", a.settings.WordWrap), item("view.wordWrapTab"), item("view.togglePreview"), item("view.previewToSide"),
			checkbox("view.vim", a.settings.Vim), checkbox("view.projectTabs", a.settings.ProjectTabs), mygo.Separator(),
			item("view.splitRight"), item("view.nextColumn"), item("view.prevColumn"),
			item("view.moveTabNext"), item("view.moveTabPrev"), mygo.Separator(),
			item("view.terminal"), item("view.newTerminal"), item("view.terminalTab"), item("view.terminalMove"),
			checkbox("view.terminalLocation", a.settings.TerminalLocation == "editor"),
			{Label: locales.MenuPanelPosition(), Submenu: []*mygo.MenuItem{
				radio("view.panelPosition.bottom", a.panelSide() == "bottom"),
				radio("view.panelPosition.left", a.panelSide() == "left"),
				radio("view.panelPosition.right", a.panelSide() == "right"),
			}}, mygo.Separator(),
			item("view.zoomIn"), item("view.zoomOut"), item("view.zoomReset"), mygo.Separator(),
			{Label: locales.MenuTheme(), Submenu: []*mygo.MenuItem{
				item("view.selectTheme"), mygo.Separator(),
				appearance("view.appearance.system", ""), appearance("view.appearance.light", "light"), appearance("view.appearance.dark", "dark"),
				mygo.Separator(), theme("view.theme.mayoi", ""), theme("view.theme.matrix", themeMatrix), theme("view.theme.claude", themeClaude),
				theme("view.theme.qoder", themeQoder), theme("view.theme.grokbot", themeGrokBot),
			}},
			{Label: locales.MenuLanguage(), Submenu: languages}, mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen}, {Role: mygo.RoleToggleDevTools},
		}},
		{Label: locales.MenuGo(), Submenu: []*mygo.MenuItem{
			item("go.quickOpen"), item("go.commands"), item("go.symbol"), item("go.line"), item("go.bracket"), mygo.Separator(),
			item("go.nextTab"), item("go.prevTab"), mygo.Separator(),
			item("go.nextProject"), item("go.prevProject"),
		}},
		{Label: locales.MenuGit(), Submenu: []*mygo.MenuItem{
			item("git.commit"), item("git.commitPush"), item("git.amend"), item("git.undoCommit"), mygo.Separator(),
			item("git.openChanges"), item("git.openAllChanges"), item("git.compareFiles"), item("git.nextChange"), item("git.prevChange"), item("git.nextFile"), item("git.prevFile"),
			checkbox("git.toggleCommitFiles", a.window.CommitFilesOpen), mygo.Separator(),
			item("git.showChange"), item("git.revertChange"), item("git.appendChange"), item("git.stageChange"), mygo.Separator(),
			item("git.stageFile"), item("git.unstageFile"), item("git.stageAll"), mygo.Separator(),
			item("git.switchBranch"), item("git.createBranch"), item("git.fetch"), item("git.pull"), item("git.push"), mygo.Separator(),
			checkbox("git.blame", a.settings.Blame), item("git.refresh"), mygo.Separator(),
			item("review.comment"), reviewItem("review.next"), reviewItem("review.previous"),
			reviewItem("review.copy"), reviewItem("review.send"), reviewItem("review.clear"),
		}},
		{Role: mygo.RoleWindowMenu},
	}
	if len(help) > 0 {
		menus = append(menus, &mygo.MenuItem{Label: locales.MenuHelp(), Submenu: help})
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

// appUIMenu fills the titlebar's menu on Windows and Linux, which have no
// menu bar, with the menu bar's items.
func (a *app) appUIMenu(m *ui.Menu) {
	menu := a.menu(false, func(id string, _ *mygo.Window) {
		a.post(func() { a.run(id) })
	})
	a.populateUIMenu(m, menu.Items())
}

// uiMenuSkips are the roles the titlebar's menu leaves out, which mygo
// does for the menu bar but an app cannot yet: the edit roles go to the
// focused element, so Copy would copy the editor's selection while a
// terminal has the keyboard, and native UI's inspector has no API. Their
// shortcuts and the context menus still have the edit roles.
var uiMenuSkips = map[mygo.MenuRole]bool{
	mygo.RoleCut: true, mygo.RoleCopy: true, mygo.RolePaste: true, mygo.RoleSelectAll: true,
	mygo.RoleToggleDevTools: true,
}

// populateUIMenu adds items to m, with a separator only between two items.
func (a *app) populateUIMenu(m *ui.Menu, items []*mygo.MenuItem) {
	added, sep := false, false
	for _, it := range items {
		if it.Type == mygo.MenuItemSeparator {
			sep = added
			continue
		}
		if it.Hidden || it.Label == "" || uiMenuSkips[it.Role] {
			continue
		}
		if sep {
			m.Separator()
		}
		added, sep = true, false
		if len(it.Submenu) > 0 {
			m.Submenu(it.Label, func(sub *ui.Menu) {
				a.populateUIMenu(sub, it.Submenu)
			})
			continue
		}
		item := m.Item(it.Label).Disabled(it.Disabled)
		if it.Type == mygo.MenuItemCheckbox || it.Type == mygo.MenuItemRadio {
			item.Checked(it.Checked)
		}
		if st, ok := keymap.ParseAccel(it.Accelerator); ok {
			item.Shortcut(st.Mods, st.Key)
		}
		if item.Chosen() {
			a.post(func() {
				if it.Click != nil {
					it.Click(it, nil)
				} else {
					a.handleRole(it.Role)
				}
			})
		}
	}
}

// handleRole does a role of the titlebar's menu that has no command.
func (a *app) handleRole(r mygo.MenuRole) {
	h, ok := a.host.(*desktop)
	if !ok || h.f == nil || h.f.win == nil {
		return
	}
	w := h.f.win
	switch r {
	case mygo.RoleMinimize:
		w.Minimize()
	case mygo.RoleClose:
		w.Close()
	case mygo.RoleToggleFullScreen:
		w.SetFullScreen(!w.IsFullScreen())
	}
}
