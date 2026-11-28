package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// dockIcons records what setDockIcon is given, failing for the pictures
// of fail.
func dockIcons(t *testing.T, fail []byte) *[][]byte {
	t.Helper()
	var got [][]byte
	was := setDockIcon
	setDockIcon = func(png []byte) error {
		got = append(got, png)
		if fail != nil && bytes.Equal(png, fail) {
			return errors.New("no image")
		}
		return nil
	}
	t.Cleanup(func() { setDockIcon = was })
	return &got
}

// The icons are squares on the icon grid: their corners clear and their
// middles solid.
func TestAppIconArt(t *testing.T) {
	for id, size := range map[string]int{"": 256, "amber": 512} {
		img, err := png.Decode(bytes.NewReader(appIconArt(id)))
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		b := img.Bounds()
		if b.Dx() != size || b.Dy() != size {
			t.Errorf("%q is %v", id, b)
		}
		if _, _, _, a := img.At(b.Min.X, b.Min.Y).RGBA(); a != 0 {
			t.Errorf("%q has a corner of alpha %d", id, a>>8)
		}
		if _, _, _, a := img.At(size/2, size/2).RGBA(); a>>8 < 250 {
			t.Errorf("%q has a middle of alpha %d", id, a>>8)
		}
	}
	if appIconID("amber") != "amber" || appIconID("aqua") != "" || !bytes.Equal(appIconArt("aqua"), defaultIcon) {
		t.Error("an unknown icon isn't Mayoi's own")
	}
}

// A tile chooses the icon with a click and the arrows, which shows at
// once and stays in the settings' file; Mayoi's own gives macOS back the
// bundle's.
func TestPrefsAppIconTiles(t *testing.T) {
	onOS(t, "darwin")
	got := dockIcons(t, nil)
	ph := newPrefsHarness(t)
	if err := ph.pt.Click("Amber"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if ph.m.s.AppIcon != "amber" || len(*got) != 1 || !bytes.Equal((*got)[0], amberIcon) {
		t.Fatalf("chose %q, showed %d icons", ph.m.s.AppIcon, len(*got))
	}
	ph.m.s.Flush()
	if s := settings.Load(ph.m.s.Path()); s.AppIcon != "amber" {
		t.Errorf("the file has %q", s.AppIcon)
	}
	if !ph.pt.Focused("Amber") {
		t.Fatal("the tile clicked hasn't the keyboard")
	}
	ph.pt.Key(0, ui.KeyLeft)
	ph.frames()
	if ph.m.s.AppIcon != "" || len(*got) != 2 || (*got)[1] != nil {
		t.Fatalf("chose %q, showed %d icons", ph.m.s.AppIcon, len(*got))
	}
	// The same icon again shows nothing anew.
	ph.w.set("appIcon", json.RawMessage(`""`))
	if len(*got) != 2 {
		t.Errorf("showed %d icons", len(*got))
	}
}

// An icon the file names that this version doesn't know shows as Mayoi's
// own, and stays in the file; reset takes Amber back to Mayoi's own.
func TestPrefsAppIconUnknownAndReset(t *testing.T) {
	onOS(t, "darwin")
	got := dockIcons(t, nil)
	ph := newPrefsHarness(t)
	ph.w.set("appIcon", json.RawMessage(`"amber"`))
	ph.w.set("appIcon", json.RawMessage(`"aqua"`))
	ph.frames()
	if ph.m.s.AppIcon != "aqua" || len(*got) != 2 || (*got)[1] != nil {
		t.Fatalf("kept %q, showed %d icons", ph.m.s.AppIcon, len(*got))
	}
	ph.w.set("appIcon", json.RawMessage(`"amber"`))
	ph.w.reset("appIcon", nil)
	if ph.m.s.AppIcon != "" || (*got)[len(*got)-1] != nil {
		t.Errorf("reset to %q", ph.m.s.AppIcon)
	}
}

// Starting with Amber chosen shows it; an icon the system can't show
// leaves Mayoi's own, and the settings say why.
func TestApplyAppIcon(t *testing.T) {
	onOS(t, "darwin")
	got := dockIcons(t, amberIcon)
	ph := newPrefsHarness(t)
	ph.m.s.AppIcon = "amber"
	ph.m.applyAppIcon()
	if len(*got) != 2 || !bytes.Equal((*got)[0], amberIcon) || (*got)[1] != nil {
		t.Fatalf("showed %d icons", len(*got))
	}
	ph.frames()
	if ph.m.appIconErr == nil || !ph.pt.HasText("Could not show the icon: no image. Mayoi shows its own.") {
		t.Error("no failure shown")
	}
	ph.m.s.AppIcon = ""
	ph.m.applyAppIcon()
	if ph.m.appIconErr != nil {
		t.Error("the failure stayed")
	}
}

// Windows and Linux are given Mayoi's own picture back, having no bundle
// to take it from.
func TestAppIconPNGElsewhere(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		onOS(t, goos)
		if !bytes.Equal(appIconPNG(""), defaultIcon) || !bytes.Equal(appIconPNG("amber"), amberIcon) {
			t.Errorf("%s isn't given the pictures", goos)
		}
	}
}
