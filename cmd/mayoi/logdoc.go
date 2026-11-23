package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mayoi/locales"
)

// A file too large to hold in memory opens in log mode: one pass makes an
// index of where its lines start, and only a window of lines around the
// view is ever put in the buffer. Edits are kept as replacements over the
// file's text, so that a save rewrites the file with them and leaves the
// parts not edited as the bytes they are.
//
// Two coordinate spaces meet here. Base runes count the file's text with
// each CR LF pair as one LF and past the byte order mark: the line index
// and the edits are in them, and they never change while the file is
// open. The text's runes are the base with the edits applied: the run of
// them the buffer holds a window of.

const (
	// logWindowBytes is the byte budget of the window of lines around the
	// view that the buffer holds.
	logWindowBytes = 2 << 20
	// logMarginLines is how many lines the window holds past the lines in
	// view, above and below, so that scrolling stays in it.
	logMarginLines = 512
	// logReadBytes is how much of the file one read takes.
	logReadBytes = 1 << 20
	// logMaxFileBytes is the largest file log mode takes; a larger one is
	// more than its index is for.
	logMaxFileBytes = 2 << 30
	// logMaxLines is the most lines log mode indexes.
	logMaxLines = 32 << 20
	// logRenameWaits is how many times a save waits to put its file in
	// place, for a reader of the path that holds it a moment.
	logRenameWaits = 20
)

// errNotLogText says a file is not the UTF-8 text log mode reads, so that
// the whole file is read the ordinary way instead.
var errNotLogText = errors.New("not UTF-8 text")

// errLogClosed says a document's windows are not built any more.
var errLogClosed = errors.New("document closed")

// errLogChanged says a log document's file is not as it was read, so that
// a save would write over what stands there now.
var errLogChanged = errors.New(locales.LogFileChanged())

// logLine is where a line of the file on disk starts: its byte offset in
// the file as it is, and its rune offset in the base text.
type logLine struct {
	byteOff int64
	runeOff int64
}

// logEdit replaces the base runes baseStart up to baseEnd with text, whose
// line ends are LF. The edits of a document are ordered by baseStart and
// do not overlap.
type logEdit struct {
	baseStart, baseEnd int64
	text               string
}

// logFile is a log document's file as it was last read or written: its
// size, its byte order mark and its line endings, and the index of where
// its lines start. Nothing of it changes once made: a document takes a
// whole new one when its file is read again or written, so that a build
// holding one keeps a file that stays whole and alike. The file itself is
// not part of it — every read opens the file of its own.
type logFile struct {
	size  int64
	bom   bool // the file begins with a UTF-8 byte order mark
	crlf  bool // the file has CR LF line endings
	lines []logLine
	end   logLine    // the end of the file, its byte and rune offsets
	stamp *fileStamp // what the file was as it was read or written
}

// logDoc is a file open in log mode: its line index, the edits made to
// it, and which window of it the document's buffer holds.
type logDoc struct {
	path string

	// io has one user of the file at a time: a window built in the
	// background, or a save that rewrites the file. Each opens the file
	// of its own; the lock keeps a save from putting its file in place
	// under a reader.
	io sync.Mutex

	mu      sync.Mutex
	file    *logFile // the index of the file as last read or written; swapped whole
	edits   []logEdit
	len     int64  // the runes of the text, the base with the edits
	version uint64 // counts the edits, so that a window built for one text is not put in another
	closed  bool

	// win says which run of the text the document's buffer holds: the
	// line and the rune its first line and rune are at, and how many
	// lines it holds. cutLine is the line, from 1, the window shows in
	// part because it is longer than the editor lays out a line, 0 for
	// none. moving says a window for another place is being built,
	// wantLin where around, of the text of wantVer.
	win     logWin
	moving  bool
	wantLin int
	wantVer uint64
}

// logWin is the window of a log document's text in its buffer.
type logWin struct {
	first    int   // the line of the text the buffer's first line is
	start    int64 // the rune of the text the buffer's first rune is
	lines    int   // the lines the buffer holds
	cutLine  int   // the line, from 1, shown in part, 0 for none
	cutBytes int64 // the bytes of the line whole, of a line shown in part

	cutAbove      int   // the line, from 1, the window starts past the cut of, 0 for none
	cutAboveBytes int64 // the bytes of that line whole
}

func (w logWin) set() bool { return w.lines > 0 }

// openLogDoc opens the file at path and makes the index of its lines.
func openLogDoc(path string) (*logDoc, error) {
	return openLogDocRead(path, nil, nil)
}

// openLogDocRead is openLogDoc that tells progress of the bytes read and
// stops when stop closes, as a document loading it does.
func openLogDocRead(path string, stop <-chan struct{}, progress func(read, total int64)) (*logDoc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		f.Close()
		return nil, &fs.PathError{Op: "read", Path: path, Err: errors.New("is a directory")}
	}
	if info.Size() > logMaxFileBytes {
		f.Close()
		return nil, errors.New(locales.ErrTooLarge())
	}
	file, err := scanLog(f, info.Size(), stampOf(info), stop, progress)
	f.Close()
	if err != nil {
		return nil, err
	}
	return &logDoc{path: path, file: file, len: file.end.runeOff, wantLin: -1}, nil
}

// close marks the document closed, so that its windows are not built any
// more. The file itself is opened by each read of its own: there is
// nothing to let go of here.
func (l *logDoc) close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}

// fileOf returns the index of the file as it is, for a read that works on
// it on its own.
func (l *logDoc) fileOf() *logFile {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file
}

// scanLog reads a whole file once, making the index of its lines. The
// scanLog reads a whole file once, making the index of its lines. The
// file is read from f, of size bytes; stamp is what the file was. The
// read tells progress and stops when stop closes.
func scanLog(f *os.File, size int64, stamp *fileStamp, stop <-chan struct{}, progress func(read, total int64)) (*logFile, error) {
	file := &logFile{size: size, stamp: stamp}
	r := bufio.NewReaderSize(f, logReadBytes)
	off := int64(0)
	// The byte order mark: UTF-8's is skipped; any other is text another
	// path reads.
	mark, _ := r.Peek(3)
	switch {
	case len(mark) == 3 && bytes.Equal(mark, []byte{0xEF, 0xBB, 0xBF}):
		file.bom = true
		r.Discard(3)
		off = 3
	case len(mark) >= 2 && (bytes.Equal(mark[:2], []byte{0xFF, 0xFE}) || bytes.Equal(mark[:2], []byte{0xFE, 0xFF})):
		return nil, errNotLogText
	}
	file.lines = append(file.lines, logLine{off, 0})
	runes := int64(0) // the runes of the lines so far, their LFs included
	// A line is read a buffer at a time however long it is, so that one
	// longer than the buffer takes no memory: its bytes are looked at as
	// they come, a rune cut at the end of a read carried into the next.
	var carry []byte
	nul := func(p []byte) bool { return bytes.IndexByte(p, 0) >= 0 }
	for {
		chunk, err := r.ReadSlice('\n')
		p := chunk
		if carry != nil {
			p = append(carry, chunk...)
			carry = nil
		}
		keep := len(p)
		if err == bufio.ErrBufferFull || err == nil && !utf8.FullRune(p) && len(chunk) > 0 {
			// A rune may be cut: look at all of it the next time.
			for keep > 0 && keep > len(p)-utf8.UTFMax && !utf8.RuneStart(p[keep-1]) {
				keep--
			}
		}
		if nul(p[:keep]) {
			return nil, errBinary
		}
		if !utf8.Valid(p[:keep]) {
			return nil, errNotLogText
		}
		runes += int64(utf8.RuneCount(p[:keep]))
		off += int64(len(chunk))
		if progress != nil && size > 0 {
			progress(off, size)
		}
		select {
		case <-stop:
			return nil, errReadCanceled
		default:
		}
		if err == bufio.ErrBufferFull {
			carry = append(carry, p[keep:]...)
			continue
		}
		// The line is whole: it ends with a LF, or the file does.
		if len(p) > 0 && p[len(p)-1] == '\n' {
			if len(p) > 1 && p[len(p)-2] == '\r' {
				file.crlf = true
				runes--
			}
			file.lines = append(file.lines, logLine{off, runes})
			if len(file.lines) > logMaxLines {
				return nil, errors.New(locales.ErrTooLarge())
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	file.end = logLine{off, runes}
	return file, nil
}

// editsCopy returns the edits as they are, for a build made in the
// background while the main thread may edit on.
func (l *logDoc) editsCopy() []logEdit {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.edits)
}

// hasEdits reports whether the text differs from the file.
func (l *logDoc) hasEdits() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.edits) > 0
}

// runeLen returns the runes of the text.
func (l *logDoc) runeLen() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.len
}

// versionOf returns the edit count of the text.
func (l *logDoc) versionOf() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version
}

// takeFile makes fs the file the document reads — the file as it was
// written, or as it was read again — moving the edits that are not among
// saved, those made since the save began, onto it by where saved leaves
// the text.
func (l *logDoc) takeFile(fs *logFile, saved []logEdit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := make([]logEdit, 0, len(l.edits))
	for _, e := range l.edits {
		if slices.Contains(saved, e) {
			continue // in the file as written
		}
		kept = append(kept, logEdit{curOf(saved, e.baseStart), curOf(saved, e.baseEnd), e.text})
	}
	l.file = fs
	l.edits = kept
	l.len = fs.end.runeOff
	for _, e := range l.edits {
		l.len += int64(utf8.RuneCountInString(e.text)) - (e.baseEnd - e.baseStart)
	}
	l.version++
}

// entryAt returns how many lines of the file start at or before base rune
// r, of lines.
func entryAt(lines []logLine, r int64) int {
	lo, hi := 0, len(lines)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if lines[m].runeOff <= r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// baseOf maps a rune of the text to the base rune it is at: inside an
// edit's replacement, the rune the edit starts at; past the end of one
// that adds runes, the rune after it. The edits are those of the text r
// is of, and *at is set to how many of them are wholly before r, where
// an edit of the base that far goes.
func baseOf(edits []logEdit, r int64, at *int) int64 {
	d := int64(0)
	n := 0
	for i := range edits {
		e := &edits[i]
		runes := int64(utf8.RuneCountInString(e.text))
		if r >= e.baseStart+d+runes {
			// The edit's replacement is wholly before r.
			d += runes - (e.baseEnd - e.baseStart)
			n = i + 1
			continue
		}
		if r >= e.baseStart+d {
			// Inside the edit's replacement.
			if at != nil {
				*at = i
			}
			return e.baseStart
		}
		break
	}
	if at != nil {
		*at = n
	}
	return r - d
}

// curOf maps a base rune to the rune of the text it is at.
func curOf(edits []logEdit, r int64) int64 {
	d := int64(0)
	for i := range edits {
		e := &edits[i]
		if e.baseEnd <= r {
			d += int64(utf8.RuneCountInString(e.text)) - (e.baseEnd - e.baseStart)
			continue
		}
		if e.baseStart > r {
			break
		}
		// Inside an edit: where its replacement is at.
		return e.baseStart + d + min(r-e.baseStart, int64(utf8.RuneCountInString(e.text)))
	}
	return r + d
}

// logPiece is a run of the text: base runes from up to a following edit,
// or an edit's replacement text.
type logPiece struct {
	edit    bool
	fresh   bool   // the piece starts where a line of the text does
	from    int64  // the base runes of a run of the base, from up to to
	to      int64  // (the base rune the run is of, past its own last)
	text    string // an edit's replacement
	curRune int64  // the rune of the text the piece starts at
	curLine int    // the line of the text the piece starts on
}

// lineStartsAt reports whether a line of the file starts at base rune r.
func lineStartsAt(lines []logLine, r int64) bool {
	i := entryAt(lines, r)
	return i > 0 && lines[i-1].runeOff == r
}

// pieces lays the text out as runs of the base with the edits between
// them, telling the rune and the line of the text each starts at, and
// whether that is where a line of the text starts.
func pieces(file *logFile, edits []logEdit) []logPiece {
	out := make([]logPiece, 0, 2*len(edits)+1)
	from, d, ln := int64(0), int64(0), 0
	for _, e := range edits {
		if e.baseStart > from {
			out = append(out, logPiece{from: from, to: e.baseStart, fresh: lineStartsAt(file.lines, from), curRune: from + d, curLine: ln})
			ln += entryAt(file.lines, e.baseStart) - entryAt(file.lines, from)
		}
		out = append(out, logPiece{edit: true, fresh: lineStartsAt(file.lines, e.baseStart), text: e.text, curRune: e.baseStart + d, curLine: ln})
		ln += strings.Count(e.text, "\n")
		d += int64(utf8.RuneCountInString(e.text)) - (e.baseEnd - e.baseStart)
		from = e.baseEnd
	}
	out = append(out, logPiece{from: from, to: file.end.runeOff, fresh: lineStartsAt(file.lines, from), curRune: from + d, curLine: ln})
	if len(out) == 1 {
		out[0].fresh = true
	}
	return out
}

// lineCount returns the lines of the text.
func (l *logDoc) lineCount() int {
	file := l.fileOf()
	return lineCountUnlocked(file, pieces(file, l.editsCopy()))
}

// lineCountUnlocked returns the lines of the text laid out as ps.
func lineCountUnlocked(file *logFile, ps []logPiece) int {
	last := &ps[len(ps)-1]
	n := last.curLine + entryAt(file.lines, file.end.runeOff) - entryAt(file.lines, last.from)
	return n + 1
}

// lineAt returns the line of the text rune r is on.
func (l *logDoc) lineAt(r int64) int {
	file := l.fileOf()
	return lineAtUnlocked(file, pieces(file, l.editsCopy()), l.runeLen(), r)
}

// lineAtUnlocked returns the line of the text laid out as ps, of runes
// length, that rune r is on.
func lineAtUnlocked(file *logFile, ps []logPiece, length, r int64) int {
	r = min(max(r, 0), length)
	for i := range ps {
		p := &ps[i]
		var end int64
		if p.edit {
			end = p.curRune + int64(utf8.RuneCountInString(p.text))
		} else if i+1 < len(ps) {
			end = ps[i+1].curRune
		} else {
			end = length
		}
		if r >= end && i+1 < len(ps) {
			continue
		}
		if p.edit {
			return p.curLine + strings.Count(runesPrefix(p.text, int(r-p.curRune)), "\n")
		}
		at := r - p.curRune + p.from
		return p.curLine + entryAt(file.lines, at) - entryAt(file.lines, p.from)
	}
	return ps[len(ps)-1].curLine
}

// lineStart returns the rune the line k of the text starts at.
func (l *logDoc) lineStart(k int) int64 {
	file := l.fileOf()
	ps := pieces(file, l.editsCopy())
	return lineStartUnlocked(file, ps, lineCountUnlocked(file, ps), k)
}

// lineStartUnlocked returns the rune the line k of the text laid out as
// ps, of total lines, starts at.
func lineStartUnlocked(file *logFile, ps []logPiece, total, k int) int64 {
	k = min(max(k, 0), total-1)
	for i := range ps {
		p := &ps[i]
		if k == p.curLine && p.fresh {
			// The piece starts where the line does.
			return p.curRune
		}
		next := total
		if p.edit {
			next = p.curLine + strings.Count(p.text, "\n")
		} else if i+1 < len(ps) {
			next = ps[i+1].curLine
		}
		if k >= next {
			continue
		}
		n := k - p.curLine // the nth line break of the piece starts line k
		if p.edit {
			return p.curRune + int64(utf8.RuneCountInString(nthLF(p.text, n)))
		}
		return p.curRune + (nthEntry(file.lines, p.from, p.to, int64(n)) - p.from)
	}
	return ps[len(ps)-1].curRune
}

// nthLF returns the text of t through its nth LF, from 1.
func nthLF(t string, n int) string {
	for i := 0; i < len(t); i++ {
		if t[i] == '\n' {
			n--
			if n == 0 {
				return t[:i+1]
			}
		}
	}
	return t
}

// nthEntry returns the rune offset of the base line that starts after
// base rune from and at or before to, the nth of them from 1.
func nthEntry(lines []logLine, from, to, n int64) int64 {
	i, end := entryAt(lines, from), entryAt(lines, to)
	if i+int(n) > end {
		return to
	}
	return lines[i+int(n)-1].runeOff
}

// logRead reads a log document's file: the file open for reading and the
// index of the file it is of, held together while the read runs.
type logRead struct {
	f    *os.File
	file *logFile
}

// openLogRead opens a log document's file to read it as its index says.
// The caller closes f.
func (l *logDoc) openLogRead() (*logRead, error) {
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	return &logRead{f: f, file: l.fileOf()}, nil
}

// readLocked is openLogRead with the mutex held: the file opened for
// reading, of the index as it is.
func (l *logDoc) readLocked() (*logRead, error) {
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	return &logRead{f: f, file: l.file}, nil
}

// byteAt returns the offset in the file of base rune r: the first byte of
// the rune there, a CR LF pair's LF counted from its CR.
func (rd *logRead) byteAt(r int64) int64 {
	file := rd.file
	if r <= 0 {
		return file.lines[0].byteOff
	}
	if r >= file.end.runeOff {
		return file.size
	}
	i := entryAt(file.lines, r) - 1 // the line r is on
	line := file.lines[i]
	if line.runeOff == r {
		return line.byteOff
	}
	// Read the line and count the bytes of the runes before r in it.
	end := file.size
	if i+1 < len(file.lines) {
		end = file.lines[i+1].byteOff
	}
	buf := make([]byte, end-line.byteOff)
	if n, _ := rd.f.ReadAt(buf, line.byteOff); int64(n) < int64(len(buf)) {
		return line.byteOff // the file is shorter than its index says
	}
	buf = buf[:max(0, len(buf)-1)] // without the LF
	if len(buf) > 0 && buf[len(buf)-1] == '\r' {
		buf = buf[:len(buf)-1]
	}
	skip := int(r - line.runeOff)
	for i := 0; i < len(buf); i++ {
		_, n := utf8.DecodeRune(buf[i:])
		i += n - 1
		skip--
		if skip == 0 {
			return line.byteOff + int64(i+n)
		}
	}
	return line.byteOff
}

// baseText returns the file's text of the base runes from up to to, with
// LF line ends. A read short of what the index says is an error, not the
// zeros a short buffer would write out.
func (rd *logRead) baseText(from, to int64) (string, error) {
	b := make([]byte, 0, to-from+1)
	buf := make([]byte, logReadBytes)
	for at, end := rd.byteAt(from), rd.byteAt(to); at < end; {
		n := min(int64(len(buf)), end-at)
		got, err := rd.f.ReadAt(buf[:n], at)
		if int64(got) < n {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return "", err
		}
		b = appendLF(b, buf[:n])
		at += n
	}
	return string(b), nil
}

// appendLF appends p to b with CR LF pairs as LF.
func appendLF(b, p []byte) []byte {
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			return append(b, p...)
		}
		if i > 0 && p[i-1] == '\r' {
			b = append(b, p[:i-1]...)
		} else {
			b = append(b, p[:i]...)
		}
		b = append(b, '\n')
		p = p[i+1:]
	}
}

// appendCRLF appends p to b with LF as CR LF.
func appendCRLF(b, p []byte) []byte {
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			return append(b, p...)
		}
		if i > 0 && p[i-1] == '\r' {
			b = append(b, p[:i]...)
		} else {
			b = append(b, p[:i]...)
			b = append(b, '\r')
		}
		b = append(b, '\n')
		p = p[i+1:]
	}
}

// mapRange maps the runes start up to end of the text to base runes,
// taking whole any edit the range reaches into, and returning the parts
// of the first and last such edits' texts before and after the range.
// The edits it reaches into are edits[lo:hi]; when it reaches into none,
// lo is where in edits a replacement of the base runes there goes. The
// edits must be locked for.
func (l *logDoc) mapRange(edits []logEdit, start, end int64) (from, to int64, prefix, suffix string, lo, hi int) {
	d := int64(0)
	first, last := -1, -1
	for i := range edits {
		e := &edits[i]
		at := e.baseStart + d // where the edit's text is at in the text
		runes := int64(utf8.RuneCountInString(e.text))
		if start < at+runes && end > at || start == end && start == at {
			if first < 0 {
				first = i
				if start > at {
					prefix = runesPrefix(e.text, int(start-at))
				}
				from = e.baseStart
			}
			last = i
			if end < at+runes {
				suffix = runesSuffix(e.text, int(end-at))
				to = e.baseEnd
			} else {
				to = max(to, e.baseEnd)
			}
		}
		d += runes - (e.baseEnd - e.baseStart)
	}
	if first >= 0 {
		return from, to, prefix, suffix, first, last + 1
	}
	// Between edits: the base runes of the range, and where among the
	// edits a replacement of them goes.
	at := 0
	from = baseOf(edits, start, &at)
	if end > start {
		to = baseOf(edits, end, nil)
	} else {
		to = from
	}
	return from, to, "", "", at, at
}

// textWith returns the text of the base runes from up to to with the
// edits given in place, as those are the ones being replaced.
func (rd *logRead) textWith(from, to int64, edits []logEdit) (string, error) {
	var b []byte
	pos := from
	for _, e := range edits {
		if e.baseStart > pos {
			s, err := rd.baseText(pos, min(e.baseStart, to))
			if err != nil {
				return "", err
			}
			b = append(b, s...)
		}
		b = append(b, e.text...)
		pos = e.baseEnd
	}
	if pos < to {
		s, err := rd.baseText(pos, to)
		if err != nil {
			return "", err
		}
		b = append(b, s...)
	}
	return string(b), nil
}

// applyEdit replaces the runes start up to end of the text with text,
// merging the edits the range reaches into, and returns the edit that
// undoes it in base runes. It returns ok false for an edit that changes
// nothing, and an error when the file cannot be read to say what it
// replaces.
func (l *logDoc) applyEdit(start, end int64, text string) (undo logEdit, ok bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	start, end = min(max(start, 0), l.len), min(max(end, 0), l.len)
	if start > end {
		start, end = end, start
	}
	rd, err := l.readLocked()
	if err != nil {
		return logEdit{}, false, err
	}
	defer rd.f.Close()
	from, to, prefix, suffix, lo, hi := l.mapRange(l.edits, start, end)
	return rd.replaceRange(l, from, to, prefix+text+suffix, lo, hi)
}

// applyBase makes the replacements of es, from the last in the base to
// the first, and returns those that undo them, in the order that undoes
// what applying the returned list undoes. Each replacement takes the
// edits within its base runes with it.
func (l *logDoc) applyBase(es []logEdit) (undo []logEdit, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rd, err := l.readLocked()
	if err != nil {
		return nil, err
	}
	defer rd.f.Close()
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		lo, hi := 0, 0
		for j := range l.edits {
			x := &l.edits[j]
			if x.baseStart >= e.baseStart && x.baseEnd <= e.baseEnd {
				if hi == lo {
					lo = j
				}
				hi = j + 1
			}
		}
		if lo == hi {
			// No edit is within the base runes: the replacement goes
			// among them by the rune it starts at.
			lo = sortSearchEdits(l.edits, e.baseStart)
			hi = lo
		}
		u, ok, err := rd.replaceRange(l, e.baseStart, e.baseEnd, e.text, lo, hi)
		if err != nil {
			return undo, err
		}
		if ok {
			undo = append(undo, u)
		}
	}
	return undo, nil
}

// sortSearchEdits returns where among the edits one that replaces base
// rune from goes, after any edit wholly before it.
func sortSearchEdits(edits []logEdit, from int64) int {
	lo, hi := 0, len(edits)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if edits[m].baseStart <= from {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// replaceRange replaces the base runes from up to to with text, taking the
// edits l.edits[lo:hi] with it, and returns the edit that undoes it. The
// mutex must be locked. It returns ok false for a replacement that changes
// nothing.
func (rd *logRead) replaceRange(l *logDoc, from, to int64, text string, lo, hi int) (logEdit, bool, error) {
	if from > to {
		from, to = to, from
	}
	old, err := rd.textWith(from, to, l.edits[lo:hi])
	if err != nil {
		return logEdit{}, false, err
	}
	if old == text {
		return logEdit{}, false, nil
	}
	undo := logEdit{from, to, old}
	keep := append(l.edits[:lo], l.edits[hi:]...)
	base, err := rd.baseText(from, to)
	if err != nil {
		return logEdit{}, false, err
	}
	if text != base {
		keep = slices.Insert(keep, min(lo, len(keep)), logEdit{from, to, text})
	}
	l.edits = keep
	l.len = l.file.end.runeOff
	for _, e := range l.edits {
		l.len += int64(utf8.RuneCountInString(e.text)) - (e.baseEnd - e.baseStart)
	}
	l.version++
	return undo, true, nil
}

// logWindow is the text of the lines around a place: the text of the
// window, the line of the text its first line is at, the rune its first
// rune is at, the lines it holds, the width of its longest line in
// columns, the line it shows in part, 0 for none, with the bytes of
// that line whole, and the line it starts past the cut of, 0 for none,
// with the bytes of that line whole.
type logWindow struct {
	text     string
	first    int
	start    int64
	lines    int
	widest   int
	cutLine  int   // the line, from 1, shown in part, 0 for none
	cutBytes int64 // the bytes of the line the window cuts, whole

	cutAbove      int   // the line, from 1, the window starts past the cut of, 0 for none
	cutAboveBytes int64 // the bytes of that line whole
}

// lineBytesAt returns the bytes of line k's text, its line ending left
// out, as the file has it.
func lineBytesAt(file *logFile, k int) int64 {
	end := file.size
	if k+1 < len(file.lines) {
		end = file.lines[k+1].byteOff
	}
	n := end - file.lines[k].byteOff
	return max(0, n-1) // the LF
}

// materialize makes the window of the lines around anchor, which with
// viewport lines in view holds as many again around it as the margins
// ask, as far as the window's byte budget goes. A line of the file
// longer than the editor lays out one shows in part, at maxLineBytes,
// and ends the window: what follows it is asked for as the window moves
// past.
func (l *logDoc) materialize(anchor, viewport int) (logWindow, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return logWindow{}, errLogClosed
	}
	l.mu.Unlock()
	// One reader of the file at a time, with the index as it is.
	l.io.Lock()
	defer l.io.Unlock()
	rd, err := l.openLogRead()
	if err != nil {
		return logWindow{}, err
	}
	defer rd.f.Close()
	l.mu.Lock()
	edits := slices.Clone(l.edits)
	length := l.len
	l.mu.Unlock()
	file := rd.file
	ps := pieces(file, edits)
	total := lineCountUnlocked(file, ps)
	anchor = max(0, min(anchor, total-1))
	first := max(0, anchor-logMarginLines)
	last := min(total-1, anchor+max(viewport, 1)+logMarginLines)
	end := length
	if last+1 < total {
		end = lineStartUnlocked(file, ps, total, last+1)
	}
	// The first line of the file in the window's base that is longer
	// than the editor lays out a line. One before the anchor's line is
	// not held: it would end the window shy of the anchor. The window
	// starts past it, and what stands before it is of the window that
	// the text had when the anchor stood before the cut.
	var from, baseFrom, baseEnd int64
	cutStop := int64(0)
	cutLine := 0 // the line of the file cut, from 1
	cutAbove := 0
	var cutAboveBytes int64
	pastCut := -1 // the line of the file the window has started past
	for {
		cutStop, cutLine = 0, 0
		from = lineStartUnlocked(file, ps, total, first)
		baseFrom, baseEnd = baseOf(edits, from, nil), baseOf(edits, end, nil)
		// The scan takes the window's first line too, the base's own
		// line of it: a long one ends the window at its cut, unless the
		// window starts past the cut of it already, in a line an edit
		// has made of it.
		scan := max(0, entryAt(file.lines, baseFrom)-1) // the line baseFrom is on
		for i := scan; i < entryAt(file.lines, baseEnd); i++ {
			if lineBytesAt(file, i) <= maxLineBytes || i <= pastCut {
				continue
			}
			if i == scan && rd.byteAt(baseFrom) >= file.lines[i].byteOff+maxLineBytes {
				continue // the window starts past the line's cut
			}
			cutStop = file.lines[i].byteOff + maxLineBytes
			cutLine = i + 1
			break
		}
		if cutLine == 0 {
			break
		}
		at := lineAtUnlocked(file, ps, length, curOf(edits, file.lines[cutLine-1].runeOff))
		if at >= anchor {
			break // the anchor's line or one past it: the anchor is in
		}
		first = at + 1
		// The cut stands above the window now: it says so, for scrolling
		// cannot reach past it.
		cutAbove, cutAboveBytes = at+1, lineBytesAt(file, cutLine-1)
		pastCut = cutLine - 1
	}
	b := make([]byte, 0, min(int(end-from)+1, 4*logWindowBytes))
	cut := false
	for i := range ps {
		p := &ps[i]
		if len(b) >= logWindowBytes {
			break // the budget is spent, the anchor's line is in
		}
		if p.edit {
			n := int64(utf8.RuneCountInString(p.text))
			if p.curRune >= end {
				break
			}
			if p.curRune+n <= from {
				continue
			}
			s := p.text
			if p.curRune < from {
				s = runesPrefix(s, int(from-p.curRune))
			}
			if p.curRune+n > end {
				s = runesPrefix(s, int(end-p.curRune))
			}
			b = append(b, s...)
			continue
		}
		// A run of the base: read it in as many reads as it takes.
		a, z := max(p.from, baseFrom), min(p.to, baseEnd)
		if z <= a {
			continue
		}
		stop := rd.byteAt(z)
		if cutLine > 0 {
			if rd.byteAt(a) >= cutStop {
				break // the line cut is where the window ends
			}
			stop = min(stop, cutStop)
		}
		for at := rd.byteAt(a); at < stop; {
			if len(b) >= logWindowBytes {
				break
			}
			n := min(int64(logReadBytes), stop-at)
			buf := make([]byte, n)
			got, err := rd.f.ReadAt(buf, at)
			if int64(got) < n {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				return logWindow{}, err
			}
			b = appendLF(b, buf)
			at += n
		}
		if cutLine > 0 && stop == cutStop {
			cut = true
			break // the line cut ends the window
		}
	}
	if len(b) > logWindowBytes {
		// The budget ends the window within a line, on a rune's edge.
		i := logWindowBytes
		for i > 0 && !utf8.RuneStart(b[i]) {
			i--
		}
		b = b[:i]
		cutLine = 0 // the budget cut, not a long line
	}
	w := logWindow{text: string(b), first: first, start: from}
	w.lines = strings.Count(w.text, "\n") + 1
	if cut {
		// The line of the text the cut line of the file is on.
		w.cutLine = lineAtUnlocked(file, ps, length, curOf(edits, file.lines[cutLine-1].runeOff)) + 1
		w.cutBytes = lineBytesAt(file, cutLine-1)
	}
	w.cutAbove, w.cutAboveBytes = cutAbove, cutAboveBytes
	c := &widthCounter{}
	c.Write(b)
	w.widest = max(c.widest, c.col)
	return w, nil
}

// runesPrefix returns the first n runes of s.
func runesPrefix(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// runesSuffix returns s past its first n runes.
func runesSuffix(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[i:]
		}
		n--
	}
	return ""
}

// saveTo writes the text to the file at path, beside it first and renamed,
// so that a failure keeps the old file, and returns what the saved file
// holds: its stat and the index of its lines. The edits written are those
// given, a snapshot of the document's; the document's own are for the
// caller to move over the new base.
func (l *logDoc) saveTo(path string, crlf bool, saved []logEdit) (fs.FileInfo, *logFile, error) {
	// One reader of the file at a time, so that the file it reads is the
	// one it puts in place. Its own hold of it ends before that: a file
	// cannot be renamed over while it is open on Windows.
	l.io.Lock()
	defer l.io.Unlock()
	rd, err := l.openLogRead()
	if err != nil {
		return nil, nil, err
	}
	closed := false
	shut := func() {
		if !closed {
			closed = true
			rd.f.Close()
		}
	}
	defer shut()
	// The file is not as it was read: a save would put the text over what
	// stands there now, cutting what was appended to it and writing past
	// where it was taken short. It fails where the person sees it, as a
	// remote conflict does, rather than write the file away.
	if rd.file.stamp != nil {
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, err
		}
		if now := stampOf(info); *now != *rd.file.stamp {
			return nil, nil, errLogChanged
		}
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".mayoi-*")
	if err != nil {
		return nil, nil, err
	}
	name := tmp.Name()
	fail := func(err error) (fs.FileInfo, *logFile, error) {
		tmp.Close()
		os.Remove(name)
		return nil, nil, err
	}
	w := bufio.NewWriterSize(tmp, logReadBytes)
	write := func(s string) error {
		if crlf && strings.Contains(s, "\n") {
			s = strings.ReplaceAll(s, "\n", "\r\n")
		}
		_, err := w.WriteString(s)
		return err
	}
	if rd.file.bom {
		if _, err := w.WriteString("\xEF\xBB\xBF"); err != nil {
			return fail(err)
		}
	}
	pos := int64(0)
	for _, e := range saved {
		if e.baseStart > pos {
			if err := rd.copyRange(w, pos, e.baseStart, crlf); err != nil {
				return fail(err)
			}
		}
		if err := write(e.text); err != nil {
			return fail(err)
		}
		pos = e.baseEnd
	}
	if pos < rd.file.end.runeOff {
		if err := rd.copyRange(w, pos, rd.file.end.runeOff, crlf); err != nil {
			return fail(err)
		}
	}
	shut() // the file is let go of before it is renamed over
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return nil, nil, err
	}
	os.Chmod(name, mode)
	// The index of the file as written, of the file beside it before it
	// takes its place.
	tf, err := os.Open(name)
	if err != nil {
		return fail(err)
	}
	ti, err := tf.Stat()
	if err != nil {
		tf.Close()
		return fail(err)
	}
	nf, err := scanLog(tf, ti.Size(), nil, nil, nil)
	tf.Close()
	if err != nil {
		return fail(err)
	}
	// The file takes its place. A reader of the path may hold it a
	// moment — an edit finding what it replaces — and is waited out.
	var renamed error
	for range logRenameWaits {
		if renamed = os.Rename(name, path); renamed == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if renamed != nil {
		os.Remove(name)
		return nil, nil, renamed
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	nf.stamp = stampOf(info)
	return info, nf, nil
}

// copyRange writes the file's text of the base runes from up to to, as
// the file has it, with the line endings crlf asks.
func (rd *logRead) copyRange(w *bufio.Writer, from, to int64, crlf bool) error {
	buf := make([]byte, logReadBytes)
	// A CR at the end of a read is of a pair only with the LF after it,
	// which only matters where the pairs go.
	var held bool
	for at, end := rd.byteAt(from), rd.byteAt(to); at < end; {
		n := min(int64(len(buf)), end-at)
		got, err := rd.f.ReadAt(buf[:n], at)
		if int64(got) < n {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return err
		}
		p := buf[:n]
		if held && len(p) > 0 {
			held = false
			if p[0] != '\n' {
				// A CR of the text's own, not of a pair.
				if _, err := w.Write([]byte{'\r'}); err != nil {
					return err
				}
			}
		}
		if p[len(p)-1] == '\r' && at+n < end {
			held = true
			p = p[:len(p)-1]
		}
		switch {
		case crlf == rd.file.crlf:
			_, err = w.Write(p)
		case crlf:
			_, err = w.Write(appendCRLF(nil, p))
		default:
			_, err = w.Write(appendLF(nil, p))
		}
		if err != nil {
			return err
		}
		at += n
	}
	if held {
		// The range ends with a CR of its own.
		if _, err := w.Write([]byte{'\r'}); err != nil {
			return err
		}
	}
	return nil
}
