package remote

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestShellLoop(t *testing.T) {
	cmd := startFrameShell(t)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		wait := make(chan struct{})
		go func() {
			cmd.Wait()
			close(wait)
		}()
		select {
		case <-wait:
		case <-time.After(3 * time.Second):
		}
	}()
	rw := streamRW{out, in}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pr, pw := io.Pipe()
	var early lockedBuf
	done := make(chan error, 1)
	go func() {
		_, err := callFrames(ctx, rw, "printf 'hi\n'; cat >/dev/null", pr, &early, io.Discard)
		done <- err
	}()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(early.String(), "hi") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(early.String(), "hi") {
		t.Fatalf("early output %q shell %q", early.String(), errb.Bytes())
	}
	pw.Close()
	if err := <-done; err != nil {
		t.Fatalf("first command: %v shell %q", err, errb.Bytes())
	}

	var got bytes.Buffer
	code, err := callFrames(ctx, rw, "cat", bytes.NewReader([]byte("a\x00b")), &got, io.Discard)
	if err != nil || code != 0 || got.String() != "a\x00b" {
		t.Fatalf("cat code %d out %q err %v shell %q", code, got.Bytes(), err, errb.Bytes())
	}
	code, err = callFrames(ctx, rw, "exit 3", bytes.NewReader(nil), io.Discard, io.Discard)
	if err != nil || code != 3 {
		t.Fatalf("exit code %d err %v shell %q", code, err, errb.Bytes())
	}
}

func startFrameShell(t *testing.T) *exec.Cmd {
	t.Helper()
	if p, err := exec.LookPath("sh"); err == nil {
		return exec.Command(p, "-c", frameShell)
	}
	if os.Getenv("MAYOI_SHELL_TEST") == "" || runtime.GOOS != "windows" {
		t.Skip("sh is not on PATH")
	}
	f, err := os.CreateTemp("", "mayoi-frame-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(frameShell); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return exec.Command("wsl.exe", "-e", "/bin/sh", wslPath(f.Name()))
}

func wslPath(p string) string {
	if len(p) < 3 || p[1] != ':' {
		return p
	}
	return "/mnt/" + strings.ToLower(p[:1]) + strings.ReplaceAll(p[2:], `\`, "/")
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
