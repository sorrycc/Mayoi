package main

import (
	"slices"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// The code's font follows the settings' preferences as they change, and
// the terminals' follows the code's where they leave it to.
func TestFontFollowsTheSettings(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	hs.settle()
	if a.font.lineH != 20 || a.font.weight != 400 {
		t.Fatalf("line height %v, weight %d", a.font.lineH, a.font.weight)
	}
	p := a.settings.Prefs
	p.FontSize, p.LineHeight, p.FontWeight, p.FontFeatures = 14, 2, 600, []string{"calt=0", "ss01"}
	a.settings.SetPrefs(p)
	hs.settle()
	if f := a.font; f.size != 14 || f.lineH != 28 || f.weight != 600 || !slices.Equal(f.featureList(), p.FontFeatures) {
		t.Fatalf("font %+v", f)
	}
	if tf := termFont(a.settings); tf.Family != "monospace" || tf.Size != 13.5 || tf.Weight != 600 || len(tf.Features) != 2 {
		t.Fatalf("terminal font %+v", tf)
	}
	a.settings.TerminalFontFamily, a.settings.TerminalFontSize = "Menlo", 11
	if tf := termFont(a.settings); tf.Family != "Menlo, monospace" || tf.Size != 11 {
		t.Fatalf("terminal's own font %+v", tf)
	}
}

// A family the system lacks falls back to a monospaced font, not to the
// system's proportional one, and a list that names a generic family is
// kept as it is.
func TestMonoFallback(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "monospace",
		"monospace":               "monospace",
		"JetBrains Mono":          "JetBrains Mono, monospace",
		"Fira Code, Menlo":        "Fira Code, Menlo, monospace",
		"Iosevka, ui-monospace":   "Iosevka, ui-monospace",
		`"Comic Mono", Monospace`: `"Comic Mono", Monospace`,
	} {
		if got := monoFallback(in); got != want {
			t.Errorf("monoFallback(%q) = %q, want %q", in, got, want)
		}
	}
	hs := newHarness(t, false)
	a := hs.a
	p := a.settings.Prefs
	p.FontFamily = "No Such Font Mayoi"
	a.settings.SetPrefs(p)
	hs.settle()
	mono := newCodeFont(settings.Prefs{FontFamily: "monospace", FontSize: p.FontSize, FontWeight: p.FontWeight, LineHeight: p.LineHeight})
	if a.font.family != "No Such Font Mayoi, monospace" || a.font.charW != mono.charW {
		t.Errorf("a missing font measures %v a cell, as %q; monospace %v", a.font.charW, a.font.family, mono.charW)
	}
	if !a.font.is(a.settings.Prefs) {
		t.Error("the font does not match the settings it was made from")
	}
}

// A line indented with tabs keeps its indent where the text system lays
// it out with the editor's tab size, as the editor draws it: the tab
// alone at the start of the line too, not only after a space (#123), at
// every tab size the settings allow.
func TestTabsIndentTheCode(t *testing.T) {
	for _, n := range []int{2, 4, 8} {
		p := settings.Default().Prefs
		p.TabSize = n
		f := newCodeFont(p)
		lines := []string{"\tcode", " \tcode", "\t\tcode"}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).AlignItems(ui.Start).Children(func() {
				for i, s := range lines {
					ui.Text(c, s).Font(f.family).FontSize(f.size).FontWeight(f.weight).
						TabSize(float32(f.tabSize)).SingleLine().Label(strconv.Itoa(i))
				}
			})
		}, 800, 200)
		for i, s := range lines {
			r, ok := tt.Find(strconv.Itoa(i))
			if !ok {
				t.Fatalf("no line %q", s)
			}
			if want := f.width(s); r.W < want-0.5 || r.W > want+0.5 {
				t.Errorf("tab size %d: %q laid out %.2f wide, the editor measures %.2f", n, s, r.W, want)
			}
		}
	}
	tabCols.Store(4)
}
