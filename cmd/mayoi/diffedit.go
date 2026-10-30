package main

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// diffEdit is the writable side of a file of a diff while it is edited:
// the document of the file, and the diff's editor of it, whose selection
// is the caret the diff paints and whose keys the diff takes.
type diffEdit struct {
	f          *diffFile
	doc        *document
	ed         *editor
	drag       bool   // the pointer is extending the selection
	compose    string // the text an input method marks, which sits in the buffer
	composeAt  int    // where the marked text begins, in rune offsets
	composeWas string // the text the mark took the place of, which an escape gives back
}

// writable reports whether the new side of a file of a diff is the work
// tree's, which a diff edits: the changes staged or committed compare
// versions Git keeps, which are not for editing here.
func (f *diffFile) writable() bool {
	return f.loaded && f.err == "" && !f.binary && f.new.ok && !f.entry.Submodule() &&
		(f.base == diffIndex || f.base == diffHead)
}

// editRow returns the row a diff begins to edit at: the line of the new
// side the row shows, or the first one after it, -1 for none.
func (v *diffView) editRow(i int) int {
	if i < 0 || i >= len(v.rows) {
		return -1
	}
	for k := i; k < len(v.rows); k++ {
		if r := v.rows[k]; r.kind == rowLine && r.new >= 0 {
			return k
		}
	}
	return -1
}

// beginDiffEdit starts editing the working tree side of the file of row i
// of a diff, with the caret at the start of the line the row shows. A file
// no editor has is read into a document of its own, which the editor opens
// as it is. It reports whether editing began.
func (a *app) beginDiffEdit(v *diffView, i int) bool {
	i = v.editRow(i)
	if i < 0 {
		return false
	}
	f := v.files[v.rows[i].file]
	if e := v.editing; e != nil && e.f == f {
		a.diffCaretToLine(e, v.rows[i].new)
		return true
	}
	if !f.writable() {
		a.fail("%s", locales.DiffEditOnlyLocal(filepath.Base(f.path)))
		return false
	}
	if f.running || f.gitStale {
		a.fail("%s", locales.DiffCatchingUpFiles())
		return false
	}
	doc, ok := a.diffEditDoc(f)
	if !ok {
		return false
	}
	e := a.diffEditOf(v, f, doc)
	a.diffCaretToLine(e, v.rows[i].new)
	v.editing = e
	v.edited = doc
	v.boundary = nil
	v.pane.Focus()
	a.invalidate()
	return true
}

// endDiffEdit leaves the editing of a diff's writable side, which goes
// back to reading it as a pager.
func (a *app) endDiffEdit(v *diffView) {
	if v.editing == nil {
		return
	}
	v.editing = nil
	a.invalidate()
}

// diffEditInput takes the input of a diff whose writable side is edited:
// the text and the edit commands the system sends, the keys of the caret,
// and the pointer on the side. It reports whether it took the event.
func (a *app) diffEditInput(v *diffView, ev ui.InputEvent) bool {
	e := v.editing
	switch ev.Kind {
	case ui.InputText:
		if e.compose != "" {
			e.commitCompose(ev.Text)
		} else {
			e.diffReplace(ev.Text)
		}
		a.invalidate()
		return true
	case ui.InputCompose:
		e.beginCompose(ev.Text)
		a.invalidate()
		return true
	case ui.InputCommand:
		return a.diffEditCommand(v, e, ev)
	case ui.InputKeyDown:
		return a.diffEditKey(v, e, ev)
	case ui.InputPointerDown:
		return a.diffEditPointerDown(v, e, ev)
	case ui.InputPointerMove:
		if !e.drag {
			return false
		}
		a.diffCaretAtPoint(v, e, ev.X, ev.Y, true)
		a.invalidate()
		return true
	case ui.InputPointerUp:
		if !e.drag {
			return false
		}
		e.drag = false
		return true
	}
	return false
}

// diffEditCommand takes an edit command of the menus on the writable side
// of a diff.
func (a *app) diffEditCommand(v *diffView, e *diffEdit, ev ui.InputEvent) bool {
	switch ev.Text {
	case "copy":
		if text := e.diffEditedText(); text != "" {
			a.services.WriteClipboard(text)
		}
	case "cut":
		if text := e.diffEditedText(); text != "" {
			a.services.WriteClipboard(text)
			e.diffReplace("")
		}
	case "paste", "pasteAndMatchStyle":
		text := strings.ReplaceAll(a.services.ReadClipboard(), "\r\n", "\n")
		if text != "" {
			e.diffReplace(text)
		}
	case "selectAll":
		e.ed.sel = [2]int{0, e.doc.buf.Snapshot().Len()}
	case "undo":
		e.endCompose()
		e.ed.undo()
	case "redo":
		e.endCompose()
		e.ed.redo()
	case "delete":
		e.diffReplace("")
	default:
		return false
	}
	a.invalidate()
	return true
}

// diffEditKey takes the keys of the writable side of a diff: those that
// move the caret and its selection, and the editor's, as typing is. The
// pager's keys stay out of it.
func (a *app) diffEditKey(v *diffView, e *diffEdit, ev ui.InputEvent) bool {
	m := ev.Mods
	snap := e.doc.buf.Snapshot()
	switch {
	case ev.Key == ui.KeyEscape && m == 0 && e.compose != "":
		// Escape cancels what the input method marks first; one more
		// leaves the editing.
		e.endCompose()
		a.invalidate()
		return true
	case ev.Key == ui.KeyEscape && m == 0 && e.compose == "":
		a.endDiffEdit(v)
		return true
	case ev.Key == ui.KeyS && m == ui.Cmd:
		a.saveDoc(e.doc, nil)
		return true
	case ev.Key == ui.KeyBackspace && (m == 0 || m == ui.Shift):
		lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
		if lo == hi && lo > 0 {
			lo-- // the rune before
		}
		e.ed.sel = [2]int{lo, hi}
		e.diffReplace("")
		a.invalidate()
		return true
	case ev.Key == ui.KeyDelete && m == 0:
		lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
		if lo == hi && hi < snap.Len() {
			hi++ // the rune after
		}
		e.ed.sel = [2]int{lo, hi}
		e.diffReplace("")
		a.invalidate()
		return true
	case ev.Key == ui.KeyEnd && (m == 0 || m == ui.Shift),
		ev.Key == ui.KeyRight && m == ui.Cmd, ev.Key == ui.KeyRight && m == ui.Cmd|ui.Shift:
		// To the end of the line.
		at := min(e.ed.sel[1], snap.Len())
		_, en := snap.LineRange(snap.LineAt(at))
		e.diffCaretTo(en, m&ui.Shift != 0)
		a.revealDiffCaret(v, e)
		a.invalidate()
		return true
	}
	if a.diffEditArrow(v, e, ev) {
		return true
	}
	return a.editorKey(e.ed, ev)
}

// diffEditArrow moves the caret of a diff's edit with the arrow keys,
// extending the selection when shift is held, and reports whether the key
// was one of its. With alt or cmd they are the editor's, as moving lines
// and going to the ends of the line are.
func (a *app) diffEditArrow(v *diffView, e *diffEdit, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
		return false
	}
	extend := ev.Mods&ui.Shift != 0
	snap := e.doc.buf.Snapshot()
	switch ev.Key {
	case ui.KeyLeft:
		at := min(e.ed.sel[1], snap.Len())
		switch {
		case !extend && e.ed.sel[0] != e.ed.sel[1]:
			at = min(e.ed.sel[0], e.ed.sel[1]) // out of the selection
		case at > 0:
			at--
		}
		e.diffCaretTo(at, extend)
	case ui.KeyRight:
		at := min(e.ed.sel[1], snap.Len())
		switch {
		case !extend && e.ed.sel[0] != e.ed.sel[1]:
			at = max(e.ed.sel[0], e.ed.sel[1]) // out of the selection
		case at < snap.Len():
			at++
		}
		e.diffCaretTo(at, extend)
	case ui.KeyUp, ui.KeyDown:
		a.diffCaretByRow(v, e, ev.Key == ui.KeyDown, extend)
	default:
		return false
	}
	a.revealDiffCaret(v, e)
	a.invalidate()
	return true
}

// diffCaretByRow moves the caret of a diff's edit to the line of the row
// above or below its own, keeping its column, among the rows of its file
// alone: the lines a gap folds away are passed over.
func (a *app) diffCaretByRow(v *diffView, e *diffEdit, down, extend bool) {
	row := v.caretRow(e)
	if row < 0 {
		// The caret is on a line no row shows: it stays in view's stead.
		line, _, _ := e.caretLine()
		a.diffCaretToLine(e, line)
		return
	}
	i := row
	for {
		if down {
			i++
		} else {
			i--
		}
		if i < 0 || i >= len(v.rows) || v.rows[i].file != v.rows[row].file {
			return // the edge of the file: the caret stays
		}
		if r := v.rows[i]; r.kind == rowLine && r.new >= 0 {
			break
		}
	}
	snap := e.doc.buf.Snapshot()
	line := v.rows[i].new
	s, en := snap.LineRange(min(line, snap.LineCount()-1))
	caret := min(e.ed.sel[1], snap.Len())
	cs, _ := snap.LineRange(snap.LineAt(caret))
	at := s + min(caret-cs, en-s)
	e.diffCaretTo(at, extend)
}

// caretRow returns the row of a diff that shows the line the caret of an
// edit is on, -1 when none does: a line in a gap folded away, or past the
// rows the diff laid out.
func (v *diffView) caretRow(e *diffEdit) int {
	line, _, _ := e.caretLine()
	file := slices.Index(v.files, e.f)
	if file < 0 || file >= len(v.heads) {
		return -1
	}
	hi := len(v.rows)
	if file+1 < len(v.heads) {
		hi = v.heads[file+1]
	}
	for i := v.heads[file]; i < hi; i++ {
		if r := v.rows[i]; r.kind == rowLine && r.new == line {
			return i
		}
	}
	return -1
}

// revealDiffCaret scrolls a diff as little as shows the caret of its edit,
// with its column, when it is out of view.
func (a *app) revealDiffCaret(v *diffView, e *diffEdit) {
	row := v.caretRow(e)
	if row < 0 {
		return
	}
	lineH := a.font.lineH
	y := editorPadY + float32(v.tops[row])*lineH
	h := float32(v.rows[row].h) * lineH
	if v.split {
		y, h = a.sideRowY(v, 1, row)
	}
	if v.split && !a.settings.DiffSyncScroll {
		// The caret is of the right side, which scrolls in its own.
		y -= a.rightShift(v)
		end := max(0, 2*editorPadY+float32(v.tops[len(v.tops)-1])*lineH-v.viewH)
		switch {
		case y < v.sideY:
			v.sideY = y
		case y+h > v.sideY+v.viewH:
			v.sideY = y + h - v.viewH
		}
		v.sideY = clamp(v.sideY, 0, end)
	} else {
		switch {
		case y < v.scroll.Y:
			v.scroll.Y = y
		case y+h > v.scroll.Y+v.viewH:
			v.scroll.Y = y + h - v.viewH
		}
	}
	if v.built.wrap {
		return
	}
	// The column shows too, as an editor's does.
	snap := e.doc.buf.Snapshot()
	caret := min(e.ed.sel[1], snap.Len())
	line := snap.LineAt(caret)
	s, _ := snap.LineRange(line)
	text := ""
	if line < len(e.f.new.lines) {
		text = e.f.new.lines[line]
	}
	l := a.diffGeometry(v, v.split, v.viewW)
	textX := l.textX
	if v.split {
		textX += l.right()
	}
	x, _ := diffCaretX(a.font, clipLine(text), caret-s, l.cols[0], false)
	x += textX
	switch {
	case x < v.scroll.X+textX:
		v.scroll.X = max(0, x-textX-4*a.font.charW)
	case x > v.scroll.X+v.viewW-textPadR:
		v.scroll.X = x - v.viewW + textPadR + 4*a.font.charW
	}
}

// diffCaretAtPoint puts the caret of a diff's edit where a point of its
// pane is, on the working tree side of its file, extending the selection
// from where it was when extend says. It reports whether the point was on
// a side that edits.
func (a *app) diffCaretAtPoint(v *diffView, e *diffEdit, x, y float32, extend bool) bool {
	l := a.diffGeometry(v, v.split, v.viewW)
	row := a.rowUnder(v, x+v.scroll.X, y+v.scroll.Y, l)
	if row < 0 || row >= len(v.rows) {
		return false
	}
	r := v.rows[row]
	if r.kind != rowLine || r.new < 0 {
		return false
	}
	if v.split && !l.onRight(x) {
		return false // the point is on the side that is read
	}
	f := v.files[r.file]
	if f != e.f {
		// Another file's working tree side: it takes the keys.
		if !f.writable() || !a.beginDiffEdit(v, row) {
			return false
		}
		e = v.editing
	}
	wrap := v.built.wrap
	lineH := a.font.lineH
	// Where the row's text starts, and the row of a wrapped line y is in.
	textX := l.textX
	if v.split {
		textX += l.right()
	}
	top := editorPadY + float32(v.tops[row])*lineH
	if v.split {
		top, _ = a.sideRowY(v, 1, row)
	}
	yInRow := y + v.scroll.Y - top
	text := ""
	if r.new < len(f.new.lines) {
		text = clipLine(f.new.lines[r.new])
	}
	col := diffCaretCol(a.font, text, x+v.scroll.X-textX, yInRow, lineH, l.cols[btoi(v.split)], wrap)
	snap := e.doc.buf.Snapshot()
	s, _ := snap.LineRange(min(r.new, snap.LineCount()-1))
	e.diffCaretTo(s+col, extend)
	a.invalidate()
	return true
}

// diffEditPointerDown puts the caret of a diff's edit where the pointer
// went down on the working tree side, and takes the moves until it comes
// up, as selecting does.
func (a *app) diffEditPointerDown(v *diffView, e *diffEdit, ev ui.InputEvent) bool {
	if ev.Button != 0 {
		return false
	}
	if !a.diffCaretAtPoint(v, e, ev.X, ev.Y, ev.Mods&ui.Shift != 0) {
		return false
	}
	e.drag = true
	a.invalidate()
	return true
}

// diffCaretCol returns how many runes of a line of a diff fit before x,
// on the row of the line that y is in, as it wraps at cols columns.
func diffCaretCol(f codeFont, text string, x, y float32, lineH float32, cols int, wrap bool) int {
	shown, at := expandTabs(text)
	breaks := diffBreaks(shown, cols, wrap)
	k := min(max(int(y/lineH), 0), len(breaks)-2)
	lo, hi := breaks[k], breaks[k+1]
	b, runes := 0, 0
	for b < len(text) && at(b) < lo {
		_, n := utf8.DecodeRuneInString(text[b:])
		b += n
		runes++
	}
	best := runes
	for b <= len(text) {
		sb := min(max(at(min(b, len(text))), lo), hi)
		if f.width(shown[lo:sb]) > x {
			break
		}
		best = runes
		if sb >= hi || b >= len(text) {
			break
		}
		_, n := utf8.DecodeRuneInString(text[b:])
		b += n
		runes++
	}
	return best
}

// diffCaretX returns where the caret at rune col of a line of a diff
// shows, and on which row of the line, as it wraps at cols columns.
func diffCaretX(f codeFont, text string, col, cols int, wrap bool) (x float32, row int) {
	shown, at := expandTabs(text)
	breaks := diffBreaks(shown, cols, wrap)
	b := 0
	for i, runes := 0, 0; i < len(text) && runes < col; {
		_, n := utf8.DecodeRuneInString(text[i:])
		i += n
		runes++
		b = i
	}
	sb := min(at(min(b, len(text))), len(shown))
	for k := 0; k+1 < len(breaks); k++ {
		if sb <= breaks[k+1] {
			return f.width(shown[breaks[k]:sb]), k
		}
	}
	return f.width(shown), len(breaks) - 2
}

// diffBreaks returns where the rows of a line shown start and end, as it
// wraps at cols columns: none when it does not.
func diffBreaks(shown string, cols int, wrap bool) []int {
	breaks := []int{0}
	if wrap {
		breaks = append(breaks, wrapAt(shown, cols)...)
	}
	breaks = append(breaks, len(shown))
	return breaks
}

// diffEditDoc returns the document a file of a diff is edited in, reading
// the work tree's file into one when no editor has it, and whether its
// text is what the diff compared.
func (a *app) diffEditDoc(f *diffFile) (*document, bool) {
	if doc := a.diffDoc(f); doc != nil {
		if doc.locked() {
			a.fail("%s", locales.DiffStillReading())
			return nil, false
		}
		if doc.buf.Version() != f.docVersion {
			// The diff compared the file before this text: it catches up
			// first, so that the edits go to what it shows.
			a.fail("%s", locales.DiffCatchingUp())
			return nil, false
		}
		return doc, true
	}
	if remote.Is(f.path) {
		a.fail("%s", locales.DiffEditRemote())
		return nil, false
	}
	a.nextID++
	doc, err := readDocument(a.nextID, f.path, a.invalidate)
	if err != nil {
		a.fail("%s", locales.DiffOpenFailEdit(filepath.Base(f.path), err.Error()))
		return nil, false
	}
	if doc.buf.Snapshot().String() != f.new.text {
		// The file is not the text the diff compared: it is read again.
		f.diskStale = true
		a.fail("%s", locales.DiffChangedSinceDrawn(filepath.Base(f.path)))
		doc.hl.Close()
		return nil, false
	}
	a.docs = append(a.docs, doc)
	if a.tree != nil {
		a.tree.Watch(f.path)
	}
	return doc, true
}

// diffEditOf returns the edit a diff keeps for a file, making it when it
// has none yet: an editor of the file's document, which the diff types in.
func (a *app) diffEditOf(v *diffView, f *diffFile, doc *document) *diffEdit {
	for _, e := range v.edits {
		if e.f != f {
			continue
		}
		if e.doc != doc {
			// The file's document was closed and made again.
			e.ed.release()
			e.doc, e.ed = doc, newEditor(doc)
		}
		return e
	}
	e := &diffEdit{f: f, doc: doc, ed: newEditor(doc)}
	v.edits = append(v.edits, e)
	return e
}

// runDiffEdits has a diff's editors take the edits their keys queued, and
// the typing their documents saw, as the frame is built. focused says
// whether the diff's pane has the keyboard.
func (a *app) runDiffEdits(v *diffView, focused bool) {
	for _, e := range v.edits {
		if e.doc.hl == nil {
			continue
		}
		if !a.prefs().Vim {
			vimReset(e.ed)
		}
		// The edit that has the keyboard types, as an editor's does.
		if v.editing == e && focused {
			e.doc.typist = e.ed
		}
		e.doc.observe()
		if len(e.ed.cmds) > 0 {
			cmds := e.ed.cmds
			e.ed.cmds = nil
			for _, cmd := range cmds {
				cmd(e.ed)
			}
			a.invalidate()
		}
	}
	// The file being edited may have gone from the diff, or its document
	// been closed.
	if e := v.editing; e != nil {
		if e.doc != a.diffDoc(e.f) || !slices.Contains(v.files, e.f) {
			v.editing = nil
		}
	}
}

// diffCaretToLine puts the caret of an edit at the start of a line of its
// document.
func (a *app) diffCaretToLine(e *diffEdit, line int) {
	snap := e.doc.buf.Snapshot()
	line = min(max(line, 0), snap.LineCount()-1)
	s, _ := snap.LineRange(line)
	e.ed.sel = [2]int{s, s}
	e.ed.caretSeen = true
}

// diffCaretLine returns the line of the document the caret of an edit is
// on, and where the selection runs, in lines.
func (e *diffEdit) caretLine() (line, from, to int) {
	snap := e.doc.buf.Snapshot()
	a, b := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
	a, b = min(a, snap.Len()), min(b, snap.Len())
	return snap.LineAt(b), snap.LineAt(a), snap.LineAt(b)
}

// diffReplace puts text in the place of the runes of the document an edit
// has selected, and the caret at its end. The typing is the editor's: the
// document's history takes it as it takes typing, which undo takes back.
func (e *diffEdit) diffReplace(text string) {
	if e.doc.locked() {
		return
	}
	e.doc.observe()
	e.endCompose()
	e.doc.observe()
	snap := e.doc.buf.Snapshot()
	lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
	lo, hi = min(lo, snap.Len()), min(hi, snap.Len())
	e.doc.buf.Replace(lo, hi, text)
	at := lo + utf8.RuneCountInString(text)
	e.ed.sel = [2]int{at, at}
	e.doc.observe()
}

// diffCaretTo puts the caret of an edit at an offset, extending the
// selection from where it was when extend says.
func (e *diffEdit) diffCaretTo(at int, extend bool) {
	snap := e.doc.buf.Snapshot()
	at = min(max(at, 0), snap.Len())
	anchor := at
	if extend {
		anchor = e.ed.sel[0]
	}
	e.ed.sel = [2]int{anchor, at}
}

// beginCompose takes what an input method marks now in the place of what
// it marked before. The marked text sits in the buffer from where the
// caret was when composing began, so that every mark replaces the last
// one wherever it began, and the edits of composing join one step of
// undo, which the text it ends with commits as. An empty mark ends
// composing and takes what it marked back.
func (e *diffEdit) beginCompose(text string) {
	if e.doc.locked() {
		return
	}
	if text == "" {
		e.endCompose()
		return
	}
	snap := e.doc.buf.Snapshot()
	at, marked := 0, 0
	if e.compose != "" {
		at, marked = e.composeAt, utf8.RuneCountInString(e.compose)
	} else {
		e.doc.observe()
		e.doc.hist.group = groupOpen
		// Composing begins in the selection's place: the mark takes what
		// is selected, as the text it commits with will.
		lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
		at = min(lo, snap.Len())
		marked = min(hi, snap.Len()) - at
		e.composeAt = at
		e.composeWas = snap.Slice(at, at+marked)
	}
	e.compose = text
	e.doc.buf.Replace(at, at+marked, text)
	e.doc.observe()
	end := at + utf8.RuneCountInString(text)
	e.ed.sel = [2]int{end, end}
}

// commitCompose puts the text an input method finished in the place of
// what it marked, and closes the step of undo composing made.
func (e *diffEdit) commitCompose(text string) {
	if e.compose == "" || e.doc.locked() {
		return
	}
	if text == "" {
		e.endCompose()
		return
	}
	e.doc.buf.Replace(e.composeAt, e.composeAt+utf8.RuneCountInString(e.compose), text)
	e.doc.observe()
	end := e.composeAt + utf8.RuneCountInString(text)
	e.ed.sel = [2]int{end, end}
	e.compose, e.composeWas = "", ""
	e.doc.hist.group = groupNone
}

// endCompose takes what an input method had marked back, once composing
// ends without text, or the mark is to go for another edit first.
func (e *diffEdit) endCompose() {
	if e.compose == "" {
		return
	}
	snap := e.doc.buf.Snapshot()
	at := min(e.composeAt, snap.Len())
	n := utf8.RuneCountInString(e.compose)
	if at+n <= snap.Len() {
		e.doc.buf.Replace(at, at+n, e.composeWas)
		e.doc.observe()
	}
	end := at + utf8.RuneCountInString(e.composeWas)
	e.ed.sel = [2]int{at, end}
	e.compose, e.composeWas = "", ""
	e.doc.hist.group = groupNone
}

// diffEditedText returns the text the selection of an edit holds, "" for
// none.
func (e *diffEdit) diffEditedText() string {
	snap := e.doc.buf.Snapshot()
	lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
	lo, hi = min(lo, snap.Len()), min(hi, snap.Len())
	return snap.Slice(lo, hi)
}

// paintDiffEdit paints the selection and the caret of a diff's edited
// side over its rows: what is selected, filled line by line, and a bar
// where the caret is, as an editor shows them.
func (a *app) paintDiffEdit(pt *ui.Painter, v *diffView, e *diffEdit, l diffGeometry, first, last int, r ui.Rect, focused bool) {
	p, fnt := a.pal, a.font
	file := slices.Index(v.files, e.f)
	if file < 0 {
		return
	}
	snap := e.doc.buf.Snapshot()
	if snap.Len() == 0 {
		return
	}
	lo, hi := min(e.ed.sel[0], e.ed.sel[1]), max(e.ed.sel[0], e.ed.sel[1])
	lo, hi = min(lo, snap.Len()), min(hi, snap.Len())
	lineLo, lineHi := snap.LineAt(lo), snap.LineAt(hi)
	caret := min(e.ed.sel[1], snap.Len())
	caretLine := snap.LineAt(caret)
	lineH := fnt.lineH
	cols := l.cols[btoi(v.split)]
	selCol := p.selected
	if !focused {
		selCol = p.selectedDim
	}
	for i := first; i <= last; i++ {
		row := &v.rows[i]
		if row.file != file || row.kind != rowLine || row.new < 0 {
			continue
		}
		if row.new < lineLo || row.new > lineHi {
			continue
		}
		// Where the row's text starts, and where it shows: the working
		// tree side of a split diff, which scrolls in its own when the
		// sides do not scroll together.
		x := r.X + l.textX
		y := r.Y + editorPadY + float32(v.tops[i])*lineH
		if v.split {
			x += l.right()
			ys, _ := a.sideRowY(v, 1, i)
			y = r.Y + ys
		}
		if row.new < snap.LineCount() {
			s0, s1 := snap.LineRange(row.new)
			text := clipLine(snap.Slice(s0, s1))
			from, to := 0, utf8.RuneCountInString(text)
			if row.new == lineLo {
				from = lo - s0
			}
			if row.new == lineHi {
				to = hi - s0
			}
			for _, rc := range diffSelRects(fnt, text, from, to, x, y, cols, v.built.wrap) {
				pt.Fill(rc, selCol, 0)
			}
		}
		if row.new == caretLine && focused {
			text := ""
			if caretLine < len(e.f.new.lines) {
				text = clipLine(e.f.new.lines[caretLine])
			}
			cs, _ := snap.LineRange(caretLine)
			cx, k := diffCaretX(fnt, text, caret-cs, cols, v.built.wrap)
			pt.Fill(ui.Rect{X: x + cx - 0.5, Y: y + float32(k)*lineH, W: 1.5, H: lineH}, p.cursor, 0)
		}
	}
}

// diffSelRects returns the rects of the runes from up to to of a line of a
// diff, on the rows the line wraps to, at x where its text starts and y
// where the row shows.
func diffSelRects(fnt codeFont, text string, from, to int, x, y float32, cols int, wrap bool) []ui.Rect {
	shown, at := expandTabs(text)
	breaks := diffBreaks(shown, cols, wrap)
	b0 := at(min(runeByte(text, from), len(text)))
	b1 := at(min(runeByte(text, to), len(text)))
	var out []ui.Rect
	for k := 0; k+1 < len(breaks); k++ {
		lo, hi := breaks[k], breaks[k+1]
		s0, s1 := max(b0, lo), min(b1, hi)
		if s1 <= s0 {
			continue
		}
		out = append(out, ui.Rect{X: x + fnt.width(shown[lo:s0]), Y: y + float32(k)*fnt.lineH,
			W: fnt.width(shown[s0:s1]), H: fnt.lineH})
	}
	return out
}

// runeByte returns the byte offset of the col-th rune of s, len(s) past
// its end.
func runeByte(s string, col int) int {
	i := 0
	for ; i < len(s) && col > 0; col-- {
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	return i
}

// diffCaretRect returns where the caret of a diff's edit shows, for the
// input methods to put their candidates at.
func (a *app) diffCaretRect(v *diffView, e *diffEdit, l diffGeometry) (ui.Rect, bool) {
	row := v.caretRow(e)
	if row < 0 || row >= len(v.tops) {
		return ui.Rect{}, false
	}
	snap := e.doc.buf.Snapshot()
	caret := min(e.ed.sel[1], snap.Len())
	line := snap.LineAt(caret)
	s, _ := snap.LineRange(line)
	text := ""
	if line < len(e.f.new.lines) {
		text = clipLine(e.f.new.lines[line])
	}
	x, k := diffCaretX(a.font, text, caret-s, l.cols[btoi(v.split)], v.built.wrap)
	y := editorPadY + float32(v.tops[row])*a.font.lineH
	x += l.textX
	if v.split {
		// The caret's line stands where the right side has it.
		x += l.right()
		y, _ = a.sideRowY(v, 1, row)
	}
	y += float32(k) * a.font.lineH
	return ui.Rect{X: x - 1, Y: y, W: 3, H: a.font.lineH}, true
}

// diffChosenText returns the text of the line a diff has chosen, of the
// side the row shows, "" for none: what copying a line of the diff takes,
// of a side that is read as well as of one that edits.
func (v *diffView) diffChosenText() string {
	i := v.focusRow()
	if i < 0 || i >= len(v.rows) || v.rows[i].kind != rowLine {
		return ""
	}
	r := v.rows[i]
	if r.file >= len(v.files) {
		return ""
	}
	f := v.files[r.file]
	if !f.loaded || f.binary {
		return ""
	}
	if n, newSide := r.line(); n >= 0 && n < len(f.side(newSide).lines) {
		return f.side(newSide).lines[n]
	}
	return ""
}

// diffEditTarget returns the file of the row a diff would begin to edit,
// nil when none of it may be.
func (a *app) diffEditTarget(v *diffView) *diffFile {
	i := v.editRow(v.focusRow())
	if i < 0 {
		return nil
	}
	// The rows may stand for files read again since, as a repository
	// switch clears them.
	if k := v.rows[i].file; k < len(v.files) && v.files[k].writable() {
		return v.files[k]
	}
	return nil
}
