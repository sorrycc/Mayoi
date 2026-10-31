package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Claude is as Claude Code draws itself in a terminal. While Git works,
// the status bar has the CLI's spinner: a ✻ that turns through its
// glyphs, the work's name with a light running across it, and the
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
	// The frame's clock may be behind the one the work started by.
	in = max(in, 0)
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
