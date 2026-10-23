package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// diffBase is what a diff compares: which version of a file with which.
type diffBase int

const (
	diffHead   diffBase = iota // HEAD with the work tree
	diffIndex                  // the index with the work tree: the changes not staged
	diffStaged                 // HEAD with the index: the changes staged
	diffCommit                 // a commit with its first parent
	diffLocal                  // two local files, both writable
)

// sides names the two versions a diff compares.
func (b diffBase) sides() (old, new string) {
	switch b {
	case diffIndex:
		return locales.SideIndex(), locales.SideWorkingTree()
	case diffStaged:
		return "HEAD", locales.SideIndex()
	case diffLocal:
		return locales.DiffSideLeft(), locales.DiffSideRight()
	}
	return "HEAD", locales.SideWorkingTree()
}

// diffLayout is how a diff shows its sides: side by side when it is wide
// enough to, or always, or in one column.
type diffLayout int

const (
	layoutAuto diffLayout = iota
	layoutSplit
	layoutInline
)

// key is how the settings name the layout.
func (l diffLayout) key() string { return [...]string{"", "split", "inline"}[l] }

func parseDiffLayout(s string) diffLayout {
	switch s {
	case "split":
		return layoutSplit
	case "inline":
		return layoutInline
	}
	return layoutAuto
}

// diffCross is how a diff's change navigation crosses into another file
// at the first or last change of one: asking for one more step that way,
// going at once, or never leaving the file.
type diffCross uint8

const (
	crossAsk    diffCross = iota // say where it is, and cross on the next step
	crossAlways                  // cross into the next or previous file at once
	crossNever                   // stay in the file
)

func parseDiffCross(s string) diffCross {
	switch s {
	case "always":
		return crossAlways
	case "never":
		return crossNever
	}
	return crossAsk
}

// diffHighlight is how a diff shows what changed within a line: the
// words that changed, the characters within them, or nothing more than
// the lines changed. What it shows is only so: the changes a diff acts
// on are its hunks of lines, whatever it highlights.
type diffHighlight uint8

const (
	highlightWord diffHighlight = iota
	highlightLine
	highlightChar
)

// key is how the settings name the highlight.
func (h diffHighlight) key() string { return [...]string{"", "line", "char"}[h] }

func parseDiffHighlight(s string) diffHighlight {
	switch s {
	case "line":
		return highlightLine
	case "char":
		return highlightChar
	}
	return highlightWord
}

// diffHighlightOf is how the diffs show what changed within a line, as
// the settings say.
func (a *app) diffHighlightOf() diffHighlight { return parseDiffHighlight(a.settings.DiffHighlight) }

// diffWhitespace controls which whitespace differences participate in the
// line comparison. The original lines remain untouched for painting and
// applying actions; only their comparison keys are normalized.
type diffWhitespace uint8

const (
	whitespaceExact diffWhitespace = iota
	whitespaceEOL
	whitespaceAll
	whitespaceBlank
)

func (w diffWhitespace) key() string { return [...]string{"", "eol", "all", "blank"}[w] }

func parseDiffWhitespace(s string) diffWhitespace {
	switch s {
	case "eol":
		return whitespaceEOL
	case "all":
		return whitespaceAll
	case "blank":
		return whitespaceBlank
	}
	return whitespaceExact
}

func (a *app) diffWhitespaceOf() diffWhitespace {
	return parseDiffWhitespace(a.settings.DiffWhitespace)
}

func normalizeDiffLine(s string, mode diffWhitespace) string {
	switch mode {
	case whitespaceEOL:
		return strings.TrimRight(s, " \t")
	case whitespaceAll:
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\u00a0' {
				return -1
			}
			return r
		}, s)
	case whitespaceBlank:
		if strings.TrimSpace(s) == "" {
			return ""
		}
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\u00a0' {
				return -1
			}
			return r
		}, s)
	default:
		return s
	}
}

func compareDiffLines(lines []string, mode diffWhitespace) []string {
	if mode == whitespaceExact {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = normalizeDiffLine(line, mode)
	}
	return out
}

// withoutBlankHunks drops changes made only of whitespace-only lines. The
// hunk offsets stay in the original line arrays so block actions still edit
// the source text they were drawn from.
func withoutBlankHunks(hs []linediff.Hunk, old, new []string) []linediff.Hunk {
	out := hs[:0]
	for _, h := range hs {
		blank := true
		for _, line := range old[h.A:min(h.A+h.ALen, len(old))] {
			if strings.TrimSpace(line) != "" {
				blank = false
				break
			}
		}
		if blank {
			for _, line := range new[h.B:min(h.B+h.BLen, len(new))] {
				if strings.TrimSpace(line) != "" {
					blank = false
					break
				}
			}
		}
		if !blank {
			out = append(out, h)
		}
	}
	return out
}

// diffContextOf is how many unchanged lines the diffs show around a
// change, as the settings say.
func (a *app) diffContextOf() int {
	return min(max(a.settings.DiffContext, 0), settings.MaxDiffContext)
}

// diffSide is a version of the file a diff shows.
type diffSide struct {
	text  string
	ok    bool // the version has the file
	lines []string
	src   *lineSource
	hl    *highlight.Highlighter // made as its lines first show
	win   *lineWindow
}

// diffFile is a file of a diff: its two versions, compared against base
// as the source control view lists it in group, and the changes between
// them, read and compared in the background.
type diffFile struct {
	repoRoot string
	path     string
	peerPath string // right-hand path for a local two-file comparison
	base     diffBase
	group    scmGroup
	entry    git.Entry
	letter   byte // its letter in the source control view

	old, new diffSide
	binary   bool
	hunks    []linediff.Hunk
	// unified and pairs are the rows of every line, in each layout.
	unified, pairs []diffRow
	widest         int // the longest line, in columns
	loaded         bool
	err            string

	// gitStale has the sides Git keeps read again, diskStale the work
	// tree's file; docVersion is the version of the open document's text
	// compared last. peerDocVersion is the corresponding version of the
	// right-hand document in a local two-file comparison.
	gitStale, diskStale        bool
	docVersion, peerDocVersion uint64
	running, waiting           bool
	started                    time.Time

	words map[[2]int][2][]linediff.Span

	// spans are the runs of a pair of lines that changed, as the diff
	// showed them last, by the words or by the characters.
	spans map[diffSpanKey][2][]linediff.Span
	// open are the gaps of unchanged lines opened, by their keys, and
	// folded is set for a file whose lines are hidden under its header.
	open   map[int]bool
	folded bool
}

func newDiffFile(e git.Entry, group scmGroup) *diffFile {
	f := &diffFile{path: e.Path, group: group, gitStale: true, diskStale: true}
	f.setEntry(e)
	return f
}

// setEntry takes what the status says of the file now.
func (f *diffFile) setEntry(e git.Entry) {
	f.entry, f.letter, f.base = e, e.Letter(), diffIndex
	if f.group == groupStaged {
		f.letter, f.base = e.X, diffStaged
	}
}

// side returns the old side, or the new.
func (f *diffFile) side(newSide bool) *diffSide {
	if newSide {
		return &f.new
	}
	return &f.old
}

// counts returns how many lines the file's changes add and remove.
func (f *diffFile) counts() (add, del int) {
	for _, h := range f.hunks {
		add, del = add+h.BLen, del+h.ALen
	}
	return add, del
}

// diffView shows every file changed, in a tab: the changes staged, from
// HEAD to the index, then those not, from the index to the work tree, as
// the source control view lists them. Each file shows under a header, its
// lines removed and added interleaved, or side by side, and those
// unchanged away from them folded.
type diffView struct {
	repoRoot string // work tree that owns this view, fixed when opened
	layout   diffLayout
	split    bool // as the layout and the width chose last
	full     bool // every unchanged line shows
	local    bool // a comparison of two local files, outside Git status
	wrap     *bool
	wrapGen  int

	files []*diffFile
	// status is the status of the repository the files were listed from.
	status *git.Status
	// commit is the commit whose changes the tab shows, from its first
	// parent; nil for the tab of the changes of the work tree.
	commit *commitDiff
	// tree is the panel of the files of a commit's tab, beside it.
	tree commitTree

	// rows are the rows of the files as they show, and tops where each
	// starts, in lines, and after the last, the lines of all. starts are
	// the first rows of each hunk, and heads the headers of each file.
	rows          []diffRow
	tops          []int
	starts, heads []int
	// cards are the rows of the cards of notes, and revealNote a note
	// whose card to scroll to once it is laid out.
	cards      []int
	revealNote *reviewNote
	// sections counts the files of the changes not staged, and of those
	// staged, as the rows were laid out.
	sections [2]int
	built    diffBuild
	gen      int // counts the changes the rows are laid out again for
	// near are the files read as they come near the view, from up to to.
	near [2]int
	// reveal is the file, or section, to scroll to once it is listed, nil
	// for none; synced is the file the source control view was last
	// told is at the top.
	reveal *diffTarget
	synced *diffFile
	// place is where the view was when a file of the diff folded under
	// it, to go back to when the file unfolds.
	place *diffAnchor

	scroll ui.ScrollState
	pane   ui.Handle
	// sideY is how far the right side of a split diff whose sides do not
	// scroll together is scrolled, in its own.
	sideY float32
	// sideTops are, per side, where each row's own lines stand in the
	// side's stream of rows, when the rows do not align across the
	// sides: a side closes up where the other has lines it lacks. Empty
	// while the rows align, which is the default.
	sideTops [2][]int
	// runs are, for each row while the sides stand in streams, the run of
	// lines it is in, from its top to its bottom in the rows aligned.
	runs [][2]int
	// current is the hunk gone to last, of starts, and cursor the row
	// chosen, -1 for none. jump is a row to scroll to in the next frame,
	// -1 for none, as jumpTo says.
	current, cursor int
	// anchor is where the rows chosen start, the cursor where they end, -1
	// for the cursor's row alone; press is the row the pointer was pressed
	// on, -1 when it is not, and dragged is set once it dragged to another.
	anchor, press int
	dragged       bool
	// acted is set when the last click opened a gap or folded a file.
	acted  bool
	jump   int
	jumpTo jumpPlace
	// glide has the jump glide there, going to a change, and glideY is
	// where the view glides to.
	glide        bool
	glideY       float32
	viewW, viewH float32
	// flash is the lines an arrow or an append of the diff just wrote,
	// which glow a moment as the diff shows them again.
	flash diffFlash

	// summary is what the status bar says of the diff, worked out for the
	// generation summaryGen, plus one.
	summary    string
	summaryGen int
	// boundary is what the change or file navigation said where it
	// stopped, at an end of a file or of the diff, until one more step
	// that way crosses into another file, nil for none.
	boundary *diffBoundary
	// edited is the document an arrow of the diff last wrote: the undo
	// of the diff, from its keyboard, works in it.
	edited *document
	// edits are the editors the diff made to edit the working tree sides
	// of its files, and editing the one whose side the caret is in, nil
	// while the diff reads as a pager.
	edits   []*diffEdit
	editing *diffEdit
}

// diffBoundary is where the navigation of a diff stopped: the step that
// reached it, and what it says.
type diffBoundary struct {
	step int
	msg  string
}

// commitDiff is the commit a diff shows, and the files it changed, read
// in the background.
type commitDiff struct {
	hash string
	// from is the commit a comparison starts at, whose changes are those
	// from it to hash; "" for the changes of the commit hash alone.
	from            string
	info            git.Commit
	files           []git.CommitFile
	loading, loaded bool
	err             string
	// lines is how many lines tall its card is, as laid out last, and
	// unfolded whether the card shows all of the message.
	lines    int
	unfolded bool
}

// diffTarget is a file of a diff to scroll to: path, as compared against
// base, or against either for any; or with no path, the section of base.
type diffTarget struct {
	path string
	base diffBase
	any  bool
}

// matches reports whether a file is the target.
func (t *diffTarget) matches(f *diffFile) bool {
	return f.path == t.path && (t.any || f.base == t.base)
}

// diffBuild is what the rows of a diff were laid out for.
type diffBuild struct {
	gen               int
	split, wrap, full bool
	aligned           bool // the rows align across the sides of a split
	context           int
	whitespace        diffWhitespace
	cols              [2]int
	card              int // how many lines tall the card of a commit is
	font              codeFont
	// review is the generation of the notes, noteW how wide the comments
	// of their cards are, and draft how many rows the comment being typed
	// takes.
	review, draft int
	noteW         float32
}

// jumpPlace is where a row jumped to shows.
type jumpPlace int

const (
	jumpThird  jumpPlace = iota // a third down the view, unless in view
	jumpTop                     // at the top
	jumpReveal                  // as little scrolled as shows it
)

func newDiffView(layout diffLayout) *diffView {
	return &diffView{layout: layout, current: -1, cursor: -1, anchor: -1, press: -1, jump: -1, near: [2]int{0, diffNear}}
}

func newLocalCompareView(layout diffLayout, left, right string) *diffView {
	v := newDiffView(layout)
	v.local = true
	v.files = []*diffFile{{
		path: left, peerPath: right, base: diffLocal, group: groupChanges,
		entry: git.Entry{Path: left}, gitStale: true,
	}}
	v.near = [2]int{0, 1}
	return v
}

// newCommitView makes the tab of a commit's changes, its message read
// already in info when its hash is set.
//
// A key "from..to" makes the tab of a comparison instead: the changes from
// commit from to commit to, read as those of a commit whose parent is from.
func newCommitView(layout diffLayout, key string, info git.Commit) *diffView {
	v := newDiffView(layout)
	if from, to, ok := strings.Cut(key, ".."); ok {
		v.commit = &commitDiff{hash: to, from: from, info: git.Commit{Hash: to, Parents: []string{from}}}
		return v
	}
	v.commit = &commitDiff{hash: key, info: info}
	return v
}

// key is what names a commit's diff: its hash, or "from..to" for a
// comparison, as newCommitView takes it.
func (cm *commitDiff) key() string {
	if cm.from == "" {
		return cm.hash
	}
	return cm.from + ".." + cm.hash
}

func (v *diffView) name() string {
	if v.local && len(v.files) > 0 {
		return filepath.Base(v.files[0].path) + " ↔ " + filepath.Base(v.files[0].peerPath)
	}
	if c := v.commit; c != nil {
		if c.from != "" {
			return git.ShortHash(c.from) + " → " + git.ShortHash(c.hash)
		}
		if c.info.Subject != "" {
			return c.info.Subject
		}
		return locales.DiffCommitName(git.ShortHash(c.hash))
	}
	return locales.DiffChangesName()
}

// same reports whether two diffs show the same changes: both those of
// the work tree, or of one commit.
func (v *diffView) same(o *diffView) bool {
	if v.repoRoot != o.repoRoot {
		return false
	}
	if v.commit == nil || o.commit == nil {
		return v.commit == nil && o.commit == nil
	}
	return v.commit.key() == o.commit.key()
}

// stop lets go of the highlighters and the editors of the sides edited.
func (v *diffView) stop() {
	for _, f := range v.files {
		f.stop()
	}
	for _, e := range v.edits {
		e.ed.release()
	}
	v.edits, v.editing = nil, nil
}

// stop lets go of a file's highlighters.
func (f *diffFile) stop() {
	for _, s := range []*diffSide{&f.old, &f.new} {
		if s.hl != nil {
			s.hl.Close()
			s.hl = nil
		}
	}
}

// changed has the rows laid out again in the next frame.
func (v *diffView) changed() { v.gen++ }

// lineSource is lines of text as the highlighter reads them.
type lineSource struct {
	text    string
	starts  []int // the byte offset of each line
	version uint64
}

var sourceVersions atomic.Uint64

func newLineSource(lines []string) *lineSource {
	s := &lineSource{text: strings.Join(lines, "\n"), starts: make([]int, len(lines)), version: sourceVersions.Add(1)}
	at := 0
	for i, l := range lines {
		s.starts[i] = at
		at += len(l) + 1
	}
	return s
}

func (s *lineSource) Version() uint64 { return s.version }
func (s *lineSource) LineCount() int  { return len(s.starts) }
func (s *lineSource) LineRange(i int) (int, int) {
	if i+1 < len(s.starts) {
		return s.starts[i], s.starts[i+1] - 1
	}
	return s.starts[i], len(s.text)
}
func (s *lineSource) Slice(start, end int) string { return s.text[start:end] }

// diffViewPause is the least time between two comparisons as a document
// is typed in.
const diffViewPause = 150 * time.Millisecond

// diffReads is how many files a diff reads at once, and diffNear how many
// files a diff reads before it knows which are in view.
const (
	diffReads = 4
	diffNear  = 8
)

// foldedLines is how many lines a file's changes take, past which the
// diff shows it folded.
const foldedLines = 2000

// changesTab returns the tab of the changes in an unlocked column, nil
// when none has one. Automatic opens leave locked columns alone.
func (a *app) changesTab(roots ...string) (*column, int) {
	root := a.diffRoot(roots)
	for _, col := range a.cols {
		if col.locked {
			continue
		}
		for i, t := range col.tabs {
			if t.diff != nil && t.diff.commit == nil && a.diffBelongs(t.diff, root) {
				return col, i
			}
		}
	}
	return nil, -1
}

// openChangesTab shows the tab of the changes, made after the tab shown
// of the column that has the keyboard when there is none, and returns its
// diff.
func (a *app) openChangesTab(focus bool, roots ...string) *diffView {
	root := a.diffRoot(roots)
	if col, i := a.changesTab(root); col != nil {
		a.activate(col, i, focus)
		return col.tabs[i].diff
	}
	col := a.fileColumn()
	at := min(col.active+1, len(col.tabs))
	v := newDiffView(parseDiffLayout(a.settings.DiffLayout))
	v.repoRoot = root
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, focus)
	return v
}

// showChanges shows the tab of the changes scrolled to a file, or with no
// path, to the section of a base, once the file is listed.
func (a *app) showChanges(target diffTarget, focus bool, roots ...string) {
	root := a.diffRoot(roots)
	if len(roots) == 0 && target.path != "" {
		if r := a.repositoryForPath(target.path).git.repo; r != nil {
			root = r.Root
		}
	}
	v := a.openChangesTab(focus, root)
	v.reveal = &target
	v.changed()
}

// compareLocalFiles opens a two-file diff. The dialog chooses exactly two
// local paths; neither file is staged or otherwise changed by opening it.
func (a *app) compareLocalFiles() {
	a.host.choosePaths(locales.CompareTwoFiles(), locales.ButtonCompare(), a.workDir(), false, func(paths []string) {
		if len(paths) != 2 || paths[0] == paths[1] {
			a.fail("%s", locales.DiffChooseDifferent())
			return
		}
		a.compareFiles(paths[0], paths[1])
	})
}

// compareFiles shows the differences of two files of this machine in a
// tab after the one shown.
func (a *app) compareFiles(left, right string) {
	col := a.column()
	at := min(col.active+1, len(col.tabs))
	v := newLocalCompareView(parseDiffLayout(a.settings.DiffLayout), left, right)
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, true)
}

// openChanges shows the changes of the file of the tab shown, or, from
// the tab of the changes, the file at its chosen row.
func (a *app) openChanges() {
	t := a.activeTab()
	switch {
	case t == nil:
	case t.diff != nil:
		a.openDiffFile(t.diff)
	case t.path() == "":
	default:
		ctx := a.repositoryForPath(t.path())
		if ctx.git.repo != nil {
			ctx.showChanges(diffTarget{path: t.path(), any: true}, true)
		}
	}
}

// openDiffFile opens the file of a diff's chosen row, or, when no row is
// chosen, of the change gone to last, at its line.
func (a *app) openDiffFile(v *diffView) {
	row := v.focusRow()
	if v.cursor < 0 && v.current >= 0 && v.current < len(v.starts) {
		row = v.starts[v.current]
	}
	if path, line := v.lineAt(row); path != "" {
		a.openFileAt(path, line, true)
	}
}

// showingDiff returns the place of a column's tab that shows the changes
// v shows, -1 when it has none.
func (col *column) showingDiff(v *diffView) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.diff != nil && t.diff.same(v) })
}

// commitTab returns a commit's changes, or a comparison's by its
// "from..to", in an unlocked column, nil when none has one.
func (a *app) commitTab(hash string, roots ...string) (*column, int) {
	root := a.diffRoot(roots)
	for _, col := range a.cols {
		if col.locked {
			continue
		}
		for i, t := range col.tabs {
			if t.diff != nil && t.diff.commit != nil && t.diff.commit.key() == hash && a.diffBelongs(t.diff, root) {
				return col, i
			}
		}
	}
	return nil, -1
}

// showCommit shows the changes of a commit in a tab of its own, made after
// the tab shown of the column that has the keyboard when there is none,
// and returns its diff.
func (a *app) showCommit(c git.Commit, focus bool, roots ...string) *diffView {
	root := a.diffRoot(roots)
	if col, i := a.commitTab(c.Hash, root); col != nil {
		a.keepTab(col.tabs[i])
		a.activate(col, i, focus)
		return col.tabs[i].diff
	}
	col := a.fileColumn()
	at := min(col.active+1, len(col.tabs))
	v := newCommitView(parseDiffLayout(a.settings.DiffLayout), c.Hash, c)
	v.repoRoot = root
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, focus)
	a.rememberTabs()
	return v
}

// previewCommit shows the changes of a commit in the preview tab of the
// column that has the keyboard, without giving it the keyboard, unless a
// tab shows them already, and returns its diff.
func (a *app) previewCommit(c git.Commit, roots ...string) *diffView {
	root := a.diffRoot(roots)
	if col, i := a.commitTab(c.Hash, root); col != nil {
		a.activate(col, i, false)
		return col.tabs[i].diff
	}
	col := a.fileColumn()
	var v *diffView
	a.previewIn(col, func(at int) {
		v = newCommitView(parseDiffLayout(a.settings.DiffLayout), c.Hash, c)
		v.repoRoot = root
		a.addTab(col, at, &colTab{diff: v, transient: true})
		a.activate(col, at, false)
	})
	return v
}

// diffDoc returns the open document of a file of a diff, whose text is
// the work tree's side, nil when it is not open or the file's changes
// are those staged.
func (a *app) diffDoc(f *diffFile) *document {
	if f.base == diffStaged || f.base == diffCommit {
		return nil
	}
	if i := a.docIndex(f.path); i >= 0 {
		return a.docs[i]
	}
	return nil
}

// diffPeerDoc returns the right-hand document of a local file comparison.
func (a *app) diffPeerDoc(f *diffFile) *document {
	if f == nil || f.base != diffLocal || f.peerPath == "" {
		return nil
	}
	if i := a.docIndex(f.peerPath); i >= 0 {
		return a.docs[i]
	}
	return nil
}

// listDiffFiles has a diff show the files the status lists now, staged
// and not, keeping what it read of those it had.
func (a *app) listDiffFiles(v *diffView) {
	if v.commit != nil {
		a.listCommitFiles(v)
		return
	}
	ctx := a.diffContext(v)
	st := ctx.git.status
	if st == v.status {
		return
	}
	v.status = st
	groups := ctx.groupEntries()
	type listed struct {
		e     git.Entry
		group scmGroup
	}
	var es []listed
	for _, e := range groups[groupStaged] {
		es = append(es, listed{e, groupStaged})
	}
	var rest []listed
	for _, g := range []scmGroup{groupChanges, groupUntracked} {
		for _, e := range groups[g] {
			rest = append(rest, listed{e, g})
		}
	}
	slices.SortFunc(rest, func(x, y listed) int { return strings.Compare(x.e.Path, y.e.Path) })
	es = append(es, rest...)
	// The files listed before, by path and whether staged.
	type fileKey struct {
		path   string
		staged bool
	}
	old := make(map[fileKey]*diffFile, len(v.files))
	for _, f := range v.files {
		old[fileKey{f.path, f.group == groupStaged}] = f
	}
	v.files = make([]*diffFile, 0, len(es))
	for _, l := range es {
		// The files' changes on disk show as they come.
		if a.tree != nil {
			a.tree.Watch(l.e.Path)
		}
		k := fileKey{l.e.Path, l.group == groupStaged}
		f, ok := old[k]
		if !ok {
			f := newDiffFile(l.e, l.group)
			f.repoRoot = v.repoRoot
			v.files = append(v.files, f)
			continue
		}
		delete(old, k)
		f.group = l.group
		f.setEntry(l.e)
		v.files = append(v.files, f)
	}
	for _, f := range old {
		f.stop()
	}
	v.changed()
}

// listCommitFiles has the diff of a commit read the commit and list the
// files it changed, once, in the background.
func (a *app) listCommitFiles(v *diffView) {
	cm := v.commit
	r := a.diffContext(v).git.repo
	if cm.loaded || cm.loading || r == nil {
		return
	}
	cm.loading = true
	hash, info := cm.hash, cm.info
	bg.Go(func() {
		defer recoverCrash()
		var err error
		if info.Hash == "" || len(info.Parents) == 0 && info.Subject == "" {
			info, err = r.CommitInfo(hash)
		}
		var files []git.CommitFile
		if err == nil {
			files, err = r.CommitFiles(info)
		}
		a.post(func() {
			cm.loading, cm.loaded = false, true
			if err != nil {
				cm.err = err.Error()
				v.changed()
				return
			}
			cm.info, cm.files = info, files
			v.files = make([]*diffFile, 0, len(files))
			for _, e := range commitEntries(files) {
				f := &diffFile{path: e.Path, base: diffCommit, group: groupCommits, letter: e.X, gitStale: true}
				f.repoRoot = v.repoRoot
				f.entry = e
				v.files = append(v.files, f)
			}
			v.tree.gen++
			if a.tree != nil {
				a.rememberTabs()
			}
			v.changed()
		})
	})
}

// commitEntries returns the files a commit changed as entries of a
// status, their letters as the commit's.
func commitEntries(files []git.CommitFile) []git.Entry {
	es := make([]git.Entry, len(files))
	for i, cf := range files {
		es[i] = git.Entry{Path: cf.Path, Orig: cf.Orig, X: cf.Status, Y: '.'}
	}
	return es
}

// refreshDiff compares the sides of the files of a diff near its view
// again in the background, where one may have changed: the others wait
// until they come near it.
func (a *app) refreshDiff(v *diffView) {
	if !v.local {
		a.listDiffFiles(v)
	}
	reading := 0
	for _, f := range v.files {
		if f.running {
			reading++
		}
	}
	lo := min(max(v.near[0], 0), len(v.files))
	hi := min(max(v.near[1], lo), len(v.files))
	order := make([]*diffFile, 0, hi-lo+1)
	if t := v.reveal; t != nil {
		for _, f := range v.files {
			if t.matches(f) {
				order = append(order, f)
				break
			}
		}
	}
	if lo < hi {
		order = append(order, v.files[lo:hi]...)
	}
	for _, f := range order {
		if reading >= diffReads {
			break
		}
		if a.refreshDiffFile(v, f) {
			reading++
		}
	}
}

// refreshDiffFile compares the sides of a file of a diff again in the
// background when one may have changed, and reports whether it started.
func (a *app) refreshDiffFile(v *diffView, f *diffFile) bool {
	r := a.diffContext(v).git.repo
	if f.running || f.waiting || (r == nil && f.base != diffLocal) {
		return false
	}
	doc := a.diffDoc(f)
	peerDoc := a.diffPeerDoc(f)
	var snap ui.TextSnapshot
	var peerSnap ui.TextSnapshot
	edited := false
	if doc != nil {
		snap = doc.buf.Snapshot()
		edited = snap.Version() != f.docVersion
	}
	if peerDoc != nil {
		peerSnap = peerDoc.buf.Snapshot()
		edited = edited || peerSnap.Version() != f.peerDocVersion
	}
	if f.loaded && !f.gitStale && !edited && !(f.diskStale && doc == nil) {
		return false
	}
	if wait := diffViewPause - time.Since(f.started); f.loaded && !f.gitStale && wait > 0 {
		f.waiting = true
		bg.AfterFunc(wait, func() { a.post(func() { f.waiting = false }) })
		return false
	}
	f.running, f.started = true, time.Now()
	readGit, readDisk := f.gitStale || !f.loaded, doc == nil && (f.diskStale || !f.loaded)
	f.gitStale, f.diskStale = false, false
	if doc != nil {
		f.docVersion = snap.Version()
	}
	if peerDoc != nil {
		f.peerDocVersion = peerSnap.Version()
	}
	old, new := f.old, f.new
	path, orig, base, peerPath := f.path, f.entry.Orig, f.base, f.peerPath
	sub, dirty := f.entry.Submodule(), f.entry.Dirty()
	whitespace := a.diffWhitespaceOf()
	var hash, parent string
	if cm := v.commit; cm != nil {
		hash = cm.hash
		if len(cm.info.Parents) > 0 {
			parent = cm.info.Parents[0]
		}
	}
	if orig == "" {
		orig = path
	}
	bg.Go(func() {
		defer recoverCrash()
		var err error
		read := func(rev, path string) (string, bool) {
			text, ok, e := r.Show(rev, path)
			if e != nil && err == nil {
				err = e
			}
			return text, ok
		}
		if base == diffLocal {
			if doc != nil {
				old.text, old.ok = snap.String(), true
			} else {
				old.text, _, err = readWork(path)
				old.ok = err == nil
			}
			if peerDoc != nil {
				new.text, new.ok = peerSnap.String(), true
			} else {
				new.text, _, err = readWork(peerPath)
				new.ok = err == nil
			}
		} else if readGit {
			switch base {
			case diffIndex:
				old.text, old.ok = read("", path)
			case diffCommit:
				old.text, old.ok = "", false
				if parent != "" {
					old.text, old.ok = read(parent, orig)
				}
				new.text, new.ok = read(hash, path)
			default:
				old.text, old.ok = read("HEAD", path)
			}
			if base == diffStaged {
				new.text, new.ok = read("", path)
			}
		}
		switch {
		case base == diffLocal:
			// Both sides were read above; open documents are incorporated by
			// the local comparison helpers when they are edited.
		case doc != nil:
			new.text, new.ok = snap.String(), true
		case readDisk && sub && base != diffStaged && base != diffCommit:
			var e error
			new.text, new.ok, e = r.WorkSubproject(path, dirty)
			if e != nil && err == nil {
				err = e
			}
		case readDisk && base != diffStaged && base != diffCommit:
			var e error
			new.text, _, e = readWork(path)
			new.ok = e == nil
			if e != nil && !errors.Is(e, os.ErrNotExist) && err == nil {
				err = e
			}
		}
		binary := isBinary([]byte(old.text)) || isBinary([]byte(new.text))
		oldLines, newLines := splitLines(old.text, old.ok), splitLines(new.text, new.ok)
		compareOld, compareNew := compareDiffLines(oldLines, whitespace), compareDiffLines(newLines, whitespace)
		var hunks []linediff.Hunk
		if !binary {
			hunks = linediff.Hunks(compareOld, compareNew)
			if whitespace == whitespaceBlank {
				hunks = withoutBlankHunks(hunks, oldLines, newLines)
			}
		}
		unified, pairs := lineRows(oldLines, newLines, hunks)
		widest := 0
		for _, ls := range [][]string{oldLines, newLines} {
			for _, l := range ls {
				if len(l) > widest {
					widest = max(widest, columns(clipLine(l)))
				}
			}
		}
		a.post(func() {
			f.running = false
			if err != nil {
				f.err = errorLine(err)
			} else {
				f.err = ""
			}
			first := !f.loaded
			f.loaded, f.binary, f.hunks, f.widest = true, binary, hunks, widest
			f.unified, f.pairs = unified, pairs
			f.spans = nil
			setDiffSide(&f.old, old, oldLines)
			setDiffSide(&f.new, new, newLines)
			if add, del := f.counts(); first && add+del > foldedLines {
				f.folded = true
			}
			v.changed()
		})
	})
	return true
}

// setDiffSide takes a side's new text, to highlight.
func setDiffSide(s *diffSide, read diffSide, lines []string) {
	s.ok = read.ok
	if s.src != nil && s.text == read.text {
		return
	}
	s.text, s.lines, s.src, s.win = read.text, lines, newLineSource(lines), nil
}

// highlighter returns the highlighter of a side of a file of a diff,
// made as its lines first show.
func (a *app) highlighter(f *diffFile, s *diffSide) *highlight.Highlighter {
	if s.hl == nil {
		s.hl = highlight.New(highlight.Lexer(f.path), a.invalidate)
	}
	return s.hl
}

// splitLines returns the lines of a version of a file, without the empty
// one after its last newline; none when it has no file.
func splitLines(text string, ok bool) []string {
	if !ok || text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// maxDiffLine is how much of a line a diff shows, in bytes: the rest of
// a longer one, as of a minified file, is cut.
const maxDiffLine = 2000

// wrapsDiff reports whether a diff wraps its lines: as its tab chose, or
// else as the settings say.
func (a *app) wrapsDiff(v *diffView) bool {
	if v.wrap != nil && v.wrapGen == a.settings.WrapGen {
		return *v.wrap
	}
	return a.settings.WordWrap
}

// splitWidth is how many columns each side of a diff needs, at least, for
// the auto layout to show them side by side.
const splitWidth = 80

// splits reports whether a diff shows its sides side by side, at a width.
func (a *app) splits(v *diffView, viewW float32) bool {
	switch v.layout {
	case layoutSplit:
		return true
	case layoutInline:
		return false
	}
	l := a.diffGeometry(v, true, viewW)
	return float32(int(viewW/2))-l.textX >= splitWidth*a.font.charW
}

// layoutRows lays out the rows of a diff again when what they show, or
// how, changed, keeping the row at the top of the view where it was, or
// scrolling to the file to reveal once it is listed.
func (a *app) layoutRows(v *diffView, l diffGeometry) {
	b := diffBuild{gen: v.gen, split: v.split, wrap: a.wrapsDiff(v), full: v.full, aligned: a.settings.DiffAlignedRows, context: a.diffContextOf(), whitespace: a.diffWhitespaceOf(), font: a.font}
	if v.commit != nil {
		b.card = v.commit.lines
	}
	if b.wrap {
		b.cols = l.cols
	}
	_, noteW := noteGeometry(l, v.split, false)
	b.review, b.noteW = a.review.gen, noteTextW(noteW)
	if r := &a.review; r.draft != nil && r.ed == nil {
		_, w := noteGeometry(l, v.split, r.draft.oldSide)
		b.draft = a.noteLines(r.draft, true, r.text, noteTextW(w))
	}
	if b == v.built && v.tops != nil {
		return
	}
	lineH := a.font.lineH
	anchor, offset := diffAnchor{}, float32(0)
	// The row chosen holds its place while in view, or else the top row.
	top := v.focusRowAt(v.topRow(lineH))
	if c := v.cursor; c >= 0 && c < len(v.rows) && v.tops != nil {
		if y := editorPadY + float32(v.tops[c])*lineH - v.scroll.Y; y >= 0 && y < v.viewH {
			top = c
		}
	}
	if v.jump < 0 && top >= 0 && v.tops != nil {
		anchor = v.anchorOf(top)
		offset = editorPadY + float32(v.tops[top])*lineH - v.scroll.Y
	}
	chosen, held := diffAnchor{}, diffAnchor{}
	if v.cursor >= 0 && v.cursor < len(v.rows) && v.tops != nil {
		chosen = v.anchorOf(v.cursor)
	}
	if v.anchor >= 0 && v.anchor < len(v.rows) && v.tops != nil {
		held = v.anchorOf(v.anchor)
	}
	// The file to reveal shows its lines.
	var target *diffFile
	t := v.reveal
	if t != nil && (v.status != nil || v.commit != nil && v.commit.loaded && v.commit.err == "") {
		v.reveal = nil
		if t.path != "" {
			for _, f := range v.files {
				if t.matches(f) && (target == nil || f.base == diffIndex) {
					target = f
				}
			}
			if target == nil {
				a.fail("%s", locales.FailNoChanges(filepath.Base(t.path)))
			} else {
				target.folded = false
				v.place = nil
			}
		}
	}
	v.built = b
	v.rows, v.starts, v.heads, v.cards = v.rows[:0], v.starts[:0], v.heads[:0], v.cards[:0]
	v.sections = [2]int{}
	for _, f := range v.files {
		v.sections[btoi(f.base == diffStaged)]++
	}
	plain := func(kind rowKind, file int, label string) {
		v.rows = append(v.rows, diffRow{kind: kind, file: file, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, h: 1, label: label})
	}
	reveal := -1
	if cm := v.commit; cm != nil && len(v.files) > 0 {
		v.rows = append(v.rows, diffRow{kind: rowCommit, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, h: max(cm.lines, 3)})
	}
	for i, f := range v.files {
		if v.commit != nil {
			if i > 0 {
				plain(rowSpace, i, "")
			}
		} else if i == 0 || f.base != v.files[i-1].base {
			if i > 0 {
				plain(rowSpace, i, "")
			}
			if t != nil && t.path == "" && t.base == f.base {
				reveal = len(v.rows)
			}
			plain(rowSection, i, "")
		} else {
			plain(rowSpace, i, "")
		}
		if f == target {
			reveal = len(v.rows)
		}
		v.heads = append(v.heads, len(v.rows))
		plain(rowFile, i, "")
		if f.folded {
			continue
		}
		note := ""
		switch {
		case !f.loaded:
			note = locales.Loading()
		case f.err != "":
			note = f.err
		case f.binary:
			note = locales.DiffBinary()
		case !f.old.ok && !f.new.ok:
			note = locales.DiffNeitherVersion()
		case len(f.hunks) == 0:
			note = locales.DiffNoTextChanges()
		}
		if note != "" {
			plain(rowNote, i, note)
			continue
		}
		all := f.unified
		if v.split {
			all = f.pairs
		}
		start := len(v.rows)
		v.rows = foldRows(v.rows, f, i, all, v.full, b.context)
		// A file one side of the diff lacks whole is not changed by arrows:
		// its changes go in or out with the file itself, as the buttons of
		// its header do them, and the diff says so.
		if (f.base == diffIndex || f.base == diffStaged) && len(f.hunks) > 0 && (!f.old.ok || !f.new.ok) {
			note := locales.DiffWholeFileNew()
			if !f.new.ok {
				note = locales.DiffWholeFileGone()
			}
			v.rows = append(v.rows, diffRow{kind: rowNote, file: i, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, h: 1, label: note})
		}
		a.noteRows(v, l, f, i, start)
	}
	for i := range v.rows {
		r := &v.rows[i]
		// The cards of notes between the lines of a change are of it.
		prev := i - 1
		for prev >= 0 && v.rows[prev].kind == rowComment {
			prev--
		}
		if r.changed() && (prev < 0 || v.rows[prev].hunk != r.hunk || v.rows[prev].file != r.file || !v.rows[prev].changed()) {
			v.starts = append(v.starts, i)
		}
		if r.kind == rowComment {
			v.cards = append(v.cards, i)
		}
		if r.kind != rowCommit && r.kind != rowComment {
			r.h = 1
		}
		if b.wrap && r.kind == rowLine {
			f := v.files[r.file]
			if v.split {
				if r.old >= 0 {
					r.h = wrapCount(f.old.lines[r.old], l.cols[0])
				}
				if r.new >= 0 {
					r.h = max(r.h, wrapCount(f.new.lines[r.new], l.cols[1]))
				}
			} else if n, newSide := r.line(); n >= 0 {
				r.h = wrapCount(f.side(newSide).lines[n], l.cols[0])
			}
		}
	}
	v.tops = slices.Grow(v.tops[:0], len(v.rows)+1)
	at := 0
	for _, r := range v.rows {
		v.tops = append(v.tops, at)
		at += r.h
	}
	v.tops = append(v.tops, at)
	// Where the rows do not align across the sides of a split diff, each
	// side's rows stand in a stream of their own: a side closes up where
	// the other has lines it lacks, so its lines after them stand higher
	// than the rows aligned have them.
	if v.split && !a.settings.DiffAlignedRows {
		v.sideTops = [2][]int{slices.Grow(v.sideTops[0][:0], len(v.rows)+1), slices.Grow(v.sideTops[1][:0], len(v.rows)+1)}
		atL, atR := 0, 0
		for i, r := range v.rows {
			if r.kind != rowLine {
				// A row across the sides, as a header, a fold or a note, stands
				// where the rows aligned have it: the streams close up
				// between such rows, and meet again at them.
				atL, atR = v.tops[i], v.tops[i]
			}
			v.sideTops[0] = append(v.sideTops[0], atL)
			v.sideTops[1] = append(v.sideTops[1], atR)
			hL, hR := r.h, r.h
			if r.kind == rowLine {
				f := v.files[r.file]
				hL = 0
				hR = 0
				if r.old >= 0 {
					hL = 1
					if b.wrap {
						hL = wrapCount(f.old.lines[r.old], l.cols[0])
					}
				}
				if r.new >= 0 {
					hR = 1
					if b.wrap {
						hR = wrapCount(f.new.lines[r.new], l.cols[1])
					}
				}
			}
			atL += hL
			atR += hR
		}
		v.sideTops[0] = append(v.sideTops[0], atL)
		v.sideTops[1] = append(v.sideTops[1], atR)
		// Each run of lines between rows across the sides, where its
		// streams stand: the lines of a side that follows the other show
		// in it alone.
		v.runs = slices.Grow(v.runs[:0], len(v.rows))
		for i := 0; i < len(v.rows); {
			j := i + 1
			if v.rows[i].kind == rowLine {
				for j < len(v.rows) && v.rows[j].kind == rowLine {
					j++
				}
			}
			for k := i; k < j; k++ {
				v.runs = append(v.runs, [2]int{v.tops[i], v.tops[j]})
			}
			i = j
		}
	} else {
		v.sideTops = [2][]int{}
		v.runs = v.runs[:0]
	}
	v.current = min(v.current, len(v.starts)-1)
	v.cursor = min(v.cursor, len(v.rows)-1)
	// The row chosen stays chosen, or the header of its file, when it is
	// folded away.
	if chosen.file != nil {
		v.cursor = -1
		for i := range v.rows {
			if a := v.anchorOf(i); a == chosen || v.cursor < 0 && a.file == chosen.file && a.kind == rowFile {
				v.cursor = i
				if a == chosen {
					break
				}
			}
		}
	}
	// A file that folded under the view unfolds where the view was.
	if p := v.place; p != nil {
		for i := range v.rows {
			if v.anchorOf(i) == *p {
				v.place = nil
				v.jumpRow(i, jumpTop)
				break
			}
		}
	}
	v.anchor = -1
	if held.file != nil {
		for i := range v.rows {
			if v.anchorOf(i) == held {
				v.anchor = i
				break
			}
		}
	}
	switch {
	case reveal >= 0:
		v.cursor = reveal
		v.jumpRow(reveal, jumpTop)
	case anchor.file != nil:
		for i := range v.rows {
			if v.anchorOf(i) == anchor {
				v.scroll.Y = max(0, editorPadY+float32(v.tops[i])*lineH-offset)
				break
			}
		}
	}
	// A note gone to is chosen, once its card is laid out.
	if n := v.revealNote; n != nil {
		for _, i := range v.cards {
			if a.review.noteAt(v.rows[i].n) == n {
				v.revealNote, v.anchor, v.cursor = nil, -1, i
				v.jumpRow(i, jumpThird)
				break
			}
		}
	}
}

// btoi is 1 for true, and 0 for false.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// diffAnchor is a row of a diff as it stays from one layout to the next:
// its kind, its file, and its line, negative for one of the old side only.
type diffAnchor struct {
	kind rowKind
	file *diffFile
	line int
}

func (v *diffView) anchorOf(i int) diffAnchor {
	if i < 0 || i >= len(v.rows) {
		return diffAnchor{}
	}
	r := v.rows[i]
	if r.file < 0 || r.file >= len(v.files) {
		return diffAnchor{}
	}
	line := r.new
	switch {
	case r.kind == rowGap:
		line = r.gap
	case r.kind == rowLine && r.new < 0:
		line = -1 - r.old
	case r.kind == rowComment:
		line = r.n
	case r.kind != rowLine:
		line = 0
	}
	return diffAnchor{r.kind, v.files[r.file], line}
}

// focusRowAt returns row i, past the rooms between files, -1 for none.
func (v *diffView) focusRowAt(i int) int {
	if i < 0 || i >= len(v.rows) {
		return -1
	}
	for i+1 < len(v.rows) && v.rows[i].between() {
		i++
	}
	return i
}

// stickyHeader returns the header of the file at the top of a diff's view
// when it scrolled above it, and where it shows instead, in the content:
// at the top of the view, or pushed up by the end of its file. It
// returns -1 when no header shows so.
func (v *diffView) stickyHeader(lineH float32) (int, float32) {
	if len(v.rows) == 0 || len(v.tops) <= len(v.rows) {
		return -1, 0
	}
	top := v.rowAt(v.scroll.Y, lineH)
	r := v.rows[top]
	if r.between() || r.file >= len(v.heads) {
		return -1, 0
	}
	h := v.heads[r.file]
	if editorPadY+float32(v.tops[h])*lineH >= v.scroll.Y {
		return -1, 0
	}
	// The file ends before the room or the header that comes next.
	end := len(v.rows)
	if r.file+1 < len(v.heads) {
		end = v.heads[r.file+1]
		for end > h+1 && v.rows[end-1].between() {
			end--
		}
	}
	return h, min(v.scroll.Y, editorPadY+float32(v.tops[end])*lineH-lineH)
}

// line returns the line a row of the unified layout shows, and whether it
// is of the new side.
func (r *diffRow) line() (int, bool) {
	if r.new >= 0 {
		return r.new, true
	}
	return r.old, false
}

// rowAt returns the row at y, in the diff's content.
func (v *diffView) rowAt(y float32, lineH float32) int {
	if len(v.rows) == 0 {
		return -1
	}
	line := int((y - editorPadY) / lineH)
	i, _ := slices.BinarySearch(v.tops, line+1)
	return min(max(i-1, 0), len(v.rows)-1)
}

// diffFlash is a run of lines of a side of a file of a diff, lo up to hi,
// that just changed: they glow from when they first show, at, and fade.
type diffFlash struct {
	path   string
	side   int
	lo, hi int
	// gen is the diff's generation as the lines were written: they glow
	// once the rows are laid out again with them, from at, and are given
	// up when that does not come soon after waiting.
	gen         int
	at, waiting time.Time
}

// flashFor is how long the lines a diff just wrote glow.
const flashFor = 900 * time.Millisecond

// flashLines has the lines lo up to hi of a side of file f glow, as
// the diff shows them next.
func (v *diffView) flashLines(f *diffFile, side, lo, hi int) {
	if hi <= lo {
		hi = lo + 1 // a removal glows at the line after it
	}
	v.flash = diffFlash{path: f.path, side: side, lo: lo, hi: hi, gen: v.gen}
}

// rowAtStream returns the row whose lines stand at y in one side's
// stream of rows, as rowAt does of the rows aligned.
func (v *diffView) rowAtStream(tops []int, y, lineH float32) int {
	if len(v.rows) == 0 || len(tops) == 0 {
		return -1
	}
	line := int((y - editorPadY) / lineH)
	i, _ := slices.BinarySearch(tops, line+1)
	return min(max(i-1, 0), len(v.rows)-1)
}

// startsIn returns the changes of a diff whose rows may be among rows
// first to last, lo up to hi of starts: the one that starts before first
// and may run into them, and those that start up to last. The frame
// looks at these alone, however many changes the diff has.
func (v *diffView) startsIn(first, last int) (lo, hi int) {
	lo = max(sort.SearchInts(v.starts, first)-1, 0)
	hi = sort.SearchInts(v.starts, last+1)
	return lo, max(lo, hi)
}

// streams reports whether the sides of a split diff stand in streams of
// their own, its rows not aligned across them.
func (a *app) streams(v *diffView) bool {
	return v.split && !a.settings.DiffAlignedRows && len(v.sideTops[0]) == len(v.rows)+1
}

// rightShift is how much lower the right side of a split diff stands in
// its content than the left: as much as it is scrolled less, when it
// scrolls on its own or follows the left in a stream of its own.
func (a *app) rightShift(v *diffView) float32 {
	if v.split && (!a.settings.DiffSyncScroll || a.streams(v)) {
		return v.scroll.Y - v.sideY
	}
	return 0
}

// sideRowY returns where the lines of row i of a side of a diff stand in
// its content, and how tall they are: in the side's own stream when the
// rows do not align, and lower or higher on the right as it is scrolled.
func (a *app) sideRowY(v *diffView, side, i int) (y, h float32) {
	lineH := a.font.lineH
	if a.streams(v) {
		t := v.sideTops[side]
		y, h = editorPadY+float32(t[i])*lineH, float32(t[i+1]-t[i])*lineH
	} else {
		y, h = editorPadY+float32(v.tops[i])*lineH, float32(v.rows[i].h)*lineH
	}
	if side == 1 {
		y += a.rightShift(v)
	}
	return y, h
}

// runClip returns the part of r, in a diff's content, that the run of
// lines row i is in covers, where the sides stand in streams of their
// own: r itself otherwise.
func (a *app) runClip(v *diffView, i int, r ui.Rect) ui.Rect {
	if !a.streams(v) || i < 0 || i >= len(v.runs) {
		return r
	}
	lineH := a.font.lineH
	top := editorPadY + float32(v.runs[i][0])*lineH
	bot := editorPadY + float32(v.runs[i][1])*lineH
	y0, y1 := max(r.Y, top), min(r.Y+r.H, bot)
	return ui.Rect{X: r.X, Y: y0, W: r.W, H: max(0, y1-y0)}
}

// followStreams scrolls the right stream of a split diff whose rows do
// not align so that it follows the left, as JetBrains' sides do: the line
// in the middle of the view on the left stands across from its own on the
// right, a run of changes taking the other side's through it at the pace
// of its own lines. Toward the top of the diff the line followed goes up
// to the top of the view, and toward its end down to the bottom, so that
// the sides start and end together.
func (v *diffView) followStreams(lineH, viewH float32) {
	l, r := v.sideTops[0], v.sideTops[1]
	n := len(v.rows)
	if n == 0 || len(l) != n+1 || len(r) != n+1 {
		return
	}
	end := max(0, 2*editorPadY+float32(v.tops[n])*lineH-viewH)
	off := clamp(min(v.scroll.Y, viewH/2)+viewH/2-min(end-v.scroll.Y, viewH/2), 0, viewH)
	mid := (v.scroll.Y + off - editorPadY) / lineH
	i := sort.Search(n, func(j int) bool { return float32(l[j+1]) > mid })
	i = min(i, n-1)
	from, to := i, i+1
	if row := v.rows[i]; row.changed() {
		for from > 0 && v.rows[from-1].changed() && v.rows[from-1].file == row.file && v.rows[from-1].hunk == row.hunk {
			from--
		}
		for to < n && v.rows[to].changed() && v.rows[to].file == row.file && v.rows[to].hunk == row.hunk {
			to++
		}
	}
	l0, l1, r0, r1 := float32(l[from]), float32(l[to]), float32(r[from]), float32(r[to])
	at := r0
	if l1 > l0 {
		at = r0 + (clamp(mid, l0, l1)-l0)*(r1-r0)/(l1-l0)
	}
	v.sideY = clamp(v.scroll.Y+(at-mid)*lineH, 0, max(0, 2*editorPadY+float32(r[n])*lineH-viewH))
}

// topRow returns the row at the top of a diff's view.
func (v *diffView) topRow(lineH float32) int {
	return v.rowAt(v.scroll.Y+lineH/2, lineH)
}

// focusRow returns the row chosen, or the one at the top of the view.
func (v *diffView) focusRow() int {
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		return v.cursor
	}
	if v.tops == nil || len(v.rows) == 0 {
		return -1
	}
	return min(len(v.rows)-1, max(0, v.topRow(v.lineH())))
}

// lineH is the height of a line of the diff, as it last laid out.
func (v *diffView) lineH() float32 {
	if v.built.font.lineH > 0 {
		return v.built.font.lineH
	}
	return 20
}

// lineAt returns the file of row i and its line of the new side, or the
// one after the nearest above it, for a row of the old side's lines only.
func (v *diffView) lineAt(i int) (string, int) {
	if i < 0 || i >= len(v.rows) {
		return "", 0
	}
	file := v.rows[i].file
	path := v.files[file].path
	switch v.rows[i].kind {
	case rowHunk:
		// The header of a run of changes: the first line of the new side
		// the run changes.
		for k := i + 1; k < len(v.rows) && v.rows[k].kind == rowLine && v.rows[k].file == file; k++ {
			if r := &v.rows[k]; r.changed() && r.new >= 0 {
				return path, r.new
			}
		}
	case rowFile:
		// A file: the line of its first change, folded or not.
		if f := v.files[file]; len(f.hunks) > 0 {
			return path, f.hunks[0].B
		}
	}
	for k := i; k >= 0 && v.rows[k].file == file; k-- {
		if n := v.rows[k].new; n >= 0 {
			if k < i {
				return path, n + 1
			}
			return path, n
		}
	}
	return path, 0
}

// stepHunk goes to the next hunk of a diff, or the previous one for a
// step of -1. At the first or last change of a file it says where it is,
// and only one more step that way crosses into the next file with
// changes, or the last of the previous, as cross says: asking for it,
// going at once, or never leaving the file. At the ends of the diff it
// says so, and stays.
func (v *diffView) stepHunk(step int, cross diffCross) {
	if len(v.starts) == 0 {
		return
	}
	v.anchor = -1
	armed := v.boundary
	v.boundary = nil
	switch {
	case v.current < 0:
		// Nothing chosen yet: either way goes to the first change.
		v.current = 0
	case v.atEdge(v.current, step):
		to := v.crossTarget(v.current, step)
		if to < 0 {
			// No other file has changes that way: it says so, and stays.
			v.boundary = &diffBoundary{step, endOfDiffMessage(step)}
			return
		}
		if cross != crossAlways && !(cross == crossAsk && armed != nil && armed.step == step) {
			v.boundary = &diffBoundary{step, v.edgeMessage(v.fileAt(v.current).path, step, cross)}
			return
		}
		v.current = to
	default:
		v.current += step
	}
	if v.cursor >= 0 {
		v.cursor = v.starts[v.current]
	}
	v.glideRow(v.starts[v.current], jumpThird)
}

// fileAt returns the file the kth change of a diff is in.
func (v *diffView) fileAt(k int) *diffFile {
	return v.files[v.rows[v.starts[k]].file]
}

// atEdge reports whether the kth change of a diff is the last of its
// file for a step of 1, or the first for -1.
func (v *diffView) atEdge(k, step int) bool {
	n := k + step
	return n < 0 || n >= len(v.starts) || v.rows[v.starts[n]].file != v.rows[v.starts[k]].file
}

// crossTarget returns the change a step across files goes to, from the
// kth: the first of the next file with changes, or the last of the
// previous, -1 when none that way has changes.
func (v *diffView) crossTarget(k, step int) int {
	f := v.rows[v.starts[k]].file
	if step > 0 {
		for i := k + 1; i < len(v.starts); i++ {
			if v.rows[v.starts[i]].file != f {
				return i
			}
		}
	} else {
		for i := k - 1; i >= 0; i-- {
			if v.rows[v.starts[i]].file != f {
				return i
			}
		}
	}
	return -1
}

// edgeMessage is what a diff says when its change navigation reaches the
// last, or first, change of a file, as cross has it: asking for one more
// step to cross, or never crossing.
func (v *diffView) edgeMessage(path string, step int, cross diffCross) string {
	name := filepath.Base(path)
	if step > 0 {
		if cross == crossAsk {
			return locales.DiffEdgeLastOfFileAsk(name)
		}
		return locales.DiffEdgeLastOfFile(name)
	}
	if cross == crossAsk {
		return locales.DiffEdgeFirstOfFileAsk(name)
	}
	return locales.DiffEdgeFirstOfFile(name)
}

// endOfDiffMessage is what a diff says when its change navigation
// reaches the end, or the start, of all of it.
func endOfDiffMessage(step int) string {
	if step > 0 {
		return locales.DiffEdgeLastOfDiff()
	}
	return locales.DiffEdgeFirstOfDiff()
}

// stepFile goes to the header of the next file of a diff of every file,
// or the previous one for a step of -1. At the ends of its files it says
// where it is, and jumps no further.
func (v *diffView) stepFile(step int) {
	if len(v.heads) == 0 {
		return
	}
	v.boundary = nil
	at := v.focusRowAt(v.focusRow())
	k := -1
	if step > 0 {
		for i, h := range v.heads {
			if h > at {
				k = i
				break
			}
		}
	} else {
		for i := len(v.heads) - 1; i >= 0; i-- {
			if v.heads[i] < at {
				k = i
				break
			}
		}
	}
	if k < 0 {
		if step > 0 {
			v.boundary = &diffBoundary{step, locales.DiffEdgeLastFile()}
		} else {
			v.boundary = &diffBoundary{step, locales.DiffEdgeFirstFile()}
		}
		return
	}
	v.anchor = -1
	v.cursor = v.heads[k]
	v.glideRow(v.cursor, jumpTop)
}

// jumpRow scrolls to a row in the next frame.
func (v *diffView) jumpRow(i int, place jumpPlace) {
	v.jump, v.jumpTo, v.glide = i, place, false
}

// glideRow is jumpRow for going to a change, a file or a note: the view
// glides there when it is near.
func (v *diffView) glideRow(i int, place jumpPlace) {
	v.jump, v.jumpTo, v.glide = i, place, true
}

// moveCursor moves the chosen row by step rows, past the room between
// files, from the row at the top of the view when none is chosen.
func (v *diffView) moveCursor(step int) {
	v.anchor = -1
	if len(v.rows) == 0 {
		return
	}
	i := v.cursor
	if i < 0 || i >= len(v.rows) {
		i = v.focusRow()
		if !v.rows[i].between() {
			step = 0
		}
	}
	i = min(max(i+step, 0), len(v.rows)-1)
	for v.rows[i].between() {
		next := i + step
		if step == 0 {
			next = i + 1
		}
		if next < 0 || next >= len(v.rows) {
			break
		}
		i = next
	}
	v.setCursor(i)
	v.jumpRow(i, jumpReveal)
}

// setCursor chooses a row, and its hunk as the current one. Moving any
// other way than by stepHunk clears what the navigation said at an end.
func (v *diffView) setCursor(i int) {
	v.cursor = i
	v.boundary = nil
	if i < 0 || i >= len(v.rows) || !v.rows[i].changed() {
		return
	}
	r := v.rows[i]
	for k, s := range v.starts {
		if v.rows[s].file == r.file && v.rows[s].hunk == r.hunk {
			v.current = k
		}
	}
}

// inCurrent reports whether a row is a line of the hunk gone to last.
func (v *diffView) inCurrent(r *diffRow) bool {
	if v.current < 0 || v.current >= len(v.starts) || !r.changed() {
		return false
	}
	s := &v.rows[v.starts[v.current]]
	return s.file == r.file && s.hunk == r.hunk
}

// activateRow does what a click on a row, or Enter, does: opens a gap,
// folds or unfolds a file, or, with open set, opens the file at a line.
func (a *app) activateRow(v *diffView, i int, open bool) {
	if i < 0 || i >= len(v.rows) {
		return
	}
	r := v.rows[i]
	f := v.files[r.file]
	switch r.kind {
	case rowGap:
		a.toggleGap(v, i)
	case rowFile:
		if !f.folded {
			// The lines fold away under the view: it comes back to them
			// when the file unfolds.
			if at := v.focusRowAt(v.topRow(a.font.lineH)); at >= 0 && v.tops != nil && v.rows[at].file == r.file {
				p := v.anchorOf(at)
				v.place = &p
			}
		}
		f.folded = !f.folded
		v.changed()
	case rowLine, rowHunk:
		if open {
			path, line := v.lineAt(i)
			a.openFileAt(path, line, true)
		}
	case rowComment:
		if n := a.review.noteAt(r.n); open && n != nil {
			a.editNote(n, nil)
		}
	}
}

// toggleGap opens the gap of row i, or folds again the gap its line is in.
func (a *app) toggleGap(v *diffView, i int) {
	if i < 0 || i >= len(v.rows) || v.rows[i].gap < 0 {
		return
	}
	r := v.rows[i]
	f := v.files[r.file]
	if f.open == nil {
		f.open = map[int]bool{}
	}
	if r.kind == rowGap {
		f.open[r.gap] = true
	} else {
		delete(f.open, r.gap)
	}
	v.changed()
	// The chosen row stays where the gap was.
	v.cursor = -1
	a.layoutRows(v, a.diffGeometry(v, v.split, v.viewW))
	for k, o := range v.rows {
		if o.file == r.file && (o.gap == r.gap || o.kind == rowGap && o.gap == r.gap) {
			v.setCursor(k)
			break
		}
	}
}

// undoDiff takes back the change an arrow of the diff last put in a
// document, or makes it again for a redo, as the undo of the diff does
// from its keyboard.
func (a *app) undoDiff(v *diffView, redo bool) {
	if e := v.editing; e != nil {
		// The document the diff edits has the keyboard: its history is
		// what the undo and redo of the diff's keyboard take, not what
		// an earlier arrow wrote. Its editor is the diff's own, which no
		// column shows, so the undo goes through it directly.
		e.endCompose()
		if redo {
			e.ed.redo()
		} else {
			e.ed.undo()
		}
		return
	}
	if v.edited == nil {
		return
	}
	d, shown := a.editorOf(v.edited)
	if d == nil {
		return
	}
	cmd := func(d *editor) {
		if redo {
			d.redo()
		} else {
			d.undo()
		}
	}
	if shown {
		a.queueEdit(d, cmd)
	} else {
		cmd(d)
	}
}

// diffInput takes the keys of a diff that has the keyboard: those of a
// pager, as Hunk has them, and, while its writable side is edited, the
// typing and the keys of an editor. The undo and redo of the documents
// its arrows have written remain available from the diff keyboard.
func (a *app) diffInput(v *diffView, ev ui.InputEvent) bool {
	// The box of a note being written takes its keys.
	if a.typingNote() {
		return false
	}
	if ev.Kind == ui.InputKeyDown && v.edited != nil {
		switch m := ev.Mods; {
		case ev.Key == ui.KeyZ && m == ui.Cmd:
			a.undoDiff(v, false)
			return true
		case ev.Key == ui.KeyZ && m == ui.Cmd|ui.Shift, ev.Key == ui.KeyY && m == ui.Ctrl && ui.Cmd == ui.Ctrl:
			a.undoDiff(v, true)
			return true
		}
	}
	// A line of the diff copies as it is, of the side the chosen row
	// shows, while the diff reads as a pager; rows that hold a review
	// note copy as an excerpt.
	if ev.Kind == ui.InputCommand && ev.Text == "copy" && v.editing == nil {
		if s := v.diffChosenText(); s != "" {
			a.services.WriteClipboard(s)
			a.tell(note{message: locales.DiffCopiedLine()})
			return true
		}
		a.copyExcerpt(v)
		return true
	}
	if v.editing != nil {
		if a.diffEditInput(v, ev) {
			return true
		}
		// A key that types, or moves the caret, is not the pager's: the
		// pager pages on, and the keys with cmd go on to the shortcuts.
		if ev.Kind == ui.InputKeyDown && ev.Mods == ev.Mods&ui.Shift &&
			ev.Key != ui.KeyPageDown && ev.Key != ui.KeyPageUp {

			return true
		}
	}
	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
		return false
	}
	shift := ev.Mods&ui.Shift != 0
	page := max(v.viewH-a.font.lineH, a.font.lineH)
	switch ev.Key {
	case ui.KeyJ, ui.KeyDown:
		if shift {
			v.extend(1)
		} else {
			v.moveCursor(1)
		}
	case ui.KeyK, ui.KeyUp:
		if shift {
			v.extend(-1)
		} else {
			v.moveCursor(-1)
		}
	case ui.KeyEscape:
		if v.anchor < 0 {
			return false
		}
		v.anchor = -1
	case ui.KeyC:
		a.commentDiff(v)
	case ui.KeyN:
		// The next card of a note, or with Shift the one before.
		if len(v.cards) == 0 {
			return false
		}
		a.stepNote(1 - 2*btoi(shift))
	case ui.KeyBackspace, ui.KeyDelete:
		// On the card of a note, it deletes the note.
		c := v.cursor
		if c < 0 || c >= len(v.rows) || v.rows[c].kind != rowComment {
			return false
		}
		if n := a.review.noteAt(v.rows[c].n); n != nil {
			a.deleteNote(n)
		}
	case ui.KeySpace:
		if shift {
			a.pageDiff(v, -page)
		} else {
			a.pageDiff(v, page)
		}
	case ui.KeyF, ui.KeyPageDown:
		a.pageDiff(v, page)
	case ui.KeyB, ui.KeyPageUp:
		a.pageDiff(v, -page)
	case ui.KeyD:
		a.pageDiff(v, page/2)
	case ui.KeyU:
		a.pageDiff(v, -page/2)
	case ui.KeyG:
		if shift {
			a.pageDiff(v, 1e9)
		} else {
			a.pageDiff(v, -1e9)
		}
	case ui.KeyHome:
		a.pageDiff(v, -1e9)
	case ui.KeyEnd:
		a.pageDiff(v, 1e9)
	case ui.KeyBracketRight:
		v.stepHunk(1, a.crossesDiff())
	case ui.KeyBracketLeft:
		v.stepHunk(-1, a.crossesDiff())
	case ui.KeyPeriod:
		v.stepFile(1)
	case ui.KeyComma:
		v.stepFile(-1)
	case ui.KeyZ:
		a.toggleGap(v, v.focusRow())
	case ui.KeyEnter:
		a.activateRow(v, v.focusRow(), true)
	case ui.KeyE:
		a.openDiffFile(v)
	case ui.KeyS:
		a.stageAtCursor(v)
	case ui.KeyI:
		// The working tree side of the chosen row takes the keys.
		a.beginDiffEdit(v, v.focusRow())
	default:
		return false
	}
	return true
}

// pageDiff scrolls a diff by dy, and keeps the chosen row in view.
func (a *app) pageDiff(v *diffView, dy float32) {
	lineH := a.font.lineH
	contentH := 2*editorPadY + float32(v.tops[len(v.tops)-1])*lineH
	v.scroll.Y = clamp(v.scroll.Y+dy, 0, max(0, contentH-v.viewH))
	if v.cursor < 0 {
		return
	}
	first := v.rowAt(v.scroll.Y+lineH/2, lineH)
	last := v.rowAt(v.scroll.Y+v.viewH-lineH/2, lineH)
	switch {
	case v.cursor < first:
		v.setCursor(first)
	case v.cursor > last:
		v.setCursor(last)
	}
}

// openPath shows a file, or opens a folder, as a submodule is, as
// `mayoi <dir>` opens it.
func (a *app) openPath(path string, focus bool) {
	if remote.Is(path) {
		if a.tree != nil && a.tree.IsDir(path) {
			a.host.openFolders([]string{path})
		} else {
			a.openFile(path, focus)
		}
		return
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		a.host.openFolders([]string{path})
		return
	}
	a.openFile(path, focus)
}

// openFileAt shows a file with the caret at the start of a line.
func (a *app) openFileAt(path string, line int, focus bool) {
	// The line is in the text, whatever markdownPreview says.
	a.openText(func() { a.openPath(path, focus) })
	if d := a.activeEditor(); d != nil && d.path == path {
		snap := d.buf.Snapshot()
		line = min(max(line, 0), snap.LineCount()-1)
		s, _ := snap.LineRange(line)
		d.selectRange(s, s, true)
	}
}

// diffStatus is what the status bar says of a diff: what the navigation
// said where it stopped, or which change of all it is at, and how many
// lines the diff adds and removes, once every file is read. While its
// writable side is edited, it says so.
func (v *diffView) diffStatus() string {
	if e := v.editing; e != nil {
		return locales.DiffEditingStatus(filepath.Base(e.f.path))
	}
	if v.boundary != nil {
		return v.boundary.msg
	}
	at := ""
	if v.current >= 0 && v.current < len(v.starts) {
		at = locales.DiffChangeOf(strconv.Itoa(v.current+1), strconv.Itoa(len(v.starts))) + " · "
	}
	// Every change to what the files read calls changed.
	if v.summaryGen != v.gen+1 {
		v.summary, v.summaryGen = v.summarize(), v.gen+1
	}
	return at + v.summary
}

func (v *diffView) summarize() string {
	hunks, add, del, files := 0, 0, 0, 0
	all := true
	for _, f := range v.files {
		if !f.loaded {
			all = false
			continue
		}
		ad, de := f.counts()
		hunks, add, del = hunks+len(f.hunks), add+ad, del+de
	}
	// A file both staged and changed since counts once.
	paths := map[string]bool{}
	for _, f := range v.files {
		paths[f.path] = true
	}
	files = len(paths)
	switch {
	case files == 0:
		return ""
	case !all:
		return locales.FilesCount(files)
	case hunks == 0:
		return locales.DiffSummaryNoText(files)
	}
	return locales.DiffSummary(files, hunks, add, del)
}

// diffButtons are the buttons at the end of a diff's breadcrumbs: going
// from hunk to hunk, folding, wrapping, the layout, and opening the file.
func (a *app) diffButtons(c *ui.Context, t *colTab) {
	p := a.pal
	v := t.diff
	button := func(icon *ui.SVG, label string, on bool) ui.Element {
		b := ui.ButtonBase(c.Key(label)).Height(crumbsH-4).Padding(0, 6).Radius(4).Shrink(0).
			AlignItems(ui.Center).Label(label).Tooltip(label)
		switch {
		case on:
			b.Background(p.active)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(p.muted) })
		return b
	}
	act := func(icon *ui.SVG, label string, fn func()) {
		if button(icon, label, false).Clicked() {
			a.post(fn)
		}
	}
	a.reviewSendButton(c)
	if v.commit != nil {
		if button(iconListTree, a.withKey(locales.DiffToggleCommitFiles(), "git.toggleCommitFiles"), a.window.CommitFilesOpen).Clicked() {
			a.post(a.toggleCommitFiles)
		}
	}
	act(iconArrowUp, locales.DiffPrevChange(), func() { v.stepHunk(-1, a.crossesDiff()) })
	act(iconArrowDown, locales.DiffNextChange(), func() { v.stepHunk(1, a.crossesDiff()) })
	act(iconChevronLeft, locales.DiffPrevFile(), func() { v.stepFile(-1) })
	act(iconChevronRight, locales.DiffNextFile(), func() { v.stepFile(1) })
	if button(iconUnfold, locales.DiffShowFullFiles(), v.full).Clicked() {
		a.post(func() { a.setDiffFull(v, !v.full) })
	}
	if button(iconWrap, a.withKey(locales.DiffToggleWrap(), "view.wordWrapTab"), a.wrapsDiff(v)).Clicked() {
		a.post(func() { a.toggleDiffWrap(v) })
	}
	// What a line that changed shows of itself: the words, the characters
	// within them, or no more than the line.
	b := button(iconHighlight, locales.DiffHighlightMenu(), false)
	b.Menu(func(m *ui.Menu) {
		for _, h := range []struct {
			mode  diffHighlight
			label string
		}{{highlightLine, locales.DiffHighlightLines()}, {highlightWord, locales.DiffHighlightWords()}, {highlightChar, locales.DiffHighlightChars()}} {
			if m.Item(h.label).Checked(a.diffHighlightOf() == h.mode).Chosen() {
				a.post(func() { a.setDiffHighlight(h.mode) })
			}
		}
	})
	// Whitespace filtering changes comparison and navigation only; the
	// original file text remains what block actions edit.
	b = button(iconWhitespace, locales.DiffWhitespaceMenu(), false)
	b.Menu(func(m *ui.Menu) {
		for _, w := range []struct {
			mode  diffWhitespace
			label string
		}{{whitespaceExact, locales.WhitespaceExact()}, {whitespaceEOL, locales.WhitespaceIgnoreEOL()}, {whitespaceAll, locales.WhitespaceIgnoreAll()}, {whitespaceBlank, locales.WhitespaceIgnoreBlank()}} {
			if m.Item(w.label).Checked(a.diffWhitespaceOf() == w.mode).Chosen() {
				a.post(func() { a.setDiffWhitespace(w.mode) })
			}
		}
	})
	// The unchanged lines around a change, which the rest folds down to.
	b = button(iconContext, locales.DiffContextMenu(), false)
	b.Menu(func(m *ui.Menu) {
		for _, n := range []int{0, 1, 2, 3, 5, 10} {
			label := locales.DiffContextNone()
			if n > 0 {
				label = locales.DiffContextLines(n)
			}
			if m.Item(label).Checked(a.diffContextOf() == n).Chosen() {
				a.post(func() { a.setDiffContext(n) })
			}
		}
	})
	icon := iconRows
	if v.split {
		icon = iconColumns
	}
	b = button(icon, locales.DiffLayout(), false)
	b.Menu(func(m *ui.Menu) {
		for _, l := range []struct {
			layout diffLayout
			label  string
		}{{layoutAuto, locales.DiffLayoutAuto()}, {layoutSplit, locales.DiffLayoutSplit()}, {layoutInline, locales.DiffLayoutInline()}} {
			if m.Item(l.label).Checked(v.layout == l.layout).Chosen() {
				a.post(func() { a.setDiffLayout(v, l.layout) })
			}
		}
		if m.Item(locales.DiffSyncScrollMenu()).Checked(a.settings.DiffSyncScroll).Chosen() {
			a.post(func() { a.setDiffSync(v, !a.settings.DiffSyncScroll) })
		}
		if m.Item(locales.DiffAlignRowsMenu()).Checked(a.settings.DiffAlignedRows).Chosen() {
			a.post(func() { a.setDiffAligned(!a.settings.DiffAlignedRows) })
		}
	})
	// Editing the working tree side of the file of the chosen row, which
	// the I key does too.
	if f := a.diffEditTarget(v); f != nil {
		editing := v.editing != nil && v.editing.f == f
		label := locales.DiffEditButton()
		if editing {
			label = locales.DiffEditButtonActive()
		}
		b = button(iconPencil, label, editing)
		b.Tooltip(locales.DiffEditTip())
		if b.Clicked() {
			a.post(func() {
				if e := v.editing; e != nil && e.f == f {
					a.endDiffEdit(v)
					return
				}
				a.beginDiffEdit(v, v.focusRow())
			})
		}
	}
	act(iconOpenFile, locales.DiffOpenFile(), func() { a.openDiffFile(v) })
}

// setDiffLayout shows a diff side by side, inline, or as wide as it is,
// as the next diffs opened will be.
func (a *app) setDiffLayout(v *diffView, l diffLayout) {
	if v.layout == l {
		return
	}
	v.layout = l
	a.settings.DiffLayout = l.key()
	a.settings.Changed()
	a.rememberTabs()
	a.keepTopRow(v, func() { v.split = a.splits(v, v.viewW) })
	if !a.settings.DiffSyncScroll {
		// The right side starts where the sides showed together.
		v.sideY = v.scroll.Y
	}
}

// setDiffFull shows every unchanged line of a diff, or folds them again.
func (a *app) setDiffFull(v *diffView, full bool) {
	a.keepTopRow(v, func() { v.full = full })
}

// setDiffContext shows n unchanged lines of a diff around each change,
// as the next diffs opened will. The diffs lay their rows out again with
// the line at the top of the view where it was.
func (a *app) setDiffContext(n int) {
	n = min(max(n, 0), settings.MaxDiffContext)
	if a.settings.DiffContext == n {
		return
	}
	a.settings.DiffContext = n
	a.settings.Changed()
	a.invalidate()
}

// setDiffHighlight shows what changed within a line of a diff by word,
// by character, or no more than the lines, as the next diffs opened will.
// What it shows is only so: the changes a diff acts on are its hunks of
// lines.
func (a *app) setDiffHighlight(h diffHighlight) {
	if a.diffHighlightOf() == h {
		return
	}
	a.settings.DiffHighlight = h.key()
	a.settings.Changed()
	a.invalidate()
}

// setDiffWhitespace changes only the comparison keys. Re-reading the sides
// rebuilds hunks and rows while leaving the source text and document state
// untouched.
func (a *app) setDiffWhitespace(w diffWhitespace) {
	if a.diffWhitespaceOf() == w {
		return
	}
	a.settings.DiffWhitespace = w.key()
	a.settings.Changed()
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.diff == nil {
				continue
			}
			for _, f := range t.diff.files {
				f.loaded = false
				f.gitStale = true
			}
			t.diff.changed()
		}
	}
	a.invalidate()
}

// setDiffSync has the sides of a side-by-side diff scroll together, or
// each on its own, as the next diffs opened will. Together again, the
// line the right side was at shows across from its counterpart, as the
// sides line up.
func (a *app) setDiffAligned(on bool) {
	if a.settings.DiffAlignedRows == on {
		return
	}
	a.settings.DiffAlignedRows = on
	a.settings.Changed()
	a.invalidate()
}

func (a *app) setDiffSync(v *diffView, on bool) {
	if a.settings.DiffSyncScroll == on {
		return
	}
	a.settings.DiffSyncScroll = on
	a.settings.Changed()
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.diff != nil {
				a.resyncDiff(t.diff, on)
			}
		}
	}
	a.invalidate()
}

// resyncDiff starts or stops the sides of a diff scrolling on their own.
func (a *app) resyncDiff(v *diffView, on bool) {
	if !v.split {
		return
	}
	if !on {
		// The sides start where they showed together.
		v.sideY = v.scroll.Y
		return
	}
	// The row at the top of the right side comes to the top, across from
	// its counterpart.
	if len(v.rows) > 0 {
		if i := v.rowAt(v.sideY+a.font.lineH/2, a.font.lineH); i >= 0 {
			v.jumpRow(i, jumpTop)
		}
	}
}

// toggleDiffWrap wraps the lines of a diff, or stops, until the settings'
// Word Wrap changes.
func (a *app) toggleDiffWrap(v *diffView) {
	a.keepTopRow(v, func() {
		w := !a.wrapsDiff(v)
		v.wrap, v.wrapGen = &w, a.settings.WrapGen
	})
}

// keepTopRow changes how a diff lays out its rows, keeping the line at
// the top of its view, or the chosen one, where it was.
func (a *app) keepTopRow(v *diffView, change func()) {
	lineH := a.font.lineH
	chosen := v.cursor
	at := v.focusRowAt(v.focusRow())
	ok := at >= 0 && v.tops != nil
	var want diffAnchor
	var offset float32
	if ok {
		want = v.anchorOf(at)
		offset = editorPadY + float32(v.tops[at])*lineH - v.scroll.Y
	}
	change()
	v.changed()
	if !ok {
		return
	}
	a.layoutRows(v, a.diffGeometry(v, v.split, v.viewW))
	for i := range v.rows {
		if v.anchorOf(i) == want {
			v.scroll.Y = max(0, editorPadY+float32(v.tops[i])*lineH-offset)
			if chosen >= 0 {
				v.cursor = i
			}
			return
		}
	}
}

// diffsChanged has the diffs read the sides Git keeps again.
func (a *app) diffsChanged(roots ...string) {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.diff != nil && t.diff.commit == nil && (len(roots) == 0 || a.diffBelongs(t.diff, roots[0])) {
				for _, f := range t.diff.files {
					f.gitStale = true
				}
			}
		}
	}
}

// diffsOnDisk has the diffs of files changed on disk read them again.
func (a *app) diffsOnDisk(changed []string) {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.diff != nil && t.diff.commit == nil {
				for _, f := range t.diff.files {
					if slices.Contains(changed, f.path) {
						f.diskStale = true
					}
				}
			}
		}
	}
}

// crossesDiff is how the settings have a diff's change navigation cross
// into another file at the ends of one.
func (a *app) crossesDiff() diffCross { return parseDiffCross(a.settings.DiffCrossFiles) }

// stepChange goes to the next change of the tab shown, or the previous
// one: a diff's hunk, or the lines of an editor changed since the last
// commit.
func (a *app) stepChange(step int) {
	t := a.activeTab()
	switch {
	case t == nil:
	case t.diff != nil:
		t.diff.stepHunk(step, a.crossesDiff())
	case t.ed != nil:
		a.queueEdit(t.ed, func(d *editor) { a.stepEditorHunk(d, step) })
	}
}

// stepDiffFile goes to the next or previous file in the active diff tab.
func (a *app) stepDiffFile(step int) {
	if t := a.activeTab(); t != nil && t.diff != nil {
		t.diff.stepFile(step)
	}
}

// expandTabs returns a line with its tabs as spaces to the next stop, and
// where each byte of the line went in it.
func expandTabs(s string) (string, func(b int) int) {
	if strings.IndexByte(s, '\t') < 0 {
		return s, func(b int) int { return b }
	}
	var out bytes.Buffer
	at := make([]int, len(s)+1)
	col := 0
	for i := 0; i < len(s); {
		at[i] = out.Len()
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == '\t' {
			k := tabWidth - col%tabWidth
			out.WriteString(strings.Repeat(" ", k))
			col += k
		} else {
			out.WriteString(s[i : i+n])
			col++
		}
		for j := 1; j < n; j++ {
			at[i+j] = out.Len()
		}
		i += n
	}
	at[len(s)] = out.Len()
	return out.String(), func(b int) int { return at[min(max(b, 0), len(s))] }
}

// errorLine is the first line of what err says, without git's "fatal: ":
// a file's note in the diff is one row tall.
func errorLine(err error) string {
	s, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	return strings.TrimPrefix(s, "fatal: ")
}
