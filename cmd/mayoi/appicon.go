package main

import (
	"bytes"
	_ "embed"
	"image/png"
	"slices"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// The app icons the settings offer, as tools/icongen places them on the
// macOS icon grid: Mayoi's own, which Windows and Linux are given back
// when it is chosen again, as macOS takes the bundle's, and Amber.
var (
	//go:embed appicon-default.png
	defaultIcon []byte
	//go:embed appicon-amber.png
	amberIcon []byte
)

// setDockIcon shows png as the app's icon while it runs, nil the bundle's
// (macOS): in the Dock and the app switcher on macOS, on the windows and
// their taskbar buttons on Windows and Linux. Tests replace it.
var setDockIcon = mygo.App.Dock.SetIcon

// appIconID is the icon of id that shows: "" for Mayoi's own, as for an
// icon this version doesn't know.
func appIconID(id string) string {
	if f, _ := settings.FieldOf("appIcon"); slices.Contains(f.Options, id) {
		return id
	}
	return ""
}

// appIconArt is the picture of the icon of id, Mayoi's own for "".
func appIconArt(id string) []byte {
	if appIconID(id) == "amber" {
		return amberIcon
	}
	return defaultIcon
}

// appIconPNG is what setDockIcon is given for the icon of id: nil on macOS
// for Mayoi's own, so that the Dock shows the bundle's.
func appIconPNG(id string) []byte {
	if appIconID(id) == "" && hostOS == "darwin" {
		return nil
	}
	return appIconArt(id)
}

// appIconBitmaps are the icons' pictures decoded for the settings window.
var appIconBitmaps = sync.OnceValue(func() map[string]*ui.Bitmap {
	f, _ := settings.FieldOf("appIcon")
	out := map[string]*ui.Bitmap{}
	for _, id := range f.Options {
		if img, err := png.Decode(bytes.NewReader(appIconArt(id))); err == nil {
			out[id] = ui.NewBitmap(img)
		}
	}
	return out
})

// applyAppIcon shows the app icon the settings choose, or Mayoi's own
// when the system can't show it, keeping why for the settings window.
func (m *mayoi) applyAppIcon() {
	id := appIconID(m.s.AppIcon)
	m.appIconErr = setDockIcon(appIconPNG(id))
	if m.appIconErr != nil && id != "" {
		setDockIcon(appIconPNG(""))
	}
	m.prefsChanged()
}
