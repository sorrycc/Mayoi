package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// markdownSync keeps a Markdown preview in step with the text of its
// document, as VS Code's does. Shown in another column, scrolling the text
// scrolls the preview to the same place, and scrolling the preview scrolls
// the text, block by block and within a block by its share of its lines.
// Switched to in the text's place, the preview opens where the text was,
// and the text opens again where the preview was.
//
// A place is a line of the text, with how far down it: 0 is the start,
// and markdownEnd the end.
type markdownSync struct {
	// rows are the blocks' rows, as the last frame laid them out.
	rows []ui.Handle
	// textY is the text's offset as the preview last followed it, or set
	// it, and resync has the preview follow it again, as its rows moved.
	textY  float32
	resync bool
	// want is the place the preview is going to, while placing, and tries
	// counts the frames it took, as the list measures the rows it guessed.
	want    float32
	placing bool
	tries   int
	// ownY is the preview's offset as the last frame found it, and again
	// has the next frame read the rows once more after it moved.
	ownY  float32
	again bool
	// top is the place at the top of the preview in the last frame, which
	// the text opens at as the preview closes.
	top float32
	// userAt is when the user last scrolled the preview, which leads
	// while it scrolls: the text follows it, and not the other way.
	userAt time.Time
}

// markdownEnd is the place of the end of the text.
const markdownEnd = math.MaxFloat32

// markdownLead is how long after the user last scrolled the preview, as
// its momentum goes on, it leads the text.
const markdownLead = 400 * time.Millisecond

// markdownSource is the editor showing the text of a preview's document in
// another column, nil when none does.
func (a *app) markdownSource(v *viewer) *editor {
	for _, col := range a.cols {
		if t := col.tab(); t != nil && t.ed != nil && t.ed.document == v.doc && t.ed.preview == nil {
			return t.ed
		}
	}
	return nil
}

// markdownTouched notes the user scrolling the preview, or pressing in
// it, which then leads the text for a moment.
func (m *markdownPreview) markdownTouched(ev ui.InputEvent) {
	switch ev.Kind {
	case ui.InputScroll, ui.InputPointerDown, ui.InputKeyDown:
		m.sync.userAt = time.Now()
	}
}

// textPlace is the place at the top of an editor's text scrolled to y.
func (a *app) textPlace(d *editor, y float32) float32 {
	if d.vscroll.MaxY > 0 && y >= d.vscroll.MaxY-0.5 {
		return markdownEnd
	}
	y -= editorPadY
	if y <= 0 {
		return 0
	}
	g := a.geometry(d, d.buf.Snapshot().LineCount())
	at := g.at(y)
	line := float32(at)
	if h := g.top(at+1) - g.top(at); h > 0 {
		line += clamp((y-g.top(at))/h, 0, 1)
	}
	return line
}

// textY is the offset that puts a place at the top of an editor's text.
func (a *app) textY(d *editor, place float32) float32 {
	lines := d.buf.Snapshot().LineCount()
	g := a.geometry(d, lines)
	if place == markdownEnd {
		return max(d.vscroll.MaxY, editorPadY+g.height())
	}
	at := min(int(place), lines-1)
	return editorPadY + g.top(at) + (place-float32(at))*(g.top(at+1)-g.top(at))
}

// markdownGroup is the run of blocks from i that start on the same line,
// as an alert's title does with its text, and the line after them.
func (m *markdownPreview) markdownGroup(i int) (first, last, next int) {
	first, last = i, i
	for first > 0 && m.blocks[first-1].line == m.blocks[i].line {
		first--
	}
	for last+1 < len(m.blocks) && m.blocks[last+1].line == m.blocks[i].line {
		last++
	}
	next = m.lines
	if last+1 < len(m.blocks) {
		next = m.blocks[last+1].line
	}
	return first, last, max(next, m.blocks[i].line+1)
}

// markdownRowY is where row i of the preview starts below the top of the
// preview, as the last frame laid it out, and whether it did.
func (a *app) markdownRowY(c *ui.Context, v *viewer, i int) (float32, bool) {
	m := v.markdown
	if i < 0 || i >= len(m.sync.rows) {
		return 0, false
	}
	first, last := m.list.Visible()
	if i < first || i > last {
		return 0, false
	}
	r := m.sync.rows[i].Bounds(c)
	if r.W == 0 && r.H == 0 {
		return 0, false
	}
	return r.Y - v.pane.Bounds(c).Y, true
}

// markdownExtent is where the blocks from first to last start below the
// top of the preview, and how far they go, to where the next starts.
func (a *app) markdownExtent(c *ui.Context, v *viewer, first, last int) (top, height float32, ok bool) {
	top, ok = a.markdownRowY(c, v, first)
	if !ok {
		return 0, 0, false
	}
	if next, ok := a.markdownRowY(c, v, last+1); ok {
		return top, next - top, true
	}
	r := v.markdown.sync.rows[last].Bounds(c)
	end, ok := a.markdownRowY(c, v, last)
	return top, end + r.H - top, ok
}

// previewPlace is the place at the top of the preview, as the last frame
// laid its rows out, and whether it found it.
func (a *app) previewPlace(c *ui.Context, v *viewer) (float32, bool) {
	m := v.markdown
	switch {
	case len(m.blocks) == 0 || m.scroll.Y <= 0:
		return 0, true
	case m.list.AtEnd():
		return markdownEnd, true
	}
	first, last := m.list.Visible()
	i := -1
	for k := max(first, 0); k <= last; k++ {
		if top, ok := a.markdownRowY(c, v, k); ok && top <= 0 {
			i = k
		}
	}
	if i < 0 {
		return 0, false
	}
	first, last, next := m.markdownGroup(i)
	top, height, ok := a.markdownExtent(c, v, first, last)
	if !ok || height <= 0 {
		return 0, false
	}
	return float32(m.blocks[first].line) + clamp(-top/height, 0, 1)*float32(next-m.blocks[first].line), true
}

// placePreview scrolls the preview to show a place at its top, from where
// the last frame laid out its rows, as it is when the user does not scroll
// it. A place out of view, or rows the list had guessed, take the frames
// after.
func (a *app) placePreview(c *ui.Context, v *viewer, place float32) {
	m := v.markdown
	if !m.sync.placing || m.sync.want != place {
		m.sync.want, m.sync.placing, m.sync.tries = place, true, 0
	}
	switch {
	case place <= 0:
		m.scroll.Y, m.sync.placing = 0, false
		return
	case place == markdownEnd:
		m.list.ScrollToEnd()
		m.sync.placing = false
		return
	}
	i := 0
	for i+1 < len(m.blocks) && float32(m.blocks[i+1].line) <= place {
		i++
	}
	first, last, next := m.markdownGroup(i)
	top, height, ok := a.markdownExtent(c, v, first, last)
	if !ok {
		m.list.ScrollTo(first, ui.Start)
		a.invalidate()
		return
	}
	share := clamp((place-float32(m.blocks[first].line))/float32(next-m.blocks[first].line), 0, 1)
	by := top + share*height
	if by > -0.5 && by < 0.5 || m.sync.tries >= 4 {
		m.sync.placing = false
		return
	}
	m.scroll.Y = max(0, m.scroll.Y+by)
	m.sync.tries++
	a.invalidate()
}

// syncMarkdown keeps the preview where its text is, before the preview's
// frame is built: with the text shown in another column, or where the text
// was as the preview opened in its place.
func (a *app) syncMarkdown(c *ui.Context, v *viewer) {
	m := v.markdown
	if len(m.sync.rows) != len(m.blocks) {
		m.sync.rows = make([]ui.Handle, len(m.blocks))
	}
	if len(m.blocks) == 0 {
		return
	}
	defer func() {
		if top, ok := a.previewPlace(c, v); ok {
			if o := &v.doc.headings; o.read && o.headingAt(placeLine(top)) != o.headingAt(placeLine(m.sync.top)) {
				// The breadcrumbs, built before the preview, name the
				// heading at the top: one more frame for the one there now.
				a.invalidate()
			}
			m.sync.top = top
		}
	}()
	ed := a.markdownSource(v)
	if ed == nil {
		// Opened in the text's place: going there, as it was asked.
		if m.sync.placing {
			a.placePreview(c, v, m.sync.want)
		}
		m.sync.resync = true
		return
	}
	if time.Since(m.sync.userAt) < markdownLead {
		a.textFromPreview(c, v, ed)
		return
	}
	m.sync.ownY = m.scroll.Y
	if ed.vscroll.Y != m.sync.textY || m.sync.resync {
		m.sync.textY, m.sync.resync = ed.vscroll.Y, false
		a.placePreview(c, v, a.textPlace(ed, ed.vscroll.Y))
	} else if m.sync.placing {
		a.placePreview(c, v, m.sync.want)
	}
}

// textFromPreview scrolls the text to the place at the top of the
// preview, which the user scrolls. It reads the rows where the last frame
// laid them out, which the preview's offset may have left since, and
// takes the next frame for that.
func (a *app) textFromPreview(c *ui.Context, v *viewer, ed *editor) {
	m := v.markdown
	m.sync.placing = false
	// Only as the preview moves, and once more after, as the rows read
	// are the frame's before: a click in it leaves the text where it is.
	if m.scroll.Y == m.sync.ownY && !m.sync.again {
		return
	}
	m.sync.again = m.scroll.Y != m.sync.ownY
	m.sync.ownY = m.scroll.Y
	if m.sync.again {
		a.invalidate()
	}
	place, ok := a.previewPlace(c, v)
	if !ok {
		return
	}
	to := a.textY(ed, place)
	if to == m.sync.textY {
		return
	}
	// Before the next frame: the text may be laid out in this one already,
	// which would put back where it was.
	m.sync.textY = to
	a.post(func() { ed.vscroll.Y = to })
}
