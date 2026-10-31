package main

import (
	"github.com/egoist/mygo/ui"
)

// Clawd, Claude Code's little character, as its banner draws it in
// quarter blocks: Claude's character, in the accent. Its eyes are cut
// from its body, and its legs move apart as it hops.

// clawdArt is Clawd's body, a cell to a character, a cell twice as tall
// as it is wide, as the quarter blocks are. The eyes are cut from it, and
// the legs are drawn apart, as they move.
var clawdArt = [...]string{
	"   ############   ",
	"   ############   ",
	" ################ ",
	"   ############   ",
}

const (
	clawdCols = 18
	clawdRows = 5 // the body's and the legs'
	// clawdEyes are the columns of the eyes, in the art's second row.
	clawdEyeL, clawdEyeR = 5, 12
)

// clawdLegs are the columns of the legs standing, and with them apart
// mid-hop.
var clawdLegs = [2][4]int{{4, 6, 11, 13}, {3, 5, 12, 14}}

// clawdSize is the size of Clawd drawn in cells of a width.
func clawdSize(cell float32) (w, h float32) {
	return clawdCols * cell, (clawdRows + 1) * 2 * cell
}

// drawClawd paints Clawd in r, centered, in cells of a width, in a pose,
// its eyes the color of bg.
func drawClawd(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, bg ui.Color, p mascotPose) {
	body := pal.accent
	w, h := clawdSize(cell)
	ch := 2 * cell
	// A cell of headroom over the body, for the hop.
	x0, y0 := r.X+(r.W-w)/2, r.Y+(r.H-h)/2+ch*(1-p.lift)
	for row, line := range clawdArt {
		// A run of cells at a time, so that the body has no seams.
		for i := 0; i < clawdCols; {
			if line[i] != '#' {
				i++
				continue
			}
			j := i
			for j < clawdCols && line[j] == '#' {
				j++
			}
			pt.Fill(ui.Rect{X: x0 + float32(i)*cell, Y: y0 + float32(row)*ch, W: float32(j-i) * cell, H: ch}, body, 0)
			i = j
		}
	}
	// The eyes, cut from the second row: shut, a slit.
	eh, ey := ch, y0+ch
	if p.blink {
		eh, ey = ch*0.25, y0+ch*1.5
	}
	for _, col := range []int{clawdEyeL, clawdEyeR} {
		pt.Fill(ui.Rect{X: x0 + float32(col)*cell, Y: ey, W: cell, H: eh}, bg, 0)
	}
	legY := y0 + float32(len(clawdArt))*ch
	for _, col := range clawdLegs[p.step] {
		pt.Fill(ui.Rect{X: x0 + float32(col)*cell, Y: legY, W: cell, H: ch * (1 + p.lift)}, body, 0)
	}
}
