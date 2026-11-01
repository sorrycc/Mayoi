package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// mergeFile is a file of the command that stopped for conflicts: in
// conflict, or resolved while the command goes on.
type mergeFile struct {
	path     string
	resolved bool
}

// mergeFiles returns the files of the command that stopped for conflicts:
// those in conflict first, then those the merge view staged as resolved
// while the command goes on. Without a command stopped, or with every
// file of it resolved, there are none.
func (a *app) mergeFiles() []mergeFile {
	g := &a.git
	st := g.status
	if st == nil || st.Op == "" {
		if g.mergeDone != nil {
			g.mergeDone, g.mergeDoneOp = nil, ""
			if r := g.repo; r != nil {
				os.Remove(filepath.Join(r.Dir, resolvedRecord))
			}
		}
	} else if st.Op != g.mergeDoneOp {
		fresh := g.mergeDone == nil
		g.mergeDone, g.mergeDoneOp = map[string]bool{}, st.Op
		// The app may have started anew since the files were resolved:
		// the record in the repository has them back, for the command's
		// own identity only.
		if fresh {
			if r := g.repo; r != nil {
				g.loadResolvedRecord(r, st.Op)
			}
		}
	}
	var out []mergeFile
	inConflict := map[string]bool{}
	for _, e := range a.groupEntries()[groupMerge] {
		inConflict[e.Path] = true
		out = append(out, mergeFile{path: e.Path})
	}
	for _, p := range slices.Sorted(maps.Keys(g.mergeDone)) {
		if !inConflict[p] {
			out = append(out, mergeFile{path: p, resolved: true})
		}
	}
	return out
}

// mergeResolvedCount is how many of the files of the command stopped for
// conflicts stand resolved.
func (a *app) mergeResolvedCount() (resolved, total int) {
	for _, f := range a.mergeFiles() {
		if f.resolved {
			resolved++
		}
		total++
	}
	return resolved, total
}

// noteMergeResolved has the files of the command in progress that the
// merge view staged as resolved stand so in the list of them, and in the
// record of the repository, which has them back after the app starts
// anew.
func (a *app) noteMergeResolved(path string) {
	if st := a.git.status; st != nil && st.Op != "" {
		if st.Op != a.git.mergeDoneOp {
			a.git.mergeDone, a.git.mergeDoneOp = map[string]bool{}, st.Op
		}
		a.git.mergeDone[path] = true
		if r := a.git.repo; r != nil {
			a.saveResolvedRecord(r)
		}
	}
}

// resolvedRecord is the name of the record of the resolved files, in the
// repository's git directory, beside what Git itself keeps of them.
const resolvedRecord = "mayoi-resolved.json"

// resolvedRecordFile is what the record holds: the command it belongs
// to, and the files of it that stand resolved.
type resolvedRecordFile struct {
	Op    string   `json:"op"`
	Paths []string `json:"paths"`
}

// saveResolvedRecord writes the resolved files of the command in
// progress, as best it can: the list works without the record, which
// only has it back after the app starts anew.
func (a *app) saveResolvedRecord(r *git.Repo) {
	rec := resolvedRecordFile{Op: string(a.git.mergeDoneOp), Paths: slices.Sorted(maps.Keys(a.git.mergeDone))}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	vfs.WriteFile(filepath.Join(r.Dir, resolvedRecord), data, 0o644)
}

// loadResolvedRecord takes the resolved files of the command back from
// the record in the repository, whose entries that stand in conflict
// again the list passes over.
func (g *gitState) loadResolvedRecord(r *git.Repo, op git.Op) {
	data, err := vfs.ReadFile(filepath.Join(r.Dir, resolvedRecord))
	if err != nil {
		return
	}
	var rec resolvedRecordFile
	if json.Unmarshal(data, &rec) != nil || rec.Op != string(op) {
		return
	}
	for _, p := range rec.Paths {
		g.mergeDone[p] = true
	}
}

// conflictsList is the box of the files of the command that stopped for
// conflicts: those in conflict, to open one by one, and those resolved,
// with their resolution to take back.
type conflictsList struct {
	open  bool
	sel   int
	moved bool
}

// openConflicts shows the box of the files in conflict, when the command
// stopped for some.
func (a *app) openConflicts() {
	if len(a.mergeFiles()) == 0 {
		return
	}
	a.conflicts = conflictsList{open: true}
	a.invalidate()
}

func (a *app) closeConflicts() {
	a.conflicts.open = false
	if a.focus != paneTerminal {
		a.focusColumn()
	}
}

// chooseMergeFile does what a row of the box of the files in conflict
// does: a file in conflict opens its merge, a resolved one comes to the
// front, as its merge has it, or as the file is.
func (a *app) chooseMergeFile(f mergeFile) {
	if !f.resolved {
		a.closeConflicts()
		a.openMerge(f.path, true)
		return
	}
	a.closeConflicts()
	if t := a.mergeTab(f.path); t != nil {
		if c, i := a.locate(t); c != nil {
			a.keepTab(t)
			a.activate(c, i, true)
		}
		return
	}
	a.openPath(f.path, true)
}

// applyMergeChanges saves the result of the merge and stages the file,
// which ends its conflict, then opens the next file in conflict, as
// applying the changes of a file goes on to the next.
func (a *app) applyMergeChanges(v *mergeView) {
	if v.doc == nil {
		return
	}
	path := v.path
	var next, first string
	for _, f := range a.mergeFiles() {
		if f.resolved || f.path == path {
			continue
		}
		if first == "" {
			first = f.path
		}
		if f.path > path && next == "" {
			next = f.path
		}
	}
	if next == "" {
		next = first // the files stand by path, round the end back up
	}
	a.markMergeResolved(v, func() {
		if next != "" {
			a.openMerge(next, true)
		}
	})
}

// saveCloseMerge saves the result of the merge in the file, and closes
// the merge, back to the files in conflict while any stand.
func (a *app) saveCloseMerge(v *mergeView) {
	a.saveMerge(v)
	t := a.mergeTab(v.path)
	if t == nil {
		return
	}
	a.closeTab(t, func() {
		for _, f := range a.mergeFiles() {
			if !f.resolved {
				a.openConflicts()
				return
			}
		}
	})
}

// conflictsView shows the box of the files in conflict while it is open.
func (a *app) conflictsView(c *ui.Context) {
	cl := &a.conflicts
	if !cl.open {
		return
	}
	p := a.pal
	files := a.mergeFiles()
	if len(files) == 0 {
		// The command ended, or went on: nothing is left to list.
		cl.open = false
		return
	}
	cl.sel = min(max(cl.sel, 0), len(files)-1)
	ui.Overlay(c, func() {
		back := a.dialogBack(c)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(520).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.MergeFilesTitle())
			box.Children(func() {
				resolved, total := a.mergeResolvedCount()
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).
					Label(locales.MergeFilesTitle()).Children(func() {
					ui.Icon(c, iconMerge).FontSize(15).TextColor(p.changed)
					ui.Text(c, locales.MergeFilesTitle()).FontSize(14).FontWeight(600).TextColor(p.text)
					ui.Spacer(c)
					ui.Text(c, locales.MergeFilesToResolve(total-resolved)).FontSize(12).TextColor(p.muted).SingleLine()
				})
				ui.Scroll(c.Key("conflicts-results")).MaxHeight(380).Padding(6).Children(func() {
					for i, f := range files {
						a.mergeFileRow(c, f, i)
					}
				})
				ui.Row(c).Height(26).Padding(0, 10, 2, 8).Gap(6).AlignItems(ui.Center).Background(p.chrome).Children(func() {
					ui.Text(c, locales.MergeFilesResolved(strconv.Itoa(resolved), strconv.Itoa(total))).
						FontSize(11.5).TextColor(p.muted).SingleLine()
					ui.Spacer(c)
					if st := a.git.status; st != nil && st.Op != "" {
						op := st.Op
						if a.mergeButton(c, "conflicts-continue", locales.MergeContinueOp(op.Title()), locales.MergeContinueOpTipFiles(op.Title())) {
							a.post(a.continueOp)
						}
					}
				})
				cl.moved = false
			})
			if back.OverlayShortcut(0, ui.KeyDown) {
				cl.sel, cl.moved = min(cl.sel+1, len(files)-1), true
			}
			if back.OverlayShortcut(0, ui.KeyUp) {
				cl.sel, cl.moved = max(cl.sel-1, 0), true
			}
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeConflicts()
			}
		})
	})
}

// mergeFileRow is a row of the box of the files in conflict: the file, by
// the state its resolution stands in, and the folder it is in.
func (a *app) mergeFileRow(c *ui.Context, f mergeFile, i int) {
	cl := &a.conflicts
	p := a.pal
	name, dir := filepath.Base(f.path), ""
	if r := a.git.repo; r != nil {
		if d := filepath.Dir(filepath.FromSlash(r.Rel(f.path))); d != "." {
			dir = d
		}
	}
	label, tip := locales.MergeOpenMerge(name), locales.MergeOpenMergeTip()
	if f.resolved {
		label, tip = locales.MergeShowResolved(name), locales.MergeShowResolvedTip()
	}
	row := ui.ButtonBase(c.Key("conflict:"+f.path)).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).
		Label(label).Tooltip(tip)
	switch {
	case i == cl.sel:
		row.Background(p.active)
	case row.Hovered():
		row.Background(p.hover)
	}
	// The keyboard follows the choice, for Enter to open it.
	if i == cl.sel && cl.moved {
		row.ScrollIntoView().Focus()
	}
	if row.Clicked() {
		a.post(func() { a.chooseMergeFile(f) })
	}
	row.Children(func() {
		icon, col := iconMerge, p.changed
		if f.resolved {
			icon, col = iconCheck, p.added
		}
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		ui.Text(c, name).FontSize(13).TextColor(p.text).SingleLine().Shrink(0)
		ui.Text(c, dir).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
		if f.resolved {
			if a.mergeButton(c, "conflict-revert:"+f.path, locales.RevertButton(), locales.MergeRestoreConflictTip()) {
				a.post(func() { a.revertMergeConflict(f.path) })
			}
		}
	})
}
