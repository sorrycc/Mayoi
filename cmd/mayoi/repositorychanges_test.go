package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"mayoi/locales"
)

func TestRepositoryHeaderSeparatesSelectionAndExpansion(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	draft := "keep this module draft"
	sub.git.message = draft
	before := parent("rev-parse", "HEAD")
	if err := hs.tt.Click(locales.ScmSelectRepository(sub.label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.repositories.selected != sub.git.repo.Root || sub.changesExpanded() {
		t.Fatal("selecting a clean repository must only select its history")
	}
	hs.shot("submodules-selected-header")
	if err := hs.tt.Click(locales.ScmToggleRepository(sub.label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !sub.changesExpanded() || !hs.tt.HasText(locales.ScmCommitMessage()) || sub.git.message != draft || !hs.tt.HasText(locales.DiffNoChanges()) {
		t.Fatalf("expanded clean repository lost its draft or clean state: %q", hs.tt.Texts())
	}
	// An expanded clean repository has no changes to commit.
	hs.tt.Click(locales.CmdGitCommit())
	hs.settle()
	if parent("rev-parse", "HEAD") != before || sub.git.message != draft {
		t.Fatal("clean commit button changed a repository or its draft")
	}
	if err := hs.tt.Click(locales.ScmToggleRepository(sub.label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if sub.changesExpanded() || a.repositories.selected != sub.git.repo.Root || sub.git.message != draft {
		t.Fatal("collapse changed the selected repository or its draft")
	}
	// A user's collapse choice survives subsequent Git status updates.
	if err := os.WriteFile(filepath.Join(sub.git.repo.Root, "module.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	if sub.changesExpanded() {
		t.Fatal("status refresh reopened a manually collapsed repository")
	}
}

func TestRepositorySubmoduleStates(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	root := sub.git.repo.Root
	file := filepath.Join(root, "module.txt")
	if err := os.WriteFile(file, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	if !hs.tt.HasText(locales.ScmSubmoduleContentLabel()) || hs.tt.HasText(locales.ScmSubmoduleCommitLabel()) {
		t.Fatal("parent must distinguish dirty content from a changed reference")
	}
	gitIn(t, root, "add", "module.txt")
	gitIn(t, root, "commit", "-qm", "change module")
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	if !hs.tt.HasText(locales.ScmSubmoduleCommitLabel()) || hs.tt.HasText(locales.ScmSubmoduleContentLabel()) {
		t.Fatal("parent did not show the new submodule commit reference")
	}
	if err := os.WriteFile(file, []byte("more changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.a.gitChanged()
	settleRepositories(t, hs, 2)
	want := locales.ScmSubmoduleCommitLabel() + " · " + locales.ScmSubmoduleContentLabel()
	if !hs.tt.HasText(want) {
		t.Fatalf("parent did not show both kinds of submodule change: %q", hs.tt.Texts())
	}
}

func TestRepositoryChangeCursorSurvivesStatusInsertion(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	hs.tt.SetSize(1200, 1000)
	a.window.ScmChangesRatio = 0.8
	root := sub.git.repo.Root
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	if err := hs.tt.ClickWith(ui.Shift, "c.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// A filesystem update inserts a row before the keyboard cursor. Space
	// must still stage the selected file, rather than the file at its old index.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	hs.key(0, ui.KeySpace)
	settleRepositories(t, hs, 2)
	staged := sub.groupEntries()[groupStaged]
	if len(staged) != 1 || filepath.Base(staged[0].Path) != "c.txt" || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
		t.Fatalf("status insertion redirected the keyboard action: %+v", staged)
	}
}

func TestRepositoryChangeCursorFollowsDiff(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	hs.tt.SetSize(1200, 1000)
	a.window.ScmChangesRatio = 0.8
	root := sub.git.repo.Root
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	if err := hs.tt.ClickWith(ui.Shift, "b.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	// The diff's file navigation also moves the keyboard cursor in source
	// control, without changing which repository owns the history.
	a.selectRepository(a.git.repo.Root)
	a.chooseScmFile(&diffFile{path: filepath.Join(root, "c.txt"), repoRoot: root, base: diffIndex})
	hs.settle()
	hs.key(0, ui.KeySpace)
	settleRepositories(t, hs, 2)
	staged := sub.groupEntries()[groupStaged]
	if len(staged) != 1 || filepath.Base(staged[0].Path) != "c.txt" || a.repositories.selected != a.git.repo.Root {
		t.Fatalf("diff navigation lost the module cursor or changed history: %+v", staged)
	}
}

func TestRepositoryChangesResizeAndRemember(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a, tt := hs.a, hs.tt
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(sub.git.repo.Root, fmt.Sprintf("new-%02d.txt", i)), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	tt.SetSize(1200, 1000)
	hs.settle()
	history, ok := tt.Find("COMMITS")
	if !ok {
		t.Fatal("missing history")
	}
	height := float32(1000)
	if history.Y < height*0.4 || history.Y > height*0.65 {
		t.Fatalf("initial split is not halfway: %v in %v", history, height)
	}
	handle, ok := tt.Find(locales.ScmResizeChanges())
	if !ok {
		t.Fatal("missing changes/history resize handle")
	}
	x, y := handle.X+handle.W/2, handle.Y+handle.H/2
	tt.Press(x, y)
	tt.Move(x, y+60)
	tt.Release(x, y+60)
	hs.settle()
	resized, ok := tt.Find("COMMITS")
	if !ok || resized.Y < history.Y+50 || a.window.ScmChangesRatio <= 0.5 {
		t.Fatalf("splitter did not resize changes: %v -> %v, ratio %v", history, resized, a.window.ScmChangesRatio)
	}
	if a.session().ScmChangesRatio != a.window.ScmChangesRatio {
		t.Fatal("split position not included in the saved session")
	}
	for i := range 40 {
		if err := os.Remove(filepath.Join(sub.git.repo.Root, fmt.Sprintf("new-%02d.txt", i))); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	a.showExplorer()
	hs.settle()
	a.showSourceControl()
	hs.settle()
	reopened, _ := tt.Find("COMMITS")
	if reopened.Y != resized.Y {
		t.Fatalf("split position lost when reopening source control: %v -> %v", resized, reopened)
	}
	hs.shot("submodules-resized")
}

func TestRepositoryHeaderUsesActualWidth(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	hs.a.window.SidebarWidth = 420
	width := float32(240)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(width).Children(func() { sub.repositoryHeader(c) })
	}, 600, 100)
	for range 3 {
		step(tt)
	}
	if tt.HasText("packages") || tt.HasText(locales.CmdGitFetch()) {
		t.Fatal("narrow header used the saved sidebar width to show extra controls")
	}
	width = 420
	for range 3 {
		step(tt)
	}
	if !tt.HasText("packages") || !tt.HasText(locales.CmdGitFetch()) {
		t.Fatal("wide header did not restore its path and remote controls")
	}
}

func TestRepositoryChangesFitContent(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a, tt := hs.a, hs.tt
	tt.SetSize(1200, 1000)
	hs.settle()
	history, ok := tt.Find("COMMITS")
	if !ok || history.Y > 200 {
		t.Fatalf("clean repositories left empty space above history: %v (%v)", history, ok)
	}
	if err := tt.Click(locales.ScmToggleRepository(sub.label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	input, ok := tt.Find(locales.ScmCommitMessage())
	expanded, _ := tt.Find("COMMITS")
	if !ok || expanded.Y <= input.Y+input.H || expanded.Y >= 450 {
		t.Fatalf("expanded draft did not size the changes pane: input=%v, history=%v", input, expanded)
	}
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(sub.git.repo.Root, fmt.Sprintf("new-%02d.txt", i)), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	full, _ := tt.Find("COMMITS")
	if full.Y < 450 || full.Y > 600 || a.window.ScmChangesRatio != 0 {
		t.Fatalf("automatic pane did not stop at half without saving a ratio: %v, ratio %v", full, a.window.ScmChangesRatio)
	}
	if err := tt.Click(locales.ScmToggleRepository(sub.label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	collapsed, _ := tt.Find("COMMITS")
	if collapsed.Y >= full.Y-50 || a.window.ScmChangesRatio != 0 {
		t.Fatalf("automatic pane did not shrink after collapse: %v -> %v", full, collapsed)
	}
}

func TestRepositoryKeysFromHeader(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(sub.git.repo.Root, name), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	// Focus the shared list without clicking a file. Its cursor starts on
	// the module's header, independently of which history is selected.
	for i, row := range a.repositoryChangeRows(a.rootRepositoryList()) {
		if row.kind == repositoryHeader && row.repo.git == sub.git {
			a.repositories.row = i
			break
		}
	}
	a.repositories.changes.Handle.Focus()
	hs.settle()
	hs.key(ui.Shift, ui.KeyDown)
	if sub.changesUI.selection.Len() != 1 {
		t.Fatalf("Shift+Down from the module header chose %d files", sub.changesUI.selection.Len())
	}
	// Cmd+A must also work directly from a non-file row, and retain a
	// file cursor for the next keyboard action.
	for i, row := range a.repositoryChangeRows(a.rootRepositoryList()) {
		if row.kind == repositoryCommit && row.repo.git == sub.git {
			a.repositories.row = i
			break
		}
	}
	hs.settle()
	hs.key(ui.Cmd, ui.KeyA)
	hs.key(0, ui.KeySpace)
	settleRepositories(t, hs, 2)
	if len(sub.groupEntries()[groupStaged]) != 2 || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
		t.Fatal("keyboard actions from a header did not remain in its repository")
	}
}

func TestRepositoryStageButtonKeepsCursor(t *testing.T) {
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	a.window.ScmChangesRatio = 0.8
	for _, name := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(sub.git.repo.Root, name), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	box, ok := hs.tt.Find("c.txt")
	if !ok {
		t.Fatal("missing module file")
	}
	hs.tt.Move(box.X+box.W/2, box.Y+box.H/2)
	hs.settle()
	if err := hs.tt.Click(locales.StageChanges()); err != nil {
		t.Fatal(err)
	}
	settleRepositories(t, hs, 2)
	staged := sub.groupEntries()[groupStaged]
	if len(staged) != 1 || filepath.Base(staged[0].Path) != "c.txt" || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
		t.Fatalf("stage button did not stage only c.txt in its repository: %+v", staged)
	}
	rows := a.repositoryChangeRows(a.rootRepositoryList())
	if i := a.repositories.row; i >= 0 && i < len(rows) {
		r := rows[i]
		if r.kind != repositoryEntry {
			t.Fatal("staging c.txt left its old index on a non-file row")
		}
		if row := r.repo.scmRows()[r.at]; !row.file() || filepath.Base(row.e.Path) != "c.txt" {
			t.Fatalf("staging c.txt moved the cursor to %+v", row)
		}
	}
}

func TestRepositoryChangesScrollContinuouslyAndPinHeaders(t *testing.T) {
	hs, sub, parent := submoduleHarness(t)
	a := hs.a
	hs.tt.SetSize(1200, 1000)
	a.window.SidebarWidth = 320
	for _, root := range []string{a.git.repo.Root, sub.git.repo.Root} {
		for i := range 120 {
			if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%03d.txt", i)), []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	// One wheel gesture moves the parent input out of view and leaves its
	// repository header pinned, without entering a nested file-list scroller.
	hs.tt.Scroll(150, 240, 0, 500)
	hs.settle()
	pinned, ok := hs.tt.Find(locales.ScmSelectRepository(a.rootRepository().label()))
	if !ok || pinned.Y > 110 {
		t.Fatalf("parent repository header did not stick: %v (%v)", pinned, ok)
	}
	// Keep scrolling over files: the next repository must become reachable
	// through the same wheel target, even with over a hundred parent changes.
	found := false
	for range 8 {
		hs.tt.Scroll(150, 240, 0, 400)
		hs.settle()
		b, ok := hs.tt.Find(locales.ScmSelectRepository(sub.label()))
		if ok && b.Y < 180 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("continuous scrolling never reached the module")
	}
	// Move past the module's commit box to its long file list. Its header
	// stays pinned, replacing the parent's at the same top edge.
	hs.tt.Scroll(150, 240, 0, 250)
	hs.settle()
	moduleHeader, ok := hs.tt.Find(locales.ScmSelectRepository(sub.label()))
	if !ok || moduleHeader.Y > 110 {
		t.Fatalf("module repository header did not stick: %v (%v)", moduleHeader, ok)
	}
	hs.shot("submodules-continuous-scroll")
	// Select a visible module range with the keyboard and stage it while
	// parent history is pinned. Neither the selection nor staging crosses roots.
	a.selectRepository(a.git.repo.Root)
	first, last := a.repositories.changes.Visible()
	rows := a.repositoryChangeRows(a.rootRepositoryList())
	for i := first; i <= last; i++ {
		r := rows[i]
		if r.kind != repositoryEntry || r.repo.git != sub.git {
			continue
		}
		e := r.repo.scmRows()[r.at]
		if !e.file() || !strings.HasPrefix(filepath.Base(e.e.Path), "file-") {
			continue
		}
		name := filepath.Base(e.e.Path)
		bounds, ok := hs.tt.Find(name)
		history, _ := hs.tt.Find("COMMITS")
		if !ok || bounds.Y < moduleHeader.Y+moduleHeader.H+22 || bounds.Y+bounds.H > history.Y-12 {
			continue
		}
		if err := hs.tt.ClickWith(ui.Shift, name); err != nil {
			t.Fatal(err)
		}
		hs.settle()
		hs.key(ui.Shift, ui.KeyDown)
		hs.key(0, ui.KeySpace)
		settleRepositories(t, hs, 2)
		if len(sub.groupEntries()[groupStaged]) != 2 || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
			t.Fatalf("shared list range action: module staged=%d, parent staged=%d, module selection=%d, cursor=%d, shared cursor=%d", len(sub.groupEntries()[groupStaged]), len(a.rootRepository().groupEntries()[groupStaged]), sub.changesUI.selection.Len(), sub.changesUI.row, a.repositories.row)
		}
		if strings.TrimSpace(parent("diff", "--cached", "--name-only")) != "" {
			t.Fatal("module range action staged files in the parent")
		}
		return
	}
	t.Fatal("no visible module file to exercise")
}

func repositoryGroupScrollFixture(t *testing.T) (*harness, *gitContext) {
	t.Helper()
	hs, sub, _ := submoduleHarness(t)
	a := hs.a
	hs.tt.SetSize(1200, 1000)
	a.window.SidebarWidth = 340
	a.window.ScmChangesRatio = 0.7
	root := sub.git.repo.Root
	for _, prefix := range []string{"staged", "working"} {
		for i := range 40 {
			if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%s-%02d.txt", prefix, i)), []byte("base\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-qm", "group fixture")
	for _, prefix := range []string{"staged", "working", "untracked"} {
		for i := range 40 {
			if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%s-%02d.txt", prefix, i)), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gitIn(t, root, "add", "--", ":(glob)staged-*.txt")
	a.gitChanged()
	settleRepositories(t, hs, 2)
	if err := hs.tt.Click(locales.ScmToggleRepository(a.rootRepository().label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	return hs, sub
}

func expectPinnedRepositoryGroup(t *testing.T, hs *harness, sub *gitContext, group scmGroup) ui.Rect {
	t.Helper()
	header, ok := hs.tt.Find(locales.ScmSelectRepository(sub.label()))
	if !ok {
		t.Fatal("missing repository header")
	}
	name := sub.label() + " · " + groupName(group)
	groupBox, ok := hs.tt.Find(name)
	if !ok || groupBox.Y < header.Y+header.H || groupBox.Y > header.Y+header.H+12 {
		t.Fatalf("%s not pinned beneath the repository: header=%v, group=%v (%v)", name, header, groupBox, ok)
	}
	return groupBox
}

func TestRepositoryChangeGroupsStayPinned(t *testing.T) {
	hs, sub := repositoryGroupScrollFixture(t)
	hs.tt.Scroll(150, 240, 0, 350)
	hs.settle()
	expectPinnedRepositoryGroup(t, hs, sub, groupStaged)
	hs.shot("submodules-pinned-staged")
	hs.tt.Scroll(150, 240, 0, 800)
	hs.settle()
	expectPinnedRepositoryGroup(t, hs, sub, groupChanges)
	hs.shot("submodules-pinned-changes")
	// Collapsing the floating header must toggle its own repository once.
	if err := hs.tt.Click(sub.label() + " · " + groupName(groupChanges)); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !sub.changesUI.collapsed[groupChanges] || hs.a.rootRepository().scm.collapsed[groupChanges] {
		t.Fatal("floating header did not collapse only the module's group")
	}
}

func TestRepositoryPinnedGroupActionsKeepTheirRepository(t *testing.T) {
	hs, sub := repositoryGroupScrollFixture(t)
	a := hs.a
	a.selectRepository(a.git.repo.Root)
	hs.tt.Scroll(150, 240, 0, 1150)
	hs.settle()
	group := expectPinnedRepositoryGroup(t, hs, sub, groupChanges)
	hs.tt.Move(220, group.Y+group.H/2)
	hs.settle()
	if err := hs.tt.Click(locales.CmdGitStageAll()); err != nil {
		t.Fatal(err)
	}
	settleRepositories(t, hs, 2)
	groups := sub.groupEntries()
	if len(groups[groupStaged]) != 80 || len(groups[groupChanges]) != 0 || len(groups[groupUntracked]) != 40 || len(a.rootRepository().groupEntries()[groupStaged]) != 0 {
		t.Fatalf("floating Changes action crossed its group or repository: staged=%d working=%d untracked=%d", len(groups[groupStaged]), len(groups[groupChanges]), len(groups[groupUntracked]))
	}
}

func TestRepositoryPinnedGroupLeavesKeyboardChoiceVisible(t *testing.T) {
	hs, sub := repositoryGroupScrollFixture(t)
	hs.tt.Scroll(150, 240, 0, 1150)
	hs.settle()
	expectPinnedRepositoryGroup(t, hs, sub, groupChanges)
	if err := hs.tt.ClickWith(ui.Shift, "working-15.txt"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	for range 13 {
		hs.key(0, ui.KeyUp)
	}
	group := expectPinnedRepositoryGroup(t, hs, sub, groupChanges)
	file, ok := hs.tt.Find("working-02.txt")
	if !ok || file.Y < group.Y+group.H {
		t.Fatalf("keyboard choice hidden behind the floating group: file=%v (%v), group=%v", file, ok, group)
	}
}

func TestRepositoryPinnedGroupStopsAtNextRepository(t *testing.T) {
	hs, sub := repositoryGroupScrollFixture(t)
	a := hs.a
	for i := range 80 {
		if err := os.WriteFile(hs.path(fmt.Sprintf("parent-%02d.txt", i)), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.gitChanged()
	settleRepositories(t, hs, 2)
	if err := hs.tt.Click(locales.ScmToggleRepository(a.rootRepository().label())); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Scroll(150, 240, 0, 500)
	hs.settle()
	expectPinnedRepositoryGroup(t, hs, a.rootRepository(), groupUntracked)
	// The module's commit input sits between repository and group headers.
	// The parent's floating group must leave it clear during the handoff.
	hs.tt.Scroll(150, 240, 0, 1500)
	hs.settle()
	header, ok := hs.tt.Find(locales.ScmSelectRepository(sub.label()))
	if !ok || header.Y > 110 {
		t.Fatalf("next repository did not reach the top: %v (%v)", header, ok)
	}
	if _, ok := hs.tt.Find(a.rootRepository().label() + " · " + groupName(groupUntracked)); ok {
		t.Fatal("previous repository's floating group covered the next repository")
	}
	input, ok := hs.tt.Find(locales.ScmCommitMessage())
	if !ok || input.Y+input.H <= header.Y+header.H {
		t.Fatalf("next repository's commit input is obscured: %v (%v)", input, ok)
	}
	hs.shot("submodules-pinned-handoff")
}
