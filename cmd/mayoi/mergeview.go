package main

import (
	"bytes"
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// mergeTextX is where the text of a pane of a merge starts, past the
// rail of its conflicts.
const mergeTextX = diffRailW + 10

// regionState is how a conflict stands in the result.
type regionState uint8

const (
	regionUndecided regionState = iota // the base's lines stand
	regionLeft                         // ours' lines
	regionRight                        // theirs' lines
	regionBoth                         // the two sides' lines are the same here
	regionEdited                       // lines of its own
	regionIgnored                      // the base's lines stand, on purpose
)

// mergeView is the three-way merge of a file in conflict: its base and
// the two sides read only, and the result in the middle, which starts as
// the base and which the user edits, takes the sides into, and saves to
// resolve the file.
type mergeView struct {
	path  string
	entry git.Entry
	op    git.Op // the command stopped for the conflict, "" without

	// sides are who ours and theirs are, as the command stopped for the
	// conflict names them; loaded once the versions and the conflicts are
	// read.
	ours, theirs git.MergeSide
	loaded       bool
	loading      bool
	err          string

	// The versions the view compares. A side that has no file, as one
	// that deleted it, has no lines.
	base, oursT, theirsT string
	baseLines            []string
	oursLines            []string
	theirsLines          []string
	regions              []*mergeRegion
	binary               bool
	// crlf is how the result the file had on disk ends its lines.
	crlf bool

	// result is the document edited in the middle, the base at first, and
	// ed its editor here. The sides have buffers of their own, which no
	// one edits.
	doc       *document
	ed        *editor
	oursBuf   *ui.TextBuffer
	theirsBuf *ui.TextBuffer
	gen       uint64 // the result's version the regions were worked out at

	// chosen is the region the actions take, -1 for none, and current the
	// one gone to last, which the next conflict goes on from.
	chosen, current int

	// wrap is the tab's own choice to wrap the panes' lines, nil to
	// follow WordWrap; wrapGen is the settings' generation it was made
	// at, as an editor's and a diff's are.
	wrap    *bool
	wrapGen int

	// ignored are the regions a Ignore took back to the base's lines on
	// purpose, by their base range: baseSame alone cannot tell a
	// deliberate ignore from a conflict nobody looked at.
	ignored map[baseRange]bool

	panes        [3]paneScroll
	viewW, viewH float32
	// lastY are where the panes were scrolled to in the last frame: the
	// one that moved since leads the others.
	lastY [3]float32

	// pairs are the hunks between each side and the result, as of the
	// result's version pairsVer, and own the hunks of each side against
	// the base, which the gutters between the panes show.
	pairs    [2][]linediff.Hunk
	pairsVer uint64
	pairsOK  bool
	own      [2][]sideHunk
	// bands are the bands of each side's gutter, as of bandsKey.
	bands    [2][]mergeBand
	bandsKey [2]mergeBandKey
	bandsOK  [2]bool
	// flash is the result's lines a button of the gutters just wrote,
	// which glow a moment.
	flash diffFlash
}

// paneScroll is what one of the three panes keeps as it shows: its
// scrolling, how wide its widest line was, and where it laid its lines
// out as they wrap.
type paneScroll struct {
	scroll, hscroll     ui.ScrollState
	viewH               float32
	widest              int
	wideVer             uint64
	lines               ui.TextLines
	laidVer             uint64  // the version the wrapping lines were laid out at
	laidW               float32 // the width they were laid out for
	laidFrom, laidTo    int     // the lines measured so far, sweeping outward
	sweeping            bool    // until every line stands measured at this width
	viewFirst, viewLast int     // the lines in view, as the last frame drew them
}

// oursLabel is what the pane of ours says it is.
func (v *mergeView) oursLabel() string {
	return locales.MergeOursPane(cmp.Or(v.ours.Name, "HEAD"))
}

// theirsLabel is what the pane of theirs says it is.
func (v *mergeView) theirsLabel() string {
	if v.theirs.Name == "" || v.theirs.Name == "Theirs" {
		return locales.MergeTheirsBare()
	}
	return locales.MergeTheirsPane(v.theirs.Name)
}

// resultLabel is what the pane of the result says it is: the result
// starts as the base of the conflict.
func (v *mergeView) resultLabel() string { return locales.MergeResultPane() }

// oursTip says who ours is, as the command stopped for the conflict
// names it.
func (v *mergeView) oursTip() string {
	tip := locales.MergeOursTip()
	if b := cmp.Or(v.ours.Name, "HEAD"); b != "HEAD" {
		tip = locales.MergeOursTipBranch(b)
	}
	if op := v.op.Title(); op != "" {
		tip += " · " + op
	}
	return tip
}

// theirsTip says who theirs is, as the command stopped for the conflict
// brings it in.
func (v *mergeView) theirsTip() string {
	tip := locales.MergeTheirsTip()
	if n := v.theirs.Name; n != "" && n != "Theirs" {
		tip = locales.MergeTheirsTipName(n)
	}
	if op := v.op.Title(); op != "" {
		tip = locales.MergeTheirsBroughtIn(tip, op)
	}
	return tip
}

// resultTip says what the result is.
func (v *mergeView) resultTip() string {
	return locales.MergeResultTip()
}

// mergeResolved reports whether the file is in conflict no more: staged
// as resolved, committed, or the command undone.
func (a *app) mergeResolved(v *mergeView) bool {
	e, ok := a.git.entries[v.path]
	return a.git.looked && a.git.status != nil && (!ok || !e.Conflicted())
}

// mergeRegion is a run of lines of the base that both sides of the merge
// changed: the lines each side has in their place, and where the region
// stands in the result, with which sides its lines are.
type mergeRegion struct {
	baseLo, baseHi     int // the lines of base, from lo up to hi
	oursLo, oursHi     int // where the side has the region's lines
	theirsLo, theirsHi int
	// atLo, atHi are where the region's lines are in the result, taken
	// which sides' they are, and baseSame whether they are the base's.
	atLo, atHi int
	takenL     bool
	takenR     bool
	baseSame   bool
}

// name is a region's ranges, as the tests read them.
func (r *mergeRegion) name() string {
	return "base " + ita(r.baseLo, r.baseHi) + " ours " + ita(r.oursLo, r.oursHi) +
		" theirs " + ita(r.theirsLo, r.theirsHi)
}

// ita is a range of line numbers, as "[lo,hi)".
func ita(lo, hi int) string {
	return "[" + strconv.Itoa(lo) + "," + strconv.Itoa(hi) + ")"
}

// mergeRegions returns the conflicts of a three-way merge of base: the
// runs of its lines that both sides changed, hunks that touch in the base
// being one, as Git merges them. A hunk of one side alone is no conflict:
// taking those into the result is another ticket's work.
func mergeRegions(base, ours, theirs []string) []*mergeRegion {
	os, ts := hunksOf(base, ours), hunksOf(base, theirs)
	var rs []*mergeRegion
	// A run gathers the hunks of both sides that overlap or touch: Git
	// conflicts changes with no line of the base between.
	oi, ti := 0, 0
	lo, hi := 0, 0
	in, ol, tr := false, false, false
	flush := func() {
		if !in || !ol || !tr {
			return // a run of one side alone is no conflict
		}
		rs = append(rs, &mergeRegion{baseLo: lo, baseHi: hi, atLo: -1, atHi: -1})
	}
	for oi < len(os) || ti < len(ts) {
		var h sideHunk
		one := false
		switch {
		case ti >= len(ts):
			h, one = os[oi], true
			oi++
		case oi >= len(os):
			h = ts[ti]
			ti++
		case os[oi].alo <= ts[ti].alo:
			h, one = os[oi], true
			oi++
		default:
			h = ts[ti]
			ti++
		}
		if !in || h.alo > hi {
			flush()
			in, lo, hi, ol, tr = true, h.alo, h.ahi, one, !one
			continue
		}
		hi = max(hi, h.ahi)
		if one {
			ol = true
		} else {
			tr = true
		}
	}
	flush()
	// Where each side has the lines of a region: the hunks of its own in
	// the region grew it, and the hunks it has below the region pushed it
	// down.
	for _, r := range rs {
		r.oursLo, r.oursHi = sideRange(os, r.baseLo, r.baseHi)
		r.theirsLo, r.theirsHi = sideRange(ts, r.baseLo, r.baseHi)
	}
	return rs
}

// sideHunk is a hunk of one side against the base: the lines of the base
// from alo up to ahi by those of the side from blo up to bhi.
type sideHunk struct{ alo, ahi, blo, bhi int }

// hunksOf returns the hunks of one side of a merge against the base.
func hunksOf(base, side []string) []sideHunk {
	hs := linediff.Hunks(base, side)
	out := make([]sideHunk, len(hs))
	for i, h := range hs {
		out[i] = sideHunk{h.A, h.A + h.ALen, h.B, h.B + h.BLen}
	}
	return out
}

// sideRange returns where a side has the lines [lo, hi) of the base: from
// lo pushed down by the growth of the hunks the side has before it, up to
// hi grown by the hunks of its own it has in the region. An insertion at
// the region's start is one of its lines, not a push.
func sideRange(hs []sideHunk, lo, hi int) (int, int) {
	down, grow := 0, 0
	for _, h := range hs {
		if h.ahi > hi {
			break // the hunks after the region move nothing of it
		}
		g := h.bhi - h.ahi
		if h.ahi < lo || h.ahi == lo && h.alo < h.ahi {
			down += g
		}
		grow += g
	}
	return lo + down, hi + grow
}

// shifted is how far the hunks before a line of the base push it down in
// the result.
func shifted(hs []sideHunk, line int) int {
	off := 0
	for _, h := range hs {
		if h.ahi > line {
			break
		}
		off += h.bhi - h.ahi
	}
	return off
}

// sync takes where the regions' lines are in the result, and which
// sides' they are, from the result's lines: a region stands where the
// diff of the base and the result has its lines, and is taken left or
// right when those are the side's lines for it. The sides' states follow
// the text alone, so that undoing or redoing an edit of the result
// brings them back with it.
func (v *mergeView) sync(result []string) {
	hs := hunksOf(v.baseLines, result)
	for _, r := range v.regions {
		r.takenL, r.takenR = false, false
		// The hunk that holds the region's whole range, when the result
		// changed it.
		at := -1
		for k, h := range hs {
			if h.alo <= r.baseLo && h.ahi >= r.baseHi {
				at = k
				break
			}
		}
		lo, hi := -1, -1
		if at >= 0 {
			h := hs[at]
			lo, hi = h.blo, h.bhi
			// The hunk may hold more than the region, as an edit beside
			// it: the region's lines are where a side's, or the base's,
			// stand in the hunk.
			if h.alo < r.baseLo || h.ahi > r.baseHi {
				if k, n := v.findRegion(result, r, h.blo, h.bhi); n >= 0 {
					lo, hi = k, n
				}
			}
		} else {
			// The result has the base's lines where the region is: the
			// hunks before it say how far down they pushed it.
			off := shifted(hs, r.baseLo)
			lo, hi = r.baseLo+off, r.baseHi+off
		}
		r.atLo, r.atHi = lo, hi
		if lo < 0 || hi < lo || hi > len(result) {
			continue
		}
		content := result[lo:hi]
		r.baseSame = slices.Equal(content, clipLines(v.baseLines, r.baseLo, r.baseHi))
		if slices.Equal(content, clipLines(v.oursLines, r.oursLo, r.oursHi)) {
			r.takenL = true
		}
		if slices.Equal(content, clipLines(v.theirsLines, r.theirsLo, r.theirsHi)) {
			r.takenR = true
		}
	}
}

// findRegion looks for a region's lines within a run of the result, as
// one of its sides has them, or as the base has, and returns where they
// are, -1 when none of the three is there whole.
func (v *mergeView) findRegion(result []string, r *mergeRegion, lo, hi int) (int, int) {
	for _, want := range [][]string{
		clipLines(v.oursLines, r.oursLo, r.oursHi),
		clipLines(v.theirsLines, r.theirsLo, r.theirsHi),
		clipLines(v.baseLines, r.baseLo, r.baseHi),
	} {
		if k := indexSeq(result, lo, hi, want); k >= 0 {
			return k, k + len(want)
		}
	}
	return -1, -1
}

// indexSeq finds seq within result[lo:hi], -1 when it is not there.
func indexSeq(result []string, lo, hi int, seq []string) int {
	n := len(seq)
	for k := lo; k+n <= hi; k++ {
		if slices.Equal(result[k:k+n], seq) {
			return k
		}
	}
	return -1
}

// clipLines returns the lines lo up to hi of lines, kept in bounds.
func clipLines(lines []string, lo, hi int) []string {
	lo = min(max(lo, 0), len(lines))
	hi = min(max(hi, lo), len(lines))
	return lines[lo:hi]
}

// baseRange names a region by its range of the base, which no two
// regions share.
type baseRange struct{ lo, hi int }

// stateOf is how a region stands in the result now.
func (v *mergeView) stateOf(r *mergeRegion) regionState {
	switch {
	case r.takenL && r.takenR:
		return regionBoth
	case r.takenL:
		return regionLeft
	case r.takenR:
		return regionRight
	case !r.baseSame:
		return regionEdited
	case v.ignored[baseRange{r.baseLo, r.baseHi}]:
		return regionIgnored
	}
	return regionUndecided
}

// unresolved counts the regions whose lines the result still has from
// the base, neither side taken.
func (v *mergeView) unresolved() int {
	n := 0
	for _, r := range v.regions {
		if v.stateOf(r) == regionUndecided {
			n++
		}
	}
	return n
}

// regionAt returns the region i, nil when there is none.
func (v *mergeView) regionAt(i int) *mergeRegion {
	if i < 0 || i >= len(v.regions) {
		return nil
	}
	return v.regions[i]
}

// regionLines returns the lines a pane shows for a region: ours' first,
// the result's, theirs' last, as the panes stand.
func (v *mergeView) regionLines(r *mergeRegion, pane int) (int, int) {
	switch pane {
	case 0:
		return r.oursLo, r.oursHi
	case 1:
		return r.atLo, r.atHi
	}
	return r.theirsLo, r.theirsHi
}

// resultLines returns the lines of the result as it is now.
func (v *mergeView) resultLines() []string {
	return splitLines(v.doc.buf.Snapshot().String(), true)
}

// syncRegions works the regions out for the result's text as it is now.
func (v *mergeView) syncRegions() {
	if v.doc == nil {
		return
	}
	v.gen = v.doc.buf.Version()
	v.sync(v.resultLines())
}

// name is what the tab says: the file's name.
func (v *mergeView) name() string { return filepath.Base(v.path) }

// stop lets go of the document's highlighter, as the tab closes.
func (v *mergeView) stop() {
	if v.doc != nil {
		v.ed.release()
		if len(v.doc.views) == 0 {
			v.doc.hl.Close()
		}
	}
}

// openMerge shows the three-way merge of a file in conflict in the
// column that has the keyboard, or the file itself when it is in
// conflict no more.
func (a *app) openMerge(path string, focus bool) {
	a.openMergeIn(a.column(), a.column().active+1, path, focus)
}

// openMergeIn shows the three-way merge of a file in conflict in a tab
// of a column, or the file itself when it is in conflict no more.
func (a *app) openMergeIn(col *column, at int, path string, focus bool) {
	if t := a.mergeTab(path); t != nil {
		if c, i := a.locate(t); c != nil {
			a.keepTab(t)
			a.activate(c, i, focus)
		}
		return
	}
	e, ok := a.git.entries[path]
	if !ok || !e.Conflicted() {
		a.openPath(path, focus)
		return
	}
	var op git.Op
	if st := a.git.status; st != nil {
		op = st.Op
	}
	v := &mergeView{path: path, entry: e, op: op, chosen: -1, current: -1}
	a.addTab(col, at, &colTab{merge: v})
	a.activate(col, at, focus)
	a.rememberTabs()
	a.refreshMerge(v)
}

// mergeTab returns the tab that shows the merge of a file, nil when none
// does.
func (a *app) mergeTab(path string) *colTab {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.merge != nil && t.merge.path == path {
				return t
			}
		}
	}
	return nil
}

// refreshMerge reads the three versions of a conflict and who its sides
// are, in the background, and makes the result of the base once they are.
func (a *app) refreshMerge(v *mergeView) {
	if v.loaded || v.loading {
		return
	}
	r := a.git.repo
	if r == nil {
		return
	}
	v.loading = true
	path, op := v.path, v.op
	bg.Go(func() {
		defer recoverCrash()
		base, ours, theirs, err := r.Stages(path)
		oursSide, theirsSide := r.MergeSides(op)
		data, readErr := vfs.ReadFile(path)
		a.post(func() {
			v.loading = false
			if a.git.repo != r {
				return
			}
			v.loaded = true
			if err != nil {
				v.err = errorLine(err)
				a.invalidate()
				return
			}
			v.ours, v.theirs = oursSide, theirsSide
			v.base, v.oursT, v.theirsT = base.Text, ours.Text, theirs.Text
			v.baseLines = splitLines(v.base, base.Ok)
			v.oursLines = splitLines(v.oursT, ours.Ok)
			v.theirsLines = splitLines(v.theirsT, theirs.Ok)
			v.oursBuf = ui.NewTextBuffer(v.oursT)
			v.theirsBuf = ui.NewTextBuffer(v.theirsT)
			v.binary = isBinary([]byte(v.base)) || isBinary([]byte(v.oursT)) || isBinary([]byte(v.theirsT))
			// A binary file has no lines to merge, nor a result to edit:
			// the view only says so.
			if !v.binary {
				v.regions = mergeRegions(v.baseLines, v.oursLines, v.theirsLines)
				a.makeMergeResult(v, data, readErr)
			}
			a.invalidate()
		})
	})
}

// makeMergeResult makes the document the middle pane edits: the base of
// the conflict, or what the file has on disk when the work tree holds a
// result of the merge saved before, without the markers of a conflict.
func (a *app) makeMergeResult(v *mergeView, data []byte, diskErr error) {
	text := v.base
	fresh := true
	if diskErr == nil && !isBinary(data) {
		work := string(data)
		crlf := bytes.Contains(data, []byte("\r\n"))
		if crlf {
			work = strings.ReplaceAll(work, "\r\n", "\n")
		}
		if !hasConflictMarkers(work) {
			text = work
			v.crlf = crlf
			fresh = false
		}
	}
	if i := a.docIndex(v.path); i >= 0 {
		// The file open already, as the raw text of the conflict: it
		// becomes the result, as a step of undo.
		v.doc = a.docs[i]
		v.doc.takeText([]byte(text))
	} else {
		a.nextID++
		v.doc = newDocument(a.nextID, v.path, text, highlight.New(highlight.Lexer(v.path), a.invalidate))
		v.doc.crlf, v.doc.diskCRLF = v.crlf, v.crlf
		a.docs = append(a.docs, v.doc)
		if a.tree != nil {
			a.tree.Watch(v.path)
		}
	}
	v.ed = newEditor(v.doc)
	v.syncRegions()
	// A result that starts from the base takes the changes that no
	// conflict holds at once, when the settings have it: the conflicts
	// stand, for the hand to decide.
	if fresh && a.settings.MergeAutoApply {
		a.applyNonConflicts(v, applyAll)
	}
	// The first conflict stands chosen, for its actions to be at hand.
	if v.chosen < 0 && len(v.regions) > 0 {
		v.chosen, v.current = 0, 0
	}
}

// hasConflictMarkers reports whether text has the marker that starts a
// conflict's block at the start of a line.
func hasConflictMarkers(text string) bool {
	for at := 0; at < len(text); {
		end := strings.IndexByte(text[at:], '\n')
		line := text[at:]
		if end >= 0 {
			line = text[at : at+end]
		}
		if strings.HasPrefix(line, "<<<<<<<") {
			return true
		}
		if end < 0 {
			break
		}
		at += end + 1
	}
	return false
}

// takeMergeSide puts the side's lines of a region in the result, as one
// step of undo.
func (a *app) takeMergeSide(v *mergeView, i int, left bool) {
	r := v.regionAt(i)
	if r == nil {
		return
	}
	lines, lo, hi := v.oursLines, r.oursLo, r.oursHi
	if !left {
		lines, lo, hi = v.theirsLines, r.theirsLo, r.theirsHi
	}
	a.replaceMergeRegion(v, r, clipLines(lines, lo, hi))
}

// dropMergeSide puts the base's lines of a region in the result, which
// takes the side's out, as one step of undo.
func (a *app) dropMergeSide(v *mergeView, i int, left bool) {
	r := v.regionAt(i)
	if r == nil {
		return
	}
	lines, lo, hi := v.baseLines, r.baseLo, r.baseHi
	if left && r.takenR {
		lines, lo, hi = v.theirsLines, r.theirsLo, r.theirsHi
	} else if !left && r.takenL {
		lines, lo, hi = v.oursLines, r.oursLo, r.oursHi
	}
	a.replaceMergeRegion(v, r, clipLines(lines, lo, hi))
	if v.ignored == nil {
		v.ignored = map[baseRange]bool{}
	}
	v.ignored[baseRange{r.baseLo, r.baseHi}] = true
}

// replaceMergeRegion puts lines in a region's place in the result, as
// one step of undo, and works the regions out again.
func (a *app) replaceMergeRegion(v *mergeView, r *mergeRegion, lines []string) {
	if r.atLo < 0 || v.ed == nil {
		return
	}
	v.replaceLines(r.atLo, r.atHi, lines)
}

// replaceLines puts lines in the place of the result's lines lo up to
// hi, as one step of undo.
func (v *mergeView) replaceLines(lo, hi int, lines []string) {
	snap := v.doc.buf.Snapshot()
	e, ok := mergeTextEdit(snap, lo, hi, lines)
	if !ok {
		return
	}
	v.ed.edit(v.ed.sel, e)
	v.syncRegions()
}

func mergeTextEdit(snap ui.TextSnapshot, lo, hi int, lines []string) (textEdit, bool) {
	n := snap.LineCount()
	if lo < 0 || lo > n || hi < lo || hi > n {
		return textEdit{}, false
	}
	s, _ := snap.LineRange(lo)
	end := snap.Len()
	if hi < n {
		end, _ = snap.LineRange(hi)
	}
	text := strings.Join(lines, "\n")
	if len(lines) > 0 && hi < n {
		text += "\n"
	}
	if s == end && text == "" {
		return textEdit{}, false
	}
	return textEdit{s, end, text}, true
}

// resolveUsingMerge puts one side's lines in the place of every region,
// which accepts the side of the conflict and ignores the other, as one
// step of undo.
func (a *app) resolveUsingMerge(v *mergeView, left bool) {
	if v.doc == nil || v.ed == nil {
		return
	}
	snap := v.doc.buf.Snapshot()
	var edits []textEdit
	last := -1
	for _, r := range v.regions {
		if r.atLo < 0 || r.atLo < last {
			continue // not found in the result, or over the edit before
		}
		lines, lo, hi := v.oursLines, r.oursLo, r.oursHi
		if !left {
			lines, lo, hi = v.theirsLines, r.theirsLo, r.theirsHi
		}
		lines = clipLines(lines, lo, hi)
		if e, ok := mergeTextEdit(snap, r.atLo, r.atHi, lines); ok {
			edits = append(edits, e)
		}
		last = r.atHi
	}
	if len(edits) == 0 {
		return
	}
	v.ed.edit(v.ed.sel, edits...)
	v.syncRegions()
}

// applySide is the sides whose changes that no conflict holds a batch
// application takes.
type applySide uint8

const (
	applyAll   applySide = iota // both sides'
	applyLeft                   // ours' alone
	applyRight                  // theirs' alone
)

// notHeld returns the hunks of a side that no conflict of the merge
// holds: the changes a batch application may take without a hand's
// judgment. A hunk a conflict holds touches its lines of the base.
func notHeld(regions []*mergeRegion, hs []sideHunk) []sideHunk {
	var out []sideHunk
	for _, h := range hs {
		held := false
		for _, r := range regions {
			if h.alo <= r.baseHi && h.ahi >= r.baseLo {
				held = true
				break
			}
		}
		if !held {
			out = append(out, h)
		}
	}
	return out
}

// locateBase finds where the base's lines [alo,ahi) stand in result, as
// the base has them or as want has, which a side took them for: from the
// hunk of the result that holds the range, or where the hunks before it
// push it down. -1 when the result has changed them beyond finding, so
// that an application cannot write over lines it does not know.
func (v *mergeView) locateBase(result []string, hs []sideHunk, alo, ahi int, want []string) (int, int) {
	at := -1
	for k, h := range hs {
		if h.alo <= alo && h.ahi >= ahi {
			at = k
			break
		}
	}
	base := clipLines(v.baseLines, alo, ahi)
	lo, hi := -1, -1
	if at >= 0 {
		h := hs[at]
		lo, hi = h.blo, h.bhi
		if h.alo < alo || h.ahi > ahi {
			if k := indexSeq(result, lo, hi, want); k >= 0 {
				lo, hi = k, k+len(want)
			} else if k := indexSeq(result, lo, hi, base); k >= 0 {
				lo, hi = k, k+len(base)
			} else {
				return -1, -1
			}
		}
	} else {
		off := shifted(hs, alo)
		lo, hi = alo+off, ahi+off
	}
	if lo < 0 || hi < lo || hi > len(result) {
		return -1, -1
	}
	content := clipLines(result, lo, hi)
	if slices.Equal(content, want) || slices.Equal(content, base) {
		return lo, hi
	}
	return -1, -1
}

// applyNonConflicts puts the changes of a side that no conflict of the
// merge holds in the result, where the base still has them, as one step
// of undo. The conflicts stand: the batch takes no line the hand must
// judge, and passes a change it cannot find the base's lines for, as one
// the result has changed beyond finding.
func (a *app) applyNonConflicts(v *mergeView, side applySide) {
	if v.doc == nil || v.ed == nil || v.binary {
		return
	}
	result := v.resultLines()
	hs := hunksOf(v.baseLines, result)
	type held struct {
		h    sideHunk
		left bool
	}
	var puts []held
	if side != applyRight {
		for _, h := range notHeld(v.regions, hunksOf(v.baseLines, v.oursLines)) {
			puts = append(puts, held{h, true})
		}
	}
	if side != applyLeft {
		for _, h := range notHeld(v.regions, hunksOf(v.baseLines, v.theirsLines)) {
			puts = append(puts, held{h, false})
		}
	}
	slices.SortFunc(puts, func(x, y held) int { return x.h.alo - y.h.alo })
	snap := v.doc.buf.Snapshot()
	var edits []textEdit
	for _, put := range puts {
		lines := v.oursLines
		if !put.left {
			lines = v.theirsLines
		}
		want := clipLines(lines, put.h.blo, put.h.bhi)
		k, m := v.locateBase(result, hs, put.h.alo, put.h.ahi, want)
		if k < 0 || slices.Equal(clipLines(result, k, m), want) {
			continue // not found, or the side's lines stand there already
		}
		if e, ok := mergeTextEdit(snap, k, m, want); ok {
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 {
		return
	}
	v.ed.edit(v.ed.sel, edits...)
	v.syncRegions()
}

// setMergeAutoApply has a result that starts from the base take the
// changes that no conflict holds as it opens, or not, as the settings
// keep it.
func (a *app) setMergeAutoApply(on bool) {
	if a.settings.MergeAutoApply == on {
		return
	}
	a.settings.MergeAutoApply = on
	a.settings.Changed()
	a.invalidate()
}

// resetMerge puts the base's text in the result, which takes back what
// every region took, as one step of undo.
func (a *app) resetMerge(v *mergeView) {
	if v.doc == nil || v.ed == nil {
		return
	}
	snap := v.doc.buf.Snapshot()
	if snap.String() != v.base {
		v.ed.edit(v.ed.sel, textEdit{0, snap.Len(), v.base})
	}
	v.syncRegions()
}

// saveMerge saves the result in the file, also when the result stands as
// it started, for the file on disk may still have the conflict's markers,
// which the result never shows.
func (a *app) saveMerge(v *mergeView) {
	if v.doc == nil {
		return
	}
	if v.doc.dirty() {
		a.saveDoc(v.doc, nil)
		return
	}
	doc, path := v.doc, v.path
	bg.Go(func() {
		data, err := vfs.ReadFile(path)
		differs := err != nil || doc.buf.String() != strings.ReplaceAll(string(data), "\r\n", "\n")
		a.post(func() {
			if v.doc != doc || !differs {
				return
			}
			a.saveDoc(doc, nil)
		})
	})
}

// markMergeResolved saves the result of a merge view and stages its file,
// which ends its conflict, and says so. The merge view and the list of the
// files in conflict both take a file as resolved this way; done runs once
// the file stands staged.
func (a *app) markMergeResolved(v *mergeView, done func()) {
	if v.doc == nil {
		return
	}
	// A conflict left as the base has neither side's lines: staging it
	// drops both sides' changes there, so it is asked first.
	if n := v.unresolved(); n > 0 {
		a.confirm = &confirm{title: locales.MergeUnresolvedTitle(v.name()), ok: locales.MergeMarkResolved(),
			message: locales.MergeUnresolvedDetail(n), run: func() { a.stageMergeResult(v, done) }}
		a.invalidate()
		return
	}
	a.stageMergeResult(v, done)
}

// stageMergeResult saves the result of a merge view and stages its file.
func (a *app) stageMergeResult(v *mergeView, done func()) {
	path := v.path
	a.saveDoc(v.doc, func() {
		a.gitDo("", func(r *git.Repo) error { return r.Stage(path) }, func() {
			a.noteMergeResolved(path)
			a.tell(note{message: locales.MergeMarkedResolved(filepath.Base(path))})
			if done != nil {
				done()
			}
		})
	})
}

// completeMerge saves the result and stages the file, which ends its
// conflict: the command stopped for it goes on once every file is.
func (a *app) completeMerge(v *mergeView) {
	a.markMergeResolved(v, nil)
}

// restoreMerge puts a file back in conflict, once asked, that the
// command stopped for has staged as resolved.
func (a *app) restoreMerge(v *mergeView) {
	a.revertMergeConflict(v.path)
}

// revertMergeConflict puts a file back in conflict, once asked: the
// result saved goes, and the two sides of the conflict come back, as the
// command stopped for them has them. A merge view of the file starts its
// result over at the base.
func (a *app) revertMergeConflict(path string) {
	a.confirm = &confirm{title: locales.MergeRestoreTitle(filepath.Base(path)), ok: locales.MergeRestoreOk(), danger: true,
		message: locales.MergeRestoreDetail(),
		run: func() {
			a.gitDo("", func(r *git.Repo) error { return r.RestoreConflict(path) }, func() {
				if t := a.mergeTab(path); t != nil {
					a.resetMerge(t.merge)
					if a.settings.MergeAutoApply {
						a.applyNonConflicts(t.merge, applyAll)
					}
				}
			})
		}}
}

// stepMergeRegion goes to the next region of a merge, or the previous
// one for a step of -1, round the ends.
func (a *app) stepMergeRegion(v *mergeView, step int) {
	if len(v.regions) == 0 {
		return
	}
	at := max(v.chosen, v.current, 0)
	v.chosen = ((at+step)%len(v.regions) + len(v.regions)) % len(v.regions)
	v.current = v.chosen
	a.revealMergeRegion(v, v.chosen)
	a.invalidate()
}

// revealMergeRegion scrolls every pane so a region shows, a third of the
// way down: the result glides there, and the sides follow it as they
// scroll together.
func (a *app) revealMergeRegion(v *mergeView, i int) {
	r := v.regionAt(i)
	if r == nil {
		return
	}
	at := [3]int{r.oursLo, max(r.atLo, 0), r.theirsLo}
	room := v.viewH - a.font.lineH*4
	if room <= 0 {
		return
	}
	y := max(0, editorPadY+a.mergeGeometry(v, 1).top(at[1])-room/3)
	if d := y - v.panes[1].scroll.Y; d != 0 && max(d, -d) < 3*v.viewH {
		v.panes[1].scroll.ScrollTo(v.panes[1].scroll.X, y, ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.EaseOut})
		return
	}
	for k, line := range at {
		v.panes[k].scroll.Y = max(0, editorPadY+a.mergeGeometry(v, k).top(line)-room/3)
	}
	v.lastY = [3]float32{v.panes[0].scroll.Y, v.panes[1].scroll.Y, v.panes[2].scroll.Y}
}

// chooseMergeLine takes the region a click in a pane went to, by the
// lines of that pane.
func (a *app) chooseMergeLine(v *mergeView, pane int, y float32) {
	line := a.mergeGeometry(v, pane).at(y + v.panes[pane].scroll.Y - editorPadY)
	for i, r := range v.regions {
		lo, hi := v.regionLines(r, pane)
		if line >= lo && line < max(hi, lo+1) {
			v.chosen, v.current = i, i
			a.invalidate()
			return
		}
	}
}
