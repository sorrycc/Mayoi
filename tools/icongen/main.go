// Command icongen places Mayoi's app icon art, cmd/mayoi/icon-art.png, on
// the macOS icon grid and writes resources/icon.png, and the Default the
// settings put back on Windows and Linux, cmd/mayoi/appicon-default.png:
//
//	go run ./tools/icongen
//
// With -amber, it places the art of the Amber icon, a tile already drawn
// with rounder corners, on the same grid as cmd/mayoi/appicon-amber.png:
//
//	go run ./tools/icongen -amber 04-Amber.png
package main

import (
	"flag"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
)

// frame is the width of the art's own pixel tile border (a dark outline
// and a light bevel), in pixels of the 1254 px art.
const frame = 52

func main() {
	amber := flag.String("amber", "", "the art of the Amber icon")
	flag.Parse()
	if *amber != "" {
		art := decode(*amber)
		// The size the Dock draws it at most, on a Retina display.
		write("cmd/mayoi/appicon-amber.png", ui.Render(grid(ui.NewBitmap(tile(art)), amberRadius), 1024, 1024, 0.5))
		return
	}
	art := decode("cmd/mayoi/icon-art.png")
	// macOS 26 shrinks an icon that does not fit its rounded tile onto a
	// gray frame, and the art's tile has square, stepped corners. So the
	// border is cropped off and the inside clipped to the tile instead.
	b := art.Bounds()
	inside := art.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(b.Inset(frame))
	view := grid(ui.NewBitmap(inside), 185)
	write("resources/icon.png", ui.Render(view, 1024, 1024, 1))
	// Large enough for a taskbar's and a title bar's icons.
	write("cmd/mayoi/appicon-default.png", ui.Render(view, 1024, 1024, 0.25))
}

// amberRadius is the radius of the corners of the Amber art's own tile, a
// little rounder than the grid's, in DIPs of the grid.
const amberRadius = 224

// grid places bitmap on the macOS icon grid, an 824 DIP square on a 1024
// canvas, its corners clipped to radius.
func grid(bitmap *ui.Bitmap, radius float32) func(c *ui.Context) {
	return func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Column(c).Fill().Center().Children(func() {
			ui.Column(c).Size(824, 824).Radius(radius).Clip().
				Shadow(0, 18, 40, 0, ui.RGBA(0, 0, 0, 0.35)).
				Children(func() {
					ui.Image(c, bitmap).Size(824, 824)
				})
		})
	}
}

// tile is the drawn tile of art: what is nearly opaque, without the specks
// and glow around it, made opaque, as the art is not quite.
func tile(art image.Image) image.Image {
	b := art.Bounds()
	box := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := art.At(x, y).RGBA(); a>>8 > 250 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Draw(out, out.Bounds(), art, box.Min, draw.Src)
	for i := 3; i < len(out.Pix); i += 4 {
		if out.Pix[i] >= 245 {
			out.Pix[i] = 255
		}
	}
	return out
}

func decode(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	return img
}

func write(path string, img *image.RGBA) {
	out, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	// An *image.RGBA encodes as an 8-bit PNG; macOS 26 frames an icns of
	// 16-bit ones as a legacy icon.
	if err := png.Encode(out, img); err != nil {
		log.Fatal(err)
	}
}
