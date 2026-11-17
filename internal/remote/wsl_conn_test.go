package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWSLConn(t *testing.T) {
	logDir, stop := useFakeWSL(t, "Ubuntu\r\nDebian\r\nMy Distro\r\n")
	t.Cleanup(func() {
		Shutdown()
		connsMu.Lock()
		for _, name := range []string{"Ubuntu", "Debian"} {
			delete(conns, connKey(KindSSH, name))
			delete(conns, connKey(KindWSL, name))
		}
		connsMu.Unlock()
	})

	if err := os.WriteFile(stop, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, _, ok := Lookup(WSLPath("Debian", "/home/me"))
	if !ok {
		t.Fatal("debian path")
	}
	failed.Connect()
	state, err := waitState(t, failed, Offline)
	if state != Offline || err == nil || !strings.Contains(err.Error(), "WSL") || strings.Contains(err.Error(), "SSH:") {
		t.Fatalf("start error %v %v", state, err)
	}
	if err := os.Remove(stop); err != nil {
		t.Fatal(err)
	}
	clearLogs(t, logDir)

	c, rpath, ok := Lookup(WSLPath("Ubuntu", "/home/me/app"))
	if !ok || rpath != "/home/me/app" || c.Kind() != KindWSL || c == For("Ubuntu") {
		t.Fatalf("conn kind %v path %q shared %v", c.Kind(), rpath, c == For("Ubuntu"))
	}
	c.Connect()
	state, err = waitState(t, c, Connected)
	if state != Connected {
		t.Fatalf("connected %v %v", state, err)
	}
	args := loggedArgs(t, logDir)
	if len(args) < poolSize {
		t.Fatalf("started %d workers", len(args))
	}
	want := strings.Join([]string{"-d", "Ubuntu", "-e", "sh", "-c", wslBootstrap}, "\n")
	for _, got := range args {
		text := strings.Join(got, "\n")
		if text != want || strings.Contains(text, "Control") || strings.Contains(text, "BatchMode") {
			t.Fatalf("worker args %q", got)
		}
	}
	started := len(args)
	for range 3 {
		if _, err := c.Run(nil, "true"); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(loggedArgs(t, logDir)); got != started {
		t.Fatalf("commands started workers: %d, was %d", got, started)
	}

	if err := os.WriteFile(stop, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = waitState(t, c, Offline)
	if state != Offline || err == nil || !strings.Contains(err.Error(), wslUnavailable) || strings.Contains(err.Error(), "SSH:") {
		t.Fatalf("after the distribution stops: %v %v", state, err)
	}
	if err := os.Remove(stop); err != nil {
		t.Fatal(err)
	}
	c.Connect()
	state, err = waitState(t, c, Connected)
	if state != Connected {
		t.Fatalf("retry %v %v", state, err)
	}
}

func useFakeWSL(t *testing.T, names string) (logDir, stop string) {
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
	copyFile(t, src, filepath.Join(dir, bin))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAYOI_FAKE_WSL", "1")
	t.Setenv("MAYOI_FAKE_WSL_NAMES", names)
	logDir = filepath.Join(dir, "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAYOI_FAKE_WSL_LOG", logDir)
	stop = filepath.Join(dir, "stop")
	t.Setenv("MAYOI_FAKE_WSL_STOP", stop)
	return logDir, stop
}

func waitState(t *testing.T, c *Conn, want State) (State, error) {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		s, err := c.State()
		if s == want {
			return s, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, err := c.State()
	t.Fatalf("state %v, want %v, err %v", s, want, err)
	return s, err
}

func clearLogs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

func loggedArgs(t *testing.T, dir string) [][]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Split(strings.TrimRight(string(b), "\n"), "\n"))
	}
	return out
}
