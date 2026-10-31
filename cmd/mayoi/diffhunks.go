package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/linediff"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// hunkAction is what a button of a run of changes in a diff does to them.
type hunkAction int

const (
	hunkStage   hunkAction = iota // into the index
	hunkUnstage                   // out of the index, which has them as HEAD does
	hunkDiscard                   // out of the work tree, which has them as the index does
	hunkAppend                    // append the compared side after the work tree change
)

// hunkArrow is a direction the lines of a change of a diff can be
// applied in: the side dir writes takes the lines of the other side in
// place of the change's own lines, in a document of its text. Its label
// is what the arrow says it does, as the versions compared name it, and
// its tip tells which of them it reads and which it writes.
type hunkArrow struct {
	dir   int    // +1 writes the new side, -1 writes the old
	label string // Revert, as the base's lines go into the local text
	icon  *ui.SVG
	tip   string
}

// hunkArrows returns the arrows of a file of a diff, one for each side
// its changes can be written to. A local comparison offers both sides;
// Git's working tree diff offers only the revert arrow, while the index and
// the versions Git keeps are read only. A file one side of the diff lacks
// whole has no arrows, since its changes go in or out with the file itself.
func hunkArrows(f *diffFile) []hunkArrow {
	if !f.loaded || f.binary || !f.old.ok || !f.new.ok || f.entry.Submodule() {
		return nil
	}
	if f.base == diffLocal {
		var out []hunkArrow
		if localFileWritable(f.peerPath) {
			out = append(out, hunkArrow{dir: 1, label: locales.DiffAcceptLeft(), icon: iconArrowRight, tip: locales.DiffAcceptLeftTip()})
		}
		if localFileWritable(f.path) {
			out = append(out, hunkArrow{dir: -1, label: locales.DiffAcceptRight(), icon: iconArrowLeft, tip: locales.DiffAcceptRightTip()})
		}
		return out
	}
	if f.base != diffIndex {
		return nil
	}
	old, new := f.base.sides()
	return []hunkArrow{{
		dir:   1,
		label: locales.RevertButton(),
		icon:  iconArrowRight,
		tip:   locales.DiffArrowRevertTip(old, new),
	}}
}

func localFileWritable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0222 != 0
}

// hunkActions returns what can be done to the changes of a file of a
// diff, one change at a time: those of a file in both versions only, as
// a file added or removed goes in or out whole, as a submodule does.
func hunkActions(f *diffFile) []hunkAction {
	if !f.loaded || f.binary || !f.old.ok || !f.new.ok || f.entry.Submodule() {
		return nil
	}
	switch {
	case f.base == diffIndex && f.group == groupChanges:
		return []hunkAction{hunkDiscard, hunkStage, hunkAppend}
	case f.base == diffStaged:
		return []hunkAction{hunkUnstage}
	}
	return nil
}

// hunkRows returns the rows of the kth change of a diff, from its first
// changed row to past its last, as they are laid out.
func (v *diffView) hunkRows(k int) (from, to int) {
	from = v.starts[k]
	to = from + 1
	for to < len(v.rows) && v.rows[to].changed() && v.rows[to].file == v.rows[from].file && v.rows[to].hunk == v.rows[from].hunk {
		to++
	}
	return from, to
}

// hunkHead returns the header of the run of changes row i of a diff is
// in, -1 when it is in none: in the full files, which have no headers,
// or out of the runs.
func (v *diffView) hunkHead(i int) int {
	if i < 0 || i >= len(v.rows) {
		return -1
	}
	file := v.rows[i].file
	// A run is a few lines on each side of its changes: the walk is short
	// but in the full files, where it stops early.
	for k := i; k >= 0 && k > i-2000 && v.rows[k].file == file; k-- {
		switch v.rows[k].kind {
		case rowHunk:
			return k
		case rowLine, rowComment:
		default:
			return -1
		}
	}
	return -1
}

// hunksAt returns the file of row i of a diff and its changes the row
// stands for: those of the run a header heads, or a changed line's.
func (v *diffView) hunksAt(i int) (*diffFile, []linediff.Hunk) {
	if i < 0 || i >= len(v.rows) {
		return nil, nil
	}
	r := v.rows[i]
	f := v.files[r.file]
	var ks []int
	switch {
	case r.changed():
		ks = []int{r.hunk}
	case r.kind == rowHunk:
		for k := i + 1; k < len(v.rows) && (v.rows[k].kind == rowLine || v.rows[k].kind == rowComment) && v.rows[k].file == r.file; k++ {
			if h := v.rows[k].hunk; h >= 0 && !slices.Contains(ks, h) {
				ks = append(ks, h)
			}
		}
	}
	var hs []linediff.Hunk
	for _, k := range ks {
		if k < len(f.hunks) {
			hs = append(hs, f.hunks[k])
		}
	}
	return f, hs
}

// hunkButtons are the buttons of the header of a run of changes, row i,
// whose right is at right and top at y in the diff's content, contentW
// wide: staging the changes, unstaging them, or discarding them. It
// returns about where they are.
func (a *app) hunkButtons(c *ui.Context, v *diffView, i int, right, contentW, y float32) ui.Rect {
	p := a.pal
	f, hs := v.hunksAt(i)
	acts := hunkActions(f)
	if len(hs) == 0 || len(acts) == 0 {
		return ui.Rect{}
	}
	type button struct {
		icon             *ui.SVG
		label, name, tip string
		act              hunkAction
	}
	var bs []button
	for _, act := range acts {
		switch act {
		case hunkStage:
			bs = append(bs, button{iconPlus, locales.HunkStage(), locales.HunkStageLabel(), locales.HunkStageTip(), act})
		case hunkUnstage:
			bs = append(bs, button{iconMinus, locales.HunkUnstage(), locales.HunkUnstageLabel(), locales.HunkUnstageTip(), act})
		case hunkDiscard:
			bs = append(bs, button{iconUndo, locales.HunkDiscard(), locales.HunkDiscardLabel(), locales.HunkDiscardTip(), act})
		case hunkAppend:
			bs = append(bs, button{iconArrowRight, locales.HunkAppend(), locales.HunkAppend(), locales.HunkAppendTip(), act})
		}
	}
	h := a.font.lineH - 2
	row := ui.Row(c.Key("hunk-buttons")).Absolute().Right(contentW - right + 18).Top(y + 1).Gap(4)
	// Its left is not known until it is laid out: a box as wide as the
	// buttons can be keeps clicks from the row under them.
	w := float32(len(bs)) * 84
	row.Children(func() {
		for _, b := range bs {
			btn := ui.ButtonBase(c.Key("hunk:"+b.label)).Height(h).Padding(0, 7).Gap(4).Radius(4).AlignItems(ui.Center).
				Background(p.editor).Border(1, p.border).Label(b.name).Tooltip(b.tip)
			col := p.muted
			if btn.Hovered() {
				btn.Background(p.hover.Over(p.editor))
				col = p.text
			}
			// They come and go with the pointer: they fade, rather than pop.
			btn.Transition(revealFade)
			pressPose(btn)
			btn.Children(func() {
				ui.Icon(c, b.icon).FontSize(12).TextColor(col)
				ui.Text(c, b.label).FontSize(11.5).TextColor(col).SingleLine()
			})
			if btn.Clicked() {
				act := b.act
				a.post(func() { a.applyHunks(v, f, hs, act) })
			}
		}
	})
	return ui.Rect{X: right - 18 - w, Y: y, W: w + 18, H: a.font.lineH}
}

// applyHunks stages changes of a file of a diff, unstages them or
// discards them. The texts are read again first, and the changes found
// again among theirs, so that what is done is to the files as they are.
func (a *app) applyHunks(v *diffView, f *diffFile, hs []linediff.Hunk, act hunkAction) {
	ctx := a.repositoryForPath(f.path)
	if f.repoRoot != "" {
		ctx = a.repositoryForRoot(f.repoRoot)
	}
	if ctx.git.repo == nil || len(hs) == 0 {
		return
	}
	path := f.path
	// The changes are of the editor's text as the diff last compared it.
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion && act != hunkUnstage {
		a.fail("%s", locales.DiffCatchingUp())
		return
	}
	switch act {
	case hunkStage:
		work, crlf, ok := a.workText(f)
		ctx.gitDo("", func(r *git.Repo) error {
			if !ok {
				var err error
				if work, crlf, err = readWork(path); err != nil {
					return err
				}
			}
			text, _, err := r.Show("", path)
			if err != nil {
				return err
			}
			index, lines := strings.Split(text, "\n"), strings.Split(work, "\n")
			picked := overlapping(linediff.Hunks(index, lines), hs)
			if len(picked) == 0 {
				return nil
			}
			return r.StageText(path, joinLines(linediff.Apply(index, lines, picked), crlf))
		}, nil)
	case hunkUnstage:
		ctx.gitDo("", func(r *git.Repo) error {
			head, _, err := r.Show("HEAD", path)
			if err != nil {
				return err
			}
			text, _, err := r.Show("", path)
			if err != nil {
				return err
			}
			_, crlf, _ := readWork(path)
			from, index := strings.Split(head, "\n"), strings.Split(text, "\n")
			picked := overlapping(linediff.Hunks(from, index), hs)
			if len(picked) == 0 {
				return nil
			}
			return r.StageText(path, joinLines(linediff.Apply(index, from, inverted(picked)), crlf))
		}, nil)
	case hunkDiscard:
		a.discardHunks(v, f, hs)
	case hunkAppend:
		a.appendHunks(v, f, hs)
	}
}

// discardHunks puts changes of a file back as the index has them, in the
// file's document, as one step of undo.
// appendHunks keeps the work tree side of each change and appends the
// corresponding lines from the compared side after it. Each block is one
// undoable edit, and an empty source is a no-op.
func (a *app) appendHunks(v *diffView, f *diffFile, hs []linediff.Hunk) {
	if len(hs) == 0 || f == nil || f.base != diffIndex {
		return
	}
	if f.running || f.gitStale {
		a.fail("%s", locales.DiffCatchingUpFiles())
		return
	}
	if v != nil {
		h := hs[0]
		v.flashLines(f, 1, h.B+h.BLen, h.B+h.BLen+h.ALen)
	}
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion {
		a.fail("%s", locales.DiffCatchingUp())
		return
	}
	if d, shown := a.editorOf(a.diffDoc(f)); d != nil {
		appendNow := func(d *editor) { a.appendInto(v, d, f, hs) }
		if shown {
			a.queueEdit(d, appendNow)
		} else {
			appendNow(d)
		}
		return
	}
	name := filepath.Base(f.path)
	a.confirm = &confirm{title: locales.DiffAppendTitle(name), message: locales.DiffAppendDetail(), ok: locales.HunkAppend(), run: func() {
		a.openText(func() { a.openFile(f.path, false) })
		d, _ := a.editorOf(a.diffDoc(f))
		if d == nil || d.loading || d.loadErr != nil || d.offline {
			a.fail("%s", locales.DiffOpenFailAppend(name))
			return
		}
		if d.buf.Snapshot().String() != f.new.text {
			f.diskStale = true
			a.fail("%s", locales.DiffChangedSinceDrawn(name))
			return
		}
		a.appendInto(v, d, f, hs)
	}}
}

func (a *app) appendInto(v *diffView, d *editor, f *diffFile, hs []linediff.Hunk) {
	if d == nil || len(hs) == 0 {
		return
	}
	snap := d.buf.Snapshot()
	var edits []textEdit
	for _, h := range slices.Backward(hs) {
		if h.B < 0 || h.B+h.BLen > len(f.new.lines) || h.ALen == 0 {
			continue
		}
		lines := append(append([]string{}, f.new.lines[h.B:h.B+h.BLen]...), f.old.lines[h.A:h.A+h.ALen]...)
		e, ok := hunkEdit(snap, h, lines)
		if ok {
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 {
		return
	}
	d.peek = -1
	d.edit([2]int{edits[0].start, edits[0].start}, edits...)
	if v != nil {
		v.edited = d.document
	}
}

func (a *app) discardHunks(v *diffView, f *diffFile, hs []linediff.Hunk) {
	a.revertInto(v, f, hs, "Discard")
}

// restoreDeletedFile puts a tracked file removed from the work tree back as
// the index has it. It is a file-level operation: an empty diff is never used
// to stand in for the file's existence, and an open document is reloaded as
// one visible state change.
func (a *app) restoreDeletedFile(e git.Entry) {
	if e.Path == "" || e.Untracked() || e.Submodule() {
		return
	}
	a.confirm = &confirm{
		title:   locales.DiffRestoreTitle(filepath.Base(e.Path)),
		message: locales.DiffRestoreDetail(),
		ok:      locales.DiffRestoreFile(),
		run: func() {
			a.gitDo(locales.DiffRestoring(filepath.Base(e.Path)), func(r *git.Repo) error {
				return r.Discard(e.Path)
			}, func() {
				if i := a.docIndex(e.Path); i >= 0 {
					a.reloadDoc(a.docs[i])
				}
			})
		},
	}
}

// hunkArrowW is the side of the round button of the arrow of a change of
// a diff, which fits between the rail and the line numbers of the one
// column, where they leave room; hunkArrowSplitW is its side in the
// gutter between the sides of a split diff, which has room for two.
const (
	hunkArrowW      float32 = 13
	hunkArrowSplitW float32 = 16
)

// hunkArrowRects returns where the arrows ars of the kth change of a diff
// stand in its content. Side by side, they stand in the gutter between
// the sides, each by the first line of the side it reads: alone in the
// middle, or two, each by the side it reads. In the one column, they
// stand between the rail and the line numbers, by the change's first
// line, staying by it as the lines scroll along.
func (a *app) hunkArrowRects(v *diffView, k int, ars []hunkArrow, l diffGeometry) []ui.Rect {
	lineH := a.font.lineH
	out := make([]ui.Rect, len(ars))
	if !v.split {
		from, _ := v.hunkRows(k)
		y := editorPadY + float32(v.tops[from])*lineH + lineH/2 - hunkArrowW/2
		for i := range ars {
			out[i] = ui.Rect{X: v.scroll.X + diffRailW + float32(i)*hunkArrowW, Y: y, W: hunkArrowW, H: hunkArrowW}
		}
		return out
	}
	w := hunkArrowSplitW
	lt, lb, rt, rb := a.hunkSides(v, k)
	for i, ar := range ars {
		top, bot := lt, lb
		x := v.scroll.X + l.half + l.mid/2 - w - 1
		if ar.dir < 0 {
			top, bot = rt, rb
			x = v.scroll.X + l.half + l.mid/2 + 1
		}
		if len(ars) == 1 {
			x = v.scroll.X + l.half + (l.mid-w)/2
		}
		y := top + min(lineH, bot-top)/2 - w/2
		out[i] = ui.Rect{X: x, Y: y, W: w, H: w}
	}
	return out
}

// hunkArrowUI is the arrows of the kth change of a diff, each of which
// puts the lines of one of its sides in place of its lines of the other,
// where hunkArrowRects has them. It returns about where they are, or
// nothing for a change that takes no arrow, or that is out of the view,
// and whether the pointer is on one of them.
func (a *app) hunkArrowUI(c *ui.Context, v *diffView, k, first, last int, l diffGeometry) (ui.Rect, bool) {
	from, to := v.hunkRows(k)
	if from > last || to <= first || from >= len(v.rows) {
		return ui.Rect{}, false
	}
	f := v.fileAt(k)
	ars := hunkArrows(f)
	if len(ars) == 0 {
		return ui.Rect{}, false
	}
	h := f.hunks[v.rows[from].hunk]
	p := a.pal
	out, hot := ui.Rect{}, false
	for i, rc := range a.hunkArrowRects(v, k, ars, l) {
		ar := ars[i]
		btn := ui.ButtonBase(c.Key(fmt.Sprintf("arrow:%d:%d", k, i))).Absolute().Left(rc.X).Top(rc.Y).Size(rc.W, rc.H).
			Radius(rc.W/2).AlignItems(ui.Center).Justify(ui.Center).Label(ar.label).Tooltip(ar.tip).
			Background(p.editor).Border(1, p.border)
		col := p.muted
		hover := btn.Hovered()
		// Holding Control as the arrow is hovered shows its Append form:
		// the lines of the compared side go after the change, as the tip
		// and the icon then say.
		icon := ar.icon
		if hover && f.base == diffIndex && c.Modifiers()&ui.Ctrl != 0 {
			btn.Label(locales.HunkAppend()).Tooltip(locales.HunkAppendTip())
			icon = iconArrowDown
		}
		if hover {
			btn.Background(p.fill).Border(1, p.fill).Shadow(0, 1, 4, 0, p.shadow)
			col = p.fillText
			hot = true
		}
		btn.Transition(hoverFade)
		arrowPose(btn, hover)
		btn.Children(func() { ui.Icon(c, icon).FontSize(rc.W - 4).TextColor(col) })
		if btn.Clicked() {
			append := btn.ClickModifiers()&ui.Ctrl != 0 && f.base != diffLocal
			a.post(func() {
				v.pane.Focus()
				if append {
					a.appendHunks(v, f, []linediff.Hunk{h})
				} else {
					a.applyArrow(v, f, h, ar)
				}
			})
		}
		if out.W == 0 {
			out = rc
		} else {
			x0, y0 := min(out.X, rc.X), min(out.Y, rc.Y)
			x1, y1 := max(out.X+out.W, rc.X+rc.W), max(out.Y+out.H, rc.Y+rc.H)
			out = ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		}
	}
	return out, hot
}

// arrowPose has a round button of a diff grow a little under the
// pointer, and give as it is pressed.
func arrowPose(b ui.Element, hover bool) {
	pose := ui.Pose{}
	switch {
	case b.Pressed():
		pose.Scale = 0.88
	case hover:
		pose.Scale = 1.12
	}
	b.AnimatePose(pose, ui.Snappy)
}

// applyArrow does what the arrow of a change of a diff does: puts the
// lines of the side it reads in place of the change's lines of the side
// it writes, in a document of that side's text, as one step of undo. The
// diff v keeps the document, as the undo of its keyboard works in it.
func (a *app) applyArrow(v *diffView, f *diffFile, h linediff.Hunk, ar hunkArrow) {
	// The lines written glow as the diff shows them.
	if ar.dir > 0 {
		v.flashLines(f, 1, h.B, h.B+h.ALen)
	} else {
		v.flashLines(f, 0, h.A, h.A+h.BLen)
	}
	if f.base == diffLocal {
		a.applyLocalArrow(v, f, h, ar)
		return
	}
	switch {
	case ar.dir > 0:
		a.revertInto(v, f, []linediff.Hunk{h}, ar.label)
	default:
		// No side these diffs compare is written from its new side: the
		// index goes on by staging, and the versions Git keeps are read
		// only, so no such arrow is offered.
	}
}

// applyLocalArrow writes one side of a local two-file comparison into the
// other side's document. The target is opened when needed so the edit joins
// that file's normal undo and save history.
func (a *app) applyLocalArrow(v *diffView, f *diffFile, h linediff.Hunk, ar hunkArrow) {
	if f.running || !f.loaded || f.binary || !f.old.ok || !f.new.ok {
		a.fail("%s", locales.DiffCatchingUpFiles())
		return
	}
	var path, expected string
	var target linediff.Hunk
	var lines []string
	var version uint64
	if h.A < 0 || h.B < 0 || h.A+h.ALen > len(f.old.lines) || h.B+h.BLen > len(f.new.lines) {
		return
	}
	switch {
	case ar.dir > 0:
		path, expected, target, lines, version = f.peerPath, f.new.text, h, f.old.lines[h.A:h.A+h.ALen], f.peerDocVersion
	default:
		path, expected = f.path, f.old.text
		target = linediff.Hunk{A: h.B, ALen: h.BLen, B: h.A, BLen: h.ALen}
		lines, version = f.new.lines[h.B:h.B+h.BLen], f.docVersion
	}
	if path == "" {
		return
	}
	doc := a.diffDoc(f)
	if ar.dir > 0 {
		doc = a.diffPeerDoc(f)
	}
	if doc != nil && version != 0 && doc.buf.Version() != version {
		a.fail("%s", locales.DiffCatchingUp())
		return
	}
	if doc == nil {
		a.openText(func() { a.openFile(path, false) })
		if ar.dir > 0 {
			doc = a.diffPeerDoc(f)
		} else {
			doc = a.diffDoc(f)
		}
	}
	if doc == nil || doc.locked() {
		a.fail("%s", locales.DiffOpenFailApply(filepath.Base(path)))
		return
	}
	if got := doc.buf.Snapshot().String(); got != expected {
		f.diskStale = true
		a.fail("%s", locales.DiffChangedSinceDrawn(filepath.Base(path)))
		return
	}
	d, _ := a.editorOf(doc)
	if d == nil {
		d = newEditor(doc)
	}
	e, ok := hunkEdit(doc.buf.Snapshot(), target, lines)
	if !ok {
		a.fail("%s", locales.DiffNoLongerFits())
		return
	}
	d.peek = -1
	d.edit([2]int{e.start, e.start}, e)
	if v != nil {
		v.edited = doc
	}
}

// revertInto puts the changes hs of a file of a diff back as the old
// side has them, in the file's document, as one step of undo: through an
// editor shown, in its frame; through one out of sight, at once; or, for
// a file no editor has, through one opened for it and found to be what
// the diff compared, after asking about it as ok names the action. A
// diff that is behind the files or the document is read again first: its
// changes are not to go over what is there now.
func (a *app) revertInto(v *diffView, f *diffFile, hs []linediff.Hunk, ok string) {
	if f.running || f.gitStale {
		a.fail("%s", locales.DiffCatchingUpFiles())
		return
	}
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion {
		a.fail("%s", locales.DiffCatchingUp())
		return
	}
	if d, shown := a.editorOf(a.diffDoc(f)); d != nil {
		into := func(d *editor) {
			a.discardInto(d, f, hs)
			if v != nil {
				v.edited = d.document
			}
		}
		// An editor shown takes edits in its frame, once its text area's
		// selection is known; one out of sight has its own.
		if shown {
			a.queueEdit(d, into)
		} else {
			into(d)
		}
		return
	}
	name := filepath.Base(f.path)
	title := locales.DiffRevertChangeTo(ok, name)
	if len(hs) > 1 {
		title = locales.DiffRevertChangesTo(ok, name)
	}
	a.confirm = &confirm{title: title, message: locales.DiffRevertDetail(),
		ok: ok, danger: true, run: func() {
			a.intoByOpening(v, f, hs)
		}}
}

// intoByOpening opens a file that no editor has, checks that its text
// is what the diff compared, and puts the changes of hunks hs back as
// the index has them, as one step of undo. A file that changed since the
// diff was drawn is left as it is and read again: its changes do not
// fit, and are not to go over what it has now.
func (a *app) intoByOpening(v *diffView, f *diffFile, hs []linediff.Hunk) {
	if f.running || f.gitStale {
		a.fail("%s", locales.DiffCatchingUpFiles())
		return
	}
	a.openText(func() { a.openFile(f.path, false) })
	d, _ := a.editorOf(a.diffDoc(f))
	if d == nil || d.loadErr != nil || d.offline {
		a.fail("%s", locales.DiffOpenFailRevert(filepath.Base(f.path)))
		return
	}
	if d.loading {
		a.fail("%s", locales.DiffStillReading())
		return
	}
	if d.buf.Snapshot().String() != f.new.text {
		// The file is not the text the changes were found among.
		f.gitStale = true
		a.fail("%s", locales.DiffChangedSinceDrawn(filepath.Base(f.path)))
		return
	}
	a.discardInto(d, f, hs)
	if v != nil {
		v.edited = d.document
	}
}

// discardInto puts the changes of hunks hs of a file of a diff back as
// the index has them, in an editor of the text the diff compared them
// in, as one step of undo: the edits made together, so that undo takes
// the whole discard back, and redo makes it again.
func (a *app) discardInto(d *editor, f *diffFile, hs []linediff.Hunk) {
	old := f.old.lines
	snap := d.buf.Snapshot()
	var edits []textEdit
	at := -1
	// From the last up, so that the lines of those above stay put.
	for _, h := range slices.Backward(hs) {
		if h.A+h.ALen > len(old) {
			continue
		}
		e, ok := hunkEdit(snap, h, old[h.A:h.A+h.ALen])
		if !ok {
			continue
		}
		if at < 0 || e.start < at {
			at = e.start
		}
		edits = append(edits, e)
	}
	if len(edits) == 0 {
		return
	}
	d.peek = -1
	d.edit([2]int{at, at}, edits...)
}

// editorOf returns an editor of an open document, nil for none: one a
// column shows when there is one, and whether it is shown.
func (a *app) editorOf(doc *document) (*editor, bool) {
	if doc == nil {
		return nil, false
	}
	var hidden *editor
	for _, col := range a.cols {
		for i, t := range col.tabs {
			switch {
			case t.ed == nil || t.ed.document != doc:
			case i == col.active:
				return t.ed, true
			case hidden == nil:
				hidden = t.ed
			}
		}
	}
	return hidden, false
}

// workText returns the text of a file of a diff in the work tree, with
// its line endings as \n, and whether they are \r\n on disk, when it is
// open in an editor; ok is false when it must be read from disk.
func (a *app) workText(f *diffFile) (text string, crlf, ok bool) {
	if doc := a.diffDoc(f); doc != nil {
		return doc.buf.Snapshot().String(), doc.crlf, true
	}
	return "", false, false
}

// readWork reads a file of the work tree, with its line endings as \n,
// and reports whether they were \r\n.
func readWork(path string) (string, bool, error) {
	data, err := vfs.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	return strings.ReplaceAll(string(data), "\r\n", "\n"), crlf, nil
}

// joinLines joins lines with the line ending of a file.
func joinLines(lines []string, crlf bool) string {
	if crlf {
		return strings.Join(lines, "\r\n")
	}
	return strings.Join(lines, "\n")
}

// appendAtCursor appends the lines of the compared side of the change
// the diff has chosen, as the arrow with Control does.
func (a *app) appendAtCursor() {
	if t := a.activeTab(); t != nil && t.diff != nil {
		i := t.diff.focusRowAt(t.diff.focusRow())
		if f, hs := t.diff.hunksAt(i); f != nil && len(hs) > 0 {
			a.appendHunks(t.diff, f, hs)
		}
	}
}

// stageAtCursor stages the changes of a diff at its chosen row, or the
// row at the top, or unstages them when they are staged.
func (a *app) stageAtCursor(v *diffView) {
	i := v.focusRow()
	if h := v.hunkHead(i); h >= 0 && !v.rows[i].changed() {
		i = h
	}
	f, hs := v.hunksAt(i)
	if f == nil {
		return
	}
	for _, act := range hunkActions(f) {
		if act == hunkStage || act == hunkUnstage {
			a.applyHunks(v, f, hs, act)
			return
		}
	}
}
