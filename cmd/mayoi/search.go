package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

const (
	// maxSearchMatches is the most matches search finds in all the files.
	maxSearchMatches = 20000
	// maxSearchBytes is the size of a file past which search passes it by.
	maxSearchBytes = 4 << 20
	// searchPause is how long after typing stops a search starts.
	searchPause = 150 * time.Millisecond
	// previewBytes is how much of its line a match keeps on either side of
	// it, to show.
	previewBytes = 240
)

// searchMatch is a match of search in a file. Its offsets are of the
// file's text with its lines ended by "\n", as a document holds it.
type searchMatch struct {
	start, end int    // byte offsets in the text
	text       string // the text matched
	line, col  int    // where it starts: its line, and runes into it, from 0
	// pre, hit and post are its line before it, the part of it on its
	// line, and the line after it, as far as they show.
	pre, hit, post string
}

// searchFile is a file search found matches in.
type searchFile struct {
	path    string // absolute
	rel     string // relative to the root, with slashes
	matches []searchMatch
}

// searchRow is a row of the results: a file's header when match is -1,
// else a match of it.
type searchRow struct{ file, match int }

// searchState is the search view of the sidebar: its query, options and
// results.
type searchState struct {
	query, with            string
	include, exclude       string
	matchCase, word, regex bool
	replacing, details     bool

	input ui.Handle
	list  ui.ListState
	row   int
	// toList moves the keyboard to the results once the list is built,
	// which Focus asks of it.
	toList bool

	// key is the query and options the results are of, or that a search
	// waits to start for; typedAt is when they changed, and waiting is set
	// until the search starts.
	key     string
	typedAt time.Time
	waiting bool
	// re finds the matches of the results, nil for none; gen counts the
	// searches, so that what an old one finds is dropped, and cancel stops
	// the one under way.
	re      *regexp.Regexp
	fold    foldQuery
	gen     int
	cancel  *atomic.Bool
	running bool
	err     string
	files   []*searchFile // by rel
	count   int           // the matches of files
	stopped bool          // the search stopped at maxSearchMatches
	// includes and excludes are the globs of the files to include and to
	// exclude the results are of.
	includes, excludes workspace.Globs
	// seen is the version of each open document's text the results are
	// of, which a document edited searches again.
	seen map[string]uint64
	// watched are the folders of results watched for changes, which the
	// tree does not watch unless they are open in it.
	watched map[string]bool

	collapsed map[string]bool // the files folded, by rel
	rows      []searchRow
	// version counts the changes of files and collapsed, and rowsOf is the
	// version rows were made for.
	version, rowsOf int
}

// searchKey is what a search's results depend on: the query and its
// options, and the files it looks in.
func (s *searchState) searchKey() string {
	return fmt.Sprintf("%v|%v|%v|%s|%s|%s", s.matchCase, s.word, s.regex, s.include, s.exclude, s.query)
}

// changed says the results changed, so their rows are made again.
func (s *searchState) changed() { s.version++ }

// showSearch shows search in the sidebar, with the keyboard in its query,
// which becomes the selection's text when it is on one line.
func (a *app) showSearch(replace bool) {
	s := &a.inFiles
	if d := a.activeEditor(); d != nil {
		snap := d.buf.Snapshot()
		if from, to := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1]); from != to {
			if text := snap.Slice(from, min(to, snap.Len())); !strings.Contains(text, "\n") && len(text) < 200 {
				s.query = text
			}
		}
	}
	s.replacing = s.replacing || replace
	a.window.SidebarClosed, a.window.SidebarView = false, "search"
	a.settings.Changed()
	s.input.Focus()
}

// resetSearch forgets the results, as for another folder, keeping the
// query to search it again.
func (a *app) resetSearch() {
	s := &a.inFiles
	if s.cancel != nil {
		s.cancel.Store(true)
	}
	s.gen++
	s.key, s.re, s.files, s.count, s.running, s.stopped, s.err = "", nil, nil, 0, false, false, ""
	s.seen, s.watched = nil, nil
	s.changed()
}

// startSearch searches the files of the index for the query, in the
// background, dropping the results of the last search.
func (a *app) startSearch() {
	s := &a.inFiles
	s.key, s.waiting = s.searchKey(), false
	if s.cancel != nil {
		s.cancel.Store(true)
	}
	s.gen++
	s.re, s.files, s.count, s.running, s.stopped, s.err = nil, nil, 0, false, false, ""
	s.changed()
	if s.query == "" || a.root == "" {
		return
	}
	re, err := compileQuery(s.query, s.matchCase, s.word, s.regex)
	if err != nil {
		s.err = locales.FindInvalid()
		return
	}
	a.indexStale = a.indexStale || a.index == nil
	a.refreshIndex()
	if a.index == nil {
		// Once the files are listed.
		s.waiting, s.running = true, true
		return
	}
	s.re, s.fold = re, foldQueryOf(s.query, s.matchCase, s.word, s.regex)
	s.includes, s.excludes = workspace.ParseGlobs(s.include), workspace.ParseGlobs(s.exclude)
	var rels []string
	for _, rel := range a.index {
		if s.wants(rel) {
			rels = append(rels, rel)
		}
	}
	// Open documents are searched as they are, edits and all.
	s.seen = map[string]uint64{}
	texts := map[string]string{}
	for _, d := range a.docs {
		if d.path == "" {
			continue
		}
		s.seen[d.path] = d.buf.Version()
		if rel, ok := a.relOf(d.path); ok && d.dirty() {
			texts[rel] = d.buf.String()
		}
	}
	cancel := &atomic.Bool{}
	s.cancel, s.running = cancel, true
	a.runSearch(s.gen, re, s.fold, rels, texts, cancel, false)
}

// wants reports whether search looks in a file, by the files to include
// and to exclude.
func (s *searchState) wants(rel string) bool {
	return (len(s.includes) == 0 || s.includes.Match(rel)) && !s.excludes.Match(rel)
}

// relOf returns a path relative to the root, with slashes, false for a
// path outside it.
func (a *app) relOf(path string) (string, bool) {
	if a.root == "" || !strings.HasPrefix(path, a.root+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(path[len(a.root)+1:]), true
}

// runSearch looks for re in the files rels, of the text in texts for
// those open, several at a time, and hands what it finds to the main
// thread as it goes. A search again of a few files, partial, gives each
// file it looked in, matches or not, which replace those of the results.
func (a *app) runSearch(gen int, re *regexp.Regexp, fold foldQuery, rels []string, texts map[string]string, cancel *atomic.Bool, partial bool) {
	root := a.root
	if remote.Is(root) {
		var pre *grepQuery
		if !partial {
			s := &a.inFiles
			pre = &grepQuery{s.query, s.matchCase, s.regex}
		}
		go a.searchRemote(gen, root, re, fold, rels, texts, cancel, partial, pre)
		return
	}
	bg.Go(func() {
		defer recoverCrash()
		var (
			mu    sync.Mutex
			batch []*searchFile
			found atomic.Int64
			next  atomic.Int64
			wg    sync.WaitGroup
		)
		flush := func(done bool) {
			mu.Lock()
			b := batch
			batch = nil
			mu.Unlock()
			stopped := found.Load() >= maxSearchMatches
			if len(b) > 0 || done {
				a.post(func() { a.takeResults(gen, b, done && !partial, stopped) })
			}
		}
		for range max(1, min(runtime.GOMAXPROCS(0), 8, len(rels))) {
			wg.Go(func() {
				defer recoverCrash()
				for !cancel.Load() {
					i := int(next.Add(1) - 1)
					if i >= len(rels) {
						return
					}
					limit := maxSearchMatches - int(found.Load())
					if limit <= 0 && !partial {
						cancel.Store(true)
						return
					}
					f := searchFileText(root, rels[i], texts, re, fold, max(limit, 1), partial)
					if f == nil {
						continue
					}
					found.Add(int64(len(f.matches)))
					mu.Lock()
					batch = append(batch, f)
					mu.Unlock()
				}
			})
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		tick := time.NewTicker(60 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				flush(false)
				continue
			case <-done:
			}
			break
		}
		flush(true)
	})
}

// searchFileText finds re in a file, or in its text in texts: nil when
// it finds nothing, unless partial, or when it cannot read the file, as
// one too large or binary. fold, when it is of a query, finds its
// matches as re does, quicker. It may run on any goroutine.
func searchFileText(root, rel string, texts map[string]string, re *regexp.Regexp, fold foldQuery, limit int, partial bool) *searchFile {
	path := filepath.Join(root, filepath.FromSlash(rel))
	f := &searchFile{path: path, rel: rel}
	text, ok := texts[rel]
	if !ok && !remote.Is(path) {
		text, ok = readSearchText(path)
	}
	if ok {
		locs, folded := fold.matches(text, limit)
		if !folded {
			locs = re.FindAllStringIndex(text, limit)
		}
		f.matches = matchesAt(text, locs)
	}
	if len(f.matches) == 0 && !partial {
		return nil
	}
	return f
}

// readSearchText reads a file's text, its lines ended by "\n", false for
// a file too large, binary, or not to be read. It opens the file once and
// asks the open file for its size, which costs half the system calls of
// a stat by path then a read: most of a search's time.
func readSearchText(path string) (string, bool) {
	// Not to wait on a FIFO, which the index may list through a link.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSearchBytes {
		return "", false
	}
	data := make([]byte, info.Size())
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", false
	}
	data = data[:n]
	if isBinary(data) {
		return "", false
	}
	if bytes.Contains(data, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	return string(data), true
}

// foldQuery is a query ignoring case, which finds its matches without
// regular expressions where it can: Go's ignoring case find no text
// quickly, and most of the files hold no match.
type foldQuery struct {
	needle string // in lower case; "" for a query that does not fold
	words  bool   // whole words, which it leaves to the expression
}

// foldQueryOf returns the foldQuery of a query, of none but one ignoring
// case of ASCII text alone, which folds so simply.
func foldQueryOf(query string, matchCase, word, regex bool) foldQuery {
	if matchCase || regex || query == "" {
		return foldQuery{}
	}
	for i := range len(query) {
		if query[i] >= utf8.RuneSelf {
			return foldQuery{}
		}
	}
	return foldQuery{strings.ToLower(query), word}
}

// foldBufs are buffers for texts in lower case, one a goroutine at a time.
var foldBufs = sync.Pool{New: func() any { return new([]byte) }}

// matches finds up to limit matches of q in text, as its expression
// does: true when it found them, false when the expression must, as for
// whole words, or for a text with the runes beyond ASCII that fold to
// ASCII letters. A text without q's needle has no matches.
func (q foldQuery) matches(text string, limit int) ([][]int, bool) {
	fold := q.needle
	if fold == "" {
		return nil, false
	}
	// The Kelvin sign and the long s fold to k and s, as (?i) folds them.
	if strings.ContainsAny(fold, "ks") && strings.ContainsAny(text, "\u212a\u017f") {
		return nil, false
	}
	bp := foldBufs.Get().(*[]byte)
	defer foldBufs.Put(bp)
	lower := append((*bp)[:0], text...)
	for i, b := range lower {
		if 'A' <= b && b <= 'Z' {
			lower[i] = b + 'a' - 'A'
		}
	}
	*bp = lower
	needle := []byte(fold)
	if !bytes.Contains(lower, needle) {
		return nil, true
	}
	if q.words {
		return nil, false
	}
	var locs [][]int
	for at := 0; len(locs) < limit || limit < 0; {
		i := bytes.Index(lower[at:], needle)
		if i < 0 {
			break
		}
		locs = append(locs, []int{at + i, at + i + len(needle)})
		at += i + len(needle)
	}
	return locs, true
}

// matchesAt are the matches of text at locs, the byte offsets of their
// starts and ends, in order, with what shows of their lines.
func matchesAt(text string, locs [][]int) []searchMatch {
	var out []searchMatch
	line, lineStart, at := 0, 0, 0 // the line of byte at, and where it starts
	for _, m := range locs {
		if m[0] == m[1] {
			continue // empty matches, as of ^ or a*
		}
		if n := strings.Count(text[at:m[0]], "\n"); n > 0 {
			line += n
			lineStart = at + strings.LastIndexByte(text[at:m[0]], '\n') + 1
		}
		at = m[0]
		lineEnd := len(text)
		if i := strings.IndexByte(text[m[0]:], '\n'); i >= 0 {
			lineEnd = m[0] + i
		}
		hitEnd := min(m[1], lineEnd)
		from := runeStart(text, max(lineStart, m[0]-previewBytes))
		to := runeStart(text, min(lineEnd, hitEnd+previewBytes))
		out = append(out, searchMatch{
			start: m[0], end: m[1], text: text[m[0]:m[1]],
			line: line, col: utf8.RuneCountInString(text[lineStart:m[0]]),
			pre: text[from:m[0]], hit: text[m[0]:hitEnd], post: text[hitEnd:to],
		})
	}
	return out
}

// runeStart moves i back to the start of the rune it is in.
func runeStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// takeResults takes the files a search found in the background, unless
// another search started since.
func (a *app) takeResults(gen int, files []*searchFile, done, stopped bool) {
	s := &a.inFiles
	if gen != s.gen {
		return
	}
	for _, f := range files {
		i, ok := slices.BinarySearchFunc(s.files, f.rel, func(e *searchFile, rel string) int { return strings.Compare(e.rel, rel) })
		if ok {
			s.count -= len(s.files[i].matches)
		}
		// The files searched at once may find more than all may.
		if room := max(maxSearchMatches-s.count, 0); len(f.matches) > room {
			f.matches, stopped = f.matches[:room], true
		}
		switch {
		case ok:
			if len(f.matches) == 0 {
				s.files = slices.Delete(s.files, i, i+1)
			} else {
				s.files[i] = f
			}
		case len(f.matches) > 0:
			s.files = slices.Insert(s.files, i, f)
		}
		if len(f.matches) > 0 {
			a.watchResult(f.path)
		}
		s.count += len(f.matches)
	}
	s.stopped = s.stopped || stopped
	if done {
		s.running = false
	}
	s.changed()
}

// maxSearchWatched is the most folders of results watched for changes:
// each takes a file descriptor or more.
const maxSearchWatched = 256

// watchResult watches the folder of a file of the results, so that the
// results follow it as it changes on disk.
func (a *app) watchResult(path string) {
	s := &a.inFiles
	dir := filepath.Dir(path)
	if a.tree == nil || s.watched[dir] || len(s.watched) >= maxSearchWatched {
		return
	}
	if s.watched == nil {
		s.watched = map[string]bool{}
	}
	s.watched[dir] = true
	a.tree.Watch(path)
}

// searchAgain searches a few files again, as they changed on disk or in
// the editor, keeping the rest of the results.
func (a *app) searchAgain(paths []string) {
	s := &a.inFiles
	if s.re == nil || a.root == "" {
		return
	}
	texts := map[string]string{}
	var rels []string
	for _, p := range paths {
		rel, ok := a.relOf(p)
		if !ok || !s.wants(rel) || slices.Contains(rels, rel) {
			continue
		}
		_, shown := slices.BinarySearchFunc(s.files, rel, func(e *searchFile, rel string) int { return strings.Compare(e.rel, rel) })
		if !shown && !slices.Contains(a.index, rel) {
			continue // a file search does not look in
		}
		if i := a.docIndex(p); i >= 0 {
			texts[rel] = a.docs[i].buf.String()
		}
		rels = append(rels, rel)
	}
	if len(rels) > 0 {
		a.runSearch(s.gen, s.re, s.fold, rels, texts, &atomic.Bool{}, true)
	}
}

// searchTick searches the open documents edited since again, and starts
// a search once typing pauses. It runs once a frame.
func (a *app) searchTick() {
	s := &a.inFiles
	if s.waiting && time.Since(s.typedAt) >= searchPause {
		a.startSearch()
	}
	if s.re == nil {
		return
	}
	var edited []string
	for _, d := range a.docs {
		if d.path == "" {
			continue
		}
		v := d.buf.Version()
		if old, ok := s.seen[d.path]; !ok || old != v {
			s.seen[d.path] = v
			if ok {
				edited = append(edited, d.path)
			}
		}
	}
	a.searchAgain(edited)
}

// searchRows are the rows of the results: each file's header, and its
// matches unless it is folded.
func (a *app) searchRows() []searchRow {
	s := &a.inFiles
	if s.rowsOf == s.version && s.rows != nil {
		return s.rows
	}
	// Anew rather than over the last: the list's functions of the last
	// frame may still read those.
	var rows []searchRow
	for i, f := range s.files {
		rows = append(rows, searchRow{i, -1})
		if s.collapsed[f.rel] {
			continue
		}
		for j := range f.matches {
			rows = append(rows, searchRow{i, j})
		}
	}
	s.rows, s.rowsOf = rows, s.version
	return rows
}

// openMatch shows a match in the editor, selected: in the column's
// preview tab, the keyboard staying in the results, or with keep in a tab
// of its own with the keyboard.
func (a *app) openMatch(f *searchFile, m searchMatch, keep bool) {
	// The match is in the text, whatever markdownPreview says.
	a.openText(func() {
		if keep {
			a.openFile(f.path, true)
		} else {
			a.previewFile(f.path)
		}
	})
	a.selectMatch(f.path, m)
}

// selectMatch selects a match in the editor shown, when it has its file.
func (a *app) selectMatch(path string, m searchMatch) {
	d := a.activeEditor()
	if d == nil || d.path != path {
		return
	}
	if d.preview != nil {
		// The match is in the text.
		a.setPreview(d, false)
	}
	snap := d.buf.Snapshot()
	if m.line >= snap.LineCount() {
		return
	}
	s, e := snap.LineRange(m.line)
	start := min(s+m.col, e)
	d.selectRange(start, min(start+utf8.RuneCountInString(m.text), snap.Len()), true)
}

// replacement is a match of a file's text and what replaces it, by byte
// offsets.
type replacement struct {
	start, end int
	with       string
}

// replacementsIn finds the matches of re in text, and what replaces each
// by the template tmpl: all of them, or with only, the one match that is
// where only was found with its text, if it still is.
func replacementsIn(text string, re *regexp.Regexp, tmpl string, only *searchMatch) []replacement {
	var out []replacement
	for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
		if m[0] == m[1] {
			continue
		}
		if only != nil && (m[0] != only.start || text[m[0]:m[1]] != only.text) {
			continue
		}
		out = append(out, replacement{m[0], m[1], string(re.ExpandString(nil, tmpl, text, m))})
	}
	return out
}

// replaceText makes the replacements in text.
func replaceText(text string, rs []replacement) string {
	var b strings.Builder
	at := 0
	for _, r := range rs {
		b.WriteString(text[at:r.start])
		b.WriteString(r.with)
		at = r.end
	}
	b.WriteString(text[at:])
	return b.String()
}

// replaceInFile makes the replacements of re in a file not open,
// keeping its line endings and its mode, and returns how many it made.
// It may run on any goroutine.
func replaceInFile(path string, re *regexp.Regexp, tmpl string, only *searchMatch) (int, error) {
	data, err := vfs.ReadFile(path)
	if err != nil || isBinary(data) {
		return 0, err
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	if crlf {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	text := string(data)
	rs := replacementsIn(text, re, tmpl, only)
	if len(rs) == 0 {
		return 0, nil
	}
	text = replaceText(text, rs)
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	_, err = vfs.WriteFile(path, []byte(text), 0o644)
	return len(rs), err
}

// replaceInDoc makes the replacements of re in an open document, as one
// step of its undo, and returns how many it made.
func replaceInDoc(doc *document, re *regexp.Regexp, tmpl string, only *searchMatch) int {
	ed := doc.main()
	if ed == nil {
		return 0
	}
	text := doc.buf.String()
	rs := replacementsIn(text, re, tmpl, only)
	switch {
	case len(rs) == 0:
		return 0
	case len(rs) > 500:
		ed.edit(ed.sel, textEdit{0, doc.buf.Len(), replaceText(text, rs)})
		return len(rs)
	}
	edits := make([]textEdit, len(rs))
	runes, at := 0, 0 // the rune offset of byte at
	for i, r := range rs {
		runes += utf8.RuneCountInString(text[at:r.start])
		start := runes
		runes += utf8.RuneCountInString(text[r.start:r.end])
		at = r.end
		edits[i] = textEdit{start, runes, r.with}
	}
	ed.edit(ed.sel, edits...)
	return len(rs)
}

// replaceMatches replaces the matches of the results in files, or with
// only, that one match of the one file. Open documents change as a step
// of undo; the other files are written in the background.
func (a *app) replaceMatches(files []*searchFile, only *searchMatch) {
	s := &a.inFiles
	re, err := compileQuery(s.query, s.matchCase, s.word, s.regex)
	if err != nil || s.query == "" {
		return
	}
	tmpl := replaceTemplate(s.with, s.regex)
	n, changed := 0, 0
	var closed []string
	for _, f := range files {
		if i := a.docIndex(f.path); i >= 0 {
			if k := replaceInDoc(a.docs[i], re, tmpl, only); k > 0 {
				n += k
				changed++
			}
			continue
		}
		closed = append(closed, f.path)
	}
	if len(closed) == 0 {
		a.toldReplaced(n, changed, only != nil)
		return
	}
	bg.Go(func() {
		var failed []string
		for _, path := range closed {
			k, err := replaceInFile(path, re, tmpl, only)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			}
			if k > 0 {
				n += k
				changed++
			}
		}
		a.post(func() {
			for _, msg := range failed {
				a.fail("%s", locales.FailReplaceIn(msg))
			}
			a.searchAgain(closed)
			a.toldReplaced(n, changed, only != nil)
		})
	})
}

// toldReplaced says how many matches a replacement replaced, or that
// the one match asked for is gone.
func (a *app) toldReplaced(n, files int, one bool) {
	switch {
	case one && n == 0:
		a.fail("%s", locales.SearchMatchChanged())
	case one:
	case n == 0:
		a.fail("%s", locales.SearchNothingToReplace())
	default:
		a.tell(note{message: locales.SearchReplacedIn(n, files)})
	}
}

// replaceAllInFiles asks, then replaces every match of the results.
func (a *app) replaceAllInFiles() {
	s := &a.inFiles
	if s.count == 0 || s.running {
		return
	}
	files := slices.Clone(s.files)
	closed := 0
	for _, f := range files {
		if a.docIndex(f.path) < 0 {
			closed++
		}
	}
	with := s.with
	if with == "" {
		with = locales.SearchWithNothing()
	} else {
		with = "“" + with + "”"
	}
	msg := locales.SearchEachOpenFile()
	if closed > 0 {
		msg = locales.SearchClosedWritten(closed)
	}
	a.confirm = &confirm{
		title:   locales.SearchReplaceQuestion(s.count, len(files), with),
		message: msg, ok: locales.FindReplaceAll(), danger: closed > 0,
		run: func() { a.replaceMatches(files, nil) },
	}
}

// searchView is the search view of the sidebar: the query and its
// options, and the matches found, under their files.
func (a *app) searchView(c *ui.Context) {
	p := a.pal
	s := &a.inFiles
	if k := s.searchKey(); k != s.key {
		s.key, s.typedAt, s.waiting = k, time.Now(), true
		bg.AfterFunc(searchPause, a.invalidate)
	}
	if s.query != "" && (s.running || s.waiting) {
		ui.Progress(c.Key("search-activity"), -1).Height(2).Radius(0).Label(locales.SearchSearching())
	} else {
		ui.Box(c.Key("search-activity")).Height(2)
	}
	ui.Column(c).Padding(0, 12, 6, 6).Gap(5).Children(func() {
		ui.Row(c).Gap(2).AlignItems(ui.Stretch).Children(func() {
			b := ui.ButtonBase(c.Key("toggle-replace")).Width(18).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
				Label(locales.SearchToggleReplace()).Tooltip(a.withKey(locales.SearchToggleReplace(), "edit.replaceInFiles"))
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Transition(hoverFade)
			b.Children(func() { chevron(c, s.replacing).FontSize(13).TextColor(p.muted) })
			if b.Clicked() {
				s.replacing = !s.replacing
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				in := a.searchField(c, "search-query", &s.query, locales.CmdViewSearch(), &s.input, func() {
					a.optionToggle(c, iconCase, locales.FindMatchCase(), &s.matchCase)
					a.optionToggle(c, iconWord, locales.FindWholeWord(), &s.word)
					a.optionToggle(c, iconRegex, locales.FindRegex(), &s.regex)
				})
				if in.Submitted() {
					a.startSearch()
				}
				if in.Shortcut(0, ui.KeyDown) && len(s.files) > 0 {
					s.row = min(1, len(a.searchRows())-1)
					s.toList = true
					a.previewRow()
				}
				if !s.replacing {
					return
				}
				with := a.searchField(c, "search-with", &s.with, locales.FindReplace(), nil, func() {
					if a.smallButton(c, iconReplaceAll, locales.FindReplaceAll()) && s.count > 0 {
						a.post(a.replaceAllInFiles)
					}
				})
				if with.Submitted() {
					a.replaceAllInFiles()
				}
			})
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 0, 0, 20).Children(func() {
			a.searchSummary(c)
			if a.iconButton(c, iconMore, locales.SearchToggleDetails(), s.details).Size(22, 18).Clicked() {
				s.details = !s.details
			}
		})
		if !s.details {
			return
		}
		ui.Column(c).Gap(4).Padding(0, 0, 0, 20).Children(func() {
			ui.Text(c, locales.SearchFilesInclude()).FontSize(11).TextColor(p.muted)
			if a.searchField(c, "search-include", &s.include, locales.SearchIncludeExample(), nil, nil).Submitted() {
				a.startSearch()
			}
			ui.Text(c, locales.SearchFilesExclude()).FontSize(11).TextColor(p.muted).Margin(2, 0, 0, 0)
			if a.searchField(c, "search-exclude", &s.exclude, locales.SearchExcludeExample(), nil, nil).Submitted() {
				a.startSearch()
			}
		})
	})
	a.searchResults(c)
}

// searchField is a text field of the search view, with what goes after
// its text, as its options, and returns its input.
func (a *app) searchField(c *ui.Context, key string, value *string, placeholder string, bind *ui.Handle, after func()) ui.Element {
	p := a.pal
	var in ui.Element
	field := ui.Row(c).Height(26).Padding(0, 4, 0, 8).Gap(2).AlignItems(ui.Center).Radius(5).
		Background(p.editor).Border(1, p.border).Transition(fieldFocus)
	field.Children(func() {
		in = a.textKeys(ui.TextInputBase(c.Key(key), value)).Grow(1).MinWidth(0).FontSize(12.5).Placeholder(placeholder).Label(placeholder)
		if bind != nil {
			in.Bind(bind)
		}
		if in.Focused() {
			field.Border(1, p.accentDim)
		}
		if after != nil {
			after()
		}
	})
	return in
}

// searchSummary says what the search found.
func (a *app) searchSummary(c *ui.Context) {
	p := a.pal
	s := &a.inFiles
	col := p.muted
	var msg string
	switch {
	case s.err != "":
		msg, col = s.err, p.dangerFg
	case s.query == "":
	case s.count > 0:
		msg = locales.SearchResultsIn(s.count, len(s.files))
		if s.stopped {
			msg = locales.SearchFirstResults(maxSearchMatches, len(s.files))
		}
		if s.running {
			msg += "…"
		}
	case s.running || s.waiting:
		msg = locales.SearchSearchingEllipsis()
	case s.re != nil:
		msg = locales.SearchNoResults()
		if s.include != "" || s.exclude != "" {
			msg = locales.SearchNoResultsFiltered()
		}
	}
	ui.Text(c, msg).FontSize(11.5).TextColor(col).Grow(1).SingleLine().Shrink(1)
}

// searchResults lists the matches under their files.
func (a *app) searchResults(c *ui.Context) {
	p := a.pal
	s := &a.inFiles
	rows, files := a.searchRows(), s.files
	if len(rows) == 0 {
		if s.query == "" {
			a.searchHint(c)
		}
		return
	}
	s.list.Selected = &s.row
	s.list.Header = func(i int) bool { return i < len(rows) && rows[i].match < 0 }
	s.list.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		r := rows[i]
		if r.match < 0 {
			return files[r.file].rel
		}
		return [2]any{files[r.file].rel, files[r.file].matches[r.match].start}
	}
	s.list.Label = func(i int) string {
		if i >= len(rows) || rows[i].match < 0 {
			return ""
		}
		m := files[rows[i].file].matches[rows[i].match]
		return strings.TrimSpace(m.pre + m.hit + m.post)
	}
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = p.active, p.text, p.hover
	c.SetTheme(&quiet)
	var action func()
	first := firstMatchRow(rows)
	out := ui.List(c.Key("search"), &s.list, len(rows), func(i int) {
		r := rows[i]
		f := files[r.file]
		// Up from the first match goes back to the query; the row asks
		// for it before the list does.
		if i == first && s.row == first && s.list.Shortcut(c, 0, ui.KeyUp) {
			action = func() { s.input.Focus() }
		}
		if r.match < 0 {
			a.searchHeader(c, f, &action)
		} else {
			a.searchMatchRow(c, i, f, f.matches[r.match], &action)
		}
	}).Grow(1).Padding(0, 6, 8, 6)
	c.SetTheme(base)
	if s.toList && s.list.Focus(c) {
		s.toList = false
	}
	if s.list.Shortcut(c, 0, ui.KeyEscape) {
		s.input.Focus()
	}
	if action != nil {
		action()
	}
	switch {
	case s.row < 0 || s.row >= len(rows) || rows[s.row].match < 0:
	case out.Submitted():
		// Enter, or a double click.
		r := rows[s.row]
		a.openMatch(files[r.file], files[r.file].matches[r.match], true)
	case out.Changed():
		// A click, or the arrow keys: the editor follows.
		a.previewRow()
	}
}

// previewRow shows the match of the row chosen in the results in the
// preview tab.
func (a *app) previewRow() {
	s := &a.inFiles
	rows := a.searchRows()
	if s.row < 0 || s.row >= len(rows) || rows[s.row].match < 0 {
		return
	}
	f := s.files[rows[s.row].file]
	a.openMatch(f, f.matches[rows[s.row].match], false)
}

// searchHint is what the view shows before a query: what it does, and
// its keys.
func (a *app) searchHint(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(10, 14, 0, 26).Gap(6).Children(func() {
		ui.Text(c, locales.SearchHintFind()).FontSize(12).TextColor(p.muted)
		for _, k := range [][2]string{{"↓", locales.SearchHintResults()}, {"⏎", locales.SearchHintOpen()}, {a.keyHint("edit.replaceInFiles"), locales.FindReplace()}} {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, k[0]).FontSize(11).TextColor(p.muted).Padding(0, 5).Radius(4).Background(p.hover).MinWidth(22).
					TextAlign(ui.Center)
				ui.Text(c, k[1]).FontSize(11.5).TextColor(p.faint)
			})
		}
	})
}

// firstMatchRow is the row of the first match, -1 for none.
func firstMatchRow(rows []searchRow) int {
	for i, r := range rows {
		if r.match >= 0 {
			return i
		}
	}
	return -1
}

// searchHeader is the row of a file of the results: a click folds it.
func (a *app) searchHeader(c *ui.Context, f *searchFile, action *func()) {
	p := a.pal
	s := &a.inFiles
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Background(p.chrome)
	hovered := row.Hovered()
	clicked := row.Clicked()
	a.dragFile(row, &fileDrag{path: f.path})
	row.Children(func() {
		chevron(c, !s.collapsed[f.rel]).FontSize(12).TextColor(p.muted)
		name := filepath.Base(f.path)
		fi, col := fileIcon(p, name)
		ui.Icon(c, fi).FontSize(14).TextColor(col)
		ui.Text(c, name).FontSize(13).TextColor(p.text).SingleLine().Shrink(0)
		if dir := filepath.ToSlash(filepath.Dir(f.rel)); dir != "." {
			ui.Text(c, dir).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
		}
		ui.Spacer(c)
		if hovered && s.replacing && a.smallButton(c, iconReplaceAll, locales.SearchReplaceAllInFile()) {
			*action = func() { a.replaceMatches([]*searchFile{f}, nil) }
		}
		ui.Text(c, fmt.Sprint(len(f.matches))).FontSize(10.5).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.hover)
	})
	if clicked && *action == nil {
		*action = func() {
			if s.collapsed == nil {
				s.collapsed = map[string]bool{}
			}
			s.collapsed[f.rel] = !s.collapsed[f.rel]
			s.changed()
		}
	}
}

// searchMatchRow is the row of a match: its line, with the match marked,
// and what replaces it while replacing. A click chooses it in the list,
// which shows it in the editor, and a double click keeps it there. It is
// dragged to a column as its file, the match selected.
func (a *app) searchMatchRow(c *ui.Context, i int, f *searchFile, m searchMatch, action *func()) {
	s := &a.inFiles
	row := ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Height(22).Padding(0, 0, 0, 30)
	hovered := row.Hovered()
	// Taking the drag, the row takes the clicks from the list too.
	a.dragFile(row, &fileDrag{path: f.path, match: &m})
	switch {
	case row.DoubleClicked():
		*action = func() { a.openMatch(f, m, true) }
	case row.Clicked():
		*action = func() {
			s.row, s.toList = i, true
			a.previewRow()
		}
	}
	row.Children(func() {
		ui.RichText(c, a.matchPreview(m)...).FontSize(12.5).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if hovered && s.replacing {
			if a.smallButton(c, iconReplace, locales.FindReplace()) {
				*action = func() { a.replaceMatches([]*searchFile{f}, &m) }
			}
		} else {
			// Its line, to tell apart the matches of lines alike.
			ui.Text(c, strconv.Itoa(m.line+1)).FontSize(10.5).TextColor(a.pal.faint).SingleLine().Shrink(0).Padding(0, 4, 0, 0)
		}
	})
}

// previewLead is how many characters of its line a match's row shows
// before it, at most, so that the match shows in a narrow sidebar.
const previewLead = 28

// matchPreview is the line of a match as its row shows it: from a little
// before the match, with the match marked, struck out when replacing and
// followed by what replaces it.
func (a *app) matchPreview(m searchMatch) []ui.Span {
	p := a.pal
	s := &a.inFiles
	pre := strings.TrimLeft(m.pre, " \t")
	if n := utf8.RuneCountInString(pre); n > previewLead {
		cut := 0
		for range n - previewLead {
			_, size := utf8.DecodeRuneInString(pre[cut:])
			cut += size
		}
		pre = "…" + pre[cut:]
	}
	oneLine := func(t string) string { return strings.ReplaceAll(strings.ReplaceAll(t, "\t", " "), "\n", "⏎") }
	spans := []ui.Span{{Text: oneLine(pre), Color: p.text}}
	if !s.replacing || s.re == nil {
		spans = append(spans, ui.Span{Text: oneLine(m.hit), Color: p.text, Background: p.matchNow})
	} else {
		with := s.re.ReplaceAllString(m.text, replaceTemplate(s.with, s.regex))
		spans = append(spans, ui.Span{Text: oneLine(m.hit), Color: p.text, Background: p.removed.Alpha(0.22), Strikethrough: true})
		if with != "" {
			spans = append(spans, ui.Span{Text: oneLine(with), Color: p.text, Background: p.added.Alpha(0.22)})
		}
	}
	return append(spans, ui.Span{Text: oneLine(m.post), Color: p.text})
}

// searchTitleActions are the buttons of the view's title.
func (a *app) searchTitleActions(c *ui.Context, hovered bool) {
	s := &a.inFiles
	if !hovered {
		return
	}
	if a.iconButton(c, iconRefresh, locales.Refresh(), false).Clicked() {
		a.startSearch()
	}
	allFolded := len(s.files) > 0
	for _, f := range s.files {
		allFolded = allFolded && s.collapsed[f.rel]
	}
	label := locales.SearchCollapseAll()
	if allFolded {
		label = locales.SearchExpandAll()
	}
	if a.iconButton(c, iconCollapse, label, false).Disabled(len(s.files) == 0).Clicked() {
		s.collapsed = map[string]bool{}
		for _, f := range s.files {
			s.collapsed[f.rel] = !allFolded
		}
		s.changed()
	}
	if a.iconButton(c, iconX, locales.SearchClearResults(), false).Disabled(s.query == "").Clicked() {
		s.query = ""
		a.startSearch()
		s.input.Focus()
	}
}
