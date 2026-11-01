package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The rain of The Matrix, behind the welcome page: columns of glyphs that
// fall at their own speeds, the head of each white-hot and its trail
// fading, mirrored as the film's are, with a far layer, smaller, dimmer
// and slower, behind the near one. It steps rather than glides, as the
// film's does, which also spares the GPU frames.
const (
	rainCellW = 16
	rainCellH = 18
	rainSize  = 13
	rainStep  = 70 * time.Millisecond
)

// rainLayer is a depth of the rain: its size and brightness beside the
// near layer's, its speed, and a seed that sets its columns apart.
type rainLayer struct {
	scale, alpha, speed float32
	seed                uint32
}

// rainLayers are the rain's depths, the far one first.
var rainLayers = []rainLayer{
	{scale: 0.75, alpha: 0.45, speed: 0.6, seed: 7919},
	{scale: 1, alpha: 1, speed: 1},
}

// rainGlyphs are what the rain falls in: half-width katakana and digits.
var rainGlyphs = func() []string {
	var gs []string
	for r := rune(0xff66); r <= 0xff9d; r++ {
		gs = append(gs, string(r))
	}
	for r := '0'; r <= '9'; r++ {
		gs = append(gs, string(r))
	}
	return gs
}()

// rainState is where the rain is: step is the moment it shows, which
// stays while it is still, and moving whether the last paint asked for
// the next.
type rainState struct {
	step   int64
	moving bool
}

// hash3 mixes three numbers into one, for the rain's columns and glyphs
// to differ without a random source, so that a moment paints the same
// each time.
func hash3(a, b, c uint32) uint32 {
	h := a*0x9e3779b1 ^ b*0x85ebca77 ^ c*0xc2b2ae3d
	h ^= h >> 15
	h *= 0x2c1b3c6d
	h ^= h >> 12
	h *= 0x297a2d39
	h ^= h >> 15
	return h
}

// drawRain paints the rain over r. still holds it at the moment it shows,
// asking for no more frames: while the window has not the keyboard, or
// the desktop asks for less motion.
func (a *app) drawRain(pt *ui.Painter, r ui.Rect, still bool) {
	rs := &a.rain
	if rs.moving = !still; rs.moving {
		rs.step = pt.Now().UnixMilli() / rainStep.Milliseconds()
		pt.After(rainStep)
	}
	p := a.pal
	head := p.accent.Mix(white, 0.85)
	for _, l := range rainLayers {
		cw, ch := rainCellW*l.scale, rainCellH*l.scale
		cols, rows := int(r.W/cw)+1, int(r.H/ch)+1
		for i := range cols {
			ci := uint32(i) + l.seed
			// Each column has its speed, its trail and a pause between its
			// drops, which makes the screen cycle out of step.
			speed := (0.25 + float64(hash3(ci, 1, 0)%1000)/1000*0.75) * float64(l.speed)
			trail := 6 + int(hash3(ci, 2, 0)%18)
			cycle := rows + trail + int(hash3(ci, 3, 0)%uint32(rows+1))
			at := int(float64(rs.step)*speed+float64(hash3(ci, 4, 0)%997)) % cycle
			x := r.X + float32(i)*cw
			// The column is mirrored about its middle, as the film's glyphs are.
			pt.Transform(ui.Scaling(-1, 1), x+cw/2, 0, func() {
				for k := range trail {
					row := at - k
					if row < 0 || row >= rows {
						continue
					}
					// A glyph changes now and then where it is, the head each step.
					tick := uint32(rs.step / int64(4+hash3(ci, uint32(row), 5)%12))
					if k == 0 {
						tick = uint32(rs.step)
					}
					g := rainGlyphs[hash3(ci, uint32(row), tick)%uint32(len(rainGlyphs))]
					col := head.Alpha(0.9 * l.alpha)
					if k > 0 {
						col = p.accent.Alpha(0.42 * l.alpha * (1 - float32(k)/float32(trail)))
					}
					pt.Text(x, r.Y+float32(row)*ch, g, rainSize*l.scale, col)
				}
			})
		}
	}
}
