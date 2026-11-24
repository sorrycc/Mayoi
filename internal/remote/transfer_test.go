package remote

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// connectFake connects to a host by an ssh that runs the commands on
// this machine.
func connectFake(t *testing.T) *Conn {
	t.Helper()
	host := os.Getenv("MAYOI_SSH_TEST_HOST")
	if host == "" {
		needsShell(t)
		host = "fake.invalid"
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	c := For(host)
	t.Cleanup(Shutdown)
	c.Connect()
	deadline := time.Now().Add(30 * time.Second)
	for !c.Connected() {
		if s, err := c.State(); s == Offline && err != nil || time.Now().After(deadline) {
			t.Fatalf("could not connect: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return c
}

func TestTransfer(t *testing.T) {
	c := connectFake(t)
	local := t.TempDir()
	write := func(rel, data string, mode os.FileMode) {
		p := filepath.Join(local, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("it's a.txt", "one", 0o644)
	write("tool.sh", "#!/bin/sh\n", 0o755)
	write("dir/sub/deep.txt", "deep", 0o600)
	os.Symlink("sub/deep.txt", filepath.Join(local, "dir", "link"))

	// The host's folder, which has a.txt's name already.
	host := t.TempDir()
	os.WriteFile(filepath.Join(host, "it's a.txt"), []byte("there"), 0o644)

	var p Progress
	got, err := c.Upload([]string{filepath.Join(local, "it's a.txt"), filepath.Join(local, "tool.sh"), filepath.Join(local, "dir")}, host, &p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{host + "/it's a copy.txt", host + "/tool.sh", host + "/dir"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("uploaded to %q", got)
	}
	if p.Total() != 3+10+4 || p.Done() < p.Total() {
		t.Errorf("progress %d of %d", p.Done(), p.Total())
	}
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}
	if read(host+"/it's a.txt") != "there" || read(host+"/it's a copy.txt") != "one" || read(host+"/dir/sub/deep.txt") != "deep" {
		t.Fatal("the files did not arrive")
	}
	if fi, _ := os.Stat(host + "/tool.sh"); fi == nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("tool.sh lost its mode: %v", fi)
	}
	if l, _ := os.Readlink(host + "/dir/link"); l != "sub/deep.txt" {
		t.Errorf("link = %q", l)
	}
	if left, _ := filepath.Glob(host + "/.mayoi-paste.*"); len(left) > 0 {
		t.Errorf("left %q", left)
	}

	// To a folder of its own, as for a terminal of the host.
	tmp, err := c.Upload([]string{filepath.Join(local, "tool.sh")}, "", nil)
	if err != nil || len(tmp) != 1 || read(tmp[0]) != "#!/bin/sh\n" {
		t.Fatalf("to a temporary folder: %q, %v", tmp, err)
	}
	os.RemoveAll(filepath.Dir(tmp[0]))

	// On the host, a folder twice, then into itself.
	got, err = c.CopyOnHost([]string{host + "/dir", host + "/dir"}, host, nil)
	if err != nil || len(got) != 2 || got[0] != host+"/dir copy" || got[1] != host+"/dir copy 2" {
		t.Fatalf("copied to %q, %v", got, err)
	}
	if read(host+"/dir copy 2/sub/deep.txt") != "deep" {
		t.Fatal("the copy is not whole")
	}
	if _, err := c.CopyOnHost([]string{host + "/dir"}, host+"/dir/sub", nil); err == nil {
		t.Fatal("pasted a folder into itself")
	}

	// Back to this machine.
	back := t.TempDir()
	var dp Progress
	if err := c.Download(host+"/dir", filepath.Join(back, "dir"), &dp); err != nil {
		t.Fatal(err)
	}
	if read(filepath.Join(back, "dir", "sub", "deep.txt")) != "deep" || dp.Done() == 0 {
		t.Fatal("the folder did not come back")
	}
	if l, _ := os.Readlink(filepath.Join(back, "dir", "link")); l != "sub/deep.txt" {
		t.Errorf("link back = %q", l)
	}
	if err := c.Download(host+"/tool.sh", filepath.Join(back, "t.sh"), nil); err != nil || read(filepath.Join(back, "t.sh")) != "#!/bin/sh\n" {
		t.Fatalf("file back: %v", err)
	}
	if err := c.Download(host+"/gone", filepath.Join(back, "gone"), nil); err == nil {
		t.Fatal("downloaded a file that is not there")
	}

	// Canceled, a transfer leaves nothing behind.
	stop := NewProgress()
	stop.Cancel()
	if _, err := c.Upload([]string{filepath.Join(local, "dir")}, host+"/dir/sub", stop); err != ErrCanceled {
		t.Fatalf("canceled upload: %v", err)
	}
	if _, err := os.Stat(host + "/dir/sub/dir"); err == nil {
		t.Fatal("a canceled upload arrived")
	}
	if err := c.Download(host+"/dir", filepath.Join(back, "again"), stop); err != ErrCanceled {
		t.Fatalf("canceled download: %v", err)
	}
}

// fakeSSHW is fakeSSH, which also carries -W host:port to a port of this
// machine, with nc.
const fakeSSHW = `#!/bin/sh
for a; do
  if [ "$w" = 1 ]; then exec nc "${a%:*}" "${a##*:}"; fi
  [ "$a" = -W ] && w=1
done
` + fakeSSH

// shPool is a connection by a pool whose workers run the scripts with
// this machine's sh, as the pool on Windows runs them on the host.
func shPool(t *testing.T) *Conn {
	t.Helper()
	needsShell(t)
	open := func() (workerPipe, error) {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		done := make(chan struct{})
		go func() {
			serveFrames(pipeRW{inR, outW}, func(script string, stdin io.Reader, stdout, stderr io.Writer) int {
				cmd := exec.Command("sh", "-c", script)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
				if err := cmd.Run(); err != nil {
					if e, ok := err.(*exec.ExitError); ok {
						return e.ExitCode()
					}
					return 127
				}
				return 0
			})
			outW.Close()
			close(done)
		}()
		return workerPipe{in: inW, out: outR, kill: func() { inR.Close(); outW.Close() }, done: done}, nil
	}
	c := &Conn{Host: "box", subs: map[int]func(){}, state: Connected}
	p := newPool(open, c.markLost)
	c.pool = p
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	return c
}

// Windows and WSL reach the host by the pool, with no master's socket.
func TestTransferOverPool(t *testing.T) {
	c := shPool(t)
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := t.TempDir()
	got, err := c.Upload([]string{filepath.Join(local, "a.txt")}, host, nil)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(got) != 1 || got[0] != host+"/a.txt" {
		t.Fatalf("uploaded to %q", got)
	}
	copied, err := c.CopyOnHost([]string{host + "/a.txt"}, host, nil)
	if err != nil || len(copied) != 1 || copied[0] != host+"/a copy.txt" {
		t.Fatalf("copied to %q: %v", copied, err)
	}
	var p Progress
	to := filepath.Join(t.TempDir(), "got")
	if err := c.Download(host, to, &p); err != nil {
		t.Fatalf("download: %v", err)
	}
	for _, name := range []string{"a.txt", "a copy.txt"} {
		if b, err := os.ReadFile(filepath.Join(to, name)); err != nil || string(b) != "one" {
			t.Errorf("downloaded %s: %q, %v", name, b, err)
		}
	}
	if p.Done() == 0 {
		t.Error("no progress")
	}
}

func TestForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		args := forwardArgs("me@box:2222", 18080, 8080)
		for _, a := range args {
			if a == "-W" || strings.Contains(a, "ControlPath") || strings.Contains(a, "ControlMaster") {
				t.Fatalf("windows forward: %q", args)
			}
		}
		text := strings.Join(args, "\n")
		if !strings.Contains(text, "-N") || !strings.Contains(text, "-L\n127.0.0.1:18080:localhost:8080") || !strings.Contains(text, "-p\n2222\nme@box") {
			t.Fatalf("forward args: %q", args)
		}
		if !consoleHidden(hiddenCommand("ssh", args...)) {
			t.Fatal("forward ssh would open a console window")
		}
		return
	}
	host := os.Getenv("MAYOI_SSH_TEST_HOST")
	if host == "" {
		needsShell(t)
		host = "fake-forward.invalid"
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSHW), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	c := For(host)
	t.Cleanup(Shutdown)
	c.Connect()
	for deadline := time.Now().Add(30 * time.Second); !c.Connected(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("could not connect")
		}
	}
	// A server "on the host": on this machine, as the fake ssh runs there.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello from the host "+r.URL.Path)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	f, err := c.Forward(port)
	if err != nil {
		t.Fatal(err)
	}
	// Its port is taken here by the server itself: another one.
	if f.Local == port || f.Local == 0 || c.LocalPort(port) != f.Local {
		t.Fatalf("local port %d for %d", f.Local, port)
	}
	if again, _ := c.Forward(port); again != f {
		t.Fatal("forwarded twice")
	}
	for range 3 {
		res, err := http.Get(f.URL() + "/x")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) != "hello from the host /x" {
			t.Fatalf("body %q", body)
		}
	}
	f.Close()
	if len(c.Forwards()) != 0 || c.LocalPort(port) != 0 {
		t.Fatal("still forwarded")
	}
	if _, err := http.Get(f.URL()); err == nil {
		t.Fatal("still listening")
	}
}

// TestWriteFileIf: a write with the stamp of the file read goes, one
// after the file changed on the host fails and leaves it.
func TestWriteFileIf(t *testing.T) {
	c := connectFake(t)
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("one\n"), 0o644)
	info, _, err := c.ReadFileInfo(p, 0)
	if err != nil || info.Stamp() == "" {
		t.Fatalf("read: %+v, %v", info, err)
	}
	info, err = c.WriteFileIf(p, []byte("two\n"), info.Stamp())
	if err != nil {
		t.Fatal(err)
	}
	// Changed on the host meanwhile.
	os.WriteFile(p, []byte("changed there\n"), 0o644)
	if _, err := c.WriteFileIf(p, []byte("three\n"), info.Stamp()); err != ErrChanged {
		t.Fatalf("over a change: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "changed there\n" {
		t.Fatalf("the file is %q", b)
	}
	// Without a stamp, or once gone, it writes.
	if _, err := c.WriteFileIf(p, []byte("four\n"), ""); err != nil {
		t.Fatal(err)
	}
	os.Remove(p)
	if _, err := c.WriteFileIf(p, []byte("five\n"), info.Stamp()); err != nil {
		t.Fatal(err)
	}
}
