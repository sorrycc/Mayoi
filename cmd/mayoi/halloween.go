package main

import (
	"github.com/egoist/mygo/ui"
)

// The characters of the Halloween themes, drawn from square cells: Jack,
// the pumpkin of Jack-o'-Lantern, its carved face glowing; the cauldron of
// Witch's Brew, its brew bubbling; and the bat of Blood Moon, its wings
// flapping as it hops. Each is in the accent of its theme, with two cells
// of headroom over it for the hop, and its eyes blink and glance.

// fillCells paints the cells of art that are ch in col, a run of them at
// a time, so that a body has no seams, its top left at x0, y0.
func fillCells(pt *ui.Painter, art []string, ch byte, col ui.Color, x0, y0, cell float32) {
	for row, line := range art {
		for i := 0; i < len(line); {
			if line[i] != ch {
				i++
				continue
			}
			j := i
			for j < len(line) && line[j] == ch {
				j++
			}
			pt.Fill(ui.Rect{X: x0 + float32(i)*cell, Y: y0 + float32(row)*cell, W: float32(j-i) * cell, H: cell}, col, 0)
			i = j
		}
	}
}

// eyeCells paints a pair of eyes a cell wide and rows tall, at their
// columns in a row, moved a cell to the side of a glance: shut, a slit at
// their foot.
func eyeCells(pt *ui.Painter, cols [2]int, row, rows int, col ui.Color, x0, y0, cell float32, p mascotPose) {
	eh, ey := float32(rows)*cell, y0+float32(row)*cell
	if p.blink {
		eh, ey = cell*0.4, ey+float32(rows)*cell-cell*0.4
	}
	for _, c := range cols {
		pt.Fill(ui.Rect{X: x0 + float32(c+p.look)*cell, Y: ey, W: cell, H: eh}, col, 0)
	}
}

// halloweenOrigin is the top left of a character w cells wide and h tall,
// with its headroom, in r, centered, lifted as the pose is.
func halloweenOrigin(r ui.Rect, w, h int, cell float32, p mascotPose) (x0, y0 float32) {
	fw, fh := float32(w)*cell, float32(h+2)*cell
	return r.X + (r.W-fw)/2, r.Y + (r.H-fh)/2 + 2*cell*(1-p.lift)
}

// Jack's stem, which leans the other way at each step of the hop, and his
// body, its ribs a shade deeper. His face is cut from it.
var (
	jackStem = [2][]string{
		{"......gg......", ".....gg......."},
		{"......gg......", ".......gg....."},
	}
	jackBody = []string{
		"..ooodooodoo..",
		".oooodooodooo.",
		"ooooodooodoooo",
		"ooooodooodoooo",
		"ooooodooodoooo",
		"ooooodooodoooo",
		"ooooodooodoooo",
		".oooodooodooo.",
		"..oooooooooo..",
	}
	jackStemColor = ui.Hex("#6f9a3a")
	jackGlow      = ui.Hex("#ffe08a")
)

const jackCols, jackRows = 14, 11

func jackSize(cell float32) (w, h float32) {
	return jackCols * cell, (jackRows + 2) * cell
}

// drawJack paints Jack in r, centered, in cells of a width, in a pose: a
// pumpkin in the accent, with a face that glows in candlelight.
func drawJack(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	x0, y0 := halloweenOrigin(r, jackCols, jackRows, cell, p)
	fillCells(pt, jackStem[p.step], 'g', jackStemColor, x0, y0, cell)
	by := y0 + 2*cell
	fillCells(pt, jackBody, 'o', pal.accent, x0, by, cell)
	fillCells(pt, jackBody, 'd', pal.accent.Mix(black, 0.2), x0, by, cell)
	// The eyes, two cells square, and the nose under them, follow a
	// glance; the mouth, with two teeth left in it, does not.
	eyeCells(pt, [2]int{3, 9}, 4, 2, jackGlow, x0, y0, cell, p)
	eyeCells(pt, [2]int{4, 10}, 4, 2, jackGlow, x0, y0, cell, p)
	look := float32(p.look) * cell
	pt.Fill(ui.Rect{X: x0 + 6*cell + look, Y: y0 + 6*cell, W: 2 * cell, H: cell}, jackGlow, 0)
	fillCells(pt, []string{"...oo.oo.oo...", "....oooooo...."}, 'o', jackGlow, x0, y0+7*cell, cell)
}

// The cauldron's bubbles, which move at each step of the hop and as it
// blinks, its brew and rim, and its pot on two legs.
var (
	cauldronBubbles = [2][]string{
		{"....l....l....", "......l..l...."},
		{"......l...l...", "...l....l....."},
	}
	cauldronPot = []string{
		"..bbbbbbbbbb..",
		"rrrrrrrrrrrrrr",
		".pppppppppppp.",
		"pppppppppppppp",
		"pppppppppppppp",
		"pppppppppppppp",
		".pppppppppppp.",
		"..pppppppppp..",
		"..p........p..",
	}
	cauldronIron = ui.Hex("#5a4a72")
	cauldronRim  = ui.Hex("#7a6896")
)

const cauldronCols, cauldronRows = 14, 11

func cauldronSize(cell float32) (w, h float32) {
	return cauldronCols * cell, (cauldronRows + 2) * cell
}

// drawCauldron paints the cauldron in r, centered, in cells of a width, in
// a pose: an iron pot of brew in the accent, with eyes that glow as the
// brew does.
func drawCauldron(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	x0, y0 := halloweenOrigin(r, cauldronCols, cauldronRows, cell, p)
	v := p.step
	if p.blink {
		v = 1 - v
	}
	fillCells(pt, cauldronBubbles[v], 'l', pal.accent.Mix(white, 0.45), x0, y0, cell)
	py := y0 + 2*cell
	fillCells(pt, cauldronPot, 'b', pal.accent, x0, py, cell)
	fillCells(pt, cauldronPot, 'r', cauldronRim, x0, py, cell)
	fillCells(pt, cauldronPot, 'p', cauldronIron, x0, py, cell)
	eyeCells(pt, [2]int{4, 9}, 5, 2, pal.accent, x0, y0, cell, p)
}

// The bat's wings, up and then down at each step of the hop, so that it
// flaps while it hops.
var batWings = [2][]string{
	{
		"b..............b",
		"bb....b..b....bb",
		"bbb...bbbb...bbb",
		"bbbb.bbbbbb.bbbb",
		"bbbbbbbbbbbbbbbb",
		".bbbbbbbbbbbbbb.",
		"..b..bbbbbb..b..",
		"......bbbb......",
	},
	{
		"................",
		"......b..b......",
		"......bbbb......",
		"....bbbbbbbb....",
		".bbbbbbbbbbbbbb.",
		"bbbbbbbbbbbbbbbb",
		"bbb..bbbbbb..bbb",
		"b.....bbbb.....b",
	},
}

var batEyes = ui.Hex("#ffe3e8")

const batCols, batRows = 16, 8

func batSize(cell float32) (w, h float32) {
	return batCols * cell, (batRows + 2) * cell
}

// drawBat paints the bat in r, centered, in cells of a width, in a pose:
// a bat in the accent, with pale eyes.
func drawBat(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	x0, y0 := halloweenOrigin(r, batCols, batRows, cell, p)
	fillCells(pt, batWings[p.step], 'b', pal.accent, x0, y0, cell)
	eyeCells(pt, [2]int{6, 9}, 3, 1, batEyes, x0, y0, cell, p)
}
