package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/linediff"
)

// editorHunks returns the runs of lines of an editor's document changed
// since the last commit, when they are of its text as it is.
func editorHunks(d *editor) []linediff.Hunk {
	if d.git.Version != d.buf.Version() {
		return nil
	}
	return d.git.Hunks
}

// stepEditorHunk puts the caret at the start of the next run of lines
// changed after it, or the previous one before it for a step of -1,
// round the ends.
func (a *app) stepEditorHunk(d *editor, step int) {
	hunks := editorHunks(d)
	if len(hunks) == 0 {
		return
	}
	snap := d.buf.Snapshot()
	line := snap.LineAt(min(d.sel[1], snap.Len()))
	k := -1
	if step > 0 {
		for i, h := range hunks {
			if h.B > line {
				k = i
				break
			}
		}
		if k < 0 {
			k = 0
		}
	} else {
		for i := len(hunks) - 1; i >= 0; i-- {
			if h := hunks[i]; h.B+max(h.BLen, 1) <= line {
				k = i
				break
			}
		}
		if k < 0 {
			k = len(hunks) - 1
		}
	}
	at := min(hunks[k].B, snap.LineCount()-1)
	s, _ := snap.LineRange(at)
	d.selectRange(s, s, true)
}

// hunkAt returns the hunk whose mark is by line, -1 for none: the lines
// it added or changed, or where it removed lines.
func hunkAt(hunks []linediff.Hunk, line, n int) int {
	for i, h := range hunks {
		switch {
		case h.BLen > 0 && line >= h.B && line < h.B+h.BLen:
			return i
		case h.BLen == 0 && (line == h.B || h.B >= n && line == n-1):
			return i
		}
	}
	return -1
}

// peekHunk shows the hunk at line in the box under it, or hides the box
// when it shows that hunk already.
func (d *editor) peekHunk(line int) bool {
	hunks := editorHunks(d)
	k := hunkAt(hunks, line, d.buf.Snapshot().LineCount())
	if k < 0 {
		return false
	}
	if d.peek >= 0 && d.peek == hunks[k].B {
		d.peek = -1
	} else {
		d.peek = hunks[k].B
	}
	return true
}

// peekedHunk returns the hunk the box under it shows, and its place
// among the hunks, -1 for none.
func (d *editor) peekedHunk() (linediff.Hunk, int) {
	if d.peek < 0 {
		return linediff.Hunk{}, -1
	}
	hunks := editorHunks(d)
	for i, h := range hunks {
		if h.B == d.peek {
			return h, i
		}
	}
	return linediff.Hunk{}, -1
}

// gutterClick shows the hunk whose mark a press at x, y in the editor's
// text area is on, and reports whether it was on one.
func (a *app) gutterClick(d *editor, x, y float32) bool {
	gutterW := d.gutterW
	if gutterW <= 0 {
		return false
	}
	if gx := x - d.hscroll.X; gx < gutterW-14 || gx > gutterW+4 {
		return false
	}
	n := d.buf.Snapshot().LineCount()
	line := a.geometry(d, n).at(y + d.vscroll.Y - editorPadY)
	return d.peekHunk(line)
}

// stepPeek shows the next hunk in the box, or the previous one, and puts
// the caret there.
func (a *app) stepPeek(d *editor, step int) {
	a.stepEditorHunk(d, step)
	snap := d.buf.Snapshot()
	line := snap.LineAt(min(d.pending0(), snap.Len()))
	if k := hunkAt(editorHunks(d), line, snap.LineCount()); k >= 0 {
		d.peek = editorHunks(d)[k].B
	}
}

// pending0 is where the caret goes in the next frame, or is.
func (d *editor) pending0() int {
	if d.pending != nil {
		return d.pending[0]
	}
	return d.sel[1]
}

// revertHunk puts the lines of a hunk back as they were at HEAD, as a
// step of undo.
func (a *app) revertHunk(d *editor, h linediff.Hunk) {
	base := d.git.Base()
	if base == nil || h.A+h.ALen > len(base) {
		return
	}
	replaceHunk(d, h, base[h.A:h.A+h.ALen])
}

// replaceHunk puts lines in place of the lines of an editor's text a hunk
// changed, as a step of undo: the lines its other side had.
func replaceHunk(d *editor, h linediff.Hunk, lines []string) {
	snap := d.buf.Snapshot()
	n := snap.LineCount()
	old := strings.Join(lines, "\n")
	var e textEdit
	switch {
	case h.BLen > 0 && h.ALen > 0:
		s, _ := snap.LineRange(h.B)
		_, end := snap.LineRange(h.B + h.BLen - 1)
		e = textEdit{s, end, old}
	case h.BLen > 0: // added lines: take them out, with a newline
		s, _ := snap.LineRange(h.B)
		end := snap.Len()
		if h.B+h.BLen < n {
			end, _ = snap.LineRange(h.B + h.BLen)
		} else if h.B > 0 {
			_, s = snap.LineRange(h.B - 1)
		}
		e = textEdit{s, end, ""}
	case h.B < n: // removed lines: put them back above line B
		s, _ := snap.LineRange(h.B)
		e = textEdit{s, s, old + "\n"}
	default:
		e = textEdit{snap.Len(), snap.Len(), "\n" + old}
	}
	d.peek = -1
	at := e.start
	d.edit([2]int{at, at}, e)
}

// stageHunk puts the lines of a hunk in the index as they are in the
// editor, and the index's other lines as they are. The hunk is of the
// lines at HEAD, which the index may have changed: it is found again
// among the index's.
func (a *app) stageHunk(d *editor, h linediff.Hunk) {
	if a.git.repo == nil || d.path == "" {
		return
	}
	path, crlf := d.path, d.crlf
	work := strings.Split(d.buf.Snapshot().String(), "\n")
	d.peek = -1
	a.gitDo("", func(r *git.Repo) error {
		text, _, err := r.Show("", path)
		if err != nil {
			return err
		}
		index := strings.Split(text, "\n")
		picked := overlapping(linediff.Hunks(index, work), []linediff.Hunk{h})
		if len(picked) == 0 {
			return nil // staged already
		}
		sep := "\n"
		if crlf {
			sep = "\r\n"
		}
		return r.StageText(path, strings.Join(linediff.Apply(index, work, picked), sep))
	}, nil)
}

// overlapping returns the hunks of hs whose lines of the new side meet
// those of any of want: the same changes, found again among hunks of
// texts read afresh.
func overlapping(hs, want []linediff.Hunk) []linediff.Hunk {
	var out []linediff.Hunk
	for _, x := range hs {
		for _, h := range want {
			over := x.B < h.B+h.BLen && h.B < x.B+x.BLen
			if x.BLen == 0 || h.BLen == 0 {
				over = x.B >= h.B && x.B <= h.B+h.BLen
			}
			if over {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

// inverted returns the hunks of a against b as hunks of b against a.
func inverted(hs []linediff.Hunk) []linediff.Hunk {
	out := make([]linediff.Hunk, len(hs))
	for i, h := range hs {
		out[i] = linediff.Hunk{A: h.B, ALen: h.BLen, B: h.A, BLen: h.ALen}
	}
	return out
}

// hunkPeekView is the box under a hunk of an editor that shows the lines
// it removed, with buttons to revert or stage it, and to go to the next.
func (a *app) hunkPeekView(c *ui.Context, d *editor) {
	h, k := d.peekedHunk()
	if k < 0 {
		return
	}
	p, f := a.pal, a.font
	snap := d.buf.Snapshot()
	n := snap.LineCount()
	g := a.geometry(d, n)
	y := editorPadY + g.top(min(h.B+h.BLen, n)) - d.vscroll.Y
	if h.BLen == 0 && h.B >= n {
		y = editorPadY + g.height() - d.vscroll.Y
	}
	view := d.hscroll.Bounds(c)
	if y < 0 || y > view.H-24 {
		return
	}
	base := d.git.Base()
	const most = 12
	box := a.floating(c, ui.Column(c.Key("hunk-peek")).Absolute().Top(y+1).Left(d.gutterW-4).Right(14).Radius(6).Clip(), false).Transition(popIn).
		Role(ui.RoleToolbar).Label("Change")
	box.Children(func() {
		ui.Row(c).Height(26).Padding(0, 4, 0, 10).Gap(2).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			what := "Changed"
			switch {
			case h.ALen == 0:
				what = "Added"
			case h.BLen == 0:
				what = "Removed"
			}
			ui.Text(c, fmt.Sprintf("%s · change %d of %d since HEAD", what, k+1, len(editorHunks(d)))).FontSize(11.5).
				TextColor(p.muted).Grow(1).SingleLine()
			if a.git.repo != nil && a.smallButton(c, iconPlus, "Stage Change") {
				a.post(func() { a.stageHunk(d, h) })
			}
			if a.smallButton(c, iconUndo, "Revert Change") {
				a.queueEdit(d, func(d *editor) { a.revertHunk(d, h) })
			}
			if a.smallButton(c, iconArrowUp, a.withKey("Previous Change", "git.prevChange")) {
				a.queueEdit(d, func(d *editor) { a.stepPeek(d, -1) })
			}
			if a.smallButton(c, iconArrowDown, a.withKey("Next Change", "git.nextChange")) {
				a.queueEdit(d, func(d *editor) { a.stepPeek(d, 1) })
			}
			if a.smallButton(c, iconX, "Close (Esc)") {
				d.peek = -1
			}
		})
		if h.ALen == 0 || base == nil {
			return
		}
		ui.Column(c).Padding(3, 0).Background(p.removed.Alpha(0.10)).Children(func() {
			for i := h.A; i < h.A+min(h.ALen, most); i++ {
				line, _ := expandTabs(base[i])
				if len(line) > 400 {
					line = line[:400]
				}
				ui.Text(c, line).Font(f.family).FontSize(f.size).FontWeight(f.weight).FontFeatures(f.featureList()...).Height(f.lineH).SingleLine().TextColor(p.text).
					Padding(0, 10)
			}
			if h.ALen > most {
				ui.Text(c, fmt.Sprintf("… %s more", plural(h.ALen-most, "line"))).FontSize(11.5).TextColor(p.faint).Padding(2, 10)
			}
		})
	})
}

// caretLine returns the line of an editor's caret.
func caretLine(d *editor) int {
	snap := d.buf.Snapshot()
	return snap.LineAt(min(d.sel[1], snap.Len()))
}

// onCaretHunk calls fn with the hunk at the caret's line, if it is in
// one.
func (a *app) onCaretHunk(d *editor, fn func(d *editor, h linediff.Hunk)) {
	hunks := editorHunks(d)
	if k := hunkAt(hunks, caretLine(d), d.buf.Snapshot().LineCount()); k >= 0 {
		fn(d, hunks[k])
	}
}
