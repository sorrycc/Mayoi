package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	gitpkg "mayoi/internal/git"
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
