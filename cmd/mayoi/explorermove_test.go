package main

import (
	"os"
	"strings"
	"testing"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// moveRowAt is the middle of the explorer's row showing name, which the
// sidebar, at the window's left, shows before the editor does.
func moveRowAt(t *testing.T, hs *harness, name string) (float32, float32) {
	t.Helper()
	r, ok := hs.tt.Find(name)
	if !ok || r.X > 300 {
		t.Fatalf("no row %q in the explorer: %q", name, hs.tt.Texts())
	}
	return r.X + r.W/2, r.Y + r.H/2
}

// dragRow drags the explorer's row of from onto the row of onto.
func dragRow(t *testing.T, hs *harness, from, onto string) {
	t.Helper()
	x0, y0 := moveRowAt(t, hs, from)
	x1, y1 := moveRowAt(t, hs, onto)
	hs.drag(x0, y0, x1, y1)
}

// toasted waits for a failure saying what, shown as a toast.
func toasted(hs *harness, what string) {
	hs.until("the toast "+what, func() bool { return hs.tt.HasText(what) })
}

func onDisk(p string) bool { _, err := os.Lstat(p); return err == nil }

// textCount is how many texts of the window are s.
func textCount(hs *harness, s string) int {
	n := 0
	for _, x := range hs.tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

func TestExplorerDragMovesFiles(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	// A file with changes not yet saved, in a tab.
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	d := a.activeEditor()
	hs.tt.Type("draft ")
	hs.settle()
	if !d.dirty() {
		t.Fatal("no unsaved changes")
	}

	// Onto a folder's row: into it, the tab and its text following.
	dragRow(t, hs, "README.md", "web")
	if onDisk(hs.path("README.md")) || !onDisk(hs.path("web/README.md")) {
		t.Fatal("README.md did not move into web")
	}
	if d.path != hs.path("web/README.md") || !d.dirty() || !strings.HasPrefix(d.buf.String(), "draft ") {
		t.Fatalf("the document is at %s, dirty %v", d.path, d.dirty())
	}
	if a.column().showingFile(hs.path("web/README.md")) < 0 {
		t.Fatal("the tab did not follow the file")
	}
	if a.explorerChoice() != hs.path("web/README.md") || !a.files.Open.Has(hs.path("web")) {
		t.Fatalf("chosen %q", a.explorerChoice())
	}

	// Onto a file's row: into that file's folder.
	dragRow(t, hs, "main.go", "style.css")
	if !onDisk(hs.path("web/main.go")) || onDisk(hs.path("main.go")) {
		t.Fatal("main.go did not move beside style.css")
	}

	// Below the rows: into the workspace's folder.
	x0, y0 := moveRowAt(t, hs, "main.go")
	_, ly := moveRowAt(t, hs, "go.mod")
	hs.drag(x0, y0, x0, ly+80)
	if !onDisk(hs.path("main.go")) || onDisk(hs.path("web/main.go")) {
		t.Fatal("main.go did not move back to the workspace")
	}

	// A folder, with what is in it.
	dragRow(t, hs, "internal", "web")
	if !onDisk(hs.path("web/internal/a/a.go")) || onDisk(hs.path("internal")) {
		t.Fatal("internal did not move into web")
	}
	if a.fails != 0 {
		t.Fatalf("%d failures", a.fails)
	}
}

func TestExplorerDragRefusesBadMoves(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	// A file of the name there already stays, as does the one dragged.
	os.WriteFile(hs.path("web/go.mod"), []byte("other\n"), 0o644)
	a.tree.Refresh()
	hs.settle()
	dragRow(t, hs, "go.mod", "web")
	toasted(hs, locales.FailMove("go.mod", locales.ErrNameExists()))
	if b, _ := os.ReadFile(hs.path("web/go.mod")); string(b) != "other\n" || !onDisk(hs.path("go.mod")) {
		t.Fatal("a move went over a file of its name")
	}

	// A folder into one of its own: refused, saying why.
	a.files.Open.Add(hs.path("internal"))
	hs.settle()
	dragRow(t, hs, "internal", "a")
	toasted(hs, locales.FailMove("internal", locales.ErrMoveIntoItself()))
	if !onDisk(hs.path("internal/a/a.go")) {
		t.Fatal("internal moved")
	}

	// Onto itself, or where it is: nothing, said or done.
	fails := a.fails
	x, y := moveRowAt(t, hs, "internal")
	hs.drag(x, y, x+30, y+3)
	dragRow(t, hs, "b", "internal")
	if !onDisk(hs.path("internal/b/b.ts")) || a.fails != fails {
		t.Fatalf("%d failures", a.fails-fails)
	}

	// What another explorer drags, or search and source control, which
	// drag no tree, moves nothing here.
	a.dropInTree(&treeDrag{path: hs.path("README.md"), from: &app{}}, hs.path("web"))
	a.dropInTree(treeSource(dragItem{file: &fileDrag{path: hs.path("README.md")}}), hs.path("web"))
	if !onDisk(hs.path("README.md")) || a.fails != fails {
		t.Fatal("a drag from elsewhere moved a file")
	}
}

// A file of the explorer still opens where it is dropped on the editor,
// and moves nothing.
func TestExplorerDragStillOpensInColumns(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("go.mod"), true)
	hs.settle()
	x0, y0 := moveRowAt(t, hs, "README.md")
	body, ok := hs.tt.Find("Editor go.mod")
	if !ok {
		t.Fatalf("no editor: %q", hs.tt.Texts())
	}
	hs.drag(x0, y0, body.X+body.W/2, body.Y+body.H/2)
	if got := tabNames(a.cols[0]); got != "go.mod README.md" {
		t.Fatalf("tabs %q", got)
	}
	if !onDisk(hs.path("README.md")) {
		t.Fatal("the file moved")
	}
}

func TestExplorerDragMovesRemoteFiles(t *testing.T) {
	hs, conn, dir := remoteHarness(t)
	a := hs.a
	a.openFile(hs.root+"/main.go", true)
	hs.until("main.go open", func() bool { return a.activeEditor() != nil })
	dragRow(t, hs, "main.go", "src")
	moved := remote.Join(hs.root, "src", "main.go")
	hs.until("main.go moved", func() bool { return len(a.moving) == 0 && a.activeEditor().path == moved })
	if _, err := conn.Run(nil, `test -f "$1/src/main.go" && ! test -e "$1/main.go"`, dir); err != nil {
		t.Fatalf("not moved on the host: %v", err)
	}
	if a.fails != 0 {
		t.Fatalf("%d failures", a.fails)
	}

	// One of the name there already: refused, nothing moved.
	if _, err := conn.Run(nil, `printf 'x\n' > "$1/a.txt"`, dir); err != nil {
		t.Fatal(err)
	}
	a.tree.Refresh()
	hs.until("the new a.txt", func() bool { return textCount(hs, "a.txt") == 2 })
	a.files.Open.Remove(hs.root + "/src")
	hs.until("src closed", func() bool { return textCount(hs, "a.txt") == 1 })
	dragRow(t, hs, "a.txt", "src")
	toasted(hs, locales.FailMove("a.txt", locales.ErrNameExists()))
	if out, err := conn.Run(nil, `cat "$1/src/a.txt"; test -f "$1/a.txt"`, dir); err != nil || string(out) != "needle here\n" {
		t.Fatalf("src/a.txt holds %q: %v", out, err)
	}
}
