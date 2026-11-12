package main

import (
	"runtime"
	"slices"
	"testing"

	"mayoi/internal/cli"
)

// The box of the terminals of herdr-core of the window's machine lists the
// ones that run here, and only those: not one of another machine, nor one
// that finished.
func TestHCWindowTerms(t *testing.T) {
	hs := newHarness(t, true)
	useFakeHC(t, func(args []string) ([]byte, error) {
		if args[0] == "ls" {
			return []byte(`{"terminals":[` +
				`{"address":"t3","agent":"codex","cwd":"/w/api","state":"live","status":"working","where":"api / 1"},` +
				`{"address":"t4","agent":"zsh","cwd":"/w/web","state":"live"},` +
				`{"address":"t5","state":"finished"},` +
				`{"address":"vps/t2","agent":"claude","cwd":"/srv/app","state":"live","where":"app / 2"}]}`), nil
		}
		return []byte("{}"), nil
	})
	terms, err := hcWindowTerms(hs.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 2 || terms[0].Address != "t3" || terms[1].Address != "t4" {
		t.Fatalf("terms %+v, want t3 then t4", terms)
	}
	for _, c := range []struct {
		addr, machine string
		want          bool
	}{
		{"t3", "", true}, {"vps/t2", "", false}, {"vps/t2", "vps", true}, {"t3", "vps", false},
	} {
		if got := hcWindowAddr(c.addr, c.machine); got != c.want {
			t.Errorf("hcWindowAddr(%q, %q) = %v, want %v", c.addr, c.machine, got, c.want)
		}
	}
	if got := hcTermFolder(terms[0]); got != "api" {
		t.Errorf("folder of t3 is %q, not the space's", got)
	}
	if got := hcTermFolder(terms[1]); got != "web" {
		t.Errorf("folder of t4 is %q, not its folder's", got)
	}
	if got := hcTermAbout(terms[0]); got != "codex · working" {
		t.Errorf("t3 is %q", got)
	}
	if got := hcTermAbout(terms[1]); got != "shell" {
		t.Errorf("t4 is %q, not a shell", got)
	}
}

// A terminal of herdr-core chosen in the box shows in a terminal of this
// window, which runs hc attach here: closing it ends the attach alone, and
// the shell there runs on.
func TestHCAttachTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no hc for Windows")
	}
	hs := newHarness(t, true)
	a := hs.a
	f := useFakeHC(t, func(args []string) ([]byte, error) {
		if args[0] == "ls" {
			return []byte(`{"terminals":[{"address":"t3","agent":"codex","cwd":"/w/api","state":"live","status":"working","where":"api / 1"}]}`), nil
		}
		return []byte("{}"), nil
	})
	// The attach command of the test stays until closed, as hc attach does
	// until Ctrl+\, which ends it.
	oldAttach := hcAttach
	hcAttach = func(string, bool) []string { return []string{"/bin/cat"} }
	t.Cleanup(func() { hcAttach = oldAttach })

	// The command, as the palette runs it.
	a.run("view.attachHC")
	hs.settle()
	if !a.pluginPick.open || len(a.pluginPick.items) != 1 {
		t.Fatalf("the box is not open on t3: open %v, items %d", a.pluginPick.open, len(a.pluginPick.items))
	}
	if it := a.pluginPick.items[0]; it.Label != "api" || it.Detail != "t3" || it.Description != "codex · working" {
		t.Errorf("the item is %+v", it)
	}
	a.closePluginPicker(0)
	hs.settle()
	var s *session
	for _, c := range a.terms {
		if c.attach == "t3" {
			s = c
		}
	}
	if s == nil {
		t.Fatal("no terminal attached to t3")
	}
	if s.hc != "" {
		t.Errorf("the terminal has hc %q: closing it would end the shell", s.hc)
	}
	if s.tab == nil {
		t.Error("the terminal is not a tab of a column")
	}
	// It is not kept as a tab of its own: it shows t3 again when asked.
	a.rememberTabs()
	for _, col := range a.window.Columns {
		if len(col.Tabs) != 0 {
			t.Errorf("the tabs kept are %+v", col.Tabs)
		}
	}
	// Closing it ends the attach, not the shell of herdr-core.
	a.killTerminal(s)
	hs.settle()
	for _, c := range f.calls {
		if c[0] == "kill" {
			t.Errorf("closing the terminal ran %q", c)
		}
	}
}

// A plugin, or the mayoi command, attaches to a terminal of herdr-core by
// its address, as hc lists it for the window's machine.
func TestHCAttachArg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no hc for Windows")
	}
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, func([]string) ([]byte, error) { return []byte("{}"), nil })
	oldAttach := hcAttach
	hcAttach = func(string, bool) []string { return []string{"/bin/cat"} }
	t.Cleanup(func() { hcAttach = oldAttach })

	runs, err := a.cliCommands([]cli.Command{{ID: "view.attachHC", Arg: "t3"}}, a.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		run()
	}
	hs.settle()
	if !slices.ContainsFunc(a.terms, func(s *session) bool { return s.attach == "t3" }) {
		t.Fatalf("no terminal attached to t3: %+v", a.terms)
	}
	// An address of another machine is one already, and a bare one is this
	// machine's.
	if got, err := hcAddrArg(hs.root, "vps/t2"); err != nil || got != "vps/t2" {
		t.Errorf("hcAddrArg(vps/t2) = %q, %v", got, err)
	}
	if got, err := hcAddrArg(hs.root, " t3 "); err != nil || got != "t3" {
		t.Errorf("hcAddrArg(t3) = %q, %v", got, err)
	}
	if _, err := hcAddrArg(hs.root, "  "); err == nil {
		t.Error("hcAddrArg of nothing is no error")
	}
}
