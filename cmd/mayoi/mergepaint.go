package main

import (
	"cmp"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// mergeViewUI shows the three-way merge of a conflicted file: the two
// sides read only beside the result, which the user edits, with the
// conflicts marked in all three, the actions of the one chosen above
// them, and the file's resolution at the end.
func (a *app) mergeViewUI(c *ui.Context, v *mergeView) {
	p := a.pal
	a.refreshMerge(v)
	message := func(msg string) {
		ui.Column(c).Fill().Background(p.editor).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(p.muted)
		})
	}
	if v.err != "" {
		message(v.err)
		return
	}
	if v.binary {
		message(locales.MergeBinary())
		return
	}
	if !v.loaded || v.doc == nil || v.loading {
		ui.Box(c).Fill().Background(p.editor)
		return
	}
	// The regions follow the result's text, whatever changed it: undo
	// brings the sides it took back, redo puts them in again.
	if ver := v.doc.buf.Version(); ver != v.gen {
		v.syncRegions()
	}
	if v.ed != nil {
		if !a.settings.Vim {
			vimReset(v.ed)
		}
		// The typing of the result is the merge editor's while the result
		// pane has the keyboard, also when the file was open already and
		// its tab editor held it before.
		if v.panes[1].scroll.FocusWithin(c) {
			v.doc.typist = v.ed
		}
		v.ed.observe()
		if len(v.ed.cmds) > 0 {
			cmds := v.ed.cmds
			v.ed.cmds = nil
			for _, cmd := range cmds {
				cmd(v.ed)
			}
			a.invalidate()
		}
		if v.ed.focusNext {
			v.ed.focusNext = false
			v.panes[1].scroll.Focus(c)
		}
	}
	a.syncMergePanes(v)
	ui.Column(c).Fill().Background(p.editor).Children(func() {
		a.mergeHead(c, v)
		a.mergeActions(c, v)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.mergePane(c, v, 0, v.oursLabel(), v.oursTip())
			a.mergeGutter(c, v, 0)
			a.mergePane(c, v, 1, v.resultLabel(), v.resultTip())
			a.mergeGutter(c, v, 1)
			a.mergePane(c, v, 2, v.theirsLabel(), v.theirsTip())
		})
	})
}

// mergeHead is the bar over the panes: the file in conflict, how many of
// its conflicts stand, how many of the files of the merge stand resolved,
// and what ends them, over a line that fills as the conflicts resolve.
func (a *app) mergeHead(c *ui.Context, v *mergeView) {
	p := a.pal
	resolved := a.mergeResolved(v)
	filesResolved, filesTotal := a.mergeResolvedCount()
	head := ui.Column(c.Key("merge-head")).Background(p.chrome)
	head.Children(func() {
		ui.Row(c).Height(34).Padding(0, 10, 0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(22, 22).Radius(6).Shrink(0).Background(p.changed.Alpha(0.14)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconMerge).FontSize(13).TextColor(p.changed)
			})
			ui.Text(c, v.name()).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
			tip := locales.MergeConflictTip()
			if op := v.op.Title(); op != "" {
				tip = locales.MergeOpStoppedFile(op)
			}
			ui.Text(c, tip).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
			if n := len(v.regions); n > 0 {
				un := v.unresolved()
				pill := ui.Box(c.Key("merge-count")).Height(18).Padding(0, 7).Radius(9).Shrink(0).
					AlignItems(ui.Center).Justify(ui.Center).
					Label(locales.MergeConflictsUnresolved()).Tooltip(locales.MergeConflictsUnresolvedTip())
				col := p.text
				if un > 0 {
					pill.Background(p.danger.Alpha(0.16))
					col = p.dangerFg
				} else {
					pill.Background(p.added.Alpha(0.16))
					col = p.addedFg
				}
				pill.Transition(hoverFade)
				pill.Children(func() {
					ui.RichText(c, ui.Span{Text: locales.MergeConflictsCount(strconv.Itoa(un), strconv.Itoa(n)), Size: 10.5, Weight: 650, Color: col}).SingleLine()
				})
			}
			if filesTotal > 1 {
				ui.Text(c, locales.MergeFilesResolved(strconv.Itoa(filesResolved), strconv.Itoa(filesTotal))).
					FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(0).
					Tooltip(locales.MergeFilesResolvedTip(cmp.Or(v.op.Title(), locales.MergeOpMerge())))
			}
			ui.Spacer(c)
			if !resolved && !v.binary && v.loaded {
				b := a.mergeButtonBase(c, "merge-apply", locales.MergeApply(), locales.MergeApplyTip())
				b.Menu(func(m *ui.Menu) {
					if m.Item(locales.MergeApplyAll()).Chosen() {
						a.post(func() { a.applyNonConflicts(v, applyAll) })
					}
					if m.Item(locales.MergeApplyLeft()).Chosen() {
						a.post(func() { a.applyNonConflicts(v, applyLeft) })
					}
					if m.Item(locales.MergeApplyRight()).Chosen() {
						a.post(func() { a.applyNonConflicts(v, applyRight) })
					}
					if m.Item(locales.MergeApplyAuto()).Checked(a.settings.MergeAutoApply).Chosen() {
						a.post(func() { a.setMergeAutoApply(!a.settings.MergeAutoApply) })
					}
				})
				col := p.text
				b.Children(func() {
					ui.Icon(c, iconChevronsRight).FontSize(12).TextColor(p.muted)
					ui.Text(c, locales.MergeApply()).FontSize(11.5).FontWeight(500).TextColor(col).SingleLine()
					ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.muted)
				})
			}
			if !resolved {
				if a.mergeIconButton(c, "merge-reset", iconUndo, locales.MergeResetBase(), locales.MergeResetBaseTip()) {
					a.post(func() { a.resetMerge(v) })
				}
			}
			mergeDivider(c, p)
			if a.mergeButton(c, "merge-save", locales.ButtonSave(), locales.MergeSaveTip()) {
				a.post(func() { a.saveMerge(v) })
			}
			if !resolved && filesTotal > 1 && a.mergeButton(c, "merge-apply-next", locales.MergeApplyChanges(), locales.MergeApplyChangesTip()) {
				a.post(func() { a.applyMergeChanges(v) })
			}
			if filesTotal > 0 && a.mergeButton(c, "merge-save-close", locales.MergeSaveClose(), locales.MergeSaveCloseTip()) {
				a.post(func() { a.saveCloseMerge(v) })
			}
			if filesTotal > 0 && a.mergeButton(c, "merge-files", locales.MergeConflictsEllipsis(), locales.MergeConflictsTip(cmp.Or(v.op.Title(), locales.MergeOpMerge()))) {
				a.post(a.openConflicts)
			}
			if !resolved {
				b := ui.PrimaryButton(c.Key("merge-resolved"), locales.MergeMarkResolved()).Height(24)
				b.Tooltip(locales.MergeMarkResolvedTip())
				pressPose(b)
				if b.Clicked() {
					a.post(func() { a.completeMerge(v) })
				}
			}
		})
		// How far the conflicts are resolved, filling as they are.
		done := float32(1)
		if n := len(v.regions); n > 0 && !resolved {
			done = float32(n-v.unresolved()) / float32(n)
		}
		fill := head.AnimateTo("done", done, ui.Smooth)
		col := p.accent
		if done >= 1 {
			col = p.added
		}
		col = head.AnimateColor("done-color", col, ui.Animation{Duration: 200 * time.Millisecond})
		ui.Box(c).Height(2).Draw(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(r, p.border.Alpha(0.6), 0)
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * clamp(fill, 0, 1), H: r.H}, col, 0)
		})
	})
}

// mergeActions is the bar of the conflict chosen, or of the file once it
// stands resolved.
func (a *app) mergeActions(c *ui.Context, v *mergeView) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 10, 0, 6).Gap(6).AlignItems(ui.Center).Background(p.chrome).Children(func() {
		if a.mergeResolved(v) {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(p.added).Shrink(0)
			done := locales.MergeFileResolved()
			if op := v.op.Title(); op != "" {
				done = locales.MergeFileResolvedOp(op)
			}
			ui.Text(c, done).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
			ui.Spacer(c)
			if v.op != "" && a.mergeButton(c, "merge-continue", locales.MergeContinueOp(v.op.Title()), locales.MergeContinueOpTip(v.op.Title())) {
				a.post(a.continueOp)
			}
			if a.mergeButton(c, "merge-restore", locales.MergeRestoreConflict(), locales.MergeRestoreConflictTip()) {
				a.post(func() { a.restoreMerge(v) })
			}
			if a.mergeButton(c, "merge-open", locales.OpenFile(), locales.MergeOpenFileTip()) {
				a.post(func() { a.openPath(v.path, true) })
			}
			return
		}
		if a.iconButton(c.Key("merge-prev"), iconArrowUp, locales.MergePrevConflict(), false).Clicked() {
			a.post(func() { a.stepMergeRegion(v, -1) })
		}
		if a.iconButton(c.Key("merge-next"), iconArrowDown, locales.MergeNextConflict(), false).Clicked() {
			a.post(func() { a.stepMergeRegion(v, 1) })
		}
		i := v.chosen
		if i < 0 || i >= len(v.regions) {
			ui.Text(c, locales.MergeConflictsToResolve(len(v.regions))).FontSize(11.5).TextColor(p.muted).SingleLine()
			ui.Spacer(c)
			return
		}
		r := v.regions[i]
		st := v.stateOf(r)
		state := map[regionState]string{
			regionUndecided: locales.MergeStateBase(),
			regionLeft:      locales.MergeStateOurs(),
			regionRight:     locales.MergeStateTheirs(),
			regionBoth:      locales.MergeStateBoth(),
			regionEdited:    locales.MergeStateEdited(),
			regionIgnored:   locales.MergeStateIgnored(),
		}[st]
		dot := p.added
		switch st {
		case regionUndecided:
			dot = p.danger
		case regionEdited:
			dot = p.changed
		}
		ui.Box(c.Key("merge-state-dot")).Size(7, 7).Radius(3.5).Shrink(0).Background(dot).Transition(hoverFade)
		ui.Text(c, locales.MergeConflictOf(strconv.Itoa(i+1), strconv.Itoa(len(v.regions)), state)).
			FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
		ui.Spacer(c)
		if a.mergeIconButton(c, "merge-accept-l", iconChevronsRight, locales.DiffAcceptLeft(), locales.MergeAcceptLeftTip()) {
			a.post(func() { a.takeMergeSide(v, i, true); a.flashMergeRegion(v, i) })
		}
		if a.mergeIconButton(c, "merge-ignore-l", iconX, locales.MergeIgnoreLeft(), locales.MergeIgnoreLeftTip()) {
			a.post(func() { a.dropMergeSide(v, i, true); a.flashMergeRegion(v, i) })
		}
		mergeDivider(c, p)
		if a.mergeIconButton(c, "merge-ignore-r", iconX, locales.MergeIgnoreRight(), locales.MergeIgnoreRightTip()) {
			a.post(func() { a.dropMergeSide(v, i, false); a.flashMergeRegion(v, i) })
		}
		if a.mergeIconButton(c, "merge-accept-r", iconChevronsLeft, locales.DiffAcceptRight(), locales.MergeAcceptRightTip()) {
			a.post(func() { a.takeMergeSide(v, i, false); a.flashMergeRegion(v, i) })
		}
		mergeDivider(c, p)
		if a.mergeButton(c, "merge-use-l", locales.MergeUseLeft(), locales.MergeUseLeftTip()) {
			a.post(func() { a.resolveUsingMerge(v, true) })
		}
		if a.mergeButton(c, "merge-use-r", locales.MergeUseRight(), locales.MergeUseRightTip()) {
			a.post(func() { a.resolveUsingMerge(v, false) })
		}
	})
}

// mergeDivider is a short line between the groups of buttons of a merge's
// bars.
func mergeDivider(c *ui.Context, p *palette) {
	ui.Box(c).Size(1, 14).Shrink(0).Margin(0, 2, 0, 2).Background(p.border)
}

// mergeButtonBase is a small button of the merge's bars, outlined, its
// face filling under the pointer, to put its content in.
func (a *app) mergeButtonBase(c *ui.Context, key, label, tip string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Height(22).Padding(0, 8).Gap(5).Radius(5).AlignItems(ui.Center).Shrink(0).
		Label(label).Tooltip(tip).Border(1, p.border)
	if b.Hovered() {
		b.Background(p.hover.Over(p.chrome))
	}
	b.Transition(hoverFade)
	pressPose(b)
	return b
}

// mergeButton is a small button of the merge's bars, and reports whether
// it was clicked.
func (a *app) mergeButton(c *ui.Context, key, label, tip string) bool {
	p := a.pal
	b := a.mergeButtonBase(c, key, label, tip)
	b.Children(func() { ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(p.text).SingleLine() })
	return b.Clicked()
}

// mergeIconButton is mergeButton with an icon before its label.
func (a *app) mergeIconButton(c *ui.Context, key string, icon *ui.SVG, label, tip string) bool {
	p := a.pal
	b := a.mergeButtonBase(c, key, label, tip)
	col := p.muted
	if b.Hovered() {
		col = p.text
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(col)
		ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(p.text).SingleLine()
	})
	return b.Clicked()
}

// mergePane is one of the three panes of a merge: its label over a pane
// that scrolls its text both ways, the conflicts marked in it. Pane 1,
// the result, is the one edited.
func (a *app) mergePane(c *ui.Context, v *mergeView, k int, label, tip string) {
	p, f := a.pal, a.font
	buf := v.paneBuf(k)
	if buf == nil {
		return
	}
	ui.Column(c.Key(fmt.Sprintf("merge-pane-%d", k))).Grow(1).MinWidth(0).Children(func() {
		head := ui.Row(c.Key(fmt.Sprintf("merge-head-%d", k))).Height(20).Padding(0, 8, 0, diffRailW+8).
			AlignItems(ui.Center).Background(p.chrome).Label(label).Tooltip(tip)
		head.Children(func() {
			ui.Text(c, label).FontSize(10.5).FontWeight(600).LetterSpacing(0.4).TextColor(p.muted).SingleLine().Shrink(1)
		})
		pane := ui.ScrollHorizontal(c.Key(fmt.Sprintf("merge-scroll-%d", k))).TrackScroll(&v.panes[k].hscroll).
			Grow(1).MinHeight(0).Background(p.editor)
		pane.Children(func() {
			// The pane's room is the scroller's, which the text area,
			// as wide as its longest line, may be wider than.
			view := v.panes[k].hscroll.Bounds(c)
			view.H = v.panes[k].scroll.Bounds(c).H
			v.viewW, v.viewH = view.W, view.H
			v.panes[k].viewH = view.H
			// The width of the longest line, worked out again as the text
			// changes: wideVer is one past the version it was of.
			ver := buf.Version()
			if v.panes[k].wideVer != ver+1 {
				v.panes[k].wideVer = ver + 1
				v.panes[k].widest = widestOf(v.paneLines(k))
			}
			contentW := max(view.W, textXOf(f, v.panes[k].widest))
			area := ui.TextAreaBufferBase(c, buf).
				Font(f.family).FontSize(f.size).FontWeight(f.weight).FixedLineHeight(f.lineH).TextColor(p.text).
				FillHeight().Padding(editorPadY, textPadR, editorPadY, mergeTextX).
				Label(label).Tooltip(tip)
			if k == 1 {
				vimKeys := a.settings.Vim && v.ed.vim.mode != vimInsert
				area.ReadOnly(vimKeys || a.chording() || v.ed.locked())
				a.takeKeys(area, placeEditor, func(ev ui.InputEvent) bool { return a.editorKey(v.ed, ev) })
				a.mergeResultSync(c, area, ver)
			} else {
				area.ReadOnly(true)
				a.takeKeys(area, placeDiff, func(ev ui.InputEvent) bool { return a.mergePaneKey(v, k, ev) })
			}
			area.NoWrap().Width(contentW).TrackScroll(&v.panes[k].scroll).
				ScrollbarInsets(2, max(0, v.panes[k].hscroll.MaxX-v.panes[k].hscroll.X)+2, 2, 0)
			area.Draw(func(pt *ui.Painter, r ui.Rect) { a.paintMergeRegions(pt, v, k, r) })
		})
	})
}

// mergeResultSync gives the result's text area the editor's selection,
// and the editor the one the area has, as the editor view does.
func (a *app) mergeResultSync(c *ui.Context, area ui.Element, ver uint64) {
	v := a.activeMerge()
	if v == nil || v.ed == nil {
		return
	}
	edited := ver != v.ed.version
	v.ed.version = ver
	if r := v.ed.pending; r != nil {
		area.SetTextSelection(r[0], r[1])
		v.ed.pending, v.ed.resync, v.ed.parked = nil, false, -1
	}
	if v.ed.resync || !v.ed.caretSeen {
		v.ed.resync = false
		area.SetTextSelection(v.ed.sel[0], v.ed.sel[1])
	}
	anchor, caret := area.TextAnchorCaret()
	if sel := [2]int{anchor, caret}; sel != v.ed.sel {
		if !edited {
			v.ed.hist.typing = false // the caret moved: typing after starts a step
		}
		v.ed.sel = sel
	}
	// The area has the selection now, whatever it reads as: a caret that
	// equals the one kept must not keep the merge re-pushing it, for the
	// push reveals the caret and the pane would snap back to it forever.
	v.ed.caretSeen = true
}

// activeMerge returns the merge view of the tab shown, nil when none is.
func (a *app) activeMerge() *mergeView {
	if t := a.activeTab(); t != nil {
		return t.merge
	}
	return nil
}

// mergePaneKey takes the keys of a read-only pane of a merge: scrolling,
// and going from conflict to conflict.
func (a *app) mergePaneKey(v *mergeView, pane int, ev ui.InputEvent) bool {
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		a.chooseMergeLine(v, pane, ev.Y)
		return false
	}
	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
		return false
	}
	lineH := a.font.lineH
	page := max(v.panes[pane].viewH-lineH, lineH)
	scroll := func(dy float32) {
		s := &v.panes[pane].scroll
		n := v.paneLineCount(pane)
		end := max(0, 2*editorPadY+float32(n)*lineH-v.panes[pane].viewH)
		s.Y = clamp(s.Y+dy, 0, end)
	}
	switch ev.Key {
	case ui.KeyJ, ui.KeyDown:
		scroll(lineH)
	case ui.KeyK, ui.KeyUp:
		scroll(-lineH)
	case ui.KeyF, ui.KeyPageDown:
		scroll(page)
	case ui.KeyB, ui.KeyPageUp:
		scroll(-page)
	case ui.KeyG:
		if ev.Mods&ui.Shift != 0 {
			scroll(1e9)
		} else {
			scroll(-1e9)
		}
	case ui.KeyBracketRight:
		a.stepMergeRegion(v, 1)
	case ui.KeyBracketLeft:
		a.stepMergeRegion(v, -1)
	default:
		return false
	}
	return true
}

// paintMergeRegions paints the conflicts of a pane: a tint over the
// lines each has there, the deeper for the one chosen, and a rail of the
// state by them.
func (a *app) paintMergeRegions(pt *ui.Painter, v *mergeView, k int, r ui.Rect) {
	p := a.pal
	lineH := a.font.lineH
	sc := v.panes[k].scroll.Y
	first := max(int((sc-editorPadY)/lineH)-1, 0)
	last := first + int(v.panes[k].viewH/lineH) + 3
	// The changes of a side's own that are no conflict, which the result
	// lacks.
	if k != 1 {
		for _, b := range a.mergeBands(v, k) {
			lo, hi := b.h.A, b.h.A+b.h.ALen
			if b.region >= 0 || !b.own || hi <= first || lo >= last {
				continue
			}
			y := r.Y + editorPadY + float32(lo)*lineH - sc
			h := max(float32(hi-lo)*lineH, 2)
			if hi == lo {
				y--
			}
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: h}, p.changed.Alpha(0.09), 0)
			pt.Fill(ui.Rect{X: r.X, Y: y, W: diffRailW, H: h}, p.changed.Alpha(0.6), 0)
		}
	}
	for i, reg := range v.regions {
		lo, hi := v.regionLines(reg, k)
		if lo < 0 || hi <= first || lo >= last {
			continue
		}
		if hi == lo {
			hi = lo + 1 // an insertion shows a row of its own
		}
		col := p.added
		switch st := v.stateOf(reg); {
		case st == regionUndecided:
			col = p.danger
		case st == regionEdited:
			col = p.changed
		case st == regionIgnored, st == regionLeft && k == 2, st == regionRight && k == 0:
			col = p.muted // the side the conflict did not take
		}
		y := r.Y + editorPadY + float32(lo)*lineH - sc
		h := float32(hi-lo) * lineH
		bg := col.Alpha(0.09)
		if i == v.chosen {
			bg = col.Alpha(0.20)
		}
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: h}, bg, 0)
		rail := col.Alpha(0.5)
		if i == v.chosen {
			rail = col
		}
		pt.Fill(ui.Rect{X: r.X, Y: y, W: diffRailW, H: h}, rail, 0)
	}
	if k == 1 {
		a.paintMergeFlash(pt, v, r)
	}
}

// paneBuf returns the text buffer a pane shows.
func (v *mergeView) paneBuf(k int) *ui.TextBuffer {
	switch k {
	case 0:
		return v.oursBuf
	case 2:
		return v.theirsBuf
	}
	if v.doc != nil {
		return v.doc.buf
	}
	return nil
}

// paneLines returns the lines a pane shows.
func (v *mergeView) paneLines(k int) []string {
	switch k {
	case 0:
		return v.oursLines
	case 2:
		return v.theirsLines
	}
	if v.doc != nil {
		return v.resultLines()
	}
	return nil
}

// paneLineCount returns how many lines a pane has.
func (v *mergeView) paneLineCount(k int) int {
	return len(v.paneLines(k))
}

// widestOf returns the longest line of lines, in columns.
func widestOf(lines []string) int {
	widest := 0
	for _, l := range lines {
		if n := columns(clipLine(l)); n > widest {
			widest = n
		}
	}
	return widest
}

// textXOf is where a pane's text starts, past the rail and the padding,
// and how wide its content is for its longest line.
func textXOf(f codeFont, widest int) float32 {
	return mergeTextX + textPadR + float32(widest)*f.charW
}
