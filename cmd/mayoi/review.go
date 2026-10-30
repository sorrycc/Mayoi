package main

import (
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// reviewNote is a comment on lines of a file, as a review of the changes
// an agent made, to paste back to it with where the lines are and what
// they were.
type reviewNote struct {
	path string
	// context is a deliberately collected snippet, whose comment is optional.
	context bool
	// commit is the commit whose diff the note is on, "" for the changes
	// of the work tree; staged is set for one on the changes staged, whose
	// new side is the index.
	commit string
	staged bool
	// oldSide is set when the lines are of the old side, the note being on
	// lines removed only. from and to are the lines of the side, to after
	// the last.
	oldSide  bool
	from, to int
	// diff is set when the snippet is of a diff, each line marked as added,
	// removed or the same. lines are the lines of the side from from to to
	// as they were, which the note finds again as the file changes, and
	// outdated is set when they are no longer in it.
	diff     bool
	snippet  []string
	lines    []string
	text     string
	outdated bool
	// version is the version of the open document the note was last
	// found in, unfound for none yet.
	version uint64
}

// unfound is the version of a note not yet found in an open document.
const unfound = ^uint64(0)

// review is the notes of a project, until they are sent.
type review struct {
	notes []*reviewNote
	// draft is the note being written, a new one or one of notes, nil for
	// none: text is its comment as typed, and ed the editor whose box
	// shows it, nil for a diff's.
	draft *reviewNote
	text  string
	ed    *editor
	// gen counts the changes to the notes, which the diffs lay out their
	// rows again for.
	gen int
	// typed is the frame the draft's box last had the keyboard in.
	typed int
	// ctx measures text as the window lays it out, and measured is the
	// heights of the comments measured.
	ctx      *ui.Context
	measured map[noteMeasure]float32
}

// noteAt returns note k of a diff's rows: of the notes, or the draft, a
// new one, after them.
func (r *review) noteAt(k int) *reviewNote {
	switch {
	case k >= 0 && k < len(r.notes):
		return r.notes[k]
	case k == len(r.notes):
		return r.draft
	}
	return nil
}

// work reports whether a note is on lines of the work tree: of an editor,
// or of the changes not staged.
func (n *reviewNote) work() bool { return n.commit == "" && !n.staged }

// inDiff reports whether a note is on lines of file f of a diff.
func (n *reviewNote) inDiff(v *diffView, f *diffFile) bool {
	if n.path != f.path {
		return false
	}
	if v.commit != nil {
		return n.commit == v.commit.key()
	}
	return n.commit == "" && n.staged == (f.base == diffStaged)
}

// same reports whether two notes are on the same lines.
func (n *reviewNote) same(o *reviewNote) bool {
	return n.path == o.path && n.commit == o.commit && n.staged == o.staged && n.oldSide == o.oldSide &&
		n.from == o.from && n.to == o.to
}

// relocate finds lines in text, at from or as near it as they are, and
// reports whether it found them.
func relocate(lines, text []string, from int) (int, bool) {
	return relocateIn(lines, len(text), func(i int) string { return text[i] }, from)
}

// relocateIn finds lines in a text of count lines, line(i) its line i, at
// from or as near it as they are, and reports whether it found them. It
// reads only the lines it compares, so a text need not be split to search
// it.
func relocateIn(lines []string, count int, line func(int) string, from int) (int, bool) {
	n := len(lines)
	at := func(i int) bool {
		if i < 0 || i+n > count {
			return false
		}
		for k, l := range lines {
			if line(i+k) != l {
				return false
			}
		}
		return true
	}
	if n == 0 || at(from) {
		return from, n > 0
	}
	for d := 1; d <= max(from, count-from); d++ {
		if at(from - d) {
			return from - d, true
		}
		if at(from + d) {
			return from + d, true
		}
	}
	return from, false
}

// follow finds a note's lines again in text, the lines of its side now,
// or marks it outdated.
func (n *reviewNote) follow(text []string) {
	n.found(relocate(n.lines, text, n.from))
}

// found moves a note to its lines found at line at, or marks it outdated
// when they were not.
func (n *reviewNote) found(at int, ok bool) {
	n.outdated = !ok
	if ok {
		n.from, n.to = at, at+len(n.lines)
	}
}

// where names a note's lines, as its card says.
func (n *reviewNote) where() string {
	s := locales.ReviewLine(n.from + 1)
	if n.to-n.from > 1 {
		s = locales.ReviewLines(n.from+1, n.to)
	}
	if n.oldSide {
		s = locales.ReviewRemoved(s)
	}
	return s
}

// ref is where a note's lines are, as an agent reads it: the path and the
// lines, and what they are of.
func (n *reviewNote) ref(rel func(string) string) string {
	at := fmt.Sprint(n.from + 1)
	if n.to-n.from > 1 {
		at += fmt.Sprintf("-%d", n.to)
	}
	s := rel(n.path) + ":" + at
	var tags []string
	if n.oldSide {
		tags = append(tags, "removed lines, numbered as before the change")
	}
	pr, prFrom, prTo, isPR := parsePRKey(n.commit)
	switch {
	case isPR:
		tags = append(tags, fmt.Sprintf("in pull request #%d, the changes from commit %s to %s", pr, git.ShortHash(prFrom), git.ShortHash(prTo)))
	case strings.Contains(n.commit, ".."):
		from, to, _ := strings.Cut(n.commit, "..")
		tags = append(tags, "in the changes from commit "+git.ShortHash(from)+" to "+git.ShortHash(to))
	case n.commit != "":
		tags = append(tags, "in commit "+git.ShortHash(n.commit))
	case n.staged:
		tags = append(tags, "staged")
	}
	if n.outdated {
		tags = append(tags, "outdated: the lines have changed since")
	}
	if len(tags) > 0 {
		s += " (" + strings.Join(tags, "; ") + ")"
	}
	return s
}

// fence returns a fence of backticks longer than any run of them in
// lines.
func fence(lines []string) string {
	most := 2
	for _, l := range lines {
		run := 0
		for _, r := range l {
			if r != '`' {
				run = 0
				continue
			}
			run++
			most = max(most, run)
		}
	}
	return strings.Repeat("`", most+1)
}

// formatReview writes notes as a message to an agent: where each is, its
// lines in a block of code and its comment, numbered when there are more
// than one. rel spells a path.
func formatReview(notes []*reviewNote, rel func(string) string) string {
	var b strings.Builder
	for i, n := range notes {
		if i > 0 {
			b.WriteString("\n")
		}
		if len(notes) > 1 {
			fmt.Fprintf(&b, "%d. ", i+1)
		}
		b.WriteString(n.ref(rel) + "\n")
		f := fence(n.snippet)
		lang := "diff"
		if !n.diff {
			lang = strings.TrimPrefix(filepath.Ext(n.path), ".")
		}
		b.WriteString(f + lang + "\n")
		for _, l := range n.snippet {
			b.WriteString(l + "\n")
		}
		b.WriteString(f + "\n")
		if t := strings.TrimSpace(n.text); t != "" {
			b.WriteString(t + "\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// commentRows returns the rows a comment shows in, cols columns wide, as
// its lines wrap.
func commentRows(text string, cols int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		shown, _ := expandTabs(line)
		at := 0
		for _, k := range wrapAt(shown, cols) {
			out = append(out, shown[at:k])
			at = k
		}
		out = append(out, shown[at:])
	}
	return out
}

// The parts of a note's card: the size of its comment's text, its
// header, the room around its inside, the stripe down its edge, the room
// around the text of the box a comment is typed in, its buttons under
// the box, and the room between a card and the rows around it in a diff.
const (
	noteFontSize = 13
	noteHeadH    = 24
	notePadX     = 12
	notePadY     = 8
	noteStripe   = 3
	noteFieldPad = 6
	noteFootH    = 34
	noteInset    = 4
)

// noteSpan is a note's comment as its card shows it.
func (a *app) noteSpan(text string) ui.Span {
	return ui.Span{Text: text, Size: noteFontSize, Color: a.pal.text}
}

// noteTextW is how wide the comment is in a card w wide.
func noteTextW(w float32) float32 { return max(w-noteStripe-2*notePadX, 60) }

// noteTextH is how tall a comment is, w wide, as its lines wrap: one
// line at least. The heights are kept, as each frame asks again.
func (a *app) noteTextH(text string, w float32) float32 {
	if text == "" {
		text = " "
	}
	r := &a.review
	k := noteMeasure{text, w}
	if h, ok := r.measured[k]; ok {
		return h
	}
	var h float32
	if r.ctx != nil {
		_, h = r.ctx.MeasureText(w, a.noteSpan(text))
	}
	if h <= 0 {
		// Measured only while the window builds: until then, as a guess.
		return float32(len(commentRows(text, max(int(w/(noteFontSize*0.55)), 10)))) * noteFontSize * 1.3
	}
	if r.measured == nil || len(r.measured) > 512 {
		r.measured = map[noteMeasure]float32{}
	}
	r.measured[k] = h
	return h
}

// noteMeasure is a comment measured at a width.
type noteMeasure struct {
	text string
	w    float32
}

// noteFieldH is how tall the box a comment is typed in is, its text w
// wide: as tall as the text, from 3 lines to 10, and its room.
func (a *app) noteFieldH(text string, w float32) float32 {
	one := a.noteTextH(" ", w)
	return clamp(a.noteTextH(text, w-2*(noteFieldPad+2)), 3*one, 10*one) + 2*noteFieldPad + 2
}

// noteHeight is how tall a note's card is, its comment w wide: its
// header and its comment, or, while it is written, the box it is typed
// in and its buttons.
func (a *app) noteHeight(n *reviewNote, editing bool, text string, w float32) float32 {
	if editing {
		return 2*notePadY + noteHeadH + a.noteFieldH(text, w) + noteFootH
	}
	h := 2*notePadY + noteHeadH + a.noteTextH(strings.TrimSpace(n.text), w) + 2
	if k := len(quoted(n)); k > 0 {
		h += a.quoteH(k) + noteQuoteGap
	}
	return h
}

// The lines an outdated note quotes: at most maxQuoted, and the room
// around them and under them.
const (
	maxQuoted    = 4
	noteQuotePad = 5
	noteQuoteGap = 8
)

// quoted returns the lines a note's card quotes: for one outdated, the
// lines it was written on, as they no longer are.
func quoted(n *reviewNote) []string {
	if !n.outdated {
		return nil
	}
	if len(n.snippet) > maxQuoted {
		return append(slices.Clip(n.snippet[:maxQuoted-1]), "…")
	}
	return n.snippet
}

// quoteH is how tall k lines quoted are.
func (a *app) quoteH(k int) float32 { return float32(k)*a.quoteLineH() + 2*noteQuotePad }

// quoteLineH is how tall a line quoted is.
func (a *app) quoteLineH() float32 { return float32(math.Round(float64(a.font.lineH * 0.9))) }

// noteLines is how many lines of a diff a note's card takes, its comment
// w wide.
func (a *app) noteLines(n *reviewNote, editing bool, text string, w float32) int {
	h := a.noteHeight(n, editing, text, w) + 2*noteInset
	return max(int(math.Ceil(float64(h/a.font.lineH))), 2)
}

// selection returns the rows of a diff chosen, from lo to hi: those from
// the anchor to the cursor, or the cursor's, or the top row's; -1 for
// none.
func (v *diffView) selection() (lo, hi int) {
	c := v.focusRow()
	if c < 0 {
		return -1, -1
	}
	if v.anchor < 0 || v.anchor >= len(v.rows) || v.cursor < 0 {
		return c, c
	}
	return min(v.anchor, c), max(v.anchor, c)
}

// selected reports whether row i is of the rows chosen, more than one.
func (v *diffView) selected(i int) bool {
	if v.anchor < 0 || v.cursor < 0 || v.anchor == v.cursor {
		return false
	}
	return i >= min(v.anchor, v.cursor) && i <= max(v.anchor, v.cursor)
}

// extend moves the cursor of a diff by step rows, keeping where the rows
// chosen start.
func (v *diffView) extend(step int) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		v.setCursor(v.focusRow())
	}
	at := v.anchor
	if at < 0 {
		at = v.cursor
	}
	v.moveCursor(step)
	v.anchor = at
}

// excerpts returns notes, without comments, of the lines of rows lo to
// hi of a diff: one for each file they are of. A header of a run of
// changes stands for the lines under it.
func (v *diffView) excerpts(lo, hi int) []*reviewNote {
	var out []*reviewNote
	for i := max(lo, 0); i <= hi && i < len(v.rows); {
		file := v.rows[i].file
		olds, news := map[int]bool{}, map[int]bool{}
		add := func(r diffRow) {
			if r.kind != rowLine {
				return
			}
			if r.old >= 0 {
				olds[r.old] = true
			}
			if r.new >= 0 {
				news[r.new] = true
			}
		}
		for ; i <= hi && i < len(v.rows) && v.rows[i].file == file; i++ {
			if v.rows[i].kind == rowHunk {
				for k := i + 1; k < len(v.rows) && v.rows[k].file == file && (v.rows[k].kind == rowLine || v.rows[k].kind == rowComment); k++ {
					add(v.rows[k])
				}
			}
			add(v.rows[i])
		}
		if n := v.excerpt(v.files[file], olds, news); n != nil {
			out = append(out, n)
		}
	}
	return out
}

// excerpt returns a note, without a comment, of the lines of a file of a
// diff, of each side: its lines as a diff, in the order of the unified
// layout, with … where lines between them are left out.
func (v *diffView) excerpt(f *diffFile, olds, news map[int]bool) *reviewNote {
	if len(olds)+len(news) == 0 {
		return nil
	}
	n := &reviewNote{path: f.path, staged: f.base == diffStaged, diff: true, version: unfound}
	if v.commit != nil {
		n.commit, n.staged = v.commit.key(), false
	}
	last := -1
	for k, r := range f.unified {
		if !(r.old >= 0 && olds[r.old] || r.new >= 0 && news[r.new]) {
			continue
		}
		if last >= 0 && k > last+1 {
			n.snippet = append(n.snippet, "…")
		}
		last = k
		switch {
		case r.old >= 0 && r.new >= 0:
			n.snippet = append(n.snippet, " "+clipLine(f.new.lines[r.new]))
		case r.new >= 0:
			n.snippet = append(n.snippet, "+"+clipLine(f.new.lines[r.new]))
		default:
			n.snippet = append(n.snippet, "-"+clipLine(f.old.lines[r.old]))
		}
	}
	lines, set := f.new.lines, news
	if len(news) == 0 {
		lines, set, n.oldSide = f.old.lines, olds, true
	}
	n.from, n.to = len(lines), 0
	for l := range set {
		n.from, n.to = min(n.from, l), max(n.to, l+1)
	}
	if n.from >= n.to || n.to > len(lines) {
		return nil
	}
	n.lines = slices.Clone(lines[n.from:n.to])
	return n
}

// editorExcerpt returns a note, without a comment, of the lines of an
// editor's selection, or of its caret's line; nil for a file not saved.
func editorExcerpt(d *editor) *reviewNote {
	if d.path == "" {
		return nil
	}
	snap := d.buf.Snapshot()
	lo, hi := min(d.sel[0], d.sel[1], snap.Len()), min(max(d.sel[0], d.sel[1]), snap.Len())
	from, to := snap.LineAt(lo), snap.LineAt(hi)
	// A selection to the start of a line ends on the line before.
	if to > from {
		if s, _ := snap.LineRange(to); s == hi {
			to--
		}
	}
	n := &reviewNote{path: d.path, from: from, to: to + 1, version: d.buf.Version()}
	for i := from; i <= to; i++ {
		s, e := snap.LineRange(i)
		n.lines = append(n.lines, strings.TrimRight(snap.Slice(s, e), "\r\n"))
	}
	n.snippet = n.lines
	return n
}

// relPath spells a path as an agent in the project's folder reads it:
// from the folder, or whole for one out of it.
func (a *app) relPath(path string) string {
	if a.tree != nil {
		if rel := a.tree.Rel(path); rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel) {
			return rel
		}
	}
	return hostPath(path)
}

// followNote finds a note on the work tree again in its file as it is
// now: in its open document, or on disk. The diffs lay out their cards
// again when it is outdated, or no longer.
func (a *app) followNote(n *reviewNote) {
	if !n.work() {
		return
	}
	was := n.outdated
	defer func() {
		if n.outdated != was {
			a.review.gen++
		}
	}()
	if i := a.docIndex(n.path); i >= 0 {
		doc := a.docs[i]
		if doc.buf.Version() == n.version {
			return
		}
		n.version = doc.buf.Version()
		// Read in place, line by line, as it is searched: a keystroke in
		// a long file copies none of it.
		snap := doc.buf.Snapshot()
		line := func(i int) string { return strings.TrimSuffix(snap.Line(i), "\r") }
		n.found(relocateIn(n.lines, snap.LineCount(), line, n.from))
		return
	}
	if remote.Is(n.path) {
		return
	}
	text, _, err := readWork(n.path)
	if err != nil {
		n.outdated = true
		return
	}
	n.follow(strings.Split(text, "\n"))
}

// addReviewComment starts a comment on what is chosen: the lines of the
// diff shown, or of the editor's selection. On the lines of a note, it
// edits the note.
func (a *app) addReviewComment() {
	if t := a.activeTab(); t != nil && t.diff != nil {
		a.commentDiff(t.diff)
		return
	}
	d := a.textEditor()
	if d == nil {
		return
	}
	a.commentEditor(d)
}

func (a *app) commentEditor(d *editor) {
	a.keepDraft()
	n := editorExcerpt(d)
	if n == nil {
		a.tell(note{message: a.reviewLabel(locales.ReviewSaveFirst(), locales.ReviewSaveFirstItems())})
		return
	}
	for _, o := range a.review.notes {
		a.followNote(o)
		if o.work() && !o.oldSide && !o.outdated && o.path == n.path && o.from <= n.from && o.to >= n.to {
			a.editNote(o, d)
			return
		}
	}
	if a.settings.EditorAgentContext {
		n.context = true
	}
	a.startDraft(n, d)
}

// commentDiff starts a comment on the lines of a diff chosen, or edits
// the note chosen.
func (a *app) commentDiff(v *diffView) {
	lo, hi := v.selection()
	if lo < 0 {
		return
	}
	if c := v.cursor; c >= 0 && c < len(v.rows) && v.rows[c].kind == rowComment && lo == hi {
		if n := a.review.noteAt(v.rows[c].n); n != nil {
			a.editNote(n, nil)
		}
		return
	}
	ns := v.excerpts(lo, hi)
	if len(ns) == 0 {
		a.tell(note{message: locales.ReviewChooseLines()})
		return
	}
	// Of the file of the cursor, when the rows are of more than one.
	n := ns[len(ns)-1]
	if c := v.cursor; c >= 0 && c < len(v.rows) {
		for _, o := range ns {
			if o.path == v.files[v.rows[c].file].path {
				n = o
			}
		}
	}
	for _, o := range a.review.notes {
		if o.same(n) {
			a.editNote(o, nil)
			return
		}
	}
	v.anchor = -1
	a.startDraft(n, nil)
	a.revealDraft(v)
}

// revealDraft scrolls a diff to the card of the note being written.
func (a *app) revealDraft(v *diffView) {
	a.layoutRows(v, a.diffGeometry(v, v.split, v.viewW))
	for i, r := range v.rows {
		if r.kind == rowComment && a.review.noteAt(r.n) == a.review.draft {
			v.jumpRow(i, jumpReveal)
			return
		}
	}
}

// startDraft has a note written, in the box of editor ed, or of a diff
// for nil.
func (a *app) startDraft(n *reviewNote, ed *editor) {
	r := &a.review
	if r.draft == n && r.ed == ed {
		return
	}
	a.keepDraft()
	r.draft, r.text, r.ed = n, n.text, ed
	if ed != nil {
		ed.releaseReview = a.keepDraft
	}
	r.gen++
}

// editNote has a note written again.
func (a *app) editNote(n *reviewNote, ed *editor) {
	a.startDraft(n, ed)
	if t := a.activeTab(); ed == nil && t != nil && t.diff != nil {
		a.revealDraft(t.diff)
	}
}

// saveDraft keeps the comment typed for its note. An empty comment is
// allowed only for a snippet deliberately collected without one.
func (a *app) saveDraft() {
	if a.storeDraft() {
		a.focusColumn()
	}
}

// storeDraft commits a valid draft without changing the active pane's focus.
func (a *app) storeDraft() bool {
	r := &a.review
	n := r.draft
	text := strings.TrimSpace(r.text)
	if n == nil || text == "" && !n.context {
		return false
	}
	n.text = text
	if !slices.Contains(r.notes, n) {
		r.notes = append(r.notes, n)
	}
	a.finishDraft()
	a.reviewChanged()
	return true
}

// keepDraft finishes an interrupted draft without losing its text or snippet.
// Empty legacy comments have nothing to save, but still release their editor.
func (a *app) keepDraft() {
	if a.review.draft == nil {
		return
	}
	if !a.storeDraft() {
		a.finishDraft()
	}
}

// endDraft closes the box of the note being written, and gives the
// keyboard back to where it was.
func (a *app) endDraft() {
	a.finishDraft()
	a.focusColumn()
}

func (a *app) finishDraft() {
	r := &a.review
	if d := r.ed; d != nil {
		d.releaseReview = nil
		if d.reviewSource != nil {
			d.reviewSource = nil
			d.resync = true
		}
	}
	r.draft, r.text, r.ed = nil, "", nil
	r.gen++
}

// deleteNote drops a note, which the toast that says so puts back.
func (a *app) deleteNote(n *reviewNote) {
	r := &a.review
	at := slices.Index(r.notes, n)
	r.notes = slices.DeleteFunc(r.notes, func(o *reviewNote) bool { return o == n })
	if r.draft == n {
		a.endDraft()
	}
	r.gen++
	a.reviewChanged()
	if at < 0 {
		return
	}
	a.tell(note{message: locales.ReviewDeleted(n.ref(a.relPath)), label: locales.ButtonUndo(), action: func() {
		a.restoreNotes(map[int]*reviewNote{at: n})
	}})
}

// restoreNotes puts notes back where they were, each at its index.
func (a *app) restoreNotes(notes map[int]*reviewNote) {
	r := &a.review
	for _, at := range slices.Sorted(maps.Keys(notes)) {
		if n := notes[at]; !slices.Contains(r.notes, n) {
			r.notes = slices.Insert(r.notes, min(at, len(r.notes)), n)
		}
	}
	r.gen++
	a.reviewChanged()
}

// reviewText is the notes as a message to an agent, each found again in
// its file first.
func (a *app) reviewText() string {
	for _, n := range a.review.notes {
		a.followNote(n)
	}
	return formatReview(a.review.notes, a.relPath)
}

// copyReview puts the notes on the clipboard.
func (a *app) copyReview() {
	k := len(a.review.notes)
	if k == 0 {
		a.tell(note{message: a.reviewLabel(locales.ReviewNoneToCopy(), locales.ReviewNoneToCopyItems())})
		return
	}
	a.services.WriteClipboard(a.reviewText())
	a.tell(note{message: a.reviewLabel(locales.ReviewCopied(k), locales.ReviewCopiedItems(k))})
}

// sendReview pastes the notes into the terminal used last, as a message
// to the agent in it, without pressing Enter, and clears them. With no
// terminal, it copies them.
func (a *app) sendReview() {
	a.keepDraft()
	k := len(a.review.notes)
	if k == 0 {
		a.tell(note{message: a.reviewLabel(locales.ReviewNoneToSend(), locales.ReviewNoneToSendItems())})
		return
	}
	if !a.pasteToTerminal(a.reviewText() + "\n") {
		a.services.WriteClipboard(a.reviewText())
		a.tell(note{message: a.reviewLabel(locales.ReviewNoTerminal(k), locales.ReviewNoTerminalItems(k))})
		return
	}
	a.dropReview()
	a.tell(note{message: a.reviewLabel(locales.ReviewSent(k), locales.ReviewSentItems(k))})
}

// pasteToTerminal pastes text into the terminal used last, without
// pressing Enter, and shows it; false with no terminal open.
func (a *app) pasteToTerminal(text string) bool {
	s := a.reviewTerminal()
	if s == nil {
		return false
	}
	s.term.Paste(text)
	a.showTerminal(s)
	return true
}

// clearReview drops every note, which the toast that says so puts back.
func (a *app) clearReview() {
	a.keepDraft()
	gone := map[int]*reviewNote{}
	for i, n := range a.review.notes {
		gone[i] = n
	}
	a.dropReview()
	if len(gone) > 0 {
		a.tell(note{message: a.reviewLabel(locales.ReviewCleared(len(gone)), locales.ReviewClearedItems(len(gone))), label: locales.ButtonUndo(), action: func() { a.restoreNotes(gone) }})
	}
}

// dropReview drops every note.
func (a *app) dropReview() {
	if a.review.draft != nil {
		a.endDraft()
	}
	r := &a.review
	r.notes, r.draft, r.text, r.ed = nil, nil, "", nil
	r.gen++
	a.reviewChanged()
}

// reviewChanged keeps the notes with the window's session, for the next
// start to show them again.
func (a *app) reviewChanged() {
	if a.window == nil {
		return
	}
	a.window.Review = a.keptReview()
	a.settings.Changed()
}

// keptReview is the notes as the settings keep them.
func (a *app) keptReview() []settings.ReviewNote {
	var out []settings.ReviewNote
	for _, n := range a.review.notes {
		out = append(out, settings.ReviewNote{
			Context: n.context,
			Path:    n.path, Commit: n.commit, Staged: n.staged, OldSide: n.oldSide, From: n.from, To: n.to,
			Diff: n.diff, Snippet: n.snippet, Lines: n.lines, Text: n.text,
		})
	}
	return out
}

// restoreReview shows the notes the settings kept, in place of those
// shown; one whose lines make no sense is left out.
func (a *app) restoreReview(kept []settings.ReviewNote) {
	if a.review.draft != nil {
		a.endDraft()
	}
	r := &a.review
	r.notes, r.draft, r.text, r.ed = nil, nil, "", nil
	r.gen++
	for _, k := range kept {
		if k.Path == "" || k.From < 0 || k.To-k.From != len(k.Lines) || len(k.Lines) == 0 || strings.TrimSpace(k.Text) == "" && !k.Context {
			continue
		}
		if k.Context && (k.Diff || k.Commit != "" || k.Staged || k.OldSide || !slices.Equal(k.Snippet, k.Lines)) {
			continue
		}
		r.notes = append(r.notes, &reviewNote{
			context: k.Context,
			path:    k.Path, commit: k.Commit, staged: k.Staged, oldSide: k.OldSide, from: k.From, to: k.To,
			diff: k.Diff, snippet: k.Snippet, lines: k.Lines, text: k.Text, version: unfound,
		})
	}
}

// reviewTerminal is the terminal the notes go to: the one that had the
// keyboard last, or the panel's.
func (a *app) reviewTerminal() *session {
	if s := a.keyTerm; s != nil && slices.Contains(a.terms, s) {
		return s
	}
	if a.panelTerm != nil {
		return a.panelTerm
	}
	if len(a.terms) > 0 {
		return a.terms[len(a.terms)-1]
	}
	return nil
}

// showTerminal shows a terminal and gives it the keyboard: in its tab, or
// in the panel.
func (a *app) showTerminal(s *session) {
	if s.tab != nil {
		if col, i := a.locate(s.tab); i >= 0 {
			a.activate(col, i, true)
		}
		return
	}
	a.panelTerm = s
	a.window.PanelOpen, a.window.TerminalOpen = true, true
	a.settings.Changed()
	a.focusTerminal()
}

// copyExcerpt puts the lines of a diff chosen on the clipboard, with
// where they are, as a message to an agent.
func (a *app) copyExcerpt(v *diffView) {
	lo, hi := v.selection()
	ns := v.excerpts(lo, hi)
	if len(ns) == 0 {
		return
	}
	a.services.WriteClipboard(formatReview(ns, a.relPath))
	refs := make([]string, len(ns))
	for i, n := range ns {
		refs[i] = n.ref(a.relPath)
	}
	a.tell(note{message: locales.CopiedText(strings.Join(refs, ", "))})
}

// goToNote shows a note: the file at its lines, or the card of one on
// lines not in the file, in the changes, or those of its commit.
func (a *app) goToNote(n *reviewNote) {
	a.followNote(n)
	if n.work() && !n.oldSide {
		a.openFileAt(n.path, n.from, true)
		return
	}
	if n.commit != "" {
		root := a.diffRoot(nil)
		if r := a.repositoryForPath(n.path).git.repo; r != nil {
			root = r.Root
		}
		a.showCommit(git.Commit{Hash: n.commit}, true, root)
	} else {
		base := diffIndex
		if n.staged {
			base = diffStaged
		}
		a.showChanges(diffTarget{path: n.path, base: base}, true)
	}
	if t := a.activeTab(); t != nil && t.diff != nil {
		t.diff.revealNote = n
		a.review.gen++
	}
}

// stepNote goes to the next note, or for step -1 the one before, round
// to the first past the last: in a diff, the card after the row chosen;
// else the note after the caret's line, in its file and then the next
// file's, in the order of their paths.
func (a *app) stepNote(step int) {
	r := &a.review
	if len(r.notes) == 0 {
		a.tell(note{message: a.reviewLabel(locales.ReviewNone(), locales.ReviewNoneItems())})
		return
	}
	if t := a.activeTab(); t != nil && t.diff != nil && len(t.diff.cards) > 0 {
		v := t.diff
		i := stepIn(v.cards, v.cursor, step, func(a, b int) int { return a - b })
		v.anchor = -1
		v.setCursor(v.cards[i])
		v.glideRow(v.cards[i], jumpThird)
		v.pane.Focus()
		return
	}
	notes := a.notesInOrder()
	at := &reviewNote{from: -1}
	if d := a.textEditor(); d != nil && d.path != "" {
		snap := d.buf.Snapshot()
		at = &reviewNote{path: d.path, from: snap.LineAt(min(d.sel[1], snap.Len()))}
	}
	a.goToNote(notes[stepIn(notes, at, step, byPlace)])
}

// notesInOrder returns the notes, each found again in its file, in the
// order of their paths and lines.
func (a *app) notesInOrder() []*reviewNote {
	notes := slices.Clone(a.review.notes)
	for _, n := range notes {
		a.followNote(n)
	}
	slices.SortStableFunc(notes, byPlace)
	return notes
}

// byPlace orders notes by their paths, then their lines.
func byPlace(x, y *reviewNote) int {
	if c := strings.Compare(x.path, y.path); c != 0 {
		return c
	}
	return x.from - y.from
}

// stepIn returns the index of the item of sorted after at, or for step
// -1 the one before it, round past either end.
func stepIn[T any](sorted []T, at T, step int, cmp func(T, T) int) int {
	if step > 0 {
		for i, x := range sorted {
			if cmp(x, at) > 0 {
				return i
			}
		}
		return 0
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		if cmp(sorted[i], at) < 0 {
			return i
		}
	}
	return len(sorted) - 1
}

// reviewMenu lists the notes under the paths of their files, in the
// order of their lines, to go to each, and sends, copies or clears them.
func (a *app) reviewMenu(m *ui.Menu) {
	notes := a.notesInOrder()
	for i, n := range notes {
		if i == 0 || n.path != notes[i-1].path {
			if i > 0 {
				m.Separator()
			}
			m.Item(a.relPath(n.path)).Disabled(true)
		}
		label := n.where() + " — " + noteHint(n)
		if n.outdated {
			label += " (outdated)"
		}
		if !a.settings.EditorAgentContext {
			if m.Item(label).Chosen() {
				a.goToNote(n)
			}
			continue
		}
		m.Submenu(label, func(m *ui.Menu) {
			if m.Item(locales.AgentContextGoTo()).Chosen() {
				a.goToNote(n)
			}
			if m.Item(locales.ReviewEditComment()).Chosen() {
				a.goToNote(n)
				var ed *editor
				if n.work() && !n.oldSide {
					ed = a.textEditor()
					if ed == nil || ed.path != n.path {
						return
					}
				}
				a.editNote(n, ed)
			}
			if m.Item(locales.AgentContextRemove()).Chosen() {
				a.deleteNote(n)
			}
		})
	}
	m.Separator()
	if m.Item(a.reviewLabel(locales.CmdReviewSend(), locales.CmdReviewSendItems())).Chosen() {
		a.sendReview()
	}
	if m.Item(a.reviewLabel(locales.CmdReviewCopy(), locales.CmdReviewCopyItems())).Chosen() {
		a.copyReview()
	}
	if m.Item(a.reviewLabel(locales.CmdReviewClear(), locales.CmdReviewClearItems())).Chosen() {
		a.clearReview()
	}
}

// noteButton is a small button of a note's card, of text, which reports
// whether it was clicked: the primary one filled with the accent, off
// while disabled.
func (a *app) noteButton(c *ui.Context, label string, primary, disabled bool) bool {
	p := a.pal
	b := ui.ButtonBase(c.Key("note:"+label)).Height(a.font.lineH-2).Padding(0, 10).Radius(5).AlignItems(ui.Center).Shrink(0).
		Label(label).Disabled(disabled)
	col := p.text
	switch {
	case disabled:
		b.Background(p.text.Alpha(0.07))
		col = p.faint
	case primary:
		col = p.fillText
		switch {
		case b.Pressed():
			b.Background(p.fillPressed)
		case b.Hovered():
			b.Background(p.fillHover)
		default:
			b.Background(p.fill)
		}
	case b.Pressed():
		b.Background(p.surfacePressed)
	case b.Hovered():
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() { ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(col).SingleLine() })
	return b.Clicked() && !disabled
}

// notePill is a word on a note's card in a pill of its color, as the
// header of a file of a diff says how it changed.
func notePill(c *ui.Context, word, tip string, col ui.Color) {
	ui.Row(c).Height(16).Padding(0, 6).Radius(8).AlignItems(ui.Center).Background(col.Alpha(0.14)).Shrink(0).Tooltip(tip).
		Children(func() { ui.Text(c, word).FontSize(10.5).FontWeight(600).TextColor(col).SingleLine() })
}

// noteColor is the color of a note's card: the accent, or for one whose
// lines have changed since, the color of a change.
func (a *app) noteColor(n *reviewNote) ui.Color {
	if n.outdated {
		return a.pal.modified
	}
	return a.pal.accent
}

// noteCard shows the inside of a note's card, its comment w wide: where
// its lines are and its comment, with buttons to edit or delete it.
// While the note is written in the box of editor ed, or of a diff for
// nil, it shows the box to type it in.
func (a *app) noteCard(c *ui.Context, n *reviewNote, ed *editor, w float32) {
	p := a.pal
	r := &a.review
	editing := r.draft == n && r.ed == ed
	kept := slices.Contains(r.notes, n)
	col := a.noteColor(n)
	// A stripe down its left edge, of its color.
	ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(noteStripe).Background(col)
	ui.Row(c).Height(noteHeadH).Gap(7).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Row(c).Size(20, 20).Radius(6).Justify(ui.Center).AlignItems(ui.Center).Background(col.Alpha(0.15)).Shrink(0).
			Children(func() { ui.Icon(c, iconComment).FontSize(11).TextColor(col) })
		ui.Text(c, n.where()).FontSize(12).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
		if editing && !kept {
			ui.Text(c, locales.ReviewNewComment()).FontSize(12).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
		}
		if n.outdated {
			notePill(c, locales.ReviewOutdated(), locales.ReviewOutdatedTip(), p.modifiedFg)
		}
		ui.Spacer(c)
		if editing {
			return
		}
		if a.smallButton(c, iconPencil, locales.ReviewEditComment()) {
			a.post(func() { a.editNote(n, ed) })
		}
		if a.smallButton(c, iconTrash, locales.ReviewDeleteComment()) {
			a.post(func() { a.deleteNote(n) })
		}
	})
	if !editing {
		ui.Box(c).Height(2).Shrink(0)
		if lines := quoted(n); len(lines) > 0 {
			a.noteQuote(c, n, lines, w)
		}
		ui.RichText(c, a.noteSpan(strings.TrimSpace(n.text))).Width(w).Shrink(0).Selectable()
		return
	}
	in := a.textKeys(ui.TextArea(c.Key("review-text"), &r.text)).AutoFocus().FontSize(noteFontSize).
		Width(w).Height(a.noteFieldH(r.text, w)).Shrink(0).Padding(noteFieldPad, noteFieldPad+2).
		Placeholder(locales.ReviewPlaceholder()).Label(locales.ReviewCommentLabel())
	if in.Focused() {
		r.typed = a.frame
	}
	empty := strings.TrimSpace(r.text) == "" && !n.context
	if in.Shortcut(ui.Cmd, ui.KeyEnter) {
		a.post(a.saveDraft)
	}
	if in.Shortcut(0, ui.KeyEscape) {
		a.post(a.endDraft)
	}
	ui.Row(c).Height(noteFootH).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
		if kept {
			b := ui.ButtonBase(c.Key("note:delete")).Height(24).Padding(0, 8).Gap(5).Radius(5).AlignItems(ui.Center).Shrink(0).
				Label(locales.ReviewDeleteComment())
			fg := p.muted
			if b.Hovered() {
				b.Background(p.removed.Alpha(0.12))
				fg = p.removed
			}
			b.Transition(hoverFade)
			b.Children(func() {
				ui.Icon(c, iconTrash).FontSize(12).TextColor(fg)
				ui.Text(c, locales.ButtonDelete()).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
			})
			if b.Clicked() {
				a.post(func() { a.deleteNote(n); a.focusColumn() })
			}
		}
		ui.Text(c, locales.ReviewSaveHint(keyText("CmdOrCtrl+Enter"))).FontSize(11).TextColor(p.faint).SingleLine().
			Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if a.noteButton(c, locales.ButtonCancel(), false, false) {
			a.post(a.endDraft)
		}
		label := locales.ReviewComment()
		if kept {
			label = locales.ButtonSave()
		} else if n.context && strings.TrimSpace(r.text) == "" {
			label = locales.AgentContextAdd()
		}
		if a.noteButton(c, label, true, empty) {
			a.post(a.saveDraft)
		}
	})
}

// noteQuote shows the lines an outdated note was written on, w wide,
// each colored as the diff had it.
func (a *app) noteQuote(c *ui.Context, n *reviewNote, lines []string, w float32) {
	p, f := a.pal, a.font
	lineH := a.quoteLineH()
	ui.Column(c).Width(w).Height(a.quoteH(len(lines))).Padding(noteQuotePad, 8).Radius(5).Shrink(0).Clip().
		Background(p.text.Alpha(0.045)).Margin(0, 0, noteQuoteGap, 0).Tooltip(locales.ReviewQuoteTip()).
		Children(func() {
			for _, l := range lines {
				col := p.muted
				if n.diff && l != "" {
					switch l[0] {
					case '+':
						col = p.added
					case '-':
						col = p.removed
					}
				}
				shown, _ := expandTabs(l)
				ui.Text(c, shown).Font(f.family).FontSize(f.size - 1).Height(lineH).SingleLine().TextColor(col).Shrink(0)
			}
		})
}

// typingNote reports whether the box of the note being written has the
// keyboard, whose keys a diff leaves to it.
func (a *app) typingNote() bool {
	return a.review.draft != nil && a.review.typed >= a.frame-1
}

// noteCardBox is the card of a note, at (x, y) w wide and h tall: raised
// a little off the lines around it.
func (a *app) noteCardBox(c *ui.Context, key string, x, y, w, h float32) ui.Element {
	p := a.pal
	box := ui.Column(c.Key(key)).Absolute().Left(x).Top(y).Width(w).Height(h).Radius(8).Clip().
		Background(p.surface).Border(1, p.border).Padding(0, notePadX, 0, notePadX+noteStripe).Justify(ui.Center)
	if p.dark {
		box.Shadow(0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.25))
	} else {
		box.Shadow(0, 1, 3, 0, ui.RGBA(20, 20, 40, 0.07))
	}
	return box
}

// noteGeometry is where the card of a note goes in a diff laid out as l:
// its left, from the view's, and its width. It spans the view, or side by
// side, the side of its lines.
func noteGeometry(l diffGeometry, split, old bool) (x, w float32) {
	x, right := float32(diffRailW+10), l.viewW-18
	if split {
		if old {
			right = l.half - 10
		} else {
			x = l.right() + diffRailW + 10
		}
	}
	return x, max(right-x, 160)
}

// noteRows adds the rows of the notes on file f of a diff laid out as l,
// number file, to the rows from start, which are of f: each under the
// last line it is on, or the gap that folds it.
func (a *app) noteRows(v *diffView, l diffGeometry, f *diffFile, file, start int) {
	r := &a.review
	shown := r.notes
	// The note being written in an editor's box shows there alone.
	if r.draft != nil && r.ed == nil && !slices.Contains(shown, r.draft) {
		shown = append(slices.Clip(shown), r.draft)
	}
	for k, n := range shown {
		if !n.inDiff(v, f) {
			continue
		}
		if f.loaded && !(r.draft == n && r.ed == nil) {
			side := f.new.lines
			if n.oldSide {
				side = f.old.lines
			}
			n.follow(side)
		}
		at, last := -1, n.to-1
		for i := start; i < len(v.rows); i++ {
			row := v.rows[i]
			line := row.new
			if n.oldSide {
				line = row.old
			}
			if (row.kind == rowLine || row.kind == rowGap) && line >= 0 && line <= last {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		for at+1 < len(v.rows) && v.rows[at+1].kind == rowComment {
			at++
		}
		_, w := noteGeometry(l, v.split, n.oldSide)
		h := a.noteLines(n, r.draft == n && r.ed == nil, r.text, noteTextW(w))
		v.rows = slices.Insert(v.rows, at+1, diffRow{kind: rowComment, file: file, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, n: k, h: h})
	}
}

// lineCommentSize is how wide and tall the button by a line of a diff is,
// which comments on it.
const lineCommentSize = 18

// lineCommentButton is the button by row i of a diff, at (x, y), which
// comments on its line, or on the rows chosen with it, and the room it
// takes.
func (a *app) lineCommentButton(c *ui.Context, v *diffView, i int, x, y float32) ui.Rect {
	a.reviewLineButton(c, x, y, locales.ReviewAddComment(), func() {
		if i >= len(v.rows) || v.rows[i].kind != rowLine {
			return
		}
		if !v.selected(i) {
			v.anchor = -1
			v.setCursor(i)
		}
		v.pane.Focus()
		a.commentDiff(v)
	})
	return ui.Rect{X: x, Y: y, W: lineCommentSize, H: lineCommentSize}
}

// reviewLineButton is shared by source files and diffs.
func (a *app) reviewLineButton(c *ui.Context, x, y float32, label string, action func()) {
	p := a.pal
	ui.Box(c.Key("line-comment")).Absolute().Left(x).Top(y).Children(func() {
		b := ui.ButtonBase(c).Size(lineCommentSize, lineCommentSize).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
			Label(label).Tooltip(label)
		switch {
		case b.Pressed():
			b.Background(p.fillPressed)
		case b.Hovered():
			b.Background(p.fillHover)
		default:
			b.Background(p.fill)
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() { ui.Icon(c, iconPlus).FontSize(12).TextColor(p.fillText) })
		if !b.Clicked() {
			return
		}
		a.post(action)
	})
}

// diffNotes returns the notes that show in a diff: those kept, and the
// one being written in it.
func (a *app) diffNotes(v *diffView) []*reviewNote {
	r := &a.review
	if r.draft != nil && r.ed == nil && !slices.Contains(r.notes, r.draft) {
		return append(slices.Clip(r.notes), r.draft)
	}
	return r.notes
}

// notedLine reports whether a line row of a diff is of the lines of one
// of notes: its line of the old side, or of the new.
func notedLine(v *diffView, notes []*reviewNote, row diffRow, old bool) bool {
	line := row.new
	if old {
		line = row.old
	}
	if line < 0 || len(notes) == 0 {
		return false
	}
	f := v.files[row.file]
	for _, n := range notes {
		if n.oldSide == old && !n.outdated && line >= n.from && line < n.to && n.inDiff(v, f) {
			return true
		}
	}
	return false
}

// notedLines returns the notes on lines of an editor's file, found again
// in its text as it changed, but for those no longer in it.
func (a *app) notedLines(d *editor) []*reviewNote {
	var out []*reviewNote
	for _, n := range a.review.notes {
		if n.work() && n.path == d.path && d.path != "" {
			a.followNote(n)
			if !n.outdated {
				out = append(out, n)
			}
		}
	}
	return out
}

// noteHint is what a note says after the last of its lines in an editor:
// its comment's first line, cut short.
func noteHint(n *reviewNote) string {
	s, _, more := strings.Cut(strings.TrimSpace(n.text), "\n")
	if s == "" && n.context {
		return locales.AgentContextSnippet()
	}
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 80 {
		s, more = string(r[:79]), true
	}
	if more {
		s += "…"
	}
	return s
}

// editorHint is a note's comment as it shows after the last of its
// lines, line, in an editor: from x, in the editor's text area, w wide.
type editorHint struct {
	n    *reviewNote
	line int
	x, w float32
	text string
}

// hintGap is the room between the icon of a hint and its text, in
// columns.
const hintGap = 0.75

// editorHints returns the hints of noted, the notes on an editor's lines,
// whose last lines are from first to last: all but the one being written
// in the editor's box.
func (a *app) editorHints(d *editor, snap ui.TextSnapshot, noted []*reviewNote, first, last int) []editorHint {
	f := a.font
	var out []editorHint
	for _, n := range noted {
		l := n.to - 1
		if l < first || l > last || a.review.draft == n && a.review.ed == d {
			continue
		}
		s, e := snap.LineRange(l)
		text := noteHint(n)
		x := d.gutterW + gutterGap + f.width(snap.Slice(s, min(e, s+maxBlameAfter))) + 4*f.charW
		w := f.size*0.85 + (hintGap+float32(len([]rune(text)))*0.92)*f.charW
		out = append(out, editorHint{n, l, x, w, text})
	}
	return out
}

// maxNoteBoxW is how wide the box a comment is typed in under lines of
// an editor grows.
const maxNoteBoxW = 760

// reviewBoxView is the box under the lines of an editor a note is being
// written on, and over the hints of its notes, buttons that edit them.
func (a *app) reviewBoxView(c *ui.Context, d *editor) {
	r := &a.review
	if len(r.notes) == 0 && r.draft == nil {
		return
	}
	snap := d.buf.Snapshot()
	count := snap.LineCount()
	g := a.geometry(d, count)
	view := d.hscroll.Bounds(c)
	if g.lines == nil {
		first, last := a.linesInView(g, d.vscroll.Y, view.H)
		for _, h := range a.editorHints(d, snap, a.notedLines(d), first, last) {
			y := editorPadY + g.top(h.line) - d.vscroll.Y
			x := h.x - d.hscroll.X
			if x < d.gutterW || y < 0 || y > view.H {
				continue
			}
			tip := locales.ReviewEditTip(noteHint(h.n))
			b := ui.ButtonBase(c.Key(fmt.Sprintf("hint:%p", h.n))).Absolute().Left(x-4).Top(y+2).Size(h.w+8, a.font.lineH-4).
				Radius(4).Label(locales.ReviewEditComment()).Tooltip(tip)
			if b.Hovered() {
				b.Background(a.pal.accent.Alpha(0.12))
			}
			if b.Clicked() {
				n := h.n
				a.post(func() { a.editNote(n, d) })
			}
		}
	}
	n := r.draft
	if n == nil || r.ed != d {
		return
	}
	left := d.gutterW - 4
	w := min(view.W-left-14, maxNoteBoxW)
	h := a.noteHeight(n, true, r.text, noteTextW(w))
	y := editorPadY + g.top(min(n.to, count)) - d.vscroll.Y
	// It stays in view, as its lines scroll away.
	y = clamp(y+2, 0, max(0, view.H-h))
	box := a.floating(c, a.noteCardBox(c, "review-box", left, y, w, h), false).Transition(popIn).Label(locales.ReviewCommentLabel())
	box.Children(func() { a.noteCard(c, n, d, noteTextW(w)) })
}

// reviewSendButton is the button of a diff's breadcrumbs that sends the
// notes to the terminal, with their count, and its menu the rest of
// what is done with them; none while there are none.
func (a *app) reviewSendButton(c *ui.Context) {
	k := len(a.review.notes)
	if k == 0 {
		return
	}
	p := a.pal
	tip := a.reviewLabel(locales.ReviewSendTip(k), locales.ReviewSendTipItems(k))
	b := ui.ButtonBase(c.Key("review-send")).Height(crumbsH-6).Padding(0, 8).Gap(5).Radius(5).Shrink(0).
		AlignItems(ui.Center).Label(tip).Tooltip(tip).Margin(0, 6, 0, 0)
	switch {
	case b.Pressed():
		b.Background(p.fillPressed)
	case b.Hovered():
		b.Background(p.fillHover)
	default:
		b.Background(p.fill)
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		ui.Icon(c, iconSend).FontSize(12).TextColor(p.fillText)
		ui.Text(c, a.reviewLabel(locales.ReviewSendButton(k), locales.ReviewSendButtonItems(k))).FontSize(11.5).FontWeight(600).TextColor(p.fillText).SingleLine()
	})
	if b.Clicked() {
		a.post(a.sendReview)
	}
}

// reviewStatus is the part of the status bar that counts the notes, with
// their menu.
func (a *app) reviewStatus(c *ui.Context, text func(string) ui.Element) {
	k := len(a.review.notes)
	if k == 0 {
		return
	}
	p := a.pal
	tip := a.reviewLabel(locales.ReviewStatusTip(k), locales.ReviewStatusTipItems(k))
	b := ui.ButtonBase(c.Key("status:review")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
		AlignItems(ui.Center).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconComment).FontSize(12).TextColor(p.statusFg)
		text(fmt.Sprint(k))
	})
	b.Menu(a.reviewMenu)
}

// reviewLabel keeps the ordinary review vocabulary when collection is off.
func (a *app) reviewLabel(comment, item string) string {
	if a.settings.EditorAgentContext {
		return item
	}
	return comment
}
