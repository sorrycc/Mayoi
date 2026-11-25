package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/locales"
)

func clickDiffText(t *testing.T, hs *harness, v *diffView, row, side int, col float32) {
	t.Helper()
	if row < 0 {
		t.Fatal("no text row")
	}
	a := hs.a
	y, _ := a.sideRowY(v, max(side, 0), row)
	v.scroll.Y = max(0, y-150)
	hs.settle()
	l := a.diffGeometry(v, v.split, v.viewW)
	origin, ok := hs.tt.Find(locales.DiffPaneLabel())
	if !ok {
		t.Fatal("no diff pane")
	}
	x := l.textX + col*a.font.charW
	if v.split && side == 1 {
		x += l.right()
	}
	hs.tt.ClickAt(origin.X+x-v.scroll.X, origin.Y+y-v.scroll.Y+a.font.lineH/2)
	hs.settle()
}

func TestDiffEditModeWorkingTree(t *testing.T) {
	for _, layout := range []diffLayout{layoutSplit, layoutInline} {
		for _, group := range []string{"untracked", "changes"} {
			t.Run(layout.key()+"/"+group, func(t *testing.T) {
				hs := newHarness(t, true)
				runGit := hs.gitRepo()
				a, path := hs.a, hs.path("new.txt")
				a.settings.AutoSave, a.settings.DiffEditMode = false, true
				if group == "changes" {
					os.WriteFile(path, []byte("old\n"), 0o644)
					runGit("add", "new.txt")
					runGit("commit", "-qm", "base")
				}
				os.WriteFile(path, []byte("new\n"), 0o644)
				hs.settleGit()
				a.showChanges(diffTarget{path: path, base: diffIndex}, true)
				v := a.activeTab().diff
				hs.settleDiff(v)
				a.setDiffLayout(v, layout)
				hs.settle()
				if _, ok := hs.tt.Find("Editing"); !ok {
					t.Fatal("the edit button did not start enabled")
				}
				clickDiffText(t, hs, v, diffEditRow(v, path, 0), 1, 1)
				if v.editing == nil || v.editing.ed.sel != [2]int{1, 1} {
					t.Fatal("the click did not place an editing caret at the character")
				}
				if active, _ := hs.tt.TextInputActive(); !active {
					t.Fatal("the native window does not accept typed characters")
				}
				hs.tt.TypeKey(ui.Shift, ui.KeyX, "X")
				hs.settleDiff(v)
				if got := v.editing.doc.buf.String(); got != "nXew\n" {
					t.Fatalf("typing at the clicked character left %q", got)
				}
				a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyS, Mods: ui.Cmd})
				if data, _ := os.ReadFile(path); string(data) != "nXew\n" {
					t.Fatalf("saving left %q", data)
				}
				if a.activeTab().diff != v || strings.TrimSpace(runGit("diff", "--cached")) != "" {
					t.Fatal("editing left the diff or changed the index")
				}
				for _, tab := range a.editorTabs() {
					if tab.path() == path {
						t.Fatal("editing opened a file tab")
					}
				}
			})
		}
	}
}

func TestDiffEditModeInsertsAndDeletesLineBreaks(t *testing.T) {
	hs, v, path, _ := diffSelTab(t, layoutSplit)
	a := hs.a
	a.settings.AutoSave, a.settings.DiffEditMode = false, true
	clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 1)
	if v.editing == nil {
		t.Fatal("the click did not start editing")
	}
	doc := v.editing.doc
	before := doc.buf.String()
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEnter})
	hs.settleDiff(v)
	if got := doc.buf.String(); got != strings.Replace(before, "line two\n", "l\nine two\n", 1) {
		t.Fatalf("inserting a line break left %q", got)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyBackspace})
	hs.settleDiff(v)
	if got := doc.buf.String(); got != before {
		t.Fatalf("deleting the line break left %q", got)
	}
}

func TestDiffEditModeLeavesOldTextReadOnly(t *testing.T) {
	for _, layout := range []diffLayout{layoutSplit, layoutInline} {
		t.Run(layout.key(), func(t *testing.T) {
			hs, v, path, _ := diffSelTab(t, layout)
			a := hs.a
			a.settings.AutoSave, a.settings.DiffEditMode = false, true
			clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 1)
			if v.editing == nil {
				t.Fatal("the working tree did not start editing")
			}
			doc, old := v.editing.doc, v.editing.f.old.text
			before := doc.buf.String()
			clickDiffText(t, hs, v, diffOldRow(v, path, 2), 0, 1)
			hs.tt.Type("must not edit the working tree")
			hs.settleDiff(v)
			if v.editing != nil || doc.buf.String() != before || v.files[0].old.text != old {
				t.Fatal("typing after clicking the old text edited a source")
			}
		})
	}
}

func TestDiffEditModeLeavesStagedAndCommitTextReadOnly(t *testing.T) {
	hs := newHarness(t, true)
	runGit := hs.gitRepo()
	a, path := hs.a, hs.path("t.txt")
	a.settings.DiffEditMode = true
	twoHunkFile(hs, runGit)
	runGit("add", "t.txt")
	hs.settleGit()
	a.showChanges(diffTarget{path: path, base: diffStaged}, true)
	v := a.activeTab().diff
	hs.settleDiff(v)
	_, editButton := hs.tt.Find("Edit")
	_, editingButton := hs.tt.Find("Editing")
	if editButton || editingButton {
		t.Fatal("a staged diff showed an edit button")
	}
	clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 1)
	hs.tt.Type("must not edit staged text")
	hs.settleDiff(v)
	if v.editing != nil || a.docIndex(path) >= 0 {
		t.Fatal("the staged version started editing")
	}
	runGit("commit", "-qm", "changed")
	v = a.showCommit(git.Commit{Hash: strings.TrimSpace(runGit("rev-parse", "HEAD"))}, true)
	hs.settleDiff(v)
	_, editButton = hs.tt.Find("Edit")
	_, editingButton = hs.tt.Find("Editing")
	if editButton || editingButton {
		t.Fatal("a commit diff showed an edit button")
	}
	clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 1)
	hs.tt.Type("must not edit a commit")
	hs.settleDiff(v)
	if v.editing != nil || a.docIndex(path) >= 0 || strings.TrimSpace(runGit("status", "--porcelain")) != "" {
		t.Fatal("the commit version started editing or changed the working tree")
	}
}

func TestDiffEditModeDefaultsToBrowsing(t *testing.T) {
	hs, v, path, _ := diffSelTab(t, layoutSplit)
	a := hs.a
	row := diffEditRow(v, path, 2)
	clickDiffText(t, hs, v, row, 1, 1)
	hs.tt.Type("must not edit")
	hs.settleDiff(v)
	if v.editing != nil || a.docIndex(path) >= 0 {
		t.Fatal("clicking started editing by default")
	}
	if _, ok := hs.tt.Find("Edit"); !ok {
		t.Fatal("the edit button did not start disabled")
	}
	a.settings.DiffEditMode = true
	clickDiffText(t, hs, v, row, 1, 1)
	if v.editing == nil {
		t.Fatal("enabling the preference did not apply to the open diff")
	}
	a.endDiffEdit(v)
	a.settings.DiffEditMode = false
	clickDiffText(t, hs, v, row, 1, 1)
	if v.editing != nil {
		t.Fatal("disabling the preference kept direct editing enabled")
	}
	if !a.beginDiffEdit(v, row) {
		t.Fatal("explicit editing stopped working")
	}
}

func assertDiffNativeTyping(t *testing.T, hs *harness, v *diffView) {
	t.Helper()
	active, noIME := hs.tt.TextInputActive()
	if !active || noIME {
		t.Fatal("the native window does not accept typed or composed characters")
	}
	if caret, ok := hs.tt.TextCaret(); !ok || caret.W <= 0 || caret.H <= 0 {
		t.Fatal("the input method has no caret")
	}
	doc := v.editing.doc
	before := doc.buf.String()
	hs.tt.TypeKey(0, ui.KeyX, "x")
	hs.tt.Compose("ni", 2)
	hs.tt.Type("你")
	hs.settleShown(v)
	if got := doc.buf.String(); got != "x你"+before {
		t.Fatalf("typing at the caret left %q", got)
	}
}

func TestDiffEditModeNativeInputInOneFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		layout  diffLayout
		enabled bool
	}{
		{"manual/split", layoutSplit, false},
		{"manual/inline", layoutInline, false},
		{"automatic/split", layoutSplit, true},
		{"automatic/inline", layoutInline, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs, v := oneFileDiffOf(t)
			a := hs.a
			a.settings.AutoSave, a.settings.DiffEditMode = false, tc.enabled
			os.WriteFile(hs.path("z.txt"), []byte("new\n"), 0o644)
			hs.settleGit()
			a.setDiffLayout(v, tc.layout)
			for _, name := range []string{"a.txt", "b.txt", "z.txt"} {
				path := hs.path(name)
				a.showChanges(diffTarget{path: path, base: diffIndex}, true)
				hs.settleShown(v)
				if tc.enabled {
					clickDiffText(t, hs, v, diffEditRow(v, path, 0), 1, 0)
				} else {
					v.setCursor(diffEditRow(v, path, 0))
					hs.tt.Key(0, ui.KeyI)
					hs.settle()
				}
				if v.editing == nil || v.editing.f.path != path {
					t.Fatalf("%s did not start editing", name)
				}
				assertDiffNativeTyping(t, hs, v)
				hs.tt.Key(0, ui.KeyEscape)
				hs.settle()
				if active, _ := hs.tt.TextInputActive(); active {
					t.Fatal("leaving editing kept native text input active")
				}
			}
		})
	}
}

func TestDiffEditModeButtonIsLocalToTheBuffer(t *testing.T) {
	labels := [...]string{"Edit", "Editing"}
	for _, enabled := range []bool{false, true} {
		t.Run(labels[btoi(enabled)], func(t *testing.T) {
			hs, v := oneFileDiffOf(t)
			a := hs.a
			a.settings.AutoSave, a.settings.DiffEditMode = false, enabled
			hs.settleShown(v)
			first, before := v.shownFile(), v.shownFile().new.text
			if _, ok := hs.tt.Find(labels[btoi(enabled)]); !ok {
				t.Fatal("the button did not follow the default")
			}
			if err := hs.tt.Click(labels[btoi(enabled)]); err != nil {
				t.Fatal(err)
			}
			hs.settleShown(v)
			a.showChanges(diffTarget{path: hs.path("b.txt"), base: diffIndex}, true)
			hs.settleShown(v)
			if _, ok := hs.tt.Find(labels[btoi(enabled)]); !ok {
				t.Fatal("toggling the first buffer changed another buffer's default")
			}
			a.showChanges(diffTarget{path: first.path, base: diffIndex}, true)
			hs.settleShown(v)
			if _, ok := hs.tt.Find(labels[btoi(!enabled)]); !ok {
				t.Fatal("returning to the buffer lost its manual choice")
			}
			clickDiffText(t, hs, v, diffEditRow(v, first.path, 0), 1, 0)
			hs.tt.TypeKey(0, ui.KeyX, "x")
			hs.settleShown(v)
			doc := a.diffDoc(first)
			if enabled {
				if v.editing != nil || doc != nil && doc.buf.String() != before {
					t.Fatal("clicking text re-enabled a buffer that was manually disabled")
				}
			} else if v.editing == nil || doc == nil || doc.buf.String() != "x"+before {
				t.Fatal("the buffer manually enabled did not take text")
			}
			if a.settings.DiffEditMode != enabled {
				t.Fatal("the edit button changed the global preference")
			}
		})
	}
}

func TestDiffEditModeDoesNotTransferBetweenBuffers(t *testing.T) {
	labels := [...]string{"Edit", "Editing"}
	for _, enabled := range []bool{false, true} {
		t.Run(labels[btoi(enabled)], func(t *testing.T) {
			hs, v := oneFileDiffOf(t)
			a := hs.a
			a.settings.AutoSave, a.settings.DiffEditMode = false, enabled
			hs.settleShown(v)
			first := v.shownFile().path
			if err := hs.tt.Click(labels[btoi(enabled)]); err != nil {
				t.Fatal(err)
			}
			hs.settleShown(v)
			second := hs.path("b.txt")
			a.showChanges(diffTarget{path: second, base: diffIndex}, true)
			hs.settleShown(v)
			if enabled {
				clickDiffText(t, hs, v, diffEditRow(v, second, 0), 1, 0)
				if v.editing == nil {
					t.Fatal("the default-enabled buffer did not start editing")
				}
				a.showChanges(diffTarget{path: first, base: diffIndex}, true)
				hs.settleShown(v)
			}
			target := v.shownFile()
			before := target.new.text
			clickDiffText(t, hs, v, diffEditRow(v, target.path, 0), 1, 0)
			hs.tt.TypeKey(0, ui.KeyX, "x")
			hs.settleShown(v)
			doc := a.diffDoc(target)
			if v.editing != nil || doc != nil && doc.buf.String() != before {
				t.Fatal("the previous buffer's caret enabled a disabled buffer")
			}
		})
	}
}

func TestDiffEditModeNavigationDoesNotEditHiddenFile(t *testing.T) {
	for _, one := range []bool{false, true} {
		for _, source := range []string{"next", "source-control"} {
			name := "all/" + source
			if one {
				name = "one/" + source
			}
			t.Run(name, func(t *testing.T) {
				hs, v := oneFileDiffOf(t)
				a := hs.a
				a.setDiffFiles(v, false)
				hs.settleDiff(v)
				a.setDiffFiles(v, one)
				hs.settleDiff(v)
				a.settings.AutoSave, a.settings.DiffEditMode = false, true
				clickDiffText(t, hs, v, diffEditRow(v, hs.path("a.txt"), 0), 1, 0)
				if v.editing == nil {
					t.Fatal("the first file did not start editing")
				}
				doc, before := v.editing.doc, v.editing.doc.buf.String()
				if source == "next" {
					v.stepFile(1)
				} else {
					a.showChanges(diffTarget{path: hs.path("b.txt"), base: diffIndex}, true)
				}
				hs.settleDiff(v)
				hs.tt.TypeKey(0, ui.KeyX, "x")
				a.services.WriteClipboard("wrong file")
				hs.tt.Command("paste")
				hs.settleDiff(v)
				if doc.buf.String() != before {
					t.Fatal("typing or pasting after navigation changed the previous file")
				}
			})
		}
	}
}

func TestDiffEditModeStagedDeletionHasNoEditor(t *testing.T) {
	hs := newHarness(t, true)
	runGit := hs.gitRepo()
	a, path := hs.a, hs.path("a.txt")
	os.WriteFile(path, []byte("old\n"), 0o644)
	runGit("add", "a.txt")
	runGit("commit", "-qm", "tracked")
	os.Remove(path)
	runGit("add", "a.txt")
	os.WriteFile(hs.path("z.txt"), []byte("new\n"), 0o644)
	a.settings.DiffEditMode = true
	hs.settleGit()
	a.showChanges(diffTarget{path: path, base: diffStaged}, true)
	v := a.activeTab().diff
	hs.settleDiff(v)
	v.setCursor(diffOldRow(v, path, 0))
	hs.settle()
	if _, ok := hs.tt.Find("Editing"); ok {
		t.Error("staged deletion showed an edit button for the next file")
	}
	hs.tt.Key(0, ui.KeyI)
	hs.settle()
	if v.editing != nil {
		t.Fatal("I on a staged deletion began editing another file")
	}
}

func TestDiffEditModeShortcutDoesNotType(t *testing.T) {
	hs, v, path, _ := diffSelTab(t, layoutSplit)
	a := hs.a
	a.settings.AutoSave, a.settings.DiffEditMode = false, true
	withKeys(t, a, `[{"key":"ctrl+. x","command":"workbench.action.toggleSidebarVisibility","when":"editorFocus"}]`)
	clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 0)
	if v.editing == nil {
		t.Fatal("no edit")
	}
	doc, before := v.editing.doc, v.editing.doc.buf.String()
	hs.tt.Key(ui.Ctrl, ui.KeyPeriod)
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyX, "x")
	hs.settleDiff(v)
	if doc.buf.String() != before {
		t.Fatal("a shortcut chord also inserted its final letter")
	}
	hs.tt.TypeKey(0, ui.KeyY, "y")
	hs.settleDiff(v)
	if got := doc.buf.String(); got != strings.Replace(before, "line two", "yline two", 1) {
		t.Fatal("typing did not resume after the shortcut")
	}
}

func TestDiffEditModeMultipleClicksSelectText(t *testing.T) {
	for _, layout := range []diffLayout{layoutSplit, layoutInline} {
		for _, word := range []bool{false, true} {
			name, clicks, want := "line", 3, "line twenty-five"
			if word {
				name, clicks, want = "word", 2, "twenty"
			}
			t.Run(layout.key()+"/"+name, func(t *testing.T) {
				hs, v, path, at := diffSelTab(t, layout)
				hs.a.settings.AutoSave, hs.a.settings.DiffEditMode = false, true
				x, y := at(diffEditRow(v, path, 25), 1, 7.3)
				for range clicks {
					hs.tt.ClickAt(x, y)
				}
				hs.settleDiff(v)
				if v.editing == nil || v.editing.diffEditedText() != want {
					t.Fatalf("%d clicks did not select %q", clicks, want)
				}
				hs.tt.Command("copy")
				if got := hs.a.services.ReadClipboard(); got != want {
					t.Fatalf("copied %q, want %q", got, want)
				}
			})
		}
	}
}

func TestDiffEditModeSelectAllKeepsNativeInput(t *testing.T) {
	for _, layout := range []diffLayout{layoutSplit, layoutInline} {
		for _, empty := range []bool{false, true} {
			name := "replace"
			if empty {
				name = "delete-then-type"
			}
			t.Run(layout.key()+"/"+name, func(t *testing.T) {
				hs, v, path, _ := diffSelTab(t, layout)
				hs.a.settings.AutoSave, hs.a.settings.DiffEditMode = false, true
				clickDiffText(t, hs, v, diffEditRow(v, path, 2), 1, 0)
				if v.editing == nil {
					t.Fatal("the file did not start editing")
				}
				doc := v.editing.doc
				hs.tt.Command("selectAll")
				if empty {
					hs.tt.Key(0, ui.KeyBackspace)
				}
				hs.settleDiff(v)
				if active, _ := hs.tt.TextInputActive(); !active {
					t.Fatal("selecting or deleting all disabled native typing")
				}
				hs.tt.TypeKey(0, ui.KeyX, "x")
				hs.settleDiff(v)
				if got := doc.buf.String(); got != "x" {
					t.Fatalf("typing after selecting or deleting all left %q", got)
				}
			})
		}
	}
}
