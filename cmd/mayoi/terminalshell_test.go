package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
)

// shellFixtureEnv makes this test binary, started as the program of
// terminalShell, print the arguments it was given and wait.
const shellFixtureEnv = "MAYOI_TEST_SHELL_FIXTURE"

// runShellFixture is the program shellFixture copies: it prints its
// arguments, each in brackets, and waits for its input to end.
func runShellFixture() {
	if os.Getenv(shellFixtureEnv) == "" {
		return
	}
	var b strings.Builder
	for _, arg := range os.Args[1:] {
		b.WriteString("[" + arg + "]")
	}
	fmt.Printf("argv:%s:end\r\n", b.String())
	io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

// shellFixture copies this test binary to name in dir, as a program that
// prints its arguments, and returns its path.
func shellFixture(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name)
	if err := os.Link(self, path); err != nil {
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// Without terminalShell a terminal runs the user's shell, as before, and
// with it the program and its arguments, as they are.
func TestLocalShell(t *testing.T) {
	var p settings.Prefs
	if got := localShell(p); got != nil {
		t.Errorf("no terminalShell runs %q", got)
	}
	p.TerminalShell, p.TerminalShellArgs = "  ", []string{"-l"}
	if got := localShell(p); got != nil {
		t.Errorf("a blank terminalShell runs %q", got)
	}
	p.TerminalShell = `C:\Program Files\Git\bin\bash.exe`
	p.TerminalShellArgs = []string{"--login", "-i", "a b", `$HOME;"x"`, ""}
	want := []string{`C:\Program Files\Git\bin\bash.exe`, "--login", "-i", "a b", `$HOME;"x"`, ""}
	if got := localShell(p); !slices.Equal(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
}

// A terminal is named for the program of terminalShell, as for the
// user's shell.
func TestLocalShellName(t *testing.T) {
	t.Setenv("SHELL", "/bin/fish")
	a := &app{settings: settings.Default()}
	if got := a.localShellName(); got != "fish" {
		t.Errorf("without terminalShell %q", got)
	}
	a.settings.TerminalShell = "/opt/homebrew/bin/nu"
	if got := a.localShellName(); got != "nu" {
		t.Errorf("nu named %q", got)
	}
	onOS(t, "windows")
	for path, want := range map[string]string{
		`C:\Program Files\PowerShell\7-preview\pwsh.exe`: "pwsh",
		`C:\Program Files\Git\bin\bash.exe`:              "bash",
		"PWSH.EXE":                                       "pwsh",
	} {
		a.settings.TerminalShell = path
		if got := a.localShellName(); got != want {
			t.Errorf("%s named %q, want %q", path, got, want)
		}
	}
}

// terminalShell is the program of a local terminal of Mayoi's only: a
// remote folder's runs its host's shell, and one of herdr-core hc.
func TestTerminalLaunchKeepsOthers(t *testing.T) {
	a := &app{settings: settings.Default()}
	a.settings.TerminalShell, a.settings.TerminalShellArgs = "/opt/bin/fish", []string{"-l"}
	dir := t.TempDir()
	if l := a.terminalLaunch(&session{}, dir); !slices.Equal(l.command, []string{"/opt/bin/fish", "-l"}) || !l.own || l.dir != dir {
		t.Errorf("local %+v", l)
	}
	t.Cleanup(remote.Shutdown)
	if l := a.terminalLaunch(&session{}, remote.WSLPath("Ubuntu", "/home/me")); l.own || len(l.command) == 0 || l.command[0] != "wsl.exe" {
		t.Errorf("remote %+v", l)
	}
	if l := a.terminalLaunch(&session{attach: "t3"}, dir); l.own || slices.Contains(l.command, "/opt/bin/fish") {
		t.Errorf("attach %+v", l)
	}
	if l := a.terminalLaunch(&session{hc: "t4"}, dir); l.own || slices.Contains(l.command, "/opt/bin/fish") {
		t.Errorf("herdr-core %+v", l)
	}
	if l := a.terminalLaunch(&session{hcStarting: true}, dir); l.own {
		t.Errorf("starting in herdr-core %+v", l)
	}
}

// New terminals, in the panel and in a tab of the editor area, run the
// program of terminalShell as it is set then, at a path with spaces or by
// its name on the path, with each argument as it is; one that is not
// there says so, and names it.
func TestTerminalShellStarts(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	dir := filepath.Join(t.TempDir(), "Program Files", "Shell 7-preview")
	fixture := shellFixture(t, dir, "my shell")
	shellFixture(t, dir, "mayoi-fixture-shell")
	t.Setenv(shellFixtureEnv, "1")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	hs := newHarness(t, true)
	a := hs.a
	a.settings.TerminalShell = fixture
	a.settings.TerminalShellArgs = []string{"-NoLogo", "a b", `$HOME;"x"`}
	want := `argv:[-NoLogo][a b][$HOME;"x"]:end`
	a.run("view.newTerminal")
	hs.settle()
	if a.panelTerm == nil {
		t.Fatal("no terminal in the panel")
	}
	panel := a.panelTerm
	hs.until("the panel's arguments", func() bool { return strings.Contains(panel.term.Text(), want) })
	if panel.title != shellBase(fixture) || panel.shell != fixture {
		t.Errorf("panel named %q, shell %q", panel.title, panel.shell)
	}

	// A change takes in the next terminal, without a restart, and leaves
	// the one running as it is.
	a.settings.TerminalShell, a.settings.TerminalShellArgs = "mayoi-fixture-shell", []string{"--login", "-i"}
	a.run("view.terminalTab")
	hs.settle()
	tab := a.column().tab().term
	if tab == nil || tab == panel {
		t.Fatal("no terminal tab")
	}
	hs.until("the tab's arguments", func() bool { return strings.Contains(tab.term.Text(), "argv:[--login][-i]:end") })
	select {
	case <-panel.term.Done():
		t.Error("the running terminal ended")
	default:
	}
	if !strings.Contains(panel.term.Text(), want) {
		t.Error("the running terminal changed")
	}

	missing := filepath.Join(dir, "missing shell.exe")
	a.settings.TerminalShell, a.settings.TerminalShellArgs = missing, nil
	n := len(a.terms)
	a.run("view.terminalTab")
	hs.settle()
	if len(a.terms) != n {
		t.Fatal("a terminal of a program not there opened")
	}
	if !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, missing) }) {
		t.Errorf("no failure names %s: %q", missing, hs.tt.Texts())
	}
	a.closeTerminals()
}
