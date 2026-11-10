package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo"

	"mayoi/locales"
)

// sshHome is a home folder of its own for the test, with its config path.
func sshHome(t *testing.T) (home, config string) {
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home, filepath.Join(home, ".ssh", "config")
}

func wantMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s: mode %v; want %v", path, info.Mode().Perm(), want)
	}
}

func wantSSHConfigOpen(t *testing.T, hs *harness, config, text string) {
	t.Helper()
	a := hs.a
	hs.until("the config", func() bool { d := a.activeEditor(); return d != nil && d.sync != syncLoading })
	if d := a.activeEditor(); d.path != config || d.buf.String() != text {
		t.Fatalf("editor = %s with %q; want %s with %q", d.path, d.buf.String(), config, text)
	}
	if a.fails > 0 {
		t.Fatalf("%d failures", a.fails)
	}
}

// An existing config opens as it is, its mode and its folder's too.
func TestOpenSSHConfig(t *testing.T) {
	home, config := sshHome(t)
	const text = "Host dev\n  HostName 10.0.0.12\n"
	writeFile(t, config, []byte(text))
	for path, mode := range map[string]fs.FileMode{filepath.Join(home, ".ssh"): 0o755, config: 0o644} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	hs := newHarness(t, true)
	hs.a.openFile(hs.path("main.go"), true)
	hs.a.openSSHConfig()
	wantSSHConfigOpen(t, hs, config, text)
	wantMode(t, filepath.Join(home, ".ssh"), 0o755)
	wantMode(t, config, 0o644)
}

// A missing config is made empty and private, in a folder of its owner's,
// or in ~/.ssh as it was.
func TestOpenSSHConfigMakesIt(t *testing.T) {
	for _, dir := range []fs.FileMode{0, 0o755} {
		home, config := sshHome(t)
		if dir != 0 {
			if err := os.Mkdir(filepath.Join(home, ".ssh"), dir); err != nil {
				t.Fatal(err)
			}
		} else {
			dir = 0o700
		}
		hs := newHarness(t, true)
		hs.a.openSSHConfig()
		wantSSHConfigOpen(t, hs, config, "")
		wantMode(t, filepath.Join(home, ".ssh"), dir)
		wantMode(t, config, 0o600)
	}
}

// A linked config opens where it points, so saving it keeps the link.
func TestOpenSSHConfigLinked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	home, config := sshHome(t)
	real := filepath.Join(home, "dotfiles", "ssh", "config")
	const text = "Host dev\n"
	writeFile(t, real, []byte(text))
	if err := os.Mkdir(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, config); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	hs := newHarness(t, true)
	hs.a.openSSHConfig()
	wantSSHConfigOpen(t, hs, want, text)
}

// A config that cannot be made says why and opens nothing.
func TestOpenSSHConfigFails(t *testing.T) {
	home, _ := sshHome(t)
	writeFile(t, filepath.Join(home, ".ssh"), []byte("not a folder"))
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	fails := a.fails
	a.openSSHConfig()
	if a.fails != fails+1 {
		t.Fatalf("%d failures, %q", a.fails-fails, a.errors)
	}
	hs.settle()
	if d := a.activeEditor(); d.path != hs.path("main.go") {
		t.Fatalf("editor %s", d.path)
	}
}

// The box of the hosts opens the config with its button, and goes.
func TestRemotePickerOpensSSHConfig(t *testing.T) {
	useFakeSSH(t)
	_, config := sshHome(t)
	const text = "Host dev\n"
	writeFile(t, config, []byte(text))
	hs := newHarness(t, true)
	a := hs.a
	a.openRemotePicker()
	hs.settle()
	if !hs.tt.HasText("dev") {
		t.Fatalf("the box lists no host: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click(locales.CmdRemoteOpenConfig()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.remotePick.open {
		t.Fatal("the box stayed open")
	}
	wantSSHConfigOpen(t, hs, config, text)
}

// File → Remote SSH and the command palette have it too.
func TestSSHConfigCommand(t *testing.T) {
	remoteOnly(t)
	_, config := sshHome(t)
	hs := newHarness(t, true)
	a := hs.a
	var item *mygo.MenuItem
	for _, menu := range a.menu(false, func(id string, _ *mygo.Window) { a.run(id) }).Items() {
		for _, sub := range menu.Submenu {
			if sub.Label != locales.MenuRemoteSSH() {
				continue
			}
			for _, it := range sub.Submenu {
				if it.Label == locales.CmdRemoteOpenConfig() {
					item = it
				}
			}
		}
	}
	if item == nil {
		t.Fatal("no Open SSH Config in File → Remote SSH")
	}
	item.Click(item, nil)
	wantSSHConfigOpen(t, hs, config, "")
	a.run("file.close")
	hs.settle()
	if a.activeEditor() != nil {
		t.Fatal("the config's tab did not close")
	}
	a.run("remote.openConfig")
	wantSSHConfigOpen(t, hs, config, "")
}
