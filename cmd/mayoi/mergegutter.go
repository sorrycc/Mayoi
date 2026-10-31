package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/linediff"
	"mayoi/locales"
)

// mergeMidW is the width of the gutters between the panes of a merge,
// where the changes of a side join the result's lines and their buttons
// stand, as JetBrains' merge has them.
const mergeMidW = 40

// mergeButtonW is the side of a round button of a merge's gutter.
const mergeButtonW float32 = 16

// pairHunks returns the hunks between a side pane of a merge, 0 for ours
// or 2 for theirs, and the result: the side's lines are A, the result's
// B. They are worked out again as the result changes.
func (v *mergeView) pairHunks(side int) []linediff.Hunk {
	if ver := v.doc.buf.Version(); !v.pairsOK || v.pairsVer != ver {
		res := v.resultLines()
		v.pairs = [2][]linediff.Hunk{linediff.Hunks(v.oursLines, res), linediff.Hunks(v.theirsLines, res)}
		if v.own[0] == nil && v.own[1] == nil {
			v.own = [2][]sideHunk{hunksOf(v.baseLines, v.oursLines), hunksOf(v.baseLines, v.theirsLines)}
		}
		v.pairsVer, v.pairsOK = ver, true
	}
	return v.pairs[btoi(side == 2)]
}

// mapMergeLine returns the line of one text across from line of another,
// by the hunks hs between them: of A to B, or of B to A when back is set.
// A line in a hunk goes through the hunk's lines of the other text at the
// pace of its own.
func mapMergeLine(hs []linediff.Hunk, line float32, back bool) float32 {
	off := float32(0)
	for _, h := range hs {
		a0, a1, b0, b1 := float32(h.A), float32(h.A+h.ALen), float32(h.B), float32(h.B+h.BLen)
		if back {
			a0, a1, b0, b1 = b0, b1, a0, a1
		}
		if line < a0 {
			break
		}
		if line < a1 {
			return b0 + (line-a0)*(b1-b0)/(a1-a0)
		}
		off = b1 - a1
	}
	return line + off
}

// syncMergePanes has the panes of a merge scroll together: the one that
// scrolled leads, and the others follow so that the line in its middle
// stands across from theirs, as the hunks between them say; toward the
// top and the end the line followed goes to the top and the end of the
// view, so that the panes start and end together.
func (a *app) syncMergePanes(v *mergeView) {
	// While a pane sweeps its wrapped lines outward, the heights of the
	// frame are part estimate, and mapping the panes onto each other by
	// them would carry the views away; they stand still until the sweep
	// has measured the file.
	for k := range 3 {
		if v.panes[k].sweeping {
			return
		}
	}
	moved := -1
	for k := range 3 {
		if v.panes[k].scroll.Y != v.lastY[k] {
			moved = k
			break
		}
	}
	if moved >= 0 && v.loaded && v.doc != nil {
		end := func(k int) float32 {
			return max(0, 2*editorPadY+a.mergeGeometry(v, k).top(v.paneLineCount(k))-v.panes[k].viewH)
		}
		h := v.panes[moved].viewH
		s := v.panes[moved].scroll.Y
		off := clamp(min(s, h/2)+h/2-min(end(moved)-s, h/2), 0, h)
		line := a.mergeGeometry(v, moved).atF(s + off - editorPadY)
		res := line
		if moved != 1 {
			res = mapMergeLine(v.pairHunks(moved), line, false)
		}
		for k := range 3 {
			if k == moved {
				continue
			}
			at := res
			if k != 1 {
				at = mapMergeLine(v.pairHunks(k), res, true)
			}
			v.panes[k].scroll.Y = clamp(editorPadY+a.mergeGeometry(v, k).topF(at)-off, 0, end(k))
		}
	}
	for k := range 3 {
		v.lastY[k] = v.panes[k].scroll.Y
	}
}

// mergeBand is a hunk between a side of a merge and the result, as the
// gutter between their panes joins them: the side's lines and the
// result's, the conflict it is of, -1 for none, whether it is a change
// of the side's own against the base, and its color.
type mergeBand struct {
	h      linediff.Hunk
	region int
	own    bool
	col    ui.Color
}

// mergeBandKey is what the bands of a merge are worked out for: the
// result's version, the conflicts ignored, and the palette's colors.
type mergeBandKey struct {
	ver     uint64
	ignored int
	pal     *palette
}

// mergeBands returns the bands of a side pane of a merge, 0 or 2, kept
// until the result, the conflicts ignored or the colors change: the
// panes and the gutters paint them in every frame.
func (a *app) mergeBands(v *mergeView, side int) []mergeBand {
	key := mergeBandKey{v.doc.buf.Version(), len(v.ignored), a.pal}
	i := btoi(side == 2)
	if v.bandsOK[i] && v.bandsKey[i] == key {
		return v.bands[i]
	}
	v.bands[i], v.bandsKey[i], v.bandsOK[i] = a.workOutBands(v, side), key, true
	return v.bands[i]
}

// workOutBands works the bands of a side pane of a merge out.
func (a *app) workOutBands(v *mergeView, side int) []mergeBand {
	p := a.pal
	hs := v.pairHunks(side)
	own := v.own[btoi(side == 2)]
	overlaps := func(lo, hi, lo2, hi2 int) bool {
		if lo == hi || lo2 == hi2 {
			return lo <= hi2 && lo2 <= hi
		}
		return lo < hi2 && lo2 < hi
	}
	out := make([]mergeBand, 0, len(hs))
	for _, h := range hs {
		b := mergeBand{h: h, region: -1}
		for i, r := range v.regions {
			if lo, hi := v.regionLines(r, side); overlaps(h.A, h.A+h.ALen, lo, hi) {
				b.region = i
				break
			}
		}
		for _, o := range own {
			if overlaps(h.A, h.A+h.ALen, o.blo, o.bhi) {
				b.own = true
				break
			}
		}
		switch {
		case b.region >= 0:
			switch v.stateOf(v.regions[b.region]) {
			case regionUndecided:
				b.col = p.danger
			case regionEdited:
				b.col = p.changed
			default:
				b.col = p.muted // a side the conflict did not take
			}
		case b.own:
			b.col = p.changed // a change of the side's that the result lacks
		default:
			b.col = p.muted // the other side's change, which the result has
		}
		out = append(out, b)
	}
	return out
}

// mergeGlow keys the glow of a band of a gutter of a merge.
type mergeGlow struct{ side, i int }

// mergeGutter is the gutter between two panes of a merge, left the
// first of them: 0 joins ours to the result, 1 the result to theirs.
// Each hunk between the side and the result is a band from the side's
// lines to the result's, and a conflict not decided has the buttons that
// take the side's lines or ignore them, by its lines on the side; a
// change of the side's own that is no conflict has one that applies it.
func (a *app) mergeGutter(c *ui.Context, v *mergeView, left int) {
	p := a.pal
	side := 0
	if left == 1 {
		side = 2
	}
	bands := a.mergeBands(v, side)
	// Where a pane's lines stand as that pane wraps them: the sides and
	// the result wrap each at their width, and the bands join their rows.
	var gs [3]geometry
	for k := range gs {
		gs[k] = a.mergeGeometry(v, k)
	}
	ui.Column(c.Key(fmt.Sprintf("merge-gutter-%d", left))).Width(mergeMidW).Shrink(0).Children(func() {
		ui.Box(c).Height(20).Background(p.chrome)
		box := ui.Box(c.Key(fmt.Sprintf("merge-bands-%d", left))).Grow(1).MinHeight(0).Background(p.text.Alpha(0.02)).Clip()
		viewH := v.panes[side].viewH
		y := func(k int, line int) float32 { return editorPadY + gs[k].top(line) - v.panes[k].scroll.Y }
		glow := map[int]float32{}
		box.Children(func() {
			hot := -1
			for i, b := range bands {
				top, bot := y(side, b.h.A), y(side, b.h.A+b.h.ALen)
				// A band keeps its buttons while any part of it stands in
				// the box: a wrapped conflict can run past the pane, and
				// its middle, the buttons' anchor, scrolls away long
				// before the band does. The anchor pins inside instead.
				if bot < 0 || top > viewH {
					continue
				}
				sy := top
				if b.h.ALen > 0 {
					sy = (top + bot) / 2
				}
				if sy < mergeButtonW/2 {
					sy = mergeButtonW / 2
				} else if sy > viewH-mergeButtonW/2 {
					sy = viewH - mergeButtonW/2
				}
				type button struct {
					icon       *ui.SVG
					label, tip string
					fn         func()
				}
				var bs []button
				accept := iconChevronsRight
				if side == 2 {
					accept = iconChevronsLeft
				}
				switch {
				case b.region >= 0 && v.stateOf(v.regions[b.region]) == regionUndecided:
					ri, l := b.region, side == 0
					label, tip, ignore, ignoreTip := locales.DiffAcceptLeft(), locales.MergeAcceptLeftTip(), locales.MergeIgnoreLeft(), locales.MergeIgnoreLeftTip()
					if !l {
						label, tip, ignore, ignoreTip = locales.DiffAcceptRight(), locales.MergeAcceptRightTip(), locales.MergeIgnoreRight(), locales.MergeIgnoreRightTip()
					}
					bs = append(bs,
						button{accept, label, tip, func() { a.takeMergeSide(v, ri, l); a.flashMergeRegion(v, ri) }},
						button{iconX, ignore, ignoreTip, func() { a.dropMergeSide(v, ri, l); a.flashMergeRegion(v, ri) }})
				case b.region < 0 && b.own:
					h, s := b.h, side
					bs = append(bs, button{accept, locales.MergeApplyChange(), locales.MergeApplyChangeTip(), func() { a.applyMergeHunk(v, s, h) }})
				}
				if side == 2 {
					// By the side on the right: the accepting one nearest it.
					for l, r := 0, len(bs)-1; l < r; l, r = l+1, r-1 {
						bs[l], bs[r] = bs[r], bs[l]
					}
				}
				for j, bt := range bs {
					x := 3 + float32(j)*(mergeButtonW+2)
					if side == 2 {
						x = mergeMidW - 3 - float32(len(bs)-j)*(mergeButtonW+2) + 2
					}
					btn := ui.ButtonBase(c.Key(fmt.Sprintf("merge-band:%d:%d:%d", side, i, j))).Absolute().Left(x).Top(sy-mergeButtonW/2).
						Size(mergeButtonW, mergeButtonW).Radius(mergeButtonW/2).AlignItems(ui.Center).Justify(ui.Center).
						Label(bt.label).Tooltip(bt.tip).Background(p.editor).Border(1, p.border)
					col := p.muted
					hover := btn.Hovered()
					if hover {
						hot = i
						col = p.fillText
						bg := p.fill
						if bt.icon == iconX {
							bg = p.dangerFill
							col = p.dangerText
						}
						btn.Background(bg).Border(1, bg).Shadow(0, 1, 4, 0, p.shadow)
					}
					btn.Transition(hoverFade)
					arrowPose(btn, hover)
					btn.Children(func() { ui.Icon(c, bt.icon).FontSize(mergeButtonW - 5).TextColor(col) })
					if btn.Clicked() {
						fn, ri := bt.fn, b.region
						a.post(func() {
							if ri >= 0 {
								v.chosen, v.current = ri, ri
							}
							fn()
						})
					}
				}
			}
			for i := range bands {
				if g := box.Animate(mergeGlow{side, i}, unit(i == hot), 140*time.Millisecond); g > 0 {
					glow[i] = g
				}
			}
		})
		box.Draw(func(pt *ui.Painter, r ui.Rect) {
			lk, rk := side, 1
			if side == 2 {
				lk, rk = 1, 2
			}
			xL, xR := r.X, r.X+r.W
			cx := (xL + xR) / 2
			// A band the scrolling carried partly past the box is cut at
			// its edge, not drawn on over the merge's header.
			pt.Clip(r, 0, func() {
				for i, b := range bands {
					ll, lh, rl, rh := b.h.A, b.h.A+b.h.ALen, b.h.B, b.h.B+b.h.BLen
					if side == 2 {
						ll, lh, rl, rh = rl, rh, ll, lh
					}
					lt, lb, rt, rb := r.Y+y(lk, ll), r.Y+y(lk, lh), r.Y+y(rk, rl), r.Y+y(rk, rh)
					if max(lb, rb) < r.Y || min(lt, rt) > r.Y+r.H {
						continue
					}
					alpha := float32(0.16)
					if b.region >= 0 && b.region == v.chosen {
						alpha = 0.26
					}
					alpha += 0.2 * glow[i]
					var band ui.Path
					band.MoveTo(xL, lt).CubeTo(cx, lt, cx, rt, xR, rt).LineTo(xR, rb).CubeTo(cx, rb, cx, lb, xL, lb).Close()
					pt.FillPath(&band, b.col.Alpha(alpha))
					var edges ui.Path
					edges.MoveTo(xL, lt).CubeTo(cx, lt, cx, rt, xR, rt)
					edges.MoveTo(xL, lb).CubeTo(cx, lb, cx, rb, xR, rb)
					pt.StrokePath(&edges, 1, b.col.Alpha(0.45+0.35*glow[i]))
				}
			})
		})
	})
}

// applyMergeHunk puts a side's lines of a hunk between it and the result
// of a merge in the result, as one step of undo: a change of the side's
// that no conflict holds.
func (a *app) applyMergeHunk(v *mergeView, side int, h linediff.Hunk) {
	lines := v.oursLines
	if side == 2 {
		lines = v.theirsLines
	}
	if v.ed == nil || h.A < 0 || h.A+h.ALen > len(lines) {
		return
	}
	v.replaceLines(h.B, h.B+h.BLen, clipLines(lines, h.A, h.A+h.ALen))
	v.flashLines(h.B, h.B+h.ALen)
}

// flashMergeRegion has the result's lines of region i glow.
func (a *app) flashMergeRegion(v *mergeView, i int) {
	if r := v.regionAt(i); r != nil && r.atLo >= 0 {
		v.flashLines(r.atLo, r.atHi)
	}
}

// flashLines has the result's lines lo up to hi glow a moment.
func (v *mergeView) flashLines(lo, hi int) {
	if hi <= lo {
		hi = lo + 1
	}
	v.flash = diffFlash{lo: lo, hi: hi}
}

// paintMergeFlash paints the glow of the result's lines just written,
// fading, in its pane's area r.
func (a *app) paintMergeFlash(pt *ui.Painter, v *mergeView, r ui.Rect) {
	fl := &v.flash
	if fl.hi <= fl.lo {
		return
	}
	now := pt.Now()
	if fl.at.IsZero() {
		fl.at = now
	}
	t := float32(now.Sub(fl.at)) / float32(flashFor)
	if t >= 1 {
		v.flash = diffFlash{}
		return
	}
	k := (1 - t) * (1 - t)
	g := a.mergeGeometry(v, 1)
	y := r.Y + editorPadY + g.top(fl.lo) - v.panes[1].scroll.Y
	pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: g.top(fl.hi) - g.top(fl.lo)}, a.pal.accent.Alpha(0.30*k), 0)
	pt.AnimationFrame()
}
