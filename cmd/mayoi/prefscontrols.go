package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// The controls of the settings window beyond the theme's widgets: the
// themes as little windows, sliders for the numbers, a switch of a few
// options, the code in the fonts chosen, and the toasts that undo.

// The size of a theme's little window in the settings window.
const themeTileW, themeTileH = 116, 70

// themeRing is the identity of the ring around the theme chosen, which
// slides from tile to tile as the choice moves.
type themeRing struct{}

// themeTiles chooses the theme from a little window of each, in the
// appearance the settings window is in: the one chosen ringed, the ring
// sliding to the one clicked as every window fades to it. The arrows move
// the choice while a tile has the keyboard.
func (w *prefsWindow) themeTiles(c *ui.Context, chosen string, apply func(any)) {
	p := w.pal
	dark := p.dark
	switch w.m.s.Appearance {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	sel := 0
	for i, t := range themeChoices {
		if t.id == chosen {
			sel = i
		}
	}
	was := sel
	tiles := ui.SegmentedBase(c.Key("prefs-themes"), &sel, len(themeChoices))
	tiles.Track.Wrap().Gap(10).Label(prefTitle("theme")).Children(func() {
		for i, t := range themeChoices {
			on := i == sel
			tile := tiles.Segment(i).Column().Width(themeTileW).Gap(7).AlignItems(ui.Center).Label(t.name()).
				Tooltip(t.about()).FocusRing(false)
			if on {
				w.first(tile)
			}
			hovered := tile.Hovered()
			pose := ui.Pose{}
			switch {
			case tile.Pressed():
				pose.Scale = 0.97
			case hovered:
				pose.Y = -2
			}
			q := paletteFor(t.id, dark || alwaysDark(t.id))
			m := themeMascotOf(t.id)
			tile.Children(func() {
				frame := ui.Box(c.Key("frame")).Size(themeTileW, themeTileH).Shrink(0)
				frame.AnimatePose(pose, ui.Snappy)
				frame.Children(func() {
					edge := q.border
					if hovered && !on {
						edge = p.muted.Alpha(0.6)
					}
					ui.Box(c.Key("swatch")).Size(themeTileW, themeTileH).Radius(8).Background(q.editor).Border(1, edge).Clip().
						Transition(hoverFade).
						Draw(func(pt *ui.Painter, r ui.Rect) { paintSwatch(pt, r, q, m, themeTileH/float32(swatchH), false) })
					if on {
						ring := ui.Box(c.Key("ring")).Absolute().Left(-4).Top(-4).Right(-4).Bottom(-4).Radius(11).Border(2, p.accent)
						if tile.FocusVisible() {
							ring.Shadow(0, 0, 0, 3, p.accent.Alpha(0.25))
						}
						ring.Transition(ui.ElementTransition{Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Match: themeRing{}})
						ui.Box(c.Key("check")).Absolute().Top(-7).Right(-7).Size(18, 18).Radius(9).Background(p.fill).
							Border(2, p.editor).Justify(ui.Center).AlignItems(ui.Center).Transition(dotPop).Children(func() {
							ui.Icon(c, iconCheck).FontSize(10).TextColor(p.fillText)
						})
					}
				})
				col, weight := p.muted, 400
				if on {
					col, weight = p.text, 600
				}
				ui.Text(c.Key("name"), t.name()).FontSize(12).FontWeight(weight).TextColor(col).SingleLine().MaxWidth(themeTileW)
			})
		}
	})
	if sel != was {
		apply(themeChoices[sel].id)
	}
}

// segmentThumb is the identity of the face under the option chosen of a
// switch of options, which slides to the one clicked.
type segmentThumb struct{ key string }

// segmented chooses one of labels, as a segmented control does: the
// face under the option chosen slides to the one clicked. It returns the
// option chosen, and whether it is a new choice.
func (w *prefsWindow) segmented(c *ui.Context, key string, sel int, labels []string, label string, disabled bool) (int, bool) {
	p := w.pal
	t := c.Theme()
	was := sel
	parts := ui.SegmentedBase(c.Key(key), &sel, len(labels))
	parts.Track.AlignItems(ui.Stretch).Padding(2).Gap(2).Radius(7).Background(p.text.Alpha(0.05)).Border(1, p.border).
		Label(label).Children(func() {
		for i, text := range labels {
			on := i == sel
			s := parts.Segment(i).Padding(3, 12).Radius(5).Label(text).Disabled(disabled)
			if on {
				w.first(s)
			}
			col := p.muted
			switch {
			case on:
				col = p.text
			case s.Hovered() && !disabled:
				col = p.text
				s.Background(p.hover)
			}
			if disabled {
				col = col.Alpha(0.5)
			}
			s.Transition(hoverFade)
			s.Children(func() {
				if on {
					thumb := ui.Box(c.Key("thumb")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(5).Border(1, p.border)
					if p.dark {
						thumb.Background(t.SurfacePressed).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.3))
					} else {
						thumb.Background(t.Surface).Shadow(0, 1, 2, 0, ui.RGBA(20, 20, 40, 0.12))
					}
					thumb.Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Ease: ui.EaseInOut, Match: segmentThumb{key}})
				}
				// The label takes the room, and a copy of it shows over the
				// face, as what is placed paints over what is laid out.
				ui.Text(c, text).FontSize(12).FontWeight(500).SingleLine().Opacity(0)
				ui.Row(c.Key("label")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(col).SingleLine()
				})
			})
		}
	})
	return sel, sel != was
}

// segmentedFits reports whether the options of a preference fit in a
// switch of them, rather than a menu: a few, of short names.
func segmentedFits(labels []string) bool {
	n := 0
	for _, l := range labels {
		n += utf8.RuneCountInString(l)
	}
	return len(labels) <= 3 && n <= 28
}

// prefSliders are the numbers the window sets with a slider, which shows
// the value as it moves: those whose range a slider spans well.
var prefSliders = map[string]bool{"zoomLevel": true, "fontSize": true, "fontWeight": true, "lineHeight": true}

// sliderControl sets a number of the preferences with a slider, its
// value beside it and a stepper for a step at a time. Dragging changes
// only what the window shows, the code of the fonts too: the value
// applies as the slider is let go, so the windows lay out once.
func (w *prefsWindow) sliderControl(c *ui.Context, f settings.Field, v float64, apply func(any)) {
	p := w.pal
	buf := w.numbers[f.Key]
	if buf == nil {
		buf = new(float64)
		w.numbers[f.Key] = buf
	}
	if w.held != f.Key {
		*buf = v
	}
	ui.Row(c.Key("prefs-slider-" + f.Key)).Gap(10).AlignItems(ui.Center).Children(func() {
		s := w.first(ui.Slider(c.Key("slider"), buf, f.Min, f.Max).Step(f.Step).Width(168).Label(prefTitle(f.Key)))
		// Changed first, which takes the input the press is of; Dragged
		// holds while the pointer leaves the slider, as Pressed does not.
		changed := s.Changed()
		_, _, held := s.Dragged()
		switch {
		case held:
			w.held = f.Key
		case w.held == f.Key || changed:
			w.held = ""
			if *buf != v {
				apply(*buf)
			}
		}
		ui.Text(c.Key("value"), prefNumber(f.Key, *buf)).FontSize(12).FontWeight(500).FontFeatures("tnum").
			TextColor(p.text).Width(54).TextAlign(ui.End).SingleLine().ContentTransition(ui.ContentTransition{Numeric: true, Down: *buf < v})
		if ui.Stepper(c.Key("stepper"), buf, f.Min, f.Max, f.Step).Label(prefTitle(f.Key)).Changed() && *buf != v {
			apply(*buf)
		}
	})
}

// liveNumber is a number of the preferences as the window shows it: as
// its slider is, while it is dragged.
func (w *prefsWindow) liveNumber(key string, v float64) float64 {
	if w.held == key && w.numbers[key] != nil {
		return *w.numbers[key]
	}
	return v
}

// prefNumber is how the value of a number of the preferences reads
// beside its slider.
func prefNumber(key string, v float64) string {
	switch key {
	case "zoomLevel":
		return fmt.Sprintf("%.0f%%", 100*math.Pow(1.2, v))
	case "fontSize":
		return strconv.FormatFloat(v, 'f', -1, 64) + " pt"
	case "lineHeight":
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// prefFonts are the text preferences that name a font family, which a
// picker of the monospaced fonts installed sets.
var prefFonts = map[string]bool{"fontFamily": true, "terminalFontFamily": true}

// fontFamilies are the monospaced font families installed, which the
// system lists once, off the UI thread: none until it has.
func (w *prefsWindow) fontFamilies() []string {
	if !w.familiesAsked && w.win != nil {
		w.familiesAsked = true
		win := w.win
		go func() {
			names := monoFamilies(ui.FontFamilies())
			win.Update(func() { w.families = names })
		}()
	}
	return w.families
}

// monoFamilies are the names of the monospaced of families, but the
// system's hidden ones, whose names start with a dot.
func monoFamilies(families []ui.FontFamily) []string {
	var out []string
	for _, f := range families {
		if f.Monospaced && !strings.HasPrefix(f.Name, ".") {
			out = append(out, f.Name)
		}
	}
	return out
}

// fontOptions are the families the picker of the fonts offers for what
// is typed in it, "" for all: the system's monospaced font first, then
// those that start with it, then those that have it.
func fontOptions(families []string, typed string) []string {
	all := append([]string{"monospace"}, families...)
	q := strings.ToLower(strings.TrimSpace(typed))
	if q == "" {
		return all
	}
	var starts, has []string
	for _, f := range all {
		switch l := strings.ToLower(f); {
		case strings.HasPrefix(l, q):
			starts = append(starts, f)
		case strings.Contains(l, q):
			has = append(has, f)
		}
	}
	return append(starts, has...)
}

// fontPicker sets the font family of a preference: a field of it, in
// that font, that offers the monospaced fonts installed, each in itself,
// filtering them as it is typed in. A font chosen applies at once; what
// is typed, a list that falls back as "Iosevka, monospace" too, as Enter
// is pressed or the field is left. The code of the fonts shows the font
// the arrows or the pointer are on.
func (w *prefsWindow) fontPicker(c *ui.Context, f settings.Field, v string, buf *string, apply func(any)) {
	p := w.pal
	t := c.Theme()
	var cb ui.ComboboxParts
	// The input is the field, so that the fonts open under all of it, and
	// the arrow that opens them floats over its end.
	ui.Box(c.Key("prefs-control-" + f.Key)).Width(240).Children(func() {
		cb = ui.ComboboxBase(c.Key("font"), buf)
		focused := cb.Input.Focused()
		edge := p.border
		if focused {
			edge = p.accent
		}
		in := w.first(cb.Input.Height(30).Padding(0, 30, 0, 10).Radius(6).Background(t.Surface).Border(1, edge).Transition(fieldFocus).
			Font(monoFallback(v)).FontSize(12.5).TextColor(p.text).Label(prefTitle(f.Key)))
		if ph := prefText("prefHint", f.Key); ph != prefKey("prefHint", f.Key) {
			in.Placeholder(ph)
		}
		open := cb.Open()
		arrow := ui.ButtonBase(c.Key("arrow")).Absolute().Right(4).Top(4).Size(22, 22).Radius(5).AlignItems(ui.Center).
			Justify(ui.Center).Label(locales.PrefsFontsShow()).KeepFocus()
		if arrow.Hovered() {
			arrow.Background(p.hover)
		}
		arrow.Transition(hoverFade)
		turn := float32(0)
		if open {
			turn = 180
		}
		arrow.Children(func() {
			ui.Icon(c.Key("chevron"), iconChevronDown).FontSize(12).TextColor(p.muted).AnimatePose(ui.Pose{Rotate: turn}, ui.Snappy)
		})
		if arrow.Clicked() {
			cb.SetOpen(!open)
			cb.Input.Focus()
		}
	})
	focused := cb.Input.Focused()
	typed := ""
	if cb.Filtering() {
		typed = *buf
	}
	peek := ""
	cb.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(8).Background(t.Surface).Border(1, p.border)
		if p.dark {
			panel.Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.42))
		} else {
			panel.Shadow(0, 8, 24, 0, ui.RGBA(20, 20, 40, 0.16))
		}
		dropIn(panel, 4)
		ui.Scroll(c.Key("fonts")).MaxHeight(264).Children(func() {
			for _, name := range fontOptions(w.fontFamilies(), typed) {
				item := cb.Item(name).Row().Height(28).Padding(0, 8).Gap(8).Radius(5).AlignItems(ui.Center)
				if item.Highlighted() {
					item.Background(p.active)
					peek = name
				}
				item.Transition(hoverFade)
				item.Children(func() {
					ui.Text(c, name).Font(monoFallback(name)).FontSize(13).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					if strings.EqualFold(strings.TrimSpace(v), name) {
						ui.Icon(c, iconCheck).FontSize(12).TextColor(p.accent).Shrink(0)
					}
				})
			}
		})
	})
	if name, ok := cb.Chosen(); ok {
		// The font chosen, not what was typed before it.
		*buf = name
		w.editing[f.Key], w.drawn[f.Key] = false, true
		delete(w.drafts, f.Key)
		if name != v {
			apply(name)
		}
		return
	}
	if f.Key == "fontFamily" && peek != w.peek {
		w.peek = peek
		w.invalidate()
	}
	w.draft(f.Key, v, buf, focused, cb.Input.Submitted(), func(x any) {
		// The code's font left empty stays as it was, as the terminal's
		// does not: that is the code's.
		if f.Key != "fontFamily" || strings.TrimSpace(x.(string)) != "" {
			apply(x)
		}
	})
}

// previewCode is the code the fonts show in, with the pairs that coding
// fonts join and the glyphs they tell apart.
const previewCode = `// The quick brown fox jumps over the lazy dog
func greet(name string) (string, error) {
    if name != "" && len(name) <= 0x20 {
        return "Hello, " + name + "!", nil
    }
    return "", errors.New("-> no name => 0O 1lI")
}
`

// previewLines is the code of the preview, lexed once.
var previewLines = sync.OnceValue(func() []highlight.Line {
	return highlight.Lex(highlight.Lexer("preview.go"), strings.TrimSuffix(previewCode, "\n"))
})

// previewCaret is the line of the preview the caret is on.
const previewCaret = 2

// fontPreview shows code as the editor would in the fonts chosen: their
// family, size, weight, line height and features, as their sliders and
// fields are while they change.
func (w *prefsWindow) fontPreview(c *ui.Context) {
	p := w.pal
	s := w.m.s.Prefs
	size := float32(w.liveNumber("fontSize", float64(s.FontSize)))
	weight := int(w.liveNumber("fontWeight", float64(s.FontWeight)))
	lh := float32(w.liveNumber("lineHeight", float64(s.LineHeight)))
	name := s.FontFamily
	if w.peek != "" {
		name = w.peek
	}
	family, features := monoFallback(name), strings.Join(s.FontFeatures, ",")
	lineH := float32(int(size*lh + 0.5))
	lines := previewLines()
	// As high as the code in the fonts applied, which a slider dragged
	// leaves be, rather than move the slider away from the pointer: the
	// preview takes its new height as it is let go.
	height := 20 + float32(len(lines))*float32(int(s.FontSize*s.LineHeight+0.5))
	ui.Column(c.Key("prefs-preview")).Radius(10).Clip().Background(p.editor).Border(1, p.border).
		Transition(ui.ElementTransition{Size: true, Duration: 140 * time.Millisecond}).Children(func() {
		ui.Row(c.Key("bar")).Height(30).Padding(0, 12).Gap(7).AlignItems(ui.Center).Background(p.chrome).
			BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			ui.Icon(c, iconFileCode).FontSize(12).TextColor(p.accent).Shrink(0)
			ui.Text(c, "greet.go").FontSize(11.5).TextColor(p.muted).SingleLine()
			ui.Spacer(c)
			ui.Text(c.Key("about"), fmt.Sprintf("%s · %s · %d · %s", strings.TrimSpace(name), prefNumber("fontSize", float64(size)),
				weight, prefNumber("lineHeight", float64(lh)))).Font(family).FontSize(11).FontFeatures("tnum").TextColor(p.faint).
				SingleLine().Shrink(1).MinWidth(0)
		})
		gutterW := size * 2.2
		ui.Column(c.Key("code")).Padding(10, 0).Height(height).Clip().Children(func() {
			for i, line := range lines {
				row := ui.Row(c.Key(i)).Height(lineH).AlignItems(ui.Center)
				if i == previewCaret {
					row.Background(p.line)
				}
				row.Children(func() {
					num := p.gutter
					if i == previewCaret {
						num = p.gutterNow
					}
					ui.Text(c, strconv.Itoa(i+1)).Font(family).FontSize(size).FontWeight(weight).FontFeatures("tnum").TextColor(num).
						Width(gutterW).Padding(0, size*0.9, 0, 0).TextAlign(ui.End).SingleLine().Shrink(0)
					ui.RichText(c, previewSpans(line, p, family, size, weight, features)...).FixedLineHeight(lineH).SingleLine().Shrink(1).MinWidth(0)
					if i == previewCaret {
						ui.Box(c.Key("caret")).Width(2).Height(size * 1.25).Radius(1).Background(p.accent).Shrink(0)
					}
				})
			}
		})
	})
}

// previewSpans are a line of the preview in the colors of its syntax.
func previewSpans(line highlight.Line, p *palette, family string, size float32, weight int, features string) []ui.Span {
	runes := []rune(line.Text)
	var out []ui.Span
	add := func(from, to int, col ui.Color) {
		if from < to {
			out = append(out, ui.Span{Text: string(runes[from:to]), Font: family, Size: size, Weight: weight, Features: features, Color: col})
		}
	}
	at := 0
	for _, t := range line.Tokens {
		add(at, min(t.Start, len(runes)), p.text)
		col := p.syntax[t.Kind]
		if col == (ui.Color{}) {
			col = p.text
		}
		add(min(t.Start, len(runes)), min(t.End, len(runes)), col)
		at = min(t.End, len(runes))
	}
	add(at, len(runes), p.text)
	return out
}

// toastMotion is how a toast of the window comes and goes: it slides up
// as it fades in, and fades out as it sinks back.
var toastMotion = ui.ElementTransition{Duration: 200 * time.Millisecond, Colors: true, Enter: &ui.Motion{Y: 16}, Exit: &ui.Motion{Y: 8}}

// toasts builds the window's toasts, in the window's look: a card at the
// foot of the window, a toast that undoes with its button.
func (w *prefsWindow) toasts(c *ui.Context) {
	p := w.pal
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		// Under the preferences, rather than the middle of the window.
		viewport.Padding(0, 16, 22, prefsSidebarW+16).AlignItems(ui.Center).Gap(8)
		for _, t := range toasts {
			tp := ui.ToastBase(c, t)
			box := tp.Root.Row().Gap(10).Padding(6, 6, 6, 12).Radius(9).AlignItems(ui.Center).MaxWidth(520).
				Background(c.Theme().Surface).Border(1, p.border)
			if p.dark {
				box.Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.42))
			} else {
				box.Shadow(0, 8, 24, 0, ui.RGBA(20, 20, 40, 0.16))
			}
			box.Transition(toastMotion)
			box.Children(func() {
				icon, col := iconUndo, p.accent
				if t.Type == "error" {
					icon, col = iconTriangleAlert, p.dangerFg
				}
				ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0)
				ui.Text(c, t.Title).FontSize(12.5).TextColor(p.text).Shrink(1).MinWidth(0).Padding(4, 0)
				if t.Action != "" {
					b := tp.ActionButton().Padding(4, 10).Radius(6).Label(t.Action).Shrink(0)
					if b.Hovered() {
						b.Background(p.accent.Alpha(0.14))
					}
					b.Transition(hoverFade)
					pressPose(b)
					b.Children(func() {
						ui.Text(c, t.Action).FontSize(12.5).FontWeight(600).TextColor(p.accent).SingleLine()
					})
				}
				x := tp.CloseButton().Size(24, 24).Radius(6).AlignItems(ui.Center).Justify(ui.Center).Label(locales.PrefsDismiss()).Shrink(0)
				if x.Hovered() {
					x.Background(p.hover)
				}
				x.Transition(hoverFade)
				x.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(p.faint) })
			})
		}
	})
}

// undoable shows a toast of what was just done, with Undo, which runs
// undo.
func (w *prefsWindow) undoable(id, message string, undo func()) {
	w.notes = append(w.notes, ui.Toast{ID: id, Title: message, Timeout: 6 * time.Second, Action: locales.ButtonUndo(), OnAction: undo})
	w.invalidate()
}
