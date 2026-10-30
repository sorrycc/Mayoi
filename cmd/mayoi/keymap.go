package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// keyPlace is where the keyboard is as the shortcuts of keybindings.json
// take a key, which their conditions ask about.
type keyPlace int

const (
	// placeNone is a place that takes no keys itself, as a button or a
	// list: the window's shortcuts take the keys of the file there.
	placeNone keyPlace = iota
	placeEditor
	placeInput
	placeTerminal
	placeExplorer
	placeDiff
	placeViewer
)

// takeKeys has el take the keys of the shortcuts of the file first while
// it has the keyboard, then fn the others, as its HandleInput.
func (a *app) takeKeys(el ui.Element, where keyPlace, fn func(ev ui.InputEvent) bool) ui.Element {
	if el.Focused() {
		a.keyFocus = where
	}
	return el.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputKeyUp && a.held != nil && *a.held == ev.Key {
			a.held = nil
			a.invalidate()
		}
		if a.keyInput(ev, where) {
			if where != placeTerminal {
				k := ev.Key
				a.held = &k
			}
			return true
		}
		return fn != nil && fn(ev)
	})
}

// textKeys has a text field take the keys of the shortcuts first.
func (a *app) textKeys(in ui.Element) ui.Element {
	if a.chording() {
		in.ReadOnly(true)
	}
	return a.takeKeys(in, placeInput, nil)
}

// chording reports whether a chord waits for its next key, or a key the
// keymap took is still down. The editor and the text fields take no text
// meanwhile: the system types the text of a key into them, on macOS,
// though the keymap took the key.
func (a *app) chording() bool { return len(a.chord) > 0 || a.held != nil }

// keyInput takes a key pressed where, when the shortcuts of the file have
// it: a key that ends a chord, or goes on with one, or any key after a
// chord started, which ends it.
func (a *app) keyInput(ev ui.InputEvent, where keyPlace) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	return a.keyStroke(keymap.Stroke{Mods: ev.Mods, Key: ev.Key}, where)
}

// keyStroke runs the binding of a stroke, after the strokes of the chord
// typed so far, with the next frame.
func (a *app) keyStroke(s keymap.Stroke, where keyPlace) bool {
	if a.keys == nil || len(a.keys.Bindings) == 0 {
		return false
	}
	seq := append(slices.Clone(a.chord), s)
	b, more := a.keys.Lookup(seq, a.whenContext(where))
	switch {
	case more:
		a.chord = seq
	case b != nil:
		a.chord = nil
		bd := *b
		a.post(func() { a.runBinding(bd.Command, bd.Args, where) })
	case len(a.chord) > 0:
		// No binding of the chord: its keys do nothing, as in VS Code.
		a.chord = nil
	default:
		return false
	}
	a.invalidate()
	return true
}

// windowKeys has the window take the keys of the file while the keyboard
// is in no place that takes them itself: the first of each binding, or
// those that go on with the chord typed.
func (a *app) windowKeys(c *ui.Context) {
	if a.keys == nil || a.keyFocus != placeNone || a.terminalFocused(c) {
		return
	}
	ctx := a.whenContext(placeNone)
	seen := map[keymap.Stroke]bool{}
	for _, b := range a.keys.Bindings {
		if b.Removal() || len(b.Keys) <= len(a.chord) || !slices.Equal(b.Keys[:len(a.chord)], a.chord) {
			continue
		}
		s := b.Keys[len(a.chord)]
		if seen[s] {
			continue
		}
		seen[s] = true
		if found, more := a.keys.Lookup(append(slices.Clone(a.chord), s), ctx); found == nil && !more {
			continue
		}
		if c.Shortcut(s.Mods, s.Key) {
			a.keyStroke(s, placeNone)
		}
	}
	if len(a.chord) > 0 && c.Shortcut(0, ui.KeyEscape) {
		a.chord = nil
	}
}

// keyBound reports whether the file binds s, which Mayoi's own shortcut
// of s gives way to.
func (a *app) keyBound(mods ui.Modifiers, key ui.Key) bool {
	return a.keys.Binds(keymap.Stroke{Mods: mods, Key: key})
}

// whenContext tells which names of the conditions of the file hold where.
func (a *app) whenContext(where keyPlace) func(string) bool {
	return func(name string) bool {
		switch name {
		case "editorTextFocus":
			return where == placeEditor
		case "editorFocus":
			return where == placeEditor || where == placeDiff || where == placeViewer
		case "inputFocus":
			return where == placeEditor || where == placeInput || where == placeTerminal
		case "textInputFocus":
			return where == placeEditor || where == placeInput
		case "filesExplorerFocus", "explorerViewletFocus", "sideBarFocus", "listFocus":
			return where == placeExplorer
		case "terminalFocus", "terminalTextSelectedInFocused":
			return where == placeTerminal
		case "terminalHasBeenCreated", "terminalProcessSupported":
			return len(a.terms) > 0 || name == "terminalProcessSupported"
		case "terminalIsOpen":
			return a.window.PanelOpen
		case "sideBarVisible", "explorerViewletVisible":
			return !a.window.SidebarClosed
		case "editorHasSelection":
			d := a.activeEditor()
			return where == placeEditor && d != nil && d.sel[0] != d.sel[1]
		case "editorReadonly":
			return false
		case "isMac":
			return hostOS == "darwin"
		case "isLinux":
			return hostOS == "linux"
		case "isWindows":
			return hostOS == "windows"
		}
		return false
	}
}

// vscodeCommands are the commands of VS Code by their names, as Mayoi
// has them.
var vscodeCommands = map[string]string{
	"workbench.action.files.newUntitledFile":              "file.new",
	"workbench.action.newWindow":                          "file.newWindow",
	"workbench.action.files.openFolder":                   "file.openFolder",
	"workbench.action.openRecent":                         "file.openRecent",
	"workbench.action.files.save":                         "file.save",
	"workbench.action.files.saveAs":                       "file.saveAs",
	"workbench.action.files.saveAll":                      "file.saveAll",
	"workbench.action.toggleAutoSave":                     "file.autoSave",
	"workbench.action.openSettings":                       "file.settings",
	"workbench.action.openSettings2":                      "file.settings",
	"workbench.action.openSettingsJson":                   "file.settingsJSON",
	"workbench.action.openGlobalSettings":                 "file.settingsJSON",
	"workbench.action.openGlobalKeybindings":              "file.keybindings",
	"workbench.action.openGlobalKeybindingsFile":          "file.keybindingsJSON",
	"workbench.action.openWorkspaceSettingsFile":          "file.folderSettings",
	"workbench.action.closeActiveEditor":                  "file.close",
	"workbench.action.reopenClosedEditor":                 "file.reopenClosed",
	"workbench.action.closeFolder":                        "file.closeFolder",
	"workbench.action.closeWindow":                        "file.closeProject",
	"workbench.action.files.copyPathOfActiveFile":         "file.copyPath",
	"copyFilePath":                                        "file.copyPath",
	"workbench.action.files.copyRelativePathOfActiveFile": "file.copyRelativePath",
	"copyRelativeFilePath":                                "file.copyRelativePath",
	"revealFileInOS":                                      "file.reveal",
	"workbench.action.files.revealActiveFileInWindows":    "file.reveal",
	"undo":                                       "edit.undo",
	"redo":                                       "edit.redo",
	"actions.find":                               "edit.find",
	"editor.action.startFindReplaceAction":       "edit.replace",
	"editor.action.nextMatchFindAction":          "edit.findNext",
	"editor.action.previousMatchFindAction":      "edit.findPrev",
	"workbench.action.findInFiles":               "edit.findInFiles",
	"workbench.action.replaceInFiles":            "edit.replaceInFiles",
	"workbench.view.search":                      "view.search",
	"editor.action.commentLine":                  "sel.comment",
	"editor.action.indentLines":                  "sel.indent",
	"editor.action.outdentLines":                 "sel.outdent",
	"editor.action.moveLinesUpAction":            "sel.moveUp",
	"editor.action.moveLinesDownAction":          "sel.moveDown",
	"editor.action.copyLinesUpAction":            "sel.copyUp",
	"editor.action.copyLinesDownAction":          "sel.copyDown",
	"editor.action.deleteLines":                  "sel.deleteLine",
	"editor.action.insertLineAfter":              "sel.lineBelow",
	"editor.action.insertLineBefore":             "sel.lineAbove",
	"expandLineSelection":                        "sel.line",
	"editor.action.indentUsingTabs":              "sel.indentTabs",
	"editor.action.transformToCamelcase":         "sel.camelCase",
	"editor.action.sortLinesAscending":           "sel.sortAsc",
	"editor.action.sortLinesDescending":          "sel.sortDesc",
	"editor.action.jumpToBracket":                "go.bracket",
	"workbench.action.quickOpen":                 "go.quickOpen",
	"workbench.action.showCommands":              "go.commands",
	"workbench.action.gotoLine":                  "go.line",
	"workbench.action.gotoSymbol":                "go.symbol",
	"workbench.action.nextEditor":                "go.nextTab",
	"workbench.action.previousEditor":            "go.prevTab",
	"workbench.action.toggleSidebarVisibility":   "view.sidebar",
	"workbench.view.explorer":                    "view.explorer",
	"workbench.view.scm":                         "view.scm",
	"workbench.action.terminal.toggleTerminal":   "view.terminal",
	"workbench.action.togglePanel":               "view.terminal",
	"workbench.action.terminal.new":              "view.newTerminal",
	"workbench.action.createTerminalEditor":      "view.terminalTab",
	"workbench.action.positionPanelBottom":       "view.panelPosition.bottom",
	"workbench.action.positionPanelLeft":         "view.panelPosition.left",
	"workbench.action.positionPanelRight":        "view.panelPosition.right",
	"workbench.action.splitEditor":               "view.splitRight",
	"workbench.action.splitEditorRight":          "view.splitRight",
	"markdown.showPreview":                       "view.togglePreview",
	"markdown.showPreviewToSide":                 "view.previewToSide",
	"workbench.action.focusNextGroup":            "view.nextColumn",
	"workbench.action.focusPreviousGroup":        "view.prevColumn",
	"workbench.action.moveEditorToNextGroup":     "view.moveTabNext",
	"workbench.action.moveEditorToPreviousGroup": "view.moveTabPrev",
	"editor.action.toggleWordWrap":               "view.wordWrapTab",
	"workbench.action.zoomIn":                    "view.zoomIn",
	"workbench.action.zoomOut":                   "view.zoomOut",
	"workbench.action.zoomReset":                 "view.zoomReset",
	"editor.action.fontZoomIn":                   "editor.fontZoomIn",
	"editor.action.fontZoomOut":                  "editor.fontZoomOut",
	"editor.action.fontZoomReset":                "editor.fontZoomReset",
	"workbench.action.selectTheme":               "view.selectTheme",
	"git.commit":                                 "git.commit",
	"git.commitStaged":                           "git.commit",
	"git.stageAll":                               "git.stageAll",
	"git.stage":                                  "git.stageFile",
	"git.unstage":                                "git.unstageFile",
	"git.openChange":                             "git.openChanges",
	"git.undoCommit":                             "git.undoCommit",
	"git.checkout":                               "git.switchBranch",
	"git.branch":                                 "git.createBranch",
	"git.stash":                                  "git.stash",
	"git.stashIncludeUntracked":                  "git.stashUntracked",
	"git.stashApply":                             "git.stashes",
	"git.stashPop":                               "git.stashes",
	"git.stashDrop":                              "git.stashes",
	"git.fetch":                                  "git.fetch",
	"git.pull":                                   "git.pull",
	"git.push":                                   "git.push",
	"git.refresh":                                "git.refresh",
	"workbench.action.editor.nextChange":         "git.nextChange",
	"workbench.action.editor.previousChange":     "git.prevChange",
	"gitlens.toggleFileBlame":                    "git.blame",
	"gitlens.toggleLineBlame":                    "git.blame",
}

// commandID is the Mayoi command a binding names, by Mayoi's name or by
// VS Code's, "" for none.
func (a *app) commandID(name string) string {
	if id, ok := vscodeCommands[name]; ok {
		return id
	}
	for _, cmd := range a.commands() {
		if cmd.id == name {
			return name
		}
	}
	return ""
}

// commandNames are the names a binding may give a Mayoi command: its
// own, and VS Code's.
func commandNames(id string) []string {
	names := []string{id}
	for name, to := range vscodeCommands {
		if to == id {
			names = append(names, name)
		}
	}
	sort.Strings(names[1:])
	return names
}

// runBinding runs the command of a binding, with its arguments.
func (a *app) runBinding(name string, args json.RawMessage, where keyPlace) {
	switch name {
	case "runCommands":
		var list struct {
			Commands []json.RawMessage `json:"commands"`
		}
		json.Unmarshal(args, &list)
		var steps []chainStep
		for _, raw := range list.Commands {
			var step chainStep
			if json.Unmarshal(raw, &step.name) != nil {
				var obj struct {
					Command string          `json:"command"`
					Args    json.RawMessage `json:"args"`
				}
				json.Unmarshal(raw, &obj)
				step.name, step.args = obj.Command, obj.Args
			}
			if step.name != "" {
				step.where = where
				steps = append(steps, step)
			}
		}
		a.chain = append(steps, a.chain...)
		a.chainFails = a.fails
		a.pumpChain()
	case "type":
		var t struct {
			Text string `json:"text"`
		}
		json.Unmarshal(args, &t)
		a.typeText(t.Text, where)
	case "workbench.action.terminal.sendSequence":
		var t struct {
			Text string `json:"text"`
		}
		json.Unmarshal(args, &t)
		if s := a.keyTerminal(where); s != nil && t.Text != "" {
			s.term.Send([]byte(t.Text))
		}
	case "vscode.open":
		var path string
		if json.Unmarshal(args, &path) != nil || path == "" {
			a.fail("%s", locales.KeymapOpenNeedsPath())
			return
		}
		path = strings.TrimPrefix(path, "file://")
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, rest)
		} else if !filepath.IsAbs(path) && a.root != "" {
			path = filepath.Join(a.root, path)
		}
		a.openFile(path, true)
	case "editor.action.indentUsingSpaces":
		a.openQuick(">Indent Using ")
	case "deleteFile", "moveFileToTrash":
		if path := a.explorerChoice(); path != "" {
			a.trash(path)
		}
	case "renameFile":
		if path := a.explorerChoice(); path != "" {
			a.askRename(path)
		}
	case "explorer.newFile", "explorer.newFolder":
		a.askNewEntry(a.targetDir(), name == "explorer.newFolder")
	default:
		id := a.commandID(name)
		if id == "" {
			a.tell(note{message: locales.KeymapNoCommand(name)})
			return
		}
		a.run(id)
	}
}

// chainStep is a command runCommands runs after those before it are done.
type chainStep struct {
	name  string
	args  json.RawMessage
	where keyPlace
}

// pumpChain runs the commands of runCommands, each once those before it
// are done: their edits made, their Git commands run and the status read
// again. A command that fails stops the others.
func (a *app) pumpChain() {
	for len(a.chain) > 0 {
		if a.fails > a.chainFails {
			a.chain = nil
			return
		}
		if g := &a.git; g.repo != nil && (g.busy != "" || g.pending > 0 || g.running || g.stale) || a.confirm != nil || a.prompt != nil {
			return
		}
		if d := a.activeEditor(); d != nil && len(d.cmds) > 0 {
			// Made as the editor is built: the next frame goes on.
			a.invalidate()
			return
		}
		step := a.chain[0]
		a.chain = a.chain[1:]
		a.runBinding(step.name, step.args, step.where)
	}
}

// typeText types text into the editor or the terminal that took the keys.
func (a *app) typeText(text string, where keyPlace) {
	if text == "" {
		return
	}
	switch where {
	case placeEditor:
		a.editActive(func(d *editor) {
			lo, hi := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
			at := lo + utf8.RuneCountInString(text)
			d.edit([2]int{at, at}, textEdit{lo, hi, text})
		})()
	case placeTerminal:
		if s := a.keyTerminal(where); s != nil {
			s.term.Send([]byte(text))
		}
	}
}

// keyTerminal is the terminal a binding is for: the one that took its
// keys, or else the one that has had the keyboard last.
func (a *app) keyTerminal(where keyPlace) *session {
	if where == placeTerminal && a.keyTerm != nil {
		return a.keyTerm
	}
	if t := a.activeTab(); t != nil && t.term != nil && a.focus != paneTerminal {
		return t.term
	}
	return a.panelTerm
}

// explorerChoice is the file or folder chosen in the explorer, for a
// binding of the explorer's.
func (a *app) explorerChoice() string {
	if a.fileRow < 0 || a.fileRow >= a.files.Rows() {
		return ""
	}
	return a.files.Item(a.fileRow)
}

// keysFailed says what of keybindings.json Mayoi could not read.
func (a *app) keysFailed(km *keymap.Map, err error) {
	switch {
	case err != nil:
		a.fail("keybindings.json: %v", err)
	case km != nil && len(km.Errors) > 0:
		a.fail("keybindings.json: %s", strings.Join(km.Errors, "; "))
	}
}

// openKeybindings opens the file of the keyboard shortcuts, made with a
// note of its format when there is none.
func (a *app) openKeybindings() {
	path := keymap.Path(a.settings.Path())
	if path == "" {
		return
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(keymap.Template), 0o644); err != nil {
			a.fail("%s", locales.FailMake(path, err.Error()))
			return
		}
	}
	a.openFile(path, true)
}

// activeFile is the file of the tab shown, "" for none.
func (a *app) activeFile() string {
	if t := a.activeTab(); t != nil {
		return t.path()
	}
	return ""
}

// copyPath copies path, relative to the workspace when rel is set.
func (a *app) copyPath(path string, rel bool) {
	if rel && a.tree != nil {
		path = a.tree.Rel(path)
	}
	a.services.WriteClipboard(hostPath(path))
}

// copyActivePath puts the path of the file shown on the clipboard,
// relative to the folder with rel.
func (a *app) copyActivePath(rel bool) {
	path := a.activeFile()
	if path == "" {
		return
	}
	a.copyPath(path, rel)
}

// revealActive shows the file of the tab shown in the file manager.
func (a *app) revealActive() {
	if path := a.activeFile(); path != "" && !remote.Is(path) {
		a.host.reveal(path)
	}
}

// chordCaps are the strokes of a chord, each as a cap of a keyboard with
// its modifiers, as "⌃." and "G".
func chordCaps(keys []keymap.Stroke) []string {
	var caps []string
	for _, s := range keys {
		caps = append(caps, capsText(accelCaps(s.Accel(), macKeys())))
	}
	return caps
}

// toCamelCase joins the words of s, apart by spaces, underscores and
// dashes, in camel case, leaving the first as it is, as VS Code does.
func toCamelCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || unicode.IsSpace(r) })
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	b.WriteString(words[0])
	for _, w := range words[1:] {
		r, n := utf8.DecodeRuneInString(w)
		b.WriteRune(unicode.ToUpper(r))
		b.WriteString(w[n:])
	}
	return b.String()
}

// camelCase writes the selection in camel case, or the word of the caret
// when nothing is selected.
func camelCase(d *editor) {
	snap := d.buf.Snapshot()
	lo, hi := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	lo, hi = min(lo, snap.Len()), min(hi, snap.Len())
	if lo == hi {
		line := snap.LineAt(lo)
		s, e := snap.LineRange(line)
		text := []rune(snap.Slice(s, e))
		word := func(r rune) bool { return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) }
		for lo > s && word(text[lo-1-s]) {
			lo--
		}
		for hi < e && word(text[hi-s]) {
			hi++
		}
	}
	text := snap.Slice(lo, hi)
	out := toCamelCase(text)
	if out == text {
		return
	}
	end := lo + utf8.RuneCountInString(out)
	d.edit([2]int{lo, end}, textEdit{lo, hi, out})
}

// sortLines sorts the lines of the selection, or of the document when
// nothing is selected, and selects them.
func sortLines(d *editor, descending bool) {
	snap := d.buf.Snapshot()
	first, last := 0, snap.LineCount()-1
	if d.sel[0] != d.sel[1] {
		first, last = lineSpan(snap, d.sel)
	}
	if last <= first {
		return
	}
	s, _ := snap.LineRange(first)
	_, e := snap.LineRange(last)
	lines := strings.Split(snap.Slice(s, e), "\n")
	sorted := slices.Clone(lines)
	slices.Sort(sorted)
	if descending {
		slices.Reverse(sorted)
	}
	if slices.Equal(sorted, lines) {
		return
	}
	d.edit([2]int{s, e}, textEdit{s, e, strings.Join(sorted, "\n")})
}
