package main

import (
	"bytes"
	"errors"
	"fmt"
	"mayoi/internal/remotefs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
)

// diffBase is what a diff compares: which version of a file with which.
type diffBase int

const (
	diffHead   diffBase = iota // HEAD with the work tree
	diffIndex                  // the index with the work tree: the changes not staged
	diffStaged                 // HEAD with the index: the changes staged
	diffCommit                 // a commit with its first parent
)

// sides names the two versions a diff compares.
func (b diffBase) sides() (old, new string) {
	switch b {
	case diffIndex:
		return "Index", "Working Tree"
	case diffStaged:
		return "HEAD", "Index"
	}
	return "HEAD", "Working Tree"
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
	path   string
	base   diffBase
	group  scmGroup
	entry  git.Entry
	letter byte // its letter in the source control view

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
	// compared last.
	gitStale, diskStale bool
	docVersion          uint64
	running, waiting    bool
	started             time.Time

	words map[[2]int][2][]linediff.Span
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
	layout  diffLayout
	split   bool // as the layout and the width chose last
	full    bool // every unchanged line shows
	wrap    *bool
	wrapGen int

	files []*diffFile
	// status is the status of the repository the files were listed from.
	status *git.Status
	// commit is the commit whose changes the tab shows, from its first
	// parent; nil for the tab of the changes of the work tree.
	commit *commitDiff

	// rows are the rows of the files as they show, and tops where each
	// starts, in lines, and after the last, the lines of all. starts are
	// the first rows of each hunk, and heads the headers of each file.
	rows          []diffRow
	tops          []int
	starts, heads []int
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

	scroll ui.ScrollState
	pane   ui.Handle
	// current is the hunk gone to last, of starts, and cursor the row
	// chosen, -1 for none. jump is a row to scroll to in the next frame,
	// -1 for none, as jumpTo says.
	current, cursor int
	// acted is set when the last click opened a gap or folded a file.
	acted        bool
	jump         int
	jumpTo       jumpPlace
	viewW, viewH float32

	// summary is what the status bar says of the diff, worked out for the
	// generation summaryGen, plus one.
	summary    string
	summaryGen int
}

// commitDiff is the commit a diff shows, and the files it changed, read
// in the background.
type commitDiff struct {
	hash            string
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
	cols              [2]int
	card              int // how many lines tall the card of a commit is
	font              codeFont
}

// jumpPlace is where a row jumped to shows.
type jumpPlace int

const (
	jumpThird  jumpPlace = iota // a third down the view, unless in view
	jumpTop                     // at the top
	jumpReveal                  // as little scrolled as shows it
)

func newDiffView(layout diffLayout) *diffView {
	return &diffView{layout: layout, current: -1, cursor: -1, jump: -1, near: [2]int{0, diffNear}}
}

// newCommitView makes the tab of a commit's changes, its message read
// already in info when its hash is set.
func newCommitView(layout diffLayout, hash string, info git.Commit) *diffView {
	v := newDiffView(layout)
	v.commit = &commitDiff{hash: hash, info: info}
	return v
}

func (v *diffView) name() string {
	if c := v.commit; c != nil {
		if c.info.Subject != "" {
			return c.info.Subject
		}
		return "Commit " + git.ShortHash(c.hash)
	}
	return "Changes"
}

// same reports whether two diffs show the same changes: both those of
// the work tree, or of one commit.
func (v *diffView) same(o *diffView) bool {
	if v.commit == nil || o.commit == nil {
		return v.commit == nil && o.commit == nil
	}
	return v.commit.hash == o.commit.hash
}

// stop lets go of the highlighters.
func (v *diffView) stop() {
	for _, f := range v.files {
		f.stop()
	}
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

// changesTab returns the tab of the changes and its column, nil when no
// column has one.
func (a *app) changesTab() (*column, int) {
	for _, col := range a.cols {
		for i, t := range col.tabs {
			if t.diff != nil && t.diff.commit == nil {
				return col, i
			}
		}
	}
	return nil, -1
}

// openChangesTab shows the tab of the changes, made after the tab shown
// of the column that has the keyboard when there is none, and returns its
// diff.
func (a *app) openChangesTab(focus bool) *diffView {
	if col, i := a.changesTab(); col != nil {
		a.activate(col, i, focus)
		return col.tabs[i].diff
	}
	col := a.column()
	at := min(col.active+1, len(col.tabs))
	v := newDiffView(parseDiffLayout(a.settings.DiffLayout))
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, focus)
	return v
}

// showChanges shows the tab of the changes scrolled to a file, or with no
// path, to the section of a base, once the file is listed.
func (a *app) showChanges(target diffTarget, focus bool) {
	v := a.openChangesTab(focus)
	v.reveal = &target
	v.changed()
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
		a.showChanges(diffTarget{path: t.path(), any: true}, true)
	}
}

// openDiffFile opens the file of a diff's chosen row, or of the row at the
// top of its view, at its line.
func (a *app) openDiffFile(v *diffView) {
	if path, line := v.lineAt(v.focusRow()); path != "" {
		a.openFileAt(path, line, true)
	}
}

// showingDiff returns the place of a column's tab that shows the changes
// v shows, -1 when it has none.
func (col *column) showingDiff(v *diffView) int {
	return slices.IndexFunc(col.tabs, func(t *colTab) bool { return t.diff != nil && t.diff.same(v) })
}

// commitTab returns the tab of a commit's changes and its column, nil
// when no column has one.
func (a *app) commitTab(hash string) (*column, int) {
	for _, col := range a.cols {
		for i, t := range col.tabs {
			if t.diff != nil && t.diff.commit != nil && t.diff.commit.hash == hash {
				return col, i
			}
		}
	}
	return nil, -1
}

// showCommit shows the changes of a commit in a tab of its own, made after
// the tab shown of the column that has the keyboard when there is none.
func (a *app) showCommit(c git.Commit, focus bool) {
	if col, i := a.commitTab(c.Hash); col != nil {
		a.keepTab(col.tabs[i])
		a.activate(col, i, focus)
		return
	}
	col := a.column()
	at := min(col.active+1, len(col.tabs))
	a.addTab(col, at, &colTab{diff: newCommitView(parseDiffLayout(a.settings.DiffLayout), c.Hash, c)})
	a.activate(col, at, focus)
	a.rememberTabs()
}

// previewCommit shows the changes of a commit in the preview tab of the
// column that has the keyboard, without giving it the keyboard, unless a
// tab shows them already.
func (a *app) previewCommit(c git.Commit) {
	if col, i := a.commitTab(c.Hash); col != nil {
		a.activate(col, i, false)
		return
	}
	col := a.column()
	a.previewIn(col, func(at int) {
		a.addTab(col, at, &colTab{diff: newCommitView(parseDiffLayout(a.settings.DiffLayout), c.Hash, c), transient: true})
		a.activate(col, at, false)
	})
}

// diffDoc returns the open document of a file of a diff, whose text is
// the work tree's side, nil when it is not open or the file's changes
// are those staged.
func (a *app) diffDoc(f *diffFile) *document {
	if f.base == diffStaged || f.base == diffCommit {
		return nil
	}
	if i := a.docIndex(f.path); i >= 0 {
		if a.docs[i].remoteLoading || a.docs[i].remoteLoadError != "" {
			return nil
		}
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
	st := a.git.status
	if st == v.status {
		return
	}
	v.status = st
	groups := a.groupEntries()
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
			v.files = append(v.files, newDiffFile(l.e, l.group))
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
	r := a.git.repo
	if cm.loaded || cm.loading || r == nil {
		return
	}
	cm.loading = true
	hash, info := cm.hash, cm.info
	go func() {
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
			for _, cf := range files {
				f := &diffFile{path: cf.Path, base: diffCommit, group: groupCommits, letter: cf.Status, gitStale: true}
				f.entry = git.Entry{Path: cf.Path, Orig: cf.Orig, X: cf.Status, Y: '.'}
				v.files = append(v.files, f)
			}
			if a.tree != nil {
				a.rememberTabs()
			}
			v.changed()
		})
	}()
}

// refreshDiff compares the sides of the files of a diff near its view
// again in the background, where one may have changed: the others wait
// until they come near it.
func (a *app) refreshDiff(v *diffView) {
	a.listDiffFiles(v)
	reading := 0
	for _, f := range v.files {
		if f.running {
			reading++
		}
	}
	lo, hi := max(v.near[0], 0), min(v.near[1], len(v.files))
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
	r := a.git.repo
	if f.running || f.waiting || r == nil {
		return false
	}
	doc := a.diffDoc(f)
	var snap ui.TextSnapshot
	edited := false
	if doc != nil {
		snap = doc.buf.Snapshot()
		edited = snap.Version() != f.docVersion
	}
	if f.loaded && !f.gitStale && !edited && !(f.diskStale && doc == nil) {
		return false
	}
	if wait := diffViewPause - time.Since(f.started); f.loaded && !f.gitStale && wait > 0 {
		f.waiting = true
		time.AfterFunc(wait, func() { a.post(func() { f.waiting = false }) })
		return false
	}
	f.running, f.started = true, time.Now()
	readGit, readDisk := f.gitStale || !f.loaded, doc == nil && (f.diskStale || !f.loaded)
	f.gitStale, f.diskStale = false, false
	if doc != nil {
		f.docVersion = snap.Version()
	}
	old, new := f.old, f.new
	path, orig, base := f.path, f.entry.Orig, f.base
	sub, dirty := f.entry.Submodule(), f.entry.Dirty()
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
	go func() {
		var err error
		read := func(rev, path string) (string, bool) {
			text, ok, e := r.Show(rev, path)
			if e != nil && err == nil {
				err = e
			}
			return text, ok
		}
		if readGit {
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
		var hunks []linediff.Hunk
		if !binary {
			hunks = linediff.Hunks(oldLines, newLines)
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
				f.err = err.Error()
			} else {
				f.err = ""
			}
			first := !f.loaded
			f.loaded, f.binary, f.hunks, f.widest = true, binary, hunks, widest
			f.unified, f.pairs = unified, pairs
			f.words = nil
			setDiffSide(&f.old, old, oldLines)
			setDiffSide(&f.new, new, newLines)
			if add, del := f.counts(); first && add+del > foldedLines {
				f.folded = true
			}
			v.changed()
		})
	}()
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
	b := diffBuild{gen: v.gen, split: v.split, wrap: a.wrapsDiff(v), full: v.full, font: a.font}
	if v.commit != nil {
		b.card = v.commit.lines
	}
	if b.wrap {
		b.cols = l.cols
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
	chosen := diffAnchor{}
	if v.cursor >= 0 && v.cursor < len(v.rows) && v.tops != nil {
		chosen = v.anchorOf(v.cursor)
	}
	// The file to reveal shows its lines.
	var target *diffFile
	t := v.reveal
	if t != nil && v.status != nil {
		v.reveal = nil
		if t.path != "" {
			for _, f := range v.files {
				if t.matches(f) && (target == nil || f.base == diffIndex) {
					target = f
				}
			}
			if target == nil {
				a.fail("%s has no changes.", filepath.Base(t.path))
			} else {
				target.folded = false
			}
		}
	}
	v.built = b
	v.rows, v.starts, v.heads = v.rows[:0], v.starts[:0], v.heads[:0]
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
			note = "Loading…"
		case f.err != "":
			note = f.err
		case f.binary:
			note = "The file is binary."
		case !f.old.ok && !f.new.ok:
			note = "Neither version has the file."
		case len(f.hunks) == 0:
			note = "No changes in the text."
		}
		if note != "" {
			plain(rowNote, i, note)
			continue
		}
		all := f.unified
		if v.split {
			all = f.pairs
		}
		v.rows = foldRows(v.rows, f, i, all, v.full)
	}
	for i := range v.rows {
		r := &v.rows[i]
		if r.changed() && (i == 0 || v.rows[i-1].hunk != r.hunk || v.rows[i-1].file != r.file || !v.rows[i-1].changed()) {
			v.starts = append(v.starts, i)
		}
		if r.kind != rowCommit {
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
	r := v.rows[i]
	line := r.new
	switch {
	case r.kind == rowGap:
		line = r.gap
	case r.kind == rowLine && r.new < 0:
		line = -1 - r.old
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
// step of -1, round the ends.
func (v *diffView) stepHunk(step int) {
	if len(v.starts) == 0 {
		return
	}
	if v.current < 0 && step < 0 {
		v.current = 0
	}
	v.current = ((v.current+step)%len(v.starts) + len(v.starts)) % len(v.starts)
	if v.cursor >= 0 {
		v.cursor = v.starts[v.current]
	}
	v.jumpRow(v.starts[v.current], jumpThird)
}

// stepFile goes to the header of the next file of a diff of every file,
// or the previous one for a step of -1.
func (v *diffView) stepFile(step int) {
	if len(v.heads) == 0 {
		return
	}
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
		return
	}
	v.cursor = v.heads[k]
	v.jumpRow(v.cursor, jumpTop)
}

// jumpRow scrolls to a row in the next frame.
func (v *diffView) jumpRow(i int, place jumpPlace) {
	v.jump, v.jumpTo = i, place
}

// moveCursor moves the chosen row by step rows, past the room between
// files, from the row at the top of the view when none is chosen.
func (v *diffView) moveCursor(step int) {
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

// setCursor chooses a row, and its hunk as the current one.
func (v *diffView) setCursor(i int) {
	v.cursor = i
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
		f.folded = !f.folded
		v.changed()
	case rowLine, rowHunk:
		if open {
			path, line := v.lineAt(i)
			a.openFileAt(path, line, true)
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

// diffInput takes the keys of a diff that has the keyboard: those of a
// pager, as Hunk has them.
func (a *app) diffInput(v *diffView, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
		return false
	}
	shift := ev.Mods&ui.Shift != 0
	page := max(v.viewH-a.font.lineH, a.font.lineH)
	switch ev.Key {
	case ui.KeyJ, ui.KeyDown:
		v.moveCursor(1)
	case ui.KeyK, ui.KeyUp:
		v.moveCursor(-1)
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
		v.stepHunk(1)
	case ui.KeyBracketLeft:
		v.stepHunk(-1)
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
	a.openPathThen(path, focus, nil)
}
func (a *app) openPathThen(path string, focus bool, done func()) {
	if remotefs.IsRemote(path) {
		generation, col := a.remoteGeneration, a.column()
		a.tell(note{message: "Opening " + filepath.Base(path) + "…"})
		go func() {
			info, err := remotefs.Stat(path)
			a.post(func() {
				if generation != a.remoteGeneration || !slices.Contains(a.cols, col) {
					return
				}
				if err != nil {
					a.fail("Could not open %s: %v", filepath.Base(path), err)
					return
				}
				if info.IsDir() {
					a.host.openFolders([]string{path})
					return
				}
				a.openFileIn(col, col.active+1, path, focus)
				if done != nil {
					done()
				}
			})
		}()
		return
	}
	if info, err := remotefs.Stat(path); err == nil && info.IsDir() {
		a.host.openFolders([]string{path})
		return
	}
	a.openFile(path, focus)
	if done != nil {
		done()
	}
}

// A search or diff jump waits for the requested file, without moving another tab.
func (a *app) openFileAt(path string, line int, focus bool) {
	a.openPathThen(path, focus, func() {
		i := a.docIndex(path)
		if i < 0 {
			return
		}
		d := a.docs[i]
		jump := func() {
			ed := d.main()
			if ed == nil {
				return
			}
			snap := d.buf.Snapshot()
			target := min(max(line, 0), snap.LineCount()-1)
			offset, _ := snap.LineRange(target)
			ed.selectRange(offset, offset, true)
		}
		if d.remoteLoading {
			d.remoteRestores = append(d.remoteRestores, jump)
		} else {
			jump()
		}
	})
}

// diffStatus is what the status bar says of a diff: how many lines it
// adds and removes, and in how many hunks and files, once every file is
// read.
func (v *diffView) diffStatus() string {
	// Every change to what the files read calls changed.
	if v.summaryGen != v.gen+1 {
		v.summary, v.summaryGen = v.summarize(), v.gen+1
	}
	return v.summary
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
		return plural(files, "file")
	case hunks == 0:
		return plural(files, "file") + " · no changes in the text"
	}
	return fmt.Sprintf("%s · %s · +%d −%d", plural(files, "file"), plural(hunks, "change"), add, del)
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
		b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(p.muted) })
		return b
	}
	act := func(icon *ui.SVG, label string, fn func()) {
		if button(icon, label, false).Clicked() {
			a.post(fn)
		}
	}
	act(iconArrowUp, "Previous Change ([)", func() { v.stepHunk(-1) })
	act(iconArrowDown, "Next Change (])", func() { v.stepHunk(1) })
	if button(iconUnfold, "Show Full Files", v.full).Clicked() {
		a.post(func() { a.setDiffFull(v, !v.full) })
	}
	if button(iconWrap, "Toggle Word Wrap (⌥Z)", a.wrapsDiff(v)).Clicked() {
		a.post(func() { a.toggleDiffWrap(v) })
	}
	icon := iconRows
	if v.split {
		icon = iconColumns
	}
	b := button(icon, "Layout", false)
	b.Menu(func(m *ui.Menu) {
		for _, l := range []struct {
			layout diffLayout
			label  string
		}{{layoutAuto, "Side by Side When Wide"}, {layoutSplit, "Side by Side"}, {layoutInline, "Inline"}} {
			if m.Item(l.label).Checked(v.layout == l.layout).Chosen() {
				a.post(func() { a.setDiffLayout(v, l.layout) })
			}
		}
	})
	act(iconOpenFile, "Open File (E)", func() { a.openDiffFile(v) })
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
}

// setDiffFull shows every unchanged line of a diff, or folds them again.
func (a *app) setDiffFull(v *diffView, full bool) {
	a.keepTopRow(v, func() { v.full = full })
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
func (a *app) diffsChanged() {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.diff != nil && t.diff.commit == nil {
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

// stepChange goes to the next change of the tab shown, or the previous
// one: a diff's hunk, or the lines of an editor changed since the last
// commit.
func (a *app) stepChange(step int) {
	t := a.activeTab()
	switch {
	case t == nil:
	case t.diff != nil:
		t.diff.stepHunk(step)
	case t.ed != nil:
		a.queueEdit(t.ed, func(d *editor) { a.stepEditorHunk(d, step) })
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
