package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// imageExts are the files a viewer shows: those MyGo decodes.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

// isImage reports whether a file is a picture the viewer shows, by its
// extension.
func isImage(path string) bool { return imageExts[strings.ToLower(filepath.Ext(path))] }

// isSVG reports whether a document is an SVG, which previews as a
// picture.
func isSVG(d *document) bool {
	return d.path != "" && strings.EqualFold(filepath.Ext(d.path), ".svg")
}

// maxBitmapSide is the longest side of a bitmap drawn: a larger picture
// is scaled down to it as it is decoded, as textures have a limit.
const maxBitmapSide = 8192

// maxSVGSide is the longest side an SVG is drawn at, in DIPs: MyGo draws
// an SVG whole, and not past 4096 pixels, which is this on a 2× display.
const maxSVGSide = 2048

// zoomSteps are the scales zooming in and out goes through.
var zoomSteps = []float32{0.05, 0.1, 0.25, 1.0 / 3, 0.5, 2.0 / 3, 1, 1.5, 2, 3, 4, 6, 8, 12, 16, 24, 32}

// viewer shows a picture, fitted to its pane until zoomed, and moved by
// dragging it once it is larger: an image file in a tab of its own, or
// the preview of an SVG or Markdown document.
type viewer struct {
	path string // the image file; "" for a document preview
	src  ui.ImageSource
	// w and h are the picture's size in DIPs, a pixel each for an image,
	// and size the bytes of the file.
	w, h float32
	size int64
	err  string
	// loaded is set once the picture was read, or failed to be.
	loaded bool
	// zoom is the scale it shows at, 0 for fitted to the pane; panX and
	// panY move its middle from the pane's.
	zoom       float32
	panX, panY float32
	// pane is the box it shows in, and paneW and paneH its size as the
	// last frame laid it out.
	pane         ui.Handle
	paneW, paneH float32
	// grab is where the pointer pressed on it, while it drags it.
	grab     bool
	grabX    float32
	grabY    float32
	diskGone bool
	// loading counts the reads of the file, so that an older one coming
	// late is dropped.
	loading int

	// doc is the document an SVG or Markdown preview shows, version the
	// version of its text parsed last, and wait the timer of a parse after
	// an edit.
	doc     *document
	version uint64
	wait    *time.Timer
	// markdown is a Markdown preview's parsed text and place, nil for
	// an image or an SVG.
	markdown *markdownPreview
}

// newImageViewer makes the viewer of an image file, which load reads.
func newImageViewer(path string) *viewer { return &viewer{path: path} }

// newPreviewViewer makes the preview of an SVG or Markdown document.
func newPreviewViewer(d *document) *viewer { return &viewer{doc: d} }

func (v *viewer) name() string { return filepath.Base(v.path) }

// loadImage reads and decodes the image file in the background, keeping the
// picture shown until the new one is ready.
func (a *app) loadImage(v *viewer) {
	v.loading++
	n, path := v.loading, v.path
	bg.Go(func() {
		defer recoverCrash()
		bm, w, h, size, err := decodeImage(path)
		a.post(func() {
			if v.loading != n {
				return
			}
			v.loaded = true
			if err != nil {
				v.err = err.Error()
				return
			}
			v.err, v.size = "", size
			if v.src != nil && (v.w != float32(w) || v.h != float32(h)) {
				v.zoom, v.panX, v.panY = 0, 0, 0 // another picture: fit it
			}
			// Zoomed in, its pixels show as squares, to be looked at one
			// by one, as in an image editor; smaller, it is smooth. Not
			// a picture decoded smaller than it is, which would show its
			// squares at its own size.
			if bw, bh := bm.Size(); bw == w && bh == h {
				bm.Pixelated()
			}
			v.src, v.w, v.h = bm, float32(w), float32(h)
		})
	})
}

// decodeImage reads an image file into a bitmap, at most maxBitmapSide
// on a side, and returns its own size.
func decodeImage(path string) (bm *ui.Bitmap, w, h int, size int64, err error) {
	data, err := readImage(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	bm, w, h, err = decodeImageData(data)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return bm, w, h, int64(len(data)), nil
}

// decodeImageData decodes an image into a bitmap, at most maxBitmapSide
// on a side, and returns its own size.
func decodeImageData(data []byte) (bm *ui.Bitmap, w, h int, err error) {
	bm, err = ui.DecodeBitmap(data)
	if errors.Is(err, image.ErrFormat) {
		return nil, 0, 0, errors.New(locales.ErrNotImage())
	}
	if err != nil {
		return nil, 0, 0, err
	}
	w, h = bm.Size()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New(locales.ErrImageEmpty())
	}
	if side := max(w, h); side > maxBitmapSide {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, err
		}
		s := float64(maxBitmapSide) / float64(side)
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*s)), max(1, int(float64(h)*s))))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
		bm = ui.NewBitmap(dst)
	}
	return bm, w, h, nil
}

// readImage reads an image file that a document names, so may be a FIFO
// or a device: it opens without blocking, takes only a regular file, as
// the file opened says, and reads at most maxOpenBytes of it.
func readImage(path string) ([]byte, error) {
	if remote.Is(path) {
		data, err := vfs.ReadFileLimit(path, maxOpenBytes)
		if errors.Is(err, remote.ErrTooLarge) {
			err = errors.New(locales.ErrImageTooLarge())
		}
		return data, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(locales.ErrImageNotRegular())
	}
	if info.Size() > maxOpenBytes {
		return nil, errors.New(locales.ErrImageTooLarge())
	}
	data, err := io.ReadAll(io.LimitReader(f, maxOpenBytes+1))
	if err == nil && int64(len(data)) > maxOpenBytes {
		err = errors.New(locales.ErrImageTooLarge())
	}
	return data, err
}

// svgDelay is how long after an edit an SVG's preview parses its text.
const svgDelay = 250 * time.Millisecond

// followSVG parses the text of an SVG's preview again once it changed,
// a moment after the last edit, keeping the last picture that parsed.
func (a *app) followSVG(v *viewer) {
	d := v.doc
	ver := d.buf.Version()
	if v.loaded && ver == v.version || v.wait != nil {
		return
	}
	parse := func() {
		v.wait = nil
		if !slices.Contains(a.docs, d) {
			return
		}
		v.version = d.buf.Version()
		v.loaded = true
		s, err := ui.ParseSVG([]byte(d.buf.String()))
		if err != nil {
			v.err = err.Error()
			return
		}
		w, h := s.Size()
		if w <= 0 || h <= 0 {
			v.err = locales.ErrSVGNoSize()
			return
		}
		v.err = ""
		if v.src != nil && (v.w != w || v.h != h) {
			v.zoom, v.panX, v.panY = 0, 0, 0
		}
		v.src, v.w, v.h = s, w, h
	}
	if !v.loaded {
		parse() // at once, as it opens
		return
	}
	v.wait = time.AfterFunc(svgDelay, func() { a.post(parse) })
}

// stop stops what the viewer waits for, as it goes.
func (v *viewer) stop() {
	if v.wait != nil {
		v.wait.Stop()
		v.wait = nil
	}
	v.loading++
	if v.markdown != nil {
		v.markdown.generation.Add(1)
	}
}

// fitMargin is the room a fitted picture leaves around it, in a pane
// that has room for it.
const fitMargin = 24

// fitScale is the scale that fits the picture in a pane w×h, never above
// its own size, with fitMargin around it.
func (v *viewer) fitScale(w, h float32) float32 {
	if v.w <= 0 || v.h <= 0 || w <= 0 || h <= 0 {
		return 1
	}
	if w > 8*fitMargin && h > 8*fitMargin {
		w, h = w-2*fitMargin, h-2*fitMargin
	}
	return min(1, w/v.w, h/v.h)
}

// maxScale is the largest the picture zooms to.
func (v *viewer) maxScale() float32 {
	if _, ok := v.src.(*ui.SVG); ok {
		return max(maxSVGSide/max(v.w, v.h), 1)
	}
	return zoomSteps[len(zoomSteps)-1]
}

// scaleIn is the scale the picture shows at in a pane w×h.
func (v *viewer) scaleIn(w, h float32) float32 {
	if v.zoom == 0 {
		return v.fitScale(w, h)
	}
	return v.zoom
}

// place returns where the picture shows in a pane w×h, from its top
// left, keeping it from being dragged out of the pane.
func (v *viewer) place(w, h float32) ui.Rect {
	s := v.scaleIn(w, h)
	iw, ih := v.w*s, v.h*s
	v.panX = clamp(v.panX, -max(iw-w, 0)/2, max(iw-w, 0)/2)
	v.panY = clamp(v.panY, -max(ih-h, 0)/2, max(ih-h, 0)/2)
	return ui.Rect{X: (w-iw)/2 + v.panX, Y: (h-ih)/2 + v.panY, W: iw, H: ih}
}

// zoomAt zooms to scale s, keeping the point x, y of the pane w×h where it
// is on the picture.
func (v *viewer) zoomAt(s, x, y, w, h float32) {
	old := v.scaleIn(w, h)
	s = clamp(s, zoomSteps[0], v.maxScale())
	// The point, from the picture's middle.
	dx, dy := x-w/2-v.panX, y-h/2-v.panY
	v.panX = x - w/2 - dx*s/old
	v.panY = y - h/2 - dy*s/old
	v.zoom = s
}

// zoomBy zooms a step in, or out, around the middle of the pane, or back
// to fitted for 0.
func (v *viewer) zoomBy(step int) {
	w, h := v.paneW, v.paneH
	if step == 0 || v.src == nil {
		v.zoom, v.panX, v.panY = 0, 0, 0
		return
	}
	now := v.scaleIn(w, h)
	next := now
	if step > 0 {
		for _, s := range zoomSteps {
			if s > now*1.001 {
				next = s
				break
			}
		}
	} else {
		for i := len(zoomSteps) - 1; i >= 0; i-- {
			if zoomSteps[i] < now*0.999 {
				next = zoomSteps[i]
				break
			}
		}
	}
	v.zoomAt(next, w/2, h/2, w, h)
}

// viewerInput zooms the picture by scrolling with Cmd or Ctrl held, and
// moves it by scrolling or dragging. A double click goes between fitted
// and its own size.
func (a *app) viewerInput(v *viewer, ev ui.InputEvent) bool {
	w, h := v.paneW, v.paneH
	if v.src == nil {
		return false
	}
	switch ev.Kind {
	case ui.InputScroll:
		if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
			by := ev.DY
			if !ev.Precise {
				by *= 8
			}
			v.zoomAt(v.scaleIn(w, h)*float32(math.Exp(float64(-by)/200)), ev.X, ev.Y, w, h)
			return true
		}
		v.panX -= ev.DX
		v.panY -= ev.DY
		return true
	case ui.InputPointerDown:
		v.pane.Focus()
		if ev.Button != 0 {
			return false
		}
		if ev.Clicks == 2 {
			if v.zoom == 0 && v.fitScale(w, h) < 1 {
				v.zoomAt(1, ev.X, ev.Y, w, h)
			} else {
				v.zoom, v.panX, v.panY = 0, 0, 0
			}
			return true
		}
		v.grab, v.grabX, v.grabY = true, ev.X, ev.Y
		return true
	case ui.InputPointerMove:
		if v.grab && ev.Button == 0 {
			v.panX += ev.X - v.grabX
			v.panY += ev.Y - v.grabY
			v.grabX, v.grabY = ev.X, ev.Y
			return true
		}
	case ui.InputPointerUp:
		if v.grab {
			v.grab = false
			return true
		}
	}
	return false
}

// viewerView is the pane of a viewer: the picture on a checkerboard,
// which shows what is transparent, or what kept it from showing.
func (a *app) viewerView(c *ui.Context, v *viewer, name string) {
	if v.markdown != nil {
		a.markdownView(c, v, name)
		return
	}
	p := a.pal
	label := locales.ImageLabel(name)
	if v.doc != nil {
		label = locales.PreviewLabel(name)
	}
	if v.doc != nil {
		a.followSVG(v)
	}
	b := v.pane.Bounds(c)
	v.paneW, v.paneH = b.W, b.H
	pane := ui.Box(c.Key("viewer")).Bind(&v.pane).Fill().Background(p.editor).Clip().Focusable().FocusRing(false).
		Label(label)
	a.takeKeys(pane, placeViewer, func(ev ui.InputEvent) bool { return a.viewerInput(v, ev) })
	switch {
	case v.grab:
		pane.Cursor(ui.CursorGrabbing)
	case v.src != nil:
		if r := v.place(b.W, b.H); r.W > b.W+0.5 || r.H > b.H+0.5 {
			pane.Cursor(ui.CursorGrab)
		}
	}
	pane.Draw(func(pt *ui.Painter, r ui.Rect) {
		if v.src == nil {
			return
		}
		at := v.place(r.W, r.H)
		at.X, at.Y = r.X+at.X, r.Y+at.Y
		shown := intersect(at, r)
		if shown.W <= 0 || shown.H <= 0 {
			return
		}
		if at.W < r.W && at.H < r.H {
			// A soft shadow lifts a picture that fits off the pane.
			pt.Shadow(at, 0, 0, 6, 24, 0, p.shadow)
		}
		pt.Clip(shown, 0, func() {
			// The checks start at the picture's corner, as it moves.
			tile := checkers(p.dark)
			for y := at.Y + float32(int((shown.Y-at.Y)/checkTile))*checkTile; y < shown.Y+shown.H; y += checkTile {
				for x := at.X + float32(int((shown.X-at.X)/checkTile))*checkTile; x < shown.X+shown.W; x += checkTile {
					pt.Image(tile, ui.Rect{X: x, Y: y, W: checkTile, H: checkTile}, ui.FillBox)
				}
			}
			pt.Image(v.src, at, ui.FillBox)
		})
		if at.W < r.W && at.H < r.H {
			// A hairline sets the picture off from the pane, as the
			// checks would not where it is transparent at its edges.
			pt.Stroke(ui.Rect{X: at.X - 1, Y: at.Y - 1, W: at.W + 2, H: at.H + 2}, p.border, 0, 1)
		}
	})
	pane.Children(func() {
		switch {
		case !v.loaded:
		case v.src == nil:
			msg := locales.CannotShow(name)
			if v.err != "" {
				msg = locales.CannotShowWhy(name, v.err)
			}
			ui.Column(c).Fill().Center().Padding(24).Children(func() {
				ui.Icon(c, iconImage).FontSize(32).TextColor(p.faint)
				ui.Text(c, msg).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).Margin(12, 0, 0, 0).MaxWidth(420)
			})
		case v.err != "":
			// The picture that parsed last stays, with what is wrong now.
			ui.Row(c).Absolute().Top(8).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
				ui.Text(c, v.err).FontSize(12).TextColor(p.dangerFg).SingleLine().Padding(4, 10).Radius(6).
					Background(p.chrome).Border(1, p.border).MaxWidthPercent(100)
			})
		}
	})
}

// checkTile is the size of the checkerboard's tile, in DIPs.
const checkTile = 256

// checkers returns the checkerboard's tile, of checks 8 DIPs wide.
func checkers(dark bool) *ui.Bitmap {
	if dark {
		return darkChecks()
	}
	return lightChecks()
}

var (
	lightChecks = sync.OnceValue(func() *ui.Bitmap { return checkTileOf(0xff, 0xe8) })
	darkChecks  = sync.OnceValue(func() *ui.Bitmap { return checkTileOf(0x2e, 0x26) })
)

func checkTileOf(a, b uint8) *ui.Bitmap {
	const n, cell = checkTile, 8
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			g := a
			if (x/cell+y/cell)%2 == 1 {
				g = b
			}
			img.SetRGBA(x, y, color.RGBA{g, g, g, 0xff})
		}
	}
	return ui.NewBitmap(img)
}

func intersect(a, b ui.Rect) ui.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x0, Y: y0, W: max(x1-x0, 0), H: max(y1-y0, 0)}
}

// zoomPercent is the scale the viewer shows at, as the status bar says it.
func (v *viewer) zoomPercent() string {
	return fmt.Sprintf("%d%%", int(math.Round(float64(v.scaleIn(v.paneW, v.paneH)*100))))
}

// sizeText is a size in bytes as people read it.
func sizeText(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f KB", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1000/1000)
	}
}

// setPreview shows a document rendered in its editor's place, or its text again.
func (a *app) setPreview(d *editor, on bool) {
	if on == (d.preview != nil) || on && !canPreview(d.document) {
		return
	}
	if on {
		d.preview = newPreviewViewer(d.document)
		if isMarkdown(d.document) {
			// It opens where the text is.
			m := &markdownPreview{}
			y := d.vscroll.Y
			if d.hidden {
				y = d.hiddenY
			}
			m.sync.want, m.sync.placing = a.textPlace(d, y), true
			d.preview.markdown = m
		}
	} else {
		if m := d.preview.markdown; m != nil && m.lines > 0 {
			// The text opens where the preview was.
			y := a.textY(d, m.sync.top)
			if d.hidden {
				d.hiddenY = y
			} else {
				d.vscroll.Y = y
			}
		}
		d.preview.stop()
		d.preview = nil
	}
	if t := a.activeTab(); t != nil && t.ed == d && a.focus == paneEditor {
		a.focusTab(t)
	}
	a.rememberTabs()
}

// togglePreview switches between a document's source and rendered view.
func (a *app) togglePreview() {
	if d := a.activeEditor(); d != nil && canPreview(d.document) {
		a.setPreview(d, d.preview == nil)
		a.focusColumn()
	}
}

// previewToSide shows the preview of the document shown in a column to
// the right, beside its text, which keeps the keyboard.
func (a *app) previewToSide() {
	d := a.activeEditor()
	if d == nil || !canPreview(d.document) {
		return
	}
	if d.preview != nil {
		// The preview is shown already: the text goes beside it.
		a.setPreview(d, false)
	}
	src := a.column()
	a.splitRight()
	side := a.activeEditor()
	// The text takes the keyboard back before the preview is shown, which
	// would take it.
	if col, i := a.locate(src.tab()); col != nil {
		a.activate(col, i, true)
	}
	if side != nil && side != d && side.document == d.document {
		a.setPreview(side, true)
	}
}

// textEditor returns the editor shown, showing its text in place of the
// rendered preview, for commands that work on the text. Nil when the
// column shows no editor.
func (a *app) textEditor() *editor {
	d := a.activeEditor()
	if d != nil && d.preview != nil {
		a.setPreview(d, false)
		a.focusColumn()
	}
	return d
}

// viewerStatus is the status bar's part for a picture: its size, the
// file's, and the zoom, which a click fits.
func (a *app) viewerStatus(c *ui.Context, v *viewer, item func(key, label, tip string, icon *ui.SVG, fn func()), text func(string) ui.Element) {
	p := a.pal
	if v.diskGone {
		ui.Row(c).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTrash).FontSize(12).TextColor(p.danger)
			text(locales.DeletedOnDisk()).TextColor(p.dangerFg)
		})
	}
	if m := v.markdown; m != nil {
		if m.words > 0 {
			item("words", locales.PreviewWords(m.words)+" · "+locales.PreviewMinutes(m.minutes), "", nil, nil)
		}
		item("preview", locales.PreviewWord(), "", nil, nil)
		return
	}
	if v.src == nil {
		return
	}
	item("dims", fmt.Sprintf("%d × %d", int(math.Round(float64(v.w))), int(math.Round(float64(v.h)))), "", nil, nil)
	if v.size > 0 {
		item("size", sizeText(v.size), "", nil, nil)
	}
	item("zoom", v.zoomPercent(), a.withKey(locales.FitToView(), "view.zoomReset"), nil, func() { v.zoomBy(0) })
}
