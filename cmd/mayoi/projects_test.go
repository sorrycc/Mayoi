package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
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
		t.Cleanup(h.a.shutdown)
		f.add(h)
		hs = append(hs, h)
	}
	f.active = hs[0]
	m.wins = slices.Clone(hs)
	tt := ui.NewTester(f.view, 1200, 760)
	step(tt)
	return f, hs, tt
}

// The rail has a square for each project, named by its folder.
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
}

// A window of one project keeps the rail, with its square and the button
// that opens another folder, while Project Tabs is on, and has none off.
func TestRailWithOneProject(t *testing.T) {
	root := workspaceFixture(t)
	f, _, tt := railFixture(t, root)
	if _, ok := tt.Find(filepath.Base(root)); !ok {
		t.Errorf("no square for %s on the rail: %v", root, tt.Texts())
	}
	if _, ok := tt.Find(locales.CmdFileOpenFolder()); !ok {
		t.Errorf("no button that opens another folder on the rail: %v", tt.Texts())
	}
	f.m.s.ProjectTabs = false
	step(tt)
	if _, ok := tt.Find(locales.CmdFileOpenFolder()); ok {
		t.Error("the rail is still there with Project Tabs off")
	}
}

// The button at the foot of the rail shows the projects wide, with their
// names, and back as a bar of squares.
func TestRailWide(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, _, tt := railFixture(t, root, other)
	if f.m.s.RailWide {
		t.Fatal("the rail opens wide")
	}
	if err := tt.Click(locales.RailShowNames()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if !f.m.s.RailWide {
		t.Fatal("the button did not show the rail wide")
	}
	want := clamp(f.m.s.RailWidth, settings.RailWideMin, settings.RailWideMax)
	if got := f.active.a.railWidth; got != want || got <= railW {
		t.Fatalf("the wide rail is %v wide, want %v", got, want)
	}
	if _, ok := tt.Find(filepath.Base(other)); !ok {
		t.Errorf("no project on the wide rail: %v", tt.Texts())
	}
	if err := tt.Click(locales.RailHideNames()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if f.m.s.RailWide || f.active.a.railWidth != railW {
		t.Fatalf("the rail is %v wide, wide=%v", f.active.a.railWidth, f.m.s.RailWide)
	}
}

// The wide rail is dragged by its edge, keeps its least width while the
// pointer goes narrower still, and is the bar once it is let go there.
func TestRailWideDropsToTheBar(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, _, tt := railFixture(t, root, other)
	f.setRailWide(true)
	step(tt)
	w := f.active.a.railWidth
	tt.Press(w-2, 300)
	tt.Move(w+58, 300)
	tt.Release(w+58, 300)
	step(tt)
	if !f.m.s.RailWide || f.m.s.RailWidth < w+50 || f.m.s.RailWidth > w+70 {
		t.Fatalf("the rail after dragging right is %v wide, wide=%v", f.m.s.RailWidth, f.m.s.RailWide)
	}
	w = f.active.a.railWidth
	tt.Press(w-2, 300)
	step(tt)
	x := settings.RailWideMin / 2
	tt.Move(x, 300)
	step(tt)
	if !f.m.s.RailWide || f.active.a.railWidth != settings.RailWideMin {
		t.Fatalf("the rail while dragged below its least is %v wide, wide=%v", f.active.a.railWidth, f.m.s.RailWide)
	}
	tt.Release(x, 300)
	step(tt)
	step(tt)
	if f.m.s.RailWide || f.active.a.railWidth != railW {
		t.Fatalf("the rail after letting go is %v wide, wide=%v", f.active.a.railWidth, f.m.s.RailWide)
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
	step(tt)
	ed := a.activeEditor()
	if ed == nil {
		t.Fatal("no editor")
	}
	at := strings.Index(ed.buf.String(), "line 200")
	ed.pending = &[2]int{at, at}
	step(tt)
	step(tt)
	sel, y := ed.sel, ed.vscroll.Y
	if sel != [2]int{at, at} || y <= 0 {
		t.Fatalf("before: sel %v, scrolled %v", sel, y)
	}
	f.active = hs[1]
	step(tt)
	step(tt)
	if !ed.hidden {
		t.Fatal("the editor out of sight is not parked")
	}
	f.active = hs[0]
	step(tt)
	step(tt)
	if ed.sel != sel || ed.vscroll.Y != y {
		t.Fatalf("back: sel %v (was %v), scrolled %v (was %v)", ed.sel, sel, ed.vscroll.Y, y)
	}
}

// What is posted to a project out of sight runs, as the window draws.
func TestHiddenProjectRunsItsPosts(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	f.active = hs[1]
	step(tt)
	f.active = hs[0]
	step(tt)
	ran := false
	hs[1].a.post(func() { ran = true })
	step(tt)
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
	step(tt)
	if !tt.HasText("SOURCE CONTROL") {
		t.Fatalf("A's sidebar: %v", tt.Texts())
	}
	f.active = hs[1]
	step(tt)
	step(tt)
	if tt.HasText("SOURCE CONTROL") || !tt.HasText("EXPLORER") || b.window.SidebarWidth == 320 || b.window.PanelHeight == 400 {
		t.Fatalf("B's sidebar: %+v, %v", b.window.Layout, tt.Texts())
	}
	b.toggleSidebar()
	f.active = hs[0]
	step(tt)
	step(tt)
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
	t.Cleanup(a.shutdown)
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
	t.Cleanup(a.shutdown)
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
	t.Cleanup(a.shutdown)
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

// The rail tells projects apart by their labels where their initials
// are the same, and labels names the same alike.
func TestRailLabels(t *testing.T) {
	for _, tc := range []struct{ names, want []string }{
		{[]string{"mayoi", "my-app", "go"}, []string{"Ma", "MA", "Go"}},
		{[]string{"Downloads", "Documents", "Desktop"}, []string{"Dw", "Dc", "De"}},
		{[]string{"Downloads", "Downloads", "Desktop"}, []string{"Do", "Do", "De"}},
		{[]string{"Downloads", "Documents", "Downloads"}, []string{"Dw", "Dc", "Dw"}},
		{[]string{"app", "apps"}, []string{"Ap", "As"}},
		{[]string{"my-app", "my-api"}, []string{"Mp", "Mi"}},
		{[]string{"x", "", "下载"}, []string{"X", "?", "下载"}},
	} {
		if got := railLabels(tc.names); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.names, got, tc.want)
		}
	}
}

// A project's own label is up to two characters as a person sees them.
func TestRailOwnLabel(t *testing.T) {
	for s, want := range map[string]string{
		"": "", " DL ": "DL", "abc": "ab", "🚀x": "🚀x", "👍🏽👋🏻z": "👍🏽👋🏻",
		"🇯🇵🇨🇳🇺🇸": "🇯🇵🇨🇳", "👩‍💻!": "👩‍💻!", "é1": "é1",
	} {
		if got := railLabel(s); got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}

// A project's own label and color are kept by its folder, and forgotten
// once both are its own again.
func TestRailTabStyle(t *testing.T) {
	dir := workspaceFixture(t)
	f, _, _ := railFixture(t, dir, workspaceFixture(t))
	f.setTabStyle(dir, func(st *settings.TabStyle) { st.Label = "DL" })
	f.setTabStyle(dir, func(st *settings.TabStyle) { st.Color = "cyan" })
	if got := f.m.s.ProjectTabStyle[dir]; got != (settings.TabStyle{Label: "DL", Color: "cyan"}) {
		t.Fatalf("style: %+v", got)
	}
	if _, ok := railColor(darkPalette(), "cyan"); !ok {
		t.Error("cyan is not a color")
	}
	if _, ok := railColor(darkPalette(), "plaid"); ok {
		t.Error("plaid is a color")
	}
	f.setTabStyle(dir, func(st *settings.TabStyle) { *st = settings.TabStyle{} })
	if f.m.s.ProjectTabStyle != nil {
		t.Fatalf("styles left: %v", f.m.s.ProjectTabStyle)
	}
}

// tinyWebP is a lossless WebP of one pixel.
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

// A picture chosen for a project is copied beside the settings under a
// name of its bytes, an SVG as it is, and what does not decode is not.
func TestImportProjectIcon(t *testing.T) {
	src, icons := t.TempDir(), filepath.Join(t.TempDir(), "project-icons")
	writePNG(t, filepath.Join(src, "a.png"), 16, 16)
	data, _ := os.ReadFile(filepath.Join(src, "a.png"))
	os.WriteFile(filepath.Join(src, "b.jpeg.png"), data, 0o644)
	os.WriteFile(filepath.Join(src, "logo.svg"), []byte(testSVG), 0o644)
	webp, _ := base64.StdEncoding.DecodeString(tinyWebP)
	os.WriteFile(filepath.Join(src, "tiny.webp"), webp, 0o644)
	os.WriteFile(filepath.Join(src, "fake.png"), []byte("not a picture"), 0o644)
	os.WriteFile(filepath.Join(src, "notes.txt"), data, 0o644)

	name, err := importProjectIcon(icons, filepath.Join(src, "a.png"))
	if err != nil || !strings.HasSuffix(name, ".png") {
		t.Fatalf("png: %q, %v", name, err)
	}
	if again, _ := importProjectIcon(icons, filepath.Join(src, "b.jpeg.png")); again != name {
		t.Errorf("the same picture kept twice: %q and %q", name, again)
	}
	svg, err := importProjectIcon(icons, filepath.Join(src, "logo.svg"))
	if err != nil || !strings.HasSuffix(svg, ".svg") {
		t.Fatalf("svg: %q, %v", svg, err)
	}
	if got, _ := os.ReadFile(filepath.Join(icons, svg)); string(got) != testSVG {
		t.Errorf("svg copied as %q", got)
	}
	if w, err := importProjectIcon(icons, filepath.Join(src, "tiny.webp")); err != nil || decodeProjectLogo(filepath.Join(icons, w)) == nil {
		t.Errorf("webp: %q, %v", w, err)
	}
	for _, bad := range []string{"fake.png", "notes.txt", "missing.png"} {
		if name, err := importProjectIcon(icons, filepath.Join(src, bad)); err == nil {
			t.Errorf("%s imported as %q", bad, name)
		}
	}
	entries, _ := os.ReadDir(icons)
	if len(entries) != 3 {
		t.Errorf("%d files kept, want 3", len(entries))
	}
	for _, p := range []string{"a.PNG", "x.jpg", "x.jpeg", "x.webp", "x.svg", "x.ico", "x.icns"} {
		if !isIconFile(p) {
			t.Errorf("%s is not an icon file", p)
		}
	}
	for _, p := range []string{"x.gif", "x.txt", "dir"} {
		if isIconFile(p) {
			t.Errorf("%s is an icon file", p)
		}
	}
}

// settleIcon draws frames until the project of dir shows an icon of its
// own, as the copy of it ends in the background.
func settleIcon(t *testing.T, f *frame, tt *ui.Tester, dir string) string {
	t.Helper()
	for range 200 {
		step(tt)
		if icon := f.m.s.ProjectTabStyle[dir].Icon; icon != "" {
			return icon
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no icon set")
	return ""
}

// A picture dropped on a project's square shows in place of its logo and
// label, a label given after takes its place, and the copy is deleted
// once no project shows it.
func TestRailOwnIcon(t *testing.T) {
	dir, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, dir, other)
	f.m.icons = filepath.Join(t.TempDir(), "project-icons")
	a := hs[0].a
	pic := filepath.Join(t.TempDir(), "pic.png")
	writePNG(t, pic, 16, 16)

	f.setTabStyle(dir, func(st *settings.TabStyle) { st.Label, st.Color = "DL", "cyan" })
	f.setProjectIcon(a, dir, pic)
	icon := settleIcon(t, f, tt, dir)
	if got := f.m.s.ProjectTabStyle[dir]; got != (settings.TabStyle{Color: "cyan", Icon: icon}) {
		t.Fatalf("style after the icon: %+v", got)
	}
	path := f.m.iconPath(icon)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	// The square shows it.
	for range 200 {
		step(tt)
		if hs[0].logoSrc != nil && hs[0].logoIcon == path {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if hs[0].logoIcon != path || hs[0].logoSrc == nil {
		t.Fatalf("the square shows %q, %v", hs[0].logoIcon, hs[0].logoSrc)
	}

	// Another project shows the same picture: the copy is shared.
	f.setProjectIcon(a, other, pic)
	if got := settleIcon(t, f, tt, other); got != icon {
		t.Fatalf("other's icon %q, want %q", got, icon)
	}
	f.askRailLabel(a, dir, filepath.Base(dir))
	if msg := a.prompt.done("🚀"); msg != "" {
		t.Fatal(msg)
	}
	if got := f.m.s.ProjectTabStyle[dir]; got != (settings.TabStyle{Label: "🚀", Color: "cyan"}) {
		t.Fatalf("style after the label: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the copy other shows is gone: %v", err)
	}

	// Reset Icon on the last project that shows it deletes the copy.
	if err := tt.RightClick(filepath.Base(other)); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Reset Icon"); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if f.m.s.ProjectTabStyle[other].Icon != "" {
		t.Error("other's icon is left")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the copy is left: %v", err)
	}
	// Without an icon, Reset Icon is disabled.
	tt.RightClick(filepath.Base(other))
	if err := tt.ChooseMenuItem("Reset Icon"); err == nil {
		t.Error("Reset Icon without an icon")
	}

	// A picture that does not decode leaves the square as it was.
	bad := filepath.Join(t.TempDir(), "bad.png")
	os.WriteFile(bad, []byte("nope"), 0o644)
	fails := a.fails
	f.setProjectIcon(a, other, bad)
	for range 200 {
		step(tt)
		if a.fails > fails {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.fails == fails || f.m.s.ProjectTabStyle[other].Icon != "" {
		t.Errorf("a bad picture: %d fails, style %+v", a.fails-fails, f.m.s.ProjectTabStyle[other])
	}
}

// Without settings, no project takes an icon.
func TestRailOwnIconWithoutSettings(t *testing.T) {
	dir := workspaceFixture(t)
	f, hs, tt := railFixture(t, dir)
	pic := filepath.Join(t.TempDir(), "pic.png")
	writePNG(t, pic, 16, 16)
	f.setProjectIcon(hs[0].a, dir, pic)
	for range 20 {
		step(tt)
		time.Sleep(2 * time.Millisecond)
	}
	if f.m.s.ProjectTabStyle != nil {
		t.Errorf("styles: %v", f.m.s.ProjectTabStyle)
	}
	if f.canSetIcon(dir) || f.canSetIcon("") {
		t.Error("an icon may be set")
	}
}

// The rail of a window of project tabs, in both themes, at twice the
// scale: the project shown with unsaved changes, and another under the
// pointer, two folders named alike, one of them on a host, and a project
// with a label and color of its own.
func TestShotRail(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		var dirs []string
		for _, name := range []string{"mayoi", "my-app", "webServer", "Downloads", "Documents", "Downloads"} {
			dir := filepath.Join(t.TempDir(), name)
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
			dirs = append(dirs, dir)
		}
		_, hs, tt := railFixture(t, dirs...)
		tt.SetDark(dark)
		tt.SetScale(2)
		a := hs[0].a
		hs[5].a.root = remote.Path("vps", "/home/me/Downloads")
		hs[1].f.setTabStyle(dirs[1], func(st *settings.TabStyle) { *st = settings.TabStyle{Label: "🚀", Color: "cyan"} })
		f := hs[0].f
		f.m.icons = t.TempDir()
		pic := filepath.Join(t.TempDir(), "pic.svg")
		os.WriteFile(pic, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><circle cx="16" cy="16" r="14" fill="#e5484d"/><path d="M10 17l4 4 8-9" stroke="#fff" stroke-width="3" fill="none"/></svg>`), 0o644)
		f.setProjectIcon(a, dirs[2], pic)
		a.openFile(filepath.Join(dirs[0], "main.go"), true)
		for range 10 {
			step(tt)
		}
		a.focusColumn()
		step(tt)
		tt.Type("// ")
		hs[3].a.alert = "Bell from zsh"
		r, _ := tt.Find("webServer")
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		for range 30 {
			step(tt)
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

// railDragTo drags the square of from with the pointer to (x, y), as a
// person does, and lets the move it posts run.
func railDragTo(tt *ui.Tester, from string, x, y float32) error {
	r, ok := tt.Find(from)
	if !ok {
		return fmt.Errorf("no square %q", from)
	}
	x0, y0 := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x0, y0)
	step(tt)
	for i := 1; i <= 4; i++ {
		f := float32(i) / 4
		tt.Move(x0+(x-x0)*f, y0+(y-y0)*f)
		step(tt)
	}
	step(tt)
	tt.Release(x, y)
	for range 3 {
		step(tt)
	}
	return nil
}

// Dragged with the pointer, a square goes before the one it is dropped
// on, not to the end, and past the last one to the end.
func TestRailDragsSquares(t *testing.T) {
	// Named so that only the rail shows the names of the projects not
	// shown.
	var dirs []string
	for _, name := range []string{"railalpha", "railbeta", "railgamma"} {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	f, hs, tt := railFixture(t, dirs...)
	f.active = hs[2]
	step(tt)
	order := func() []*desktop { return slices.Clone(f.tabs) }
	r, _ := tt.Find("railalpha")
	if err := railDragTo(tt, "railbeta", r.X+r.W/2, r.Y+r.H/2); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []*desktop{hs[1], hs[0], hs[2]}) {
		t.Fatalf("dropped on the first: %v, want the second before it", got)
	}
	// Below the squares, on the rail itself.
	if err := railDragTo(tt, "railbeta", r.X+r.W/2, 600); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []*desktop{hs[0], hs[2], hs[1]}) {
		t.Fatalf("dropped past the last: %v, want it at the end", got)
	}
}

// A picture dragged from the sidebar onto a square shows in it, and
// leaves the rail's order as it was.
func TestRailTakesPictureFromSidebar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "railalpha")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shown := workspaceFixture(t)
	writePNG(t, filepath.Join(shown, "railpic.png"), 16, 16)
	f, hs, tt := railFixture(t, shown, dir)
	f.m.icons = filepath.Join(t.TempDir(), "project-icons")
	for range 200 {
		step(tt)
		if _, ok := tt.Find("railpic.png"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, _ := tt.Find("railalpha")
	if err := railDragTo(tt, "railpic.png", r.X+r.W/2, r.Y+r.H/2); err != nil {
		t.Fatal(err)
	}
	settleIcon(t, f, tt, dir)
	if !slices.Equal(f.tabs, hs) {
		t.Errorf("the rail is reordered: %v", f.tabs)
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
	step(tt)
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
		step(tt)
	}
	b.ResetTimer()
	for range b.N {
		step(tt)
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
			step(tt)
			time.Sleep(5 * time.Millisecond)
		}
	}
	b.ResetTimer()
	for i := range b.N {
		f.active = hs[i%2]
		step(tt)
	}
}

// A terminal's bell in a project out of sight marks its square, in its
// tip too, until the project shows.
func TestRailMarksAttention(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	// Shown once, as a project with a terminal was.
	f.active = hs[1]
	step(tt)
	f.active = hs[0]
	step(tt)
	hs[1].a.post(func() { hs[1].a.alert = "Bell from zsh" })
	step(tt)
	if hs[1].a.alert == "" {
		t.Fatal("the mark of a project out of sight went")
	}
	r, _ := tt.Find(filepath.Base(other))
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	if !tt.HasText("Bell from zsh") {
		t.Errorf("the tip does not say why: %v", tt.Texts())
	}
	f.active = hs[1]
	step(tt)
	if hs[1].a.alert != "" {
		t.Error("the mark stays as the project shows")
	}
}

// What the programs of a project out of sight wait for and work at show
// in the tip of its square, the program that waits first, beside what was
// asked attention for.
func TestRailTipsWork(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	f, hs, tt := railFixture(t, root, other)
	// Shown once, as a project with a terminal was.
	f.active = hs[1]
	step(tt)
	f.active = hs[0]
	step(tt)
	hs[1].a.post(func() {
		hs[1].a.terms = append(hs[1].a.terms, &session{status: terminal.ProgramStatus{Records: []terminal.StatusRecord{
			{ID: "", State: terminal.StatusWorking, App: "cargo", Message: "Building", Progress: 42},
			{ID: "deploy", State: terminal.StatusBlocked, App: "claude", Message: "Needs approval", Progress: -1},
		}}})
		hs[1].a.alert, hs[1].a.alertState = "codex: Done", terminal.StatusDone
	})
	step(tt)
	r, _ := tt.Find(filepath.Base(other))
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	for _, want := range []string{"claude: Needs approval", "cargo: Building 42%", "codex: Done"} {
		if !tt.HasText(want) {
			t.Errorf("the tip does not say %q: %v", want, tt.Texts())
		}
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
		`<link rel="icon" href="/brand/mark.svg">`:                             "/brand/mark.svg",
		`<link href="icons/a.png?v=2" rel="shortcut icon">`:                    "icons/a.png",
		`<link REL='APPLE-TOUCH-ICON' HREF='/apple.png'>`:                      "/apple.png",
		`<link rel="stylesheet" href="app.css"><link rel="icon" href="f.svg">`: "f.svg",
		`{ rel: "icon", href: "/icons/root.png" }`:                             "/icons/root.png",
		`{ rel: "icon", url: "/images/icons/favicon-v2.ico" }`:                 "/images/icons/favicon-v2.ico",
		`icons: { icon: "/icon.png", apple: "/apple.png" }`:                    "/icon.png",
		`export const m = { apple: "/ap.png" }`:                                "/ap.png",
		`<link data-href="/wrong.png" href="/r.png" rel="icon">`:               "/r.png",
		`<link rel="icon" href="./site.png">`:                                  "site.png",
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

// resolveIconHref looks in public/ first for a leading slash, as it names
// the static folder, and beside the source first for a relative href.
func TestResolveIconHref(t *testing.T) {
	for _, c := range []struct {
		href, base string
		want       []string
	}{
		{"/brand/mark.svg", "", []string{"public/brand/mark.svg", "brand/mark.svg"}},
		{"/brand/mark.svg", "site", []string{"public/brand/mark.svg", "brand/mark.svg", "site/brand/mark.svg"}},
		{"icon.png", "", []string{"icon.png", "public/icon.png"}},
		{"icon.png", "site", []string{"site/icon.png", "public/icon.png", "icon.png"}},
		{"./icon.png", "public", []string{"public/icon.png", "icon.png"}},
	} {
		if got := resolveIconHref(c.href, c.base); !slices.Equal(got, c.want) {
			t.Errorf("%q in %q: got %q, want %q", c.href, c.base, got, c.want)
		}
	}
	if resolveIconHref("data:image/png,xxx", "") != nil {
		t.Error("a data URI resolves")
	}
}

// A site of its own in site/ declares its icon beside its page, as
// Tiller's does.
func TestLoadProjectLogoFromSite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "site", "index.html"), []byte(`<link rel="icon" type="image/png" href="icon.png">`))
	if loadProjectLogo(dir) != nil {
		t.Fatal("a logo without its file")
	}
	writeFile(t, filepath.Join(dir, "site", "icon.png"), pngBytes(t, 16, 16))
	if got := findIconFromSource(dir); got != filepath.Join(dir, "site", "icon.png") {
		t.Errorf("got %q, want site/icon.png", got)
	}
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from the icon site/index.html declares")
	}
}

// A mygo app's icon is the one its configuration names, else
// resources/icon.png, as mygo takes it.
func TestFindMygoIcon(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "resources", "icon.png"), pngBytes(t, 16, 16))
	if findMygoIcon(dir) != "" {
		t.Error("an icon of a folder that is not a mygo app")
	}
	writeFile(t, filepath.Join(dir, "mygo.json"), []byte(`{"name": "App"}`))
	if got, want := findMygoIcon(dir), filepath.Join(dir, "resources", "icon.png"); got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from resources/icon.png")
	}
	writeFile(t, filepath.Join(dir, "art", "app.png"), pngBytes(t, 16, 16))
	writeFile(t, filepath.Join(dir, "mygo.json"), []byte(`{"name": "App", "icon": "art/app.png"}`))
	if got, want := findMygoIcon(dir), filepath.Join(dir, "art", "app.png"); got != want {
		t.Errorf("named: got %q, want %q", got, want)
	}
	writeFile(t, filepath.Join(dir, "mygo.json"), []byte(`{"icon": "../outside.png"}`))
	if got, want := findMygoIcon(dir), filepath.Join(dir, "resources", "icon.png"); got != want {
		t.Errorf("outside the project: got %q, want %q", got, want)
	}
	ts := t.TempDir()
	writeFile(t, filepath.Join(ts, "brand", "icon.png"), pngBytes(t, 16, 16))
	writeFile(t, filepath.Join(ts, "mygo.config.ts"), []byte("export default defineConfig({\n  name: \"App\",\n  icon: \"brand/icon.png\",\n})\n"))
	if got, want := findMygoIcon(ts), filepath.Join(ts, "brand", "icon.png"); got != want {
		t.Errorf("mygo.config.ts: got %q, want %q", got, want)
	}
}

// A macOS app's .icns in its resources decodes, at its PNG entry nearest
// the rail's size.
func TestLoadProjectLogoICNS(t *testing.T) {
	dir := t.TempDir()
	icns := filepath.Join(dir, "app", "Resources", "App.icns")
	writeFile(t, icns, icnsBytes(t, map[string][]byte{
		"ic04": []byte("ARGB not a png"),
		"ic11": pngBytes(t, 32, 32),
		"ic07": pngBytes(t, 128, 128),
		"ic10": pngBytes(t, 1024, 1024),
	}))
	if got := findNativeIcons(dir); !slices.Equal(got, []string{icns}) {
		t.Errorf("got %q, want %q", got, icns)
	}
	bm, ok := loadProjectLogo(dir).(*ui.Bitmap)
	if !ok {
		t.Fatal("no logo from the .icns")
	}
	if w, h := bm.Size(); w != logoSide || h != logoSide {
		t.Errorf("logo is %dx%d, want %dx%d", w, h, logoSide, logoSide)
	}
	img, err := decodeICNS(icnsBytes(t, map[string][]byte{"ic11": pngBytes(t, 32, 32), "ic12": pngBytes(t, 64, 64)}))
	if err != nil {
		t.Fatal(err)
	}
	if w := img.Bounds().Dx(); w != 64 {
		t.Errorf("small entries: took %d, want the largest, 64", w)
	}
	for _, data := range [][]byte{
		[]byte("not an icns"),
		icnsBytes(t, map[string][]byte{"ic04": []byte("ARGB")}),
		append([]byte("icns\x00\x00\x00\x10ic07\xff\xff\xff\xff"), pngBytes(t, 8, 8)...),
	} {
		if _, err := decodeICNS(data); err == nil {
			t.Errorf("%q decoded", data[:min(len(data), 16)])
		}
	}
}

// An Xcode project's AppIcon set is found in a top-level folder's asset
// catalog, its picture nearest the rail's size first.
func TestFindNativeIconsAppIconSet(t *testing.T) {
	dir := t.TempDir()
	set := filepath.Join(dir, "MyApp", "Assets.xcassets", "AppIcon.appiconset")
	writeFile(t, filepath.Join(set, "Contents.json"), []byte(`{"images": [
		{"filename": "16.png", "idiom": "mac", "scale": "1x", "size": "16x16"},
		{"filename": "32@2x.png", "idiom": "mac", "scale": "2x", "size": "32x32"},
		{"filename": "128.png", "idiom": "mac", "scale": "1x", "size": "128x128"},
		{"filename": "512@2x.png", "idiom": "mac", "scale": "2x", "size": "512x512"},
		{"filename": "../escape.png", "idiom": "mac", "scale": "1x", "size": "1024x1024"},
		{"idiom": "mac", "scale": "1x", "size": "256x256"}
	]}`))
	for _, name := range []string{"16.png", "32@2x.png", "128.png", "512@2x.png"} {
		writeFile(t, filepath.Join(set, name), pngBytes(t, 16, 16))
	}
	other := filepath.Join(dir, "MyApp", "Assets.xcassets", "AccentColor.colorset")
	writeFile(t, filepath.Join(other, "Contents.json"), []byte(`{}`))
	// Dependencies are not looked in.
	writeFile(t, filepath.Join(dir, "node_modules", "x", "Assets.xcassets", "AppIcon.appiconset", "Contents.json"), []byte(`{}`))
	want := []string{"128.png", "512@2x.png", "32@2x.png", "16.png"}
	for i, name := range want {
		want[i] = filepath.Join(set, name)
	}
	if got := findNativeIcons(dir); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from the AppIcon set")
	}
}

// The well-known files come before a mygo app's icon, which comes before
// the icon a source declares, which comes before a native one.
func TestLoadProjectLogoOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Resources", "App.icns"), icnsBytes(t, map[string][]byte{"ic07": pngBytes(t, 128, 128)}))
	writeFile(t, filepath.Join(dir, "site", "index.html"), []byte(`<link rel="icon" href="icon.png">`))
	writeFile(t, filepath.Join(dir, "site", "icon.png"), pngBytes(t, 20, 20))
	writeFile(t, filepath.Join(dir, "mygo.json"), []byte(`{}`))
	writeFile(t, filepath.Join(dir, "resources", "icon.png"), pngBytes(t, 30, 30))
	writeFile(t, filepath.Join(dir, "logo.png"), pngBytes(t, 40, 40))
	side := func() int {
		bm, ok := loadProjectLogo(dir).(*ui.Bitmap)
		if !ok {
			t.Fatal("no logo")
		}
		w, _ := bm.Size()
		return w
	}
	for _, c := range []struct {
		remove string
		want   int
	}{{"", 40}, {"logo.png", 30}, {"mygo.json", 20}, {"site", logoSide}} {
		if c.remove != "" {
			if err := os.RemoveAll(filepath.Join(dir, c.remove)); err != nil {
				t.Fatal(err)
			}
		}
		if got := side(); got != c.want {
			t.Errorf("without %q: a logo %d wide, want %d", c.remove, got, c.want)
		}
	}
}

// writeFile writes data at path, making its folders.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// icnsBytes encodes an icns of entries, by type, in the order of their
// types.
func icnsBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var body bytes.Buffer
	for _, typ := range slices.Sorted(maps.Keys(entries)) {
		body.WriteString(typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(entries[typ])))
		body.Write(entries[typ])
	}
	var b bytes.Buffer
	b.WriteString("icns")
	binary.Write(&b, binary.BigEndian, uint32(8+body.Len()))
	b.Write(body.Bytes())
	return b.Bytes()
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

// The rail looks for a project's logo on its own, finding none in an
// empty folder, which keeps its initials, and the one a folder has.
func TestProjectIcons(t *testing.T) {
	root, bare := t.TempDir(), t.TempDir()
	logo := filepath.Join(root, "public", "logo.png")
	if err := os.MkdirAll(filepath.Dir(logo), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, logo, 32, 32)
	_, hs, tt := railFixture(t, root, bare)
	for end := time.Now().Add(5 * time.Second); hs[0].logoSrc == nil || !hs[1].logoDone; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("no logo found for a folder with one")
		}
		step(tt)
	}
	if hs[1].logoSrc != nil {
		t.Error("a logo for a folder without one")
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
	_, hs, tt := railFixture(t, root, other)
	hs[0].logoDone, hs[0].logoDir = true, root
	hs[0].logoSrc = loadProjectLogo(root)
	step(tt)
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
	f.scanProjectLogo(h.a, h, t.TempDir(), "")
	if h.logoSrc != nil {
		t.Error("the old folder's logo shows while the new folder is scanned")
	}
}

// The rail looks for the logo of every project, one not yet shown too.
func TestRailScansProjectsNotShown(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	writePNG(t, filepath.Join(other, "logo.png"), 16, 16)
	_, hs, tt := railFixture(t, root, other)
	if hs[1].a.started {
		t.Fatal("the second project started")
	}
	step(tt)
	if !hs[1].logoDone || hs[1].logoDir != other {
		t.Fatal("no scan for a project not shown")
	}
	for end := time.Now().Add(5 * time.Second); hs[1].logoSrc == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the logo of a project not shown never came")
		}
		step(tt)
	}
}

// settleFixture is a window of one project with a changed file, whose
// question about it stays open until answered.
func settleFixture(t *testing.T) (*frame, *fakeHost, *ui.Tester) {
	t.Helper()
	root := workspaceFixture(t)
	f, hs, tt := railFixture(t, root)
	// As the app keeps the windows it opens.
	f.m.frames = []*frame{f}
	f.m.s.AutoSave = false
	a := hs[0].a
	a.openFile(filepath.Join(root, "main.go"), true)
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "// changed\n"})
	step(tt)
	host := a.host.(*fakeHost)
	host.holdClose = true
	return f, host, tt
}

// Quitting again while the question about a change shows asks it once,
// so that one Cancel puts the quit off, and the next quit asks again.
func TestQuitTwiceAsksOnce(t *testing.T) {
	f, host, tt := settleFixture(t)
	m := f.m
	for range 2 {
		if m.beforeQuit() {
			t.Fatal("quit with a change not asked about")
		}
		step(tt)
	}
	if len(host.asked) != 1 || len(host.held) != 1 {
		t.Fatalf("asked %v", host.asked)
	}
	host.held[0](2)
	step(tt)
	if m.life != lifeOpen {
		t.Errorf("after Cancel the app is %v", m.life)
	}
	if m.beforeQuit() {
		t.Fatal("quit with a change not asked about")
	}
	step(tt)
	if len(host.asked) != 2 {
		t.Errorf("quitting after Cancel asked %v", host.asked)
	}
}

// Closing the window again while the question about a change shows asks
// it once, as does quitting meanwhile.
func TestCloseTwiceAsksOnce(t *testing.T) {
	f, host, tt := settleFixture(t)
	for range 2 {
		if f.beforeClose() {
			t.Fatal("closed with a change not asked about")
		}
		step(tt)
	}
	if f.m.beforeQuit() {
		t.Fatal("quit with a change not asked about")
	}
	step(tt)
	if len(host.asked) != 1 || len(host.held) != 1 {
		t.Fatalf("asked %v", host.asked)
	}
	host.held[0](2)
	step(tt)
	if f.life != lifeOpen || f.m.life != lifeOpen {
		t.Errorf("after Cancel the window is %v, the app %v", f.life, f.m.life)
	}
}
