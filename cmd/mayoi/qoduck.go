package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Qoduck, Qoder's duck, as its icon draws it, a little rounder: a yellow
// body under a dome of a head, a tuft of three feathers, a frowning brow,
// two eyes and an orange beak. It is Qoder's character, drawn from shapes
// rather than cells, so that it reads as small as the status bar, and in
// its own colors rather than the accent. In the air, its tuft trails behind it.

// The duck is drawn in units of half a cell, in a square of qoduckSpan of
// them, as its icon is: the coordinates below are the icon's, a 32nd of
// its 1024 pixels, moved right a little so that the beak fits.
const qoduckSpan = 30

var (
	qoduckBody = ui.Hex("#ffd966")
	qoduckBeak = ui.Hex("#f87825")
	qoduckInk  = ui.Hex("#0f0d0c")
	// On a light window, where the yellow fades into the white, the body
	// and the beak are edged in deeper shades of theirs.
	qoduckBodyEdge = ui.Hex("#e3b13a")
	qoduckBeakEdge = ui.Hex("#d6611a")
	// Its props: the laptop and its prompt, and the forest green
	// megaphone.
	qoduckLaptop     = ui.Hex("#232523")
	qoduckLaptopEdge = ui.Hex("#6b736d")
	qoduckPrompt     = ui.Hex("#a7ada8")
	qoduckHorn       = ui.Hex("#3a8a62")
	qoduckHornEdge   = ui.Hex("#1f4c37")
)

// qoduckSize is the size of the duck drawn in cells of a width, with two
// cells of headroom over it for the hop.
func qoduckSize(cell float32) (w, h float32) {
	u := cell / 2
	return qoduckSpan * u, qoduckSpan*u + 2*cell
}

// drawQoduck paints the duck in r, centered, in cells of a width, in a
// pose.
func drawQoduck(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	u := cell / 2
	w, h := qoduckSize(cell)
	x0, y0 := r.X+(r.W-w)/2, r.Y+(r.H-h)/2+2*cell*(1-p.lift)
	// at moves a point in units to the window.
	at := func(x, y float32) (float32, float32) { return x0 + x*u, y0 + y*u }
	move := func(path *ui.Path, x, y float32) *ui.Path { return path.MoveTo(at(x, y)) }
	quad := func(path *ui.Path, cx, cy, x, y float32) *ui.Path {
		cx, cy = at(cx, cy)
		x, y = at(x, y)
		return path.QuadTo(cx, cy, x, y)
	}
	cube := func(path *ui.Path, c1x, c1y, c2x, c2y, x, y float32) *ui.Path {
		c1x, c1y = at(c1x, c1y)
		c2x, c2y = at(c2x, c2y)
		x, y = at(x, y)
		return path.CubeTo(c1x, c1y, c2x, c2y, x, y)
	}

	// The head's dome, the sides, and the rounded corners of the bottom.
	// Rounder than the icon's, the far side curves all the way down into
	// the bottom.
	body := move(new(ui.Path), 3.5, 14)
	cube(body, 3.5, 8.6, 9.2, 5.6, 16, 5.6)
	cube(body, 22.8, 5.6, 29.5, 8.6, 29.5, 14.5)
	cube(body, 29.5, 23.9, 26, 29.2, 17, 29.2)
	body.LineTo(at(9.9, 29.2))
	cube(body, 6.3, 29.2, 3.5, 26.2, 3.5, 22.6)
	body.Close()
	// The tuft, its tips trailing down in the air.
	droop := 0.8 * float32(p.step)
	tuft := move(new(ui.Path), 16.5, 6.5)
	quad(tuft, 16.2, 3.8, 16.9, 1.8+droop)
	move(tuft, 14, 6.8)
	quad(tuft, 13.2, 4.6, 12.6, 3.4+droop)
	move(tuft, 19, 6.8)
	quad(tuft, 19.8, 4.6, 20.6, 3.6+droop)
	tuftW := 2.2 * u
	beak := ellipse(new(ui.Path), x0+7.4*u, y0+20.4*u, 7.2*u, 4.6*u, 18)

	// The edges first, half of them under the shapes.
	edge := max(0.8*u, 1)
	light := !pal.dark
	if light {
		pt.StrokePath(tuft, tuftW+2*edge, qoduckBodyEdge)
		pt.StrokePath(body, 2*edge, qoduckBodyEdge)
	}
	pt.StrokePath(tuft, tuftW, qoduckBody)
	pt.FillPath(body, qoduckBody)

	// The brow, and the eyes under it: shut, a slit.
	brow := move(new(ui.Path), 10.9, 11.5)
	quad(brow, 14.8, 14.6, 19.6, 12.1)
	pt.StrokePath(brow, 1.4*u, qoduckInk)
	ew, eh := 2.75*u, 3.75*u
	if p.blink {
		eh = 0.9 * u
	}
	for _, eye := range [2][2]float32{{11.4, 16.1}, {17.15, 16.9}} {
		x, y := at(eye[0], eye[1])
		pt.Fill(ui.Rect{X: x - ew/2, Y: y - eh/2, W: ew, H: eh}, qoduckInk, min(ew, eh)/2)
	}

	// The beak, tilted down to the front, over the lower half of the near
	// eye.
	if light {
		pt.StrokePath(beak, 2*edge, qoduckBeakEdge)
	}
	pt.FillPath(beak, qoduckBeak)
	drawQoduckProp(pal, pt, p.prop, x0, y0, u)
}

// drawQoduckProp paints what the duck holds up, in units of u from its
// corner at x0, y0, as Qoder's pictures of it have them: the magnifier
// over its near eye, the laptop before it with a prompt, the megaphone at
// its beak, and the headset over its head, the microphone at its cheek.
func drawQoduckProp(pal *palette, pt *ui.Painter, prop mascotProp, x0, y0, u float32) {
	at := func(x, y float32) (float32, float32) { return x0 + x*u, y0 + y*u }
	// line is a stroke of w units, no thinner than shows.
	line := func(w float32) float32 { return max(w*u, 0.75) }
	switch prop {
	case propSearch:
		lens := ellipse(new(ui.Path), x0+7.6*u, y0+18.4*u, 5.2*u, 5.2*u, 0)
		pt.FillPath(lens, ui.RGBA(255, 255, 255, 0.35))
		pt.StrokePath(lens, line(1.7), qoduckInk)
		pt.StrokePath(new(ui.Path).MoveTo(at(10.9, 22.5)).LineTo(at(13.6, 27)), line(2.4), qoduckInk)
	case propGit:
		lid := ui.Rect{X: x0 + 15*u, Y: y0 + 18.5*u, W: 15 * u, H: 11 * u}
		pt.Fill(lid, qoduckLaptop, 1.8*u)
		pt.Stroke(lid, qoduckLaptopEdge, 1.8*u, line(0.7))
		prompt := new(ui.Path).MoveTo(at(17.8, 21.6)).LineTo(at(19.8, 23.3)).LineTo(at(17.8, 25))
		prompt.MoveTo(at(21.1, 26.2)).LineTo(at(24.1, 26.2))
		pt.StrokePath(prompt, line(1), qoduckPrompt)
	case propNews:
		horn := new(ui.Path).MoveTo(at(1.5, 17.8)).LineTo(at(-3.4, 14.6)).LineTo(at(-3.4, 26.4)).LineTo(at(1.5, 23.2)).Close()
		pt.FillPath(horn, qoduckHorn)
		pt.StrokePath(horn, line(0.6), qoduckHornEdge)
		pt.Fill(ui.Rect{X: x0 + 0.6*u, Y: y0 + 17.2*u, W: 2.4 * u, H: 6.6 * u}, qoduckHornEdge, u)
		sound := new(ui.Path).MoveTo(at(-5, 16.2)).LineTo(at(-6.2, 15.2))
		sound.MoveTo(at(-5.4, 20.5)).LineTo(at(-7, 20.5))
		sound.MoveTo(at(-5, 24.8)).LineTo(at(-6.2, 25.8))
		ink := qoduckInk
		if pal.dark {
			ink = pal.muted
		}
		pt.StrokePath(sound, line(1.1), ink)
	case propAI:
		band := new(ui.Path).MoveTo(at(5.6, 13.2))
		c1x, c1y := at(6.4, 4.8)
		c2x, c2y := at(25.6, 4.8)
		x, y := at(26.8, 13.2)
		pt.StrokePath(band.CubeTo(c1x, c1y, c2x, c2y, x, y), line(1.3), qoduckInk)
		pt.Fill(ui.Rect{X: x0 + 25.2*u, Y: y0 + 11.6*u, W: 3.6 * u, H: 6.8 * u}, qoduckInk, 1.8*u)
		arm := new(ui.Path).MoveTo(at(27, 18.4))
		c1x, c1y = at(27, 23.4)
		c2x, c2y = at(24, 25.8)
		x, y = at(19.4, 26)
		pt.StrokePath(arm.CubeTo(c1x, c1y, c2x, c2y, x, y), line(1.1), qoduckInk)
		pt.FillPath(ellipse(new(ui.Path), x0+19.2*u, y0+26*u, 1.4*u, 1.4*u, 0), qoduckInk)
	}
}

// ellipse adds an ellipse as a subpath, in four cubic curves, turned by
// deg degrees clockwise about its center.
func ellipse(p *ui.Path, cx, cy, rx, ry, deg float32) *ui.Path {
	const k = 0.5522847498
	sin, cos := math.Sincos(float64(deg) * math.Pi / 180)
	// at turns a point of the level ellipse, from its center.
	at := func(x, y float32) (float32, float32) {
		s, c := float32(sin), float32(cos)
		return cx + x*c - y*s, cy + x*s + y*c
	}
	cube := func(c1x, c1y, c2x, c2y, x, y float32) {
		c1x, c1y = at(c1x, c1y)
		c2x, c2y = at(c2x, c2y)
		x, y = at(x, y)
		p.CubeTo(c1x, c1y, c2x, c2y, x, y)
	}
	p.MoveTo(at(rx, 0))
	cube(rx, ry*k, rx*k, ry, 0, ry)
	cube(-rx*k, ry, -rx, ry*k, -rx, 0)
	cube(-rx, -ry*k, -rx*k, -ry, 0, -ry)
	cube(rx*k, -ry, rx, -ry*k, rx, 0)
	return p.Close()
}
