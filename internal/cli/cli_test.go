package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The mayoi command runs the app's executable with its arguments, told it
// is the command.
func TestScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	app := filepath.Join(dir, "My Mayoi.app")
	moved := filepath.Join(dir, "Moved.app")
	for _, a := range []string{app, moved} {
		exe := filepath.Join(a, "Contents", "MacOS", "Mayoi")
		os.MkdirAll(filepath.Dir(exe), 0o755)
		os.WriteFile(exe, []byte("#!/bin/sh\necho \"$0\" \"$MAYOI_CLI\"\nfor a in \"$@\"; do echo \"$a\"; done\n"), 0o755)
	}
	if err := write(bin, app); err != nil {
		t.Fatal(err)
	}
	// mdfind knows the app that moved.
	os.WriteFile(filepath.Join(bin, "mdfind"), []byte("#!/bin/sh\necho '"+moved+"'\n"), 0o755)
	run := func(args ...string) []string {
		cmd := exec.Command(filepath.Join(bin, "mayoi"), args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	}
	exe := filepath.Join(app, "Contents", "MacOS", "Mayoi")
	if got := run(); !slices.Equal(got, []string{exe + " 1"}) {
		t.Errorf("no args: %v", got)
	}
	got := run("-g", "a b.go:3", "run")
	if want := []string{exe + " 1", "-g", "a b.go:3", "run"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	os.RemoveAll(app)
	got = run("x")
	if want := []string{filepath.Join(moved, "Contents", "MacOS", "Mayoi") + " 1", "x"}; !slices.Equal(got, want) {
		t.Errorf("moved: got %v, want %v", got, want)
	}
}

// Refresh brings an older Mayoi's command up to date, and leaves another
// program's.
func TestRefresh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	old, foreign := filepath.Join(dir, "old"), filepath.Join(dir, "foreign")
	os.MkdirAll(old, 0o755)
	os.MkdirAll(foreign, 0o755)
	os.WriteFile(filepath.Join(old, "mayoi"), []byte("#!/bin/sh\n"+scriptMark+"\nopen -b x \"$@\"\n"), 0o755)
	os.WriteFile(filepath.Join(foreign, "mayoi"), []byte("#!/bin/sh\necho other\n"), 0o755)
	Refresh([]string{old, foreign, filepath.Join(dir, "none")}, "/Apps/Mayoi.app")
	if data, _ := os.ReadFile(filepath.Join(old, "mayoi")); string(data) != script("/Apps/Mayoi.app") {
		t.Errorf("not refreshed: %s", data)
	}
	if data, _ := os.ReadFile(filepath.Join(foreign, "mayoi")); string(data) != "#!/bin/sh\necho other\n" {
		t.Errorf("another program's changed: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "none", "mayoi")); err == nil {
		t.Error("installed where there was none")
	}
}

func TestInstallAndUninstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	p, err := Install([]string{first, second}, "")
	if err != nil || p != filepath.Join(first, "mayoi") {
		t.Fatalf("installed at %q: %v", p, err)
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("not executable: %v", err)
	}
	// Again, over its own.
	if _, err := Install([]string{first}, ""); err != nil {
		t.Fatal(err)
	}
	// Another program's command stays.
	os.MkdirAll(second, 0o755)
	foreign := filepath.Join(second, "mayoi")
	os.WriteFile(foreign, []byte("#!/bin/sh\necho other\n"), 0o755)
	if _, err := Install([]string{second}, ""); err == nil {
		t.Error("overwrote another program's mayoi")
	}
	removed, err := Uninstall([]string{first, second})
	if err != nil || !slices.Equal(removed, []string{p}) {
		t.Fatalf("removed %v: %v", removed, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("removed another program's mayoi")
	}
}

func TestDirs(t *testing.T) {
	local := filepath.Join("/home/u", ".local", "bin")
	if got := Dirs("/home/u", []string{"/usr/bin", local}); !slices.Equal(got, []string{local, "/usr/local/bin"}) {
		t.Errorf("on the path: %v", got)
	}
	if got := Dirs("/home/u", []string{"/usr/bin"}); !slices.Equal(got, []string{"/usr/local/bin"}) {
		t.Errorf("off the path: %v", got)
	}
}

// Opened from the Finder, the app has the system's folders only: the login
// shell's come first, and none twice.
func TestShellPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	tools := t.TempDir()
	sh := filepath.Join(t.TempDir(), "zsh")
	script := "#!/bin/sh\nPATH=" + tools + ":/usr/bin:$PATH; export PATH; shift; exec /bin/sh -c \"$1\"\n"
	if err := os.WriteFile(sh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	want := []string{tools, "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	if got := ShellPath(); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
