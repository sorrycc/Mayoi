package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == bridgeArg {
		HandleBridge(os.Args)
		os.Exit(1)
	}
	switch os.Getenv("MAYOI_FAKE_REMOTE") {
	case "ssh", "wsl":
		err := serveFrames(stdio{}, fakeRun)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// fakeRun is the shell inside a fake ssh or wsl. cat copies standard
// input, including NUL, and "exit N" ends with N. NOTIFY writes the lines
// a file watch reads, then holds the worker until stdin ends, as
// inotifywait does.
func fakeRun(script string, in io.Reader, stdout, stderr io.Writer) int {
	script = scriptBody(script)
	if script == "NOTIFY" {
		stderr.Write([]byte("Watches established\n"))
		stdout.Write([]byte("CREATE|/watched\n"))
		io.Copy(io.Discard, in)
		return 0
	}
	body, _ := io.ReadAll(in)
	switch {
	case script == "cat":
		stdout.Write(body)
		return 0
	case strings.HasPrefix(script, "exit "):
		n, _ := strconv.Atoi(strings.TrimSpace(script[len("exit "):]))
		stderr.Write(body)
		return n
	default:
		stdout.Write(body)
		return 0
	}
}

func scriptBody(script string) string {
	if i := strings.LastIndex(script, preamble); i >= 0 {
		return script[i+len(preamble):]
	}
	return script
}

func TestFrameCodecKeepsNUL(t *testing.T) {
	payload := []byte("C 4\n\x00len 12")
	var raw bytes.Buffer
	w := newFramer(bytes.NewReader(nil), &raw)
	if err := w.write(frameIn, payload); err != nil {
		t.Fatal(err)
	}
	if err := w.write(frameEnd, nil); err != nil {
		t.Fatal(err)
	}
	r := newFramer(&raw, io.Discard)
	kind, got, err := r.read()
	if err != nil || kind != frameIn || !bytes.Equal(got, payload) {
		t.Fatalf("frame %q %q %v", kind, got, err)
	}
	kind, got, err = r.read()
	if err != nil || kind != frameEnd || len(got) != 0 {
		t.Fatalf("end %q %q %v", kind, got, err)
	}
}

// TestFrameRoundTrip sends input whose length is not known ahead of
// time, including NUL and bytes that look like a frame header.
func TestFrameRoundTrip(t *testing.T) {
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		errc <- serveFrames(pipeRW{r: serverR, w: serverW}, fakeRun)
	}()
	defer clientR.Close()
	payload := append([]byte("a\x00b"), []byte("C 8\nnot-a-header")...)
	payload = append(payload, bytes.Repeat([]byte{0, '1', '\n'}, 200)...)
	var out, errb bytes.Buffer
	code, err := callFrames(context.Background(), pipeRW{r: clientR, w: clientW}, "cat", &oneByte{rest: append([]byte(nil), payload...)}, &out, &errb)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || !bytes.Equal(out.Bytes(), payload) || errb.Len() != 0 {
		t.Fatalf("code %d out %q err %q", code, out.Bytes(), errb.Bytes())
	}
	var errOut bytes.Buffer
	code, err = callFrames(context.Background(), pipeRW{r: clientR, w: clientW}, "exit 7", bytes.NewReader([]byte("no\x00")), io.Discard, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 || !bytes.Equal(errOut.Bytes(), []byte("no\x00")) {
		t.Fatalf("exit code %d stderr %q", code, errOut.Bytes())
	}
	clientW.Close()
	<-errc
}

// oneByte reports no length. The sender has to chunk until EOF.
type oneByte struct{ rest []byte }

func (o *oneByte) Read(p []byte) (int, error) {
	if len(o.rest) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = o.rest[0]
	o.rest = o.rest[1:]
	return 1, nil
}

type pipeRW struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipeRW) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRW) Write(b []byte) (int, error) { return p.w.Write(b) }

func TestFakeSSHRoundTrip(t *testing.T) { testFake(t, "ssh") }
func TestFakeWSLRoundTrip(t *testing.T) { testFake(t, "wsl") }

func testFake(t *testing.T, kind string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MAYOI_FAKE_REMOTE="+kind)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var procErr bytes.Buffer
	cmd.Stderr = &procErr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		in.Close()
		cmd.Wait()
	}()
	rw := streamRW{out, in}
	payload := []byte("\x00C 3\n")
	payload = append(payload, bytes.Repeat([]byte{'z', 0}, 100)...)
	var got bytes.Buffer
	code, err := callFrames(context.Background(), rw, "cat", bytes.NewReader(payload), &got, io.Discard)
	if err != nil {
		t.Fatalf("%s: %v\n%s", kind, err, procErr.Bytes())
	}
	if code != 0 || !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("%s code %d out %q", kind, code, got.Bytes())
	}
}

type streamRW struct {
	io.Reader
	io.Writer
}
