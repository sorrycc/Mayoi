// Package gitmarks finds the lines of a file changed since Git's last
// commit, which the editor's gutter marks.
package gitmarks

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/linediff"
	"mayoi/internal/vfs"
)

// Marks are the lines of a document changed since Git's last commit. The
// text of the file at HEAD is read, and the lines compared with it, in
// the background.
type Marks struct {
	// Lines are the marks of the lines, one each; Version the version of
	// the text they are of.
	Lines   []linediff.Kind
	Version uint64
	// Hunks are the runs of lines changed, against the lines at HEAD,
	// of the same version.
	Hunks []linediff.Hunk

	base    []string // the lines at HEAD; nil for a file Git has none of
	of      []string // the base the hunks are of
	loaded  bool
	loading bool
	stale   bool // HEAD may have moved: read the base again

	diffed  bool // the marks are of the base read last
	running bool
	started time.Time // when the last comparison started
	waiting bool      // a comparison waits for the pause between them
}

// diffPause is the least time between two comparisons of a document's
// lines, so that typing in a long one does not keep a core comparing.
const diffPause = 150 * time.Millisecond

// Refresh reads the base of the file at path again, when it may have
// changed, and compares the lines of snap once its text changed. The work
// in the background reports through post, which runs a function on the
// goroutine that calls Refresh.
func (g *Marks) Refresh(path string, snap ui.TextSnapshot, post func(func())) {
	if path == "" {
		return
	}
	if (!g.loaded || g.stale) && !g.loading {
		g.loading, g.stale = true, false
		dir, name := filepath.Dir(path), filepath.Base(path)
		bg.Go(func() {
			out, err := vfs.Output(dir, "git", "show", "HEAD:./"+name)
			post(func() {
				g.loading, g.loaded, g.diffed = false, true, false
				// The marks of the old base show until those of the new.
				g.base = nil
				if err == nil {
					text := strings.ReplaceAll(string(out), "\r\n", "\n")
					g.base = strings.Split(text, "\n")
				} else {
					g.Lines, g.Hunks, g.of = nil, nil, nil
				}
			})
		})
	}
	if !g.loaded || g.base == nil || g.running || g.waiting || g.diffed && g.Version == snap.Version() {
		return
	}
	if wait := diffPause - time.Since(g.started); g.diffed && wait > 0 {
		g.waiting = true
		bg.AfterFunc(wait, func() { post(func() { g.waiting = false }) })
		return
	}
	g.running, g.started = true, time.Now()
	base := g.base
	bg.Go(func() {
		lines := strings.Split(snap.String(), "\n")
		hunks := linediff.Hunks(base, lines)
		marks := linediff.Marks(hunks, len(lines))
		post(func() {
			g.running = false
			if g.loaded && sameLines(g.base, base) {
				g.Lines, g.Hunks, g.of, g.Version, g.diffed = marks, hunks, base, snap.Version(), true
			}
		})
	})
}

// Base returns the lines of the file at HEAD that the hunks are of, nil
// for a file Git has none of.
func (g *Marks) Base() []string { return g.of }

// HeadMoved has the next Refresh read the base again, as when a commit
// may have been made.
func (g *Marks) HeadMoved() { g.stale = true }

// sameLines reports whether two bases are one.
func sameLines(a, b []string) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
