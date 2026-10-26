package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// SocketPath is where the app of the data folder dir answers the mayoi
// command: in the folder, else, where that is too long a path for a
// socket, in the temporary folder by a name of its own.
func SocketPath(dir string) string {
	if p := filepath.Join(dir, "cli.sock"); len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(dir))
	file := "mayoi-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(sum[:6]) + ".sock"
	return filepath.Join(os.TempDir(), file)
}

// Listen takes the socket at path, over one a crashed app left; another
// app answering there keeps it.
func Listen(path string) (net.Listener, error) {
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("%s is in use", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o600)
	return ln, nil
}

// Serve answers each request of ln with handle, which calls reply once,
// at once or later, as --wait does, until ln closes.
func Serve(ln net.Listener, handle func(req Request, reply func(Reply))) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var req Request
			if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil {
				return
			}
			conn.SetReadDeadline(time.Time{})
			done := make(chan Reply, 1)
			handle(req, func(r Reply) {
				select {
				case done <- r:
				default:
				}
			})
			// The command may give up waiting, as with ^C.
			gone := make(chan struct{})
			go func() {
				io.Copy(io.Discard, conn)
				close(gone)
			}()
			select {
			case r := <-done:
				json.NewEncoder(conn).Encode(r)
			case <-gone:
			}
		}()
	}
}

// ErrNotRunning is no app answering on the socket.
var ErrNotRunning = errors.New("the app is not running")

// Ask sends req to the app answering at path and returns its reply.
func Ask(path string, req Request) (Reply, error) {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return Reply{}, ErrNotRunning
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Reply{}, err
	}
	var r Reply
	if err := json.NewDecoder(conn).Decode(&r); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Reply{}, fmt.Errorf("the app gave no answer: %w", err)
	}
	return r, nil
}
