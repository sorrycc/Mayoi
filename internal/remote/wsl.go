package remote

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Distros lists installed WSL distributions, as wsl -l -q prints them.
// The text is UTF-16 LE, with or without a BOM.
func Distros() ([]string, error) {
	out, err := hiddenCommand("wsl", "-l", "-q").Output()
	if err != nil {
		return nil, err
	}
	return decodeWSLList(out)
}

func decodeWSLList(b []byte) ([]string, error) {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	} else if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return nil, errors.New("wsl list is not UTF-16 LE")
	}
	if len(b)%2 != 0 {
		return nil, errors.New("wsl list is not UTF-16")
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	var names []string
	for _, line := range strings.Split(string(utf16.Decode(units)), "\n") {
		line = strings.Trim(line, " \t\r\x00\uFEFF")
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

// allowDistro reports whether name is an installed distribution.
// Names come only from that list. CheckHost rejects some of them.
func allowDistro(name string) error {
	names, err := Distros()
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("WSL distribution %q is not installed", name)
}

// distroArgs is wsl's -d and name, then extra, when name is installed.
func distroArgs(name string, extra ...string) ([]string, error) {
	if err := allowDistro(name); err != nil {
		return nil, err
	}
	args := make([]string, 0, 2+len(extra))
	args = append(args, "-d", name)
	return append(args, extra...), nil
}

// wslUnavailable is what a window says when its distribution is not running.
const wslUnavailable = "the WSL distribution is not available"

// WSLUnavailable is that sentence, for a window that has not opened.
func WSLUnavailable() string { return wslUnavailable }

// wslBootstrap is the short command wsl.exe runs. The frame shell follows
// on stdin, so an argument that looks like a Windows path is never rewritten.
const wslBootstrap = `IFS= read -r n; eval "$(dd bs=1 count=$n 2>/dev/null)"`

// wslReadyLine is the one line the shell writes before it speaks frames.
// startWSL waits for it, so a process that exits without running the shell
// is a start failure even when the script fits in the pipe buffer.
const wslReadyLine = "ready"

// dialWSL starts the session pool with wsl.exe -d. The user's SSH config
// is not read. A distribution that is stopped is started by that process.
func (c *Conn) dialWSL(ctx context.Context) {
	fail := func(err error) {
		c.mu.Lock()
		if c.ctx == ctx {
			c.state, c.err, c.pool = Offline, err, nil
			c.cancel()
		}
		c.mu.Unlock()
		c.notify()
	}
	if err := allowDistro(c.Host); err != nil {
		fail(err)
		return
	}
	var notes syncBuffer
	var gate sync.Mutex
	up := false
	p := newPool(func() (workerPipe, error) {
		return startWSL(ctx, c.Host, &notes)
	}, func(error) {
		gate.Lock()
		ok := up
		gate.Unlock()
		if ok {
			c.markLost(errors.New(wslUnavailable))
		}
	})
	if err := p.Start(); err != nil {
		fail(wslFail(notes.String(), err))
		return
	}
	if ctx.Err() != nil {
		p.Stop()
		return
	}
	c.mu.Lock()
	if c.ctx != ctx {
		c.mu.Unlock()
		p.Stop()
		return
	}
	c.pool = p
	c.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	info, err := c.probePool(pctx, p, false)
	cancel()
	if err != nil {
		p.Stop()
		c.mu.Lock()
		if c.pool == p {
			c.pool = nil
		}
		c.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("timed out connecting")
		} else {
			err = wslFail(notes.String(), err)
		}
		fail(err)
		return
	}
	gate.Lock()
	up = true
	gate.Unlock()
	c.mu.Lock()
	if c.ctx != ctx || c.state != Connecting {
		c.mu.Unlock()
		p.Stop()
		return
	}
	c.state, c.info = Connected, info
	c.mu.Unlock()
	c.notify()
}

// startWSL runs one long-lived wsl.exe -d distro. The name is used only
// after it has been found in the distribution list.
func startWSL(ctx context.Context, distro string, stderr *syncBuffer) (workerPipe, error) {
	cmd := hiddenCommand("wsl", "-d", distro, "-e", "sh", "-c", wslBootstrap)
	if stderr == nil {
		cmd.Stderr = io.Discard
	} else {
		cmd.Stderr = stderr
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return workerPipe{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return workerPipe{}, err
	}
	if err := cmd.Start(); err != nil {
		return workerPipe{}, wslStartError(err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	// The ready line is not part of the frame stream. It is read here, and
	// the pipe the pool uses starts at the first frame. dd writes it: a
	// shell can buffer printf on a pipe and never flush before it reads
	// the next command.
	script := "printf '%s\\n' " + wslReadyLine + " | dd bs=1 2>/dev/null\n" + frameShell
	payload := strconv.Itoa(len(script)) + "\n" + script
	writeErr := make(chan error, 1)
	go func() {
		_, err := io.WriteString(in, payload)
		writeErr <- err
	}()
	frames := bufio.NewReader(out)
	readyErr := make(chan error, 1)
	go func() {
		line, err := frames.ReadString('\n')
		if err != nil {
			readyErr <- err
			return
		}
		if strings.TrimSpace(line) != wslReadyLine {
			readyErr <- errors.New(wslUnavailable)
			return
		}
		readyErr <- nil
	}()
	fail := func() (workerPipe, error) {
		msg := ""
		if stderr != nil {
			msg = stderr.String()
		}
		return workerPipe{}, wslFail(msg, errors.New(wslUnavailable))
	}
	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return workerPipe{}, ctx.Err()
	case <-done:
		return fail()
	case err := <-writeErr:
		if err != nil {
			cmd.Process.Kill()
			<-done
			return fail()
		}
		// The script can sit in the pipe before the process runs. Ready,
		// or the process exiting, is what says whether the shell started.
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			<-done
			return workerPipe{}, ctx.Err()
		case <-done:
			return fail()
		case err := <-readyErr:
			if err != nil {
				cmd.Process.Kill()
				<-done
				return fail()
			}
		case <-time.After(45 * time.Second):
			cmd.Process.Kill()
			<-done
			return workerPipe{}, errors.New("timed out connecting")
		}
	case err := <-readyErr:
		if err != nil {
			cmd.Process.Kill()
			<-done
			return fail()
		}
	case <-time.After(45 * time.Second):
		cmd.Process.Kill()
		<-done
		return workerPipe{}, errors.New("timed out connecting")
	}
	return workerPipe{in: in, out: readerCloser{frames, out}, kill: func() { cmd.Process.Kill() }, done: done}, nil
}

// readerCloser is a bufio.Reader over a pipe, closed with the pipe.
type readerCloser struct {
	io.Reader
	io.Closer
}

func wslStartError(err error) error {
	return fmt.Errorf("could not run wsl: %w", err)
}

// wslFail is the distribution's own words, or that it is not available.
// It is not an SSH login failure.
func wslFail(stderr string, fallback error) error {
	var last string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "Warning:") {
			last = line
		}
	}
	if last == "" {
		if fallback != nil {
			return fallback
		}
		return errors.New(wslUnavailable)
	}
	return errors.New(last)
}
