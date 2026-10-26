package cli

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// Client is the mayoi command's side of the socket.
type Client struct {
	Socket  string
	Version string
	// Start starts the app, which opens paths as it starts, nil for
	// none.
	Start func(paths []string) error
	// Inline leaves a request that only opens paths, with no app
	// running, to the app starting in this process: Run reports it did
	// not handle it.
	Inline bool
	// Starting is how long the app may take to answer once started.
	Starting time.Duration
}

// Run runs the mayoi command with args, in cwd, and returns its exit
// status, unless the app is to start in this process, when handled is
// false.
func (c Client) Run(args []string, cwd string, stdout, stderr io.Writer) (status int, handled bool) {
	req, err := Parse(args, cwd)
	switch {
	case errors.Is(err, ErrHelp):
		fmt.Fprint(stdout, Usage)
		return 0, true
	case errors.Is(err, ErrVersion):
		fmt.Fprintln(stdout, "Mayoi", c.Version)
		return 0, true
	case err != nil:
		fmt.Fprintf(stderr, "mayoi: %v\nRun mayoi --help for the options.\n", err)
		return 2, true
	}
	reply, err := Ask(c.Socket, req)
	if errors.Is(err, ErrNotRunning) {
		if req.Plain() && c.Inline {
			return 0, false
		}
		if req.Plain() && c.Start != nil {
			paths := make([]string, len(req.Paths))
			for i, t := range req.Paths {
				paths[i] = t.Path
			}
			if err := c.Start(paths); err != nil {
				fmt.Fprintf(stderr, "mayoi: %v\n", err)
				return 1, true
			}
			return 0, true
		}
		reply, err = c.startAndAsk(req)
	}
	if req.Wait && errors.Is(err, io.ErrUnexpectedEOF) {
		// The app quit, closing the files.
		err = nil
	}
	if err != nil {
		fmt.Fprintf(stderr, "mayoi: %v\n", err)
		return 1, true
	}
	fmt.Fprint(stdout, reply.Output)
	if reply.Error != "" {
		fmt.Fprintf(stderr, "mayoi: %s\n", reply.Error)
		return 1, true
	}
	return 0, true
}

// startAndAsk starts the app with nothing to open and asks it once it
// answers.
func (c Client) startAndAsk(req Request) (Reply, error) {
	if c.Start == nil {
		return Reply{}, ErrNotRunning
	}
	if err := c.Start(nil); err != nil {
		return Reply{}, err
	}
	wait := c.Starting
	if wait == 0 {
		wait = 20 * time.Second
	}
	for deadline := time.Now().Add(wait); ; {
		reply, err := Ask(c.Socket, req)
		if !errors.Is(err, ErrNotRunning) {
			return reply, err
		}
		if time.Now().After(deadline) {
			return Reply{}, errors.New("the app started but did not answer")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
