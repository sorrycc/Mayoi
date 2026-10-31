package main

import (
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// Claude is as Claude Code draws itself in a terminal. Its welcome page
// is the CLI's welcome box: a box in the accent with the name in its top
// border, Clawd and a greeting on its left, tips and the folders opened
// last on its right, and under it a prompt that opens Quick Open. While
// Git works, the status bar has the CLI's spinner: a ✻ that turns through
// its glyphs, the work's name with a light running across it, and the
// seconds it has taken. Like the rain, it steps rather than glides, and
// holds still while the window has not the keyboard.

const (
	spinStep = 120 * time.Millisecond
	// After spinFlavorAt, the spinner's word turns now and then to one of
	// Claude Code's own, and back to the work's name.
	spinFlavorAt = 5 * time.Second
	spinFlavor   = 3 * time.Second
)

// spinGlyphs are the spinner's steps, out and back, as Claude Code's.
var spinGlyphs = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// spinVerbs are Claude Code's words for working, which it shows in
// English whatever the language.
var spinVerbs = []string{"Clauding…", "Noodling…", "Puttering…", "Schlepping…", "Pondering…"}

// spinState is the work the spinner shows, and since when.
type spinState struct {
	label string
	since time.Time
}

// spinAt returns what the spinner shows after a time: its glyph, its
// word, and where the light on the word is, in runes from its start.
func spinAt(in time.Duration, label string) (glyph, word string, light float64) {
	k := int(in / spinStep)
	glyph, word = spinGlyphs[k%len(spinGlyphs)], label
	if in >= spinFlavorAt {
		if n := int((in - spinFlavorAt) / spinFlavor); n%2 == 0 {
			word = spinVerbs[(n/2)%len(spinVerbs)]
		}
	}
	// The light crosses the word and a little past it, then again.
	span := len([]rune(word)) + 8
	return glyph, word, float64(k%span) - 4
}

// claudeSpinner is the spinner in the status bar while Git does what busy
// names.
func (a *app) claudeSpinner(c *ui.Context, busy string) {
	p := a.pal
	s := &a.spin
	if s.label != busy {
		s.label, s.since = busy, time.Now()
	}
	f := ui.Font{Family: a.font.family, Size: 11.5}
	advance := func(str string) float32 {
		var w float32
		for _, g := range ui.Shape(str, f) {
			w += g.Advance
		}
		return w
	}
	// As wide as its widest word, so that the bar holds still as it turns.
	words := advance(busy)
	for _, v := range spinVerbs {
		words = max(words, advance(v))
	}
	gap := advance(" ")
	glyphW, secsW := advance("✻"), advance("(999s)")
	still := a.mascotStill(c)
	ui.Row(c.Key("status:busy")).Size(glyphW+words+secsW+3*gap+12, statusBarH).Tooltip(busy).Draw(func(pt *ui.Painter, r ui.Rect) {
		in := pt.Now().Sub(s.since)
		glyph, word, light := spinAt(in, busy)
		if still {
			glyph, word, light = "✻", busy, -10
		} else {
			pt.After(spinStep - in%spinStep)
		}
		m := f.Metrics()
		y := r.Y + r.H/2 + (m.Ascent-m.Descent)/2
		x := r.X + 6
		pt.Glyphs(ui.Shape(glyph, f), x, y, p.accent)
		x += glyphW + gap
		// Each rune lit as near as it is to the light.
		glow := p.text
		if !p.dark {
			glow = p.accent.Mix(white, 0.55)
		}
		gs := ui.Shape(word, f)
		for i := range gs {
			k := float32(math.Max(0, 1-math.Abs(float64(gs[i].Cluster)-light)/2.5))
			pt.Glyphs(gs[i:i+1], x, y, p.accent.Mix(glow, 0.7*k))
		}
		x += advance(word) + gap
		pt.Glyphs(ui.Shape(fmt.Sprintf("(%ds)", int(in.Seconds())), f), x, y, p.faint)
	})
}

// welcomeBox is the welcome page of Claude.
func (a *app) welcomeBox(c *ui.Context, view ui.Rect) {
	p := a.pal
	ui.Column(c).FillWidth().MinHeight(view.H).Center().Padding(40, 24, 64).Children(func() {
		riseIn(ui.Column(c.Key("welcome:box")).Width(760).MaxWidthPercent(100).Gap(14), 0).Children(func() {
			box := ui.Row(c.Key("welcome:frame")).FillWidth().Border(1, p.accent).Radius(8).Padding(24, 0, 18)
			box.Children(func() {
				// The name in the top border, as the CLI titles its box.
				ui.Row(c).Absolute().Top(-8).Left(16).Padding(0, 8).Gap(8).Background(p.editor).Children(func() {
					ui.Text(c, "Mayoi").FontSize(12.5).FontWeight(700).TextColor(p.text).SingleLine()
					ui.Text(c, appVersion()).FontSize(12.5).TextColor(p.accent).SingleLine()
				})
				ui.Column(c).Width(270).Shrink(0).Padding(4, 20).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.WelcomeBack()).FontSize(15).FontWeight(700).TextColor(p.text).Padding(0, 0, 10)
					if m := a.themeMascot(); m != nil {
						a.welcomeMascot(c, m)
					}
					ui.Box(c).Height(8)
					if a.root == "" {
						ui.Text(c, locales.WelcomeTagline()).FontSize(12).TextColor(p.muted).TextAlign(ui.Center)
						return
					}
					if b := a.repository().branch(); b != "" {
						ui.Text(c, b).FontSize(12).TextColor(p.muted).SingleLine()
					}
					ui.Text(c, shortenHome(hostPath(a.root))).FontSize(12).TextColor(p.muted).SingleLine().Shrink(1)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(2, 20).Gap(2).BorderWidth(0, 0, 0, 1).
					BorderColor(p.accent.Alpha(0.5)).Children(func() {
					a.boxHeading(c, locales.WelcomeTips())
					if v := a.host.update(); v != "" {
						a.boxTip(c, "↑", locales.UpdateAvailable(v), a.host.installUpdate)
					}
					if a.root == "" {
						a.boxTip(c, a.keyHint("file.openFolder"), locales.CmdFileOpenFolder(), a.chooseFolder)
						if remoteSupported {
							a.boxTip(c, a.keyHint("remote.connect"), locales.CmdRemoteConnect(), a.openRemotePicker)
						}
					} else {
						a.boxTip(c, a.keyHint("go.quickOpen"), locales.CmdGoQuickOpen(), func() { a.openQuick("") })
						a.boxTip(c, a.keyHint("edit.findInFiles"), locales.CmdEditFindInFiles(), func() { a.showSearch(false) })
					}
					a.boxTip(c, a.keyHint("go.commands"), locales.CmdGoCommands(), func() { a.openQuick(">") })
					a.boxTip(c, a.keyHint("view.terminal"), locales.CmdViewTerminal(), a.togglePanel)
					a.boxTip(c, a.keyHint("view.selectTheme"), locales.CmdViewSelectTheme(), a.openThemePicker)
					if a.host.account() == "" && a.settings.Path() != "" {
						a.boxTip(c, "", locales.CmdAccountSignIn(), func() { a.host.signIn() })
					}
					ui.Box(c).FillWidth().Height(1).Margin(10, 0).Background(p.accent.Alpha(0.45))
					a.boxHeading(c, locales.WelcomeActivity())
					// The folders before this one, or before none.
					recent := a.settings.RecentExcept(a.root)
					if len(recent) == 0 {
						ui.Text(c, locales.WelcomeNoActivity()).FontSize(12.5).TextColor(p.faint).Padding(4, 8)
					}
					for _, dir := range recent[:min(4, len(recent))] {
						a.boxRecent(c, dir)
					}
					if len(recent) > 4 {
						a.boxTip(c, a.keyHint("file.openRecent"), locales.MenuOpenRecentMore(), a.openRecentPicker)
					}
				})
			})
			// The prompt: what it opens, Quick Open, takes what the CLI's
			// prompt would.
			pr := ui.ButtonBase(c.Key("welcome:prompt")).FillWidth().Height(38).Padding(0, 14).Gap(10).Radius(8).
				Border(1, p.faint).Justify(ui.Start).AlignItems(ui.Center).Label(locales.CmdGoQuickOpen())
			if pr.Hovered() {
				pr.Border(1, p.accent)
			}
			pr.Transition(hoverFade)
			pr.Children(func() {
				ui.Text(c, ">").FontSize(13.5).FontWeight(700).TextColor(p.accent)
				ui.Text(c, locales.WelcomePrompt()).FontSize(13.5).TextColor(p.faint).SingleLine().Shrink(1)
			})
			if pr.Clicked() {
				if a.root == "" {
					a.chooseFolder()
				} else {
					a.openQuick("")
				}
			}
			ui.Row(c).FillWidth().Padding(0, 14).AlignItems(ui.Center).Children(func() {
				k := ui.ButtonBase(c.Key("welcome:keys")).Height(22).Radius(4).Padding(0, 4).Label(locales.CmdFileKeybindings())
				col := p.faint
				if k.Hovered() {
					col = p.accent
				}
				k.Children(func() { ui.Text(c, locales.WelcomeShortcuts()).FontSize(12).TextColor(col).SingleLine() })
				if k.Clicked() {
					a.host.showSettings(prefsShortcuts, "")
				}
				ui.Spacer(c)
				save := locales.WelcomeAutoSaveOff()
				col = p.faint
				if a.prefs().AutoSave {
					save, col = locales.WelcomeAutoSaveOn(), p.changed
				}
				ui.Text(c, "⏵⏵ "+save).FontSize(12).TextColor(col).SingleLine()
			})
			a.welcomeStar(c)
		})
	})
}

// boxHeading is a heading in the welcome box, in the accent.
func (a *app) boxHeading(c *ui.Context, s string) {
	ui.Text(c, s).FontSize(12.5).FontWeight(700).TextColor(a.pal.accent).Padding(2, 8, 4).SingleLine()
}

// boxTip is a row of the welcome box that runs a command: its keys, then
// its name.
func (a *app) boxTip(c *ui.Context, keys, label string, fn func()) {
	p := a.pal
	r := ui.ButtonBase(c.Key("tip:"+label)).FillWidth().Height(24).Padding(0, 8).Radius(4).Gap(12).
		Justify(ui.Start).AlignItems(ui.Center).Label(label)
	col := p.muted
	if r.Hovered() {
		r.Background(p.active)
		col = p.text
	}
	r.Transition(hoverFade)
	r.Children(func() {
		ui.Text(c, keys).FontSize(12.5).FontWeight(700).TextColor(p.text).Width(64).Shrink(0).SingleLine()
		ui.Text(c, label).FontSize(12.5).TextColor(col).SingleLine().Shrink(1)
	})
	if r.Clicked() {
		fn()
	}
}

// boxRecent is a folder opened last, in the welcome box: with Cmd, it
// opens in a window of its own.
func (a *app) boxRecent(c *ui.Context, dir string) {
	p := a.pal
	r := ui.ButtonBase(c.Key("recent:"+dir)).FillWidth().Height(24).Padding(0, 8).Radius(4).Gap(12).
		Justify(ui.Start).AlignItems(ui.Center).Label(locales.RecentOpenLabel(dir)).Tooltip(shortenHome(dir))
	if r.Hovered() {
		r.Background(p.active)
	}
	r.Transition(hoverFade)
	r.Children(func() {
		name := filepath.Base(dir)
		if remote.Is(dir) {
			name = "⇄ " + name
		}
		ui.Text(c, name).FontSize(12.5).FontWeight(700).TextColor(p.text).Width(64).Shrink(0).SingleLine()
		ui.Text(c, shortenHome(filepath.Dir(dir))).FontSize(12.5).TextColor(p.muted).SingleLine().Shrink(1)
	})
	switch {
	case r.Focused() && c.Shortcut(ui.Cmd, ui.KeyEnter):
		a.host.newWindow(dir)
	case r.Clicked():
		if r.ClickModifiers()&ui.Cmd != 0 {
			a.host.newWindow(dir)
		} else {
			a.switchFolder(dir)
		}
	}
}
