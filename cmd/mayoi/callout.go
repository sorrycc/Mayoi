package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Obsidian's callouts fold: [!faq]- shows its title alone until opened,
// [!faq]+ shows all until folded. A click on its title folds or opens
// one, in Live Preview and the Reading view alike, without changing the
// text: its document keeps which, by the callout's marker line.

// calloutFolded reports whether the callout of key, which folds as fold,
// shows folded in doc.
func calloutFolded(doc *document, key string, fold byte) bool {
	if fold == 0 {
		return false
	}
	if folded, ok := doc.folds[key]; ok {
		return folded
	}
	return fold == '-'
}

// calloutHidden reports whether a block in the callouts of keys, their
// marker lines, is hidden, as one of them is folded.
func calloutHidden(doc *document, keys []string) bool {
	for _, k := range keys {
		if calloutFolded(doc, k, markdownAlertFold(k)) {
			return true
		}
	}
	return false
}

// toggleCallout folds the callout of key, or opens it.
func (a *app) toggleCallout(doc *document, key string, fold byte) {
	if doc.folds == nil {
		doc.folds = map[string]bool{}
	}
	doc.folds[key] = !calloutFolded(doc, key, fold)
	doc.foldKey, doc.foldNow = key, true
	a.invalidate()
}

// calloutSince is how long ago at now the callout of doc.foldKey was
// folded or opened, by the clock of the frames, which note when.
func calloutSince(doc *document, now time.Time) time.Duration {
	if doc.foldNow {
		doc.foldAt, doc.foldNow = now, false
	}
	if doc.foldAt.IsZero() {
		return -1
	}
	return now.Sub(doc.foldAt)
}

// calloutTurn is how far a callout's chevron is turned at now, from 0,
// pointing right, folded, to 1, pointing down, open: it turns over
// motionHover after a click, and reports whether it still does.
func calloutTurn(doc *document, key string, folded bool, now time.Time) (float32, bool) {
	k := float32(1)
	since := calloutSince(doc, now)
	turning := key == doc.foldKey && since >= 0 && since < motionHover
	if turning {
		k = ui.EaseOut(float32(since) / float32(motionHover))
	}
	if folded {
		k = 1 - k
	}
	return k, turning
}

// drawChevron draws a chevron in box, turned from pointing right by
// turn, 0 to 1, to pointing down.
func drawChevron(pt *ui.Painter, box ui.Rect, turn float32, c ui.Color) {
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	h := box.W * 0.22
	// The points of a chevron pointing right, about its middle, turned
	// a quarter at most.
	pts := [3][2]float32{{-h / 2, -h}, {h / 2, 0}, {-h / 2, h}}
	s, co := math.Sincos(float64(turn) * math.Pi / 2)
	sin, cos := float32(s), float32(co)
	var out [][2]float32
	for _, q := range pts {
		out = append(out, [2]float32{cx + q[0]*cos - q[1]*sin, cy + q[0]*sin + q[1]*cos})
	}
	pt.StrokePolyline(out, max(1.25, box.W/12), c)
}
