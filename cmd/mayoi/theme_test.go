package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// matrixHarness is a dark window in The Matrix.
func matrixHarness(t testing.TB) *harness {
	hs := newHarness(t, true)
	hs.a.setTheme(themeMatrix)
	hs.settle()
	return hs
}

func TestMatrixTheme(t *testing.T) {
	if paletteFor(themeMatrix, false) != matrixPal || paletteFor(themeMatrix, true) != matrixPal {
		t.Error("The Matrix does not follow the window's light or dark")
	}
	if paletteFor("", false) != lightPal || paletteFor("dark", true) != darkPal {
		t.Error("light and dark windows have the wrong palettes")
	}
	if themeSource(themeMatrix, "light") != mygo.ThemeDark || themeSource("", "light") != mygo.ThemeLight {
		t.Error("the native window is not dark under The Matrix")
	}
	hs := matrixHarness(t)
	a := hs.a
	if a.pal != matrixPal {
		t.Fatal("the window is not in The Matrix")
	}
	if a.termTheme != themeMatrix {
		t.Error("the terminals do not have The Matrix's colors")
	}
	if th := termTheme(matrixPal); th.Background != matrixPal.panel || th.Palette != matrixPal.ansi {
		t.Error("the terminal's colors are not the palette's")
	}
	// The icons of files are greens, not their languages' colors.
	if _, c := fileIcon(a.pal, "main.go"); c != langColor(matrixPal, "#00add8", "") || c.G < c.R || c.G < c.B {
		t.Errorf("a Go file's icon is %v, not a green", c)
	}
	a.setTheme("")
	hs.settle()
	if a.pal != darkPal || a.termTheme != "" {
		t.Error("the window does not leave The Matrix")
	}
}

// The rain falls while the window has the keyboard, and holds still
// without it, asking for no frames.
func TestMatrixRainStopsWhenBlurred(t *testing.T) {
	hs := matrixHarness(t)
	a := hs.a
	if len(a.cols) != 1 || len(a.cols[0].tabs) != 0 {
		t.Fatal("the welcome page does not show")
	}
	if !a.rain.moving {
		t.Fatal("the rain does not fall on the welcome page")
	}
	a.post(func() { a.blurred = true })
	hs.settle()
	step := a.rain.step
	if a.rain.moving {
		t.Error("the rain falls in a window without the keyboard")
	}
	hs.settle()
	if a.rain.step != step {
		t.Error("the rain moved while still")
	}
	a.post(func() { a.blurred = false })
	hs.settle()
	if !a.rain.moving {
		t.Error("the rain does not fall again with the keyboard back")
	}
	// Out of The Matrix, there is none.
	a.setTheme("")
	a.rain.moving = false
	hs.settle()
	if a.rain.moving {
		t.Error("the rain falls in a dark window")
	}
}

func TestShotMatrix(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	hs := matrixHarness(t)
	a := hs.a
	hs.shot("matrix-welcome")
	src, _ := os.ReadFile("editor.go")
	os.WriteFile(hs.path("editor.go"), src, 0o644)
	a.openFile(hs.path("editor.go"), true)
	hs.settle()
	d := a.activeEditor()
	i := strings.Index(string(src), "func newCodeFont")
	at := len([]rune(string(src)[:i]))
	hs.place(d, at, at+20)
	hs.shot("matrix-editor")
	a.openQuick("")
	hs.tt.Type("ed")
	hs.settle()
	hs.shot("matrix-quick")

	hs = matrixHarness(t)
	scmFixture(hs)
	hs.a.run("view.scm")
	hs.settleGit()
	hs.a.showChanges(diffTarget{path: hs.path("main.go"), base: diffIndex}, true)
	hs.settleGit()
	hs.shot("matrix-scm")
}

// themeHarness is a window, dark or not, in a theme.
func themeHarness(t testing.TB, theme string, dark bool) *harness {
	hs := newHarness(t, dark)
	hs.a.setTheme(theme)
	hs.settle()
	return hs
}

// Claude and Qoder have a light and a dark palette, as the appearance is.
func TestLightOrDarkThemes(t *testing.T) {
	for _, tc := range []struct {
		name, theme string
		light, dark *palette
	}{{"Claude", themeClaude, claudeLightPal, claudeDarkPal}, {"Qoder", themeQoder, qoderLightPal, qoderDarkPal}} {
		if paletteFor(tc.theme, false) != tc.light || paletteFor(tc.theme, true) != tc.dark {
			t.Errorf("%s does not follow the window's light or dark", tc.name)
		}
		// It is light, dark or the system's, as the appearance is.
		if themeSource(tc.theme, "light") != mygo.ThemeLight || themeSource(tc.theme, "dark") != mygo.ThemeDark ||
			themeSource(tc.theme, "") != themeSource("", "") {
			t.Errorf("the native window does not follow the appearance under %s", tc.name)
		}
		for _, dark := range []bool{false, true} {
			hs := themeHarness(t, tc.theme, dark)
			a := hs.a
			if a.pal != paletteFor(tc.theme, dark) || a.pal.dark != dark {
				t.Fatalf("the window (dark %v) is not in %s", dark, tc.name)
			}
			if a.termTheme != tc.theme {
				t.Errorf("the terminals do not have %s's colors", tc.name)
			}
			light, darkTh := a.termThemes()
			if light.Background != tc.light.panel || darkTh.Palette != tc.dark.ansi {
				t.Errorf("the terminal's colors are not %s's", tc.name)
			}
			// The icons of files keep their languages' colors.
			if _, c := fileIcon(a.pal, "main.go"); c == langColor(matrixPal, "#00add8", "") {
				t.Error("a Go file's icon is a mono shade")
			}
			// Choosing an appearance keeps the theme.
			a.setAppearance("light")
			if a.settings.Theme != tc.theme {
				t.Errorf("choosing Light leaves %s", tc.name)
			}
			a.setTheme("")
			hs.settle()
			if a.termTheme != "" || a.pal == tc.light || a.pal == tc.dark {
				t.Errorf("the window does not leave %s", tc.name)
			}
		}
	}
}

// A character blinks for a moment, bobs once in a few periods, and hops
// while there is work, the hop changing every step.
func TestMascotPoses(t *testing.T) {
	at := func(ms int64) time.Time { return time.UnixMilli(ms) }
	period := mascotPeriod.Milliseconds()
	if p, hold := poseAt(at(3*period+10), false); !p.blink || hold != mascotBlink-10*time.Millisecond {
		t.Errorf("no blink at a period's start: %+v for %v", p, hold)
	}
	if p, hold := poseAt(at(period+500), false); p.blink || p.lift != 0 || hold != mascotPeriod-500*time.Millisecond {
		t.Errorf("the character does not rest until the next blink: %+v for %v", p, hold)
	}
	bob := mascotBobAt.Milliseconds()
	if p, _ := poseAt(at(3*period+bob+10), false); p.lift == 0 {
		t.Error("the character does not bob")
	}
	if p, hold := poseAt(at(3*period+500), false); p.lift != 0 || hold != mascotBobAt-500*time.Millisecond {
		t.Errorf("the character does not wait for its bob: %+v for %v", p, hold)
	}
	a, _ := poseAt(at(0), true)
	b, hold := poseAt(at(mascotHop.Milliseconds()), true)
	if a == b || hold != mascotHop {
		t.Errorf("the character does not hop: %+v then %+v for %v", a, b, hold)
	}
}

// The theme's character moves on the welcome page and in the status bar
// while the window has the keyboard, hops while Git works, and holds
// still without the keyboard: Clawd in Claude, and Qoduck in Qoder.
func TestMascotMoves(t *testing.T) {
	for _, tc := range []struct {
		theme string
		m     *mascot
		name  string
	}{{themeClaude, clawd, "Claude"}, {themeQoder, qoduck, "Qoder"}} {
		hs := themeHarness(t, tc.theme, true)
		a := hs.a
		if a.themeMascot() != tc.m {
			t.Fatalf("%s has not its character", tc.name)
		}
		if !a.mascot.moving || a.mascot.hopping {
			t.Fatalf("the character does not idle in %s", tc.name)
		}
		a.post(func() { a.git.busy = "Pushing…" })
		hs.settle()
		if !a.mascot.hopping {
			t.Errorf("the character does not hop while Git pushes in %s", tc.name)
		}
		if tip := a.mascotTip(tc.m); tip != tc.name+" · Pushing…" {
			t.Errorf("the character's tip is %q", tip)
		}
		a.post(func() { a.git.busy, a.blurred = "", true })
		hs.settle()
		if a.mascot.moving || a.mascot.hopping {
			t.Errorf("the character moves in a window without the keyboard in %s", tc.name)
		}
		a.post(func() { a.blurred = false })
		hs.settle()
		if !a.mascot.moving {
			t.Errorf("the character does not move again with the keyboard back in %s", tc.name)
		}
		// Out of the theme, there is none.
		a.setTheme("")
		a.mascot.moving = false
		hs.settle()
		if a.themeMascot() != nil || a.mascot.moving {
			t.Errorf("a character shows in a dark window after %s", tc.name)
		}
	}
}

func TestShotClaude(t *testing.T) {
	shotTheme(t, themeClaude)
}

func TestShotQoder(t *testing.T) {
	shotTheme(t, themeQoder)
}

// shotTheme shoots a theme's welcome page and an editor, light and dark.
func shotTheme(t *testing.T, theme string) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		name := theme + "-light"
		if dark {
			name = theme + "-dark"
		}
		hs := themeHarness(t, theme, dark)
		a := hs.a
		hs.shot(name + "-welcome")
		src, _ := os.ReadFile("editor.go")
		os.WriteFile(hs.path("editor.go"), src, 0o644)
		a.openFile(hs.path("editor.go"), true)
		hs.settle()
		d := a.activeEditor()
		i := strings.Index(string(src), "func newCodeFont")
		at := len([]rune(string(src)[:i]))
		hs.place(d, at, at+20)
		a.togglePanel()
		hs.settle()
		hs.shot(name + "-editor")
	}
}

// The appearances are greyed out in the menu under The Matrix, which is
// always dark, and the themes and appearances are checked apart.
func TestThemeMenu(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	find := func(label string) *mygo.MenuItem {
		var walk func(items []*mygo.MenuItem) *mygo.MenuItem
		walk = func(items []*mygo.MenuItem) *mygo.MenuItem {
			for _, it := range items {
				if it.Label == label {
					return it
				}
				if r := walk(it.Submenu); r != nil {
					return r
				}
			}
			return nil
		}
		return walk(a.menu(false, func(string, *mygo.Window) {}).Items())
	}
	a.setTheme(themeClaude)
	a.setAppearance("light")
	hs.settle()
	if a.pal != claudeLightPal {
		t.Errorf("palette is %v, want claudeLightPal", a.pal)
	}
	if l, c := find("Light"), find("Claude"); l == nil || c == nil || !l.Checked || !c.Checked || l.Disabled {
		t.Fatalf("Light and Claude are not both checked: %+v %+v", l, c)
	}
	if !a.commandOn("view.theme.claude") || !a.commandOn("view.appearance.light") || a.commandOn("view.theme.mayoi") {
		t.Error("the palette does not check Claude and Light")
	}
	a.setTheme(themeMatrix)
	if l := find("Light"); !l.Disabled || !l.Checked {
		t.Error("Light is not greyed out, and kept, under The Matrix")
	}
	if find("Mayoi").Checked || !find("The Matrix").Checked {
		t.Error("The Matrix is not the theme checked")
	}
	a.setTheme(themeQoder)
	if q, l := find("Qoder"), find("Light"); q == nil || !q.Checked || find("The Matrix").Checked || l.Disabled {
		t.Error("Qoder is not the theme checked, with Light to choose")
	}
	if !a.commandOn("view.theme.qoder") {
		t.Error("the palette does not check Qoder")
	}
}

// A button's label reads on its face, the accent's and danger's, at the
// 4.5:1 of WCAG's AA in every theme, and under the pointer, a moment, at
// least at the 3:1 AA asks of large or bold text.
func TestButtonContrast(t *testing.T) {
	pals := map[string]*palette{"light": lightPal, "dark": darkPal, "matrix": matrixPal, "claude light": claudeLightPal, "claude dark": claudeDarkPal,
		"qoder light": qoderLightPal, "qoder dark": qoderDarkPal}
	for name, p := range pals {
		for _, pair := range []struct {
			what     string
			fg, back ui.Color
			least    float64
		}{
			{"fill", p.fillText, p.fill, 4.5},
			{"danger", p.dangerText, p.dangerFill, 4.5},
			{"text", p.text, p.editor, 4.5},
			{"fill hovered", p.fillText, p.fillHover, 3},
			{"danger hovered", p.dangerText, p.dangerHover, 3},
		} {
			if r := contrast(pair.fg, pair.back); r < pair.least {
				t.Errorf("%s: %s reads at %.2f:1", name, pair.what, r)
			}
		}
	}
}

// The colors of Git's changes and of danger read as text at 4.5:1 on the
// chrome, the editor and the panel, in every theme, as a file's name in
// Source Control does on a light window.
func TestStatusTextContrast(t *testing.T) {
	pals := map[string]*palette{"light": lightPal, "dark": darkPal, "matrix": matrixPal, "claude light": claudeLightPal, "claude dark": claudeDarkPal,
		"qoder light": qoderLightPal, "qoder dark": qoderDarkPal}
	for name, p := range pals {
		for what, fg := range map[string]ui.Color{"modified": p.modifiedFg, "added": p.addedFg, "removed": p.removedFg, "danger": p.dangerFg} {
			for _, back := range []ui.Color{p.chrome, p.editor, p.panel} {
				if r := contrast(fg, back); r < 4.5 {
					t.Errorf("%s: %s reads at %.2f:1", name, what, r)
				}
			}
		}
	}
	// A color that reads already is kept.
	if darkPal.modifiedFg != darkPal.modified {
		t.Error("the dark window's modified changed, though it reads")
	}
}
