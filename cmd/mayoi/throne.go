package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The apse of Purple Gold, behind the welcome page: a mosaic of purple
// tesserae with an apse of gold ones at its middle, thinning toward its
// rim, and a band of light that crosses it corner to corner. Like the
// rain, it steps rather than glides, and holds still while the window has
// not the keyboard.
const (
	// throneFont is the welcome page's name under Purple Gold: Optima,
	// whose letters come from Roman inscriptions, or a serif without it.
	throneFont = "Optima, Candara, serif"
	throneTile = 16
	throneStep = 120 * time.Millisecond
)

// throneState is where the light is: step is the moment it shows, which
// stays while it is still, and moving whether the last paint asked for
// the next.
type throneState struct {
	step   int64
	moving bool
}

var (
	throneAmethyst = ui.Hex("#a659dc")
	thronePurples  = [2]ui.Color{ui.Hex("#6a2a92"), ui.Hex("#45175f")}
)

// drawThrone paints the apse over r. still holds it at the moment it
// shows, asking for no more frames: while the window has not the
// keyboard, or the desktop asks for less motion.
func (a *app) drawThrone(pt *ui.Painter, r ui.Rect, still bool) {
	ts := &a.throne
	if ts.moving = !still; ts.moving {
		ts.step = pt.Now().UnixMilli() / throneStep.Milliseconds()
		pt.After(throneStep)
	}
	p := a.pal
	gilt := p.gilt
	if gilt.A == 0 {
		gilt = p.accent.Mix(white, 0.5)
	}
	shade := p.accent.Mix(black, 0.35)
	// The apse rises behind the welcome page's card, which is centered.
	cx, cy := r.W/2, r.H/2-60
	rad := float32(math.Max(260, math.Min(380, float64(max(r.W, r.H))*0.32)))
	cols, rows := int(r.W/throneTile)+1, int(r.H/throneTile)+1
	// The band of light, along x+y, from before the top left corner to
	// past the bottom right.
	span := int64(r.W + r.H + 900)
	band := float32(ts.step*12%span) - 300
	for j := range rows {
		for i := range cols {
			h := hash3(uint32(i), uint32(j), 1)
			k := float32(h%1000) / 1000
			x, y := float32(i)*throneTile, float32(j)*throneTile
			dx, dy := x+throneTile/2-cx, y+throneTile/2-cy
			d := float32(math.Sqrt(float64(dx*dx+dy*dy))) / rad
			lit := max(0, 1-float32(math.Abs(float64(x+y-band)))/110)
			var col ui.Color
			var alpha float32
			if d < 1 {
				near := float32(math.Pow(float64(1-d), 0.45))
				switch {
				case k < 0.08:
					col = throneAmethyst
				case k < 0.6:
					col = p.accent
				default:
					col = gilt
				}
				alpha = (0.08+0.46*near)*(0.45+0.75*k) + lit*0.4*(0.3+k)
			} else {
				switch {
				case k < 0.55:
					col = thronePurples[0]
				case k < 0.9:
					col = thronePurples[1]
				default:
					col = shade
				}
				alpha = 0.08 + 0.1*k + lit*0.1
			}
			// Each tessera set a little off true, as a mosaic's are.
			jx := (float32(hash3(uint32(i), uint32(j), 2)%100)/100 - 0.5) * 1.6
			jy := (float32(hash3(uint32(i), uint32(j), 3)%100)/100 - 0.5) * 1.6
			pt.Fill(ui.Rect{X: r.X + x + 2 + jx, Y: r.Y + y + 2 + jy, W: throneTile - 4, H: throneTile - 4}, col.Alpha(min(alpha, 0.8)), 0)
		}
	}
	// The apse's rim, twice, in gold.
	pt.Stroke(ui.Rect{X: r.X + cx - rad, Y: r.Y + cy - rad, W: 2 * rad, H: 2 * rad}, p.accent.Alpha(0.4), rad, 1.5)
	pt.Stroke(ui.Rect{X: r.X + cx - rad - 12, Y: r.Y + cy - rad - 12, W: 2 * (rad + 12), H: 2 * (rad + 12)}, p.accent.Alpha(0.16), rad+12, 1)
	// Fading into the editor at the foot, so the mosaic stays behind.
	foot := ui.Rect{X: r.X, Y: r.Y + r.H*0.55, W: r.W, H: r.H * 0.45}
	pt.FillGradient(foot, ui.LinearGradient{From: p.editor.Alpha(0), To: p.editor.Alpha(0.85), Angle: 180}, 0)
}
