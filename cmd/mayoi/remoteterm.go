package main

import (
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// A terminal of a host ends with the connection: rather than close, it
// stays, with what it showed, says the connection went, and starts again
// once the host is back, in the folder its shell was in when the shell
// said so, with what it showed above.

// lostNote is what a terminal of a host shows once its connection went.
func lostNote(host string) string {
	return "\r\n\x1b[0;2m" + locales.TermLost(host) + "\x1b[0m\r\n"
}

// terminalExited takes the end of a terminal's program: a terminal of a
// host whose connection went waits for it, any other closes.
func (a *app) terminalExited(s *session, t *terminal.Terminal, code int) {
	if s.term != t {
		return // one restarted in its place
	}
	if s.hc != "" {
		a.hcExited(s, t)
		return
	}
	// Attached to a terminal of herdr-core: what hc attach printed stays
	// as it goes, and the shell there runs on.
	if s.attach != "" {
		if time.Since(s.started) < exitedEarlyWithin {
			a.fail("%s", locales.FailAttachHC(s.attach))
		}
		a.removeTerminal(s)
		return
	}
	// ssh exits with 255 when the connection fails, which a shell does
	// only if asked.
	if c := sessionConn(s); c != nil && c == a.conn && (code == 255 || !c.Connected()) {
		if !s.lost {
			s.lost = true
			a.beginWait(s)
			t.Feed([]byte(lostNote(c.Host)))
		}
		return
	}
	if time.Since(s.started) < exitedEarlyWithin {
		a.exitedEarly(s, t, code)
		return
	}
	a.removeTerminal(s)
}

// exitedEarlyWithin is how soon after it started a shell that exits
// counts as one that could not start, as for a broken rc file or a
// $SHELL that is not there, rather than one the user ended.
const exitedEarlyWithin = 2 * time.Second

// exitedEarly keeps a terminal whose shell exited as it started, rather
// than have it flash and vanish: it shows what the shell printed and why
// it stopped, and offers to report it.
func (a *app) exitedEarly(s *session, t *terminal.Terminal, code int) {
	shell, c := shellName(), strconv.Itoa(code)
	t.Feed([]byte("\r\n\x1b[0;2m" + locales.TermExitedEarly(shell, c) + "\x1b[0m\r\n"))
	out := lastLines(t.Text(), 20)
	a.tell(note{message: locales.NoteTermExitedEarly(shell, c), label: locales.ReportProblem(), action: func() {
		a.reportProblem(shell+" exited with code "+c+" as the terminal started",
			"Shell: "+os.Getenv("SHELL")+"\nExit code: "+c+"\n\nWhat it printed:\n```\n"+out+"\n```")
	}})
}

// lastLines returns the last n lines of s that have text.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

// restartTerminals starts again the terminals of the host the connection
// ended, as it comes back.
func (a *app) restartTerminals() {
	for _, s := range a.terms {
		if s.lost {
			a.restartTerminal(s)
		}
	}
}

// restartTerminal starts a terminal of a host again, in the folder its
// shell was last in, showing what it showed before. One whose shell runs
// in herdr-core attaches to it again.
func (a *app) restartTerminal(s *session) {
	if s.hc != "" {
		a.hcReattach(s)
		return
	}
	c := sessionConn(s)
	if c == nil {
		return
	}
	dir := s.dir
	if d := s.term.Dir(); path.IsAbs(d) {
		dir = remote.Path(c.Host, d)
	}
	old := s.term
	t, err := a.spawnTerminal(s, dir)
	if err != nil {
		a.fail("%s", locales.FailRestartTerminal(err.Error()))
		return
	}
	t.Feed(old.Snapshot())
	t.Feed([]byte("\x1b[0m\r\n"))
	s.term, s.lost = t, false
	// A new shell: what the programs of the old one said is gone.
	s.status = terminal.ProgramStatus{}
	old.Close()
}
