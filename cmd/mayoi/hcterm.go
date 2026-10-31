package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// With terminalBackend "hc", a terminal's shell runs in a terminal of
// herdr-core, and the view runs hc attach on it: the shell outlives the
// window. Closing a terminal ends it there; closing the window or
// quitting only ends the attach, and the shell runs on.

// hcRun runs hc with args and returns what it printed, or the message
// of the error it printed instead. Tests replace it.
var hcRun = func(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(cmp.Or(hcPath(), "hc"), args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, hcError(stderr.Bytes(), err)
	}
	return out, nil
}

// hcErr is an error hc printed, of a kind such as "not_found".
type hcErr struct{ kind, message string }

func (e *hcErr) Error() string { return e.message }

// hcError is the error hc printed, {"error": {"kind": ..., "message":
// ...}}, or fallback when it printed none.
func hcError(out []byte, fallback error) error {
	var e struct {
		Error struct{ Kind, Message string } `json:"error"`
	}
	if json.Unmarshal(out, &e) == nil && e.Error.Message != "" {
		return &hcErr{e.Error.Kind, e.Error.Message}
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return fallback
}

// hcGone reports whether an error of hc says the terminal is gone, not
// that hc could not tell, as when it can't reach the terminal's machine.
func hcGone(err error) bool {
	var e *hcErr
	return errors.As(err, &e) && (e.kind == "not_found" || e.kind == "finished")
}

// hcLocalBin is the folder herdr's installer puts hc in, which tests
// replace.
var hcLocalBin = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

// hcPath is the hc Mayoi runs: "hc" on the path, else the one herdr's
// installer put in hcLocalBin, which the path of a Mayoi opened from the
// Finder may lack. That is herdr-core when another hc was on the path as
// it installed. "" when there is none.
func hcPath() string {
	if _, err := lookPath("hc"); err == nil {
		return "hc"
	}
	dir := hcLocalBin()
	for _, name := range []string{"hc", "herdr-core"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// hcBackend reports whether new terminals run in herdr-core: the
// settings ask for it, and hc is there.
func (a *app) hcBackend() bool {
	return a.settings.TerminalBackend == "hc" && hcPath() != ""
}

// hcStart starts a shell in dir in a terminal of herdr-core, on machine
// unless it is "", labeled with root, the window's folder, and returns
// its address. It is removed once its shell exits. It may take a while,
// as for another machine: run it in the background.
func hcStart(root, dir, machine string) (string, error) {
	args := []string{"run", "--bg", "--json", "--no-parent", "--rm", "--cwd", dir, "--label", "mayoi.folder=" + root}
	if machine != "" {
		args = append(args, "--on", machine)
	}
	out, err := hcRun(args...)
	if err != nil {
		return "", err
	}
	var t struct{ Address string }
	if err := json.Unmarshal(out, &t); err != nil || t.Address == "" {
		return "", errors.New("hc run printed no address")
	}
	if machine != "" && !strings.Contains(t.Address, "/") {
		t.Address = machine + "/" + t.Address
	}
	return t.Address, nil
}

// hcWait is what a terminal shows while its shell starts in herdr-core:
// nothing, until it is closed. What is typed meanwhile goes to the shell
// once the terminal attached to it.
type hcWait struct {
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	typed []byte
}

// hcTypedMax is as much as a terminal keeps of what is typed while its
// shell starts, which the shell gets once attached.
const hcTypedMax = 4 << 10

func newHCWait() *hcWait { return &hcWait{done: make(chan struct{})} }

func (w *hcWait) Read([]byte) (int, error) {
	<-w.done
	return 0, io.EOF
}

func (w *hcWait) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.typed = append(w.typed, p[:min(len(p), hcTypedMax-len(w.typed))]...)
	return len(p), nil
}

// take returns what was typed, once.
func (w *hcWait) take() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	typed := w.typed
	w.typed = nil
	return typed
}

func (w *hcWait) Close() error {
	w.once.Do(func() { close(w.done) })
	return nil
}

// hcBegin starts the shell of a terminal in herdr-core in the
// background, on the host's machine in a remote folder, while the
// terminal shows that it starts, and then attaches to it. A host hc
// doesn't know, or a shell that can't start, leaves the terminal to run
// as it would without.
func (a *app) hcBegin(s *session) {
	dir, host := s.dir, ""
	if c, rdir, ok := remote.Lookup(dir); ok {
		dir, host = rdir, c.Host
	}
	root := a.root
	bg.Go(func() {
		machine, known := "", true
		if host != "" {
			machine, known = hcMachine(host)
		}
		var addr string
		var err error
		if known {
			addr, err = hcStart(root, dir, machine)
		}
		a.post(func() { a.hcBegun(s, host, known, addr, err) })
	})
}

// hcBegun takes the shell hcBegin started at addr, or why it didn't: the
// terminal attaches to it, or runs as it would without. A terminal closed
// meanwhile ends the shell.
func (a *app) hcBegun(s *session, host string, known bool, addr string, err error) {
	if !s.hcStarting || !slices.Contains(a.terms, s) {
		if addr != "" {
			hcKill(addr)
		}
		return
	}
	s.hcStarting = false
	switch {
	case !known:
		a.hcOffer(host)
	case err != nil:
		a.fail("%s", locales.FailStartHC(err.Error()))
	default:
		s.hc, s.hcNew = addr, true
	}
	wait := s.hcWait
	s.hcWait = nil
	a.hcReattach(s)
	if s.hc != "" {
		a.hcGotThere(s)
	}
	if wait != nil && slices.Contains(a.terms, s) {
		if typed := wait.take(); len(typed) > 0 {
			s.term.Send(typed)
		}
	}
	a.rememberTabs()
}

// hcAttach is the command that shows the terminal of herdr-core at addr,
// with what scrolled off its screen when history is set. Tests replace
// it.
var hcAttach = func(addr string, history bool) []string {
	hc := cmp.Or(hcPath(), "hc")
	if history {
		return []string{hc, "attach", "--history", "1000", addr}
	}
	return []string{hc, "attach", addr}
}

// hcKill ends the shell of a terminal of herdr-core, in the background.
func hcKill(addr string) {
	bg.Go(func() { hcRun("kill", "--json", addr) })
}

// hcDetachKey reports whether a key is the one hc attach detaches with,
// Ctrl+\, which Mayoi takes so that a terminal can't come off its shell:
// the shell gets it from hc keys instead.
func hcDetachKey(s *session, mods ui.Modifiers, key ui.Key) bool {
	if s.hc == "" || mods != ui.Ctrl || key != ui.KeyBackslash {
		return false
	}
	addr := s.hc
	bg.Go(func() { hcRun("keys", "--json", addr, `ctrl+\`) })
	return true
}

// killTerminal ends a terminal the user closes: its shell in herdr-core
// too, which closing the window leaves running.
func (a *app) killTerminal(s *session) {
	if s.hc != "" {
		hcKill(s.hc)
	}
	a.removeTerminal(s)
}

// hcGet returns the state of the terminal of herdr-core at addr, "live"
// while it runs, and the folder it is in.
func hcGet(addr string) (state, cwd string, err error) {
	out, err := hcRun("get", "--json", addr)
	if err != nil {
		return "", "", err
	}
	var t struct{ State, Cwd string }
	if err := json.Unmarshal(out, &t); err != nil {
		return "", "", err
	}
	return t.State, t.Cwd, nil
}

// hcExited takes the end of hc attach in a terminal: it closes once its
// shell ended, and attaches again while the shell runs on, as after
// herdr-core restarts. One that ends as soon as it attached closes, to
// not attach again and again. One of another machine that hc can't
// reach waits for it, saying so, as a terminal of a host does for its
// connection.
func (a *app) hcExited(s *session, t *terminal.Terminal) {
	if !slices.Contains(a.terms, s) {
		return // closed here, or its window closed
	}
	addr, quick := s.hc, time.Since(s.attached) < time.Second
	bg.Go(func() {
		state, _, err := hcGet(addr)
		a.post(func() {
			if s.term != t || !slices.Contains(a.terms, s) {
				return
			}
			switch {
			case state == "live" && quick:
				a.fail("%s", locales.FailAttachHC(addr))
			case state == "live":
				a.hcReattach(s)
				return
			case err != nil && !hcGone(err) && sessionConn(s) != nil:
				a.hcLost(s, t)
				return
			}
			a.removeTerminal(s)
		})
	})
}

// hcLookFor is how long a start waits for hc to say whether the
// terminals of other machines run, before it attaches to those that did
// not answer anyway.
var hcLookFor = 1500 * time.Millisecond

// hcRetry is how long a terminal of another machine that hc can't reach
// waits before it looks again.
var hcRetry = 5 * time.Second

// hcLost has a terminal of another machine that hc can't reach say so,
// and look again in a while; the connection coming back attaches it
// again sooner, as restartTerminals does.
func (a *app) hcLost(s *session, t *terminal.Terminal) {
	if !s.lost {
		s.lost = true
		a.beginWait(s)
		t.Feed([]byte(hcLostNote(sessionConn(s).Host)))
	}
	bg.AfterFunc(hcRetry, func() {
		a.post(func() {
			if s.lost && s.term == t {
				a.hcExited(s, t)
			}
		})
	})
}

// hcLostNote is what a terminal of another machine that hc can't reach
// shows: its shell runs on there, unlike that of a terminal over ssh.
func hcLostNote(host string) string {
	return "\r\n\x1b[0;2m" + locales.HcLost(host) + "\x1b[0m\r\n"
}

// hcReattach attaches a terminal again to its shell in herdr-core: one
// that waited for its machine says it got there.
func (a *app) hcReattach(s *session) {
	old, waited := s.term, s.lost
	t, err := a.spawnTerminal(s, s.dir)
	if err != nil {
		a.fail("%s", locales.FailRestartTerminal(err.Error()))
		a.removeTerminal(s)
		return
	}
	s.term, s.lost = t, false
	old.Close()
	if waited && s.hc != "" {
		a.hcGotThere(s)
	}
}

// hcGotThere has a terminal say for a moment that its shell in
// herdr-core is attached, having started or waited, and builds the view
// again once it stops saying so.
func (a *app) hcGotThere(s *session) {
	s.hcReady = time.Now()
	bg.AfterFunc(hcReadyFor, func() { a.post(func() {}) })
}

// hcTerms returns those of the terminals of herdr-core at addrs that
// run, by address, with the folder each is in, as a path of the window's
// host for one of another machine: none when hc is not there.
func (a *app) hcTerms(addrs []string) map[string]string {
	if hcPath() == "" {
		return nil
	}
	live := map[string]string{}
	if out, err := a.hcList(); err == nil {
		var l struct {
			Terminals []struct{ Address, Cwd, State string }
		}
		if json.Unmarshal(out, &l) == nil {
			for _, t := range l.Terminals {
				if t.State == "live" && !strings.Contains(t.Address, "/") {
					live[t.Address] = t.Cwd
				}
			}
		}
	}
	// Those of other machines, all at once, as hc may wait on each to
	// answer, their folders the host's. One whose machine hc can't reach,
	// or that doesn't answer in hcLookFor, may run on there: it attaches
	// in the window's folder, and waits for its machine.
	c, _, isRemote := remote.Lookup(a.root)
	type look struct {
		addr, cwd string
		live      bool
	}
	looks := make(chan look, len(addrs))
	pending := map[string]bool{}
	for _, addr := range addrs {
		if _, ok := live[addr]; ok || !strings.Contains(addr, "/") || pending[addr] {
			continue
		}
		pending[addr] = true
		bg.Go(func() {
			state, cwd, err := hcGet(addr)
			switch {
			case err == nil && state == "live":
				if isRemote && cwd != "" {
					cwd = remote.Path(c.Host, cwd)
				}
				looks <- look{addr, cmp.Or(cwd, a.root), true}
			case err != nil && !hcGone(err):
				looks <- look{addr, a.root, true}
			default:
				looks <- look{addr: addr}
			}
		})
	}
	timeout := time.After(hcLookFor)
	for len(pending) > 0 {
		select {
		case l := <-looks:
			delete(pending, l.addr)
			if l.live {
				live[l.addr] = l.cwd
			}
		case <-timeout:
			for addr := range pending {
				live[addr] = a.root
			}
			return live
		}
	}
	return live
}

// hcListFor is how long hcList keeps what hc ls printed: a start
// restores the tabs and then the panel, and asks once.
const hcListFor = 2 * time.Second

// hcList returns what hc ls --json printed, once for every restore that
// follows within hcListFor. The main thread's.
func (a *app) hcList() ([]byte, error) {
	if l := &a.hcListed; l.out != nil && time.Since(l.at) < hcListFor {
		return l.out, nil
	}
	out, err := hcRun("ls", "--json")
	if err == nil {
		a.hcListed.out, a.hcListed.at = out, time.Now()
	}
	return out, err
}

// hcShown reports whether a terminal here shows the shell at addr in
// herdr-core already.
func (a *app) hcShown(addr string) bool {
	return slices.ContainsFunc(a.terms, func(s *session) bool { return s.hc == addr })
}

// attachTerminal shows the shell at addr in herdr-core, in dir, in a
// terminal: one of a window before it closed.
func (a *app) attachTerminal(addr, dir string) *session {
	return a.addTerminal(&session{dir: dir, title: shellName(), hc: addr})
}

// restorePanel attaches the panel again to those of its terminals at
// addrs whose shells run on in herdr-core, showing the one at shown,
// and reports whether there were any. The panel stays as it was, shown
// or hidden.
func (a *app) restorePanel(addrs []string, shown int) bool {
	if len(addrs) == 0 {
		return false
	}
	live := a.hcTerms(addrs)
	var show *session
	for i, addr := range addrs {
		dir, ok := live[addr]
		if !ok || a.hcShown(addr) {
			continue
		}
		if s := a.attachTerminal(addr, dir); s != nil && (show == nil || i == shown) {
			show = s
		}
	}
	if show == nil {
		return false
	}
	a.panelTerm = show
	a.rememberTabs()
	return true
}
