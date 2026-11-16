package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func TestCrumbsOf(t *testing.T) {
	join := filepath.Join
	// The folders of this machine as it spells them, with backslashes on
	// Windows.
	w, u := filepath.FromSlash("/w"), filepath.FromSlash("/u")
	cases := []struct {
		path, root, home string
		want             []crumb
	}{
		{join(w, "src", "app", "main.go"), w, u,
			[]crumb{{"src", join(w, "src")}, {"app", join(w, "src", "app")}, {"main.go", ""}}},
		{join(w, "main.go"), w, u, []crumb{{"main.go", ""}}},
		{join(u, "notes", "a b", "中文.md"), w, u,
			[]crumb{{"~", u}, {"notes", join(u, "notes")}, {"a b", join(u, "notes", "a b")}, {"中文.md", ""}}},
		{join("/tmp", "Mayoi", "settings.json"), "", u,
			[]crumb{{"tmp", join("/tmp")}, {"Mayoi", join("/tmp", "Mayoi")}, {"settings.json", ""}}},
	}
	cases = append(cases, []struct {
		path, root, home string
		want             []crumb
	}{
		{"/ssh:box/home/me/x.go", w, u,
			[]crumb{{"box", "/ssh:box"}, {"home", "/ssh:box/home"}, {"me", "/ssh:box/home/me"}, {"x.go", ""}}},
		{"/ssh:box/srv/app/x.go", "/ssh:box/srv", u,
			[]crumb{{"app", "/ssh:box/srv/app"}, {"x.go", ""}}},
		{"/wsl:Ubuntu/home/me/app/x.go", "/wsl:Ubuntu/home/me", u,
			[]crumb{{"app", "/wsl:Ubuntu/home/me/app"}, {"x.go", ""}}},
		{"/wsl:Ubuntu/home/me/x.go", w, u,
			[]crumb{{"Ubuntu", "/wsl:Ubuntu"}, {"home", "/wsl:Ubuntu/home"}, {"me", "/wsl:Ubuntu/home/me"}, {"x.go", ""}}},
	}...)
	for _, c := range cases {
		if got := crumbsOf(c.path, c.root, c.home); !slices.Equal(got, c.want) {
			t.Errorf("crumbsOf(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

// crumbsHarness opens internal/a/a.go with the Explorer hidden, so that
// the breadcrumbs' names are the only ones on screen.
func crumbsHarness(t *testing.T) *harness {
	hs := newHarness(t, false)
	hs.a.openFile(hs.path("internal/a/a.go"), true)
	hs.settle()
	hs.a.run("view.sidebar")
	hs.settle()
	return hs
}

// A folder of the breadcrumbs shows its files in a box: its folders open
// in it, and a file chosen opens and closes the box, leaving the Explorer
// as it was.
func TestCrumbsBrowseAndOpen(t *testing.T) {
	hs := crumbsHarness(t)
	a, tt := hs.a, hs.tt
	explorerOpen := slices.Sorted(a.files.Open.All())
	if err := tt.Click("internal"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.crumbs.open || a.crumbs.dir != hs.path("internal") {
		t.Fatalf("no box for internal: %+v", a.crumbs.open)
	}
	if !tt.HasText("a") || !tt.HasText("b") {
		t.Fatalf("the box does not list a and b: %v", tt.Texts())
	}
	// The folder on the way to the file is chosen, and has the keyboard.
	if a.crumbs.sel < 0 || a.crumbs.tree.Item(a.crumbs.sel) != hs.path("internal/a") {
		t.Errorf("the folder of the file is not chosen: row %d", a.crumbs.sel)
	}
	if err := tt.Click("b"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !tt.HasText("b.ts") {
		t.Fatalf("b does not open in the box: %v", tt.Texts())
	}
	if err := tt.Click("b.ts"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := a.activeTab().path(); got != hs.path("internal/b/b.ts") {
		t.Fatalf("the file shown is %s", got)
	}
	if a.crumbs.open {
		t.Error("the box stays open after a file is chosen")
	}
	if got := slices.Sorted(a.files.Open.All()); !slices.Equal(got, explorerOpen) {
		t.Errorf("the Explorer's open folders changed: %v, were %v", got, explorerOpen)
	}
}

// The box closes with Escape, and with a second click on its folder.
func TestCrumbsClose(t *testing.T) {
	hs := crumbsHarness(t)
	a, tt := hs.a, hs.tt
	tt.Click("internal")
	hs.settle()
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.crumbs.open {
		t.Error("Escape leaves the box open")
	}
	tt.Click("internal")
	hs.settle()
	if !a.crumbs.open {
		t.Fatal("no box")
	}
	tt.Click("internal")
	hs.settle()
	if a.crumbs.open {
		t.Error("a second click leaves the box open")
	}
	// A read that comes back after the box closed is dropped.
	a.openCrumbs(a.column(), a.activeTab(), hs.path("web"))
	a.closeCrumbs()
	hs.settle()
	if a.crumbs.kids != nil {
		t.Error("a read for a closed box was kept")
	}
}

// The keys move in the box: Up and Down choose, Right opens the folder
// chosen, Enter shows the file chosen.
func TestCrumbsKeys(t *testing.T) {
	hs := crumbsHarness(t)
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("internal/b/b.ts"), true)
	hs.settle()
	tt.Click("internal")
	hs.settle()
	// b, on the way to the file, is chosen: Up goes to a.
	for _, k := range []ui.Key{ui.KeyUp, ui.KeyRight, ui.KeyDown} {
		tt.Key(0, k)
		hs.settle()
	}
	if a.crumbs.sel < 0 || a.crumbs.tree.Item(a.crumbs.sel) != hs.path("internal/a/a.go") {
		t.Fatalf("the keys did not reach a.go: row %d of %d", a.crumbs.sel, a.crumbs.tree.Rows())
	}
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if got := a.activeTab().path(); got != hs.path("internal/a/a.go") || a.crumbs.open {
		t.Errorf("Enter shows %s, the box open: %v", got, a.crumbs.open)
	}
}

// A file chosen goes to the column whose breadcrumbs opened the box, not
// to the one with the keyboard.
func TestCrumbsOpenInTheirColumn(t *testing.T) {
	hs := crumbsHarness(t)
	a, tt := hs.a, hs.tt
	a.run("view.splitRight")
	hs.settle()
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	if len(a.cols) != 2 || a.col != 1 {
		t.Fatalf("%d columns, the keyboard in %d", len(a.cols), a.col)
	}
	left := a.cols[0]
	a.openCrumbs(left, left.tab(), hs.path("internal"))
	hs.settle()
	tt.Click("b")
	hs.settle()
	tt.Click("b.ts")
	hs.settle()
	if left.tab().path() != hs.path("internal/b/b.ts") {
		t.Errorf("the left column shows %s", left.tab().path())
	}
	if a.cols[1].showingFile(hs.path("internal/b/b.ts")) >= 0 {
		t.Error("the file opened in the right column")
	}
}

// An empty folder and one that cannot be read say so, rather than show
// nothing, and the box goes as its column shows another file.
func TestCrumbsEmptyAndUnreadable(t *testing.T) {
	hs := crumbsHarness(t)
	a, tt := hs.a, hs.tt
	os.Mkdir(hs.path("internal/empty"), 0o755)
	tt.Click("internal")
	hs.settle()
	tt.Click("empty")
	hs.settle()
	if !tt.HasText(locales.CrumbsEmpty()) {
		t.Errorf("an empty folder does not say so: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if err := os.Chmod(hs.path("internal/a"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(hs.path("internal/a"), 0o755) })
	if _, err := os.ReadDir(hs.path("internal/a")); err == nil {
		t.Skip("the folder reads without permission")
	}
	tt.Click("a")
	hs.settle()
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool { return strings.HasPrefix(s, "Could not read a") }) {
		t.Errorf("a folder that cannot be read does not say so: %v", tt.Texts())
	}
	os.Chmod(hs.path("internal/a"), 0o755)
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	if a.crumbs.open {
		t.Error("the box stays open over another file")
	}
}
