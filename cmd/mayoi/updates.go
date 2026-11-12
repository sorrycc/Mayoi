package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"mayoi/locales"
)

// Mayoi checks for a new version itself, every updateInterval, and shows
// it in the status bar, the app menu and the welcome page rather than in
// the updater's window, which opens when one of them is clicked.
const (
	updateInterval = time.Hour
	// updateFirstWait is how long after the launch, or after the computer
	// wakes, the next check waits, for the network to come up.
	updateFirstWait = 10 * time.Second
	// updateTimeout gives up on a check that hangs.
	updateTimeout = time.Minute
)

// startUpdates checks for updates from now on, in builds that can update
// themselves.
func (m *mayoi) startUpdates() {
	if !mygo.Updater.Enabled() {
		return
	}
	// Skip This Version in the updater's window hides the version.
	updater.OnChange(func() { m.setUpdate(m.updateFound) })
	wake := make(chan struct{}, 1)
	mygo.Power.OnResume(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	go func() {
		defer recoverCrash()
		timer := time.NewTimer(updateFirstWait)
		for {
			select {
			case <-timer.C:
				m.checkUpdate()
				timer.Reset(updateInterval)
			case <-wake:
				// Timers stop while the computer sleeps.
				timer.Reset(updateFirstWait)
			}
		}
	}()
}

// checkUpdate asks for the newest version. A check that fails keeps what
// the last one found, and the next tries again.
func (m *mayoi) checkUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	if err != nil {
		return
	}
	version := ""
	if up != nil {
		version = up.Version
	}
	mygo.RunOnMain(func() { m.setUpdate(version) })
}

// setUpdate keeps the version a check found, "" for none, and shows it
// anew where it shows, unless it was skipped.
func (m *mayoi) setUpdate(found string) {
	m.updateFound = found
	shown := found
	if shown != "" && shown == skippedVersion() {
		shown = ""
	}
	if shown == m.update {
		return
	}
	m.update = shown
	for _, w := range m.wins {
		w.a.invalidate()
	}
	m.refreshMenu()
}

// skippedVersion is the version Skip This Version skipped, which the
// updater keeps in updater.json beside the settings.
func skippedVersion() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "updater.json"))
	if err != nil {
		return ""
	}
	var st struct {
		SkippedVersion string `json:"skippedVersion"`
	}
	_ = json.Unmarshal(data, &st) // a damaged file skips nothing
	return st.SkippedVersion
}

// offerNativeBuild tells, at each launch of the Intel build on a Mac with
// Apple Silicon, that Rosetta runs it slower and crashes it at times, with
// the download of the Apple Silicon build, which updates install too
// (mygo.Updater.Check).
func (m *mayoi) offerNativeBuild(h *desktop) {
	if !mygo.App.IsTranslated() {
		return
	}
	h.do(func() {
		h.a.tell(note{message: locales.NoteIntelBuild(), label: locales.DownloadButton(), action: func() {
			h.a.services.OpenURL(nativeBuildURL)
		}})
	})
}

// nativeBuildURL is where the Apple Silicon build downloads from.
const nativeBuildURL = repoURL + "/releases/latest"

// The host's side of updates.

func (h *desktop) update() string { return h.m.update }

func (h *desktop) installUpdate() { updater.CheckForUpdates() }
