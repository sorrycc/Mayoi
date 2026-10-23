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
// As the window narrows, the file actions go one by one into the
// overflow menu, the least needed first, each only once the row would
// not hold it, as the status bar's parts go.
func (a *app) mergeHead(c *ui.Context, v *mergeView) {
	p := a.pal
	resolved := a.mergeResolved(v)
	filesResolved, filesTotal := a.mergeResolvedCount()
	measure := func(s string, size float32) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: s, Size: size})
		return w
	}
	// The file actions of the row: on the bar while it fits, in the
	// overflow menu once it does not. Rank is the order they go in; the
	// counts go before them and only hide, the menu holds no word.
	button := func(label string) float32 { return mergeTextButtonW(c, label) }
	var acts []*mergeAction
	add := func(rank int, label, tip string, w float32, act func(), menu bool) {
		acts = append(acts, &mergeAction{rank: rank, label: label, tip: tip, w: w, act: act, menu: menu, onBar: true})
	}
	if n := len(v.regions); n > 0 {
		pill := locales.MergeConflictsCount(strconv.Itoa(v.unresolved()), strconv.Itoa(n))
		add(-2, pill, "", measure(pill, 10.5)+2*7, nil, false)
	}
	if filesTotal > 1 {
		count := locales.MergeFilesResolved(strconv.Itoa(filesResolved), strconv.Itoa(filesTotal))
		add(-1, count, "", measure(count, 11.5), nil, false)
	}
	if filesTotal > 0 {
		tip := locales.MergeConflictsTip(cmp.Or(v.op.Title(), locales.MergeOpMerge()))
		add(0, locales.MergeConflictsEllipsis(), tip, button(locales.MergeConflictsEllipsis()), a.openConflicts, true)
		add(1, locales.MergeSaveClose(), locales.MergeSaveCloseTip(), button(locales.MergeSaveClose()),
			func() { a.post(func() { a.saveCloseMerge(v) }) }, true)
	}
	if !resolved && filesTotal > 1 {
		add(2, locales.MergeApplyChanges(), locales.MergeApplyChangesTip(), button(locales.MergeApplyChanges()),
			func() { a.post(func() { a.applyMergeChanges(v) }) }, true)
	}
	add(3, locales.ButtonSave(), locales.MergeSaveTip(), button(locales.ButtonSave()),
		func() { a.post(func() { a.saveMerge(v) }) }, true)
	if !resolved {
		add(4, locales.MergeResetBase(), locales.MergeResetBaseTip(), button(locales.MergeResetBase())+12+5,
			func() { a.post(func() { a.resetMerge(v) }) }, true)
		add(5, locales.MergeMarkResolved(), locales.MergeMarkResolvedTip(), measure(locales.MergeMarkResolved(), 13)+2*14+8,
			func() { a.post(func() { a.completeMerge(v) }) }, true)
	}
	head := ui.Column(c.Key("merge-head")).Background(p.chrome)
	head.Children(func() {
		row := ui.Row(c.Key("merge-head-row")).Height(34).Padding(0, 10, 0, 10).Gap(8).AlignItems(ui.Center)
		// The row's room, as the last frame laid it out — a frame behind,
		// as the panel's title and the commit message have it.
		width := row.Bounds().W
		if width <= 0 {
			width, _ = c.Size()
		}
		// What the row holds besides them: its padding, the icon, the
		// name, and the divider between Apply and the rest. The tip's
		// gaps and the name's weight the measures miss go in too.
		used := 2*10 + 22 + measure(v.name(), 13) + 6 + 3*8 + 2*8 + 4
		if !resolved {
			used += 21
		}
		any := mergeDrop(acts, used, width, 8)
		onBar := func(rank int) *mergeAction { return mergeOnBar(acts, rank) }
		pillAct, countAct := onBar(-2), onBar(-1)
		row.Children(func() {
			ui.Box(c).Size(22, 22).Radius(6).Shrink(0).Background(p.changed.Alpha(0.14)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconMerge).FontSize(13).TextColor(p.changed)
			})
			ui.Text(c, v.name()).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
			tip := locales.MergeConflictTip()
			if op := v.op.Title(); op != "" {
				tip = locales.MergeOpStoppedFile(op)
			}
			ui.Text(c, tip).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
			if pillAct != nil {
				col := p.text
				pillBox := ui.Box(c.Key("merge-count")).Height(18).Padding(0, 7).Radius(9).Shrink(0).
					AlignItems(ui.Center).Justify(ui.Center).
					Label(locales.MergeConflictsUnresolved()).Tooltip(locales.MergeConflictsUnresolvedTip())
				if v.unresolved() > 0 {
					pillBox.Background(p.danger.Alpha(0.16))
					col = p.dangerFg
				} else {
					pillBox.Background(p.added.Alpha(0.16))
					col = p.addedFg
				}
				pillBox.Transition(hoverFade)
				pillBox.Children(func() {
					ui.RichText(c, ui.Span{Text: pillAct.label, Size: 10.5, Weight: 650, Color: col}).SingleLine()
				})
			}
			if countAct != nil {
				ui.Text(c, countAct.label).
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
				if ac := onBar(4); ac != nil {
					if a.mergeIconButton(c, "merge-reset", iconUndo, ac.label, ac.tip) {
						ac.act()
					}
				}
			}
			// The divider stands while a button it separates does.
			if !resolved && (onBar(3) != nil || onBar(2) != nil || onBar(1) != nil || onBar(0) != nil || onBar(5) != nil) {
				mergeDivider(c, p)
			}
			if ac := onBar(3); ac != nil {
				if a.mergeButton(c, "merge-save", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := onBar(2); ac != nil {
				if a.mergeButton(c, "merge-apply-next", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := onBar(1); ac != nil {
				if a.mergeButton(c, "merge-save-close", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := onBar(0); ac != nil {
				if a.mergeButton(c, "merge-files", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := onBar(5); ac != nil {
				b := ui.PrimaryButton(c.Key("merge-resolved"), ac.label).Height(24)
				b.Tooltip(ac.tip)
				pressPose(b)
				if b.Clicked() {
					ac.act()
				}
			}
			if any {
				a.mergeMore(c, "merge-more", acts)
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
// stands resolved. Its actions go into its overflow menu one by one as
// the window narrows, as the head's do.
func (a *app) mergeActions(c *ui.Context, v *mergeView) {
	p := a.pal
	resolved := a.mergeResolved(v)
	row := ui.Row(c.Key("merge-actions-row")).Height(30).Padding(0, 10, 0, 6).Gap(6).AlignItems(ui.Center).Background(p.chrome)
	// The row's room, as the last frame laid it out — a frame behind.
	width := row.Bounds().W
	if width <= 0 {
		width, _ = c.Size()
	}
	button := func(label string) float32 { return mergeTextButtonW(c, label) }
	var acts []*mergeAction
	add := func(rank int, label, tip string, act func()) {
		acts = append(acts, &mergeAction{rank: rank, label: label, tip: tip, w: button(label), act: act, menu: true, onBar: true})
	}
	if resolved {
		if v.op != "" {
			add(2, locales.MergeContinueOp(v.op.Title()), locales.MergeContinueOpTip(v.op.Title()), func() { a.post(a.continueOp) })
		}
		add(1, locales.MergeRestoreConflict(), locales.MergeRestoreConflictTip(), func() { a.post(func() { a.restoreMerge(v) }) })
		add(0, locales.OpenFile(), locales.MergeOpenFileTip(), func() { a.post(func() { a.openPath(v.path, true) }) })
	} else if i := v.chosen; i >= 0 && i < len(v.regions) {
		// The bluntest go first: the whole-file sides, then the sides of
		// the conflict, the right before the left, keeping to the left.
		add(0, locales.MergeUseRight(), locales.MergeUseRightTip(), func() { a.post(func() { a.resolveUsingMerge(v, false) }) })
		add(1, locales.MergeUseLeft(), locales.MergeUseLeftTip(), func() { a.post(func() { a.resolveUsingMerge(v, true) }) })
		add(2, locales.MergeIgnoreRight(), locales.MergeIgnoreRightTip(), func() {
			a.post(func() { a.dropMergeSide(v, i, false); a.flashMergeRegion(v, i) })
		})
		add(3, locales.MergeIgnoreLeft(), locales.MergeIgnoreLeftTip(), func() {
			a.post(func() { a.dropMergeSide(v, i, true); a.flashMergeRegion(v, i) })
		})
		add(4, locales.DiffAcceptRight(), locales.MergeAcceptRightTip(), func() {
			a.post(func() { a.takeMergeSide(v, i, false); a.flashMergeRegion(v, i) })
		})
		add(5, locales.DiffAcceptLeft(), locales.MergeAcceptLeftTip(), func() {
			a.post(func() { a.takeMergeSide(v, i, true); a.flashMergeRegion(v, i) })
		})
	}
	// What the row holds besides the actions: its padding, the gaps, and
	// then the arrows of the conflicts and the state's dot, or the check
	// of a file resolved. The dividers come with their sides below.
	used := float32(2*10 + 6*6 + 6 + 6)
	if resolved {
		used += 13
	} else {
		used += 2*26 + 7
		if v.chosen >= 0 && v.chosen < len(v.regions) {
			used += 2 * 17 // the dividers between the groups
		}
	}
	any := mergeDrop(acts, used, width, 6)
	row.Children(func() {
		if resolved {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(p.added).Shrink(0)
			done := locales.MergeFileResolved()
			if op := v.op.Title(); op != "" {
				done = locales.MergeFileResolvedOp(op)
			}
			ui.Text(c, done).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
			ui.Spacer(c)
			if ac := mergeOnBar(acts, 2); ac != nil {
				if a.mergeButton(c, "merge-continue", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := mergeOnBar(acts, 1); ac != nil {
				if a.mergeButton(c, "merge-restore", ac.label, ac.tip) {
					ac.act()
				}
			}
			if ac := mergeOnBar(acts, 0); ac != nil {
				if a.mergeButton(c, "merge-open", ac.label, ac.tip) {
					ac.act()
				}
			}
			if any {
				a.mergeMore(c, "merge-actions-more", acts)
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
		if ac := mergeOnBar(acts, 5); ac != nil {
			if a.mergeIconButton(c, "merge-accept-l", iconChevronsRight, ac.label, ac.tip) {
				ac.act()
			}
		}
		if ac := mergeOnBar(acts, 3); ac != nil {
			if a.mergeIconButton(c, "merge-ignore-l", iconX, ac.label, ac.tip) {
				ac.act()
			}
		}
		// The dividers stand while a button of each side they separate
		// does: the conflict's left, its right, the whole file's sides.
		if mergeOnBar(acts, 5) != nil || mergeOnBar(acts, 3) != nil {
			if mergeOnBar(acts, 2) != nil || mergeOnBar(acts, 4) != nil {
				mergeDivider(c, p)
			}
		}
		if ac := mergeOnBar(acts, 2); ac != nil {
			if a.mergeIconButton(c, "merge-ignore-r", iconX, ac.label, ac.tip) {
				ac.act()
			}
		}
		if ac := mergeOnBar(acts, 4); ac != nil {
			if a.mergeIconButton(c, "merge-accept-r", iconChevronsLeft, ac.label, ac.tip) {
				ac.act()
			}
		}
		if mergeOnBar(acts, 5) != nil || mergeOnBar(acts, 3) != nil || mergeOnBar(acts, 2) != nil || mergeOnBar(acts, 4) != nil {
			if mergeOnBar(acts, 1) != nil || mergeOnBar(acts, 0) != nil {
				mergeDivider(c, p)
			}
		}
		if ac := mergeOnBar(acts, 1); ac != nil {
			if a.mergeButton(c, "merge-use-l", ac.label, ac.tip) {
				ac.act()
			}
		}
		if ac := mergeOnBar(acts, 0); ac != nil {
			if a.mergeButton(c, "merge-use-r", ac.label, ac.tip) {
				ac.act()
			}
		}
		if any {
			a.mergeMore(c, "merge-actions-more", acts)
		}
	})
}

// mergeDivider is a short line between the groups of buttons of a merge's
// bars.
func mergeDivider(c *ui.Context, p *palette) {
	ui.Box(c).Size(1, 14).Shrink(0).Margin(0, 2, 0, 2).Background(p.border)
}

// mergeAction is an action of a merge bar: on its row while it fits, in
// the row's overflow menu once it does not. Rank is the order they go in,
// the least needed first; menu says the overflow holds it. The words of
// the head, which only hide, have none.
type mergeAction struct {
	rank  int
	label string
	tip   string
	w     float32
	act   func()
	menu  bool
	onBar bool
}

// mergeTextButtonW is the width of a merge bar's button of a label: the text,
// its padding and border, and a little the measure misses.
func mergeTextButtonW(c *ui.Context, label string) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: label, Size: 11.5})
	return w + 2*8 + 2 + 8
}

// mergeDrop marks the actions of a merge bar as its row drops them, the
// least needed first, each only once the row would not hold it, and says
// whether one went to the overflow. Used is what the row holds besides
// the actions, width the room it has, gap the room after every child.
func mergeDrop(acts []*mergeAction, used, width, gap float32) bool {
	const overflow = float32(12 + 2*8 + 2 + 4)
	for _, ac := range acts {
		used += ac.w + gap
	}
	any := false
	for _, ac := range acts {
		if used <= width {
			break
		}
		ac.onBar = false
		used -= ac.w + gap
		if ac.menu && !any {
			any = true
			used += overflow + gap
		}
	}
	return any
}

// mergeOnBar returns the action of a rank while it stands on the row.
func mergeOnBar(acts []*mergeAction, rank int) *mergeAction {
	for _, ac := range acts {
		if ac.rank == rank && ac.onBar {
			return ac
		}
	}
	return nil
}

// mergeMore is the ⋯ button of a merge bar's row, holding the actions
// that went from it, in the row's order.
func (a *app) mergeMore(c *ui.Context, key string, acts []*mergeAction) {
	p := a.pal
	b := a.mergeButtonBase(c, key, locales.MergeMoreActions(), locales.MergeMoreActions())
	b.Menu(func(m *ui.Menu) {
		for _, ac := range acts {
			if ac.onBar || !ac.menu {
				continue
			}
			if m.Item(ac.label).Chosen() {
				ac.act()
			}
		}
	})
	b.Children(func() {
		ui.Icon(c, iconMore).FontSize(12).TextColor(p.muted)
	})
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
			FillWidth().Grow(1).MinHeight(0).Background(p.editor)
		pane.Children(func() {
			// The pane's room is the scroller's, which the text area,
			// as wide as its longest line, may be wider than.
			view := v.panes[k].hscroll.Bounds(c)
			view.H = v.panes[k].scroll.Bounds(c).H
			v.viewW, v.viewH = view.W, view.H
			v.panes[k].viewH = view.H
			wrap := a.wrapsMerge(v)
			// The width of the longest line, worked out again as the text
			// changes: wideVer is one past the version it was of.
			ver := buf.Version()
			contentW := view.W
			if !wrap {
				if v.panes[k].wideVer != ver+1 {
					v.panes[k].wideVer = ver + 1
					v.panes[k].widest = widestOf(v.paneLines(k))
				}
				contentW = max(view.W, textXOf(f, v.panes[k].widest))
			}
			area := ui.TextAreaBufferBase(c, buf).
				Font(f.family).FontSize(f.size).FontWeight(f.weight).FixedLineHeight(f.lineH).TextColor(p.text).
				FillHeight().Padding(editorPadY, textPadR, editorPadY, mergeTextX).
				Label(label).Tooltip(tip)
			if k == 1 {
				vimKeys := a.settings.Vim && v.ed.vim.mode != vimInsert
				area.ReadOnly(vimKeys || a.chording() || v.ed.locked())
				a.takeKeys(area, placeEditor, func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputScroll && max(ev.DX, -ev.DX) > max(ev.DY, -ev.DY) {
						// A sideways swipe, or a mouse's Shift+wheel,
						// scrolls the pane, as the side panes' swipe
						// does: the editor's own sideways scroll is no
						// scroller's here, and taking either for it
						// would stand the pane still. Wrapped, there is
						// nothing to scroll, and the swipe stands the
						// text where it is.
						s := &v.panes[1].hscroll
						if x := clamp(s.X+ev.DX, 0, s.MaxX); x != s.X {
							s.X = x
							a.invalidate()
						}
						return true
					}
					return a.editorKey(v.ed, ev)
				})
				a.mergeResultSync(c, area, ver)
			} else {
				area.ReadOnly(true)
				a.takeKeys(area, placeDiff, func(ev ui.InputEvent) bool { return a.mergePaneKey(v, k, ev) })
			}
			// The area is as wide as the pane as it wraps, or as its
			// widest line as it does not, and the pane scrolls sideways to
			// that. The width comes from the pane's box rather than the
			// area's own: a wrapping area is measured as it currently
			// wraps, so a width read back from it feeds forward, and an
			// early frame pins the panes narrow beside a blank. The widest
			// line only grows, so what the sides read while they load
			// cannot shrink what stands.
			if contentW > 0 {
				area.Width(contentW)
			} else {
				area.FillWidth()
			}
			if wrap {
				v.panes[k].hscroll.X = 0
				area.TrackLines(&v.panes[k].lines)
			} else {
				area.NoWrap()
			}
			area.TrackScroll(&v.panes[k].scroll).
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
	g := a.mergeGeometry(v, pane)
	scroll := func(dy float32) {
		s := &v.panes[pane].scroll
		end := max(0, 2*editorPadY+g.top(v.paneLineCount(pane))-v.panes[pane].viewH)
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
	if a.wrapsMerge(v) {
		// In Draw the pane's area stands laid out: its lines can be laid
		// out for their height, rather than estimated.
		a.layOutMergeLines(v, k, v.paneBuf(k).Version(), r.W)
	}
	g := a.mergeGeometry(v, k)
	sc := v.panes[k].scroll.Y
	first := max(g.at(sc-editorPadY)-1, 0)
	last := g.at(sc+v.panes[k].viewH) + 2
	if !v.panes[k].sweeping {
		// A sweeping frame reads estimates: its range says nothing about
		// where the view is.
		v.panes[k].viewFirst, v.panes[k].viewLast = first, last
	}
	// The changes of a side's own that are no conflict, which the result
	// lacks.
	if k != 1 {
		for _, b := range a.mergeBands(v, k) {
			lo, hi := b.h.A, b.h.A+b.h.ALen
			if b.region >= 0 || !b.own || hi <= first || lo >= last {
				continue
			}
			y := r.Y + editorPadY + g.top(lo) - sc
			h := max(g.top(hi)-g.top(lo), 2)
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
		y := r.Y + editorPadY + g.top(lo) - sc
		h := g.top(hi) - g.top(lo)
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

// mergeGeometry places the lines of pane k of a merge in its content: a
// row of its own each as the lines do not wrap, or where the pane laid
// them out as they do — an editor's geometry over the pane's lines.
func (a *app) mergeGeometry(v *mergeView, k int) geometry {
	g := geometry{lineH: a.font.lineH, n: v.paneLineCount(k)}
	if a.wrapsMerge(v) {
		g.lines = &v.panes[k].lines
	}
	return g
}

// mergeLayOutMax is how many lines of a wrapping merge pane are laid out
// for their height at once: every line of the files a merge stops for, or
// the first ones of a giant one.
const mergeLayOutMax = 8192

// mergeLayOutStep is how many lines of a wrapping merge pane a frame
// lays out beyond those in view, until every line stands measured.
const mergeLayOutStep = 768

// layOutMergeLines lays out the lines of pane k as it wraps, once for
// its text and the width it has. A pane is narrow, so its lines wrap
// deep; the heights the area has not measured it estimates, and a scroll
// into estimated country — as revealing a conflict goes — lands the view
// where its heights say there is text that is not, and the pane shows a
// blank. The lines in view are laid out at once; the rest follow a band
// a frame, so that dragging the window or the sidebar lays out the view
// of the frame, not every line of a giant file three times over.
func (a *app) layOutMergeLines(v *mergeView, k int, ver uint64, w float32) {
	p := &v.panes[k]
	n := min(p.lines.Count(), mergeLayOutMax)
	if p.laidVer == ver && p.laidW == w && p.laidFrom == 0 && p.laidTo >= n {
		return
	}
	if p.laidVer != ver || p.laidW != w {
		// A new text or a new width: the lines in view first, the rest a
		// band a frame, sweeping outward until all stand measured. While
		// the width keeps changing the far lines wait, so a drag does
		// not reshape thousands of them a frame. The window is the view
		// as the last frame drew it, by line: the heights of this frame
		// are estimates until the area lays the new width out, and
		// reading them here would place the window by a guess.
		p.laidVer, p.laidW = ver, w
		slack := int(v.panes[k].viewH/a.font.lineH) + 48
		p.laidFrom = max(p.viewFirst-slack, 0)
		p.laidTo = min(p.viewLast+slack, n)
		if p.viewLast <= p.viewFirst {
			p.laidFrom, p.laidTo = 0, min(slack, n) // no frame has drawn: the head of the file
		}
		for i := p.laidFrom; i < p.laidTo; i++ {
			p.lines.Rows(i)
		}
		p.sweeping = p.laidFrom > 0 || p.laidTo < n
		return
	}
	if !p.sweeping {
		return
	}
	// The width stands: sweep outward a band a frame.
	from := max(p.laidFrom-mergeLayOutStep, 0)
	to := min(p.laidTo+mergeLayOutStep, n)
	for i := p.laidFrom; i < from; i++ {
		p.lines.Rows(i)
	}
	for i := p.laidTo; i < to; i++ {
		p.lines.Rows(i)
	}
	p.laidFrom, p.laidTo = from, to
	p.sweeping = p.laidFrom > 0 || p.laidTo < n
}
