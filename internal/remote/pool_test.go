package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolReusesWorkers(t *testing.T) {
	var starts atomic.Int32
	var fail atomic.Bool
	var mu sync.Mutex
	var cmds []*exec.Cmd
	open := func() (workerPipe, error) {
		if fail.Load() {
			return workerPipe{}, errors.New("ssh is gone")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "MAYOI_FAKE_REMOTE=ssh")
		hideConsole(cmd)
		in, err := cmd.StdinPipe()
		if err != nil {
			return workerPipe{}, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return workerPipe{}, err
		}
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return workerPipe{}, err
		}
		starts.Add(1)
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		mu.Lock()
		cmds = append(cmds, cmd)
		mu.Unlock()
		return workerPipe{in: in, out: out, kill: func() { cmd.Process.Kill() }, done: done}, nil
	}
	c := &Conn{Host: "box", subs: map[int]func(){}, state: Connected}
	p := newPool(open, c.markLost)
	c.pool = p
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if got := starts.Load(); got != int32(poolSize) {
		t.Fatalf("started %d workers, want %d", got, poolSize)
	}

	var quick bytes.Buffer
	for i := 0; i < 8; i++ {
		code, err := p.Call(context.Background(), "cat", bytes.NewReader([]byte{byte(i), 0}), &quick, nil)
		if err != nil || code != 0 {
			t.Fatalf("command %d: code %d err %v", i, code, err)
		}
	}
	if got := starts.Load(); got != int32(poolSize) {
		t.Fatalf("ssh started %d times for 8 commands", got)
	}

	ctxs := make([]context.CancelFunc, poolSize)
	holds := make([]*io.PipeWriter, poolSize)
	for i := 0; i < poolSize; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs[i] = cancel
		pr, pw := io.Pipe()
		holds[i] = pw
		go p.Call(ctx, "inotifywait -m", pr, nil, nil)
	}
	waitFor(t, func() bool {
		_, busy, q := p.counts()
		return busy == poolSize && q == 0
	})
	catDone := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		_, err := p.Call(context.Background(), "cat", bytes.NewReader([]byte{'z', 0}), &buf, nil)
		catDone <- err
	}()
	waitFor(t, func() bool {
		_, _, q := p.counts()
		return q == 1
	})
	if got := starts.Load(); got != int32(poolSize) {
		t.Fatalf("a queued command started ssh, %d times", got)
	}
	ctxs[0]()
	select {
	case err := <-catDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued command did not run after cancel")
	}
	if got := starts.Load(); got != int32(poolSize)+1 {
		t.Fatalf("cancel replaced %d workers, starts %d", 1, got)
	}
	for _, cancel := range ctxs {
		cancel()
	}
	for _, pw := range holds {
		pw.Close()
	}
	fail.Store(true)
	waitFor(t, func() bool {
		mu.Lock()
		all := append([]*exec.Cmd(nil), cmds...)
		mu.Unlock()
		for _, cmd := range all {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
		s, err := c.State()
		return s == Offline && errors.Is(err, ErrLost)
	})
}

func TestPoolDisconnect(t *testing.T) {
	open, cmds := fakeOpener(nil)
	c := &Conn{Host: "box", subs: map[int]func(){}, state: Connected}
	p := newPool(open, c.markLost)
	c.pool = p
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var holds []*io.PipeWriter
	for range poolSize {
		pr, pw := io.Pipe()
		holds = append(holds, pw)
		go p.Call(ctx, "inotifywait -m", pr, nil, nil)
	}
	defer func() {
		for _, pw := range holds {
			pw.Close()
		}
	}()
	waitFor(t, func() bool {
		_, busy, q := p.counts()
		return busy == poolSize && q == 0
	})
	queued := make(chan error, poolSize)
	for range poolSize {
		go func() {
			_, err := p.Call(context.Background(), "cat", bytes.NewReader(nil), nil, nil)
			queued <- err
		}()
	}
	waitFor(t, func() bool {
		_, _, q := p.counts()
		return q == poolSize
	})
	c.Disconnect()
	deadline := time.After(5 * time.Second)
	for range poolSize {
		select {
		case err := <-queued:
			if !errors.Is(err, ErrOffline) {
				t.Fatalf("queued command: %v", err)
			}
		case <-deadline:
			t.Fatal("Disconnect did not end the queue")
		}
	}
	s, err := c.State()
	if s != Offline || err != nil {
		t.Fatalf("disconnect state %v %v", s, err)
	}
	gone := time.After(5 * time.Second)
	for _, proc := range cmds() {
		select {
		case <-proc.done:
		case <-gone:
			t.Fatal("a worker survived Disconnect")
		}
	}
}

type startedProc struct {
	cmd  *exec.Cmd
	done <-chan struct{}
}

func fakeOpener(fail *atomic.Bool) (func() (workerPipe, error), func() []startedProc) {
	var mu sync.Mutex
	var cmds []startedProc
	open := func() (workerPipe, error) {
		if fail != nil && fail.Load() {
			return workerPipe{}, errors.New("ssh is gone")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "MAYOI_FAKE_REMOTE=ssh")
		hideConsole(cmd)
		in, err := cmd.StdinPipe()
		if err != nil {
			return workerPipe{}, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return workerPipe{}, err
		}
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return workerPipe{}, err
		}
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		mu.Lock()
		cmds = append(cmds, startedProc{cmd, done})
		mu.Unlock()
		return workerPipe{in: in, out: out, kill: func() { cmd.Process.Kill() }, done: done}, nil
	}
	return open, func() []startedProc {
		mu.Lock()
		defer mu.Unlock()
		return append([]startedProc(nil), cmds...)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}
