package main

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"mayoi/internal/usage"
)

// Mayoi counts its use anonymously, as usageStats in the settings allows:
// a view of /version and an "active" event a day while it is open, and an
// "update" event on the first launch of a new version.
const (
	// usageInterval is how often Mayoi looks whether a day began.
	usageInterval = time.Hour
	// usageFirstWait is how long after the launch, or after the computer
	// wakes, the first look waits, for the network to come up.
	usageFirstWait = 20 * time.Second
	// usageTimeout gives up on events that hang.
	usageTimeout = 30 * time.Second
)

// usageState is what usage.json beside the settings keeps of the events
// sent: the day of the last "active", and the version that sent it.
type usageState struct {
	Day     string `json:"day,omitempty"`
	Version string `json:"version,omitempty"`
}

// sendsUsage reports whether this build counts its use: a release, which
// updates itself, or one MAYOI_UMAMI_URL points at an Umami to try it
// with, and never with DO_NOT_TRACK set.
func sendsUsage() bool {
	if dnt := os.Getenv("DO_NOT_TRACK"); dnt != "" && dnt != "0" {
		return false
	}
	return os.Getenv("MAYOI_UMAMI_URL") != "" || mygo.Updater.Enabled()
}

// startUsage counts the use from now on, while usageStats is on.
func (m *mayoi) startUsage() {
	path := settingsPath()
	if path == "" || !sendsUsage() || os.Getenv("MAYOI_SNAPSHOT") != "" {
		return
	}
	path = filepath.Join(filepath.Dir(path), "usage.json")
	c := usage.New(cmp.Or(os.Getenv("MAYOI_UMAMI_URL"), usage.URL), usage.Website, appVersion())
	wake := make(chan struct{}, 1)
	mygo.Power.OnResume(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	go func() {
		defer recoverCrash()
		timer := time.NewTimer(usageFirstWait)
		for {
			select {
			case <-timer.C:
				on := make(chan bool)
				mygo.RunOnMain(func() { on <- m.s.UsageStats })
				if <-on {
					ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
					reportUsage(ctx, c, path, appVersion(), i18n.Locale(), time.Now())
					cancel()
				}
				timer.Reset(usageInterval)
			case <-wake:
				// Timers stop while the computer sleeps.
				timer.Reset(usageFirstWait)
			}
		}
	}()
}

// reportUsage sends the events due at now and keeps in the file at path
// those sent: "update" once the version differs from the last that sent,
// and "active" once a day. An event that fails is sent at the next look.
func reportUsage(ctx context.Context, c *usage.Client, path, version, lang string, now time.Time) error {
	var st usageState
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &st)
	}
	system := map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH}
	save := func() error {
		b, _ := json.Marshal(st)
		return os.WriteFile(path, b, 0o644)
	}
	if st.Version != "" && st.Version != version {
		data := map[string]any{"from": st.Version, "to": version, "os": runtime.GOOS, "arch": runtime.GOARCH}
		if err := c.Send(ctx, c.Event("update", lang, data)); err != nil {
			return err
		}
		st.Version = version
		if err := save(); err != nil {
			return err
		}
	}
	if day := now.Format(time.DateOnly); st.Day != day {
		if err := c.Send(ctx, c.View(version, lang)); err != nil {
			return err
		}
		if err := c.Send(ctx, c.Event("active", lang, system)); err != nil {
			return err
		}
		st.Day, st.Version = day, version
		return save()
	}
	return nil
}
