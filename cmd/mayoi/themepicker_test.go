package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The box of the color themes shows each theme as the choice moves to it,
// and each appearance as it is switched to: Return keeps them, and Escape
// goes back to those it opened on.
func TestThemePickerPreviewsAndKeeps(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.run("view.selectTheme")
	hs.settle()
	if !a.themes.open || a.themes.sel != 0 {
		t.Fatalf("the box is not open on Mayoi's theme: open %v, row %d", a.themes.open, a.themes.sel)
	}
	hs.shot("theme-picker-mayoi")
	for range 4 {
		hs.key(0, ui.KeyDown)
	}
	hs.settle()
	if a.settings.Theme != themeGrokBot || a.pal != grokBotDarkPal {
		t.Fatalf("the window shows %q, not Grok Bot, as the choice moves to it", a.settings.Theme)
	}
	hs.key(0, ui.KeyLeft)
	hs.settle()
	if a.settings.Appearance != "" {
		t.Errorf("← from the system's appearance gives %q", a.settings.Appearance)
	}
	hs.key(0, ui.KeyRight)
	hs.settle()
	if a.settings.Appearance != "light" || a.pal != grokBotLightPal {
		t.Fatalf("→ does not show Grok Bot light: %q", a.settings.Appearance)
	}
	hs.shot("theme-picker-grokbot-light")
	hs.key(0, ui.KeyEscape)
	hs.settle()
	if a.themes.open || a.settings.Theme != "" || a.settings.Appearance != "" || a.pal != darkPal {
		t.Fatalf("Escape does not go back to the theme the box opened on: %q %q", a.settings.Theme, a.settings.Appearance)
	}

	a.run("view.selectTheme")
	hs.settle()
	hs.tt.Type("claude")
	hs.settle()
	if a.settings.Theme != themeClaude {
		t.Fatalf("the window does not show the theme the query matches, but %q", a.settings.Theme)
	}
	hs.key(0, ui.KeyEnter)
	hs.settle()
	if a.themes.open || a.settings.Theme != themeClaude || a.pal != claudeDarkPal {
		t.Errorf("Return does not keep Claude: %q", a.settings.Theme)
	}
}

// The window fades from one palette to the next, each color on its way,
// and settles on the new palette itself.
func TestPaletteFade(t *testing.T) {
	mid := mixPalettes(lightPal, darkPal, 0.5)
	if mid.editor == lightPal.editor || mid.editor == darkPal.editor {
		t.Errorf("halfway, the editor is %v", mid.editor)
	}
	if mid.syntax[0] == lightPal.syntax[0] || mid.ansi[1] == lightPal.ansi[1] {
		t.Error("the code's and the terminal's colors do not fade")
	}
	if !mid.dark || mid.inverse != darkPal.inverse {
		t.Error("what is not a color, or a color left to the theme, is not the new palette's")
	}
	if m := mixPalettes(lightPal, darkPal, 1); m.editor != darkPal.editor || m.text != darkPal.text {
		t.Error("the end of the fade is not the new palette")
	}

	hs := newHarness(t, true)
	a := hs.a
	a.setTheme(themeGrokBot)
	step(hs.tt)
	step(hs.tt)
	if a.pal == grokBotDarkPal || a.pal == darkPal {
		t.Error("the window does not fade to the new theme")
	}
	hs.settle()
	if a.pal != grokBotDarkPal {
		t.Error("the window does not settle on the new theme")
	}
}
