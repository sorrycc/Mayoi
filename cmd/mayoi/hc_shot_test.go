package main

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestShotHC shoots the terminals of herdr-core in each state: kept,
// starting and waiting for their machine, in the panel and a tab.
func TestShotHC(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("SHOTS is not set")
	}
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		a := hs.a
		useFakeHC(t, nil)
		kept := hcSession(t, a, "k7")
		kept.title = "zsh"
		starting := hcSession(t, a, "")
		starting.title, starting.hcStarting, starting.waitSince = "zsh", true, time.Now()
		waiting := hcSession(t, a, "vps/t2")
		waiting.title, waiting.lost, waiting.waitSince = "vps", true, time.Now()
		ready := hcSession(t, a, "r9")
		ready.title, ready.hcReady = "ready", time.Now()
		waiting.term.Feed([]byte("$ tail -f deploy.log\r\nbuild ok\r\n" + lostNote("vps")))
		a.terminalToColumn(kept, a.column(), 0)
		a.panelTerm = waiting
		a.window.PanelOpen = true
		hs.tt.SetScale(2)
		hs.settle()
		for range 20 {
			hs.tt.Advance(16 * time.Millisecond)
			hs.tt.Frame()
		}
		name := "hc-light"
		if dark {
			name = "hc-dark"
		}
		hs.shot(name)
		hs.shotRect(name+"-tab", image.Rect(248, 38, 700, 76), 2)
		a.panelTerm = ready
		for range 20 {
			hs.tt.Advance(16 * time.Millisecond)
			hs.tt.Frame()
		}
		hcAdding = "vps"
		for range 20 {
			hs.tt.Advance(16 * time.Millisecond)
			hs.tt.Frame()
		}
		hs.shotRect(name+"-status", image.Rect(0, 736, 600, 760), 2)
		hcAdding = ""
		hs.shotRect(name+"-ready", image.Rect(248, 476, 1200, 560), 2)
		waiting.waitSince = time.Now().Add(-time.Hour)
		a.panelTerm = waiting
		for range 20 {
			hs.tt.Advance(16 * time.Millisecond)
			hs.tt.Frame()
		}
		hs.shotRect(name+"-away", image.Rect(248, 476, 1200, 560), 2)
		hs.shotRect(name+"-panel", image.Rect(248, 476, 1200, 560), 2)
	}
}

// TestShotHCMotion films a terminal's shell getting to herdr-core: the
// chip's badge and the pill, frame by frame, one row of the strip each.
func TestShotHCMotion(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	s := hcSession(t, a, "")
	s.title, s.hcStarting, s.waitSince = "zsh", true, time.Now()
	a.window.PanelOpen = true
	hs.tt.SetScale(2)
	hs.settle()
	chip, pill := image.Rect(248, 480, 420, 510), image.Rect(980, 516, 1200, 550)
	var rows []image.Image
	film := func() {
		img := hs.tt.Image()
		rows = append(rows, img.SubImage(image.Rect(chip.Min.X*2, chip.Min.Y*2, chip.Max.X*2, chip.Max.Y*2)),
			img.SubImage(image.Rect(pill.Min.X*2, pill.Min.Y*2, pill.Max.X*2, pill.Max.Y*2)))
	}
	for range 3 {
		hs.tt.Advance(350 * time.Millisecond)
		film()
	}
	s.hcStarting, s.hc, s.hcReady = false, "t1", hs.tt.Now()
	hs.tt.Frame()
	for range 8 {
		film()
		hs.tt.Advance(100 * time.Millisecond)
	}
	hs.tt.Advance(time.Second)
	hs.tt.Frame()
	film()
	w, h := 0, 0
	for i := 0; i < len(rows); i += 2 {
		w = max(w, rows[i].Bounds().Dx()+rows[i+1].Bounds().Dx())
		h += max(rows[i].Bounds().Dy(), rows[i+1].Bounds().Dy()) + 4
	}
	strip := image.NewRGBA(image.Rect(0, 0, w, h))
	y := 0
	for i := 0; i < len(rows); i += 2 {
		c, p := rows[i], rows[i+1]
		draw.Draw(strip, image.Rect(0, y, c.Bounds().Dx(), y+c.Bounds().Dy()), c, c.Bounds().Min, draw.Src)
		x := c.Bounds().Dx()
		draw.Draw(strip, image.Rect(x, y, x+p.Bounds().Dx(), y+p.Bounds().Dy()), p, p.Bounds().Min, draw.Src)
		y += max(c.Bounds().Dy(), p.Bounds().Dy()) + 4
	}
	f, err := os.Create(filepath.Join(dir, "hc-motion.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, strip)
	f.Close()
}

// TestShotHCConfirm shoots the dialogs that ask to add a host to hc and
// to install hc.
func TestShotHCConfirm(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("SHOTS is not set")
	}
	hs := newHarness(t, true)
	a := hs.a
	hs.tt.SetScale(2)
	delete(hcAsked, "vps")
	a.hcOffer("vps")
	hs.settle()
	hs.shot("hc-confirm-add")
	a.confirm = nil
	hs.settle()
	a.settings.TerminalBackend = "hc"
	old := hcOffered
	hcOffered = false
	t.Cleanup(func() { hcOffered = old })
	useFakeHC(t, nil)
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	a.offerHC()
	hs.settle()
	hs.shot("hc-confirm-install")
}
