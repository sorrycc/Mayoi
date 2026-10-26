package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// writePNG writes a w×h image, half of it transparent.
func writePNG(t testing.TB, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w / 2 {
			img.Set(x, y, color.NRGBA{200, 60, 40, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="32" viewBox="0 0 64 32"><rect width="64" height="32" fill="#36c"/></svg>
`

// until draws frames until ok, failing after a while.
func (hs *harness) until(what string, ok func() bool) {
	hs.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			hs.t.Fatalf("timed out waiting for %s", what)
		}
		hs.settle()
	}
}

func TestImageOpensInAViewer(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	path := hs.path("art.png")
	writePNG(t, path, 40, 30)
	a.openFile(path, true)
	hs.until("the picture", func() bool { v := a.activeViewer(); return v != nil && v.src != nil })
	v := a.activeTab().view
	if a.activeEditor() != nil || len(a.docs) != 0 || v.w != 40 || v.h != 30 {
		t.Fatalf("editor %v, %d docs, picture %v×%v", a.activeEditor(), len(a.docs), v.w, v.h)
	}
	for _, s := range []string{"40 × 30", "100%", "art.png"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	hs.shot("30-image")
	// Opening it again shows the same tab.
	a.openFile(path, true)
	if n := len(a.column().tabs); n != 1 {
		t.Fatalf("%d tabs", n)
	}

	// ⌘= zooms the picture, not the text.
	size := a.settings.FontSize
	a.run("view.zoomIn")
	hs.settle()
	if v.zoom != 1.5 || a.settings.FontSize != size || !tt.HasText("150%") {
		t.Fatalf("zoom %v, font size %v", v.zoom, a.settings.FontSize)
	}
	a.run("view.zoomOut")
	a.run("view.zoomOut")
	if v.zoom != 2.0/3 {
		t.Fatalf("zoomed out to %v", v.zoom)
	}
	a.run("view.zoomReset")
	if v.zoom != 0 {
		t.Fatalf("reset to %v", v.zoom)
	}

	// Scrolling with Cmd held zooms around the pointer, and dragging moves
	// the picture once it is larger than the pane.
	box, ok := tt.Find("Image art.png")
	if !ok {
		t.Fatal("no pane")
	}
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	// (The tester's scrolling has no modifiers: the event goes straight.)
	for range 20 {
		a.viewerInput(v, ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Cmd, X: box.W/2 + 30, Y: box.H / 2, DY: -100, Precise: true})
	}
	hs.settle()
	if v.zoom <= 4 || v.panX >= 0 {
		t.Fatalf("scrolled to zoom %v, pan %v", v.zoom, v.panX)
	}
	x0 := v.panX
	hs.drag(cx, cy, cx+80, cy)
	if v.panX <= x0 {
		t.Fatalf("dragged from %v to %v", x0, v.panX)
	}
	// Editing commands leave the picture be.
	a.run("sel.comment")
	a.run("edit.find")
	hs.settle()
	if a.activeTab().view != v || len(a.docs) != 0 {
		t.Fatal("an editing command changed the tab")
	}
}

func TestOtherBinaryFilesStillRefuse(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("blob.bin"), []byte("a\x00b"), 0o644)
	os.WriteFile(hs.path("broken.png"), []byte("not a png"), 0o644)
	a.openFile(hs.path("blob.bin"), true)
	if len(a.errors) != 1 || !strings.Contains(a.errors[0], "binary") || len(a.column().tabs) != 0 {
		t.Fatalf("errors %q, %d tabs", a.errors, len(a.column().tabs))
	}
	// An image that does not decode says why in its tab.
	a.openFile(hs.path("broken.png"), true)
	hs.until("the error", func() bool { return a.activeViewer().loaded })
	if v := a.activeViewer(); v.src != nil || v.err == "" || !hs.tt.HasText("Cannot show broken.png: "+v.err) {
		t.Fatalf("err %q, texts %q", v.err, hs.tt.Texts())
	}
}

func TestLongLineRefuses(t *testing.T) {
	line := strings.Repeat("x", maxLineBytes)
	for _, c := range []struct {
		data string
		long bool
	}{
		{"", false},
		{line, false},
		{line + "\n" + line, false},
		{line + "x", true},
		{"a\n" + line + "x\nb", true},
		{line + "\n" + line + "x", true},
	} {
		if got := hasLongLine([]byte(c.data)); got != c.long {
			t.Errorf("%d bytes: long %v", len(c.data), got)
		}
	}
	hs := newHarness(t, true)
	a := hs.a
	os.WriteFile(hs.path("index.js.map"), []byte(line+"x"), 0o644)
	a.openFile(hs.path("index.js.map"), true)
	if len(a.errors) != 1 || !strings.Contains(a.errors[0], "too long") || len(a.column().tabs) != 0 {
		t.Fatalf("errors %q, %d tabs", a.errors, len(a.column().tabs))
	}
}

func TestImageFollowsTheDisk(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	path := hs.path("art.png")
	writePNG(t, path, 40, 30)
	a.openFile(path, true)
	hs.until("the picture", func() bool { return a.activeViewer().src != nil })
	v := a.activeViewer()
	writePNG(t, path, 80, 20)
	hs.until("the new picture", func() bool { return v.w == 80 && v.h == 20 })
	os.Remove(path)
	hs.until("the file gone", func() bool { return v.diskGone })
	if !hs.tt.HasText("Deleted on disk") {
		t.Error("the status bar does not say the file is gone")
	}
}

func TestImageTabsMoveAndRestore(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	path := hs.path("art.png")
	writePNG(t, path, 40, 30)
	a.openFile(hs.path("main.go"), true)
	a.openFile(path, true)
	hs.until("the picture", func() bool { return a.activeViewer().src != nil })
	// Split: the picture in both columns, each with its own zoom.
	a.run("view.splitRight")
	hs.settle()
	if len(a.cols) != 2 || a.cols[1].tab().view == nil || a.cols[1].tab().view == a.cols[0].tab().view || a.cols[1].tab().view.src == nil {
		t.Fatalf("%d columns", len(a.cols))
	}
	a.run("view.zoomIn")
	if a.cols[0].tab().view.zoom != 0 {
		t.Error("zooming one column zoomed the other")
	}
	// Moved where the picture shows already, the tab goes.
	a.run("view.moveTabPrev")
	hs.settle()
	if len(a.cols) != 1 || len(a.cols[0].tabs) != 2 || a.activeViewer() == nil {
		t.Fatalf("%d columns, %d tabs", len(a.cols), len(a.cols[0].tabs))
	}
	if got := a.window.Columns[0].Tabs; len(got) != 2 || got[1].File != path {
		t.Fatalf("remembered %+v", got)
	}

	// The next start opens it again.
	b := newApp(&fakeHost{folder: hs.root}, a.settings, a.window)
	t.Cleanup(b.shutdown)
	tt := ui.NewTester(b.view, 1200, 760)
	deadline := time.Now().Add(5 * time.Second)
	for (b.activeViewer() == nil || b.activeViewer().src == nil) && time.Now().Before(deadline) {
		step(tt)
		time.Sleep(5 * time.Millisecond)
	}
	if v := b.activeViewer(); v == nil || v.src == nil || v.path != path {
		t.Fatalf("restored %v", b.activeTab())
	}
	// Closing it leaves nothing behind.
	b.run("file.close")
	if len(b.column().tabs) != 1 || b.activeTab().ed == nil {
		t.Fatalf("%d tabs after closing", len(b.column().tabs))
	}
}

func TestSVGPreview(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	path := hs.path("logo.svg")
	os.WriteFile(path, []byte(testSVG), 0o644)
	a.openFile(path, true)
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.preview != nil || !tt.HasText("Show Preview (⇧⌘V)") {
		t.Fatalf("not opened as text: %v", d)
	}
	if err := tt.Click("Show Preview (⇧⌘V)"); err != nil {
		t.Fatal(err)
	}
	hs.until("the preview", func() bool { return d.preview != nil && d.preview.src != nil })
	v := d.preview
	if v.w != 64 || v.h != 32 || !tt.HasText("64 × 32") || !tt.HasText("Show Source (⇧⌘V)") {
		t.Fatalf("preview %v×%v, texts %q", v.w, v.h, tt.Texts())
	}
	hs.shot("31-svg-preview")
	if got := a.window.Columns[0].Tabs[0]; !got.Preview {
		t.Errorf("preview not remembered: %+v", got)
	}
	a.run("view.zoomIn")
	if v.zoom != 1.5 {
		t.Fatalf("zoom %v", v.zoom)
	}

	// The preview follows the text, saved or not.
	snap := d.buf.Snapshot()
	at := strings.Index(snap.String(), `width="64"`)
	d.edit(d.sel, textEdit{at, at + len(`width="64"`), `width="96"`})
	hs.until("the edit", func() bool { return v.w == 96 })
	// Text that does not parse keeps the picture, saying what is wrong.
	d.edit(d.sel, textEdit{0, 1, "x"})
	hs.until("the error", func() bool { return v.err != "" })
	if v.src == nil || !tt.HasText(v.err) {
		t.Fatalf("picture %v, error %q shown %v", v.src, v.err, tt.HasText(v.err))
	}
	d.edit(d.sel, textEdit{0, 1, "<"})
	hs.until("the fix", func() bool { return v.err == "" })

	// Editing commands show the text again.
	a.run("sel.comment")
	hs.settle()
	if d.preview != nil || !tt.HasText("Show Preview (⇧⌘V)") {
		t.Fatal("still previewing after an edit")
	}
	if got := a.window.Columns[0].Tabs[0]; got.Preview {
		t.Errorf("preview still remembered: %+v", got)
	}
	a.run("view.togglePreview")
	hs.settle()
	if d.preview == nil {
		t.Fatal("⇧⌘V did not show the preview")
	}

	// The next start shows it as it was.
	a.saveAll()
	b := newApp(&fakeHost{folder: hs.root}, a.settings, a.window)
	t.Cleanup(b.shutdown)
	bt := ui.NewTester(b.view, 1200, 760)
	step(bt)
	step(bt)
	if e := b.activeEditor(); e == nil || e.preview == nil || e.preview.src == nil {
		t.Fatalf("restored %v", b.activeTab())
	}
}
