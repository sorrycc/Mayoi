package remotefs

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestPathsAndQuoting(t *testing.T) {
	for _, raw := range []string{"ssh://work/", "ssh://work/home/me/project", "ssh://me@work:2222/a%20b/x%27y", "ssh://me@[::1]:22/tmp"} {
		virtual, err := Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		virtual = filepath.Clean(virtual)
		again, err := Parse(Display(virtual))
		if err != nil || filepath.Clean(again) != virtual {
			t.Fatalf("roundtrip %q: %q %v", raw, again, err)
		}
		if !IsRemote(virtual) {
			t.Fatal("lost remote namespace")
		}
	}
	for _, raw := range []string{"/tmp", "ssh://-oProxyCommand=bad/tmp", "ssh://me:secret@host/tmp", "ssh://host:0/tmp", "ssh://host/tmp?cmd=bad", "ssh://host/%00"} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	original := "a b'$(touch should-not-exist);\nend"
	out, err := exec.Command("sh", "-c", "printf %s "+Quote(original)).Output()
	if err != nil || string(out) != original {
		t.Fatalf("unsafe quoting: %q %v", out, err)
	}
}

// Exercise real SFTP IO through the operating system's server, without needing
// an SSH account, keys, a listening port, or modifications to user SSH config.
func TestSFTPAtomicWrite(t *testing.T) {
	server := "/usr/libexec/sftp-server"
	if _, err := os.Stat(server); err != nil {
		t.Skip("system SFTP server unavailable")
	}
	cmd := exec.Command(server)
	rd, _ := cmd.StdoutPipe()
	wr, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c, err := sftp.NewClientPipe(rd, wr)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{Host: "sftp-test"}
	connections.Lock()
	connections.clients[target] = &connection{client: c, cmd: cmd}
	connections.Unlock()
	t.Cleanup(func() {
		c.Close()
		cmd.Process.Kill()
		cmd.Wait()
		connections.Lock()
		delete(connections.clients, target)
		connections.Unlock()
	})
	dir := t.TempDir()
	original := filepath.Join(dir, "a ' file.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(original, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(target.Virtual(link), []byte("after\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	limited, err := ReadFileLimit(target.Virtual(original), 6)
	if err != nil || string(limited) != "after\n" {
		t.Fatalf("bounded read: %q %v", limited, err)
	}
	if _, err := ReadFileLimit(target.Virtual(original), 5); err == nil {
		t.Fatal("oversized read accepted")
	}
	if _, err := ReadFileLimit(target.Virtual(dir), 100); err == nil {
		t.Fatal("directory read accepted")
	}
	data, _ := os.ReadFile(original)
	if !bytes.Equal(data, []byte("after\n")) {
		t.Fatalf("not written remotely: %q", data)
	}
	info, _ := os.Lstat(link)
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	info, _ = os.Stat(original)
	if info.Mode().Perm() != 0o640 {
		t.Fatal("permissions changed")
	}
	entries, err := ReadDir(target.Virtual(dir))
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files leaked: %v %v", entries, err)
	}
	if err := WriteFile(target.Virtual(filepath.Join(dir, "missing", "file")), []byte("bad"), 0o644); err == nil {
		t.Fatal("missing directory accepted")
	}
	if err := Rename(target.Virtual(original), Target{Host: "other"}.Virtual(original)); err == nil {
		t.Fatal("cross-host rename accepted")
	}
	c.Close()
	if err := WriteFile(target.Virtual(original), []byte("lost"), 0o644); err == nil {
		t.Fatal("write to closed connection succeeded")
	}
	data, _ = os.ReadFile(original)
	if string(data) != "after\n" {
		t.Fatal("original lost after failed save")
	}
}

func TestSSHIntegration(t *testing.T) {
	raw := os.Getenv("MAYOI_SSH_TEST_URL")
	if raw == "" {
		t.Skip("set MAYOI_SSH_TEST_URL to a disposable remote fixture directory")
	}
	root, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := Connect(root); err != nil {
		t.Fatal(err)
	}
	defer CloseAll()
	p := filepath.Join(root, "ssh-proof.txt")
	if err := WriteFile(p, []byte("written over SSH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(p)
	if err != nil || string(data) != "written over SSH\n" {
		t.Fatalf("read: %q %v", data, err)
	}
	cmd, cancel := GitCommand(root, "status", "--porcelain")
	defer cancel()
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ssh-proof.txt") {
		t.Fatalf("remote git: %s %v", out, err)
	}
	t.Log("SSH authentication, SFTP write/read, and remote git status passed")
}

// Shutdown also owns an SSH process still waiting for the SFTP handshake.
func TestCloseAllStopsPendingHandshake(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ssh shim")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	result := make(chan error, 1)
	go func() { result <- Connect(Target{Host: "handshake-fixture"}.Virtual("/demo")) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		connections.Lock()
		pending := len(connections.pending)
		connections.Unlock()
		if pending > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SSH handshake did not start")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	CloseAll()
	if time.Since(start) > time.Second {
		t.Fatal("shutdown waited for the handshake timeout")
	}
	if err := <-result; err == nil {
		t.Fatal("closed handshake reported success")
	}
	connections.Lock()
	count := len(connections.clients) + len(connections.pending)
	connections.closing = false
	connections.Unlock()
	if count != 0 {
		t.Fatal("SSH processes remained registered")
	}
	if _, err := os.Stat(controlDir); !os.IsNotExist(err) {
		t.Fatal("control directory was not removed")
	}
	controlOnce = sync.Once{}
	controlDir = ""
}
