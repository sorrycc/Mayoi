package main

import (
	"github.com/egoist/mygo/ui"
)

// The White Rabbit, the character of The Matrix, whom Neo is told to
// follow: white, its ears pink inside, its eyes the red pill. Its ears
// lean apart at the second step of the hop.

// rabbitArt is the rabbit, a cell to a character: w its fur, p the pink
// of its ears and n its nose. The ears are drawn apart from the head, by
// rabbitEars. The eyes are drawn over it.
var rabbitArt = []string{
	"..............",
	"..............",
	"..............",
	"..............",
	"..............",
	"..wwwwwwwwww..",
	".wwwwwwwwwwww.",
	".wwwwwwwwwwww.",
	".wwwwwnnwwwww.",
	"..wwwwwwwwww..",
	"...wwwwwwww...",
	"...ww....ww...",
}

// rabbitEars are the ears, upright and leaning apart.
var rabbitEars = [2][]string{
	{"...ww....ww...", "...wp....pw...", "...wp....pw...", "...wp....pw...", "...ww....ww..."},
	{"..ww......ww..", "..wp......pw..", "...wp....pw...", "...wp....pw...", "...ww....ww..."},
}

var (
	rabbitFur  = ui.Hex("#eef2f0")
	rabbitPink = ui.Hex("#f2a0ae")
	rabbitEyes = ui.Hex("#d8323f")
)

const rabbitCols, rabbitRows = 14, 12

func rabbitSize(cell float32) (w, h float32) {
	return rabbitCols * cell, (rabbitRows + 2) * cell
}

// drawRabbit paints the White Rabbit in r, centered, in cells of a width,
// in a pose.
func drawRabbit(_ *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	x0, y0 := halloweenOrigin(r, rabbitCols, rabbitRows, cell, p)
	ears := rabbitEars[p.step%2]
	fillCells(pt, ears, 'w', rabbitFur, x0, y0, cell)
	fillCells(pt, ears, 'p', rabbitPink, x0, y0, cell)
	fillCells(pt, rabbitArt, 'w', rabbitFur, x0, y0, cell)
	fillCells(pt, rabbitArt, 'n', rabbitPink, x0, y0, cell)
	eyeCells(pt, [2]int{4, 9}, 6, 2, rabbitEyes, x0, y0, cell, p)
}
