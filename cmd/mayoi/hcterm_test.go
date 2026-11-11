package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// fakeHC stands in for hc: it records the commands run and answers them
// with reply, by the command's name.
type fakeHC struct {
	mu    sync.Mutex
	calls [][]string
	reply func(args []string) ([]byte, error)
}

func useFakeHC(t *testing.T, reply func(args []string) ([]byte, error)) *fakeHC {
	f := &fakeHC{reply: reply}
	oldRun, oldLook, oldBin := hcRun, lookPath, hcLocalBin
	bin := t.TempDir()
	hcLocalBin = func() string { return bin }
	t.Cleanup(func() { hcRun, lookPath, hcLocalBin = oldRun, oldLook, oldBin })
	hcRun = func(args ...string) ([]byte, error) {
		f.mu.Lock()
		f.calls = append(f.calls, args)
		reply := f.reply
		f.mu.Unlock()
		if reply == nil {
			return []byte("{}"), nil
		}
		return reply(args)
	}
	lookPath = func(name string) (string, error) {
		if name == "hc" {
			return "/usr/local/bin/hc", nil
		}
		return oldLook(name)
	}
	return f
}

// answer has hc answer with reply from now on, as commands may be
// running in the background.
func (f *fakeHC) answer(reply func(args []string) ([]byte, error)) {
	f.mu.Lock()
	f.reply = reply
	f.mu.Unlock()
}

// ran returns the commands run whose name is cmd, waiting a moment for
// those run in the background.
func (f *fakeHC) ran(cmd string) [][]string {
	var out [][]string
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		f.mu.Lock()
		out = nil
		for _, c := range f.calls {
			if c[0] == cmd {
				out = append(out, c)
			}
		}
		f.mu.Unlock()
		if len(out) > 0 {
			break
		}
	}
	return out
}

// connTerminal is a terminal of no program, for a session that never
// starts one.
func connTerminal(t *testing.T) *terminal.Terminal {
	near, far := net.Pipe()
	t.Cleanup(func() { far.Close() })
	term, err := terminal.New(terminal.Options{Conn: near})
	if err != nil {
		t.Fatal(err)
	}
	return term
}

// hcSession adds a terminal of the panel whose shell runs in herdr-core
// at addr.
func hcSession(t *testing.T, a *app, addr string) *session {
	a.nextTerm++
	s := &session{id: a.nextTerm, dir: a.root, hc: addr, term: connTerminal(t)}
	a.terms = append(a.terms, s)
	a.panelTerm = s
	return s
}

// With terminalBackendHcPath, hc is found there first, as the program or
// in the folder that has it, and the old places without a working one.
func TestHCPathPreference(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	hs := newHarness(t, true)
	a := hs.a
	oldLook, oldBin := lookPath, hcLocalBin
	dir := t.TempDir()
	hcLocalBin = func() string { return dir }
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() {
		lookPath, hcLocalBin = oldLook, oldBin
		setHCConfig(settings.Prefs{})
	})

	custom := filepath.Join(dir, "custom-hc")
	if err := os.WriteFile(custom, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.settings.TerminalBackend = "hc"
	a.settings.TerminalBackendHcPath = custom
	setHCConfig(a.settings.Prefs)
	if got := hcPath(); got != custom {
		t.Errorf("hc %q, want %q", got, custom)
	}

	folder := filepath.Join(dir, "bin")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(folder, "herdr-core")
	if err := os.WriteFile(nested, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.settings.TerminalBackendHcPath = folder
	setHCConfig(a.settings.Prefs)
	if got := hcPath(); got != nested {
		t.Errorf("hc %q in a folder, want %q", got, nested)
	}

	fallback := filepath.Join(dir, "hc")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.settings.TerminalBackendHcPath = filepath.Join(dir, "missing")
	setHCConfig(a.settings.Prefs)
	if got := hcPath(); got != fallback {
		t.Errorf("hc %q without the preferred one, want %q", got, fallback)
	}

	a.settings.TerminalBackend = ""
	a.settings.TerminalBackendHcPath = custom
	setHCConfig(a.settings.Prefs)
	if got := hcPath(); got != fallback {
		t.Errorf("hc %q with another backend, want %q", got, fallback)
	}
}

// terminalBackendHcEnv is KEY=value strings, with those without one left
// out, and only with the hc backend.
func TestHCEnvPreference(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(func() { setHCConfig(settings.Prefs{}) })
	a.settings.TerminalBackend = "hc"
	a.settings.TerminalBackendHcEnv = []string{"A=1", "B=x=y", "NOEQUAL", "=empty", "C=", "", "D=a\x00b"}
	setHCConfig(a.settings.Prefs)
	want := []string{"A=1", "B=x=y", "C="}
	if got := hcEnv(); !slices.Equal(got, want) {
		t.Errorf("env %q, want %q", got, want)
	}
	a.settings.TerminalBackend = ""
	setHCConfig(a.settings.Prefs)
	if got := hcEnv(); got != nil {
		t.Errorf("env %q with another backend", got)
	}
}

// hc's commands run with terminalBackendHcEnv in their environment, from
// terminalBackendHcPath.
func TestHCRunEnv(t *testing.T) {
	skipOnWindows(t, "shell scripts")
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(func() { setHCConfig(settings.Prefs{}) })
	dir := t.TempDir()
	hc := filepath.Join(dir, "hc")
	if err := os.WriteFile(hc, []byte("#!/bin/sh\nprintf '%s' \"$HC_TEST_VALUE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.settings.TerminalBackend = "hc"
	a.settings.TerminalBackendHcPath = hc
	a.settings.TerminalBackendHcEnv = []string{"HC_TEST_VALUE=hello", "IGNORED"}
	setHCConfig(a.settings.Prefs)
	out, err := hcRun("ls")
	if err != nil || string(out) != "hello" {
		t.Errorf("hc ran with %q, %v", out, err)
	}
}

// A bare name of terminalBackendHcPath is found on the path, and an
// install is not offered for a path the person set.
func TestHCPathBareNameAndOffer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	oldLook, oldOffered := lookPath, hcOffered
	hcOffered = false
	t.Cleanup(func() {
		lookPath, hcOffered = oldLook, oldOffered
		setHCConfig(settings.Prefs{})
	})
	a.settings.TerminalBackend = "hc"
	a.settings.TerminalBackendHcPath = "my-hc"
	setHCConfig(a.settings.Prefs)
	lookPath = func(name string) (string, error) {
		if name == "my-hc" {
			return "/opt/bin/my-hc", nil
		}
		return "", errors.New("not found")
	}
	if got := hcPath(); got != "/opt/bin/my-hc" {
		t.Errorf("hc %q, want the bare name on the path", got)
	}
	a.offerHC()
	if a.confirm != nil {
		t.Error("offered to install with a path set")
	}
}

func TestHCError(t *testing.T) {
	fallback := errors.New("exit status 1")
	for in, want := range map[string]string{
		`{"error":{"kind":"spawn_failed","message":"cannot start \"/bin/zsh\""}}`: `cannot start "/bin/zsh"`,
		"hc: no core\n": "hc: no core",
		"":              "exit status 1",
	} {
		if got := hcError([]byte(in), fallback).Error(); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// With terminalBackend "hc", a terminal starts a shell in herdr-core,
// labeled with the folder, and attaches to it; without hc, or without
// the setting, it runs here.
func TestHCStart(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		return []byte(`{"address":"t9","state":"live"}`), nil
	})
	if a.hcBackend() {
		t.Fatal("hc without terminalBackend")
	}
	a.settings.TerminalBackend = "hc"
	if !a.hcBackend() {
		t.Fatal("no hc with terminalBackend hc")
	}
	addr, err := hcStart(hs.root, hs.root, "")
	if err != nil || addr != "t9" {
		t.Fatalf("hcStart: %q, %v", addr, err)
	}
	run := f.ran("run")
	want := []string{"run", "--bg", "--json", "--no-parent", "--rm", "--cwd", hs.root, "--label", "mayoi.folder=" + hs.root}
	if len(run) != 1 || !slices.Equal(run[0], want) {
		t.Errorf("ran %q, want %q", run, want)
	}
	if got := hcAttach("t9", false); !slices.Equal(got, []string{"hc", "attach", "t9"}) {
		t.Errorf("attach command %q", got)
	}
	if got := hcAttach("t9", true); !slices.Equal(got, []string{"hc", "attach", "--history", "1000", "t9"}) {
		t.Errorf("attach command with history %q", got)
	}
	f.answer(func([]string) ([]byte, error) { return nil, errors.New("no core") })
	if _, err := hcStart(hs.root, hs.root, ""); err == nil || err.Error() != "no core" {
		t.Errorf("hcStart with no core: %v", err)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if a.hcBackend() {
		t.Error("hc backend without hc")
	}
}

// With terminalBackendHcShowInHerdr, a new shell of this machine is
// labeled with the Herdr space of the window's folder, and the shells it
// attaches again that Herdr doesn't show yet are labeled so: none of
// another machine, nor with the setting off.
func TestHCShowInHerdr(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		if args[0] == "ls" {
			return []byte(`{"terminals":[{"address":"t3","state":"live"},{"address":"t4","state":"live","where":"api / 1"},` +
				`{"address":"t5","state":"live"},{"address":"t6","state":"finished"}]}`), nil
		}
		return []byte(`{"address":"t9","state":"live"}`), nil
	})
	t.Cleanup(func() { setHCConfig(settings.Prefs{}) })
	a.settings.TerminalBackend = "hc"
	if !a.settings.TerminalBackendHcShowInHerdr {
		t.Fatal("terminalBackendHcShowInHerdr is off by default")
	}
	setHCConfig(a.settings.Prefs)
	space := hcHerdrSpace(hs.root)
	if len(space) != 34 || !strings.HasPrefix(space, "sp") || space != hcHerdrSpace(hs.root) || space == hcHerdrSpace(hs.path("web")) {
		t.Errorf("space id %q", space)
	}
	hcStart(hs.root, hs.root, "")
	hcStart(hs.root, hs.root, "vps")
	run := f.ran("run")
	if len(run) != 2 || !slices.Contains(run[0], "herdr.space="+space) || slices.ContainsFunc(run[1], func(a string) bool { return strings.HasPrefix(a, "herdr.") }) {
		t.Errorf("ran %q", run)
	}
	a.hcTerms([]string{"t3", "t4", "t6", "vps/t2"})
	label := f.ran("label")
	if want := []string{"label", "--json", "t3", "herdr.space=" + space}; len(label) != 1 || !slices.Equal(label[0], want) {
		t.Errorf("labeled %q, want %q", label, want)
	}

	a.settings.TerminalBackendHcShowInHerdr = false
	setHCConfig(a.settings.Prefs)
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	hcStart(hs.root, hs.root, "")
	if run := f.ran("run"); len(run) != 1 || slices.ContainsFunc(run[0], func(a string) bool { return strings.HasPrefix(a, "herdr.") }) {
		t.Errorf("ran %q with the setting off", run)
	}
	a.hcListed.out = nil
	a.hcTerms([]string{"t3"})
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c[0] == "label" {
			t.Errorf("labeled %q with the setting off", c)
		}
	}
}

// A new terminal shows at once, saying that it starts, while its shell
// starts in herdr-core in the background, and then attaches to it. One
// closed before its shell started ends the shell.
func TestHCStartAsync(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a program")
	}
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(a.closeTerminals)
	a.settings.TerminalBackend = "hc"
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		return []byte(`{"address":"t9","state":"live"}`), nil
	})
	attached := fakeAttach(t)
	s := a.startTerminal(a.root)
	if s == nil || termStatus(s) != termStarting || termWait(s) == "" || s.hc != "" {
		t.Fatalf("a starting terminal: %+v", s)
	}
	hs.settle()
	if s.hc != "t9" || termStatus(s) != termKept || termWait(s) != "" || !slices.Equal(*attached, []string{"t9"}) {
		t.Fatalf("address %q, state %d, attached %q", s.hc, termStatus(s), *attached)
	}
	gone := a.startTerminal(a.root)
	a.killTerminal(gone)
	hs.settle()
	if kill := f.ran("kill"); len(kill) != 1 || kill[0][len(kill[0])-1] != "t9" {
		t.Errorf("closing a starting terminal ran %q", kill)
	}
	f.answer(func([]string) ([]byte, error) { return nil, errors.New("no core") })
	fails := a.fails
	here := a.startTerminal(a.root)
	hs.settle()
	if here.hc != "" || here.hcStarting || !slices.Contains(a.terms, here) || a.fails != fails+1 {
		t.Errorf("a shell that can't start: address %q, starting %v, %d errors", here.hc, here.hcStarting, a.fails-fails)
	}
}

// Closing a terminal ends its shell in herdr-core; closing the window
// leaves it running there.
func TestHCKill(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	f := useFakeHC(t, nil)
	s := hcSession(t, a, "t4")
	a.killTerminal(s)
	if kill := f.ran("kill"); len(kill) != 1 || kill[0][len(kill[0])-1] != "t4" {
		t.Fatalf("closing ran %q", kill)
	}
	if slices.Contains(a.terms, s) {
		t.Error("the closed terminal stays")
	}
	hcSession(t, a, "t5")
	a.closeTerminals()
	hs.settle()
	if kill := f.ran("kill"); len(kill) != 1 {
		t.Errorf("closing the window ran %q", kill)
	}
}

// Ctrl+\ in a terminal of herdr-core goes to its shell through hc keys,
// rather than detaching hc attach; other keys, and terminals of Mayoi's,
// are left alone.
func TestHCDetachKey(t *testing.T) {
	f := useFakeHC(t, nil)
	here := &session{}
	if hcDetachKey(here, ui.Ctrl, ui.KeyBackslash) {
		t.Error("took Ctrl+\\ of a terminal of Mayoi's")
	}
	s := &session{hc: "t3"}
	if hcDetachKey(s, ui.Ctrl|ui.Shift, ui.KeyBackslash) || hcDetachKey(s, ui.Ctrl, ui.KeyC) {
		t.Error("took another key")
	}
	if !hcDetachKey(s, ui.Ctrl, ui.KeyBackslash) {
		t.Fatal("left Ctrl+\\ to hc attach")
	}
	keys := f.ran("keys")
	if len(keys) != 1 || !slices.Equal(keys[0], []string{"keys", "--json", "t3", `ctrl+\`}) {
		t.Errorf("ran %q", keys)
	}
}

// As hc attach ends, its terminal closes once the shell ended, and one
// whose attach ended as soon as it began closes with an error rather
// than attach again and again.
func TestHCExited(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	state := "finished"
	useFakeHC(t, func(args []string) ([]byte, error) {
		if args[0] == "get" && state == "" {
			return nil, errors.New("no terminal")
		}
		return []byte(`{"state":"` + state + `"}`), nil
	})
	for _, st := range []string{"finished", ""} {
		state = st
		s := hcSession(t, a, "t6")
		s.attached = time.Now().Add(-time.Minute)
		a.terminalExited(s, s.term, 0)
		hs.settle()
		if slices.Contains(a.terms, s) {
			t.Errorf("the terminal of a shell %q stays", st)
		}
	}
	state = "live"
	fails := a.fails
	s := hcSession(t, a, "t7")
	s.attached = time.Now()
	a.terminalExited(s, s.term, 0)
	hs.settle()
	if slices.Contains(a.terms, s) || a.fails != fails+1 {
		t.Errorf("an attach that ended at once: open %v, %d errors", slices.Contains(a.terms, s), a.fails-fails)
	}
	// One closed here is not attached again.
	s = hcSession(t, a, "t8")
	t8 := s.term
	a.killTerminal(s)
	a.terminalExited(s, t8, 0)
	hs.settle()
	if slices.Contains(a.terms, s) {
		t.Error("a closed terminal came back")
	}
}

// A shell running on in herdr-core as hc attach ends, as after the core
// restarts, gets its terminal attached again, in the same tab.
func TestHCReattach(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a program")
	}
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, func([]string) ([]byte, error) { return []byte(`{"state":"live"}`), nil })
	attached := fakeAttach(t)
	s := hcSession(t, a, "t9")
	first := s.term
	s.attached = time.Now().Add(-time.Minute)
	a.terminalExited(s, first, 0)
	hs.settle()
	if !slices.Contains(a.terms, s) || s.term == first || !slices.Equal(*attached, []string{"t9+history"}) {
		t.Fatalf("not attached again: open %v, attached %q", slices.Contains(a.terms, s), *attached)
	}
	a.closeTerminals()
}

// The settings keep the addresses of the terminals whose shells run in
// herdr-core, of tabs and of the panel, with the one the panel shows.
func TestHCRemembered(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	hcSession(t, a, "t3")
	shown := hcSession(t, a, "t4")
	here := &session{id: 99, dir: a.root, term: connTerminal(t)}
	a.terms = append(a.terms, here)
	tab := hcSession(t, a, "t5")
	a.panelTerm = shown
	tab.tab = &colTab{term: tab}
	a.addTab(a.column(), len(a.column().tabs), tab.tab)
	a.rememberTabs()
	if w := a.window; !slices.Equal(w.PanelTerms, []string{"t3", "t4"}) || w.PanelTerm != 1 {
		t.Errorf("panel %q, showing %d", w.PanelTerms, w.PanelTerm)
	}
	tabs := a.window.Columns[0].Tabs
	if ts := tabs[len(tabs)-1]; !ts.Terminal || ts.HC != "t5" {
		t.Errorf("tab %+v", ts)
	}
}

// fakeAttach has terminals of herdr-core run a program that waits, and
// returns the addresses attached to, with history or not.
func fakeAttach(t *testing.T) *[]string {
	old := hcAttach
	t.Cleanup(func() { hcAttach = old })
	var attached []string
	hcAttach = func(addr string, history bool) []string {
		if history {
			addr += "+history"
		}
		attached = append(attached, addr)
		return []string{"sleep", "30"}
	}
	return &attached
}

// Starting again, a window attaches to the terminals whose shells still
// run in herdr-core, tabs and the panel's, with what they showed before,
// and starts a new shell for a tab whose shell ended.
func TestHCRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a program")
	}
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(a.closeTerminals)
	sub := hs.path("web")
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		return []byte(`{"terminals":[{"address":"t3","cwd":"` + sub + `","state":"live"},` +
			`{"address":"t4","state":"finished"},{"address":"t6","state":"live"},{"address":"t7","state":"live"}]}`), nil
	})
	attached := fakeAttach(t)
	a.window.Columns = []settings.Column{{Tabs: []settings.Tab{{Terminal: true, HC: "t3"}, {Terminal: true, HC: "t4"}}}}
	a.restoreTabs()
	hs.settle()
	var terms []*session
	for _, tab := range a.column().tabs {
		if tab.term != nil {
			terms = append(terms, tab.term)
		}
	}
	if len(terms) != 2 || terms[0].hc != "t3" || terms[0].dir != sub || terms[1].hc != "" {
		t.Fatalf("restored %d terminal tabs", len(terms))
	}
	if !slices.Equal(*attached, []string{"t3+history"}) {
		t.Errorf("attached %q", *attached)
	}
	// The panel: t5 ended, t3 shows already, and the one it showed was t7.
	if !a.restorePanel([]string{"t5", "t6", "t3", "t7"}, 3) {
		t.Fatal("the panel attached to none")
	}
	var panel []string
	for _, s := range a.panelTerms() {
		panel = append(panel, s.hc)
	}
	if !slices.Equal(panel, []string{"t6", "t7"}) || a.panelTerm == nil || a.panelTerm.hc != "t7" {
		t.Errorf("panel %q", panel)
	}
	if a.restorePanel([]string{"t5"}, 0) {
		t.Error("attached to a shell that ended")
	}
	if ls := f.ran("ls"); len(ls) != 1 {
		t.Errorf("asked hc ls %d times", len(ls))
	}
}

// Switching or closing a folder leaves its terminals in the window. Its
// saved session must not attach those shells again when the folder opens
// in another window, while the window that kept them still remembers them.
func TestHCRestoreDoesNotShareTerminalsWithPreviousFolder(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a program")
	}
	for _, tc := range []struct {
		name        string
		closeFolder bool
	}{
		{name: "switch folder"},
		{name: "close folder", closeFolder: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, other := workspaceFixture(t), workspaceFixture(t)
			s := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
			w := s.NewWindow()
			w.Folder = root
			a := newApp(&fakeHost{}, s, w)
			t.Cleanup(a.shutdown)
			t.Cleanup(a.closeTerminals)
			tt := ui.NewTester(a.view, 1200, 760)
			step(tt)
			useFakeHC(t, func(args []string) ([]byte, error) {
				return []byte(`{"terminals":[{"address":"t-panel","state":"live"},{"address":"t-tab","state":"live"}]}`), nil
			})
			panel := hcSession(t, a, "t-panel")
			tab := hcSession(t, a, "t-tab")
			a.terminalToColumn(tab, a.column(), a.column().active+1)
			a.openFile(filepath.Join(root, "main.go"), true)
			a.panelTerm = panel
			a.rememberTabs()

			if tc.closeFolder {
				a.closeFolder()
			} else {
				a.switchFolder(other)
			}
			step(tt)
			for _, addr := range []string{"t-panel", "t-tab"} {
				if !slices.ContainsFunc(a.terms, func(s *session) bool { return s.hc == addr }) {
					t.Errorf("the window lost %s", addr)
				}
			}

			ses := s.Sessions[root]
			if ses == nil {
				t.Fatal("the previous folder has no session")
			}
			if len(ses.PanelTerms) != 0 {
				t.Errorf("the previous folder still restores panel terminals %q", ses.PanelTerms)
			}
			for _, col := range ses.Columns {
				for _, tab := range col.Tabs {
					if tab.HC != "" {
						t.Errorf("the previous folder still restores %s in a tab", tab.HC)
					}
				}
			}
			if col := ses.Columns[0]; len(col.Tabs) != 1 || col.Active != 0 || col.Tabs[0].File != filepath.Join(root, "main.go") {
				t.Errorf("the file tab after the removed terminal: %+v", col)
			}

			// Leaving the addresses out of the saved copy must not clear them
			// from the window that kept the terminals.
			a.rememberTabs()
			if !slices.Equal(a.window.PanelTerms, []string{"t-panel"}) {
				t.Errorf("the window forgot its panel terminal: %q", a.window.PanelTerms)
			}
			if !slices.ContainsFunc(a.window.Columns, func(col settings.Column) bool {
				return slices.ContainsFunc(col.Tabs, func(tab settings.Tab) bool { return tab.HC == "t-tab" })
			}) {
				t.Error("the window forgot its terminal tab")
			}

			// An update or restart restores the session from disk, not from
			// the settings still in memory.
			s.Flush()
			s = settings.Load(s.Path())
			t.Cleanup(s.Flush)
			taken := s.TakeSession(root)
			b := newApp(&fakeHost{}, s, s.NewWindow())
			t.Cleanup(b.shutdown)
			t.Cleanup(b.closeTerminals)
			b.openFolder(root)
			fakeAttach(t)
			if b.restorePanel(taken.PanelTerms, taken.PanelTerm) || slices.ContainsFunc(b.terms, func(s *session) bool {
				return s.hc == "t-panel" || s.hc == "t-tab"
			}) {
				t.Error("two windows attached to the same shell")
			}
		})
	}
}

// What is typed while a shell starts in herdr-core is kept, up to a
// limit, for the shell, and taken once.
func TestHCWaitTyped(t *testing.T) {
	w := newHCWait()
	w.Write([]byte("ls"))
	w.Write([]byte(" -la\r"))
	if got := string(w.take()); got != "ls -la\r" {
		t.Errorf("typed %q", got)
	}
	if got := w.take(); got != nil {
		t.Errorf("taken again: %q", got)
	}
	w.Write(make([]byte, hcTypedMax+10))
	if got := len(w.take()); got != hcTypedMax {
		t.Errorf("kept %d bytes", got)
	}
	w.Close()
	if n, err := w.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Errorf("read %d, %v after closing", n, err)
	}
}

// A terminal whose shell starts in herdr-core, or that waits for its
// machine, says so in a pill over it, which goes once it attached.
func TestTermWaitPill(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	s := hcSession(t, a, "")
	s.hcStarting = true
	a.window.Panels.On(settings.SideBottom).Open = true
	shows := func(text string) bool {
		step(hs.tt)
		return slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, text) })
	}
	if !shows("Starting in herdr-core") {
		t.Errorf("no pill as it starts: %q", hs.tt.Texts())
	}
	s.hcStarting, s.hc, s.lost = false, "vps/t2", true
	if !shows("Waiting for vps") {
		t.Errorf("no pill as it waits: %q", hs.tt.Texts())
	}
	s.lost = false
	if shows("Waiting for") || shows("Starting in") {
		t.Error("the pill stays once attached")
	}
	if termStatus(s) != termKept {
		t.Errorf("state %d", termStatus(s))
	}
}

// The menu of a terminal whose shell runs in herdr-core copies its
// address; that of one of Mayoi's has no such item.
func TestHCCopyAddress(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	s := hcSession(t, a, "vps/t2")
	s.title = "vps"
	a.window.Panels.On(settings.SideBottom).Open = true
	hs.settle()
	if err := hs.tt.RightClick("1: vps"); err != nil {
		t.Fatal(err)
	}
	if err := hs.tt.ChooseMenuItem("Copy hc Address"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.tt.Clipboard(); got != "vps/t2" {
		t.Errorf("copied %q", got)
	}
	s.hc = ""
	hs.settle()
	if err := hs.tt.RightClick("1: vps"); err != nil {
		t.Fatal(err)
	}
	if err := hs.tt.ChooseMenuItem("Copy hc Address"); err == nil {
		t.Error("a terminal of Mayoi's copies an address")
	}
}

// Starting, a window attaches its panel again to the terminals it had,
// though restoring its tabs remembers them first.
func TestHCStartRestoresPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a program")
	}
	hs := newHarness(t, true)
	a := hs.a
	t.Cleanup(a.closeTerminals)
	useFakeHC(t, func(args []string) ([]byte, error) {
		return []byte(`{"terminals":[{"address":"t6","cwd":"` + hs.root + `","state":"live"}]}`), nil
	})
	attached := fakeAttach(t)
	a.window.PanelTerms, a.window.PanelTerm = []string{"t6"}, 0
	a.window.Panels.On(settings.SideBottom).Open = true
	a.start()
	hs.settle()
	if a.panelTerm == nil || a.panelTerm.hc != "t6" || !slices.Equal(*attached, []string{"t6+history"}) {
		t.Fatalf("panel shows %+v, attached %q", a.panelTerm, *attached)
	}
	if !slices.Equal(a.window.PanelTerms, []string{"t6"}) {
		t.Errorf("remembered %q", a.window.PanelTerms)
	}
}
