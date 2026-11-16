package remote

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Frames carry one command at a time over a long-lived worker. A command
// is a script, then its standard input in chunks, because a tar upload
// has no length known ahead of time. The worker answers with chunks of
// standard output and standard error, then the exit code. A chunk may
// hold any byte, including NUL.
//
// A frame is one line, "<kind> <length>", then exactly that many bytes.
// The kind is C command, I input, E end of input, O output, R error, or
// X exit code. The exit payload is the code in decimal.
const (
	frameCmd   = 'C'
	frameIn    = 'I'
	frameEnd   = 'E'
	frameOut   = 'O'
	frameErr   = 'R'
	frameExit  = 'X'
	frameChunk = 32 << 10
	frameMax   = 8 << 20
)

type framer struct {
	r  *bufio.Reader
	w  *bufio.Writer
	mu sync.Mutex
}

func newFramer(r io.Reader, w io.Writer) *framer {
	return &framer{r: bufio.NewReader(r), w: bufio.NewWriter(w)}
}

func (f *framer) write(kind byte, p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	hdr := append(append(make([]byte, 0, 24), kind, ' '), strconv.AppendInt(nil, int64(len(p)), 10)...)
	hdr = append(hdr, '\n')
	if _, err := f.w.Write(hdr); err != nil {
		return err
	}
	if _, err := f.w.Write(p); err != nil {
		return err
	}
	return f.w.Flush()
}

func (f *framer) readLine() (string, error) {
	line, err := f.r.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	s := string(line)
	return strings.TrimRight(s, "\r\n"), nil
}

func (f *framer) read() (byte, []byte, error) {
	line, err := f.r.ReadSlice('\n')
	if err != nil {
		return 0, nil, err
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if len(line) < 3 || line[1] != ' ' {
		return 0, nil, fmt.Errorf("remote frame: bad header")
	}
	kind := line[0]
	n, err := strconv.Atoi(string(line[2:]))
	if err != nil || n < 0 || n > frameMax {
		return 0, nil, fmt.Errorf("remote frame: bad length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return 0, nil, err
	}
	return kind, buf, nil
}

// callFrames runs script on a worker speaking the frame loop. stdin is
// read until EOF; its length is not needed. The exit code is script's.
// One worker reuses a framer across commands; callFrames is one command
// on a fresh stream.
func callFrames(ctx context.Context, rw io.ReadWriter, script string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return newFramer(rw, rw).roundTrip(ctx, script, stdin, stdout, stderr)
}

func (f *framer) roundTrip(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if err := f.write(frameCmd, []byte(script)); err != nil {
		return 0, err
	}
	inErr := make(chan error, 1)
	go func() {
		inErr <- sendInput(ctx, f, stdin)
	}()
	code, err := recvOutput(ctx, f, stdout, stderr)
	if err != nil {
		return 0, err
	}
	if err := <-inErr; err != nil {
		return code, err
	}
	return code, nil
}

func sendInput(ctx context.Context, f *framer, stdin io.Reader) error {
	if stdin != nil {
		buf := make([]byte, frameChunk)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := stdin.Read(buf)
			if n > 0 {
				if werr := f.write(frameIn, buf[:n]); werr != nil {
					return werr
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return f.write(frameEnd, nil)
}

func recvOutput(ctx context.Context, f *framer, stdout, stderr io.Writer) (int, error) {
	for {
		kind, p, err := readFrame(ctx, f)
		if err != nil {
			return 0, err
		}
		switch kind {
		case frameOut:
			if _, err := stdout.Write(p); err != nil {
				return 0, err
			}
		case frameErr:
			if _, err := stderr.Write(p); err != nil {
				return 0, err
			}
		case frameExit:
			return strconv.Atoi(string(p))
		default:
			return 0, fmt.Errorf("remote frame: want output, got %q", kind)
		}
	}
}

func readFrame(ctx context.Context, f *framer) (byte, []byte, error) {
	type result struct {
		kind byte
		p    []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		kind, p, err := f.read()
		ch <- result{kind, p, err}
	}()
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case r := <-ch:
		return r.kind, r.p, r.err
	}
}

// serveFrames reads commands from rw and runs each one. It returns when
// the stream ends.
func serveFrames(rw io.ReadWriter, run func(script string, stdin io.Reader, stdout, stderr io.Writer) int) error {
	f := newFramer(rw, rw)
	for {
		kind, script, err := f.read()
		if err != nil {
			return err
		}
		if kind != frameCmd {
			return fmt.Errorf("remote frame: want command, got %q", kind)
		}
		if err := serveOne(f, string(script), run); err != nil {
			return err
		}
	}
}

func serveOne(f *framer, script string, run func(string, io.Reader, io.Writer, io.Writer) int) error {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	codeCh := make(chan int, 1)
	go func() {
		code := run(script, inR, outW, errW)
		outW.Close()
		errW.Close()
		codeCh <- code
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pumpFrames(f, frameOut, outR)
	}()
	go func() {
		defer wg.Done()
		pumpFrames(f, frameErr, errR)
	}()
	feedErr := feedInput(f, inW, codeCh)
	code := <-codeCh
	inW.Close()
	wg.Wait()
	if feedErr != nil {
		return feedErr
	}
	return f.write(frameExit, []byte(strconv.Itoa(code)))
}

func pumpFrames(f *framer, kind byte, r io.Reader) {
	buf := make([]byte, frameChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := f.write(kind, buf[:n]); werr != nil {
				io.Copy(io.Discard, r)
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// feedInput copies input frames into w until end of input. A script that
// stops reading does not stall the loop: the rest is discarded.
func feedInput(f *framer, w *io.PipeWriter, done <-chan int) error {
	dropped := false
	for {
		kind, p, err := f.read()
		if err != nil {
			w.CloseWithError(err)
			return err
		}
		switch kind {
		case frameEnd:
			return w.Close()
		case frameIn:
		default:
			w.CloseWithError(fmt.Errorf("remote frame: want input, got %q", kind))
			return fmt.Errorf("remote frame: want input, got %q", kind)
		}
		if dropped {
			continue
		}
		select {
		case <-done:
			dropped = true
			w.Close()
			continue
		default:
		}
		errc := make(chan error, 1)
		go func() {
			_, err := w.Write(p)
			errc <- err
		}()
		select {
		case err := <-errc:
			if err != nil {
				dropped = true
			}
		case <-done:
			dropped = true
			w.Close()
			<-errc
		}
	}
}
