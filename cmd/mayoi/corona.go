package main

import (
	"github.com/egoist/mygo/ui"
)

// Corona, the character of Purple Gold: a living crown in gold leaf, its
// light side gilt, set with amethysts on its points and band and a ruby at
// the band's middle. Its eyes blink and glance, and its jewels flash at
// each step of the hop.

// coronaArt is the crown, a cell to a character: g its gold, h the gilt of
// its left side, d the shade of its band, j and p its amethysts, r its
// ruby and m its mouth. The eyes are drawn over it.
var coronaArt = []string{
	".j....jj....j.",
	".g....gg....g.",
	".hg..gggg..gg.",
	".hggggggggggg.",
	".hggggggggggg.",
	".hggggggggggg.",
	".gggggmmggggg.",
	".dddddddddddd.",
	".dpdddrrdddpd.",
	".dddddddddddd.",
}

var (
	coronaAmethyst = ui.Hex("#a659dc")
	coronaRuby     = ui.Hex("#f2667a")
	coronaMouth    = ui.Hex("#5a1a6e")
	coronaEyes     = ui.Hex("#2a0a38")
	coronaFlash    = ui.Hex("#fff6cf")
)

const coronaCols, coronaRows = 14, 10

func coronaSize(cell float32) (w, h float32) {
	return coronaCols * cell, (coronaRows + 2) * cell
}

// drawCorona paints Corona in r, centered, in cells of a width, in a
// pose: a crown in the accent, gilt on its left, whose top jewels flash
// at the second step of the hop.
func drawCorona(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	x0, y0 := halloweenOrigin(r, coronaCols, coronaRows, cell, p)
	gilt := pal.gilt
	if gilt.A == 0 {
		gilt = pal.accent.Mix(white, 0.5)
	}
	jewel := coronaAmethyst
	if p.step == 1 {
		jewel = coronaFlash
	}
	fillCells(pt, coronaArt, 'g', pal.accent, x0, y0, cell)
	fillCells(pt, coronaArt, 'h', gilt, x0, y0, cell)
	fillCells(pt, coronaArt, 'd', pal.accent.Mix(black, 0.35), x0, y0, cell)
	fillCells(pt, coronaArt, 'j', jewel, x0, y0, cell)
	fillCells(pt, coronaArt, 'p', coronaAmethyst, x0, y0, cell)
	fillCells(pt, coronaArt, 'r', coronaRuby, x0, y0, cell)
	fillCells(pt, coronaArt, 'm', coronaMouth, x0, y0, cell)
	eyeCells(pt, [2]int{4, 9}, 4, 2, coronaEyes, x0, y0, cell, p)
}

// drawGilt paints the line along the top of the tab shown in a theme with
// gilt: gold leaf, deep at its ends and catching the light at its middle,
// where a gem is set, of which the tab shows the lower half.
func drawGilt(pt *ui.Painter, r ui.Rect, pal *palette) {
	deep, mid := pal.accent.Mix(black, 0.35), r.X+r.W/2
	pt.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W / 2, H: r.H}, ui.LinearGradient{From: deep, To: pal.gilt, Angle: 90}, 0)
	pt.FillGradient(ui.Rect{X: mid, Y: r.Y, W: r.W / 2, H: r.H}, ui.LinearGradient{From: pal.gilt, To: deep, Angle: 90}, 0)
	const g = 4
	var gem ui.Path
	gem.MoveTo(mid-g, r.Y).LineTo(mid, r.Y-g).LineTo(mid+g, r.Y).LineTo(mid, r.Y+g).Close()
	pt.FillPath(&gem, pal.gilt)
}
