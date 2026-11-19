package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// railSettle draws frames until done says the drop has landed.
func railSettle(t *testing.T, tt *ui.Tester, done func() bool) {
	t.Helper()
	for range 400 {
		step(tt)
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the drop never landed")
}

// allTabs names the tabs of every column of a, in order.
func allTabs(a *app) []string {
	var out []string
	for _, col := range a.cols {
		for _, t := range col.tabs {
			out = append(out, t.name())
		}
	}
	return out
}

// Files dropped from another app on another project's square show that
// project and open in its file column, a batch in order, a file from
// outside its folder and a picture too, which leaves its icon alone.
func TestRailDropOpensFilesInProject(t *testing.T) {
	shown := workspaceFixture(t)
	other := filepath.Join(t.TempDir(), "railother")
	if err := writeFixture(other); err != nil {
		t.Fatal(err)
	}
	f, hs, tt := railFixture(t, shown, other)
	f.m.icons = filepath.Join(t.TempDir(), "project-icons")
	outside := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(outside, []byte("hi\n"), 0o644)
	pic := filepath.Join(t.TempDir(), "railpic.png")
	writePNG(t, pic, 16, 16)

	r, ok := tt.Find("railother")
	if !ok {
		t.Fatal("no square")
	}
	if !tt.DropFiles(r.X+r.W/2, r.Y+r.H/2, filepath.Join(other, "go.mod"), outside, pic) {
		t.Fatal("the square did not take the files")
	}
	b := hs[1].a
	railSettle(t, tt, func() bool { return len(allTabs(b)) >= 3 })
	if f.active != hs[1] {
		t.Error("the project dropped on does not show")
	}
	if got := allTabs(b); !slices.Equal(got, []string{"go.mod", "notes.txt", "railpic.png"}) {
		t.Errorf("tabs %q", got)
	}
	if got := allTabs(hs[0].a); len(got) != 0 {
		t.Errorf("the other project opened %q", got)
	}
	if st := f.m.s.ProjectTabStyle[other]; st.Icon != "" {
		t.Errorf("the picture became the icon: %+v", st)
	}
}

// Files dropped on the rail around the squares open in the project
// shown, which stays.
func TestRailDropOnBackground(t *testing.T) {
	shown, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, shown, other)
	if !tt.DropFiles(railW/2, 700, filepath.Join(shown, "go.mod")) {
		t.Fatal("the rail did not take the file")
	}
	a := hs[0].a
	railSettle(t, tt, func() bool { return len(allTabs(a)) > 0 })
	if f.active != hs[0] || !slices.Equal(allTabs(a), []string{"go.mod"}) || len(allTabs(hs[1].a)) != 0 {
		t.Errorf("shown %v, tabs %q and %q", f.active == hs[0], allTabs(a), allTabs(hs[1].a))
	}
}

// Folders dropped on a square open as projects and show no other; with
// files, the files open in the project dropped on.
func TestRailDropFolders(t *testing.T) {
	shown, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, shown, other)
	host := hs[0].a.host.(*fakeHost)
	r, _ := tt.Find(filepath.Base(other))
	folder := filepath.Join(other, "web")
	tt.DropFiles(r.X+r.W/2, r.Y+r.H/2, folder)
	railSettle(t, tt, func() bool { return len(host.folders) == 1 })
	if f.active != hs[0] || host.folders[0] != folder {
		t.Fatalf("a folder alone: shown %v, folders %q", f.active == hs[0], host.folders)
	}
	tt.DropFiles(r.X+r.W/2, r.Y+r.H/2, folder, filepath.Join(other, "go.mod"))
	railSettle(t, tt, func() bool { return len(allTabs(hs[1].a)) > 0 })
	if f.active != hs[1] || len(host.folders) != 2 || !slices.Equal(allTabs(hs[1].a), []string{"go.mod"}) {
		t.Errorf("mixed: shown %v, folders %q, tabs %q", f.active == hs[1], host.folders, allTabs(hs[1].a))
	}
}

// Files dropped on a project closed before the drop runs open in the
// one shown; those of a locked column go to the file column.
func TestRailDropTargetGoneAndLocked(t *testing.T) {
	shown, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, shown, other)
	a := hs[0].a
	gone := &desktop{m: f.m, a: hs[1].a}
	f.dropOnRail(a, gone, []string{filepath.Join(shown, "go.mod")})
	railSettle(t, tt, func() bool { return len(allTabs(a)) > 0 })
	if f.active != hs[0] || !slices.Equal(allTabs(a), []string{"go.mod"}) {
		t.Fatalf("shown %v, tabs %q", f.active == hs[0], allTabs(a))
	}

	locked := a.cols[0]
	locked.locked = true
	f.dropOnRail(a, nil, []string{filepath.Join(shown, "main.go")})
	railSettle(t, tt, func() bool { return len(allTabs(a)) > 1 })
	for _, t2 := range locked.tabs {
		if t2.name() == "main.go" {
			t.Errorf("opened in the locked column")
		}
	}
}
