package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// bridgeArg starts the internal process that carries one command's
// standard streams. It is not a mayoi command: main recognizes it before
// the command client and the single-instance lock, and this process does
// not set MAYOI_CLI or open a window.
const bridgeArg = "--mayoi-bridge"

// HandleBridge runs that process when args ask for it, and does not
// return: the process exits with the remote command's status. Any other
// args return so the app can start.
func HandleBridge(args []string) {
	if len(args) != 4 || args[1] != bridgeArg {
		return
	}
	code, err := bridgeClient(args[2], args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

// bridgeClient dials the editor's loopback bridge, sends token, and copies
// standard streams until the pooled command exits.
func bridgeClient(addr, token string) (int, error) {
	if !loopbackAddr(addr) {
		return 0, fmt.Errorf("remote bridge: %s is not local", addr)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "%s\n", token); err != nil {
		return 0, err
	}
	f := newFramer(conn, conn)
	// The editor is the parent. If it is gone, leave: a blocked read of a
	// dropped socket must not keep this process, and the test binary, alive.
	go exitWhenParentExits()
	go writeInput(f, os.Stdin)
	return readBridgeOutput(f, os.Stdout, os.Stderr)
}

func writeInput(f *framer, r io.Reader) {
	buf := make([]byte, frameChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := f.write(frameIn, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			f.write(frameEnd, nil)
			return
		}
	}
}

func readBridgeOutput(f *framer, stdout, stderr io.Writer) (int, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	for {
		kind, p, err := f.read()
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
			return 0, fmt.Errorf("remote bridge: unexpected %c", kind)
		}
	}
}

// bridge is the editor's half of the local process. It listens on
// 127.0.0.1 and lends one pooled worker to each one-time token.
type bridge struct {
	ln      net.Listener
	pool    *pool
	mu      sync.Mutex
	tokens  map[string]string
	cancels []context.CancelFunc
	cmds    []*exec.Cmd
}

func (c *Conn) openBridge() (*bridge, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != Connected || c.pool == nil {
		return nil, ErrOffline
	}
	if c.bridge != nil {
		return c.bridge, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := &bridge{ln: ln, pool: c.pool, tokens: map[string]string{}}
	c.bridge = b
	go b.loop()
	return b, nil
}

func (b *bridge) loop() {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return
		}
		go b.serve(conn)
	}
}

func (b *bridge) register(script string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens == nil {
		return "", ErrOffline
	}
	b.tokens[token] = script
	return token, nil
}

func (b *bridge) take(token string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens == nil {
		return "", false
	}
	script, ok := b.tokens[token]
	delete(b.tokens, token)
	return script, ok
}

func (b *bridge) bind(cmd *exec.Cmd, cancel context.CancelFunc) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens == nil {
		return ErrOffline
	}
	b.cancels = append(b.cancels, cancel)
	b.cmds = append(b.cmds, cmd)
	return nil
}

func (b *bridge) close() {
	b.mu.Lock()
	b.tokens = nil
	cancels := b.cancels
	b.cancels = nil
	cmds := b.cmds
	b.cmds = nil
	b.mu.Unlock()
	b.ln.Close()
	for _, cancel := range cancels {
		cancel()
	}
	// Kill here, on this call, not in a goroutine. The process that started
	// the bridge can exit as soon as Disconnect returns, and a client that
	// has not reached its own main yet would keep the executable locked.
	for _, cmd := range cmds {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
}

func (b *bridge) serve(conn net.Conn) {
	defer conn.Close()
	if !fromLoopback(conn.RemoteAddr()) {
		return
	}
	f := newFramer(conn, conn)
	token, err := f.readLine()
	if err != nil {
		return
	}
	script, ok := b.take(token)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	gate := &callGate{cancel: cancel}
	go func() {
		err := bridgeStdin(f, pw)
		if err != nil {
			gate.fail()
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()
	code, err := b.pool.Call(ctx, script, pr, frameSink{f, frameOut}, frameSink{f, frameErr})
	// A successful call must not cancel ctx. The worker is still selecting
	// on it, and cancel would recycle a healthy process.
	if !gate.finish() || err != nil {
		if err != nil {
			cancel()
		}
		return
	}
	f.write(frameExit, []byte(strconv.Itoa(code)))
}

// callGate lets the stdin reader cancel the pooled command when the local
// process goes away, without cancelling after the command has finished.
type callGate struct {
	mu       sync.Mutex
	finished bool
	cancel   context.CancelFunc
}

func (g *callGate) fail() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.finished {
		return
	}
	g.finished = true
	g.cancel()
}

func (g *callGate) finish() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.finished {
		return false
	}
	g.finished = true
	return true
}

func bridgeStdin(f *framer, w io.Writer) error {
	for {
		kind, p, err := f.read()
		if err != nil {
			return err
		}
		switch kind {
		case frameEnd:
			return nil
		case frameIn:
			if _, err := w.Write(p); err != nil {
				return err
			}
		default:
			return fmt.Errorf("remote bridge: want input, got %q", kind)
		}
	}
}

type frameSink struct {
	f    *framer
	kind byte
}

func (s frameSink) Write(p []byte) (int, error) {
	if err := s.f.write(s.kind, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func fromLoopback(addr net.Addr) bool {
	return addr != nil && loopbackAddr(addr.String())
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Conn) bridgeCommand(stdin io.Reader, script string, args ...string) (*exec.Cmd, error) {
	b, err := c.openBridge()
	if err != nil {
		return nil, err
	}
	token, err := b.register(commandScript(script, args))
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	base := c.commandContext()
	if base == nil {
		base = context.Background()
	}
	// Cancelled when the connection ends, including when its own context
	// is already done, so Start does not launch a client after close.
	ctx, cancel := context.WithCancel(base)
	cmd := exec.CommandContext(ctx, exe, bridgeArg, b.ln.Addr().String(), token)
	if err := b.bind(cmd, cancel); err != nil {
		cancel()
		return nil, err
	}
	cmd.Stdin = stdin
	cmd.Env = bridgeEnv()
	hideConsole(cmd)
	return cmd, nil
}

// bridgeEnv is the editor's environment without the variables that make
// this same binary, in tests, block in init instead of bridging.
func bridgeEnv() []string {
	skip := map[string]bool{
		"MAYOI_FAKE_LSP":           true,
		"MAYOI_FAKE_WSL":           true,
		"MAYOI_FAKE_WSL_LOG":       true,
		"MAYOI_FAKE_WSL_NAMES":     true,
		"MAYOI_FAKE_WSL_STOP":      true,
		"MAYOI_TEST_RESOLVER":      true,
		"MAYOI_TEST_RESOLVER_MODE": true,
		"MAYOI_TEST_WORKER":        true,
	}
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, v := range env {
		key, _, _ := strings.Cut(v, "=")
		if skip[key] {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (c *Conn) commandContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}
