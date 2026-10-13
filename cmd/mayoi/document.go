package main

import (
	"bytes"
	"errors"
	"mayoi/internal/remotefs"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/gitmarks"
	"mayoi/internal/highlight"
)

// maxOpenBytes is the largest file the editor opens.
const maxOpenBytes = 64 << 20

var errBinary = errors.New("binary file")

// document is a file open in the editor: its text, and what follows from
// the text alone. Each tab that shows it has an editor of its own.
type document struct {
	remoteSaving, remoteSaveAgain bool
	remoteAfterSave               []func()
	id                            int
	path                          string // absolute; "" for an untitled document
	title                         string // an untitled document's name
	buf                           *ui.TextBuffer
	saved                         uint64 // the buffer's version on disk
	// savedSnap is the text on disk, which undoing back to makes the
	// document clean again.
	savedSnap ui.TextSnapshot
	crlf      bool // saved with CRLF line endings
	diskCRLF  bool // the file on disk has CRLF line endings
	hl        *highlight.Highlighter
	indent    indentStyle
	hist      history
	git       gitmarks.Marks

	// widest is the longest line in columns seen, which sizes the text
	// area's width; widestVersion is the version it was last measured at
	// in full.
	widest        int
	widestVersion uint64
	diskGone      bool // the file was removed from the disk
	autoSave      autoSave

	// views are the editors that show the document, and typist the one
	// that has the keyboard, or had it last, whose typing its is.
	views  []*editor
	typist *editor
}

// editor is a document as a tab shows it: its selection, its place, and
// what is painted around its text. The editors of a document share its
// text and its undo history, so that a file shows in several columns.
type editor struct {
	*document
	// cmds are edits asked for by keys and menus, which the next frame
	// makes, once the editor has its selection.
	cmds []func(d *editor)
	deco decorations
	// closers are the offsets of the closing brackets and quotes the
	// editor put after the caret, which typing them steps over.
	closers []int
	// sticky are the lines stuck at the top of the editor in the last
	// frame, which a click goes to.
	sticky []int
	scopes scopeCache
	// win is the text of the lines around the view, of version winVersion,
	// which the decorations and the scopes read their lines from.
	win        *lineWindow
	winVersion uint64
	colors     colorCache
	// measured is the text and the lines in view whose widths the editor
	// last took into widest.
	measured struct {
		version     uint64
		first, last int
	}
	selShape selectionShape
	stuck    stickyCache
	overview overview

	vscroll ui.ScrollState // the text area's
	hscroll ui.ScrollState // the container that scrolls it sideways
	// lines is where the text area lays out the lines, while it wraps
	// them.
	lines ui.TextLines
	// wrap is the tab's own choice to wrap its lines or not, made while
	// the settings' WordWrap was of generation wrapGen; nil to follow it.
	wrap    *bool
	wrapGen int
	// rows are where the lines in view wrap, as the last frame laid them
	// out.
	rows wrapRows

	caret     int    // the caret's rune offset in the last frame
	sel       [2]int // the selection in the last frame
	caretSeen bool
	// pending is a selection the app asked for, which the next frame
	// makes, scrolling the editor to it.
	pending *[2]int
	// version is the version of the text the editor showed last.
	version uint64
	// resync has the next frame give the text area the selection, which
	// an edit made in another editor moved, without scrolling to it.
	resync bool
	// parked is where the text area's caret waits, in view, while the
	// selection is out of view and the keyboard elsewhere, -1 otherwise:
	// the area scrolls to its caret when its text changes under it.
	parked int
	// hidden is set while another tab shows in the editor's place, and
	// hiddenY is where it was scrolled then.
	hidden  bool
	hiddenY float32
	// pinFrame is the frame that gave the text area a selection the view
	// is not to follow, 0 for none, and pinY where the editor was scrolled
	// then, which the frame after puts back.
	pinFrame int
	pinY     float32
	// preview shows an SVG document as a picture in the editor's place,
	// nil while it shows the text.
	preview *viewer
	// gutterW is how wide the line numbers were in the last frame; peek
	// the first line of the hunk whose box shows under it, -1 for none.
	gutterW float32
	peek    int
	blame   blameState
	vim     vimState
}

// newEditor makes an editor of doc, its caret at the start.
func newEditor(doc *document) *editor {
	d := &editor{document: doc, parked: -1, peek: -1, version: doc.buf.Version()}
	doc.views = append(doc.views, d)
	if doc.typist == nil {
		doc.typist = d
	}
	return d
}

// release takes the editor from its document, as its tab closes.
func (d *editor) release() {
	doc := d.document
	doc.views = slices.DeleteFunc(doc.views, func(o *editor) bool { return o == d })
	if doc.typist == d {
		doc.typist = nil
		if len(doc.views) > 0 {
			doc.typist = doc.views[0]
		}
	}
}

// main returns the editor that edits of the document as a whole are
// made in, as reloading it from the disk: the one typed in last.
func (d *document) main() *editor {
	if d.typist == nil && len(d.views) > 0 {
		return d.views[0]
	}
	return d.typist
}

func (d *document) name() string {
	if d.path == "" {
		return d.title
	}
	return filepath.Base(d.path)
}

func (d *document) dirty() bool { return d.buf.Version() != d.saved || d.crlf != d.diskCRLF }

// readDocument reads a file into a document.
func readDocument(id int, path string, notify func()) (*document, error) {
	info, err := remotefs.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxOpenBytes {
		return nil, errors.New("file is too large to open")
	}
	data, err := remotefs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		return nil, errBinary
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	if crlf {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	d := newDocument(id, path, string(data), highlight.New(highlight.Lexer(path), notify))
	d.crlf, d.diskCRLF = crlf, crlf
	return d, nil
}

// newDocument makes a document of text, clean, with the indentation its
// lines use.
func newDocument(id int, path, text string, hl *highlight.Highlighter) *document {
	d := &document{id: id, path: path, buf: ui.NewTextBuffer(text), hl: hl}
	d.markSaved()
	d.indent = detectIndent(d.buf.Snapshot(), path)
	d.measureWidest()
	return d
}

// isBinary reports whether data looks like something other than text: a
// NUL byte, or invalid UTF-8, in its first 8 KB.
func isBinary(data []byte) bool {
	head := data[:min(len(data), 8000)]
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	if len(head) < len(data) && len(head) > 0 {
		// A rune cut at the end of the head is not invalid.
		i := len(head) - 1
		for i > 0 && !utf8.RuneStart(head[i]) && len(head)-i < utf8.UTFMax {
			i--
		}
		if !utf8.FullRune(head[i:]) {
			head = head[:i]
		}
	}
	return !utf8.Valid(head)
}

// save writes the document to its path.
func (d *document) save() error {
	snap := d.buf.Snapshot()
	var b bytes.Buffer
	b.Grow(snap.ByteLen())
	if _, err := snap.WriteTo(&b); err != nil {
		return err
	}
	data := b.Bytes()
	if d.crlf {
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	mode := os.FileMode(0o644)
	if info, err := remotefs.Stat(d.path); err == nil {
		mode = info.Mode().Perm()
	}
	if remotefs.IsRemote(d.path) {
		if err := remotefs.WriteFile(d.path, data, mode); err != nil {
			return err
		}
		d.savedSnap, d.saved = snap, snap.Version()
		d.diskCRLF = d.crlf
		return nil
	}
	// Write beside the file and rename, so that a failure keeps the old one.
	tmp, err := os.CreateTemp(filepath.Dir(d.path), "."+filepath.Base(d.path)+".mayoi-*")
	if err != nil {
		return remotefs.WriteFile(d.path, data, mode)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Chmod(tmp.Name(), mode)
	if err := remotefs.Rename(tmp.Name(), d.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	d.savedSnap, d.saved = snap, snap.Version()
	d.diskCRLF = d.crlf
	return nil
}

// measureWidest measures the longest line of the whole document.
func (d *document) measureWidest() {
	snap := d.buf.Snapshot()
	w := &widthCounter{}
	snap.WriteTo(w)
	d.widest = max(w.widest, w.col)
	d.widestVersion = snap.Version()
}

// widthCounter finds the longest line, in columns, of what is written to
// it.
type widthCounter struct{ col, widest int }

func (w *widthCounter) Write(p []byte) (int, error) {
	for i := 0; i < len(p); {
		c := p[i]
		switch {
		case c == '\n':
			w.widest = max(w.widest, w.col)
			w.col = 0
			i++
		case c == '\t':
			w.col += tabWidth - w.col%tabWidth
			i++
		case c < utf8.RuneSelf:
			w.col++
			i++
		default:
			_, n := utf8.DecodeRune(p[i:])
			w.col++
			i += n
		}
	}
	return len(p), nil
}

// columns returns how many columns s takes, with tabs to the next stop.
func columns(s string) int {
	col := 0
	for _, r := range s {
		if r == '\t' {
			col += tabWidth - col%tabWidth
		} else {
			col++
		}
	}
	return col
}

// selectRange selects runes start up to end in the next frame, and
// scrolls the editor to show them, nearer its top than its bottom when
// center is set or they are out of view.
func (d *editor) selectRange(start, end int, center bool) {
	d.pending = &[2]int{start, end}
}
