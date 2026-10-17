package main

import (
	"fmt"
	"time"

	"mayoi/internal/git"
	"mayoi/locales"

	"github.com/egoist/mygo/i18n"
)

// blameState is who last changed the caret's line of an editor, which
// shows faint after it once the caret rests there.
type blameState struct {
	// line and version are those the blame is of, since when the caret
	// rested there.
	line    int
	version uint64
	since   time.Time
	running bool
	done    bool
	text    string // "" for a line not committed, or none read yet
}

// maxBlameAfter is the longest line, in runes, after which the blame
// shows: past it, the line is measured only so far.
const maxBlameAfter = 2000

// blamePause is how long the caret rests on a line before it is blamed.
const blamePause = 350 * time.Millisecond

// blameLine returns what shows after the caret's line of an editor: who
// changed it last, when, and in what commit, "" for nothing. It blames
// the line in the background once the caret rests there.
func (a *app) blameLine(d *editor, line int, version uint64, focused bool) string {
	ctx := a.repositoryForPath(d.path)
	r := ctx.git.repo
	if !a.settings.Blame || !focused || r == nil || d.path == "" || !r.Contains(d.path) {
		return ""
	}
	if e, ok := ctx.git.entries[d.path]; ok && e.Untracked() {
		return ""
	}
	b := &d.blame
	if b.line != line || b.version != version {
		*b = blameState{line: line, version: version, since: time.Now()}
		time.AfterFunc(blamePause, a.invalidate)
		return ""
	}
	if b.done || b.running {
		return b.text
	}
	if time.Since(b.since) < blamePause {
		return ""
	}
	b.running = true
	path, text := d.path, d.buf.Snapshot().String()
	go func() {
		bl, err := r.BlameLine(path, line, text)
		a.post(func() {
			if d.blame.line != line || d.blame.version != version {
				return
			}
			d.blame.running, d.blame.done = false, true
			if err == nil {
				d.blame.text = blameText(bl, time.Now())
			}
		})
	}()
	return ""
}

// blameText is how a line's blame reads.
func blameText(b git.Blame, now time.Time) string {
	if b.Commit == "" {
		return ""
	}
	return fmt.Sprintf("%s, %s · %s", b.Author, ago(b.Time, now), b.Summary)
}

// ago says how long before now t was, roughly.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return locales.AgoJustNow()
	case d < time.Hour:
		return locales.AgoMinutes(int(d / time.Minute))
	case d < 24*time.Hour:
		return locales.AgoHours(int(d / time.Hour))
	case d < 30*24*time.Hour:
		return locales.AgoDays(int(d / (24 * time.Hour)))
	case d < 365*24*time.Hour:
		return locales.AgoMonths(int(d / (30 * 24 * time.Hour)))
	}
	return locales.AgoYears(int(d / (365 * 24 * time.Hour)))
}

// dateTime is a date and time to the minute, in the language shown, as
// "Jan 2, 2006, 15:04" in English.
func dateTime(t time.Time) string {
	return i18n.FormatDate(i18n.Locale(), t, i18n.DateOptions{Skeleton: "yMMMdHm"})
}

// toggleBlame shows the blame of the caret's line, or stops.
func (a *app) toggleBlame() {
	a.settings.Blame = !a.settings.Blame
	a.settings.Changed()
}
