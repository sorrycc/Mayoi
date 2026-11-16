package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/settings"

	"github.com/egoist/mygo/ui"
)

func TestWSLConn(t *testing.T) {
	logDir, stop := installFakeWSL(t)
	s := settings.Default()
	w := s.NewWindow()
	h := &fakeHost{}
	a := newApp(h, s, w)
	t.Cleanup(func() {
		a.shutdown()
		remote.Shutdown()
	})
	tt := ui.NewTester(a.view, 1200, 760)
	hs := &harness{t: t, a: a, h: h, tt: tt}
	step(tt)

	if err := os.WriteFile(stop, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openWSL("Debian", "/home/me/app")
	waitText(t, tt, "WSL")
	if len(h.windows) != 0 || a.root != "" {
		t.Fatalf("a distribution that does not start opened %q windows %q", a.root, h.windows)
	}
	if joined := strings.Join(append(tt.Texts(), a.windowTitle()), "\n"); strings.Contains(joined, "SSH:") {
		t.Fatalf("start failure mentions SSH: %q", joined)
	}
	if err := os.Remove(stop); err != nil {
		t.Fatal(err)
	}

	folder := remote.WSLPath("Ubuntu", "/home/me/app")
	a.openWSL("Ubuntu", "/home/me/app")
	openedBy := time.Now().Add(45 * time.Second)
	for a.root != folder && time.Now().Before(openedBy) {
		step(tt)
	}
	if a.root != folder || len(h.windows) != 0 {
		t.Fatalf("opened %q windows %q errors %q", a.root, h.windows, a.errors)
	}
	step(tt)
	if !strings.Contains(a.windowTitle(), "[WSL: Ubuntu]") {
		t.Fatalf("title %q", a.windowTitle())
	}
	if joined := strings.Join(tt.Texts(), "\n"); strings.Contains(joined, "SSH:") || !strings.Contains(joined, "WSL: Ubuntu") {
		t.Fatalf("status %q", tt.Texts())
	}
	if len(logFiles(t, logDir)) == 0 {
		t.Fatal("no wsl worker")
	}

	if err := os.WriteFile(stop, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	lostBy := time.Now().Add(40 * time.Second)
	for a.link.kind != linkBackoff && time.Now().Before(lostBy) {
		step(tt)
	}
	if a.root != folder {
		t.Fatalf("the window was closed: %q", a.root)
	}
	if a.link.kind != linkBackoff || a.online() {
		t.Fatalf("link %+v online %v", a.link, a.online())
	}
	step(tt)
	joined := strings.Join(append(tt.Texts(), a.windowTitle()), "\n")
	if strings.Contains(joined, "SSH:") || !strings.Contains(joined, "WSL") {
		t.Fatalf("lost status %q", joined)
	}
	if err := os.Remove(stop); err != nil {
		t.Fatal(err)
	}
	a.reconnect()
	retryBy := time.Now().Add(45 * time.Second)
	for !a.online() && time.Now().Before(retryBy) {
		step(tt)
	}
	if !a.online() {
		t.Fatalf("retry left the folder offline: %+v", a.link)
	}
	_ = hs
}

func installFakeWSL(t *testing.T) (logDir, stop string) {
	t.Helper()
	dir := t.TempDir()
	bin := "wsl"
	if os.PathSeparator == '\\' {
		bin = "wsl.exe"
	}
	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(filepath.Join(dir, bin), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAYOI_FAKE_WSL", "1")
	t.Setenv("MAYOI_FAKE_WSL_NAMES", "Ubuntu\r\nDebian\r\nMy Distro\r\n")
	logDir = filepath.Join(dir, "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAYOI_FAKE_WSL_LOG", logDir)
	stop = filepath.Join(dir, "stop")
	t.Setenv("MAYOI_FAKE_WSL_STOP", stop)
	return logDir, stop
}

func waitText(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		step(tt)
		if strings.Contains(strings.Join(tt.Texts(), "\n"), text) {
			return
		}
	}
	t.Fatalf("timed out waiting for %q: %q", text, tt.Texts())
}

func logFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
