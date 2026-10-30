package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

// agentContextView offers the review gutter action on the hovered line or
// selected lines, without taking the pointer while text is being selected.
func (a *app) agentContextView(c *ui.Context, d *editor) {
	if !a.settings.EditorAgentContext || d.selecting || a.review.draft != nil {
		return
	}
	snap := d.buf.Snapshot()
	g := a.geometry(d, snap.LineCount())
	view := d.hscroll.Bounds(c)
	line := snap.LineAt(min(d.sel[1], snap.Len()))
	if d.sel[1] > d.sel[0] {
		if at, _ := snap.LineRange(line); at == d.sel[1] && line > 0 {
			line--
		}
	}
	if d.reviewHover >= 0 {
		line = d.reviewHover
	} else if d.sel[0] == d.sel[1] || a.activeEditor() != d || a.focus != paneEditor {
		return
	}
	y := editorPadY + g.top(line) - d.vscroll.Y
	if y < 0 || y+a.font.lineH > view.H {
		return
	}
	ui.Box(c.Key("agent-context-gutter")).Absolute().Top(0).Left(0).Children(func() {
		a.reviewLineButton(c, d.gutterW-lineCommentSize, y+(a.font.lineH-lineCommentSize)/2, locales.AgentContextComment(), func() {
			if !a.settings.EditorAgentContext {
				return
			}
			n := editorExcerpt(d)
			if n == nil || line < n.from || line >= n.to || d.sel[0] == d.sel[1] {
				at, _ := d.buf.Snapshot().LineRange(line)
				d.sel = [2]int{at, at}
			}
			for i, col := range a.cols {
				if t := col.tab(); t != nil && t.ed == d {
					a.col, a.focus = i, paneEditor
					break
				}
			}
			a.commentEditor(d)
		})
	})
}

// editorReview displays source around a draft with room for its card.
// It is a presentation of a snapshot, never extra text in the document.
// Saving or cancelling returns to the normal editor and its undo history.
type editorReview struct {
	note     *reviewNote
	version  uint64
	font     codeFont
	cols     int
	wrap     bool
	file     diffFile
	tops     []float32
	scroll   ui.ScrollState
	started  bool
	width    float32
	contentW float32
}

func (a *app) editorReviewView(c *ui.Context, d *editor) bool {
	n := a.review.draft
	if !a.settings.EditorAgentContext || n == nil || a.review.ed != d {
		if d.reviewSource != nil {
			d.reviewSource = nil
			d.resync, d.focusNext = true, true
		}
		return false
	}
	a.followNote(n)
	if d.reviewSource == nil || d.reviewSource.note != n {
		d.reviewSource = &editorReview{note: n, scroll: ui.ScrollState{X: d.hscroll.X, Y: d.vscroll.Y}}
	}
	v := d.reviewSource
	view := v.scroll.Bounds(c)
	if view.W <= 0 {
		view = d.hscroll.Bounds(c)
	}
	d.gutterW = a.font.gutterWidth(d.buf.Snapshot().LineCount())
	left := d.gutterW - 4
	w := max(float32(160), view.W-left-14)
	v.width = w
	h := a.noteHeight(n, true, a.review.text, noteTextW(w))
	gap := h + 2*noteInset
	snap := d.buf.Snapshot()
	wrap := a.wraps(d)
	cols := max(1, int((view.W-d.gutterW-gutterGap-textPadR)/a.font.charW))
	if v.tops == nil || v.version != snap.Version() || v.font != a.font || v.wrap != wrap || v.cols != cols {
		v.version, v.font, v.wrap, v.cols = snap.Version(), a.font, wrap, cols
		v.file.new = diffSide{ok: true, lines: strings.Split(snap.String(), "\n"), hl: d.hl}
		v.tops = make([]float32, len(v.file.new.lines)+1)
		v.contentW = view.W
		for i, line := range v.file.new.lines {
			shown, _ := expandTabs(clipLine(line))
			rows := len(diffBreaks(shown, cols, wrap)) - 1
			if !wrap {
				v.contentW = max(v.contentW, d.gutterW+gutterGap+a.font.width(shown)+textPadR)
			}
			v.tops[i+1] = v.tops[i] + float32(rows)*a.font.lineH
		}
	}
	count := len(v.file.new.lines)
	if wrap {
		v.scroll.X = 0
	}
	after := min(max(n.to, 1), count)
	cardY := editorPadY + v.tops[after]
	if !v.started && view.H > 0 {
		v.started = true
		if cardY+gap > v.scroll.Y+view.H || cardY < v.scroll.Y {
			v.scroll.Y = max(0, cardY-min(3*a.font.lineH, max(0, view.H-gap)))
		}
	}
	top := func(i int) float32 {
		y := editorPadY + v.tops[i]
		if i >= after {
			y += gap
		}
		return y
	}
	first := max(0, sort.Search(count, func(i int) bool { return top(i)+v.tops[i+1]-v.tops[i] > v.scroll.Y })-1)
	last := min(count-1, sort.Search(count, func(i int) bool { return top(i) > v.scroll.Y+view.H })+1)
	if d.hl != nil {
		d.hl.Request(snap, first, last)
	}
	ui.ScrollBoth(c.Key("editor-review-source")).TrackScroll(&v.scroll).Fill().Background(a.pal.editor).Children(func() {
		ui.Box(c).Height(top(count) + editorPadY).Width(max(view.W, v.contentW)).Draw(func(pt *ui.Painter, r ui.Rect) {
			for i := first; i <= last; i++ {
				y := r.Y + top(i)
				if i >= n.from && i < n.to {
					pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: v.tops[i+1] - v.tops[i]}, a.pal.selectedDim, 0)
					pt.Fill(ui.Rect{X: r.X + 3, Y: y, W: noteStripe, H: v.tops[i+1] - v.tops[i]}, a.pal.accent, 0)
				}
				span := a.font.span(strconv.Itoa(i+1), a.pal.faint)
				nw, nh := pt.MeasureText(0, span)
				pt.RichText(r.X+d.gutterW-nw-4, y+(a.font.lineH-nh)/2, 0, span)
				a.paintDiffLine(pt, &v.file, true, i, -1, r.X+d.gutterW+gutterGap, y, ui.Color{}, cols, wrap)
			}
		}).Children(func() {
			card := a.noteCardBox(c, "review-box", v.scroll.X+left, cardY+noteInset, w, h).Label(locales.ReviewCommentLabel())
			card.Children(func() { a.noteCard(c, n, d, noteTextW(w)) })
		})
	})
	return true
}
