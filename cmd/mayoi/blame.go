package main

import (
	"fmt"
	"strings"
	"time"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/settings"
	"mayoi/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// blameState is who last changed the caret's line of an editor, which
// shows faint after it once the caret rests there.
type blameState struct {
	// line and version are those the blame is of, since when the caret
	// rested there.
	line    int
	version uint64
	since   time.Time
	phase   blamePhase
	text    string // "" for a line not committed, or none read yet
}

// blamePhase is how far the blame of the caret's line is.
type blamePhase int

const (
	blameResting blamePhase = iota // the caret has to rest there first
	blameReading                   // Git is asked, in the background
	blameRead                      // text says it, or Git could not
)

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
	if !a.prefs().Blame || !focused || r == nil || d.path == "" || !r.Contains(d.path) {
		return ""
	}
	if e, ok := ctx.git.entries[d.path]; ok && e.Untracked() {
		return ""
	}
	b := &d.blame
	if b.line != line || b.version != version {
		*b = blameState{line: line, version: version, since: time.Now()}
		bg.AfterFunc(blamePause, a.invalidate)
		return ""
	}
	if b.phase != blameResting {
		return b.text
	}
	if time.Since(b.since) < blamePause {
		return ""
	}
	b.phase = blameReading
	path, text := d.path, d.buf.Snapshot().String()
	bg.Go(func() {
		defer recoverCrash()
		bl, err := r.BlameLine(path, line, text)
		a.post(func() {
			if d.blame.line != line || d.blame.version != version {
				return
			}
			d.blame.phase = blameRead
			if err == nil {
				d.blame.text = blameText(bl, time.Now())
			}
		})
	})
	return ""
}

// blameText is how a line's blame reads.
func blameText(b git.Blame, now time.Time) string {
	if b.Commit == "" {
		return ""
	}
	return fmt.Sprintf("%s, %s · %s", b.Author, ago(b.Time, now), b.Summary)
}

// blameAfterRows places the blame of line i of an editor that wraps after
// the end of the line's last row, its text starting at left: its x in the
// area, the row, and its text cut to fit before right, as width measures it.
func (a *app) blameAfterRows(d *editor, snap ui.TextSnapshot, i int, text string, left, right float32, width func(string) float32) (float32, int, string) {
	f := a.font
	d.rows.runes = d.lines.AppendRows(d.rows.runes[:0], i)
	s, e := snap.LineRange(i)
	at, row := 0, 0
	if n := len(d.rows.runes); n > 1 {
		at, row = d.rows.runes[n-1], n-1
	}
	x := left + f.width(snap.Slice(min(e, s+at), e)) + 4*f.charW
	return x, row, fitBlame(text, right-x, width)
}

// minBlameRunes is the fewest runes of a blame cut to fit that show.
const minBlameRunes = 4

// fitBlame returns as much of a blame as fits in room, as width measures
// it, ending in "…" when cut, or "" when too little of it fits.
func fitBlame(text string, room float32, width func(string) float32) string {
	if width(text) <= room {
		return text
	}
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if width(string(runes[:mid])+"…") <= room {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo < minBlameRunes {
		return ""
	}
	return strings.TrimRight(string(runes[:lo]), " ·,") + "…"
}

// ago says how long before now t was, roughly, for a week; older, it
// says the date, as "340 days ago" is a sum for the reader to do: the day
// and month this year, with the year before.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return locales.AgoJustNow()
	case d < time.Hour:
		return locales.AgoMinutes(int(d / time.Minute))
	case d < 24*time.Hour:
		return locales.AgoHours(int(d / time.Hour))
	case d < agoFor:
		return locales.AgoDays(int(d / (24 * time.Hour)))
	}
	skeleton := "yMMMd"
	if t.Year() == now.Year() {
		skeleton = "MMMd"
	}
	return i18n.FormatDate(i18n.Locale(), t.Local(), i18n.DateOptions{Skeleton: skeleton})
}

// agoFor is how long ago says how long before: past it, it says the date.
const agoFor = 7 * 24 * time.Hour

// dateTime is a date and time to the minute, in the language shown, as
// "Jan 2, 2006, 15:04" in English.
func dateTime(t time.Time) string {
	return i18n.FormatDate(i18n.Locale(), t, i18n.DateOptions{Skeleton: "yMMMdHm"})
}

// toggleBlame shows the blame of the caret's line, or stops.
func (a *app) toggleBlame() {
	a.setPref("blame", func(p *settings.Prefs) { p.Blame = !p.Blame })
}
