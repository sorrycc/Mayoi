package main

import (
	"github.com/egoist/mygo/ui"
)

// Grok Bot's face, as its icon and x.ai's page draw it: a round head with
// two slanted pill eyes. It is drawn from shapes rather than cells, so
// that it reads as small as the status bar: light on a dark window and
// dark on a light one, as x.ai's white face is on its black page, with the
// eyes cut out of it in the ground under it. In the air, it looks up,
// and now and then it glances to one side and the other.

// The face is drawn in units of half a cell, in a square of grokBotSpan
// of them.
const grokBotSpan = 30

var (
	grokBotLight = ui.Hex("#f5f5f5")
	grokBotDark  = ui.Hex("#1c1c1c")
)

// grokBotSize is the size of the face drawn in cells of a width, with two
// cells of headroom over it for the hop.
func grokBotSize(cell float32) (w, h float32) {
	u := cell / 2
	return grokBotSpan * u, grokBotSpan*u + 2*cell
}

// drawGrokBot paints the face in r, centered, in cells of a width, in a
// pose, on bg.
func drawGrokBot(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, bg ui.Color, p mascotPose) {
	u := cell / 2
	w, h := grokBotSize(cell)
	x0, y0 := r.X+(r.W-w)/2, r.Y+(r.H-h)/2+2*cell*(1-p.lift)
	face := grokBotDark
	if pal.dark {
		face = grokBotLight
	}
	pt.FillPath(ellipse(new(ui.Path), x0+15*u, y0+15*u, 13.5*u, 13.5*u, 0), face)

	// The eyes, leaning right, a little right of the middle and above it:
	// shut, a dot.
	ew, eh := 3.8*u, 8.4*u
	if p.blink {
		eh = ew
	}
	up := 1.2 * float32(p.step) * u
	// Glancing, the eyes move to a side of the face.
	side := 2.2 * float32(p.look) * u
	for _, eye := range [2][2]float32{{12.4, 13.6}, {19.4, 13.6}} {
		pt.FillPath(ellipse(new(ui.Path), x0+eye[0]*u+side, y0+eye[1]*u-up, ew/2, eh/2, 24), bg)
	}
}
