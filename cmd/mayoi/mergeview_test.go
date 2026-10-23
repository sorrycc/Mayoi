package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	gitpkg "mayoi/internal/git"
	"mayoi/locales"
)

func TestMergeRegions(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Both sides changed line 3: one conflict, at the side's own lines.
	rs := mergeRegions(base,
		[]string{"l1", "l2", "OURS", "l4", "l5", "l6"},
		[]string{"l1", "l2", "SIDE", "l4", "l5", "l6"})
	if len(rs) != 1 {
		t.Fatalf("got %d regions, want 1", len(rs))
	}
	r := rs[0]
	if r.baseLo != 2 || r.baseHi != 3 {
		t.Errorf("base [%d,%d), want [2,3)", r.baseLo, r.baseHi)
	}
	if r.oursLo != 2 || r.oursHi != 3 {
		t.Errorf("ours [%d,%d), want [2,3)", r.oursLo, r.oursHi)
	}
	if r.theirsLo != 2 || r.theirsHi != 3 {
		t.Errorf("theirs [%d,%d), want [2,3)", r.theirsLo, r.theirsHi)
	}
}

// regionsOf is what the tests read of the regions: their ranges, as
// "base 2:3 ours 2:3 theirs 2:3".
func regionsOf(rs []*mergeRegion) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.name()
	}
	return out
}

func TestMergeRegionsJoin(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Hunks that touch in the base are one conflict, as Git merges them:
	// ours changed line 3, theirs lines 3 and 4. Ours has the base's line
	// 4 as it is, so its side of the region is two lines too.
	rs := mergeRegions(base,
		[]string{"l1", "l2", "OURS", "l4", "l5", "l6"},
		[]string{"l1", "l2", "SIDE", "SIDE4", "l5", "l6"})
	want := []string{"base [2,4) ours [2,4) theirs [2,4)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsApart(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}
	// Changes a line apart are no conflict: only the base both sides
	// changed makes one.
	rs := mergeRegions(base,
		[]string{"l1", "OURS", "l3", "l4", "l5", "l6", "l7", "l8"},
		[]string{"l1", "l2", "l3", "l4", "SIDE", "l6", "l7", "l8"})
	if len(rs) != 0 {
		t.Errorf("got %v, want none", regionsOf(rs))
	}
}

func TestMergeRegionsInsert(t *testing.T) {
	base := []string{"l1", "l2", "l3"}
	// Both sides inserted at the same place: a conflict of empty base.
	rs := mergeRegions(base,
		[]string{"l1", "OURS", "OURS2", "l2", "l3"},
		[]string{"l1", "SIDE", "l2", "l3"})
	want := []string{"base [1,1) ours [1,3) theirs [1,2)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsDelete(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Ours deleted the lines theirs changed: the conflict keeps both, and
	// the side's range is empty where it has no lines, the base's lines it
	// left in theirs' place.
	rs := mergeRegions(base,
		[]string{"l1", "l6"},
		[]string{"l1", "l2", "SIDE", "SIDE4", "l5", "l6"})
	want := []string{"base [1,5) ours [1,1) theirs [1,5)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsTwo(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	// Two conflicts apart: both sides changed b, c and h, i. The changes
	// of ours at b, c moved its h, i down a line.
	rs := mergeRegions(base,
		[]string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"},
		[]string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"})
	want := []string{"base [1,3) ours [1,3) theirs [1,3)", "base [7,9) ours [7,9) theirs [7,9)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// syncOf works the regions of a three-way text out for a result, and
// returns how each stands there: the lines it has of the region, as "-",
// "L", "R" or "B" for a side taken, both, or the base's.
func syncOf(base, ours, theirs, result []string) []string {
	v := &mergeView{baseLines: base, oursLines: ours, theirsLines: theirs}
	v.regions = mergeRegions(base, ours, theirs)
	v.sync(result)
	out := make([]string, len(v.regions))
	for i, r := range v.regions {
		s := "B"
		switch {
		case r.takenL && r.takenR:
			s = "LR"
		case r.takenL:
			s = "L"
		case r.takenR:
			s = "R"
		}
		content := ""
		if r.atLo >= 0 && r.atHi >= r.atLo && r.atHi <= len(result) {
			content = strings.Join(result[r.atLo:r.atHi], "|")
		}
		out[i] = s + " " + ita(r.atLo, r.atHi) + " " + content
	}
	return out
}

func TestSyncRegionsBase(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	got := syncOf(base, ours, theirs, base)
	want := []string{"B [1,3) b|c", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsTaken(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The first region taken ours: it is its lines, the second stays the
	// base's, pushed down by the first's growth.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"L [1,3) O|O2", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Both regions taken theirs.
	got = syncOf(base, ours, theirs, []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"})
	want = []string{"R [1,3) T|T2", "R [7,9) T7|T8"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsEdited(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The region taken ours and then edited by hand: its lines are of no
	// side, and it stands where it did.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2 edited", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"B [1,3) O|O2 edited", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsGrown(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "O3", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The first region taken ours, three lines where two were: the second
	// region is pushed down one.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2", "O3", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"L [1,4) O|O2|O3", "B [8,10) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// mergeRepo makes the harness's folder a work tree where merging side
// into main stops for a conflict of shared.txt, which both branches
// changed at the same lines. The merge itself may fail: that is the
// conflict.
func (hs *harness) mergeRepo(base, ours, theirs string) func(args ...string) string {
	t := hs.t
	git := hs.gitRepo()
	os.WriteFile(hs.path("shared.txt"), []byte(base), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "side")
	os.WriteFile(hs.path("shared.txt"), []byte(theirs), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "theirs")
	git("checkout", "-q", "main")
	os.WriteFile(hs.path("shared.txt"), []byte(ours), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "ours")
	cmd := exec.Command("git", "-C", hs.root, "merge", "side")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge went clean: %s", out)
	}
	hs.settleGit()
	return git
}

// mergeOf opens the merge view of the harness's conflict and returns it,
// settled.
func (hs *harness) mergeOf() *mergeView {
	return hs.mergeOfRel("shared.txt")
}

// mergeOfRel opens the merge view of a file of the harness's conflict and
// returns it, settled.
func (hs *harness) mergeOfRel(rel string) *mergeView {
	t := hs.t
	path := hs.path(rel)
	if e := hs.a.git.entries[path]; !e.Conflicted() {
		t.Fatalf("%s is not in conflict: %+v", rel, e)
	}
	hs.a.openMerge(path, true)
	hs.settle()
	tm := hs.a.activeTab()
	if tm == nil || tm.merge == nil {
		t.Fatalf("no merge tab: %+v", tm)
	}
	hs.awaitMerge()
	return tm.merge
}

// awaitMerge waits for the merge of the active tab to read its stages,
// which it does in the background.
func (hs *harness) awaitMerge() {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		tm := hs.a.activeTab()
		if tm == nil || tm.merge == nil || tm.merge.loaded && !tm.merge.loading {
			break
		}
		hs.settle()
	}
	hs.settle()
}

func TestMergeViewOpens(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	if len(v.regions) != 1 {
		t.Fatalf("got %d regions, want 1", len(v.regions))
	}
	// The result starts as the base, not as the file's conflict markers.
	if v.doc == nil || v.doc.buf.String() != "a\nm\nz\n" {
		t.Fatalf("the result is not the base: %q", v.doc.buf.String())
	}
	// The result starts as the base: taking nothing, the conflicts are
	// all unresolved.
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("the result is not the base: %q", got)
	}
	// The sides are named as the merge makes them, and the result says it
	// starts as the base.
	for _, s := range []string{"OURS · main", "THEIRS · side", "RESULT · FROM BASE", "1 of 1"} {
		if !hs.tt.HasText(s) {
			t.Errorf("no %q: %q", s, hs.tt.Texts())
		}
	}
	if got := v.stateOf(v.regions[0]); got != regionUndecided {
		t.Errorf("state %v, want undecided", got)
	}
	// The command stopped for the conflict does not go on while it stands.
	a := hs.a
	a.continueOp()
	hs.settle()
	if st := a.git.status; st == nil || st.Op != gitpkg.OpMerge {
		t.Errorf("the merge went on with the conflict unresolved: %+v", st)
	}
	if a.fails == 0 {
		t.Error("the app does not say why the merge did not go on")
	}
	// One of the conflicts taken, for the picture of the view at work.
	hs.a.takeMergeSide(v, 0, false)
	hs.settle()
	hs.shot("merge-view")
}

func TestMergeViewAcceptAndUndo(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	// Accepting ours puts its lines in the result, as one step of undo.
	a.takeMergeSide(v, 0, true)
	if got := v.doc.buf.String(); got != "a\nours\nz\n" {
		t.Fatalf("after accepting ours: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Errorf("state %v, want ours", st)
	}
	// Undo brings the base's lines back, and the state with them; redo
	// puts ours in again.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Fatalf("after undo: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("after undo the state is %v, want undecided", st)
	}
	if !v.ed.redo() {
		t.Fatal("no step of redo")
	}
	a.invalidate()
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Errorf("after redo the state is %v, want ours", st)
	}
	// Ignoring ours takes it back out; accepting theirs takes its lines.
	a.takeMergeSide(v, 0, false)
	if st := v.stateOf(v.regions[0]); st != regionRight {
		t.Errorf("state %v, want theirs", st)
	}
	a.dropMergeSide(v, 0, false)
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("after ignoring theirs: %q", got)
	}
}

func TestMergeViewResolveUsing(t *testing.T) {
	hs := newHarness(t, true)
	// Two conflicts: both sides change the first and the last lines, with
	// a line neither changed between.
	hs.mergeRepo("top\nm\nz\nend\n", "OURS\nm\nz\nOURS2\n", "SIDE\nm\nz\nSIDE2\n")
	v := hs.mergeOf()
	if len(v.regions) != 2 {
		t.Fatalf("got %d regions, want 2", len(v.regions))
	}
	hs.a.resolveUsingMerge(v, true)
	if got := v.doc.buf.String(); got != "OURS\nm\nz\nOURS2\n" {
		t.Errorf("resolve using ours: %q", got)
	}
	if v.unresolved() != 0 {
		t.Errorf("%d unresolved, want 0", v.unresolved())
	}
	// The undo of the whole resolution is one step, back to the base.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	hs.a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"top", "m", "z", "end"}) {
		t.Errorf("after undo: %q", got)
	}
	if v.unresolved() != 2 {
		t.Errorf("%d unresolved, want 2", v.unresolved())
	}
}

func TestMergeViewComplete(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	path := hs.path("shared.txt")
	// Resolving with theirs and staging the file ends its conflict.
	a.takeMergeSide(v, 0, false)
	a.completeMerge(v)
	hs.settleGit()
	if e := a.git.entries[path]; e.Conflicted() || e.Letter() != 'M' {
		t.Errorf("after marking resolved: %+v", e)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "a\nside\nz\n" {
		t.Errorf("the file has %q (%v)", b, err)
	}
	if !a.mergeResolved(v) {
		t.Error("the view does not say the file stands resolved")
	}
	// A resolved file has the actions of one: it is not marked again.
	if hs.tt.HasText("Mark Resolved") || !hs.tt.HasText("Restore Conflict…") {
		t.Errorf("no actions of a resolved file: %q", hs.tt.Texts())
	}
	hs.shot("merge-resolved")
	// The merge goes on, its conflicts gone.
	a.continueOp()
	hs.settleGit()
	if st := a.git.status; st == nil || st.Op != "" {
		t.Errorf("the merge does not end: %+v", st)
	}
	if b, err := exec.Command("git", "-C", hs.root, "log", "--oneline", "-1").CombinedOutput(); err == nil {
		t.Log("after continue:", strings.TrimSpace(string(b)))
	}
	_ = git
}

func TestMergeViewExitAndContinue(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	// A result taken as ours and saved: the file has it, the conflict
	// still open.
	a.takeMergeSide(v, 0, true)
	a.saveMerge(v)
	hs.settleGit()
	if b, err := os.ReadFile(hs.path("shared.txt")); err != nil || string(b) != "a\nours\nz\n" {
		t.Fatalf("the result is not saved: %q (%v)", b, err)
	}
	if e := a.git.entries[hs.path("shared.txt")]; !e.Conflicted() {
		t.Fatalf("the conflict is not open: %+v", e)
	}
	// Out of the merge, and back in: the result goes on where it stood.
	a.closeTab(a.activeTab(), nil)
	hs.settle()
	a.openMerge(hs.path("shared.txt"), true)
	hs.settle()
	hs.awaitMerge()
	v2 := a.activeTab().merge
	if v2 == nil || v2.doc == nil {
		t.Fatal("no merge tab back")
	}
	if got := v2.doc.buf.String(); got != "a\nours\nz\n" {
		t.Errorf("the result did not go on: %q", got)
	}
	if st := v2.stateOf(v2.regions[0]); st != regionLeft {
		t.Errorf("the state did not go on: %v", st)
	}
}

func TestMergeViewRestoreConflict(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	// The result started over at the base would save itself a second
	// later, over the markers, when git is slow to settle.
	a.settings.AutoSave = false
	path := hs.path("shared.txt")
	a.takeMergeSide(v, 0, false)
	a.completeMerge(v)
	hs.settleGit()
	if e := a.git.entries[path]; e.Conflicted() {
		t.Fatalf("the file did not stand resolved: %+v", e)
	}
	// Putting it back in conflict brings the stages and the markers, and
	// the view starts the result over at the base.
	a.restoreMerge(v)
	if a.confirm == nil {
		t.Fatal("no confirm")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if e := a.git.entries[path]; !e.Conflicted() {
		t.Fatalf("the file is not in conflict again: %+v", e)
	}
	if b, err := os.ReadFile(path); err != nil || !bytes.Contains(b, []byte("<<<<<<<")) {
		t.Errorf("the markers are not back: %q (%v)", b, err)
	}
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("the result is not the base: %q", got)
	}
	if v.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v.unresolved())
	}
}

func TestMergeViewTypeResult(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	// Typing in the result pane edits the result: the conflict's lines
	// become lines of its own, and the regions follow the text.
	v.panes[1].scroll.Focus()
	hs.settle()
	hs.tt.Type("edit ")
	hs.settle()
	text := v.doc.buf.String()
	if !strings.Contains(text, "edit ") {
		t.Errorf("the typing did not land in the result: %q", text)
	}
	if v.gen != v.doc.buf.Version() {
		t.Error("the regions did not follow the text")
	}
}

// mergeFilesRepo makes the harness's folder a work tree where merging
// side into main stops for the conflicts of one.txt and two.txt, while
// three.txt merges clean, no conflict of its own. ours and theirs changed
// more than the conflicts of one.txt: ours changed its first line alone,
// and theirs its last alone, so the merge of the file has changes that no
// conflict holds.
func (hs *harness) mergeFilesRepo() func(args ...string) string {
	t := hs.t
	git := hs.gitRepo()
	commit := func(files map[string]string) {
		for name, body := range map[string]string{
			"one.txt":   "a\nb\nc\nm\nz\n",
			"two.txt":   "a\nb\nc\nm\nz\n",
			"three.txt": "a\nb\nc\nd\ne\n",
			"four.txt":  "k\nl\np\nq\nr\n",
		} {
			if s, ok := files[name]; ok {
				body = s
			}
			os.WriteFile(hs.path(name), []byte(body), 0o644)
			git("add", name)
		}
		git("commit", "-qm", "change")
	}
	commit(nil)
	git("checkout", "-qb", "side")
	commit(map[string]string{
		"one.txt":   "a\nb\nside\nm\nside2\n",
		"two.txt":   "a\nb\nside\nm\nz\n",
		"three.txt": "a\nb\nc\nD\ne\n",
		"four.txt":  "k\nl\nside\nq\nS\n",
	})
	git("checkout", "-q", "main")
	commit(map[string]string{
		"one.txt":   "ours\nb\nours2\nm\nz\n",
		"two.txt":   "a\nb\nours\nm\nz\n",
		"three.txt": "A\nb\nc\nd\ne\n",
		"four.txt":  "K\nl\nours\nq\nr\n",
	})
	cmd := exec.Command("git", "-C", hs.root, "merge", "side")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge went clean: %s", out)
	}
	hs.settleGit()
	return git
}

func TestMergeApplyNonConflicts(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	v := hs.mergeOfRel("one.txt")
	// ours changed the first line alone, theirs the last alone: applying
	// the changes no conflict holds puts both in the result, and leaves
	// the conflict the base's, for the hand to decide.
	a.applyNonConflicts(v, applyAll)
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "side2"}) {
		t.Fatalf("after applying: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("the conflict stands %v, want undecided", st)
	}
	if v.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v.unresolved())
	}
	// The whole application is one step of undo.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "b", "c", "m", "z"}) {
		t.Errorf("after undo: %q", got)
	}
	// A side of its own: ours' first, then theirs'.
	a.applyNonConflicts(v, applyLeft)
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "z"}) {
		t.Errorf("after applying ours': %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("the conflict stands %v, want undecided", st)
	}
	a.applyNonConflicts(v, applyRight)
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "side2"}) {
		t.Errorf("after applying theirs': %q", got)
	}
	// Nothing of theirs' alone is left: applying again changes nothing,
	// and makes no step of undo.
	steps := len(v.ed.hist.undo)
	a.applyNonConflicts(v, applyAll)
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "side2"}) {
		t.Errorf("after applying again: %q", got)
	}
	if len(v.ed.hist.undo) != steps {
		t.Error("applying what stood made a step of undo")
	}
	// A file whose conflicts hold every change of both sides has nothing
	// to apply.
	v2 := hs.mergeOfRel("two.txt")
	a.applyNonConflicts(v2, applyAll)
	if got := v2.resultLines(); !slices.Equal(got, []string{"a", "b", "c", "m", "z"}) {
		t.Errorf("two.txt changed: %q", got)
	}
	if len(v2.ed.hist.undo) != 0 {
		t.Error("applying nothing made a step of undo")
	}
}

func TestMergeApplyNonConflictSafety(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	v := hs.mergeOfRel("one.txt")
	// A line the hand changed, neither the base's nor a side's, the batch
	// leaves as it is, and takes what it knows around it.
	v.replaceLines(0, 1, []string{"junk"})
	a.applyNonConflicts(v, applyAll)
	if got := v.resultLines(); !slices.Equal(got, []string{"junk", "b", "c", "m", "side2"}) {
		t.Fatalf("after applying over a line of the hand's: %q", got)
	}
	// The conflict keeps standing, however much the batch takes: it takes
	// no line the hand must judge.
	if v.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v.unresolved())
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("the conflict stands %v, want undecided", st)
	}
	// The conflict taken theirs': the batch takes the sides' changes
	// alone, and leaves the conflict's lines as theirs' has them.
	hs = newHarness(t, true)
	hs.mergeFilesRepo()
	v = hs.mergeOfRel("one.txt")
	a.takeMergeSide(v, 0, false)
	a.applyNonConflicts(v, applyAll)
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "side", "m", "side2"}) {
		t.Errorf("after applying over the conflict taken: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionRight {
		t.Errorf("the conflict stands %v, want theirs", st)
	}
}

func TestMergeAutoApply(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	// The setting on, as it is: a conflict seen the first time has the
	// changes that no conflict holds already in the result, and the
	// conflicts still to the hand.
	v := hs.mergeOfRel("one.txt")
	if got := v.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "side2"}) {
		t.Fatalf("the changes of no conflict are not in: %q", got)
	}
	if v.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v.unresolved())
	}
	// Taking it back out is one step of undo, to the base.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	hs.a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "b", "c", "m", "z"}) {
		t.Errorf("after undo: %q", got)
	}
	// The setting off: the result starts as the base.
	hs.a.settings.MergeAutoApply = false
	v2 := hs.mergeOfRel("four.txt")
	if got := v2.resultLines(); !slices.Equal(got, []string{"k", "l", "p", "q", "r"}) {
		t.Errorf("the result is not the base: %q", got)
	}
}

func TestMergeAutoApplyKept(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	v := hs.mergeOfRel("one.txt")
	// The changes applied, taken back out and saved: the file opens again
	// on the result as it was, and takes nothing in again.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	hs.a.saveMerge(v)
	hs.settleGit()
	a := hs.a
	a.closeTab(a.activeTab(), nil)
	hs.settle()
	a.openMerge(hs.path("one.txt"), true)
	hs.settle()
	hs.awaitMerge()
	v2 := a.activeTab().merge
	if v2 == nil || v2.doc == nil {
		t.Fatal("no merge tab back")
	}
	if got := v2.resultLines(); !slices.Equal(got, []string{"a", "b", "c", "m", "z"}) {
		t.Errorf("the saved result did not go on: %q", got)
	}
}

func TestMergeFilesList(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	// The files of the merge: those in conflict, not the one that merged
	// clean.
	files := a.mergeFiles()
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(files), files)
	}
	for i, name := range []string{"four.txt", "one.txt", "two.txt"} {
		if base := filepath.Base(files[i].path); base != name || files[i].resolved {
			t.Errorf("file %d is %s, want %s in conflict", i, base, name)
		}
	}
	// The list shows them, and how many of the files stand.
	a.openConflicts()
	hs.settle()
	for _, text := range []string{"four.txt", "one.txt", "two.txt", "0 of 3 files resolved"} {
		if !hs.tt.HasText(text) {
			t.Errorf("no %q: %q", text, hs.tt.Texts())
		}
	}
	// A file of the list chosen opens its merge.
	a.chooseMergeFile(files[0])
	hs.settle()
	hs.awaitMerge()
	if v := a.activeTab().merge; v == nil || v.path != files[0].path {
		t.Fatalf("the merge of the file chosen is not open: %+v", a.activeTab())
	}
	if a.conflicts.open {
		t.Error("the list is open still")
	}
	// One file resolved: the list has it, and the merge does not go on
	// while the others stand in conflict.
	a.takeMergeSide(a.activeTab().merge, 0, false)
	a.completeMerge(a.activeTab().merge)
	hs.settleGit()
	files = a.mergeFiles()
	if len(files) != 3 || !files[2].resolved || filepath.Base(files[2].path) != "four.txt" {
		t.Fatalf("the file resolved is not last of three: %+v", files)
	}
	for i, name := range []string{"one.txt", "two.txt"} {
		if filepath.Base(files[i].path) != name || files[i].resolved {
			t.Errorf("file %d does not stand in conflict: %+v", i, files[i])
		}
	}
	a.continueOp()
	hs.settleGit()
	if st := a.git.status; st == nil || st.Op != gitpkg.OpMerge {
		t.Errorf("the merge went on with a conflict unresolved: %+v", st)
	}
	if a.fails == 0 {
		t.Error("the app does not say why the merge did not go on")
	}
	// The list says one file stands, of the three.
	a.openConflicts()
	hs.settle()
	if !hs.tt.HasText("1 of 3 files resolved") {
		t.Errorf("no progress: %q", hs.tt.Texts())
	}
	// The last files resolved, the merge goes on.
	a.closeConflicts()
	for _, rel := range []string{"one.txt", "two.txt"} {
		v := hs.mergeOfRel(rel)
		a.takeMergeSide(v, 0, false)
		a.completeMerge(v)
		hs.settleGit()
	}
	a.continueOp()
	hs.settleGit()
	if st := a.git.status; st == nil || st.Op != "" {
		t.Errorf("the merge does not end: %+v", st)
	}
	if files := a.mergeFiles(); len(files) != 0 {
		t.Errorf("the files of the merge stand still: %+v", files)
	}
}

func TestMergeApplyChanges(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	v := hs.mergeOfRel("one.txt")
	// The conflict taken theirs', Apply Changes saves the result, stages
	// the file, and opens the next file in conflict.
	a.takeMergeSide(v, 0, false)
	a.applyMergeChanges(v)
	hs.settleGit()
	hs.awaitMerge()
	if b, err := os.ReadFile(hs.path("one.txt")); err != nil || string(b) != "ours\nb\nside\nm\nside2\n" {
		t.Fatalf("one.txt is not saved: %q (%v)", b, err)
	}
	if e := a.git.entries[hs.path("one.txt")]; e.Conflicted() {
		t.Errorf("one.txt does not stand resolved: %+v", e)
	}
	if v2 := a.activeTab().merge; v2 == nil || v2.path != hs.path("two.txt") {
		t.Fatalf("the next file in conflict is not open: %+v", a.activeTab())
	}
	// Two resolved, the next after it opens in turn.
	a.takeMergeSide(a.activeTab().merge, 0, false)
	a.applyMergeChanges(a.activeTab().merge)
	hs.settleGit()
	hs.awaitMerge()
	if v3 := a.activeTab().merge; v3 == nil || v3.path != hs.path("four.txt") {
		t.Fatalf("the next file in conflict is not open: %+v", a.activeTab())
	}
	// The last file of the merge: Apply Changes stages it, and no file of
	// the merge stands in conflict.
	a.takeMergeSide(a.activeTab().merge, 0, false)
	a.applyMergeChanges(a.activeTab().merge)
	hs.settleGit()
	hs.awaitMerge()
	if e := a.git.entries[hs.path("four.txt")]; e.Conflicted() {
		t.Errorf("four.txt does not stand resolved: %+v", e)
	}
	for _, f := range a.mergeFiles() {
		if !f.resolved {
			t.Errorf("%s does not stand resolved: %+v", filepath.Base(f.path), f)
		}
	}
}

func TestMergeSaveClose(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	v := hs.mergeOfRel("one.txt")
	// The result taken ours', Save and Close saves it in the file, whose
	// conflict stands, and goes back to the files in conflict.
	a.takeMergeSide(v, 0, true)
	a.saveCloseMerge(v)
	hs.settleGit()
	if b, err := os.ReadFile(hs.path("one.txt")); err != nil || string(b) != "ours\nb\nours2\nm\nside2\n" {
		t.Fatalf("one.txt is not saved: %q (%v)", b, err)
	}
	if e := a.git.entries[hs.path("one.txt")]; !e.Conflicted() {
		t.Errorf("one.txt does not stand in conflict: %+v", e)
	}
	if t2 := a.activeTab(); t2 != nil && t2.merge != nil && t2.merge.path == v.path {
		t.Error("the merge tab is open still")
	}
	if !a.conflicts.open {
		t.Error("the files in conflict do not show")
	}
	if !hs.tt.HasText("0 of 3 files resolved") {
		t.Errorf("no progress: %q", hs.tt.Texts())
	}
	// Back in, the result goes on where it stood.
	a.closeConflicts()
	a.openMerge(v.path, true)
	hs.settle()
	hs.awaitMerge()
	v2 := a.activeTab().merge
	if v2 == nil || v2.path != v.path {
		t.Fatal("the merge did not open again")
	}
	if st := v2.stateOf(v2.regions[0]); st != regionLeft {
		t.Errorf("the result did not go on: %v", st)
	}
}

func TestMergeReopenResolved(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	// one.txt resolved, its merge closed: the list has it resolved, with
	// its resolution to take back.
	v := hs.mergeOfRel("one.txt")
	a.takeMergeSide(v, 0, false)
	a.completeMerge(v)
	hs.settleGit()
	a.saveCloseMerge(v)
	hs.settle()
	if !a.conflicts.open {
		t.Fatal("the list does not show")
	}
	var resolved mergeFile
	for _, f := range a.mergeFiles() {
		if f.resolved {
			resolved = f
		}
	}
	if resolved.path == "" || filepath.Base(resolved.path) != "one.txt" {
		t.Fatalf("one.txt does not stand resolved: %+v", a.mergeFiles())
	}
	// Its resolution taken back, the file stands in conflict again, and
	// its merge opens on the result at the base, with the changes that no
	// conflict holds in.
	a.revertMergeConflict(resolved.path)
	if a.confirm == nil {
		t.Fatal("no confirm")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if e := a.git.entries[resolved.path]; !e.Conflicted() {
		t.Fatalf("one.txt does not stand in conflict again: %+v", e)
	}
	a.openMerge(resolved.path, true)
	hs.settle()
	hs.awaitMerge()
	v2 := a.activeTab().merge
	if v2 == nil || v2.path != resolved.path {
		t.Fatalf("the merge did not open again: %+v", a.activeTab())
	}
	if got := v2.resultLines(); !slices.Equal(got, []string{"ours", "b", "c", "m", "side2"}) {
		t.Errorf("the result is not the base with the changes of no conflict: %q", got)
	}
	if v2.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v2.unresolved())
	}
}

func TestMergeViewFilesBar(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	v := hs.mergeOfRel("one.txt")
	// The head of the merge says how many of the files stand resolved,
	// and has the actions of the file and the list of them.
	for _, text := range []string{"0 of 3 files resolved", "Apply Changes", "Save and Close", "Conflicts…"} {
		if !hs.tt.HasText(text) {
			t.Errorf("no %q: %q", text, hs.tt.Texts())
		}
	}
	// The files in conflict show, and a row of them opens its file's
	// merge.
	if err := hs.tt.Click("Conflicts…"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.conflicts.open {
		t.Error("the list does not show")
	}
	if err := hs.tt.Click("Open the merge of two.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// The merge of two.txt reads its sides in the background; its head,
	// with Apply Changes, shows once they are in.
	hs.awaitMerge()
	if v2 := a.activeTab().merge; v2 == nil || v2.path != hs.path("two.txt") {
		t.Fatalf("the merge of the file chosen is not open: %+v", a.activeTab())
	}
	if a.conflicts.open {
		t.Error("the list is open still")
	}
	// Apply Changes saves the result, stages the file and opens the next
	// file in conflict, round the end of the list back up.
	a.takeMergeSide(a.activeTab().merge, 0, false)
	if err := hs.tt.Click("Apply Changes"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if e := a.git.entries[hs.path("two.txt")]; e.Conflicted() {
		t.Errorf("two.txt does not stand resolved: %+v", e)
	}
	if v3 := a.activeTab().merge; v3 == nil || v3.path != hs.path("four.txt") {
		t.Errorf("the next file in conflict is not open: %+v", a.activeTab())
	}
	_ = v
}

// TestMergeViewTypesAsTheResultEditor: the typing of the result is the
// merge editor's, also when the file was open already and its tab editor
// held the document's typing before.
func TestMergeViewTypesAsTheResultEditor(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	// The file is open as its own tab, whose editor the document's
	// typing is credited to.
	hs.a.openFile(hs.path("shared.txt"), false)
	hs.settle()
	opened := hs.a.docs[hs.a.docIndex(hs.path("shared.txt"))].typist
	if opened == nil {
		t.Fatal("the file's tab editor does not hold the typing")
	}
	v := hs.mergeOf()
	if v.doc.typist != opened {
		t.Fatalf("the merge took the typing over before a key: %p", v.doc.typist)
	}
	// The result pane takes the keyboard, and the typing goes in.
	v.ed.focusNext = true
	hs.settle()
	hs.tt.Type("X")
	hs.settle()
	if v.doc.typist != v.ed {
		t.Fatal("the typing is not the merge editor's")
	}
	if !strings.Contains(v.doc.buf.String(), "X") {
		t.Fatalf("the typing did not land in the result: %q", v.doc.buf.String())
	}
}

// TestMergeViewIgnoreIsAHandledState: ignoring a conflict keeps the
// base's lines on purpose, which counts as handled — the unresolved
// count drops, and the undo and redo of the ignoring bring the state
// back with the text.
func TestMergeViewIgnoreIsAHandledState(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	if v.unresolved() != 1 {
		t.Fatalf("unresolved %d, want 1", v.unresolved())
	}
	// Accept ours, then ignore it: the base's lines stand, and the
	// conflict counts as handled.
	a.takeMergeSide(v, 0, true)
	a.dropMergeSide(v, 0, true)
	if st := v.stateOf(v.regions[0]); st != regionIgnored {
		t.Fatalf("state %v, want ignored", st)
	}
	if v.unresolved() != 0 {
		t.Fatalf("unresolved %d after ignoring, want 0", v.unresolved())
	}
	// Ignoring the other side too, from the base, keeps it handled.
	a.dropMergeSide(v, 0, false)
	if st := v.stateOf(v.regions[0]); st != regionIgnored {
		t.Fatalf("state %v after ignoring the other side, want ignored", st)
	}
	if v.unresolved() != 0 {
		t.Fatalf("unresolved %d after ignoring both, want 0", v.unresolved())
	}
	// The ignore of the other side took no text with it: the undo of it
	// leaves the region ignored still.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	a.invalidate()
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionIgnored {
		t.Fatalf("after undoing the empty ignore the state is %v, want ignored", st)
	}
	// Undo takes the ignore of ours back with the text: ours' lines
	// stand again, and the conflict is unresolved once more.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	a.invalidate()
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Fatalf("after undo the state is %v, want ours", st)
	}
	// Ours taken counts as handled, as accepting it did.
	if v.unresolved() != 0 {
		t.Fatalf("unresolved %d after undo, want 0", v.unresolved())
	}
	// Redo ignores ours again.
	if !v.ed.redo() {
		t.Fatal("no step of redo")
	}
	a.invalidate()
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionIgnored {
		t.Fatalf("after redo the state is %v, want ignored", st)
	}
}

// TestMergeResolvedFilesSurviveRestart: the files a merge view staged as
// resolved stand in the list of them after the app has started anew, and
// the record of them goes when the command is done.
func TestMergeResolvedFilesSurviveRestart(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	// four.txt merges clean of conflicts: accepting a side and completing
	// it stages the file as resolved.
	a.openMerge(hs.path("four.txt"), true)
	hs.settle()
	hs.awaitMerge()
	a.takeMergeSide(a.activeTab().merge, 0, false)
	a.completeMerge(a.activeTab().merge)
	hs.settle()
	hs.settleGit()
	files := a.mergeFiles()
	if len(files) != 3 || !files[2].resolved {
		t.Fatalf("four.txt is not the resolved one: %+v", files)
	}
	// The app starts anew: the memory of the resolved files is gone, and
	// the list takes it from the record in the repository.
	a.git.mergeDone, a.git.mergeDoneOp = nil, ""
	files = a.mergeFiles()
	if len(files) != 3 || !files[2].resolved || filepath.Base(files[2].path) != "four.txt" {
		t.Fatalf("after the restart four.txt is not resolved: %+v", files)
	}
	// The command done, the record of the resolved files goes with it,
	// and a fresh start has none of them resolved.
	if a.git.status != nil {
		a.git.status.Op = ""
	}
	dir := a.git.repo.Dir
	a.mergeFiles()
	if _, err := os.Stat(filepath.Join(dir, "mayoi-resolved.json")); !os.IsNotExist(err) {
		t.Fatal("the record of the resolved files is still there")
	}
	a.git.mergeDone, a.git.mergeDoneOp = nil, ""
	files = a.mergeFiles()
	for _, f := range files {
		if f.resolved {
			t.Fatalf("a file stands resolved without a command: %+v", files)
		}
	}
}

func TestMergeMarkResolvedAsks(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	path := hs.path("shared.txt")
	// A conflict left as the base: marking the file resolved asks first,
	// and stages nothing until it is told to.
	a.completeMerge(v)
	hs.settleGit()
	if a.confirm == nil {
		t.Fatal("marking a file with a conflict unresolved does not ask")
	}
	if e := a.git.entries[path]; !e.Conflicted() {
		t.Fatalf("the file was staged before the answer: %+v", e)
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if e := a.git.entries[path]; e.Conflicted() {
		t.Errorf("the file does not stand resolved once told to: %+v", e)
	}
}

func TestMergeFilesKeys(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeFilesRepo()
	a := hs.a
	files := a.mergeFiles()
	a.openConflicts()
	hs.settle()
	// The files stand in the repository's root: no folder is shown, least
	// of all the absolute one.
	for _, s := range hs.tt.Texts() {
		if strings.Contains(s, hs.root) && !strings.HasPrefix(s, "Reveal ") {
			t.Errorf("the list shows an absolute folder: %q", s)
		}
	}
	// The arrows move the choice, Enter opens its merge.
	hs.tt.Key(0, ui.KeyDown)
	hs.tt.Key(0, ui.KeyDown)
	hs.tt.Key(0, ui.KeyUp)
	hs.tt.Key(0, ui.KeyEnter)
	hs.settleGit()
	hs.settle()
	if tm := a.activeTab(); tm == nil || tm.merge == nil || tm.merge.path != files[1].path {
		t.Fatalf("Enter does not open the merge of the file chosen: %+v sel=%d", tm.merge.path, a.conflicts.sel)
	}
	if a.conflicts.open {
		t.Error("the list is open still")
	}
}

// The texts of TestMergeSyncScrollFollows: two sides of a base that
// moved and added functions, so the mapping between the sides is not
// one to one, and theirs ends short of where ours scrolls to.
const (
	syncScrollBase = `package calc

import "errors"

// Add returns the sum of a and b, the simplest case.
func Add(a, b int) int {
	return a + b
}

// Subtract returns a minus b.
func Subtract(a, b int) int {
	return a - b
}

// Multiply returns the product of a and b.
func Multiply(a, b int) int {
	return a * b
}

// Divide returns a over b, or an error when b is zero.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

// Sum adds every number in xs.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller of a and b.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Clamp keeps v within low and high.
func Clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
`
	syncScrollOurs = `package calc

import "errors"

// Add sums a and b, as ours wrote it.
func Add(a, b int) int {
	return a + b
}

// Subtract returns a minus b.
func Subtract(a, b int) int {
	return a - b
}

// Multiply returns the product of a and b.
func Multiply(a, b int) int {
	return a * b
}

// Divide returns a over b, or an error when b is zero.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("ours: zero divisor")
	}
	return a / b, nil
}

// Clamp keeps v within low and high.
func Clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// Sum adds every number in xs.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller of a and b.
func Min(a, b int) int {
	if a < b {
		return small(a, b)
	}
	return b
}

// Clamp keeps v within low and high.
func Clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// small is the lesser of a and b, shared by Min.
func small(a, b int) int {
	if a < b {
		return a
	}
	return b
}
`
	syncScrollTheirs = `package calc

import "errors"

// Add sums a and b, as theirs wrote it.
func Add(a, b int) int {
	return a + b
}

// Subtract returns a minus b.
func Subtract(a, b int) int {
	return a - b
}

// Multiply returns the product of a and b.
func Multiply(a, b int) int {
	return a * b
}

// Divide returns a over b, or an error when b is zero.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("theirs: zero divisor")
	}
	return a / b, nil
}

// Sum adds every number in xs.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller of a and b.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

`
)

// TestMergeSyncScrollFollows: scrolling one pane of a merge moves the
// others with it, so that the line in the middle of the mover stands
// across from theirs as the hunks between the texts say — and the result
// pane follows too, which it did not while its selection sync kept
// revealing a caret it believed unseen, pinning it to the top and, with
// the panes together, dragging the sides back there with it.
func TestMergeSyncScrollFollows(t *testing.T) {
	base, ours, theirs := syncScrollBase, syncScrollOurs, syncScrollTheirs
	hs := newHarness(t, true)
	hs.mergeRepo(base, ours, theirs)
	v := hs.mergeOf()
	a := hs.a
	lineH := a.font.lineH
	mid := func(k int) int {
		return int((v.panes[k].scroll.Y + v.panes[k].viewH/2 - editorPadY) / lineH)
	}
	// Scroll ours deep, where the texts have diverged and the mapping is
	// not one to one: the result lands on the line ours maps to, theirs
	// on the line that maps back to it, and none stands pinned at the
	// top.
	v.panes[0].scroll.Y = 45 * lineH
	hs.settle()
	if mid(0) < 40 {
		t.Fatalf("ours stands at line %d, the scroll did not take", mid(0))
	}
	if v.panes[1].scroll.Y <= 0 {
		t.Error("the result pane stands pinned at the top while ours scrolled")
	}
	// A follower stands where the mapping puts it, clamped to its own
	// ends: a side shorter than the position mapped to ends together
	// with the rest rather than overshooting into blank.
	clampedMid := func(k int, at float32) int {
		h := v.panes[k].viewH
		end := max(0, 2*editorPadY+float32(v.paneLineCount(k))*lineH-h)
		y := clamp(editorPadY+at*lineH-h/2, 0, end)
		return int((y + h/2 - editorPadY) / lineH)
	}
	res := mapMergeLine(v.pairHunks(0), float32(mid(0)), false)
	if d := float32(mid(1)) - res; d < -1 || d > 1 {
		t.Errorf("the result's middle line %d does not stand across ours' %d (want %.0f)", mid(1), mid(0), res)
	}
	at2 := mapMergeLine(v.pairHunks(2), res, true)
	want := clampedMid(2, at2)
	if d := float32(mid(2) - want); d < -1 || d > 1 {
		t.Errorf("theirs' middle line %d does not stand across the result's %.0f (want %d, clamped of %.0f)", mid(2), res, want, at2)
	}
	// Scrolling the result leads the sides the same way.
	v.panes[1].scroll.Y = 30 * lineH
	hs.settle()
	if v.panes[0].scroll.Y == 45*lineH {
		t.Error("ours did not follow the result")
	}
	at0 := mapMergeLine(v.pairHunks(0), float32(mid(1)), true)
	want0 := clampedMid(0, at0)
	if d := float32(mid(0) - want0); d < -1 || d > 1 {
		t.Errorf("ours' middle line %d does not stand across the result's %d (want %d, clamped of %.0f)", mid(0), mid(1), want0, at0)
	}
	at2 = mapMergeLine(v.pairHunks(2), float32(mid(1)), true)
	want2 := clampedMid(2, at2)
	if d := float32(mid(2) - want2); d < -1 || d > 1 {
		t.Errorf("theirs' middle line %d does not stand across the result's %d (want %d, clamped of %.0f)", mid(2), mid(1), want2, at2)
	}
}

// mergeOverflowSetup makes a two-file merge and opens one.txt's merge,
// its first conflict taken so the result differs from the base.
func (hs *harness) mergeOverflowSetup(t *testing.T) *mergeView {
	hs.mergeFilesRepo()
	v := hs.mergeOfRel("one.txt")
	hs.a.takeMergeSide(v, 0, true)
	hs.settle()
	if got := v.resultLines(); slices.Equal(got, []string{"a", "b", "c", "m", "z"}) || v.unresolved() >= len(v.regions) {
		t.Fatalf("the conflict did not go in: %q unresolved=%d", got, v.unresolved())
	}
	return v
}

// mergeOverflowPress presses the merge header's action where it stands:
// on the bar while it fits, from the overflow menu once it went there.
func (hs *harness) mergeOverflowPress(t *testing.T, label string) {
	if _, on := hs.tt.Find(label); on {
		if err := hs.tt.Click(label); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := hs.tt.Click(locales.MergeMoreActions()); err != nil {
		t.Fatalf("%q is neither on the bar nor has the menu a place to open: %v", label, err)
	}
	if !slices.Contains(hs.tt.Menu(), label) {
		t.Fatalf("%q is neither on the bar nor in the menu: %q", label, hs.tt.Menu())
	}
	if err := hs.tt.ChooseMenuItem(label); err != nil {
		t.Fatal(err)
	}
}

// TestMergeHeadDropsLeastNeededFirst: as the window narrows, the merge
// header's file actions go one by one, the least needed first, each only
// once the row would not hold it, and those left fit. Whatever went
// stands in the overflow menu, and nothing stays on both. The bar of the
// conflict chosen goes as its rows do, into its own overflow.
func TestMergeHeadDropsLeastNeededFirst(t *testing.T) {
	skipOnWindows(t, "Windows' wider font holds the actions at 540")
	hs := newHarness(t, true)
	hs.mergeOverflowSetup(t)
	// In the order they go: the head's file actions, then the bar of the
	// conflict.
	parts := []string{"Conflicts…", "Save and Close", "Apply Changes", "Save", "Reset to Base", "Mark Resolved"}
	sideParts := []string{"Resolve using Right", "Resolve using Left", "Ignore Right", "Ignore Left", "Accept Right", "Accept Left"}
	gone, sideGone := 0, 0
	for w := 1200; w >= 540; w -= 20 {
		hs.tt.SetSize(w, 640)
		hs.settle()
		for i, s := range parts {
			box, shown := hs.tt.Find(s)
			switch {
			case !shown:
				gone = max(gone, i+1)
			case i < gone:
				t.Errorf("at %d, %q is back", w, s)
			case box.X+box.W > float32(w):
				t.Errorf("at %d, %q does not fit: %.0f+%.0f", w, s, box.X, box.W)
			}
		}
		for i, s := range sideParts {
			box, shown := hs.tt.Find(s)
			switch {
			case !shown:
				sideGone = max(sideGone, i+1)
			case i < sideGone:
				t.Errorf("at %d, %q is back", w, s)
			case box.X+box.W > float32(w):
				t.Errorf("at %d, %q does not fit: %.0f+%.0f", w, s, box.X, box.W)
			}
		}
		if box, ok := hs.tt.Find(locales.MergeApply()); !ok || box.X+box.W > float32(w) {
			t.Errorf("at %d, Apply does not fit: %v", w, box)
		}
	}
	if gone == 0 {
		t.Fatal("the actions never go, though the row ran out of room")
	}
	if sideGone == 0 {
		t.Fatal("the sides of the conflict never go, though their row ran out of room")
	}
	// What went stands in the overflow menu, in the row's order, and
	// nothing stands in both places. The menu of the head holds no action
	// of the conflict's bar: each row keeps its own.
	if err := hs.tt.Click(locales.MergeMoreActions()); err != nil {
		t.Fatal(err)
	}
	menu := hs.tt.Menu()
	if len(menu) == 0 {
		t.Fatal("the overflow menu holds nothing")
	}
	last := -1
	for _, s := range menu {
		i := slices.Index(parts, s)
		if i < 0 {
			if s != locales.MergeApply() && !slices.Contains(sideParts, s) {
				t.Errorf("the menu holds %q, which no action of the row is", s)
			}
			if slices.Contains(sideParts, s) {
				t.Errorf("the head's menu holds %q, which the conflict's bar went without", s)
			}
			continue
		}
		if _, on := hs.tt.Find(s); on {
			t.Errorf("%q stands on the bar and in the menu", s)
		}
		if i < last {
			t.Errorf("%q stands out of the row's order in the menu: %q", s, menu)
		}
		last = i
	}
	// The conflict's bar keeps its own overflow: it holds what went from
	// it, and the head's menu held none of it.
	box, ok := hs.tt.Find(locales.MergePrevConflict())
	if !ok {
		t.Fatal("the bar of the conflict is gone altogether")
	}
	hs.tt.ClickAt(float32(540)-25, box.Y+box.H/2)
	sideMenu := hs.tt.Menu()
	if len(sideMenu) == 0 {
		t.Fatal("the conflict bar's overflow holds nothing")
	}
	sideLast := -1
	for _, s := range sideMenu {
		i := slices.Index(sideParts, s)
		if i < 0 {
			t.Errorf("the conflict bar's menu holds %q, which no action of it is", s)
			continue
		}
		if _, on := hs.tt.Find(s); on {
			t.Errorf("%q stands on the bar and in the menu", s)
		}
		if i < sideLast {
			t.Errorf("%q stands out of the row's order in the menu: %q", s, sideMenu)
		}
		sideLast = i
	}
	if slices.Contains(sideMenu, locales.MergeResetBase()) {
		t.Error("the conflict bar's menu holds an action of the head")
	}
}

// TestMergeHeadOverflowMenuActs: an action of the overflow menu does what
// the button it went from does, on the bar or in the menu as the width
// has it.
func TestMergeHeadOverflowMenuActs(t *testing.T) {
	hs := newHarness(t, true)
	v := hs.mergeOverflowSetup(t)
	// The row cannot hold every action at this width.
	hs.tt.SetSize(760, 640)
	hs.settle()
	if _, ok := hs.tt.Find(locales.MergeMoreActions()); !ok {
		t.Fatal("no overflow menu at 760, though the actions cannot fit")
	}
	// Reset to Base puts the base back, wherever it stands.
	hs.mergeOverflowPress(t, locales.MergeResetBase())
	hs.settle()
	if got := v.doc.buf.String(); got != "a\nb\nc\nm\nz\n" {
		t.Fatalf("the result is not the base: %q", got)
	}
	// Conflicts… opens the files in conflict.
	hs.mergeOverflowPress(t, locales.MergeConflictsEllipsis())
	hs.settle()
	if !hs.a.conflicts.open {
		t.Error("the files in conflict do not open")
	}
	hs.a.closeConflicts()
	hs.settle()
	// Save and Close saves the file and ends the merge.
	hs.mergeOverflowPress(t, locales.MergeSaveClose())
	hs.settle()
	body, err := os.ReadFile(hs.path("one.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "a\nb\nc\nm\nz\n" {
		t.Errorf("the file does not hold the result: %q", body)
	}
	if tm := hs.a.activeTab(); tm != nil && tm.merge != nil {
		t.Error("the merge does not close")
	}
}

// TestMergeActionsOverflowMenuActs: an action that went from the bar of
// the conflict stands in its overflow, and does what its button does.
func TestMergeActionsOverflowMenuActs(t *testing.T) {
	hs := newHarness(t, true)
	v := hs.mergeOverflowSetup(t)
	// The bar of the conflict cannot hold its actions at this width.
	hs.tt.SetSize(760, 640)
	hs.settle()
	if _, ok := hs.tt.Find(locales.MergeUseRight()); ok {
		t.Fatal("Resolve using Right still stands on the bar at 760")
	}
	box, ok := hs.tt.Find(locales.MergePrevConflict())
	if !ok {
		t.Fatal("the bar of the conflict is gone altogether")
	}
	hs.tt.ClickAt(760-25, box.Y+box.H/2)
	if !slices.Contains(hs.tt.Menu(), locales.MergeUseRight()) {
		t.Fatalf("the conflict bar's overflow lacks Resolve using Right: %q", hs.tt.Menu())
	}
	if err := hs.tt.ChooseMenuItem(locales.MergeUseRight()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if v.unresolved() != 0 {
		t.Errorf("Resolve using Right from the overflow leaves %d conflicts", v.unresolved())
	}
	for _, r := range v.regions {
		if st := v.stateOf(r); st != regionRight {
			t.Errorf("a conflict stands %v, not theirs", st)
		}
	}
}

// TestMergePaneWrapsLines: a merge's panes wrap their lines at their
// width as Word Wrap says, ⌥Z unwraps the merge alone, and a change of
// the setting takes the tab's choice away again.
func TestMergePaneWrapsLines(t *testing.T) {
	hs := newHarness(t, true)
	long := strings.Repeat("a long merge line ", 20)
	hs.mergeRepo("a\n"+long+"\nz\n", "ours\n"+long+"\nz\n", "theirs\n"+long+"\nz\n")
	v := hs.mergeOf()
	a := hs.a
	lineH := a.font.lineH
	// Without wrapping, a line stands one lineH apart.
	if top := a.mergeGeometry(v, 0).top(2); top != 2*lineH {
		t.Fatalf("line 2 of ours stands %v from the top, want %v", top, 2*lineH)
	}
	// Word Wrap on: the long line of every pane wraps, and the line after
	// it stands lower than the lines apart of no wrapping — but not to
	// ribbons either, as a pane that hugged its content would, wrapping
	// every line again and again in a collapsed column.
	a.toggleWordWrap()
	hs.settle()
	for k := range v.panes {
		top := a.mergeGeometry(v, k).top(2)
		if top <= 2*lineH {
			t.Errorf("pane %d does not wrap: line 2 stands %v from the top", k, top)
		}
		if top >= 20*lineH {
			t.Errorf("pane %d wraps to ribbons: line 2 stands %v from the top", k, top)
		}
	}
	// ⌥Z unwraps the merge alone: the setting stays on.
	a.toggleTabWrap()
	hs.settle()
	if top := a.mergeGeometry(v, 0).top(2); top != 2*lineH {
		t.Fatalf("after ⌥Z line 2 stands %v, want %v", top, 2*lineH)
	}
	if !a.settings.WordWrap {
		t.Error("⌥Z turned the Word Wrap setting off")
	}
	// A change of the setting takes the tab's choice away: the panes
	// wrap again.
	a.toggleWordWrap()
	a.toggleWordWrap()
	hs.settle()
	if top := a.mergeGeometry(v, 0).top(2); top <= 2*lineH {
		t.Fatalf("the tab's choice outlived the setting: line 2 stands %v", top)
	}
}

// TestMergePaneWrapChoosesRegion: a click on a row of a wrapped line
// finds the conflict the line belongs to, not the line its row sits at.
func TestMergePaneWrapChoosesRegion(t *testing.T) {
	hs := newHarness(t, true)
	long := strings.Repeat("a long merge line ", 20)
	hs.mergeRepo(long+"\n"+long+"\nz\n", long+"\n"+long+"\nours\n", long+"\n"+long+"\ntheirs\n")
	v := hs.mergeOf()
	a := hs.a
	a.toggleWordWrap()
	hs.settle()
	// The conflict holds line 2; the rows its wrapped lines above take
	// push it far past where one line a row would have it. A click on its
	// first row still finds it. chooseMergeLine takes the pointer's place
	// in the pane: the scroll and the padding come off inside it.
	v.chosen = -1
	y := a.mergeGeometry(v, 0).top(2) + a.font.lineH/2 - v.panes[0].scroll.Y + editorPadY
	a.chooseMergeLine(v, 0, y)
	if v.chosen != 0 {
		t.Fatalf("a click on line 2 chose region %d, want 0", v.chosen)
	}
}

// TestMergePaneWrapHoldsNoEstimates: as a merge's panes wrap, every line
// of them is laid out for its height, so that none stands on an estimate
// — a pane is narrow and wraps deep, and a scroll into estimated
// country, as revealing a far conflict goes, shows a blank where the
// heights say text is. All measured, the height of the lines is the rows
// they wrap to.
func TestMergePaneWrapHoldsNoEstimates(t *testing.T) {
	hs := newHarness(t, true)
	var base, ours, theirs []string
	for i := 0; i < 80; i++ {
		switch i % 4 {
		case 0:
			base = append(base, fmt.Sprintf("// comment %02d — a fairly long line of commentary text that wraps in a narrow merge pane", i))
		case 1:
			base = append(base, fmt.Sprintf("func f%02d() int {", i))
		case 2:
			base = append(base, "	return 1")
		default:
			base = append(base, "}")
		}
	}
	ours, theirs = append([]string{}, base...), append([]string{}, base...)
	ours[61] = "// comment 61 changed by ours — a fairly long line of it"
	theirs[61] = "// comment 61 changed by theirs — a fairly long line"
	hs.mergeRepo(strings.Join(base, "\n")+"\n", strings.Join(ours, "\n")+"\n", strings.Join(theirs, "\n")+"\n")
	v := hs.mergeOf()
	a := hs.a
	a.toggleWordWrap()
	hs.settle()
	a.stepMergeRegion(v, 1)
	hs.settle()
	lineH := a.font.lineH
	for k := 0; k < 3; k++ {
		g := a.mergeGeometry(v, k)
		rows := float32(0)
		for i := 0; i < v.paneLineCount(k); i++ {
			rows += float32(len(v.panes[k].lines.Rows(i)))
		}
		if h := g.top(v.paneLineCount(k)); h < rows*lineH-lineH/8 || h > rows*lineH+lineH/8 {
			t.Errorf("pane %d stands on estimated heights: %v high, its rows %v", k, h, rows*lineH)
		}
	}
}

// TestMergeSyncScrollFollowsWrapped: with the panes wrapping, scrolling
// one pane of a merge still moves the others with it, so that the line in
// the middle of the mover stands across from theirs — by the rows the
// lines wrap to, which put the mapped lines far from where one row a line
// would stand them.
func TestMergeSyncScrollFollowsWrapped(t *testing.T) {
	hs := newHarness(t, true)
	head := []string{"package calc", "", "// one", "// two", "// three", ""}
	longs := make([]string, 36)
	for i := range longs {
		longs[i] = fmt.Sprintf("// wrapped note %02d — a fairly long line of commentary text that keeps going well past the width of a merge pane, wrapping deep", i)
	}
	base := strings.Join(slices.Concat(head,
		[]string{"func Conflict() int {", "\treturn 1", "\treturn 2", "\treturn 3", "}"}, longs), "\n")
	ours := strings.Join(slices.Concat(head,
		[]string{"func Conflict() int {", "\treturn 12 // ours took its own line", "}"}, longs), "\n")
	theirs := strings.Join(slices.Concat(head,
		[]string{"func Conflict() int {", "\treturn 13", "\treturn 14", "\treturn 15", "\treturn 16", "\treturn 17", "}"}, longs), "\n")
	hs.mergeRepo(base+"\n", ours+"\n", theirs+"\n")
	v := hs.mergeOf()
	a := hs.a
	a.toggleWordWrap()
	hs.settle()
	midAt := func(k int) float32 {
		return a.mergeGeometry(v, k).atF(v.panes[k].scroll.Y + v.panes[k].viewH/2 - editorPadY)
	}
	// Scroll ours so the middle of its view stands in the long stretch,
	// well past the conflict and well before the end, where the follower
	// takes the anchor unhindered: ours' line there maps two lines on in
	// the result and four on in theirs.
	g := a.mergeGeometry(v, 0)
	v.panes[0].scroll.Y = editorPadY + g.top(20) - v.panes[0].viewH/2
	hs.settle()
	if at := midAt(0); at < 19 || at > 21 {
		t.Fatalf("ours stands at line %v, the scroll did not take", at)
	}
	res := mapMergeLine(v.pairHunks(0), midAt(0), false)
	if d := midAt(1) - res; d < -0.1 || d > 0.1 {
		t.Errorf("the result's middle line %.2f does not stand across ours' %.2f (want %.2f)", midAt(1), midAt(0), res)
	}
	at2 := mapMergeLine(v.pairHunks(2), res, true)
	if d := midAt(2) - at2; d < -0.1 || d > 0.1 {
		t.Errorf("theirs' middle line %.2f does not stand across the result's %.2f (want %.2f)", midAt(2), res, at2)
	}
	// Scrolling the result leads the sides the same way.
	g1 := a.mergeGeometry(v, 1)
	v.panes[1].scroll.Y = editorPadY + g1.top(24) - v.panes[1].viewH/2
	hs.settle()
	if v.panes[0].scroll.Y == editorPadY+g.top(20)-v.panes[0].viewH/2 {
		t.Error("ours did not follow the result")
	}
	res = midAt(1)
	at0 := mapMergeLine(v.pairHunks(0), res, true)
	if d := midAt(0) - at0; d < -0.1 || d > 0.1 {
		t.Errorf("ours' middle line %.2f does not stand across the result's %.2f (want %.2f)", midAt(0), res, at0)
	}
	at2 = mapMergeLine(v.pairHunks(2), res, true)
	if d := midAt(2) - at2; d < -0.1 || d > 0.1 {
		t.Errorf("theirs' middle line %.2f does not stand across the result's %.2f (want %.2f)", midAt(2), res, at2)
	}
	// At the very top the mapped line goes negative: the followers rest
	// at the top too, their first line across the leader's, not one pad
	// below it.
	v.panes[0].scroll.Y = 0
	hs.settle()
	if y := v.panes[1].scroll.Y; y != 0 {
		t.Errorf("the result rests at %v with ours at the top, want 0", y)
	}
	if y := v.panes[2].scroll.Y; y != 0 {
		t.Errorf("theirs rests at %v with ours at the top, want 0", y)
	}
}

// TestMergeGutterButtonChoosesWrapped: with the panes wrapping, the
// button of a conflict whose lines the rows above have pushed deep still
// stands with its band, and taking its side takes that conflict's, not
// the first's, whose rows stand above the view.
func TestMergeGutterButtonChoosesWrapped(t *testing.T) {
	hs := newHarness(t, true)
	longs := make([]string, 10)
	for i := range longs {
		longs[i] = fmt.Sprintf("// wrapped note %02d — a fairly long line of commentary that keeps going well past the width of a merge pane", i)
	}
	join := func(ss ...[]string) string {
		var all []string
		for _, s := range ss {
			all = append(all, s...)
		}
		return strings.Join(all, "\n") + "\n"
	}
	one := []string{"func conflictOne() int {", "\treturn 1", "}"}
	oneOurs := []string{"func conflictOne() int {", "\treturn 11", "}"}
	oneTheirs := []string{"func conflictOne() int {", "\treturn 12", "}"}
	two := []string{"func conflictTwo() int {", "\treturn 2", "}"}
	twoOurs := []string{"func conflictTwo() int {", "\treturn 21", "}"}
	twoTheirs := []string{"func conflictTwo() int {", "\treturn 22", "}"}
	hs.mergeRepo(join([]string{"package calc", ""}, one, longs, two),
		join([]string{"package calc", ""}, oneOurs, longs, twoOurs),
		join([]string{"package calc", ""}, oneTheirs, longs, twoTheirs))
	v := hs.mergeOf()
	a := hs.a
	a.toggleWordWrap()
	// The head's bar of the conflict chosen holds an Accept Left of its
	// own: none chosen, so the gutter's is the one the click finds.
	v.chosen, v.current = -1, -1
	hs.settle()
	// Ours scrolled so the second conflict's band stands in the middle of
	// the view; the first's stands above it, culled.
	g := a.mergeGeometry(v, 0)
	lo, hi := v.regionLines(v.regions[1], 0)
	mid := (g.top(lo) + g.top(hi)) / 2
	v.panes[0].scroll.Y = max(0, editorPadY+mid-v.panes[0].viewH/2)
	hs.settle()
	for i, r := range v.regions {
		if st := v.stateOf(r); st != regionUndecided {
			t.Fatalf("conflict %d stands %v before the click", i, st)
		}
	}
	if err := hs.tt.Click("Accept Left"); err != nil {
		t.Fatalf("the second conflict's button: %v", err)
	}
	hs.settle()
	if st := v.stateOf(v.regions[1]); st != regionLeft {
		t.Errorf("the second conflict stands %v, not ours' side", st)
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("the first conflict moved too: %v", st)
	}
	if got := v.resultLines(); !slices.Contains(got, "\treturn 21") {
		t.Errorf("the result lacks ours' second conflict: %q", got)
	}
}

// TestMergeGutterButtonSurvivesTallConflict: a wrapped conflict taller
// than the pane keeps its buttons while any part of it shows, even once
// its middle has scrolled past — the buttons pin to the pane's edge
// instead of vanishing with the middle.
func TestMergeGutterButtonSurvivesTallConflict(t *testing.T) {
	hs := newHarness(t, true)
	base := make([]string, 24)
	ours := make([]string, 24)
	theirs := make([]string, 24)
	for i := range base {
		base[i] = fmt.Sprintf("// base note %02d", i)
		ours[i] = fmt.Sprintf("// ours note %02d — a fairly long line of commentary that keeps going well past the width of a merge pane, wrapping deep", i)
		theirs[i] = fmt.Sprintf("// theirs note %02d — a fairly long line of commentary that keeps going well past the width of a merge pane, wrapping deep", i)
	}
	join := func(ss []string) string { return "package calc\n\n" + strings.Join(ss, "\n") + "\n" }
	hs.mergeRepo(join(base), join(ours), join(theirs))
	v := hs.mergeOf()
	a := hs.a
	a.toggleWordWrap()
	// The head's bar of the conflict chosen holds an Accept Left of its
	// own: none chosen, so the gutter's is the one the click finds.
	v.chosen, v.current = -1, -1
	hs.settle()
	if n := len(v.regions); n != 1 {
		t.Fatalf("%d conflicts, want 1", n)
	}
	// Scroll ours so the conflict's top stands at the top of the pane:
	// its middle, tens of wrapped rows down, lies past the pane's
	// bottom, and the buttons used to be culled away with it.
	lo, _ := v.regionLines(v.regions[0], 0)
	g := a.mergeGeometry(v, 0)
	v.panes[0].scroll.Y = max(0, editorPadY+g.top(lo))
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Fatalf("the conflict stands %v before the click", st)
	}
	if err := hs.tt.Click("Accept Left"); err != nil {
		t.Fatalf("the tall conflict's button: %v", err)
	}
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Errorf("the conflict stands %v, not ours' side", st)
	}
}

// TestMergeResultScrollsSideways: a mostly sideways swipe on the result
// pane scrolls the pane, as the side panes do — the editor the pane
// embeds must not take the swipe for its own sideways scroll, which no
// scroller of the merge tracks.
func TestMergeResultScrollsSideways(t *testing.T) {
	hs := newHarness(t, true)
	long := strings.Repeat("a long merge line ", 20)
	hs.mergeRepo("a\n"+long+"\nz\n", "ours\n"+long+"\nz\n", "theirs\n"+long+"\nz\n")
	v := hs.mergeOf()
	r, ok := hs.tt.Find(v.resultLabel())
	if !ok {
		t.Fatal("no result header")
	}
	hs.tt.Scroll(r.X+r.W/2, r.Y+r.H+120, 90, 0)
	hs.settle()
	if v.panes[1].hscroll.X <= 0 {
		t.Errorf("the result pane stands at %v after a sideways swipe", v.panes[1].hscroll.X)
	}
}

// TestMergePaneWrapLaysOutViewFirst: a change of the width lays the
// lines in view out first and sweeps outward a band a frame, so dragging
// the window does not reshape every far line of a giant file a frame,
// and what is in view stands measured at once.
func TestMergePaneWrapLaysOutViewFirst(t *testing.T) {
	hs := newHarness(t, true)
	body := make([]string, 3000)
	for i := range body {
		switch i % 4 {
		case 0:
			body[i] = fmt.Sprintf("// note %04d — a fairly long line of commentary text that keeps going well past the width of a merge pane, wrapping deep", i)
		case 1:
			body[i] = fmt.Sprintf("func f%04d() int {", i)
		case 2:
			body[i] = "\treturn 1"
		default:
			body[i] = "}"
		}
	}
	mk := func(tag string) string {
		return "package calc\n\n" + strings.Replace(strings.Join(body, "\n"), "// note 0000", "// "+tag+" 0000", 1) + "\n"
	}
	hs.a.settings.MergeAutoApply = false
	hs.mergeRepo(mk("base"), mk("ours"), mk("theirs"))
	v := hs.mergeOf()
	a := hs.a
	// Anchor the view at the top of the file: the sweep starts from the
	// view as the last frame drew it.
	v.panes[0].scroll.Y = 0
	hs.settle()
	a.toggleWordWrap()
	hs.settle()
	p := &v.panes[0]
	n := v.paneLineCount(0)
	if p.laidTo < n {
		t.Fatalf("after settling, only %d of %d lines stand measured", p.laidTo, n)
	}
	// The width changes: the frame of the change lays out the view and
	// leaves the far lines for the frames after.
	ver := v.paneBuf(0).Version()
	a.layOutMergeLines(v, 0, ver, p.laidW-40)
	if p.laidFrom != 0 {
		t.Errorf("the sweep does not cover the top of the view: from %d", p.laidFrom)
	}
	if p.laidTo >= n {
		t.Fatalf("the frame of the width change laid out all %d lines at once", n)
	}
	// The frames after sweep outward, a band a frame, until all stand
	// measured.
	frames := 0
	for p.laidFrom > 0 || p.laidTo < n {
		to0, from0 := p.laidTo, p.laidFrom
		a.layOutMergeLines(v, 0, ver, p.laidW)
		if p.laidTo == to0 && p.laidFrom == from0 {
			t.Fatal("the sweep stands still")
		}
		frames++
		if frames > 10 {
			t.Fatalf("the sweep needs more than %d frames of %d lines", frames, n)
		}
	}
	if p.laidFrom != 0 || p.laidTo < n {
		t.Fatalf("after the sweep, %d..%d of %d lines stand measured", p.laidFrom, p.laidTo, n)
	}
}

// TestSwipeSideways: a swipe mostly sideways moves the scroll state it
// is given, clamped at its ends; a mostly vertical one, and a mouse's
// Shift+wheel, are left for the caller.
func TestSwipeSideways(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	s := &ui.ScrollState{MaxX: 100}
	if !a.swipeSideways(s, ui.InputEvent{Kind: ui.InputScroll, DX: 30, DY: 2}) {
		t.Fatal("a sideways swipe was not taken")
	}
	if s.X != 30 {
		t.Fatalf("the swipe stood the scroll at %v, want 30", s.X)
	}
	if a.swipeSideways(s, ui.InputEvent{Kind: ui.InputScroll, DX: 2, DY: 30}) {
		t.Error("a mostly vertical swipe was taken")
	}
	if a.swipeSideways(s, ui.InputEvent{Kind: ui.InputScroll, DX: 30, DY: 2, Mods: ui.Shift}) {
		t.Error("a Shift+wheel was taken")
	}
	if a.swipeSideways(s, ui.InputEvent{Kind: ui.InputPointerDown, DX: 30, DY: 2}) {
		t.Error("a pointer event was taken")
	}
	a.swipeSideways(s, ui.InputEvent{Kind: ui.InputScroll, DX: 500})
	if s.X != 100 {
		t.Fatalf("the swipe ran past the end: %v, want 100", s.X)
	}
	a.swipeSideways(s, ui.InputEvent{Kind: ui.InputScroll, DX: -500})
	if s.X != 0 {
		t.Fatalf("the swipe ran before the start: %v, want 0", s.X)
	}
}
