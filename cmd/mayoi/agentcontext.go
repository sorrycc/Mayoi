package main

import (
	"sort"
	"strconv"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/linediff"
	"mayoi/locales"
)

// editorGutterWidth reserves an action lane clear of numbers and Git marks.
func (a *app) editorGutterWidth(lines int) float32 {
	w := a.font.gutterWidth(lines)
	if a.settings.EditorAgentContext {
		w += lineCommentSize + 2
	}
	return w
}

// agentContextView offers the review gutter action on the hovered line or
// selected lines, without taking the pointer while text is being selected.
func (a *app) agentContextView(c *ui.Context, d *editor) {
	if !a.settings.EditorAgentContext || d.selecting || a.review.draft != nil && a.review.ed == d {
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
		a.reviewLineButton(c, 2, y+(a.font.lineH-lineCommentSize)/2, locales.AgentContextComment(), func() {
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
	height   float32
	viewH    float32
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
	d.gutterW = a.editorGutterWidth(d.buf.Snapshot().LineCount())
	left := d.gutterW - 4
	w := min(max(float32(160), view.W-left-14), maxNoteBoxW)
	v.width = w
	h := a.noteHeight(n, true, a.review.text, noteTextW(w))
	gap := h + 2*noteInset
	snap := d.buf.Snapshot()
	wrap := a.wraps(d)
	cols := max(1, int((view.W-d.gutterW-gutterGap-textPadR)/a.font.charW))
	if v.tops == nil || v.version != snap.Version() || v.font != a.font || v.wrap != wrap || wrap && v.cols != cols {
		v.version, v.font, v.wrap, v.cols = snap.Version(), a.font, wrap, cols
		// Keep only visible source strings. Unwrapped line positions need no
		// text measurement, and resizing does not scan the document again.
		v.file.new = diffSide{ok: true, lines: make([]string, snap.LineCount()), hl: d.hl}
		v.tops = make([]float32, snap.LineCount()+1)
		v.contentW = 0
		if !wrap {
			v.contentW = d.gutterW + gutterGap + float32(d.widest)*a.font.charW + textPadR
		}
		for i := 0; i < snap.LineCount(); i++ {
			rows := 1
			if wrap {
				shown, _ := expandTabs(clipLine(snap.Line(i)))
				rows = len(diffBreaks(shown, cols, true)) - 1
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
	if (!v.started || v.height != h || v.viewH != view.H) && view.H > 0 {
		v.height, v.viewH = h, view.H
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
	for i := first; i <= last; i++ {
		v.file.new.lines[i] = snap.Line(i)
		if !wrap {
			shown, _ := expandTabs(clipLine(v.file.new.lines[i]))
			v.contentW = max(v.contentW, d.gutterW+gutterGap+a.font.width(shown)+textPadR)
		}
	}
	d.git.Refresh(d.path, snap, a.post)
	if d.hl != nil {
		d.hl.Request(snap, first, last)
	}
	ui.ScrollBoth(c.Key("editor-review-source")).TrackScroll(&v.scroll).KeepFocus().Fill().Background(a.pal.editor).Children(func() {
		ui.Box(c).Height(top(count) + editorPadY).Width(max(view.W, v.contentW)).Draw(func(pt *ui.Painter, r ui.Rect) {
			for i := first; i <= last; i++ {
				y := r.Y + top(i)
				if i >= n.from && i < n.to {
					pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: v.tops[i+1] - v.tops[i]}, a.pal.selectedDim, 0)
				}
				a.paintDiffLine(pt, &v.file, true, i, -1, r.X+d.gutterW+gutterGap, y, ui.Color{}, cols, wrap)
			}
			// Paint the gutter over horizontally scrolled code, as the editor does.
			x := r.X + v.scroll.X
			pt.Fill(ui.Rect{X: x, Y: r.Y, W: d.gutterW + gutterGap - 2, H: r.H}, a.pal.editor, 0)
			for i := first; i <= last; i++ {
				y, lineH := r.Y+top(i), v.tops[i+1]-v.tops[i]
				span := a.font.span(strconv.Itoa(i+1), a.pal.gutter)
				nw, nh := pt.MeasureText(0, span)
				pt.RichText(x+d.gutterW-14-nw, y+(a.font.lineH-nh)/2, 0, span)
				if i >= n.from && i < n.to {
					pt.Fill(ui.Rect{X: x + 3, Y: y, W: noteStripe, H: lineH}, a.pal.accent, 0)
				}
				if i < len(d.git.Lines) {
					bar := ui.Rect{X: x + d.gutterW - 7, Y: y, W: 3, H: lineH}
					switch d.git.Lines[i] {
					case linediff.Added:
						pt.Fill(bar, a.pal.added, 0)
					case linediff.Modified:
						pt.Fill(bar, a.pal.changed, 0)
					case linediff.Deleted, linediff.DeletedBelow:
						if d.git.Lines[i] == linediff.DeletedBelow {
							bar.Y += lineH
						}
						bar.Y -= 2.5
						bar.W, bar.H = 7, 5
						pt.Fill(bar, a.pal.removed, 2.5)
					}
				}
			}
		}).Children(func() {
			card := a.noteCardBox(c, "review-box", v.scroll.X+left, cardY+noteInset, w, h).Label(locales.ReviewCommentLabel())
			card.Children(func() { a.noteCard(c, n, d, noteTextW(w)) })
		})
	})
	return true
}
