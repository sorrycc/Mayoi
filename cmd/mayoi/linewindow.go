package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// lineWindow is the text of a run of lines, read from the buffer at once,
// for code that looks at many lines around one: reading each line from the
// buffer's tree on its own costs far more than the text.
type lineWindow struct {
	first  int   // the line the window starts at
	starts []int // the byte offset of each line in text, and of its end
	text   string
}

// newLineWindow reads lines from up to to (exclusive) of a snapshot,
// clamped to it.
func newLineWindow(snap ui.TextSnapshot, from, to int) *lineWindow {
	from, to = max(from, 0), min(to, snap.LineCount())
	w := &lineWindow{first: from}
	if from >= to {
		return w
	}
	s, _ := snap.LineRange(from)
	_, e := snap.LineRange(to - 1)
	w.text = snap.Slice(s, e)
	w.starts = make([]int, 0, to-from+1)
	w.starts = append(w.starts, 0)
	for i := 0; ; {
		k := strings.IndexByte(w.text[i:], '\n')
		if k < 0 {
			break
		}
		i += k + 1
		w.starts = append(w.starts, i)
	}
	w.starts = append(w.starts, len(w.text)+1)
	return w
}

// covers reports whether the window has lines from up to to (exclusive).
func (w *lineWindow) covers(from, to int) bool {
	return from >= w.first && to-w.first <= len(w.starts)-1
}

// window returns a window of lines from up to to (exclusive) of a
// snapshot of d, clamped to it. Read again as the view scrolls, the
// window is read with as many lines again around it and kept, so that
// the frames that follow, scrolling line by line, find theirs in it. A
// text just changed, as each key typed changes it, is read only as far
// as asked: the next change would drop the rest unread.
func (d *editor) window(snap ui.TextSnapshot, from, to int) *lineWindow {
	from, to = max(from, 0), min(to, snap.LineCount())
	w := d.win
	if w != nil && d.winVersion == snap.Version() && w.covers(from, to) {
		return w
	}
	if w != nil && d.winVersion == snap.Version() {
		from, to = from-maxLineScan, to+maxLineScan
	}
	d.win, d.winVersion = newLineWindow(snap, from, to), snap.Version()
	return d.win
}

// has reports whether line i is in the window.
func (w *lineWindow) has(i int) bool { return i >= w.first && i-w.first < len(w.starts)-1 }

// line returns the text of line i, which must be in the window.
func (w *lineWindow) line(i int) string {
	k := i - w.first
	return w.text[w.starts[k] : w.starts[k+1]-1]
}

// indent returns how many columns line i's indent takes, -1 for a blank
// line or one outside the window.
func (w *lineWindow) indent(i int) int {
	if !w.has(i) {
		return -1
	}
	text := w.line(i)
	lead := leading(text)
	if len(lead) == len(text) {
		return -1
	}
	return columns(lead)
}
