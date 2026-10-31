package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSocketPath(t *testing.T) {
	if got := SocketPath("/short"); got != filepath.Join("/short", "cli.sock") {
		t.Errorf("short: %s", got)
	}
	long := "/" + strings.Repeat("x", 120)
	if got := SocketPath(long); len(got) >= 100 || got == SocketPath(long+"y") {
		t.Errorf("long: %s", got)
	}
}

// The command asks the app, prints its answer, and starts it when it is
// not running.
func TestClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Unix socket")
	}
	dir, err := os.MkdirTemp("", "cli")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := SocketPath(dir)
	got := make(chan Request, 10)
	serve := func() func() {
		ln, err := Listen(sock)
		if err != nil {
			t.Fatal(err)
		}
		go Serve(ln, func(req Request, reply func(Reply)) {
			got <- req
			switch {
			case req.List:
				reply(Reply{Output: "a.b\tA\n"})
			case len(req.Run) > 0:
				reply(Reply{Error: "no command " + req.Run[0].ID})
			case req.Wait:
				go func() { time.Sleep(50 * time.Millisecond); reply(Reply{}) }()
			default:
				reply(Reply{})
			}
		})
		return func() { ln.Close() }
	}
	var started [][]string
	c := Client{Socket: sock, Version: "1.2", Starting: 2 * time.Second, Start: func(paths []string) error {
		started = append(started, paths)
		if paths == nil {
			serve()
		}
		return nil
	}}
	run := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		status, handled := c.Run(args, dir, &out, &errs)
		if !handled {
			t.Fatalf("%q not handled", args)
		}
		return status, out.String(), errs.String()
	}
	if s, out, _ := run("-v"); s != 0 || out != "Mayoi 1.2\n" {
		t.Errorf("-v: %d %q", s, out)
	}
	if s, _, errs := run("-q"); s != 2 || !strings.Contains(errs, "unknown option") {
		t.Errorf("-q: %d %q", s, errs)
	}
	// Not running: paths go to the app as it starts.
	if s, _, _ := run("a"); s != 0 || len(started) != 1 || started[0][0] != filepath.Join(dir, "a") {
		t.Errorf("start with paths: %d %v", s, started)
	}
	// Anything else waits for it to answer.
	if s, out, _ := run("commands"); s != 0 || out != "a.b\tA\n" || len(started) != 2 || started[1] != nil {
		t.Errorf("start and ask: %d %q %v", s, out, started)
	}
	<-got
	if s, _, errs := run("run", "x.y"); s != 1 || errs != "mayoi: no command x.y\n" {
		t.Errorf("error: %d %q", s, errs)
	}
	<-got
	if s, _, _ := run("-w", "f"); s != 0 {
		t.Errorf("wait: %d", s)
	}
	if req := <-got; !req.Wait || req.Paths[0].Path != filepath.Join(dir, "f") {
		t.Errorf("wait sent %+v", req)
	}
	if len(started) != 2 {
		t.Errorf("started again: %v", started)
	}
	// Inline leaves opening paths to the app in this process.
	c2 := Client{Socket: filepath.Join(dir, "none.sock"), Inline: true}
	if _, handled := c2.Run([]string{"a"}, dir, &bytes.Buffer{}, &bytes.Buffer{}); handled {
		t.Error("inline handled")
	}
}
