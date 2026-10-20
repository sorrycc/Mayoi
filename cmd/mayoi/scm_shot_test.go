package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shotRect saves the part of the window in r, in points.
func (hs *harness) shotRect(name string, r image.Rectangle, scale int) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		return
	}
	img := hs.tt.Image()
	sub := img.SubImage(image.Rect(r.Min.X*scale, r.Min.Y*scale, r.Max.X*scale, r.Max.Y*scale))
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		hs.t.Fatal(err)
	}
	png.Encode(f, sub)
	f.Close()
}

// scmFixture changes the harness's work tree in each way source control
// lists: staged, changed, removed and untracked.
func scmFixture(hs *harness) {
	git := hs.gitRepo()
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nimport \"fmt\"\n\n// main says hi.\nfunc main() {\n\tfmt.Println(\"hi, mayoi\")\n\tfmt.Println(\"bye\")\n}\n"), 0o644)
	os.WriteFile(hs.path("internal/a/a.go"), []byte("package a\n\nconst Answer = 43\n\nconst Question = \"?\"\n"), 0o644)
	git("add", "internal/a/a.go")
	os.WriteFile(hs.path("web/style.css"), []byte("body { color: #222; margin: 0; }\n"), 0o644)
	os.Remove(hs.path("README.md"))
	os.WriteFile(hs.path("internal/b/new.ts"), []byte("export const x = 1;\n"), 0o644)
	os.WriteFile(hs.path("notes.txt"), []byte("hi\n"), 0o644)
}

// The source control view and the diff tab, with changes of each kind.
func TestShotSourceControl(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		scmFixture(hs)
		a := hs.a
		a.run("view.scm")
		hs.settleGit()
		name := "scm-light"
		if dark {
			name = "scm-dark"
		}
		hs.shot(name)
		a.showChanges(diffTarget{path: hs.path("main.go"), base: diffIndex}, true)
		hs.settleGit()
		hs.shot(name + "-diff")
		// The pointer on a change shows the buttons of its run.
		v := a.activeTab().diff
		for i, r := range v.rows {
			if r.changed() && v.files[r.file].path == hs.path("main.go") {
				y := titleBarH + tabStripH + crumbsH + editorPadY + float32(v.tops[i])*a.font.lineH - v.scroll.Y + a.font.lineH/2
				hs.tt.Move(a.window.SidebarWidth+300, y)
				break
			}
		}
		hs.settle()
		hs.shot(name + "-hunk")
		a.setDiffLayout(a.activeTab().diff, layoutSplit)
		hs.settle()
		hs.shot(name + "-split")
	}
}

// The source control view up close, at twice the scale.
func TestShotSourceControlClose(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		hs.tt.SetScale(2)
		scmFixture(hs)
		a := hs.a
		a.run("view.scm")
		hs.settleGit()
		a.showChanges(diffTarget{path: hs.path("main.go"), base: diffIndex}, false)
		hs.settleGit()
		name := "scm-close-light"
		if dark {
			name = "scm-close-dark"
		}
		hs.tt.Move(120, 300)
		hs.settle()
		hs.shotRect(name, image.Rect(0, 0, 250, 420), 2)
		hs.shotRect(name+"-diff", image.Rect(248, 70, 1200, 470), 2)
	}
}

// The source control view's other states: clean and ahead of its
// upstream, amending with a long summary, and outside a repository.
func TestShotSourceControlStates(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		hs.tt.SetScale(2)
		git := hs.gitRepo()
		remote := filepath.Join(t.TempDir(), "remote.git")
		git("init", "-q", "--bare", remote)
		git("remote", "add", "origin", remote)
		git("push", "-q", "-u", "origin", "main")
		os.WriteFile(hs.path("main.go"), []byte("package main\n"), 0o644)
		git("commit", "-qam", "second")
		a := hs.a
		a.run("view.scm")
		hs.settleGit()
		hs.shotRect("scm-clean"+suffix, image.Rect(0, 0, 250, 420), 2)

		os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
		git("add", "main.go")
		hs.settleGit()
		a.startAmend()
		hs.settleGit()
		a.git.message = "Make the source control view look and feel professional, at last\n\nMore words."
		hs.settle()
		hs.shotRect("scm-amend"+suffix, image.Rect(0, 0, 250, 420), 2)

		hs2 := newHarness(t, dark)
		hs2.tt.SetScale(2)
		hs2.a.run("view.scm")
		hs2.settle()
		hs2.shotRect("scm-norepo"+suffix, image.Rect(0, 0, 250, 420), 2)
	}
}

// The history in source control, and the changes of a commit.
func TestShotCommits(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		git := hs.gitRepo()
		remote := filepath.Join(t.TempDir(), "remote.git")
		git("init", "-q", "--bare", remote)
		git("remote", "add", "origin", remote)
		os.WriteFile(hs.path("main.go"), []byte("package main\n\nimport \"fmt\"\n\n// main says hi.\nfunc main() {\n\tfmt.Println(\"hi, mayoi\")\n}\n"), 0o644)
		git("commit", "-qam", "Say hi rather than hello")
		git("tag", "v0.1.0")
		git("switch", "-q", "-c", "polish/greeting")
		os.WriteFile(hs.path("web/style.css"), []byte("body { color: #444; }\n"), 0o644)
		git("commit", "-qam", "Soften the text color")
		git("switch", "-q", "main")
		os.WriteFile(hs.path("main.go"), []byte("package main\n\nimport \"fmt\"\n\n// main says hi.\nfunc main() {\n\tfmt.Println(\"hi, mayoi!\")\n}\n"), 0o644)
		git("commit", "-qam", "Greet with more cheer")
		git("merge", "-q", "--no-ff", "-m", "Merge branch 'polish/greeting'", "polish/greeting")
		git("tag", "v0.2.0")
		git("push", "-q", "-u", "origin", "main")
		os.WriteFile(hs.path("internal/a/a.go"), []byte("package a\n\n// Answer is the answer.\nconst Answer = 42\n"), 0o644)
		git("mv", "README.md", "README.markdown")
		git("commit", "-qam", "Document the answer and rename the README\n\nThe README keeps its words; only its name changes.\nThe answer is still 42.")
		os.WriteFile(hs.path("web/style.css"), []byte("body { color: #222; }\n"), 0o644)
		a := hs.a
		a.run("view.scm")
		hs.settleGit()
		cs := a.git.history
		if len(cs) != 6 {
			t.Fatalf("history %d", len(cs))
		}
		a.showCommit(cs[0], false)
		hs.settleGit()
		hs.settle()
		hs.shot("commits" + suffix)
	}
}

// The box that switches branches.
func TestShotBranchPicker(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		git := hs.gitRepo()
		remote := filepath.Join(t.TempDir(), "remote.git")
		git("init", "-q", "--bare", remote)
		git("remote", "add", "origin", remote)
		for _, b := range []string{"fix-scroll", "feature/branch-picker", "release-0.3"} {
			git("switch", "-q", "-c", b)
			os.WriteFile(hs.path("notes-"+strings.ReplaceAll(b, "/", "-")+".txt"), []byte(b), 0o644)
			git("add", ".")
			git("commit", "-qm", "Work on "+b)
		}
		git("push", "-q", "origin", "release-0.3")
		git("switch", "-q", "main")
		git("branch", "-q", "-D", "release-0.3")
		hs.a.run("view.scm")
		hs.settleGit()
		hs.a.openBranchPicker()
		hs.settle()
		hs.shot("branches" + suffix)
		hs.tt.Type("feature/new-picker")
		hs.settle()
		hs.shot("branches-create" + suffix)
	}
}

// The changes in a tree of their folders.
func TestShotScmTree(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		hs := newHarness(t, dark)
		hs.tt.SetScale(2)
		scmFixture(hs)
		os.WriteFile(hs.path("internal/a/more.go"), []byte("package a\n"), 0o644)
		a := hs.a
		a.settings.ScmTree = true
		a.run("view.scm")
		hs.settleGit()
		hs.tt.Move(120, 300)
		hs.settle()
		hs.shotRect("scm-tree"+suffix, image.Rect(0, 0, 250, 460), 2)
	}
}
