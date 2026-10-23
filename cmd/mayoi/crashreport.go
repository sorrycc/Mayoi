package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"mayoi/internal/mon"
	"mayoi/locales"
)

// crashApp is Mayoi's client id at Mon, which takes its crash reports.
const crashApp = "mayoi"

// maxCrashReport is how much of a crash log a report sends: Mon takes
// 256 KB, which the log, escaped in JSON, stays under.
const maxCrashReport = 192 << 10

// crashUnknownVersion is the version a report gives for a log that
// doesn't say which crashed.
const crashUnknownVersion = "unknown"

// The kinds of crash log, as their names in the crashes folder end:
// -recovered for a panic Mayoi recovered from, -system for the system's
// report.
const (
	crashFatal     = mon.CrashFatal
	crashRecovered = mon.CrashRecovered
	crashSystem    = mon.CrashSystem
)

// The states of a crash log, as its name ends: .log not asked about yet,
// .send.log to send, .sent.log sent and .unsent.log not to.
const (
	crashPending = ""
	crashQueued  = "send"
	crashSent    = "sent"
	crashUnsent  = "unsent"
)

// crashLog is a log in the crashes folder.
type crashLog struct {
	path, stem, state, kind string
	// at is when it crashed.
	at time.Time
}

// listCrashes returns the crash logs in dir, oldest first.
func listCrashes(dir string) []crashLog {
	entries, _ := os.ReadDir(dir)
	var out []crashLog
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".log")
		if e.IsDir() || !ok {
			continue
		}
		c := crashLog{path: filepath.Join(dir, e.Name()), stem: base, kind: crashFatal}
		if i := strings.LastIndexByte(base, '.'); i >= 0 {
			c.stem, c.state = base[:i], base[i+1:]
		}
		if !slices.Contains([]string{crashPending, crashQueued, crashSent, crashUnsent}, c.state) || len(c.stem) < len(crashTime) {
			continue
		}
		at, err := time.ParseInLocation(crashTime, c.stem[:len(crashTime)], time.Local)
		if err != nil {
			continue
		}
		c.at = at
		switch {
		case strings.Contains(c.stem, "-recovered"):
			c.kind = crashRecovered
		case strings.Contains(c.stem, "-system"):
			c.kind = crashSystem
		}
		out = append(out, c)
	}
	return out
}

// mark renames the log for its new state.
func (c crashLog) mark(state string) {
	name := c.stem + ".log"
	if state != crashPending {
		name = c.stem + "." + state + ".log"
	}
	os.Rename(c.path, filepath.Join(filepath.Dir(c.path), name))
}

// sendsCrashes reports whether this build sends crash reports: a release,
// which updates itself, or one MAYOI_MON_URL points at a Mon to try them
// with.
func sendsCrashes() bool {
	return os.Getenv("MAYOI_MON_URL") != "" || mygo.Updater.Enabled()
}

// offerCrashes, as Mayoi starts, sends the crash logs the person chose to
// send and asks about the new ones in h's window, or, as crashReports in
// the settings says, sends them or doesn't without asking.
func (m *mayoi) offerCrashes(h *desktop) {
	if crashDir == "" || !sendsCrashes() {
		return
	}
	var pending, queued []crashLog
	for _, c := range listCrashes(filepath.Join(crashDir, "crashes")) {
		switch c.state {
		case crashPending:
			pending = append(pending, c)
		case crashQueued:
			queued = append(queued, c)
		}
	}
	switch m.s.CrashReports {
	case "always":
		for _, c := range pending {
			c.mark(crashQueued)
		}
		queued = append(queued, listQueued(pending)...)
		pending = nil
	case "never":
		for _, c := range pending {
			c.mark(crashUnsent)
		}
		pending = nil
	}
	if len(queued) > 0 {
		go sendCrashes(queued)
	}
	if len(pending) > 0 {
		go m.askCrashes(h, pending)
	}
}

// listQueued is the logs, marked to send.
func listQueued(logs []crashLog) []crashLog {
	out := make([]crashLog, len(logs))
	for i, c := range logs {
		c.path = filepath.Join(filepath.Dir(c.path), c.stem+"."+crashQueued+".log")
		c.state = crashQueued
		out[i] = c
	}
	return out
}

// askCrashes asks, in h's window, whether to send the logs: Show Report
// opens them, as they would be sent, and asks again. A checked Don't Ask
// Again keeps the answer in the settings. Closing the dialog without
// answering, as when quitting, asks at the next launch.
func (m *mayoi) askCrashes(h *desktop, logs []crashLog) {
	question := locales.CrashRecoveredQuestion()
	if slices.ContainsFunc(logs, func(c crashLog) bool { return c.kind != crashRecovered }) {
		question = locales.CrashQuestion()
	}
	buttons := []string{locales.ButtonSendReport(), locales.ButtonDontSend(), locales.ButtonShowReport()}
	for {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: h.f.win, Type: mygo.MessageWarning, Message: question, Detail: locales.CrashDetail(),
			Buttons: buttons, DefaultButton: 0, CancelButton: 1, CheckboxLabel: locales.CrashDontAsk(),
		})
		if err != nil {
			return
		}
		switch res.Button {
		case 2:
			h.a.post(func() {
				for _, c := range logs {
					h.a.openFile(c.path, true)
				}
			})
			buttons = buttons[:2]
			continue
		case 0:
			for _, c := range logs {
				c.mark(crashQueued)
			}
			go sendCrashes(listQueued(logs))
		default:
			for _, c := range logs {
				c.mark(crashUnsent)
			}
		}
		if res.CheckboxChecked {
			pref := "never"
			if res.Button == 0 {
				pref = "always"
			}
			mygo.RunOnMain(func() {
				m.s.CrashReports = pref
				m.s.Changed()
			})
		}
		return
	}
}

// sendCrashes sends the logs to Mon, anonymously, and marks each sent, or
// unsent when Mon won't take it. When Mon can't be reached, the rest stay
// marked to send, at the next launch.
func sendCrashes(logs []crashLog) {
	defer recoverCrash()
	c := mon.New(monURL())
	for _, l := range logs {
		b, err := os.ReadFile(l.path)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = c.SendCrash(ctx, crashReport(l, string(b)))
		cancel()
		var me *mon.Error
		switch {
		case err == nil:
			l.mark(crashSent)
		case errors.As(err, &me) && me.Status == http.StatusBadRequest:
			l.mark(crashUnsent)
		default:
			return
		}
	}
}

// crashReport is the report of a log: of the version and system that
// crashed, as the log says, which may be older than this one.
func crashReport(l crashLog, log string) mon.Crash {
	c := mon.Crash{
		App: crashApp, Version: appVersion(), OS: runtime.GOOS, Arch: runtime.GOARCH, Runtime: runtime.Version(),
		Kind: l.kind, OccurredAt: l.at.Unix(), Log: log,
	}
	first, _, _ := strings.Cut(log, "\n")
	if l.kind == crashSystem {
		// An .ips report's first line is JSON about it.
		var ips struct {
			AppVersion string `json:"app_version"`
		}
		if json.Unmarshal([]byte(first), &ips) == nil && ips.AppVersion != "" {
			c.Version = ips.AppVersion
		}
	} else if f := strings.Fields(first); len(f) >= 4 && f[0] == "Mayoi" {
		// crashHeader's line.
		c.Version, c.Runtime = f[1], strings.TrimSuffix(f[3], ",")
		if goos, arch, ok := strings.Cut(f[2], "/"); ok {
			c.OS, c.Arch = goos, arch
		}
	} else {
		// A log of before 0.11.6, without the line: of a version older
		// than this one, not known which, nor its Go.
		c.Version, c.Runtime = crashUnknownVersion, ""
	}
	if len(c.Log) > maxCrashReport {
		// The goroutine that crashed comes first.
		c.Log = strings.ToValidUTF8(c.Log[:maxCrashReport], "")
	}
	return c
}
