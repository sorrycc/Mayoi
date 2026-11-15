package main

import (
	"math"
	"math/bits"

	"github.com/egoist/mygo/ui"
)

// The wanted level, the character of GTA VI: five stars, as many of them
// lit as the files that differ from HEAD call for, 1, 2 to 3, 4 to 7, 8 to
// 15, or 16 and more. While Git works they flash, as the stars do while
// the police search, in place of the hop, and a commit puts them out.
// With none lit, the first flashes.

const (
	wantedStars = 5
	// A star is wantedStar cells wide, with a cell between two.
	wantedStar = 6
	wantedCols = wantedStars*wantedStar + wantedStars - 1
	wantedRows = wantedStar

	wantedMinCell = 1.6
)

var wantedFlash = ui.Hex("#7d8796")

// wantedLevel is how many stars n files that differ from HEAD light.
func wantedLevel(n int) int {
	return min(bits.Len(uint(max(n, 0))), wantedStars)
}

// wantedCell is the cell the stars are drawn in for a cell of a width: no
// less than wantedMinCell, as in the status bar's a star would be a dot.
func wantedCell(cell float32) float32 {
	return max(cell, wantedMinCell)
}

func wantedSize(cell float32) (w, h float32) {
	cell = wantedCell(cell)
	return wantedCols * cell, (wantedRows + 2) * cell
}

// drawWanted paints the stars in r, centered, in cells of a width, in a
// pose: white for those lit, faint for the others, and grey at the
// second step of the hop for those that flash.
func drawWanted(pal *palette, pt *ui.Painter, r ui.Rect, cell float32, _ ui.Color, p mascotPose) {
	cell = wantedCell(cell)
	fw, fh := float32(wantedCols)*cell, float32(wantedRows)*cell
	x0, y0 := r.X+(r.W-fw)/2, r.Y+(r.H-fh)/2
	R := float32(wantedStar) * cell / 2
	flashing := max(p.level, 1)
	for i := range wantedStars {
		col := pal.text.Alpha(0.18)
		if i < p.level {
			col = white
		}
		if p.step == 1 && i < flashing {
			col = wantedFlash
		}
		cx := x0 + float32(i*(wantedStar+1))*cell + R
		pt.FillPath(starPath(cx, y0+R*1.05, R), col)
	}
}

// starPath is a five-pointed star, point up, of a radius about a center.
func starPath(cx, cy, r float32) *ui.Path {
	var path ui.Path
	for i := range 10 {
		k := r
		if i%2 == 1 {
			k = r * 0.45
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		x, y := cx+k*float32(math.Cos(a)), cy+k*float32(math.Sin(a))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	path.Close()
	return &path
}
