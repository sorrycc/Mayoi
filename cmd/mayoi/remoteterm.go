package main

import (
	"path"

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
	a.removeTerminal(s)
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
	old.Close()
}
