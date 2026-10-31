package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// useFakeInstall stands in for herdr's installer, which install runs,
// with hc missing until then.
func useFakeInstall(t *testing.T, install func() ([]byte, error)) (runs *int) {
	useFakeHC(t, nil)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	old := hcInstall
	t.Cleanup(func() { hcInstall, hcInstalling, hcOffered = old, false, false })
	runs = new(int)
	hcInstall = func() ([]byte, error) {
		*runs++
		return install()
	}
	return runs
}

// hc that herdr's installer put in ~/.local/bin is found off the path,
// as herdr-core when the hc beside it is missing, and runs from there.
func TestHCLocalBin(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	hs := newHarness(t, true)
	a := hs.a
	useFakeHC(t, nil)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	a.settings.TerminalBackend = "hc"
	if a.hcBackend() || hcPath() != "" {
		t.Fatalf("hc %q without one", hcPath())
	}
	core := filepath.Join(hcLocalBin(), "herdr-core")
	if err := os.WriteFile(core, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.hcBackend() || hcPath() != core {
		t.Errorf("hc %q, want %q", hcPath(), core)
	}
	hc := filepath.Join(hcLocalBin(), "hc")
	if err := os.WriteFile(hc, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := hcAttach("t1", false); !slices.Equal(got, []string{hc, "attach", "t1"}) {
		t.Errorf("attach command %q", got)
	}
}

// Install hc… runs the installer once at a time, says so, and once it
// is done new terminals run in herdr-core.
func TestInstallHC(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	hs := newHarness(t, true)
	a := hs.a
	a.settings.TerminalBackend = "hc"
	runs := useFakeInstall(t, func() ([]byte, error) {
		return []byte("hc 0.1.14 installed into " + hcLocalBin() + "/herdr-core.\n"), os.WriteFile(filepath.Join(hcLocalBin(), "hc"), nil, 0o755)
	})
	a.run("view.installHC")
	a.run("view.installHC")
	hs.settle()
	if *runs != 1 {
		t.Errorf("installed %d times", *runs)
	}
	if !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, "New terminals run in herdr-core") }) {
		t.Errorf("no toast: %q", hs.tt.Texts())
	}
	if !a.hcBackend() {
		t.Error("no hc after installing")
	}
}

// An installer that fails says why, in the last line it printed.
func TestInstallHCFails(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	useFakeInstall(t, func() ([]byte, error) {
		return []byte("hc: no build for riscv64\n"), errors.New("exit status 1")
	})
	fails := a.fails
	a.installHC()
	hs.settle()
	if a.fails != fails+1 || !slices.ContainsFunc(hs.tt.Texts(), func(s string) bool { return strings.Contains(s, "Could not install hc: no build for riscv64") }) {
		t.Errorf("%d errors: %q", a.fails-fails, hs.tt.Texts())
	}
	if hcInstalling {
		t.Error("still installing")
	}
	if got := hcInstallError(nil, errors.New("exit status 127")); got != "exit status 127" {
		t.Errorf("error without output %q", got)
	}
}

// With terminalBackend "hc" and no hc, the first new terminal offers to
// install it, once, and runs in Mayoi; without the setting, none does.
func TestOfferHC(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	hs := newHarness(t, true)
	a := hs.a
	runs := useFakeInstall(t, func() ([]byte, error) { return nil, nil })
	a.run("view.newTerminal")
	hs.settle()
	if a.confirm != nil {
		t.Fatal("offered without terminalBackend hc")
	}
	a.settings.TerminalBackend = "hc"
	n := len(a.terms)
	a.run("view.newTerminal")
	hs.settle()
	if a.confirm == nil || len(a.terms) != n+1 || a.terms[n].hc != "" {
		t.Fatalf("offered %v, terminals %d", a.confirm != nil, len(a.terms)-n)
	}
	install := a.confirm.run
	a.confirm = nil
	a.run("view.newTerminal")
	hs.settle()
	if a.confirm != nil {
		t.Error("offered again")
	}
	install()
	hs.settle()
	if *runs != 1 {
		t.Errorf("installed %d times", *runs)
	}
}

// Where herdr has no build of hc, there is no Install hc….
func TestInstallHCHidden(t *testing.T) {
	hs := newHarness(t, true)
	old := hostOS
	t.Cleanup(func() { hostOS = old })
	hostOS = "windows"
	if slices.ContainsFunc(hs.a.commands(), func(c command) bool { return c.id == "view.installHC" }) {
		t.Error("Install hc… on Windows")
	}
}
