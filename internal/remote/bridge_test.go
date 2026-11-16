package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func testPool(t *testing.T) (*Conn, func() []startedProc) {
	t.Helper()
	open, procs := fakeOpener(nil)
	c := &Conn{Host: "box", subs: map[int]func(){}, state: Connected}
	p := newPool(open, c.markLost)
	c.pool = p
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Disconnect()
		deadline := time.After(5 * time.Second)
		for _, proc := range procs() {
			select {
			case <-proc.done:
			case <-deadline:
				proc.cmd.Process.Kill()
			}
		}
	})
	return c, procs
}

func watchCmd(t *testing.T, cmd *exec.Cmd) <-chan error {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- cmd.Wait()
		close(finished)
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-finished
	})
	return errc
}

func TestBridgeRejectsOffline(t *testing.T) {
	c := &Conn{Host: "box", subs: map[int]func(){}, state: Offline, pool: newPool(nil, nil)}
	if _, err := c.Command(nil, "cat"); !errors.Is(err, ErrOffline) {
		t.Fatalf("got %v", err)
	}
}

func TestBridgeStdoutIsTheScript(t *testing.T) {
	c, procs := testPool(t)
	cmd, err := c.Command(bytes.NewReader([]byte("a\x00b")), "cat")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != bridgeArg || !strings.HasPrefix(cmd.Args[2], "127.0.0.1:") {
		t.Fatalf("command %q", cmd.Args)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v stderr %q", err, errb.Bytes())
	}
	if out.String() != "a\x00b" || errb.Len() != 0 {
		t.Fatalf("out %q err %q", out.Bytes(), errb.Bytes())
	}
	if got := len(procs()); got != poolSize {
		t.Fatalf("ssh started %d times for one command", got)
	}
}

func TestBridgeExitCode(t *testing.T) {
	c, _ := testPool(t)
	cmd, err := c.Command(bytes.NewReader([]byte("no\x00")), "exit 7")
	if err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	cmd.Stdout = io.Discard
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("err %v stderr %q", err, errb.Bytes())
	}
	if string(errb.Bytes()) != "no\x00" {
		t.Fatalf("stderr %q", errb.Bytes())
	}
}

func TestBridgeStaysWhileHeld(t *testing.T) {
	c, _ := testPool(t)
	cmd, err := c.Command(nil, "cat")
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = io.Discard
	cmd.Env = append(os.Environ(), "MAYOI_CLI=1")
	done := watchCmd(t, cmd)
	select {
	case err := <-done:
		t.Fatalf("bridge exited while this instance holds the command: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	in.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not finish")
	}
}

func TestBridgeTokenIsOnce(t *testing.T) {
	c, _ := testPool(t)
	cmd, err := c.Command(bytes.NewReader([]byte("hi")), "cat")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", cmd.Args[2])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "%s\n", cmd.Args[3]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, busy, _ := c.pool.counts()
		return busy >= 1
	})
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	done := watchCmd(t, cmd)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a used token ran the command")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge stayed up with a used token")
	}
}

func TestBridgeKillReplacesWorker(t *testing.T) {
	c, procs := testPool(t)
	cmd, err := c.Command(nil, "cat")
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cmd.Stdout = io.Discard
	done := watchCmd(t, cmd)
	waitFor(t, func() bool {
		_, busy, _ := c.pool.counts()
		return busy >= 1
	})
	if got := len(procs()); got != poolSize {
		t.Fatalf("started %d before kill", got)
	}
	cmd.Process.Kill()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("killed bridge did not exit")
	}
	waitFor(t, func() bool { return len(procs()) == poolSize+1 })
}

func TestBridgeDisconnectEndsTheProcess(t *testing.T) {
	c, _ := testPool(t)
	cmd, err := c.Command(nil, "cat")
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cmd.Stdout = io.Discard
	done := watchCmd(t, cmd)
	waitFor(t, func() bool {
		_, busy, _ := c.pool.counts()
		return busy >= 1
	})
	c.Disconnect()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge still running after disconnect")
	}
}

func TestNotifyReadsScriptOutput(t *testing.T) {
	c, _ := testPool(t)
	cmd, err := c.Command(nil, "NOTIFY")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	errs, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	})
	errScan := bufio.NewScanner(errs)
	if !errScan.Scan() || !strings.Contains(errScan.Text(), "Watches established") {
		t.Fatalf("stderr %q %v", errScan.Text(), errScan.Err())
	}
	outScan := bufio.NewScanner(out)
	if !outScan.Scan() || outScan.Text() != "CREATE|/watched" {
		t.Fatalf("stdout %q %v", outScan.Text(), outScan.Err())
	}
	cmd.Wait()
}
