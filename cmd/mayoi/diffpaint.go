package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/linediff"
	"mayoi/locales"
)

// diffGeometry is where a diff's columns are, for its width.
type diffGeometry struct {
	numW  float32 // the width of a column of line numbers
	textX float32 // where the text starts, from the side's left
	half  float32 // the width of the left side, side by side
	viewW float32
	// cols are how many columns of text each side has room for, as the
	// lines wrap: of the one column only, in the unified layout.
	cols [2]int
}

// diffRailW is the width of the bar along the left of a diff's rows,
// colored as their lines changed.
const diffRailW = 3

// diffGutter is the room of the line numbers of n lines in a diff.
func (a *app) diffGutter(n int) float32 {
	return float32(max(len(strconv.Itoa(n)), 3))*a.font.charW + 16
}

func (a *app) diffGeometry(v *diffView, split bool, viewW float32) diffGeometry {
	n := 0
	for _, f := range v.files {
		n = max(n, len(f.old.lines), len(f.new.lines))
	}
	numW := a.diffGutter(n)
	l := diffGeometry{numW: numW, viewW: viewW}
	room := func(w float32) int { return max(int((w-l.textX-14)/a.font.charW), 8) }
	if split {
		l.half = float32(int(viewW / 2))
		l.textX = diffRailW + numW + 22
		l.cols = [2]int{room(l.half), room(viewW - l.half)}
	} else {
		l.textX = diffRailW + 2*numW + 22
		l.cols = [2]int{room(viewW), 0}
	}
	return l
}

// diffViewUI shows a diff: its rows painted in a pane that scrolls both
// ways, keeping the line numbers in place, or down only as its lines wrap.
func (a *app) diffViewUI(c *ui.Context, v *diffView) {
	p, f := a.pal, a.font
	a.refreshDiff(v)
	message := func(msg string) {
		ui.Column(c).Fill().Background(p.editor).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(p.muted)
		})
	}
	// clean says the work tree has nothing to show, as source control does.
	clean := func() {
		ui.Column(c).Fill().Background(p.editor).Center().Gap(6).Children(func() {
			ui.Box(c).Size(52, 52).Radius(26).Background(p.added.Alpha(0.13)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconCheck).FontSize(24).TextColor(p.added)
			})
			ui.Text(c, locales.DiffNoChanges()).FontSize(14).FontWeight(600).TextColor(p.text).Margin(8, 0, 0, 0)
			ui.Text(c, locales.DiffTreeMatches()).FontSize(12.5).TextColor(p.muted)
		})
	}
	if cm := v.commit; cm != nil && len(v.files) == 0 {
		switch {
		case !cm.loaded:
			ui.Box(c).Fill().Background(p.editor)
		case cm.err != "":
			message(cm.err)
		case cm.from != "":
			message(locales.NoDifferencesBetween(git.ShortHash(cm.from), git.ShortHash(cm.hash)))
		default:
			message(locales.DiffCommitNoFiles())
		}
		return
	}
	if len(v.files) == 0 {
		switch {
		case a.diffContext(v).git.looked && a.diffContext(v).git.repo == nil:
			message(locales.DiffNotRepo())
		case a.diffContext(v).git.status != nil:
			clean()
		default:
			ui.Box(c).Fill().Background(p.editor)
		}
		return
	}
	lineH, charW := f.lineH, f.charW
	view := v.pane.Bounds(c)
	if view.W > 0 {
		v.viewW, v.viewH = view.W, view.H
	}
	v.split = a.splits(v, v.viewW)
	l := a.diffGeometry(v, v.split, v.viewW)
	var card *commitCard
	if v.commit != nil {
		card = a.layOutCommitCard(c, v, cardWidth(v.viewW))
		v.commit.lines = card.lines
	}
	a.layoutRows(v, l)
	rows := v.rows
	wrap := v.built.wrap
	widest := 0
	for _, file := range v.files {
		widest = max(widest, file.widest)
	}
	textW := float32(widest)*charW + textPadR
	contentW := max(view.W, l.textX+textW)
	if v.split {
		contentW = view.W + max(0, textW-(l.half-l.textX))
	}
	if wrap {
		contentW, v.scroll.X = view.W, 0
	}
	contentH := 2*editorPadY + float32(v.tops[len(rows)])*lineH
	if j := v.jump; j >= 0 && view.H > 0 {
		v.jump = -1
		if j < len(rows) {
			y := editorPadY + float32(v.tops[j])*lineH
			h := float32(rows[j].h) * lineH
			top := v.scroll.Y
			switch v.jumpTo {
			case jumpTop:
				top = y
			case jumpReveal:
				if y < top {
					top = y
				} else if y+h > top+view.H {
					top = y + h - view.H
				}
			default:
				if y < top || y+h > top+view.H {
					top = y - view.H/3
				}
			}
			v.scroll.Y = clamp(top, 0, max(0, contentH-view.H))
		}
	}
	first, last := 0, -1
	if len(rows) > 0 {
		first, last = v.rowAt(v.scroll.Y, lineH), v.rowAt(v.scroll.Y+view.H, lineH)
	}
	// The files read are those within a view's height of it, and the
	// source control view chooses the file chosen, or at its top.
	if len(rows) > 0 {
		pad := int(view.H/lineH) + 1
		v.near = [2]int{rows[max(0, first-pad)].file, rows[min(len(rows)-1, last+pad)].file + 1}
		at := v.focusRowAt(v.topRow(lineH))
		if v.cursor >= first && v.cursor <= last {
			at = v.cursor
		}
		if at >= 0 {
			if fl := v.files[rows[at].file]; fl != v.synced && v.commit == nil {
				v.synced = fl
				a.post(func() { a.chooseScmFile(fl) })
			}
		}
	}
	// The lines of each side of each file in view, to highlight.
	for k := first; k <= last; {
		file := rows[k].file
		lo, hi := [2]int{-1, -1}, [2]int{-1, -1}
		for ; k <= last && rows[k].file == file; k++ {
			if rows[k].kind != rowLine {
				continue
			}
			for side, n := range [2]int{rows[k].old, rows[k].new} {
				if n >= 0 {
					if lo[side] < 0 {
						lo[side] = n
					}
					hi[side] = n
				}
			}
		}
		fl := v.files[file]
		for side := range 2 {
			if s := fl.side(side == 1); lo[side] >= 0 && s.src != nil {
				a.highlighter(fl, s).Request(s.src, lo[side], hi[side])
			}
		}
	}
	sticky, stickyY := v.stickyHeader(lineH)
	focused := v.pane.FocusWithin(c)
	pane := ui.ScrollBoth(c.Key("diff")).TrackScroll(&v.scroll).Bind(&v.pane).Fill().Background(p.editor).
		Focusable().FocusRing(false).Label(locales.DiffPaneLabel())
	a.takeKeys(pane, placeDiff, func(ev ui.InputEvent) bool { return a.diffInput(v, ev) }).Children(func() {
		box := ui.Box(c).Width(contentW).Height(contentH)
		// The buttons of the file's header under the pointer, or chosen.
		onSticky := func(y float32) bool { return sticky >= 0 && y >= stickyY && y < stickyY+lineH }
		hovered := -1
		if box.Hovered() {
			if _, y, ok := box.PointerPosition(); ok {
				hovered = v.rowAt(y, lineH)
				if onSticky(y) {
					hovered = sticky
				}
			}
		}
		var buttons, hunkButtons, cardRect, commentButton ui.Rect
		box.Children(func() {
			if card != nil && first == 0 && rows[0].kind == rowCommit {
				cardRect = ui.Rect{X: v.scroll.X, Y: editorPadY + float32(v.tops[0])*lineH, W: view.W, H: float32(rows[0].h) * lineH}
				a.commitCardUI(c, v, card, cardRect.X, cardRect.Y, cardRect.W, cardRect.H)
			}
			for _, i := range []int{hovered, v.cursor} {
				if i != sticky && (i < first || i > last) || i < 0 || rows[i].kind != rowFile || buttons.W > 0 {
					continue
				}
				y := editorPadY + float32(v.tops[i])*lineH
				if i == sticky {
					y = stickyY
				}
				buttons = a.fileButtons(c, v, v.files[rows[i].file], v.scroll.X+view.W, y)
			}
			// The cards of the notes in view.
			for i := first; i <= last; i++ {
				if rows[i].kind != rowComment {
					continue
				}
				if n := a.review.noteAt(rows[i].n); n != nil {
					y := editorPadY + float32(v.tops[i])*lineH
					noteX, noteW := noteGeometry(l, v.split, n.oldSide)
					card := a.noteCardBox(c, fmt.Sprintf("note:%p", n), v.scroll.X+noteX, y+noteInset, noteW, float32(rows[i].h)*lineH-2*noteInset)
					card.Children(func() { a.noteCard(c, n, nil, noteTextW(noteW)) })
				}
			}
			// A button by the line under the pointer comments on it, unless
			// the pointer chooses rows.
			if hovered >= first && hovered <= last && hovered != sticky && rows[hovered].kind == rowLine && !v.dragged {
				row := rows[hovered]
				x := diffRailW + 2*l.numW + 3
				if v.split {
					x = diffRailW + l.numW + 3
					if row.new >= 0 {
						x += l.half
					}
				}
				y := editorPadY + float32(v.tops[hovered])*lineH + (lineH-lineCommentSize)/2
				commentButton = a.lineCommentButton(c, v, hovered, v.scroll.X+x, y)
			}
			// The buttons of the run of changes under the pointer, or chosen.
			for _, i := range []int{hovered, v.cursor} {
				h := v.hunkHead(i)
				if h < first || h > last || hunkButtons.W > 0 {
					continue
				}
				// Not under the header that sticks at the top.
				if y := editorPadY + float32(v.tops[h])*lineH; sticky >= 0 && y < stickyY+lineH {
					continue
				}
				hunkButtons = a.hunkButtons(c, v, h, v.scroll.X+view.W, contentW, editorPadY+float32(v.tops[h])*lineH)
			}
		})
		// Dragging from a row chooses the rows to the one under the pointer.
		if box.Pressed() {
			if _, y, ok := box.PointerPosition(); ok && len(rows) > 0 && !onSticky(y) {
				i := v.rowAt(y, lineH)
				switch {
				case v.press < 0:
					v.press, v.dragged = i, false
				case i != v.press || v.dragged:
					if v.press < len(rows) && rows[v.press].kind == rowLine && i != v.cursor {
						v.dragged = true
						v.anchor = v.press
						v.setCursor(i)
						v.jumpRow(i, jumpReveal)
					}
				}
			}
		} else {
			v.press = -1
		}
		// A click chooses a row, and opens a gap or folds a file; a double
		// click opens the file at a line, unless its first click did that.
		// Shift chooses the rows from the one chosen to it.
		if clicked, double := box.Clicked(), box.DoubleClicked(); clicked || double {
			if x, y, ok := box.PointerPosition(); ok && len(rows) > 0 && !buttons.Contains(x, y) && !hunkButtons.Contains(x, y) && !cardRect.Contains(x, y) &&
				!commentButton.Contains(x, y) {
				i := v.rowAt(y, lineH)
				stuck := onSticky(y)
				if stuck {
					i = sticky
				}
				shift := box.ClickModifiers()&ui.Shift != 0
				dragged := v.dragged
				v.dragged = false
				a.post(func() {
					if i >= len(v.rows) || !stuck && v.rows[i].kind == rowComment {
						return
					}
					v.pane.Focus()
					if dragged {
						return
					}
					if shift && !stuck && !double {
						at := v.anchor
						if at < 0 {
							at = max(v.cursor, 0)
						}
						v.setCursor(i)
						v.anchor = at
						return
					}
					v.anchor = -1
					if stuck {
						// The file folds under its header, which shows at the top.
						v.setCursor(i)
						v.acted = true
						a.activateRow(v, i, false)
						v.jumpRow(i, jumpTop)
						return
					}
					if double {
						if !v.acted {
							a.activateRow(v, i, true)
						}
						return
					}
					v.setCursor(i)
					v.acted = v.rows[i].kind != rowLine && v.rows[i].kind != rowHunk
					a.activateRow(v, i, false)
				})
			}
		}
		box.Draw(func(pt *ui.Painter, r ui.Rect) {
			vx, vy := r.X+v.scroll.X, r.Y+v.scroll.Y // the view's corner
			_, numH := pt.MeasureText(0, f.span("0", ui.Color{}))
			mid := (lineH - numH) / 2
			txt := func(x, y float32, s string, col ui.Color) float32 {
				span := f.span(s, col)
				w, _ := pt.MeasureText(0, span)
				pt.RichText(x, y+mid, 0, span)
				return w
			}
			number := func(n int, right, y float32, col ui.Color) {
				label := strconv.Itoa(n + 1)
				txt(right-float32(len(label))*charW, y, label, col)
			}
			rail := func(x, y, h float32, col ui.Color) {
				pt.Fill(ui.Rect{X: x, Y: y, W: diffRailW, H: h}, col, 0)
			}
			dim := func(col ui.Color) ui.Color { return col.Mix(p.editor, 0.55) }
			neutral := p.gutter.Alpha(0.45)
			// A bar by the rail along the lines of each note.
			notes := a.diffNotes(v)
			noted := func(x, y, h float32, row diffRow, old bool) {
				if notedLine(v, notes, row, old) {
					pt.Fill(ui.Rect{X: x + diffRailW + 1, Y: y, W: 2, H: h}, p.accent, 0)
				}
			}
			for i := first; i <= last; i++ {
				row := &rows[i]
				file := v.files[row.file]
				y := r.Y + editorPadY + float32(v.tops[i])*lineH
				h := float32(row.h) * lineH
				full := ui.Rect{X: vx, Y: y, W: view.W, H: h}
				switch row.kind {
				case rowSpace:
					continue
				case rowComment:
					rail(vx, y, h, neutral)
					if i == v.cursor {
						pt.Fill(ui.Rect{X: vx, Y: y, W: diffRailW, H: h}, p.accent, 0)
					}
					if v.split {
						// The sides go on past the card, under one of them.
						rail(vx+l.half, y, h, neutral)
						pt.Fill(ui.Rect{X: vx + l.half - 0.5, Y: y, W: 1, H: h}, p.border, 0)
					}
				case rowCommit:
					a.paintCommitCard(pt, full)
				case rowFile:
					a.paintFileHeader(pt, file, full, i == hovered, txt)
				case rowSection:
					a.paintSection(pt, file.base, v.sections[btoi(file.base == diffStaged)], full)
				case rowNote:
					// Said of the file, not a line of it: in the window's font.
					uiText(pt, vx+l.textX, y, lineH, row.label, 12.5, 400, p.faint)
				case rowHunk:
					pt.Fill(full, p.text.Alpha(0.035), 0)
					rail(vx, y, h, neutral)
					at := vx + diffRailW + 10
					head, scope := row.label, ""
					if k := strings.Index(row.label[min(3, len(row.label)):], " @@"); k >= 0 {
						head, scope = row.label[:k+6], row.label[k+6:]
					}
					at += txt(at, y, head, p.faint)
					txt(at, y, scope, p.muted)
				case rowGap:
					bg := p.text.Alpha(0.025)
					if i == hovered {
						bg = p.accent.Alpha(0.08)
					}
					pt.Fill(full, bg, 0)
					rail(vx, y, h, neutral)
					col := p.muted
					if i == hovered {
						col = p.text
					}
					at := vx + diffRailW + 10
					pt.Icon(iconUnfold, ui.Rect{X: at, Y: y + h/2 - 6, W: 12, H: 12}, col)
					uiText(pt, at+18, y, h, locales.DiffShowUnchanged(row.n), headerFontSize, 400, col)
				case rowLine:
					if v.split {
						for side := range 2 {
							x0 := vx + float32(side)*l.half
							w := l.half
							if side == 1 {
								w = view.W - l.half
							}
							n, tint := row.old, p.removed
							if side == 1 {
								n, tint = row.new, p.added
							}
							pt.Clip(ui.Rect{X: x0, Y: vy, W: w, H: view.H}, 0, func() {
								if n < 0 {
									a.paintFiller(pt, ui.Rect{X: x0, Y: y, W: w, H: h})
									return
								}
								changed := row.hunk >= 0
								if changed {
									pt.Fill(ui.Rect{X: x0, Y: y, W: w, H: h}, tint.Alpha(0.16), 0)
								}
								peer := -1
								if changed && row.old >= 0 && row.new >= 0 {
									peer = row.new
									if side == 1 {
										peer = row.old
									}
								}
								pt.Clip(ui.Rect{X: x0 + l.textX - 4, Y: vy, W: w - l.textX + 4, H: view.H}, 0, func() {
									a.paintDiffLine(pt, file, side == 1, n, peer, r.X+float32(side)*l.half+l.textX, y, tint, l.cols[side], wrap)
								})
								numCol := p.gutter
								switch {
								case changed && v.inCurrent(row):
									rail(x0, y, h, tint)
									numCol = tint
								case changed:
									rail(x0, y, h, dim(tint))
									numCol = tint
								default:
									rail(x0, y, h, neutral)
								}
								number(n, x0+diffRailW+l.numW, y, numCol)
								noted(x0, y, h, *row, side == 0)
								if changed {
									mark := "-"
									if side == 1 {
										mark = "+"
									}
									txt(x0+diffRailW+l.numW+8, y, mark, tint)
								}
							})
						}
						pt.Fill(ui.Rect{X: vx + l.half - 0.5, Y: y, W: 1, H: h}, p.border, 0)
						break
					}
					changed := row.hunk >= 0
					tint := p.added
					if row.new < 0 {
						tint = p.removed
					}
					if changed {
						pt.Fill(ui.Rect{X: r.X, Y: y, W: max(r.W, view.W), H: h}, tint.Alpha(0.16), 0)
					}
					n, newSide := row.line()
					pt.Clip(ui.Rect{X: vx + l.textX - 4, Y: vy, W: view.W - l.textX + 4, H: view.H}, 0, func() {
						a.paintDiffLine(pt, file, newSide, n, row.peer, r.X+l.textX, y, tint, l.cols[0], wrap)
					})
					// The gutter, which stays as the text scrolls sideways.
					gut := ui.Rect{X: vx, Y: y, W: l.textX - 4, H: h}
					pt.Fill(gut, p.editor, 0)
					numCol := p.gutter
					switch {
					case changed:
						pt.Fill(gut, tint.Alpha(0.16), 0)
						numCol = tint
						if v.inCurrent(row) {
							rail(vx, y, h, tint)
						} else {
							rail(vx, y, h, dim(tint))
						}
					default:
						rail(vx, y, h, neutral)
					}
					noted(vx, y, h, *row, row.new < 0)
					if row.old >= 0 {
						number(row.old, vx+diffRailW+l.numW, y, numCol)
					}
					if row.new >= 0 {
						number(row.new, vx+diffRailW+2*l.numW, y, numCol)
					}
					if changed {
						mark := "+"
						if row.new < 0 {
							mark = "-"
						}
						txt(vx+diffRailW+2*l.numW+8, y, mark, tint)
					}
				}
				if v.selected(i) && row.kind != rowComment {
					pt.Fill(full, p.accent.Alpha(0.12), 0)
					pt.Fill(ui.Rect{X: vx, Y: y, W: diffRailW, H: h}, p.accent.Alpha(0.6), 0)
				}
				if i == v.cursor && row.kind != rowComment {
					col := p.text.Alpha(0.05)
					if focused {
						col = p.text.Alpha(0.09)
					}
					pt.Fill(full, col, 0)
					pt.Fill(ui.Rect{X: vx, Y: y, W: diffRailW, H: h}, p.accent, 0)
				}
			}
			// The header of the file at the top, over its lines.
			if sticky >= 0 {
				head := ui.Rect{X: vx, Y: r.Y + stickyY, W: view.W, H: lineH}
				pt.Shadow(head, 0, 0, 2, 6, 0, p.shadow)
				a.paintFileHeader(pt, v.files[rows[sticky].file], head, hovered == sticky, txt)
				if v.cursor == sticky {
					col := p.text.Alpha(0.05)
					if focused {
						col = p.text.Alpha(0.09)
					}
					pt.Fill(head, col, 0)
					pt.Fill(ui.Rect{X: vx, Y: head.Y, W: diffRailW, H: lineH}, p.accent, 0)
				}
			}
			// Along the right edge, the hunks of every file.
			if total := v.tops[len(rows)]; total > 1 {
				right := vx + view.W - 10
				for _, at := range v.starts {
					s := rows[at]
					end := at
					for end < len(rows) && rows[end].changed() && rows[end].hunk == s.hunk && rows[end].file == s.file {
						end++
					}
					hk := v.files[s.file].hunks[s.hunk]
					y0 := vy + float32(v.tops[at])/float32(total)*view.H
					hh := max(float32(v.tops[end]-v.tops[at])/float32(total)*view.H, 2)
					col := p.changed
					switch {
					case hk.ALen == 0:
						col = p.added
					case hk.BLen == 0:
						col = p.removed
					}
					pt.Fill(ui.Rect{X: right, Y: y0, W: 3, H: hh}, col, 0)
				}
				// And the cards of the notes.
				for _, at := range v.cards {
					y0 := vy + float32(v.tops[at])/float32(total)*view.H
					pt.Fill(ui.Rect{X: right + 4, Y: y0 - 1, W: 5, H: 3}, p.accent, 1)
				}
			}
		})
	})
}

// statusWord names a letter of Git's, as a file's header says it.
func statusWord(letter byte) string {
	switch letter {
	case 'M':
		return locales.StatusModified()
	case 'A':
		return locales.StatusAdded()
	case 'D':
		return locales.StatusDeleted()
	case 'R':
		return locales.StatusRenamed()
	case 'C':
		return locales.StatusConflict()
	case 'T':
		return locales.StatusTypeChanged()
	case 'U':
		return locales.StatusUntracked()
	}
	return ""
}

// diffStatBlocks is how many blocks the bar of a file's header has, which
// shows how its changes add and remove lines, as GitHub's does.
const diffStatBlocks = 5

// headerFontSize is the size of the text of the headers of a diff.
const headerFontSize = 12

// uiText paints s in the window's font at x, centered in a row y and h
// tall, and returns its width.
func uiText(pt *ui.Painter, x, y, h float32, s string, size float32, weight int, col ui.Color) float32 {
	span := ui.Span{Text: s, Size: size, Weight: weight, Color: col}
	w, th := pt.MeasureText(0, span)
	pt.RichText(x, y+(h-th)/2, 0, span)
	return w
}

// fileHeaderRight is how far from the right of a file's header its
// counts start, as it paints them: the lines added and removed and the
// bar of blocks after them.
func (a *app) fileHeaderRight(f *diffFile) float32 {
	if !f.loaded || f.binary {
		return 18
	}
	add, del := f.counts()
	return 18 + float32(len([]rune(fmt.Sprintf("+%d −%d", add, del))))*a.font.charW + 10 + diffStatBlocks*8
}

// paintFileHeader paints the header of a file in the diff of every file:
// whether its lines show, its icon, path and state, and the lines its
// changes add and remove, in numbers and in a bar.
func (a *app) paintFileHeader(pt *ui.Painter, f *diffFile, r ui.Rect, hovered bool, txt func(x, y float32, s string, col ui.Color) float32) {
	p := a.pal
	bg := p.chrome
	if hovered {
		bg = p.text.Alpha(0.035).Over(p.chrome)
	}
	pt.Fill(r, bg, 0)
	pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, p.border, 0)
	pt.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.border, 0)
	mid := r.Y + r.H/2
	x := r.X + diffRailW + 8
	chevron := iconChevronDown
	if f.folded {
		chevron = iconChevronRight
	}
	pt.Icon(chevron, ui.Rect{X: x, Y: mid - 6, W: 12, H: 12}, p.muted)
	x += 18
	name := filepath.Base(f.path)
	icon, iconCol := entryIcon(p, f.entry, name)
	pt.Icon(icon, ui.Rect{X: x, Y: mid - 7, W: 14, H: 14}, iconCol)
	x += 20
	rel := a.relOrPath(f.path)
	dir := ""
	if k := strings.LastIndexAny(rel, `/\`); k >= 0 {
		dir, rel = rel[:k+1], rel[k+1:]
	}
	if (f.base == diffStaged || f.base == diffCommit) && f.entry.Orig != "" {
		x += uiText(pt, x, r.Y, r.H, a.relOrPath(f.entry.Orig), headerFontSize, 400, p.muted) + 6
		x += uiText(pt, x, r.Y, r.H, "→", headerFontSize, 400, p.faint) + 6
	}
	x += uiText(pt, x, r.Y, r.H, dir, headerFontSize, 400, p.muted)
	nameCol := p.text
	if f.letter == 'D' {
		nameCol = p.muted
	}
	x += uiText(pt, x, r.Y, r.H, rel, headerFontSize, 600, nameCol)
	if word := statusWord(f.letter); word != "" {
		col := gitColor(p, f.letter)
		span := ui.Span{Text: word, Size: 10.5, Weight: 600, Color: col}
		w, th := pt.MeasureText(0, span)
		pill := ui.Rect{X: x + 8, Y: mid - 8, W: w + 12, H: 16}
		pt.Fill(pill, col.Alpha(0.14), 8)
		pt.RichText(pill.X+6, mid-th/2, 0, span)
	}
	if !f.loaded || f.binary {
		return
	}
	add, del := f.counts()
	plus, minus := fmt.Sprintf("+%d", add), fmt.Sprintf(" −%d", del)
	at := r.X + r.W - a.fileHeaderRight(f)
	addCol, delCol := p.added, p.removed
	if add == 0 {
		addCol = p.faint
	}
	if del == 0 {
		delCol = p.faint
	}
	at += txt(at, r.Y, plus, addCol)
	at += txt(at, r.Y, minus, delCol)
	// The bar: blocks green for the lines added, red for those removed,
	// in proportion, and gray for none.
	at += 10
	green, red := 0, 0
	if total := add + del; total > 0 {
		green = int(float32(add)/float32(total)*diffStatBlocks + 0.5)
		red = diffStatBlocks - green
		if add > 0 && green == 0 {
			green, red = 1, red-1
		}
		if del > 0 && red == 0 {
			green, red = green-1, 1
		}
	}
	for i := range diffStatBlocks {
		col := p.text.Alpha(0.12)
		switch {
		case i < green:
			col = p.added
		case i < green+red:
			col = p.removed
		}
		pt.Fill(ui.Rect{X: at + float32(i)*8, Y: mid - 3.5, W: 7, H: 7}, col, 1.5)
	}
}

// maxCardSubject is how many lines a commit's subject wraps to in the
// card at the top of its diff before it is cut, and maxCardBody how many
// the rest of its message shows until asked for all of it.
const (
	maxCardSubject = 3
	maxCardBody    = 12
)

// The room around and between the parts of a commit's card.
const (
	cardPadX    = 12
	cardPadY    = 8
	cardGap     = 4  // under the subject
	cardBodyGap = 10 // over the body
	cardMetaH   = 20 // a line of who made it, when, its hash and refs
	cardMoreH   = 20 // the button that shows the rest of the body
)

// cardPiece is a piece of the line of a commit's card under its subject,
// which wraps between pieces: how wide it is, whether a dot sets it off
// from the piece before it on its line, and what shows it.
type cardPiece struct {
	w    float32
	dot  bool
	show func()
}

// commitCard is the card of a commit laid out at a width: its subject
// wrapped, the pieces of each line under it, and the rest of its message.
type commitCard struct {
	w        float32 // the width of its text
	subject  ui.Span
	subjectH float32
	cut      bool // the subject is longer than it shows
	meta     [][]cardPiece
	body     ui.Span
	bodyH    float32
	folds    bool // the body is longer than it shows folded
	lines    int  // how many lines of the diff tall it is
}

// cardWidth is the width of the text of a commit's card in a view w wide.
func cardWidth(w float32) float32 {
	return max(w-diffRailW-2*cardPadX, 120)
}

// layOutCommitCard lays out the card at the top of a commit's diff, its
// text w wide.
func (a *app) layOutCommitCard(c *ui.Context, v *diffView, w float32) *commitCard {
	p := a.pal
	cm := v.commit
	info := cm.info
	k := &commitCard{w: w}
	lineOf := func(s ui.Span) float32 {
		s.Text = "M"
		_, h := c.MeasureText(0, s)
		return h
	}
	subject := info.Subject
	if cm.from != "" {
		subject = locales.ChangesFromTo(a.diffContext(v).revisionLabel(cm.from), a.diffContext(v).revisionLabel(cm.hash))
	}
	k.subject = ui.Span{Text: subject, Size: 15, Weight: 600, Color: p.text}
	_, k.subjectH = c.MeasureText(w, k.subject)
	if one := lineOf(k.subject); k.subjectH > maxCardSubject*one+0.5 {
		k.subjectH, k.cut = maxCardSubject*one, true
	}
	// Who, when, which: in pieces that wrap to the next line.
	measure := func(s ui.Span) float32 {
		w, _ := c.MeasureText(0, s)
		return w
	}
	label := func(s string, weight int, col ui.Color) ui.Span {
		return ui.Span{Text: s, Size: headerFontSize, Weight: weight, Color: col}
	}
	text := func(s ui.Span, dot bool) cardPiece {
		return cardPiece{w: measure(s), dot: dot, show: func() {
			ui.RichText(c, s).SingleLine().Shrink(1).MinWidth(0)
		}}
	}
	hashChip := func(h string, dot bool) cardPiece {
		hash := ui.Span{Text: git.ShortHash(h), Font: a.font.family, Size: a.font.size - 1, Color: p.accent}
		return cardPiece{w: measure(hash) + 12, dot: dot, show: func() {
			b := ui.ButtonBase(c.Key("card-hash:"+h)).Height(18).Padding(0, 6).Radius(5).AlignItems(ui.Center).
				Background(p.accent.Alpha(0.1)).Label(locales.CopyCommitHash()).Tooltip(locales.CopyCommitHash())
			if b.Hovered() {
				b.Background(p.accent.Alpha(0.2))
			}
			if b.Clicked() {
				a.post(func() { a.copyCommitHash(h) })
			}
			b.Children(func() { ui.RichText(c, hash).SingleLine() })
		}}
	}
	var pieces []cardPiece
	if cm.from != "" {
		// A comparison: its two commits, and what changed between them.
		pieces = append(pieces, hashChip(cm.from, false), text(label("→", 400, p.muted), false), hashChip(cm.hash, false),
			text(label(locales.CardFilesChanged(len(v.files)), 400, p.muted), true))
		return a.finishCommitCard(c, v, k, w, pieces)
	}
	author := label(info.Author, 600, p.text)
	col := avatarColor(p, info.Email+info.Author)
	pieces = append(pieces, cardPiece{w: 18 + 6 + measure(author), show: func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
			ui.Box(c).Size(18, 18).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
				pt.Fill(b, col.Alpha(0.22), 9)
				paintInitials(pt, b, info.Author, 8.5, col)
			})
			ui.RichText(c, author).SingleLine().Shrink(1).MinWidth(0)
		})
	}})
	if !info.Time.IsZero() {
		when := locales.CardCommitted(ago(info.Time, time.Now())) + " · " + dateTime(info.Time)
		pieces = append(pieces, text(label(when, 400, p.muted), false))
	}
	pieces = append(pieces, hashChip(cm.hash, true))
	for i, b := range refBadges(info.Refs) {
		fg, bg := p.accent, p.accent.Alpha(0.14)
		switch b.Kind {
		case git.RefRemote:
			fg, bg = mix(p.accent, p.muted, 0.4), p.accent.Alpha(0.07)
		case git.RefTag:
			fg, bg = p.text, p.hover
		}
		weight := 500
		if b.Head || b.Kind == git.RefHead {
			weight = 700
		}
		s := label(b.label(), weight, fg)
		s.Size = headerFontSize - 0.5
		if b.Kind == git.RefTag {
			s.Text = "◆ " + s.Text
		}
		pieces = append(pieces, cardPiece{w: measure(s) + 12, dot: i == 0, show: func() {
			ui.Row(c).Height(18).Padding(0, 6).Radius(5).AlignItems(ui.Center).Background(bg).Shrink(1).MinWidth(0).Children(func() {
				ui.RichText(c, s).SingleLine().Shrink(1).MinWidth(0)
			})
		}})
	}
	if len(info.Parents) > 1 {
		pieces = append(pieces, text(label(locales.CardMergeOf(len(info.Parents)), 400, p.muted), true))
	}
	pieces = append(pieces, text(label(locales.CardFilesChanged(len(v.files)), 400, p.muted), true))
	return a.finishCommitCard(c, v, k, w, pieces)
}

// finishCommitCard wraps the pieces of a card's lines to its width w,
// lays out the rest of its message, and works out how tall it is.
func (a *app) finishCommitCard(c *ui.Context, v *diffView, k *commitCard, w float32, pieces []cardPiece) *commitCard {
	p := a.pal
	cm := v.commit
	info := cm.info
	lineOf := func(s ui.Span) float32 {
		s.Text = "M"
		_, h := c.MeasureText(0, s)
		return h
	}
	dot, _ := c.MeasureText(0, ui.Span{Text: "·", Size: headerFontSize, Color: p.faint})
	dotW := dot
	var line []cardPiece
	lineW := float32(0)
	for _, pc := range pieces {
		add := pc.w
		if len(line) > 0 {
			add += 6
			if pc.dot {
				add += dotW + 6
			}
		}
		if len(line) > 0 && lineW+add > w {
			k.meta = append(k.meta, line)
			line, lineW, add = nil, 0, pc.w
		}
		line, lineW = append(line, pc), lineW+add
	}
	k.meta = append(k.meta, line)
	h := 2*cardPadY + k.subjectH + cardGap + float32(len(k.meta))*cardMetaH
	// The rest of the message, folded past its first lines.
	if body := strings.TrimRight(info.Body, "\n"); body != "" {
		k.body = ui.Span{Text: body, Font: a.font.family, Size: a.font.size - 1, Color: p.muted}
		_, k.bodyH = c.MeasureText(w, k.body)
		if one := lineOf(k.body); k.bodyH > maxCardBody*one+0.5 {
			k.folds = true
			if !cm.unfolded {
				k.bodyH = maxCardBody * one
			}
			h += cardMoreH
		}
		h += cardBodyGap + k.bodyH
	}
	k.lines = max(int(math.Ceil(float64(h/a.font.lineH))), 3)
	return k
}

// revisionLabel is how a comparison's card names a commit: HEAD while it
// is the one checked out, else its short hash.
func (a *gitContext) revisionLabel(hash string) string {
	if st := a.git.status; st != nil && st.Head == hash {
		return "HEAD"
	}
	return git.ShortHash(hash)
}

// copyCommitHash puts the hash of a commit on the clipboard.
func (a *app) copyCommitHash(hash string) {
	a.services.WriteClipboard(hash)
	a.tell(note{message: locales.CopiedText(git.ShortHash(hash))})
}

// commitCardUI shows the card at the top of a commit's diff at (x, y), w
// wide and h tall, its text selectable: its subject, who made it and
// when, its hash, which copies, and the rest of its message, which unfolds.
func (a *app) commitCardUI(c *ui.Context, v *diffView, k *commitCard, x, y, w, h float32) {
	p := a.pal
	cm := v.commit
	ui.Column(c.Key("commit-card")).Absolute().Left(x).Top(y).Width(w).Height(h).Clip().
		Padding(cardPadY, cardPadX, cardPadY, diffRailW+cardPadX).Justify(ui.Center).AlignItems(ui.Start).Selectable().Children(func() {
		subject := ui.RichText(c, k.subject).Width(k.w).MaxLines(maxCardSubject).Shrink(0)
		if k.cut {
			subject.Tooltip(k.subject.Text)
		}
		ui.Box(c).Height(cardGap).Shrink(0)
		dot := ui.Span{Text: "·", Size: headerFontSize, Color: p.faint}
		for _, line := range k.meta {
			ui.Row(c).Width(k.w).Height(cardMetaH).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				for i, pc := range line {
					if i > 0 && pc.dot {
						ui.RichText(c, dot).SingleLine().Shrink(0)
					}
					pc.show()
				}
			})
		}
		if k.body.Text == "" {
			return
		}
		ui.Box(c).Height(cardBodyGap).Shrink(0)
		body := ui.RichText(c, k.body).Width(k.w).Shrink(0)
		if k.folds && !cm.unfolded {
			body.MaxLines(maxCardBody)
		}
		if !k.folds {
			return
		}
		label := locales.ShowMore()
		if cm.unfolded {
			label = locales.ShowLess()
		}
		b := ui.ButtonBase(c.Key("card-more")).Height(cardMoreH).AlignItems(ui.Center).Shrink(0).Label(label)
		col := p.accent
		if b.Hovered() {
			col = mix(p.accent, p.text, 0.3)
		}
		if b.Clicked() {
			a.post(func() {
				cm.unfolded = !cm.unfolded
				v.changed()
			})
		}
		b.Children(func() { ui.Text(c, label).FontSize(headerFontSize).TextColor(col).SingleLine() })
	})
}

// avatarColor is the color of the circle of an author's initials, the
// same for each author.
func avatarColor(p *palette, who string) ui.Color {
	h := uint32(2166136261)
	for i := 0; i < len(who); i++ {
		h = (h ^ uint32(who[i])) * 16777619
	}
	cols := []ui.Color{p.accent, p.added, p.changed, p.modified, p.removed, p.syntax[highlight.Type]}
	return cols[h%uint32(len(cols))]
}

// paintInitials paints the initials of who in the middle of b, in a
// bold font of size. It shapes them in their own font: RichText lays a
// small span on a line of the theme's font, whose baseline sits too low.
func paintInitials(pt *ui.Painter, b ui.Rect, who string, size float32, col ui.Color) {
	paintLabel(pt, b, initials(who), size, col)
}

// paintLabel paints s in the middle of b, as paintInitials paints
// initials.
func paintLabel(pt *ui.Painter, b ui.Rect, s string, size float32, col ui.Color) {
	f := ui.Font{Size: size, Weight: 700}
	gs := ui.Shape(s, f)
	var w float32
	for _, g := range gs {
		w += g.Advance
	}
	m := f.Metrics()
	pt.Glyphs(gs, b.X+(b.W-w)/2, b.Y+b.H/2+(m.Ascent-m.Descent)/2, col)
}

// initials returns the first letters of the first two words of a name.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r, _ := utf8.DecodeRuneInString(w)
		out = append(out, unicode.ToUpper(r))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// paintCommitCard paints the ground of the card at the top of a commit's
// diff, which commitCardUI fills.
func (a *app) paintCommitCard(pt *ui.Painter, r ui.Rect) {
	p := a.pal
	pt.Fill(r, p.chrome, 0)
	pt.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.border, 0)
}

// paintSection paints the header of the changes staged, or of those not:
// its name, how many files it has, and what it compares.
func (a *app) paintSection(pt *ui.Painter, base diffBase, files int, r ui.Rect) {
	p := a.pal
	pt.Fill(r, p.editor, 0)
	label := "CHANGES"
	if base == diffStaged {
		label = "STAGED CHANGES"
	}
	old, new := base.sides()
	x := r.X + diffRailW + 8
	span := ui.Span{Text: label, Size: 11, Weight: 600, Color: p.muted, LetterSpacing: 0.5}
	w, th := pt.MeasureText(0, span)
	pt.RichText(x, r.Y+(r.H-th)/2, 0, span)
	x += w + 8
	count := ui.Span{Text: fmt.Sprint(files), Size: 10.5, Weight: 600, Color: p.muted}
	cw, ch := pt.MeasureText(0, count)
	pill := ui.Rect{X: x, Y: r.Y + r.H/2 - 8, W: max(cw+10, 18), H: 16}
	pt.Fill(pill, p.text.Alpha(0.07), 8)
	pt.RichText(pill.X+(pill.W-cw)/2, r.Y+(r.H-ch)/2, 0, count)
	x += pill.W + 10
	uiText(pt, x, r.Y, r.H, old+" → "+new, 11.5, 400, p.faint)
}

// paintFiller paints the room across from lines the other side has.
func (a *app) paintFiller(pt *ui.Painter, r ui.Rect) {
	p := a.pal
	pt.Fill(r, p.text.Alpha(0.025), 0)
	var stripes ui.Path
	for x := r.X - r.H; x < r.X+r.W; x += 8 {
		stripes.MoveTo(x, r.Y+r.H).LineTo(x+r.H, r.Y)
	}
	pt.StrokePath(&stripes, 1, p.text.Alpha(0.04))
}

// paintDiffLine paints line n of a side of a file of a diff at x, y: its
// words that changed from the line peer of the other side, then its text
// in the colors of its tokens, on rows cols wide as it wraps.
func (a *app) paintDiffLine(pt *ui.Painter, file *diffFile, newSide bool, n, peer int, x, y float32, tint ui.Color, cols int, wrap bool) {
	f, p := a.font, a.pal
	s := file.side(newSide)
	if n < 0 || n >= len(s.lines) {
		return
	}
	text := clipLine(s.lines[n])
	shown, at := expandTabs(text)
	// Where each row starts in shown, and ends.
	breaks := []int{0}
	if wrap {
		breaks = append(breaks, wrapAt(shown, cols)...)
	}
	breaks = append(breaks, len(shown))
	// piece calls fn with each part of the run from b0 up to b1 of shown,
	// on its row: the row, and where the part starts and ends on it.
	piece := func(b0, b1 int, fn func(k int, x0, x1 float32, s0, s1 int)) {
		for k := 0; k+1 < len(breaks); k++ {
			lo, hi := breaks[k], breaks[k+1]
			s0, s1 := max(b0, lo), min(b1, hi)
			if s0 >= s1 && !(b0 == b1 && b0 == lo) {
				continue
			}
			fn(k, f.width(shown[lo:s0]), f.width(shown[lo:s1]), s0, s1)
		}
	}
	if peer >= 0 {
		key := [2]int{n, peer}
		if !newSide {
			key = [2]int{peer, n}
		}
		ws, ok := file.words[key]
		if !ok && key[0] < len(file.old.lines) && key[1] < len(file.new.lines) {
			o, w := linediff.Words(file.old.lines[key[0]], file.new.lines[key[1]])
			ws = [2][]linediff.Span{o, w}
			if file.words == nil {
				file.words = map[[2]int][2][]linediff.Span{}
			}
			file.words[key] = ws
		}
		spans := ws[0]
		if newSide {
			spans = ws[1]
		}
		for _, sp := range spans {
			if sp.Start >= len(text) {
				continue
			}
			piece(at(sp.Start), at(min(sp.End, len(text))), func(k int, x0, x1 float32, _, _ int) {
				pt.Fill(ui.Rect{X: x + x0, Y: y + float32(k)*f.lineH, W: max(x1-x0, 2), H: f.lineH}, tint.Alpha(0.30), 0)
			})
		}
	}
	// The runs of shown in the colors of their tokens.
	type run struct {
		from, to int
		col      ui.Color
	}
	var runs []run
	plain := func(from, to int) {
		if to > from {
			runs = append(runs, run{from, to, p.text})
		}
	}
	var toks []highlight.Token
	if s.hl != nil && len(s.lines[n]) <= highlight.MaxLineRunes {
		toks = s.hl.Line(n, s.lines[n])
	}
	if len(toks) == 0 {
		plain(0, len(shown))
	} else {
		// Tokens are in runes from the line's start.
		runeAt := make([]int, 0, len(text)+1)
		for i := range text {
			runeAt = append(runeAt, i)
		}
		runeAt = append(runeAt, len(text))
		byteOf := func(r int) int { return runeAt[min(r, len(runeAt)-1)] }
		done := 0
		for _, t := range toks {
			b0, b1 := at(byteOf(t.Start)), at(byteOf(t.End))
			if b0 < done || b1 <= b0 {
				continue
			}
			plain(done, b0)
			runs = append(runs, run{b0, b1, p.syntax[t.Kind]})
			done = b1
		}
		plain(done, len(shown))
	}
	rowSpans := make([][]ui.Span, len(breaks)-1)
	for _, rn := range runs {
		piece(rn.from, rn.to, func(k int, _, _ float32, s0, s1 int) {
			if s1 > s0 {
				rowSpans[k] = append(rowSpans[k], f.span(shown[s0:s1], rn.col))
			}
		})
	}
	for k, spans := range rowSpans {
		if len(spans) > 0 {
			_, h := pt.MeasureText(0, spans[0])
			pt.RichText(x, y+float32(k)*f.lineH+(f.lineH-h)/2, 0, spans...)
		}
	}
}

// fileButtons are the buttons of a file's header, whose right is at right
// and top at y in the diff's content: opening the file, staging or
// unstaging it, and discarding its changes. It returns where they are.
func (a *app) fileButtons(c *ui.Context, v *diffView, f *diffFile, right, y float32) ui.Rect {
	ctx := a.diffContext(v)
	e := f.entry
	type button struct {
		icon  *ui.SVG
		label string
		fn    func()
	}
	var bs []button
	if f.letter != 'D' || f.base == diffStaged {
		bs = append(bs, button{iconOpenFile, openLabel(e), func() { a.openFileAt(f.path, 0, true) }})
	}
	switch {
	case f.base == diffCommit:
	case f.base == diffStaged:
		bs = append(bs, button{iconMinus, locales.UnstageChanges(), func() { ctx.unstage(entryPaths(e, true)...) }})
	default:
		if !e.Submodule() {
			bs = append(bs, button{iconUndo, locales.DiscardChangesEllipsis(), func() { ctx.discard(e) }})
		}
		bs = append(bs, button{iconPlus, locales.StageChanges(), func() { ctx.stage(e.Path) }})
	}
	if len(bs) == 0 {
		return ui.Rect{}
	}
	w := float32(len(bs)) * 22
	r := ui.Rect{X: right - a.fileHeaderRight(f) - 10 - w, Y: y, W: w, H: a.font.lineH}
	ui.Row(c.Key("file-buttons")).Absolute().Left(r.X).Top(y + (a.font.lineH-20)/2).Gap(2).Children(func() {
		for _, b := range bs {
			if a.smallButton(c, b.icon, b.label) {
				a.post(b.fn)
			}
		}
	})
	return r
}
