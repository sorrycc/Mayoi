package main

import (
	"image"
	"math"
	"math/rand/v2"
	"sync"

	"github.com/egoist/mygo/ui"
)

// In a theme with a card, Qoder's, the window is laid out as Qoder's
// workbench is: the editors and the panel below them sit in a card with
// rounded corners, a hairline and a soft shadow toward the sidebar, and
// the title bar, the sidebar and the status bar around it are its shell,
// in the chrome, grained, with no lines between them. A splitter's line
// lights around the pointer rather than all along.

// workbenchGap is how far the card is from the window's edge beside it.
const workbenchGap = 6

// asCard makes main the card, in a theme with one: inset from the
// window's left edge when nothing is left of it, and from its right when
// nothing is right of it.
func (a *app) asCard(main ui.Element, left, right bool) {
	p := a.pal
	if p.card == 0 {
		return
	}
	var l, r float32
	if left {
		l = workbenchGap
	}
	if right {
		r = workbenchGap
	}
	main.Margin(0, r, 0, l).Radius(p.card).Clip().Border(1, p.border).Shadow(-4, 0, 12, -8, p.shadow)
}

// grainTile is the side of a tile of the grain, in points. Its bitmap has
// two pixels to a point.
const grainTile = 128

// grainTiles are the tiles of grain made, by their strength.
var grainTiles sync.Map

// grainBitmap returns the tile of grain of a strength: specks of black,
// most of them faint and a few darker, as Qoder's noise is, the same each
// time.
func grainBitmap(strength float32) *ui.Bitmap {
	if b, ok := grainTiles.Load(strength); ok {
		return b.(*ui.Bitmap)
	}
	const n = 2 * grainTile
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	r := rand.New(rand.NewPCG(1, 2))
	for i := 3; i < len(img.Pix); i += 4 {
		u := r.Float32()
		img.Pix[i] = uint8(u * u * u * 138 * strength)
	}
	b, _ := grainTiles.LoadOrStore(strength, ui.NewBitmap(img))
	return b.(*ui.Bitmap)
}

// grained paints the shell's grain over e, a part of the shell, in tiles
// laid from the window's corner so that they meet across its parts.
func (a *app) grained(e ui.Element) {
	p := a.pal
	if p.card == 0 || p.grain == 0 {
		return
	}
	tile := grainBitmap(p.grain)
	e.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		pt.Clip(r, 0, func() {
			x0 := float32(math.Floor(float64(r.X/grainTile))) * grainTile
			y0 := float32(math.Floor(float64(r.Y/grainTile))) * grainTile
			for y := y0; y < r.Y+r.H; y += grainTile {
				for x := x0; x < r.X+r.W; x += grainTile {
					pt.Image(tile, ui.Rect{X: x, Y: y, W: grainTile, H: grainTile}, ui.FillBox)
				}
			}
		})
	})
}

// seamReach is how far a splitter's line lights either way of the
// pointer, around a card.
const seamReach = 90

// drawSeamLight paints a splitter's line in r lit around the pointer, at
// at along it: across the window when across, else down it.
func drawSeamLight(pt *ui.Painter, r ui.Rect, across bool, at float32, col ui.Color) {
	g := ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: col.Alpha(0), At: 0}, {Color: col, At: 0.5}, {Color: col.Alpha(0), At: 1}}}
	lit := r
	if across {
		g.Angle = 90
		lit.X, lit.W = at-seamReach, 2*seamReach
	} else {
		lit.Y, lit.H = at-seamReach, 2*seamReach
	}
	pt.Clip(r, 0, func() { pt.FillGradient(lit, g, 0) })
}
