package main

import (
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/lsp"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
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
	cmds := []command{
		{"file.new", t.CmdFileNew(), "CmdOrCtrl+N", a.newFile},
		{"file.newWindow", t.CmdFileNewWindow(), "Shift+CmdOrCtrl+N", func() { a.host.newBlankWindow() }},
		{"file.openFolder", t.CmdFileOpenFolder(), "CmdOrCtrl+O", a.chooseFolder},
		{"file.openFolderWindow", t.CmdFileOpenFolderWindow(), "Alt+CmdOrCtrl+O", a.chooseFolderWindow},
		// ⌃R, which the view takes itself, so that a terminal keeps it.
		{"file.openRecent", t.CmdFileOpenRecent(), "", a.openRecentPicker},
		{"file.clearRecent", t.CmdFileClearRecent(), "", a.clearRecent},
		{"remote.connect", t.CmdRemoteConnect(), "", a.openRemotePicker},
		{"remote.openConfig", t.CmdRemoteOpenConfig(), "", a.openSSHConfig},
		{"remote.openFolder", t.CmdRemoteOpenFolder(), "", a.openRemoteFolder},
		{"remote.reconnect", t.CmdRemoteReconnect(), "", a.reconnect},
		{"remote.disconnect", t.CmdRemoteDisconnect(), "", a.disconnect},
		{"remote.forwardPort", t.CmdRemoteForwardPort(), "", a.askForwardPort},
		{"wsl.openFolder", t.CmdWSLOpenFolder(), "", a.openWSLPicker},
		{"file.save", t.CmdFileSave(), "CmdOrCtrl+S", a.saveActive},
		{"file.saveAs", t.CmdFileSaveAs(), "Shift+CmdOrCtrl+S", a.saveActiveAs},
		{"file.saveAll", t.CmdFileSaveAll(), "Alt+CmdOrCtrl+S", a.saveAll},
		{"file.autoSave", t.CmdFileAutoSave(), "", a.toggleAutoSave},
		{"file.settings", t.CmdFileSettings(), "CmdOrCtrl+,", func() { a.host.showSettings("", "") }},
		{"file.settingsJSON", t.CmdFileSettingsJSON(), "", a.openSettings},
		{"file.keybindings", t.CmdFileKeybindings(), "", func() { a.host.showSettings(prefsShortcuts, "") }},
		{"file.keybindingsJSON", t.CmdFileKeybindingsJSON(), "", a.openKeybindings},
		{"file.folderSettings", t.CmdFileFolderSettings(), "", a.openFolderSettings},
		{"file.copyPath", t.CmdFileCopyPath(), "", func() { a.copyActivePath(false) }},
		{"file.copyRelativePath", t.CmdFileCopyRelativePath(), "", func() { a.copyActivePath(true) }},
		{"file.reveal", revealActiveLabel(t), "", a.revealActive},
		{"file.rename", t.CmdFileRename(), "", a.renameActive},
		{"file.close", t.CmdFileClose(), "CmdOrCtrl+W", a.closeActive},
		{"file.reopenClosed", t.CmdFileReopenClosed(), "Shift+CmdOrCtrl+T", a.reopenClosed},
		{"file.closeFolder", t.CmdFileCloseFolder(), "", a.closeFolder},
		{"file.closeProject", t.CmdFileCloseProject(), "", func() { a.host.closeProject() }},
		{"account.signIn", t.CmdAccountSignIn(), "", func() { a.host.signIn() }},
		{"account.show", t.CmdAccountShow(), "", a.showSync},
		{"account.syncNow", t.CmdAccountSyncNow(), "", a.syncNow},
		{"account.compareKeybindings", t.CmdAccountCompareKeybindings(), "", a.compareKeybindings},
		{"account.manage", t.CmdAccountManage(), "", func() { a.services.OpenURL(monURL()) }},
		{"help.reportProblem", t.CmdHelpReportProblem(), "", a.reportFromMenu},
		{"account.signOut", t.CmdAccountSignOut(), "", a.confirmSignOut},
		{"file.installCLI", t.CmdFileInstallCLI(), "", func() { a.host.installCLI() }},
		{"file.uninstallCLI", t.CmdFileUninstallCLI(), "", func() { a.host.uninstallCLI() }},
		{"file.addContextMenu", t.CmdFileAddContextMenu(), "", func() { a.host.addContextMenu() }},
		{"file.removeContextMenu", t.CmdFileRemoveContextMenu(), "", func() { a.host.removeContextMenu() }},
		// Undo and Redo take no keys in the menu: ⌘Z goes to the focused
		// control, the editor's history or a text field's.
		{"edit.undo", t.CmdEditUndo(), "", a.editAll(func(d *editor) { d.undo() })},
		{"edit.redo", t.CmdEditRedo(), "", a.editAll(func(d *editor) { d.redo() })},
		{"edit.cut", t.CmdEditCut(), "CmdOrCtrl+X", a.editAll(a.cut)},
		{"edit.selectAll", t.CmdEditSelectAll(), "CmdOrCtrl+A", a.editActive(selectAll)},
		{"edit.find", t.CmdEditFind(), "CmdOrCtrl+F", func() { a.openFind(false) }},
		{"edit.replace", t.CmdEditReplace(), "Alt+CmdOrCtrl+F", func() { a.openFind(true) }},
		{"edit.findNext", t.CmdEditFindNext(), "CmdOrCtrl+G", func() { a.findStep(1) }},
		{"edit.findPrev", t.CmdEditFindPrev(), "Shift+CmdOrCtrl+G", func() { a.findStep(-1) }},
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
		// Several selections (multicursor.go): their keys, as VS Code's.
		{"sel.addCursorAbove", t.CmdSelAddCursorAbove(), "Alt+CmdOrCtrl+Up", a.multiCmd(func(d *editor) { a.addCursor(d, -1) })},
		{"sel.addCursorBelow", t.CmdSelAddCursorBelow(), "Alt+CmdOrCtrl+Down", a.multiCmd(func(d *editor) { a.addCursor(d, 1) })},
		// ⌃⌘G on macOS, where ⌘D splits as in iTerm2; Ctrl+D elsewhere.
		{"sel.addNextOccurrence", t.CmdSelAddNextOccurrence(), "Ctrl+CmdOrCtrl+G", a.multiCmd(a.addNextOccurrence)},
		{"sel.selectAllOccurrences", t.CmdSelSelectAllOccurrences(), "Shift+CmdOrCtrl+L", a.multiCmd(a.selectAllOccurrences)},
		{"sel.columnUp", t.CmdSelColumnUp(), "Shift+Alt+CmdOrCtrl+Up", a.multiCmd(func(d *editor) { a.columnSelect(d, -1, 0) })},
		{"sel.columnDown", t.CmdSelColumnDown(), "Shift+Alt+CmdOrCtrl+Down", a.multiCmd(func(d *editor) { a.columnSelect(d, 1, 0) })},
		{"sel.columnLeft", t.CmdSelColumnLeft(), "Shift+Alt+CmdOrCtrl+Left", a.multiCmd(func(d *editor) { a.columnSelect(d, 0, -1) })},
		{"sel.columnRight", t.CmdSelColumnRight(), "Shift+Alt+CmdOrCtrl+Right", a.multiCmd(func(d *editor) { a.columnSelect(d, 0, 1) })},
		{"sel.removeCursors", t.CmdSelRemoveCursors(), "", a.editActive(func(d *editor) { d.selectTyped(d.sel[0], d.sel[1]) })},
		{"sel.indentTabs", t.CmdSelIndentTabs(), "", a.editActive(func(d *editor) { d.indent.tabs = true })},
		{"sel.indent2", t.CmdSelIndentSpaces(2), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 2} })},
		{"sel.indent4", t.CmdSelIndentSpaces(4), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 4} })},
		{"sel.indent8", t.CmdSelIndentSpaces(8), "", a.editActive(func(d *editor) { d.indent = indentStyle{size: 8} })},
		{"sel.camelCase", t.CmdSelCamelCase(), "", a.editActive(camelCase)},
		{"sel.joinLines", t.CmdSelJoinLines(), "", a.editActive(joinSelLines)},
		{"sel.sortAsc", t.CmdSelSortAsc(), "", a.editActive(func(d *editor) { sortLines(d, false) })},
		{"sel.sortDesc", t.CmdSelSortDesc(), "", a.editActive(func(d *editor) { sortLines(d, true) })},
		{"sel.format", t.CmdSelFormat(), "Shift+Alt+F", a.formatActive},
		{"editor.changeLanguage", t.CmdEditorChangeLanguage(), "", a.openLanguagePicker},
		{"sel.trimWhitespace", t.CmdSelTrimWhitespace(), "", a.editActive(a.trimActive)},
		{"sel.insertSnippet", t.CmdSelInsertSnippet(), "", a.insertSnippet},
		// ⌃Space, which the editor takes itself, so that a terminal keeps it.
		{"sel.complete", t.CmdSelComplete(), "", a.completeOne},
		{"markdown.toggleTask", t.CmdMarkdownToggleTask(), "", a.editActive(toggleMdTask)},
		{"markdown.moveItemUp", t.CmdMarkdownMoveItemUp(), "", a.editActive(func(d *editor) { mdMoveItem(d, false) })},
		{"markdown.moveItemDown", t.CmdMarkdownMoveItemDown(), "", a.editActive(func(d *editor) { mdMoveItem(d, true) })},
		{"markdown.toggleList", t.CmdMarkdownToggleList(), "", a.editActive(toggleMdList)},
		{"markdown.toggleQuote", t.CmdMarkdownToggleQuote(), "", a.editActive(toggleMdQuote)},
		{"markdown.heading1", t.CmdMarkdownHeading("1"), "Alt+CmdOrCtrl+1", a.editActive(func(d *editor) { toggleMdHeading(d, 1) })},
		{"markdown.heading2", t.CmdMarkdownHeading("2"), "Alt+CmdOrCtrl+2", a.editActive(func(d *editor) { toggleMdHeading(d, 2) })},
		{"markdown.heading3", t.CmdMarkdownHeading("3"), "Alt+CmdOrCtrl+3", a.editActive(func(d *editor) { toggleMdHeading(d, 3) })},
		{"markdown.heading4", t.CmdMarkdownHeading("4"), "Alt+CmdOrCtrl+4", a.editActive(func(d *editor) { toggleMdHeading(d, 4) })},
		{"markdown.heading5", t.CmdMarkdownHeading("5"), "Alt+CmdOrCtrl+5", a.editActive(func(d *editor) { toggleMdHeading(d, 5) })},
		{"markdown.heading6", t.CmdMarkdownHeading("6"), "Alt+CmdOrCtrl+6", a.editActive(func(d *editor) { toggleMdHeading(d, 6) })},
		{"markdown.clearFormatting", t.CmdMarkdownClearFormatting(), "", a.editActive(clearMdFormatting)},
		{"markdown.toggleEmbed", t.CmdMarkdownToggleEmbed(), "", a.editActive(toggleMdEmbed)},
		// ⌥⌘[ and ⌥⌘] fold and open the lines of an editor, and the files
		// of a diff where one is shown.
		{"edit.fold", t.CmdEditFold(), "Alt+CmdOrCtrl+[", func() { a.foldInContext(true) }},
		{"edit.unfold", t.CmdEditUnfold(), "Alt+CmdOrCtrl+]", func() { a.foldInContext(false) }},
		{"edit.toggleFold", t.CmdEditToggleFold(), "", a.editActive(a.toggleFoldCmd)},
		{"edit.foldAll", t.CmdEditFoldAll(), "", a.editActive(a.foldAllCmd)},
		{"edit.unfoldAll", t.CmdEditUnfoldAll(), "", a.editActive(a.unfoldAllCmd)},
		{"go.bracket", t.CmdGoBracket(), "Shift+CmdOrCtrl+\\", a.editActive(goToBracket)},
		{"go.definition", t.CmdGoDefinition(), "F12", a.goToDefinition},
		{"go.typeDefinition", t.CmdGoTypeDefinition(), "", a.jumpCommand(lsp.TypeDefinition)},
		{"go.sourceDefinition", t.CmdGoSourceDefinition(), "", a.jumpCommand(sourceDefinition)},
		{"go.implementation", t.CmdGoImplementation(), "CmdOrCtrl+F12", a.jumpCommand(lsp.Implementation)},
		{"go.references", t.CmdGoReferences(), "Shift+F12", a.jumpCommand(lsp.References)},
		{"go.hover", t.CmdGoHover(), "", a.showHover},
		// ⌃- and ⌃⇧- on macOS, as VS Code has them, where ⌥⌘← goes to the
		// column on the left and ⌥← by words.
		{"go.back", t.CmdGoBack(), "Ctrl+-", func() { a.navGo(-1) }},
		{"go.forward", t.CmdGoForward(), "Ctrl+Shift+-", func() { a.navGo(1) }},
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
		{"view.compactFolders", t.CmdViewCompactFolders(), "", a.toggleCompactFolders},
		{"view.search", t.CmdViewSearch(), "", func() { a.showSearch(false) }},
		{"view.scm", t.CmdViewScm(), "Ctrl+Shift+G", a.showSourceControl},
		{"view.sessions", t.CmdViewSessions(), "", a.showSessions},
		{"view.archivedSessions", t.CmdViewArchivedSessions(), "", a.toggleArchivedSessions},
		{"view.github", t.CmdViewGitHub(), "", a.showGitHub},
		{"git.commit", t.CmdGitCommit(), "", func() { a.commit(false) }},
		{"git.commitPush", t.CmdGitCommitPush(), "", func() { a.commit(true) }},
		{"git.amend", t.CmdGitAmend(), "", func() { a.showSourceControl(); a.startAmend() }},
		{"git.writeMessage", t.CmdGitWriteMessage(), "", func() { a.showSourceControl(); a.writeMessage() }},
		{"git.openChanges", t.CmdGitOpenChanges(), "", a.openChanges},
		{"git.fileHistory", t.CmdGitFileHistory(), "", a.openFileHistory},
		{"git.folderHistory", t.CmdGitFolderHistory(), "", a.openFolderHistory},
		{"git.openAllChanges", t.CmdGitOpenAllChanges(), "", func() { a.openChangesTab(true) }},
		{"git.compareFiles", t.CmdGitCompareFiles(), "", a.compareLocalFiles},
		{"git.solveConflicts", t.CmdGitSolveConflicts(), "", a.solveActiveConflicts},
		{"git.conflictFiles", t.CmdGitConflictFiles(), "", a.openConflicts},
		{"git.nextChange", t.CmdGitNextChange(), "Alt+F5", func() { a.stepChange(1) }},
		{"git.prevChange", t.CmdGitPrevChange(), "Shift+Alt+F5", func() { a.stepChange(-1) }},
		{"git.nextFile", t.CmdGitNextFile(), "", func() { a.stepDiffFile(1) }},
		{"git.prevFile", t.CmdGitPrevFile(), "", func() { a.stepDiffFile(-1) }},
		{"git.collapseAllFiles", t.CmdGitCollapseAllFiles(), "", func() { a.foldDiffFiles(true) }},
		{"git.expandAllFiles", t.CmdGitExpandAllFiles(), "", func() { a.foldDiffFiles(false) }},
		{"git.toggleCommitFiles", t.CmdGitToggleCommitFiles(), "", a.toggleCommitFiles},
		{"git.onlyPaths", t.CmdGitOnlySelectedPaths(), "", a.toggleOnlyPaths},
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
		{"git.switchWorktree", t.CmdGitSwitchWorktree(), "", a.openWorktreePicker},
		{"git.createBranch", t.CmdGitCreateBranch(), "", a.askNewBranch},
		{"git.stash", t.CmdGitStash(), "", func() { a.askStash(false) }},
		{"git.stashUntracked", t.CmdGitStashUntracked(), "", func() { a.askStash(true) }},
		{"git.stashes", t.CmdGitStashes(), "", a.openStashPicker},
		{"git.openPullRequest", t.CmdGitOpenPullRequest(), "", a.openPRPicker},
		{"git.showPullRequestChecks", t.CmdGitShowPullRequestChecks(), "", a.openChecks},
		{"git.newIssue", t.CmdGitNewIssue(), "", func() { a.newIssue(a.repository()) }},
		{"git.blame", t.CmdGitBlame(), "", a.toggleBlame},
		{"git.openFileOnRemote", t.CmdGitOpenFileOnRemote(), "", func() { a.activeFileOnRemote(false) }},
		{"git.copyPermalink", t.CmdGitCopyPermalink(), "", func() { a.activeFileOnRemote(true) }},
		{"git.fetch", t.CmdGitFetch(), "", a.fetch},
		{"git.pull", t.CmdGitPull(), "", a.pull},
		{"git.push", t.CmdGitPush(), "", a.push},
		{"git.refresh", t.CmdGitRefresh(), "", a.gitChanged},
		{"review.comment", t.CmdReviewComment(), "Shift+CmdOrCtrl+M", a.addReviewComment},
		{"review.next", t.CmdReviewNext(), "", func() { a.stepNote(1) }},
		{"review.previous", t.CmdReviewPrevious(), "", func() { a.stepNote(-1) }},
		{"review.copy", a.reviewLabel(t.CmdReviewCopy(), t.CmdReviewCopyItems()), "", a.copyReview},
		{"review.send", a.reviewLabel(t.CmdReviewSend(), t.CmdReviewSendItems()), "", a.sendReview},
		{"review.clear", a.reviewLabel(t.CmdReviewClear(), t.CmdReviewClearItems()), "", a.clearReview},
		{"view.terminal", t.CmdViewTerminal(), "Ctrl+`", a.togglePanel},
		{"view.newTerminal", t.CmdViewNewTerminal(), "Ctrl+Shift+`", func() { a.openTerminal(a.workDir()) }},
		{"view.terminalTab", t.CmdViewTerminalTab(), "", func() { a.newTerminalTab(a.fileColumn(), a.workDir(), true) }},
		{"view.terminalMove", t.CmdViewTerminalMove(), "", a.moveTerminal},
		{"view.terminalRename", t.CmdViewTerminalRename(), "", a.renameTerminal},
		{"view.installHC", t.CmdViewInstallHC(), "", a.installHC},
		{"view.attachHC", t.CmdViewAttachHC(), "", a.pickHCAttach},
		{"view.terminalLocation", t.CmdViewTerminalLocation(), "", a.toggleTerminalLocation},
		{"view.terminalLockRight", t.CmdViewTerminalLockRight(), "", a.moveTerminalToLockedRight},
		{"view.panelPosition.bottom", t.CmdViewPanelBottom(), "", func() { a.setPanelPosition("") }},
		{"view.panelPosition.left", t.CmdViewPanelLeft(), "", func() { a.setPanelPosition("left") }},
		{"view.panelPosition.right", t.CmdViewPanelRight(), "", func() { a.setPanelPosition("right") }},
		{"view.lockColumn", t.CmdViewLockColumn(), "", func() { a.toggleColumnLock(a.column()) }},
		// Split Down has Ctrl+Shift+D off macOS, none there: ⌘D and ⇧⌘D open
		// an empty column instead, as iTerm2's split an empty pane.
		{"view.splitRight", t.CmdViewSplitRight(), "CmdOrCtrl+\\", a.splitRight},
		{"view.splitDown", t.CmdViewSplitDown(), "", a.splitDown},
		{"view.newColumnRight", t.CmdViewNewColumnRight(), "CmdOrCtrl+D", func() { a.newEmptyColumn(false) }},
		{"view.newColumnDown", t.CmdViewNewColumnDown(), "Shift+CmdOrCtrl+D", func() { a.newEmptyColumn(true) }},
		{"view.nextColumn", t.CmdViewNextColumn(), "Alt+CmdOrCtrl+Right", func() { a.focusColumnBy(1) }},
		{"view.prevColumn", t.CmdViewPrevColumn(), "Alt+CmdOrCtrl+Left", func() { a.focusColumnBy(-1) }},
		{"view.moveTabNext", t.CmdViewMoveTabNext(), "", func() { a.moveTabBy(1) }},
		{"view.moveTabPrev", t.CmdViewMoveTabPrev(), "", func() { a.moveTabBy(-1) }},
		{"view.togglePinTab", t.CmdViewTogglePinTab(), "", a.togglePinTab},
		{"view.wordWrap", t.CmdViewWordWrap(), "", a.toggleWordWrap},
		{"view.vim", t.CmdViewVim(), "", a.toggleVim},
		{"view.projectTabs", t.CmdViewProjectTabs(), "", a.toggleProjectTabs},
		{"view.wordWrapTab", t.CmdViewWordWrapTab(), "Alt+Z", a.toggleTabWrap},
		{"view.togglePreview", t.CmdViewTogglePreview(), "Shift+CmdOrCtrl+V", a.togglePreview},
		{"view.previewToSide", t.CmdViewPreviewToSide(), "", a.previewToSide},
		{"view.toggleLivePreview", t.CmdViewToggleLivePreview(), "Alt+CmdOrCtrl+V", a.toggleLivePreview},
		{"view.markdownOpen.source", t.CmdViewMarkdownOpenSource(), "", func() { a.setMarkdownOpen("") }},
		{"view.markdownOpen.live", t.CmdViewMarkdownOpenLive(), "", func() { a.setMarkdownOpen("live") }},
		{"view.markdownOpen.reading", t.CmdViewMarkdownOpenReading(), "", func() { a.setMarkdownOpen("reading") }},
		{"view.jsonlOpen.table", t.CmdViewJsonlOpenTable(), "", func() { a.setJSONLOpen("") }},
		{"view.jsonlOpen.source", t.CmdViewJsonlOpenSource(), "", func() { a.setJSONLOpen("source") }},
		// The whole window, independent of the focused pane.
		{"view.zoomIn", t.CmdViewZoomIn(), "CmdOrCtrl+=", func() { a.zoom(1) }},
		{"view.zoomOut", t.CmdViewZoomOut(), "CmdOrCtrl+-", func() { a.zoom(-1) }},
		{"view.zoomReset", t.CmdViewZoomReset(), "CmdOrCtrl+0", func() { a.zoom(0) }},
		{"editor.fontZoomIn", t.CmdEditorFontZoomIn(), "", func() { a.fontZoom(1) }},
		{"editor.fontZoomOut", t.CmdEditorFontZoomOut(), "", func() { a.fontZoom(-1) }},
		{"editor.fontZoomReset", t.CmdEditorFontZoomReset(), "", func() { a.fontZoom(0) }},
		{"image.zoomIn", t.CmdImageZoomIn(), "", func() { a.imageZoom(1) }},
		{"image.zoomOut", t.CmdImageZoomOut(), "", func() { a.imageZoom(-1) }},
		{"image.zoomReset", t.CmdImageZoomReset(), "", func() { a.imageZoom(0) }},
		{"mermaid.copyImage", t.CmdMermaidCopyImage(), "", a.copyActiveDiagram},
		{"mermaid.exportPNG", t.CmdMermaidExportPNG(), "", func() { a.exportActiveDiagram("png") }},
		{"mermaid.exportSVG", t.CmdMermaidExportSVG(), "", func() { a.exportActiveDiagram("svg") }},
		{"view.appearance.system", t.CmdViewAppearanceSystem(), "", func() { a.setAppearance("") }},
		{"view.appearance.light", t.CmdViewAppearanceLight(), "", func() { a.setAppearance("light") }},
		{"view.appearance.dark", t.CmdViewAppearanceDark(), "", func() { a.setAppearance("dark") }},
		{"view.selectTheme", t.CmdViewSelectTheme(), "", a.openThemePicker},
		{"view.theme.mayoi", "Mayoi", "", func() { a.setTheme("") }},
		{"view.theme.matrix", t.CmdViewThemeMatrix(), "", func() { a.setTheme(themeMatrix) }},
		{"view.theme.claude", "Claude", "", func() { a.setTheme(themeClaude) }},
		{"view.theme.qoder", "Qoder", "", func() { a.setTheme(themeQoder) }},
		{"view.theme.grokbot", "Grok Bot", "", func() { a.setTheme(themeGrokBot) }},
		{"view.theme.cursor", "Cursor", "", func() { a.setTheme(themeCursor) }},
		{"view.theme.purplegold", t.CmdViewThemePurpleGold(), "", func() { a.setTheme(themePurpleGold) }},
		{"view.theme.pumpkin", t.CmdViewThemePumpkin(), "", func() { a.setTheme(themePumpkin) }},
		{"view.theme.witchsbrew", t.CmdViewThemeWitchsBrew(), "", func() { a.setTheme(themeWitchsBrew) }},
		{"view.theme.bloodmoon", t.CmdViewThemeBloodMoon(), "", func() { a.setTheme(themeBloodMoon) }},
		{"view.theme.gtavi", "GTA VI", "", func() { a.setTheme(themeGTAVI) }},
		{"view.theme.ackers", "Ackers", "", func() { a.setTheme(themeAckers) }},
		{localeCommand + "system", t.CmdViewLocaleSystem(), "", func() { a.setLocale("") }},
	}
	// The languages, each named in itself.
	for _, tag := range mygo.App.Languages() {
		cmds = append(cmds, command{localeCommand + tag, i18n.Autonym(tag), "", func() { a.setLocale(tag) }})
	}
	// Quitting as Cmd+Q does, which asks first (quitApp).
	cmds = append(cmds, command{"file.quit", t.CmdFileQuit(), "CmdOrCtrl+Q", func() { a.host.quitApp() }})
	cmds = append(cmds, command{"plugins.problems", t.CmdPluginsProblems(), "", a.showProblems})
	cmds = append(cmds, a.pluginCommands(t, cmds)...)
	for i := range cmds {
		cmds[i].accel = platformAccel(cmds[i].id, cmds[i].accel)
	}
	if !remoteSupported {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return strings.HasPrefix(c.id, "remote.") })
	}
	if !wslAvailable() {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return strings.HasPrefix(c.id, "wsl.") })
	}
	if !hcInstallable() {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return c.id == "view.installHC" })
	}
	// Explorer's right-click menu is of Windows.
	if runtime.GOOS != "windows" {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return strings.HasSuffix(c.id, "ContextMenu") })
	}
	if hcPath() == "" {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return c.id == "view.attachHC" })
	}
	// Only macOS's app menu has Quit; elsewhere, closing the last window
	// quits.
	if runtime.GOOS != "darwin" {
		cmds = slices.DeleteFunc(cmds, func(c command) bool { return c.id == "file.quit" })
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
	dir = remote.Clean(dir)
	switch {
	case a.host.showFolder(dir):
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
	a.closeTab(t, nil, nil)
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

// renameTerminal asks for a new name for the terminal that has the
// keyboard, or had it last.
func (a *app) renameTerminal() {
	switch t := a.activeTab(); {
	case a.focus == paneTerminal && a.panelTerm != nil:
		a.askRenameTerminal(a.panelTerm)
	case t != nil && t.term != nil:
		a.askRenameTerminal(t.term)
	case a.panelTerm != nil:
		a.askRenameTerminal(a.panelTerm)
	}
}

// askRenameTerminal asks for a new name for s, which it keeps over the
// titles its programs give it; an empty one has it show those again.
func (a *app) askRenameTerminal(s *session) {
	a.prompt = &prompt{title: locales.TermRenameTitle(), hint: s.title, value: s.label(), optional: true, done: func(name string) string {
		s.name = strings.TrimSpace(name)
		return ""
	}}
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

// zoom scales all window content, or restores its default scale for 0.
func (a *app) zoom(by float32) {
	s := a.settings
	if by == 0 {
		s.ZoomLevel = 0
	} else {
		s.ZoomLevel = clamp(s.ZoomLevel+by, settings.MinZoomLevel, settings.MaxZoomLevel)
	}
	s.Changed()
	a.host.redrawWindows()
}

// imageZoom changes only the picture shown, or fits it for 0: the
// diagram over the window while one is.
func (a *app) imageZoom(by int) {
	if a.diagram != nil {
		a.diagram.view.zoomBy(by)
		return
	}
	if v := a.activeViewer(); v != nil && v.markdown == nil {
		v.zoomBy(by)
	}
}

// fontZoom changes the base text size without scaling the window's UI.
func (a *app) fontZoom(by float32) {
	s := a.settings
	if by == 0 {
		s.FontSize = 13
	} else {
		s.FontSize = clamp(s.FontSize+by, 9, 28)
	}
	s.Changed()
	a.host.redrawWindows()
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
// that install the mayoi command, and on Windows those of Explorer's
// right-click menu; builds that can update themselves get
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
	// The appearances, which the themes that are always dark have no
	// choice of, and the themes.
	appearance := func(id, ap string) *mygo.MenuItem {
		it := radio(id, a.settings.Appearance == ap)
		it.Disabled = alwaysDark(a.settings.Theme)
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
		reconnect.Disabled, disconnect.Disabled = a.conn == nil, a.conn == nil
		forward.Disabled = a.conn == nil || a.conn.Kind() == remote.KindWSL
		file = append(file, &mygo.MenuItem{Label: locales.MenuRemoteSSH(), Submenu: []*mygo.MenuItem{
			item("remote.connect"), item("remote.openFolder"), item("remote.openConfig"),
			mygo.Separator(), reconnect, disconnect, mygo.Separator(), forward,
		}})
	}
	if wslAvailable() {
		file = append(file, item("wsl.openFolder"))
	}
	file = append(file, mygo.Separator(),
		item("file.save"), item("file.saveAs"), item("file.saveAll"), checkbox("file.autoSave", a.prefs().AutoSave), mygo.Separator(),
	)
	if runtime.GOOS != "darwin" {
		file = append(file, item("file.settings"))
	}
	// The sessions listed are of this machine, as a folder's own settings.
	sessions := item("view.sessions")
	sessions.Disabled = !a.hasSessions()
	archived := checkbox("view.archivedSessions", a.sessions.archived && a.window.SidebarView == "sessions" && !a.window.SidebarClosed)
	archived.Disabled = !a.hasSessions()
	github := item("view.github")
	github.Disabled = !a.hasGitHub()
	// A folder's own settings are of a folder on this machine.
	folderSettings := item("file.folderSettings")
	folderSettings.Disabled = a.root == "" || remote.Is(a.root)
	file = append(file, item("file.settingsJSON"), item("file.keybindings"), item("file.keybindingsJSON"), folderSettings, mygo.Separator(),
		item("file.close"), item("file.reopenClosed"), item("file.closeFolder"), item("file.closeProject"),
	)
	if cli {
		file = append(file, mygo.Separator(), item("file.installCLI"), item("file.uninstallCLI"))
		if runtime.GOOS == "windows" {
			file = append(file, item("file.addContextMenu"), item("file.removeContextMenu"))
		}
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
	report := item("help.reportProblem")
	help = append(help, mygo.Separator(), report)
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	if runtime.GOOS == "darwin" {
		// Help keeps only the report: the rest is in the app menu.
		help = []*mygo.MenuItem{report}
		appMenu = &mygo.MenuItem{Label: mygo.App.Name(), Submenu: slices.Concat(about, []*mygo.MenuItem{mygo.Separator(), item("file.settings"), mygo.Separator()}, account, []*mygo.MenuItem{
			mygo.Separator(),
			{Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(),
			// Mayoi's own, rather than the Quit role's, so that it asks
			// first while quitting otherwise, as to update or log out,
			// does not.
			item("file.quit"),
		})}
	}
	// The paths of a history shown alone, off unless a commit opened from
	// one shows; the panel of a commit's files is off while they show.
	onlyFile := checkbox("git.onlyPaths", a.activeDiffOnly())
	onlyFile.Disabled = a.activeDiff() == nil || a.activeDiff().commit == nil || len(a.activeDiff().paths) == 0
	commitFiles := checkbox("git.toggleCommitFiles", a.window.CommitFilesOpen)
	commitFiles.Disabled = a.activeDiffOnly()
	// The items of the review comments, off while there are none.
	reviewItem := func(id string) *mygo.MenuItem {
		it := item(id)
		it.Disabled = len(a.review.notes) == 0
		return it
	}
	// The items of pull requests, off until gh answered where the
	// repository is.
	prItem := func(id string) *mygo.MenuItem {
		it := item(id)
		it.Disabled = !a.repository().git.ghOK
		return it
	}
	cut, selectAll := &mygo.MenuItem{Role: mygo.RoleCut}, &mygo.MenuItem{Role: mygo.RoleSelectAll}
	copyItem := &mygo.MenuItem{Role: mygo.RoleCopy}
	if a.keyFocus == placeEditor {
		cut, selectAll = item("edit.cut"), item("edit.selectAll")
		// With several selections, Copy takes them all, which the text
		// area's own Copy would not.
		if a.copyAll {
			copyItem = &mygo.MenuItem{Label: locales.CmdEditCopy(), Accelerator: "CmdOrCtrl+C", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.editAll(a.copySpans)()
			}}
		}
	}
	installHC := item("view.installHC")
	// Attach to Terminal in herdr-core… is not a command while hc is
	// missing, which would leave an item with no label.
	attachHC := item("view.attachHC")
	_, hasHC := byID["view.attachHC"]
	attachHC.Hidden = !hasHC
	menus := []*mygo.MenuItem{
		appMenu,
		{Label: locales.MenuFile(), Submenu: file},
		{Label: locales.MenuEdit(), Submenu: []*mygo.MenuItem{
			item("edit.undo"), item("edit.redo"), mygo.Separator(),
			cut, copyItem, {Role: mygo.RolePaste}, selectAll, mygo.Separator(),
			item("edit.find"), item("edit.replace"), item("edit.findNext"), item("edit.findPrev"), mygo.Separator(),
			item("edit.findInFiles"), item("edit.replaceInFiles"), mygo.Separator(),
			item("edit.fold"), item("edit.unfold"), item("edit.toggleFold"), item("edit.foldAll"), item("edit.unfoldAll"), mygo.Separator(),
			{Label: locales.MenuMarkdown(), Submenu: []*mygo.MenuItem{
				item("markdown.toggleTask"), item("markdown.toggleList"), item("markdown.toggleQuote"), mygo.Separator(),
				item("markdown.moveItemUp"), item("markdown.moveItemDown"), mygo.Separator(),
				item("markdown.heading1"), item("markdown.heading2"), item("markdown.heading3"),
				item("markdown.heading4"), item("markdown.heading5"), item("markdown.heading6"), mygo.Separator(),
				item("markdown.clearFormatting"), item("markdown.toggleEmbed"),
			}},
		}},
		{Label: locales.MenuSelection(), Submenu: []*mygo.MenuItem{
			item("sel.line"), mygo.Separator(),
			item("sel.addCursorAbove"), item("sel.addCursorBelow"), item("sel.addNextOccurrence"), item("sel.selectAllOccurrences"), mygo.Separator(),
			item("sel.comment"), item("sel.indent"), item("sel.outdent"), mygo.Separator(),
			item("sel.moveUp"), item("sel.moveDown"), item("sel.copyUp"), item("sel.copyDown"), mygo.Separator(),
			item("sel.lineBelow"), item("sel.lineAbove"), item("sel.deleteLine"), item("sel.joinLines"), mygo.Separator(),
			item("sel.sortAsc"), item("sel.sortDesc"), item("sel.camelCase"), mygo.Separator(),
			item("sel.format"), item("sel.insertSnippet"), item("sel.complete"),
		}},
		{Label: locales.MenuView(), Submenu: []*mygo.MenuItem{
			item("view.sidebar"), checkbox("view.sidebarRight", a.settings.SidebarRight), item("view.explorer"), item("view.search"), item("view.scm"), github, sessions, archived, mygo.Separator(),
			checkbox("view.compactFolders", a.settings.ExplorerCompactFolders), mygo.Separator(),
			checkbox("view.wordWrap", a.prefs().WordWrap), item("view.wordWrapTab"), item("view.togglePreview"), item("view.toggleLivePreview"), item("view.previewToSide"),
			{Label: locales.MenuOpenMarkdownAs(), Submenu: []*mygo.MenuItem{
				radio("view.markdownOpen.source", a.prefs().MarkdownMode == ""),
				radio("view.markdownOpen.live", a.prefs().MarkdownMode == "live"),
				radio("view.markdownOpen.reading", a.prefs().MarkdownMode == "reading"),
			}},
			{Label: locales.MenuOpenJsonlAs(), Submenu: []*mygo.MenuItem{
				radio("view.jsonlOpen.table", a.prefs().JSONLMode == ""),
				radio("view.jsonlOpen.source", a.prefs().JSONLMode == "source"),
			}}, mygo.Separator(),
			checkbox("view.vim", a.prefs().Vim), checkbox("view.projectTabs", a.settings.ProjectTabs), mygo.Separator(),
			item("view.splitRight"), item("view.splitDown"), item("view.newColumnRight"), item("view.newColumnDown"),
			item("view.nextColumn"), item("view.prevColumn"),
			item("view.moveTabNext"), item("view.moveTabPrev"), mygo.Separator(),
			item("view.terminal"), item("view.newTerminal"), item("view.terminalTab"), item("view.terminalMove"), attachHC, installHC,
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
				theme("view.theme.qoder", themeQoder), theme("view.theme.grokbot", themeGrokBot), theme("view.theme.cursor", themeCursor),
				theme("view.theme.purplegold", themePurpleGold), theme("view.theme.pumpkin", themePumpkin),
				theme("view.theme.witchsbrew", themeWitchsBrew), theme("view.theme.bloodmoon", themeBloodMoon),
				theme("view.theme.gtavi", themeGTAVI), theme("view.theme.ackers", themeAckers),
			}},
			{Label: locales.MenuLanguage(), Submenu: languages}, mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen}, {Role: mygo.RoleToggleDevTools},
		}},
		{Label: locales.MenuGo(), Submenu: []*mygo.MenuItem{
			item("go.quickOpen"), item("go.commands"), item("go.symbol"), item("go.line"), item("go.bracket"), mygo.Separator(),
			item("go.definition"), item("go.typeDefinition"), item("go.sourceDefinition"),
			item("go.implementation"), item("go.references"), item("go.hover"), mygo.Separator(),
			item("go.back"), item("go.forward"), mygo.Separator(),
			item("go.nextTab"), item("go.prevTab"), mygo.Separator(),
			item("go.nextProject"), item("go.prevProject"),
		}},
		{Label: locales.MenuGit(), Submenu: []*mygo.MenuItem{
			item("git.commit"), item("git.commitPush"), item("git.amend"), item("git.undoCommit"), mygo.Separator(),
			item("git.openChanges"), item("git.fileHistory"), item("git.folderHistory"), item("git.openAllChanges"), item("git.compareFiles"), item("git.nextChange"), item("git.prevChange"), item("git.nextFile"), item("git.prevFile"),
			item("git.collapseAllFiles"), item("git.expandAllFiles"), commitFiles, onlyFile, mygo.Separator(),
			item("git.showChange"), item("git.revertChange"), item("git.appendChange"), item("git.stageChange"), mygo.Separator(),
			item("git.stageFile"), item("git.unstageFile"), item("git.stageAll"), mygo.Separator(),
			item("git.switchBranch"), item("git.switchWorktree"), item("git.createBranch"), item("git.fetch"), item("git.pull"), item("git.push"), mygo.Separator(),
			item("git.stash"), item("git.stashUntracked"), item("git.stashes"), mygo.Separator(),
			prItem("git.openPullRequest"), prItem("git.showPullRequestChecks"), prItem("git.newIssue"), mygo.Separator(),
			checkbox("git.blame", a.prefs().Blame), item("git.openFileOnRemote"), item("git.copyPermalink"), item("git.refresh"), mygo.Separator(),
			item("review.comment"), reviewItem("review.next"), reviewItem("review.previous"),
			reviewItem("review.copy"), reviewItem("review.send"), reviewItem("review.clear"),
		}},
		a.pluginMenu(item),
		{Role: mygo.RoleWindowMenu},
	}
	if len(help) > 0 {
		menus = append(menus, &mygo.MenuItem{Label: locales.MenuHelp(), Submenu: help})
	}
	// Install hc… only where herdr has builds of hc.
	if !hcInstallable() {
		for _, m := range menus {
			m.Submenu = slices.DeleteFunc(m.Submenu, func(it *mygo.MenuItem) bool { return it == installHC })
		}
	}
	return mygo.NewMenu(menus)
}

// menuAccel is the accelerator of a command's menu item: none when
// keybindings.json binds its keys, which the menu would take first, or
// takes them away from it. CmdOrCtrl is Command on macOS's keys, as
// Mayoi's systems have them, not as MyGo's machine does.
func (a *app) menuAccel(cmd command) string {
	st, ok := parseAccel(cmd.accel)
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
	menu := a.menu(runtime.GOOS == "windows" && mygo.App.IsPackaged(), func(id string, _ *mygo.Window) {
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
