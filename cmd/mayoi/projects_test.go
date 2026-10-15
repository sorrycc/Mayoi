package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// railFixture is a window of project tabs with a project for each folder,
// the first shown, built by a tester as the window builds it.
func railFixture(t testing.TB, dirs ...string) (*frame, []*desktop, *ui.Tester) {
	t.Helper()
	s := settings.Default()
	s.ProjectTabs = true
	m := &mayoi{s: s}
	f := &frame{m: m}
	var hs []*desktop
	for _, dir := range dirs {
		w := s.NewWindow()
		w.Folder = dir
		// The app asks a fake host for what a window does: the tests have
		// none.
		h := &desktop{m: m}
		h.a = newApp(&fakeHost{}, s, w)
		f.add(h)
		hs = append(hs, h)
	}
	f.active = hs[0]
	m.wins = slices.Clone(hs)
	tt := ui.NewTester(f.view, 1200, 760)
	tt.Frame()
	return f, hs, tt
}

// The rail has a square for each project, named by its folder, which a
// window of one project has none of.
func TestRailShowsTheProjects(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	for _, dir := range []string{root, other} {
		if _, ok := tt.Find(filepath.Base(dir)); !ok {
			t.Fatalf("no square for %s on the rail: %v", dir, tt.Texts())
		}
	}
	if !slices.Equal(f.m.s.Rail, []int{hs[0].a.window.ID, hs[1].a.window.ID}) {
		t.Errorf("rail order %v", f.m.s.Rail)
	}
	if err := tt.RightClick(filepath.Base(other)); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "Close Project") {
		t.Errorf("the square's menu: %v", menu)
	}
	tt.CloseMenu()
	f.tabs = f.tabs[:1]
	tt.Frame()
	if _, ok := tt.Find(filepath.Base(other)); ok {
		t.Error("a window of one project has a rail")
	}
}

// A project out of sight keeps its caret and where it was scrolled, and
// gets them back as it shows again, although its text area is a new one.
func TestProjectKeepsItsPlace(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	path := filepath.Join(root, "long.go")
	writeLines(t, path, 400)
	f, hs, tt := railFixture(t, root, other)
	a := hs[0].a
	a.openFile(path, true)
	tt.Frame()
	ed := a.activeEditor()
	if ed == nil {
		t.Fatal("no editor")
	}
	at := strings.Index(ed.buf.String(), "line 200")
	ed.pending = &[2]int{at, at}
	tt.Frame()
	tt.Frame()
	sel, y := ed.sel, ed.vscroll.Y
	if sel != [2]int{at, at} || y <= 0 {
		t.Fatalf("before: sel %v, scrolled %v", sel, y)
	}
	f.active = hs[1]
	tt.Frame()
	tt.Frame()
	if !ed.hidden {
		t.Fatal("the editor out of sight is not parked")
	}
	f.active = hs[0]
	tt.Frame()
	tt.Frame()
	if ed.sel != sel || ed.vscroll.Y != y {
		t.Fatalf("back: sel %v (was %v), scrolled %v (was %v)", ed.sel, sel, ed.vscroll.Y, y)
	}
}

// What is posted to a project out of sight runs, as the window draws.
func TestHiddenProjectRunsItsPosts(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	f.active = hs[1]
	tt.Frame()
	f.active = hs[0]
	tt.Frame()
	ran := false
	hs[1].a.post(func() { ran = true })
	tt.Frame()
	if !ran {
		t.Error("a hidden project's post never ran")
	}
}

// Each project keeps its own layout: what its sidebar shows, whether it
// is closed, how wide it is, and its panel's height, as another shows
// meanwhile.
func TestProjectKeepsItsLayout(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	a, b := hs[0].a, hs[1].a
	a.showSourceControl()
	a.window.SidebarWidth, a.window.PanelHeight = 320, 400
	tt.Frame()
	if !tt.HasText("SOURCE CONTROL") {
		t.Fatalf("A's sidebar: %v", tt.Texts())
	}
	f.active = hs[1]
	tt.Frame()
	tt.Frame()
	if tt.HasText("SOURCE CONTROL") || !tt.HasText("EXPLORER") || b.window.SidebarWidth == 320 || b.window.PanelHeight == 400 {
		t.Fatalf("B's sidebar: %+v, %v", b.window.Layout, tt.Texts())
	}
	b.toggleSidebar()
	f.active = hs[0]
	tt.Frame()
	tt.Frame()
	if !tt.HasText("SOURCE CONTROL") || a.window.SidebarClosed || a.window.SidebarWidth != 320 || a.window.PanelHeight != 400 {
		t.Fatalf("A's sidebar again: %+v, %v", a.window.Layout, tt.Texts())
	}
}

// A folder opens again with the layout it had as it closed.
func TestFolderKeepsItsLayout(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Default()
	s.AddRecent(root)
	w := s.NewWindow()
	w.Folder, w.Expanded = root, []string{"src"}
	w.SidebarView, w.SidebarWidth = "git", 320
	s.Forget(w)
	x := s.NewWindow()
	a := newApp(&fakeHost{folder: root}, s, x)
	a.openSession(root)
	if x.SidebarView != "git" || x.SidebarWidth != 320 {
		t.Fatalf("layout %+v", x.Layout)
	}
}

// A project of the rail not shown since the app started keeps the session
// the settings have of it as the app quits.
func TestUnshownProjectKeepsItsSession(t *testing.T) {
	s := settings.Default()
	w := s.NewWindow()
	w.Columns = []settings.Column{{Tabs: []settings.Tab{{File: "/tmp/a.go"}}, Weight: 1}}
	a := newApp(&fakeHost{}, s, w)
	a.keepSession()
	a.park()
	if len(w.Columns) != 1 || len(w.Columns[0].Tabs) != 1 {
		t.Fatalf("session lost: %+v", w.Columns)
	}
}

// With project tabs, a folder chosen opens in a tab of its own, and a
// window with no folder takes it.
func TestProjectTabsOpenFoldersInTabs(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	s := settings.Default()
	s.ProjectTabs = true
	h := &fakeHost{}
	a := newApp(h, s, s.NewWindow())
	a.switchFolder(root)
	if a.root != root || len(h.windows) != 0 {
		t.Fatalf("no folder: folder %q, windows %v", a.root, h.windows)
	}
	a.switchFolder(other)
	if a.root != root || !slices.Equal(h.windows, []string{other}) {
		t.Fatalf("with a folder: folder %q, windows %v", a.root, h.windows)
	}
}

// writeLines writes a file of n numbered lines.
func writeLines(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "// line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectWords(t *testing.T) {
	for name, want := range map[string]string{
		"mayoi": "M", "my-app": "MA", "mayoi_editor": "ME", "mayoiEditor": "ME", "go.dev": "GD", "": "?",
	} {
		if got := initials(projectWords(name)); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

// The rail of a window of project tabs, in both themes, at twice the
// scale: the project shown with unsaved changes, and another under the
// pointer.
func TestShotRail(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		var dirs []string
		for _, name := range []string{"mayoi", "my-app", "webServer", "docs", "go"} {
			dir := filepath.Join(t.TempDir(), name)
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
			dirs = append(dirs, dir)
		}
		_, hs, tt := railFixture(t, dirs...)
		tt.SetDark(dark)
		tt.SetScale(2)
		a := hs[0].a
		a.openFile(filepath.Join(dirs[0], "main.go"), true)
		for range 10 {
			tt.Frame()
		}
		a.focusColumn()
		tt.Frame()
		tt.Type("// ")
		hs[3].a.alert = "Bell from zsh"
		r, _ := tt.Find("webServer")
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		for range 30 {
			tt.Frame()
			time.Sleep(10 * time.Millisecond)
		}
		name := "rail-light"
		if dark {
			name = "rail-dark"
		}
		(&harness{t: t, a: a, tt: tt}).shot(name)
	}
}

// A square dragged along the rail goes before the one it is dropped on,
// or at the end, and the rail's order is kept.
func TestRailReorders(t *testing.T) {
	a, b, c := workspaceFixture(t), workspaceFixture(t), workspaceFixture(t)
	f, hs, _ := railFixture(t, a, b, c)
	ids := func() []int {
		var out []int
		for _, h := range f.tabs {
			out = append(out, h.a.window.ID)
		}
		return out
	}
	id := func(i int) int { return hs[i].a.window.ID }
	f.move(hs[2], hs[0])
	if got := ids(); !slices.Equal(got, []int{id(2), id(0), id(1)}) {
		t.Fatalf("after moving the last first: %v", got)
	}
	f.move(hs[2], nil)
	if got := ids(); !slices.Equal(got, []int{id(0), id(1), id(2)}) {
		t.Fatalf("after moving it to the end: %v", got)
	}
	if !slices.Equal(f.m.s.Rail, ids()) {
		t.Errorf("rail kept as %v, shown as %v", f.m.s.Rail, ids())
	}
}

// The square's menu reveals the folder, copies its path and closes the
// project or the others.
func TestRailMenu(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	_, _, tt := railFixture(t, root, other)
	if err := tt.RightClick(filepath.Base(other)); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	for _, want := range []string{revealLabel(), "Copy Path", "Close Project", "Close Other Projects"} {
		if !slices.Contains(menu, want) {
			t.Errorf("no %q in the square's menu: %v", want, menu)
		}
	}
	if err := tt.ChooseMenuItem("Copy Path"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != other {
		t.Errorf("copied %q, want %q", got, other)
	}
}

// BenchmarkRailFrame is a frame of a window of twelve projects, drawn
// with nothing changing, as the rail is built every frame.
func BenchmarkRailFrame(b *testing.B) {
	var dirs []string
	for range 12 {
		dirs = append(dirs, workspaceFixture(b))
	}
	_, _, tt := railFixture(b, dirs...)
	for range 30 {
		tt.Frame()
	}
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
}

// BenchmarkProjectSwitch is the frame that shows another project, each
// with a long Go file open, as a click on the rail does.
func BenchmarkProjectSwitch(b *testing.B) {
	src, _ := os.ReadFile("editor.go")
	var dirs []string
	for range 2 {
		dir := workspaceFixture(b)
		os.WriteFile(filepath.Join(dir, "editor.go"), src, 0o644)
		dirs = append(dirs, dir)
	}
	f, hs, tt := railFixture(b, dirs...)
	for i, h := range hs {
		f.active = h
		h.a.openFile(filepath.Join(dirs[i], "editor.go"), true)
		for range 30 {
			tt.Frame()
			time.Sleep(5 * time.Millisecond)
		}
	}
	b.ResetTimer()
	for i := range b.N {
		f.active = hs[i%2]
		tt.Frame()
	}
}

// A terminal's bell in a project out of sight marks its square, in its
// tip too, until the project shows.
func TestRailMarksAttention(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	// Shown once, as a project with a terminal was.
	f.active = hs[1]
	tt.Frame()
	f.active = hs[0]
	tt.Frame()
	hs[1].a.post(func() { hs[1].a.alert = "Bell from zsh" })
	tt.Frame()
	if hs[1].a.alert == "" {
		t.Fatal("the mark of a project out of sight went")
	}
	r, _ := tt.Find(filepath.Base(other))
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	for range 5 {
		tt.Frame()
		time.Sleep(150 * time.Millisecond)
	}
	if !tt.HasText("Bell from zsh") {
		t.Errorf("the tip does not say why: %v", tt.Texts())
	}
	f.active = hs[1]
	tt.Frame()
	if hs[1].a.alert != "" {
		t.Error("the mark stays as the project shows")
	}
}

// findProjectLogo takes the first favicon or logo file, the root before
// public/, and app/ before src/, but no icon alone that does not decode.
func TestFindProjectLogo(t *testing.T) {
	write := func(dir, rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()
	if got := findProjectLogo(empty); got != "" {
		t.Errorf("empty folder: %q", got)
	}
	dir := t.TempDir()
	write(dir, "public/logo.png")
	write(dir, "src/assets/logo.svg")
	if got, want := findProjectLogo(dir), filepath.Join(dir, "public", "logo.png"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	write(dir, "favicon.svg")
	if got, want := findProjectLogo(dir), filepath.Join(dir, "favicon.svg"); got != want {
		t.Errorf("root first: got %q, want %q", got, want)
	}
}

// extractIconHref takes the icon a source declares: link tags in any
// attribute order, or icon metadata, skipping data URIs and remote URLs.
func TestExtractIconHref(t *testing.T) {
	for source, want := range map[string]string{
		`<link rel="icon" href="/brand/mark.svg">`:                             "brand/mark.svg",
		`<link href="icons/a.png?v=2" rel="shortcut icon">`:                    "icons/a.png",
		`<link REL='APPLE-TOUCH-ICON' HREF='/apple.png'>`:                      "apple.png",
		`<link rel="stylesheet" href="app.css"><link rel="icon" href="f.svg">`: "f.svg",
		`{ rel: "icon", href: "/icons/root.png" }`:                             "icons/root.png",
		`{ rel: "icon", url: "/images/icons/favicon-v2.ico" }`:                 "images/icons/favicon-v2.ico",
		`icons: { icon: "/icon.png", apple: "/apple.png" }`:                    "icon.png",
		`export const m = { apple: "/ap.png" }`:                                "ap.png",
		`<link data-href="/wrong.png" href="/r.png" rel="icon">`:               "r.png",
		`<link rel="mask-icon" href="m.svg">`:                                  "",
		`<link rel="icon" href="../up.png">`:                                   "",
		`<link rel="icon" href="//host/x.png">`:                                "",
		`<link rel="icon" href="data:image/svg+xml,xxx">`:                      "",
		`<link rel="icon" href="https://example.com/f.png">`:                   "",
		`<link rel="stylesheet" href="app.css">`:                               "",
		`no links here`:                                                        "",
	} {
		if got := extractIconHref(source); got != want {
			t.Errorf("%q: got %q, want %q", source, got, want)
		}
	}
}

// resolveIconHref looks in public/ first, as a leading slash names the
// static folder.
func TestResolveIconHref(t *testing.T) {
	if got, want := resolveIconHref("/brand/mark.svg"), []string{"public/brand/mark.svg", "brand/mark.svg"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if resolveIconHref("data:image/png,xxx") != nil {
		t.Error("a data URI resolves")
	}
}

// An icon no candidate names is found through the sources declaring it.
func TestLoadProjectLogoFromSource(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.html")
	if err := os.WriteFile(p, []byte(`<link rel="icon" href="/brand/mark.svg">`), 0o644); err != nil {
		t.Fatal(err)
	}
	if findProjectLogo(dir) != "" {
		t.Fatal("a declared icon is a candidate")
	}
	if loadProjectLogo(dir) != nil {
		t.Fatal("a logo without its file")
	}
	mark := filepath.Join(dir, "public", "brand", "mark.svg")
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte(testSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from the declared icon")
	}
}

// An icon in a non-standard folder is found through Next.js metadata in
// app/layout.tsx.
func TestLoadProjectLogoFromLayout(t *testing.T) {
	dir := t.TempDir()
	layout := filepath.Join(dir, "app", "layout.tsx")
	if err := os.MkdirAll(filepath.Dir(layout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout, []byte("export const metadata = {\n  icons: {\n    icon: [\n      {\n        rel: \"icon\",\n        type: \"image/x-icon\",\n        url: \"/images/icons/favicon-v2.ico\",\n      },\n    ],\n    apple: \"/images/icons/apple-touch-icon-v2.webp\",\n  },\n};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(dir) != nil {
		t.Fatal("a logo without its file")
	}
	icons := filepath.Join(dir, "public", "images", "icons", "favicon-v2.ico")
	if err := os.MkdirAll(filepath.Dir(icons), 0o755); err != nil {
		t.Fatal(err)
	}
	writeICO(t, icons, pngBytes(t, 16, 16))
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from the metadata-declared icon")
	}
}

// An apple-only declaration resolves through its own key.
func TestLoadProjectLogoFromApple(t *testing.T) {
	dir := t.TempDir()
	layout := filepath.Join(dir, "app", "layout.tsx")
	if err := os.MkdirAll(filepath.Dir(layout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout, []byte("export const metadata = {\n  icons: {\n    apple: \"/icons/ap.png\",\n  },\n};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	icon := filepath.Join(dir, "public", "icons", "ap.png")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, icon, 16, 16)
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from the apple declaration")
	}
}

// The Next.js file convention under src/app/ resolves: icon.ico,
// apple-icon.png.
func TestFindProjectLogoSrcApp(t *testing.T) {
	dir := t.TempDir()
	icon := filepath.Join(dir, "src", "app", "icon.ico")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	writeICO(t, icon, pngBytes(t, 16, 16))
	if got := findProjectLogo(dir); got != icon {
		t.Errorf("got %q, want %q", got, icon)
	}
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from src/app/icon.ico")
	}
	apple := filepath.Join(dir, "src", "app", "apple-icon.png")
	if err := os.MkdirAll(filepath.Dir(apple), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, apple, 16, 16)
	if err := os.Remove(icon); err != nil {
		t.Fatal(err)
	}
	if got := findProjectLogo(dir); got != apple {
		t.Errorf("got %q, want %q", got, apple)
	}
}

// loadProjectLogo decodes a project's png or svg logo, and takes nothing
// for a broken file.
func TestLoadProjectLogo(t *testing.T) {
	dir := t.TempDir()
	logo := filepath.Join(dir, "public", "logo.png")
	if err := os.MkdirAll(filepath.Dir(logo), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, logo, 32, 32)
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from public/logo.png")
	}
	svg := t.TempDir()
	p := filepath.Join(svg, "public", "favicon.svg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(testSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(svg) == nil {
		t.Error("no logo from public/favicon.svg")
	}
	broken := t.TempDir()
	bp := filepath.Join(broken, "public", "logo.png")
	if err := os.MkdirAll(filepath.Dir(bp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bp, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(broken) != nil {
		t.Error("a logo from a broken png")
	}
}

// A large logo is kept no larger than the rail needs, its shape kept.
func TestLoadProjectLogoScalesDown(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "logo.png"), 1024, 512)
	bm, ok := loadProjectLogo(dir).(*ui.Bitmap)
	if !ok {
		t.Fatal("no bitmap from logo.png")
	}
	if w, h := bm.Size(); w != logoSide || h != logoSide/2 {
		t.Errorf("logo is %dx%d, want %dx%d", w, h, logoSide, logoSide/2)
	}
}

// A favicon.ico decodes: its PNG entry, or its 32-bit BMP one.
func TestLoadProjectLogoICO(t *testing.T) {
	dir := t.TempDir()
	ico := filepath.Join(dir, "favicon.ico")
	writeICO(t, ico, pngBytes(t, 16, 16))
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from favicon.ico with a PNG entry")
	}
	bmp := t.TempDir()
	writeICOBMP(t, filepath.Join(bmp, "favicon.ico"))
	if loadProjectLogo(bmp) == nil {
		t.Error("no logo from favicon.ico with a BMP entry")
	}
	if err := os.WriteFile(filepath.Join(bmp, "favicon.ico"), []byte("not an ico"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(bmp) != nil {
		t.Error("a logo from a broken ico")
	}
}

// Project icons are off by default, leaving the initials; on, a decoded
// logo is what the rail shows.
func TestProjectIcons(t *testing.T) {
	if settings.Default().ProjectIcons {
		t.Error("project icons are on by default")
	}
	root := t.TempDir()
	f, hs, _ := railFixture(t, root)
	h := hs[0]
	if h.projectLogo() != nil {
		t.Error("no logo scanned, yet one shows")
	}
	h.logoSrc = loadProjectLogo(t.TempDir())
	if h.projectLogo() != nil {
		t.Error("project icons off, yet a logo shows")
	}
	f.m.s.ProjectIcons = true
	logo := filepath.Join(root, "public", "logo.png")
	if err := os.MkdirAll(filepath.Dir(logo), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, logo, 32, 32)
	h.logoSrc = loadProjectLogo(root)
	if h.projectLogo() == nil {
		t.Error("project icons on with a logo, yet the initials show")
	}
}

// A rail with a project's logo builds, as one without does.
func TestRailShowsProjectLogo(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	logo := filepath.Join(root, "public", "logo.png")
	if err := os.MkdirAll(filepath.Dir(logo), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, logo, 32, 32)
	f, hs, tt := railFixture(t, root, other)
	f.m.s.ProjectIcons = true
	hs[0].logoDone, hs[0].logoDir = true, root
	hs[0].logoSrc = loadProjectLogo(root)
	tt.Frame()
	if _, ok := tt.Find(filepath.Base(root)); !ok {
		t.Fatalf("no square for %s on the rail: %v", root, tt.Texts())
	}
}

// decodeDIB keeps the channels and the rows the right way up, top-down or
// bottom-up, and a broken larger entry falls back to a good one.
func TestDecodeICO(t *testing.T) {
	// Top-left red, top-right green, bottom-left blue, bottom-right white.
	want := []color.NRGBA{
		{0xff, 0, 0, 0xff}, {0, 0xff, 0, 0xff},
		{0, 0, 0xff, 0xff}, {0xff, 0xff, 0xff, 0xff},
	}
	dir := t.TempDir()
	bottom := filepath.Join(dir, "bottom.ico")
	writeICOBMP(t, bottom)
	if got := icoPixels(t, bottom); !slices.Equal(got, want) {
		t.Errorf("bottom-up: %v", got)
	}
	for _, doubled := range []bool{false, true} {
		top := filepath.Join(dir, "top.ico")
		writeICOTopDown(t, top, doubled)
		if got := icoPixels(t, top); !slices.Equal(got, want) {
			t.Errorf("top-down doubled=%v: %v", doubled, got)
		}
		os.Remove(top)
	}
	fallback := filepath.Join(dir, "fallback.ico")
	writeICOFallback(t, fallback, pngBytes(t, 16, 16))
	data, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeICO(data)
	if err != nil {
		t.Fatalf("no fallback: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("fallback size %v, want 16×16", b)
	}
}

// A lying DIB header errors instead of panicking or allocating.
func TestDecodeDIBLyingHeader(t *testing.T) {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(40))
	binary.Write(&b, binary.LittleEndian, int32(0x7fffffff))
	binary.Write(&b, binary.LittleEndian, int32(-2147483648))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint32(0))
	if _, err := decodeDIB(b.Bytes()); err == nil {
		t.Error("no error for a lying header")
	}
	// A PNG entry lying about 8000×8000 is skipped, not decoded.
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	ico.Write([]byte{32, 32, 0, 0})
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(32))
	ihdr := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 25)...)
	binary.BigEndian.PutUint32(ihdr[16:20], 8000)
	binary.BigEndian.PutUint32(ihdr[20:24], 8000)
	binary.Write(&ico, binary.LittleEndian, uint32(len(ihdr)))
	binary.Write(&ico, binary.LittleEndian, uint32(6+16))
	ico.Write(ihdr)
	if _, err := decodeICO(ico.Bytes()); err == nil {
		t.Error("a bomb PNG entry decoded")
	}
}

// pngBytes encodes a w×h red png.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{0xff, 0, 0, 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// writeICO writes an ico of one entry holding data.
func writeICO(t *testing.T, path string, data []byte) {
	t.Helper()
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint16(0))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	b.Write([]byte{32, 32, 0, 0})
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	binary.Write(&b, binary.LittleEndian, uint32(6+16))
	b.Write(data)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeICOBMP writes an ico of one 2×2 32-bit BMP entry.
func writeICOBMP(t *testing.T, path string) {
	t.Helper()
	var dib bytes.Buffer
	binary.Write(&dib, binary.LittleEndian, uint32(40))
	binary.Write(&dib, binary.LittleEndian, int32(2))
	binary.Write(&dib, binary.LittleEndian, int32(4))
	binary.Write(&dib, binary.LittleEndian, uint16(1))
	binary.Write(&dib, binary.LittleEndian, uint16(32))
	binary.Write(&dib, binary.LittleEndian, uint32(0))
	dib.Write(make([]byte, 20))
	// Bottom row first: blue, white / red, green.
	dib.Write([]byte{0xff, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff})
	dib.Write([]byte{0, 0, 0xff, 0xff, 0, 0xff, 0, 0xff})
	writeICO(t, path, dib.Bytes())
}

// writeICOTopDown writes an ico of one 2×2 top-down BMP entry, the height
// doubled or not.
func writeICOTopDown(t *testing.T, path string, doubled bool) {
	t.Helper()
	var dib bytes.Buffer
	binary.Write(&dib, binary.LittleEndian, uint32(40))
	binary.Write(&dib, binary.LittleEndian, int32(2))
	if doubled {
		binary.Write(&dib, binary.LittleEndian, int32(-4))
	} else {
		binary.Write(&dib, binary.LittleEndian, int32(-2))
	}
	binary.Write(&dib, binary.LittleEndian, uint16(1))
	binary.Write(&dib, binary.LittleEndian, uint16(32))
	binary.Write(&dib, binary.LittleEndian, uint32(0))
	dib.Write(make([]byte, 20))
	// Top row first: red, green / blue, white.
	dib.Write([]byte{0, 0, 0xff, 0xff, 0, 0xff, 0, 0xff})
	dib.Write([]byte{0xff, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff})
	writeICO(t, path, dib.Bytes())
}

// writeICOFallback writes an ico whose larger entry is truncated PNG and
// whose smaller one decodes.
func writeICOFallback(t *testing.T, path string, good []byte) {
	t.Helper()
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint16(0))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	bad := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 2048)...)
	b.Write([]byte{32, 32, 0, 0})
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint32(len(bad)))
	binary.Write(&b, binary.LittleEndian, uint32(6+32))
	b.Write([]byte{16, 16, 0, 0})
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint32(len(good)))
	binary.Write(&b, binary.LittleEndian, uint32(6+32+len(bad)))
	b.Write(bad)
	b.Write(good)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// icoPixels decodes path and returns its pixels row by row.
func icoPixels(t *testing.T, path string) []color.NRGBA {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeICO(data)
	if err != nil {
		t.Fatal(err)
	}
	var out []color.NRGBA
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			out = append(out, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	return out
}

// A logo that does not decode leaves the next candidate to try.
func TestLoadProjectLogoSkipsBroken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("not an svg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(dir) != nil {
		t.Fatal("a logo from a broken svg")
	}
	writePNG(t, filepath.Join(dir, "favicon.png"), 16, 16)
	if loadProjectLogo(dir) == nil {
		t.Error("the png after a broken svg is not tried")
	}
}

// A scan for another folder drops the logo of the folder before.
func TestScanProjectLogoDropsOldLogo(t *testing.T) {
	root := t.TempDir()
	f, hs, _ := railFixture(t, root)
	h := hs[0]
	writePNG(t, filepath.Join(root, "logo.png"), 16, 16)
	h.logoSrc = loadProjectLogo(root)
	if h.logoSrc == nil {
		t.Fatal("no logo to drop")
	}
	f.scanProjectLogo(h.a, h, t.TempDir())
	if h.logoSrc != nil {
		t.Error("the old folder's logo shows while the new folder is scanned")
	}
}

// The rail looks for the logo of every project, one not yet shown too.
func TestRailScansProjectsNotShown(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	writePNG(t, filepath.Join(other, "logo.png"), 16, 16)
	f, hs, tt := railFixture(t, root, other)
	if hs[1].a.started {
		t.Fatal("the second project started")
	}
	f.m.s.ProjectIcons = true
	tt.Frame()
	if !hs[1].logoDone || hs[1].logoDir != other {
		t.Fatal("no scan for a project not shown")
	}
	for end := time.Now().Add(5 * time.Second); hs[1].logoSrc == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the logo of a project not shown never came")
		}
		tt.Frame()
	}
}
