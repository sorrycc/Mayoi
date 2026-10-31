package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/settings"
)

func scmSelectionFixture(t *testing.T, tree bool) *harness {
	t.Helper()
	hs := newHarness(t, true)
	hs.gitRepo()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := os.WriteFile(hs.path(name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hs.a.settings.ScmTree = tree
	hs.a.run("view.scm")
	hs.settleGit()
	return hs
}

func expectScmSelection(t *testing.T, hs *harness, names ...string) {
	t.Helper()
	var got, want []string
	for _, r := range hs.a.scmRows() {
		if r.file() && hs.a.scm.selection.Has(r.key) {
			got = append(got, r.e.Path)
		}
	}
	for _, name := range names {
		want = append(want, hs.path(name))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("selected %v, want %v", got, want)
	}
}

func TestScmRangeSelectionAndSpace(t *testing.T) {
	for _, tree := range []bool{false, true} {
		name := "list"
		if tree {
			name = "tree"
		}
		t.Run(name, func(t *testing.T) {
			hs := scmSelectionFixture(t, tree)
			if err := hs.tt.Click("d.txt"); err != nil {
				t.Fatal(err)
			}
			hs.settle()
			hs.tt.ClickWith(ui.Shift, "b.txt")
			hs.settle()
			expectScmSelection(t, hs, "b.txt", "c.txt", "d.txt")
			// Moving back towards the anchor shrinks the range.
			hs.key(ui.Shift, ui.KeyDown)
			expectScmSelection(t, hs, "c.txt", "d.txt")
			hs.key(ui.Shift, ui.KeyUp)
			expectScmSelection(t, hs, "b.txt", "c.txt", "d.txt")
			hs.shot("scm-range-" + name)
			hs.key(0, ui.KeySpace)
			hs.settleGit()
			if g := hs.a.groupEntries(); len(g[groupStaged]) != 3 || len(g[groupUntracked]) != 1 || g[groupUntracked][0].Path != hs.path("a.txt") {
				t.Fatalf("stage range: %v", g)
			}
			// Refreshed rows cannot retain keys for files that moved groups.
			if hs.a.scm.selection.Len() != 0 {
				t.Fatalf("stale selection: %v", slices.Collect(hs.a.scm.selection.All()))
			}
			hs.tt.Click("b.txt")
			hs.tt.ClickWith(ui.Shift, "d.txt")
			hs.settle()
			expectScmSelection(t, hs, "b.txt", "c.txt", "d.txt")
			hs.key(0, ui.KeySpace)
			hs.settleGit()
			if g := hs.a.groupEntries(); len(g[groupStaged]) != 0 || len(g[groupUntracked]) != 4 {
				t.Fatalf("unstage range: %v", g)
			}
		})
	}
}

func TestScmSelectionButtonsAndMenu(t *testing.T) {
	hs := scmSelectionFixture(t, false)
	hs.tt.Click("d.txt")
	hs.tt.ClickWith(ui.Shift, "b.txt")
	hs.settle()
	// Hovering an unselected row exposes its action without changing the range.
	b, ok := hs.tt.Find("a.txt")
	if !ok {
		t.Fatal("missing a.txt")
	}
	hs.tt.Move(b.X+b.W/2, b.Y+b.H/2)
	hs.settle()
	if err := hs.tt.Click("Stage Changes"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if g := hs.a.groupEntries(); len(g[groupStaged]) != 1 || g[groupStaged][0].Path != hs.path("a.txt") {
		t.Fatalf("unselected row staged other files: %v", g)
	}
	expectScmSelection(t, hs, "b.txt", "c.txt", "d.txt")
	// A selected row's menu stages the whole selected range.
	hs.tt.RightClick("c.txt")
	hs.settle()
	if err := hs.tt.Click("Stage Changes"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if g := hs.a.groupEntries(); len(g[groupStaged]) != 4 {
		t.Fatalf("context menu did not stage the selection: %v", g)
	}
	hs.tt.Click("d.txt")
	hs.tt.ClickWith(ui.Shift, "b.txt")
	hs.settle()
	b, _ = hs.tt.Find("c.txt")
	hs.tt.Move(b.X+b.W/2, b.Y+b.H/2)
	hs.settle()
	if err := hs.tt.Click("Unstage Changes"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if g := hs.a.groupEntries(); len(g[groupStaged]) != 1 || g[groupStaged][0].Path != hs.path("a.txt") {
		t.Fatalf("button did not unstage the selection: %v", g)
	}
}

func TestScmToggleSelectionAndKeyboardNavigation(t *testing.T) {
	hs := scmSelectionFixture(t, false)
	hs.tt.Click("b.txt")
	hs.tt.ClickWith(ui.Cmd, "d.txt")
	hs.settle()
	expectScmSelection(t, hs, "b.txt", "d.txt")
	hs.tt.ClickWith(ui.Cmd, "d.txt")
	hs.settle()
	expectScmSelection(t, hs, "b.txt")
	// A plain click gives the diff the keyboard; a Shift-click keeps it in
	// the list.
	hs.tt.Click("b.txt")
	hs.tt.ClickWith(ui.Shift, "b.txt")
	expectScmSelection(t, hs, "b.txt")
	hs.key(0, ui.KeyDown)
	expectScmSelection(t, hs, "c.txt")
	hs.key(ui.Shift, ui.KeyDown)
	expectScmSelection(t, hs, "c.txt", "d.txt")
	hs.key(ui.Shift, ui.KeyHome)
	expectScmSelection(t, hs, "a.txt", "b.txt", "c.txt")
	hs.command("selectAll")
	expectScmSelection(t, hs, "a.txt", "b.txt", "c.txt", "d.txt")
}

func TestScmSelectionGroupsAndCollapsedFolders(t *testing.T) {
	root := t.TempDir()
	a := &app{settings: settings.Default()}
	a.settings.ScmTree = true
	a.git.repo = &git.Repo{Root: root}
	a.git.status = &git.Status{Entries: []git.Entry{
		{Path: filepath.Join(root, "dir", "both.txt"), X: 'M', Y: 'M'},
		{Path: filepath.Join(root, "dir", "new.txt"), X: '?', Y: '?'},
	}}
	var staged, changed scmRow
	for _, r := range a.scmRows() {
		if r.file() {
			a.scm.selection.Add(r.key)
			if r.group == groupStaged {
				staged = r
			} else if r.group == groupChanges {
				changed = r
			}
		}
	}
	if es := a.scmActionEntries(staged); len(es) != 1 {
		t.Fatalf("unstage includes working tree entries: %v", es)
	}
	if es := a.scmActionEntries(changed); len(es) != 2 {
		t.Fatalf("stage should include changes and untracked files: %v", es)
	}
	for _, r := range a.scmRows() {
		if r.dir != "" {
			a.toggleDir(r)
		}
	}
	a.scmRows()
	if a.scm.selection.Len() != 0 {
		t.Fatal("collapsed files remain selected")
	}
}

func TestScmSelectionUnstagesRenames(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	git("mv", "main.go", "renamed.go")
	if err := os.WriteFile(hs.path("README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	hs.a.run("view.scm")
	hs.settleGit()
	hs.tt.Click("README.md")
	hs.tt.ClickWith(ui.Shift, "renamed.go")
	hs.settle()
	hs.key(0, ui.KeySpace)
	hs.settleGit()
	if es := hs.a.groupEntries()[groupStaged]; len(es) != 0 {
		t.Fatalf("renamed file's original path remains staged: %v", es)
	}
}

func TestScmSelectionDiscard(t *testing.T) {
	hs := scmSelectionFixture(t, false)
	hs.tt.Click("d.txt")
	hs.tt.ClickWith(ui.Shift, "b.txt")
	hs.settle()
	hs.tt.RightClick("c.txt")
	hs.settle()
	if err := hs.tt.Click("Discard Changes…"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.a.confirm == nil || hs.a.confirm.title != "Discard the changes of 3 files?" {
		t.Fatalf("discard did not ask about the selection: %+v", hs.a.confirm)
	}
	hs.a.confirm = nil
	// An unselected row's menu discards that file alone.
	hs.tt.RightClick("a.txt")
	hs.settle()
	if err := hs.tt.Click("Discard Changes…"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.a.confirm == nil || hs.a.confirm.title != "Move a.txt to "+trashName()+"?" {
		t.Fatalf("discard of an unselected row: %+v", hs.a.confirm)
	}
}

func TestScmCommitSelectionAndCopy(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	for _, msg := range []string{"second", "third", "fourth"} {
		if err := os.WriteFile(hs.path(msg+".txt"), []byte(msg+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-qm", msg)
	}
	if err := os.WriteFile(hs.path("loose.txt"), []byte("loose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, tt := hs.a, hs.tt
	a.run("view.scm")
	hs.settleGit()
	h := a.git.history
	short := func(subjects ...string) string {
		var hashes []string
		for _, s := range subjects {
			for _, cm := range h {
				if cm.Subject == s {
					hashes = append(hashes, cm.Short())
				}
			}
		}
		return strings.Join(hashes, "\n")
	}
	chosen := func() string {
		var subjects []string
		for _, r := range a.scmRows() {
			if r.commit != nil && a.scm.selection.Has(r.key) {
				subjects = append(subjects, r.commit.Subject)
			}
		}
		return strings.Join(subjects, ",")
	}
	// The rows are found before a preview shows their subjects elsewhere.
	at := map[string]ui.Rect{}
	for _, name := range []string{"loose.txt", "fourth", "third", "second"} {
		b, ok := tt.Find(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		at[name] = b
	}
	click := func(mods ui.Modifiers, name string) {
		b := at[name]
		tt.ClickAtWith(mods, b.X+b.W/2, b.Y+b.H/2)
		hs.settle()
	}
	// A file chosen is let go by a commit chosen, and the other way round.
	click(0, "loose.txt")
	click(0, "fourth")
	expectScmSelection(t, hs)
	hs.command("copy")
	if got := tt.Clipboard(); got != short("fourth") {
		t.Fatalf("copied after a click %q", got)
	}
	click(ui.Cmd, "second")
	if got := chosen(); got != "fourth,second" {
		t.Fatalf("Cmd-click chose %q", got)
	}
	hs.command("copy")
	if got := tt.Clipboard(); got != short("fourth", "second") {
		t.Fatalf("copied %q", got)
	}
	click(ui.Cmd, "second")
	if got := chosen(); got != "fourth" {
		t.Fatalf("Cmd-click again chose %q", got)
	}
	click(0, "fourth")
	click(ui.Shift, "second")
	if got := chosen(); got != "fourth,third,second" {
		t.Fatalf("Shift-click chose %q", got)
	}
	hs.key(ui.Shift, ui.KeyDown)
	if got := chosen(); got != "fourth,third,second,first" {
		t.Fatalf("Shift+Down chose %q", got)
	}
	hs.command("copy")
	if got := tt.Clipboard(); got != short("fourth", "third", "second", "first") {
		t.Fatalf("copied range %q", got)
	}
	click(0, "third")
	hs.command("selectAll")
	if got := chosen(); got != "fourth,third,second,first" {
		t.Fatalf("Select All chose %q", got)
	}
	click(0, "loose.txt")
	if got := chosen(); got != "" {
		t.Fatalf("file click kept commits %q", got)
	}
	expectScmSelection(t, hs, "loose.txt")
}
