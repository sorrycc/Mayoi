package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
)

// A bulk choice made before the commit's file list arrives must survive
// both that list and the first read of a large file's contents.
func TestDiffFoldAllBeforeCommitLoads(t *testing.T) {
	for _, folded := range []bool{true, false} {
		name, command := "expand", "git.expandAllFiles"
		if folded {
			name, command = "collapse", "git.collapseAllFiles"
		}
		t.Run(name, func(t *testing.T) {
			hs := newHarness(t, true)
			gitCmd := hs.gitRepo()
			for path, text := range map[string]string{
				"a.txt": strings.Repeat("line\n", foldedLines+1),
				"b.txt": "small\n",
			} {
				if err := os.WriteFile(hs.path(path), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gitCmd("add", ".")
			gitCmd("commit", "-qm", "two files")
			hs.a.showCommit(git.Commit{Hash: strings.TrimSpace(gitCmd("rev-parse", "HEAD"))}, true)
			v := hs.a.activeTab().diff
			hs.a.run(command)
			hs.settleDiff(v)
			if len(v.files) != 2 {
				t.Fatalf("files: %d, want 2", len(v.files))
			}
			for _, f := range v.files {
				if f.folded != folded {
					t.Errorf("%s folded=%v, want %v", f.path, f.folded, folded)
				}
			}
		})
	}
}

// User shortcuts run the same commands as the menu and palette. They
// retain the current file, affect only this tab, and leave Git untouched.
func TestDiffFoldAllCommands(t *testing.T) {
	hs := newHarness(t, true)
	gitCmd := hs.gitRepo()
	a, tt := hs.a, hs.tt
	for _, path := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(hs.path(path), []byte(strings.Repeat("line\n", 60)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd("add", ".")
	gitCmd("commit", "-qm", "two files")
	a.compareFiles(hs.path("a.txt"), hs.path("b.txt"))
	other := a.activeTab().diff
	hs.settleDiff(other)
	a.showCommit(git.Commit{Hash: strings.TrimSpace(gitCmd("rev-parse", "HEAD"))}, true)
	v := a.activeTab().diff
	hs.settleDiff(v)
	withKeys(t, a, `[
		{"key":"ctrl+shift+f9","command":"git.collapseAllFiles"},
		{"key":"ctrl+shift+f10","command":"git.expandAllFiles"}
	]`)
	for i, r := range v.rows {
		if r.file == 1 && r.kind == rowLine {
			v.setCursor(i)
			v.jumpRow(i, jumpTop)
			break
		}
	}
	hs.settle()
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyF9)
	hs.settle()
	for _, f := range v.files {
		if !f.folded {
			t.Fatalf("shortcut left %s expanded", f.path)
		}
	}
	for _, r := range v.rows {
		if r.kind == rowLine {
			t.Fatal("collapsed diff still shows file contents")
		}
	}
	if v.cursor < 0 || v.rows[v.cursor].kind != rowFile || v.rows[v.cursor].file != 1 {
		t.Fatalf("collapse lost the current file: cursor %d", v.cursor)
	}
	if y := editorPadY + float32(v.tops[v.cursor])*a.font.lineH - v.scroll.Y; y < 0 || y >= v.viewH {
		t.Fatalf("collapsed file header is outside the view at %v", y)
	}
	a.setDiffLayout(v, layoutSplit)
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyF10)
	hs.settle()
	for _, f := range v.files {
		if f.folded {
			t.Fatalf("shortcut left %s collapsed", f.path)
		}
	}
	if other.foldFiles != nil || other.files[0].folded {
		t.Fatal("bulk folding changed another diff tab")
	}
	a.openFile(hs.path("a.txt"), true)
	hs.settle()
	a.run("git.collapseAllFiles")
	for _, f := range v.files {
		if f.folded {
			t.Fatal("the command changed an inactive diff from a text tab")
		}
	}
	if status := gitCmd("status", "--porcelain"); status != "" {
		t.Fatalf("view commands changed Git state: %s", status)
	}
}
