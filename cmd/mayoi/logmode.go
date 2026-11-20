package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// The app's side of log mode: when a local file opens in it, how its
// window moves as the view scrolls, and how it saves and reloads. The
// editors keep their places in the window's own runes, which a window
// put in its place moves for them; only the places kept beyond the app —
// a session's, a closed tab's, a jump's — are of the text as a whole.

// logThreshold is the size over which a local file opens in log mode, 0
// when the settings have it off.
func (a *app) logThreshold() int64 {
	if mb := a.prefs().LargeFileModeMB; mb > 0 {
		return int64(mb) << 20
	}
	return 0
}

// loadLog reads a document's file whole, in the background, with its
// progress in the loading view: a file over the threshold shows all of
// its text, however big. One file is read at a time; the rest wait
// their turn, as documents queued whole-file reads do.
func (a *app) loadLog(d *document) {
	d.sync, d.loadErr, d.readingAt = syncLoading, nil, time.Time{}
	if !slices.Contains(a.logQueue, d) {
		a.logQueue = append(a.logQueue, d)
	}
	a.pumpLog()
}

// logWholeMax is the largest file that opens whole, over the threshold.
const logWholeMax = 2 << 30

// pumpLog starts the reads the queue holds, one at a time.
func (a *app) pumpLog() {
	if a.logReading || a.loadsClosed || len(a.logQueue) == 0 {
		return
	}
	d := a.logQueue[0]
	a.logQueue, a.logReading = a.logQueue[1:], true
	path := d.path
	root := a.root
	bg.Go(func() {
		got := readWholeFile(path, func(read, total int64) {
			a.post(func() {
				if d.sync == syncLoading && total > 0 {
					d.logProgress = float32(read) / float32(total)
				}
			})
		})
		var buf *ui.TextBuffer
		var widest int
		var indent indentStyle
		if got.err == nil {
			// Built in the background: a buffer of the whole text, and
			// what a frame would otherwise walk it for.
			buf = ui.NewTextBuffer(got.text)
			snap := buf.Snapshot()
			w := &widthCounter{}
			snap.WriteTo(w)
			widest = max(w.widest, w.col)
			indent = detectIndent(snap, path)
		}
		a.post(func() {
			a.logReading = false
			a.pumpLog()
			d.logProgress = 0
			if !a.loadsClosed && a.root == root && slices.Contains(a.docs, d) && d.path == path && d.sync == syncLoading && !d.dirty() && got.err == nil {
				d.logWhole = true
				d.buf.Restore(buf.Snapshot())
				d.hist = history{}
				d.markSaved()
				d.crlf, d.diskCRLF = got.crlf, got.crlf
				d.encoding = got.enc
				d.indent = indent
				d.widest, d.widestVersion = widest, d.buf.Version()
				d.stamp = got.stamp
				d.longLine = 0
				d.sync, d.loadErr = syncIdle, nil
				for _, ed := range d.views {
					if ts := ed.restoreTab; ts != nil {
						ed.restoreTab = nil
						a.restoreEditor(ed, *ts)
					}
					if target := ed.loadedTarget; target != nil {
						ed.loadedTarget = nil
						target(ed)
					}
				}
				d.readDone()
				a.find.stale = true
				a.invalidate()
				return
			}
			if slices.Contains(a.docs, d) && d.path == path && d.sync == syncLoading {
				if got.err != nil {
					d.sync, d.loadErr = syncLoadFailed, got.err
				} else {
					d.sync = syncIdle // a reload of a document typed in: its text stays
				}
				d.readDone()
				a.invalidate()
				return
			}
			d.readDone()
		})
	})
}

// wholeFile is a file read whole: its text, in UTF-8 with LF line
// endings, how its line endings and encoding were, and the file's
// stamp, or why it could not be read.
type wholeFile struct {
	text  string
	crlf  bool
	enc   *textEncoding
	stamp *fileStamp
	err   error
}

// readWholeFile reads a local file whole, telling progress of the bytes
// read. A line of any length is read as it is: the text shows in full.
func readWholeFile(path string, progress func(read, total int64)) wholeFile {
	f, err := os.Open(path)
	if err != nil {
		return wholeFile{err: err}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return wholeFile{err: err}
	}
	if info.IsDir() {
		return wholeFile{err: &fs.PathError{Op: "read", Path: path, Err: errors.New("is a directory")}}
	}
	if info.Size() > logWholeMax {
		return wholeFile{err: errors.New(locales.ErrTooLarge())}
	}
	total := info.Size()
	var out bytes.Buffer
	out.Grow(int(min(total, 1<<30)))
	chunk := make([]byte, 4<<20)
	for {
		n, err := f.Read(chunk)
		out.Write(chunk[:n])
		if progress != nil && total > 0 {
			progress(int64(out.Len()), total)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return wholeFile{err: err}
		}
	}
	data := out.Bytes()
	text, enc, ok := decodeText(data, systemText())
	if !ok {
		return wholeFile{err: errBinary}
	}
	crlf := bytes.Contains(text, []byte("\r\n"))
	if crlf {
		text = bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
	}
	return wholeFile{text: string(text), crlf: crlf, enc: enc, stamp: stampOf(info)}
}

// logViewport is how many lines the views of a document show, a guess
// before one has laid any out.
func (a *app) logViewport(d *document) int {
	n := 50
	if ed := d.main(); ed != nil && a.font.lineH > 0 && ed.lastView.H > 0 {
		n = int(ed.lastView.H / a.font.lineH)
	}
	return n
}

// logMove builds the window of a document's text around a line in the
// background and puts it in the document's buffer when it is ready. A
// window asked for the same place of the same text is the one in place:
// it is not built again.
func (a *app) logMove(d *document, anchor int) {
	l := d.log
	if l == nil || l.moving {
		return
	}
	version := l.versionOf()
	if l.wantLin == anchor && l.wantVer == version {
		return
	}
	viewport := a.logViewport(d)
	l.moving, l.wantLin, l.wantVer = true, anchor, version
	bufVer := d.buf.Version()
	path := d.path
	bg.Go(func() {
		w, err := l.materialize(anchor, viewport)
		if err != nil {
			a.post(func() {
				l.moving = false
				if slices.Contains(a.docs, d) && d.path == path && d.log == l {
					if d.sync == syncLoading {
						d.sync, d.loadErr = syncLoadFailed, err
						d.readDone()
					}
					// A window of a file gone under the view: the view
					// keeps what it has.
				}
			})
			return
		}
		buf := ui.NewTextBuffer(w.text)
		a.post(func() { a.logInstall(d, path, l, w, buf, version, bufVer) })
	})
}

// logInstall puts a window built in the background into a document's
// buffer, moving the editors' places with it.
func (a *app) logInstall(d *document, path string, l *logDoc, w logWindow, buf *ui.TextBuffer, version, bufVer uint64) {
	l.moving = false
	if !slices.Contains(a.docs, d) || d.path != path || d.log != l {
		return
	}
	if l.versionOf() != version || d.buf.Version() != bufVer {
		return // typed since: another window is asked for as it is needed
	}
	old, oldLen := l.win, d.buf.Len()
	d.buf.Restore(buf.Snapshot())
	l.win = logWin{first: w.first, start: w.start, lines: w.lines, cutLine: w.cutLine, cutBytes: w.cutBytes,
		cutAbove: w.cutAbove, cutAboveBytes: w.cutAboveBytes}
	newLen := int64(d.buf.Len())
	lineH := a.font.lineH
	// A place of the window's goes to the same place of the text.
	to := func(x int) int {
		if !old.set() {
			return min(max(x, 0), int(newLen))
		}
		at := min(max(int64(x), 0), int64(oldLen)) + old.start
		return int(min(max(at-w.start, 0), newLen))
	}
	for _, ed := range d.views {
		ed.sel = [2]int{to(ed.sel[0]), to(ed.sel[1])}
		ed.caret, ed.parked = to(ed.caret), -1
		if ed.pending != nil {
			ed.pending = &[2]int{to(ed.pending[0]), to(ed.pending[1])}
		}
		if p := ed.logSel; p != nil {
			// A jump asked for in the text: it is in the window now.
			ed.pending = &[2]int{int(min(max(p[0]-w.start, 0), newLen)), int(min(max(p[1]-w.start, 0), newLen))}
			ed.logSel = nil
		}
		ed.closers = edLogMove(ed.closers, to)
		ed.hiddenAt = to(ed.hiddenAt)
		if old.set() && lineH > 0 {
			// The line at the top of the view stays at the top.
			top := int(min(max((ed.vscroll.Y-editorPadY)/lineH, 0), float32(old.lines-1)))
			ed.vscroll.Y = editorPadY + float32(max(0, min(top+old.first-w.first, w.lines-1)))*lineH
		}
		ed.resync, ed.lateLine, ed.sticky = true, -1, ed.sticky[:0]
	}
	d.marks = edLogMoveMarks(d.marks, to)
	d.changed = to(d.changed)
	d.tracked = edLogMove(d.tracked, to)
	for _, p := range d.navs {
		p.at = to(p.at)
	}
	d.hist.snap, d.hist.typing = d.buf.Snapshot(), false
	d.widest, d.widestVersion = w.widest, d.buf.Version()
	a.find.stale = true
	if !old.set() {
		// The first window ends the read.
		d.sync, d.loadErr, d.longLine = syncIdle, nil, 0
		d.crlf, d.diskCRLF, d.encoding = l.file.crlf, l.file.crlf, nil
		if l.file.bom {
			d.encoding = utf8BOMText
		}
		d.stamp = l.file.stamp
		d.indent = detectIndent(d.buf.Snapshot(), d.path)
		for _, ed := range d.views {
			if ts := ed.restoreTab; ts != nil {
				ed.restoreTab = nil
				a.restoreEditor(ed, *ts)
			}
			if target := ed.loadedTarget; target != nil {
				ed.loadedTarget = nil
				target(ed)
			}
		}
		d.readDone()
	}
	a.invalidate()
}

// edLogMove moves the offsets of a slice by to.
func edLogMove(s []int, to func(int) int) []int {
	out := s[:0]
	for _, x := range s {
		out = append(out, to(x))
	}
	return out
}

// edLogMoveMarks moves vim's marks by to.
func edLogMoveMarks(marks map[rune]int, to func(int) int) map[rune]int {
	for r, x := range marks {
		marks[r] = to(x)
	}
	return marks
}

// logEnsure keeps a log document's window around the view: as the view
// reaches an edge of it, or a jump asks for a place out of it, the window
// moves there. A line the window shows in part is as far as it goes: the
// window moves past it, to the lines after.
func (a *app) logEnsure(ed *editor) {
	d := ed.document
	l := d.log
	if l == nil || l.moving || d.sync != syncIdle && d.sync != syncLoading {
		return
	}
	if p := ed.logSel; p != nil {
		line := l.lineAt(p[1])
		if l.win.set() && line >= l.win.first+logMarginLines && line < l.win.first+l.win.lines-logMarginLines {
			// In the window: the frame's own selection takes it.
			lo := int(min(max(p[0]-l.win.start, 0), int64(d.buf.Len())))
			hi := int(min(max(p[1]-l.win.start, 0), int64(d.buf.Len())))
			ed.pending, ed.logSel = &[2]int{lo, hi}, nil
			return
		}
		a.logMove(d, line)
		return
	}
	if r := ed.pending; r != nil && !ed.quiet && !ed.typedSel && l.win.set() {
		// A jump landing this frame, in the window's own runes. The window
		// holds for it — not by the caret it moves from, which the
		// window's move carried to an edge of this one — or goes to it,
		// when it lands out of the window altogether.
		line := l.lineAt(l.win.start + int64(min(max(r[1], 0), d.buf.Len())))
		if line < l.win.first || line >= l.win.first+l.win.lines {
			a.logMove(d, line)
		}
		return
	}
	if !l.win.set() || a.font.lineH <= 0 {
		return
	}
	// The lines in view, in the window's own numbers.
	view := int(ed.lastView.H / a.font.lineH)
	if view <= 0 {
		view = 1
	}
	top := int(min(max((ed.vscroll.Y-editorPadY)/a.font.lineH, 0), float32(l.win.lines-1)))
	// The caret on the line the window shows in part holds the window
	// there, as a jump to the line put it: it is seen, in part.
	cutHeld := func() bool {
		if l.win.cutLine <= 0 {
			return false
		}
		snap := d.buf.Snapshot()
		return snap.LineAt(min(ed.sel[1], snap.Len())) >= l.win.lines-1
	}
	// A window that ends at a line shown in part cannot hold the view:
	// it moves past that line.
	past := func(anchor int) int {
		if l.win.cutLine > 0 && anchor >= l.win.cutLine-1 {
			return l.win.cutLine
		}
		return anchor
	}
	switch {
	case top < logMarginLines && l.win.first > 0:
		a.logMove(d, past(l.win.first+top))
	case top+view > l.win.lines-logMarginLines:
		if total := l.lineCount(); l.win.first+l.win.lines < total && !cutHeld() {
			a.logMove(d, past(l.win.first+top))
		}
	case ed.sel[1] >= d.buf.Len()-1 && d.buf.Len() > 0:
		// The caret at the end of the window, of a text with more to show.
		if total := l.lineCount(); l.win.first+l.win.lines < total && !cutHeld() {
			a.logMove(d, past(l.win.first+l.win.lines-1))
		}
	}
}

// logSelect asks for a selection of a log document's text, of the whole
// of it rather than the window.
func (d *editor) logSelect(start, end int64) {
	s, e := min(start, end), max(start, end)
	d.logSel = &[2]int64{s, e}
}

// writeLog saves a log document in the background, for the saves waiting,
// which it answers.
func (a *app) writeLog(d *document) {
	d.sync = syncSaving
	l := d.log
	if l == nil {
		d.sync = syncIdle
		a.answerSaves(d, errNotSaved)
		a.nextSave(d)
		return
	}
	crlf, path := d.crlf, d.path
	saved := l.editsCopy()
	bg.Go(func() {
		_, nf, err := l.saveTo(path, crlf, saved)
		a.post(func() {
			if err != nil {
				d.sync = syncIdle
				a.answerSaves(d, err)
				a.nextSave(d)
				return
			}
			if slices.Contains(a.docs, d) && d.path == path && d.log == l {
				// The file is the text: its index is the base now, and
				// the edits made since the save began move onto it. The
				// undo steps are of the base that was.
				l.takeFile(nf, saved)
				d.hist.undo, d.hist.redo = nil, nil
				d.stamp = nf.stamp
				d.diskCRLF = crlf
			}
			d.sync = syncIdle
			a.answerSaves(d, nil)
			a.nextSave(d)
		})
	})
}

// reloadLog makes a log document's text the file's on disk, as reloadDoc
// does, when the file is not as it was last read or written: that costs a
// stat, not the file.
func (a *app) reloadLog(d *document) {
	switch d.sync {
	case syncLoadFailed:
		d.logSkipped = false
		if d.log != nil {
			d.log.close()
			d.log = nil
		}
		a.loadLog(d)
		return
	case syncIdle, syncConflict:
	default:
		return
	}
	l := d.log
	if l == nil {
		return
	}
	old := d.stamp
	path := d.path
	bg.Go(func() {
		ld, err := openLogDoc(path)
		a.post(func() {
			if err != nil || ld == nil {
				return // the file stays as the document has it
			}
			if !slices.Contains(a.docs, d) || d.path != path || d.log != l || d.sync != syncIdle && d.sync != syncConflict || d.dirty() || d.stamp != old {
				return
			}
			// A clean document of the file as it is: the same text, of
			// the file's own lines again.
			win := l.win
			l.takeFile(ld.file, nil)
			d.stamp = ld.file.stamp
			d.hist.undo, d.hist.redo = nil, nil
			l.win = logWin{}
			d.readDone()
			// The window is of the text that was; build it again where
			// the view was, as far as the file now goes.
			a.logMove(d, min(win.first, max(0, l.lineCount()-1)))
		})
	})
}

// viewLines is how many lines a document of any kind has: the whole of a
// log document's text, the buffer's own otherwise.
func (d *document) viewLines() int {
	if d.log != nil {
		return d.log.lineCount()
	}
	return d.buf.LineCount()
}

// logFirstLine is the line of the text a document's buffer's first line
// is at, 0 for a document that is not in log mode.
func (d *document) logFirstLine() int {
	if d.log != nil {
		return d.log.win.first
	}
	return 0
}

// bigSaveBytes is the size over which a local document saves in the
// background, so that writing it does not hold the frames.
const bigSaveBytes = 8 << 20

// writeWhole saves a big local document in the background, for the
// saves waiting, which it answers. It refuses to write a file that
// changed on disk since the document read it.
func (a *app) writeWhole(d *document) {
	d.sync = syncSaving
	snap := d.buf.Snapshot()
	crlf, enc, path, stamp := d.crlf, d.encoding, d.path, d.stamp
	bg.Go(func() {
		var b bytes.Buffer
		b.Grow(snap.ByteLen())
		snap.WriteTo(&b)
		data := b.Bytes()
		if crlf {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		var info fs.FileInfo
		var err error
		if data, err = encodeText(data, enc); err == nil {
			if info, err = os.Stat(path); err == nil {
				if now := stampOf(info); stamp == nil || *now != *stamp {
					err = errLogChanged
				}
			}
		}
		if err == nil {
			info, err = vfs.WriteFile(path, data, 0o644)
		}
		a.post(func() {
			if err == nil && slices.Contains(a.docs, d) && d.path == path {
				d.wrote(snap, crlf)
				if info != nil {
					d.stamp = stampOf(info)
				}
				d.diskCRLF = crlf
			}
			d.sync = syncIdle
			a.answerSaves(d, err)
			a.nextSave(d)
		})
	})
}
