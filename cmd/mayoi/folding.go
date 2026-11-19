package main

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/fold"
	"mayoi/internal/highlight"
	"mayoi/internal/keymap"
)

// Code folding: the runs of lines a document folds are found off the main
// thread (foldScan), for each version of its text; each editor of it keeps
// which it folded (foldView), by offsets that follow the edits, and shows
// their lines as text blocks that take no room. The text stays whole:
// copying, saving and undo see every line.

// foldColW is how wide the column of the gutter is that the chevrons of
// the lines that fold show in, between the numbers and the marks of git.
const foldColW = 14

// foldScanDelay is how long a text waits after an edit before it is read
// again for its folds: typing reads it once it pauses, not at each key.
const foldScanDelay = 250 * time.Millisecond

// foldScan holds the runs of lines a document folds, found for a version
// of its text in a language.
type foldScan struct {
	version uint64
	lang    string
	ranges  []fold.Range
	done    bool
	// busy is set while a scan runs; seen is the version last noticed
	// and seenAt when, for a long text to wait for the edits to pause.
	busy   bool
	seen   uint64
	seenAt time.Time
	timing bool // a timer waits for the edits to pause
}

// foldMark is a run of lines folded in an editor: head is the end of the
// line that shows, and end the end of the last line hidden, as offsets
// of runes that follow the edits.
type foldMark struct{ head, end int }

// hiddenRun is a run of lines hidden, from to to.
type hiddenRun struct{ from, to int }

// foldView is what an editor folded.
type foldView struct {
	marks []foldMark // by head
	gen   int        // counts the changes of marks
	// runs are the lines hidden, of version and gen.
	runs              []hiddenRun
	runsVer           uint64
	runsGen, runsLive int
	// restore are the lines a session folded, folded again once the runs
	// of the text are found.
	restore []int
	// settle is set while the text area may not know the heights of lines
	// folded or opened out of view, which it is asked line by line from
	// settleAt.
	settle   bool
	settleAt int
	// turnLine is the line whose chevron turned last, at turnAt.
	turnLine int
	turnAt   time.Time
	// hover is set while the pointer is over the gutter; pills are where
	// the folded lines' marks were painted, in the area.
	hover bool
	pills []foldPill
	// reduce is set while the system asks for less motion.
	reduce bool
}

// foldPill is where the mark after a folded line is, which a click opens.
type foldPill struct {
	r    ui.Rect
	line int
}

// foldLang is the name of the language a document folds as.
func foldLang(doc *document) string {
	if doc.hl == nil {
		return ""
	}
	return highlight.Name(doc.hl.Lexer())
}

// foldRanges returns the runs of lines the document folds: those found for
// its text as it is, or for the last version read while the next is read
// off the main thread.
func (a *app) foldRanges(doc *document) []fold.Range {
	snap := doc.buf.Snapshot()
	s := &doc.foldScan
	lang := foldLang(doc)
	if s.done && s.version == snap.Version() && s.lang == lang || s.busy {
		return s.ranges
	}
	if snap.ByteLen() > fold.MaxBytes {
		s.version, s.lang, s.ranges, s.done = snap.Version(), lang, nil, true
		return nil
	}
	// Edited, the text waits for the edits to pause.
	if s.done && s.lang == lang {
		if s.seen != snap.Version() {
			s.seen, s.seenAt = snap.Version(), time.Now()
		}
		if wait := foldScanDelay - time.Since(s.seenAt); wait > 0 {
			if !s.timing {
				s.timing = true
				a.foldTimer(doc, wait)
			}
			return s.ranges
		}
	}
	s.busy = true
	ver := snap.Version()
	go func() {
		rs := fold.Find(fold.For(lang), snap.String())
		a.post(func() {
			s.busy = false
			s.version, s.lang, s.ranges, s.done = ver, lang, rs, true
			a.foldsFound(doc)
			a.invalidate()
		})
	}()
	return s.ranges
}

// foldTimer waits for the edits of doc to pause for foldScanDelay, then
// draws a frame, which reads the text: no frame while typing goes on.
func (a *app) foldTimer(doc *document, wait time.Duration) {
	time.AfterFunc(wait+time.Millisecond, func() {
		mygo.RunOnMain(func() {
			s := &doc.foldScan
			if left := foldScanDelay - time.Since(s.seenAt); left > 0 {
				a.foldTimer(doc, left)
				return
			}
			s.timing = false
			a.invalidate()
		})
	})
}

// shownFolds returns the runs of lines that fold of an editor shown, none
// in Live Preview, and folds again those of a session restored once found.
func (a *app) shownFolds(d *editor, live bool) []fold.Range {
	if live {
		return nil
	}
	rs := a.foldRanges(d.document)
	if d.folding.restore != nil {
		a.foldsFound(d.document)
	}
	return rs
}

// foldRangesNow returns the runs of lines the document folds as its text
// is now, reading it at once if it was not yet: for a command.
func (a *app) foldRangesNow(doc *document) []fold.Range {
	snap := doc.buf.Snapshot()
	s := &doc.foldScan
	lang := foldLang(doc)
	if !(s.done && s.version == snap.Version() && s.lang == lang) {
		var rs []fold.Range
		if snap.ByteLen() <= fold.MaxBytes {
			rs = fold.Find(fold.For(lang), snap.String())
		}
		s.version, s.lang, s.ranges, s.done = snap.Version(), lang, rs, true
		a.foldsFound(doc)
	}
	return s.ranges
}

// rangeAt returns the run that starts at line among rs.
func rangeAt(rs []fold.Range, line int) (fold.Range, bool) {
	k := sort.Search(len(rs), func(i int) bool { return rs[i].Start >= line })
	if k < len(rs) && rs[k].Start == line {
		return rs[k], true
	}
	return fold.Range{}, false
}

// foldsFound follows the runs found for the text as it is: each editor's
// folds whose line no longer starts a run open, the others hide what
// their run does now, and those of a session restored fold.
func (a *app) foldsFound(doc *document) {
	s := &doc.foldScan
	snap := doc.buf.Snapshot()
	if !s.done || s.version != snap.Version() {
		return
	}
	for _, d := range doc.views {
		fv := &d.folding
		if fv.restore != nil {
			for _, line := range fv.restore {
				if r, ok := rangeAt(s.ranges, line); ok {
					d.addFold(snap, r)
				}
			}
			fv.restore = nil
			// The caret stays in sight.
			d.revealAt(snap, d.sel[1])
			d.revealAt(snap, d.sel[0])
		}
		if len(fv.marks) == 0 {
			continue
		}
		keep := fv.marks[:0]
		changed := false
		for _, m := range fv.marks {
			line := snap.LineAt(min(m.head, snap.Len()))
			r, ok := rangeAt(s.ranges, line)
			if !ok {
				changed = true
				continue
			}
			_, end := snap.LineRange(r.End)
			if end != m.end {
				m.end, changed = end, true
			}
			keep = append(keep, m)
		}
		fv.marks = keep
		if changed {
			fv.changed()
		}
	}
}

// changed notes that the folds changed: the text area is told the heights
// of the lines again.
func (fv *foldView) changed() {
	fv.gen++
	fv.settle, fv.settleAt = true, 0
}

// addFold folds the lines of r, unless they are folded.
func (d *editor) addFold(snap ui.TextSnapshot, r fold.Range) {
	if r.End <= r.Start || r.End >= snap.LineCount() {
		return
	}
	_, head := snap.LineRange(r.Start)
	_, end := snap.LineRange(r.End)
	fv := &d.folding
	k := sort.Search(len(fv.marks), func(i int) bool { return fv.marks[i].head >= head })
	if k < len(fv.marks) && fv.marks[k].head == head {
		return
	}
	fv.marks = slices.Insert(fv.marks, k, foldMark{head, end})
	fv.changed()
}

// removeFold opens the fold whose line that shows is line, and reports
// whether there was one.
func (d *editor) removeFold(snap ui.TextSnapshot, line int) bool {
	fv := &d.folding
	for i, m := range fv.marks {
		if snap.LineAt(min(m.head, snap.Len())) == line {
			fv.marks = slices.Delete(fv.marks, i, i+1)
			fv.changed()
			return true
		}
	}
	return false
}

// folded reports whether line is the line that shows of a fold.
func (d *editor) folded(snap ui.TextSnapshot, line int) bool {
	for _, m := range d.folding.marks {
		if snap.LineAt(min(m.head, snap.Len())) == line {
			return true
		}
	}
	return false
}

// hasFolds reports whether an editor of the document folded lines.
func (doc *document) hasFolds() bool {
	for _, v := range doc.views {
		if len(v.folding.marks) > 0 {
			return true
		}
	}
	return false
}

// hiddenRuns returns the lines the editor hides, the outermost of the
// folds within each other, none in Live Preview.
func (a *app) hiddenRuns(d *editor, snap ui.TextSnapshot) []hiddenRun {
	fv := &d.folding
	if len(fv.marks) == 0 {
		return nil
	}
	live := 0
	if a.livePreview(d) {
		live = 1
	}
	if fv.runsVer == snap.Version() && fv.runsGen == fv.gen && fv.runsLive == live && fv.runs != nil {
		return fv.runs
	}
	fv.runs = fv.runs[:0]
	if live == 0 {
		n := snap.Len()
		for _, m := range fv.marks {
			from, to := snap.LineAt(min(m.head, n))+1, snap.LineAt(min(m.end, n))
			if to < from {
				continue
			}
			if k := len(fv.runs) - 1; k >= 0 && from <= fv.runs[k].to {
				fv.runs[k].to = max(fv.runs[k].to, to)
				continue
			}
			fv.runs = append(fv.runs, hiddenRun{from, to})
		}
	}
	if fv.runs == nil {
		fv.runs = []hiddenRun{}
	}
	fv.runsVer, fv.runsGen, fv.runsLive = snap.Version(), fv.gen, live
	return fv.runs
}

// runAt returns the run of runs that hides line.
func runAt(runs []hiddenRun, line int) (hiddenRun, bool) {
	k := sort.Search(len(runs), func(i int) bool { return runs[i].to >= line })
	if k < len(runs) && runs[k].from <= line {
		return runs[k], true
	}
	return hiddenRun{}, false
}

// revealAt opens the folds that hide the line of offset at.
func (d *editor) revealAt(snap ui.TextSnapshot, at int) bool {
	fv := &d.folding
	if len(fv.marks) == 0 {
		return false
	}
	n := snap.Len()
	line := snap.LineAt(min(max(at, 0), n))
	keep := fv.marks[:0]
	for _, m := range fv.marks {
		if snap.LineAt(min(m.head, n)) < line && line <= snap.LineAt(min(m.end, n)) {
			continue
		}
		keep = append(keep, m)
	}
	if len(keep) == len(fv.marks) {
		return false
	}
	fv.marks = keep
	fv.changed()
	return true
}

// snapOut moves a caret a key moved into hidden lines out of them, past
// them the way it went: forward to the line after them, back to the end
// of the line before.
func snapOut(snap ui.TextSnapshot, r hiddenRun, caret, was int) int {
	if caret >= was && r.to+1 < snap.LineCount() {
		s, _ := snap.LineRange(r.to + 1)
		return s
	}
	_, e := snap.LineRange(r.from - 1)
	return e
}

// foldsFollow moves the folds of the document's editors with edits made in
// the text before now, which leave an offset where moved says: a fold an
// edit reaches into opens.
func (doc *document) foldsFollow(edits []textEdit, moved func(int) int, now ui.TextSnapshot) {
	n := now.Len()
	for _, v := range doc.views {
		fv := &v.folding
		if len(fv.marks) == 0 {
			continue
		}
		keep := fv.marks[:0]
		dropped := false
		for _, m := range fv.marks {
			if slices.ContainsFunc(edits, func(e textEdit) bool { return foldTouched(m, e) }) {
				dropped = true
				continue
			}
			h := now.LineAt(min(moved(m.head), n))
			e := now.LineAt(min(moved(m.end), n))
			if e <= h {
				dropped = true
				continue
			}
			_, m.head = now.LineRange(h)
			_, m.end = now.LineRange(e)
			if k := len(keep) - 1; k >= 0 && keep[k].head == m.head {
				dropped = true
				continue
			}
			keep = append(keep, m)
		}
		fv.marks = keep
		if dropped {
			fv.changed()
		}
	}
}

// foldTouched reports whether edit e reaches into the lines fold m hides:
// it takes away a rune of them or the newline before them, or puts text
// among them or a line break before them.
func foldTouched(m foldMark, e textEdit) bool {
	if e.end > e.start {
		return e.start <= m.end && e.end > m.head
	}
	return e.start > m.head && e.start <= m.end || e.start == m.head && strings.Contains(e.text, "\n")
}

// foldLines returns the lines that show of the editor's folds, as a
// session keeps them.
func (d *editor) foldLines() []int {
	if d.folding.restore != nil {
		return d.folding.restore
	}
	snap := d.buf.Snapshot()
	var out []int
	for _, m := range d.folding.marks {
		out = append(out, snap.LineAt(min(m.head, snap.Len())))
	}
	return out
}

// settleFolds has the text area measure, for a little while each frame,
// the lines whose height it may not know since they were folded or
// opened: those out of view, which it does not lay out itself.
func (a *app) settleFolds(c *ui.Context, d *editor, runs []hiddenRun) {
	fv := &d.folding
	if !fv.settle || a.livePreview(d) {
		return
	}
	l := &d.lines
	if _, _, _, ok := l.Caret(0); !ok {
		// Not laid out yet: the frame after.
		c.AnimationFrame()
		return
	}
	n := l.Count()
	start := time.Now()
	for k := 0; fv.settleAt < n; fv.settleAt++ {
		i := fv.settleAt
		if k++; k%64 == 0 && time.Since(start) > time.Millisecond {
			c.AnimationFrame()
			return
		}
		_, hidden := runAt(runs, i)
		if h := l.Top(i+1) - l.Top(i); hidden != (h == 0) {
			l.Rows(i)
		}
	}
	fv.settle = false
}

// The commands.

// foldCmd folds the innermost run of lines the caret is in that is not
// folded.
func (a *app) foldCmd(d *editor) {
	if a.livePreview(d) {
		return
	}
	snap := d.buf.Snapshot()
	rs := a.foldRangesNow(d.document)
	line := snap.LineAt(min(d.sel[1], snap.Len()))
	for k := len(rs) - 1; k >= 0; k-- {
		r := rs[k]
		if r.Start <= line && line <= r.End && !d.folded(snap, r.Start) {
			d.addFold(snap, r)
			a.foldTurned(d, r.Start)
			a.caretOutOfFolds(d, snap)
			return
		}
	}
}

// unfoldCmd opens the fold of the caret's line, or the innermost the
// caret is in.
func (a *app) unfoldCmd(d *editor) {
	snap := d.buf.Snapshot()
	line := snap.LineAt(min(d.sel[1], snap.Len()))
	if d.removeFold(snap, line) {
		a.foldTurned(d, line)
		return
	}
	rs := a.foldRangesNow(d.document)
	for k := len(rs) - 1; k >= 0; k-- {
		if r := rs[k]; r.Start <= line && line <= r.End && d.removeFold(snap, r.Start) {
			a.foldTurned(d, r.Start)
			return
		}
	}
}

// toggleFoldCmd opens the fold of the caret's line, or folds the run it is
// in.
func (a *app) toggleFoldCmd(d *editor) {
	snap := d.buf.Snapshot()
	if d.folded(snap, snap.LineAt(min(d.sel[1], snap.Len()))) {
		a.unfoldCmd(d)
	} else {
		a.foldCmd(d)
	}
}

// foldAllCmd folds every run of lines.
func (a *app) foldAllCmd(d *editor) {
	if a.livePreview(d) {
		return
	}
	snap := d.buf.Snapshot()
	for _, r := range a.foldRangesNow(d.document) {
		d.addFold(snap, r)
	}
	a.caretOutOfFolds(d, snap)
}

// unfoldAllCmd opens every fold.
func (a *app) unfoldAllCmd(d *editor) {
	if len(d.folding.marks) > 0 {
		d.folding.marks = d.folding.marks[:0]
		d.folding.changed()
	}
}

// toggleFoldAt folds or opens the run of lines that starts at line, as a
// click on its chevron does, the caret moving out of what it hides.
func (a *app) toggleFoldAt(d *editor, line int) bool {
	snap := d.buf.Snapshot()
	if d.removeFold(snap, line) {
		a.foldTurned(d, line)
		return true
	}
	r, ok := rangeAt(a.foldRanges(d.document), line)
	if !ok || r.End >= snap.LineCount() {
		return false
	}
	d.addFold(snap, r)
	a.foldTurned(d, line)
	a.caretOutOfFolds(d, snap)
	return true
}

// foldTurned has the chevron of line turn.
func (a *app) foldTurned(d *editor, line int) {
	d.folding.turnLine, d.folding.turnAt = line, time.Now()
	a.invalidate()
}

// caretOutOfFolds puts the caret, where a fold hides it, at the end of the
// line that shows of the outermost.
func (a *app) caretOutOfFolds(d *editor, snap ui.TextSnapshot) {
	runs := a.hiddenRuns(d, snap)
	at := d.sel[1]
	if d.pending != nil {
		at = d.pending[1]
	}
	if r, ok := runAt(runs, snap.LineAt(min(at, snap.Len()))); ok {
		_, e := snap.LineRange(r.from - 1)
		d.selectRange(e, e, false)
	}
}

// foldChevron returns how far the chevron of line has turned, 1 pointing
// down for open, and whether it is turning.
func foldChevron(d *editor, line int, folded bool, now time.Time, reduce bool) (float32, bool) {
	k := float32(1)
	since := now.Sub(d.folding.turnAt)
	turning := line == d.folding.turnLine && since >= 0 && since < motionHover && !reduce
	if turning {
		k = ui.EaseOut(float32(since) / float32(motionHover))
	}
	if folded {
		k = 1 - k
	}
	return k, turning
}

// foldClick folds or opens a run of lines by a click at (x, y) in the
// text area of an editor: on a chevron in the gutter, or on the mark
// after a folded line.
func (a *app) foldClick(d *editor, x, y float32) bool {
	if a.livePreview(d) || d.gutterW <= 0 {
		return false
	}
	for _, p := range d.folding.pills {
		if x >= p.r.X && x <= p.r.X+p.r.W && y >= p.r.Y && y <= p.r.Y+p.r.H {
			line := p.line
			d.cmds = append(d.cmds, func(d *editor) { a.toggleFoldAt(d, line) })
			return true
		}
	}
	gx := x - d.hscroll.X
	if gx < d.gutterW-14-foldColW || gx >= d.gutterW-14 || y < float32(len(d.sticky))*a.font.lineH {
		return false
	}
	snap := d.buf.Snapshot()
	line := a.geometry(d, snap.LineCount()).at(y + d.vscroll.Y - editorPadY)
	if _, ok := rangeAt(a.foldRanges(d.document), line); !ok && !d.folded(snap, line) {
		return false
	}
	d.cmds = append(d.cmds, func(d *editor) { a.toggleFoldAt(d, line) })
	return true
}

// paintFoldChevrons paints the chevrons of the lines from to to that start
// a run that folds, in the gutter's column at x: always of those folded,
// pointing right, and of the others while the pointer is over the gutter.
func (a *app) paintFoldChevrons(pt *ui.Painter, d *editor, snap ui.TextSnapshot, g geometry, ranges []fold.Range, from, to int, x float32, lineY func(int) float32) {
	p, lineH := a.pal, a.font.lineH
	cs := min(lineH, foldColW)
	n := snap.Len()
	draw := func(line int, folded bool) {
		if g.hidden(line) {
			return
		}
		turn, turning := foldChevron(d, line, folded, pt.Now(), d.folding.reduce)
		if turning {
			pt.AnimationFrame()
		}
		col := p.gutter
		if folded {
			col = p.muted
		}
		drawChevron(pt, ui.Rect{X: x, Y: lineY(line) + (lineH-cs)/2, W: cs, H: cs}, turn, col)
	}
	folded := map[int]bool{}
	for _, m := range d.folding.marks {
		if line := snap.LineAt(min(m.head, n)); line >= from && line <= to {
			folded[line] = true
			draw(line, true)
		}
	}
	if !d.folding.hover && pt.Now().Sub(d.folding.turnAt) >= motionHover {
		return
	}
	k := sort.Search(len(ranges), func(i int) bool { return ranges[i].Start >= from })
	for ; k < len(ranges) && ranges[k].Start <= to; k++ {
		line := ranges[k].Start
		if folded[line] || ranges[k].End >= snap.LineCount() {
			continue
		}
		// Out of the gutter, the chevron of the line just opened turns
		// before it goes.
		if !d.folding.hover {
			if _, turning := foldChevron(d, line, false, pt.Now(), d.folding.reduce); !turning {
				continue
			}
		}
		draw(line, false)
	}
}

// foldPillText is what the mark after a folded line says.
const foldPillText = "⋯"

// paintFoldPills paints a mark after each folded line from from to to, and
// keeps where, in the area, for a click to open it.
func (a *app) paintFoldPills(pt *ui.Painter, d *editor, snap ui.TextSnapshot, g geometry, from, to int, r ui.Rect, place func(r ui.Rect, i int, x0, x1 float32, fn func(y, x0, x1 float32))) {
	fv := &d.folding
	fv.pills = fv.pills[:0]
	if len(fv.marks) == 0 {
		return
	}
	p, f := a.pal, a.font
	n := snap.Len()
	span := ui.Span{Text: foldPillText, Font: f.family, Size: f.size, Color: p.muted}
	tw, th := pt.MeasureText(0, span)
	w := tw + f.charW
	for _, m := range fv.marks {
		line := snap.LineAt(min(m.head, n))
		if line < from || line > to || g.hidden(line) {
			continue
		}
		s, e := snap.LineRange(line)
		x0 := f.width(snap.Slice(s, min(e, s+maxBlameAfter))) + f.charW/2
		done := false
		place(r, line, x0, x0+w, func(y, x0, x1 float32) {
			if done {
				return
			}
			done = true
			box := ui.Rect{X: x0, Y: y + 2, W: w, H: f.lineH - 4}
			pt.Fill(box, p.muted.Alpha(0.16), 4)
			pt.RichText(x0+(w-tw)/2, y+(f.lineH-th)/2, 0, span)
			fv.pills = append(fv.pills, foldPill{r: ui.Rect{X: box.X - r.X, Y: box.Y - r.Y, W: box.W, H: box.H}, line: line})
		})
	}
}

// builtinChords are the commands Mayoi gives keys of more than one stroke
// in an editor, as VS Code does, after those of keybindings.json.
var builtinChords = map[string]string{
	"edit.foldAll":    "cmd+k cmd+0",
	"edit.unfoldAll":  "cmd+k cmd+j",
	"edit.toggleFold": "cmd+k cmd+l",
}

// chordKeys returns the strokes of a command's own chord on the system,
// nil for none.
func chordKeys(id string) []keymap.Stroke {
	s, ok := builtinChords[id]
	if !ok {
		return nil
	}
	if !macKeys() {
		s = strings.ReplaceAll(s, "cmd+", "ctrl+")
	}
	keys, err := keymap.ParseKeys(s)
	if err != nil {
		return nil
	}
	return keys
}

// builtinChord returns the command whose own chord seq is, unless the
// file binds or takes away its keys, or reports whether seq starts one.
func (a *app) builtinChord(seq []keymap.Stroke) (id string, prefix bool) {
	for cmd := range builtinChords {
		names := commandNames(cmd)
		if a.keys.Removes(names...) || a.keys.Last(names...) != nil {
			continue
		}
		keys := chordKeys(cmd)
		if len(keys) < len(seq) || !slices.Equal(keys[:len(seq)], seq) {
			continue
		}
		if len(keys) == len(seq) {
			return cmd, false
		}
		prefix = true
	}
	return "", prefix
}

// foldInContext folds the files of the diff shown, or opens them, and
// elsewhere the lines of the editor the caret is in.
func (a *app) foldInContext(folded bool) {
	if t := a.activeTab(); t != nil && t.diff != nil {
		a.foldDiffFiles(folded)
		return
	}
	if folded {
		a.editActive(a.foldCmd)()
	} else {
		a.editActive(a.unfoldCmd)()
	}
}
